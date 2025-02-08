// Package pointsub 提供点对点订阅功能的实现
// 该包实现了基于 dep2p 的点对点通信功能,支持请求-响应模式的消息传递
// 主要功能包括:
// - 支持点对点的消息订阅和发布
// - 提供可靠的请求-响应通信模式
// - 支持连接池管理和自动重试机制
// - 提供完整的指标统计和监控能力
// - 支持超时控制和错误处理
package pointsub

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dep2p/go-dep2p/core/host"
	"github.com/dep2p/go-dep2p/core/peer"
	"github.com/dep2p/go-dep2p/core/protocol"
	"github.com/dep2p/go-dep2p/p2plib/msgio"
)

// ClientConfig 定义客户端配置参数
// 包含了客户端运行所需的各项配置选项
// 通过这些配置可以调整客户端的性能和行为特征
type ClientConfig struct {
	ReadTimeout         time.Duration // 读取超时时间,控制单次读取操作的最大等待时间
	WriteTimeout        time.Duration // 写入超时时间,控制单次写入操作的最大等待时间
	ConnectTimeout      time.Duration // 连接超时时间,控制建立连接的最大等待时间
	MaxRetries          int           // 最大重试次数,发送失败时的最大重试次数
	RetryInterval       time.Duration // 重试间隔时间,两次重试之间的等待时间
	MaxBlockSize        int           // 最大数据块大小,单次传输的最大字节数
	EnableCompression   bool          // 是否启用压缩,控制是否对传输数据进行压缩
	MaxIdleConns        int           // 最大空闲连接数,连接池中保持的最大空闲连接数
	IdleConnTimeout     time.Duration // 空闲连接超时时间,空闲连接被清理前的最大存活时间
	MaxIdleConnsPerPeer int           // 每个peer的最大空闲连接数,限制每个节点的最大空闲连接数
	MaxTotalConns       int           // 总连接数限制,所有连接的最大数量限制
	IdleTimeout         time.Duration // 空闲连接超时时间,连接空闲多久后被关闭
}

// DefaultClientConfig 返回默认的客户端配置
// 该函数返回一个使用推荐默认值的配置实例
// 这些默认值经过实践验证,适用于大多数场景
//
// 返回值:
//   - *ClientConfig: 包含默认配置值的 ClientConfig 实例
func DefaultClientConfig() *ClientConfig {
	return &ClientConfig{
		ReadTimeout:         30 * time.Second, // 默认读取超时30秒
		WriteTimeout:        30 * time.Second, // 默认写入超时30秒
		ConnectTimeout:      5 * time.Second,  // 默认连接超时5秒
		MaxRetries:          3,                // 默认最大重试3次
		RetryInterval:       time.Second,      // 默认重试间隔1秒
		MaxBlockSize:        1 << 25,          // 默认最大块大小32MB
		EnableCompression:   true,             // 默认启用压缩
		MaxIdleConns:        100,              // 默认最大空闲连接100个
		IdleConnTimeout:     5 * time.Minute,  // 默认空闲连接超时5分钟
		MaxIdleConnsPerPeer: 10,
		MaxTotalConns:       100,
		IdleTimeout:         5 * time.Minute,
	}
}

// ClientMetrics 定义客户端指标结构
// 用于记录和统计客户端运行时的各项指标数据
// 所有指标都使用原子操作保证并发安全
type ClientMetrics struct {
	TotalRequests  atomic.Int64 // 总请求数,记录客户端发起的总请求次数
	FailedRequests atomic.Int64 // 失败请求数,记录请求失败的次数
	ActiveRequests atomic.Int32 // 活跃请求数,当前正在处理的请求数量
	BytesRead      atomic.Int64 // 读取字节数,已接收的总字节数
	BytesWritten   atomic.Int64 // 写入字节数,已发送的总字节数
}

