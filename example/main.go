package main

import (
	"bytes"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/dep2p/pointsub"
)

// PointSub智能消息传输系统使用示例
// 该示例展示了PointSub系统的核心特性：
// 1. 统一的消息传输接口 - 无论消息大小，都使用相同的API
// 2. 智能的内部处理 - 自动选择最佳传输策略
// 3. 进度跟踪和错误处理 - 提供完善的状态反馈

<<<<<<< HEAD
// 定义一个简单的测试日志器
type testLogger struct{}

func (t *testLogger) Logf(format string, args ...interface{}) {
	fmt.Printf(format+"\n", args...)
}

func (t *testLogger) Fatalf(format string, args ...interface{}) {
	log.Fatalf(format, args...)
}

=======
>>>>>>> 6613f0351ad580eb6dda4edd3f91c53cbf4b91a9
func main() {
	// 示例1：基础消息发送与接收
	fmt.Println("=== 示例1: 基础消息发送与接收 ===")
	fmt.Println("展示PointSub统一消息传输接口的基本用法")
	basicExample()

	// 示例2：流式传输
	fmt.Println("\n=== 示例2: 流式数据传输 ===")
	fmt.Println("展示PointSub处理未知大小数据流的能力")
	streamExample()

	// 示例3：大文件传输与进度跟踪
	fmt.Println("\n=== 示例3: 大文件传输与进度跟踪 ===")
	fmt.Println("展示PointSub处理大文件时的进度反馈机制")
	largeFileExample()

	// 示例4：错误处理
	fmt.Println("\n=== 示例4: 错误处理 ===")
	fmt.Println("展示PointSub的错误处理和恢复机制")
	errorHandlingExample()
}

