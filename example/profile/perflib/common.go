package perflib

import (
<<<<<<< HEAD
	"fmt"
	"math/rand"
	"net"
=======
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math/rand"
	"net"
	"os"
>>>>>>> 6613f0351ad580eb6dda4edd3f91c53cbf4b91a9
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

<<<<<<< HEAD
=======
	"github.com/dep2p/go-dep2p"
	"github.com/dep2p/go-dep2p/core/host"
	"github.com/dep2p/go-dep2p/core/peer"
	"github.com/dep2p/go-dep2p/core/peerstore"
	"github.com/dep2p/go-dep2p/core/protocol"
	"github.com/dep2p/go-dep2p/multiformats/multiaddr"
>>>>>>> 6613f0351ad580eb6dda4edd3f91c53cbf4b91a9
	"github.com/dep2p/pointsub"
)

// 全局测试配置
type TestConfig struct {
	// 是否输出详细日志
	Verbose bool

	// 结果输出目录
	OutputDir string

	// 测试持续时间上限
	MaxTestDuration time.Duration
}

// 测试结果
type TestResult struct {
	// 测试名称
	Name string

	// 测试开始时间
	StartTime time.Time

	// 测试持续时间
	Duration time.Duration

	// 发送的消息总数
	TotalMessages int64

	// 发送的字节总数
	TotalBytes int64

	// 吞吐量指标
	MessagesPerSecond float64
	MBPerSecond       float64

	// 延迟指标 (毫秒)
	MinLatency float64
	MaxLatency float64
	AvgLatency float64
	P50Latency float64 // 中位数延迟
	P90Latency float64 // 90%分位数延迟
	P99Latency float64 // 99%分位数延迟

	// 资源使用
	MaxMemoryUsage uint64
	AvgMemoryUsage uint64
	MaxCPUUsage    float64
	AvgCPUUsage    float64

	// GC统计
	GCPauseTotal time.Duration
	GCCount      uint32

	// 错误统计
	ErrorCount   int64
	TimeoutCount int64
	RetryCount   int64
}

// 测试指标收集器
type MetricsCollector struct {
	startTime    time.Time
	endTime      time.Time
	messageCount int64
	byteCount    int64
	latencies    []float64
	errors       int64
	timeouts     int64
	retries      int64
	memSamples   []uint64
	cpuSamples   []float64
	gcPauseTotal int64
	gcCount      uint32
	mu           sync.Mutex
}

// 创建新的指标收集器
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		startTime:  time.Now(),
		latencies:  make([]float64, 0, 10000),
		memSamples: make([]uint64, 0, 100),
		cpuSamples: make([]float64, 0, 100),
	}
}

// 记录消息发送/接收
func (m *MetricsCollector) RecordMessage(size int, latency time.Duration) {
	atomic.AddInt64(&m.messageCount, 1)
	atomic.AddInt64(&m.byteCount, int64(size))

	m.mu.Lock()
	m.latencies = append(m.latencies, float64(latency.Milliseconds()))
	m.mu.Unlock()
}

// 记录错误
func (m *MetricsCollector) RecordError() {
	atomic.AddInt64(&m.errors, 1)
}

// 记录超时
func (m *MetricsCollector) RecordTimeout() {
	atomic.AddInt64(&m.timeouts, 1)
}

// 记录重试
func (m *MetricsCollector) RecordRetry() {
	atomic.AddInt64(&m.retries, 1)
}

// 记录发送消息
func (m *MetricsCollector) RecordSend(size int64) {
	atomic.AddInt64(&m.messageCount, 1)
	atomic.AddInt64(&m.byteCount, size)
}

// 采样资源使用情况
func (m *MetricsCollector) SampleResourceUsage() {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	m.mu.Lock()
	m.memSamples = append(m.memSamples, memStats.Alloc)
	// 简化的CPU采样，实际实现可能需要使用cgo或其他方法获取实际CPU使用率
	m.cpuSamples = append(m.cpuSamples, 0)

	// 更新GC统计
	if len(m.memSamples) > 1 {
		m.gcPauseTotal = int64(memStats.PauseTotalNs)
		m.gcCount = memStats.NumGC
	}
	m.mu.Unlock()
}

