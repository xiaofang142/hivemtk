// approval_wiring_test.go T-P1-05 接线 + T-P1-06 三态：冷触达审批门的契约。
//
// T-P1-05 卡面 AC：
//
//	①cold outreach 工具调用产生 Decision 审计行 → TestApprovalDecisionAuditLineLandsInLog
//	②IsApproved=false 时仍放行并打 shadow 标记   → TestApprovalGateShadowLetsColdOutreachThrough
//	③NewWhiteList / SetGlobalApprovalChecker 有非测试调用点 → TestApplyApprovalGateShadowWiresRealWhitelist
//	  （后者断言接上去的确实是 approval 包的真白名单实现，不是随手写的假 checker）
//
// T-P1-06 卡面 AC：
//
//	①block 下 cold outreach 被拒且返回可读原因   → TestApprovalGateBlockDeniesUnapprovedColdOutreach
//	②off/shadow/block 三态各有测试               → TestParseApprovalGateMode（映射）
//	  + TestApplyApprovalGateOffLeavesExecutorUnchanged / …ShadowWiresRealWhitelist /
//	    …BlockWiresGatedChecker（每态各自的接线后果）
//	③shadow 期对比报告（多少调用会被拦）          → would_deny 口径在 block 后不变：
//	  TestApprovalGateBlockKeepsWouldDenyMeaning
//
// 另有三条不属于卡面、但灰度更依赖的：
//
//	关旗必须逐字回到"没接过线"（TestApplyApprovalGateOffLeavesExecutorUnchanged）
//	shadow 的全局注入点不得交出拦人的权限（TestGlobalApprovalCheckerIsShadowSafe）
//	block 的刹车：白名单旗子没开时不拦（TestApprovalGateBlockBrakeWithoutWhitelistFlag）
//
// 本文件全部用**真**的 WhiteListApprovalChecker + 真 featureflag：审批门的危险恰恰不在
// 装饰器逻辑，而在"默认拒绝"这个语义被接到生产上，用假 checker 测不出来。
package app

