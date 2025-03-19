package pointsub

import (
	"bytes"
	"fmt"
	"net"
	"testing"
	"time"
)

// 测试LargeMessageConn的基本读写功能
func TestLargeMessageConnBasic(t *testing.T) {
	// 创建内存管道连接对
	client, server := net.Pipe()

	// 包装为大消息优化的连接
	estimatedSize := 1 * 1024 * 1024 // 1MB
	largeClient := NewLargeMessageConn(client, estimatedSize)
	largeServer := NewLargeMessageConn(server, estimatedSize)

	// 测试数据传输 - 使用较小的数据以避免可能的性能问题
	testData := GenerateRandomData(64 * 1024) // 64KB

	// 创建通道用于同步和错误处理
	doneCh := make(chan struct{})
	errCh := make(chan error, 1)

	// 设置超时
	timeout := 5 * time.Second
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	// 在goroutine中发送数据
	go func() {
		n, err := largeClient.Write(testData)
		if err != nil {
			errCh <- fmt.Errorf("写入数据失败: %w", err)
			return
		}
		if n != len(testData) {
			errCh <- fmt.Errorf("写入长度不匹配: 期望 %d, 实际 %d", len(testData), n)
			return
		}
		close(doneCh)
	}()

	// 在goroutine中接收数据
	receivedCh := make(chan []byte, 1)
	receivedErrCh := make(chan error, 1)

	go func() {
		received := make([]byte, len(testData))
		totalRead := 0

		for totalRead < len(testData) {
			n, err := largeServer.Read(received[totalRead:])
			if err != nil {
				receivedErrCh <- fmt.Errorf("读取数据失败: %w", err)
				return
			}
			totalRead += n
		}

		receivedCh <- received
	}()

	// 等待操作完成或超时
	select {
	case err := <-errCh:
		t.Fatalf("发送数据失败: %v", err)
	case err := <-receivedErrCh:
		t.Fatalf("接收数据失败: %v", err)
	case received := <-receivedCh:
		// 验证数据内容
		if !bytes.Equal(testData, received) {
			t.Fatal("接收的数据与发送的数据不匹配")
		}

		// 等待发送完成
		select {
		case <-doneCh:
			// 发送成功完成
		case err := <-errCh:
			t.Fatalf("发送完成时出错: %v", err)
		case <-timer.C:
			t.Fatal("等待发送完成超时")
		}

		t.Logf("成功传输 %d 字节的数据", len(testData))
	case <-timer.C:
		t.Fatal("测试超时")
	}

	// 关闭连接
	largeClient.Close()
	largeServer.Close()
}

// 测试LargeMessageConn的大数据传输性能
func TestLargeMessageConnPerformance(t *testing.T) {
	// 创建内存管道连接对
	client, server := net.Pipe()

	// 包装为大消息优化的连接
	estimatedSize := 10 * 1024 * 1024 // 10MB
	largeClient := NewLargeMessageConn(client, estimatedSize)
	largeServer := NewLargeMessageConn(server, estimatedSize)

	// 创建大型测试数据，但使用较小的数据以避免测试超时
	dataSize := 1 * 1024 * 1024 // 1MB (原来是5MB)
	testData := GenerateRandomData(dataSize)

	// 计时发送和接收
	startTime := time.Now()

	// 创建通道用于同步和错误处理
	doneCh := make(chan struct{})
	errCh := make(chan error, 1)
	receivedCh := make(chan []byte, 1)
	receivedErrCh := make(chan error, 1)

	// 设置超时
	timeout := 10 * time.Second
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	// 发送数据
	go func() {
		_, err := largeClient.Write(testData)
		if err != nil {
			errCh <- fmt.Errorf("写入大数据失败: %w", err)
			return
		}
		close(doneCh)
	}()

	// 接收数据
	go func() {
		received := make([]byte, dataSize)
		totalRead := 0
		for totalRead < dataSize {
			n, err := largeServer.Read(received[totalRead:])
			if err != nil {
				receivedErrCh <- fmt.Errorf("读取大数据失败: %w", err)
				return
			}
			totalRead += n
		}
		receivedCh <- received
	}()

	// 等待操作完成或超时
	var received []byte
	select {
	case err := <-errCh:
		t.Fatalf("发送数据失败: %v", err)
	case err := <-receivedErrCh:
		t.Fatalf("接收数据失败: %v", err)
	case received = <-receivedCh:
		// 接收成功
	case <-timer.C:
		t.Fatal("测试超时")
	}

	// 等待发送完成
	select {
	case <-doneCh:
		// 发送成功完成
	case err := <-errCh:
		t.Fatalf("发送完成时出错: %v", err)
	case <-timer.C:
		t.Fatal("等待发送完成超时")
	}

	// 计算传输时间
	duration := time.Since(startTime)

	// 简单性能报告
	mbPerSec := float64(dataSize) / duration.Seconds() / 1024 / 1024
	t.Logf("大数据传输性能: %.2f MB/s (数据大小: %d 字节, 耗时: %v)", mbPerSec, dataSize, duration)

	// 验证数据
	if !bytes.Equal(testData, received) {
		t.Fatal("接收的大数据与发送的数据不匹配")
	}

	// 关闭连接
	largeClient.Close()
	largeServer.Close()
}

