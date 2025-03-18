package pointsub

import (
	"bufio"
	"io"
	"net"
	"time"
)

// LargeMessageOption 定义大消息连接选项
type LargeMessageOption func(*largeMessageOptions)

// largeMessageOptions 存储大消息连接的配置参数
type largeMessageOptions struct {
	chunkSize        int           // 分块大小
	adaptiveChunking bool          // 自适应块大小
	readBufferSize   int           // 读缓冲区大小
	writeBufferSize  int           // 写缓冲区大小
	noBlockDelay     bool          // 是否禁用块间延迟
	blockDelay       time.Duration // 块间延迟时间
}

// WithChunkSize 设置分块大小
func WithChunkSize(size int) LargeMessageOption {
	return func(o *largeMessageOptions) {
		o.chunkSize = size
	}
}

// WithAdaptiveChunking 设置是否使用自适应块大小
func WithAdaptiveChunking(adaptive bool) LargeMessageOption {
	return func(o *largeMessageOptions) {
		o.adaptiveChunking = adaptive
	}
}

// WithBufferSizes 设置读写缓冲区大小
func WithBufferSizes(readSize, writeSize int) LargeMessageOption {
	return func(o *largeMessageOptions) {
		o.readBufferSize = readSize
		o.writeBufferSize = writeSize
	}
}

// WithNoBlockDelay 禁用块间延迟
func WithNoBlockDelay() LargeMessageOption {
	return func(o *largeMessageOptions) {
		o.noBlockDelay = true
	}
}

// WithBlockDelay 设置块间延迟时间
func WithBlockDelay(delay time.Duration) LargeMessageOption {
	return func(o *largeMessageOptions) {
		o.blockDelay = delay
	}
}

// largeMessageConn 实现了针对大消息传输优化的连接
type largeMessageConn struct {
	net.Conn
	reader             *bufio.Reader
	writer             *bufio.Writer
	opts               largeMessageOptions
	readDeadline       time.Time
	writeDeadline      time.Time
	messageSize        int // 预期的消息大小，用于自适应块调整
	stats              *connStats
	readTimeoutActive  bool
	writeTimeoutActive bool
}

// connStats 存储连接统计信息
type connStats struct {
	chunksSent     int64
	chunksReceived int64
	bytesSent      int64
	bytesReceived  int64
	errors         int64
}

// NewLargeMessageConn 创建针对大消息传输优化的连接
func NewLargeMessageConn(conn net.Conn, estimatedMessageSize int, options ...LargeMessageOption) net.Conn {
	// 默认选项
	opts := largeMessageOptions{
		chunkSize:        DefaultMediumChunkSize,
		adaptiveChunking: true,
		readBufferSize:   DefaultReadBufferSize,
		writeBufferSize:  DefaultWriteBufferSize,
		noBlockDelay:     false,
		blockDelay:       time.Millisecond,
	}

	// 应用选项
	for _, opt := range options {
		opt(&opts)
	}

	// 根据消息大小调整缓冲区
	adjustBufferSizes(&opts, estimatedMessageSize)

	return &largeMessageConn{
		Conn:        conn,
		reader:      bufio.NewReaderSize(conn, opts.readBufferSize),
		writer:      bufio.NewWriterSize(conn, opts.writeBufferSize),
		opts:        opts,
		messageSize: estimatedMessageSize,
		stats:       &connStats{},
	}
}

// 根据消息大小调整缓冲区大小
func adjustBufferSizes(opts *largeMessageOptions, messageSize int) {
	// 自适应块大小
	if opts.adaptiveChunking {
		if messageSize > LargeMessageThreshold {
			opts.chunkSize = DefaultHugeChunkSize
		} else if messageSize > MediumMessageThreshold {
			opts.chunkSize = DefaultLargeChunkSize
		} else if messageSize > SmallMessageThreshold {
			opts.chunkSize = DefaultMediumChunkSize
		} else {
			opts.chunkSize = DefaultSmallChunkSize
		}
	}

	// 调整缓冲区大小
	idealBufferSize := messageSize / 8
	if idealBufferSize > MaxBufferSize {
		idealBufferSize = MaxBufferSize
	}

	if idealBufferSize > opts.readBufferSize {
		opts.readBufferSize = idealBufferSize
	}

	if idealBufferSize > opts.writeBufferSize {
		opts.writeBufferSize = idealBufferSize
	}
}

