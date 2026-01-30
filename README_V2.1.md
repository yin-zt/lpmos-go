# LPMOS v2.1 - Region-Based OS Provisioning (Fix for MAC Matching)

## Quick Start

### 1. Start etcd
```bash
make start-etcd
```

### 2. Start Control Plane (Terminal 1)
```bash
go run cmd/control-plane-v2/main.go
# Dashboard: http://localhost:8080
# Add Task: http://localhost:8080/tasks/create
```

### 3. Start Regional Client - dc1 (Terminal 2)
```bash
go run cmd/regional-client-v2/main.go --label=dc1 --api-port=8081
# Or with environment variable:
# REGION_LABEL=dc1 API_PORT=8081 go run cmd/regional-client-v2/main.go
```

### 4. (Optional) Start Regional Client - dc2 (Terminal 3)
```bash
go run cmd/regional-client-v2/main.go --label=dc2 --api-port=8082
```

### 5. Run Agent (Terminal 4)
```bash
go run cmd/agent-minimal/main.go --regional-url=http://localhost:8081
# For dc2:
# go run cmd/agent-minimal/main.go --regional-url=http://localhost:8082
```

## What Was Fixed in v2.1

### ❌ Error Before (v2.0)
```
2026/01/30 10:00:09 [dc1] Received hardware report from agent: fe:b7:02:c0:95:e0
2026/01/30 10:00:09 Failed to find task for MAC fe:b7:02:c0:95:e0: no task found
```

### ✅ Fixes in v2.1

1. **Region-Based etcd Structure**
   - **Before**: `/lpmos/tasks/{task_id}/...`
   - **After**: `/os/install/task/{region}/{task_id}/...`
   - Tasks are now organized by region

2. **Regional Client Label**
   - **New**: `--label` flag (e.g., `--label=dc1`)
   - Regional client only watches tasks in its region
   - Logs include region label: `[dc1] Message...`

3. **Fixed MAC Matching**
   - Regional client searches etcd `/os/install/task/{region}/*/metadata`
   - Compares `target_mac` field (case-insensitive)
   - If match found: updates hardware info and sets status to `pending_approval`
   - If NO match: logs error, stores unmatched report, returns HTTP 404 to agent

4. **Agent Retry Logic**
   - Agent retries up to 10 times if task not found
   - Waits for `retry_after` seconds between attempts
   - Useful when task is created slightly after agent boots

5. **Unmatched Reports Storage**
   - Stored in etcd: `/os/unmatched_reports/{region}/{timestamp}-{mac}`
   - Control plane can monitor and alert on unmatched reports
   - Operators can manually match later

6. **Agent Regional URL Flag**
   - **New**: `--regional-url` flag
   - Agent communicates ONLY with regional client (not etcd)
   - Example: `--regional-url=http://regional-dc1:8081`

7. **Frontend Add Task Page**
   - New page: `/tasks/create`
   - Region selector dropdown
   - Validates MAC address format
   - Creates task in correct region path

## Complete Flow (Fixed)

### Step 1: Create Task via Web UI

1. Open browser: `http://localhost:8080/tasks/create`
2. Fill form:
   - Region: `dc1`
   - MAC: `00:1a:2b:3c:4d:5e`
   - OS: `ubuntu 22.04`
3. Click "Create Task"

**etcd writes**:
```
/os/install/task/dc1/550e8400-.../metadata = {region_id: "dc1", target_mac: "00:1a:2b:3c:4d:5e", ...}
/os/install/task/dc1/550e8400-.../status = "pending"
```

### Step 2: Regional Client Watches

Regional client (label=dc1) watches `/os/install/task/dc1/`

**Log output**:
```
[dc1] New task assigned: 550e8400-...
[dc1] Task 550e8400-... ready for PXE boot (target MAC: 00:1a:2b:3c:4d:5e)
```

### Step 3: Agent Reports Hardware

Agent boots, collects hardware, sends HTTP POST:

```bash
curl -X POST http://localhost:8081/api/report \
  -H "Content-Type: application/json" \
  -d '{
    "mac_address": "00:1a:2b:3c:4d:5e",
    "hardware": {"cpu": ..., "memory": ...}
  }'
```

