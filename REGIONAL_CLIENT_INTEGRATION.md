# Regional Client 集成完成 - DHCP + TFTP + PXE

## ✅ 已完成的集成工作

### 1. 模块导入和结构体更新

**修改文件**: `cmd/regional-client/main.go`

**添加的导入**:
```go
import (
    "github.com/lpmos/lpmos-go/cmd/regional-client/dhcp"
    "github.com/lpmos/lpmos-go/cmd/regional-client/pxe"
    "github.com/lpmos/lpmos-go/cmd/regional-client/tftp"
    // ... 其他导入
)
```

**更新的结构体**:
```go
type RegionalClient struct {
    idc          string
    etcdClient   *etcd.Client
    ctx          context.Context
    cancel       context.CancelFunc
    leases       map[string]clientv3.LeaseID

    // 新增: PXE 基础设施
    dhcpServer   *dhcp.Server      // DHCP 服务器
    tftpServer   *tftp.Server      // TFTP 服务器
    pxeGenerator *pxe.Generator    // PXE 配置生成器

    // 新增: 配置参数
    serverIP     string            // 服务器IP
    networkIface string            // 网络接口
}
```

### 2. 主函数更新 - 支持启动参数

**新增启动参数**:
```bash
--enable-dhcp          # 启用 DHCP 服务器
--enable-tftp          # 启用 TFTP 服务器
--server-ip=<IP>       # 服务器 IP 地址 (默认: 192.168.100.1)
--interface=<name>     # 网络接口名称 (默认: eth1)
```

**使用示例**:
```bash
# 基础模式 (不启用 DHCP/TFTP)
./regional-client --idc=dc1 --api-port=8081

# 完整模式 (启用 DHCP + TFTP + PXE)
sudo ./regional-client --idc=dc1 --api-port=8081 --enable-dhcp --enable-tftp --server-ip=192.168.100.1 --interface=eth1
```

### 3. 初始化函数

#### 3.1 TFTP 初始化 (`initTFTP`)

```go
func (rc *RegionalClient) initTFTP() error
```

功能:
- 创建 TFTP 根目录 `/tftpboot`
- 自动创建子目录: `pxelinux.cfg/`, `kernels/`, `initrds/`
- 启动 TFTP 服务器 (端口 69)
- 配置: 最大 100 个并发客户端, 30 秒超时, 512 字节块大小

#### 3.2 PXE 初始化 (`initPXE`)

```go
func (rc *RegionalClient) initPXE() error
```

功能:
- 创建 PXE 配置生成器
- 生成默认 PXE 配置文件 `/tftpboot/pxelinux.cfg/default`
- 准备为每个 MAC 地址生成专属配置

#### 3.3 DHCP 初始化 (`initDHCP`)

```go
func (rc *RegionalClient) initDHCP() error
```

功能:
- 启动 DHCP 服务器 (端口 67)
- 配置 IP 池: `192.168.100.10` - `192.168.100.200`
- 设置网关、DNS、TFTP 服务器地址
- 配置 PXE 启动文件: `pxelinux.0`
- 租约时间: 1 小时

### 4. 自动化 PXE 启动配置

#### 4.1 任务监听增强 (`watchTasks`)

```go
func (rc *RegionalClient) watchTasks()
```

**工作流程**:
1. 监听 etcd 中的任务更新
2. 检测到 `TaskStatusApproved` 状态的任务
3. 自动触发 `configurePXEBoot()` 进行 PXE 环境配置

#### 4.2 PXE 启动配置 (`configurePXEBoot`)

```go
func (rc *RegionalClient) configurePXEBoot(task *models.TaskV3)
```

**自动化流程**:

```
检测到装机任务
   ↓
1. 添加 DHCP 静态绑定
   MAC: 00:1a:2b:3c:4d:5e
   IP:  192.168.100.10
   BootFile: pxelinux.0
   ↓
2. 生成 PXE 配置文件
   文件: /tftpboot/pxelinux.cfg/01-00-1a-2b-3c-4d-5e
   内核: /kernels/ubuntu-22.04-vmlinuz
   Initrd: /initrds/ubuntu-22.04-initrd.img
   参数: regional_url, sn, dc, hostname, ip
   ↓
3. 配置交换机 (TODO)
   将服务器端口加入装机 VLAN
   ↓
4. 控制 BMC (TODO)
   设置 PXE 启动
   重启服务器
   ↓
5. 更新任务日志
   记录 PXE 配置完成
```

**日志输出**:
```
[dc1] Configuring PXE boot for SN123456 (MAC: 00:1a:2b:3c:4d:5e, IP: 192.168.100.10)
[dc1] ✓ DHCP binding added: 00:1a:2b:3c:4d:5e -> 192.168.100.10
[dc1] ✓ PXE configuration generated: /tftpboot/pxelinux.cfg/01-00-1a-2b-3c-4d-5e
[dc1] TODO: Configure switch for SN123456
[dc1] TODO: Control BMC to reboot SN123456 into PXE mode
[dc1] ✓ PXE boot environment configured for SN123456
```

