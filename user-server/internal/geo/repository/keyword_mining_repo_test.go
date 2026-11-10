// keyword_mining_repo_test.go 关键词挖掘持久化组的仓储层 SQL 契约（PG 定向，8217）。
//
// AV-04b 自 KeywordMiningService 下沉的五组查询/写库在这里锁定 SQL 语义：
// keyword IN 谓词与软删除、TouchMinedAt/UpdateMiningFields 的字段白名单、
// 漏斗 COALESCE 的 NULL≠空字符串语义、suggest_engines 载荷的谓词边界。
// 业务编排/合并逻辑在 service 层单测与 keyword_mining_pg_test.go。
package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func miningRepoSetupDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testutil.NewTestDB(t, &model.GeoKeyword{})
	if db == nil {
		t.Fatal("测试库不可达：挖掘仓储 SQL 契约判不了（不 Skip，跳过等于没锁）")
	}
	return db
}

func TestKeywordMiningRepo_FindExistingByKeywordsInPredicateAndSoftDelete(t *testing.T) {
	db := miningRepoSetupDB(t)
	repo := NewGeoKeywordRepositoryWithDB(db)
	ctx := context.Background()

	keep := &model.GeoKeyword{Keyword: "在库词", Layer: "suggest", Status: "active"}
	if err := repo.Create(ctx, keep); err != nil {
		t.Fatalf("创建在库词失败: %v", err)
	}
	deleted := &model.GeoKeyword{Keyword: "已删词", Layer: "seed", Status: "active"}
	if err := repo.Create(ctx, deleted); err != nil {
		t.Fatalf("创建待删词失败: %v", err)
	}
	if err := repo.Delete(ctx, deleted.ID); err != nil {
		t.Fatalf("软删除失败: %v", err)
	}

	rows, err := repo.FindExistingByKeywords(ctx, []string{"在库词", "已删词", "不在库词", "在库词"})
	if err != nil {
		t.Fatalf("FindExistingByKeywords 失败: %v", err)
	}
	if len(rows) != 1 || rows[0].Keyword != "在库词" || rows[0].ID != keep.ID {
		t.Fatalf("IN 谓词应只命中未删除的在库词（重复入参不去重也不放行软删行），实际 %+v", rows)
	}
}

func TestKeywordMiningRepo_TouchMinedAtByIDsWhitelist(t *testing.T) {
	db := miningRepoSetupDB(t)
	repo := NewGeoKeywordRepositoryWithDB(db)
	ctx := context.Background()

	row := &model.GeoKeyword{Keyword: "种子词", Layer: "seed", Status: "active",
		Source: "manual_seed", SuggestCount: 3, SuggestEngines: `["baidu"]`}
	if err := repo.Create(ctx, row); err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	// PG timestamp 只存到微秒：内存里的 CreatedAt 带纳秒，先对齐精度再比对，
	// 否则等值断言会因截断而假红（本仓统一口径，见 audit_test.go）。
	createdAt := row.CreatedAt.Truncate(time.Microsecond)
	now := time.Now()
	if err := repo.TouchMinedAtByIDs(ctx, []string{row.ID}, now); err != nil {
		t.Fatalf("TouchMinedAtByIDs 失败: %v", err)
	}

	var got model.GeoKeyword
	if err := db.WithContext(ctx).Where("id = ?", row.ID).First(&got).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if got.LastMinedAt == nil || got.UpdatedAt.Before(now.Add(-time.Second)) {
		t.Fatalf("last_mined_at/updated_at 应刷新为 now: last=%v updated=%v", got.LastMinedAt, got.UpdatedAt)
	}
	if got.SuggestCount != 3 || got.SuggestEngines != `["baidu"]` || got.Source != "manual_seed" ||
		got.Layer != "seed" || got.Status != "active" || got.Keyword != "种子词" {
		t.Fatalf("白名单外字段不得被触碰: %+v", got)
	}
	if !got.CreatedAt.Equal(createdAt) {
		t.Fatalf("created_at 不得被改: %v → %v", createdAt.Truncate(time.Microsecond), got.CreatedAt)
	}

	// 空切片：与改前 GORM 行为一致——WHERE 恒假，不更新不报错（服务层另有 len>0 守卫）。
	if err := repo.TouchMinedAtByIDs(ctx, []string{}, now); err != nil {
		t.Fatalf("空切片应等价于不更新而非报错: %v", err)
	}
}

