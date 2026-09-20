// conversion_funnel_opportunity_test.go T-P4-06 的取数层：漏斗商机段读的是**哪张表、按哪一列切窗**。
//
// 这一层单独测的理由不是"多一层多一份覆盖"，是本卡有三条各自会静默失灵的错法：
// ① **读错表**：`conversion_funnels` 里真有一列叫 `stage`、真有一行 `stage='opportunity'`、
// 还真有一个 `count` 列。把这一段接成"读演示表那一行"，AC① 立刻通过（响应里商机段
// 有数了），而那个数是 `cmd/seed` 造的假数 —— R-4 收了口的双源会从这里长第二回，
// 且这一回带着"真实源"的名牌。所以有一条用例专门往演示表里种巨量假行来验真实源。
// ② **吞错回 0**：`Count` 报错时返回 `(0, nil)`，症状与"这个月真的没有商机"逐字节相同，
// 而且比它更常见（表没建、列漂移、连接断）。取数层一旦吞错，上面无论怎么判都判不出来。
// ③ **窗口只查一侧**：与 T-P2-03 同一课（那里的变异实测是"只查全年窗口时把时间窗整个删掉，
// 用例照样绿"），所以两侧都要读，差值才是"过滤真的在过滤"。
package repository

import (
	"context"
	"testing"
	"time"

	sysmodel "hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// 窗口沿用 §4.11 那批 2019 年的常量形状：远离"最近 30 天"的默认窗口，
// 不与任何按 now 取数的用例重叠。
var (
	oppMonthFrom = time.Date(2019, 3, 1, 0, 0, 0, 0, time.UTC)
	oppMonthTo   = time.Date(2019, 3, 31, 23, 59, 59, 0, time.UTC)
	oppYearFrom  = time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	oppYearTo    = time.Date(2019, 12, 31, 0, 0, 0, 0, time.UTC)
	oppIn1       = time.Date(2019, 3, 5, 10, 0, 0, 0, time.UTC)
	oppIn2       = time.Date(2019, 3, 20, 12, 0, 0, 0, time.UTC)
	oppIn3       = time.Date(2019, 3, 31, 23, 0, 0, 0, time.UTC)
	oppBefore    = time.Date(2019, 2, 1, 8, 0, 0, 0, time.UTC)
	oppAfter     = time.Date(2019, 4, 15, 8, 0, 0, 0, time.UTC)
)

func setupFunnelOppDB(t *testing.T, models ...any) *gorm.DB {
	t.Helper()
	database := testutil.NewTestDB(t, models...)
	db.SetTestDB(database)
	t.Cleanup(func() { db.SetTestDB(nil) })
	return database
}

// seedOpportunity 种一行商机。时间戳显式给出（同 internal/repository 那批用例的理由）：
// 依赖 autoCreateTime 会让"这条算不算在窗口里"变成插入时刻的函数。
func seedOpportunity(t *testing.T, database *gorm.DB, id string, createdAt time.Time) {
	t.Helper()
	row := &sysmodel.Opportunity{
		ID:         id,
		Code:       "CODE-" + id,
		CustomerID: "cust-" + id,
		Stage:      sysmodel.OpportunityStageQualification,
		Status:     sysmodel.OpportunityStatusOpen,
		CreatedAt:  createdAt,
		UpdatedAt:  createdAt,
	}
	if err := database.Create(row).Error; err != nil {
		t.Fatalf("写入商机 %s 失败：%v", id, err)
	}
}

// TestFunnelRepo_OpportunityCountBothWindows 两条窗口各读一次。
// 只读一条等于把"漏了过滤"和"过滤正确"混成同一个读数。
func TestFunnelRepo_OpportunityCountBothWindows(t *testing.T) {
	database := setupFunnelOppDB(t, &sysmodel.Opportunity{})
	for i, at := range []time.Time{oppIn1, oppIn2, oppIn3, oppBefore, oppAfter} {
		seedOpportunity(t, database, "opp-win-"+string(rune('a'+i)), at)
	}

	repo := NewConversionFunnelRepository()
	ctx := context.Background()
	month, err := repo.CountOpportunitiesByTimeRange(ctx, oppMonthFrom, oppMonthTo)
	if err != nil {
		t.Fatalf("当月窗口取数失败：%v", err)
	}
	year, err := repo.CountOpportunitiesByTimeRange(ctx, oppYearFrom, oppYearTo)
	if err != nil {
		t.Fatalf("全年窗口取数失败：%v", err)
	}
	if month != 3 {
		t.Errorf("当月窗口应为 3（3 月内三条），实际 %d", month)
	}
	if year != 5 {
		t.Errorf("全年窗口应为 5（含 2 月与 4 月各一条），实际 %d", year)
	}
	if diff := year - month; diff != 2 {
		t.Errorf("全年比当月多 %d 条，期望 2 ⇒ 时间窗没生效或多算了行", diff)
	}
}

// TestFunnelRepo_OpportunityCountIgnoresDemoTable 是 AC②/AC③ 的取数层落点：
// 演示表里那些 stage='opportunity' 的行**一个都不许算进来**。
// 这条用例的形状刻意做成"两边都有数"：只种演示表不种真表的话，
// 读到 0 也能通过（读对了是 0，读错了也是 0），那是个假绿。
func TestFunnelRepo_OpportunityCountIgnoresDemoTable(t *testing.T) {
	database := setupFunnelOppDB(t, &sysmodel.Opportunity{}, &sysmodel.ConversionFunnel{})
	seedOpportunity(t, database, "opp-real-1", oppIn1)
	seedOpportunity(t, database, "opp-real-2", oppIn2)

	// 演示表那套列里 stage/Count 都是现成的，seed 造的行也确实是 'opportunity'。
	for _, st := range []string{"opportunity", "deal", "visit"} {
		demo := &sysmodel.ConversionFunnel{
			StatDate: "2019-03-05", FunnelType: "sales", Stage: st, StageOrder: 5,
			Count: 99999,
		}
		if err := database.Create(demo).Error; err != nil {
			t.Fatalf("写入演示表行 %s 失败：%v", st, err)
		}
	}

	got, err := NewConversionFunnelRepository().
		CountOpportunitiesByTimeRange(context.Background(), oppMonthFrom, oppMonthTo)
	if err != nil {
		t.Fatalf("取数失败：%v", err)
	}
	if got != 2 {
		t.Errorf("商机段应为真表里的 2 条，实际 %d ⇒ %d 是演示表的假数（读错源，R-4 双源回潮）",
			got, got-2)
	}
}

// TestFunnelRepo_OpportunityCountPropagatesError 表不存在必须**报错**，不是回 0。
// 判据与 internal/repository/opportunity_test.go 那条"句柄没了 ⇒ 每个方法都要报错"同源：
// 取数层一吞错，服务层再怎么写都写不出"我不知道"这个形状。
func TestFunnelRepo_OpportunityCountPropagatesError(t *testing.T) {
	// 本包没有别的用例建 opportunities，但测试库是**进程级**的（同包共用，NewTestDB 只
	// DROP 自己 models 里列出的那几张）⇒ 先显式删掉，否则这条绿是撞上了文件名字母序。
	database := setupFunnelOppDB(t)
	if err := database.Exec(`DROP TABLE IF EXISTS opportunities`).Error; err != nil {
		t.Fatalf("清理 opportunities 失败：%v", err)
	}

	got, err := NewConversionFunnelRepository().
		CountOpportunitiesByTimeRange(context.Background(), oppMonthFrom, oppMonthTo)
	if err == nil {
		t.Errorf("表不存在却返回成功（count=%d）：错误必须原样上抛，由服务层决定怎么报", got)
	}
	if got != 0 {
		t.Errorf("取数失败却给出 %d：失败路径只能交零，交别的都会被当成一次真实读数", got)
	}
}
