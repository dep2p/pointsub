package pointsub

import (
	"io"
	"net"
	"time"
)

// RetryableError 表示可以重试的错误
type RetryableError struct {
	Err error
}

// Error 实现error接口
func (r *RetryableError) Error() string {
	return "可重试错误: " + r.Err.Error()
}

// Unwrap 返回原始错误
func (r *RetryableError) Unwrap() error {
	return r.Err
}

// MessageTransporter 提供统一的消息传输接口
type MessageTransporter interface {
	// 发送任意大小的消息
	Send(data []byte) error

	// 接收消息
	Receive() ([]byte, error)

	// 发送流式数据
	SendStream(reader io.Reader) error

	// 接收流式数据
	ReceiveStream(writer io.Writer) error

	// 关闭连接
	Close() error

	// 设置读写截止时间
	SetDeadline(t time.Time) error

	// 设置读取截止时间
	SetReadDeadline(t time.Time) error

	// 设置写入截止时间
	SetWriteDeadline(t time.Time) error
}

// 基本的MessageTransporter实现，基于网络连接
type baseMessageTransporter struct {
	conn            net.Conn
	messageSizer    MessageSizer
	frameProcessor  FrameProcessor
	errorHandler    ErrorHandler
	bufferManager   AdaptiveBuffer
	progressTracker ProgressTracker
}

// MessageTransporterOption 定义MessageTransporter的配置选项
type MessageTransporterOption func(*messageTransporterOptions)

// messageTransporterOptions 保存MessageTransporter的配置参数
type messageTransporterOptions struct {
	// 消息大小检测器
	sizer MessageSizer

	// 帧处理器
	frameProc FrameProcessor

	// 错误处理器
	errHandler ErrorHandler

	// 缓冲区管理器
	buffer AdaptiveBuffer

	// 进度跟踪器
	tracker ProgressTracker

	// 最大消息大小限制 (0表示无限制)
	maxMessageSize int64

	// 是否启用校验和
	enableChecksum bool

	// 启用进度跟踪（仅对大消息有效）
	enableProgressTracking bool
}

// WithMessageSizer 配置消息大小检测器
func WithMessageSizer(sizer MessageSizer) MessageTransporterOption {
	return func(o *messageTransporterOptions) {
		o.sizer = sizer
	}
}

// WithFrameProcessor 配置帧处理器
func WithFrameProcessor(frameProc FrameProcessor) MessageTransporterOption {
	return func(o *messageTransporterOptions) {
		o.frameProc = frameProc
	}
}

// WithErrorHandler 配置错误处理器
func WithErrorHandler(errHandler ErrorHandler) MessageTransporterOption {
	return func(o *messageTransporterOptions) {
		o.errHandler = errHandler
	}
}

// WithAdaptiveBuffer 配置缓冲区管理器
func WithAdaptiveBuffer(buffer AdaptiveBuffer) MessageTransporterOption {
	return func(o *messageTransporterOptions) {
		o.buffer = buffer
	}
}

// WithProgressTracker 配置进度跟踪器
func WithProgressTracker(tracker ProgressTracker) MessageTransporterOption {
	return func(o *messageTransporterOptions) {
		o.tracker = tracker
		o.enableProgressTracking = true
	}
}

// WithMaxMessageSize 设置最大消息大小限制
func WithMaxMessageSize(maxSize int64) MessageTransporterOption {
	return func(o *messageTransporterOptions) {
		o.maxMessageSize = maxSize
	}
}

// WithChecksumEnabled 启用或禁用校验和
func WithChecksumEnabled(enabled bool) MessageTransporterOption {
	return func(o *messageTransporterOptions) {
		o.enableChecksum = enabled
	}
}

// NewMessageTransporter 创建新的消息传输器
func NewMessageTransporter(conn net.Conn, options ...MessageTransporterOption) MessageTransporter {
	// 默认选项
	opts := &messageTransporterOptions{
		enableChecksum:         true,
		maxMessageSize:         0, // 无限制
		enableProgressTracking: false,
	}

	// 应用选项
	for _, opt := range options {
		opt(opts)
	}

	// 创建默认组件（如果未指定）
	if opts.sizer == nil {
		opts.sizer = NewDefaultMessageSizer()
	}

	if opts.frameProc == nil {
		opts.frameProc = NewDefaultFrameProcessor()
	}

	if opts.errHandler == nil {
		opts.errHandler = NewDefaultErrorHandler()
	}

	if opts.buffer == nil {
		opts.buffer = NewDefaultAdaptiveBuffer()
	}

	var tracker ProgressTracker
	if opts.enableProgressTracking {
		if opts.tracker != nil {
			tracker = opts.tracker
		} else {
			tracker = NewProgressTracker()
		}
	}

	return &baseMessageTransporter{
		conn:            conn,
		messageSizer:    opts.sizer,
		frameProcessor:  opts.frameProc,
		errorHandler:    opts.errHandler,
		bufferManager:   opts.buffer,
		progressTracker: tracker,
	}
}