func TestKeywordMiningRepo_UpdateMiningFieldsWhitelist(t *testing.T) {
	db := miningRepoSetupDB(t)
	repo := NewGeoKeywordRepositoryWithDB(db)
	ctx := context.Background()

	row := &model.GeoKeyword{Keyword: "既有词", Source: "suggest_baidu", Category: "suggest",
		Status: "active", SearchVolume: 42, Cluster: "聚类c1"}
	if err := repo.Create(ctx, row); err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	// PG timestamp 只存到微秒：内存里的 CreatedAt 带纳秒，先对齐精度再比对，
	// 否则等值断言会因截断而假红（本仓统一口径，见 audit_test.go）。
	createdAt := row.CreatedAt.Truncate(time.Microsecond)
	now := time.Now()
	patch := GeoKeywordMiningPatch{
		Layer:          "suggest",
		ParentID:       "parent-1",
		ParentKeyword:  "种子词",
		QueryIntent:    "how_to",
		Intent:         "how_to",
		FunnelStage:    "cognitive",
		SuggestEngines: `["bing"]`,
		SuggestCount:   2,
		LastMinedAt:    &now,
		UpdatedAt:      now,
	}
	if err := repo.UpdateMiningFields(ctx, row.ID, patch); err != nil {
		t.Fatalf("UpdateMiningFields 失败: %v", err)
	}

	var got model.GeoKeyword
	if err := db.WithContext(ctx).Where("id = ?", row.ID).First(&got).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if got.Layer != "suggest" || got.ParentID != "parent-1" || got.ParentKeyword != "种子词" ||
		got.QueryIntent != "how_to" || got.Intent != "how_to" || got.FunnelStage != "cognitive" ||
		got.SuggestEngines != `["bing"]` || got.SuggestCount != 2 {
		t.Fatalf("白名单十字段应逐项落库: %+v", got)
	}
	if got.LastMinedAt == nil || got.UpdatedAt.Before(now.Add(-time.Second)) {
		t.Fatalf("last_mined_at/updated_at 应落库: %v %v", got.LastMinedAt, got.UpdatedAt)
	}
	// 身份字段物理不在白名单内：写入前后必须原样。
	if got.Keyword != "既有词" || got.Source != "suggest_baidu" || got.Category != "suggest" ||
		got.Status != "active" || got.SearchVolume != 42 || got.Cluster != "聚类c1" {
		t.Fatalf("白名单外字段被改写: %+v", got)
	}
	if !got.CreatedAt.Equal(createdAt) {
		t.Fatalf("created_at 不得被改: %v → %v", createdAt.Truncate(time.Microsecond), got.CreatedAt)
	}
}

