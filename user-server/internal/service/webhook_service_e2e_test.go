package service

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"hivemtk-user/internal/model"
)

func setupTestDB(t *testing.T) *gorm.DB {
	return testutil.NewTestDB(t,
		&model.WeComAccount{},
		&model.WeComCustomer{},
		&model.WeComGroup{},
		&model.WeComMessage{},
		&model.WeComTag{},
		&model.UnifiedMessage{},
		&model.MessageHub{},
		&model.InboxConversation{},
		&model.WebhookEvent{},
		&model.Customer{},
	)
}

func computeWeComSignatureURL(token, timestamp, nonce, echostr string) string {
	parts := []string{token, timestamp, nonce, echostr}
	sort.Strings(parts)
	h := sha1.Sum([]byte(strings.Join(parts, "")))
	return hex.EncodeToString(h[:])
}

// encryptWeComPlain 已删除：它按「随机 IV 前置到密文」自造夹具，那是飞书的方案；
// 企微官方是 IV=AESKey 前 16 字节、密文不前置。统一改用
// wecomOfficialEncrypt（见 wecom_crypto_official_contract_test.go），
// 避免生产实现和测试夹具同时错成一套而互相印证。

// TestWeCom_VerifyURL_OK 验证 URL 验证挑战
func TestWeCom_VerifyURL_OK(t *testing.T) {
	token := "TestToken123456"
	aesKey := "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789ABCDEFG"
	receiveID := "wxcorpid123"

	echostr := "test_echo_string_001"
	encEcho := wecomOfficialEncrypt(t, aesKey, echostr, receiveID)

	timestamp := "1700000000"
	nonce := "abc123"
	sig := computeWeComSignatureURL(token, timestamp, nonce, echostr)

	plain, err := VerifyURL(token, aesKey, sig, timestamp, nonce, encEcho)
	if err != nil {
		t.Fatalf("VerifyURL: %v", err)
	}
	plain = strings.TrimSpace(plain)
	if !strings.Contains(plain, echostr) {
		t.Errorf("expected plain to contain %q, got %q", echostr, plain)
	}
}

// TestWeCom_Decrypt_OK 验证消息解密
func TestWeCom_Decrypt_OK(t *testing.T) {
	aesKey := "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789ABCDEFG"
	receiveID := "wxcorpid"
	msg := `{"ToUserName":"wxcorpid","FromUserName":"user001","CreateTime":1700000000,"MsgType":"text","Content":"hello","MsgId":"m_001"}`

	enc := wecomOfficialEncrypt(t, aesKey, msg, receiveID)
	plain, err := DecryptWeComMessage(aesKey, enc)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !strings.Contains(string(plain), "hello") {
		t.Errorf("expected plain contain 'hello', got %s", string(plain))
	}
	if !strings.Contains(string(plain), "user001") {
		t.Errorf("expected plain contain 'user001', got %s", string(plain))
	}
}

// TestWeCom_Decrypt_BadKey 错误 EncodingAESKey 解密失败
func TestWeCom_Decrypt_BadKey(t *testing.T) {
	goodKey := "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789ABCDEFG"
	badKey := "ZYXWVUTSRQPONMLKJIHGFEDCBA9876543210ZYXWVUT"
	enc := wecomOfficialEncrypt(t, goodKey, "hello", "wxcorpid")

	_, err := DecryptWeComMessage(badKey, enc)
	if err == nil {
		t.Error("expected decrypt to fail with bad key")
	}
}

// TestWeCom_Decrypt_InvalidKeyLength 非法 key 长度
func TestWeCom_Decrypt_InvalidKeyLength(t *testing.T) {
	_, err := DecryptWeComMessage("short", "ignored")
	if err == nil {
		t.Error("expected error on invalid key length")
	}
}

