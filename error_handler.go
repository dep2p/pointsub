package pointsub

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// ErrorCode 错误类型枚举
type ErrorCode int

const (
	// ErrorNetworkTimeout 网络超时错误
	ErrorNetworkTimeout ErrorCode = 1000 + iota
	// ErrorConnectionReset 连接重置错误
	ErrorConnectionReset
	// ErrorConnectionClosed 连接关闭错误
	ErrorConnectionClosed
	// ErrorNetworkUnreachable 网络不可达错误
	ErrorNetworkUnreachable

	// ErrorOutOfMemory 内存不足错误
	ErrorOutOfMemory ErrorCode = 2000 + iota
	// ErrorTooManyConnections 连接数过多错误
	ErrorTooManyConnections
	// ErrorBufferExhausted 缓冲区耗尽错误
	ErrorBufferExhausted

	// ErrorInvalidHeader 无效头部错误
	ErrorInvalidHeader ErrorCode = 3000 + iota
	// ErrorChecksumMismatch 校验和不匹配错误
	ErrorChecksumMismatch
	// ErrorProtocolViolation 协议违规错误
	ErrorProtocolViolation

	// ErrorMessageTooLarge 消息过大错误
	ErrorMessageTooLarge ErrorCode = 4000 + iota
	// ErrorInvalidMessage 无效消息错误
	ErrorInvalidMessage
	// ErrorMessageRejected 消息被拒绝错误
	ErrorMessageRejected
)

// ErrorSeverity 错误严重程度
type ErrorSeverity int

const (
	// SeverityInfo 错误级别：信息
	SeverityInfo ErrorSeverity = iota
	// SeverityWarning 错误级别：警告
	SeverityWarning
	// SeverityError 错误级别：错误
	SeverityError
	// SeverityCritical 错误级别：严重错误
	SeverityCritical
	// SeverityFatal 错误级别：致命错误
	SeverityFatal
)

// ErrorType 错误类型
type ErrorType int

const (
	// NetworkConnError 网络错误 - 连接相关
	NetworkConnError ErrorType = iota
	// NetworkTimeoutError 网络错误 - 超时相关
	NetworkTimeoutError
	// NetworkIOError 网络错误 - I/O相关
	NetworkIOError
	// ProtocolParseError 协议错误 - 解析失败
	ProtocolParseError
	// ProtocolUnsupportedError 协议错误 - 不支持的操作
	ProtocolUnsupportedError
	// ProtocolSecurityError 协议错误 - 安全相关
	ProtocolSecurityError
	// SystemResourceError 系统错误 - 资源不足
	SystemResourceError
	// SystemInternalError 系统错误 - 内部错误
	SystemInternalError
	// UserInputError 用户错误 - 参数错误
	UserInputError
	// UserConfigError 用户错误 - 配置错误
	UserConfigError
)

// ErrorContext 错误上下文信息
type ErrorContext struct {
	// 错误发生的时间
	Timestamp time.Time

	// 错误关联的远程地址（如果适用）
	RemoteAddr net.Addr

	// 错误关联的本地地址（如果适用）
	LocalAddr net.Addr

	// 错误关联的连接ID（如果适用）
	ConnectionID string

	// 错误关联的消息ID（如果适用）
	MessageID string

	// 错误发生时的操作
	Operation string

	// 额外的上下文信息
	Metadata map[string]string
}

// MessageError 包含详细的错误信息
type MessageError struct {
	// 原始错误
	Err error

	// 错误类型
	Type ErrorType

	// 错误严重级别
	Severity ErrorSeverity

	// 是否可恢复
	Recoverable bool

	// 错误上下文
	Context ErrorContext

	// 错误详情
	Details string
}

