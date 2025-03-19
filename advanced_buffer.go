package pointsub

import (
	"bufio"
	"io"
	"runtime"
	"sync"
	"time"
)

// BufferMonitor 缓冲区监控接口
type BufferMonitor interface {
	// 记录缓冲区分配
	RecordAllocation(size int)

	// 记录缓冲区释放
	RecordRelease(size int)

	// 记录命中缓存
	RecordHit(isRead bool)

	// 记录缓存未命中
	RecordMiss(isRead bool)

	// 获取监控统计
	GetStats() BufferMonitorStats
}

// BufferMonitorStats 监控统计信息
type BufferMonitorStats struct {
	// 当前分配的总内存
	AllocatedMemory int64

	// 峰值内存使用
	PeakMemoryUsage int64

	// 读缓冲区命中率
	ReadHitRate float64

	// 写缓冲区命中率
	WriteHitRate float64

	// 平均读缓冲区大小
	AvgReadBufferSize int

	// 平均写缓冲区大小
	AvgWriteBufferSize int

	// 缓冲区分配计数
	AllocationsCount int64

	// 缓冲区释放计数
	ReleasesCount int64

	// 系统内存压力级别
	SystemMemoryPressure int

	// 自上次报告以来的时间
	TimeSinceLastReport time.Duration
}

// AdvancedBufferOption 高级缓冲区选项
type AdvancedBufferOption func(*advancedBuffer)

// advancedBuffer 实现了高级缓冲区管理
type advancedBuffer struct {
	AdaptiveBuffer

	// 分层缓冲池
	tinyBuffers   sync.Pool
	smallBuffers  sync.Pool
	mediumBuffers sync.Pool
	largeBuffers  sync.Pool
	hugeBuffers   sync.Pool

	// 读写器缓存
	readerCache sync.Map
	writerCache sync.Map

	// 缓冲区大小配置
	tinySize   int
	smallSize  int
	mediumSize int
	largeSize  int
	hugeSize   int

	// 监控和统计
	monitor          BufferMonitor
	lastReportTime   time.Time
	reportInterval   time.Duration
	enableAutoReport bool

	// 预分配配置
	enablePreallocation    bool
	preallocatedBuffers    [][]byte
	preallocatedBufferSize int
	preallocatedCount      int

	// 清理和调整配置
	cleanupInterval       time.Duration
	lastCleanupTime       time.Time
	enablePeriodicCleanup bool
	maxIdleTime           time.Duration

	// 内存压力检测
	memoryPressureCheckInterval time.Duration
	lastMemoryPressureCheck     time.Time
	currentMemoryPressure       int

	// 高级优化选项
	useDirectIO             bool
	enableBufferCompression bool
	adaptiveGrowth          bool
	adaptiveShrink          bool
	minBufferSize           int
	maxBufferSize           int

	// 同步保护
	mu sync.RWMutex
}

// 缓冲区元数据
type bufferMetadata struct {
	lastUsed        time.Time
	accessCount     int64
	creationTime    time.Time
	originalSize    int
	actualSize      int
	compressionRate float64
}

// DefaultAdvancedBufferMonitor 默认的缓冲区监控器
type DefaultAdvancedBufferMonitor struct {
	allocatedMemory  int64
	peakMemoryUsage  int64
	readHits         int64
	readMisses       int64
	writeHits        int64
	writeMisses      int64
	totalReadSize    int64
	totalWriteSize   int64
	readBufferCount  int64
	writeBufferCount int64
	allocationsCount int64
	releasesCount    int64
	lastReportTime   time.Time
	mu               sync.RWMutex
}

// WithTinyBufferSize 设置微小缓冲区大小
func WithTinyBufferSize(size int) AdvancedBufferOption {
	return func(b *advancedBuffer) {
		if size > 0 {
			b.tinySize = size
		}
	}
}

// WithSmallBufferSize 设置小缓冲区大小
func WithSmallBufferSize(size int) AdvancedBufferOption {
	return func(b *advancedBuffer) {
		if size > 0 {
			b.smallSize = size
		}
	}
}

// WithMediumBufferSize 设置中等缓冲区大小
func WithMediumBufferSize(size int) AdvancedBufferOption {
	return func(b *advancedBuffer) {
		if size > 0 {
			b.mediumSize = size
		}
	}
}

// WithLargeBufferSize 设置大缓冲区大小
func WithLargeBufferSize(size int) AdvancedBufferOption {
	return func(b *advancedBuffer) {
		if size > 0 {
			b.largeSize = size
		}
	}
}

