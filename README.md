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

### 服务端选项
- WithServerReadTimeout(d time.Duration)：设置读取超时
- WithServerWriteTimeout(d time.Duration)：设置写入超时
- WithMaxConcurrentConns(n int)：设置最大并发连接数
- WithMaxBlockSize(n int)：设置最大消息大小
- WithBufferPoolSize(n int)：设置缓冲池大小
- WithCleanupInterval(d time.Duration)：设置清理间隔
- WithServerCompression(enable bool)：设置是否启用压缩

### 客户端选项
- WithReadTimeout(d time.Duration)：设置读取超时
- WithWriteTimeout(d time.Duration)：设置写入超时
- WithConnectTimeout(d time.Duration)：设置连接超时
- WithMaxRetries(n int)：设置最大重试次数
- WithRetryInterval(d time.Duration)：设置重试间隔
- WithCompression(enable bool)：设置是否启用压缩
- WithMaxIdleConns(n int)：设置最大空闲连接数
- WithIdleConnTimeout(d time.Duration)：设置空闲连接超时

### 服务端配置 (ServerConfig)
- ReadTimeout：读取超时时间（默认30秒）
- WriteTimeout：写入超时时间（默认30秒）
- MaxConcurrentConns：最大并发连接数
  - 初始值：1000
  - 最小值：100
  - 最大值：10000
  - 动态调整：根据负载自动增减
    - 负载过高时减少10%
    - 负载较低时增加10%
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

### 连接管理
- 动态连接数调整
  - 自动根据系统负载调整并发连接数
  - 增长率：10%
  - 收缩率：10%
  - 触发条件：
    - 当负载因子 > maxLoadFactor 时减少连接数
    - 当负载因子 < maxLoadFactor*0.5 时增加连接数
- 连接数限制
  - 最小值(MinConcurrentConns)：100，确保基本服务能力
  - 最大值(MaxConcurrentConns)：10000，防止资源耗尽
  - 动态范围：在最小值和最大值之间自动调整
- 负载计算
  - 负载因子 = 当前活跃连接数 / 最大并发连接数
  - 实时监控和调整
  - 防止系统过载

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
    server, err := pointsub.NewServer(serverHost,
        pointsub.WithMaxConcurrentConns(1000),
        pointsub.WithServerReadTimeout(30*time.Second),
        pointsub.WithServerWriteTimeout(30*time.Second),
    )
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
    client, err := pointsub.NewClient(clientHost,
        pointsub.WithReadTimeout(30*time.Second),
        pointsub.WithWriteTimeout(30*time.Second),
        pointsub.WithMaxRetries(3),
        pointsub.WithConnectTimeout(5*time.Second),
    )
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

    // 发送到最近节点
    response, err = client.SendClosest(
    context.Background(),
    protocolID,
    []byte("请求数据")
    )
    if err != nil {
    // 处理错误
    }
```

### 最近节点路由

PointSub 提供了基于节点距离的智能路由功能：

- SendClosest：自动选择最近的可用节点发送请求
  - 基于节点距离计算
  - 自动故障转移
  - 支持重试机制

#### 节点管理
```go
// 添加服务器节点
client.AddServerNode(protocolID, serverID)

// 移除服务器节点
client.RemoveServerNode(protocolID, serverID)

// 获取当前节点列表
nodes := client.GetServerNodes(protocolID)

// 清除所有节点
client.ClearServerNodes(protocolID)
```

#### 使用场景
- 多节点部署时自动选择最优节点
- 故障转移和负载均衡
- 就近接入提升性能

#### 特性
- 自动计算节点距离
- 智能节点选择
- 故障节点自动跳过
- 支持自定义重试策略
- 连接池复用

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

## 协议与服务

### 协议处理机制

#### 服务端协议处理

服务端可以注册并处理多个不同的协议：

```go
// Server结构
type Server struct {
    handlers sync.Map  // key为protocolID, value为StreamHandler
    // ...
}

// 注册多个协议示例
server, _ := NewServer(host, config)

// 协议1: 文件传输
server.Start("/file/1.0.0", fileHandler)

// 协议2: 消息聊天
server.Start("/chat/1.0.0", chatHandler)

// 协议3: 数据同步 
server.Start("/sync/1.0.0", syncHandler)
```

特点：
- 一个Server实例可以处理多个协议
- 每个协议对应一个独立的处理器(handler)
- 使用sync.Map存储协议与处理器的映射

#### 客户端使用

客户端可以访问服务端的多个协议：

```go
// 单个Client实例可访问多个协议
client, _ := NewClient(host, config)

// 发送文件请求
client.Send(ctx, peerID, "/file/1.0.0", fileData)

// 发送聊天消息
client.Send(ctx, peerID, "/chat/1.0.0", chatMsg)

// 发送同步请求
client.Send(ctx, peerID, "/sync/1.0.0", syncData)
```

特点：
- 一个Client实例可以访问所有协议
- 不需要为每个协议创建单独的客户端
- 在Send时指定要使用的协议

### 协议设计总结

- **服务端架构**: 一个Server实例可以注册多个协议处理器
- **客户端架构**: 一个Client实例可以访问多个协议
- **连接复用**: 相同peer之间的连接会被复用，提高性能
- **协议隔离**: 不同协议有独立的处理逻辑，互不影响
- **灵活扩展**: 可以随时添加新的协议而不影响现有功能

### 协议管理功能

客户端支持基于协议的服务器节点管理：

```go
// 添加服务器节点
client.AddServerNode("/chat/1.0.0", serverID)

// 向最近的服务器节点发送请求
response, err := client.SendClosest(ctx, "/chat/1.0.0", message)

// 获取协议的服务器节点
nodes := client.GetServerNodes("/chat/1.0.0")

// 移除服务器节点
client.RemoveServerNode("/chat/1.0.0", serverID)

// 清除协议的所有节点
client.ClearServerNodes("/chat/1.0.0")
```

这个功能允许客户端：
- 管理每个协议的服务器节点列表
- 自动选择合适的节点发送请求
- 支持服务器节点的动态添加和移除

## 版本信息

```json
{
  "version": "v1.0.4"
}
```