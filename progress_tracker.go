package pointsub

import (
	"sync"
	"time"
)

// TransferStatus 表示传输状态
type TransferStatus int

const (
	// StatusInitializing 表示传输正在初始化
	StatusInitializing TransferStatus = iota
	// StatusTransferring 表示正在传输中
	StatusTransferring
	// StatusPaused 表示传输已暂停
	StatusPaused
	// StatusResuming 表示传输正在恢复
	StatusResuming
	// StatusCompleted 表示传输已完成
	StatusCompleted
	// StatusFailed 表示传输失败
	StatusFailed
	// StatusCancelled 表示传输被取消
	StatusCancelled
)

// 进度记录常量
const (
	// 默认速度采样窗口大小
	DefaultSpeedSampleSize = 10

	// 默认速度计算间隔
	DefaultSpeedCalcInterval = 500 * time.Millisecond

	// 默认进度更新阈值（百分比）
	DefaultProgressThreshold = 1.0

	// 历史记录保留时间
	DefaultHistoryRetention = 1 * time.Hour

	// 最大回调执行超时
	MaxCallbackTimeout = 100 * time.Millisecond
)

// SpeedSample 速度样本
type SpeedSample struct {
	// 传输字节数
	Bytes int64

	// 耗时
	Duration time.Duration

	// 时间戳
	Timestamp time.Time
}

// ProgressCallback 是进度更新的回调接口
type ProgressCallback interface {
	// OnProgress 处理进度更新
	OnProgress(transferID string, total int64, transferred int64, percentage float64)
	// OnStatusChange 处理状态变化
	OnStatusChange(transferID string, oldStatus, newStatus TransferStatus)
	// OnSpeedUpdate 处理速度更新
	OnSpeedUpdate(transferID string, bytesPerSecond float64, estimatedTimeLeft time.Duration)
	// OnError 处理错误
	OnError(transferID string, err error, isFatal bool)
	// OnComplete 处理传输完成
	OnComplete(transferID string, totalBytes int64, totalTime time.Duration)
}

// ProgressTracker 接口，用于跟踪传输进度
type ProgressTracker interface {
	// StartTracking 开始跟踪传输进度
	StartTracking(transferID string, totalSize int64) error
	// UpdateProgress 更新传输进度
	UpdateProgress(transferID string, bytesTransferred int64) error
	// UpdateStatus 更新传输状态
	UpdateStatus(transferID string, status TransferStatus) error
	// StopTracking 停止跟踪传输进度
	StopTracking(transferID string) error
	// AddCallback 添加进度回调
	AddCallback(callback ProgressCallback)
	// PauseTracking 暂停跟踪
	PauseTracking(transferID string) error
	// ResumeTracking 恢复跟踪
	ResumeTracking(transferID string) error
	// MarkFailed 标记传输失败
	MarkFailed(transferID string, err error) error
	// GetActiveTransfers 获取活跃的传输ID列表
	GetActiveTransfers() []string
	// UpdateTotalSize 更新传输总大小
	UpdateTotalSize(transferID string, newTotalSize int64) error

	// 以下是为了支持仪表盘功能而添加的方法
	// GetStatus 获取传输的当前状态
	GetStatus(transferID string) TransferStatus
	// GetProgress 获取传输的进度信息，返回已传输字节数、总字节数和百分比
	GetProgress(transferID string) (transferred int64, total int64, percentage float64)
	// GetSpeed 获取传输的当前速度（字节/秒）
	GetSpeed(transferID string) float64
	// GetStartTime 获取传输的开始时间
	GetStartTime(transferID string) time.Time
	// GetElapsedTime 获取传输的已用时间
	GetElapsedTime(transferID string) time.Duration
}

// defaultProgressTracker 是 ProgressTracker 的默认实现
type defaultProgressTracker struct {
	// 传输记录
	transfers map[string]*transferRecord

	// 回调列表
	callbacks []ProgressCallback

	// 同步锁
	mu sync.RWMutex

	// 是否启用速度采样
	enableSpeedSampling bool

	// 速度采样窗口大小
	speedSampleSize int

	// 速度计算间隔
	speedCalcInterval time.Duration

	// 进度更新阈值（百分比）
	progressThreshold float64

	// 历史记录清理器
	cleanupTicker *time.Ticker

	// 历史记录保留时间
	historyRetention time.Duration
}

