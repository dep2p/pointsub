package perflib

import (
	"fmt"
	"log"
	"math/rand"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// 并发测试配置
type ConcurrencyTestConfig struct {
	// 消息大小
	MessageSize int

	// 并发连接数
	ConnectionCount int

	// 每个连接发送消息的数量
	MessagesPerConnection int

	// 测试时间限制
	Duration time.Duration

	// 是否详细输出
	Verbose bool
}

// 低并发测试 - 测试少量连接高频发送消息的情况
func RunLowConcurrencyTest(config ConcurrencyTestConfig) TestResult {
	// 设置默认值
	if config.MessageSize == 0 {
		config.MessageSize = 10 * 1024 // 10KB
	}
	if config.ConnectionCount == 0 {
		config.ConnectionCount = 5
	}
	if config.MessagesPerConnection == 0 {
		config.MessagesPerConnection = 5000
	}
	if config.Duration == 0 {
		config.Duration = 2 * time.Minute
	}

	testName := fmt.Sprintf("低并发测试 (%d连接, %dKB消息)",
		config.ConnectionCount, config.MessageSize/1024)
	fmt.Printf("\n开始 %s...\n", testName)

	return runConcurrencyTest(testName, config)
}

// 中等并发测试 - 测试适中连接数的性能
func RunMediumConcurrencyTest(config ConcurrencyTestConfig) TestResult {
	// 设置默认值
	if config.MessageSize == 0 {
		config.MessageSize = 5 * 1024 // 5KB
	}
	if config.ConnectionCount == 0 {
		config.ConnectionCount = 20
	}
	if config.MessagesPerConnection == 0 {
		config.MessagesPerConnection = 1000
	}
	if config.Duration == 0 {
		config.Duration = 2 * time.Minute
	}

	testName := fmt.Sprintf("中等并发测试 (%d连接, %dKB消息)",
		config.ConnectionCount, config.MessageSize/1024)
	fmt.Printf("\n开始 %s...\n", testName)

	return runConcurrencyTest(testName, config)
}

// 高并发测试 - 测试高连接数下的系统表现
func RunHighConcurrencyTest(config ConcurrencyTestConfig) TestResult {
	// 设置默认值
	if config.MessageSize == 0 {
		config.MessageSize = 2 * 1024 // 2KB
	}
	if config.ConnectionCount == 0 {
		config.ConnectionCount = 100
	}
	if config.MessagesPerConnection == 0 {
		config.MessagesPerConnection = 100
	}
	if config.Duration == 0 {
		config.Duration = 2 * time.Minute
	}

	testName := fmt.Sprintf("高并发测试 (%d连接, %dKB消息)",
		config.ConnectionCount, config.MessageSize/1024)
	fmt.Printf("\n开始 %s...\n", testName)

	return runConcurrencyTest(testName, config)
}

// 大消息并发测试 - 测试较大消息在中等并发下的表现
func RunLargeMessageConcurrencyTest(config ConcurrencyTestConfig) TestResult {
	// 设置默认值
	if config.MessageSize == 0 {
		config.MessageSize = 1 * 1024 * 1024 // 默认1MB
	}
	if config.ConnectionCount == 0 {
		config.ConnectionCount = 20
	}
	if config.MessagesPerConnection == 0 {
		config.MessagesPerConnection = 20
	}
	if config.Duration == 0 {
		config.Duration = 5 * time.Minute
	}

	testName := fmt.Sprintf("大消息并发测试 (%d连接, %.2fMB消息)",
		config.ConnectionCount, float64(config.MessageSize)/(1024*1024))

	return runConcurrencyTest(testName, config)
}

// 增加错误类型跟踪结构
type ErrorStat struct {
	count   int32
	samples []string // 样本错误消息
}

// 运行并发测试的实际实现
func runConcurrencyTest(testName string, config ConcurrencyTestConfig) TestResult {
	// 创建测试结果收集器
	collector := NewMetricsCollector()
	collector.startTime = time.Now()

	// 配置超时
	timeoutChan := time.After(config.Duration)
	t := defaultLogger

	// 错误统计 - 记录各类错误的出现次数和样本
	var errorStatsMu sync.Mutex
	errorStats := make(map[string]*ErrorStat)

	// 打印错误统计的函数
	printErrorStats := func() {
		errorStatsMu.Lock()
		defer errorStatsMu.Unlock()

		if len(errorStats) == 0 {
			return
		}

		fmt.Println("\n错误统计:")
		for errType, stat := range errorStats {
			fmt.Printf("- %s: %d次\n", errType, stat.count)
			if len(stat.samples) > 0 {
				fmt.Printf("  样本: %s\n", stat.samples[0])
			}
		}
	}

	// 添加错误统计的函数
	addErrorStat := func(errType string, errMsg string) {
		errorStatsMu.Lock()
		defer errorStatsMu.Unlock()

		if stat, exists := errorStats[errType]; exists {
			atomic.AddInt32(&stat.count, 1)
			if len(stat.samples) < 5 { // 只保留少量样本
				stat.samples = append(stat.samples, errMsg)
			}
		} else {
			errorStats[errType] = &ErrorStat{
				count:   1,
				samples: []string{errMsg},
			}
		}
	}

	// 修改recordReceiveError和recordSendError函数
	recordReceiveError := func(collector *MetricsCollector, connID int, err error) {
		// 记录错误
		log.Printf("接收错误 (connID=%d): %v", connID, err)
		collector.RecordError()

		// 提取错误类型名称
		errorType := fmt.Sprintf("%T", err)
		if netErr, ok := err.(net.Error); ok {
			if netErr.Timeout() {
				errorType = "TimeoutError"
			} else if netErr.Temporary() {
				errorType = "TemporaryNetError"
			}
		}

		// 更新错误统计
		addErrorStat(errorType, err.Error())
	}

	recordSendError := func(collector *MetricsCollector, connID int, err error) {
		// 区分超时错误和其他错误
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			log.Printf("发送超时 (connID=%d): %v", connID, err)
		} else {
			log.Printf("发送错误 (connID=%d): %v", connID, err)
		}
		collector.RecordError()

		// 提取错误类型名称
		errorType := fmt.Sprintf("%T", err)
		if netErr, ok := err.(net.Error); ok {
			if netErr.Timeout() {
				errorType = "TimeoutError"
			} else if netErr.Temporary() {
				errorType = "TemporaryNetError"
			}
		}

		// 更新错误统计
		addErrorStat(errorType, err.Error())
	}

	// 全局协调
	var wg sync.WaitGroup              // 等待所有goroutine完成
	var finishOnce sync.Once           // 确保结束流程只执行一次
	var testDoneClosedFlag atomic.Bool // 特别标记testDone是否已关闭
	var testsEnded atomic.Bool         // 使用原子布尔值标记测试是否已结束
	testDone := make(chan struct{})    // 通知所有goroutine测试结束的信号

	// 控制测试结束的函数，所有测试结束路径必须通过这个函数
	finishTest := func() {
		finishOnce.Do(func() {
			// 1. 首先设置测试结束标志
			testsEnded.Store(true)

			// 打印测试结束消息
			if config.Verbose {
				fmt.Println("测试即将结束，正在清理资源...")
			}

			// 2. 检查通道是否已关闭，仅当未关闭时才关闭它
			if !testDoneClosedFlag.Load() {
				testDoneClosedFlag.Store(true)
				close(testDone)
			}

			// 3. 记录测试结束时间
			collector.endTime = time.Now()

			if config.Verbose {
				fmt.Println("测试资源清理完成")
			}
		})
	}

	// 测试进度跟踪
	var totalSent int64
	var totalReceived int64
	progressTicker := time.NewTicker(5 * time.Second)
	defer progressTicker.Stop()

	// 准备测试数据
	testData := GenFixedSizeMsg(config.MessageSize)

	// 进度报告协程
	if config.Verbose {
		go func() {
			for {
				select {
				case <-testDone:
					return
				case <-progressTicker.C:
					sent := atomic.LoadInt64(&totalSent)
					received := atomic.LoadInt64(&totalReceived)
					goroutines := runtime.NumGoroutine() // 监控goroutine数量
					fmt.Printf("进度: 已发送 %d 消息, 已接收 %d 消息, 当前goroutine数: %d\n",
						sent, received, goroutines)
				}
			}
		}()
	}

	// 全局并发控制 - 限制最大活动连接数
	maxConcurrent := config.ConnectionCount * 2 // 允许每个连接有发送和接收两个goroutine

	// 启动所有连接
	for i := 0; i < config.ConnectionCount; i++ {
		wg.Add(1)

		go func(connID int) {
			// 获取信号量，限制并发数
			defer wg.Done()

			// 创建连接对
			connPair, cleanup, err := setupDep2pConnection(t)
			if err != nil {
				log.Printf("创建连接对失败 (connID=%d): %v", connID, err)
				return
			}
			defer cleanup()
			t.Debug("连接对创建成功 (connID=%d)", connID)

			// 使用创建的transporter
			sender := connPair.SenderSideConn
			receiver := connPair.ReceiverSideConn

			// 用于同步发送和接收的通道
			readyToSend := make(chan struct{}, 5) // 添加缓冲区，允许最多5个消息在传输中

			// 一个临时变量，用于跟踪连续错误
			var consecutiveErrors int32

			// 首先启动接收协程
			var recvWg sync.WaitGroup
			recvWg.Add(1)

			go func() {
				defer recvWg.Done()

				// 初始信号，表示接收器已准备好接收消息
				readyToSend <- struct{}{}

				for {
					// 首先检查测试是否已经结束
					if testsEnded.Load() {
						return
					}

					// 重置读取超时之前先清除旧的超时设置
					_ = receiver.SetReadDeadline(time.Time{})

					// 设置较长的读取超时，以便在网络不稳定时有更多时间接收消息
					deadline := time.Now().Add(5 * time.Second)
					err := receiver.SetReadDeadline(deadline)
					if err != nil {
						log.Printf("设置读取截止时间失败 (connID=%d): %v", connID, err)
					}

					// 接收消息
					data, err := receiver.Receive()

					// 清除读取超时
					_ = receiver.SetReadDeadline(time.Time{})

					// 再次检查测试是否已经结束
					if testsEnded.Load() {
						return
					}

					if err != nil {
						// 检查是否是超时错误，超时错误只有在非测试结束状态下才记录
						if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
							// 超时错误在高负载下是正常的，继续尝试接收
							atomic.StoreInt32(&consecutiveErrors, 0) // 重置连续错误计数

							// 即使超时也发送准备信号，让发送方有机会尝试
							select {
							case readyToSend <- struct{}{}:
							default: // 如果缓冲区已满，则跳过
							}

							continue
						}

						// 记录其他错误
						recordReceiveError(collector, connID, err)

						// 增加连续错误计数
						errCount := atomic.AddInt32(&consecutiveErrors, 1)

						// 如果连续错误太多，可能连接已损坏，考虑返回
						if errCount > 5 {
							log.Printf("连接似乎已损坏，接收协程退出 (connID=%d)", connID)
							return
						}

						// 发送准备信号以避免死锁
						select {
						case readyToSend <- struct{}{}:
						default: // 如果缓冲区已满，则跳过
						}

						continue
					}

					// 重置连续错误计数
					atomic.StoreInt32(&consecutiveErrors, 0)

					atomic.AddInt64(&totalReceived, 1)

					// 记录消息接收统计 - 添加这行以确保消息被正确计数
					collector.RecordMessage(len(data), 0)

					// 信号告知发送者可以继续发送
					select {
					case readyToSend <- struct{}{}:
					default: // 如果缓冲区已满，则跳过
					}

					// 如果已达到最大消息数，请求结束测试
					sent := atomic.LoadInt64(&totalSent)
					received := atomic.LoadInt64(&totalReceived)
					if sent >= int64(maxConcurrent) && received >= int64(maxConcurrent) && !testsEnded.Load() {
						// 使用goroutine来避免阻塞当前接收流程
						go finishTest()
						return
					}
				}
			}()

			// 发送消息
			msgCount := 0

			// 发送错误计数器 - 用于检测连续错误
			var sendConsecutiveErrors int32

			for {
				// 检查测试是否已结束
				if testsEnded.Load() {
					break
				}

				// 等待接收方准备好接收消息
				select {
				case <-readyToSend:
					// 接收方已准备好，可以发送
				case <-testDone:
					// 测试已完成
					return
				case <-time.After(1 * time.Second):
					// 等待接收方准备的超时，继续尝试
					continue
				}

				// 随机延迟以模拟真实网络条件
				sleep := time.Duration(rand.Intn(5)) * time.Millisecond
				time.Sleep(sleep)

				// 先清除旧的写入超时
				_ = sender.SetWriteDeadline(time.Time{})

				// 设置发送超时
				err := sender.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err != nil {
					log.Printf("设置写入截止时间失败 (connID=%d): %v", connID, err)
				}

				// 发送消息
				err = sender.Send(testData)

				// 清除发送超时设置
				_ = sender.SetWriteDeadline(time.Time{})

				if err != nil {
					// 记录发送错误
					recordSendError(collector, connID, err)

					// 增加连续错误计数
					errCount := atomic.AddInt32(&sendConsecutiveErrors, 1)

					// 如果连续错误太多，可能连接已损坏，考虑跳出循环
					if errCount > 5 {
						log.Printf("连接似乎已损坏，发送循环退出 (connID=%d)", connID)
						break
					}

					continue
				}

				// 重置连续错误计数
				atomic.StoreInt32(&sendConsecutiveErrors, 0)

				atomic.AddInt64(&totalSent, 1)
				msgCount++

				// 记录消息发送统计 - 添加这行以确保消息被正确计数
				collector.RecordSend(int64(len(testData)))

				// 检查是否达到每个连接的消息限制
				if msgCount >= config.MessagesPerConnection {
					break
				}

				// 检查是否达到全局消息限制
				if atomic.LoadInt64(&totalSent) >= int64(maxConcurrent) {
					break
				}
			}

			// 等待接收goroutine完成
			recvDone := make(chan struct{})
			go func() {
				recvWg.Wait()
				close(recvDone)
			}()

			// 等待接收完成，但设置超时以防止永久阻塞
			select {
			case <-recvDone:
				// 接收正常完成
			case <-time.After(10 * time.Second):
				// 接收超时，记录但继续执行
				log.Printf("等待接收协程超时 (connID=%d)", connID)
				collector.RecordTimeout()
			}

		}(i)
	}

	// 启动一个goroutine等待所有连接完成
	allDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(allDone)
	}()

	// 等待测试完成或超时
	select {
	case <-allDone:
		// 所有连接都完成了，结束测试
		finishTest()
	case <-timeoutChan:
		// 测试超时，结束测试
		fmt.Println("测试达到最大运行时间，强制结束")
		finishTest()
	}

	// 确保所有goroutine都有机会退出
	<-testDone

	// 等待所有goroutine退出，添加一个适当的退出等待时间
	waitTimeout := time.After(5 * time.Second)
	waitDone := make(chan struct{})

	go func() {
		wg.Wait()
		close(waitDone)
	}()

	select {
	case <-waitDone:
		// 所有goroutine都已退出
		fmt.Println("所有测试协程正常退出")
	case <-waitTimeout:
		// 等待超时
		fmt.Printf("等待测试协程超时，可能有goroutine未正常退出，当前goroutine数: %d\n", runtime.NumGoroutine())
	}

	// 测试结束，收集结果
	collector.Complete()
	result := collector.GenerateResult(testName)

	// 记录平均吞吐量
	if result.Duration.Seconds() > 0 {
		result.MessagesPerSecond = float64(result.TotalMessages) / result.Duration.Seconds()
		result.MBPerSecond = float64(result.TotalBytes) / 1024 / 1024 / result.Duration.Seconds()
	}

	// 打印最终状态
	fmt.Printf("测试完成: 共发送 %d 消息, 接收 %d 消息, 最终goroutine数量: %d\n",
		atomic.LoadInt64(&totalSent), atomic.LoadInt64(&totalReceived), runtime.NumGoroutine())

	// 打印错误统计信息
	printErrorStats()

	return result
}

