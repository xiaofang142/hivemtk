// approval_wiring_test.go T-P1-05：冷触达审批门接线的四条契约。
//
// 卡面 AC：
//
//	①cold outreach 工具调用产生 Decision 审计行 → TestApprovalDecisionAuditLineLandsInLog
//	②IsApproved=false 时仍放行并打 shadow 标记   → TestApprovalGateShadowLetsColdOutreachThrough
//	③NewWhiteList / SetGlobalApprovalChecker 有非测试调用点 → TestApplyApprovalGateShadowWiresRealWhitelist
//	  （后者断言接上去的确实是 approval 包的真白名单实现，不是随手写的假 checker）
//
// 另有两条不属于卡面、但灰度更依赖的：
//
//	关旗必须逐字回到"没接过线"（TestApplyApprovalGateOffLeavesExecutorUnchanged）
//	全局注入点交出的必须是"永不拦"的包装版（TestGlobalApprovalCheckerIsShadowSafe）
//
// 本文件全部用**真**的 WhiteListApprovalChecker + 真 featureflag：审批门的危险恰恰不在
// 装饰器逻辑，而在"默认拒绝"这个语义被接到生产上，用假 checker 测不出来。
package app

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/approval"
	"hivemtk-user/internal/pkg/featureflag"
	"hivemtk-user/internal/pkg/utils/logger"
)

// restoreApprovalState 保存/还原包级审批门状态；同 restoreCircuitState 的理由。
func restoreApprovalState(t *testing.T) {
	t.Helper()
	om, oc, od, osg := approvalModeValue, approvalCheckerRef, approvalDecisions, approvalGlobalSet
	t.Cleanup(func() {
		approvalModeValue, approvalCheckerRef, approvalDecisions, approvalGlobalSet = om, oc, od, osg
		tooluse.SetGlobalApprovalChecker(nil)
	})
	tooluse.SetGlobalApprovalChecker(nil)
}

// setApprovalWhitelistFlag 翻 approval.FlagKey 这把**内层**旗子（白名单是否生效）。
//
// 顺序有讲究：t.Cleanup 后注册者先跑，所以 ReloadAll 必须注册在 t.Setenv 之前，
// 才能在本子测试结束、env 复原之后再刷一次缓存，否则旗子会带着 true 漏给下一个测试。
func setApprovalWhitelistFlag(t *testing.T, on bool) {
	t.Helper()
	featureflag.Get(approval.FlagKey)
	t.Cleanup(func() { featureflag.DefaultManager().ReloadAll() })
	value := "0"
	if on {
		value = "1"
	}
	t.Setenv("FF_"+strings.ToUpper(approval.FlagKey), value)
	featureflag.DefaultManager().ReloadAll()
}

type approvalProbeTool struct {
	tooluse.BaseTool
	mu    sync.Mutex
	calls int
}

func newApprovalProbeTool(name string, category tooluse.ToolCategory) *approvalProbeTool {
	return &approvalProbeTool{BaseTool: tooluse.BaseTool{
		NameVal:     name,
		CategoryVal: category,
		ParamsVal:   tooluse.ToolParameters{Type: "object", Properties: map[string]tooluse.ToolParam{}},
	}}
}

func (p *approvalProbeTool) Execute(ctx context.Context, args map[string]any) (tooluse.ToolResult, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	return tooluse.SuccessResult(p.NameVal, map[string]any{"sent_to": p.NameVal}), nil
}

func (p *approvalProbeTool) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// alwaysDenyChecker 只用于"配置里确实被清空了"这类断言：它一旦被留在配置上就会拦。
type alwaysDenyChecker struct{}

func (alwaysDenyChecker) IsApproved(ctx context.Context, toolName, accountIDorOwnerKey string) bool {
	return false
}

