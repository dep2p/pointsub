// Package pointsub 提供点对点订阅功能的实现
// 该包实现了基于 LibP2P 的点对点通信功能,支持请求-响应模式的消息传递
package pointsub

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/dep2p/pointsub/logger"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-msgio"
)

// ClientConfig 定义客户端配置参数
// 包含了客户端运行所需的各项配置选项
type ClientConfig struct {
	ReadTimeout       time.Duration // 读取超时时间,控制单次读取操作的最大等待时间
	WriteTimeout      time.Duration // 写入超时时间,控制单次写入操作的最大等待时间
	ConnectTimeout    time.Duration // 连接超时时间,控制建立连接的最大等待时间
	MaxRetries        int           // 最大重试次数,发送失败时的最大重试次数
	RetryInterval     time.Duration // 重试间隔时间,两次重试之间的等待时间
	MaxBlockSize      int           // 最大数据块大小,单次传输的最大字节数
	EnableCompression bool          // 是否启用压缩,控制是否对传输数据进行压缩
	MaxIdleConns      int           // 最大空闲连接数,连接池中保持的最大空闲连接数
	IdleConnTimeout   time.Duration // 空闲连接超时时间,空闲连接被清理前的最大存活时间
}

// DefaultClientConfig 返回默认的客户端配置
// 该函数返回一个使用推荐默认值的配置实例
// 返回值:
// - *ClientConfig: 包含默认配置值的 ClientConfig 实例
func DefaultClientConfig() *ClientConfig {
	return &ClientConfig{
		ReadTimeout:       30 * time.Second, // 默认读取超时30秒
		WriteTimeout:      30 * time.Second, // 默认写入超时30秒
		ConnectTimeout:    5 * time.Second,  // 默认连接超时5秒
		MaxRetries:        3,                // 默认最大重试3次
		RetryInterval:     time.Second,      // 默认重试间隔1秒
		MaxBlockSize:      1 << 25,          // 默认最大块大小32MB
		EnableCompression: true,             // 默认启用压缩
		MaxIdleConns:      100,              // 默认最大空闲连接100个
		IdleConnTimeout:   5 * time.Minute,  // 默认空闲连接超时5分钟
	}
}

// Client 定义客户端结构体
// 该结构体封装了客户端的所有功能和状态
type Client struct {
	host        host.Host     // libp2p主机实例,用于网络通信
	config      *ClientConfig // 客户端配置,存储所有配置参数
	activeConns sync.Map      // 活跃连接映射,存储当前所有活跃的连接
	done        chan struct{} // 关闭信号通道,用于通知清理协程退出
	closeOnce   sync.Once     // 确保只关闭一次,避免重复关闭
}

// NewClient 创建新的客户端实例
// 该函数初始化一个新的客户端,并启动必要的后台任务
// 参数:
// - h: libp2p主机实例,用于网络通信
// - config: 客户端配置,如果为nil则使用默认配置
// 返回值:
// - *Client: 新创建的客户端实例
// - error: 如果配置无效则返回错误
func NewClient(h host.Host, config *ClientConfig) (*Client, error) {
	// 如果未提供配置,使用默认配置
	if config == nil {
		config = DefaultClientConfig()
	}

	// 验证配置的有效性
	if err := validateClientConfig(config); err != nil {
		logger.Errorf("配置无效: %v", err)
		return nil, errors.New("配置无效")
	}

	// 创建客户端实例
	client := &Client{
		host:   h,
		config: config,
		done:   make(chan struct{}),
	}

	// 启动连接清理循环
	client.startCleanupLoop()
	return client, nil
}

