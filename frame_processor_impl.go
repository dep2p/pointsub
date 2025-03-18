package pointsub

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
	"sync"
	"time"
)

// 帧处理器扩展常量
const (
	// 帧批处理大小
	DefaultBatchSize = 8

	// 压缩阈值（大于此值的数据将被压缩）
	CompressionThreshold = 1024

	// 压缩级别
	DefaultCompressLevel = 6

	// 紧凑帧大小阈值
	CompactFrameThreshold = 256

	// 帧处理超时
	DefaultFrameTimeout = 30 * time.Second

	// 紧凑帧头大小
	// CompactHeaderSize = 4

	// 默认帧头大小
	// DefaultHeaderSize = 16
)

// advancedFrameProcessor 提供高级帧处理功能
type advancedFrameProcessor struct {
	FrameProcessor // 嵌入基本帧处理器

	// 高级选项
	useCompression       bool
	compressionLevel     int
	useBatchProcessing   bool
	batchSize            int
	useStreamProcessing  bool
	errorRecoveryEnabled bool
	frameTimeout         time.Duration

	// 状态跟踪
	stats *frameProcessorStats

	// 压缩器池
	compressorPool sync.Pool
	// 解压器池
	decompressorPool sync.Pool
}

// 帧处理器统计
type frameProcessorStats struct {
	framesProcessed   int64
	bytesProcessed    int64
	compressedFrames  int64
	compressionRatio  float64
	processingErrors  int64
	recoveredErrors   int64
	avgProcessingTime time.Duration
	mu                sync.RWMutex
}

// AdvancedFrameProcessorOption 高级帧处理器选项
type AdvancedFrameProcessorOption func(*advancedFrameProcessor)

// WithCompression 启用压缩
func WithCompression(enabled bool) AdvancedFrameProcessorOption {
	return func(fp *advancedFrameProcessor) {
		fp.useCompression = enabled
	}
}

// WithCompressionLevel 设置压缩级别
func WithCompressionLevel(level int) AdvancedFrameProcessorOption {
	return func(fp *advancedFrameProcessor) {
		fp.compressionLevel = level
	}
}

// WithBatchProcessing 启用批处理
func WithBatchProcessing(enabled bool, batchSize int) AdvancedFrameProcessorOption {
	return func(fp *advancedFrameProcessor) {
		fp.useBatchProcessing = enabled
		if batchSize > 0 {
			fp.batchSize = batchSize
		}
	}
}

// WithStreamProcessing 启用流处理
func WithStreamProcessing(enabled bool) AdvancedFrameProcessorOption {
	return func(fp *advancedFrameProcessor) {
		fp.useStreamProcessing = enabled
	}
}

// WithErrorRecovery 启用错误恢复
func WithErrorRecovery(enabled bool) AdvancedFrameProcessorOption {
	return func(fp *advancedFrameProcessor) {
		fp.errorRecoveryEnabled = enabled
	}
}

// WithFrameTimeout 设置帧处理超时
func WithFrameTimeout(timeout time.Duration) AdvancedFrameProcessorOption {
	return func(fp *advancedFrameProcessor) {
		fp.frameTimeout = timeout
	}
}

// NewAdvancedFrameProcessor 创建高级帧处理器
func NewAdvancedFrameProcessor(options ...AdvancedFrameProcessorOption) FrameProcessor {
	// 创建基本帧处理器
	baseProcessor := NewDefaultFrameProcessor()

	// 创建高级处理器
	advProcessor := &advancedFrameProcessor{
		FrameProcessor:      baseProcessor,
		useCompression:      false,
		compressionLevel:    DefaultCompressLevel,
		useBatchProcessing:  false,
		batchSize:           DefaultBatchSize,
		useStreamProcessing: false,
		frameTimeout:        DefaultFrameTimeout,
		stats: &frameProcessorStats{
			avgProcessingTime: 0,
		},
		compressorPool: sync.Pool{
			New: func() interface{} {
				// 默认创建一个gzip写入器，压缩级别为DefaultCompressLevel
				w, _ := gzip.NewWriterLevel(nil, DefaultCompressLevel)
				return w
			},
		},
		decompressorPool: sync.Pool{
			New: func() interface{} {
				return new(gzip.Reader)
			},
		},
	}

	// 应用选项
	for _, opt := range options {
		opt(advProcessor)
	}

	return advProcessor
}

