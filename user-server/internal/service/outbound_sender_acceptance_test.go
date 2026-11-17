package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

// specs/024 T102–T109 验收点名测试（逐渠道 sender 行为冻结的单元级钉子）。
// 与 outbound_golden_test.go 互补：golden 钉「全序列」，这里钉「单点契约」。
// 平台 HTTP 全部经 goldenRouter 劫持（同 golden 夹具）；账号行种测试库。

// senderTestDB 建测试库并返回句柄（各用例按需种渠道账号行）。
func senderTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t,
		&model.MessageHub{}, &model.InboxConversation{}, &model.DelayedOutboundReply{},
		&model.WebhookEvent{}, &model.IntegrationAccount{}, &model.UnifiedMessage{},
		&model.CustomerSession{}, &model.SessionMessage{}, &model.AISuggestion{},
		&model.AgentStatus{}, &model.Customer{}, &model.MessageTrace{},
		&model.FeishuAccount{}, &model.FeishuMessage{}, &model.TelegramAccount{},
		&model.WechatAccount{}, &model.WeComAccount{}, &model.QQAccount{},
		&model.WhatsAppCloudAccount{},
	)
}

// senderCapture 万能假平台：按路径取固定应答并记录调用。
func senderCapture(t *testing.T, router *goldenRouter, host string, calls *[]goldenCall,
	handlers map[string]func(*http.Request) (int, string)) {
	t.Helper()
	router.serve(t, host, (&goldenRecorder{calls: calls, handlers: handlers}).handler())
}

func senderReadCalls(calls *[]goldenCall, mu *sync.Mutex) []goldenCall {
	mu.Lock()
	defer mu.Unlock()
	out := make([]goldenCall, len(*calls))
	copy(out, *calls)
	return out
}

// ── T102 wecom ──────────────────────────────────────────────────────────

func TestWecomSender_DelegatesToIntegration(t *testing.T) {
	t.Setenv("WECOM_ALLOW_OUTBOUND", "1")    // IS_TEST_MODE=1 时企微出站被哨兵跳过
	t.Setenv("WECOM_DISABLE_OUTBOUND", "0") // 金样要钉真实委托形状
	senderTestDB(t)
	database := senderTestDB(t)
	if err := database.Create(&model.WeComAccount{ID: 104, TenantID: 1, CorpID: "corp-golden", CorpSecret: "corp-sec-golden", Status: 1}).Error; err != nil {
		t.Fatalf("seed wecom: %v", err)
	}
	releaseReplyClaimForTest(t, "evt-sa-wecom")
	router := newGoldenRouter(t)
	var mu sync.Mutex
	calls := make([]goldenCall, 0, 2)
	senderCapture(t, router, "qyapi.weixin.qq.com", &calls, map[string]func(*http.Request) (int, string){
		"/cgi-bin/gettoken": func(*http.Request) (int, string) {
			return 200, `{"errcode":0,"access_token":"tok","expires_in":7200}`
		},
		"/cgi-bin/externalcontact/message/send": func(*http.Request) (int, string) {
			return 200, `{"errcode":0,"errmsg":"ok","msgid":"m1"}`
		},
	})
	svc := NewWebhookService()
	t.Cleanup(func() { svc.Stop(context.Background()) })

	sent, sendErr := svc.sendOutbound(context.Background(), ChannelWeCom, "104",
		&ParsedPayload{EventID: "evt-sa-wecom", Sender: "wo-golden", Content: "在吗"},
		" delegation 正文", nil, nil)
	if !sent || sendErr != nil {
		t.Fatalf("wecom 出站应成功: sent=%v err=%v", sent, sendErr)
	}
	got := senderReadCalls(&calls, &mu)
	var sendBody map[string]any
	for i := range got {
		if strings.Contains(got[i].Path, "/cgi-bin/externalcontact/message/send") && got[i].Body != nil {
			sendBody = got[i].Body
		}
	}
	if sendBody == nil {
		t.Fatalf("未捕获到 message/send 调用: %+v", got)
	}
	// 搬迁契约：委托 integration 的 (ExternalUserID, MsgType, Content) 与搬迁前一致
	//（AccountID 经 healthSvc 选号 + wecom-out-<account> msgID 自证）。
	if got2, _ := sendBody["external_userid"].([]any); len(got2) == 0 || got2[0] != "wo-golden" {
		t.Errorf("external_userid 应为 [wo-golden]，got %v", sendBody["external_userid"])
	}
	if sendBody["msgtype"] != "text" {
		t.Errorf("msgtype 应为 text，got %v", sendBody["msgtype"])
	}
	if txt, _ := sendBody["text"].(map[string]any); txt == nil || txt["content"] != " delegation 正文" {
		t.Errorf("text.content 不符: %v", sendBody["text"])
	}
	var hub model.MessageHub
	if err := database.Where("platform = ? AND direction = ?", "wecom", "outbound").First(&hub).Error; err != nil {
		t.Fatalf("应落 message_hub 出站行: %v", err)
	}
	if !strings.HasPrefix(hub.MsgID, "wecom-out-") {
		t.Errorf("msgID 应为 wecom-out-*，got %s", hub.MsgID)
	}
	if hub.ReceiverID != "wo-golden" {
		t.Errorf("出站行 ReceiverID 应为委托目标 wo-golden，got %s", hub.ReceiverID)
	}
}