// TestWeCom_Verify_OK 验签合法
func TestWeCom_Verify_OK(t *testing.T) {

	// timestamp 必须落在 verifyWeCom 的 freshness 时窗内（审计 S-05）
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := []byte(`{"encrypt":"ENC123","timestamp":"` + ts + `","nonce":"nonce123"}`)
	nonce := "nonce123"
	sig := computeWeComSignature4("MyT", ts, nonce, "ENC123")

	ok, err := verifyWeCom("MyT", "", body, map[string]string{
		"msg_signature": sig,
		"timestamp":     ts,
		"nonce":         nonce,
	})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !ok {
		t.Error("expected ok=true")
	}
}

// TestWeCom_Verify_BadSig 验签失败
func TestWeCom_Verify_BadSig(t *testing.T) {
	body := []byte(`{"MsgType":"text","Content":"hi"}`)
	ok, err := verifyWeCom("MyT", "", body, map[string]string{
		"msg_signature": "invalidsig",
		"timestamp":     "1700000123",
		"nonce":         "nonce123",
	})
	if err == nil {
		t.Error("expected error on bad sig")
	}
	if ok {
		t.Error("expected ok=false on bad sig")
	}
}

// TestWeCom_Dispatch_Ingest 入站分发到消息中台
func TestWeCom_Dispatch_Ingest(t *testing.T) {
	db := setupTestDB(t)
	acc := &model.WeComAccount{
		CorpID:         "wxcorpid",
		CorpSecret:     "secret",
		CallbackToken:  "T",
		EncodingAESKey: "K",
		WebhookEnabled: true,
	}
	db.Create(acc)

	integ := NewWeComIntegrationService(db)
	hub, conv, err := integ.IngestMessage(context.Background(), &IngestRequest{
		AccountID:      acc.ID,
		ExternalUserID: "external_user_1",
		Name:           "张三",
		MsgType:        "text",
		Content:        "你好",
		MsgID:          "msg_in_1",
		ConversationID: "wecom-conv-1",
	})
	if err != nil {
		t.Fatalf("IngestMessage: %v", err)
	}
	if hub == nil {
		t.Error("hub nil")
	}
	if conv == nil {
		t.Error("conv nil")
	}

	var hubMsg model.MessageHub
	if err := db.Where("msg_id = ?", "msg_in_1").First(&hubMsg).Error; err != nil {
		t.Errorf("hub msg not found: %v", err)
	}
	if hubMsg.SenderID != "external_user_1" {
		t.Errorf("sender: %s", hubMsg.SenderID)
	}
	if hubMsg.Content != "你好" {
		t.Errorf("content: %s", hubMsg.Content)
	}
	if hubMsg.Platform != "wecom" {
		t.Errorf("platform: %s", hubMsg.Platform)
	}
}

// TestWeCom_Dispatch_GroupMsg 群消息入站
func TestWeCom_Dispatch_GroupMsg(t *testing.T) {
	db := setupTestDB(t)
	acc := &model.WeComAccount{
		CorpID:         "wx",
		CorpSecret:     "s",
		CallbackToken:  "T",
		EncodingAESKey: "K",
		WebhookEnabled: true,
	}
	db.Create(acc)

	integ := NewWeComIntegrationService(db)
	hub, _, err := integ.IngestMessage(context.Background(), &IngestRequest{
		AccountID:      acc.ID,
		ExternalUserID: "user_2",
		Name:           "李四",
		MsgType:        "text",
		Content:        "大家好",
		MsgID:          "msg_g_1",
		ConversationID: "wecom-group-chat-1",
		IsGroup:        true,
		GroupID:        "chat_abc",
	})
	if err != nil {
		t.Fatalf("IngestMessage: %v", err)
	}
	if !hub.IsGroup {
		t.Error("expected IsGroup=true")
	}
	if hub.GroupID != "chat_abc" {
		t.Errorf("group id: %s", hub.GroupID)
	}
}

