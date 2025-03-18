package pointsub

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// ConsoleProgressCallback 命令行进度回调实现
type ConsoleProgressCallback struct {
	// 输出写入器，默认为标准输出
	output io.Writer

	// 进度条宽度
	width int

	// 更新间隔
	updateInterval time.Duration

	// 传输记录
	transfers map[string]*consoleTransferRecord

	// 同步锁
	mu sync.RWMutex

	// 是否显示详细信息
	verbose bool

	// 是否使用彩色输出
	useColor bool

	// 是否显示速度
	showSpeed bool

	// 是否显示剩余时间
	showETA bool

	// 进度条字符
	progressChar string
	emptyChar    string
}

// consoleTransferRecord 控制台显示用传输记录
type consoleTransferRecord struct {
	// 传输ID
	ID string

	// 总大小
	Total int64

	// 已传输大小
	Transferred int64

	// 进度百分比
	Percentage float64

	// 当前状态
	Status TransferStatus

	// 显示名称
	DisplayName string

	// 传输速度（字节/秒）
	Speed float64

	// 预估剩余时间
	ETA time.Duration

	// 上次更新时间
	LastUpdateTime time.Time

	// 上次显示时间
	LastDisplayTime time.Time

	// 最后显示的进度条
	LastProgressBar string

	// 是否已输出完成消息
	CompletionMessageShown bool

	// 是否显示
	Visible bool
}

// ConsoleProgressOption 控制台进度选项
type ConsoleProgressOption func(*ConsoleProgressCallback)

// WithOutput 设置输出写入器
func WithOutput(output io.Writer) ConsoleProgressOption {
	return func(c *ConsoleProgressCallback) {
		if output != nil {
			c.output = output
		}
	}
}

// WithProgressWidth 设置进度条宽度
func WithProgressWidth(width int) ConsoleProgressOption {
	return func(c *ConsoleProgressCallback) {
		if width > 10 {
			c.width = width
		}
	}
}

// WithUpdateInterval 设置更新间隔
func WithUpdateInterval(interval time.Duration) ConsoleProgressOption {
	return func(c *ConsoleProgressCallback) {
		if interval > 0 {
			c.updateInterval = interval
		}
	}
}

// WithVerboseOutput 设置是否显示详细信息
func WithVerboseOutput(verbose bool) ConsoleProgressOption {
	return func(c *ConsoleProgressCallback) {
		c.verbose = verbose
	}
}

// WithColorOutput 设置是否使用彩色输出
func WithColorOutput(useColor bool) ConsoleProgressOption {
	return func(c *ConsoleProgressCallback) {
		c.useColor = useColor
	}
}

// WithProgressChars 设置进度条字符
func WithProgressChars(progressChar, emptyChar string) ConsoleProgressOption {
	return func(c *ConsoleProgressCallback) {
		if progressChar != "" {
			c.progressChar = progressChar
		}
		if emptyChar != "" {
			c.emptyChar = emptyChar
		}
	}
}

// WithSpeedDisplay 设置是否显示速度
func WithSpeedDisplay(show bool) ConsoleProgressOption {
	return func(c *ConsoleProgressCallback) {
		c.showSpeed = show
	}
}

// WithETADisplay 设置是否显示剩余时间
func WithETADisplay(show bool) ConsoleProgressOption {
	return func(c *ConsoleProgressCallback) {
		c.showETA = show
	}
}

// NewConsoleProgressCallback 创建新的控制台进度回调
func NewConsoleProgressCallback(options ...ConsoleProgressOption) *ConsoleProgressCallback {
	callback := &ConsoleProgressCallback{
		output:         os.Stdout,
		width:          50,
		updateInterval: 200 * time.Millisecond,
		transfers:      make(map[string]*consoleTransferRecord),
		verbose:        false,
		useColor:       true,
		showSpeed:      true,
		showETA:        true,
		progressChar:   "█",
		emptyChar:      "░",
	}

	// 应用选项
	for _, opt := range options {
		opt(callback)
	}

	return callback
}

