// Package pointsub 提供了点对点订阅功能的实现
package pointsub

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"runtime/debug"

	"github.com/dep2p/go-dep2p/core/host"
	"github.com/dep2p/go-dep2p/core/protocol"
	pool "github.com/dep2p/go-dep2p/p2plib/buffer/pool"
	"github.com/dep2p/go-dep2p/p2plib/msgio"
)

// 错误定义
var (
	ErrServerClosed    = errors.New("服务器已关闭")    // 服务器已经关闭时返回的错误
	ErrConnMaxRequests = errors.New("连接达到最大请求数") // 单个连接达到最大请求数限制时返回的错误
	ErrMaxConns        = errors.New("达到最大并发连接数") // 达到最大并发连接数限制时返回的错误
	ErrMessageTooLarge = errors.New("消息大小超过限制")  // 消息大小超过配置的最大值时返回的错误
	ErrProtocolExists  = errors.New("协议已注册")     // 尝试注册已存在的协议时返回的错误
	ErrInvalidConfig   = errors.New("无效的配置")     // 配置参数无效时返回的错误
)

// ServerConfig 定义服务器配置
// 包含连接、并发、资源、内存和监控等相关配置项
type ServerConfig struct {
	// 连接相关配置
	ReadTimeout  time.Duration // 读取超时时间,控制单次读取操作的最大时间
	WriteTimeout time.Duration // 写入超时时间,控制单次写入操作的最大时间
	MaxBlockSize int           // 最大消息大小,限制单条消息的最大字节数

	// 并发控制
	MaxConcurrentConns int           // 最大并发连接数,限制同时处理的最大连接数
	ConnectTimeout     time.Duration // 连接建立超时时间,控制连接建立的最大等待时间

	// 资源控制
	MaxRequestsPerConn int           // 每个连接最大请求数,限制单个连接可处理的最大请求数
	IdleTimeout        time.Duration // 空闲连接超时时间,超过此时间的空闲连接将被关闭

	// 内存控制
	BufferPoolSize    int  // 缓冲池大小,控制内存池中单个缓冲区的大小
	EnableCompression bool // 是否启用压缩,控制是否对消息进行压缩处理

	// 连接清理相关配置
	CleanupInterval time.Duration // 清理检查间隔,定期检查并清理空闲连接的时间间隔
	MaxIdleTime     time.Duration // 最大空闲时间,连接允许的最大空闲时间

	// 告警相关配置
	MaxMemoryUsage uint64 // 最大内存使用量,限制服务器的最大内存使用量
}

// defaultServerConfig 返回默认的服务器配置
// 返回值:
//   - *ServerConfig: 包含默认值的配置对象
func defaultServerConfig() *ServerConfig {
	return &ServerConfig{
		ReadTimeout:        30 * time.Second, // 默认读取超时30秒
		WriteTimeout:       30 * time.Second, // 默认写入超时30秒
		MaxBlockSize:       1 << 25,          // 默认最大块大小32MB
		MaxConcurrentConns: 1000,             // 默认最大并发连接1000
		ConnectTimeout:     5 * time.Second,  // 默认连接超时5秒
		MaxRequestsPerConn: 10000,            // 默认每连接最大请求10000
		IdleTimeout:        60 * time.Second, // 默认空闲超时60秒
		BufferPoolSize:     1024 * 1024,      // 默认缓冲池大小1MB
		EnableCompression:  true,             // 默认启用压缩
		CleanupInterval:    5 * time.Minute,  // 默认清理间隔5分钟
		MaxIdleTime:        30 * time.Minute, // 默认最大空闲时间30分钟
		MaxMemoryUsage:     1 << 30,          // 默认最大内存使用1GB
	}
}

// ServerMetrics 定义服务器指标结构
type ServerMetrics struct {
	TotalRequests    int64 // 总请求数,记录服务器处理的总请求数
	FailedRequests   int64 // 失败请求数,记录处理失败的请求数
	ActiveRequests   int32 // 活跃请求数,当前正在处理的请求数
	TotalConnections int64 // 总连接数,记录服务器建立的总连接数
	BytesRead        int64 // 读取字节数,记录从连接读取的总字节数
	BytesWritten     int64 // 写入字节数,记录写入连接的总字节数
	ActiveConns      int32 // 当前活跃连接数,当前保持的连接数
}

