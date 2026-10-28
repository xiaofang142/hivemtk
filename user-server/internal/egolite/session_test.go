package egolite

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"hivemtk-user/internal/browsercore"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func newSessionTestEnv(t *testing.T) (*EgoSessionService, EgoSessionRepository, EgoRoundRepository, EgoExecutionRepository, *EgoRunnerRegistry) {
	t.Helper()
	db := testutil.NewTestDB(t,
		&model.EgoliteSession{}, &model.EgoliteRound{},
		&model.EgoliteExecution{}, &model.EgoEventLog{},
	)
	if db == nil {
		t.Fatal("测试库不可用")
	}
	sessions := NewEgoSessionRepositoryWithDB(db)
	rounds := NewEgoRoundRepositoryWithDB(db)
	execRepo := NewEgoExecutionRepositoryWithDB(db)
	evRepo := NewEgoEventRepositoryWithDB(db)
	registry := NewEgoRunnerRegistry()
	svc := NewEgoSessionService(sessions, rounds, execRepo, evRepo, registry, nil)
	return svc, sessions, rounds, execRepo, registry
}

func TestSessionOpenPolicyFailClose(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _, _ := newSessionTestEnv(t)

	// 空 domains = 拒绝一切导航（fail-close）
	_, err := svc.Open(ctx, browsercore.OpenRequest{UserID: 1, Policy: browsercore.Policy{}})
	if !errors.Is(err, browsercore.ErrPolicyViolation) {
		t.Fatalf("空 domains 应报策略错误, got %v", err)
	}
	// 合法策略但 runner 离线 → 明确错误，不落半截会话
	pol := browsercore.DefaultPolicy()
	pol.Domains = []string{"example.com"}
	_, err = svc.Open(ctx, browsercore.OpenRequest{UserID: 1, Policy: pol, SpaceName: "s-offline"})
	if !errors.Is(err, ErrEgoRunnerOffline) {
		t.Fatalf("runner 离线应报 ErrEgoRunnerOffline, got %v", err)
	}
}

func TestSessionActPolicyChecks(t *testing.T) {
	ctx := context.Background()
	svc, sessions, _, _, _ := newSessionTestEnv(t)

	pol := browsercore.DefaultPolicy()
	pol.Domains = []string{"example.com"}
	raw, _ := json.Marshal(pol)
	sess := &model.EgoliteSession{UserID: 2, Engine: "ego", Mode: "ops", SpaceName: "s-act",
		PolicyJSON: raw, State: browsercore.SessionCreated}
	if err := sessions.Create(ctx, sess); err != nil {
		t.Fatal(err)
	}

	// 越域 goto：策略层直接拒（在 EnsureOnline 之前，避免"离线掩盖越域"）
	_, err := svc.Act(ctx, browsercore.RoundRequest{UserID: 2, SessionID: sess.ID,
		Ops: []browsercore.Op{{Op: "goto", URL: "https://evil.com"}}})
	if !errors.Is(err, browsercore.ErrPolicyViolation) {
		t.Fatalf("越域应被拒, got %v", err)
	}
	// 非法动作
	_, err = svc.Act(ctx, browsercore.RoundRequest{UserID: 2, SessionID: sess.ID,
		Ops: []browsercore.Op{{Op: "evaluate"}}})
	if !errors.Is(err, browsercore.ErrPolicyViolation) {
		t.Fatalf("非法动作应被拒, got %v", err)
	}
	// 轮次上限
	sess.Round = 30
	_ = sessions.Update(ctx, sess.ID, map[string]any{"round": 30})
	_, err = svc.Act(ctx, browsercore.RoundRequest{UserID: 2, SessionID: sess.ID,
		Ops: []browsercore.Op{{Op: "goto", URL: "https://example.com"}}})
	if !errors.Is(err, browsercore.ErrPolicyViolation) {
		t.Fatalf("轮次上限应被拒, got %v", err)
	}
}

