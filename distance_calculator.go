// Package pointsub 提供了基于 dep2p 的点对点订阅功能的实现
package pointsub

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/dep2p/go-dep2p/core/host"
	"github.com/dep2p/go-dep2p/core/peer"
)

// keySize 定义了 SHA-256 哈希的长度
const keySize = 32 // SHA-256 哈希长度

// 全局变量定义
var (
	distanceCache  sync.Map // 距离计算缓存,用于存储节点间距离计算结果
	nodeStateCache sync.Map // 节点状态缓存,用于存储节点的在线状态、延迟等信息

	// 配置参数
	maxCacheSize  = 10000           // 最大缓存条目数,限制缓存大小
	cacheTTL      = 5 * time.Minute // 缓存过期时间,控制缓存项的生命周期
	maxConcurrent = 100             // 最大并发计算数,限制同时进行的计算任务数

	// host 实例,用于网络通信
	hostInstance host.Host
)

// NodeState 定义节点状态
type NodeState struct {
	Online    bool          // 节点是否在线
	Latency   time.Duration // 到节点的网络延迟
	Load      float64       // 节点负载情况
	LastCheck time.Time     // 最后检查时间
}

// nodeDistance 定义节点距离信息
type nodeDistance struct {
	node     peer.ID       // 节点ID
	distance *big.Int      // 到目标的距离
	online   bool          // 节点在线状态
	latency  time.Duration // 网络延迟
}

// CalculateDistance 计算内容与节点之间的距离
// 参数:
//   - content: 要计算距离的内容
//   - nodeID: 目标节点ID
//
// 返回值:
//   - *big.Int: 计算得到的距离值
//   - error: 计算过程中的错误
func CalculateDistance(content []byte, nodeID peer.ID) (*big.Int, error) {
	// 1. 输入验证
	if len(content) == 0 || nodeID == "" {
		return nil, fmt.Errorf("invalid input: content=%d bytes, nodeID=%s", len(content), nodeID)
	}

	// 2. 检查缓存
	cacheKey := fmt.Sprintf("%x-%s", sha256.Sum256(content), nodeID)
	if cached, ok := distanceCache.Load(cacheKey); ok {
		return cached.(*big.Int), nil
	}

	// 3. 计算距离
	contentHash := sha256.Sum256(content)
	nodeHash := sha256.Sum256([]byte(nodeID))

	distance := make([]byte, keySize)
	for i := 0; i < keySize; i++ {
		distance[i] = contentHash[i] ^ nodeHash[i]
	}

	result := new(big.Int).SetBytes(distance)

	// 4. 更新缓存
	distanceCache.Store(cacheKey, result)

	return result, nil
}

// FindNearestNode 查找最近的节点
// 参数:
//   - content: 要查找的内容
//   - nodes: 候选节点列表
//
// 返回值:
//   - peer.ID: 最近的节点ID
//   - error: 查找过程中的错误
func FindNearestNode(content []byte, nodes []peer.ID) (peer.ID, error) {
	if len(nodes) == 0 {
		return "", fmt.Errorf("empty node list")
	}

	// 1. 并发计算节点权重
	type nodeScore struct {
		node  peer.ID
		score float64
		err   error
	}

	scoreChan := make(chan nodeScore, len(nodes))
	sem := make(chan struct{}, maxConcurrent) // 限制并发数

	for _, node := range nodes {
		sem <- struct{}{} // 获取信号量
		go func(n peer.ID) {
			defer func() { <-sem }() // 释放信号量

			score, err := calculateNodeScore(content, n)
			scoreChan <- nodeScore{n, score, err}
		}(node)
	}

	// 2. 收集结果并找出最佳节点
	var (
		bestNode  peer.ID
		bestScore float64
		firstNode = true
	)

	for i := 0; i < len(nodes); i++ {
		score := <-scoreChan
		if score.err != nil {
			logger.Warnf("计算节点分数失败: %v", score.err)
			continue
		}

		if firstNode || score.score > bestScore {
			bestScore = score.score
			bestNode = score.node
			firstNode = false
		}
	}

	if bestNode == "" {
		return "", fmt.Errorf("no valid nodes found")
	}

	return bestNode, nil
}