// Error 实现error接口
func (e *MessageError) Error() string {
	var sb strings.Builder

	// 严重级别
	var severityStr string
	switch e.Severity {
	case SeverityInfo:
		severityStr = "INFO"
	case SeverityWarning:
		severityStr = "WARNING"
	case SeverityError:
		severityStr = "ERROR"
	case SeverityCritical:
		severityStr = "CRITICAL"
	case SeverityFatal:
		severityStr = "FATAL"
	}

	// 错误类型
	var typeStr string
	switch e.Type {
	case NetworkConnError:
		typeStr = "NetworkConn"
	case NetworkTimeoutError:
		typeStr = "NetworkTimeout"
	case NetworkIOError:
		typeStr = "NetworkIO"
	case ProtocolParseError:
		typeStr = "ProtocolParse"
	case ProtocolUnsupportedError:
		typeStr = "ProtocolUnsupported"
	case ProtocolSecurityError:
		typeStr = "ProtocolSecurity"
	case SystemResourceError:
		typeStr = "SystemResource"
	case SystemInternalError:
		typeStr = "SystemInternal"
	case UserInputError:
		typeStr = "UserInput"
	case UserConfigError:
		typeStr = "UserConfig"
	}

	sb.WriteString(fmt.Sprintf("[%s][%s] ", severityStr, typeStr))

	// 是否可恢复
	if e.Recoverable {
		sb.WriteString("(recoverable) ")
	} else {
		sb.WriteString("(non-recoverable) ")
	}

	// 错误详情
	if e.Details != "" {
		sb.WriteString(e.Details)
		sb.WriteString(": ")
	}

	// 原始错误
	if e.Err != nil {
		sb.WriteString(e.Err.Error())
	}

	// 操作信息
	if e.Context.Operation != "" {
		sb.WriteString(fmt.Sprintf(" (during: %s)", e.Context.Operation))
	}

	// 连接信息
	if e.Context.ConnectionID != "" {
		sb.WriteString(fmt.Sprintf(" [conn: %s]", e.Context.ConnectionID))
	}

	// 消息信息
	if e.Context.MessageID != "" {
		sb.WriteString(fmt.Sprintf(" [msg: %s]", e.Context.MessageID))
	}

	return sb.String()
}

// Unwrap 方法用于错误链
func (e *MessageError) Unwrap() error {
	return e.Err
}

// ErrorHandler 定义错误处理接口
type ErrorHandler interface {
	// 处理错误
	HandleError(err error) ErrorStrategy

	// 注册错误处理回调
	RegisterCallback(severity ErrorSeverity, callback func(err *MessageError))

	// 创建新的错误
	NewError(err error, errType ErrorType, severity ErrorSeverity, details string) *MessageError

	// 创建新的上下文错误
	NewContextError(err error, errType ErrorType, severity ErrorSeverity, ctx ErrorContext, details string) *MessageError

	// 设置默认错误处理策略
	SetDefaultStrategy(severity ErrorSeverity, strategy ErrorStrategy)

	// 判断错误是可恢复的
	IsRecoverable(err error) bool
}

// ErrorStrategy 错误处理策略
type ErrorStrategy int

const (
	// StrategyIgnore 忽略错误
	StrategyIgnore ErrorStrategy = iota

	// StrategyRetry 重试错误
	StrategyRetry

	// StrategyAbort 中止当前操作
	StrategyAbort

	// StrategyReset 重置连接
	StrategyReset

	// StrategyClose 关闭连接
	StrategyClose

	// StrategyPanic 崩溃并报告
	StrategyPanic

	// StrategyContinue 继续执行
	StrategyContinue
)

// ConnectionClosedInfo 表示连接已关闭的信息
type ConnectionClosedInfo struct {
	// 原始错误
	Err error
	// 关闭原因
	Reason string
	// 是否为对方主动关闭
	RemoteInitiated bool
	// 是否优雅关闭
	Graceful bool
}

