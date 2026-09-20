// permission_wiring_test.go T-P3-05 的 app 侧接线：分级层的三件事——
// 旗子怎么解、挂上后放行结果是否真的没变、包装层有没有把声明洗掉。
package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/agent/tooluse"
)

type wiringRiskTool struct {
	tooluse.BaseTool
	calls int
}

func (t *wiringRiskTool) Execute(ctx context.Context, args map[string]any) (tooluse.ToolResult, error) {
	t.calls++
	return tooluse.SuccessResult(t.Name(), map[string]any{"calls": t.calls}), nil
}

func TestParseRiskGateMode(t *testing.T) {
	cases := []struct {
		raw  string
		want riskGateMode
	}{
		{"", riskGateOff},
		{"off", riskGateOff},
		{"false", riskGateOff},
		{"0", riskGateOff},
		{"shadow", riskGateShadow},
		{"observe", riskGateShadow},
		{" SHADOW ", riskGateShadow},
		// 阻断语气的值一律落到 shadow：本构建没有拒绝路径，但意图不能被判成"关掉"。
		{"enforce", riskGateShadow},
		{"block", riskGateShadow},
		{"true", riskGateShadow},
		{"1", riskGateShadow},
		// 认不出的值不挂链（挂了也没人消费）。
		{"banana", riskGateOff},
	}
	for _, c := range cases {
		if got := parseRiskGateMode(c.raw); got != c.want {
			t.Errorf("parseRiskGateMode(%q)=%s，期望 %s", c.raw, got, c.want)
		}
	}
}

func TestApplyRiskGate_OffLeavesConfigUntouched(t *testing.T) {
	t.Setenv(RiskGateFlagEnv, "off")
	cfg := tooluse.ToolExecutorConfig{DefaultTimeout: time.Second}
	if mode := applyRiskGate(&cfg); mode != riskGateOff {
		t.Fatalf("mode=%s，期望 off", mode)
	}
	if cfg.RiskGrants != nil || cfg.RiskObserver != nil {
		t.Error("off 模式下两个字段必须显式清空（不是「没赋值」，是「赋成 nil」）")
	}
}

func TestApplyRiskGate_ShadowMountsObserver(t *testing.T) {
	t.Setenv(RiskGateFlagEnv, "shadow")
	cfg := tooluse.ToolExecutorConfig{DefaultTimeout: time.Second}
	if mode := applyRiskGate(&cfg); mode != riskGateShadow {
		t.Fatalf("mode=%s，期望 shadow", mode)
	}
	if cfg.RiskGrants == nil || cfg.RiskObserver == nil {
		t.Fatal("shadow 模式下两个字段都必须挂上")
	}
	if _, ok := cfg.RiskGrants.(*tooluse.WhitelistPermissionChecker); !ok {
		t.Errorf("grants 实际类型 %T：判定必须直接读 Agent 白名单那张表", cfg.RiskGrants)
	}
}

// TestRiskGate_EndToEndThroughPermissionGuardAndExecutor 是本卡最重要的一条：
// 生产路径上每个工具都被 permissionGuardedTool 包了一层（rewirePermissionDecorators），
// 而那个结构体嵌入的是 tooluse.Tool **接口** —— 接口方法集里没有 RiskLevel，
// 所以它不会自动透出内层声明。忘了写转发方法的后果是"所有工具都被兜底成 high_write"，
// 而这条链在 shadow 态下不会报错，只会在 P9 的报告中表现为"100% 会被拦"。
func TestRiskGate_EndToEndThroughPermissionGuardAndExecutor(t *testing.T) {
	t.Setenv(RiskGateFlagEnv, "shadow")
	// 名字带 wiringtest：不撞生产工具名，撞名会让同进程里真实接线用例的注册静默失败。
	const toolName = "reach.wiringtest.send"
	tool := &wiringRiskTool{BaseTool: tooluse.BaseTool{
		NameVal: toolName, CategoryVal: tooluse.CategoryReach, RiskVal: tooluse.RiskHighWrite,
	}}

	// ① 包装层透出声明
	guarded := &permissionGuardedTool{Tool: tool, checker: tooluse.NewWhitelistPermissionChecker()}
	level, declared := tooluse.EffectiveRisk(guarded)
	if !declared || level != tooluse.RiskHighWrite {
		t.Fatalf("permissionGuardedTool 洗完分级：(%s,%v)，期望 (high_write,true)", level, declared)
	}
	// 未声明的工具也不能被洗成已声明
	blank := &permissionGuardedTool{Tool: &wiringRiskTool{BaseTool: tooluse.BaseTool{NameVal: "mystery.tool"}}}
	if _, declared := tooluse.EffectiveRisk(blank); declared {
		t.Error("包装层把未声明洗成了已声明")
	}

	// ② 挂上真实配置后，一次高危调用照常执行且留下判定。
	//
	// 注册中心刻意用**全局那一个**（而不是新建的）：GetToolRiskReport 读的就是它，
	// 生产里 executor 也用它（tool_executor_wiring.go:58）。两边同源，这条断言才覆盖
	// "报告口径 = 执行口径"；各建一个注册中心会把它退化成只看本地那份。
	registry := tooluse.GetGlobalRegistry()
	if err := registry.Register(guarded); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := registry.Unregister(toolName); err != nil {
			t.Errorf("清理失败：%v（残留会污染同进程其它用例的报告总数）", err)
		}
	})
	cfg := tooluse.ToolExecutorConfig{DefaultTimeout: 5 * time.Second}
	// 不必修"把包级观察层复位"的清理：两个读 riskModeValue/riskObserverRef 的用例
	// 各自在开头调一次 applyRiskGate，断言时刻的 globals 一定是自己刚挂上的那份。
	// 真正会跨用例泄漏的只有注册中心，所以上面只 Unregister。
	applyRiskGate(&cfg)
	exec := tooluse.NewToolExecutor(registry, cfg)

	// 该 Agent 一条白名单都没配 ⇒ 判定必为 would_deny，但放行结果不得改变。
	res, err := exec.ExecuteByNameWithCtx(context.Background(), toolName, nil,
		&tooluse.ToolContext{AgentID: "wiring-test-agent"})
	if err != nil {
		t.Fatalf("shadow 判定门下调用被拒：%v", err)
	}
	if !res.Success {
		t.Errorf("放行结果被改写：%+v", res)
	}
	if tool.calls != 1 {
		t.Errorf("工具执行次数=%d，期望 1", tool.calls)
	}
	report := GetToolRiskReport()
	if report.Mode != string(riskGateShadow) {
		t.Errorf("报告 mode=%s，期望 shadow", report.Mode)
	}
	if report.BlocksWhenDenied {
		t.Error("blocks_when_denied=true：本构建不存在拒绝路径，这个字段是假的")
	}
	var row *tooluse.RiskReportTool
	for i := range report.Tools {
		if report.Tools[i].Name == toolName {
			row = &report.Tools[i]
		}
	}
	if row == nil {
		t.Fatalf("报告里没有 %s：刚注册并执行过的工具没进报告，口径不可信", toolName)
	}
	if row.Calls == 0 || row.WouldDeny == 0 {
		t.Errorf("报告计数 calls=%d would_deny=%d，期望都 >0（刚跑过一次会被拦的调用）", row.Calls, row.WouldDeny)
	}
}

