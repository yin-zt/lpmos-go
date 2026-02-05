package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/lpmos/lpmos-go/cmd/agent-minimal/install"
	"github.com/lpmos/lpmos-go/cmd/agent-minimal/kickstart"
	"github.com/lpmos/lpmos-go/cmd/agent-minimal/raid"
)

// HardwareInfo contains collected hardware information
type HardwareInfo struct {
	SerialNumber string        `json:"serial_number"`
	MACAddress   string        `json:"mac_address"`
	Company      string        `json:"company"`    // System manufacturer
	Product      string        `json:"product"`    // Product name
	ModelName    string        `json:"model_name"` // Model name
	IsVM         bool          `json:"is_vm"`      // Virtual machine detection
	CPU          CPUInfo       `json:"cpu"`
	Memory       MemoryInfo    `json:"memory"`
	Disks        []DiskInfo    `json:"disks"`
	Network      []NetworkInfo `json:"network"`
	BIOS         BIOSInfo      `json:"bios"`
}

type CPUInfo struct {
	Model   string `json:"model"`
	Cores   int    `json:"cores"`
	Threads int    `json:"threads"`
}

type MemoryInfo struct {
	TotalGB int `json:"total_gb"`
}

type DiskInfo struct {
	Device string `json:"device"`
	SizeGB int    `json:"size_gb"`
	Type   string `json:"type"`
}

type NetworkInfo struct {
	Interface string `json:"interface"`
	MAC       string `json:"mac"`
	Speed     string `json:"speed"`
}

type BIOSInfo struct {
	Vendor  string `json:"vendor"`
	Version string `json:"version"`
	Serial  string `json:"serial"`
}

type ProgressReport struct {
	SN         string                 `json:"sn"`
	MACAddress string                 `json:"mac_address"`
	TaskID     string                 `json:"task_id"`
	Step       string                 `json:"step"`
	Percent    int                    `json:"percent"`
	Message    string                 `json:"message"`
	Details    map[string]interface{} `json:"details,omitempty"`
}

type TaskResponse struct {
	TaskID      string `json:"task_id"`
	SN          string `json:"sn"`
	MAC         string `json:"mac"`
	Status      string `json:"status"`
	OSType      string `json:"os_type"`
	OSVersion   string `json:"os_version"`
	DiskLayout  string `json:"disk_layout"`
	NetworkConf string `json:"network_config"`
}

// InstallQueueRequest represents request to check install queue status
type InstallQueueRequest struct {
	SN string `json:"sn"`
}

// InstallQueueResponse represents response from install queue check
type InstallQueueResponse struct {
	Result bool `json:"result"`
}

// NextOperationRequest represents request to get next operation
type NextOperationRequest struct {
	SN string `json:"sn"`
}

// NextOperationResponse represents response with next operation
type NextOperationResponse struct {
	Operation string                 `json:"operation"` // hardware_config|network_config|reboot|complete
	Data      map[string]interface{} `json:"data,omitempty"`
}

// HardwareConfigRequest represents request to get hardware config scripts
type HardwareConfigRequest struct {
	SN string `json:"sn"`
}

// HardwareScript represents a single hardware configuration script
type HardwareScript struct {
	Name   string `json:"name"`
	Script string `json:"script"` // base64 encoded
}

// RAIDConfig represents RAID configuration
type RAIDConfig struct {
	Enabled     bool     `json:"enabled"`
	Level       string   `json:"level"`
	Disks       []string `json:"disks"`
	Controller  string   `json:"controller"`
	VirtualDisk string   `json:"virtual_disk"`
}

// HardwareConfigResponse represents response with hardware config scripts
type HardwareConfigResponse struct {
	Scripts []HardwareScript `json:"scripts"`
	RAID    *RAIDConfig      `json:"raid,omitempty"`
}

// OperationCompleteRequest represents operation completion report
type OperationCompleteRequest struct {
	SN        string `json:"sn"`
	Operation string `json:"operation"`
	Success   bool   `json:"success"`
	Message   string `json:"message,omitempty"`
}

var (
	regionalClientURL = ""
	taskID            string
	serialNumber      string
	macAddress        string
	pollingInterval   = 10 * time.Second
)

