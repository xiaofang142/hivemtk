package browsercore

import (
	"context"
	"strings"
	"testing"
)

const e16Snapshot = `[p1 "Example Domain" | space "hivemtk-ego-generic-research-1028"(13): 1 managed, 0 untracked — p1* "Example Domain"]
root
  svg_root
  paragraph
    text "This domain is for use in documentation examples without needing permission."
  anchor [ref=1, loc=href:https://iana.org/help/example-domains, url=https://iana.org/help/example-domains]
    text "Learn more"
  button [ref=7, loc=role:button[name='Sign in']]
    text "Sign in"
`

func TestParseSnapshotE16Shape(t *testing.T) {
	targets, text := ParseSnapshot(e16Snapshot)
	if len(targets) != 2 {
		t.Fatalf("targets=%d want=2: %+v", len(targets), targets)
	}
	if targets[0].Ref != "e1" || targets[0].Role != "anchor" {
		t.Fatalf("first target = %+v", targets[0])
	}
	if targets[0].Loc != "href:https://iana.org/help/example-domains" {
		t.Fatalf("loc = %q", targets[0].Loc)
	}
	if targets[0].Name != "Learn more" {
		t.Fatalf("name = %q", targets[0].Name)
	}
	if targets[1].Name != "Sign in" || targets[1].Ref != "e7" {
		t.Fatalf("second target = %+v", targets[1])
	}
	if !strings.Contains(text, "This domain is for use") {
		t.Fatalf("text = %q", text)
	}
}

func TestRedact(t *testing.T) {
	in := "联系 13800138000 或 a.b@example.com，token=abc123"
	out := Redact(in, nil)
	for _, bad := range []string{"13800138000", "a.b@example.com", "abc123"} {
		if strings.Contains(out, bad) {
			t.Fatalf("未脱敏 %q: %q", bad, out)
		}
	}
}

func TestObservationSanitize(t *testing.T) {
	o := Observation{Text: strings.Repeat("字", 50), Targets: make([]Target, 10)}
	o.Sanitize(3, 10, nil)
	if len(o.Targets) != 3 || !o.TextTruncated {
		t.Fatalf("sanitize 失败 targets=%d truncated=%v", len(o.Targets), o.TextTruncated)
	}
	if len([]rune(o.Text)) != 10 {
		t.Fatalf("text len=%d", len([]rune(o.Text)))
	}
}

func TestValidateOps(t *testing.T) {
	ok := []Op{
		{Op: OpGoto, URL: "https://example.com"},
		{Op: OpClick, Target: "loc=role:button[name='Sign in']"},
		{Op: OpFill, Target: "loc=css:#email", Value: "a@b.c"},
		{Op: OpExtract, Target: "css=article", Fields: []string{"text"}, Limit: 5},
	}
	if err := ValidateOps(ok, 20); err != nil {
		t.Fatalf("合法动作被拒: %v", err)
	}
	bad := [][]Op{
		{{Op: "evaluate"}},
		{{Op: OpClick, Target: "@12"}},
		{{Op: OpClick, Target: "ref=12"}},
		{{Op: OpGoto, URL: "file:///etc/passwd"}},
		{{Op: OpFill, Target: "css=#x"}},
		{{Op: "unknown"}},
	}
	for i, ops := range bad {
		if err := ValidateOps(ops, 20); err == nil {
			t.Fatalf("非法动作集 #%d 未被拒", i)
		}
	}
	if err := ValidateOps(ok, 2); err == nil {
		t.Fatal("动作数超限未被拒")
	}
}

func TestPolicy(t *testing.T) {
	p := DefaultPolicy()
	p.Domains = nil
	if err := p.Validate(); err == nil {
		t.Fatal("空 domains 必须拒绝（fail-close）")
	}
	p.Domains = []string{"example.com"}
	if err := p.Validate(); err != nil {
		t.Fatalf("合法策略被拒: %v", err)
	}
	cases := []struct {
		url  string
		want bool
	}{
		{"https://example.com/a", true},
		{"https://www.example.com/a", true},
		{"https://sub.example.com/a", true},
		{"https://example.com.evil.com/a", false},
		{"https://evil.com/?x=example.com", false},
	}
	for _, c := range cases {
		if got := DomainAllowed(p.Domains, c.url); got != c.want {
			t.Fatalf("DomainAllowed(%s)=%v want=%v", c.url, got, c.want)
		}
	}
}

type fakeExec struct{ engine string }

func (f fakeExec) Engine() string { return f.engine }
func (fakeExec) Open(ctx context.Context, req OpenRequest) (*SessionInfo, error) {
	return nil, nil
}
func (fakeExec) Act(ctx context.Context, req RoundRequest) (*ActResult, error)       { return nil, nil }
func (fakeExec) Observe(ctx context.Context, req ObserveRequest) (*RoundInfo, error) { return nil, nil }
func (fakeExec) Confirm(ctx context.Context, userID, sessionID uint, allow bool) (*SessionInfo, error) {
	return nil, nil
}
func (fakeExec) Abort(ctx context.Context, userID, sessionID uint, reason string) (*SessionInfo, error) {
	return nil, nil
}
func (fakeExec) Finish(ctx context.Context, userID, sessionID uint, keep []string) (*SessionInfo, error) {
	return nil, nil
}
func (fakeExec) Get(ctx context.Context, userID, sessionID uint) (*SessionInfo, error) {
	return nil, nil
}
func (fakeExec) List(ctx context.Context, userID uint, page, pageSize int) ([]*SessionInfo, int64, error) {
	return nil, 0, nil
}
func (fakeExec) Rounds(ctx context.Context, userID, sessionID uint, limit int) ([]*RoundInfo, error) {
	return nil, nil
}

func TestRouter(t *testing.T) {
	r := NewRouter()
	if _, err := r.Pick(EngineAuto); err == nil {
		t.Fatal("空路由器必须报不可用")
	}
	_ = r.Register(fakeExec{engine: EngineNM})
	e, err := r.Pick(EngineAuto)
	if err != nil || e.Engine() != EngineNM {
		t.Fatalf("auto 兜底失败: %v", err)
	}
	_ = r.Register(fakeExec{engine: EngineEgo})
	e, _ = r.Pick(EngineAuto)
	if e.Engine() != EngineEgo {
		t.Fatalf("auto 应优先 ego, got %s", e.Engine())
	}
	if err := r.Register(fakeExec{engine: EngineEgo}); err == nil {
		t.Fatal("重复注册必须报错")
	}
}
