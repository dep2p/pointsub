// Package pointsub 允许用 [libp2p](https://github.com/libp2p/go-libp2p) 流替换 Go 的标准网络栈。
//
// 给定一个 libp2p.Host，pointsub 提供 Dial() 和 Listen() 方法，这些方法返回 net.Conn 和 net.Listener 的实现。
//
// 与常规的 "host:port" 寻址方式不同，`pointsub` 使用 Peer ID，而不是原始 TCP 连接，pointsub 将使用 libp2p 的 net.Stream。
// 这意味着您的连接将利用 libp2p 的多路由、NAT 穿透和流多路复用功能。
//
// 注意，libp2p 主机不能向自己拨号，所以不可能使用同一个 Host 作为服务器和客户端。
package pointsub

// Network 是 pointsub 连接使用的地址返回的 "net.Addr.Network()" 名称。相应地，"net.Addr.String()" 将是一个 peer ID。
var Network = "libp2p"
