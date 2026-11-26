package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func TestHandleIngress_CanonicalContentHash_AlreadyExists_Skipped(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressService()
	ctx := context.Background()

	const (
		platform = "xiaohongshu"
		account  = "acct-canonical-dedup"
		conv     = "conv-canonical-dedup-1"
		content  = "您好！HiveMtk 支持私有化部署，云服务器完全可以。"
	)

	algo2Hash := ContentHashMsgID(platform, conv, content)
	now := time.Now()
	if err := db.Create(&model.MessageHub{
		MsgID: algo2Hash, Platform: platform, AccountID: account, Direction: "outbound", Status: "delivered",
		MsgType: "text", ConversationID: conv, Content: content, SentAt: now,
	}).Error; err != nil {
		t.Fatalf("预置 outbound 失败: %v", err)
	}

	algo1Hash := "mh:0000aabb"
	evt := &model.MessageEvent{
		Channel:        model.ChannelXHS,
		SenderID:       "customer-xhs-100",
		SenderType:     "customer",
		Content:        content,
		EventID:        algo1Hash,
		ConversationID: conv,
		Extra:          map[string]interface{}{"account_id": account},
	}
	res, err := svc.HandleIngressMessage(ctx, evt)
	if err != nil {
		t.Fatalf("HandleIngressMessage 不应报错: %v", err)
	}
	if !res.Accepted {
		t.Fatalf("钩子2.5 命中：Accepted 应为 true（幂等跳过）")
	}
	if res.QueuedForAI {
		t.Fatalf("钩子2.5 命中：QueuedForAI 必须 false（不触发 AI）")
	}

	var sameContentCount int64
	db.Model(&model.MessageHub{}).
		Where("platform=? AND md5(content)=md5(?)", platform, content).
		Count(&sameContentCount)
	if sameContentCount != 1 {
		t.Fatalf("同 content 应仅 1 条（algo2 outbound），实际=%d", sameContentCount)
	}
}