func main() {
	// Parse command-line flags
	regionalURL := flag.String("regional-url", getEnv("REGIONAL_CLIENT_URL", "http://localhost:8081"), "Regional client URL")
	flag.Parse()

	regionalClientURL = *regionalURL

	log.Println("=== LPMOS Agent Started (OS-Agent Workflow) ===")
	log.Printf("Regional Client: %s", regionalClientURL)
	log.Printf("Polling Interval: %v", pollingInterval)

	// ===== STAGE 1: Collect and Report Hardware Information =====
	log.Println("\n[Stage 1] Collecting hardware information...")
	hwInfo := collectHardware()
	serialNumber = hwInfo.SerialNumber
	macAddress = hwInfo.MACAddress

	log.Printf("  Serial Number: %s", hwInfo.SerialNumber)
	log.Printf("  MAC Address: %s", hwInfo.MACAddress)
	log.Printf("  Company: %s", hwInfo.Company)
	log.Printf("  Product: %s", hwInfo.Product)
	log.Printf("  Model: %s", hwInfo.ModelName)
	log.Printf("  Is VM: %v", hwInfo.IsVM)
	log.Printf("  CPU: %s (%d cores)", hwInfo.CPU.Model, hwInfo.CPU.Cores)
	log.Printf("  Memory: %d GB", hwInfo.Memory.TotalGB)
	log.Printf("  Disks: %d", len(hwInfo.Disks))
	for _, disk := range hwInfo.Disks {
		log.Printf("    - %s: %d GB (%s)", disk.Device, disk.SizeGB, disk.Type)
	}

	log.Println("\n[Stage 1] Reporting hardware to regional client...")
	if err := reportHardware(hwInfo); err != nil {
		log.Fatalf("Failed to report hardware: %v", err)
	}
	log.Println("  Hardware reported successfully")

	// ===== STAGE 2: Poll for Install Queue =====
	log.Println("\n[Stage 2] Checking if added to install queue...")
	if err := pollInstallQueue(); err != nil {
		log.Fatalf("Failed while polling install queue: %v", err)
	}
	log.Println("  Machine added to install queue!")

	// ===== STAGE 3: Operation Loop - Keep asking "What should I do next?" =====
	log.Println("\n[Stage 3] Entering operation loop (servant mode)...")
	if err := operationLoop(); err != nil {
		log.Fatalf("Operation loop failed: %v", err)
	}

	log.Println("\n=== Agent Workflow Completed Successfully ===")
}

// collectHardware gathers enhanced hardware information using stdlib only
func collectHardware() HardwareInfo {
	hw := HardwareInfo{}

	// Collect system information (Company, Product, Model)
	hw.Company = getSystemInfo("sys_vendor")
	hw.Product = getSystemInfo("product_name")
	hw.ModelName = getSystemInfo("product_version")

	// Detect if running in VM
	hw.IsVM = detectVirtualMachine()

	// Collect CPU info
	hw.CPU = collectCPU()

	// Collect memory info
	hw.Memory = collectMemory()

	// Collect disk info
	hw.Disks = collectDisks()

	// Collect network info
	hw.Network = collectNetwork()

	// Get primary MAC address
	if len(hw.Network) > 0 {
		hw.MACAddress = hw.Network[0].MAC
	}

	// Collect BIOS info
	hw.BIOS = collectBIOS()

	// Get serial number - try multiple sources
	hw.SerialNumber = getSerialNumber()
	if hw.SerialNumber == "" {
		// Fallback: use MAC as serial if BIOS serial not available
		hw.SerialNumber = strings.ReplaceAll(hw.MACAddress, ":", "-")
		log.Printf("  Warning: Using MAC address as serial number (no system serial found)")
	}

	return hw
}

// getSystemInfo reads system information from DMI
func getSystemInfo(field string) string {
	// Try Linux DMI first
	path := "/sys/class/dmi/id/" + field
	if data, err := os.ReadFile(path); err == nil {
		return strings.TrimSpace(string(data))
	}

	// Try macOS system_profiler
	if runtime.GOOS == "darwin" {
		cmd := exec.Command("system_profiler", "SPHardwareDataType")
		output, err := cmd.Output()
		if err == nil {
			lines := strings.Split(string(output), "\n")
			var lookupKey string
			switch field {
			case "sys_vendor":
				lookupKey = "Model Identifier"
			case "product_name":
				lookupKey = "Model Name"
			case "product_version":
				lookupKey = "Model Number"
			}

			if lookupKey != "" {
				for _, line := range lines {
					if strings.Contains(line, lookupKey) {
						parts := strings.Split(line, ":")
						if len(parts) > 1 {
							return strings.TrimSpace(parts[1])
						}
					}
				}
			}
		}
	}

	return "Unknown"
}

