package performance

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/core/protocol"

	"github.com/dep2p/pointsub"
)

// PerfConfig 性能测试配置
type PerfConfig struct {
	Name             string           // 测试名称
	Description      string           // 测试描述
	Protocol         protocol.ID      // 测试协议
	MessageSize      int              // 消息大小(字节)
	MessageCount     int              // 消息数量
	BufferSize       int              // 缓冲区大小(字节)
	ConcurrentConns  int              // 并发连接数
	Duration         time.Duration    // 测试持续时间
	NetworkCondition NetworkCondition // 网络条件
	ReportInterval   time.Duration    // 进度报告间隔
}

// PerfResult 性能测试结果
type PerfResult struct {
	Config        PerfConfig    // 测试配置
	TotalBytes    int64         // 传输总字节数
	TotalMessages int64         // 传输总消息数
	Duration      time.Duration // 实际测试时间
	MBPerSecond   float64       // 吞吐量(MB/s)
	MsgsPerSecond float64       // 消息速率(msg/s)
	AvgLatency    time.Duration // 平均延迟
	P50Latency    time.Duration // 50%延迟
	P90Latency    time.Duration // 90%延迟
	P99Latency    time.Duration // 99%延迟
	MaxLatency    time.Duration // 最大延迟
	ErrorCount    int64         // 错误数量
	SuccessRate   float64       // 成功率(%)
	MemoryUsageMB float64       // 内存使用(MB)
}

