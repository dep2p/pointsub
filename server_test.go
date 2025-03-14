// Package pointsub 提供了基于 dep2p 的网络通信功能的测试
package pointsub

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dep2p/go-dep2p"
	"github.com/dep2p/go-dep2p/core/host"
	"github.com/dep2p/go-dep2p/core/protocol"
	"github.com/stretchr/testify/assert"
)

// TestLocalClientServerInteraction 测试本地客户端和服务端的交互
// 参数:
//   - t: 测试对象
func TestLocalClientServerInteraction(t *testing.T) {
	// 创建服务端 host
	serverHost, err := dep2p.New()
	assert.NoError(t, err)
	defer serverHost.Close()

	// 创建客户端 host
	clientHost, err := dep2p.New()
	assert.NoError(t, err)
	defer clientHost.Close()

	// 创建服务端
	server, err := NewServer(serverHost,
		WithMaxConcurrentConns(1000),
		WithServerReadTimeout(30*time.Second),
		WithServerWriteTimeout(30*time.Second),
	)
	assert.NoError(t, err)
	defer server.Stop()

	// handler 定义消息处理函数,将接收到的消息反转
	// 参数:
	//   - request: 请求消息字节切片
	// 返回值:
	//   - []byte: 响应消息字节切片
	//   - error: 错误信息
	handler := func(request []byte) ([]byte, error) {
		// 先将字节切片转换为字符串
		str := string(request)

		// 如果是空字符串，直接返回
		if len(str) == 0 {
			return request, nil
		}

		// 将字符串转换为 rune 切片以正确处理 UTF-8 字符
		runes := []rune(str)

		// 反转 rune 切片
		length := len(runes)
		for i := 0; i < length/2; i++ {
			runes[i], runes[length-1-i] = runes[length-1-i], runes[i]
		}

		// 将反转后的 rune 切片转换回字节切片
		return []byte(string(runes)), nil
	}

	// 定义测试协议
	testProtocol := protocol.ID("/test/1.0.0")

	// 启动服务端
	err = server.Start(testProtocol, handler)
	assert.NoError(t, err)

	// 创建客户端
	client, err := NewClient(clientHost,
		WithReadTimeout(30*time.Second),
		WithWriteTimeout(30*time.Second),
		WithMaxRetries(3),
	)
	assert.NoError(t, err)
	defer client.Close()

	// 连接到服务端
	err = clientHost.Connect(context.Background(), serverHost.Peerstore().PeerInfo(serverHost.ID()))
	assert.NoError(t, err)

	// 定义测试用例
	testCases := []struct {
		name     string // 测试名称
		input    string // 输入消息
		expected string // 期望输出
	}{
		{"简单消息", "Hello", "olleH"},
		{"空消息", "", ""},
		{"中文消息", "你好世界", "界世好你"},
		{"特殊字符", "!@#$%", "%$#@!"},
		{"长消息", "The quick brown fox jumps over the lazy dog", "god yzal eht revo spmuj xof nworb kciuq ehT"},
	}

	// 执行测试用例
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// 发送请求
			response, err := client.Send(
				context.Background(),
				serverHost.ID(),
				testProtocol,
				[]byte(tc.input),
			)
			assert.NoError(t, err)
			assert.Equal(t, tc.expected, string(response))
		})

		// 每次请求之间稍作延迟，模拟真实场景
		time.Sleep(100 * time.Millisecond)
	}

	// 测试并发请求
	t.Run("并发请求", func(t *testing.T) {
		concurrentRequests := 10 // 并发请求数
		done := make(chan bool)

		// 启动多个并发请求
		for i := 0; i < concurrentRequests; i++ {
			go func(index int) {
				msg := "Concurrent Message"
				response, err := client.Send(
					context.Background(),
					serverHost.ID(),
					testProtocol,
					[]byte(msg),
				)
				assert.NoError(t, err)
				assert.Equal(t, "egasseM tnerrucnoC", string(response))
				done <- true
			}(i)
		}

		// 等待所有并发请求完成
		for i := 0; i < concurrentRequests; i++ {
			<-done
		}
	})
}

