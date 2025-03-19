package pointsub

import logging "github.com/dep2p/log"

var logger = logging.Logger("pointsub")

// init 初始化全局日志实例
// 该函数在包初始化时自动执行,用于设置默认的日志配置
func init() {
	// 设置默认的日志配置
	// 使用JSON格式输出,输出到标准错误,日志级别为INFO
	logging.SetupLogging(logging.Config{
		Format: logging.JSONOutput, // 设置输出格式为JSON
		Stderr: true,               // 输出到标准错误
		// Level:  logging.LevelDebug,  // 设置日志级别为DEBUG
		Level: logging.LevelError, // 设置日志级别为ERROR
	})
}
