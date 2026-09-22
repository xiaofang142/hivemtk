package service

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// 批F-6 / 审计 N-13：sendOutbound 的「请求根本没发出去」分支此前一律
// `sent=false, sendErr=nil` 后 return —— 调用方（重放 worker、告警、失败轨迹）
// 拿不到原因、类别，库里也留不下痕迹。收口后这些分支必须：
//   1. 回传一个不可重试的结构化 *ChannelError；
//   2. 在 message_hub 留下一行 status='send_failed' 的出站轨迹（含被丢弃的正文）；
//   3. 不占用持久化重投队列（重投必然同样失败）；
//   4. 让「客户仍未收到回复」这个事实继续可判（轨迹行不得被算作已回复）。
//
// 每条腿都断言**落库那一行**而不是内存里的返回值，且带前置 t.Fatal：
// 夹具不满足判据前置时，绿是假的。

// bf6Trace 读回某个会话的 send_failed 轨迹行；找不到即判红。
func bf6Trace(t *testing.T, db *gorm.DB, conv string) model.MessageHub {
	t.Helper()
	var row model.MessageHub
	err := db.Where("direction = ? AND conversation_id = ? AND status = ?", "outbound", conv, "send_failed").
		Order("id DESC").First(&row).Error
	if err != nil {
		t.Fatalf("会话 %s 应有一行 send_failed 出站轨迹，实得查询错误 %v（N-13：投递失败在库里无痕）", conv, err)
	}
	return row
}

// bf6Preconditions 钉住本批判据的前置：repo 已接线、seedInboundHub 交出的确实是
// 「入站 + 已落库」的那条内存副本（正是生产链路里 hubMsg 的形状）。
// 前置不成立时 outboundSendFailed 会静默走 return，断言就变成空跑。
func bf6Preconditions(t *testing.T, svc *WebhookService, hub *model.MessageHub) {
	t.Helper()
	if svc.messageHubRepo == nil {
		t.Fatal("前置不成立：messageHubRepo 未接线，轨迹路径根本不会执行")
	}
	if hub == nil {
		t.Fatal("前置不成立：hubMsg 为 nil")
	}
	if hub.ID == 0 || hub.Direction != "inbound" {
		t.Fatalf("前置不成立：本批要覆盖的是「拿到的 hubMsg 是入站副本」这一形状，实得 id=%d direction=%s",
			hub.ID, hub.Direction)
	}
}

func bf6ChannelErr(t *testing.T, conv string, sent bool, sendErr error) *ChannelError {
	t.Helper()
	if sent {
		t.Fatalf("%s：前置不满足的失败不得判为已投递", conv)
	}
	if sendErr == nil {
		t.Fatalf("%s：N-13 症结——回传 nil 等于向调用方谎报「没有发生失败」", conv)
	}
	ce := AsChannelError(sendErr)
	if ce == nil {
		t.Fatalf("%s：回传错误应能归一为 *ChannelError，实得 %T", conv, sendErr)
	}
	if ce.Retryable {
		t.Fatalf("%s：前置不满足属确定性失败，重投只会同样失败，实得 retryable=true", conv)
	}
	return ce
}