// Client 结构体定义了客户端的所有必要字段
// 它封装了与服务端通信所需的所有功能
// 包括连接管理、请求发送、指标统计等
type Client struct {
	host           host.Host                 // dep2p主机实例,用于网络通信
	config         *ClientConfig             // 客户端配置,控制客户端行为
	activeConns    sync.Map                  // 活跃连接映射,记录当前活跃的连接
	done           chan struct{}             // 关闭信号通道,用于优雅关闭
	closeOnce      sync.Once                 // 确保只关闭一次,避免重复关闭
	serverProtocol map[protocol.ID][]peer.ID // 服务端协议映射,记录协议与节点的对应关系
	metrics        *ClientMetrics            // 客户端指标,记录运行时统计数据
	connPool       sync.Map                  // 连接池,缓存空闲连接
	maxIdleConn    int                       // 每个peer的最大空闲连接数限制
	mu             sync.Mutex                // 连接池操作锁,保护连接池操作
	backoff        *backoff                  // 退避管理器
}

// ClientOption 定义客户端选项函数类型
// 用于在创建客户端时配置客户端实例
// 通过函数式选项模式提供灵活的配置方式
//
// 参数:
//   - *Client: 客户端实例指针
//
// 返回值:
//   - error: 选项应用过程中的错误
type ClientOption func(*Client) error

// WithReadTimeout 设置读取超时时间
// 控制单次读取操作的最大等待时间
// 超过此时间未完成读取则返回超时错误
//
// 参数:
//   - timeout: 读取超时时间
//
// 返回值:
//   - ClientOption: 返回一个客户端选项函数
func WithReadTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) error {
		if timeout <= 0 {
			return errors.New("读取超时时间必须大于0")
		}
		c.config.ReadTimeout = timeout
		return nil
	}
}

// WithWriteTimeout 设置写入超时时间
// 控制单次写入操作的最大等待时间
// 超过此时间未完成写入则返回超时错误
//
// 参数:
//   - timeout: 写入超时时间
//
// 返回值:
//   - ClientOption: 返回一个客户端选项函数
func WithWriteTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) error {
		if timeout <= 0 {
			return errors.New("写入超时时间必须大于0")
		}
		c.config.WriteTimeout = timeout
		return nil
	}
}

// WithConnectTimeout 设置连接超时时间
// 控制建立连接的最大等待时间
// 超过此时间未建立连接则返回超时错误
//
// 参数:
//   - timeout: 连接超时时间
//
// 返回值:
//   - ClientOption: 返回一个客户端选项函数
func WithConnectTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) error {
		if timeout <= 0 {
			return errors.New("连接超时时间必须大于0")
		}
		c.config.ConnectTimeout = timeout
		return nil
	}
}

// WithMaxRetries 设置最大重试次数
// 控制请求失败时的重试次数
// 达到最大重试次数后不再重试
//
// 参数:
//   - retries: 最大重试次数
//
// 返回值:
//   - ClientOption: 返回一个客户端选项函数
func WithMaxRetries(retries int) ClientOption {
	return func(c *Client) error {
		if retries < 0 {
			return errors.New("重试次数不能为负数")
		}
		c.config.MaxRetries = retries
		return nil
	}
}

// WithMaxBlockSize 设置最大数据块大小
// 限制单次传输的最大字节数
// 超过此大小的数据将被拒绝发送
//
// 参数:
//   - size: 最大数据块大小
//
// 返回值:
//   - ClientOption: 返回一个客户端选项函数
func WithMaxBlockSize(size int) ClientOption {
	return func(c *Client) error {
		if size <= 0 {
			return errors.New("数据块大小必须大于0")
		}
		c.config.MaxBlockSize = size
		return nil
	}
}

// WithCompression 设置是否启用压缩
// 控制是否对传输数据进行压缩
// 启用压缩可以减少网络传输量
//
// 参数:
//   - enable: 是否启用压缩
//
// 返回值:
//   - ClientOption: 返回一个客户端选项函数
func WithCompression(enable bool) ClientOption {
	return func(c *Client) error {
		c.config.EnableCompression = enable
		return nil
	}
}

