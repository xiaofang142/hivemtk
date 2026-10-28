package repository

import (
	"context"
	"testing"
	"time"

	sysmodel "hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
)

// 平均响应时长只该统计「用户 → 首条回复」的间隔。
// 旧口径取任意相邻消息差，把 AI 连发/用户连发/跨天回流都算进去，
// 实测 98 个会话被算成 3220 秒（53 分钟），页面数字失真。
func TestGetAvgResponseTime_OnlyUserToFirstReply(t *testing.T) {
	database := testutil.NewTestDB(t, &sysmodel.SessionMessage{})
	db.SetTestDB(database)
	t.Cleanup(func() { db.SetTestDB(nil) })

	base := time.Date(2020, 5, 1, 10, 0, 0, 0, time.UTC)
	at := func(sec int) time.Time { return base.Add(time.Duration(sec) * time.Second) }
	rows := []sysmodel.SessionMessage{
		// 会话 A：用户 → 2s 后 AI；AI 再连发（不该重复计入）；用户 → 5s 后人工。
		{SessionID: "s-a", SenderType: "user", CreatedAt: at(0)},
		{SessionID: "s-a", SenderType: "ai", CreatedAt: at(2)},
		{SessionID: "s-a", SenderType: "ai", CreatedAt: at(30)},
		{SessionID: "s-a", SenderType: "user", CreatedAt: at(100)},
		{SessionID: "s-a", SenderType: "agent", CreatedAt: at(105)},
		// 会话 B：用户 → 1s 后 AI。
		{SessionID: "s-b", SenderType: "user", CreatedAt: at(0)},
		{SessionID: "s-b", SenderType: "ai", CreatedAt: at(1)},
		// 会话 C：只有 AI，没有用户消息，不得计入。
		{SessionID: "s-c", SenderType: "ai", CreatedAt: at(0)},
	}
	for i := range rows {
		if err := database.Create(&rows[i]).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	repo := NewAIProductivityRepository()
	got, err := repo.GetAvgResponseTime(context.Background(), base.Add(-time.Minute), base.Add(time.Hour))
	if err != nil {
		t.Fatalf("GetAvgResponseTime: %v", err)
	}
	// (2 + 5 + 1) / 3 = 2.666…
	if got < 2.6 || got > 2.7 {
		t.Fatalf("平均响应时长应为 2.67s（仅用户→首条回复），实际 %.4f", got)
	}
}

// 没有用户消息时返回 0 而不是 NULL/NaN。
func TestGetAvgResponseTime_NoUserMessagesReturnsZero(t *testing.T) {
	database := testutil.NewTestDB(t, &sysmodel.SessionMessage{})
	db.SetTestDB(database)
	t.Cleanup(func() { db.SetTestDB(nil) })

	base := time.Date(2020, 6, 1, 10, 0, 0, 0, time.UTC)
	if err := database.Create(&sysmodel.SessionMessage{SessionID: "s-x", SenderType: "ai", CreatedAt: base}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	repo := NewAIProductivityRepository()
	got, err := repo.GetAvgResponseTime(context.Background(), base.Add(-time.Minute), base.Add(time.Hour))
	if err != nil {
		t.Fatalf("GetAvgResponseTime: %v", err)
	}
	if got != 0 {
		t.Fatalf("无用户消息应返回 0，实际 %.4f", got)
	}
}
