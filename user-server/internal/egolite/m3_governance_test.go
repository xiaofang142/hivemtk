package egolite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/browsercore"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/datatypes"
)

// M3 治理批（T19/T20/T21/T22）验收用例。T18（Policy 引擎）已由
// browsercore_test.go TestPolicy 钉住（空域名拒绝/上限拒绝/写确认）。

// ---------- T19 观测留存：清内容、保账本 ----------

func TestRetentionSweepNullsObservationKeepsLedger(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewTestDB(t, &model.EgoliteSession{}, &model.EgoliteRound{})
	if db == nil {
		t.Fatal("测试库不可用")
	}
	old := &model.EgoliteRound{SessionID: 1, ExecutionID: 1, UserID: 1, RoundNo: 1,
		Ok: boolPtr(true), ErrorCode: "", DurationMs: 12, Actions: 2,
		ObservationJSON: datatypes.JSON(`{"text":"页面正文","targets":[{"ref":"e1"}]}`),
		CreatedAt:       time.Now().UTC().Add(-8 * 24 * time.Hour)} // 7 天窗口外
	fresh := &model.EgoliteRound{SessionID: 1, ExecutionID: 2, UserID: 1, RoundNo: 2,
		Ok: boolPtr(true), DurationMs: 5, Actions: 1,
		ObservationJSON: datatypes.JSON(`{"text":"新观测"}`)}
	if err := db.Create(old).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(fresh).Error; err != nil {
		t.Fatal(err)
	}

	// 保留 7 天：旧观测应被清，新观测不动
	n, err := RetentionSweep(ctx, db, 7)
	if err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("应清理 1 行，实得 %d", n)
	}
	var gotOld, gotFresh model.EgoliteRound
	if err := db.First(&gotOld, old.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&gotFresh, fresh.ID).Error; err != nil {
		t.Fatal(err)
	}
	if gotOld.ObservationJSON != nil {
		t.Fatalf("过期观测内容应置空，实得 %s", gotOld.ObservationJSON)
	}
	// 账本列必须原样（验收红线：过期清理不误删账本）
	if gotOld.Ok == nil || !*gotOld.Ok || gotOld.DurationMs != 12 || gotOld.Actions != 2 {
		t.Fatalf("账本列被误删: %+v", gotOld)
	}
	if gotFresh.ObservationJSON == nil {
		t.Fatal("窗口内观测不应被清")
	}

	// 幂等：再扫一轮是 no-op
	n2, err := RetentionSweep(ctx, db, 7)
	if err != nil || n2 != 0 {
		t.Fatalf("重复扫描应 no-op，实得 n=%d err=%v", n2, err)
	}

	// 永不清理档：keepDays<=0 直接不动
	n3, err := RetentionSweep(ctx, db, 0)
	if err != nil || n3 != 0 {
		t.Fatalf("keepDays=0 应不清理，实得 n=%d err=%v", n3, err)
	}
}

// ---------- T20 事件协议 lint v2：缺 round_result 终帧判失败 ----------

