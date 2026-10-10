package repository

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupSOPTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testutil.NewTestDB(t, &model.SOPTemplate{})
	if err := db.Exec("TRUNCATE TABLE sop_templates RESTART IDENTITY").Error; err != nil {
		t.Fatalf("truncate sop_templates: %v", err)
	}
	return db
}

func setupSOPRepoWithTX(t *testing.T) (repo *SOPTemplateRepository, tx *gorm.DB, cleanup func()) {
	t.Helper()
	db := setupSOPTestDB(t)
	tx = db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	repo = NewSOPTemplateRepository(tx)
	cleanup = func() { tx.Rollback() }
	return
}

func TestSOPTemplateRepository_Create(t *testing.T) {
	repo, _, done := setupSOPRepoWithTX(t)
	defer done()
	ctx := context.Background()

	tpl := &model.SOPTemplate{
		Name:       "韵达不发标准回复",
		Intent:     "logistics",
		Stage:      "initial",
		Template:   "亲，{{.ProductName}} 发 {{.ExpressCompany}} 哦",
		Confidence: 0.9,
		Enabled:    boolPtr(true),
	}
	if err := repo.Create(ctx, tpl); err != nil {
		t.Fatal(err)
	}
	if tpl.ID == 0 {
		t.Error("expected auto-increment ID")
	}
}

func TestSOPTemplateRepository_ListEnabled(t *testing.T) {
	repo, _, done := setupSOPRepoWithTX(t)
	defer done()
	ctx := context.Background()

	tpls := []*model.SOPTemplate{
		{Name: "sop1", Intent: "logistics", Stage: "initial", Template: "t1", Enabled: boolPtr(true), Priority: 10},
		{Name: "sop2", Intent: "logistics", Stage: "middle", Template: "t2", Enabled: boolPtr(true), Priority: 20},
		{Name: "sop3", Intent: "pricing", Stage: "initial", Template: "t3", Enabled: boolPtr(false), Priority: 5},
	}
	for _, x := range tpls {
		if err := repo.Create(ctx, x); err != nil {
			t.Fatal(err)
		}
	}

	got, err := repo.ListEnabled(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 enabled, got %d", len(got))
	}
	if got[0].Priority < got[1].Priority {
		t.Errorf("expected priority DESC, got %d then %d", got[0].Priority, got[1].Priority)
	}
}

func TestSOPTemplateRepository_MatchByIntent(t *testing.T) {
	repo, _, done := setupSOPRepoWithTX(t)
	defer done()
	ctx := context.Background()

	tpls := []*model.SOPTemplate{
		{Name: "l1", Intent: "logistics", Stage: "initial", Template: "t1", Enabled: boolPtr(true), Priority: 10},
		{Name: "l2", Intent: "logistics", Stage: "middle", Template: "t2", Enabled: boolPtr(true), Priority: 20},
		{Name: "p1", Intent: "pricing", Stage: "initial", Template: "t3", Enabled: boolPtr(true), Priority: 5},
	}
	for _, x := range tpls {
		if err := repo.Create(ctx, x); err != nil {
			t.Fatal(err)
		}
	}

	matches, err := repo.MatchByIntent(ctx, "logistics")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Errorf("expected 2 logistics templates, got %d", len(matches))
	}
}

func TestSOPTemplateRepository_MatchByIntentStage(t *testing.T) {
	repo, _, done := setupSOPRepoWithTX(t)
	defer done()
	ctx := context.Background()

	tpls := []*model.SOPTemplate{
		{Name: "li1", Intent: "logistics", Stage: "initial", Template: "t1", Enabled: boolPtr(true), Priority: 10},
		{Name: "li2", Intent: "logistics", Stage: "initial", Template: "t2", Enabled: boolPtr(true), Priority: 20},
		{Name: "lm1", Intent: "logistics", Stage: "middle", Template: "t3", Enabled: boolPtr(true), Priority: 5},
	}
	for _, x := range tpls {
		if err := repo.Create(ctx, x); err != nil {
			t.Fatal(err)
		}
	}

	matches, err := repo.MatchByIntentStage(ctx, "logistics", "initial")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Errorf("expected 2 logistics+initial templates, got %d", len(matches))
	}
	if matches[0].Priority < matches[1].Priority {
		t.Errorf("expected priority DESC, got %d then %d", matches[0].Priority, matches[1].Priority)
	}
}

