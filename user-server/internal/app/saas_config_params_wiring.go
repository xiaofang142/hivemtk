package app

// SaaS 配置面改造（2026-11-17）：env→参数中心迁移的 seam 装配点。
//
// 本文件集中装配五条 provider seam（消费包不反向依赖 service，与本仓其余
// 参数接线同一范式，见 middleware/body_limit.go 的 provider 注释）：
//   - etl.parser_sidecar_url / etl.image_describe_url / etl.image_describe_model
//   - misc.brain_token_budget（browser_automation session 级 token 预算）
//   - misc.file_url_ttl（storage 签名 URL 时效，秒；0=不过期）
//   - knowledge.contextual_enhance_enabled（RAG 上下文增强总开关）
//
// 对应退役的环境变量：PARSER_SIDECAR_URL / IMAGE_DESCRIBE_URL / IMAGE_DESCRIBE_MODEL /
// HEALTH_EMBEDDING_CRITICAL / BRAIN_TOKEN_BUDGET / FILE_URL_TTL / RAG_CONTEXTUAL_ENHANCE。

import (
	"context"

	knowledgesvc "hivemtk-user/internal/aiagent/knowledge/service"
	bassvc "hivemtk-user/internal/browser_automation/service"
	"hivemtk-user/internal/etl"
	"hivemtk-user/internal/service"
	"hivemtk-user/internal/storage"
)

// WireSaaSConfigParamSeams 把 env→参数中心迁移的各消费包 provider 接上参数中心。
// 在 db 连接 + SeedConfigParams 之后调用一次；参数中心未装配时各 Get* 走兜底默认值，
// 与改接前「env 未配置」的行为一致（fail-safe，不因装配顺序炸启动）。
func WireSaaSConfigParamSeams() {
	svc := service.GlobalConfigParam()

	// etl：外挂解析地址（进程级、启动期快照语义，不热切——解析引擎切换影响索引一致性）
	sidecarURL := svc.GetString(context.Background(), "etl", "parser_sidecar_url", "")
	etl.SetSidecarURLProvider(func() string {
		return sidecarURL
	})
	// etl：图片描述服务（url, model 成对读）
	etl.SetImageDescribeConfigProvider(func() (string, string) {
		return svc.GetString(context.Background(), "etl", "image_describe_url", ""),
			svc.GetString(context.Background(), "etl", "image_describe_model", "")
	})
	// browser_automation：session 级 token 预算
	bassvc.SetBrainTokenBudgetProvider(func() int {
		return svc.GetInt(context.Background(), "misc", "brain_token_budget", 0)
	})
	// storage：签名 URL 时效（秒；0=不过期）
	storage.SetFileURLTTLProvider(func() int {
		return svc.GetInt(context.Background(), "misc", "file_url_ttl", 0)
	})
	// knowledge：上下文增强总开关（默认 true，与退役前 env 未设时语义一致）
	knowledgesvc.SetContextualEnhanceProvider(func() bool {
		return svc.GetBool(context.Background(), "knowledge", "contextual_enhance_enabled", true)
	})
}
