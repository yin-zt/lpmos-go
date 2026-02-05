package kickstart

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Config represents kickstart installation configuration
type Config struct {
	KickstartURL string
	KernelURL    string
	InitrdURL    string
	OSType       string
	OSVersion    string
}

// Installer handles kickstart-based installation using kexec
type Installer struct {
	config    *Config
	workDir   string
	kernelPath string
	initrdPath string
	ksPath     string
}

// NewInstaller creates a new kickstart installer
func NewInstaller(config *Config) *Installer {
	return &Installer{
		config:  config,
		workDir: "/tmp/ks-install",
	}
}

// Install performs kickstart installation using kexec
func (i *Installer) Install() error {
	log.Println("Starting kickstart installation...")

	// Step 1: Prepare working directory
	if err := i.prepareWorkDir(); err != nil {
		return fmt.Errorf("failed to prepare work dir: %w", err)
	}

	// Step 2: Download kickstart file
	if err := i.downloadKickstart(); err != nil {
		return fmt.Errorf("failed to download kickstart: %w", err)
	}

	// Step 3: Download kernel and initrd
	if err := i.downloadBootFiles(); err != nil {
		return fmt.Errorf("failed to download boot files: %w", err)
	}

	// Step 4: Load kernel with kexec
	if err := i.loadKexec(); err != nil {
		return fmt.Errorf("failed to load kexec: %w", err)
	}

	// Step 5: Execute kexec (reboot into installer)
	log.Println("Rebooting into kickstart installer...")
	if err := i.executeKexec(); err != nil {
		return fmt.Errorf("failed to execute kexec: %w", err)
	}

	return nil
}

// prepareWorkDir creates working directory
func (i *Installer) prepareWorkDir() error {
	log.Printf("Preparing work directory: %s", i.workDir)

	if err := os.MkdirAll(i.workDir, 0755); err != nil {
		return fmt.Errorf("failed to create work dir: %w", err)
	}

	i.kernelPath = filepath.Join(i.workDir, "vmlinuz")
	i.initrdPath = filepath.Join(i.workDir, "initrd.img")
	i.ksPath = filepath.Join(i.workDir, "kickstart.cfg")

	return nil
}