// 完成测试
func (m *MetricsCollector) Complete() {
	m.endTime = time.Now()
}

// 计算百分位数
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}

	idx := int(float64(len(sorted)-1) * p)
	return sorted[idx]
}

// 生成测试结果
func (m *MetricsCollector) GenerateResult(testName string) TestResult {
	m.mu.Lock()
	defer m.mu.Unlock()

	duration := m.endTime.Sub(m.startTime)

	// 计算延迟统计
	var minLatency, maxLatency, totalLatency float64
	p50, p90, p99 := 0.0, 0.0, 0.0

	if len(m.latencies) > 0 {
		// 排序用于计算百分位数
		sorted := make([]float64, len(m.latencies))
		copy(sorted, m.latencies)
		// 排序延迟数据用于计算百分位数
		sort.Float64s(sorted)

		minLatency = sorted[0]
		maxLatency = sorted[len(sorted)-1]

		for _, lat := range m.latencies {
			totalLatency += lat
		}

		// 计算百分位数
		p50 = percentile(sorted, 0.5)  // 中位数
		p90 = percentile(sorted, 0.9)  // 90%分位数
		p99 = percentile(sorted, 0.99) // 99%分位数
	}

	// 计算内存使用
	var maxMem, totalMem uint64
	for _, mem := range m.memSamples {
		totalMem += mem
		if mem > maxMem {
			maxMem = mem
		}
	}

	var avgMem uint64
	if len(m.memSamples) > 0 {
		avgMem = totalMem / uint64(len(m.memSamples))
	}

	// 计算吞吐量
	msgPerSec := float64(m.messageCount) / duration.Seconds()
	mbPerSec := float64(m.byteCount) / duration.Seconds() / (1024 * 1024)

	avgLatency := 0.0
	if len(m.latencies) > 0 {
		avgLatency = totalLatency / float64(len(m.latencies))
	}

	return TestResult{
		Name:              testName,
		StartTime:         m.startTime,
		Duration:          duration,
		TotalMessages:     m.messageCount,
		TotalBytes:        m.byteCount,
		MessagesPerSecond: msgPerSec,
		MBPerSecond:       mbPerSec,
		MinLatency:        minLatency,
		MaxLatency:        maxLatency,
		AvgLatency:        avgLatency,
		P50Latency:        p50,
		P90Latency:        p90,
		P99Latency:        p99,
		MaxMemoryUsage:    maxMem,
		AvgMemoryUsage:    avgMem,
		GCPauseTotal:      time.Duration(m.gcPauseTotal),
		GCCount:           m.gcCount,
		ErrorCount:        m.errors,
		TimeoutCount:      m.timeouts,
		RetryCount:        m.retries,
	}
}

// 生成固定大小的随机消息
func GenFixedSizeMsg(size int) []byte {
	data := make([]byte, size)

	// 对于测试目的，使用简单的重复模式而不是随机数据
	// 这样有助于调试并减少因数据复杂性带来的问题
	for i := 0; i < size; i++ {
		// 使用简单的递增模式 (0-255循环)
		data[i] = byte(i % 256)
	}

	return data
}

// 创建指定大小范围内的随机消息
func GenRandomSizedMsg(minSize, maxSize int) []byte {
	size := minSize
	if maxSize > minSize {
		size = minSize + rand.Intn(maxSize-minSize)
	}
	return GenFixedSizeMsg(size)
}

// GenRandomSizeMsg 生成指定大小的随机消息
func GenRandomSizeMsg(size int) []byte {
	msg := make([]byte, size)
	rand.Read(msg)
	return msg
}

// 启动资源监控
func StartResourceMonitor(collector *MetricsCollector, interval time.Duration) chan struct{} {
	done := make(chan struct{})

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				collector.SampleResourceUsage()
			case <-done:
				return
			}
		}
	}()

	return done
}

