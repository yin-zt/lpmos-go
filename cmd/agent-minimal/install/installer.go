package install

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Config represents OS installation configuration
type Config struct {
	Method       string           `json:"install_method"` // kickstart or agent_direct
	OSType       string           `json:"os_type"`
	OSVersion    string           `json:"os_version"`
	MirrorURL    string           `json:"mirror_url"`
	KickstartURL string           `json:"kickstart_url,omitempty"`
	DiskLayout   DiskLayoutConfig `json:"disk_layout"`
	Network      NetworkConfig    `json:"network"`
	Packages     []string         `json:"packages"`
	PostScript   string           `json:"post_install_script,omitempty"` // Base64 encoded
	RootPassword string           `json:"root_password,omitempty"`       // Encrypted
}

// DiskLayoutConfig represents disk partition layout
type DiskLayoutConfig struct {
	RootDisk       string            `json:"root_disk"`       // /dev/sda
	PartitionTable string            `json:"partition_table"` // gpt or msdos
	Partitions     []PartitionConfig `json:"partitions"`
}

// PartitionConfig represents a single partition
type PartitionConfig struct {
	MountPoint string `json:"mount_point"` // /, /boot, /home, swap
	Size       string `json:"size"`        // 10G, 100G, 0 (remaining)
	FSType     string `json:"fstype"`      // ext4, xfs, swap
}

// NetworkConfig represents network configuration
type NetworkConfig struct {
	Interface string `json:"interface"` // eth0, ens33
	Method    string `json:"method"`    // static or dhcp
	IP        string `json:"ip,omitempty"`
	Netmask   string `json:"netmask,omitempty"`
	Gateway   string `json:"gateway,omitempty"`
	DNS       string `json:"dns,omitempty"`
	Hostname  string `json:"hostname"`
}

// Installer handles OS installation
type Installer struct {
	config    *Config
	mountRoot string
}

// NewInstaller creates a new OS installer
func NewInstaller(config *Config) *Installer {
	return &Installer{
		config:    config,
		mountRoot: "/mnt",
	}
}

// Install performs the OS installation
func (i *Installer) Install() error {
	log.Printf("Starting OS installation: %s %s", i.config.OSType, i.config.OSVersion)

	// Step 1: Partition disks
	if err := i.partitionDisks(); err != nil {
		return fmt.Errorf("disk partitioning failed: %w", err)
	}

	// Step 2: Format partitions
	if err := i.formatPartitions(); err != nil {
		return fmt.Errorf("partition formatting failed: %w", err)
	}

	// Step 3: Mount filesystems
	if err := i.mountFilesystems(); err != nil {
		return fmt.Errorf("filesystem mount failed: %w", err)
	}

	// Step 4: Install base system
	if err := i.installBaseSystem(); err != nil {
		return fmt.Errorf("base system installation failed: %w", err)
	}

	// Step 5: Configure system
	if err := i.configureSystem(); err != nil {
		return fmt.Errorf("system configuration failed: %w", err)
	}

	// Step 6: Install bootloader
	if err := i.installBootloader(); err != nil {
		return fmt.Errorf("bootloader installation failed: %w", err)
	}

	// Step 7: Execute post-install script (if any)
	if i.config.PostScript != "" {
		if err := i.executePostScript(); err != nil {
			log.Printf("Warning: Post-install script failed: %v", err)
		}
	}

	// Step 8: Unmount filesystems
	if err := i.unmountFilesystems(); err != nil {
		log.Printf("Warning: Failed to unmount filesystems: %v", err)
	}

	log.Println("OS installation completed successfully")
	return nil
}