// captureApprovalLog 把 fn 期间写到 stdout 的日志抓成字符串。
//
// 为什么真抓日志而不是给 checker 塞一个假回调：AC① 说的是"产生 Decision 审计行"，
// 那行只有经真 logger、真 encoder 落出去才算数——字段名拼错、JSON 不合法、
// 级别被过滤掉，假回调全都看不见。
func captureApprovalLog(t *testing.T, fn func()) string {
	t.Helper()
	oldOut := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("管道创建失败：%v", err)
	}
	os.Stdout = w
	logger.InitLogger(logger.LoggingConfig{Level: "info", Format: "json", Output: "stdout"})
	defer func() {
		os.Stdout = oldOut
		logger.InitLogger(logger.DefaultConfig())
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("关闭写端失败：%v", err)
	}
	captured, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatalf("读取日志失败：%v", readErr)
	}
	return string(captured)
}

func newExecutorWith(tool tooluse.Tool, cfg tooluse.ToolExecutorConfig) *tooluse.ToolExecutor {
	reg := tooluse.NewToolRegistry()
	_ = reg.Register(tool)
	cfg.DefaultTimeout = 5 * time.Second
	return tooluse.NewToolExecutor(reg, cfg)
}

func execCold(t *testing.T, exec *tooluse.ToolExecutor, name, caller string) tooluse.ExecuteResult {
	t.Helper()
	_ = t
	return exec.Execute(context.Background(), tooluse.ExecuteRequest{
		ToolName: name,
		Args:     map[string]any{"text": "hi"},
		ToolCtx:  &tooluse.ToolContext{CallerID: caller},
	})
}

func TestParseApprovalGateMode(t *testing.T) {
	cases := []struct {
		raw  string
		want approvalGateMode
	}{
		{"", approvalGateOff},
		{"   ", approvalGateOff},
		{"off", approvalGateOff},
		{"OFF", approvalGateOff},
		{"false", approvalGateOff},
		{"0", approvalGateOff},
		{"no", approvalGateOff},
		{"disabled", approvalGateOff},
		{"shadow", approvalGateShadow},
		{" Shadow ", approvalGateShadow},
		{"observe", approvalGateShadow},
		{"report", approvalGateShadow},
		{"true", approvalGateShadow},
		{"1", approvalGateShadow},
		{"yes", approvalGateShadow},
		{"on", approvalGateShadow},
		// 本卡不交付阻断：写了 block/enforce 也只到 shadow，且必须显式告警（见下）
		{"block", approvalGateShadow},
		{"enforce", approvalGateShadow},
		{"active", approvalGateShadow},
		// 认不出的值判 off：把旗子拼错的人不该意外获得一个"看起来开了"的闸门
		{"shodow", approvalGateOff},
		{"maybe", approvalGateOff},
		{"2", approvalGateOff},
	}
	for _, c := range cases {
		t.Run(c.raw, func(t *testing.T) {
			if got := parseApprovalGateMode(c.raw); got != c.want {
				t.Errorf("parseApprovalGateMode(%q) = %s, want %s", c.raw, got, c.want)
			}
		})
	}
}

