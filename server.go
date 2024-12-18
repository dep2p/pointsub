// Package pointsub 提供了点对点订阅功能的实现
package pointsub

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dep2p/pointsub/logger"
	pool "github.com/libp2p/go-buffer-pool"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-msgio"
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

// DefaultServerConfig 返回默认的服务器配置
// 返回值:
//   - *ServerConfig: 包含默认值的配置对象
func DefaultServerConfig() *ServerConfig {
	return &ServerConfig{
		ReadTimeout:        30 * time.Second, // 默认读超时30秒
		WriteTimeout:       30 * time.Second, // 默认写超时30秒
		MaxBlockSize:       1 << 25,          // 默认最大块大小32MB
		MaxConcurrentConns: 1000,             // 默认最大并发连接1000
		ConnectTimeout:     5 * time.Second,  // 默认连接超时5秒
		MaxRequestsPerConn: 10000,            // 默认每连接最大请求10000
		IdleTimeout:        60 * time.Second, // 默认空闲超时60秒
		BufferPoolSize:     1024 * 1024,      // 默认缓冲池大小1MB
		EnableCompression:  true,             // 默认启用压缩
		CleanupInterval:    5 * time.Minute,  // 默认5分钟检查一次
		MaxIdleTime:        30 * time.Minute, // 默认30分钟无活动则清理
		MaxMemoryUsage:     1 << 30,          // 默认最大内存使用量1GB
	}
}

// Server 定义服务结构
// 包含服务器运行所需的各种组件
type Server struct {
	host          host.Host     // libp2p主机实例
	config        *ServerConfig // 服务器配置
	handlers      sync.Map      // 处理器映射,key为protocol.ID,value为StreamHandler
	listeners     sync.Map      // 监听器映射,key为protocol.ID,value为net.Listener
	connCount     int32         // 当前连接数
	pool          sync.Pool     // 内存池
	done          chan struct{} // 关闭信号通道
	closeOnce     sync.Once     // 确保只关闭一次
	mu            sync.RWMutex  // 读写锁
	activeConns   sync.Map      // 活跃连接映射
	cleanupTicker *time.Ticker  // 清理定时器
	cleanupMu     sync.RWMutex  // 清理锁
	poolSize      int32         // 当前池大小
	maxPoolSize   int32         // 最大池大小
	semaphore     chan struct{} // 并发控制信号量
}

// StreamHandler 定义了处理消息的函数类型
// 参数:
//   - request: 请求消息字节切片
//
// 返回值:
//   - []byte: 响应消息
//   - error: 错误信息
type StreamHandler func(request []byte) (response []byte, err error)

// NewServer 创建新的服务器实例
// 参数:
//   - h: libp2p 主机实例
//   - config: 服务器配置,如果为nil则使用默认配置
//
// 返回值:
//   - *Server: 新创建的服务器实例
//   - error: 错误信息
func NewServer(h host.Host, config *ServerConfig) (*Server, error) {
	// 如果配置为空,使用默认配置
	if config == nil {
		config = DefaultServerConfig()
	}

	// 验证配置有效性
	if err := config.Validate(); err != nil {
		logger.Errorf("配置验证失败: %v", err)
		return nil, errors.New("配置无效")
	}

	// 创建服务器实例
	s := &Server{
		host:   h,
		config: config,
		pool: sync.Pool{
			New: func() interface{} {
				b := make([]byte, 0, config.BufferPoolSize)
				return &b
			},
		},
		done:        make(chan struct{}),
		semaphore:   make(chan struct{}, config.MaxConcurrentConns),
		maxPoolSize: 1000,
	}
	return s, nil
}

// Start 启动服务器
// 参数:
//   - protocolID: 协议标识符
//   - handler: 消息处理函数
//
// 返回值:
//   - error: 错误信息
func (s *Server) Start(protocolID protocol.ID, handler StreamHandler) error {
	// 初始化清理定时器
	s.mu.Lock()
	if s.cleanupTicker == nil {
		s.cleanupTicker = time.NewTicker(s.config.CleanupInterval)
		go s.cleanupIdleConnections()
	}
	s.mu.Unlock()

	// 检查服务是否已关闭
	select {
	case <-s.done:
		s.cleanupTicker.Stop()
		return errors.New("服务器已关闭")
	default:
	}

	// 检查处理器是否已存在
	if _, exists := s.handlers.Load(protocolID); exists {
		logger.Errorf("协议 %s 已经注册", protocolID)
		return fmt.Errorf("协议 %s 已经注册", protocolID)
	}

	// 创建新的监听器
	listener, err := Listen(s.host, protocolID)
	if err != nil {
		logger.Errorf("为协议 %s 创建监听器时失败: %v", protocolID, err)
		return fmt.Errorf("为协议 %s 创建监听器时失败", protocolID)
	}

	// 保存监听器和处理器
	s.handlers.Store(protocolID, handler)
	s.listeners.Store(protocolID, listener)

	// 启动连接处理协程
	go s.acceptConnections(protocolID, listener)

	return nil
}

