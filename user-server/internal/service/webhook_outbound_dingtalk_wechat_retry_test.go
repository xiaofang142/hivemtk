package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// quietHoursOffForTest 免打扰时段（aiReplyQuietStartHour–aiReplyQuietEndHour）内
// sendOutbound 会先走「延后首发」分支直接 return，渠道真实投递根本不会执行。
// 凡断言「投递失败 → 进重试队列」的用例都必须显式关掉这个开关，
// 否则用例结论随挂钟时间与 DISABLE_AI_QUIET_HOURS 环境变量漂移。
func quietHoursOffForTest(t *testing.T) {
	t.Helper()
	orig := aiReplyQuietHoursFn
	aiReplyQuietHoursFn = func(time.Time) bool { return false }
	t.Cleanup(func() { aiReplyQuietHoursFn = orig })
}

// seedInboundHub 落一条入站 hub 记录，返回可直接用于 sendOutbound 的会话上下文。
func seedInboundHub(t *testing.T, db *gorm.DB, platform, accountID, conv, eventID string) *model.MessageHub {
	t.Helper()
	hub := &model.MessageHub{
		MsgID: "mh:in-" + eventID, Platform: platform, AccountID: accountID,
		Direction: "inbound", MsgType: "text", SenderID: conv,
		Content: "在吗", ConversationID: conv, SentAt: time.Now(),
	}
	if err := db.Create(hub).Error; err != nil {
		t.Fatalf("seed inbound hub: %v", err)
	}
	return hub
}

