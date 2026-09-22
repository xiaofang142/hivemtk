package service

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// 批H：钉钉入站官方「会话与身份面」落地（N-22）。
//
// 官方依据（A 档取证：.tmp_files/mut-evidence-2026-09-20/batchH/dt-robot-receive-message-official.txt，
// https://open.dingtalk.com/document/development/robot-message-type，正文 17,156 字符）：
//
//	「当用户@群机器人或与机器人发送单聊消息时，钉钉会把机器人接收到的消息发送到开发者设置的机器人回调服务。」
//	conversationType String 会话类型：1：单聊 2：群聊
//	conversationTitle String 群聊时才有的会话标题
//	senderNick String 发送者昵称
//	isAdmin Boolean 是否为管理员 …… 机器人发布上线后生效，否则不返回
//	isInAtList Boolean 是否在@列表中
//	atUsers Array of Object 被@人的信息（dingtalkId / staffId / unionId）；机器人自身 id 在 chatbotUserId
//
// 由此推翻 N-22 的半个前提：中台**不需要**再做一个 @判定门 —— 群聊回调本身就是「@了机器人」才发生的。
// 所以这里读 isInAtList 只作存档，不参与是否回复的判定（用例⑦把这条钉住）。

const batchHSecret = "SECbatchH"

// batchHSetup 同 setupDingTalkInbound，额外把 *gorm.DB 交出来：批H 的断言必须打在
// **落库的那一行 message_hub** 上，而不是内存里的 event 视图。
func batchHSetup(t *testing.T) (*DingTalkAppService, *fakeAITrigger, *gorm.DB, uint) {
	t.Helper()
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "")
	db := testutil.NewTestDBOrSkip(t, &model.DingTalkAppAccount{}, &model.MessageHub{})
	acc := &model.DingTalkAppAccount{
		UserID: 1, AppKey: "ak-h-" + strconv.FormatInt(time.Now().UnixNano(), 10),
		AppSecret: batchHSecret, InboundEnabled: true, Status: 1,
	}
	if err := db.Create(acc).Error; err != nil {
		t.Fatalf("create dingtalk account: %v", err)
	}
	mc := cache.NewMemoryCache()
	t.Cleanup(mc.Close)
	ingress := NewInboxIngressServiceWithDB(db, mc)
	tr := &fakeAITrigger{}
	ingress.SetAITrigger(tr)
	return NewDingTalkAppService(db, &WebhookService{ingressSvc: ingress}), tr, db, acc.ID
}

// batchHPayload 造一条官方形态的群聊机器人回调。over 里 value 为 nil 表示**删掉该字段**，
// 用来演「应用未发布上线 ⇒ 官方明写不返回」那一类缺席；一次性 conversationId / msgId
// 避开历次运行的行串进同一段会话（N-19 的教训）。
func batchHPayload(t *testing.T, over map[string]any) map[string]any {
	t.Helper()
	now := time.Now()
	nonce := strconv.FormatInt(now.UnixNano(), 10)
	p := map[string]any{
		"conversationId":    "cid-h-" + nonce,
		"msgId":             "m-h-" + nonce,
		"createAt":          now.UnixMilli(),
		"conversationType":  "2",
		"conversationTitle": "销售一组",
		"senderId":          "$:LWCP_v1:$xyz",
		"senderStaffId":     "staff-9",
		"senderNick":        "小钉",
		"isAdmin":           true,
		"isInAtList":        true,
		"chatbotUserId":     "$:LWCP_xxxxr5",
		"senderCorpId":      "ding9733d095",
		"chatbotCorpId":     "ding9733d095",
		"senderPlatform":    "Mac",
		// 官方语义：atUsers 是**被@的人**，机器人自身在 chatbotUserId。
		"atUsers":                   []any{map[string]any{"dingtalkId": "$:LWCP_v1:$xyz", "staffId": "staff-9", "unionId": "edxxx34"}},
		"msgtype":                   "text",
		"text":                      map[string]any{"content": "你们产品多少钱"},
		"sessionWebhook":            "https://oapi.dingtalk.com/robot/sendBySession?session=abc123",
		"sessionWebhookExpiredTime": now.Add(time.Hour).UnixMilli(),
	}
	for k, v := range over {
		if v == nil {
			delete(p, k)
			continue
		}
		p[k] = v
	}
	return p
}

// batchHSend 走官方明文机器人回调（header timestamp + sign），返回一次性会话 id。
func batchHSend(t *testing.T, svc *DingTalkAppService, accountID uint, payload map[string]any) (string, error) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	convID, _ := payload["conversationId"].(string)
	return convID, svc.ReceiveMessage(context.Background(), accountID, raw, nil,
		dtRobotHeaders(batchHSecret, time.Now().UnixMilli()))
}