// Send 实现了MessageTransporter接口的Send方法
func (t *baseMessageTransporter) Send(data []byte) error {
	// 创建传输ID（用于进度跟踪）
	transferID := ""
	if t.progressTracker != nil && len(data) > LargeMessageThreshold {
		transferID = generateTransferID()
		t.progressTracker.StartTracking(transferID, int64(len(data)))
		defer t.progressTracker.StopTracking(transferID)
	}

	// 使用帧处理器直接写入数据
	err := t.frameProcessor.WriteFrame(t.conn, data, FlagNormal)

	// 处理错误
	if err != nil {
		if t.progressTracker != nil && transferID != "" {
			t.progressTracker.MarkFailed(transferID, err)
		}
		return t.handleError(err)
	}

	// 更新完成状态
	if t.progressTracker != nil && transferID != "" {
		t.progressTracker.UpdateProgress(transferID, int64(len(data)))
		t.progressTracker.UpdateStatus(transferID, StatusCompleted)
	}

	return nil
}

// Receive 实现了MessageTransporter接口的Receive方法
func (t *baseMessageTransporter) Receive() ([]byte, error) {
	// 使用帧处理器读取完整帧，而不是先读取样本
	data, err := t.frameProcessor.ReadFrame(t.conn)
	if err != nil {
		return nil, t.handleError(err)
	}

	// 验证数据完整性（如果有校验和）
	// 这里我们假设校验和已由ReadFrame内部处理

	// 如果消息很大且配置了进度跟踪，则更新进度
	if t.progressTracker != nil && len(data) > LargeMessageThreshold {
		transferID := generateTransferID()
		t.progressTracker.StartTracking(transferID, int64(len(data)))
		t.progressTracker.UpdateProgress(transferID, int64(len(data)))
		t.progressTracker.UpdateStatus(transferID, StatusCompleted)
		t.progressTracker.StopTracking(transferID)
	}

	return data, nil
}

// handleError 处理传输错误并返回适当的策略
func (t *baseMessageTransporter) handleError(err error) error {
	if err == nil {
		return nil
	}

	// 错误处理
	strategy := t.errorHandler.HandleError(err)

	// 根据错误处理策略执行不同操作
	switch strategy {
	case StrategyRetry:
		// 对于重试策略，返回原始错误，让调用者决定是否重试
		return &RetryableError{Err: err}
	case StrategyIgnore:
		return nil // 忽略错误
	case StrategyAbort, StrategyClose, StrategyPanic:
		logger.Errorf("错误处理策略: %v", strategy)
		return err // 返回错误
	default:
		return err
	}
}

// SendStream 实现了MessageTransporter接口的SendStream方法
func (t *baseMessageTransporter) SendStream(reader io.Reader) error {
	// 估计总数据大小（如果可能）
	var totalSize int64 = 0
	var transferID string

	// 尝试获取底层读取器的大小（如果是文件等可以提供大小的读取器）
	if sizer, ok := reader.(interface{ Size() int64 }); ok {
		totalSize = sizer.Size()
	}

	// 如果可以获取总大小并且需要跟踪进度
	if t.progressTracker != nil && totalSize > LargeMessageThreshold {
		transferID = generateTransferID()
		t.progressTracker.StartTracking(transferID, totalSize)
		defer t.progressTracker.StopTracking(transferID)
	}

	// 初始化已传输数据计数
	var totalSent int64 = 0

	// 选择适当的块大小
	// 对于小文件使用较小的块，大文件使用较大的块
	chunkSize := DefaultMediumChunkSize
	if totalSize > 0 {
		if totalSize < LargeMessageThreshold {
			chunkSize = DefaultSmallChunkSize
		} else {
			chunkSize = DefaultLargeChunkSize
		}
	}

	// 创建读取缓冲区
	buffer := make([]byte, chunkSize)
	isFirstChunk := true

	// 逐块读取并发送数据
	for {
		// 读取一块数据
		n, err := reader.Read(buffer)

		// 处理读取到的数据
		if n > 0 {
			// 设置帧标志
			flags := uint32(0)

			// 第一个块标记为分片帧的开始
			if isFirstChunk {
				flags |= FlagFragmented
				isFirstChunk = false
			}

			// 最后一个块标记为最后一帧
			if err == io.EOF {
				flags |= FlagLastFrame
			}

			// 发送数据块
			sendErr := t.frameProcessor.WriteFrame(t.conn, buffer[:n], flags)
			if sendErr != nil {
				if t.progressTracker != nil && transferID != "" {
					t.progressTracker.MarkFailed(transferID, sendErr)
				}
				return t.handleError(sendErr)
			}

			// 更新发送计数
			totalSent += int64(n)

			// 更新进度
			if t.progressTracker != nil && transferID != "" {
				t.progressTracker.UpdateProgress(transferID, totalSent)
			}
		}

		// 处理读取结束或错误
		if err == io.EOF {
			// 传输完成
			if t.progressTracker != nil && transferID != "" {
				t.progressTracker.UpdateStatus(transferID, StatusCompleted)
			}
			return nil
		}

		if err != nil {
			if t.progressTracker != nil && transferID != "" {
				t.progressTracker.MarkFailed(transferID, err)
			}
			return t.handleError(err)
		}
	}
}

