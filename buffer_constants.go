package pointsub

// 缓冲区大小限制补充常量
const (
	// MinBufferLimit 最小缓冲区大小限制
	MinBufferLimit = 64

	// DefaultTinyChunkSize 默认微小块大小
	DefaultTinyChunkSize = 512
	// DefaultSmallChunkSize 默认小块大小 - 用于小于16KB的消息
	DefaultSmallChunkSize = 4 * 1024 // 4KB - 用于小于16KB的消息
	// DefaultMediumChunkSize 默认中等块大小 - 用于16KB-128KB的消息
	DefaultMediumChunkSize = 16 * 1024 // 16KB - 用于16KB-128KB的消息
	// DefaultLargeChunkSize 默认大块大小 - 用于大于128KB的消息
	DefaultLargeChunkSize = 64 * 1024 // 64KB - 用于大于128KB的消息
	// DefaultHugeChunkSize 默认超大块大小 - 用于大于1MB的消息
	DefaultHugeChunkSize = 256 * 1024 // 256KB - 用于大于1MB的消息

	// DefaultReadBufferSize 默认读缓冲区大小
	DefaultReadBufferSize = 8 * 1024 // 8KB 默认读缓冲区
	// DefaultWriteBufferSize 默认写缓冲区大小
	DefaultWriteBufferSize = 8 * 1024 // 8KB 默认写缓冲区
	// MaxBufferSize 最大缓冲区大小
	MaxBufferSize = 2 * 1024 * 1024 // 2MB 最大缓冲区大小

	// SmallMessageThreshold 小消息阈值
	SmallMessageThreshold = 16 * 1024 // 16KB
	// MediumMessageThreshold 中等消息阈值
	MediumMessageThreshold = 128 * 1024 // 128KB
	// LargeMessageThreshold 大消息阈值
	LargeMessageThreshold = 1024 * 1024 // 1MB

	// TinyMessageSize 微小消息大小 (<1KB)
	TinyMessageSize = 1 * 1024 // 微小消息 (<1KB)
	// SmallMessageSize 小消息大小 (<16KB)
	SmallMessageSize = 16 * 1024 // 小消息 (<16KB)
	// MediumMessageSize 中等消息大小 (<128KB)
	MediumMessageSize = 128 * 1024 // 中等消息 (<128KB)
	// LargeMessageSize 大消息大小 (<1MB)
	LargeMessageSize = 1 * 1024 * 1024 // 大消息 (<1MB)
	// HugeMessageSize 超大消息大小 (>=1MB)
	HugeMessageSize = 1 * 1024 * 1024 // 超大消息 (>=1MB)
)

// 内存压力级别常量已在adaptive_buffer.go中定义，此处省略
// const (
// 	LowMemoryPressure = 30
// 	MediumMemoryPressure = 60
// 	HighMemoryPressure = 85
// )

// 缓冲区池配置常量
const (
	// DefaultPoolCapacity 默认缓冲区池初始容量
	DefaultPoolCapacity = 100

	// PoolGrowthFactor 缓冲区池增长因子
	PoolGrowthFactor = 1.5

	// PoolShrinkFactor 缓冲区池缩小因子
	PoolShrinkFactor = 0.8

	// PoolHighUtilizationThreshold 缓冲区池高利用率阈值 - 高于此值进行扩容
	PoolHighUtilizationThreshold = 0.8

	// PoolLowUtilizationThreshold 缓冲区池低利用率阈值 - 低于此值进行缩容
	PoolLowUtilizationThreshold = 0.3
)

// 适应性缓冲区统计标志
const (
	// EnableStats 启用统计收集
	EnableStats = 1 << iota

	// EnableAutoAdjust 启用自动调整
	EnableAutoAdjust

	// EnableVerboseLogging 启用详细日志
	EnableVerboseLogging

	// EnablePerfTracking 启用性能追踪
	EnablePerfTracking
)

// 缓冲区特性标志
const (
	// 启用预分配
	FlagPreallocate = 1 << iota

	// 启用压缩
	FlagCompression

	// 启用池化
	FlagPooled

	// 启用直接IO
	FlagDirectIO

	// 启用零拷贝
	FlagZeroCopy
)
