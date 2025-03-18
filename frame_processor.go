package pointsub

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
)

// 帧头部常量
const (
	// 帧头部大小
	DefaultHeaderSize = 12 // 大小(4字节) + 校验和(4字节) + 标志(4字节)

	// 紧凑帧头部大小
	CompactHeaderSize = 4 // 大小(2字节) + 校验和(1字节) + 标志(1字节)

	// 帧标志
	FlagNormal     = 0x00 // 普通帧
	FlagFragmented = 0x01 // 分片帧
	FlagLastFrame  = 0x02 // 最后一帧
	FlagCompressed = 0x04 // 压缩帧
	FlagError      = 0x08 // 错误帧
	FlagControl    = 0x10 // 控制帧
	FlagEmpty      = 0x20 // 空帧
	FlagCompact    = 0x40 // 紧凑帧
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

	// 校验和
	Checksum uint32

	// 帧标志
	Flags uint32

	// 序列号
	Sequence uint16

	// 协议版本
	Version uint8
}

// FrameProcessor 处理消息帧的编码和解码
type FrameProcessor interface {
	// 编码消息帧头
	EncodeHeader(size int, checksum uint32, flags uint32) ([]byte, error)

	// 解码消息帧头
	DecodeHeader(header []byte) (FrameHeader, error)

	// 计算校验和
	CalculateChecksum(data []byte) uint32

	// 验证数据完整性
	VerifyData(data []byte, expectedChecksum uint32) bool

	// 读取帧
	ReadFrame(reader io.Reader) ([]byte, error)

	// 写入帧
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