// batchHReadHub 读回落库那一行；用独立零值 struct（复用已填充的 dest 会把旧字段并进 WHERE）。
func batchHReadHub(t *testing.T, db *gorm.DB, convID string) model.MessageHub {
	t.Helper()
	var hub model.MessageHub
	if err := db.Where("platform = ? AND conversation_id = ?", model.ChannelDingTalk, convID).
		First(&hub).Error; err != nil {
		t.Fatalf("会话 %s 应有一行 message_hub: %v", convID, err)
	}
	return hub
}

func batchHExtra(t *testing.T, hub model.MessageHub) map[string]any {
	t.Helper()
	if len(hub.Extra) == 0 {
		t.Fatalf("hub Extra 为空，群/身份面无处落地（msg_id=%s）", hub.MsgID)
	}
	return map[string]any(hub.Extra)
}

// ① 群聊：官方身份面字段必须一路落到 hub 行与 AI 触发参数上。
func TestBatchH_DingTalkGroupCallbackLandsGroupFaces(t *testing.T) {
	svc, tr, db, id := batchHSetup(t)
	convID, err := batchHSend(t, svc, id, batchHPayload(t, nil))
	if err != nil {
		t.Fatalf("官方群聊回调必须收下: %v", err)
	}

	hub := batchHReadHub(t, db, convID)
	if !hub.IsGroup {
		t.Error("落库行 is_group 必须为 true（conversationType=2）")
	}
	if hub.GroupID != convID {
		t.Errorf("落库行 group_id 应为官方 conversationId %q，got %q", convID, hub.GroupID)
	}
	if hub.SenderName != "小钉" {
		t.Errorf("落库行 sender_name 应取官方 senderNick，got %q", hub.SenderName)
	}
	extra := batchHExtra(t, hub)
	if extra["group_name"] != "销售一组" {
		t.Errorf("Extra.group_name 应取官方 conversationTitle，got %#v", extra["group_name"])
	}
	if extra["is_admin"] != true {
		t.Errorf("Extra.is_admin 应存档官方 isAdmin=true，got %#v", extra["is_admin"])
	}
	if extra["is_in_at_list"] != true {
		t.Errorf("Extra.is_in_at_list 应存档官方 isInAtList=true，got %#v", extra["is_in_at_list"])
	}
	if extra["conversation_type"] != "2" {
		t.Errorf("Extra.conversation_type 应原样留官方值，got %#v", extra["conversation_type"])
	}

	if tr.called != 1 {
		t.Fatalf("群聊 @机器人 必须触发 AI 1 次，实际 %d", tr.called)
	}
	if tr.lastMeta == nil {
		t.Fatal("AI 触发参数缺失")
	}
	if !tr.lastMeta.IsGroup || tr.lastMeta.GroupID != convID || tr.lastMeta.GroupName != "销售一组" {
		t.Errorf("AI 侧群身份面丢失: %+v", tr.lastMeta)
	}
	if tr.lastMeta.SenderName != "小钉" {
		t.Errorf("AI 侧 senderNick 丢失: %+v", tr.lastMeta)
	}
}

// ② 单聊：conversationType=1 不得带上任何群面，但 senderNick 仍要落。
func TestBatchH_DingTalkSingleChatHasNoGroupFaces(t *testing.T) {
	svc, tr, db, id := batchHSetup(t)
	convID, err := batchHSend(t, svc, id, batchHPayload(t, map[string]any{
		"conversationType":  "1",
		"conversationTitle": nil, // 官方：群聊时才有
	}))
	if err != nil {
		t.Fatalf("官方单聊回调必须收下: %v", err)
	}
	hub := batchHReadHub(t, db, convID)
	if hub.IsGroup {
		t.Error("conversationType=1 落库行 is_group 必须 false")
	}
	if hub.GroupID != "" {
		t.Errorf("单聊不得有 group_id，got %q", hub.GroupID)
	}
	extra := batchHExtra(t, hub)
	if _, ok := extra["group_name"]; ok {
		t.Errorf("单聊不得有 group_name，got %#v", extra["group_name"])
	}
	if hub.SenderName != "小钉" {
		t.Errorf("单聊同样有 senderNick，落库行 sender_name got %q", hub.SenderName)
	}
	if tr.called != 1 || tr.lastMeta.IsGroup {
		t.Errorf("单聊必须触发一次 AI 且不带群面: called=%d meta=%+v", tr.called, tr.lastMeta)
	}
}

