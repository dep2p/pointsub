package pointsub

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// 消息优先级
type MessagePriority int

const (
	// 低优先级
	PriorityLow MessagePriority = iota

	// 普通优先级
	PriorityNormal

	// 高优先级
	PriorityHigh

	// 紧急优先级
	PriorityCritical
)

// 消息类型
type MessageType int

const (
	// 数据消息
	MessageTypeData MessageType = iota

	// 控制消息
	MessageTypeControl

	// 心跳消息
	MessageTypeHeartbeat

	// 回执消息
	MessageTypeAck

	// 错误消息
	MessageTypeError
)

// 消息标志位
const (
	// 压缩消息
	MessageFlagCompressed uint8 = 1 << iota

	// 加密消息
	MessageFlagEncrypted

	// 分片消息
	MessageFlagFragmented

	// 带签名消息
	MessageFlagSigned

	// 需要回执
	MessageFlagAckRequired

	// 重传消息
	MessageFlagRetransmitted
)

// 消息状态
type MessageStatus int

const (
	// 已创建
	MessageStatusCreated MessageStatus = iota

	// 等待发送
	MessageStatusPending

	// 正在发送
	MessageStatusSending

	// 已发送
	MessageStatusSent

	// 已收到回执
	MessageStatusAcknowledged

	// 发送失败
	MessageStatusFailed

	// 已取消
	MessageStatusCancelled
)

// MessageHeader 消息头部
type MessageHeader struct {
	// 消息版本号
	Version uint8

	// 消息优先级
	Priority MessagePriority

	// 消息类型
	Type MessageType

	// 消息标志位
	Flags uint8

	// 消息ID
	ID uint32

	// 消息体长度
	BodyLength uint32

	// 消息时间戳
	Timestamp int64

	// 消息TTL (以秒为单位)
	TTL uint16

	// 消息路由键
	RoutingKeyLength uint8

	// 检验和
	Checksum uint32
}

// Message 完整消息
type Message struct {
	// 消息头部
	Header MessageHeader

	// 路由键
	RoutingKey string

	// 消息体
	Body []byte

	// 消息状态
	Status MessageStatus

	// 上下文，用于取消等操作
	Context context.Context

	// 取消函数
	CancelFunc context.CancelFunc

	// 创建时间
	CreatedAt time.Time

	// 更新时间
	UpdatedAt time.Time

	// 重试次数
	RetryCount int

	// 下一次重试时间
	NextRetry time.Time

	// 原始数据缓存
	rawData []byte
}

// MessageProcessor 消息处理器接口
type MessageProcessor interface {
	// 编码消息为二进制数据
	EncodeMessage(msg *Message) ([]byte, error)

	// 解码二进制数据为消息
	DecodeMessage(data []byte) (*Message, error)

	// 验证消息有效性
	ValidateMessage(msg *Message) error

	// 读取消息
	ReadMessage(reader io.Reader) (*Message, error)

	// 写入消息
	WriteMessage(writer io.Writer, msg *Message) error

	// 批量处理消息
	ProcessMessages(msgs []*Message) ([]*Message, error)

	// 应用过滤器
	ApplyFilters(msg *Message) bool

	// 设置消息处理中间件
	SetMiddleware(middleware MessageMiddleware)
}

// MessageMiddleware 消息处理中间件
type MessageMiddleware interface {
	// 前置处理
	PreProcess(msg *Message) (*Message, error)

	// 后置处理
	PostProcess(msg *Message) (*Message, error)
}

// MessageFilter 消息过滤器
type MessageFilter func(*Message) bool

// 默认消息处理器
type defaultMessageProcessor struct {
	// 最大消息大小
	maxBodySize int

	// 缓冲区管理器
	bufferManager AdaptiveBuffer

	// 消息头部大小
	headerSize int

	// 消息中间件
	middleware MessageMiddleware

	// 消息过滤器
	filters []MessageFilter

	// 消息池
	messagePool *sync.Pool

	// 缓冲区池
	bufferPool *sync.Pool

	// 消息处理计数器
	processCounter uint64

	// 消息重用率
	reuseRate float64

	// 缓冲区命中率
	bufferHitRate float64
}

// MessageProcessorOption 定义处理器选项
type MessageProcessorOption func(*defaultMessageProcessor)

// WithMaxBodySize 设置最大消息体大小
func WithMaxBodySize(maxSize int) MessageProcessorOption {
	return func(p *defaultMessageProcessor) {
		p.maxBodySize = maxSize
	}
}

// WithBufferManager 设置缓冲区管理器
func WithBufferManager(manager AdaptiveBuffer) MessageProcessorOption {
	return func(p *defaultMessageProcessor) {
		p.bufferManager = manager
	}
}

