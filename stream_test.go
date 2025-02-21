// Package PointSub 提供了基于 dep2p 的流式处理功能
package pointsub

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dep2p/go-dep2p"
	"github.com/dep2p/go-dep2p/core/host"
	"github.com/dep2p/go-dep2p/core/peerstore"
	"github.com/dep2p/go-dep2p/core/protocol"
	"github.com/dep2p/go-dep2p/multiformats/multiaddr"
	"github.com/stretchr/testify/assert"
)

// newHost 创建一个新的 dep2p 主机实例
// 参数:
//   - t: 测试对象,用于报告测试失败
//   - listen: 监听的多地址
//
// 返回值:
//   - host.Host: 创建的 dep2p 主机实例
func newHost(t *testing.T, listen multiaddr.Multiaddr) host.Host {
	// 使用给定的监听地址创建新的 dep2p 主机
	h, err := dep2p.New(
		dep2p.ListenAddrs(listen),
	)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// TestServerClient 测试 dep2p 的服务端和客户端通信功能
// 该测试用例模拟了一个完整的客户端-服务端通信流程:
// 1. 创建两个独立的 dep2p 主机(服务端和客户端)
// 2. 建立连接并交换消息
// 3. 验证连接属性和消息内容
// 参数:
//   - t: 测试对象
func TestServerClient(t *testing.T) {
	// 创建两个本地测试用的多地址
	// 使用 127.0.0.1 环回地址,分别监听 10000 和 10001 端口
	m1, _ := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/10000")
	m2, _ := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/10001")

	// 分别为服务端和客户端创建 dep2p 主机实例
	srvHost := newHost(t, m1)
	clientHost := newHost(t, m2)
	// 确保测试结束时关闭两个主机
	defer srvHost.Close()
	defer clientHost.Close()

	// 在对等存储(peerstore)中添加双方的地址信息
	// 这样双方才能相互发现和连接
	// PermanentAddrTTL 表示这些地址信息永久有效
	srvHost.Peerstore().AddAddrs(clientHost.ID(), clientHost.Addrs(), peerstore.PermanentAddrTTL)
	clientHost.Peerstore().AddAddrs(srvHost.ID(), srvHost.Addrs(), peerstore.PermanentAddrTTL)

	// 定义用于标识此测试通信的协议 ID
	var tag protocol.ID = "/testitytest"
	// 创建可取消的上下文,用于控制整个测试流程
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 创建同步通道,用于等待服务端处理完成
	done := make(chan struct{})
	// 启动服务端处理 goroutine
	go func() {
		defer close(done)
		// 创建基于 dep2p 的网络监听器
		listener, err := Listen(srvHost, tag)
		if err != nil {
			t.Error(err)
			return
		}
		defer listener.Close()

		// 验证监听器的地址是否为服务端主机的 ID
		if listener.Addr().String() != srvHost.ID().String() {
			t.Error("错误的监听地址")
			return
		}

		// 等待并接受来自客户端的连接
		servConn, err := listener.Accept()
		if err != nil {
			t.Error(err)
			return
		}
		defer servConn.Close()

		// 创建带缓冲的读取器,用于从连接中读取数据
		reader := bufio.NewReader(servConn)
		for {
			// 读取客户端发送的消息,以换行符为分隔
			msg, err := reader.ReadString('\n')
			if err == io.EOF {
				break // 连接关闭
			}
			if err != nil {
				t.Error(err)
				return
			}
			fmt.Printf("请求: ===> %v", msg)
			// 验证接收到的消息内容是否符合预期
			if msg != "dep2p 很棒吗？\n" {
				t.Errorf("收到不良消息: %s", msg)
				return
			}

			// 向客户端发送响应消息
			_, err = servConn.Write([]byte("是的\n"))
			if err != nil {
				t.Error(err)
				return
			}
		}
	}()

	// 客户端通过 Dial 连接到服务端
	// 使用服务端的 ID 和协议标识符建立连接
	clientConn, err := Dial(ctx, clientHost, srvHost.ID(), tag)
	if err != nil {
		t.Fatal(err)
	}

	// 验证连接的本地地址是否为客户端 ID
	if clientConn.LocalAddr().String() != clientHost.ID().String() {
		t.Fatal("错误的本地地址")
	}

	// 验证连接的远程地址是否为服务端 ID
	if clientConn.RemoteAddr().String() != srvHost.ID().String() {
		t.Fatal("远程地址错误")
	}

	// 验证连接使用的网络类型是否正确
	if clientConn.LocalAddr().Network() != Network {
		t.Fatal("网络不佳()")
	}

	// 设置整个连接的超时时间为 1 秒
	err = clientConn.SetDeadline(time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}

	// 设置读操作的超时时间为 1 秒
	err = clientConn.SetReadDeadline(time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}

	// 设置写操作的超时时间为 1 秒
	err = clientConn.SetWriteDeadline(time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}

	// 向服务端发送测试消息
	_, err = clientConn.Write([]byte("dep2p 很棒吗？\n"))
	if err != nil {
		t.Fatal(err)
	}

	// 创建读取器并读取服务端的响应
	reader := bufio.NewReader(clientConn)
	resp, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}

	fmt.Printf("响应: ===> %v", resp)
	// 验证服务端响应的内容是否符合预期
	if string(resp) != "是的\n" {
		t.Errorf("收到不良响应: %s", resp)
	}

	// 关闭客户端连接
	err = clientConn.Close()
	if err != nil {
		t.Fatal(err)
	}
	// 等待服务端处理完成
	<-done
}

