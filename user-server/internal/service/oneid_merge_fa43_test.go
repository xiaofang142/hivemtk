package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/identity"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// fa43DB 建出 MergeCustomers 事务改写的全部表 + 规则持久化的 system_config_kv。
func fa43DB(t *testing.T) *gorm.DB {
	t.Helper()
	database := testutil.NewTestDB(t,
		&model.SystemConfigKV{},
		&model.ContactConsentIdentityLink{},
		&model.Customer{},
		&model.CustomerSession{},
		&model.CustomerEvent{},
		&model.CustomerChannel{},
		&model.CustomerDoNotContact{},
		&model.CSATSurvey{},
		&model.Clue{},
		&model.ScriptExposureLog{},
		&model.Order{},
		&model.OperationLog{},
	)
	db.SetTestDB(database)
	return database
}

func fa43Rules(t *testing.T, ctx context.Context) *MergeRuleSet {
	t.Helper()
	rules, err := NewOneIDMergeRuleService().GetRules(ctx)
	if err != nil {
		t.Fatalf("GetRules: %v", err)
	}
	return rules
}

// 规则按租户持久化 + 版本化：A/B 相反规则互不覆盖；重启（新 service 实例）
// 仍按各自版本运行，不是"同进程保存后 GET 相同"的假验收。
func TestFA43_RulesTenantScopedVersioned(t *testing.T) {
	fa43DB(t)
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	ctxA := tenant.WithTenantID(context.Background(), 101)
	ctxB := tenant.WithTenantID(context.Background(), 202)

	setA := fa43Rules(t, ctxA)
	if setA.Version != 0 {
		t.Fatalf("未配置租户版本应为 0，实际 %d", setA.Version)
	}
	setA.Strategy.AutoMergeEnabled = false
	setA.Custom = []MergeRule{{ID: 101, Name: "A-only", Field: "email", Op: "like", Priority: 10, Enabled: true}}
	savedA, err := NewOneIDMergeRuleService().SaveRules(ctxA, setA)
	if err != nil {
		t.Fatalf("SaveRules A: %v", err)
	}
	if savedA.Version != 1 {
		t.Fatalf("首次保存版本应为 1，实际 %d", savedA.Version)
	}
	if _, err := NewOneIDMergeRuleService().SaveRules(ctxA, savedA); err != nil {
		t.Fatalf("SaveRules A again: %v", err)
	}

	setB := fa43Rules(t, ctxB)
	if setB.Version != 0 || len(setB.Custom) != 0 {
		t.Fatalf("租户 B 不应读到 A 的规则版本/自定义项: %+v", setB)
	}
	// 用 wechat_open_id（内置规则未覆盖该字段）避免与内置"同手机号合并"抢占命中。
	setB.Custom = []MergeRule{{ID: 202, Name: "B-only", Field: "wechat_open_id", Op: "eq", Priority: 10, Enabled: true}}
	savedB, err := NewOneIDMergeRuleService().SaveRules(ctxB, setB)
	if err != nil {
		t.Fatalf("SaveRules B: %v", err)
	}
	if savedB.Version != 1 {
		t.Fatalf("租户 B 首次保存版本应为 1，实际 %d", savedB.Version)
	}

	// 双副本/重启：全新 service 实例各自读回自己的版本与规则。
	freshA := fa43Rules(t, ctxA)
	if freshA.Version != 2 || freshA.Strategy.AutoMergeEnabled || len(freshA.Custom) != 1 || freshA.Custom[0].Name != "A-only" {
		t.Fatalf("租户 A 重启后版本/规则不符: %+v", freshA)
	}
	freshB := fa43Rules(t, ctxB)
	if freshB.Version != 1 || !freshB.Strategy.AutoMergeEnabled || len(freshB.Custom) != 1 || freshB.Custom[0].Name != "B-only" {
		t.Fatalf("租户 B 重启后版本/规则不符: %+v", freshB)
	}

	// 相反规则按各自租户生效：A 的 like-email 命中、B 的 eq-phone 命中，互不影响。
	svcA := NewOneIDMergeRuleService()
	hitA, nameA, err := svcA.ApplyRules(ctxA,
		map[string]string{"email": "alice@example.com"},
		map[string]string{"email": "alice@example.com.cn"})
	if err != nil || !hitA || nameA != "A-only" {
		t.Fatalf("A 自定义 like 规则未命中: hit=%v name=%s err=%v", hitA, nameA, err)
	}
	hitB, nameB, err := NewOneIDMergeRuleService().ApplyRules(ctxB,
		map[string]string{"wechat_open_id": "wxid-b-002"},
		map[string]string{"wechat_open_id": "wxid-b-002"})
	if err != nil || !hitB || nameB != "B-only" {
		t.Fatalf("B 自定义 eq 规则未命中: hit=%v name=%s err=%v", hitB, nameB, err)
	}
	if hitB2, _, _ := NewOneIDMergeRuleService().ApplyRules(ctxA,
		map[string]string{"wechat_open_id": "wxid-b-002"}, map[string]string{"wechat_open_id": "wxid-b-002"}); hitB2 {
		t.Fatal("租户 A 不应命中租户 B 的 eq 规则")
	}
}

