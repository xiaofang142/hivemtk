package service

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
)

// T01：全局默认兜底下线。账号/渠道规则是唯一真实入口，
// 缺失启用规则一律 ErrLeadRuleNotFound；legacy 配置只贡献阈值/模型/要求等参数默认值。

type lmRuleRepoStub struct {
	rows map[string]*model.LeadMiningRule
	err  error
}

func (r *lmRuleRepoStub) GetByScope(_ context.Context, scope string, _ uint) (*model.LeadMiningRule, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.rows[scope], nil
}
func (r *lmRuleRepoStub) List(context.Context) ([]model.LeadMiningRule, error) { return nil, nil }
func (r *lmRuleRepoStub) Create(context.Context, *model.LeadMiningRule) error  { return nil }
func (r *lmRuleRepoStub) Update(context.Context, int64, map[string]any) error  { return nil }
func (r *lmRuleRepoStub) Delete(context.Context, int64) error                  { return nil }

type lmConfigRepoStub struct {
	cfg *model.LeadMiningConfig
	err error
}

func (r *lmConfigRepoStub) GetSingleton(context.Context) (*model.LeadMiningConfig, error) {
	return r.cfg, r.err
}
func (r *lmConfigRepoStub) Save(context.Context, *model.LeadMiningConfig) error { return nil }

func TestLeadMiningConfigResolver_PriorityAndNoGlobalFallback(t *testing.T) {
	ctx := context.Background()
	rules := &lmRuleRepoStub{rows: map[string]*model.LeadMiningRule{
		"account": {Scope: "account", Enabled: true, SystemDescription: "account-rule", Keywords: model.JSONStrings{"账号词"}},
		"channel": {Scope: "channel", Enabled: true, SystemDescription: "channel-rule", Keywords: model.JSONStrings{"渠道词"}},
		"global":  {Scope: "global", Enabled: true, SystemDescription: "全局兜底", Keywords: model.JSONStrings{"全局词"}},
	}}
	r := NewLeadMiningConfigResolverWithDeps(rules, &lmConfigRepoStub{cfg: &model.LeadMiningConfig{
		Enabled: true, Keywords: model.JSONStrings{"legacy"}, MinIntentScore: 60, Model: "legacy-model",
	}})
	target := LeadRuleTarget{ChannelAccountID: 1, ChannelID: 2}

	snap, err := r.Resolve(ctx, target)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if snap.RuleScope != "account" || snap.RuleDescription != "account-rule" || snap.MinIntentScore != 60 || snap.Model != "legacy-model" {
		t.Fatalf("account 优先 + legacy 参数默认值应生效: %+v", snap)
	}
	if len(snap.Keywords) != 1 || snap.Keywords[0] != "账号词" {
		t.Fatalf("关键词只能来自命中的规则，不得混入 legacy/global: %+v", snap.Keywords)
	}

	// 账号级关闭：合法继承渠道级。
	rules.rows["account"].Enabled = false
	snap, err = r.Resolve(ctx, target)
	if err != nil || snap.RuleScope != "channel" {
		t.Fatalf("账号关闭应继承 channel，实际 scope=%v err=%v", snap.RuleScope, err)
	}

	// 账号/渠道全部关闭：即使 global 行启用，也必须 fail-closed。
	rules.rows["channel"].Enabled = false
	snap, err = r.Resolve(ctx, target)
	if !errors.Is(err, ErrLeadRuleNotFound) || snap != nil {
		t.Fatalf("无启用规则必须返回 ErrLeadRuleNotFound，不得回落 global；snap=%+v err=%v", snap, err)
	}

	// 两级都没有规则行：同样 fail-closed。
	delete(rules.rows, "channel")
	snap, err = r.Resolve(ctx, LeadRuleTarget{})
	if !errors.Is(err, ErrLeadRuleNotFound) || snap != nil {
		t.Fatalf("缺失规则必须 fail-closed；snap=%+v err=%v", snap, err)
	}
}

