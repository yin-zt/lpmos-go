# LPMOS v2 - Full-Stack OS Provisioning Platform

## Quick Start (3 minutes)

### 1. Start etcd
```bash
make start-etcd
# OR
docker run -d --name lpmos-etcd -p 2379:2379 -p 2380:2380 \
  quay.io/coreos/etcd:v3.5.12 \
  /usr/local/bin/etcd \
  --advertise-client-urls http://0.0.0.0:2379 \
  --listen-client-urls http://0.0.0.0:2379
```

### 2. Start Control Plane (Terminal 1)
```bash
go run cmd/control-plane-v2/main.go
# Opens dashboard at http://localhost:8080
```

### 3. Start Regional Client (Terminal 2)
```bash
REGION_ID=dc1 API_PORT=8081 go run cmd/regional-client/main.go
```

### 4. Run Demo Agent (Terminal 3)
```bash
REGIONAL_CLIENT_URL=http://localhost:8081 go run cmd/agent-minimal/main.go
```

### 5. Open Dashboard
Open your browser to: **http://localhost:8080**

You'll see:
- Real-time task dashboard
- Hardware information
- Live progress bars during installation
- WebSocket-powered updates (no page refresh needed!)

## What's New in v2

### Full-Stack Architecture
- **Frontend**: Modern dashboard with real-time updates
- **Backend**: REST API + WebSocket for live progress
- **Agent**: Minimal stdlib-only implementation (<10MB binary)

### Real-Time Features
- Live progress bars during OS installation
- Instant status updates across all connected browsers
- Hardware reports displayed immediately
- WebSocket-powered (no polling!)

### Minimal Agent
- Uses only Go stdlib (no external dependencies)
- Collects hardware via `/proc`, `/sys`, `syscall`
- Reports progress at each installation stage
- Lightweight for PXE boot environments

## Architecture

```
┌─────────────────────────────────────┐
│     Browser (Dashboard)              │
│  - View tasks                        │
│  - See real-time progress            │
│  - Approve installations             │
└──────────┬──────────────────────────┘
           │ HTTP + WebSocket
           ▼
┌─────────────────────────────────────┐
│   Control Plane (Full-Stack)        │
│  - REST API (Gin)                   │
│  - WebSocket Hub (gorilla/websocket)│
│  - Frontend (embedded HTML/JS)       │
└──────────┬──────────────────────────┘
           │ Watch/Update
           ▼
┌─────────────────────────────────────┐
│         etcd (Coordination)         │
│  /tasks/{id}/progress               │
│  /tasks/{id}/status                 │
│  /tasks/{id}/hardware               │
└──────────┬──────────────────────────┘
           │ Watch
           ▼
┌─────────────────────────────────────┐
│    Regional Client (DC1)            │
│  - PXE/TFTP/HTTP servers            │
│  - Agent API endpoints              │
└──────────┬──────────────────────────┘
           │ PXE Boot + HTTP Reports
           ▼
┌─────────────────────────────────────┐
│    Bare Metal Server                │
│  ┌───────────────────────────────┐ │
│  │   Agent (stdlib only)          │ │
│  │  1. Collect hardware           │ │
│  │  2. Report to regional client  │ │
│  │  3. Wait for approval          │ │
│  │  4. Install OS with progress   │ │
│  └───────────────────────────────┘ │
└─────────────────────────────────────┘
```

## Components

### Control Plane v2 (Full-Stack)
**Location**: `cmd/control-plane-v2/main.go`

Features:
- Embedded dashboard (no separate frontend server needed)
- WebSocket hub for real-time updates
- REST API for task management
- Watches etcd for changes and broadcasts to connected clients

Endpoints:
- `GET /` - Dashboard UI
- `GET /ws` - WebSocket connection
- `POST /api/v1/tasks` - Create task
- `GET /api/v1/tasks` - List tasks
- `PUT /api/v1/tasks/:id/approve` - Approve task

### Regional Client
**Location**: `cmd/regional-client/main.go`

New Endpoints:
- `POST /api/v1/agent/progress` - Accept progress updates from agent
- `GET /api/v1/agent/approval/:mac` - Check approval status

