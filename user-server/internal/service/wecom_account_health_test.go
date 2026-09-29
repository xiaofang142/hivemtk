package service

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/model"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupWeComHealthTestDB(t *testing.T) *gorm.DB {
	return testutil.NewTestDB(t,
		&model.WeComAccount{},
		&model.WeComAccountHealth{},
		&model.MessageHub{},
		&model.InboxConversation{},
		&model.InboxAssignment{},
	)
}

func newWeComHealthService(t *testing.T) (*WeComAccountHealthService, *gorm.DB) {
	db := setupWeComHealthTestDB(t)
	return NewWeComAccountHealthService(db), db
}

func mkWeComAccount(id uint) *model.WeComAccount {
	now := time.Now()
	return &model.WeComAccount{
		ID: id,

		CorpID:        fmt.Sprintf("corp-%d", id),
		CorpSecret:    "secret",
		AgentID:       1000000 + int(id),
		Status:        1,
		LoginState:    WeComLoginOnline,
		DailyMsgQuota: 500,
		DailyMsgUsed:  0,
		Weight:        100,
		RiskLevel:     WeComRiskNormal,
		LastActiveAt:  &now,
	}
}

// 1. 基本正常上报
func TestReportHealth_Normal(t *testing.T) {
	svc, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	rec, err := svc.ReportHealth(context.Background(), &ReportHealthRequest{

		AccountID:   1,
		LoginState:  WeComLoginOnline,
		QuotaUsed:   100,
		QuotaTotal:  500,
		SuccessRate: 98,
		ErrorCount:  1,
	})
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if rec.HealthScore < 90 {
		t.Errorf("expected high score, got %d", rec.HealthScore)
	}
	if rec.RiskLevel != WeComRiskNormal {
		t.Errorf("expected normal, got %s", rec.RiskLevel)
	}
	if rec.QuotaUsageRate < 19 || rec.QuotaUsageRate > 21 {
		t.Errorf("expected ~20%%, got %f", rec.QuotaUsageRate)
	}
}

// 2. 高配额使用率
func TestReportHealth_HighQuotaUsage(t *testing.T) {
	svc, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	rec, _ := svc.ReportHealth(context.Background(), &ReportHealthRequest{
		AccountID: 1,
		QuotaUsed: 460, QuotaTotal: 500, SuccessRate: 99, ErrorCount: 0,
	})
	if rec.HealthScore > 90 {
		t.Errorf("expected lower score, got %d", rec.HealthScore)
	}
}

// 3. 极低成功率
func TestReportHealth_LowSuccessRate(t *testing.T) {
	svc, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	rec, _ := svc.ReportHealth(context.Background(), &ReportHealthRequest{
		AccountID: 1,
		QuotaUsed: 10, QuotaTotal: 500, SuccessRate: 30, ErrorCount: 0,
	})
	if rec.HealthScore > 70 {
		t.Errorf("expected much lower, got %d", rec.HealthScore)
	}
}

// 4. 封禁状态
func TestReportHealth_Banned(t *testing.T) {
	svc, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	rec, _ := svc.ReportHealth(context.Background(), &ReportHealthRequest{
		AccountID:  1,
		LoginState: WeComLoginBanned,
		QuotaUsed:  0, QuotaTotal: 500, SuccessRate: 0, ErrorCount: 0,
	})
	if rec.HealthScore != 0 {
		t.Errorf("expected 0, got %d", rec.HealthScore)
	}
	if rec.RiskLevel != WeComRiskBanned {
		t.Errorf("expected banned, got %s", rec.RiskLevel)
	}
}

// 5. 离线状态
func TestReportHealth_Offline(t *testing.T) {
	svc, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	rec, _ := svc.ReportHealth(context.Background(), &ReportHealthRequest{
		AccountID:  1,
		LoginState: WeComLoginOffline,
		QuotaUsed:  0, QuotaTotal: 500, SuccessRate: 100, ErrorCount: 0,
	})
	if rec.HealthScore > 75 {
		t.Errorf("expected lower for offline, got %d", rec.HealthScore)
	}
}

// 6. 多错误数
func TestReportHealth_ManyErrors(t *testing.T) {
	svc, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	rec, _ := svc.ReportHealth(context.Background(), &ReportHealthRequest{
		AccountID: 1,
		QuotaUsed: 0, QuotaTotal: 500, SuccessRate: 99, ErrorCount: 100,
	})
	if rec.HealthScore > 70 {
		t.Errorf("expected much lower, got %d", rec.HealthScore)
	}
}

// 8. 空 account
func TestReportHealth_EmptyAccount(t *testing.T) {
	svc, _ := newWeComHealthService(t)
	_, err := svc.ReportHealth(context.Background(), &ReportHealthRequest{})
	if err == nil {
		t.Error("expected error for empty account")
	}
}

// 9. 默认 platform
func TestReportHealth_DefaultPlatform(t *testing.T) {
	svc, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	rec, _ := svc.ReportHealth(context.Background(), &ReportHealthRequest{
		AccountID: 1,
		QuotaUsed: 0, QuotaTotal: 500, SuccessRate: 100, ErrorCount: 0,
	})
	if rec.Platform != "wecom" {
		t.Errorf("expected wecom, got %s", rec.Platform)
	}
}

// 10. 自定义 metrics
func TestReportHealth_CustomMetrics(t *testing.T) {
	svc, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	metrics := map[string]any{"cpu": 50.0, "memory": 60.0}
	rec, _ := svc.ReportHealth(context.Background(), &ReportHealthRequest{
		AccountID: 1,
		QuotaUsed: 0, QuotaTotal: 500, SuccessRate: 100, ErrorCount: 0,
		Metrics: metrics,
	})
	if len(rec.Metrics) == 0 {
		t.Error("expected metrics saved")
	}
}

