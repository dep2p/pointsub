package pointsub

import (
	"fmt"
	"sync"
	"time"
)

// ProgressDashboard 是一个集中式的进度仪表盘，可以管理和展示多个传输任务的状态
type ProgressDashboard struct {
	// 跟踪器映射表，key为传输ID
	trackers map[string]ProgressTracker

	// 传输组映射表，key为组名
	transferGroups map[string][]string

	// 全局回调列表
	globalCallbacks []ProgressCallback

	// 摘要统计信息
	stats DashboardStats

	// 上次更新时间
	lastUpdateTime time.Time

	// 更新间隔
	updateInterval time.Duration

	// 锁
	mu sync.RWMutex
}

// DashboardStats 保存仪表盘的聚合统计信息
type DashboardStats struct {
	// 总传输数量
	TotalTransfers int

	// 活跃传输数量
	ActiveTransfers int

	// 已完成传输数量
	CompletedTransfers int

	// 失败传输数量
	FailedTransfers int

	// 暂停传输数量
	PausedTransfers int

	// 已取消传输数量
	CancelledTransfers int

	// 总传输字节数
	TotalBytes int64

	// 已传输字节数
	TransferredBytes int64

	// 总体完成百分比
	OverallPercentage float64

	// 平均传输速度（字节/秒）
	AverageSpeed float64

	// 预估剩余时间
	EstimatedTimeLeft time.Duration

	// 开始时间
	StartTime time.Time

	// 最高传输速度
	PeakSpeed float64

	// 当前传输速度
	CurrentSpeed float64
}

// DashboardOption 定义仪表盘选项函数类型
type DashboardOption func(*ProgressDashboard)

// WithDashboardUpdateInterval 设置仪表盘更新间隔
func WithDashboardUpdateInterval(interval time.Duration) DashboardOption {
	return func(d *ProgressDashboard) {
		if interval > 0 {
			d.updateInterval = interval
		}
	}
}

// WithDashboardGlobalCallback 添加全局进度回调
func WithDashboardGlobalCallback(callback ProgressCallback) DashboardOption {
	return func(d *ProgressDashboard) {
		if callback != nil {
			d.globalCallbacks = append(d.globalCallbacks, callback)
		}
	}
}

// NewProgressDashboard 创建新的进度仪表盘
func NewProgressDashboard(options ...DashboardOption) *ProgressDashboard {
	dashboard := &ProgressDashboard{
		trackers:        make(map[string]ProgressTracker),
		transferGroups:  make(map[string][]string),
		globalCallbacks: []ProgressCallback{},
		lastUpdateTime:  time.Now(),
		updateInterval:  1 * time.Second,
		stats: DashboardStats{
			StartTime: time.Now(),
		},
	}

	// 应用选项
	for _, opt := range options {
		opt(dashboard)
	}

	return dashboard
}

// AddTracker 将进度跟踪器添加到仪表盘
func (d *ProgressDashboard) AddTracker(transferID string, tracker ProgressTracker) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.trackers[transferID] = tracker

	// 为每个全局回调注册到跟踪器
	for _, callback := range d.globalCallbacks {
		tracker.AddCallback(callback)
	}

	// 更新统计信息
	d.stats.TotalTransfers++
	d.stats.ActiveTransfers++

	// 触发统计更新
	d.updateStatsLocked()
}

// RemoveTracker 从仪表盘移除进度跟踪器
func (d *ProgressDashboard) RemoveTracker(transferID string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if tracker, exists := d.trackers[transferID]; exists {
		// 从所有组中移除
		for groupName, ids := range d.transferGroups {
			newIDs := make([]string, 0, len(ids))
			for _, id := range ids {
				if id != transferID {
					newIDs = append(newIDs, id)
				}
			}
			d.transferGroups[groupName] = newIDs
		}

		// 获取当前状态以更新统计信息
		status := tracker.GetStatus(transferID)
		switch status {
		case StatusCompleted:
			d.stats.CompletedTransfers--
		case StatusFailed:
			d.stats.FailedTransfers--
		case StatusPaused:
			d.stats.PausedTransfers--
		case StatusCancelled:
			d.stats.CancelledTransfers--
		default:
			d.stats.ActiveTransfers--
		}

		// 移除跟踪器
		delete(d.trackers, transferID)
		d.stats.TotalTransfers--

		// 更新统计信息
		d.updateStatsLocked()
	}
}

