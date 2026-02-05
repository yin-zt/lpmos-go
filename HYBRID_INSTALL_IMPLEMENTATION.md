# 方案 3 混合安装方式 - 实现完成

## ✅ 已完成的工作

### 1. 数据模型更新 (`pkg/models/types.go`)

**新增结构体**:
```go
// 安装方式枚举
type InstallMethod string
const (
    InstallMethodKickstart   InstallMethod = "kickstart"
    InstallMethodAgentDirect InstallMethod = "agent_direct"
)

// OS 安装配置
type OSInstallConfig struct {
    Method        InstallMethod
    OSType        string
    OSVersion     string
    MirrorURL     string
    KickstartURL  string         // Kickstart 方式使用
    DiskLayout    DiskLayoutConfig // Agent 直接安装使用
    Network       NetworkConfig
    Packages      []string
    PostScript    string
    RootPassword  string
}

// 磁盘布局配置
type DiskLayoutConfig struct {
    RootDisk       string
    PartitionTable string
    Partitions     []PartitionConfig
}

// 分区配置
type PartitionConfig struct {
    MountPoint string
    Size       string
    FSType     string
}

// 网络配置
type NetworkConfig struct {
    Interface string
    Method    string
    IP        string
    Netmask   string
    Gateway   string
    DNS       string
    Hostname  string
}

// RAID 配置
type RAIDConfig struct {
    Enabled     bool
    Level       string
    Disks       []string
    Controller  string
    VirtualDisk string
}

// 硬件配置
type HardwareConfig struct {
    RAID         *RAIDConfig
    BIOS         map[string]string
    CustomScript string
}
```

---

### 2. Kickstart/Preseed 生成器

#### 文件结构
```
cmd/regional-client/kickstart/
├── generator.go   # 生成器核心逻辑
└── templates.go   # 各种 OS 的模板
```

#### 支持的操作系统
- ✅ CentOS 7
- ✅ CentOS 8 / Stream
- ✅ Rocky Linux 8
- ✅ Rocky Linux 9
- ✅ Ubuntu 20.04 (Preseed)
- ✅ Ubuntu 22.04 (Preseed)

#### 主要功能
```go
generator := kickstart.NewGenerator()

// 生成 Kickstart 文件
ksContent, err := generator.Generate(task, config)

// 生成 Preseed 文件
preseedContent, err := generator.GeneratePreseed(task, config)
```

---

### 3. Regional Client 更新

#### 新增 API 端点

**安装配置**:
```
POST /api/v1/device/getOSInstallConfig
- Agent 获取操作系统安装配置
- 返回安装方式和详细参数
```

**Kickstart/Preseed**:
```
GET /api/v1/kickstart/:sn
- 动态生成 Kickstart 文件 (CentOS/Rocky)

GET /api/v1/preseed/:sn
- 动态生成 Preseed 文件 (Ubuntu/Debian)
```

**安装完成通知**:
```
POST /api/v1/device/installComplete
- 系统安装完成后通知
- 自动清理 PXE 配置
```

**静态文件服务**:
```
GET /static/*
- 提供 kernel, initramfs 等文件

GET /repos/*
- 提供软件包仓库镜像
```

#### 安装方式决策逻辑

```go
func (rc *RegionalClient) determineInstallMethod(task *models.TaskV3) models.InstallMethod {
    // 1. 有特殊磁盘布局或网络配置 → Agent 直接安装
    if task.DiskLayout != "" || task.NetworkConf != "" {
        return models.InstallMethodAgentDirect
    }

    // 2. Ubuntu/Debian → Agent 直接安装 (debootstrap)
    if task.OSType == "ubuntu" || task.OSType == "debian" {
        return models.InstallMethodAgentDirect
    }

    // 3. CentOS/Rocky → Kickstart (更成熟)
    if task.OSType == "centos" || task.OSType == "rocky" {
        return models.InstallMethodKickstart
    }

    // 默认: Agent 直接安装 (更灵活)
    return models.InstallMethodAgentDirect
}
```

#### getNextOperation 增强

现在返回完整的安装配置:

```json
{
  "operation": "os_install",
  "data": {
    "install_method": "agent_direct",
    "os_type": "ubuntu",
    "os_version": "22.04",
    "mirror_url": "http://192.168.100.1:8081",
    "disk_layout": {
      "root_disk": "/dev/sda",
      "partition_table": "gpt",
      "partitions": [
        {"mount_point": "/boot", "size": "1G", "fstype": "ext4"},
        {"mount_point": "swap", "size": "16G", "fstype": "swap"},
        {"mount_point": "/", "size": "0", "fstype": "ext4"}
      ]
    },
    "network": {
      "interface": "eth0",
      "method": "static",
      "ip": "192.168.100.10",
      "netmask": "255.255.255.0",
      "gateway": "192.168.100.1",
      "dns": "192.168.100.1",
      "hostname": "server-01"
    },
    "packages": [
      "openssh-server",
      "wget",
      "curl",
      "vim",
      "net-tools"
    ]
  }
}
```