// TestGetToolRiskReport_ReadableWithFlagOff 旗子关掉时报告仍要可读：
// 静态声明面与授权面不依赖观察层，P9 评审恰恰是在旗子还关着的时候做的。
//
// 断言全部**从报告自身推导**，不写死 45/20 这类绝对数字：本进程的全局注册中心里
// 有多少工具，取决于同包其它用例装配了哪几批（agent_reach_wiring_test.go 会注册真工具），
// 把普查数字抄到这里只会造出一个随执行顺序漂移到假红的用例。
// 全集普查在 tooluse 包的 tool_risk_coverage_test.go 里做 —— 那里的工具集合是构造出来的、确定性的。
func TestGetToolRiskReport_ReadableWithFlagOff(t *testing.T) {
	t.Setenv(RiskGateFlagEnv, "off")
	cfg := tooluse.ToolExecutorConfig{}
	applyRiskGate(&cfg)

	rep := GetToolRiskReport()
	if rep.Mode != "off" {
		t.Errorf("mode=%s，期望 off", rep.Mode)
	}
	if rep.ObservationsPersisted {
		t.Error("off 模式下观察层不该挂着")
	}
	if len(rep.Counts) != 0 {
		t.Errorf("off 模式下 counts 非空（%d 条）：说明观察层没被清干净", len(rep.Counts))
	}
	total := len(tooluse.GetGlobalRegistry().List())
	if rep.Total != total {
		t.Errorf("报告 total=%d，全局注册中心实际 %d：两边不同源，报告不可信", rep.Total, total)
	}
	if total == 0 {
		t.Skip("本测试进程里全局注册中心为空：分级一致性断言交给已装配的用例")
	}
	if len(rep.Undeclared) != 0 {
		t.Errorf("有 %d 个工具未声明分级：%v —— AC① 要求生产工具全部声明", len(rep.Undeclared), rep.Undeclared)
	}
	summed := 0
	for _, n := range rep.ByLevel {
		summed += n
	}
	if summed != rep.Total {
		t.Errorf("各档之和=%d，总数=%d：有工具的分级没被计入任何一档", summed, rep.Total)
	}
	t.Logf("✅ 本进程全局注册中心装配到的工具：%d 个，分级分布 %v", rep.Total, rep.ByLevel)
	// 盲区判据：报告里"高危且不在审批门内"的行数，必须等于 high_write 与门内数量之差。
	// 写死"20 - 2"同样会随装配漂移，这里双向对上就足够咬住漏算。
	var outsideGate int
	for _, row := range rep.Tools {
		if row.Level == string(tooluse.RiskHighWrite) && !row.ColdOutreach {
			outsideGate++
		}
	}
	if rep.HighWriteInGate+outsideGate != rep.ByLevel[string(tooluse.RiskHighWrite)] {
		t.Errorf("门内 %d + 门外 %d ≠ 高危总数 %d", rep.HighWriteInGate, outsideGate,
			rep.ByLevel[string(tooluse.RiskHighWrite)])
	}
	hints := strings.Join(rep.ReadingHint, "|")
	if !strings.Contains(hints, "没在观察") {
		t.Errorf("reading_hint=%q，期望含「未挂载观察层」的提示", hints)
	}
}
