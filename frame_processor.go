package pointsub

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"time"
)

// 帧头部常量
const (
	// 帧头部大小 - 结构如下:
	// 4字节: 数据大小 - 支持大型消息
	// 4字节: 校验和 - 完整CRC32校验保证数据完整性
	// 4字节: 标志 - 支持多种帧类型和扩展标记
	DefaultHeaderSize = 12 // 大小(4字节) + 校验和(4字节) + 标志(4字节)

	// 紧凑帧头部大小 - 结构如下:
	// 2字节: 数据大小 - 足够支持最大64KB的小型消息
	// 1字节: 校验和 - 简化的校验和，仅取CRC32最低字节，降低带宽消耗
	// 1字节: 标志 - 支持基本帧类型标记
	CompactHeaderSize = 4 // 大小(2字节) + 校验和(1字节) + 标志(1字节)

	// 帧标志 - 每个标志占用一个位，允许组合使用
	FlagNormal     = 0x00 // 普通帧 - 单条完整消息
	FlagFragmented = 0x01 // 分片帧 - 流式传输的开始
	FlagLastFrame  = 0x02 // 最后一帧 - 流式传输的结束
	FlagCompressed = 0x04 // 压缩帧 - 包含压缩数据
	FlagError      = 0x08 // 错误帧 - 包含错误信息
	FlagControl    = 0x10 // 控制帧 - 系统控制消息
	FlagEmpty      = 0x20 // 空帧 - 无数据的帧（如心跳）
	FlagCompact    = 0x40 // 紧凑帧 - 使用紧凑帧头
)

// 错误类型
var (
	ErrInvalidHeaderSize   = errors.New("无效的帧头大小")
	ErrInvalidHeaderFormat = errors.New("无效的帧头格式")
	ErrChecksumMismatch    = errors.New("校验和不匹配")
	ErrFrameTooBig         = errors.New("帧大小超出限制")
)

// FrameHeader 表示消息帧头
type FrameHeader struct {
	// 消息大小
	Size int

	// 校验和 - 使用CRC32校验保证数据完整性
	// 对于紧凑帧，只使用最低8位
	Checksum uint32

	// 帧标志 - 表示消息的类型和处理方式
	Flags uint32

	// 序列号 - 可用于跟踪消息顺序（扩展用）
	Sequence uint16

	// 协议版本 - 支持协议演化（扩展用）
	Version uint8
}

// FrameProcessor 处理消息帧的编码和解码
// 本接口的设计支持以下特性:
// 1. 统一的帧处理 - 通过标准接口处理不同类型的帧
// 2. 自适应帧大小 - 根据消息大小自动选择紧凑帧或完整帧
// 3. 数据完整性 - 使用校验和验证消息完整性
// 4. 流式传输 - 通过分片帧和最后一帧标志支持流式数据
// 5. 错误传递 - 支持传递错误信息
type FrameProcessor interface {
	// EncodeHeader 编码消息帧头
	// 根据数据大小和标志自动选择紧凑帧或完整帧格式
	EncodeHeader(size int, checksum uint32, flags uint32) ([]byte, error)

	// DecodeHeader 解码消息帧头
	// 支持解析紧凑帧和完整帧格式
	DecodeHeader(header []byte) (FrameHeader, error)

	// CalculateChecksum 计算校验和
	// 使用CRC32算法计算数据的校验和
	CalculateChecksum(data []byte) uint32

	// VerifyData 验证数据完整性
	// 通过比较计算的校验和与期望的校验和来验证数据完整性
	VerifyData(data []byte, expectedChecksum uint32) bool

	// ReadFrame 从读取器读取一个完整的帧
	// 会自动处理帧头解析、数据读取和校验
	ReadFrame(reader io.Reader) ([]byte, error)

	// ReadFrameWithFlags 读取帧并返回帧标志
	// 与ReadFrame相似，但同时返回帧的标志信息
	ReadFrameWithFlags(reader io.Reader) ([]byte, uint32, error)

	// WriteFrame 写入帧
	// 处理帧头生成、校验和计算和数据写入
	WriteFrame(writer io.Writer, data []byte, flags uint32) error
}

// defaultFrameProcessor 默认的帧处理器实现
type defaultFrameProcessor struct {
	// 最大帧大小限制 (0表示无限制)
	maxFrameSize int
}

// FrameProcessorOption 定义帧处理器选项
type FrameProcessorOption func(*defaultFrameProcessor)

// WithMaxFrameSize 设置最大帧大小限制
func WithMaxFrameSize(maxSize int) FrameProcessorOption {
	return func(fp *defaultFrameProcessor) {
		fp.maxFrameSize = maxSize
	}
}

// NewDefaultFrameProcessor 创建默认帧处理器
func NewDefaultFrameProcessor(options ...FrameProcessorOption) FrameProcessor {
	fp := &defaultFrameProcessor{
		maxFrameSize: 0, // 默认无限制
	}

	for _, opt := range options {
		opt(fp)
	}

	return fp
}