// WithMessageMiddleware The middleware that processes the messages
func WithMessageMiddleware(middleware MessageMiddleware) MessageProcessorOption {
	return func(p *defaultMessageProcessor) {
		p.middleware = middleware
	}
}

// WithMessageFilter 添加消息过滤器
func WithMessageFilter(filter MessageFilter) MessageProcessorOption {
	return func(p *defaultMessageProcessor) {
		p.filters = append(p.filters, filter)
	}
}

// 创建新的消息处理器
func NewDefaultMessageProcessor(options ...MessageProcessorOption) MessageProcessor {
	proc := &defaultMessageProcessor{
		maxBodySize:    10 * 1024 * 1024, // 默认10MB
		headerSize:     24,               // 基本头部大小
		filters:        make([]MessageFilter, 0),
		processCounter: 0,
		reuseRate:      0.0,
		bufferHitRate:  0.0,
		messagePool: &sync.Pool{
			New: func() interface{} {
				return &Message{
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				}
			},
		},
		bufferPool: &sync.Pool{
			New: func() interface{} {
				return bytes.NewBuffer(make([]byte, 0, 1024))
			},
		},
	}

	// 应用选项
	for _, opt := range options {
		opt(proc)
	}

	// 如果没有设置缓冲区管理器，创建默认管理器
	if proc.bufferManager == nil {
		proc.bufferManager = NewDefaultAdaptiveBuffer()
	}

	return proc
}

// EncodeMessage 实现MessageProcessor接口
func (p *defaultMessageProcessor) EncodeMessage(msg *Message) ([]byte, error) {
	// 参数验证
	if msg == nil {
		return nil, errors.New("消息为空")
	}

	// 应用中间件前置处理
	if p.middleware != nil {
		processedMsg, err := p.middleware.PreProcess(msg)
		if err != nil {
			logger.Errorf("前置处理消息失败: %v", err)
			return nil, err
		}
		msg = processedMsg
	}

	// 计算路由键长度
	routingKeyLen := len(msg.RoutingKey)
	if routingKeyLen > 255 {
		return nil, errors.New("路由键过长")
	}
	msg.Header.RoutingKeyLength = uint8(routingKeyLen)

	// 计算消息体长度
	msg.Header.BodyLength = uint32(len(msg.Body))

	// 验证消息体大小
	if msg.Header.BodyLength > uint32(p.maxBodySize) {
		return nil, errors.New("消息体超过最大限制")
	}

	// 设置时间戳
	if msg.Header.Timestamp == 0 {
		msg.Header.Timestamp = time.Now().UnixNano()
	}

	// 从对象池获取缓冲区
	buffer := p.bufferPool.Get().(*bytes.Buffer)
	buffer.Reset()
	defer p.bufferPool.Put(buffer)

	// 编码头部
	// 版本 + 优先级 + 类型 (1字节)
	versionAndPriority := (msg.Header.Version << 4) | uint8(msg.Header.Priority&0x0F)
	buffer.WriteByte(versionAndPriority)

	// 类型 + 标志 (1字节)
	typeAndFlags := (uint8(msg.Header.Type&0x0F) << 4) | (msg.Header.Flags & 0x0F)
	buffer.WriteByte(typeAndFlags)

	// 消息ID (4字节)
	idBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(idBytes, msg.Header.ID)
	buffer.Write(idBytes)

	// 消息体长度 (4字节)
	bodyLenBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(bodyLenBytes, msg.Header.BodyLength)
	buffer.Write(bodyLenBytes)

	// 时间戳 (8字节)
	timestampBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(timestampBytes, uint64(msg.Header.Timestamp))
	buffer.Write(timestampBytes)

	// TTL (2字节)
	ttlBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(ttlBytes, msg.Header.TTL)
	buffer.Write(ttlBytes)

	// 路由键长度 (1字节)
	buffer.WriteByte(msg.Header.RoutingKeyLength)

	// 计算头部校验和并写入 (4字节)
	headerData := buffer.Bytes()
	checksum := crc32.ChecksumIEEE(headerData)
	checksumBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(checksumBytes, checksum)
	buffer.Write(checksumBytes)

	// 写入路由键
	if routingKeyLen > 0 {
		buffer.WriteString(msg.RoutingKey)
	}

	// 写入消息体
	if msg.Header.BodyLength > 0 {
		buffer.Write(msg.Body)
	}

	// 复制结果
	result := make([]byte, buffer.Len())
	copy(result, buffer.Bytes())

	// 应用中间件后置处理
	if p.middleware != nil {
		processedMsg, err := p.middleware.PostProcess(msg)
		if err != nil {
			logger.Errorf("后置处理消息失败: %v", err)
			return nil, err
		}
		msg = processedMsg
	}

	// 缓存原始数据
	msg.rawData = result

	return result, nil
}

