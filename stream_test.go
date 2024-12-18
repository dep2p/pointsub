// Package PointSub 提供了基于 libp2p 的流式处理功能
package pointsub

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"
)

// newHost 创建一个新的 libp2p 主机实例
// 参数:
//   - t: 测试对象,用于报告测试失败
//   - listen: 监听的多地址
//
// 返回值:
//   - host.Host: 创建的 libp2p 主机实例
func newHost(t *testing.T, listen multiaddr.Multiaddr) host.Host {
	// 使用给定的监听地址创建新的 libp2p 主机
	h, err := libp2p.New(
		libp2p.ListenAddrs(listen),
	)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// TestServerClient 测试 libp2p 的服务端和客户端通信功能
// 该测试用例模拟了一个完整的客户端-服务端通信流程:
// 1. 创建两个独立的 libp2p 主机(服务端和客户端)
// 2. 建立连接并交换消息
// 3. 验证连接属性和消息内容
// 参数:
//   - t: 测试对象
func TestServerClient(t *testing.T) {
	// 创建两个本地测试用的多地址
	// 使用 127.0.0.1 环回地址,分别监听 10000 和 10001 端口
	m1, _ := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/10000")
	m2, _ := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/10001")

	// 分别为服务端和客户端创建 libp2p 主机实例
	srvHost := newHost(t, m1)
	clientHost := newHost(t, m2)
	// 确保测试结束时关闭两个主机
	defer srvHost.Close()
	defer clientHost.Close()

	// 在对等存储(peerstore)中添加双方的地址信息
	// 这样双方才能相互发现和连接
	// PermanentAddrTTL 表示这些地址信息永久有效
	srvHost.Peerstore().AddAddrs(clientHost.ID(), clientHost.Addrs(), peerstore.PermanentAddrTTL)
	clientHost.Peerstore().AddAddrs(srvHost.ID(), srvHost.Addrs(), peerstore.PermanentAddrTTL)

	// 定义用于标识此测试通信的协议 ID
	var tag protocol.ID = "/testitytest"
	// 创建可取消的上下文,用于控制整个测试流程
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 创建同步通道,用于等待服务端处理完成
	done := make(chan struct{})
	// 启动服务端处理 goroutine
	go func() {
		defer close(done)
		// 创建基于 libp2p 的网络监听器
		listener, err := Listen(srvHost, tag)
		if err != nil {
			t.Error(err)
			return
		}
		defer listener.Close()

		// 验证监听器的地址是否为服务端主机的 ID
		if listener.Addr().String() != srvHost.ID().String() {
			t.Error("错误的监听地址")
			return
		}

		// 等待并接受来自客户端的连接
		servConn, err := listener.Accept()
		if err != nil {
			t.Error(err)
			return
		}
		defer servConn.Close()

		// 创建带缓冲的读取器,用于从连接中读取数据
		reader := bufio.NewReader(servConn)
		for {
			// 读取客户端发送的消息,以换行符为分隔
			msg, err := reader.ReadString('\n')
			if err == io.EOF {
				break // 连接关闭
			}
			if err != nil {
				t.Error(err)
				return
			}
			fmt.Printf("请求: ===> %v", msg)
			// 验证接收到的消息内容是否符合预期
			if msg != "libp2p 很棒吗？\n" {
				t.Errorf("收到不良消息: %s", msg)
				return
			}

			// 向客户端发送响应消息
			_, err = servConn.Write([]byte("是的\n"))
			if err != nil {
				t.Error(err)
				return
			}
		}
	}()

	// 客户端通过 Dial 连接到服务端
	// 使用服务端的 ID 和协议标识符建立连接
	clientConn, err := Dial(ctx, clientHost, srvHost.ID(), tag)
	if err != nil {
		t.Fatal(err)
	}

	// 验证连接的本地地址是否为客户端 ID
	if clientConn.LocalAddr().String() != clientHost.ID().String() {
		t.Fatal("错误的本地地址")
	}

	// 验证连接的远程地址是否为服务端 ID
	if clientConn.RemoteAddr().String() != srvHost.ID().String() {
		t.Fatal("远程地址错误")
	}

	// 验证连接使用的网络类型是否正确
	if clientConn.LocalAddr().Network() != Network {
		t.Fatal("网络不佳()")
	}

	// 设置整个连接的超时时间为 1 秒
	err = clientConn.SetDeadline(time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}

	// 设置读操作的超时时间为 1 秒
	err = clientConn.SetReadDeadline(time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}

	// 设置写操作的超时时间为 1 秒
	err = clientConn.SetWriteDeadline(time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}

	// 向服务端发送测试消息
	_, err = clientConn.Write([]byte("libp2p 很棒吗？\n"))
	if err != nil {
		t.Fatal(err)
	}

	// 创建读取器并读取服务端的响应
	reader := bufio.NewReader(clientConn)
	resp, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}

	fmt.Printf("响应: ===> %v", resp)
	// 验证服务端响应的内容是否符合预期
	if string(resp) != "是的\n" {
		t.Errorf("收到不良响应: %s", resp)
	}

	// 关闭客户端连接
	err = clientConn.Close()
	if err != nil {
		t.Fatal(err)
	}
	// 等待服务端处理完成
	<-done
}
