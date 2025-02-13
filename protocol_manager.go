// Package pointsub 提供了基于 dep2p 的点对点订阅功能的实现
package pointsub

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/dep2p/go-dep2p/core/peer"
	"github.com/dep2p/go-dep2p/core/protocol"
)

// SendResult 定义发送操作的结果
type SendResult struct {
	Response []byte  // 响应数据
	Target   peer.ID // 成功发送到的目标节点
}

// nodeFailureInfo 记录节点失败信息
type nodeFailureInfo struct {
	lastFailTime time.Time // 最后失败时间
	failCount    int       // 连续失败次数
	totalFails   int       // 总失败次数
}

// failedNodeCache 用于缓存失败的节点
type failedNodeCache struct {
	nodes map[peer.ID]*nodeFailureInfo
	mu    sync.RWMutex
}

// newFailedNodeCache 创建一个新的失败节点缓存
// 返回值:
//   - *failedNodeCache: 新的失败节点缓存
func newFailedNodeCache() *failedNodeCache {
	return &failedNodeCache{
		nodes: make(map[peer.ID]*nodeFailureInfo),
	}
}

// add 添加一个失败的节点
// 参数:
//   - id: 节点ID
func (c *failedNodeCache) add(id peer.ID) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if info, exists := c.nodes[id]; exists {
		info.lastFailTime = time.Now()
		info.failCount++
		info.totalFails++
	} else {
		c.nodes[id] = &nodeFailureInfo{
			lastFailTime: time.Now(),
			failCount:    1,
			totalFails:   1,
		}
	}
}

// resetFailCount 重置节点的连续失败计数
// 参数:
//   - id: 节点ID
func (c *failedNodeCache) resetFailCount(id peer.ID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if info, exists := c.nodes[id]; exists {
		info.failCount = 0
	}
}

// shouldRetry 判断是否应该重试该节点
// 参数:
//   - id: 节点ID
//
// 返回值:
//   - bool: 是否应该重试
func (c *failedNodeCache) shouldRetry(id peer.ID) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	info, exists := c.nodes[id]
	if !exists {
		return true
	}

	now := time.Now()
	timeSinceLastFail := now.Sub(info.lastFailTime)

	// 基于失败次数的指数退避重试策略
	backoffDuration := time.Duration(1<<uint(info.failCount)) * time.Second
	if backoffDuration > 15*time.Minute {
		backoffDuration = 15 * time.Minute
	}

	return timeSinceLastFail > backoffDuration
}

// cleanup 清理恢复正常的节点
func (c *failedNodeCache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for id, info := range c.nodes {
		// 如果节点超过15分钟没有失败，或者失败次数为0，则从失败缓存中移除
		if now.Sub(info.lastFailTime) > 15*time.Minute || info.failCount == 0 {
			delete(c.nodes, id)
		}
	}
}

// RetryConfig 定义重试相关的配置
type RetryConfig struct {
	MaxRetryInterval time.Duration // 最大重试间隔
	BaseRetryDelay   time.Duration // 基础重试延迟
	MaxConcurrent    int           // 最大并发请求数
	RequestTimeout   time.Duration // 单个请求超时时间
}

// DefaultRetryConfig 返回默认的重试配置
// 返回值:
//   - *RetryConfig: 默认的重试配置
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxRetryInterval: 15 * time.Minute,
		BaseRetryDelay:   time.Second,
		MaxConcurrent:    3,
		RequestTimeout:   5 * time.Second,
	}
}

// AddServerNode 向指定协议添加服务器节点
// 参数:
//   - protocolID: 协议标识符
//   - peerID: 服务器节点ID
//
// 返回值:
//   - error: 错误信息
func (c *Client) AddServerNode(protocolID protocol.ID, peerID peer.ID) error {
	// 检查是否是自身节点
	if peerID == c.host.ID() {
		logger.Error("不能添加自身作为服务器节点")
		return errors.New("不能添加自身作为服务器节点")
	}

	// 检查节点列表是否已存在
	nodes, exists := c.serverProtocol[protocolID]
	if !exists {
		nodes = make([]peer.ID, 0)
	}

	// 检查节点是否已存在
	for _, existingPeer := range nodes {
		if existingPeer == peerID {
			return nil // 节点已存在，直接返回
		}
	}

	// 添加新节点
	c.serverProtocol[protocolID] = append(nodes, peerID)
	logger.Debugf("为协议 %s 添加服务器节点: %s", protocolID, peerID)
	return nil
}

// RemoveServerNode 从指定协议中移除服务器节点
// 参数:
//   - protocolID: 协议标识符
//   - peerID: 要移除的服务器节点ID
//
// 返回值:
//   - error: 错误信息
func (c *Client) RemoveServerNode(protocolID protocol.ID, peerID peer.ID) error {
	nodes, exists := c.serverProtocol[protocolID]
	if !exists {
		return errors.New("协议不存在")
	}

	// 查找并移除节点
	for i, existingPeer := range nodes {
		if existingPeer == peerID {
			// 如果这是最后一个节点，直接从map中删除该协议
			if len(nodes) == 1 {
				delete(c.serverProtocol, protocolID)
				logger.Debugf("从协议 %s 移除最后一个服务器节点: %s，协议已删除", protocolID, peerID)
			} else {
				c.serverProtocol[protocolID] = append(nodes[:i], nodes[i+1:]...)
				logger.Debugf("从协议 %s 移除服务器节点: %s", protocolID, peerID)
			}
			return nil
		}
	}

	return errors.New("节点不存在")
}

