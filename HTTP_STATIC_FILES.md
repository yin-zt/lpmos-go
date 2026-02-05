# Regional Client HTTP 静态文件服务

## 📦 功能说明

Regional Client 提供 HTTP 静态文件服务，用于：
1. **PXE 启动文件**: kernel, initramfs
2. **OS 安装镜像**: Ubuntu, CentOS, Rocky Linux, Debian 仓库镜像
3. **自定义文件**: 任何需要通过 HTTP 分发的文件

## 🚀 快速开始

### 1. 启动 Regional Client

```bash
# 使用默认路径 /tftpboot
./regional-client-linux-amd64 --idc=mailong-test --server-ip=192.168.246.140

# 使用自定义路径
./regional-client-linux-amd64 \
  --idc=mailong-test \
  --server-ip=192.168.246.140 \
  --static-root=/data/lpmos
```

**启动日志**：
```
Starting LPMOS Regional Client v3.0 for IDC: mailong-test
Configuration: API Port=8081, Server IP=192.168.246.140, Interface=eth1, Static Root=/tftpboot
✓ Kickstart/Preseed generator initialized
✓ Static file directories ready: /tftpboot
✓ Regional Client registered to etcd: /os/region/mailong-test
[mailong-test] Heartbeat started (lease: xxx)
Regional client API listening on :8081
```

### 2. 目录结构

Regional Client 会自动创建以下目录结构：

```
/tftpboot/                          # 静态文件根目录
├── README.md                       # 使用说明
├── static/                         # 静态文件
│   ├── kernels/                    # Linux 内核
│   │   ├── vmlinuz-ubuntu-22.04
│   │   ├── vmlinuz-centos-8
│   │   └── vmlinuz-rocky-9
│   └── initramfs/                  # Initramfs 镜像
│       ├── lpmos-agent-initramfs.gz
│       └── lpmos-agent-initramfs-debug.gz
└── repos/                          # 软件包仓库镜像
    ├── ubuntu/
    │   ├── 20.04/
    │   │   ├── dists/
    │   │   └── pool/
    │   └── 22.04/
    │       ├── dists/
    │       └── pool/
    ├── centos/
    │   ├── 7/
    │   │   ├── BaseOS/
    │   │   └── AppStream/
    │   └── 8/
    │       ├── BaseOS/
    │       └── AppStream/
    ├── rocky/
    │   ├── 8/
    │   └── 9/
    └── debian/
        ├── 11/
        └── 12/
```

## 📥 准备文件

### 方法 1: 手动放置文件

```bash
# 创建目录
mkdir -p /tftpboot/static/kernels
mkdir -p /tftpboot/static/initramfs
mkdir -p /tftpboot/repos/ubuntu/22.04

# 复制 kernel
cp /path/to/vmlinuz /tftpboot/static/kernels/vmlinuz-ubuntu-22.04

# 复制 initramfs
cp /path/to/lpmos-agent-initramfs.gz /tftpboot/static/initramfs/

# 设置权限
chmod -R 755 /tftpboot
```

### 方法 2: 使用脚本同步镜像

**Ubuntu 镜像同步**：
```bash
#!/bin/bash
# sync-ubuntu-mirror.sh

MIRROR_URL="http://archive.ubuntu.com/ubuntu"
LOCAL_PATH="/tftpboot/repos/ubuntu/22.04"

# 使用 rsync 同步（推荐）
rsync -avz --delete \
  rsync://archive.ubuntu.com/ubuntu/dists/jammy/ \
  $LOCAL_PATH/dists/jammy/

rsync -avz --delete \
  rsync://archive.ubuntu.com/ubuntu/pool/ \
  $LOCAL_PATH/pool/

# 或使用 apt-mirror
apt-mirror /etc/apt/mirror.list
```

**CentOS 镜像同步**：
```bash
#!/bin/bash
# sync-centos-mirror.sh

MIRROR_URL="rsync://mirrors.kernel.org/centos/8-stream"
LOCAL_PATH="/tftpboot/repos/centos/8"

rsync -avz --delete \
  $MIRROR_URL/BaseOS/ \
  $LOCAL_PATH/BaseOS/

rsync -avz --delete \
  $MIRROR_URL/AppStream/ \
  $LOCAL_PATH/AppStream/
```

### 方法 3: 使用反向代理（节省空间）

如果不想存储完整镜像，可以配置 nginx 反向代理：

