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
	Code      string    `json:"code"`
	Message   string    `json:"message"`
	Details   string    `json:"details,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}

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

// RegionalClientHeartbeat represents health information
type RegionalClientHeartbeat struct {
	RegionID      string            `json:"region_id"`
	Status        string            `json:"status"` // online, offline
	Services      map[string]string `json:"services"`
	EtcdConnected bool              `json:"etcd_connected"`
	LastHeartbeat time.Time         `json:"last_heartbeat"`
}