// SendClosest 将请求路由到最近的服务器节点
// 参数:
//   - ctx: 上下文对象
//   - protocolID: 协议标识符
//   - request: 请求数据
//   - excludeNodes: 要排除的节点列表（可选，用于避开之前失败的节点）
//
// 返回值:
//   - *SendResult: 发送结果，包含响应数据和目标节点
//   - error: 错误信息
func (c *Client) SendClosest(ctx context.Context, protocolID protocol.ID, request []byte, excludeNodes ...peer.ID) (*SendResult, error) {
	startTime := time.Now()
	defer func() {
		logger.Infof("SendClosest 总耗时: %v", time.Since(startTime))
	}()

	// 检查消息大小
	if len(request) > c.config.MaxBlockSize {
		logger.Errorf("消息大小超过限制: %d > %d", len(request), c.config.MaxBlockSize)
		return nil, ErrMessageTooLarge
	}

	nodes, exists := c.serverProtocol[protocolID]
	if !exists || len(nodes) == 0 {
		logger.Error("没有可用的服务器节点")
		return nil, errors.New("没有可用的服务器节点")
	}

	// 清理恢复的节点
	c.failedNodes.cleanup()

	// 对节点进行分类
	var availableNodes, retryNodes []peer.ID
	for _, node := range nodes {
		excluded := false
		for _, excludeNode := range excludeNodes {
			if node == excludeNode {
				excluded = true
				break
			}
		}
		if !excluded {
			if c.failedNodes.shouldRetry(node) {
				availableNodes = append(availableNodes, node)
			} else {
				retryNodes = append(retryNodes, node)
			}
		}
	}

	// 如果没有可用节点，尝试使用重试节点
	if len(availableNodes) == 0 {
		availableNodes = retryNodes
		logger.Info("没有可用节点，尝试使用重试节点列表")
	}

	if len(availableNodes) == 0 {
		return nil, errors.New("没有可用的节点")
	}

	// 按距离排序所有节点
	sortedNodes, err := SortNodesByDistance(request, availableNodes)
	if err != nil {
		logger.Errorf("节点排序失败: %v", err)
		return nil, fmt.Errorf("节点排序失败: %w", err)
	}

	// 动态调整并发数
	maxConcurrent := 3
	if len(sortedNodes) < maxConcurrent {
		maxConcurrent = len(sortedNodes)
	}

	// 创建一个取消函数组
	var cancels []context.CancelFunc
	defer func() {
		for _, cancel := range cancels {
			cancel()
		}
	}()

	// 在发送请求前记录开始时间
	sendStartTime := time.Now()

	type result struct {
		response *SendResult
		err      error
		node     peer.ID
	}

	resultChan := make(chan result, maxConcurrent)
	for i := 0; i < maxConcurrent; i++ {
		// 为每个请求创建独立的上下文
		reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		cancels = append(cancels, cancel)

		go func(node peer.ID) {
			response, err := c.Send(reqCtx, node, protocolID, request)
			if err != nil {
				c.failedNodes.add(node)
				resultChan <- result{nil, err, node}
				return
			}
			// 重置失败计数
			c.failedNodes.resetFailCount(node)
			resultChan <- result{
				&SendResult{
					Response: response,
					Target:   node,
				}, nil, node}
		}(sortedNodes[i])
	}

	// 等待第一个成功的响应或所有失败
	var lastErr error
	successCount := 0
	failCount := 0

	for i := 0; i < maxConcurrent; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case res := <-resultChan:
			if res.err == nil {
				successCount++
				// 记录成功率统计
				logger.Infof("节点 %s 请求成功", res.node.String()[:8])
				return res.response, nil
			}
			failCount++
			lastErr = res.err
			logger.Warnf("节点 %s 请求失败: %v", res.node.String()[:8], res.err)
		}
	}

	// 记录失败统计
	logger.Warnf("请求完成，成功: %d, 失败: %d", successCount, failCount)

	// 在发送完成后记录耗时
	logger.Infof("节点请求耗时统计：")
	logger.Infof("- 节点排序耗时: %v", sendStartTime.Sub(startTime))
	logger.Infof("- 请求发送耗时: %v", time.Since(sendStartTime))

	return nil, fmt.Errorf("所有节点都发送失败: %w", lastErr)
}

// HasAvailableNodes 检查指定协议是否有可用节点
// 参数:
//   - protocolID: 协议标识符
//
// 返回值:
//   - bool: 是否有可用节点
func (c *Client) HasAvailableNodes(protocolID protocol.ID) bool {
	nodes, exists := c.serverProtocol[protocolID]
	if !exists {
		return false
	}
	return len(nodes) > 0
}

// GetServerNodes 获取指定协议的所有服务器节点
// 参数:
//   - protocolID: 协议标识符
//
// 返回值:
//   - []peer.ID: 服务器节点ID列表
func (c *Client) GetServerNodes(protocolID protocol.ID) []peer.ID {
	nodes, exists := c.serverProtocol[protocolID]
	if !exists {
		return make([]peer.ID, 0)
	}
	return append([]peer.ID{}, nodes...) // 返回副本以防止外部修改
}

// ClearServerNodes 清除指定协议的所有服务器节点
// 参数:
//   - protocolID: 协议标识符
func (c *Client) ClearServerNodes(protocolID protocol.ID) {
	delete(c.serverProtocol, protocolID)
	logger.Debugf("清除协议 %s 的所有服务器节点", protocolID)
}