// RegisterTransfer 注册传输到控制台显示
func (c *ConsoleProgressCallback) RegisterTransfer(transferID string, displayName string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.transfers[transferID] = &consoleTransferRecord{
		ID:              transferID,
		DisplayName:     displayName,
		Status:          StatusInitializing,
		Visible:         true,
		LastUpdateTime:  time.Now(),
		LastDisplayTime: time.Now(),
	}
}

// SetVisible 设置传输是否可见
func (c *ConsoleProgressCallback) SetVisible(transferID string, visible bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if record, exists := c.transfers[transferID]; exists {
		record.Visible = visible
	}
}

// OnProgress 实现ProgressCallback接口
func (c *ConsoleProgressCallback) OnProgress(transferID string, total int64, transferred int64, percentage float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 获取或创建传输记录
	record, exists := c.transfers[transferID]
	if !exists {
		record = &consoleTransferRecord{
			ID:              transferID,
			DisplayName:     transferID, // 使用ID作为默认显示名称
			Status:          StatusTransferring,
			Visible:         true,
			LastUpdateTime:  time.Now(),
			LastDisplayTime: time.Now(),
		}
		c.transfers[transferID] = record
	}

	// 更新记录
	record.Total = total
	record.Transferred = transferred
	record.Percentage = percentage
	record.LastUpdateTime = time.Now()

	// 如果距离上次显示时间超过更新间隔，则更新显示
	if record.Visible && time.Since(record.LastDisplayTime) >= c.updateInterval {
		c.displayProgress(record)
		record.LastDisplayTime = time.Now()
	}
}

// OnStatusChange 实现ProgressCallback接口
func (c *ConsoleProgressCallback) OnStatusChange(transferID string, oldStatus, newStatus TransferStatus) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 获取或创建传输记录
	record, exists := c.transfers[transferID]
	if !exists {
		record = &consoleTransferRecord{
			ID:              transferID,
			DisplayName:     transferID,
			Visible:         true,
			LastUpdateTime:  time.Now(),
			LastDisplayTime: time.Now(),
		}
		c.transfers[transferID] = record
	}

	// 更新状态
	record.Status = newStatus
	record.LastUpdateTime = time.Now()

	// 立即更新显示（不受间隔限制）
	if record.Visible {
		c.displayStatusChange(record, oldStatus, newStatus)
		record.LastDisplayTime = time.Now()
	}
}

// OnSpeedUpdate 实现ProgressCallback接口
func (c *ConsoleProgressCallback) OnSpeedUpdate(transferID string, bytesPerSecond float64, estimatedTimeLeft time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 获取传输记录
	record, exists := c.transfers[transferID]
	if !exists {
		return
	}

	// 更新速度和ETA
	record.Speed = bytesPerSecond
	record.ETA = estimatedTimeLeft
	record.LastUpdateTime = time.Now()

	// 如果显示速度和ETA，且距离上次显示时间超过更新间隔，则更新显示
	if record.Visible && c.showSpeed && time.Since(record.LastDisplayTime) >= c.updateInterval {
		c.displayProgress(record)
		record.LastDisplayTime = time.Now()
	}
}

// OnError 实现ProgressCallback接口
func (c *ConsoleProgressCallback) OnError(transferID string, err error, isFatal bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 获取传输记录
	record, exists := c.transfers[transferID]
	if !exists {
		return
	}

	// 立即显示错误
	if record.Visible {
		// 清除上一行进度条
		if record.LastProgressBar != "" {
			c.clearLine()
		}

		// 显示错误消息
		errMsg := fmt.Sprintf("%s: 错误 - %v", getDisplayName(record), err)
		if isFatal {
			if c.useColor {
				errMsg = fmt.Sprintf("\033[31m%s\033[0m", errMsg)
			}
			fmt.Fprintln(c.output, errMsg)
		} else if c.verbose {
			if c.useColor {
				errMsg = fmt.Sprintf("\033[33m%s\033[0m", errMsg)
			}
			fmt.Fprintln(c.output, errMsg)
		}

		record.LastDisplayTime = time.Now()
	}
}

