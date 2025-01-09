// Package PointSub 提供了基于 libp2p 的流式处理功能
package pointsub

import (
	"context"
	"net"

	"github.com/dep2p/libp2p/core/host"
	"github.com/dep2p/libp2p/core/network"
	"github.com/dep2p/libp2p/core/protocol"
)

// listener 结构体实现了 net.Listener 接口,用于处理来自 libp2p 连接的带标签流
// 可以通过 Listen() 函数创建一个监听器
type listener struct {
	host     host.Host           // libp2p 主机对象
	ctx      context.Context     // 上下文对象,用于控制生命周期
	tag      protocol.ID         // 协议标识符
	cancel   func()              // 取消函数,用于终止监听器
	streamCh chan network.Stream // 流通道,用于接收新的流连接
}

// Accept 返回此监听器的下一个连接
// 如果没有连接则会阻塞
// 底层使用 libp2p 流作为连接
// 参数:
//   - l: listener 结构体指针
//
// 返回值:
//   - net.Conn: 标准网络连接接口
//   - error: 错误信息
func (l *listener) Accept() (net.Conn, error) {
	select {
	case s := <-l.streamCh: // 从流通道接收新的流
		return newConn(s), nil // 将流封装为连接对象返回
	case <-l.ctx.Done(): // 如果上下文被取消
		return nil, l.ctx.Err() // 返回上下文错误
	}
}

// Close 终止此监听器
// 终止后将不再处理任何新的流连接
// 参数:
//   - l: listener 结构体指针
//
// 返回值:
//   - error: 错误信息
func (l *listener) Close() error {
	l.cancel()                        // 调用取消函数终止上下文
	l.host.RemoveStreamHandler(l.tag) // 移除流处理器
	return nil
}

// Addr 返回此监听器的地址(即其 libp2p 对等节点 ID)
// 参数:
//   - l: listener 结构体指针
//
// 返回值:
//   - net.Addr: 网络地址接口
func (l *listener) Addr() net.Addr {
	return &addr{l.host.ID()} // 返回封装了主机 ID 的地址对象
}

// Listen 提供一个标准的 net.Listener 接口用于接受"连接"
// 底层使用带有指定协议 ID 标签的 libp2p 流
// 参数:
//   - h: libp2p 主机对象
//   - tag: 协议标识符
//
// 返回值:
//   - net.Listener: 标准网络监听器接口
//   - error: 错误信息
func Listen(h host.Host, tag protocol.ID) (net.Listener, error) {
	// 创建一个可取消的上下文
	ctx, cancel := context.WithCancel(context.Background())

	// 创建监听器对象
	l := &listener{
		host:     h,
		ctx:      ctx,
		cancel:   cancel,
		tag:      tag,
		streamCh: make(chan network.Stream),
	}

	// 设置流处理器
	h.SetStreamHandler(tag, func(s network.Stream) {
		select {
		case l.streamCh <- s: // 将新的流发送到通道
		case <-ctx.Done(): // 如果上下文被取消
			s.Reset() // 重置流
		}
	})

	return l, nil
}
