# Regional Client 自注册功能说明

## ✅ 已添加功能

Regional Client 现在会在启动时自动注册到 etcd，并保持心跳。

## 🔧 新增功能

### 1. 启动时自动注册

Regional Client 启动时会在 etcd 中创建以下键：

```
/os/region/{idc}/info          # Regional Client 信息
/os/region/{idc}/heartbeat     # 心跳（带 30s lease）
```

### 2. 持续心跳

- 每 30 秒自动续约 lease
- 如果 Regional Client 崩溃，心跳 30 秒后自动消失
- 自动重连和重建 lease

### 3. 优雅关闭

当 Regional Client 收到 `SIGINT` 或 `SIGTERM` 信号时：
1. 更新状态为 `offline`
2. 撤销心跳 lease
3. 记录 `stopped_at` 时间

## 📊 etcd 数据结构

### /os/region/{idc}/info

```json
{
  "idc": "mailong-test",
  "server_ip": "192.168.246.140",
  "api_port": "8081",
  "dhcp_enabled": false,
  "tftp_enabled": true,
  "started_at": "2026-02-04T12:00:00Z",
  "status": "online"
}
```

**离线状态**（优雅关闭后）：
```json
{
  "idc": "mailong-test",
  "server_ip": "192.168.246.140",
  "api_port": "8081",
  "dhcp_enabled": false,
  "tftp_enabled": true,
  "started_at": "2026-02-04T12:00:00Z",
  "stopped_at": "2026-02-04T13:00:00Z",
  "status": "offline"
}
```

### /os/region/{idc}/heartbeat

```json
{
  "status": "online",
  "last_updated": "2026-02-04T12:00:30Z",
  "lease_id": 12345678
}
```

**特点**：
- 带 30 秒 TTL 的 lease
- Regional Client 崩溃后 30 秒自动消失
- Control Plane 可以通过监听此键检测 Regional Client 在线状态

## 🧪 测试步骤

### 1. 部署新版本

```bash
# 将编译好的二进制上传到服务器
scp bin/regional-client-linux-amd64 user@server:/path/to/

# 或者在服务器上编译
make linux-regional-client
```

### 2. 启动 Regional Client

```bash
./regional-client-linux-amd64 --idc=mailong-test --server-ip=192.168.246.140
```

**预期日志输出**：
```
Starting LPMOS Regional Client v3.0 for IDC: mailong-test
Configuration: API Port=8081, Server IP=192.168.246.140, Interface=eth1
✓ Kickstart/Preseed generator initialized
✓ Regional Client registered to etcd: /os/region/mailong-test    # 新增
[mailong-test] Heartbeat started (lease: 7587869725825474147)   # 新增
[mailong-test] Watching for new servers at: /os/mailong-test/servers/
[mailong-test] Watching for task updates at: /os/mailong-test/machines/
Regional client API listening on :8081
```

### 3. 验证注册信息

```bash
# 查看 Regional Client 信息
etcdctl get /os/region/mailong-test/info

# 预期输出
/os/region/mailong-test/info
{"idc":"mailong-test","server_ip":"192.168.246.140","api_port":"8081",...}
```

### 4. 验证心跳

```bash
# 查看心跳
etcdctl get /os/region/mailong-test/heartbeat

# 预期输出
/os/region/mailong-test/heartbeat
{"status":"online","last_updated":"2026-02-04T12:00:30Z",...}
```

### 5. 测试心跳自动续约

```bash
# 持续监听心跳键（会看到它一直存在）
watch -n 5 'etcdctl get /os/region/mailong-test/heartbeat'

# 应该每次都能看到数据，说明 lease 在自动续约
```

### 6. 测试崩溃场景

```bash
# 强制杀死进程（模拟崩溃）
kill -9 $(pgrep regional-client)

# 等待 30-35 秒后查看心跳
sleep 35
etcdctl get /os/region/mailong-test/heartbeat

# 预期：没有输出（心跳已消失）

# 但 info 仍然存在，状态还是 online（因为没有优雅关闭）
etcdctl get /os/region/mailong-test/info
```

### 7. 测试优雅关闭

```bash
# 启动 Regional Client
./regional-client-linux-amd64 --idc=mailong-test --server-ip=192.168.246.140

# 按 Ctrl+C 或发送 SIGTERM
kill $(pgrep regional-client)

# 查看日志输出
Shutting down regional client...
[mailong-test] Unregistering from etcd...
[mailong-test] Unregistered from etcd

# 验证状态
etcdctl get /os/region/mailong-test/info
# 应该看到 status: "offline" 和 stopped_at 字段

etcdctl get /os/region/mailong-test/heartbeat
# 应该没有输出（lease 已撤销）
```

## 🔍 监控和调试

### 查看所有 Regional Client

