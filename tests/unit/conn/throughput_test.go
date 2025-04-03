package pointsub_test

import (
	"context"
	"io"
	"math/rand"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 定义吞吐量测试所需的常量
const (
	// 测试数据量大小
	smallDataSize  = 512 * 1024       // 512KB
	mediumDataSize = 1 * 1024 * 1024  // 1MB
	largeDataSize  = 5 * 1024 * 1024  // 5MB
	xlargeDataSize = 50 * 1024 * 1024 // 50MB

	// 缓冲区大小
	defaultBufferSize = 4096 // 4KB

	// 测试持续时间
	shortTestDuration  = 1 * time.Second
	mediumTestDuration = 2 * time.Second
	longTestDuration   = 10 * time.Second
	fullTestDuration   = 30 * time.Second
)

// 获取测试配置
func getTestConfig(t *testing.T) (dataSize int64, duration time.Duration, connectionCount int) {
	if testing.Short() {
		return smallDataSize, shortTestDuration, 2
	}

	// 检查是否启用了完整测试
	if os.Getenv("TEST_FULL") == "1" {
		return xlargeDataSize, fullTestDuration, 100
	}

	// 默认配置
	return mediumDataSize, mediumTestDuration, 4
}

// 生成指定大小的随机数据
func generateRandomData(size int) []byte {
	data := make([]byte, size)
	rand.Read(data)
	return data
}

// 基本吞吐量测试：单连接，发送固定大小数据
func TestThroughputBasic(t *testing.T) {
	dataSize, duration, _ := getTestConfig(t)

	// 创建TCP监听器
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("创建监听器失败: %v", err)
	}
	defer listener.Close()

	// 记录地址
	addr := listener.Addr().String()

	// 创建上下文以便取消
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	// 计数器
	var bytesReceived int64

	// 启动服务器
	serverReady := make(chan struct{})
	serverDone := make(chan struct{})

	go func() {
		// 接受连接
		close(serverReady)
		conn, err := listener.Accept()
		if err != nil {
			t.Logf("接受连接错误: %v", err)
			return
		}
		defer conn.Close()

		// 读取数据并计数
		buffer := make([]byte, defaultBufferSize)
		for {
			select {
			case <-ctx.Done():
				close(serverDone)
				return
			default:
				n, err := conn.Read(buffer)
				if err != nil {
					if err != io.EOF && ctx.Err() == nil {
						t.Logf("读取错误: %v", err)
					}
					close(serverDone)
					return
				}
				atomic.AddInt64(&bytesReceived, int64(n))
			}
		}
	}()

	// 等待服务器准备就绪
	<-serverReady

	// 准备测试数据
	testData := generateRandomData(int(dataSize))

	// 连接到服务器
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("连接到服务器失败: %v", err)
	}
	defer conn.Close()

	// 记录开始时间
	startTime := time.Now()

	// 持续发送数据直到上下文取消
	bytesSent := 0
	buffer := make([]byte, defaultBufferSize)

	for ctx.Err() == nil {
		// 复制一部分测试数据到缓冲区
		copySize := defaultBufferSize
		if bytesSent+copySize > len(testData) {
			bytesSent = 0 // 循环使用测试数据
		}
		copy(buffer, testData[bytesSent:bytesSent+copySize])

		// 发送数据
		n, err := conn.Write(buffer)
		if err != nil {
			if ctx.Err() == nil {
				t.Logf("发送数据错误: %v", err)
			}
			break
		}
		bytesSent += n
	}

	// 等待服务器完成
	<-serverDone

	// 计算吞吐量
	elapsedTime := time.Since(startTime)
	totalBytesReceived := atomic.LoadInt64(&bytesReceived)
	throughputBytesPerSec := float64(totalBytesReceived) / elapsedTime.Seconds()
	throughputMBPerSec := throughputBytesPerSec / (1024 * 1024)

	t.Logf("基本吞吐量测试结果:")
	t.Logf("  接收数据总量: %.2f MB", float64(totalBytesReceived)/(1024*1024))
	t.Logf("  测试时长: %.2f 秒", elapsedTime.Seconds())
	t.Logf("  吞吐量: %.2f MB/s", throughputMBPerSec)

	// 根据测试环境设置合理的最低吞吐量期望
	// 本地机器通常可以达到很高的吞吐量
	minimumExpectedThroughput := 50.0 // MB/s
	if throughputMBPerSec < minimumExpectedThroughput {
		t.Errorf("吞吐量不达标: %.2f MB/s, 期望至少 %.2f MB/s",
			throughputMBPerSec, minimumExpectedThroughput)
	}
}

