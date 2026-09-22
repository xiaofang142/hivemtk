package core

import (
	"fmt"
	"time"
)

// APIError 渠道 API 调用失败时"客户端当场才知道"的三项事实：
// HTTP 状态码、渠道自带业务码、渠道明确给出的等待秒数。
//
// 为什么必须在这里定义（而不是 service 层的 ChannelError）：出站客户端在
// internal/channelbot/** 侧，它不 import internal/service（反向依赖），
// 而这三项信息一旦只拼成错误串跨层，到上层就再也还原不回来 ——
// 审计 N-11 的四处丢信号（retry_after= 解不出、裸 429 无 status 字样、
// errcode 被丢弃、拿到了没人用）根因都在这一层。
//
// 语义约定：只承载事实，不判定性质。某个业务码算不算限流、要不要重试，
// 由 service 层按渠道解释（见 internal/service/channel_error.go 的码表）。
type APIError struct {
	Channel    string        // 渠道标识，取 service 侧 WebhookChannel 的取值（telegram/qq/whatsapp/…）
	StatusCode int           // HTTP 状态码；0 = 未拿到（传输层失败等）
	Code       string        // 渠道业务码原文；空 = 渠道没给或不稳定（如 Telegram 的 error_code）
	RetryAfter time.Duration // 渠道明确要求的等待时长；0 = 渠道没说
	Raw        string        // 给人看的原文（含响应体片段），日志与失败轨迹用它
}

func (e *APIError) Error() string {
	if e.Raw != "" {
		return e.Raw
	}
	return fmt.Sprintf("%s api failed: status=%d code=%s", e.Channel, e.StatusCode, e.Code)
}