// Read 实现了优化的分块读取逻辑
func (c *largeMessageConn) Read(b []byte) (n int, err error) {
	// 设置超时（如果有）
	if !c.readDeadline.IsZero() && c.readTimeoutActive {
		c.Conn.SetReadDeadline(c.readDeadline)
		defer c.Conn.SetReadDeadline(time.Time{})
	}

	// 对于小缓冲区读取，直接使用底层读取器
	if len(b) < c.opts.chunkSize {
		n, err = c.reader.Read(b)
		if err == nil {
			c.stats.bytesReceived += int64(n)
			c.stats.chunksReceived++
		}
		return
	}

	// 对于大缓冲区读取，使用高效填充方式
	totalRead := 0
	for totalRead < len(b) {
		nr, er := c.reader.Read(b[totalRead:])
		if nr > 0 {
			totalRead += nr
			c.stats.bytesReceived += int64(nr)

			// 如果读取了完整块或到达缓冲区末尾，算作一个块
			if nr == c.opts.chunkSize || totalRead == len(b) {
				c.stats.chunksReceived++
			}
		}

		if er != nil {
			if er == io.EOF && totalRead > 0 {
				// 返回已读数据，下次调用时返回EOF
				return totalRead, nil
			}
			err = er
			break
		}

		// 如果缓冲区已满，退出循环
		if totalRead == len(b) {
			break
		}
	}

	if totalRead > 0 {
		return totalRead, err
	}

	return 0, err
}

// Write 实现了优化的分块写入逻辑
func (c *largeMessageConn) Write(b []byte) (n int, err error) {
	// 设置超时（如果有）
	if !c.writeDeadline.IsZero() && c.writeTimeoutActive {
		c.Conn.SetWriteDeadline(c.writeDeadline)
		defer c.Conn.SetWriteDeadline(time.Time{})
	}

	// 对于小数据，直接写入
	if len(b) < c.opts.chunkSize {
		n, err = c.writer.Write(b)
		if err == nil {
			err = c.writer.Flush()
			if err == nil {
				c.stats.bytesSent += int64(n)
				c.stats.chunksSent++
			}
		}
		return
	}

	// 对于大数据，采用分块写入
	totalWritten := 0
	chunkCount := 0
	totalChunks := (len(b) + c.opts.chunkSize - 1) / c.opts.chunkSize

	for totalWritten < len(b) {
		end := totalWritten + c.opts.chunkSize
		if end > len(b) {
			end = len(b)
		}

		chunk := b[totalWritten:end]
		nw, ew := c.writer.Write(chunk)

		if nw > 0 {
			totalWritten += nw
			chunkCount++

			// 只有在非最后一块时才flush，最后一次统一flush
			if chunkCount < totalChunks {
				if err := c.writer.Flush(); err != nil {
					return totalWritten, err
				}

				// 在块间添加短暂延迟，避免网络拥塞（如果未禁用）
				if !c.opts.noBlockDelay {
					time.Sleep(c.opts.blockDelay)
				}
			}
		}

		if ew != nil {
			err = ew
			break
		}

		// 如果所有数据已写入，退出循环
		if totalWritten == len(b) {
			break
		}
	}

	// 最后统一刷新
	if totalWritten > 0 && err == nil {
		err = c.writer.Flush()
		if err == nil {
			c.stats.bytesSent += int64(totalWritten)
			c.stats.chunksSent += int64(chunkCount)
		}
	}

	if totalWritten > 0 {
		return totalWritten, err
	}

	return 0, err
}

// SetReadDeadline 设置读取截止时间
func (c *largeMessageConn) SetReadDeadline(t time.Time) error {
	c.readDeadline = t
	c.readTimeoutActive = !t.IsZero()
	return nil
}

// SetWriteDeadline 设置写入截止时间
func (c *largeMessageConn) SetWriteDeadline(t time.Time) error {
	c.writeDeadline = t
	c.writeTimeoutActive = !t.IsZero()
	return nil
}

// Close 关闭连接并清理资源
func (c *largeMessageConn) Close() error {
	// 确保所有数据都刷新
	if err := c.writer.Flush(); err != nil {
		// 只记录错误，不返回，仍然尝试关闭连接
		c.stats.errors++
	}
	return c.Conn.Close()
}

// Stats 返回连接统计信息
func (c *largeMessageConn) Stats() *connStats {
	return c.stats
}

// NewLargeMessageTimeoutConn 创建具有超时管理和大消息优化的连接
func NewLargeMessageTimeoutConn(conn net.Conn, readTimeout, writeTimeout time.Duration,
	estimatedMessageSize int, options ...LargeMessageOption) net.Conn {

	// 先创建大消息连接
	lmConn := NewLargeMessageConn(conn, estimatedMessageSize, options...)

	// 然后添加超时管理
	return NewTimeoutConn(lmConn, readTimeout, writeTimeout)
}