// calculateNodeScore 计算节点的综合得分
// 参数:
//   - content: 要计算的内容
//   - node: 目标节点
//
// 返回值:
//   - float64: 计算得到的得分
//   - error: 计算过程中的错误
func calculateNodeScore(content []byte, node peer.ID) (float64, error) {
	// 1. 获取或更新节点状态
	state, err := getNodeState(node)
	if err != nil {
		return 0, err
	}

	// 2. 计算距离分数
	distance, err := CalculateDistance(content, node)
	if err != nil {
		return 0, err
	}

	// 3. 计算综合得分
	// - 距离权重: 40%
	// - 延迟权重: 30%
	// - 负载权重: 20%
	// - 在线状态: 10%
	distanceScore := 1.0 / (1.0 + float64(distance.Int64()))
	latencyScore := 1.0 / (1.0 + float64(state.Latency.Milliseconds()))
	loadScore := 1.0 - state.Load
	onlineScore := 0.0
	if state.Online {
		onlineScore = 1.0
	}

	return 0.4*distanceScore + 0.3*latencyScore + 0.2*loadScore + 0.1*onlineScore, nil
}

// getNodeState 获取或更新节点状态
// 参数:
//   - node: 目标节点
//
// 返回值:
//   - *NodeState: 节点状态信息
//   - error: 获取过程中的错误
func getNodeState(node peer.ID) (*NodeState, error) {
	// 1. 检查缓存
	if cached, ok := nodeStateCache.Load(node); ok {
		state := cached.(*NodeState)
		if time.Since(state.LastCheck) < cacheTTL {
			return state, nil
		}
	}

	// 2. 更新节点状态
	state := &NodeState{
		LastCheck: time.Now(),
	}

	// 并发获取节点信息
	var wg sync.WaitGroup
	var errChan = make(chan error, 3)

	wg.Add(3)
	go func() {
		defer wg.Done()
		online, err := checkNodeStatus(node)
		if err != nil {
			errChan <- err
			return
		}
		state.Online = online
	}()

	go func() {
		defer wg.Done()
		latency, err := measureNodeLatency(node)
		if err != nil {
			errChan <- err
			return
		}
		state.Latency = latency
	}()

	go func() {
		defer wg.Done()
		load, err := calculateNodeLoad(node)
		if err != nil {
			errChan <- err
			return
		}
		state.Load = load
	}()

	wg.Wait()
	close(errChan)

	// 3. 处理错误
	if len(errChan) > 0 {
		return nil, fmt.Errorf("failed to get node state: %v", <-errChan)
	}

	// 4. 更新缓存
	nodeStateCache.Store(node, state)

	return state, nil
}

// FindNodesWithinDistance 找到在指定距离范围内的所有节点
// 参数:
//   - content: 内容数据
//   - nodes: 候选节点列表
//   - maxDistance: 最大距离
//
// 返回值:
//   - []peer.ID: 在范围内的节点列表
func FindNodesWithinDistance(content []byte, nodes []peer.ID, maxDistance *big.Int) []peer.ID {
	var result []peer.ID

	for _, node := range nodes {
		distance, err := CalculateDistance(content, node)
		if err != nil {
			continue // 跳过出错的节点
		}
		if distance.Cmp(maxDistance) <= 0 {
			result = append(result, node)
		}
	}

	return result
}

