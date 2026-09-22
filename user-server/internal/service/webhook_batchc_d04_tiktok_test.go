package service

// 审计 D-04：TikTok 入站验签按官方契约收口。
//
// 官方原文（https://developers.tiktok.com/doc/webhooks-verification ，2026-09-20 取到）：
//   签名放在 `TikTok-Signature` 头里，值是逗号分隔的 `t=<timestamp>,s=<signature>`；
//   `signed_payload` = 时间戳字符串 + `.` + 请求体原始 JSON；
//   摘要 = HMAC-SHA256(client_secret, signed_payload)，十六进制。
//
// 修复前 tiktok 与 douyin 共用 verifyHMAC —— 只签 body、不查这个头、也不带时间戳，
// 真实回调 100% 验不过（表现为 tiktok 入站整条静默死路，而不是可绕过的洞）。
// 因此本文件的反向锚点是「按旧的 body-only 口径造签名必须被拒」：
// 这条一旦变绿，说明两条渠道又被并进同一个分支了。

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
)

const (
	d04TiktokSecret = "tt_client_secret_d04"
	d04DouyinSecret = "dy_secret_d04"
	d04Timestamp    = "1633174587"
)

// tiktokOfficialSign 独立按官方口径造签名，不复用被测实现（否则夹具与实现互相印证＝假绿）。
func tiktokOfficialSign(secret, ts string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// tiktokLegacySign 复刻修复前的错误口径：只对 body 做 HMAC。仅作反向锚点。
func tiktokLegacySign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestParseTiktokSignature_OfficialShapeAndStructuralErrors(t *testing.T) {
	body := []byte(`{"event_id":"d04-parse","content":"hi"}`)
	good := tiktokOfficialSign(d04TiktokSecret, d04Timestamp, body)

	cases := []struct {
		name    string
		header  string
		wantSig string
		wantTS  string
		wantErr string // 空串表示必须解析成功
	}{
		{"官方形态", "t=" + d04Timestamp + ",s=" + good, good, d04Timestamp, ""},
		{"逗号后带空格", "t=" + d04Timestamp + ", s=" + good, good, d04Timestamp, ""},
		{"段顺序颠倒", "s=" + good + ",t=" + d04Timestamp, good, d04Timestamp, ""},
		{"容忍未知段", "v1=x,t=" + d04Timestamp + ",s=" + good, good, d04Timestamp, ""},
		{"空头", "", "", "", "missing TikTok-Signature"},
		{"缺 s 段", "t=" + d04Timestamp, "", "", "必须同时带"},
		{"缺 t 段", "s=" + good, "", "", "必须同时带"},
		{"时间戳非数字", "t=abc,s=" + good, "", "", "不是数字时间戳"},
		{"无键值对", good, "", "", "必须同时带"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sig, ts, err := parseTiktokSignature(tc.header)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("结构残缺必须报错（不能静默判成「签名不对」），got sig=%q ts=%q", sig, ts)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("报错要点明 %q，got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("合法头不得报错: %v", err)
			}
			if sig != tc.wantSig || ts != tc.wantTS {
				t.Errorf("解析错位 sig=%q ts=%q want sig=%q ts=%q", sig, ts, tc.wantSig, tc.wantTS)
			}
		})
	}
}

