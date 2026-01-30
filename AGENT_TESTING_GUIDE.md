# LPMOS Agent 重构完成 - 测试指南

## 🎉 重构完成概览

Agent 已经基于 os-agent 项目重构，具有完整的硬件采集、任务轮询、进度上报功能。

## ✨ 新增功能

### 1. 增强的硬件采集
- ✅ **系统信息**: Company (制造商), Product (产品名), ModelName (型号)
- ✅ **虚拟机检测**: 自动识别 VMware, VirtualBox, KVM, QEMU, Xen, Parallels
- ✅ **序列号采集**: 多源采集 (DMI, dmidecode, system_profiler)，带回退机制
- ✅ **跨平台支持**: Linux 和 macOS 完整支持

### 2. 轮询式任务接收
- ✅ **定期轮询**: 每 10 秒检查一次任务状态
- ✅ **超时保护**: 最多轮询 60 次（10 分钟）
- ✅ **状态检查**: 等待任务状态变为 "approved"
- ✅ **优雅重试**: 网络错误自动重试

### 3. 精细化进度上报
- ✅ **多阶段上报**: 10%, 15%, 20%, 30%, 40%, 50%, 60%, 70%, 80%, 90%, 100%
- ✅ **详细消息**: 每个阶段都有清晰的状态描述
- ✅ **实时通信**: 通过 Regional Client 实时上报到 Control Plane

### 4. 清晰的工作流状态机
```
Stage 1 (10%)  → 采集硬件信息
Stage 2 (15%)  → 上报到 Regional Client
Stage 3 (20%)  → 轮询等待任务分配
Stage 4 (30%)  → 接收任务详情
Stage 5 (40-100%) → 执行安装流程
```

## 📋 完整测试流程

### 准备工作

确保 etcd 正在运行：
```bash
make demo
```

### Terminal 1: Control Plane (管理后台)

```bash
make run
```

访问 http://localhost:8080 查看 Web 界面

### Terminal 2: Regional Client (机房客户端 DC1)

```bash
make run-regional
```

输出示例：
```
Starting LPMOS Regional Client v3.0 for IDC: dc1
Regional client API listening on :8081
[dc1] Watching for server additions in /os/dc1/servers/
[dc1] Watching for task changes in /os/dc1/machines/
```

### Terminal 3: Agent (装机代理)

```bash
make run-agent
```

## 🔍 预期的 Agent 输出

```
=== LPMOS Agent Started (Enhanced) ===
Regional Client: http://localhost:8081
Polling Interval: 10s

[Stage 1/5] Collecting hardware information...
  Serial Number: C02ABC123XYZ
  MAC Address: fe:b7:02:c0:95:e0
  Company: Apple Inc.
  Product: MacBookPro18,1
  Model: MacBookPro18,1
  Is VM: false
  CPU: Apple M1 Max (14 cores)
  Memory: 32 GB
  Disks: 1
    - /dev/disk0: 931 GB (Unknown)
  Progress: [10%] hardware_collect - Hardware information collected successfully (no task assigned yet)

[Stage 2/5] Reporting hardware to regional client...
  Hardware reported (no task assigned yet)
  Hardware reported successfully
  Progress: [15%] hardware_report - Hardware reported to regional client (no task assigned yet)

[Stage 3/5] Polling for task assignment...
  Progress: [20%] task_wait - Waiting for task assignment (no task assigned yet)
  Polling for task (attempt 1/60)...
  No task assigned yet
  Polling for task (attempt 2/60)...
  No task assigned yet
  [等待任务分配...]
```

**此时 Agent 进入轮询状态，等待通过 Web 界面创建任务**

## 🎯 创建装机任务

### 步骤 1: 访问 Web 界面

打开 http://localhost:8080

### 步骤 2: 创建任务

1. 点击 **"➕ 新建装机任务"**
2. 填写信息：
   - **机房**: DC1 - 北京数据中心
   - **服务器序列号**: `C02ABC123XYZ` (使用 agent 输出的序列号)
   - **MAC地址**: `fe:b7:02:c0:95:e0` (使用 agent 输出的 MAC)
   - **操作系统**: Ubuntu 22.04 LTS
   - **系统版本**: 22.04
3. 点击 **"创建任务"**

### 步骤 3: 审批任务

在任务列表中找到刚创建的任务，点击 **"✓ 审批"**

## ✅ Agent 继续执行

一旦任务被审批，agent 会立即检测到并继续执行：

```
  Polling for task (attempt 5/60)...
  Task found and approved!
  Task received! Task ID: task-abc12345
  OS Type: Ubuntu 22.04
  OS Version: 22.04
  Progress: [30%] task_received - Task received: Ubuntu 22.04 22.04

[Stage 4/5] Starting OS installation...
  Progress: [40%] install_start - Starting OS installation process
  OS Type: Ubuntu 22.04
  OS Version: 22.04
  Disk Layout:
  [partitioning] Creating disk partitions...
  Progress: [50%] partitioning - Creating disk partitions
  [downloading] Downloading OS image...
  Progress: [60%] downloading - Downloading OS image
  [installing] Installing base system...
  Progress: [70%] installing - Installing base system
  [configuring] Configuring system...
  Progress: [80%] configuring - Configuring system
  [finalizing] Finalizing installation...
  Progress: [90%] finalizing - Finalizing installation
  [completed] Installation completed successfully...
  Progress: [100%] completed - Installation completed successfully
  Progress: [45%] install_progress - Installation in progress

[Stage 5/5] Installation completed
  Progress: [50%] completed - OS installation completed successfully

=== OS Installation Completed Successfully ===
```

## 📊 在 Web 界面查看进度