import (
	"context"
	"encoding/json"
	"errors"
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
	t.Setenv(featureflag.EnvNameOf(approval.FlagKey), value)
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
		// 布尔真值只到 shadow：写 true 的人未必知道"true"会把客户的冷触达拒掉，
		// 转阻断必须由字面量表达（告警文案由 TestApprovalGateWarningsRenderCompletely 锁）
		{"true", approvalGateShadow},
		{"1", approvalGateShadow},
		{"yes", approvalGateShadow},
		{"on", approvalGateShadow},
		// T-P1-06：第三态只有这三个字面量进 block
		{"block", approvalGateBlock},
		{"BLOCK", approvalGateBlock},
		{" block ", approvalGateBlock},
		{"enforce", approvalGateBlock},
		{"active", approvalGateBlock},
		// 认不出的值判 off：把旗子拼错的人不该意外获得一个"看起来开了"的闸门，
		// 更不该因为拼错"shadow"而拿到 block
		{"shodow", approvalGateOff},
		{"blok", approvalGateOff},
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
		t.Error("shadow 态 config.ApprovalShadow 必须为 true（只有 block 态才允许 false）")
	}
	if _, ok := config.ApprovalChecker.(*approval.WhiteListApprovalChecker); !ok {
		t.Fatalf("接的是 %T，不是 *approval.WhiteListApprovalChecker", config.ApprovalChecker)
	}
	if snap, _ := GetApprovalSnapshot(); snap.BlocksWhenDenied {
		t.Error("shadow 态快照必须报 blocks_when_denied=false：拒绝不会真的传下去")
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
		for _, key := range []string{"mode", "would_deny", "blocked", "allowed", "tool_name", "account", "reason", "whitelist_flag_on"} {
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
		if parsed["mode"] != string(approvalGateShadow) {
			t.Errorf("审计行 mode = %v, want %q", parsed["mode"], approvalGateShadow)
		}
		if parsed["blocked"] != false {
			t.Errorf("shadow 态一条都不该真拦，blocked 必须恒 false：%v", parsed["blocked"])
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
// 会告警的写法分两类：解析期（错拼 ⇒ 落 off；布尔真值 ⇒ 落 shadow）与
// 装配期（block 的三句：刹车生效 / 有效条目=0 / 行为变更）。落点映射本身由
// TestParseApprovalGateMode 逐条覆盖，这里锁的是"告警说清楚了吗"。
func TestApprovalGateWarningsRenderCompletely(t *testing.T) {
	cases := []struct {
		raw      string
		wantMode approvalGateMode
	}{
		{"shodow", approvalGateOff},
		{"true", approvalGateShadow},
		{"yes", approvalGateShadow},
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
			// 布尔真值的告警必须把可用的三态列全，否则运维下一轮只会换个别的真值再试一次
			if c.wantMode == approvalGateShadow && !strings.Contains(logged, "off|shadow|block") {
				t.Errorf("布尔真值的告警没列出可用三态：\n%s", logged)
			}
		})
	}
}

// TestApprovalGateBlockWarningsRenderCompletely block 态的三句装配告警逐个验渲染。
//
// 这三句是转阻断时唯一会读到"我这么开旗到底拦不拦人"的地方，任何一个 %s 漏了实参
// 都会把刹车说明变成一串 %!q(MISSING)，而那正是最需要看懂的一行。
func TestApprovalGateBlockWarningsRenderCompletely(t *testing.T) {
	cases := []struct {
		name    string
		flagOn  bool
		grant   bool
		want    []string
		notWant []string
	}{
		{
			// 两把旗子只到位一把：必须既说清刹车，又说清"条目还是 0"
			name:   "白名单旗子未开 ⇒ 报刹车",
			flagOn: false,
			want: []string{"刹车生效", approval.ReasonDisabledByFlag,
				featureflag.EnvNameOf(approval.FlagKey), "有效白名单条目=0"},
		},
		{
			name:    "两把旗子都开 ⇒ 不报刹车，但空表仍要单独告警",
			flagOn:  true,
			grant:   true,
			want:    []string{"模式=block", "有效白名单条目=0", "行为变更", "所有**冷触达都会被拒", "白名单变更 grant"},
			notWant: []string{"刹车生效"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			restoreApprovalState(t)
			setApprovalWhitelistFlag(t, c.flagOn)
			t.Setenv(ApprovalGateFlagEnv, "block")
			logged := captureApprovalLog(t, func() {
				config := tooluse.ToolExecutorConfig{}
				applyApprovalGate(&config)
				if !c.grant {
					return
				}
				if !ApprovalWhitelistMutate("reach.batch", "acct-1", time.Time{}, false) {
					t.Error("授权入口应可用")
				}
			})
			if strings.Contains(logged, "%!") {
				t.Errorf("block 告警渲染残缺：\n%s", logged)
			}
			for _, want := range c.want {
				if !strings.Contains(logged, want) {
					t.Errorf("block 告警里缺 %q：\n%s", want, logged)
				}
			}
			for _, notWant := range c.notWant {
				if strings.Contains(logged, notWant) {
					t.Errorf("block 告警里出现了不该出现的 %q：\n%s", notWant, logged)
				}
			}
			if !strings.Contains(logged, ApprovalGateFlagEnv) {
				t.Errorf("block 告警未点名旗子：\n%s", logged)
			}
			if c.grant && !strings.Contains(logged, "白名单变更 grant") {
				t.Errorf("授权变更必须留痕（含变更后的有效条目数）：\n%s", logged)
			}
		})
	}
}

// TestApprovalWhitelistMutateLogsActiveEntries 授权留痕必须带上"改完还剩多少条"。
//
// block 态 revoke 是唯一能立刻把在跑的账号关掉的动作，事后要能回答
// "几点几分谁把它撤了、撤完表里还有几条"。
func TestApprovalWhitelistMutateLogsActiveEntries(t *testing.T) {
	restoreApprovalState(t)
	setApprovalWhitelistFlag(t, true)
	t.Setenv(ApprovalGateFlagEnv, "block")

	config := tooluse.ToolExecutorConfig{}
	applyApprovalGate(&config)

	logged := captureApprovalLog(t, func() {
		if !ApprovalWhitelistMutate("reach.batch", "acct-a", time.Time{}, false) {
			t.Error("grant 应成功")
		}
		expire := time.Now().Add(-time.Minute)
		if !ApprovalWhitelistMutate("reach.batch", "acct-b", expire, false) {
			t.Error("带过期时间的 grant 也应成功入账（判定时才判过期）")
		}
		if !ApprovalWhitelistMutate("reach.batch", "acct-a", time.Time{}, true) {
			t.Error("revoke 应成功")
		}
	})
	for _, want := range []string{"白名单变更 grant", "白名单变更 revoke", "有效条目=1", "有效条目=0"} {
		if !strings.Contains(logged, want) {
			t.Errorf("留痕缺 %q：\n%s", want, logged)
		}
	}
	if strings.Contains(logged, "%!") {
		t.Errorf("留痕渲染残缺：\n%s", logged)
	}
	// 终态：acct-a 已撤，acct-b 那条虽然还在表里但已过期 ⇒ 有效条目必须是 0。
	// 这个数字是 block 态放量前的自检读数，把过期项算进去就等于读数永远偏大。
	if snap, _ := GetApprovalSnapshot(); snap.WhitelistActiveEntries != 0 {
		t.Errorf("快照里的有效条目 = %d, want 0（已撤 + 已过期都不计）：%+v", snap.WhitelistActiveEntries, snap)
	}
}

// --- T-P1-06：block 态 ---

// TestApplyApprovalGateBlockWiresGatedChecker block 态的接线后果（AC② 第三态）。
//
// 这里盯的是两件容易在后续改动里偷偷退化的事：
//  1. ApprovalShadow 必须翻成 false —— 否则装饰器只会记不会拦，block 名存实亡；
//  2. 装饰器侧必须是 blockApprovalChecker 而不是裸 checker —— 否则刹车丢失，
//     白名单旗子没开时会立刻变成"全量冷触达无差别失败"。
func TestApplyApprovalGateBlockWiresGatedChecker(t *testing.T) {
	restoreApprovalState(t)
	setApprovalWhitelistFlag(t, true)
	t.Setenv(ApprovalGateFlagEnv, "block")

	config := tooluse.ToolExecutorConfig{}
	if mode := applyApprovalGate(&config); mode != approvalGateBlock {
		t.Fatalf("模式 = %s, want block", mode)
	}
	if config.ApprovalShadow {
		t.Error("block 态 config.ApprovalShadow 必须为 false")
	}
	if _, ok := config.ApprovalChecker.(blockApprovalChecker); !ok {
		t.Fatalf("block 态装饰器侧接的是 %T，必须是 blockApprovalChecker", config.ApprovalChecker)
	}
	snap, decisions := GetApprovalSnapshot()
	if !snap.Wired || !snap.GlobalCheckerSet || decisions == nil {
		t.Errorf("block 接线后快照应完整：%+v decisions==nil? %v", snap, decisions == nil)
	}
	if !snap.BlocksWhenDenied {
		t.Error("block 态必须报 blocks_when_denied=true")
	}
	if snap.Mode != string(approvalGateBlock) {
		t.Errorf("快照 mode = %q, want block", snap.Mode)
	}
	if snap.WhitelistFlagEnv != featureflag.EnvNameOf(approval.FlagKey) {
		t.Errorf("快照 WhitelistFlagEnv = %q，必须与 featureflag 的实际推导同源", snap.WhitelistFlagEnv)
	}
	if snap.WhitelistActiveEntries != 0 {
		t.Errorf("新接线的白名单应为空，实际 %d 条", snap.WhitelistActiveEntries)
	}

	// 全局注入点在 block 态交出的是同一个有裁决权的 gate：WithApproval 那条路也必须拦，
	// 否则同一个工具经两条路进来会得到两种后果，灰度读数无法解释。
	cold := newApprovalProbeTool("reach.telegram.dm", tooluse.CategoryReach)
	res, err := tooluse.WithApproval(cold).Execute(
		tooluse.WithToolContext(context.Background(), &tooluse.ToolContext{CallerID: "acct-x"}), nil)
	if err == nil || res.Success {
		t.Errorf("block + 白名单旗子已开 + 未授权 ⇒ WithApproval 那条路也必须拒，实际 err=%v", err)
	}
	if cold.count() != 0 {
		t.Errorf("被拒的调用不该到达工具本体，实际 %d 次", cold.count())
	}
}

// TestApprovalGateBlockDeniesUnapprovedColdOutreach AC①：block 下冷触达被拒且原因可读。
func TestApprovalGateBlockDeniesUnapprovedColdOutreach(t *testing.T) {
	restoreApprovalState(t)
	setApprovalWhitelistFlag(t, true)
	t.Setenv(ApprovalGateFlagEnv, "block")

	config := tooluse.ToolExecutorConfig{}
	applyApprovalGate(&config)
	cold := newApprovalProbeTool("reach.batch_send.sms", tooluse.CategoryReach)
	// 显式带重试策略：拒绝必须是终止语义，重试一次就是对着同一堵墙多发一次
	config.RetryPolicy = tooluse.NewExponentialBackoffPolicy(3, time.Millisecond, 5*time.Millisecond)
	config.AuditLogger = tooluse.NewMemoryAuditLogger(10)
	exec := newExecutorWith(cold, config)

	res := execCold(t, exec, "reach.batch_send.sms", "acct-1")
	if res.Err == nil || res.Success {
		t.Fatalf("未授权账号的冷触达必须被拒，实际 err=%v", res.Err)
	}
	if !errors.Is(res.Err, tooluse.ErrApprovalDenied) {
		t.Errorf("拒绝必须是 ErrApprovalDenied，实际 %v", res.Err)
	}
	if got := tooluse.ClassifyToolError(res.Err); got != tooluse.ToolErrApprovalDenied {
		t.Errorf("错误码 = %s, want %s（归错类会被当成可重试错误）", got, tooluse.ToolErrApprovalDenied)
	}
	for _, want := range []string{"reach.batch_send.sms", "approval"} {
		if !strings.Contains(res.Err.Error(), want) {
			t.Errorf("拒绝原因里读不到 %q：%s", want, res.Err.Error())
		}
	}
	if cold.count() != 0 {
		t.Errorf("被拒的外发一次都不该到达工具本体（含重试），实际 %d 次", cold.count())
	}

	_, decisions := GetApprovalSnapshot()
	rep := decisions.Report()
	if rep.Total != 1 || rep.WouldDeny != 1 {
		t.Errorf("block 态的判定口径须与 shadow 一致（total/would_deny 各 1），实际 %+v", rep)
	}
	if rep.ByReason[approval.ReasonDeniedDefault] != 1 {
		t.Errorf("未授权账号的 reason 应为 %q，实际 %v", approval.ReasonDeniedDefault, rep.ByReason)
	}
}

// TestApprovalGateBlockBrakeWithoutWhitelistFlag 刹车：白名单旗子没开时 block 不拦。
//
// 同时证明"这不是留了个后门"：旗子没开时**即使灌了授权也不生效**，
// 所以放行不是"绕过了白名单"，而是"白名单这一侧根本没有裁决可依"。
func TestApprovalGateBlockBrakeWithoutWhitelistFlag(t *testing.T) {
	restoreApprovalState(t)
	setApprovalWhitelistFlag(t, false)
	t.Setenv(ApprovalGateFlagEnv, "block")

	config := tooluse.ToolExecutorConfig{}
	applyApprovalGate(&config)
	cold := newApprovalProbeTool("reach.batch", tooluse.CategoryReach)
	exec := newExecutorWith(cold, config)

	if !ApprovalWhitelistMutate("reach.batch", "acct-brake", time.Time{}, false) {
		t.Fatal("授权入口在 block 态必须可用")
	}
	logged := captureApprovalLog(t, func() {
		res := execCold(t, exec, "reach.batch", "acct-brake")
		if res.Err != nil || !res.Success {
			t.Errorf("白名单旗子没开 ⇒ 刹车必须放行，实际 err=%v", res.Err)
		}
	})
	if cold.count() != 1 {
		t.Errorf("刹车放行后必须真的外发，实际 %d 次", cold.count())
	}

	_, decisions := GetApprovalSnapshot()
	rep := decisions.Report()
	if rep.Total != 1 || rep.WouldDeny != 1 {
		t.Errorf("刹车放行不改判定口径：应仍记 1 笔 would_deny，实际 %+v", rep)
	}
	if rep.ByReason[approval.ReasonDisabledByFlag] != 1 {
		t.Errorf("reason 应为 %q（证明授权确实没生效），实际 %v", approval.ReasonDisabledByFlag, rep.ByReason)
	}
	if !strings.Contains(logged, `"blocked":false`) {
		t.Errorf("审计行必须标 blocked=false（会被拦但没拦）：\n%s", logged)
	}
	if !strings.Contains(logged, `"would_deny":true`) {
		t.Errorf("审计行的 would_deny 不能因放行而被抹掉：\n%s", logged)
	}
}

// TestApprovalGateBlockGrantRevokeAndExpiry block 态的三种结局：放行 / 撤权即拒 / 过期拒。
func TestApprovalGateBlockGrantRevokeAndExpiry(t *testing.T) {
	restoreApprovalState(t)
	setApprovalWhitelistFlag(t, true)
	t.Setenv(ApprovalGateFlagEnv, "block")

	config := tooluse.ToolExecutorConfig{}
	applyApprovalGate(&config)
	cold := newApprovalProbeTool("reach.schedule", tooluse.CategoryReach)
	exec := newExecutorWith(cold, config)
	_, decisions := GetApprovalSnapshot()

	// ① 授权后放行
	if !ApprovalWhitelistMutate("reach.schedule", "acct-ok", time.Time{}, false) {
		t.Fatal("grant 应成功")
	}
	if res := execCold(t, exec, "reach.schedule", "acct-ok"); res.Err != nil || !res.Success {
		t.Fatalf("已授权账号必须放行，实际 err=%v", res.Err)
	}
	if got := decisions.Report().ByReason[approval.ReasonWhitelisted]; got != 1 {
		t.Errorf("应记 1 笔 %q，实际 %v", approval.ReasonWhitelisted, decisions.Report().ByReason)
	}
	// ② 撤权后立刻被拒（运行期改表是真有后果的）
	if !ApprovalWhitelistMutate("reach.schedule", "acct-ok", time.Time{}, true) {
		t.Fatal("revoke 应成功")
	}
	res := execCold(t, exec, "reach.schedule", "acct-ok")
	if res.Err == nil || res.Success {
		t.Error("撤权后下一次冷触达必须被拒")
	}
	if got := decisions.Report().ByReason[approval.ReasonDeniedDefault]; got != 1 {
		t.Errorf("撤权后应回到 %q，实际 %v", approval.ReasonDeniedDefault, decisions.Report().ByReason)
	}
	// ③ 过期条目：入账时不判过期，判定时才拒（reason=denied_explicit，与"没开过"区分开）
	if !ApprovalWhitelistMutate("reach.schedule", "acct-exp", time.Now().Add(-time.Minute), false) {
		t.Fatal("grant 应成功")
	}
	res = execCold(t, exec, "reach.schedule", "acct-exp")
	if res.Err == nil || res.Success {
		t.Error("已过期的授权必须被拒")
	}
	rep := decisions.Report()
	if rep.ByReason[approval.ReasonDeniedExplicit] != 1 {
		t.Errorf("过期条目应记 %q，实际 %v", approval.ReasonDeniedExplicit, rep.ByReason)
	}
	// 三次调用里只第一次真的外发
	if cold.count() != 1 {
		t.Errorf("只放行的那次该到达工具本体，实际 %d 次", cold.count())
	}
	if rep.Total != 3 || rep.WouldDeny != 2 {
		t.Errorf("total/would_deny 应为 3/2，实际 %d/%d", rep.Total, rep.WouldDeny)
	}
}

// TestApprovalGateBlockWarmToolsUntouched block 不得外溢到非冷触达工具。
func TestApprovalGateBlockWarmToolsUntouched(t *testing.T) {
	restoreApprovalState(t)
	setApprovalWhitelistFlag(t, true)
	t.Setenv(ApprovalGateFlagEnv, "block")

	config := tooluse.ToolExecutorConfig{}
	applyApprovalGate(&config)
	warm := newApprovalProbeTool("reach.weixin.send", tooluse.CategoryReach)
	exec := newExecutorWith(warm, config)

	if res := execCold(t, exec, "reach.weixin.send", "acct-none"); res.Err != nil || !res.Success {
		t.Fatalf("warm 工具在 block 态也必须原样执行，实际 err=%v", res.Err)
	}
	if _, decisions := GetApprovalSnapshot(); decisions.Report().Total != 0 {
		t.Errorf("warm 工具不该被审批门询问：%+v", decisions.Report())
	}
}

// TestApprovalGateBlockKeepsWouldDenyMeaning AC③：观察期报告的口径在转阻断后仍然成立。
//
// would_deny 永远是"切阻断后会被拦的量"，与实际拦没拦无关。这条把两个模式的读数
// 放在同一个用例里对比（1 放行 + 1 拒 ⇒ 两个模式下都是 total=2/would_deny=1），
// 否则报告在转阻断当天就失去可比性，而那份报告正是转阻断的准入证据。
//
// 同时逐模式锁审计行的 blocked：shadow 一次都不该为 true（哪怕判定是拒绝），
// block 才有 true。少了这半截，"blocked 只看判定不看模式"这种改动能在 shadow 期
// 造出"已经拦下了"的假日志，而读数看起来一切正常。
func TestApprovalGateBlockKeepsWouldDenyMeaning(t *testing.T) {
	coldName := "reach.batch_outbound"
	for _, c := range []struct {
		mode        approvalGateMode
		wantBlocked bool
	}{
		{approvalGateShadow, false},
		{approvalGateBlock, true},
	} {
		t.Run(string(c.mode), func(t *testing.T) {
			restoreApprovalState(t)
			setApprovalWhitelistFlag(t, true)
			t.Setenv(ApprovalGateFlagEnv, string(c.mode))

			config := tooluse.ToolExecutorConfig{}
			applyApprovalGate(&config)
			cold := newApprovalProbeTool(coldName, tooluse.CategoryReach)
			exec := newExecutorWith(cold, config)
			if !ApprovalWhitelistMutate(coldName, "acct-granted", time.Time{}, false) {
				t.Fatal("grant 应成功")
			}

			logged := captureApprovalLog(t, func() {
				execCold(t, exec, coldName, "acct-granted")
				res := execCold(t, exec, coldName, "acct-denied")
				// 唯一按模式分叉的行为断言：block 拒掉未授权那次，shadow 一单不拦
				if got := res.Err == nil; got != (c.mode == approvalGateShadow) {
					t.Errorf("mode=%s 时放行与否判错：err=%v", c.mode, res.Err)
				}
			})

			_, decisions := GetApprovalSnapshot()
			rep := decisions.Report()
			if rep.Total != 2 || rep.WouldDeny != 1 || rep.WouldDenyRatePct != 50 {
				t.Errorf("两种模式的读数口径必须一致，实际 %+v", rep)
			}
			if snap, _ := GetApprovalSnapshot(); snap.BlocksWhenDenied != (c.mode == approvalGateBlock) {
				t.Errorf("blocks_when_denied 没跟上模式：mode=%s snap=%+v", c.mode, snap)
			}

			blockedLines := strings.Count(logged, `"blocked":true`)
			wantLines := 0
			if c.wantBlocked {
				wantLines = 1
			}
			if blockedLines != wantLines {
				t.Errorf("mode=%s 的 blocked=true 行数 = %d, want %d：\n%s",
					c.mode, blockedLines, wantLines, logged)
			}
			if denied := strings.Count(logged, `"would_deny":true`); denied != 1 {
				t.Errorf("mode=%s 应有且只有 1 行 would_deny=true，实际 %d：\n%s", c.mode, denied, logged)
			}
		})
	}
}