// Server 定义服务结构
type Server struct {
	ctx           context.Context    // 上下文,用于控制服务器生命周期
	cancel        context.CancelFunc // 取消函数,用于停止服务器
	host          host.Host          // dep2p主机实例
	config        *ServerConfig      // 服务器配置
	handlers      sync.Map           // 协议处理器映射表
	listeners     sync.Map           // 协议监听器映射表
	activeConns   sync.Map           // 活动连接映射表 net.Conn -> connState
	connCount     atomic.Int32       // 当前连接计数器
	closeOnce     sync.Once          // 确保只关闭一次的同步原语
	cleanupTicker *time.Ticker       // 清理定时器
	pool          sync.Pool          // 缓冲区对象池
	poolSize      atomic.Int32       // 对象池大小计数器
	memoryUsage   atomic.Int64       // 当前内存使用量
	bufferChan    chan *[]byte       // 缓冲区通道,用于限制总数
	metrics       *serverMetrics     // 服务器指标
	loadFactor    atomic.Value       // 负载因子
	maxLoadFactor float64            // 最大负载因子
}

// serverMetrics 定义服务器指标
type serverMetrics struct {
	totalRequests    atomic.Int64 // 总请求数的原子计数器
	failedRequests   atomic.Int64 // 失败请求数的原子计数器
	activeRequests   atomic.Int32 // 活跃请求数的原子计数器
	totalConnections atomic.Int64 // 总连接数的原子计数器
	bytesRead        atomic.Int64 // 读取字节数的原子计数器
	bytesWritten     atomic.Int64 // 写入字节数的原子计数器
}

// StreamHandler 定义了处理消息的函数类型
// 参数:
//   - request: 请求消息字节切片
//
// 返回值:
//   - []byte: 响应消息
//   - error: 错误信息
type StreamHandler func(request []byte) (response []byte, err error)

// ServerOption 定义服务端选项函数类型
type ServerOption func(*Server) error

// WithMaxConcurrentConns 设置最大并发连接数
// 参数:
//   - max: 最大并发连接数
//
// 返回值:
//   - ServerOption: 返回一个服务端选项函数
func WithMaxConcurrentConns(max int) ServerOption {
	return func(s *Server) error {
		if max <= 0 {
			logger.Errorf("最大并发连接数必须大于0")
			return errors.New("最大并发连接数必须大于0")
		}
		s.config.MaxConcurrentConns = max
		return nil
	}
}

// WithServerReadTimeout 设置服务端读取超时
// 参数:
//   - timeout: 读取超时时间
//
// 返回值:
//   - ServerOption: 返回一个服务端选项函数
func WithServerReadTimeout(timeout time.Duration) ServerOption {
	return func(s *Server) error {
		if timeout <= 0 {
			logger.Errorf("读取超时时间必须大于0")
			return errors.New("读取超时时间必须大于0")
		}
		s.config.ReadTimeout = timeout
		return nil
	}
}

// WithServerWriteTimeout 设置服务端写入超时
// 参数:
//   - timeout: 写入超时时间
//
// 返回值:
//   - ServerOption: 返回一个服务端选项函数
func WithServerWriteTimeout(timeout time.Duration) ServerOption {
	return func(s *Server) error {
		if timeout <= 0 {
			logger.Errorf("写入超时时间必须大于0")
			return errors.New("写入超时时间必须大于0")
		}
		s.config.WriteTimeout = timeout
		return nil
	}
}

// WithServerBufferPoolSize 设置缓冲池大小
// 参数:
//   - size: 缓冲池大小
//
// 返回值:
//   - ServerOption: 返回一个服务端选项函数
func WithServerBufferPoolSize(size int) ServerOption {
	return func(s *Server) error {
		if size <= 0 {
			logger.Errorf("缓冲池大小必须大于0")
			return errors.New("缓冲池大小必须大于0")
		}
		s.config.BufferPoolSize = size
		return nil
	}
}