func TestKeywordMiningRepo_FunnelCountsCoalesceSemantics(t *testing.T) {
	db := miningRepoSetupDB(t)
	repo := NewGeoKeywordRepositoryWithDB(db)
	ctx := context.Background()

	// 夹具含：NULL layer / 空字符串 layer、NULL/空 intent 与 stage、inactive 行。
	// COALESCE 只兜 NULL——空字符串必须保持独立分组，不得并入缺省标签。
	// 每条为完整元组：tenant_id, id, keyword, layer, query_intent, funnel_stage, status, suggest_engines。
	fixtures := []string{
		`(1,'f1','词1','seed','pricing','decision','active','[]')`,
		`(1,'f2','词2',NULL,NULL,NULL,'active','')`,                                // 全 NULL → seed/unknown/unknown
		`(1,'f3','词3','','','','active','')`,                                      // 全空串 → 独立 '' 组
		`(1,'f4','词4','suggest','case_study','retention','active',NULL)`,          // engines NULL：谓词排除
		`(1,'f5','词5','suggest','case_study','retention','active','')`,            // engines ''：谓词排除
		`(1,'f6','词6','suggest','case_study','retention','inactive','["baidu"]')`, // inactive：漏斗整体排除
		`(1,'f7','词7','longtail','case_study','retention','active','["baidu"]')`,  // 非 suggest 层：引擎不聚合
		`(1,'f8','词8','related','case_study','retention','active','')`,
		`(1,'f9','词9','suggest','case_study','retention','active','{"bad"')`, // 坏 JSON 原样返回
	}
	insert := `INSERT INTO geo_keywords
		(tenant_id, id, keyword, layer, query_intent, funnel_stage, status, suggest_engines)
		VALUES ` + strings.Join(fixtures, ", ")
	if err := db.Exec(insert).Error; err != nil {
		t.Fatalf("夹具写入失败: %v\nSQL: %s", err, insert)
	}

	layerRows, err := repo.FunnelCounts(ctx, GeoKeywordFunnelLayer)
	if err != nil {
		t.Fatalf("FunnelCounts(layer) 失败: %v", err)
	}
	wantLayer := map[string]int64{"seed": 2, "related": 1, "suggest": 3, "longtail": 1, "": 1}
	if len(layerRows) != len(wantLayer) {
		t.Fatalf("layer 分组键集应 %v（NULL 并 seed，'' 独立），实际 %+v", wantLayer, layerRows)
	}
	var total int64
	for _, r := range layerRows {
		if wantLayer[r.Value] != r.Count {
			t.Fatalf("layer[%q] 应为 %d，实际 %d", r.Value, wantLayer[r.Value], r.Count)
		}
		total += r.Count
	}
	if total != 8 {
		t.Fatalf("总量应为 8（f6 inactive 排除），实际 %d", total)
	}

	intentRows, err := repo.FunnelCounts(ctx, GeoKeywordFunnelQueryIntent)
	if err != nil {
		t.Fatalf("FunnelCounts(query_intent) 失败: %v", err)
	}
	wantIntent := map[string]int64{"pricing": 1, "unknown": 1, "": 1, "case_study": 5} // f6 inactive 不进任何分组
	if len(intentRows) != len(wantIntent) {
		t.Fatalf("intent 分组键集应 %v（NULL 并 unknown，'' 独立），实际 %+v", wantIntent, intentRows)
	}
	for _, r := range intentRows {
		if wantIntent[r.Value] != r.Count {
			t.Fatalf("intent[%q] 应为 %d，实际 %d", r.Value, wantIntent[r.Value], r.Count)
		}
	}

	stageRows, err := repo.FunnelCounts(ctx, GeoKeywordFunnelStage)
	if err != nil {
		t.Fatalf("FunnelCounts(funnel_stage) 失败: %v", err)
	}
	wantStage := map[string]int64{"decision": 1, "unknown": 1, "": 1, "retention": 5} // f6 inactive 不进任何分组
	if len(stageRows) != len(wantStage) {
		t.Fatalf("stage 分组键集应 %v，实际 %+v", wantStage, stageRows)
	}
	for _, r := range stageRows {
		if wantStage[r.Value] != r.Count {
			t.Fatalf("stage[%q] 应为 %d，实际 %d", r.Value, wantStage[r.Value], r.Count)
		}
	}

	payloads, err := repo.ListSuggestEnginePayloads(ctx)
	if err != nil {
		t.Fatalf("ListSuggestEnginePayloads 失败: %v", err)
	}
	// 谓词 status='active' AND layer='suggest' AND suggest_engines <> ''：
	// f4(NULL)/f5('')/f6(inactive)/f7(longtail) 排除，仅 f9 坏 JSON 原样交回。
	if len(payloads) != 1 || payloads[0] != `{"bad"` {
		t.Fatalf("引擎载荷应恰为 1 条坏 JSON 原文，实际 %#v", payloads)
	}

	// 闭合枚举外的取值必须报错，而不是拼出任意列名的查询。
	if _, err := repo.FunnelCounts(ctx, GeoKeywordFunnelField(99)); err == nil {
		t.Fatal("未知漏斗字段应报错")
	}
}