<<<<<<< HEAD
// 基本消息收发示例
// 展示了PointSub统一的消息传输接口的基本使用方法
func basicExample() {
	fmt.Println("准备进行基本消息传输测试...")

	// 创建帧处理器
	frameProcessor := pointsub.NewAdvancedFrameProcessor(
		pointsub.WithFrameTimeout(10*time.Second), // 设置帧处理超时
		pointsub.WithErrorRecovery(true),          // 启用错误恢复
	)

	// 使用net.Pipe创建连接对
	clientConn, serverConn := net.Pipe()

	// 创建消息传输器
	clientTransporter := pointsub.NewMessageTransporter(clientConn,
		pointsub.WithFrameProcessor(frameProcessor),
	)
	serverTransporter := pointsub.NewMessageTransporter(serverConn,
		pointsub.WithFrameProcessor(frameProcessor),
	)

	// 准备测试消息
	testMessage := "这是一条测试消息 - Hello from PointSub!"
	done := make(chan struct{})

	// 在另一个goroutine中接收消息
	go func() {
		defer close(done)
		fmt.Println("接收方: 等待接收消息...")

		// 接收消息
		message, err := serverTransporter.Receive()
=======
// 基础消息发送与接收示例
// 展示了PointSub统一的消息传输接口如何简化小消息的发送和接收
func basicExample() {
	// 创建一对连接的pipe，模拟网络连接
	client, server := net.Pipe()

	// 创建客户端发送器
	// 通过统一的MessageTransporter接口处理所有大小的消息
	clientTransporter := pointsub.NewMessageTransporter(client)

	// 创建服务端接收器
	serverTransporter := pointsub.NewMessageTransporter(server)

	// 测试消息
	message := []byte("这是一条测试消息，用于展示PointSub系统的基础功能")

	// 在另一个goroutine中接收消息
	go func() {
		// 接收消息 - 使用统一的Receive方法，无需关心消息大小
		received, err := serverTransporter.Receive()
>>>>>>> 6613f0351ad580eb6dda4edd3f91c53cbf4b91a9
		if err != nil {
			log.Fatalf("接收消息失败: %v", err)
		}

<<<<<<< HEAD
		// 打印接收到的消息
		fmt.Printf("接收方: 成功接收消息 - %s\n", string(message))

		// 发送响应确认
		err = serverTransporter.Send([]byte("确认接收"))
		if err != nil {
			log.Fatalf("发送确认消息失败: %v", err)
		}
	}()

	// 发送测试消息
	fmt.Println("发送方: 发送测试消息...")
	err := clientTransporter.Send([]byte(testMessage))
	if err != nil {
		log.Fatalf("发送消息失败: %v", err)
	}
	fmt.Println("发送方: 消息发送成功")

	// 接收确认响应
	response, err := clientTransporter.Receive()
	if err != nil {
		log.Fatalf("接收确认消息失败: %v", err)
	}
	fmt.Printf("发送方: 收到确认响应 - %s\n", string(response))

	// 等待接收goroutine完成
	<-done

	// 关闭连接
	clientTransporter.Close()
	serverTransporter.Close()
}

// 流式数据传输示例
// 展示了PointSub处理未知大小的流式数据的能力
func streamExample() {
	fmt.Println("准备进行流式数据传输测试...")

	// 创建帧处理器，设置较长的超时和错误恢复
	frameProcessor := pointsub.NewAdvancedFrameProcessor(
		pointsub.WithFrameTimeout(30*time.Second), // 设置帧处理超时
		pointsub.WithErrorRecovery(true),          // 启用错误恢复
	)

	// 使用net.Pipe创建连接对
	clientConn, serverConn := net.Pipe()

	// 创建消息传输器
	clientTransporter := pointsub.NewMessageTransporter(clientConn,
		pointsub.WithFrameProcessor(frameProcessor),
	)
	serverTransporter := pointsub.NewMessageTransporter(serverConn,
		pointsub.WithFrameProcessor(frameProcessor),
	)

	// 创建测试数据 - 大约2800字节
	testData := bytes.Repeat([]byte("流数据测试消息-"), 200)
	fmt.Printf("准备流传输测试数据，大小: %.2f KB\n", float64(len(testData))/1024)

	// 用于同步的通道
	done := make(chan struct{})
	resultCh := make(chan struct {
		data []byte
		err  error
	})

	// 在另一个goroutine中接收数据
	go func() {
		defer close(done)
		fmt.Println("接收方: 准备接收流数据...")

		// 准备接收缓冲区
		var receivedBuffer bytes.Buffer

		// 设置更长的超时
		serverTransporter.SetReadDeadline(time.Now().Add(30 * time.Second))

		// 接收流数据 - ReceiveStream方法处理未知大小的数据流
		fmt.Println("接收方: 开始调用ReceiveStream()...")
		err := serverTransporter.ReceiveStream(&receivedBuffer)

		// 重置超时
		serverTransporter.SetReadDeadline(time.Time{})

		if err != nil {
			fmt.Printf("接收方: 接收出错: %v\n", err)
		} else {
			fmt.Printf("接收方: 成功接收 %d 字节\n", receivedBuffer.Len())
		}

		// 发送结果
		resultCh <- struct {
			data []byte
			err  error
		}{
			data: receivedBuffer.Bytes(),
			err:  err,
		}
	}()

	// 等待接收方准备好
	time.Sleep(2 * time.Second)

	// 设置发送超时
	clientTransporter.SetWriteDeadline(time.Now().Add(30 * time.Second))

	// 发送流数据
	fmt.Println("发送方: 开始发送流数据...")
	err := clientTransporter.SendStream(bytes.NewReader(testData))

	// 重置超时
	clientTransporter.SetWriteDeadline(time.Time{})

	if err != nil {
		log.Fatalf("发送流数据失败: %v", err)
	}
	fmt.Println("发送方: 流数据发送完成")

	// 设置超时等待接收结果
	select {
	case result := <-resultCh:
		if result.err != nil {
			log.Fatalf("接收流数据失败: %v", result.err)
		}

		// 验证数据完整性
		if bytes.Equal(result.data, testData) {
			fmt.Println("流数据接收成功，内容完全匹配")
			fmt.Printf("接收到 %d 字节数据\n", len(result.data))
		} else {
			if len(result.data) == 0 {
				fmt.Println("流数据接收失败: 未收到任何数据")
			} else {
				fmt.Printf("流数据内容不匹配: 期望 %d 字节，收到 %d 字节\n",
					len(testData), len(result.data))
				// 添加额外调试信息
				if len(result.data) != len(testData) {
					fmt.Printf("大小不匹配: 预期 %d 字节, 收到 %d 字节\n", len(testData), len(result.data))
				} else {
					// 查找第一个不匹配的位置
					for i := 0; i < len(testData); i++ {
						if result.data[i] != testData[i] {
							fmt.Printf("首个不匹配位置: 第 %d 字节, 预期 %02X, 收到 %02X\n",
								i, testData[i], result.data[i])
							break
						}
					}
				}
			}
		}
	case <-time.After(40 * time.Second):
		log.Fatalf("接收流数据超时")
	}

	// 等待接收goroutine完成
	<-done

	// 清理连接
	clientTransporter.Close()
	serverTransporter.Close()
=======
		// 验证消息
		if bytes.Equal(received, message) {
			fmt.Println("消息接收成功，内容匹配")
			fmt.Printf("接收到消息: %s\n", string(received))
		} else {
			fmt.Println("消息内容不匹配")
		}
	}()

	// 发送消息 - 使用统一的Send方法，系统会自动选择最佳传输策略
	err := clientTransporter.Send(message)
	if err != nil {
		log.Fatalf("发送消息失败: %v", err)
	}
	fmt.Println("消息发送成功")

	// 等待接收完成
	time.Sleep(100 * time.Millisecond)

	// 关闭连接
	client.Close()
	server.Close()
}

// 流式数据传输示例
// 展示了PointSub如何处理未知大小的数据流，自动适应传输策略
func streamExample() {
	// 创建一对连接的pipe，模拟网络连接
	client, server := net.Pipe()

	// 创建客户端发送器
	clientTransporter := pointsub.NewMessageTransporter(client)

	// 创建服务端接收器
	serverTransporter := pointsub.NewMessageTransporter(server)

	// 准备测试数据
	testData := bytes.Repeat([]byte("流式传输测试数据块-"), 1000) // 约25KB数据

	// 在另一个goroutine中接收流数据
	go func() {
		// 准备接收缓冲区
		var receivedBuffer bytes.Buffer

		// 接收流数据 - ReceiveStream方法处理未知大小的数据流
		// 系统会自动优化缓冲区管理和内存使用
		err := serverTransporter.ReceiveStream(&receivedBuffer)
		if err != nil {
			log.Fatalf("接收流数据失败: %v", err)
		}

		// 验证数据完整性
		if bytes.Equal(receivedBuffer.Bytes(), testData) {
			fmt.Println("流数据接收成功，内容匹配")
			fmt.Printf("接收到 %d 字节数据\n", receivedBuffer.Len())
		} else {
			fmt.Println("流数据内容不匹配")
		}
	}()

	// 发送流数据 - SendStream方法处理未知大小的数据流
	// 系统会根据实际传输情况自动调整分块大小和策略
	err := clientTransporter.SendStream(bytes.NewReader(testData))
	if err != nil {
		log.Fatalf("发送流数据失败: %v", err)
	}
	fmt.Println("流数据发送成功")

	// 等待接收完成
	time.Sleep(500 * time.Millisecond)

	// 关闭连接
	client.Close()
	server.Close()
>>>>>>> 6613f0351ad580eb6dda4edd3f91c53cbf4b91a9
}

// 大文件传输与进度跟踪示例
// 展示了PointSub处理大文件时的进度跟踪机制
func largeFileExample() {
<<<<<<< HEAD
	// 创建基于dep2p的连接对
	fmt.Println("准备进行大文件传输测试...")

	// 创建帧处理器，设置较长的超时
	frameProcessor := pointsub.NewAdvancedFrameProcessor(
		pointsub.WithFrameTimeout(60*time.Second), // 设置更长的帧处理超时
		pointsub.WithErrorRecovery(true),          // 启用错误恢复
	)

	// 使用net.Pipe创建连接对，以便更可靠地测试大文件传输
	clientConn, serverConn := net.Pipe()

	// 创建消息传输器
	clientTransporter := pointsub.NewMessageTransporter(clientConn,
		pointsub.WithFrameProcessor(frameProcessor),
		pointsub.WithProgressTracker(pointsub.NewProgressTracker(
			pointsub.WithSpeedSampling(true, 5),
			pointsub.WithProgressThreshold(5.0),
		)),
	)

	serverTransporter := pointsub.NewMessageTransporter(serverConn,
		pointsub.WithFrameProcessor(frameProcessor),
		pointsub.WithProgressTracker(pointsub.NewProgressTracker(
			pointsub.WithSpeedSampling(true, 5),
			pointsub.WithProgressThreshold(5.0),
		)),
	)

	// 创建进度跟踪器用于UI显示
	progressTracker := pointsub.NewProgressTracker(
		pointsub.WithSpeedSampling(true, 5),
		pointsub.WithProgressThreshold(5.0),
	)

	// 添加进度回调
	progressTracker.AddCallback(&progressCallback{})

	// 创建模拟大文件数据（为便于调试，首先使用较小的数据）
	largeData := bytes.Repeat([]byte("大文件传输测试数据块-"), 1000) // 约28KB
	fmt.Printf("准备传输数据大小: %.2f KB\n", float64(len(largeData))/1024)

	// 通道用于同步和传递结果
	done := make(chan struct{})
	resultCh := make(chan struct {
		data []byte
		err  error
	})

	// 在另一个goroutine中接收文件
	go func() {
		defer close(done)
		fmt.Println("接收方: 准备接收大文件数据...")

		// 准备接收缓冲区
		var receivedBuffer bytes.Buffer

		// 设置较长的读取超时
		serverTransporter.SetReadDeadline(time.Now().Add(90 * time.Second))

		// 接收文件数据
		fmt.Println("接收方: 开始调用ReceiveStream()...")
		err := serverTransporter.ReceiveStream(&receivedBuffer)

		// 重置超时
		serverTransporter.SetReadDeadline(time.Time{})

		if err != nil {
			fmt.Printf("接收方: 接收出错: %v\n", err)
		} else {
			fmt.Printf("接收方: 成功接收 %d 字节\n", receivedBuffer.Len())
		}

		// 发送结果
		resultCh <- struct {
			data []byte
			err  error
		}{
			data: receivedBuffer.Bytes(),
			err:  err,
=======
	// 创建一对连接的pipe，模拟网络连接
	client, server := net.Pipe()

	// 创建进度跟踪器 - 允许实时监控传输进度
	progressTracker := pointsub.NewProgressTracker(
		pointsub.WithSpeedSampling(true, 5), // 启用速度采样，每5秒更新一次
		pointsub.WithProgressThreshold(5.0), // 每增加5%更新一次进度
	)

	// 添加进度回调 - 实现用户界面反馈
	progressTracker.AddCallback(&progressCallback{})

	// 创建客户端发送器（带进度跟踪）
	// 通过选项模式配置传输器行为，保持API一致性
	clientTransporter := pointsub.NewMessageTransporter(
		client,
		pointsub.WithProgressTracker(progressTracker),
	)

	// 创建服务端接收器
	serverTransporter := pointsub.NewMessageTransporter(server)

	// 创建模拟大文件数据 (约5MB)
	largeData := bytes.Repeat([]byte("大文件传输测试数据块-"), 200000)
	fmt.Printf("准备传输数据大小: %.2f MB\n", float64(len(largeData))/(1024*1024))

	// 在另一个goroutine中接收文件
	go func() {
		// 准备接收缓冲区
		var receivedBuffer bytes.Buffer

		// 接收文件数据 - 对于大文件，系统自动采用流处理模式
		// 优化内存使用和传输效率
		err := serverTransporter.ReceiveStream(&receivedBuffer)
		if err != nil {
			log.Fatalf("接收文件数据失败: %v", err)
		}

		// 验证数据完整性
		if receivedBuffer.Len() == len(largeData) {
			fmt.Println("文件接收成功，大小匹配")
			fmt.Printf("接收到 %.2f MB数据\n", float64(receivedBuffer.Len())/(1024*1024))
		} else {
			fmt.Printf("文件大小不匹配: 期望 %d 字节, 实际接收 %d 字节\n",
				len(largeData), receivedBuffer.Len())
>>>>>>> 6613f0351ad580eb6dda4edd3f91c53cbf4b91a9
		}
	}()

	// 开始传输前创建传输ID和进度跟踪
	transferID := "file-transfer-1"
	progressTracker.StartTracking(transferID, int64(len(largeData)))

<<<<<<< HEAD
	// 等待接收方准备好
	time.Sleep(2 * time.Second)

	// 设置较长的写入超时
	clientTransporter.SetWriteDeadline(time.Now().Add(90 * time.Second))

	// 发送文件数据
	fmt.Println("发送方: 开始发送大文件数据...")
	err := clientTransporter.SendStream(bytes.NewReader(largeData))

	// 重置超时
	clientTransporter.SetWriteDeadline(time.Time{})

	if err != nil {
		fmt.Printf("发送文件数据失败: %v\n", err)
		progressTracker.MarkFailed(transferID, err)
		log.Fatalf("退出测试: 发送失败")
	} else {
		fmt.Println("发送方: 文件数据发送完成")
		progressTracker.UpdateStatus(transferID, pointsub.StatusCompleted)
	}

	// 设置超时等待接收结果
	select {
	case result := <-resultCh:
		if result.err != nil {
			log.Fatalf("接收文件数据失败: %v", result.err)
		}

		// 验证数据完整性
		if bytes.Equal(result.data, largeData) {
			fmt.Println("文件接收成功，数据完全匹配")
			fmt.Printf("接收到 %.2f KB数据\n", float64(len(result.data))/1024)
		} else {
			fmt.Printf("文件内容不匹配: 期望 %d 字节, 实际接收 %d 字节\n",
				len(largeData), len(result.data))

			// 添加额外的错误诊断信息
			if len(result.data) != len(largeData) {
				fmt.Printf("大小不匹配: 预期 %d 字节, 收到 %d 字节\n", len(largeData), len(result.data))
			} else {
				// 查找第一个不匹配的位置
				for i := 0; i < len(largeData); i++ {
					if result.data[i] != largeData[i] {
						fmt.Printf("首个不匹配位置: 第 %d 字节, 预期 %02X, 收到 %02X\n",
							i, largeData[i], result.data[i])
						break
					}
				}
			}
		}
	case <-time.After(100 * time.Second):
		log.Fatalf("接收文件数据超时")
	}

	// 等待接收goroutine完成
	<-done
=======
	// 发送文件数据 - 使用与小消息相同的API，系统自动处理大文件传输策略
	err := clientTransporter.SendStream(bytes.NewReader(largeData))
	if err != nil {
		log.Fatalf("发送文件数据失败: %v", err)
		progressTracker.MarkFailed(transferID, err)
	} else {
		fmt.Println("文件数据发送成功")
		progressTracker.UpdateStatus(transferID, pointsub.StatusCompleted)
	}

	// 等待接收完成
	time.Sleep(1 * time.Second)
>>>>>>> 6613f0351ad580eb6dda4edd3f91c53cbf4b91a9

	// 停止跟踪
	progressTracker.StopTracking(transferID)

<<<<<<< HEAD
	// 清理网络连接
	clientTransporter.Close()
	serverTransporter.Close()
=======
	// 关闭连接
	client.Close()
	server.Close()
>>>>>>> 6613f0351ad580eb6dda4edd3f91c53cbf4b91a9
}

// 错误处理示例
// 展示了PointSub的错误处理机制
func errorHandlingExample() {
<<<<<<< HEAD
	fmt.Println("准备进行错误处理测试...")

	// 创建自定义错误处理器
	errorHandler := pointsub.NewDefaultErrorHandler()

	// 创建帧处理器
	frameProcessor := pointsub.NewAdvancedFrameProcessor(
		pointsub.WithFrameTimeout(10*time.Second),
		pointsub.WithErrorRecovery(true),
	)

	// 使用net.Pipe创建连接对
	clientConn, serverConn := net.Pipe()

	// 创建消息传输器
	clientTransporter := pointsub.NewMessageTransporter(clientConn,
		pointsub.WithFrameProcessor(frameProcessor),
		pointsub.WithErrorHandler(errorHandler),
	)
	serverTransporter := pointsub.NewMessageTransporter(serverConn,
		pointsub.WithFrameProcessor(frameProcessor),
		pointsub.WithErrorHandler(errorHandler),
	)

	// 注册错误处理回调
=======
	// 创建自定义错误处理器 - 可以根据错误类型定制处理策略
	errorHandler := pointsub.NewDefaultErrorHandler()

	// 创建一对连接的pipe，模拟网络连接
	client, server := net.Pipe()

	// 注册错误处理回调 - 允许应用程序对错误做出响应
>>>>>>> 6613f0351ad580eb6dda4edd3f91c53cbf4b91a9
	errorHandler.RegisterCallback(pointsub.SeverityError, func(err *pointsub.MessageError) {
		fmt.Printf("错误处理回调: %s\n", err.Error())
	})

<<<<<<< HEAD
	// 模拟错误情况：先关闭接收端
	err := serverTransporter.Close()
	if err != nil {
		log.Printf("关闭接收端出错: %v", err)
	}
	fmt.Println("接收端连接已关闭，准备发送消息...")

	// 尝试发送消息（预期会失败）
	err = clientTransporter.Send([]byte("这条消息应该发送失败"))
=======
	// 创建发送器，使用自定义错误处理器
	// 通过选项模式添加错误处理能力，保持API一致性
	transporter := pointsub.NewMessageTransporter(
		client,
		pointsub.WithErrorHandler(errorHandler),
	)

	// 模拟错误情况：关闭接收端连接
	server.Close()

	// 尝试发送消息（预期会失败）
	err := transporter.Send([]byte("这条消息应该发送失败"))
>>>>>>> 6613f0351ad580eb6dda4edd3f91c53cbf4b91a9

	if err != nil {
		fmt.Printf("预期的错误: %v\n", err)

<<<<<<< HEAD
		// 检查是否可重试
=======
		// 检查是否可重试 - 错误分类机制帮助应用程序决定后续操作
>>>>>>> 6613f0351ad580eb6dda4edd3f91c53cbf4b91a9
		if errorHandler.IsRecoverable(err) {
			fmt.Println("这是一个可恢复的错误，可以尝试重试操作")
		} else {
			fmt.Println("这是一个不可恢复的错误，需要中止操作")
		}
	} else {
		fmt.Println("错误：预期应该失败，但发送成功了")
	}

<<<<<<< HEAD
	// 清理资源
	clientTransporter.Close()
=======
	// 关闭客户端连接
	client.Close()
>>>>>>> 6613f0351ad580eb6dda4edd3f91c53cbf4b91a9
}

// 进度回调实现
// 展示如何接收和处理传输进度信息
type progressCallback struct{}

func (pc *progressCallback) OnProgress(transferID string, total int64, transferred int64, percentage float64) {
	fmt.Printf("\r传输进度: %.1f%% (%d/%d 字节)", percentage, transferred, total)
	if percentage >= 100 {
		fmt.Println()
	}
}

func (pc *progressCallback) OnStatusChange(transferID string, oldStatus, newStatus pointsub.TransferStatus) {
	statusNames := map[pointsub.TransferStatus]string{
		pointsub.StatusInitializing: "初始化中",
		pointsub.StatusTransferring: "传输中",
		pointsub.StatusPaused:       "已暂停",
		pointsub.StatusResuming:     "恢复中",
		pointsub.StatusCompleted:    "已完成",
		pointsub.StatusFailed:       "失败",
		pointsub.StatusCancelled:    "已取消",
	}

	oldStatusName, ok1 := statusNames[oldStatus]
	newStatusName, ok2 := statusNames[newStatus]

	if !ok1 {
		oldStatusName = "未知状态"
	}
	if !ok2 {
		newStatusName = "未知状态"
	}

	fmt.Printf("传输状态变更: %s -> %s\n", oldStatusName, newStatusName)
}

func (pc *progressCallback) OnSpeedUpdate(transferID string, bytesPerSecond float64, estimatedTimeLeft time.Duration) {
	fmt.Printf("传输速度: %.2f MB/s, 预计剩余时间: %s\n",
		bytesPerSecond/(1024*1024),
		formatDuration(estimatedTimeLeft))
}

func (pc *progressCallback) OnError(transferID string, err error, isFatal bool) {
	if isFatal {
		fmt.Printf("传输致命错误: %v\n", err)
	} else {
		fmt.Printf("传输错误(非致命): %v\n", err)
	}
}

func (pc *progressCallback) OnComplete(transferID string, totalBytes int64, totalTime time.Duration) {
	fmt.Printf("传输完成: 共传输 %.2f MB, 耗时 %s, 平均速度 %.2f MB/s\n",
		float64(totalBytes)/(1024*1024),
		formatDuration(totalTime),
		float64(totalBytes)/(1024*1024)/totalTime.Seconds())
}

// 格式化时间
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	} else if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	} else if d < time.Hour {
		minutes := int(d.Minutes())
		seconds := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	} else {
		hours := int(d.Hours())
		minutes := int(d.Minutes()) % 60
		return fmt.Sprintf("%dh%dm", hours, minutes)
	}
}