// TestWeCom_GetWeComSecrets_FromDB 从数据库读取 token
func TestWeCom_GetWeComSecrets_FromDB(t *testing.T) {
	db := setupTestDB(t)
	acc := &model.WeComAccount{
		CorpID:         "wx",
		CorpSecret:     "s",
		CallbackToken:  "T_test",
		EncodingAESKey: "K_test",
		WebhookEnabled: true,
	}
	db.Create(acc)

	svc := NewWebhookService(db)
	token, aesKey, err := svc.GetWeComSecrets(context.Background(), fmt.Sprintf("%d", acc.ID))
	if err != nil {
		t.Fatalf("getWeComSecrets: %v", err)
	}
	if token != "T_test" {
		t.Errorf("token: %s", token)
	}
	if aesKey != "K_test" {
		t.Errorf("aesKey: %s", aesKey)
	}

	token2, _, err := svc.GetWeComSecrets(context.Background(), "99999")
	if err != nil {
		t.Fatalf("fallback: %v", err)
	}
	if token2 != "T_test" {
		t.Errorf("fallback token: %s", token2)
	}
}

// TestWeCom_ReceiveCallback 完整回调流程
func TestWeCom_ReceiveCallback(t *testing.T) {
	db := setupTestDB(t)
	acc := &model.WeComAccount{
		CorpID:         "wx",
		CorpSecret:     "s",
		CallbackToken:  "T",
		EncodingAESKey: "K",
		WebhookEnabled: true,
	}
	db.Create(acc)

	integ := NewWeComIntegrationService(db)
	hub, _, err := integ.ReceiveCallback(context.Background(), &ReceiveCallbackRequest{
		AccountID: acc.ID,
		FromUser:  "from_user_x",
		FromName:  "X",
		MsgType:   "text",
		Content:   "callback_msg",
		MsgID:     "cb_1",
		ChatID:    "chat_1",
		ChatType:  "single",
	})
	if err != nil {
		t.Fatalf("ReceiveCallback: %v", err)
	}
	if hub.Content != "callback_msg" {
		t.Errorf("content: %s", hub.Content)
	}
}

// TestSalesEngine_ProcessIncomingMessage_AllChannels 4 渠道统一入口
func TestSalesEngine_ProcessIncomingMessage_AllChannels(t *testing.T) {
	engine := NewSalesEngine(nil, nil, nil, nil, nil, nil, nil, nil)
	if engine == nil {
		t.Fatal("engine nil")
	}

	channels := []struct {
		channel string
		content string
		from    string
	}{
		{"wecom", "你好", "user_wecom_1"},
		{"whatsapp", "hi", "user_wa_1"},
		{"telegram", "hello", "user_tg_1"},
		{"feishu", "在吗", "user_fs_1"},
	}
	for _, tc := range channels {
		t.Run(tc.channel, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			resp, _ := engine.ProcessIncomingMessage(ctx, &ChannelMessage{
				Channel:      tc.channel,
				AccountID:    "1",
				ExternalUser: tc.from,
				Content:      tc.content,
				MsgType:      "text",
			})
			if resp == nil {
				t.Errorf("[%s] resp nil", tc.channel)
			}
		})
	}
}

// TestSalesEngine_ProcessIncomingMessage_Empty 空内容跳过
func TestSalesEngine_ProcessIncomingMessage_Empty(t *testing.T) {
	engine := NewSalesEngine(nil, nil, nil, nil, nil, nil, nil, nil)
	resp, err := engine.ProcessIncomingMessage(context.Background(), &ChannelMessage{
		Channel: "wecom",
		Content: "",
	})
	if err != nil {
		t.Errorf("err: %v", err)
	}
	if resp == nil {
		t.Error("resp nil")
	}
}

