package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/lpmos/lpmos-go/cmd/regional-client/dhcp"
	"github.com/lpmos/lpmos-go/cmd/regional-client/kickstart"
	"github.com/lpmos/lpmos-go/cmd/regional-client/pxe"
	"github.com/lpmos/lpmos-go/cmd/regional-client/tftp"
	"github.com/lpmos/lpmos-go/pkg/etcd"
	"github.com/lpmos/lpmos-go/pkg/models"
)

// RegionalClient handles regional PXE/TFTP services with OPTIMIZED SCHEMA v3.0
type RegionalClient struct {
	idc        string
	etcdClient *etcd.Client
	ctx        context.Context
	cancel     context.CancelFunc
	leases     map[string]clientv3.LeaseID // sn -> leaseID mapping

	// PXE infrastructure
	dhcpServer         *dhcp.Server
	tftpServer         *tftp.Server
	pxeGenerator       *pxe.Generator
	kickstartGenerator *kickstart.Generator

	// Configuration
	serverIP     string
	networkIface string
	apiPort      string
	enableDHCP   bool
	enableTFTP   bool
	startedAt    time.Time
	staticRoot   string // Root directory for static files

	// Self registration
	selfLeaseID clientv3.LeaseID
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Usage: regional-client --idc=<idc> [--api-port=8081] [--enable-dhcp] [--enable-tftp] [--server-ip=192.168.100.1] [--interface=eth1]")
	}

	var idc string
	apiPort := "8081"
	enableDHCP := false
	enableTFTP := false
	serverIP := "192.168.100.1"
	networkIface := "eth1"
	staticRoot := "/tftpboot" // Root directory for static files

	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "--idc=") {
			idc = strings.TrimPrefix(arg, "--idc=")
		}
		if strings.HasPrefix(arg, "--api-port=") {
			apiPort = strings.TrimPrefix(arg, "--api-port=")
		}
		if arg == "--enable-dhcp" {
			enableDHCP = true
		}
		if arg == "--enable-tftp" {
			enableTFTP = true
		}
		if strings.HasPrefix(arg, "--server-ip=") {
			serverIP = strings.TrimPrefix(arg, "--server-ip=")
		}
		if strings.HasPrefix(arg, "--interface=") {
			networkIface = strings.TrimPrefix(arg, "--interface=")
		}
		if strings.HasPrefix(arg, "--static-root=") {
			staticRoot = strings.TrimPrefix(arg, "--static-root=")
		}
	}

	if idc == "" {
		log.Fatal("--idc flag is required")
	}

	log.Printf("Starting LPMOS Regional Client v3.0 for IDC: %s", idc)
	log.Printf("Configuration: API Port=%s, Server IP=%s, Interface=%s, Static Root=%s", apiPort, serverIP, networkIface, staticRoot)

	// Initialize etcd client
	etcdClient, err := etcd.NewClient(etcd.Config{
		Endpoints:      []string{"localhost:2379"},
		DialTimeout:    5 * time.Second,
		RequestTimeout: 10 * time.Second,
	})
	if err != nil {
		log.Fatalf("Failed to connect to etcd: %v", err)
	}
	defer etcdClient.Close() // Release etcd connection when done

	// Create regional client
	ctx, cancel := context.WithCancel(context.Background())
	rc := &RegionalClient{
		idc:                idc,
		etcdClient:         etcdClient,
		ctx:                ctx,
		cancel:             cancel,
		leases:             make(map[string]clientv3.LeaseID),
		serverIP:           serverIP,
		networkIface:       networkIface,
		apiPort:            apiPort,
		enableDHCP:         enableDHCP,
		enableTFTP:         enableTFTP,
		startedAt:          time.Now(),
		staticRoot:         staticRoot,
		kickstartGenerator: kickstart.NewGenerator(),
	}

	log.Println("✓ Kickstart/Preseed generator initialized")

	// Ensure static file directories exist
	if err := rc.ensureStaticDirectories(); err != nil {
		log.Fatalf("Failed to create static directories: %v", err)
	}
	log.Printf("✓ Static file directories ready: %s", staticRoot)

	// Register Regional Client to etcd
	if err := rc.registerToEtcd(); err != nil {
		log.Fatalf("Failed to register to etcd: %v", err)
	}
	log.Printf("✓ Regional Client registered to etcd: /os/region/%s", idc)

	// Initialize TFTP server if enabled
	if enableTFTP {
		if err := rc.initTFTP(); err != nil {
			log.Fatalf("Failed to initialize TFTP server: %v", err)
		}
		log.Println("✓ TFTP server initialized and started")
	}

	// Initialize PXE generator (requires TFTP)
	if enableTFTP {
		if err := rc.initPXE(); err != nil {
			log.Fatalf("Failed to initialize PXE generator: %v", err)
		}
		log.Println("✓ PXE generator initialized")
	}

	// Initialize DHCP server if enabled
	if enableDHCP {
		if err := rc.initDHCP(); err != nil {
			log.Fatalf("Failed to initialize DHCP server: %v", err)
		}
		log.Println("✓ DHCP server initialized and started")
	}

	// Start watchers
	go rc.watchServers()
	go rc.watchTasks()

	// Setup HTTP server for agents
	router := setupRouter(rc)
	srv := &http.Server{
		Addr:    ":" + apiPort,
		Handler: router,
	}

	go func() {
		log.Printf("Regional client API listening on :%s", apiPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down regional client...")

	// Unregister from etcd
	rc.unregisterFromEtcd()

	// Stop DHCP server
	if rc.dhcpServer != nil {
		log.Println("Stopping DHCP server...")
		rc.dhcpServer.Stop()
	}

	// Stop TFTP server
	if rc.tftpServer != nil {
		log.Println("Stopping TFTP server...")
		rc.tftpServer.Stop()
	}

	cancel()
	srv.Shutdown(context.Background())
}

func setupRouter(rc *RegionalClient) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()

	// Agent endpoints
	api := router.Group("/api/v1")
	{
		api.POST("/report", rc.handleHardwareReport)
		api.POST("/progress", rc.handleProgressUpdate)
		api.GET("/task/:sn", rc.getTask)

		// os-agent style endpoints
		device := api.Group("/device")
		{
			device.POST("/isInInstallQueue", rc.isInInstallQueue)
			device.POST("/getNextOperation", rc.getNextOperation)
			device.POST("/getHardwareConfig", rc.getHardwareConfig)
			device.POST("/getOSInstallConfig", rc.getOSInstallConfig)
			device.POST("/operationComplete", rc.operationComplete)
			device.POST("/installComplete", rc.installComplete)
		}

		// Kickstart/Preseed endpoints
		api.GET("/kickstart/:sn", rc.generateKickstart)
		api.GET("/preseed/:sn", rc.generatePreseed)

		// PXE infrastructure management endpoints
		pxe := api.Group("/pxe")
		{
			pxe.GET("/dhcp/status", rc.getDHCPStatus)
			pxe.GET("/dhcp/leases", rc.getDHCPLeases)
			pxe.GET("/tftp/status", rc.getTFTPStatus)
			pxe.GET("/tftp/files", rc.getTFTPFiles)
			pxe.GET("/configs", rc.getPXEConfigs)
		}
	}

	router.GET("/health", func(c *gin.Context) {
		status := gin.H{
			"status": "healthy",
			"idc":    rc.idc,
		}

		if rc.dhcpServer != nil {
			status["dhcp"] = "enabled"
		}
		if rc.tftpServer != nil {
			status["tftp"] = "enabled"
		}
		if rc.pxeGenerator != nil {
			status["pxe"] = "enabled"
		}

		c.JSON(http.StatusOK, status)
	})

	// Static files for HTTP download (kernel, initramfs, repos, etc.)
	staticDir := rc.staticRoot + "/static"
	reposDir := rc.staticRoot + "/repos"

	router.Static("/static", staticDir)
	router.Static("/repos", reposDir)

	// File listing endpoints (for debugging and verification)
	api.GET("/files/static", func(c *gin.Context) {
		files, err := listDirectory(staticDir, "")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"path": "/static", "files": files})
	})

	api.GET("/files/repos", func(c *gin.Context) {
		files, err := listDirectory(reposDir, "")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"path": "/repos", "files": files})
	})

	return router
}