// AddToGroup 将传输添加到指定组
func (d *ProgressDashboard) AddToGroup(groupName string, transferID string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// 检查跟踪器是否存在
	if _, exists := d.trackers[transferID]; !exists {
		return
	}

	// 检查组是否存在，不存在则创建
	if _, exists := d.transferGroups[groupName]; !exists {
		d.transferGroups[groupName] = make([]string, 0)
	}

	// 检查传输是否已在组中
	for _, id := range d.transferGroups[groupName] {
		if id == transferID {
			return // 已存在，不需要添加
		}
	}

	// 添加到组
	d.transferGroups[groupName] = append(d.transferGroups[groupName], transferID)
}

// RemoveFromGroup 从指定组中移除传输
func (d *ProgressDashboard) RemoveFromGroup(groupName string, transferID string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if ids, exists := d.transferGroups[groupName]; exists {
		newIDs := make([]string, 0, len(ids))
		for _, id := range ids {
			if id != transferID {
				newIDs = append(newIDs, id)
			}
		}
		d.transferGroups[groupName] = newIDs
	}
}

// GetGroupTransfers 获取组内所有传输ID
func (d *ProgressDashboard) GetGroupTransfers(groupName string) []string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if ids, exists := d.transferGroups[groupName]; exists {
		result := make([]string, len(ids))
		copy(result, ids)
		return result
	}
	return []string{}
}

// PauseGroup 暂停组内所有传输
func (d *ProgressDashboard) PauseGroup(groupName string) {
	d.mu.RLock()
	ids := d.GetGroupTransfers(groupName)
	d.mu.RUnlock()

	for _, id := range ids {
		d.mu.RLock()
		tracker, exists := d.trackers[id]
		d.mu.RUnlock()

		if exists {
			tracker.PauseTracking(id)
		}
	}
}

// ResumeGroup 恢复组内所有传输
func (d *ProgressDashboard) ResumeGroup(groupName string) {
	d.mu.RLock()
	ids := d.GetGroupTransfers(groupName)
	d.mu.RUnlock()

	for _, id := range ids {
		d.mu.RLock()
		tracker, exists := d.trackers[id]
		d.mu.RUnlock()

		if exists {
			tracker.ResumeTracking(id)
		}
	}
}

// CancelGroup 取消组内所有传输
func (d *ProgressDashboard) CancelGroup(groupName string) {
	d.mu.RLock()
	ids := d.GetGroupTransfers(groupName)
	d.mu.RUnlock()

	for _, id := range ids {
		d.mu.RLock()
		tracker, exists := d.trackers[id]
		d.mu.RUnlock()

		if exists {
			tracker.StopTracking(id)
		}
	}
}

// AddGlobalCallback 添加全局进度回调
func (d *ProgressDashboard) AddGlobalCallback(callback ProgressCallback) {
	if callback == nil {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// 添加到全局回调列表
	d.globalCallbacks = append(d.globalCallbacks, callback)

	// 为所有现有跟踪器注册此回调
	for _, tracker := range d.trackers {
		tracker.AddCallback(callback)
	}
}

// RemoveGlobalCallback 移除全局进度回调
func (d *ProgressDashboard) RemoveGlobalCallback(callback ProgressCallback) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// 从全局回调列表中移除
	newCallbacks := make([]ProgressCallback, 0, len(d.globalCallbacks))
	for _, cb := range d.globalCallbacks {
		if cb != callback {
			newCallbacks = append(newCallbacks, cb)
		}
	}
	d.globalCallbacks = newCallbacks

	// 从所有跟踪器中移除此回调
	// 注意：由于ProgressTracker接口没有提供移除回调的方法，这里无法实现
	// 实际应用中应在ProgressTracker接口中添加RemoveCallback方法
}

