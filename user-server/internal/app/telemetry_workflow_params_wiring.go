package app

import (
	"context"
	"time"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/monitor"
	"hivemtk-user/internal/pkg/featureflag"
	appsvc "hivemtk-user/internal/service"
)

// WireTelemetryWorkflowConfigParams 把 config_params 的 telemetry 组与 workflow 组
// 接进八个点位。
//
// 分层口径：monitor / geo/service / pkg/featureflag / aiagent/llm 这四个包都不能
// 反向 import internal/service（它们在依赖链更底层），所以注入统一在装配层做；
// workflow 四条本来就在 internal/service 里，注入也放在这里，让「一组接线一个函数」
// 这个约定不被打断。
//
// 两条需要额外说明的：
//   - telemetry.trace_sink_buffer 与 agent_llm.db_sink_stop_deadline 同在
//     aiagent/llm 的 DBTraceSink 上。stop_deadline 管的是「排空缓冲等多久」，本条管的是
//     「缓冲多大」；前者到点强退、后者决定能存多少，方向相反、互不覆盖。
//   - telemetry.feature_flag_poll_interval 管的是 pkg/featureflag 的 env 热加载轮询；
//     internal/service/feature_flag.go 的 DB 灰度开关（feature_flags 表）走的是
//     另一套求值路径，两者不要混。
//
// Get* 调用与键名字面量必须写在同一行：scripts/check-config-param-readpoints.py
// 认的就是这个形状。
func WireTelemetryWorkflowConfigParams() []string {
	bg := context.Background()
	cp := appsvc.GlobalConfigParam()
	wired := make([]string, 0, 8)

	monitor.SetNodeHealthWindowProvider(func() time.Duration {
		return cp.GetDuration(bg, "telemetry", "node_health_window", monitor.DefaultNodeHealthWindow)
	})
	wired = append(wired, "telemetry.node_health_window")

	llm.SetDBSinkBufferSizeProvider(func() int {
		return cp.GetInt(bg, "telemetry", "trace_sink_buffer", llm.DefaultDBSinkBufferSize)
	})
	wired = append(wired, "telemetry.trace_sink_buffer")

	service.SetPositionDedupWindowProvider(func() int {
		return cp.GetInt(bg, "telemetry", "geo_position_window", service.DefaultPositionDedupWindow)
	})
	wired = append(wired, "telemetry.geo_position_window")

	featureflag.SetPollIntervalProvider(func() time.Duration {
		return cp.GetDuration(bg, "telemetry", "feature_flag_poll_interval", featureflag.DefaultPollInterval)
	})
	wired = append(wired, "telemetry.feature_flag_poll_interval")

	appsvc.SetMaxSubflowDepthProvider(func() int {
		return cp.GetInt(bg, "workflow", "max_subflow_depth", appsvc.DefaultMaxSubflowDepth)
	})
	wired = append(wired, "workflow.max_subflow_depth")

	appsvc.SetMaxWorkflowStepsProvider(func() int {
		return cp.GetInt(bg, "workflow", "max_workflow_steps", appsvc.DefaultMaxWorkflowSteps)
	})
	wired = append(wired, "workflow.max_workflow_steps")

	appsvc.SetMaxRunningPerSOPProvider(func() int {
		return cp.GetInt(bg, "workflow", "max_running_per_sop", appsvc.DefaultMaxRunningPerSOP)
	})
	wired = append(wired, "workflow.max_running_per_sop")

	appsvc.SetMaxExecutedNodeTraceProvider(func() int {
		return cp.GetInt(bg, "workflow", "max_executed_node_trace", appsvc.DefaultMaxExecutedNodeTrace)
	})
	wired = append(wired, "workflow.max_executed_node_trace")

	return wired
}