#### 4.3 PXE 配置清理 (`cleanupPXEBoot`)

```go
func (rc *RegionalClient) cleanupPXEBoot(task *models.TaskV3)
```

**清理时机**: 装机完成时自动触发

**清理步骤**:
1. 删除 PXE 配置文件
2. 删除 DHCP 静态绑定
3. 恢复交换机配置 (TODO)
4. 更新任务日志

### 5. 管理 API 端点

**新增 API 端点**: `/api/v1/pxe/*`

#### 5.1 DHCP 状态查询

```bash
GET /api/v1/pxe/dhcp/status
```

返回:
```json
{
  "status": "running",
  "static_bindings": 5
}
```

#### 5.2 DHCP 租约查询

```bash
GET /api/v1/pxe/dhcp/leases
```

返回:
```json
{
  "leases": [
    {
      "mac": "00:1a:2b:3c:4d:5e",
      "ip": "192.168.100.10",
      "hostname": "server-01",
      "expire_time": "2026-01-30T15:00:00Z"
    }
  ],
  "bindings": {
    "00:1a:2b:3c:4d:5e": {
      "mac": "00:1a:2b:3c:4d:5e",
      "ip": "192.168.100.10",
      "hostname": "server-01",
      "boot_file": "pxelinux.0"
    }
  }
}
```

#### 5.3 TFTP 状态查询

```bash
GET /api/v1/pxe/tftp/status
```

返回:
```json
{
  "status": "running",
  "total_requests": 156,
  "success": 152,
  "failed": 4,
  "bytes_served": 89123456
}
```

#### 5.4 TFTP 文件列表

```bash
GET /api/v1/pxe/tftp/files
```

返回:
```json
{
  "files": [
    {
      "name": "pxelinux.0",
      "size": 26828,
      "mod_time": "2026-01-30T12:00:00Z"
    },
    {
      "name": "pxelinux.cfg/default",
      "size": 234,
      "mod_time": "2026-01-30T12:05:00Z"
    }
  ],
  "total": 2
}
```

#### 5.5 PXE 配置列表

```bash
GET /api/v1/pxe/configs
```

返回:
```json
{
  "configs": [
    "01-00-1a-2b-3c-4d-5e",
    "01-00-aa-bb-cc-dd-ee"
  ],
  "total": 2
}
```

#### 5.6 健康检查增强

```bash
GET /health
```

返回:
```json
{
  "status": "healthy",
  "idc": "dc1",
  "dhcp": "enabled",
  "tftp": "enabled",
  "pxe": "enabled"
}
```

### 6. Makefile 更新

**新增命令**:

```makefile
# 基础模式 (不启用 DHCP/TFTP)
make run-regional          # DC1, 端口 8081
make run-regional-dc2      # DC2, 端口 8082

# 完整模式 (启用 DHCP + TFTP + PXE)
make run-regional-full     # DC1, 需要 root 权限
make run-regional-dc2-full # DC2, 需要 root 权限
```

**使用示例**:
```bash
# 启动完整 PXE 环境
make build-regional-client
make run-regional-full

# 输出:
# 启动 Regional Client (dc1) with DHCP+TFTP+PXE...
# ⚠️  需要 root 权限 (DHCP 端口67, TFTP 端口69)
# [dc1] TFTP server started: root=/tftpboot, port=69
# [dc1] PXE generator initialized
# [dc1] DHCP server started: pool=192.168.100.10-192.168.100.200, port=67
# Regional client API listening on :8081
```

## 🔄 完整的装机流程

### 用户操作流程

```
1. Control Plane 前端
   用户点击"提交装机任务"
   填写: SN, MAC, IP, Hostname, OS类型
   ↓
2. Control Plane 后台
   将任务写入 etcd
   key: /lpmos/machines/dc1/{sn}/task
   status: approved
   ↓
3. Regional Client (自动)
   watchTasks() 检测到新任务
   ↓
4. configurePXEBoot() (自动)
   a. 添加 DHCP 绑定: MAC -> IP
   b. 生成 PXE 配置文件
   c. 配置交换机: 端口加入装机 VLAN
   d. 控制 BMC: 设置 PXE 启动 + 重启
   ↓
5. 服务器启动 (自动)
   a. DHCP 获取 IP: 192.168.100.10
   b. TFTP 下载: pxelinux.0
   c. TFTP 下载: pxelinux.cfg/01-{mac}
   d. TFTP 下载: kernel + initrd
   e. 启动内存系统
   ↓
6. Agent 启动 (自动)
   a. 上报硬件信息
   b. 请求: isInInstallQueue
   c. 请求: getNextOperation
   d. 执行: hardware_config
   e. 报告: operationComplete
   ↓
7. 安装操作系统 (自动)
   a. 请求: getNextOperation (os_install)
   b. 执行操作系统安装
   c. 报告: operationComplete (100%)
   ↓
8. cleanupPXEBoot() (自动)
   a. 删除 PXE 配置
   b. 删除 DHCP 绑定
   c. 移出装机 VLAN
   ↓
9. 完成
   Control Plane 前端显示: 装机完成
```