// DecodeMessage 实现MessageProcessor接口
func (p *defaultMessageProcessor) DecodeMessage(data []byte) (*Message, error) {
	// 参数验证
	if len(data) < p.headerSize {
		return nil, errors.New("数据长度不足以包含消息头")
	}

	// 从对象池获取消息对象
	msg := p.messagePool.Get().(*Message)
	msg.CreatedAt = time.Now()
	msg.UpdatedAt = time.Now()
	msg.Status = MessageStatusCreated
	msg.RetryCount = 0
	msg.rawData = nil

	// 解析头部
	// 版本 + 优先级
	versionAndPriority := data[0]
	msg.Header.Version = versionAndPriority >> 4
	msg.Header.Priority = MessagePriority(versionAndPriority & 0x0F)

	// 类型 + 标志
	typeAndFlags := data[1]
	msg.Header.Type = MessageType((typeAndFlags >> 4) & 0x0F)
	msg.Header.Flags = typeAndFlags & 0x0F

	// 消息ID
	msg.Header.ID = binary.BigEndian.Uint32(data[2:6])

	// 消息体长度
	msg.Header.BodyLength = binary.BigEndian.Uint32(data[6:10])

	// 时间戳
	msg.Header.Timestamp = int64(binary.BigEndian.Uint64(data[10:18]))

	// TTL
	msg.Header.TTL = binary.BigEndian.Uint16(data[18:20])

	// 路由键长度
	msg.Header.RoutingKeyLength = data[20]

	// 校验和
	expectedChecksum := binary.BigEndian.Uint32(data[21:25])

	// 计算校验和
	actualChecksum := crc32.ChecksumIEEE(data[:21])
	if actualChecksum != expectedChecksum {
		p.messagePool.Put(msg) // 回收对象
		return nil, errors.New("消息头校验和不匹配")
	}

	// 解析路由键
	headerSize := p.headerSize
	routingKeyEnd := headerSize + int(msg.Header.RoutingKeyLength)
	if len(data) < routingKeyEnd {
		p.messagePool.Put(msg) // 回收对象
		return nil, errors.New("数据长度不足以包含路由键")
	}

	if msg.Header.RoutingKeyLength > 0 {
		msg.RoutingKey = string(data[headerSize:routingKeyEnd])
	} else {
		msg.RoutingKey = ""
	}

	// 解析消息体
	bodyStart := routingKeyEnd
	expectedEnd := bodyStart + int(msg.Header.BodyLength)

	if len(data) < expectedEnd {
		p.messagePool.Put(msg) // 回收对象
		return nil, errors.New("数据长度不足以包含消息体")
	}

	if msg.Header.BodyLength > 0 {
		// 复制消息体
		msg.Body = make([]byte, msg.Header.BodyLength)
		copy(msg.Body, data[bodyStart:expectedEnd])
	} else {
		msg.Body = nil
	}

	// 缓存原始数据
	msg.rawData = data

	// 应用中间件
	if p.middleware != nil {
		processedMsg, err := p.middleware.PostProcess(msg)
		if err != nil {
			p.messagePool.Put(msg) // 回收对象
			logger.Errorf("后置处理消息失败: %v", err)
			return nil, err
		}
		msg = processedMsg
	}

	// 增加处理计数
	atomic.AddUint64(&p.processCounter, 1)

	return msg, nil
}

// ValidateMessage 实现MessageProcessor接口
func (p *defaultMessageProcessor) ValidateMessage(msg *Message) error {
	// 参数验证
	if msg == nil {
		return errors.New("消息为空")
	}

	// 检查消息版本
	if msg.Header.Version == 0 {
		return errors.New("无效的消息版本")
	}

	// 检查消息大小
	if msg.Header.BodyLength > uint32(p.maxBodySize) {
		return errors.New("消息体超过最大限制")
	}

	// 检查消息体长度与实际长度是否匹配
	if msg.Header.BodyLength != uint32(len(msg.Body)) {
		return errors.New("消息体长度与头部声明不符")
	}

	// 检查路由键长度与实际长度是否匹配
	if msg.Header.RoutingKeyLength != uint8(len(msg.RoutingKey)) {
		return errors.New("路由键长度与头部声明不符")
	}

	// 检查消息TTL
	if msg.Header.TTL > 0 {
		// 计算消息已存在时间（秒）
		messageAge := time.Since(time.Unix(0, msg.Header.Timestamp)).Seconds()
		if messageAge > float64(msg.Header.TTL) {
			return errors.New("消息已过期")
		}
	}

	return nil
}

