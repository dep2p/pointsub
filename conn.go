// Package PointSub 提供了基于 dep2p 的流式处理功能
package pointsub

import (
	"context"
	"net"

	"github.com/dep2p/go-dep2p/core/host"
	"github.com/dep2p/go-dep2p/core/network"
	"github.com/dep2p/go-dep2p/core/peer"
	"github.com/dep2p/go-dep2p/core/protocol"
)

// conn 结构体实现了 net.Conn 接口,封装了 dep2p 的流
type conn struct {
	network.Stream // 内嵌 dep2p 的 Stream 接口
}

// newConn 创建一个新的 conn 对象
// 参数:
//   - s: dep2p 的流对象
//
// 返回值:
//   - net.Conn: 标准网络连接接口
func newConn(s network.Stream) net.Conn {
	return &conn{s} // 返回封装了流的 conn 对象
}

// LocalAddr 返回本地网络地址
// 参数:
//   - c: conn 结构体指针
//
// 返回值:
//   - net.Addr: 本地网络地址
func (c *conn) LocalAddr() net.Addr {
	return &addr{c.Stream.Conn().LocalPeer()} // 返回本地对等节点的地址
}

// RemoteAddr 返回远程网络地址
// 参数:
//   - c: conn 结构体指针
//
// 返回值:
//   - net.Addr: 远程网络地址
func (c *conn) RemoteAddr() net.Addr {
	return &addr{c.Stream.Conn().RemotePeer()} // 返回远程对等节点的地址
}

// Dial 使用给定的主机打开到目标地址的流
// 参数:
//   - ctx: 上下文对象,用于控制操作的生命周期
//   - h: dep2p 主机对象
//   - pid: 目标对等节点的 ID
//   - tag: 协议标识符
//
// 返回值:
//   - net.Conn: 标准网络连接接口
//   - error: 错误信息
func Dial(ctx context.Context, h host.Host, pid peer.ID, tag protocol.ID) (net.Conn, error) {
	// 创建新的流
	s, err := h.NewStream(
		network.WithNoDial(ctx, "should already have connection"), // 如果已经建立了连接，则不进行新的连接
		pid, // 目标对等节点的 ID
		tag, // 协议标识符
	)
	if err != nil {
		logger.Errorf("创建新的流失败: %v", err)
		return nil, err // 如果出错则返回错误
	}
	return newConn(s), nil // 返回封装了流的连接对象
}