// 打印测试结果
func PrintTestResult(result TestResult) {
	fmt.Printf("\n---------- 测试结果: %s ----------\n", result.Name)
	fmt.Printf("开始时间: %s\n", result.StartTime.Format(time.RFC3339))
	fmt.Printf("测试时长: %.2f 秒\n", result.Duration.Seconds())
	fmt.Printf("消息数量: %d\n", result.TotalMessages)

	// 根据数据大小选择合适的单位
	dataSize := float64(result.TotalBytes)
	var sizeStr string
	switch {
	case dataSize >= 1024*1024*1024:
		sizeStr = fmt.Sprintf("%.2f GB", dataSize/(1024*1024*1024))
	case dataSize >= 1024*1024:
		sizeStr = fmt.Sprintf("%.2f MB", dataSize/(1024*1024))
	case dataSize >= 1024:
		sizeStr = fmt.Sprintf("%.2f KB", dataSize/1024)
	default:
		sizeStr = fmt.Sprintf("%.0f 字节", dataSize)
	}
	fmt.Printf("传输数据: %s\n", sizeStr)

	fmt.Printf("吞吐量: %.2f 消息/秒\n", result.MessagesPerSecond)
	fmt.Printf("数据率: %.2f MB/秒\n", result.MBPerSecond)
	fmt.Printf("延迟 (毫秒) - 最小: %.2f, 平均: %.2f, 最大: %.2f\n",
		result.MinLatency, result.AvgLatency, result.MaxLatency)
	fmt.Printf("延迟百分位 (毫秒) - P50: %.2f, P90: %.2f, P99: %.2f\n",
		result.P50Latency, result.P90Latency, result.P99Latency)
	fmt.Printf("内存使用 - 峰值: %.2f MB, 平均: %.2f MB\n",
		float64(result.MaxMemoryUsage)/(1024*1024), float64(result.AvgMemoryUsage)/(1024*1024))
	fmt.Printf("GC - 次数: %d, 总暂停时间: %.2f ms\n",
		result.GCCount, float64(result.GCPauseTotal.Microseconds())/1000)
	fmt.Printf("错误: %d, 超时: %d, 重试: %d\n",
		result.ErrorCount, result.TimeoutCount, result.RetryCount)
	fmt.Println("------------------------------------")
}

<<<<<<< HEAD
=======
// 将测试结果保存到CSV文件
func SaveResultToCSV(result TestResult, filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// 写入CSV头
	fmt.Fprintf(file, "测试名称,开始时间,持续时间(秒),消息数量,总字节数,消息/秒,MB/秒,"+
		"最小延迟(ms),平均延迟(ms),最大延迟(ms),P50延迟(ms),P90延迟(ms),P99延迟(ms),"+
		"内存峰值(MB),平均内存(MB),GC次数,GC暂停(ms),错误数,超时数,重试数\n")

	// 写入数据
	fmt.Fprintf(file, "%s,%s,%.2f,%d,%d,%.2f,%.2f,%.2f,%.2f,%.2f,%.2f,%.2f,%.2f,%d,%.2f,%d,%d,%d\n",
		result.Name,
		result.StartTime.Format(time.RFC3339),
		result.Duration.Seconds(),
		result.TotalMessages,
		result.TotalBytes,
		result.MessagesPerSecond,
		result.MBPerSecond,
		result.MinLatency,
		result.AvgLatency,
		result.MaxLatency,
		result.P50Latency,
		result.P90Latency,
		result.P99Latency,
		float64(result.MaxMemoryUsage)/(1024*1024),
		float64(result.AvgMemoryUsage)/(1024*1024),
		result.GCCount,
		float64(result.GCPauseTotal.Microseconds())/1000,
		result.ErrorCount,
		result.TimeoutCount,
		result.RetryCount)

	return nil
}

>>>>>>> 6613f0351ad580eb6dda4edd3f91c53cbf4b91a9
// Logger 接口定义
type Logger interface {
	Logf(format string, args ...interface{})
	Debug(format string, args ...interface{})
	Info(format string, args ...interface{})
	Warn(format string, args ...interface{})
	Error(format string, args ...interface{})
}

