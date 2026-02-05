# 在内存系统中手动下载和运行 Agent

## 场景

当你通过 PXE 启动进入一个 Live 系统（如 Ubuntu Live CD 或自定义 initramfs）后，需要手动下载并运行 LPMOS Agent 来进行系统安装。

## 前提条件

1. 已经通过 PXE 启动进入内存系统
2. 网络已配置（通常 DHCP 自动配置）
3. Regional Client 正在运行（提供 HTTP 服务）

## 方法 1：从 Regional Client 下载（推荐）

### 步骤 1：验证网络连接

```bash
# 检查 IP 地址
ip addr show

# 测试连接 Regional Client
ping -c 3 192.168.246.140

# 测试 HTTP 服务
curl -I http://192.168.246.140:8081/health
```

### 步骤 2：下载 Agent 二进制文件

Regional Client 应该提供 Agent 二进制文件的下载。有两种方式：

**方式 A：通过 HTTP 静态文件服务**

```bash
# 下载 Agent（假设 Regional Client 提供了静态文件）
wget http://192.168.246.140:8081/static/agent/lpmos-agent -O /tmp/lpmos-agent

# 或使用 curl
curl -o /tmp/lpmos-agent http://192.168.246.140:8081/static/agent/lpmos-agent

# 添加执行权限
chmod +x /tmp/lpmos-agent
```

**方式 B：通过专用 API 端点**

如果 Regional Client 提供了专用的 Agent 下载端点：

```bash
# 下载 Agent
curl -o /tmp/lpmos-agent http://192.168.246.140:8081/api/v1/agent/download

# 添加执行权限
chmod +x /tmp/lpmos-agent
```

### 步骤 3：运行 Agent

```bash
# 运行 Agent（前台模式，查看输出）
/tmp/lpmos-agent \
  --regional-url=http://192.168.246.140:8081 \
  --sn=$(dmidecode -s system-serial-number) \
  --mode=install

# 或后台运行
nohup /tmp/lpmos-agent \
  --regional-url=http://192.168.246.140:8081 \
  --sn=$(dmidecode -s system-serial-number) \
  --mode=install > /tmp/agent.log 2>&1 &

# 查看日志
tail -f /tmp/agent.log
```

## 方法 2：从外部服务器下载

如果 Regional Client 没有提供 Agent 下载，可以从其他服务器下载：

```bash
# 从 GitHub Releases 下载（示例）
wget https://github.com/your-org/lpmos-go/releases/download/v1.0.0/agent-linux-amd64 -O /tmp/lpmos-agent

# 或从内部文件服务器下载
wget http://your-file-server/lpmos-agent -O /tmp/lpmos-agent

# 添加执行权限
chmod +x /tmp/lpmos-agent

# 运行
/tmp/lpmos-agent --regional-url=http://192.168.246.140:8081
```

## 方法 3：使用 initramfs 内置的 Agent

如果你构建了包含 Agent 的 initramfs，Agent 应该已经在系统中：

```bash
# 查找 Agent
which lpmos-agent
# 或
find / -name "lpmos-agent" -type f 2>/dev/null

# 如果找到，直接运行
lpmos-agent --regional-url=http://192.168.246.140:8081
```

## Agent 参数说明

```bash
/tmp/lpmos-agent \
  --regional-url=http://192.168.246.140:8081 \  # Regional Client 的 URL（必需）
  --sn=SERVER001 \                               # 服务器序列号（可选，自动检测）
  --mac=00:0c:29:88:53:51 \                      # MAC 地址（可选，自动检测）
  --mode=install \                               # 运行模式：install（安装）或 report（仅上报硬件）
  --log-level=debug                              # 日志级别：debug, info, warn, error
```

## 自动检测硬件信息

Agent 可以自动检测硬件信息，但如果需要手动获取：

```bash
# 获取序列号
dmidecode -s system-serial-number

# 获取 MAC 地址
ip link show | grep -A 1 "state UP" | grep "link/ether" | awk '{print $2}'

# 获取主机名
hostname

# 获取 IP 地址
ip -4 addr show | grep inet | grep -v 127.0.0.1 | awk '{print $2}' | cut -d/ -f1
```

## 完整示例脚本

创建一个自动化脚本：

