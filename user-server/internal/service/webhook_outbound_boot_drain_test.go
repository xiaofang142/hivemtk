package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// TestNewWebhookService_DrainsDueDelayedOnBoot 锁住 T-P0-07 的接线：免打扰到期回复的
// 消费循环必须由**服务装配**起来，而不是等"下一次又命中免打扰入队"时才顺带启动 ——
// 那种惰性启动在进程重启后等于没有消费者，已到期的 AI 回复会永久搁置。
//
// 被测路径刻意只调用 NewWebhookService（生产装配入口），不直接调 dispatchDueDelayedOutbound，
// 否则测试就退化成对派发函数本身的覆盖，接线的洞依然无人看守。
// 反向测试：删掉 webhook.go 中的 s.startDelayedOutboundDispatch() ⇒ 本测试 10s 内等不到
// 出站行而 FAIL（ticker 亦未起来，无从兜底）。
func TestNewWebhookService_DrainsDueDelayedOnBoot(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &DelayedOutboundReply{}, &model.InboxConversation{})

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	account := "acct-boot-" + suffix
	conv := "conv-boot-" + suffix

	rec := &DelayedOutboundReply{
		Platform:       string(ChannelDouyin),
		AccountID:      account,
		ConversationID: conv,
		SenderID:       "sender-boot-" + suffix,
		Content:        "重启后应被投递的到期回复 " + suffix,
		SendAt:         time.Now().Add(-time.Minute),
		Status:         "pending",
	}
	if err := db.Create(rec).Error; err != nil {
		t.Fatalf("预置到期记录失败: %v", err)
	}

	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	deadline := time.Now().Add(10 * time.Second)
	var out model.MessageHub
	for {
		if err := db.Where("direction = ? AND conversation_id = ?", "outbound", conv).
			Order("id DESC").First(&out).Error; err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("服务装配后 10s 内到期回复未被投递 ⇒ 延迟出站队列消费者未在启动时注册")
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !out.IsAIReply {
		t.Errorf("重放落库行应保留 is_ai_reply=true，got %v", out.IsAIReply)
	}
	// 桥接渠道（douyin）重放只落库不外呼，扩展端凭 status=pending 从 outbox 领取
	if out.Status != "pending" {
		t.Errorf("重放落库行应为 pending 待领取，got %q", out.Status)
	}
	if out.ConversationID != conv || out.AccountID != account {
		t.Errorf("重放行归属不符: conv=%q account=%q", out.ConversationID, out.AccountID)
	}

	var after DelayedOutboundReply
	if err := db.First(&after, rec.ID).Error; err != nil {
		t.Fatalf("回读延迟记录失败: %v", err)
	}
	if after.Status == "pending" {
		t.Error("到期记录应已被抢占投递，status 不应仍为 pending")
	}
}
