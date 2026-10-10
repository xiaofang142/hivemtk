package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/cache"
)

// 这批测试是 2c 的重点：seam 测试只证明「provider 被读了」，行为测试证明
// 「读到的值真的作用到锁的生命周期上」。锁 TTL 参数化的风险恰恰在中间那一段——
// 接上了读取函数但某个读取点漏改，单测全绿而锁仍然按 24 小时过期。

func newBehaviorFixture(t *testing.T) (*InboxIngressService, cache.Cache) {
	t.Helper()
	mc := cache.NewMemoryCache()
	t.Cleanup(func() { mc.Close() })
	// db 传 nil：本组用例只碰 cache，不碰 DB；NewInboxIngressServiceWithDB 对
	// nil db 只把 repo 留空，锁路径不会走到 repo。
	svc := NewInboxIngressServiceWithDB(nil, mc)
	return svc, mc
}

func injectHumanLockTTL(t *testing.T, d time.Duration) {
	t.Helper()
	SetInboxHumanLockTTLProvider(func() time.Duration { return d })
	t.Cleanup(func() { SetInboxHumanLockTTLProvider(func() time.Duration { return InboxLockTTL }) })
}

func injectAIProcessingTTL(t *testing.T, d time.Duration) {
	t.Helper()
	SetAIProcessingTTLProvider(func() time.Duration { return d })
	t.Cleanup(func() { SetAIProcessingTTLProvider(func() time.Duration { return InboxAIProcessingTTL }) })
}

func injectPendingTTL(t *testing.T, d time.Duration) {
	t.Helper()
	SetInboxPendingTTLProvider(func() time.Duration { return d })
	t.Cleanup(func() { SetInboxPendingTTLProvider(func() time.Duration { return InboxPendingTTL }) })
}

func keyExists(t *testing.T, mc cache.Cache, key string) bool {
	t.Helper()
	ok, err := mc.Exists(context.Background(), key)
	if err != nil {
		t.Fatalf("Exists(%s) 失败: %v", key, err)
	}
	return ok
}

// TestHumanLockExpiresByConfiguredTTL 人工接管锁按配置值过期。
// 默认兜底是 24 小时，睡不过来；注入 60ms 后锁应当在两次 sleep 之间消失。
// 若 :227 的 cache.Set 仍写常量，这里会永久为 true 而卡在第一个检查点。
func TestHumanLockExpiresByConfiguredTTL(t *testing.T) {
	svc, mc := newBehaviorFixture(t)
	injectHumanLockTTL(t, 60*time.Millisecond)

	ctx := context.Background()
	if err := svc.LockSessionForHuman(ctx, "sess-human", "客户要投诉"); err != nil {
		t.Fatalf("LockSessionForHuman 失败: %v", err)
	}

	lockKey := InboxHumanLockKey + "sess-human"
	reasonKey := InboxHumanLockKey + "reason:sess-human"
	if !keyExists(t, mc, lockKey) {
		t.Fatal("刚接管完锁就应当存在")
	}
	if !keyExists(t, mc, reasonKey) {
		t.Fatal("接管理由键应当与锁一同写入")
	}

	// reason 键 :231 原先写裸 24h，与锁不同步；接线后两者应当同时过期。
	time.Sleep(110 * time.Millisecond)
	if keyExists(t, mc, lockKey) {
		t.Error("锁在配置的 TTL 之后仍然存在，说明 cache.Set 走的不是注入值")
	}
	if keyExists(t, mc, reasonKey) {
		t.Error("接管理由键在配置的 TTL 之后仍然存在，:231 的字面量没有被接线")
	}
}

// TestRenewHumanLockUsesConfiguredTTL RenewSessionHumanLock 的 ttl<=0 兜底分支。
// 该分支原本回落 InboxLockTTL（24h），接线后回落注入值；传 0 进去是唯一的触发方式。
func TestRenewHumanLockUsesConfiguredTTL(t *testing.T) {
	svc, mc := newBehaviorFixture(t)
	injectHumanLockTTL(t, 60*time.Millisecond)

	ctx := context.Background()
	if err := svc.RenewSessionHumanLock(ctx, "sess-renew", 0); err != nil {
		t.Fatalf("RenewSessionHumanLock 失败: %v", err)
	}
	// 显式传值仍然赢过参数中心：这是「调用方说了算」的最低保证。
	if err := svc.RenewSessionHumanLock(ctx, "sess-renew2", 2*time.Hour); err != nil {
		t.Fatalf("RenewSessionHumanLock(显式值) 失败: %v", err)
	}

	lockKey := InboxHumanLockKey + "sess-renew"
	if !keyExists(t, mc, lockKey) {
		t.Fatal("续期写完锁就应当存在")
	}
	time.Sleep(110 * time.Millisecond)
	if keyExists(t, mc, lockKey) {
		t.Error("用配置 TTL 续期的锁没有按时过期，:251 的兜底分支没接上")
	}

	explicitKey := InboxHumanLockKey + "sess-renew2"
	if !keyExists(t, mc, explicitKey) {
		t.Error("显式传入 2h 的锁应当不受参数中心影响，却在配置 TTL 内消失了")
	}
}