func TestSOPTemplateRepository_DisabledNotMatched(t *testing.T) {
	repo, _, done := setupSOPRepoWithTX(t)
	defer done()
	ctx := context.Background()

	if err := repo.Create(ctx, &model.SOPTemplate{
		Name:     "禁用模板",
		Intent:   "logistics",
		Stage:    "initial",
		Template: "tt",
		Enabled:  boolPtr(false),
	}); err != nil {
		t.Fatal(err)
	}
	matches, _ := repo.MatchByIntent(ctx, "logistics")
	if len(matches) != 0 {
		t.Errorf("disabled SOP should not match, got %d", len(matches))
	}
}

// 智能体隔离测试 - 验证共享+私有隔离
func TestSOPTemplateRepository_AgentIsolation_MatchByIntentForAgent(t *testing.T) {
	repo, _, done := setupSOPRepoWithTX(t)
	defer done()
	ctx := context.Background()

	agentA := uint(10)
	agentB := uint(20)
	tpls := []*model.SOPTemplate{
		{Name: "shared_logistics", Intent: "logistics", Stage: "initial", Template: "shared", Enabled: boolPtr(true), AgentID: nil},
		{Name: "agentA_logistics", Intent: "logistics", Stage: "initial", Template: "agentA", Enabled: boolPtr(true), AgentID: &agentA},
		{Name: "agentB_logistics", Intent: "logistics", Stage: "initial", Template: "agentB", Enabled: boolPtr(true), AgentID: &agentB},
	}
	for _, x := range tpls {
		if err := repo.Create(ctx, x); err != nil {
			t.Fatal(err)
		}
	}

	matches, err := repo.MatchByIntentForAgent(ctx, "logistics", agentA)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Errorf("agentA expected 2 (shared+agentA), got %d", len(matches))
	}

	matches, err = repo.MatchByIntentForAgent(ctx, "logistics", agentB)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Errorf("agentB expected 2 (shared+agentB), got %d", len(matches))
	}

	matches, err = repo.MatchByIntent(ctx, "logistics")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 3 {
		t.Errorf("legacy agentID=0 expected 3 (all), got %d", len(matches))
	}
}

// MatchByIntentStageForAgent 测试
func TestSOPTemplateRepository_AgentIsolation_MatchByIntentStageForAgent(t *testing.T) {
	repo, _, done := setupSOPRepoWithTX(t)
	defer done()
	ctx := context.Background()

	agentA := uint(10)
	tpls := []*model.SOPTemplate{
		{Name: "shared_li", Intent: "logistics", Stage: "initial", Template: "shared", Enabled: boolPtr(true), AgentID: nil},
		{Name: "agentA_li", Intent: "logistics", Stage: "initial", Template: "agentA", Enabled: boolPtr(true), AgentID: &agentA},
		{Name: "agentA_lm", Intent: "logistics", Stage: "middle", Template: "agentA_middle", Enabled: boolPtr(true), AgentID: &agentA},
	}
	for _, x := range tpls {
		if err := repo.Create(ctx, x); err != nil {
			t.Fatal(err)
		}
	}

	matches, err := repo.MatchByIntentStageForAgent(ctx, "logistics", "initial", agentA)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Errorf("agentA logistics+initial expected 2, got %d", len(matches))
	}

	matches, err = repo.MatchByIntentStageForAgent(ctx, "logistics", "middle", agentA)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Errorf("agentA logistics+middle expected 1, got %d", len(matches))
	}
}

// MatchByAgent 强 1:1 测试
func TestSOPTemplateRepository_MatchByAgent_StrictOneToOne(t *testing.T) {
	repo, _, done := setupSOPRepoWithTX(t)
	defer done()
	ctx := context.Background()

	agentA := uint(10)
	agentB := uint(20)
	tpls := []*model.SOPTemplate{
		{Name: "shared", Intent: "logistics", Stage: "initial", Template: "shared", Enabled: boolPtr(true), AgentID: nil},
		{Name: "agentA1", Intent: "logistics", Stage: "initial", Template: "agentA1", Enabled: boolPtr(true), AgentID: &agentA},
		{Name: "agentA2", Intent: "logistics", Stage: "middle", Template: "agentA2", Enabled: boolPtr(true), AgentID: &agentA},
		{Name: "agentB", Intent: "logistics", Stage: "initial", Template: "agentB", Enabled: boolPtr(true), AgentID: &agentB},
	}
	for _, x := range tpls {
		if err := repo.Create(ctx, x); err != nil {
			t.Fatal(err)
		}
	}

	matches, err := repo.MatchByAgent(ctx, agentA, "logistics", "initial")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Errorf("agentA logistics+initial strict 1:1 expected 1, got %d", len(matches))
	}
	if matches[0].AgentID == nil || *matches[0].AgentID != agentA {
		t.Errorf("expected agentA's own template, got %v", matches[0].AgentID)
	}

	matches, err = repo.MatchByAgent(ctx, 0, "logistics", "initial")
	if err != nil {
		t.Fatal(err)
	}
	if matches != nil {
		t.Errorf("agentID=0 expected nil, got %d", len(matches))
	}
}

