// Package apperr 机器可读错误分类（借鉴 catbus errors.ts 模式）。
//
// 目标：让调用方（ReAct Agent、MCP 客户端、前端）拿到的不只是文案，还有
// 稳定的错误类别 + 是否可重试 + 下一步建议，驱动自动化决策而不是盲试。
package apperr

import (
	"errors"
	"fmt"
)

// Code 稳定错误类别。类别集合是契约：新增可以，改名/删名不允许。
type Code string

const (
	// RiskControl 平台风控/限流拦截：不可自动重试，需人工介入或等待。
	RiskControl Code = "RISK_CONTROL"
	// AuthExpired 凭证失效：重新认证后重试。
	AuthExpired Code = "AUTH_EXPIRED"
	// ConfirmRequired 操作不可撤回，需要用户显式确认后重发。
	ConfirmRequired Code = "CONFIRM_REQUIRED"
	// RateLimited 本系统侧限流：按 RetryAfter 等待后可重试。
	RateLimited Code = "RATE_LIMITED"
	// Upstream 第三方上游错误：可有限次重试。
	Upstream Code = "UPSTREAM"
	// InvalidInput 入参不合法：修正参数后重试。
	InvalidInput Code = "INVALID_INPUT"
	// NotFound 目标不存在：核对标识。
	NotFound Code = "NOT_FOUND"
	// Internal 本系统内部错误：不可重试，需看日志。
	Internal Code = "INTERNAL"
)

// Error 携带类别与下一步建议的错误。
type Error struct {
	Code       Code   `json:"code"`
	Message    string `json:"message"`
	Hint       string `json:"hint,omitempty"` // 下一步该做什么（给 Agent/用户）
	Retryable  bool   `json:"retryable"`      // 是否可自动重试
	RetryAfter int    `json:"retry_after_ms,omitempty"`
	cause      error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.cause }

// New 构造指定类别错误。
func New(code Code, msg, hint string, retryable bool) *Error {
	return &Error{Code: code, Message: msg, Hint: hint, Retryable: retryable}
}

// Wrap 包装底层错误（errors.Is/As 可穿透）。
func Wrap(code Code, msg, hint string, retryable bool, cause error) *Error {
	return &Error{Code: code, Message: msg, Hint: hint, Retryable: retryable, cause: cause}
}

// From 从任意 error 提取 *Error（非 apperr 时归类 Internal）。
func From(err error) *Error {
	var ae *Error
	if errors.As(err, &ae) {
		return ae
	}
	return &Error{Code: Internal, Message: err.Error(), Retryable: false}
}
