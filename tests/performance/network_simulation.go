package performance

import (
	"io"
	"math/rand"
	"net"
	"sync"
	"time"
)

// NetworkCondition 定义网络测试条件
type NetworkCondition struct {
	Name           string
	LatencyMin     time.Duration // 最小延迟
	LatencyMax     time.Duration // 最大延迟
	Jitter         time.Duration // 延迟抖动
	PacketLossRate float64       // 丢包率 (0-1)
	CorruptionRate float64       // 数据损坏率 (0-1)
	Bandwidth      int64         // 带宽限制 (字节/秒)
}

// 预定义的网络条件
var (
	// 本地网络 - 高速、低延迟
	LocalNetwork = NetworkCondition{
		Name:           "local",
		LatencyMin:     1 * time.Millisecond,
		LatencyMax:     5 * time.Millisecond,
		Jitter:         1 * time.Millisecond,
		PacketLossRate: 0,
		CorruptionRate: 0,
		Bandwidth:      1024 * 1024 * 100, // 100 MB/s
	}

	// 低延迟WAN - 高速互联网
	LowLatencyWAN = NetworkCondition{
		Name:           "low-latency-wan",
		LatencyMin:     10 * time.Millisecond,
		LatencyMax:     50 * time.Millisecond,
		Jitter:         10 * time.Millisecond,
		PacketLossRate: 0.001,
		CorruptionRate: 0.0001,
		Bandwidth:      1024 * 1024 * 10, // 10 MB/s
	}

	// 高延迟WAN - 卫星或跨洲际
	HighLatencyWAN = NetworkCondition{
		Name:           "high-latency-wan",
		LatencyMin:     200 * time.Millisecond,
		LatencyMax:     500 * time.Millisecond,
		Jitter:         50 * time.Millisecond,
		PacketLossRate: 0.01,
		CorruptionRate: 0.001,
		Bandwidth:      1024 * 1024 * 1, // 1 MB/s
	}

	// 不稳定网络 - 丢包率高，连接不稳定
	UnstableNetwork = NetworkCondition{
		Name:           "unstable",
		LatencyMin:     50 * time.Millisecond,
		LatencyMax:     300 * time.Millisecond,
		Jitter:         100 * time.Millisecond,
		PacketLossRate: 0.05,
		CorruptionRate: 0.01,
		Bandwidth:      1024 * 512, // 512 KB/s
	}

	// 极端条件 - 非常糟糕的网络
	ExtremeCondition = NetworkCondition{
		Name:           "extreme",
		LatencyMin:     300 * time.Millisecond,
		LatencyMax:     1000 * time.Millisecond,
		Jitter:         200 * time.Millisecond,
		PacketLossRate: 0.2,
		CorruptionRate: 0.05,
		Bandwidth:      1024 * 128, // 128 KB/s
	}
)

// GetNetworkCondition 根据名称获取预定义的网络条件
func GetNetworkCondition(name string) NetworkCondition {
	switch name {
	case "local":
		return LocalNetwork
	case "low-latency-wan":
		return LowLatencyWAN
	case "high-latency-wan":
		return HighLatencyWAN
	case "unstable":
		return UnstableNetwork
	case "extreme":
		return ExtremeCondition
	case "simulated-delay":
		return NetworkCondition{
			Name:           "simulated-delay",
			LatencyMin:     50 * time.Millisecond,
			LatencyMax:     150 * time.Millisecond,
			Jitter:         30 * time.Millisecond,
			PacketLossRate: 0,
			CorruptionRate: 0,
			Bandwidth:      1024 * 1024 * 10, // 10 MB/s
		}
	case "simulated-loss":
		return NetworkCondition{
			Name:           "simulated-loss",
			LatencyMin:     10 * time.Millisecond,
			LatencyMax:     50 * time.Millisecond,
			Jitter:         10 * time.Millisecond,
			PacketLossRate: 0.05,
			CorruptionRate: 0.01,
			Bandwidth:      1024 * 1024 * 5, // 5 MB/s
		}
	default:
		return LocalNetwork
	}
}

// SimulatedConn 实现模拟网络条件的连接
type SimulatedConn struct {
	net.Conn
	condition     NetworkCondition
	writeLock     sync.Mutex
	readLock      sync.Mutex
	lastReadTime  time.Time
	lastWriteTime time.Time
}