// # 方式1：禁用测试超时
// go test -timeout 30m ./...

// # 方式2：使用-short标志运行短版本测试
// go test -short ./...

// # 方式3：只运行这个特定的测试
// go test -timeout 30m -run TestHighConcurrentStreamCommunication

/**
============= 高并发流式通信测试结果 =============
    总请求数: 10000
    并发工作协程数: 50
    成功请求数: 10000
    失败请求数: 0
    服务端接收消息数: 10000
    总耗时: 10.943368833s
    平均请求耗时: 2.960825ms
    最短请求耗时: 51.708µs
    最长请求耗时: 43.800042ms
    每秒处理请求数 (QPS): 913.80
===============================================

============= 高并发流式通信测试结果 =============
    总请求数: 10000
    并发工作协程数: 20
    成功请求数: 8459
    失败请求数: 1541
    服务端接收消息数: 8858
    总耗时: 23.137301458s
    平均请求耗时: 1.25258ms
    最短请求耗时: 4.208µs
    最长请求耗时: 44.067375ms
    每秒处理请求数 (QPS): 365.60
    错误统计:
    - 发送错误: i/o deadline reached: 1142 次
    - 接收错误: i/o deadline reached: 399 次
===============================================

============= 高并发流式通信测试结果 =============
    总请求数: 10000
    并发工作协程数: 10
    成功请求数: 6794
    失败请求数: 3206
    服务端接收消息数: 6979
    总耗时: 36.752512875s
    平均请求耗时: 848.512µs
    最短请求耗时: 3.75µs
    最长请求耗时: 10.563208ms
    每秒处理请求数 (QPS): 184.86
    错误统计:
    - 发送错误: i/o deadline reached: 3021 次
    - 接收错误: i/o deadline reached: 185 次
===============================================
*/

