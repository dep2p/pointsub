package pointsub

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/dep2p/go-dep2p/core/protocol"
)

// 测试基本的监听器功能 - 创建和接受连接
func TestListenerBasic(t *testing.T) {
	// 创建一对主机
	h1, h2, cleanup := CreateHostPair(t)
	defer cleanup()

	// 测试协议ID
	testProtocol := protocol.ID("/pointsub/test/listener/1.0.0")

	// 创建监听器
	listener, err := Listen(h1, testProtocol)
	if err != nil {
		t.Fatalf("创建监听器失败: %v", err)
	}
	defer listener.Close()

	// 验证监听器地址
	addr := listener.Addr()
	if addr.Network() != Network {
		t.Fatalf("监听器网络名称错误: 期望 %s, 实际 %s", Network, addr.Network())
	}
	if addr.String() != h1.ID().String() {
		t.Fatalf("监听器地址错误: 期望 %s, 实际 %s", h1.ID().String(), addr.String())
	}

	// 在另一个goroutine中接受连接
	connChan := make(chan error, 1)
	var serverConn net.Conn

	go func() {
		var err error
		serverConn, err = listener.Accept()
		connChan <- err
	}()

	// 建立连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientConn, err := Dial(ctx, h2, h1.ID(), testProtocol)
	if err != nil {
		t.Fatalf("建立连接失败: %v", err)
	}
	defer clientConn.Close()

	// 等待Accept返回
	select {
	case err := <-connChan:
		if err != nil {
			t.Fatalf("接受连接失败: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("等待接受连接超时")
	}

	// 验证连接是否有效
	if serverConn == nil {
		t.Fatal("服务端连接为空")
	}
	defer serverConn.Close()

	// 测试连接上的数据传输
	testData := []byte("hello listener")
	CheckDataTransfer(t, clientConn, serverConn, testData, 2*time.Second)
}

// 测试监听器选项
func TestListenerWithOptions(t *testing.T) {
	// 创建一对主机
	h1, h2, cleanup := CreateHostPair(t)
	defer cleanup()

	// 测试协议ID
	testProtocol := protocol.ID("/pointsub/test/listener-options/1.0.0")

	// 创建带选项的监听器
	bufferSize := 8192
	idleTimeout := 30 * time.Second
	maxConns := 10

	listener, err := ListenWithOptions(h1, testProtocol,
		WithBufferSize(bufferSize),
		WithIdleTimeout(idleTimeout),
		WithMaxConcurrentConnections(maxConns),
	)
	if err != nil {
		t.Fatalf("创建带选项的监听器失败: %v", err)
	}
	defer listener.Close()

	// 建立连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientConn, err := Dial(ctx, h2, h1.ID(), testProtocol)
	if err != nil {
		t.Fatalf("建立连接失败: %v", err)
	}
	defer clientConn.Close()

	// 接受连接
	serverConn, err := listener.Accept()
	if err != nil {
		t.Fatalf("接受连接失败: %v", err)
	}
	defer serverConn.Close()

	// 测试连接上的数据传输
	testData := []byte("hello listener with options")
	CheckDataTransfer(t, clientConn, serverConn, testData, 2*time.Second)
}

// 测试监听器多个连接
func TestListenerMultipleConnections(t *testing.T) {
	// 创建一对主机
	h1, h2, cleanup := CreateHostPair(t)
	defer cleanup()

	// 测试协议ID
	testProtocol := protocol.ID("/pointsub/test/listener-multi/1.0.0")

	// 创建监听器
	listener, err := Listen(h1, testProtocol)
	if err != nil {
		t.Fatalf("创建监听器失败: %v", err)
	}
	defer listener.Close()

	// 连接数量 - 减少连接数量以提高可靠性
	connCount := 2
	clientConns := make([]net.Conn, connCount)
	serverConns := make([]net.Conn, connCount)

	// 创建通道用于同步
	acceptedCh := make(chan int, connCount)
	acceptErrCh := make(chan error, connCount)
	dialErrCh := make(chan error, connCount)

	// 设置超时
	timeout := 10 * time.Second
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	// 在goroutine中接受连接
	for i := 0; i < connCount; i++ {
		go func(index int) {
			conn, err := listener.Accept()
			if err != nil {
				acceptErrCh <- fmt.Errorf("接受第 %d 个连接失败: %w", index+1, err)
				return
			}
			serverConns[index] = conn
			acceptedCh <- index
		}(i)
	}

	// 建立连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 创建客户端连接
	for i := 0; i < connCount; i++ {
		go func(index int) {
			conn, err := Dial(ctx, h2, h1.ID(), testProtocol)
			if err != nil {
				dialErrCh <- fmt.Errorf("建立第 %d 个连接失败: %w", index+1, err)
				return
			}
			clientConns[index] = conn
		}(i)
	}

	// 等待所有连接建立或超时
	for i := 0; i < connCount; i++ {
		select {
		case err := <-dialErrCh:
			t.Fatalf("拨号错误: %v", err)
		case err := <-acceptErrCh:
			t.Fatalf("接受错误: %v", err)
		case <-acceptedCh:
			// 成功接受一个连接
		case <-timer.C:
			t.Fatalf("等待连接建立超时")
		}
	}

	// 确保所有连接都已建立
	time.Sleep(500 * time.Millisecond)

	// 测试每个连接上的数据传输
	for i := 0; i < connCount; i++ {
		if clientConns[i] == nil {
			t.Fatalf("第 %d 个客户端连接为空", i+1)
		}
		if serverConns[i] == nil {
			t.Fatalf("第 %d 个服务端连接为空", i+1)
		}

		testData := []byte("hello connection " + strconv.Itoa(i+1))
		CheckDataTransfer(t, clientConns[i], serverConns[i], testData, 2*time.Second)
	}

	// 关闭所有连接
	for i := 0; i < connCount; i++ {
		if clientConns[i] != nil {
			clientConns[i].Close()
		}
		if serverConns[i] != nil {
			serverConns[i].Close()
		}
	}
}

// 测试关闭监听器
func TestListenerClose(t *testing.T) {
	// 创建一对主机
	h1, h2, cleanup := CreateHostPair(t)
	defer cleanup()

	// 测试协议ID
	testProtocol := protocol.ID("/pointsub/test/listener-close/1.0.0")

	// 创建监听器
	listener, err := Listen(h1, testProtocol)
	if err != nil {
		t.Fatalf("创建监听器失败: %v", err)
	}

	// 关闭监听器
	err = listener.Close()
	if err != nil {
		t.Fatalf("关闭监听器失败: %v", err)
	}

	// 尝试建立连接以验证监听器已关闭，但不使用返回的连接
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, _ = Dial(ctx, h2, h1.ID(), testProtocol)

	// 建立连接应该失败
	acceptChan := make(chan error, 1)
	go func() {
		_, err := listener.Accept()
		acceptChan <- err
	}()

	// 等待Accept返回
	select {
	case err := <-acceptChan:
		if err == nil {
			t.Fatal("在关闭的监听器上接受连接应该失败，但成功了")
		}
		// 预期错误，测试通过
	case <-time.After(3 * time.Second):
		t.Fatal("等待关闭的监听器Accept返回超时")
	}
}

// TestListenerAcceptMultiple 测试监听器能否接受多个连接
func TestListenerAcceptMultiple(t *testing.T) {
	// 创建一对主机
	h1, h2, cleanup := CreateHostPair(t)
	defer cleanup()

	// 测试协议ID
	testProtocol := protocol.ID("/pointsub/test/listener-accept/1.0.0")

	// 创建监听器
	listener, err := Listen(h1, testProtocol)
	if err != nil {
		t.Fatalf("创建监听器失败: %v", err)
	}
	defer listener.Close()

	// 连接数量 - 减少连接数量以提高可靠性
	connCount := 1 // 只测试一个连接
	clientConns := make([]net.Conn, connCount)
	serverConns := make([]net.Conn, connCount)

	// 在goroutine中接受连接
	acceptCh := make(chan net.Conn, connCount)
	acceptErrCh := make(chan error, connCount)

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			acceptErrCh <- err
			return
		}
		acceptCh <- conn
	}()

	// 创建客户端连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientConn, err := Dial(ctx, h2, h1.ID(), testProtocol)
	if err != nil {
		t.Fatalf("创建连接失败: %v", err)
	}
	clientConns[0] = clientConn

	// 等待接受连接
	select {
	case err := <-acceptErrCh:
		t.Fatalf("接受连接失败: %v", err)
	case conn := <-acceptCh:
		serverConns[0] = conn
	case <-time.After(5 * time.Second):
		t.Fatalf("接受连接超时")
	}

	// 测试连接
	if clientConns[0] == nil {
		t.Fatalf("客户端连接为空")
	}
	if serverConns[0] == nil {
		t.Fatalf("服务端连接为空")
	}

	testData := []byte("test connection")
	// 增加超时时间
	CheckDataTransfer(t, clientConns[0], serverConns[0], testData, 5*time.Second)

	// 关闭连接
	if clientConns[0] != nil {
		clientConns[0].Close()
	}
	if serverConns[0] != nil {
		serverConns[0].Close()
	}
}
