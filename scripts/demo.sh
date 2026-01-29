#!/bin/bash

# LPMOS Demo Script
# This script demonstrates the complete workflow of the LPMOS system

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
CONTROL_PLANE_URL="http://localhost:8080"
REGIONAL_CLIENT_URL="http://localhost:8081"
REGION_ID="dc1"
TARGET_MAC="00:1a:2b:3c:4d:5e"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}LPMOS - Complete Workflow Demo${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# Check if services are running
check_service() {
    local url=$1
    local name=$2

    if curl -s -f "${url}/api/v1/health" > /dev/null 2>&1; then
        echo -e "${GREEN}✓${NC} ${name} is running"
        return 0
    else
        echo -e "${RED}✗${NC} ${name} is not running at ${url}"
        return 1
    fi
}

echo -e "${YELLOW}Step 1: Checking services...${NC}"
check_service "${CONTROL_PLANE_URL}" "Control Plane" || exit 1
check_service "${REGIONAL_CLIENT_URL}" "Regional Client (${REGION_ID})" || exit 1
echo ""

# Step 2: Create installation task
echo -e "${YELLOW}Step 2: Creating installation task...${NC}"
TASK_RESPONSE=$(curl -s -X POST "${CONTROL_PLANE_URL}/api/v1/tasks" \
  -H "Content-Type: application/json" \
  -d '{
    "region_id": "'"${REGION_ID}"'",
    "target_mac": "'"${TARGET_MAC}"'",
    "os_type": "ubuntu",
    "os_version": "22.04",
    "disk_layout": "auto",
    "network_config": "dhcp",
    "tags": {
      "environment": "production",
      "purpose": "demo-server"
    }
  }')

TASK_ID=$(echo "${TASK_RESPONSE}" | grep -o '"task_id":"[^"]*"' | cut -d'"' -f4)

if [ -z "${TASK_ID}" ]; then
    echo -e "${RED}Failed to create task${NC}"
    echo "Response: ${TASK_RESPONSE}"
    exit 1
fi

echo -e "${GREEN}✓${NC} Task created: ${TASK_ID}"
echo "   Status: pending"
echo ""

# Wait for regional client to pick up the task
sleep 2

# Step 3: Check task status
echo -e "${YELLOW}Step 3: Checking task status...${NC}"
TASK_STATUS=$(curl -s "${CONTROL_PLANE_URL}/api/v1/tasks/${TASK_ID}" | grep -o '"status":"[^"]*"' | cut -d'"' -f4)
echo -e "${GREEN}✓${NC} Task status: ${TASK_STATUS}"
echo ""

# Step 4: Simulate agent hardware report
echo -e "${YELLOW}Step 4: Simulating agent hardware report...${NC}"
REPORT_RESPONSE=$(curl -s -X POST "${REGIONAL_CLIENT_URL}/api/v1/agent/report" \
  -H "Content-Type: application/json" \
  -d '{
    "mac_address": "'"${TARGET_MAC}"'",
    "hardware": {
      "mac_address": "'"${TARGET_MAC}"'",
      "cpu": {
        "model": "Intel Xeon E5-2680 v4",
        "cores": 28,
        "threads": 56
      },
      "memory": {
        "total_gb": 256,
        "dimms": [
          {
            "slot": "A1",
            "size_gb": 16,
            "type": "DDR4",
            "speed_mhz": 2400
          }
        ]
      },
      "disks": [
        {
          "device": "/dev/sda",
          "size_gb": 480,
          "type": "SSD",
          "model": "Samsung 860 PRO"
        },
        {
          "device": "/dev/sdb",
          "size_gb": 2000,
          "type": "HDD",
          "model": "Seagate ST2000"
        }
      ],
      "network": [
        {
          "interface": "eth0",
          "mac": "'"${TARGET_MAC}"'",
          "speed": "10Gbps"
        },
        {
          "interface": "eth1",
          "mac": "00:1a:2b:3c:4d:5f",
          "speed": "10Gbps"
        }
      ],
      "bios": {
        "vendor": "Dell Inc.",
        "version": "2.10.0",
        "serial": "SN123456789"
      },
      "collected_at": "'"$(date -u +%Y-%m-%dT%H:%M:%SZ)"'"
    }
  }')

echo -e "${GREEN}✓${NC} Hardware report submitted"
echo ""

# Wait for status update
sleep 2

# Step 5: View hardware information
echo -e "${YELLOW}Step 5: Viewing collected hardware information...${NC}"
TASK_DETAILS=$(curl -s "${CONTROL_PLANE_URL}/api/v1/tasks/${TASK_ID}")
echo "${TASK_DETAILS}" | grep -o '"cpu":{[^}]*}' | sed 's/^/   /'
echo "${TASK_DETAILS}" | grep -o '"memory":{[^}]*}' | sed 's/^/   /'
echo ""

# Step 6: Check approval status
echo -e "${YELLOW}Step 6: Checking approval status...${NC}"
TASK_STATUS=$(curl -s "${CONTROL_PLANE_URL}/api/v1/tasks/${TASK_ID}" | grep -o '"status":"[^"]*"' | cut -d'"' -f4)
echo -e "${GREEN}✓${NC} Task status: ${TASK_STATUS}"
echo ""

# Step 7: Approve the task
echo -e "${YELLOW}Step 7: Approving installation task...${NC}"
APPROVAL_RESPONSE=$(curl -s -X PUT "${CONTROL_PLANE_URL}/api/v1/tasks/${TASK_ID}/approve" \
  -H "Content-Type: application/json" \
  -d '{
    "approved": true,
    "notes": "Hardware verified via demo script, proceeding with installation"
  }')

echo -e "${GREEN}✓${NC} Task approved"
echo ""

# Wait for installation to start
sleep 2

# Step 8: Monitor installation progress
echo -e "${YELLOW}Step 8: Monitoring installation...${NC}"
for i in {1..5}; do
    TASK_STATUS=$(curl -s "${CONTROL_PLANE_URL}/api/v1/tasks/${TASK_ID}" | grep -o '"status":"[^"]*"' | cut -d'"' -f4)
    echo -e "   Status: ${TASK_STATUS}"

    if [ "${TASK_STATUS}" == "completed" ]; then
        break
    fi

    sleep 2
done
echo ""

# Step 9: Final status
echo -e "${YELLOW}Step 9: Final task status...${NC}"
FINAL_STATUS=$(curl -s "${CONTROL_PLANE_URL}/api/v1/tasks/${TASK_ID}")
FINAL_STATE=$(echo "${FINAL_STATUS}" | grep -o '"status":"[^"]*"' | cut -d'"' -f4)

if [ "${FINAL_STATE}" == "completed" ]; then
    echo -e "${GREEN}✓${NC} Installation completed successfully!"
else
    echo -e "${YELLOW}⚠${NC} Current status: ${FINAL_STATE}"
fi
echo ""

# Summary
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}Demo Summary${NC}"
echo -e "${BLUE}========================================${NC}"
echo -e "Task ID:       ${TASK_ID}"
echo -e "Region:        ${REGION_ID}"
echo -e "Target MAC:    ${TARGET_MAC}"
echo -e "Final Status:  ${FINAL_STATE}"
echo ""
echo -e "${GREEN}Demo completed successfully!${NC}"
echo ""
echo "To view full task details:"
echo "  curl ${CONTROL_PLANE_URL}/api/v1/tasks/${TASK_ID} | jq"
echo ""
echo "To list all tasks:"
echo "  curl ${CONTROL_PLANE_URL}/api/v1/tasks | jq"
echo ""