// detectVirtualMachine detects if running in a virtual machine
func detectVirtualMachine() bool {
	// Method 1: Check DMI system-product-name
	if data, err := os.ReadFile("/sys/class/dmi/id/sys_vendor"); err == nil {
		vendor := strings.ToLower(string(data))
		vmVendors := []string{"vmware", "virtualbox", "qemu", "kvm", "xen", "microsoft corporation", "innotek", "parallels"}
		for _, vm := range vmVendors {
			if strings.Contains(vendor, vm) {
				return true
			}
		}
	}

	// Method 2: Check product name
	if data, err := os.ReadFile("/sys/class/dmi/id/product_name"); err == nil {
		product := strings.ToLower(string(data))
		vmProducts := []string{"virtual", "vmware", "virtualbox", "kvm", "qemu"}
		for _, vm := range vmProducts {
			if strings.Contains(product, vm) {
				return true
			}
		}
	}

	// Method 3: Check for hypervisor flag on Linux
	if data, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		if strings.Contains(string(data), "hypervisor") {
			return true
		}
	}

	// Method 4: macOS virtualization detection
	if runtime.GOOS == "darwin" {
		cmd := exec.Command("sysctl", "-n", "machdep.cpu.features")
		output, err := cmd.Output()
		if err == nil && strings.Contains(string(output), "VMM") {
			return true
		}
	}

	return false
}

// getSerialNumber retrieves system serial number from multiple sources
func getSerialNumber() string {
	// Try DMI product serial
	// Use only the first field to handle VMware VMs with spaces in serial number
	if data, err := os.ReadFile("/sys/class/dmi/id/product_serial"); err == nil {
		serial := strings.TrimSpace(string(data))
		// Extract only the first field (space-separated)
		if fields := strings.Fields(serial); len(fields) > 0 {
			serial = fields[0]
		}
		if serial != "" && serial != "Not Specified" && serial != "To Be Filled By O.E.M." && serial != "Default string" {
			return serial
		}
	}

	// Try DMI board serial
	// Use only the first field to handle VMware VMs with spaces in serial number
	if data, err := os.ReadFile("/sys/class/dmi/id/board_serial"); err == nil {
		serial := strings.TrimSpace(string(data))
		// Extract only the first field (space-separated)
		if fields := strings.Fields(serial); len(fields) > 0 {
			serial = fields[0]
		}
		if serial != "" && serial != "Not Specified" && serial != "To Be Filled By O.E.M." && serial != "Default string" {
			return serial
		}
	}

	// Try dmidecode command (requires root)
	// Use awk to get only the first field to handle VMware VMs with spaces in serial number
	// Example: "VMware-56 4d f9 07..." becomes "VMware-56"
	// Physical servers have continuous serial numbers without spaces, so they remain unchanged
	cmd := exec.Command("sh", "-c", "dmidecode -s system-serial-number | awk '{print $1}'")
	if output, err := cmd.Output(); err == nil {
		serial := strings.TrimSpace(string(output))
		if serial != "" && serial != "Not Specified" && serial != "To Be Filled By O.E.M." && serial != "Default string" {
			return serial
		}
	}

	// macOS system_profiler
	if runtime.GOOS == "darwin" {
		cmd := exec.Command("system_profiler", "SPHardwareDataType")
		output, err := cmd.Output()
		if err == nil {
			lines := strings.Split(string(output), "\n")
			for _, line := range lines {
				if strings.Contains(line, "Serial Number") {
					parts := strings.Split(line, ":")
					if len(parts) > 1 {
						serial := strings.TrimSpace(parts[1])
						if serial != "" {
							return serial
						}
					}
				}
			}
		}
	}

	return ""
}