// Stop 停止服务器
// 返回值:
//   - error: 错误信息
func (s *Server) Stop() error {
	var stopErr error
	s.closeOnce.Do(func() {
		// 停止清理定时器
		s.cleanupMu.Lock()
		if s.cleanupTicker != nil {
			s.cleanupTicker.Stop()
		}
		s.cleanupMu.Unlock()

		// 关闭done通道
		close(s.done)

		// 等待所有活跃连接完成
		var wg sync.WaitGroup
		s.activeConns.Range(func(key, value interface{}) bool {
			if conn, ok := key.(net.Conn); ok {
				wg.Add(1)
				go func() {
					defer wg.Done()
					conn.Close()
				}()
			}
			s.activeConns.Delete(key)
			return true
		})
		wg.Wait() // 等待所有连接关闭
	})
	return stopErr
}

// acceptConnections 处理新的连接
// 参数:
//   - protocolID: 协议标识符
//   - listener: 网络监听器
func (s *Server) acceptConnections(protocolID protocol.ID, listener net.Listener) {
	backoff := time.Millisecond * 100 // 初始退避时间
	maxBackoff := time.Second * 5     // 最大退避时间

	for {
		select {
		case <-s.done:
			return
		default:
			// 接受新连接
			conn, err := listener.Accept()
			if err != nil {
				if !isTemporaryError(err) {
					logger.Errorf("协议 %s 停止接受连接: %v", protocolID, err)
					return
				}
				// 临时错误,使用退避策略
				time.Sleep(backoff)
				backoff = min(backoff*2, maxBackoff)
				continue
			}
			backoff = time.Millisecond * 100 // 重置退避时间

			// 检查是否可以接受新连接
			if !s.canAcceptConnection() {
				conn.Close()
				continue
			}

			// 获取对应协议的处理器
			handler, exists := s.handlers.Load(protocolID)
			if !exists {
				conn.Close()
				continue
			}

			// 启动连接处理协程
			go s.handleConnection(conn, handler.(StreamHandler))
		}
	}
}

// handleConnection 处理单个连接
// 参数:
//   - conn: 网络连接对象
//   - handler: 消息处理函数
func (s *Server) handleConnection(conn net.Conn, handler StreamHandler) {
	// 重置连接超时
	defer conn.SetDeadline(time.Time{})

	// 设置初始读取超时
	if err := conn.SetDeadline(time.Now().Add(s.config.ReadTimeout)); err != nil {
		logger.Errorf("设置连接超时失败: %v", err)
		return
	}

	// 获取并发控制信号量
	select {
	case s.semaphore <- struct{}{}:
		defer func() { <-s.semaphore }()
	default:
		conn.Close()
		return
	}

	// 包装连接添加大小限制
	conn = NewLimitedConn(conn, s.config.MaxBlockSize)
	s.activeConns.Store(conn, time.Now())
	atomic.AddInt32(&s.connCount, 1)

	// 创建消息读写器
	reader := msgio.NewReaderSize(conn, s.config.MaxBlockSize)
	defer reader.Close()

	writer := msgio.NewWriterWithPool(conn, pool.GlobalPool)

	// 获取缓冲区
	bufPtr := s.getBuffer()
	defer s.putBuffer(bufPtr)

	// 读取请求消息
	request, err := reader.ReadMsg()
	if err != nil {
		logger.Errorf("读取请求失败: %v", err)
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			logger.Warnf("读取超时: %v", err)
		} else if err == io.EOF {
			logger.Debugf("连接已关闭: %v", err)
		}
		return
	}
	defer reader.ReleaseMsg(request)

	// 处理请求
	response, err := handler(request)
	if err != nil {
		logger.Errorf("处理请求失败: %v", err)
		errorResponse := []byte(fmt.Sprintf("处理请求失败: %v", err))
		writer.WriteMsg(errorResponse)
		return
	}

	// 检查服务是否已关闭
	select {
	case <-s.done:
		return
	default:
		// 设置写入超时
		if err := conn.SetDeadline(time.Now().Add(s.config.WriteTimeout)); err != nil {
			logger.Errorf("设置写入超时失败: %v", err)
			return
		}
	}

	// 发送响应
	if err := writer.WriteMsg(response); err != nil {
		logger.Errorf("发送响应失败: %v", err)
		return
	}

	// 更新连接最后活跃时间
	s.activeConns.Store(conn, time.Now())
}

// canAcceptConnection 检查是否可以接受新连接
// 返回值:
//   - bool: 是否可以接受新连接
func (s *Server) canAcceptConnection() bool {
	return atomic.LoadInt32(&s.connCount) < int32(s.config.MaxConcurrentConns)
}

// 错误定义
var (
	ErrServerAlreadyStarted = errors.New("服务器已经启动") // 服务器已启动错误
)