// TestSendOutbound_BF6_DingTalkMissingWebhookLeavesTrace 缺 sessionWebhook：
// 除结构化错误外，必须另造一行 send_failed 轨迹（AI 链路拿到的 hubMsg 是入站副本，
// 就地改状态那条路永远命中 0 行）。
func TestSendOutbound_BF6_DingTalkMissingWebhookLeavesTrace(t *testing.T) {
	quietHoursOffForTest(t)
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &DelayedOutboundReply{})
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	const eventID = "evt-bf6-dt-missing"
	const conv = "bf6-dt-missing"
	releaseReplyClaimForTest(t, eventID)
	hub := seedInboundHub(t, db, "dingtalk", "7", conv, eventID)
	bf6Preconditions(t, svc, hub)

	p := &ParsedPayload{EventID: eventID, Sender: conv, Content: "在吗", ChatID: conv}
	const reply = "这条回复压根没发出去"
	sent, sendErr := svc.sendOutbound(context.Background(), ChannelDingTalk, "7", p, reply, hub, nil)
	ce := bf6ChannelErr(t, conv, sent, sendErr)
	if ce.Category != CategoryWindowExpired {
		t.Errorf("缺 sessionWebhook = 被动回复窗口不存在，期望 %s，实得 %s", CategoryWindowExpired, ce.Category)
	}

	row := bf6Trace(t, db, conv)
	if row.Content != reply {
		t.Errorf("轨迹行必须带上被丢弃的正文，否则事后无从知道回了什么，实得 %q", row.Content)
	}
	if row.Platform != "dingtalk" {
		t.Errorf("轨迹行 platform 期望 dingtalk，实得 %s", row.Platform)
	}
	if !row.IsAIReply {
		t.Errorf("轨迹行应标为 AI 回复，实得 is_ai_reply=%v", row.IsAIReply)
	}
	if row.ReceiverID != hub.SenderID {
		t.Errorf("轨迹行收件人应是被冷落的那个客户 %q，实得 %q", hub.SenderID, row.ReceiverID)
	}
	if got, _ := row.Extra["send_failed_category"].(string); got != string(CategoryWindowExpired) {
		t.Errorf("Extra.send_failed_category 期望 %s，实得 %q", CategoryWindowExpired, got)
	}
	if got, _ := row.Extra["send_failed_reason"].(string); got == "" {
		t.Error("Extra.send_failed_reason 不得为空，否则轨迹只剩状态没有原因")
	}
	if _, ok := row.Extra["send_failed_at"]; !ok {
		t.Error("Extra.send_failed_at 缺失，无法定位失败时刻")
	}

	var cnt int64
	if err := db.Model(&DelayedOutboundReply{}).Where("conversation_id = ?", conv).Count(&cnt).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != 0 {
		t.Errorf("不可重试失败不该占用重投队列，实得 %d 条", cnt)
	}

	// 全链判据：这行「没到达客户的出站」不能被算成已回复，否则补触发就此失效。
	unreplied, withinWindow, err := svc.messageHubRepo.HasUnrepliedCustomerMessage(context.Background(), conv, 5*time.Minute)
	if err != nil {
		t.Fatalf("oracle: %v", err)
	}
	if !unreplied || !withinWindow {
		t.Errorf("期望 (true,true)——客户仍在等回复，实得 (%v,%v)", unreplied, withinWindow)
	}
}

// TestSendOutbound_BF6_DingTalkExpiredWebhookReasonCarriesDeadline 过期分支的原因里
// 必须带上 expired_at，运维才分得清「没带 webhook」和「带晚了」。
func TestSendOutbound_BF6_DingTalkExpiredWebhookReasonCarriesDeadline(t *testing.T) {
	quietHoursOffForTest(t)
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &DelayedOutboundReply{})
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	const eventID = "evt-bf6-dt-expired"
	const conv = "bf6-dt-expired"
	releaseReplyClaimForTest(t, eventID)
	hub := seedInboundHub(t, db, "dingtalk", "7", conv, eventID)
	bf6Preconditions(t, svc, hub)
	expiredAt := time.Now().Add(-2 * time.Hour).Unix()
	// 钉钉下发的是毫秒；这里刻意喂毫秒，让"秒/毫秒归一"那一格也有一条腿踩着。
	// 地址用必然拒绝的本地端口而不是真实 oapi.dingtalk.com：万一"过期判定"整格被抹掉，
	// 用例只会快速失败，不会在变异电池里对真平台发出真请求。
	hub.Extra = model.JSONMap{
		"session_webhook":            "http://127.0.0.1:1/robot/sendBySession?session=x",
		"session_webhook_expired_at": expiredAt * 1000,
	}

	p := &ParsedPayload{EventID: eventID, Sender: conv, Content: "在吗", ChatID: conv}
	sent, sendErr := svc.sendOutbound(context.Background(), ChannelDingTalk, "7", p, "过期后的回复", hub, nil)
	ce := bf6ChannelErr(t, conv, sent, sendErr)
	if ce.Category != CategoryWindowExpired {
		t.Errorf("期望 %s，实得 %s", CategoryWindowExpired, ce.Category)
	}
	if !strings.Contains(ce.Raw, strconv.FormatInt(expiredAt, 10)) {
		t.Errorf("原因应含过期时刻 %d 以便与「未携带」区分，实得 %q", expiredAt, ce.Raw)
	}
	if got, _ := bf6Trace(t, db, conv).Extra["send_failed_reason"].(string); !strings.Contains(got, strconv.FormatInt(expiredAt, 10)) {
		t.Errorf("落库轨迹的原因同样要含过期时刻，实得 %q", got)
	}
}