// 控制台日志记录器实现
type consoleLogger struct{}

func (l *consoleLogger) Logf(format string, args ...interface{}) {
	fmt.Printf(format+"\n", args...)
}

func (l *consoleLogger) Debug(format string, args ...interface{}) {
	fmt.Printf("[DEBUG] "+format+"\n", args...)
}

func (l *consoleLogger) Info(format string, args ...interface{}) {
	fmt.Printf("[INFO] "+format+"\n", args...)
}

func (l *consoleLogger) Warn(format string, args ...interface{}) {
	fmt.Printf("[WARN] "+format+"\n", args...)
}

func (l *consoleLogger) Error(format string, args ...interface{}) {
	fmt.Printf("[ERROR] "+format+"\n", args...)
}

// 全局默认日志对象
var defaultLogger = &consoleLogger{}

// ConnectionPair 表示一对连接
type ConnectionPair struct {
<<<<<<< HEAD
	SenderSideConn   pointsub.MessageTransporter
	ReceiverSideConn pointsub.MessageTransporter
=======
	SenderSideConn   *PerfTestMessageTransporter
	ReceiverSideConn *PerfTestMessageTransporter
>>>>>>> 6613f0351ad580eb6dda4edd3f91c53cbf4b91a9
}

// 使用Dep2P host创建一对连接
func setupDep2pConnection(logger Logger) (ConnectionPair, func(), error) {
	logger.Debug("正在创建测试连接对")

<<<<<<< HEAD
	// 创建临时测试对象用于函数调用
	t := &testWrapper{logger: logger}

	// 使用pointsub包中的标准化函数创建连接对
	senderTransporter, receiverTransporter, cleanupFunc := pointsub.CreateDep2pTransporterPair(t)

	// 检查连接是否有效
	if senderTransporter == nil || receiverTransporter == nil {
		cleanupFunc() // 确保资源被释放
		return ConnectionPair{}, func() {}, fmt.Errorf("创建连接失败：发送方或接收方传输器为空")
	}

	logger.Debug("连接对创建成功")

	// 创建连接对
	pair := ConnectionPair{
		SenderSideConn:   senderTransporter,
		ReceiverSideConn: receiverTransporter,
	}

	// 增强的清理函数 - 确保在测试结束时正确关闭连接
	enhancedCleanup := func() {
		logger.Debug("正在关闭连接对")
		cleanupFunc()
	}

	return pair, enhancedCleanup, nil
}

// testWrapper 实现pointsub.TestingT接口，用于包装Logger
=======
	// 随机选择测试协议ID
	protocolID := protocol.ID(fmt.Sprintf("/pointsub/test/%d", rand.Intn(1000)))

	// 创建临时测试对象用于函数调用
	t := &testWrapper{logger: logger}

	// 创建一对连接的主机和连接
	h1, h2, serverConn, clientConn, cleanup := createConnectedPair(t, protocolID)

	// 额外检查连接是否有效
	if clientConn == nil || serverConn == nil {
		cleanup() // 确保资源被释放
		return ConnectionPair{}, func() {}, fmt.Errorf("创建连接失败：客户端或服务端连接为空")
	}

	// 创建一对测试传输器
	sender := NewPerfTestMessageTransporter(clientConn)
	receiver := NewPerfTestMessageTransporter(serverConn)

	logger.Debug("连接对创建成功 (协议: %s)", protocolID)

	// 记录主机信息，用于调试
	logger.Debug("主机1: %s 主机2: %s", h1.ID().String(), h2.ID().String())

	// 创建连接对
	pair := ConnectionPair{
		SenderSideConn:   sender,
		ReceiverSideConn: receiver,
	}

	// 清理函数 - 确保在测试结束时正确关闭连接
	cleanupFunc := func() {
		logger.Debug("正在关闭连接对")

		// 先尝试优雅地关闭传输器
		if sender != nil {
			err1 := sender.Close()
			if err1 != nil {
				logger.Warn("关闭发送方连接时出错: %v", err1)
			}
		}

		if receiver != nil {
			err2 := receiver.Close()
			if err2 != nil {
				logger.Warn("关闭接收方连接时出错: %v", err2)
			}
		}

		// 最后调用host的清理函数
		cleanup()
	}

	return pair, cleanupFunc, nil
}