// transferRecord 记录单个传输的信息
type transferRecord struct {
	// 传输ID
	ID string

	// 总大小（字节）
	TotalSize int64

	// 已传输大小（字节）
	Transferred int64

	// 完成百分比
	Percentage float64

	// 当前状态
	Status TransferStatus

	// 开始时间
	StartTime time.Time

	// 上次更新时间
	LastUpdateTime time.Time

	// 存储速度计算样本的数组，用于平滑计算
	speedSamples []float64

	// 当前传输速度（字节/秒）
	CurrentSpeed float64

	// 上次通知进度时的百分比，用于控制通知频率
	LastNotifiedPercentage float64

	// 是否已发送完成通知
	CompletionNotified bool

	// 如果失败，记录错误信息
	Error error
}

// ProgressTrackerOption 进度跟踪器选项
type ProgressTrackerOption func(*defaultProgressTracker)

// WithSpeedSampling 设置速度采样
func WithSpeedSampling(enabled bool, sampleSize int) ProgressTrackerOption {
	return func(pt *defaultProgressTracker) {
		pt.enableSpeedSampling = enabled
		if sampleSize > 0 {
			pt.speedSampleSize = sampleSize
		}
	}
}

// WithSpeedCalcInterval 设置速度计算间隔
func WithSpeedCalcInterval(interval time.Duration) ProgressTrackerOption {
	return func(pt *defaultProgressTracker) {
		if interval > 0 {
			pt.speedCalcInterval = interval
		}
	}
}

// WithProgressThreshold 设置进度更新阈值
func WithProgressThreshold(threshold float64) ProgressTrackerOption {
	return func(pt *defaultProgressTracker) {
		if threshold > 0 {
			pt.progressThreshold = threshold
		}
	}
}

// WithHistoryRetention 设置历史记录保留时间
func WithHistoryRetention(retention time.Duration) ProgressTrackerOption {
	return func(pt *defaultProgressTracker) {
		if retention > 0 {
			pt.historyRetention = retention
		}
	}
}

// NewProgressTracker 创建一个新的进度跟踪器
func NewProgressTracker(options ...ProgressTrackerOption) ProgressTracker {
	pt := &defaultProgressTracker{
		transfers:           make(map[string]*transferRecord),
		callbacks:           make([]ProgressCallback, 0),
		enableSpeedSampling: true,
		speedSampleSize:     DefaultSpeedSampleSize,
		speedCalcInterval:   DefaultSpeedCalcInterval,
		progressThreshold:   DefaultProgressThreshold,
		historyRetention:    DefaultHistoryRetention,
	}

	// 应用选项
	for _, opt := range options {
		opt(pt)
	}

	// 启动历史记录清理器
	pt.cleanupTicker = time.NewTicker(pt.historyRetention / 10)
	go pt.cleanupRoutine()

	return pt
}

// cleanupRoutine 清理已完成的历史记录
func (pt *defaultProgressTracker) cleanupRoutine() {
	for range pt.cleanupTicker.C {
		pt.mu.Lock()
		now := time.Now()
		for id, record := range pt.transfers {
			// 清理已完成、失败或取消的传输记录
			if (record.Status == StatusCompleted || record.Status == StatusFailed || record.Status == StatusCancelled) &&
				now.Sub(record.LastUpdateTime) > pt.historyRetention {
				delete(pt.transfers, id)
			}
		}
		pt.mu.Unlock()
	}
}