// NewSimulatedConn 创建一个模拟网络条件的连接
func NewSimulatedConn(conn net.Conn, condition NetworkCondition) *SimulatedConn {
	return &SimulatedConn{
		Conn:          conn,
		condition:     condition,
		lastReadTime:  time.Now(),
		lastWriteTime: time.Now(),
	}
}

// Read 模拟读取延迟和丢包
func (s *SimulatedConn) Read(b []byte) (n int, err error) {
	s.readLock.Lock()
	defer s.readLock.Unlock()

	// 模拟带宽限制
	now := time.Now()
	timeSinceLastRead := now.Sub(s.lastReadTime)
	s.lastReadTime = now

	// 计算基于延迟的读取等待
	latency := calculateLatency(s.condition)
	time.Sleep(latency)

	// 模拟丢包
	if rand.Float64() < s.condition.PacketLossRate {
		return 0, io.ErrUnexpectedEOF
	}

	// 读取实际数据
	n, err = s.Conn.Read(b)
	if err != nil {
		return n, err
	}

	// 如果有带宽限制，计算应该等待多久才能完成读取
	if s.condition.Bandwidth > 0 {
		expectedTime := time.Duration(float64(n) / float64(s.condition.Bandwidth) * float64(time.Second))
		if expectedTime > timeSinceLastRead+latency {
			time.Sleep(expectedTime - timeSinceLastRead - latency)
		}
	}

	// 模拟数据损坏
	if rand.Float64() < s.condition.CorruptionRate && n > 0 {
		idx := rand.Intn(n)
		b[idx] = byte(rand.Intn(256))
	}

	return n, err
}

// Write 模拟写入延迟和带宽限制
func (s *SimulatedConn) Write(b []byte) (n int, err error) {
	s.writeLock.Lock()
	defer s.writeLock.Unlock()

	// 模拟带宽限制
	now := time.Now()
	timeSinceLastWrite := now.Sub(s.lastWriteTime)
	s.lastWriteTime = now

	// 计算基于延迟的写入等待
	latency := calculateLatency(s.condition)
	time.Sleep(latency)

	// 模拟丢包
	if rand.Float64() < s.condition.PacketLossRate {
		return len(b), nil // 假装写入成功但实际丢失
	}

	// 写入实际数据
	n, err = s.Conn.Write(b)

	// 如果有带宽限制，计算应该等待多久才能完成写入
	if s.condition.Bandwidth > 0 {
		expectedTime := time.Duration(float64(n) / float64(s.condition.Bandwidth) * float64(time.Second))
		if expectedTime > timeSinceLastWrite+latency {
			time.Sleep(expectedTime - timeSinceLastWrite - latency)
		}
	}

	return n, err
}

// 计算随机延迟
func calculateLatency(condition NetworkCondition) time.Duration {
	baseLatency := condition.LatencyMin
	if condition.LatencyMax > condition.LatencyMin {
		jitterRange := condition.LatencyMax - condition.LatencyMin
		baseLatency += time.Duration(rand.Int63n(int64(jitterRange)))
	}

	// 添加抖动
	if condition.Jitter > 0 {
		jitter := time.Duration(rand.Int63n(int64(condition.Jitter)*2)) - condition.Jitter
		baseLatency += jitter
		if baseLatency < 0 {
			baseLatency = 0
		}
	}

	return baseLatency
}

// SimulatedListener 用于创建模拟网络条件的连接的监听器
type SimulatedListener struct {
	net.Listener
	condition NetworkCondition
}

// NewSimulatedListener 创建一个模拟网络条件的监听器
func NewSimulatedListener(listener net.Listener, condition NetworkCondition) *SimulatedListener {
	return &SimulatedListener{
		Listener:  listener,
		condition: condition,
	}
}

// Accept 重写接受连接方法，返回模拟网络条件的连接
func (s *SimulatedListener) Accept() (net.Conn, error) {
	conn, err := s.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return NewSimulatedConn(conn, s.condition), nil
}

// Dial 模拟网络条件下的拨号
func SimulatedDial(network, address string, condition NetworkCondition) (net.Conn, error) {
	conn, err := net.Dial(network, address)
	if err != nil {
		return nil, err
	}
	return NewSimulatedConn(conn, condition), nil
}

// 初始化随机数生成器
func init() {
	rand.Seed(time.Now().UnixNano())
}