// ── T103 feishu ─────────────────────────────────────────────────────────

func feishuAcceptFixture(t *testing.T, router *goldenRouter, calls *[]goldenCall, textCode int, mu *sync.Mutex, messagesCalled *int) {
	database := senderTestDB(t)
	if err := database.Create(&model.FeishuAccount{ID: 101, TenantID: 1, AccountName: "sa-fs", AppID: "cli_sa", AppSecret: "sec_sa", Status: 1}).Error; err != nil {
		t.Fatalf("seed feishu: %v", err)
	}
	senderCapture(t, router, "open.feishu.cn", calls, map[string]func(*http.Request) (int, string){
		"/open-apis/auth/v3/tenant_access_token/internal": func(*http.Request) (int, string) {
			return 200, `{"code":0,"tenant_access_token":"t-sa","expire":7200}`
		},
		"/open-apis/im/v1/messages": func(*http.Request) (int, string) {
			if textCode != 0 {
				mu.Lock()
				n := *messagesCalled
				*messagesCalled++
				mu.Unlock()
				if n == 0 { // 第一次（正文）失败；第二次（卡片）成功——顺序契约由调用次数保证
					return 200, `{"code":230013,"msg":"target not in contact range"}`
				}
			}
			return 200, `{"code":0,"data":{"message_id":"om-sa"}}`
		},
	})
}

func TestFeishuSender_RichCardPayload(t *testing.T) {
	releaseReplyClaimForTest(t, "evt-sa-fs-card")
	router := newGoldenRouter(t)
	var mu sync.Mutex
	calls := make([]goldenCall, 0, 4)
	messagesCalled := 0
	feishuAcceptFixture(t, router, &calls, 0, &mu, &messagesCalled)
	svc := NewWebhookService()
	t.Cleanup(func() { svc.Stop(context.Background()) })

	sent, sendErr := svc.sendOutbound(context.Background(), ChannelFeishu, "101",
		&ParsedPayload{EventID: "evt-sa-fs-card", Sender: "ou_sa", Content: "在吗"},
		"卡片正文", nil, []model.RichCard{{
			Title: "报价", Description: "acceptance 卡片",
			Buttons: []model.CardButton{{Text: "查看", URL: "https://example.com/sa"}},
		}})
	if !sent || sendErr != nil {
		t.Fatalf("feishu 卡片出站应成功: sent=%v err=%v", sent, sendErr)
	}
	got := senderReadCalls(&calls, &mu)
	var cardBody map[string]any
	for i := range got {
		if got[i].Body == nil {
			continue
		}
		if b, ok := got[i].Body["msg_type"].(string); ok && b == "interactive" {
			cardBody = got[i].Body
		}
	}
	if cardBody == nil {
		t.Fatalf("应有一次 interactive 卡片调用: %+v", got)
	}
	raw, _ := json.Marshal(cardBody["content"])
	if !strings.Contains(string(raw), "i18n_elements") || !strings.Contains(string(raw), "acceptance 卡片") {
		t.Errorf("interactive 卡片 JSON 结构缺失（i18n_elements/标题），got %s", string(raw))
	}
}

func TestFeishuSender_TextFailStillSendsCards(t *testing.T) {
	releaseReplyClaimForTest(t, "evt-sa-fs-fail")
	router := newGoldenRouter(t)
	var mu sync.Mutex
	calls := make([]goldenCall, 0, 4)
	messagesCalled := 0
	feishuAcceptFixture(t, router, &calls, 230013, &mu, &messagesCalled)
	svc := NewWebhookService()
	t.Cleanup(func() { svc.Stop(context.Background()) })

	sent, _ := svc.sendOutbound(context.Background(), ChannelFeishu, "101",
		&ParsedPayload{EventID: "evt-sa-fs-fail", Sender: "ou_sa", Content: "在吗"},
		"正文（将失败）", nil, []model.RichCard{{Title: "仍要发", Buttons: []model.CardButton{{Text: "看", URL: "https://example.com/x"}}}})
	got := senderReadCalls(&calls, &mu)
	interactive := 0
	for i := range got {
		if got[i].Body == nil {
			continue
		}
		if b, _ := got[i].Body["msg_type"].(string); b == "interactive" {
			interactive++
		}
	}
	if interactive != 1 {
		t.Fatalf("文本失败后卡片仍必须被调用 1 次，实际 %d（calls=%+v）", interactive, got)
	}
	if !sent {
		t.Fatal("正文失败但卡片成功 ⇒ 整体 sent=true（搬迁前顺序契约）")
	}
}

// ── T104 telegram ───────────────────────────────────────────────────────

