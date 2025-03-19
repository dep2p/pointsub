package pointsub

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"sync"
	"time"
)

// 帧处理器扩展常量
const (
	// 压缩阈值（大于此值的数据将被压缩）
	CompressionThreshold = 1024

	// 压缩级别
	DefaultCompressLevel = 6

	// 紧凑帧大小阈值
	CompactFrameThreshold = 256

	// 帧处理超时
	DefaultFrameTimeout = 30 * time.Second
)

// advancedFrameProcessor 提供高级帧处理功能
type advancedFrameProcessor struct {
	FrameProcessor // 嵌入基本帧处理器

	// 高级选项
	useCompression       bool
	compressionLevel     int
	errorRecoveryEnabled bool
	frameTimeout         time.Duration

	// 状态跟踪
	stats *frameProcessorStats

	// 压缩器池
	compressorPool sync.Pool
	// 解压器池
	decompressorPool sync.Pool
}

// frameProcessorStats 保存帧处理器的统计信息
type frameProcessorStats struct {
	mu                sync.Mutex
	framesProcessed   int64
	bytesProcessed    int64
	processingErrors  int64
	checksumErrors    int64
	recoveredErrors   int64
	compressedFrames  int64
	compressionRatio  float64
	avgProcessingTime time.Duration
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

// WithFrameTimeout 设置帧处理超时时间
func WithFrameTimeout(timeout time.Duration) AdvancedFrameProcessorOption {
	return func(fp *advancedFrameProcessor) {
		fp.frameTimeout = timeout
	}
}

// WithErrorRecovery 启用错误恢复
func WithErrorRecovery(enabled bool) AdvancedFrameProcessorOption {
	return func(fp *advancedFrameProcessor) {
		fp.errorRecoveryEnabled = enabled
	}
}

// NewAdvancedFrameProcessor 创建高级帧处理器
func NewAdvancedFrameProcessor(options ...AdvancedFrameProcessorOption) FrameProcessor {
	// 创建基础帧处理器
	baseProcessor := NewDefaultFrameProcessor()

	// 创建高级帧处理器
	advanced := &advancedFrameProcessor{
		FrameProcessor:   baseProcessor,
		compressionLevel: DefaultCompressLevel,
		frameTimeout:     DefaultFrameTimeout,
		stats:            &frameProcessorStats{},
	}

	// 初始化压缩器池
	advanced.compressorPool = sync.Pool{
		New: func() interface{} {
			writer, _ := gzip.NewWriterLevel(nil, advanced.compressionLevel)
			return writer
		},
	}

	// 初始化解压器池
	advanced.decompressorPool = sync.Pool{
		New: func() interface{} {
			return new(gzip.Reader)
		},
	}

	// 应用选项
	for _, opt := range options {
		opt(advanced)
	}

	return advanced
}

// GetStats 获取帧处理器统计信息
func (p *advancedFrameProcessor) GetStats() *frameProcessorStats {
	p.stats.mu.Lock()
	defer p.stats.mu.Unlock()

	// 返回一个副本，避免并发访问问题
	statsCopy := &frameProcessorStats{
		framesProcessed:   p.stats.framesProcessed,
		bytesProcessed:    p.stats.bytesProcessed,
		processingErrors:  p.stats.processingErrors,
		checksumErrors:    p.stats.checksumErrors,
		recoveredErrors:   p.stats.recoveredErrors,
		compressedFrames:  p.stats.compressedFrames,
		compressionRatio:  p.stats.compressionRatio,
		avgProcessingTime: p.stats.avgProcessingTime,
	}

	return statsCopy
}

// compressData 压缩数据
func (p *advancedFrameProcessor) compressData(data []byte) ([]byte, error) {
	// 如果数据小于压缩阈值，则不压缩
	if len(data) < CompressionThreshold {
		return data, nil
	}

	// 获取压缩器
	compressor := p.compressorPool.Get().(*gzip.Writer)
	defer p.compressorPool.Put(compressor)

	// 准备缓冲区
	var buf bytes.Buffer
	compressor.Reset(&buf)

	// 压缩数据
	if _, err := compressor.Write(data); err != nil {
		return nil, err
	}

	// 完成压缩
	if err := compressor.Close(); err != nil {
		return nil, err
	}

	// 检查压缩是否有效
	if buf.Len() >= len(data) {
		// 压缩无效，返回原始数据
		return data, nil
	}

	// 更新压缩统计
	p.stats.mu.Lock()
	p.stats.compressedFrames++
	ratio := float64(buf.Len()) / float64(len(data))
	p.stats.compressionRatio = (p.stats.compressionRatio + ratio) / 2
	p.stats.mu.Unlock()

	// 返回压缩数据
	return buf.Bytes(), nil
}

// decompressData 解压数据
func (p *advancedFrameProcessor) decompressData(data []byte, isCompressed bool) ([]byte, error) {
	// 检查数据是否压缩
	if !isCompressed {
		return data, nil
	}

	// 获取解压器
	decompressor := p.decompressorPool.Get().(*gzip.Reader)
	defer p.decompressorPool.Put(decompressor)

	// 重置解压器
	if err := decompressor.Reset(bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("重置解压器失败: %w", err)
	}

	// 解压数据
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, decompressor); err != nil {
		return nil, fmt.Errorf("解压数据失败: %w", err)
	}

	// 关闭解压器
	if err := decompressor.Close(); err != nil {
		return nil, fmt.Errorf("关闭解压器失败: %w", err)
	}

	return buf.Bytes(), nil
}

