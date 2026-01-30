package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yourusername/lpmos-go/cmd/regional-client/tftp"
)

func main() {
	fmt.Println("=== TFTP Server Example ===\n")

	// 1. 创建 TFTP 根目录
	tftpRoot := "/tftpboot"
	fmt.Printf("--- Setting up TFTP Root Directory: %s ---\n", tftpRoot)

	// 2. 创建文件管理器并初始化目录结构
	fileManager := tftp.NewFileManager(tftpRoot)
	if err := fileManager.EnsureDirectories(); err != nil {
		log.Fatalf("Failed to create directories: %v", err)
	}
	fmt.Println("✓ Directory structure created:")
	fmt.Println("  /tftpboot/")
	fmt.Println("  /tftpboot/pxelinux.cfg/")
	fmt.Println("  /tftpboot/kernels/")
	fmt.Println("  /tftpboot/initrds/")

	// 3. 创建一些测试文件
	fmt.Println("\n--- Creating Test Files ---")

	// 创建测试文件 1
	testContent1 := []byte("This is a test file for TFTP server\n")
	if err := fileManager.WriteFile("test.txt", testContent1); err != nil {
		log.Printf("Failed to create test file: %v", err)
	} else {
		fmt.Println("✓ Created: /tftpboot/test.txt")
	}

	// 创建 PXE 配置文件示例
	pxeConfig := []byte(`DEFAULT linux
LABEL linux
  KERNEL /kernels/vmlinuz
  APPEND initrd=/initrds/initrd.img
`)
	if err := fileManager.WriteFile("pxelinux.cfg/default", pxeConfig); err != nil {
		log.Printf("Failed to create PXE config: %v", err)
	} else {
		fmt.Println("✓ Created: /tftpboot/pxelinux.cfg/default")
	}

	// 创建 README 文件
	readme := []byte(`TFTP Server Files
=================

This directory contains files served by the TFTP server:

- pxelinux.cfg/  : PXE boot configurations
- kernels/       : Kernel images
- initrds/       : Initrd images

For PXE boot, place your kernel and initrd files in the respective directories.
`)
	if err := fileManager.WriteFile("README.txt", readme); err != nil {
		log.Printf("Failed to create README: %v", err)
	} else {
		fmt.Println("✓ Created: /tftpboot/README.txt")
	}

	// 4. 创建 TFTP 服务器配置
	config := tftp.Config{
		RootDir:    tftpRoot,            // TFTP 根目录
		ListenAddr: ":69",               // 监听地址 (标准 TFTP 端口)
		MaxClients: 100,                 // 最大并发客户端数
		Timeout:    30 * time.Second,    // 传输超时时间
		BlockSize:  512,                 // 块大小 (标准 TFTP 块大小)
	}

	// 5. 创建 TFTP 服务器
	server, err := tftp.NewServer(config)
	if err != nil {
		log.Fatalf("Failed to create TFTP server: %v", err)
	}
	fmt.Println("\n✓ TFTP server created")

	// 6. 启动 TFTP 服务器
	fmt.Println("\n--- Starting TFTP Server ---")
	if err := server.Start(); err != nil {
		log.Fatalf("Failed to start TFTP server: %v", err)
	}
	fmt.Printf("✓ TFTP server started on %s\n", config.ListenAddr)
	fmt.Printf("  Root directory: %s\n", config.RootDir)
	fmt.Printf("  Max clients: %d\n", config.MaxClients)
	fmt.Printf("  Timeout: %v\n", config.Timeout)
	fmt.Printf("  Block size: %d bytes\n", config.BlockSize)

	// 7. 列出所有可用文件
	fmt.Println("\n--- Available Files ---")
	files, err := server.ListFiles()
	if err != nil {
		log.Printf("Failed to list files: %v", err)
	} else {
		fmt.Printf("Total files: %d\n", len(files))
		for _, file := range files {
			fmt.Printf("  %s (Size: %d bytes, Modified: %s)\n",
				file.Name, file.Size, file.ModTime.Format("2006-01-02 15:04:05"))
		}
	}

	// 8. 启动监控协程，定期输出统计信息
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			fmt.Println("\n--- TFTP Server Statistics ---")
			stats := server.GetStats()
			fmt.Printf("Total requests: %d\n", stats.TotalRequests)
			fmt.Printf("  Success: %d\n", stats.SuccessRequests)
			fmt.Printf("  Failed: %d\n", stats.FailedRequests)
			fmt.Printf("Total bytes served: %d (%.2f MB)\n",
				stats.TotalBytesServed,
				float64(stats.TotalBytesServed)/(1024*1024))

			if stats.TotalRequests > 0 {
				successRate := float64(stats.SuccessRequests) / float64(stats.TotalRequests) * 100
				fmt.Printf("Success rate: %.2f%%\n", successRate)
			}
		}
	}()

	// 9. 测试提示
	fmt.Println("\n--- TFTP Server Running ---")
	fmt.Println("You can test the server with:")
	fmt.Printf("  tftp -v localhost -c get test.txt\n")
	fmt.Printf("  curl -v tftp://localhost/test.txt\n")
	fmt.Println("\nPress Ctrl+C to stop...")

	// 10. 等待中断信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	// 11. 停止服务器
	fmt.Println("\n--- Stopping TFTP Server ---")
	if err := server.Stop(); err != nil {
		log.Printf("Error stopping server: %v", err)
	}

	// 显示最终统计
	stats := server.GetStats()
	fmt.Println("\n--- Final Statistics ---")
	fmt.Printf("Total requests: %d\n", stats.TotalRequests)
	fmt.Printf("  Success: %d\n", stats.SuccessRequests)
	fmt.Printf("  Failed: %d\n", stats.FailedRequests)
	fmt.Printf("Total bytes served: %d bytes\n", stats.TotalBytesServed)

	fmt.Println("\n✓ TFTP server stopped")
}