// IsConnectionClosed 判断错误是否表示连接已关闭
// 这不是真正的错误，而是连接生命周期的正常事件
func IsConnectionClosed(err error) (bool, *ConnectionClosedInfo) {
	if err == nil {
		return false, nil
	}

	// 最常见的EOF情况
	if errors.Is(err, io.EOF) {
		return true, &ConnectionClosedInfo{
			Err:             err,
			Reason:          "end of file",
			RemoteInitiated: true,
			Graceful:        true,
		}
	}

	errStr := err.Error()

	// 检查常见的连接关闭提示
	if strings.Contains(errStr, "use of closed network connection") {
		return true, &ConnectionClosedInfo{
			Err:             err,
			Reason:          "connection already closed",
			RemoteInitiated: false,
			Graceful:        true,
		}
	}

	if strings.Contains(errStr, "connection reset by peer") {
		return true, &ConnectionClosedInfo{
			Err:             err,
			Reason:          "reset by peer",
			RemoteInitiated: true,
			Graceful:        false,
		}
	}

	if strings.Contains(errStr, "broken pipe") {
		return true, &ConnectionClosedInfo{
			Err:             err,
			Reason:          "broken pipe",
			RemoteInitiated: true,
			Graceful:        false,
		}
	}

	// 检查是否为网络错误的连接关闭
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			// 超时不是连接关闭
			return false, nil
		}

		// 其他网络错误可能是非优雅的连接关闭
		return true, &ConnectionClosedInfo{
			Err:             err,
			Reason:          "network error",
			RemoteInitiated: true,
			Graceful:        false,
		}
	}

	return false, nil
}

// 默认错误处理实现
type defaultErrorHandler struct {
	// 严重级别对应的处理策略
	strategies map[ErrorSeverity]ErrorStrategy

	// 错误处理回调函数
	callbacks map[ErrorSeverity][]func(err *MessageError)

	// 可恢复性判断函数
	recoverabilityCheckers []func(err error) bool
}

// NewDefaultErrorHandler 创建默认错误处理器
func NewDefaultErrorHandler() ErrorHandler {
	handler := &defaultErrorHandler{
		strategies: make(map[ErrorSeverity]ErrorStrategy),
		callbacks:  make(map[ErrorSeverity][]func(err *MessageError)),
	}

	// 设置默认策略
	handler.strategies[SeverityInfo] = StrategyIgnore
	handler.strategies[SeverityWarning] = StrategyRetry
	handler.strategies[SeverityError] = StrategyAbort
	handler.strategies[SeverityCritical] = StrategyReset
	handler.strategies[SeverityFatal] = StrategyClose

	// 添加默认的可恢复性检查器
	handler.recoverabilityCheckers = append(handler.recoverabilityCheckers, isNetworkErrorRecoverable)
	handler.recoverabilityCheckers = append(handler.recoverabilityCheckers, isTimeoutErrorRecoverable)

	return handler
}

// HandleError 处理错误，确定操作策略
func (h *defaultErrorHandler) HandleError(err error) ErrorStrategy {
	if err == nil {
		return StrategyContinue // 无错误，继续执行
	}

	// 首先检查是否为连接关闭
	closed, info := IsConnectionClosed(err)
	if closed {
		if info.Graceful {
			// 优雅关闭不是错误，应继续或退出循环
			return StrategyContinue
		}
		// 非优雅关闭可能需要重试
		return StrategyRetry
	}

	// 如果是MessageError类型，使用其中的严重性
	var msgErr *MessageError
	if errors.As(err, &msgErr) {
		// 获取策略
		strategy, ok := h.strategies[msgErr.Severity]
		if ok {
			// 调用回调函数
			h.invokeCallbacks(msgErr)
			return strategy
		}
	}

	// 尝试将普通错误转换为MessageError
	converted := h.convertError(err)
	strategy, ok := h.strategies[converted.Severity]
	if ok {
		// 调用回调函数
		h.invokeCallbacks(converted)
		return strategy
	}

	// 默认策略
	return StrategyRetry
}

