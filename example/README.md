# PointSub 智能消息传输系统使用指南

本文档提供 PointSub 智能消息传输系统的使用方法和示例说明。PointSub 是一个专为处理大小差异极大的消息传输而设计的框架，提供简洁一致的接口和智能内部处理。

> **性能测试结果**: 查看 [测试结果文档](./TEST_RESULTS.md) 了解系统在不同场景下的性能表现。

## 1. 基本概念

PointSub 智能消息传输系统的核心设计理念是：**简单的 API，智能的内部处理**。无论是几字节的控制消息，还是数GB的大文件传输，系统都能自动选择最佳处理方式，无需开发者干预。

### 核心组件

- **MessageTransporter**: 提供统一的消息传输接口
- **MessageSizer**: 负责消息大小检测和策略选择
- **FrameProcessor**: 处理消息帧的编码和解码
- **AdaptiveBuffer**: 提供智能缓冲管理
- **ProgressTracker**: 提供传输进度跟踪和状态通知
- **ErrorHandler**: 提供错误处理机制

## 2. 安装和导入

首先安装 PointSub 包：

```bash
go get github.com/dep2p/pointsub
```

然后在代码中导入：

```go
import "github.com/dep2p/pointsub"
```

## 3. 基本使用

### 3.1 创建消息传输器

```go
// 使用网络连接创建传输器
conn, err := net.Dial("tcp", "example.com:12345")
if err != nil {
    log.Fatalf("连接失败: %v", err)
}

// 创建基本消息传输器
transporter := pointsub.NewMessageTransporter(conn)
```

### 3.2 发送和接收消息

```go
// 发送消息
message := []byte("Hello, PointSub!")
err := transporter.Send(message)
if err != nil {
    log.Fatalf("发送失败: %v", err)
}

// 接收消息
received, err := transporter.Receive()
if err != nil {
    log.Fatalf("接收失败: %v", err)
}
fmt.Printf("接收到消息: %s\n", string(received))
```

### 3.3 流式传输

```go
// 发送流式数据
file, err := os.Open("large_file.dat")
if err != nil {
    log.Fatalf("打开文件失败: %v", err)
}
defer file.Close()

err = transporter.SendStream(file)
if err != nil {
    log.Fatalf("发送流失败: %v", err)
}

// 接收流式数据
outFile, err := os.Create("received_file.dat")
if err != nil {
    log.Fatalf("创建文件失败: %v", err)
}
defer outFile.Close()

err = transporter.ReceiveStream(outFile)
if err != nil {
    log.Fatalf("接收流失败: %v", err)
}
```

## 4. 高级功能

### 4.1 进度跟踪

PointSub 支持实时跟踪消息传输进度：

```go
// 创建进度跟踪器
progressTracker := pointsub.NewProgressTracker(
    pointsub.WithSpeedSampling(true, 5),
    pointsub.WithProgressThreshold(1.0), // 每增加1%更新一次进度
)

// 实现进度回调
type myProgressCallback struct{}

func (pc *myProgressCallback) OnProgress(transferID string, total int64, transferred int64, percentage float64) {
    fmt.Printf("\r传输进度: %.1f%% (%d/%d 字节)", percentage, transferred, total)
}

// 其他必须实现的回调方法
func (pc *myProgressCallback) OnStatusChange(transferID string, oldStatus, newStatus pointsub.TransferStatus) {
    // 状态变更处理
}

func (pc *myProgressCallback) OnSpeedUpdate(transferID string, bytesPerSecond float64, estimatedTimeLeft time.Duration) {
    // 速度更新处理
}

func (pc *myProgressCallback) OnError(transferID string, err error, isFatal bool) {
    // 错误处理
}

func (pc *myProgressCallback) OnComplete(transferID string, totalBytes int64, totalTime time.Duration) {
    // 完成处理
}

// 注册回调
progressTracker.AddCallback(&myProgressCallback{})

// 创建带进度跟踪的传输器
transporter := pointsub.NewMessageTransporter(
    conn,
    pointsub.WithProgressTracker(progressTracker),
)

// 开始跟踪
transferID := "my-transfer-1"
progressTracker.StartTracking(transferID, fileSize)

// 发送大文件...

// 完成后停止跟踪
progressTracker.StopTracking(transferID)
```

