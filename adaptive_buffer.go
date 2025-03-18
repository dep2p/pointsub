package pointsub

import (
	"bufio"
	"io"
	"runtime"
	"sync"
	"sync/atomic"
)

// 缓冲区大小常量
const (
	// SmallBufferSize 定义小缓冲区大小
	SmallBufferSize = 4 * 1024 // 4KB

	// MediumBufferSize 定义中缓冲区大小
	MediumBufferSize = 32 * 1024 // 32KB

	// LargeBufferSize 定义大缓冲区大小
	LargeBufferSize = 256 * 1024 // 256KB

	// MaxBufferLimit 定义最大缓冲区大小限制
	MaxBufferLimit = 2 * 1024 * 1024 // 2MB

	// LowMemoryPressure 定义低内存压力阈值
	LowMemoryPressure = 70 // 70% 系统内存使用率 - 低压力
	// MediumMemoryPressure 定义中等内存压力阈值
	MediumMemoryPressure = 80 // 80% 系统内存使用率 - 中等压力
	// HighMemoryPressure 定义高内存压力阈值
	HighMemoryPressure = 90 // 90% 系统内存使用率 - 高压力
)

// BufferStats 缓冲区统计信息
type BufferStats struct {
	// 读缓冲区命中率
	ReadHitRate float64

	// 写缓冲区命中率
	WriteHitRate float64

	// 平均读缓冲大小
	AvgReadSize int

	// 平均写缓冲大小
	AvgWriteSize int

	// 内存压力级别 (0-低, 1-中, 2-高)
	MemoryPressure int
}

// AdaptiveBuffer 提供智能缓冲管理
type AdaptiveBuffer interface {
	// 获取适合当前消息大小的读缓冲区
	GetReadBuffer(size int) *bufio.Reader

	// 获取适合当前消息大小的写缓冲区
	GetWriteBuffer(size int) *bufio.Writer

	// 监控并调整缓冲区大小
	AdjustBufferSizes(stats BufferStats)

	// 重置缓冲区
	Reset()

	// 获取当前缓冲区统计
	GetStats() BufferStats

	// 创建IO读缓冲区
	WrapReader(r io.Reader, size int) *bufio.Reader

	// 创建IO写缓冲区
	WrapWriter(w io.Writer, size int) *bufio.Writer
}

// 默认自适应缓冲区实现
type defaultAdaptiveBuffer struct {
	// 小缓冲区池 (<8KB)
	smallBuffers sync.Pool

	// 中等缓冲区池 (8KB-64KB)
	mediumBuffers sync.Pool

	// 大缓冲区池 (64KB-1MB)
	largeBuffers sync.Pool

	// 最大内存限制
	maxMemory int64

	// 当前分配的内存
	allocatedMem int64

	// 内存压力检测
	pressureLevel int

	// 缓冲区统计
	stats BufferStats

	// 同步保护
	mu sync.RWMutex
}

// AdaptiveBufferOption 定义缓冲区选项
type AdaptiveBufferOption func(*defaultAdaptiveBuffer)

// WithAdaptiveMaxMemory 设置最大内存限制
func WithAdaptiveMaxMemory(maxMem int64) AdaptiveBufferOption {
	return func(b *defaultAdaptiveBuffer) {
		b.maxMemory = maxMem
	}
}

// WithPoolSizes 设置自定义缓冲区池大小
func WithPoolSizes(small, medium, large int) AdaptiveBufferOption {
	return func(b *defaultAdaptiveBuffer) {
		// 重新创建缓冲池
		b.smallBuffers = sync.Pool{
			New: func() interface{} {
				buf := make([]byte, small)
				return buf
			},
		}
		b.mediumBuffers = sync.Pool{
			New: func() interface{} {
				buf := make([]byte, medium)
				return buf
			},
		}
		b.largeBuffers = sync.Pool{
			New: func() interface{} {
				buf := make([]byte, large)
				return buf
			},
		}
	}
}

// NewDefaultAdaptiveBuffer 创建默认自适应缓冲区管理器
func NewDefaultAdaptiveBuffer(options ...AdaptiveBufferOption) AdaptiveBuffer {
	buffer := &defaultAdaptiveBuffer{
		smallBuffers: sync.Pool{
			New: func() interface{} {
				buf := make([]byte, SmallBufferSize)
				return buf
			},
		},
		mediumBuffers: sync.Pool{
			New: func() interface{} {
				buf := make([]byte, MediumBufferSize)
				return buf
			},
		},
		largeBuffers: sync.Pool{
			New: func() interface{} {
				buf := make([]byte, LargeBufferSize)
				return buf
			},
		},
		maxMemory:     100 * 1024 * 1024, // 100MB
		allocatedMem:  0,
		pressureLevel: 0,
		stats: BufferStats{
			ReadHitRate:  1.0,
			WriteHitRate: 1.0,
			AvgReadSize:  SmallBufferSize,
			AvgWriteSize: SmallBufferSize,
		},
	}

	// 应用选项
	for _, opt := range options {
		opt(buffer)
	}

	return buffer
}