// initTFTP initializes and starts the TFTP server
func (rc *RegionalClient) initTFTP() error {
	tftpRoot := rc.staticRoot

	// Create file manager and ensure directories exist
	fileManager := tftp.NewFileManager(tftpRoot)
	if err := fileManager.EnsureDirectories(); err != nil {
		return fmt.Errorf("failed to create TFTP directories: %w", err)
	}

	// Create TFTP server
	tftpConfig := tftp.Config{
		RootDir:    tftpRoot,
		ListenAddr: ":69",
		MaxClients: 100,
		Timeout:    30 * time.Second,
		BlockSize:  512,
	}

	server, err := tftp.NewServer(tftpConfig)
	if err != nil {
		return fmt.Errorf("failed to create TFTP server: %w", err)
	}

	if err := server.Start(); err != nil {
		return fmt.Errorf("failed to start TFTP server: %w", err)
	}

	rc.tftpServer = server
	log.Printf("[%s] TFTP server started: root=%s, port=69", rc.idc, tftpRoot)
	return nil
}

// initPXE initializes the PXE configuration generator
func (rc *RegionalClient) initPXE() error {
	generator, err := pxe.NewGenerator(pxe.Config{
		TFTPRoot: rc.staticRoot,
	})
	if err != nil {
		return fmt.Errorf("failed to create PXE generator: %w", err)
	}

	// Generate default PXE configuration
	if err := generator.GenerateDefaultConfig(); err != nil {
		return fmt.Errorf("failed to generate default PXE config: %w", err)
	}

	rc.pxeGenerator = generator
	log.Printf("[%s] PXE generator initialized", rc.idc)
	return nil
}

// initDHCP initializes and starts the DHCP server
func (rc *RegionalClient) initDHCP() error {
	dhcpConfig := dhcp.Config{
		Interface:  rc.networkIface,
		ServerIP:   rc.serverIP,
		Gateway:    rc.serverIP,
		DNSServers: []string{rc.serverIP, "8.8.8.8"},
		TFTPServer: rc.serverIP,
		BootFile:   "pxelinux.0",
		LeaseTime:  24 * 3600 * time.Second, // 24 hours (extended for installation)
		StartIP:    rc.serverIP[:strings.LastIndex(rc.serverIP, ".")] + ".10",
		EndIP:      rc.serverIP[:strings.LastIndex(rc.serverIP, ".")] + ".200",
		Netmask:    "255.255.255.0",
	}

	server, err := dhcp.NewServer(dhcpConfig)
	if err != nil {
		return fmt.Errorf("failed to create DHCP server: %w", err)
	}

	if err := server.Start(); err != nil {
		return fmt.Errorf("failed to start DHCP server: %w", err)
	}

	rc.dhcpServer = server
	log.Printf("[%s] DHCP server started: pool=%s-%s, port=67",
		rc.idc, dhcpConfig.StartIP, dhcpConfig.EndIP)
	return nil
}

