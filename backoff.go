// 作用：实现对等节点的退避（backoff）机制，用于处理消息发布失败时的重试策略。
// 功能：管理对等节点的退避时间和逻辑，确保在消息发布失败后不会立即重试，而是等待一段时间再重试。

package pointsub

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/dep2p/go-dep2p/core/peer"
)

// 定义了一些常量，用于控制退避算法的参数
const (
	MinBackoffDelay        = 100 * time.Millisecond // 最小退避延迟时间
	MaxBackoffDelay        = 10 * time.Second       // 最大退避延迟时间
	TimeToLive             = 10 * time.Minute       // 退避信息的存活时间
	BackoffCleanupInterval = 1 * time.Minute        // 清理退避信息的间隔时间
	BackoffMultiplier      = 2                      // 退避时间的倍增因子
	MaxBackoffJitterCoff   = 100                    // 最大退避抖动系数
	MaxBackoffAttempts     = 50                     // 最大退避尝试次数
)

// backoffHistory 结构体记录了每个节点的退避历史
type backoffHistory struct {
	duration  time.Duration // 当前的退避持续时间
	lastTried time.Time     // 上次尝试连接的时间
	attempts  int           // 已经尝试连接的次数
}

// backoff 结构体管理退避信息
type backoff struct {
	mu          sync.Mutex                  // 互斥锁，用于保护共享数据
	info        map[peer.ID]*backoffHistory // 节点ID到退避历史的映射
	ct          int                         // 触发清理的大小阈值
	ci          time.Duration               // 清理间隔
	maxAttempts int                         // 最大退避尝试次数
}

// newBackoff 函数创建并初始化一个新的 backoff 实例
// 参数：
//   - ctx: 上下文，用于控制清理循环的生命周期
//   - sizeThreshold: 触发清理的大小阈值
//   - cleanupInterval: 清理间隔时间
//   - maxAttempts: 最大退避尝试次数
//
// 返回：
//   - *backoff: 返回初始化后的 backoff 实例
func newBackoff(ctx context.Context, sizeThreshold int, cleanupInterval time.Duration, maxAttempts int) *backoff {
	// 初始化 backoff 实例
	b := &backoff{
		mu:          sync.Mutex{},                      // 初始化互斥锁
		ct:          sizeThreshold,                     // 设置大小阈值
		ci:          cleanupInterval,                   // 设置清理间隔
		maxAttempts: maxAttempts,                       // 设置最大尝试次数
		info:        make(map[peer.ID]*backoffHistory), // 初始化信息映射
	}

	rand.Seed(time.Now().UnixNano()) // 设置随机种子，用于退避时间的抖动
	go b.cleanupLoop(ctx)            // 启动清理循环，运行在独立的goroutine中

	return b
}

// updateAndGet 方法更新并获取给定节点的退避时间
// 参数：
//   - id: 节点ID
//
// 返回：
//   - time.Duration: 返回下次尝试连接的退避时间
//   - error: 如果达到最大尝试次数，返回错误信息
func (b *backoff) updateAndGet(id peer.ID) (time.Duration, error) {
	b.mu.Lock()         // 加锁，保护共享数据
	defer b.mu.Unlock() // 方法结束时解锁

	h, ok := b.info[id] // 获取节点的退避历史
	switch {
	case !ok || time.Since(h.lastTried) > TimeToLive: // 如果没有记录或记录已过期
		// 首次请求或过期后第一次请求立即执行
		h = &backoffHistory{
			duration: time.Duration(0), // 初始退避时间为0
			attempts: 0,                // 初始尝试次数为0
		}
	case time.Since(h.lastTried) > MaxBackoffDelay:
		// 如果距离上次尝试时间超过最大退避时间，重置尝试次数
		h.attempts = 0
		h.duration = 0
	case h.attempts >= b.maxAttempts: // 如果已经达到最大尝试次数
		return 0, fmt.Errorf("peer %s has reached its maximum backoff attempts", id) // 返回错误

	case h.duration < MinBackoffDelay: // 如果当前退避时间小于最小退避延迟
		h.duration = MinBackoffDelay // 设置为最小退避延迟

	case h.duration < MaxBackoffDelay: // 如果当前退避时间小于最大退避延迟
		jitter := rand.Intn(MaxBackoffJitterCoff)                                              // 生成抖动时间
		h.duration = (BackoffMultiplier * h.duration) + time.Duration(jitter)*time.Millisecond // 计算新的退避时间
		if h.duration > MaxBackoffDelay || h.duration < 0 {                                    // 检查退避时间是否超出范围
			h.duration = MaxBackoffDelay // 超出范围则设置为最大退避延迟
		}
	}

	h.attempts += 1          // 增加尝试次数
	h.lastTried = time.Now() // 更新上次尝试时间
	b.info[id] = h           // 更新退避历史记录
	return h.duration, nil   // 返回退避时间
}

// cleanup 方法清理过期的退避信息
func (b *backoff) cleanup() {
	b.mu.Lock()         // 加锁，保护共享数据
	defer b.mu.Unlock() // 方法结束时解锁

	for id, h := range b.info { // 遍历所有退避记录
		if time.Since(h.lastTried) > TimeToLive { // 如果记录已过期
			delete(b.info, id) // 删除过期记录
		}
	}
}

// cleanupLoop 方法定期清理过期的退避信息
// 参数：
//   - ctx: 上下文，用于控制清理循环的生命周期
func (b *backoff) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(b.ci) // 创建ticker，根据清理间隔触发
	defer ticker.Stop()            // 方法结束时停止ticker

	for {
		select {
		case <-ctx.Done(): // 如果上下文取消
			return // 退出清理循环
		case <-ticker.C: // 每次ticker触发时
			b.cleanup() // 调用cleanup方法清理过期记录
		}
	}
}