// WriteFrame 实现FrameProcessor接口，写入一个完整的帧
func (p *advancedFrameProcessor) WriteFrame(writer io.Writer, data []byte, flags uint32) error {
	// 记录开始时间
	initialTimer := time.Now()

	fmt.Printf("WriteFrame: 开始写入帧，大小: %d 字节，标志: 0x%X，时间: %v\n",
		len(data), flags, initialTimer.Format("15:04:05.000"))

	// 检查各个标志位
	if (flags & FlagFragmented) != 0 {
		fmt.Println("WriteFrame: 帧包含分片标志(FlagFragmented)")
	}
	if (flags & FlagLastFrame) != 0 {
		fmt.Println("WriteFrame: 帧包含最后一帧标志(FlagLastFrame)")
	}

	// 设置写入超时（如果连接支持）
	var conn interface {
		SetWriteDeadline(t time.Time) error
	}
	var hasTimeout bool

	if p.frameTimeout > 0 {
		if connWithTimeout, ok := writer.(interface {
			SetWriteDeadline(t time.Time) error
		}); ok {
			conn = connWithTimeout
			hasTimeout = true

			// 设置初始写入超时（比默认超时更长，避免过早超时）
			initialTimeout := p.frameTimeout * 2
			if err := conn.SetWriteDeadline(time.Now().Add(initialTimeout)); err != nil {
				logger.Warnf("高级帧处理器: 设置初始写入超时失败: %v", err)
			} else {
				fmt.Printf("WriteFrame: 设置初始写入超时: %v\n", initialTimeout)
			}
		}
	}

	// 确保在函数结束前清除超时
	if hasTimeout {
		defer func() {
			if err := conn.SetWriteDeadline(time.Time{}); err != nil {
				logger.Warnf("高级帧处理器: 清除写入超时失败: %v", err)
			} else {
				fmt.Println("WriteFrame: 已清除写入超时")
			}
		}()
	}

	// 计算校验和
	checksum := p.FrameProcessor.CalculateChecksum(data)
	fmt.Printf("WriteFrame: 计算校验和: %X\n", checksum)

	// 检查是否需要压缩数据
	var compressedData []byte
	if p.useCompression && len(data) > CompressionThreshold {
		fmt.Println("WriteFrame: 尝试压缩数据")
		compressed, err := p.compressData(data)
		if err != nil {
			p.stats.mu.Lock()
			p.stats.processingErrors++
			p.stats.mu.Unlock()
			fmt.Printf("WriteFrame: 压缩数据失败: %v\n", err)
			logger.Errorf("高级帧处理器: 压缩数据失败: %v", err)
			return err
		}

		// 只有压缩率超过阈值时才使用压缩数据
		compressionRatio := float64(len(compressed)) / float64(len(data))
		fmt.Printf("WriteFrame: 压缩率: %.2f%%\n", compressionRatio*100)

		if compressionRatio < 0.9 { // 使用90%的压缩率阈值
			compressedData = compressed
			flags |= FlagCompressed
			fmt.Printf("WriteFrame: 启用压缩，原始大小: %d 字节，压缩后: %d 字节\n",
				len(data), len(compressedData))
		} else {
			fmt.Println("WriteFrame: 压缩效果不佳，使用原始数据")
		}
	}

	// 使用压缩数据（如果有）
	if compressedData != nil {
		data = compressedData
		// 更新校验和
		checksum = p.FrameProcessor.CalculateChecksum(data)
		fmt.Printf("WriteFrame: 更新压缩数据校验和: %X\n", checksum)
	}

	// 如果帧很大，调整写入超时
	if hasTimeout && len(data) > DefaultLargeChunkSize {
		// 根据数据大小调整超时，使用更宽松的公式
		// 至少60秒，每MB额外增加20秒
		timeout := 60 * time.Second
		mbSize := len(data) / (1024 * 1024)
		if mbSize > 0 {
			additionalTime := time.Duration(mbSize) * 20 * time.Second
			if additionalTime > 10*time.Minute { // 限制最大附加时间
				additionalTime = 10 * time.Minute
			}
			timeout += additionalTime
		}

		fmt.Printf("WriteFrame: 调整写入超时为 %v (帧大小: %d 字节)\n",
			timeout, len(data))
		logger.Debugf("高级帧处理器: 调整写入超时为 %v (帧大小: %d 字节)",
			timeout, len(data))

		// 设置新的超时
		if err := conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
			logger.Warnf("高级帧处理器: 重置写入超时失败: %v", err)
		}
	}

	// 使用底层帧处理器编码并写入帧
	err := p.FrameProcessor.WriteFrame(writer, data, flags)
	if err != nil {
		p.stats.mu.Lock()
		p.stats.processingErrors++
		p.stats.mu.Unlock()
		fmt.Printf("WriteFrame: 写入帧失败: %v\n", err)
		logger.Errorf("高级帧处理器: 写入帧失败: %v", err)
		return err
	}

	// 更新统计信息
	p.stats.mu.Lock()
	p.stats.framesProcessed++
	p.stats.bytesProcessed += int64(len(data))
	p.stats.avgProcessingTime = (p.stats.avgProcessingTime + time.Since(initialTimer)) / 2
	p.stats.mu.Unlock()

	processingTime := time.Since(initialTimer)
	fmt.Printf("WriteFrame: 完成帧写入，处理时间: %v\n", processingTime)

	return nil
}