---

## 🔄 完整工作流程

### 方式 1: Kickstart 安装 (CentOS/Rocky)

```
1. Agent 完成 RAID 配置
   POST /api/v1/device/operationComplete
   {"operation": "hardware_config", "success": true}
   ↓
2. Agent 请求下一步操作
   POST /api/v1/device/getNextOperation
   {"sn": "SN123"}
   ↓
3. Regional Client 返回
   {
     "operation": "os_install",
     "data": {
       "install_method": "kickstart",
       "kickstart_url": "http://192.168.100.1:8081/api/v1/kickstart/SN123"
     }
   }
   ↓
4. Agent 下载 Kickstart 文件
   GET /api/v1/kickstart/SN123
   ↓
5. Agent 调用 kexec 重启到安装程序
   kexec -l /boot/vmlinuz --initrd=/boot/initrd.img \
     --append="ks=http://192.168.100.1:8081/api/v1/kickstart/SN123"
   kexec -e
   ↓
6. Anaconda 使用 Kickstart 自动安装
   - 分区
   - 安装软件包
   - 配置系统
   - 执行 %post 脚本
   ↓
7. Post 脚本通知完成
   curl -X POST http://192.168.100.1:8081/api/v1/device/installComplete \
     -d '{"sn":"SN123","status":"success"}'
   ↓
8. Regional Client 清理 PXE 配置
   ↓
9. 系统重启到新安装的 OS
```

### 方式 2: Agent 直接安装 (Ubuntu/Debian)

```
1. Agent 完成 RAID 配置
   POST /api/v1/device/operationComplete
   {"operation": "hardware_config", "success": true}
   ↓
2. Agent 请求下一步操作
   POST /api/v1/device/getNextOperation
   {"sn": "SN123"}
   ↓
3. Regional Client 返回完整安装配置
   {
     "operation": "os_install",
     "data": {
       "install_method": "agent_direct",
       "disk_layout": {...},
       "network": {...},
       "packages": [...]
     }
   }
   ↓
4. Agent 执行安装 (在 initramfs 中)
   a. 分区磁盘
      sgdisk -Z /dev/sda
      sgdisk -n 1:0:+1G /dev/sda (boot)
      sgdisk -n 2:0:+16G /dev/sda (swap)
      sgdisk -n 3:0:0 /dev/sda (root)

   b. 格式化
      mkfs.ext4 /dev/sda1
      mkswap /dev/sda2
      mkfs.ext4 /dev/sda3

   c. 挂载
      mount /dev/sda3 /mnt
      mkdir /mnt/boot
      mount /dev/sda1 /mnt/boot

   d. debootstrap 安装基础系统
      debootstrap jammy /mnt http://192.168.100.1:8081/repos/ubuntu

   e. chroot 配置系统
      chroot /mnt /bin/bash
      - 配置 hostname
      - 配置网络
      - 配置 fstab
      - 安装软件包
      - 安装 grub

   f. 执行 post script (如果有)

   g. 卸载并重启
   ↓
5. Agent 报告完成
   POST /api/v1/device/installComplete
   {"sn":"SN123","status":"success"}
   ↓
6. Regional Client 清理 PXE 配置
   ↓
7. 系统重启到新安装的 OS
```

---

## 📂 目录结构

```
/tftpboot/
├── pxelinux.0
├── pxelinux.cfg/
│   ├── default
│   └── 01-{mac-address}
├── static/
│   ├── kernels/
│   │   └── lpmos-vmlinuz
│   └── initramfs/
│       └── lpmos-agent-initramfs.gz
└── repos/
    ├── ubuntu/
    │   ├── 20.04/
    │   └── 22.04/
    ├── centos/
    │   ├── 7/
    │   └── 8/
    └── rocky/
        ├── 8/
        └── 9/
```

---

## 🎯 Kickstart 模板特点

### CentOS/Rocky Kickstart

**包含内容**:
- 网络配置（静态 IP）
- 磁盘分区
- 软件包选择
- Root 密码
- SELinux/防火墙配置
- %post 脚本
  - 网络配置持久化
  - 安装完成通知
  - 自定义脚本执行