// 执行窗口按传入时刻判定（避免依赖测试跑起来的真实钟点）。
func TestFA43_MergeWindow(t *testing.T) {
	fa43DB(t)
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	ctx := tenant.WithTenantID(context.Background(), 303)

	rules := fa43Rules(t, ctx)
	rules.Strategy.AutoMergeEnabled = true
	rules.Strategy.WindowStart, rules.Strategy.WindowEnd = "02:00", "05:00"
	if _, err := NewOneIDMergeRuleService().SaveRules(ctx, rules); err != nil {
		t.Fatalf("SaveRules: %v", err)
	}
	svc := NewOneIDMergeRuleService()
	in, err := svc.CanAutoMerge(ctx, time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC))
	if err != nil || !in {
		t.Fatalf("窗口内应允许自动合并: in=%v err=%v", in, err)
	}
	out, err := svc.CanAutoMerge(ctx, time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC))
	if err != nil || out {
		t.Fatalf("窗口外应拒绝自动合并: out=%v err=%v", out, err)
	}

	// 跨午夜窗口 22:00–02:00。
	rules.Strategy.WindowStart, rules.Strategy.WindowEnd = "22:00", "02:00"
	if _, err := NewOneIDMergeRuleService().SaveRules(ctx, rules); err != nil {
		t.Fatalf("SaveRules 跨午夜: %v", err)
	}
	late, _ := svc.CanAutoMerge(ctx, time.Date(2026, 1, 1, 23, 0, 0, 0, time.UTC))
	noon, _ := svc.CanAutoMerge(ctx, time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	if !late || noon {
		t.Fatalf("跨午夜窗口判定错误: late=%v noon=%v", late, noon)
	}
}

