package perflib

import (
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
<<<<<<< HEAD

	"github.com/dep2p/pointsub"
=======
>>>>>>> 6613f0351ad580eb6dda4edd3f91c53cbf4b91a9
)

// 新增全局变量
// 注入错误计数器
var injectedErrors int64

// 稳定性测试配置
type StabilityTestConfig struct {
	// 测试持续时间
	Duration time.Duration

	// 连接数量
	ConnectionCount int

	// 消息大小范围
	MinMessageSize int
	MaxMessageSize int

	// 错误注入频率 (1/N的概率注入错误)
	ErrorInjectionRate int

	// 是否详细输出
	Verbose bool
}

// 长时间稳定性测试 - 测试系统在持续运行下的表现
func RunLongDurationTest(config StabilityTestConfig) TestResult {
	// 设置默认值
	if config.Duration == 0 {
		config.Duration = 10 * time.Minute
	}
	if config.ConnectionCount == 0 {
		config.ConnectionCount = 5
	}
	if config.MinMessageSize == 0 {
		config.MinMessageSize = 1024 // 1KB
	}
	if config.MaxMessageSize == 0 {
		config.MaxMessageSize = 100 * 1024 // 100KB
	}

	testName := fmt.Sprintf("长时间稳定性测试 (%s, %d连接)",
		config.Duration.String(), config.ConnectionCount)
	fmt.Printf("\n开始 %s...\n", testName)

	return runStabilityTest(testName, config, false)
}

// 错误恢复测试 - 测试系统在错误情况下的恢复能力
func RunErrorRecoveryTest(config StabilityTestConfig) TestResult {
	// 设置默认值
	if config.Duration == 0 {
		config.Duration = 5 * time.Minute
	}
	if config.ConnectionCount == 0 {
		config.ConnectionCount = 10
	}
	if config.MinMessageSize == 0 {
		config.MinMessageSize = 1024 // 1KB
	}
	if config.MaxMessageSize == 0 {
		config.MaxMessageSize = 50 * 1024 // 50KB
	}
	if config.ErrorInjectionRate == 0 {
		config.ErrorInjectionRate = 50 // 1/50的概率
	}

	testName := fmt.Sprintf("错误恢复测试 (%s, 错误率:1/%d)",
		config.Duration.String(), config.ErrorInjectionRate)
	fmt.Printf("\n开始 %s...\n", testName)

	return runStabilityTest(testName, config, true)
}