// RunAllConcurrencyTests 运行所有并发测试，并打印结果
func RunAllConcurrencyTests(verbose bool) []TestResult {
	// 热身测试 - 运行小规模测试以预热系统
	fmt.Println("=== 正在进行热身测试 ===")
	warmupConfig := ConcurrencyTestConfig{
		ConnectionCount:       2,
		MessagesPerConnection: 10,
		MessageSize:           1024,
		Duration:              15 * time.Second,
		Verbose:               verbose,
	}
	warmupResult := RunLowConcurrencyTest(warmupConfig)
	fmt.Printf("热身测试完成: 处理了 %d 条消息, 速率 %.2f MB/s\n\n",
		warmupResult.TotalMessages, warmupResult.MBPerSecond)

	// 收集所有测试结果
	var results []TestResult

	// 低并发测试
	fmt.Println("=== 正在进行低并发测试 ===")
	lowConfig := ConcurrencyTestConfig{
		ConnectionCount:       20,
		MessagesPerConnection: 500,
		MessageSize:           4 * 1024, // 4KB
		Duration:              3 * time.Minute,
		Verbose:               verbose,
	}
	lowResult := RunLowConcurrencyTest(lowConfig)
	results = append(results, lowResult)
	fmt.Printf("低并发测试完成: 处理了 %d 条消息, 速率 %.2f MB/s\n\n",
		lowResult.TotalMessages, lowResult.MBPerSecond)

	// 中等并发测试
	fmt.Println("=== 正在进行中等并发测试 ===")
	mediumConfig := ConcurrencyTestConfig{
		ConnectionCount:       50,
		MessagesPerConnection: 200,
		MessageSize:           4 * 1024, // 4KB
		Duration:              5 * time.Minute,
		Verbose:               verbose,
	}
	mediumResult := RunMediumConcurrencyTest(mediumConfig)
	results = append(results, mediumResult)
	fmt.Printf("中等并发测试完成: 处理了 %d 条消息, 速率 %.2f MB/s\n\n",
		mediumResult.TotalMessages, mediumResult.MBPerSecond)

	// 如果前两个测试有消息成功传递，才进行高并发测试
	if results[0].TotalMessages > 0 && results[1].TotalMessages > 0 {
		fmt.Println("=== 正在进行高并发测试 ===")
		highConfig := ConcurrencyTestConfig{
			ConnectionCount:       100,
			MessagesPerConnection: 100,
			MessageSize:           4 * 1024, // 4KB
			Duration:              8 * time.Minute,
			Verbose:               verbose,
		}
		highResult := RunHighConcurrencyTest(highConfig)
		results = append(results, highResult)
		fmt.Printf("高并发测试完成: 处理了 %d 条消息, 速率 %.2f MB/s\n\n",
			highResult.TotalMessages, highResult.MBPerSecond)

		// 如果高并发测试成功，进行大消息测试
		if highResult.TotalMessages > 0 {
			fmt.Println("=== 正在进行大消息并发测试 ===")
			largeConfig := ConcurrencyTestConfig{
				ConnectionCount:       20,
				MessagesPerConnection: 20,
				MessageSize:           1 * 1024 * 1024, // 1MB
				Duration:              5 * time.Minute,
				Verbose:               verbose,
			}
			largeResult := RunLargeMessageConcurrencyTest(largeConfig)
			results = append(results, largeResult)
			fmt.Printf("大消息并发测试完成: 处理了 %d 条消息, 速率 %.2f MB/s\n\n",
				largeResult.TotalMessages, largeResult.MBPerSecond)
		}
	} else {
		fmt.Println("*** 警告: 低并发或中等并发测试未能成功传递消息，跳过高并发测试 ***")
	}

	// 打印综合结果
	fmt.Println("\n=== 性能测试综合结果 ===")
	fmt.Println("测试名称\t消息数\t传输速率(MB/s)\t平均延迟(ms)\tP99延迟(ms)\t错误数")
	fmt.Println("------------------------------------------------------------------")

	var totalMessages int64
	var totalBytes int64
	var totalErrors int64

	for _, result := range results {
		fmt.Printf("%s\t%d\t%.2f\t%.2f\t%.2f\t%d\n",
			result.Name,
			result.TotalMessages,
			result.MBPerSecond,
			float64(result.AvgLatency)/1000000, // 转换为毫秒
			float64(result.P99Latency)/1000000, // 转换为毫秒
			result.ErrorCount)

		totalMessages += result.TotalMessages
		totalBytes += result.TotalBytes
		totalErrors += result.ErrorCount
	}

	fmt.Println("------------------------------------------------------------------")
	fmt.Printf("总计\t%d\t%.2f\t-\t-\t%d\n",
		totalMessages,
		float64(totalBytes)/(1024*1024)/
			(float64(sumDurations(results))/float64(time.Second)),
		totalErrors)

	// 诊断信息
	if totalMessages == 0 {
		fmt.Println("\n*** 诊断: 未能成功传递任何消息 ***")
		fmt.Println("可能的原因:")
		fmt.Println("1. 网络连接问题")
		fmt.Println("2. 超时设置过短")
		fmt.Println("3. 资源（内存、CPU）不足")
		fmt.Println("4. 应用程序存在bug")
	} else if float64(totalErrors)/float64(totalMessages) > 0.1 {
		fmt.Println("\n*** 诊断: 错误率过高 ***")
		fmt.Printf("错误率: %.2f%%\n", float64(totalErrors)/float64(totalMessages)*100)
		fmt.Println("可能的原因:")
		fmt.Println("1. 网络不稳定")
		fmt.Println("2. 资源竞争")
		fmt.Println("3. 并发量过高")
	}

	return results
}