// ListShared / ListByAgent 测试
func TestSOPTemplateRepository_AgentIsolation_Lists(t *testing.T) {
	repo, _, done := setupSOPRepoWithTX(t)
	defer done()
	ctx := context.Background()

	agentA := uint(10)
	agentB := uint(20)
	tpls := []*model.SOPTemplate{
		{Name: "shared1", Intent: "logistics", Stage: "initial", Template: "s1", Enabled: boolPtr(true), AgentID: nil},
		{Name: "shared2", Intent: "pricing", Stage: "initial", Template: "s2", Enabled: boolPtr(true), AgentID: nil},
		{Name: "agentA1", Intent: "logistics", Stage: "initial", Template: "a1", Enabled: boolPtr(true), AgentID: &agentA},
		{Name: "agentA2", Intent: "logistics", Stage: "middle", Template: "a2", Enabled: boolPtr(true), AgentID: &agentA},
		{Name: "agentB1", Intent: "logistics", Stage: "initial", Template: "b1", Enabled: boolPtr(true), AgentID: &agentB},
	}
	for _, x := range tpls {
		if err := repo.Create(ctx, x); err != nil {
			t.Fatal(err)
		}
	}

	shared, err := repo.ListShared(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(shared) != 2 {
		t.Errorf("ListShared expected 2, got %d", len(shared))
	}

	aList, err := repo.ListByAgent(ctx, agentA, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(aList) != 2 {
		t.Errorf("ListByAgent(agentA) expected 2, got %d", len(aList))
	}
}

// ListWithFilter AgentID 字段测试
func TestSOPTemplateRepository_ListWithFilter_AgentID(t *testing.T) {
	repo, _, done := setupSOPRepoWithTX(t)
	defer done()
	ctx := context.Background()

	agentA := uint(10)
	tpls := []*model.SOPTemplate{
		{Name: "shared", Intent: "logistics", Stage: "initial", Template: "s", Enabled: boolPtr(true), AgentID: nil},
		{Name: "agentA1", Intent: "logistics", Stage: "initial", Template: "a1", Enabled: boolPtr(true), AgentID: &agentA},
		{Name: "agentA2", Intent: "logistics", Stage: "middle", Template: "a2", Enabled: boolPtr(true), AgentID: &agentA},
	}
	for _, x := range tpls {
		if err := repo.Create(ctx, x); err != nil {
			t.Fatal(err)
		}
	}

	agentZero := uint(0)
	got, _, err := repo.ListWithFilter(ctx, SOPTemplateListParams{AgentID: &agentZero, Page: 1, PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("ListWithFilter AgentID=&0 expected 1 (shared), got %d", len(got))
	}

	got, _, err = repo.ListWithFilter(ctx, SOPTemplateListParams{AgentID: &agentA, Page: 1, PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("ListWithFilter AgentID=&10 expected 2, got %d", len(got))
	}

	got, _, err = repo.ListWithFilter(ctx, SOPTemplateListParams{Page: 1, PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("ListWithFilter no AgentID expected 3, got %d", len(got))
	}
}

func TestSOPTemplateRepository_IncrementHitCount(t *testing.T) {
	repo, _, done := setupSOPRepoWithTX(t)
	defer done()
	ctx := context.Background()

	tpl := &model.SOPTemplate{
		Name: "incr", Intent: "logistics", Stage: "initial", Template: "t", Enabled: boolPtr(true),
	}
	if err := repo.Create(ctx, tpl); err != nil {
		t.Fatal(err)
	}
	if err := repo.IncrementHitCount(ctx, tpl.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.IncrementHitCount(ctx, tpl.ID); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetByID(ctx, tpl.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.HitCount != 2 {
		t.Errorf("expected hit_count=2, got %d", got.HitCount)
	}
}
