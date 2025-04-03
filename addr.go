package pointsub

import "github.com/libp2p/go-libp2p/core/peer"

// addr 实现了 net.Addr 接口并持有 libp2p peer ID。
type addr struct{ id peer.ID }

// Network 返回此地址所属的网络名称（libp2p）。
func (a *addr) Network() string { return Network }

// String 返回此地址的 peer ID 的字符串形式（B58 编码）。
func (a *addr) String() string { return a.id.String() }