// 以下是必要的测试辅助函数，内联自test_utils.go

// 创建一个用于测试的dep2p主机
func createTestHost(t TestingT, port int) host.Host {
	// 创建本地主机的多地址
	addr, err := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", port))
	if err != nil {
		t.Fatalf("创建测试多地址失败: %v", err)
	}

	// 创建dep2p主机，使用更简单的配置
	h, err := dep2p.New(
		dep2p.ListenAddrs(addr),
		dep2p.NATPortMap(),
	)
	if err != nil {
		t.Fatalf("创建测试dep2p主机失败: %v", err)
	}

	// 输出主机信息用于调试
	t.Logf("创建测试主机: ID=%s, 地址=%v", h.ID().String(), h.Addrs())

	return h
}

// 创建一对相互连接的dep2p主机
func createHostPair(t TestingT) (host.Host, host.Host, func()) {
	// 随机选择端口，避免端口冲突
	port1 := 10000 + rand.Intn(10000)
	port2 := port1 + 1000 + rand.Intn(1000)

	// 创建主机
	h1 := createTestHost(t, port1)
	h2 := createTestHost(t, port2)

	// 相互添加对方的Peer信息
	h1.Peerstore().AddAddrs(h2.ID(), h2.Addrs(), peerstore.PermanentAddrTTL)
	h2.Peerstore().AddAddrs(h1.ID(), h1.Addrs(), peerstore.PermanentAddrTTL)

	// 尝试建立连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 尝试从h2连接到h1
	if err := h2.Connect(ctx, peer.AddrInfo{
		ID:    h1.ID(),
		Addrs: h1.Addrs(),
	}); err != nil {
		t.Logf("主机连接尝试失败，但这可能不是问题: %v", err)
	}

	// 返回清理函数
	cleanup := func() {
		// 关闭所有连接
		for _, conn := range h1.Network().Conns() {
			conn.Close()
		}
		for _, conn := range h2.Network().Conns() {
			conn.Close()
		}

		// 关闭主机
		h1.Close()
		h2.Close()
	}

	return h1, h2, cleanup
}

// 等待两个主机之间建立连接
func waitForConnection(t TestingT, h1 host.Host, h2 host.Host, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			if len(h1.Network().ConnsToPeer(h2.ID())) > 0 &&
				len(h2.Network().ConnsToPeer(h1.ID())) > 0 {
				return true
			}
		}
	}
}

