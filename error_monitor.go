package pointsub

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ErrorMonitorConfig 错误监控配置
type ErrorMonitorConfig struct {
	// 启用错误监控
	Enabled bool

	// 错误历史记录大小
	HistorySize int

	// 错误分析周期
	AnalysisPeriod time.Duration

	// 重要错误阈值
	CriticalThreshold int

	// 警告错误阈值
	WarningThreshold int

	// 是否记录详细错误信息
	LogDetailedErrors bool

	// 记录错误上下文
	LogErrorContext bool
}

// ErrorRecord 错误记录
type ErrorRecord struct {
	// 错误时间
	Timestamp time.Time

	// 错误类型
	Type ErrorType

	// 错误级别
	Severity ErrorSeverity

	// 错误消息
	Message string

	// 是否可恢复
	Recoverable bool

	// 错误上下文
	Context ErrorContext

	// 错误分类 (如网络错误、文件错误等)
	Category string

	// 错误源 (如传输组件、连接组件等)
	Source string
}

// ErrorStatistics 错误统计信息
type ErrorStatistics struct {
	// 总错误数
	TotalErrors int64

	// 按严重性统计的错误数
	SeverityCounts map[ErrorSeverity]int64

	// 按类型统计的错误数
	TypeCounts map[ErrorType]int64

	// 按类别统计的错误数
	CategoryCounts map[string]int64

	// 按源统计的错误数
	SourceCounts map[string]int64

	// 可恢复错误数
	RecoverableCount int64

	// 不可恢复错误数
	NonRecoverableCount int64

	// 最频繁的错误
	MostFrequentErrors map[string]int64

	// 最近一次错误时间
	LastErrorTime time.Time

	// 错误频率 (每分钟)
	ErrorRate float64

	// 统计开始时间
	StartTime time.Time

	// 统计结束时间
	EndTime time.Time
}

// ErrorTrend 错误趋势
type ErrorTrend struct {
	// 时间点
	TimePoint time.Time

	// 该时间点的错误数
	ErrorCount int

	// 该时间点的错误率
	ErrorRate float64
}

// ErrorAlertConfig 错误警报配置
type ErrorAlertConfig struct {
	// 错误率阈值 (每分钟)
	ErrorRateThreshold float64

	// 严重错误阈值
	CriticalErrorThreshold int

	// 连续错误阈值
	ConsecutiveErrorThreshold int

	// 警报间隔
	AlertInterval time.Duration

	// 静默期
	QuietPeriod time.Duration
}

// ErrorMonitor 错误监控器
type ErrorMonitor struct {
	// 配置
	config ErrorMonitorConfig

	// 错误历史记录
	errorHistory []*ErrorRecord

	// 错误计数
	errorCount int64

	// 错误统计
	statistics ErrorStatistics

	// 错误趋势
	trends []ErrorTrend

	// 错误监听器列表
	listeners []ErrorMonitorListener

	// 警报配置
	alertConfig ErrorAlertConfig

	// 上次警报时间
	lastAlertTime time.Time

	// 错误缓冲区，用于批处理
	errorBuffer chan *ErrorRecord

	// 锁，保护errorHistory和trends
	mu sync.RWMutex

	// 是否正在运行
	running atomic.Bool
}

// ErrorMonitorListener 错误监听器接口
type ErrorMonitorListener interface {
	// 处理错误
	OnError(record *ErrorRecord)

	// 处理统计信息更新
	OnStatisticsUpdated(stats ErrorStatistics)

	// 处理警报
	OnAlert(alertType string, message string, stats ErrorStatistics)
}

// LoggingErrorListener 日志错误监听器
type LoggingErrorListener struct {
	// 日志前缀
	Prefix string

	// 是否记录详细信息
	Verbose bool

	// 日志输出函数
	LogFunc func(string)
}

// OnError 实现ErrorMonitorListener接口
func (l *LoggingErrorListener) OnError(record *ErrorRecord) {
	if l.LogFunc == nil {
		return
	}

	// 基本错误信息
	msg := fmt.Sprintf("%s错误: [%s] %s", l.Prefix,
		getSeverityString(record.Severity), record.Message)

	// 添加详细信息
	if l.Verbose {
		msg += fmt.Sprintf(" [类型: %s, 来源: %s, 类别: %s, 可恢复: %v, 时间: %s]",
			getTypeString(record.Type), record.Source, record.Category,
			record.Recoverable, record.Timestamp.Format(time.RFC3339))
	}

	l.LogFunc(msg)
}

