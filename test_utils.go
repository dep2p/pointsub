package pointsub

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"time"

	"github.com/dep2p/go-dep2p"
	"github.com/dep2p/go-dep2p/core/host"
	"github.com/dep2p/go-dep2p/core/peer"
	"github.com/dep2p/go-dep2p/core/peerstore"
	"github.com/dep2p/go-dep2p/core/protocol"
	"github.com/dep2p/go-dep2p/multiformats/multiaddr"
)

// testRnd 是测试专用的随机数生成器
var testRnd = rand.New(rand.NewSource(time.Now().UnixNano()))

// TestingT 是testing.TB的子集，用于我们的测试函数和性能测试
type TestingT interface {
	Logf(format string, args ...interface{})
	Fatalf(format string, args ...interface{})
}

// CreateTestHost 创建一个用于测试的dep2p主机
func CreateTestHost(t TestingT, port int) host.Host {
	// 创建本地主机的多地址
	addr, err := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", port))
	if err != nil {
		t.Fatalf("创建测试多地址失败: %v", err)
	}

	// 创建dep2p主机，使用更简单的配置
	h, err := dep2p.New(
		dep2p.ListenAddrs(addr),
		dep2p.NATPortMap(),
	)
	if err != nil {
		t.Fatalf("创建测试dep2p主机失败: %v", err)
	}

	// 输出主机信息用于调试
	t.Logf("创建测试主机: ID=%s, 地址=%v", h.ID().String(), h.Addrs())

	return h
}

// CreateHostPair 创建一对相互连接的dep2p主机
func CreateHostPair(t TestingT) (host.Host, host.Host, func()) {
	// 随机选择端口，避免端口冲突
	port1 := 10000 + testRnd.Intn(10000)
	port2 := port1 + 1000 + testRnd.Intn(1000)

	// 创建主机
	h1 := CreateTestHost(t, port1)
	h2 := CreateTestHost(t, port2)

	// 相互添加对方的Peer信息
	h1.Peerstore().AddAddrs(h2.ID(), h2.Addrs(), peerstore.PermanentAddrTTL)
	h2.Peerstore().AddAddrs(h1.ID(), h1.Addrs(), peerstore.PermanentAddrTTL)

	// 尝试建立连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 尝试从h2连接到h1
	if err := h2.Connect(ctx, peer.AddrInfo{
		ID:    h1.ID(),
		Addrs: h1.Addrs(),
	}); err != nil {
		t.Logf("主机连接尝试失败，但这可能不是问题: %v", err)
	}

	// 返回清理函数
	cleanup := func() {
		// 关闭所有连接
		for _, conn := range h1.Network().Conns() {
			conn.Close()
		}
		for _, conn := range h2.Network().Conns() {
			conn.Close()
		}

		// 关闭主机
		h1.Close()
		h2.Close()
	}

	return h1, h2, cleanup
}

