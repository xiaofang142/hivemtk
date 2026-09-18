// tool_circuit_breaker_wiring_test.go T-P1-04：工具熔断接线的三条契约。
//
// 卡面 AC：
//
//	①注入连续失败后审计日志出现熔断判定记录 → TestToolCircuitDecisionLandsInAuditLog
//	  （真抓日志：把 os.Stdout 换成管道，再跑够触发判定的调用）
//	②shadow 模式下放行行为不变（有断言测试） → TestToolCircuitShadowKeepsToolCallsIdentical
//	③熔断阈值走配置不硬编码 → TestParseToolCircuitMode / TestToolCircuitConfigFromEnv
//
// 另有一条不属于卡面但更要紧的：关旗时 executor 配置必须回到"没人接熔断"的原样
// （TestApplyToolCircuitBreakerOffLeavesExecutorUnchanged），否则"默认 off"只是注释里的承诺。
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
	"hivemtk-user/internal/pkg/utils/logger"
)

// restoreCircuitState 保存/还原包级熔断状态。
//
// 必须还原：本包其它测试（以及未来的接线测试）会读同一组全局变量，
// 只靠 t.Setenv 复原环境变量不够——applyToolCircuitBreaker 已经把 registry 写进去了。
func restoreCircuitState(t *testing.T) {
	t.Helper()
	om, oc, or, od := circuitModeValue, circuitConfigApplied, circuitRegistry, circuitDecisions
	t.Cleanup(func() {
		circuitModeValue, circuitConfigApplied, circuitRegistry, circuitDecisions = om, oc, or, od
	})
}

type circuitProbeTool struct {
	tooluse.BaseTool
	mu    sync.Mutex
	calls int
}

func newCircuitProbeTool(name string) *circuitProbeTool {
	return &circuitProbeTool{BaseTool: tooluse.BaseTool{
		NameVal:     name,
		CategoryVal: tooluse.CategoryCustomer,
		ParamsVal:   tooluse.ToolParameters{Type: "object", Properties: map[string]tooluse.ToolParam{}},
	}}
}

func (p *circuitProbeTool) Execute(ctx context.Context, args map[string]any) (tooluse.ToolResult, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	err := errors.New("downstream unavailable")
	return tooluse.ErrorResult(p.NameVal, err), err
}

func (p *circuitProbeTool) called() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func newExecutorWithCircuit(t *testing.T, cfg tooluse.ToolExecutorConfig) (*tooluse.ToolExecutor, *circuitProbeTool, string) {
	t.Helper()
	name := "circuit.probe"
	tool := newCircuitProbeTool(name)
	reg := tooluse.NewToolRegistry()
	if err := reg.Register(tool); err != nil {
		t.Fatalf("注册探针工具失败：%v", err)
	}
	cfg.DefaultTimeout = 5 * time.Second
	return tooluse.NewToolExecutor(reg, cfg), tool, name
}

func TestParseToolCircuitMode(t *testing.T) {
	cases := []struct {
		raw  string
		want toolCircuitMode
	}{
		{"", toolCircuitOff},
		{"   ", toolCircuitOff},
		{"off", toolCircuitOff},
		{"OFF", toolCircuitOff},
		{"false", toolCircuitOff},
		{"0", toolCircuitOff},
		{"no", toolCircuitOff},
		{"disabled", toolCircuitOff},
		{"shadow", toolCircuitShadow},
		{" Shadow ", toolCircuitShadow},
		{"observe", toolCircuitShadow},
		{"watch", toolCircuitShadow},
		{"enforce", toolCircuitEnforce},
		{"BLOCK", toolCircuitEnforce},
		{"active", toolCircuitEnforce},
		// 布尔真值只给 shadow：一把能改生产行为的旗子不该因有人按习惯写 true 就拿到拦截能力
		{"true", toolCircuitShadow},
		{"TRUE", toolCircuitShadow},
		{"1", toolCircuitShadow},
		{"yes", toolCircuitShadow},
		{"on", toolCircuitShadow},
		{"y", toolCircuitShadow},
		// 认不出一律判关
		{" enable ", toolCircuitOff},
		{"enable", toolCircuitOff},
		{"ture", toolCircuitOff},
		{"2", toolCircuitOff},
		{"-1", toolCircuitOff},
		{"yesno", toolCircuitOff},
		{"影子", toolCircuitOff},
	}
	for _, tt := range cases {
		if got := parseToolCircuitMode(tt.raw); got != tt.want {
			t.Errorf("parseToolCircuitMode(%q) = %s，期望 %s", tt.raw, got, tt.want)
		}
	}
}