func TestSessionDoneWithoutRoundResultFails(t *testing.T) {
	ctx := context.Background()
	svc, sessions, rounds, _, _ := newSessionTestEnv(t)

	pol := browsercore.DefaultPolicy()
	pol.Domains = []string{"example.com"}
	raw, _ := json.Marshal(pol)
	sess := &model.EgoliteSession{UserID: 9, Engine: "ego", Mode: "ops", SpaceName: "s-lint",
		PolicyJSON: raw, State: browsercore.SessionActive, Round: 1, ExecutionID: 201}
	if err := sessions.Create(ctx, sess); err != nil {
		t.Fatal(err)
	}
	// 当前轮只开了账（Ok=nil），脚本没打 round_result 就正常退出（done exit=0）
	if _, err := rounds.CreateOrIgnore(ctx, &model.EgoliteRound{SessionID: sess.ID, ExecutionID: 201, UserID: 9, RoundNo: 1}); err != nil {
		t.Fatal(err)
	}
	donePayload, _ := json.Marshal(map[string]any{"note": "exited"})
	if err := svc.IngestEvent(ctx, 9, RunnerEvent{Type: "done", ExecutionID: "ego-201", ExitCode: 0, Payload: donePayload}); err != nil {
		t.Fatal(err)
	}

	rec, err := rounds.GetByRound(ctx, sess.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Ok == nil || *rec.Ok {
		t.Fatalf("缺终帧的轮次应判失败，实得 ok=%v", rec.Ok)
	}
	if rec.ErrorCode != "MISSING_ROUND_RESULT" {
		t.Fatalf("error_code=%q，要 MISSING_ROUND_RESULT", rec.ErrorCode)
	}
	got, _ := sessions.GetForUser(ctx, sess.ID, 9)
	if got.State != browsercore.SessionFailed {
		t.Fatalf("会话应随缺帧判 failed，实得 %s", got.State)
	}

	// 对照组：终帧先到、再收 done —— 不许误判（幂等/乱序不破坏状态）
	svc2, sessions2, rounds2, _, _ := newSessionTestEnv(t)
	sess2 := &model.EgoliteSession{UserID: 10, Engine: "ego", Mode: "ops", SpaceName: "s-ok",
		PolicyJSON: raw, State: browsercore.SessionActive, Round: 1, ExecutionID: 202}
	if err := sessions2.Create(ctx, sess2); err != nil {
		t.Fatal(err)
	}
	if _, err := rounds2.CreateOrIgnore(ctx, &model.EgoliteRound{SessionID: sess2.ID, ExecutionID: 202, UserID: 10, RoundNo: 1}); err != nil {
		t.Fatal(err)
	}
	rr, _ := json.Marshal(map[string]any{"type": "round_result", "ok": true, "round": 1, "usage": map[string]any{"ms": 1, "actions": 1}})
	if err := svc2.IngestEvent(ctx, 10, RunnerEvent{Type: "round_result", ExecutionID: "ego-202", Payload: rr}); err != nil {
		t.Fatal(err)
	}
	if err := svc2.IngestEvent(ctx, 10, RunnerEvent{Type: "done", ExecutionID: "ego-202", ExitCode: 0, Payload: donePayload}); err != nil {
		t.Fatal(err)
	}
	ok2, _ := sessions2.GetForUser(ctx, sess2.ID, 10)
	if ok2.State != browsercore.SessionActive {
		t.Fatalf("正常终帧+正常退出不许误判失败，实得 %s", ok2.State)
	}
}

// ---------- T21 成本与配额：按日三维、超限拒绝码 ----------

func TestQuotaRejectionsPerDimension(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewTestDB(t, &model.EgoliteSession{}, &model.EgoliteRound{},
		&model.EgoliteExecution{}, &model.EgoEventLog{})
	if db == nil {
		t.Fatal("测试库不可用")
	}

	// 会话配额：正好 19 条放行，第 20 条拒（space 名唯一键，逐条起名）
	for i := 0; i < SessionQuotaPerDay-1; i++ {
		if err := db.Create(&model.EgoliteSession{UserID: 5, Engine: "ego", Mode: "ops",
			SpaceName: fmt.Sprintf("q-s-%d", i), State: browsercore.SessionFailed}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := CheckSessionQuota(ctx, db, 5); err != nil {
		t.Fatalf("未达配额应放行，实得 %v", err)
	}
	if err := db.Create(&model.EgoliteSession{UserID: 5, Engine: "ego", Mode: "ops",
		SpaceName: "q-s-final", State: browsercore.SessionFailed}).Error; err != nil {
		t.Fatal(err)
	}
	if err := CheckSessionQuota(ctx, db, 5); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("达配额应拒（429 面），实得 %v", err)
	}
	// 别的用户不受影响（按 user 维度）
	if err := CheckSessionQuota(ctx, db, 6); err != nil {
		t.Fatalf("其他用户不应被踩，实得 %v", err)
	}

	// 轮次配额：塞满 RoundQuotaPerDay 条轮次行（bulk insert 一次性灌）
	rounds := make([]*model.EgoliteRound, 0, RoundQuotaPerDay)
	for i := 0; i < RoundQuotaPerDay; i++ {
		rounds = append(rounds, &model.EgoliteRound{SessionID: 1, ExecutionID: uint(i + 1),
			UserID: 7, RoundNo: i + 1})
	}
	if err := db.CreateInBatches(rounds, 100).Error; err != nil {
		t.Fatal(err)
	}
	if err := CheckRoundQuota(ctx, db, 7); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("轮次配额应拒，实得 %v", err)
	}
	if err := CheckRoundQuota(ctx, db, 8); err != nil {
		t.Fatalf("轮次配额按用户隔离，实得 %v", err)
	}
}

// ---------- T22 审计导出：链完整、与库一致、归属校验 ----------

func TestExportSessionChainAndOwnership(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewTestDB(t, &model.EgoliteSession{}, &model.EgoliteRound{},
		&model.EgoliteExecution{}, &model.EgoEventLog{})
	if db == nil {
		t.Fatal("测试库不可用")
	}
	sess := &model.EgoliteSession{UserID: 11, Engine: "ego", Mode: "ops", SpaceName: "s-exp",
		State: browsercore.SessionFailed}
	if err := db.Create(sess).Error; err != nil {
		t.Fatal(err)
	}
	exec := &model.EgoliteExecution{UserID: 11, Origin: "session", SpaceName: "s-exp",
		Round: 1, State: model.EgoExecFailed}
	if err := db.Create(exec).Error; err != nil {
		t.Fatal(err)
	}
	round := &model.EgoliteRound{SessionID: sess.ID, ExecutionID: exec.ID, UserID: 11, RoundNo: 1,
		Ok: boolPtr(false), ErrorCode: "OP_FAILED", DurationMs: 9, Actions: 1,
		ObservationJSON: datatypes.JSON(`{"text":"some content"}`)}
	if err := db.Create(round).Error; err != nil {
		t.Fatal(err)
	}
	evs := []*model.EgoEventLog{
		{ExecutionID: exec.ID, Seq: 1, Type: "action"},
		{ExecutionID: exec.ID, Seq: 2, Type: "artifact"},
	}
	for _, ev := range evs {
		if err := db.Create(ev).Error; err != nil {
			t.Fatal(err)
		}
	}

	// JSON：链上四段都在，读数与库一致
	exp, err := ExportSession(ctx, db, 11, sess.ID, false)
	if err != nil {
		t.Fatalf("导出失败: %v", err)
	}
	if exp.Session.ID != sess.ID || len(exp.Rounds) != 1 || len(exp.Executions) != 1 || len(exp.Events) != 2 {
		t.Fatalf("导出链不完整: sess=%v rounds=%d execs=%d events=%d",
			exp.Session != nil, len(exp.Rounds), len(exp.Executions), len(exp.Events))
	}
	if exp.ArtifactCount != 1 {
		t.Fatalf("产物计数应=1，实得 %d", exp.ArtifactCount)
	}
	if exp.Rounds[0].ErrorCode != "OP_FAILED" {
		t.Fatalf("导出内容与库不一致: %+v", exp.Rounds[0])
	}

	// 归属校验：user 12 导 user 11 的会话 → 同响应防枚举（ErrSessionNotFound）
	if _, err := ExportSession(ctx, db, 12, sess.ID, false); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("越权导出应同响应防枚举，实得 %v", err)
	}
	// admin 通道可导
	if _, err := ExportSession(ctx, db, 99, sess.ID, true); err != nil {
		t.Fatalf("admin 应可导，实得 %v", err)
	}

	// CSV：表头 + 每轮一行，列读数与库一致
	csvB, err := ExportSessionCSV(ctx, db, 11, sess.ID, false)
	if err != nil {
		t.Fatalf("CSV 导出失败: %v", err)
	}
	csvText := string(csvB)
	if !contains(csvText, "round_no,execution_id,ok,error_code,duration_ms,actions,events,artifacts,created_at") {
		t.Fatalf("CSV 表头缺失: %s", csvText)
	}
	if !contains(csvText, "1,") || !contains(csvText, "OP_FAILED") {
		t.Fatalf("CSV 行内容与库不一致: %s", csvText)
	}
}

func boolPtr(b bool) *bool { return &b }

func contains(s, sub string) bool { return strings.Contains(s, sub) }
