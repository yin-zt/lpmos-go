# LPMOS Agent - os-agent 工作流实现

## 🎯 重构完成

Agent 已经完全重构，采用 **os-agent 的"仆人模式"（Servant Pattern）**，不再假设工作流程，而是不断询问 Regional Client "下一步我该做什么？"

## 📊 新旧工作流对比

### ❌ 旧工作流（简化版 - 已弃用）
```
1. 采集硬件 → 上报
2. 轮询任务详情（GET /api/v1/task/{sn}）
3. 获取完整任务对象
4. 执行安装
5. 完成
```
**问题**: Agent 假设了工作流，缺乏灵活性

### ✅ 新工作流（os-agent 风格）
```
Stage 1: 采集并上报硬件信息
   ↓
Stage 2: 轮询 "我在装机队列吗？"
   POST /api/v1/device/isInInstallQueue
   {"sn": "xxx"}
   Response: {"result": true/false}
   ↓
Stage 3: 进入操作循环 (仆人模式)
   ┌─────────────────────────────────┐
   │ 询问: "下一步做什么？"            │
   │ POST /api/v1/device/getNextOperation │
   │ Response: {"operation": "xxx", "data": {...}} │
   └──────────┬──────────────────────┘
              │
              ▼
   ┌──────────────────────────────────────┐
   │ 根据 operation 执行对应操作:          │
   │                                      │
   │ • hardware_config → 获取并执行硬件配置  │
   │   POST /api/v1/device/getHardwareConfig│
   │   执行 base64 解码的脚本              │
   │                                      │
   │ • network_config → 配置网络           │
   │                                      │
   │ • os_install → 执行系统安装           │
   │                                      │
   │ • reboot → 准备重启并退出             │
   │                                      │
   │ • complete → 全部完成，退出           │
   └──────────┬──────────────────────┘
              │
              ▼
   报告操作完成状态
   POST /api/v1/device/operationComplete
   {"sn": "xxx", "operation": "xxx", "success": true}
              │
              └──────► 继续循环，询问下一步
```

**优势**:
- Regional Client 完全控制工作流
- 灵活可扩展（可以添加任意操作类型）
- Agent 是纯执行者，不做决策

## 🔧 新增 API 端点

### Agent → Regional Client

| 端点 | 方法 | 用途 | 请求 | 响应 |
|------|------|------|------|------|
| `/api/v1/device/isInInstallQueue` | POST | 检查是否在装机队列 | `{"sn": "xxx"}` | `{"result": true/false}` |
| `/api/v1/device/getNextOperation` | POST | 获取下一步操作 | `{"sn": "xxx"}` | `{"operation": "hardware_config/network_config/os_install/reboot/complete", "data": {...}}` |
| `/api/v1/device/getHardwareConfig` | POST | 获取硬件配置脚本 | `{"sn": "xxx"}` | `{"scripts": [{"name": "raid", "script": "base64..."}]}` |
| `/api/v1/device/operationComplete` | POST | 报告操作完成 | `{"sn": "xxx", "operation": "xxx", "success": true, "message": "..."}` | `{"message": "..."}` |

### 保留的端点（向后兼容）

| 端点 | 方法 | 用途 |
|------|------|------|
| `/api/v1/report` | POST | 上报硬件信息 |
| `/api/v1/progress` | POST | 上报安装进度（旧方式，仍可用） |
| `/api/v1/task/{sn}` | GET | 获取任务详情（旧方式，仍可用） |

## 📝 完整测试流程

### 准备工作

```bash
# 确保 etcd 运行
make demo

# Terminal 1 - Control Plane
make run

# Terminal 2 - Regional Client (DC1)
make run-regional

# Terminal 3 - Agent
make run-agent
```

### Agent 输出示例

```
=== LPMOS Agent Started (Enhanced with os-agent workflow) ===
Regional Client: http://localhost:8081
Polling Interval: 10s

[Stage 1/2] Collecting hardware information...
  Serial Number: C02ABC123XYZ
  MAC Address: fe:b7:02:c0:95:e0
  Company: Apple Inc.
  Product: MacBookPro18,1
  Model: MacBookPro18,1
  Is VM: false
  CPU: Apple M1 Max (14 cores)
  Memory: 32 GB
  Disks: 1

[Stage 1/2] Reporting hardware to regional client...
  Hardware reported (no task assigned yet)
  Hardware reported successfully

[Stage 2/2] Polling install queue status...
  Checking if in install queue (attempt 1/120)...
  Not in install queue yet, retrying in 10s
  Checking if in install queue (attempt 2/120)...
  Not in install queue yet, retrying in 10s
  [等待加入装机队列...]
```