// WithServerCleanupInterval 设置清理间隔
// 参数:
//   - interval: 清理间隔时间
//
// 返回值:
//   - ServerOption: 返回一个服务端选项函数
func WithServerCleanupInterval(interval time.Duration) ServerOption {
	return func(s *Server) error {
		if interval <= 0 {
			logger.Errorf("清理间隔必须大于0")
			return errors.New("清理间隔必须大于0")
		}
		s.config.CleanupInterval = interval
		return nil
	}
}

// NewServer 创建新的服务端实例
// 参数:
//   - h: dep2p主机实例
//   - opts: 服务端选项
//
// 返回值:
//   - *Server: 新创建的服务端实例
//   - error: 错误信息
func NewServer(h host.Host, opts ...ServerOption) (*Server, error) {
	ctx, cancel := context.WithCancel(context.Background())

	server := &Server{
		ctx:           ctx,
		cancel:        cancel,
		host:          h,
		config:        defaultServerConfig(),
		metrics:       &serverMetrics{},
		bufferChan:    make(chan *[]byte, defaultServerConfig().MaxConcurrentConns),
		maxLoadFactor: 0.8,
		pool: sync.Pool{
			New: func() interface{} {
				b := make([]byte, 0, defaultServerConfig().BufferPoolSize)
				return &b
			},
		},
	}
	server.loadFactor.Store(float64(0))

	// 应用选项配置
	for _, opt := range opts {
		if err := opt(server); err != nil {
			cancel()
			logger.Errorf("应用选项失败: %v", err)
			return nil, err
		}
	}

	// 验证配置
	if err := server.config.Validate(); err != nil {
		cancel()
		logger.Errorf("配置验证失败: %v", err)
		return nil, err
	}

	// 启动健康检查和清理
	server.startCleanupLoop()
	server.startHealthCheck()

	return server, nil
}

// Start 启动服务器
// 参数:
//   - protocolID: 协议标识符
//   - handler: 消息处理函数
//
// 返回值:
//   - error: 错误信息
func (s *Server) Start(protocolID protocol.ID, handler StreamHandler) error {
	select {
	case <-s.ctx.Done():
		logger.Errorf("服务器已关闭")
		return errors.New("服务器已关闭")
	default:
	}

	if _, exists := s.handlers.Load(protocolID); exists {
		logger.Errorf("协议 %s 已经注册", protocolID)
		return fmt.Errorf("协议 %s 已经注册", protocolID)
	}

	listener, err := Listen(s.host, protocolID)
	if err != nil {
		logger.Errorf("创建监听器失败: %v", err)
		return err
	}

	s.handlers.Store(protocolID, handler)
	s.listeners.Store(protocolID, listener)

	go s.acceptConnections(protocolID, listener)

	return nil
}

// Stop 停止服务器
func (s *Server) Stop() {
	s.closeOnce.Do(func() {
		s.cancel()

		// 停止清理定时器
		if s.cleanupTicker != nil {
			s.cleanupTicker.Stop()
		}

		// 关闭所有监听器
		s.listeners.Range(func(key, value interface{}) bool {
			if listener, ok := value.(net.Listener); ok {
				listener.Close()
			}
			return true
		})

		// 关闭所有连接
		s.activeConns.Range(func(key, value interface{}) bool {
			if conn, ok := key.(net.Conn); ok {
				conn.Close()
			}
			return true
		})
	})
}

// acceptConnections 接受并处理新的连接
// 参数:
//   - protocolID: 协议标识符
//   - listener: 网络监听器
func (s *Server) acceptConnections(protocolID protocol.ID, listener net.Listener) {
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
			// 接受连接
			conn, err := listener.Accept()
			if err != nil {
				// 如果错误是关闭的网络连接错误,则停止接受连接
				if strings.Contains(err.Error(), "use of closed network connection") {
					return
				}
				// logger.Errorf("接受连接失败: %v", err)
				continue
			}

			// 如果连接数达到最大并发连接数限制,则关闭连接并继续
			if s.connCount.Load() >= int32(s.config.MaxConcurrentConns) {
				logger.Warn("达到最大并发连接数限制")
				conn.Close()
				continue
			}

			// 增加连接计数
			s.connCount.Add(1)
			// 处理连接
			go s.handleConnection(protocolID, conn)
		}
	}
}