// WithServerProtocols 设置服务端协议映射
// 配置协议ID与节点ID的对应关系
// 用于确定请求应该发送给哪些节点
//
// 参数:
//   - protocols: 协议到节点ID的映射
//
// 返回值:
//   - ClientOption: 返回一个客户端选项函数
func WithServerProtocols(protocols map[protocol.ID][]peer.ID) ClientOption {
	return func(c *Client) error {
		if protocols == nil {
			return errors.New("协议映射不能为空")
		}
		for protocolID, peerIDs := range protocols {
			if len(peerIDs) == 0 {
				return fmt.Errorf("协议 %s 的节点列表不能为空", protocolID)
			}
			c.serverProtocol[protocolID] = append([]peer.ID{}, peerIDs...)
		}
		return nil
	}
}

// NewClient 创建新的客户端实例
// 初始化客户端并应用配置选项
// 启动必要的后台任务
//
// 参数:
//   - h: dep2p主机实例
//   - opts: 客户端选项
//
// 返回值:
//   - *Client: 新创建的客户端实例
//   - error: 创建过程中的错误
func NewClient(h host.Host, opts ...ClientOption) (*Client, error) {
	// 创建使用默认配置的客户端
	client := &Client{
		host:           h,
		config:         DefaultClientConfig(),
		done:           make(chan struct{}),
		serverProtocol: make(map[protocol.ID][]peer.ID),
		metrics:        &ClientMetrics{},
		maxIdleConn:    DefaultClientConfig().MaxIdleConnsPerPeer,
		connPool:       sync.Map{},
		activeConns:    sync.Map{},
		backoff:        newBackoff(context.Background(), 1000, BackoffCleanupInterval, MaxBackoffAttempts),
	}

	// 应用所有选项配置
	for _, opt := range opts {
		if err := opt(client); err != nil {
			logger.Errorf("应用选项失败: %v", err)
			return nil, err
		}
	}

	// 启动连接清理循环
	client.startPoolCleaner()
	return client, nil
}

// Send 发送请求并接收响应
// 该方法实现了完整的请求-响应通信流程,支持重试机制
// 在发生可重试错误时会自动进行重试
//
// 参数:
//   - ctx: 上下文,用于控制请求的生命周期
//   - peerID: 目标节点ID,指定请求发送的目标节点(服务端节点)
//   - protocolID: 协议ID,指定使用的通信协议
//   - request: 请求数据,要发送的数据内容
//
// 返回值:
//   - []byte: 响应数据,目标节点返回的响应内容
//   - error: 发送过程中的错误,如果发生错误则返回对应的错误信息
func (c *Client) Send(ctx context.Context, peerID peer.ID, protocolID protocol.ID, request []byte) ([]byte, error) {
	// 检查并获取退避时间
	if delay, err := c.backoff.updateAndGet(peerID); err != nil {
		return nil, fmt.Errorf("节点 %s 已达到最大重试次数: %w", peerID, err)
	} else if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// 检查消息大小
	if len(request) > c.config.MaxBlockSize {
		logger.Errorf("消息大小超过限制: %d > %d", len(request), c.config.MaxBlockSize)
		return nil, ErrMessageTooLarge
	}

	// 不能发送请求给自己
	if peerID == c.host.ID() {
		return nil, errors.New("不能发送请求给自己")
	}

	// 重试机制
	var lastErr error
	// 重试次数
	for i := 0; i <= c.config.MaxRetries; i++ {
		// 执行单次发送操作
		response, err := c.sendOnce(ctx, peerID, protocolID, request)
		if err == nil {
			return response, nil
		}

		// 保存最后一次错误
		lastErr = err
		// 如果重试次数未达到最大值，且错误是可重试的，则进行重试
		if i < c.config.MaxRetries {
			// 判断错误是否可重试
			if !isRetryableError(err) {
				logger.Errorf("请求失败(不可重试): %v", err)
				return nil, err
			}

			logger.Warnf("请求失败，正在重试(%d/%d): %v", i+1, c.config.MaxRetries, err)
			// 等待重试间隔时间
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(c.config.RetryInterval):
				continue
			}
		}
	}

	logger.Errorf("请求失败(重试耗尽): %v", lastErr)
	return nil, lastErr
}