// 创建一对已连接的主机并返回连接
func createConnectedPair(t TestingT, protocolID protocol.ID) (host.Host, host.Host, net.Conn, net.Conn, func()) {
	// 创建主机对
	h1, h2, hostCleanup := createHostPair(t)

	// 确保主机间可以连接
	if !waitForConnection(t, h1, h2, 5*time.Second) {
		t.Logf("警告: 主机间连接未建立，尝试强制连接")
		// 尝试强制连接
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := h2.Connect(ctx, peer.AddrInfo{
			ID:    h1.ID(),
			Addrs: h1.Addrs(),
		}); err != nil {
			t.Logf("强制连接失败: %v", err)
		}

		// 再次等待连接建立，如果失败就放弃
		if !waitForConnection(t, h1, h2, 5*time.Second) {
			hostCleanup()
			t.Fatalf("主机间连接建立失败")
		}
	}

	// 创建通道用于同步
	ready := make(chan struct{})
	listenerErrCh := make(chan error, 1)

	// 创建监听器
	var listener net.Listener
	var listenerErr error

	// 服务端监听
	listenerDone := make(chan struct{})
	go func() {
		defer close(listenerDone)
		listener, listenerErr = pointsub.Listen(h1, protocolID)
		if listenerErr != nil {
			listenerErrCh <- listenerErr
			return
		}
		close(ready)
	}()

	// 等待服务端就绪或出错，增加了超时处理
	select {
	case err := <-listenerErrCh:
		hostCleanup()
		t.Fatalf("服务端监听失败: %v", err)
	case <-ready:
		// 监听器创建成功
	case <-time.After(5 * time.Second):
		hostCleanup()
		// 检查goroutine是否仍在运行
		select {
		case <-listenerDone:
			// 已经完成但未报告结果
			if listenerErr != nil {
				t.Fatalf("服务端监听失败(延迟报告): %v", listenerErr)
			} else {
				t.Fatalf("服务端监听异常: 监听器创建完成但未通知就绪")
			}
		default:
			t.Fatalf("等待监听器就绪超时")
		}
	}

	// 确保监听器有效
	if listener == nil {
		hostCleanup()
		t.Fatalf("监听器创建成功但为nil")
	}

	// 定义连接变量
	var clientConn, serverConn net.Conn
	var dialErr, acceptErr error

	// 使用WaitGroup确保连接过程完成
	var wg sync.WaitGroup
	wg.Add(2) // 等待两个连接过程

	// 客户端连接
	go func() {
		defer wg.Done()

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		clientConn, dialErr = pointsub.Dial(ctx, h2, h1.ID(), protocolID)
		if dialErr != nil {
			t.Logf("客户端连接失败: %v", dialErr)
		}
	}()

	// 服务端接受连接
	go func() {
		defer wg.Done()

		// 设置accept超时
		if dl, ok := listener.(interface{ SetDeadline(time.Time) error }); ok {
			dl.SetDeadline(time.Now().Add(10 * time.Second))
		}

		serverConn, acceptErr = listener.Accept()
		if acceptErr != nil {
			t.Logf("服务端接受连接失败: %v", acceptErr)
		}
	}()

	// 等待连接过程完成
	connDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(connDone)
	}()

	// 等待连接完成或超时
	select {
	case <-connDone:
		// 连接过程已完成，检查错误
	case <-time.After(15 * time.Second):
		if listener != nil {
			listener.Close()
		}
		hostCleanup()
		t.Fatalf("连接建立超时")
	}

	// 检查连接错误
	if dialErr != nil {
		if listener != nil {
			listener.Close()
		}
		hostCleanup()
		t.Fatalf("客户端连接失败: %v", dialErr)
	}

	if acceptErr != nil {
		if listener != nil {
			listener.Close()
		}
		if clientConn != nil {
			clientConn.Close()
		}
		hostCleanup()
		t.Fatalf("服务端接受连接失败: %v", acceptErr)
	}

	// 验证连接是否有效
	if clientConn == nil || serverConn == nil {
		if listener != nil {
			listener.Close()
		}
		if clientConn != nil {
			clientConn.Close()
		}
		if serverConn != nil {
			serverConn.Close()
		}
		hostCleanup()
		t.Fatalf("连接建立失败: clientConn=%v, serverConn=%v", clientConn != nil, serverConn != nil)
	}

	cleanup := func() {
		if serverConn != nil {
			serverConn.Close()
		}
		if clientConn != nil {
			clientConn.Close()
		}
		if listener != nil {
			listener.Close()
		}
		hostCleanup()
	}

	return h1, h2, serverConn, clientConn, cleanup
}

// TestingT 是testing.TB的子集，用于我们的测试函数
type TestingT interface {
	Logf(format string, args ...interface{})
	Fatalf(format string, args ...interface{})
}

// testWrapper 实现TestingT接口，用于包装Logger
>>>>>>> 6613f0351ad580eb6dda4edd3f91c53cbf4b91a9
type testWrapper struct {
	logger Logger
	failed bool
}

func (t *testWrapper) Fatalf(format string, args ...interface{}) {
	t.logger.Error(format, args...)
	// 对于致命错误，我们记录但不真正中止程序，因为这是性能测试
	t.failed = true
}

func (t *testWrapper) Logf(format string, args ...interface{}) {
	t.logger.Debug(format, args...)
}