```nginx
# /etc/nginx/conf.d/lpmos-repos.conf

server {
    listen 8081;
    server_name _;

    # 静态文件直接服务
    location /static/ {
        alias /tftpboot/static/;
        autoindex on;
    }

    # Ubuntu 仓库反向代理
    location /repos/ubuntu/ {
        proxy_pass http://archive.ubuntu.com/ubuntu/;
        proxy_set_header Host archive.ubuntu.com;
    }

    # CentOS 仓库反向代理
    location /repos/centos/ {
        proxy_pass http://mirror.centos.org/centos/;
        proxy_set_header Host mirror.centos.org;
    }
}
```

## 🌐 HTTP API

### 静态文件访问

**访问 kernel**：
```bash
curl http://192.168.246.140:8081/static/kernels/vmlinuz-ubuntu-22.04 -O
```

**访问 initramfs**：
```bash
curl http://192.168.246.140:8081/static/initramfs/lpmos-agent-initramfs.gz -O
```

**访问仓库文件**：
```bash
# Ubuntu 包
curl http://192.168.246.140:8081/repos/ubuntu/22.04/pool/main/o/openssh/openssh-server_8.9p1-3ubuntu0.1_amd64.deb -O

# CentOS 包
curl http://192.168.246.140:8081/repos/centos/8/BaseOS/x86_64/os/Packages/kernel-4.18.0-348.el8.x86_64.rpm -O
```

### 文件列表 API

**列出 /static 目录**：
```bash
curl http://192.168.246.140:8081/api/v1/files/static | jq .
```

**响应示例**：
```json
{
  "path": "/static",
  "files": [
    {
      "name": "kernels",
      "path": "/kernels",
      "is_dir": true,
      "size": 4096
    },
    {
      "name": "vmlinuz-ubuntu-22.04",
      "path": "/kernels/vmlinuz-ubuntu-22.04",
      "is_dir": false,
      "size": 8388608,
      "modified": "2026-02-04T14:00:00Z"
    },
    {
      "name": "initramfs",
      "path": "/initramfs",
      "is_dir": true,
      "size": 4096
    },
    {
      "name": "lpmos-agent-initramfs.gz",
      "path": "/initramfs/lpmos-agent-initramfs.gz",
      "is_dir": false,
      "size": 52428800,
      "modified": "2026-02-04T14:00:00Z"
    }
  ]
}
```

**列出 /repos 目录**：
```bash
curl http://192.168.246.140:8081/api/v1/files/repos | jq .
```

## 🔧 Agent 使用示例

### 在 Agent 中下载文件

```go
// 下载 kernel
kernelURL := "http://192.168.246.140:8081/static/kernels/vmlinuz-ubuntu-22.04"
resp, err := http.Get(kernelURL)
if err != nil {
    return err
}
defer resp.Body.Close()

file, err := os.Create("/tmp/vmlinuz")
if err != nil {
    return err
}
defer file.Close()

io.Copy(file, resp.Body)
```

### 在 Kickstart 中使用

```bash
# kickstart 文件中指定仓库
url --url=http://192.168.246.140:8081/repos/centos/8/BaseOS/x86_64/os/

# 或在 kernel 参数中
inst.repo=http://192.168.246.140:8081/repos/centos/8/BaseOS/x86_64/os/
```

### 在 debootstrap 中使用

```bash
# 使用本地镜像
debootstrap jammy /mnt http://192.168.246.140:8081/repos/ubuntu/22.04
```

## 📊 监控和调试

### 检查文件是否可访问

```bash
# 测试 kernel 下载
curl -I http://192.168.246.140:8081/static/kernels/vmlinuz-ubuntu-22.04

# 预期响应
HTTP/1.1 200 OK
Content-Type: application/octet-stream
Content-Length: 8388608
```

### 查看访问日志

Regional Client 使用 Gin 框架，会自动记录 HTTP 访问：

```
[GIN] 2026/02/04 - 14:00:00 | 200 |  1.234567ms |  192.168.246.150 | GET      "/static/kernels/vmlinuz-ubuntu-22.04"
[GIN] 2026/02/04 - 14:00:01 | 200 | 52.345678ms |  192.168.246.150 | GET      "/static/initramfs/lpmos-agent-initramfs.gz"
```

### 测试文件列表 API