// configurePXEBoot configures PXE boot environment for a task
func (rc *RegionalClient) configurePXEBoot(task *models.TaskV3) {
	log.Printf("[%s] Configuring PXE boot for %s (MAC: %s, IP: %s)",
		rc.idc, task.SN, task.MAC, task.IP)

	// Parse MAC address
	mac, err := net.ParseMAC(task.MAC)
	if err != nil {
		log.Printf("[%s] ERROR: Invalid MAC address %s: %v", rc.idc, task.MAC, err)
		return
	}

	// Step 1: Add DHCP static binding (if DHCP is enabled)
	if rc.dhcpServer != nil {
		if err := rc.dhcpServer.AddStaticBinding(
			task.MAC,
			task.IP,
			task.Hostname,
			"pxelinux.0",
		); err != nil {
			log.Printf("[%s] ERROR: Failed to add DHCP binding for %s: %v", rc.idc, task.SN, err)
			return
		}
		log.Printf("[%s] ✓ DHCP binding added: %s -> %s", rc.idc, task.MAC, task.IP)
	}

	// Step 2: Generate PXE configuration (if PXE is enabled)
	if rc.pxeGenerator != nil {
		bootConfig := &pxe.BootConfig{
			MAC:          mac,
			IP:           net.ParseIP(task.IP),
			Hostname:     task.Hostname,
			OSType:       task.OSType,
			OSVersion:    task.OSVersion,
			KernelPath:   fmt.Sprintf("/static/kernels/vmlinuz-%s-%s", task.OSType, task.OSVersion),
			InitrdPath:   fmt.Sprintf("/static/initramfs/initrd-%s-%s.img", task.OSType, task.OSVersion),
			RegionalURL:  fmt.Sprintf("http://%s:8081/api/v1", rc.serverIP),
			SerialNumber: task.SN,
			DataCenter:   rc.idc,
		}

		if err := rc.pxeGenerator.GenerateConfig(bootConfig); err != nil {
			log.Printf("[%s] ERROR: Failed to generate PXE config for %s: %v", rc.idc, task.SN, err)
			return
		}
		log.Printf("[%s] ✓ PXE configuration generated: %s/pxelinux.cfg/01-%s",
			rc.idc, rc.staticRoot, strings.ReplaceAll(strings.ToLower(task.MAC), ":", "-"))
	}

	// Step 3: Configure switch (TODO: Implement switch management module)
	// rc.switchManager.ConfigurePort(task.SwitchPort, task.InstallVLAN)
	log.Printf("[%s] TODO: Configure switch for %s", rc.idc, task.SN)

	// Step 4: Control BMC to reboot into PXE (TODO: Implement BMC control module)
	// rc.bmcController.SetBootDevice("pxe")
	// rc.bmcController.PowerCycle()
	log.Printf("[%s] TODO: Control BMC to reboot %s into PXE mode", rc.idc, task.SN)

	// Update task status to indicate PXE is ready
	taskKey := etcd.TaskKeyV3(rc.idc, task.SN)
	rc.etcdClient.AtomicUpdate(taskKey, func(data []byte) (interface{}, error) {
		var t models.TaskV3
		if err := json.Unmarshal(data, &t); err != nil {
			return nil, err
		}

		// Set PXE configured flag to prevent reconfiguration
		t.PXEConfigured = true
		// Don't add logs to etcd to prevent database bloat
		// Logs are already written to Regional Client's log output
		t.UpdatedAt = time.Now()

		return t, nil
	})

	log.Printf("[%s] ✓ PXE boot environment configured for %s", rc.idc, task.SN)
}

// cleanupPXEBoot cleans up PXE boot configuration after installation completes
func (rc *RegionalClient) cleanupPXEBoot(task *models.TaskV3) {
	log.Printf("[%s] Cleaning up PXE boot configuration for %s", rc.idc, task.SN)

	mac, err := net.ParseMAC(task.MAC)
	if err != nil {
		log.Printf("[%s] ERROR: Invalid MAC address %s: %v", rc.idc, task.MAC, err)
		return
	}

	// Remove PXE configuration
	if rc.pxeGenerator != nil {
		if err := rc.pxeGenerator.RemoveConfig(mac); err != nil {
			log.Printf("[%s] ERROR: Failed to remove PXE config for %s: %v", rc.idc, task.SN, err)
		} else {
			log.Printf("[%s] ✓ PXE configuration removed", rc.idc)
		}
	}

	// Remove DHCP binding
	if rc.dhcpServer != nil {
		if err := rc.dhcpServer.RemoveStaticBinding(task.MAC); err != nil {
			log.Printf("[%s] ERROR: Failed to remove DHCP binding for %s: %v", rc.idc, task.SN, err)
		} else {
			log.Printf("[%s] ✓ DHCP binding removed", rc.idc)
		}
	}

	// Restore switch configuration (TODO)
	// rc.switchManager.ConfigurePort(task.SwitchPort, task.ProductionVLAN)

	log.Printf("[%s] ✓ PXE boot configuration cleaned up for %s", rc.idc, task.SN)
}

// getDHCPStatus returns DHCP server status
func (rc *RegionalClient) getDHCPStatus(c *gin.Context) {
	if rc.dhcpServer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "DHCP server not enabled"})
		return
	}

	bindings := rc.dhcpServer.GetStaticBindings()
	c.JSON(http.StatusOK, gin.H{
		"status":          "running",
		"static_bindings": len(bindings),
	})
}

// getDHCPLeases returns current DHCP leases
func (rc *RegionalClient) getDHCPLeases(c *gin.Context) {
	if rc.dhcpServer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "DHCP server not enabled"})
		return
	}

	leases := rc.dhcpServer.GetLeases()
	bindings := rc.dhcpServer.GetStaticBindings()

	c.JSON(http.StatusOK, gin.H{
		"leases":   leases,
		"bindings": bindings,
	})
}

// getTFTPStatus returns TFTP server status
func (rc *RegionalClient) getTFTPStatus(c *gin.Context) {
	if rc.tftpServer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "TFTP server not enabled"})
		return
	}

	stats := rc.tftpServer.GetStats()
	c.JSON(http.StatusOK, gin.H{
		"status":         "running",
		"total_requests": stats.TotalRequests,
		"success":        stats.SuccessRequests,
		"failed":         stats.FailedRequests,
		"bytes_served":   stats.TotalBytesServed,
	})
}

// getTFTPFiles returns list of files in TFTP root
func (rc *RegionalClient) getTFTPFiles(c *gin.Context) {
	if rc.tftpServer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "TFTP server not enabled"})
		return
	}

	files, err := rc.tftpServer.ListFiles()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"files": files,
		"total": len(files),
	})
}

// getPXEConfigs returns list of PXE configurations
func (rc *RegionalClient) getPXEConfigs(c *gin.Context) {
	if rc.pxeGenerator == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "PXE generator not enabled"})
		return
	}

	configs, err := rc.pxeGenerator.ListConfigs()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"configs": configs,
		"total":   len(configs),
	})
}

