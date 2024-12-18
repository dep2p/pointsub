// Package PointSub 提供了使用 LibP2P streams 替换 Go 标准网络栈的功能
//
// 主要功能:
// - 接收一个 libp2p.Host 参数
// - 提供 Dial() 和 Listen() 方法,返回 net.Conn 和 net.Listener 的实现
//
// 网络寻址:
// - 不使用传统的 "host:port" 寻址方式
// - 使用 Peer ID 进行寻址
// - 使用 libp2p 的 net.Stream 替代原始 TCP 连接
// - 支持 LibP2P 的多路由、NAT 穿透和流复用功能
//
// 使用限制:
// - LibP2P hosts 不能自己连接自己
// - 同一个 Host 不能同时作为服务端和客户端使用
package pointsub

// Network 定义了 PointSub 连接使用的网络类型名称
// 用于在调用 net.Addr.Network() 时返回
// 对应的 net.Addr.String() 将返回 peer ID
var Network = "pointsub"
