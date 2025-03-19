package pointsub

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"time"
)

// 将以下常量定义注释，因为已经移至buffer_constants.go
/*
// 消息大小常量
const (
	// 微小消息 (<1KB)
	TinyMessageSize = 1 * 1024

	// 小消息 (<16KB)
	SmallMessageSize = 16 * 1024

	// 中等消息 (<128KB)
	MediumMessageSize = 128 * 1024

	// 大消息 (<1MB)
	LargeMessageSize = 1 * 1024 * 1024

	// 超大消息 (>=1MB)
	HugeMessageSize = 1 * 1024 * 1024

	// 默认参数常量
	DefaultTinyChunkSize   = 512
	DefaultSmallChunkSize  = 4 * 1024
	DefaultMediumChunkSize = 16 * 1024
	DefaultLargeChunkSize  = 64 * 1024
	DefaultHugeChunkSize   = 256 * 1024

	DefaultReadBufferSize  = 8 * 1024
	DefaultWriteBufferSize = 8 * 1024
)
*/

// MessageSizer 负责消息大小检测和策略选择
type MessageSizer interface {
	// 根据消息大小选择最佳传输策略
	SelectStrategy(size int) TransportStrategy

	// 根据消息大小优化传输参数
	OptimizeParams(size int) TransportParams

	// 预估消息大小范围（用于未知大小的消息）
	EstimateSize(sample []byte, sampleSize int) int

	// 注册自定义策略
	RegisterStrategy(strategy TransportStrategy)
}

// TransportStrategy 定义传输策略接口
type TransportStrategy interface {
	// 策略名称
	Name() string

	// 适用的消息大小范围
	SizeRange() (min, max int)

	// 执行传输（发送）
	Send(conn net.Conn, data []byte, params TransportParams) error

	// 执行传输（接收）
	Receive(conn net.Conn, params TransportParams) ([]byte, error)
}

// TransportParams 存储传输参数
type TransportParams struct {
	// 分块大小
	ChunkSize int

	// 是否使用分块
	UseChunking bool

	// 分块间延迟
	BlockDelay time.Duration

	// 禁用块间延迟
	NoBlockDelay bool

	// 读缓冲区大小
	ReadBufferSize int

	// 写缓冲区大小
	WriteBufferSize int

	// 读超时
	ReadTimeout time.Duration

	// 写超时
	WriteTimeout time.Duration

	// 最大重试次数
	MaxRetries int

	// 启用校验和
	EnableChecksum bool
}

// defaultMessageSizer 实现了MessageSizer接口
type defaultMessageSizer struct {
	// 存储已注册的策略
	strategies []TransportStrategy
}

// NewDefaultMessageSizer 创建默认的消息大小检测器
func NewDefaultMessageSizer() MessageSizer {
	sizer := &defaultMessageSizer{
		strategies: make([]TransportStrategy, 0, 5),
	}

	// 注册默认策略
	sizer.RegisterStrategy(&tinyMessageStrategy{})
	sizer.RegisterStrategy(&smallMessageStrategy{})
	sizer.RegisterStrategy(&mediumMessageStrategy{})
	sizer.RegisterStrategy(&largeMessageStrategy{})
	sizer.RegisterStrategy(&hugeMessageStrategy{})

	return sizer
}

// SelectStrategy 根据消息大小选择最佳传输策略
func (s *defaultMessageSizer) SelectStrategy(size int) TransportStrategy {
	for _, strategy := range s.strategies {
		min, max := strategy.SizeRange()
		if (min <= size && size < max) || (min <= size && max == 0) {
			return strategy
		}
	}

	// 如果没有找到合适的策略，使用最后一个策略（通常是处理最大消息的策略）
	if len(s.strategies) > 0 {
		return s.strategies[len(s.strategies)-1]
	}

	// 没有任何策略时，创建一个默认的大消息策略
	return &largeMessageStrategy{}
}

