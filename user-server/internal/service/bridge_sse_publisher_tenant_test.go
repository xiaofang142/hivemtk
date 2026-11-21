// bridge_sse_publisher_tenant_test.go 钉住 SSE 推送回调的租户注入通道。
//
// 回归背景（2026-10-22 线上，SaaS 部署 TENANT_MODE=on）：SSE 下行推送前的
// "出站认领"（ClaimOutboundForPush）要 UPDATE message_hub 这张租户表，
// 而认领那一步拿到的 ctx 不带租户 ⇒ 被隔离守卫 fail-closed 拒绝，
// 现场每条都打：
//
//	[SSE] 出站认领报错，放弃本次推送（行仍待办，超时后重投）
//
// 表现是"SSE 看着推了，其实每条都靠超时重投兜着"，而且每次都多打一条 ERROR。
//
// 根因：GlobalSSEPublisher 的回调签名里**没有 ctx**，调用点在起 goroutine 前
// 把请求 ctx 整个丢掉了（goroutine 里的 ctx 还会随请求结束被取消），
// 于是 Publish → claimForPush 一路走的是 context.Background()。
//
// 修法：回调签名加 ctx，调用点在起 goroutine 前用 tenant.BackgroundWithTenant(ctx)
// 派生（保留租户归属、丢掉请求的取消信号），bus 侧改用 PublishCtx。
package service

import (
	"hivemtk-user/internal/capregistry"
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

// TestSSEScopedPublisher_ReceivesTenantBoundContext SSE 推送回调收到的 ctx 必须带租户。
//
// 判据是"回调收到的 ctx 里 tenant.FromContext 能读出那个租户"——
// 读不出就说明认领那一步仍会被隔离守卫拒绝。
func TestSSEScopedPublisher_ReceivesTenantBoundContext(t *testing.T) {
	setupBridgeWhitelistForTest(t, "douyin")
	testutil.NewTestDB(t, &model.MessageHub{})
	svc := NewInboxIngressService(capregistry.AuthorizeBridgeSubmission)
	SetGlobalInboxIngressService(svc)
	t.Cleanup(func() { SetGlobalInboxIngressService(nil) })

	const tenantID = uint(66)

	// 装一个记录者：把回调收到的 ctx 存下来。
	type received struct {
		tenantID uint
		bound    bool
	}
	got := make(chan received, 4)
	prev := GlobalSSEPublisher
	SetGlobalSSEPublisher(func(ctx context.Context, channel, accountID string, hubID uint64, convID, msgType, receiverID, content, mediaURL string, isAIReply bool, createdAt time.Time) {
		id, ok := tenant.FromContext(ctx)
		got <- received{tenantID: id, bound: ok}
	})
	t.Cleanup(func() { SetGlobalSSEPublisher(prev) })

	// 带租户的 ctx（与真实调用方一致：请求 ctx 已由 bindBridgeTenant 绑定）。
	ctx := localBridgeSubmitContext(t, tenant.WithTenantID(context.Background(), tenantID), "douyin", "acc_sse_1", "text")
	if err := DeliverBridgeOutbound(ctx, "douyin", "acc_sse_1", "conv_sse_1", "text", "SSE 租户上下文测试", "evt_sse_1"); err != nil {
		t.Fatalf("DeliverBridgeOutbound 失败: %v", err)
	}

	select {
	case r := <-got:
		if !r.bound {
			t.Fatal("SSE 回调收到的 ctx 未绑定租户：出站认领会 UPDATE message_hub（租户表），on 模式下必被隔离守卫 fail-closed 拒绝")
		}
		if r.tenantID != tenantID {
			t.Fatalf("SSE 回调收到的租户=%d want %d", r.tenantID, tenantID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("5 秒内没收到 SSE 回调：发布路径没走到")
	}
}

// TestSSEScopedPublisher_NoTenantDoesNotFallBackToDefault 无租户的 ctx 不得被绑到默认租户。
//
// on 模式下这条链路在**写库那一步**就已经被隔离守卫 fail-closed 拒绝
// （message_hub 是租户表，create 需要租户上下文）⇒ 回调根本不会被调用。
// 断言的是这个更强的性质：宁可不发，也不把消息落到默认租户去。
func TestSSEScopedPublisher_NoTenantDoesNotFallBackToDefault(t *testing.T) {
	prev := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(prev) })

	setupBridgeWhitelistForTest(t, "douyin")
	testutil.NewTestDB(t, &model.MessageHub{})
	svc := NewInboxIngressService()
	SetGlobalInboxIngressService(svc)
	t.Cleanup(func() { SetGlobalInboxIngressService(nil) })

	called := make(chan struct{}, 4)
	prevPub := GlobalSSEPublisher
	SetGlobalSSEPublisher(func(ctx context.Context, channel, accountID string, hubID uint64, convID, msgType, receiverID, content, mediaURL string, isAIReply bool, createdAt time.Time) {
		// 回调若被调用，租户必须已绑定（不得回落默认租户）。
		if id, ok := tenant.FromContext(ctx); !ok || id == tenant.DefaultTenantID {
			t.Errorf("SSE 回调收到了未绑定（或默认）租户的 ctx：tenant=%d bound=%v", id, ok)
		}
		called <- struct{}{}
	})
	t.Cleanup(func() { SetGlobalSSEPublisher(prevPub) })

	// 无租户的 ctx ⇒ 写库即被拒（fail-closed），回调不应被触发。
	err := DeliverBridgeOutbound(context.Background(), "douyin", "acc_sse_2", "conv_sse_2", "text", "无租户场景", "evt_sse_2")
	if err == nil {
		t.Fatal("无租户 ctx 下 DeliverBridgeOutbound 应当被隔离守卫拒绝（不得静默落到默认租户）")
	}
	select {
	case <-called:
		t.Fatal("写库已被拒绝，SSE 回调不应被触发")
	case <-time.After(500 * time.Millisecond):
		// 预期路径：写被拒 ⇒ 没有回调。
	}
}
