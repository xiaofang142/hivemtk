package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/browser_automation/model"
)

// fakeBrain planRound 测试替身：脚本化 plan，不打真 LLM。
type fakeBrain struct {
	stepsJSON []byte
	done      bool
	err       error
	tokens    int
	calls     int
	lastGoal  string
	lastSnap  string
}

func (f *fakeBrain) GeneratePlanReflect(ctx context.Context, taskID, sessionID uint, goal, platformID, snapshot string, st *reflectState) ([]byte, bool, error) {
	f.calls++
	f.lastGoal = goal
	f.lastSnap = snapshot
	return f.stepsJSON, f.done, f.err
}

func (f *fakeBrain) LastPlanTokens() int   { return f.tokens }
func (f *fakeBrain) LastPlanModel() string { return "" }
func (f *fakeBrain) LastAuxTokens() int    { return 0 }
func (f *fakeBrain) JudgeDone(ctx context.Context, taskID, sessionID uint, goal, evidence string) (bool, string) {
	return true, "fake"
}
func (f *fakeBrain) SummarizeSession(ctx context.Context, taskID, sessionID uint, goal string, success, total int, extracts string) string {
	return ""
}

func testTaskSession() (*model.BrowserTask, *model.BrowserSession) {
	return &model.BrowserTask{ID: 7, UserID: 26, BrainGoal: "点一下登录"},
		&model.BrowserSession{ID: 11}
}

const jevTestSnap = `button "登录" @e1
*button "发送" @e2
textbox "搜索" @e3
link "帮助" @e4
not a snapshot line
button "缺ref"
`

func TestParseSnapshotElements(t *testing.T) {
	elems := parseSnapshotElements(jevTestSnap)
	if len(elems) != 4 {
		t.Fatalf("应解析出 4 个元素（含 * 新标记行），got %d: %+v", len(elems), elems)
	}
	if elems[0].Index != "@e1" || elems[0].Role != "button" || elems[0].Name != "登录" {
		t.Fatalf("首元素解析错: %+v", elems[0])
	}
	if elems[1].Index != "@e2" {
		t.Fatalf("* 标记行应正常解析: %+v", elems[1])
	}
	// 250 截断
	var b strings.Builder
	for i := 0; i < 300; i++ {
		b.WriteString("button \"x\" @e" + itoa(i) + "\n")
	}
	if got := len(parseSnapshotElements(b.String())); got != jevMaxElements {
		t.Fatalf("应截断到 %d，got %d", jevMaxElements, got)
	}
}

func TestValidateChoice(t *testing.T) {
	ids := []string{"a", "b"}
	ok := jevChoiceRecord{Choice: "a", Probabilities: map[string]float64{"a": 0.7, "b": 0.3}, Confidence: 0.7}
	if err := validateChoice(ok, ids); err != nil {
		t.Fatalf("合法答案应通过: %v", err)
	}
	cases := []struct {
		name string
		rec  jevChoiceRecord
	}{
		{"未知choice", jevChoiceRecord{Choice: "zzz", Probabilities: map[string]float64{"a": 0.7, "b": 0.3}}},
		{"键数不等", jevChoiceRecord{Choice: "a", Probabilities: map[string]float64{"a": 1.0}}},
		{"未知概率键", jevChoiceRecord{Choice: "a", Probabilities: map[string]float64{"a": 0.7, "zzz": 0.3}}},
		{"概率越界", jevChoiceRecord{Choice: "a", Probabilities: map[string]float64{"a": 1.5, "b": -0.5}}},
		{"和偏离1", jevChoiceRecord{Choice: "a", Probabilities: map[string]float64{"a": 0.5, "b": 0.3}}},
		{"非argmax", jevChoiceRecord{Choice: "b", Probabilities: map[string]float64{"a": 0.7, "b": 0.3}}},
	}
	for _, c := range cases {
		if err := validateChoice(c.rec, ids); err == nil {
			t.Fatalf("%s 应被拒绝", c.name)
		} else if !strings.Contains(err.Error(), "invalid TypeSafe response") {
			t.Fatalf("%s 错误文案应带 invalid TypeSafe response，got %v", c.name, err)
		}
	}
}