// 预定义测试配置
var (
	// 吞吐量测试配置 - 不同消息大小
	ThroughputTests = []PerfConfig{
		{
			Name:             "小消息吞吐量(1KB)",
			Description:      "测试小型消息(1KB)的传输性能",
			Protocol:         "/pointsub/perf/throughput/1.0.0",
			MessageSize:      1 * 1024,
			MessageCount:     10000,
			BufferSize:       64 * 1024,
			ConcurrentConns:  1,
			Duration:         10 * time.Second,
			NetworkCondition: LocalNetwork,
			ReportInterval:   2 * time.Second,
		},
		{
			Name:             "中型消息吞吐量(100KB)",
			Description:      "测试中型消息(100KB)的传输性能",
			Protocol:         "/pointsub/perf/throughput/1.0.0",
			MessageSize:      100 * 1024,
			MessageCount:     1000,
			BufferSize:       256 * 1024,
			ConcurrentConns:  1,
			Duration:         10 * time.Second,
			NetworkCondition: LocalNetwork,
			ReportInterval:   2 * time.Second,
		},
		{
			Name:             "大型消息吞吐量(1MB)",
			Description:      "测试大型消息(1MB)的传输性能",
			Protocol:         "/pointsub/perf/throughput/1.0.0",
			MessageSize:      1 * 1024 * 1024,
			MessageCount:     100,
			BufferSize:       4 * 1024 * 1024,
			ConcurrentConns:  1,
			Duration:         10 * time.Second,
			NetworkCondition: LocalNetwork,
			ReportInterval:   2 * time.Second,
		},
		{
			Name:             "超大消息吞吐量(10MB)",
			Description:      "测试超大型消息(10MB)的传输性能",
			Protocol:         "/pointsub/perf/throughput/1.0.0",
			MessageSize:      10 * 1024 * 1024,
			MessageCount:     10,
			BufferSize:       16 * 1024 * 1024,
			ConcurrentConns:  1,
			Duration:         20 * time.Second,
			NetworkCondition: LocalNetwork,
			ReportInterval:   2 * time.Second,
		},
	}

	// 并发测试配置 - 不同连接数
	ConcurrentTests = []PerfConfig{
		{
			Name:             "低并发(5连接)",
			Description:      "测试5个并发连接的性能",
			Protocol:         "/pointsub/perf/concurrent/1.0.0",
			MessageSize:      100 * 1024,
			MessageCount:     1000,
			BufferSize:       256 * 1024,
			ConcurrentConns:  5,
			Duration:         10 * time.Second,
			NetworkCondition: LocalNetwork,
			ReportInterval:   2 * time.Second,
		},
		{
			Name:             "中并发(20连接)",
			Description:      "测试20个并发连接的性能",
			Protocol:         "/pointsub/perf/concurrent/1.0.0",
			MessageSize:      10 * 1024,
			MessageCount:     10000,
			BufferSize:       64 * 1024,
			ConcurrentConns:  20,
			Duration:         10 * time.Second,
			NetworkCondition: LocalNetwork,
			ReportInterval:   2 * time.Second,
		},
		{
			Name:             "高并发(50连接)",
			Description:      "测试50个并发连接的性能",
			Protocol:         "/pointsub/perf/concurrent/1.0.0",
			MessageSize:      1 * 1024,
			MessageCount:     50000,
			BufferSize:       32 * 1024,
			ConcurrentConns:  50,
			Duration:         10 * time.Second,
			NetworkCondition: LocalNetwork,
			ReportInterval:   2 * time.Second,
		},
	}

	// 网络条件测试配置 - 不同网络环境
	NetworkConditionTests = []PerfConfig{
		{
			Name:             "延迟网络(100ms)",
			Description:      "模拟高延迟网络环境(~100ms)下的性能",
			Protocol:         "/pointsub/perf/network/1.0.0",
			MessageSize:      100 * 1024,
			MessageCount:     500,
			BufferSize:       256 * 1024,
			ConcurrentConns:  1,
			Duration:         15 * time.Second,
			NetworkCondition: GetNetworkCondition("simulated-delay"),
			ReportInterval:   3 * time.Second,
		},
		{
			Name:             "不稳定网络(5%丢包)",
			Description:      "模拟不稳定网络环境(5%丢包)下的性能",
			Protocol:         "/pointsub/perf/network/1.0.0",
			MessageSize:      100 * 1024,
			MessageCount:     500,
			BufferSize:       256 * 1024,
			ConcurrentConns:  1,
			Duration:         15 * time.Second,
			NetworkCondition: GetNetworkCondition("simulated-loss"),
			ReportInterval:   3 * time.Second,
		},
		{
			Name:             "极端网络条件",
			Description:      "模拟极端网络条件(高延迟、高丢包、低带宽)下的性能",
			Protocol:         "/pointsub/perf/network/1.0.0",
			MessageSize:      10 * 1024,
			MessageCount:     1000,
			BufferSize:       128 * 1024,
			ConcurrentConns:  1,
			Duration:         20 * time.Second,
			NetworkCondition: ExtremeCondition,
			ReportInterval:   3 * time.Second,
		},
	}

	// 缓冲区大小测试配置
	BufferSizeTests = []PerfConfig{
		{
			Name:             "小缓冲区(4KB)",
			Description:      "测试小缓冲区(4KB)的性能",
			Protocol:         "/pointsub/perf/buffer/1.0.0",
			MessageSize:      1 * 1024 * 1024,
			MessageCount:     10,
			BufferSize:       4 * 1024,
			ConcurrentConns:  1,
			Duration:         10 * time.Second,
			NetworkCondition: LocalNetwork,
			ReportInterval:   2 * time.Second,
		},
		{
			Name:             "中缓冲区(64KB)",
			Description:      "测试中等缓冲区(64KB)的性能",
			Protocol:         "/pointsub/perf/buffer/1.0.0",
			MessageSize:      1 * 1024 * 1024,
			MessageCount:     10,
			BufferSize:       64 * 1024,
			ConcurrentConns:  1,
			Duration:         10 * time.Second,
			NetworkCondition: LocalNetwork,
			ReportInterval:   2 * time.Second,
		},
		{
			Name:             "大缓冲区(1MB)",
			Description:      "测试大缓冲区(1MB)的性能",
			Protocol:         "/pointsub/perf/buffer/1.0.0",
			MessageSize:      1 * 1024 * 1024,
			MessageCount:     10,
			BufferSize:       1 * 1024 * 1024,
			ConcurrentConns:  1,
			Duration:         10 * time.Second,
			NetworkCondition: LocalNetwork,
			ReportInterval:   2 * time.Second,
		},
	}
)