// partitionDisks creates disk partitions
func (i *Installer) partitionDisks() error {
	log.Printf("Partitioning disk: %s", i.config.DiskLayout.RootDisk)

	disk := i.config.DiskLayout.RootDisk

	// Wipe existing partition table
	log.Println("  Wiping existing partition table...")
	cmd := exec.Command("sgdisk", "-Z", disk)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to wipe partition table: %w\n%s", err, string(output))
	}

	// Create new partition table
	partTable := i.config.DiskLayout.PartitionTable
	if partTable == "" {
		partTable = "gpt" // Default to GPT
	}

	if partTable == "gpt" {
		// GPT partition table
		partNum := 1
		for _, part := range i.config.DiskLayout.Partitions {
			log.Printf("  Creating partition %d: %s (%s, %s)", partNum, part.MountPoint, part.Size, part.FSType)

			var size string
			if part.Size == "0" || part.Size == "" {
				size = "0" // Use remaining space
			} else {
				size = "+" + part.Size
			}

			cmd := exec.Command("sgdisk", "-n", fmt.Sprintf("%d:0:%s", partNum, size), disk)
			if output, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("failed to create partition %d: %w\n%s", partNum, err, string(output))
			}

			partNum++
		}
	} else {
		// MBR partition table using fdisk
		return fmt.Errorf("MBR partition table not yet implemented")
	}

	// Inform kernel of partition changes
	cmd = exec.Command("partprobe", disk)
	cmd.Run()

	log.Println("  Disk partitioning completed")
	return nil
}

// formatPartitions formats all partitions
func (i *Installer) formatPartitions() error {
	log.Println("Formatting partitions...")

	disk := i.config.DiskLayout.RootDisk
	partNum := 1

	for _, part := range i.config.DiskLayout.Partitions {
		partDevice := fmt.Sprintf("%s%d", disk, partNum)
		// Handle nvme devices (e.g., /dev/nvme0n1p1)
		if strings.Contains(disk, "nvme") {
			partDevice = fmt.Sprintf("%sp%d", disk, partNum)
		}

		log.Printf("  Formatting %s as %s", partDevice, part.FSType)

		var cmd *exec.Cmd
		switch part.FSType {
		case "ext4":
			cmd = exec.Command("mkfs.ext4", "-F", partDevice)
		case "xfs":
			cmd = exec.Command("mkfs.xfs", "-f", partDevice)
		case "swap":
			cmd = exec.Command("mkswap", partDevice)
		default:
			log.Printf("  Warning: Unknown filesystem type %s, skipping", part.FSType)
			partNum++
			continue
		}

		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to format %s: %w\n%s", partDevice, err, string(output))
		}

		partNum++
	}

	log.Println("  Partition formatting completed")
	return nil
}

// mountFilesystems mounts all partitions to /mnt
func (i *Installer) mountFilesystems() error {
	log.Println("Mounting filesystems...")

	disk := i.config.DiskLayout.RootDisk

	// First, mount root filesystem
	var rootPart string
	partNum := 1
	for _, part := range i.config.DiskLayout.Partitions {
		if part.MountPoint == "/" {
			rootPart = fmt.Sprintf("%s%d", disk, partNum)
			if strings.Contains(disk, "nvme") {
				rootPart = fmt.Sprintf("%sp%d", disk, partNum)
			}
			break
		}
		partNum++
	}

	if rootPart == "" {
		return fmt.Errorf("no root partition found")
	}

	log.Printf("  Mounting root partition %s to %s", rootPart, i.mountRoot)
	if err := os.MkdirAll(i.mountRoot, 0755); err != nil {
		return fmt.Errorf("failed to create mount point: %w", err)
	}

	cmd := exec.Command("mount", rootPart, i.mountRoot)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to mount root: %w\n%s", err, string(output))
	}

	// Mount other filesystems
	partNum = 1
	for _, part := range i.config.DiskLayout.Partitions {
		if part.MountPoint == "/" || part.MountPoint == "swap" {
			partNum++
			continue
		}

		partDevice := fmt.Sprintf("%s%d", disk, partNum)
		if strings.Contains(disk, "nvme") {
			partDevice = fmt.Sprintf("%sp%d", disk, partNum)
		}

		mountPoint := filepath.Join(i.mountRoot, part.MountPoint)
		log.Printf("  Mounting %s to %s", partDevice, mountPoint)

		if err := os.MkdirAll(mountPoint, 0755); err != nil {
			return fmt.Errorf("failed to create mount point %s: %w", mountPoint, err)
		}

		cmd := exec.Command("mount", partDevice, mountPoint)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to mount %s: %w\n%s", partDevice, err, string(output))
		}

		partNum++
	}

	// Activate swap if present
	partNum = 1
	for _, part := range i.config.DiskLayout.Partitions {
		if part.MountPoint == "swap" {
			partDevice := fmt.Sprintf("%s%d", disk, partNum)
			if strings.Contains(disk, "nvme") {
				partDevice = fmt.Sprintf("%sp%d", disk, partNum)
			}

			log.Printf("  Activating swap on %s", partDevice)
			cmd := exec.Command("swapon", partDevice)
			if output, err := cmd.CombinedOutput(); err != nil {
				log.Printf("  Warning: Failed to activate swap: %v\n%s", err, string(output))
			}
		}
		partNum++
	}

	log.Println("  Filesystem mounting completed")
	return nil
}

