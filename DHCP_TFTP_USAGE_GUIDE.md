# DHCP 和 TFTP 模块使用指南

## 目录

1. [快速开始](#快速开始)
2. [DHCP 服务器使用](#dhcp-服务器使用)
3. [TFTP 服务器使用](#tftp-服务器使用)
4. [集成使用 (DHCP + TFTP + PXE)](#集成使用)
5. [实际测试](#实际测试)
6. [常见问题](#常见问题)

---

## 快速开始

### 前置要求

1. **Root 权限**: DHCP (端口 67) 和 TFTP (端口 69) 需要 root 权限
2. **网络接口**: 确保网络接口存在且已配置 IP
3. **防火墙**: 确保端口 67 (DHCP) 和 69 (TFTP) 已开放

```bash
# 检查网络接口
ip addr show

# 开放防火墙端口 (CentOS/RHEL)
firewall-cmd --add-service=dhcp --permanent
firewall-cmd --add-service=tftp --permanent
firewall-cmd --reload

# 开放防火墙端口 (Ubuntu)
ufw allow 67/udp
ufw allow 69/udp
```

### 快速测试

```bash
# 1. 运行 DHCP 示例
cd examples
sudo go run dhcp-example.go

# 2. 运行 TFTP 示例 (另一个终端)
sudo go run tftp-example.go

# 3. 运行集成示例
sudo go run dhcp-tftp-pxe-integrated.go
```

---

## DHCP 服务器使用

### 基本配置

```go
import "github.com/yourusername/lpmos-go/cmd/regional-client/dhcp"

// 创建配置
config := dhcp.Config{
    Interface:  "eth1",                              // 网卡接口
    ServerIP:   "192.168.100.1",                     // DHCP 服务器 IP
    Gateway:    "192.168.100.1",                     // 网关
    DNSServers: []string{"192.168.100.1", "8.8.8.8"}, // DNS 服务器
    TFTPServer: "192.168.100.1",                     // TFTP 服务器地址
    BootFile:   "pxelinux.0",                        // PXE 启动文件
    LeaseTime:  3600 * time.Second,                  // 租约时间: 1 小时
    StartIP:    "192.168.100.10",                    // IP 池起始
    EndIP:      "192.168.100.200",                   // IP 池结束
    Netmask:    "255.255.255.0",                     // 子网掩码
}

// 创建并启动服务器
server, err := dhcp.NewServer(config)
if err != nil {
    log.Fatal(err)
}

if err := server.Start(); err != nil {
    log.Fatal(err)
}
```

### 静态 MAC-IP 绑定

```go
// 添加静态绑定
err := server.AddStaticBinding(
    "00:1a:2b:3c:4d:5e",    // MAC 地址
    "192.168.100.10",       // 固定 IP
    "server-01",            // 主机名
    "pxelinux.0",           // 启动文件
)

// 删除静态绑定
err := server.RemoveStaticBinding("00:1a:2b:3c:4d:5e")

// 获取所有静态绑定
bindings := server.GetStaticBindings()
for mac, binding := range bindings {
    fmt.Printf("%s -> %s (%s)\n", mac, binding.IP, binding.Hostname)
}
```

### 租约管理

```go
// 获取所有租约
leases := server.GetLeases()
for _, lease := range leases {
    fmt.Printf("MAC: %s, IP: %s, Hostname: %s, Expires: %s\n",
        lease.MAC,
        lease.IP,
        lease.Hostname,
        lease.ExpireTime.Format("2006-01-02 15:04:05"))
}
```

### 配置参数说明

| 参数 | 说明 | 示例 |
|-----|------|------|
| Interface | 网卡接口名称 | `eth1`, `ens33` |
| ServerIP | DHCP 服务器 IP 地址 | `192.168.100.1` |
| Gateway | 网关地址 | `192.168.100.1` |
| DNSServers | DNS 服务器列表 | `[]string{"8.8.8.8", "8.8.4.4"}` |
| TFTPServer | TFTP 服务器地址 (PXE 用) | `192.168.100.1` |
| BootFile | PXE 启动文件名 | `pxelinux.0` |
| LeaseTime | 租约时间 | `3600 * time.Second` (1 小时) |
| StartIP | IP 池起始地址 | `192.168.100.10` |
| EndIP | IP 池结束地址 | `192.168.100.200` |
| Netmask | 子网掩码 | `255.255.255.0` |

---

## TFTP 服务器使用

### 基本配置

```go
import "github.com/yourusername/lpmos-go/cmd/regional-client/tftp"

// 创建配置
config := tftp.Config{
    RootDir:    "/tftpboot",         // TFTP 根目录
    ListenAddr: ":69",               // 监听地址
    MaxClients: 100,                 // 最大并发客户端
    Timeout:    30 * time.Second,    // 传输超时
    BlockSize:  512,                 // 块大小 (标准 TFTP)
}

// 创建并启动服务器
server, err := tftp.NewServer(config)
if err != nil {
    log.Fatal(err)
}

if err := server.Start(); err != nil {
    log.Fatal(err)
}
```

### 文件管理

```go
// 创建文件管理器
fileManager := tftp.NewFileManager("/tftpboot")

// 初始化目录结构
// 自动创建: pxelinux.cfg/, kernels/, initrds/
if err := fileManager.EnsureDirectories(); err != nil {
    log.Fatal(err)
}

// 写入文件
content := []byte("Hello TFTP!")
err := fileManager.WriteFile("test.txt", content)

// 读取文件
data, err := fileManager.ReadFile("test.txt")

// 复制文件
err := fileManager.CopyFile("/path/to/source", "dest.txt")

// 删除文件
err := fileManager.DeleteFile("test.txt")

// 检查文件存在
exists := fileManager.FileExists("test.txt")

// 列出目录
files, err := fileManager.ListDirectory("kernels")
```

### 服务器管理

```go
// 列出所有文件
files, err := server.ListFiles()
for _, file := range files {
    fmt.Printf("%s - %d bytes\n", file.Name, file.Size)
}

// 检查文件存在
exists := server.FileExists("pxelinux.0")

// 获取文件大小
size, err := server.GetFileSize("pxelinux.0")

// 获取统计信息
stats := server.GetStats()
fmt.Printf("Total requests: %d\n", stats.TotalRequests)
fmt.Printf("Success: %d\n", stats.SuccessRequests)
fmt.Printf("Failed: %d\n", stats.FailedRequests)
fmt.Printf("Bytes served: %d\n", stats.TotalBytesServed)
```

### 配置参数说明

| 参数 | 说明 | 示例 |
|-----|------|------|
| RootDir | TFTP 根目录 | `/tftpboot` |
| ListenAddr | 监听地址和端口 | `:69`, `0.0.0.0:69` |
| MaxClients | 最大并发客户端数 | `100` |
| Timeout | 传输超时时间 | `30 * time.Second` |
| BlockSize | TFTP 块大小 | `512` (标准), `1024`, `1428` |

---

## 集成使用

### 完整的 PXE 启动环境

```go
import (
    "github.com/yourusername/lpmos-go/cmd/regional-client/dhcp"
    "github.com/yourusername/lpmos-go/cmd/regional-client/tftp"
    "github.com/yourusername/lpmos-go/cmd/regional-client/pxe"
)

func setupPXEEnvironment() error {
    // 1. 启动 TFTP 服务器
    tftpServer, err := tftp.NewServer(tftp.Config{
        RootDir:    "/tftpboot",
        ListenAddr: ":69",
        MaxClients: 100,
        Timeout:    30 * time.Second,
        BlockSize:  512,
    })
    if err != nil {
        return err
    }
    tftpServer.Start()

    // 2. 创建 PXE 生成器
    pxeGen, err := pxe.NewGenerator(pxe.Config{
        TFTPRoot: "/tftpboot",
    })
    if err != nil {
        return err
    }

    // 3. 启动 DHCP 服务器
    dhcpServer, err := dhcp.NewServer(dhcp.Config{
        Interface:  "eth1",
        ServerIP:   "192.168.100.1",
        Gateway:    "192.168.100.1",
        DNSServers: []string{"192.168.100.1"},
        TFTPServer: "192.168.100.1",
        BootFile:   "pxelinux.0",
        LeaseTime:  3600 * time.Second,
        StartIP:    "192.168.100.10",
        EndIP:      "192.168.100.200",
        Netmask:    "255.255.255.0",
    })
    if err != nil {
        return err
    }
    dhcpServer.Start()

    // 4. 为服务器配置 PXE 启动
    mac, _ := net.ParseMAC("00:1a:2b:3c:4d:5e")

    // 添加 DHCP 绑定
    dhcpServer.AddStaticBinding(
        mac.String(),
        "192.168.100.10",
        "server-01",
        "pxelinux.0",
    )

    // 生成 PXE 配置
    pxeGen.GenerateConfig(&pxe.BootConfig{
        MAC:          mac,
        IP:           net.ParseIP("192.168.100.10"),
        Hostname:     "server-01",
        OSType:       "ubuntu",
        OSVersion:    "22.04",
        KernelPath:   "/kernels/ubuntu-vmlinuz",
        InitrdPath:   "/initrds/ubuntu-initrd.img",
        RegionalURL:  "http://192.168.100.1:8080",
        SerialNumber: "SN123456789",
        DataCenter:   "dc1",
    })

    return nil
}
```

### Regional Client 集成示例

```go
type RegionalClient struct {
    dhcpServer   *dhcp.Server
    tftpServer   *tftp.Server
    pxeGenerator *pxe.Generator
}

func (rc *RegionalClient) Initialize() error {
    // 初始化 TFTP
    var err error
    rc.tftpServer, err = tftp.NewServer(tftp.Config{
        RootDir:    "/tftpboot",
        ListenAddr: ":69",
        MaxClients: 100,
        Timeout:    30 * time.Second,
        BlockSize:  512,
    })
    if err != nil {
        return err
    }

    // 初始化 PXE 生成器
    rc.pxeGenerator, err = pxe.NewGenerator(pxe.Config{
        TFTPRoot: "/tftpboot",
    })
    if err != nil {
        return err
    }

    // 初始化 DHCP
    rc.dhcpServer, err = dhcp.NewServer(dhcp.Config{
        Interface:  "eth1",
        ServerIP:   "192.168.100.1",
        Gateway:    "192.168.100.1",
        DNSServers: []string{"192.168.100.1"},
        TFTPServer: "192.168.100.1",
        BootFile:   "pxelinux.0",
        LeaseTime:  3600 * time.Second,
        StartIP:    "192.168.100.10",
        EndIP:      "192.168.100.200",
        Netmask:    "255.255.255.0",
    })
    if err != nil {
        return err
    }

    return nil
}

func (rc *RegionalClient) Start() error {
    if err := rc.tftpServer.Start(); err != nil {
        return err
    }
    if err := rc.dhcpServer.Start(); err != nil {
        return err
    }
    return nil
}

// 处理装机任务
func (rc *RegionalClient) HandleInstallTask(task *InstallTask) error {
    mac, _ := net.ParseMAC(task.MAC)

    // 1. 添加 DHCP 绑定
    err := rc.dhcpServer.AddStaticBinding(
        task.MAC,
        task.IP,
        task.Hostname,
        "pxelinux.0",
    )
    if err != nil {
        return err
    }

    // 2. 生成 PXE 配置
    err = rc.pxeGenerator.GenerateConfig(&pxe.BootConfig{
        MAC:          mac,
        IP:           net.ParseIP(task.IP),
        Hostname:     task.Hostname,
        OSType:       task.OSType,
        OSVersion:    task.OSVersion,
        KernelPath:   fmt.Sprintf("/kernels/%s-%s-vmlinuz", task.OSType, task.OSVersion),
        InitrdPath:   fmt.Sprintf("/initrds/%s-%s-initrd.img", task.OSType, task.OSVersion),
        RegionalURL:  "http://192.168.100.1:8080",
        SerialNumber: task.SerialNumber,
        DataCenter:   task.DataCenter,
    })
    if err != nil {
        return err
    }

    // 3. 配置交换机 (TODO: 实现交换机模块后)
    // rc.switchManager.ConfigurePort(task.SwitchPort, task.InstallVLAN)

    // 4. 控制 BMC 重启 (TODO: 实现 BMC 模块后)
    // rc.bmcController.SetBootDevice("pxe")
    // rc.bmcController.PowerCycle()

    return nil
}

// 清理装机配置
func (rc *RegionalClient) CleanupInstallTask(task *InstallTask) error {
    mac, _ := net.ParseMAC(task.MAC)

    // 1. 删除 PXE 配置
    if err := rc.pxeGenerator.RemoveConfig(mac); err != nil {
        return err
    }

    // 2. 删除 DHCP 绑定
    if err := rc.dhcpServer.RemoveStaticBinding(task.MAC); err != nil {
        return err
    }

    // 3. 配置交换机移出装机 VLAN
    // rc.switchManager.ConfigurePort(task.SwitchPort, task.ProductionVLAN)

    return nil
}
```

---

## 实际测试

### 1. 准备 PXE 启动文件

```bash
# 创建 TFTP 根目录
sudo mkdir -p /tftpboot/{pxelinux.cfg,kernels,initrds}

# 下载 PXE 启动文件 (Ubuntu 为例)
cd /tftpboot
sudo wget http://archive.ubuntu.com/ubuntu/dists/jammy/main/installer-amd64/current/legacy-images/netboot/pxelinux.0
sudo wget http://archive.ubuntu.com/ubuntu/dists/jammy/main/installer-amd64/current/legacy-images/netboot/ldlinux.c32
sudo wget http://archive.ubuntu.com/ubuntu/dists/jammy/main/installer-amd64/current/legacy-images/netboot/libcom32.c32
sudo wget http://archive.ubuntu.com/ubuntu/dists/jammy/main/installer-amd64/current/legacy-images/netboot/libutil.c32

# 下载内核和 initrd
cd kernels
sudo wget http://archive.ubuntu.com/ubuntu/dists/jammy/main/installer-amd64/current/legacy-images/netboot/ubuntu-installer/amd64/linux -O ubuntu-22.04-vmlinuz

cd ../initrds
sudo wget http://archive.ubuntu.com/ubuntu/dists/jammy/main/installer-amd64/current/legacy-images/netboot/ubuntu-installer/amd64/initrd.gz -O ubuntu-22.04-initrd.img

# 设置权限
sudo chmod -R 755 /tftpboot
```

### 2. 启动服务

```bash
# 运行集成示例
cd examples
sudo go run dhcp-tftp-pxe-integrated.go
```

### 3. 测试 TFTP

```bash
# 使用 tftp 命令测试
tftp localhost
> get test.txt
> quit

# 或使用 curl
curl -v tftp://localhost/test.txt

# 或使用 atftp
atftp --get -r test.txt -l /tmp/test.txt localhost
```

### 4. 测试 DHCP

```bash
# 方法 1: 使用 dhclient (需要另一台机器或虚拟机)
sudo dhclient -v eth0

# 方法 2: 使用 dhcping
dhcping -s 192.168.100.1

# 方法 3: 使用 nmap
nmap --script broadcast-dhcp-discover

# 方法 4: 查看服务器日志
# 服务器会输出 DHCP 请求日志
```

### 5. 测试 PXE 启动

**准备测试环境**:

1. 准备一台测试服务器或虚拟机
2. 确保服务器网卡与 DHCP/TFTP 服务器在同一网段
3. 在 BIOS/UEFI 中启用 PXE 启动
4. 设置启动顺序：Network Boot 优先

**启动流程**:

1. 服务器开机
2. 网卡发送 DHCP Discover
3. DHCP 服务器响应，分配 IP 和 TFTP 信息
4. 服务器通过 TFTP 下载 pxelinux.0
5. pxelinux.0 加载并读取配置文件 `01-{mac-address}`
6. 下载内核和 initrd
7. 启动操作系统安装程序

**查看日志**:

服务器运行时会输出详细日志：
```
[DHCP] DISCOVER from 00:1a:2b:3c:4d:5e
[DHCP] Static binding found: 00:1a:2b:3c:4d:5e -> 192.168.100.10
[DHCP] REQUEST from 00:1a:2b:3c:4d:5e for 192.168.100.10
[DHCP] ACK to 00:1a:2b:3c:4d:5e: 192.168.100.10
[TFTP] Request from 192.168.100.10:xxxxx: pxelinux.0
[TFTP] Transfer complete: pxelinux.0 (26828 bytes) to 192.168.100.10:xxxxx
[TFTP] Request from 192.168.100.10:xxxxx: pxelinux.cfg/01-00-1a-2b-3c-4d-5e
[TFTP] Transfer complete: pxelinux.cfg/01-00-1a-2b-3c-4d-5e (234 bytes)
[TFTP] Request from 192.168.100.10:xxxxx: /kernels/ubuntu-22.04-vmlinuz
[TFTP] Transfer complete: /kernels/ubuntu-22.04-vmlinuz (8912384 bytes)
```

---

## 常见问题

### Q1: 权限错误 "permission denied"

**问题**: 无法绑定端口 67 或 69

**解决方案**:
```bash
# 使用 sudo 运行
sudo go run main.go

# 或给二进制文件添加 cap
sudo setcap cap_net_bind_service=+ep ./regional-client
```

### Q2: DHCP 服务器无法启动

**问题**: 已有其他 DHCP 服务器运行

**解决方案**:
```bash
# 检查是否有其他 DHCP 服务
sudo netstat -ulnp | grep :67

# 停止系统 DHCP 服务
sudo systemctl stop dhcpd
sudo systemctl stop isc-dhcp-server
```

### Q3: TFTP 传输失败

**问题**: 防火墙阻止 TFTP 连接

**解决方案**:
```bash
# CentOS/RHEL
sudo firewall-cmd --add-service=tftp --permanent
sudo firewall-cmd --reload

# Ubuntu
sudo ufw allow 69/udp

# 临时禁用防火墙测试
sudo systemctl stop firewalld  # CentOS
sudo ufw disable               # Ubuntu
```

### Q4: PXE 启动找不到配置文件

**问题**: pxelinux.0 无法读取配置文件

**检查清单**:
1. 确认配置文件名格式正确: `01-{mac-address}`
   - MAC: `00:1a:2b:3c:4d:5e`
   - 文件: `01-00-1a-2b-3c-4d-5e` (注意是小写)
2. 确认文件权限: `chmod 644 /tftpboot/pxelinux.cfg/*`
3. 查看 TFTP 日志，确认请求的文件路径

### Q5: 如何调试 PXE 启动问题

**步骤**:

1. **检查 DHCP 响应**:
   ```bash
   # 在服务器上查看日志
   # 应该看到 DISCOVER, REQUEST, ACK 消息
   ```

2. **检查 TFTP 传输**:
   ```bash
   # 手动测试 TFTP
   tftp localhost
   > get pxelinux.0
   > get pxelinux.cfg/01-00-1a-2b-3c-4d-5e
   ```

3. **检查网络连通性**:
   ```bash
   # Ping DHCP/TFTP 服务器
   ping 192.168.100.1

   # 检查路由
   ip route
   ```

4. **启用 PXE 调试模式**:
   在 PXE 配置文件中添加:
   ```
   APPEND ... debug=1
   ```

### Q6: 如何支持 UEFI 启动

**修改配置**:

```go
// BIOS 启动使用 pxelinux.0
// UEFI 启动使用 grubx64.efi

config := dhcp.Config{
    // ... 其他配置
    BootFile: "pxelinux.0",  // BIOS
    // BootFile: "grubx64.efi",  // UEFI
}

// 或动态判断 (需要从 DHCP 请求中识别 UEFI/BIOS)
```

### Q7: 如何提高 TFTP 传输速度

**优化方法**:

1. **增大块大小**:
   ```go
   config := tftp.Config{
       BlockSize: 1428,  // 默认 512, 可增大到 1428
   }
   ```

2. **使用 SSD 存储**:
   将 `/tftpboot` 目录放在 SSD 上

3. **调整并发数**:
   ```go
   config := tftp.Config{
       MaxClients: 200,  // 增大并发数
   }
   ```

---

## 参考资料

- [DHCP RFC 2131](https://datatracker.ietf.org/doc/html/rfc2131)
- [TFTP RFC 1350](https://datatracker.ietf.org/doc/html/rfc1350)
- [PXE Specification](https://www.intel.com/content/dam/doc/product-specification/preboot-execution-environment-pxe-specification.pdf)
- [SYSLINUX Project](https://wiki.syslinux.org/)

---

**文档版本**: 1.0
**更新日期**: 2026-01-30
**作者**: LPMOS Team