// GetStats 获取仪表盘统计信息
func (d *ProgressDashboard) GetStats() DashboardStats {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// 如果需要更新统计信息
	if time.Since(d.lastUpdateTime) >= d.updateInterval {
		d.mu.RUnlock()
		d.mu.Lock()
		d.updateStatsLocked()
		d.mu.Unlock()
		d.mu.RLock()
	}

	// 返回统计信息的副本
	stats := d.stats
	return stats
}

// updateStatsLocked 更新仪表盘统计信息（已加锁）
func (d *ProgressDashboard) updateStatsLocked() {
	// 重置统计计数
	activeCount := 0
	completedCount := 0
	failedCount := 0
	pausedCount := 0
	cancelledCount := 0
	totalBytes := int64(0)
	transferredBytes := int64(0)
	totalSpeed := float64(0)
	speedCount := 0

	// 收集所有传输的统计信息
	for transferID, tracker := range d.trackers {
		status := tracker.GetStatus(transferID)

		// 更新状态计数
		switch status {
		case StatusCompleted:
			completedCount++
		case StatusFailed:
			failedCount++
		case StatusPaused:
			pausedCount++
		case StatusCancelled:
			cancelledCount++
		case StatusTransferring, StatusInitializing, StatusResuming:
			activeCount++
		}

		// 更新字节计数
		transferred, total, _ := tracker.GetProgress(transferID)
		totalBytes += total
		transferredBytes += transferred

		// 更新速度
		speed := tracker.GetSpeed(transferID)
		if speed > 0 {
			totalSpeed += speed
			speedCount++

			// 更新峰值速度
			if speed > d.stats.PeakSpeed {
				d.stats.PeakSpeed = speed
			}
		}
	}

	// 更新统计信息
	d.stats.ActiveTransfers = activeCount
	d.stats.CompletedTransfers = completedCount
	d.stats.FailedTransfers = failedCount
	d.stats.PausedTransfers = pausedCount
	d.stats.CancelledTransfers = cancelledCount
	d.stats.TotalBytes = totalBytes
	d.stats.TransferredBytes = transferredBytes

	// 计算总进度百分比
	if totalBytes > 0 {
		d.stats.OverallPercentage = float64(transferredBytes) * 100.0 / float64(totalBytes)
	} else {
		d.stats.OverallPercentage = 0
	}

	// 计算平均速度
	if speedCount > 0 {
		d.stats.AverageSpeed = totalSpeed / float64(speedCount)
		d.stats.CurrentSpeed = totalSpeed
	} else {
		d.stats.AverageSpeed = 0
		d.stats.CurrentSpeed = 0
	}

	// 计算预估剩余时间
	if d.stats.CurrentSpeed > 0 && totalBytes > transferredBytes {
		remainingBytes := totalBytes - transferredBytes
		d.stats.EstimatedTimeLeft = time.Duration(float64(remainingBytes) / d.stats.CurrentSpeed * float64(time.Second))
	} else {
		d.stats.EstimatedTimeLeft = 0
	}

	// 更新最后更新时间
	d.lastUpdateTime = time.Now()
}

// GetActiveTransfers 获取所有活跃传输的ID
func (d *ProgressDashboard) GetActiveTransfers() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	activeTransfers := make([]string, 0)

	for transferID, tracker := range d.trackers {
		status := tracker.GetStatus(transferID)
		if status == StatusTransferring || status == StatusInitializing || status == StatusResuming {
			activeTransfers = append(activeTransfers, transferID)
		}
	}

	return activeTransfers
}