// OnComplete 实现ProgressCallback接口
func (c *ConsoleProgressCallback) OnComplete(transferID string, totalBytes int64, totalTime time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 获取传输记录
	record, exists := c.transfers[transferID]
	if !exists {
		return
	}

	// 如果已经显示过完成消息，则不再显示
	if record.CompletionMessageShown {
		return
	}

	// 更新记录
	record.Total = totalBytes
	record.Transferred = totalBytes
	record.Percentage = 100
	record.Status = StatusCompleted
	record.CompletionMessageShown = true

	// 显示完成消息
	if record.Visible {
		// 清除上一行进度条
		if record.LastProgressBar != "" {
			c.clearLine()
		}

		// 计算平均速度
		avgSpeed := float64(totalBytes) / totalTime.Seconds()
		speedStr := formatSpeed(avgSpeed)

		// 构造完成消息
		completeMsg := fmt.Sprintf("%s: 完成 - 总大小: %s, 耗时: %s, 平均速度: %s",
			getDisplayName(record),
			formatSize(totalBytes),
			formatDuration(totalTime),
			speedStr)

		// 如果启用了颜色，则使用绿色显示完成消息
		if c.useColor {
			completeMsg = fmt.Sprintf("\033[32m%s\033[0m", completeMsg)
		}

		fmt.Fprintln(c.output, completeMsg)
		record.LastDisplayTime = time.Now()
	}
}

// displayProgress 显示进度条
func (c *ConsoleProgressCallback) displayProgress(record *consoleTransferRecord) {
	// 如果有上一行进度条，则清除
	if record.LastProgressBar != "" {
		c.clearLine()
	}

	// 构造进度条
	var progressBar string
	if record.Total > 0 {
		// 计算完成的部分长度
		completed := int(float64(c.width) * record.Percentage / 100.0)

		// 构造进度条字符串
		progress := strings.Repeat(c.progressChar, completed)
		empty := strings.Repeat(c.emptyChar, c.width-completed)

		progressBar = fmt.Sprintf("[%s%s]", progress, empty)
	} else {
		// 如果总大小未知，显示旋转动画
		animChars := []string{"-", "\\", "|", "/"}
		animIdx := int(time.Now().UnixNano()/int64(250*time.Millisecond)) % len(animChars)
		progressBar = fmt.Sprintf("[%s]", animChars[animIdx])
	}

	// 构造状态行
	displayName := getDisplayName(record)
	statusLine := fmt.Sprintf("%s: %s %.1f%%", displayName, progressBar, record.Percentage)

	// 添加传输大小信息
	sizeInfo := fmt.Sprintf(" %s/%s",
		formatSize(record.Transferred),
		formatSize(record.Total))
	statusLine += sizeInfo

	// 添加速度信息
	if c.showSpeed && record.Speed > 0 {
		speedInfo := fmt.Sprintf(" @ %s", formatSpeed(record.Speed))
		statusLine += speedInfo
	}

	// 添加ETA信息
	if c.showETA && record.ETA > 0 {
		etaInfo := fmt.Sprintf(" - %s剩余", formatDuration(record.ETA))
		statusLine += etaInfo
	}

	// 根据状态添加颜色
	if c.useColor {
		switch record.Status {
		case StatusTransferring:
			statusLine = fmt.Sprintf("\033[34m%s\033[0m", statusLine) // 蓝色
		case StatusPaused:
			statusLine = fmt.Sprintf("\033[33m%s\033[0m", statusLine) // 黄色
		case StatusCompleted:
			statusLine = fmt.Sprintf("\033[32m%s\033[0m", statusLine) // 绿色
		case StatusFailed, StatusCancelled:
			statusLine = fmt.Sprintf("\033[31m%s\033[0m", statusLine) // 红色
		}
	}

	// 输出状态行
	fmt.Fprint(c.output, statusLine)
	record.LastProgressBar = statusLine
}

