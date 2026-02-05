# LPMOS 编译指南

## 📦 交叉编译说明

本项目支持在 **macOS ARM64** 上交叉编译 **Linux AMD64** 可执行文件，无需修改全局 Go 环境变量。

## 🚀 快速开始

### 生产环境编译 (Linux AMD64)

适用于部署到 Linux 服务器：

```bash
# 编译主要组件 (Regional Client + Agent)
make linux

# 编译所有组件 (包括 Control Plane)
make linux-all

# 单独编译特定组件
make linux-regional-client
make linux-agent
make linux-control-plane
```

**生成文件**:
- `bin/regional-client-linux-amd64`
- `bin/agent-minimal-linux-amd64`
- `bin/control-plane-linux-amd64`

### 本地测试编译 (macOS ARM64)

适用于在 Mac 上本地测试：

```bash
# 编译所有组件
make mac

# 单独编译特定组件
make mac-regional-client
make mac-agent
make mac-control-plane
```

**生成文件**:
- `bin/regional-client-darwin-arm64`
- `bin/agent-minimal-darwin-arm64`
- `bin/control-plane-darwin-arm64`

## 🔧 常用命令

### 编译命令

| 命令 | 说明 | 输出文件 |
|------|------|---------|
| `make linux` | 编译 Linux 主要组件 | regional-client + agent (Linux AMD64) |
| `make linux-all` | 编译所有 Linux 组件 | 所有组件 (Linux AMD64) |
| `make mac` | 编译 macOS 所有组件 | 所有组件 (macOS ARM64) |
| `make build` | 使用当前系统设置编译 | 使用 go env 的 GOOS/GOARCH |

### 清理和依赖

```bash
# 清理编译产物
make clean

# 下载和整理依赖
make deps

# 格式化代码
make fmt

# 运行测试
make test
```

### 查看帮助

```bash
make help
```

## 📝 技术细节

### 交叉编译原理

Makefile 使用**环境变量**而不是 `go env -w` 来设置交叉编译参数：

```makefile
# ✅ 正确方式 (使用环境变量)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o output main.go

# ❌ 错误方式 (修改全局设置)
go env -w GOOS=linux GOARCH=amd64
go build -o output main.go
go env -w GOOS=darwin GOARCH=arm64  # 需要手动恢复
```

**优势**:
1. ✅ 不修改全局 Go 环境
2. ✅ 无需手动恢复设置
3. ✅ 多个 make 命令可并行执行
4. ✅ 更安全可靠

### 编译标志说明

```makefile
CGO_ENABLED=0    # 禁用 CGO (生成静态链接二进制)
GOOS=linux       # 目标操作系统
GOARCH=amd64     # 目标架构
-ldflags="-s -w" # 去除调试信息，减小文件大小
```

**CGO_ENABLED=0 的作用**:
- 生成完全静态链接的可执行文件
- 无需依赖 C 库 (libc)
- 可以在任何 Linux 发行版上运行
- 适合打包到 initramfs

## 🎯 使用场景

### 场景 1: 在 Mac 上开发，部署到 Linux 服务器

```bash
# 在 Mac 上编译 Linux 版本
make linux

# 复制到服务器
scp bin/regional-client-linux-amd64 user@server:/usr/local/bin/regional-client
scp bin/agent-minimal-linux-amd64 user@server:/usr/local/bin/agent-minimal

# 在服务器上运行
ssh user@server "sudo /usr/local/bin/regional-client --idc=dc1"
```

### 场景 2: 构建 Initramfs

```bash
# 编译 Agent (Linux AMD64)
make linux-agent

# 构建 initramfs
sudo ./scripts/build-initramfs.sh bin/agent-minimal-linux-amd64

# 输出
# /tftpboot/static/initramfs/lpmos-agent-initramfs.gz
```

### 场景 3: 本地 Mac 测试

```bash
# 编译 macOS 版本
make mac

# 启动 etcd
make start-etcd

# 运行 Regional Client (使用 macOS 二进制)
./bin/regional-client-darwin-arm64 --idc=dc1

# 运行 Agent (使用 macOS 二进制)
./bin/agent-minimal-darwin-arm64 --regional-url=http://localhost:8081
```

### 场景 4: 批量编译多平台

```bash
# 一次性编译所有平台
make linux-all mac

# 查看生成的文件
ls -lh bin/

# 输出示例:
# bin/agent-minimal-darwin-arm64
# bin/agent-minimal-linux-amd64
# bin/control-plane-darwin-arm64
# bin/control-plane-linux-amd64
# bin/regional-client-darwin-arm64
# bin/regional-client-linux-amd64
```

## 🔍 验证编译结果

### 检查文件类型

```bash
# Linux 二进制文件
file bin/agent-minimal-linux-amd64
# 输出: ELF 64-bit LSB executable, x86-64, statically linked

# macOS 二进制文件
file bin/agent-minimal-darwin-arm64
# 输出: Mach-O 64-bit arm64 executable
```

### 检查文件大小

```bash
ls -lh bin/

# 典型大小:
# agent-minimal:       5-6 MB (静态链接)
# regional-client:     16-18 MB (包含模板)
# control-plane:       12-14 MB
```

### 测试 Linux 二进制 (使用 Docker)

```bash
# 在 Docker 容器中测试 Linux 二进制
docker run --rm -v $(pwd)/bin:/app alpine:latest /app/agent-minimal-linux-amd64 --help

# 应该正常显示帮助信息
```

## ⚠️ 常见问题

### Q1: 为什么我的 go env 显示 GOOS=linux？

A: 如果你之前使用过 `go env -w GOOS=linux`，需要恢复：

```bash
# 恢复 macOS 设置
go env -w GOOS=darwin
go env -w GOARCH=arm64

# 或者使用 unset (推荐)
go env -u GOOS
go env -u GOARCH
```

但是使用 Makefile 编译时**无需关心**全局设置，因为 Makefile 使用临时环境变量。

### Q2: 编译后的二进制文件能在哪些系统上运行？

A: Linux AMD64 二进制文件可以在以下系统运行：
- ✅ Ubuntu 18.04+
- ✅ Debian 9+
- ✅ CentOS 7+
- ✅ Rocky Linux 8+
- ✅ RHEL 7+
- ✅ Alpine Linux

因为使用了 `CGO_ENABLED=0`，生成的是静态链接二进制。

### Q3: 为什么有些命令需要 root 权限？

A: 以下场景需要 root 权限：
- ✅ DHCP 服务器 (端口 67)
- ✅ TFTP 服务器 (端口 69)
- ✅ 构建 initramfs (访问系统目录)

编译本身**不需要** root 权限。

### Q4: 如何验证编译环境？

```bash
# 检查 Go 版本
go version
# 需要: go1.21 或更高

# 检查当前 GOOS/GOARCH
go env GOOS GOARCH

# 测试交叉编译
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/test main.go
file /tmp/test
```

## 📚 更多信息

- [完整实现文档](IMPLEMENTATION_SUMMARY.md)
- [Regional Client 文档](HYBRID_INSTALL_IMPLEMENTATION.md)
- [Agent 文档](AGENT_IMPLEMENTATION.md)

## 🎉 快速参考

```bash
# 最常用的命令
make linux              # 编译生产环境版本
make clean              # 清理
make help               # 查看所有命令

# 完整工作流
make clean              # 清理旧文件
make deps               # 更新依赖
make linux              # 编译 Linux 版本
make test               # 运行测试

# 生成的文件路径
bin/regional-client-linux-amd64
bin/agent-minimal-linux-amd64
```

---

**提示**: 使用 `make help` 查看完整命令列表！
