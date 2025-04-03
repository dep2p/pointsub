package pointsub_test

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 使用真实TCP连接测试监听器的基本功能
func TestRealTCPListener(t *testing.T) {
	// 创建TCP监听器
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("创建监听器失败: %v", err)
	}
	defer listener.Close()

	// 记录地址
	addr := listener.Addr().String()
	t.Logf("监听地址: %s", addr)

	// 通道用于同步和错误处理
	doneCh := make(chan struct{})
	errCh := make(chan error, 1)

	// 启动服务端监听
	go func() {
		// 接受连接
		conn, err := listener.Accept()
		if err != nil {
			errCh <- fmt.Errorf("接受连接失败: %w", err)
			return
		}
		defer conn.Close()

		// 读取数据
		buffer := make([]byte, 1024)
		n, err := conn.Read(buffer)
		if err != nil {
			errCh <- fmt.Errorf("读取数据失败: %w", err)
			return
		}

		// 发送回复
		_, err = conn.Write(buffer[:n])
		if err != nil {
			errCh <- fmt.Errorf("发送回复失败: %w", err)
			return
		}

		close(doneCh)
	}()

	// 短暂延迟，确保服务端已启动
	time.Sleep(100 * time.Millisecond)

	// 客户端连接
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("连接服务端失败: %v", err)
	}
	defer conn.Close()

	// 发送数据
	testData := []byte("hello listener")
	_, err = conn.Write(testData)
	if err != nil {
		t.Fatalf("发送数据失败: %v", err)
	}

	// 接收回复
	respBuffer := make([]byte, 1024)
	n, err := conn.Read(respBuffer)
	if err != nil {
		t.Fatalf("接收回复失败: %v", err)
	}

	// 验证回复
	if string(respBuffer[:n]) != string(testData) {
		t.Fatalf("回复不匹配: 预期 %q, 实际 %q", testData, respBuffer[:n])
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

// 测试同时处理多个连接
func TestRealTCPListenerMultipleConnections(t *testing.T) {
	// 创建TCP监听器
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("创建监听器失败: %v", err)
	}
	defer listener.Close()

	// 记录地址
	addr := listener.Addr().String()
	t.Logf("监听地址: %s", addr)

	// 连接计数
	connectionCount := 5

	// 并发控制
	var wg sync.WaitGroup

	// 使用通道接收和发送经过验证的数据
	type connMessage struct {
		id       int
		request  string
		response string
	}
	clientMessages := make(chan connMessage, connectionCount)
	serverMessages := make(chan connMessage, connectionCount)

	// 通道用于错误处理
	errCh := make(chan error, connectionCount*2)

	// 创建上下文用于取消
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 启动服务端监听
	for i := 0; i < connectionCount; i++ {
		wg.Add(1)
		go func(serverID int) {
			defer wg.Done()

			// 接受连接
			conn, err := listener.Accept()
			if err != nil {
				errCh <- fmt.Errorf("接受连接 %d 失败: %v", serverID, err)
				return
			}
			defer conn.Close()

			// 读取请求
			buffer := make([]byte, 1024)
			n, err := conn.Read(buffer)
			if err != nil {
				errCh <- fmt.Errorf("服务端 %d 读取失败: %v", serverID, err)
				return
			}

			// 解析客户端ID
			clientID := -1
			reqMsg := string(buffer[:n])
			_, err = fmt.Sscanf(reqMsg, "hello from client %d", &clientID)
			if err != nil || clientID < 0 || clientID >= connectionCount {
				errCh <- fmt.Errorf("服务端 %d 收到无效请求格式: %s", serverID, reqMsg)
				return
			}

			// 记录接收到的消息
			serverMessages <- connMessage{
				id:      clientID,
				request: reqMsg,
			}

			// 发送响应，确保包含客户端ID
			response := fmt.Sprintf("hello from server to client %d", clientID)
			_, err = conn.Write([]byte(response))
			if err != nil {
				errCh <- fmt.Errorf("服务端 %d 发送响应失败: %v", serverID, err)
				return
			}
		}(i)
	}

	// 短暂延迟，确保服务端准备好
	time.Sleep(100 * time.Millisecond)

	// 启动客户端连接
	for i := 0; i < connectionCount; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()

			// 连接到服务端
			conn, err := net.Dial("tcp", addr)
			if err != nil {
				errCh <- fmt.Errorf("客户端 %d 连接失败: %v", clientID, err)
				return
			}
			defer conn.Close()

			// 发送请求，包含客户端ID
			request := fmt.Sprintf("hello from client %d", clientID)
			_, err = conn.Write([]byte(request))
			if err != nil {
				errCh <- fmt.Errorf("客户端 %d 发送失败: %v", clientID, err)
				return
			}

			// 读取响应
			buffer := make([]byte, 1024)
			n, err := conn.Read(buffer)
			if err != nil {
				errCh <- fmt.Errorf("客户端 %d 接收失败: %v", clientID, err)
				return
			}

			// 记录接收到的响应
			response := string(buffer[:n])
			clientMessages <- connMessage{
				id:       clientID,
				response: response,
			}

			// 验证响应是否包含正确的客户端ID
			expectedResponse := fmt.Sprintf("hello from server to client %d", clientID)
			if response != expectedResponse {
				errCh <- fmt.Errorf("客户端 %d 收到错误响应: %q, 期望: %q",
					clientID, response, expectedResponse)
				return
			}
		}(i)
	}

	// 等待所有操作完成或出错
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// 所有操作完成
		close(clientMessages)
		close(serverMessages)

		// 检查是否有等待的错误
		select {
		case err := <-errCh:
			t.Fatalf("测试失败: %v", err)
		default:
			t.Logf("成功处理 %d 个连接", connectionCount)
		}
	case err := <-errCh:
		t.Fatalf("测试失败: %v", err)
	case <-ctx.Done():
		t.Fatalf("测试超时")
	}
}