// TestApplyApprovalGateOffLeavesExecutorUnchanged 关旗的等价性：必须回到"没接过线"。
//
// 故意先把 config 填成"已接好"的样子：只测"off 时字段是零值"会漏掉真正要防的那件事
// ——有人删掉那三行清零代码，而测试仍然绿。
func TestApplyApprovalGateOffLeavesExecutorUnchanged(t *testing.T) {
	restoreApprovalState(t)
	t.Setenv(ApprovalGateFlagEnv, "off")

	config := tooluse.ToolExecutorConfig{
		ApprovalChecker: alwaysDenyChecker{},
		ApprovalShadow:  true,
	}
	approvalCheckerRef = approval.NewWhiteList(nil, nil)
	approvalDecisions = approval.NewDecisionCounter()
	approvalGlobalSet = true
	tooluse.SetGlobalApprovalChecker(alwaysDenyChecker{})

	if mode := applyApprovalGate(&config); mode != approvalGateOff {
		t.Fatalf("模式 = %s, want off", mode)
	}
	if config.ApprovalChecker != nil {
		t.Error("off 时 config.ApprovalChecker 必须为 nil")
	}
	if config.ApprovalShadow {
		t.Error("off 时 config.ApprovalShadow 必须为 false")
	}
	snap, decisions := GetApprovalSnapshot()
	if snap.Wired || decisions != nil || snap.GlobalCheckerSet {
		t.Errorf("off 时快照应全空：%+v decisions==nil? %v", snap, decisions == nil)
	}
	if snap.Mode != string(approvalGateOff) {
		t.Errorf("快照 mode = %q, want off", snap.Mode)
	}
	// 全局注入点也要归零：否则将来任何 WithApproval 调用点都会拿到上一轮的默认拒绝 checker
	cold := newApprovalProbeTool("reach.telegram.dm", tooluse.CategoryReach)
	res, err := tooluse.WithApproval(cold).Execute(context.Background(), nil)
	if err != nil || !res.Success {
		t.Errorf("off 后 WithApproval 包装必须回到原样放行，实际 err=%v", err)
	}
	if cold.count() != 1 {
		t.Errorf("off 后冷触达应原样执行，实际 %d 次", cold.count())
	}
}

// TestApplyApprovalGateShadowWiresRealWhitelist AC③：接上去的是 approval 包的真白名单实现。
func TestApplyApprovalGateShadowWiresRealWhitelist(t *testing.T) {
	restoreApprovalState(t)
	setApprovalWhitelistFlag(t, false)
	t.Setenv(ApprovalGateFlagEnv, "shadow")

	config := tooluse.ToolExecutorConfig{}
	if mode := applyApprovalGate(&config); mode != approvalGateShadow {
		t.Fatalf("模式 = %s, want shadow", mode)
	}
	if !config.ApprovalShadow {
		t.Error("shadow 态 config.ApprovalShadow 必须为 true（本卡不存在置 false 的分支）")
	}
	if _, ok := config.ApprovalChecker.(*approval.WhiteListApprovalChecker); !ok {
		t.Fatalf("接的是 %T，不是 *approval.WhiteListApprovalChecker", config.ApprovalChecker)
	}
	snap, decisions := GetApprovalSnapshot()
	if !snap.Wired || decisions == nil {
		t.Errorf("shadow 后快照应为已接线：%+v", snap)
	}
	if !snap.GlobalCheckerSet {
		t.Error("SetGlobalApprovalChecker 必须被调用过（AC③）")
	}
	if snap.WhitelistFlagOn {
		t.Error("白名单旗子被本用例显式关掉，快照不该报 on")
	}
	if snap.WhitelistFlagKey != approval.FlagKey || snap.GateFlagEnv != ApprovalGateFlagEnv {
		t.Errorf("快照里的旗子名不对：%+v", snap)
	}
}

// TestApprovalGateShadowLetsColdOutreachThrough AC②：真白名单默认拒绝，冷触达仍然外发。
func TestApprovalGateShadowLetsColdOutreachThrough(t *testing.T) {
	restoreApprovalState(t)
	setApprovalWhitelistFlag(t, false)
	t.Setenv(ApprovalGateFlagEnv, "shadow")

	config := tooluse.ToolExecutorConfig{}
	applyApprovalGate(&config)

	cold := newApprovalProbeTool("reach.batch_send.sms", tooluse.CategoryReach)
	config.RetryPolicy = tooluse.NewExponentialBackoffPolicy(3, time.Millisecond, 5*time.Millisecond)
	config.AuditLogger = tooluse.NewMemoryAuditLogger(10)
	exec := newExecutorWith(cold, config)

	const calls = 4
	for i := 0; i < calls; i++ {
		res := execCold(t, exec, "reach.batch_send.sms", "acct-1")
		if res.Err != nil || !res.Success {
			t.Fatalf("第 %d 次调用应照常外发，实际 err=%v", i+1, res.Err)
		}
	}
	if cold.count() != calls {
		t.Errorf("%d 次冷触达应全部外发，实际 %d 次", calls, cold.count())
	}

	_, decisions := GetApprovalSnapshot()
	rep := decisions.Report()
	if rep.Total != calls || rep.WouldDeny != calls {
		t.Fatalf("判定数对不上：total=%d would_deny=%d（期望各 %d）", rep.Total, rep.WouldDeny, calls)
	}
	if rep.WouldDenyRatePct != 100 {
		t.Errorf("白名单旗子关着 ⇒ 100%% 会被拦，实际 %v", rep.WouldDenyRatePct)
	}
	if rep.ByReason[approval.ReasonDisabledByFlag] != calls {
		t.Errorf("这些拒绝的 reason 应全是 %q，实际 %v", approval.ReasonDisabledByFlag, rep.ByReason)
	}
	if rep.PerTool[0].ToolName != "reach.batch_send.sms" {
		t.Errorf("按工具拆的报表缺工具名：%+v", rep.PerTool)
	}
}