// EncodeHeader 实现FrameProcessor接口
func (p *defaultFrameProcessor) EncodeHeader(size int, checksum uint32, flags uint32) ([]byte, error) {
	// 检查帧大小限制
	if p.maxFrameSize > 0 && size > p.maxFrameSize {
		return nil, ErrFrameTooBig
	}

	// 使用紧凑帧头（对于小消息）
	if size < 65536 && flags&FlagCompact != 0 {
		header := make([]byte, CompactHeaderSize)
		binary.BigEndian.PutUint16(header[0:2], uint16(size))
		header[2] = byte(checksum & 0xFF) // 简化校验和
		header[3] = byte(flags & 0xFF)    // 标志
		return header, nil
	}

	// 使用标准帧头
	header := make([]byte, DefaultHeaderSize)
	binary.BigEndian.PutUint32(header[0:4], uint32(size))
	binary.BigEndian.PutUint32(header[4:8], checksum)
	binary.BigEndian.PutUint32(header[8:12], flags)
	return header, nil
}

// DecodeHeader 实现FrameProcessor接口
func (p *defaultFrameProcessor) DecodeHeader(header []byte) (FrameHeader, error) {
	// 检查头部长度
	if len(header) < CompactHeaderSize {
		return FrameHeader{}, ErrInvalidHeaderSize
	}

	// 判断是否为紧凑帧头
	isCompact := false
	if len(header) == CompactHeaderSize {
		isCompact = true
	} else if len(header) < DefaultHeaderSize {
		return FrameHeader{}, ErrInvalidHeaderSize
	}

	var result FrameHeader

	// 解析头部
	if isCompact {
		// 紧凑帧头解析
		result.Size = int(binary.BigEndian.Uint16(header[0:2]))
		result.Checksum = uint32(header[2])
		result.Flags = uint32(header[3])
		result.Flags |= FlagCompact // 标记为紧凑帧
	} else {
		// 标准帧头解析
		result.Size = int(binary.BigEndian.Uint32(header[0:4]))
		result.Checksum = binary.BigEndian.Uint32(header[4:8])
		result.Flags = binary.BigEndian.Uint32(header[8:12])
	}

	// 检查帧大小限制
	if p.maxFrameSize > 0 && result.Size > p.maxFrameSize {
		return FrameHeader{}, ErrFrameTooBig
	}

	return result, nil
}

// CalculateChecksum 实现FrameProcessor接口
func (p *defaultFrameProcessor) CalculateChecksum(data []byte) uint32 {
	return crc32.ChecksumIEEE(data)
}

// VerifyData 实现FrameProcessor接口
func (p *defaultFrameProcessor) VerifyData(data []byte, expectedChecksum uint32) bool {
	actualChecksum := p.CalculateChecksum(data)
	return actualChecksum == expectedChecksum
}

// ReadFrame 从读取器读取一个完整的帧
func (p *defaultFrameProcessor) ReadFrame(reader io.Reader) ([]byte, error) {
	// 尝试读取紧凑帧头
	compactHeader := make([]byte, CompactHeaderSize)
	if _, err := io.ReadFull(reader, compactHeader); err != nil {
		logger.Errorf("读取紧凑帧头失败: %v", err)
		return nil, err
	}

	// 检查是否为紧凑帧
	isCompact := (compactHeader[3] & FlagCompact) != 0

	var header []byte
	if isCompact {
		header = compactHeader
	} else {
		// 不是紧凑帧，需要读取完整帧头
		remainingHeaderBytes := make([]byte, DefaultHeaderSize-CompactHeaderSize)
		if _, err := io.ReadFull(reader, remainingHeaderBytes); err != nil {
			logger.Errorf("读取完整帧头失败: %v", err)
			return nil, err
		}

		// 组合完整帧头
		header = append(compactHeader, remainingHeaderBytes...)
	}

	// 解码帧头
	frameHeader, err := p.DecodeHeader(header)
	if err != nil {
		logger.Errorf("解码帧头失败: %v", err)
		return nil, err
	}

	// 读取帧数据
	if frameHeader.Size == 0 {
		// 空消息特殊处理
		return []byte{}, nil
	}

	data := make([]byte, frameHeader.Size)
	if _, err := io.ReadFull(reader, data); err != nil {
		logger.Errorf("读取帧数据失败: %v", err)
		return nil, err
	}

	// 验证校验和（如果有）
	if frameHeader.Checksum != 0 {
		// 对于紧凑帧，校验和只有1字节，我们只比较最低字节
		if isCompact {
			actualChecksum := p.CalculateChecksum(data)
			// 只比较最低字节
			if byte(actualChecksum&0xFF) != byte(frameHeader.Checksum) {
				logger.Errorf("校验和不匹配: %v", actualChecksum)
				return nil, ErrChecksumMismatch
			}
		} else {
			if !p.VerifyData(data, frameHeader.Checksum) {
				logger.Errorf("校验和不匹配: %v", frameHeader.Checksum)
				return nil, ErrChecksumMismatch
			}
		}
	}

	return data, nil
}