**Regional client logic**:
1. Searches etcd `/os/install/task/dc1/*/metadata` for `target_mac = "00:1a:2b:3c:4d:5e"`
2. **Match found!**
3. Updates `/os/install/task/dc1/550e8400-.../hardware`
4. Updates `/os/install/task/dc1/550e8400-.../status = "pending_approval"`
5. Returns `{status: "ok", task_id: "550e8400-..."}`

**Log output**:
```
[dc1] Received hardware report from agent: 00:1a:2b:3c:4d:5e
[dc1] Found matching task 550e8400-... for MAC 00:1a:2b:3c:4d:5e
[dc1] Hardware report stored for task 550e8400-...
```

### Step 4: User Approves

1. Dashboard shows task with hardware info
2. Click "Approve Installation"

**etcd write**:
```
/os/install/task/dc1/550e8400-.../approval = {status: "approved", ...}
```

### Step 5: Installation with Progress

Agent polls `/api/approval/{mac}`, gets approval, then installs:

```bash
# Agent reports progress
curl -X POST http://localhost:8081/api/progress \
  -H "Content-Type: application/json" \
  -d '{
    "mac_address": "00:1a:2b:3c:4d:5e",
    "task_id": "550e8400-...",
    "stage": "downloading",
    "percentage": 45,
    "message": "Downloaded 450 MB / 1000 MB"
  }'
```

**etcd updates**:
```
/os/install/task/dc1/550e8400-.../progress = {stage: "downloading", percentage: 45, ...}
/os/install/task/dc1/550e8400-.../status = "installing"
```

**Dashboard**: Progress bar updates in real-time via WebSocket!

## Error Handling: Unmatched MAC

### Scenario: Agent reports, but NO task exists

```bash
curl -X POST http://localhost:8081/api/report \
  -d '{"mac_address": "fe:b7:02:c0:95:e0", ...}'
```

**Regional client response**:
```json
HTTP 404 Not Found
{
  "status": "error",
  "message": "No task found for MAC fe:b7:02:c0:95:e0 in region dc1",
  "retry_after": 10
}
```

**Regional client actions**:
1. Log error: `[dc1] Failed to find task for MAC fe:b7:02:c0:95:e0`
2. Store unmatched report in etcd:
   ```
   /os/unmatched_reports/dc1/20260130100009-fe:b7:02:c0:95:e0 = {
     mac_address: "fe:b7:02:c0:95:e0",
     region: "dc1",
     hardware: {...},
     received_at: "2026-01-30T10:00:09Z",
     error: "No task found for this MAC in region dc1"
   }
   ```

**Agent behavior**:
1. Receives HTTP 404
2. Waits 10 seconds (from `retry_after`)
3. Retries POST /api/report (up to 10 attempts)
4. If still failing after 10 attempts, enters error state

**Control plane monitoring**:
- Dashboard can show count of unmatched reports per region
- Alert operators if threshold exceeded
- Provide UI to view/match unmatched reports

## Configuration Examples

### Regional Client
```bash
# Using flags
./regional-client-v2 \
  --label=dc1 \
  --etcd-endpoints=etcd1:2379,etcd2:2379 \
  --api-port=8081

# Using environment variables
REGION_LABEL=dc1 \
ETCD_ENDPOINTS=etcd1:2379 \
API_PORT=8081 \
./regional-client-v2
```

### Agent
```bash
# Using flags
./agent-minimal \
  --regional-url=http://regional-dc1:8081

# Using environment variable
REGIONAL_CLIENT_URL=http://regional-dc1:8081 \
./agent-minimal
```

## etcd Key Structure

```
/os/
├── install/
│   └── task/
│       ├── dc1/                    # Region: Data Center 1
│       │   └── {task_id}/
│       │       ├── metadata        # Task config
│       │       ├── status          # Current status
│       │       ├── hardware        # Hardware report
│       │       ├── progress        # Install progress
│       │       └── approval        # Approval status
│       └── dc2/                    # Region: Data Center 2
│           └── ...
├── region/
│   ├── dc1/
│   │   ├── heartbeat               # Heartbeat (TTL: 30s)
│   │   └── status                  # online/offline
│   └── dc2/
│       └── ...
└── unmatched_reports/              # ← NEW: Unmatched hardware reports
    ├── dc1/
    │   └── 20260130100009-fe:b7:02:c0:95:e0
    └── dc2/
        └── ...
```

## Testing the Fix

### Test 1: Normal Flow (MAC Matches)