func TestDecisionToSteps(t *testing.T) {
	click, done, terminal, err := decisionToSteps(JevDecision{Operation: jevOpClick, Target: "@e2"}, "")
	if err != nil || done || terminal != "" {
		t.Fatalf("CLICK 应产单步，got err=%v done=%v terminal=%q", err, done, terminal)
	}
	var items []struct {
		Action string `json:"action"`
		Target string `json:"target"`
	}
	if err := json.Unmarshal(click, &items); err != nil || len(items) != 1 || items[0].Action != "click" || items[0].Target != "@e2" {
		t.Fatalf("CLICK 映射错: %s err=%v", string(click), err)
	}
	if _, d, _, err := decisionToSteps(JevDecision{Operation: jevOpDone}, ""); err != nil || !d {
		t.Fatalf("DONE 应 done=true，got %v %v", d, err)
	}
	if _, _, term, err := decisionToSteps(JevDecision{Operation: jevOpBlocked}, ""); err != nil || term == "" {
		t.Fatalf("BLOCKED 应给 terminal，got %q %v", term, err)
	}
	for _, op := range []string{jevOpScrollDown, jevOpScrollUp, jevOpWait} {
		if _, d, term, err := decisionToSteps(JevDecision{Operation: op}, ""); err != nil || d || term != "" {
			t.Fatalf("%s 应产单步，got %v %v %q", op, d, term, err)
		}
	}
	// TYPE_TEXT：有文本→产 type 步（Value=任务文案）；空文本→拒绝（JEV 绝不编造文本）。
	typeBlob, d, term, err := decisionToSteps(JevDecision{Operation: jevOpTypeText, Target: "@e3"}, "好评，已关注")
	if err != nil || d || term != "" {
		t.Fatalf("TYPE_TEXT 应产单步，got err=%v done=%v terminal=%q", err, d, term)
	}
	var typeItems []struct {
		Action string `json:"action"`
		Target string `json:"target"`
		Value  string `json:"value"`
	}
	if err := json.Unmarshal(typeBlob, &typeItems); err != nil || len(typeItems) != 1 ||
		typeItems[0].Action != "type" || typeItems[0].Target != "@e3" || typeItems[0].Value != "好评，已关注" {
		t.Fatalf("TYPE_TEXT 映射错: %s err=%v", string(typeBlob), err)
	}
	if _, _, _, err := decisionToSteps(JevDecision{Operation: jevOpTypeText, Target: "@e3"}, ""); err == nil {
		t.Fatal("TYPE_TEXT 空文本应拒绝")
	}
	if _, _, _, err := decisionToSteps(JevDecision{Operation: "TYPE_TEXTT", Target: "@e3"}, "x"); err == nil {
		t.Fatal("未知 operation 应拒绝")
	}
	if _, _, _, err := decisionToSteps(JevDecision{Operation: jevOpClick}, ""); err == nil {
		t.Fatal("CLICK 无目标应拒绝")
	}
	if _, _, _, err := decisionToSteps(JevDecision{Operation: jevOpTypeText}, "x"); err == nil {
		t.Fatal("TYPE_TEXT 无目标应拒绝")
	}
}

func jevTypeTextOp() jevChoiceRecord {
	return jevChoiceRecord{Choice: jevOpTypeText, Probabilities: map[string]float64{jevOpTypeText: 0.8, "CLICK": 0.05, "SCROLL_DOWN": 0.05, "SCROLL_UP": 0.0, "WAIT": 0.03, "DONE": 0.04, "BLOCKED": 0.03}, Confidence: 0.8}
}

func jevTypeTarget() jevChoiceRecord {
	return jevChoiceRecord{Choice: "@e3", Probabilities: map[string]float64{"@e3": 1.0}, Confidence: 0.9}
}

func TestChooseTypeText(t *testing.T) {
	fd := &fakeJevDispatcher{
		content: jevAnswersJSONWith(t, jevTypeTextOp(), "type_target", jevTypeTarget()),
		usage:   llm.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}
	fb := &fakeBrain{stepsJSON: []byte(`[]`)}
	e := &Executor{brain: fb}
	e.SetJevClient(NewJevClientWithConfig(JevConfig{Enabled: true}, fd))
	task, session := testTaskSession()
	task.CopyText = "好评，已关注"
	seq := 0
	out, err := e.planRound(context.Background(), task, session, jevTestSnap, "https://x.test/", &reflectState{}, nil, &seq, nil)
	if err != nil || out.done || out.terminal != "" {
		t.Fatalf("JEV TYPE_TEXT 应产单步，got err=%v done=%v terminal=%q", err, out.done, out.terminal)
	}
	var items []struct {
		Action string `json:"action"`
		Target string `json:"target"`
		Value  string `json:"value"`
	}
	if err := json.Unmarshal(out.stepsJSON, &items); err != nil || len(items) != 1 ||
		items[0].Action != "type" || items[0].Target != "@e3" || items[0].Value != "好评，已关注" {
		t.Fatalf("TYPE_TEXT 应映射为 type @e3（Value=任务文案），got %s err=%v", string(out.stepsJSON), err)
	}
	if fb.calls != 0 {
		t.Fatal("JEV 成功时不应调 Brain")
	}
	if fd.calls != 1 {
		t.Fatalf("应恰调一次调度，got %d", fd.calls)
	}
}