// TestMultiClientServerInteractions 测试多客户端与服务端的交互
// 参数:
//   - t: 测试对象
func TestMultiClientServerInteractions(t *testing.T) {
	// 创建服务端 host
	serverHost, err := dep2p.New()
	assert.NoError(t, err)
	defer serverHost.Close()

	// 创建服务端
	server, err := NewServer(serverHost,
		WithMaxConcurrentConns(1000),
		WithServerReadTimeout(30*time.Second),
		WithServerWriteTimeout(30*time.Second),
	)
	assert.NoError(t, err)
	defer server.Stop()

	// handler 定义消息处理函数,实现简单的问答系统
	// 参数:
	//   - request: 请求消息字节切片
	// 返回值:
	//   - []byte: 响应消息字节切片
	//   - error: 错误信息
	handler := func(request []byte) ([]byte, error) {
		question := string(request)
		var response []byte

		// 根据问题返回不同的响应
		switch question {
		case "你是谁?":
			response = []byte("我是服务器")
		case "现在几点?":
			response = []byte(time.Now().Format("15:04:05"))
		case "ping":
			response = []byte("pong")
		default:
			response = []byte("我不明白你的问题: " + question)
		}

		t.Logf("【服务端】收到请求: %s ==> 响应: %s", question, string(response))
		return response, nil
	}

	// 定义测试协议
	testProtocol := protocol.ID("/chat/1.0.0")

	// 启动服务端
	err = server.Start(testProtocol, handler)
	assert.NoError(t, err)

	// 创建多个客户端
	clientCount := 3
	clients := make([]*Client, clientCount)
	clientHosts := make([]host.Host, clientCount)

	// 初始化所有客户端
	for i := 0; i < clientCount; i++ {
		// 创建客户端 host
		clientHosts[i], err = dep2p.New()
		assert.NoError(t, err)
		defer clientHosts[i].Close()

		// 创建客户端
		clients[i], err = NewClient(clientHosts[i],
			WithReadTimeout(30*time.Second),
			WithWriteTimeout(30*time.Second),
			WithMaxRetries(3),
		)
		assert.NoError(t, err)
		defer clients[i].Close()

		// 连接到服务端
		err = clientHosts[i].Connect(context.Background(), serverHost.Peerstore().PeerInfo(serverHost.ID()))
		assert.NoError(t, err)
	}

	// 使用 WaitGroup 等待所有测试完成
	var wg sync.WaitGroup

	// 客户端1: 频繁发送 ping
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			msg := "ping"
			t.Logf("【客户端1】发送: %s", msg)
			response, err := clients[0].Send(
				context.Background(),
				serverHost.ID(),
				testProtocol,
				[]byte(msg),
			)
			assert.NoError(t, err)
			t.Logf("【客户端1】收到: %s", string(response))
			assert.Equal(t, "pong", string(response))
			time.Sleep(100 * time.Millisecond)
		}
	}()

	// 客户端2: 每秒查询时间
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 3; i++ {
			msg := "现在几点?"
			t.Logf("【客户端2】发送: %s", msg)
			response, err := clients[1].Send(
				context.Background(),
				serverHost.ID(),
				testProtocol,
				[]byte(msg),
			)
			assert.NoError(t, err)
			t.Logf("【客户端2】收到: %s", string(response))
			// 验证返回的是有效的时间格式
			_, err = time.Parse("15:04:05", string(response))
			assert.NoError(t, err)
			time.Sleep(time.Second)
		}
	}()

	// 客户端3: 随机间隔发送身份询问
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 4; i++ {
			msg := "你是谁?"
			t.Logf("【客户端3】发送: %s", msg)
			response, err := clients[2].Send(
				context.Background(),
				serverHost.ID(),
				testProtocol,
				[]byte(msg),
			)
			assert.NoError(t, err)
			t.Logf("【客户端3】收到: %s", string(response))
			assert.Equal(t, "我是服务器", string(response))
			// 随机等待 200-700ms
			time.Sleep(time.Duration(200+rand.Intn(500)) * time.Millisecond)
		}
	}()

	// 设置测试超时
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	// 等待所有测试完成或超时
	select {
	case <-done:
		// 测试正常完成
	case <-time.After(5 * time.Second):
		t.Fatal("测试超时")
	}

	// 获取并打印服务器连接信息
	connInfo := server.GetConnectionsInfo()
	t.Logf("\n============= 服务器连接信息 =============")
	t.Logf("活跃连接数: %d", len(connInfo))
	for i, info := range connInfo {
		t.Logf("连接 %d:\n  远程地址: %s\n  空闲时间: %v",
			i+1, info.RemoteAddr, info.IdleTime)
	}
	t.Logf("=========================================")
}

