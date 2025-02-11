package pointsub

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/dep2p/go-dep2p"
	"github.com/dep2p/go-dep2p/core/host"
	"github.com/dep2p/go-dep2p/core/peer"
	"github.com/dep2p/go-dep2p/core/protocol"
	"github.com/stretchr/testify/assert"
)

// TestNewPointSub 测试创建新的 PointSub 实例
func TestNewPointSub(t *testing.T) {
	// 测试 host 为 nil 的情况
	ps, err := NewPointSub(nil, nil, nil, false)
	assert.Error(t, err)
	assert.Nil(t, ps)
	assert.Contains(t, err.Error(), "host不能为nil")

	// 创建有效的 host
	testHost, err := dep2p.New()
	assert.NoError(t, err)
	defer testHost.Close()

	// 测试基本配置
	t.Run("Basic configuration", func(t *testing.T) {
		clientOpts := []ClientOption{
			WithReadTimeout(5 * time.Second), //
			WithWriteTimeout(5 * time.Second),
			WithConnectTimeout(2 * time.Second),
			WithMaxRetries(3),
			WithCompression(true),
		}

		serverOpts := []ServerOption{
			WithMaxConcurrentConns(100),
			WithServerReadTimeout(5 * time.Second),
			WithServerWriteTimeout(5 * time.Second),
		}

		ps, err := NewPointSub(testHost, clientOpts, serverOpts, true)
		assert.NoError(t, err)
		assert.NotNil(t, ps)
		assert.True(t, ps.IsServerRunning())
		assert.NotNil(t, ps.Server())
		assert.NotNil(t, ps.Client())
		ps.StopServer()
	})

	// 测试仅客户端模式
	t.Run("Client only mode", func(t *testing.T) {
		ps, err := NewPointSub(testHost, nil, nil, false)
		assert.NoError(t, err)
		assert.NotNil(t, ps)
		assert.False(t, ps.IsServerRunning())
		assert.Nil(t, ps.Server())
		assert.NotNil(t, ps.Client())
	})

	// 测试服务器启动和停止
	t.Run("Server start/stop", func(t *testing.T) {
		ps, err := NewPointSub(testHost, nil, nil, false)
		assert.NoError(t, err)

		// 启动服务器
		err = ps.StartServer([]ServerOption{
			WithMaxConcurrentConns(100),
			WithServerReadTimeout(5 * time.Second),
		})
		assert.NoError(t, err)
		assert.True(t, ps.IsServerRunning())

		// 重复启动服务器
		err = ps.StartServer(nil)
		assert.NoError(t, err)

		// 停止服务器
		err = ps.StopServer()
		assert.NoError(t, err)
		assert.False(t, ps.IsServerRunning())

		// 重复停止服务器
		err = ps.StopServer()
		assert.NoError(t, err)
	})

	// 测试完整的客户端-服务器通信
	t.Run("Full client-server communication", func(t *testing.T) {
		t.Log("\n=== 开始客户端-服务器通信测试 ===")
		// 创建服务端 host
		serverHost, err := dep2p.New()
		assert.NoError(t, err)
		defer serverHost.Close()

		// 创建客户端 host
		clientHost, err := dep2p.New()
		assert.NoError(t, err)
		defer clientHost.Close()

		// 用于验证消息接收
		receivedMessages := make(chan []byte, 1)

		// 创建服务端 PointSub
		serverPS, err := NewPointSub(serverHost, nil, nil, true)
		assert.NoError(t, err)
		defer serverPS.Stop()

		// 创建客户端 PointSub
		clientPS, err := NewPointSub(clientHost, nil, nil, false)
		assert.NoError(t, err)

		// 注册处理函数
		protocolID := protocol.ID("/test/1.0.0")
		err = serverPS.Start(protocolID, func(req []byte) ([]byte, error) {
			receivedMessages <- req
			t.Logf("【服务端】收到消息: %s", string(req))
			return append([]byte("response: "), req...), nil
		})
		assert.NoError(t, err)

		// 连接到服务端
		err = clientHost.Connect(context.Background(), serverHost.Peerstore().PeerInfo(serverHost.ID()))
		assert.NoError(t, err)

		// 测试消息发送和接收
		testMessage := []byte("[TEST-001] Initial test message")
		t.Logf("【客户端】发送消息: %s", string(testMessage))
		resp, err := clientPS.Send(
			context.Background(),
			serverHost.ID(),
			protocolID,
			testMessage,
		)
		assert.NoError(t, err)
		t.Logf("【客户端】收到响应: %s", string(resp))
		assert.Equal(t, "response: "+string(testMessage), string(resp))

		// 验证服务端确实收到了消息
		select {
		case received := <-receivedMessages:
			assert.Equal(t, testMessage, received, "服务端接收到的消息与发送的消息不匹配")
		case <-time.After(time.Second):
			t.Error("超时：服务端没有收到消息")
		}

		// 测试多条消息的发送和接收
		t.Run("Multiple messages", func(t *testing.T) {
			t.Log("\n=== 开始多消息测试 ===")
			messages := []string{
				"[TEST-002] Hello message",
				"[TEST-003] 中文测试消息",
				"[TEST-004] Special chars: !@#$%",
				"[TEST-005] Long message with spaces and other content",
			}

			for i, msg := range messages {
				t.Logf("\n--- 消息测试 %d/%d ---", i+1, len(messages))
				t.Logf("【客户端】发送消息: %s", msg)
				resp, err := clientPS.Send(
					context.Background(),
					serverHost.ID(),
					protocolID,
					[]byte(msg),
				)
				assert.NoError(t, err)
				t.Logf("【客户端】收到响应: %s", string(resp))
				assert.Equal(t, "response: "+msg, string(resp))

				// 验证接收
				select {
				case received := <-receivedMessages:
					assert.Equal(t, msg, string(received))
					t.Logf("【验证】消息匹配成功")
				case <-time.After(time.Second):
					t.Errorf("超时：服务端没有收到消息 '%s'", msg)
				}
			}
			t.Log("=== 多消息测试完成 ===")
		})
		t.Log("=== 客户端-服务器通信测试完成 ===")
	})

	// 测试无效的服务器配置
	t.Run("Invalid server configuration", func(t *testing.T) {
		ps, err := NewPointSub(testHost, nil, []ServerOption{
			WithMaxConcurrentConns(-1), // 无效的连接数
		}, true)
		assert.Error(t, err)
		assert.Nil(t, ps)
	})

	// 测试无效的客户端配置
	t.Run("Invalid client configuration", func(t *testing.T) {
		ps, err := NewPointSub(testHost, []ClientOption{
			WithReadTimeout(-1 * time.Second), // 无效的超时时间
		}, nil, false)
		assert.Error(t, err)
		assert.Nil(t, ps)
	})

	// 测试 SendClosest 功能
	t.Run("SendClosest functionality", func(t *testing.T) {
		t.Log("\n=== 开始 SendClosest 测试 ===")

		// 创建测试环境
		ctx := context.Background()
		protocolID := protocol.ID("/test/closest/1.0.0")
		testMsg := []byte("[TEST-CLOSEST] Finding nearest node")

		// 创建多个服务端节点
		numNodes := 3
		servers := make([]*Server, numNodes)
		serverHosts := make([]host.Host, numNodes)

		for i := 0; i < numNodes; i++ {
			h, err := dep2p.New()
			assert.NoError(t, err)
			defer h.Close()

			serverHosts[i] = h
			server, err := NewServer(h,
				WithMaxConcurrentConns(1000),
				WithServerReadTimeout(30*time.Second),
				WithServerWriteTimeout(30*time.Second),
			)
			assert.NoError(t, err)

			nodeID := h.ID()
			// 注册处理函数
			err = server.Start(protocolID, func(req []byte) ([]byte, error) {
				logger.Infof("节点 %s 收到请求: %s", nodeID.String()[:8], string(req))
				resp := append([]byte(fmt.Sprintf("来自节点 %s 的响应: ", nodeID.String()[:8])), req...)
				return resp, nil
			})
			assert.NoError(t, err)
			servers[i] = server
			logger.Infof("创建服务节点 %d: %s", i, nodeID.String()[:8])
		}

		// 创建客户端
		clientHost, err := dep2p.New()
		assert.NoError(t, err)
		defer clientHost.Close()

		client, err := NewClient(clientHost,
			WithReadTimeout(30*time.Second),
			WithWriteTimeout(30*time.Second),
			WithMaxRetries(3),
		)
		assert.NoError(t, err)
		defer client.Close()

		logger.Infof("创建客户端节点: %s", clientHost.ID().String()[:8])

		// 连接到所有服务端节点
		for i, sh := range serverHosts {
			err = clientHost.Connect(ctx, sh.Peerstore().PeerInfo(sh.ID()))
			assert.NoError(t, err)

			// 添加到协议的服务器节点列表
			err = client.AddServerNode(protocolID, sh.ID())
			assert.NoError(t, err)
			logger.Infof("客户端连接到服务节点 %d: %s", i, sh.ID().String()[:8])
		}

		// 测试1: 基本功能 - 发送到最近的节点
		t.Run("Basic functionality", func(t *testing.T) {
			logger.Info("开始测试发送到最近节点...")
			result, err := client.SendClosest(ctx, protocolID, testMsg)
			assert.NoError(t, err)
			logger.Infof("收到响应: %s", string(result.Response))
			assert.Contains(t, string(result.Response), "来自节点")
			assert.Contains(t, string(result.Response), string(testMsg))
		})

		// 测试2: 节点下线后的行为
		t.Run("Node offline behavior", func(t *testing.T) {
			// 清除所有节点
			client.ClearServerNodes(protocolID)

			// 添加一些不可用的节点
			var failedNodes []peer.ID
			for i := 0; i < 3; i++ {
				fakeID, err := peer.Decode("QmYyQSo1c1Ym7orWxLYvCrM2EmxFTANf8wXmmE7DWjhx5N")
				assert.NoError(t, err)
				err = client.AddServerNode(protocolID, fakeID)
				assert.NoError(t, err)
				failedNodes = append(failedNodes, fakeID)
			}

			// 添加一个可用节点
			err = client.AddServerNode(protocolID, serverHosts[1].ID())
			assert.NoError(t, err)

			// 关闭一个节点
			servers[0].Stop()

			result, err := client.SendClosest(ctx, protocolID, []byte("[TEST-OFFLINE] Node down"), failedNodes...)
			assert.NoError(t, err)
			assert.Contains(t, string(result.Response), "来自节点")
			assert.Contains(t, string(result.Response), "[TEST-OFFLINE] Node down")
		})

		// 测试3: 无效协议
		t.Run("Invalid protocol", func(t *testing.T) {
			// 清除所有节点
			client.ClearServerNodes(protocolID)

			invalidProtocol := protocol.ID("/invalid/1.0.0")
			_, err = client.SendClosest(ctx, invalidProtocol, []byte("test"))
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "没有可用的服务器节点")
		})

		// 在函数末尾添加清理代码
		defer func() {
			for _, server := range servers {
				server.Stop()
			}
		}()

		t.Log("=== SendClosest 测试完成 ===")
	})
}