func TestSessionActOfflineKeepsAudit(t *testing.T) {
	ctx := context.Background()
	svc, sessions, rounds, execRepo, _ := newSessionTestEnv(t)

	pol := browsercore.DefaultPolicy()
	pol.Domains = []string{"example.com"}
	raw, _ := json.Marshal(pol)
	sess := &model.EgoliteSession{UserID: 3, Engine: "ego", Mode: "ops", SpaceName: "s-off2",
		PolicyJSON: raw, State: browsercore.SessionCreated}
	if err := sessions.Create(ctx, sess); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Act(ctx, browsercore.RoundRequest{UserID: 3, SessionID: sess.ID,
		Ops: []browsercore.Op{{Op: "goto", URL: "https://example.com"}}})
	if !errors.Is(err, ErrEgoRunnerOffline) {
		t.Fatalf("want offline, got %v", err)
	}
	// 轮次已记账（ok=false + RUNNER_OFFLINE），但执行行仍在 created（不假装下发）
	list, err := rounds.ListBySession(ctx, sess.ID, 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("rounds=%d err=%v", len(list), err)
	}
	if list[0].Ok == nil || *list[0].Ok {
		t.Fatalf("离线轮次应记 ok=false, got %+v", list[0].Ok)
	}
	if list[0].ErrorCode != "RUNNER_OFFLINE" {
		t.Fatalf("error_code=%q", list[0].ErrorCode)
	}
	e, err := execRepo.GetByID(ctx, list[0].ExecutionID)
	if err != nil || e.State != model.EgoExecCreated {
		t.Fatalf("execution state=%v err=%v", e, err)
	}
}

