package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// retryableSendErr 与真实断网错误同形（分类器按 "connection refused" 判 network/可重试）。
func retryableSendErr() error {
	return errors.New("send tg msg: Post https://api.telegram.org/bot:x/sendMessage: dial tcp: connection refused")
}

// nonRetryableSendErr 授权类失败：重试只会把 token 再撞墙一次，必须直接落终态。
func nonRetryableSendErr() error {
	return errors.New("send tg msg: Bad Request: status 403 Forbidden: bot is not a member")
}

// TestEnqueueSendRetry_OnlyRetryableEnqueues 只有可重试失败才进持久化队列。
func TestEnqueueSendRetry_OnlyRetryableEnqueues(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &DelayedOutboundReply{})
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	p := &ParsedPayload{EventID: "evt-retry-1", Sender: "sender-retry"}
	hub := &model.MessageHub{ConversationID: "8608488936"}

	svc.enqueueSendRetry(context.Background(), ChannelTelegram, "5", p, "被断网挡住的回复", hub, nil, retryableSendErr())

	var rec DelayedOutboundReply
	if err := db.Order("id ASC").First(&rec).Error; err != nil {
		t.Fatalf("可重试失败应落一条待重投记录: %v", err)
	}
	if rec.Kind != model.DelayedKindSendRetry {
		t.Errorf("kind = %q, want %q", rec.Kind, model.DelayedKindSendRetry)
	}
	if rec.Status != model.DelayedStatusPending {
		t.Errorf("status = %q, want pending", rec.Status)
	}
	if rec.Attempts != 0 {
		t.Errorf("首次入队 attempts = %d, want 0", rec.Attempts)
	}
	if !strings.Contains(rec.LastError, "connection refused") {
		t.Errorf("last_error 应留下失败原因，got %q", rec.LastError)
	}
	if rec.ConversationID != "8608488936" || rec.Content != "被断网挡住的回复" || rec.AccountID != "5" {
		t.Errorf("字段不符: %+v", rec)
	}
	if d := time.Until(rec.SendAt); d < 50*time.Second || d > 70*time.Second {
		t.Errorf("首次重投应退避约 60s，实际间隔 %s", d)
	}

	var cnt int64
	if err := db.Model(&DelayedOutboundReply{}).Count(&cnt).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("可重试失败应只入队 1 条，got %d", cnt)
	}

	// 反向对照：不可重试失败不得入队（否则授权失效的账号会攒出一堆永远投不出去的重投任务）。
	svc.enqueueSendRetry(context.Background(), ChannelTelegram, "5", p, "不该重投的回复", hub, nil, nonRetryableSendErr())
	if err := db.Model(&DelayedOutboundReply{}).Count(&cnt).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("不可重试失败不应入队，实际队列 %d 条", cnt)
	}
}

// TestNextSendRetryAt_BackoffAndQuietHours 退避阶梯与免打扰顺延。
func TestNextSendRetryAt_BackoffAndQuietHours(t *testing.T) {
	now := time.Date(2026, 9, 19, 15, 0, 0, 0, cstZone)
	cases := []struct {
		attempts int
		want     time.Duration
	}{
		{0, 60 * time.Second},
		{1, 2 * time.Minute},
		{2, 4 * time.Minute},
		{7, 4 * time.Minute}, // 超出表长按最后一档封顶，不能越退越快
	}
	for _, c := range cases {
		if got := nextSendRetryAt(now, c.attempts).Sub(now); got != c.want {
			t.Errorf("attempts=%d 退避 = %s, want %s", c.attempts, got, c.want)
		}
	}

	orig := aiReplyQuietHoursFn
	aiReplyQuietHoursFn = func(time.Time) bool { return true }
	defer func() { aiReplyQuietHoursFn = orig }()

	at := nextSendRetryAt(now, 0)
	if at.In(cstZone).Hour() != aiReplyQuietEndHour || at.In(cstZone).Minute() != 0 {
		t.Errorf("退避点落在免打扰时段时应顺延到 %02d:00，got %s", aiReplyQuietEndHour, at)
	}
}