// ReadMessage 实现MessageProcessor接口
func (p *defaultMessageProcessor) ReadMessage(reader io.Reader) (*Message, error) {
	// 参数验证
	if reader == nil {
		return nil, errors.New("读取器为空")
	}

	// 使用缓冲读取器提高效率
	var bufReader *bufio.Reader
	if br, ok := reader.(*bufio.Reader); ok {
		bufReader = br
	} else {
		// 创建自适应缓冲区
		bufReader = p.bufferManager.GetReadBuffer(8192) // 初始估计大小
		bufReader.Reset(reader)
		defer func() {
			// 这里应该有缓冲区回收逻辑
		}()
	}

	// 读取头部
	headerData := make([]byte, p.headerSize)
	_, err := io.ReadFull(bufReader, headerData)
	if err != nil {
		logger.Errorf("读取头部失败: %v", err)
		return nil, err
	}

	// 解析基本头部信息以确定完整消息大小
	if len(headerData) < 21 {
		return nil, errors.New("头部数据不完整")
	}

	// 获取消息体长度和路由键长度
	bodyLength := binary.BigEndian.Uint32(headerData[6:10])
	routingKeyLength := headerData[20]

	// 计算完整消息大小
	totalSize := p.headerSize + int(routingKeyLength) + int(bodyLength)

	// 分配完整消息缓冲区
	messageData := make([]byte, totalSize)

	// 复制已读取的头部
	copy(messageData, headerData)

	// 读取剩余数据
	_, err = io.ReadFull(bufReader, messageData[p.headerSize:])
	if err != nil {
		logger.Errorf("读取剩余数据失败: %v", err)
		return nil, err
	}

	// 解码完整消息
	return p.DecodeMessage(messageData)
}

// WriteMessage 实现MessageProcessor接口
func (p *defaultMessageProcessor) WriteMessage(writer io.Writer, msg *Message) error {
	// 参数验证
	if writer == nil {
		return errors.New("写入器为空")
	}
	if msg == nil {
		return errors.New("消息为空")
	}

	// 如果已经有编码后的数据，直接使用
	if msg.rawData != nil {
		_, err := writer.Write(msg.rawData)
		return err
	}

	// 否则编码消息
	data, err := p.EncodeMessage(msg)
	if err != nil {
		logger.Errorf("编码消息失败: %v", err)
		return err
	}

	// 写入数据
	_, err = writer.Write(data)
	return err
}

// ProcessMessages 实现MessageProcessor接口
func (p *defaultMessageProcessor) ProcessMessages(msgs []*Message) ([]*Message, error) {
	if len(msgs) == 0 {
		return nil, nil
	}

	results := make([]*Message, 0, len(msgs))

	// 处理每个消息
	for _, msg := range msgs {
		// 验证消息
		err := p.ValidateMessage(msg)
		if err != nil {
			continue
		}

		// 应用过滤器
		if !p.ApplyFilters(msg) {
			continue
		}

		// 应用中间件前置处理
		if p.middleware != nil {
			processedMsg, err := p.middleware.PreProcess(msg)
			if err != nil {
				continue
			}
			msg = processedMsg
		}

		// TODO: 实现消息处理逻辑

		// 应用中间件后置处理
		if p.middleware != nil {
			processedMsg, err := p.middleware.PostProcess(msg)
			if err != nil {
				continue
			}
			msg = processedMsg
		}

		results = append(results, msg)
	}

	return results, nil
}

// ApplyFilters 实现MessageProcessor接口
func (p *defaultMessageProcessor) ApplyFilters(msg *Message) bool {
	if msg == nil || len(p.filters) == 0 {
		return true
	}

	// 应用所有过滤器
	for _, filter := range p.filters {
		if !filter(msg) {
			return false
		}
	}

	return true
}

// SetMiddleware 实现MessageProcessor接口
func (p *defaultMessageProcessor) SetMiddleware(middleware MessageMiddleware) {
	p.middleware = middleware
}

// ReleaseMessage 将消息对象返回到对象池
func (p *defaultMessageProcessor) ReleaseMessage(msg *Message) {
	if msg == nil {
		return
	}

	// 清空数据以便重用
	msg.Header = MessageHeader{}
	msg.RoutingKey = ""
	msg.Body = nil
	msg.Status = MessageStatusCreated
	msg.Context = nil
	msg.CancelFunc = nil
	msg.RetryCount = 0
	msg.NextRetry = time.Time{}
	msg.rawData = nil

	// 返回对象池
	p.messagePool.Put(msg)
}