### 4.2 自定义错误处理

PointSub 允许自定义错误处理策略：

```go
// 创建错误处理器
errorHandler := pointsub.NewDefaultErrorHandler()

// 注册错误回调
errorHandler.RegisterCallback(pointsub.SeverityError, func(err *pointsub.MessageError) {
    fmt.Printf("发生错误: %s\n", err.Error())
})

// 设置错误处理策略
errorHandler.SetDefaultStrategy(pointsub.SeverityWarning, pointsub.StrategyRetry)

// 创建带自定义错误处理的传输器
transporter := pointsub.NewMessageTransporter(
    conn,
    pointsub.WithErrorHandler(errorHandler),
)
```

### 4.3 自定义缓冲区管理

PointSub 提供灵活的缓冲区管理：

```go
// 创建自适应缓冲区
buffer := pointsub.NewDefaultAdaptiveBuffer(
    pointsub.WithAdaptiveMaxMemory(100 * 1024 * 1024), // 100MB内存限制
    pointsub.WithPoolSizes(8*1024, 64*1024, 512*1024), // 自定义缓冲区大小
)

// 创建带自定义缓冲区的传输器
transporter := pointsub.NewMessageTransporter(
    conn,
    pointsub.WithAdaptiveBuffer(buffer),
)
```

## 5. 示例代码说明

本目录下的 `main.go` 文件提供了完整的示例，展示 PointSub 系统的主要功能：

### 5.1 基础消息发送与接收

演示了如何使用 PointSub 发送和接收简单消息，展示了基本 API 的使用方法。

### 5.2 流式数据传输

展示了如何使用 PointSub 进行流式数据传输，适用于大小未知的数据流。

### 5.3 大文件传输与进度跟踪

展示了如何使用 PointSub 传输大文件，并利用进度跟踪功能实时显示传输状态。

### 5.4 错误处理

展示了 PointSub 的错误处理机制，包括错误回调、错误分类和可恢复性判断。

## 6. 运行示例

确保已经安装了 PointSub 包，然后运行示例：

```bash
cd pointsub/example
go run main.go
```

## 7. 传输状态说明

PointSub 定义了以下传输状态，用于跟踪传输进度：

```go
// 传输状态
const (
    // 初始化中
    StatusInitializing TransferStatus = iota
    
    // 正在传输
    StatusTransferring
    
    // 暂停
    StatusPaused
    
    // 恢复中
    StatusResuming
    
    // 完成
    StatusCompleted
    
    // 失败
    StatusFailed
    
    // 取消
    StatusCancelled
)
```

## 8. 最佳实践

1. **根据消息大小选择合适的方法**
   - 小消息（<1MB）：使用 `Send`/`Receive` 方法
   - 大消息（>1MB）：使用 `SendStream`/`ReceiveStream` 方法

2. **错误处理**
   - 始终检查返回的错误
   - 对于关键操作，使用自定义错误处理策略
   - 利用错误的可恢复性判断，实现自动重试机制

3. **进度跟踪**
   - 对于大文件传输，使用进度跟踪功能提供用户反馈
   - 实现合适的回调函数，显示传输速度和预计剩余时间
   - 监听传输状态变化，及时处理异常情况

4. **资源管理**
   - 传输完成后关闭连接
   - 停止不再需要的进度跟踪
   - 对于大型应用，考虑使用内存管理器控制资源使用

## 9. 故障排除

### 常见问题

1. **发送/接收超时**
   - 检查网络连接是否稳定
   - 考虑增加超时设置：`pointsub.WithTimeout()`

2. **内存使用过高**
   - 限制最大缓冲区大小：`pointsub.WithMaxBufferSize()`
   - 启用内存压力监控：`pointsub.WithMemoryPressureCheckInterval()`

3. **传输速度慢**
   - 调整缓冲区大小：`pointsub.WithCustomBufferSizes()`
   - 优化块大小：`pointsub.WithChunkSize()`
   - 禁用块间延迟：`pointsub.WithNoBlockDelay()`

### 诊断技巧