// CreateConnectedPair 创建一对已连接的主机并返回连接
func CreateConnectedPair(t TestingT, protocolID protocol.ID) (host.Host, host.Host, net.Conn, net.Conn, func()) {
	// 创建主机对
	h1, h2, hostCleanup := CreateHostPair(t)

	// 确保主机间可以连接
	if !WaitForConnection(t, h1, h2, 5*time.Second) {
		t.Logf("警告: 主机间连接未建立，尝试强制连接")
		// 尝试强制连接
		if err := h2.Connect(context.Background(), peer.AddrInfo{
			ID:    h1.ID(),
			Addrs: h1.Addrs(),
		}); err != nil {
			t.Logf("强制连接失败: %v", err)
		}

		// 再次等待连接
		if !WaitForConnection(t, h1, h2, 5*time.Second) {
			t.Fatalf("主机间连接建立失败")
		}
	}

	// 创建通道用于同步
	ready := make(chan struct{})
	listenerErrCh := make(chan error, 1)

	// 创建监听器
	var listener net.Listener

	// 服务端监听
	go func() {
		var err error
		listener, err = Listen(h1, protocolID)
		if err != nil {
			listenerErrCh <- err
			return
		}
		close(ready)
	}()

	// 等待服务端就绪或出错
	select {
	case err := <-listenerErrCh:
		hostCleanup()
		t.Fatalf("服务端监听失败: %v", err)
	case <-ready:
		// 监听器创建成功
	case <-time.After(5 * time.Second):
		hostCleanup()
		t.Fatalf("等待监听器就绪超时")
	}

	// 客户端连接
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var clientConn net.Conn
	dialErrCh := make(chan error, 1)
	dialDoneCh := make(chan struct{})

	go func() {
		var dialErr error
		clientConn, dialErr = Dial(ctx, h2, h1.ID(), protocolID)
		if dialErr != nil {
			dialErrCh <- dialErr
			return
		}
		close(dialDoneCh)
	}()

	// 服务端接受连接
	acceptErrCh := make(chan error, 1)
	acceptDoneCh := make(chan struct{})
	var serverConn net.Conn

	go func() {
		var acceptErr error
		serverConn, acceptErr = listener.Accept()
		if acceptErr != nil {
			acceptErrCh <- acceptErr
			return
		}
		close(acceptDoneCh)
	}()

	// 等待连接建立或出错
	select {
	case err := <-dialErrCh:
		if listener != nil {
			listener.Close()
		}
		hostCleanup()
		t.Fatalf("客户端连接失败: %v", err)
	case err := <-acceptErrCh:
		if listener != nil {
			listener.Close()
		}
		hostCleanup()
		t.Fatalf("服务端接受连接失败: %v", err)
	case <-dialDoneCh:
		// 客户端连接成功
	case <-time.After(10 * time.Second):
		if listener != nil {
			listener.Close()
		}
		hostCleanup()
		t.Fatalf("等待连接建立超时")
	}

	// 等待服务端接受连接
	select {
	case err := <-acceptErrCh:
		if listener != nil {
			listener.Close()
		}
		if clientConn != nil {
			clientConn.Close()
		}
		hostCleanup()
		t.Fatalf("服务端接受连接失败: %v", err)
	case <-acceptDoneCh:
		// 服务端接受连接成功
	case <-time.After(5 * time.Second):
		if listener != nil {
			listener.Close()
		}
		if clientConn != nil {
			clientConn.Close()
		}
		hostCleanup()
		t.Fatalf("等待服务端接受连接超时")
	}

	// 验证连接是否有效
	if clientConn == nil || serverConn == nil {
		if listener != nil {
			listener.Close()
		}
		if clientConn != nil {
			clientConn.Close()
		}
		if serverConn != nil {
			serverConn.Close()
		}
		hostCleanup()
		t.Fatalf("连接建立失败: clientConn=%v, serverConn=%v", clientConn != nil, serverConn != nil)
	}

	cleanup := func() {
		if serverConn != nil {
			serverConn.Close()
		}
		if clientConn != nil {
			clientConn.Close()
		}
		if listener != nil {
			listener.Close()
		}
		hostCleanup()
	}

	return h1, h2, serverConn, clientConn, cleanup
}

// CreateMemoryConnPair 创建一对内存连接，用于不需要网络的测试
func CreateMemoryConnPair(t TestingT) (net.Conn, net.Conn) {
	// 创建内存管道
	client, server := net.Pipe()

	// 记录日志
	t.Logf("创建内存连接对")

	return client, server
}

// CreateMemoryTransporterPair 创建一对内存连接，并直接返回已包装的MessageTransporter接口
// 这比先创建net.Conn再单独包装更方便
func CreateMemoryTransporterPair(t TestingT) (MessageTransporter, MessageTransporter) {
	// 创建内存管道
	client, server := net.Pipe()

	// 记录日志
	t.Logf("创建内存传输器对")

	// 包装为MessageTransporter
	clientTransporter := NewMessageTransporter(client)
	serverTransporter := NewMessageTransporter(server)

	return clientTransporter, serverTransporter
}

// GenerateRandomData 生成指定大小的随机数据
func GenerateRandomData(size int) []byte {
	data := make([]byte, size)
	testRnd.Read(data)
	return data
}

// WaitForConnection 等待两个主机之间建立连接
func WaitForConnection(t TestingT, h1 host.Host, h2 host.Host, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			if len(h1.Network().ConnsToPeer(h2.ID())) > 0 &&
				len(h2.Network().ConnsToPeer(h1.ID())) > 0 {
				return true
			}
		}
	}
}