func TestVerifyTiktokWebhook_Contract(t *testing.T) {
	body := []byte(`{"event_id":"d04-v","content":"你好"}`)

	ok, err := verifyTiktokWebhook(d04TiktokSecret, body,
		map[string]string{"TikTok-Signature": "t=" + d04Timestamp + ",s=" + tiktokOfficialSign(d04TiktokSecret, d04Timestamp, body)})
	if err != nil || !ok {
		t.Fatalf("官方口径签名必须通过，got ok=%v err=%v", ok, err)
	}

	// 反向锚点 1：只签 body（修复前口径）不得通过 —— 时间戳没进被签串就是契约违背。
	legacy := "t=" + d04Timestamp + ",s=" + tiktokLegacySign(d04TiktokSecret, body)
	if ok, _ := verifyTiktokWebhook(d04TiktokSecret, body, map[string]string{"TikTok-Signature": legacy}); ok {
		t.Error("body-only HMAC（旧口径）不得验签通过")
	}

	// 反向锚点 2：时间戳参与签名 ⇒ 换时间戳必须失效（否则签名可被无限期重放）。
	stamped := tiktokOfficialSign(d04TiktokSecret, d04Timestamp, body)
	if ok, _ := verifyTiktokWebhook(d04TiktokSecret, body,
		map[string]string{"TikTok-Signature": "t=1633174588,s=" + stamped}); ok {
		t.Error("签名与时间戳绑定：换 t= 后必须验不过")
	}

	// 反向锚点 3：body 改一个字节必须失效。
	tampered := append(append([]byte{}, body...), '!')
	if ok, _ := verifyTiktokWebhook(d04TiktokSecret, tampered,
		map[string]string{"TikTok-Signature": "t=" + d04Timestamp + ",s=" + stamped}); ok {
		t.Error("body 被改后不得验签通过")
	}

	// 反向锚点 4：密钥不同必须失效（不能是恒真比较）。
	if ok, _ := verifyTiktokWebhook("other_secret", body,
		map[string]string{"TikTok-Signature": "t=" + d04Timestamp + ",s=" + stamped}); ok {
		t.Error("换密钥后不得验签通过")
	}

	// 缺头要报错而不是静默 false：否则渠道侧看到的是一个无法归因的 400。
	if ok, err := verifyTiktokWebhook(d04TiktokSecret, body, map[string]string{}); ok || err == nil {
		t.Errorf("缺签名头要报错，got ok=%v err=%v", ok, err)
	}
}

// TestVerifyTiktokWebhook_HeaderNameIsCaseInsensitive Go 的 textproto 规范化会把
// `TikTok-Signature` 落成 `Tiktok-signature`，HTTP 层递到 service 的就是那个拼法；
// 按字面量直取等于「单测全绿、真实回调全挂」。单独成测，让这条性质的失败信号
// 不与「时间戳没进被签串」共用同一个用例名。
func TestVerifyTiktokWebhook_HeaderNameIsCaseInsensitive(t *testing.T) {
	body := []byte(`{"event_id":"d04-case","content":"你好"}`)
	header := "t=" + d04Timestamp + ",s=" + tiktokOfficialSign(d04TiktokSecret, d04Timestamp, body)

	for _, spelling := range []string{"TikTok-Signature", "Tiktok-Signature", "tiktok-signature", "TIKTOK-SIGNATURE"} {
		ok, err := verifyTiktokWebhook(d04TiktokSecret, body, map[string]string{spelling: header})
		if err != nil || !ok {
			t.Errorf("头名 %s 也要认（HTTP 层会规范化大小写），got ok=%v err=%v", spelling, ok, err)
		}
	}
}

// TestWebhookService_Verify_TiktokNotSharedWithDouyinBranch 渠道分支必须分开：
// tiktok 走官方 t=/s= HMAC，抖音走官方 hex(sha1(client_secret ‖ body))（批G 取到原文，见 §16.1）。
func TestWebhookService_Verify_TiktokNotSharedWithDouyinBranch(t *testing.T) {
	db := setupWebhookTestDB(t)
	s := NewWebhookService(db)
	defer s.Stop(context.Background())

	if err := db.Create(&model.IntegrationAccount{Platform: string(ChannelTiktok), APISecret: d04TiktokSecret, Status: 1}).Error; err != nil {
		t.Fatalf("seed tiktok: %v", err)
	}
	if err := db.Create(&model.IntegrationAccount{Platform: string(ChannelDouyin), APISecret: d04DouyinSecret, Status: 1}).Error; err != nil {
		t.Fatalf("seed douyin: %v", err)
	}

	body := []byte(`{"event_id":"d04-branch","content":"hi"}`)
	official := "t=" + d04Timestamp + ",s=" + tiktokOfficialSign(d04TiktokSecret, d04Timestamp, body)

	if ok, err := s.Verify(context.Background(), ChannelTiktok, "1", body, map[string]string{"TikTok-Signature": official}, nil); err != nil || !ok {
		t.Errorf("tiktok 官方签名必须通过，got ok=%v err=%v", ok, err)
	}
	if ok, _ := s.Verify(context.Background(), ChannelTiktok, "1", body,
		map[string]string{"X-Douyin-Signature": tiktokLegacySign(d04TiktokSecret, body)}, nil); ok {
		t.Error("tiktok 分支不得再吃抖音的 body-only 签名")
	}
	// 缺头必须报出缺哪个头，便于定位反代丢头。
	if _, err := s.Verify(context.Background(), ChannelTiktok, "1", body, map[string]string{}, nil); err == nil ||
		!strings.Contains(err.Error(), "TikTok-Signature") {
		t.Errorf("tiktok 缺头要点名 TikTok-Signature，got %v", err)
	}

	// 抖音侧：官方 sha1 拼接签名必须通过，且两家算法互不吃对方（反向断言见批G 的同名用例）。
	if ok, err := s.Verify(context.Background(), ChannelDouyin, "1", body,
		map[string]string{"X-Douyin-Signature": gDyOfficialSign(d04DouyinSecret, body)}, nil); err != nil || !ok {
		t.Errorf("douyin 的 X-Douyin-Signature 应通过，got ok=%v err=%v", ok, err)
	}
	if ok, _ := s.Verify(context.Background(), ChannelDouyin, "1", body,
		map[string]string{"X-Douyin-Signature": tiktokLegacySign(d04DouyinSecret, body)}, nil); ok {
		t.Error("douyin 分支不得再吃修复前的 HMAC-SHA256(body) 口径")
	}
}

