package main

import (
	"fmt"
	"log"
	"net"

	"github.com/lpmos/lpmos-go/cmd/regional-client/pxe"
)

func main() {
	fmt.Println("=== PXE Configuration Generator Example ===\n")

	// 1. Create PXE generator
	generator, err := pxe.NewGenerator(pxe.Config{
		TFTPRoot: "/tftpboot",
	})
	if err != nil {
		log.Fatalf("Failed to create PXE generator: %v", err)
	}
	fmt.Println("✓ PXE generator created")

	// 2. Generate default configuration
	if err := generator.GenerateDefaultConfig(); err != nil {
		log.Fatalf("Failed to generate default config: %v", err)
	}
	fmt.Println("✓ Default configuration generated: /tftpboot/pxelinux.cfg/default")

	// 3. Generate Ubuntu installation configuration
	fmt.Println("\n--- Generating Ubuntu 22.04 Configuration ---")
	mac1, _ := net.ParseMAC("00:1a:2b:3c:4d:5e")
	ubuntuConfig := &pxe.BootConfig{
		MAC:          mac1,
		IP:           net.ParseIP("192.168.100.10"),
		Hostname:     "ubuntu-server-01",
		OSType:       "ubuntu",
		OSVersion:    "22.04",
		KernelPath:   "/kernels/ubuntu-22.04-vmlinuz",
		InitrdPath:   "/initrds/ubuntu-22.04-initrd.img",
		RegionalURL:  "http://192.168.100.1:8080",
		SerialNumber: "SN-UBUNTU-001",
		DataCenter:   "dc1",
		CustomParams: map[string]string{
			"debug": "true",
		},
	}

	if err := generator.GenerateConfig(ubuntuConfig); err != nil {
		log.Fatalf("Failed to generate Ubuntu config: %v", err)
	}
	fmt.Printf("✓ Ubuntu configuration generated for MAC: %s\n", mac1)
	fmt.Printf("  File: /tftpboot/pxelinux.cfg/01-%s\n", mac1.String())

	// 4. Generate CentOS installation configuration
	fmt.Println("\n--- Generating CentOS 7 Configuration ---")
	mac2, _ := net.ParseMAC("00:aa:bb:cc:dd:ee")
	centosConfig := &pxe.BootConfig{
		MAC:          mac2,
		IP:           net.ParseIP("192.168.100.20"),
		Hostname:     "centos-server-02",
		OSType:       "centos",
		OSVersion:    "7.9",
		KernelPath:   "/kernels/centos-7.9-vmlinuz",
		InitrdPath:   "/initrds/centos-7.9-initrd.img",
		RegionalURL:  "http://192.168.100.1:8080",
		SerialNumber: "SN-CENTOS-002",
		DataCenter:   "dc2",
	}

	if err := generator.GenerateConfig(centosConfig); err != nil {
		log.Fatalf("Failed to generate CentOS config: %v", err)
	}
	fmt.Printf("✓ CentOS configuration generated for MAC: %s\n", mac2)
	fmt.Printf("  File: /tftpboot/pxelinux.cfg/01-%s\n", mac2.String())

	// 5. Generate Rocky Linux installation configuration
	fmt.Println("\n--- Generating Rocky Linux 8 Configuration ---")
	mac3, _ := net.ParseMAC("00:11:22:33:44:55")
	rockyConfig := &pxe.BootConfig{
		MAC:          mac3,
		IP:           net.ParseIP("192.168.100.30"),
		Hostname:     "rocky-server-03",
		OSType:       "rocky",
		OSVersion:    "8.6",
		KernelPath:   "/kernels/rocky-8.6-vmlinuz",
		InitrdPath:   "/initrds/rocky-8.6-initrd.img",
		RegionalURL:  "http://192.168.100.1:8080",
		SerialNumber: "SN-ROCKY-003",
		DataCenter:   "dc3",
	}

	if err := generator.GenerateConfig(rockyConfig); err != nil {
		log.Fatalf("Failed to generate Rocky config: %v", err)
	}
	fmt.Printf("✓ Rocky Linux configuration generated for MAC: %s\n", mac3)
	fmt.Printf("  File: /tftpboot/pxelinux.cfg/01-%s\n", mac3.String())

	// 6. List all configurations
	fmt.Println("\n--- Listing All Configurations ---")
	configs, err := generator.ListConfigs()
	if err != nil {
		log.Fatalf("Failed to list configs: %v", err)
	}
	fmt.Printf("Total configurations: %d\n", len(configs))
	for i, config := range configs {
		fmt.Printf("  %d. %s\n", i+1, config)
	}

	// 7. Check if configuration exists
	fmt.Println("\n--- Checking Configuration Existence ---")
	if generator.ConfigExists(mac1) {
		fmt.Printf("✓ Configuration exists for MAC: %s\n", mac1)
	}

	// 8. Remove a configuration
	fmt.Println("\n--- Removing Configuration ---")
	if err := generator.RemoveConfig(mac3); err != nil {
		log.Fatalf("Failed to remove config: %v", err)
	}
	fmt.Printf("✓ Configuration removed for MAC: %s\n", mac3)

	// 9. List templates
	fmt.Println("\n--- Available Templates ---")
	templates := pxe.TemplateList()
	for i, tmpl := range templates {
		fmt.Printf("  %d. %s\n", i+1, tmpl)
	}

	fmt.Println("\n=== PXE Configuration Generator Example Complete ===")
}