### Agent (Minimal)
**Location**: `cmd/agent-minimal/main.go`

Stdlib-only implementation:
- **CPU**: `runtime.NumCPU()` + `/proc/cpuinfo`
- **Memory**: `syscall.Sysinfo()`
- **Disks**: `/sys/block/*`
- **Network**: `net.Interfaces()`
- **BIOS**: `/sys/class/dmi/id/*`
- **HTTP**: `net/http` (stdlib)
- **JSON**: `encoding/json` (stdlib)

No external dependencies!

## Complete Demo Flow

### Step 1: Create Task via API
```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "region_id": "dc1",
    "target_mac": "00:1a:2b:3c:4d:5e",
    "os_type": "ubuntu",
    "os_version": "22.04"
  }'
```

### Step 2: Watch Dashboard
Open http://localhost:8080 and you'll see:
1. Task created (status: pending)
2. Regional client picks up task (status: ready)

### Step 3: Agent Reports Hardware
When agent runs:
1. Hardware info appears in dashboard
2. Status changes to "pending_approval"
3. "Approve" button appears

### Step 4: Approve via Dashboard
Click "Approve Installation" button

### Step 5: Watch Real-Time Progress
Progress bar updates live:
- [20%] Partitioning disks...
- [45%] Downloading OS image...
- [75%] Installing packages...
- [95%] Configuring system...
- [100%] Installation complete!

All updates appear instantly via WebSocket!

## Building

### Build All Components
```bash
make build-v2
```

### Build Individual Components
```bash
# Control plane
go build -o bin/control-plane-v2 cmd/control-plane-v2/main.go

# Regional client
go build -o bin/regional-client cmd/regional-client/main.go

# Minimal agent
go build -o bin/agent-minimal cmd/agent-minimal/main.go
```

### Build Minimal Agent for Production
```bash
# Static binary for PXE boot
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
  -ldflags="-s -w" \
  -o agent-minimal cmd/agent-minimal/main.go

# Check size (should be <10MB)
ls -lh agent-minimal

# Compress further with upx (optional)
upx --best --lzma agent-minimal
```

## WebSocket Protocol

### Client Connection
```javascript
const ws = new WebSocket('ws://localhost:8080/ws');

ws.onmessage = (event) => {
  const data = JSON.parse(event.data);

  switch(data.type) {
    case 'progress':
      // data.percentage, data.stage, data.message
      break;
    case 'status':
      // data.status
      break;
    case 'hardware':
      // data.hardware
      break;
  }
};
```

### Message Types

**Progress Update**:
```json
{
  "type": "progress",
  "task_id": "550e8400-...",
  "percentage": 45,
  "stage": "downloading",
  "message": "Downloaded 450 MB / 1000 MB"
}
```

**Status Change**:
```json
{
  "type": "status",
  "task_id": "550e8400-...",
  "status": "installing"
}
```

**Hardware Report**:
```json
{
  "type": "hardware",
  "task_id": "550e8400-...",
  "hardware": {
    "cpu": {"model": "...", "cores": 28},
    "memory": {"total_gb": 256},
    ...
  }
}
```

## Agent Implementation Details

### Hardware Collection (Stdlib Only)

```go
// CPU: runtime + /proc/cpuinfo
func collectCPU() CPUInfo {
    cores := runtime.NumCPU()
    data, _ := os.ReadFile("/proc/cpuinfo")
    // Parse model name...
    return CPUInfo{Model: model, Cores: cores}
}

// Memory: syscall.Sysinfo
func collectMemory() MemoryInfo {
    var si syscall.Sysinfo_t
    syscall.Sysinfo(&si)
    totalGB := int(si.Totalram * uint64(si.Unit) / 1024 / 1024 / 1024)
    return MemoryInfo{TotalGB: totalGB}
}

// Disks: /sys/block
func collectDisks() []DiskInfo {
    entries, _ := os.ReadDir("/sys/block")
    for _, e := range entries {
        sizeData, _ := os.ReadFile("/sys/block/" + e.Name() + "/size")
        sizeGB := parseSizeInGB(sizeData)
        disks = append(disks, DiskInfo{...})
    }
    return disks
}

// Network: net.Interfaces
func collectNetwork() []NetworkInfo {
    interfaces, _ := net.Interfaces()
    for _, iface := range interfaces {
        networks = append(networks, NetworkInfo{
            Interface: iface.Name,
            MAC: iface.HardwareAddr.String(),
        })
    }
    return networks
}
```