// TestThroughputPerformance 测试吞吐量性能
func TestThroughputPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("在短测试模式下跳过吞吐量性能测试")
	}

	RunPerformanceTests(t, "吞吐量测试", ThroughputTests)
}

// TestConcurrentPerformance 测试并发性能
func TestConcurrentPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("在短测试模式下跳过并发性能测试")
	}

	RunPerformanceTests(t, "并发测试", ConcurrentTests)
}

// TestNetworkConditionsPerformance 测试不同网络条件下的性能
func TestNetworkConditionsPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("在短测试模式下跳过网络条件性能测试")
	}

	RunPerformanceTests(t, "网络条件测试", NetworkConditionTests)
}

// TestBufferSizePerformance 测试不同缓冲区大小的性能
func TestBufferSizePerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("在短测试模式下跳过缓冲区大小性能测试")
	}

	RunPerformanceTests(t, "缓冲区大小测试", BufferSizeTests)
}

// RunPerformanceTests 运行一组性能测试
func RunPerformanceTests(t *testing.T, testGroupName string, tests []PerfConfig) {
	var results []PerfResult

	for _, config := range tests {
		t.Run(config.Name, func(t *testing.T) {
			result := RunPerfTest(t, config)
			results = append(results, result)
		})
	}

	// 输出测试结果汇总
	t.Logf("\n==== %s结果汇总 ====", testGroupName)
	t.Logf("%-25s | %-10s | %-12s | %-15s | %-10s | %-10s",
		"测试名称", "消息大小", "并发连接", "吞吐量(MB/s)", "P99延迟", "成功率")
	t.Logf("%s-|-%s-|-%s-|-%s-|-%s-|-%s",
		"-------------------------", "----------", "------------", "---------------", "----------", "----------")

	for _, result := range results {
		t.Logf("%-25s | %-10s | %-12d | %-15.2f | %-10s | %-10.2f%%",
			result.Config.Name,
			formatBytes(int64(result.Config.MessageSize)),
			result.Config.ConcurrentConns,
			result.MBPerSecond,
			formatDuration(result.P99Latency),
			result.SuccessRate)
	}
}