func telegramAcceptFixture(t *testing.T, router *goldenRouter, calls *[]goldenCall) {
	senderTestDB(t)
	if err := senderTestDB(t).Create(&model.TelegramAccount{ID: 102, TenantID: 1, AccountName: "sa-tg", BotToken: "tok-sa", Status: 1}).Error; err != nil {
		t.Fatalf("seed telegram: %v", err)
	}
	senderCapture(t, router, "api.telegram.org", calls, map[string]func(*http.Request) (int, string){
		"/bottok-sa/sendMessage": func(*http.Request) (int, string) {
			return 200, `{"ok":true,"result":{"message_id":881}}`
		},
	})
}

func TestTelegramSender_GroupMention(t *testing.T) {
	releaseReplyClaimForTest(t, "evt-sa-tg-group")
	router := newGoldenRouter(t)
	var mu sync.Mutex
	calls := make([]goldenCall, 0, 2)
	telegramAcceptFixture(t, router, &calls)
	svc := NewWebhookService()
	t.Cleanup(func() { svc.Stop(context.Background()) })

	ctx := TelegramReplyMetaToContext(context.Background(), &TelegramReplyMeta{
		FromUsername: "golden_user", FromName: "Golden", FromUserID: 555001,
		ReplyToMsgID: 4242, TriggerReason: "mention",
	})
	hub := &model.MessageHub{Platform: "telegram", ConversationID: "-100555001", IsGroup: true, SenderID: "555001"}
	sent, sendErr := svc.sendOutbound(ctx, ChannelTelegram, "102",
		&ParsedPayload{EventID: "evt-sa-tg-group", Sender: "555001", Content: "在吗"},
		"群聊回复正文", hub, nil)
	if !sent || sendErr != nil {
		t.Fatalf("群聊出站应成功: sent=%v err=%v", sent, sendErr)
	}
	got := senderReadCalls(&calls, &mu)
	if len(got) != 1 || got[0].Body == nil {
		t.Fatalf("应捕获 1 次 sendMessage: %+v", got)
	}
	if pm, _ := got[0].Body["parse_mode"].(string); pm != "HTML" {
		t.Errorf("parse_mode 应为 HTML，got %v", got[0].Body["parse_mode"])
	}
	rp, _ := got[0].Body["reply_parameters"].(map[string]any)
	if rp == nil {
		// 兼容旧形态 reply_to_message_id
		if v, _ := got[0].Body["reply_to_message_id"].(float64); v == 4242 {
			return
		}
		t.Fatalf("群聊回复应携带 reply_parameters（或 reply_to_message_id=4242），got %v", got[0].Body)
	}
	if id, _ := rp["message_id"].(float64); id != 4242 {
		t.Errorf("reply_parameters.message_id 应为 4242，got %v", rp["message_id"])
	}
}

func TestTelegramSender_Cards(t *testing.T) {
	releaseReplyClaimForTest(t, "evt-sa-tg-card")
	router := newGoldenRouter(t)
	var mu sync.Mutex
	calls := make([]goldenCall, 0, 4)
	telegramAcceptFixture(t, router, &calls)
	svc := NewWebhookService()
	t.Cleanup(func() { svc.Stop(context.Background()) })

	sent, sendErr := svc.sendOutbound(context.Background(), ChannelTelegram, "102",
		&ParsedPayload{EventID: "evt-sa-tg-card", Sender: "555001", Content: "在吗"},
		"卡片正文", &model.MessageHub{Platform: "telegram", ConversationID: "555001", SenderID: "555001"},
		[]model.RichCard{{Title: "工单", Buttons: []model.CardButton{{Text: "查看", URL: "https://example.com/tg-card"}}}})
	if !sent || sendErr != nil {
		t.Fatalf("卡片出站应成功: sent=%v err=%v", sent, sendErr)
	}
	got := senderReadCalls(&calls, &mu)
	hasKeyboard := false
	for i := range got {
		if got[i].Body == nil {
			continue
		}
		if _, ok := got[i].Body["reply_markup"].(map[string]any); ok {
			hasKeyboard = true
		}
	}
	if !hasKeyboard {
		t.Fatalf("卡片调用应携带 reply_markup inline_keyboard: %+v", got)
	}
}

// ── T105 qq ─────────────────────────────────────────────────────────────

func qqAcceptFixture(t *testing.T, router *goldenRouter, calls *[]goldenCall) {
	senderTestDB(t)
	if err := senderTestDB(t).Create(&model.QQAccount{ID: 105, TenantID: 1, AppID: "qq-sa", AppSecret: "qq-sec-sa"}).Error; err != nil {
		t.Fatalf("seed qq: %v", err)
	}
	senderCapture(t, router, "api.bot.qq.com", calls, map[string]func(*http.Request) (int, string){
		"/app/getAppAccessToken": func(*http.Request) (int, string) {
			return 200, `{"code":0,"message":"ok","access_token":"qq-sa-token","expires_in":7200}`
		},
		"/v2/users/cs-sa-qq/messages": func(*http.Request) (int, string) {
			return 200, `{"code":0,"data":{"msg_id":"qq-sa-out"}}`
		},
	})
}

