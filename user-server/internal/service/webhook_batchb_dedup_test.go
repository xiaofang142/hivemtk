package service

// 批B S-04：入站幂等必须按「官方文档明示的去重键」，而不是整包字节哈希。
//
// 修复前（审计 §5 S-04 坐实）：ParsedPayload.EventID 只在 webhook.go 两处赋值——
// 顶层 JSON 的 event_id/EventID/msg_id/MsgId，以及整包 body 的 sha256 兜底。
// 各渠道官方文档给出的重复投递判定键（TG update_id、QQ 信封 id、飞书
// header.event_id、企微 MsgId、WA wamid）全都取不到，导致缓存层与
// webhook_events 永久层都在按「字节是否完全一样」判重放：渠道重试时重排字段、
// 补写一个键，同一条客户消息就会被当成新事件再跑一次 AI。
//
// 现在 Receive 入口按 officialEventID 取官方键（账号作用域前缀），取不到才退回
// 整包哈希；加解密渠道（飞书/企微密文态）的官方键由 dispatch 层落到
// message_hub.msg_id 兜底，本文件末两条用例分别坐实这两层。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"
	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func TestOfficialEventID_PerChannel(t *testing.T) {
	cases := []struct {
		name    string
		channel WebhookChannel
		account string
		body    string
		want    string
	}{
		{"telegram update_id", ChannelTelegram, "7", `{"update_id":5001,"message":{"message_id":3,"text":"hi"}}`, "telegram:7:upd-5001"},
		{"telegram update_id 为字符串", ChannelTelegram, "7", `{"update_id":"5002"}`, "telegram:7:upd-5002"},
		{"telegram 缺 update_id", ChannelTelegram, "7", `{"message":{"message_id":3}}`, ""},
		{"telegram update_id=0 不是合法键", ChannelTelegram, "7", `{"update_id":0}`, ""},
		{"qq 信封 id", ChannelQQ, "3", `{"id":"evt-abc","op":0,"d":{"id":"m1"}}`, "qq:3:evt-evt-abc"},
		{"飞书 v2 header.event_id", ChannelFeishu, "9", `{"schema":"2.0","header":{"event_id":"ev-1","event_type":"im.message.receive_v1"}}`, "feishu:9:ev-1"},
		{"飞书 v1 uuid", ChannelFeishu, "9", `{"uuid":"u-1","type":"event_callback","event":{}}`, "feishu:9:u-1"},
		{"企微 MsgId", ChannelWeCom, "5", `{"MsgId":"4285185416602254651","MsgType":"text"}`, "wecom:5:4285185416602254651"},
		{"whatsapp wamid", ChannelWhatsapp, "11", `{"object":"whatsapp_business_account","entry":[{"changes":[{"value":{"messages":[{"id":"wamid.HBgLMTg5MTIw","type":"text"}]}}]}]}`, "whatsapp:11:wamid-"},
		// 同一 payload 里两条 wamid：换序不得变成另一个键（否则批次重投判不出重复）
		{"whatsapp wamid 与顺序无关", ChannelWhatsapp, "11", `{"entry":[{"changes":[{"value":{"messages":[{"id":"wamid.B"},{"id":"wamid.A"}]}}]}]}`, "whatsapp:11:wamid-"},
		{"钉钉不在 webhook 层去重（独立入口）", ChannelDingTalk, "1", `{"msgId":"m-1"}`, ""},
		{"非 JSON 体", ChannelTelegram, "1", `not-json`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := officialEventID(tc.channel, tc.account, []byte(tc.body))
			if tc.want == "" {
				if got != "" {
					t.Fatalf("取不到官方键时必须回空串交给兜底，got %q", got)
				}
				return
			}
			if !strings.HasPrefix(got, tc.want) {
				t.Fatalf("EventID = %q, want prefix %q", got, tc.want)
			}
		})
	}
}

// whatsapp 的官方键只在「同一批 wamid 集合」上有意义：换序同集合必须同键，
// 集合不同必须不同键（否则会把两条不同消息判成重复而丢消息）。
func TestOfficialEventID_WhatsappWamidSet(t *testing.T) {
	mk := func(ids ...string) string {
		msgs := make([]any, 0, len(ids))
		for _, id := range ids {
			msgs = append(msgs, map[string]any{"id": id, "type": "text"})
		}
		raw, _ := json.Marshal(map[string]any{
			"entry": []any{map[string]any{"changes": []any{map[string]any{"value": map[string]any{"messages": msgs}}}}},
		})
		return string(raw)
	}
	a := officialEventID(ChannelWhatsapp, "1", []byte(mk("wamid.A", "wamid.B")))
	b := officialEventID(ChannelWhatsapp, "1", []byte(mk("wamid.B", "wamid.A")))
	c := officialEventID(ChannelWhatsapp, "1", []byte(mk("wamid.A")))
	if a == "" {
		t.Fatal("两条 wamid 应能组成官方键")
	}
	if a != b {
		t.Errorf("同批 wamid 换序应得同一去重键：%q != %q", a, b)
	}
	if a == c {
		t.Errorf("不同 wamid 集合必须得不同键，否则第二条消息会被判重复丢弃")
	}
}

