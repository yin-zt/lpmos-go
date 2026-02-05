# DHCP、TFTP、PXE 模块测试指南

## 📚 可用的测试示例

本目录包含 4 个独立的测试示例程序，用于测试 DHCP、TFTP 和 PXE 模块的功能。

### 1. DHCP 服务器测试
**文件**: `dhcp-example.go`

**功能**:
- 启动 DHCP 服务器
- 配置 IP 地址池 (192.168.100.10 - 192.168.100.200)
- 添加静态 MAC-IP 绑定
- 查看租约和绑定信息
- 每 30 秒输出服务器状态

**运行方法**:
```bash
cd examples
sudo go run dhcp-example.go
```

### 2. TFTP 服务器测试
**文件**: `tftp-example.go`

**功能**:
- 启动 TFTP 服务器 (端口 69)
- 自动创建目录结构 (pxelinux.cfg, kernels, initrds)
- 创建测试文件
- 列出所有可用文件
- 显示传输统计信息

**运行方法**:
```bash
cd examples
sudo go run tftp-example.go
```

**测试文件下载**:
```bash
# 在另一个终端
tftp localhost
> get test.txt
> quit

# 或使用 curl
curl -v tftp://localhost/test.txt
```

### 3. PXE 配置生成器测试
**文件**: `pxe-example.go`

**功能**:
- 生成默认 PXE 配置
- 为不同服务器生成专属 PXE 配置
- 支持多种操作系统 (Ubuntu, CentOS, Rocky Linux)
- 列出所有配置文件
- 演示配置管理 (删除、检查存在)

**运行方法**:
```bash
cd examples
go run pxe-example.go
```

**注意**: 此示例不需要 root 权限，因为只生成配置文件，不启动网络服务。

### 4. 集成测试 (DHCP + TFTP + PXE)
**文件**: `integrated-example.go`

**功能**:
- 同时启动 DHCP、TFTP 服务器
- 初始化 PXE 配置生成器
- 为 3 台服务器配置完整的 PXE 启动环境
- 监控服务器运行状态
- 显示配置摘要和统计信息

**运行方法**:
```bash
cd examples
sudo go run integrated-example.go
```

## 🔧 前置要求

### 1. Root 权限
DHCP (端口 67) 和 TFTP (端口 69) 需要 root 权限：
```bash
# 使用 sudo 运行
sudo go run dhcp-example.go
```

### 2. 网络接口
确保网络接口存在并已配置：
```bash
# 查看网络接口
ip addr show

# 如果使用的不是 eth1，修改代码中的接口名称
# 例如: Interface: "ens33"
```

### 3. 防火墙配置
开放必要的端口：

**CentOS/RHEL**:
```bash
sudo firewall-cmd --add-service=dhcp --permanent
sudo firewall-cmd --add-service=tftp --permanent
sudo firewall-cmd --reload
```

**Ubuntu**:
```bash
sudo ufw allow 67/udp
sudo ufw allow 69/udp
```

### 4. 停止系统 DHCP 服务
如果系统已有 DHCP 服务运行，需要先停止：
```bash
# CentOS/RHEL
sudo systemctl stop dhcpd

# Ubuntu
sudo systemctl stop isc-dhcp-server
```

### 5. 创建 TFTP 根目录
```bash
sudo mkdir -p /tftpboot
sudo chmod -R 755 /tftpboot
```

## 📝 测试场景

### 场景 1: 单独测试 DHCP

**目标**: 验证 DHCP 服务器可以正确分配 IP 地址

**步骤**:
1. 启动 DHCP 服务器:
   ```bash
   cd examples
   sudo go run dhcp-example.go
   ```

2. 在另一台机器或虚拟机上请求 DHCP:
   ```bash
   sudo dhclient -v eth0
   ```

3. 观察 DHCP 服务器日志:
   ```
   [DHCP] DISCOVER from 00:1a:2b:3c:4d:5e
   [DHCP] OFFER to 00:1a:2b:3c:4d:5e: 192.168.100.10
   [DHCP] REQUEST from 00:1a:2b:3c:4d:5e for 192.168.100.10
   [DHCP] ACK to 00:1a:2b:3c:4d:5e: 192.168.100.10
   ```

**预期结果**:
- 客户端获得 IP 地址
- DHCP 服务器显示租约信息
- 静态绑定的 MAC 地址获得指定的 IP

### 场景 2: 单独测试 TFTP

**目标**: 验证 TFTP 服务器可以正确传输文件

**步骤**:
1. 启动 TFTP 服务器:
   ```bash
   cd examples
   sudo go run tftp-example.go
   ```

2. 在另一个终端测试文件下载:
   ```bash
   # 方法 1: 使用 tftp 命令
   tftp localhost
   > get test.txt
   > quit

   # 方法 2: 使用 curl
   curl -v tftp://localhost/test.txt

   # 方法 3: 使用 atftp
   atftp --get -r test.txt -l /tmp/test.txt localhost
   ```

3. 观察 TFTP 服务器日志:
   ```
   [TFTP] Request from 127.0.0.1:xxxxx: test.txt
   [TFTP] Transfer complete: test.txt (37 bytes) to 127.0.0.1:xxxxx
   ```

**预期结果**:
- 成功下载 test.txt 文件
- TFTP 服务器显示传输统计
- 统计信息中成功请求数 +1

### 场景 3: 测试 PXE 配置生成

**目标**: 验证 PXE 配置生成器可以正确生成配置文件

**步骤**:
1. 运行 PXE 生成器:
   ```bash
   cd examples
   go run pxe-example.go
   ```

