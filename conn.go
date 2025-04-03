package pointsub

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// conn 是 net.Conn 的实现，它包装了 libp2p 流。
type conn struct {
	network.Stream
}

// newConn 根据给定的 libp2p 流创建一个 conn
func newConn(s network.Stream) net.Conn {
	return &conn{s}
}

// LocalAddr 返回本地网络地址。
func (c *conn) LocalAddr() net.Addr {
	return &addr{c.Stream.Conn().LocalPeer()}
}

// RemoteAddr 返回远程网络地址。
func (c *conn) RemoteAddr() net.Addr {
	return &addr{c.Stream.Conn().RemotePeer()}
}

// Dial 使用给定的 host 打开一个到目标地址（应该可以解析为 peer ID）的流，并将其作为标准的 net.Conn 返回。
func Dial(ctx context.Context, h host.Host, pid peer.ID, tag protocol.ID) (net.Conn, error) {
	s, err := h.NewStream(ctx, pid, tag)
	if err != nil {
		logger.Errorf("创建流失败: %v", err)
		return nil, err
	}
	return newConn(s), nil
}

// DialOption 定义拨号选项
type DialOption func(*dialOptions)

type dialOptions struct {
	noDial         bool
	timeout        time.Duration
	resetRecovery  bool
	resetMaxRetry  int
	resetRetryWait time.Duration
	// 新增大消息优化选项
	largeMessage       bool
	estimatedMsgSize   int
	adaptiveChunking   bool
	chunkSize          int
	noBlockDelay       bool
	blockDelay         time.Duration
	customReadBufSize  int
	customWriteBufSize int
}

// WithNoDial 创建一个不需要新建连接的拨号选项
func WithNoDial() DialOption {
	return func(o *dialOptions) {
		o.noDial = true
	}
}

// WithTimeout 设置连接超时
func WithTimeout(timeout time.Duration) DialOption {
	return func(o *dialOptions) {
		o.timeout = timeout
	}
}

// WithResetRecovery 启用流重置自动恢复
func WithResetRecovery(maxRetry int, retryWait time.Duration) DialOption {
	return func(o *dialOptions) {
		o.resetRecovery = true
		o.resetMaxRetry = maxRetry
		o.resetRetryWait = retryWait
	}
}

// WithLargeMessage 启用大消息传输优化
func WithLargeMessage(estimatedSize int) DialOption {
	return func(o *dialOptions) {
		o.largeMessage = true
		o.estimatedMsgSize = estimatedSize
	}
}

// WithDialAdaptiveChunking 启用自适应块大小
func WithDialAdaptiveChunking(enabled bool) DialOption {
	return func(o *dialOptions) {
		o.adaptiveChunking = enabled
	}
}

// WithCustomChunkSize 设置自定义块大小
func WithCustomChunkSize(size int) DialOption {
	return func(o *dialOptions) {
		o.chunkSize = size
	}
}

// WithDialNoBlockDelay 禁用块间延迟
func WithDialNoBlockDelay() DialOption {
	return func(o *dialOptions) {
		o.noBlockDelay = true
	}
}

// WithCustomBlockDelay 设置自定义块间延迟
func WithCustomBlockDelay(delay time.Duration) DialOption {
	return func(o *dialOptions) {
		o.blockDelay = delay
	}
}

// WithCustomBufferSizes 设置自定义读写缓冲区大小
func WithCustomBufferSizes(readSize, writeSize int) DialOption {
	return func(o *dialOptions) {
		o.customReadBufSize = readSize
		o.customWriteBufSize = writeSize
	}
}

