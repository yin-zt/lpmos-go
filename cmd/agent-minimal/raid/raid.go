package raid

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
)

// Config represents RAID configuration
type Config struct {
	Enabled     bool     `json:"enabled"`
	Level       string   `json:"level"`       // 0, 1, 5, 6, 10
	Disks       []string `json:"disks"`       // ["/dev/sdb", "/dev/sdc"]
	Controller  string   `json:"controller"`  // megacli, hpacucli, mdadm
	VirtualDisk string   `json:"virtual_disk"` // /dev/sda (after RAID)
}

// Configurator handles RAID configuration
type Configurator struct {
	config *Config
}

// NewConfigurator creates a new RAID configurator
func NewConfigurator(config *Config) *Configurator {
	return &Configurator{
		config: config,
	}
}

// Configure executes RAID configuration based on controller type
func (c *Configurator) Configure() error {
	if !c.config.Enabled {
		log.Println("RAID not enabled, skipping configuration")
		return nil
	}

	log.Printf("Configuring RAID %s using %s controller", c.config.Level, c.config.Controller)

	switch strings.ToLower(c.config.Controller) {
	case "megacli":
		return c.configureMegaCli()
	case "hpacucli":
		return c.configureHPACUCLI()
	case "mdadm":
		return c.configureMdadm()
	default:
		return fmt.Errorf("unsupported RAID controller: %s", c.config.Controller)
	}
}

// configureMegaCli configures LSI MegaRAID controllers
func (c *Configurator) configureMegaCli() error {
	log.Println("Configuring MegaRAID controller...")

	// Check if MegaCli is installed
	if _, err := exec.LookPath("MegaCli64"); err != nil {
		return fmt.Errorf("MegaCli64 not found: %w", err)
	}

	// Clear existing configuration (optional, be careful!)
	log.Println("  Clearing existing RAID configuration...")
	cmd := exec.Command("MegaCli64", "-CfgLdDel", "-LALL", "-aALL")
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("  Warning: Failed to clear config: %v\n%s", err, string(output))
	}

	// Create RAID array based on level
	log.Printf("  Creating RAID %s with disks: %v", c.config.Level, c.config.Disks)

	var raidLevel string
	switch c.config.Level {
	case "0":
		raidLevel = "-r0"
	case "1":
		raidLevel = "-r1"
	case "5":
		raidLevel = "-r5"
	case "6":
		raidLevel = "-r6"
	case "10":
		raidLevel = "-r10"
	default:
		return fmt.Errorf("unsupported RAID level: %s", c.config.Level)
	}

	// Build disk list for MegaCli (e.g., "0:1,0:2,0:3" for enclosure:slot)
	// This is a simplified version - real implementation needs to map /dev/sdX to enclosure:slot
	diskList := c.buildMegaCliDiskList()

	// Create virtual drive
	args := []string{"-CfgLdAdd", raidLevel, fmt.Sprintf("[%s]", diskList), "WB", "Direct", "-a0"}
	cmd = exec.Command("MegaCli64", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create RAID: %w\n%s", err, string(output))
	}

	log.Printf("  MegaCli output: %s", string(output))
	log.Println("  RAID configuration completed successfully")

	return nil
}

// configureHPACUCLI configures HP Smart Array controllers
func (c *Configurator) configureHPACUCLI() error {
	log.Println("Configuring HP Smart Array controller...")

	// Check if hpacucli is installed
	if _, err := exec.LookPath("hpacucli"); err != nil {
		return fmt.Errorf("hpacucli not found: %w", err)
	}

	// Clear existing configuration
	log.Println("  Clearing existing logical drives...")
	cmd := exec.Command("hpacucli", "controller", "slot=0", "logicaldrive", "all", "delete", "forced")
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("  Warning: Failed to clear config: %v\n%s", err, string(output))
	}

	// Create RAID array
	log.Printf("  Creating RAID %s with disks: %v", c.config.Level, c.config.Disks)

	// Build disk list for hpacucli (e.g., "1I:1:1,1I:1:2")
	diskList := c.buildHPACUCLIDiskList()

	// Create logical drive
	args := []string{
		"controller", "slot=0",
		"create", "type=logicaldrive",
		fmt.Sprintf("drives=%s", diskList),
		fmt.Sprintf("raid=%s", c.config.Level),
	}
	cmd = exec.Command("hpacucli", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create RAID: %w\n%s", err, string(output))
	}

	log.Printf("  hpacucli output: %s", string(output))
	log.Println("  RAID configuration completed successfully")

	return nil
}

