package pointsub

import (
	"sync"
	"time"
)

// BufferType 缓冲区类型枚举
type BufferType int

const (
	// BasicBuffer 基本缓冲区
	BasicBuffer BufferType = iota

	// AdaptiveBufferType 自适应缓冲区类型
	AdaptiveBufferType

	// AdvancedBufferType 高级缓冲区类型
	AdvancedBufferType
)

// BufferFactory 缓冲区工厂接口
type BufferFactory interface {
	// 创建缓冲区管理器
	CreateBuffer(bufferType BufferType, options map[string]interface{}) AdaptiveBuffer

	// 获取默认缓冲区管理器
	GetDefaultBuffer() AdaptiveBuffer

	// 获取自定义缓冲区管理器
	GetCustomBuffer(name string) (AdaptiveBuffer, bool)

	// 注册自定义缓冲区管理器
	RegisterBuffer(name string, buffer AdaptiveBuffer)
}

// defaultBufferFactory 默认的缓冲区工厂实现
type defaultBufferFactory struct {
	sync.RWMutex

	// 缓存的缓冲区管理器
	bufferCache map[string]AdaptiveBuffer

	// 默认的缓冲区管理器
	defaultBuffer AdaptiveBuffer
}

// NewBufferFactory 创建新的缓冲区工厂
func NewBufferFactory() BufferFactory {
	// 创建默认的自适应缓冲区管理器
	defaultBuffer := NewDefaultAdaptiveBuffer()

	return &defaultBufferFactory{
		bufferCache:   make(map[string]AdaptiveBuffer),
		defaultBuffer: defaultBuffer,
	}
}

// CreateBuffer 实现BufferFactory接口
func (f *defaultBufferFactory) CreateBuffer(bufferType BufferType, options map[string]interface{}) AdaptiveBuffer {
	switch bufferType {
	case BasicBuffer:
		return f.createBasicBuffer(options)
	case AdaptiveBufferType:
		return f.createAdaptiveBuffer(options)
	case AdvancedBufferType:
		return f.createAdvancedBuffer(options)
	default:
		return f.defaultBuffer
	}
}

// GetDefaultBuffer 实现BufferFactory接口
func (f *defaultBufferFactory) GetDefaultBuffer() AdaptiveBuffer {
	return f.defaultBuffer
}

// GetCustomBuffer 实现BufferFactory接口
func (f *defaultBufferFactory) GetCustomBuffer(name string) (AdaptiveBuffer, bool) {
	f.RLock()
	defer f.RUnlock()

	buffer, ok := f.bufferCache[name]
	return buffer, ok
}

// RegisterBuffer 实现BufferFactory接口
func (f *defaultBufferFactory) RegisterBuffer(name string, buffer AdaptiveBuffer) {
	f.Lock()
	defer f.Unlock()

	f.bufferCache[name] = buffer
}

// createBasicBuffer 创建基本缓冲区
func (f *defaultBufferFactory) createBasicBuffer(options map[string]interface{}) AdaptiveBuffer {
	// 此处简单返回默认实现
	return NewDefaultAdaptiveBuffer()
}

// createAdaptiveBuffer 创建自适应缓冲区
func (f *defaultBufferFactory) createAdaptiveBuffer(options map[string]interface{}) AdaptiveBuffer {
	buffer := NewDefaultAdaptiveBuffer()

	// 配置自适应缓冲区选项
	if readSize, ok := options["readSize"].(int); ok && readSize > 0 {
		// 这里可以添加配置，但基础类型没有配置方法，所以这是一个示例
	}

	if writeSize, ok := options["writeSize"].(int); ok && writeSize > 0 {
		// 这里可以添加配置，但基础类型没有配置方法，所以这是一个示例
	}

	return buffer
}