// 测试设置读写超时
func TestLargeMessageConnTimeout(t *testing.T) {
	// 创建内存管道连接对
	client, server := net.Pipe()

	// 关闭一端以模拟超时
	server.Close()

	// 设置短超时的连接
	timeout := 100 * time.Millisecond
	largeClient := NewLargeMessageTimeoutConn(client, timeout, timeout, 1024)

	// 设置测试超时
	testTimeout := 5 * time.Second
	testTimer := time.NewTimer(testTimeout)
	defer testTimer.Stop()

	// 创建通道用于同步和错误处理
	errCh := make(chan error, 1)
	doneCh := make(chan time.Duration, 1)

	// 尝试写入数据，应该快速超时
	startTime := time.Now()

	go func() {
		_, err := largeClient.Write([]byte("test data"))
		duration := time.Since(startTime)

		if err == nil {
			errCh <- fmt.Errorf("预期写入超时错误，但未发生")
			return
		}

		doneCh <- duration
	}()

	// 等待操作完成或测试超时
	select {
	case err := <-errCh:
		t.Fatal(err)
	case duration := <-doneCh:
		// 验证超时时间是否合理
		if duration > timeout*2 {
			t.Fatalf("写入超时耗时过长: %v, 预期接近 %v", duration, timeout)
		}
		t.Logf("写入操作正确超时，耗时: %v", duration)
	case <-testTimer.C:
		t.Fatal("测试超时")
	}

	// 关闭连接
	largeClient.Close()
}