Web 界面会实时显示：
- ✅ **待审批任务** 区域：显示新创建的任务
- ✅ **安装中任务** 区域：审批后，任务移到这里，显示实时进度
- ✅ **已完成任务** 区域：安装完成后，任务移到这里
- ✅ **进度条**: 实时更新 0% → 100%
- ✅ **状态标签**: pending → approved → installing → completed

## 🔧 Regional Client 输出

Regional Client 会记录所有操作：

```
[dc1] Received hardware report from C02ABC123XYZ (MAC: fe:b7:02:c0:95:e0)
[dc1] Hardware report unmatched (stored): fe:b7:02:c0:95:e0
[dc1] Progress update from C02ABC123XYZ: hardware_collect (10%)
[dc1] Progress update from C02ABC123XYZ: hardware_report (15%)
[dc1] Progress update from C02ABC123XYZ: task_wait (20%)
[dc1] Progress update from C02ABC123XYZ: task_received (30%)
[dc1] Progress update from C02ABC123XYZ: install_start (40%)
[dc1] Progress update from C02ABC123XYZ: partitioning (50%)
[dc1] Progress update from C02ABC123XYZ: downloading (60%)
[dc1] Progress update from C02ABC123XYZ: installing (70%)
[dc1] Progress update from C02ABC123XYZ: configuring (80%)
[dc1] Progress update from C02ABC123XYZ: finalizing (90%)
[dc1] Progress update from C02ABC123XYZ: completed (100%)
```

## 🗂️ etcd 数据结构

### 服务器注册
```bash
etcdctl get --prefix /os/dc1/servers/

# 输出:
/os/dc1/servers/C02ABC123XYZ
{
  "sn": "C02ABC123XYZ",
  "mac": "fe:b7:02:c0:95:e0",
  "status": "registered",
  "added_at": "2026-01-30T14:50:00Z"
}
```

### 任务信息
```bash
etcdctl get --prefix /os/dc1/machines/C02ABC123XYZ/task

# 输出:
/os/dc1/machines/C02ABC123XYZ/task
{
  "task_id": "task-abc12345",
  "sn": "C02ABC123XYZ",
  "mac": "fe:b7:02:c0:95:e0",
  "status": "completed",
  "os_type": "Ubuntu 22.04",
  "os_version": "22.04",
  "progress": [
    {"step": "hardware_collect", "percent": 10, "message": "..."},
    {"step": "partitioning", "percent": 50, "message": "..."},
    {"step": "completed", "percent": 100, "message": "..."}
  ],
  "logs": ["[INFO] Hardware collected: ...", "..."],
  "created_at": "2026-01-30T14:50:00Z",
  "updated_at": "2026-01-30T14:52:00Z"
}
```

### 硬件元数据
```bash
etcdctl get /os/dc1/machines/C02ABC123XYZ/meta

# 输出:
{
  "serial_number": "C02ABC123XYZ",
  "mac_address": "fe:b7:02:c0:95:e0",
  "company": "Apple Inc.",
  "product": "MacBookPro18,1",
  "model_name": "MacBookPro18,1",
  "is_vm": false,
  "cpu": {"model": "Apple M1 Max", "cores": 14},
  "memory": {"total_gb": 32},
  "disks": [{"device": "/dev/disk0", "size_gb": 931, "type": "Unknown"}]
}
```

## 🎯 API 端点总结

### Agent → Regional Client

| 端点 | 方法 | 用途 | 阶段 |
|------|------|------|------|
| `/api/v1/report` | POST | 上报硬件信息 | Stage 2 (15%) |
| `/api/v1/task/{sn}` | GET | 轮询任务状态 | Stage 3 (20-30%) |
| `/api/v1/progress` | POST | 上报安装进度 | Stage 4-5 (40-100%) |

### Regional Client → etcd

- 写入: `/os/{dc}/servers/{sn}`
- 写入: `/os/{dc}/machines/{sn}/task`
- 写入: `/os/{dc}/machines/{sn}/meta`
- 监听: `/os/{dc}/servers/` (watch)
- 监听: `/os/{dc}/machines/` (watch)

### Control Plane → etcd

- 读取: `/os/{dc}/servers/`
- 读取: `/os/{dc}/machines/`
- 创建: `/os/{dc}/servers/{sn}`
- 创建: `/os/{dc}/machines/{sn}/task`

## 🐛 故障排查

### Agent 无法连接 Regional Client

```
Failed to send request: dial tcp 127.0.0.1:8081: connection refused
```

**解决**: 确保 Regional Client 已启动 (`make run-regional`)

### Agent 一直轮询，找不到任务

```
Polling for task (attempt 10/60)...
No task assigned yet
```

**原因**:
1. 任务尚未创建
2. 任务 SN 与 agent 序列号不匹配
3. 任务尚未审批

**解决**:
1. 在 Web 界面创建任务，使用 agent 输出的 **准确序列号**
2. 审批任务

### 进度上报失败

```
Progress update failed: 404 - Task not found
```

**原因**: Regional Client 无法找到对应的任务

**解决**: 确保任务已创建且 SN 匹配

## 📈 性能特性

- ✅ **轮询间隔**: 10 秒（可配置）
- ✅ **超时保护**: 10 分钟最大等待
- ✅ **并发安全**: etcd CAS 原子操作
- ✅ **实时更新**: WebSocket 推送到前端
- ✅ **自动清理**: Lease TTL 机制

## 🎓 架构优势

1. **解耦设计**: Agent ↔ Regional Client ↔ Control Plane 完全解耦
2. **可扩展**: 支持多机房、多 Regional Client
3. **容错**: 网络错误自动重试，状态持久化
4. **监控友好**: 精细化进度上报，便于监控
5. **跨平台**: Linux 和 macOS 完整支持

---

**更新时间**: 2026-01-30
**版本**: v3.0 (Enhanced)
**状态**: ✅ 生产就绪
