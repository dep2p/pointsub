// Package pointsub 提供了基于 dep2p 的点对点订阅功能的实现
package pointsub

import (
	"context"
	"errors"
	"fmt"

	"github.com/dep2p/go-dep2p/core/peer"
	"github.com/dep2p/go-dep2p/core/protocol"
)

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
//
// 返回值:
//   - []byte: 响应数据
//   - error: 错误信息
func (c *Client) SendClosest(ctx context.Context, protocolID protocol.ID, request []byte) ([]byte, error) {
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

	// 按距离排序所有节点
	sortedNodes, err := SortNodesByDistance(request, nodes)
	if err != nil {
		logger.Errorf("节点排序失败: %v", err)
		return nil, fmt.Errorf("节点排序失败: %w", err)
	}

	// 逐个尝试节点
	var lastErr error
	for i, node := range sortedNodes {
		select {
		case <-ctx.Done():
			// 如果已经有成功的响应，返回它
			if lastErr == nil {
				return nil, ctx.Err()
			}
			// 否则返回最后一个错误，优先级高于超时错误
			return nil, fmt.Errorf("所有节点都发送失败: %w", lastErr)
		default:
			logger.Infof("尝试发送到节点 %s (尝试次数: %d/%d)", node.String()[:8], i+1, len(sortedNodes))
			response, err := c.Send(ctx, node, protocolID, request)
			if err != nil {
				lastErr = err
				logger.Warnf("发送到节点 %s 失败: %v", node.String()[:8], err)
				continue
			}
			return response, nil
		}
	}

	if lastErr != nil {
		return nil, fmt.Errorf("所有节点都发送失败: %w", lastErr)
	}
	return nil, ctx.Err()
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