2. 检查生成的配置文件:
   ```bash
   ls -la /tftpboot/pxelinux.cfg/
   cat /tftpboot/pxelinux.cfg/default
   cat /tftpboot/pxelinux.cfg/01-00-1a-2b-3c-4d-5e
   ```

**预期结果**:
- 生成 default 配置文件
- 为每个 MAC 地址生成专属配置文件
- 配置文件包含正确的内核和 initrd 路径
- 配置文件包含启动参数

### 场景 4: 集成测试

**目标**: 验证 DHCP、TFTP、PXE 可以协同工作

**步骤**:
1. 启动集成环境:
   ```bash
   cd examples
   sudo go run integrated-example.go
   ```

2. 观察启动日志，确认所有组件正常:
   ```
   ✓ TFTP server started on :69
   ✓ PXE generator created
   ✓ DHCP server started on :67
   ✓ DHCP binding: 00:1a:2b:3c:4d:5e -> 192.168.100.10
   ✓ PXE config: /tftpboot/pxelinux.cfg/01-00:1a:2b:3c:4d:5e
   ```

3. 检查配置摘要，确认所有绑定和配置已生成

4. 等待 60 秒，观察状态监控输出

**预期结果**:
- DHCP、TFTP、PXE 全部启动成功
- 3 个静态绑定已添加
- 3 个 PXE 配置文件已生成
- 可以看到定期的状态报告

### 场景 5: 完整的 PXE 启动测试

**目标**: 使用真实服务器或虚拟机测试完整的 PXE 启动流程

**前置准备**:
1. 下载 PXE 启动文件:
   ```bash
   cd /tftpboot
   # Ubuntu 22.04 为例
   sudo wget http://archive.ubuntu.com/ubuntu/dists/jammy/main/installer-amd64/current/legacy-images/netboot/pxelinux.0
   sudo wget http://archive.ubuntu.com/ubuntu/dists/jammy/main/installer-amd64/current/legacy-images/netboot/ldlinux.c32
   ```

2. 下载内核和 initrd:
   ```bash
   cd /tftpboot/kernels
   sudo wget http://archive.ubuntu.com/ubuntu/dists/jammy/main/installer-amd64/current/legacy-images/netboot/ubuntu-installer/amd64/linux -O ubuntu-22.04-vmlinuz

   cd /tftpboot/initrds
   sudo wget http://archive.ubuntu.com/ubuntu/dists/jammy/main/installer-amd64/current/legacy-images/netboot/ubuntu-installer/amd64/initrd.gz -O ubuntu-22.04-initrd.img
   ```

**步骤**:
1. 启动集成环境:
   ```bash
   sudo go run integrated-example.go
   ```

2. 配置测试服务器/虚拟机:
   - 设置 MAC 地址为预配置的地址之一 (如 00:1a:2b:3c:4d:5e)
   - 在 BIOS/UEFI 中启用 PXE 启动
   - 设置网络启动为第一启动项

3. 启动服务器，观察启动流程:
   - DHCP 请求和响应
   - TFTP 文件下载
   - 内核加载
   - Initrd 加载

4. 观察集成环境的日志输出

**预期结果**:
- 服务器成功通过 PXE 启动
- DHCP 日志显示分配了正确的 IP
- TFTP 日志显示传输了所有必要的文件
- 服务器启动到操作系统安装界面

## 🐛 常见问题

### Q1: 权限错误
**错误**: `bind: permission denied`

**解决方案**: 使用 sudo 运行
```bash
sudo go run dhcp-example.go
```

### Q2: 端口已被占用
**错误**: `address already in use`

**解决方案**: 检查并停止占用端口的进程
```bash
# 检查端口 67 (DHCP)
sudo netstat -ulnp | grep :67
sudo systemctl stop dhcpd

# 检查端口 69 (TFTP)
sudo netstat -ulnp | grep :69
```

### Q3: 网络接口不存在
**错误**: `no such device`

**解决方案**: 检查并使用正确的接口名称
```bash
# 查看所有网络接口
ip addr show

# 修改代码中的接口名称
Interface: "ens33"  // 替换为实际的接口名称
```

### Q4: 防火墙阻止
**错误**: TFTP 客户端超时

**解决方案**: 临时禁用防火墙测试
```bash
# CentOS/RHEL
sudo systemctl stop firewalld

# Ubuntu
sudo ufw disable
```

### Q5: 目录权限问题
**错误**: `permission denied` 写入 /tftpboot

**解决方案**: 设置正确的目录权限
```bash
sudo mkdir -p /tftpboot
sudo chmod -R 755 /tftpboot
sudo chown -R $USER:$USER /tftpboot
```

## 📊 测试检查清单

- [ ] DHCP 服务器可以启动
- [ ] DHCP 可以分配 IP 地址
- [ ] DHCP 静态绑定工作正常
- [ ] TFTP 服务器可以启动
- [ ] TFTP 可以传输文件
- [ ] TFTP 统计信息正确
- [ ] PXE 配置文件生成正确
- [ ] PXE 配置包含正确的参数
- [ ] DHCP + TFTP + PXE 集成正常
- [ ] 服务器可以通过 PXE 启动

## 🎯 下一步

测试完成后，可以：
1. 集成到 Regional Client 主程序中
2. 实现 BMC 控制模块 (自动重启服务器)
3. 实现交换机管理模块 (自动配置 VLAN)
4. 实现完整的自动化装机流程

---

**测试指南版本**: 1.0
**更新日期**: 2026-01-30
**作者**: LPMOS Team
