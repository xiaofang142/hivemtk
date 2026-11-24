package service

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

// TestFA44_UniqueIDsAndTargetedEdits 连续新建两条规则必须拿到不同稳定 ID；
// 重载 / 编辑 / 删除只影响目标规则，不牵连另一条。
func TestFA44_UniqueIDsAndTargetedEdits(t *testing.T) {
	fa43DB(t)
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	ctx := tenant.WithTenantID(context.Background(), 44)

	svc := NewOneIDMergeRuleService()
	set := fa43Rules(t, ctx)
	set.Custom = []MergeRule{{
		Name: "规则甲", Type: "deterministic", Fields: []string{"phone"},
		Op: "eq", Threshold: 100, Action: "auto", Priority: 10, Enabled: true,
	}}
	saved1, err := svc.SaveRules(ctx, set)
	if err != nil {
		t.Fatalf("首次保存: %v", err)
	}
	if len(saved1.Custom) != 1 || saved1.Custom[0].ID <= 0 {
		t.Fatalf("新建规则应分配正数稳定 ID: %+v", saved1.Custom)
	}
	idA := saved1.Custom[0].ID

	// 第二次新建：提交 ID=0（编辑框新建态），后端必须分配一个不同的稳定 ID。
	saved1.Custom = append(saved1.Custom, MergeRule{
		Name: "规则乙", Type: "probabilistic", Fields: []string{"email"},
		Op: "like", Threshold: 90, Action: "review", Priority: 20, Enabled: true,
	})
	saved2, err := NewOneIDMergeRuleService().SaveRules(ctx, saved1)
	if err != nil {
		t.Fatalf("第二次保存: %v", err)
	}
	if len(saved2.Custom) != 2 {
		t.Fatalf("应有两条自定义规则: %+v", saved2.Custom)
	}
	idB := saved2.Custom[1].ID
	if idB <= 0 || idB == idA {
		t.Fatalf("连续新建的规则 ID 必须不同且稳定: idA=%d idB=%d", idA, idB)
	}

	// 重载（新实例模拟页面刷新）拿到稳定 ID 与完整字段契约。
	reloaded := fa43Rules(t, ctx)
	if len(reloaded.Custom) != 2 {
		t.Fatalf("重载后应有两条规则: %+v", reloaded.Custom)
	}
	if reloaded.Custom[0].ID != idA || reloaded.Custom[1].ID != idB {
		t.Fatalf("重载后 ID 漂移: %+v", reloaded.Custom)
	}
	byID := func(id int64) *MergeRule {
		for i := range reloaded.Custom {
			if reloaded.Custom[i].ID == id {
				return &reloaded.Custom[i]
			}
		}
		return nil
	}
	a := byID(idA)
	if a == nil || a.Type != "deterministic" || a.Threshold != 100 || a.Action != "auto" || len(a.Fields) != 1 || a.Fields[0] != "phone" {
		t.Fatalf("规则甲字段契约丢失: %+v", a)
	}
	b := byID(idB)
	if b == nil || b.Type != "probabilistic" || b.Threshold != 90 || b.Action != "review" {
		t.Fatalf("规则乙字段契约丢失: %+v", b)
	}

	// 编辑只改甲的 threshold，乙的字段不动。
	reloaded.Custom[0].Threshold = 70
	edited, err := NewOneIDMergeRuleService().SaveRules(ctx, reloaded)
	if err != nil {
		t.Fatalf("编辑保存: %v", err)
	}
	bAfter := byIDIn(edited.Custom, idB)
	if bAfter == nil || bAfter.Threshold != 90 || bAfter.Action != "review" {
		t.Fatalf("编辑甲不应影响乙: %+v", bAfter)
	}
	aAfter := byIDIn(edited.Custom, idA)
	if aAfter == nil || aAfter.Threshold != 70 {
		t.Fatalf("甲的 threshold 未生效: %+v", aAfter)
	}

	// 删除甲只影响甲，乙仍在。
	out, err := NewOneIDMergeRuleService().DeleteRule(ctx, idA)
	if err != nil {
		t.Fatalf("删除: %v", err)
	}
	if len(out.Custom) != 1 || out.Custom[0].ID != idB {
		t.Fatalf("删除应只影响目标规则: %+v", out.Custom)
	}
}

func byIDIn(rules []MergeRule, id int64) *MergeRule {
	for i := range rules {
		if rules[i].ID == id {
			return &rules[i]
		}
	}
	return nil
}