<<<<<<< HEAD
// GetNetConn 尝试获取底层的net.Conn，如果可用的话
func GetNetConn(transporter pointsub.MessageTransporter) net.Conn {
	// 尝试类型断言获取底层net.Conn
	if t, ok := transporter.(interface{ UnderlyingConn() net.Conn }); ok {
		return t.UnderlyingConn()
	}
	return nil
}
=======
// PerfTestMessageTransporter 是一个简化版的消息传输器，用于性能测试
// 实现了pointsub.MessageTransporter接口
type PerfTestMessageTransporter struct {
	conn   net.Conn
	reader *bufio.Reader
	writer *bufio.Writer
	mu     sync.Mutex // 保护并发读写
	closed bool       // 标记连接是否已关闭
}

// NewPerfTestMessageTransporter 创建一个用于性能测试的传输器
func NewPerfTestMessageTransporter(conn net.Conn) *PerfTestMessageTransporter {
	if conn == nil {
		return nil
	}

	// 使用大容量缓冲区来提高性能
	reader := bufio.NewReaderSize(conn, 64*1024) // 64KB读缓冲区
	writer := bufio.NewWriterSize(conn, 64*1024) // 64KB写缓冲区

	return &PerfTestMessageTransporter{
		conn:   conn,
		reader: reader,
		writer: writer,
		closed: false,
	}
}

// Send 发送消息
// 使用简单的长度前缀协议：[4字节消息长度][消息内容]
func (t *PerfTestMessageTransporter) Send(data []byte) error {
	// 检查连接和writer是否有效
	if t == nil || t.conn == nil || t.writer == nil {
		return fmt.Errorf("发送失败: 连接或写入器为空")
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return fmt.Errorf("发送失败: 连接已关闭")
	}

	// 写入4字节的长度前缀（大端字节序）
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(data)))

	// 写入长度前缀
	_, err := t.writer.Write(lenBuf)
	if err != nil {
		return fmt.Errorf("发送失败: 写入长度前缀错误: %w", err)
	}

	// 写入实际数据
	_, err = t.writer.Write(data)
	if err != nil {
		return fmt.Errorf("发送失败: 写入数据错误: %w", err)
	}

	// 刷新缓冲区确保数据发送
	if err := t.writer.Flush(); err != nil {
		return fmt.Errorf("发送失败: 刷新缓冲区错误: %w", err)
	}

	return nil
}

// Receive 接收消息
// 读取长度前缀协议：[4字节消息长度][消息内容]
func (t *PerfTestMessageTransporter) Receive() ([]byte, error) {
	// 检查连接和reader是否有效
	if t == nil || t.conn == nil || t.reader == nil {
		return nil, fmt.Errorf("接收失败: 连接或读取器为空")
	}

	// 只锁定读取长度前缀的部分
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil, fmt.Errorf("接收失败: 连接已关闭")
	}

	// 读取4字节的长度前缀
	lenBuf := make([]byte, 4)
	_, err := io.ReadFull(t.reader, lenBuf)
	t.mu.Unlock() // 尽早释放锁，允许其他操作

	if err != nil {
		return nil, fmt.Errorf("接收失败: 读取长度前缀错误: %w", err)
	}

	// 解析消息长度
	msgLen := binary.BigEndian.Uint32(lenBuf)

	// 检查消息长度是否合理，防止分配过大内存
	if msgLen > 100*1024*1024 { // 限制为100MB
		return nil, fmt.Errorf("接收失败: 消息长度 %d 超过限制", msgLen)
	}

	// 分配缓冲区并读取完整消息
	data := make([]byte, msgLen)

	// 不需要锁定整个读取过程，直接从reader读取
	_, err = io.ReadFull(t.reader, data)
	if err != nil {
		return nil, fmt.Errorf("接收失败: 读取消息内容错误: %w", err)
	}

	return data, nil
}

// SendStream 发送流式数据
func (t *PerfTestMessageTransporter) SendStream(reader io.Reader) error {
	// 检查连接和writer是否有效
	if t == nil || t.conn == nil || t.writer == nil {
		return fmt.Errorf("发送流失败: 连接或写入器为空")
	}

	// 检查参数
	if reader == nil {
		return fmt.Errorf("发送流失败: 输入读取器为空")
	}

	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return fmt.Errorf("发送流失败: 连接已关闭")
	}
	t.mu.Unlock()

	// 对于测试目的，我们将整个流读入内存后作为单个消息发送
	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("发送流失败: 读取流错误: %w", err)
	}

	return t.Send(data)
}