// WithHugeBufferSize 设置超大缓冲区大小
func WithHugeBufferSize(size int) AdvancedBufferOption {
	return func(b *advancedBuffer) {
		if size > 0 {
			b.hugeSize = size
		}
	}
}

// WithBufferMonitor 设置缓冲区监控器
func WithBufferMonitor(monitor BufferMonitor) AdvancedBufferOption {
	return func(b *advancedBuffer) {
		if monitor != nil {
			b.monitor = monitor
		}
	}
}

// WithReportInterval 设置报告间隔
func WithReportInterval(interval time.Duration) AdvancedBufferOption {
	return func(b *advancedBuffer) {
		if interval > 0 {
			b.reportInterval = interval
			b.enableAutoReport = true
		}
	}
}

// WithPreallocation 设置预分配配置
func WithPreallocation(bufferSize, count int) AdvancedBufferOption {
	return func(b *advancedBuffer) {
		if bufferSize > 0 && count > 0 {
			b.enablePreallocation = true
			b.preallocatedBufferSize = bufferSize
			b.preallocatedCount = count
		}
	}
}

// WithCleanupInterval 设置清理间隔
func WithCleanupInterval(interval time.Duration) AdvancedBufferOption {
	return func(b *advancedBuffer) {
		if interval > 0 {
			b.cleanupInterval = interval
			b.enablePeriodicCleanup = true
		}
	}
}

// WithMaxIdleTime 设置最大空闲时间
func WithMaxIdleTime(maxIdle time.Duration) AdvancedBufferOption {
	return func(b *advancedBuffer) {
		if maxIdle > 0 {
			b.maxIdleTime = maxIdle
		}
	}
}

// WithMemoryPressureCheckInterval 设置内存压力检查间隔
func WithMemoryPressureCheckInterval(interval time.Duration) AdvancedBufferOption {
	return func(b *advancedBuffer) {
		if interval > 0 {
			b.memoryPressureCheckInterval = interval
		}
	}
}

// WithDirectIO 设置是否使用直接IO
func WithDirectIO(enable bool) AdvancedBufferOption {
	return func(b *advancedBuffer) {
		b.useDirectIO = enable
	}
}

// WithBufferCompression 设置是否启用缓冲区压缩
func WithBufferCompression(enable bool) AdvancedBufferOption {
	return func(b *advancedBuffer) {
		b.enableBufferCompression = enable
	}
}

// WithAdaptiveBufferSize 设置自适应缓冲区大小范围
func WithAdaptiveBufferSize(minSize, maxSize int) AdvancedBufferOption {
	return func(b *advancedBuffer) {
		if minSize > 0 && maxSize >= minSize {
			b.adaptiveGrowth = true
			b.adaptiveShrink = true
			b.minBufferSize = minSize
			b.maxBufferSize = maxSize
		}
	}
}

// NewAdvancedBufferMonitor 创建新的缓冲区监控器
func NewAdvancedBufferMonitor() BufferMonitor {
	return &DefaultAdvancedBufferMonitor{
		lastReportTime: time.Now(),
	}
}

// RecordAllocation 实现BufferMonitor接口
func (m *DefaultAdvancedBufferMonitor) RecordAllocation(size int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.allocationsCount++
	m.allocatedMemory += int64(size)

	if m.allocatedMemory > m.peakMemoryUsage {
		m.peakMemoryUsage = m.allocatedMemory
	}
}

// RecordRelease 实现BufferMonitor接口
func (m *DefaultAdvancedBufferMonitor) RecordRelease(size int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.releasesCount++
	m.allocatedMemory -= int64(size)
	if m.allocatedMemory < 0 {
		m.allocatedMemory = 0
	}
}

// RecordHit 实现BufferMonitor接口
func (m *DefaultAdvancedBufferMonitor) RecordHit(isRead bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if isRead {
		m.readHits++
	} else {
		m.writeHits++
	}
}

// RecordMiss 实现BufferMonitor接口
func (m *DefaultAdvancedBufferMonitor) RecordMiss(isRead bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if isRead {
		m.readMisses++
	} else {
		m.writeMisses++
	}
}

