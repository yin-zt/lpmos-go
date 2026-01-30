#!/bin/bash

echo "=== LPMOS 系统检查 ==="
echo ""

# 检查二进制文件
echo "1. 检查二进制文件..."
if [ -f "bin/control-plane" ]; then
    echo "   ✅ bin/control-plane 存在 ($(ls -lh bin/control-plane | awk '{print $5}'))"
else
    echo "   ❌ bin/control-plane 不存在"
fi

if [ -f "bin/regional-client" ]; then
    echo "   ✅ bin/regional-client 存在 ($(ls -lh bin/regional-client | awk '{print $5}'))"
else
    echo "   ❌ bin/regional-client 不存在"
fi

if [ -f "bin/agent-minimal" ]; then
    echo "   ✅ bin/agent-minimal 存在 ($(ls -lh bin/agent-minimal | awk '{print $5}'))"
else
    echo "   ❌ bin/agent-minimal 不存在"
fi

echo ""

# 检查前端文件
echo "2. 检查前端文件..."
if [ -f "web/index.html" ]; then
    lines=$(wc -l < web/index.html)
    echo "   ✅ web/index.html 存在 ($lines 行)"
else
    echo "   ❌ web/index.html 不存在"
fi

echo ""

# 检查 etcd
echo "3. 检查 etcd 状态..."
if docker ps | grep -q lpmos-etcd; then
    echo "   ✅ etcd 容器正在运行"
else
    echo "   ⚠️  etcd 容器未运行，需要执行: make start-etcd"
fi

echo ""

# 检查端口占用
echo "4. 检查端口占用..."
if lsof -i :8080 > /dev/null 2>&1; then
    echo "   ⚠️  端口 8080 已被占用"
    lsof -i :8080 | tail -n +2
else
    echo "   ✅ 端口 8080 可用"
fi

if lsof -i :8081 > /dev/null 2>&1; then
    echo "   ⚠️  端口 8081 已被占用"
else
    echo "   ✅ 端口 8081 可用"
fi

echo ""
echo "=== 启动建议 ==="
echo ""
echo "如果 etcd 未运行，先执行:"
echo "  make demo"
echo ""
echo "然后在三个终端分别运行:"
echo "  Terminal 1: make run"
echo "  Terminal 2: make run-regional"
echo "  Terminal 3: make run-agent"
echo ""
echo "最后访问: http://localhost:8080"
echo ""