// ensureStaticDirectories creates necessary directories for static files
func (rc *RegionalClient) ensureStaticDirectories() error {
	dirs := []string{
		rc.staticRoot,
		rc.staticRoot + "/static",
		rc.staticRoot + "/static/kernels",
		rc.staticRoot + "/static/initramfs",
		rc.staticRoot + "/repos",
		rc.staticRoot + "/repos/ubuntu",
		rc.staticRoot + "/repos/centos",
		rc.staticRoot + "/repos/rocky",
		rc.staticRoot + "/repos/debian",
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Create README files
	readmeContent := `# LPMOS Static Files Directory

This directory contains static files served over HTTP for PXE boot and OS installation.

## Directory Structure

/static/
  ├── kernels/          # Linux kernels (vmlinuz)
  ├── initramfs/        # Initramfs images (lpmos-agent-initramfs.gz)
  └── ...

/repos/
  ├── ubuntu/           # Ubuntu repository mirrors
  │   ├── 20.04/
  │   └── 22.04/
  ├── centos/           # CentOS repository mirrors
  │   ├── 7/
  │   └── 8/
  ├── rocky/            # Rocky Linux repository mirrors
  │   ├── 8/
  │   └── 9/
  └── debian/           # Debian repository mirrors
      ├── 11/
      └── 12/

## Usage

Files are accessible via HTTP:
- http://<server-ip>:8081/static/kernels/vmlinuz
- http://<server-ip>:8081/static/initramfs/lpmos-agent-initramfs.gz
- http://<server-ip>:8081/repos/ubuntu/22.04/...

## File Listing API

- GET /api/v1/files/static - List files in /static
- GET /api/v1/files/repos  - List files in /repos
`

	readmePath := rc.staticRoot + "/README.md"
	if err := os.WriteFile(readmePath, []byte(readmeContent), 0644); err != nil {
		log.Printf("Warning: Failed to create README: %v", err)
	}

	return nil
}

// listDirectory recursively lists files in a directory
func listDirectory(dir string, prefix string) ([]map[string]interface{}, error) {
	var files []map[string]interface{}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		fullPath := dir + "/" + entry.Name()
		relativePath := prefix + "/" + entry.Name()

		info, err := entry.Info()
		if err != nil {
			continue
		}

		fileInfo := map[string]interface{}{
			"name":  entry.Name(),
			"path":  relativePath,
			"is_dir": entry.IsDir(),
			"size":  info.Size(),
		}

		if !entry.IsDir() {
			fileInfo["modified"] = info.ModTime().Format(time.RFC3339)
		}

		files = append(files, fileInfo)

		// Recursively list subdirectories (limit depth to avoid too much data)
		if entry.IsDir() && len(prefix) < 100 {
			subFiles, err := listDirectory(fullPath, relativePath)
			if err == nil {
				files = append(files, subFiles...)
			}
		}
	}

	return files, nil
}

// registerToEtcd registers Regional Client to etcd with heartbeat
func (rc *RegionalClient) registerToEtcd() error {
	// Create Regional Client info
	info := map[string]interface{}{
		"idc":          rc.idc,
		"server_ip":    rc.serverIP,
		"api_port":     rc.apiPort,
		"dhcp_enabled": rc.enableDHCP,
		"tftp_enabled": rc.enableTFTP,
		"started_at":   rc.startedAt.Format(time.RFC3339),
		"status":       "online",
	}

	infoKey := fmt.Sprintf("/os/region/%s/info", rc.idc)
	if err := rc.etcdClient.Put(infoKey, info); err != nil {
		return fmt.Errorf("failed to put regional client info: %w", err)
	}

	// Start heartbeat
	go rc.maintainHeartbeat()

	return nil
}

// maintainHeartbeat maintains Regional Client heartbeat with lease
func (rc *RegionalClient) maintainHeartbeat() {
	heartbeatKey := fmt.Sprintf("/os/region/%s/heartbeat", rc.idc)

	for {
		// Create lease with 30s TTL
		leaseID, keepAliveChan, err := rc.etcdClient.GrantLease(rc.ctx, 30)
		if err != nil {
			log.Printf("[%s] Failed to create heartbeat lease: %v", rc.idc, err)
			time.Sleep(5 * time.Second)
			continue
		}

		rc.selfLeaseID = leaseID

		// Put heartbeat key with lease
		heartbeatValue := map[string]interface{}{
			"status":       "online",
			"last_updated": time.Now().Format(time.RFC3339),
			"lease_id":     leaseID,
		}

		if err := rc.etcdClient.Put(heartbeatKey, heartbeatValue); err != nil {
			log.Printf("[%s] Failed to put heartbeat: %v", rc.idc, err)
			time.Sleep(5 * time.Second)
			continue
		}

		log.Printf("[%s] Heartbeat started (lease: %d)", rc.idc, leaseID)

		// Keep-alive loop - continuously consume responses
	keepAliveLoop:
		for {
			select {
			case <-rc.ctx.Done():
				log.Printf("[%s] Heartbeat stopped (context cancelled)", rc.idc)
				return

			case ka, ok := <-keepAliveChan:
				if !ok {
					// Channel closed, need to recreate lease
					log.Printf("[%s] Heartbeat channel closed, recreating lease...", rc.idc)
					break keepAliveLoop
				}
				if ka == nil {
					// Keep-alive failed
					log.Printf("[%s] Heartbeat keep-alive failed, recreating lease...", rc.idc)
					break keepAliveLoop
				}
				// Successfully renewed, continue consuming
			}
		}

		// Wait before recreating lease
		time.Sleep(2 * time.Second)
	}
}

// unregisterFromEtcd removes Regional Client registration from etcd
func (rc *RegionalClient) unregisterFromEtcd() {
	log.Printf("[%s] Unregistering from etcd...", rc.idc)

	// Update status to offline
	infoKey := fmt.Sprintf("/os/region/%s/info", rc.idc)
	info := map[string]interface{}{
		"idc":          rc.idc,
		"server_ip":    rc.serverIP,
		"api_port":     rc.apiPort,
		"dhcp_enabled": rc.enableDHCP,
		"tftp_enabled": rc.enableTFTP,
		"started_at":   rc.startedAt.Format(time.RFC3339),
		"stopped_at":   time.Now().Format(time.RFC3339),
		"status":       "offline",
	}

	if err := rc.etcdClient.Put(infoKey, info); err != nil {
		log.Printf("[%s] Warning: Failed to update status to offline: %v", rc.idc, err)
	}

	// Revoke heartbeat lease (will delete heartbeat key)
	if rc.selfLeaseID != 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := rc.etcdClient.GetClient().Revoke(ctx, rc.selfLeaseID); err != nil {
			log.Printf("[%s] Warning: Failed to revoke heartbeat lease: %v", rc.idc, err)
		}
	}

	log.Printf("[%s] Unregistered from etcd", rc.idc)
}