// TestSendOutbound_BF6_DingTalkIllegalHostNeverTouchesNetwork 域名非法这一支既要
// 判成不可重试的 bad_request，更要证明真的没出网（SSRF 守卫不许在失败路径上先打一发）。
func TestSendOutbound_BF6_DingTalkIllegalHostNeverTouchesNetwork(t *testing.T) {
	quietHoursOffForTest(t)
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&hits, 1)
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer srv.Close()

	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &DelayedOutboundReply{})
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	const eventID = "evt-bf6-dt-host"
	const conv = "bf6-dt-host"
	releaseReplyClaimForTest(t, eventID)
	hub := seedInboundHub(t, db, "dingtalk", "7", conv, eventID)
	bf6Preconditions(t, svc, hub)
	hub.Extra = model.JSONMap{"session_webhook": srv.URL + "/sendBySession"}

	p := &ParsedPayload{EventID: eventID, Sender: conv, Content: "在吗", ChatID: conv}
	sent, sendErr := svc.sendOutbound(context.Background(), ChannelDingTalk, "7", p, "往非法域名投的回复", hub, nil)
	ce := bf6ChannelErr(t, conv, sent, sendErr)
	if ce.Category != CategoryBadRequest {
		t.Errorf("域名非法是请求本身不合法，期望 %s，实得 %s", CategoryBadRequest, ce.Category)
	}
	if n := atomic.LoadInt64(&hits); n != 0 {
		t.Errorf("域名校验必须在出网之前，实际打到测试服务器 %d 次", n)
	}
	bf6Trace(t, db, conv)
}

// TestSendOutbound_BF6_TelegramNonNumericChatIDLeavesTrace TG 的 conversation_id
// 解析不出 chat_id 时同样属「根本没发」，且 telegram 在平台词表内 ⇒ 轨迹要落库。
func TestSendOutbound_BF6_TelegramNonNumericChatIDLeavesTrace(t *testing.T) {
	quietHoursOffForTest(t)
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &DelayedOutboundReply{})
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	const eventID = "evt-bf6-tg-chatid"
	const conv = "bf6-tg-not-a-number"
	releaseReplyClaimForTest(t, eventID)
	hub := seedInboundHub(t, db, "telegram", "12", conv, eventID)
	bf6Preconditions(t, svc, hub)

	p := &ParsedPayload{EventID: eventID, Sender: conv, Content: "在吗", ChatID: conv}
	sent, sendErr := svc.sendOutbound(context.Background(), ChannelTelegram, "12", p, "chat_id 解析不出的回复", hub, nil)
	ce := bf6ChannelErr(t, conv, sent, sendErr)
	if ce.Category != CategoryBadRequest {
		t.Errorf("会话键形态不合法属 %s，实得 %s", CategoryBadRequest, ce.Category)
	}
	row := bf6Trace(t, db, conv)
	if row.Content != "chat_id 解析不出的回复" {
		t.Errorf("轨迹正文应是被丢弃的那条回复，实得 %q", row.Content)
	}
}

