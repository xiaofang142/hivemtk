package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// CSAT 看板三处口径（2026-10-08 页面巡检发现，均已在库上实测到失效形态）：
// ① 会话重触发把已回收的评分推回 sent ⇒ 打了分却在按 status 过滤的看板上消失；
// ② Stats 无视 window 参数 ⇒ 卡片写着"本月"、数的是全量；
// ③ 差评列表只有调查单本体 ⇒ 坐席/客户两列永远空；按 cs.agent_id 取列的坐席归因
//    SQL 直接报 42703，被上层降级成"无 CSAT 数据"，绩效页的均分静默为 0。

func newCSATDashboardFixture(t *testing.T) (*CSATService, *gorm.DB) {
	t.Helper()
	database := testutil.NewTestDB(t,
		&model.CustomerSession{},
		&model.CSATSurvey{},
		&model.SystemConfigKV{},
	)
	db.SetTestDB(database)
	return NewCSATService(), database
}

// seedSession 建一条已关闭的 web 会话（Trigger 会先按 session_id 回查它）。
func seedSession(t *testing.T, database *gorm.DB, sessionID string, agentID uint, agentName, userName string) {
	t.Helper()
	sess := &model.CustomerSession{
		SessionID: sessionID,
		Platform:  "web",
		Status:    "closed",
		OneID:     "oneid-" + sessionID,
		UserName:  userName,
		AgentID:   agentID,
		AgentName: agentName,
	}
	if err := database.Create(sess).Error; err != nil {
		t.Fatalf("seed session %s: %v", sessionID, err)
	}
}

func readSurvey(t *testing.T, database *gorm.DB, sessionID string) model.CSATSurvey {
	t.Helper()
	var out model.CSATSurvey
	if err := database.Where("session_id = ?", sessionID).First(&out).Error; err != nil {
		t.Fatalf("回查调查单 %s: %v", sessionID, err)
	}
	return out
}

// TestCSATService_ReTriggerKeepsRespondedSurvey 评分回收之后再次触发，不得把调查单推回 sent。
func TestCSATService_ReTriggerKeepsRespondedSurvey(t *testing.T) {
	svc, database := newCSATDashboardFixture(t)
	ctx := context.Background()
	const sid = "csat-retrigger-keep-responded"
	seedSession(t, database, sid, 7, "admin", "客户甲")

	if _, err := svc.Trigger(ctx, sid, "auto"); err != nil {
		t.Fatalf("首次 Trigger: %v", err)
	}
	if got := readSurvey(t, database, sid).Status; got != model.CSATStatusSent {
		t.Fatalf("首次触发后状态应为 %s，实际 %s", model.CSATStatusSent, got)
	}

	if _, err := svc.Submit(ctx, sid, 4, "还行"); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := svc.Trigger(ctx, sid, "auto"); err != nil {
		t.Fatalf("重触发 Trigger: %v", err)
	}

	after := readSurvey(t, database, sid)
	if after.Status != model.CSATStatusResponded {
		t.Fatalf("重触发把已回收的评分推回了 %q（看板上这条会凭空消失）", after.Status)
	}
	if after.Score != 4 {
		t.Fatalf("评分应保持 4，实际 %d", after.Score)
	}

	// 判据打在"看板读的那一侧"：status 没被改写才会被统计到
	stats, err := svc.Stats(ctx, "")
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if got, _ := stats["responded"].(int64); got != 1 {
		t.Fatalf("看板应统计到这条评分，responded=%v（want 1）", got)
	}
}

// TestCSATService_StatsWindowAndDerivedRates window 必须真的筛掉窗口外的评分，
// 好评率/差评数从同一份分布派生（阈值只有一个事实源）。
func TestCSATService_StatsWindowAndDerivedRates(t *testing.T) {
	svc, database := newCSATDashboardFixture(t)
	ctx := context.Background()
	now := time.Now()

	seed := func(sid string, score int, respondedAt time.Time) {
		seedSession(t, database, sid, 1, "admin", "客户")
		survey := &model.CSATSurvey{
			SessionID:   sid,
			OneID:       "oneid-" + sid,
			Score:       score,
			Status:      model.CSATStatusResponded,
			TriggeredBy: "auto",
			RespondedAt: &respondedAt,
		}
		if err := database.Create(survey).Error; err != nil {
			t.Fatalf("seed survey %s: %v", sid, err)
		}
	}
	seed("csat-win-5now", 5, now)
	seed("csat-win-1now", 1, now)
	// 40 天前：无论当月是 28/29/30/31 天，都必然落在"本月"窗口之外
	seed("csat-win-4old", 4, now.AddDate(0, 0, -40))

	all, err := svc.Stats(ctx, "")
	if err != nil {
		t.Fatalf("Stats(all): %v", err)
	}
	if got, _ := all["responded"].(int64); got != 3 {
		t.Fatalf("全量 responded=%v（want 3）", got)
	}
	// 默认模板 low_threshold=3 ⇒ 1 分算差评、4/5 分算好评
	if got, _ := all["negative_count"].(int64); got != 1 {
		t.Fatalf("全量 negative_count=%v（want 1）", got)
	}
	if got, _ := all["positive_rate"].(float64); got != 66.67 {
		t.Fatalf("全量 positive_rate=%v（want 66.67）", got)
	}
	if got, _ := all["window"].(string); got != "全部" {
		t.Fatalf("window 回显=%q（want 全部）", got)
	}

	month, err := svc.Stats(ctx, "month")
	if err != nil {
		t.Fatalf("Stats(month): %v", err)
	}
	if got, _ := month["responded"].(int64); got != 2 {
		t.Fatalf("本月 responded=%v（want 2，窗口外的 4 分那条不该计入）", got)
	}
	if got, _ := month["avg_score"].(float64); got != 3 {
		t.Fatalf("本月 avg_score=%v（want 3）", got)
	}
	if got, _ := month["positive_rate"].(float64); got != 50 {
		t.Fatalf("本月 positive_rate=%v（want 50）", got)
	}
	if got, _ := month["window"].(string); got != "本月" {
		t.Fatalf("window 回显=%q（want 本月）", got)
	}
}