// collectCPU uses runtime and platform-specific methods
func collectCPU() CPUInfo {
	cpu := CPUInfo{
		Cores:   runtime.NumCPU(),
		Threads: runtime.NumCPU(),
	}

	// Linux: Read /proc/cpuinfo for model name
	if data, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "model name") {
				parts := strings.Split(line, ":")
				if len(parts) > 1 {
					cpu.Model = strings.TrimSpace(parts[1])
					break
				}
			}
		}
	}

	// macOS: Use sysctl
	if cpu.Model == "" && runtime.GOOS == "darwin" {
		cmd := exec.Command("sysctl", "-n", "machdep.cpu.brand_string")
		if output, err := cmd.Output(); err == nil {
			cpu.Model = strings.TrimSpace(string(output))
		}
	}

	if cpu.Model == "" {
		cpu.Model = "Unknown CPU"
	}

	return cpu
}

// collectMemory collects memory info (cross-platform)
func collectMemory() MemoryInfo {
	// Linux: Read /proc/meminfo
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "MemTotal:") {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					kb, _ := strconv.ParseInt(parts[1], 10, 64)
					totalGB := int(kb / 1024 / 1024)
					return MemoryInfo{TotalGB: totalGB}
				}
			}
		}
	}

	// macOS: Use sysctl
	if runtime.GOOS == "darwin" {
		cmd := exec.Command("sysctl", "-n", "hw.memsize")
		if output, err := cmd.Output(); err == nil {
			bytes, _ := strconv.ParseInt(strings.TrimSpace(string(output)), 10, 64)
			totalGB := int(bytes / 1024 / 1024 / 1024)
			return MemoryInfo{TotalGB: totalGB}
		}
	}

	log.Printf("Warning: Failed to get memory info")
	return MemoryInfo{TotalGB: 0}
}

// collectDisks reads disk information (cross-platform)
func collectDisks() []DiskInfo {
	var disks []DiskInfo

	if runtime.GOOS == "darwin" {
		// macOS: Use diskutil
		cmd := exec.Command("diskutil", "list")
		if output, err := cmd.Output(); err == nil {
			lines := strings.Split(string(output), "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "/dev/disk") && !strings.Contains(line, "synthesized") {
					fields := strings.Fields(line)
					if len(fields) >= 3 {
						disk := DiskInfo{
							Device: fields[0],
							Type:   "Unknown",
						}
						// Parse size
						sizeStr := fields[len(fields)-2]
						if strings.HasSuffix(sizeStr, "GB") {
							size, _ := strconv.ParseFloat(strings.TrimSuffix(sizeStr, "GB"), 64)
							disk.SizeGB = int(size)
						} else if strings.HasSuffix(sizeStr, "TB") {
							size, _ := strconv.ParseFloat(strings.TrimSuffix(sizeStr, "TB"), 64)
							disk.SizeGB = int(size * 1024)
						}
						if disk.SizeGB > 0 {
							disks = append(disks, disk)
						}
					}
				}
			}
		}
		return disks
	}

	// Linux: Read from /sys/block
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		log.Printf("Warning: Failed to read /sys/block: %v", err)
		return disks
	}

	for _, entry := range entries {
		name := entry.Name()

		// Skip loop devices and other virtual devices
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") {
			continue
		}

		// Read size from /sys/block/{dev}/size (in 512-byte sectors)
		sizePath := fmt.Sprintf("/sys/block/%s/size", name)
		sizeData, err := os.ReadFile(sizePath)
		if err != nil {
			continue
		}

		sizeBlocks, err := strconv.ParseInt(strings.TrimSpace(string(sizeData)), 10, 64)
		if err != nil || sizeBlocks == 0 {
			continue
		}

		sizeGB := int(sizeBlocks * 512 / 1024 / 1024 / 1024)

		// Determine disk type (heuristic)
		diskType := "HDD"
		if strings.HasPrefix(name, "nvme") {
			diskType = "NVMe"
		} else if sizeGB < 1000 {
			diskType = "SSD" // Assume smaller disks are SSDs
		}

		disks = append(disks, DiskInfo{
			Device: "/dev/" + name,
			SizeGB: sizeGB,
			Type:   diskType,
		})
	}

	return disks
}