// OptimizeParams 根据消息大小优化传输参数
func (s *defaultMessageSizer) OptimizeParams(size int) TransportParams {
	// 默认参数
	params := TransportParams{
		ChunkSize:       DefaultMediumChunkSize,
		UseChunking:     size > SmallMessageSize,
		BlockDelay:      time.Millisecond,
		NoBlockDelay:    false,
		ReadBufferSize:  DefaultReadBufferSize,
		WriteBufferSize: DefaultWriteBufferSize,
		ReadTimeout:     30 * time.Second,
		WriteTimeout:    30 * time.Second,
		MaxRetries:      3,
		EnableChecksum:  true,
	}

	// 根据消息大小调整参数
	if size < TinyMessageSize {
		// 微小消息 - 不使用分块，小缓冲区
		params.UseChunking = false
		params.ReadBufferSize = 1024
		params.WriteBufferSize = 1024
		params.ReadTimeout = 5 * time.Second
		params.WriteTimeout = 5 * time.Second

	} else if size < SmallMessageSize {
		// 小消息 - 不使用分块，小缓冲区
		params.UseChunking = false
		params.ReadBufferSize = 4 * 1024
		params.WriteBufferSize = 4 * 1024
		params.ReadTimeout = 10 * time.Second
		params.WriteTimeout = 10 * time.Second

	} else if size < MediumMessageSize {
		// 中等消息 - 可选分块，中等缓冲区
		params.UseChunking = true
		params.ChunkSize = DefaultSmallChunkSize
		params.ReadBufferSize = 16 * 1024
		params.WriteBufferSize = 16 * 1024
		params.ReadTimeout = 20 * time.Second
		params.WriteTimeout = 20 * time.Second

	} else if size < LargeMessageSize {
		// 大消息 - 使用分块，大缓冲区
		params.UseChunking = true
		params.ChunkSize = DefaultMediumChunkSize
		params.ReadBufferSize = 64 * 1024
		params.WriteBufferSize = 64 * 1024
		params.ReadTimeout = 60 * time.Second
		params.WriteTimeout = 60 * time.Second

	} else {
		// 超大消息 - 使用分块，超大缓冲区
		params.UseChunking = true
		params.ChunkSize = DefaultLargeChunkSize
		params.ReadBufferSize = 128 * 1024
		params.WriteBufferSize = 128 * 1024
		params.ReadTimeout = 120 * time.Second
		params.WriteTimeout = 120 * time.Second
	}

	return params
}

// EstimateSize 预估消息大小范围
func (s *defaultMessageSizer) EstimateSize(sample []byte, sampleSize int) int {
	if len(sample) == 0 {
		return 0
	}

	// 如果提供了实际大小，直接返回
	if sampleSize > 0 {
		return sampleSize
	}

	// 简单的大小估计，假设消息比样本大10倍
	// 在实际场景中，可能需要更复杂的算法
	estimatedSize := len(sample) * 10

	// 对超大估计值进行限制
	if estimatedSize > 10*1024*1024 {
		estimatedSize = 10 * 1024 * 1024
	}

	return estimatedSize
}

// RegisterStrategy 注册自定义策略
func (s *defaultMessageSizer) RegisterStrategy(strategy TransportStrategy) {
	// 检查是否已经存在相同的策略
	for i, existingStrategy := range s.strategies {
		if existingStrategy.Name() == strategy.Name() {
			// 替换现有策略
			s.strategies[i] = strategy
			return
		}
	}

	// 添加新策略
	s.strategies = append(s.strategies, strategy)

	// 根据大小范围排序（从小到大）
	for i := 0; i < len(s.strategies)-1; i++ {
		for j := i + 1; j < len(s.strategies); j++ {
			minI, _ := s.strategies[i].SizeRange()
			minJ, _ := s.strategies[j].SizeRange()
			if minI > minJ {
				s.strategies[i], s.strategies[j] = s.strategies[j], s.strategies[i]
			}
		}
	}
}

// ===========================================================================
// 微小消息策略实现 (< 1KB)
// ===========================================================================
type tinyMessageStrategy struct{}

func (s *tinyMessageStrategy) Name() string {
	return "TinyMessageStrategy"
}

func (s *tinyMessageStrategy) SizeRange() (min, max int) {
	return 0, TinyMessageSize
}

func (s *tinyMessageStrategy) Send(conn net.Conn, data []byte, params TransportParams) error {
	// 对于微小消息，直接一次性发送，不使用分块
	// 设置写超时
	if params.WriteTimeout > 0 {
		conn.SetWriteDeadline(time.Now().Add(params.WriteTimeout))
		defer conn.SetWriteDeadline(time.Time{})
	}

	_, err := conn.Write(data)
	return err
}

func (s *tinyMessageStrategy) Receive(conn net.Conn, params TransportParams) ([]byte, error) {
	// 设置读超时
	if params.ReadTimeout > 0 {
		conn.SetReadDeadline(time.Now().Add(params.ReadTimeout))
		defer conn.SetReadDeadline(time.Time{})
	}

	// 预先分配适当大小的缓冲区
	buffer := make([]byte, TinyMessageSize)
	n, err := conn.Read(buffer)
	if err != nil {
		logger.Errorf("读取微小消息失败: %v", err)
		return nil, err
	}

	// 返回实际读取的数据
	return buffer[:n], nil
}

