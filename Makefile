.PHONY: build run clean start-etcd stop-etcd demo help linux linux-all mac

# Variables
BINARY_DIR=bin
CONTROL_PLANE_BINARY=$(BINARY_DIR)/control-plane
REGIONAL_CLIENT_BINARY=$(BINARY_DIR)/regional-client
AGENT_BINARY=$(BINARY_DIR)/agent-minimal

# Linux AMD64 binaries (生产环境)
CONTROL_PLANE_LINUX=$(BINARY_DIR)/control-plane-linux-amd64
REGIONAL_CLIENT_LINUX=$(BINARY_DIR)/regional-client-linux-amd64
AGENT_LINUX=$(BINARY_DIR)/agent-minimal-linux-amd64

# macOS ARM64 binaries (本地测试)
CONTROL_PLANE_MAC=$(BINARY_DIR)/control-plane-darwin-arm64
REGIONAL_CLIENT_MAC=$(BINARY_DIR)/regional-client-darwin-arm64
AGENT_MAC=$(BINARY_DIR)/agent-minimal-darwin-arm64

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOMOD=$(GOCMD) mod

# Build flags
LDFLAGS=-ldflags="-s -w"

# Build flags
LDFLAGS=-ldflags="-s -w"

# ============================================================================
# Linux AMD64 编译目标 (生产环境 - 交叉编译)
# ============================================================================

# 编译所有 Linux AMD64 可执行文件
linux-all: linux-control-plane linux-regional-client linux-agent
	@echo "✅ 所有 Linux AMD64 组件编译完成"

# 简化的 linux 目标 (常用)
linux: linux-regional-client linux-agent
	@echo "✅ Linux AMD64 主要组件编译完成"

