// lead_mining_tenant_scope_test.go 钉住线索发掘 worker 的租户注入通道。
//
// 回归背景（2026-10-22 线上，SaaS 部署 TENANT_MODE=on）：每条桥接入站消息稳定打出
//
//	[lead-mining] 读取配置失败: [tenant] 表 lead_mining_configs 需要租户上下文但 ctx 未绑定（query）
//
// 业务后果是整条线索发掘链路静默不跑（读配置读不到 ⇒ 直接 return，无异常、无产出）。
// 根因：worker 的 ctx 来自 context.Background()（无租户），而 lead_mining_configs /
// message_hub / clues 都是租户表，on 模式下未绑定租户即被隔离守卫 fail-closed 拒绝。
// 修法：按行自证 —— MessageHub 是租户表、落库时已由隔离 callback 填好 tenant_id，
// worker 用它重新绑定 ctx（tenant.OnlyTenant）。
package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

// tenantScopeCfgRepo 记录 GetSingleton 收到的 ctx 里带的是哪个租户。
type tenantScopeCfgRepo struct {
	seenTenant uint
	seenBound  bool
	calls      int
}

func (f *tenantScopeCfgRepo) GetSingleton(ctx context.Context) (*model.LeadMiningConfig, error) {
	f.calls++
	f.seenTenant, f.seenBound = tenant.FromContext(ctx)
	return &model.LeadMiningConfig{ID: 1, MinIntentScore: 50, Enabled: true}, nil
}

func (f *tenantScopeCfgRepo) Save(context.Context, *model.LeadMiningConfig) error { return nil }

// TestLeadMiningProcess_BindsTenantFromMessage 线索发掘读配置时，ctx 必须已绑定
// 该条消息所属的租户 —— 回归前恒为"未绑定"，被隔离守卫拒绝。
func TestLeadMiningProcess_BindsTenantFromMessage(t *testing.T) {
	prev := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(prev) })

	cfgRepo := &tenantScopeCfgRepo{}
	svc := &Service{
		judge:          &fakeJudge{resp: &LeadJudgement{IsLead: true, IntentScore: 90}},
		historyFetcher: cannedHistory(),
		custRepo:       newFakeCustRepo(),
		clueRepo:       newFakeClueRepo(),
		cfgRepo:        cfgRepo,
		lastJudge:      map[string]time.Time{},
	}

	hub := sampleHub("douyin", "sender-scope-1", "我想了解一下报价")
	hub.TenantID = 42

	// worker 起的 ctx 没有任何租户（这正是回归前的形态）。
	svc.process(context.Background(), hub)

	if cfgRepo.calls == 0 {
		t.Fatal("配置仓储未被调用：线索发掘在读配置前就返回了")
	}
	if !cfgRepo.seenBound {
		t.Fatalf("读配置时 ctx 未绑定租户（隔离守卫会 fail-closed 拒绝，线索发掘整条链路静默不跑）")
	}
	if cfgRepo.seenTenant != 42 {
		t.Fatalf("读配置时的租户=%d want 42（须按消息行的 tenant_id 自证，不能回落默认租户）", cfgRepo.seenTenant)
	}
}

// TestLeadMiningProcess_NoTenantKeepsFailClosed 消息行没有 tenant_id 时必须在配置读取前拒绝，
// 不把缺失归属交给仓储或隔离守卫兜底，也不得回落默认租户。
func TestLeadMiningProcess_NoTenantKeepsFailClosed(t *testing.T) {
	prev := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(prev) })

	cfgRepo := &tenantScopeCfgRepo{}
	svc := &Service{
		judge:          &fakeJudge{resp: &LeadJudgement{IsLead: true, IntentScore: 90}},
		historyFetcher: cannedHistory(),
		custRepo:       newFakeCustRepo(),
		clueRepo:       newFakeClueRepo(),
		cfgRepo:        cfgRepo,
		lastJudge:      map[string]time.Time{},
	}

	hub := sampleHub("douyin", "sender-scope-2", "我想了解一下报价")
	hub.TenantID = 0

	svc.process(context.Background(), hub)

	if cfgRepo.calls != 0 {
		t.Fatalf("缺少 tenant_id 的消息必须在配置读取前拒绝，实际调用 %d 次", cfgRepo.calls)
	}
	if cfgRepo.seenBound {
		t.Fatalf("无 tenant_id 的消息不得被绑到任何租户，实际绑到 %d", cfgRepo.seenTenant)
	}
}