// collectNetwork uses net.Interfaces from stdlib
func collectNetwork() []NetworkInfo {
	var networks []NetworkInfo

	interfaces, err := net.Interfaces()
	if err != nil {
		log.Printf("Failed to get network interfaces: %v", err)
		return networks
	}

	for _, iface := range interfaces {
		// Skip loopback and down interfaces
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}

		// Skip virtual interfaces
		if strings.HasPrefix(iface.Name, "veth") ||
			strings.HasPrefix(iface.Name, "docker") ||
			strings.HasPrefix(iface.Name, "br-") {
			continue
		}

		networks = append(networks, NetworkInfo{
			Interface: iface.Name,
			MAC:       iface.HardwareAddr.String(),
			Speed:     "Unknown",
		})
	}

	return networks
}

// collectBIOS reads BIOS information (cross-platform)
func collectBIOS() BIOSInfo {
	bios := BIOSInfo{}

	// Linux: Try DMI
	if data, err := os.ReadFile("/sys/class/dmi/id/bios_vendor"); err == nil {
		bios.Vendor = strings.TrimSpace(string(data))
	}

	if data, err := os.ReadFile("/sys/class/dmi/id/bios_version"); err == nil {
		bios.Version = strings.TrimSpace(string(data))
	}

	if data, err := os.ReadFile("/sys/class/dmi/id/product_serial"); err == nil {
		serial := strings.TrimSpace(string(data))
		if serial != "" && serial != "Not Specified" && serial != "To Be Filled By O.E.M." {
			bios.Serial = serial
		}
	}

	// macOS: Use system_profiler
	if runtime.GOOS == "darwin" {
		cmd := exec.Command("system_profiler", "SPHardwareDataType")
		output, err := cmd.Output()
		if err == nil {
			lines := strings.Split(string(output), "\n")
			for _, line := range lines {
				if strings.Contains(line, "Serial Number") {
					parts := strings.Split(line, ":")
					if len(parts) > 1 {
						bios.Serial = strings.TrimSpace(parts[1])
					}
				} else if strings.Contains(line, "Boot ROM Version") {
					parts := strings.Split(line, ":")
					if len(parts) > 1 {
						bios.Version = strings.TrimSpace(parts[1])
					}
				}
			}
		}
		if bios.Vendor == "" {
			bios.Vendor = "Apple Inc."
		}
	}

	return bios
}

// reportHardware sends hardware info to regional client
func reportHardware(hw HardwareInfo) error {
	url := regionalClientURL + "/api/v1/report"

	reqBody := map[string]interface{}{
		"sn":          hw.SerialNumber,
		"mac_address": hw.MACAddress,
		"hardware":    hw,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(data))
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK:
		var result map[string]interface{}
		if err := json.Unmarshal(body, &result); err == nil {
			log.Printf("Hardware report response: %s", result["message"])
		}
		return nil
	case http.StatusNotFound:
		// Machine not registered yet - this is expected
		log.Printf("Hardware reported (machine will be registered)")
		return nil
	default:
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}
}

// pollInstallQueue polls until the machine is added to install queue
func pollInstallQueue() error {
	url := regionalClientURL + "/api/v1/device/isInInstallQueue"

	maxAttempts := 120 // 20 minutes with 10-second intervals
	attempt := 0

	client := &http.Client{
		Timeout: 5 * time.Second, // 建议 3~10 秒
	}

	for attempt < maxAttempts {
		attempt++
		log.Printf("  Polling install queue (attempt %d/%d)...", attempt, maxAttempts)

		log.Printf("  Serial number: %s", serialNumber)
		req := InstallQueueRequest{SN: serialNumber}
		data, err := json.Marshal(req)
		log.Printf("  Request data: %s", string(data))
		log.Printf("  URL: %s", url)
		log.Printf("  Error: %v", err)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}

		resp, err := client.Post(url, "application/json", bytes.NewBuffer(data))
		log.Printf("  Response: %+v", resp)
		log.Printf("  Error: %v", err)
		if err != nil {
			log.Printf("  Network error: %v, retrying in %v", err, pollingInterval)
			time.Sleep(pollingInterval)
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			var queueResp InstallQueueResponse
			if err := json.Unmarshal(body, &queueResp); err != nil {
				log.Printf("  Failed to parse response: %v", err)
				time.Sleep(pollingInterval)
				continue
			}
			log.Printf("  Queue response: %+v", queueResp)
			if queueResp.Result {
				log.Printf("  Machine is in install queue!")
				return nil
			} else {
				log.Printf("  Not in install queue yet, waiting...")
			}
		} else {
			log.Printf("  Unexpected response: %d - %s", resp.StatusCode, string(body))
		}

		time.Sleep(pollingInterval)
	}

	return fmt.Errorf("timeout waiting for install queue after %d attempts", maxAttempts)
}

