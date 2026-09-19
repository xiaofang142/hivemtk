// seed_stats.go 模块 J：统计看板种子数据
//
// ⚠️ 本模块写的**全部是演示数据**，不是任何看板的真实源。三张表的情况各不相同，
// 其中 `conversion_funnels` 是 R-4 点名的僵尸表：
//
//	conversion_funnels (35) 转化漏斗，7 天 × 5 个阶段 —— **全仓没有读路径**。
//	      `GET /api/conversion-funnel` 走 `internal/ops/service` 对 customer_events /
//	      clues / intent_records / customer_sessions 的实时聚合，既不读本表、阶段名也
//	      与本文件的 exposure/click/consult/add_wecom/deal 完全不同（真实词表在
//	      `internal/ops/repository.FunnelStageKey`）。所以这里改阶段名不会影响任何线上
//	      读数；反过来，谁想给漏斗加阶段，去改那份词表 + 聚合分支，**不要往本表补行**。
//	      判据与"什么时候可以真删这张表"写在 model.ConversionFunnel 的注释里。
//	sales_personas (8) 销冠画像，覆盖 S/A/B/C 四个等级
//	wecom_account_health (8) 企微账号健康度，覆盖 normal/warning/critical/banned
package main

import (
	"fmt"
	"log"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

type statsSeeder struct{}

func (s *statsSeeder) Name() string { return "stats" }
func (s *statsSeeder) Description() string {
	return "漏斗(35)+销冠画像(8)+企微健康度(8)；均为演示数据，其中 conversion_funnels 无读路径"
}

func (s *statsSeeder) Clean(database *gorm.DB) error {
	if err := database.Where("last_error LIKE ? OR metrics LIKE ?", "%"+seedTag+"%", "%"+seedTag+"%").
		Delete(&model.WeComAccountHealth{}).Error; err != nil {
		return fmt.Errorf("清空 wecom_account_health 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.SalesPersona{}, "sales_name LIKE ?", "%"+seedTag+"%"); err != nil {
		return fmt.Errorf("清空 sales_personas 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.ConversionFunnel{}, "extra LIKE ?", "%"+seedTag+"%"); err != nil {
		return fmt.Errorf("清空 conversion_funnels 失败: %w", err)
	}
	return nil
}

func (s *statsSeeder) Seed(database *gorm.DB, ctx *SeedContext) error {
	funnels := s.buildFunnels()
	if err := batchInsert(database, funnels, 50); err != nil {
		return fmt.Errorf("写入 conversion_funnels 失败: %w", err)
	}
	for _, f := range funnels {
		ctx.FunnelIDs = append(ctx.FunnelIDs, uint64(f.ID))
	}

	personas := s.buildSalesPersonas(ctx)
	if err := batchInsert(database, personas, 50); err != nil {
		return fmt.Errorf("写入 sales_personas 失败: %w", err)
	}
	for _, p := range personas {
		ctx.SalesPersonaIDs = append(ctx.SalesPersonaIDs, uint64(p.ID))
	}

	healths := s.buildWecomHealth(ctx)
	if err := batchInsert(database, healths, 50); err != nil {
		return fmt.Errorf("写入 wecom_account_health 失败: %w", err)
	}

	log.Printf("  ✓ 已写入 漏斗%d+销冠画像%d+企微健康度%d",
		len(funnels), len(personas), len(healths))
	// "演示表不是真实源"这件事最容易在跑完 seed 的第二天被忘掉，所以每次 seed 都打出来。
	log.Printf("  ⚠️ conversion_funnels 是演示表：GET /api/conversion-funnel 不读它（走 ops/service 实时聚合），" +
		"两边阶段名也不成套 ⇒ 看板/报表都别从这张表取数（R-4；判据见 model.ConversionFunnel 注释）")
	return nil
}

// buildFunnels 生成 7 天 × 5 阶段的漏斗数据
// 漏斗阶段：曝光 → 点击 → 咨询 → 加微 → 成交
//
// 这五个阶段名是**本文件自造的演示词表**，与线上漏斗真正用的
// `ops/repository.FunnelStageKey`（visit/clue/intent/session）不成一套。刻意不去对齐：
// 对齐了只会让这张僵尸表看起来更像真的（R-4 要的是能辨真伪，不是看起来一致）。
func (s *statsSeeder) buildFunnels() []model.ConversionFunnel {
	funnelType := "sales"
	stages := []struct {
		Name  string
		Order int
		Base  int
	}{
		{"exposure", 1, 10000},
		{"click", 2, 3500},
		{"consult", 3, 880},
		{"add_wecom", 4, 220},
		{"deal", 5, 38},
	}
	funnels := make([]model.ConversionFunnel, 0, 35)
	for d := 0; d < 7; d++ {
		statDate := daysAgo(d).Format("2006-01-02")
		// 引入 ±10% 浮动
		var prevCount int
		for _, st := range stages {
			count := st.Base
			if d > 0 {
				factor := 0.9 + 0.2*float64((d*7+st.Order)%5)/4.0
				count = int(float64(st.Base) * factor)
			}
			var convRate, dropOffRate float64
			if prevCount > 0 {
				convRate = float64(count) / float64(prevCount) * 100
				dropOffRate = 100 - convRate
			}
			f := model.ConversionFunnel{
				StatDate:       statDate,
				FunnelType:     funnelType,
				Stage:          st.Name,
				StageOrder:     st.Order,
				Count:          count,
				ConversionRate: float64(int(convRate*100)) / 100,
				DropOffRate:    float64(int(dropOffRate*100)) / 100,
				AvgDurationSec: randInt(30, 600),
				Extra: model.JSONMap{
					"seed":      seedTag,
					"scenario":  "演示销售漏斗",
					"funnel":    funnelType,
					"demo_only": true,
					// 把真源指针写进行数据里：拿 BI 脚本/临时 SQL 读这张表的人不会先去翻代码注释。
					"real_source": "GET /api/conversion-funnel 走 ops/service 实时聚合，不读本表",
				},
			}
			funnels = append(funnels, f)
			prevCount = count
		}
	}
	return funnels
}

// buildSalesPersonas 生成 8 个销冠画像，覆盖 S/A/B/C 四个等级
func (s *statsSeeder) buildSalesPersonas(ctx *SeedContext) []model.SalesPersona {
	specs := []struct {
		Name      string
		Level     string
		Score     float64
		Scenarios []string
		Skills    []string
	}{
		{"陈美琳", "S", 96.5, []string{"高客单价", "VIP客户", "复购运营"}, []string{"倾听", "共情", "逼单", "异议处理"}},
		{"王浩然", "S", 94.2, []string{"电商爆款", "直播转化"}, []string{"节奏控场", "数据驱动", "限时逼单"}},
		{"李雪", "A", 88.7, []string{"教育行业", "高潜客户"}, []string{"耐心讲解", "案例分享"}},
		{"赵强", "A", 85.3, []string{"3C数码", "技术型客户"}, []string{"产品专业", "比价分析"}},
		{"刘婷", "B", 75.6, []string{"服饰美妆", "新客培育"}, []string{"亲和力", "搭配推荐"}},
		{"孙伟", "B", 72.1, []string{"快消品", "活动营销"}, []string{"活动引导", "话术执行"}},
		{"周敏", "C", 65.4, []string{"基础咨询"}, []string{"标准话术"}},
		{"吴磊", "C", 61.8, []string{"售后跟进"}, []string{"工单跟进"}},
	}
	personas := make([]model.SalesPersona, 0, len(specs))
	for i, sp := range specs {
		var salesID string
		if len(ctx.CSUserIDs) > i {
			salesID = fmt.Sprintf("%d", ctx.CSUserIDs[i])
		} else {
			salesID = fmt.Sprintf("seed-sales-%d", i+1)
		}
		totalCustomers := randInt(20, 500)
		convertedCustomers := int(float64(totalCustomers) * (sp.Score / 100.0) * 0.3)
		activeCustomers := totalCustomers - convertedCustomers - randInt(0, 20)
		if activeCustomers < 0 {
			activeCustomers = 0
		}
		avgDeal := int64(randInt(500, 10000)) * 100
		totalRev := avgDeal * int64(convertedCustomers)
		lastActive := hoursAgo(randInt(1, 48))
		p := model.SalesPersona{
			SalesID:            salesID,
			SalesName:          sp.Name + " " + seedTag,
			Avatar:             fmt.Sprintf("https://example.com/seed/sales-%d.png", i+1),
			TotalCustomers:     totalCustomers,
			ActiveCustomers:    activeCustomers,
			ConvertedCustomers: convertedCustomers,
			ConversionRate:     float64(int(float64(convertedCustomers)/float64(totalCustomers)*10000)) / 100,
			AvgResponseSec:     randInt(15, 300),
			AvgDealAmount:      avgDeal,
			TotalRevenue:       totalRev,
			SkillTags:          model.JSONArray(toAnySlice(sp.Skills)),
			BestScenarios:      model.JSONArray(toAnySlice(sp.Scenarios)),
			WorkDays:           randInt(30, 800),
			LastActiveAt:       &lastActive,
			Level:              sp.Level,
			LevelScore:         sp.Score,
		}
		personas = append(personas, p)
	}
	return personas
}

// buildWecomHealth 生成 8 个企微账号的健康度记录
func (s *statsSeeder) buildWecomHealth(ctx *SeedContext) []model.WeComAccountHealth {
	riskSpecs := []struct {
		Risk        string
		Health      int
		QuotaUsed   int
		QuotaTotal  int
		SuccessRate float64
		ErrorCount  int
		LoginState  string
	}{
		{"normal", 95, 30, 100, 99.5, 0, "online"},
		{"normal", 88, 50, 100, 98.2, 1, "online"},
		{"warning", 72, 75, 100, 92.0, 5, "online"},
		{"warning", 65, 85, 100, 88.5, 12, "online"},
		{"critical", 40, 95, 100, 75.0, 30, "online"},
		{"critical", 32, 98, 100, 68.5, 50, "degraded"},
		{"banned", 15, 100, 100, 45.0, 100, "offline"},
		{"banned", 8, 100, 100, 32.0, 150, "offline"},
	}
	healths := make([]model.WeComAccountHealth, 0, len(riskSpecs))
	for i, sp := range riskSpecs {
		var accountID uint
		if len(ctx.PlatformAccountIDs) > i {
			accountID = ctx.PlatformAccountIDs[i]
		} else {
			accountID = uint(i + 1)
		}
		var lastError string
		if sp.ErrorCount > 0 {
			lastError = fmt.Sprintf("演示：累计 %d 次错误 %s", sp.ErrorCount, seedTag)
		}
		h := model.WeComAccountHealth{
			AccountID:      accountID,
			Platform:       "wecom",
			HealthScore:    sp.Health,
			RiskLevel:      sp.Risk,
			LoginState:     sp.LoginState,
			QuotaUsed:      sp.QuotaUsed,
			QuotaTotal:     sp.QuotaTotal,
			QuotaUsageRate: float64(sp.QuotaUsed) / float64(sp.QuotaTotal) * 100,
			SuccessRate:    sp.SuccessRate,
			ErrorCount:     sp.ErrorCount,
			LastError:      lastError,
			Metrics: model.JSONMap{
				"seed":         seedTag,
				"avg_latency":  randInt(50, 2000),
				"login_count":  randInt(1, 30),
				"logout_count": randInt(0, 5),
				"risk_reason":  []string{"频繁操作", "敏感词触发", "投诉举报", "新设备登录"}[i%4],
			},
			ReportedAt: hoursAgo(randInt(0, 12)),
		}
		healths = append(healths, h)
	}
	return healths
}

// toAnySlice 将 []string 转为 []any（便于构造 JSONArray）
func toAnySlice(ss []string) []any {
	out := make([]any, 0, len(ss))
	for _, s := range ss {
		out = append(out, s)
	}
	return out
}

// 防止 time 未使用警告
var _ = time.Now