// cleanupIdleConnections 清理空闲连接
func (s *Server) cleanupIdleConnections() {
	for {
		select {
		case <-s.done:
			return
		case <-s.cleanupTicker.C:
			// 获取当前时间
			now := time.Now()
			var closedCount int32

			// 遍历所有连接
			s.activeConns.Range(func(key, value interface{}) bool {
				conn := key.(net.Conn)
				lastActive := value.(time.Time)

				// 检查是否超过最大空闲时间
				if now.Sub(lastActive) > s.config.MaxIdleTime {
					// 关闭空闲连接
					conn.Close()
					s.activeConns.Delete(key)
					atomic.AddInt32(&s.connCount, -1)
					atomic.AddInt32(&closedCount, 1)
				}
				return true
			})

			// 记录清理日志
			if closedCount > 0 {
				logger.Infof("已清理 %d 个空闲连接", closedCount)
			}
		}
	}
}

// GetConnectionsInfo 获取当前活跃连接信息
// 返回值:
//   - []ConnectionInfo: 连接信息列表
func (s *Server) GetConnectionsInfo() []ConnectionInfo {
	var infos []ConnectionInfo
	s.activeConns.Range(func(key, value interface{}) bool {
		conn := key.(net.Conn)
		lastActive := value.(time.Time)
		infos = append(infos, ConnectionInfo{
			RemoteAddr: conn.RemoteAddr().String(),
			LastActive: lastActive,
			IdleTime:   time.Since(lastActive),
		})
		return true
	})
	return infos
}

// ConnectionInfo 连接信息结构体
type ConnectionInfo struct {
	RemoteAddr string        // 远程地址
	LastActive time.Time     // 最后活跃时间
	IdleTime   time.Duration // 空闲时间
}

// getBuffer 获取缓冲区
// 返回值:
//   - *[]byte: 缓冲区指针
func (s *Server) getBuffer() *[]byte {
	// 检查是否超过最大池大小
	if atomic.LoadInt32(&s.poolSize) >= s.maxPoolSize {
		b := make([]byte, 0, s.config.BufferPoolSize)
		return &b
	}
	atomic.AddInt32(&s.poolSize, 1)
	return s.pool.Get().(*[]byte)
}

// putBuffer 释放缓冲区
// 参数:
//   - buf: 要释放的缓冲区指针
func (s *Server) putBuffer(buf *[]byte) {
	// 检查缓冲区大小是否超过限制
	if cap(*buf) > s.config.BufferPoolSize {
		atomic.AddInt32(&s.poolSize, -1)
		return // 不回收过大的缓冲区
	}
	*buf = (*buf)[:0] // 重置切片长度
	s.pool.Put(buf)
}

// Validate 验证配置是否有效
// 返回值:
//   - error: 错误信息
func (c *ServerConfig) Validate() error {
	// 验证读取超时时间
	if c.ReadTimeout <= 0 {
		return errors.New("读取超时时间必须为正数")
	}
	// 验证写入超时时间
	if c.WriteTimeout <= 0 {
		return errors.New("写入超时时必须为正数")
	}
	// 验证最大块大小
	if c.MaxBlockSize <= 0 {
		return errors.New("最大块大小必须为正数")
	}
	if c.MaxBlockSize > 1<<30 { // 1GB
		return errors.New("最大块大小过大")
	}
	// ... 其他配置验证 ...
	return nil
}

// LimitedConn 包装了一个带有大小限制的连接
type LimitedConn struct {
	net.Conn     // 底层连接
	maxSize  int // 最大大小限制
}

// NewLimitedConn 创建一个新的限制大小的连接
// 参数:
//   - conn: 原始连接
//   - maxSize: 最大大小限制
//
// 返回值:
//   - net.Conn: 限制大小的连接
func NewLimitedConn(conn net.Conn, maxSize int) net.Conn {
	return &LimitedConn{
		Conn:    conn,
		maxSize: maxSize,
	}
}

// Read 读取数据
// 参数:
//   - b: 读取缓冲区
//
// 返回值:
//   - n: 读取的字节数
//   - err: 错误信息
func (c *LimitedConn) Read(b []byte) (n int, err error) {
	// 限制读取大小
	if len(b) > c.maxSize {
		b = b[:c.maxSize]
	}
	return c.Conn.Read(b)
}

// Write 写入数据
// 参数:
//   - b: 要写入的数据
//
// 返回值:
//   - n: 写入的字节数
//   - err: 错误信息
func (c *LimitedConn) Write(b []byte) (n int, err error) {
	// 检查数据大小是否超过限制
	if len(b) > c.maxSize {
		return 0, fmt.Errorf("数据大小超过限制: %d > %d", len(b), c.maxSize)
	}
	return c.Conn.Write(b)
}

// isTemporaryError 检查错误是否为临时性错误
// 参数:
//   - err: 要检查的错误
//
// 返回值:
//   - bool: 是否为临时性错误
func isTemporaryError(err error) bool {
	if netErr, ok := err.(net.Error); ok {
		// 明确检查超时和特定的网络错误类型
		return netErr.Timeout() ||
			errors.Is(err, net.ErrClosed) ||
			strings.Contains(err.Error(), "use of closed network connection")
	}
	return false
}
