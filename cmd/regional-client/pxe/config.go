package pxe

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// Generator generates PXE configuration files
type Generator struct {
	tftpRoot string
	configDir string
}

// Config holds PXE boot configuration
type Config struct {
	TFTPRoot string
}

// BootConfig represents a PXE boot configuration for a specific server
type BootConfig struct {
	MAC           net.HardwareAddr
	IP            net.IP
	Hostname      string
	OSType        string // ubuntu, centos, rocky
	OSVersion     string
	KernelPath    string
	InitrdPath    string
	RegionalURL   string
	SerialNumber  string
	DataCenter    string
	CustomParams  map[string]string
}

// NewGenerator creates a new PXE configuration generator
func NewGenerator(config Config) (*Generator, error) {
	// Validate TFTP root directory
	if _, err := os.Stat(config.TFTPRoot); os.IsNotExist(err) {
		return nil, fmt.Errorf("TFTP root directory does not exist: %s", config.TFTPRoot)
	}

	configDir := filepath.Join(config.TFTPRoot, "pxelinux.cfg")

	// Ensure pxelinux.cfg directory exists
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create pxelinux.cfg directory: %w", err)
	}

	return &Generator{
		tftpRoot:  config.TFTPRoot,
		configDir: configDir,
	}, nil
}

// GenerateConfig generates a PXE configuration file for a server
func (g *Generator) GenerateConfig(bc *BootConfig) error {
	// Validate boot config
	if err := g.validateBootConfig(bc); err != nil {
		return fmt.Errorf("invalid boot config: %w", err)
	}

	// Get configuration template based on OS type
	tmpl, err := g.getTemplate(bc.OSType)
	if err != nil {
		return fmt.Errorf("failed to get template: %w", err)
	}

	// Generate configuration file name: 01-{mac-address}
	// Example: 01-00-1a-2b-3c-4d-5e
	configFileName := g.getMACConfigFileName(bc.MAC)
	configFilePath := filepath.Join(g.configDir, configFileName)

	// Create configuration file
	file, err := os.Create(configFilePath)
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}
	defer file.Close()

	// Execute template
	if err := tmpl.Execute(file, bc); err != nil {
		return fmt.Errorf("failed to execute template: %w", err)
	}

	return nil
}

// GenerateDefaultConfig generates a default PXE configuration
func (g *Generator) GenerateDefaultConfig() error {
	defaultConfigPath := filepath.Join(g.configDir, "default")

	defaultContent := `DEFAULT menu.c32
PROMPT 0
TIMEOUT 100
ONTIMEOUT local

MENU TITLE PXE Boot Menu

LABEL local
  MENU LABEL Boot from local disk
  LOCALBOOT 0

LABEL install
  MENU LABEL OS Installation (Manual)
  KERNEL /kernels/vmlinuz
  APPEND initrd=/initrds/initrd.img

MENU END
`

	return os.WriteFile(defaultConfigPath, []byte(defaultContent), 0644)
}

// RemoveConfig removes a PXE configuration file for a MAC address
func (g *Generator) RemoveConfig(mac net.HardwareAddr) error {
	configFileName := g.getMACConfigFileName(mac)
	configFilePath := filepath.Join(g.configDir, configFileName)

	if err := os.Remove(configFilePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove config file: %w", err)
	}

	return nil
}

// ConfigExists checks if a configuration file exists for a MAC address
func (g *Generator) ConfigExists(mac net.HardwareAddr) bool {
	configFileName := g.getMACConfigFileName(mac)
	configFilePath := filepath.Join(g.configDir, configFileName)

	_, err := os.Stat(configFilePath)
	return err == nil
}

// ListConfigs lists all PXE configuration files
func (g *Generator) ListConfigs() ([]string, error) {
	entries, err := os.ReadDir(g.configDir)
	if err != nil {
		return nil, err
	}

	var configs []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "01-") {
			configs = append(configs, entry.Name())
		}
	}

	return configs, nil
}

// getMACConfigFileName converts MAC address to PXE config file name
// Example: 00:1a:2b:3c:4d:5e -> 01-00-1a-2b-3c-4d-5e
func (g *Generator) getMACConfigFileName(mac net.HardwareAddr) string {
	macStr := strings.ToLower(mac.String())
	macStr = strings.ReplaceAll(macStr, ":", "-")
	return "01-" + macStr
}

// getTemplate returns the appropriate template for the OS type
func (g *Generator) getTemplate(osType string) (*template.Template, error) {
	var tmplContent string

	switch strings.ToLower(osType) {
	case "ubuntu":
		tmplContent = ubuntuTemplate
	case "centos":
		tmplContent = centosTemplate
	case "rocky", "rockylinux":
		tmplContent = rockyTemplate
	case "debian":
		tmplContent = debianTemplate
	default:
		return nil, fmt.Errorf("unsupported OS type: %s", osType)
	}

	tmpl, err := template.New("pxe-config").Parse(tmplContent)
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}

	return tmpl, nil
}

// validateBootConfig validates boot configuration
func (g *Generator) validateBootConfig(bc *BootConfig) error {
	if bc.MAC == nil {
		return fmt.Errorf("MAC address is required")
	}

	if bc.OSType == "" {
		return fmt.Errorf("OS type is required")
	}

	if bc.KernelPath == "" {
		return fmt.Errorf("kernel path is required")
	}

	if bc.InitrdPath == "" {
		return fmt.Errorf("initrd path is required")
	}

	if bc.RegionalURL == "" {
		return fmt.Errorf("regional URL is required")
	}

	return nil
}

// GetBootParams generates boot parameters string
func (bc *BootConfig) GetBootParams() string {
	params := []string{}

	// Base parameters
	params = append(params, fmt.Sprintf("regional_url=%s", bc.RegionalURL))

	if bc.SerialNumber != "" {
		params = append(params, fmt.Sprintf("sn=%s", bc.SerialNumber))
	}

	if bc.DataCenter != "" {
		params = append(params, fmt.Sprintf("dc=%s", bc.DataCenter))
	}

	if bc.Hostname != "" {
		params = append(params, fmt.Sprintf("hostname=%s", bc.Hostname))
	}

	// Network parameters
	if bc.IP != nil {
		params = append(params, fmt.Sprintf("ip=%s", bc.IP.String()))
	}

	// Custom parameters
	for key, value := range bc.CustomParams {
		params = append(params, fmt.Sprintf("%s=%s", key, value))
	}

	return strings.Join(params, " ")
}