// StartTracking 实现 ProgressTracker 接口
func (t *defaultProgressTracker) StartTracking(transferID string, totalSize int64) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// 如果已经存在，检查是否可以重用
	if record, exists := t.transfers[transferID]; exists {
		// 如果状态是已完成、失败或取消，则可以重用
		if record.Status == StatusCompleted || record.Status == StatusFailed || record.Status == StatusCancelled {
			// 重置记录
			record.TotalSize = totalSize
			record.Transferred = 0
			record.Percentage = 0
			record.Status = StatusInitializing
			record.StartTime = time.Now()
			record.LastUpdateTime = time.Now()
			record.speedSamples = make([]float64, 0, 10)
			record.CurrentSpeed = 0
			record.LastNotifiedPercentage = 0
			record.CompletionNotified = false
			record.Error = nil

			// 通知状态变化
			t.notifyStatusChange(record.ID, StatusCancelled, StatusInitializing)
			return nil
		}
		// 否则，不允许重用
		return ErrTransferAlreadyActive
	}

	// 创建新记录
	t.transfers[transferID] = &transferRecord{
		ID:             transferID,
		TotalSize:      totalSize,
		Transferred:    0,
		Percentage:     0,
		Status:         StatusInitializing,
		StartTime:      time.Now(),
		LastUpdateTime: time.Now(),
		speedSamples:   make([]float64, 0, 10),
		CurrentSpeed:   0,
	}

	// 通知状态初始化
	t.notifyStatusChange(transferID, StatusInitializing, StatusInitializing)

	return nil
}

// UpdateProgress 实现 ProgressTracker 接口
func (t *defaultProgressTracker) UpdateProgress(transferID string, bytesTransferred int64) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	record, exists := t.transfers[transferID]
	if !exists {
		return ErrTransferNotFound
	}

	// 检查状态
	if record.Status != StatusTransferring && record.Status != StatusInitializing && record.Status != StatusResuming {
		// 如果不是正在传输、初始化或恢复状态，则不更新进度
		if record.Status == StatusPaused {
			return ErrTransferPaused
		} else if record.Status == StatusCancelled {
			return ErrTransferCancelled
		} else if record.Status == StatusFailed {
			return ErrTransferFailed
		} else if record.Status == StatusCompleted {
			return ErrTransferCompleted
		}
		return ErrInvalidTransferState
	}

	// 如果当前状态是初始化或恢复，变更为传输中
	if record.Status == StatusInitializing || record.Status == StatusResuming {
		oldStatus := record.Status
		record.Status = StatusTransferring
		t.notifyStatusChange(transferID, oldStatus, StatusTransferring)
	}

	// 更新已传输字节数
	record.Transferred = bytesTransferred

	// 计算百分比
	if record.TotalSize > 0 {
		record.Percentage = float64(bytesTransferred) * 100.0 / float64(record.TotalSize)
	} else {
		record.Percentage = 0
	}

	// 计算传输速度
	now := time.Now()
	elapsed := now.Sub(record.LastUpdateTime)

	// 只有当有足够的时间间隔时才计算速度，避免频繁计算导致不稳定
	if elapsed > 200*time.Millisecond {
		// 计算当前瞬时速度 (bytesTransferred / elapsed)
		instantSpeed := float64(bytesTransferred-record.Transferred) / elapsed.Seconds()

		// 更新速度样本，保持最多10个样本
		record.speedSamples = append(record.speedSamples, instantSpeed)
		if len(record.speedSamples) > 10 {
			record.speedSamples = record.speedSamples[1:]
		}

		// 计算平均速度
		record.CurrentSpeed = t.calculateSpeed(record.speedSamples)

		// 更新最后更新时间
		record.LastUpdateTime = now
	}

	// 检查是否需要通知进度更新
	// 通知策略：
	// 1. 当进度变化超过1%时通知
	// 2. 当总大小较小时（<1MB），每10%通知一次
	// 3. 当总大小中等时（1MB-100MB），每5%通知一次
	// 4. 当总大小较大时（>100MB），每1%通知一次
	notifyThreshold := 1.0
	if record.TotalSize < 1024*1024 { // < 1MB
		notifyThreshold = 10.0
	} else if record.TotalSize < 100*1024*1024 { // < 100MB
		notifyThreshold = 5.0
	}

	if record.Percentage-record.LastNotifiedPercentage >= notifyThreshold ||
		record.Percentage >= 100.0 && !record.CompletionNotified {
		// 通知进度更新
		t.notifyProgress(record.ID, record.TotalSize, record.Transferred, record.Percentage)
		record.LastNotifiedPercentage = record.Percentage

		// 通知速度更新
		// 计算预估剩余时间
		var estimatedTimeLeft time.Duration
		if record.CurrentSpeed > 0 && record.TotalSize > record.Transferred {
			remainingBytes := record.TotalSize - record.Transferred
			estimatedTimeLeft = time.Duration(float64(remainingBytes) / record.CurrentSpeed * float64(time.Second))
		}
		t.notifySpeedUpdate(record.ID, record.CurrentSpeed, estimatedTimeLeft)
	}

	// 检查是否完成
	if bytesTransferred >= record.TotalSize && record.TotalSize > 0 {
		// 更新状态为已完成
		oldStatus := record.Status
		record.Status = StatusCompleted
		record.Percentage = 100.0
		record.CompletionNotified = true

		// 通知状态变化
		t.notifyStatusChange(record.ID, oldStatus, StatusCompleted)

		// 通知完成
		totalTime := time.Since(record.StartTime)
		t.notifyCompletion(record.ID, record.TotalSize, totalTime)
	}

	return nil
}

