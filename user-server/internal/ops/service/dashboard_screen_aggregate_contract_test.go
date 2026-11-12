// dashboard_screen_aggregate_service_test.go 大屏聚合服务层契约冻结。
//
// AV-04c 批次5 刀1：查询下沉 DashboardScreenRepository 后，在服务边界冻结
// 聚合结果（KPI/漏斗/趋势长度/渠道/转化）与实时活动流——isAdmin 两分支
// 原本同实现（恒等 scope 闭包），下沉后必须仍然同结果。
// 依赖测试运行当日时刻的边界桶（今日/昨日/本周）不在本层断言，
// 由仓储层测试用显式边界钉死（dashboard_screen_aggregate_test.go）。
package service

import (
	"context"
	"reflect"
	"testing"
	"time"

	sysmodel "hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func TestDashboardScreenService_AggregateDashboardData_Contract(t *testing.T) {
	database := testutil.NewTestDB(t,
		&sysmodel.Clue{},
		&sysmodel.Customer{},
		&sysmodel.Order{},
	)

	service := NewDashboardScreenService()
	ctx := context.Background()
	now := time.Now()

	seed := func(tag, sourceID, city string, isVerify int64, createdAt time.Time) {
		t.Helper()
		row := &sysmodel.Clue{
			SourceID:   sourceID,
			City:       city,
			IsVerify:   isVerify,
			Name:       "clue-" + tag,
			Account:    "acct-" + tag,
			CreateTime: createdAt.Unix(),
		}
		if err := database.Create(row).Error; err != nil {
			t.Fatalf("预置线索 %s 失败: %v", tag, err)
		}
	}
	seed("a1", "a", "北京", 1, now.Add(-1*time.Hour))
	seed("a2", "a", "上海", 0, now.Add(-2*time.Hour))
	seed("a3", "", "北京", 0, now.Add(-26*time.Hour))

	if err := database.Create(&sysmodel.Customer{Email: "agg-svc@example.com"}).Error; err != nil {
		t.Fatalf("预置客户失败: %v", err)
	}
	if err := database.Create(&sysmodel.Order{Price: "100", AccountID: "acct-order", CreateTime: now.Add(-1 * time.Hour).Unix()}).Error; err != nil {
		t.Fatalf("预置订单失败: %v", err)
	}

	admin, err := service.AggregateDashboardData(ctx, true)
	if err != nil {
		t.Fatalf("AggregateDashboardData(admin): %v", err)
	}
	agent, err := service.AggregateDashboardData(ctx, false)
	if err != nil {
		t.Fatalf("AggregateDashboardData(agent): %v", err)
	}
	if !reflect.DeepEqual(admin.Kpis, agent.Kpis) || !reflect.DeepEqual(admin.Funnel, agent.Funnel) ||
		!reflect.DeepEqual(admin.Channels, agent.Channels) || !reflect.DeepEqual(admin.Regions, agent.Regions) ||
		!reflect.DeepEqual(admin.Conversion, agent.Conversion) {
		t.Errorf("isAdmin 两分支应同结果（原恒等 scope 语义），实际不一致")
	}

	agg := admin
	if agg.Funnel[0].Value != 3 || agg.Funnel[1].Value != 1 || agg.Funnel[2].Value != 1 {
		t.Errorf("漏斗应为 线索3/客户1/成单1，实际 %+v", agg.Funnel)
	}
	if agg.Kpis[0].Value != int64(3) {
		t.Errorf("总线索 KPI 应为 3，实际 %v", agg.Kpis[0].Value)
	}
	if agg.Kpis[4].Value != int64(1) {
		t.Errorf("已验证线索 KPI 应为 1，实际 %v", agg.Kpis[4].Value)
	}
	if v := agg.Kpis[5].Value; v != int64(33) { // pct(1,3) = 33
		t.Errorf("验证率 KPI 应为 33，实际 %v", v)
	}

	// 30 天趋势：日期长度 30。原实现（保真保留）的日期数组是
	// [thirtyDaysAgo .. thirtyDaysAgo+29d=昨天]，**不含今天**——今日线索不进趋势，
	// 逐日求和只应命中「本地日期已非今天」的种子。
	// 期望值不写死 1：种子用 now-1h/-2h/-26h 相对时间，00:00-02:00 之间运行时
	// 「今日种子」会跨过午夜落到昨天（2026-11-13 01:14 实测 sum=2：now-2h=昨 23:14），
	// 按实现同款「本地日期 < 今天」边界从种子推导期望，契约本身（30 天长、
	// 今日不计入、昨日计入）不变。
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	wantTrendSum := int64(0)
	for _, at := range []time.Time{now.Add(-1 * time.Hour), now.Add(-2 * time.Hour), now.Add(-26 * time.Hour)} {
		if at.Before(todayStart) {
			wantTrendSum++
		}
	}
	if len(agg.Trend.Dates) != 30 || len(agg.Trend.Clues) != 30 {
		t.Fatalf("趋势应 30 天，实际 dates=%d clues=%d", len(agg.Trend.Dates), len(agg.Trend.Clues))
	}
	sum := int64(0)
	for _, v := range agg.Trend.Clues {
		sum += v
	}
	if sum != wantTrendSum {
		t.Errorf("趋势逐日求和应为 %d（日期早于今天的种子数；契约=窗口止于昨天、今日不计入），实际 %d", wantTrendSum, sum)
	}

	// 渠道/地区：空 source_id 归「未知」，计数降序。
	if len(agg.Channels) != 2 || agg.Channels[0].Name != "a" || agg.Channels[0].Value != 2 {
		t.Errorf("渠道分布期望 a=2 在首（共 2 组），实际 %+v", agg.Channels)
	}
	if len(agg.Sources) != 2 {
		t.Errorf("Sources 为渠道前 5 截断（此处共 2 组），实际 %d", len(agg.Sources))
	}
	if len(agg.Regions) != 2 || agg.Regions[0].Name != "北京" || agg.Regions[0].Value != 2 {
		t.Errorf("地区分布期望 北京=2 在首，实际 %+v", agg.Regions)
	}

	// 双周对比窗口依赖运行时刻（周起点随星期漂移），本层只冻结形状；
	// 窗口计数的数值语义由仓储层测试用显式边界钉死。
	if len(agg.Conversion.Dates) != 2 || len(agg.Conversion.ThisWeek) != 1 || len(agg.Conversion.LastWeek) != 1 {
		t.Errorf("转化对比形状应为 2 日期 × 双单值窗口，实际 %+v", agg.Conversion)
	}
}

