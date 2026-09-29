package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// TestHandleIngress_HumanLock_StillIdempotentByMsgID 守护「人工锁定不得跳过 msg_id 幂等」这条顺序。
// 人工接管中的会话恰恰是最容易把同一条内容重复灌进来的状态（运营在平台回话、渠道把回声再推一遍），
// 而锁定判定一旦排在幂等判定之前，这条回声就会被当成新消息再落一行 —— bridge 端到端仿真的
// "msg_id 回环" 用例就是这么漏判的（快手那一轮 accepted=true / duplicate=false）。
func TestHandleIngress_HumanLock_StillIdempotentByMsgID(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	const (
		platform = "douyin"
		account  = "acct-lock-dedup"
		conv     = "conv-lock-dedup-1"
		session  = "sess-lock-dedup-1"
		content  = "您好，HiveMtk 支持私有化部署，云服务器完全可以。"
	)
	msgID := "mh:0000lockdedup"

	if err := db.Create(&model.MessageHub{
		MsgID: msgID, Platform: platform, AccountID: account, Direction: "inbound",
		Status: "delivered", MsgType: "text", ConversationID: conv, Content: content, SentAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("预置已存在的 msg_id 失败: %v", err)
	}
	if err := svc.LockSessionForHuman(ctx, session, "回归测试：运营接管"); err != nil {
		t.Fatalf("置人工锁失败: %v", err)
	}

	evt := &model.MessageEvent{
		Channel:        model.ChannelDouyin,
		SenderID:       "customer-lock-dedup",
		SenderType:     "customer",
		Content:        content,
		EventID:        msgID,
		SessionID:      session,
		ConversationID: conv,
		Extra:          map[string]interface{}{"account_id": account},
	}
	res, err := svc.HandleIngressMessage(ctx, evt)
	if err != nil {
		t.Fatalf("HandleIngressMessage 报错: %v", err)
	}
	if res.Reason != "msg_id already exists in DB; idempotent skip" {
		t.Fatalf("人工锁定不得抢在幂等判定之前，实际 reason=%q humanLocked=%v", res.Reason, res.HumanLocked)
	}

	var rows int64
	db.Model(&model.MessageHub{}).Where("msg_id = ?", msgID).Count(&rows)
	if rows != 1 {
		t.Fatalf("重复灌入被当成新消息落库：msg_id=%s 现有 %d 行（期望 1）", msgID, rows)
	}
}

// TestHandleIngress_HumanLock_NewMessagePersistsWithoutAI 保住反方向：锁定会话的**新**消息
// 仍要落库、但不穿透 AI。幂等判定前移不能把这条既有语义一起改掉。
func TestHandleIngress_HumanLock_NewMessagePersistsWithoutAI(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	const (
		platform = "douyin"
		account  = "acct-lock-newmsg"
		conv     = "conv-lock-newmsg-1"
		session  = "sess-lock-newmsg-1"
		content  = "在吗？我想问下售后政策（lock-newmsg 唯一内容）"
	)
	if err := svc.LockSessionForHuman(ctx, session, "回归测试：运营接管"); err != nil {
		t.Fatalf("置人工锁失败: %v", err)
	}

	evt := &model.MessageEvent{
		Channel:        model.ChannelDouyin,
		SenderID:       "customer-lock-newmsg",
		SenderType:     "customer",
		Content:        content,
		EventID:        "mh:0000locknewmsg",
		SessionID:      session,
		ConversationID: conv,
		Extra:          map[string]interface{}{"account_id": account},
	}
	res, err := svc.HandleIngressMessage(ctx, evt)
	if err != nil {
		t.Fatalf("HandleIngressMessage 报错: %v", err)
	}
	if !res.HumanLocked || !res.Accepted || res.QueuedForAI {
		t.Fatalf("锁定会话的新消息应落库且不触发 AI，实际 %+v", res)
	}
	var rows int64
	db.Model(&model.MessageHub{}).Where("conversation_id = ? AND content = ?", conv, content).Count(&rows)
	if rows != 1 {
		t.Fatalf("新消息必须落库一行，实际 %d 行", rows)
	}
}

// TestHandleIngressBatch_HumanLock_StillIdempotentByMsgID 同一条顺序在批次入口的那一份复制
// （handleIngressSingleForBatch 是独立的一份实现，改一处不会带动另一处）：渠道一次推多条时
// 走的是这条路径，锁定判定同样不得抢在 msg_id 幂等之前。
func TestHandleIngressBatch_HumanLock_StillIdempotentByMsgID(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	const (
		platform = "douyin"
		account  = "acct-lock-dedup-batch"
		conv     = "conv-lock-dedup-batch-1"
		session  = "sess-lock-dedup-batch-1"
		content  = "您好，HiveMtk 支持私有化部署，云服务器完全可以。（批次）"
	)
	msgID := "mh:0000lockdedupb"

	if err := db.Create(&model.MessageHub{
		MsgID: msgID, Platform: platform, AccountID: account, Direction: "inbound",
		Status: "delivered", MsgType: "text", ConversationID: conv, Content: content, SentAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("预置已存在的 msg_id 失败: %v", err)
	}
	if err := svc.LockSessionForHuman(ctx, session, "回归测试：运营接管"); err != nil {
		t.Fatalf("置人工锁失败: %v", err)
	}

	evt := &model.MessageEvent{
		Channel:        model.ChannelDouyin,
		SenderID:       "customer-lock-dedup-batch",
		SenderType:     "customer",
		Content:        content,
		EventID:        msgID,
		SessionID:      session,
		ConversationID: conv,
		Extra:          map[string]interface{}{"account_id": account},
	}
	batch, err := svc.HandleIngressBatch(ctx, []*model.MessageEvent{evt})
	if err != nil {
		t.Fatalf("HandleIngressBatch 报错: %v", err)
	}
	per := batch.PerEvent[0]
	if per.Reason != "msg_id already exists in DB; idempotent skip" {
		t.Fatalf("批次路径的人工锁定不得抢在幂等判定之前，实际 reason=%q humanLocked=%v",
			per.Reason, per.HumanLocked)
	}

	var rows int64
	db.Model(&model.MessageHub{}).Where("msg_id = ?", msgID).Count(&rows)
	if rows != 1 {
		t.Fatalf("批次里重复灌入被当成新消息落库：msg_id=%s 现有 %d 行（期望 1）", msgID, rows)
	}
}
