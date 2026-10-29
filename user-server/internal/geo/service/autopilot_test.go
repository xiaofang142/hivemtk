package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/geo/dto"
	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupAutopilotTestDB(t *testing.T) (repository.GeoAutopilotRunRepository, *gorm.DB) {
	t.Helper()
	db := testutil.NewTestDB(t,
		&model.GeoAutopilotRun{},
		&model.GeoConfig{},
		&model.GeoKeyword{},
		&model.GeoKeywordGroup{},
		&model.GeoArticle{},
		&model.GeoOptimization{},
		&model.GeoAPICall{},
		&model.GeoKnowledgeDocument{},
	)
	return repository.NewGeoAutopilotRunRepositoryWithDB(db), db
}

// T011-1 仓储 CRUD + steps JSON 往返 + List
func TestAutopilotRepoCRUD(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupAutopilotTestDB(t)

	run := &model.GeoAutopilotRun{Keyword: "客服系统", Status: "running"}
	run.SetSteps(autopilotPlanSteps("https://example.com"))
	if err := repo.Create(ctx, run); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if run.ID == "" {
		t.Fatal("ID should be auto-generated")
	}
	// steps 往返
	steps := run.GetSteps()
	if len(steps) != 9 {
		t.Fatalf("expected 9 steps with site_url, got %d", len(steps))
	}
	if steps[0].Name != "brand_check" || steps[8].Name != "summary" {
		t.Fatalf("unexpected step order: %s..%s", steps[0].Name, steps[8].Name)
	}

	got, err := repo.GetByID(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Keyword != "客服系统" || len(got.GetSteps()) != 9 {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}

	got.Status = "success"
	if err := repo.Update(ctx, got); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if ok, err := repo.HasRunning(ctx); err != nil || ok {
		t.Fatalf("HasRunning should be false, got %v %v", ok, err)
	}
	run2 := &model.GeoAutopilotRun{Keyword: "第二个", Status: "running"}
	if err := repo.Create(ctx, run2); err != nil {
		t.Fatalf("Create 2: %v", err)
	}
	if ok, err := repo.HasRunning(ctx); err != nil || !ok {
		t.Fatalf("HasRunning should be true, got %v %v", ok, err)
	}

	_, total, err := repo.List(ctx, 1, 10)
	if err != nil || total != 2 {
		t.Fatalf("List: total=%d err=%v", total, err)
	}
}

// T011-2 坏 steps JSON 不 panic（GetSteps 容错）
func TestAutopilotStepsJSONTolerant(t *testing.T) {
	run := &model.GeoAutopilotRun{Steps: "{bad json"}
	if steps := run.GetSteps(); len(steps) != 0 {
		t.Fatalf("expected empty steps for bad json, got %d", len(steps))
	}
	empty := &model.GeoAutopilotRun{}
	if steps := empty.GetSteps(); len(steps) != 0 {
		t.Fatalf("expected empty steps for blank, got %d", len(steps))
	}
}

// T004 步骤骨架：无 site_url 时 7 步，有 site_url 时 9 步
func TestAutopilotPlanSteps(t *testing.T) {
	if got := autopilotPlanSteps(""); len(got) != 7 {
		t.Fatalf("expected 7 steps without site_url, got %d", len(got))
	}
	if got := autopilotPlanSteps("https://x.com"); len(got) != 9 {
		t.Fatalf("expected 9 steps with site_url, got %d", len(got))
	}
}

// T011-3 参数校验（400 语义）
func TestAutopilotCreateValidation(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupAutopilotTestDB(t)
	svc := NewAutopilotService(repo)

	if _, err := svc.Create(ctx, &dto.AutopilotCreateRequest{Keyword: "  "}); err == nil || !strings.Contains(err.Error(), "关键词") {
		t.Fatalf("empty keyword should fail: %v", err)
	}
	if _, err := svc.Create(ctx, &dto.AutopilotCreateRequest{Keyword: "x", SiteURL: "example.com"}); err == nil || !strings.Contains(err.Error(), "http") {
		t.Fatalf("bad site url should fail: %v", err)
	}
	if _, err := svc.Create(ctx, &dto.AutopilotCreateRequest{Keyword: strings.Repeat("长", 101)}); err == nil {
		t.Fatal("overlong keyword should fail")
	}
}

// T011-4 并发闸：已有 running 行时 Create 拒绝（409 语义 sentinel）
func TestAutopilotRunningGate(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupAutopilotTestDB(t)
	svc := NewAutopilotService(repo)

	if err := repo.Create(ctx, &model.GeoAutopilotRun{Keyword: "占位", Status: "running"}); err != nil {
		t.Fatalf("seed running: %v", err)
	}
	_, err := svc.Create(ctx, &dto.AutopilotCreateRequest{Keyword: "客服系统"})
	if err == nil || err != ErrAutopilotRunning {
		t.Fatalf("expected ErrAutopilotRunning, got %v", err)
	}
}

// T011-5 品牌缺失硬失败：run 终态 failed 且 error 指引配置入口；零 LLM/出站调用
func TestAutopilotBrandMissingHardFail(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupAutopilotTestDB(t)
	svc := NewAutopilotService(repo)

	run, err := svc.Create(ctx, &dto.AutopilotCreateRequest{Keyword: "客服系统"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		got, gerr := repo.GetByID(ctx, run.ID)
		if gerr != nil {
			t.Fatalf("GetByID: %v", gerr)
		}
		if got.Status != "running" {
			if got.Status != "failed" {
				t.Fatalf("expected failed, got %s", got.Status)
			}
			if !strings.Contains(got.Error, "品牌") {
				t.Fatalf("error should guide brand config, got: %s", got.Error)
			}
			// brand_check 步骤必须 failed，后续步骤不得出现 success
			for _, st := range got.GetSteps() {
				if st.Name == "brand_check" && st.Status != "failed" {
					t.Fatalf("brand_check should be failed, got %s", st.Status)
				}
				if st.Name != "brand_check" && st.Status == "success" {
					t.Fatalf("step %s should not run after hard fail", st.Name)
				}
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("run did not reach terminal state in time")
}

// T011-6 markStep 进度单调与状态回填（不跑真实管线，直接驱动状态机）
func TestAutopilotMarkStepProgress(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupAutopilotTestDB(t)
	svc := NewAutopilotService(repo)

	run := &model.GeoAutopilotRun{Keyword: "k", Status: "running"}
	run.SetSteps(autopilotPlanSteps(""))
	if err := repo.Create(ctx, run); err != nil {
		t.Fatalf("Create: %v", err)
	}
	steps := run.GetSteps()
	svc.markStep(ctx, run, steps, "brand_check", "success", "品牌：x", nil)
	if run.Progress <= 0 || run.Progress >= 100 {
		t.Fatalf("progress should be in (0,100), got %d", run.Progress)
	}
	got, _ := repo.GetByID(ctx, run.ID)
	found := false
	for _, st := range got.GetSteps() {
		if st.Name == "brand_check" {
			found = true
			if st.Status != "success" || st.Detail != "品牌：x" {
				t.Fatalf("step not persisted: %+v", st)
			}
		}
	}
	if !found {
		t.Fatal("brand_check step missing after markStep")
	}

	svc.finishRun(ctx, run, "partial", "部分完成")
	got, _ = repo.GetByID(ctx, run.ID)
	if got.Status != "partial" || got.Progress != 100 || got.FinishedAt == nil {
		t.Fatalf("finishRun not persisted: %+v", got)
	}
}

// 审计回归：评分提取对两种 scores 载体同构（启发式 map[string]float64 /
// LLM parseScoreResult 的匿名 struct——修前两条路径类型断言全打不中，均分恒 0）
func TestAutopilotScoreTotal(t *testing.T) {
	heuristic := map[string]any{
		"scores": map[string]float64{"structure": 90, "total": 82.5},
	}
	if got := autopilotScoreTotal(heuristic); got != 82.5 {
		t.Fatalf("heuristic path: got %v want 82.5", got)
	}
	llmShaped := map[string]any{
		"scores": struct {
			Structure    float64 `json:"structure"`
			BrandMention float64 `json:"brand_mention"`
			Authority    float64 `json:"authority"`
			Citations    float64 `json:"citations"`
			Total        float64 `json:"total"`
		}{Structure: 80, Total: 77},
	}
	if got := autopilotScoreTotal(llmShaped); got != 77 {
		t.Fatalf("llm path: got %v want 77", got)
	}
	if got := autopilotScoreTotal(map[string]any{}); got != 0 {
		t.Fatalf("missing scores: got %v want 0", got)
	}
}

// 审计回归：TOCTOU 兜底——部分唯一索引在位时，绕过 HasRunning 的第二次
// running 插入必须撞 23505，且 Create 的错误映射把它翻译为 ErrAutopilotRunning
func TestAutopilotUniqueIndexBackstop(t *testing.T) {
	ctx := context.Background()
	repo, db := setupAutopilotTestDB(t)

	if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uq_geo_autopilot_runs_running ON geo_autopilot_runs (tenant_id) WHERE status = 'running'`).Error; err != nil {
		t.Fatalf("create index: %v", err)
	}
	defer db.Exec(`DROP INDEX IF EXISTS uq_geo_autopilot_runs_running`)

	if err := repo.Create(ctx, &model.GeoAutopilotRun{Keyword: "占位", Status: "running"}); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	err := repo.Create(ctx, &model.GeoAutopilotRun{Keyword: "撞线", Status: "running"})
	if !autopilotIsUniqueViolation(err) {
		t.Fatalf("second running insert should violate unique index, got %v", err)
	}
	svc := NewAutopilotService(repo)
	if _, err := svc.Create(ctx, &dto.AutopilotCreateRequest{Keyword: "客服系统"}); err != ErrAutopilotRunning {
		t.Fatalf("Create should map to ErrAutopilotRunning, got %v", err)
	}
}
