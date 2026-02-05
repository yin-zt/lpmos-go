package hardware

import (
	"fmt"
	"log"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/lpmos/lpmos-go/pkg/models"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

// Collector collects hardware information from the system
type Collector struct{}

// NewCollector creates a new hardware collector
func NewCollector() *Collector {
	return &Collector{}
}

// Collect gathers all hardware information
func (c *Collector) Collect() (*models.HardwareInfo, error) {
	log.Println("Collecting hardware information...")

	info := &models.HardwareInfo{
		CollectedAt: time.Now(),
	}

	// Get MAC address
	mac, err := c.getPrimaryMAC()
	if err != nil {
		return nil, fmt.Errorf("failed to get MAC address: %w", err)
	}
	info.MACAddress = mac

	// Collect CPU info
	cpuInfo, err := c.collectCPU()
	if err != nil {
		log.Printf("Warning: failed to collect CPU info: %v", err)
	} else {
		info.CPU = cpuInfo
	}

	// Collect memory info
	memInfo, err := c.collectMemory()
	if err != nil {
		log.Printf("Warning: failed to collect memory info: %v", err)
	} else {
		info.Memory = memInfo
	}

	// Collect disk info
	diskInfo, err := c.collectDisks()
	if err != nil {
		log.Printf("Warning: failed to collect disk info: %v", err)
	} else {
		info.Disks = diskInfo
	}

	// Collect network info
	netInfo, err := c.collectNetwork()
	if err != nil {
		log.Printf("Warning: failed to collect network info: %v", err)
	} else {
		info.Network = netInfo
	}

	// Collect BIOS info
	biosInfo, err := c.collectBIOS()
	if err != nil {
		log.Printf("Warning: failed to collect BIOS info: %v", err)
	} else {
		info.BIOS = biosInfo
	}

	return info, nil
}

func (c *Collector) getPrimaryMAC() (string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}

	for _, iface := range interfaces {
		// Skip loopback and down interfaces
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}

		// Skip virtual interfaces
		if strings.HasPrefix(iface.Name, "veth") || strings.HasPrefix(iface.Name, "docker") {
			continue
		}

		if iface.HardwareAddr != nil && len(iface.HardwareAddr) > 0 {
			return iface.HardwareAddr.String(), nil
		}
	}

	return "", fmt.Errorf("no valid MAC address found")
}

func (c *Collector) collectCPU() (models.CPUInfo, error) {
	cpuInfo := models.CPUInfo{}

	// Get CPU info
	infos, err := cpu.Info()
	if err != nil {
		return cpuInfo, err
	}

	if len(infos) > 0 {
		cpuInfo.Model = infos[0].ModelName
		cpuInfo.Cores = int(infos[0].Cores)

		// Get logical CPU count (threads)
		counts, err := cpu.Counts(true)
		if err == nil {
			cpuInfo.Threads = counts
		}
	}

	return cpuInfo, nil
}

func (c *Collector) collectMemory() (models.MemoryInfo, error) {
	memInfo := models.MemoryInfo{}

	vmStat, err := mem.VirtualMemory()
	if err != nil {
		return memInfo, err
	}

	memInfo.TotalGB = int(vmStat.Total / 1024 / 1024 / 1024)

	// Try to get detailed DIMM info using dmidecode (requires root)
	dimms := c.collectDIMMs()
	if len(dimms) > 0 {
		memInfo.DIMMs = dimms
	}

	return memInfo, nil
}

func (c *Collector) collectDIMMs() []models.DIMMInfo {
	var dimms []models.DIMMInfo

	// This requires root privileges and dmidecode installed
	cmd := exec.Command("dmidecode", "-t", "memory")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return dimms
	}

	// Parse dmidecode output (simplified)
	lines := strings.Split(string(output), "\n")
	var currentDIMM *models.DIMMInfo

	for _, line := range lines {
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "Locator:") && !strings.Contains(line, "Bank") {
			if currentDIMM != nil && currentDIMM.SizeGB > 0 {
				dimms = append(dimms, *currentDIMM)
			}
			currentDIMM = &models.DIMMInfo{
				Slot: strings.TrimSpace(strings.TrimPrefix(line, "Locator:")),
			}
		} else if strings.HasPrefix(line, "Size:") && currentDIMM != nil {
			sizeStr := strings.TrimSpace(strings.TrimPrefix(line, "Size:"))
			if strings.Contains(sizeStr, "GB") {
				size, _ := strconv.Atoi(strings.Fields(sizeStr)[0])
				currentDIMM.SizeGB = size
			}
		} else if strings.HasPrefix(line, "Type:") && currentDIMM != nil {
			currentDIMM.Type = strings.TrimSpace(strings.TrimPrefix(line, "Type:"))
		} else if strings.HasPrefix(line, "Speed:") && currentDIMM != nil {
			speedStr := strings.TrimSpace(strings.TrimPrefix(line, "Speed:"))
			if strings.Contains(speedStr, "MHz") {
				speed, _ := strconv.Atoi(strings.Fields(speedStr)[0])
				currentDIMM.SpeedMHz = speed
			}
		}
	}

	if currentDIMM != nil && currentDIMM.SizeGB > 0 {
		dimms = append(dimms, *currentDIMM)
	}

	return dimms
}

