package models

import "time"

// TaskStatus represents the current status of an installation task
type TaskStatus string

const (
	TaskStatusPending         TaskStatus = "pending"
	TaskStatusReady           TaskStatus = "ready"
	TaskStatusBooting         TaskStatus = "booting"
	TaskStatusPendingApproval TaskStatus = "pending_approval"
	TaskStatusApproved        TaskStatus = "approved"
	TaskStatusInstalling      TaskStatus = "installing"
	TaskStatusCompleted       TaskStatus = "completed"
	TaskStatusFailed          TaskStatus = "failed"
)

// ApprovalStatus represents the approval state
type ApprovalStatus string

const (
	ApprovalStatusPending  ApprovalStatus = "pending"
	ApprovalStatusApproved ApprovalStatus = "approved"
	ApprovalStatusRejected ApprovalStatus = "rejected"
)

// Task represents an OS installation task
type Task struct {
	ID          string                 `json:"id"`
	RegionID    string                 `json:"region_id"`
	TargetMAC   string                 `json:"target_mac"`
	OSType      string                 `json:"os_type"`
	OSVersion   string                 `json:"os_version"`
	DiskLayout  string                 `json:"disk_layout"`
	NetworkConf string                 `json:"network_config"`
	CreatedAt   time.Time              `json:"created_at"`
	CreatedBy   string                 `json:"created_by"`
	Tags        map[string]string      `json:"tags,omitempty"`
	Status      TaskStatus             `json:"status"`
	Hardware    *HardwareInfo          `json:"hardware,omitempty"`
	Approval    *Approval              `json:"approval,omitempty"`
	Error       *TaskError             `json:"error,omitempty"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// HardwareInfo contains hardware details collected by the agent
type HardwareInfo struct {
	MACAddress  string         `json:"mac_address"`
	CPU         CPUInfo        `json:"cpu"`
	Memory      MemoryInfo     `json:"memory"`
	Disks       []DiskInfo     `json:"disks"`
	Network     []NetworkInfo  `json:"network"`
	BIOS        BIOSInfo       `json:"bios"`
	CollectedAt time.Time      `json:"collected_at"`
}

// CPUInfo represents CPU information
type CPUInfo struct {
	Model   string `json:"model"`
	Cores   int    `json:"cores"`
	Threads int    `json:"threads"`
}

// MemoryInfo represents memory information
type MemoryInfo struct {
	TotalGB int        `json:"total_gb"`
	DIMMs   []DIMMInfo `json:"dimms"`
}

// DIMMInfo represents individual memory module
type DIMMInfo struct {
	Slot     string `json:"slot"`
	SizeGB   int    `json:"size_gb"`
	Type     string `json:"type"`
	SpeedMHz int    `json:"speed_mhz"`
}

// DiskInfo represents disk information
type DiskInfo struct {
	Device string `json:"device"`
	SizeGB int    `json:"size_gb"`
	Type   string `json:"type"` // SSD, HDD, NVMe
	Model  string `json:"model"`
}

// NetworkInfo represents network interface information
type NetworkInfo struct {
	Interface string `json:"interface"`
	MAC       string `json:"mac"`
	Speed     string `json:"speed"`
}

// BIOSInfo represents BIOS information
type BIOSInfo struct {
	Vendor  string `json:"vendor"`
	Version string `json:"version"`
	Serial  string `json:"serial"`
}

// Approval represents task approval information
type Approval struct {
	Status     ApprovalStatus `json:"status"`
	ApprovedBy string         `json:"approved_by,omitempty"`
	ApprovedAt *time.Time     `json:"approved_at,omitempty"`
	RejectedBy string         `json:"rejected_by,omitempty"`
	RejectedAt *time.Time     `json:"rejected_at,omitempty"`
	Notes      string         `json:"notes,omitempty"`
	Reason     string         `json:"reason,omitempty"`
}

// TaskError represents error information
type TaskError struct {
	Code       string    `json:"code"`
	Message    string    `json:"message"`
	Details    string    `json:"details,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}

// Progress represents installation progress information
type Progress struct {
	TaskID     string                 `json:"task_id"`
	Stage      string                 `json:"stage"` // partitioning, downloading, installing, configuring
	Percentage int                    `json:"percentage"`
	Message    string                 `json:"message"`
	Details    map[string]interface{} `json:"details,omitempty"`
	UpdatedAt  time.Time              `json:"updated_at"`
}

// InstallStage represents different stages of installation
type InstallStage string

const (
	StagePartitioning InstallStage = "partitioning"
	StageDownloading  InstallStage = "downloading"
	StageInstalling   InstallStage = "installing"
	StageConfiguring  InstallStage = "configuring"
)

// CreateTaskRequest represents the request to create a task
type CreateTaskRequest struct {
	RegionID    string            `json:"region_id" binding:"required"`
	TargetMAC   string            `json:"target_mac" binding:"required"`
	OSType      string            `json:"os_type" binding:"required"`
	OSVersion   string            `json:"os_version" binding:"required"`
	DiskLayout  string            `json:"disk_layout"`
	NetworkConf string            `json:"network_config"`
	Tags        map[string]string `json:"tags"`
}

// ApprovalRequest represents approval/rejection request
type ApprovalRequest struct {
	Approved bool   `json:"approved"`
	Notes    string `json:"notes"`
	Reason   string `json:"reason"` // For rejection
}

// AgentReportRequest represents hardware report from agent
type AgentReportRequest struct {
	MACAddress string       `json:"mac_address" binding:"required"`
	Hardware   HardwareInfo `json:"hardware" binding:"required"`
}

// AgentStatusRequest represents status update from agent
type AgentStatusRequest struct {
	MACAddress string `json:"mac_address" binding:"required"`
	TaskID     string `json:"task_id" binding:"required"`
	Status     string `json:"status" binding:"required"`
	Progress   int    `json:"progress"`
	Message    string `json:"message"`
}

// AgentProgressRequest represents progress update from agent
type AgentProgressRequest struct {
	MACAddress string                 `json:"mac_address" binding:"required"`
	TaskID     string                 `json:"task_id" binding:"required"`
	Stage      string                 `json:"stage" binding:"required"`
	Percentage int                    `json:"percentage" binding:"required"`
	Message    string                 `json:"message"`
	Details    map[string]interface{} `json:"details,omitempty"`
}

// WebSocketMessage represents a message sent via WebSocket
type WebSocketMessage struct {
	Type    string      `json:"type"` // progress, status, hardware, task_update, task_created, etc.
	TaskID  string      `json:"task_id,omitempty"`
	IDC     string      `json:"idc,omitempty"`     // v3.0 field
	SN      string      `json:"sn,omitempty"`      // v3.0 field
	Payload interface{} `json:"payload,omitempty"`
	// For progress type
	Percentage int    `json:"percentage,omitempty"`
	Stage      string `json:"stage,omitempty"`
	Message    string `json:"message,omitempty"`
	// For status type
	Status string `json:"status,omitempty"`
	// For hardware type
	Hardware *HardwareInfo `json:"hardware,omitempty"`
}

// RegionalClientHeartbeat represents health information
type RegionalClientHeartbeat struct {
	RegionID      string            `json:"region_id"`
	Status        string            `json:"status"` // online, offline
	Services      map[string]string `json:"services"`
	EtcdConnected bool              `json:"etcd_connected"`
	LastHeartbeat time.Time         `json:"last_heartbeat"`
}

// ===== OPTIMIZED SCHEMA v3.0 Types =====

// ProgressStep represents a single step in the installation process (v3.0)
type ProgressStep struct {
	Step      string    `json:"step"`      // e.g., "hardware_collect", "partitioning", "downloading"
	Percent   int       `json:"percent"`   // 0-100
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message,omitempty"`
}

// StatusChange tracks status transitions (v3.0)
type StatusChange struct {
	Status    TaskStatus `json:"status"`
	Timestamp time.Time  `json:"timestamp"`
	Reason    string     `json:"reason,omitempty"`
}

// ServerEntry represents an entry in /os/{idc}/servers/{sn} (v3.0)
type ServerEntry struct {
	SN       string    `json:"sn"`
	Status   string    `json:"status"` // "pending", "unmatched", etc.
	MAC      string    `json:"mac,omitempty"`
	AddedAt  time.Time `json:"added_at"`
	Note     string    `json:"note,omitempty"`
}

// TaskV3 represents a merged task + state structure (OPTIMIZED SCHEMA v3.0)
// This combines the old separate "tasks" and "state" keys into a single atomic structure
type TaskV3 struct {
	TaskID    string     `json:"task_id"`
	SN        string     `json:"sn"`         // Serial number
	MAC       string     `json:"mac"`        // MAC address (for compatibility)
	IP        string     `json:"ip"`         // IP address for PXE boot
	Hostname  string     `json:"hostname"`   // Hostname for the server
	OSType    string     `json:"os_type"`
	OSVersion string     `json:"os_version"`
	DiskLayout string    `json:"disk_layout,omitempty"`
	NetworkConf string   `json:"network_config,omitempty"`

	// Merged status (replaces separate state key)
	Status        TaskStatus      `json:"status"`
	StatusHistory []StatusChange  `json:"status_history,omitempty"`

	// Progress steps with details
	Progress []ProgressStep `json:"progress"`

	// Logs array
	Logs []string `json:"logs"`

	// Approval info (embedded)
	Approval *Approval `json:"approval,omitempty"`

	// Metadata
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	CreatedBy string    `json:"created_by,omitempty"`
}

// IDCStats represents statistics for an IDC (stored in /os/global/stats/{idc}) (v3.0)
type IDCStats struct {
	IDC           string    `json:"idc"`
	TotalMachines int       `json:"total_machines"`
	Pending       int       `json:"pending"`
	Installing    int       `json:"installing"`
	Completed     int       `json:"completed"`
	Failed        int       `json:"failed"`
	LastUpdated   time.Time `json:"last_updated"`
}

// CreateTaskRequestV3 represents the request to create a task (v3.0)
type CreateTaskRequestV3 struct {
	IDC         string            `json:"idc" binding:"required"`
	SN          string            `json:"sn" binding:"required"`
	MAC         string            `json:"mac"`                          // Optional, for compatibility
	OSType      string            `json:"os_type" binding:"required"`
	OSVersion   string            `json:"os_version" binding:"required"`
	DiskLayout  string            `json:"disk_layout"`
	NetworkConf string            `json:"network_config"`
	Tags        map[string]string `json:"tags"`
}

// AgentReportRequestV3 represents hardware report from agent (v3.0)
type AgentReportRequestV3 struct {
	SN       string       `json:"sn" binding:"required"`
	MAC      string       `json:"mac_address" binding:"required"`
	Hardware HardwareInfo `json:"hardware" binding:"required"`
}

// AgentProgressRequestV3 represents progress update from agent (v3.0)
type AgentProgressRequestV3 struct {
	SN         string                 `json:"sn" binding:"required"`
	MAC        string                 `json:"mac_address" binding:"required"`
	TaskID     string                 `json:"task_id" binding:"required"`
	Step       string                 `json:"step" binding:"required"`
	Percent    int                    `json:"percent" binding:"required"`
	Message    string                 `json:"message"`
	Details    map[string]interface{} `json:"details,omitempty"`
}