// UpdateStatus 实现 ProgressTracker 接口
func (t *defaultProgressTracker) UpdateStatus(transferID string, status TransferStatus) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	record, exists := t.transfers[transferID]
	if !exists {
		return ErrTransferNotFound
	}

	// 检查状态转换是否有效
	if !isValidStatusTransition(record.Status, status) {
		return ErrInvalidStatusTransition
	}

	// 更新状态
	oldStatus := record.Status
	record.Status = status
	record.LastUpdateTime = time.Now()

	// 通知状态变化
	t.notifyStatusChange(transferID, oldStatus, status)

	return nil
}

// StopTracking 实现 ProgressTracker 接口
func (t *defaultProgressTracker) StopTracking(transferID string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	record, exists := t.transfers[transferID]
	if !exists {
		return ErrTransferNotFound
	}

	// 如果传输还未完成或失败，则标记为取消
	if record.Status != StatusCompleted && record.Status != StatusFailed {
		oldStatus := record.Status
		record.Status = StatusCancelled
		record.LastUpdateTime = time.Now()

		// 通知状态变化
		t.notifyStatusChange(transferID, oldStatus, StatusCancelled)
	}

	return nil
}

// AddCallback 实现 ProgressTracker 接口
func (t *defaultProgressTracker) AddCallback(callback ProgressCallback) {
	if callback == nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// 检查回调是否已存在
	for _, cb := range t.callbacks {
		if cb == callback {
			return // 已存在，不重复添加
		}
	}

	t.callbacks = append(t.callbacks, callback)
}

// PauseTracking 实现 ProgressTracker 接口
func (t *defaultProgressTracker) PauseTracking(transferID string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	record, exists := t.transfers[transferID]
	if !exists {
		return ErrTransferNotFound
	}

	// 只有正在传输的才能暂停
	if record.Status != StatusTransferring && record.Status != StatusInitializing {
		return ErrCannotPause
	}

	oldStatus := record.Status
	record.Status = StatusPaused
	record.LastUpdateTime = time.Now()

	// 通知状态变化
	t.notifyStatusChange(transferID, oldStatus, StatusPaused)

	return nil
}

// ResumeTracking 实现 ProgressTracker 接口
func (t *defaultProgressTracker) ResumeTracking(transferID string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	record, exists := t.transfers[transferID]
	if !exists {
		return ErrTransferNotFound
	}

	// 只有已暂停的才能恢复
	if record.Status != StatusPaused {
		return ErrCannotResume
	}

	oldStatus := record.Status
	record.Status = StatusResuming
	record.LastUpdateTime = time.Now()

	// 通知状态变化
	t.notifyStatusChange(transferID, oldStatus, StatusResuming)

	return nil
}

