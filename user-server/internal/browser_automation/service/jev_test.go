package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

func (f *fakeBrain) LastPlanTokens() int { return f.tokens }
func (f *fakeBrain) LastAuxTokens() int  { return 0 }
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
		} else if !strings.Contains(err.Error(), "Invalid TypeSafe response") {
			t.Fatalf("%s 错误文案应带 Invalid TypeSafe response，got %v", c.name, err)
		}
	}
}

func TestDecisionToSteps(t *testing.T) {
	click, done, terminal, err := decisionToSteps(JevDecision{Operation: jevOpClick, Target: "@e2"})
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
	if _, d, _, err := decisionToSteps(JevDecision{Operation: jevOpDone}); err != nil || !d {
		t.Fatalf("DONE 应 done=true，got %v %v", d, err)
	}
	if _, _, term, err := decisionToSteps(JevDecision{Operation: jevOpBlocked}); err != nil || term == "" {
		t.Fatalf("BLOCKED 应给 terminal，got %q %v", term, err)
	}
	for _, op := range []string{jevOpScrollDown, jevOpScrollUp, jevOpWait} {
		if _, d, term, err := decisionToSteps(JevDecision{Operation: op}); err != nil || d || term != "" {
			t.Fatalf("%s 应产单步，got %v %v %q", op, d, term, err)
		}
	}
	if _, _, _, err := decisionToSteps(JevDecision{Operation: "TYPE_TEXT", Target: "@e3"}); err == nil {
		t.Fatal("未知 operation 应拒绝（v1 无 TYPE_TEXT）")
	}
	if _, _, _, err := decisionToSteps(JevDecision{Operation: jevOpClick}); err == nil {
		t.Fatal("CLICK 无目标应拒绝")
	}
}

// jevFakeServer 具名 canned 答案的 fake System-One。
func jevFakeServer(t *testing.T, answers map[string]jevChoiceRecord, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("JEV 应为 POST，got %s", r.Method)
		}
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"answers": answers})
	}))
}

func TestPlanRoundBrainFallbackWhenDisabled(t *testing.T) {
	fb := &fakeBrain{stepsJSON: []byte(`[{"action":"click","target":"@e1"}]`), tokens: 123}
	e := &Executor{brain: fb, jevClient: NewJevClientWithConfig(JevConfig{Enabled: false}, nil)}
	task, session := testTaskSession()
	seq := 0
	st := &reflectState{}
	steps, done, terminal, tokens, err := e.planRound(context.Background(), task, session, jevTestSnap, "https://x.test/", st, []string{"click @e9"}, &seq)
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
	srv := jevFakeServer(t, map[string]jevChoiceRecord{
		"operation":    {Choice: "CLICK", Probabilities: map[string]float64{"CLICK": 0.8, "SCROLL_DOWN": 0.05, "SCROLL_UP": 0.05, "WAIT": 0.03, "DONE": 0.04, "BLOCKED": 0.03}, Confidence: 0.8},
		"click_target": {Choice: "@e1", Probabilities: map[string]float64{"@e1": 0.6, "@e2": 0.3, "@e4": 0.1}, Confidence: 0.6},
	}, 0)
	defer srv.Close()
	fb := &fakeBrain{stepsJSON: []byte(`[]`)}
	e := &Executor{brain: fb}
	e.SetJevClient(NewJevClientWithConfig(JevConfig{Enabled: true, Endpoint: srv.URL, APIKey: "k", Model: "m"}, nil))
	task, session := testTaskSession()
	seq := 0
	steps, done, terminal, tokens, err := e.planRound(context.Background(), task, session, jevTestSnap, "https://x.test/", &reflectState{}, nil, &seq)
	if err != nil || done || terminal != "" {
		t.Fatalf("JEV CLICK 应产单步，got err=%v done=%v terminal=%q", err, done, terminal)
	}
	if tokens != 0 {
		t.Fatalf("JEV 轮 token 应为 0，got %d", tokens)
	}
	if fb.calls != 0 {
		t.Fatal("JEV 成功时不应调 Brain")
	}
	if !strings.Contains(string(steps), `"action":"click"`) || !strings.Contains(string(steps), "@e1") {
		t.Fatalf("CLICK 应映射为 click @e1，got %s", string(steps))
	}
}