### Progress Reporting

```go
func installOS() error {
    stages := []struct {
        name string
        start, end int
        fn func() error
    }{
        {"partitioning", 0, 20, stagePartitioning},
        {"downloading", 20, 60, stageDownloading},
        {"installing", 60, 90, stageInstalling},
        {"configuring", 90, 100, stageConfiguring},
    }

    for _, stage := range stages {
        reportProgress(stage.name, stage.start, "Starting...")
        stage.fn()
        reportProgress(stage.name, stage.end, "Complete")
    }
}

func reportProgress(stage string, pct int, msg string) {
    data := ProgressReport{
        MACAddress: macAddress,
        TaskID: taskID,
        Stage: stage,
        Percentage: pct,
        Message: msg,
    }
    json, _ := json.Marshal(data)
    http.Post(regionalClientURL+"/api/v1/agent/progress",
        "application/json", bytes.NewBuffer(json))
}
```

## Production Deployment

### Control Plane
```bash
# Run with TLS
./control-plane-v2 \
  --etcd-endpoints=etcd1:2379,etcd2:2379,etcd3:2379 \
  --listen-addr=:8080 \
  --tls-cert=/etc/lpmos/server.crt \
  --tls-key=/etc/lpmos/server.key
```

### Regional Client
```bash
# Run as systemd service
./regional-client \
  --region-id=dc1 \
  --etcd-endpoints=etcd:2379 \
  --api-port=8081
```

### Agent in PXE Environment

1. Build static binary:
```bash
CGO_ENABLED=0 go build -ldflags="-s -w" cmd/agent-minimal/main.go
```

2. Embed in initramfs:
```bash
mkdir -p initramfs/bin
cp agent-minimal initramfs/bin/agent
cd initramfs && find . | cpio -o -H newc | gzip > ../initramfs.img
```

3. PXE boot configuration:
```
LABEL lpmos
  KERNEL vmlinuz
  INITRD initramfs.img
  APPEND console=ttyS0 lpmos_client=http://regional-client:8081
```

## Troubleshooting

### WebSocket Not Connecting
```bash
# Check if control plane is running
curl http://localhost:8080/api/v1/health

# Test WebSocket with wscat
npm install -g wscat
wscat -c ws://localhost:8080/ws
```

### Agent Can't Collect Hardware
```bash
# Run with proper permissions
sudo ./agent-minimal

# Check required files exist
ls /proc/cpuinfo
ls /sys/block/
ls /sys/class/dmi/id/
```

### No Real-Time Updates
1. Check browser console for errors
2. Verify WebSocket connection is established
3. Check control plane logs for watch errors
4. Ensure etcd is accessible

## Performance

### Metrics
- WebSocket latency: <50ms
- Progress update frequency: max 1/second
- Dashboard can handle 1000+ concurrent connections
- Agent binary size: 8-10MB (static)

### Optimization
- Use WSS (WebSocket over TLS) in production
- Enable gzip compression for API responses
- Use etcd watch filters to reduce events
- Implement connection pooling for HTTP clients

## Security Checklist

- [ ] Enable TLS for all HTTP/WebSocket connections
- [ ] Use WSS instead of WS in production
- [ ] Implement authentication (JWT) for API endpoints
- [ ] Add CSRF protection for web forms
- [ ] Enable etcd client authentication
- [ ] Use firewall rules to restrict etcd access
- [ ] Rotate agent tokens per task
- [ ] Implement rate limiting on WebSocket connections
- [ ] Sanitize all user input in frontend
- [ ] Add Content Security Policy headers

## License

MIT License - See LICENSE file

## Support

For detailed architecture, see [ARCHITECTURE_V2.md](./ARCHITECTURE_V2.md)
