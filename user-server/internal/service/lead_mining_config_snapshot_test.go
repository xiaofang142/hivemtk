package service

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
)

// LEAD-07 RED/GREEN：旧新配置统一、优先级/继承/关闭、一次分析冻结同一版本，
// 且 gate/三路/判定/持久化共同消费同一份快照，切换配置不混用版本。

type lmRuleRepoStub struct {
	rows map[string]*model.LeadMiningRule
	err  error
}

func (r *lmRuleRepoStub) GetByScope(_ context.Context, scope string, id uint) (*model.LeadMiningRule, error) {
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

func TestLeadMiningConfigResolver_PriorityAndInheritance(t *testing.T) {
	ctx := context.Background()
	rules := &lmRuleRepoStub{rows: map[string]*model.LeadMiningRule{
		"account": {Scope: "account", Enabled: true, SystemDescription: "account-rule"},
		"channel": {Scope: "channel", Enabled: true, SystemDescription: "channel-rule"},
		"global":  {Scope: "global", Enabled: true, SystemDescription: "global-rule"},
	}}
	r := NewLeadMiningConfigResolverWithDeps(rules, &lmConfigRepoStub{cfg: &model.LeadMiningConfig{
		Enabled: true, Keywords: model.JSONStrings{"legacy"}, MinIntentScore: 60, Model: "legacy-model",
	}})
	snap, err := r.Resolve(ctx, LeadRuleTarget{ChannelAccountID: 1, ChannelID: 2})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if snap.RuleScope != "account" || snap.RuleDescription != "account-rule" || snap.MinIntentScore != 60 || snap.Model != "legacy-model" {
		t.Fatalf("account 优先 + legacy 阈值应生效: %+v", snap)
	}
	// 账号级关闭：合法继承渠道级；关闭不是回退到 legacy 放行。
	rules.rows["account"].Enabled = false
	snap, err = r.Resolve(ctx, LeadRuleTarget{ChannelAccountID: 1, ChannelID: 2})
	if err != nil || snap.RuleScope != "channel" {
		t.Fatalf("账号关闭应继承 channel，实际 scope=%s err=%v", snap.RuleScope, err)
	}
	// 全部关闭：fail-closed 为 disabled 快照，不启用 legacy。
	rules.rows["channel"].Enabled = false
	rules.rows["global"].Enabled = false
	snap, err = r.Resolve(ctx, LeadRuleTarget{ChannelAccountID: 1, ChannelID: 2})
	if err != nil || snap.Enabled {
		t.Fatalf("全部关闭必须禁用发掘，实际 enabled=%t err=%v", snap.Enabled, err)
	}
}

func TestLeadMiningConfigResolver_UnifyLegacyAndAgentVersion(t *testing.T) {
	ctx := context.Background()
	r := NewLeadMiningConfigResolverWithDeps(&lmRuleRepoStub{rows: map[string]*model.LeadMiningRule{
		"global": {Scope: "global", Enabled: true, SystemDescription: "g"},
	}}, &lmConfigRepoStub{cfg: &model.LeadMiningConfig{Enabled: false, MinIntentScore: 55, Model: "cfg-model", Requirement: "legacy req"}})
	snap, err := r.Resolve(ctx, LeadRuleTarget{})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if snap.Enabled || snap.MinIntentScore != 55 || snap.Model != "cfg-model" {
		t.Fatalf("legacy 关闭不得被全局规则反向开启，但阈值/模型应继承: %+v", snap)
	}
	if snap.Requirement != "legacy req" {
		t.Fatalf("legacy 表单配置未统一: %+v", snap)
	}
	if snap.Version == "" {
		t.Fatal("必须生成版本标识用于冻结快照")
	}
}

func TestLeadMiningConfigResolver_RejectsScopeAndLookupErrors(t *testing.T) {
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
	r := NewLeadMiningConfigResolverWithDeps(&lmRuleRepoStub{rows: map[string]*model.LeadMiningRule{
		"global": {Scope: "global", Enabled: true, SystemDescription: "v1"},
	}}, &lmConfigRepoStub{cfg: &model.LeadMiningConfig{Enabled: true, Model: "m1", MinIntentScore: 50}})
	snapA, err := r.Resolve(ctx, LeadRuleTarget{})
	if err != nil {
		t.Fatalf("快照 A 失败: %v", err)
	}
	// 分析过程中切换配置：新解析必须得到新版本，旧分析仍消费冻结的 A。
	r.rules.(*lmRuleRepoStub).rows["global"].SystemDescription = "v2"
	r.config.(*lmConfigRepoStub).cfg.Model = "m2"
	snapB, err := r.Resolve(ctx, LeadRuleTarget{})
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
	// gate/三路/判定/持久化共同消费同一冻结快照的标识。
	for _, consumer := range []string{"gate", "detection", "judge", "persistence"} {
		if got := snapA.Consume(consumer); got != snapA.Version {
			t.Fatalf("%s 消费了错版本 %s，期望 %s", consumer, got, snapA.Version)
		}
	}
	if snapA.Enabled != true || snapA.MinIntentScore != 50 {
		t.Fatalf("冻结快照内容错误: %+v", snapA)
	}
}