// TestSalesEngine_ProcessIncomingMessage_Nil nil 入参
func TestSalesEngine_ProcessIncomingMessage_Nil(t *testing.T) {
	engine := NewSalesEngine(nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := engine.ProcessIncomingMessage(context.Background(), nil)
	if err == nil {
		t.Error("expected err on nil")
	}
}

// TestSalesEngine_ProcessIncomingMessage_Media 媒体消息
func TestSalesEngine_ProcessIncomingMessage_Media(t *testing.T) {
	engine := NewSalesEngine(nil, nil, nil, nil, nil, nil, nil, nil)
	for _, mt := range []string{"image", "voice", "video", "file"} {
		t.Run(mt, func(t *testing.T) {
			resp, _ := engine.ProcessIncomingMessage(context.Background(), &ChannelMessage{
				Channel:  "wecom",
				Content:  "desc",
				MsgType:  mt,
				MediaURL: "https://example.com/x",
			})
			if resp == nil {
				t.Errorf("[%s] resp nil", mt)
			}
		})
	}
}

// TestSalesEngine_ProcessIncomingMessage_Group 群消息
func TestSalesEngine_ProcessIncomingMessage_Group(t *testing.T) {
	engine := NewSalesEngine(nil, nil, nil, nil, nil, nil, nil, nil)
	resp, _ := engine.ProcessIncomingMessage(context.Background(), &ChannelMessage{
		Channel:      "wecom",
		Content:      "群消息测试",
		MsgType:      "text",
		ExternalUser: "user_grp_1",
		ChatID:       "group_chat_1",
		IsGroup:      true,
	})
	if resp == nil {
		t.Error("resp nil")
	}
}

// TestNormalizeChannelMessage 验证渠道清洗
func TestNormalizeChannelMessage(t *testing.T) {
	engine := NewSalesEngine(nil, nil, nil, nil, nil, nil, nil, nil)
	m := &ChannelMessage{
		Channel:      "wecom",
		AccountID:    "1",
		ExternalUser: "u1",
		Content:      "hello",
		MsgType:      "text",
		ChatID:       "c1",
	}
	content, sessionID, customerID := engine.normalizeChannelMessage(context.Background(), m)
	if content != "hello" {
		t.Errorf("content: %s", content)
	}
	if sessionID != "wecom:c1" {
		t.Errorf("sessionID: %s", sessionID)
	}
	if customerID != "wecom:u1" {
		t.Errorf("customerID: %s", customerID)
	}
}

// TestNormalizeChannelMessage_Image 图片消息清洗
func TestNormalizeChannelMessage_Image(t *testing.T) {
	engine := NewSalesEngine(nil, nil, nil, nil, nil, nil, nil, nil)
	m := &ChannelMessage{
		Channel:  "wecom",
		Content:  "我的图片",
		MsgType:  "image",
		MediaURL: "https://example.com/img.jpg",
	}
	content, _, _ := engine.normalizeChannelMessage(context.Background(), m)
	if !strings.Contains(content, "[图片]") {
		t.Errorf("expected [图片], got %s", content)
	}
}

// TestWebhook_EmptyBody 拒绝空 body
func TestWebhook_EmptyBody(t *testing.T) {
	svc := NewWebhookService(nil)
	res, _ := svc.Receive(context.Background(), &ReceiveRequest{
		Channel:   ChannelWeCom,
		AccountID: "1",
		Body:      nil,
	})
	if res.Accepted {
		t.Error("expected rejected on empty body")
	}
}

// TestWebhook_NoAccountID 拒绝无 accountID
func TestWebhook_NoAccountID(t *testing.T) {
	svc := NewWebhookService(nil)
	res, _ := svc.Receive(context.Background(), &ReceiveRequest{
		Channel:   ChannelWeCom,
		AccountID: "",
		Body:      []byte(`{}`),
	})
	if res.Accepted {
		t.Error("expected rejected on no accountID")
	}
	if !strings.Contains(res.Reason, "account_id") {
		t.Errorf("reason: %s", res.Reason)
	}
}

// TestWebhook_DuplicateEvent 幂等
func TestWebhook_DuplicateEvent(t *testing.T) {
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "true")
	t.Setenv("ALLOW_INSECURE_TELEGRAM_WEBHOOK", "true")
	db := setupTestDB(t)
	svc := NewWebhookService(db)
	body := []byte(`{"FromUserName":"u1","MsgType":"text","Content":"hi","MsgId":"m1"}`)
	req := &ReceiveRequest{
		Channel:   ChannelDouyin,
		AccountID: "1",
		Body:      body,
	}
	r1, _ := svc.Receive(context.Background(), req)
	if !r1.Accepted {
		t.Fatalf("first: %+v", r1)
	}
	r2, _ := svc.Receive(context.Background(), req)
	if !r2.Duplicate {
		t.Error("expected duplicate=true on second call")
	}
}