// 内部通用稳定性测试实现
func runStabilityTest(testName string, config StabilityTestConfig, injectErrors bool) TestResult {
	// 创建收集器
	collector := NewMetricsCollector()

	// 创建测试对象 (用于dep2p网络)
	t := defaultLogger

	// 启动资源监控 (采样间隔更长，减少开销)
	monitorDone := StartResourceMonitor(collector, 1*time.Second)
	defer close(monitorDone)

	// 全局协调
	var wg sync.WaitGroup
	testDone := make(chan struct{})

	// 测试进度跟踪
	var totalSent int64
	var totalReceived int64
	var totalErrors int64
	var totalRetries int64
	var injectedErrors int64 // 新增注入错误计数
	progressTicker := time.NewTicker(30 * time.Second)
	defer progressTicker.Stop()

	// 进度报告协程
	go func() {
		for {
			select {
			case <-testDone:
				return
			case <-progressTicker.C:
				sent := atomic.LoadInt64(&totalSent)
				received := atomic.LoadInt64(&totalReceived)
				errors := atomic.LoadInt64(&totalErrors)
				retries := atomic.LoadInt64(&totalRetries)
				injected := atomic.LoadInt64(&injectedErrors) // 加载注入错误计数

				elapsed := time.Since(collector.startTime)
				fmt.Printf("测试进度 [%s]: 已发送 %d 消息, 已接收 %d 消息, 真实错误 %d, 注入错误 %d, 重试 %d\n",
					elapsed.Round(time.Second).String(), sent, received, errors, injected, retries)
			}
		}
	}()

	// 启动所有连接
	for i := 0; i < config.ConnectionCount; i++ {
		wg.Add(1)
		go func(connID int) {
			defer wg.Done()

			// 防止所有连接同时启动
			startDelay := time.Duration(rand.Intn(3000)) * time.Millisecond
			time.Sleep(startDelay)

			// 创建错误注入管道 (用于模拟连接错误)
			errorPipe := &errorInjectPipe{
				injectionRate: config.ErrorInjectionRate,
				enabled:       injectErrors,
			}

			// 连接循环 - 模拟连接断开和重连的场景
			for {
				select {
				case <-testDone:
					return
				default:
					// 继续测试
				}

				// 创建连接对
				connPair, cleanup, err := setupDep2pConnection(t)
				if err != nil {
					if config.Verbose {
						log.Printf("无法建立连接 (connID=%d): %v", connID, err)
					}
					atomic.AddInt64(&totalErrors, 1)
					collector.RecordError()

					// 等待一段时间后重试
					time.Sleep(1 * time.Second)
					continue
				}

				// 获取连接
				senderConn := connPair.SenderSideConn
				receiverConn := connPair.ReceiverSideConn

				// 检查连接是否正常
				if senderConn == nil || receiverConn == nil {
					if config.Verbose {
						log.Printf("连接无效 (connID=%d): sender=%v, receiver=%v",
							connID, senderConn != nil, receiverConn != nil)
					}
					atomic.AddInt64(&totalErrors, 1)
					collector.RecordError()

					// 确保资源被清理
					cleanup()

					// 等待一段时间后重试
					time.Sleep(1 * time.Second)
					continue
				}

				// 应用错误注入包装
				wrappedConn := errorPipe.WrapConn(senderConn)

				// 接收协程
				recvDone := make(chan struct{})
				go func() {
					defer close(recvDone)

					for {
						select {
						case <-testDone:
							return
						default:
							// 继续接收
						}

						// 设置超时
						deadline := time.Now().Add(2 * time.Second)
<<<<<<< HEAD
						receiverConn.SetReadDeadline(deadline)
=======
						_ = receiverConn.SetReadDeadline(deadline)
>>>>>>> 6613f0351ad580eb6dda4edd3f91c53cbf4b91a9

						// 接收消息
						data, err := receiverConn.Receive()
						if err != nil {
							// 根据错误类型处理
							if errorPipe.IsInjectedError(err) || err == io.EOF {
								// 注入的错误或连接关闭，不计入错误统计
								if config.Verbose {
									log.Printf("预期内的错误或连接关闭 (connID=%d): %v", connID, err)
								}
								// 不增加totalErrors计数
								return
							} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
								// 超时错误，继续尝试
								continue
							} else if strings.Contains(err.Error(), "use of closed network connection") {
								// 正常的连接关闭场景，不计为错误
								if config.Verbose {
									log.Printf("连接已关闭 (connID=%d): %v", connID, err)
								}
								// 不增加totalErrors计数
								return
							} else {
								// 其他错误 - 真实的系统错误
								if config.Verbose {
									log.Printf("接收错误 (connID=%d): %v", connID, err)
								}
								atomic.AddInt64(&totalErrors, 1)
								collector.RecordError()
								return
							}
						}

						// 更新计数
						atomic.AddInt64(&totalReceived, 1)

						// 记录收到的消息大小
						if data != nil {
							collector.RecordMessage(len(data), 0)
						}
					}
				}()

				// 发送消息循环
			msgLoop:
				for msgCount := 0; msgCount < 50; msgCount++ { // 限制每个连接发送的消息数
					select {
					case <-testDone:
						senderConn.Close()
						receiverConn.Close()
						return
					default:
						// 继续发送
					}

					// 暂停一下，控制发送速率
					time.Sleep(time.Duration(rand.Intn(500)+50) * time.Millisecond)

					// 生成随机大小的消息
					msgSize := rand.Intn(config.MaxMessageSize-config.MinMessageSize) + config.MinMessageSize
					msgData := GenRandomSizeMsg(msgSize)

					// 使用Send方法发送消息而不是Write
					// 设置写超时
					wrappedConn.SetWriteDeadline(time.Now().Add(2 * time.Second))
					err := wrappedConn.Send(msgData)
					if err != nil {
						// 连接可能已关闭
						if errorPipe.IsInjectedError(err) {
							// 注入的错误不计入真实错误
							if config.Verbose {
								log.Printf("注入的发送错误 (connID=%d): %v", connID, err)
							}
							break msgLoop
						} else if strings.Contains(err.Error(), "use of closed network connection") {
							// 正常的连接关闭场景，不计为错误
							if config.Verbose {
								log.Printf("发送时连接已关闭 (connID=%d): %v", connID, err)
							}
							break msgLoop
						} else {
							// 真实系统错误
							if config.Verbose {
								log.Printf("发送错误 (connID=%d): %v", connID, err)
							}
							atomic.AddInt64(&totalErrors, 1)
							collector.RecordError()
							break msgLoop
						}
					}

					// 更新计数
					atomic.AddInt64(&totalSent, 1)
					collector.RecordSend(int64(len(msgData)))
				}

				// 关闭连接并等待接收协程结束
				senderConn.Close()
				receiverConn.Close()
				cleanup() // 清理资源
				<-recvDone

				// 随机等待一段时间后重新建立连接
				if injectErrors {
					reconnectDelay := time.Duration(rand.Intn(1000)+200) * time.Millisecond
					time.Sleep(reconnectDelay)
				} else {
					reconnectDelay := time.Duration(rand.Intn(100)+50) * time.Millisecond
					time.Sleep(reconnectDelay)
				}
			}
		}(i)
	}

	// 等待测试时间结束
	time.Sleep(config.Duration)
	close(testDone)

	// 等待所有协程退出
	wg.Wait()

	// 完成测试
	collector.Complete()
	result := collector.GenerateResult(testName)

	// 添加其他统计信息
	result.ErrorCount = atomic.LoadInt64(&totalErrors)
	result.RetryCount = atomic.LoadInt64(&totalRetries)

	// 获取注入错误计数
	injectedCount := atomic.LoadInt64(&injectedErrors)

	// 打印最终状态
	fmt.Printf("测试完成: 共发送 %d 消息, 接收 %d 消息, 真实错误 %d, 注入错误 %d, 重试 %d\n",
		atomic.LoadInt64(&totalSent), atomic.LoadInt64(&totalReceived),
		result.ErrorCount, injectedCount, result.RetryCount)

	// 打印结果
	PrintTestResult(result)

	return result
}