// TestSendOutbound_DingTalk_PlatformRejectionEntersRetryLane 钉钉 sessionWebhook 回 errcode!=0
// 只记日志的话，这条 AI 回复就永远停在「已发出」的假象里：必须回传错误并进入持久化重试通道。
func TestSendOutbound_DingTalk_PlatformRejectionEntersRetryLane(t *testing.T) {
	quietHoursOffForTest(t)
	allowAnyDingtalkHostForTest(t)
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &DelayedOutboundReply{})
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	const eventID = "evt-dt-retry-lane"
	releaseReplyClaimForTest(t, eventID)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"errcode":310000,"errmsg":"keyword not in content"}`))
	}))
	defer srv.Close()

	const conv = "dt-conv-retry-lane"
	hub := seedInboundHub(t, db, "dingtalk", "7", conv, eventID)
	hub.Extra = model.JSONMap{
		"session_webhook":            srv.URL,
		"session_webhook_expired_at": time.Now().Add(30 * time.Minute).UnixMilli(),
	}
	p := &ParsedPayload{EventID: eventID, Sender: conv, Content: "在吗", ChatID: conv}

	sent, sendErr := svc.sendOutbound(context.Background(), ChannelDingTalk, "7", p, "被平台拒绝的回复", hub, nil)
	if sent {
		t.Fatal("errcode!=0 不得判为已送达")
	}
	if sendErr == nil {
		t.Fatal("钉钉平台拒绝必须回传错误，否则持久化重试通道永不触发")
	}
	var rec DelayedOutboundReply
	if err := db.Where("kind = ? AND conversation_id = ?", model.DelayedKindSendRetry, conv).First(&rec).Error; err != nil {
		t.Fatalf("可重试的钉钉投递失败应进入重试队列: %v", err)
	}
	if rec.Content != "被平台拒绝的回复" {
		t.Errorf("重投内容应一致，got %q", rec.Content)
	}
}

// TestSendOutbound_DingTalk_TransportFailureEntersRetryLane 传输层失败（连接被拒）同样要回传错误。
func TestSendOutbound_DingTalk_TransportFailureEntersRetryLane(t *testing.T) {
	quietHoursOffForTest(t)
	allowAnyDingtalkHostForTest(t)
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &DelayedOutboundReply{})
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	const eventID = "evt-dt-transport-lane"
	releaseReplyClaimForTest(t, eventID)

	// 起一个立刻关闭的服务器，拿到一个必然拒绝连接的端口。
	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := closed.URL
	closed.Close()

	const conv = "dt-conv-transport-lane"
	hub := seedInboundHub(t, db, "dingtalk", "7", conv, eventID)
	hub.Extra = model.JSONMap{
		"session_webhook":            deadURL,
		"session_webhook_expired_at": time.Now().Add(30 * time.Minute).UnixMilli(),
	}
	p := &ParsedPayload{EventID: eventID, Sender: conv, Content: "在吗", ChatID: conv}

	sent, sendErr := svc.sendOutbound(context.Background(), ChannelDingTalk, "7", p, "断网时的回复", hub, nil)
	if sent || sendErr == nil {
		t.Fatalf("连接失败应回传错误，got sent=%v err=%v", sent, sendErr)
	}
	var cnt int64
	if err := db.Model(&DelayedOutboundReply{}).Where("kind = ? AND conversation_id = ?", model.DelayedKindSendRetry, conv).
		Count(&cnt).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("传输失败应落 1 条重投记录，实际 %d", cnt)
	}
}

// TestSendOutbound_DingTalk_MissingSessionWebhookStaysNonRetryable 缺 sessionWebhook 是前置不满足，
// 重投也不会有变化：必须回传一个**不可重试**的结构化错误（而不是静默 (false, nil)），
// 同时不得占用重投队列。
//
// 口径变更（审计 N-13）：以前这一支返回 (false, nil)，"不占重投队列"是靠"根本没有错误"
// 实现的 —— 代价是失败原因、类别、轨迹一起消失。现在错误在、Retryable=false 在，
// 不占用队列这一半由类别判定守住，比原来更可观测。
func TestSendOutbound_DingTalk_MissingSessionWebhookStaysNonRetryable(t *testing.T) {
	quietHoursOffForTest(t)
	allowAnyDingtalkHostForTest(t)
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &DelayedOutboundReply{})
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	const eventID = "evt-dt-no-webhook"
	releaseReplyClaimForTest(t, eventID)

	const conv = "dt-conv-no-webhook"
	hub := seedInboundHub(t, db, "dingtalk", "7", conv, eventID)
	p := &ParsedPayload{EventID: eventID, Sender: conv, Content: "在吗", ChatID: conv}

	sent, sendErr := svc.sendOutbound(context.Background(), ChannelDingTalk, "7", p, "无处可投的回复", hub, nil)
	if sent {
		t.Fatal("缺 sessionWebhook 不得判为已投递")
	}
	if sendErr == nil {
		t.Fatal("缺 sessionWebhook 必须回传结构化错误，否则重放路径与失败轨迹都无从判定原因")
	}
	ce := AsChannelError(sendErr)
	if ce == nil {
		t.Fatalf("回传错误应能归一为 *ChannelError，实得 %T", sendErr)
	}
	if ce.Retryable {
		t.Errorf("前置不满足的失败必须判不可重试，实得 retryable=true category=%s", ce.Category)
	}
	if ce.Category != CategoryWindowExpired {
		t.Errorf("缺 sessionWebhook 属被动回复窗口不存在，期望 %s，实得 %s", CategoryWindowExpired, ce.Category)
	}
	var cnt int64
	if err := db.Model(&DelayedOutboundReply{}).Where("conversation_id = ?", conv).Count(&cnt).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != 0 {
		t.Errorf("前置不满足不该占用重投队列，实际 %d 条", cnt)
	}
}

// TestSendOutbound_Wechat_FailureEntersRetryLane 公众号出站失败此前只写日志，
// 既不回传错误也不入重试队列，客户永久收不到回复且无从观测。
func TestSendOutbound_Wechat_FailureEntersRetryLane(t *testing.T) {
	quietHoursOffForTest(t)
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &DelayedOutboundReply{}, &model.WechatAccount{})
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	const eventID = "evt-wx-retry-lane"
	releaseReplyClaimForTest(t, eventID)

	const conv = "wx-conv-retry-lane"
	hub := seedInboundHub(t, db, "wechat", "990001", conv, eventID)
	p := &ParsedPayload{EventID: eventID, Sender: conv, Content: "在吗", ChatID: conv}

	// 账号 990001 不存在 → 取 token 即失败（不触网），正是「真实投递接口报错」的最小场景。
	sent, sendErr := svc.sendOutbound(context.Background(), ChannelWechat, "990001", p, "公众号发不出的回复", hub, nil)
	if sent {
		t.Fatal("取不到 access_token 的公众号投递不得判成功")
	}
	if sendErr == nil {
		t.Fatal("公众号出站失败必须回传错误并进入重试通道，而不是只留一条日志")
	}
	var rec DelayedOutboundReply
	if err := db.Where("kind = ? AND conversation_id = ?", model.DelayedKindSendRetry, conv).First(&rec).Error; err != nil {
		t.Fatalf("公众号失败应进入重试队列: %v", err)
	}
	if got := strconv.Itoa(int(rec.Attempts)); got != "0" {
		t.Errorf("首发入队 attempts 应为 0，got %s", got)
	}
}
