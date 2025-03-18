package perflib

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// 延迟测试配置
type LatencyTestConfig struct {
	// 消息大小
	MessageSize int

	// 发送间隔 (控制负载)
	SendInterval time.Duration

	// 测试持续时间
	Duration time.Duration

	// 是否详细输出
	Verbose bool
}

// 低负载延迟测试 (测试基础延迟)
func RunLowLoadLatencyTest(config LatencyTestConfig) TestResult {
	// 设置默认值
	if config.MessageSize == 0 {
		config.MessageSize = 1024 // 1KB
	}
	if config.SendInterval == 0 {
		config.SendInterval = 100 * time.Millisecond
	}
	if config.Duration == 0 {
		config.Duration = 30 * time.Second
	}

	testName := fmt.Sprintf("低负载延迟测试 (%dKB, %dms间隔)", config.MessageSize/1024, config.SendInterval.Milliseconds())
	fmt.Printf("\n开始 %s...\n", testName)

	return runLatencyTest(testName, config)
}

// 中等负载延迟测试
func RunMediumLoadLatencyTest(config LatencyTestConfig) TestResult {
	// 设置默认值
	if config.MessageSize == 0 {
		config.MessageSize = 10 * 1024 // 10KB
	}
	if config.SendInterval == 0 {
		config.SendInterval = 50 * time.Millisecond
	}
	if config.Duration == 0 {
		config.Duration = 30 * time.Second
	}

	testName := fmt.Sprintf("中等负载延迟测试 (%dKB, %dms间隔)", config.MessageSize/1024, config.SendInterval.Milliseconds())
	fmt.Printf("\n开始 %s...\n", testName)

	return runLatencyTest(testName, config)
}

// 高负载延迟测试
func RunHighLoadLatencyTest(config LatencyTestConfig) TestResult {
	// 设置默认值
	if config.MessageSize == 0 {
		config.MessageSize = 100 * 1024 // 100KB
	}
	if config.SendInterval == 0 {
		config.SendInterval = 10 * time.Millisecond
	}
	if config.Duration == 0 {
		config.Duration = 30 * time.Second
	}

	testName := fmt.Sprintf("高负载延迟测试 (%dKB, %dms间隔)", config.MessageSize/1024, config.SendInterval.Milliseconds())
	fmt.Printf("\n开始 %s...\n", testName)

	return runLatencyTest(testName, config)
}

// 大消息延迟测试 (1MB-2MB)
func RunLargeMessageLatencyTest(config LatencyTestConfig) TestResult {
	// 设置默认值
	if config.MessageSize == 0 {
		config.MessageSize = 1 * 1024 * 1024 // 1MB
	}
	if config.SendInterval == 0 {
		config.SendInterval = 500 * time.Millisecond
	}
	if config.Duration == 0 {
		config.Duration = 1 * time.Minute
	}

	testName := fmt.Sprintf("大消息延迟测试 (%dMB, %dms间隔)", config.MessageSize/(1024*1024), config.SendInterval.Milliseconds())
	fmt.Printf("\n开始 %s...\n", testName)

	return runLatencyTest(testName, config)
}

// 内部通用延迟测试实现
func runLatencyTest(testName string, config LatencyTestConfig) TestResult {
	// 创建收集器
	collector := NewMetricsCollector()

	// 创建测试对象 (用于dep2p网络)
	t := defaultLogger

	// 启动资源监控
	monitorDone := StartResourceMonitor(collector, 500*time.Millisecond)
	defer close(monitorDone)

	// 创建连接对
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

	// 使用创建的transporter
	sender := connPair.SenderSideConn
	receiver := connPair.ReceiverSideConn

	// 准备测试数据
	testData := GenFixedSizeMsg(config.MessageSize)

	// 启动接收协程
	var wg sync.WaitGroup
	wg.Add(1)

	// 测试是否应该结束的信号
	testDone := make(chan struct{})

	go func() {
		defer wg.Done()

		for {
			select {
			case <-testDone:
				return
			default:
				// 继续接收
			}

			// 设置接收超时
			deadline := time.Now().Add(2 * time.Second)
			_ = receiver.SetReadDeadline(deadline)

			_, err := receiver.Receive()
			if err != nil {
				// 如果是因为测试结束导致的错误，则不记录
				select {
				case <-testDone:
					return
				default:
					// 其他错误
					if config.Verbose {
						log.Printf("接收错误: %v", err)
					}
					collector.RecordError()
				}
			}
		}
	}()

	// 发送循环
	testStart := time.Now()
	messageCount := 0

	for time.Since(testStart) < config.Duration {
		// 发送消息并测量延迟
		start := time.Now()
		err := sender.Send(testData)
		latency := time.Since(start)

		if err != nil {
			if config.Verbose {
				log.Printf("发送错误: %v", err)
			}
			collector.RecordError()
		} else {
			collector.RecordMessage(len(testData), latency)
			messageCount++

			if config.Verbose && messageCount%100 == 0 {
				log.Printf("已发送: %d 条消息", messageCount)
			}
		}

		// 等待发送间隔
		time.Sleep(config.SendInterval)
	}

	// 测试完成
	close(testDone)

	// 等待接收协程结束
	wg.Wait()

	// 生成结果
	collector.Complete()
	result := collector.GenerateResult(testName)

	// 打印结果
	PrintTestResult(result)

	return result
}

// 运行所有延迟测试
func RunAllLatencyTests(verbose bool) []TestResult {
	var results []TestResult

	// 1. 低负载延迟测试
	lowConfig := LatencyTestConfig{
		MessageSize:  1024, // 1KB
		SendInterval: 100 * time.Millisecond,
		Duration:     30 * time.Second,
		Verbose:      verbose,
	}
	results = append(results, RunLowLoadLatencyTest(lowConfig))

	// 2. 中等负载延迟测试
	mediumConfig := LatencyTestConfig{
		MessageSize:  10 * 1024, // 10KB
		SendInterval: 50 * time.Millisecond,
		Duration:     30 * time.Second,
		Verbose:      verbose,
	}
	results = append(results, RunMediumLoadLatencyTest(mediumConfig))

	// 3. 高负载延迟测试
	highConfig := LatencyTestConfig{
		MessageSize:  100 * 1024, // 100KB
		SendInterval: 10 * time.Millisecond,
		Duration:     30 * time.Second,
		Verbose:      verbose,
	}
	results = append(results, RunHighLoadLatencyTest(highConfig))

	// 4. 大消息延迟测试 (1MB)
	largeConfig := LatencyTestConfig{
		MessageSize:  1 * 1024 * 1024, // 1MB
		SendInterval: 500 * time.Millisecond,
		Duration:     1 * time.Minute,
		Verbose:      verbose,
	}
	results = append(results, RunLargeMessageLatencyTest(largeConfig))

	// 5. 大消息延迟测试 (2MB)
	largeConfig2 := LatencyTestConfig{
		MessageSize:  2 * 1024 * 1024, // 2MB
		SendInterval: 500 * time.Millisecond,
		Duration:     1 * time.Minute,
		Verbose:      verbose,
	}
	results = append(results, RunLargeMessageLatencyTest(largeConfig2))

	return results
}
