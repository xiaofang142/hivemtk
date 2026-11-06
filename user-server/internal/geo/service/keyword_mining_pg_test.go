// keyword_mining_pg_test.go KeywordMiningService 持久化编排的 PG 定向回归（8217 专用实例）。
//
// AV-04b 下沉后端到端锁定：SaveMiningResults 的行数/字段逐项核对（空 results、
// 新 seed、重复 seed、既有子词、重复 results、已有意图不覆盖、新增引擎仅增一次）、
// 中途仓储失败的原成功计数与租户隔离（A/B 同文本不同父 ID，B 不更新）、
// BuildFunnel 的 COALESCE/坏 JSON 语义与租户范围。
package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

func TestSaveMiningResultsPG_SeedAndMergeLifecycle(t *testing.T) {
	db := testutil.NewTestDB(t, &model.GeoKeyword{})
	if db == nil {
		t.Fatal("测试库不可达：SaveMiningResults 端到端契约判不了（不 Skip）")
	}
	repo := repository.NewGeoKeywordRepositoryWithDB(db)
	svc := NewKeywordMiningService(repo, nil)
	ctx := context.Background()

	countRows := func(t *testing.T) (seeds, children int64) {
		t.Helper()
		if err := db.WithContext(ctx).Model(&model.GeoKeyword{}).Where("layer = ?", "seed").Count(&seeds).Error; err != nil {
			t.Fatalf("统计 seed 失败: %v", err)
		}
		if err := db.WithContext(ctx).Model(&model.GeoKeyword{}).Where("layer = ?", "suggest").Count(&children).Error; err != nil {
			t.Fatalf("统计 suggest 失败: %v", err)
		}
		return
	}

	// 1. 空 results → 0，且连种子都不建（改前即如此：短路在种子站位之前）。
	n, err := svc.SaveMiningResults(ctx, []string{"VPN"}, nil)
	if err != nil || n != 0 {
		t.Fatalf("空 results 应 (0, nil)，实际 (%d, %v)", n, err)
	}
	if seeds, children := countRows(t); seeds != 0 || children != 0 {
		t.Fatalf("空 results 不得写任何行：seed=%d suggest=%d", seeds, children)
	}

	// 2. 新 seed + 新子词：1 行种子、2 行 suggest，ParentID 回填、引擎与计数落库。
	r1 := &model.GeoKeyword{Keyword: "VPN推荐", ParentKeyword: "VPN", Layer: "suggest",
		Category: "suggest", Source: "suggest_baidu", Status: "active",
		SuggestEngines: `["baidu"]`, SuggestCount: 1}
	r2 := &model.GeoKeyword{Keyword: "VPN怎么选", ParentKeyword: "VPN", Layer: "suggest",
		Category: "suggest", Source: "suggest_bing", Status: "active",
		SuggestEngines: `["bing"]`, SuggestCount: 1}
	if n, err = svc.SaveMiningResults(ctx, []string{"VPN"}, []*model.GeoKeyword{r1, r2}); err != nil || n != 2 {
		t.Fatalf("首轮期望 (2, nil)，实际 (%d, %v)", n, err)
	}
	var seedRow model.GeoKeyword
	if err := db.WithContext(ctx).Where("keyword = ? AND layer = ?", "VPN", "seed").First(&seedRow).Error; err != nil {
		t.Fatalf("种子行未落库: %v", err)
	}
	if seedRow.Category != "seed" || seedRow.Source != "manual_seed" || seedRow.Status != "active" || seedRow.LastMinedAt == nil {
		t.Fatalf("种子行字段不对: %+v", seedRow)
	}
	var child1 model.GeoKeyword
	if err := db.WithContext(ctx).Where("keyword = ?", "VPN推荐").First(&child1).Error; err != nil {
		t.Fatalf("子词未落库: %v", err)
	}
	if child1.ParentID != seedRow.ID || child1.ParentKeyword != "VPN" || child1.SuggestEngines != `["baidu"]` {
		t.Fatalf("子词站位/引擎不对: %+v", child1)
	}
	if seeds, children := countRows(t); seeds != 1 || children != 2 {
		t.Fatalf("首轮行数不对：seed=%d suggest=%d", seeds, children)
	}

	// 3. 重复 seed（含空白变体）+ 重复 results：仍 1 个种子、不重复建子词。
	dup := &model.GeoKeyword{Keyword: "VPN怎么选", ParentKeyword: "VPN", Layer: "suggest",
		SuggestEngines: `["bing"]`, SuggestCount: 1}
	if n, err = svc.SaveMiningResults(ctx, []string{"VPN", "VPN", " VPN "}, []*model.GeoKeyword{dup, dup}); err != nil || n != 1 {
		t.Fatalf("重复轮期望 (1, nil)，实际 (%d, %v)", n, err)
	}
	if seeds, children := countRows(t); seeds != 1 || children != 2 {
		t.Fatalf("重复轮行数应不变：seed=%d suggest=%d", seeds, children)
	}

	// 4. 已有意图不覆盖 + 重复引擎仅增一次：再跑一轮带新引擎与不同意图的结果。
	marked := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := db.WithContext(ctx).Model(&model.GeoKeyword{}).Where("keyword = ?", "VPN推荐").
		Updates(map[string]any{"query_intent": "pricing", "intent": "pricing", "last_mined_at": marked}).Error; err != nil {
		t.Fatalf("预置已有意图失败: %v", err)
	}
	re := &model.GeoKeyword{Keyword: "VPN推荐", ParentKeyword: "VPN", Layer: "suggest",
		SuggestEngines: `["baidu","bing","bing"]`, SuggestCount: 1, QueryIntent: "how_to"}
	if n, err = svc.SaveMiningResults(ctx, []string{"VPN"}, []*model.GeoKeyword{re}); err != nil || n != 1 {
		t.Fatalf("合并轮期望 (1, nil)，实际 (%d, %v)", n, err)
	}
	var merged model.GeoKeyword
	if err := db.WithContext(ctx).Where("keyword = ?", "VPN推荐").First(&merged).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if merged.SuggestEngines != `["baidu","bing"]` {
		t.Fatalf("新增引擎应恰增一次: %s", merged.SuggestEngines)
	}
	if merged.SuggestCount != 2 {
		t.Fatalf("SuggestCount 应 1→2，实际 %d", merged.SuggestCount)
	}
	if merged.QueryIntent != "pricing" || merged.Intent != "pricing" {
		t.Fatalf("已有意图不得覆盖: %q/%q", merged.QueryIntent, merged.Intent)
	}
	if merged.LastMinedAt == nil || !merged.LastMinedAt.After(marked) {
		t.Fatalf("last_mined_at 应被刷新: %v (基准 %v)", merged.LastMinedAt, marked)
	}

	// 5. 既有子词 ParentID 不覆盖。
	if merged.ParentID != seedRow.ID {
		t.Fatalf("既有 ParentID 不得被改: %q", merged.ParentID)
	}
}

