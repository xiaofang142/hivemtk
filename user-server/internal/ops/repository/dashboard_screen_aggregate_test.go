// dashboard_screen_aggregate_test.go 大屏聚合只读查询的仓储契约。
//
// AV-04c 批次5 刀1：这组查询原先写在 ops/service/dashboard_screen.go（经
// sysrepo.GetDB 直查 clues/customers/"order"），下沉时把谓词、投影、排序、
// HasTable 守卫逐字钉死。边界一律由测试显式给定，不依赖测试运行的当日时刻。
package repository

import (
	"context"
	"testing"
	"time"

	sysmodel "hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func aggSetupDB(t *testing.T, models ...any) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t, models...)
}

func clueSeed(t *testing.T, db *gorm.DB, tag, sourceID, city string, isVerify int64, createdAt time.Time) *sysmodel.Clue {
	t.Helper()
	row := &sysmodel.Clue{
		SourceID:   sourceID,
		Account:    "acct-" + tag,
		Name:       "clue-" + tag,
		City:       city,
		IsVerify:   isVerify,
		CreateTime: createdAt.Unix(),
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("预置线索 %s 失败: %v", tag, err)
	}
	return row
}

func orderSeed(t *testing.T, db *gorm.DB, tag string, createdAt time.Time) *sysmodel.Order {
	t.Helper()
	row := &sysmodel.Order{
		Price:      "100",
		AccountID:  "acct-" + tag,
		CreateTime: createdAt.Unix(),
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("预置订单 %s 失败: %v", tag, err)
	}
	return row
}

// 计数族：总数/时间窗/已验证/客户与订单的 HasTable 存在路径。
func TestDashboardScreenRepository_AggregateCounts(t *testing.T) {
	db := aggSetupDB(t, &sysmodel.Clue{}, &sysmodel.Customer{}, &sysmodel.Order{})
	repo := NewDashboardScreenRepository()
	ctx := context.Background()

	base := time.Now()
	clueSeed(t, db, "t1", "a", "北京", 1, base.Add(-5*time.Hour))
	clueSeed(t, db, "t2", "", "上海", 0, base.Add(-6*time.Hour))
	clueSeed(t, db, "y1", "b", "广州", 1, base.Add(-30*time.Hour))
	clueSeed(t, db, "o1", "a", "", 0, base.Add(-40*24*time.Hour)) // 30 天窗外

	if n, err := repo.CountClues(ctx); err != nil || n != 4 {
		t.Errorf("CountClues = %d, %v; 期望 4, nil", n, err)
	}
	// 时间窗显式给定，与测试运行时刻无关。
	if n, err := repo.CountCluesCreatedSince(ctx, base.Add(-10*time.Hour)); err != nil || n != 2 {
		t.Errorf("CountCluesCreatedSince = %d, %v; 期望 2, nil", n, err)
	}
	if n, err := repo.CountCluesCreatedBetween(ctx, base.Add(-40*time.Hour), base.Add(-10*time.Hour)); err != nil || n != 1 {
		t.Errorf("CountCluesCreatedBetween = %d, %v; 期望 1, nil", n, err)
	}
	if n, err := repo.CountVerifiedClues(ctx); err != nil || n != 2 {
		t.Errorf("CountVerifiedClues = %d, %v; 期望 2, nil", n, err)
	}

	cust := &sysmodel.Customer{Email: "agg-test@example.com"}
	if err := db.Create(cust).Error; err != nil {
		t.Fatalf("预置客户失败: %v", err)
	}
	if n, err := repo.CountCustomersIfTableExists(ctx); err != nil || n != 1 {
		t.Errorf("CountCustomersIfTableExists = %d, %v; 期望 1, nil", n, err)
	}

	orderSeed(t, db, "w1", base.Add(-5*time.Hour))
	orderSeed(t, db, "w2", base.Add(-40*24*time.Hour))
	if n, err := repo.CountOrdersIfTableExists(ctx); err != nil || n != 2 {
		t.Errorf("CountOrdersIfTableExists = %d, %v; 期望 2, nil", n, err)
	}
	if n, err := repo.CountOrdersCreatedSinceIfTableExists(ctx, base.Add(-10*time.Hour)); err != nil || n != 1 {
		t.Errorf("CountOrdersCreatedSinceIfTableExists = %d, %v; 期望 1, nil", n, err)
	}
	if n, err := repo.CountOrdersCreatedBetweenIfTableExists(ctx, base.Add(-40*24*time.Hour), base.Add(-10*time.Hour)); err != nil || n != 1 {
		t.Errorf("CountOrdersCreatedBetweenIfTableExists = %d, %v; 期望 1, nil", n, err)
	}
}

