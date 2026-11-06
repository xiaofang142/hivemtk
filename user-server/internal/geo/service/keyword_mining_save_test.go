// keyword_mining_save_test.go KeywordMiningService 持久化编排的单测（fake 仓储，不碰 PG）。
//
// AV-04b 下沉后，SaveMiningResults/BuildFunnel 的库交互全部走 GeoKeywordRepository
// 接口。这里用 fake 仓储锁定业务层语义：种子站位/去重、父子回填、引擎合并「仅增一次」、
// 已有意图不覆盖、部分成功计数与错误路径、漏斗映射与坏 JSON 跳过。
// SQL 语义（IN/白名单/COALESCE/软删除/租户注入）由 repository 层 PG 回归锁定。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
)

// fakeMiningRepo 只覆写 KeywordMiningService 会调用的方法；
// 其余方法经由内嵌接口透传——若业务代码调到未覆写方法，nil 接口 panic 即滥用信号。
type fakeMiningRepo struct {
	repository.GeoKeywordRepository

	existing  []model.GeoKeyword // FindExistingByKeywords 的固定回包
	findErr   error
	touchErr  error
	batchErr  error
	updateErr func(id string, p repository.GeoKeywordMiningPatch) error

	idSeq        int
	seedBatches  [][]*model.GeoKeyword // 每次 BatchCreate 收到的批次（按调用序）
	updates      []repository.GeoKeywordMiningPatch
	updatedIDs   []string
	touchedIDs   []string
	batchCalls   int
	findKeywords []string
}

func (f *fakeMiningRepo) FindExistingByKeywords(_ context.Context, keywords []string) ([]model.GeoKeyword, error) {
	f.findKeywords = append(f.findKeywords, keywords...)
	if f.findErr != nil {
		return nil, f.findErr
	}
	return f.existing, nil
}

func (f *fakeMiningRepo) BatchCreate(_ context.Context, rows []*model.GeoKeyword) error {
	f.batchCalls++
	if f.batchErr != nil {
		return f.batchErr
	}
	// 模拟真实 BeforeCreate：入库行获得 ID，供 ParentID 回填链路使用。
	for _, row := range rows {
		if row.ID == "" {
			f.idSeq++
			row.ID = fmt.Sprintf("gen-%d", f.idSeq)
		}
	}
	f.seedBatches = append(f.seedBatches, rows)
	return nil
}

func (f *fakeMiningRepo) TouchMinedAtByIDs(_ context.Context, ids []string, _ time.Time) error {
	if f.touchErr != nil {
		return f.touchErr
	}
	f.touchedIDs = append(f.touchedIDs, ids...)
	return nil
}

func (f *fakeMiningRepo) UpdateMiningFields(_ context.Context, id string, p repository.GeoKeywordMiningPatch) error {
	if f.updateErr != nil {
		if err := f.updateErr(id, p); err != nil {
			return err
		}
	}
	f.updates = append(f.updates, p)
	f.updatedIDs = append(f.updatedIDs, id)
	return nil
}

func miningSuggest(keyword, parent string, engines ...string) *model.GeoKeyword {
	b, _ := json.Marshal(engines)
	return &model.GeoKeyword{
		Keyword:        keyword,
		ParentKeyword:  parent,
		Source:         "suggest_baidu",
		Category:       "suggest",
		Layer:          "suggest",
		Intent:         "info",
		FunnelStage:    "cognitive",
		Status:         "active",
		SuggestEngines: string(b),
		SuggestCount:   1,
	}
}

func TestSaveMiningResults_EmptyResultsReturnsZeroWithoutWrites(t *testing.T) {
	fake := &fakeMiningRepo{}
	svc := &KeywordMiningService{kwRepo: fake}

	n, err := svc.SaveMiningResults(context.Background(), []string{"种子词"}, nil)
	if err != nil || n != 0 {
		t.Fatalf("空 results 应返回 (0, nil)，实际 (%d, %v)", n, err)
	}
	if fake.batchCalls != 0 || len(fake.touchedIDs) != 0 || len(fake.updates) != 0 {
		t.Fatalf("空 results 不得触发任何写库：batches=%d touched=%d updates=%d",
			fake.batchCalls, len(fake.touchedIDs), len(fake.updates))
	}
}

