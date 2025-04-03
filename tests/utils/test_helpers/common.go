package test_helpers

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"net"
	"strconv"
	"testing"
	"time"
)

// CreateTestData 创建指定大小的测试数据
func CreateTestData(size int) []byte {
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 256)
	}
	return data
}

// CreateRandomTestData 创建指定大小的随机测试数据
func CreateRandomTestData(size int) []byte {
	data := make([]byte, size)
	rand.Read(data)
	return data
}

// FormatSizeStr 将字节大小格式化为人类可读的字符串
func FormatSizeStr(bytes int) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// CreateMemoryPipe 创建内存管道用于测试
func CreateMemoryPipe() (io.ReadWriteCloser, io.ReadWriteCloser) {
	return net.Pipe()
}

// AssertEquals 断言预期值和实际值相等
func AssertEquals(t *testing.T, expected, actual interface{}, msgAndArgs ...interface{}) {
	t.Helper()
	if fmt.Sprintf("%v", expected) != fmt.Sprintf("%v", actual) {
		if len(msgAndArgs) > 0 {
			t.Errorf("预期: %v, 实际: %v - %s", expected, actual, fmt.Sprint(msgAndArgs...))
		} else {
			t.Errorf("预期: %v, 实际: %v", expected, actual)
		}
	}
}

// AssertTrue 断言条件为真
func AssertTrue(t *testing.T, condition bool, msgAndArgs ...interface{}) {
	t.Helper()
	if !condition {
		if len(msgAndArgs) > 0 {
			t.Errorf("条件应为真 - %s", fmt.Sprint(msgAndArgs...))
		} else {
			t.Errorf("条件应为真")
		}
	}
}

// AssertFalse 断言条件为假
func AssertFalse(t *testing.T, condition bool, msgAndArgs ...interface{}) {
	t.Helper()
	if condition {
		if len(msgAndArgs) > 0 {
			t.Errorf("条件应为假 - %s", fmt.Sprint(msgAndArgs...))
		} else {
			t.Errorf("条件应为假")
		}
	}
}

// AssertNil 断言值为nil
func AssertNil(t *testing.T, object interface{}, msgAndArgs ...interface{}) {
	t.Helper()
	if object != nil {
		if len(msgAndArgs) > 0 {
			t.Errorf("预期为nil，实际: %v - %s", object, fmt.Sprint(msgAndArgs...))
		} else {
			t.Errorf("预期为nil，实际: %v", object)
		}
	}
}

// AssertNotNil 断言值不为nil
func AssertNotNil(t *testing.T, object interface{}, msgAndArgs ...interface{}) {
	t.Helper()
	if object == nil {
		if len(msgAndArgs) > 0 {
			t.Errorf("不应为nil - %s", fmt.Sprint(msgAndArgs...))
		} else {
			t.Errorf("不应为nil")
		}
	}
}

// CompareData 比较两个数据切片是否相等
func CompareData(a, b []byte) bool {
	return bytes.Equal(a, b)
}

// GenerateUniqueTestName 生成唯一的测试名称
func GenerateUniqueTestName(prefix string) string {
	return fmt.Sprintf("%s-%d-%s",
		prefix,
		time.Now().UnixNano(),
		strconv.FormatInt(rand.Int63(), 36))
}

// WaitForCondition 等待条件函数返回true，或者超时
func WaitForCondition(condition func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// MeasureTime 测量函数执行时间
func MeasureTime(f func()) time.Duration {
	start := time.Now()
	f()
	return time.Since(start)
}

// Retry 重试函数直到成功或者达到最大尝试次数
func Retry(f func() error, maxAttempts int, delay time.Duration) error {
	var err error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err = f()
		if err == nil {
			return nil
		}
		if attempt < maxAttempts {
			time.Sleep(delay)
		}
	}
	return fmt.Errorf("在%d次尝试后失败: %w", maxAttempts, err)
}