// watchServers watches for new servers (INDIVIDUAL KEYS v3.0)
func (rc *RegionalClient) watchServers() {
	prefix := etcd.ServerPrefix(rc.idc)
	log.Printf("[%s] Watching for new servers at: %s", rc.idc, prefix)

	watchChan := rc.etcdClient.Watch(rc.ctx, prefix, true)

	for watchResp := range watchChan {
		for _, event := range watchResp.Events {
			if event.Type == clientv3.EventTypePut {
				var serverEntry models.ServerEntry
				if err := json.Unmarshal(event.Kv.Value, &serverEntry); err == nil {
					log.Printf("[%s] New server detected: %s (status: %s)", rc.idc, serverEntry.SN, serverEntry.Status)

					// Start lease-based heartbeat for this server
					go rc.startHeartbeat(serverEntry.SN)
				}
			}
		}
	}
}

// watchTasks watches for task updates
func (rc *RegionalClient) watchTasks() {
	prefix := etcd.MachinePrefix(rc.idc)
	log.Printf("[%s] Watching for task updates at: %s", rc.idc, prefix)

	watchChan := rc.etcdClient.Watch(rc.ctx, prefix, true)

	for watchResp := range watchChan {
		for _, event := range watchResp.Events {
			key := string(event.Kv.Key)

			if !strings.HasSuffix(key, "/task") {
				continue
			}

			if event.Type == clientv3.EventTypePut {
				var task models.TaskV3
				if err := json.Unmarshal(event.Kv.Value, &task); err == nil {
					// Only configure PXE if task is approved and PXE not yet configured
					if task.Status == models.TaskStatusApproved && !task.PXEConfigured {
						log.Printf("[%s] Task approved for %s, configuring PXE boot...", rc.idc, task.SN)
						// Configure PXE boot environment
						go rc.configurePXEBoot(&task)
					}
				}
			}
		}
	}
}

// startHeartbeat starts lease-based heartbeat for a server
func (rc *RegionalClient) startHeartbeat(sn string) {
	leaseKey := etcd.LeaseKey(rc.idc, sn)

	// Create lease with 30s TTL
	leaseID, keepAliveChan, err := rc.etcdClient.GrantLease(rc.ctx, 30)
	if err != nil {
		log.Printf("[%s] Failed to create lease for %s: %v", rc.idc, sn, err)
		return
	}

	rc.leases[sn] = leaseID

	// Put lease key
	if err := rc.etcdClient.Put(leaseKey, fmt.Sprintf("lease-%d", leaseID)); err != nil {
		log.Printf("[%s] Failed to put lease key for %s: %v", rc.idc, sn, err)
		return
	}

	log.Printf("[%s] Started heartbeat for %s (lease: %d)", rc.idc, sn, leaseID)

	// Keep-alive loop
	for {
		select {
		case ka, ok := <-keepAliveChan:
			if !ok || ka == nil {
				log.Printf("[%s] Lease expired for %s", rc.idc, sn)
				delete(rc.leases, sn)
				return
			}
			// Lease refreshed successfully
		case <-rc.ctx.Done():
			return
		}
	}
}

// handleHardwareReport handles hardware reports from agents (ATOMIC UPDATE)
func (rc *RegionalClient) handleHardwareReport(c *gin.Context) {
	var req models.AgentReportRequestV3
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[%s] Received hardware report from %s (MAC: %s)", rc.idc, req.SN, req.MAC)

	// Find task by SN
	taskKey := etcd.TaskKeyV3(rc.idc, req.SN)

	// Atomic update to merged task structure
	err := rc.etcdClient.AtomicUpdate(taskKey, func(data []byte) (interface{}, error) {
		var task models.TaskV3
		if err := json.Unmarshal(data, &task); err != nil {
			return nil, fmt.Errorf("task not found or invalid")
		}

		// Verify MAC matches (if provided in task)
		if task.MAC != "" && !strings.EqualFold(task.MAC, req.MAC) {
			return nil, fmt.Errorf("MAC mismatch: expected %s, got %s", task.MAC, req.MAC)
		}

		// Update MAC if not set
		if task.MAC == "" {
			task.MAC = req.MAC
		}

		// Add hardware collection progress
		task.Progress = append(task.Progress, models.ProgressStep{
			Step:      "hardware_collect",
			Percent:   100,
			Timestamp: time.Now(),
			Message:   fmt.Sprintf("Hardware: %d cores, %dGB RAM, %d disks", req.Hardware.CPU.Cores, req.Hardware.Memory.TotalGB, len(req.Hardware.Disks)),
		})

		// Add log entry
		task.Logs = append(task.Logs, fmt.Sprintf("[INFO] Hardware collected: %d cores, %dGB RAM", req.Hardware.CPU.Cores, req.Hardware.Memory.TotalGB))
		task.UpdatedAt = time.Now()

		return task, nil
	})

	if err != nil {
		// Store as unmatched report (without timestamp in key)
		unmatchedKey := fmt.Sprintf("/os/unmatched_reports/%s/%s", rc.idc, req.MAC)
		rc.etcdClient.Put(unmatchedKey, req)

		log.Printf("[%s] Hardware report unmatched (stored): %s", rc.idc, req.MAC)
		c.JSON(http.StatusNotFound, gin.H{"error": "No matching task found", "retry_after": 10})
		return
	}

	// Store hardware metadata separately
	metaKey := etcd.MetaKey(rc.idc, req.SN)
	rc.etcdClient.Put(metaKey, req.Hardware)

	// Update server entry with agent info
	serverKey := etcd.ServerKey(rc.idc, req.SN)
	serverEntry := models.ServerEntry{
		SN:      req.SN,
		MAC:     req.MAC,
		Status:  "registered", // Agent has reported
		AddedAt: time.Now(),
	}
	if err := rc.etcdClient.Put(serverKey, serverEntry); err != nil {
		log.Printf("[%s] Warning: Failed to update server entry: %v", rc.idc, err)
	}

	log.Printf("[%s] Hardware report processed for %s", rc.idc, req.SN)
	c.JSON(http.StatusOK, gin.H{"message": "Hardware reported successfully"})
}