// TestMultiNodeCommunication 测试多节点之间的通信
// 参数:
//   - t: 测试对象
func TestMultiNodeCommunication(t *testing.T) {
	// 创建多个节点
	nodeCount := 5
	nodes := make([]struct {
		host    host.Host
		server  *Server
		client  *Client
		msgChan chan string
	}, nodeCount)

	// 初始化所有节点
	for i := 0; i < nodeCount; i++ {
		// 创建节点的 host
		h, err := dep2p.New()
		assert.NoError(t, err)
		defer h.Close()

		// 创建服务端
		server, err := NewServer(h,
			WithMaxConcurrentConns(1000),
			WithServerReadTimeout(30*time.Second),
			WithServerWriteTimeout(30*time.Second),
		)
		assert.NoError(t, err)
		defer server.Stop()

		// 创建客户端
		client, err := NewClient(h,
			WithReadTimeout(30*time.Second),
			WithWriteTimeout(30*time.Second),
			WithMaxRetries(3),
		)
		assert.NoError(t, err)
		defer client.Close()

		// 为每个节点创建消息通道
		nodes[i] = struct {
			host    host.Host
			server  *Server
			client  *Client
			msgChan chan string
		}{
			host:    h,
			server:  server,
			client:  client,
			msgChan: make(chan string, 100),
		}

		// handler 定义节点的消息处理函数
		// 参数:
		//   - i: 节点索引
		// 返回值:
		//   - StreamHandler: 消息处理函数
		handler := func(i int) StreamHandler {
			return func(request []byte) ([]byte, error) {
				msg := string(request)
				nodes[i].msgChan <- msg
				response := fmt.Sprintf("节点%d已收到消息: %s", i, msg)
				return []byte(response), nil
			}
		}

		// 启动服务端
		testProtocol := protocol.ID(fmt.Sprintf("/test/node/%d/1.0.0", i))
		err = server.Start(testProtocol, handler(i))
		assert.NoError(t, err)
	}

	// 所有节点相互连接
	for i := 0; i < nodeCount; i++ {
		for j := i + 1; j < nodeCount; j++ {
			err := nodes[i].host.Connect(context.Background(), nodes[j].host.Peerstore().PeerInfo(nodes[j].host.ID()))
			assert.NoError(t, err)
		}
	}

	// 使用WaitGroup等待所有消息发送和接收完成
	var wg sync.WaitGroup

	// 每个节点向其他所有节点发送消息
	for i := 0; i < nodeCount; i++ {
		wg.Add(1)
		go func(fromNode int) {
			defer wg.Done()
			for toNode := 0; toNode < nodeCount; toNode++ {
				if fromNode == toNode {
					continue
				}

				msg := fmt.Sprintf("来自节点%d的消息", fromNode)
				protocol := protocol.ID(fmt.Sprintf("/test/node/%d/1.0.0", toNode))

				// 发送消息
				response, err := nodes[fromNode].client.Send(
					context.Background(),
					nodes[toNode].host.ID(),
					protocol,
					[]byte(msg),
				)
				assert.NoError(t, err)
				t.Logf("节点%d -> 节点%d: %s, 响应: %s",
					fromNode, toNode, msg, string(response))

				// 随机延迟，模拟真实网络情况
				time.Sleep(time.Duration(100+rand.Intn(200)) * time.Millisecond)
			}
		}(i)
	}

	// 监听每个节点接收到的消息
	for i := 0; i < nodeCount; i++ {
		wg.Add(1)
		go func(nodeIndex int) {
			defer wg.Done()
			expectedMsgs := nodeCount - 1 // 期望收到其他所有节点的消息
			receivedMsgs := 0
			timeout := time.After(10 * time.Second)

			for {
				select {
				case msg := <-nodes[nodeIndex].msgChan:
					t.Logf("节点%d收到消息: %s", nodeIndex, msg)
					receivedMsgs++
					if receivedMsgs >= expectedMsgs {
						return
					}
				case <-timeout:
					t.Errorf("节点%d接收消息超时", nodeIndex)
					return
				}
			}
		}(i)
	}

	// 等待所有操作完成
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	// 设置测试超时
	select {
	case <-done:
		// 测试成功完成
		t.Log("所有节点通信测试完成")
	case <-time.After(30 * time.Second):
		t.Fatal("测试超时")
	}

	// 获取并打印每个节点的连接信息
	t.Log("\n=========== 节点连接信息 ===========")
	for i := 0; i < nodeCount; i++ {
		connInfo := nodes[i].server.GetConnectionsInfo()
		t.Logf("节点%d - 活跃连接数: %d", i, len(connInfo))
		for j, info := range connInfo {
			t.Logf("  连接%d: 远程地址=%s, 空闲时间=%v",
				j+1, info.RemoteAddr, info.IdleTime)
		}
	}
	t.Log("===================================")
}

