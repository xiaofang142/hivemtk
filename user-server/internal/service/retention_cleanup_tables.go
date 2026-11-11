package service

// retention_cleanup_tables.go CC-04 刀2：九表接入规格（八张高写入自观测/审计表
// + operation_logs 自动调度口径）。
//
// 窗口定档（逐表依据与种子 DefaultValue 同值；改一处要改两处）：
//   - api_logs 30d：引擎级每请求落一行、请求/响应体可达 16KB（MA A2 同源），
//     全仓零读端（纯引擎自观测），对齐 message_trace 掩码窗 30 天量级；
//   - llm_routing_logs 90d：报表/日报聚合读取窗均有界（controller 缺省 7d、
//     月度类 ≤90d），对齐 operation_log 缺省 90d；
//   - tool_call_audits 90d：W-6 工具审计落库链，审计面从宽；
//   - feature_flag_eval_logs 30d：纯自观测（ListEvalLogs 只读近期 limit）；
//   - layer_decision_logs 30d：路由决策自观测（Recent/GetByTraceID 近期排障）；
//   - handoff_decisions 90d：转派决策审计面从宽（转派链 cron 扫的是
//     customer_sessions，本表无活链读依赖）；
//   - trace_events 30d：LLM 调用批量落，trace 列表只展示近期 limit 50；
//   - platform_llm_usage_records 180d：PLT-01 读端已上线（2026-11-11 校准批
//     裁决），运营分析+审计对账长尾从宽；
//   - operation_logs 90d：对齐既有手动清理端点缺省 days=90（人工端点保留不动，
//     本规格只补自动调度）。
//
// Days 闭包必须以字面键调 GetInt 且同行 —— 读取点门
// （scripts/check-config-param-readpoints.py）只认「同一行既有字面键又有 Get*(」
// 的形状，经变量拼装的键名对门不可见。

import (
	"context"

	"hivemtk-user/internal/model"
)

func retentionTableSpecs() []RetentionSpec {
	return []RetentionSpec{
		{
			Table: model.APILog{}.TableName(), Model: model.APILog{}, TimeCol: "created_at",
			DefaultDays: 30,
			Days: func(ctx context.Context) int {
				return GlobalConfigParam().GetInt(ctx, "retention", "api_logs_days", 30)
			},
		},
		{
			Table: model.LLMRoutingLog{}.TableName(), Model: model.LLMRoutingLog{}, TimeCol: "created_at",
			DefaultDays: 90,
			Days: func(ctx context.Context) int {
				return GlobalConfigParam().GetInt(ctx, "retention", "llm_routing_logs_days", 90)
			},
		},
		{
			Table: model.ToolCallAudit{}.TableName(), Model: model.ToolCallAudit{}, TimeCol: "created_at",
			DefaultDays: 90,
			Days: func(ctx context.Context) int {
				return GlobalConfigParam().GetInt(ctx, "retention", "tool_call_audits_days", 90)
			},
		},
		{
			Table: model.FeatureFlagEvalLog{}.TableName(), Model: model.FeatureFlagEvalLog{}, TimeCol: "created_at",
			DefaultDays: 30,
			Days: func(ctx context.Context) int {
				return GlobalConfigParam().GetInt(ctx, "retention", "feature_flag_eval_logs_days", 30)
			},
		},
		{
			Table: model.LayerDecisionLog{}.TableName(), Model: model.LayerDecisionLog{}, TimeCol: "created_at",
			DefaultDays: 30,
			Days: func(ctx context.Context) int {
				return GlobalConfigParam().GetInt(ctx, "retention", "layer_decision_logs_days", 30)
			},
		},
		{
			Table: model.HandoffDecisionRecord{}.TableName(), Model: model.HandoffDecisionRecord{}, TimeCol: "created_at",
			DefaultDays: 90,
			Days: func(ctx context.Context) int {
				return GlobalConfigParam().GetInt(ctx, "retention", "handoff_decisions_days", 90)
			},
		},
		{
			// trace_events 无 CreatedAt 字段，度量列是事件时刻 timestamp。
			Table: model.TraceEvent{}.TableName(), Model: model.TraceEvent{}, TimeCol: "timestamp",
			DefaultDays: 30,
			Days: func(ctx context.Context) int {
				return GlobalConfigParam().GetInt(ctx, "retention", "trace_events_days", 30)
			},
		},
		{
			// PLT-01 读端已上线，180 天从宽定档（审计/对账长尾需要，2026-11-11 校准批裁决）。
			Table: model.PlatformLLMUsageRecord{}.TableName(), Model: model.PlatformLLMUsageRecord{}, TimeCol: "created_at",
			DefaultDays: 180,
			Days: func(ctx context.Context) int {
				return GlobalConfigParam().GetInt(ctx, "retention", "platform_llm_usage_records_days", 180)
			},
		},
		{
			// operation_logs 自动调度口径：repo.DeleteOldLogs 仍是手动端点
			// （controller/operation_log.go CleanLogs）的路径，本规格只补周期调度，
			// 人工端点与 repo 方法零改动。
			Table: model.OperationLog{}.TableName(), Model: model.OperationLog{}, TimeCol: "created_at",
			DefaultDays: 90,
			Days: func(ctx context.Context) int {
				return GlobalConfigParam().GetInt(ctx, "retention", "operation_logs_days", 90)
			},
		},
	}
}
