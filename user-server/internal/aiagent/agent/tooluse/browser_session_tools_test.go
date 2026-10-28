package tooluse

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/browsercore"
)

type fakeSessionPort struct {
	opened   *browsercore.OpenRequest
	acted    *browsercore.RoundRequest
	observed *browsercore.ObserveRequest
	finished struct {
		userID, sessionID uint
		keep              []string
	}
	err error
}

func (f *fakeSessionPort) Open(ctx context.Context, req browsercore.OpenRequest) (*browsercore.SessionInfo, error) {
	f.opened = &req
	if f.err != nil {
		return nil, f.err
	}
	return &browsercore.SessionInfo{ID: 7, State: browsercore.SessionCreated, Engine: req.Engine, SpaceName: req.SpaceName}, nil
}

func (f *fakeSessionPort) Act(ctx context.Context, req browsercore.RoundRequest) (*browsercore.ActResult, error) {
	f.acted = &req
	if f.err != nil {
		return nil, f.err
	}
	return &browsercore.ActResult{
		Session: browsercore.SessionInfo{ID: req.SessionID, State: browsercore.SessionActive},
		Round:   browsercore.RoundInfo{Round: 1, ExecutionID: 9},
	}, nil
}

func (f *fakeSessionPort) Observe(ctx context.Context, req browsercore.ObserveRequest) (*browsercore.RoundInfo, error) {
	f.observed = &req
	if f.err != nil {
		return nil, f.err
	}
	return &browsercore.RoundInfo{Round: 1, URL: "https://example.com"}, nil
}

func (f *fakeSessionPort) Finish(ctx context.Context, userID, sessionID uint, keep []string) (*browsercore.SessionInfo, error) {
	f.finished.userID, f.finished.sessionID, f.finished.keep = userID, sessionID, keep
	return &browsercore.SessionInfo{ID: sessionID, State: browsercore.SessionFinished}, nil
}

func TestBrowserSessionOpenTool(t *testing.T) {
	port := &fakeSessionPort{}
	SetBrowserSessionPort(port)
	defer SetBrowserSessionPort(nil)
	tool := NewBrowserSessionOpenTool(BrowserToolDeps{})

	res, _ := tool.Execute(context.Background(), map[string]any{"user_id": 3})
	if res.Success {
		t.Fatal("缺 domains 必须即时反馈")
	}
	res, _ = tool.Execute(context.Background(), map[string]any{
		"user_id": 3, "goal": "抓取文章", "domains": []any{"example.com"}, "timeout_sec": 60,
	})
	if !res.Success {
		t.Fatalf("合法入参失败: %+v", res)
	}
	if port.opened == nil || port.opened.UserID != 3 || len(port.opened.Policy.Domains) != 1 {
		t.Fatalf("端口收到 %+v", port.opened)
	}
	if port.opened.Policy.TimeoutSec != 60 {
		t.Fatalf("timeout 未透传: %+v", port.opened.Policy)
	}
}

func TestBrowserActTool(t *testing.T) {
	port := &fakeSessionPort{}
	SetBrowserSessionPort(port)
	defer SetBrowserSessionPort(nil)
	tool := NewBrowserActTool(BrowserToolDeps{})

	_, _ = tool.Execute(context.Background(), map[string]any{"user_id": 1, "session_id": 2})
	res, _ := tool.Execute(context.Background(), map[string]any{
		"user_id": 1, "session_id": 2,
		"ops": []any{map[string]any{"op": "click", "target": "loc=role:button[name='Go']"}},
	})
	if !res.Success {
		t.Fatalf("act 失败: %+v", res)
	}
	if port.acted == nil || len(port.acted.Ops) != 1 || port.acted.Ops[0].Op != "click" {
		t.Fatalf("ops 未正确解析: %+v", port.acted)
	}
	// 非法 ops 形状
	_, _ = tool.Execute(context.Background(), map[string]any{
		"user_id": 1, "session_id": 2, "ops": "not-an-array",
	})
}

func TestBrowserObserveAndFinishTools(t *testing.T) {
	port := &fakeSessionPort{}
	SetBrowserSessionPort(port)
	defer SetBrowserSessionPort(nil)

	obs := NewBrowserObserveTool(BrowserToolDeps{})
	res, _ := obs.Execute(context.Background(), map[string]any{"user_id": 5, "session_id": 6})
	if !res.Success || port.observed == nil || port.observed.SessionID != 6 {
		t.Fatalf("observe 失败: %+v / %+v", res, port.observed)
	}
	if obs.RiskLevel() != RiskReadonly {
		t.Fatalf("observe 必须是只读工具, got %s", obs.RiskLevel())
	}

	fin := NewBrowserSessionFinishTool(BrowserToolDeps{})
	res, _ = fin.Execute(context.Background(), map[string]any{"user_id": 5, "session_id": 6, "keep": []any{"p1"}})
	if !res.Success || len(port.finished.keep) != 1 || port.finished.sessionID != 6 {
		t.Fatalf("finish 失败: %+v / %+v", res, port.finished)
	}

	// 端口错误必须原样回灌
	port.err = errors.New("boom")
	res, _ = obs.Execute(context.Background(), map[string]any{"user_id": 5, "session_id": 6})
	if res.Success {
		t.Fatal("端口错误不应成功")
	}
}