// TestSendOutbound_RetryableFailureEntersRetryLane 实时投递失败 → 落队列，而不是只留日志。
func TestSendOutbound_RetryableFailureEntersRetryLane(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &DelayedOutboundReply{})
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())
	releaseReplyClaimForTest(t, "evt-retry-live")

	const conv = "8608488941"
	hub := &model.MessageHub{
		MsgID: "mh:in-retry-live", Platform: "telegram", AccountID: "5",
		Direction: "inbound", MsgType: "text", SenderID: "8608488941",
		Content: "在吗", ConversationID: conv, SentAt: time.Now(),
	}
	if err := db.Create(hub).Error; err != nil {
		t.Fatalf("seed inbound: %v", err)
	}
	p := &ParsedPayload{EventID: "evt-retry-live", Sender: "8608488941", Content: "在吗", ChatID: conv}

	// 账号 5 在测试库里不存在 → SendMessageEx 失败（分类 unknown ⇒ 可重试）。
	sent, sendErr := svc.sendOutbound(context.Background(), ChannelTelegram, "5", p, "AI 回复（投递失败）", hub, nil)
	if sent {
		t.Fatal("账号不存在的投递不该判成功")
	}
	if sendErr == nil {
		t.Fatal("sendOutbound 必须把渠道真实错误回传，否则重试通道无从判断")
	}

	var rec DelayedOutboundReply
	if err := db.Where("kind = ?", model.DelayedKindSendRetry).First(&rec).Error; err != nil {
		t.Fatalf("实时失败应转入持久化重试队列: %v", err)
	}
	if rec.ConversationID != conv || rec.Content != "AI 回复（投递失败）" {
		t.Errorf("重投记录内容/会话不符: %+v", rec)
	}

	// 反向对照：走通的路径（桥接渠道落库成功）不得顺手也入一条重投队列。
	releaseReplyClaimForTest(t, "evt-retry-ok")
	hub2 := &model.MessageHub{
		MsgID: "mh:in-retry-ok", Platform: "douyin", AccountID: "acct-retry-ok",
		Direction: "inbound", MsgType: "text", SenderID: "cust-retry-ok",
		Content: "在吗", ConversationID: "conv-retry-ok", SentAt: time.Now(),
	}
	if err := db.Create(hub2).Error; err != nil {
		t.Fatalf("seed inbound 2: %v", err)
	}
	p2 := &ParsedPayload{EventID: "evt-retry-ok", Sender: "cust-retry-ok", Content: "在吗", ChatID: "conv-retry-ok"}
	sent2, sendErr2 := svc.sendOutbound(context.Background(), ChannelDouyin, "acct-retry-ok", p2, "AI 回复（成功）", hub2, nil)
	if !sent2 || sendErr2 != nil {
		t.Fatalf("桥接渠道应判成功且无错误，got sent=%v err=%v", sent2, sendErr2)
	}
	var retryCnt int64
	if err := db.Model(&DelayedOutboundReply{}).Where("kind = ?", model.DelayedKindSendRetry).Count(&retryCnt).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if retryCnt != 1 {
		t.Fatalf("成功路径不得入重试队列，实际队列 %d 条", retryCnt)
	}
}

