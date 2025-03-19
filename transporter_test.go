package pointsub

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/dep2p/go-dep2p/core/protocol"
)

// 测试消息发送和接收的基本功能
func TestTransporterBasic(t *testing.T) {
	// 创建内存连接对，不使用实际网络
	clientConn, serverConn := CreateMemoryConnPair(t)

	// 创建消息发送器和接收器
	sender := NewTestMessageTransporter(clientConn)
	receiver := NewTestMessageTransporter(serverConn)

	// 测试发送小消息
	smallMsg := []byte("hello world")

	// 创建通道用于同步和错误处理
	doneCh := make(chan struct{})
	errCh := make(chan error, 2)
	receivedCh := make(chan []byte, 1)

	// 在goroutine中接收消息
	go func() {
		receivedMsg, err := receiver.Receive()
		if err != nil {
			errCh <- err
			return
		}
		receivedCh <- receivedMsg
	}()

	// 在goroutine中发送消息
	go func() {
		err := sender.Send(smallMsg)
		if err != nil {
			errCh <- err
			return
		}
		close(doneCh)
	}()

	// 等待操作完成或出错
	select {
	case err := <-errCh:
		t.Fatalf("操作失败: %v", err)
	case receivedMsg := <-receivedCh:
		// 验证消息内容
		if !bytes.Equal(smallMsg, receivedMsg) {
			t.Fatalf("消息内容不匹配: 发送 %v, 接收 %v", smallMsg, receivedMsg)
		}

		// 等待发送完成
		select {
		case <-doneCh:
			// 发送成功完成
		case err := <-errCh:
			t.Fatalf("发送过程中出错: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatalf("等待发送完成超时")
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("操作超时")
	}
}

// 测试发送和接收空消息
func TestTransporterEmptyMessage(t *testing.T) {
	// 创建内存连接对
	clientConn, serverConn := CreateMemoryConnPair(t)
	defer clientConn.Close()
	defer serverConn.Close()

	// 创建消息发送器和接收器
	sender := NewTestMessageTransporter(clientConn)
	receiver := NewTestMessageTransporter(serverConn)

	// 发送空消息
	emptyMsg := []byte{}

	// 创建通道用于同步和错误处理
	done := make(chan struct{})

	// 在goroutine中接收消息
	go func() {
		defer close(done)
		receivedMsg, err := receiver.Receive()
		if err != nil {
			t.Errorf("接收操作失败: %v", err)
			return
		}

		// 验证消息内容
		if len(receivedMsg) != 0 {
			t.Errorf("接收到的空消息不为空: %v", receivedMsg)
			return
		}
	}()

	// 短暂延迟，确保接收方已经准备好
	time.Sleep(100 * time.Millisecond)

	// 发送消息
	err := sender.Send(emptyMsg)
	if err != nil {
		t.Fatalf("发送空消息失败: %v", err)
	}

	// 等待接收操作完成
	select {
	case <-done:
		// 接收操作成功完成
	case <-time.After(5 * time.Second):
		t.Fatalf("接收操作超时")
	}
}

// 测试发送和接收大消息
func TestTransporterLargeMessage(t *testing.T) {
	// 创建内存连接对
	clientConn, serverConn := CreateMemoryConnPair(t)

	// 创建消息发送器和接收器
	sender := NewTestMessageTransporter(clientConn)
	receiver := NewTestMessageTransporter(serverConn)

	// 创建1MB的测试消息
	dataSize := 1 * 1024 * 1024
	largeMsg := GenerateRandomData(dataSize)

	// 创建通道用于同步和错误处理
	doneCh := make(chan struct{})
	errCh := make(chan error, 2)
	receivedCh := make(chan []byte, 1)

	// 在goroutine中接收消息
	go func() {
		receivedMsg, err := receiver.Receive()
		if err != nil {
			errCh <- err
			return
		}
		receivedCh <- receivedMsg
	}()

	// 在goroutine中发送消息
	go func() {
		err := sender.Send(largeMsg)
		if err != nil {
			errCh <- err
			return
		}
		close(doneCh)
	}()

	// 等待操作完成或出错
	select {
	case err := <-errCh:
		t.Fatalf("操作失败: %v", err)
	case receivedMsg := <-receivedCh:
		// 验证消息大小
		if len(receivedMsg) != dataSize {
			t.Fatalf("消息大小不匹配: 预期 %d 字节, 实际 %d 字节", dataSize, len(receivedMsg))
		}

		// 验证消息内容
		if !bytes.Equal(largeMsg, receivedMsg) {
			t.Fatal("大消息内容不匹配")
		}

		// 等待发送完成
		select {
		case <-doneCh:
			// 发送成功完成
		case err := <-errCh:
			t.Fatalf("发送过程中出错: %v", err)
		case <-time.After(10 * time.Second): // 大消息需要更长的超时时间
			t.Fatalf("等待发送完成超时")
		}
	case <-time.After(10 * time.Second): // 大消息需要更长的超时时间
		t.Fatalf("操作超时")
	}
}

// 测试连续发送多个消息
func TestTransporterMultipleMessages(t *testing.T) {
	// 创建内存连接对
	clientConn, serverConn := CreateMemoryConnPair(t)

	// 创建消息发送器和接收器
	sender := NewTestMessageTransporter(clientConn)
	receiver := NewTestMessageTransporter(serverConn)

	// 准备多个测试消息
	messages := [][]byte{
		[]byte("message 1"),
		[]byte("message 2"),
		[]byte("message 3"),
		GenerateRandomData(10 * 1024), // 10KB消息
		[]byte("message 5"),
	}

	// 创建通道用于同步和错误处理
	sendDoneCh := make(chan struct{})
	sendErrCh := make(chan error, 1)
	recvDoneCh := make(chan struct{})
	recvErrCh := make(chan error, 1)
	receivedMessages := make([][]byte, 0, len(messages))

	// 在goroutine中发送所有消息
	go func() {
		for i, msg := range messages {
			err := sender.Send(msg)
			if err != nil {
				sendErrCh <- fmt.Errorf("发送消息 %d 失败: %w", i+1, err)
				return
			}
			// 短暂延迟，确保接收方有时间处理
			time.Sleep(50 * time.Millisecond)
		}
		close(sendDoneCh)
	}()

	// 在goroutine中接收所有消息
	go func() {
		for i := 0; i < len(messages); i++ {
			received, err := receiver.Receive()
			if err != nil {
				recvErrCh <- fmt.Errorf("接收消息 %d 失败: %w", i+1, err)
				return
			}
			receivedMessages = append(receivedMessages, received)
		}
		close(recvDoneCh)
	}()

	// 等待所有操作完成或出错
	timeout := 30 * time.Second
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case err := <-sendErrCh:
		t.Fatalf("发送错误: %v", err)
	case err := <-recvErrCh:
		t.Fatalf("接收错误: %v", err)
	case <-sendDoneCh:
		// 发送完成，等待接收完成
		select {
		case err := <-recvErrCh:
			t.Fatalf("接收错误: %v", err)
		case <-recvDoneCh:
			// 所有操作成功完成
		case <-timer.C:
			t.Fatalf("等待接收完成超时")
		}
	case <-timer.C:
		t.Fatalf("操作超时")
	}

	// 验证接收到的消息数量
	if len(receivedMessages) != len(messages) {
		t.Fatalf("接收到的消息数量不匹配: 预期 %d, 实际 %d", len(messages), len(receivedMessages))
	}

	// 验证每个消息的内容
	for i, expected := range messages {
		if !bytes.Equal(expected, receivedMessages[i]) {
			t.Fatalf("消息 %d 内容不匹配", i+1)
		}
	}
}

// 测试在实际网络上使用Transporter
func TestTransporterOverNetwork(t *testing.T) {
	// 设置测试协议
	testProtocol := protocol.ID("/pointsub/test/transporter/1.0.0")

	// 创建一对已连接的主机
	_, _, serverConn, clientConn, cleanup := CreateConnectedPair(t, testProtocol)
	defer cleanup()

	// 创建消息发送器和接收器
	sender := NewTestMessageTransporter(clientConn)
	receiver := NewTestMessageTransporter(serverConn)

	// 测试各种大小的消息
	messageSizes := []int{
		10,          // 10 bytes
		1024,        // 1 KB
		10 * 1024,   // 10 KB
		100 * 1024,  // 100 KB
		1024 * 1024, // 1 MB
	}

	for _, size := range messageSizes {
		t.Run(fmt.Sprintf("Size_%d", size), func(t *testing.T) {
			// 创建指定大小的消息
			testMsg := GenerateRandomData(size)

			// 创建通道用于同步和错误处理
			doneCh := make(chan struct{})
			errCh := make(chan error, 2)
			receivedCh := make(chan []byte, 1)

			// 在goroutine中接收消息
			go func() {
				receivedMsg, err := receiver.Receive()
				if err != nil {
					errCh <- err
					return
				}
				receivedCh <- receivedMsg
			}()

			// 在goroutine中发送消息
			go func() {
				err := sender.Send(testMsg)
				if err != nil {
					errCh <- err
					return
				}
				close(doneCh)
			}()

			// 设置超时时间，根据消息大小调整
			timeout := 5 * time.Second
			if size > 100*1024 {
				timeout = 20 * time.Second
			}

			// 等待操作完成或出错
			select {
			case err := <-errCh:
				t.Fatalf("操作失败: %v", err)
			case receivedMsg := <-receivedCh:
				// 验证消息大小
				if len(receivedMsg) != size {
					t.Fatalf("消息大小不匹配: 预期 %d 字节, 实际 %d 字节", size, len(receivedMsg))
				}

				// 验证消息内容
				if !bytes.Equal(testMsg, receivedMsg) {
					t.Fatalf("%d 字节消息内容不匹配", size)
				}

				// 等待发送完成
				select {
				case <-doneCh:
					// 发送成功完成
				case err := <-errCh:
					t.Fatalf("发送过程中出错: %v", err)
				case <-time.After(timeout):
					t.Fatalf("等待发送完成超时")
				}
			case <-time.After(timeout):
				t.Fatalf("操作超时")
			}
		})
	}
}

// 测试错误处理 - 在接收过程中关闭连接
func TestTransporterErrorHandling(t *testing.T) {
	// 创建内存连接对
	clientConn, serverConn := CreateMemoryConnPair(t)

	// 创建消息发送器和接收器
	_ = NewTestMessageTransporter(clientConn) // 我们只需要原始连接，不使用sender
	receiver := NewTestMessageTransporter(serverConn)

	// 创建通道用于同步和错误处理
	errCh := make(chan error, 1)

	// 在goroutine中接收消息
	go func() {
		_, err := receiver.Receive()
		errCh <- err
	}()

	// 发送消息头部，但不发送完整消息
	header := make([]byte, 4) // 4字节表示消息长度
	size := 1024 * 1024       // 1MB
	encodeLength(header, uint32(size))

	_, err := clientConn.Write(header)
	if err != nil {
		t.Fatalf("写入消息头失败: %v", err)
	}

	// 写入部分数据
	partialData := make([]byte, size/2)
	for i := range partialData {
		partialData[i] = byte(i % 256)
	}

	_, err = clientConn.Write(partialData)
	if err != nil {
		t.Fatalf("写入部分数据失败: %v", err)
	}

	// 关闭连接，模拟错误
	clientConn.Close()

	// 等待接收操作完成或超时
	select {
	case err := <-errCh:
		// 验证错误类型
		if err == nil {
			t.Fatal("预期接收失败，但成功了")
		}
		if !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
			t.Fatalf("预期EOF或UnexpectedEOF错误，但得到: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("等待接收操作超时")
	}
}

// 测试超时处理
func TestTransporterTimeout(t *testing.T) {
	// 创建内存连接对
	_, serverConn := CreateMemoryConnPair(t)

	// 设置短超时
	serverConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))

	// 创建接收器
	receiver := NewTestMessageTransporter(serverConn)

	// 创建通道用于同步和错误处理
	errCh := make(chan error, 1)

	// 在goroutine中接收消息
	go func() {
		_, err := receiver.Receive()
		errCh <- err
	}()

	// 等待接收操作完成或超时
	select {
	case err := <-errCh:
		// 验证是否为超时错误
		if err == nil {
			t.Fatal("预期超时错误，但接收成功")
		}
		if !isTimeout(err) {
			t.Fatalf("预期超时错误，但得到: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("等待接收操作超时")
	}
}

// encodeLength 将32位长度编码为4字节
func encodeLength(buf []byte, length uint32) {
	buf[0] = byte(length >> 24)
	buf[1] = byte(length >> 16)
	buf[2] = byte(length >> 8)
	buf[3] = byte(length)
}