// MarkFailed 实现 ProgressTracker 接口
func (t *defaultProgressTracker) MarkFailed(transferID string, err error) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	record, exists := t.transfers[transferID]
	if !exists {
		return ErrTransferNotFound
	}

	// 如果已经是完成或取消状态，不允许标记为失败
	if record.Status == StatusCompleted || record.Status == StatusCancelled {
		return ErrCannotMarkFailed
	}

	oldStatus := record.Status
	record.Status = StatusFailed
	record.Error = err
	record.LastUpdateTime = time.Now()

	// 通知状态变化
	t.notifyStatusChange(transferID, oldStatus, StatusFailed)

	// 通知错误
	t.notifyError(transferID, err, true)

	return nil
}

// GetActiveTransfers 实现 ProgressTracker 接口
func (t *defaultProgressTracker) GetActiveTransfers() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	activeTransfers := make([]string, 0)
	for id, record := range t.transfers {
		// 活跃的传输是那些不是已完成、失败或取消状态的传输
		if record.Status != StatusCompleted && record.Status != StatusFailed && record.Status != StatusCancelled {
			activeTransfers = append(activeTransfers, id)
		}
	}

	return activeTransfers
}

// GetStatus 获取传输的当前状态
func (t *defaultProgressTracker) GetStatus(transferID string) TransferStatus {
	t.mu.RLock()
	defer t.mu.RUnlock()

	record, exists := t.transfers[transferID]
	if !exists {
		return StatusFailed // 如果找不到传输，默认返回失败状态
	}

	return record.Status
}

// GetProgress 获取传输的进度信息
func (t *defaultProgressTracker) GetProgress(transferID string) (transferred int64, total int64, percentage float64) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	record, exists := t.transfers[transferID]
	if !exists {
		return 0, 0, 0
	}

	return record.Transferred, record.TotalSize, record.Percentage
}

// GetSpeed 获取传输的当前速度
func (t *defaultProgressTracker) GetSpeed(transferID string) float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()

	record, exists := t.transfers[transferID]
	if !exists {
		return 0
	}

	return record.CurrentSpeed
}

// GetStartTime 获取传输的开始时间
func (t *defaultProgressTracker) GetStartTime(transferID string) time.Time {
	t.mu.RLock()
	defer t.mu.RUnlock()

	record, exists := t.transfers[transferID]
	if !exists {
		return time.Time{} // 返回零值时间
	}

	return record.StartTime
}

// GetElapsedTime 获取传输的已用时间
func (t *defaultProgressTracker) GetElapsedTime(transferID string) time.Duration {
	t.mu.RLock()
	defer t.mu.RUnlock()

	record, exists := t.transfers[transferID]
	if !exists {
		return 0
	}

	return time.Since(record.StartTime)
}

// calculateSpeed 计算平均速度
func (t *defaultProgressTracker) calculateSpeed(samples []float64) float64 {
	if len(samples) == 0 {
		return 0
	}

	// 计算所有样本的平均值
	var sum float64
	for _, speed := range samples {
		sum += speed
	}
	return sum / float64(len(samples))
}

// notifyProgress 通知进度更新
func (t *defaultProgressTracker) notifyProgress(transferID string, total int64, transferred int64, percentage float64) {
	for _, callback := range t.callbacks {
		go callback.OnProgress(transferID, total, transferred, percentage)
	}
}

// notifyStatusChange 通知状态变化
func (t *defaultProgressTracker) notifyStatusChange(transferID string, oldStatus, newStatus TransferStatus) {
	for _, callback := range t.callbacks {
		go callback.OnStatusChange(transferID, oldStatus, newStatus)
	}
}

// notifySpeedUpdate 通知速度更新
func (t *defaultProgressTracker) notifySpeedUpdate(transferID string, bytesPerSecond float64, estimatedTimeLeft time.Duration) {
	for _, callback := range t.callbacks {
		go callback.OnSpeedUpdate(transferID, bytesPerSecond, estimatedTimeLeft)
	}
}

// notifyError 通知错误
func (t *defaultProgressTracker) notifyError(transferID string, err error, isFatal bool) {
	for _, callback := range t.callbacks {
		go callback.OnError(transferID, err, isFatal)
	}
}