// TestWebhook_MultiChannelConcurrent 多渠道并发
func TestWebhook_MultiChannelConcurrent(t *testing.T) {
	svc := NewWebhookService(nil)
	svc.accountRepo = nil
	var wg sync.WaitGroup
	channels := []WebhookChannel{ChannelWeCom, ChannelWhatsapp, ChannelTelegram, ChannelFeishu}
	for _, ch := range channels {
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func(c WebhookChannel, idx int) {
				defer wg.Done()
				body := []byte(fmt.Sprintf(`{"MsgType":"text","Content":"hi_%d_%d"}`, idx, idx))
				_, _ = svc.Receive(context.Background(), &ReceiveRequest{
					Channel:   c,
					AccountID: "1",
					Body:      body,
				})
			}(ch, i)
		}
	}
	wg.Wait()
}

// TestParsePayload_Common 解析通用 payload
func TestParsePayload_Common(t *testing.T) {
	svc := NewWebhookService(nil)
	body := []byte(`{"FromUserName":"u1","MsgType":"text","Content":"hi","MsgId":"m1"}`)
	p, err := svc.ParsePayload(context.Background(), ChannelWeCom, body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.EventType != "text" {
		t.Errorf("event: %s", p.EventType)
	}
	if p.Sender != "u1" {
		t.Errorf("sender: %s", p.Sender)
	}
	if p.Content != "hi" {
		t.Errorf("content: %s", p.Content)
	}
}

// TestParsePayload_Invalid 无效 JSON
func TestParsePayload_Invalid(t *testing.T) {
	svc := NewWebhookService(nil)
	_, err := svc.ParsePayload(context.Background(), ChannelWeCom, []byte(`not json`))
	if err == nil {
		t.Error("expected err on invalid JSON")
	}
}

// TestDispatchToUnified_Integration 验证入库 unified_messages
//
// 渠道归属判定已改版：有入站适配器的渠道正常消息直接落 message_hub，不再写 unified_messages
// （审计 D-03 把"哪条路由真正服务这个渠道"变成显式能力表）。unified_messages 现在
// 承接的是「适配器报错、消息仍要留痕」这一条路径，故用一条 telegram 官方形状都对不上的
// 报文来驱动：ParseUpdate 必失败，handleJob 才继续往下写。
func TestDispatchToUnified_Integration(t *testing.T) {
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "true")
	t.Setenv("ALLOW_INSECURE_TELEGRAM_WEBHOOK", "true")
	db := setupTestDB(t)
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())
	body := []byte(`{"update_id":"not-a-number","from":"u_d03_unified","content":"hi"}`)
	r, _ := svc.Receive(context.Background(), &ReceiveRequest{
		Channel:   ChannelTelegram,
		AccountID: "1",
		Body:      body,
	})
	if !r.Accepted {
		t.Fatalf("receive: %+v", r)
	}
	time.Sleep(500 * time.Millisecond)
	var um model.UnifiedMessage
	// 用本用例独有的 sender：共用库里若命中别人的行，这条断言就成了假绿。
	if err := db.Where("sender_id = ?", "u_d03_unified").First(&um).Error; err != nil {
		t.Errorf("unified message not found: %v", err)
	}
	if um.Content != "hi" {
		t.Errorf("content: %s", um.Content)
	}
}