// DialWithOptions 使用选项打开到指定对等节点的流
func DialWithOptions(ctx context.Context, h host.Host, pid peer.ID, tag protocol.ID, opts ...DialOption) (net.Conn, error) {
	dopts := &dialOptions{
		timeout:          30 * time.Second,
		resetMaxRetry:    3,
		resetRetryWait:   time.Second,
		largeMessage:     false,
		estimatedMsgSize: 0,
		adaptiveChunking: true,
		chunkSize:        DefaultMediumChunkSize,
		noBlockDelay:     false,
		blockDelay:       time.Millisecond,
	}

	for _, opt := range opts {
		opt(dopts)
	}

	var streamCtx context.Context
	var cancel context.CancelFunc

	if dopts.timeout > 0 {
		streamCtx, cancel = context.WithTimeout(ctx, dopts.timeout)
		defer cancel()
	} else {
		streamCtx = ctx
	}

	if dopts.noDial {
		streamCtx = network.WithNoDial(streamCtx, "should already have connection")
	}

	s, err := h.NewStream(streamCtx, pid, tag)
	if err != nil {
		logger.Errorf("创建流失败: %v", err)
		return nil, err
	}

	// 创建基础连接
	nc := newConn(s)

	// 应用连接增强功能
	var enhancedConn net.Conn = nc

	// 如果需要大消息传输优化
	if dopts.largeMessage {
		// 转换为大消息选项
		var largeOpts []LargeMessageOption

		// 自适应分块
		if dopts.adaptiveChunking {
			largeOpts = append(largeOpts, WithAdaptiveChunking(true))
		} else {
			largeOpts = append(largeOpts, WithAdaptiveChunking(false))
		}

		// 自定义块大小
		if dopts.chunkSize > 0 {
			largeOpts = append(largeOpts, WithChunkSize(dopts.chunkSize))
		}

		// 块间延迟
		if dopts.noBlockDelay {
			largeOpts = append(largeOpts, WithNoBlockDelay())
		} else if dopts.blockDelay > 0 {
			largeOpts = append(largeOpts, WithBlockDelay(dopts.blockDelay))
		}

		// 自定义缓冲区大小
		if dopts.customReadBufSize > 0 || dopts.customWriteBufSize > 0 {
			readSize := dopts.customReadBufSize
			if readSize <= 0 {
				readSize = DefaultReadBufferSize
			}
			writeSize := dopts.customWriteBufSize
			if writeSize <= 0 {
				writeSize = DefaultWriteBufferSize
			}
			largeOpts = append(largeOpts, WithBufferSizes(readSize, writeSize))
		}

		enhancedConn = NewLargeMessageConn(enhancedConn, dopts.estimatedMsgSize, largeOpts...)
	}

	// 如果启用了重置恢复，则包装连接
	if dopts.resetRecovery {
		return &resetAwareConn{
			Conn:      enhancedConn, // 使用可能已增强的连接
			host:      h,
			pid:       pid,
			tag:       tag,
			maxRetry:  dopts.resetMaxRetry,
			retryWait: dopts.resetRetryWait,
			ctx:       ctx,
			dopts:     dopts, // 保存选项以便重连时重新应用
		}, nil
	}

	return enhancedConn, nil
}

// 检测常见的流重置错误
func isResetError(err error) bool {
	if err == nil {
		return false
	}

	// 使用连接状态判断
	closed, info := IsConnectionClosed(err)
	if closed {
		// 非优雅关闭的连接视为重置错误
		return !info.Graceful
	}

	// 保留原有的字符串匹配逻辑作为备份
	errStr := err.Error()
	resetPatterns := []string{
		"stream reset",
	}

	for _, pattern := range resetPatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	// 检查是否为网络错误（非超时）
	var netErr net.Error
	if errors.As(err, &netErr) && !netErr.Timeout() {
		return true
	}

	return false
}

// resetAwareConn 包装基本连接，增加重置恢复能力
type resetAwareConn struct {
	net.Conn
	host      host.Host
	pid       peer.ID
	tag       protocol.ID
	ctx       context.Context
	maxRetry  int
	retryWait time.Duration
	dopts     *dialOptions // 保存选项以便重连时重新应用
}