// sendOnce 执行单次发送操作
// 该方法处理单次请求-响应的完整流程,包括建立连接、发送数据和接收响应
// 它负责管理连接的生命周期并更新相关指标
//
// 参数:
//   - ctx: 上下文,用于控制请求的生命周期
//   - peerID: 目标节点ID,指定请求发送的目标节点
//   - protocolID: 协议ID,指定使用的通信协议
//   - request: 请求数据,要发送的数据内容
//
// 返回值:
//   - []byte: 响应数据,目标节点返回的响应内容
//   - error: 发送过程中的错误,如果发生错误则返回对应的错误信息
func (c *Client) sendOnce(ctx context.Context, peerID peer.ID, protocolID protocol.ID, request []byte) ([]byte, error) {
	c.metrics.ActiveRequests.Add(1)
	defer c.metrics.ActiveRequests.Add(-1)

	// 获取连接
	conn, err := c.getConnection(ctx, peerID, protocolID)
	if err != nil {
		logger.Errorf("获取连接失败: %v", err)
		return nil, err
	}

	// 初始化或获取连接状态
	var state *connState
	// 如果连接状态已存在，则直接使用
	if existingState, ok := c.activeConns.Load(conn); ok {
		// 获取连接状态
		state = existingState.(*connState)
	} else {
		// 如果连接状态不存在，则创建新的连接状态
		state = &connState{
			lastActive: time.Now(),
			createdAt:  time.Now(),
		}
		// 将新的连接状态存储到活跃连接映射中
		c.activeConns.Store(conn, state)
	}

	defer func() {
		if r := recover(); r != nil {
			// 释放连接
			c.releaseConn(peerID, conn)
			// 记录panic信息
			logger.Errorf("请求处理panic: %v", r)
			// 重新抛出panic
			panic(r)
		}
	}()

	// 设置超时和处理写入
	if err := c.writeRequest(ctx, conn, request); err != nil {
		c.metrics.FailedRequests.Add(1)
		logger.Errorf("写入请求失败: %v", err)
		return nil, err
	}
	// 记录写入的字节数
	c.metrics.BytesWritten.Add(int64(len(request)))

	// 读取响应
	response, err := c.readResponse(ctx, conn)
	if err != nil {
		c.metrics.FailedRequests.Add(1)
		logger.Errorf("读取响应失败: %v", err)
		return nil, err
	}

	c.metrics.BytesRead.Add(int64(len(response)))
	c.metrics.TotalRequests.Add(1)
	state.requestCount.Add(1)
	state.lastActive = time.Now()

	return response, nil
}

// writeRequest 处理请求写入
// 负责将请求数据写入连接
// 包含超时控制和错误处理
//
// 参数:
//   - ctx: 上下文,用于控制写入操作的生命周期
//   - conn: 网络连接
//   - request: 要写入的请求数据
//
// 返回值:
//   - error: 写入过程中的错误
func (c *Client) writeRequest(ctx context.Context, conn net.Conn, request []byte) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return c.writeMessage(conn, request)
	}
}

// readResponse 处理响应读取
// 负责从连接中读取响应数据
// 包含超时控制和错误处理
//
// 参数:
//   - ctx: 上下文,用于控制读取操作的生命周期
//   - conn: 网络连接
//
// 返回值:
//   - []byte: 读取到的响应数据
//   - error: 读取过程中的错误
func (c *Client) readResponse(ctx context.Context, conn net.Conn) ([]byte, error) {
	// 设置读取超时
	if err := conn.SetReadDeadline(time.Now().Add(c.config.ReadTimeout)); err != nil {
		logger.Errorf("设置读取超时失败: %v", err)
		return nil, err
	}
	defer conn.SetReadDeadline(time.Time{})

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		// 创建读取器
		reader := msgio.NewReader(conn)
		// 读取响应
		response, err := reader.ReadMsg()
		if err != nil {
			// 如果错误是EOF，则表示连接已关闭
			if err == io.EOF {
				logger.Errorf("连接已关闭: %v", err)
				return nil, err
			}
			// 如果错误是超时错误，则表示读取超时
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				logger.Errorf("读取响应超时: %v", err)
				return nil, err
			}
			// 其他错误
			logger.Errorf("读取响应失败: %v", err)
			return nil, err
		}
		// 返回响应
		return response, nil
	}
}

