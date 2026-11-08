// cs04_announcement_ai_count_test.go CS-04 刀3：转接公告的 AI 应答量归零观测口径。
//
// 缺口③：桥接出站行统一打 IsAIReply=true / scenario=auto_reply（webhook_outbound.go
// 桥接分支常量），转接公告与 AI 回复同行走同一条出库链、同一行常量——但公告不是
// AI 应答：低置信转人工轮（SampleB 实测形态）里那 1 条出站是「已转人工」提示，
// 计进 AICount 等于「AI 答了」。CS-01 样本②已在行上落 handler_type=human
// （HandleResultToContext 注入，审计分流的凭据），小时聚合（aggregateHubRows）
// 却只认 IsAIReply 布尔——公告全部灌进 AICount，AI 应答量虚高。
//
// 本文件钉住：出站行 Extra.handler_type=human 的行**不计 AICount**，也不冒充
// HumanCount（坐席没回话，公告是系统提示；归 MessageCount 口径）。正常 AI 回复
// （handler_type=ai 或无标记的既有行）照旧计 AICount。
package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// cs04HubRow 刀3 造行 helper：与包内 hubRow 同形但可带 Extra（handler_type 在 Extra 里）。
func cs04HubRow(at time.Time, isAI bool, direction string, extra model.JSONMap) *model.MessageHub {
	return &model.MessageHub{
		Platform:       "douyin",
		MsgID:          "cs04-agg-" + at.Format("150405.000000000"),
		AccountID:      "acc-cs04",
		Direction:      direction,
		Status:         "done",
		MsgType:        "text",
		ConversationID: "cs04-agg-conv",
		IsAIReply:      isAI,
		Extra:          extra,
		SentAt:         at,
		CreatedAt:      at,
	}
}

func TestCS04_AnnouncementNotCountedAsAIReply(t *testing.T) {
	database := setupSummaryTestDB(t)
	ctx := context.Background()
	h0 := time.Now().Truncate(time.Hour).Add(-1 * time.Hour)

	// 三行同桶：正常 AI 回复 / 转接公告（handler_type=human）/ 人工出站（无 AI 标记）。
	rows := []*model.MessageHub{
		cs04HubRow(h0.Add(1*time.Minute), true, "outbound", model.JSONMap{
			"handler_type": "ai", "scenario": "auto_reply",
		}),
		// 公告行形态=SampleB 实测：IsAIReply 常量 true + handler_type=human。
		cs04HubRow(h0.Add(2*time.Minute), true, "outbound", model.JSONMap{
			"handler_type": "human", "scenario": "auto_reply",
		}),
		cs04HubRow(h0.Add(3*time.Minute), false, "outbound", nil),
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	svc := NewMessageHubSummaryAggregationService(repository.NewMessageHubSummaryRepository(database))
	if _, err := svc.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	var s model.MsgHourlySummary
	if err := database.First(&s).Error; err != nil {
		t.Fatalf("读汇总: %v", err)
	}
	if s.AICount != 1 {
		t.Errorf("AICount 应恰 1（公告不计 AI 应答），实际 %d", s.AICount)
	}
	if s.HumanCount != 1 {
		t.Errorf("HumanCount 应恰 1（公告也不冒充坐席应答），实际 %d", s.HumanCount)
	}
	if s.MessageCount != 3 {
		t.Errorf("MessageCount 应 3（公告仍计入消息总量），实际 %d", s.MessageCount)
	}
}

// TestCS04_AnnouncementNotCountedInRawFallback 驾驶舱 SSE 的 raw 兜底路径同一口径：
// summary 主路径（上面的 RunOnce）与 raw 聚合（QueryMessageVolumeRaw，summary 缺失/
// 过期时兜底）必须给出同样的 ai/human 计数——否则曲线在主/兜底切换点跳变。
// 公告行形态同上格：IsAIReply=true + Extra.handler_type=human。
func TestCS04_AnnouncementNotCountedInRawFallback(t *testing.T) {
	database := setupSummaryTestDB(t)
	ctx := context.Background()
	since := time.Now().Add(-2 * time.Hour)

	rows := []*model.MessageHub{
		cs04HubRow(since.Add(1*time.Minute), true, "outbound", model.JSONMap{
			"handler_type": "ai", "scenario": "auto_reply",
		}),
		cs04HubRow(since.Add(2*time.Minute), true, "outbound", model.JSONMap{
			"handler_type": "human", "scenario": "auto_reply",
		}),
		cs04HubRow(since.Add(3*time.Minute), false, "outbound", nil),
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	repo := repository.NewDashboardStatsRepository(database)
	got, err := repo.QueryMessageVolumeRaw(ctx, since)
	if err != nil {
		t.Fatalf("QueryMessageVolumeRaw: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("raw 聚合应 1 桶，实际 %d", len(got))
	}
	if got[0].AICount != 1 {
		t.Errorf("raw 路径 AICount 应恰 1（公告不计），实际 %d", got[0].AICount)
	}
	if got[0].HumanCount != 1 {
		t.Errorf("raw 路径 HumanCount 应恰 1，实际 %d", got[0].HumanCount)
	}
	if got[0].MessageCount != 3 {
		t.Errorf("raw 路径 MessageCount 应 3，实际 %d", got[0].MessageCount)
	}
}