// TestReplayDelayedOutbound_Outcomes 重放三种结局：成功落 sent、仍可重试则退避改排、
// 次数用尽落 failed。免打扰首发记录维持一次性语义（失败也按 sent 收口）。
func TestReplayDelayedOutbound_Outcomes(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &DelayedOutboundReply{})
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	// SendAt 全部放未来：避免 30s 轮询的后台派发器与本用例抢占同一批行。
	future := time.Now().Add(time.Hour)

	// 主键必须显式指定且足够大：重放走 sendOutbound 的幂等键是 "delayed-<rec.ID>"，
	// 而 reply_guard 认领表是进程级共享状态（内存 map 或 Redis，TTL 10 分钟）。
	// 测试库每个用例重建后自增 id 又从 1 开始，于是撞上同包更早用例留下的 delayed-1
	// 认领时，sendOutbound 直接按「已回复」跳过并返回 (false, nil)，本用例就被误判成
	// 「放弃重投」（全量跑红、单跑绿）。用时间戳派生的大主键让键在本用例内唯一。
	baseID := uint(800_000_000 + time.Now().UnixNano()%19_000_000)

	t.Run("重投仍失败则原地退避改排", func(t *testing.T) {
		rec := &DelayedOutboundReply{
			ID:       baseID,
			Platform: string(ChannelTelegram), AccountID: "5", ConversationID: "8608488951",
			SenderID: "8608488951", Content: "重投还是失败",
			SendAt: future, Status: model.DelayedStatusPending,
			Kind: model.DelayedKindSendRetry,
		}
		if err := db.Create(rec).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
		svc.replayDelayedOutbound(context.Background(), rec)

		var got DelayedOutboundReply
		if err := db.First(&got, rec.ID).Error; err != nil {
			t.Fatalf("read back: %v", err)
		}
		if got.Status != model.DelayedStatusPending {
			t.Fatalf("可重试失败应回到 pending 等待下一次，got %q", got.Status)
		}
		if got.Attempts != 1 {
			t.Errorf("attempts 应累加为 1，got %d", got.Attempts)
		}
		if !time.Now().Before(got.SendAt) {
			t.Errorf("下一次 send_at 应在未来，got %s", got.SendAt)
		}
		if got.LastError == "" {
			t.Error("last_error 应留下本次失败原因")
		}
		var rows int64
		if err := db.Model(&DelayedOutboundReply{}).Where("conversation_id = ?", rec.ConversationID).Count(&rows).Error; err != nil {
			t.Fatalf("count: %v", err)
		}
		if rows != 1 {
			t.Errorf("重投必须原地改排、不新建行，实际 %d 行", rows)
		}
	})

	t.Run("次数用尽落 failed", func(t *testing.T) {
		rec := &DelayedOutboundReply{
			ID:       baseID + 1,
			Platform: string(ChannelTelegram), AccountID: "5", ConversationID: "8608488952",
			SenderID: "8608488952", Content: "再也投不出去",
			SendAt: future, Status: model.DelayedStatusPending,
			Kind: model.DelayedKindSendRetry, Attempts: sendRetryMaxAttempts,
		}
		if err := db.Create(rec).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
		svc.replayDelayedOutbound(context.Background(), rec)

		var got DelayedOutboundReply
		if err := db.First(&got, rec.ID).Error; err != nil {
			t.Fatalf("read back: %v", err)
		}
		if got.Status != model.DelayedStatusFailed {
			t.Fatalf("attempts=%d 时应判 failed，got %q", rec.Attempts, got.Status)
		}
		if !strings.Contains(got.LastError, "重投次数用尽") {
			t.Errorf("last_error 应说明放弃原因，got %q", got.LastError)
		}
		var outbound int64
		if err := db.Model(&model.MessageHub{}).Where("conversation_id = ? AND direction = ?", rec.ConversationID, "outbound").
			Count(&outbound).Error; err != nil {
			t.Fatalf("count: %v", err)
		}
		if outbound != 0 {
			t.Errorf("次数用尽后不得再发起投递，实际出站 %d 行", outbound)
		}
	})

	t.Run("会话已被回复则旧出站作废", func(t *testing.T) {
		const conv = "8608488953"
		if err := db.Create(&model.MessageHub{
			MsgID: "mh:in-sup", Platform: "telegram", AccountID: "5", Direction: "inbound",
			MsgType: "text", SenderID: conv, Content: "在吗", ConversationID: conv,
			SentAt: time.Now().Add(-3 * time.Minute),
		}).Error; err != nil {
			t.Fatalf("seed inbound: %v", err)
		}
		if err := db.Create(&model.MessageHub{
			MsgID: "mh:human-sup", Platform: "telegram", AccountID: "5", Direction: "outbound",
			MsgType: "text", SenderID: "5", ReceiverID: conv, Content: "坐席已回复",
			ConversationID: conv, SentAt: time.Now().Add(-time.Minute),
		}).Error; err != nil {
			t.Fatalf("seed outbound: %v", err)
		}
		rec := &DelayedOutboundReply{
			ID:       baseID + 2,
			Platform: string(ChannelTelegram), AccountID: "5", ConversationID: conv,
			SenderID: conv, Content: "等待期间的旧 AI 回复",
			SendAt: future, Status: model.DelayedStatusPending,
			Kind: model.DelayedKindSendRetry,
		}
		if err := db.Create(rec).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
		svc.replayDelayedOutbound(context.Background(), rec)

		var got DelayedOutboundReply
		if err := db.First(&got, rec.ID).Error; err != nil {
			t.Fatalf("read back: %v", err)
		}
		if got.Status != model.DelayedStatusSuperseded {
			t.Fatalf("会话已被回复时应判 superseded，got %q", got.Status)
		}
		var after int64
		if err := db.Model(&model.MessageHub{}).Where("conversation_id = ? AND content = ?", conv, "等待期间的旧 AI 回复").
			Count(&after).Error; err != nil {
			t.Fatalf("count: %v", err)
		}
		if after != 0 {
			t.Error("作废的出站不得再投给真实客户")
		}
	})

	t.Run("免打扰首发保持一次性投递", func(t *testing.T) {
		rec := &DelayedOutboundReply{
			ID:       baseID + 3,
			Platform: string(ChannelTelegram), AccountID: "5", ConversationID: "8608488954",
			SenderID: "8608488954", Content: "夜间首发失败",
			SendAt: future, Status: model.DelayedStatusPending,
			Kind: model.DelayedKindQuietHours,
		}
		if err := db.Create(rec).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
		svc.replayDelayedOutbound(context.Background(), rec)

		var got DelayedOutboundReply
		if err := db.First(&got, rec.ID).Error; err != nil {
			t.Fatalf("read back: %v", err)
		}
		if got.Status != model.DelayedStatusSent {
			t.Fatalf("quiet hours 首发应维持原有一次性收口(sent)，got %q", got.Status)
		}
		if got.Attempts != 0 {
			t.Errorf("quiet hours 首发不该占用重试次数，got attempts=%d", got.Attempts)
		}
	})
}