```bash
# 查看所有已注册的 Regional Client
etcdctl get /os/region --prefix --keys-only

# 示例输出
/os/region/dc1/info
/os/region/dc1/heartbeat
/os/region/dc2/info
/os/region/dc2/heartbeat
/os/region/mailong-test/info
/os/region/mailong-test/heartbeat
```

### 查看特定机房的详细信息

```bash
# 查看 mailong-test 机房的所有信息
etcdctl get /os/region/mailong-test --prefix

# 更友好的输出
etcdctl get /os/region/mailong-test/info | tail -n 1 | jq .
etcdctl get /os/region/mailong-test/heartbeat | tail -n 1 | jq .
```

### 监听 Regional Client 状态变化

```bash
# 实时监听注册信息变化
etcdctl watch /os/region/mailong-test --prefix

# 然后启动/停止 Regional Client，可以看到实时变化
```

### 检查在线状态

```bash
# 简单脚本检查所有 Regional Client 状态
for idc in $(etcdctl get /os/region --prefix --keys-only | grep info | cut -d/ -f4); do
  echo "=== $idc ==="

  # 检查 info
  has_info=$(etcdctl get /os/region/$idc/info | wc -l)

  # 检查 heartbeat
  has_heartbeat=$(etcdctl get /os/region/$idc/heartbeat | wc -l)

  if [ $has_heartbeat -gt 0 ]; then
    echo "✅ ONLINE"
  elif [ $has_info -gt 0 ]; then
    echo "⚠️  OFFLINE (info存在但无心跳)"
  else
    echo "❌ NOT REGISTERED"
  fi
  echo
done
```

## 🎯 与其他组件集成

### Control Plane 可以使用此功能

Control Plane 可以：

1. **监控 Regional Client 在线状态**：
   ```go
   // 监听心跳键的变化
   watchChan := etcdClient.Watch(ctx, "/os/region/", true)

   for resp := range watchChan {
       for _, ev := range resp.Events {
           if strings.HasSuffix(string(ev.Kv.Key), "/heartbeat") {
               if ev.Type == clientv3.EventTypeDelete {
                   // Regional Client 离线
                   idc := extractIDC(ev.Kv.Key)
                   log.Printf("Regional Client %s went offline", idc)
               }
           }
       }
   }
   ```

2. **获取可用的 Regional Client 列表**：
   ```go
   // 查询所有有心跳的 Regional Client
   resp, _ := etcdClient.Get(ctx, "/os/region/", clientv3.WithPrefix())

   var onlineRegionals []string
   for _, kv := range resp.Kvs {
       if strings.HasSuffix(string(kv.Key), "/heartbeat") {
           idc := extractIDC(kv.Key)
           onlineRegionals = append(onlineRegionals, idc)
       }
   }
   ```

3. **任务分发时检查 Regional Client 是否在线**：
   ```go
   func assignTask(idc, sn string) error {
       // 检查 Regional Client 是否在线
       heartbeatKey := fmt.Sprintf("/os/region/%s/heartbeat", idc)
       resp, err := etcdClient.Get(ctx, heartbeatKey)

       if err != nil || len(resp.Kvs) == 0 {
           return fmt.Errorf("Regional Client %s is offline", idc)
       }

       // 创建任务...
   }
   ```

## 📝 变更总结

### 新增字段

`RegionalClient` 结构体：
- `apiPort string`
- `enableDHCP bool`
- `enableTFTP bool`
- `startedAt time.Time`
- `selfLeaseID clientv3.LeaseID`

### 新增方法

- `registerToEtcd() error` - 注册到 etcd
- `maintainHeartbeat()` - 维护心跳（goroutine）
- `unregisterFromEtcd()` - 注销（优雅关闭时调用）

### 新增 etcd 包方法

- `GetClient() *clientv3.Client` - 获取原始 etcd 客户端

### 修改的逻辑

- `main()` 函数：在初始化后调用 `registerToEtcd()`
- `main()` 函数：关闭前调用 `unregisterFromEtcd()`

## ⚠️ 注意事项

1. **Lease TTL 是 30 秒**：如果 Regional Client 挂掉，最多 30 秒后心跳才会消失
2. **自动重连**：如果 etcd 连接断开，会自动重连并重建 lease
3. **多次启动**：同一个 `idc` 可以启动多个 Regional Client，但建议只启动一个
4. **清理旧数据**：如果需要清理旧的离线 Regional Client 数据：
   ```bash
   etcdctl del /os/region/old-idc --prefix
   ```

## 🎉 完成

现在 Regional Client 会自动注册到 etcd 并保持心跳！

Control Plane 和其他组件可以通过监听 `/os/region/{idc}/heartbeat` 来实时了解 Regional Client 的在线状态。