// installBaseSystem installs the base OS
func (i *Installer) installBaseSystem() error {
	log.Println("Installing base system...")

	switch strings.ToLower(i.config.OSType) {
	case "ubuntu", "debian":
		return i.installDebian()
	case "centos", "rocky", "rhel":
		return i.installRHEL()
	default:
		return fmt.Errorf("unsupported OS type: %s", i.config.OSType)
	}
}

// installDebian installs Debian/Ubuntu using debootstrap
func (i *Installer) installDebian() error {
	log.Printf("Installing %s %s using debootstrap...", i.config.OSType, i.config.OSVersion)

	// Check if debootstrap is available
	if _, err := exec.LookPath("debootstrap"); err != nil {
		return fmt.Errorf("debootstrap not found: %w", err)
	}

	// Map version to codename
	codename := i.getDebianCodename()

	// Build mirror URL
	mirrorURL := i.config.MirrorURL
	if mirrorURL == "" {
		if i.config.OSType == "ubuntu" {
			mirrorURL = "http://archive.ubuntu.com/ubuntu"
		} else {
			mirrorURL = "http://deb.debian.org/debian"
		}
	}

	// Run debootstrap
	log.Printf("  Running debootstrap %s from %s...", codename, mirrorURL)
	cmd := exec.Command("debootstrap", "--arch=amd64", codename, i.mountRoot, mirrorURL)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("debootstrap failed: %w", err)
	}

	log.Println("  Base system installation completed")
	return nil
}

// installRHEL installs RHEL/CentOS/Rocky using dnf/yum
func (i *Installer) installRHEL() error {
	log.Printf("Installing %s %s using dnf/yum...", i.config.OSType, i.config.OSVersion)

	// Determine package manager
	pkgMgr := "dnf"
	if _, err := exec.LookPath("dnf"); err != nil {
		pkgMgr = "yum"
	}

	// Build release URL
	releaseURL := i.config.MirrorURL
	if releaseURL == "" {
		releaseURL = fmt.Sprintf("http://mirror.centos.org/centos/%s/BaseOS/x86_64/os/", i.config.OSVersion)
	}

	// Install base packages using installroot
	log.Printf("  Installing base packages using %s...", pkgMgr)

	packages := []string{"@core", "kernel", "grub2", "grub2-tools"}
	args := []string{
		"--installroot=" + i.mountRoot,
		"--releasever=" + i.config.OSVersion,
		"-y",
		"install",
	}
	args = append(args, packages...)

	cmd := exec.Command(pkgMgr, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), fmt.Sprintf("http_proxy=%s", releaseURL))

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s install failed: %w", pkgMgr, err)
	}

	log.Println("  Base system installation completed")
	return nil
}

// getDebianCodename maps version to codename
func (i *Installer) getDebianCodename() string {
	if i.config.OSType == "ubuntu" {
		switch i.config.OSVersion {
		case "20.04":
			return "focal"
		case "22.04":
			return "jammy"
		case "24.04":
			return "noble"
		default:
			return "jammy" // Default to 22.04
		}
	} else {
		// Debian
		switch i.config.OSVersion {
		case "11":
			return "bullseye"
		case "12":
			return "bookworm"
		default:
			return "bookworm"
		}
	}
}