// miningFailDecor 只在指定位次让 UpdateMiningFields 失败的仓储装饰器。
type miningFailDecor struct {
	repository.GeoKeywordRepository
	failOn    func(call int, id string) bool
	callCount int
}

func (d *miningFailDecor) UpdateMiningFields(ctx context.Context, id string, p repository.GeoKeywordMiningPatch) error {
	d.callCount++
	if d.failOn != nil && d.failOn(d.callCount, id) {
		return errInjectedMiningUpdate
	}
	return d.GeoKeywordRepository.UpdateMiningFields(ctx, id, p)
}

var errInjectedMiningUpdate = errorString("注入的更新失败")

type errorString string

func (e errorString) Error() string { return string(e) }

func TestSaveMiningResultsPG_MidwayFailurePartialCountAndBTenantUntouched(t *testing.T) {
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() {
		tenant.ResetRegistryForTest()
		tenant.SetMode(tenant.ModeOff)
	})
	db := testutil.NewTestDB(t, &model.GeoKeyword{})
	if db == nil {
		t.Fatal("测试库不可达：中途失败契约判不了（不 Skip）")
	}
	repo := repository.NewGeoKeywordRepositoryWithDB(db)

	const (
		tenantA = uint(401)
		tenantB = uint(402)
	)
	ctxA := tenant.WithTenantID(context.Background(), tenantA)
	ctxB := tenant.WithTenantID(context.Background(), tenantB)

	// A/B 租户同文本、不同父 ID：A 行 parent-A / 引擎 baidu；B 行 parent-B / 引擎 sogou。
	mkRow := func(parentID string, engines string, count int) *model.GeoKeyword {
		return &model.GeoKeyword{Keyword: "同文本词", Layer: "suggest", Status: "active",
			ParentID: parentID, ParentKeyword: parentID,
			SuggestEngines: engines, SuggestCount: count,
			QueryIntent: "pricing", Intent: "pricing"}
	}
	rowA := mkRow("parent-A", `["baidu"]`, 1)
	rowB := mkRow("parent-B", `["sogou"]`, 2)
	if err := repo.Create(ctxA, rowA); err != nil {
		t.Fatalf("A 行创建失败: %v", err)
	}
	if err := repo.Create(ctxB, rowB); err != nil {
		t.Fatalf("B 行创建失败: %v", err)
	}

	readRow := func(t *testing.T, ctx context.Context, id string) model.GeoKeyword {
		t.Helper()
		var got model.GeoKeyword
		if err := db.WithContext(ctx).Where("id = ?", id).First(&got).Error; err != nil {
			t.Fatalf("回查失败: %v", err)
		}
		return got
	}

	// ── 正向：租户 A 的合并只落 A 行，B 行（不同父 ID/引擎/计数）原样。──
	decor := &miningFailDecor{GeoKeywordRepository: repo}
	svcDecor := &KeywordMiningService{kwRepo: decor}
	ra := &model.GeoKeyword{Keyword: "同文本词", ParentKeyword: "种子X", Layer: "suggest",
		SuggestEngines: `["baidu","bing"]`, SuggestCount: 1}
	n, err := svcDecor.SaveMiningResults(ctxA, []string{"种子X"}, []*model.GeoKeyword{ra})
	if err != nil || n != 1 {
		t.Fatalf("A 租户合并期望 (1, nil)，实际 (%d, %v)", n, err)
	}
	gotA := readRow(t, ctxA, rowA.ID)
	if gotA.SuggestEngines != `["baidu","bing"]` || gotA.SuggestCount != 2 || gotA.ParentID != "parent-A" {
		t.Fatalf("A 行应合并引擎且保父 ID: %+v", gotA)
	}
	gotB := readRow(t, ctxB, rowB.ID)
	if gotB.SuggestEngines != `["sogou"]` || gotB.SuggestCount != 2 || gotB.ParentID != "parent-B" {
		t.Fatalf("B 行不得被 A 租户的挖掘触碰: %+v", gotB)
	}
	if gotB.LastMinedAt != nil {
		t.Fatalf("B 行 last_mined_at 不得被刷新: %v", gotB.LastMinedAt)
	}

	// ── 中途失败：第 2 次更新失败 → 返回原成功计数 1 + 错误；已成功行保持；B 仍不动。──
	exA1 := mkRow("parent-A2", `["baidu"]`, 1)
	exA1.Keyword = "中途词1"
	exA2 := mkRow("parent-A3", `["baidu"]`, 1)
	exA2.Keyword = "中途词2"
	exB1 := mkRow("parent-B2", `["sogou"]`, 3)
	exB1.Keyword = "中途词1"
	exB2 := mkRow("parent-B3", `["sogou"]`, 4)
	exB2.Keyword = "中途词2"
	for _, r := range []*model.GeoKeyword{exA1, exA2} {
		if err := repo.Create(ctxA, r); err != nil {
			t.Fatalf("A 夹具创建失败: %v", err)
		}
	}
	for _, r := range []*model.GeoKeyword{exB1, exB2} {
		if err := repo.Create(ctxB, r); err != nil {
			t.Fatalf("B 夹具创建失败: %v", err)
		}
	}

	decor2 := &miningFailDecor{GeoKeywordRepository: repo}
	decor2.failOn = func(call int, id string) bool { return call == 2 }
	svcDecor2 := &KeywordMiningService{kwRepo: decor2}
	results := []*model.GeoKeyword{
		{Keyword: "中途词1", ParentKeyword: "中途种子", Layer: "suggest", SuggestEngines: `["baidu","bing"]`, SuggestCount: 1},
		{Keyword: "中途词2", ParentKeyword: "中途种子", Layer: "suggest", SuggestEngines: `["baidu","bing"]`, SuggestCount: 1},
	}
	n, err = svcDecor2.SaveMiningResults(ctxA, []string{"中途种子"}, results)
	if err == nil {
		t.Fatal("中途失败应返回错误")
	}
	if n != 1 {
		t.Fatalf("应返回原成功计数 1（第 1 行已更新、第 2 行失败即止），实际 %d", n)
	}
	gotA1 := readRow(t, ctxA, exA1.ID)
	if gotA1.SuggestEngines != `["baidu","bing"]` || gotA1.SuggestCount != 2 {
		t.Fatalf("已成功行应保持合并结果: %+v", gotA1)
	}
	gotA2 := readRow(t, ctxA, exA2.ID)
	if gotA2.SuggestEngines != `["baidu"]` || gotA2.SuggestCount != 1 {
		t.Fatalf("失败行不得被合并: %+v", gotA2)
	}
	gotB1 := readRow(t, ctxB, exB1.ID)
	if gotB1.SuggestEngines != `["sogou"]` || gotB1.SuggestCount != 3 || gotB1.ParentID != "parent-B2" {
		t.Fatalf("中途失败时 B1 不得更新: %+v", gotB1)
	}
	gotB2 := readRow(t, ctxB, exB2.ID)
	if gotB2.SuggestEngines != `["sogou"]` || gotB2.SuggestCount != 4 || gotB2.ParentID != "parent-B3" {
		t.Fatalf("中途失败时 B2 不得更新: %+v", gotB2)
	}
}