func (c *Collector) collectDisks() ([]models.DiskInfo, error) {
	var disks []models.DiskInfo

	// Use lsblk to get disk information
	cmd := exec.Command("lsblk", "-d", "-n", "-o", "NAME,SIZE,TYPE,MODEL")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return disks, err
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}

		// Only include disk type (not loop, rom, etc.)
		if fields[2] != "disk" {
			continue
		}

		disk := models.DiskInfo{
			Device: "/dev/" + fields[0],
			Model:  "",
		}

		// Parse size
		sizeStr := fields[1]
		if strings.HasSuffix(sizeStr, "G") {
			size, _ := strconv.ParseFloat(strings.TrimSuffix(sizeStr, "G"), 64)
			disk.SizeGB = int(size)
		} else if strings.HasSuffix(sizeStr, "T") {
			size, _ := strconv.ParseFloat(strings.TrimSuffix(sizeStr, "T"), 64)
			disk.SizeGB = int(size * 1024)
		}

		// Get model if available
		if len(fields) > 3 {
			disk.Model = strings.Join(fields[3:], " ")
		}

		// Determine disk type (simplified heuristic)
		if strings.Contains(fields[0], "nvme") {
			disk.Type = "NVMe"
		} else if disk.SizeGB < 1000 {
			disk.Type = "SSD" // Assume smaller disks are SSDs
		} else {
			disk.Type = "HDD"
		}

		disks = append(disks, disk)
	}

	return disks, nil
}

func (c *Collector) collectNetwork() ([]models.NetworkInfo, error) {
	var netInterfaces []models.NetworkInfo

	interfaces, err := net.Interfaces()
	if err != nil {
		return netInterfaces, err
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

		netInfo := models.NetworkInfo{
			Interface: iface.Name,
			MAC:       iface.HardwareAddr.String(),
			Speed:     "Unknown",
		}

		// Try to get speed from ethtool (requires root)
		cmd := exec.Command("ethtool", iface.Name)
		output, err := cmd.CombinedOutput()
		if err == nil {
			lines := strings.Split(string(output), "\n")
			for _, line := range lines {
				if strings.Contains(line, "Speed:") {
					speed := strings.TrimSpace(strings.Split(line, ":")[1])
					netInfo.Speed = speed
					break
				}
			}
		}

		netInterfaces = append(netInterfaces, netInfo)
	}

	return netInterfaces, nil
}

func (c *Collector) collectBIOS() (models.BIOSInfo, error) {
	biosInfo := models.BIOSInfo{}

	// Use dmidecode to get BIOS info (requires root)
	cmd := exec.Command("dmidecode", "-t", "bios")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return biosInfo, err
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "Vendor:") {
			biosInfo.Vendor = strings.TrimSpace(strings.TrimPrefix(line, "Vendor:"))
		} else if strings.HasPrefix(line, "Version:") {
			biosInfo.Version = strings.TrimSpace(strings.TrimPrefix(line, "Version:"))
		}
	}

	// Get serial number from system info
	// Use awk to get only the first field to handle VMware VMs with spaces in serial number
	// Example: "VMware-56 4d f9 07..." becomes "VMware-56"
	// Physical servers have continuous serial numbers without spaces, so they remain unchanged
	cmd = exec.Command("sh", "-c", "dmidecode -s system-serial-number | awk '{print $1}'")
	output, err = cmd.CombinedOutput()
	if err == nil {
		biosInfo.Serial = strings.TrimSpace(string(output))
	}

	return biosInfo, nil
}