// ---------------------------------------------------------------------------
// Receive 级：同一官方事件在不同字节下仍判重复
// ---------------------------------------------------------------------------

// 显式大主键：全量测试同进程共享自增序列，撞号会让账号归属漂移。
const (
	dedupAccountA = uint(930001)
	dedupAccountB = uint(930002)
)

func setupDedupEnv(t *testing.T) *WebhookService {
	t.Helper()
	db := testutil.NewTestDBOrSkip(t,
		&model.TelegramAccount{},
		&model.WebhookEvent{},
		&model.MessageHub{},
		&model.InboxConversation{},
		&model.UnifiedMessage{},
		&model.IntegrationAccount{},
		&model.CustomerSession{},
		&model.SessionMessage{},
		&model.AgentStatus{},
	)
	dbutil.SetTestDB(db)
	t.Cleanup(func() { dbutil.SetTestDB(nil) })
	for _, seed := range []struct {
		id   uint
		name string
	}{{dedupAccountA, "dedup-a"}, {dedupAccountB, "dedup-b"}} {
		acc := &model.TelegramAccount{
			ID: seed.id, AccountName: seed.name, BotToken: fmt.Sprintf("%d:TOKEN-%s", seed.id, seed.name),
			WebhookSecret: "whsec-" + seed.name, Status: 1,
		}
		if err := db.Create(acc).Error; err != nil {
			t.Fatalf("seed telegram account %s: %v", seed.name, err)
		}
	}
	svc := NewWebhookService(db)
	t.Cleanup(func() { svc.Stop(context.Background()) })
	return svc
}

func tgReceive(t *testing.T, svc *WebhookService, accountID uint, body string) *ReceiveResult {
	t.Helper()
	return tgReceiveNamed(t, svc, accountID, body, "dedup-a")
}

func tgReceiveNamed(t *testing.T, svc *WebhookService, accountID uint, body, name string) *ReceiveResult {
	t.Helper()
	res, err := svc.Receive(context.Background(), &ReceiveRequest{
		Channel:   ChannelTelegram,
		AccountID: fmt.Sprintf("%d", accountID),
		Body:      []byte(body),
		Headers:   map[string]string{"X-Telegram-Bot-Api-Secret-Token": "whsec-" + name},
	})
	if err != nil {
		t.Fatalf("telegram Receive: %v", err)
	}
	return res
}

func TestReceive_Telegram_OfficialUpdateIDDedupsReencodedReplay(t *testing.T) {
	svc := setupDedupEnv(t)

	first := `{"update_id":90001,"message":{"message_id":1,"date":1700000000,"chat":{"id":910001,"type":"private"},"from":{"id":1,"first_name":"u"},"text":"重复投递测试"}}`
	// 同一条 update 的另一种字节表示（键序/空白变化即足以骗过整包哈希）
	replay := `{"message":{"text":"重复投递测试","from":{"first_name":"u","id":1},"chat":{"type":"private","id":910001},"date":1700000000,"message_id":1},"update_id":90001}`

	r1 := tgReceive(t, svc, dedupAccountA, first)
	if !r1.Accepted || r1.Duplicate {
		t.Fatalf("首次投递应 accepted 且非重复，got %+v", r1)
	}
	r2 := tgReceive(t, svc, dedupAccountA, replay)
	if !r2.Duplicate {
		t.Fatalf("同一 update_id 的重编码重投必须判重复（S-04），got %+v —— 当前实现按整包字节哈希，换字节即视为新事件", r2)
	}

	other := strings.Replace(first, `"update_id":90001`, `"update_id":90002`, 1)
	if r3 := tgReceive(t, svc, dedupAccountA, other); r3.Duplicate {
		t.Fatalf("不同 update_id 是不同事件，不得判重复，got %+v", r3)
	}
}

func TestReceive_OfficialEventIDScopedPerAccount(t *testing.T) {
	svc := setupDedupEnv(t)
	// 两个 bot 各自独立编号，撞上同一个 update_id 是常态：账号作用域前缀
	// 缺失会让第二个账号的消息被当成重复而静默丢弃。
	body := `{"update_id":777001,"message":{"message_id":1,"date":1700000000,"chat":{"id":920001,"type":"private"},"from":{"id":1,"first_name":"u"},"text":"跨账号同 id"}}`
	if r := tgReceive(t, svc, dedupAccountA, body); r.Duplicate {
		t.Fatalf("账号 1 首次投递不应判重复，got %+v", r)
	}
	if r := tgReceiveNamed(t, svc, dedupAccountB, body, "dedup-b"); r.Duplicate {
		t.Fatalf("账号 2 的同名 update_id 是另一条事件，不得判重复（S-04 作用域回归），got %+v", r)
	}
}