// downloadKickstart downloads kickstart file
func (i *Installer) downloadKickstart() error {
	log.Printf("Downloading kickstart from: %s", i.config.KickstartURL)

	resp, err := http.Get(i.config.KickstartURL)
	if err != nil {
		return fmt.Errorf("failed to download kickstart: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("kickstart download failed: HTTP %d", resp.StatusCode)
	}

	file, err := os.Create(i.ksPath)
	if err != nil {
		return fmt.Errorf("failed to create kickstart file: %w", err)
	}
	defer file.Close()

	if _, err := io.Copy(file, resp.Body); err != nil {
		return fmt.Errorf("failed to write kickstart file: %w", err)
	}

	log.Printf("Kickstart saved to: %s", i.ksPath)

	// Log kickstart content for debugging
	content, _ := os.ReadFile(i.ksPath)
	log.Printf("Kickstart content:\n%s", string(content))

	return nil
}

// downloadBootFiles downloads kernel and initrd
func (i *Installer) downloadBootFiles() error {
	// If kernel/initrd URLs not provided, construct from OS type
	if i.config.KernelURL == "" || i.config.InitrdURL == "" {
		if err := i.constructBootURLs(); err != nil {
			return err
		}
	}

	// Download kernel
	log.Printf("Downloading kernel from: %s", i.config.KernelURL)
	if err := i.downloadFile(i.config.KernelURL, i.kernelPath); err != nil {
		return fmt.Errorf("failed to download kernel: %w", err)
	}

	// Download initrd
	log.Printf("Downloading initrd from: %s", i.config.InitrdURL)
	if err := i.downloadFile(i.config.InitrdURL, i.initrdPath); err != nil {
		return fmt.Errorf("failed to download initrd: %w", err)
	}

	log.Println("Boot files downloaded successfully")
	return nil
}

// constructBootURLs constructs kernel/initrd URLs from kickstart URL
func (i *Installer) constructBootURLs() error {
	// Extract base URL from kickstart URL
	// e.g., http://192.168.100.1:8081/api/v1/kickstart/SN123
	// -> http://192.168.100.1:8081
	parts := strings.Split(i.config.KickstartURL, "/api/")
	if len(parts) < 2 {
		return fmt.Errorf("invalid kickstart URL format")
	}
	baseURL := parts[0]

	// Construct URLs based on OS type
	switch strings.ToLower(i.config.OSType) {
	case "centos", "rocky":
		i.config.KernelURL = fmt.Sprintf("%s/repos/%s/%s/isolinux/vmlinuz", baseURL, i.config.OSType, i.config.OSVersion)
		i.config.InitrdURL = fmt.Sprintf("%s/repos/%s/%s/isolinux/initrd.img", baseURL, i.config.OSType, i.config.OSVersion)
	case "ubuntu":
		i.config.KernelURL = fmt.Sprintf("%s/repos/%s/%s/casper/vmlinuz", baseURL, i.config.OSType, i.config.OSVersion)
		i.config.InitrdURL = fmt.Sprintf("%s/repos/%s/%s/casper/initrd", baseURL, i.config.OSType, i.config.OSVersion)
	default:
		return fmt.Errorf("unsupported OS type for boot URL construction: %s", i.config.OSType)
	}

	log.Printf("Constructed kernel URL: %s", i.config.KernelURL)
	log.Printf("Constructed initrd URL: %s", i.config.InitrdURL)

	return nil
}

// downloadFile downloads a file from URL
func (i *Installer) downloadFile(url, destPath string) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	file, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	if _, err := io.Copy(file, resp.Body); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// loadKexec loads kernel and initrd into kexec
func (i *Installer) loadKexec() error {
	log.Println("Loading kernel into kexec...")

	// Check if kexec-tools is installed
	if _, err := exec.LookPath("kexec"); err != nil {
		return fmt.Errorf("kexec not found: %w (please install kexec-tools)", err)
	}

	// Build kernel command line
	cmdline := i.buildKernelCmdline()

	log.Printf("Kernel cmdline: %s", cmdline)

	// Load kernel with kexec
	args := []string{
		"-l", i.kernelPath,
		"--initrd=" + i.initrdPath,
		"--append=" + cmdline,
	}

	cmd := exec.Command("kexec", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("kexec load failed: %w\n%s", err, string(output))
	}

	log.Printf("kexec output: %s", string(output))
	log.Println("Kernel loaded into kexec successfully")

	return nil
}

// buildKernelCmdline builds kernel command line for kickstart installation
func (i *Installer) buildKernelCmdline() string {
	var params []string

	// Basic parameters
	params = append(params, "console=tty0")
	params = append(params, "console=ttyS0,115200n8")

	// Kickstart URL
	params = append(params, fmt.Sprintf("ks=%s", i.config.KickstartURL))

	// Installation mode
	switch strings.ToLower(i.config.OSType) {
	case "centos", "rocky":
		params = append(params, "inst.text")
		params = append(params, "inst.cmdline")
	case "ubuntu":
		params = append(params, "auto=true")
		params = append(params, "priority=critical")
		params = append(params, fmt.Sprintf("url=%s", i.config.KickstartURL))
	}

	// Network configuration (use DHCP for installation)
	params = append(params, "ip=dhcp")

	return strings.Join(params, " ")
}

// executeKexec executes kexec to reboot into installer
func (i *Installer) executeKexec() error {
	log.Println("Executing kexec (rebooting into installer)...")

	// Sync filesystems
	exec.Command("sync").Run()

	// Execute kexec
	cmd := exec.Command("kexec", "-e")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kexec execution failed: %w", err)
	}

	// If we reach here, kexec failed (should not happen)
	return fmt.Errorf("kexec did not reboot system")
}

// Verify checks if kickstart file is valid
func (i *Installer) Verify() error {
	log.Println("Verifying kickstart configuration...")

	// Read kickstart file
	content, err := os.ReadFile(i.ksPath)
	if err != nil {
		return fmt.Errorf("failed to read kickstart: %w", err)
	}

	ksContent := string(content)

	// Basic validation
	requiredKeywords := []string{"network", "rootpw", "bootloader", "clearpart"}
	for _, keyword := range requiredKeywords {
		if !strings.Contains(ksContent, keyword) {
			log.Printf("Warning: Kickstart may be missing '%s' directive", keyword)
		}
	}

	log.Println("Kickstart verification completed")
	return nil
}
