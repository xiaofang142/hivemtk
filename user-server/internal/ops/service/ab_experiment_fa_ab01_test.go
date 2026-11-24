package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"hivemtk-user/internal/ops/model"
)

func faAB01Req(name string, variantNames ...string) *CreateExperimentRequest {
	req := &CreateExperimentRequest{
		Name:         name,
		Description:  "d",
		SourceType:   "page",
		SourceID:     "page_1",
		TrafficSplit: 50,
	}
	for i, vn := range variantNames {
		req.Variants = append(req.Variants, VariantConfig{Name: vn, IsControl: i == 0, Weight: 50})
	}
	return req
}

// 运行中实验：编辑被拒，变体身份不变。
func TestFAAB01_RunningExperimentRejectsEdit(t *testing.T) {
	ctx := context.Background()
	db := setupABExperimentServiceTestDB(t)
	svc := NewABExperimentService()
	exp, err := svc.CreateExperiment(ctx, faAB01Req("orig", "Control", "Variant A"))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.StartExperiment(ctx, exp.ID); err != nil {
		t.Fatal(err)
	}
	var before []model.ABVariant
	if err := db.Where("experiment_id = ?", exp.ID).Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateExperiment(ctx, exp.ID, faAB01Req("hacked", "X", "Y")); !errors.Is(err, ErrExperimentNotEditable) {
		t.Fatalf("运行中编辑应被拒，实际 %v", err)
	}
	var after []model.ABVariant
	if err := db.Where("experiment_id = ?", exp.ID).Find(&after).Error; err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) {
		t.Fatalf("变体数变了: %d -> %d", len(before), len(after))
	}
	for i := range before {
		if before[i].ID != after[i].ID || before[i].Name != after[i].Name {
			t.Fatalf("变体身份变了: %+v -> %+v", before[i], after[i])
		}
	}
	var cur model.ABExperiment
	if err := db.First(&cur, exp.ID).Error; err != nil {
		t.Fatal(err)
	}
	if cur.Name != "orig" || cur.Status != "running" {
		t.Fatalf("主记录被改: %+v", cur)
	}
}

// 草稿编辑失败（第二个变体名超长触发 DB 错）→ 整事务回滚，不留半套配置。
func TestFAAB01_DraftEditRollsBackOnFailure(t *testing.T) {
	ctx := context.Background()
	db := setupABExperimentServiceTestDB(t)
	svc := NewABExperimentService()
	exp, err := svc.CreateExperiment(ctx, faAB01Req("orig", "Control", "Variant A"))
	if err != nil {
		t.Fatal(err)
	}
	var before []model.ABVariant
	if err := db.Where("experiment_id = ?", exp.ID).Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	longName := strings.Repeat("x", 51)
	_, err = svc.UpdateExperiment(ctx, exp.ID, faAB01Req("hacked", "Control", longName))
	if err == nil {
		t.Fatal("超长变体名应触发 DB 错误")
	}
	var cur model.ABExperiment
	if err := db.First(&cur, exp.ID).Error; err != nil {
		t.Fatal(err)
	}
	if cur.Name != "orig" {
		t.Fatalf("主记录未回滚: %+v", cur)
	}
	var after []model.ABVariant
	if err := db.Where("experiment_id = ?", exp.ID).Find(&after).Error; err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("变体回滚失败: %d -> %d", len(before), len(after))
	}
	for i := range before {
		if before[i].ID != after[i].ID || before[i].Name != after[i].Name {
			t.Fatalf("变体身份变了: %+v -> %+v", before[i], after[i])
		}
	}
}

// 已有历史事件 → 编辑被拒，事件 VariantID 仍指向原变体。
func TestFAAB01_HistoricalEventsRejectEdit(t *testing.T) {
	ctx := context.Background()
	db := setupABExperimentServiceTestDB(t)
	svc := NewABExperimentService()
	exp, err := svc.CreateExperiment(ctx, faAB01Req("orig", "Control", "Variant A"))
	if err != nil {
		t.Fatal(err)
	}
	var variants []model.ABVariant
	if err := db.Where("experiment_id = ?", exp.ID).Order("id").Find(&variants).Error; err != nil {
		t.Fatal(err)
	}
	origVariantID := variants[0].ID
	ev := &model.ABConversionEvent{ExperimentID: exp.ID, VariantID: origVariantID, EventName: "signup", UserID: "u1"}
	if err := db.Create(ev).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateExperiment(ctx, exp.ID, faAB01Req("hacked", "X", "Y")); !errors.Is(err, ErrExperimentHistoricalData) {
		t.Fatalf("历史数据应拒绝编辑，实际 %v", err)
	}
	var got model.ABConversionEvent
	if err := db.First(&got, ev.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.VariantID != origVariantID {
		t.Fatalf("事件 VariantID 变了: %d -> %d", origVariantID, got.VariantID)
	}
	var cur model.ABExperiment
	if err := db.First(&cur, exp.ID).Error; err != nil {
		t.Fatal(err)
	}
	if cur.Name != "orig" {
		t.Fatalf("主记录被改: %+v", cur)
	}
}

// 创建中途失败（第二个变体名超长）→ 无实验行、无变体行。
func TestFAAB01_CreateRollsBackOnFailure(t *testing.T) {
	ctx := context.Background()
	db := setupABExperimentServiceTestDB(t)
	svc := NewABExperimentService()
	longName := strings.Repeat("y", 51)
	_, err := svc.CreateExperiment(ctx, faAB01Req("boom", "Control", longName))
	if err == nil {
		t.Fatal("超长变体名应触发 DB 错误")
	}
	var exps []model.ABExperiment
	if err := db.Where("name = ?", "boom").Find(&exps).Error; err != nil {
		t.Fatal(err)
	}
	if len(exps) != 0 {
		t.Fatalf("回滚后有实验行: %d", len(exps))
	}
	var variants []model.ABVariant
	if err := db.Find(&variants).Error; err != nil {
		t.Fatal(err)
	}
	if len(variants) != 0 {
		t.Fatalf("回滚后有变体行: %d", len(variants))
	}
}
