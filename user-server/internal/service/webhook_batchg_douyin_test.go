package service

// 批G-1：抖音/TikTok dop webhook 的入站契约（验签算法 + 事件信封 + 消息类型 + 时间戳 + 账号作用域键）。
//
// 官方取证（A 档，原文与字节数登记在审计文档 §16.1）：
//   - https://developer.open-douyin.com/docs/resource/zh-CN/dop/develop/webhooks/summarize (2,917 B)
//     「抖音服务端会将应用的(client secret + 消息体)使用 sha1 哈希作为 X-Douyin-Signature header 的 value」
//     并给出 go 实现：h.Write(clientSecret); h.Write(body); fmt.Sprintf("%x", h.Sum(nil))
//   - .../mini-app/.../private-msg-webhook (12,395 B)
//     信封 {event, client_key, from_user_id, to_user_id, log_id, content}；
//     content {conversation_short_id, server_message_id, conversation_type, message_type,
//              text/resource_url/item_id/card_*, create_time(13 位毫秒), source, index, user_infos[]}
//     message_type 全集：text / image / user_local_image / emoji / video / user_local_video /
//                        retain_consult_card / other
//   - .../dop/develop/webhooks/event-list (8,849 B)
//     群/单聊由**事件名**区分（im_receive_msg vs im_group_receive_msg），信封里没有任何 group 字段
//
// 修复前的现场：验签用 HMAC-SHA256(secret, body)（官方是 sha1 拼接，永不相等）；
// 解析用的 douyinWebhookPayload 是 {event_type, data:{message,from,to,conversation}} —— 官方
// 报文里根本没有这些键 ⇒ 解出来 sender 为空，命中 `p.Sender == ""` 的 return nil,nil,nil：
// 不落库、不进收件箱、不触发 AI，而 HTTP 仍回 200，抖音据此认为投递成功、按「5s/3 次」不再重投，
// 客户消息**静默蒸发**。
//
// 本文件的夹具全部抄官方示例原文（含真实形态：@ 开头的 base64 会话/消息 ID、13 位毫秒、
// conversation_type 与事件名不同步、user_infos 里才有的昵称），不复用被测实现的任何输出。

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
)

const gDySecret = "dy_client_secret_batchg"

