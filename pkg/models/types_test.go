package models

import (
	"testing"
	"time"
)

func TestTaskStatus(t *testing.T) {
	tests := []struct {
		name   string
		status TaskStatus
		want   string
	}{
		{"pending", TaskStatusPending, "pending"},
		{"ready", TaskStatusReady, "ready"},
		{"pending_approval", TaskStatusPendingApproval, "pending_approval"},
		{"completed", TaskStatusCompleted, "completed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.status) != tt.want {
				t.Errorf("TaskStatus = %v, want %v", tt.status, tt.want)
			}
		})
	}
}

func TestTask(t *testing.T) {
	now := time.Now()

	task := &Task{
		ID:          "test-id",
		RegionID:    "dc1",
		TargetMAC:   "00:1a:2b:3c:4d:5e",
		OSType:      "ubuntu",
		OSVersion:   "22.04",
		DiskLayout:  "auto",
		NetworkConf: "dhcp",
		CreatedAt:   now,
		CreatedBy:   "test@example.com",
		Status:      TaskStatusPending,
		UpdatedAt:   now,
	}

	if task.ID != "test-id" {
		t.Errorf("Task.ID = %v, want test-id", task.ID)
	}

	if task.Status != TaskStatusPending {
		t.Errorf("Task.Status = %v, want %v", task.Status, TaskStatusPending)
	}
}

func TestApproval(t *testing.T) {
	now := time.Now()

	approval := &Approval{
		Status:     ApprovalStatusApproved,
		ApprovedBy: "admin@example.com",
		ApprovedAt: &now,
		Notes:      "Hardware verified",
	}

	if approval.Status != ApprovalStatusApproved {
		t.Errorf("Approval.Status = %v, want %v", approval.Status, ApprovalStatusApproved)
	}

	if approval.ApprovedBy != "admin@example.com" {
		t.Errorf("Approval.ApprovedBy = %v, want admin@example.com", approval.ApprovedBy)
	}
}

func TestHardwareInfo(t *testing.T) {
	hwInfo := &HardwareInfo{
		MACAddress: "00:1a:2b:3c:4d:5e",
		CPU: CPUInfo{
			Model:   "Intel Xeon",
			Cores:   28,
			Threads: 56,
		},
		Memory: MemoryInfo{
			TotalGB: 256,
		},
		Disks: []DiskInfo{
			{
				Device: "/dev/sda",
				SizeGB: 480,
				Type:   "SSD",
				Model:  "Samsung 860 PRO",
			},
		},
	}

	if hwInfo.CPU.Cores != 28 {
		t.Errorf("HardwareInfo.CPU.Cores = %v, want 28", hwInfo.CPU.Cores)
	}

	if hwInfo.Memory.TotalGB != 256 {
		t.Errorf("HardwareInfo.Memory.TotalGB = %v, want 256", hwInfo.Memory.TotalGB)
	}

	if len(hwInfo.Disks) != 1 {
		t.Errorf("len(HardwareInfo.Disks) = %v, want 1", len(hwInfo.Disks))
	}

	if hwInfo.Disks[0].Type != "SSD" {
		t.Errorf("HardwareInfo.Disks[0].Type = %v, want SSD", hwInfo.Disks[0].Type)
	}
}