// handleConnection 处理单个连接
// 参数:
//   - protocolID: 协议标识符
//   - conn: 网络连接
func (s *Server) handleConnection(protocolID protocol.ID, conn net.Conn) {
	defer func() {
		s.connCount.Add(-1)
		s.updateLoadFactor() // 更新负载因子
		s.metrics.totalConnections.Add(1)
		conn.Close()
		s.activeConns.Delete(conn)
		// 如果发生panic,则记录错误信息
		if r := recover(); r != nil {
			logger.Errorf("连接处理panic: %v\n%s", r, debug.Stack())
		}
	}()

	// 更新负载因子
	s.updateLoadFactor()

	// 获取协议处理器
	handler, ok := s.handlers.Load(protocolID)
	if !ok {
		logger.Errorf("协议 %s 未注册处理器", protocolID)
		return
	}

	// 获取协议处理器
	streamHandler, ok := handler.(StreamHandler)
	if !ok {
		logger.Error("处理器类型错误")
		return
	}

	// 使用带缓冲的读写器
	reader := msgio.NewReaderWithPool(conn, pool.GlobalPool)
	writer := msgio.NewWriterWithPool(conn, pool.GlobalPool)
	defer func() {
		reader.Close()
		writer.Close()
	}()

	// 创建连接状态
	state := &connState{
		lastActive: time.Now(),
		createdAt:  time.Now(),
		addr:       conn.RemoteAddr().String(),
	}
	s.activeConns.Store(conn, state)

	// 获取缓冲区
	buf := s.getBuffer()
	defer s.putBuffer(buf)

	// 设置最大重试次数
	const maxRetries = 3
	// 初始化重试计数器
	retryCount := 0
	// 创建指数退避对象
	backoff := &exponentialBackoff{
		initial: 100 * time.Millisecond,
		max:     5 * time.Second,
		factor:  2,
		jitter:  0.1,
	}

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
			// 如果请求计数达到最大请求数限制,则关闭连接并返回
			if state.requestCount.Load() >= int32(s.config.MaxRequestsPerConn) {
				logger.Warnf("连接 %s 达到最大请求数限制", state.addr)
				return
			}

			// 清空缓冲区
			*buf = (*buf)[:0]
			// 处理请求
			err := s.processRequest(conn, reader, writer, streamHandler, buf, state)
			// 如果处理请求失败,则进行重试
			if err != nil {
				// 如果错误是临时性错误,则进行重试
				if isTemporaryError(err) && retryCount < maxRetries {
					retryCount++
					// 计算延迟时间
					delay := backoff.nextBackoff()
					logger.Debugf("重试请求 %d/%d: %v, 延迟: %v",
						retryCount, maxRetries, err, delay)
					time.Sleep(delay)
					continue
				}

				// 如果错误不是预期错误,则记录错误信息
				if !isExpectedError(err) {
					logger.Errorf("请求处理错误: %v, 连接: %s", err, state.addr)
					s.metrics.failedRequests.Add(1)
				}
				return
			}

			// 重置重试计数器和退避对象
			retryCount = 0
			backoff.reset()
			// 增加请求计数
			s.metrics.totalRequests.Add(1)
		}
	}
}