```bash
#!/bin/bash
# download-and-run-agent.sh

set -e

REGIONAL_URL="http://192.168.246.140:8081"
AGENT_PATH="/tmp/lpmos-agent"

echo "=== LPMOS Agent 自动下载和运行 ==="

# 1. 检查网络
echo "检查网络连接..."
if ! ping -c 1 192.168.246.140 > /dev/null 2>&1; then
    echo "错误：无法连接到 Regional Client (192.168.246.140)"
    exit 1
fi
echo "✓ 网络连接正常"

# 2. 下载 Agent
echo "下载 Agent..."
if wget -q ${REGIONAL_URL}/static/agent/lpmos-agent -O ${AGENT_PATH}; then
    echo "✓ Agent 下载成功"
elif curl -s -o ${AGENT_PATH} ${REGIONAL_URL}/static/agent/lpmos-agent; then
    echo "✓ Agent 下载成功（使用 curl）"
else
    echo "错误：无法下载 Agent"
    exit 1
fi

# 3. 添加执行权限
chmod +x ${AGENT_PATH}

# 4. 获取硬件信息
SN=$(dmidecode -s system-serial-number 2>/dev/null || echo "UNKNOWN")
MAC=$(ip link show | grep -A 1 "state UP" | grep "link/ether" | awk '{print $2}' | head -1)

echo "硬件信息："
echo "  序列号: ${SN}"
echo "  MAC 地址: ${MAC}"

# 5. 运行 Agent
echo "启动 Agent..."
${AGENT_PATH} \
  --regional-url=${REGIONAL_URL} \
  --sn=${SN} \
  --mac=${MAC} \
  --mode=install \
  --log-level=info

echo "Agent 已启动"
```

使用方法：

```bash
# 下载脚本
wget http://192.168.246.140:8081/static/scripts/download-and-run-agent.sh -O /tmp/run.sh

# 或直接执行
bash <(curl -s http://192.168.246.140:8081/static/scripts/download-and-run-agent.sh)
```

## 故障排查

### 问题 1：无法下载 Agent

```bash
# 检查 Regional Client 是否提供文件
curl http://192.168.246.140:8081/api/v1/files/static

# 查看可用文件列表
curl http://192.168.246.140:8081/api/v1/files/static | jq .
```

### 问题 2：Agent 无法连接 Regional Client

```bash
# 测试 API 连接
curl http://192.168.246.140:8081/health

# 检查防火墙
iptables -L -n | grep 8081
```

### 问题 3：Agent 运行失败

```bash
# 查看详细日志
/tmp/lpmos-agent --regional-url=http://192.168.246.140:8081 --log-level=debug

# 检查 Agent 版本
/tmp/lpmos-agent --version
```

## Regional Client 配置

为了支持 Agent 下载，Regional Client 需要提供静态文件服务。

### 在 Regional Client 服务器上准备 Agent 文件

```bash
# 1. 创建 agent 目录
mkdir -p /data/tftpboot/static/agent

# 2. 复制 Agent 二进制文件
cp /path/to/agent-linux-amd64 /data/tftpboot/static/agent/lpmos-agent

# 3. 设置权限
chmod 755 /data/tftpboot/static/agent/lpmos-agent

# 4. 验证文件可访问
curl -I http://192.168.246.140:8081/static/agent/lpmos-agent
```

### 创建自动化脚本

```bash
# 创建脚本目录
mkdir -p /data/tftpboot/static/scripts

# 复制上面的脚本
cat > /data/tftpboot/static/scripts/download-and-run-agent.sh << 'EOF'
#!/bin/bash
# ... (上面的完整脚本内容)
EOF

# 设置权限
chmod 755 /data/tftpboot/static/scripts/download-and-run-agent.sh
```

## 在 PXE 配置中集成

修改 `/data/tftpboot/pxelinux.cfg/default`，添加自动下载和运行 Agent 的选项：

```
LABEL lpmos-auto
    MENU LABEL LPMOS Automated Installation
    KERNEL http://192.168.246.140:8081/static/kernels/vmlinuz
    APPEND initrd=http://192.168.246.140:8081/static/initramfs/initrd.img ip=dhcp auto_script=http://192.168.246.140:8081/static/scripts/download-and-run-agent.sh
```

这样，系统启动后会自动下载并运行 Agent。

## 总结

**推荐流程**：
1. PXE 启动进入 Live 系统
2. 系统自动配置网络（DHCP）
3. 从 Regional Client 下载 Agent：`wget http://192.168.246.140:8081/static/agent/lpmos-agent`
4. 运行 Agent：`./lpmos-agent --regional-url=http://192.168.246.140:8081`
5. Agent 自动完成硬件上报和系统安装

**关键点**：
- ✅ Regional Client 必须提供 Agent 二进制文件的 HTTP 下载
- ✅ Agent 文件路径：`/data/tftpboot/static/agent/lpmos-agent`
- ✅ 下载 URL：`http://192.168.246.140:8081/static/agent/lpmos-agent`