// ReadFrameWithFlags 读取帧并返回帧标志
func (p *advancedFrameProcessor) ReadFrameWithFlags(reader io.Reader) ([]byte, uint32, error) {
	// 记录开始时间
	initialTimer := time.Now()

	fmt.Printf("ReadFrameWithFlags: 开始读取帧，时间: %v\n", initialTimer.Format("15:04:05.000"))

	// 设置读取超时（如果已配置并且连接支持）
	var conn interface {
		SetReadDeadline(t time.Time) error
	}
	var hasTimeout bool

	if p.frameTimeout > 0 {
		if connWithTimeout, ok := reader.(interface {
			SetReadDeadline(t time.Time) error
		}); ok {
			conn = connWithTimeout
			hasTimeout = true

			// 设置初始读取超时（比默认超时更长，避免过早超时）
			initialTimeout := p.frameTimeout * 2
			if err := conn.SetReadDeadline(time.Now().Add(initialTimeout)); err != nil {
				logger.Warnf("高级帧处理器: 设置初始读取超时失败: %v", err)
			} else {
				fmt.Printf("ReadFrameWithFlags: 设置初始读取超时: %v\n", initialTimeout)
			}
		}
	}

	// 确保在函数结束前清除超时
	if hasTimeout {
		defer func() {
			if err := conn.SetReadDeadline(time.Time{}); err != nil {
				logger.Warnf("高级帧处理器: 清除读取超时失败: %v", err)
			} else {
				fmt.Println("ReadFrameWithFlags: 已清除读取超时")
			}
		}()
	}

	// 读取帧头部
	compactHeader := make([]byte, CompactHeaderSize)
	if _, err := io.ReadFull(reader, compactHeader); err != nil {
		p.stats.mu.Lock()
		p.stats.processingErrors++
		p.stats.mu.Unlock()
		fmt.Printf("ReadFrameWithFlags: 读取紧凑帧头失败: %v\n", err)
		logger.Errorf("高级帧处理器: 读取紧凑帧头失败: %v", err)
		return nil, 0, err
	}

	fmt.Printf("ReadFrameWithFlags: 成功读取紧凑帧头: %X\n", compactHeader)

	// 检查是否为紧凑帧
	isCompact := (compactHeader[3] & FlagCompact) != 0
	fmt.Printf("ReadFrameWithFlags: 是否为紧凑帧: %v\n", isCompact)

	var header []byte
	if isCompact {
		header = compactHeader
	} else {
		// 不是紧凑帧，需要读取完整帧头
		remainingHeaderBytes := make([]byte, DefaultHeaderSize-CompactHeaderSize)
		if _, err := io.ReadFull(reader, remainingHeaderBytes); err != nil {
			p.stats.mu.Lock()
			p.stats.processingErrors++
			p.stats.mu.Unlock()
			fmt.Printf("ReadFrameWithFlags: 读取完整帧头失败: %v\n", err)
			logger.Errorf("高级帧处理器: 读取完整帧头失败: %v", err)
			return nil, 0, err
		}

		fmt.Printf("ReadFrameWithFlags: 成功读取额外帧头部分: %X\n", remainingHeaderBytes)

		// 组合完整帧头
		header = append(compactHeader, remainingHeaderBytes...)
	}

	// 解码帧头
	frameHeader, err := p.FrameProcessor.DecodeHeader(header)
	if err != nil {
		p.stats.mu.Lock()
		p.stats.processingErrors++
		p.stats.mu.Unlock()
		fmt.Printf("ReadFrameWithFlags: 解码帧头失败: %v\n", err)
		logger.Errorf("高级帧处理器: 解码帧头失败: %v", err)
		return nil, 0, err
	}

	// 获取帧标志
	flags := frameHeader.Flags
	fmt.Printf("ReadFrameWithFlags: 帧头解码成功，大小: %d 字节，标志: 0x%X\n", frameHeader.Size, flags)

	// 检查各标志位
	if (flags & FlagFragmented) != 0 {
		fmt.Println("ReadFrameWithFlags: 帧包含分片标志(FlagFragmented)")
	}
	if (flags & FlagLastFrame) != 0 {
		fmt.Println("ReadFrameWithFlags: 帧包含最后一帧标志(FlagLastFrame)")
	}
	if (flags & FlagCompressed) != 0 {
		fmt.Println("ReadFrameWithFlags: 帧包含压缩标志(FlagCompressed)")
	}

	// 检查是否是压缩帧
	isCompressed := (flags & FlagCompressed) != 0

	// 处理空帧
	if frameHeader.Size == 0 {
		p.stats.mu.Lock()
		p.stats.framesProcessed++
		p.stats.avgProcessingTime = (p.stats.avgProcessingTime + time.Since(initialTimer)) / 2
		p.stats.mu.Unlock()
		fmt.Println("ReadFrameWithFlags: 接收到空帧，直接返回")
		return []byte{}, flags, nil
	}

	// 如果帧很大，调整读取超时
	if hasTimeout && frameHeader.Size > DefaultLargeChunkSize {
		// 根据数据大小调整超时，使用更宽松的公式
		// 至少60秒，每MB额外增加20秒
		timeout := 60 * time.Second
		mbSize := frameHeader.Size / (1024 * 1024)
		if mbSize > 0 {
			additionalTime := time.Duration(mbSize) * 20 * time.Second
			if additionalTime > 10*time.Minute { // 限制最大附加时间
				additionalTime = 10 * time.Minute
			}
			timeout += additionalTime
		}

		fmt.Printf("ReadFrameWithFlags: 调整读取超时为 %v (帧大小: %d 字节)\n", timeout, frameHeader.Size)
		logger.Debugf("高级帧处理器: 调整读取超时为 %v (帧大小: %d 字节)", timeout, frameHeader.Size)

		// 设置新的超时
		if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
			logger.Warnf("高级帧处理器: 重置读取超时失败: %v", err)
		}
	}

	// 读取帧数据
	data := make([]byte, frameHeader.Size)
	if _, err := io.ReadFull(reader, data); err != nil {
		p.stats.mu.Lock()
		p.stats.processingErrors++
		p.stats.mu.Unlock()
		fmt.Printf("ReadFrameWithFlags: 读取帧数据失败: %v\n", err)
		logger.Errorf("高级帧处理器: 读取帧数据失败: %v", err)
		return nil, 0, err
	}

	fmt.Printf("ReadFrameWithFlags: 成功读取帧数据，大小: %d 字节\n", len(data))

	// 验证校验和
	if frameHeader.Checksum != 0 {
		checksumValid := false

		if isCompact {
			actualChecksum := p.FrameProcessor.CalculateChecksum(data)
			if byte(actualChecksum&0xFF) == byte(frameHeader.Checksum) {
				checksumValid = true
			} else {
				fmt.Printf("ReadFrameWithFlags: 紧凑帧校验和不匹配: 预期 %X, 实际 %X\n",
					byte(frameHeader.Checksum), byte(actualChecksum&0xFF))
				logger.Errorf("高级帧处理器: 紧凑帧校验和不匹配: 预期 %X, 实际 %X",
					byte(frameHeader.Checksum), byte(actualChecksum&0xFF))
			}
		} else if p.FrameProcessor.VerifyData(data, frameHeader.Checksum) {
			checksumValid = true
		} else {
			actualChecksum := p.FrameProcessor.CalculateChecksum(data)
			fmt.Printf("ReadFrameWithFlags: 校验和不匹配: 预期 %X, 实际 %X\n",
				frameHeader.Checksum, actualChecksum)
			logger.Errorf("高级帧处理器: 校验和不匹配: 预期 %X, 实际 %X",
				frameHeader.Checksum, actualChecksum)
		}

		// 如果校验和不匹配且不允许错误恢复，返回错误
		if !checksumValid && !p.errorRecoveryEnabled {
			p.stats.mu.Lock()
			p.stats.checksumErrors++
			p.stats.mu.Unlock()
			return nil, 0, ErrChecksumMismatch
		} else if !checksumValid {
			// 校验和不匹配但允许错误恢复，记录警告
			fmt.Println("ReadFrameWithFlags: 校验和不匹配但允许错误恢复")
			logger.Warnf("高级帧处理器: 校验和不匹配但允许错误恢复")
			p.stats.mu.Lock()
			p.stats.checksumErrors++
			p.stats.mu.Unlock()
		}
	}

	// 解压数据
	if isCompressed {
		fmt.Println("ReadFrameWithFlags: 开始解压数据")
		uncompressed, err := p.decompressData(data, true)
		if err != nil {
			p.stats.mu.Lock()
			p.stats.processingErrors++
			p.stats.mu.Unlock()
			fmt.Printf("ReadFrameWithFlags: 解压数据失败: %v\n", err)
			logger.Errorf("高级帧处理器: 解压数据失败: %v", err)
			return nil, 0, err
		}
		fmt.Printf("ReadFrameWithFlags: 解压数据成功，原始大小: %d 字节，解压后: %d 字节\n",
			len(data), len(uncompressed))
		data = uncompressed
	}

	// 更新统计信息
	p.stats.mu.Lock()
	p.stats.framesProcessed++
	p.stats.bytesProcessed += int64(len(data))
	p.stats.avgProcessingTime = (p.stats.avgProcessingTime + time.Since(initialTimer)) / 2
	p.stats.mu.Unlock()

	processingTime := time.Since(initialTimer)
	fmt.Printf("ReadFrameWithFlags: 完成帧处理，处理时间: %v，返回数据大小: %d 字节，标志: 0x%X\n",
		processingTime, len(data), flags)

	return data, flags, nil
}

// ReadFrame 重写基础帧处理器的ReadFrame方法，添加解压功能
func (p *advancedFrameProcessor) ReadFrame(reader io.Reader) ([]byte, error) {
	// 使用ReadFrameWithFlags实现
	data, _, err := p.ReadFrameWithFlags(reader)
	return data, err
}
