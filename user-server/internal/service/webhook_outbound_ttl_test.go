package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// TestNewWebhookService_ExpiresStaleDelayedOnBoot 锁住 TTL 的装配级接线：服务启动首轮
// 派发必须先于抢占判期——超 TTL(24h) 仍 pending 的旧回复直接 expired，不得进入重放；
// 未超期的到期行照常投递。仓储层 ExpireStale 单测只覆盖 SQL 语义，本测试看守的是
// "dispatch 真的先判期后取件"这一顺序。
//
// 反向测试：删掉 dispatchDueDelayedOutbound 顶部的 ExpireStale 调用 ⇒ stale 行会被
// 当作普通到期件重放并标 sent，本测试 status=expired 断言即红。
func TestNewWebhookService_ExpiresStaleDelayedOnBoot(t *testing.T) {
	database := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &DelayedOutboundReply{}, &model.InboxConversation{})

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	stale := &DelayedOutboundReply{
		Platform:       string(ChannelDouyin),
		AccountID:      "acct-ttl-" + suffix,
		ConversationID: "conv-ttl-stale-" + suffix,
		SenderID:       "sender-ttl-" + suffix,
		Content:        "超期 25h 的旧回复，不应补投 " + suffix,
		SendAt:         time.Now().Add(-25 * time.Hour),
		Status:         "pending",
	}
	fresh := &DelayedOutboundReply{
		Platform:       string(ChannelDouyin),
		AccountID:      "acct-ttl-" + suffix,
		ConversationID: "conv-ttl-fresh-" + suffix,
		SenderID:       "sender-ttl-" + suffix,
		Content:        "到期 1 分钟的正常回复，应照常投递 " + suffix,
		SendAt:         time.Now().Add(-time.Minute),
		Status:         "pending",
	}
	for _, rec := range []*DelayedOutboundReply{stale, fresh} {
		if err := database.Create(rec).Error; err != nil {
			t.Fatalf("预置延迟记录失败: %v", err)
		}
	}

	svc := NewWebhookService(database)
	defer svc.Stop(context.Background())

	var got model.DelayedOutboundReply
	if err := database.First(&got, stale.ID).Error; err != nil {
		t.Fatalf("回读 stale 记录失败: %v", err)
	}
	if got.Status != "expired" {
		t.Errorf("超 TTL 记录 status = %q, want expired ⇒ dispatch 未先判期", got.Status)
	}
	var probe model.MessageHub
	var cnt int64
	if err := database.Model(&probe).Where("direction = ? AND conversation_id = ?", "outbound", stale.ConversationID).
		Count(&cnt).Error; err != nil {
		t.Fatalf("统计 stale 出站行失败: %v", err)
	}
	if cnt != 0 {
		t.Errorf("stale 记录产生了 %d 条出站 ⇒ 判期未先于重放", cnt)
	}

	var freshAfter model.DelayedOutboundReply
	if err := database.First(&freshAfter, fresh.ID).Error; err != nil {
		t.Fatalf("回读 fresh 记录失败: %v", err)
	}
	if freshAfter.Status == "pending" || freshAfter.Status == "expired" {
		t.Errorf("未超期到期记录 status = %q, 应被正常抢占投递", freshAfter.Status)
	}
}