// operationLoop keeps asking the server "what should I do next?"
func operationLoop() error {
	operationCount := 0
	maxOperations := 100 // Safety limit to prevent infinite loops

	for operationCount < maxOperations {
		operationCount++
		log.Printf("\n[Operation %d] Asking server: What should I do next?", operationCount)

		// Get next operation from server
		nextOp, err := getNextOperation()
		if err != nil {
			return fmt.Errorf("failed to get next operation: %w", err)
		}

		log.Printf("  Server says: %s", nextOp.Operation)

		// Execute the operation
		switch nextOp.Operation {
		case "hardware_config":
			if err := executeHardwareConfig(); err != nil {
				log.Printf("  Hardware config failed: %v", err)
				reportOperationComplete("hardware_config", false, err.Error())
				return err
			}
			reportOperationComplete("hardware_config", true, "Hardware config completed")

		case "network_config":
			if err := executeNetworkConfig(nextOp.Data); err != nil {
				log.Printf("  Network config failed: %v", err)
				reportOperationComplete("network_config", false, err.Error())
				return err
			}
			reportOperationComplete("network_config", true, "Network config completed")

		case "os_install":
			if err := executeOSInstall(nextOp.Data); err != nil {
				log.Printf("  OS install failed: %v", err)
				reportOperationComplete("os_install", false, err.Error())
				return err
			}
			reportOperationComplete("os_install", true, "OS install completed")

		case "reboot":
			log.Println("  Server requests reboot")
			reportOperationComplete("reboot", true, "Preparing to reboot")
			log.Println("  [SIMULATED] System would reboot now")
			return nil

		case "complete":
			log.Println("  All operations completed!")
			return nil

		default:
			log.Printf("  Unknown operation: %s (skipping)", nextOp.Operation)
			reportOperationComplete(nextOp.Operation, false, "Unknown operation type")
		}

		// Small delay between operations
		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("reached maximum operation count (%d), stopping", maxOperations)
}

// getNextOperation asks the server what to do next
func getNextOperation() (*NextOperationResponse, error) {
	url := regionalClientURL + "/api/v1/device/getNextOperation"

	req := NextOperationRequest{SN: serialNumber}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(data))
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	var nextOp NextOperationResponse
	if err := json.Unmarshal(body, &nextOp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &nextOp, nil
}

// executeHardwareConfig gets and executes hardware configuration scripts
func executeHardwareConfig() error {
	log.Println("  Executing hardware configuration...")

	// Get hardware config from server
	url := regionalClientURL + "/api/v1/device/getHardwareConfig"

	req := HardwareConfigRequest{SN: serialNumber}
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(data))
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	var hwConfig HardwareConfigResponse
	if err := json.Unmarshal(body, &hwConfig); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Check if RAID configuration is present in data
	if hwConfig.RAID != nil && hwConfig.RAID.Enabled {
		log.Println("  RAID configuration detected, configuring RAID...")

		raidConfig := &raid.Config{
			Enabled:     hwConfig.RAID.Enabled,
			Level:       hwConfig.RAID.Level,
			Disks:       hwConfig.RAID.Disks,
			Controller:  hwConfig.RAID.Controller,
			VirtualDisk: hwConfig.RAID.VirtualDisk,
		}

		configurator := raid.NewConfigurator(raidConfig)
		if err := configurator.Configure(); err != nil {
			return fmt.Errorf("RAID configuration failed: %w", err)
		}

		// Verify RAID configuration
		if err := configurator.Verify(); err != nil {
			log.Printf("Warning: RAID verification failed: %v", err)
		}

		log.Println("  RAID configuration completed successfully")
	}

	// Execute custom scripts if present
	log.Printf("  Received %d hardware config script(s)", len(hwConfig.Scripts))

	for i, script := range hwConfig.Scripts {
		log.Printf("  [%d/%d] Executing script: %s", i+1, len(hwConfig.Scripts), script.Name)

		if err := executeScript(script); err != nil {
			log.Printf("  Script %s failed: %v", script.Name, err)
			return fmt.Errorf("script %s failed: %w", script.Name, err)
		}

		log.Printf("  Script %s completed successfully", script.Name)
	}

	return nil
}

