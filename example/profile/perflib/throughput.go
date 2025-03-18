package perflib

import (
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// 吞吐量测试配置
type ThroughputTestConfig struct {
	// 消息大小
	MessageSize int

	// 并发连接数
	Concurrency int

	// 每个连接发送的消息数
	MessagesPerConnection int

	// 测试时长 (超过后强制结束)
	Duration time.Duration

	// 是否详细输出
	Verbose bool
}

// 小消息吞吐量测试 (< 1KB)
func RunSmallMsgThroughputTest(config ThroughputTestConfig) TestResult {
	// 设置默认值
	if config.MessageSize == 0 {
		config.MessageSize = 512 // 512 bytes
	}
	if config.Concurrency == 0 {
		config.Concurrency = 10
	}
	if config.MessagesPerConnection == 0 {
		config.MessagesPerConnection = 1000
	}
	if config.Duration == 0 {
		config.Duration = 1 * time.Minute
	}

	testName := fmt.Sprintf("小消息吞吐量测试 (%d字节, %d并发)", config.MessageSize, config.Concurrency)
	fmt.Printf("\n开始 %s...\n", testName)

	return runThroughputTest(testName, config)
}

// 中等消息吞吐量测试 (10KB-100KB)
func RunMediumMsgThroughputTest(config ThroughputTestConfig) TestResult {
	// 设置默认值
	if config.MessageSize == 0 {
		config.MessageSize = 50 * 1024 // 50KB
	}
	if config.Concurrency == 0 {
		config.Concurrency = 5
	}
	if config.MessagesPerConnection == 0 {
		config.MessagesPerConnection = 100
	}
	if config.Duration == 0 {
		config.Duration = 2 * time.Minute
	}

	testName := fmt.Sprintf("中等消息吞吐量测试 (%dKB, %d并发)", config.MessageSize/1024, config.Concurrency)
	fmt.Printf("\n开始 %s...\n", testName)

	return runThroughputTest(testName, config)
}

// 大消息吞吐量测试 (1MB-2MB)
func RunLargeMsgThroughputTest(config ThroughputTestConfig) TestResult {
	// 设置默认值
	if config.MessageSize == 0 {
		config.MessageSize = 1 * 1024 * 1024 // 1MB
	}
	if config.Concurrency == 0 {
		config.Concurrency = 3
	}
	if config.MessagesPerConnection == 0 {
		config.MessagesPerConnection = 10
	}
	if config.Duration == 0 {
		config.Duration = 3 * time.Minute
	}

	testName := fmt.Sprintf("大消息吞吐量测试 (%dMB, %d并发)", config.MessageSize/(1024*1024), config.Concurrency)
	fmt.Printf("\n开始 %s...\n", testName)

	return runThroughputTest(testName, config)
}

// 超大消息吞吐量测试 (> 10MB)
func RunHugeMsgThroughputTest(config ThroughputTestConfig) TestResult {
	// 设置默认值
	if config.MessageSize == 0 {
		config.MessageSize = 10 * 1024 * 1024 // 10MB
	}
	if config.Concurrency == 0 {
		config.Concurrency = 2
	}
	if config.MessagesPerConnection == 0 {
		config.MessagesPerConnection = 5
	}
	if config.Duration == 0 {
		config.Duration = 5 * time.Minute
	}

	testName := fmt.Sprintf("超大消息吞吐量测试 (%dMB, %d并发)", config.MessageSize/(1024*1024), config.Concurrency)
	fmt.Printf("\n开始 %s...\n", testName)

	return runThroughputTest(testName, config)
}

// 内部通用吞吐量测试实现
func runThroughputTest(testName string, config ThroughputTestConfig) TestResult {
	// 创建收集器
	collector := NewMetricsCollector()

	// 创建测试对象 (用于dep2p网络)
	t := defaultLogger

	// 启动资源监控
	monitorDone := StartResourceMonitor(collector, 500*time.Millisecond)
	defer close(monitorDone)

	// 创建等待组
	var wg sync.WaitGroup

	// 测试是否应该结束的信号
	testDone := make(chan struct{})

	// 准备测试数据
	testData := GenFixedSizeMsg(config.MessageSize)

	// 记录连接创建成功和失败计数
	var connectionSuccessCount int32
	var connectionFailureCount int32

	// 启动并发测试
	for i := 0; i < config.Concurrency; i++ {
		wg.Add(1)
		go func(routineID int) {
			defer wg.Done()

			// 创建conn连接对
			connPair, cleanup, err := setupDep2pConnection(t)
			// 延迟执行清理，但确保一定执行
			defer func() {
				if cleanup != nil {
					cleanup()
				}
			}()

			// 如果连接建立失败，记录错误并返回
			if err != nil {
				if config.Verbose {
					log.Printf("无法建立连接 (routineID=%d): %v", routineID, err)
				}
				atomic.AddInt32(&connectionFailureCount, 1)
				collector.RecordError()
				return
			}

			// 连接成功
			atomic.AddInt32(&connectionSuccessCount, 1)

			// 使用创建的transporter
			sender := connPair.SenderSideConn
			receiver := connPair.ReceiverSideConn

			// 检查双方连接是否都成功创建
			if sender == nil || receiver == nil {
				if config.Verbose {
					log.Printf("连接创建不完整 (routineID=%d): sender=%v, receiver=%v",
						routineID, sender != nil, receiver != nil)
				}
				collector.RecordError()
				return
			}

			// 设置超时以避免无限阻塞
			sender.SetDeadline(time.Now().Add(30 * time.Second))
			receiver.SetDeadline(time.Now().Add(30 * time.Second))

			// 接收goroutine
			var receivedCount int64
			recvDone := make(chan struct{})
			recvErr := make(chan error, 1)

			go func() {
				defer close(recvDone)

				for {
					select {
					case <-testDone:
						return // 测试结束
					default:
						// 继续接收
					}

					// 每次接收前重置超时
					receiver.SetReadDeadline(time.Now().Add(5 * time.Second))

					data, err := receiver.Receive()
					if err != nil {
						if config.Verbose {
							log.Printf("接收错误 (routineID=%d): %v", routineID, err)
						}
						recvErr <- err
						collector.RecordError()
						return
					}

					// 验证数据大小
					if len(data) != config.MessageSize {
						if config.Verbose {
							log.Printf("接收数据大小不匹配: 预期=%d, 实际=%d", config.MessageSize, len(data))
						}
						collector.RecordError()
					}

					newCount := atomic.AddInt64(&receivedCount, 1)

					// 检查是否接收完所有消息
					if newCount >= int64(config.MessagesPerConnection) {
						return
					}
				}
			}()

			// 发送消息，带有重试机制
			for j := 0; j < config.MessagesPerConnection; j++ {
				select {
				case <-testDone:
					return // 测试时间到，终止发送
				case err := <-recvErr:
					if config.Verbose {
						log.Printf("接收端出错，停止发送 (routineID=%d): %v", routineID, err)
					}
					return // 接收端出错，停止发送
				default:
					// 继续发送
				}

				// 重试机制
				var err error
				var latency time.Duration
				maxRetries := 3

				for retry := 0; retry < maxRetries; retry++ {
					// 设置写超时
					sender.SetWriteDeadline(time.Now().Add(5 * time.Second))

					start := time.Now()
					err = sender.Send(testData)
					latency = time.Since(start)

					if err == nil {
						// 发送成功
						break
					}

					if config.Verbose && retry < maxRetries-1 {
						log.Printf("发送重试 (routineID=%d, msgID=%d, retry=%d): %v",
							routineID, j, retry, err)
					}

					collector.RecordRetry()

					// 最后一次重试失败就放弃
					if retry == maxRetries-1 {
						if config.Verbose {
							log.Printf("发送失败，达到最大重试次数 (routineID=%d, msgID=%d): %v",
								routineID, j, err)
						}
						collector.RecordError()
						continue
					}

					// 退避等待
					backoff := time.Duration(50*(retry+1)) * time.Millisecond
					select {
					case <-testDone:
						return // 测试时间到，终止发送
					case <-time.After(backoff):
						// 继续重试
					}
				}

				if err == nil {
					collector.RecordMessage(len(testData), latency)
				}
			}

			// 等待接收完成或测试结束
			select {
			case <-recvDone:
				// 接收正常完成
				if config.Verbose {
					log.Printf("接收完成 (routineID=%d): 收到 %d 条消息", routineID, atomic.LoadInt64(&receivedCount))
				}
			case err := <-recvErr:
				// 接收出错
				if config.Verbose {
					log.Printf("接收出错 (routineID=%d): %v", routineID, err)
				}
			case <-testDone:
				// 测试结束
			case <-time.After(10 * time.Second):
				// 接收超时
				if config.Verbose {
					log.Printf("等待接收完成超时 (routineID=%d): 已收到 %d/%d 条消息",
						routineID, atomic.LoadInt64(&receivedCount), config.MessagesPerConnection)
				}
				collector.RecordTimeout()
			}

		}(i)
	}

	// 等待测试完成或超时
	go func() {
		wg.Wait()
		close(testDone)
	}()

	select {
	case <-testDone:
		// 测试正常完成
	case <-time.After(config.Duration):
		// 测试超时
		close(testDone) // 通知所有goroutine停止
		fmt.Println("测试达到最大运行时间，强制结束")
	}

	// 完成测试
	collector.Complete()
	result := collector.GenerateResult(testName)

	// 添加连接统计
	fmt.Printf("\n连接统计:\n")
	fmt.Printf("成功连接数: %d/%d\n", atomic.LoadInt32(&connectionSuccessCount), config.Concurrency)
	fmt.Printf("失败连接数: %d/%d\n", atomic.LoadInt32(&connectionFailureCount), config.Concurrency)

	// 打印结果
	PrintTestResult(result)

	return result
}

// 运行所有吞吐量测试
func RunAllThroughputTests(verbose bool) []TestResult {
	var results []TestResult

	// 小规模测试，更容易成功
	miniConfig := ThroughputTestConfig{
		MessageSize:           256, // 只有256字节
		Concurrency:           2,   // 只有2个并发连接
		MessagesPerConnection: 10,  // 每个连接只发10条消息
		Duration:              30 * time.Second,
		Verbose:               verbose,
	}
	fmt.Println("\n================ 最小吞吐量测试 ================")
	results = append(results, RunSmallMsgThroughputTest(miniConfig))

	// 1. 小消息吞吐量测试 (减少规模)
	smallConfig := ThroughputTestConfig{
		MessageSize:           512, // 512 bytes
		Concurrency:           5,   // 减少并发数
		MessagesPerConnection: 50,  // 减少消息数
		Duration:              1 * time.Minute,
		Verbose:               verbose,
	}
	fmt.Println("\n================ 小消息吞吐量测试 ================")
	results = append(results, RunSmallMsgThroughputTest(smallConfig))

	// 2. 中等消息吞吐量测试
	mediumConfig := ThroughputTestConfig{
		MessageSize:           50 * 1024, // 50KB
		Concurrency:           3,         // 减少并发数
		MessagesPerConnection: 20,        // 减少消息数
		Duration:              1 * time.Minute,
		Verbose:               verbose,
	}
	fmt.Println("\n================ 中等消息吞吐量测试 ================")
	results = append(results, RunMediumMsgThroughputTest(mediumConfig))

	// 3. 大消息吞吐量测试
	largeConfig := ThroughputTestConfig{
		MessageSize:           1 * 1024 * 1024, // 1MB
		Concurrency:           2,               // 减少并发数
		MessagesPerConnection: 5,               // 减少消息数
		Duration:              1 * time.Minute,
		Verbose:               verbose,
	}
	fmt.Println("\n================ 大消息吞吐量测试 ================")
	results = append(results, RunLargeMsgThroughputTest(largeConfig))

	// 4. 超大消息吞吐量测试
	hugeConfig := ThroughputTestConfig{
		MessageSize:           5 * 1024 * 1024, // 5MB
		Concurrency:           1,               // 只有1个连接
		MessagesPerConnection: 2,               // 只发2条消息
		Duration:              2 * time.Minute,
		Verbose:               verbose,
	}
	fmt.Println("\n================ 超大消息吞吐量测试 ================")
	results = append(results, RunHugeMsgThroughputTest(hugeConfig))

	return results
}