// RunPerfTest 运行单个性能测试
func RunPerfTest(t *testing.T, config PerfConfig) PerfResult {
	// 创建服务器libp2p主机
	serverHost, err := libp2p.New()
	if err != nil {
		t.Fatalf("[%s] 创建服务器主机失败: %v", config.Name, err)
	}
	defer serverHost.Close()

	// 创建客户端libp2p主机
	clientHost, err := libp2p.New()
	if err != nil {
		t.Fatalf("[%s] 创建客户端主机失败: %v", config.Name, err)
	}
	defer clientHost.Close()

	// 设置对等体信息
	clientHost.Peerstore().AddAddrs(serverHost.ID(), serverHost.Addrs(), peerstore.PermanentAddrTTL)
	serverHost.Peerstore().AddAddrs(clientHost.ID(), clientHost.Addrs(), peerstore.PermanentAddrTTL)

	t.Logf("[%s] 创建了libp2p测试环境", config.Name)
	t.Logf("[%s] 服务器ID: %s", config.Name, serverHost.ID().String())
	t.Logf("[%s] 客户端ID: %s", config.Name, clientHost.ID().String())

	// 在服务器上启动监听器
	serverDone := make(chan struct{})
	serverReceivedBytes := int64(0)
	serverReceivedMsgs := int64(0)
	serverErrors := int64(0)

	listener, err := pointsub.Listen(serverHost, config.Protocol)
	if err != nil {
		t.Fatalf("[%s] 创建监听器失败: %v", config.Name, err)
	}
	defer listener.Close()

	// 服务器接收消息处理
	go func() {
		defer close(serverDone)

		for {
			conn, err := listener.Accept()
			if err != nil {
				atomic.AddInt64(&serverErrors, 1)
				t.Logf("[%s] 接受连接失败: %v", config.Name, err)
				return
			}

			go func() {
				buffer := make([]byte, config.BufferSize)
				for {
					n, err := conn.Read(buffer)
					if err != nil {
						if err != io.EOF {
							atomic.AddInt64(&serverErrors, 1)
							t.Logf("[%s] 读取数据失败: %v", config.Name, err)
						}
						return
					}

					atomic.AddInt64(&serverReceivedBytes, int64(n))
					atomic.AddInt64(&serverReceivedMsgs, 1)
				}
			}()
		}
	}()

	// 准备测试数据
	testData := make([][]byte, config.MessageCount)
	for i := 0; i < config.MessageCount; i++ {
		testData[i] = make([]byte, config.MessageSize)
		_, err := rand.Read(testData[i])
		if err != nil {
			t.Fatalf("[%s] 生成测试数据失败: %v", config.Name, err)
		}
	}

	// 创建结果统计变量
	var (
		totalSentBytes int64
		totalSentMsgs  int64
		clientErrors   int64
		testStartTime  = time.Now()
		testDone       = make(chan struct{})
		latencies      = make([]time.Duration, 0, config.MessageCount*config.ConcurrentConns)
		latenciesLock  sync.Mutex
	)

	// 创建连接并发送消息
	var wg sync.WaitGroup
	for c := 0; c < config.ConcurrentConns; c++ {
		wg.Add(1)
		go func(connID int) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			t.Logf("[%s] 连接 #%d 开始连接到服务器", config.Name, connID)
			conn, err := pointsub.Dial(ctx, clientHost, serverHost.ID(), config.Protocol)
			if err != nil {
				atomic.AddInt64(&clientErrors, 1)
				t.Errorf("[%s] 连接 #%d 创建连接失败: %v", config.Name, connID, err)
				return
			}
			defer conn.Close()
			t.Logf("[%s] 连接 #%d 成功连接到服务器", config.Name, connID)

			ticker := time.NewTicker(time.Duration(float64(config.Duration) / float64(config.MessageCount) * float64(config.ConcurrentConns)))
			defer ticker.Stop()

			for i := 0; i < config.MessageCount/config.ConcurrentConns; i++ {
				select {
				case <-testDone:
					return
				case <-ticker.C:
					// 继续发送
				}

				dataIndex := (connID*config.MessageCount/config.ConcurrentConns + i) % config.MessageCount
				startTime := time.Now()

				n, err := conn.Write(testData[dataIndex])

				latency := time.Since(startTime)

				if err != nil {
					atomic.AddInt64(&clientErrors, 1)
					t.Logf("[%s] 连接 #%d 发送消息 #%d 失败: %v", config.Name, connID, i, err)
					continue
				}

				atomic.AddInt64(&totalSentBytes, int64(n))
				atomic.AddInt64(&totalSentMsgs, 1)

				latenciesLock.Lock()
				latencies = append(latencies, latency)
				latenciesLock.Unlock()
			}
		}(c)
	}

	// 设置测试超时
	timer := time.NewTimer(config.Duration)

	// 定期报告进度
	if config.ReportInterval > 0 {
		ticker := time.NewTicker(config.ReportInterval)
		defer ticker.Stop()

		go func() {
			prevSent := int64(0)
			for {
				select {
				case <-testDone:
					return
				case <-ticker.C:
					current := atomic.LoadInt64(&totalSentBytes)
					elapsed := time.Since(testStartTime)
					rate := float64(current-prevSent) / config.ReportInterval.Seconds() / 1024 / 1024
					t.Logf("[%s] 进度报告: %.2f MB/s, 已发送: %s, 已接收: %s, 耗时: %v",
						config.Name,
						rate,
						formatBytes(current),
						formatBytes(atomic.LoadInt64(&serverReceivedBytes)),
						elapsed.Round(time.Millisecond))
					prevSent = current
				}
			}
		}()
	}

	// 等待测试完成
	select {
	case <-timer.C:
		// 测试时间到
		close(testDone)
	}

	// 等待所有goroutine完成
	wg.Wait()

	// 计算测试结果
	testDuration := time.Since(testStartTime)
	sentBytes := atomic.LoadInt64(&totalSentBytes)
	sentMsgs := atomic.LoadInt64(&totalSentMsgs)
	errors := atomic.LoadInt64(&clientErrors) + atomic.LoadInt64(&serverErrors)

	// 计算延迟统计
	var avgLatency, p50Latency, p90Latency, p99Latency, maxLatency time.Duration

	latenciesLock.Lock()
	if len(latencies) > 0 {
		// 排序延迟以计算百分位
		sort.Slice(latencies, func(i, j int) bool {
			return latencies[i] < latencies[j]
		})

		// 计算各种延迟统计
		var sum time.Duration
		for _, lat := range latencies {
			sum += lat
		}
		avgLatency = sum / time.Duration(len(latencies))

		p50Idx := len(latencies) / 2
		p90Idx := int(float64(len(latencies)) * 0.9)
		p99Idx := int(float64(len(latencies)) * 0.99)

		if p50Idx < len(latencies) {
			p50Latency = latencies[p50Idx]
		}
		if p90Idx < len(latencies) {
			p90Latency = latencies[p90Idx]
		}
		if p99Idx < len(latencies) {
			p99Latency = latencies[p99Idx]
		}

		maxLatency = latencies[len(latencies)-1]
	}
	latenciesLock.Unlock()

	// 计算内存使用情况
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	memoryUsage := float64(memStats.Alloc) / 1024 / 1024

	// 计算成功率
	var successRate float64
	if sentMsgs > 0 {
		successRate = 100 * float64(sentMsgs-errors) / float64(sentMsgs)
	}

	// 生成测试结果
	result := PerfResult{
		Config:        config,
		TotalBytes:    sentBytes,
		TotalMessages: sentMsgs,
		Duration:      testDuration,
		MBPerSecond:   float64(sentBytes) / testDuration.Seconds() / 1024 / 1024,
		MsgsPerSecond: float64(sentMsgs) / testDuration.Seconds(),
		AvgLatency:    avgLatency,
		P50Latency:    p50Latency,
		P90Latency:    p90Latency,
		P99Latency:    p99Latency,
		MaxLatency:    maxLatency,
		ErrorCount:    errors,
		SuccessRate:   successRate,
		MemoryUsageMB: memoryUsage,
	}

	// 打印测试结果
	t.Logf("\n--- %s 测试结果 ---", config.Name)
	t.Logf("消息大小: %s", formatBytes(int64(config.MessageSize)))
	t.Logf("并发连接: %d", config.ConcurrentConns)
	t.Logf("测试时长: %v", testDuration.Round(time.Millisecond))
	t.Logf("发送消息数: %d", sentMsgs)
	t.Logf("发送数据量: %s", formatBytes(sentBytes))
	t.Logf("吞吐量: %.2f MB/s", result.MBPerSecond)
	t.Logf("消息速率: %.2f msgs/s", result.MsgsPerSecond)
	t.Logf("平均延迟: %v", avgLatency.Round(time.Microsecond))
	t.Logf("P50延迟: %v", p50Latency.Round(time.Microsecond))
	t.Logf("P90延迟: %v", p90Latency.Round(time.Microsecond))
	t.Logf("P99延迟: %v", p99Latency.Round(time.Microsecond))
	t.Logf("最大延迟: %v", maxLatency.Round(time.Microsecond))
	t.Logf("错误数: %d", errors)
	t.Logf("成功率: %.2f%%", result.SuccessRate)
	t.Logf("内存使用: %.2f MB", memoryUsage)

	return result
}

// formatBytes 将字节大小格式化为人类可读的形式
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// formatDuration 将时间格式化为人类可读的形式
func formatDuration(d time.Duration) string {
	if d < time.Microsecond {
		return fmt.Sprintf("%d ns", d.Nanoseconds())
	} else if d < time.Millisecond {
		return fmt.Sprintf("%.2f µs", float64(d.Nanoseconds())/1000)
	} else if d < time.Second {
		return fmt.Sprintf("%.2f ms", float64(d.Nanoseconds())/1000000)
	}
	return fmt.Sprintf("%.2f s", d.Seconds())
}
