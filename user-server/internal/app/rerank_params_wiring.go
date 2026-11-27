package app

import (
	"context"

	llmpkg "hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/service"
)

// WireRerankConfigParams 把参数中心的 rerank.global_override 接进 aiagent/llm。
//
// 与 llm/embedding 那批同构：两缝（读口+写口），写侧必须经 ConfigParamService
// 才有值类型校验、缓存失效和变更审计；本包不能反向 import service，
// 所以读写口由装配层注入闭包。
//
// 全局 override 必须写平台默认行：管理端 JWT 带租户上下文时 UpdateValue 会写
// tenant_config_params，平台级读取点看不到，线上 PUT 400 即由此产生。
//
// Get*/UpdatePlatformDefault 调用与键名字面量写在同一行
// （scripts/check-config-param-readpoints.py 认的就是这个形状）。
//
// 返回本批接上的键名列表，供启动日志与装配层测试断言。
func WireRerankConfigParams() []string {
	return wireRerankConfigParamsWith(service.GlobalConfigParam())
}

func wireRerankConfigParamsWith(cp globalOverridePlatformConfigParams) []string {
	llmpkg.SetRerankOverrideProvider(func(ctx context.Context) string {
		return cp.GetString(ctx, "rerank", "global_override", "")
	})

	llmpkg.SetRerankOverrideWriter(func(ctx context.Context, raw string, actorID uint) error {
		return cp.UpdatePlatformDefault(ctx, "rerank", "global_override", raw, actorID)
	})

	return []string{"rerank.global_override"}
}
