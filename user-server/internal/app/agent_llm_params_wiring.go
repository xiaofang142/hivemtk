package app

import (
	"context"
	"time"

	llmpkg "hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/service"
)

// WireAgentLLMConfigParams 把 config_params 的 agent_llm 组接进 aiagent/llm 包。
//
// 接线点分散在 aiagent/llm 内部的 provider_failover.go / dispatcher_dispatch.go /
// llm.go / trace_sink.go，而 aiagent/llm 不能反向依赖 service（那会成环），
// 所以统一在装配层注入。接线范式与 misc / confidence 两批一致：
//
//   - 每条一个 SetXxxProvider，只注入闭包，不注入值——这些读取口在构造期和运行期
//     都会调，注入值等于把参数快照冻在启动那一刻，运维改完不重启不生效。
//   - Get* 调用与键名字面量必须写在同一行：scripts/check-config-param-readpoints.py
//     认的就是这个形状，把 Get* 藏进通用 helper 会让门禁判成「未接线」。
//   - 非法值（非正数、越界）一律回落 aiagent/llm 里的常量兜底，这层判断放在读取口
//     而不是这里，理由同前两批。
//
// 返回值是本批真正接上的键名列表，供启动日志与装配层测试断言，避免"接线函数写了
// 但少接一条"这种只有门禁能发现的退化。
func WireAgentLLMConfigParams() []string {
	bg := context.Background()
	cp := service.GlobalConfigParam()
	wired := make([]string, 0, 7)

	llmpkg.SetHealthCheckIntervalProvider(func() time.Duration {
		return cp.GetDuration(bg, "agent_llm", "default_health_check_interval", llmpkg.DefaultHealthCheckInterval)
	})
	wired = append(wired, "agent_llm.default_health_check_interval")

	llmpkg.SetFailureThresholdProvider(func() int {
		return cp.GetInt(bg, "agent_llm", "default_failure_threshold", llmpkg.DefaultFailureThreshold)
	})
	wired = append(wired, "agent_llm.default_failure_threshold")

	llmpkg.SetCircuitOpenDurationProvider(func() time.Duration {
		return cp.GetDuration(bg, "agent_llm", "default_circuit_open_duration", llmpkg.DefaultCircuitOpenDuration)
	})
	wired = append(wired, "agent_llm.default_circuit_open_duration")

	llmpkg.SetHealthCheckTimeoutProvider(func() time.Duration {
		return cp.GetDuration(bg, "agent_llm", "default_health_check_timeout", llmpkg.DefaultHealthCheckTimeout)
	})
	wired = append(wired, "agent_llm.default_health_check_timeout")

	llmpkg.SetVoteAgreementThresholdProvider(func() float64 {
		return cp.GetFloat(bg, "agent_llm", "vote_agreement_threshold", llmpkg.DefaultVoteAgreementThreshold)
	})
	wired = append(wired, "agent_llm.vote_agreement_threshold")

	llmpkg.SetDefaultHTTPTimeoutProvider(func() time.Duration {
		return cp.GetDuration(bg, "agent_llm", "default_http_timeout", llmpkg.DefaultLLMHTTPTimeout)
	})
	wired = append(wired, "agent_llm.default_http_timeout")

	llmpkg.SetDBSinkStopDeadlineProvider(func() time.Duration {
		return cp.GetDuration(bg, "agent_llm", "db_sink_stop_deadline", llmpkg.DefaultDBSinkStopDeadline)
	})
	wired = append(wired, "agent_llm.db_sink_stop_deadline")

	return wired
}
