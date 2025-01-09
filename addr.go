// Package PointSub 提供了基于 libp2p 的流式处理功能
package pointsub

import "github.com/dep2p/libp2p/core/peer"

// addr 结构体实现了 net.Addr 接口,用于保存 libp2p 的对等节点 ID
// id: 对等节点的唯一标识符
type addr struct{ id peer.ID }

// Network 返回此地址所属的网络名称(libp2p)
// 参数:
//   - a: addr 结构体指针
//
// 返回值:
//   - string: 网络名称,固定返回 Network 常量
func (a *addr) Network() string { return Network }

// String 将此地址的对等节点 ID 转换为字符串形式(B58编码)
// 参数:
//   - a: addr 结构体指针
//
// 返回值:
//   - string: B58编码的对等节点 ID 字符串
func (a *addr) String() string { return a.id.String() }