func TestBuildFunnelPG_CoalesceAndBadJSONFullMatrix(t *testing.T) {
	db := testutil.NewTestDB(t, &model.GeoKeyword{})
	if db == nil {
		t.Fatal("测试库不可达：漏斗全矩阵判不了（不 Skip）")
	}
	repo := repository.NewGeoKeywordRepositoryWithDB(db)
	svc := NewKeywordMiningService(repo, nil)
	ctx := context.Background()

	fixtures := []string{
		`(1,'g1','k1','seed','pricing','decision','active','[]')`,
		`(1,'g2','k2',NULL,NULL,NULL,'active','')`, // 全 NULL → seed/unknown/unknown
		`(1,'g3','k3','','','','active','')`,       // 全空串 → '' 独立组
		`(1,'g4','k4','suggest','case_study','retention','active','["baidu","bing"]')`,
		`(1,'g5','k5','suggest','case_study','retention','active','["bing","sogou"]')`, // 跨行重复引擎
		`(1,'g6','k6','suggest','case_study','retention','inactive','["baidu"]')`,      // inactive：整体排除
		`(1,'g7','k7','longtail','how_to','cognitive','active','["sogou"]')`,
		`(1,'g8','k8','related','case_study','retention','active','{"bad json"')`, // 坏 JSON：跳过不报错
		`(1,'g9','k9','suggest','how_to','cognitive','active','')`,                // 空载荷：谓词排除
	}
	insert := `INSERT INTO geo_keywords
		(tenant_id, id, keyword, layer, query_intent, funnel_stage, status, suggest_engines)
		VALUES ` + strings.Join(fixtures, ", ")
	if err := db.Exec(insert).Error; err != nil {
		t.Fatalf("夹具写入失败: %v", err)
	}

	f, err := svc.BuildFunnel(ctx)
	if err != nil {
		t.Fatalf("BuildFunnel 失败: %v", err)
	}
	if f.SeedCount != 2 || f.RelatedCount != 1 || f.SuggestCount != 3 || f.LongtailCount != 1 {
		t.Fatalf("四层计数不对: seed=%d related=%d suggest=%d longtail=%d",
			f.SeedCount, f.RelatedCount, f.SuggestCount, f.LongtailCount)
	}
	if f.Total != 8 { // g6 inactive 排除
		t.Fatalf("总量应为 8，实际 %d", f.Total)
	}
	wantIntents := map[string]int{"pricing": 1, "unknown": 1, "": 1, "case_study": 3, "how_to": 2}
	if len(f.Intents) != len(wantIntents) {
		t.Fatalf("intents 键集应 %v（NULL≠''），实际 %v", wantIntents, f.Intents)
	}
	for k, v := range wantIntents {
		if f.Intents[k] != v {
			t.Fatalf("intents[%q] 应为 %d，实际 %d", k, v, f.Intents[k])
		}
	}
	wantStages := map[string]int{"decision": 1, "unknown": 1, "": 1, "retention": 3, "cognitive": 2}
	if len(f.FunnelStages) != len(wantStages) {
		t.Fatalf("stages 键集应 %v，实际 %v", wantStages, f.FunnelStages)
	}
	for k, v := range wantStages {
		if f.FunnelStages[k] != v {
			t.Fatalf("stages[%q] 应为 %d，实际 %d", k, v, f.FunnelStages[k])
		}
	}
	// 引擎计数：g4 [baidu,bing] + g5 [bing,sogou]（跨行 bing 累加）；
	// g7 是 longtail 层不进引擎聚合；g8 坏 JSON 跳过；g6 inactive、g9 空载荷排除。baidu=1, bing=2, sogou=1。
	wantEngines := map[string]int{"baidu": 1, "bing": 2, "sogou": 1}
	if len(f.SuggestEngines) != len(wantEngines) {
		t.Fatalf("引擎键集应 %v（坏 JSON 跳过、空载荷排除），实际 %v", wantEngines, f.SuggestEngines)
	}
	for k, v := range wantEngines {
		if f.SuggestEngines[k] != v {
			t.Fatalf("engines[%q] 应为 %d，实际 %d", k, v, f.SuggestEngines[k])
		}
	}
}