// TestApprovalGateWarmToolsNotAsked 接线不得外溢到非冷触达工具。
func TestApprovalGateWarmToolsNotAsked(t *testing.T) {
	restoreApprovalState(t)
	setApprovalWhitelistFlag(t, false)
	t.Setenv(ApprovalGateFlagEnv, "shadow")

	config := tooluse.ToolExecutorConfig{}
	applyApprovalGate(&config)
	exec := newExecutorWith(newApprovalProbeTool("reach.weixin.send", tooluse.CategoryReach), config)

	if res := execCold(t, exec, "reach.weixin.send", "acct-1"); res.Err != nil || !res.Success {
		t.Fatalf("warm 工具必须原样执行，实际 err=%v", res.Err)
	}
	_, decisions := GetApprovalSnapshot()
	if rep := decisions.Report(); rep.Total != 0 {
		t.Errorf("warm 工具一次都不该被审批门询问，实际记了 %d 笔：%v", rep.Total, rep.ByReason)
	}
}

// TestApprovalWhitelistGrantChangesReason 白名单灌入后 reason 从"没批准"翻成"已放行"。
//
// 这条是 T-P1-06 能不能转阻断的分水岭：只有当"已授权账号"能被真实观测到，
// would_deny 才不再是"全是没人开过闸"的一坨数字。
func TestApprovalWhitelistGrantChangesReason(t *testing.T) {
	restoreApprovalState(t)
	setApprovalWhitelistFlag(t, true)
	t.Setenv(ApprovalGateFlagEnv, "shadow")

	config := tooluse.ToolExecutorConfig{}
	applyApprovalGate(&config)
	cold := newApprovalProbeTool("reach.lead_outreach", tooluse.CategoryReach)
	exec := newExecutorWith(cold, config)

	// 先未授权：denied_default
	if res := execCold(t, exec, "reach.lead_outreach", "acct-9"); res.Err != nil {
		t.Fatalf("shadow 下不该报错：%v", res.Err)
	}
	_, decisions := GetApprovalSnapshot()
	if got := decisions.Report().ByReason[approval.ReasonDeniedDefault]; got != 1 {
		t.Fatalf("未授权账号应记 1 笔 %q，实际 %v", approval.ReasonDeniedDefault, decisions.Report().ByReason)
	}

	if !ApprovalWhitelistMutate("reach.lead_outreach", "acct-9", time.Time{}, false) {
		t.Fatal("shadow 态下授权入口应可用")
	}
	if res := execCold(t, exec, "reach.lead_outreach", "acct-9"); res.Err != nil {
		t.Fatalf("放行后仍不该报错：%v", res.Err)
	}
	rep := decisions.Report()
	if rep.ByReason[approval.ReasonWhitelisted] != 1 {
		t.Errorf("授权后应出现 1 笔 %q，实际 %v", approval.ReasonWhitelisted, rep.ByReason)
	}
	if rep.WouldDeny != 1 || rep.Total != 2 {
		t.Errorf("两次调用都应外发成功，但判定应记 1 拦 1 放：total=%d would_deny=%d", rep.Total, rep.WouldDeny)
	}
	if cold.count() != 2 {
		t.Errorf("两次都应到达工具本体，实际 %d 次", cold.count())
	}

	// 撤权后再问，回到 denied_default
	if !ApprovalWhitelistMutate("reach.lead_outreach", "acct-9", time.Time{}, true) {
		t.Fatal("撤权入口应可用")
	}
	execCold(t, exec, "reach.lead_outreach", "acct-9")
	if got := decisions.Report().ByReason[approval.ReasonDeniedDefault]; got != 2 {
		t.Errorf("撤权后应回到 %q（累计 2 笔），实际 %v", approval.ReasonDeniedDefault, decisions.Report().ByReason)
	}
}