func qqAcceptSend(t *testing.T, svc *WebhookService, evtID, msgID string) (bool, error) {
	t.Helper()
	releaseReplyClaimForTest(t, evtID)
	hub := &model.MessageHub{Platform: "qq", ConversationID: "cs-sa-qq", MsgID: msgID,
		SenderID: "cust-sa-qq", Extra: model.JSONMap{"channel_msg_id": msgID}}
	return svc.sendOutbound(context.Background(), ChannelQQ, "105",
		&ParsedPayload{EventID: evtID, Sender: "cust-sa-qq", Content: "在吗"},
		"QQ 正文", hub, nil)
}

func TestQQSender_PassiveWindowMsgSeq(t *testing.T) {
	router := newGoldenRouter(t)
	var mu sync.Mutex
	calls := make([]goldenCall, 0, 16)
	qqAcceptFixture(t, router, &calls)
	svc := NewWebhookService()
	t.Cleanup(func() { svc.Stop(context.Background()) })

	// 连续 6 条同一原消息：msg_seq 单调 1→6，被动回复始终携带 msg_id（窗口限额由平台侧执法，
	// 与原实现一致——本仓不做本地降级，seq 持续单调即可与搬迁前逐字节对齐）。
	seqs := make([]float64, 0, 6)
	for i := 1; i <= 6; i++ {
		sent, err := qqAcceptSend(t, svc, "evt-sa-qq-pw-"+string(rune('a'+i)), "qq-msg-sa-passive")
		if !sent || err != nil {
			t.Fatalf("第 %d 条应成功: sent=%v err=%v", i, sent, err)
		}
		got := senderReadCalls(&calls, &mu)
		var last map[string]any
		for j := range got {
			if strings.Contains(got[j].Path, "/v2/users/") {
				last = got[j].Body
			}
		}
		if last == nil {
			t.Fatalf("第 %d 条未捕获发送调用", i)
		}
		if v, _ := last["msg_seq"].(float64); v != float64(i) {
			t.Fatalf("第 %d 条 msg_seq 应为 %d，got %v", i, i, last["msg_seq"])
		}
		if last["msg_id"] == nil || last["msg_id"] == "" {
			t.Fatalf("第 %d 条被动回复必须携带 msg_id", i)
		}
		seqs = append(seqs, last["msg_seq"].(float64))
	}
	for i := 1; i < len(seqs); i++ {
		if seqs[i] <= seqs[i-1] {
			t.Fatalf("msg_seq 必须单调递增: %v", seqs)
		}
	}
}

func TestQQSender_SingletonSeqMonotonic(t *testing.T) {
	router := newGoldenRouter(t)
	var mu sync.Mutex
	calls := make([]goldenCall, 0, 8)
	qqAcceptFixture(t, router, &calls)
	svc := NewWebhookService()
	t.Cleanup(func() { svc.Stop(context.Background()) })

	// 同一 service（惰性单例 integration）两次发送同 msg_id：seq 必须 1→2（不能每次新建实例归 1）。
	if _, err := qqAcceptSend(t, svc, "evt-sa-qq-single-1", "qq-msg-sa-single"); err != nil {
		t.Fatalf("第一次发送失败: %v", err)
	}
	if _, err := qqAcceptSend(t, svc, "evt-sa-qq-single-2", "qq-msg-sa-single"); err != nil {
		t.Fatalf("第二次发送失败: %v", err)
	}
	got := senderReadCalls(&calls, &mu)
	seq1, seq2 := float64(0), float64(0)
	for i := range got {
		if !strings.Contains(got[i].Path, "/v2/users/") || got[i].Body == nil {
			continue
		}
		if v, _ := got[i].Body["msg_seq"].(float64); v > 0 {
			if seq1 == 0 {
				seq1 = v
			} else if seq2 == 0 {
				seq2 = v
			}
		}
	}
	if seq1 != 1 || seq2 != 2 {
		t.Fatalf("同实例同 msg_id 的 seq 应 1→2，实际 %v → %v", seq1, seq2)
	}
}

// ── T106 whatsapp ───────────────────────────────────────────────────────

func whatsappAcceptFixture(t *testing.T, database *gorm.DB, router *goldenRouter, calls *[]goldenCall) {
	if err := database.Create(&model.WhatsAppCloudAccount{ID: 106, TenantID: 1, AccountName: "sa-wa",
		PhoneNumberID: "phone-sa", AccessToken: "wa-tok-sa", Status: 1}).Error; err != nil {
		t.Fatalf("seed whatsapp: %v", err)
	}
	senderCapture(t, router, "graph.facebook.com", calls, map[string]func(*http.Request) (int, string){
		"/v21.0/phone-sa/messages": func(*http.Request) (int, string) {
			return 200, `{"messages":[{"id":"wamid.sa001"}]}`
		},
	})
}