```bash
# Terminal 1: Control plane
go run cmd/control-plane-v2/main.go

# Terminal 2: Regional client
go run cmd/regional-client-v2/main.go --label=dc1

# Terminal 3: Create task
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "region_id": "dc1",
    "target_mac": "00:1a:2b:3c:4d:5e",
    "os_type": "ubuntu",
    "os_version": "22.04"
  }'

# Terminal 4: Run agent
go run cmd/agent-minimal/main.go --regional-url=http://localhost:8081

# Expected: Agent successfully reports, task found, status → pending_approval
```

### Test 2: Unmatched MAC (Error Handling)

```bash
# Don't create a task, just run agent directly
go run cmd/agent-minimal/main.go --regional-url=http://localhost:8081

# Expected:
# - Agent retries 10 times
# - Regional client logs: [dc1] Failed to find task for MAC...
# - Unmatched report stored in etcd
# - Agent eventually gives up with error message
```

## Troubleshooting

### Problem: "No task found for MAC"

**Check**:
1. Is the regional client running with correct label?
   ```bash
   curl http://localhost:8081/api/health
   # Should show: "region_label": "dc1"
   ```

2. Is the task created in the correct region?
   ```bash
   etcdctl get --prefix /os/install/task/dc1/
   ```

3. Does the MAC address match exactly?
   ```bash
   # In etcd metadata, check target_mac field
   # Agent report uses hardware MAC
   # They must match (case-insensitive)
   ```

4. Check unmatched reports:
   ```bash
   etcdctl get --prefix /os/unmatched_reports/dc1/
   ```

### Problem: Agent can't connect to regional client

**Check**:
1. Is regional client running?
   ```bash
   curl http://localhost:8081/api/health
   ```

2. Is `--regional-url` correct?
   ```bash
   # Agent should use: http://localhost:8081
   # NOT: http://localhost:8080 (that's control plane)
   ```

3. Check agent logs for connection errors

## Building

```bash
# Build all v2.1 components
go build -o bin/control-plane-v2 ./cmd/control-plane-v2
go build -o bin/regional-client-v2 ./cmd/regional-client-v2
go build -o bin/agent-minimal ./cmd/agent-minimal

# Or use Makefile
make build-v2
```

## Production Deployment

### Multi-Region Setup

```
                Control Plane (Cloud)
                    port: 8080
                        │
        ┌───────────────┴───────────────┐
        │                               │
    Regional DC1                   Regional DC2
    label: dc1                     label: dc2
    port: 8081                     port: 8082
        │                               │
    Servers: 100                   Servers: 200
    MACs: 00:1a:..                 MACs: 00:2b:..
```

### etcd Data Distribution

```
/os/install/task/
├── dc1/                        # 100 tasks
│   ├── task-001/
│   ├── task-002/
│   └── ...
└── dc2/                        # 200 tasks
    ├── task-101/
    ├── task-102/
    └── ...
```

Each regional client ONLY watches its own region → efficient!

## Summary of Changes

| Component | v2.0 (Before) | v2.1 (After/Fixed) |
|-----------|---------------|-------------------|
| **etcd Path** | `/lpmos/tasks/{id}` | `/os/install/task/{region}/{id}` |
| **Regional Client** | No label | `--label=dc1` flag required |
| **Task Matching** | ❌ Failed | ✅ Fixed: searches by MAC in region |
| **Agent URL** | Env var only | `--regional-url` flag |
| **Error Handling** | Basic | ✅ Retry + unmatched reports storage |
| **Frontend** | Dashboard only | ✅ Add Task page with region selector |
| **Logs** | Generic | ✅ Include region label: `[dc1] ...` |

## Next Steps

1. **Monitor unmatched reports**:
   - Add dashboard widget for unmatched count
   - Set up alerts for threshold
   - Provide UI to manually match

2. **Auto-matching**:
   - If agent reports before task created, queue report
   - When task created, check queue for matching MAC
   - Auto-link and proceed

3. **Multi-region dashboard**:
   - Filter tasks by region
   - Show region health status
   - Per-region statistics

## Documentation

- **Architecture**: See [ARCHITECTURE_V2.1.md](./ARCHITECTURE_V2.1.md)
- **Original v2**: See [README_V2.md](./README_V2.md)
- **Delivery Summary**: See [DELIVERY_SUMMARY.md](./DELIVERY_SUMMARY.md)