func TestToolCircuitConfigFromEnvDefaults(t *testing.T) {
	for _, k := range []string{
		circuitThresholdEnv, circuitBaseCooldownEnv, circuitMaxCooldownEnv, circuitBackoffEnv, circuitHalfOpenEnv,
	} {
		t.Setenv(k, "")
	}
	if got := toolCircuitConfigFromEnv(); got != tooluse.DefaultCircuitBreakerConfig() {
		t.Fatalf("未配环境变量时应逐字沿用 tooluse 默认配置，实际 %+v", got)
	}
}

func TestToolCircuitConfigFromEnvOverrides(t *testing.T) {
	t.Setenv(circuitThresholdEnv, "12")
	t.Setenv(circuitBaseCooldownEnv, "45s")
	t.Setenv(circuitMaxCooldownEnv, "12m")
	t.Setenv(circuitBackoffEnv, "3.5")
	t.Setenv(circuitHalfOpenEnv, "4")

	got := toolCircuitConfigFromEnv()
	want := tooluse.CircuitBreakerConfig{
		FailureThreshold: 12, BaseCooldown: 45 * time.Second, MaxCooldown: 12 * time.Minute,
		BackoffMultiplier: 3.5, HalfOpenMaxAttempts: 4,
	}
	if got != want {
		t.Fatalf("AC③ 五项阈值未全部落到配置上：\n got %+v\nwant %+v", got, want)
	}
}

func TestToolCircuitConfigFromEnvRejectsBadValues(t *testing.T) {
	// 基线：默认配置（threshold=5 base=30s max=5m backoff=2 halfopen=1）
	t.Setenv(circuitThresholdEnv, "0")
	got := toolCircuitConfigFromEnv()
	if got.FailureThreshold != 5 {
		t.Fatalf("threshold=0（会让每个请求都被拒）不应被采纳，实际 %+v", got)
	}

	t.Setenv(circuitThresholdEnv, "100000")
	if got := toolCircuitConfigFromEnv(); got.FailureThreshold != 5 {
		t.Fatalf("越界 threshold 不应被采纳，实际 %+v", got)
	}

	t.Setenv(circuitThresholdEnv, "abc")
	if got := toolCircuitConfigFromEnv(); got.FailureThreshold != 5 {
		t.Fatalf("非整数 threshold 不应被采纳，实际 %+v", got)
	}

	// 常见事故一：想写 30 秒只写了 30 ⇒ ParseDuration 直接报错，回退默认 30s
	t.Setenv(circuitThresholdEnv, "8")
	t.Setenv(circuitBaseCooldownEnv, "30")
	if got := toolCircuitConfigFromEnv(); got.BaseCooldown != 30*time.Second {
		t.Fatalf("裸数字时长不应被采纳，实际 %s", got.BaseCooldown)
	}
	// 常见事故二：单位写错成纳秒（能解析但量级差 9 个数量级）⇒ 低于下限，回退默认 30s
	t.Setenv(circuitBaseCooldownEnv, "1ns")
	if got := toolCircuitConfigFromEnv(); got.BaseCooldown != 30*time.Second {
		t.Fatalf("亚毫秒冷却不应被采纳（等于没有熔断），实际 %s", got.BaseCooldown)
	}

	t.Setenv(circuitBaseCooldownEnv, "10m")
	t.Setenv(circuitMaxCooldownEnv, "5s")
	if got := toolCircuitConfigFromEnv(); got.MaxCooldown < got.BaseCooldown {
		t.Fatalf("max<base 时退避上限应被抬起（否则指数退避被反向夹住），实际 %+v", got)
	}

	t.Setenv(circuitMaxCooldownEnv, "")
	t.Setenv(circuitBackoffEnv, "0.5")
	if got := toolCircuitConfigFromEnv(); got.BackoffMultiplier != 2.0 {
		t.Fatalf("<1 的退避倍数不应被采纳，实际 %+v", got)
	}
}

