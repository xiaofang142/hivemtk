package controller

import "strings"

func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "不存在") || strings.Contains(msg, "not found")
}

// isUpstreamSyncError 判断错误是否来自「外部数据源不可达/回错」这类上游问题。
// 这类失败不是本服务的内部故障，控制器应返回 502 并把可执行原因透传给用户，
// 否则会被统一兜底成「服务器内部错误」，用户完全无法判断该改端点还是改认证。
func isUpstreamSyncError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, kw := range []string{
		"no such host", "connection refused", "i/o timeout", "context deadline exceeded",
		"请求失败", "读取响应失败", "tls:", "TLS handshake", "certificate", "EOF",
		"HTTP 4", "HTTP 5",
	} {
		if strings.Contains(msg, kw) {
			return true
		}
	}
	return false
}