// handleProgressUpdate handles progress updates from agents (ATOMIC UPDATE)
func (rc *RegionalClient) handleProgressUpdate(c *gin.Context) {
	var req models.AgentProgressRequestV3
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[%s] Progress update from %s: %s (%d%%)", rc.idc, req.SN, req.Step, req.Percent)

	taskKey := etcd.TaskKeyV3(rc.idc, req.SN)

	// Atomic update to merged task
	err := rc.etcdClient.AtomicUpdate(taskKey, func(data []byte) (interface{}, error) {
		var task models.TaskV3
		if err := json.Unmarshal(data, &task); err != nil {
			return nil, err
		}

		// Verify task ID matches
		if task.TaskID != req.TaskID {
			return nil, fmt.Errorf("task ID mismatch")
		}

		// Add progress step
		task.Progress = append(task.Progress, models.ProgressStep{
			Step:      req.Step,
			Percent:   req.Percent,
			Timestamp: time.Now(),
			Message:   req.Message,
		})

		// Update status based on progress
		if req.Percent >= 100 && req.Step == "completed" {
			task.Status = models.TaskStatusCompleted
			task.StatusHistory = append(task.StatusHistory, models.StatusChange{
				Status:    models.TaskStatusCompleted,
				Timestamp: time.Now(),
				Reason:    "Installation completed successfully",
			})
		} else if req.Percent > 0 && task.Status != models.TaskStatusInstalling {
			task.Status = models.TaskStatusInstalling
			task.StatusHistory = append(task.StatusHistory, models.StatusChange{
				Status:    models.TaskStatusInstalling,
				Timestamp: time.Now(),
				Reason:    "Installation started",
			})
		}

		// Add log entry
		logEntry := fmt.Sprintf("[INFO] %s: %s (%d%%)", req.Step, req.Message, req.Percent)
		task.Logs = append(task.Logs, logEntry)
		task.UpdatedAt = time.Now()

		return task, nil
	})

	if err != nil {
		log.Printf("[%s] Failed to update progress: %v", rc.idc, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Progress updated"})
}

// getTask retrieves task details for an agent
func (rc *RegionalClient) getTask(c *gin.Context) {
	sn := c.Param("sn")
	taskKey := etcd.TaskKeyV3(rc.idc, sn)

	var task models.TaskV3
	if err := rc.etcdClient.GetJSON(taskKey, &task); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task not found"})
		return
	}

	c.JSON(http.StatusOK, task)
}

// isInInstallQueue checks if machine is in install queue (os-agent workflow)
func (rc *RegionalClient) isInInstallQueue(c *gin.Context) {
	var req struct {
		SN string `json:"sn" binding:"required"`
	}
	if err := c.BindJSON(&req); err != nil {
		log.Printf("[%s] isInInstallQueue: Failed to bind JSON: %v", rc.idc, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[%s] isInInstallQueue: Checking queue for SN=%s", rc.idc, req.SN)

	// Check if task exists and is approved
	taskKey := etcd.TaskKeyV3(rc.idc, req.SN)
	log.Printf("[%s] isInInstallQueue: Task key=%s", rc.idc, taskKey)

	var task models.TaskV3
	if err := rc.etcdClient.GetJSON(taskKey, &task); err != nil {
		// No task found - not in queue yet
		log.Printf("[%s] isInInstallQueue: Failed to get task from etcd: %v", rc.idc, err)
		c.JSON(http.StatusOK, gin.H{"result": false})
		return
	}

	log.Printf("[%s] isInInstallQueue: Task found, status=%s", rc.idc, task.Status)

	// Check if task is approved (approved or later stages mean it's in queue)
	inQueue := task.Status == models.TaskStatusApproved ||
		task.Status == models.TaskStatusInstalling ||
		task.Status == models.TaskStatusCompleted

	log.Printf("[%s] isInInstallQueue query from %s: %v (status: %s)",
		rc.idc, req.SN, inQueue, task.Status)

	c.JSON(http.StatusOK, gin.H{"result": inQueue})
}

// getNextOperation returns the next operation for agent to execute (os-agent workflow)
func (rc *RegionalClient) getNextOperation(c *gin.Context) {
	var req struct {
		SN string `json:"sn" binding:"required"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	taskKey := etcd.TaskKeyV3(rc.idc, req.SN)
	var task models.TaskV3
	if err := rc.etcdClient.GetJSON(taskKey, &task); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task not found"})
		return
	}

	// Determine next operation based on task status and progress
	var operation string
	var data interface{}

	switch task.Status {
	case models.TaskStatusApproved:
		// Task just approved - start with hardware config
		operation = "hardware_config"
		data = gin.H{"message": "Configure hardware settings"}

	case models.TaskStatusInstalling:
		// Check progress to determine next step
		lastProgress := 0
		lastStep := ""
		if len(task.Progress) > 0 {
			lastProgress = task.Progress[len(task.Progress)-1].Percent
			lastStep = task.Progress[len(task.Progress)-1].Step
		}

		if lastProgress < 40 || lastStep == "" {
			operation = "hardware_config"
			data = gin.H{"message": "Configure hardware settings"}
		} else if lastProgress < 50 {
			operation = "network_config"
			data = gin.H{"message": "Configure network settings"}
		} else if lastProgress < 100 {
			operation = "os_install"

			// 决定安装方式并返回完整配置
			installMethod := rc.determineInstallMethod(&task)

			config := &models.OSInstallConfig{
				Method:    installMethod,
				OSType:    task.OSType,
				OSVersion: task.OSVersion,
				MirrorURL: fmt.Sprintf("http://%s:8081", rc.serverIP),
				Network: models.NetworkConfig{
					Interface: "eth0",
					Method:    "static",
					IP:        task.IP,
					Netmask:   "255.255.255.0",
					Gateway:   rc.serverIP,
					DNS:       rc.serverIP,
					Hostname:  task.Hostname,
				},
			}

			if installMethod == models.InstallMethodKickstart {
				config.KickstartURL = fmt.Sprintf("http://%s:8081/api/v1/kickstart/%s", rc.serverIP, req.SN)
			} else {
				config.DiskLayout = models.DiskLayoutConfig{
					RootDisk:       "/dev/sda",
					PartitionTable: "gpt",
					Partitions: []models.PartitionConfig{
						{MountPoint: "/boot", Size: "1G", FSType: "ext4"},
						{MountPoint: "swap", Size: "16G", FSType: "swap"},
						{MountPoint: "/", Size: "0", FSType: "ext4"},
					},
				}
				config.Packages = []string{
					"openssh-server",
					"wget",
					"curl",
					"vim",
					"net-tools",
				}
			}

			data = config
		} else {
			operation = "reboot"
			data = gin.H{"message": "Reboot to new system"}
		}

	case models.TaskStatusCompleted:
		operation = "complete"
		data = gin.H{"message": "All operations completed"}

	default:
		operation = "wait"
		data = gin.H{"message": "Waiting for approval"}
	}

	log.Printf("[%s] getNextOperation for %s: %s", rc.idc, req.SN, operation)
	c.JSON(http.StatusOK, gin.H{
		"operation": operation,
		"data":      data,
	})
}

// getHardwareConfig returns hardware configuration scripts (os-agent workflow)
func (rc *RegionalClient) getHardwareConfig(c *gin.Context) {
	var req struct {
		SN string `json:"sn" binding:"required"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// In a real implementation, this would fetch hardware config scripts from database
	// For now, return empty scripts (simulated)
	scripts := []gin.H{
		{
			"name":   "raid_config",
			"script": "", // base64 encoded script would go here
		},
	}

	log.Printf("[%s] getHardwareConfig for %s: %d scripts", rc.idc, req.SN, len(scripts))
	c.JSON(http.StatusOK, gin.H{"scripts": scripts})
}

// operationComplete handles operation completion report (os-agent workflow)
func (rc *RegionalClient) operationComplete(c *gin.Context) {
	var req struct {
		SN        string `json:"sn" binding:"required"`
		Operation string `json:"operation" binding:"required"`
		Success   bool   `json:"success"`
		Message   string `json:"message"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[%s] Operation complete from %s: %s (success: %v) - %s",
		rc.idc, req.SN, req.Operation, req.Success, req.Message)

	// Update task status based on operation
	taskKey := etcd.TaskKeyV3(rc.idc, req.SN)
	var completedTask *models.TaskV3
	err := rc.etcdClient.AtomicUpdate(taskKey, func(data []byte) (interface{}, error) {
		var task models.TaskV3
		if err := json.Unmarshal(data, &task); err != nil {
			return nil, err
		}

		// Add progress entry
		percent := 0
		switch req.Operation {
		case "hardware_config":
			percent = 40
			task.Status = models.TaskStatusInstalling
		case "network_config":
			percent = 50
		case "os_install":
			percent = 100
			if req.Success {
				task.Status = models.TaskStatusCompleted
				completedTask = &task
			} else {
				task.Status = models.TaskStatusFailed
			}
		}

		task.Progress = append(task.Progress, models.ProgressStep{
			Step:      req.Operation,
			Percent:   percent,
			Timestamp: time.Now(),
			Message:   req.Message,
		})

		task.Logs = append(task.Logs, fmt.Sprintf("[INFO] %s: %s", req.Operation, req.Message))
		task.UpdatedAt = time.Now()

		return task, nil
	})

	if err != nil {
		log.Printf("[%s] Failed to update task for operation complete: %v", rc.idc, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update task"})
		return
	}

	// Clean up PXE boot configuration if installation completed
	if completedTask != nil {
		go rc.cleanupPXEBoot(completedTask)
	}

	c.JSON(http.StatusOK, gin.H{"message": "Operation status updated"})
}

// ========== OS Installation Handlers ==========

// determineInstallMethod determines the installation method based on task configuration
func (rc *RegionalClient) determineInstallMethod(task *models.TaskV3) models.InstallMethod {
	// 场景 1: 如果任务指定了特殊的磁盘布局或软件包，使用 Agent 直接安装
	if task.DiskLayout != "" || task.NetworkConf != "" {
		return models.InstallMethodAgentDirect
	}
	
	// 场景 2: Ubuntu 使用 Agent 直接安装（debootstrap）
	if task.OSType == "ubuntu" || task.OSType == "debian" {
		return models.InstallMethodAgentDirect
	}
	
	// 场景 3: CentOS/Rocky 使用 Kickstart（更成熟）
	if task.OSType == "centos" || task.OSType == "rocky" {
		return models.InstallMethodKickstart
	}
	
	// 默认使用 Agent 直接安装（更灵活）
	return models.InstallMethodAgentDirect
}

// getOSInstallConfig returns OS installation configuration
func (rc *RegionalClient) getOSInstallConfig(c *gin.Context) {
	var req struct {
		SN string `json:"sn" binding:"required"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	taskKey := etcd.TaskKeyV3(rc.idc, req.SN)
	var task models.TaskV3
	if err := rc.etcdClient.GetJSON(taskKey, &task); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task not found"})
		return
	}

	// 决定安装方式
	installMethod := rc.determineInstallMethod(&task)

	// 构建安装配置
	config := &models.OSInstallConfig{
		Method:    installMethod,
		OSType:    task.OSType,
		OSVersion: task.OSVersion,
		MirrorURL: fmt.Sprintf("http://%s:8081", rc.serverIP),
		Network: models.NetworkConfig{
			Interface: "eth0",
			Method:    "static",
			IP:        task.IP,
			Netmask:   "255.255.255.0",
			Gateway:   rc.serverIP,
			DNS:       rc.serverIP,
			Hostname:  task.Hostname,
		},
	}

	// 根据安装方式配置不同的参数
	if installMethod == models.InstallMethodKickstart {
		// Kickstart 方式：提供 kickstart URL
		config.KickstartURL = fmt.Sprintf("http://%s:8081/api/v1/kickstart/%s", rc.serverIP, req.SN)
	} else {
		// Agent 直接安装方式：提供详细的安装参数
		config.DiskLayout = models.DiskLayoutConfig{
			RootDisk:       "/dev/sda",
			PartitionTable: "gpt",
			Partitions: []models.PartitionConfig{
				{MountPoint: "/boot", Size: "1G", FSType: "ext4"},
				{MountPoint: "swap", Size: "16G", FSType: "swap"},
				{MountPoint: "/", Size: "0", FSType: "ext4"}, // 0 表示使用剩余空间
			},
		}

		// 默认软件包
		config.Packages = []string{
			"openssh-server",
			"wget",
			"curl",
			"vim",
			"net-tools",
		}

		// 如果需要 post-install 脚本
		// config.PostScript = base64.StdEncoding.EncodeToString([]byte("#!/bin/bash\necho 'Post install complete'\n"))
	}

	log.Printf("[%s] getOSInstallConfig for %s: method=%s", rc.idc, req.SN, installMethod)
	c.JSON(http.StatusOK, config)
}

// generateKickstart generates kickstart file for a server
func (rc *RegionalClient) generateKickstart(c *gin.Context) {
	sn := c.Param("sn")

	taskKey := etcd.TaskKeyV3(rc.idc, sn)
	var task models.TaskV3
	if err := rc.etcdClient.GetJSON(taskKey, &task); err != nil {
		c.String(http.StatusNotFound, "Task not found for SN: %s", sn)
		return
	}

	// 构建安装配置
	config := &models.OSInstallConfig{
		Method:      models.InstallMethodKickstart,
		OSType:      task.OSType,
		OSVersion:   task.OSVersion,
		MirrorURL:   fmt.Sprintf("http://%s:8081/repos/%s/%s", rc.serverIP, task.OSType, task.OSVersion),
		RegionalURL: fmt.Sprintf("http://%s:8081", rc.serverIP),
		Network: models.NetworkConfig{
			Interface: "eth0",
			Method:    "static",
			IP:        task.IP,
			Netmask:   "255.255.255.0",
			Gateway:   rc.serverIP,
			DNS:       rc.serverIP,
			Hostname:  task.Hostname,
		},
		DiskLayout: models.DiskLayoutConfig{
			RootDisk:       "/dev/sda",
			PartitionTable: "gpt",
			Partitions: []models.PartitionConfig{
				{MountPoint: "/boot", Size: "1G", FSType: "ext4"},
				{MountPoint: "swap", Size: "16G", FSType: "swap"},
				{MountPoint: "/", Size: "0", FSType: "ext4"},
			},
		},
		RootPassword: "$6$rounds=656000$YourSaltHere$HashedPasswordHere", // 应该从配置读取
		Packages: []string{
			"wget",
			"curl",
			"vim",
			"net-tools",
		},
	}

	// 生成 kickstart 内容
	ksContent, err := rc.kickstartGenerator.Generate(&task, config)
	if err != nil {
		log.Printf("[%s] Failed to generate kickstart for %s: %v", rc.idc, sn, err)
		c.String(http.StatusInternalServerError, "Failed to generate kickstart: %v", err)
		return
	}

	log.Printf("[%s] Generated kickstart for %s", rc.idc, sn)
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.String(http.StatusOK, ksContent)
}

// generatePreseed generates preseed file for Ubuntu/Debian
func (rc *RegionalClient) generatePreseed(c *gin.Context) {
	sn := c.Param("sn")

	taskKey := etcd.TaskKeyV3(rc.idc, sn)
	var task models.TaskV3
	if err := rc.etcdClient.GetJSON(taskKey, &task); err != nil {
		c.String(http.StatusNotFound, "Task not found for SN: %s", sn)
		return
	}

	// 构建安装配置
	config := &models.OSInstallConfig{
		Method:    models.InstallMethodKickstart,
		OSType:    task.OSType,
		OSVersion: task.OSVersion,
		MirrorURL: fmt.Sprintf("http://%s:8081/repos/%s/%s", rc.serverIP, task.OSType, task.OSVersion),
		Network: models.NetworkConfig{
			Interface: "eth0",
			Method:    "static",
			IP:        task.IP,
			Netmask:   "255.255.255.0",
			Gateway:   rc.serverIP,
			DNS:       rc.serverIP,
			Hostname:  task.Hostname,
		},
		DiskLayout: models.DiskLayoutConfig{
			RootDisk:       "/dev/sda",
			PartitionTable: "gpt",
			Partitions: []models.PartitionConfig{
				{MountPoint: "/boot", Size: "1G", FSType: "ext4"},
				{MountPoint: "swap", Size: "16G", FSType: "swap"},
				{MountPoint: "/", Size: "0", FSType: "ext4"},
			},
		},
		RootPassword: "$6$rounds=656000$YourSaltHere$HashedPasswordHere",
	}

	// 生成 preseed 内容
	preseedContent, err := rc.kickstartGenerator.GeneratePreseed(&task, config)
	if err != nil {
		log.Printf("[%s] Failed to generate preseed for %s: %v", rc.idc, sn, err)
		c.String(http.StatusInternalServerError, "Failed to generate preseed: %v", err)
		return
	}

	log.Printf("[%s] Generated preseed for %s", rc.idc, sn)
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.String(http.StatusOK, preseedContent)
}

// installComplete handles installation completion notification
func (rc *RegionalClient) installComplete(c *gin.Context) {
	var req struct {
		SN      string `json:"sn" binding:"required"`
		Status  string `json:"status" binding:"required"`
		Message string `json:"message"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[%s] Installation complete notification from %s: status=%s, message=%s",
		rc.idc, req.SN, req.Status, req.Message)

	taskKey := etcd.TaskKeyV3(rc.idc, req.SN)
	err := rc.etcdClient.AtomicUpdate(taskKey, func(data []byte) (interface{}, error) {
		var task models.TaskV3
		if err := json.Unmarshal(data, &task); err != nil {
			return nil, err
		}

		// 更新任务状态
		if req.Status == "success" {
			task.Status = models.TaskStatusCompleted
			task.StatusHistory = append(task.StatusHistory, models.StatusChange{
				Status:    models.TaskStatusCompleted,
				Timestamp: time.Now(),
				Reason:    "OS installation completed successfully",
			})
		} else {
			task.Status = models.TaskStatusFailed
			task.StatusHistory = append(task.StatusHistory, models.StatusChange{
				Status:    models.TaskStatusFailed,
				Timestamp: time.Now(),
				Reason:    fmt.Sprintf("OS installation failed: %s", req.Message),
			})
		}

		task.Progress = append(task.Progress, models.ProgressStep{
			Step:      "os_install",
			Percent:   100,
			Timestamp: time.Now(),
			Message:   req.Message,
		})

		task.Logs = append(task.Logs, fmt.Sprintf("[INFO] Installation %s: %s", req.Status, req.Message))
		task.UpdatedAt = time.Now()

		return task, nil
	})

	if err != nil {
		log.Printf("[%s] Failed to update task status: %v", rc.idc, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update task"})
		return
	}

	// 清理 PXE 配置
	var task models.TaskV3
	if err := rc.etcdClient.GetJSON(taskKey, &task); err == nil {
		go rc.cleanupPXEBoot(&task)
	}

	c.JSON(http.StatusOK, gin.H{"message": "Installation status updated"})
}