// 真实合并链消费规则：总闸关闭 → 不合并；冲突需审核 → 不自动合并；
// 开启且 auto_merge → 按命中规则合并。每租户各按自己的版本运行。
func TestFA43_RealMergeConsumesRules(t *testing.T) {
	database := fa43DB(t)
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	ctxA := tenant.WithTenantID(context.Background(), 101)
	ctxB := tenant.WithTenantID(context.Background(), 202)
	ctxC := tenant.WithTenantID(context.Background(), 303)

	repo := repository.NewCustomerRepository()
	seedPair := func(ctx context.Context, phone string) {
		// 显式给不同 UnifiedID：BeforeCreate 会按 phone 自动生成同名 OneID，
		// 两个同手机号客户会撞 uniqueIndex；真实待合并重复档也应各自带独立 OneID。
		for i, c := range []*model.Customer{
			{Phone: phone, Tags: "[]", ChurnRisk: "low"},
			{Phone: phone, Email: "sec-" + phone + "@example.com", Tags: "[]", ChurnRisk: "low"},
		} {
			c.UnifiedID = fmt.Sprintf("uid-%s-%d", phone, i)
			if err := repo.Create(ctx, c); err != nil {
				t.Fatalf("create customer: %v", err)
			}
		}
	}
	seedPair(ctxA, "13900001001")
	seedPair(ctxB, "13900001002")
	seedPair(ctxC, "13900001003")

	// A：总闸开启 + auto_merge + 全天窗口 → 命中手机号内置规则即合并。
	rulesA := fa43Rules(t, ctxA)
	rulesA.Strategy.AutoMergeEnabled = true
	rulesA.Strategy.ConflictBehavior = "auto_merge"
	rulesA.Strategy.WindowStart, rulesA.Strategy.WindowEnd = "", ""
	if _, err := NewOneIDMergeRuleService().SaveRules(ctxA, rulesA); err != nil {
		t.Fatalf("SaveRules A: %v", err)
	}
	// B：总闸关闭 → 不合并。
	rulesB := fa43Rules(t, ctxB)
	rulesB.Strategy.AutoMergeEnabled = false
	if _, err := NewOneIDMergeRuleService().SaveRules(ctxB, rulesB); err != nil {
		t.Fatalf("SaveRules B: %v", err)
	}
	// C：冲突需审核 → 跳过自动合并。
	rulesC := fa43Rules(t, ctxC)
	rulesC.Strategy.AutoMergeEnabled = true
	rulesC.Strategy.ConflictBehavior = "queue_review"
	rulesC.Strategy.WindowStart, rulesC.Strategy.WindowEnd = "", ""
	if _, err := NewOneIDMergeRuleService().SaveRules(ctxC, rulesC); err != nil {
		t.Fatalf("SaveRules C: %v", err)
	}

	svc := NewCustomerIdentityService()
	for _, tc := range []struct {
		ctx   context.Context
		phone string
	}{
		{ctxA, "13900001001"}, {ctxB, "13900001002"}, {ctxC, "13900001003"},
	} {
		if _, err := svc.MergeByIdentity(tc.ctx, identity.Identifiers{Phone: tc.phone}); err != nil {
			t.Fatalf("MergeByIdentity(%s): %v", tc.phone, err)
		}
	}

	count := func(ctx context.Context, phone string) int64 {
		var n int64
		if err := database.WithContext(ctx).Model(&model.Customer{}).Where("phone = ?", phone).Count(&n).Error; err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}
	if got := count(ctxA, "13900001001"); got != 1 {
		t.Fatalf("A 应自动合并为 1 个客户，实际 %d", got)
	}
	if got := count(ctxB, "13900001002"); got != 2 {
		t.Fatalf("B 总闸关闭应保留 2 个客户，实际 %d", got)
	}
	if got := count(ctxC, "13900001003"); got != 2 {
		t.Fatalf("C 冲突需审核应保留 2 个客户，实际 %d", got)
	}
}

// 主客户策略按版本生效：latest_active 取最近更新者作为主档（保留其 UnifiedID）。
func TestFA43_PrimaryStrategyLatestActive(t *testing.T) {
	fa43DB(t)
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	ctx := tenant.WithTenantID(context.Background(), 404)

	rules := fa43Rules(t, ctx)
	rules.Strategy.PrimaryRule = "latest_active"
	if _, err := NewOneIDMergeRuleService().SaveRules(ctx, rules); err != nil {
		t.Fatalf("SaveRules: %v", err)
	}
	old := &model.Customer{Phone: "13900001004", UnifiedID: "uid-1004-old", Name: "old", Tags: "[]", ChurnRisk: "low"}
	fresh := &model.Customer{Phone: "13900001004", UnifiedID: "uid-1004-fresh", Name: "fresh", Tags: "[]", ChurnRisk: "low"}
	repo := repository.NewCustomerRepository()
	if err := repo.Create(ctx, old); err != nil {
		t.Fatalf("create old: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	if err := repo.Create(ctx, fresh); err != nil {
		t.Fatalf("create fresh: %v", err)
	}
	chosen, err := NewOneIDMergeRuleService().ChoosePrimary(ctx, []*model.Customer{old, fresh})
	if err != nil {
		t.Fatalf("ChoosePrimary: %v", err)
	}
	if chosen.ID != fresh.ID {
		t.Fatalf("latest_active 应选最近更新的 fresh，实际 %s", chosen.Name)
	}
}