// configureMdadm configures software RAID using mdadm
func (c *Configurator) configureMdadm() error {
	log.Println("Configuring software RAID (mdadm)...")

	// Check if mdadm is installed
	if _, err := exec.LookPath("mdadm"); err != nil {
		return fmt.Errorf("mdadm not found: %w", err)
	}

	// Unmount and zero superblock on all disks
	log.Println("  Preparing disks...")
	for _, disk := range c.config.Disks {
		// Zero superblock
		cmd := exec.Command("mdadm", "--zero-superblock", disk)
		if output, err := cmd.CombinedOutput(); err != nil {
			log.Printf("  Warning: Failed to zero %s: %v\n%s", disk, err, string(output))
		}
	}

	// Create RAID array
	log.Printf("  Creating RAID %s with disks: %v", c.config.Level, c.config.Disks)

	deviceName := c.config.VirtualDisk
	if deviceName == "" {
		deviceName = "/dev/md0"
	}

	args := []string{
		"--create", deviceName,
		fmt.Sprintf("--level=%s", c.config.Level),
		fmt.Sprintf("--raid-devices=%d", len(c.config.Disks)),
	}
	args = append(args, c.config.Disks...)

	cmd := exec.Command("mdadm", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create RAID: %w\n%s", err, string(output))
	}

	log.Printf("  mdadm output: %s", string(output))

	// Wait for array to sync (background process)
	log.Println("  RAID array created, syncing in background...")
	log.Printf("  Virtual disk available at: %s", deviceName)

	return nil
}

// buildMegaCliDiskList builds disk list in MegaCli format
func (c *Configurator) buildMegaCliDiskList() string {
	// This is a simplified version
	// Real implementation should query MegaCli to map /dev/sdX to enclosure:slot
	// For now, assume disks are in slots 1,2,3,etc. on enclosure 0
	var slots []string
	for i := range c.config.Disks {
		slots = append(slots, fmt.Sprintf("0:%d", i+1))
	}
	return strings.Join(slots, ",")
}

// buildHPACUCLIDiskList builds disk list in hpacucli format
func (c *Configurator) buildHPACUCLIDiskList() string {
	// This is a simplified version
	// Real implementation should query hpacucli to map /dev/sdX to port:box:bay
	// For now, assume disks are at 1I:1:1, 1I:1:2, etc.
	var drives []string
	for i := range c.config.Disks {
		drives = append(drives, fmt.Sprintf("1I:1:%d", i+1))
	}
	return strings.Join(drives, ",")
}

// Verify checks if RAID configuration was successful
func (c *Configurator) Verify() error {
	if !c.config.Enabled {
		return nil
	}

	log.Println("Verifying RAID configuration...")

	switch strings.ToLower(c.config.Controller) {
	case "megacli":
		return c.verifyMegaCli()
	case "hpacucli":
		return c.verifyHPACUCLI()
	case "mdadm":
		return c.verifyMdadm()
	default:
		return fmt.Errorf("unsupported RAID controller: %s", c.config.Controller)
	}
}

// verifyMegaCli verifies MegaRAID configuration
func (c *Configurator) verifyMegaCli() error {
	cmd := exec.Command("MegaCli64", "-LDInfo", "-Lall", "-aALL")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to verify RAID: %w", err)
	}

	log.Printf("  RAID status:\n%s", string(output))
	return nil
}

// verifyHPACUCLI verifies HP Smart Array configuration
func (c *Configurator) verifyHPACUCLI() error {
	cmd := exec.Command("hpacucli", "controller", "slot=0", "logicaldrive", "all", "show")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to verify RAID: %w", err)
	}

	log.Printf("  RAID status:\n%s", string(output))
	return nil
}

// verifyMdadm verifies software RAID configuration
func (c *Configurator) verifyMdadm() error {
	deviceName := c.config.VirtualDisk
	if deviceName == "" {
		deviceName = "/dev/md0"
	}

	cmd := exec.Command("mdadm", "--detail", deviceName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to verify RAID: %w", err)
	}

	log.Printf("  RAID status:\n%s", string(output))
	return nil
}
