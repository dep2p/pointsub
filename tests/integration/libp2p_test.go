package integration

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/core/protocol"

	"github.com/dep2p/pointsub"
)

// TestBasicPointSubTransport 测试基本的PointSub传输功能
func TestBasicPointSubTransport(t *testing.T) {
	// 创建两个libp2p主机
	h1, err := libp2p.New()
	if err != nil {
		t.Fatalf("创建libp2p主机1失败: %v", err)
	}
	defer h1.Close()

	h2, err := libp2p.New()
	if err != nil {
		t.Fatalf("创建libp2p主机2失败: %v", err)
	}
	defer h2.Close()

	// 设置对等体信息
	h1.Peerstore().AddAddrs(h2.ID(), h2.Addrs(), peerstore.PermanentAddrTTL)
	h2.Peerstore().AddAddrs(h1.ID(), h1.Addrs(), peerstore.PermanentAddrTTL)

	t.Logf("创建了两个libp2p主机:")
	t.Logf("主机1: %s", h1.ID().String())
	t.Logf("主机2: %s", h2.ID().String())

	// 定义协议ID
	protocolID := protocol.ID("/pointsub/test/1.0.0")

	// 在主机1上启动PointSub监听器
	listener, err := pointsub.Listen(h1, protocolID)
	if err != nil {
		t.Fatalf("在主机1上启动PointSub监听器失败: %v", err)
	}
	defer listener.Close()

	// 测试数据传输
	done := make(chan struct{})
	dataSize := 1024 * 1024 // 1MB
	expectedData := generateRandomData(dataSize)

	// 在主机1上启动接收处理
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			t.Errorf("接受连接失败: %v", err)
			return
		}
		t.Logf("主机1接受了来自主机2的连接")

		receivedData := make([]byte, dataSize)
		n, err := io.ReadFull(conn, receivedData)
		if err != nil {
			t.Errorf("读取数据失败: %v", err)
			return
		}

		if n != dataSize {
			t.Errorf("数据长度不匹配，期望%d字节，实际读取%d字节", dataSize, n)
			return
		}

		// 验证数据完整性
		for i := 0; i < len(expectedData); i++ {
			if expectedData[i] != receivedData[i] {
				t.Errorf("数据在位置%d不匹配", i)
				return
			}
		}

		t.Logf("成功接收并验证了%d字节的数据", n)
		close(done)
	}()

	// 从主机2连接到主机1
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 使用PointSub.Dial连接到主机1
	t.Logf("主机2尝试连接到主机1...")
	conn, err := pointsub.Dial(ctx, h2, h1.ID(), protocolID)
	if err != nil {
		t.Fatalf("主机2连接到主机1失败: %v", err)
	}
	defer conn.Close()
	t.Logf("主机2成功连接到主机1")

	// 发送数据
	startTime := time.Now()
	n, err := conn.Write(expectedData)
	if err != nil {
		t.Fatalf("发送数据失败: %v", err)
	}
	if n != dataSize {
		t.Fatalf("发送数据不完整，期望发送%d字节，实际发送%d字节", dataSize, n)
	}
	t.Logf("主机2发送了%d字节的数据", n)

	// 等待接收完成
	select {
	case <-done:
		transferTime := time.Since(startTime)
		throughput := float64(dataSize) / transferTime.Seconds() / 1024 / 1024
		t.Logf("数据传输完成，耗时: %v, 吞吐量: %.2f MB/s", transferTime, throughput)
	case <-time.After(30 * time.Second):
		t.Fatalf("等待数据接收超时")
	}
}