// executeScript decodes and executes a base64-encoded script
func executeScript(script HardwareScript) error {
	// Decode base64 script
	scriptBytes, err := base64.StdEncoding.DecodeString(script.Script)
	if err != nil {
		return fmt.Errorf("failed to decode script: %w", err)
	}

	// Create temporary script file
	tmpFile, err := os.CreateTemp("", fmt.Sprintf("hw-config-%s-*.sh", script.Name))
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	// Write script content
	if _, err := tmpFile.Write(scriptBytes); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write script: %w", err)
	}

	// Make executable
	if err := tmpFile.Chmod(0755); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to make script executable: %w", err)
	}
	tmpFile.Close()

	// Execute script
	log.Printf("    Executing: %s", tmpFile.Name())
	cmd := exec.Command("/bin/bash", tmpFile.Name())
	output, err := cmd.CombinedOutput()

	if err != nil {
		log.Printf("    Script output: %s", string(output))
		return fmt.Errorf("script execution failed: %w", err)
	}

	log.Printf("    Script output: %s", string(output))
	return nil
}

// executeNetworkConfig configures network settings
func executeNetworkConfig(data map[string]interface{}) error {
	log.Println("  Executing network configuration...")
	log.Printf("  Network config data: %+v", data)

	// This is a placeholder - actual implementation would configure network
	// based on the data provided by the server
	time.Sleep(2 * time.Second)

	log.Println("  Network configuration completed (simulated)")
	return nil
}

// executeOSInstall performs OS installation
func executeOSInstall(data map[string]interface{}) error {
	log.Println("  Executing OS installation...")

	// Extract installation method
	installMethod, ok := data["install_method"].(string)
	if !ok {
		installMethod = "agent_direct" // Default
	}

	log.Printf("  Installation method: %s", installMethod)

	switch installMethod {
	case "kickstart":
		return executeKickstartInstall(data)
	case "agent_direct":
		return executeAgentDirectInstall(data)
	default:
		return fmt.Errorf("unknown installation method: %s", installMethod)
	}
}

// executeKickstartInstall performs kickstart-based installation
func executeKickstartInstall(data map[string]interface{}) error {
	log.Println("  Using kickstart installation method...")

	// Extract kickstart URL
	kickstartURL, ok := data["kickstart_url"].(string)
	if !ok || kickstartURL == "" {
		return fmt.Errorf("kickstart_url not provided")
	}

	osType, _ := data["os_type"].(string)
	osVersion, _ := data["os_version"].(string)

	log.Printf("  Kickstart URL: %s", kickstartURL)

	// Create kickstart installer
	ksConfig := &kickstart.Config{
		KickstartURL: kickstartURL,
		OSType:       osType,
		OSVersion:    osVersion,
	}

	installer := kickstart.NewInstaller(ksConfig)

	// Verify kickstart (optional)
	// installer.Verify()

	// Perform installation (will reboot system via kexec)
	if err := installer.Install(); err != nil {
		return fmt.Errorf("kickstart installation failed: %w", err)
	}

	// If we reach here, kexec should have rebooted the system
	// This code should not execute
	log.Println("  System should have rebooted via kexec")
	return nil
}

// executeAgentDirectInstall performs agent direct installation
func executeAgentDirectInstall(data map[string]interface{}) error {
	log.Println("  Using agent direct installation method...")

	// Convert map to JSON and back to install.Config
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal install config: %w", err)
	}

	var installConfig install.Config
	if err := json.Unmarshal(jsonData, &installConfig); err != nil {
		return fmt.Errorf("failed to unmarshal install config: %w", err)
	}

	log.Printf("  Installing: %s %s", installConfig.OSType, installConfig.OSVersion)
	log.Printf("  Disk: %s", installConfig.DiskLayout.RootDisk)
	log.Printf("  Partitions: %d", len(installConfig.DiskLayout.Partitions))

	// Create installer
	installer := install.NewInstaller(&installConfig)

	// Perform installation
	if err := installer.Install(); err != nil {
		return fmt.Errorf("agent direct installation failed: %w", err)
	}

	log.Println("  Agent direct installation completed successfully")
	return nil
}

