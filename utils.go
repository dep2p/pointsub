package pointsub

import (
	"fmt"
	"math/rand"
	"time"
)

// 全局随机数生成器
var rnd = rand.New(rand.NewSource(time.Now().UnixNano()))

// generateTransferID 生成传输ID
// 用于标识不同的消息传输任务
func generateTransferID() string {
	// 使用时间戳和随机字符串组合
	timestamp := time.Now().UnixNano()
	random := randomString(8)
	return fmt.Sprintf("%d-%s", timestamp, random)
}

// randomString 生成指定长度的随机字符串
func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, length)
	for i := range result {
		result[i] = charset[rnd.Intn(len(charset))]
	}
	return string(result)
}