func TestLeadMiningConfigResolver_LegacyParamsDoNotOverrideRuleSwitch(t *testing.T) {
	ctx := context.Background()
	r := NewLeadMiningConfigResolverWithDeps(&lmRuleRepoStub{rows: map[string]*model.LeadMiningRule{
		"account": {Scope: "account", Enabled: true, Keywords: model.JSONStrings{"采购"}},
	}}, &lmConfigRepoStub{cfg: &model.LeadMiningConfig{
		Enabled: false, Keywords: model.JSONStrings{"legacy"}, MinIntentScore: 55,
		Model: "cfg-model", Requirement: "legacy req",
	}})
	snap, err := r.Resolve(ctx, LeadRuleTarget{ChannelAccountID: 1})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if !snap.Enabled {
		t.Fatalf("规则是唯一开关源，legacy cfg.Enabled=false 不得再 AND 关闭规则: %+v", snap)
	}
	if snap.MinIntentScore != 55 || snap.Model != "cfg-model" || snap.Requirement != "legacy req" {
		t.Fatalf("阈值/模型/要求仍应继承参数默认值: %+v", snap)
	}
	if len(snap.Keywords) != 1 || snap.Keywords[0] != "采购" {
		t.Fatalf("legacy 关键词不得兜底进入规则快照: %+v", snap.Keywords)
	}
	if snap.Version == "" {
		t.Fatal("必须生成版本标识用于冻结快照")
	}
}

func TestLeadMiningConfigResolver_RejectsLookupErrors(t *testing.T) {
	ctx := context.Background()
	r := NewLeadMiningConfigResolverWithDeps(&lmRuleRepoStub{err: errors.New("db down")}, &lmConfigRepoStub{cfg: &model.LeadMiningConfig{Enabled: true}})
	snap, err := r.Resolve(ctx, LeadRuleTarget{ChannelAccountID: 1})
	if err == nil {
		t.Fatalf("规则查询故障不得静默放大为全局启用，实际 snap=%+v", snap)
	}
	if snap != nil && snap.Enabled {
		t.Fatal("故障快照不得启用")
	}
}

func TestLeadMiningAnalysisSnapshot_FrozenVersionAcrossConsumers(t *testing.T) {
	ctx := context.Background()
	rules := &lmRuleRepoStub{rows: map[string]*model.LeadMiningRule{
		"account": {Scope: "account", Enabled: true, SystemDescription: "v1", Keywords: model.JSONStrings{"约"}},
	}}
	r := NewLeadMiningConfigResolverWithDeps(rules, &lmConfigRepoStub{cfg: &model.LeadMiningConfig{Model: "m1", MinIntentScore: 50}})
	target := LeadRuleTarget{ChannelAccountID: 1}
	snapA, err := r.Resolve(ctx, target)
	if err != nil {
		t.Fatalf("快照 A 失败: %v", err)
	}
	// 分析过程中切换配置：新解析必须得到新版本，旧分析仍消费冻结的 A。
	rules.rows["account"].SystemDescription = "v2"
	r.config.(*lmConfigRepoStub).cfg.Model = "m2"
	snapB, err := r.Resolve(ctx, target)
	if err != nil {
		t.Fatalf("快照 B 失败: %v", err)
	}
	if snapA.Version == snapB.Version || snapA.Model == snapB.Model {
		t.Fatalf("配置切换应产生不同版本，A=%+v B=%+v", snapA, snapB)
	}
	if snapA.Matches(snapB) {
		t.Fatal("不同版本不得判定为同一份快照")
	}
	if !snapA.Matches(snapA) {
		t.Fatal("同一快照必须自洽")
	}
	for _, consumer := range []string{"gate", "detection", "judge", "persistence"} {
		if got := snapA.Consume(consumer); got != snapA.Version {
			t.Fatalf("%s 消费了错版本 %s，期望 %s", consumer, got, snapA.Version)
		}
	}
	if !snapA.Enabled || snapA.MinIntentScore != 50 {
		t.Fatalf("冻结快照内容错误: %+v", snapA)
	}
}
