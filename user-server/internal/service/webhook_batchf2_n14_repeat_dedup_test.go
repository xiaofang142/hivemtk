package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// N-14：入站「内容窗口去重」把**平台给了不同消息 ID 的两次真实消息**当成重复丢掉。
//
// 现场（批F-2 写媒体用例时跑出来的）：Meta 一次推两条图片，第一条正常入库，第二条
// 被判 `duplicate(channel+sender+content) within window`（两条的占位正文都是 "[图片]"），
// 于是 message_hub 只有一行、客户第二张照片既没入库也没回复。
// 同一形态对纯文本一样成立：客户在五分钟内连发两条"好的"，第二条直接消失。
//
// 平台的 at-least-once 重投本来就有 message_hub 的 (platform, msg_id, conversation_id)
// 唯一索引兜底（下游 isDuplicateKey 已按幂等容忍），内容窗口去重只对"没有稳定消息 ID"
// 的渠道才有独立价值。
//
// 三组用例都用进程内 MemoryCache，而不是共享 Redis：去重键跨 agent、跨用例可见，
// 用 nonce 只是碰巧不撞，撞了就是查不出来的假红/假绿。

const n14AccountID = "90113"

func n14Body(nonce, wamid1, wamid2 string) []byte {
	msg := func(id, ts string) string {
		return `{"from":"+8613900000014","id":"` + id + `","timestamp":"` + ts + `","type":"text","text":{"body":"确认收到 ` + nonce + `"}}`
	}
	return []byte(`{"object":"whatsapp_business_account","entry":[{"id":"W","changes":[{"value":{"messages":[` +
		msg(wamid1, "1700000101") + `,` + msg(wamid2, "1700000102") +
		`],"contacts":[{"profile":{"name":"Carol"},"wa_id":"+8613900000014"}]},"field":"messages"}]}]}`)
}

func TestN14_RepeatedContentWithDistinctWAMIDsIsNotDropped(t *testing.T) {
	db := testutil.NewTestDB(t,
		&model.WhatsAppCloudAccount{}, &model.MessageHub{}, &model.InboxConversation{},
		&model.UnifiedMessage{}, &model.WebhookEvent{}, &model.IntegrationAccount{},
	)
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())
	mc := cache.NewMemoryCache()
	t.Cleanup(mc.Close)
	svc.SetIngressSvc(NewInboxIngressServiceWithDB(db, mc))

	nonce := fmt.Sprintf("n14-wa-%d", time.Now().UnixNano())

	if _, err := svc.dispatchWhatsApp(context.Background(), n14AccountID,
		&ParsedPayload{EventID: "evt-n14"}, n14Body(nonce, "wamid-n14-1", "wamid-n14-2")); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	for _, wamid := range []string{"wamid-n14-1", "wamid-n14-2"} {
		var hub model.MessageHub
		err := db.Where("platform = ? AND account_id = ? AND msg_id = ?", "whatsapp", n14AccountID, wamid).
			First(&hub).Error
		if err != nil {
			t.Errorf("N-14 未达成：msg_id=%s 没入库（%v）⇒ 同内容不同 wamid 的第二条被内容窗口去重丢掉", wamid, err)
			continue
		}
		if hub.Content != "确认收到 "+nonce {
			t.Errorf("msg_id=%s content=%q", wamid, hub.Content)
		}
	}
}

