package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/lpmos/lpmos-go/pkg/hardware"
	"github.com/lpmos/lpmos-go/pkg/models"
)

func main() {
	log.Println("Starting LPMOS Boot Agent...")

	// Get configuration from kernel command line or environment
	regionalClientURL := getEnv("REGIONAL_CLIENT_URL", "http://localhost:8081")

	// Wait a bit for system to stabilize
	time.Sleep(2 * time.Second)

	// Collect hardware information
	collector := hardware.NewCollector()
	hwInfo, err := collector.Collect()
	if err != nil {
		log.Fatalf("Failed to collect hardware info: %v", err)
	}

	log.Printf("Hardware collected for MAC: %s", hwInfo.MACAddress)
	log.Printf("CPU: %s (%d cores, %d threads)", hwInfo.CPU.Model, hwInfo.CPU.Cores, hwInfo.CPU.Threads)
	log.Printf("Memory: %d GB", hwInfo.Memory.TotalGB)
	log.Printf("Disks: %d", len(hwInfo.Disks))
	for _, disk := range hwInfo.Disks {
		log.Printf("  - %s: %d GB %s (%s)", disk.Device, disk.SizeGB, disk.Type, disk.Model)
	}
	log.Printf("Network interfaces: %d", len(hwInfo.Network))
	for _, nic := range hwInfo.Network {
		log.Printf("  - %s: %s (%s)", nic.Interface, nic.MAC, nic.Speed)
	}

	// Report to regional client
	reportURL := fmt.Sprintf("%s/api/v1/agent/report", regionalClientURL)
	log.Printf("Reporting to regional client: %s", reportURL)

	report := models.AgentReportRequest{
		MACAddress: hwInfo.MACAddress,
		Hardware:   *hwInfo,
	}

	if err := sendReport(reportURL, report); err != nil {
		log.Fatalf("Failed to send report: %v", err)
	}

	log.Println("Hardware report sent successfully!")
	log.Println("Waiting for approval and installation instructions...")

	// In production, the agent would:
	// 1. Wait for installation commands
	// 2. Monitor installation progress
	// 3. Report status updates
	// 4. Reboot into installed OS when complete

	// Keep agent running
	select {}
}

func sendReport(url string, report models.AgentReportRequest) error {
	data, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("failed to marshal report: %w", err)
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(data))
	if err != nil {
		return fmt.Errorf("failed to send HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	log.Printf("Server response: %v", result)
	return nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