**示例** (`centos-7.tmpl`):
```
#version=RHEL7
text
network --bootproto=static --device=eth0 --ip={{.IP}} --netmask={{.Netmask}}
rootpw --iscrypted {{.RootPasswordHash}}
url --url={{.RepoURL}}
bootloader --location=mbr --boot-drive={{.BootDisk}}
clearpart --all --drives={{.TargetDisks}} --initlabel
part /boot --fstype="ext4" --size=1024
part swap --fstype="swap" --size=16384
part / --fstype="ext4" --size=1 --grow

%post
curl -X POST "{{.RegionalURL}}/api/v1/device/installComplete" \
  -H "Content-Type: application/json" \
  -d '{"sn":"{{.SN}}","status":"success"}'
%end
```

### Ubuntu Preseed

**包含内容**:
- Locale/键盘配置
- 网络配置（静态 IP）
- 磁盘分区方案
- 账户配置
- 软件包选择
- Late command
  - 安装完成通知

**示例** (`ubuntu-20.04.tmpl`):
```
d-i netcfg/get_ipaddress string {{.IP}}
d-i netcfg/get_netmask string {{.Netmask}}
d-i netcfg/get_gateway string {{.Gateway}}
d-i passwd/root-password-crypted password {{.RootPasswordHash}}
d-i partman-auto/disk string {{.BootDisk}}
d-i partman-auto/method string regular

d-i preseed/late_command string \
    in-target curl -X POST "{{.RegionalURL}}/api/v1/device/installComplete" \
    -d '{"sn":"{{.SN}}","status":"success"}'
```

---

## 🧪 测试指南

### 1. 准备环境

```bash
# 1. 创建目录结构
sudo mkdir -p /tftpboot/{static/{kernels,initramfs},repos}

# 2. 准备 kernel 和 initramfs
# (需要先构建 initramfs，包含 agent)

# 3. 准备软件包仓库镜像 (可选)
sudo mkdir -p /tftpboot/repos/ubuntu/22.04
# 同步 Ubuntu 镜像或配置代理到公共镜像
```

### 2. 启动 Regional Client

```bash
sudo ./bin/regional-client \
  --idc=dc1 \
  --enable-dhcp \
  --enable-tftp \
  --server-ip=192.168.100.1 \
  --interface=eth1
```

### 3. 测试 Kickstart 生成

```bash
# 假设已有任务 SN123
curl http://192.168.100.1:8081/api/v1/kickstart/SN123

# 应该返回完整的 Kickstart 文件
```

### 4. 测试安装配置获取

```bash
curl -X POST http://192.168.100.1:8081/api/v1/device/getOSInstallConfig \
  -H "Content-Type: application/json" \
  -d '{"sn":"SN123"}'

# 应该返回完整的安装配置
```

---

## 📊 方案优势

### ✅ 灵活性
- 支持多种安装方式
- 根据场景自动选择最优方案
- 可轻松扩展新的 OS 支持

### ✅ 标准化
- 使用成熟的 Kickstart/Preseed
- 兼容传统安装流程
- 易于维护和调试

### ✅ 可控性
- Agent 直接安装提供完全控制
- 可处理复杂的磁盘配置
- 灵活的 post-install 脚本

### ✅ 完整性
- RAID 配置 → 系统安装 → 配置 → 清理
- 完整的生命周期管理
- 自动化程度高

---

## 🚀 下一步工作

### Agent 端实现 (下一个 PR)

需要实现:
1. **RAID 配置模块** (`cmd/agent-minimal/raid/`)
   - MegaCli 支持 (LSI RAID)
   - hpacucli 支持 (HP RAID)
   - mdadm 支持 (软 RAID)

2. **系统安装模块** (`cmd/agent-minimal/install/`)
   - Debian installer (debootstrap)
   - RHEL installer (dnf/yum)
   - 分区管理
   - 文件系统操作
   - Grub 安装

3. **Kickstart 安装模块** (`cmd/agent-minimal/kickstart/`)
   - 下载 kickstart 文件
   - kexec 重启到安装程序

### 测试和文档
1. 端到端测试
2. Agent 使用文档
3. 故障排查指南

---

## ✅ 总结

**已完成**:
- ✅ 数据模型定义
- ✅ Kickstart/Preseed 模板
- ✅ Regional Client API
- ✅ 安装决策逻辑
- ✅ 动态配置生成
- ✅ 安装完成通知

**编译状态**: ✅ 通过

**代码量**: 约 600+ 行 (不含模板)

**支持系统**: 6 种 OS (CentOS 7/8, Rocky 8/9, Ubuntu 20.04/22.04)

现在 Regional Client 端已经完全支持混合安装方案！接下来只需要实现 Agent 端的安装逻辑即可。
