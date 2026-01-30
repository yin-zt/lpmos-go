package main

import (
	"bytes"
	"encoding/json"
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
)

// HardwareInfo contains collected hardware information
type HardwareInfo struct {
	MACAddress string        `json:"mac_address"`
	CPU        CPUInfo       `json:"cpu"`
	Memory     MemoryInfo    `json:"memory"`
	Disks      []DiskInfo    `json:"disks"`
	Network    []NetworkInfo `json:"network"`
	BIOS       BIOSInfo      `json:"bios"`
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
	MACAddress string                 `json:"mac_address"`
	TaskID     string                 `json:"task_id"`
	Stage      string                 `json:"stage"`
	Percentage int                    `json:"percentage"`
	Message    string                 `json:"message"`
	Details    map[string]interface{} `json:"details,omitempty"`
}

var (
	regionalClientURL = getEnv("REGIONAL_CLIENT_URL", "http://localhost:8081")
	taskID            string
	macAddress        string
)

func main() {
	log.Println("=== LPMOS Agent Started ===")
	log.Printf("Regional Client: %s", regionalClientURL)

	// Step 1: Collect hardware information
	log.Println("\n[1/4] Collecting hardware information...")
	hwInfo := collectHardware()
	macAddress = hwInfo.MACAddress

	log.Printf("  MAC Address: %s", hwInfo.MACAddress)
	log.Printf("  CPU: %s (%d cores)", hwInfo.CPU.Model, hwInfo.CPU.Cores)
	log.Printf("  Memory: %d GB", hwInfo.Memory.TotalGB)
	log.Printf("  Disks: %d", len(hwInfo.Disks))
	for _, disk := range hwInfo.Disks {
		log.Printf("    - %s: %d GB (%s)", disk.Device, disk.SizeGB, disk.Type)
	}

	// Step 2: Report hardware to regional client
	log.Println("\n[2/4] Reporting hardware to regional client...")
	if err := reportHardware(hwInfo); err != nil {
		log.Fatalf("Failed to report hardware: %v", err)
	}
	log.Println("  Hardware reported successfully")

	// Step 3: Wait for approval
	log.Println("\n[3/4] Waiting for approval...")
	if err := waitForApproval(); err != nil {
		log.Fatalf("Failed to get approval: %v", err)
	}
	log.Printf("  Task approved! Task ID: %s", taskID)

	// Step 4: Install OS
	log.Println("\n[4/4] Starting OS installation...")
	if err := installOS(); err != nil {
		log.Fatalf("Installation failed: %v", err)
	}

	log.Println("\n=== OS Installation Completed Successfully ===")
}

// collectHardware gathers hardware information using stdlib only
func collectHardware() HardwareInfo {
	hw := HardwareInfo{}

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

	return hw
}

// collectCPU uses runtime and /proc/cpuinfo
func collectCPU() CPUInfo {
	cpu := CPUInfo{
		Cores:   runtime.NumCPU(),
		Threads: runtime.NumCPU(),
	}

	// Read /proc/cpuinfo for model name
	data, err := os.ReadFile("/proc/cpuinfo")
	if err == nil {
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

	if cpu.Model == "" {
		cpu.Model = "Unknown CPU"
	}

	return cpu
}

// collectMemory collects memory info (cross-platform)
func collectMemory() MemoryInfo {
	// Read /proc/meminfo (works on Linux)
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		log.Printf("Failed to get memory info: %v", err)
		return MemoryInfo{TotalGB: 0}
	}

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

	return MemoryInfo{TotalGB: 0}
}