```bash
# 查看所有静态文件
curl http://192.168.246.140:8081/api/v1/files/static | jq '.files[] | select(.is_dir == false) | .path'

# 输出
"/kernels/vmlinuz-ubuntu-22.04"
"/initramfs/lpmos-agent-initramfs.gz"
```

## 🔐 安全建议

### 1. 限制访问

使用防火墙限制只有内网可以访问：

```bash
# iptables 规则
iptables -A INPUT -p tcp --dport 8081 -s 192.168.0.0/16 -j ACCEPT
iptables -A INPUT -p tcp --dport 8081 -j DROP
```

### 2. 使用 HTTPS（可选）

如果需要 HTTPS，可以在前面加 nginx 反向代理：

```nginx
server {
    listen 443 ssl;
    server_name lpmos.example.com;

    ssl_certificate /etc/ssl/certs/lpmos.crt;
    ssl_certificate_key /etc/ssl/private/lpmos.key;

    location / {
        proxy_pass http://localhost:8081;
    }
}
```

### 3. 文件权限

```bash
# 确保文件只有 root 可以写入
chown -R root:root /tftpboot
chmod -R 755 /tftpboot
chmod -R 644 /tftpboot/static/*
```

## 📈 性能优化

### 1. 使用 CDN 或缓存

对于大文件（如 ISO 镜像），建议：
- 使用 nginx 缓存
- 使用 CDN 加速
- 使用本地 SSD 存储

### 2. 启用 gzip 压缩

在 nginx 中启用压缩：

```nginx
gzip on;
gzip_types text/plain application/json;
gzip_min_length 1000;
```

### 3. 限速（可选）

防止单个客户端占用所有带宽：

```nginx
location /repos/ {
    limit_rate 10m;  # 限制每个连接 10MB/s
}
```

## 🐛 故障排查

### 问题 1: 404 Not Found

**原因**: 文件不存在或路径错误

**解决**:
```bash
# 检查文件是否存在
ls -la /tftpboot/static/kernels/

# 检查权限
ls -ld /tftpboot/static/

# 查看文件列表 API
curl http://192.168.246.140:8081/api/v1/files/static
```

### 问题 2: 403 Forbidden

**原因**: 权限不足

**解决**:
```bash
# 修复权限
chmod -R 755 /tftpboot
chown -R root:root /tftpboot
```

### 问题 3: 下载速度慢

**原因**: 网络带宽或磁盘 I/O 限制

**解决**:
- 使用 SSD 存储
- 增加网络带宽
- 使用本地镜像而不是反向代理

## 📝 命令行参数

```bash
./regional-client-linux-amd64 \
  --idc=mailong-test \              # 机房 ID（必需）
  --server-ip=192.168.246.140 \     # 服务器 IP
  --api-port=8081 \                 # API 端口（默认 8081）
  --static-root=/tftpboot \         # 静态文件根目录（默认 /tftpboot）
  --enable-dhcp \                   # 启用 DHCP 服务器
  --enable-tftp \                   # 启用 TFTP 服务器
  --interface=eth1                  # 网络接口
```

## ✅ 完整示例

### 部署完整的 PXE + HTTP 环境

```bash
# 1. 创建目录结构
mkdir -p /tftpboot/static/{kernels,initramfs}
mkdir -p /tftpboot/repos/{ubuntu,centos,rocky}

# 2. 复制文件
cp vmlinuz /tftpboot/static/kernels/vmlinuz-ubuntu-22.04
cp initramfs.gz /tftpboot/static/initramfs/lpmos-agent-initramfs.gz

# 3. 同步 Ubuntu 镜像（可选）
rsync -avz rsync://archive.ubuntu.com/ubuntu/dists/jammy/ \
  /tftpboot/repos/ubuntu/22.04/dists/jammy/

# 4. 启动 Regional Client
./regional-client-linux-amd64 \
  --idc=mailong-test \
  --server-ip=192.168.246.140 \
  --enable-tftp \
  --enable-dhcp

# 5. 验证
curl http://192.168.246.140:8081/api/v1/files/static
curl -I http://192.168.246.140:8081/static/kernels/vmlinuz-ubuntu-22.04
```

## 🎉 总结

Regional Client 现在提供完整的 HTTP 静态文件服务：

✅ **自动创建目录结构**
✅ **支持自定义根目录**
✅ **提供文件列表 API**
✅ **支持大文件下载**
✅ **与 PXE/TFTP 集成**

Agent 可以通过 HTTP 下载所需的所有文件！