1. 利用 `ProgressTracker` 监控传输速度和状态
2. 注册错误回调，记录详细的错误信息
3. 使用 `ErrorMonitor` 收集和分析错误趋势

## 10. 深入阅读

有关 PointSub 系统的更多信息，请参阅：

- 设计文档：项目根目录下的`README.md`
- API 文档：[GoDoc - github.com/dep2p/pointsub](https://pkg.go.dev/github.com/dep2p/pointsub)
- 源代码：`github.com/dep2p/pointsub`目录 

## 性能测试结果摘要

我们对PointSub系统进行了全面的性能测试，包括吞吐量测试、延迟测试、并发测试、稳定性测试和大数据流测试。以下是主要测试结果：

### 吞吐量测试

| 消息大小 | 并发连接数 | 消息速率 (消息/秒) | 数据速率 | 
|---------|-----------|------------------|----------|
| 256字节  | 2         | 159.97          | 0.04 MB/s |
| 512字节  | 5         | 1,985.56        | 0.97 MB/s |
| 50KB    | 3         | 504.31          | 24.62 MB/s |
| 1MB     | 2         | 20.19           | 20.19 MB/s |
| 5MB     | 1         | 2.02            | 10.10 MB/s |

### 延迟测试

| 负载条件 | 消息大小 | 发送间隔 | 消息数 | 数据速率 | 平均延迟 | 最大延迟 |
|---------|---------|---------|-------|----------|---------|---------|
| 低负载   | 1KB     | 100ms   | 297   | 0.01 MB/s | 0.00ms  | 0.00ms  |
| 中负载   | 10KB    | 50ms    | 588   | 0.18 MB/s | 0.01ms  | 4.00ms  |
| 高负载   | 100KB   | 10ms    | 2,722 | 8.31 MB/s | 0.00ms  | 2.00ms  |
| 大消息   | 1MB     | 500ms   | 120   | 1.95 MB/s | 0.01ms  | 1.00ms  |
| 大消息   | 2MB     | 500ms   | 120   | 3.89 MB/s | 0.03ms  | 2.00ms  |

### 稳定性测试

| 测试场景 | 消息大小 | 连接数 | 持续时间 | 发送消息数 | 接收消息数 | 错误恢复 |
|---------|---------|--------|---------|-----------|-----------|----------|
| 长时间稳定性 | 10KB | 2 | 10分钟 | 35,842 | 35,842 | 100% |
| 错误恢复能力 | 1KB | 3 | 5分钟 | 15,000 | 14,678 | 97.85% |

### 大数据流测试

| 消息大小 | 总数据量 | 消息数 | 吞吐量 | 平均内存使用 | GC次数 | 完成率 |
|---------|---------|--------|--------|------------|-------|--------|
| 1MB     | 10GB    | 10,240 | 18.53 MB/s | 6.21 MB | 312 | 100% |
| 2MB     | 10GB    | 5,120  | 17.98 MB/s | 9.67 MB | 278 | 100% |

### 性能分析

1. **吞吐量特性**:
   - 小消息(512字节)在高并发(5)下达到约2,000消息/秒
   - 中等消息(50KB)实现了24.62 MB/秒的数据传输率
   - 随着消息大小增加，每秒处理的消息数量减少，但单位时间数据传输量增加

2. **延迟表现**:
   - 系统在各种负载条件下维持极低延迟，大多数情况下平均延迟接近0毫秒
   - 最高延迟出现在中等负载测试中，为4.00毫秒

3. **稳定性和错误恢复**:
   - 长时间稳定性测试中，系统在10分钟内保持100%稳定，无消息丢失
   - 在错误恢复测试中，注入随机错误后系统能恢复97.85%的消息传输

4. **大数据流性能**:
   - 系统能够稳定传输10GB数据，保持约18MB/秒的吞吐量
   - 内存使用保持在合理范围，没有出现内存泄漏或过度GC问题

**注意：** 以上测试在本地网络环境下进行，实际分布式环境中的性能可能会有所不同。

完整的测试结果和详细分析，请查看[测试结果文档](./TEST_RESULTS.md)。

性能测试工具和方法详情，请查看[性能测试框架文档](./profile/README.md)。 