// createAdvancedBuffer 创建高级缓冲区
func (f *defaultBufferFactory) createAdvancedBuffer(options map[string]interface{}) AdaptiveBuffer {
	// 准备高级缓冲区选项
	var advancedOptions []AdvancedBufferOption

	// 解析配置选项并创建对应的AdvancedBufferOption
	if tinySize, ok := options["tinySize"].(int); ok && tinySize > 0 {
		advancedOptions = append(advancedOptions, WithTinyBufferSize(tinySize))
	}

	if smallSize, ok := options["smallSize"].(int); ok && smallSize > 0 {
		advancedOptions = append(advancedOptions, WithSmallBufferSize(smallSize))
	}

	if mediumSize, ok := options["mediumSize"].(int); ok && mediumSize > 0 {
		advancedOptions = append(advancedOptions, WithMediumBufferSize(mediumSize))
	}

	if largeSize, ok := options["largeSize"].(int); ok && largeSize > 0 {
		advancedOptions = append(advancedOptions, WithLargeBufferSize(largeSize))
	}

	if hugeSize, ok := options["hugeSize"].(int); ok && hugeSize > 0 {
		advancedOptions = append(advancedOptions, WithHugeBufferSize(hugeSize))
	}

	// 配置报告间隔
	if reportInterval, ok := options["reportInterval"].(time.Duration); ok && reportInterval > 0 {
		advancedOptions = append(advancedOptions, WithReportInterval(reportInterval))
	}

	// 配置预分配
	if preallocate, ok := options["preallocate"].(bool); ok && preallocate {
		size := 4 * 1024 // 默认4KB
		count := 100     // 默认100个缓冲区

		if preallocationSize, ok := options["preallocationSize"].(int); ok && preallocationSize > 0 {
			size = preallocationSize
		}

		if preallocationCount, ok := options["preallocationCount"].(int); ok && preallocationCount > 0 {
			count = preallocationCount
		}

		advancedOptions = append(advancedOptions, WithPreallocation(size, count))
	}

	// 配置清理间隔
	if cleanupInterval, ok := options["cleanupInterval"].(time.Duration); ok && cleanupInterval > 0 {
		advancedOptions = append(advancedOptions, WithCleanupInterval(cleanupInterval))
	}

	// 配置最大空闲时间
	if maxIdleTime, ok := options["maxIdleTime"].(time.Duration); ok && maxIdleTime > 0 {
		advancedOptions = append(advancedOptions, WithMaxIdleTime(maxIdleTime))
	}

	// 配置内存压力检查间隔
	if memoryPressureCheckInterval, ok := options["memoryPressureCheckInterval"].(time.Duration); ok && memoryPressureCheckInterval > 0 {
		advancedOptions = append(advancedOptions, WithMemoryPressureCheckInterval(memoryPressureCheckInterval))
	}

	// 配置直接IO
	if useDirectIO, ok := options["useDirectIO"].(bool); ok {
		advancedOptions = append(advancedOptions, WithDirectIO(useDirectIO))
	}

	// 配置缓冲区压缩
	if enableCompression, ok := options["enableCompression"].(bool); ok {
		advancedOptions = append(advancedOptions, WithBufferCompression(enableCompression))
	}

	// 配置自适应缓冲区大小
	minSize := MinBufferLimit
	maxSize := MaxBufferLimit

	if min, ok := options["minBufferSize"].(int); ok && min > 0 {
		minSize = min
	}

	if max, ok := options["maxBufferSize"].(int); ok && max > minSize {
		maxSize = max
	}

	advancedOptions = append(advancedOptions, WithAdaptiveBufferSize(minSize, maxSize))

	// 创建高级缓冲区
	return NewAdvancedBuffer(advancedOptions...)
}

// BufferManagerConfig 缓冲区管理器配置
type BufferManagerConfig struct {
	// 默认缓冲区类型
	DefaultBufferType BufferType

	// 工作负载类型
	WorkloadType string

	// 应用场景(如网络IO、文件IO、内存处理等)
	ApplicationScenario string

	// 高级选项
	AdvancedOptions map[string]interface{}

	// 预定义的自定义缓冲区
	CustomBuffers map[string]AdaptiveBuffer
}

// BufferManager 全局缓冲区管理器
type BufferManager struct {
	factory    BufferFactory
	config     BufferManagerConfig
	bufferPool sync.Pool
}

// NewBufferManager 创建全局缓冲区管理器
func NewBufferManager(config BufferManagerConfig) *BufferManager {
	factory := NewBufferFactory()

	// 注册自定义缓冲区
	for name, buffer := range config.CustomBuffers {
		factory.RegisterBuffer(name, buffer)
	}

	// 创建缓冲区管理器
	manager := &BufferManager{
		factory: factory,
		config:  config,
		bufferPool: sync.Pool{
			New: func() interface{} {
				return factory.CreateBuffer(config.DefaultBufferType, config.AdvancedOptions)
			},
		},
	}

	return manager
}

// GetBuffer 获取指定类型的缓冲区
func (m *BufferManager) GetBuffer(bufferType BufferType) AdaptiveBuffer {
	return m.factory.CreateBuffer(bufferType, m.config.AdvancedOptions)
}

// GetCustomBuffer 获取指定名称的自定义缓冲区
func (m *BufferManager) GetCustomBuffer(name string) (AdaptiveBuffer, bool) {
	return m.factory.GetCustomBuffer(name)
}

// GetPooledBuffer 从池中获取缓冲区
func (m *BufferManager) GetPooledBuffer() AdaptiveBuffer {
	return m.bufferPool.Get().(AdaptiveBuffer)
}

// ReleaseBuffer 释放缓冲区回池
func (m *BufferManager) ReleaseBuffer(buffer AdaptiveBuffer) {
	// 重置缓冲区
	buffer.Reset()
	// 放回池中
	m.bufferPool.Put(buffer)
}

// RecommendBufferType 根据工作负载类型和应用场景推荐缓冲区类型
func (m *BufferManager) RecommendBufferType() BufferType {
	// 基于工作负载类型和应用场景进行智能推荐
	switch m.config.WorkloadType {
	case "highThroughput":
		return AdvancedBufferType
	case "lowLatency":
		return AdaptiveBufferType
	case "memoryConstrained":
		return BasicBuffer
	default:
		// 默认根据应用场景判断
		switch m.config.ApplicationScenario {
		case "networkIO":
			return AdvancedBufferType
		case "fileIO":
			return AdaptiveBufferType
		default:
			return m.config.DefaultBufferType
		}
	}
}
