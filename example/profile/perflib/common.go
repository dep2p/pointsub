package perflib

import (
	"fmt"
	"math/rand"
	"net"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

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
	SenderSideConn   pointsub.MessageTransporter
	ReceiverSideConn pointsub.MessageTransporter
}

// 使用Dep2P host创建一对连接
func setupDep2pConnection(logger Logger) (ConnectionPair, func(), error) {
	logger.Debug("正在创建测试连接对")

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

// GetNetConn 尝试获取底层的net.Conn，如果可用的话
func GetNetConn(transporter pointsub.MessageTransporter) net.Conn {
	// 尝试类型断言获取底层net.Conn
	if t, ok := transporter.(interface{ UnderlyingConn() net.Conn }); ok {
		return t.UnderlyingConn()
	}
	return nil
}