// 11. 拉取最新健康度
func TestGetLatestHealth_Success(t *testing.T) {
	svc, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	svc.ReportHealth(context.Background(), &ReportHealthRequest{
		AccountID:  1,
		LoginState: WeComLoginOnline,
		QuotaUsed:  10, QuotaTotal: 100, SuccessRate: 99, ErrorCount: 1,
	})
	time.Sleep(10 * time.Millisecond)
	svc.ReportHealth(context.Background(), &ReportHealthRequest{
		AccountID:  1,
		LoginState: WeComLoginBanned,
		QuotaUsed:  100, QuotaTotal: 100, SuccessRate: 0, ErrorCount: 100,
	})
	latest, err := svc.GetLatestHealth(context.Background(), 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if latest.HealthScore != 0 {
		t.Errorf("expected latest banned=0, got %d", latest.HealthScore)
	}
}

// 12. 不存在
func TestGetLatestHealth_NotFound(t *testing.T) {
	svc, _ := newWeComHealthService(t)
	_, err := svc.GetLatestHealth(context.Background(), 999)
	if err != ErrWeComHealthNotFound {
		t.Errorf("expected ErrWeComHealthNotFound, got %v", err)
	}
}

// 13. 历史列表
func TestListHealthHistory_Success(t *testing.T) {
	svc, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	for i := 0; i < 5; i++ {
		svc.ReportHealth(context.Background(), &ReportHealthRequest{
			AccountID: 1,
			QuotaUsed: i * 10, QuotaTotal: 100, SuccessRate: 100, ErrorCount: 0,
		})
	}
	list, total, err := svc.ListHealthHistory(context.Background(), 1, 1, 3)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 5 {
		t.Errorf("expected 5, got %d", total)
	}
	if len(list) != 3 {
		t.Errorf("expected 3 items, got %d", len(list))
	}
}

// 14. 分页
func TestListHealthHistory_Page2(t *testing.T) {
	svc, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	for i := 0; i < 7; i++ {
		svc.ReportHealth(context.Background(), &ReportHealthRequest{
			AccountID: 1,
			QuotaUsed: 0, QuotaTotal: 100, SuccessRate: 100, ErrorCount: 0,
		})
	}
	_, total, _ := svc.ListHealthHistory(context.Background(), 1, 2, 3)
	if total != 7 {
		t.Errorf("expected 7 total, got %d", total)
	}
}

// 15. 空列表
func TestListHealthHistory_Empty(t *testing.T) {
	svc, _ := newWeComHealthService(t)
	list, total, _ := svc.ListHealthHistory(context.Background(), 999, 1, 10)
	if total != 0 || len(list) != 0 {
		t.Errorf("expected empty, got list=%d total=%d", len(list), total)
	}
}

// 16. 风险账号列表
func TestGetRiskAccounts_WithRisks(t *testing.T) {
	svc, db := newWeComHealthService(t)
	a1 := mkWeComAccount(1)
	a1.RiskLevel = WeComRiskWarning
	db.Create(a1)
	db.Create(mkWeComAccount(2))
	a3 := mkWeComAccount(3)
	a3.RiskLevel = WeComRiskCritical
	db.Create(a3)

	risks, err := svc.GetRiskAccounts(context.Background())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(risks) != 2 {
		t.Errorf("expected 2 risk accounts, got %d", len(risks))
	}
}

// 17. 没有风险账号
func TestGetRiskAccounts_AllNormal(t *testing.T) {
	svc, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	db.Create(mkWeComAccount(2))
	risks, _ := svc.GetRiskAccounts(context.Background())
	if len(risks) != 0 {
		t.Errorf("expected 0, got %d", len(risks))
	}
}

// 18. 选最佳账号 - 配额最低优先
func TestSelectHealthyAccount_QuotaFirst(t *testing.T) {
	svc, db := newWeComHealthService(t)
	a1 := mkWeComAccount(1)
	a1.DailyMsgUsed = 100
	db.Create(a1)
	a2 := mkWeComAccount(2)
	a2.DailyMsgUsed = 50
	db.Create(a2)
	a3 := mkWeComAccount(3)
	a3.DailyMsgUsed = 200
	db.Create(a3)
	best, err := svc.SelectHealthyAccount(context.Background())
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if best.ID != 2 {
		t.Errorf("expected account 2 (lowest quota used), got %d", best.ID)
	}
}

// 19. 跳过封禁账号
func TestSelectHealthyAccount_SkipBanned(t *testing.T) {
	svc, db := newWeComHealthService(t)
	a1 := mkWeComAccount(1)
	a1.RiskLevel = WeComRiskBanned
	db.Create(a1)
	a2 := mkWeComAccount(2)
	db.Create(a2)
	best, _ := svc.SelectHealthyAccount(context.Background())
	if best.ID != 2 {
		t.Errorf("expected account 2, got %d", best.ID)
	}
}

// 20. 没有可用账号
func TestSelectHealthyAccount_NoneAvailable(t *testing.T) {
	svc, db := newWeComHealthService(t)
	a1 := mkWeComAccount(1)
	a1.LoginState = WeComLoginBanned
	db.Create(a1)
	_, err := svc.SelectHealthyAccount(context.Background())
	if err == nil {
		t.Error("expected error")
	}
}

// 21. 跳过禁用账号
func TestSelectHealthyAccount_SkipDisabled(t *testing.T) {
	svc, db := newWeComHealthService(t)
	a1 := mkWeComAccount(1)
	a1.LoginState = WeComLoginBanned
	db.Create(a1)
	a2 := mkWeComAccount(2)
	db.Create(a2)
	best, _ := svc.SelectHealthyAccount(context.Background())
	if best.ID != 2 {
		t.Errorf("expected 2, got %d", best.ID)
	}
}

// 22. 按权重排序
func TestSelectHealthyAccount_ByWeight(t *testing.T) {
	svc, db := newWeComHealthService(t)
	a1 := mkWeComAccount(1)
	a1.Weight = 50
	a1.DailyMsgUsed = 0
	db.Create(a1)
	a2 := mkWeComAccount(2)
	a2.Weight = 100
	a2.DailyMsgUsed = 0
	db.Create(a2)
	best, _ := svc.SelectHealthyAccount(context.Background())
	if best.ID != 2 {
		t.Errorf("expected 2 (weight 100), got %d", best.ID)
	}
}

// 23. 配额消耗
func TestConsumeQuota_Success(t *testing.T) {
	svc, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	if err := svc.ConsumeQuota(context.Background(), 1, 10); err != nil {
		t.Fatalf("consume: %v", err)
	}
	var a model.WeComAccount
	db.First(&a, 1)
	if a.DailyMsgUsed != 10 {
		t.Errorf("expected 10, got %d", a.DailyMsgUsed)
	}
	if a.TotalSent != 10 {
		t.Errorf("expected total 10, got %d", a.TotalSent)
	}
}

// 24. 超过配额
func TestConsumeQuota_Exceeded(t *testing.T) {
	svc, db := newWeComHealthService(t)
	a1 := mkWeComAccount(1)
	a1.DailyMsgUsed = 490
	a1.DailyMsgQuota = 500
	db.Create(a1)
	err := svc.ConsumeQuota(context.Background(), 1, 20)
	if err != ErrWeComQuotaExceeded {
		t.Errorf("expected quota exceeded, got %v", err)
	}
}

// 25. 封禁账号
func TestConsumeQuota_Banned(t *testing.T) {
	svc, db := newWeComHealthService(t)
	a1 := mkWeComAccount(1)
	a1.LoginState = WeComLoginBanned
	db.Create(a1)
	err := svc.ConsumeQuota(context.Background(), 1, 10)
	if err != ErrWeComAccountBanned {
		t.Errorf("expected banned, got %v", err)
	}
}

// 26. 禁用账号
func TestConsumeQuota_Disabled(t *testing.T) {
	svc, db := newWeComHealthService(t)
	a1 := mkWeComAccount(1)
	db.Create(a1)
	db.Model(&model.WeComAccount{}).Where("id = ?", 1).Update("status", 0)
	err := svc.ConsumeQuota(context.Background(), 1, 10)
	if err != ErrWeComAccountBanned {
		t.Errorf("expected banned, got %v", err)
	}
}

// 27. 多次消耗
func TestConsumeQuota_Multiple(t *testing.T) {
	svc, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	svc.ConsumeQuota(context.Background(), 1, 5)
	svc.ConsumeQuota(context.Background(), 1, 10)
	svc.ConsumeQuota(context.Background(), 1, 15)
	var a model.WeComAccount
	db.First(&a, 1)
	if a.DailyMsgUsed != 30 {
		t.Errorf("expected 30, got %d", a.DailyMsgUsed)
	}
}

// 28. 零消耗
func TestConsumeQuota_Zero(t *testing.T) {
	svc, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	if err := svc.ConsumeQuota(context.Background(), 1, 0); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

// 29. 负数消耗
func TestConsumeQuota_Negative(t *testing.T) {
	svc, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	if err := svc.ConsumeQuota(context.Background(), 1, -5); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

// 30. 账号不存在
func TestConsumeQuota_NotFound(t *testing.T) {
	svc, _ := newWeComHealthService(t)
	err := svc.ConsumeQuota(context.Background(), 999, 10)
	if err == nil {
		t.Error("expected error for missing account")
	}
}

// 31. 重置日配额
func TestResetDailyQuota_Success(t *testing.T) {
	svc, db := newWeComHealthService(t)
	a1 := mkWeComAccount(1)
	a1.DailyMsgUsed = 100
	db.Create(a1)
	a2 := mkWeComAccount(2)
	a2.DailyMsgUsed = 200
	db.Create(a2)
	n, _ := svc.ResetDailyQuota(context.Background())
	if n != 2 {
		t.Errorf("expected 2, got %d", n)
	}
	var a model.WeComAccount
	db.First(&a, 1)
	if a.DailyMsgUsed != 0 {
		t.Errorf("expected 0, got %d", a.DailyMsgUsed)
	}
}

// 32. 重置空库
func TestResetDailyQuota_Empty(t *testing.T) {
	svc, _ := newWeComHealthService(t)
	n, _ := svc.ResetDailyQuota(context.Background())
	if n != 0 {
		t.Errorf("expected 0, got %d", n)
	}
}

// 33. 重置设置 QuotaResetAt
func TestResetDailyQuota_SetsTime(t *testing.T) {
	svc, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	svc.ResetDailyQuota(context.Background())
	var a model.WeComAccount
	db.First(&a, 1)
	if a.QuotaResetAt == nil {
		t.Error("expected quota_reset_at to be set")
	}
}

// 34. 健康概览
func TestGetHealthSummary_Basic(t *testing.T) {
	svc, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	a2 := mkWeComAccount(2)
	a2.LoginState = WeComLoginOffline
	db.Create(a2)
	a3 := mkWeComAccount(3)
	a3.LoginState = WeComLoginBanned
	db.Create(a3)
	summary, err := svc.GetHealthSummary(context.Background())
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary.TotalAccounts != 3 {
		t.Errorf("expected 3, got %d", summary.TotalAccounts)
	}
	if summary.OnlineCount != 1 {
		t.Errorf("expected 1 online, got %d", summary.OnlineCount)
	}
	if summary.OfflineCount != 1 {
		t.Errorf("expected 1 offline, got %d", summary.OfflineCount)
	}
	if summary.BannedCount != 1 {
		t.Errorf("expected 1 banned, got %d", summary.BannedCount)
	}
	if summary.TotalQuota != 1500 {
		t.Errorf("expected 1500 total, got %d", summary.TotalQuota)
	}
}

// 35. 空账号概览
func TestGetHealthSummary_Empty(t *testing.T) {
	svc, _ := newWeComHealthService(t)
	summary, _ := svc.GetHealthSummary(context.Background())
	if summary.TotalAccounts != 0 {
		t.Errorf("expected 0, got %d", summary.TotalAccounts)
	}
	if summary.AvgScore != 0 {
		t.Errorf("expected 0 avg, got %f", summary.AvgScore)
	}
}

// 36. 风险账号包含
func TestGetHealthSummary_IncludesRisk(t *testing.T) {
	svc, db := newWeComHealthService(t)
	a1 := mkWeComAccount(1)
	a1.RiskLevel = WeComRiskCritical
	db.Create(a1)
	db.Create(mkWeComAccount(2))
	summary, _ := svc.GetHealthSummary(context.Background())
	if summary.CriticalCount != 1 {
		t.Errorf("expected 1 critical, got %d", summary.CriticalCount)
	}
	if len(summary.RiskAccounts) != 1 {
		t.Errorf("expected 1 risk account, got %d", len(summary.RiskAccounts))
	}
}

// 37. 包含账户详情
func TestGetHealthSummary_AccountDetails(t *testing.T) {
	svc, db := newWeComHealthService(t)
	a1 := mkWeComAccount(1)
	a1.DailyMsgUsed = 250
	a1.DailyMsgQuota = 500
	db.Create(a1)
	summary, _ := svc.GetHealthSummary(context.Background())
	if len(summary.Accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(summary.Accounts))
	}
	if summary.Accounts[0].QuotaUsageRate != 0.5 {
		t.Errorf("expected 0.5, got %f", summary.Accounts[0].QuotaUsageRate)
	}
}

// 38. 健康分 - 正常
func TestComputeHealthScore_Normal(t *testing.T) {
	score := computeHealthScore(WeComLoginOnline, 0.1, 99, 1)
	if score != 100 {
		t.Errorf("expected 100, got %d", score)
	}
}

// 39. 健康分 - 封禁
func TestComputeHealthScore_Banned(t *testing.T) {
	score := computeHealthScore(WeComLoginBanned, 0, 100, 0)
	if score != 0 {
		t.Errorf("expected 0, got %d", score)
	}
}

// 40. 健康分 - 离线
func TestComputeHealthScore_Offline(t *testing.T) {
	score := computeHealthScore(WeComLoginOffline, 0, 100, 0)
	if score > 75 {
		t.Errorf("expected <75, got %d", score)
	}
}

// 41. 健康分 - 配额>95%
func TestComputeHealthScore_QuotaOver95(t *testing.T) {
	score := computeHealthScore(WeComLoginOnline, 0.96, 100, 0)
	if score > 80 {
		t.Errorf("expected <80, got %d", score)
	}
}

// 42. 健康分 - 配额>90%
func TestComputeHealthScore_QuotaOver90(t *testing.T) {
	score := computeHealthScore(WeComLoginOnline, 0.91, 100, 0)
	if score > 90 {
		t.Errorf("expected <90, got %d", score)
	}
}

// 43. 健康分 - 配额>70%
func TestComputeHealthScore_QuotaOver70(t *testing.T) {
	score := computeHealthScore(WeComLoginOnline, 0.71, 100, 0)
	if score > 99 {
		t.Errorf("expected <99, got %d", score)
	}
}

// 44. 健康分 - 配额<=70%
func TestComputeHealthScore_QuotaUnder70(t *testing.T) {
	score := computeHealthScore(WeComLoginOnline, 0.5, 100, 0)
	if score != 100 {
		t.Errorf("expected 100, got %d", score)
	}
}

// 45. 健康分 - 成功率<50%
func TestComputeHealthScore_SuccessUnder50(t *testing.T) {
	score := computeHealthScore(WeComLoginOnline, 0, 40, 0)
	if score > 75 {
		t.Errorf("expected <75, got %d", score)
	}
}

// 46. 健康分 - 成功率<80%
func TestComputeHealthScore_SuccessUnder80(t *testing.T) {
	score := computeHealthScore(WeComLoginOnline, 0, 70, 0)
	if score > 90 {
		t.Errorf("expected <90, got %d", score)
	}
}

// 47. 健康分 - 成功率<95%
func TestComputeHealthScore_SuccessUnder95(t *testing.T) {
	score := computeHealthScore(WeComLoginOnline, 0, 90, 0)
	if score > 99 {
		t.Errorf("expected <99, got %d", score)
	}
}

// 48. 健康分 - 错误>50
func TestComputeHealthScore_ErrorsOver50(t *testing.T) {
	score := computeHealthScore(WeComLoginOnline, 0, 100, 60)
	if score > 80 {
		t.Errorf("expected <80, got %d", score)
	}
}

// 49. 健康分 - 错误>20
func TestComputeHealthScore_ErrorsOver20(t *testing.T) {
	score := computeHealthScore(WeComLoginOnline, 0, 100, 25)
	if score > 90 {
		t.Errorf("expected <90, got %d", score)
	}
}

// 50. 健康分 - 错误>5
func TestComputeHealthScore_ErrorsOver5(t *testing.T) {
	score := computeHealthScore(WeComLoginOnline, 0, 100, 10)
	if score > 99 {
		t.Errorf("expected <99, got %d", score)
	}
}

// 51. 健康分 - 下限0
func TestComputeHealthScore_LowerBound(t *testing.T) {
	score := computeHealthScore(WeComLoginOffline, 0.99, 30, 100)
	if score < 0 {
		t.Errorf("expected >=0, got %d", score)
	}
}

// 52. 健康分 - 上限100
func TestComputeHealthScore_UpperBound(t *testing.T) {
	score := computeHealthScore(WeComLoginOnline, 0, 100, 0)
	if score > 100 {
		t.Errorf("expected <=100, got %d", score)
	}
}

// 53. 风险等级 - 100
func TestComputeRiskLevel_100(t *testing.T) {
	risk := computeRiskLevel(100, WeComLoginOnline)
	if risk != WeComRiskNormal {
		t.Errorf("expected normal, got %s", risk)
	}
}

// 54. 风险等级 - 80
func TestComputeRiskLevel_80(t *testing.T) {
	risk := computeRiskLevel(80, WeComLoginOnline)
	if risk != WeComRiskWarning {
		t.Errorf("expected warning, got %s", risk)
	}
}

// 55. 风险等级 - 50
func TestComputeRiskLevel_50(t *testing.T) {
	risk := computeRiskLevel(50, WeComLoginOnline)
	if risk != WeComRiskCritical {
		t.Errorf("expected critical, got %s", risk)
	}
}

// 56. 风险等级 - 30
func TestComputeRiskLevel_30(t *testing.T) {
	risk := computeRiskLevel(30, WeComLoginOnline)
	if risk != WeComRiskCritical {
		t.Errorf("expected critical, got %s", risk)
	}
}

// 57. 风险等级 - 0
func TestComputeRiskLevel_0(t *testing.T) {
	risk := computeRiskLevel(0, WeComLoginOnline)
	if risk != WeComRiskCritical {
		t.Errorf("expected critical, got %s", risk)
	}
}

// 58. 风险等级 - 70 边界
func TestComputeRiskLevel_70Boundary(t *testing.T) {
	risk := computeRiskLevel(70, WeComLoginOnline)
	if risk != WeComRiskWarning {
		t.Errorf("expected warning, got %s", risk)
	}
}

// 59. 风险等级 - 40 边界
func TestComputeRiskLevel_40Boundary(t *testing.T) {
	risk := computeRiskLevel(40, WeComLoginOnline)
	if risk != WeComRiskCritical {
		t.Errorf("expected critical, got %s", risk)
	}
}

// 60. 风险等级 - 封禁优先
func TestComputeRiskLevel_BannedPriority(t *testing.T) {
	risk := computeRiskLevel(100, WeComLoginBanned)
	if risk != WeComRiskBanned {
		t.Errorf("expected banned, got %s", risk)
	}
}

// 61. 同步账号状态 - last_active_at
func TestSyncAccountState_LastActiveAt(t *testing.T) {
	svc, db := newWeComHealthService(t)
	a1 := mkWeComAccount(1)
	a1.LastActiveAt = nil
	db.Create(a1)
	svc.ReportHealth(context.Background(), &ReportHealthRequest{
		AccountID: 1,
		QuotaUsed: 0, QuotaTotal: 100, SuccessRate: 100, ErrorCount: 0,
	})
	var a model.WeComAccount
	db.First(&a, 1)
	if a.LastActiveAt == nil {
		t.Error("expected last_active_at to be set")
	}
}

// 62. 同步账号状态 - 错误计数
func TestSyncAccountState_ErrorCount(t *testing.T) {
	svc, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	svc.ReportHealth(context.Background(), &ReportHealthRequest{
		AccountID: 1,
		QuotaUsed: 0, QuotaTotal: 100, SuccessRate: 100, ErrorCount: 0,
		LastError: "send failed",
	})
	var a model.WeComAccount
	db.First(&a, 1)
	if a.ErrorCount < 1 {
		t.Errorf("expected error_count to increase, got %d", a.ErrorCount)
	}
	if a.LastErrorMsg == "" {
		t.Error("expected last_error_msg")
	}
}

// 63. 同步账号状态 - last_error_at
func TestSyncAccountState_LastErrorAt(t *testing.T) {
	svc, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	svc.ReportHealth(context.Background(), &ReportHealthRequest{
		AccountID: 1,
		QuotaUsed: 0, QuotaTotal: 100, SuccessRate: 100, ErrorCount: 0,
		LastError: "fail",
	})
	var a model.WeComAccount
	db.First(&a, 1)
	if a.LastErrorAt == nil {
		t.Error("expected last_error_at to be set")
	}
}

// 64. 同步账号状态 - 配额>90% 自动降为 warning
func TestSyncAccountState_AutoDegradeOnQuota(t *testing.T) {
	svc, db := newWeComHealthService(t)
	a1 := mkWeComAccount(1)
	a1.RiskLevel = WeComRiskNormal
	db.Create(a1)
	svc.ReportHealth(context.Background(), &ReportHealthRequest{
		AccountID: 1,
		QuotaUsed: 92, QuotaTotal: 100, SuccessRate: 100, ErrorCount: 0,
	})
	var a model.WeComAccount
	db.First(&a, 1)
	if a.RiskLevel != WeComRiskWarning {
		t.Errorf("expected auto-degrade to warning, got %s", a.RiskLevel)
	}
}

// 65. 同步账号状态 - 封禁同步 risk
func TestSyncAccountState_BannedRisk(t *testing.T) {
	svc, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	svc.ReportHealth(context.Background(), &ReportHealthRequest{
		AccountID:  1,
		LoginState: WeComLoginBanned,
		QuotaUsed:  0, QuotaTotal: 100, SuccessRate: 0, ErrorCount: 0,
	})
	var a model.WeComAccount
	db.First(&a, 1)
	if a.RiskLevel != WeComRiskBanned {
		t.Errorf("expected banned, got %s", a.RiskLevel)
	}
	if a.Weight != 0 {
		t.Errorf("expected weight 0, got %d", a.Weight)
	}
}

// 66. 同步账号状态 - 离线降权
func TestSyncAccountState_OfflineWeight(t *testing.T) {
	svc, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	svc.ReportHealth(context.Background(), &ReportHealthRequest{
		AccountID:  1,
		LoginState: WeComLoginOffline,
		QuotaUsed:  0, QuotaTotal: 100, SuccessRate: 100, ErrorCount: 0,
	})
	var a model.WeComAccount
	db.First(&a, 1)
	if a.Weight != 50 {
		t.Errorf("expected weight 50, got %d", a.Weight)
	}
}

// 67. 同步账号状态 - 在线恢复
func TestSyncAccountState_OnlineRestores(t *testing.T) {
	svc, db := newWeComHealthService(t)
	a1 := mkWeComAccount(1)
	a1.Weight = 0
	a1.LoginState = WeComLoginBanned
	db.Create(a1)
	svc.ReportHealth(context.Background(), &ReportHealthRequest{
		AccountID:  1,
		LoginState: WeComLoginOnline,
		QuotaUsed:  0, QuotaTotal: 100, SuccessRate: 100, ErrorCount: 0,
	})
	var a model.WeComAccount
	db.First(&a, 1)
	if a.Weight != 100 {
		t.Errorf("expected weight 100, got %d", a.Weight)
	}
}

// 68. WeCom 集成 - IngestMessage 创建消息与会话
func TestWeComIntegration_IngestMessage(t *testing.T) {
	_, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	integ := NewWeComIntegrationService(db)
	hubMsg, conv, err := integ.IngestMessage(context.Background(), &IngestRequest{

		AccountID:      1,
		ExternalUserID: "ext-001",
		Name:           "Alice",
		MsgType:        "text",
		Content:        "hello",
		MsgID:          "msg-001",
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if hubMsg == nil {
		t.Fatal("expected hub msg")
	}
	if conv == nil {
		t.Fatal("expected conversation")
	}
	if conv.CustomerID != "ext-001" {
		t.Errorf("expected ext-001, got %s", conv.CustomerID)
	}
}

// 69. 集成 - 多次 ingest 累加
func TestWeComIntegration_IngestAccumulate(t *testing.T) {
	_, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	integ := NewWeComIntegrationService(db)
	for i := 0; i < 3; i++ {
		integ.IngestMessage(context.Background(), &IngestRequest{

			AccountID:      1,
			ExternalUserID: "ext-001",
			MsgType:        "text",
			Content:        fmt.Sprintf("msg-%d", i),
			MsgID:          fmt.Sprintf("msg-%d", i),
		})
	}
	var conv model.InboxConversation
	db.Where("customer_id = ?", "ext-001").First(&conv)
	if conv.TotalCount != 3 {
		t.Errorf("expected 3 total, got %d", conv.TotalCount)
	}
	if conv.UnreadCount != 3 {
		t.Errorf("expected 3 unread, got %d", conv.UnreadCount)
	}
}

// 70. 集成 - 群消息
func TestWeComIntegration_IngestGroupMessage(t *testing.T) {
	_, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	integ := NewWeComIntegrationService(db)
	hubMsg, _, err := integ.IngestMessage(context.Background(), &IngestRequest{

		AccountID:      1,
		ExternalUserID: "ext-001",
		MsgType:        "text",
		Content:        "群消息",
		IsGroup:        true,
		GroupID:        "g-001",
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if !hubMsg.IsGroup {
		t.Error("expected is_group=true")
	}
	if hubMsg.GroupID != "g-001" {
		t.Errorf("expected g-001, got %s", hubMsg.GroupID)
	}
}

// 72. 集成 - 缺 account
func TestWeComIntegration_MissingAccount(t *testing.T) {
	_, db := newWeComHealthService(t)
	integ := NewWeComIntegrationService(db)
	_, _, err := integ.IngestMessage(context.Background(), &IngestRequest{})
	if err == nil {
		t.Error("expected error")
	}
}

// 73. 集成 - 缺 user
func TestWeComIntegration_MissingUser(t *testing.T) {
	_, db := newWeComHealthService(t)
	integ := NewWeComIntegrationService(db)
	_, _, err := integ.IngestMessage(context.Background(), &IngestRequest{})
	if err == nil {
		t.Error("expected error")
	}
}

// 74. 集成 - 自动生成 MsgID
func TestWeComIntegration_AutoMsgID(t *testing.T) {
	_, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	integ := NewWeComIntegrationService(db)
	hubMsg, _, err := integ.IngestMessage(context.Background(), &IngestRequest{

		AccountID:      1,
		ExternalUserID: "ext-001",
		Content:        "x",
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if hubMsg.MsgID == "" {
		t.Error("expected auto msg_id")
	}
}

// 75. 集成 - 自动生成 ConversationID
func TestWeComIntegration_AutoConvID(t *testing.T) {
	_, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	integ := NewWeComIntegrationService(db)
	hubMsg, _, _ := integ.IngestMessage(context.Background(), &IngestRequest{

		AccountID:      1,
		ExternalUserID: "ext-001",
		Content:        "x",
	})
	if hubMsg.ConversationID == "" {
		t.Error("expected auto conversation_id")
	}
}

// 76. 集成 - 幂等 (同一 MsgID)
func TestWeComIntegration_Idempotent(t *testing.T) {
	_, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	integ := NewWeComIntegrationService(db)
	_, _, _ = integ.IngestMessage(context.Background(), &IngestRequest{

		Content: "x", MsgID: "dup-1",
	})
	_, _, err := integ.IngestMessage(context.Background(), &IngestRequest{

		Content: "y", MsgID: "dup-1",
	})
	if err == nil {
		t.Error("expected idempotent error")
	}
}

// 77. 集成 - SendMessage
func TestWeComIntegration_SendMessage(t *testing.T) {
	_, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	integ := NewWeComIntegrationService(db)
	hubMsg, err := integ.SendMessage(context.Background(), &WeComSendRequest{
		AccountID:      1,
		ExternalUserID: "ext-001",
		MsgType:        "text", Content: "hi",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if hubMsg.Direction != "outbound" {
		t.Errorf("expected outbound, got %s", hubMsg.Direction)
	}
}

// 78. 集成 - Send AI 标记
func TestWeComIntegration_SendAI(t *testing.T) {
	_, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	integ := NewWeComIntegrationService(db)
	hubMsg, _ := integ.SendMessage(context.Background(), &WeComSendRequest{
		AccountID:      1,
		ExternalUserID: "ext-001",
		MsgType:        "text", Content: "AI reply", IsAIReply: true, AIAgent: "sop-001",
	})
	if !hubMsg.IsAIReply {
		t.Error("expected is_ai_reply=true")
	}
	if hubMsg.AIAgent != "sop-001" {
		t.Errorf("expected sop-001, got %s", hubMsg.AIAgent)
	}
}

// 79. 集成 - Send 配额超出
func TestWeComIntegration_SendQuotaExceeded(t *testing.T) {
	_, db := newWeComHealthService(t)
	a1 := mkWeComAccount(1)
	a1.DailyMsgUsed = 500
	a1.DailyMsgQuota = 500
	db.Create(a1)
	integ := NewWeComIntegrationService(db)
	_, err := integ.SendMessage(context.Background(), &WeComSendRequest{
		AccountID:      1,
		ExternalUserID: "ext-001",
		Content:        "x",
	})
	if err != ErrWeComQuotaExceeded {
		t.Errorf("expected quota exceeded, got %v", err)
	}
}

// 80. 集成 - Send 封禁
func TestWeComIntegration_SendBanned(t *testing.T) {
	_, db := newWeComHealthService(t)
	a1 := mkWeComAccount(1)
	a1.LoginState = WeComLoginBanned
	db.Create(a1)
	integ := NewWeComIntegrationService(db)
	_, err := integ.SendMessage(context.Background(), &WeComSendRequest{
		AccountID:      1,
		ExternalUserID: "ext-001",
		Content:        "x",
	})
	if err != ErrWeComAccountNotFound {
		t.Errorf("expected account not found, got %v", err)
	}
}

// 81. 集成 - Send 无可用账号
func TestWeComIntegration_SendNoAccount(t *testing.T) {
	_, db := newWeComHealthService(t)
	integ := NewWeComIntegrationService(db)
	_, err := integ.SendMessage(context.Background(), &WeComSendRequest{
		AccountID:      1,
		ExternalUserID: "ext-001",
		Content:        "x",
	})
	if err == nil {
		t.Error("expected error")
	}
}

// 82. 集成 - UpdateAccountStatus 封禁
func TestWeComIntegration_UpdateStatusBanned(t *testing.T) {
	_, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	integ := NewWeComIntegrationService(db)
	integ.UpdateAccountStatus(context.Background(), 1, WeComLoginBanned, "")
	var a model.WeComAccount
	db.First(&a, 1)
	if a.Weight != 0 || a.RiskLevel != WeComRiskBanned {
		t.Errorf("expected banned, got weight=%d risk=%s", a.Weight, a.RiskLevel)
	}
}

// 83. 集成 - UpdateAccountStatus 离线
func TestWeComIntegration_UpdateStatusOffline(t *testing.T) {
	_, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	integ := NewWeComIntegrationService(db)
	integ.UpdateAccountStatus(context.Background(), 1, WeComLoginOffline, "")
	var a model.WeComAccount
	db.First(&a, 1)
	if a.Weight != 50 {
		t.Errorf("expected weight 50, got %d", a.Weight)
	}
}

// 84. 集成 - UpdateAccountStatus 在线
func TestWeComIntegration_UpdateStatusOnline(t *testing.T) {
	_, db := newWeComHealthService(t)
	a1 := mkWeComAccount(1)
	a1.Weight = 0
	a1.LoginState = WeComLoginBanned
	db.Create(a1)
	integ := NewWeComIntegrationService(db)
	integ.UpdateAccountStatus(context.Background(), 1, WeComLoginOnline, "")
	var a model.WeComAccount
	db.First(&a, 1)
	if a.Weight != 100 {
		t.Errorf("expected weight 100, got %d", a.Weight)
	}
}

// 85. 集成 - ListAccountsWithHealth
func TestWeComIntegration_ListAccountsWithHealth(t *testing.T) {
	_, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	integ := NewWeComIntegrationService(db)
	list, err := integ.ListAccountsWithHealth(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1, got %d", len(list))
	}
}

// 86. 集成 - ListAccountsWithHealth 多个
func TestWeComIntegration_ListAccountsWithHealth_Multiple(t *testing.T) {
	_, db := newWeComHealthService(t)
	for i := uint(1); i <= 5; i++ {
		db.Create(mkWeComAccount(i))
	}
	integ := NewWeComIntegrationService(db)
	list, _ := integ.ListAccountsWithHealth(context.Background())
	if len(list) != 5 {
		t.Errorf("expected 5, got %d", len(list))
	}
}

// 87. 集成 - ListAccountsWithHealth 包含 health
func TestWeComIntegration_ListAccountsWithHealth_WithHealth(t *testing.T) {
	svc, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	svc.ReportHealth(context.Background(), &ReportHealthRequest{
		AccountID: 1,
		QuotaUsed: 0, QuotaTotal: 100, SuccessRate: 100, ErrorCount: 0,
	})
	integ := NewWeComIntegrationService(db)
	list, _ := integ.ListAccountsWithHealth(context.Background())
	if len(list) != 1 || list[0].Health == nil {
		t.Error("expected health to be included")
	}
}

// 88. 集成 - ReceiveCallback
func TestWeComIntegration_ReceiveCallback(t *testing.T) {
	_, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	integ := NewWeComIntegrationService(db)
	hubMsg, conv, err := integ.ReceiveCallback(context.Background(), &ReceiveCallbackRequest{
		AccountID: 1,
		FromUser:  "ext-001",
		MsgType:   "text", Content: "callback", MsgID: "cb-001",
	})
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	if hubMsg == nil || conv == nil {
		t.Error("expected hub and conv")
	}
}

// 89. 集成 - ReceiveCallback 群
func TestWeComIntegration_ReceiveCallbackGroup(t *testing.T) {
	_, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	integ := NewWeComIntegrationService(db)
	hubMsg, _, _ := integ.ReceiveCallback(context.Background(), &ReceiveCallbackRequest{
		AccountID: 1,
		FromUser:  "ext-002",
		Content:   "group msg", ChatID: "g-1", ChatType: "group", MsgID: "g-1",
	})
	if !hubMsg.IsGroup {
		t.Error("expected group")
	}
}

// 90. 集成 - ReceiveCallback 缺字段
func TestWeComIntegration_ReceiveCallbackMissing(t *testing.T) {
	_, db := newWeComHealthService(t)
	integ := NewWeComIntegrationService(db)
	_, _, err := integ.ReceiveCallback(context.Background(), &ReceiveCallbackRequest{
		Content: "x", MsgID: "y",
	})
	if err == nil {
		t.Error("expected error")
	}
}

// 91. 初始化服务
func TestInitWeComAccountHealthService(t *testing.T) {
	svc1 := InitWeComAccountHealthService(nil)
	svc2 := GetWeComAccountHealthService()
	if svc1 != svc2 {
		t.Error("expected same instance")
	}
}

// 92. 全局实例一致性
func TestWeComAccountHealthService_Global(t *testing.T) {
	svc := GetWeComAccountHealthService()
	if svc == nil {
		t.Error("expected non-nil")
	}
}

// 93. 错误封装
func TestWeComErrors_NonNil(t *testing.T) {
	if ErrWeComAccountNotFound == nil {
		t.Error("expected non-nil")
	}
	if ErrWeComInvalidAccountID == nil {
		t.Error("expected non-nil")
	}
	if ErrWeComQuotaExceeded == nil {
		t.Error("expected non-nil")
	}
	if ErrWeComAccountBanned == nil {
		t.Error("expected non-nil")
	}
	if ErrWeComAccountOffline == nil {
		t.Error("expected non-nil")
	}
	if ErrWeComHealthNotFound == nil {
		t.Error("expected non-nil")
	}
}

// 94. 常量
func TestWeComConstants(t *testing.T) {
	if WeComHealthScoreNormal != 100 {
		t.Errorf("expected 100")
	}
	if WeComHealthScoreBanned != 0 {
		t.Errorf("expected 0")
	}
	if WeComRiskNormal == "" {
		t.Error("expected non-empty")
	}
}

// 95. accountHealthFromModel
func TestAccountHealthFromModel(t *testing.T) {
	a := &model.WeComAccount{Weight: 88}
	if accountHealthFromModel(a) != 88 {
		t.Error("expected 88")
	}
	if accountHealthFromModel(nil) != 0 {
		t.Error("expected 0 for nil")
	}
}

// 96. ReportHealth 多次累加错误数
func TestReportHealth_AccumulatesErrors(t *testing.T) {
	svc, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	for i := 0; i < 3; i++ {
		svc.ReportHealth(context.Background(), &ReportHealthRequest{
			AccountID: 1,
			QuotaUsed: 0, QuotaTotal: 100, SuccessRate: 100, ErrorCount: 0,
			LastError: "err",
		})
	}
	var a model.WeComAccount
	db.First(&a, 1)
	if a.ErrorCount < 3 {
		t.Errorf("expected >=3 errors, got %d", a.ErrorCount)
	}
}

// 97. 配额使用率 0
func TestReportHealth_QuotaZero(t *testing.T) {
	svc, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	rec, _ := svc.ReportHealth(context.Background(), &ReportHealthRequest{
		AccountID: 1,
		QuotaUsed: 0, QuotaTotal: 0, SuccessRate: 100, ErrorCount: 0,
	})
	if rec.QuotaUsageRate != 0 {
		t.Errorf("expected 0, got %f", rec.QuotaUsageRate)
	}
}

// 98. nil metrics 自动初始化
func TestReportHealth_NilMetrics(t *testing.T) {
	svc, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	rec, _ := svc.ReportHealth(context.Background(), &ReportHealthRequest{
		AccountID: 1,
		QuotaUsed: 0, QuotaTotal: 100, SuccessRate: 100, ErrorCount: 0,
		Metrics: nil,
	})
	if rec.Metrics == nil {
		t.Error("expected non-nil metrics")
	}
}

// 99. ListHistory 空分页保护
func TestListHealthHistory_EmptyPage(t *testing.T) {
	svc, _ := newWeComHealthService(t)
	list, total, _ := svc.ListHealthHistory(context.Background(), 1, 0, 0)
	if total != 0 || list == nil {
		t.Errorf("expected empty")
	}
}

// 100. ListHealthHistory pageSize 上限
func TestListHealthHistory_PageSizeLimit(t *testing.T) {
	svc, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	svc.ReportHealth(context.Background(), &ReportHealthRequest{
		AccountID: 1,
		QuotaUsed: 0, QuotaTotal: 100, SuccessRate: 100, ErrorCount: 0,
	})
	_, total, _ := svc.ListHealthHistory(context.Background(), 1, 1, 99999)
	if total != 1 {
		t.Errorf("expected 1, got %d", total)
	}
}

// 101. 集成 Ingest 设置 ReceivedAt
func TestWeComIntegration_IngestReceivedAt(t *testing.T) {
	_, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	integ := NewWeComIntegrationService(db)
	now := time.Now()
	hubMsg, _, _ := integ.IngestMessage(context.Background(), &IngestRequest{

		AccountID:      1,
		ExternalUserID: "ext-001",
		Content:        "x",
		ReceivedAt:     now,
	})
	if hubMsg.SentAt.IsZero() {
		t.Error("expected sent_at set")
	} else if !hubMsg.SentAt.Equal(now) {
		t.Errorf("expected sent_at=%v, got %v", now, hubMsg.SentAt)
	}
}

// 102. 健康分 - 多种边界组合
func TestComputeHealthScore_MultipleBounds(t *testing.T) {
	score := computeHealthScore(WeComLoginOffline, 0.99, 30, 100)
	if score > 25 {
		t.Errorf("expected very low, got %d", score)
	}
}

// 103. 集成 SendMessage 媒体ID
func TestWeComIntegration_SendMedia(t *testing.T) {
	_, db := newWeComHealthService(t)
	db.Create(mkWeComAccount(1))
	integ := NewWeComIntegrationService(db)
	hubMsg, _ := integ.SendMessage(context.Background(), &WeComSendRequest{
		AccountID:      1,
		ExternalUserID: "ext-001",
		MsgType:        "image", MediaID: "media-001",
	})
	if hubMsg.MsgType != "image" {
		t.Errorf("expected image, got %s", hubMsg.MsgType)
	}
	if hubMsg.MediaURL != "media-001" {
		t.Errorf("expected media-001, got %s", hubMsg.MediaURL)
	}
}

// 104. 健康概览 - 配额汇总
func TestGetHealthSummary_TotalQuota(t *testing.T) {
	svc, db := newWeComHealthService(t)
	a1 := mkWeComAccount(1)
	a1.DailyMsgQuota = 1000
	a1.DailyMsgUsed = 200
	db.Create(a1)
	a2 := mkWeComAccount(2)
	a2.DailyMsgQuota = 2000
	a2.DailyMsgUsed = 500
	db.Create(a2)
	summary, _ := svc.GetHealthSummary(context.Background())
	if summary.TotalQuota != 3000 {
		t.Errorf("expected 3000, got %d", summary.TotalQuota)
	}
	if summary.TotalUsed != 700 {
		t.Errorf("expected 700, got %d", summary.TotalUsed)
	}
}

// 105. 并发扣减不超发：quota=10，50 个并发各扣 1，恰好成功 10 次、used 终值 10
func TestConsumeQuota_ConcurrentNoOvershoot(t *testing.T) {
	svc, db := newWeComHealthService(t)
	a := mkWeComAccount(1)
	a.DailyMsgQuota = 10
	db.Create(a)

	const goroutines = 50
	var wg sync.WaitGroup
	var success int64
	var mu sync.Mutex
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := svc.ConsumeQuota(context.Background(), 1, 1)
			if err == nil {
				mu.Lock()
				success++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if success != 10 {
		t.Errorf("W-7 未达成：期望恰好 10 次成功（不超发不漏发），实际 %d 次", success)
	}
	var final model.WeComAccount
	db.First(&final, 1)
	if final.DailyMsgUsed != 10 {
		t.Errorf("expected daily_msg_used=10, got %d", final.DailyMsgUsed)
	}
	if final.TotalSent != 10 {
		t.Errorf("expected total_sent=10, got %d", final.TotalSent)
	}
}

// 106. 原子扣减后 last_active_at 被更新
func TestConsumeQuota_UpdatesLastActiveAt(t *testing.T) {
	svc, db := newWeComHealthService(t)
	a := mkWeComAccount(1)
	old := time.Now().Add(-24 * time.Hour)
	a.LastActiveAt = &old
	db.Create(a)
	if err := svc.ConsumeQuota(context.Background(), 1, 3); err != nil {
		t.Fatalf("consume: %v", err)
	}
	var final model.WeComAccount
	db.First(&final, 1)
	if final.LastActiveAt == nil || final.LastActiveAt.Before(time.Now().Add(-time.Minute)) {
		t.Error("expected last_active_at refreshed")
	}
}