// processRequest 处理单个请求
// 参数:
//   - conn: 网络连接
//   - reader: 消息读取器
//   - writer: 消息写入器
//   - handler: 消息处理函数
//   - buf: 缓冲区
//   - state: 连接状态
//
// 返回值:
//   - error: 错误信息
func (s *Server) processRequest(
	conn net.Conn,
	reader msgio.ReadCloser,
	writer msgio.WriteCloser,
	handler StreamHandler,
	buf *[]byte,
	state *connState,
) error {
	s.metrics.activeRequests.Add(1)
	defer s.metrics.activeRequests.Add(-1)

	// 设置读取超时
	if err := conn.SetReadDeadline(time.Now().Add(s.config.ReadTimeout)); err != nil {
		logger.Errorf("设置读取超时失败: %v", err)
		return err
	}

	// 读取消息
	msg, err := reader.ReadMsg()
	if err != nil {
		// 如果错误是意外EOF,则记录错误信息
		if err == io.ErrUnexpectedEOF {
			logger.Errorf("连接意外断开: %v", err)
			return err
		}
		// 如果错误是超时错误,则记录错误信息
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			logger.Errorf("读取超时: %v", err)
			return err
		}
		return err
	}
	s.metrics.bytesRead.Add(int64(len(msg)))

	// 重置读取超时
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		logger.Warnf("重置读取超时失败: %v", err)
	}

	// 检查消息大小
	if len(msg) > s.config.MaxBlockSize {
		logger.Errorf("消息太大: %v", len(msg))
		return ErrMessageTooLarge
	}

	// 将消息追加到缓冲区
	*buf = append(*buf, msg...)

	// 处理请求
	response, err := handler(*buf)
	if err != nil {
		logger.Errorf("请求处理失败: %v", err)
		return err
	}

	// 设置写入超时
	if err := conn.SetWriteDeadline(time.Now().Add(s.config.WriteTimeout)); err != nil {
		logger.Errorf("设置写入超时失败: %v", err)
		return err
	}

	// 写入响应
	if err := writer.WriteMsg(response); err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			logger.Errorf("写入超时: %v", err)
			return err
		}
		logger.Errorf("写入响应失败: %v", err)
		return err
	}
	// 增加写入字节数
	s.metrics.bytesWritten.Add(int64(len(response)))

	// 重置写入超时
	if err := conn.SetWriteDeadline(time.Time{}); err != nil {
		logger.Warnf("重置写入超时失败: %v", err)
	}

	state.lastActive = time.Now()
	state.requestCount.Add(1)

	return nil
}

// exponentialBackoff 实现指数退避算法
type exponentialBackoff struct {
	initial time.Duration // 初始退避时间
	max     time.Duration // 最大退避时间
	factor  float64       // 退避因子
	jitter  float64       // 抖动因子
	current time.Duration // 当前退避时间
	rand    *rand.Rand    // 随机数生成器
}

// nextBackoff 计算下一个退避时间
// 返回值:
//   - time.Duration: 下一个退避时间
func (b *exponentialBackoff) nextBackoff() time.Duration {
	// 如果当前退避时间为0,则设置为初始退避时间
	if b.current == 0 {
		b.current = b.initial
	} else {
		b.current = time.Duration(float64(b.current) * b.factor)
	}

	// 如果当前退避时间超过最大退避时间,则设置为最大退避时间
	if b.current > b.max {
		b.current = b.max
	}

	// 添加抖动
	jitterRange := float64(b.current) * b.jitter
	jitter := b.rand.Float64() * jitterRange
	return b.current + time.Duration(jitter)
}

// reset 重置退避时间
func (b *exponentialBackoff) reset() {
	b.current = 0
}

// startHealthCheck 启动健康检查
func (s *Server) startHealthCheck() {
	ticker := time.NewTicker(30 * time.Second)
	go func() {
		for {
			select {
			case <-s.ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				// 执行健康检查
				s.checkHealth()
			}
		}
	}()
}

// checkHealth 执行健康检查
func (s *Server) checkHealth() {
	// 获取活动连接数
	activeConns := s.connCount.Load()
	// 获取活动请求数
	activeRequests := s.metrics.activeRequests.Load()
	// 获取失败率
	failRate := float64(s.metrics.failedRequests.Load()) /
		float64(s.metrics.totalRequests.Load())

	// 记录健康检查信息
	logger.Infof("健康检查 - 活动连接: %d, 活动请求: %d, 失败率: %.2f%%",
		activeConns, activeRequests, failRate*100)

	// 检查关键指标
	if failRate > 0.1 { // 失败率超过10%
		logger.Warnf("失败率过高: %.2f%%", failRate*100)
	}

	if activeConns > int32(s.config.MaxConcurrentConns*80/100) { // 连接数超过80%
		logger.Warnf("连接数接近上限: %d/%d",
			activeConns, s.config.MaxConcurrentConns)
	}
}