func TestSaveMiningResults_NewSeedCreatesSeedRowAndBackfillsParent(t *testing.T) {
	fake := &fakeMiningRepo{}
	svc := &KeywordMiningService{kwRepo: fake}

	results := []*model.GeoKeyword{miningSuggest("咖啡机推荐", "咖啡机")}
	n, err := svc.SaveMiningResults(context.Background(), []string{"咖啡机"}, results)
	if err != nil {
		t.Fatalf("SaveMiningResults 失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("新词插入应计 1，实际 %d", n)
	}
	if fake.batchCalls != 2 {
		t.Fatalf("应有种子批+结果批两次 BatchCreate，实际 %d 次", fake.batchCalls)
	}
	seedBatch := fake.seedBatches[0]
	if len(seedBatch) != 1 {
		t.Fatalf("种子批应 1 行，实际 %d", len(seedBatch))
	}
	seed := seedBatch[0]
	if seed.Layer != "seed" || seed.Category != "seed" || seed.Source != "manual_seed" || seed.Status != "active" {
		t.Fatalf("种子行字段不对: %+v", seed)
	}
	if seed.LastMinedAt == nil {
		t.Fatal("种子行 LastMinedAt 应被设置")
	}
	resultBatch := fake.seedBatches[1]
	if len(resultBatch) != 1 {
		t.Fatalf("结果批应 1 行，实际 %d", len(resultBatch))
	}
	if resultBatch[0].ParentID != seed.ID {
		t.Fatalf("结果词 ParentID 应回填为种子行 ID %q，实际 %q", seed.ID, resultBatch[0].ParentID)
	}
	if len(fake.updates) != 0 {
		t.Fatalf("全为新词不应有更新调用，实际 %d", len(fake.updates))
	}
}

func TestSaveMiningResults_DuplicateSeedsTrimmedAndDeduped(t *testing.T) {
	fake := &fakeMiningRepo{}
	svc := &KeywordMiningService{kwRepo: fake}

	n, err := svc.SaveMiningResults(context.Background(),
		[]string{"咖啡机", "咖啡机", " 咖啡机 "}, []*model.GeoKeyword{miningSuggest("词A", "咖啡机")})
	if err != nil || n != 1 {
		t.Fatalf("期望 (1, nil)，实际 (%d, %v)", n, err)
	}
	if len(fake.seedBatches[0]) != 1 {
		t.Fatalf("重复种子（含空白变体）应去重为 1 行，实际 %d", len(fake.seedBatches[0]))
	}
}

func TestSaveMiningResults_ExistingWordMergesEngineOnceAndKeepsIntent(t *testing.T) {
	existing := model.GeoKeyword{
		ID:             "kw-1",
		Keyword:        "咖啡机对比",
		Layer:          "suggest",
		ParentID:       "parent-old",
		ParentKeyword:  "老种子",
		QueryIntent:    "pricing",
		Intent:         "pricing",
		FunnelStage:    "decision",
		SuggestEngines: `["baidu"]`,
		SuggestCount:   1,
		Status:         "active",
	}
	fake := &fakeMiningRepo{existing: []model.GeoKeyword{existing}}
	svc := &KeywordMiningService{kwRepo: fake}

	r := miningSuggest("咖啡机对比", "咖啡机")
	b, _ := json.Marshal([]string{"baidu", "bing", "bing"}) // 重复引擎名只算一次
	r.SuggestEngines = string(b)
	r.QueryIntent = "how_to" // 不得覆盖已有意图
	r.FunnelStage = ""       // 新行空 stage 不得清掉已有值

	n, err := svc.SaveMiningResults(context.Background(), []string{"咖啡机"}, []*model.GeoKeyword{r})
	if err != nil {
		t.Fatalf("SaveMiningResults 失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("既有词更新应计 1，实际 %d", n)
	}
	if fake.batchCalls != 1 {
		t.Fatalf("仅种子站位应 1 次 BatchCreate，实际 %d", fake.batchCalls)
	}
	if len(fake.updates) != 1 || len(fake.updatedIDs) != 1 || fake.updatedIDs[0] != "kw-1" {
		t.Fatalf("应对 kw-1 恰好一次白名单更新，实际 updates=%v", fake.updatedIDs)
	}
	p := fake.updates[0]
	if p.SuggestEngines != `["baidu","bing"]` {
		t.Fatalf("引擎合并结果应为 [\"baidu\",\"bing\"]，实际 %s", p.SuggestEngines)
	}
	if p.SuggestCount != 2 {
		t.Fatalf("新增一个引擎 SuggestCount 应 +1 为 2，实际 %d", p.SuggestCount)
	}
	if p.QueryIntent != "pricing" || p.Intent != "pricing" {
		t.Fatalf("已有意图不得覆盖：query_intent=%q intent=%q", p.QueryIntent, p.Intent)
	}
	if p.FunnelStage != "decision" {
		t.Fatalf("已有 funnel_stage 不得被空值清掉，实际 %q", p.FunnelStage)
	}
	if p.ParentID != "parent-old" {
		t.Fatalf("已有 ParentID 不得覆盖，实际 %q", p.ParentID)
	}
	if p.Layer != "suggest" {
		t.Fatalf("layer 应保持 suggest，实际 %q", p.Layer)
	}
	if p.LastMinedAt == nil || p.UpdatedAt.IsZero() {
		t.Fatalf("last_mined_at/updated_at 应刷新: %+v", p)
	}
}

func TestSaveMiningResults_DuplicateResultsProcessedOnce(t *testing.T) {
	fake := &fakeMiningRepo{}
	svc := &KeywordMiningService{kwRepo: fake}

	r1 := miningSuggest("同词", "种子")
	r2 := miningSuggest("同词", "种子")
	b2, _ := json.Marshal([]string{"bing"})
	r2.SuggestEngines = string(b2) // 第二条引擎不同也不得二次处理

	n, err := svc.SaveMiningResults(context.Background(), []string{"种子"}, []*model.GeoKeyword{r1, r2})
	if err != nil || n != 1 {
		t.Fatalf("重复 results 应只处理一次计 1，实际 (%d, %v)", n, err)
	}
	if fake.batchCalls != 2 {
		t.Fatalf("种子批+结果批应各 1 次，实际 %d", fake.batchCalls)
	}
	if len(fake.seedBatches[1]) != 1 {
		t.Fatalf("重复 results 应去重为 1 行，实际 %d", len(fake.seedBatches[1]))
	}
	if len(fake.updates) != 0 {
		t.Fatalf("不应走更新路径，实际 %d 次", len(fake.updates))
	}
}

func TestSaveMiningResults_MidwayUpdateFailureKeepsSuccessCount(t *testing.T) {
	fake := &fakeMiningRepo{existing: []model.GeoKeyword{
		{ID: "kw-1", Keyword: "词1", Layer: "suggest", SuggestEngines: `["baidu"]`, SuggestCount: 1},
		{ID: "kw-2", Keyword: "词2", Layer: "suggest", SuggestEngines: `["baidu"]`, SuggestCount: 1},
	}}
	fake.updateErr = func(id string, _ repository.GeoKeywordMiningPatch) error {
		if id == "kw-2" {
			return errors.New("注入的更新失败")
		}
		return nil
	}
	svc := &KeywordMiningService{kwRepo: fake}

	results := []*model.GeoKeyword{miningSuggest("词1", "s"), miningSuggest("词2", "s")}
	n, err := svc.SaveMiningResults(context.Background(), []string{"s"}, results)
	if err == nil {
		t.Fatal("中途更新失败应返回错误")
	}
	if n != 1 {
		t.Fatalf("kw-1 已更新成功，计数应为 1（非全败、非全成），实际 %d", n)
	}
	if len(fake.updatedIDs) != 1 || fake.updatedIDs[0] != "kw-1" {
		t.Fatalf("失败后不应继续更新后续行，实际 updatedIDs=%v", fake.updatedIDs)
	}
}

func TestSaveMiningResults_CreateFailureReturnsZeroAndError(t *testing.T) {
	fake := &fakeMiningRepo{batchErr: errors.New("注入的批量写入失败")}
	svc := &KeywordMiningService{kwRepo: fake}

	n, err := svc.SaveMiningResults(context.Background(), []string{"种子"},
		[]*model.GeoKeyword{miningSuggest("词A", "种子")})
	if err == nil || n != 0 {
		t.Fatalf("创建失败应返回 (0, err)，实际 (%d, %v)", n, err)
	}
}

func TestSaveMiningResults_SeedTouchFailureAbortsBeforeResults(t *testing.T) {
	existing := model.GeoKeyword{ID: "seed-1", Keyword: "老种子", Layer: "seed", Status: "active"}
	fake := &fakeMiningRepo{existing: []model.GeoKeyword{existing}, touchErr: errors.New("注入的刷时间失败")}
	svc := &KeywordMiningService{kwRepo: fake}

	n, err := svc.SaveMiningResults(context.Background(), []string{"老种子"},
		[]*model.GeoKeyword{miningSuggest("新词", "老种子")})
	if err == nil || n != 0 {
		t.Fatalf("刷 seed 时间失败应返回 (0, err)，实际 (%d, %v)", n, err)
	}
	if len(fake.updates) != 0 || fake.batchCalls != 0 {
		t.Fatalf("失败应中止在种子刷新处，不得继续结果写入：updates=%d batches=%d", len(fake.updates), fake.batchCalls)
	}
}

func TestSaveMiningResults_FindErrorReturnsZeroAndError(t *testing.T) {
	fake := &fakeMiningRepo{findErr: errors.New("注入的查询失败")}
	svc := &KeywordMiningService{kwRepo: fake}

	n, err := svc.SaveMiningResults(context.Background(), []string{"种子"},
		[]*model.GeoKeyword{miningSuggest("词A", "种子")})
	if err == nil || n != 0 {
		t.Fatalf("查询失败应返回 (0, err)，实际 (%d, %v)", n, err)
	}
	if fake.batchCalls != 0 {
		t.Fatalf("查询失败不得进入写路径，实际 %d 次 BatchCreate", fake.batchCalls)
	}
}

func TestBuildFunnel_MappingBadJSONSkippedAndEngineDedup(t *testing.T) {
	svc := &KeywordMiningService{kwRepo: &funnelFakeRepo{}}

	f, err := svc.BuildFunnel(context.Background())
	if err != nil {
		t.Fatalf("BuildFunnel 失败: %v", err)
	}
	if f.SeedCount != 3 || f.RelatedCount != 2 || f.SuggestCount != 1 || f.LongtailCount != 0 {
		t.Fatalf("四层计数不对: seed=%d related=%d suggest=%d longtail=%d",
			f.SeedCount, f.RelatedCount, f.SuggestCount, f.LongtailCount)
	}
	if f.Total != 7 {
		t.Fatalf("总量应为 7（含未命名层），实际 %d", f.Total)
	}
	wantIntents := map[string]int{"unknown": 1, "": 2, "pricing": 4}
	if len(f.Intents) != len(wantIntents) {
		t.Fatalf("intents 键集应 %v（'' 与 unknown 不得合并），实际 %v", wantIntents, f.Intents)
	}
	for k, v := range wantIntents {
		if f.Intents[k] != v {
			t.Fatalf("intents[%q] 应为 %d，实际 %d", k, v, f.Intents[k])
		}
	}
	wantStages := map[string]int{"unknown": 2, "": 1, "decision": 4}
	for k, v := range wantStages {
		if f.FunnelStages[k] != v {
			t.Fatalf("stages[%q] 应为 %d，实际 %d", k, v, f.FunnelStages[k])
		}
	}
	wantEngines := map[string]int{"baidu": 1, "bing": 3, "sogou": 1} // P1/P2/P3 各计一次：行内重复去重，跨行累加
	if len(f.SuggestEngines) != len(wantEngines) {
		t.Fatalf("引擎键集应 %v（坏 JSON 行跳过、行内重复引擎只计一次），实际 %v", wantEngines, f.SuggestEngines)
	}
	for k, v := range wantEngines {
		if f.SuggestEngines[k] != v {
			t.Fatalf("engines[%q] 应为 %d，实际 %d", k, v, f.SuggestEngines[k])
		}
	}
}

// funnelFakeRepo 只供 BuildFunnel 使用：漏斗不走 FindExisting/BatchCreate，
// 其余方法经内嵌接口透传（误用即 nil panic）。
type funnelFakeRepo struct {
	repository.GeoKeywordRepository
}

func (f *funnelFakeRepo) FunnelCounts(_ context.Context, field repository.GeoKeywordFunnelField) ([]repository.GeoKeywordFunnelCount, error) {
	switch field {
	case repository.GeoKeywordFunnelLayer:
		return []repository.GeoKeywordFunnelCount{
			{Value: "seed", Count: 3},
			{Value: "related", Count: 2},
			{Value: "suggest", Count: 1},
			{Value: "", Count: 1}, // 空字符串层：进总量但不进四层命名桶
		}, nil
	case repository.GeoKeywordFunnelQueryIntent:
		return []repository.GeoKeywordFunnelCount{
			{Value: "unknown", Count: 1},
			{Value: "", Count: 2},
			{Value: "pricing", Count: 4},
		}, nil
	case repository.GeoKeywordFunnelStage:
		return []repository.GeoKeywordFunnelCount{
			{Value: "unknown", Count: 2},
			{Value: "", Count: 1},
			{Value: "decision", Count: 4},
		}, nil
	}
	return nil, fmt.Errorf("未知字段")
}

func (f *funnelFakeRepo) ListSuggestEnginePayloads(_ context.Context) ([]string, error) {
	return []string{
		`["baidu","bing"]`,
		`["bing"]`,
		`["bing","bing"]`, // 行内重复引擎只计一次
		`{"bad json"`,     // 坏 JSON：跳过且不报错
		`[]`,
		`["", "sogou"]`, // 空引擎名跳过
	}, nil
}