// TestN14_IDLessEventStillDedupsByContent 反向边界：没有稳定消息 ID 的事件仍要走内容窗口
// 去重，否则平台重投会双份入库、双份触发 AI。
func TestN14_IDLessEventStillDedupsByContent(t *testing.T) {
	db := testutil.NewTestDB(t,
		&model.WhatsAppCloudAccount{}, &model.MessageHub{}, &model.InboxConversation{},
		&model.UnifiedMessage{}, &model.WebhookEvent{}, &model.IntegrationAccount{},
	)
	mc := cache.NewMemoryCache()
	t.Cleanup(mc.Close)
	ingest := NewInboxIngressServiceWithDB(db, mc)
	nonce := fmt.Sprintf("n14-idless-%d", time.Now().UnixNano())

	ev := func() *model.MessageEvent {
		return &model.MessageEvent{
			EventID:        "evt-" + nonce,
			Channel:        "whatsapp",
			SenderID:       "+8613900000015",
			ConversationID: "+8613900000015",
			Content:        "无id重复内容 " + nonce,
			MsgType:        "text",
			Timestamp:      time.Now(),
			Extra:          map[string]any{"account_id": n14AccountID},
		}
	}
	first, err := ingest.interceptInbound(context.Background(), ev())
	if err != nil {
		t.Fatalf("intercept first: %v", err)
	}
	if first.Blocked {
		t.Fatalf("首条不该被拦：%s", first.Reason)
	}
	second, err := ingest.interceptInbound(context.Background(), ev())
	if err != nil {
		t.Fatalf("intercept second: %v", err)
	}
	if !second.Blocked || !second.IsDup {
		t.Errorf("无 channel_msg_id 的同内容第二条应仍被内容窗口去重拦下，got blocked=%v dup=%v reason=%q",
			second.Blocked, second.IsDup, second.Reason)
	}
}

// TestN14_DingTalkRepeatedTextIsNotDropped 钉钉入站事件不带 channel_msg_id，
// 是同一条缺陷的第二处现场：同一客户五分钟内连发两条相同文本，第二条会被丢掉。
func TestN14_DingTalkRepeatedTextIsNotDropped(t *testing.T) {
	nonce := fmt.Sprintf("n14-dt-%d", time.Now().UnixNano())
	conv := "cid-" + nonce
	body := func(msgID string) []byte {
		return []byte(`{"conversationId":"` + conv + `","msgId":"` + msgID +
			`","conversationType":"2","senderStaffId":"staff-n14","msgtype":"text","text":{"content":"在吗 ` +
			nonce + `"},"sessionWebhook":"https://oapi.dingtalk.com/robot/sendBySession?session=n14","sessionWebhookExpiredTime":1893456000000}`)
	}
	svc, id, db := n14SetupDingTalk(t)
	headers := dtRobotHeaders("SECn14", time.Now().UnixMilli())

	for _, msgID := range []string{"m-n14-1", "m-n14-2"} {
		if err := svc.ReceiveMessage(context.Background(), id, body(msgID), nil, headers); err != nil {
			t.Fatalf("ReceiveMessage(%s): %v", msgID, err)
		}
	}

	var hubs []model.MessageHub
	if err := db.Where("platform = ? AND conversation_id = ?", "dingtalk", conv).
		Order("id ASC").Find(&hubs).Error; err != nil {
		t.Fatalf("query hubs: %v", err)
	}
	if len(hubs) != 2 {
		for _, h := range hubs {
			t.Logf("existing hub msg_id=%s content=%q", h.MsgID, h.Content)
		}
		t.Errorf("N-14 未达成：钉钉两条不同 msgId 的同内容消息应各入库一行，实际 %d 行", len(hubs))
	}
}

// n14SetupDingTalk 自建钉钉入站管线（不复用 setupDingTalkInbound）：那条夹具不返回 *gorm.DB，
// 而本用例要直接查 message_hub 数行数。
func n14SetupDingTalk(t *testing.T) (*DingTalkAppService, uint, *gorm.DB) {
	t.Helper()
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "")
	db := testutil.NewTestDBOrSkip(t, &model.DingTalkAppAccount{}, &model.MessageHub{})
	acc := &model.DingTalkAppAccount{
		AppKey: fmt.Sprintf("ak-n14-%d", time.Now().UnixNano()), AppSecret: "SECn14",
		InboundEnabled: true, Status: 1,
	}
	acc.UserID = 1
	if err := db.Create(acc).Error; err != nil {
		t.Fatalf("create dingtalk account: %v", err)
	}
	mc := cache.NewMemoryCache()
	t.Cleanup(mc.Close)
	return NewDingTalkAppService(db, &WebhookService{ingressSvc: NewInboxIngressServiceWithDB(db, mc)}), acc.ID, db
}
