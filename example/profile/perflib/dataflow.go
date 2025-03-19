package perflib

import (
	"fmt"
	"log"
	"net"
	"os"
	"runtime"
	"sync/atomic"
	"time"
)

// 数据流测试配置
type DataFlowTestConfig struct {
	// 消息大小 (字节)
	MessageSize int

	// 目标数据总量 (字节)
	TotalDataTarget int64

	// 最大测试时间 (超时后强制结束)
	MaxDuration time.Duration

	// 采样间隔 (多久记录一次性能数据)
	SamplingInterval time.Duration

	// 进度报告间隔
	ProgressInterval time.Duration

	// 是否记录详细性能数据
	VerbosePerformance bool

	// 是否启用详细日志
	Verbose bool
}

// 性能采样点
type PerformanceSample struct {
	// 采样时间
	Timestamp time.Time

	// 已传输数据量 (字节)
	TransferredBytes int64

	// 当前瞬时速率 (MB/s)
	CurrentSpeed float64

	// 内存使用情况 (字节)
	MemoryUsage uint64

	// 垃圾回收次数
	GCCount uint32

	// 垃圾回收总时间 (纳秒)
	GCTotalPause uint64

	// 测试开始以来的平均速率 (MB/s)
	AverageSpeed float64

	// 传输百分比完成
	PercentComplete float64
}

// 大数据流测试 - 1MB消息
func RunDataFlow1MBTest(config DataFlowTestConfig) TestResult {
	// 设置默认值
	if config.MessageSize == 0 {
		config.MessageSize = 1 * 1024 * 1024 // 1MB
	}
	if config.TotalDataTarget == 0 {
		config.TotalDataTarget = 10 * 1024 * 1024 * 1024 // 10GB
	}
	if config.MaxDuration == 0 {
		config.MaxDuration = 1 * time.Hour
	}
	if config.SamplingInterval == 0 {
		config.SamplingInterval = 5 * time.Second
	}
	if config.ProgressInterval == 0 {
		config.ProgressInterval = 30 * time.Second
	}

	testName := fmt.Sprintf("大数据流测试 (1MB消息, %.2fGB总量)",
		float64(config.TotalDataTarget)/(1024*1024*1024))
	fmt.Printf("\n开始 %s...\n", testName)

	return runDataFlowTest(testName, config)
}

// 大数据流测试 - 2MB消息
func RunDataFlow2MBTest(config DataFlowTestConfig) TestResult {
	// 设置默认值
	if config.MessageSize == 0 {
		config.MessageSize = 2 * 1024 * 1024 // 2MB
	}
	if config.TotalDataTarget == 0 {
		config.TotalDataTarget = 10 * 1024 * 1024 * 1024 // 10GB
	}
	if config.MaxDuration == 0 {
		config.MaxDuration = 1 * time.Hour
	}
	if config.SamplingInterval == 0 {
		config.SamplingInterval = 5 * time.Second
	}
	if config.ProgressInterval == 0 {
		config.ProgressInterval = 30 * time.Second
	}

	testName := fmt.Sprintf("大数据流测试 (2MB消息, %.2fGB总量)",
		float64(config.TotalDataTarget)/(1024*1024*1024))
	fmt.Printf("\n开始 %s...\n", testName)

	return runDataFlowTest(testName, config)
}

