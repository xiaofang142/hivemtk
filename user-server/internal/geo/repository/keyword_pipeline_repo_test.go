// keyword_pipeline_repo_test.go 关键词管线持久化组的仓储层 SQL 契约（PG 定向，8217）。
//
// AV-04c 批次3 自 geo/service/keyword_pipeline.go 下沉的两组查询在这里锁定 SQL 语义：
//   - UpdateClusterByKeywords：只回填空簇（” 或 NULL）行、白名单单列、RowsAffected 计数；
//   - ListPendingSEOKeywords：LEFT JOIN 无文章谓词、层/状态过滤、NULLS LAST 轮转排序。
//
// 业务编排（批次大小、行自证 OnlyTenant）在 service 层 keyword_pipeline.go。
package repository

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func pipelineRepoSetupDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testutil.NewTestDB(t, &model.GeoKeyword{}, &model.GeoArticle{})
	if db == nil {
		t.Fatal("测试库不可达：关键词管线仓储 SQL 契约判不了（不 Skip，跳过等于没锁）")
	}
	return db
}

func TestGeoKeywordRepo_UpdateClusterByKeywordsEmptyClusterOnly(t *testing.T) {
	db := pipelineRepoSetupDB(t)
	repo := NewGeoKeywordRepositoryWithDB(db)
	ctx := context.Background()

	empty := &model.GeoKeyword{Keyword: "空簇词", Layer: "seed", Status: "active"}
	if err := repo.Create(ctx, empty); err != nil {
		t.Fatalf("创建空簇行失败: %v", err)
	}
	if err := db.WithContext(ctx).Exec(
		`UPDATE geo_keywords SET cluster = NULL WHERE id = ?`, empty.ID).Error; err != nil {
		t.Fatalf("置 NULL cluster 失败: %v", err)
	}
	inListNull := &model.GeoKeyword{Keyword: "NULL簇词", Layer: "seed", Status: "active"}
	if err := repo.Create(ctx, inListNull); err != nil {
		t.Fatalf("创建 NULL 簇行失败: %v", err)
	}
	if err := db.WithContext(ctx).Exec(
		`UPDATE geo_keywords SET cluster = NULL WHERE id = ?`, inListNull.ID).Error; err != nil {
		t.Fatalf("置 NULL cluster 失败: %v", err)
	}
	occupied := &model.GeoKeyword{Keyword: "已有线词", Cluster: "旧业务线", Layer: "seed", Status: "active"}
	if err := repo.Create(ctx, occupied); err != nil {
		t.Fatalf("创建非空簇行失败: %v", err)
	}
	if err := repo.Create(ctx, &model.GeoKeyword{Keyword: "不在名单词", Layer: "seed", Status: "active"}); err != nil {
		t.Fatalf("创建名单外行失败: %v", err)
	}

	// 同词重复入参不去重：IN (?) 原样展开，命中行只更新一次。
	affected, err := repo.UpdateClusterByKeywords(ctx, []string{"空簇词", "空簇词", "NULL簇词", "已有线词"}, "业务线1")
	if err != nil {
		t.Fatalf("UpdateClusterByKeywords 失败: %v", err)
	}
	if affected != 2 {
		t.Fatalf("应只回填空簇（''/NULL）两行，RowsAffected=%d", affected)
	}

	// 回查用独立变量：gorm 会把 dest 已有主键并进 First 的 WHERE（复用同一个
	// struct 会让下一条 First 带上上一行的 id），不是仓储行为问题。
	var gotEmpty, gotNull, gotOccupied, gotOutside model.GeoKeyword
	if err := db.WithContext(ctx).Where("keyword = ?", "空簇词").First(&gotEmpty).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if gotEmpty.Cluster != "业务线1" {
		t.Fatalf("'' 簇应回填，实际 %q", gotEmpty.Cluster)
	}
	if err := db.WithContext(ctx).Where("keyword = ?", "NULL簇词").First(&gotNull).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if gotNull.Cluster != "业务线1" {
		t.Fatalf("NULL 簇应回填，实际 %q", gotNull.Cluster)
	}
	if err := db.WithContext(ctx).Where("keyword = ?", "已有线词").First(&gotOccupied).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if gotOccupied.Cluster != "旧业务线" {
		t.Fatalf("非空簇不许被覆盖，实际 %q", gotOccupied.Cluster)
	}
	if err := db.WithContext(ctx).Where("keyword = ?", "不在名单词").First(&gotOutside).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if gotOutside.Cluster != "" {
		t.Fatalf("名单外行不许被改，实际 %q", gotOutside.Cluster)
	}

	// 空名单：IN 空集命中 0 行（不报错），与改前 gorm 展开行为一致。
	affected, err = repo.UpdateClusterByKeywords(ctx, []string{}, "业务线2")
	if err != nil || affected != 0 {
		t.Fatalf("空名单应命中 0 行且无错：affected=%d err=%v", affected, err)
	}
}