// GetMetrics 获取服务器指标
// 返回值:
//   - ServerMetrics: 服务器指标
func (s *Server) GetMetrics() ServerMetrics {
	return ServerMetrics{
		TotalRequests:    s.metrics.totalRequests.Load(),
		FailedRequests:   s.metrics.failedRequests.Load(),
		ActiveRequests:   s.metrics.activeRequests.Load(),
		TotalConnections: s.metrics.totalConnections.Load(),
		BytesRead:        s.metrics.bytesRead.Load(),
		BytesWritten:     s.metrics.bytesWritten.Load(),
		ActiveConns:      s.connCount.Load(),
	}
}

// cleanupIdleConnections 清理空闲连接
func (s *Server) cleanupIdleConnections() {
	now := time.Now()
	var closedCount int32

	s.activeConns.Range(func(key, value interface{}) bool {
		conn := key.(net.Conn)
		state := value.(*connState)

		if now.Sub(state.lastActive) > s.config.MaxIdleTime {
			logger.Debugf("清理空闲连接: %s, 空闲时间: %v",
				conn.RemoteAddr(), now.Sub(state.lastActive))
			conn.Close()
			s.activeConns.Delete(key)
			s.connCount.Add(-1)
			atomic.AddInt32(&closedCount, 1)
		}
		return true
	})

	if closedCount > 0 {
		logger.Infof("已清理 %d 个空闲连接", closedCount)
	}
}

// getBuffer 从对象池获取或创建新的缓冲区
// 参数:
//   - s: 服务器实例
//
// 返回值:
//   - *[]byte: 缓冲区
func (s *Server) getBuffer() *[]byte {
	// 检查内存使用量是否超过限制
	if s.memoryUsage.Load() >= int64(s.config.MaxMemoryUsage) {
		// 等待有缓冲区被释放
		select {
		case buf := <-s.bufferChan:
			return buf
		case <-time.After(100 * time.Millisecond):
			logger.Warn("等待缓冲区超时")
			return nil
		}
	}

	// 尝试从对象池获取
	if buf := s.pool.Get(); buf != nil {
		s.poolSize.Add(1)
		s.memoryUsage.Add(int64(s.config.BufferPoolSize))
		return buf.(*[]byte)
	}

	// 创建新的缓冲区
	if s.poolSize.Load() < int32(s.config.MaxConcurrentConns) {
		b := make([]byte, 0, s.config.BufferPoolSize)
		s.poolSize.Add(1)
		s.memoryUsage.Add(int64(s.config.BufferPoolSize))
		s.bufferChan <- &b
		return &b
	}

	return nil
}

// putBuffer 将缓冲区放回对象池
// 参数:
//   - buf: 要放回的缓冲区
func (s *Server) putBuffer(buf *[]byte) {
	if buf == nil {
		return
	}
	// 重置缓冲区
	*buf = (*buf)[:0]

	// 更新计数
	s.memoryUsage.Add(-int64(s.config.BufferPoolSize))
	s.poolSize.Add(-1)

	// 放回对象池
	s.pool.Put(buf)

	// 从通道中移除
	select {
	case <-s.bufferChan:
	default:
	}
}

// isTemporaryError 检查错误是否为临时性错误
// 参数:
//   - err: 要检查的错误
//
// 返回值:
//   - bool: 是否为临时性错误
func isTemporaryError(err error) bool {
	// 如果错误是网络错误,则检查是否为超时错误或连接重置错误
	if netErr, ok := err.(net.Error); ok {
		return netErr.Timeout() ||
			// 如果错误包含"connection reset"、"broken pipe"或"connection refused"
			strings.Contains(err.Error(), "connection reset") ||
			strings.Contains(err.Error(), "broken pipe") ||
			strings.Contains(err.Error(), "connection refused")
	}
	return false
}

// startCleanupLoop 启动连接清理循环
func (s *Server) startCleanupLoop() {
	// 创建清理定时器
	s.cleanupTicker = time.NewTicker(s.config.CleanupInterval)
	// 启动清理循环
	go func() {
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-s.cleanupTicker.C:
				// 执行清理操作
				s.cleanupIdleConnections()
			}
		}
	}()
}

// GetConnectionsInfo 获取当前连接信息
// 返回值:
//   - []ConnectionInfo: 连接信息列表
func (s *Server) GetConnectionsInfo() []ConnectionInfo {
	var infos []ConnectionInfo
	// 遍历活跃连接映射
	s.activeConns.Range(func(key, value interface{}) bool {
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
		// 将连接信息添加到列表
		infos = append(infos, info)
		return true
	})
	// 返回连接信息列表
	return infos
}