// TestWebhookStats 统计
func TestWebhookStats(t *testing.T) {
	svc := NewWebhookService(nil)
	if svc.QueueLen(context.Background()) < 0 {
		t.Error("queue len error")
	}
}

// TestWeCom_VerifyURL_HTTP HTTP 端到端测试
func TestWeCom_VerifyURL_HTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	token := "HttpT"
	aesKey := "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789ABCDEFG"

	r := gin.New()
	r.GET("/api/webhook/wecom/:account_id", func(c *gin.Context) {
		accID := c.Param("account_id")
		sig := c.Query("msg_signature")
		ts := c.Query("timestamp")
		nonce := c.Query("nonce")
		echo := c.Query("echostr")
		_ = accID
		plain, err := VerifyURL(token, aesKey, sig, ts, nonce, echo)
		if err != nil {
			c.String(http.StatusUnauthorized, err.Error())
			return
		}
		c.String(http.StatusOK, strings.TrimSpace(plain))
	})

	echostr := "echo_http_001"
	encEcho := wecomOfficialEncrypt(t, aesKey, echostr, "wx")
	ts := "1700000001"
	nonce := "n001"
	sig := computeWeComSignatureURL(token, ts, nonce, echostr)

	url := fmt.Sprintf("/api/webhook/wecom/1?msg_signature=%s&timestamp=%s&nonce=%s&echostr=%s",
		sig, ts, nonce, url.QueryEscape(encEcho))
	req := httptest.NewRequest("GET", url, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), echostr) {
		t.Errorf("expected body contain %q, got %q", echostr, rec.Body.String())
	}
}

// TestWeCom_VerifyURL_BadSig HTTP 验签失败
func TestWeCom_VerifyURL_BadSig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/webhook/wecom/:account_id", func(c *gin.Context) {
		sig := c.Query("msg_signature")
		ts := c.Query("timestamp")
		nonce := c.Query("nonce")
		echo := c.Query("echostr")
		_, err := VerifyURL("T", "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789ABCDEFG", sig, ts, nonce, echo)
		if err != nil {
			c.String(http.StatusUnauthorized, err.Error())
			return
		}
		c.String(http.StatusOK, "ok")
	})

	url := "/api/webhook/wecom/1?msg_signature=badsig&timestamp=1700000000&nonce=n&echostr=echo"
	req := httptest.NewRequest("GET", url, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

// TestWeChat_Verify 微信公众号验签
func TestWeChat_Verify(t *testing.T) {
	body := []byte(`{"Content":"hi"}`)
	ts := "1700000123"
	nonce := "n001"
	parts := []string{"WToken", ts, nonce}
	sort.Strings(parts)
	h := sha1.Sum([]byte(strings.Join(parts, "")))
	sig := hex.EncodeToString(h[:])

	ok := verifyWechat("WToken", body, map[string]string{
		"X-Wechat-Timestamp": ts,
		"X-Wechat-Nonce":     nonce,
		"X-Wechat-Signature": sig,
	})
	if !ok {
		t.Error("expected ok")
	}

	ok = verifyWechat("WToken", body, map[string]string{
		"X-Wechat-Timestamp": ts,
		"X-Wechat-Nonce":     nonce,
		"X-Wechat-Signature": "bad",
	})
	if ok {
		t.Error("expected fail on bad sig")
	}
}

// TestWeChat_Verify_Missing 缺参数
func TestWeChat_Verify_Missing(t *testing.T) {
	ok := verifyWechat("T", []byte(`{}`), map[string]string{})
	if ok {
		t.Error("expected fail on missing params")
	}
}

func computeWeComSignature4(token, ts, nonce, fourth string) string {
	parts := []string{token, ts, nonce, fourth}
	sortStrings(parts)
	return sha1Hex([]byte(strings.Join(parts, "")))
}