// configureSystem configures the installed system
func (i *Installer) configureSystem() error {
	log.Println("Configuring system...")

	// Configure hostname
	if err := i.configureHostname(); err != nil {
		return err
	}

	// Configure network
	if err := i.configureNetwork(); err != nil {
		return err
	}

	// Configure fstab
	if err := i.configureFstab(); err != nil {
		return err
	}

	// Set root password
	if err := i.setRootPassword(); err != nil {
		return err
	}

	// Install additional packages
	if len(i.config.Packages) > 0 {
		if err := i.installPackages(); err != nil {
			log.Printf("Warning: Failed to install packages: %v", err)
		}
	}

	log.Println("  System configuration completed")
	return nil
}

// configureHostname sets the hostname
func (i *Installer) configureHostname() error {
	hostname := i.config.Network.Hostname
	if hostname == "" {
		hostname = "localhost"
	}

	log.Printf("  Setting hostname to %s", hostname)

	// Write /etc/hostname
	hostnamePath := filepath.Join(i.mountRoot, "etc", "hostname")
	if err := os.WriteFile(hostnamePath, []byte(hostname+"\n"), 0644); err != nil {
		return fmt.Errorf("failed to write hostname: %w", err)
	}

	// Update /etc/hosts
	hostsContent := fmt.Sprintf("127.0.0.1   localhost\n127.0.1.1   %s\n", hostname)
	hostsPath := filepath.Join(i.mountRoot, "etc", "hosts")
	if err := os.WriteFile(hostsPath, []byte(hostsContent), 0644); err != nil {
		return fmt.Errorf("failed to write hosts: %w", err)
	}

	return nil
}

// configureNetwork configures network settings
func (i *Installer) configureNetwork() error {
	log.Println("  Configuring network...")

	netConfig := i.config.Network

	switch strings.ToLower(i.config.OSType) {
	case "ubuntu", "debian":
		return i.configureDebianNetwork(netConfig)
	case "centos", "rocky", "rhel":
		return i.configureRHELNetwork(netConfig)
	default:
		return fmt.Errorf("unsupported OS type for network config: %s", i.config.OSType)
	}
}

// configureDebianNetwork configures Debian/Ubuntu network using netplan
func (i *Installer) configureDebianNetwork(netConfig NetworkConfig) error {
	netplanDir := filepath.Join(i.mountRoot, "etc", "netplan")
	if err := os.MkdirAll(netplanDir, 0755); err != nil {
		return fmt.Errorf("failed to create netplan dir: %w", err)
	}

	var netplanConfig string
	if netConfig.Method == "static" {
		netplanConfig = fmt.Sprintf(`network:
  version: 2
  ethernets:
    %s:
      addresses:
        - %s/%s
      gateway4: %s
      nameservers:
        addresses:
          - %s
`, netConfig.Interface, netConfig.IP, i.cidrFromNetmask(netConfig.Netmask), netConfig.Gateway, netConfig.DNS)
	} else {
		netplanConfig = fmt.Sprintf(`network:
  version: 2
  ethernets:
    %s:
      dhcp4: true
`, netConfig.Interface)
	}

	configPath := filepath.Join(netplanDir, "01-netcfg.yaml")
	if err := os.WriteFile(configPath, []byte(netplanConfig), 0600); err != nil {
		return fmt.Errorf("failed to write netplan config: %w", err)
	}

	return nil
}

// configureRHELNetwork configures RHEL/CentOS network using ifcfg files
func (i *Installer) configureRHELNetwork(netConfig NetworkConfig) error {
	networkScriptsDir := filepath.Join(i.mountRoot, "etc", "sysconfig", "network-scripts")
	if err := os.MkdirAll(networkScriptsDir, 0755); err != nil {
		return fmt.Errorf("failed to create network-scripts dir: %w", err)
	}

	var ifcfgContent string
	if netConfig.Method == "static" {
		ifcfgContent = fmt.Sprintf(`DEVICE=%s
BOOTPROTO=static
ONBOOT=yes
IPADDR=%s
NETMASK=%s
GATEWAY=%s
DNS1=%s
`, netConfig.Interface, netConfig.IP, netConfig.Netmask, netConfig.Gateway, netConfig.DNS)
	} else {
		ifcfgContent = fmt.Sprintf(`DEVICE=%s
BOOTPROTO=dhcp
ONBOOT=yes
`, netConfig.Interface)
	}

	configPath := filepath.Join(networkScriptsDir, "ifcfg-"+netConfig.Interface)
	if err := os.WriteFile(configPath, []byte(ifcfgContent), 0644); err != nil {
		return fmt.Errorf("failed to write ifcfg: %w", err)
	}

	return nil
}