// 错误注入网络连接包装器
type errorInjectPipe struct {
<<<<<<< HEAD
	conn          pointsub.MessageTransporter // 使用标准MessageTransporter接口
=======
	conn          *PerfTestMessageTransporter // 改为使用*PerfTestMessageTransporter而不是net.Conn
>>>>>>> 6613f0351ad580eb6dda4edd3f91c53cbf4b91a9
	injectionRate int                         // 注入错误的频率 (1/N的概率)
	enabled       bool                        // 是否启用错误注入
}

// WrapConn 包装连接，应用错误注入
<<<<<<< HEAD
func (e *errorInjectPipe) WrapConn(conn pointsub.MessageTransporter) *errorInjectPipe {
=======
func (e *errorInjectPipe) WrapConn(conn *PerfTestMessageTransporter) *errorInjectPipe {
>>>>>>> 6613f0351ad580eb6dda4edd3f91c53cbf4b91a9
	e.conn = conn
	return e
}

// IsInjectedError 判断是否为注入的错误
func (e *errorInjectPipe) IsInjectedError(err error) bool {
	// 判断是否为注入的错误
	isInjected := err != nil && err.Error() == "injected error"

	// 如果是注入错误，增加计数
	if isInjected && e.enabled {
		atomic.AddInt64(&injectedErrors, 1)
	}

	return isInjected
}

// Send 发送消息，可能注入错误
func (e *errorInjectPipe) Send(data []byte) error {
	if e.enabled && rand.Intn(e.injectionRate) == 0 {
		// 模拟随机错误
		return fmt.Errorf("injected error")
	}

	if e.conn == nil {
		return fmt.Errorf("connection is nil")
	}

	return e.conn.Send(data)
}

<<<<<<< HEAD
// SetDeadline 设置读写超时
=======
// Read 实现io.Reader接口
func (e *errorInjectPipe) Read(b []byte) (n int, err error) {
	if e.enabled && rand.Intn(e.injectionRate) == 0 {
		// 模拟随机错误
		return 0, fmt.Errorf("injected error")
	}
	return e.conn.Read(b)
}

// Write 实现io.Writer接口
func (e *errorInjectPipe) Write(b []byte) (n int, err error) {
	if e.enabled && rand.Intn(e.injectionRate) == 0 {
		// 模拟随机错误
		return 0, fmt.Errorf("injected error")
	}
	return e.conn.Write(b)
}

// Close 实现io.Closer接口
func (e *errorInjectPipe) Close() error {
	if e.conn == nil {
		return nil
	}
	return e.conn.Close()
}

// LocalAddr 实现net.Conn接口
func (e *errorInjectPipe) LocalAddr() net.Addr {
	if e.conn == nil {
		return nil
	}
	return e.conn.LocalAddr()
}

// RemoteAddr 实现net.Conn接口
func (e *errorInjectPipe) RemoteAddr() net.Addr {
	if e.conn == nil {
		return nil
	}
	return e.conn.RemoteAddr()
}

// SetDeadline 实现net.Conn接口
>>>>>>> 6613f0351ad580eb6dda4edd3f91c53cbf4b91a9
func (e *errorInjectPipe) SetDeadline(t time.Time) error {
	if e.conn == nil {
		return fmt.Errorf("connection is nil")
	}
	return e.conn.SetDeadline(t)
}

<<<<<<< HEAD
// SetReadDeadline 设置读取超时
=======
// SetReadDeadline 实现net.Conn接口
>>>>>>> 6613f0351ad580eb6dda4edd3f91c53cbf4b91a9
func (e *errorInjectPipe) SetReadDeadline(t time.Time) error {
	if e.conn == nil {
		return fmt.Errorf("connection is nil")
	}
	return e.conn.SetReadDeadline(t)
}

<<<<<<< HEAD
// SetWriteDeadline 设置写入超时
=======
// SetWriteDeadline 实现net.Conn接口
>>>>>>> 6613f0351ad580eb6dda4edd3f91c53cbf4b91a9
func (e *errorInjectPipe) SetWriteDeadline(t time.Time) error {
	if e.conn == nil {
		return fmt.Errorf("connection is nil")
	}
	return e.conn.SetWriteDeadline(t)
}

<<<<<<< HEAD
// Close 关闭连接
func (e *errorInjectPipe) Close() error {
	if e.conn == nil {
		return nil
	}
	return e.conn.Close()
}

=======
>>>>>>> 6613f0351ad580eb6dda4edd3f91c53cbf4b91a9
// 运行所有稳定性测试
func RunAllStabilityTests(verbose bool) []TestResult {
	var results []TestResult

	// 1. 长时间稳定性测试
	longDurationConfig := StabilityTestConfig{
		Duration:        10 * time.Minute, // 实际测试可设置更长时间，如1小时或更多
		ConnectionCount: 5,
		MinMessageSize:  1024,       // 1KB
		MaxMessageSize:  100 * 1024, // 100KB
		Verbose:         verbose,
	}

	// 为了示例测试，将测试时间缩短
	if !verbose {
		longDurationConfig.Duration = 3 * time.Minute
	}

	results = append(results, RunLongDurationTest(longDurationConfig))

	// 2. 错误恢复测试
	errorRecoveryConfig := StabilityTestConfig{
		Duration:           5 * time.Minute,
		ConnectionCount:    10,
		MinMessageSize:     1024,      // 1KB
		MaxMessageSize:     50 * 1024, // 50KB
		ErrorInjectionRate: 50,        // 1/50的概率注入错误
		Verbose:            verbose,
	}

	// 为了示例测试，将测试时间缩短
	if !verbose {
		errorRecoveryConfig.Duration = 2 * time.Minute
		errorRecoveryConfig.ErrorInjectionRate = 30 // 提高错误率，加速测试
	}

	results = append(results, RunErrorRecoveryTest(errorRecoveryConfig))

	return results
}