// TestClientServer 测试客户端和服务端之间的通信
func TestClientServer(t *testing.T) {
	// 创建测试环境
	ctx := context.Background()
	serverHost, err := dep2p.New()
	assert.NoError(t, err)
	defer serverHost.Close()

	server, err := NewServer(serverHost,
		WithMaxConcurrentConns(100),          // 降低最大连接数
		WithServerReadTimeout(5*time.Second), // 缩短超时时间
		WithServerWriteTimeout(5*time.Second),
	)
	assert.NoError(t, err)
	defer server.Stop()

	// 创建客户端
	clientHost, err := dep2p.New()
	assert.NoError(t, err)
	defer clientHost.Close()

	client, err := NewClient(clientHost,
		WithReadTimeout(5*time.Second),
		WithWriteTimeout(5*time.Second),
		WithConnectTimeout(2*time.Second),
		WithMaxRetries(2),
	)
	assert.NoError(t, err)
	defer client.Close()

	// 连接到服务端
	err = clientHost.Connect(ctx, serverHost.Peerstore().PeerInfo(serverHost.ID()))
	assert.NoError(t, err)

	// 1. 基本功能测试
	t.Run("Basic functionality", func(t *testing.T) {
		protocolID := protocol.ID("/test/basic/1.0.0")
		err := server.Start(protocolID, func(req []byte) ([]byte, error) {
			return append([]byte("response: "), req...), nil
		})
		assert.NoError(t, err)

		resp, err := client.Send(ctx, serverHost.ID(), protocolID, []byte("hello"))
		assert.NoError(t, err)
		assert.Equal(t, "response: hello", string(resp))
	})

	// 2. 错误处理测试
	t.Run("Error handling", func(t *testing.T) {
		// 测试无效协议
		invalidProtocol := protocol.ID("/invalid/1.0.0")
		_, err := client.Send(ctx, serverHost.ID(), invalidProtocol, []byte("test"))
		assert.Error(t, err)
		t.Logf("Invalid protocol error: %v", err)
		assert.True(t,
			strings.Contains(err.Error(), "创建新的流") ||
				strings.Contains(err.Error(), "protocols not supported"),
			"错误消息应包含协议不支持相关信息")

		// 测试超时
		timeoutProtocol := protocol.ID("/test/timeout/1.0.0")
		err = server.Start(timeoutProtocol, func(req []byte) ([]byte, error) {
			time.Sleep(200 * time.Millisecond)
			return []byte("delayed"), nil
		})
		assert.NoError(t, err)

		ctxTimeout, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		_, err = client.Send(ctxTimeout, serverHost.ID(), timeoutProtocol, []byte("test"))
		assert.Error(t, err)
		t.Logf("Timeout error: %v", err)
		assert.True(t,
			strings.Contains(err.Error(), "deadline exceeded") ||
				strings.Contains(err.Error(), "context deadline exceeded"),
			"错误消息应包含超时相关信息")
	})

	// 3. 并发测试（小规模）
	t.Run("Concurrent requests", func(t *testing.T) {
		protocolID := protocol.ID("/test/concurrent/1.0.0")
		err := server.Start(protocolID, func(req []byte) ([]byte, error) {
			return append([]byte("response: "), req...), nil
		})
		assert.NoError(t, err)

		var wg sync.WaitGroup
		errors := make(chan error, 5)

		// 只发送5个并发请求
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				msg := fmt.Sprintf("concurrent request %d", i)
				resp, err := client.Send(ctx, serverHost.ID(), protocolID, []byte(msg))
				if err != nil {
					errors <- err
					return
				}
				if !strings.Contains(string(resp), msg) {
					errors <- fmt.Errorf("响应不匹配: %s", msg)
				}
			}(i)
		}

		wg.Wait()
		close(errors)

		for err := range errors {
			assert.NoError(t, err)
		}
	})
}

