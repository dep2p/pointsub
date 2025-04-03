package pointsub_test

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// 使用真实TCP连接的基础测试，不依赖libp2p模拟
func TestRealTCPConnBasic(t *testing.T) {
	// 启动监听器
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("无法创建监听器: %v", err)
	}
	defer listener.Close()

	// 记录监听地址
	addr := listener.Addr().String()
	t.Logf("监听器地址: %s", addr)

	// 通道用于同步和错误处理
	doneCh := make(chan struct{})
	errCh := make(chan error, 2)

	// 测试数据
	testData := []byte("hello, this is a tcp connection test")

	// 服务端Goroutine
	go func() {
		// 接受连接
		serverConn, err := listener.Accept()
		if err != nil {
			errCh <- fmt.Errorf("接受连接失败: %w", err)
			return
		}
		defer serverConn.Close()
		t.Logf("接受连接: %s -> %s", serverConn.RemoteAddr(), serverConn.LocalAddr())

		// 读取数据
		buffer := make([]byte, 1024)
		n, err := serverConn.Read(buffer)
		if err != nil {
			errCh <- fmt.Errorf("服务端读取失败: %w", err)
			return
		}

		// 验证数据
		if string(buffer[:n]) != string(testData) {
			errCh <- fmt.Errorf("数据不匹配: 预期 %q, 实际 %q", testData, buffer[:n])
			return
		}

		// 发送回复
		response := []byte("response from server")
		_, err = serverConn.Write(response)
		if err != nil {
			errCh <- fmt.Errorf("服务端写入失败: %w", err)
			return
		}

		close(doneCh)
	}()

	// 短暂延迟，确保服务端已启动
	time.Sleep(100 * time.Millisecond)

	// 客户端连接
	clientConn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("客户端连接失败: %v", err)
	}
	defer clientConn.Close()
	t.Logf("客户端连接: %s -> %s", clientConn.LocalAddr(), clientConn.RemoteAddr())

	// 发送数据
	_, err = clientConn.Write(testData)
	if err != nil {
		t.Fatalf("客户端写入失败: %v", err)
	}

	// 读取回复
	respBuffer := make([]byte, 1024)
	n, err := clientConn.Read(respBuffer)
	if err != nil {
		t.Fatalf("客户端读取失败: %v", err)
	}

	// 验证回复
	expectedResponse := "response from server"
	if string(respBuffer[:n]) != expectedResponse {
		t.Fatalf("回复不匹配: 预期 %q, 实际 %q", expectedResponse, respBuffer[:n])
	}

	// 等待服务端完成或出错
	select {
	case err := <-errCh:
		t.Fatalf("测试失败: %v", err)
	case <-doneCh:
		// 测试成功
	case <-time.After(2 * time.Second):
		t.Fatalf("测试超时")
	}
}

// 测试TCP连接的超时行为
func TestRealTCPConnTimeout(t *testing.T) {
	// 启动监听器
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("无法创建监听器: %v", err)
	}
	defer listener.Close()

	// 记录监听地址
	addr := listener.Addr().String()
	t.Logf("监听器地址: %s", addr)

	// 通道用于同步和错误处理
	timeoutDetectedCh := make(chan struct{})

	// 服务端Goroutine
	go func() {
		// 接受连接
		serverConn, err := listener.Accept()
		if err != nil {
			t.Errorf("接受连接失败: %v", err)
			return
		}
		defer serverConn.Close()

		// 设置读取超时
		timeout := 200 * time.Millisecond
		serverConn.SetReadDeadline(time.Now().Add(timeout))

		// 尝试读取 - 应该超时
		buffer := make([]byte, 10)
		_, err = serverConn.Read(buffer)

		// 验证是否为超时错误
		if err != nil && isTimeout(err) {
			t.Logf("正确检测到超时错误: %v", err)
			close(timeoutDetectedCh)
		} else if err != nil {
			t.Errorf("预期超时错误，但得到: %v", err)
		} else {
			t.Errorf("预期超时错误，但读取成功")
		}
	}()

	// 短暂延迟，确保服务端已启动
	time.Sleep(100 * time.Millisecond)

	// 客户端连接
	clientConn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("客户端连接失败: %v", err)
	}
	defer clientConn.Close()

	// 不发送任何数据，等待服务端超时

	// 等待超时检测或测试超时
	select {
	case <-timeoutDetectedCh:
		// 测试成功，正确检测到超时
	case <-time.After(1 * time.Second):
		t.Fatalf("未检测到超时或测试本身超时")
	}
}

