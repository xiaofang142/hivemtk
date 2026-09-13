package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/browser_automation/repository"

	"gorm.io/datatypes"
)

// R27-1 I5 审计导出：SessionExport 归并会话+步+命令流+LLM 成本账的契约锁。
// 归属校验/仓储缺位置空不报错（与 D1 读侧同纪律）。

type exportSessionRepo struct {
	fakeSessionRepo
	sess *model.BrowserSession
}

func (f *exportSessionRepo) GetByID(_ context.Context, id, userID uint) (*model.BrowserSession, error) {
	if f.sess == nil || f.sess.ID != id || f.sess.UserID != userID {
		return nil, errors.New("not found")
	}
	return f.sess, nil
}

type exportStepRepo struct {
	repository.BrowserStepRepository
	steps []*model.BrowserStep
}

func (e *exportStepRepo) ListBySessionID(_ context.Context, _ uint) ([]*model.BrowserStep, error) {
	return e.steps, nil
}

type exportCmdLogRepo struct {
	repository.BrowserCommandLogRepository
	logs []*model.BrowserCommandLog
}

func (e *exportCmdLogRepo) ListBySessionID(_ context.Context, _ uint) ([]*model.BrowserCommandLog, error) {
	return e.logs, nil
}

type exportPlanRepo struct {
	repository.BrowserLLMPlanRepository
	plans []*model.BrowserLLMPlan
}

func (e *exportPlanRepo) ListBySessionID(_ context.Context, _ uint, limit int) ([]*model.BrowserLLMPlan, error) {
	if limit > 0 && limit < len(e.plans) {
		return e.plans[:limit], nil
	}
	return e.plans, nil
}

func TestSessionExportAggregates(t *testing.T) {
	sess := &model.BrowserSession{ID: 9, UserID: 3, Status: "completed"}
	sr := &exportSessionRepo{sess: sess}
	steps := &exportStepRepo{steps: []*model.BrowserStep{{ID: 1, SessionID: 9, Action: "click", Status: "success"}}}
	cl := &exportCmdLogRepo{logs: []*model.BrowserCommandLog{{ID: 1, SessionID: 9, Seq: 1, Direction: "command", Action: "click", Ok: true}}}
	pl := &exportPlanRepo{plans: []*model.BrowserLLMPlan{
		{ID: 1, SessionID: 9, Kind: "plan", Goal: "g", TokenIn: 10, TokenOut: 2, Snapshot: "BIG-IGNORED"},
		{ID: 2, SessionID: 9, Kind: "judge", Goal: "g", Reasoning: "approve"},
	}}
	s := NewSessionService(sr, steps, nil)
	s.SetCommandLogRepository(cl)
	s.SetLLMPlanRepository(pl)

	got, gs, gl, gp, err := s.SessionExport(context.Background(), 9, 3)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 9 || len(gs) != 1 || len(gl) != 1 || len(gp) != 2 {
		t.Fatalf("归并不全: sess=%v steps=%d logs=%d plans=%d", got, len(gs), len(gl), len(gp))
	}
	if gp[0].Kind != "plan" || gp[0].TokenIn != 10 {
		t.Errorf("LLM 成本账字段丢失: %+v", gp[0])
	}
	// Snapshot 大文本不进导出行（SessionExportRow 无该字段即证明）
	blob, _ := json.Marshal(gp)
	if strings.Contains(string(blob), "BIG-IGNORED") {
		t.Error("导出不得携带 snapshot 大文本")
	}

	// 归属越界 → 报错（GetByID 校验 userID）
	if _, _, _, _, err := s.SessionExport(context.Background(), 9, 999); err == nil {
		t.Error("越权归属必须报错")
	}

	// 仓储缺位（nil）不报错、置空（D1 同纪律）
	s2 := NewSessionService(sr, steps, nil)
	if _, _, logs, plans, err := s2.SessionExport(context.Background(), 9, 3); err != nil || logs != nil || plans != nil {
		t.Errorf("未装配分量应置空不报错: %v %v %v", err, logs, plans)
	}

	// llm_plan steps JSON 原样透传为字节
	s3 := NewSessionService(sr, &exportStepRepo{}, nil)
	s3.SetLLMPlanRepository(&exportPlanRepo{plans: []*model.BrowserLLMPlan{
		{ID: 1, SessionID: 9, Kind: "plan", Steps: datatypes.JSON(`[{"action":"click"}]`)},
	}})
	if _, _, _, rows, _ := s3.SessionExport(context.Background(), 9, 3); len(rows) != 1 || string(rows[0].Steps) != `[{"action":"click"}]` {
		t.Errorf("steps JSON 透传失败: %+v", rows)
	}
}
