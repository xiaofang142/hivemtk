package app

import (
	"context"
	"time"

	appsvc "hivemtk-user/internal/service"

	knowledgesvc "hivemtk-user/internal/aiagent/knowledge/service"
	"hivemtk-user/internal/middleware"
)

// WireSalesMiddlewareKnowledgeWecomChannelGWConfigParams 注入 sales / middleware /
// knowledge / wecom / channelgw 五组参数中心读取器。
//
// 收录本函数的标准是「点位横跨 model / middleware / knowledge / channelgw 等不能反向
// 依赖 service 的下层包」，就地接线只能各造函数变量，装配层是唯一能同时看见全部的地方。
//
// 每条都写成显式闭包，且 Get* 与键名字面量必须在同一行：
// scripts/check-config-param-readpoints.py 认的是这个形状，藏进通用 helper 会被判成未接线。
//
// 函数不接 ctx 参数：provider 是延迟读的，捕获装配时的 ctx 会随请求取消一起失效。
func WireSalesMiddlewareKnowledgeWecomChannelGWConfigParams() []string {
	bg := context.Background()
	cp := appsvc.GlobalConfigParam()
	wired := make([]string, 0, 6)

	appsvc.SetIdentityMaxAttemptsProvider(func() int {
		return cp.GetInt(bg, "sales", "identity_max_attempts", appsvc.DefaultIdentityMaxAttempts)
	})
	wired = append(wired, "sales.identity_max_attempts")

	middleware.SetAuditFlushIntervalProvider(func() time.Duration {
		return cp.GetDuration(bg, "middleware", "audit_flush_interval", middleware.DefaultAuditFlushInterval)
	})
	wired = append(wired, "middleware.audit_flush_interval")

	middleware.SetMfaRecentVerifyTTLProvider(func() time.Duration {
		return cp.GetDuration(bg, "middleware", "mfa_recent_verify_ttl", middleware.DefaultMfaRecentVerifyTTL)
	})
	wired = append(wired, "middleware.mfa_recent_verify_ttl")

	// 种子是 int 类型、上限有 500MB 上界（524288000），int64 完全放得下，
	// 这里取 int 是安全的；换算只在这一行，不下沉到 knowledge 包。
	knowledgesvc.SetMaxUploadFileSizeProvider(func() int64 {
		return int64(cp.GetInt(bg, "knowledge", "max_upload_file_size", int(knowledgesvc.DefaultMaxUploadFileSize)))
	})
	wired = append(wired, "knowledge.max_upload_file_size")

	knowledgesvc.SetMerchantKnowledgeMaxLenProvider(func() int {
		return cp.GetInt(bg, "knowledge", "merchant_knowledge_max_len", knowledgesvc.DefaultMerchantKnowledgeMaxLen)
	})
	wired = append(wired, "knowledge.merchant_knowledge_max_len")

	// channelgw.ws_push_interval 与 wecom.quota_degrade 不在这里：
	// 它们走的是「读取函数自己调 GlobalConfigParam()」的直连范式（见 channelgw/ws.go、
	// wecom_account_health.go），不需要注入 provider。ws_push_interval 原先的读取点用了
	// 一个种子表里根本不存在的 key（ws_push_interval_default），本轮已改正。

	return wired
}