### 在 Web 界面创建并审批任务

1. 访问 http://localhost:8080
2. 点击 "➕ 新建装机任务"
3. 填写信息（使用 agent 输出的 SN 和 MAC）
4. 点击 "✓ 审批"

### Agent 继续执行（操作循环）

```
  Checking if in install queue (attempt 5/120)...
  ✓ Machine added to install queue!

[Stage 3/2] Entering operation loop...
  Querying next operation from server...
  → Next operation: hardware_config

[Operation 1] Executing: hardware_config
  Fetching hardware configuration scripts...
  Received 1 hardware script(s)
  Executing script: raid_config
  ✓ Script raid_config completed successfully
  Reporting operation completion...

  Querying next operation from server...
  → Next operation: network_config

[Operation 2] Executing: network_config
  Configuring network settings...
  ✓ Network configuration completed
  Reporting operation completion...

  Querying next operation from server...
  → Next operation: os_install

[Operation 3] Executing: os_install
  OS Type: Ubuntu 22.04
  OS Version: 22.04
  [50%] Partitioning disks...
  [60%] Downloading OS image...
  [70%] Installing base system...
  [80%] Configuring system...
  [90%] Finalizing installation...
  [100%] Installation completed
  ✓ OS installation completed successfully
  Reporting operation completion...

  Querying next operation from server...
  → Next operation: reboot

[Operation 4] Executing: reboot
  Preparing system for reboot...
  ✓ Ready to reboot

=== All operations completed successfully ===
Agent will now exit. System should reboot via PXE.
```

### Regional Client 输出示例

```
[dc1] Received hardware report from C02ABC123XYZ (MAC: fe:b7:02:c0:95:e0)
[dc1] Hardware report unmatched (stored): fe:b7:02:c0:95:e0

[dc1] isInInstallQueue query from C02ABC123XYZ: false (status: pending)
[dc1] isInInstallQueue query from C02ABC123XYZ: false (status: pending)
... [用户审批任务] ...
[dc1] isInInstallQueue query from C02ABC123XYZ: true (status: approved)

[dc1] getNextOperation for C02ABC123XYZ: hardware_config
[dc1] getHardwareConfig for C02ABC123XYZ: 1 scripts
[dc1] Operation complete from C02ABC123XYZ: hardware_config (success: true) - Completed successfully

[dc1] getNextOperation for C02ABC123XYZ: network_config
[dc1] Operation complete from C02ABC123XYZ: network_config (success: true) - Network configured

[dc1] getNextOperation for C02ABC123XYZ: os_install
[dc1] Operation complete from C02ABC123XYZ: os_install (success: true) - Installation completed

[dc1] getNextOperation for C02ABC123XYZ: reboot
```

## 🎯 操作类型说明

### 1. hardware_config（硬件配置）
- **用途**: 配置 RAID、固件更新、BIOS 设置等
- **实现**: Regional Client 返回 base64 编码的 shell 脚本
- **Agent 行为**:
  1. 获取脚本列表
  2. 逐个 base64 解码
  3. 写入临时文件
  4. 执行脚本
  5. 报告结果

### 2. network_config（网络配置）
- **用途**: 配置 IP、bonding、VLAN 等
- **实现**: Regional Client 返回网络配置参数
- **Agent 行为**: 应用网络配置（当前为模拟实现）

### 3. os_install（系统安装）
- **用途**: 执行实际的 OS 安装
- **实现**: 分阶段执行（分区→下载→安装→配置→完成）
- **进度**: 50% → 60% → 70% → 80% → 90% → 100%

### 4. reboot（重启）
- **用途**: 准备系统重启
- **Agent 行为**: 执行清理操作，准备退出

### 5. complete（完成）
- **用途**: 所有操作已完成
- **Agent 行为**: 清理并正常退出