// getConnection 获取或创建连接
// 该方法负责从连接池获取连接或创建新连接
// 实现了连接复用和创建的逻辑
//
// 参数:
//   - ctx: 上下文,用于控制连接建立的生命周期
//   - peerID: 目标节点ID,指定要连接的节点
//   - protocolID: 协议ID,指定使用的通信协议
//
// 返回值:
//   - net.Conn: 建立的网络连接
//   - error: 获取连接过程中的错误
func (c *Client) getConnection(ctx context.Context, peerID peer.ID, protocolID protocol.ID) (net.Conn, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, c.config.ConnectTimeout)
	defer cancel()

	err := c.host.Connect(timeoutCtx, peer.AddrInfo{ID: peerID})
	if err != nil {
		// 记录连接失败
		c.metrics.FailedRequests.Add(1)
		return nil, fmt.Errorf("连接节点失败: %w", err)
	}

	// 确保连接被清理
	var conn net.Conn
	defer func() {
		if err != nil && conn != nil {
			conn.Close()
		}
	}()

	// 尝试从连接池获取
	if conns, ok := c.connPool.Load(peerID); ok {
		// 获取连接列表
		connList := conns.([]net.Conn)
		// 如果连接列表不为空，则从列表中获取最后一个连接
		if len(connList) > 0 {
			c.mu.Lock()
			// 获取最后一个连接
			conn = connList[len(connList)-1]
			// 移除最后一个连接
			connList = connList[:len(connList)-1]
			// 将剩余的连接放回连接池
			c.connPool.Store(peerID, connList)
			c.mu.Unlock()
			return conn, nil
		}
	}

	// 创建新连接
	return Dial(ctx, c.host, peerID, protocolID)
}

// releaseConn 归还连接到连接池
// 该方法负责将连接归还到连接池或关闭连接
// 实现了连接池的管理逻辑
//
// 参数:
//   - peerID: 目标节点ID,指定要归还连接的节点
//   - conn: 要归还的网络连接
func (c *Client) releaseConn(peerID peer.ID, conn net.Conn) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 获取当前连接列表,如果不存在则创建新的
	var connList []net.Conn
	if conns, ok := c.connPool.Load(peerID); ok {
		connList = conns.([]net.Conn)
	}

	// 检查空闲连接数量限制
	if len(connList) < c.maxIdleConn {
		connList = append(connList, conn)
		c.connPool.Store(peerID, connList)
	} else {
		conn.Close()
	}
}

// Close 关闭客户端
// 该方法负责清理客户端资源,确保所有连接被正确关闭
// 实现了优雅关闭的逻辑
//
// 返回值:
//   - error: 关闭过程中的错误
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

// startPoolCleaner 启动连接清理循环
// 该方法启动一个后台协程,定期清理超时的空闲连接
// 实现了连接池的维护机制
func (c *Client) startPoolCleaner() {
	// 启动后台协程
	go func() {
		// 创建一个定时器
		ticker := time.NewTicker(c.config.IdleTimeout / 2)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// 清理空闲连接
				c.cleanIdleConnections()
			case <-c.done:
				return
			}
		}
	}()
}

// cleanIdleConnections 清理空闲连接
// 遍历连接池中的所有连接,关闭超时的空闲连接
// 维护连接池的健康状态
func (c *Client) cleanIdleConnections() {
	now := time.Now()
	// 遍历连接池中的所有连接
	c.connPool.Range(func(key, value interface{}) bool {
		conns := value.([]net.Conn)
		var active []net.Conn
		// 遍历连接列表,检查每个连接的状态
		for _, conn := range conns {
			// 如果连接状态存在,则检查连接是否超时
			if state, ok := c.activeConns.Load(conn); ok {
				// 如果连接未超时,则保留该连接
				if now.Sub(state.(*connState).lastActive) < c.config.IdleTimeout {
					// 将未超时的连接添加到活跃连接列表
					active = append(active, conn)
					continue
				}
			}
			conn.Close()
		}
		// 如果活跃连接列表不为空,则将连接列表存储到连接池
		if len(active) > 0 {
			c.connPool.Store(key, active)
		} else {
			// 如果活跃连接列表为空,则删除连接池中的连接
			c.connPool.Delete(key)
		}
		return true
	})
}

