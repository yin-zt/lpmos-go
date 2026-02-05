package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lpmos/lpmos-go/cmd/regional-client/dhcp"
)

func main() {
	fmt.Println("=== DHCP Server Example ===\n")

	// 1. 创建 DHCP 服务器配置
	config := dhcp.Config{
		Interface:  "eth1",                               // 网卡接口名称
		ServerIP:   "192.168.100.1",                      // DHCP 服务器 IP
		Gateway:    "192.168.100.1",                      // 网关地址
		DNSServers: []string{"192.168.100.1", "8.8.8.8"}, // DNS 服务器列表
		TFTPServer: "192.168.100.1",                      // TFTP 服务器地址
		BootFile:   "pxelinux.0",                         // PXE 启动文件
		LeaseTime:  3600 * time.Second,                   // 租约时间: 1 小时
		StartIP:    "192.168.100.10",                     // IP 池起始地址
		EndIP:      "192.168.100.200",                    // IP 池结束地址
		Netmask:    "255.255.255.0",                      // 子网掩码
	}

	// 2. 创建 DHCP 服务器
	server, err := dhcp.NewServer(config)
	if err != nil {
		log.Fatalf("Failed to create DHCP server: %v", err)
	}
	fmt.Println("✓ DHCP server created")

	// 3. 添加静态 MAC-IP 绑定
	fmt.Println("\n--- Adding Static Bindings ---")

	// 绑定 1: Ubuntu 服务器
	err = server.AddStaticBinding(
		"00:1a:2b:3c:4d:5e", // MAC 地址
		"192.168.100.10",    // 固定 IP
		"ubuntu-server-01",  // 主机名
		"pxelinux.0",        // 启动文件
	)
	if err != nil {
		log.Printf("Failed to add binding: %v", err)
	}
	fmt.Println("✓ Added binding: 00:1a:2b:3c:4d:5e -> 192.168.100.10")

	// 绑定 2: CentOS 服务器 (自定义启动文件)
	err = server.AddStaticBinding(
		"00:aa:bb:cc:dd:ee",
		"192.168.100.20",
		"centos-server-02",
		"pxelinux.0",
	)
	if err != nil {
		log.Printf("Failed to add binding: %v", err)
	}
	fmt.Println("✓ Added binding: 00:aa:bb:cc:dd:ee -> 192.168.100.20")

	// 绑定 3: 测试服务器
	err = server.AddStaticBinding(
		"00:11:22:33:44:55",
		"192.168.100.30",
		"test-server-03",
		"pxelinux.0",
	)
	if err != nil {
		log.Printf("Failed to add binding: %v", err)
	}
	fmt.Println("✓ Added binding: 00:11:22:33:44:55 -> 192.168.100.30")

	// 4. 启动 DHCP 服务器
	fmt.Println("\n--- Starting DHCP Server ---")
	if err := server.Start(); err != nil {
		log.Fatalf("Failed to start DHCP server: %v", err)
	}
	fmt.Println("✓ DHCP server started on port 67")
	fmt.Printf("  IP Pool: %s - %s\n", config.StartIP, config.EndIP)
	fmt.Printf("  Gateway: %s\n", config.Gateway)
	fmt.Printf("  DNS: %v\n", config.DNSServers)
	fmt.Printf("  TFTP Server: %s\n", config.TFTPServer)
	fmt.Printf("  Boot File: %s\n", config.BootFile)

	// 5. 启动监控协程，定期输出状态
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			fmt.Println("\n--- DHCP Server Status ---")

			// 获取所有租约
			leases := server.GetLeases()
			fmt.Printf("Active leases: %d\n", len(leases))
			for _, lease := range leases {
				fmt.Printf("  MAC: %s -> IP: %s (Hostname: %s, Expires: %s)\n",
					lease.MAC, lease.IP, lease.Hostname, lease.ExpireTime.Format("15:04:05"))
			}

			// 获取静态绑定
			bindings := server.GetStaticBindings()
			fmt.Printf("Static bindings: %d\n", len(bindings))
			for mac, binding := range bindings {
				fmt.Printf("  %s -> %s (Hostname: %s, Boot: %s)\n",
					mac, binding.IP, binding.Hostname, binding.BootFile)
			}
		}
	}()

	// 6. 等待中断信号
	fmt.Println("\n--- DHCP Server Running ---")
	fmt.Println("Press Ctrl+C to stop...")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	// 7. 停止服务器
	fmt.Println("\n--- Stopping DHCP Server ---")
	if err := server.Stop(); err != nil {
		log.Printf("Error stopping server: %v", err)
	}
	fmt.Println("✓ DHCP server stopped")
}
