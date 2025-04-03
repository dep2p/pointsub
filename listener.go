package pointsub

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// ListenOption 定义监听器选项
type ListenOption func(*listenOptions)

type listenOptions struct {
	bufferSize               int           // 流缓冲区大小
	idleTimeout              time.Duration // 监听器空闲超时
	enableLargeMessageMode   bool          // 启用大消息模式
	largeMessageSize         int           // 预期大消息大小，用于自适应优化
	enableAdaptiveChunking   bool          // 启用自适应分块
	chunkSize                int           // 分块大小
	noBlockDelay             bool          // 是否禁用块间延迟
	blockDelay               time.Duration // 块间延迟
	customReadBufferSize     int           // 自定义读缓冲区大小
	customWriteBufferSize    int           // 自定义写缓冲区大小
	maxConcurrentConnections int           // 最大并发连接数
}

// WithBufferSize 设置流缓冲区大小
func WithBufferSize(size int) ListenOption {
	return func(o *listenOptions) {
		o.bufferSize = size
	}
}

// WithIdleTimeout 设置监听器空闲超时
func WithIdleTimeout(timeout time.Duration) ListenOption {
	return func(o *listenOptions) {
		o.idleTimeout = timeout
	}
}

// WithLargeMessageSupport 为监听器启用大消息支持
func WithLargeMessageSupport(expectedSize int) ListenOption {
	return func(o *listenOptions) {
		o.enableLargeMessageMode = true
		o.largeMessageSize = expectedSize
	}
}

// WithListenerAdaptiveChunking 为监听器启用自适应分块
func WithListenerAdaptiveChunking(enabled bool) ListenOption {
	return func(o *listenOptions) {
		o.enableAdaptiveChunking = enabled
	}
}

// WithListenerChunkSize 设置监听器的分块大小
func WithListenerChunkSize(size int) ListenOption {
	return func(o *listenOptions) {
		o.chunkSize = size
	}
}

// WithListenerNoBlockDelay 为监听器禁用块间延迟
func WithListenerNoBlockDelay() ListenOption {
	return func(o *listenOptions) {
		o.noBlockDelay = true
	}
}

// WithListenerBlockDelay 设置监听器的块间延迟
func WithListenerBlockDelay(delay time.Duration) ListenOption {
	return func(o *listenOptions) {
		o.blockDelay = delay
	}
}

// WithListenerBufferSizes 设置监听器的读写缓冲区大小
func WithListenerBufferSizes(readSize, writeSize int) ListenOption {
	return func(o *listenOptions) {
		o.customReadBufferSize = readSize
		o.customWriteBufferSize = writeSize
	}
}

// WithMaxConcurrentConnections 设置最大并发连接数
func WithMaxConcurrentConnections(max int) ListenOption {
	return func(o *listenOptions) {
		o.maxConcurrentConnections = max
	}
}

// listener 是 net.Listener 的实现，用于处理来自 libp2p 连接的标记流。
type listener struct {
	host     host.Host
	ctx      context.Context
	tag      protocol.ID
	cancel   func()
	streamCh chan network.Stream
	closedMu sync.RWMutex
	closed   bool
	// 监控
	stats *listenerStats
	// 监听器选项
	opts listenOptions
}

// 监听器统计
type listenerStats struct {
	accepted int64
	dropped  int64
	mu       sync.Mutex
}

// Accept 返回此监听器的下一个连接。
// 如果没有连接，它会阻塞。在底层，连接是 libp2p 流。
func (l *listener) Accept() (net.Conn, error) {
	l.closedMu.RLock()
	if l.closed {
		l.closedMu.RUnlock()
		return nil, net.ErrClosed
	}
	l.closedMu.RUnlock()

	select {
	case s := <-l.streamCh:
		if l.stats != nil {
			l.stats.mu.Lock()
			l.stats.accepted++
			l.stats.mu.Unlock()
		}

		// 创建基本连接
		conn := newConn(s)

		// 如果启用了大消息模式，应用大消息优化
		if l.opts.enableLargeMessageMode {
			// 准备大消息选项
			var largeOpts []LargeMessageOption

			// 自适应分块
			if l.opts.enableAdaptiveChunking {
				largeOpts = append(largeOpts, WithAdaptiveChunking(true))
			} else {
				largeOpts = append(largeOpts, WithAdaptiveChunking(false))
			}

			// 块大小
			if l.opts.chunkSize > 0 {
				largeOpts = append(largeOpts, WithChunkSize(l.opts.chunkSize))
			}

			// 块间延迟
			if l.opts.noBlockDelay {
				largeOpts = append(largeOpts, WithNoBlockDelay())
			} else if l.opts.blockDelay > 0 {
				largeOpts = append(largeOpts, WithBlockDelay(l.opts.blockDelay))
			}

			// 自定义缓冲区
			if l.opts.customReadBufferSize > 0 || l.opts.customWriteBufferSize > 0 {
				readSize := l.opts.customReadBufferSize
				if readSize <= 0 {
					readSize = DefaultReadBufferSize
				}
				writeSize := l.opts.customWriteBufferSize
				if writeSize <= 0 {
					writeSize = DefaultWriteBufferSize
				}
				largeOpts = append(largeOpts, WithBufferSizes(readSize, writeSize))
			}

			// 创建优化连接
			return NewLargeMessageConn(conn, l.opts.largeMessageSize, largeOpts...), nil
		}

		return conn, nil
	case <-l.ctx.Done():
		return nil, l.ctx.Err()
	}
}