func TestApplyToolCircuitBreakerOffLeavesExecutorUnchanged(t *testing.T) {
	restoreCircuitState(t)
	t.Setenv(circuitFlagEnv, "")

	config := tooluse.ToolExecutorConfig{
		DefaultTimeout: 30 * time.Second,
		RateLimiter:    tooluse.NewTokenBucketLimiter(20, 50),
		// 预置一个 registry：off 分支必须把它清掉，否则"关旗 = 与接线前一致"只是注释里的承诺，
		// 而装配顺序若将来被挪到别处（先挂熔断再判旗），这里就是唯一的锁。
		CircuitBreaker:       tooluse.NewCircuitBreakerRegistry(tooluse.DefaultCircuitBreakerConfig()),
		CircuitBreakerShadow: true,
		OnCircuitDecision:    func(context.Context, tooluse.CircuitDecision) {},
	}
	before := config
	if mode := applyToolCircuitBreaker(&config); mode != toolCircuitOff {
		t.Fatalf("未配开关应为 off，实际 %s", mode)
	}
	if config.CircuitBreaker != nil {
		t.Error("off 时 CircuitBreaker 必须为 nil（非 nil 会让熔断进装饰链）")
	}
	if config.CircuitBreakerShadow || config.OnCircuitDecision != nil {
		t.Error("off 时 shadow 标志与判定回调都应清空")
	}
	if config.DefaultTimeout != before.DefaultTimeout || config.RateLimiter != before.RateLimiter {
		t.Error("apply 不应动无关字段")
	}
	if mode, _, registry, decisions := GetToolCircuitSnapshot(); mode != "off" || registry != nil || decisions != nil {
		t.Fatalf("off 时快照应整体为空：%s registry=%v decisions=%v", mode, registry, decisions)
	}

	tool := newCircuitProbeTool("off.tool")
	reg := tooluse.NewToolRegistry()
	_ = reg.Register(tool)
	exec := tooluse.NewToolExecutor(reg, config)
	for i := 0; i < 20; i++ {
		if res := exec.Execute(context.Background(), tooluse.ExecuteRequest{ToolName: "off.tool"}); errors.Is(res.Err, tooluse.ErrCircuitOpen) {
			t.Fatalf("关旗后第 %d 次调用被拦 ⇒ 熔断事实上仍生效", i+1)
		}
	}
	if tool.called() != 20 {
		t.Fatalf("关旗时 20 次调用应全部到达工具本体，实际 %d", tool.called())
	}
}

func TestApplyToolCircuitBreakerShadowAndEnforceFlags(t *testing.T) {
	cases := []struct {
		env         string
		wantMode    toolCircuitMode
		wantShadow  bool
		wantWired   bool
		wantReports bool
	}{
		{"shadow", toolCircuitShadow, true, true, true},
		{"enforce", toolCircuitEnforce, false, true, true},
		{"true", toolCircuitShadow, true, true, true},
		{"garbage", toolCircuitOff, false, false, false},
	}
	for _, tt := range cases {
		t.Run(tt.env, func(t *testing.T) {
			restoreCircuitState(t)
			t.Setenv(circuitFlagEnv, tt.env)
			t.Setenv(circuitThresholdEnv, "2")

			var config tooluse.ToolExecutorConfig
			mode := applyToolCircuitBreaker(&config)
			if mode != tt.wantMode {
				t.Fatalf("模式 = %s，期望 %s", mode, tt.wantMode)
			}
			if (config.CircuitBreaker != nil) != tt.wantWired {
				t.Fatalf("registry 是否挂载 = %v，期望 %v", config.CircuitBreaker != nil, tt.wantWired)
			}
			if config.CircuitBreakerShadow != tt.wantShadow {
				t.Fatalf("shadow 标志 = %v，期望 %v", config.CircuitBreakerShadow, tt.wantShadow)
			}
			if (config.OnCircuitDecision != nil) != tt.wantReports {
				t.Fatalf("判定回调 = %v，期望 %v", config.OnCircuitDecision != nil, tt.wantReports)
			}
			if tt.wantWired {
				if got := config.CircuitBreaker.Config().FailureThreshold; got != 2 {
					t.Fatalf("阈值未落到挂载的 registry 上：%d", got)
				}
			}
		})
	}
}