// 实时活动流：create_time 倒序、limit 夹取、非法 limit 回落 20。
func TestDashboardScreenService_FetchRealtimeActivities(t *testing.T) {
	database := testutil.NewTestDB(t, &sysmodel.Clue{})
	service := NewDashboardScreenService()
	ctx := context.Background()
	now := time.Now()

	mk := func(tag string, offset time.Duration) {
		t.Helper()
		row := &sysmodel.Clue{
			Name:       "clue-" + tag,
			Account:    "user-" + tag,
			SourceID:   "src",
			CreateTime: now.Add(offset).Unix(),
		}
		if err := database.Create(row).Error; err != nil {
			t.Fatalf("预置线索 %s 失败: %v", tag, err)
		}
	}
	mk("old", -5*time.Hour)
	mk("new", -1*time.Hour)
	mk("mid", -3*time.Hour)

	acts, err := service.FetchRealtimeActivities(ctx, 2)
	if err != nil {
		t.Fatalf("FetchRealtimeActivities(2): %v", err)
	}
	if len(acts) != 2 {
		t.Fatalf("limit=2 应返回 2 条，实际 %d", len(acts))
	}
	if acts[0].UserName != "user-new" || acts[1].UserName != "user-mid" {
		t.Errorf("应按 create_time 倒序（new→mid），实际 %s→%s", acts[0].UserName, acts[1].UserName)
	}
	if acts[0].Type != "clue" || acts[0].Title != "新线索：clue-new" {
		t.Errorf("活动项投影失真: %+v", acts[0])
	}
	if !acts[0].CreatedAt.Equal(time.Unix(now.Add(-1*time.Hour).Unix(), 0)) {
		t.Errorf("CreatedAt 应为 create_time 的 Unix 还原: %v", acts[0].CreatedAt)
	}

	all, err := service.FetchRealtimeActivities(ctx, 0)
	if err != nil {
		t.Fatalf("FetchRealtimeActivities(0): %v", err)
	}
	if len(all) != 3 {
		t.Errorf("非法 limit 回落 20：应返回全部 3 条，实际 %d", len(all))
	}
}
