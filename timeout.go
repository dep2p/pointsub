package pointsub

import (
	"net"
	"time"
)

// SetReadTimeoutWithReset 设置读取超时并提供自动重置功能
func SetReadTimeoutWithReset(conn net.Conn, timeout time.Duration, autoReset bool) error {
	err := conn.SetReadDeadline(time.Now().Add(timeout))
	if err != nil {
		logger.Errorf("设置读取超时失败: %v", err)
		return err
	}

	if autoReset {
		// 创建一个定时器在超时后自动重置
		time.AfterFunc(timeout+time.Millisecond*100, func() {
			conn.SetReadDeadline(time.Time{})
		})
	}

	return nil
}

// SetWriteTimeoutWithReset 设置写入超时并提供自动重置功能
func SetWriteTimeoutWithReset(conn net.Conn, timeout time.Duration, autoReset bool) error {
	err := conn.SetWriteDeadline(time.Now().Add(timeout))
	if err != nil {
		logger.Errorf("设置写入超时失败: %v", err)
		return err
	}

	if autoReset {
		// 创建一个定时器在超时后自动重置
		time.AfterFunc(timeout+time.Millisecond*100, func() {
			conn.SetWriteDeadline(time.Time{})
		})
	}

	return nil
}

// NewTimeoutConn 创建一个带有自动超时管理的连接包装器
func NewTimeoutConn(conn net.Conn, readTimeout, writeTimeout time.Duration) net.Conn {
	return &timeoutConn{
		Conn:         conn,
		readTimeout:  readTimeout,
		writeTimeout: writeTimeout,
	}
}

// timeoutConn 实现带有自动超时控制的连接
type timeoutConn struct {
	net.Conn
	readTimeout  time.Duration
	writeTimeout time.Duration
}

// Read 实现自动超时管理的读取
func (t *timeoutConn) Read(b []byte) (n int, err error) {
	if t.readTimeout > 0 {
		t.Conn.SetReadDeadline(time.Now().Add(t.readTimeout))
		defer t.Conn.SetReadDeadline(time.Time{})
	}
	return t.Conn.Read(b)
}

// Write 实现自动超时管理的写入
func (t *timeoutConn) Write(b []byte) (n int, err error) {
	if t.writeTimeout > 0 {
		t.Conn.SetWriteDeadline(time.Now().Add(t.writeTimeout))
		defer t.Conn.SetWriteDeadline(time.Time{})
	}
	return t.Conn.Write(b)
}