func TestGeoKeywordRepo_UpdateClusterByKeywordsTenantScoped(t *testing.T) {
	db := pipelineRepoSetupDB(t)
	repo := NewGeoKeywordRepositoryWithDB(db)
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() {
		tenant.ResetRegistryForTest()
		tenant.SetMode(tenant.ModeOff)
	})
	const (
		ta = uint(341)
		tb = uint(342)
	)
	ctxA := tenant.WithTenantID(context.Background(), ta)
	ctxB := tenant.WithTenantID(context.Background(), tb)

	if err := repo.Create(ctxA, &model.GeoKeyword{Keyword: "租户词A", Layer: "seed", Status: "active"}); err != nil {
		t.Fatalf("租户 A 建词失败: %v", err)
	}
	if err := repo.Create(ctxB, &model.GeoKeyword{Keyword: "租户词A", Layer: "seed", Status: "active"}); err != nil {
		t.Fatalf("租户 B 建词失败: %v", err)
	}

	affected, err := repo.UpdateClusterByKeywords(ctxA, []string{"租户词A"}, "A线")
	if err != nil {
		t.Fatalf("租户 A 回填失败: %v", err)
	}
	if affected != 1 {
		t.Fatalf("Table 形态更新应被隔离 callback 钉在本租户，RowsAffected=%d", affected)
	}
	var bRow model.GeoKeyword
	if err := db.WithContext(tenant.WithSystemScope(context.Background())).
		Where("keyword = ? AND tenant_id = ?", "租户词A", tb).First(&bRow).Error; err != nil {
		t.Fatalf("回查租户 B 行失败: %v", err)
	}
	if bRow.Cluster != "" {
		t.Fatalf("租户 B 同名行不许被 A 的回填污染，实际 %q", bRow.Cluster)
	}
}

func TestGeoKeywordRepo_ListPendingSEOKeywordsRotationAndJoin(t *testing.T) {
	db := pipelineRepoSetupDB(t)
	repo := NewGeoKeywordRepositoryWithDB(db)
	ctx := context.Background()

	mk := func(keyword, layer, status string, minedAt *time.Time) *model.GeoKeyword {
		return &model.GeoKeyword{Keyword: keyword, Layer: layer, Status: status, LastMinedAt: minedAt}
	}
	tOld := time.Now().Add(-48 * time.Hour)
	tNew := time.Now().Add(-24 * time.Hour)
	rows := []*model.GeoKeyword{
		mk("待SEO晚", "longtail", "active", &tNew), // last_mined_at 较晚 → 第二
		mk("待SEO早", "longtail", "active", &tOld), // 较早 → 第一
		mk("待SEO空", "suggest", "active", nil),    // NULL → NULLS LAST → 第三
		mk("已发文", "longtail", "active", &tOld),   // 有文章 → a.id IS NULL 排除
		mk("种子层", "seed", "active", &tOld),       // layer 不在面内 → 排除
		mk("停用词", "longtail", "paused", &tOld),   // status 过滤 → 排除
	}
	for _, r := range rows {
		if err := repo.Create(ctx, r); err != nil {
			t.Fatalf("创建 %s 失败: %v", r.Keyword, err)
		}
	}
	if err := db.WithContext(ctx).Create(&model.GeoArticle{
		Title: "已发文的文章", Keyword: "已发文", Content: "正文", Status: "published",
	}).Error; err != nil {
		t.Fatalf("创建文章失败: %v", err)
	}

	pending, err := repo.ListPendingSEOKeywords(ctx, 10)
	if err != nil {
		t.Fatalf("ListPendingSEOKeywords 失败: %v", err)
	}
	got := make([]string, 0, len(pending))
	for _, p := range pending {
		got = append(got, p.Keyword)
	}
	want := []string{"待SEO早", "待SEO晚", "待SEO空"}
	if len(got) != len(want) {
		t.Fatalf("待 SEO 名单应恰为 %v，实际 %v（有文章/种子层/停用词必须排除）", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("轮转序应 last_mined_at ASC NULLS LAST：第 %d 位 got=%q want=%q", i, got[i], want[i])
		}
	}

	// limit 截断：批次上限生效（调用方保证 >0）。
	capped, err := repo.ListPendingSEOKeywords(ctx, 2)
	if err != nil {
		t.Fatalf("带 limit 查询失败: %v", err)
	}
	if len(capped) != 2 || capped[0].Keyword != "待SEO早" || capped[1].Keyword != "待SEO晚" {
		t.Fatalf("limit=2 应取轮转最前两行，实际 %+v", capped)
	}
}