// TestCSATService_NegativeCarriesAgentAndCustomerNames 差评列表要带出坐席与客户名，
// 否则页面上那两列永远空白（名字只存在于 customer_sessions）。
func TestCSATService_NegativeCarriesAgentAndCustomerNames(t *testing.T) {
	svc, database := newCSATDashboardFixture(t)
	ctx := context.Background()
	now := time.Now()

	seedSession(t, database, "csat-neg-1", 42, "坐席乙", "客户丙")
	if err := database.Create(&model.CSATSurvey{
		SessionID:   "csat-neg-1",
		Score:       2,
		Comment:     "等太久",
		Status:      model.CSATStatusResponded,
		RespondedAt: &now,
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	list, threshold, err := svc.Negative(ctx, 50)
	if err != nil {
		t.Fatalf("Negative: %v", err)
	}
	if threshold != 3 {
		t.Fatalf("阈值应取模板 low_threshold=3，实际 %d", threshold)
	}
	if len(list) != 1 {
		t.Fatalf("差评应有 1 条，实际 %d", len(list))
	}
	if list[0].AgentName != "坐席乙" {
		t.Fatalf("坐席名未随差评返回：%q", list[0].AgentName)
	}
	if list[0].UserName != "客户丙" {
		t.Fatalf("客户名未随差评返回：%q", list[0].UserName)
	}
	if list[0].SessionID != "csat-neg-1" {
		t.Fatalf("会话 ID 应回显，实际 %q", list[0].SessionID)
	}
}

// TestAgentAttribution_CSATAggregatesBySessionAgent 坐席归因的 CSAT SQL 必须能跑通并按坐席聚合。
// csat_surveys 从来没有 agent_id 列，早先按 cs.agent_id 取列时报 42703，
// 而调用方把错误降级成"无 CSAT 数据"，所以这条判据打的是**聚合结果**而不是 err==nil。
func TestAgentAttribution_CSATAggregatesBySessionAgent(t *testing.T) {
	_, database := newCSATDashboardFixture(t)
	ctx := context.Background()
	now := time.Now()

	seedSession(t, database, "csat-attr-1", 42, "坐席乙", "客户丙")
	if err := database.Create(&model.CSATSurvey{
		SessionID:   "csat-attr-1",
		Score:       5,
		Status:      model.CSATStatusResponded,
		RespondedAt: &now,
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	repo := repository.NewAgentAttributionRepository()
	rows, err := repo.AggregateCSATByAgent(ctx, now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatalf("AggregateCSATByAgent 仍然跑不通（坐席绩效页的 CSAT 会静默为 0）：%v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("应按坐席聚出 1 行，实际 %d 行", len(rows))
	}
	if rows[0].AgentID != 42 || rows[0].Responded != 1 || rows[0].AvgScore != 5 {
		t.Fatalf("聚合结果不符：agent=%d responded=%d avg=%.2f", rows[0].AgentID, rows[0].Responded, rows[0].AvgScore)
	}
}

// TestCSATService_NegativeSerializesEmptyListAsArray 没有差评时接口必须回 []，不是 null：
// 消费端（看板、坐席端）都按数组遍历，null 让"今天零差评"和"接口坏了"长得一模一样。
func TestCSATService_NegativeSerializesEmptyListAsArray(t *testing.T) {
	svc, database := newCSATDashboardFixture(t)
	ctx := context.Background()
	now := time.Now()

	seedSession(t, database, "csat-empty-1", 43, "坐席甲", "客户丁")
	if err := database.Create(&model.CSATSurvey{
		SessionID:   "csat-empty-1",
		Score:       5,
		Status:      model.CSATStatusResponded,
		RespondedAt: &now,
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	list, _, err := svc.Negative(ctx, 50)
	if err != nil {
		t.Fatalf("Negative: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("5 分不该进差评列表（阈值 3），实际 %d 条", len(list))
	}
	raw, err := json.Marshal(list)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(raw) != "[]" {
		t.Fatalf("空差评应序列化成 []，实际 %s", raw)
	}
}
