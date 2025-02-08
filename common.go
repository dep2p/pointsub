package pointsub

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
)

// LimitedConn 包装了一个带有大小限制的连接
type LimitedConn struct {
	net.Conn     // 底层连接
	maxSize  int // 最大大小限制
}

// NewLimitedConn 创建一个新的限制大小的连接
// 参数:
//   - conn: 原始连接
//   - maxSize: 最大大小限制
//
// 返回值:
//   - net.Conn: 限制大小的连接
func NewLimitedConn(conn net.Conn, maxSize int) net.Conn {
	return &LimitedConn{
		Conn:    conn,
		maxSize: maxSize,
	}
}

// Read 读取数据
// 参数:
//   - b: 读取缓冲区
//
// 返回值:
//   - n: 读取的字节数
//   - err: 错误信息
func (c *LimitedConn) Read(b []byte) (n int, err error) {
	if len(b) > c.maxSize {
		b = b[:c.maxSize]
	}
	return c.Conn.Read(b)
}

// Write 写入数据
// 参数:
//   - b: 要写入的数据
//
// 返回值:
//   - n: 写入的字节数
//   - err: 错误信息
func (c *LimitedConn) Write(b []byte) (n int, err error) {
	if len(b) > c.maxSize {
		return 0, fmt.Errorf("数据大小超过限制: %d > %d", len(b), c.maxSize)
	}
	return c.Conn.Write(b)
}

// isRetryableError 判断错误是否可重试
// 参数:
//   - err: 要检查的错误
//
// 返回值:
//   - bool: 是否可以重试
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	if netErr, ok := err.(net.Error); ok {
		return netErr.Temporary() || netErr.Timeout()
	}

	errStr := err.Error()
	return strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "connection refused")
}

// isExpectedError 判断是否为预期的错误
// 参数:
//   - err: 要检查的错误
//
// 返回值:
//   - bool: 是否为预期错误
func isExpectedError(err error) bool {
	if err == nil {
		return true
	}
	if ne, ok := err.(net.Error); ok {
		return ne.Temporary() || ne.Timeout()
	}
	if errors.Is(err, io.EOF) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	errStr := err.Error()
	return strings.Contains(errStr, "use of closed network connection") ||
		strings.Contains(errStr, "connection reset by peer") ||
		strings.Contains(errStr, "broken pipe")
}