// 飞书/企微开启加解密后，官方键在密文里，Receive 层的 officialEventID 取不到。
// 这条用例坐实第二层兜底确实成立：dispatch 阶段解出的官方 message_id 直接落到
// message_hub.msg_id，同一条消息重投（即使外层密文/事件字节不同）不会多出一行。
func TestFeishuDispatch_OfficialMessageIDDedupsAtHubLayer(t *testing.T) {
	db := setupChannelFullDB(t)
	acc := &model.FeishuAccount{
		AccountName: "FS-dedup", AppID: "a", AppSecret: "b",
		WebhookEnabled: true, AIAgentEnabled: false, Status: 1,
	}
	if err := db.Create(acc).Error; err != nil {
		t.Fatalf("seed feishu account: %v", err)
	}
	svc := NewWebhookService(db)
	t.Cleanup(func() { svc.Stop(context.Background()) })

	// 同一条 om_dup_1，两次投递的其余字节不同（header.event_id / create_time 变化
	// 足以让整包哈希判成新事件）
	first := []byte(`{"schema":"2.0","header":{"event_type":"im.message.receive_v1","event_id":"ev-a","token":"v"},"event":{"sender":{"sender_id":{"open_id":"ou_1"}},"message":{"message_id":"om_dup_1","chat_id":"oc_1","chat_type":"p2p","message_type":"text","create_time":"1700000000000","content":"{\"text\":\"重复投递\"}"}}}`)
	retry := []byte(`{"schema":"2.0","header":{"event_type":"im.message.receive_v1","event_id":"ev-b","token":"v"},"event":{"sender":{"sender_id":{"open_id":"ou_1"}},"message":{"message_id":"om_dup_1","chat_id":"oc_1","chat_type":"p2p","message_type":"text","create_time":"1700000009000","content":"{\"text\":\"重复投递\"}"}}}`)

	if _, err := svc.dispatchFeishu(context.Background(), fmt.Sprintf("%d", acc.ID), &ParsedPayload{}, first); err != nil {
		t.Fatalf("首次投递: %v", err)
	}
	if _, err := svc.dispatchFeishu(context.Background(), fmt.Sprintf("%d", acc.ID), &ParsedPayload{}, retry); err != nil {
		t.Fatalf("重投递: %v", err)
	}
	var rows []string
	if err := db.Model(&model.MessageHub{}).
		Where("platform = ? AND conversation_id = ?", "feishu", "oc_1").
		Pluck("msg_id", &rows).Error; err != nil {
		t.Fatalf("查询 hub 失败: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("同一官方 message_id 的重投必须在 hub 层收敛成 1 行，实际 %d 行: %v", len(rows), rows)
	}
	if rows[0] != "om_dup_1" {
		t.Fatalf("hub.msg_id 必须是官方 message_id，got %q", rows[0])
	}
}

// ---------------------------------------------------------------------------
// 微信公众号：事件推送无 MsgId 时不得把幂等键交给人手一个 uuid
// ---------------------------------------------------------------------------

func TestWechatDedupKey_OfficialMsgIDAndEventPushFallback(t *testing.T) {
	svc := &WechatService{}
	parse := func(t *testing.T, xmlBody string) *WechatIncomingMessage {
		t.Helper()
		m, err := svc.ParseIncomingMessage([]byte(xmlBody))
		if err != nil {
			t.Fatalf("parse wechat xml: %v", err)
		}
		return m
	}

	textMsg := `<xml><ToUserName>gh_bot</ToUserName><FromUserName>o_cust</FromUserName>` +
		`<CreateTime>1700000000</CreateTime><MsgType>text</MsgType><Content>你好</Content><MsgId>12345</MsgId></xml>`
	m := parse(t, textMsg)
	if got := m.DedupKey(7); got != "wx-7-12345" {
		t.Fatalf("有官方 MsgId 时必须用它，got %q", got)
	}

	// 事件推送：官方结构里没有 MsgId
	evSub := `<xml><ToUserName>gh_bot</ToUserName><FromUserName>o_cust</FromUserName>` +
		`<CreateTime>1700000000</CreateTime><MsgType>event</MsgType><Event>subscribe</Event></xml>`
	a := parse(t, evSub).DedupKey(7)
	b := parse(t, evSub).DedupKey(7)
	if a == "" || a != b {
		t.Fatalf("同一条事件重推必须得同一幂等键（空键会被 ingress 兜底成随机 uuid，幂等失效）：%q vs %q", a, b)
	}
	if strings.HasSuffix(a, "-") || !strings.HasPrefix(a, "wx-7-") {
		t.Fatalf("幂等键必须账号作用域且非退化常量: %q", a)
	}

	otherAccount := parse(t, evSub).DedupKey(8)
	if otherAccount == a {
		t.Fatal("不同账号的同一事件推送不得共用幂等键")
	}
	evUnsub := strings.Replace(evSub, "<Event>subscribe</Event>", "<Event>unsubscribe</Event>", 1)
	if parse(t, evUnsub).DedupKey(7) == a {
		t.Fatal("内容不同的两条事件不得共用幂等键，否则第二条被静默丢弃")
	}
}

// ---------------------------------------------------------------------------
// 钉钉：msgId 缺失时 EventID 不得退化成常量键
// ---------------------------------------------------------------------------

func dtRobotMsgJSONWithout(convID, content string) string {
	return `{"conversationId":"` + convID + `","chatbotUserId":"bot-1","createAt":1700000000000,` +
		`"conversationType":"2","senderId":"$:LWCP_v1:$xyz","senderStaffId":"staff-9","msgtype":"text",` +
		`"text":{"content":"` + content + `"},"sessionWebhook":"https://oapi.dingtalk.com/robot/sendBySession?session=abc"}`
}

// setupDingTalkInboundWithDB 与 setupDingTalkInbound 同构，额外返回 db：
// 本用例要断言「消息是否真的入库」，而去重/幂等发生在持久化层，触发计数器在
// 会话级 AI 排他锁下会失真（那是另一层机制，不属于 S-04）。
func setupDingTalkInboundWithDB(t *testing.T, acc *model.DingTalkAppAccount) (*DingTalkAppService, uint, *gorm.DB) {
	t.Helper()
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "")
	db := testutil.NewTestDBOrSkip(t, &model.DingTalkAppAccount{}, &model.MessageHub{})
	acc.UserID = 1
	if err := db.Create(acc).Error; err != nil {
		t.Fatalf("create dingtalk account: %v", err)
	}
	mc := cache.NewMemoryCache()
	t.Cleanup(mc.Close)
	ingress := NewInboxIngressServiceWithDB(db, mc)
	ingress.SetAITrigger(&fakeAITrigger{})
	return NewDingTalkAppService(db, &WebhookService{ingressSvc: ingress}), acc.ID, db
}

