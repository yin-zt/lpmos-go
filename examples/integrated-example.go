package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lpmos/lpmos-go/cmd/regional-client/dhcp"
	"github.com/lpmos/lpmos-go/cmd/regional-client/pxe"
	"github.com/lpmos/lpmos-go/cmd/regional-client/tftp"
)

func main() {
	fmt.Println("=== DHCP + TFTP + PXE Integrated Example ===\n")

	// ========== 第 1 步: 设置 TFTP 服务器 ==========
	fmt.Println("--- Step 1: Setting up TFTP Server ---")

	tftpRoot := "/tftpboot"
	fileManager := tftp.NewFileManager(tftpRoot)
	if err := fileManager.EnsureDirectories(); err != nil {
		log.Fatalf("Failed to create directories: %v", err)
	}
	fmt.Println("✓ TFTP directories created")

	// 创建 TFTP 服务器
	tftpConfig := tftp.Config{
		RootDir:    tftpRoot,
		ListenAddr: ":69",
		MaxClients: 100,
		Timeout:    30 * time.Second,
		BlockSize:  512,
	}

	tftpServer, err := tftp.NewServer(tftpConfig)
	if err != nil {
		log.Fatalf("Failed to create TFTP server: %v", err)
	}

	if err := tftpServer.Start(); err != nil {
		log.Fatalf("Failed to start TFTP server: %v", err)
	}
	fmt.Println("✓ TFTP server started on :69")

	// ========== 第 2 步: 设置 PXE 配置生成器 ==========
	fmt.Println("\n--- Step 2: Setting up PXE Generator ---")

	pxeGenerator, err := pxe.NewGenerator(pxe.Config{
		TFTPRoot: tftpRoot,
	})
	if err != nil {
		log.Fatalf("Failed to create PXE generator: %v", err)
	}

	// 生成默认 PXE 配置
	if err := pxeGenerator.GenerateDefaultConfig(); err != nil {
		log.Fatalf("Failed to generate default PXE config: %v", err)
	}
	fmt.Println("✓ PXE generator created")
	fmt.Println("✓ Default PXE configuration generated")

	// ========== 第 3 步: 设置 DHCP 服务器 ==========
	fmt.Println("\n--- Step 3: Setting up DHCP Server ---")

	dhcpConfig := dhcp.Config{
		Interface:  "eth1",
		ServerIP:   "192.168.100.1",
		Gateway:    "192.168.100.1",
		DNSServers: []string{"192.168.100.1", "8.8.8.8"},
		TFTPServer: "192.168.100.1",
		BootFile:   "pxelinux.0",
		LeaseTime:  3600 * time.Second,
		StartIP:    "192.168.100.10",
		EndIP:      "192.168.100.200",
		Netmask:    "255.255.255.0",
	}

	dhcpServer, err := dhcp.NewServer(dhcpConfig)
	if err != nil {
		log.Fatalf("Failed to create DHCP server: %v", err)
	}

	if err := dhcpServer.Start(); err != nil {
		log.Fatalf("Failed to start DHCP server: %v", err)
	}
	fmt.Println("✓ DHCP server started on :67")

	// ========== 第 4 步: 配置服务器 PXE 启动 ==========
	fmt.Println("\n--- Step 4: Configuring Servers for PXE Boot ---")

	// 服务器 1: Ubuntu 22.04 安装
	server1MAC, _ := net.ParseMAC("00:1a:2b:3c:4d:5e")
	server1IP := "192.168.100.10"

	// 添加 DHCP 静态绑定
	dhcpServer.AddStaticBinding(
		server1MAC.String(),
		server1IP,
		"ubuntu-server-01",
		"pxelinux.0",
	)
	fmt.Printf("✓ DHCP binding: %s -> %s\n", server1MAC, server1IP)

	// 生成 PXE 配置
	pxeGenerator.GenerateConfig(&pxe.BootConfig{
		MAC:          server1MAC,
		IP:           net.ParseIP(server1IP),
		Hostname:     "ubuntu-server-01",
		OSType:       "ubuntu",
		OSVersion:    "22.04",
		KernelPath:   "/kernels/ubuntu-22.04-vmlinuz",
		InitrdPath:   "/initrds/ubuntu-22.04-initrd.img",
		RegionalURL:  "http://192.168.100.1:8080",
		SerialNumber: "SN-UBUNTU-001",
		DataCenter:   "dc1",
	})
	fmt.Printf("✓ PXE config: /tftpboot/pxelinux.cfg/01-%s\n", server1MAC.String())

	// 服务器 2: CentOS 7.9 安装
	server2MAC, _ := net.ParseMAC("00:aa:bb:cc:dd:ee")
	server2IP := "192.168.100.20"

	dhcpServer.AddStaticBinding(
		server2MAC.String(),
		server2IP,
		"centos-server-02",
		"pxelinux.0",
	)
	fmt.Printf("✓ DHCP binding: %s -> %s\n", server2MAC, server2IP)

	pxeGenerator.GenerateConfig(&pxe.BootConfig{
		MAC:          server2MAC,
		IP:           net.ParseIP(server2IP),
		Hostname:     "centos-server-02",
		OSType:       "centos",
		OSVersion:    "7.9",
		KernelPath:   "/kernels/centos-7.9-vmlinuz",
		InitrdPath:   "/initrds/centos-7.9-initrd.img",
		RegionalURL:  "http://192.168.100.1:8080",
		SerialNumber: "SN-CENTOS-002",
		DataCenter:   "dc2",
	})
	fmt.Printf("✓ PXE config: /tftpboot/pxelinux.cfg/01-%s\n", server2MAC.String())

	// 服务器 3: Rocky Linux 8 安装
	server3MAC, _ := net.ParseMAC("00:11:22:33:44:55")
	server3IP := "192.168.100.30"

	dhcpServer.AddStaticBinding(
		server3MAC.String(),
		server3IP,
		"rocky-server-03",
		"pxelinux.0",
	)
	fmt.Printf("✓ DHCP binding: %s -> %s\n", server3MAC, server3IP)

	pxeGenerator.GenerateConfig(&pxe.BootConfig{
		MAC:          server3MAC,
		IP:           net.ParseIP(server3IP),
		Hostname:     "rocky-server-03",
		OSType:       "rocky",
		OSVersion:    "8.6",
		KernelPath:   "/kernels/rocky-8.6-vmlinuz",
		InitrdPath:   "/initrds/rocky-8.6-initrd.img",
		RegionalURL:  "http://192.168.100.1:8080",
		SerialNumber: "SN-ROCKY-003",
		DataCenter:   "dc3",
	})
	fmt.Printf("✓ PXE config: /tftpboot/pxelinux.cfg/01-%s\n", server3MAC.String())

	// ========== 第 5 步: 显示配置摘要 ==========
	fmt.Println("\n=== Configuration Summary ===")
	fmt.Println("\nDHCP Server:")
	fmt.Printf("  Interface: %s\n", dhcpConfig.Interface)
	fmt.Printf("  Server IP: %s\n", dhcpConfig.ServerIP)
	fmt.Printf("  IP Pool: %s - %s\n", dhcpConfig.StartIP, dhcpConfig.EndIP)
	fmt.Printf("  Gateway: %s\n", dhcpConfig.Gateway)
	fmt.Printf("  DNS: %v\n", dhcpConfig.DNSServers)

	fmt.Println("\nTFTP Server:")
	fmt.Printf("  Root: %s\n", tftpConfig.RootDir)
	fmt.Printf("  Listen: %s\n", tftpConfig.ListenAddr)

	fmt.Println("\nStatic Bindings:")
	bindings := dhcpServer.GetStaticBindings()
	for mac, binding := range bindings {
		fmt.Printf("  %s -> %s (%s)\n", mac, binding.IP, binding.Hostname)
	}

	fmt.Println("\nPXE Configurations:")
	configs, _ := pxeGenerator.ListConfigs()
	for _, cfg := range configs {
		fmt.Printf("  %s\n", cfg)
	}

	// ========== 第 6 步: 启动监控 ==========
	go monitorServers(dhcpServer, tftpServer)

	// ========== 第 7 步: 等待中断信号 ==========
	fmt.Println("\n=== Servers Running ===")
	fmt.Println("PXE boot environment is ready!")
	fmt.Println("\nNext steps:")
	fmt.Println("  1. Place pxelinux.0 and other boot files in /tftpboot/")
	fmt.Println("  2. Place kernel images in /tftpboot/kernels/")
	fmt.Println("  3. Place initrd images in /tftpboot/initrds/")
	fmt.Println("  4. Power on or reboot servers with PXE boot enabled")
	fmt.Println("\nPress Ctrl+C to stop all servers...")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	// ========== 第 8 步: 清理和停止 ==========
	fmt.Println("\n\n=== Shutting Down ===")

	fmt.Println("Stopping DHCP server...")
	dhcpServer.Stop()

	fmt.Println("Stopping TFTP server...")
	tftpServer.Stop()

	// 显示最终统计
	fmt.Println("\n--- Final Statistics ---")
	stats := tftpServer.GetStats()
	fmt.Printf("TFTP - Total requests: %d (Success: %d, Failed: %d)\n",
		stats.TotalRequests, stats.SuccessRequests, stats.FailedRequests)
	fmt.Printf("TFTP - Total bytes served: %d bytes\n", stats.TotalBytesServed)

	leases := dhcpServer.GetLeases()
	fmt.Printf("DHCP - Active leases: %d\n", len(leases))

	fmt.Println("\n✓ All servers stopped")
}

// monitorServers 监控服务器状态
func monitorServers(dhcpServer *dhcp.Server, tftpServer *tftp.Server) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		fmt.Println("\n=== Status Report ===")

		// DHCP 状态
		leases := dhcpServer.GetLeases()
		bindings := dhcpServer.GetStaticBindings()
		fmt.Printf("DHCP - Active leases: %d, Static bindings: %d\n",
			len(leases), len(bindings))

		if len(leases) > 0 {
			fmt.Println("  Active leases:")
			for _, lease := range leases {
				remaining := time.Until(lease.ExpireTime)
				fmt.Printf("    %s -> %s (Expires in: %s)\n",
					lease.MAC, lease.IP, remaining.Round(time.Minute))
			}
		}

		// TFTP 状态
		stats := tftpServer.GetStats()
		fmt.Printf("TFTP - Total: %d, Success: %d, Failed: %d, Bytes: %d\n",
			stats.TotalRequests,
			stats.SuccessRequests,
			stats.FailedRequests,
			stats.TotalBytesServed)
	}
}