// reportOperationComplete reports operation completion to server
func reportOperationComplete(operation string, success bool, message string) {
	url := regionalClientURL + "/api/v1/device/operationComplete"

	req := OperationCompleteRequest{
		SN:        serialNumber,
		Operation: operation,
		Success:   success,
		Message:   message,
	}

	data, err := json.Marshal(req)
	if err != nil {
		log.Printf("Failed to marshal operation complete: %v", err)
		return
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(data))
	if err != nil {
		log.Printf("Failed to report operation complete: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("Operation complete report failed: %d - %s", resp.StatusCode, string(body))
	} else {
		log.Printf("  Operation '%s' reported as %v", operation, success)
	}
}

// Legacy function kept for compatibility but not used in new workflow
func pollForTask() (*TaskResponse, error) {
	url := fmt.Sprintf("%s/api/v1/task/%s", regionalClientURL, serialNumber)

	maxAttempts := 60 // 10 minutes with 10-second intervals
	attempt := 0

	for attempt < maxAttempts {
		attempt++
		log.Printf("  Polling for task (attempt %d/%d)...", attempt, maxAttempts)

		resp, err := http.Get(url)
		if err != nil {
			log.Printf("  Network error: %v, retrying in %v", err, pollingInterval)
			time.Sleep(pollingInterval)
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			var task TaskResponse
			if err := json.Unmarshal(body, &task); err != nil {
				log.Printf("  Failed to parse task: %v", err)
				time.Sleep(pollingInterval)
				continue
			}

			// Check if task is approved
			if task.Status == "approved" {
				log.Printf("  Task found and approved!")
				return &task, nil
			} else {
				log.Printf("  Task found but not approved yet (status: %s)", task.Status)
			}
		} else if resp.StatusCode == http.StatusNotFound {
			log.Printf("  No task assigned yet")
		} else {
			log.Printf("  Unexpected response: %d - %s", resp.StatusCode, string(body))
		}

		// Wait before next poll
		time.Sleep(pollingInterval)
	}

	return nil, fmt.Errorf("timeout waiting for task assignment after %d attempts", maxAttempts)
}

// Legacy function kept for compatibility but not used in new workflow
func installOS(task *TaskResponse) error {
	log.Printf("  OS Type: %s", task.OSType)
	log.Printf("  OS Version: %s", task.OSVersion)
	log.Printf("  Disk Layout: %s", task.DiskLayout)

	// Simulate installation stages
	stages := []struct {
		name    string
		percent int
		message string
		delay   time.Duration
	}{
		{"partitioning", 50, "Creating disk partitions", 2 * time.Second},
		{"downloading", 60, "Downloading OS image", 3 * time.Second},
		{"installing", 70, "Installing base system", 3 * time.Second},
		{"configuring", 80, "Configuring system", 2 * time.Second},
		{"finalizing", 90, "Finalizing installation", 2 * time.Second},
		{"completed", 100, "Installation completed successfully", 1 * time.Second},
	}

	for _, stage := range stages {
		log.Printf("  [%s] %s...", stage.name, stage.message)
		time.Sleep(stage.delay)

		reportProgress(stage.name, stage.percent, stage.message, nil)
	}

	return nil
}

// reportProgress sends progress update to regional client (legacy, kept for compatibility)
func reportProgress(step string, percent int, message string, details map[string]interface{}) {
	if taskID == "" && percent < 20 {
		// Before task assignment, we can't report with task_id
		log.Printf("  Progress: [%d%%] %s - %s (no task assigned yet)", percent, step, message)
		return
	}

	url := regionalClientURL + "/api/v1/progress"

	progress := ProgressReport{
		SN:         serialNumber,
		MACAddress: macAddress,
		TaskID:     taskID,
		Step:       step,
		Percent:    percent,
		Message:    message,
		Details:    details,
	}

	data, err := json.Marshal(progress)
	if err != nil {
		log.Printf("Failed to marshal progress: %v", err)
		return
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(data))
	if err != nil {
		log.Printf("Failed to send progress: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("Progress update failed: %d - %s", resp.StatusCode, string(body))
	} else {
		log.Printf("  Progress: [%d%%] %s - %s", percent, step, message)
	}
}

// Helper functions

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