func dtInboundRows(t *testing.T, db *gorm.DB, convID string) []string {
	t.Helper()
	var msgIDs []string
	if err := db.Model(&model.MessageHub{}).
		Where("conversation_id = ? AND direction = ?", convID, "inbound").
		Order("id asc").Pluck("msg_id", &msgIDs).Error; err != nil {
		t.Fatalf("查询 message_hub 失败: %v", err)
	}
	return msgIDs
}

func TestDingTalkInbound_MissingMsgIDMustNotCollapseToConstantKey(t *testing.T) {
	svc, id, db := setupDingTalkInboundWithDB(t, &model.DingTalkAppAccount{
		AppKey: "ak-nomid", AppSecret: "SECrobot", InboundEnabled: true, Status: 1,
	})
	headers := dtRobotHeaders("SECrobot", time.Now().UnixMilli())
	ctx := context.Background()

	if err := svc.ReceiveMessage(ctx, id, []byte(dtRobotMsgJSONWithout("cid-nomid", "第一条")), nil, headers); err != nil {
		t.Fatalf("第一条无 msgId 消息应入站: %v", err)
	}
	if err := svc.ReceiveMessage(ctx, id, []byte(dtRobotMsgJSONWithout("cid-nomid", "第二条")), nil, headers); err != nil {
		t.Fatalf("第二条无 msgId 消息应入站: %v", err)
	}
	rows := dtInboundRows(t, db, "cid-nomid")
	if len(rows) != 2 {
		t.Fatalf("两条内容不同的消息都必须入库，实际 %d 行（%v）—— EventID 退化成常量 dt-<account>- 会让第二条被钩子2 判重复而静默丢弃", len(rows), rows)
	}
	if rows[0] == rows[1] {
		t.Fatalf("两条不同消息的幂等键不得相同: %q", rows[0])
	}
	for _, msgID := range rows {
		if strings.HasSuffix(msgID, "-") {
			t.Fatalf("EventID 退化为常量键: %q", msgID)
		}
	}

	// 字节完全相同的一条被重投时仍须判重，不得多出第三行
	if err := svc.ReceiveMessage(ctx, id, []byte(dtRobotMsgJSONWithout("cid-nomid", "第二条")), nil, headers); err != nil {
		t.Fatalf("重投请求本身应被接受: %v", err)
	}
	if again := dtInboundRows(t, db, "cid-nomid"); len(again) != 2 {
		t.Fatalf("字节完全相同的重投应被判重复入库 2 行，实际 %d 行（%v）", len(again), again)
	}
}