// ReceiveStream 实现了MessageTransporter接口的ReceiveStream方法
func (t *baseMessageTransporter) ReceiveStream(writer io.Writer) error {
	// 创建传输ID（用于进度跟踪）
	var transferID string
	if t.progressTracker != nil {
		transferID = generateTransferID()
		t.progressTracker.StartTracking(transferID, LargeMessageSize)
		defer t.progressTracker.StopTracking(transferID)
	}

	// 初始化已接收字节计数
	var totalReceived int64 = 0

	// 标记是否收到了分片开始帧
	var receivedFragmentStart bool = false

	// 标记是否收到了最后一帧
	var receivedLastFrame bool = false

	// 接收数据帧直到收到标记为最后一帧的数据
	for !receivedLastFrame {
		// 读取下一个帧，包括帧标志
		data, flags, err := t.frameProcessor.ReadFrameWithFlags(t.conn)
		if err != nil {
			if t.progressTracker != nil && transferID != "" {
				t.progressTracker.MarkFailed(transferID, err)
			}
			return t.handleError(err)
		}

		// 检查是否是分片的开始
		if (flags & FlagFragmented) != 0 {
			receivedFragmentStart = true
		}

		// 检查是否是最后一帧
		if (flags & FlagLastFrame) != 0 {
			receivedLastFrame = true
		}

		// 确保收到了分片开始帧
		if !receivedFragmentStart {
			return t.handleError(NewPointSubError(
				"protocol violation: received non-first fragment without start marker",
				"协议错误: 收到了不带开始标记的非首个分片",
			))
		}

		// 写入接收到的数据
		if len(data) > 0 {
			n, err := writer.Write(data)
			if err != nil {
				if t.progressTracker != nil && transferID != "" {
					t.progressTracker.MarkFailed(transferID, err)
				}
				return t.handleError(err)
			}

			// 更新已接收字节计数
			totalReceived += int64(n)

			// 更新进度
			if t.progressTracker != nil && transferID != "" {
				// 调整总大小估计（如果实际接收超过了初始估计）
				if totalReceived > LargeMessageSize {
					t.progressTracker.UpdateTotalSize(transferID, totalReceived*2)
				}
				t.progressTracker.UpdateProgress(transferID, totalReceived)
			}
		}

		// 如果是空的最后一帧，退出循环
		if len(data) == 0 && receivedLastFrame {
			break
		}
	}

	// 更新状态为完成
	if t.progressTracker != nil && transferID != "" {
		t.progressTracker.UpdateStatus(transferID, StatusCompleted)
	}

	return nil
}

// Close 实现了MessageTransporter接口的Close方法
func (t *baseMessageTransporter) Close() error {
	return t.conn.Close()
}

// SetDeadline 实现了MessageTransporter接口的SetDeadline方法
func (t *baseMessageTransporter) SetDeadline(deadline time.Time) error {
	return t.conn.SetDeadline(deadline)
}

// SetReadDeadline 实现了MessageTransporter接口的SetReadDeadline方法
func (t *baseMessageTransporter) SetReadDeadline(deadline time.Time) error {
	return t.conn.SetReadDeadline(deadline)
}

// SetWriteDeadline 实现了MessageTransporter接口的SetWriteDeadline方法
func (t *baseMessageTransporter) SetWriteDeadline(deadline time.Time) error {
	return t.conn.SetWriteDeadline(deadline)
}