func TestHandleIngress_CrossConv_SameInboundContent_NotSkipped(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressService()
	ctx := context.Background()

	const (
		platform = "xiaohongshu"
		account  = "acct-cross-conv"
		// 中性客户文本（2026-11-15 起平台公告由输入卫生层拦截，此处不得用公告文案）
		content = "你们支持哪些渠道接入？"
	)

	convA := "conv-cross-A"
	if err := db.Create(&model.MessageHub{
		MsgID: "mh:preexistingA", Platform: platform, AccountID: account, Direction: "inbound", Status: "pending",
		MsgType: "text", ConversationID: convA, Content: content, SentAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("预置 A 会话 inbound 失败: %v", err)
	}

	convB := "conv-cross-B"
	evt := &model.MessageEvent{
		Channel:        model.ChannelXHS,
		SenderID:       "customer-B",
		SenderType:     "customer",
		Content:        content,
		EventID:        "mh:crossconv0001",
		ConversationID: convB,
		Extra:          map[string]interface{}{"account_id": account},
	}
	res, err := svc.HandleIngressMessage(ctx, evt)
	if err != nil {
		t.Fatalf("HandleIngressMessage 不应报错: %v", err)
	}
	if !res.Accepted {
		t.Fatalf("跨会话同 content inbound 必须被接受（不应被误判为回显跳过）, Reason=%s", res.Reason)
	}
	if !res.QueuedForAI {
		t.Fatalf("跨会话客户消息应触发 AI, QueuedForAI=%v", res.QueuedForAI)
	}

	var row model.MessageHub
	if err := db.Where("msg_id=?", "mh:crossconv0001").First(&row).Error; err != nil {
		t.Fatalf("B 会话消息应入库: %v", err)
	}
	if row.ConversationID != convB {
		t.Fatalf("入库会话 ID 错误: got=%s want=%s", row.ConversationID, convB)
	}
	if row.Direction != "inbound" {
		t.Fatalf("入库方向应为 inbound: got=%s", row.Direction)
	}
}

func TestHandleIngress_CrossConv_SameAlgo2MsgID_NotSkipped(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressService()
	ctx := context.Background()

	const (
		platform = "xiaohongshu"
		account  = "acct-cross-conv-algo2"
		content  = "测试跨会话去重内容unique20260807"
	)
	algo2EventID := ContentHashMsgID(platform, "", content)

	convA := "conv-cross-algo2-A"
	if err := db.Create(&model.MessageHub{
		MsgID: algo2EventID, Platform: platform, AccountID: account, Direction: "inbound", Status: "pending",
		MsgType: "text", ConversationID: convA, Content: content, SentAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("预置 A 会话 inbound 失败: %v", err)
	}

	convB := "conv-cross-algo2-B"
	evt := &model.MessageEvent{
		Channel:        model.ChannelXHS,
		SenderID:       "customer-B",
		SenderType:     "customer",
		Content:        content,
		EventID:        algo2EventID,
		ConversationID: convB,
		Extra:          map[string]interface{}{"account_id": account, "content_hash": algo2EventID},
	}
	res, err := svc.HandleIngressMessage(ctx, evt)
	if err != nil {
		t.Fatalf("HandleIngressMessage 不应报错: %v", err)
	}
	if !res.Accepted {
		t.Fatalf("跨会话同 algo2 msg_id 必须被接受（不应被钩子2 误跳过）, Reason=%s", res.Reason)
	}

	var row model.MessageHub
	if err := db.Where("msg_id = ? AND conversation_id = ?", algo2EventID, convB).First(&row).Error; err != nil {
		t.Fatalf("B 会话消息应入库（钩子2 限本会话后不应跳过）: %v", err)
	}
	if row.ConversationID != convB {
		t.Fatalf("入库会话 ID 错误: got=%s want=%s", row.ConversationID, convB)
	}

	var rowA model.MessageHub
	if err := db.Where("msg_id = ? AND conversation_id = ?", algo2EventID, convA).First(&rowA).Error; err != nil {
		t.Fatalf("A 会话消息应仍在 DB: %v", err)
	}
}

// 对照（防过度跳过）：全新内容必须正常入库为 inbound（不要误拦真实新消息）。
func TestHandleIngress_NewContent_NotSkipped(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressService()
	ctx := context.Background()

	const (
		platform = "xiaohongshu"
		account  = "acct-new-content"
		conv     = "conv-new-content-1"
		content  = "这是真实客户发的全新消息"
	)
	evt := &model.MessageEvent{
		Channel:        model.ChannelXHS,
		SenderID:       "customer-xhs-200",
		SenderType:     "customer",
		Content:        content,
		EventID:        "mh:new0001",
		ConversationID: conv,
		Extra:          map[string]interface{}{"account_id": account},
	}
	res, err := svc.HandleIngressMessage(ctx, evt)
	if err != nil {
		t.Fatalf("HandleIngressMessage 不应报错: %v", err)
	}
	if !res.Accepted {
		t.Fatalf("全新内容应被接受，got Accepted=%v", res.Accepted)
	}
	if !res.QueuedForAI {
		t.Fatalf("全新 customer inbound 应触发 AI，got QueuedForAI=%v", res.QueuedForAI)
	}
	var row model.MessageHub
	if err := db.Where("msg_id=?", "mh:new0001").First(&row).Error; err != nil {
		t.Fatalf("全新内容应入库: %v", err)
	}
}

func TestPersistBridgeHistory_NormalizedDedup_DOMWhitespaceVariance_Skipped(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressService()
	ctx := context.Background()

	const (
		platform = "xiaohongshu"
		account  = "acct-persist-norm"
		conv     = "conv-persist-norm-1"
	)
	dbContent := "您好！😊 很高兴为您服务！\n\n- 🛍️ **产品推荐**：根据您的需求推荐合适"
	domContent := "您好！😊 很高兴为您服务！ - 🛍️ **产品推荐**：根据您的需求推荐合适"

	algo2Hash := ContentHashMsgID(platform, conv, dbContent)
	now := time.Now()
	if err := db.Create(&model.MessageHub{
		MsgID: algo2Hash, Platform: platform, AccountID: account, Direction: "outbound", Status: "delivered",
		MsgType: "text", ConversationID: conv, Content: dbContent, SentAt: now,
	}).Error; err != nil {
		t.Fatalf("预置 outbound 失败: %v", err)
	}

	domMsgID := ContentHashMsgID(platform, conv, domContent)
	evt := &model.MessageEvent{
		Channel:        model.ChannelXHS,
		SenderID:       "customer-persist-norm",
		SenderType:     "customer",
		Content:        domContent,
		EventID:        domMsgID,
		ConversationID: conv,
		Extra:          map[string]interface{}{"account_id": account},
	}

	if err := svc.PersistBridgeHistory(ctx, evt, "inbound"); err != nil {
		t.Fatalf("PersistBridgeHistory 不应报错: %v", err)
	}

	var count int64
	db.Model(&model.MessageHub{}).
		Where("platform=? AND direction='inbound'", platform).
		Count(&count)
	if count != 0 {
		t.Fatalf("PersistBridgeHistory 归一化去重应跳过，不得新增 inbound，实际 inbound=%d", count)
	}

	var outCount int64
	db.Model(&model.MessageHub{}).
		Where("platform=? AND direction='outbound'", platform).
		Count(&outCount)
	if outCount != 1 {
		t.Fatalf("原 outbound 应仅 1 条，实际=%d", outCount)
	}
}

func TestPersistBridgeHistory_NewContent_Persisted(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressService()
	ctx := context.Background()

	const (
		platform = "xiaohongshu"
		account  = "acct-persist-new"
		conv     = "conv-persist-new-1"
		content  = "这是 PersistBridgeHistory 路径的全新客户消息"
	)
	evt := &model.MessageEvent{
		Channel:        model.ChannelXHS,
		SenderID:       "customer-persist-new",
		SenderType:     "customer",
		Content:        content,
		EventID:        "mh:persist-new-0001",
		ConversationID: conv,
		Extra:          map[string]interface{}{"account_id": account},
	}
	if err := svc.PersistBridgeHistory(ctx, evt, "inbound"); err != nil {
		t.Fatalf("PersistBridgeHistory 全新内容应入库: %v", err)
	}
	var row model.MessageHub
	if err := db.Where("msg_id=?", "mh:persist-new-0001").First(&row).Error; err != nil {
		t.Fatalf("全新内容应入库: %v", err)
	}
	if row.Direction != "inbound" {
		t.Fatalf("direction 应为 inbound，got=%s", row.Direction)
	}
}