// GetGroupStats 获取组的聚合统计信息
func (d *ProgressDashboard) GetGroupStats(groupName string) DashboardStats {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// 初始化组统计信息
	stats := DashboardStats{
		StartTime: time.Now(),
	}

	// 检查组是否存在
	ids, exists := d.transferGroups[groupName]
	if !exists || len(ids) == 0 {
		return stats
	}

	// 收集组内所有传输的统计信息
	activeCount := 0
	completedCount := 0
	failedCount := 0
	pausedCount := 0
	cancelledCount := 0
	totalBytes := int64(0)
	transferredBytes := int64(0)
	totalSpeed := float64(0)
	speedCount := 0
	earliestStartTime := time.Now()

	for _, transferID := range ids {
		tracker, exists := d.trackers[transferID]
		if !exists {
			continue
		}

		stats.TotalTransfers++

		// 更新状态计数
		status := tracker.GetStatus(transferID)
		switch status {
		case StatusCompleted:
			completedCount++
		case StatusFailed:
			failedCount++
		case StatusPaused:
			pausedCount++
		case StatusCancelled:
			cancelledCount++
		case StatusTransferring, StatusInitializing, StatusResuming:
			activeCount++
		}

		// 更新字节计数
		transferred, total, _ := tracker.GetProgress(transferID)
		totalBytes += total
		transferredBytes += transferred

		// 更新速度
		speed := tracker.GetSpeed(transferID)
		if speed > 0 {
			totalSpeed += speed
			speedCount++

			// 更新峰值速度
			if speed > stats.PeakSpeed {
				stats.PeakSpeed = speed
			}
		}

		// 更新开始时间
		transferStartTime := tracker.GetStartTime(transferID)
		if transferStartTime.Before(earliestStartTime) {
			earliestStartTime = transferStartTime
		}
	}

	// 更新组统计信息
	stats.ActiveTransfers = activeCount
	stats.CompletedTransfers = completedCount
	stats.FailedTransfers = failedCount
	stats.PausedTransfers = pausedCount
	stats.CancelledTransfers = cancelledCount
	stats.TotalBytes = totalBytes
	stats.TransferredBytes = transferredBytes
	stats.StartTime = earliestStartTime

	// 计算总进度百分比
	if totalBytes > 0 {
		stats.OverallPercentage = float64(transferredBytes) * 100.0 / float64(totalBytes)
	}

	// 计算平均速度
	if speedCount > 0 {
		stats.AverageSpeed = totalSpeed / float64(speedCount)
		stats.CurrentSpeed = totalSpeed
	}

	// 计算预估剩余时间
	if stats.CurrentSpeed > 0 && totalBytes > transferredBytes {
		remainingBytes := totalBytes - transferredBytes
		stats.EstimatedTimeLeft = time.Duration(float64(remainingBytes) / stats.CurrentSpeed * float64(time.Second))
	}

	return stats
}

// FormatDashboardStats 将仪表盘统计信息格式化为字符串
func FormatDashboardStats(stats DashboardStats) string {
	// 计算运行时间
	runningTime := time.Since(stats.StartTime)

	// 构建统计信息字符串
	return fmt.Sprintf(
		"总进度: %.1f%% (%s/%s)\n"+
			"传输: %d 总计, %d 活跃, %d 完成, %d 失败, %d 暂停, %d 取消\n"+
			"当前速度: %s/s, 平均速度: %s/s, 峰值速度: %s/s\n"+
			"运行时间: %s, 预估剩余时间: %s",
		stats.OverallPercentage,
		formatSize(stats.TransferredBytes),
		formatSize(stats.TotalBytes),
		stats.TotalTransfers,
		stats.ActiveTransfers,
		stats.CompletedTransfers,
		stats.FailedTransfers,
		stats.PausedTransfers,
		stats.CancelledTransfers,
		formatSize(int64(stats.CurrentSpeed)),
		formatSize(int64(stats.AverageSpeed)),
		formatSize(int64(stats.PeakSpeed)),
		formatDuration(runningTime),
		formatDuration(stats.EstimatedTimeLeft),
	)
}