// GetStats 实现BufferMonitor接口
func (m *DefaultAdvancedBufferMonitor) GetStats() BufferMonitorStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now()
	timeSinceLastReport := now.Sub(m.lastReportTime)

	// 计算读命中率
	readTotal := m.readHits + m.readMisses
	readHitRate := 0.0
	if readTotal > 0 {
		readHitRate = float64(m.readHits) / float64(readTotal)
	}

	// 计算写命中率
	writeTotal := m.writeHits + m.writeMisses
	writeHitRate := 0.0
	if writeTotal > 0 {
		writeHitRate = float64(m.writeHits) / float64(writeTotal)
	}

	// 计算平均缓冲区大小
	avgReadSize := 0
	if m.readBufferCount > 0 {
		avgReadSize = int(m.totalReadSize / m.readBufferCount)
	}

	avgWriteSize := 0
	if m.writeBufferCount > 0 {
		avgWriteSize = int(m.totalWriteSize / m.writeBufferCount)
	}

	// 复制状态以重置一些计数器
	stats := BufferMonitorStats{
		AllocatedMemory:     m.allocatedMemory,
		PeakMemoryUsage:     m.peakMemoryUsage,
		ReadHitRate:         readHitRate,
		WriteHitRate:        writeHitRate,
		AvgReadBufferSize:   avgReadSize,
		AvgWriteBufferSize:  avgWriteSize,
		AllocationsCount:    m.allocationsCount,
		ReleasesCount:       m.releasesCount,
		TimeSinceLastReport: timeSinceLastReport,
	}

	// 检测系统内存压力
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// 计算系统内存使用率（百分比）
	usedPercent := float64(memStats.Alloc) / float64(memStats.Sys) * 100

	// 判断内存压力级别
	switch {
	case usedPercent >= HighMemoryPressure:
		stats.SystemMemoryPressure = 2 // 高压力
	case usedPercent >= MediumMemoryPressure:
		stats.SystemMemoryPressure = 1 // 中等压力
	default:
		stats.SystemMemoryPressure = 0 // 低压力
	}

	// 更新最后报告时间
	m.lastReportTime = now

	return stats
}

// NewAdvancedBuffer 创建新的高级缓冲区管理器
func NewAdvancedBuffer(options ...AdvancedBufferOption) AdaptiveBuffer {
	// 创建基本缓冲区实现
	baseBuffer := NewDefaultAdaptiveBuffer()

	// 创建高级缓冲区
	ab := &advancedBuffer{
		AdaptiveBuffer: baseBuffer,
		tinySize:       512,
		smallSize:      4 * 1024,
		mediumSize:     32 * 1024,
		largeSize:      256 * 1024,
		hugeSize:       1024 * 1024,

		reportInterval:              5 * time.Minute,
		enableAutoReport:            false,
		enablePreallocation:         false,
		cleanupInterval:             10 * time.Minute,
		enablePeriodicCleanup:       true,
		maxIdleTime:                 30 * time.Minute,
		memoryPressureCheckInterval: time.Minute,
		useDirectIO:                 false,
		enableBufferCompression:     false,
		adaptiveGrowth:              true,
		adaptiveShrink:              true,
		minBufferSize:               1024,
		maxBufferSize:               MaxBufferLimit,

		lastReportTime:          time.Now(),
		lastCleanupTime:         time.Now(),
		lastMemoryPressureCheck: time.Now(),
		currentMemoryPressure:   0,
	}

	// 创建默认监控器
	ab.monitor = NewAdvancedBufferMonitor()

	// 初始化缓冲池
	ab.tinyBuffers = sync.Pool{
		New: func() interface{} {
			buf := make([]byte, ab.tinySize)
			return buf
		},
	}

	ab.smallBuffers = sync.Pool{
		New: func() interface{} {
			buf := make([]byte, ab.smallSize)
			return buf
		},
	}

	ab.mediumBuffers = sync.Pool{
		New: func() interface{} {
			buf := make([]byte, ab.mediumSize)
			return buf
		},
	}

	ab.largeBuffers = sync.Pool{
		New: func() interface{} {
			buf := make([]byte, ab.largeSize)
			return buf
		},
	}

	ab.hugeBuffers = sync.Pool{
		New: func() interface{} {
			buf := make([]byte, ab.hugeSize)
			return buf
		},
	}

	// 应用选项
	for _, opt := range options {
		opt(ab)
	}

	// 执行预分配（如果启用）
	if ab.enablePreallocation {
		ab.preallocateBuffers()
	}

	// 启动后台任务
	if ab.enablePeriodicCleanup || ab.enableAutoReport {
		go ab.backgroundTasks()
	}

	return ab
}