// 根据消息大小获取合适的缓冲区
func (b *defaultAdaptiveBuffer) getBuffer(size int) []byte {
	// 检查内存压力
	memPressure := b.detectMemoryPressure()
	if memPressure != b.pressureLevel {
		b.mu.Lock()
		b.pressureLevel = memPressure
		b.mu.Unlock()
	}

	// 根据内存压力调整分配策略
	switch {
	case size <= SmallBufferSize || b.pressureLevel >= 2:
		// 高内存压力或小消息使用小缓冲区
		buf := b.smallBuffers.Get().([]byte)
		atomic.AddInt64(&b.allocatedMem, int64(cap(buf)))
		return buf[:size]

	case size <= MediumBufferSize || b.pressureLevel >= 1:
		// 中等内存压力或中等消息使用中缓冲区
		buf := b.mediumBuffers.Get().([]byte)
		atomic.AddInt64(&b.allocatedMem, int64(cap(buf)))
		return buf[:size]

	default:
		// 低内存压力和大消息使用大缓冲区
		buf := b.largeBuffers.Get().([]byte)
		atomic.AddInt64(&b.allocatedMem, int64(cap(buf)))
		return buf[:size]
	}
}

// 释放缓冲区
func (b *defaultAdaptiveBuffer) releaseBuffer(buf []byte) {
	atomic.AddInt64(&b.allocatedMem, -int64(cap(buf)))

	// 根据容量将缓冲区放回合适的池
	switch cap(buf) {
	case SmallBufferSize:
		b.smallBuffers.Put(buf[:cap(buf)])
	case MediumBufferSize:
		b.mediumBuffers.Put(buf[:cap(buf)])
	case LargeBufferSize:
		b.largeBuffers.Put(buf[:cap(buf)])
	}
}

// 检测系统内存压力
func (b *defaultAdaptiveBuffer) detectMemoryPressure() int {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// 计算系统内存使用率（百分比）
	usedPercent := float64(memStats.Alloc) / float64(memStats.Sys) * 100

	// 判断内存压力级别
	switch {
	case usedPercent >= HighMemoryPressure:
		return 2 // 高压力
	case usedPercent >= MediumMemoryPressure:
		return 1 // 中等压力
	default:
		return 0 // 低压力
	}
}

// GetReadBuffer 实现AdaptiveBuffer接口
func (b *defaultAdaptiveBuffer) GetReadBuffer(size int) *bufio.Reader {
	// 确定合适的缓冲区大小
	bufferSize := b.calculateBufferSize(size, true)

	// 创建reader
	return bufio.NewReaderSize(nil, bufferSize)
}

// GetWriteBuffer 实现AdaptiveBuffer接口
func (b *defaultAdaptiveBuffer) GetWriteBuffer(size int) *bufio.Writer {
	// 确定合适的缓冲区大小
	bufferSize := b.calculateBufferSize(size, false)

	// 创建writer
	return bufio.NewWriterSize(nil, bufferSize)
}

// 计算合适的缓冲区大小
func (b *defaultAdaptiveBuffer) calculateBufferSize(dataSize int, isRead bool) int {
	// 根据内存压力和数据大小计算缓冲区大小
	var baseSizeRatio float64

	switch b.pressureLevel {
	case 0: // 低压力
		baseSizeRatio = 0.25 // 缓冲区大小为数据大小的1/4
	case 1: // 中等压力
		baseSizeRatio = 0.125 // 缓冲区大小为数据大小的1/8
	case 2: // 高压力
		baseSizeRatio = 0.0625 // 缓冲区大小为数据大小的1/16
	}

	// 为读缓冲区分配稍大的空间
	if isRead {
		baseSizeRatio *= 1.5
	}

	// 计算建议的缓冲区大小
	suggestedSize := int(float64(dataSize) * baseSizeRatio)

	// 根据大小范围调整
	switch {
	case suggestedSize <= 1024:
		return 1024 // 最小1KB
	case suggestedSize <= SmallBufferSize:
		return SmallBufferSize
	case suggestedSize <= MediumBufferSize:
		return MediumBufferSize
	case suggestedSize <= LargeBufferSize:
		return LargeBufferSize
	default:
		// 限制最大大小
		if suggestedSize > MaxBufferLimit {
			return MaxBufferLimit
		}
		return suggestedSize
	}
}

// AdjustBufferSizes 实现AdaptiveBuffer接口
func (b *defaultAdaptiveBuffer) AdjustBufferSizes(stats BufferStats) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// 更新统计信息
	b.stats = stats

	// 检测内存压力
	b.pressureLevel = b.detectMemoryPressure()

	// 如果内存压力高，主动释放一些资源
	if b.pressureLevel >= 2 {
		runtime.GC()
	}
}

// Reset 实现AdaptiveBuffer接口
func (b *defaultAdaptiveBuffer) Reset() {
	atomic.StoreInt64(&b.allocatedMem, 0)

	// 注意：无法重置sync.Pool中的对象
	// 这里仅重置统计信息
	b.mu.Lock()
	b.stats = BufferStats{
		ReadHitRate:  1.0,
		WriteHitRate: 1.0,
		AvgReadSize:  SmallBufferSize,
		AvgWriteSize: SmallBufferSize,
	}
	b.mu.Unlock()

	// 提示垃圾收集器回收未使用的缓冲区
	runtime.GC()
}

// GetStats 获取当前缓冲区统计
func (b *defaultAdaptiveBuffer) GetStats() BufferStats {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// 获取当前内存压力
	b.stats.MemoryPressure = b.detectMemoryPressure()

	return b.stats
}

// WrapReader 创建IO读缓冲区
func (b *defaultAdaptiveBuffer) WrapReader(r io.Reader, size int) *bufio.Reader {
	bufferSize := b.calculateBufferSize(size, true)
	return bufio.NewReaderSize(r, bufferSize)
}

// WrapWriter 创建IO写缓冲区
func (b *defaultAdaptiveBuffer) WrapWriter(w io.Writer, size int) *bufio.Writer {
	bufferSize := b.calculateBufferSize(size, false)
	return bufio.NewWriterSize(w, bufferSize)
}