// 内部实现大数据流测试
func runDataFlowTest(testName string, config DataFlowTestConfig) TestResult {
	// 创建指标收集器
	collector := NewMetricsCollector()

	// 创建测试对象 (用于dep2p网络)
	t := defaultLogger

	// 记录测试开始时间
	testStartTime := time.Now()

	// 启动资源监控
	monitorDone := StartResourceMonitor(collector, 1*time.Second)
	defer close(monitorDone)

	// 准备测试数据
	testData := GenFixedSizeMsg(config.MessageSize)

	// 创建dep2p连接对
	connPair, cleanup, err := setupDep2pConnection(t)
	defer cleanup()

	// 如果连接建立失败，返回空结果
	if err != nil {
		log.Printf("无法建立连接: %v", err)

		// 返回一个表示失败的结果
		collector.Complete()
		result := collector.GenerateResult(testName + " (连接失败)")
		result.ErrorCount = 1
		return result
	}

	// 获取连接
	sender := connPair.SenderSideConn
	receiver := connPair.ReceiverSideConn

	// 性能采样点列表
	var samples []PerformanceSample

	// 共享变量用于跟踪进度
	var transferredBytes int64
	var messageCount int64
	var lastSampleTime = time.Now()
	var lastProgressTime = time.Now()
	var lastSampleBytes int64

	// 结束信号
	done := make(chan struct{})

	// 启动接收协程
	go func() {
		defer close(done)

		// 记录GC统计起始值
		var lastNumGC uint32
		var memStats runtime.MemStats
		runtime.ReadMemStats(&memStats)
		lastNumGC = memStats.NumGC

		// 循环接收消息
		for {
			// 设置超时
			deadline := time.Now().Add(10 * time.Second)
			receiver.SetReadDeadline(deadline)

			// 接收消息
			data, err := receiver.Receive()
			if err != nil {
				if config.Verbose {
					log.Printf("接收错误: %v", err)
				}

				// 判断是否是超时错误
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					// 检查是否已达到目标数据量
					if atomic.LoadInt64(&transferredBytes) >= config.TotalDataTarget {
						fmt.Println("接收超时且已达到目标数据量，结束接收")
						return
					}

					// 检查是否测试已超时
					if time.Since(testStartTime) > config.MaxDuration {
						fmt.Println("接收超时且测试已达到最大时间限制，结束接收")
						return
					}

					// 继续尝试接收
					continue
				}

				collector.RecordError()
				return
			}

			// 更新计数器
			curBytes := int64(len(data))
			atomic.AddInt64(&transferredBytes, curBytes)
			atomic.AddInt64(&messageCount, 1)

			// 采样性能数据
			now := time.Now()

			// 性能采样
			if now.Sub(lastSampleTime) >= config.SamplingInterval {
				bytesInInterval := atomic.LoadInt64(&transferredBytes) - lastSampleBytes
				timeInInterval := now.Sub(lastSampleTime).Seconds()
				speedInInterval := float64(bytesInInterval) / timeInInterval / (1024 * 1024) // MB/s

				// 读取最新的内存统计
				runtime.ReadMemStats(&memStats)

				// 创建采样点
				sample := PerformanceSample{
					Timestamp:        now,
					TransferredBytes: atomic.LoadInt64(&transferredBytes),
					CurrentSpeed:     speedInInterval,
					MemoryUsage:      memStats.Alloc,
					GCCount:          memStats.NumGC - lastNumGC,
					GCTotalPause:     memStats.PauseTotalNs,
					AverageSpeed:     float64(transferredBytes) / now.Sub(testStartTime).Seconds() / (1024 * 1024),
					PercentComplete:  float64(transferredBytes) * 100 / float64(config.TotalDataTarget),
				}

				// 添加到样本列表
				samples = append(samples, sample)

				// 如果启用了详细性能记录，则输出当前性能
				if config.VerbosePerformance {
					fmt.Printf("[%s] 速率: %.2f MB/s, 平均: %.2f MB/s, 内存: %.2f MB, GC: %d次\n",
						now.Format("15:04:05"),
						sample.CurrentSpeed,
						sample.AverageSpeed,
						float64(sample.MemoryUsage)/(1024*1024),
						sample.GCCount)
				}

				// 更新基准值为当前值
				lastSampleTime = now
				lastSampleBytes = atomic.LoadInt64(&transferredBytes)
				lastNumGC = memStats.NumGC
			}

			// 打印进度
			if now.Sub(lastProgressTime) >= config.ProgressInterval {
				total := atomic.LoadInt64(&transferredBytes)
				percent := float64(total) * 100 / float64(config.TotalDataTarget)
				elapsedTime := now.Sub(testStartTime)
				estimatedTotal := elapsedTime.Seconds() * float64(config.TotalDataTarget) / float64(total)
				estimatedRemaining := time.Duration(estimatedTotal)*time.Second - elapsedTime

				fmt.Printf("进度: %.2f%% (%.2f GB / %.2f GB) | 预计剩余时间: %s\n",
					percent,
					float64(total)/(1024*1024*1024),
					float64(config.TotalDataTarget)/(1024*1024*1024),
					estimatedRemaining.Round(time.Second))

				lastProgressTime = now
			}

			// 检查是否达到目标数据量
			if atomic.LoadInt64(&transferredBytes) >= config.TotalDataTarget {
				fmt.Println("达到目标数据量，测试完成")
				return
			}
		}
	}()

	// 发送循环 - 使用超时保护避免无限阻塞
	testStart := time.Now()
	checkTimer := time.NewTicker(5 * time.Second)
	defer checkTimer.Stop()

	for {
		// 检查是否测试结束
		select {
		case <-done:
			// 接收端已结束
			goto TestComplete
		case <-checkTimer.C:
			// 定期检查状态
			// 检查是否超时
			if time.Since(testStart) > config.MaxDuration {
				fmt.Println("测试达到最大时间限制，强制结束")
				goto TestComplete
			}

			// 检查是否已发送足够数据
			if atomic.LoadInt64(&transferredBytes) >= config.TotalDataTarget {
				fmt.Println("已达到目标数据量，结束发送")
				goto TestComplete
			}
			continue
		default:
			// 继续发送
		}

		// 检查是否超时
		if time.Since(testStart) > config.MaxDuration {
			fmt.Println("测试达到最大时间限制，强制结束")
			goto TestComplete
		}

		// 检查是否已发送足够数据
		if atomic.LoadInt64(&transferredBytes) >= config.TotalDataTarget {
			goto TestComplete
		}

		// 发送消息
		start := time.Now()
		// 设置写超时
		sender.SetWriteDeadline(time.Now().Add(5 * time.Second))

		err = sender.Send(testData)
		latency := time.Since(start)

		if err != nil {
			if config.Verbose {
				log.Printf("发送错误: %v", err)
			}
			collector.RecordError()
			// 短暂暂停后重试
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// 记录消息
		collector.RecordMessage(len(testData), latency)
	}

TestComplete:
	// 确保关闭所有连接
	if sender != nil {
		err := sender.Close()
		if err != nil && config.Verbose {
			log.Printf("关闭发送端连接失败: %v", err)
		}
	}

	if receiver != nil {
		err := receiver.Close()
		if err != nil && config.Verbose {
			log.Printf("关闭接收端连接失败: %v", err)
		}
	}

	// 测试完成，等待接收协程结束
	select {
	case <-done:
		fmt.Println("接收协程已结束")
	case <-time.After(5 * time.Second):
		fmt.Println("等待接收协程超时，强制结束测试")
	}

	// 总结测试
	collector.Complete()
	result := collector.GenerateResult(testName)

	// 添加额外信息
	result.TotalMessages = messageCount
	result.TotalBytes = transferredBytes

	// 计算平均速率
	avgSpeed := float64(transferredBytes) / result.Duration.Seconds() / (1024 * 1024) // MB/s
	fmt.Printf("\n测试完成: 总数据量: %.2f GB, 平均速率: %.2f MB/s, 总消息数: %d\n",
		float64(transferredBytes)/(1024*1024*1024),
		avgSpeed,
		messageCount)

	// 打印详细性能统计
	if len(samples) > 0 {
		// 找出最高和最低速率
		var maxSpeed, minSpeed float64
		var maxMem uint64
		maxSpeed = samples[0].CurrentSpeed
		minSpeed = samples[0].CurrentSpeed
		maxMem = samples[0].MemoryUsage

		for _, s := range samples[1:] {
			if s.CurrentSpeed > maxSpeed {
				maxSpeed = s.CurrentSpeed
			}
			if s.CurrentSpeed < minSpeed {
				minSpeed = s.CurrentSpeed
			}
			if s.MemoryUsage > maxMem {
				maxMem = s.MemoryUsage
			}
		}

		fmt.Printf("最高速率: %.2f MB/s, 最低速率: %.2f MB/s\n", maxSpeed, minSpeed)
		fmt.Printf("内存峰值: %.2f MB\n", float64(maxMem)/(1024*1024))
		fmt.Printf("速率波动: %.2f%%\n", 100*(maxSpeed-minSpeed)/avgSpeed)
	}

	// 保存性能数据到CSV (如果有详细性能记录)
	if config.VerbosePerformance && len(samples) > 0 {
		// 创建性能数据CSV
		perfFileName := fmt.Sprintf("dataflow_perf_%s.csv", time.Now().Format("20060102_150405"))
		savePerformanceDataToCSV(samples, perfFileName)
		fmt.Printf("详细性能数据已保存至: %s\n", perfFileName)
	}

	// 打印测试结果
	PrintTestResult(result)

	return result
}