// ===========================================================================
// 小消息策略实现 (1KB-16KB)
// ===========================================================================
type smallMessageStrategy struct{}

func (s *smallMessageStrategy) Name() string {
	return "SmallMessageStrategy"
}

func (s *smallMessageStrategy) SizeRange() (min, max int) {
	return TinyMessageSize, SmallMessageSize
}

func (s *smallMessageStrategy) Send(conn net.Conn, data []byte, params TransportParams) error {
	// 设置写超时
	if params.WriteTimeout > 0 {
		conn.SetWriteDeadline(time.Now().Add(params.WriteTimeout))
		defer conn.SetWriteDeadline(time.Time{})
	}

	// 为小消息创建一个简单的包装头(大小+1字节校验和)
	header := make([]byte, 3)
	header[0] = byte(len(data) >> 8)
	header[1] = byte(len(data))

	// 简单的校验和（对于小消息）
	var checksum byte
	for _, b := range data {
		checksum ^= b
	}
	header[2] = checksum

	// 先发送头部
	_, err := conn.Write(header)
	if err != nil {
		logger.Errorf("发送小消息头部失败: %v", err)
		return err
	}

	// 再发送数据
	_, err = conn.Write(data)
	return err
}

func (s *smallMessageStrategy) Receive(conn net.Conn, params TransportParams) ([]byte, error) {
	// 设置读超时
	if params.ReadTimeout > 0 {
		conn.SetReadDeadline(time.Now().Add(params.ReadTimeout))
		defer conn.SetReadDeadline(time.Time{})
	}

	// 先读取3字节头部
	header := make([]byte, 3)
	_, err := io.ReadFull(conn, header)
	if err != nil {
		logger.Errorf("读取小消息头部失败: %v", err)
		return nil, err
	}

	// 解析大小
	size := (int(header[0]) << 8) | int(header[1])
	expectedChecksum := header[2]

	// 检查大小是否合理
	if size <= 0 || size >= SmallMessageSize {
		return nil, ErrInvalidHeaderFormat
	}

	// 读取数据
	data := make([]byte, size)
	_, err = io.ReadFull(conn, data)
	if err != nil {
		logger.Errorf("读取小消息数据失败: %v", err)
		return nil, err
	}

	// 验证校验和
	if params.EnableChecksum {
		var checksum byte
		for _, b := range data {
			checksum ^= b
		}

		if checksum != expectedChecksum {
			return nil, ErrChecksumMismatch
		}
	}

	return data, nil
}

// ===========================================================================
// 中等消息策略实现 (16KB-128KB)
// ===========================================================================
type mediumMessageStrategy struct{}

func (s *mediumMessageStrategy) Name() string {
	return "MediumMessageStrategy"
}

func (s *mediumMessageStrategy) SizeRange() (min, max int) {
	return SmallMessageSize, MediumMessageSize
}

func (s *mediumMessageStrategy) Send(conn net.Conn, data []byte, params TransportParams) error {
	// 使用帧处理器发送数据
	frameProc := NewDefaultFrameProcessor(WithMaxFrameSize(MediumMessageSize))

	// 设置写超时
	if params.WriteTimeout > 0 {
		conn.SetWriteDeadline(time.Now().Add(params.WriteTimeout))
		defer conn.SetWriteDeadline(time.Time{})
	}

	// 如果使用分块且消息大于一定大小，则分块发送
	if params.UseChunking && len(data) > params.ChunkSize {
		// 分块发送
		for i := 0; i < len(data); i += params.ChunkSize {
			end := i + params.ChunkSize
			if end > len(data) {
				end = len(data)
			}

			chunk := data[i:end]
			flags := uint32(0)

			// 标记第一个和最后一个分块
			if i == 0 {
				flags |= FlagFragmented
			}
			if end == len(data) {
				flags |= FlagLastFrame
			}

			err := frameProc.WriteFrame(conn, chunk, flags)
			if err != nil {
				logger.Errorf("发送分块消息失败: %v", err)
				return err
			}

			// 分块间延迟
			if !params.NoBlockDelay && i+params.ChunkSize < len(data) {
				time.Sleep(params.BlockDelay)
			}
		}
		return nil
	}

	// 一次性发送小消息
	return frameProc.WriteFrame(conn, data, 0)
}