// OnStatisticsUpdated 实现ErrorMonitorListener接口
func (l *LoggingErrorListener) OnStatisticsUpdated(stats ErrorStatistics) {
	if !l.Verbose || l.LogFunc == nil {
		return
	}

	msg := fmt.Sprintf("%s统计更新: 总错误数: %d, 可恢复: %d, 不可恢复: %d, 错误率: %.2f/分钟",
		l.Prefix, stats.TotalErrors, stats.RecoverableCount, stats.NonRecoverableCount, stats.ErrorRate)

	l.LogFunc(msg)
}

// OnAlert 实现ErrorMonitorListener接口
func (l *LoggingErrorListener) OnAlert(alertType string, message string, stats ErrorStatistics) {
	if l.LogFunc == nil {
		return
	}

	msg := fmt.Sprintf("%s警报 [%s]: %s (总错误: %d, 错误率: %.2f/分钟)",
		l.Prefix, alertType, message, stats.TotalErrors, stats.ErrorRate)

	l.LogFunc(msg)
}

// 辅助函数，将ErrorSeverity转换为字符串
func getSeverityString(severity ErrorSeverity) string {
	switch severity {
	case SeverityInfo:
		return "信息"
	case SeverityWarning:
		return "警告"
	case SeverityError:
		return "错误"
	case SeverityCritical:
		return "严重"
	case SeverityFatal:
		return "致命"
	default:
		return "未知"
	}
}

// 辅助函数，将ErrorType转换为字符串
func getTypeString(errorType ErrorType) string {
	switch errorType {
	case NetworkConnError:
		return "网络连接"
	case NetworkTimeoutError:
		return "网络超时"
	case NetworkIOError:
		return "网络IO"
	case ProtocolParseError:
		return "协议解析"
	case ProtocolUnsupportedError:
		return "协议不支持"
	case ProtocolSecurityError:
		return "协议安全"
	case SystemResourceError:
		return "系统资源"
	case SystemInternalError:
		return "系统内部"
	case UserInputError:
		return "用户输入"
	case UserConfigError:
		return "用户配置"
	default:
		return "未知"
	}
}

// NewErrorMonitor 创建新的错误监控器
func NewErrorMonitor(config ErrorMonitorConfig) *ErrorMonitor {
	// 使用默认配置
	if config.HistorySize <= 0 {
		config.HistorySize = 1000
	}
	if config.AnalysisPeriod <= 0 {
		config.AnalysisPeriod = 5 * time.Minute
	}

	monitor := &ErrorMonitor{
		config:       config,
		errorHistory: make([]*ErrorRecord, 0, config.HistorySize),
		statistics: ErrorStatistics{
			SeverityCounts:     make(map[ErrorSeverity]int64),
			TypeCounts:         make(map[ErrorType]int64),
			CategoryCounts:     make(map[string]int64),
			SourceCounts:       make(map[string]int64),
			MostFrequentErrors: make(map[string]int64),
			StartTime:          time.Now(),
		},
		trends:      make([]ErrorTrend, 0, 24), // 存储24个时间点的趋势
		errorBuffer: make(chan *ErrorRecord, 100),
		alertConfig: ErrorAlertConfig{
			ErrorRateThreshold:        10.0,             // 每分钟10个错误
			CriticalErrorThreshold:    5,                // 5个严重错误
			ConsecutiveErrorThreshold: 3,                // 3个连续错误
			AlertInterval:             15 * time.Minute, // 警报间隔15分钟
			QuietPeriod:               1 * time.Hour,    // 静默期1小时
		},
	}

	return monitor
}

// Start 启动错误监控器
func (m *ErrorMonitor) Start() {
	if m.running.Load() {
		return
	}

	m.running.Store(true)

	// 启动错误处理协程
	go m.processErrors()

	// 启动统计分析协程
	go m.analyzeErrorsRoutine()
}

// Stop 停止错误监控器
func (m *ErrorMonitor) Stop() {
	if !m.running.Load() {
		return
	}

	m.running.Store(false)
	close(m.errorBuffer)

	// 更新统计结束时间
	m.mu.Lock()
	m.statistics.EndTime = time.Now()
	m.mu.Unlock()
}