// ReadFrameWithFlags 实现FrameProcessor接口，读取帧并返回内容与标志
func (p *defaultFrameProcessor) ReadFrameWithFlags(reader io.Reader) ([]byte, uint32, error) {
	// 为每次读取操作设置一个较为严格的超时上限（如果reader支持）
	if conn, ok := reader.(interface {
		SetReadDeadline(t time.Time) error
	}); ok {
		// 设置读取帧头的超时为5秒（通常应很快）
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		// 函数返回前清除超时
		defer conn.SetReadDeadline(time.Time{})
	}

	// 尝试读取紧凑帧头
	compactHeader := make([]byte, CompactHeaderSize)
	if _, err := io.ReadFull(reader, compactHeader); err != nil {
		logger.Errorf("读取紧凑帧头失败: %v", err)
		return nil, 0, err
	}

	// 检查是否为紧凑帧
	isCompact := (compactHeader[3] & FlagCompact) != 0

	var header []byte
	if isCompact {
		header = compactHeader
	} else {
		// 不是紧凑帧，需要读取完整帧头
		remainingHeaderBytes := make([]byte, DefaultHeaderSize-CompactHeaderSize)
		if _, err := io.ReadFull(reader, remainingHeaderBytes); err != nil {
			logger.Errorf("读取完整帧头失败: %v", err)
			return nil, 0, err
		}

		// 组合完整帧头
		header = append(compactHeader, remainingHeaderBytes...)
	}

	// 解码帧头
	frameHeader, err := p.DecodeHeader(header)
	if err != nil {
		logger.Errorf("解码帧头失败: %v", err)
		return nil, 0, err
	}

	// 提取帧标志信息
	flags := frameHeader.Flags

	// 处理空帧情况（可能是控制帧或最后一帧标记）
	if frameHeader.Size == 0 {
		return []byte{}, flags, nil
	}

	// 重置超时（如果reader支持），为大型数据帧提供更多时间
	if conn, ok := reader.(interface {
		SetReadDeadline(t time.Time) error
	}); ok {
		// 根据帧大小调整超时，最小30秒
		timeout := 30 * time.Second
		if frameHeader.Size > 1024*1024 { // 大于1MB
			// 每MB增加10秒超时
			additionalTimeout := time.Duration(frameHeader.Size/(1024*1024)) * 10 * time.Second
			if additionalTimeout > 5*time.Minute { // 最大5分钟
				additionalTimeout = 5 * time.Minute
			}
			timeout += additionalTimeout
		}
		conn.SetReadDeadline(time.Now().Add(timeout))
	}

	// 读取帧数据
	data := make([]byte, frameHeader.Size)
	if _, err := io.ReadFull(reader, data); err != nil {
		logger.Errorf("读取帧数据失败: %v", err)
		return nil, 0, err
	}

	// 验证校验和（如果有）
	if frameHeader.Checksum != 0 {
		// 对于紧凑帧，校验和只有1字节，我们只比较最低字节
		if isCompact {
			actualChecksum := p.CalculateChecksum(data)
			// 只比较最低字节
			if byte(actualChecksum&0xFF) != byte(frameHeader.Checksum) {
				logger.Errorf("校验和不匹配: 预期 %X, 实际 %X",
					byte(frameHeader.Checksum), byte(actualChecksum&0xFF))
				return nil, 0, ErrChecksumMismatch
			}
		} else {
			if !p.VerifyData(data, frameHeader.Checksum) {
				actualChecksum := p.CalculateChecksum(data)
				logger.Errorf("校验和不匹配: 预期 %X, 实际 %X",
					frameHeader.Checksum, actualChecksum)
				return nil, 0, ErrChecksumMismatch
			}
		}
	}

	return data, flags, nil
}

// WriteFrame 将数据写入帧
func (p *defaultFrameProcessor) WriteFrame(writer io.Writer, data []byte, flags uint32) error {
	// 计算校验和
	checksum := p.CalculateChecksum(data)

	// 确定是否使用紧凑帧
	useCompact := len(data) < 65536
	if useCompact {
		flags |= FlagCompact
	}

	// 编码帧头
	header, err := p.EncodeHeader(len(data), checksum, flags)
	if err != nil {
		logger.Errorf("编码帧头失败: %v", err)
		return err
	}

	// 写入帧头
	if _, err := writer.Write(header); err != nil {
		logger.Errorf("写入帧头失败: %v", err)
		return err
	}

	// 写入数据
	if len(data) > 0 {
		if _, err := writer.Write(data); err != nil {
			logger.Errorf("写入数据失败: %v", err)
			return err
		}
	}

	return nil
}