// TestClientConfig 测试客户端配置
func TestClientConfig(t *testing.T) {
	// 测试默认配置
	config := DefaultClientConfig()
	assert.Equal(t, 30*time.Second, config.ReadTimeout)
	assert.Equal(t, 30*time.Second, config.WriteTimeout)
	assert.Equal(t, 5*time.Second, config.ConnectTimeout)
	assert.Equal(t, 3, config.MaxRetries)
	assert.True(t, config.EnableCompression)

	// 测试自定义配置
	clientHost, err := dep2p.New()
	assert.NoError(t, err)
	defer clientHost.Close()

	client, err := NewClient(clientHost,
		WithReadTimeout(10*time.Second),
		WithWriteTimeout(15*time.Second),
		WithConnectTimeout(3*time.Second),
		WithMaxRetries(5),
		WithCompression(false),
	)
	assert.NoError(t, err)
	defer client.Close()

	assert.Equal(t, 10*time.Second, client.config.ReadTimeout)
	assert.Equal(t, 15*time.Second, client.config.WriteTimeout)
	assert.Equal(t, 3*time.Second, client.config.ConnectTimeout)
	assert.Equal(t, 5, client.config.MaxRetries)
	assert.False(t, client.config.EnableCompression)
}

// go test -timeout 30m -run TestInvalidClientConfig
// 测试无效配置
func TestInvalidClientConfig(t *testing.T) {
	clientHost, err := dep2p.New()
	assert.NoError(t, err)
	defer clientHost.Close()

	// 测试无效的读取超时
	_, err = NewClient(clientHost, WithReadTimeout(-1*time.Second))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "读取超时时间必须大于0")

	// 测试无效的重试次数
	_, err = NewClient(clientHost, WithMaxRetries(-1))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "重试次数不能为负数")
}