// TestMultipleConnections 测试多连接场景
func TestMultipleConnections(t *testing.T) {
	// 创建一个中心节点和多个客户端节点
	centerHost, err := libp2p.New()
	if err != nil {
		t.Fatalf("创建中心节点失败: %v", err)
	}
	defer centerHost.Close()

	// 定义协议ID
	protocolID := protocol.ID("/pointsub/test/1.0.0")

	// 在中心节点启动PointSub监听器
	listener, err := pointsub.Listen(centerHost, protocolID)
	if err != nil {
		t.Fatalf("在中心节点启动PointSub监听器失败: %v", err)
	}
	defer listener.Close()

	// 创建客户端节点
	clientCount := 5
	clients := make([]host.Host, clientCount)
	for i := 0; i < clientCount; i++ {
		client, err := libp2p.New()
		if err != nil {
			t.Fatalf("创建客户端节点%d失败: %v", i, err)
		}
		defer client.Close()

		// 设置对等体信息
		client.Peerstore().AddAddrs(centerHost.ID(), centerHost.Addrs(), peerstore.PermanentAddrTTL)
		centerHost.Peerstore().AddAddrs(client.ID(), client.Addrs(), peerstore.PermanentAddrTTL)

		clients[i] = client
	}

	t.Logf("创建了1个中心节点和%d个客户端节点", clientCount)

	// 处理来自客户端的连接
	go func() {
		for i := 0; i < clientCount; i++ {
			conn, err := listener.Accept()
			if err != nil {
				t.Errorf("接受连接失败: %v", err)
				return
			}

			// 处理每个连接
			go func(id int, conn io.ReadWriteCloser) {
				buffer := make([]byte, 4096)
				n, err := conn.Read(buffer)
				if err != nil {
					t.Errorf("从客户端%d读取数据失败: %v", id, err)
					return
				}

				t.Logf("中心节点收到来自客户端%d的%d字节数据", id, n)

				// 将数据回传给客户端
				_, err = conn.Write(buffer[:n])
				if err != nil {
					t.Errorf("向客户端%d发送数据失败: %v", id, err)
					return
				}
			}(i, conn)
		}
	}()

	// 从每个客户端连接到中心节点
	for i, client := range clients {
		go func(id int, client host.Host) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			// 连接到中心节点
			conn, err := pointsub.Dial(ctx, client, centerHost.ID(), protocolID)
			if err != nil {
				t.Errorf("客户端%d连接到中心节点失败: %v", id, err)
				return
			}
			defer conn.Close()

			// 生成并发送测试数据
			testData := []byte(fmt.Sprintf("来自客户端%d的测试数据", id))
			_, err = conn.Write(testData)
			if err != nil {
				t.Errorf("客户端%d发送数据失败: %v", id, err)
				return
			}

			// 读取回应数据
			buffer := make([]byte, len(testData))
			n, err := io.ReadFull(conn, buffer)
			if err != nil {
				t.Errorf("客户端%d读取回应失败: %v", id, err)
				return
			}

			if n != len(testData) || string(buffer) != string(testData) {
				t.Errorf("客户端%d收到的回应数据与发送的不匹配", id)
				return
			}

			t.Logf("客户端%d成功完成数据交换", id)
		}(i, client)
	}

	// 等待所有客户端完成
	time.Sleep(5 * time.Second)
}

// TestLargeFileTransfer 测试大文件传输
func TestLargeFileTransfer(t *testing.T) {
	// 创建两个主机
	h1, err := libp2p.New()
	if err != nil {
		t.Fatalf("创建主机1失败: %v", err)
	}
	defer h1.Close()

	h2, err := libp2p.New()
	if err != nil {
		t.Fatalf("创建主机2失败: %v", err)
	}
	defer h2.Close()

	// 设置对等体信息
	h1.Peerstore().AddAddrs(h2.ID(), h2.Addrs(), peerstore.PermanentAddrTTL)
	h2.Peerstore().AddAddrs(h1.ID(), h1.Addrs(), peerstore.PermanentAddrTTL)

	// 定义协议ID
	protocolID := protocol.ID("/pointsub/test/1.0.0")

	// 在主机1上启动PointSub监听器
	listener, err := pointsub.Listen(h1, protocolID)
	if err != nil {
		t.Fatalf("在主机1上启动PointSub监听器失败: %v", err)
	}
	defer listener.Close()

	// 测试不同大小的文件传输
	fileSizes := []int{
		100 * 1024,       // 100KB
		1 * 1024 * 1024,  // 1MB
		10 * 1024 * 1024, // 10MB
	}

	for _, size := range fileSizes {
		testData := generateRandomData(size)
		done := make(chan struct{})

		t.Logf("开始测试%d字节的文件传输", size)

		// 接收方
		go func() {
			conn, err := listener.Accept()
			if err != nil {
				t.Errorf("接受连接失败: %v", err)
				return
			}

			receivedData := make([]byte, size)
			n, err := io.ReadFull(conn, receivedData)
			if err != nil {
				t.Errorf("读取数据失败: %v", err)
				return
			}

			if n != size {
				t.Errorf("数据长度不匹配，期望%d字节，实际读取%d字节", size, n)
				return
			}

			for i := 0; i < size; i += size / 10 {
				if testData[i] != receivedData[i] {
					t.Errorf("数据在位置%d不匹配", i)
					return
				}
			}

			close(done)
		}()

		// 发送方
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		conn, err := pointsub.Dial(ctx, h2, h1.ID(), protocolID)
		if err != nil {
			cancel()
			t.Fatalf("连接失败: %v", err)
		}

		startTime := time.Now()
		n, err := conn.Write(testData)
		if err != nil {
			cancel()
			conn.Close()
			t.Fatalf("发送数据失败: %v", err)
		}

		if n != size {
			cancel()
			conn.Close()
			t.Fatalf("发送数据不完整，期望发送%d字节，实际发送%d字节", size, n)
		}

		// 等待接收完成
		select {
		case <-done:
			transferTime := time.Since(startTime)
			throughput := float64(size) / transferTime.Seconds() / 1024 / 1024
			t.Logf("%d字节文件传输完成，耗时: %v, 吞吐量: %.2f MB/s", size, transferTime, throughput)
		case <-time.After(60 * time.Second):
			t.Fatalf("%d字节文件传输超时", size)
		}

		conn.Close()
		cancel()
		time.Sleep(1 * time.Second) // 等待连接完全关闭
	}
}

// 生成指定大小的随机数据
func generateRandomData(size int) []byte {
	data := make([]byte, size)
	_, err := rand.Read(data)
	if err != nil {
		panic(fmt.Sprintf("生成随机数据失败: %v", err))
	}
	return data
}
