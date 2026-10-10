package app

import (
	"context"
	"time"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/service"
)

// WireAgentToolConfigParams 把 config_params 的 agent_tool 组接进工具执行器与
// 工具路由中心四个点位。
//
// 分层口径：这两个点位住在 tooluse 包里，该包不能反向 import service，所以注入在
// 装配层做，与 app/tool_executor_wiring.go（装 ToolRouter）、app/tool_circuit_breaker_wiring.go
// （装装饰链熔断器）同一个层次。
//
// 注意与 tool_circuit_breaker_wiring.go 的分工：那一份管的是 executor 装饰链上的
// 按工具熔断，取值来自环境变量（TOOL_CIRCUIT_FAILURE_THRESHOLD 等），处于 off /
// shadow / enforce 三态灰度；这里管的是 ToolRouter 内部"失败就换同类工具"的短路。
// 两套熔断独立计量、互不共享状态，所以这里接的参数不会影响那一份的判定阈值。
//
// 未接线的一条：agent_tool.result_cache_ttl。ResultCache 的实现（result_cache.go）
// 在，但 NewResultCache 全仓只有 p2_test.go 调用，生产链路从未构造过它——没有构造点
// 就谈不上接 TTL，该条如实保留「未接线」标注，不为了凑数造一个没人调用的 seam。
//
// Get* 调用与键名字面量必须写在同一行：scripts/check-config-param-readpoints.py
// 认的就是这个形状。
func WireAgentToolConfigParams() []string {
	bg := context.Background()
	cp := service.GlobalConfigParam()
	wired := make([]string, 0, 4)

	tooluse.SetMaxConcurrentProvider(func() int {
		return cp.GetInt(bg, "agent_tool", "max_concurrent", tooluse.DefaultMaxConcurrent)
	})
	wired = append(wired, "agent_tool.max_concurrent")

	tooluse.SetMaxContentLenProvider(func() int {
		return cp.GetInt(bg, "agent_tool", "max_content_len", tooluse.DefaultMaxContentLen)
	})
	wired = append(wired, "agent_tool.max_content_len")

	tooluse.SetRouterFailThresholdProvider(func() int {
		return cp.GetInt(bg, "agent_tool", "fail_threshold", tooluse.DefaultRouterFailThreshold)
	})
	wired = append(wired, "agent_tool.fail_threshold")

	tooluse.SetRouterCooldownDurationProvider(func() time.Duration {
		return cp.GetDuration(bg, "agent_tool", "cooldown_duration", tooluse.DefaultRouterCooldownDuration)
	})
	wired = append(wired, "agent_tool.cooldown_duration")

	return wired
}