// Read 增强读取功能，自动处理流重置
func (r *resetAwareConn) Read(b []byte) (n int, err error) {
	n, err = r.Conn.Read(b)
	if err == nil {
		return n, nil
	}

	// 检查是否为连接关闭
	closed, info := IsConnectionClosed(err)
	if closed {
		if info.Graceful {
			logger.Errorf("优雅关闭: %v", info.Reason)
			// 优雅关闭（如EOF）时，直接返回，不需要重试
			return n, fmt.Errorf("connection closed: %s", info.Reason)
		}
		// 非优雅关闭，需要重试
	} else if !isResetError(err) {
		logger.Errorf("非连接关闭也非重置错误: %v", err)
		// 非连接关闭也非重置错误，直接返回
		return n, err
	}

	// 尝试重新建立流
	for i := 0; i < r.maxRetry; i++ {
		// 等待一段时间再重试
		time.Sleep(r.retryWait)

		s, rerr := r.host.NewStream(r.ctx, r.pid, r.tag)
		if rerr == nil {
			// 成功重新连接，创建基础连接
			newConn := newConn(s)

			// 恢复原有增强
			if r.dopts != nil && r.dopts.largeMessage {
				// 转换为大消息选项
				var largeOpts []LargeMessageOption

				// 自适应分块
				if r.dopts.adaptiveChunking {
					largeOpts = append(largeOpts, WithAdaptiveChunking(true))
				} else {
					largeOpts = append(largeOpts, WithAdaptiveChunking(false))
				}

				// 自定义块大小
				if r.dopts.chunkSize > 0 {
					largeOpts = append(largeOpts, WithChunkSize(r.dopts.chunkSize))
				}

				// 块间延迟
				if r.dopts.noBlockDelay {
					largeOpts = append(largeOpts, WithNoBlockDelay())
				} else if r.dopts.blockDelay > 0 {
					largeOpts = append(largeOpts, WithBlockDelay(r.dopts.blockDelay))
				}

				// 自定义缓冲区大小
				if r.dopts.customReadBufSize > 0 || r.dopts.customWriteBufSize > 0 {
					readSize := r.dopts.customReadBufSize
					if readSize <= 0 {
						readSize = DefaultReadBufferSize
					}
					writeSize := r.dopts.customWriteBufSize
					if writeSize <= 0 {
						writeSize = DefaultWriteBufferSize
					}
					largeOpts = append(largeOpts, WithBufferSizes(readSize, writeSize))
				}

				r.Conn = NewLargeMessageConn(newConn, r.dopts.estimatedMsgSize, largeOpts...)
			} else {
				r.Conn = newConn
			}

			return r.Conn.Read(b)
		}
	}

	return n, err
}

// Write 增强写入功能，自动处理流重置
func (r *resetAwareConn) Write(b []byte) (n int, err error) {
	n, err = r.Conn.Write(b)
	if err == nil {
		return n, nil
	}

	// 检查是否为连接关闭
	closed, info := IsConnectionClosed(err)
	if closed {
		if info.Graceful {
			logger.Errorf("优雅关闭: %v", info.Reason)
			// 优雅关闭时返回特定错误
			return n, fmt.Errorf("connection closed: %s", info.Reason)
		}
		// 非优雅关闭，需要重试
	} else if !isResetError(err) {
		logger.Errorf("非连接关闭也非重置错误: %v", err)
		// 非连接关闭也非重置错误，直接返回
		return n, err
	}

	// 尝试重新建立流
	for i := 0; i < r.maxRetry; i++ {
		time.Sleep(r.retryWait)

		s, rerr := r.host.NewStream(r.ctx, r.pid, r.tag)
		if rerr == nil {
			// 成功重新连接，创建基础连接
			newConn := newConn(s)

			// 恢复原有增强
			if r.dopts != nil && r.dopts.largeMessage {
				// 转换为大消息选项
				var largeOpts []LargeMessageOption

				// 自适应分块
				if r.dopts.adaptiveChunking {
					largeOpts = append(largeOpts, WithAdaptiveChunking(true))
				} else {
					largeOpts = append(largeOpts, WithAdaptiveChunking(false))
				}

				// 自定义块大小
				if r.dopts.chunkSize > 0 {
					largeOpts = append(largeOpts, WithChunkSize(r.dopts.chunkSize))
				}

				// 块间延迟
				if r.dopts.noBlockDelay {
					largeOpts = append(largeOpts, WithNoBlockDelay())
				} else if r.dopts.blockDelay > 0 {
					largeOpts = append(largeOpts, WithBlockDelay(r.dopts.blockDelay))
				}

				// 自定义缓冲区大小
				if r.dopts.customReadBufSize > 0 || r.dopts.customWriteBufSize > 0 {
					readSize := r.dopts.customReadBufSize
					if readSize <= 0 {
						readSize = DefaultReadBufferSize
					}
					writeSize := r.dopts.customWriteBufSize
					if writeSize <= 0 {
						writeSize = DefaultWriteBufferSize
					}
					largeOpts = append(largeOpts, WithBufferSizes(readSize, writeSize))
				}

				r.Conn = NewLargeMessageConn(newConn, r.dopts.estimatedMsgSize, largeOpts...)
			} else {
				r.Conn = newConn
			}

			// 重试写入
			return r.Conn.Write(b)
		}
	}

	return n, err
}