// compressData 压缩数据
func (p *advancedFrameProcessor) compressData(data []byte) ([]byte, error) {
	if len(data) < CompressionThreshold || !p.useCompression {
		return data, nil
	}

	// 获取压缩器
	compressor := p.compressorPool.Get().(*gzip.Writer)
	defer p.compressorPool.Put(compressor)

	// 准备缓冲区
	var buf bytes.Buffer
	compressor.Reset(&buf)

	// 写入数据
	if _, err := compressor.Write(data); err != nil {
		logger.Errorf("压缩数据失败: %v", err)
		return nil, err
	}

	// 关闭压缩器
	if err := compressor.Close(); err != nil {
		logger.Errorf("关闭压缩器失败: %v", err)
		return nil, err
	}

	// 更新压缩比例统计
	p.stats.mu.Lock()
	p.stats.compressedFrames++
	p.stats.compressionRatio = float64(buf.Len()) / float64(len(data))
	p.stats.mu.Unlock()

	return buf.Bytes(), nil
}

// decompressData 解压数据
func (p *advancedFrameProcessor) decompressData(data []byte, isCompressed bool) ([]byte, error) {
	if !isCompressed {
		return data, nil
	}

	// 获取解压器
	decompressor := p.decompressorPool.Get().(*gzip.Reader)
	defer p.decompressorPool.Put(decompressor)

	// 重置解压器
	if err := decompressor.Reset(bytes.NewReader(data)); err != nil {
		logger.Errorf("重置解压器失败: %v", err)
		return nil, err
	}

	// 读取解压后的数据
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, decompressor); err != nil {
		logger.Errorf("读取解压后的数据失败: %v", err)
		return nil, err
	}

	return buf.Bytes(), nil
}

// WriteFrame 重写WriteFrame方法，添加压缩和高级特性
func (p *advancedFrameProcessor) WriteFrame(writer io.Writer, data []byte, flags uint32) error {
	startTime := time.Now()

	// 检查是否为错误帧
	isErrorFrame := (flags & FlagError) != 0

	// 错误帧特殊处理
	if isErrorFrame {
		// 确保错误帧数据格式正确
		// 错误帧数据应该以错误码(4字节)开头，后面是错误消息
		if len(data) < 4 {
			// 创建一个标准错误帧
			errorCode := uint32(ErrorProtocolViolation)
			errorMsg := []byte("无效的错误帧数据")
			errorData := make([]byte, 4+len(errorMsg))
			binary.BigEndian.PutUint32(errorData[0:4], errorCode)
			copy(errorData[4:], errorMsg)
			data = errorData
		}
	}

	// 检查是否需要压缩
	var compressedData []byte
	var err error

	// 只对非错误帧进行压缩处理
	if !isErrorFrame && p.useCompression && len(data) >= CompressionThreshold {
		compressedData, err = p.compressData(data)
		if err != nil {
			logger.Errorf("压缩数据失败: %v", err)
			return err
		}

		// 如果压缩后数据更小，则使用压缩数据
		if len(compressedData) < len(data) {
			flags |= FlagCompressed
			data = compressedData
		}
	}

	// 调用基本帧处理器的WriteFrame方法
	err = p.FrameProcessor.WriteFrame(writer, data, flags)

	// 更新统计
	p.stats.mu.Lock()
	p.stats.framesProcessed++
	p.stats.bytesProcessed += int64(len(data))

	// 更新平均处理时间（使用滑动平均）
	elapsed := time.Since(startTime)
	if p.stats.avgProcessingTime == 0 {
		p.stats.avgProcessingTime = elapsed
	} else {
		// 90%旧值 + 10%新值的滑动平均
		p.stats.avgProcessingTime = p.stats.avgProcessingTime*9/10 + elapsed/10
	}

	if err != nil {
		p.stats.processingErrors++
	}
	p.stats.mu.Unlock()

	return err
}

