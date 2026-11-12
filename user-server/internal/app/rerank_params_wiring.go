package app

import (
	"context"

	llmpkg "hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/service"
)

// WireRerankConfigParams 把参数中心的 rerank.global_override 接进 aiagent/llm。
//
// 与 embedding 那批同构：两缝（读口+写口），写侧必须经 ConfigParamService
// 才有值类型校验、缓存失效和变更审计；本包不能反向 import service，
// 所以读写口由装配层注入闭包。Get*/UpdateValue 调用与键名字面量写在同一行
// （scripts/check-config-param-readpoints.py 认的就是这个形状）。
//
// 返回本批接上的键名列表，供启动日志与装配层测试断言。
func WireRerankConfigParams() []string {
	cp := service.GlobalConfigParam()

	llmpkg.SetRerankOverrideProvider(func(ctx context.Context) string {
		return cp.GetString(ctx, "rerank", "global_override", "")
	})

	llmpkg.SetRerankOverrideWriter(func(ctx context.Context, raw string, actorID uint) error {
		return cp.UpdateValue(ctx, "rerank", "global_override", raw, actorID)
	})

	return []string{"rerank.global_override"}
}