## 📊 架构总览

```
Control Plane (前端 + 后台)
         ↓ (写入任务)
       etcd
         ↓ (监听变化)
Regional Client
    ├── DHCP Server (端口 67)
    │   ├── 静态 MAC-IP 绑定
    │   └── 动态 IP 分配池
    ├── TFTP Server (端口 69)
    │   ├── /tftpboot/pxelinux.0
    │   ├── /tftpboot/pxelinux.cfg/*
    │   ├── /tftpboot/kernels/*
    │   └── /tftpboot/initrds/*
    ├── PXE Generator
    │   └── 为每个 MAC 生成专属配置
    ├── Switch Manager (TODO)
    │   └── 配置交换机端口 VLAN
    └── BMC Controller (TODO)
        └── 远程控制服务器启动
         ↓
     服务器
    ├── PXE 启动
    ├── DHCP 获取 IP
    ├── TFTP 下载文件
    └── Agent 执行装机
```

## 🚀 测试指南

### 1. 启动环境

```bash
# Terminal 1: 启动 etcd
make start-etcd

# Terminal 2: 启动 Control Plane
make build-control-plane
make run

# Terminal 3: 启动 Regional Client (完整模式)
make build-regional-client
sudo make run-regional-full
```

### 2. 查看状态

```bash
# 查看健康状态
curl http://localhost:8081/health

# 查看 DHCP 状态
curl http://localhost:8081/api/v1/pxe/dhcp/status

# 查看 TFTP 状态
curl http://localhost:8081/api/v1/pxe/tftp/status

# 查看 PXE 配置列表
curl http://localhost:8081/api/v1/pxe/configs
```

### 3. 提交装机任务

通过 Control Plane 前端提交任务，或使用 API:

```bash
curl -X POST http://localhost:8080/api/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "sn": "SN123456789",
    "mac": "00:1a:2b:3c:4d:5e",
    "ip": "192.168.100.10",
    "hostname": "server-01",
    "os_type": "ubuntu",
    "os_version": "22.04",
    "idc": "dc1"
  }'
```

### 4. 观察日志

Regional Client 会输出详细日志:

```
[dc1] Task approved for SN123456789, configuring PXE boot...
[dc1] Configuring PXE boot for SN123456789 (MAC: 00:1a:2b:3c:4d:5e, IP: 192.168.100.10)
[dc1] ✓ DHCP binding added: 00:1a:2b:3c:4d:5e -> 192.168.100.10
[dc1] ✓ PXE configuration generated: /tftpboot/pxelinux.cfg/01-00-1a-2b-3c-4d-5e
[dc1] ✓ PXE boot environment configured for SN123456789
```

## 📝 下一步工作

### 优先级 1: BMC 控制模块

实现 `cmd/regional-client/bmc/`:
- `controller.go` - BMC 控制器接口
- `ipmi.go` - IPMI 实现 (使用 ipmitool)
- `redfish.go` - Redfish 实现

功能:
- `PowerOn()` - 开机
- `PowerOff()` - 关机
- `PowerCycle()` - 重启
- `SetBootDevice(device string)` - 设置启动设备 (pxe, disk, cdrom)
- `GetPowerStatus()` - 获取电源状态

### 优先级 2: 交换机管理模块

实现 `cmd/regional-client/switch/`:
- `manager.go` - 交换机管理器接口
- `cisco.go` - Cisco 交换机 (SSH/SNMP)
- `huawei.go` - 华为交换机 (SSH/SNMP)
- `h3c.go` - H3C 交换机 (SSH/SNMP)

功能:
- `ConfigurePort(port string, vlan int)` - 配置端口 VLAN
- `EnablePort(port string)` - 启用端口
- `DisablePort(port string)` - 禁用端口
- `GetPortStatus(port string)` - 获取端口状态

### 优先级 3: 完善 PXE 启动文件

准备常用操作系统的 PXE 启动文件:
- Ubuntu 22.04 / 20.04
- CentOS 7.9 / 8
- Rocky Linux 8 / 9
- Debian 11 / 12

## ✅ 总结

**已完成**:
- ✅ DHCP 服务器集成到 Regional Client
- ✅ TFTP 服务器集成到 Regional Client
- ✅ PXE 配置生成器集成
- ✅ 自动化 PXE 启动配置流程
- ✅ 装机完成后自动清理
- ✅ 管理 API 端点
- ✅ Makefile 更新

**待实现**:
- ⏳ BMC 控制模块 (远程重启服务器)
- ⏳ 交换机管理模块 (VLAN 配置)

**当前状态**: Regional Client 已具备完整的 PXE 自动装机基础设施！只需补充 BMC 和交换机管理模块，即可实现端到端的全自动装机流程。

---

**完成时间**: 2026-01-30
**集成代码行数**: 约 300 行 (Regional Client main.go)
**新增 API**: 6 个管理端点
**状态**: ✅ DHCP + TFTP + PXE 已完全集成到 Regional Client