// Close 终止此监听器。它将不再处理任何传入的流
func (l *listener) Close() error {
	l.closedMu.Lock()
	defer l.closedMu.Unlock()

	if l.closed {
		return nil
	}

	l.closed = true
	l.cancel()
	l.host.RemoveStreamHandler(l.tag)
	return nil
}

// Addr 返回监听器的地址
func (l *listener) Addr() net.Addr {
	return &addr{l.host.ID()}
}

// Stats 返回监听器的统计信息
func (l *listener) Stats() (accepted, dropped int64) {
	if l.stats == nil {
		return 0, 0
	}
	l.stats.mu.Lock()
	accepted = l.stats.accepted
	dropped = l.stats.dropped
	l.stats.mu.Unlock()
	return
}

// Listen 创建一个新的监听器，将为给定的协议标签处理传入的流
func Listen(h host.Host, tag protocol.ID) (net.Listener, error) {
	return ListenWithOptions(h, tag)
}

// ListenWithOptions 创建一个新的监听器，将为给定的协议标签处理传入的流，支持自定义选项
func ListenWithOptions(h host.Host, tag protocol.ID, opts ...ListenOption) (net.Listener, error) {
	// 处理选项
	options := listenOptions{
		bufferSize:             256,
		idleTimeout:            60 * time.Second,
		enableLargeMessageMode: false,
		enableAdaptiveChunking: true,
		chunkSize:              DefaultMediumChunkSize,
		noBlockDelay:           false,
		blockDelay:             time.Millisecond,
	}

	for _, opt := range opts {
		opt(&options)
	}

	ctx, cancel := context.WithCancel(context.Background())

	l := &listener{
		host:     h,
		ctx:      ctx,
		tag:      tag,
		cancel:   cancel,
		streamCh: make(chan network.Stream, options.bufferSize),
		stats:    &listenerStats{},
		opts:     options,
	}

	h.SetStreamHandler(tag, func(s network.Stream) {
		select {
		case l.streamCh <- s:
			// 流被接受
		case <-ctx.Done():
			// 监听器已关闭
			logger.Warnf("流被拒绝，监听器已关闭: %s", s.ID())
			s.Reset()
			if l.stats != nil {
				l.stats.mu.Lock()
				l.stats.dropped++
				l.stats.mu.Unlock()
			}
		default:
			if l.stats != nil {
				l.stats.mu.Lock()
				l.stats.dropped++
				l.stats.mu.Unlock()
			}
			// 没有消费者就拒绝流
			logger.Warnf("流被拒绝，缓冲区已满: %s", s.ID())
			s.Reset()
		}
	})

	// 如果设置了空闲超时，启动超时检查协程
	if options.idleTimeout > 0 {
		go func() {
			defer cancel()

			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()

			var lastAccepted, lastDropped int64
			lastActivity := time.Now()

			for {
				select {
				case <-ticker.C:
					// 检查是否有活动
					accepted, dropped := l.Stats()

					// 如果有新的接受或丢弃，更新最后活动时间
					if accepted > lastAccepted || dropped > lastDropped {
						lastAccepted = accepted
						lastDropped = dropped
						lastActivity = time.Now()
					} else if time.Since(lastActivity) > options.idleTimeout {
						// 空闲超时，关闭监听器
						logger.Infof("监听器空闲超时，关闭: %s", tag)
						_ = l.Close()
						return
					}
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	logger.Infof("监听器启动成功, 协议: %s, 缓冲区: %d", tag, options.bufferSize)
	return l, nil
}