// 测试监听器在高负载下的行为
func TestRealTCPListenerHighLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过高负载测试")
	}

	// 创建TCP监听器
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("创建监听器失败: %v", err)
	}
	defer listener.Close()

	// 记录地址
	addr := listener.Addr().String()
	t.Logf("监听地址: %s", addr)

	// 连接计数
	connectionCount := 20 // 减少连接数
	var successfulConnections int32

	// 通道用于同步和错误处理
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second) // 减少超时时间
	defer cancel()

	// 启动服务端接受连接
	go func() {
		for i := 0; ; i++ {
			select {
			case <-ctx.Done():
				return
			default:
				// 接受连接
				conn, err := listener.Accept()
				if err != nil {
					// 忽略上下文取消导致的错误
					if ctx.Err() != nil {
						return
					}
					continue
				}

				// 处理连接
				go func(c net.Conn, connID int) {
					defer c.Close()

					// 设置超时
					c.SetDeadline(time.Now().Add(5 * time.Second))

					// 读取数据
					buffer := make([]byte, 1024)
					n, err := c.Read(buffer)
					if err != nil {
						return
					}

					// 简单回复收到的数据
					_, err = c.Write(buffer[:n])
					if err != nil {
						return
					}

					// 增加成功处理计数
					atomic.AddInt32(&successfulConnections, 1)
				}(conn, i)
			}
		}
	}()

	// 短暂延迟，确保服务端已启动
	time.Sleep(100 * time.Millisecond)

	// 启动多个客户端连接
	var wg sync.WaitGroup
	wg.Add(connectionCount)

	for i := 0; i < connectionCount; i++ {
		go func(clientID int) {
			defer wg.Done()

			// 随机延迟，避免所有连接同时发起
			time.Sleep(time.Duration(clientID%20) * 10 * time.Millisecond)

			// 客户端连接
			conn, err := net.Dial("tcp", addr)
			if err != nil {
				return
			}
			defer conn.Close()

			// 设置超时
			conn.SetDeadline(time.Now().Add(5 * time.Second))

			// 发送数据
			message := fmt.Sprintf("high load test from client %d", clientID)
			_, err = conn.Write([]byte(message))
			if err != nil {
				return
			}

			// 接收回复
			buffer := make([]byte, 1024)
			n, err := conn.Read(buffer)
			if err != nil {
				return
			}

			// 简单验证回复长度
			if n != len(message) {
				return
			}
		}(i)
	}

	// 等待所有客户端完成
	wg.Wait()

	// 检查成功连接数
	successCount := atomic.LoadInt32(&successfulConnections)
	successRate := float64(successCount) / float64(connectionCount) * 100

	t.Logf("高负载测试结果: %d/%d 成功 (%.2f%%)",
		successCount, connectionCount, successRate)

	// 要求至少80%的连接成功处理
	if successRate < 80 {
		t.Errorf("高负载测试失败: 成功率 %.2f%%, 低于要求的 80%%", successRate)
	}
}