// cidrFromNetmask converts netmask to CIDR notation
func (i *Installer) cidrFromNetmask(netmask string) string {
	masks := map[string]string{
		"255.255.255.0":   "24",
		"255.255.255.128": "25",
		"255.255.255.192": "26",
		"255.255.255.224": "27",
		"255.255.255.240": "28",
		"255.255.255.248": "29",
		"255.255.255.252": "30",
		"255.255.0.0":     "16",
		"255.0.0.0":       "8",
	}

	if cidr, ok := masks[netmask]; ok {
		return cidr
	}
	return "24" // Default
}

// configureFstab generates /etc/fstab
func (i *Installer) configureFstab() error {
	log.Println("  Generating /etc/fstab...")

	disk := i.config.DiskLayout.RootDisk
	var fstabLines []string

	partNum := 1
	for _, part := range i.config.DiskLayout.Partitions {
		partDevice := fmt.Sprintf("%s%d", disk, partNum)
		if strings.Contains(disk, "nvme") {
			partDevice = fmt.Sprintf("%sp%d", disk, partNum)
		}

		// Get UUID
		cmd := exec.Command("blkid", "-s", "UUID", "-o", "value", partDevice)
		uuidBytes, err := cmd.Output()
		uuid := strings.TrimSpace(string(uuidBytes))
		if err != nil || uuid == "" {
			uuid = partDevice // Fallback to device name
		} else {
			uuid = "UUID=" + uuid
		}

		var line string
		if part.MountPoint == "swap" {
			line = fmt.Sprintf("%s none swap sw 0 0", uuid)
		} else {
			dumpPass := "0 0"
			if part.MountPoint == "/" {
				dumpPass = "0 1"
			} else if part.MountPoint == "/boot" {
				dumpPass = "0 2"
			}
			line = fmt.Sprintf("%s %s %s defaults %s", uuid, part.MountPoint, part.FSType, dumpPass)
		}

		fstabLines = append(fstabLines, line)
		partNum++
	}

	fstabContent := strings.Join(fstabLines, "\n") + "\n"
	fstabPath := filepath.Join(i.mountRoot, "etc", "fstab")
	if err := os.WriteFile(fstabPath, []byte(fstabContent), 0644); err != nil {
		return fmt.Errorf("failed to write fstab: %w", err)
	}

	return nil
}

// setRootPassword sets the root password
func (i *Installer) setRootPassword() error {
	if i.config.RootPassword == "" {
		log.Println("  Skipping root password (not provided)")
		return nil
	}

	log.Println("  Setting root password...")

	// Use chroot to set password
	cmd := exec.Command("chroot", i.mountRoot, "/bin/bash", "-c",
		fmt.Sprintf("echo 'root:%s' | chpasswd -e", i.config.RootPassword))

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to set root password: %w\n%s", err, string(output))
	}

	return nil
}

// installPackages installs additional packages in chroot
func (i *Installer) installPackages() error {
	log.Printf("  Installing %d additional packages...", len(i.config.Packages))

	// Mount proc, sys, dev for chroot
	i.mountChrootFilesystems()
	defer i.unmountChrootFilesystems()

	var cmd *exec.Cmd
	switch strings.ToLower(i.config.OSType) {
	case "ubuntu", "debian":
		pkgList := strings.Join(i.config.Packages, " ")
		cmd = exec.Command("chroot", i.mountRoot, "apt-get", "install", "-y", pkgList)
	case "centos", "rocky", "rhel":
		pkgList := strings.Join(i.config.Packages, " ")
		pkgMgr := "dnf"
		if _, err := exec.LookPath("dnf"); err != nil {
			pkgMgr = "yum"
		}
		cmd = exec.Command("chroot", i.mountRoot, pkgMgr, "install", "-y", pkgList)
	default:
		return fmt.Errorf("unsupported OS type: %s", i.config.OSType)
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("package installation failed: %w", err)
	}

	return nil
}