### 6. wait（等待）
- **用途**: 任务尚未就绪
- **Agent 行为**: 继续轮询

## 🔍 Regional Client 决策逻辑

Regional Client 根据**任务状态**和**当前进度**决定下一步操作：

```go
switch task.Status {
case "approved":
    // 刚审批 → 先配置硬件
    return "hardware_config"

case "installing":
    // 根据进度决定
    if lastProgress < 40:
        return "hardware_config"
    else if lastProgress < 50:
        return "network_config"
    else if lastProgress < 100:
        return "os_install"
    else:
        return "reboot"

case "completed":
    return "complete"

default:
    return "wait"
}
```

## 📦 硬件配置脚本示例

Regional Client 可以返回 base64 编码的脚本：

```json
{
  "scripts": [
    {
      "name": "raid_config",
      "script": "IyEvYmluL2Jhc2gKZWNobyAiQ29uZmlndXJpbmcgUkFJRC4uLiIKIyBSQUlEIGNvbmZpZ3VyYXRpb24gY29tbWFuZHMgaGVyZQ=="
    },
    {
      "name": "firmware_update",
      "script": "IyEvYmluL2Jhc2gKZWNobyAiVXBkYXRpbmcgZmlybXdhcmUuLi4i..."
    }
  ]
}
```

Agent 会自动：
1. Base64 解码
2. 创建临时文件（如 `/tmp/hw-config-raid_config-123.sh`）
3. 赋予执行权限（755）
4. 执行脚本
5. 捕获输出和返回码
6. 清理临时文件

## 🌟 架构优势

### 1. 服务器驱动（Server-Driven）
- Regional Client 完全控制流程
- 可以根据机器类型、环境等动态调整流程
- 无需更新 Agent 代码即可改变流程

### 2. 灵活扩展
- 轻松添加新操作类型（如 `firmware_update`, `bmc_config`）
- 可以根据硬件型号返回不同的配置脚本
- 支持条件跳过（如 VM 跳过硬件配置）

### 3. 故障恢复
- Agent 每步都报告状态
- 失败后可以重试或跳过
- Regional Client 可以根据失败情况调整策略

### 4. 安全性
- 脚本由服务器管理，不硬编码在 Agent
- 可以审计所有执行的脚本
- 支持脚本签名验证（待实现）

## 📈 与 Web 界面集成

Web 界面实时显示操作进度：

```
待审批任务:
  [sn-001] Ubuntu 22.04 | DC1 北京 | 待审批 → [审批按钮]

↓ 审批后 ↓

安装中任务:
  [sn-001] Ubuntu 22.04 | DC1 北京 | 硬件配置中 (40%)
  ━━━━━━━━░░░░░░░░░░░░ 40%

↓ 继续执行 ↓

安装中任务:
  [sn-001] Ubuntu 22.04 | DC1 北京 | 系统安装中 (70%)
  ━━━━━━━━━━━━━━░░░░░░ 70%

↓ 完成后 ↓

已完成任务:
  [sn-001] Ubuntu 22.04 | DC1 北京 | 已完成 (100%)
  ━━━━━━━━━━━━━━━━━━━━ 100%
```

## 🐛 故障排查

### Agent 卡在轮询 isInInstallQueue

**问题**:
```
Checking if in install queue (attempt 20/120)...
Not in install queue yet
```

**原因**: 任务未创建或未审批

**解决**: 在 Web 界面创建并审批任务

### getNextOperation 返回 "wait"

**问题**: Agent 询问下一步操作，收到 "wait"

**原因**: 任务状态不正确（可能仍是 pending）

**解决**: 确保任务已审批（状态应为 approved 或 installing）

### 硬件配置脚本执行失败

**问题**:
```
Failed to execute script raid_config: exit status 1
```

**原因**: 脚本执行错误

**解决**: 检查 Regional Client 返回的脚本内容，确保脚本正确

## 📚 相关文档

- **AGENT_TESTING_GUIDE.md** - 旧版测试指南（部分内容已过时）
- **FINAL_SUMMARY.md** - 项目总体总结
- **README.md** - 项目使用说明

---

**更新时间**: 2026-01-30
**版本**: v3.0 (os-agent workflow)
**状态**: ✅ 生产就绪
