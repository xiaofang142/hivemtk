package eval

// dialogue_gold_test.go T-P8-04：对话级 gold set 三件事（LTC-15）。
//
// AC① 基线入库：fixture reference 自评 composite 必须 1.0（恒等自评：
// ChrF=1、关键词全中、动作全对），ToRunRecord 落 dialogue_eval_runs 真表，读回核对；
// AC② CI 离线：全程只用 ChrFEvaluator，不调 LLM；
// AC③ 反向掉分：degraded（删关键词 + 翻动作）composite 必须比基线掉至少 0.3。

import (
	"math"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func loadGoldFixture(t *testing.T) []DialogueSample {
	t.Helper()
	samples, err := LoadDialogueGold("testdata/dialogue_gold.json")
	if err != nil {
		t.Fatalf("读 fixture 失败: %v", err)
	}
	if len(samples) != 8 {
		t.Fatalf("样本数 = %d want 8", len(samples))
	}
	return samples
}

// TestDialogueGold_BaselineIsPerfect AC①：reference 自评满分。
func TestDialogueGold_BaselineIsPerfect(t *testing.T) {
	samples := loadGoldFixture(t)
	res, err := RunDialogueGold(NewChrFEvaluator(), samples, false)
	if err != nil {
		t.Fatalf("RunDialogueGold(reference): %v", err)
	}
	if res.SampleCount != 8 {
		t.Errorf("SampleCount = %d want 8", res.SampleCount)
	}
	if res.CompositeAvg != 1.0 {
		t.Errorf("基线 CompositeAvg = %v want 1.0", res.CompositeAvg)
	}
	if res.ActionMatchRate != 1.0 {
		t.Errorf("基线 ActionMatchRate = %v want 1.0", res.ActionMatchRate)
	}
	if res.KeyCoverageAvg != 1.0 {
		t.Errorf("基线 KeyCoverageAvg = %v want 1.0", res.KeyCoverageAvg)
	}
}

// TestDialogueGold_DegradedDrops AC③：退化版必须掉分（margin 0.3）。
func TestDialogueGold_DegradedDrops(t *testing.T) {
	samples := loadGoldFixture(t)
	chrf := NewChrFEvaluator()
	base, err := RunDialogueGold(chrf, samples, false)
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}
	degraded, err := RunDialogueGold(chrf, samples, true)
	if err != nil {
		t.Fatalf("degraded: %v", err)
	}
	drop := base.CompositeAvg - degraded.CompositeAvg
	if drop < 0.3 {
		t.Errorf("掉分 = %v（基线 %v → 退化 %v），want >= 0.3", drop, base.CompositeAvg, degraded.CompositeAvg)
	}
	if degraded.ActionMatchRate != 0.0 {
		t.Errorf("退化 ActionMatchRate = %v want 0.0（全部翻动作）", degraded.ActionMatchRate)
	}
	if degraded.KeyCoverageAvg != 0.0 {
		t.Errorf("退化 KeyCoverageAvg = %v want 0.0（关键词全删）", degraded.KeyCoverageAvg)
	}
}

// TestDialogueGold_BaselinePersisted AC①后半：基线入库 dialogue_eval_runs，读回核对。
func TestDialogueGold_BaselinePersisted(t *testing.T) {
	samples := loadGoldFixture(t)
	res, err := RunDialogueGold(NewChrFEvaluator(), samples, false)
	if err != nil {
		t.Fatalf("RunDialogueGold: %v", err)
	}
	db := testutil.NewTestDB(t, &model.DialogueEvalRun{})
	rec := res.ToRunRecord("gold-baseline-test")
	if err := db.Create(&rec).Error; err != nil {
		t.Fatalf("基线入库失败: %v", err)
	}
	var back model.DialogueEvalRun
	if err := db.First(&back, rec.ID).Error; err != nil {
		t.Fatalf("读回基线失败: %v", err)
	}
	if back.SampleCount != 8 {
		t.Errorf("SampleCount = %d want 8", back.SampleCount)
	}
	for _, tc := range []struct {
		name string
		got  float64
	}{
		{"CompositeAvg", back.CompositeAvg},
		{"KeyCoverageAvg", back.KeyCoverageAvg},
		{"ActionMatchRate", back.ActionMatchRate},
		{"ChrfAvg", back.ChrfAvg},
	} {
		if math.Abs(tc.got-1.0) > 1e-9 {
			t.Errorf("%s = %v want 1.0", tc.name, tc.got)
		}
	}
	if back.Details == "" || back.Details == "null" {
		t.Errorf("Details 为空，反向掉分定位不到样本")
	}
}

// TestDialogueGold_DegradedLengthMismatch 长度不一致直接报错，不静默截断。
func TestDialogueGold_DegradedLengthMismatch(t *testing.T) {
	samples := loadGoldFixture(t)
	samples[0].Degraded = samples[0].Degraded[:1]
	if _, err := RunDialogueGold(NewChrFEvaluator(), samples, true); err == nil {
		t.Errorf("degraded 长度不一致 want error，got nil")
	}
}

// TestBadCaseToDialogueSample bad-case 导出转样本：单轮形状，动作维跳过。
func TestBadCaseToDialogueSample(t *testing.T) {
	if got := BadCaseToDialogueSample(nil); got.ID != "" {
		t.Errorf("nil 输入 want 空样本，got %+v", got)
	}
	row := &model.BadCase{
		ID: "bc-1", Label: model.BadCaseLabelKBMissing,
		FixLayer:  model.BadCaseFixLayerKnowledge,
		QueryText: "退货政策是什么？", AnswerText: "库里没有这条。",
	}
	s := BadCaseToDialogueSample(row)
	if s.ID != "badcase-bc-1" || s.Scene != "badcase" {
		t.Errorf("样本头 = %+v want ID badcase-bc-1/scene badcase", s)
	}
	if len(s.Turns) != 2 {
		t.Fatalf("回合数 = %d want 2", len(s.Turns))
	}
	res, err := RunDialogueGold(NewChrFEvaluator(), []DialogueSample{s}, false)
	if err != nil {
		t.Fatalf("RunDialogueGold: %v", err)
	}
	if res.CompositeAvg != 1.0 {
		t.Errorf("单轮自评 = %v want 1.0（动作跳过、关键词空表恒 1）", res.CompositeAvg)
	}
	if res.ActionMatchRate != 1.0 {
		t.Errorf("无动作期望时 ActionMatchRate = %v want 1.0（空分母按恒成立）", res.ActionMatchRate)
	}
}
