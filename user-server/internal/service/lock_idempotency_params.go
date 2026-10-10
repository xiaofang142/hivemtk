package service

import "time"

// 锁与幂等 TTL 的参数化（阶段 2c，对应 config_params 的 lock 组）。
//
// 这一组与其它参数组的性质不同：它们是正确性参数而不是调优项。写错的结果是
// 死锁、并发重复落库或重复回复，所以每个取值都配了上下界注解，且统一走
// 「provider 注入 + 非正值回落兜底」的范式——非正值必须回落，因为
// time.Duration(0) 的锁语义是「永不过期」，那比任何写坏的数字都危险。
//
// 之所以把六个 provider 集中在这一个文件而不是散回各自的常量旁边：这些常量
// 分处 message_hub.go / inbox_ingress.go / inbox_ingress_ingest.go 三处，
// 散着写看不出「锁的世界观」全貌，将来加第二条锁时也容易漏接注入。
// 装配点集中在 internal/app/lock_idempotency_params_wiring.go。

// 兜底常量就是原有导出常量本身，不另起名字。理由：这些常量已被测试与下游
// 引用（如 inbox_ingress_recheck_test.go 用 InboxAIProcessingTTL），改名会把
// 一次参数化放大成一片 diff；保留原名，「常量 = 兜底、函数 = 可注入」的区分
// 由下面的读取函数承担。
var (
	inboxHumanLockTTLProvider    = func() time.Duration { return InboxLockTTL }
	inboxPendingTTLProvider      = func() time.Duration { return InboxPendingTTL }
	inboxContentDedupTTLProvider = func() time.Duration { return InboxContentDedupTTL }
	ingestLockTTLProvider        = func() time.Duration { return IngestLockTTL }
	aiProcessingTTLProvider      = func() time.Duration { return InboxAIProcessingTTL }
	messageHubIdemTTLProvider    = func() time.Duration { return MessageHubDefaultIdemTTL }
)

// SetInboxHumanLockTTLProvider 注入人工接管锁 TTL 读取函数。fn 为 nil 视为不注入
// （装配顺序出错时保留兜底值，而不是把锁打成零值）。
func SetInboxHumanLockTTLProvider(fn func() time.Duration) {
	if fn != nil {
		inboxHumanLockTTLProvider = fn
	}
}

// SetInboxPendingTTLProvider 注入待处理队列 TTL 读取函数。
func SetInboxPendingTTLProvider(fn func() time.Duration) {
	if fn != nil {
		inboxPendingTTLProvider = fn
	}
}

// SetInboxContentDedupTTLProvider 注入内容去重窗口读取函数。
func SetInboxContentDedupTTLProvider(fn func() time.Duration) {
	if fn != nil {
		inboxContentDedupTTLProvider = fn
	}
}

// SetIngestLockTTLProvider 注入入站落库锁 TTL 读取函数。
func SetIngestLockTTLProvider(fn func() time.Duration) {
	if fn != nil {
		ingestLockTTLProvider = fn
	}
}

// SetAIProcessingTTLProvider 注入 AI 处理中标记 TTL 读取函数。
func SetAIProcessingTTLProvider(fn func() time.Duration) {
	if fn != nil {
		aiProcessingTTLProvider = fn
	}
}

// SetMessageHubIdemTTLProvider 注入消息中台幂等 TTL 读取函数。
func SetMessageHubIdemTTLProvider(fn func() time.Duration) {
	if fn != nil {
		messageHubIdemTTLProvider = fn
	}
}

// inboxHumanLockTTL 人工接管锁的存活时间。接管理由键与之共用同一个参数：
// 理由先于锁过期会让一条被接管的会话显示不出「为什么被接管」，而理由比锁活得
// 久只会留下一段无主数据，两者都不值得单开一个参数。
func inboxHumanLockTTL() time.Duration {
	if v := inboxHumanLockTTLProvider(); v > 0 {
		return v
	}
	return InboxLockTTL
}

func inboxPendingTTL() time.Duration {
	if v := inboxPendingTTLProvider(); v > 0 {
		return v
	}
	return InboxPendingTTL
}

func inboxContentDedupTTL() time.Duration {
	if v := inboxContentDedupTTLProvider(); v > 0 {
		return v
	}
	return InboxContentDedupTTL
}

func ingestLockTTL() time.Duration {
	if v := ingestLockTTLProvider(); v > 0 {
		return v
	}
	return IngestLockTTL
}

func aiProcessingTTL() time.Duration {
	if v := aiProcessingTTLProvider(); v > 0 {
		return v
	}
	return InboxAIProcessingTTL
}

func messageHubIdemTTL() time.Duration {
	if v := messageHubIdemTTLProvider(); v > 0 {
		return v
	}
	return MessageHubDefaultIdemTTL
}

// Probe 系列：各读取函数在包内未导出，装配层 own_test 需要稳定的公开面来断言
// 「注入真的被读到了」。与 1.2 以来各批一致的做法。

func ProbeInboxHumanLockTTL() time.Duration { return inboxHumanLockTTL() }

func ProbeInboxPendingTTL() time.Duration { return inboxPendingTTL() }

func ProbeInboxContentDedupTTL() time.Duration { return inboxContentDedupTTL() }

func ProbeIngestLockTTL() time.Duration { return ingestLockTTL() }

func ProbeAIProcessingTTL() time.Duration { return aiProcessingTTL() }

func ProbeMessageHubIdemTTL() time.Duration { return messageHubIdemTTL() }