// collectDisks reads from /sys/block
func collectDisks() []DiskInfo {
	var disks []DiskInfo

	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		log.Printf("Failed to read /sys/block: %v", err)
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

// collectBIOS reads from /sys/class/dmi/id/
func collectBIOS() BIOSInfo {
	bios := BIOSInfo{}

	// Read BIOS vendor
	if data, err := os.ReadFile("/sys/class/dmi/id/bios_vendor"); err == nil {
		bios.Vendor = strings.TrimSpace(string(data))
	}

	// Read BIOS version
	if data, err := os.ReadFile("/sys/class/dmi/id/bios_version"); err == nil {
		bios.Version = strings.TrimSpace(string(data))
	}

	// Read system serial number
	if data, err := os.ReadFile("/sys/class/dmi/id/product_serial"); err == nil {
		bios.Serial = strings.TrimSpace(string(data))
	}

	return bios
}

// reportHardware sends hardware info to regional client
func reportHardware(hw HardwareInfo) error {
	url := regionalClientURL + "/api/v1/agent/report"

	reqBody := map[string]interface{}{
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

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	// Parse response to get task ID
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if tid, ok := result["task_id"].(string); ok {
		taskID = tid
	}

	return nil
}

// waitForApproval polls the regional client for approval
func waitForApproval() error {
	url := fmt.Sprintf("%s/api/v1/agent/approval/%s", regionalClientURL, macAddress)

	for i := 0; i < 300; i++ { // Wait up to 5 minutes
		resp, err := http.Get(url)
		if err != nil {
			time.Sleep(1 * time.Second)
			continue
		}

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			time.Sleep(1 * time.Second)
			continue
		}
		resp.Body.Close()

		if approved, ok := result["approved"].(bool); ok && approved {
			if tid, ok := result["task_id"].(string); ok {
				taskID = tid
			}
			return nil
		}

		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("approval timeout")
}

// installOS performs the OS installation with progress reporting
func installOS() error {
	stages := []struct {
		name       string
		startPct   int
		endPct     int
		fn         func() error
	}{
		{"partitioning", 0, 20, stagePartitioning},
		{"downloading", 20, 60, stageDownloading},
		{"installing", 60, 90, stageInstalling},
		{"configuring", 90, 100, stageConfiguring},
	}

	for _, stage := range stages {
		reportProgress(stage.name, stage.startPct, fmt.Sprintf("Starting %s...", stage.name))

		if err := stage.fn(); err != nil {
			reportProgress(stage.name, stage.startPct, fmt.Sprintf("Failed: %v", err))
			return fmt.Errorf("%s failed: %w", stage.name, err)
		}

		reportProgress(stage.name, stage.endPct, fmt.Sprintf("%s completed", stage.name))
	}

	return nil
}

// stagePartitioning simulates disk partitioning
func stagePartitioning() error {
	log.Println("  [Partitioning] Creating partitions...")

	// Simulate partitioning work
	time.Sleep(2 * time.Second)
	reportProgress("partitioning", 10, "Created boot partition")

	time.Sleep(1 * time.Second)
	reportProgress("partitioning", 15, "Created root partition")

	time.Sleep(1 * time.Second)
	reportProgress("partitioning", 20, "Partitioning complete")

	return nil
}

// stageDownloading simulates OS image download
func stageDownloading() error {
	log.Println("  [Downloading] Downloading OS image...")

	// Simulate download with progress
	totalSize := int64(1000) // Simulated 1000 MB
	downloaded := int64(0)

	for downloaded < totalSize {
		time.Sleep(100 * time.Millisecond)
		downloaded += 50

		percentage := 20 + int(float64(downloaded)/float64(totalSize)*40)
		msg := fmt.Sprintf("Downloaded %d MB / %d MB", downloaded, totalSize)

		reportProgress("downloading", percentage, msg)
	}

	return nil
}

// stageInstalling simulates OS installation
func stageInstalling() error {
	log.Println("  [Installing] Installing OS packages...")

	steps := []string{
		"Installing base system",
		"Installing kernel",
		"Installing bootloader",
		"Installing utilities",
	}

	for i, step := range steps {
		time.Sleep(1 * time.Second)
		percentage := 60 + (i+1)*7
		reportProgress("installing", percentage, step)
	}

	reportProgress("installing", 90, "Installation complete")
	return nil
}

// stageConfiguring simulates system configuration
func stageConfiguring() error {
	log.Println("  [Configuring] Configuring system...")

	configs := []string{
		"Configuring network",
		"Configuring bootloader",
		"Setting hostname",
		"Finalizing",
	}

	for i, config := range configs {
		time.Sleep(500 * time.Millisecond)
		percentage := 90 + (i+1)*2
		reportProgress("configuring", percentage, config)
	}

	reportProgress("configuring", 100, "Configuration complete")
	return nil
}

// reportProgress sends progress update to regional client
func reportProgress(stage string, percentage int, message string) {
	if taskID == "" {
		return
	}

	url := regionalClientURL + "/api/v1/agent/progress"

	progress := ProgressReport{
		MACAddress: macAddress,
		TaskID:     taskID,
		Stage:      stage,
		Percentage: percentage,
		Message:    message,
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

	log.Printf("  Progress: [%d%%] %s - %s", percentage, stage, message)
}

// Helper functions

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s failed: %w, output: %s", name, err, string(output))
	}
	return nil
}