// 多连接并发吞吐量测试
func TestThroughputConcurrent(t *testing.T) {
	_, duration, connectionCount := getTestConfig(t)

	// 创建TCP监听器
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("创建监听器失败: %v", err)
	}
	defer listener.Close()

	// 记录地址
	addr := listener.Addr().String()

	// 创建上下文以便取消
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	// 计数器
	var totalBytesReceived int64

	// 准备服务器
	var wg sync.WaitGroup
	serverReady := make(chan struct{})

	// 启动服务器处理多个连接
	go func() {
		// 发出准备信号
		close(serverReady)

		for i := 0; i < connectionCount; i++ {
			wg.Add(1)

			go func(connID int) {
				defer wg.Done()

				// 接受连接
				conn, err := listener.Accept()
				if err != nil {
					if ctx.Err() == nil {
						t.Logf("接受连接 %d 错误: %v", connID, err)
					}
					return
				}
				defer conn.Close()

				// 读取数据并计数
				buffer := make([]byte, defaultBufferSize)
				var connBytesReceived int64

				for {
					select {
					case <-ctx.Done():
						t.Logf("连接 %d 接收: %.2f MB",
							connID, float64(connBytesReceived)/(1024*1024))
						return
					default:
						n, err := conn.Read(buffer)
						if err != nil {
							if err != io.EOF && ctx.Err() == nil {
								t.Logf("连接 %d 读取错误: %v", connID, err)
							}
							t.Logf("连接 %d 接收: %.2f MB",
								connID, float64(connBytesReceived)/(1024*1024))
							return
						}
						connBytes := int64(n)
						connBytesReceived += connBytes
						atomic.AddInt64(&totalBytesReceived, connBytes)
					}
				}
			}(i)
		}
	}()

	// 等待服务器准备就绪
	<-serverReady

	// 准备客户端
	clientWg := sync.WaitGroup{}
	clientWg.Add(connectionCount)

	// 记录开始时间
	startTime := time.Now()

	// 启动多个客户端连接
	for i := 0; i < connectionCount; i++ {
		go func(clientID int) {
			defer clientWg.Done()

			// 连接到服务器
			conn, err := net.Dial("tcp", addr)
			if err != nil {
				t.Logf("客户端 %d 连接失败: %v", clientID, err)
				return
			}
			defer conn.Close()

			// 准备测试数据 - 每个客户端使用不同大小的数据
			dataSize := smallDataSize
			if clientID%2 == 1 {
				dataSize = mediumDataSize
			}
			testData := generateRandomData(int(dataSize))

			// 持续发送数据直到上下文取消
			bytesSent := 0
			buffer := make([]byte, defaultBufferSize)
			var clientBytesSent int64

			for ctx.Err() == nil {
				// 复制一部分测试数据到缓冲区
				copySize := defaultBufferSize
				if bytesSent+copySize > len(testData) {
					bytesSent = 0 // 循环使用测试数据
				}
				copy(buffer, testData[bytesSent:bytesSent+copySize])

				// 发送数据
				n, err := conn.Write(buffer)
				if err != nil {
					if ctx.Err() == nil {
						t.Logf("客户端 %d 发送数据错误: %v", clientID, err)
					}
					break
				}
				bytesSent += n
				clientBytesSent += int64(n)
			}

			t.Logf("客户端 %d 发送: %.2f MB",
				clientID, float64(clientBytesSent)/(1024*1024))

		}(i)
	}

	// 等待上下文取消（测试结束）
	<-ctx.Done()

	// 等待客户端完成
	clientWaitCh := make(chan struct{})
	go func() {
		clientWg.Wait()
		close(clientWaitCh)
	}()

	// 添加一点额外时间让客户端优雅关闭
	select {
	case <-clientWaitCh:
		// 客户端全部完成
	case <-time.After(500 * time.Millisecond):
		// 超时，继续执行
	}

	// 计算吞吐量
	elapsedTime := time.Since(startTime)
	bytesReceived := atomic.LoadInt64(&totalBytesReceived)
	throughputBytesPerSec := float64(bytesReceived) / elapsedTime.Seconds()
	throughputMBPerSec := throughputBytesPerSec / (1024 * 1024)

	t.Logf("并发吞吐量测试结果:")
	t.Logf("  连接数: %d", connectionCount)
	t.Logf("  接收数据总量: %.2f MB", float64(bytesReceived)/(1024*1024))
	t.Logf("  测试时长: %.2f 秒", elapsedTime.Seconds())
	t.Logf("  总吞吐量: %.2f MB/s", throughputMBPerSec)
	t.Logf("  每连接平均吞吐量: %.2f MB/s", throughputMBPerSec/float64(connectionCount))

	// 检查吞吐量是否达标
	// 由于是多连接，总吞吐量预期会更高
	minimumExpectedThroughput := 100.0 // MB/s
	if throughputMBPerSec < minimumExpectedThroughput {
		t.Errorf("并发吞吐量不达标: %.2f MB/s, 期望至少 %.2f MB/s",
			throughputMBPerSec, minimumExpectedThroughput)
	}
}