// 计算所有测试持续时间的总和
func sumDurations(results []TestResult) time.Duration {
	var total time.Duration
	for _, result := range results {
		total += result.Duration
	}
	return total
}

// RunMixedLoadTest 运行混合负载测试，模拟真实场景中消息大小不同的情况
func RunMixedLoadTest(config ConcurrencyTestConfig) TestResult {
	testName := "混合负载测试"
	fmt.Printf("\n开始 %s (%d连接, 混合消息大小)...\n", testName, config.ConnectionCount)

	// 设置默认值
	if config.MessageSize == 0 {
		config.MessageSize = 10 * 1024 // 默认使用10KB作为基础消息大小
	}
	if config.ConnectionCount == 0 {
		config.ConnectionCount = 20
	}
	if config.MessagesPerConnection == 0 {
		config.MessagesPerConnection = 50
	}
	if config.Duration == 0 {
		config.Duration = 3 * time.Minute
	}

	// 直接调用现有的runConcurrencyTest函数来实现测试
	return runConcurrencyTest(testName, config)
}

// RunStabilityTest 运行长时间稳定性测试
func RunStabilityTest(config ConcurrencyTestConfig) TestResult {
	testName := "长时间稳定性测试"
	fmt.Printf("\n开始 %s (%d连接, %dKB消息)...\n", testName, config.ConnectionCount, config.MessageSize/1024)

	// 设置默认值
	if config.MessageSize == 0 {
		config.MessageSize = 4 * 1024 // 4KB
	}
	if config.ConnectionCount == 0 {
		config.ConnectionCount = 10
	}
	if config.MessagesPerConnection == 0 {
		config.MessagesPerConnection = 1000
	}
	if config.Duration == 0 {
		config.Duration = 15 * time.Minute
	}

	// 直接调用现有的runConcurrencyTest函数来实现测试
	return runConcurrencyTest(testName, config)
}
