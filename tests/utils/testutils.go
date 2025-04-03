package utils

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// GenerateRandomBytes 生成指定大小的随机字节数组
func GenerateRandomBytes(size int) ([]byte, error) {
	data := make([]byte, size)
	_, err := rand.Read(data)
	if err != nil {
		return nil, fmt.Errorf("生成随机数据失败: %v", err)
	}
	return data, nil
}

// CompareBytes 比较两个字节数组是否相等，并返回详细的不匹配信息
func CompareBytes(expected, actual []byte) (bool, string) {
	if len(expected) != len(actual) {
		return false, fmt.Sprintf("数据长度不匹配: 预期 %d, 实际 %d", len(expected), len(actual))
	}

	if bytes.Equal(expected, actual) {
		return true, ""
	}

	// 寻找第一个不匹配的位置
	pos := 0
	for i := 0; i < len(expected); i++ {
		if expected[i] != actual[i] {
			pos = i
			break
		}
	}

	// 计算不匹配位置前后的上下文
	contextSize := 16
	start := pos - contextSize
	if start < 0 {
		start = 0
	}

	end := pos + contextSize
	if end > len(expected) {
		end = len(expected)
	}

	return false, fmt.Sprintf("数据不匹配，位置: %d\n预期: % x\n实际: % x",
		pos, expected[start:end], actual[start:end])
}

// WithTimeout 使用超时执行函数
func WithTimeout(timeout time.Duration, fn func()) bool {
	done := make(chan struct{})

	go func() {
		fn()
		close(done)
	}()

	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// GetTestDataPath 返回测试数据文件的完整路径
func GetTestDataPath(filename string) string {
	_, currentFile, _, _ := runtime.Caller(0)
	testDataDir := filepath.Join(filepath.Dir(currentFile), "..", "fixtures")
	return filepath.Join(testDataDir, filename)
}

// CreateTempFile 创建包含指定数据的临时文件
func CreateTempFile(prefix string, data []byte) (string, error) {
	f, err := os.CreateTemp("", prefix)
	if err != nil {
		return "", err
	}

	defer f.Close()

	if _, err := f.Write(data); err != nil {
		os.Remove(f.Name())
		return "", err
	}

	return f.Name(), nil
}

// RemoveTempFile 安全删除临时文件
func RemoveTempFile(path string) error {
	return os.Remove(path)
}

// ByteCountToString 将字节数转换为人类可读的字符串
func ByteCountToString(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}

	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// HumanDuration 将持续时间转换为人类可读的字符串
func HumanDuration(d time.Duration) string {
	if d < time.Microsecond {
		return fmt.Sprintf("%d ns", d.Nanoseconds())
	}

	if d < time.Millisecond {
		return fmt.Sprintf("%.2f µs", float64(d.Nanoseconds())/1000)
	}

	if d < time.Second {
		return fmt.Sprintf("%.2f ms", float64(d.Nanoseconds())/1000000)
	}

	if d < time.Minute {
		return fmt.Sprintf("%.2f s", d.Seconds())
	}

	if d < time.Hour {
		return fmt.Sprintf("%.2f m", d.Minutes())
	}

	return fmt.Sprintf("%.2f h", d.Hours())
}

// CalcPercentage 计算百分比，处理分母为零的情况
func CalcPercentage(numerator, denominator int64) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator) * 100
}

// BenchmarkTimer 简单的基准测试计时器
type BenchmarkTimer struct {
	StartTime time.Time
	Duration  time.Duration
	Running   bool
}

// NewBenchmarkTimer 创建新的基准测试计时器
func NewBenchmarkTimer() *BenchmarkTimer {
	return &BenchmarkTimer{}
}

// Start 开始计时
func (t *BenchmarkTimer) Start() {
	t.StartTime = time.Now()
	t.Running = true
}

// Stop 停止计时
func (t *BenchmarkTimer) Stop() time.Duration {
	if !t.Running {
		return t.Duration
	}

	t.Duration = time.Since(t.StartTime)
	t.Running = false
	return t.Duration
}

// Reset 重置计时器
func (t *BenchmarkTimer) Reset() {
	t.Duration = 0
	t.Running = false
}

// GetDuration 获取持续时间
func (t *BenchmarkTimer) GetDuration() time.Duration {
	if t.Running {
		return time.Since(t.StartTime)
	}
	return t.Duration
}

// String 返回易读的持续时间
func (t *BenchmarkTimer) String() string {
	return HumanDuration(t.GetDuration())
}
