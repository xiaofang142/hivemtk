package repository

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func TestSweepProbeProdShape(t *testing.T) {
	if testing.Short() {
		t.Skip("需要测试库")
	}
	database := testutil.NewTestDB(t, &model.MessageHub{})
	if database == nil {
		t.Fatal("测试库不可达")
	}
	ctx := context.Background()
	repo := NewMessageHubRepositoryWithDB(database)
	t.Cleanup(func() { database.Where("conversation_id = ?", "sweep-probe-conv").Delete(&model.MessageHub{}) })

	row := model.MessageHub{
		TenantID: 1, Platform: "xiaohongshu", AccountID: "sweep-probe-acc", Direction: "outbound",
		Status: "pending", MsgType: "text", SenderID: "sweep-probe-acc", ReceiverID: "p",
		Content: "生产形态复刻", ConversationID: "sweep-probe-conv",
		MsgID: "sweep-probe-1", PushAttempts: 1,
		Extra: model.JSONMap{"agent_id": "unknown", "dm_target": "conv", "scenario": "auto_reply", "triggered_by": "ai_dispatch"},
	}
	old := time.Now().Add(-30 * 24 * time.Hour)
	row.CreatedAt = old
	row.UpdatedAt = old
	if err := database.Create(&row).Error; err != nil {
		t.Fatalf("造样失败: %v", err)
	}
	// 核对 created_at 真的写成了 30 天前（GORM autoCreateTime 覆盖疑点）
	var back model.MessageHub
	database.First(&back, row.ID)
	t.Logf("created_at 回读=%s（期望 30 天前=%s）", back.CreatedAt, old)

	reclaimed, exhausted, aged, err := repo.ReclaimOutboundSweep(ctx, 30*time.Second, 7*24*time.Hour)
	t.Logf("reclaimed=%d exhausted=%d aged=%d err=%v", reclaimed, exhausted, aged, err)
	var after model.MessageHub
	database.First(&after, row.ID)
	t.Logf("终态=%s push_error=%q", after.Status, after.PushError)
}