func TestBuildFunnelPG_TenantScopedCounts(t *testing.T) {
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() {
		tenant.ResetRegistryForTest()
		tenant.SetMode(tenant.ModeOff)
	})
	db := testutil.NewTestDB(t, &model.GeoKeyword{})
	if db == nil {
		t.Fatal("测试库不可达：漏斗租户范围判不了（不 Skip）")
	}
	repo := repository.NewGeoKeywordRepositoryWithDB(db)
	svc := NewKeywordMiningService(repo, nil)

	const (
		tenantA = uint(411)
		tenantB = uint(412)
	)
	ctxA := tenant.WithTenantID(context.Background(), tenantA)
	ctxB := tenant.WithTenantID(context.Background(), tenantB)

	mk := func(keyword, layer, engines string) *model.GeoKeyword {
		return &model.GeoKeyword{Keyword: keyword, Layer: layer, Status: "active",
			SuggestEngines: engines, QueryIntent: "pricing", FunnelStage: "decision"}
	}
	// A：2 seed + 1 suggest(bing) + 1 longtail；B：1 seed + 1 suggest(baidu)。
	for _, r := range []*model.GeoKeyword{mk("a-seed1", "seed", ""), mk("a-seed2", "seed", ""),
		mk("a-sug", "suggest", `["bing"]`), mk("a-lt", "longtail", "")} {
		if err := repo.Create(ctxA, r); err != nil {
			t.Fatalf("A 夹具失败: %v", err)
		}
	}
	for _, r := range []*model.GeoKeyword{mk("b-seed", "seed", ""), mk("b-sug", "suggest", `["baidu"]`)} {
		if err := repo.Create(ctxB, r); err != nil {
			t.Fatalf("B 夹具失败: %v", err)
		}
	}

	fa, err := svc.BuildFunnel(ctxA)
	if err != nil {
		t.Fatalf("A 漏斗失败: %v", err)
	}
	if fa.SeedCount != 2 || fa.SuggestCount != 1 || fa.LongtailCount != 1 || fa.RelatedCount != 0 || fa.Total != 4 {
		t.Fatalf("A 漏斗应只见 A 行: %+v", fa)
	}
	if fa.SuggestEngines["bing"] != 1 || len(fa.SuggestEngines) != 1 {
		t.Fatalf("A 引擎应只含 bing: %v", fa.SuggestEngines)
	}
	if fa.Intents["pricing"] != 4 || fa.FunnelStages["decision"] != 4 {
		t.Fatalf("A intent/stage 应只统计 A 行: %v %v", fa.Intents, fa.FunnelStages)
	}

	fb, err := svc.BuildFunnel(ctxB)
	if err != nil {
		t.Fatalf("B 漏斗失败: %v", err)
	}
	if fb.SeedCount != 1 || fb.SuggestCount != 1 || fb.Total != 2 {
		t.Fatalf("B 漏斗应只见 B 行: %+v", fb)
	}
	if fb.SuggestEngines["baidu"] != 1 || len(fb.SuggestEngines) != 1 {
		t.Fatalf("B 引擎应只含 baidu: %v", fb.SuggestEngines)
	}
}
