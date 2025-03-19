package pointsub

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dep2p/go-dep2p/core/protocol"
)

// 测试连接的建立、基本读写和关闭功能
func TestConnBasic(t *testing.T) {
	// 设置测试协议
	testProtocol := protocol.ID("/pointsub/test/conn/1.0.0")

	// 创建一对已连接的主机
	h1, h2, serverConn, clientConn, cleanup := CreateConnectedPair(t, testProtocol)
	defer cleanup()

	// 测试连接是否成功建立
	if h1.ID() == h2.ID() {
		t.Fatal("两个测试主机ID相同，应该不同")
	}

	if serverConn == nil || clientConn == nil {
		t.Fatal("连接为空")
	}

	// 测试基本数据传输 - 从客户端到服务端
	testData := []byte("hello from client")
	CheckDataTransfer(t, clientConn, serverConn, testData, 2*time.Second)

	// 测试基本数据传输 - 从服务端到客户端
	testData = []byte("hello from server")
	CheckDataTransfer(t, serverConn, clientConn, testData, 2*time.Second)
}

// 测试连接超时
func TestConnTimeout(t *testing.T) {
	// 设置测试协议
	testProtocol := protocol.ID("/pointsub/test/conn/timeout/1.0.0")

	// 创建一对已连接的主机
	_, _, serverConn, clientConn, cleanup := CreateConnectedPair(t, testProtocol)
	defer cleanup()

	// 设置短超时
	timeout := 100 * time.Millisecond
	serverConn.SetReadDeadline(time.Now().Add(timeout))

	// 尝试读取数据，应该超时
	buf := make([]byte, 1024)
	_, err := serverConn.Read(buf)
	if err == nil {
		t.Fatal("期望超时错误，但没有发生")
	}

	// 确认是超时错误
	if !isTimeout(err) {
		t.Fatalf("期望超时错误，但得到: %v", err)
	}

	// 重置超时，确保连接仍能正常工作
	serverConn.SetReadDeadline(time.Time{})

	// 验证连接在超时后仍能正常传输数据
	testData := []byte("after timeout")
	CheckDataTransfer(t, clientConn, serverConn, testData, 2*time.Second)
}

// 测试大数据传输
func TestConnLargeData(t *testing.T) {
	// 设置测试协议
	testProtocol := protocol.ID("/pointsub/test/conn/large/1.0.0")

	// 创建一对已连接的主机
	_, _, serverConn, clientConn, cleanup := CreateConnectedPair(t, testProtocol)
	defer cleanup()

	// 生成1MB的随机数据
	dataSize := 1 * 1024 * 1024
	largeData := GenerateRandomData(dataSize)

	// 传输大数据 - 客户端到服务端
	doneCh := make(chan struct{})

	go func() {
		n, err := clientConn.Write(largeData)
		if err != nil {
			t.Errorf("写入大数据失败: %v", err)
		}
		if n != dataSize {
			t.Errorf("写入不完整: 预期 %d 字节, 实际 %d 字节", dataSize, n)
		}
		close(doneCh)
	}()

	// 接收大数据
	receivedData := make([]byte, dataSize)
	totalRead := 0

	for totalRead < dataSize {
		n, err := serverConn.Read(receivedData[totalRead:])
		if err != nil {
			t.Fatalf("读取大数据失败: %v", err)
		}
		totalRead += n
	}

	// 验证接收到的数据是否完整
	if totalRead != dataSize {
		t.Fatalf("接收数据不完整: 预期 %d 字节, 实际 %d 字节", dataSize, totalRead)
	}

	// 检查数据内容是否正确
	for i := 0; i < dataSize; i++ {
		if receivedData[i] != largeData[i] {
			t.Fatalf("数据不匹配: 位置 %d", i)
		}
	}

	// 等待发送完成
	<-doneCh
}

// 测试多路复用 - 同一对主机间的多个连接
func TestConnMultiplex(t *testing.T) {
	// 设置两个不同的测试协议
	testProtocol1 := protocol.ID("/pointsub/test/conn/multiplex/1.0.0")
	testProtocol2 := protocol.ID("/pointsub/test/conn/multiplex/2.0.0")

	// 创建一对主机
	h1, h2, cleanup := CreateHostPair(t)
	defer cleanup()

	// 为第一个协议创建监听器
	listener1, err := Listen(h1, testProtocol1)
	if err != nil {
		t.Fatalf("创建第一个监听器失败: %v", err)
	}
	defer listener1.Close()

	// 为第二个协议创建监听器
	listener2, err := Listen(h1, testProtocol2)
	if err != nil {
		t.Fatalf("创建第二个监听器失败: %v", err)
	}
	defer listener2.Close()

	// 客户端连接到第一个协议
	ctx1, cancel1 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel1()

	conn1, err := Dial(ctx1, h2, h1.ID(), testProtocol1)
	if err != nil {
		t.Fatalf("连接到第一个协议失败: %v", err)
	}
	defer conn1.Close()

	// 服务端接受第一个连接
	serverConn1, err := listener1.Accept()
	if err != nil {
		t.Fatalf("接受第一个连接失败: %v", err)
	}
	defer serverConn1.Close()

	// 客户端连接到第二个协议
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()

	conn2, err := Dial(ctx2, h2, h1.ID(), testProtocol2)
	if err != nil {
		t.Fatalf("连接到第二个协议失败: %v", err)
	}
	defer conn2.Close()

	// 服务端接受第二个连接
	serverConn2, err := listener2.Accept()
	if err != nil {
		t.Fatalf("接受第二个连接失败: %v", err)
	}
	defer serverConn2.Close()

	// 在第一个连接上传输数据
	data1 := []byte("data for protocol 1")
	CheckDataTransfer(t, conn1, serverConn1, data1, 2*time.Second)

	// 在第二个连接上传输数据
	data2 := []byte("data for protocol 2")
	CheckDataTransfer(t, conn2, serverConn2, data2, 2*time.Second)
}

// 测试连接关闭处理
func TestConnClose(t *testing.T) {
	// 设置测试协议
	testProtocol := protocol.ID("/pointsub/test/conn/close/1.0.0")

	// 创建一对已连接的主机
	_, _, serverConn, clientConn, cleanup := CreateConnectedPair(t, testProtocol)
	defer cleanup()

	// 测试客户端连接关闭
	if err := clientConn.Close(); err != nil {
		t.Fatalf("关闭客户端连接失败: %v", err)
	}

	// 服务端应该能检测到连接已关闭
	buf := make([]byte, 10)
	_, err := serverConn.Read(buf)
	if err == nil {
		t.Fatal("预期连接关闭错误，但读取成功")
	}
}

// isTimeout 判断错误是否为超时错误
func isTimeout(err error) bool {
	if err == nil {
		return false
	}

	// 尝试使用标准接口检测超时
	if netErr, ok := err.(interface{ Timeout() bool }); ok {
		return netErr.Timeout()
	}

	// 检查错误字符串中是否包含超时相关关键词
	errStr := err.Error()
	timeoutPatterns := []string{
		"timeout",
		"timed out",
		"deadline exceeded",
		"i/o timeout",
	}

	for _, pattern := range timeoutPatterns {
		if strings.Contains(strings.ToLower(errStr), pattern) {
			return true
		}
	}

	return false
}
