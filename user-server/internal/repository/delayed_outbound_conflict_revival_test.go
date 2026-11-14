package repository

// CreateMessageHubIdempotent 冲突复活语义（2026-11-14 生产终局修复回归钉）。
//
// 事故：AI 对同一问题确定性输出同一话术 → 同键历史行（failed/软删/delivered）占位
// 唯一索引 → DO NOTHING 把新生成的回复静默吞掉（19:10/19:12/19:14/19:19 四次
// id=0，客户「给我一份功能清单」两次追问石沉大海）。
// 契约：冲突行 failed/软删 → 复活为新 pending（同 id 回填）；delivered/pending/
// inflight 占位 → DO NOTHING（防同文案刷屏，上层走人工跟进旁路）。

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupConflictRevivalDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t, &model.MessageHub{})
	db.SetTestDB(database)
	return database
}

func TestCreateMessageHubIdempotent_ConflictRevival(t *testing.T) {
	g := setupConflictRevivalDB(t)
	r := &DelayedOutboundRepository{db: g}
	ctx := context.Background()

	mk := func(msgID, status string, deleted bool) *model.MessageHub {
		row := &model.MessageHub{
			TenantID: 18, Platform: "douyin", MsgID: msgID, ConversationID: "conv:rev",
			Direction: "outbound", Status: status, MsgType: "text",
			SenderID: "acc", ReceiverID: "cust", Content: "旧话术",
			IsAIReply: true, IsRead: true, SentAt: time.Now().Add(-time.Hour),
			ClaimedAt: nil, PushAttempts: 3, PushError: "旧错误",
		}
		if deleted {
			g.Create(row)  // 先落库再软删（写 deleted_at）
			g.Delete(row)
		} else {
			g.Create(row)
		}
		return row
	}

	// ① failed 占位 → 复活为 pending（同 id），内容/错误位刷新
	seed := mk("mh:rev-failed", "failed", false)
	newMsg := &model.MessageHub{
		TenantID: 18, Platform: "douyin", MsgID: "mh:rev-failed", ConversationID: "conv:rev",
		Direction: "outbound", Status: "pending", MsgType: "text",
		SenderID: "acc", ReceiverID: "cust", Content: "新生成的回复",
		IsAIReply: true, IsRead: true, SentAt: time.Now(),
	}
	got, err := r.CreateMessageHubIdempotent(ctx, newMsg)
	if err != nil {
		t.Fatalf("failed 占位复活应成功: %v", err)
	}
	if got.ID != seed.ID {
		t.Fatalf("复活应命中同一行（id=%d），实际 id=%d", seed.ID, got.ID)
	}
	var row model.MessageHub
	g.First(&row, seed.ID)
	if row.Status != "pending" || row.Content != "新生成的回复" || row.PushAttempts != 0 || row.PushError != "" {
		t.Fatalf("复活后应 pending/新内容/预算清零，实际 %+v", row)
	}

	// ② 软删占位 → 复活（deleted_at 清空、回到待投）
	deleted := mk("mh:rev-deleted", "pending", true)
	newMsg2 := &model.MessageHub{
		TenantID: 18, Platform: "douyin", MsgID: "mh:rev-deleted", ConversationID: "conv:rev",
		Direction: "outbound", Status: "pending", MsgType: "text",
		SenderID: "acc", ReceiverID: "cust", Content: "再次生成的回复",
		IsAIReply: true, IsRead: true, SentAt: time.Now(),
	}
	if _, err := r.CreateMessageHubIdempotent(ctx, newMsg2); err != nil {
		t.Fatalf("软删占位复活应成功: %v", err)
	}
	var row2 model.MessageHub
	g.First(&row2, deleted.ID)
	if row2.DeletedAt.Valid {
		t.Fatalf("复活后 deleted_at 应清空")
	}
	if row2.Content != "再次生成的回复" {
		t.Fatalf("复活应写入新内容，实际 %q", row2.Content)
	}

	// ③ delivered 占位 → DO NOTHING（防同文案刷屏），新行 id=0
	g.Create(&model.MessageHub{
		TenantID: 18, Platform: "douyin", MsgID: "mh:rev-delivered", ConversationID: "conv:rev",
		Direction: "outbound", Status: "delivered", MsgType: "text",
		SenderID: "acc", ReceiverID: "cust", Content: "已送达的同话术",
		IsAIReply: true, IsRead: true, SentAt: time.Now(),
	})
	newMsg3 := &model.MessageHub{
		TenantID: 18, Platform: "douyin", MsgID: "mh:rev-delivered", ConversationID: "conv:rev",
		Direction: "outbound", Status: "pending", MsgType: "text",
		SenderID: "acc", ReceiverID: "cust", Content: "重复生成的同话术",
		IsAIReply: true, IsRead: true, SentAt: time.Now(),
	}
	got3, err := r.CreateMessageHubIdempotent(ctx, newMsg3)
	if err != nil {
		t.Fatalf("delivered 冲突应无错（DO NOTHING）: %v", err)
	}
	if got3.ID != 0 {
		t.Fatalf("delivered 占位应 DO NOTHING（id=0 交上层人工旁路），实际 id=%d", got3.ID)
	}
	var row3 model.MessageHub
	g.Where("msg_id = ? AND status = 'delivered'", "mh:rev-delivered").First(&row3)
	if row3.Content != "已送达的同话术" {
		t.Fatalf("delivered 占位内容不应被改写，实际 %q", row3.Content)
	}
}