func TestWhatsappSender_PayloadShape(t *testing.T) {
	releaseReplyClaimForTest(t, "evt-sa-wa")
	router := newGoldenRouter(t)
	var mu sync.Mutex
	calls := make([]goldenCall, 0, 2)
	whatsappAcceptFixture(t, senderTestDB(t), router, &calls)
	svc := NewWebhookService()
	t.Cleanup(func() { svc.Stop(context.Background()) })

	sent, sendErr := svc.sendOutbound(context.Background(), ChannelWhatsapp, "106",
		&ParsedPayload{EventID: "evt-sa-wa", Sender: "8613800000000", Content: "在吗"},
		"wa 正文", &model.MessageHub{Platform: "whatsapp", ConversationID: "cs-sa-wa"}, nil)
	if !sent || sendErr != nil {
		t.Fatalf("whatsapp 出站应成功: sent=%v err=%v", sent, sendErr)
	}
	got := senderReadCalls(&calls, &mu)
	if len(got) != 1 || got[0].Body == nil {
		t.Fatalf("应捕获 1 次 messages 调用: %+v", got)
	}
	if got[0].Body["messaging_product"] != "whatsapp" || got[0].Body["type"] != "text" {
		t.Errorf("payload 形状漂移: %v", got[0].Body)
	}
	if to, _ := got[0].Body["to"].(string); to != "8613800000000" {
		t.Errorf("to 应为 8613800000000，got %v", got[0].Body["to"])
	}
}

func whatsappSeedOldInbound(t *testing.T, database *gorm.DB, conv string) {
	t.Helper()
	hub := &model.MessageHub{Platform: "whatsapp", AccountID: "106", Direction: "inbound",
		MsgID: "mh-sa-wa-in-" + conv, SenderID: "8613800000000", Content: "问",
		ConversationID: conv, SentAt: time.Now().Add(-25 * time.Hour)}
	if err := database.Create(hub).Error; err != nil {
		t.Fatalf("seed 25h 前入站行: %v", err)
	}
}

func TestWhatsappSender_WindowExpired(t *testing.T) {
	database := senderTestDB(t)
	whatsappSeedOldInbound(t, database, "cs-sa-wa-exp")
	releaseReplyClaimForTest(t, "evt-sa-wa-exp")
	router := newGoldenRouter(t)
	var mu sync.Mutex
	calls := make([]goldenCall, 0, 2)
	whatsappAcceptFixture(t, database, router, &calls)
	senderCapture(t, router, "graph.facebook.com", &calls, map[string]func(*http.Request) (int, string){
		"/v21.0/phone-sa/messages": func(*http.Request) (int, string) {
			return 200, `{"messages":[{"id":"wamid.never"}]}`
		},
	})
	svc := NewWebhookService()
	t.Cleanup(func() { svc.Stop(context.Background()) })

	sent, sendErr := svc.sendOutbound(context.Background(), ChannelWhatsapp, "106",
		&ParsedPayload{EventID: "evt-sa-wa-exp", Sender: "8613800000000", Content: "在吗"},
		"超窗正文", &model.MessageHub{Platform: "whatsapp", ConversationID: "cs-sa-wa-exp"}, nil)
	got := senderReadCalls(&calls, &mu)
	if got := len(got); got != 0 {
		t.Fatalf("超 24h 窗且未配模板：零 HTTP 请求，实际 %d", got)
	}
	if sent {
		t.Fatal("超窗无模板不得判成功")
	}
	ce := AsChannelError(sendErr)
	if ce == nil || ce.Category != CategoryWindowExpired {
		t.Fatalf("应为 window_expired 失败，got %+v", sendErr)
	}
}

func TestWhatsappSender_WindowExpiredWithTemplate(t *testing.T) {
	database := senderTestDB(t)
	whatsappSeedOldInbound(t, database, "cs-sa-wa-tpl")
	releaseReplyClaimForTest(t, "evt-sa-wa-tpl")
	router := newGoldenRouter(t)
	var mu sync.Mutex
	calls := make([]goldenCall, 0, 2)
	whatsappAcceptFixture(t, database, router, &calls)
	svc := NewWebhookService()
	t.Cleanup(func() { svc.Stop(context.Background()) })

	// 注入回退模板参数（mode off，无租户前缀，桩键与读取键一致）
	stub := &ConfigParamService{
		cache:  map[string]paramEntry{"channelbot.whatsapp_fallback_template": {value: "golden_sa_tpl", expiresAt: time.Now().Add(time.Hour)}},
		loaded: map[string]bool{}, nowFn: time.Now,
	}
	prev := GlobalConfigParam()
	SetGlobalForTest(stub)
	t.Cleanup(func() { SetGlobalForTest(prev) })

	sent, sendErr := svc.sendOutbound(context.Background(), ChannelWhatsapp, "106",
		&ParsedPayload{EventID: "evt-sa-wa-tpl", Sender: "8613800000000", Content: "在吗"},
		"超窗模板正文", &model.MessageHub{Platform: "whatsapp", ConversationID: "cs-sa-wa-tpl"}, nil)
	if !sent || sendErr != nil {
		t.Fatalf("超窗+模板应按模板发送成功: sent=%v err=%v", sent, sendErr)
	}
	got := senderReadCalls(&calls, &mu)
	if len(got) != 1 || got[0].Body == nil {
		t.Fatalf("应捕获 1 次模板调用: %+v", got)
	}
	if got[0].Body["type"] != "template" {
		t.Fatalf("超窗正文必须降级为模板消息，got %v", got[0].Body["type"])
	}
	tpl, _ := got[0].Body["template"].(map[string]any)
	if tpl == nil || tpl["name"] != "golden_sa_tpl" {
		t.Errorf("template.name 应为 golden_sa_tpl，got %v", got[0].Body["template"])
	}
}