// TestWebhookService_Verify_DouyinIgnoresLarkHeader 抖音分支原先把 X-Lark-Signature 排在
// X-Douyin-Signature 之前，而 verifyHMAC 取「第一个非空」头 —— 飞书签名是 base64、
// 与这里的 hex 永不相等，结果是一条合法抖音回调被一个无关头挤掉。
func TestWebhookService_Verify_DouyinIgnoresLarkHeader(t *testing.T) {
	db := setupWebhookTestDB(t)
	s := NewWebhookService(db)
	defer s.Stop(context.Background())

	if err := db.Create(&model.IntegrationAccount{Platform: string(ChannelDouyin), APISecret: d04DouyinSecret, Status: 1}).Error; err != nil {
		t.Fatalf("seed douyin: %v", err)
	}
	body := []byte(`{"event_id":"d04-lark","content":"hi"}`)
	headers := map[string]string{
		"X-Lark-Signature":   "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY=",
		"X-Douyin-Signature": gDyOfficialSign(d04DouyinSecret, body),
	}
	if ok, err := s.Verify(context.Background(), ChannelDouyin, "1", body, headers, nil); err != nil || !ok {
		t.Errorf("携带无关飞书头时合法抖音签名仍须生效，got ok=%v err=%v", ok, err)
	}
	// 反向：只有飞书头、没有抖音头时不得被当成合法（那是另一家的摘要算法与编码）。
	onlyLark := map[string]string{"X-Lark-Signature": headers["X-Lark-Signature"]}
	if ok, err := s.Verify(context.Background(), ChannelDouyin, "1", body, onlyLark, nil); ok {
		t.Errorf("仅带飞书签名头不得被抖音分支受理，got ok=%v err=%v", ok, err)
	} else if err == nil || !strings.Contains(err.Error(), "X-Douyin-Signature") {
		t.Errorf("缺头要点名 X-Douyin-Signature，got %v", err)
	}
}