// TestSendOutbound_BF6_WhatsAppWindowClosedWithoutTemplateLeavesTrace 超 24h 窗口且未配
// 兜底模板：这是一条真实的「客户收不到且无人知道」的路径，窗口关闭时刻要写进原因。
func TestSendOutbound_BF6_WhatsAppWindowClosedWithoutTemplateLeavesTrace(t *testing.T) {
	quietHoursOffForTest(t)
	t.Setenv("WHATSAPP_FALLBACK_TEMPLATE", "")
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &DelayedOutboundReply{})
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	const eventID = "evt-bf6-wa-window"
	const conv = "bf6-wa-window"
	releaseReplyClaimForTest(t, eventID)
	hub := seedInboundHub(t, db, "whatsapp", "13", conv, eventID)
	bf6Preconditions(t, svc, hub)
	lastInbound := time.Now().Add(-25 * time.Hour)
	if err := db.Model(&model.MessageHub{}).Where("msg_id = ?", hub.MsgID).
		Update("sent_at", lastInbound).Error; err != nil {
		t.Fatalf("把入站时刻推到窗口外: %v", err)
	}

	p := &ParsedPayload{EventID: eventID, Sender: conv, Content: "在吗", ChatID: conv}
	sent, sendErr := svc.sendOutbound(context.Background(), ChannelWhatsapp, "13", p, "窗口关闭后的回复", hub, nil)
	ce := bf6ChannelErr(t, conv, sent, sendErr)
	if ce.Category != CategoryWindowExpired {
		t.Errorf("24h 窗口关闭属 window_expired，实得 %s", ce.Category)
	}
	if !strings.Contains(ce.Raw, lastInbound.Format("2006-01-02 15:04:05")) {
		t.Errorf("原因应含最后入站时刻，实得 %q", ce.Raw)
	}
	bf6Trace(t, db, conv)
}

// TestSendOutbound_BF6_UnsupportedChannelErrorsWithoutTraceRow 出站分支表里没有的渠道：
// 平台词表必然校验不过，写库是注定失败的一刀，所以这一支只回传错误 + 结构化日志。
// （回传错误本身是新口径：以前这一支连错误都是 nil。）
func TestSendOutbound_BF6_UnsupportedChannelErrorsWithoutTraceRow(t *testing.T) {
	quietHoursOffForTest(t)
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &DelayedOutboundReply{})
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	const eventID = "evt-bf6-unknown-chan"
	const conv = "bf6-unknown-chan"
	releaseReplyClaimForTest(t, eventID)
	hub := seedInboundHub(t, db, "telegram", "12", conv, eventID)
	bf6Preconditions(t, svc, hub)

	p := &ParsedPayload{EventID: eventID, Sender: conv, Content: "在吗", ChatID: conv}
	sent, sendErr := svc.sendOutbound(context.Background(), WebhookChannel("carrier_pigeon"), "12", p, "无处投递的回复", hub, nil)
	ce := bf6ChannelErr(t, conv, sent, sendErr)
	if ce.Category != CategoryBadRequest {
		t.Errorf("分支表漂移属中台自身不合法，期望 %s，实得 %s", CategoryBadRequest, ce.Category)
	}
	var cnt int64
	if err := db.Model(&model.MessageHub{}).Where("direction = ? AND status = ?", "outbound", "send_failed").
		Count(&cnt).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != 0 {
		t.Errorf("未登记渠道的失败轨迹必然过不了平台校验，不该尝试落库（只留日志），实得 %d 行", cnt)
	}
}

