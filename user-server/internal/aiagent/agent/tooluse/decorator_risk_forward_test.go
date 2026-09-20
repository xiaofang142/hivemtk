// decorator_risk_forward_test.go T-P3-05：包装层不得改写内层工具的分级。
//
// 生产路径上每个工具在注册后都会被 permissionGuardedTool 再包一层
// （app.rewirePermissionDecorators），审批门/DNC 也各是一层包装。包装结构体
// 自己不声明分级，于是"分级"这件事很容易在包装之后消失 —— 而消失的方向是
// high_write（兜底），表现为"所有工具突然都成高危"。enforce 一开就是全站拒调用，
// 且报告里 declared=false 会占满，看不出是谁弄丢的。
//
// 所以每层包装都要**原样透出**内层的声明，包括"内层没声明"这件事本身。
package tooluse

import "testing"

func TestRiskLevel_FiltersForwardInnerDeclaration(t *testing.T) {
	declared := &riskStubTool{BaseTool{NameVal: "reach.sms.send", RiskVal: RiskHighWrite}}
	undeclared := &riskStubTool{BaseTool{NameVal: "mystery.tool"}}

	wrappers := []struct {
		name string
		wrap func(Tool) Tool
	}{
		{"approvalTool", func(t Tool) Tool { return WithApproval(t) }},
		{"dncTool", func(t Tool) Tool { return WithDNCGuard(t) }},
	}

	for _, w := range wrappers {
		got, ok := EffectiveRisk(w.wrap(declared))
		if got != RiskHighWrite || !ok {
			t.Errorf("%s 包住的已声明工具 → (%s, %v)，期望 (high_write, true)：包装层弄丢了内层声明",
				w.name, got, ok)
		}
		// 关键的一半：内层没声明时，包装层不能替它"声明"出一个等级来。
		got, ok = EffectiveRisk(w.wrap(undeclared))
		if got != RiskHighWrite || ok {
			t.Errorf("%s 包住的未声明工具 → (%s, %v)，期望 (high_write, false)：包装层把未声明洗成了已声明",
				w.name, got, ok)
		}
	}
}

func TestRiskLevel_ApprovalToolStillExposesIdentity(t *testing.T) {
	// 包装层透出分级时不能顺手改掉 Name/Category —— IsColdOutreachTool 依赖 Category 判定。
	inner := &riskStubTool{BaseTool{NameVal: "reach.batch", CategoryVal: CategoryReach, RiskVal: RiskHighWrite}}
	outer := WithApproval(inner)
	if outer.Name() != "reach.batch" || outer.Category() != CategoryReach {
		t.Errorf("包装后身份漂移：name=%s category=%s", outer.Name(), outer.Category())
	}
	if !IsColdOutreachTool(outer) {
		t.Error("包装后的 reach.batch 不再被认成冷触达：分级层与审批门的判据会各读各的")
	}
}

// TestRiskLevel_NilInnerDoesNotPanic 保证透出方法在 inner 为 nil 时不炸。
// WithApproval(nil) 返回的是 nil Tool，调用方要能判；但已经存在的包装体里
// inner 被换成了 nil（测试替身重置）时也不该 panic。
func TestRiskLevel_NilInnerDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("内层为 nil 时 panic：%v", r)
		}
	}()
	a := &approvalTool{}
	if got := a.RiskLevel(); got != "" {
		t.Errorf("nil 内层 → %q，期望空串（未声明）", got)
	}
	d := &dncTool{}
	if got := d.RiskLevel(); got != "" {
		t.Errorf("nil 内层 → %q，期望空串（未声明）", got)
	}
	if _, ok := EffectiveRisk(&approvalTool{}); ok {
		t.Error("空包装被判成已声明")
	}
	_ = EffectiveRisk
}
