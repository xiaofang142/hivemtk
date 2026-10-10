package app

import (
	"context"

	llmpkg "hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/service"
)

// WireEmbeddingConfigParams 把参数中心的 embedding.global_override 接进 aiagent/llm。
//
// 这一条与其它组的形状不同：它有**两**条缝，一读一写。原因是写侧原本是一条
// `INSERT INTO system_kv_config … ON CONFLICT` 的裸 SQL（D12 要拦的就是这个形状），
// 而参数中心的写侧必须经 ConfigParamService 才有值类型校验、缓存失效和变更审计 ——
// 只接读口会把"管理端在线改"这半边留在旧路径上，那份配置就同时住在两张表里。
//
// aiagent/llm 不能反向 import service（service 已经 import 它，会成环），
// 所以和 channelbot / agent_llm 两批一样在装配层注入闭包。
//
// Get*/UpdateValue 调用与键名字面量写在同一行：
// scripts/check-config-param-readpoints.py 认的就是这个形状。
//
// 返回本批接上的键名列表，供启动日志与装配层测试断言。
func WireEmbeddingConfigParams() []string {
	cp := service.GlobalConfigParam()

	llmpkg.SetEmbeddingOverrideProvider(func(ctx context.Context) string {
		return cp.GetString(ctx, "embedding", "global_override", "")
	})

	llmpkg.SetEmbeddingOverrideWriter(func(ctx context.Context, raw string, actorID uint) error {
		return cp.UpdateValue(ctx, "embedding", "global_override", raw, actorID)
	})

	return []string{"embedding.global_override"}
}