// TestSendOutbound_BF6_RetryableFailureWritesNoTraceButKeepsRetryLane 可重试失败归重投队列管，
// 不得同时留终态轨迹行 —— 否则一次抖动先落一条 send_failed，重投成功后库里仍留着「失败」。
func TestSendOutbound_BF6_RetryableFailureWritesNoTraceButKeepsRetryLane(t *testing.T) {
	quietHoursOffForTest(t)
	allowAnyDingtalkHostForTest(t)
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &DelayedOutboundReply{})
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"errcode":310000,"errmsg":"keyword not in content"}`))
	}))
	defer srv.Close()

	const eventID = "evt-bf6-retryable"
	const conv = "bf6-retryable"
	releaseReplyClaimForTest(t, eventID)
	hub := seedInboundHub(t, db, "dingtalk", "7", conv, eventID)
	bf6Preconditions(t, svc, hub)
	hub.Extra = model.JSONMap{
		"session_webhook":            srv.URL,
		"session_webhook_expired_at": time.Now().Add(30 * time.Minute).UnixMilli(),
	}

	p := &ParsedPayload{EventID: eventID, Sender: conv, Content: "在吗", ChatID: conv}
	sent, sendErr := svc.sendOutbound(context.Background(), ChannelDingTalk, "7", p, "平台拒绝但有重投机会", hub, nil)
	if sent || sendErr == nil {
		t.Fatalf("平台拒绝必须回传错误，实得 sent=%v err=%v", sent, sendErr)
	}
	ce := AsChannelError(sendErr)
	if !ce.Retryable {
		t.Fatalf("前置不成立：这一支要的是可重试失败，实得 category=%s retryable=false", ce.Category)
	}
	var failed int64
	if err := db.Model(&model.MessageHub{}).Where("direction = ? AND status = ?", "outbound", "send_failed").
		Count(&failed).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if failed != 0 {
		t.Errorf("可重试失败不得留终态轨迹，实得 %d 行", failed)
	}
	var queued int64
	if err := db.Model(&DelayedOutboundReply{}).Where("kind = ? AND conversation_id = ?", model.DelayedKindSendRetry, conv).
		Count(&queued).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if queued != 1 {
		t.Errorf("可重试失败应落 1 条重投记录，实得 %d", queued)
	}
}

// TestOutboundSendFailed_BF6_PersistedOutboundMarksInPlace 只有「本来就有一行出站记录」时
// 才就地改状态：不得另造第二行，也不得把原行的方向/正文改掉。
func TestOutboundSendFailed_BF6_PersistedOutboundMarksInPlace(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &DelayedOutboundReply{})
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())
	if svc.messageHubRepo == nil {
		t.Fatal("前置不成立：messageHubRepo 未接线")
	}

	const conv = "bf6-inplace"
	out := &model.MessageHub{
		MsgID: "mh:out-bf6-inplace", Platform: "telegram", AccountID: "5",
		Direction: "outbound", Status: "pending", MsgType: "text",
		SenderID: "5", ReceiverID: "cust1", Content: "已落库待发的回复",
		ConversationID: conv, SentAt: time.Now(),
	}
	if err := db.Create(out).Error; err != nil {
		t.Fatalf("seed outbound: %v", err)
	}
	if out.ID == 0 {
		t.Fatal("前置不成立：出站行未拿到自增 id")
	}

	svc.outboundSendFailed(context.Background(), ChannelTelegram, "5", out, "不该另造一行的正文",
		&ChannelError{Channel: "telegram", Category: CategoryBadRequest, Retryable: false, Raw: "就地失败"})

	var rows []model.MessageHub
	if err := db.Where("conversation_id = ?", conv).Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("就地改状态不得另造轨迹行，实得 %d 行：%+v", len(rows), bf6RowIDs(rows))
	}
	if rows[0].ID != out.ID || rows[0].Direction != "outbound" {
		t.Fatalf("唯一一行应是原出站行本身，实得 id=%d direction=%s", rows[0].ID, rows[0].Direction)
	}
	if rows[0].Status != "send_failed" {
		t.Errorf("原行状态应改为 send_failed，实得 %s", rows[0].Status)
	}
	if got, _ := rows[0].Extra["send_failed_category"].(string); got != string(CategoryBadRequest) {
		t.Errorf("Extra.send_failed_category 期望 bad_request，实得 %q", got)
	}
	if got, _ := rows[0].Extra["send_failed_reason"].(string); got != "就地失败" {
		t.Errorf("Extra.send_failed_reason 期望原样带过 Raw，实得 %q", got)
	}
}

// TestOutboundSendFailed_BF6_InboundCopyCreatesExactlyOneRow 生产形状（入站副本、ID 已落库但
// direction=inbound）下必须另造恰好一行轨迹，且原入站行不受影响。
func TestOutboundSendFailed_BF6_InboundCopyCreatesExactlyOneRow(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &DelayedOutboundReply{})
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())
	if svc.messageHubRepo == nil {
		t.Fatal("前置不成立：messageHubRepo 未接线")
	}

	const conv = "bf6-newrow"
	hub := seedInboundHub(t, db, "dingtalk", "7", conv, "evt-bf6-newrow")
	bf6Preconditions(t, svc, hub)

	svc.outboundSendFailed(context.Background(), ChannelDingTalk, "7", hub, "另造一行的正文",
		&ChannelError{Channel: "dingtalk", Category: CategoryWindowExpired, Retryable: false, Raw: "没有回复地址"})

	var rows []model.MessageHub
	if err := db.Where("conversation_id = ?", conv).Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("期望 1 入站 + 1 失败轨迹 = 2 行，实得 %d：%+v", len(rows), bf6RowIDs(rows))
	}
	if rows[0].Direction != "inbound" || rows[0].Status == "send_failed" {
		t.Errorf("入站行不得被顺手改成失败轨迹，实得 direction=%s status=%q", rows[0].Direction, rows[0].Status)
	}
	tr := rows[1]
	if tr.Direction != "outbound" || tr.Status != "send_failed" {
		t.Fatalf("轨迹行期望 outbound/send_failed，实得 %s/%s", tr.Direction, tr.Status)
	}
	if tr.Content != "另造一行的正文" {
		t.Errorf("轨迹正文错误，实得 %q", tr.Content)
	}
	if !strings.HasPrefix(tr.MsgID, "dingtalk-fail-") {
		t.Errorf("轨迹 msg_id 应带渠道前缀以便与真实出站区分，实得 %q", tr.MsgID)
	}
}

// TestOutboundSendFailed_BF6_OutboundShapeNeverPersistedStillCreatesRow 「方向是出站但没拿到
// 自增 id」这一格是 ID 判定唯一独立承重的位置：MarkOutboundSendFailed 对 id==0 直接返回 nil
// （它的 WHERE 也要求 direction='outbound' 命中真实行），所以只看方向就会让这一支走一趟空 Mark
// 后 return —— 又一次"库里无痕"。没落库过的出站副本必须退回另造一行。
func TestOutboundSendFailed_BF6_OutboundShapeNeverPersistedStillCreatesRow(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &DelayedOutboundReply{})
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())
	if svc.messageHubRepo == nil {
		t.Fatal("前置不成立：messageHubRepo 未接线")
	}

	const conv = "bf6-outbound-unpersisted"
	// 生产里 sendOutbound 的三个调用点都不会交出这种形状（AI 链路给入站行，重放给零方向行），
	// 所以这一格是对"出站行落库"那一步补齐后的前射保护，用直调打桩。
	memOut := &model.MessageHub{Platform: "telegram", AccountID: "5", Direction: "outbound",
		MsgType: "text", SenderID: "5", ReceiverID: "cust-unpersisted",
		Content: "只在内存里的出站行", ConversationID: conv, SentAt: time.Now()}
	if memOut.ID != 0 {
		t.Fatal("前置不成立：夹具已被落库，不再是「未落库的出站副本」")
	}

	svc.outboundSendFailed(context.Background(), ChannelTelegram, "5", memOut, "未落库出站行的失败正文",
		&ChannelError{Channel: "telegram", Category: CategoryBadRequest, Retryable: false, Raw: "id 为 0"})

	var rows []model.MessageHub
	if err := db.Where("conversation_id = ?", conv).Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("期望恰好另造 1 行轨迹（空 Mark 之后必须退回新建），实得 %d 行：%+v", len(rows), bf6RowIDs(rows))
	}
	if rows[0].Direction != "outbound" || rows[0].Status != "send_failed" {
		t.Fatalf("轨迹行期望 outbound/send_failed，实得 %s/%s", rows[0].Direction, rows[0].Status)
	}
	if !strings.HasPrefix(rows[0].MsgID, "telegram-fail-") {
		t.Errorf("轨迹 msg_id 期望 telegram-fail- 前缀，实得 %q", rows[0].MsgID)
	}
}

// TestOutboundSendFailed_BF6_NoTraceTargetOnlyLogs 两种"轨迹无处归属"的形状：
// hubMsg 为 nil（桥接缺入站上下文）和会话键为空。两者都只能记日志——不 panic、
// 不凭空造一行没有归属的出站。
func TestOutboundSendFailed_BF6_NoTraceTargetOnlyLogs(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &DelayedOutboundReply{})
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())
	if svc.messageHubRepo == nil {
		t.Fatal("前置不成立：messageHubRepo 未接线")
	}

	svc.outboundSendFailed(context.Background(), ChannelTelegram, "5", nil, "没有上下文的正文",
		&ChannelError{Channel: "telegram", Category: CategoryBadRequest, Retryable: false, Raw: "nil hub"})

	// 有入站行但会话键为空：归属不到的轨迹只会变成一条谁也看不到的出站。
	orphan := &model.MessageHub{MsgID: "mh:in-bf6-orphan", Platform: "telegram", AccountID: "5",
		Direction: "inbound", MsgType: "text", Content: "hi", ConversationID: "", SentAt: time.Now()}
	if err := db.Create(orphan).Error; err != nil {
		t.Fatalf("seed orphan: %v", err)
	}
	svc.outboundSendFailed(context.Background(), ChannelTelegram, "5", orphan, "没有会话键的正文",
		&ChannelError{Channel: "telegram", Category: CategoryBadRequest, Retryable: false, Raw: "no conv"})

	var cnt int64
	if err := db.Model(&model.MessageHub{}).Count(&cnt).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != 1 {
		t.Errorf("除手工 seed 的那行孤儿入站外不得再落任何行，实得 %d 行", cnt)
	}
}

// TestSendOutbound_BF6_BridgeUndeliverableKeepsSingleOutboundRow 桥接族的失败轨迹本来就是它
// 自己落的那一行（status=failed + scenario=undeliverable + 原因）。这一支改走"只记日志"后
// 仍回传结构化错误；若顺手再补一行 send_failed，同一会话会出现两行出站，坐席侧与补触发侧
// 都会看到一条根本不存在的回复。
func TestSendOutbound_BF6_BridgeUndeliverableKeepsSingleOutboundRow(t *testing.T) {
	quietHoursOffForTest(t)
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &model.InboxConversation{}, &DelayedOutboundReply{})
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	const eventID = "evt-bf6-bridge-undel"
	const conv = "bf6-bridge-undel"
	releaseReplyClaimForTest(t, eventID)
	hub := seedInboundHub(t, db, "xiaohongshu", "xiaohongshu-unknown", conv, eventID)
	bf6Preconditions(t, svc, hub)

	p := &ParsedPayload{EventID: eventID, Sender: conv, Content: "在吗", ChatID: conv}
	sent, sendErr := svc.sendOutbound(context.Background(), ChannelXiaohongshu, "xiaohongshu-unknown", p,
		"不可达账号的回复", hub, nil)
	ce := bf6ChannelErr(t, conv, sent, sendErr)
	if ce.Category != CategoryBadRequest {
		t.Errorf("占位账号不可达属 bad_request，实得 %s", ce.Category)
	}

	var rows []model.MessageHub
	if err := db.Where("conversation_id = ? AND direction = ?", conv, "outbound").
		Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("桥接不可达只应留桥接自己那一行出站，实得 %d：%+v", len(rows), bf6RowIDs(rows))
	}
	if rows[0].Status != "failed" {
		t.Errorf("桥接不可达行的状态期望 failed（它自己的口径），实得 %q", rows[0].Status)
	}
	if v, _ := rows[0].Extra["scenario"].(string); v != "undeliverable" {
		t.Errorf("Extra.scenario 期望 undeliverable，实得 %q", v)
	}
}

// TestBF6_ChannelVocabularyCoversEveryOutboundChannel 静态锁（读源码，不是读内存常量表）：
//
//   - message_hub 平台词表必须覆盖每个 WebhookChannel 常量，否则该渠道的失败轨迹
//     会被 Normalize 判非法、只在日志里消失（本批修的就是「库里无痕」）；
//   - sendOutbound 的 switch 只能 case 已登记的渠道常量，漂移到未登记常量就等于
//     走 default 之外的投递，平台校验那层再也不会拦。
//
// 用源码扫描而不是手写清单：新增一个 Channel 常量时清单不会自动更新，那样锁是假的。
func TestBF6_ChannelVocabularyCoversEveryOutboundChannel(t *testing.T) {
	consts := bf6ChannelConstants(t)
	if len(consts) < 10 {
		t.Fatalf("源码扫描只取到 %d 个渠道常量，扫描口径本身已失效", len(consts))
	}
	for name, value := range consts {
		if !messageHubPlatforms[value] {
			t.Errorf("%s(%q) 不在 messageHubPlatforms 里：该渠道的投递失败轨迹永远落不了库", name, value)
		}
	}

	cases := bf6OutboundSwitchChannels(t)
	if len(cases) < 8 {
		t.Fatalf("只取到 %d 个出站 case，switch 解析口径已失效", len(cases))
	}
	for name := range cases {
		if _, ok := consts[name]; !ok {
			t.Errorf("sendOutbound case 了未登记为 WebhookChannel 常量的 %s", name)
		}
	}
}

func bf6RowIDs(rows []model.MessageHub) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, fmt.Sprintf("%s/%s/%s", r.MsgID, r.Direction, r.Status))
	}
	return out
}

// bf6ChannelConstants 从本包源码里收集全部 `ChannelX WebhookChannel = "y"` 常量。
func bf6ChannelConstants(t *testing.T) map[string]string {
	t.Helper()
	consts := map[string]string{}
	bf6EachFile(t, func(f *ast.File) {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Values) == 0 {
					continue
				}
				if id, ok := vs.Type.(*ast.Ident); !ok || id.Name != "WebhookChannel" {
					continue
				}
				for i, nm := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					if bl, ok := vs.Values[i].(*ast.BasicLit); ok {
						consts[nm.Name] = strings.Trim(bl.Value, `"`)
					}
				}
			}
		}
	})
	return consts
}