// TestApprovalWhitelistMutateUnwired 未接线时授权入口必须拒绝，而不是静默写进一个没人读的对象。
func TestApprovalWhitelistMutateUnwired(t *testing.T) {
	restoreApprovalState(t)
	t.Setenv(ApprovalGateFlagEnv, "off")
	config := tooluse.ToolExecutorConfig{}
	applyApprovalGate(&config)
	if ApprovalWhitelistMutate("reach.batch", "acct-1", time.Time{}, false) {
		t.Error("off 态下授权应返回 false")
	}
}

// TestGlobalApprovalCheckerIsShadowSafe 全局注入点不得交出拦人的权限。
//
// approvalTool（WithApproval 那条路）拿到 false 就硬拦、不认识 shadow 字段。
// 今天生产没有它的调用点，但"shadow 不阻断"这条承诺必须对**两条**路都成立，
// 否则 T-P1-06 之后有人按工具粒度包一层，就会拿到一次没被任何旗子授权的拦截。
func TestGlobalApprovalCheckerIsShadowSafe(t *testing.T) {
	restoreApprovalState(t)
	setApprovalWhitelistFlag(t, false)
	t.Setenv(ApprovalGateFlagEnv, "shadow")

	config := tooluse.ToolExecutorConfig{}
	applyApprovalGate(&config)

	cold := newApprovalProbeTool("reach.telegram.dm", tooluse.CategoryReach)
	res, err := tooluse.WithApproval(cold).Execute(
		tooluse.WithToolContext(context.Background(), &tooluse.ToolContext{CallerID: "acct-7"}), nil)
	if err != nil || !res.Success {
		t.Fatalf("shadow 态下 WithApproval 也必须放行，实际 err=%v", err)
	}
	if cold.count() != 1 {
		t.Errorf("放行的调用应到达工具本体，实际 %d 次", cold.count())
	}
	_, decisions := GetApprovalSnapshot()
	if rep := decisions.Report(); rep.WouldDeny != 1 {
		t.Errorf("放行不等于不记录：应有 1 笔 would_deny，实际 %+v", rep)
	}
}

