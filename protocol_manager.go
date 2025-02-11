// Package pointsub 提供了基于 dep2p 的点对点订阅功能的实现
package pointsub

import (
	"context"
	"errors"
	"fmt"

	"github.com/dep2p/go-dep2p/core/peer"
	"github.com/dep2p/go-dep2p/core/protocol"
)

// SendResult 定义发送操作的结果
type SendResult struct {
	Response []byte  // 响应数据
	Target   peer.ID // 成功发送到的目标节点
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

	// 过滤掉要排除的节点
	availableNodes := filterExcludeNodes(nodes, excludeNodes)
	if len(availableNodes) == 0 {
		return nil, errors.New("没有可用的未失败节点")
	}

	// 按距离排序所有节点
	sortedNodes, err := SortNodesByDistance(request, availableNodes)
	if err != nil {
		logger.Errorf("节点排序失败: %v", err)
		return nil, fmt.Errorf("节点排序失败: %w", err)
	}

	// 逐个尝试节点
	var lastErr error
	for i, node := range sortedNodes {
		select {
		case <-ctx.Done():
			if lastErr == nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("所有节点都发送失败: %w", lastErr)
		default:
			logger.Infof("尝试发送到节点 %s (尝试次数: %d/%d)", node.String()[:8], i+1, len(sortedNodes))
			response, err := c.Send(ctx, node, protocolID, request)
			if err != nil {
				lastErr = err
				logger.Warnf("发送到节点 %s 失败: %v", node.String()[:8], err)
				continue
			}
			return &SendResult{
				Response: response,
				Target:   node,
			}, nil
		}
	}

	if lastErr != nil {
		return nil, fmt.Errorf("所有节点都发送失败: %w", lastErr)
	}
	return nil, ctx.Err()
}

// filterExcludeNodes 从节点列表中过滤掉要排除的节点
func filterExcludeNodes(nodes []peer.ID, excludeNodes []peer.ID) []peer.ID {
	if len(excludeNodes) == 0 {
		return nodes
	}

	result := make([]peer.ID, 0, len(nodes))
	for _, node := range nodes {
		excluded := false
		for _, excludeNode := range excludeNodes {
			if node == excludeNode {
				excluded = true
				break
			}
		}
		if !excluded {
			result = append(result, node)
		}
	}
	return result
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