// TestFA44_MultiFieldThresholdActionConsistency 多字段 + 阈值 + 审核动作
// 在预览与正式执行（ApplyRulesWithAction）口径一致：命中 rule 的 Action=review
// 时返回 review，页面预览样例也标注 review。
func TestFA44_MultiFieldThresholdActionConsistency(t *testing.T) {
	database := fa43DB(t)
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	ctx := tenant.WithTenantID(context.Background(), 441)

	// 纯函数：多字段 OR 取最高分；threshold 决定 matched；阈值<=0 视为 100。
	mr := MergeRule{Fields: []string{"phone", "email"}, Op: "eq", Threshold: 90}
	if ok, score := evaluateRule(
		map[string]string{"phone": "p-1", "email": "e-1"},
		map[string]string{"phone": "p-2", "email": "e-2"}, mr); ok || score != 0 {
		t.Fatalf("无字段命中应不匹配: ok=%v score=%d", ok, score)
	}
	if ok, score := evaluateRule(
		map[string]string{"phone": "p-1", "email": "e-1"},
		map[string]string{"phone": "p-1", "email": "e-2"}, mr); !ok || score != 100 {
		t.Fatalf("phone 命中应得 100 且 matched: ok=%v score=%d", ok, score)
	}
	mrHigh := MergeRule{Fields: []string{"phone"}, Op: "eq", Threshold: 101}
	if ok, _ := evaluateRule(map[string]string{"phone": "x"}, map[string]string{"phone": "x"}, mrHigh); ok {
		t.Fatal("超过 100 的阈值不应被满足")
	}

	// 正式执行：保存一条 review 动作规则，ApplyRulesWithAction 返回 review。
	set := fa43Rules(t, ctx)
	set.Strategy.ConflictBehavior = "auto_merge"
	set.Custom = []MergeRule{{
		Name: "审核规则", Fields: []string{"wechat_open_id"}, Op: "eq",
		Threshold: 100, Action: "review", Priority: 10, Enabled: true,
	}}
	if _, err := NewOneIDMergeRuleService().SaveRules(ctx, set); err != nil {
		t.Fatalf("保存审核规则: %v", err)
	}
	hit, name, action, score, err := NewOneIDMergeRuleService().ApplyRulesWithAction(ctx,
		map[string]string{"wechat_open_id": "wx-fa44-1"},
		map[string]string{"wechat_open_id": "wx-fa44-1"})
	if err != nil || !hit || name != "审核规则" || action != "review" || score != 100 {
		t.Fatalf("审核动作未透出: hit=%v name=%s action=%s score=%d err=%v", hit, name, action, score, err)
	}

	// 预览：两名客户同 wechat_open_id，预览样例应标注同一 rule / action。
	c1 := &model.Customer{ID: "fa44-c1", UnifiedID: "fa44-u1", Name: "甲", WechatOpenID: "wx-fa44-1"}
	c2 := &model.Customer{ID: "fa44-c2", UnifiedID: "fa44-u2", Name: "乙", WechatOpenID: "wx-fa44-1"}
	if err := database.WithContext(ctx).Create(c1).Error; err != nil {
		t.Fatalf("插客户1: %v", err)
	}
	if err := database.WithContext(ctx).Create(c2).Error; err != nil {
		t.Fatalf("插客户2: %v", err)
	}
	preview, err := NewOneIDMergeRuleService().PreviewMergeRules(ctx, []MergePreviewRule{{
		Name: "审核规则", Fields: []string{"wechat_open_id"}, Op: "eq",
		Threshold: 100, Action: "review", Enabled: true,
	}})
	if err != nil {
		t.Fatalf("预览: %v", err)
	}
	if preview.CandidateCount < 1 || len(preview.Samples) == 0 {
		t.Fatalf("预览应产生候选对: %+v", preview)
	}
	found := false
	for _, s := range preview.Samples {
		if s.Rule == "审核规则" && s.Action == "review" {
			found = true
		}
	}
	if !found {
		t.Fatalf("预览样例未标注规则/审核动作: %+v", preview.Samples)
	}
}

// TestFA44_ValidateRejectsUnknownAndDuplicateFields 未知字段 / 重复字段 /
// 重复规则 ID / 非法类型枚举必须有明确错误，不能静默当已保存。
func TestFA44_ValidateRejectsUnknownAndDuplicateFields(t *testing.T) {
	base := func() *MergeRuleSet {
		s := defaultRuleSet()
		s.Custom = nil
		return s
	}
	cases := []struct {
		name    string
		mutate  func(*MergeRuleSet)
		wantErr string
	}{
		{"unknown field", func(s *MergeRuleSet) {
			s.Custom = []MergeRule{{Name: "x", Fields: []string{"not_a_field"}, Enabled: true}}
		}, "不支持字段"},
		{"duplicate field", func(s *MergeRuleSet) {
			s.Custom = []MergeRule{{Name: "x", Fields: []string{"phone", "phone"}, Enabled: true}}
		}, "重复"},
		{"duplicate rule id", func(s *MergeRuleSet) {
			s.Custom = []MergeRule{
				{ID: 7, Name: "a", Fields: []string{"phone"}, Enabled: true},
				{ID: 7, Name: "b", Fields: []string{"email"}, Enabled: true},
			}
		}, "ID 7 重复"},
		{"bad action", func(s *MergeRuleSet) {
			s.Custom = []MergeRule{{Name: "x", Fields: []string{"phone"}, Action: "ignore", Enabled: true}}
		}, "action"},
		{"bad threshold", func(s *MergeRuleSet) {
			s.Custom = []MergeRule{{Name: "x", Fields: []string{"phone"}, Threshold: 101, Enabled: true}}
		}, "threshold"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := base()
			tc.mutate(s)
			err := validateRuleSet(s)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("期望错误含 %q，实际 %v", tc.wantErr, err)
			}
		})
	}
}

// 守门：导出函数契约仍供 FA-43 既有调用，且 Threshold<=0 视为默认 100。
func TestFA44_ThresholdDefault(t *testing.T) {
	r := MergeRule{Fields: []string{"phone"}, Op: "eq", Threshold: 0}
	if ok, score := evaluateRule(map[string]string{"phone": "x"}, map[string]string{"phone": "x"}, r); !ok || score != 100 {
		t.Fatalf("threshold<=0 应视为 100: ok=%v score=%d", ok, score)
	}
}