func TestPlanRoundTypeTextEmptyFallsBack(t *testing.T) {
	// 任务无文案却选中 TYPE_TEXT → 拒绝执行，回退 Brain（JEV 绝不编造文本）。
	fd := &fakeJevDispatcher{content: jevAnswersJSONWith(t, jevTypeTextOp(), "type_target", jevTypeTarget())}
	fb := &fakeBrain{stepsJSON: []byte(`[{"action":"wait"}]`), tokens: 9}
	e := &Executor{brain: fb}
	e.SetJevClient(NewJevClientWithConfig(JevConfig{Enabled: true}, fd))
	task, session := testTaskSession() // CopyText 为空
	seq := 0
	out, err := e.planRound(context.Background(), task, session, jevTestSnap, "https://x.test/", &reflectState{}, nil, &seq, nil)
	if err != nil {
		t.Fatalf("空文案 TYPE_TEXT 应回退 Brain 而非报错，got %v", err)
	}
	if fb.calls != 1 || string(out.stepsJSON) != string(fb.stepsJSON) {
		t.Fatalf("应回退 Brain，got calls=%d steps=%s", fb.calls, string(out.stepsJSON))
	}
	if !out.jevAttempted || out.jevOK {
		t.Fatalf("应记 attempted 且 !ok，got attempted=%v ok=%v", out.jevAttempted, out.jevOK)
	}
}

// fakeJevDispatcher jevDispatcher 替身：脚本化 choice JSON（经统一调度口径，不打真 LLM）。
type fakeJevDispatcher struct {
	content string // DispatchStructured  unmarshalled 进 schema 的 JSON
	usage   llm.TokenUsage
	err     error
	calls   int
	lastReq llm.DispatchRequest
}

func (f *fakeJevDispatcher) DispatchStructured(ctx context.Context, req llm.DispatchRequest, schema any) (*llm.DispatchResult, error) {
	f.calls++
	f.lastReq = req
	if f.err != nil {
		return nil, f.err
	}
	if err := json.Unmarshal([]byte(f.content), schema); err != nil {
		return nil, err
	}
	return &llm.DispatchResult{Provider: "fake", Model: "fake-m", Usage: f.usage}, nil
}

// jevAnswersJSON 组装同体 choice 响应 JSON。
func jevAnswersJSON(t *testing.T, op jevChoiceRecord, target jevChoiceRecord) string {
	t.Helper()
	return jevAnswersJSONWith(t, op, "click_target", target)
}