// 趋势与分布：to_char 日期投影升序、渠道/地区分组（空串归「未知」）按计数降序。
func TestDashboardScreenRepository_TrendAndDistribution(t *testing.T) {
	db := aggSetupDB(t, &sysmodel.Clue{})
	repo := NewDashboardScreenRepository()
	ctx := context.Background()

	base := time.Now()
	day := func(offsetDays int, hour int) time.Time {
		return time.Date(base.Year(), base.Month(), base.Day(), hour, 0, 0, 0, base.Location()).AddDate(0, 0, -offsetDays)
	}
	d2 := day(2, 10)
	d5 := day(5, 10)
	clueSeed(t, db, "d2a", "a", "北京", 0, d2)
	clueSeed(t, db, "d2b", "a", "上海", 0, d2.Add(time.Hour))
	clueSeed(t, db, "d5a", "b", "北京", 0, d5)
	clueSeed(t, db, "d5b", "", "广州", 0, d5.Add(2*time.Hour))

	rows, err := repo.ClueTrendDaily(ctx, d5.Add(-time.Hour))
	if err != nil {
		t.Fatalf("ClueTrendDaily: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("趋势应含 2 个日期桶，实际 %d: %+v", len(rows), rows)
	}
	wantD2, wantD5 := d2.Format("01-02"), d5.Format("01-02")
	if rows[0].Day != wantD5 || rows[0].Cnt != 2 {
		t.Errorf("较早日期应在前：期望 %s=2，实际 %s=%d", wantD5, rows[0].Day, rows[0].Cnt)
	}
	if rows[1].Day != wantD2 || rows[1].Cnt != 2 {
		t.Errorf("期望 %s=2，实际 %s=%d", wantD2, rows[1].Day, rows[1].Cnt)
	}

	chs, err := repo.ClueChannelDistribution(ctx)
	if err != nil {
		t.Fatalf("ClueChannelDistribution: %v", err)
	}
	if len(chs) != 3 {
		t.Fatalf("渠道应分 3 组（a/b/未知），实际 %d: %+v", len(chs), chs)
	}
	if chs[0].Name != "a" || chs[0].Value != 2 {
		t.Errorf("计数最多组应在首：期望 a=2，实际 %s=%d", chs[0].Name, chs[0].Value)
	}
	foundUnknown := false
	for _, c := range chs {
		if c.Name == "未知" && c.Value == 1 {
			foundUnknown = true
		}
	}
	if !foundUnknown {
		t.Errorf("空 source_id 应归「未知」组: %+v", chs)
	}

	regs, err := repo.ClueRegionDistribution(ctx)
	if err != nil {
		t.Fatalf("ClueRegionDistribution: %v", err)
	}
	if len(regs) != 3 || regs[0].Name != "北京" || regs[0].Value != 2 {
		t.Errorf("地区分布期望 北京=2 在首，实际 %+v", regs)
	}
}

// HasTable 守卫缺表路径：customers / "order" 未建表时按 0 处理、不报错。
// 测试库按包共享且 ensureSchema 只建不删——前序用例（AggregateCounts）可能已把
// 两表建出来，这里显式落回「缺表」物理状态，用例结束后恢复建表。
func TestDashboardScreenRepository_HasTableGuards_WhenTablesAbsent(t *testing.T) {
	db := aggSetupDB(t, &sysmodel.Clue{}) // 本用例自身只需要 clues
	if err := db.Migrator().DropTable(&sysmodel.Customer{}); err != nil {
		t.Fatalf("drop customers 失败: %v", err)
	}
	if err := db.Migrator().DropTable(&sysmodel.Order{}); err != nil {
		t.Fatalf("drop order 失败: %v", err)
	}
	t.Cleanup(func() {
		_ = db.AutoMigrate(&sysmodel.Customer{}, &sysmodel.Order{})
	})
	repo := NewDashboardScreenRepository()
	ctx := context.Background()
	base := time.Now()

	if n, err := repo.CountCustomersIfTableExists(ctx); err != nil || n != 0 {
		t.Errorf("缺 customers 表应 (0, nil)，实际 (%d, %v)", n, err)
	}
	if n, err := repo.CountOrdersIfTableExists(ctx); err != nil || n != 0 {
		t.Errorf("缺 order 表应 (0, nil)，实际 (%d, %v)", n, err)
	}
	if n, err := repo.CountOrdersCreatedSinceIfTableExists(ctx, base.Add(-time.Hour)); err != nil || n != 0 {
		t.Errorf("缺表时窗口计数应 (0, nil)，实际 (%d, %v)", n, err)
	}
	if rows, err := repo.OrderTrendDaily(ctx, base.Add(-24*time.Hour)); err != nil || rows != nil {
		t.Errorf("缺表时成单趋势应 (nil, nil)，实际 (%v, %v)", rows, err)
	}
}
