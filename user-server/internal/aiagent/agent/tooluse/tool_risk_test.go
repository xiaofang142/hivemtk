// tool_risk_test.go T-P3-05：工具风险分级的读取口径。
//
// 这一层的存在理由只有一个：**没声明不等于低风险**。分级元数据一旦可选，
// 默认值就决定了新工具落地时的行为 —— 默认 readonly 意味着"忘了分级"表现为
// "什么都能干"，而没人会去查一个绿灯。所以 EffectiveRisk 的唯一兜底方向是
// high_write，并且这条兜底必须能在报告里被认出来（declared=false），
// 否则 safe-by-default 和"其实没分级"在数据上长得一样。
package tooluse

import (
	"context"
	"testing"
)

type riskStubTool struct {
	BaseTool
}

func (t *riskStubTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	return SuccessResult(t.Name(), nil), nil
}

// noBaseTool 不嵌入 BaseTool：模拟第三方 Provider 交上来的、没参与过本仓约定的工具。
type noBaseTool struct{ name string }

func (t *noBaseTool) Name() string               { return t.name }
func (t *noBaseTool) Category() ToolCategory     { return CategoryBusiness }
func (t *noBaseTool) Description() string        { return "stub" }
func (t *noBaseTool) Parameters() ToolParameters { return ToolParameters{Type: "object"} }
func (t *noBaseTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	return SuccessResult(t.name, nil), nil
}

func TestEffectiveRisk_DeclaredLevelsArePassedThrough(t *testing.T) {
	cases := []struct {
		level ToolRiskLevel
		want  ToolRiskLevel
	}{
		{RiskReadonly, "readonly"},
		{RiskLowWrite, "low_write"},
		{RiskHighWrite, "high_write"},
	}
	for _, c := range cases {
		tool := &riskStubTool{BaseTool{NameVal: "declared." + string(c.level), RiskVal: c.level}}
		got, declared := EffectiveRisk(tool)
		if got != c.want {
			t.Errorf("分级=%s，期望 %s", got, c.want)
		}
		if !declared {
			t.Errorf("%s 已声明分级却报成未声明", tool.Name())
		}
	}
}

func TestEffectiveRisk_UndeclaredFallsToHighWrite(t *testing.T) {
	// 嵌入 BaseTool 但没填 RiskVal：这是最容易发生的一种漏声明（编译期零提示）。
	blank := &riskStubTool{BaseTool{NameVal: "customer.search"}}
	if got, declared := EffectiveRisk(blank); got != RiskHighWrite || declared {
		t.Errorf("空分级 → (%s, %v)，期望 (high_write, false)：没声明不能读成低风险", got, declared)
	}

	// 完全不认识本约定 implements Tool 的外部工具。
	outsider := &noBaseTool{name: "third_party.tool"}
	if got, declared := EffectiveRisk(outsider); got != RiskHighWrite || declared {
		t.Errorf("未实现 RiskDeclared → (%s, %v)，期望 (high_write, false)", got, declared)
	}

	// 声明了一个不存在的等级（拼写错 / 从别处抄来的字符串）。
	bogus := &riskStubTool{BaseTool{NameVal: "typo.tool", RiskVal: ToolRiskLevel("high")}}
	if got, declared := EffectiveRisk(bogus); got != RiskHighWrite || declared {
		t.Errorf("非法分级字面量 → (%s, %v)，期望 (high_write, false)", got, declared)
	}

	if got, declared := EffectiveRisk(nil); got != RiskHighWrite || declared {
		t.Errorf("nil 工具 → (%s, %v)，期望 (high_write, false)", got, declared)
	}
}

func TestKnownRiskLevels_IsTheAuthorityForValidity(t *testing.T) {
	// 报告与判定共用同一份合法集；写第三处字面量就会漂移。
	if len(KnownRiskLevels) != 3 {
		t.Fatalf("合法分级数=%d，期望 3", len(KnownRiskLevels))
	}
	for _, l := range KnownRiskLevels {
		if !IsValidRiskLevel(l) {
			t.Errorf("%s 在合法集里却判为非法", l)
		}
	}
	for _, bad := range []ToolRiskLevel{"", "Readonly", "critical", "high_write "} {
		if IsValidRiskLevel(bad) {
			t.Errorf("%q 被判为合法分级：大小写或空格差异不该放过", bad)
		}
	}
}