// ── T107 dingtalk ───────────────────────────────────────────────────────

func dingtalkSend(t *testing.T, svc *WebhookService, evtID, webhookURL string, expiredAt int64) (bool, error, *[]goldenCall, *sync.Mutex) {
	t.Helper()
	releaseReplyClaimForTest(t, evtID)
	hub := &model.MessageHub{Platform: "dingtalk", ConversationID: "cs-sa-dt",
		Extra: model.JSONMap{"session_webhook": webhookURL, "session_webhook_expired_at": expiredAt}}
	var mu sync.Mutex
	calls := make([]goldenCall, 0, 2)
	callsPtr := &calls
	sent, sendErr := svc.sendOutbound(context.Background(), ChannelDingTalk, "107",
		&ParsedPayload{EventID: evtID, Sender: "staff", Content: "在吗"},
		"dingtalk 正文", hub, nil)
	return sent, sendErr, callsPtr, &mu
}

func TestDingtalkSender_RejectsNonAllowlistedHost(t *testing.T) {
	router := newGoldenRouter(t) // 不放行 any host：默认白名单只认 *.dingtalk.com
	var mu sync.Mutex
	calls := make([]goldenCall, 0, 2)
	// 域外主机若被请求即测试失败（零请求断言的牙齿）
	router.serve(t, "evil.example.com", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, goldenCall{Method: r.Method, Path: r.URL.Path})
		mu.Unlock()
		t.Errorf("域外主机不得被请求（allowlist 失守）")
	}))
	svc := &WebhookService{}
	sent, sendErr, ptr, _ := dingtalkSend(t, svc, "evt-sa-dt-host",
		"https://evil.example.com/robot/send?access_token=x", time.Now().Add(time.Hour).Unix())
	mu.Lock()
	defer mu.Unlock()
	if len(*ptr) != 0 {
		t.Fatalf("必须零请求，实际 %d", len(*ptr))
	}
	if sent || sendErr == nil {
		t.Fatalf("域外主机必须明确失败: sent=%v err=%v", sent, sendErr)
	}
	if ce := AsChannelError(sendErr); ce == nil || ce.Category != CategoryBadRequest {
		t.Fatalf("域外主机应为 bad_request，got %+v", sendErr)
	}
}

func TestDingtalkSender_ExpiredWebhook(t *testing.T) {
	router := newGoldenRouter(t)
	var mu sync.Mutex
	calls := make([]goldenCall, 0, 2)
	router.serve(t, "goldendt.example.com", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, goldenCall{Method: r.Method, Path: r.URL.Path})
		mu.Unlock()
	}))
	svc := &WebhookService{}
	sent, sendErr, ptr, _ := dingtalkSend(t, svc, "evt-sa-dt-exp",
		"https://goldendt.example.com/robot/send", time.Now().Add(-time.Hour).Unix())
	mu.Lock()
	defer mu.Unlock()
	if len(*ptr) != 0 {
		t.Fatalf("过期 webhook 必须零请求，实际 %d", len(*ptr))
	}
	if sent {
		t.Fatal("过期 webhook 不得判成功")
	}
	ce := AsChannelError(sendErr)
	if ce == nil || ce.Category != CategoryWindowExpired {
		t.Fatalf("应为 window_expired，got %+v", sendErr)
	}
}

func TestDingtalkSender_ErrcodeNonZero(t *testing.T) {
	allowAnyDingtalkHostForTest(t)
	releaseReplyClaimForTest(t, "evt-sa-dt-err")
	router := newGoldenRouter(t)
	var mu sync.Mutex
	calls := make([]goldenCall, 0, 2)
	senderCapture(t, router, "goldendt.example.com", &calls, map[string]func(*http.Request) (int, string){
		"/goldendt": func(*http.Request) (int, string) {
			return 200, `{"errcode":310000,"errmsg":"session webhook has expired"}`
		},
	})
	svc := &WebhookService{}
	sent, sendErr, _, _ := dingtalkSend(t, svc, "evt-sa-dt-err",
		"https://goldendt.example.com/goldendt", time.Now().Add(time.Hour).Unix())
	got := senderReadCalls(&calls, &mu)
	if len(got) != 1 {
		t.Fatalf("errcode 拒绝前必须真实发出请求（1 次），实际 %d", len(got))
	}
	if sent {
		t.Fatal("errcode≠0 不得判成功")
	}
	if AsChannelError(sendErr) == nil {
		t.Fatalf("必须回传 *ChannelError，got %T", sendErr)
	}
}