// 保存性能数据到CSV
func savePerformanceDataToCSV(samples []PerformanceSample, fileName string) error {
	// 创建CSV文件内容
	csvContent := "时间戳,已传输数据(字节),当前速率(MB/s),内存使用(字节),GC次数,GC暂停时间(纳秒),平均速率(MB/s),完成百分比(%)\n"

	for _, s := range samples {
		line := fmt.Sprintf("%s,%d,%.2f,%d,%d,%d,%.2f,%.2f\n",
			s.Timestamp.Format("2006-01-02 15:04:05"),
			s.TransferredBytes,
			s.CurrentSpeed,
			s.MemoryUsage,
			s.GCCount,
			s.GCTotalPause,
			s.AverageSpeed,
			s.PercentComplete)
		csvContent += line
	}

	// 写入文件
	return os.WriteFile(fileName, []byte(csvContent), 0644)
}

// 运行所有数据流测试
func RunAllDataFlowTests(verbose bool) []TestResult {
	var results []TestResult

	// 基本配置
	baseConfig := DataFlowTestConfig{
		TotalDataTarget:    10 * 1024 * 1024 * 1024, // 10GB
		MaxDuration:        2 * time.Hour,
		SamplingInterval:   5 * time.Second,
		ProgressInterval:   30 * time.Second,
		VerbosePerformance: true,
		Verbose:            verbose,
	}

	// 1. 运行1MB消息大数据流测试
	config1MB := baseConfig
	config1MB.MessageSize = 1 * 1024 * 1024 // 1MB

	fmt.Printf("\n===== 开始1MB消息大数据流测试 (10GB) =====\n")
	results = append(results, RunDataFlow1MBTest(config1MB))

	// 2. 运行2MB消息大数据流测试
	config2MB := baseConfig
	config2MB.MessageSize = 2 * 1024 * 1024 // 2MB

	fmt.Printf("\n===== 开始2MB消息大数据流测试 (10GB) =====\n")
	results = append(results, RunDataFlow2MBTest(config2MB))

	return results
}
