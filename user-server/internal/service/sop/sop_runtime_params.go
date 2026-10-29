package sop

import (
	"context"
	"time"
)

// SOP 调度器与 Saga 补偿的参数（阶段 3.1 第三批，对应 config_params 的 sop 组）。
//
// 两组都取自 DefaultXxxConfig()，且都是**构造期读一次**：调度器的 worker 数、
// 队列 channel 容量、LLM 信号量在 NewSOPExecutionDispatcher 里 make(chan, N) /
// 起 goroutine 时定型；补偿管理器的 MaxPlansKept 决定的是内存里保留的计划表大小。
// 所以六条种子全部标 Restart=true，改完只对下次启动的进程生效。
//
// 装配路径：
//   - cmd/api/main.go:380 InitSOPExecutionDispatcher(sopSvc, nil) —— cfg 传 nil，
//     旧代码在这里回落 DefaultSOPDispatcherConfig()（里头是三个写死的 16/1000/4）。
//   - cmd/api/main.go:384 InitSOPCompensation(execDispatcher) —— 受
//     FF_LTC_SAGA_COMPENSATION 开关控制，默认关闭；开启时才构造管理器。
//
// 走直连而不是 provider seam：读取点全在 internal/service 包内（3.1 第一批
// geo.default_visibility_days 定的判据）。每键拆 raw / 带兜底两个函数的理由同
// webhook_runtime_params.go：读取点硬闸认的是「某行同时出现 Get*( 与键名常量」。

// ParamGroupSOP 是这一组在参数中心里的组名。
const ParamGroupSOP = "sop"

const (
	SOPDispatcherWorkerCountParam    = "dispatcher_worker_count"
	SOPDispatcherQueueCapacityParam  = "dispatcher_queue_capacity"
	SOPDispatcherLLMConcurrencyParam = "dispatcher_llm_concurrency"
	SOPCompensationMaxAttemptsParam  = "compensation_max_attempts"
	SOPCompensationTTLParam          = "compensation_ttl"
	SOPCompensationMaxPlansKeptParam = "compensation_max_plans_kept"
)

func positiveIntOrSOP(v, def int) int {
	if v > 0 {
		return v
	}
	return def
}

func positiveDurationOrSOP(v, def time.Duration) time.Duration {
	if v > 0 {
		return v
	}
	return def
}

// ── raw 读口：只读参数中心，读不到/非正数一律返回 0 ──

func sopDispatcherWorkerCountRaw(ctx context.Context) int {
	return globalParams().GetInt(ctx, ParamGroupSOP, SOPDispatcherWorkerCountParam, 0)
}

func sopDispatcherQueueCapacityRaw(ctx context.Context) int {
	return globalParams().GetInt(ctx, ParamGroupSOP, SOPDispatcherQueueCapacityParam, 0)
}

func sopDispatcherLLMConcurrencyRaw(ctx context.Context) int {
	return globalParams().GetInt(ctx, ParamGroupSOP, SOPDispatcherLLMConcurrencyParam, 0)
}

func sopCompensationMaxAttemptsRaw(ctx context.Context) int {
	return globalParams().GetInt(ctx, ParamGroupSOP, SOPCompensationMaxAttemptsParam, 0)
}

func sopCompensationTTLRaw(ctx context.Context) time.Duration {
	return globalParams().GetDuration(ctx, ParamGroupSOP, SOPCompensationTTLParam, 0)
}

func sopCompensationMaxPlansKeptRaw(ctx context.Context) int {
	return globalParams().GetInt(ctx, ParamGroupSOP, SOPCompensationMaxPlansKeptParam, 0)
}

// ── 带兜底的读口 ──
//
// 非正数一律回落常量。这三类没有"配 0 就照做"的合法语义：worker 0 = 没人取任务、
// 队列 0 = 每条节点同步执行、LLM 并发 0 = 一个都不放行、补偿次数 0 = 一次都不重试、
// 计划表 0 = 每条补偿计划写入即被淘汰、补偿 TTL 0 = context 立刻过期。

func sopDispatcherWorkerCount(ctx context.Context) int {
	return positiveIntOrSOP(sopDispatcherWorkerCountRaw(ctx), DefaultSOPDispatcherWorkerCount)
}

func sopDispatcherQueueCapacity(ctx context.Context) int {
	return positiveIntOrSOP(sopDispatcherQueueCapacityRaw(ctx), DefaultSOPDispatcherQueueCapacity)
}

func sopDispatcherLLMConcurrency(ctx context.Context) int {
	return positiveIntOrSOP(sopDispatcherLLMConcurrencyRaw(ctx), DefaultSOPDispatcherLLMConcurrency)
}

func sopCompensationMaxAttempts(ctx context.Context) int {
	return positiveIntOrSOP(sopCompensationMaxAttemptsRaw(ctx), DefaultCompensationMaxAttempts)
}

func sopCompensationTTL(ctx context.Context) time.Duration {
	return positiveDurationOrSOP(sopCompensationTTLRaw(ctx), DefaultCompensationPerTTL)
}

func sopCompensationMaxPlansKept(ctx context.Context) int {
	return positiveIntOrSOP(sopCompensationMaxPlansKeptRaw(ctx), DefaultCompensationMaxPlansKept)
}

// SOPDispatcherConfigFromParams 按参数中心（读不到则常量兜底）构造调度器配置。
func SOPDispatcherConfigFromParams(ctx context.Context) *SOPDispatcherConfig {
	return &SOPDispatcherConfig{
		WorkerCount:       sopDispatcherWorkerCount(ctx),
		QueueCapacity:     sopDispatcherQueueCapacity(ctx),
		LLMConcurrency:    sopDispatcherLLMConcurrency(ctx),
		MaxAttempts:       DefaultSOPDispatcherMaxAttempts,
		InitialBackoff:    DefaultSOPDispatcherInitialBackoff,
		MaxBackoff:        DefaultSOPDispatcherMaxBackoff,
		BackoffMultiplier: DefaultSOPDispatcherBackoffMultiplier,
	}
}

// CompensationConfigFromParams 同 SOPDispatcherConfigFromParams，用于补偿管理器。
func CompensationConfigFromParams(ctx context.Context) CompensationConfig {
	base := DefaultCompensationConfig()
	base.MaxAttempts = sopCompensationMaxAttempts(ctx)
	base.PerCompensationTTL = sopCompensationTTL(ctx)
	base.MaxPlansKept = sopCompensationMaxPlansKept(ctx)
	return base
}