// GetReadBuffer 获取读缓冲区
func (b *advancedBuffer) GetReadBuffer(size int) *bufio.Reader {
	// 检查缓存
	cacheKey := size
	if cached, ok := b.readerCache.Load(cacheKey); ok {
		reader := cached.(*bufio.Reader)
		b.monitor.RecordHit(true)
		return reader
	}

	// 缓存未命中
	b.monitor.RecordMiss(true)

	// 检查内存压力
	b.checkMemoryPressure()

	// 创建新的读缓冲区
	bufferSize := b.calculateOptimalBufferSize(size, true)
	reader := bufio.NewReaderSize(nil, bufferSize)

	// 记录分配
	b.monitor.RecordAllocation(bufferSize)

	// 缓存读缓冲区
	b.readerCache.Store(cacheKey, reader)

	return reader
}

// GetWriteBuffer 获取写缓冲区
func (b *advancedBuffer) GetWriteBuffer(size int) *bufio.Writer {
	// 检查缓存
	cacheKey := size
	if cached, ok := b.writerCache.Load(cacheKey); ok {
		writer := cached.(*bufio.Writer)
		b.monitor.RecordHit(false)
		return writer
	}

	// 缓存未命中
	b.monitor.RecordMiss(false)

	// 检查内存压力
	b.checkMemoryPressure()

	// 创建新的写缓冲区
	bufferSize := b.calculateOptimalBufferSize(size, false)
	writer := bufio.NewWriterSize(nil, bufferSize)

	// 记录分配
	b.monitor.RecordAllocation(bufferSize)

	// 缓存写缓冲区
	b.writerCache.Store(cacheKey, writer)

	return writer
}

// WrapReader 创建IO读缓冲区
func (b *advancedBuffer) WrapReader(r io.Reader, size int) *bufio.Reader {
	bufferSize := b.calculateOptimalBufferSize(size, true)
	reader := bufio.NewReaderSize(r, bufferSize)

	// 记录分配
	b.monitor.RecordAllocation(bufferSize)

	return reader
}

// WrapWriter 创建IO写缓冲区
func (b *advancedBuffer) WrapWriter(w io.Writer, size int) *bufio.Writer {
	bufferSize := b.calculateOptimalBufferSize(size, false)
	writer := bufio.NewWriterSize(w, bufferSize)

	// 记录分配
	b.monitor.RecordAllocation(bufferSize)

	return writer
}

// Reset 重置缓冲区
func (b *advancedBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	// 清除缓存
	b.readerCache = sync.Map{}
	b.writerCache = sync.Map{}

	// 调用基础缓冲区重置
	b.AdaptiveBuffer.Reset()

	// 提示垃圾收集
	runtime.GC()
}

// AdjustBufferSizes 根据统计信息调整缓冲区大小
func (b *advancedBuffer) AdjustBufferSizes(stats BufferStats) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// 调用基础缓冲区调整
	b.AdaptiveBuffer.AdjustBufferSizes(stats)

	// 自动调整缓冲区大小
	if b.adaptiveGrowth || b.adaptiveShrink {
		// 根据命中率和内存压力自动调整缓冲池大小
		switch {
		case stats.MemoryPressure >= 2:
			// 高内存压力：缩小缓冲区大小
			if b.adaptiveShrink {
				b.shrinkBufferSizes(0.8) // 缩小至80%
			}
		case stats.MemoryPressure == 1:
			// 中等内存压力：保持不变
		case stats.ReadHitRate < 0.5 || stats.WriteHitRate < 0.5:
			// 低命中率：调整缓冲区大小
			if b.adaptiveGrowth && stats.MemoryPressure == 0 {
				b.growBufferSizes(1.2) // 增大至120%
			}
		}
	}
}

// GetStats 获取统计信息
func (b *advancedBuffer) GetStats() BufferStats {
	// 获取基础缓冲区统计
	baseStats := b.AdaptiveBuffer.GetStats()

	// 获取监控器统计
	monitorStats := b.monitor.GetStats()

	// 合并统计信息
	baseStats.MemoryPressure = monitorStats.SystemMemoryPressure
	baseStats.ReadHitRate = monitorStats.ReadHitRate
	baseStats.WriteHitRate = monitorStats.WriteHitRate
	baseStats.AvgReadSize = monitorStats.AvgReadBufferSize
	baseStats.AvgWriteSize = monitorStats.AvgWriteBufferSize

	return baseStats
}

