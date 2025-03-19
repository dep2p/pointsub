package pointsub

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// MemoryManagerConfig 内存管理器配置
type MemoryManagerConfig struct {
	// 是否启用内存压力监控
	EnableMemoryPressureMonitoring bool

	// 低内存压力阈值（百分比）
	LowMemoryPressureThreshold float64

	// 中等内存压力阈值（百分比）
	MediumMemoryPressureThreshold float64

	// 高内存压力阈值（百分比）
	HighMemoryPressureThreshold float64

	// 内存压力检查间隔
	MemoryPressureCheckInterval time.Duration

	// 垃圾收集触发阈值（百分比）
	GCTriggerThreshold float64

	// 内存使用限制（字节）
	MemoryUsageLimit int64

	// 是否启用内存预分配
	EnablePreallocation bool

	// 预分配内存大小（字节）
	PreallocationSize int64

	// 缓冲区池配置
	BufferPoolConfig map[string]interface{}

	// 缓冲区管理器
	BufferManager *BufferManager
}

// MemoryStats 内存统计信息
type MemoryStats struct {
	// 当前系统内存使用情况
	SystemMemStats runtime.MemStats

	// 已分配的内存总量（字节）
	AllocatedMemory int64

	// 峰值内存使用量（字节）
	PeakMemoryUsage int64

	// 缓冲区分配计数
	BufferAllocationsCount int64

	// 缓冲区释放计数
	BufferReleasesCount int64

	// 当前内存压力级别（0-低，1-中，2-高）
	MemoryPressureLevel int

	// 已运行的垃圾收集次数
	GCCount uint32

	// 上次垃圾收集时间
	LastGCTime time.Time

	// 缓冲区命中率
	BufferHitRate float64

	// 空闲内存百分比
	FreeMemoryPercent float64

	// 监控开始时间
	MonitoringStartTime time.Time

	// 上次报告时间
	LastReportTime time.Time
}

// MemoryPressureListener 内存压力监听器接口
type MemoryPressureListener interface {
	// 处理内存压力变化
	OnMemoryPressureChanged(oldLevel, newLevel int)
}

// MemoryManager 内存管理器
type MemoryManager struct {
	// 配置信息
	config MemoryManagerConfig

	// 统计信息
	stats MemoryStats

	// 内存压力监听器列表
	listeners []MemoryPressureListener

	// 预分配的内存块（用于紧急情况）
	emergencyMemory []byte

	// 缓冲区管理器
	bufferManager *BufferManager

	// 同步保护
	mu sync.RWMutex

	// 是否正在运行
	running int32

	// 停止信号通道
	stopCh chan struct{}
}

// NewDefaultMemoryManagerConfig 创建默认内存管理器配置
func NewDefaultMemoryManagerConfig() MemoryManagerConfig {
	return MemoryManagerConfig{
		EnableMemoryPressureMonitoring: true,
		LowMemoryPressureThreshold:     LowMemoryPressure,
		MediumMemoryPressureThreshold:  MediumMemoryPressure,
		HighMemoryPressureThreshold:    HighMemoryPressure,
		MemoryPressureCheckInterval:    10 * time.Second,
		GCTriggerThreshold:             85.0, // 系统内存使用率超过85%触发GC
		MemoryUsageLimit:               0,    // 0表示不限制
		EnablePreallocation:            true,
		PreallocationSize:              10 * 1024 * 1024, // 10MB预分配内存
		BufferPoolConfig: map[string]interface{}{
			"enablePooling": true,
			"initialSize":   DefaultPoolCapacity,
			"maxSize":       DefaultPoolCapacity * 10,
		},
	}
}

// NewMemoryManager 创建新的内存管理器
func NewMemoryManager(config MemoryManagerConfig) *MemoryManager {
	// 初始化默认配置
	if config.MemoryPressureCheckInterval == 0 {
		config.MemoryPressureCheckInterval = 10 * time.Second
	}

	// 创建内存管理器
	mm := &MemoryManager{
		config: config,
		stats: MemoryStats{
			MonitoringStartTime: time.Now(),
			LastReportTime:      time.Now(),
		},
		listeners: make([]MemoryPressureListener, 0),
		stopCh:    make(chan struct{}),
	}

	// 设置缓冲区管理器
	mm.bufferManager = config.BufferManager

	// 执行预分配（如果启用）
	if config.EnablePreallocation && config.PreallocationSize > 0 {
		mm.emergencyMemory = make([]byte, config.PreallocationSize)
	}

	return mm
}

