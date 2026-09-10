package service

import (
	"strings"
)

// isRetryableLLMError LLM 调用错误分类（P0-3）：
// 可重试 = 限流/服务端错/网络/超时/JSON 抖动（有恢复提示救回）；
// 不可重试 = 鉴权/权限类（重试无意义，快败省预算）。
func isRetryableLLMError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	nonRetryable := []string{"401", "403", "unauthorized", "invalid_api_key", "forbidden", "permission denied"}
	for _, k := range nonRetryable {
		if strings.Contains(msg, k) {
			return false
		}
	}
	retryable := []string{
		"429", "rate limit", "too many requests",
		"500", "502", "503", "504", "internal server error", "bad gateway", "service unavailable",
		"timeout", "deadline", "connection", "eof", "reset by peer",
		"parse json", "invalid character", "no json content",
	}
	for _, k := range retryable {
		if strings.Contains(msg, k) {
			return true
		}
	}
	return false // 未知错误保守不重试（快败）
}

// clampBrainStepParams Brain 下发步骤参数的服务端钳位（P0-4）：
// LLM 幻觉 retry_count=100 会被指数退避放大成数小时——服务端说了算。
func clampBrainStepParams(retryCount, backoffMs int) (int, int) {
	if retryCount < 0 {
		retryCount = 0
	}
	if retryCount > 3 {
		retryCount = 3
	}
	if backoffMs <= 0 {
		backoffMs = 1000
	}
	if backoffMs > 10000 {
		backoffMs = 10000
	}
	return retryCount, backoffMs
}