# Linux Control Plane
linux-control-plane:
	@echo "构建 Control Plane (Linux AMD64)..."
	@mkdir -p $(BINARY_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(CONTROL_PLANE_LINUX) cmd/control-plane/main.go
	@echo "✅ Control Plane (Linux) 构建完成: $(CONTROL_PLANE_LINUX)"

# Linux Regional Client
linux-regional-client:
	@echo "构建 Regional Client (Linux AMD64)..."
	@mkdir -p $(BINARY_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(REGIONAL_CLIENT_LINUX) cmd/regional-client/main.go
	@echo "✅ Regional Client (Linux) 构建完成: $(REGIONAL_CLIENT_LINUX)"

# Linux Agent
linux-agent:
	@echo "构建 Agent (Linux AMD64)..."
	@mkdir -p $(BINARY_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(AGENT_LINUX) cmd/agent-minimal/main.go
	@echo "✅ Agent (Linux) 构建完成: $(AGENT_LINUX)"
	@ls -lh $(AGENT_LINUX)

# ============================================================================
# macOS ARM64 编译目标 (本地测试)
# ============================================================================

# 编译所有 macOS ARM64 可执行文件
mac: mac-control-plane mac-regional-client mac-agent
	@echo "✅ 所有 macOS ARM64 组件编译完成"

# macOS Control Plane
mac-control-plane:
	@echo "构建 Control Plane (macOS ARM64)..."
	@mkdir -p $(BINARY_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(CONTROL_PLANE_MAC) cmd/control-plane/main.go
	@echo "✅ Control Plane (macOS) 构建完成: $(CONTROL_PLANE_MAC)"

# macOS Regional Client
mac-regional-client:
	@echo "构建 Regional Client (macOS ARM64)..."
	@mkdir -p $(BINARY_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(REGIONAL_CLIENT_MAC) cmd/regional-client/main.go
	@echo "✅ Regional Client (macOS) 构建完成: $(REGIONAL_CLIENT_MAC)"

# macOS Agent
mac-agent:
	@echo "构建 Agent (macOS ARM64)..."
	@mkdir -p $(BINARY_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(AGENT_MAC) cmd/agent-minimal/main.go
	@echo "✅ Agent (macOS) 构建完成: $(AGENT_MAC)"
	@ls -lh $(AGENT_MAC)

# ============================================================================
# 兼容旧版构建命令 (使用当前平台设置)
# ============================================================================

# Build all binaries
build: build-control-plane build-regional-client build-agent

# Build control plane (v3 optimized architecture)
build-control-plane:
	@echo "构建 Control Plane (v3优化架构)..."
	@mkdir -p $(BINARY_DIR)
	$(GOBUILD) -o $(CONTROL_PLANE_BINARY) cmd/control-plane/main.go
	@echo "✅ Control Plane 构建完成: $(CONTROL_PLANE_BINARY)"

# Build regional client (v3 optimized architecture)
build-regional-client:
	@echo "构建 Regional Client (v3优化架构)..."
	@mkdir -p $(BINARY_DIR)
	$(GOBUILD) -o $(REGIONAL_CLIENT_BINARY) cmd/regional-client/main.go
	@echo "✅ Regional Client 构建完成: $(REGIONAL_CLIENT_BINARY)"

# Build agent
build-agent:
	@echo "构建 Agent..."
	@mkdir -p $(BINARY_DIR)
	CGO_ENABLED=0 $(GOBUILD) -ldflags="-s -w" -o $(AGENT_BINARY) cmd/agent-minimal/main.go
	@echo "✅ Agent 构建完成: $(AGENT_BINARY)"
	@ls -lh $(AGENT_BINARY)

# Clean build artifacts
clean:
	@echo "清理..."
	$(GOCLEAN)
	rm -rf $(BINARY_DIR)
	@echo "✅ 清理完成"

# Download dependencies
deps:
	@echo "下载依赖..."
	$(GOMOD) download
	$(GOMOD) tidy
	@echo "✅ 依赖下载完成"

# Run tests
test:
	@echo "运行测试..."
	$(GOTEST) -v ./...

# Run control plane
run:
	@echo "启动 Control Plane..."
	@echo "Dashboard: http://localhost:8080"
	@if [ ! -f $(CONTROL_PLANE_BINARY) ]; then echo "❌ 二进制文件不存在，请先运行 make build-control-plane"; exit 1; fi
	ETCD_ENDPOINTS=localhost:2379 API_PORT=8080 $(CONTROL_PLANE_BINARY)

# Run regional client (dc1)
run-regional:
	@echo "启动 Regional Client (dc1)..."
	@if [ ! -f $(REGIONAL_CLIENT_BINARY) ]; then echo "❌ 二进制文件不存在，请先运行 make build-regional-client"; exit 1; fi
	$(REGIONAL_CLIENT_BINARY) --idc=dc1 --api-port=8081

# Run regional client with DHCP+TFTP (dc1) - requires root
run-regional-full:
	@echo "启动 Regional Client (dc1) with DHCP+TFTP+PXE..."
	@if [ ! -f $(REGIONAL_CLIENT_BINARY) ]; then echo "❌ 二进制文件不存在，请先运行 make build-regional-client"; exit 1; fi
	@echo "⚠️  需要 root 权限 (DHCP 端口67, TFTP 端口69)"
	sudo $(REGIONAL_CLIENT_BINARY) --idc=dc1 --api-port=8081 --enable-dhcp --enable-tftp --server-ip=192.168.100.1 --interface=eth1

# Run regional client (dc2)
run-regional-dc2:
	@echo "启动 Regional Client (dc2)..."
	@if [ ! -f $(REGIONAL_CLIENT_BINARY) ]; then echo "❌ 二进制文件不存在，请先运行 make build-regional-client"; exit 1; fi
	$(REGIONAL_CLIENT_BINARY) --idc=dc2 --api-port=8082

# Run regional client with DHCP+TFTP (dc2) - requires root
run-regional-dc2-full:
	@echo "启动 Regional Client (dc2) with DHCP+TFTP+PXE..."
	@if [ ! -f $(REGIONAL_CLIENT_BINARY) ]; then echo "❌ 二进制文件不存在，请先运行 make build-regional-client"; exit 1; fi
	@echo "⚠️  需要 root 权限 (DHCP 端口67, TFTP 端口69)"
	sudo $(REGIONAL_CLIENT_BINARY) --idc=dc2 --api-port=8082 --enable-dhcp --enable-tftp --server-ip=192.168.200.1 --interface=eth2

# Run agent
run-agent:
	@echo "启动 Agent..."
	@if [ ! -f $(AGENT_BINARY) ]; then echo "❌ 二进制文件不存在，请先运行 make build-agent"; exit 1; fi
	$(AGENT_BINARY) --regional-url=http://localhost:8081

# Start etcd with Docker
start-etcd:
	@echo "启动 etcd..."
	docker run -d --name lpmos-etcd \
		-p 2379:2379 \
		-p 2380:2380 \
		quay.io/coreos/etcd:v3.5.12 \
		/usr/local/bin/etcd \
		--advertise-client-urls http://0.0.0.0:2379 \
		--listen-client-urls http://0.0.0.0:2379
	@echo "✅ etcd 已启动: localhost:2379"

# Stop etcd
stop-etcd:
	@echo "停止 etcd..."
	docker stop lpmos-etcd || true
	docker rm lpmos-etcd || true
	@echo "✅ etcd 已停止"

# Full demo setup
demo: start-etcd
	@echo "等待 etcd 准备就绪..."
	@sleep 3
	@echo ""
	@echo "================================================"
	@echo "  LPMOS 装机管理平台 - Demo环境就绪"
	@echo "================================================"
	@echo ""
	@echo "架构: v3优化架构"
	@echo "  ⚡ 10x更快的服务器添加"
	@echo "  ⚡ 2x更快的进度更新"
	@echo "  ⚡ 90%更少的watch流量"
	@echo "  ✅ 原子事务保证一致性"
	@echo "  ✅ Lease自动清理"
	@echo ""
	@echo "下一步操作："
	@echo "  1. Terminal 1: make run"
	@echo "  2. Terminal 2: make run-regional"
	@echo "  3. Terminal 3: make run-agent"
	@echo "  4. 浏览器访问: http://localhost:8080"
	@echo ""
	@echo "停止: make stop-etcd"
	@echo ""

# Format code
fmt:
	@echo "格式化代码..."
	$(GOCMD) fmt ./...


clean-mac:
	@echo "清理 macOS ARM64 构建产物..."
	rm -f $(BINARY_DIR)/control-plane-darwin-arm64
	rm -f $(BINARY_DIR)/regional-client-darwin-arm64
	rm -f $(BINARY_DIR)/agent-minimal-darwin-arm64
	@echo "✅ macOS ARM64 构建产物清理完成"

clean-linux:
	@echo "清理 Linux AMD64 构建产物..."
	rm -f $(BINARY_DIR)/control-plane-linux-amd64
	rm -f $(BINARY_DIR)/regional-client-linux-amd64
	rm -f $(BINARY_DIR)/agent-minimal-linux-amd64
	@echo "✅ Linux AMD64 构建产物清理完成"
# Help
help:
	@echo "LPMOS 装机管理平台 - Makefile 命令"
	@echo ""
	@echo "==================== 生产环境编译 (Linux AMD64) ===================="
	@echo "  make linux              - 编译 Regional Client + Agent (Linux AMD64)"
	@echo "  make linux-all          - 编译所有组件 (Linux AMD64)"
	@echo "  make linux-control-plane - 编译 Control Plane (Linux AMD64)"
	@echo "  make linux-regional-client - 编译 Regional Client (Linux AMD64)"
	@echo "  make linux-agent        - 编译 Agent (Linux AMD64)"
	@echo ""
	@echo "==================== 本地测试编译 (macOS ARM64) ===================="
	@echo "  make mac                - 编译所有组件 (macOS ARM64)"
	@echo "  make mac-control-plane  - 编译 Control Plane (macOS ARM64)"
	@echo "  make mac-regional-client - 编译 Regional Client (macOS ARM64)"
	@echo "  make mac-agent          - 编译 Agent (macOS ARM64)"
	@echo ""
	@echo "==================== 兼容旧版构建命令 ===================="
	@echo "  make build              - 构建所有组件 (使用当前系统设置)"
	@echo "  make build-control-plane - 构建 Control Plane"
	@echo "  make build-regional-client - 构建 Regional Client"
	@echo "  make build-agent        - 构建 Agent"
	@echo ""
	@echo "==================== 运行命令 ===================="
	@echo "  make run                - 启动 Control Plane (端口8080)"
	@echo "  make run-regional       - 启动 Regional Client DC1 (端口8081)"
	@echo "  make run-regional-full  - 启动 Regional Client DC1 with DHCP+TFTP+PXE (需要root)"
	@echo "  make run-regional-dc2   - 启动 Regional Client DC2 (端口8082)"
	@echo "  make run-regional-dc2-full - 启动 Regional Client DC2 with DHCP+TFTP+PXE (需要root)"
	@echo "  make run-agent          - 启动 Agent"
	@echo ""
	@echo "==================== 环境命令 ===================="
	@echo "  make start-etcd         - 启动 etcd"
	@echo "  make stop-etcd          - 停止 etcd"
	@echo "  make demo               - 一键启动Demo环境"
	@echo ""
	@echo "==================== 其他命令 ===================="
	@echo "  make clean              - 清理构建产物"
	@echo "  make deps               - 下载依赖"
	@echo "  make test               - 运行测试"
	@echo "  make fmt                - 格式化代码"
	@echo ""
	@echo "💡 生产环境编译: make linux"
	@echo "💡 本地测试: make mac"
	@echo "💡 快速开始: make demo"
	@echo "💡 完整PXE环境: make run-regional-full"
	@echo ""
	@echo "⚠️  注意: 交叉编译使用环境变量，不会修改全局 go env 设置"
	@echo ""
