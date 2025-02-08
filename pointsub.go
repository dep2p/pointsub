// Package pointsub 提供了使用 dep2p streams 替换 Go 标准网络栈的功能
//
// 主要功能:
// - 接收一个 dep2p.Host 参数
// - 提供 Dial() 和 Listen() 方法,返回 net.Conn 和 net.Listener 的实现
//
// 网络寻址:
// - 不使用传统的 "host:port" 寻址方式
// - 使用 Peer ID 进行寻址
// - 使用 dep2p 的 net.Stream 替代原始 TCP 连接
// - 支持 dep2p 的多路由、NAT 穿透和流复用功能
//
// 使用限制:
// - dep2p hosts 不能自己连接自己
// - 同一个 Host 不能同时作为服务端和客户端使用
package pointsub

import (
	"context"
	"fmt"
	"sync"

	"github.com/dep2p/go-dep2p/core/host"
	"github.com/dep2p/go-dep2p/core/peer"
	"github.com/dep2p/go-dep2p/core/protocol"
	logging "github.com/dep2p/log"
)

var logger = logging.Logger("pointsub")

// Network 定义了 PointSub 连接使用的网络类型名称
// 用于在调用 net.Addr.Network() 时返回
// 对应的 net.Addr.String() 将返回 peer ID
var Network = "pointsub"

// PointSub 封装了点对点通信的服务端和客户端
type PointSub struct {
	server  *Server      // 服务端
	client  *Client      // 客户端
	host    host.Host    // dep2p主机实例
	mu      sync.RWMutex // 互斥锁
	started bool         // 是否已启动
}

// NewPointSub 创建并返回一个新的 PointSub 实例
// 参数:
//   - h: dep2p主机实例
//   - clientOpts: 客户端配置选项
//   - serverOpts: 服务端配置选项
//   - enableServer: 是否启动服务端
//
// 返回值:
//   - *PointSub: 新创建的PointSub实例
//   - error: 如果创建过程中发生错误则返回错误信息
func NewPointSub(h host.Host, clientOpts []ClientOption, serverOpts []ServerOption, enableServer bool) (*PointSub, error) {
	if h == nil {
		return nil, fmt.Errorf("host不能为nil")
	}

	// 初始化客户端
	client, err := NewClient(h, clientOpts...)
	if err != nil {
		logger.Errorf("创建客户端失败: %v", err)
		return nil, err
	}

	ps := &PointSub{
		client:  client,
		host:    h,
		started: false,
	}

	// 如果配置了自动启动服务端，则启动服务端
	if enableServer {
		if err := ps.StartServer(serverOpts); err != nil {
			logger.Errorf("启动服务端失败: %v", err)
			return nil, err
		}
	}

	return ps, nil
}

// StartServer 启动服务端，如果服务端已经启动则直接返回
// 参数:
//   - opts: 服务端配置选项
//
// 返回值:
//   - error: 如果启动过程中发生错误则返回错误信息
func (ps *PointSub) StartServer(opts []ServerOption) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if ps.started {
		return nil
	}

	server, err := NewServer(ps.host, opts...)
	if err != nil {
		logger.Errorf("创建服务端失败: %v", err)
		return err
	}

	ps.server = server
	ps.started = true
	logger.Info("服务端启动成功")
	return nil
}

// StopServer 停止服务端，如果服务端未启动则直接返回
// 返回值:
//   - error: 如果停止过程中发生错误则返回错误信息
func (ps *PointSub) StopServer() error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if !ps.started {
		return nil
	}

	if ps.server != nil {
		ps.server.Stop()
		ps.server = nil
		ps.started = false
		logger.Info("服务端停止成功")
	}
	return nil
}

// IsServerRunning 检查服务端是否正在运行
// 返回值:
//   - bool: 如果服务端正在运行则返回true，否则返回false
func (ps *PointSub) IsServerRunning() bool {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.started
}

// Server 获取服务端实例
// 返回值:
//   - *Server: 服务端实例，如果服务端未启动则返回nil
func (ps *PointSub) Server() *Server {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.server
}

// Client 获取客户端实例
// 返回值:
//   - *Client: 客户端实例
func (ps *PointSub) Client() *Client {
	return ps.client
}

// Start 启动指定协议的处理服务
// 参数:
//   - protocolID: 协议ID
//   - handler: 消息处理函数
//
// 返回值:
//   - error: 如果启动失败则返回错误信息
func (ps *PointSub) Start(protocolID protocol.ID, handler StreamHandler) error {
	if !ps.IsServerRunning() {
		return fmt.Errorf("服务端未启动")
	}
	return ps.Server().Start(protocolID, handler)
}

// Send 发送消息到指定节点
// 参数:
//   - ctx: 上下文
//   - peerID: 目标节点ID
//   - protocolID: 协议ID
//   - data: 要发送的数据
//
// 返回值:
//   - []byte: 响应数据
//   - error: 如果发送失败则返回错误信息
func (ps *PointSub) Send(ctx context.Context, peerID peer.ID, protocolID protocol.ID, data []byte) ([]byte, error) {
	return ps.Client().Send(ctx, peerID, protocolID, data)
}

// SendClosest 发送消息到最近的支持指定协议的节点
// 参数:
//   - ctx: 上下文
//   - protocolID: 协议ID
//   - data: 要发送的数据
//
// 返回值:
//   - []byte: 响应数据
//   - error: 如果发送失败则返回错误信息
func (ps *PointSub) SendClosest(ctx context.Context, protocolID protocol.ID, data []byte) ([]byte, error) {
	return ps.Client().SendClosest(ctx, protocolID, data)
}

// Stop 停止所有服务
// 这个方法会同时停止服务端和客户端
func (ps *PointSub) Stop() {
	ps.StopServer()
	if ps.client != nil {
		ps.client.Close()
	}
}