func TestPlanRoundJevInvalidFallsBack(t *testing.T) {
	// click_target 概率和偏离 1 → 非法 → 回退 Brain。
	srv := jevFakeServer(t, map[string]jevChoiceRecord{
		"operation":    {Choice: "CLICK", Probabilities: map[string]float64{"CLICK": 0.8, "SCROLL_DOWN": 0.05, "SCROLL_UP": 0.05, "WAIT": 0.03, "DONE": 0.04, "BLOCKED": 0.03}},
		"click_target": {Choice: "@e1", Probabilities: map[string]float64{"@e1": 0.5, "@e2": 0.3, "@e4": 0.1}},
	}, 0)
	defer srv.Close()
	fb := &fakeBrain{stepsJSON: []byte(`[{"action":"wait"}]`), tokens: 9}
	e := &Executor{brain: fb}
	e.SetJevClient(NewJevClientWithConfig(JevConfig{Enabled: true, Endpoint: srv.URL, APIKey: "k", Model: "m"}, nil))
	task, session := testTaskSession()
	seq := 0
	steps, _, _, tokens, err := e.planRound(context.Background(), task, session, jevTestSnap, "https://x.test/", &reflectState{}, nil, &seq)
	if err != nil {
		t.Fatalf("非法 JEV 响应应回退 Brain 而非报错，got %v", err)
	}
	if fb.calls != 1 || tokens != 9 || string(steps) != string(fb.stepsJSON) {
		t.Fatalf("应回退 Brain，got calls=%d tokens=%d steps=%s", fb.calls, tokens, string(steps))
	}
}

func TestPlanRoundJevBlockedAndDone(t *testing.T) {
	mkOp := func(choice string) *httptest.Server {
		probs := map[string]float64{}
		for _, id := range jevOpIDs() {
			probs[id] = 0.0
		}
		probs[choice] = 1.0
		return jevFakeServer(t, map[string]jevChoiceRecord{"operation": {Choice: choice, Probabilities: probs}}, 0)
	}
	// BLOCKED
	srv := mkOp("BLOCKED")
	defer srv.Close()
	fb := &fakeBrain{}
	e := &Executor{brain: fb}
	e.SetJevClient(NewJevClientWithConfig(JevConfig{Enabled: true, Endpoint: srv.URL, APIKey: "k", Model: "m"}, nil))
	task, session := testTaskSession()
	seq := 0
	if _, _, terminal, _, err := e.planRound(context.Background(), task, session, jevTestSnap, "https://x.test/", &reflectState{}, nil, &seq); err != nil || terminal == "" {
		t.Fatalf("BLOCKED 应终止本轮，got terminal=%q err=%v", terminal, err)
	}
	if fb.calls != 0 {
		t.Fatal("BLOCKED 不应再调 Brain")
	}
	// DONE
	srv2 := mkOp("DONE")
	defer srv2.Close()
	e.SetJevClient(NewJevClientWithConfig(JevConfig{Enabled: true, Endpoint: srv2.URL, APIKey: "k", Model: "m"}, nil))
	seq = 0
	if _, done, terminal, _, err := e.planRound(context.Background(), task, session, jevTestSnap, "https://x.test/", &reflectState{}, nil, &seq); err != nil || !done || terminal != "" {
		t.Fatalf("DONE 应 done=true，got done=%v terminal=%q err=%v", done, terminal, err)
	}
}

func TestLoadJevConfigDisabledByDefault(t *testing.T) {
	t.Setenv("BROWSER_JEV_ENABLED", "")
	t.Setenv("BROWSER_JEV_API_KEY", "")
	if c := NewJevClientFromEnv(); c.Ready() {
		t.Fatal("默认（无 env）JEV 必须关闭")
	}
	t.Setenv("BROWSER_JEV_ENABLED", "1")
	if c := NewJevClientFromEnv(); c.Ready() {
		t.Fatal("有开关无 key 仍必须关闭（fail-closed）")
	}
}
