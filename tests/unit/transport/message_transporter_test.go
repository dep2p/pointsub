package pointsub_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/dep2p/pointsub/tests/utils/test_helpers"
)

// 测试消息传输器的基本功能
func TestMessageTransporterBasic(t *testing.T) {
	// 创建内存连接对
	clientConn, serverConn := test_helpers.CreateMemoryConnPair()

	// 创建消息发送器和接收器
	sender := test_helpers.NewTestMessageTransporter(clientConn)
	receiver := test_helpers.NewTestMessageTransporter(serverConn)

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

// 测试消息传输器的流式传输功能
func TestMessageTransporterStream(t *testing.T) {
	// 创建内存连接对
	clientConn, serverConn := test_helpers.CreateMemoryConnPair()

	// 创建消息发送器和接收器
	sender := test_helpers.NewTestMessageTransporter(clientConn)
	receiver := test_helpers.NewTestMessageTransporter(serverConn)

	// 创建测试数据
	testData := test_helpers.GenerateRandomData(1024 * 1024) // 1MB
	reader := bytes.NewReader(testData)
	var receivedData bytes.Buffer

	// 创建通道用于同步和错误处理
	doneCh := make(chan struct{})
	errCh := make(chan error, 2)

	// 在goroutine中接收流式数据
	go func() {
		err := receiver.ReceiveStream(&receivedData)
		if err != nil {
			errCh <- err
			return
		}
		close(doneCh)
	}()

	// 发送流式数据
	err := sender.SendStream(reader)
	if err != nil {
		t.Fatalf("发送流式数据失败: %v", err)
	}

	// 等待接收完成或出错
	select {
	case err := <-errCh:
		t.Fatalf("接收流式数据失败: %v", err)
	case <-doneCh:
		// 验证接收到的数据
		if !bytes.Equal(testData, receivedData.Bytes()) {
			t.Fatal("流式传输数据不匹配")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("流式传输超时")
	}
}