// gDyOfficialSign 按官方口径独立造签名：hex(sha1(client_secret ‖ 原始 body))。
// 造夹具的算法必须与「被测实现要学的算法」同源，否则等于拿实现的反推当契约。
func gDyOfficialSign(secret string, body []byte) string {
	h := sha1.New()
	h.Write([]byte(secret))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// gDyLegacySign 复刻修复前的错误口径：HMAC-SHA256(secret, body) 的 hex。仅作反向锚点。
func gDyLegacySign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func gDySetup(t *testing.T) (*WebhookService, context.Context, string) {
	t.Helper()
	db := newD03DispatchDB(t)
	acc := &model.IntegrationAccount{Platform: string(ChannelDouyin), APISecret: gDySecret, Status: 1}
	if err := db.Create(acc).Error; err != nil {
		t.Fatalf("seed douyin account: %v", err)
	}
	tiktokAcc := &model.IntegrationAccount{Platform: string(ChannelTiktok), APISecret: gDySecret + "_tt", Status: 1}
	if err := db.Create(tiktokAcc).Error; err != nil {
		t.Fatalf("seed tiktok account: %v", err)
	}
	svc := NewWebhookService(db)
	t.Cleanup(func() { svc.Stop(context.Background()) })
	return svc, context.Background(), gDySecret
}

// ─── G-1 验签算法 ───────────────────────────────────────────────

// TestBatchG_DouyinVerify_OfficialSha1Contract 官方 sha1(client_secret+body) 必须验过；
// 修复前的 HMAC-SHA256(body) 口径必须验不过（它是「永不相等」的那一侧，真实回调 100% 被拒）。
func TestBatchG_DouyinVerify_OfficialSha1Contract(t *testing.T) {
	svc, ctx, _ := gDySetup(t)

	body := []byte(`{"event":"im_receive_msg","client_key":"aaa","from_user_id":"u1","to_user_id":"u2","content":{"message_type":"text","text":"你好，抖音"}}`)

	if ok, err := svc.Verify(ctx, ChannelDouyin, "1", body,
		map[string]string{"X-Douyin-Signature": gDyOfficialSign(gDySecret, body)}, nil); err != nil || !ok {
		t.Errorf("官方 sha1 签名必须验签通过，got ok=%v err=%v", ok, err)
	}

	if ok, _ := svc.Verify(ctx, ChannelDouyin, "1", body,
		map[string]string{"X-Douyin-Signature": gDyLegacySign(gDySecret, body)}, nil); ok {
		t.Error("修复前的 HMAC-SHA256(body) 口径不得再被接受（那是永远对不上官方的算法）")
	}

	// 摘要与 body/密钥绑定：改一字节、换密钥都必须失效。
	tampered := append(append([]byte{}, body...), '!')
	if ok, _ := svc.Verify(ctx, ChannelDouyin, "1", tampered,
		map[string]string{"X-Douyin-Signature": gDyOfficialSign(gDySecret, body)}, nil); ok {
		t.Error("body 被改一个字节后不得验签通过")
	}
	if ok, _ := svc.Verify(ctx, ChannelDouyin, "1", body,
		map[string]string{"X-Douyin-Signature": gDyOfficialSign("other_secret", body)}, nil); ok {
		t.Error("换密钥后不得验签通过")
	}

	// 缺头必须报错并点名官方头：否则渠道侧只看到一个无法归因的 401（与 tiktok 同一口径）。
	if ok, err := svc.Verify(ctx, ChannelDouyin, "1", body, map[string]string{}, nil); ok ||
		err == nil || !strings.Contains(err.Error(), "X-Douyin-Signature") {
		t.Errorf("缺签名头要报出缺的是 X-Douyin-Signature，got ok=%v err=%v", ok, err)
	}

	// 非官方的裸 Signature 头兜底必须去掉：它是 verifyHMAC 的遗留参数，
	// 抖音契约里没有这个头，留着只会让「带任意 Signature 的请求」多一条被误判的路径。
	if ok, _ := svc.Verify(ctx, ChannelDouyin, "1", body,
		map[string]string{"Signature": gDyOfficialSign(gDySecret, body)}, nil); ok {
		t.Error("官方只认 X-Douyin-Signature，裸 Signature 兜底不得再放行")
	}
}

// TestBatchG_DouyinVerify_HeaderSpelling HTTP 层会把头名规范化，按字面量直取会让
// 「单测全绿、真实回调全挂」。与 tiktok 那条同一口径。
func TestBatchG_DouyinVerify_HeaderSpelling(t *testing.T) {
	svc, ctx, _ := gDySetup(t)
	body := []byte(`{"event":"im_receive_msg","from_user_id":"u1","content":{"text":"拼写"}}`)
	sig := gDyOfficialSign(gDySecret, body)

	for _, spelling := range []string{"X-Douyin-Signature", "X-douyin-signature", "x-douyin-signature", "X-DOUYIN-SIGNATURE"} {
		if ok, err := svc.Verify(ctx, ChannelDouyin, "1", body, map[string]string{spelling: sig}, nil); err != nil || !ok {
			t.Errorf("头名 %s 也要认（HTTP 层会规范化大小写），got ok=%v err=%v", spelling, ok, err)
		}
	}
}

// TestBatchG_DouyinVerify_NotSharedWithTiktokBranch 两家签名算法不同，分支必须分开：
// tiktok 走官方 t=/s= HMAC，抖音走 sha1 拼接。互相喂对方的签名都要被拒。
func TestBatchG_DouyinVerify_NotSharedWithTiktokBranch(t *testing.T) {
	svc, ctx, _ := gDySetup(t)
	body := []byte(`{"event":"im_receive_msg","from_user_id":"u1","content":{"text":"分家"}}`)

	if ok, _ := svc.Verify(ctx, ChannelDouyin, "1", body,
		map[string]string{"TikTok-Signature": "t=1633174587,s=" + gDyOfficialSign(gDySecret, body)}, nil); ok {
		t.Error("抖音分支不得吃 TikTok-Signature 头")
	}
	if ok, _ := svc.Verify(ctx, ChannelTiktok, "1", body,
		map[string]string{"X-Douyin-Signature": gDyOfficialSign(gDySecret+"_tt", body)}, nil); ok {
		t.Error("tiktok 分支不得吃抖音的 sha1 拼接签名")
	}
}

// ─── G-3 事件信封 ───────────────────────────────────────────────

// gDyTextEventRaw 抄官方「事件参数示例」原文（private-msg-webhook 页尾），
// 唯一改动是把 index 之后缺失的逗号补上（原文示例本身少一个逗号，是文档笔误）。
const gDyTextEventRaw = `{"event":"im_receive_msg","client_key":"asdfavetgbvasf","from_user_id":"aaa-ae9b-4dbf-add1-bf67e4093fab","to_user_id":"aaa-7ae0-4399-914a-5eb1df5861ba","log_id":"2023429834752345783","content":{"conversation_short_id":"@9Vxc1/yDU8sqaS+gN4koEc7912foPPiLPZVyrQmgKVQabPH460zdRmYqig357zEBMaXjKvvhvwY02ISB7llsWQ==","server_message_id":"@9Vxc1/yDU8sqaS+gN4koEc7912foPPyAPpF2rgykLFgbafb560zdRmYqig357zEBmWXVPshhwyd/y/7BQpXy/w==","conversation_type":1,"create_time":1681303285997,"message_type":"text","source":"","text":"捷途70","index":"1672502407220000","user_infos":[{"open_id":"aaa-ae9b-4dbf-add1-bf67e4093fab","nick_name":"刘东","avatar":"https://p3.douyinpic.com/aweme/720x720/xxx.jpeg"},{"open_id":"aaa-7ae0-4399-914a-5eb1df5861ba","nick_name":"毕节--捷途万丰店","avatar":"https://p3.douyinpic.com/aweme/720x720/yyy.jpeg"}]}}`

const (
	gDySenderOpenID = "aaa-ae9b-4dbf-add1-bf67e4093fab"
	gDyConvShortID  = "@9Vxc1/yDU8sqaS+gN4koEc7912foPPiLPZVyrQmgKVQabPH460zdRmYqig357zEBMaXjKvvhvwY02ISB7llsWQ=="
	gDyServerMsgID  = "@9Vxc1/yDU8sqaS+gN4koEc7912foPPyAPpF2rgykLFgbafb560zdRmYqig357zEBmWXVPshhwyd/y/7BQpXy/w=="
	gDyCreateTimeMS = 1681303285997
)

// TestBatchG_DispatchDouyin_OfficialTextEventLandsHubRow 官方文本示例必须落成一行可用记录。
//
// 这条就是「静默蒸发」的现场取证：修复前 dispatchDouyin 返回 (nil, nil, nil) —— 客户在抖音里
// 发的第一条消息在中台完全不存在。
func TestBatchG_DispatchDouyin_OfficialTextEventLandsHubRow(t *testing.T) {
	svc, ctx, _ := gDySetup(t)

	hub, _, err := svc.dispatchDouyin(ctx, ChannelDouyin, "gdy-acc", &ParsedPayload{EventID: "gdy-text-1"}, []byte(gDyTextEventRaw))
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if hub == nil {
		t.Fatal("G-3 未达成：官方 im_receive_msg 报文解不出发送人，整条客户消息被丢弃（不落库/不进收件箱/不回 AI）")
	}
	if hub.Platform != "douyin" || hub.AccountID != "gdy-acc" {
		t.Errorf("落库平台/账号错位 platform=%q account=%q", hub.Platform, hub.AccountID)
	}
	if hub.Direction != "inbound" {
		t.Errorf("im_receive_msg 必须是入站，got %q", hub.Direction)
	}
	if hub.SenderID != gDySenderOpenID {
		t.Errorf("SenderID 应为信封 from_user_id（官方：发送方 open_id），got %q", hub.SenderID)
	}
	if hub.SenderName != "刘东" {
		t.Errorf("昵称只在 content.user_infos[] 里（信封上没有），应按 open_id 匹配取到「刘东」，got %q", hub.SenderName)
	}
	if hub.ConversationID != gDyConvShortID {
		t.Errorf("ConversationID 应为 content.conversation_short_id，got %q", hub.ConversationID)
	}
	if hub.MsgType != "text" {
		t.Errorf("message_type=text 应落 text，got %q", hub.MsgType)
	}
	if hub.Content != "捷途70" {
		t.Errorf("正文应取 content.text，got %q", hub.Content)
	}
	if !hub.SentAt.Equal(time.UnixMilli(gDyCreateTimeMS)) {
		t.Errorf("SentAt 应为官方 create_time（13 位毫秒），got %v want %v", hub.SentAt, time.UnixMilli(gDyCreateTimeMS))
	}
	if hub.IsGroup {
		t.Error("单聊事件（im_receive_msg）不得判成群")
	}
	if extra := hub.Extra; extra == nil || extra["server_message_id"] != gDyServerMsgID {
		t.Errorf("官方消息 ID 要留在 Extra 里供回查（MsgID 用哈希，见另一条测），got %+v", extra)
	}

	var rows int64
	if err := svc.lazyDB().Model(&model.MessageHub{}).Where("account_id = ?", "gdy-acc").Count(&rows).Error; err != nil {
		t.Fatalf("count hub: %v", err)
	}
	if rows != 1 {
		t.Errorf("hub 行必须真落库 1 条，实际 %d 条", rows)
	}
}

// TestBatchG_DispatchDouyin_GroupFromEventName 群/单聊由**事件名**区分；
// 官方示例里 conversation_type 与是否群聊并不对应（文本消息示例给的是 2，其余给 1），
// 所以既不能靠它判群，也不能因为它是 2 就把单聊误判成群。
func TestBatchG_DispatchDouyin_GroupFromEventName(t *testing.T) {
	svc, ctx, _ := gDySetup(t)

	groupRaw := `{"event":"im_group_receive_msg","client_key":"kk","from_user_id":"grp-user-1","to_user_id":"bot-1","content":{"conversation_short_id":"@convgrp+abc==","server_message_id":"@msggrp+abc==","conversation_type":1,"create_time":1681303285997,"message_type":"text","text":"这个多少钱"}}`
	hub, _, err := svc.dispatchDouyin(ctx, ChannelDouyin, "gdy-grp", &ParsedPayload{EventID: "gdy-grp-1"}, []byte(groupRaw))
	if err != nil {
		t.Fatalf("group dispatch: %v", err)
	}
	if hub == nil {
		t.Fatal("im_group_receive_msg 必须落库（群线索的唯一来源）")
	}
	if !hub.IsGroup {
		t.Errorf("群事件要落群聊标记，got %+v", hub)
	}

	// 反向：conversation_type=2 的单聊示例（官方文本示例就是这个值）不得被判成群。
	singleRaw := `{"event":"im_receive_msg","from_user_id":"u-type2","content":{"conversation_short_id":"@c2","server_message_id":"@m2","conversation_type":2,"create_time":1656571939562,"message_type":"text","text":"你们产品多少钱"}}`
	hub2, _, err := svc.dispatchDouyin(ctx, ChannelDouyin, "gdy-grp", &ParsedPayload{EventID: "gdy-grp-2"}, []byte(singleRaw))
	if err != nil {
		t.Fatalf("single dispatch: %v", err)
	}
	if hub2 == nil {
		t.Fatal("conversation_type=2 的单聊事件同样必须落库")
	}
	if hub2.IsGroup {
		t.Error("不得从 conversation_type 推断群聊（官方示例自身就与它不一致，见 §16.3）")
	}
}

// TestBatchG_DispatchDouyin_SendEventIsOutbound im_send_msg 是「用户发送私信触发」，
// 即我方/客户侧的**发出**回声。它必须落出站行，且不得驱动一次「回复自己发出去的话」。
func TestBatchG_DispatchDouyin_SendEventIsOutbound(t *testing.T) {
	svc, ctx, _ := gDySetup(t)

	raw := `{"event":"im_send_msg","client_key":"myclientkey","from_user_id":"seller-open-id","to_user_id":"fan-open-id","content":{"conversation_short_id":"@conv-send","server_message_id":"@msg-send","conversation_type":1,"create_time":1681303285997,"message_type":"text","text":"您好，稍后联系您","source":"myclientkey"}}`
	hub, extra, err := svc.dispatchDouyin(ctx, ChannelDouyin, "gdy-send", &ParsedPayload{EventID: "gdy-send-1"}, []byte(raw))
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if hub == nil {
		t.Fatal("im_send_msg 也要留痕（出站回声），不能整条丢弃")
	}
	if hub.Direction != "outbound" {
		t.Errorf("im_send_msg 必须是出站方向，got %q", hub.Direction)
	}
	if hub.SenderID != "seller-open-id" {
		t.Errorf("出站行的 sender 应为 from_user_id，got %q", hub.SenderID)
	}
	// 不打 `extra != nil &&` 这种自禁用前缀：本实现恒返回非 nil extra，
	// 写成条件断言等于哪天实现改成返回 nil 时这条悄悄失效。
	if extra == nil {
		t.Fatal("dispatchDouyin 约定返回非 nil extra（出站回声的商机判定要看得见）")
	}
	if extra.NewOpportunity {
		t.Error("自己发出的消息不得被判成新商机")
	}
	if src, _ := hub.Extra["source"].(string); src != "myclientkey" {
		t.Errorf("官方 source 字段（区分发出应用）要留在 Extra 里，got %v", hub.Extra["source"])
	}
}

// TestBatchG_DispatchDouyin_ContentAsJSONObjectOrString content 在小程序页是「结构体」，
// 而 dop 侧同类 webhook 的 content 出现过 JSON 字符串形态（同一平台两种外壳）。
// 只接受其中一种，就是拿文档的一半当全部。
func TestBatchG_DispatchDouyin_ContentAsJSONObjectOrString(t *testing.T) {
	svc, ctx, _ := gDySetup(t)

	objectRaw := `{"event":"im_receive_msg","from_user_id":"u-obj","content":{"conversation_short_id":"@c-obj","server_message_id":"@m-obj","create_time":1681303285997,"message_type":"text","text":"对象形态"}}`
	hubObj, _, err := svc.dispatchDouyin(ctx, ChannelDouyin, "gdy-form", &ParsedPayload{EventID: "gdy-form-1"}, []byte(objectRaw))
	if err != nil || hubObj == nil {
		t.Fatalf("content 为对象时必须落库，got hub=%v err=%v", hubObj, err)
	}

	stringRaw := `{"event":"im_receive_msg","from_user_id":"u-str","content":"{\"conversation_short_id\":\"@c-str\",\"server_message_id\":\"@m-str\",\"create_time\":1681303285997,\"message_type\":\"text\",\"text\":\"字符串形态\"}"}`
	hubStr, _, err := svc.dispatchDouyin(ctx, ChannelDouyin, "gdy-form", &ParsedPayload{EventID: "gdy-form-2"}, []byte(stringRaw))
	if err != nil || hubStr == nil {
		t.Fatalf("content 为 JSON 字符串时同样要落库，got hub=%v err=%v", hubStr, err)
	}
	if hubStr.Content != "字符串形态" {
		t.Errorf("字符串形态 content 的正文也要解出来，got %q", hubStr.Content)
	}
	if hubObj.Content != "对象形态" {
		t.Errorf("对象形态 content 的正文错位，got %q", hubObj.Content)
	}
}

// TestBatchG_DispatchDouyin_MsgTypesInsideHubVocabulary 官方 8 个 message_type 都要落在
// 中台词表内（message_hub.msg_type 只有 text/image/file/audio/video/link/card/location/event）。
// 越词表的写法在走 hub.Push 的渠道上是**整条消息被拒**，在直写 repo 的渠道上是**永远筛不到**。
func TestBatchG_DispatchDouyin_MsgTypesInsideHubVocabulary(t *testing.T) {
	svc, ctx, _ := gDySetup(t)

	cases := []struct {
		messageType string
		contentSeg  string
		wantType    string
		wantContent string
	}{
		{"text", `"text":"在吗"`, "text", "在吗"},
		{"other", `"text":"你收到一条新消息，请打开抖音app查看"`, "text", "你收到一条新消息，请打开抖音app查看"},
		{"image", ``, "image", "[图片]"},
		{"user_local_image", ``, "image", "[图片]"},
		{"emoji", `"resource_url":"https://p3.douyinpic.com/emoji.gif","resource_type":"gif"`, "image", "[表情]"},
		{"video", `"item_id":"@72NwHyW53"`, "video", "[视频]"},
		{"user_local_video", ``, "video", "[视频]"},
		{"retain_consult_card", `"card_id":"@72MqAjfym","card_status":2`, "card", "[留资卡片]"},
		{"future_unknown_type", ``, "text", "[future_unknown_type]"},
	}
	for _, tc := range cases {
		t.Run(tc.messageType, func(t *testing.T) {
			content := `"conversation_short_id":"@conv-` + tc.messageType + `",` +
				`"server_message_id":"@msg-` + tc.messageType + `",` +
				`"create_time":1681303285997,"message_type":"` + tc.messageType + `"`
			if tc.contentSeg != "" {
				content += "," + tc.contentSeg
			}
			raw := `{"event":"im_receive_msg","client_key":"kk","from_user_id":"u-type-` + tc.messageType +
				`","log_id":"lg-1","content":{` + content + `,"index":"1"}}`
			hub, _, err := svc.dispatchDouyin(ctx, ChannelDouyin, "gdy-type", &ParsedPayload{EventID: "gdy-type-" + tc.messageType}, []byte(raw))
			if err != nil {
				t.Fatalf("dispatch message_type=%s: %v", tc.messageType, err)
			}
			if hub == nil {
				t.Fatalf("message_type=%s 的事件被整条丢弃", tc.messageType)
			}
			if !messageHubMsgTypes[hub.MsgType] {
				t.Errorf("msg_type=%q 越过中台词表", hub.MsgType)
			}
			if hub.MsgType != tc.wantType {
				t.Errorf("message_type=%s 应映射成 %s，got %s", tc.messageType, tc.wantType, hub.MsgType)
			}
			if hub.Content != tc.wantContent {
				t.Errorf("message_type=%s 正文应为 %q，got %q", tc.messageType, tc.wantContent, hub.Content)
			}
		})
	}
}

// TestBatchG_DispatchDouyin_NonMessageEventsWriteNoRow 这条通道同时承载 URL 校验、
// 进入会话、加群审核、授权等非会话事件（D-04 已确立的规则）。换成官方信封后仍要成立，
// 且不能靠「sender 恰好解不出来」实现 —— 这些事件**是有 from_user_id 的**。
func TestBatchG_DispatchDouyin_NonMessageEventsWriteNoRow(t *testing.T) {
	svc, ctx, _ := gDySetup(t)

	cases := map[string]string{
		"verify_webhook":         `{"event":"verify_webhook","client_key":"","content":{"challenge":12345}}`,
		"im_enter_direct_msg":    `{"event":"im_enter_direct_msg","client_key":"kk","from_user_id":"u-enter","to_user_id":"bot","content":{"conversation_short_id":"@c-enter"}}`,
		"group_fans_event":       `{"event":"group_fans_event","client_key":"kk","from_user_id":"u-fans","to_user_id":"bot","content":{"group_id":"g1"}}`,
		"enter_group_audit_chan": `{"event":"enter_group_audit_change","client_key":"kk","from_user_id":"u-audit","to_user_id":"bot","content":{"group_id":"g1"}}`,
	}
	for name, raw := range cases {
		hub, _, err := svc.dispatchDouyin(ctx, ChannelDouyin, "gdy-none", &ParsedPayload{EventID: "gdy-none-" + name}, []byte(raw))
		if err != nil {
			t.Errorf("%s dispatch 不应报错: %v", name, err)
		}
		if hub != nil {
			t.Errorf("%s 不是会话消息，不得落 hub 行/建假会话，got %+v", name, hub)
		}
	}
	var rows int64
	if err := svc.lazyDB().Model(&model.MessageHub{}).Where("account_id = ?", "gdy-none").Count(&rows).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 0 {
		t.Errorf("四类非会话事件应零落库，实际 %d 行", rows)
	}
}

// TestBatchG_DispatchDouyin_UnknownEventWithSenderStillVisible 官方事件表还会新增
// （dop 侧另有 contract_authorize 等）：带 from_user_id 却不在已知会话事件集合里的名字，
// 不能凭空造出一行会话消息，也不许 panic。
func TestBatchG_DispatchDouyin_UnknownEventWithSenderStillVisible(t *testing.T) {
	svc, ctx, _ := gDySetup(t)
	raw := `{"event":"some_future_im_event","client_key":"kk","from_user_id":"u-future","content":{"message_type":"text","text":"新事件"}}`
	hub, _, err := svc.dispatchDouyin(ctx, ChannelDouyin, "gdy-future", &ParsedPayload{EventID: "gdy-future-1"}, []byte(raw))
	if err != nil {
		t.Fatalf("未知事件不得报错: %v", err)
	}
	if hub != nil {
		t.Errorf("未知事件名不得被当成会话消息落库（避免假会话），got %+v", hub)
	}
}

// ─── G-4 幂等键（账号作用域 + 列宽） ─────────────────────────────

// TestBatchG_DispatchDouyin_SameServerMsgIDAcrossAccountsBothLand N-16 的抖音版：
// 幂等键原先是 `dy_ + server_message_id`，不带账号。官方消息 ID 是**每应用**作用域的，
// 两个抖音应用各自会话里的同一条消息可能解出同一个串（更常见的是同一租户换绑/复制账号），
// 第二家会在 (platform,msg_id,conversation_id) 唯一键上撞死 —— 而这里 Create 的 UNIQUE 错
// 是被吞掉的（只降级成日志），于是第二家的客户消息永远查不到，接口却回 200。
func TestBatchG_DispatchDouyin_SameServerMsgIDAcrossAccountsBothLand(t *testing.T) {
	svc, ctx, _ := gDySetup(t)

	mk := func(account string) []byte {
		return []byte(`{"event":"im_receive_msg","from_user_id":"u-share-` + account + `","content":{"conversation_short_id":"@conv-share","server_message_id":"@msg-share","create_time":1681303285997,"message_type":"text","text":"同一条消息 ID"}}`)
	}
	h1, _, err := svc.dispatchDouyin(ctx, ChannelDouyin, "acct-A", &ParsedPayload{EventID: "gdy-a1"}, mk("acct-A"))
	if err != nil || h1 == nil {
		t.Fatalf("acct-A 必须落库: hub=%v err=%v", h1, err)
	}
	h2, _, err := svc.dispatchDouyin(ctx, ChannelDouyin, "acct-B", &ParsedPayload{EventID: "gdy-b1"}, mk("acct-B"))
	if err != nil || h2 == nil {
		t.Fatalf("acct-B 必须落库: hub=%v err=%v", h2, err)
	}
	if h1.MsgID == h2.MsgID {
		t.Errorf("幂等键必须含账号作用域，否则第二家撞唯一键被静默吞掉（两家都是 %s）", h1.MsgID)
	}

	var rows int64
	if err := svc.lazyDB().Model(&model.MessageHub{}).
		Where("account_id IN ?", []string{"acct-A", "acct-B"}).Count(&rows).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 2 {
		t.Errorf("两个账号各 1 行，实际 %d 行", rows)
	}
}

// TestBatchG_DispatchDouyin_MsgIDDeterministicAndWithinColumnLimit 同一条消息重投要得到同一个
// MsgID（幂等），不同消息要不同；且官方 server_message_id 是 88 字符的 base64（含 + / =），
// 加上前缀与账号后仍要容得下 msg_id varchar(100) —— 超列宽会让插入直接失败，而失败是被吞掉的。
func TestBatchG_DispatchDouyin_MsgIDDeterministicAndWithinColumnLimit(t *testing.T) {
	svc, ctx, _ := gDySetup(t)

	longAccount := "dy-account-with-a-very-long-identifier-0123456789"
	first, _, err := svc.dispatchDouyin(ctx, ChannelDouyin, longAccount, &ParsedPayload{EventID: "gdy-d1"}, []byte(gDyTextEventRaw))
	if err != nil || first == nil {
		t.Fatalf("dispatch1: hub=%v err=%v", first, err)
	}
	second, _, err := svc.dispatchDouyin(ctx, ChannelDouyin, longAccount, &ParsedPayload{EventID: "gdy-d2"}, []byte(gDyTextEventRaw))
	if err != nil || second == nil {
		t.Fatalf("dispatch2: hub=%v err=%v", second, err)
	}
	if first.MsgID != second.MsgID {
		t.Errorf("同一条官方消息两次投递 MsgID 必须一致，got %q vs %q", first.MsgID, second.MsgID)
	}
	if n := len(first.MsgID); n > 100 {
		t.Errorf("MsgID 长度 %d 超出 msg_id varchar(100)：插入会失败且错误被吞掉（值=%q）", n, first.MsgID)
	}
	if strings.ContainsAny(first.MsgID, "+/=@ ") {
		t.Errorf("MsgID 不该带官方 ID 的 base64 特殊字符（长度不可控、日志难读），got %q", first.MsgID)
	}
	if !strings.Contains(first.MsgID, longAccount) {
		t.Errorf("MsgID 要含账号作用域段，got %q", first.MsgID)
	}

	// 不同 server_message_id → 不同键。
	other := strings.Replace(gDyTextEventRaw, "PPyAPpF2rgykLFgbafb560zdRmYqig357zEBmWXVPshhwyd", "PPyAPpF2rgykLFgbafb560zdRmYqig99999BmWXVPshhwyd", 1)
	third, _, err := svc.dispatchDouyin(ctx, ChannelDouyin, longAccount, &ParsedPayload{EventID: "gdy-d3"}, []byte(other))
	if err != nil || third == nil {
		t.Fatalf("dispatch3: hub=%v err=%v", third, err)
	}
	if third.MsgID == first.MsgID {
		t.Errorf("不同 server_message_id 不得共用 MsgID：%s", first.MsgID)
	}

	// 官方 ID 原文要留在 Extra，便于按抖音侧 ID 回查。
	if got, _ := first.Extra["server_message_id"].(string); got != gDyServerMsgID {
		t.Errorf("Extra.server_message_id 应为官方原文，got %q", got)
	}
}

// TestBatchG_DispatchDouyin_MissingServerMessageIDFallsBackToContentKey server_message_id
// 缺失（官方表格标的是「必填」，但取不到时不能整条丢，也不能恒定撞同一个键）。
func TestBatchG_DispatchDouyin_MissingServerMessageIDFallsBackToContentKey(t *testing.T) {
	svc, ctx, _ := gDySetup(t)

	mk := func(text string) []byte {
		return []byte(`{"event":"im_receive_msg","from_user_id":"u-noid","content":{"conversation_short_id":"@c-noid","create_time":1681303285997,"message_type":"text","text":"` + text + `"}}`)
	}
	h1, _, err := svc.dispatchDouyin(ctx, ChannelDouyin, "gdy-noid", &ParsedPayload{EventID: "n1"}, mk("同内容"))
	if err != nil || h1 == nil {
		t.Fatalf("缺官方 ID 也必须落库: hub=%v err=%v", h1, err)
	}
	h2, _, err := svc.dispatchDouyin(ctx, ChannelDouyin, "gdy-noid", &ParsedPayload{EventID: "n2"}, mk("同内容"))
	if err != nil || h2 == nil {
		t.Fatalf("第二次投递: hub=%v err=%v", h2, err)
	}
	if h1.MsgID != h2.MsgID {
		t.Errorf("同一条重投的兜底键要稳定，got %q vs %q", h1.MsgID, h2.MsgID)
	}
	h3, _, err := svc.dispatchDouyin(ctx, ChannelDouyin, "gdy-noid", &ParsedPayload{EventID: "n3"}, mk("另一条内容"))
	if err != nil || h3 == nil {
		t.Fatalf("第三次投递: hub=%v err=%v", h3, err)
	}
	if h3.MsgID == h1.MsgID {
		t.Error("不同内容不得共用兜底键（否则第二条被唯一键吞掉）")
	}
}

// TestBatchG_DispatchDouyin_TolerantNumberShapes create_time 官方表格标 int，但同类文档
// 的 index 标 int 而示例全是字符串 ⇒ 数值字段形态不可信。声明成强类型数字会让
// **整包 Unmarshal 失败**（encoding/json 遇到类型不符就报错），那条消息就又蒸发了。
func TestBatchG_DispatchDouyin_TolerantNumberShapes(t *testing.T) {
	svc, ctx, _ := gDySetup(t)

	raw := `{"event":"im_receive_msg","from_user_id":"u-flex","content":{"conversation_short_id":"@c-flex","server_message_id":"@m-flex","create_time":"1681303285997","conversation_type":"1","message_type":"text","text":"字符串时间戳"}}`
	hub, _, err := svc.dispatchDouyin(ctx, ChannelDouyin, "gdy-flex", &ParsedPayload{EventID: "gdy-flex"}, []byte(raw))
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if hub == nil {
		t.Fatal("数值形态与文档表格不符时不得整条丢弃")
	}
	if !hub.SentAt.Equal(time.UnixMilli(gDyCreateTimeMS)) {
		t.Errorf("字符串形态 create_time 也要解出时间，got %v", hub.SentAt)
	}

	// 完全取不到 create_time 时退回当前时间（不能落 0001-01-01 这种把会话排序打乱的值）。
	noTime := `{"event":"im_receive_msg","from_user_id":"u-nots","content":{"conversation_short_id":"@c-nots","server_message_id":"@m-nots","message_type":"text","text":"没有时间戳"}}`
	hub2, _, err := svc.dispatchDouyin(ctx, ChannelDouyin, "gdy-flex", &ParsedPayload{EventID: "gdy-nots"}, []byte(noTime))
	if err != nil || hub2 == nil {
		t.Fatalf("无 create_time 也要落库: hub=%v err=%v", hub2, err)
	}
	if time.Since(hub2.SentAt) > time.Minute || time.Until(hub2.SentAt) > time.Minute {
		t.Errorf("缺 create_time 时应退回当前时间，got %v", hub2.SentAt)
	}
}

// TestBatchG_DispatchDouyin_SenderlessEventStillWritesNoRow 保留 D-04 的规则，但换成官方形态：
// 审核/授权类事件即便带了 to_user_id 而没有 from_user_id，也不得建出一条无法回复的假会话。
func TestBatchG_DispatchDouyin_SenderlessEventStillWritesNoRow(t *testing.T) {
	svc, ctx, _ := gDySetup(t)
	raw := `{"event":"im_receive_msg","client_key":"kk","to_user_id":"bot-only","content":{"conversation_short_id":"@c","server_message_id":"@m","message_type":"text","text":"没人发的消息"}}`
	hub, _, err := svc.dispatchDouyin(ctx, ChannelDouyin, "gdy-nosender", &ParsedPayload{EventID: "gdy-nosender"}, []byte(raw))
	if err != nil || hub != nil {
		t.Errorf("无 from_user_id 的事件不得落 hub 行，got hub=%v err=%v", hub, err)
	}
}

// TestBatchG_DispatchDouyin_EnvelopeFieldsInExtra 官方留的排查抓手（log_id 用于
// 「抖音内部排查问题」）必须可回查，否则客服报障时我们拿不出任何对得上的标识。
func TestBatchG_DispatchDouyin_EnvelopeFieldsInExtra(t *testing.T) {
	svc, ctx, _ := gDySetup(t)
	raw := `{"event":"im_receive_msg","client_key":"my_ck","log_id":"2023429834752345783","from_user_id":"u-extra","to_user_id":"bot","content":{"conversation_short_id":"@c-extra","server_message_id":"@m-extra","create_time":1681303285997,"message_type":"text","text":"要能回查"}}`
	hub, _, err := svc.dispatchDouyin(ctx, ChannelDouyin, "gdy-extra", &ParsedPayload{EventID: "gdy-extra"}, []byte(raw))
	if err != nil || hub == nil {
		t.Fatalf("dispatch: hub=%v err=%v", hub, err)
	}
	if got, _ := hub.Extra["log_id"].(string); got != "2023429834752345783" {
		t.Errorf("Extra.log_id 要留官方排查 ID，got %v", hub.Extra["log_id"])
	}
	if got, _ := hub.Extra["event"].(string); got != "im_receive_msg" {
		t.Errorf("Extra.event 要留官方事件名（群/单聊与方向的判定依据），got %v", hub.Extra["event"])
	}
	if got, _ := hub.Extra["conversation_short_id"].(string); got != "@c-extra" {
		t.Errorf("Extra.conversation_short_id 要留官方原文，got %v", hub.Extra["conversation_short_id"])
	}
}

// TestBatchG_DispatchDouyin_UnparseableBodyStillGoesGeneric 结构解析失败时的兜底路径
// （dispatchDouyinGeneric）不能被这次重写弄丢：非 JSON 的怪报文要有留痕机会，
// 而不是让 dispatch 直接 panic 或报错打断队列。
func TestBatchG_DispatchDouyin_UnparseableBodyStillGoesGeneric(t *testing.T) {
	svc, ctx, _ := gDySetup(t)

	hub, _, err := svc.dispatchDouyin(ctx, ChannelDouyin, "gdy-broken",
		&ParsedPayload{EventID: "gdy-broken", Sender: "u-broken", Content: "浏览器桥上报的正文"},
		[]byte("{not-json"))
	if err != nil {
		t.Fatalf("坏报文不得报错: %v", err)
	}
	if hub == nil {
		t.Fatal("通用兜底分支失效：非 JSON 报文在带 sender/content 时仍应落库")
	}
	if hub.Content != "浏览器桥上报的正文" {
		t.Errorf("兜底分支正文错位，got %q", hub.Content)
	}
}

// TestBatchG_DispatchDouyin_ExtraIsJSONSerializable hub.Extra 要能整包落库（JSONMap 列）。
// 这一条盯的是「往 Extra 里塞了 json.RawMessage」这类编译期看不出来、
// 落库时才炸的写法。
func TestBatchG_DispatchDouyin_ExtraIsJSONSerializable(t *testing.T) {
	svc, ctx, _ := gDySetup(t)
	hub, _, err := svc.dispatchDouyin(ctx, ChannelDouyin, "gdy-json", &ParsedPayload{EventID: "gdy-json"}, []byte(gDyTextEventRaw))
	if err != nil || hub == nil {
		t.Fatalf("dispatch: hub=%v err=%v", hub, err)
	}
	if _, merr := json.Marshal(hub.Extra); merr != nil {
		t.Errorf("Extra 必须可 JSON 序列化（要写进 message_hub.extra 列）: %v", merr)
	}
}
