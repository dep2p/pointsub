// Package pointsub 提供了基于 LibP2P 的网络通信功能的测试
package pointsub

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/dep2p/libp2p"
	"github.com/dep2p/libp2p/core/host"
	"github.com/dep2p/libp2p/core/protocol"
	"github.com/stretchr/testify/assert"
)

// TestLocalClientServerInteraction 测试本地客户端和服务端的交互
// 参数:
//   - t: 测试对象
func TestLocalClientServerInteraction(t *testing.T) {
	// 创建服务端 host
	serverHost, err := libp2p.New()
	assert.NoError(t, err)
	defer serverHost.Close()

	// 创建客户端 host
	clientHost, err := libp2p.New()
	assert.NoError(t, err)
	defer clientHost.Close()

	// 创建服务端
	server, err := NewServer(serverHost, DefaultServerConfig())
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
	client, err := NewClient(clientHost, DefaultClientConfig())
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
	serverHost, err := libp2p.New()
	assert.NoError(t, err)
	defer serverHost.Close()

	// 创建服务端
	server, err := NewServer(serverHost, DefaultServerConfig())
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
		clientHosts[i], err = libp2p.New()
		assert.NoError(t, err)
		defer clientHosts[i].Close()

		// 创建客户端
		clients[i], err = NewClient(clientHosts[i], DefaultClientConfig())
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
		h, err := libp2p.New()
		assert.NoError(t, err)
		defer h.Close()

		// 创建服务端
		server, err := NewServer(h, DefaultServerConfig())
		assert.NoError(t, err)
		defer server.Stop()

		// 创建客户端
		client, err := NewClient(h, DefaultClientConfig())
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