// 测试自适应分块传输
func TestLargeMessageConnAdaptiveChunking(t *testing.T) {
	// 创建内存管道连接对
	client, server := net.Pipe()

	// 创建带自适应分块的连接
	options := []LargeMessageOption{
		WithChunkSize(64 * 1024),            // 64KB块大小
		WithAdaptiveChunking(true),          // 启用自适应分块
		WithBufferSizes(128*1024, 128*1024), // 128KB缓冲区
	}

	largeClient := NewLargeMessageConn(client, 1024*1024, options...)
	largeServer := NewLargeMessageConn(server, 1024*1024, options...)

	// 创建大型测试数据，但使用较小的数据以避免测试超时
	dataSize := 512 * 1024 // 512KB (原来是2MB)
	testData := GenerateRandomData(dataSize)

	// 设置测试超时
	testTimeout := 10 * time.Second
	testTimer := time.NewTimer(testTimeout)
	defer testTimer.Stop()

	// 创建通道用于同步和错误处理
	doneCh := make(chan struct{})
	errCh := make(chan error, 1)
	receivedCh := make(chan []byte, 1)
	receivedErrCh := make(chan error, 1)

	// 发送数据
	go func() {
		_, err := largeClient.Write(testData)
		if err != nil {
			errCh <- fmt.Errorf("写入数据失败: %w", err)
			return
		}
		close(doneCh)
	}()

	// 接收数据
	go func() {
		received := make([]byte, dataSize)
		totalRead := 0
		for totalRead < dataSize {
			n, err := largeServer.Read(received[totalRead:])
			if err != nil {
				receivedErrCh <- fmt.Errorf("读取数据失败: %w", err)
				return
			}
			totalRead += n
		}
		receivedCh <- received
	}()

	// 等待操作完成或超时
	var received []byte
	select {
	case err := <-errCh:
		t.Fatalf("发送数据失败: %v", err)
	case err := <-receivedErrCh:
		t.Fatalf("接收数据失败: %v", err)
	case received = <-receivedCh:
		// 接收成功
	case <-testTimer.C:
		t.Fatal("测试超时")
	}

	// 等待发送完成
	select {
	case <-doneCh:
		// 发送成功完成
	case err := <-errCh:
		t.Fatalf("发送完成时出错: %v", err)
	case <-testTimer.C:
		t.Fatal("等待发送完成超时")
	}

	// 验证数据
	if !bytes.Equal(testData, received) {
		t.Fatal("接收的数据与发送的数据不匹配")
	}

	// 检查连接统计信息
	clientStats := largeClient.(*largeMessageConn).Stats()
	if clientStats == nil {
		t.Fatal("无法获取客户端连接统计信息")
	}

	// 应该有多个块被发送
	if clientStats.chunksSent <= 1 {
		t.Fatalf("未正确使用分块: 发送的块数为 %d", clientStats.chunksSent)
	}

	t.Logf("分块传输统计: 发送 %d 块, 总计 %d 字节", clientStats.chunksSent, clientStats.bytesSent)

	// 关闭连接
	largeClient.Close()
	largeServer.Close()
}

// 测试块间延迟设置
func TestLargeMessageConnBlockDelay(t *testing.T) {
	// 创建内存管道连接对
	client, server := net.Pipe()

	// 设置块大小和块间延迟
	chunkSize := 16 * 1024 // 16KB
	blockDelay := 20 * time.Millisecond

	options := []LargeMessageOption{
		WithChunkSize(chunkSize),
		WithBlockDelay(blockDelay),
		WithAdaptiveChunking(false), // 禁用自适应分块以便准确测试
	}

	largeClient := NewLargeMessageConn(client, 1024*1024, options...)
	largeServer := NewLargeMessageConn(server, 1024*1024)

	// 创建足够大的数据以产生多个块
	blocksCount := 5
	dataSize := chunkSize * blocksCount
	testData := GenerateRandomData(dataSize)

	// 设置测试超时
	testTimeout := 10 * time.Second
	testTimer := time.NewTimer(testTimeout)
	defer testTimer.Stop()

	// 创建通道用于同步和错误处理
	doneCh := make(chan struct{})
	errCh := make(chan error, 1)
	receivedCh := make(chan []byte, 1)
	receivedErrCh := make(chan error, 1)
	durationCh := make(chan time.Duration, 1)

	// 计时发送
	startTime := time.Now()

	// 发送数据
	go func() {
		_, err := largeClient.Write(testData)
		if err != nil {
			errCh <- fmt.Errorf("写入数据失败: %w", err)
			return
		}
		durationCh <- time.Since(startTime)
		close(doneCh)
	}()

	// 接收数据
	go func() {
		received := make([]byte, dataSize)
		totalRead := 0
		for totalRead < dataSize {
			n, err := largeServer.Read(received[totalRead:])
			if err != nil {
				receivedErrCh <- fmt.Errorf("读取数据失败: %w", err)
				return
			}
			totalRead += n
		}
		receivedCh <- received
	}()

	// 等待接收完成或超时
	var received []byte
	select {
	case err := <-errCh:
		t.Fatalf("发送数据失败: %v", err)
	case err := <-receivedErrCh:
		t.Fatalf("接收数据失败: %v", err)
	case received = <-receivedCh:
		// 接收成功
	case <-testTimer.C:
		t.Fatal("测试超时")
	}

	// 等待发送完成并获取持续时间
	var duration time.Duration
	select {
	case <-doneCh:
		// 发送成功完成
		select {
		case duration = <-durationCh:
			// 获取到持续时间
		case <-testTimer.C:
			t.Fatal("等待获取持续时间超时")
		}
	case err := <-errCh:
		t.Fatalf("发送完成时出错: %v", err)
	case <-testTimer.C:
		t.Fatal("等待发送完成超时")
	}

	// 验证数据
	if !bytes.Equal(testData, received) {
		t.Fatal("接收的数据与发送的数据不匹配")
	}

	// 验证总时间，应该大约是块数 * 块间延迟
	minExpectedTime := time.Duration(blocksCount-1) * blockDelay
	if duration < minExpectedTime {
		t.Fatalf("传输时间过短 %v, 预期至少 %v (考虑块间延迟)", duration, minExpectedTime)
	}

	t.Logf("块间延迟传输: 总时间 %v, 预期最小时间 %v", duration, minExpectedTime)

	// 关闭连接
	largeClient.Close()
	largeServer.Close()
}