// Start 启动内存管理器
func (mm *MemoryManager) Start() {
	// 设置运行状态
	if !atomic.CompareAndSwapInt32(&mm.running, 0, 1) {
		// 已经在运行
		return
	}

	// 开始监控内存压力
	if mm.config.EnableMemoryPressureMonitoring {
		go mm.monitorMemoryPressure()
	}
}

// Stop 停止内存管理器
func (mm *MemoryManager) Stop() {
	// 设置停止状态
	if !atomic.CompareAndSwapInt32(&mm.running, 1, 0) {
		// 已经停止
		return
	}

	// 发送停止信号
	close(mm.stopCh)
}

// AddListener 添加内存压力监听器
func (mm *MemoryManager) AddListener(listener MemoryPressureListener) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	mm.listeners = append(mm.listeners, listener)
}

// RemoveListener 移除内存压力监听器
func (mm *MemoryManager) RemoveListener(listener MemoryPressureListener) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	for i, l := range mm.listeners {
		if l == listener {
			// 移除监听器（保持顺序）
			mm.listeners = append(mm.listeners[:i], mm.listeners[i+1:]...)
			return
		}
	}
}

// GetStats 获取内存统计信息
func (mm *MemoryManager) GetStats() MemoryStats {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	// 更新系统内存统计
	mm.updateSystemMemStats()

	// 复制统计信息
	stats := mm.stats

	return stats
}

// GetCurrentMemoryPressureLevel 获取当前内存压力级别
func (mm *MemoryManager) GetCurrentMemoryPressureLevel() int {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	return mm.stats.MemoryPressureLevel
}

// ReleaseEmergencyMemory 释放紧急内存
func (mm *MemoryManager) ReleaseEmergencyMemory() {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	// 释放紧急内存
	mm.emergencyMemory = nil

	// 手动触发垃圾收集
	runtime.GC()
}

// ReallocateEmergencyMemory 重新分配紧急内存
func (mm *MemoryManager) ReallocateEmergencyMemory() {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	// 如果已经存在紧急内存，则不重新分配
	if mm.emergencyMemory != nil {
		return
	}

	// 重新分配紧急内存
	if mm.config.EnablePreallocation && mm.config.PreallocationSize > 0 {
		mm.emergencyMemory = make([]byte, mm.config.PreallocationSize)
	}
}

// GetBufferManager 获取缓冲区管理器
func (mm *MemoryManager) GetBufferManager() *BufferManager {
	return mm.bufferManager
}

// SetBufferManager 设置缓冲区管理器
func (mm *MemoryManager) SetBufferManager(manager *BufferManager) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	mm.bufferManager = manager
}

// RecordAllocation 记录内存分配
func (mm *MemoryManager) RecordAllocation(size int64) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	mm.stats.AllocatedMemory += size
	mm.stats.BufferAllocationsCount++

	// 更新峰值使用量
	if mm.stats.AllocatedMemory > mm.stats.PeakMemoryUsage {
		mm.stats.PeakMemoryUsage = mm.stats.AllocatedMemory
	}

	// 检查是否超过内存使用限制
	if mm.config.MemoryUsageLimit > 0 && mm.stats.AllocatedMemory > mm.config.MemoryUsageLimit {
		// 触发内存紧急措施
		mm.handleMemoryLimit()
	}
}

// RecordRelease 记录内存释放
func (mm *MemoryManager) RecordRelease(size int64) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	mm.stats.AllocatedMemory -= size
	mm.stats.BufferReleasesCount++

	// 确保不会变为负值
	if mm.stats.AllocatedMemory < 0 {
		mm.stats.AllocatedMemory = 0
	}
}

