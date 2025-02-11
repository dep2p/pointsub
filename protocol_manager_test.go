package pointsub

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dep2p/go-dep2p"
	"github.com/dep2p/go-dep2p/core/host"
	"github.com/dep2p/go-dep2p/core/peer"
	"github.com/dep2p/go-dep2p/core/protocol"
	"github.com/stretchr/testify/assert"
)

func TestSendClosest(t *testing.T) {
	// 创建测试环境
	ctx := context.Background()
	protocolID := protocol.ID("/test/1.0.0")
	testMsg := []byte("test message")

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

	// 创建客户端，使用新的选项模式
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

	// 测试 SendClosest
	t.Run("SendClosest success", func(t *testing.T) {
		logger.Info("开始测试发送到最近节点...")
		result, err := client.SendClosest(ctx, protocolID, testMsg)
		assert.NoError(t, err)
		logger.Infof("收到响应: %s", string(result.Response))
		assert.Contains(t, string(result.Response), "来自节点")
		assert.Contains(t, string(result.Response), "test message")
	})

	t.Run("SendClosest with no nodes", func(t *testing.T) {
		// 清除所有节点
		client.ClearServerNodes(protocolID)
		_, err := client.SendClosest(ctx, protocolID, testMsg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "没有可用的服务器节点")
	})

	t.Run("SendClosest with invalid protocol", func(t *testing.T) {
		invalidProtocol := protocol.ID("/invalid/1.0.0")
		_, err := client.SendClosest(ctx, invalidProtocol, testMsg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "没有可用的服务器节点")
	})

	t.Run("SendClosest_timeout", func(t *testing.T) {
		// 创建一个新的测试专用服务器和客户端
		testHost, err := dep2p.New()
		assert.NoError(t, err)
		defer testHost.Close()

		testServer, err := NewServer(testHost)
		assert.NoError(t, err)
		defer testServer.Stop()

		// 使用更长的处理时间确保一定会超时
		err = testServer.Start(protocolID, func(req []byte) ([]byte, error) {
			time.Sleep(5 * time.Second)
			return []byte("response"), nil
		})
		assert.NoError(t, err)

		// 创建新的客户端
		testClientHost, err := dep2p.New()
		assert.NoError(t, err)
		defer testClientHost.Close()

		testClient, err := NewClient(testClientHost,
			WithMaxRetries(1),               // 设置最小重试次数
			WithConnectTimeout(time.Second), // 设置较短的连接超时
			WithReadTimeout(time.Second),    // 设置较短的读取超时
			WithWriteTimeout(time.Second),   // 设置较短的写入超时
		)
		assert.NoError(t, err)
		defer testClient.Close()

		// 等待确保服务器完全启动
		time.Sleep(100 * time.Millisecond)

		// 连接到测试服务器
		err = testClientHost.Connect(ctx, testHost.Peerstore().PeerInfo(testHost.ID()))
		assert.NoError(t, err)

		// 添加服务器节点
		err = testClient.AddServerNode(protocolID, testHost.ID())
		assert.NoError(t, err)

		// 等待确保连接建立
		time.Sleep(100 * time.Millisecond)

		// 使用短超时
		ctxTimeout, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		defer cancel()

		// 发送请求，应该超时
		_, err = testClient.SendClosest(ctxTimeout, protocolID, testMsg)
		assert.Error(t, err)
		assert.True(t, err == context.DeadlineExceeded ||
			strings.Contains(err.Error(), "context deadline exceeded"))
	})

	t.Run("SendClosest with multiple nodes", func(t *testing.T) {
		// 确保有多个节点可用
		for i := 0; i < numNodes; i++ {
			err = client.AddServerNode(protocolID, serverHosts[i].ID())
			assert.NoError(t, err)
		}

		// 发送请求
		result, err := client.SendClosest(ctx, protocolID, testMsg)
		assert.NoError(t, err)
		assert.Contains(t, string(result.Response), "来自节点")
		assert.Contains(t, string(result.Response), "test message")
	})

	t.Run("SendClosest with node failures", func(t *testing.T) {
		// 创建一些不可用的节点
		var failedNodes []peer.ID
		for i := 0; i < 3; i++ {
			fakeID, err := peer.Decode("QmYyQSo1c1Ym7orWxLYvCrM2EmxFTANf8wXmmE7DWjhx5N")
			assert.NoError(t, err)
			err = client.AddServerNode(protocolID, fakeID)
			assert.NoError(t, err)
			failedNodes = append(failedNodes, fakeID)
		}

		// 添加一个可用节点
		err = client.AddServerNode(protocolID, serverHosts[0].ID())
		assert.NoError(t, err)

		// 发送请求应该成功路由到可用节点，同时排除失败的节点
		result, err := client.SendClosest(ctx, protocolID, testMsg, failedNodes...)
		assert.NoError(t, err)
		assert.Contains(t, string(result.Response), "来自节点")
	})

	t.Run("SendClosest with all nodes failing", func(t *testing.T) {
		// 清除现有节点
		client.ClearServerNodes(protocolID)

		// 添加一些不可用的节点
		for i := 0; i < 3; i++ {
			fakeID, err := peer.Decode("QmYyQSo1c1Ym7orWxLYvCrM2EmxFTANf8wXmmE7DWjhx5N")
			assert.NoError(t, err)
			err = client.AddServerNode(protocolID, fakeID)
			assert.NoError(t, err)
		}

		// 发送请求应该失败
		_, err := client.SendClosest(ctx, protocolID, testMsg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "所有节点都发送失败")
	})

	t.Run("SendClosest with message size limit", func(t *testing.T) {
		// 添加一个可用节点
		err = client.AddServerNode(protocolID, serverHosts[0].ID())
		assert.NoError(t, err)

		// 创建一个超过大小限制的消息
		largeMsg := make([]byte, client.config.MaxBlockSize+1)

		// 发送请求应该失败
		_, err := client.SendClosest(ctx, protocolID, largeMsg)
		assert.Error(t, err)
		assert.Equal(t, ErrMessageTooLarge, err)
	})

	t.Run("SendClosest with context cancellation", func(t *testing.T) {
		// 添加一些节点
		for i := 0; i < numNodes; i++ {
			err = client.AddServerNode(protocolID, serverHosts[i].ID())
			assert.NoError(t, err)
		}

		// 创建一个已取消的上下文
		cancelCtx, cancel := context.WithCancel(ctx)
		cancel()

		// 发送请求应该返回上下文取消错误
		_, err := client.SendClosest(cancelCtx, protocolID, testMsg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "context canceled")
	})

	t.Run("Protocol node management", func(t *testing.T) {
		// 先清除所有现有节点
		client.ClearServerNodes(protocolID)

		// 测试添加节点
		err = client.AddServerNode(protocolID, serverHosts[0].ID())
		assert.NoError(t, err)

		// 测试重复添加相同节点
		err = client.AddServerNode(protocolID, serverHosts[0].ID())
		assert.NoError(t, err)

		// 测试添加自身节点
		err = client.AddServerNode(protocolID, clientHost.ID())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "不能添加自身作为服务器节点")

		// 测试获取节点列表
		nodes := client.GetServerNodes(protocolID)
		assert.Equal(t, 1, len(nodes), "节点列表长度应该为1")
		assert.Equal(t, serverHosts[0].ID().String(), nodes[0].String(), "节点ID应该匹配")

		// 测试移除节点
		err = client.RemoveServerNode(protocolID, serverHosts[0].ID())
		assert.NoError(t, err)

		// 测试移除不存在的节点
		err = client.RemoveServerNode(protocolID, serverHosts[0].ID())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "协议不存在")

		// 测试清除所有节点
		err = client.AddServerNode(protocolID, serverHosts[0].ID())
		assert.NoError(t, err)
		client.ClearServerNodes(protocolID)
		assert.Equal(t, 0, len(client.GetServerNodes(protocolID)))
	})

	// 在函数末尾添加清理代码
	defer func() {
		for _, server := range servers {
			server.Stop()
		}
	}()
}