func (s *mediumMessageStrategy) Receive(conn net.Conn, params TransportParams) ([]byte, error) {
	// 使用帧处理器接收数据
	frameProc := NewDefaultFrameProcessor(WithMaxFrameSize(MediumMessageSize))

	// 设置读超时
	if params.ReadTimeout > 0 {
		conn.SetReadDeadline(time.Now().Add(params.ReadTimeout))
		defer conn.SetReadDeadline(time.Time{})
	}

	// 接收第一帧
	firstFrame, err := frameProc.ReadFrame(conn)
	if err != nil {
		logger.Errorf("读取中等消息第一帧失败: %v", err)
		return nil, err
	}

	// 解析帧头以检查是否为分片帧
	header := make([]byte, DefaultHeaderSize)
	if len(firstFrame) < len(header) {
		// 帧太小，不是有效帧
		return nil, ErrInvalidHeaderFormat
	}

	frameHeader, err := frameProc.DecodeHeader(header)
	if err != nil {
		logger.Errorf("解析中等消息帧头失败: %v", err)
		return nil, err
	}

	// 检查是否为分片帧
	if frameHeader.Flags&FlagFragmented != 0 {
		// 这是分片消息，需要接收所有分片
		var fullData []byte
		fullData = append(fullData, firstFrame...)

		// 继续接收其他分片，直到收到最后一帧
		for {
			if frameHeader.Flags&FlagLastFrame != 0 {
				// 已接收最后一帧
				break
			}

			// 读下一帧
			if params.ReadTimeout > 0 {
				conn.SetReadDeadline(time.Now().Add(params.ReadTimeout))
			}

			nextFrame, err := frameProc.ReadFrame(conn)
			if err != nil {
				logger.Errorf("读取中等消息下一帧失败: %v", err)
				return nil, err
			}

			// 解析帧头
			if len(nextFrame) < len(header) {
				return nil, ErrInvalidHeaderFormat
			}

			frameHeader, err = frameProc.DecodeHeader(header)
			if err != nil {
				logger.Errorf("解析中等消息帧头失败: %v", err)
				return nil, err
			}

			// 添加帧数据
			fullData = append(fullData, nextFrame...)
		}

		return fullData, nil
	}

	// 非分片消息，直接返回
	return firstFrame, nil
}

// ===========================================================================
// 大消息策略实现 (128KB-1MB)
// ===========================================================================
type largeMessageStrategy struct{}

func (s *largeMessageStrategy) Name() string {
	return "LargeMessageStrategy"
}

func (s *largeMessageStrategy) SizeRange() (min, max int) {
	return MediumMessageSize, LargeMessageSize
}

func (s *largeMessageStrategy) Send(conn net.Conn, data []byte, params TransportParams) error {
	// 对于大消息，使用大消息连接优化
	lmConn := NewLargeMessageConn(conn, len(data),
		WithChunkSize(params.ChunkSize),
		WithAdaptiveChunking(true),
		WithBufferSizes(params.ReadBufferSize, params.WriteBufferSize),
		WithBlockDelay(params.BlockDelay),
	)

	// 设置写超时
	if params.WriteTimeout > 0 {
		lmConn.SetWriteDeadline(time.Now().Add(params.WriteTimeout))
		defer lmConn.SetWriteDeadline(time.Time{})
	}

	// 使用优化连接发送数据
	_, err := lmConn.Write(data)
	return err
}

func (s *largeMessageStrategy) Receive(conn net.Conn, params TransportParams) ([]byte, error) {
	// 对于大消息接收，使用大消息连接优化
	lmConn := NewLargeMessageConn(conn, LargeMessageSize,
		WithChunkSize(params.ChunkSize),
		WithAdaptiveChunking(true),
		WithBufferSizes(params.ReadBufferSize, params.WriteBufferSize),
		WithBlockDelay(params.BlockDelay),
	)

	// 设置读超时
	if params.ReadTimeout > 0 {
		lmConn.SetReadDeadline(time.Now().Add(params.ReadTimeout))
		defer lmConn.SetReadDeadline(time.Time{})
	}

	// 使用缓冲区逐步读取数据
	var buffer bytes.Buffer
	chunk := make([]byte, params.ChunkSize)

	for {
		n, err := lmConn.Read(chunk)
		if n > 0 {
			buffer.Write(chunk[:n])
		}

		if err == io.EOF {
			break
		}

		if err != nil {
			logger.Errorf("读取大消息失败: %v", err)
			return nil, err
		}

		// 检查是否超过大小限制
		if buffer.Len() > LargeMessageSize {
			return nil, ErrFrameTooBig
		}
	}

	return buffer.Bytes(), nil
}