// jevAnswersJSONWith 组装同体 choice 响应 JSON（target head 键名可指定：click_target/type_target）。
func jevAnswersJSONWith(t *testing.T, op jevChoiceRecord, targetKey string, target jevChoiceRecord) string {
	t.Helper()
	m := map[string]jevChoiceRecord{"operation": op}
	if target.Choice != "" || len(target.Probabilities) > 0 {
		m[targetKey] = target
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func jevClickOp() jevChoiceRecord {
	return jevChoiceRecord{Choice: "CLICK", Probabilities: map[string]float64{"CLICK": 0.8, "TYPE_TEXT": 0.0, "SCROLL_DOWN": 0.05, "SCROLL_UP": 0.05, "WAIT": 0.03, "DONE": 0.04, "BLOCKED": 0.03}, Confidence: 0.8}
}

func jevClickTarget() jevChoiceRecord {
	return jevChoiceRecord{Choice: "@e1", Probabilities: map[string]float64{"@e1": 0.6, "@e2": 0.3, "@e4": 0.1}, Confidence: 0.6}
}

func TestPlanRoundBrainFallbackWhenDisabled(t *testing.T) {
	fb := &fakeBrain{stepsJSON: []byte(`[{"action":"click","target":"@e1"}]`), tokens: 123}
	e := &Executor{brain: fb, jevClient: NewJevClientWithConfig(JevConfig{Enabled: false}, &fakeJevDispatcher{})}
	task, session := testTaskSession()
	seq := 0
	st := &reflectState{}
	out, err := e.planRound(context.Background(), task, session, jevTestSnap, "https://x.test/", st, []string{"click @e9"}, &seq, nil)
	steps, done, terminal, tokens := out.stepsJSON, out.done, out.terminal, out.brainTokens+out.jevTokens
	if err != nil || done || terminal != "" {
		t.Fatalf("未启用 JEV 应原样走 Brain，got err=%v done=%v terminal=%q", err, done, terminal)
	}
	if tokens != 123 || fb.calls != 1 {
		t.Fatalf("Brain token 应透传（123）且调用 1 次，got tokens=%d calls=%d", tokens, fb.calls)
	}
	if string(steps) != string(fb.stepsJSON) {
		t.Fatalf("应返回 Brain 原文，got %s", string(steps))
	}
}

func TestPlanRoundJevClick(t *testing.T) {
	fd := &fakeJevDispatcher{
		content: jevAnswersJSON(t, jevClickOp(), jevClickTarget()),
		usage:   llm.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}
	fb := &fakeBrain{stepsJSON: []byte(`[]`)}
	e := &Executor{brain: fb}
	e.SetJevClient(NewJevClientWithConfig(JevConfig{Enabled: true}, fd))
	task, session := testTaskSession()
	seq := 0
	out, err := e.planRound(context.Background(), task, session, jevTestSnap, "https://x.test/", &reflectState{}, nil, &seq, nil)
	steps, done, terminal, tokens := out.stepsJSON, out.done, out.terminal, out.brainTokens+out.jevTokens
	if err != nil || done || terminal != "" {
		t.Fatalf("JEV CLICK 应产单步，got err=%v done=%v terminal=%q", err, done, terminal)
	}
	if tokens != 15 {
		t.Fatalf("JEV 轮 token 应为实计（15），got %d", tokens)
	}
	if fb.calls != 0 {
		t.Fatal("JEV 成功时不应调 Brain")
	}
	if fd.calls != 1 {
		t.Fatalf("应恰调一次调度，got %d", fd.calls)
	}
	if fd.lastReq.Scenario != llm.ScenarioJevChoice {
		t.Fatalf("应走 jev_choice scenario，got %q", fd.lastReq.Scenario)
	}
	if !strings.Contains(string(steps), `"action":"click"`) || !strings.Contains(string(steps), "@e1") {
		t.Fatalf("CLICK 应映射为 click @e1，got %s", string(steps))
	}
}

func TestPlanRoundJevInvalidFallsBack(t *testing.T) {
	// click_target 概率和偏离 1 → 非法 → 回退 Brain。
	fd := &fakeJevDispatcher{content: jevAnswersJSON(t, jevClickOp(),
		jevChoiceRecord{Choice: "@e1", Probabilities: map[string]float64{"@e1": 0.5, "@e2": 0.3, "@e4": 0.1}})}
	fb := &fakeBrain{stepsJSON: []byte(`[{"action":"wait"}]`), tokens: 9}
	e := &Executor{brain: fb}
	e.SetJevClient(NewJevClientWithConfig(JevConfig{Enabled: true}, fd))
	task, session := testTaskSession()
	seq := 0
	out, err := e.planRound(context.Background(), task, session, jevTestSnap, "https://x.test/", &reflectState{}, nil, &seq, nil)
	steps, tokens := out.stepsJSON, out.brainTokens+out.jevTokens
	if err != nil {
		t.Fatalf("非法 JEV 响应应回退 Brain 而非报错，got %v", err)
	}
	if fb.calls != 1 || tokens != 9 || string(steps) != string(fb.stepsJSON) {
		t.Fatalf("应回退 Brain，got calls=%d tokens=%d steps=%s", fb.calls, tokens, string(steps))
	}
}

func TestPlanRoundJevBlockedAndDone(t *testing.T) {
	mkOp := func(choice string) *fakeJevDispatcher {
		probs := map[string]float64{}
		for _, id := range jevOpIDs() {
			probs[id] = 0.0
		}
		probs[choice] = 1.0
		return &fakeJevDispatcher{content: jevAnswersJSON(t, jevChoiceRecord{Choice: choice, Probabilities: probs}, jevChoiceRecord{})}
	}
	// BLOCKED
	fd := mkOp("BLOCKED")
	fb := &fakeBrain{}
	e := &Executor{brain: fb}
	e.SetJevClient(NewJevClientWithConfig(JevConfig{Enabled: true}, fd))
	task, session := testTaskSession()
	seq := 0
	if out, err := e.planRound(context.Background(), task, session, jevTestSnap, "https://x.test/", &reflectState{}, nil, &seq, nil); err != nil || out.terminal == "" {
		t.Fatalf("BLOCKED 应终止本轮，got terminal=%q err=%v", out.terminal, err)
	}
	if fb.calls != 0 {
		t.Fatal("BLOCKED 不应再调 Brain")
	}
	// DONE
	fd2 := mkOp("DONE")
	e.SetJevClient(NewJevClientWithConfig(JevConfig{Enabled: true}, fd2))
	seq = 0
	if out, err := e.planRound(context.Background(), task, session, jevTestSnap, "https://x.test/", &reflectState{}, nil, &seq, nil); err != nil || !out.done || out.terminal != "" {
		t.Fatalf("DONE 应 done=true，got done=%v terminal=%q err=%v", out.done, out.terminal, err)
	}
}

func TestLoadJevConfigDisabledByDefault(t *testing.T) {
	t.Setenv("BROWSER_JEV_ENABLED", "")
	t.Setenv("BROWSER_JEV_ENDPOINT", "")
	if c := NewJevClientFromEnv(); c.Ready() {
		t.Fatal("默认（无 env）JEV 必须关闭")
	}
	// v1.1：启用不再要求 key（统一调度下本地网关免 key）；无可用 provider 时
	// 调度报错 → planRound 回退 Brain（fail-closed 在调度层，不在配置层）。
	t.Setenv("BROWSER_JEV_ENABLED", "1")
	if c := NewJevClientFromEnv(); !c.Ready() {
		t.Fatal("BROWSER_JEV_ENABLED=1 应 Ready（key 不再是启用条件）")
	}
}

func TestPlanRoundJevDispatchErrorFallsBack(t *testing.T) {
	// 调度层无可用 provider（报错）→ 回退 Brain，不抛错。
	fd := &fakeJevDispatcher{err: context.DeadlineExceeded}
	fb := &fakeBrain{stepsJSON: []byte(`[{"action":"wait"}]`), tokens: 9}
	e := &Executor{brain: fb}
	e.SetJevClient(NewJevClientWithConfig(JevConfig{Enabled: true}, fd))
	task, session := testTaskSession()
	seq := 0
	out, err := e.planRound(context.Background(), task, session, jevTestSnap, "https://x.test/", &reflectState{}, nil, &seq, nil)
	steps, tokens := out.stepsJSON, out.brainTokens+out.jevTokens
	if err != nil {
		t.Fatalf("调度失败应回退 Brain 而非报错，got %v", err)
	}
	if !out.jevAttempted || out.jevOK {
		t.Fatalf("调度失败应记 attempted 且 !ok，got attempted=%v ok=%v", out.jevAttempted, out.jevOK)
	}
	if fb.calls != 1 || tokens != 9 || string(steps) != string(fb.stepsJSON) {
		t.Fatalf("应回退 Brain（调度 1 次重试 + Brain），got fb.calls=%d tokens=%d steps=%s fd.calls=%d", fb.calls, tokens, string(steps), fd.calls)
	}
	if fd.calls != 2 {
		t.Fatalf("可重试错误应调调度 2 次（含 1 次重试），got %d", fd.calls)
	}
}

func TestIsJevStale(t *testing.T) {
	if isJevStale("a", "a") {
		t.Fatal("相同指纹不应判 stale")
	}
	if !isJevStale("a", "b") {
		t.Fatal("不同指纹应判 stale")
	}
	if isJevStale("", "b") || isJevStale("a", "") {
		t.Fatal("空指纹应 fail-soft 放行（不断 stale）")
	}
}

func TestPlanRoundJevOffSkipsJev(t *testing.T) {
	// session 熔断后（js.off）→ 直走 Brain，不碰调度。
	fd := &fakeJevDispatcher{
		content: jevAnswersJSON(t, jevClickOp(), jevClickTarget()),
		usage:   llm.TokenUsage{TotalTokens: 15},
	}
	fb := &fakeBrain{stepsJSON: []byte(`[{"action":"wait"}]`), tokens: 7}
	e := &Executor{brain: fb}
	e.SetJevClient(NewJevClientWithConfig(JevConfig{Enabled: true}, fd))
	task, session := testTaskSession()
	seq := 0
	js := &jevSessionState{off: true}
	out, err := e.planRound(context.Background(), task, session, jevTestSnap, "https://x.test/", &reflectState{}, nil, &seq, js)
	if err != nil {
		t.Fatalf("熔断后应直走 Brain，got %v", err)
	}
	if out.jevAttempted || fd.calls != 0 || fb.calls != 1 {
		t.Fatalf("熔断后不应尝试 JEV，got attempted=%v fd.calls=%d fb.calls=%d", out.jevAttempted, fd.calls, fb.calls)
	}
	if out.brainTokens != 7 {
		t.Fatalf("Brain token 应透传（7），got %d", out.brainTokens)
	}
}