// ── T108 wechat ─────────────────────────────────────────────────────────

func wechatAcceptFixture(t *testing.T, router *goldenRouter, calls *[]goldenCall) {
	senderTestDB(t)
	if err := senderTestDB(t).Create(&model.WechatAccount{ID: 103, TenantID: 1, AppID: "wx_sa", AppSecret: "wx-sec-sa", Status: "active"}).Error; err != nil {
		t.Fatalf("seed wechat: %v", err)
	}
	senderCapture(t, router, "api.weixin.qq.com", calls, map[string]func(*http.Request) (int, string){
		"/cgi-bin/token": func(*http.Request) (int, string) {
			return 200, `{"access_token":"wx-sa-token","expires_in":7200}`
		},
		"/cgi-bin/message/custom/send": func(*http.Request) (int, string) {
			return 200, `{"errcode":0,"errmsg":"ok"}`
		},
	})
}

func TestWechatSender_CustomerServiceMessage(t *testing.T) {
	releaseReplyClaimForTest(t, "evt-sa-wx")
	router := newGoldenRouter(t)
	var mu sync.Mutex
	calls := make([]goldenCall, 0, 4)
	wechatAcceptFixture(t, router, &calls)
	svc := NewWebhookService()
	t.Cleanup(func() { svc.Stop(context.Background()) })

	sent, sendErr := svc.sendOutbound(context.Background(), ChannelWechat, "103",
		&ParsedPayload{EventID: "evt-sa-wx", Sender: "openid-sa", Content: "在吗"},
		"客服正文", nil, nil)
	if !sent || sendErr != nil {
		t.Fatalf("公众号客服消息应成功: sent=%v err=%v", sent, sendErr)
	}
	got := senderReadCalls(&calls, &mu)
	var send *goldenCall
	for i := range got {
		if strings.Contains(got[i].Path, "/cgi-bin/message/custom/send") {
			send = &got[i]
		}
	}
	if send == nil {
		t.Fatalf("未捕获 custom/send: %+v", got)
	}
	if send.Body["touser"] != "openid-sa" || send.Body["msgtype"] != "text" {
		t.Errorf("客服消息形状漂移: %v", send.Body)
	}
}

func TestWechatSender_BadAccountIDDoesNotShortCircuit(t *testing.T) {
	releaseReplyClaimForTest(t, "evt-sa-wx-bad")
	router := newGoldenRouter(t)
	var mu sync.Mutex
	calls := make([]goldenCall, 0, 4)
	wechatAcceptFixture(t, router, &calls)
	svc := NewWebhookService()
	t.Cleanup(func() { svc.Stop(context.Background()) })

	// accountID 非法（"abc"→0）不得短路失败：原实现回退 GetFirstActiveAccount 真发。
	sent, sendErr := svc.sendOutbound(context.Background(), ChannelWechat, "abc",
		&ParsedPayload{EventID: "evt-sa-wx-bad", Sender: "openid-sa", Content: "在吗"},
		"回退正文", nil, nil)
	if !sent || sendErr != nil {
		t.Fatalf("非法 accountID 应回退活跃账号真发，不得短路: sent=%v err=%v", sent, sendErr)
	}
	got := senderReadCalls(&calls, &mu)
	n := 0
	for i := range got {
		if strings.Contains(got[i].Path, "/cgi-bin/message/custom/send") {
			n++
		}
	}
	_ = n
	if n != 1 {
		t.Fatalf("应真实发出 1 条客服消息，实际 %d", n)
	}
}

// ── T109 bridge ─────────────────────────────────────────────────────────

func bridgeAcceptSvc(t *testing.T) *WebhookService {
	t.Helper()
	svc := NewWebhookService()
	t.Cleanup(func() { svc.Stop(context.Background()) })
	return svc
}

