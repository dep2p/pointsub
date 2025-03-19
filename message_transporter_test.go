package pointsub

import (
	"encoding/binary"
	"io"
	"net"
<<<<<<< HEAD
	"time"
=======
>>>>>>> 6613f0351ad580eb6dda4edd3f91c53cbf4b91a9
)

// 简化版的消息传输器，专门用于测试
type testMessageTransporter struct {
	conn net.Conn
}

// NewTestMessageTransporter 创建一个用于测试的简化版消息传输器
func NewTestMessageTransporter(conn net.Conn) MessageTransporter {
	return &testMessageTransporter{
		conn: conn,
	}
}

// Send 发送消息
func (t *testMessageTransporter) Send(data []byte) error {
	// 写入消息长度（4字节）
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(data)))

	_, err := t.conn.Write(lenBuf)
	if err != nil {
		return &RetryableError{Err: err}
	}

	// 即使是空消息，也要确保写入操作完成
	if len(data) > 0 {
		_, err = t.conn.Write(data)
		if err != nil {
			return &RetryableError{Err: err}
		}
	}

	return nil
}

// Receive 接收消息
func (t *testMessageTransporter) Receive() ([]byte, error) {
	// 读取消息长度（4字节）
	lenBuf := make([]byte, 4)
	_, err := io.ReadFull(t.conn, lenBuf)
	if err != nil {
		return nil, &RetryableError{Err: err}
	}

	// 解析消息长度
	msgLen := binary.BigEndian.Uint32(lenBuf)

	// 如果消息长度为0，直接返回空切片
	if msgLen == 0 {
		return []byte{}, nil
	}

	// 读取消息内容
	data := make([]byte, msgLen)
	_, err = io.ReadFull(t.conn, data)
	if err != nil {
		return nil, &RetryableError{Err: err}
	}

	return data, nil
}

// SendStream 发送流式数据
func (t *testMessageTransporter) SendStream(reader io.Reader) error {
	// 简化实现，将所有数据读入内存后发送
	data, err := io.ReadAll(reader)
	if err != nil {
		return &RetryableError{Err: err}
	}

	return t.Send(data)
}

// ReceiveStream 接收流式数据
func (t *testMessageTransporter) ReceiveStream(writer io.Writer) error {
	// 简化实现，接收所有数据后写入
	data, err := t.Receive()
	if err != nil {
		return err
	}

	_, err = writer.Write(data)
	if err != nil {
		return &RetryableError{Err: err}
	}

	return nil
}

// Close 关闭连接
func (t *testMessageTransporter) Close() error {
	return t.conn.Close()
}
<<<<<<< HEAD

// SetDeadline 设置读写超时
func (t *testMessageTransporter) SetDeadline(deadline time.Time) error {
	return t.conn.SetDeadline(deadline)
}

// SetReadDeadline 设置读取超时
func (t *testMessageTransporter) SetReadDeadline(deadline time.Time) error {
	return t.conn.SetReadDeadline(deadline)
}

// SetWriteDeadline 设置写入超时
func (t *testMessageTransporter) SetWriteDeadline(deadline time.Time) error {
	return t.conn.SetWriteDeadline(deadline)
}
=======
>>>>>>> 6613f0351ad580eb6dda4edd3f91c53cbf4b91a9