// GetMetrics 获取客户端指标
// 返回当前客户端的运行指标快照
// 用于监控和统计分析
//
// 返回值:
//   - ClientMetricsSnapshot: 客户端指标快照
func (c *Client) GetMetrics() ClientMetricsSnapshot {
	return ClientMetricsSnapshot{
		TotalRequests:  c.metrics.TotalRequests.Load(),
		FailedRequests: c.metrics.FailedRequests.Load(),
		ActiveRequests: c.metrics.ActiveRequests.Load(),
		BytesRead:      c.metrics.BytesRead.Load(),
		BytesWritten:   c.metrics.BytesWritten.Load(),
	}
}

// ClientMetricsSnapshot 定义客户端指标快照
// 记录了客户端运行时的各项统计数据
// 用于外部监控和分析
type ClientMetricsSnapshot struct {
	TotalRequests  int64 // 总请求数,记录所有已处理的请求数量
	FailedRequests int64 // 失败请求数,记录处理失败的请求数量
	ActiveRequests int32 // 活跃请求数,当前正在处理的请求数量
	BytesRead      int64 // 读取字节数,已接收的总字节数
	BytesWritten   int64 // 写入字节数,已发送的总字节数
}

// GetConnectionsInfo 获取当前连接信息
// 返回所有活跃连接的详细信息
// 用于连接状态监控和调试
//
// 返回值:
//   - []ConnectionInfo: 连接信息列表
func (c *Client) GetConnectionsInfo() []ConnectionInfo {
	var infos []ConnectionInfo
	// 遍历活跃连接映射
	c.activeConns.Range(func(key, value interface{}) bool {
		// 获取连接
		conn := key.(net.Conn)
		// 获取连接状态
		state := value.(*connState)
		// 创建连接信息
		info := ConnectionInfo{
			RemoteAddr:   conn.RemoteAddr().String(),   // 获取远程地址
			LastActive:   state.lastActive,             // 获取最后活跃时间
			IdleTime:     time.Since(state.lastActive), // 获取空闲时间
			RequestCount: state.requestCount.Load(),    // 获取请求计数
			CreatedAt:    state.createdAt,              // 获取创建时间
		}
		infos = append(infos, info)
		return true
	})
	return infos
}

// ConnectionInfo 定义连接信息结构
type ConnectionInfo struct {
	RemoteAddr   string        // 远程地址
	LastActive   time.Time     // 最后活跃时间
	IdleTime     time.Duration // 空闲时间
	RequestCount int32         // 请求计数
	CreatedAt    time.Time     // 创建时间
}

// writeMessage 使用统一的超时控制
func (c *Client) writeMessage(conn net.Conn, msg []byte) error {
	if err := c.setConnDeadlines(conn, 0, c.config.WriteTimeout); err != nil {
		return err
	}
	defer c.setConnDeadlines(conn, 0, 0)

	// 检查消息大小
	if len(msg) > c.config.MaxBlockSize {
		logger.Errorf("消息大小超过限制: %d > %d", len(msg), c.config.MaxBlockSize)
		return ErrMessageTooLarge
	}

	// 使用 msgio 写入消息
	writer := msgio.NewWriter(conn)
	return writer.WriteMsg(msg)
}

// setConnDeadlines 统一设置连接超时
func (c *Client) setConnDeadlines(conn net.Conn, readTimeout, writeTimeout time.Duration) error {
	if readTimeout > 0 {
		if err := conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
			return fmt.Errorf("设置读取超时失败: %w", err)
		}
	}

	if writeTimeout > 0 {
		if err := conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
			return fmt.Errorf("设置写入超时失败: %w", err)
		}
	}
	return nil
}