// processErrors 处理错误缓冲区中的错误
func (m *ErrorMonitor) processErrors() {
	for record := range m.errorBuffer {
		if !m.running.Load() {
			return
		}

		m.mu.Lock()
		// 添加到历史记录
		m.errorHistory = append(m.errorHistory, record)
		// 如果超过历史记录大小，移除最旧的记录
		if len(m.errorHistory) > m.config.HistorySize {
			m.errorHistory = m.errorHistory[1:]
		}
		m.mu.Unlock()

		// 更新统计信息
		m.updateStatistics(record)

		// 通知监听器
		for _, listener := range m.listeners {
			go listener.OnError(record)
		}

		// 检查是否需要触发警报
		m.checkAlerts()
	}
}

// RecordError 记录错误
func (m *ErrorMonitor) RecordError(err error, source, category string) {
	if !m.config.Enabled || !m.running.Load() {
		return
	}

	// 尝试转换为MessageError
	var msgErr *MessageError
	if errors.As(err, &msgErr) {
		record := &ErrorRecord{
			Timestamp:   time.Now(),
			Type:        msgErr.Type,
			Severity:    msgErr.Severity,
			Message:     msgErr.Error(),
			Recoverable: msgErr.Recoverable,
			Context:     msgErr.Context,
			Category:    category,
			Source:      source,
		}

		// 添加到错误缓冲区
		select {
		case m.errorBuffer <- record:
			// 添加成功
		default:
			// 缓冲区已满，丢弃错误
		}
	} else {
		// 不是MessageError，创建基本错误记录
		record := &ErrorRecord{
			Timestamp:   time.Now(),
			Type:        SystemInternalError, // 默认类型
			Severity:    SeverityError,       // 默认级别
			Message:     err.Error(),
			Recoverable: false, // 默认不可恢复
			Context:     ErrorContext{Timestamp: time.Now()},
			Category:    category,
			Source:      source,
		}

		// 添加到错误缓冲区
		select {
		case m.errorBuffer <- record:
			// 添加成功
		default:
			// 缓冲区已满，丢弃错误
		}
	}

	// 增加错误计数
	atomic.AddInt64(&m.errorCount, 1)
}

// updateStatistics 更新错误统计信息
func (m *ErrorMonitor) updateStatistics(record *ErrorRecord) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 更新总错误数
	m.statistics.TotalErrors++

	// 更新按严重性统计
	m.statistics.SeverityCounts[record.Severity]++

	// 更新按类型统计
	m.statistics.TypeCounts[record.Type]++

	// 更新按类别统计
	m.statistics.CategoryCounts[record.Category]++

	// 更新按源统计
	m.statistics.SourceCounts[record.Source]++

	// 更新可恢复/不可恢复统计
	if record.Recoverable {
		m.statistics.RecoverableCount++
	} else {
		m.statistics.NonRecoverableCount++
	}

	// 更新最频繁错误
	m.statistics.MostFrequentErrors[record.Message]++

	// 更新最近一次错误时间
	m.statistics.LastErrorTime = record.Timestamp

	// 计算错误率
	duration := time.Since(m.statistics.StartTime).Minutes()
	if duration > 0 {
		m.statistics.ErrorRate = float64(m.statistics.TotalErrors) / duration
	}
}

// analyzeErrorsRoutine 定期分析错误
func (m *ErrorMonitor) analyzeErrorsRoutine() {
	ticker := time.NewTicker(m.config.AnalysisPeriod)
	defer ticker.Stop()

	for {
		if !m.running.Load() {
			return
		}

		select {
		case <-ticker.C:
			// 执行错误分析
			m.analyzeErrors()

			// 更新错误趋势
			m.updateTrend()

			// 通知监听器
			m.mu.RLock()
			stats := m.statistics
			m.mu.RUnlock()

			for _, listener := range m.listeners {
				go listener.OnStatisticsUpdated(stats)
			}
		}
	}
}

// analyzeErrors 分析错误模式
func (m *ErrorMonitor) analyzeErrors() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 这里可以添加更复杂的错误分析逻辑
	// 例如检测错误率增长趋势、识别常见错误模式等
}

// updateTrend 更新错误趋势
func (m *ErrorMonitor) updateTrend() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 获取当前时间点
	now := time.Now()

	// 计算当前时间点的错误数和错误率
	var count int
	for _, record := range m.errorHistory {
		if now.Sub(record.Timestamp) <= m.config.AnalysisPeriod {
			count++
		}
	}

	rate := float64(count) / m.config.AnalysisPeriod.Minutes()

	// 添加新的趋势点
	trend := ErrorTrend{
		TimePoint:  now,
		ErrorCount: count,
		ErrorRate:  rate,
	}

	m.trends = append(m.trends, trend)

	// 保持趋势数据在24个点以内
	if len(m.trends) > 24 {
		m.trends = m.trends[len(m.trends)-24:]
	}
}

