# PointSub

PointSub 是一个基于 dep2p 的网络通信库，提供了使用 dep2p streams 替换 Go 标准网络栈的功能。

## 主要特性

- 基于 dep2p 的流式通信
- 提供标准的 net.Conn 和 net.Listener 接口实现 
- 支持多路由、NAT 穿透和流复用
- 使用 Peer ID 进行寻址，无需传统的 host:port 方式
- 可配置的连接超时、并发控制、资源限制等
- 支持同一个 Host 同时作为服务端和客户端使用
- 支持多节点之间的全双工通信
- 内置连接池管理和自动重试机制
- 支持消息大小限制和压缩

## 使用限制

- dep2p hosts 不能自己连接自己
- 客户端不能向自己发送请求
- 每个 Host 需要唯一的 Peer ID
- 同一个 Host 上的不同协议需要使用不同的 Protocol ID
- 消息大小受 MaxBlockSize 限制（默认32MB）

## 配置选项

### 服务端配置 (ServerConfig)
- ReadTimeout：读取超时时间（默认30秒）
- WriteTimeout：写入超时时间（默认30秒）
- MaxConcurrentConns：最大并发连接数（默认1000）
- MaxBlockSize：最大消息块大小（默认32MB）
- BufferPoolSize：缓冲池大小（默认4KB）
- CleanupInterval：空闲连接清理间隔（默认5分钟）
- EnableCompression：是否启用压缩（默认true）

### 客户端配置 (ClientConfig)
- ReadTimeout：读取超时时间（默认30秒）
- WriteTimeout：写入超时时间（默认30秒）
- ConnectTimeout：连接超时时间（默认5秒）
- MaxRetries：最大重试次数（默认3次）
- RetryInterval：重试间隔时间（默认1秒）
- MaxBlockSize：最大消息块大小（默认32MB）
- MaxIdleConns：最大空闲连接数（默认100）
- IdleConnTimeout：空闲连接超时时间（默认5分钟）
- EnableCompression：是否启用压缩（默认true）

## 使用方式

### 服务端使用

1. 创建 dep2p Host:
```go
    serverHost, err := dep2p.New()
    if err != nil {
    // 处理错误
    }
    defer serverHost.Close()
```

2. 创建服务端实例:

```go
    server, err := pointsub.NewServer(serverHost, pointsub.DefaultServerConfig())
    if err != nil {
    // 处理错误
    }
    defer server.Stop()
```

3. 定义消息处理函数:

```go
    handler := func(request []byte) ([]byte, error) {
    // 处理请求并返回响应
    return response, nil
    }
```

4. 启动服务:

```go
    protocolID := protocol.ID("/test/1.0.0")
    err = server.Start(protocolID, handler)
    if err != nil {
    // 处理错误
    }
```

### 客户端使用

1. 创建 dep2p Host:

```go
    clientHost, err := dep2p.New()
    if err != nil {
    // 处理错误
    }
    defer clientHost.Close()
```

2. 创建客户端实例:

```go
    client, err := pointsub.NewClient(clientHost, pointsub.DefaultClientConfig())
    if err != nil {
    // 处理错误
    }
    defer client.Close()
```

3. 连接到服务端:

```go
    err = clientHost.Connect(context.Background(), serverHost.Peerstore().PeerInfo(serverHost.ID()))
    if err != nil {
    // 处理错误
    }
```

4. 发送请求:

```go
    response, err := client.Send(
    context.Background(),
    serverHost.ID(),
    protocolID,
    []byte("请求数据")
    )
    if err != nil {
    // 处理错误
    }
```

## 高级特性

### 多节点通信

PointSub 支持多个节点之间的全双工通信：

- 每个节点可以同时作为服务端和客户端
- 支持多对多的通信模式
- 自动处理连接管理和资源清理

### 连接管理

- 自动清理空闲连接
- 连接池管理
- 并发连接数限制
- 自动重试机制

### 性能优化

- 内置缓冲池管理
- 消息大小限制
- 可配置的压缩选项
- 高效的内存使用

## 错误处理

PointSub 提供了完善的错误处理机制：

- 网络错误自动重试
- 超时控制
- 资源限制保护
- 优雅的错误恢复

## 监控和调试

### 连接信息获取

可以通过 Server 的 GetConnectionsInfo 方法获取当前活跃连接的详细信息：
```go
    connInfo := server.GetConnectionsInfo()
    for , info := range connInfo {
    fmt.Printf("远程地址: %s, 最后活跃: %v, 空闲时间: %v\n",
    info.RemoteAddr, info.LastActive, info.IdleTime)
    }
```

## 最佳实践

1. 合理配置超时时间
2. 根据实际需求调整并发连接数
3. 适当设置消息大小限制
4. 在高并发场景下使用连接池
5. 启用压缩以节省带宽
6. 定期监控连接状态
7. 实现适当的错误重试策略