// 测试不同缓冲区大小对吞吐量的影响
func TestThroughputBufferSizes(t *testing.T) {
	if testing.Short() {
		t.Skip("在短测试模式下跳过缓冲区大小测试")
	}

	// 测试不同的缓冲区大小
	bufferSizes := []int{
		1024,   // 1KB
		4096,   // 4KB
		16384,  // 16KB
		65536,  // 64KB
		262144, // 256KB
	}

	// 根据测试模式选择数据大小
	dataSize := mediumDataSize
	if os.Getenv("TEST_FULL") == "1" {
		dataSize = largeDataSize
	}

	results := make(map[int]float64)

	for _, bufferSize := range bufferSizes {
		t.Logf("测试缓冲区大小: %d 字节", bufferSize)

		// 创建TCP监听器
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("创建监听器失败: %v", err)
		}

		// 记录地址
		addr := listener.Addr().String()

		// 创建上下文以便取消
		ctx, cancel := context.WithTimeout(context.Background(), shortTestDuration)

		// 计数器
		var totalBytesReceived int64

		// 启动服务器
		serverReady := make(chan struct{})
		serverDone := make(chan struct{})

		go func() {
			// 接受连接
			close(serverReady)
			conn, err := listener.Accept()
			if err != nil {
				t.Logf("接受连接错误: %v", err)
				close(serverDone)
				return
			}
			defer conn.Close()

			// 设置TCP缓冲区大小
			if tcpConn, ok := conn.(*net.TCPConn); ok {
				tcpConn.SetReadBuffer(bufferSize)
			}

			// 读取数据并计数
			buffer := make([]byte, bufferSize)
			for {
				select {
				case <-ctx.Done():
					close(serverDone)
					return
				default:
					n, err := conn.Read(buffer)
					if err != nil {
						if err != io.EOF && ctx.Err() == nil {
							t.Logf("读取错误: %v", err)
						}
						close(serverDone)
						return
					}
					atomic.AddInt64(&totalBytesReceived, int64(n))
				}
			}
		}()

		// 等待服务器准备就绪
		<-serverReady

		// 准备测试数据
		testData := generateRandomData(int(dataSize))

		// 连接到服务器
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			listener.Close()
			cancel()
			t.Fatalf("连接到服务器失败: %v", err)
		}

		// 设置TCP缓冲区大小
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			tcpConn.SetWriteBuffer(bufferSize)
		}

		// 记录开始时间
		startTime := time.Now()

		// 持续发送数据直到上下文取消
		bytesSent := 0
		buffer := make([]byte, bufferSize)

		for ctx.Err() == nil {
			// 复制一部分测试数据到缓冲区
			copySize := bufferSize
			if bytesSent+copySize > len(testData) {
				bytesSent = 0 // 循环使用测试数据
			}
			copy(buffer, testData[bytesSent:bytesSent+copySize])

			// 发送数据
			n, err := conn.Write(buffer)
			if err != nil {
				if ctx.Err() == nil {
					t.Logf("发送数据错误: %v", err)
				}
				break
			}
			bytesSent += n
		}

		// 关闭连接
		conn.Close()

		// 等待服务器完成
		<-serverDone

		// 计算吞吐量
		elapsedTime := time.Since(startTime)
		bytesReceived := atomic.LoadInt64(&totalBytesReceived)
		throughputBytesPerSec := float64(bytesReceived) / elapsedTime.Seconds()
		throughputMBPerSec := throughputBytesPerSec / (1024 * 1024)

		// 保存结果
		results[bufferSize] = throughputMBPerSec

		t.Logf("  缓冲区大小 %d 字节的吞吐量: %.2f MB/s",
			bufferSize, throughputMBPerSec)

		// 清理
		listener.Close()
		cancel()

		// 短暂暂停，让系统资源恢复
		time.Sleep(500 * time.Millisecond)
	}

	// 分析结果
	var bestBufferSize int
	var bestThroughput float64

	for bufferSize, throughput := range results {
		if throughput > bestThroughput {
			bestBufferSize = bufferSize
			bestThroughput = throughput
		}
	}

	t.Logf("缓冲区大小测试结果总结:")
	t.Logf("  最佳缓冲区大小: %d 字节", bestBufferSize)
	t.Logf("  最佳吞吐量: %.2f MB/s", bestThroughput)

	// 生成比较报告
	t.Logf("各缓冲区大小吞吐量对比:")
	for _, size := range bufferSizes {
		throughput := results[size]
		percentOfBest := (throughput / bestThroughput) * 100
		t.Logf("  %7d 字节: %7.2f MB/s (%6.2f%%)",
			size, throughput, percentOfBest)
	}
}