// updateSystemMemStats 更新系统内存统计信息
func (mm *MemoryManager) updateSystemMemStats() {
	// 读取系统内存统计
	runtime.ReadMemStats(&mm.stats.SystemMemStats)

	// 计算空闲内存百分比
	mm.stats.FreeMemoryPercent = 100.0 - (float64(mm.stats.SystemMemStats.Alloc) / float64(mm.stats.SystemMemStats.Sys) * 100.0)

	// 更新GC统计
	mm.stats.GCCount = mm.stats.SystemMemStats.NumGC

	// 检查是否需要触发GC
	usedPercent := 100.0 - mm.stats.FreeMemoryPercent
	if usedPercent >= mm.config.GCTriggerThreshold {
		runtime.GC()
	}
}

// monitorMemoryPressure 监控内存压力
func (mm *MemoryManager) monitorMemoryPressure() {
	ticker := time.NewTicker(mm.config.MemoryPressureCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			mm.checkMemoryPressure()
		case <-mm.stopCh:
			return
		}
	}
}

// checkMemoryPressure 检查内存压力
func (mm *MemoryManager) checkMemoryPressure() {
	// 获取当前内存压力级别
	oldLevel := mm.GetCurrentMemoryPressureLevel()

	// 检测新的内存压力级别
	newLevel := mm.detectMemoryPressure()

	// 如果内存压力级别发生变化，通知监听器
	if oldLevel != newLevel {
		mm.mu.Lock()
		mm.stats.MemoryPressureLevel = newLevel
		listeners := make([]MemoryPressureListener, len(mm.listeners))
		copy(listeners, mm.listeners)
		mm.mu.Unlock()

		// 通知所有监听器
		for _, listener := range listeners {
			go listener.OnMemoryPressureChanged(oldLevel, newLevel)
		}

		// 处理内存压力变化
		mm.handleMemoryPressure(oldLevel, newLevel)
	}
}

// detectMemoryPressure 检测当前内存压力级别
func (mm *MemoryManager) detectMemoryPressure() int {
	// 更新系统内存统计
	mm.updateSystemMemStats()

	// 计算内存使用率
	usedPercent := 100.0 - mm.stats.FreeMemoryPercent

	// 根据使用率判断压力级别
	switch {
	case usedPercent >= mm.config.HighMemoryPressureThreshold:
		return 2 // 高压力
	case usedPercent >= mm.config.MediumMemoryPressureThreshold:
		return 1 // 中等压力
	default:
		return 0 // 低压力
	}
}

// handleMemoryPressure 处理内存压力变化
func (mm *MemoryManager) handleMemoryPressure(oldLevel, newLevel int) {
	// 内存压力增加
	if newLevel > oldLevel {
		switch newLevel {
		case 1: // 中等压力
			// 触发部分清理
			mm.performPartialCleanup()
		case 2: // 高压力
			// 释放紧急内存并触发全面清理
			mm.ReleaseEmergencyMemory()
			mm.performFullCleanup()
		}
	} else if newLevel < oldLevel {
		// 内存压力减少
		if newLevel == 0 && mm.emergencyMemory == nil {
			// 重新分配紧急内存
			mm.ReallocateEmergencyMemory()
		}
	}
}

// handleMemoryLimit 处理内存超出限制
func (mm *MemoryManager) handleMemoryLimit() {
	// 释放紧急内存
	mm.ReleaseEmergencyMemory()

	// 强制垃圾收集
	runtime.GC()

	// 执行全面清理
	mm.performFullCleanup()
}

// performPartialCleanup 执行部分清理
func (mm *MemoryManager) performPartialCleanup() {
	// 提示垃圾收集器回收未使用的内存
	runtime.GC()

	// 如果有缓冲区管理器，清理空闲缓冲区
	if mm.bufferManager != nil {
		// 获取自适应缓冲区
		buffer := mm.bufferManager.GetBuffer(AdaptiveBufferType)
		// 调整缓冲区大小
		buffer.AdjustBufferSizes(buffer.GetStats())
	}
}

// performFullCleanup 执行全面清理
func (mm *MemoryManager) performFullCleanup() {
	// 强制垃圾收集
	runtime.GC()

	// 如果有缓冲区管理器，执行完全重置
	if mm.bufferManager != nil {
		// 获取所有类型的缓冲区并重置
		for _, bufferType := range []BufferType{BasicBuffer, AdaptiveBufferType, AdvancedBufferType} {
			buffer := mm.bufferManager.GetBuffer(bufferType)
			buffer.Reset()
		}
	}
}