// TestApprovalDecisionAuditLineLandsInLog AC①：冷触达调用真的产生 Decision 审计行。
func TestApprovalDecisionAuditLineLandsInLog(t *testing.T) {
	restoreApprovalState(t)
	setApprovalWhitelistFlag(t, false)
	t.Setenv(ApprovalGateFlagEnv, "shadow")

	logged := captureApprovalLog(t, func() {
		config := tooluse.ToolExecutorConfig{}
		applyApprovalGate(&config)
		cold := newApprovalProbeTool("reach.batch", tooluse.CategoryReach)
		exec := newExecutorWith(cold, config)
		for i := 0; i < 3; i++ {
			execCold(t, exec, "reach.batch", "acct-3")
		}
		// warm 工具一次：用来证明审计行只属于冷触达
		execWarm := newExecutorWith(newApprovalProbeTool("reach.weixin.send", tooluse.CategoryReach), config)
		execCold(t, execWarm, "reach.weixin.send", "acct-3")
	})
	t.Logf("捕获到的审批日志：\n%s", logged)

	var lines int
	for _, line := range strings.Split(logged, "\n") {
		if !strings.Contains(line, `"event":"tool_approval_decision"`) {
			continue
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(line), &parsed); err != nil {
			t.Fatalf("Decision 审计行不是合法 JSON（下游采集会挂）：%v\n%s", err, line)
		}
		for _, key := range []string{"shadow", "would_deny", "allowed", "tool_name", "account", "reason", "whitelist_flag_on"} {
			if _, ok := parsed[key]; !ok {
				t.Errorf("审计行缺字段 %q：%v", key, parsed)
			}
		}
		if parsed["tool_name"] != "reach.batch" {
			t.Errorf("审计行工具名 = %v, want reach.batch", parsed["tool_name"])
		}
		if parsed["account"] != "acct-3" {
			t.Errorf("审计行账号 = %v, want acct-3（owner key 必须是 CallerID）", parsed["account"])
		}
		if parsed["shadow"] != true {
			t.Errorf("shadow 态的审计行必须标 shadow=true：%v", parsed["shadow"])
		}
		if parsed["reason"] != approval.ReasonDisabledByFlag {
			t.Errorf("reason = %v, want %q", parsed["reason"], approval.ReasonDisabledByFlag)
		}
		if parsed["would_deny"] != true || parsed["allowed"] != false {
			t.Errorf("白名单旗子关着 ⇒ would_deny=true/allowed=false，实际 %v/%v", parsed["would_deny"], parsed["allowed"])
		}
		lines++
	}
	if lines != 3 {
		t.Errorf("3 次冷触达应有 3 行 Decision 审计，实际 %d 行（warm 工具不得产生）", lines)
	}

	_, decisions := GetApprovalSnapshot()
	if rep := decisions.Report(); rep.Total != int64(lines) {
		t.Errorf("日志行数 %d 与计数 total=%d 不一致（同源要求）", lines, rep.Total)
	}
}

// TestApprovalGateWarningsRenderCompletely 告警必须把自己要说的原值打出来。
//
// Warnf/Infof 少传一个实参会渲染成 `%!q(MISSING)`，vet 抓不到（logger.Warnf 不在它的
// printf 识别表里）。这类残缺恰好出现在最需要看清"我到底把旗子写成了什么"的那条日志上，
// 所以逐条断言渲染完整，而不是只看它有没有报警。
//
// 三个写法都会告警（错拼 ⇒ 不识别；block/enforce ⇒ 本卡不支持的第三态），
// 但落入的模式不同：错拼只能落 off，别名才落 shadow。这里锁的是"告警说清楚了吗"，
// 模式映射本身由 TestParseApprovalGateMode 逐条覆盖。
func TestApprovalGateWarningsRenderCompletely(t *testing.T) {
	cases := []struct {
		raw      string
		wantMode approvalGateMode
	}{
		{"shodow", approvalGateOff},
		{"block", approvalGateShadow},
		{"enforce", approvalGateShadow},
	}
	for _, c := range cases {
		t.Run(c.raw, func(t *testing.T) {
			logged := captureApprovalLog(t, func() {
				if got := parseApprovalGateMode(c.raw); got != c.wantMode {
					t.Errorf("模式 = %s, want %s", got, c.wantMode)
				}
			})
			if !strings.Contains(logged, c.raw) {
				t.Errorf("告警里看不到原值 %q，运维无从知道自己写错了什么：\n%s", c.raw, logged)
			}
			if strings.Contains(logged, "%!") {
				t.Errorf("告警渲染残缺（fmt 动词与实参不匹配）：\n%s", logged)
			}
			if !strings.Contains(logged, ApprovalGateFlagEnv) {
				t.Errorf("告警未点名旗子 %s：\n%s", ApprovalGateFlagEnv, logged)
			}
		})
	}
}