// displayStatusChange 显示状态变化
func (c *ConsoleProgressCallback) displayStatusChange(record *consoleTransferRecord, oldStatus, newStatus TransferStatus) {
	// 如果有上一行进度条，则清除
	if record.LastProgressBar != "" {
		c.clearLine()
	}

	// 只在特定状态变化时显示消息
	var statusMsg string
	switch newStatus {
	case StatusPaused:
		statusMsg = fmt.Sprintf("%s: 已暂停", getDisplayName(record))
		if c.useColor {
			statusMsg = fmt.Sprintf("\033[33m%s\033[0m", statusMsg) // 黄色
		}
	case StatusResuming:
		statusMsg = fmt.Sprintf("%s: 正在恢复", getDisplayName(record))
		if c.useColor {
			statusMsg = fmt.Sprintf("\033[34m%s\033[0m", statusMsg) // 蓝色
		}
	case StatusCancelled:
		statusMsg = fmt.Sprintf("%s: 已取消", getDisplayName(record))
		if c.useColor {
			statusMsg = fmt.Sprintf("\033[31m%s\033[0m", statusMsg) // 红色
		}
	case StatusFailed:
		statusMsg = fmt.Sprintf("%s: 失败", getDisplayName(record))
		if c.useColor {
			statusMsg = fmt.Sprintf("\033[31m%s\033[0m", statusMsg) // 红色
		}
	}

	// 如果有状态消息，则显示
	if statusMsg != "" {
		fmt.Fprintln(c.output, statusMsg)
		record.LastProgressBar = ""
	} else {
		// 否则更新进度条显示
		c.displayProgress(record)
	}
}

// clearLine 清除当前行
func (c *ConsoleProgressCallback) clearLine() {
	// 回到行首
	fmt.Fprint(c.output, "\r")

	// 清除整行
	fmt.Fprint(c.output, "\033[K")
}

// getDisplayName 获取显示名称
func getDisplayName(record *consoleTransferRecord) string {
	if record.DisplayName != "" {
		return record.DisplayName
	}
	return record.ID
}

// formatSize 格式化文件大小
func formatSize(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	} else if bytes < 1024*1024 {
		return fmt.Sprintf("%.2f KB", float64(bytes)/1024)
	} else if bytes < 1024*1024*1024 {
		return fmt.Sprintf("%.2f MB", float64(bytes)/(1024*1024))
	} else {
		return fmt.Sprintf("%.2f GB", float64(bytes)/(1024*1024*1024))
	}
}

// formatSpeed 格式化传输速度
func formatSpeed(bytesPerSecond float64) string {
	if bytesPerSecond < 1024 {
		return fmt.Sprintf("%.0f B/s", bytesPerSecond)
	} else if bytesPerSecond < 1024*1024 {
		return fmt.Sprintf("%.2f KB/s", bytesPerSecond/1024)
	} else if bytesPerSecond < 1024*1024*1024 {
		return fmt.Sprintf("%.2f MB/s", bytesPerSecond/(1024*1024))
	} else {
		return fmt.Sprintf("%.2f GB/s", bytesPerSecond/(1024*1024*1024))
	}
}

// formatDuration 格式化持续时间
func formatDuration(d time.Duration) string {
	// 对于小于1秒的时间，显示毫秒
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}

	// 对于小于1分钟的时间，显示秒
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}

	// 计算小时、分钟和秒
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	// 根据时间长度选择合适的格式
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	} else {
		return fmt.Sprintf("%d:%02d", minutes, seconds)
	}
}