// TestAIProcessingFlagExpiresByConfiguredTTL AI 处理中标记过期后可重新获取。
// 这条是 2c 里最该有行为测试的一条：标记提前过期 = 推理期间的新消息被补触发
// （重复回复），标记永不过期 = AI 崩了之后该会话彻底哑掉。
func TestAIProcessingFlagExpiresByConfiguredTTL(t *testing.T) {
	svc, mc := newBehaviorFixture(t)
	injectAIProcessingTTL(t, 60*time.Millisecond)

	conv := "conv-ai"
	if !svc.markAIProcessing(context.Background(), conv) {
		t.Fatal("首次获取 AI 处理中标记应当成功")
	}
	if !keyExists(t, mc, InboxAIProcessingKey+conv) {
		t.Fatal("标记写入后应当存在")
	}

	// 未过期：第二次获取必须失败（这正是它防重复回复的机制）
	if svc.markAIProcessing(context.Background(), conv) {
		t.Error("标记未过期时重复获取竟然成功了，AI 排他闸失效")
	}

	time.Sleep(110 * time.Millisecond)
	if keyExists(t, mc, InboxAIProcessingKey+conv) {
		t.Error("标记在配置 TTL 之后仍存在，:600 的 SetNX 走的不是注入值")
	}
	if !svc.markAIProcessing(context.Background(), conv) {
		t.Error("标记过期后应当能重新获取，说明 TTL 被接上了但值不对")
	}
}

// TestPendingMessagesExpireByConfiguredTTL 防抖暂存队列按配置 TTL 过期。
// 写小的后果是「防抖窗口还没走完消息就消失」，客户那句话无投递痕迹地丢失。
func TestPendingMessagesExpireByConfiguredTTL(t *testing.T) {
	svc, mc := newBehaviorFixture(t)
	injectPendingTTL(t, 60*time.Millisecond)

	ctx := context.Background()
	sessionID := "sess-pending"
	if err := svc.AppendPendingMessage(ctx, sessionID, "客户说了一半的话"); err != nil {
		t.Fatalf("AppendPendingMessage 失败: %v", err)
	}
	if !keyExists(t, mc, InboxPendingKey+sessionID) {
		t.Fatal("刚写入的待处理队列应当存在")
	}

	time.Sleep(110 * time.Millisecond)
	got, err := svc.PopPendingMessages(ctx, sessionID)
	if err != nil {
		t.Fatalf("PopPendingMessages 失败: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("队列在配置 TTL 之后仍返回 %d 条，说明 TTL 没有生效", len(got))
	}
}

// TestPendingTTLLongEnoughKeepsMessage 反向钉住：TTL 足够长时要留下来。
// 少了这一条，把「立刻过期」当成功判据的实现也能让上面几条全绿。
func TestPendingTTLLongEnoughKeepsMessage(t *testing.T) {
	svc, _ := newBehaviorFixture(t)
	injectPendingTTL(t, time.Hour)

	ctx := context.Background()
	if err := svc.AppendPendingMessage(ctx, "sess-keep", "完整的一句话"); err != nil {
		t.Fatalf("AppendPendingMessage 失败: %v", err)
	}

	got, err := svc.PopPendingMessages(ctx, "sess-keep")
	if err != nil {
		t.Fatalf("PopPendingMessages 失败: %v", err)
	}
	if len(got) != 1 || got[0] != "完整的一句话" {
		t.Fatalf("TTL 足够长时消息必须原样保留，实际得到 %v", got)
	}
}

// TestContentDedupWindowUsesConfiguredTTL 内容去重窗口按注入值走。
// :144 原本 return 常量；contentDedupWindow 还有一条「结构体字段优先」的旧路径，
// 所以字段为零值时才落到 provider，这个先后次序不能反。
func TestContentDedupWindowUsesConfiguredTTL(t *testing.T) {
	svc, _ := newBehaviorFixture(t)
	SetInboxContentDedupTTLProvider(func() time.Duration { return 3 * time.Minute })
	t.Cleanup(func() {
		SetInboxContentDedupTTLProvider(func() time.Duration { return InboxContentDedupTTL })
	})

	if got := svc.contentDedupWindow(); got != 3*time.Minute {
		t.Fatalf("字段未注入时应读 provider，得到 %s", got)
	}

	// 结构体字段仍然优先：这是既有 unit test 依赖的行为，不能为了让参数生效而改掉。
	svc.contentDedupTTL = 7 * time.Minute
	if got := svc.contentDedupWindow(); got != 7*time.Minute {
		t.Fatalf("结构体字段非零时应当优先，得到 %s", got)
	}
}