func TestBridgeSender_DuplicateOccupied(t *testing.T) {
	database := senderTestDB(t) // 必须先于 svc：仓储构造时捕获全局句柄
	setupBridgeWhitelistForTest(t, "douyin")
	releaseReplyClaimForTest(t, "evt-sa-br-dup")
	svc := bridgeAcceptSvc(t)

	const conv = "cs-sa-br-dup"
	hub := bridgeInboundHub("douyin", "acct-sa-br", conv, "cust-sa-br")
	if err := database.Create(hub).Error; err != nil {
		t.Fatalf("seed inbound: %v", err)
	}
	content := "golden 桥接重复正文"
	dup := &model.MessageHub{
		MsgID: ContentHashMsgID("douyin", conv, content),
		Platform: "douyin", AccountID: "acct-sa-br", Direction: "outbound",
		Status: "delivered", MsgType: "text", SenderID: "acct-sa-br",
		ReceiverID: hub.SenderID, Content: content,
		ConversationID: conv, IsAIReply: true, SentAt: time.Now(),
	}
	if err := database.Create(dup).Error; err != nil {
		t.Fatalf("seed duplicate: %v", err)
	}

	bridgeCtx := tenant.WithTenantID(context.Background(), 1) // 占位/审计行需要租户（fail-closed）
	sent, sendErr := svc.sendOutbound(bridgeCtx, ChannelDouyin, "acct-sa-br",
		&ParsedPayload{EventID: "evt-sa-br-dup", Sender: hub.SenderID, Content: "在吗", ChatID: conv},
		content, hub, nil)
	if sent {
		t.Fatal("重复占位不得判成功")
	}
	ce := AsChannelError(sendErr)
	if ce == nil || ce.Retryable {
		t.Fatalf("重复占位应为不可重试失败，got %+v", sendErr)
	}
	var auditRow model.MessageHub
	if err := database.Where("platform = ? AND conversation_id = ? AND status = ?", "douyin", conv, "failed").
		Order("id desc").First(&auditRow).Error; err != nil {
		t.Fatalf("应落 failed 占位行（坐席可见）: %v", err)
	}
	if auditRow.Extra["duplicate_reason"] == nil {
		t.Errorf("占位行应携带 duplicate_reason，Extra=%v", auditRow.Extra)
	}
}

func TestBridgeSender_UndeliverableReason(t *testing.T) {
	database := senderTestDB(t) // 必须先于 svc：仓储构造时捕获全局句柄
	setupBridgeWhitelistForTest(t, "xiaohongshu")
	releaseReplyClaimForTest(t, "evt-sa-br-undel")
	svc := bridgeAcceptSvc(t)

	conv := "cs-sa-br-undel"
	hub := bridgeInboundHub("xiaohongshu", "xiaohongshu-unknown", conv, "cust-sa-br-undel")
	if err := database.Create(hub).Error; err != nil {
		t.Fatalf("seed inbound: %v", err)
	}
	bridgeCtx := tenant.WithTenantID(context.Background(), 1)
	sent, sendErr := svc.sendOutbound(bridgeCtx, ChannelXiaohongshu, hub.AccountID,
		&ParsedPayload{EventID: "evt-sa-br-undel", Sender: hub.SenderID, Content: "在吗", ChatID: conv},
		"不可达正文", hub, nil)
	if sent {
		t.Fatal("占位账号不得判成功")
	}
	ce := AsChannelError(sendErr)
	if ce == nil || ce.Retryable {
		t.Fatalf("占位账号失败不可重试，got %+v", sendErr)
	}
	var row model.MessageHub
	if err := database.Where("platform = ? AND conversation_id = ? AND direction = ?", "xiaohongshu", conv, "outbound").
		Order("id desc").First(&row).Error; err != nil {
		t.Fatalf("应落出站行: %v", err)
	}
	if row.Status != "failed" || row.Extra["undeliverable_reason"] != "placeholder-account" {
		t.Errorf("应标 failed 且携带 undeliverable_reason，status=%s Extra=%v", row.Status, row.Extra)
	}
}

func TestBridgeSender_RetryCarriesStrippedContent(t *testing.T) {
	database := senderTestDB(t) // 必须先于 svc：仓储构造时捕获全局句柄
	setupBridgeWhitelistForTest(t, "douyin")
	releaseReplyClaimForTest(t, "evt-sa-br-strip")
	svc := bridgeAcceptSvc(t)

	md := "**限时优惠**：详情见 [产品页](https://example.com/p)\n- 第一步\n- 第二步"
	stripped := StripMarkdownForDM(md)
	if stripped == md {
		t.Fatal("对照失效：StripMarkdownForDM 对样例无变化")
	}
	// 超长会话号触发 message_hub 出站行写入失败（varchar 100）→ 可重试失败 → 重试行入队。
	// 入站 hub 行只在内存里造（不落库）——先撞同一个长度限制就轮不到出库失败分支（b5 同款）。
	longConv := "cs-sa-br-strip-" + strings.Repeat("x", 110)
	hub := bridgeInboundHub("douyin", "acct-sa-br", longConv, "cust-sa-br-strip")
	bridgeCtx := tenant.WithTenantID(context.Background(), 1)
	sent, sendErr := svc.sendOutbound(bridgeCtx, ChannelDouyin, "acct-sa-br",
		&ParsedPayload{EventID: "evt-sa-br-strip", Sender: hub.SenderID, Content: "在吗", ChatID: longConv},
		md, hub, nil)
	if sent {
		t.Fatal("落库失败不得判成功")
	}
	if ce := AsChannelError(sendErr); ce == nil || !ce.Retryable {
		t.Fatalf("落库失败应可重试，got %+v", sendErr)
	}
	var rec DelayedOutboundReply
	if err := database.Where("kind = ? AND conversation_id = ?", model.DelayedKindSendRetry, longConv).
		Order("id desc").First(&rec).Error; err != nil {
		t.Fatalf("应入持久化重试队列: %v", err)
	}
	if rec.Content != stripped {
		t.Fatalf("重试入队必须携带清洗后文本：want %q got %q", stripped, rec.Content)
	}
}