// ReceiveStream 接收流式数据
func (t *PerfTestMessageTransporter) ReceiveStream(writer io.Writer) error {
	// 检查连接和reader是否有效
	if t == nil || t.conn == nil || t.reader == nil {
		return fmt.Errorf("接收流失败: 连接或读取器为空")
	}

	// 检查参数
	if writer == nil {
		return fmt.Errorf("接收流失败: 输出写入器为空")
	}

	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return fmt.Errorf("接收流失败: 连接已关闭")
	}
	t.mu.Unlock()

	// 接收单个消息，然后写入输出流
	data, err := t.Receive()
	if err != nil {
		return fmt.Errorf("接收流失败: %w", err)
	}

	_, err = writer.Write(data)
	if err != nil {
		return fmt.Errorf("接收流失败: 写入数据错误: %w", err)
	}

	return nil
}

// Close 关闭连接
func (t *PerfTestMessageTransporter) Close() error {
	if t == nil {
		return nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// 避免重复关闭
	if t.closed {
		return nil
	}

	t.closed = true

	// 先刷新缓冲区确保所有数据已发送
	if t.writer != nil {
		t.writer.Flush()
	}

	// 关闭底层连接
	if t.conn != nil {
		return t.conn.Close()
	}

	return nil
}

// SetReadDeadline 设置读取超时
func (t *PerfTestMessageTransporter) SetReadDeadline(deadline time.Time) error {
	if t == nil || t.conn == nil {
		return fmt.Errorf("设置读取超时失败: 连接为空")
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return fmt.Errorf("设置读取超时失败: 连接已关闭")
	}

	return t.conn.SetReadDeadline(deadline)
}

// SetWriteDeadline 设置写入超时
func (t *PerfTestMessageTransporter) SetWriteDeadline(deadline time.Time) error {
	if t == nil || t.conn == nil {
		return fmt.Errorf("设置写入超时失败: 连接为空")
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return fmt.Errorf("设置写入超时失败: 连接已关闭")
	}

	return t.conn.SetWriteDeadline(deadline)
}

// SetDeadline 设置读写超时
func (t *PerfTestMessageTransporter) SetDeadline(deadline time.Time) error {
	if t == nil || t.conn == nil {
		return fmt.Errorf("设置读写超时失败: 连接为空")
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return fmt.Errorf("设置读写超时失败: 连接已关闭")
	}

	return t.conn.SetDeadline(deadline)
}

// LocalAddr 获取本地地址
func (t *PerfTestMessageTransporter) LocalAddr() net.Addr {
	if t == nil || t.conn == nil {
		return nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}

	return t.conn.LocalAddr()
}

// RemoteAddr 获取远程地址
func (t *PerfTestMessageTransporter) RemoteAddr() net.Addr {
	if t == nil || t.conn == nil {
		return nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}

	return t.conn.RemoteAddr()
}

// Read 实现io.Reader接口
func (t *PerfTestMessageTransporter) Read(b []byte) (n int, err error) {
	if t == nil || t.reader == nil {
		return 0, fmt.Errorf("读取失败: 读取器为空")
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return 0, fmt.Errorf("读取失败: 连接已关闭")
	}

	return t.reader.Read(b)
}

// Write 实现io.Writer接口
func (t *PerfTestMessageTransporter) Write(b []byte) (n int, err error) {
	if t == nil || t.writer == nil {
		return 0, fmt.Errorf("写入失败: 写入器为空")
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return 0, fmt.Errorf("写入失败: 连接已关闭")
	}

	n, err = t.writer.Write(b)
	if err == nil {
		err = t.writer.Flush() // 确保数据被写入
	}
	return
}
>>>>>>> 6613f0351ad580eb6dda4edd3f91c53cbf4b91a9
