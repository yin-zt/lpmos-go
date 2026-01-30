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
	dhcpServer   *dhcp.Server
	tftpServer   *tftp.Server
	pxeGenerator *pxe.Generator

	// Configuration
	serverIP     string
	networkIface string
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
	}

	if idc == "" {
		log.Fatal("--idc flag is required")
	}

	log.Printf("Starting LPMOS Regional Client v3.0 for IDC: %s", idc)
	log.Printf("Configuration: API Port=%s, Server IP=%s, Interface=%s", apiPort, serverIP, networkIface)

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
		idc:          idc,
		etcdClient:   etcdClient,
		ctx:          ctx,
		cancel:       cancel,
		leases:       make(map[string]clientv3.LeaseID),
		serverIP:     serverIP,
		networkIface: networkIface,
	}

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
			device.POST("/operationComplete", rc.operationComplete)
		}

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

	return router
}

// initTFTP initializes and starts the TFTP server
func (rc *RegionalClient) initTFTP() error {
	tftpRoot := "/tftpboot"

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
		TFTPRoot: "/tftpboot",
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
		LeaseTime:  3600 * time.Second, // 1 hour
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
			KernelPath:   fmt.Sprintf("/kernels/%s-%s-vmlinuz", task.OSType, task.OSVersion),
			InitrdPath:   fmt.Sprintf("/initrds/%s-%s-initrd.img", task.OSType, task.OSVersion),
			RegionalURL:  fmt.Sprintf("http://%s:8081", rc.serverIP),
			SerialNumber: task.SN,
			DataCenter:   rc.idc,
		}

		if err := rc.pxeGenerator.GenerateConfig(bootConfig); err != nil {
			log.Printf("[%s] ERROR: Failed to generate PXE config for %s: %v", rc.idc, task.SN, err)
			return
		}
		log.Printf("[%s] ✓ PXE configuration generated: /tftpboot/pxelinux.cfg/01-%s",
			rc.idc, strings.ReplaceAll(strings.ToLower(task.MAC), ":", "-"))
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

		t.Logs = append(t.Logs, fmt.Sprintf("[INFO] PXE boot environment configured"))
		t.Logs = append(t.Logs, fmt.Sprintf("[INFO] DHCP binding: %s -> %s", task.MAC, task.IP))
		t.Logs = append(t.Logs, fmt.Sprintf("[INFO] PXE config: 01-%s", strings.ReplaceAll(strings.ToLower(task.MAC), ":", "-")))
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
					if task.Status == models.TaskStatusApproved {
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
		// Store as unmatched report
		unmatchedKey := fmt.Sprintf("/os/unmatched_reports/%s/%d-%s", rc.idc, time.Now().Unix(), req.MAC)
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
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Check if task exists and is approved
	taskKey := etcd.TaskKeyV3(rc.idc, req.SN)
	var task models.TaskV3
	if err := rc.etcdClient.GetJSON(taskKey, &task); err != nil {
		// No task found - not in queue yet
		c.JSON(http.StatusOK, gin.H{"result": false})
		return
	}

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
		if len(task.Progress) > 0 {
			lastProgress = task.Progress[len(task.Progress)-1].Percent
		}

		if lastProgress < 40 {
			operation = "hardware_config"
		} else if lastProgress < 50 {
			operation = "network_config"
		} else if lastProgress < 100 {
			operation = "os_install"
			data = gin.H{
				"os_type":    task.OSType,
				"os_version": task.OSVersion,
			}
		} else {
			operation = "reboot"
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
