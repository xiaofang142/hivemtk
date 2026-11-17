package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/tracing"
)

type debounceTenantCall struct {
	tenantID uint
	content  string
	carrier  *tracing.Carrier
	canceled bool
	system   bool
}

type debounceTenantTrigger struct{ calls chan debounceTenantCall }

func (s *debounceTenantTrigger) TriggerInboundAI(ctx context.Context, channel, accountID, conversationID, customerID, content, eventID string, opts ...TriggerInboundOption) {
	id, _ := tenant.FromContext(ctx)
	s.calls <- debounceTenantCall{
		tenantID: id, content: content, carrier: tracing.CarrierFromContext(ctx),
		canceled: ctx.Err() != nil, system: tenant.IsSystemScope(ctx),
	}
}

func TestAIDebounceTenant_CallbackRetainsIsolatedScope(t *testing.T) {
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previous) })
	mem := cache.NewMemoryCache()
	t.Cleanup(mem.Close)
	svc := NewInboxIngressService()
	svc.cache = mem
	svc.hubRepo = nil
	trigger := &debounceTenantTrigger{calls: make(chan debounceTenantCall, 2)}
	svc.aiTrigger = trigger
	parents := make(map[uint]*tracing.Carrier)
	for _, id := range []uint{18, 19} {
		parent := &tracing.Carrier{TenantID: id, TraceID: "debounce-trace-" + string(rune('a'+id)), Channel: "xiaohongshu", AccountID: "acc-1", ConversationID: "shared-session-conv", MsgID: "first", Direction: "inbound", AnswerAttempt: 1}
		parents[id] = parent
		ctx, cancel := context.WithCancel(tracing.WithCarrier(tenant.OnlyTenant(context.Background(), id), parent))
		svc.triggerAIWithDebounce(ctx, debounceEvent("shared-session", "第一条", 1))
		svc.triggerAIWithDebounce(ctx, debounceEvent("shared-session", "第二条", 1))
		cancel()
	}
	seen := make(map[uint]bool)
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for range 2 {
		select {
		case call := <-trigger.calls:
			if call.tenantID != 18 && call.tenantID != 19 {
				t.Fatalf("回调必须保留确定租户，实际%d", call.tenantID)
			}
			if seen[call.tenantID] {
				t.Fatal("同租户合并窗口不能重复触发")
			}
			seen[call.tenantID] = true
			if call.content != "第一条\n第二条" {
				t.Fatalf("必须只合并本租户两条消息，实际%q", call.content)
			}
			parent := parents[call.tenantID]
			if call.carrier == nil || call.carrier == parent || call.carrier.TraceID != parent.TraceID || call.carrier.TenantID != call.tenantID {
				t.Fatal("回调必须携带独立且同租户的追踪副本")
			}
			if call.canceled || call.system {
				t.Fatal("已入窗处理不继承请求取消或系统豁免")
			}
		case <-deadline.C:
			t.Fatal("两个租户各自的合并回调应在窗口后执行")
		}
	}
}

func TestAIDebounceTenant_UnconfirmedScopeCannotTrigger(t *testing.T) {
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previous) })
	contexts := []struct {
		name string
		ctx  context.Context
	}{
		{name: "无上下文", ctx: nil},
		{name: "未绑定租户", ctx: context.Background()},
		{name: "系统豁免", ctx: tenant.WithSystemScope(tenant.OnlyTenant(context.Background(), 18))},
		{name: "追踪租户冲突", ctx: tracing.WithCarrier(tenant.OnlyTenant(context.Background(), 18), &tracing.Carrier{TenantID: 19})},
	}
	for _, tc := range contexts {
		t.Run(tc.name, func(t *testing.T) {
			svc, stub, session := newDebounceFixture(t, 0)
			svc.hubRepo = nil
			svc.triggerAIWithDebounce(tc.ctx, debounceEvent(session, "普通咨询", 0))
			if stub.count() != 0 {
				t.Fatal("租户归属不明确时不能触发AI，即使明确关闭合并窗")
			}
		})
	}
}