// TestWebhookService_Receive_Tiktok_LegacySignatureRejected 端到端一点：旧口径签名
// 必须判「验签失败」且不留事件行；官方口径才允许进漏斗。
func TestWebhookService_Receive_Tiktok_LegacySignatureRejected(t *testing.T) {
	db := newD03DispatchDB(t)
	s := NewWebhookService(db)
	defer s.Stop(context.Background())

	if err := db.Create(&model.IntegrationAccount{Platform: string(ChannelTiktok), APISecret: d04TiktokSecret, Status: 1}).Error; err != nil {
		t.Fatalf("seed tiktok: %v", err)
	}
	// 非消息事件形状（官方 URL 校验/通知类回调）：既无 sender 也无会话内容。
	body := []byte(`{"event_id":"d04-recv-ping","event_type":"url_verification","challenge":"cz1"}`)

	res, err := s.Receive(context.Background(), &ReceiveRequest{
		Channel: ChannelTiktok, AccountID: "d04-acc", Body: body,
		Headers: map[string]string{"Signature": tiktokLegacySign(d04TiktokSecret, body)},
	})
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if res.Accepted || !res.VerifyFail {
		t.Errorf("旧口径签名必须判验签失败，got %+v", res)
	}
	var rejectedRows int64
	if err := db.Model(&model.WebhookEvent{}).Where("raw_data LIKE ?", "%d04-recv-ping%").Count(&rejectedRows).Error; err != nil {
		t.Fatalf("count rejected: %v", err)
	}
	if rejectedRows != 0 {
		t.Errorf("验签失败不得留下事件行，实际 %d 条", rejectedRows)
	}

	official := "t=" + d04Timestamp + ",s=" + tiktokOfficialSign(d04TiktokSecret, d04Timestamp, body)
	res2, err := s.Receive(context.Background(), &ReceiveRequest{
		Channel: ChannelTiktok, AccountID: "d04-acc", Body: body,
		Headers: map[string]string{"TikTok-Signature": official},
	})
	if err != nil {
		t.Fatalf("Receive official: %v", err)
	}
	if !res2.Accepted {
		t.Fatalf("官方口径签名必须被受理，got %+v", res2)
	}
	var acceptedRows int64
	if err := db.Model(&model.WebhookEvent{}).Where("raw_data LIKE ?", "%d04-recv-ping%").Count(&acceptedRows).Error; err != nil {
		t.Fatalf("count accepted: %v", err)
	}
	if acceptedRows != 1 {
		t.Errorf("官方签名回调要留下 1 条事件行，实际 %d 条", acceptedRows)
	}
	// 非消息事件不得落收件箱（D-03 同口径，这里连 content 兜底行也不允许有）。
	var hubRows int64
	if err := db.Model(&model.MessageHub{}).Where("account_id = ?", "d04-acc").Count(&hubRows).Error; err != nil {
		t.Fatalf("count hub: %v", err)
	}
	if hubRows != 0 {
		t.Errorf("无 sender 的 tiktok 事件不应落收件箱，实际 %d 行", hubRows)
	}
}

// TestDispatchDouyin_SenderlessEventWritesNoHubRow D-04 连带发现：抖音/TikTok 的回调
// 通道也承载 URL 校验、审核通知等非会话事件，此前结构化分支照样建 hub 行并驱动 AI。
// 反向：真带 from.user_id 的消息事件必须照常落库，别把修复做成新的丢消息洞。
func TestDispatchDouyin_SenderlessEventWritesNoHubRow(t *testing.T) {
	db := newD03DispatchDB(t)
	s := NewWebhookService(db)
	defer s.Stop(context.Background())

	ctx := context.Background()

	ping := &ParsedPayload{EventID: "d04-d-ping", EventType: "url_verification"}
	pingRaw := []byte(`{"event":"url_verification","challenge":"abc"}`)
	for _, ch := range []WebhookChannel{ChannelDouyin, ChannelTiktok} {
		hub, extra, err := s.dispatchDouyin(ctx, ch, "d04-nosender", ping, pingRaw)
		if err != nil || hub != nil || extra != nil {
			t.Errorf("%s 无 sender 的事件被判为消息：hub=%v extra=%v err=%v", ch, hub, extra, err)
		}
	}

	msg := &ParsedPayload{EventID: "d04-d-msg", EventType: "im_message"}
	msgRaw := []byte(`{"event":"im_receive_msg","client_key":"ck_d04","from_user_id":"u_d04_1","to_user_id":"bot_d04",` +
		`"content":{"conversation_short_id":"@c_d04_1","server_message_id":"m_d04_1","create_time":1681303285997,` +
		`"message_type":"text","text":"在吗","user_infos":[{"open_id":"u_d04_1","nick_name":"小明"}]}}`)
	hub, _, err := s.dispatchDouyin(ctx, ChannelTiktok, "d04-sender", msg, msgRaw)
	if err != nil {
		t.Fatalf("带 sender 的消息必须落库: %v", err)
	}
	if hub == nil {
		t.Fatal("带 sender 的 tiktok 消息被判成了非消息事件（过度收紧）")
	}
	if hub.Platform != string(ChannelTiktok) || hub.SenderID != "u_d04_1" {
		t.Errorf("落库字段错位 platform=%q sender=%q", hub.Platform, hub.SenderID)
	}
	var hubRows int64
	if err := db.Model(&model.MessageHub{}).Where("account_id IN ?", []string{"d04-nosender", "d04-sender"}).Count(&hubRows).Error; err != nil {
		t.Fatalf("count hub: %v", err)
	}
	if hubRows != 1 {
		t.Errorf("应只有那条真消息落库（1 行），实际 %d 行", hubRows)
	}
}