// 将普通错误转换为MessageError
func (h *defaultErrorHandler) convertError(err error) *MessageError {
	// 首先检查是否为连接关闭
	closed, info := IsConnectionClosed(err)
	if closed {
		// 连接关闭是正常事件，不是错误
		// 但仍然可以创建一个低严重性的消息
		return &MessageError{
			Err:         err,
			Type:        NetworkConnError,
			Severity:    SeverityInfo, // 将其视为信息，而非错误
			Recoverable: true,
			Details: fmt.Sprintf("连接关闭: %s (优雅=%v, 远程=%v)",
				info.Reason, info.Graceful, info.RemoteInitiated),
		}
	}

	// 检查是否为超时
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return &MessageError{
			Err:         err,
			Type:        NetworkTimeoutError,
			Severity:    SeverityWarning,
			Recoverable: true,
			Details:     "网络超时",
		}
	}

	// 其他类型的错误
	return &MessageError{
		Err:         err,
		Type:        SystemInternalError,
		Severity:    SeverityError,
		Recoverable: h.IsRecoverable(err),
		Details:     err.Error(),
	}
}

// RegisterCallback 实现ErrorHandler接口
func (h *defaultErrorHandler) RegisterCallback(severity ErrorSeverity, callback func(err *MessageError)) {
	h.callbacks[severity] = append(h.callbacks[severity], callback)
}

// NewError 实现ErrorHandler接口
func (h *defaultErrorHandler) NewError(err error, errType ErrorType, severity ErrorSeverity, details string) *MessageError {
	return h.NewContextError(err, errType, severity, ErrorContext{
		Timestamp: time.Now(),
	}, details)
}

// NewContextError 实现ErrorHandler接口
func (h *defaultErrorHandler) NewContextError(err error, errType ErrorType, severity ErrorSeverity, ctx ErrorContext, details string) *MessageError {
	// 确保时间戳被设置
	if ctx.Timestamp.IsZero() {
		ctx.Timestamp = time.Now()
	}

	// 判断是否可恢复
	recoverable := h.IsRecoverable(err)

	return &MessageError{
		Err:         err,
		Type:        errType,
		Severity:    severity,
		Recoverable: recoverable,
		Context:     ctx,
		Details:     details,
	}
}

// SetDefaultStrategy 实现ErrorHandler接口
func (h *defaultErrorHandler) SetDefaultStrategy(severity ErrorSeverity, strategy ErrorStrategy) {
	h.strategies[severity] = strategy
}

// IsRecoverable 实现ErrorHandler接口
func (h *defaultErrorHandler) IsRecoverable(err error) bool {
	if err == nil {
		return true
	}

	// 先检查是否已经是MessageError
	var msgErr *MessageError
	if errors.As(err, &msgErr) {
		return msgErr.Recoverable
	}

	// 使用注册的可恢复检查器
	for _, checker := range h.recoverabilityCheckers {
		if checker(err) {
			return true
		}
	}

	// 默认不可恢复
	return false
}

// 网络错误可恢复性检查
func isNetworkErrorRecoverable(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		// 超时错误通常可恢复
		if netErr.Timeout() {
			return true
		}

		// 检查其他可恢复的网络错误
		errMsg := err.Error()
		recoverableErrors := []string{
			"connection reset by peer",
			"broken pipe",
			"connection refused",
			"network is unreachable",
			"no route to host",
		}

		for _, msg := range recoverableErrors {
			if strings.Contains(strings.ToLower(errMsg), msg) {
				return true
			}
		}
	}

	return false
}

// 超时错误可恢复性检查
func isTimeoutErrorRecoverable(err error) bool {
	if err == nil {
		return true
	}

	// 检查是否是超时错误
	errMsg := strings.ToLower(err.Error())
	timeoutMsgs := []string{"timeout", "timed out", "deadline exceeded"}

	for _, msg := range timeoutMsgs {
		if strings.Contains(errMsg, msg) {
			return true
		}
	}

	return false
}

// 调用回调函数
func (h *defaultErrorHandler) invokeCallbacks(err *MessageError) {
	if callbacks, ok := h.callbacks[err.Severity]; ok {
		for _, callback := range callbacks {
			callback(err)
		}
	}
}