// ===========================================================================
// 超大消息策略实现 (>1MB)
// ===========================================================================
type hugeMessageStrategy struct{}

func (s *hugeMessageStrategy) Name() string {
	return "HugeMessageStrategy"
}

func (s *hugeMessageStrategy) SizeRange() (min, max int) {
	return LargeMessageSize, 0 // 0表示无上限
}

func (s *hugeMessageStrategy) Send(conn net.Conn, data []byte, params TransportParams) error {
	// 对于超大消息，使用流式处理和进度跟踪
	// 创建一个带进度跟踪的读取器
	dataReader := bytes.NewReader(data)

	// 使用帧处理器
	frameProc := NewDefaultFrameProcessor()

	// 生成传输ID用于跟踪 - 目前仅作日志使用，不返回

	// 设置写超时
	if params.WriteTimeout > 0 {
		conn.SetWriteDeadline(time.Now().Add(params.WriteTimeout))
		defer conn.SetWriteDeadline(time.Time{})
	}

	// 首先发送一个头部帧，包含总大小信息
	headerData := make([]byte, 8)
	binary.BigEndian.PutUint64(headerData, uint64(len(data)))
	err := frameProc.WriteFrame(conn, headerData, FlagFragmented)
	if err != nil {
		logger.Errorf("发送超大消息头部失败: %v", err)
		return err
	}

	// 分块读取并发送数据
	buffer := make([]byte, params.ChunkSize)
	totalSent := 0

	for {
		n, err := dataReader.Read(buffer)
		if n > 0 {
			// 确定帧标志
			flags := uint32(0)
			if totalSent == 0 {
				flags |= FlagFragmented
			}
			if totalSent+n == len(data) {
				flags |= FlagLastFrame
			}

			// 发送这个块
			err := frameProc.WriteFrame(conn, buffer[:n], flags)
			if err != nil {
				logger.Errorf("发送超大消息块失败: %v", err)
				return err
			}

			totalSent += n

			// 分块间延迟
			if !params.NoBlockDelay && totalSent < len(data) {
				time.Sleep(params.BlockDelay)
			}
		}

		if err == io.EOF {
			break
		}

		if err != nil {
			logger.Errorf("发送超大消息块失败: %v", err)
			return err
		}
	}

	return nil
}

func (s *hugeMessageStrategy) Receive(conn net.Conn, params TransportParams) ([]byte, error) {
	// 使用帧处理器
	frameProc := NewDefaultFrameProcessor()

	// 设置读超时
	if params.ReadTimeout > 0 {
		conn.SetReadDeadline(time.Now().Add(params.ReadTimeout))
		defer conn.SetReadDeadline(time.Time{})
	}

	// 先读取头部帧获取总大小
	headerFrame, err := frameProc.ReadFrame(conn)
	if err != nil {
		logger.Errorf("读取超大消息头部失败: %v", err)
		return nil, err
	}

	if len(headerFrame) < 8 {
		return nil, ErrInvalidHeaderFormat
	}

	// 解析总大小
	totalSize := int(binary.BigEndian.Uint64(headerFrame))

	// 检查大小是否合理
	if totalSize <= 0 || (params.MaxRetries > 0 && totalSize > params.MaxRetries) {
		return nil, ErrFrameTooBig
	}

	// 预分配足够大的缓冲区
	var buffer bytes.Buffer
	buffer.Grow(totalSize)

	// 循环接收所有数据帧
	receivedBytes := 0

	for receivedBytes < totalSize {
		// 重新设置超时
		if params.ReadTimeout > 0 {
			conn.SetReadDeadline(time.Now().Add(params.ReadTimeout))
		}

		// 读取下一帧
		dataFrame, err := frameProc.ReadFrame(conn)
		if err != nil {
			logger.Errorf("读取超大消息下一帧失败: %v", err)
			return nil, err
		}

		// 添加到缓冲区
		buffer.Write(dataFrame)
		receivedBytes += len(dataFrame)

		// 检查是否已接收所有数据
		if receivedBytes >= totalSize {
			break
		}
	}

	// 校验接收的数据大小
	if buffer.Len() != totalSize {
		return nil, errors.New("接收的数据大小与预期不符")
	}

	return buffer.Bytes(), nil
}

// ValidateChunkSize 验证分块大小是否合理
func ValidateChunkSize(chunkSize int) bool {
	return chunkSize >= 64 && chunkSize <= 1024*1024
}