// installBootloader installs and configures GRUB
func (i *Installer) installBootloader() error {
	log.Println("Installing bootloader...")

	// Mount proc, sys, dev for chroot
	i.mountChrootFilesystems()
	defer i.unmountChrootFilesystems()

	disk := i.config.DiskLayout.RootDisk

	// Install GRUB
	log.Printf("  Installing GRUB to %s", disk)

	var cmd *exec.Cmd
	switch strings.ToLower(i.config.OSType) {
	case "ubuntu", "debian":
		cmd = exec.Command("chroot", i.mountRoot, "grub-install", "--target=x86_64-efi", "--efi-directory=/boot/efi", "--bootloader-id=LPMOS", "--recheck", disk)
		if output, err := cmd.CombinedOutput(); err != nil {
			// Try legacy BIOS mode
			log.Printf("  EFI install failed, trying legacy BIOS mode: %v\n%s", err, string(output))
			cmd = exec.Command("chroot", i.mountRoot, "grub-install", disk)
			if output, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("grub-install failed: %w\n%s", err, string(output))
			}
		}

		// Update GRUB config
		cmd = exec.Command("chroot", i.mountRoot, "update-grub")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("update-grub failed: %w\n%s", err, string(output))
		}

	case "centos", "rocky", "rhel":
		cmd = exec.Command("chroot", i.mountRoot, "grub2-install", disk)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("grub2-install failed: %w\n%s", err, string(output))
		}

		// Generate GRUB config
		cmd = exec.Command("chroot", i.mountRoot, "grub2-mkconfig", "-o", "/boot/grub2/grub.cfg")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("grub2-mkconfig failed: %w\n%s", err, string(output))
		}
	}

	log.Println("  Bootloader installation completed")
	return nil
}

// mountChrootFilesystems mounts proc/sys/dev for chroot
func (i *Installer) mountChrootFilesystems() {
	filesystems := []struct {
		source string
		target string
		fstype string
		flags  string
	}{
		{"/proc", filepath.Join(i.mountRoot, "proc"), "proc", ""},
		{"/sys", filepath.Join(i.mountRoot, "sys"), "sysfs", ""},
		{"/dev", filepath.Join(i.mountRoot, "dev"), "", "--bind"},
		{"/dev/pts", filepath.Join(i.mountRoot, "dev", "pts"), "", "--bind"},
	}

	for _, fs := range filesystems {
		os.MkdirAll(fs.target, 0755)
		var cmd *exec.Cmd
		if fs.flags == "--bind" {
			cmd = exec.Command("mount", "--bind", fs.source, fs.target)
		} else {
			cmd = exec.Command("mount", "-t", fs.fstype, fs.source, fs.target)
		}
		cmd.Run()
	}
}

// unmountChrootFilesystems unmounts chroot filesystems
func (i *Installer) unmountChrootFilesystems() {
	targets := []string{
		filepath.Join(i.mountRoot, "dev", "pts"),
		filepath.Join(i.mountRoot, "dev"),
		filepath.Join(i.mountRoot, "sys"),
		filepath.Join(i.mountRoot, "proc"),
	}

	for _, target := range targets {
		cmd := exec.Command("umount", target)
		cmd.Run()
	}
}

// executePostScript executes post-install script
func (i *Installer) executePostScript() error {
	log.Println("Executing post-install script...")

	// Decode base64 script
	// scriptBytes, err := base64.StdEncoding.DecodeString(i.config.PostScript)
	// ... implementation similar to hardware config scripts

	return nil
}

// unmountFilesystems unmounts all filesystems
func (i *Installer) unmountFilesystems() error {
	log.Println("Unmounting filesystems...")

	// Deactivate swap
	exec.Command("swapoff", "-a").Run()

	// Unmount all under /mnt
	cmd := exec.Command("umount", "-R", i.mountRoot)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to unmount: %w\n%s", err, string(output))
	}

	return nil
}