// 计算最佳缓冲区大小
func (b *advancedBuffer) calculateOptimalBufferSize(dataSize int, isRead bool) int {
	// 检查当前内存压力级别
	pressureLevel := b.currentMemoryPressure

	// 根据数据大小和内存压力选择合适的缓冲区大小
	var multiplier float64
	switch pressureLevel {
	case 0: // 低压力
		multiplier = 1.0
	case 1: // 中压力
		multiplier = 0.75
	case 2: // 高压力
		multiplier = 0.5
	}

	// 读缓冲区稍大以提高性能
	if isRead {
		multiplier *= 1.2
	}

	// 计算建议大小
	suggestedSize := int(float64(dataSize) * multiplier)

	// 确保不小于最小大小且不大于最大大小
	if suggestedSize < b.minBufferSize {
		suggestedSize = b.minBufferSize
	} else if suggestedSize > b.maxBufferSize {
		suggestedSize = b.maxBufferSize
	}

	// 根据大小范围选择最接近的标准缓冲区大小
	if suggestedSize <= b.tinySize {
		return b.tinySize
	} else if suggestedSize <= b.smallSize {
		return b.smallSize
	} else if suggestedSize <= b.mediumSize {
		return b.mediumSize
	} else if suggestedSize <= b.largeSize {
		return b.largeSize
	} else {
		return b.hugeSize
	}
}

// 预分配缓冲区
func (b *advancedBuffer) preallocateBuffers() {
	b.preallocatedBuffers = make([][]byte, b.preallocatedCount)

	for i := 0; i < b.preallocatedCount; i++ {
		b.preallocatedBuffers[i] = make([]byte, b.preallocatedBufferSize)
		b.monitor.RecordAllocation(b.preallocatedBufferSize)
	}
}

// 后台任务
func (b *advancedBuffer) backgroundTasks() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()

		// 定期清理
		if b.enablePeriodicCleanup && now.Sub(b.lastCleanupTime) >= b.cleanupInterval {
			b.cleanup()
			b.lastCleanupTime = now
		}

		// 自动报告
		if b.enableAutoReport && now.Sub(b.lastReportTime) >= b.reportInterval {
			// 获取最新统计信息
			_ = b.monitor.GetStats()
			b.lastReportTime = now
		}

		// 内存压力检测
		if now.Sub(b.lastMemoryPressureCheck) >= b.memoryPressureCheckInterval {
			b.checkMemoryPressure()
			b.lastMemoryPressureCheck = now
		}
	}
}

// 检查内存压力
func (b *advancedBuffer) checkMemoryPressure() {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// 计算系统内存使用率（百分比）
	usedPercent := float64(memStats.Alloc) / float64(memStats.Sys) * 100

	// 判断内存压力级别
	var newPressureLevel int
	switch {
	case usedPercent >= HighMemoryPressure:
		newPressureLevel = 2 // 高压力
	case usedPercent >= MediumMemoryPressure:
		newPressureLevel = 1 // 中等压力
	default:
		newPressureLevel = 0 // 低压力
	}

	// 更新内存压力级别
	if newPressureLevel != b.currentMemoryPressure {
		b.mu.Lock()
		b.currentMemoryPressure = newPressureLevel
		b.mu.Unlock()

		// 如果内存压力升高，主动清理
		if newPressureLevel > 0 {
			b.cleanup()
		}
	}
}

// 清理过期缓存
func (b *advancedBuffer) cleanup() {
	// 提示垃圾收集器回收未使用的缓冲区
	runtime.GC()

	// 扫描并清理读缓冲区缓存
	b.readerCache.Range(func(key, value interface{}) bool {
		// 清理逻辑
		return true
	})

	// 扫描并清理写缓冲区缓存
	b.writerCache.Range(func(key, value interface{}) bool {
		// 清理逻辑
		return true
	})
}

// 增大缓冲区大小
func (b *advancedBuffer) growBufferSizes(factor float64) {
	// 只在增长因子大于1时调整
	if factor <= 1.0 {
		return
	}

	// 确保不超过最大限制
	b.smallSize = min(b.maxBufferSize, int(float64(b.smallSize)*factor))
	b.mediumSize = min(b.maxBufferSize, int(float64(b.mediumSize)*factor))
	b.largeSize = min(b.maxBufferSize, int(float64(b.largeSize)*factor))
	b.hugeSize = min(b.maxBufferSize, int(float64(b.hugeSize)*factor))
}

// 缩小缓冲区大小
func (b *advancedBuffer) shrinkBufferSizes(factor float64) {
	// 只在缩小因子小于1时调整
	if factor >= 1.0 {
		return
	}

	// 确保不小于最小限制
	b.smallSize = max(b.minBufferSize, int(float64(b.smallSize)*factor))
	b.mediumSize = max(b.smallSize*2, int(float64(b.mediumSize)*factor))
	b.largeSize = max(b.mediumSize*2, int(float64(b.largeSize)*factor))
	b.hugeSize = max(b.largeSize*2, int(float64(b.hugeSize)*factor))
}

// 工具函数
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