// CheckDataTransfer 检查数据传输是否正确
func CheckDataTransfer(t TestingT, sender, receiver net.Conn, data []byte, timeout time.Duration) {
	// 创建通道用于同步和错误处理
	doneCh := make(chan struct{})
	errCh := make(chan error, 2)
	receivedCh := make(chan []byte, 1)

	// 设置写入超时
	if err := sender.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		t.Fatalf("设置写入超时失败: %v", err)
	}

	// 发送数据
	go func() {
		n, err := sender.Write(data)
		// 重置写入超时
		if resetErr := sender.SetWriteDeadline(time.Time{}); resetErr != nil {
			errCh <- fmt.Errorf("清除写入超时失败: %w", resetErr)
			return
		}

		if err != nil {
			errCh <- fmt.Errorf("发送数据失败: %w", err)
			return
		}
		if n != len(data) {
			errCh <- fmt.Errorf("发送数据不完整: 预期 %d 字节, 实际发送 %d 字节", len(data), n)
			return
		}
		close(doneCh)
	}()

	// 设置接收超时
	if err := receiver.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		t.Fatalf("设置读取超时失败: %v", err)
	}

	// 接收数据
	go func() {
		received := make([]byte, len(data))
		totalRead := 0

		// 循环读取，确保接收完整数据
		for totalRead < len(data) {
			n, err := receiver.Read(received[totalRead:])
			if err != nil {
				errCh <- fmt.Errorf("接收数据失败: %w", err)
				return
			}
			totalRead += n

			// 如果读取了部分数据但未完成，重置超时
			if totalRead < len(data) {
				if err := receiver.SetReadDeadline(time.Now().Add(timeout)); err != nil {
					errCh <- fmt.Errorf("重置读取超时失败: %w", err)
					return
				}
			}
		}

		// 重置超时
		if err := receiver.SetReadDeadline(time.Time{}); err != nil {
			errCh <- fmt.Errorf("清除读取超时失败: %w", err)
			return
		}

		receivedCh <- received[:totalRead]
	}()

	// 等待操作完成或超时
	timer := time.NewTimer(timeout * 2)
	defer timer.Stop()

	select {
	case err := <-errCh:
		t.Fatalf("%v", err)
	case received := <-receivedCh:
		// 验证数据长度
		if len(received) != len(data) {
			t.Fatalf("接收数据不完整，预期 %d 字节，实际接收 %d 字节", len(data), len(received))
		}

		// 验证数据内容
		for i := 0; i < len(received); i++ {
			if received[i] != data[i] {
				t.Fatalf("数据不匹配: 位置 %d, 预期 %d, 实际 %d", i, data[i], received[i])
			}
		}

		// 等待发送完成
		select {
		case <-doneCh:
			// 发送成功完成
		case err := <-errCh:
			t.Fatalf("发送过程中出错: %v", err)
		case <-timer.C:
			t.Fatalf("等待发送完成超时")
		}
	case <-timer.C:
		t.Fatalf("数据传输超时")
	}
}

// CreateDep2pTransporterPair 创建一对基于dep2p的连接，并直接返回包装好的MessageTransporter接口
// 这是为示例和测试提供的便捷方法
func CreateDep2pTransporterPair(t TestingT) (MessageTransporter, MessageTransporter, func()) {
	// 生成随机协议ID以避免冲突
	protocolID := protocol.ID(fmt.Sprintf("/pointsub/test/%d", testRnd.Intn(1000)))

	// 记录日志
	t.Logf("创建dep2p传输器对，协议: %s", protocolID)

	// 创建连接对
	h1, h2, serverConn, clientConn, cleanup := CreateConnectedPair(t, protocolID)

	// 包装为MessageTransporter
	senderTransporter := NewMessageTransporter(clientConn)
	receiverTransporter := NewMessageTransporter(serverConn)

	// 创建增强的清理函数
	enhancedCleanup := func() {
		t.Logf("关闭dep2p传输器对 (主机: %s, %s)", h1.ID().String(), h2.ID().String())
		cleanup()
	}

	return senderTransporter, receiverTransporter, enhancedCleanup
}
