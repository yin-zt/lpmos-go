# LPMOS PXE 自动装机完整测试指南

## 📋 目录

1. [环境准备](#环境准备)
2. [文件准备](#文件准备)
3. [启动 Regional Client](#启动-regional-client)
4. [测试 PXE 启动](#测试-pxe-启动)
5. [完整装机流程](#完整装机流程)
6. [故障排查](#故障排查)

---

## 🔧 环境准备

### 1. 服务器要求

**Regional Client 服务器**：
- OS: Linux (CentOS 7+, Ubuntu 18.04+, Rocky Linux 8+)
- CPU: 2 核心+
- 内存: 4GB+
- 磁盘: 100GB+ (用于存储镜像)
- 网络: 至少一个网卡，配置静态 IP

**目标机器**（待装机）：
- 支持 PXE 网络启动
- 与 Regional Client 在同一网段
- BIOS 设置为网络启动优先

### 2. 网络拓扑

```
┌─────────────────────────────────────────────────────┐
│                    网络: 192.168.246.0/24            │
├─────────────────────────────────────────────────────┤
│                                                      │
│  ┌──────────────────────┐      ┌─────────────────┐ │
│  │ Regional Client      │      │ 目标机器         │ │
│  │ 192.168.246.140      │◄────►│ PXE Boot        │ │
│  │                      │      │ (DHCP 获取 IP)   │ │
│  │ - DHCP Server (67)   │      │                 │ │
│  │ - TFTP Server (69)   │      │                 │ │
│  │ - HTTP Server (8081) │      │                 │ │
│  │ - etcd (2379)        │      │                 │ │
│  └──────────────────────┘      └─────────────────┘ │
│                                                      │
└─────────────────────────────────────────────────────┘
```

### 3. 防火墙配置

```bash
# CentOS/Rocky Linux
firewall-cmd --permanent --add-service=dhcp
firewall-cmd --permanent --add-port=69/udp    # TFTP
firewall-cmd --permanent --add-port=8081/tcp  # HTTP API
firewall-cmd --permanent --add-port=2379/tcp  # etcd
firewall-cmd --reload

# Ubuntu
ufw allow 67/udp    # DHCP
ufw allow 69/udp    # TFTP
ufw allow 8081/tcp  # HTTP API
ufw allow 2379/tcp  # etcd
```

### 4. 关闭冲突服务

```bash
# 停止系统自带的 DHCP 服务（如果有）
systemctl stop dhcpd
systemctl disable dhcpd

# 停止 dnsmasq（如果有）
systemctl stop dnsmasq
systemctl disable dnsmasq

# 检查端口占用
netstat -lnup | grep -E '67|69'
```

---

## 📦 文件准备

### 1. 目录结构

```bash
# 创建目录
mkdir -p /tftpboot/{pxelinux.cfg,static/{kernels,initramfs},repos}

# 目录结构
/tftpboot/
├── pxelinux.0                  # PXE 引导程序
├── ldlinux.c32                 # SYSLINUX 库文件
├── menu.c32                    # 菜单模块
├── libutil.c32                 # 工具库
├── pxelinux.cfg/
│   ├── default                 # 默认配置
│   └── 01-{mac}                # MAC 特定配置（自动生成）
├── static/
│   ├── kernels/
│   │   └── vmlinuz             # Linux 内核
│   └── initramfs/
│       └── lpmos-agent-initramfs.gz  # Agent initramfs
└── repos/
    ├── ubuntu/
    └── centos/
```

### 2. 下载 SYSLINUX 文件

```bash
# 方法 1: 从系统包安装
# CentOS/Rocky
yum install -y syslinux

# Ubuntu
apt-get install -y pxelinux syslinux-common

# 复制文件到 TFTP 根目录
cp /usr/share/syslinux/pxelinux.0 /tftpboot/
cp /usr/share/syslinux/ldlinux.c32 /tftpboot/
cp /usr/share/syslinux/menu.c32 /tftpboot/
cp /usr/share/syslinux/libutil.c32 /tftpboot/

# 方法 2: 手动下载
cd /tmp
wget https://mirrors.edge.kernel.org/pub/linux/utils/boot/syslinux/syslinux-6.03.tar.gz
tar -xzf syslinux-6.03.tar.gz
cp syslinux-6.03/bios/core/pxelinux.0 /tftpboot/
cp syslinux-6.03/bios/com32/elflink/ldlinux/ldlinux.c32 /tftpboot/
cp syslinux-6.03/bios/com32/menu/menu.c32 /tftpboot/
cp syslinux-6.03/bios/com32/libutil/libutil.c32 /tftpboot/
```

### 3. 准备 Linux Kernel 和 Initramfs

**选项 A: 使用现有系统的 kernel**（测试用）：
```bash
# 从当前系统复制
cp /boot/vmlinuz-$(uname -r) /tftpboot/static/kernels/vmlinuz
```

**选项 B: 从 ISO 提取**：
```bash
# 挂载 Ubuntu ISO
mkdir /mnt/iso
mount -o loop ubuntu-22.04-server-amd64.iso /mnt/iso

# 复制 kernel 和 initrd
cp /mnt/iso/casper/vmlinuz /tftpboot/static/kernels/vmlinuz-ubuntu-22.04
cp /mnt/iso/casper/initrd /tftpboot/static/initramfs/initrd-ubuntu-22.04

umount /mnt/iso
```

**选项 C: 构建 LPMOS Agent Initramfs**（生产用）：
```bash
# 编译 Agent
make linux-agent

# 构建 initramfs（需要 root 权限）
sudo ./scripts/build-initramfs.sh bin/agent-minimal-linux-amd64

# 输出文件
ls -lh /tftpboot/static/initramfs/lpmos-agent-initramfs.gz
```

### 4. 创建默认 PXE 配置

```bash
cat > /tftpboot/pxelinux.cfg/default << 'EOF'
DEFAULT menu.c32
PROMPT 0
TIMEOUT 300
ONTIMEOUT local

MENU TITLE LPMOS PXE Boot Menu
MENU BACKGROUND
MENU COLOR title 1;37;44 #ffffffff #00000000 std

LABEL local
    MENU LABEL ^Boot from local disk
    MENU DEFAULT
    LOCALBOOT 0

LABEL lpmos-test
    MENU LABEL LPMOS Test Boot (Ubuntu 22.04)
    KERNEL /static/kernels/vmlinuz-ubuntu-22.04
    APPEND initrd=/static/initramfs/initrd-ubuntu-22.04 boot=casper netboot=nfs nfsroot=192.168.246.140:/tftpboot/repos/ubuntu/22.04

LABEL lpmos-agent
    MENU LABEL LPMOS Agent Boot (Automated Installation)
    KERNEL /static/kernels/vmlinuz
    APPEND initrd=/static/initramfs/lpmos-agent-initramfs.gz REGIONAL_URL=http://192.168.246.140:8081 console=tty0 console=ttyS0,115200n8

EOF
```

### 5. 设置权限

```bash
chmod -R 755 /tftpboot
chown -R root:root /tftpboot
```

---

## 🚀 启动 Regional Client

### 1. 启动 etcd

```bash
# 使用 Docker
docker run -d --name lpmos-etcd \
  -p 2379:2379 \
  -p 2380:2380 \
  quay.io/coreos/etcd:v3.5.12 \
  /usr/local/bin/etcd \
  --advertise-client-urls http://0.0.0.0:2379 \
  --listen-client-urls http://0.0.0.0:2379

# 或使用系统服务
systemctl start etcd

# 验证
etcdctl endpoint health
```

### 2. 启动 Regional Client（完整模式）

```bash
# 使用 root 权限启动（DHCP/TFTP 需要）
sudo ./regional-client-linux-amd64 \
  --idc=mailong-test \
  --server-ip=192.168.246.140 \
  --interface=eth0 \
  --enable-dhcp \
  --enable-tftp \
  --static-root=/tftpboot
```

**预期日志输出**：
```
Starting LPMOS Regional Client v3.0 for IDC: mailong-test
Configuration: API Port=8081, Server IP=192.168.246.140, Interface=eth0, Static Root=/tftpboot
✓ Kickstart/Preseed generator initialized
✓ Static file directories ready: /tftpboot
✓ Regional Client registered to etcd: /os/region/mailong-test
[mailong-test] Heartbeat started (lease: xxx)
✓ TFTP server initialized and started
  TFTP Root: /tftpboot
  Listen: :69
✓ PXE generator initialized
✓ DHCP server initialized and started
  Interface: eth0
  Server IP: 192.168.246.140
  IP Range: 192.168.246.50 - 192.168.246.100
  Gateway: 192.168.246.1
  DNS: 192.168.246.140
  TFTP Server: 192.168.246.140
  Boot File: pxelinux.0
[mailong-test] Watching for new servers at: /os/region/mailong-test/servers/
[mailong-test] Watching for task updates at: /os/region/mailong-test/machines/
Regional client API listening on :8081
```

### 3. 验证服务状态

```bash
# 检查端口监听
netstat -lnup | grep -E '67|69|8081|2379'

# 预期输出
udp    0.0.0.0:67     # DHCP
udp    0.0.0.0:69     # TFTP
tcp    :::8081        # HTTP API
tcp    :::2379        # etcd

# 测试 TFTP
tftp 192.168.246.140 -c get pxelinux.0

# 测试 HTTP
curl http://192.168.246.140:8081/api/v1/files/static

# 查看 etcd 注册
etcdctl get /os/region/mailong-test --prefix
```

---

## 🖥️ 测试 PXE 启动

### 1. 物理机测试

**BIOS 设置**：
1. 进入 BIOS 设置（通常按 F2, F12, Del）
2. 找到 Boot Order（启动顺序）
3. 将 Network Boot / PXE Boot 设置为第一启动项
4. 保存并重启

**预期流程**：
```
1. 机器上电
   ↓
2. BIOS 初始化
   ↓
3. PXE ROM 启动
   ↓
4. 发送 DHCP Discover 广播
   ↓
5. Regional Client DHCP 响应
   - IP: 192.168.246.50
   - Next-Server: 192.168.246.140
   - Filename: pxelinux.0
   ↓
6. 通过 TFTP 下载 pxelinux.0
   ↓
7. 加载 SYSLINUX
   ↓
8. 读取 pxelinux.cfg/default
   ↓
9. 显示启动菜单
   ↓
10. 选择 "LPMOS Agent Boot"
   ↓
11. 下载 kernel 和 initramfs
   ↓
12. 启动到 initramfs
   ↓
13. Agent 启动并连接 Regional Client
```

### 2. 虚拟机测试（推荐）

**使用 VirtualBox**：
```bash
# 创建虚拟机
VBoxManage createvm --name "lpmos-test" --register
VBoxManage modifyvm "lpmos-test" \
  --memory 2048 \
  --cpus 2 \
  --nic1 bridged \
  --bridgeadapter1 eth0 \
  --boot1 net \
  --boot2 disk

# 启动虚拟机
VBoxManage startvm "lpmos-test"
```

**使用 QEMU/KVM**：
```bash
# 创建虚拟磁盘
qemu-img create -f qcow2 /var/lib/libvirt/images/lpmos-test.qcow2 20G

# 启动虚拟机（PXE 启动）
qemu-system-x86_64 \
  -m 2048 \
  -smp 2 \
  -boot n \
  -netdev bridge,id=net0,br=br0 \
  -device virtio-net-pci,netdev=net0 \
  -drive file=/var/lib/libvirt/images/lpmos-test.qcow2,format=qcow2 \
  -vnc :1
```

**使用 virt-manager**：
1. 创建新虚拟机
2. 选择 "Network Boot (PXE)"
3. 网络选择 "Bridge" 模式
4. 启动虚拟机

### 3. 查看 Regional Client 日志

启动目标机器后，Regional Client 应该显示：

```
[DHCP] Received DISCOVER from 00:1a:2b:3c:4d:5e
[DHCP] Offering IP 192.168.246.50 to 00:1a:2b:3c:4d:5e
[DHCP] Received REQUEST from 00:1a:2b:3c:4d:5e for 192.168.246.50
[DHCP] ACK sent to 00:1a:2b:3c:4d:5e (192.168.246.50)
[TFTP] Client 192.168.246.50 requested: pxelinux.0
[TFTP] Sending file: pxelinux.0 (size: 42KB)
[TFTP] Client 192.168.246.50 requested: ldlinux.c32
[TFTP] Client 192.168.246.50 requested: pxelinux.cfg/01-00-1a-2b-3c-4d-5e
[TFTP] Client 192.168.246.50 requested: pxelinux.cfg/default
[TFTP] Client 192.168.246.50 requested: /static/kernels/vmlinuz
[TFTP] Client 192.168.246.50 requested: /static/initramfs/lpmos-agent-initramfs.gz
```

---

## 🔄 完整装机流程

### 阶段 1: PXE 启动和硬件收集

1. **目标机器 PXE 启动**
2. **Agent 启动并上报硬件**

**Regional Client 日志**：
```
[mailong-test] Received hardware report from SERVER001 (MAC: 00:1a:2b:3c:4d:5e)
[mailong-test] New server detected: SERVER001 (status: pending)
[mailong-test] Heartbeat started for SERVER001 (lease: xxx)
```

**验证**：
```bash
# 查看服务器列表
etcdctl get /os/mailong-test/servers/SERVER001

# 应该看到
{
  "sn": "SERVER001",
  "mac": "00:1a:2b:3c:4d:5e",
  "status": "pending",
  "added_at": "2026-02-04T14:00:00Z"
}
```

### 阶段 2: 创建安装任务

```bash
# 方法 1: 直接在 etcd 创建任务（测试用）
etcdctl put /os/mailong-test/machines/SERVER001 '{
  "task_id": "task-001",
  "sn": "SERVER001",
  "mac": "00:1a:2b:3c:4d:5e",
  "ip": "192.168.246.50",
  "hostname": "test-server-001",
  "os_type": "ubuntu",
  "os_version": "22.04",
  "status": "pending",
  "progress": [],
  "logs": [],
  "created_at": "2026-02-04T14:00:00Z",
  "updated_at": "2026-02-04T14:00:00Z"
}'

# 方法 2: 通过 Control Plane API（生产用）
curl -X POST http://192.168.246.140:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "idc": "mailong-test",
    "sn": "SERVER001",
    "mac": "00:1a:2b:3c:4d:5e",
    "os_type": "ubuntu",
    "os_version": "22.04"
  }'
```

### 阶段 3: Agent 执行安装

**Agent 会自动**：
1. 轮询检查是否在安装队列
2. 获取下一步操作
3. 执行 RAID 配置（如果需要）
4. 执行系统安装
5. 报告完成

**Regional Client 日志**：
```
[mailong-test] Task update for SERVER001: status=installing
[mailong-test] Progress update from SERVER001: hardware_config (100%)
[mailong-test] Progress update from SERVER001: os_install (50%)
[mailong-test] Progress update from SERVER001: os_install (100%)
[mailong-test] Installation complete for SERVER001
```

### 阶段 4: 验证安装

```bash
# 查看任务状态
etcdctl get /os/mailong-test/machines/SERVER001

# 应该看到 status: "completed"
```

---

## 🐛 故障排查

### 问题 1: 目标机器无法获取 IP

**症状**：
- PXE ROM 显示 "PXE-E51: No DHCP or proxyDHCP offers were received"
- 或一直在 "Searching for DHCP server..."

**排查**：
```bash
# 1. 检查 DHCP 服务是否运行
netstat -lnup | grep 67

# 2. 检查网络接口
ip addr show eth0

# 3. 检查防火墙
firewall-cmd --list-all

# 4. 抓包查看 DHCP 请求
tcpdump -i eth0 -n port 67 or port 68

# 5. 查看 Regional Client 日志
# 应该看到 DHCP DISCOVER 消息
```

**解决**：
- 确保 Regional Client 以 root 权限运行
- 确保网络接口正确（--interface=eth0）
- 确保防火墙开放 UDP 67 端口
- 确保没有其他 DHCP 服务器冲突

### 问题 2: TFTP 下载失败

**症状**：
- "PXE-E32: TFTP open timeout"
- "PXE-E3B: TFTP Error - File not found"

**排查**：
```bash
# 1. 检查 TFTP 服务
netstat -lnup | grep 69

# 2. 手动测试 TFTP
tftp 192.168.246.140 -c get pxelinux.0

# 3. 检查文件权限
ls -la /tftpboot/pxelinux.0

# 4. 检查文件是否存在
ls -la /tftpboot/static/kernels/
```

**解决**：
- 确保文件存在且权限正确（chmod 755）
- 确保 TFTP 根目录正确（--static-root=/tftpboot）
- 确保防火墙开放 UDP 69 端口

### 问题 3: Kernel 启动失败

**症状**：
- "Kernel panic - not syncing"
- "Unable to mount root fs"

**排查**：
```bash
# 1. 检查 kernel 和 initramfs 是否匹配
file /tftpboot/static/kernels/vmlinuz
file /tftpboot/static/initramfs/lpmos-agent-initramfs.gz

# 2. 检查 kernel 参数
cat /tftpboot/pxelinux.cfg/default
```

**解决**：
- 确保 kernel 和 initramfs 版本匹配
- 检查 APPEND 行的参数是否正确
- 确保 REGIONAL_URL 参数正确

### 问题 4: Agent 无法连接 Regional Client

**症状**：
- Agent 启动但无法上报硬件
- Regional Client 没有收到硬件报告

**排查**：
```bash
# 1. 检查网络连通性
ping 192.168.246.140

# 2. 检查 API 端口
curl http://192.168.246.140:8081/api/v1/files/static

# 3. 查看 Agent 日志（在串口或 VGA 输出）
# 应该看到 "Connecting to Regional Client..."
```

**解决**：
- 确保 REGIONAL_URL 参数正确
- 确保防火墙开放 TCP 8081 端口
- 检查 DNS 解析（如果使用域名）

### 问题 5: PXE 菜单不显示

**症状**：
- 下载 pxelinux.0 后黑屏
- 或显示 "Boot failed"

**排查**：
```bash
# 1. 检查 SYSLINUX 文件
ls -la /tftpboot/*.c32

# 2. 检查配置文件
cat /tftpboot/pxelinux.cfg/default

# 3. 查看 TFTP 日志
# Regional Client 应该显示下载了哪些文件
```

**解决**：
- 确保所有 .c32 文件都存在
- 确保 pxelinux.cfg/default 语法正确
- 使用 SYSLINUX 6.03 版本（推荐）

---

## 📊 监控和调试

### 实时监控 DHCP/TFTP

```bash
# 终端 1: 监控 DHCP
tcpdump -i eth0 -n port 67 or port 68 -v

# 终端 2: 监控 TFTP
tcpdump -i eth0 -n port 69 -v

# 终端 3: 查看 Regional Client 日志
tail -f /var/log/regional-client.log
```

### 查看 etcd 数据

```bash
# 查看所有服务器
etcdctl get /os/mailong-test/servers --prefix

# 查看所有任务
etcdctl get /os/mailong-test/machines --prefix

# 查看 Regional Client 状态
etcdctl get /os/region/mailong-test --prefix

# 实时监听变化
etcdctl watch /os/mailong-test --prefix
```

### 性能统计

```bash
# DHCP 租约统计
curl http://192.168.246.140:8081/api/v1/pxe/dhcp/leases | jq .

# TFTP 传输统计
curl http://192.168.246.140:8081/api/v1/pxe/tftp/stats | jq .
```

---

## ✅ 测试检查清单

### 启动前检查

- [ ] etcd 正在运行
- [ ] 防火墙端口已开放 (67, 69, 8081, 2379)
- [ ] 没有其他 DHCP 服务器冲突
- [ ] /tftpboot 目录结构正确
- [ ] pxelinux.0 和 .c32 文件存在
- [ ] kernel 和 initramfs 文件存在
- [ ] 网络接口配置正确

### 启动后检查

- [ ] Regional Client 成功启动
- [ ] DHCP 服务监听在 UDP 67
- [ ] TFTP 服务监听在 UDP 69
- [ ] HTTP API 监听在 TCP 8081
- [ ] etcd 中有 Regional Client 注册信息
- [ ] 可以通过 TFTP 下载文件
- [ ] 可以通过 HTTP 访问文件列表

### PXE 启动检查

- [ ] 目标机器获取到 IP 地址
- [ ] 目标机器下载 pxelinux.0
- [ ] 显示 PXE 启动菜单
- [ ] 可以下载 kernel 和 initramfs
- [ ] Agent 成功启动
- [ ] Agent 上报硬件信息
- [ ] etcd 中有服务器记录

### 安装流程检查

- [ ] 任务创建成功
- [ ] Agent 检测到任务
- [ ] RAID 配置成功（如果需要）
- [ ] 系统安装成功
- [ ] 安装完成通知
- [ ] 任务状态更新为 completed

---

## 🎉 成功标志

当你看到以下输出时，说明 PXE 装机环境已经成功搭建：

**Regional Client 日志**：
```
✓ DHCP server initialized and started
✓ TFTP server initialized and started
[DHCP] ACK sent to 00:1a:2b:3c:4d:5e (192.168.246.50)
[TFTP] Sending file: pxelinux.0
[TFTP] Sending file: /static/kernels/vmlinuz
[TFTP] Sending file: /static/initramfs/lpmos-agent-initramfs.gz
[mailong-test] Received hardware report from SERVER001
[mailong-test] New server detected: SERVER001 (status: pending)
```

**目标机器屏幕**：
```
LPMOS PXE Boot Menu
-------------------
1. Boot from local disk
2. LPMOS Test Boot (Ubuntu 22.04)
3. LPMOS Agent Boot (Automated Installation)

Select option: _
```

**etcd 数据**：
```bash
$ etcdctl get /os/mailong-test/servers/SERVER001
{
  "sn": "SERVER001",
  "mac": "00:1a:2b:3c:4d:5e",
  "status": "pending",
  "added_at": "2026-02-04T14:00:00Z"
}
```

恭喜！你的 LPMOS PXE 自动装机环境已经就绪！🎉

---

## 📚 相关文档

- [HTTP 静态文件服务](HTTP_STATIC_FILES.md)
- [Regional Client 注册](REGIONAL_CLIENT_REGISTRATION.md)
- [Agent 实现](AGENT_IMPLEMENTATION.md)
- [完整实现总结](IMPLEMENTATION_SUMMARY.md)