// TestToolCircuitShadowKeepsToolCallsIdentical AC②：走生产接线函数装配出的 executor，
// shadow 下的调用序列必须与"根本没接熔断"逐次一致。
func TestToolCircuitShadowKeepsToolCallsIdentical(t *testing.T) {
	const calls = 10

	// 对照组：关旗
	restoreCircuitState(t)
	t.Setenv(circuitFlagEnv, "off")
	t.Setenv(circuitThresholdEnv, "2")
	offConfig := tooluse.ToolExecutorConfig{}
	applyToolCircuitBreaker(&offConfig)
	offExec, offTool, name := newExecutorWithCircuit(t, offConfig)
	offResults := make([]string, 0, calls)
	for i := 0; i < calls; i++ {
		offResults = append(offResults, probeOutcome(offExec.Execute(context.Background(), tooluse.ExecuteRequest{ToolName: name})))
	}
	if offTool.called() != calls {
		t.Fatalf("对照组应有 %d 次到达下游，实际 %d", calls, offTool.called())
	}

	// 实验组：shadow，同一工具、同样次数
	restoreCircuitState(t)
	t.Setenv(circuitFlagEnv, "shadow")
	t.Setenv(circuitThresholdEnv, "2")
	shadowConfig := tooluse.ToolExecutorConfig{}
	if mode := applyToolCircuitBreaker(&shadowConfig); mode != toolCircuitShadow {
		t.Fatalf("模式 = %s，期望 shadow", mode)
	}
	shadowExec, shadowTool, name2 := newExecutorWithCircuit(t, shadowConfig)
	shadowResults := make([]string, 0, calls)
	blocked := 0
	for i := 0; i < calls; i++ {
		res := shadowExec.Execute(context.Background(), tooluse.ExecuteRequest{ToolName: name2})
		shadowResults = append(shadowResults, probeOutcome(res))
		if errors.Is(res.Err, tooluse.ErrCircuitOpen) {
			blocked++
		}
	}
	if blocked != 0 {
		t.Fatalf("shadow 下出现 %d 次 ErrCircuitOpen，放行行为已变", blocked)
	}
	if shadowTool.called() != calls {
		t.Errorf("shadow 下应 %d 次全部到达下游，实际 %d 次", calls, shadowTool.called())
	}
	for i := range offResults {
		if offResults[i] != shadowResults[i] {
			t.Fatalf("第 %d 次调用结果与关旗不一致：\n off=%s\n shadow=%s", i+1, offResults[i], shadowResults[i])
		}
	}
	// 前置：熔断确实打开过，否则"一致"是因为没接线
	if st := shadowConfig.CircuitBreaker.State(name2); st != tooluse.CircuitOpen {
		t.Fatalf("前置条件不成立：shadow 跑 %d 次后应为 open，实际 %s", calls, st)
	}
	// 判定报告必须已把"本会被拦"的次数记下来（AC①的另一半：可统计）
	_, _, _, decisions := GetToolCircuitSnapshot()
	if decisions == nil {
		t.Fatal("shadow 下判定累计器不应为 nil")
	}
	rep := decisions.Report()
	if rep.Total != calls || rep.WouldBlock != calls-2 {
		t.Fatalf("判定报告错：%+v（阈值 2 ⇒ 期望 total=%d would_block=%d）", rep, calls, calls-2)
	}
}

// TestToolCircuitEnforceBlocksAfterThreshold 反向对照：enforce 真的会拦。
// 缺了它，上一条测试可能被"shadow 分支根本没进熔断器"蒙混过关。
func TestToolCircuitEnforceBlocksAfterThreshold(t *testing.T) {
	const calls = 10
	restoreCircuitState(t)
	t.Setenv(circuitFlagEnv, "enforce")
	t.Setenv(circuitThresholdEnv, "2")

	config := tooluse.ToolExecutorConfig{}
	if mode := applyToolCircuitBreaker(&config); mode != toolCircuitEnforce {
		t.Fatalf("模式 = %s，期望 enforce", mode)
	}
	exec, tool, name := newExecutorWithCircuit(t, config)
	blocked := 0
	for i := 0; i < calls; i++ {
		res := exec.Execute(context.Background(), tooluse.ExecuteRequest{ToolName: name})
		if errors.Is(res.Err, tooluse.ErrCircuitOpen) {
			blocked++
		}
	}
	if blocked == 0 {
		t.Fatal("enforce 下一次都没拦 ⇒ 熔断未生效，shadow 对照失效")
	}
	if tool.called() >= calls {
		t.Errorf("enforce 下应有调用被拦下，实际全部到达下游（%d 次）", tool.called())
	}
	if _, _, _, decisions := GetToolCircuitSnapshot(); decisions.Report().WouldBlock == 0 {
		t.Error("生效态也必须产出判定报告（转阻断后仍要能看见拦了多少）")
	}
}