// checkAlerts 检查是否需要触发警报
func (m *ErrorMonitor) checkAlerts() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 检查是否在静默期内
	if time.Since(m.lastAlertTime) < m.alertConfig.QuietPeriod {
		return
	}

	// 检查错误率是否超过阈值
	if m.statistics.ErrorRate > m.alertConfig.ErrorRateThreshold {
		message := fmt.Sprintf("错误率过高: %.2f/分钟 (阈值: %.2f/分钟)",
			m.statistics.ErrorRate, m.alertConfig.ErrorRateThreshold)
		m.triggerAlert("错误率", message)
		return
	}

	// 检查严重错误数是否超过阈值
	criticalCount := m.statistics.SeverityCounts[SeverityCritical] + m.statistics.SeverityCounts[SeverityFatal]
	if criticalCount > int64(m.alertConfig.CriticalErrorThreshold) {
		message := fmt.Sprintf("严重错误数量过多: %d (阈值: %d)",
			criticalCount, m.alertConfig.CriticalErrorThreshold)
		m.triggerAlert("严重错误", message)
		return
	}

	// 检查连续错误
	if len(m.errorHistory) >= m.alertConfig.ConsecutiveErrorThreshold {
		// 检查最近几个错误是否连续发生（时间上接近）
		consecutive := true
		for i := len(m.errorHistory) - 1; i > len(m.errorHistory)-m.alertConfig.ConsecutiveErrorThreshold; i-- {
			if m.errorHistory[i].Timestamp.Sub(m.errorHistory[i-1].Timestamp) > 5*time.Second {
				consecutive = false
				break
			}
		}

		if consecutive {
			message := fmt.Sprintf("检测到连续错误: %d个错误在短时间内连续发生",
				m.alertConfig.ConsecutiveErrorThreshold)
			m.triggerAlert("连续错误", message)
		}
	}
}

// triggerAlert 触发警报
func (m *ErrorMonitor) triggerAlert(alertType, message string) {
	// 记录本次警报时间
	m.mu.Lock()
	now := time.Now()
	if now.Sub(m.lastAlertTime) < m.alertConfig.AlertInterval {
		m.mu.Unlock()
		return
	}
	m.lastAlertTime = now
	stats := m.statistics
	m.mu.Unlock()

	// 通知监听器
	for _, listener := range m.listeners {
		go listener.OnAlert(alertType, message, stats)
	}
}

// AddListener 添加错误监听器
func (m *ErrorMonitor) AddListener(listener ErrorMonitorListener) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.listeners = append(m.listeners, listener)
}

// RemoveListener 移除错误监听器
func (m *ErrorMonitor) RemoveListener(listener ErrorMonitorListener) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, l := range m.listeners {
		if l == listener {
			m.listeners = append(m.listeners[:i], m.listeners[i+1:]...)
			break
		}
	}
}

// GetStatistics 获取当前错误统计信息
func (m *ErrorMonitor) GetStatistics() ErrorStatistics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 返回统计信息的副本
	return m.statistics
}

// GetErrorHistory 获取错误历史记录
func (m *ErrorMonitor) GetErrorHistory() []*ErrorRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 返回历史记录的副本
	history := make([]*ErrorRecord, len(m.errorHistory))
	copy(history, m.errorHistory)
	return history
}

// GetErrorTrends 获取错误趋势
func (m *ErrorMonitor) GetErrorTrends() []ErrorTrend {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 返回趋势数据的副本
	trends := make([]ErrorTrend, len(m.trends))
	copy(trends, m.trends)
	return trends
}

// SetAlertConfig 设置警报配置
func (m *ErrorMonitor) SetAlertConfig(config ErrorAlertConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.alertConfig = config
}

// ResetStatistics 重置统计信息
func (m *ErrorMonitor) ResetStatistics() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.statistics = ErrorStatistics{
		SeverityCounts:     make(map[ErrorSeverity]int64),
		TypeCounts:         make(map[ErrorType]int64),
		CategoryCounts:     make(map[string]int64),
		SourceCounts:       make(map[string]int64),
		MostFrequentErrors: make(map[string]int64),
		StartTime:          time.Now(),
	}

	m.errorCount = 0
	m.trends = make([]ErrorTrend, 0, 24)
}
