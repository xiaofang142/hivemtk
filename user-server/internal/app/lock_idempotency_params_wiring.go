package app

import (
	"context"
	"time"

	"hivemtk-user/internal/service"
)

// 锁与幂等 TTL 的参数装配（阶段 2c）。
//
// 这一组的 explicit-closure 注入与前八批一致，特殊之处只在于：6 条全是 duration
// 且 5 条热改、1 条需重启，所以每条都选对了取值侧口径——
//   - 锁 TTL 在每次 cache.Set/SetNX 时读，改完对新建的锁立刻生效；
//   - message_hub_idem_ttl 在账号服务构造期读一次并存在字段上，改完只影响
//     之后新构造的账号，所以种子标了 Restart: true，Description 也写明这一点。
//     这里仍然照常注入：不注入则连重启后的新账号也拿不到配置值。
func WireLockIdempotencyConfigParams() []string {
	cp := service.GlobalConfigParam()
	var wired []string
	// Provider 是延迟读的，捕获装配时的 ctx 会随请求取消一起失效，统一用后台 ctx。
	bg := context.Background()

	service.SetInboxHumanLockTTLProvider(func() time.Duration {
		return cp.GetDuration(bg, "lock", "inbox_human_ttl", service.InboxLockTTL)
	})
	wired = append(wired, "lock.inbox_human_ttl")

	service.SetInboxPendingTTLProvider(func() time.Duration {
		return cp.GetDuration(bg, "lock", "inbox_pending_ttl", service.InboxPendingTTL)
	})
	wired = append(wired, "lock.inbox_pending_ttl")

	service.SetInboxContentDedupTTLProvider(func() time.Duration {
		return cp.GetDuration(bg, "lock", "inbox_content_dedup_ttl", service.InboxContentDedupTTL)
	})
	wired = append(wired, "lock.inbox_content_dedup_ttl")

	service.SetIngestLockTTLProvider(func() time.Duration {
		return cp.GetDuration(bg, "lock", "ingest_lock_ttl", service.IngestLockTTL)
	})
	wired = append(wired, "lock.ingest_lock_ttl")

	service.SetAIProcessingTTLProvider(func() time.Duration {
		return cp.GetDuration(bg, "lock", "ai_processing_ttl", service.InboxAIProcessingTTL)
	})
	wired = append(wired, "lock.ai_processing_ttl")

	service.SetMessageHubIdemTTLProvider(func() time.Duration {
		return cp.GetDuration(bg, "lock", "message_hub_idem_ttl", service.MessageHubDefaultIdemTTL)
	})
	wired = append(wired, "lock.message_hub_idem_ttl")

	return wired
}
