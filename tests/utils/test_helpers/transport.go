package test_helpers

import (
	"errors"
	"io"
	"net"
)

// CreateMemoryConnPair 创建一对内存连接
func CreateMemoryConnPair() (io.ReadWriteCloser, io.ReadWriteCloser) {
	return net.Pipe()
}

// CreateConnectedPair 创建一对已连接的连接
func CreateConnectedPair() (io.ReadWriteCloser, io.ReadWriteCloser) {
	return net.Pipe()
}

// TestMessageTransporter 用于测试的消息传输器
type TestMessageTransporter struct {
	conn io.ReadWriteCloser
}

// NewTestMessageTransporter 创建新的测试消息传输器
func NewTestMessageTransporter(conn io.ReadWriteCloser) *TestMessageTransporter {
	return &TestMessageTransporter{
		conn: conn,
	}
}

// Send 发送消息
func (t *TestMessageTransporter) Send(data []byte) error {
	// 发送消息长度
	header := make([]byte, 4)
	EncodeLength(header, uint32(len(data)))
	if _, err := t.conn.Write(header); err != nil {
		return err
	}

	// 发送消息内容
	if _, err := t.conn.Write(data); err != nil {
		return err
	}

	return nil
}

// Receive 接收消息
func (t *TestMessageTransporter) Receive() ([]byte, error) {
	// 读取消息长度
	header := make([]byte, 4)
	if _, err := io.ReadFull(t.conn, header); err != nil {
		return nil, err
	}

	// 解码消息长度
	length := DecodeLength(header)
	if length > 1024*1024*10 { // 10MB限制
		return nil, errors.New("消息太大")
	}

	// 读取消息内容
	data := make([]byte, length)
	if _, err := io.ReadFull(t.conn, data); err != nil {
		return nil, err
	}

	return data, nil
}

// EncodeLength 将32位长度编码为4字节
func EncodeLength(buf []byte, length uint32) {
	buf[0] = byte(length >> 24)
	buf[1] = byte(length >> 16)
	buf[2] = byte(length >> 8)
	buf[3] = byte(length)
}

// DecodeLength 从4字节解码32位长度
func DecodeLength(buf []byte) uint32 {
	return uint32(buf[0])<<24 | uint32(buf[1])<<16 | uint32(buf[2])<<8 | uint32(buf[3])
}

// GenerateRandomData 生成指定大小的随机数据
func GenerateRandomData(size int) []byte {
	return CreateRandomTestData(size)
}

// IsTimeout 检查错误是否为超时错误
func IsTimeout(err error) bool {
	if err == nil {
		return false
	}
	netErr, ok := err.(net.Error)
	return ok && netErr.Timeout()
}

// SendStream 发送流式数据
func (t *TestMessageTransporter) SendStream(reader io.Reader) error {
	// 读取所有数据
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}

	// 发送数据
	return t.Send(data)
}

// ReceiveStream 接收流式数据
func (t *TestMessageTransporter) ReceiveStream(writer io.Writer) error {
	// 接收数据
	data, err := t.Receive()
	if err != nil {
		return err
	}

	// 写入数据
	_, err = writer.Write(data)
	return err
}