// SortNodesByDistance 根据与内容的距离对节点进行排序
// 参数:
//   - content: 内容数据
//   - nodes: 要排序的节点列表
//
// 返回值:
//   - []peer.ID: 排序后的节点列表
//   - error: 排序过程中的错误
func SortNodesByDistance(content []byte, nodes []peer.ID) ([]peer.ID, error) {
	if len(nodes) == 0 {
		return nil, fmt.Errorf("empty node list")
	}

	// 修复 CalculateDistance 调用
	distances := make([]nodeDistance, len(nodes))
	var wg sync.WaitGroup
	errChan := make(chan error, len(nodes))

	for i, node := range nodes {
		wg.Add(1)
		go func(idx int, n peer.ID) {
			defer wg.Done()
			dist, err := CalculateDistance(content, n)
			if err != nil {
				errChan <- err
				return
			}
			online, err := checkNodeStatus(n)
			if err != nil {
				errChan <- err
				return
			}
			latency, err := measureNodeLatency(n)
			if err != nil {
				errChan <- err
				return
			}
			distances[idx] = nodeDistance{
				node:     n,
				distance: dist,
				online:   online,
				latency:  latency,
			}
		}(i, node)
	}

	wg.Wait()
	close(errChan)

	if len(errChan) > 0 {
		return nil, <-errChan
	}

	// 根据距离排序
	sorted := make([]peer.ID, len(nodes))
	for i := range nodes {
		minIdx := i
		for j := i + 1; j < len(distances); j++ {
			if distances[j].distance.Cmp(distances[minIdx].distance) < 0 {
				minIdx = j
			}
		}
		// 交换位置
		distances[i], distances[minIdx] = distances[minIdx], distances[i]
		sorted[i] = distances[i].node
	}

	return sorted, nil
}

// SetHost 设置全局 host 实例
// 参数:
//   - h: 要设置的 host 实例
func SetHost(h host.Host) {
	hostInstance = h
}

// checkNodeStatus 检查节点是否在线
// 参数:
//   - node: 要检查的节点
//
// 返回值:
//   - bool: 节点是否在线
//   - error: 检查过程中的错误
func checkNodeStatus(node peer.ID) (bool, error) {
	if hostInstance == nil {
		return true, nil // 如果没有设置 host，默认返回在线
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := hostInstance.Connect(ctx, peer.AddrInfo{ID: node})
	if err != nil {
		return false, nil
	}
	return true, nil
}

// measureNodeLatency 测量到节点的网络延迟
// 参数:
//   - node: 要测量的节点
//
// 返回值:
//   - time.Duration: 测量到的延迟时间
//   - error: 测量过程中的错误
func measureNodeLatency(node peer.ID) (time.Duration, error) {
	if hostInstance == nil {
		return time.Millisecond * 100, nil // 默认延迟
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := hostInstance.Connect(ctx, peer.AddrInfo{ID: node})
	if err != nil {
		return 0, err
	}

	return time.Since(start), nil
}

// calculateNodeLoad 计算节点负载
// 参数:
//   - node: 要计算负载的节点
//
// 返回值:
//   - float64: 节点负载值(0-1之间)
//   - error: 计算过程中的错误
func calculateNodeLoad(node peer.ID) (float64, error) {
	if hostInstance == nil {
		return 0.5, nil // 默认负载
	}

	timeoutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := hostInstance.Connect(timeoutCtx, peer.AddrInfo{ID: node})
	if err != nil {
		logger.Errorf("连接节点失败: %v", err)
		return 0, err
	}
	defer hostInstance.Network().ClosePeer(node)

	metrics, err := hostInstance.Peerstore().Get(node, "metrics")
	if err != nil {
		return 0.5, nil
	}

	if load, ok := metrics.(float64); ok {
		return load, nil
	}
	return 0.5, nil
}

// init 初始化函数
// 启动缓存清理机制
func init() {
	go func() {
		ticker := time.NewTicker(cacheTTL)
		defer ticker.Stop()

		for range ticker.C {
			cleanCache(&distanceCache, maxCacheSize)
			cleanCache(&nodeStateCache, maxCacheSize)
		}
	}()
}

// cleanCache 清理缓存
// 参数:
//   - cache: 要清理的缓存
//   - maxSize: 最大缓存大小
func cleanCache(cache *sync.Map, maxSize int) {
	var count int
	cache.Range(func(key, value interface{}) bool {
		count++
		if count > maxSize {
			cache.Delete(key)
		}
		return true
	})
}