// Send 发送请求并接收响应
// 该方法实现了完整的请求-响应通信流程,支持重试机制
// 参数:
// - ctx: 上下文,用于控制请求的生命周期
// - peerID: 目标节点ID,指定请求发送的目标节点
// - protocolID: 协议ID,指定使用的通信协议
// - request: 请求数据,要发送的数据内容
// 返回值:
// - []byte: 响应数据,目标节点返回的响应内容
// - error: 发送过程中的错误,如果发生错误则返回对应的错误信息
func (c *Client) Send(ctx context.Context, peerID peer.ID, protocolID protocol.ID, request []byte) ([]byte, error) {
	// 检查请求数据大小是否超过限制
	if len(request) > c.config.MaxBlockSize {
		logger.Errorf("请求数据超过最大限制: %d > %d", len(request), c.config.MaxBlockSize)
		return nil, errors.New("请求数据超过最大限制")
	}

	// 检查是否在向自己发送请求
	if peerID == c.host.ID() {
		logger.Error("不能发送请求给自己")
		return nil, errors.New("不能发送请求给自己")
	}

	var lastErr error
	// 循环重试直到达到最大重试次数
	for i := 0; i <= c.config.MaxRetries; i++ {
		response, err := c.sendOnce(ctx, peerID, protocolID, request)
		if err == nil {
			return response, nil
		}

		lastErr = err
		if i < c.config.MaxRetries {
			logger.Warnf("请求失败，正在重试(%d/%d): %v", i+1, c.config.MaxRetries, err)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(c.config.RetryInterval):
				continue
			}
		}
	}

	logger.Errorf("请求失败: %v", lastErr)
	return nil, errors.New("请求失败")
}

// sendOnce 执行单次发送操作
// 该方法处理单次请求-响应的完整流程,包括建立连接、发送数据和接收响应
// 参数:
// - ctx: 上下文,用于控制请求的生命周期
// - peerID: 目标节点ID,指定请求发送的目标节点
// - protocolID: 协议ID,指定使用的通信协议
// - request: 请求数据,要发送的数据内容
// 返回值:
// - []byte: 响应数据,目标节点返回的响应内容
// - error: 发送过程中的错误,如果发生错误则返回对应的错误信息
func (c *Client) sendOnce(ctx context.Context, peerID peer.ID, protocolID protocol.ID, request []byte) ([]byte, error) {
	// 获取连接
	conn, err := c.getConnection(ctx, peerID, protocolID)
	if err != nil {
		logger.Errorf("获取连接失败: %v", err)
		return nil, errors.New("获取连接失败")
	}

	// 处理panic情况,确保连接被正确关闭
	defer func() {
		if r := recover(); r != nil {
			c.putConnection(conn)
			panic(r) // 重新抛出panic
		}
	}()

	// 使用defer清除超时设置
	defer conn.SetWriteDeadline(time.Time{})
	defer conn.SetReadDeadline(time.Time{})

	// 设置写入超时
	if err := conn.SetWriteDeadline(time.Now().Add(c.config.WriteTimeout)); err != nil {
		logger.Errorf("设置写入超时失败: %v", err)
		return nil, errors.New("设置写入超时失败")
	}

	// 写入请求数据时检查上下文是否取消
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		writer := msgio.NewWriter(conn)
		if err := writer.WriteMsg(request); err != nil {
			logger.Errorf("写入请求失败: %v", err)
			return nil, errors.New("写入请求失败")
		}
	}

	// 设置读取超时
	if err := conn.SetReadDeadline(time.Now().Add(c.config.ReadTimeout)); err != nil {
		logger.Errorf("设置读取超时失败: %v", err)
		return nil, errors.New("设置读取超时失败")
	}

	// 读取响应数据时检查上下文是否取消
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		reader := msgio.NewReader(conn)
		response, err := reader.ReadMsg()
		if err != nil {
			// 处理连接关闭错误
			if err == io.EOF {
				logger.Errorf("连接已关闭: %v", err)
				return nil, errors.New("连接已关闭")
			}

			// 处理超时错误
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				logger.Errorf("读取响应超时: %v", err)
				return nil, errors.New("读取响应超时")
			}

			logger.Errorf("读取响应失败: %v", err)
			return nil, errors.New("读取响应失败")
		}

		return response, nil
	}
}