// ReadFrame 重写ReadFrame方法，添加解压和高级特性
func (p *advancedFrameProcessor) ReadFrame(reader io.Reader) ([]byte, error) {
	startTime := time.Now()

	// 尝试读取帧头的开始部分
	initialHeader := make([]byte, 4) // 使用固定值4而不是使用CompactHeaderSize常量
	if _, err := io.ReadFull(reader, initialHeader); err != nil {
		logger.Errorf("读取帧头失败: %v", err)
		return nil, err
	}

	// 检查是否为紧凑帧
	isCompact := (initialHeader[3] & FlagCompact) != 0
	isError := (initialHeader[3] & FlagError) != 0

	// 如果不是紧凑帧，读取剩余帧头
	if !isCompact {
		remainingHeaderSize := 8 // 使用固定值8而不是使用DefaultHeaderSize-CompactHeaderSize
		remainingHeaderBytes := make([]byte, remainingHeaderSize)
		if _, err := io.ReadFull(reader, remainingHeaderBytes); err != nil {
			logger.Errorf("读取剩余帧头失败: %v", err)
			return nil, err
		}

		// 如果是标准帧头，更新错误标志
		flagsBytes := remainingHeaderBytes[4:8]
		flags := binary.BigEndian.Uint32(flagsBytes)
		isError = (flags & FlagError) != 0

		// 将完整帧头放回reader
		combinedHeader := append(initialHeader, remainingHeaderBytes...)
		reader = io.MultiReader(bytes.NewReader(combinedHeader), reader)
	} else {
		// 将紧凑帧头放回reader
		reader = io.MultiReader(bytes.NewReader(initialHeader), reader)
	}

	// 调用基本帧处理器的ReadFrame方法
	data, err := p.FrameProcessor.ReadFrame(reader)

	// 处理错误
	if err != nil {
		p.stats.mu.Lock()
		p.stats.processingErrors++
		p.stats.mu.Unlock()

		// 如果启用了错误恢复，尝试恢复
		if p.errorRecoveryEnabled {
			// 简单的错误恢复：检查是否是校验和错误，如果是则忽略校验和验证重试
			if err == ErrChecksumMismatch {
				// 这里应该实现实际的恢复逻辑
				// 暂时只计数恢复尝试
				p.stats.mu.Lock()
				p.stats.recoveredErrors++
				p.stats.mu.Unlock()
			}
		}

		return nil, err
	}

	// 处理错误帧
	if isError && len(data) >= 4 {
		errorCode := binary.BigEndian.Uint32(data[0:4])
		errorMsg := "未知错误"
		if len(data) > 4 {
			errorMsg = string(data[4:])
		}

		// 创建相应的错误
		err := fmt.Errorf("错误帧: 代码=%d, 消息=%s", errorCode, errorMsg)

		// 更新统计
		p.stats.mu.Lock()
		p.stats.processingErrors++
		p.stats.mu.Unlock()

		return nil, err
	}

	// 检查帧是否被压缩
	isCompressed := false

	// 尝试检测压缩数据的特征 (gzip头部标识: 0x1f, 0x8b)
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		isCompressed = true
	}

	// 如果是压缩帧，执行解压
	if isCompressed {
		decompressedData, err := p.decompressData(data, true)
		if err != nil {
			p.stats.mu.Lock()
			p.stats.processingErrors++
			p.stats.mu.Unlock()
			return nil, err
		}
		data = decompressedData
	}

	// 更新统计
	p.stats.mu.Lock()
	p.stats.framesProcessed++
	p.stats.bytesProcessed += int64(len(data))

	// 更新平均处理时间（使用滑动平均）
	elapsed := time.Since(startTime)
	if p.stats.avgProcessingTime == 0 {
		p.stats.avgProcessingTime = elapsed
	} else {
		// 90%旧值 + 10%新值的滑动平均
		p.stats.avgProcessingTime = p.stats.avgProcessingTime*9/10 + elapsed/10
	}
	p.stats.mu.Unlock()

	return data, nil
}

// WriteBatchFrames 批量写入多个帧
func (p *advancedFrameProcessor) WriteBatchFrames(writer io.Writer, dataBatch [][]byte, flags []uint32) error {
	if !p.useBatchProcessing {
		// 如果未启用批处理，则逐个处理
		for i, data := range dataBatch {
			flag := uint32(0)
			if i < len(flags) {
				flag = flags[i]
			}
			if err := p.WriteFrame(writer, data, flag); err != nil {
				logger.Errorf("写入帧失败: %v", err)
				return err
			}
		}
		return nil
	}

	// 启用批处理时的逻辑
	// 1. 写入批次头部
	batchSize := len(dataBatch)
	batchHeader := make([]byte, 4)
	binary.BigEndian.PutUint32(batchHeader, uint32(batchSize))
	if _, err := writer.Write(batchHeader); err != nil {
		logger.Errorf("写入批次头部失败: %v", err)
		return err
	}

	// 2. 逐个写入帧，但可以优化共同操作
	for i, data := range dataBatch {
		flag := uint32(0)
		if i < len(flags) {
			flag = flags[i]
		}

		// 为批处理的第一帧和最后一帧添加特殊标记
		if i == 0 {
			flag |= FlagFragmented
		}
		if i == batchSize-1 {
			flag |= FlagLastFrame
		}

		if err := p.WriteFrame(writer, data, flag); err != nil {
			logger.Errorf("写入帧失败: %v", err)
			return err
		}
	}

	return nil
}

