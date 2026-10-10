package service

import (
	"context"
	"os"
	"strconv"
	"time"
)

// webhook 组的并发、限流与重试参数（阶段 3.1b，对应 config_params 的 webhook 组）。
//
// 七个常量原先散在 webhook_dedup.go / webhook.go / webhook_ai.go 三处，且本已有一层
// env 覆盖（webhookEnvInt）。本文件把参数中心插到 env 与常量之间：
//
//	env > config_params(webhook 组) > 常量兜底
//
// env 保留为部署层应急开关：改 env 必须重启进程，参数中心改完即生效（构造期读的那三条
// 例外，见下）。日常调参走参数中心，临时顶一把才动 env。
//
// 生效时机分两类，写种子时按 Restart 区分：
//   - 每请求读：dedup_ttl（去重键 SetNX）、rate_limit / rate_burst（令牌桶按 key 懒构造，
//     桶用完即丢，下一个 key 就是新值）、max_retries（两条重试循环）。改完下一条消息
//     就按新值。
//   - 构造期读：worker_count / queue_size / reply_concurrency。NewWebhookService 里
//     make(chan, N) 与 startWorkers 起的 goroutine 数只在装配时算一次，改完只对下次
//     启动的服务实例生效，故种子标 Restart=true。
//
// 走直连而不是 provider seam：七个读取点全在 internal/service 包内，本就看得见
// GlobalConfigParam()。3.1a 定的接线判据就是「读取点在不在 internal/service 包内」。
//
// 每个键拆成 raw / 带兜底两个函数，不是为了对称：raw 那一行必须同时出现
// `GetDuration(`/`GetInt(` 与键名常量，参数中心读取点门禁（scripts/
// check-config-param-readpoints.py）认的就是这个形状，读点藏在通用 helper 的形参里
// 它判不出来（本批第一版就是这么写的，6 条一起被判成 UNDECLARED）。

// ParamGroupWebhook 是这一组在参数中心里的组名。
const ParamGroupWebhook = "webhook"

// 七个参数键。抽成常量而不是在调用点写字面量：1.8 栽过一次——channelgw/ws.go 把种子
// 登记的 "ws_push_interval" 写成 "ws_push_interval_default"，每次读都 miss、每次回落
// 兜底，而读取点门禁结构上看不出这类漂移（读取点存在，只是用的别的 key）。
const (
	WebhookDedupTTLParam         = "dedup_ttl"
	WebhookRateLimitParam        = "rate_limit"
	WebhookRateBurstParam        = "rate_burst"
	WebhookMaxRetriesParam       = "max_retries"
	WebhookWorkerCountParam      = "worker_count"
	WebhookQueueSizeParam        = "queue_size"
	WebhookReplyConcurrencyParam = "reply_concurrency"
)

func positiveDurationOr(v, def time.Duration) time.Duration {
	if v > 0 {
		return v
	}
	return def
}

func positiveIntOr(v, def int) int {
	if v > 0 {
		return v
	}
	return def
}

// ── raw 读口：只读参数中心，读不到/非正数一律返回 0，不带兜底 ──

func webhookDedupTTLRaw(ctx context.Context) time.Duration {
	return GlobalConfigParam().GetDuration(ctx, ParamGroupWebhook, WebhookDedupTTLParam, 0)
}

func webhookRateLimitRaw(ctx context.Context) int {
	return GlobalConfigParam().GetInt(ctx, ParamGroupWebhook, WebhookRateLimitParam, 0)
}

func webhookRateBurstRaw(ctx context.Context) int {
	return GlobalConfigParam().GetInt(ctx, ParamGroupWebhook, WebhookRateBurstParam, 0)
}

func webhookMaxRetriesRaw(ctx context.Context) int {
	return GlobalConfigParam().GetInt(ctx, ParamGroupWebhook, WebhookMaxRetriesParam, 0)
}

func webhookWorkerCountRaw(ctx context.Context) int {
	return GlobalConfigParam().GetInt(ctx, ParamGroupWebhook, WebhookWorkerCountParam, 0)
}

func webhookQueueSizeRaw(ctx context.Context) int {
	return GlobalConfigParam().GetInt(ctx, ParamGroupWebhook, WebhookQueueSizeParam, 0)
}

func webhookReplyConcurrencyRaw(ctx context.Context) int {
	return GlobalConfigParam().GetInt(ctx, ParamGroupWebhook, WebhookReplyConcurrencyParam, 0)
}

// ── 带兜底的读口 ──
//
// 非正数一律回落常量。这一组全是数量/速率语义，0 或负数在每种语义下都是写坏了：
// 并发 0 = 一个 worker 都不起、队列 0 = 每条消息同步处理、速率 0 = 全拒、
// burst 0 = 一条都放不进去、去重 0 = 之后所有重试都被判重复而丢掉。

func webhookDedupTTL(ctx context.Context) time.Duration {
	return positiveDurationOr(webhookDedupTTLRaw(ctx), WebhookDedupTTL)
}

func webhookRateLimit(ctx context.Context) int {
	return positiveIntOr(webhookRateLimitRaw(ctx), WebhookRateLimit)
}

func webhookRateBurst(ctx context.Context) int {
	return positiveIntOr(webhookRateBurstRaw(ctx), WebhookRateBurst)
}

func webhookMaxRetries(ctx context.Context) int {
	return positiveIntOr(webhookMaxRetriesRaw(ctx), WebhookMaxRetries)
}

// webhookIntAfterEnv 在既有 env 覆盖之下补一层参数中心。
// env 是空串或非法值时不短路整条链（原 webhookEnvInt 已如此）：否则一个残留的空 env
// 变量会把运维在参数中心刚调好的值盖掉。
func webhookIntAfterEnv(ctx context.Context, envKey string, paramValue, def int) int {
	if v := os.Getenv(envKey); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return positiveIntOr(paramValue, def)
}

func webhookWorkerCount(ctx context.Context) int {
	return webhookIntAfterEnv(ctx, "WEBHOOK_WORKER_COUNT",
		webhookWorkerCountRaw(ctx), WebhookWorkerCount)
}

func webhookQueueSize(ctx context.Context) int {
	return webhookIntAfterEnv(ctx, "WEBHOOK_QUEUE_SIZE",
		webhookQueueSizeRaw(ctx), WebhookQueueSize)
}

func webhookReplyConcurrency(ctx context.Context) int {
	return webhookIntAfterEnv(ctx, "WEBHOOK_REPLY_CONCURRENCY",
		webhookReplyConcurrencyRaw(ctx), WebhookReplyConcurrency)
}
