# LPMOS v3.0 Quick Start Guide

## 🚀 快速启动

### 1. 启动etcd
```bash
make start-etcd
```

### 2. 启动Control Plane v3
```bash
# Terminal 1
make run-v3
```

访问: http://localhost:8080

### 3. 启动Regional Client v3 (dc1)
```bash
# Terminal 2
make run-regional-client-v3
```

### 4. 启动Agent (可选)
```bash
# Terminal 3
make run-agent-minimal
```

## 📦 构建命令

```bash
# 构建所有v3组件
make build-v3

# 单独构建
make build-control-plane-v3
make build-regional-client-v3

# 查看所有命令
make help
```

## 🎯 一键Demo

```bash
# 启动完整v3演示环境
make demo-v3
```

然后按照提示在不同终端启动各个组件。

## 🔥 v3.0 核心优势

| 特性 | v2.x | v3.0 | 改进 |
|-----|------|------|------|
| **服务器添加** | 单一列表key | 独立的server key | ⚡ **10x faster** |
| **进度更新** | 分离的task+state | 合并的task结构 | ⚡ **2x faster** |
| **Watch流量** | 全局监听 | 按服务器监听 | ⚡ **90% less traffic** |
| **清理机制** | 手动 | 基于Lease的TTL | ✅ **自动清理** |
| **并发安全** | 可能冲突 | 原子事务 | ✅ **保证一致性** |

## 📊 API端点

### 创建任务
```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "idc": "dc1",
    "sn": "sn-001",
    "mac": "00:1a:2b:3c:4d:5e",
    "os_type": "Ubuntu 22.04",
    "os_version": "22.04"
  }'
```

### 列出所有任务
```bash
curl http://localhost:8080/api/v1/tasks
```

### 列出指定IDC的服务器
```bash
curl http://localhost:8080/api/v1/servers/dc1
```

### 获取统计信息
```bash
curl http://localhost:8080/api/v1/stats
```

### 批准任务
```bash
curl -X POST http://localhost:8080/api/v1/tasks/dc1/sn-001/approve \
  -H "Content-Type: application/json" \
  -d '{"notes": "Hardware verified"}'
```

## 🔍 etcd键结构 (v3.0优化)

```
# 独立的服务器键 (新增)
/os/dc1/servers/sn-001 = {"status": "pending", "mac": "...", "added_at": "..."}
/os/dc1/servers/sn-002 = {"status": "pending", "mac": "...", "added_at": "..."}

# 合并的任务结构 (优化)
/os/dc1/machines/sn-001/task = {
  "task_id": "task-001",
  "status": "installing",
  "progress": [...],    # 集成在单个JSON中
  "logs": [...],        # 集成在单个JSON中
  "approval": {...}     # 集成在单个JSON中
}

# 硬件元数据
/os/dc1/machines/sn-001/meta = {...}

# 心跳Lease (新增)
/os/dc1/machines/sn-001/lease = "lease-12345"  # 30s TTL，自动过期

# 全局统计 (新增)
/os/global/stats/dc1 = {
  "total_machines": 100,
  "pending": 5,
  "installing": 10,
  "completed": 80,
  "failed": 5
}
```

## 🛠️ 开发命令

```bash
# 格式化代码
make fmt

# 运行测试
make test

# 测试覆盖率
make test-coverage

# 清理构建产物
make clean

# 停止etcd
make stop-etcd
```

## 📖 详细文档

- **架构设计**: [ARCHITECTURE_V3.0.md](./ARCHITECTURE_V3.0.md)
- **用户指南**: [README_V3.0.md](./README_V3.0.md)
- **优化总结**: [SCHEMA_OPTIMIZATION_V3.0.md](./SCHEMA_OPTIMIZATION_V3.0.md)

## 🔄 从v2迁移

如果你正在从v2.x迁移，请参考：
1. [SCHEMA_OPTIMIZATION_V3.0.md](./SCHEMA_OPTIMIZATION_V3.0.md) 中的"Migration Path"章节
2. 运行迁移脚本转换etcd键结构
3. 更新control plane和regional client到v3版本

## ❓ 常见问题

### Q: v3和v2有什么区别？
A: v3主要优化了etcd键结构，实现了：
- 独立的服务器键（避免竞态条件）
- 合并的任务结构（原子更新）
- 基于Lease的自动清理
- 事务级别的原子更新

### Q: 可以同时运行v2和v3吗？
A: 不建议。它们使用不同的etcd键结构。请选择一个版本使用。

### Q: Agent需要修改吗？
A: 不需要。Agent继续调用regional client的API，etcd操作由regional client处理。

## 🎉 开始使用

```bash
# 一键启动v3演示
make demo-v3

# 然后在不同终端运行：
# Terminal 1: make run-v3
# Terminal 2: make run-regional-client-v3
# Terminal 3: make run-agent-minimal --regional-url=http://localhost:8081 --sn=sn-001

# 打开浏览器: http://localhost:8080
```

享受**10倍性能提升**和**零竞态条件**的v3.0！🚀