// 测试大数据传输
func TestRealTCPConnLargeData(t *testing.T) {
	// 启动监听器
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("无法创建监听器: %v", err)
	}
	defer listener.Close()

	// 记录监听地址
	addr := listener.Addr().String()

	// 创建大数据(1MB)
	dataSize := 1 * 1024 * 1024
	largeData := make([]byte, dataSize)
	for i := 0; i < dataSize; i++ {
		largeData[i] = byte(i % 256)
	}

	// 通道用于同步和错误处理
	doneCh := make(chan struct{})
	errCh := make(chan error, 2)

	// 服务端Goroutine
	go func() {
		// 接受连接
		serverConn, err := listener.Accept()
		if err != nil {
			errCh <- fmt.Errorf("接受连接失败: %w", err)
			return
		}
		defer serverConn.Close()

		// 接收大数据
		receivedData := make([]byte, dataSize)
		totalRead := 0

		for totalRead < dataSize {
			n, err := serverConn.Read(receivedData[totalRead:])
			if err != nil {
				errCh <- fmt.Errorf("服务端读取失败: %w", err)
				return
			}
			totalRead += n
		}

		// 验证数据
		for i := 0; i < dataSize; i++ {
			if receivedData[i] != largeData[i] {
				errCh <- fmt.Errorf("数据不匹配，位置 %d: 预期 %d, 实际 %d",
					i, largeData[i], receivedData[i])
				return
			}
		}

		// 发送确认
		_, err = serverConn.Write([]byte("OK"))
		if err != nil {
			errCh <- fmt.Errorf("服务端写入确认失败: %w", err)
			return
		}

		close(doneCh)
	}()

	// 短暂延迟，确保服务端已启动
	time.Sleep(100 * time.Millisecond)

	// 客户端连接
	clientConn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("客户端连接失败: %v", err)
	}
	defer clientConn.Close()

	// 发送大数据
	written := 0
	for written < len(largeData) {
		n, err := clientConn.Write(largeData[written:])
		if err != nil {
			t.Fatalf("客户端写入失败: %v", err)
		}
		written += n
	}

	// 读取确认
	response := make([]byte, 2)
	n, err := clientConn.Read(response)
	if err != nil {
		t.Fatalf("客户端读取确认失败: %v", err)
	}

	if string(response[:n]) != "OK" {
		t.Fatalf("确认不匹配: 预期 'OK', 实际 %q", response[:n])
	}

	// 等待服务端完成或出错
	select {
	case err := <-errCh:
		t.Fatalf("测试失败: %v", err)
	case <-doneCh:
		// 测试成功
		t.Logf("成功传输 %d 字节数据", dataSize)
	case <-time.After(10 * time.Second):
		t.Fatalf("测试超时")
	}
}

// 测试多个连接
func TestRealTCPConnMultiple(t *testing.T) {
	// 创建一个监听器
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("创建监听器失败: %v", err)
	}
	defer listener.Close()

	// 获取监听器地址
	addr := listener.Addr().String()

	// 连接数量
	connectionCount := 4

	// 通信通道
	type connData struct {
		clientID int
		message  string
	}

	// 使用带ID的通道来保证连接响应的正确性
	receiveCh := make(chan connData, connectionCount)

	// 用于同步的等待组
	var wg sync.WaitGroup
	wg.Add(connectionCount)

	// 启动服务器
	go func() {
		for i := 0; i < connectionCount; i++ {
			// 接受新连接
			conn, err := listener.Accept()
			if err != nil {
				t.Logf("接受连接错误: %v", err)
				return
			}

			// 每个连接都在新的goroutine中处理
			go func(conn net.Conn, connIndex int) {
				defer conn.Close()

				// 读取客户端消息
				buffer := make([]byte, 1024)
				n, err := conn.Read(buffer)
				if err != nil {
					t.Logf("读取错误: %v", err)
					return
				}

				// 解析客户端ID，确保使用正确的ID返回
				clientID := -1
				fmt.Sscanf(string(buffer[:n]), "message from client %d", &clientID)

				if clientID == -1 {
					t.Logf("无法解析客户端ID，使用连接索引 %d", connIndex)
					clientID = connIndex
				}

				// 发送回复，包含客户端ID确保正确性
				response := fmt.Sprintf("response to client %d", clientID)
				_, err = conn.Write([]byte(response))
				if err != nil {
					t.Logf("写入错误: %v", err)
				}
			}(conn, i)
		}
	}()

	// 等待监听器完全启动
	time.Sleep(100 * time.Millisecond)

	// 启动多个客户端
	for i := 0; i < connectionCount; i++ {
		go func(clientID int) {
			defer wg.Done()

			// 连接到服务器
			conn, err := net.Dial("tcp", addr)
			if err != nil {
				t.Errorf("客户端 %d 连接失败: %v", clientID, err)
				return
			}
			defer conn.Close()

			// 发送带ID的消息，确保服务器可以正确识别
			message := fmt.Sprintf("message from client %d", clientID)
			_, err = conn.Write([]byte(message))
			if err != nil {
				t.Errorf("客户端 %d 发送失败: %v", clientID, err)
				return
			}

			// 读取响应
			buffer := make([]byte, 1024)
			n, err := conn.Read(buffer)
			if err != nil {
				t.Errorf("客户端 %d 接收失败: %v", clientID, err)
				return
			}

			// 将接收到的消息和客户端ID放入通道
			receiveCh <- connData{
				clientID: clientID,
				message:  string(buffer[:n]),
			}
		}(i)
	}

	// 等待所有客户端完成
	wg.Wait()
	close(receiveCh)

	// 验证所有客户端接收到的应答
	responseMap := make(map[int]string)
	errorCount := 0

	// 收集所有响应
	for data := range receiveCh {
		responseMap[data.clientID] = data.message
	}

	// 验证每个客户端是否收到正确的响应
	for clientID := 0; clientID < connectionCount; clientID++ {
		response, ok := responseMap[clientID]
		if !ok {
			t.Errorf("客户端 %d 没有收到响应", clientID)
			errorCount++
			continue
		}

		expectedResponse := fmt.Sprintf("response to client %d", clientID)
		if response != expectedResponse {
			t.Errorf("客户端 %d 收到非预期回复: %q, 预期: %q", clientID, response, expectedResponse)
			errorCount++
		}
	}

	if errorCount == 0 {
		t.Logf("所有 %d 个客户端均正确收发消息", connectionCount)
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