// notifyCompletion 通知完成
func (t *defaultProgressTracker) notifyCompletion(transferID string, totalBytes int64, totalTime time.Duration) {
	for _, callback := range t.callbacks {
		go callback.OnComplete(transferID, totalBytes, totalTime)
	}
}

// isValidStatusTransition 检查状态转换是否有效
func isValidStatusTransition(oldStatus, newStatus TransferStatus) bool {
	switch oldStatus {
	case StatusInitializing:
		// 从初始化状态可以转换到传输中、暂停、失败或取消
		return newStatus == StatusTransferring || newStatus == StatusPaused ||
			newStatus == StatusFailed || newStatus == StatusCancelled
	case StatusTransferring:
		// 从传输中状态可以转换到暂停、完成、失败或取消
		return newStatus == StatusPaused || newStatus == StatusCompleted ||
			newStatus == StatusFailed || newStatus == StatusCancelled
	case StatusPaused:
		// 从暂停状态可以转换到恢复、失败或取消
		return newStatus == StatusResuming || newStatus == StatusFailed ||
			newStatus == StatusCancelled
	case StatusResuming:
		// 从恢复状态可以转换到传输中、暂停、失败或取消
		return newStatus == StatusTransferring || newStatus == StatusPaused ||
			newStatus == StatusFailed || newStatus == StatusCancelled
	case StatusCompleted:
		// 已完成状态不允许转换到其他状态
		return false
	case StatusFailed:
		// 失败状态只能转换到初始化状态（重试）
		return newStatus == StatusInitializing
	case StatusCancelled:
		// 取消状态只能转换到初始化状态（重试）
		return newStatus == StatusInitializing
	default:
		return false
	}
}

// 定义错误类型
var (
	ErrTransferNotFound        = NewPointSubError("transfer not found", "传输未找到")
	ErrTransferAlreadyActive   = NewPointSubError("transfer already active", "传输已经处于活跃状态")
	ErrInvalidTransferState    = NewPointSubError("invalid transfer state", "传输状态无效")
	ErrTransferPaused          = NewPointSubError("transfer is paused", "传输已暂停")
	ErrTransferCancelled       = NewPointSubError("transfer is cancelled", "传输已取消")
	ErrTransferFailed          = NewPointSubError("transfer has failed", "传输已失败")
	ErrTransferCompleted       = NewPointSubError("transfer is already completed", "传输已完成")
	ErrCannotPause             = NewPointSubError("cannot pause transfer in current state", "当前状态下无法暂停传输")
	ErrCannotResume            = NewPointSubError("cannot resume transfer in current state", "当前状态下无法恢复传输")
	ErrCannotMarkFailed        = NewPointSubError("cannot mark transfer as failed in current state", "当前状态下无法将传输标记为失败")
	ErrInvalidStatusTransition = NewPointSubError("invalid status transition", "无效的状态转换")
)

// NewPointSubError 创建一个新的错误
func NewPointSubError(engMsg, cnMsg string) error {
	return &PointSubError{
		EngMsg: engMsg,
		CnMsg:  cnMsg,
	}
}

// PointSubError 是一个自定义错误类型
type PointSubError struct {
	EngMsg string
	CnMsg  string
}

// Error 实现 error 接口
func (e *PointSubError) Error() string {
	return e.EngMsg
}

// LocalizedError 返回本地化的错误消息
func (e *PointSubError) LocalizedError() string {
	return e.CnMsg
}

// UpdateTotalSize 更新传输总大小
func (t *defaultProgressTracker) UpdateTotalSize(transferID string, newTotalSize int64) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	record, exists := t.transfers[transferID]
	if !exists {
		return NewPointSubError(
			"transfer record not found",
			"找不到传输记录",
		)
	}

	// 更新总大小
	oldTotalSize := record.TotalSize
	record.TotalSize = newTotalSize

	// 重新计算百分比
	if record.TotalSize > 0 {
		record.Percentage = float64(record.Transferred) / float64(record.TotalSize) * 100
	} else {
		record.Percentage = 0
	}

	// 记录日志
	logger.Debug("传输 %s 总大小从 %d 更新为 %d 字节", transferID, oldTotalSize, newTotalSize)

	return nil
}