// Validate 验证配置是否有效
// 返回值:
//   - error: 错误信息
func (c *ServerConfig) Validate() error {
	if c.ReadTimeout <= 0 {
		return errors.New("读取超时时间必须为正数")
	}
	if c.WriteTimeout <= 0 {
		return errors.New("写入超时时必须为正数")
	}
	if c.MaxBlockSize <= 0 {
		return errors.New("最大块大小必须为正数")
	}
	if c.MaxBlockSize > 1<<30 {
		return errors.New("最大块大小过大")
	}
	if c.MaxConcurrentConns <= 0 {
		return errors.New("最大并发连接数必须为正数")
	}
	if c.BufferPoolSize <= 0 {
		return errors.New("缓冲池大小必须为正数")
	}
	if c.CleanupInterval <= 0 {
		return errors.New("清理间隔必须为正数")
	}
	if c.MaxIdleTime <= 0 {
		return errors.New("最大空闲时间必须为正数")
	}
	return nil
}

// Accept 接受新连接
// 参数:
//   - protocolID: 协议ID
//
// 返回值:
//   - net.Conn: 新连接
//   - error: 错误信息
func (s *Server) Accept(protocolID protocol.ID) (net.Conn, error) {
	// 从 listeners map 获取对应协议的监听器
	l, ok := s.listeners.Load(protocolID)
	if !ok {
		// 如果未找到监听器,则返回错误
		return nil, fmt.Errorf("未找到协议 %s 的监听器", protocolID)
	}

	// 获取监听器
	listener := l.(net.Listener)
	// 循环接受连接
	for {
		conn, err := listener.Accept()
		if err != nil {
			logger.Errorf("接受连接失败: %v", err)
			return nil, err
		}

		// 如果连接数达到最大并发连接数,则关闭连接
		if s.connCount.Load() >= int32(s.config.MaxConcurrentConns) {
			conn.Close()
			continue
		}

		// 增加连接计数
		s.connCount.Add(1)
		// 返回连接
		return conn, nil
	}
}

// connState 定义连接状态
type connState struct {
	lastActive   time.Time    // 最后活跃时间
	requestCount atomic.Int32 // 请求计数
	createdAt    time.Time    // 创建时间
	addr         string       // 远程地址
}

// 添加常量定义
const (
	MinConcurrentConns = 100   // 最小并发连接数
	MaxConcurrentConns = 10000 // 最大并发连接数
	ConnGrowthRate     = 0.1   // 连接数增长率(10%)
)

// adjustMaxConns 动态调整最大连接数
func (s *Server) adjustMaxConns() {
	currentLoad := s.loadFactor.Load().(float64)
	if currentLoad > s.maxLoadFactor {
		// 减少最大连接数
		newMax := int(float64(s.config.MaxConcurrentConns) * 0.9) // 减少10%
		if newMax >= MinConcurrentConns {
			s.config.MaxConcurrentConns = newMax
			logger.Warnf("降低最大连接数至: %d", newMax)
		}
	} else if currentLoad < s.maxLoadFactor*0.5 {
		// 增加最大连接数，但有上限控制
		newMax := int(float64(s.config.MaxConcurrentConns) * (1 + ConnGrowthRate))
		if newMax <= MaxConcurrentConns {
			s.config.MaxConcurrentConns = newMax
			logger.Warnf("提高最大连接数至: %d", newMax)
		} else {
			logger.Warnf("已达到最大连接数上限: %d", MaxConcurrentConns)
		}
	}
}

// updateLoadFactor 更新负载因子
func (s *Server) updateLoadFactor() {
	activeConns := float64(s.connCount.Load())
	maxConns := float64(s.config.MaxConcurrentConns)

	// 确保不会除以0
	if maxConns <= 0 {
		maxConns = float64(MinConcurrentConns)
	}

	// 计算新的负载因子
	newLoadFactor := activeConns / maxConns
	s.loadFactor.Store(newLoadFactor)

	// 调整最大连接数
	s.adjustMaxConns()
}