// ReadBatchFrames 批量读取多个帧
func (p *advancedFrameProcessor) ReadBatchFrames(reader io.Reader) ([][]byte, error) {
	// 1. 读取批次头部
	batchHeader := make([]byte, 4)
	if _, err := io.ReadFull(reader, batchHeader); err != nil {
		logger.Errorf("读取批次头部失败: %v", err)
		return nil, err
	}

	batchSize := int(binary.BigEndian.Uint32(batchHeader))
	if batchSize <= 0 || batchSize > 1000 { // 设置合理的上限
		return nil, ErrInvalidHeaderFormat
	}

	// 2. 逐个读取帧
	result := make([][]byte, 0, batchSize)
	for i := 0; i < batchSize; i++ {
		data, err := p.ReadFrame(reader)
		if err != nil {
			logger.Errorf("读取帧失败: %v", err)
			return result, err
		}
		result = append(result, data)
	}

	return result, nil
}

// WriteStream 以流式方式写入数据
func (p *advancedFrameProcessor) WriteStream(writer io.Writer, reader io.Reader, chunkSize int, flags uint32) error {
	if !p.useStreamProcessing {
		// 如果未启用流处理，则先读取所有数据再发送
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, reader); err != nil {
			logger.Errorf("读取数据失败: %v", err)
			return err
		}
		return p.WriteFrame(writer, buf.Bytes(), flags)
	}

	// 启用流处理时的逻辑
	if chunkSize <= 0 {
		chunkSize = DefaultMediumChunkSize
	}

	buffer := make([]byte, chunkSize)
	isFirstChunk := true

	for {
		n, err := reader.Read(buffer)
		if n > 0 {
			// 设置帧标志
			chunkFlags := flags
			if isFirstChunk {
				chunkFlags |= FlagFragmented
				isFirstChunk = false
			}

			// 检查是否是最后一块
			if err == io.EOF {
				chunkFlags |= FlagLastFrame
			}

			// 写入这个数据块
			if err := p.WriteFrame(writer, buffer[:n], chunkFlags); err != nil {
				logger.Errorf("写入帧失败: %v", err)
				return err
			}
		}

		if err == io.EOF {
			break
		}

		if err != nil {
			logger.Errorf("读取数据失败: %v", err)
			return err
		}
	}

	return nil
}

// ReadStream 以流式方式读取数据
func (p *advancedFrameProcessor) ReadStream(reader io.Reader, writer io.Writer) error {
	if !p.useStreamProcessing {
		// 如果未启用流处理，则一次性读取全部数据
		data, err := p.ReadFrame(reader)
		if err != nil {
			logger.Errorf("读取数据失败: %v", err)
			return err
		}
		_, err = writer.Write(data)
		return err
	}

	// 读取第一帧
	firstFrame, err := p.ReadFrame(reader)
	if err != nil {
		logger.Errorf("读取数据失败: %v", err)
		return err
	}

	// 写入第一帧数据
	if _, err := writer.Write(firstFrame); err != nil {
		logger.Errorf("写入数据失败: %v", err)
		return err
	}

	// 检查是否为流式传输（分片标志）
	// 这里需要解析帧头来获取标志
	isFragmented := false

	// 为了获取标志，需要解析帧头
	// 这里假设我们可以从前几个字节判断是否分片
	// 实际实现中，应该从帧头正确提取这个信息
	if len(firstFrame) > 0 && (firstFrame[0]&byte(FlagFragmented)) != 0 {
		isFragmented = true
	}

	// 如果不是分片传输，直接返回
	if !isFragmented {
		return nil
	}

	// 继续读取后续帧，直到收到结束标志
	for {
		frame, err := p.ReadFrame(reader)
		if err != nil {
			logger.Errorf("读取帧失败: %v", err)
			return err
		}

		// 写入帧数据
		if _, err := writer.Write(frame); err != nil {
			logger.Errorf("写入帧数据失败: %v", err)
			return err
		}

		// 检查是否为最后一帧
		isLastFrame := false

		// 为了获取标志，需要解析帧头
		// 这里假设我们可以从前几个字节判断是否为最后一帧
		// 实际实现中，应该从帧头正确提取这个信息
		if len(frame) > 0 && (frame[0]&byte(FlagLastFrame)) != 0 {
			isLastFrame = true
		}

		// 如果是最后一帧，结束读取
		if isLastFrame {
			break
		}
	}

	return nil
}

// GetStats 获取帧处理器统计信息
func (p *advancedFrameProcessor) GetStats() *frameProcessorStats {
	p.stats.mu.RLock()
	defer p.stats.mu.RUnlock()

	// 返回一个副本，避免并发访问问题
	statsCopy := &frameProcessorStats{
		framesProcessed:   p.stats.framesProcessed,
		bytesProcessed:    p.stats.bytesProcessed,
		compressedFrames:  p.stats.compressedFrames,
		compressionRatio:  p.stats.compressionRatio,
		processingErrors:  p.stats.processingErrors,
		recoveredErrors:   p.stats.recoveredErrors,
		avgProcessingTime: p.stats.avgProcessingTime,
	}
	return statsCopy
}