// TestToolCircuitDecisionLandsInAuditLog AC①：连续失败后**日志里**出现熔断判定记录。
//
// 抓日志要做两件事才对：换 os.Stdout 之外还必须重建全局日志器——GetLogger 会把实例
// 连同当时的 os.Stdout 一起缓存，只换 stdout 抓不到任何东西（这条最容易写成假绿）。
func TestToolCircuitDecisionLandsInAuditLog(t *testing.T) {
	restoreCircuitState(t)
	t.Setenv(circuitFlagEnv, "shadow")
	t.Setenv(circuitThresholdEnv, "2")

	oldOut := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("管道创建失败：%v", err)
	}
	os.Stdout = w
	logger.InitLogger(logger.LoggingConfig{Level: "info", Format: "json", Output: "stdout"})
	t.Cleanup(func() {
		os.Stdout = oldOut
		logger.InitLogger(logger.DefaultConfig())
	})

	config := tooluse.ToolExecutorConfig{}
	if mode := applyToolCircuitBreaker(&config); mode != toolCircuitShadow {
		t.Fatalf("模式 = %s，期望 shadow", mode)
	}
	exec, _, name := newExecutorWithCircuit(t, config)
	for i := 0; i < 5; i++ {
		exec.Execute(context.Background(), tooluse.ExecuteRequest{ToolName: name})
	}

	if err := w.Close(); err != nil {
		t.Fatalf("关闭写端失败：%v", err)
	}
	os.Stdout = oldOut
	captured, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatalf("读取日志失败：%v", readErr)
	}
	logged := string(captured)
	t.Logf("捕获到的审计日志：\n%s", logged)

	if !strings.Contains(logged, `"event":"tool_circuit_decision"`) {
		t.Fatal("日志里没有熔断判定记录（AC① 未兑现）")
	}
	if !strings.Contains(logged, `"shadow":true`) {
		t.Error("shadow 态的判定记录应标 shadow=true")
	}
	if !strings.Contains(logged, `"would_block":true`) {
		t.Error("阈值 2、调用 5 次 ⇒ 应有 would_block=true 的记录")
	}
	if strings.Contains(logged, `"would_block":false`) {
		t.Error("放行判定不该产生日志（每次工具调用两行且无信息量）")
	}
	// 日志与计数必须同源：同一次跑出来的数字要能对上
	_, _, _, decisions := GetToolCircuitSnapshot()
	if decisions == nil {
		t.Fatal("shadow 下判定累计器不应为 nil")
	}
	rep := decisions.Report()
	var loggedDecisions int
	for _, line := range strings.Split(logged, "\n") {
		if !strings.Contains(line, `"event":"tool_circuit_decision"`) {
			continue
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(line), &parsed); err != nil {
			t.Fatalf("审计日志不是合法 JSON（下游采集会挂）：%v\n%s", err, line)
		}
		if parsed["tool_name"] != name {
			t.Errorf("判定记录缺工具名：%v", parsed["tool_name"])
		}
		loggedDecisions++
	}
	if loggedDecisions != int(rep.WouldBlock) {
		t.Errorf("日志条数 %d 与计数 would_block=%d 不一致", loggedDecisions, rep.WouldBlock)
	}
}

func probeOutcome(res tooluse.ExecuteResult) string {
	errText := "<nil>"
	if res.Err != nil {
		errText = res.Err.Error()
	}
	return errText + "|" + res.ToolResult.Error + "|" + boolStr(res.Success)
}

func boolStr(b bool) string {
	if b {
		return "ok"
	}
	return "fail"
}

// TestObserveToolCircuitDecisionNilCounterSafe 判定回调在计数器缺失时不得 panic
// （observeToolCircuitDecision 会被挂进每一次工具调用的装饰链里）。
func TestObserveToolCircuitDecisionNilCounterSafe(t *testing.T) {
	restoreCircuitState(t)
	circuitDecisions = nil
	func() {
		defer func() {
			if p := recover(); p != nil {
				t.Fatalf("判定回调 panic：%v", p)
			}
		}()
		observeToolCircuitDecision(context.Background(), tooluse.CircuitDecision{ToolName: "x", WouldBlock: true, Shadow: true})
		observeToolCircuitDecision(context.Background(), tooluse.CircuitDecision{ToolName: "x", WouldBlock: false})
	}()
}