// ③ 缺席 ≠ false：官方明写 isAdmin/isInAtList「机器人发布上线后生效，否则不返回」，
// 未下发时 Extra 里必须**没有**这个键，而不是写一个 false 让 ops 误判成「非管理员」。
func TestBatchH_DingTalkAbsentOptionalFlagsStayAbsent(t *testing.T) {
	svc, tr, db, id := batchHSetup(t)
	convID, err := batchHSend(t, svc, id, batchHPayload(t, map[string]any{
		"isAdmin": nil, "isInAtList": nil, "conversationType": nil, "conversationTitle": nil,
	}))
	if err != nil {
		t.Fatalf("可选字段缺席的回调必须收下: %v", err)
	}
	hub := batchHReadHub(t, db, convID)
	extra := batchHExtra(t, hub)
	if _, ok := extra["is_admin"]; ok {
		t.Errorf("官方未下发 isAdmin 时不得写 is_admin，got %#v", extra["is_admin"])
	}
	if _, ok := extra["is_in_at_list"]; ok {
		t.Errorf("官方未下发 isInAtList 时不得写 is_in_at_list，got %#v", extra["is_in_at_list"])
	}
	if hub.IsGroup {
		t.Error("conversationType 未下发时不得当群聊")
	}
	if tr.called != 1 {
		t.Errorf("可选字段缺席不得影响 AI 触发，called=%d", tr.called)
	}
}

// ④ 显式 false 与缺席必须可分辨（否则 ③ 那半边判据没有对立样本）。
func TestBatchH_DingTalkExplicitFalseLandsAsFalse(t *testing.T) {
	svc, _, db, id := batchHSetup(t)
	convID, err := batchHSend(t, svc, id, batchHPayload(t, map[string]any{
		"isAdmin": false, "isInAtList": false,
	}))
	if err != nil {
		t.Fatalf("收下失败: %v", err)
	}
	extra := batchHExtra(t, batchHReadHub(t, db, convID))
	v, ok := extra["is_admin"]
	if !ok {
		t.Fatal("显式 isAdmin=false 必须落成 is_admin=false，不能与「未下发」混为一谈")
	}
	if b, _ := v.(bool); b {
		t.Errorf("is_admin 应为 false，got %#v", v)
	}
	if b, ok := extra["is_in_at_list"].(bool); !ok || b {
		t.Errorf("is_in_at_list 应为 false，got %#v (ok=%v)", extra["is_in_at_list"], ok)
	}
}

// ⑤ 类型漂移：文档类型栏写 String、示例给 "2"，而同一份文档的 createAt 写 String 实际给数字。
// conversationType 若真以数字下发，绝不能让整包解析失败把客户消息丢掉。
func TestBatchH_DingTalkNumericConversationTypeStillGroup(t *testing.T) {
	svc, _, db, id := batchHSetup(t)
	convID, err := batchHSend(t, svc, id, batchHPayload(t, map[string]any{"conversationType": json.Number("2")}))
	if err != nil {
		t.Fatalf("数字形态 conversationType 必须收下而不是丢消息: %v", err)
	}
	hub := batchHReadHub(t, db, convID)
	if !hub.IsGroup {
		t.Error("数字 2 同样必须判成群聊")
	}
	if got := batchHExtra(t, hub)["conversation_type"]; got != "2" {
		t.Errorf("Extra.conversation_type 应归一为 \"2\"，got %#v", got)
	}
}

// ⑥ 旁证字段形态变化不许把整条消息带走：isAdmin 若哪天变成 1/0，
// 为它报解析错 = 群里每条客户消息 400 且不重投，代价远大于少存一枚旗标。
func TestBatchH_DingTalkUnknownBoolFormDoesNotSinkMessage(t *testing.T) {
	svc, tr, db, id := batchHSetup(t)
	convID, err := batchHSend(t, svc, id, batchHPayload(t, map[string]any{"isAdmin": json.Number("1")}))
	if err != nil {
		t.Fatalf("无法识别的 isAdmin 形态只能放弃该旗标，不能丢消息: %v", err)
	}
	hub := batchHReadHub(t, db, convID)
	if _, ok := batchHExtra(t, hub)["is_admin"]; ok {
		t.Error("解析不出的形态不得伪造成 true/false 落库")
	}
	if tr.called != 1 {
		t.Errorf("AI 链路必须照常触发，called=%d", tr.called)
	}
}

// ⑦ 钉死「不加 @判定门」这个结论：群里既没有 atUsers 也没有 isInAtList 的回调
// 仍然必须触发 AI。将来若有人按 N-22 旧口径补一道 @判定，这条会红。
func TestBatchH_DingTalkGroupWithoutAtSignalStillTriggersAI(t *testing.T) {
	svc, tr, _, id := batchHSetup(t)
	if _, err := batchHSend(t, svc, id, batchHPayload(t, map[string]any{
		"atUsers": nil, "isInAtList": nil,
	})); err != nil {
		t.Fatalf("收下失败: %v", err)
	}
	if tr.called != 1 {
		t.Fatalf("官方只在「@机器人」时回调群消息，无 @ 信号也必须触发 AI，called=%d", tr.called)
	}
}