// TestHighConcurrentStreamCommunication 测试基础流式通信的高并发性能
func TestHighConcurrentStreamCommunication(t *testing.T) {
	// 创建服务端和客户端的多地址
	srvAddr, _ := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/10002")
	clientAddr, _ := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/10003")

	// 创建服务端和客户端主机
	srvHost := newHost(t, srvAddr)
	clientHost := newHost(t, clientAddr)
	defer srvHost.Close()
	defer clientHost.Close()

	// 添加对等信息
	srvHost.Peerstore().AddAddrs(clientHost.ID(), clientHost.Addrs(), peerstore.PermanentAddrTTL)
	clientHost.Peerstore().AddAddrs(srvHost.ID(), srvHost.Addrs(), peerstore.PermanentAddrTTL)

	// 定义测试协议
	testProtocol := protocol.ID("/concurrent-test/1.0.0")

	// 修改测试参数，进一步降低并发压力
	const (
		totalRequests     = 10000 // 从200降到100
		concurrentWorkers = 50    // 从20降到10
		requestsPerWorker = totalRequests / concurrentWorkers
	)

	// 统计变量
	var (
		successCount        int32
		failureCount        int32
		totalDuration       time.Duration
		mu                  sync.Mutex
		errorMessages       = make(map[string]int)
		serverReceivedCount int32
	)

	// 修改上下文超时时间
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second) // 从30秒增加到60秒
	defer cancel()

	// 修改服务端处理部分
	done := make(chan struct{})
	go func() {
		defer close(done)

		listener, err := Listen(srvHost, testProtocol)
		if err != nil {
			t.Error(err)
			return
		}
		defer listener.Close()

		// 添加监听器的 context 控制
		listenerCtx, listenerCancel := context.WithCancel(ctx)
		defer listenerCancel()

		// 使用 WaitGroup 追踪所有连接处理
		var wg sync.WaitGroup

		// 启动接受连接的 goroutine
		go func() {
			for {
				select {
				case <-listenerCtx.Done():
					return
				default:
					conn, err := listener.Accept()
					if err != nil {
						if listenerCtx.Err() != nil {
							return // 上下文已取消
						}
						continue
					}

					wg.Add(1)
					go func(c net.Conn) {
						defer wg.Done()
						defer c.Close()

						reader := bufio.NewReader(c)
						for {
							select {
							case <-listenerCtx.Done():
								return
							default:
								msg, err := reader.ReadString('\n')
								if err == io.EOF {
									return
								}
								if err != nil {
									mu.Lock()
									errorMessages["读取错误: "+err.Error()]++
									mu.Unlock()
									return
								}

								// 增加服务端接收计数
								atomic.AddInt32(&serverReceivedCount, 1)
								t.Logf("服务端收到消息: %s", msg)

								// 减少处理延迟
								time.Sleep(time.Duration(rand.Intn(3)) * time.Millisecond)

								response := "已处理: " + msg
								_, err = c.Write([]byte(response))
								if err != nil {
									mu.Lock()
									errorMessages["写入错误: "+err.Error()]++
									mu.Unlock()
									return
								}
								t.Logf("服务端发送响应: %s", response)
							}
						}
					}(conn)
				}
			}
		}()

		// 等待所有请求处理完成
		<-ctx.Done()
		listenerCancel() // 停止接受新连接
		wg.Wait()        // 等待所有连接处理完成
	}()

	// 等待服务端启动
	time.Sleep(100 * time.Millisecond)

	// 创建结果通道
	results := make(chan time.Duration, totalRequests)
	var wg sync.WaitGroup

	// 记录开始时间
	startTime := time.Now()

	// 修改客户端连接超时和重试逻辑
	for i := 0; i < concurrentWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			// 为每个worker创建一个长连接，而不是每次请求都创建新连接
			conn, err := Dial(ctx, clientHost, srvHost.ID(), testProtocol)
			if err != nil {
				atomic.AddInt32(&failureCount, 1)
				mu.Lock()
				errorMessages["连接错误: "+err.Error()]++
				mu.Unlock()
				return
			}
			defer conn.Close()

			// 设置更长的超时时间
			conn.SetDeadline(time.Now().Add(20 * time.Second))

			for j := 0; j < requestsPerWorker; j++ {
				requestStart := time.Now()

				msg := fmt.Sprintf("worker-%d-request-%d\n", workerID, j)
				t.Logf("客户端 %d 发送消息: %s", workerID, msg)
				_, err = conn.Write([]byte(msg))
				if err != nil {
					atomic.AddInt32(&failureCount, 1)
					mu.Lock()
					errorMessages["发送错误: "+err.Error()]++
					mu.Unlock()
					continue
				}

				reader := bufio.NewReader(conn)
				response, err := reader.ReadString('\n')
				if err != nil {
					atomic.AddInt32(&failureCount, 1)
					mu.Lock()
					errorMessages["接收错误: "+err.Error()]++
					mu.Unlock()
					continue
				}
				t.Logf("客户端 %d 收到响应: %s", workerID, response)

				duration := time.Since(requestStart)
				results <- duration
				atomic.AddInt32(&successCount, 1)

				// 增加请求间隔，避免过度竞争
				time.Sleep(time.Millisecond * 50)
			}
		}(i)
	}

	// 等待所有请求完成
	go func() {
		wg.Wait()
		close(results)
	}()

	// 收集结果
	var (
		minDuration = time.Hour
		maxDuration time.Duration
	)

	for duration := range results {
		mu.Lock()
		totalDuration += duration
		if duration < minDuration {
			minDuration = duration
		}
		if duration > maxDuration {
			maxDuration = duration
		}
		mu.Unlock()
	}

	// 计算统计指标
	totalTime := time.Since(startTime)
	successTotal := atomic.LoadInt32(&successCount)
	failureTotal := atomic.LoadInt32(&failureCount)
	avgDuration := totalDuration / time.Duration(successTotal)
	requestsPerSecond := float64(successTotal) / totalTime.Seconds()

	// 输出测试结果
	t.Logf("\n============= 高并发流式通信测试结果 =============")
	t.Logf("总请求数: %d", totalRequests)
	t.Logf("并发工作协程数: %d", concurrentWorkers)
	t.Logf("成功请求数: %d", successTotal)
	t.Logf("失败请求数: %d", failureTotal)
	t.Logf("服务端接收消息数: %d", atomic.LoadInt32(&serverReceivedCount))
	t.Logf("总耗时: %v", totalTime)
	t.Logf("平均请求耗时: %v", avgDuration)
	t.Logf("最短请求耗时: %v", minDuration)
	t.Logf("最长请求耗时: %v", maxDuration)
	t.Logf("每秒处理请求数 (QPS): %.2f", requestsPerSecond)

	if len(errorMessages) > 0 {
		t.Logf("\n错误统计:")
		for errMsg, count := range errorMessages {
			t.Logf("- %s: %d 次", errMsg, count)
		}
	}
	t.Logf("===============================================")

	// 验证测试结果
	assert.Equal(t, totalRequests, int(successTotal+failureTotal), "总请求数应等于成功数+失败数")
	assert.True(t, successTotal > int32(totalRequests*95/100), "成功率应大于95%")
	assert.True(t, avgDuration < 200*time.Millisecond, "平均请求时间应小于200ms")
	assert.True(t, requestsPerSecond > 100, "QPS应大于100")
	assert.Equal(t, totalRequests, int(atomic.LoadInt32(&serverReceivedCount)),
		"服务端接收消息数应等于总请求数")

	// 在测试结束前确保清理
	defer func() {
		// 先取消上下文
		cancel()

		// 等待所有goroutine完成
		wg.Wait()

		// 等待服务端完全关闭
		<-done

		// 给一些时间让资源清理完成
		time.Sleep(200 * time.Millisecond)
	}()
}