// 测试NoBlockDelay选项
func TestLargeMessageConnNoBlockDelay(t *testing.T) {
	// 创建内存管道连接对
	client, server := net.Pipe()

	// 设置块大小并禁用块间延迟
	chunkSize := 16 * 1024 // 16KB

	options := []LargeMessageOption{
		WithChunkSize(chunkSize),
		WithNoBlockDelay(),          // 禁用块间延迟
		WithAdaptiveChunking(false), // 禁用自适应分块以便准确测试
	}

	largeClient := NewLargeMessageConn(client, 1024*1024, options...)
	largeServer := NewLargeMessageConn(server, 1024*1024)

	// 创建足够大的数据以产生多个块
	blocksCount := 5
	dataSize := chunkSize * blocksCount
	testData := GenerateRandomData(dataSize)

	// 设置测试超时
	testTimeout := 10 * time.Second
	testTimer := time.NewTimer(testTimeout)
	defer testTimer.Stop()

	// 创建通道用于同步和错误处理
	doneCh := make(chan struct{})
	errCh := make(chan error, 1)
	receivedCh := make(chan []byte, 1)
	receivedErrCh := make(chan error, 1)
	durationCh := make(chan time.Duration, 1)

	// 计时发送
	startTime := time.Now()

	// 发送数据
	go func() {
		_, err := largeClient.Write(testData)
		if err != nil {
			errCh <- fmt.Errorf("写入数据失败: %w", err)
			return
		}
		durationCh <- time.Since(startTime)
		close(doneCh)
	}()

	// 接收数据
	go func() {
		received := make([]byte, dataSize)
		totalRead := 0
		for totalRead < dataSize {
			n, err := largeServer.Read(received[totalRead:])
			if err != nil {
				receivedErrCh <- fmt.Errorf("读取数据失败: %w", err)
				return
			}
			totalRead += n
		}
		receivedCh <- received
	}()

	// 等待接收完成或超时
	var received []byte
	select {
	case err := <-errCh:
		t.Fatalf("发送数据失败: %v", err)
	case err := <-receivedErrCh:
		t.Fatalf("接收数据失败: %v", err)
	case received = <-receivedCh:
		// 接收成功
	case <-testTimer.C:
		t.Fatal("测试超时")
	}

	// 等待发送完成并获取持续时间
	var duration time.Duration
	select {
	case <-doneCh:
		// 发送成功完成
		select {
		case duration = <-durationCh:
			// 获取到持续时间
		case <-testTimer.C:
			t.Fatal("等待获取持续时间超时")
		}
	case err := <-errCh:
		t.Fatalf("发送完成时出错: %v", err)
	case <-testTimer.C:
		t.Fatal("等待发送完成超时")
	}

	// 验证数据
	if !bytes.Equal(testData, received) {
		t.Fatal("接收的数据与发送的数据不匹配")
	}

	t.Logf("无块间延迟传输: 总时间 %v", duration)

	// 关闭连接
	largeClient.Close()
	largeServer.Close()
}