// bf6OutboundSwitchChannels 取 sendOutbound 里 `switch channel` 那一层 case 的常量名集合。
// 只认根 switch 的 case，内层嵌套 switch（按渠道值之外的判断）不算。
func bf6OutboundSwitchChannels(t *testing.T) map[string]bool {
	t.Helper()
	found := map[string]bool{}
	bf6EachFile(t, func(f *ast.File) {
		fd, ok := bfindSendOutbound(f)
		if !ok {
			return
		}
		ast.Inspect(fd, func(n ast.Node) bool {
			sw, ok := n.(*ast.SwitchStmt)
			if !ok || sw.Tag == nil {
				return true
			}
			if be, ok := sw.Tag.(*ast.Ident); !ok || be.Name != "channel" {
				return true
			}
			for _, st := range sw.Body.List {
				cc, ok := st.(*ast.CaseClause)
				if !ok {
					continue
				}
				for _, e := range cc.List {
					if id, ok := e.(*ast.Ident); ok {
						found[id.Name] = true
					}
				}
			}
			return false
		})
	})
	return found
}

// bfindSendOutbound 在 f 中定位 sendOutbound 函数声明。
func bfindSendOutbound(f *ast.File) (*ast.FuncDecl, bool) {
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == "sendOutbound" {
			return fd, true
		}
	}
	return nil, false
}

// bf6EachFile 对本包每个非测试 .go 文件解析一次并回调；解析失败/无匹配一律判红，
// 绝不静默跳过（跳过 = 锁失效 = 假绿）。
func bf6EachFile(t *testing.T, fn func(*ast.File)) {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("读包目录失败：静态锁无从判定，实为 %v", err)
	}
	parsed := 0
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("读 %s 失败: %v", name, err)
		}
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatalf("解析 %s 失败，静态锁不能靠跳过继续放行: %v", name, err)
		}
		parsed++
		fn(f)
	}
	if parsed == 0 {
		t.Fatal("一个源文件都没扫到：静态锁在空转")
	}
}