// go test -timeout 30m -run TestHighConcurrentSend
// TestHighConcurrentSend 测试客户端发送方法在高并发情况下的表现
func TestHighConcurrentSend(t *testing.T) {
	// 创建服务端 host
	serverHost, err := dep2p.New()
	assert.NoError(t, err)
	defer serverHost.Close()

	// 创建服务端
	server, err := NewServer(serverHost,
		WithMaxConcurrentConns(5000),          // 设置较高的并发连接数
		WithServerReadTimeout(30*time.Second), // 设置合理的超时时间
		WithServerWriteTimeout(30*time.Second),
	)
	assert.NoError(t, err)
	defer server.Stop()

	// 创建客户端 host
	clientHost, err := dep2p.New()
	assert.NoError(t, err)
	defer clientHost.Close()

	// 创建客户端
	client, err := NewClient(clientHost,
		WithReadTimeout(30*time.Second),
		WithWriteTimeout(30*time.Second),
		WithMaxRetries(3),
	)
	assert.NoError(t, err)
	defer client.Close()

	// 连接到服务端
	err = clientHost.Connect(context.Background(), serverHost.Peerstore().PeerInfo(serverHost.ID()))
	assert.NoError(t, err)

	// 定义测试协议
	testProtocol := protocol.ID("/test/concurrent/1.0.0")

	// 实现一个模拟延迟的处理函数
	handler := func(request []byte) ([]byte, error) {
		// 随机延迟 10-50ms，模拟真实处理时间
		delay := time.Duration(10+rand.Intn(40)) * time.Millisecond
		time.Sleep(delay)
		return append([]byte("已处理: "), request...), nil
	}

	// 启动服务端
	err = server.Start(testProtocol, handler)
	assert.NoError(t, err)

	// 测试参数
	const (
		totalRequests     = 10000 // 总请求数
		concurrentWorkers = 50    // 并发工作协程数
		requestsPerWorker = totalRequests / concurrentWorkers
	)

	// 准备测试数据
	type result struct {
		duration time.Duration
		err      error
	}

	results := make(chan result, totalRequests)
	var wg sync.WaitGroup

	// 记录开始时间
	startTime := time.Now()

	// 启动并发工作协程
	for i := 0; i < concurrentWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for j := 0; j < requestsPerWorker; j++ {
				requestStart := time.Now()
				msg := fmt.Sprintf("worker-%d-request-%d", workerID, j)

				// 发送请求
				resp, err := client.Send(
					context.Background(),
					serverHost.ID(),
					testProtocol,
					[]byte(msg),
				)

				duration := time.Since(requestStart)
				if err != nil {
					results <- result{duration, err}
					continue
				}

				// 验证响应
				expectedPrefix := "已处理: "
				if !strings.HasPrefix(string(resp), expectedPrefix) {
					results <- result{duration, fmt.Errorf("响应格式错误: %s", string(resp))}
					continue
				}

				results <- result{duration, nil}
			}
		}(i)
	}

	// 等待所有请求完成
	go func() {
		wg.Wait()
		close(results)
	}()

	// 统计结果
	var (
		successCount  int
		failureCount  int
		totalDuration time.Duration
		maxDuration   time.Duration
		minDuration   = time.Hour // 初始设置一个较大值
		errorMessages = make(map[string]int)
	)

	for r := range results {
		if r.err != nil {
			failureCount++
			errorMessages[r.err.Error()]++
		} else {
			successCount++
			totalDuration += r.duration
			if r.duration > maxDuration {
				maxDuration = r.duration
			}
			if r.duration < minDuration {
				minDuration = r.duration
			}
		}
	}

	// 计算统计指标
	totalTime := time.Since(startTime)
	avgDuration := totalDuration / time.Duration(successCount)
	requestsPerSecond := float64(successCount) / totalTime.Seconds()

	// 输出测试结果
	t.Logf("\n============= 高并发测试结果 =============")
	t.Logf("总请求数: %d", totalRequests)
	t.Logf("并发工作协程数: %d", concurrentWorkers)
	t.Logf("成功请求数: %d", successCount)
	t.Logf("失败请求数: %d", failureCount)
	t.Logf("总耗时: %v", totalTime)
	t.Logf("平均请求耗时: %v", avgDuration)
	t.Logf("最长请求耗时: %v", maxDuration)
	t.Logf("最短请求耗时: %v", minDuration)
	t.Logf("每秒处理请求数: %.2f", requestsPerSecond)

	if len(errorMessages) > 0 {
		t.Logf("\n错误统计:")
		for errMsg, count := range errorMessages {
			t.Logf("- %s: %d 次", errMsg, count)
		}
	}
	t.Logf("=======================================")

	// 验证测试结果
	assert.Equal(t, totalRequests, successCount+failureCount, "总请求数应等于成功数+失败数")
	assert.True(t, successCount > totalRequests*95/100, "成功率应大于95%")
	assert.True(t, avgDuration < 200*time.Millisecond, "平均请求时间应小于200ms")
	assert.True(t, requestsPerSecond > 100, "每秒处理请求数应大于100")
}