// getConnection 获取连接
// 该方法负责建立和管理与目标节点的网络连接
// 参数:
// - ctx: 上下文,用于控制连接建立的生命周期
// - peerID: 目标节点ID,指定要连接的节点
// - protocolID: 协议ID,指定使用的通信协议
// 返回值:
// - net.Conn: 建立的网络连接
// - error: 获取连接过程中的错误
func (c *Client) getConnection(ctx context.Context, peerID peer.ID, protocolID protocol.ID) (net.Conn, error) {
	// 创建新连接
	conn, err := Dial(ctx, c.host, peerID, protocolID)
	if err != nil {
		logger.Errorf("创建连接失败: %v", err)
		return nil, errors.New("创建连接失败")
	}

	// 包装连接以限制数据块大小
	conn = NewLimitedConn(conn, c.config.MaxBlockSize)
	c.activeConns.Store(conn, time.Now())
	return conn, nil
}

// putConnection 归还连接
// 该方法负责处理不再使用的连接,包括清理和关闭操作
// 参数:
// - conn: 要归还的网络连接
func (c *Client) putConnection(conn net.Conn) {
	c.activeConns.Delete(conn)
	if err := conn.Close(); err != nil {
		logger.Debugf("关闭连接失败: %v", err)
	}
}

// Close 关闭客户端
// 该方法负责清理客户端资源,确保所有连接被正确关闭
// 返回值:
// - error: 关闭过程中的错误
func (c *Client) Close() error {
	var closeErr error
	c.closeOnce.Do(func() {
		close(c.done)
		// 关闭所有活跃连接
		c.activeConns.Range(func(key, value interface{}) bool {
			if conn, ok := key.(net.Conn); ok {
				if err := conn.Close(); err != nil {
					closeErr = err // 保存第一个遇到的错误
					logger.Debugf("关闭连接失败: %v", err)
				}
			}
			c.activeConns.Delete(key)
			return true
		})
	})
	return closeErr
}

// validateClientConfig 验证客户端配置
// 该方法检查配置参数的有效性,确保所有参数都在合理范围内
// 参数:
// - config: 要验证的客户端配置
// 返回值:
// - error: 配置无效时返回相应的错误
func validateClientConfig(config *ClientConfig) error {
	if config.ReadTimeout <= 0 {
		logger.Error("读取超时时间必须为正数")
		return errors.New("读取超时时间必须为正数")
	}
	if config.WriteTimeout <= 0 {
		logger.Error("写入超时时间必须为正数")
		return errors.New("写入超时时间必须为正数")
	}
	if config.ConnectTimeout <= 0 {
		logger.Error("连接超时时间必须为正数")
		return errors.New("连接超时时间必须为正数")
	}
	if config.MaxBlockSize <= 0 {
		logger.Error("最大块大小必须为正数")
		return errors.New("最大块大小必须为正数")
	}
	if config.MaxBlockSize > 1<<30 {
		logger.Error("最大块大小过大")
		return errors.New("最大块大小过大")
	}
	if config.MaxRetries < 0 {
		logger.Error("最大重试次数不能为负数")
		return errors.New("最大重试次数不能为负数")
	}
	if config.RetryInterval <= 0 {
		logger.Error("重试间隔必须为正数")
		return errors.New("重试间隔必须为正数")
	}
	if config.MaxIdleConns <= 0 {
		logger.Error("最大空闲连接数必须为正数")
		return errors.New("最大空闲连接数必须为正数")
	}
	if config.IdleConnTimeout <= 0 {
		logger.Error("空闲连接超时时间必须为正数")
		return errors.New("空闲连接超时时间必须为正数")
	}
	return nil
}

// startCleanupLoop 启动连接清理循环
// 该方法启动一个后台协程,定期清理超时的空闲连接
func (c *Client) startCleanupLoop() {
	go func() {
		// 创建定时器,间隔为空闲超时时间的一半
		ticker := time.NewTicker(c.config.IdleConnTimeout / 2)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// 获取当前时间
				now := time.Now()
				// 遍历所有连接
				c.activeConns.Range(func(key, value interface{}) bool {
					if lastActive, ok := value.(time.Time); ok {
						// 如果连接超时,则关闭并删除
						if now.Sub(lastActive) > c.config.IdleConnTimeout {
							if conn, ok := key.(net.Conn); ok {
								conn.Close()
								c.activeConns.Delete(key)
							}
						}
					}
					return true
				})
			case <-c.done:
				// 收到关闭信号时退出
				return
			}
		}
	}()
}