func TestSessionIngestRoundResult(t *testing.T) {
	ctx := context.Background()
	svc, sessions, rounds, _, _ := newSessionTestEnv(t)

	pol := browsercore.DefaultPolicy()
	pol.Domains = []string{"example.com"}
	raw, _ := json.Marshal(pol)
	sess := &model.EgoliteSession{UserID: 4, Engine: "ego", Mode: "ops", SpaceName: "s-ing",
		PolicyJSON: raw, State: browsercore.SessionActive, Round: 1, ExecutionID: 101}
	if err := sessions.Create(ctx, sess); err != nil {
		t.Fatal(err)
	}
	_, _ = rounds.CreateOrIgnore(ctx, &model.EgoliteRound{SessionID: sess.ID, ExecutionID: 101, UserID: 4, RoundNo: 1})

	payload := map[string]any{
		"type": "round_result", "ok": true, "round": 1, "url": "https://example.com/",
		"title": "Example Domain", "space_id": "13",
		"targets": []map[string]any{{"ref": "e1", "role": "anchor", "name": "Learn more", "loc": "loc=href:https://iana.org"}},
		"text":    "hello 13800138000 a@b.com", "usage": map[string]any{"ms": 12, "actions": 2},
	}
	b, _ := json.Marshal(payload)
	if err := svc.IngestEvent(ctx, 4, RunnerEvent{Type: "round_result", ExecutionID: "ego-101", Payload: b}); err != nil {
		t.Fatal(err)
	}
	rec, err := rounds.GetByRound(ctx, sess.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Ok == nil || !*rec.Ok {
		t.Fatalf("ok 未落库: %+v", rec.Ok)
	}
	if rec.DurationMs != 12 || rec.Actions != 2 {
		t.Fatalf("usage 未落库: %+v", rec)
	}
	if json.Valid(rec.ObservationJSON) {
		var obs browsercore.Observation
		_ = json.Unmarshal(rec.ObservationJSON, &obs)
		if len(obs.Targets) != 1 || obs.Targets[0].Ref != "e1" {
			t.Fatalf("observation targets=%+v", obs.Targets)
		}
		if obs.Text == "" || containsAny(obs.Text, "13800138000", "a@b.com") {
			t.Fatalf("观测未脱敏: %q", obs.Text)
		}
	}
	got, _ := sessions.GetForUser(ctx, sess.ID, 4)
	if got.State != browsercore.SessionActive {
		t.Fatalf("state=%s want active", got.State)
	}
	if got.SpaceID != "13" {
		t.Fatalf("space_id=%q", got.SpaceID)
	}
}

func TestSessionIngestHandoffAndFatal(t *testing.T) {
	ctx := context.Background()
	svc, sessions, rounds, _, _ := newSessionTestEnv(t)
	pol := browsercore.DefaultPolicy()
	pol.Domains = []string{"example.com"}
	raw, _ := json.Marshal(pol)

	// handoff
	s1 := &model.EgoliteSession{UserID: 5, Engine: "ego", Mode: "ops", SpaceName: "s-ho",
		PolicyJSON: raw, State: browsercore.SessionActive, Round: 1, ExecutionID: 201}
	_ = sessions.Create(ctx, s1)
	_, _ = rounds.CreateOrIgnore(ctx, &model.EgoliteRound{SessionID: s1.ID, ExecutionID: 201, UserID: 5, RoundNo: 1})
	b, _ := json.Marshal(map[string]any{"type": "round_result", "ok": true, "round": 1, "handoff": true})
	_ = svc.IngestEvent(ctx, 5, RunnerEvent{Type: "round_result", ExecutionID: "ego-201", Payload: b})
	got, _ := sessions.GetForUser(ctx, s1.ID, 5)
	if got.State != browsercore.SessionHandedOff {
		t.Fatalf("handoff state=%s", got.State)
	}

	// fatal
	s2 := &model.EgoliteSession{UserID: 6, Engine: "ego", Mode: "ops", SpaceName: "s-fatal",
		PolicyJSON: raw, State: browsercore.SessionActive, Round: 1, ExecutionID: 202}
	_ = sessions.Create(ctx, s2)
	_, _ = rounds.CreateOrIgnore(ctx, &model.EgoliteRound{SessionID: s2.ID, ExecutionID: 202, UserID: 6, RoundNo: 1})
	b2, _ := json.Marshal(map[string]any{"type": "round_result", "ok": false, "round": 1, "fatal": true,
		"error": map[string]any{"code": "OP_FAILED", "msg": "boom"}})
	_ = svc.IngestEvent(ctx, 6, RunnerEvent{Type: "round_result", ExecutionID: "ego-202", Payload: b2})
	got2, _ := sessions.GetForUser(ctx, s2.ID, 6)
	if got2.State != browsercore.SessionFailed {
		t.Fatalf("fatal state=%s", got2.State)
	}
}

func TestSessionWatchdogAndAbort(t *testing.T) {
	ctx := context.Background()
	svc, sessions, _, execRepo, _ := newSessionTestEnv(t)
	pol := browsercore.DefaultPolicy()
	pol.Domains = []string{"example.com"}
	raw, _ := json.Marshal(pol)

	// execution 已 timeout、session 还挂着 → 看门狗收敛
	e := &model.EgoliteExecution{UserID: 7, Origin: "session", State: model.EgoExecTimeout}
	if err := execRepo.Create(ctx, e); err != nil {
		t.Fatal(err)
	}
	s := &model.EgoliteSession{UserID: 7, Engine: "ego", Mode: "ops", SpaceName: "s-wd",
		PolicyJSON: raw, State: browsercore.SessionActive, Round: 1, ExecutionID: e.ID}
	_ = sessions.Create(ctx, s)
	n, err := svc.WatchdogSweep(ctx)
	if err != nil || n != 1 {
		t.Fatalf("sweep=%d err=%v", n, err)
	}
	got, _ := sessions.GetForUser(ctx, s.ID, 7)
	if got.State != browsercore.SessionTimeout {
		t.Fatalf("state=%s", got.State)
	}

	// 无 execution 的 created 会话可直接 abort（不需要 runner）
	s2 := &model.EgoliteSession{UserID: 8, Engine: "ego", Mode: "ops", SpaceName: "s-abort",
		PolicyJSON: raw, State: browsercore.SessionCreated}
	_ = sessions.Create(ctx, s2)
	got2, err := svc.Abort(ctx, 8, s2.ID, "test")
	if err != nil || got2.State != browsercore.SessionStopped {
		t.Fatalf("abort state=%v err=%v", got2, err)
	}
	// 终态再 abort → 冲突
	if _, err := svc.Abort(ctx, 8, s2.ID, "again"); !errors.Is(err, browsercore.ErrStateConflict) {
		t.Fatalf("终态 abort 应冲突, got %v", err)
	}
}

func TestSessionOwnership(t *testing.T) {
	ctx := context.Background()
	svc, sessions, _, _, _ := newSessionTestEnv(t)
	pol := browsercore.DefaultPolicy()
	pol.Domains = []string{"example.com"}
	raw, _ := json.Marshal(pol)
	s := &model.EgoliteSession{UserID: 9, Engine: "ego", Mode: "ops", SpaceName: "s-own",
		PolicyJSON: raw, State: browsercore.SessionCreated}
	_ = sessions.Create(ctx, s)
	if _, err := svc.Get(ctx, 10, s.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("越权读应 404, got %v", err)
	}
	if _, err := svc.Observe(ctx, browsercore.ObserveRequest{UserID: 10, SessionID: s.ID}); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("越权 observe 应 404, got %v", err)
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(sub) > 0 && (len(s) >= len(sub)) && (indexOf(s, sub) >= 0) {
			return true
		}
	}
	return false
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
