package app

import (
	"context"

	llmpkg "hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/service"
)

// globalOverridePlatformConfigParams 是装配层对参数中心的最小依赖。
// 全局 override 必须写平台默认行：管理端 JWT 带租户上下文，写租户行会让
// 平台级读取点永远看不到新值（线上 PUT 400 的根因）。
type globalOverridePlatformConfigParams interface {
	GetString(ctx context.Context, group, key, fallback string) string
	UpdatePlatformDefault(ctx context.Context, group, key, newValue string, actorID uint) error
}

// WireLLMConfigParams 把参数中心的 llm.global_override 接进 aiagent/llm。
//
// 与 embedding 那批同构：两缝（读口+写口），写侧必须经 ConfigParamService
// 才有值类型校验、缓存失效和变更审计；本包不能反向 import service，
// 所以读写口由装配层注入闭包。Get*/UpdatePlatformDefault 调用与键名字面量写在同一行
// （scripts/check-config-param-readpoints.py 认的就是这个形状）。
//
// 返回本批接上的键名列表，供启动日志与装配层测试断言。
func WireLLMConfigParams() []string {
	return wireLLMConfigParamsWith(service.GlobalConfigParam())
}

func wireLLMConfigParamsWith(cp globalOverridePlatformConfigParams) []string {
	llmpkg.SetLLMOverrideProvider(func(ctx context.Context) string {
		return cp.GetString(ctx, "llm", "global_override", "")
	})

	llmpkg.SetLLMOverrideWriter(func(ctx context.Context, raw string, actorID uint) error {
		return cp.UpdatePlatformDefault(ctx, "llm", "global_override", raw, actorID)
	})

	return []string{"llm.global_override"}
}
