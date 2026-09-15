package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/pkg/httpclient"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
)

func setupChannelFullDB(t *testing.T) *gorm.DB {
	return testutil.NewTestDB(t,
		&model.WeComAccount{},
		&model.FeishuAccount{},
		&model.FeishuCustomer{},
		&model.FeishuMessage{},
		&model.TelegramAccount{},
		&model.WhatsAppCloudAccount{},
		&model.MessageHub{},
		&model.InboxConversation{},
		&model.UnifiedMessage{},
		&model.WebhookEvent{},
		&model.IntegrationAccount{},
	)
}

func TestE2E_Feishu_AccountCreateAndList(t *testing.T) {
	db := setupChannelFullDB(t)
	svc := NewFeishuService(db)

	acc := &model.FeishuAccount{
		AccountName:       "飞书主账号",
		AppID:             "cli_xxxx",
		AppSecret:         "secret_abc",
		VerificationToken: "vtoken",
		EncryptKey:        "ekey1234567890123456789012345678",
		WebhookEnabled:    true,
		AIAgentEnabled:    true,
	}
	out, err := svc.CreateAccount(context.Background(), acc)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if out.ID == 0 {
		t.Error("expected ID > 0")
	}

	all, err := svc.ListAccounts(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("expected 1, got %d", len(all))
	}

	appID, vToken, eKey, err := svc.GetSecretsByAccountID(context.Background(), "1")
	if err != nil {
		t.Fatalf("secrets: %v", err)
	}
	if appID != "cli_xxxx" || vToken != "vtoken" || eKey == "" {
		t.Errorf("secrets mismatch: appID=%s vToken=%s eKey=%s", appID, vToken, eKey)
	}
}

func TestE2E_Feishu_IngestMessage(t *testing.T) {
	db := setupChannelFullDB(t)
	svc := NewFeishuIntegrationService(db)

	acc := &model.FeishuAccount{
		AccountName: "测试飞书账号",
		AppID:       "cli_yyyy",
		AppSecret:   "secret_def",
		Status:      1,
	}
	acc.ID = 100
	if err := db.Create(acc).Error; err != nil {
		t.Fatalf("create acc: %v", err)
	}

	hub, conv, err := svc.IngestMessage(context.Background(), &FeishuIngestRequest{
		AccountID: 100,
		OpenID:    "ou_openid_001",
		Name:      "张三",
		MsgType:   "text",
		Content:   "你好，我想了解一下产品",
		MsgID:     "om_msg_001",
		ChatID:    "oc_chat_001",
		ChatType:  "p2p",
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if hub == nil {
		t.Fatal("expected hub")
	}
	if hub.Platform != "feishu" {
		t.Errorf("expected feishu, got %s", hub.Platform)
	}
	if hub.SenderID != "ou_openid_001" {
		t.Errorf("expected ou_openid_001, got %s", hub.SenderID)
	}
	if hub.Content != "你好，我想了解一下产品" {
		t.Errorf("content mismatch: %s", hub.Content)
	}
	if conv == nil {
		t.Error("expected inbox conversation")
	}

	var cnt int64
	db.Model(&model.InboxConversation{}).Where("platform = ?", "feishu").Count(&cnt)
	if cnt != 1 {
		t.Errorf("expected 1 inbox, got %d", cnt)
	}
}

func TestE2E_Feishu_IngestMessage_GroupChat(t *testing.T) {
	db := setupChannelFullDB(t)
	svc := NewFeishuIntegrationService(db)
	acc := &model.FeishuAccount{AccountName: "G", AppID: "a", AppSecret: "b", Status: 1}
	acc.ID = 200
	db.Create(acc)

	hub, _, err := svc.IngestMessage(context.Background(), &FeishuIngestRequest{
		AccountID: 200, OpenID: "ou_user1", MsgType: "text",
		Content: "群消息测试", MsgID: "g_001", ChatID: "oc_g1", ChatType: "group",
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if !hub.IsGroup {
		t.Error("expected IsGroup=true")
	}
	if hub.GroupID != "oc_g1" {
		t.Errorf("expected oc_g1, got %s", hub.GroupID)
	}
}

func TestE2E_Feishu_DecryptEvent(t *testing.T) {
	plain := `{"event":{"message":{"message_id":"m1","chat_id":"c1","chat_type":"p2p","message_type":"text","content":"{\"text\":\"hi\"}"}}}`
	ek := "12345678901234567890123456789012"
	enc := encryptFeishuForTest(t, ek, plain)
	out, err := DecryptFeishuEvent(ek, enc)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !strings.Contains(string(out), "hi") {
		t.Errorf("decrypt mismatch: %s", string(out))
	}
}

func encryptFeishuForTest(t *testing.T, key, plaintext string) string {

	var k []byte
	if len(key) >= 32 {
		k = []byte(key)[:32]
	} else {
		k = make([]byte, 32)
		copy(k, key)
	}
	pt := []byte(plaintext)
	padLen := 16 - (len(pt) % 16)
	if padLen == 0 {
		padLen = 16
	}
	pad := make([]byte, padLen)
	for i := range pad {
		pad[i] = byte(padLen)
	}
	pt = append(pt, pad...)

	iv := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		t.Fatalf("rand iv: %v", err)
	}

	block, err := aes.NewCipher(k)
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	cipherText := make([]byte, len(pt))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(cipherText, pt)

	combined := append(iv, cipherText...)
	return base64.StdEncoding.EncodeToString(combined)
}

func TestE2E_Telegram_AccountCreateAndGet(t *testing.T) {
	db := setupChannelFullDB(t)
	svc := NewTelegramService(db)

	acc := &model.TelegramAccount{
		AccountName:    "测试TG",
		BotToken:       "123:abcdef",
		WebhookURL:     "https://example.com/hook",
		WebhookSecret:  "sec1",
		WebhookEnabled: true,
		AIAgentEnabled: true,
	}
	out, err := svc.CreateAccount(context.Background(), acc)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if out.ID == 0 {
		t.Error("expected ID > 0")
	}
	got, err := svc.GetAccount(context.Background(), out.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.BotToken != "123:abcdef" {
		t.Errorf("token mismatch: %s", got.BotToken)
	}

	bt, ws, err := svc.GetSecretsByAccountID(context.Background(), "1")
	if err != nil {
		t.Fatalf("secrets: %v", err)
	}
	if bt != "123:abcdef" || ws != "sec1" {
		t.Errorf("secrets mismatch: %s/%s", bt, ws)
	}
}

func TestE2E_Telegram_IngestMessage(t *testing.T) {
	db := setupChannelFullDB(t)
	svc := NewTelegramIntegrationService(db)
	acc := &model.TelegramAccount{AccountName: "TG", BotToken: "tok", Status: 1}
	acc.ID = 50
	db.Create(acc)

	hub, conv, err := svc.IngestMessage(context.Background(), &TelegramIngestRequest{
		AccountID: 50, ChatID: 12345, FromID: 67890,
		FromName: "Alice", MsgType: "text", Content: "hello tg",
		MsgID: 999, IsGroup: false,
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if hub.Platform != "telegram" {
		t.Errorf("expected telegram, got %s", hub.Platform)
	}
	if hub.SenderID != "67890" {
		t.Errorf("expected 67890, got %s", hub.SenderID)
	}
	if hub.ConversationID != "12345" {
		t.Errorf("expected conv 12345, got %s", hub.ConversationID)
	}
	if conv == nil {
		t.Error("expected inbox conv")
	}
}

func TestE2E_Telegram_IngestMessage_Group(t *testing.T) {
	db := setupChannelFullDB(t)
	svc := NewTelegramIntegrationService(db)
	acc := &model.TelegramAccount{AccountName: "TG2", BotToken: "tok2", Status: 1}
	acc.ID = 51
	db.Create(acc)

	hub, _, err := svc.IngestMessage(context.Background(), &TelegramIngestRequest{
		AccountID: 51, ChatID: -1001, FromID: 67890,
		FromName: "Bob", MsgType: "text", Content: "group msg",
		MsgID: 1000, IsGroup: true, GroupTitle: "TestGroup",
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if !hub.IsGroup {
		t.Error("expected IsGroup=true")
	}
}

func TestE2E_WhatsApp_AccountCreate(t *testing.T) {
	db := setupChannelFullDB(t)
	svc := NewWhatsAppCloudService(db)

	acc := &model.WhatsAppCloudAccount{
		AccountName:        "WA主",
		PhoneNumberID:      "1234567890",
		WhatsAppBusinessID: "WABA001",
		AccessToken:        "EAAxxxx",
		VerifyToken:        "vtoken",
		AppSecret:          "appsec",
		WebhookEnabled:     true,
		AIAgentEnabled:     true,
	}
	out, err := svc.CreateAccount(context.Background(), acc)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if out.ID == 0 {
		t.Error("expected ID")
	}
	got, _ := svc.GetAccount(context.Background(), out.ID)
	if got.PhoneNumberID != "1234567890" {
		t.Errorf("phone mismatch: %s", got.PhoneNumberID)
	}
	got2, _ := svc.GetAccountByPhone(context.Background(), "1234567890")
	if got2 == nil || got2.ID != out.ID {
		t.Errorf("getByPhone mismatch: %+v", got2)
	}
}

func TestE2E_WhatsApp_IngestMessage(t *testing.T) {
	db := setupChannelFullDB(t)
	svc := NewWhatsAppCloudIntegrationService(db)
	acc := &model.WhatsAppCloudAccount{
		AccountName: "WA", PhoneNumberID: "111", WhatsAppBusinessID: "W1",
		AccessToken: "tk", Status: 1,
	}
	acc.ID = 10
	db.Create(acc)

	hub, conv, err := svc.IngestMessage(context.Background(), &WhatsAppIngestRequest{
		AccountID:    10,
		PhoneFrom:    "+8613800000001",
		CustomerName: "客户A",
		MsgType:      "text",
		Content:      "Hello WA",
		MsgID:        "wamid.001",
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if hub.Platform != "whatsapp" {
		t.Errorf("expected whatsapp, got %s", hub.Platform)
	}
	if hub.SenderID != "+8613800000001" {
		t.Errorf("sender mismatch: %s", hub.SenderID)
	}
	if conv == nil {
		t.Error("expected inbox conv")
	}
}

func TestE2E_WhatsApp_VerifySignature(t *testing.T) {
	body := []byte(`{"entry":[]}`)
	appSecret := "testsecret"
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))

	if !VerifyWhatsAppSignature(appSecret, body, expected) {
		t.Error("expected verify ok with hex sig")
	}
	if !VerifyWhatsAppSignature(appSecret, body, "sha256="+expected) {
		t.Error("expected verify ok with sha256= prefix")
	}
	if VerifyWhatsAppSignature(appSecret, body, "deadbeef") {
		t.Error("expected fail with wrong sig")
	}
	if !VerifyWhatsAppSignature("", body, "anything") {
		t.Error("expected pass with empty secret (dev mode)")
	}
}

func TestE2E_MessageHub_Whitelist_FourChannels(t *testing.T) {
	for _, p := range []string{"wecom", "whatsapp", "telegram", "feishu"} {
		if !messageHubPlatforms[p] {
			t.Errorf("expected platform %s in whitelist", p)
		}
	}
}

func TestE2E_WebhookService_DispatchWhatsApp(t *testing.T) {
	db := setupChannelFullDB(t)
	acc := &model.WhatsAppCloudAccount{
		AccountName: "WA", PhoneNumberID: "1", WhatsAppBusinessID: "W",
		AccessToken: "tk", WebhookEnabled: true, AIAgentEnabled: false, Status: 1,
	}
	db.Create(acc)
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	body := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"W","changes":[{"value":{"messages":[{"from":"+8613800000001","id":"w1","timestamp":"1700000000","type":"text","text":{"body":"hi"}}],"contacts":[{"profile":{"name":"Alice"},"wa_id":"+8613800000001"}]},"field":"messages"}]}]}`)

	hub, err := svc.dispatchWhatsApp(context.Background(), "1", &ParsedPayload{EventID: "w1"}, body)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if hub == nil {
		t.Fatal("expected hub msg")
	}
	if hub.Content != "hi" {
		t.Errorf("content mismatch: %s", hub.Content)
	}
	if hub.SenderID != "+8613800000001" {
		t.Errorf("sender mismatch: %s", hub.SenderID)
	}
}

func TestE2E_WebhookService_DispatchTelegram(t *testing.T) {
	db := setupChannelFullDB(t)
	acc := &model.TelegramAccount{
		AccountName: "TG", BotToken: "tok", WebhookEnabled: true, AIAgentEnabled: false, Status: 1,
	}
	db.Create(acc)
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	body := []byte(`{"update_id":1,"message":{"message_id":100,"from":{"id":67890,"first_name":"Bob"},"chat":{"id":12345,"type":"private"},"date":1700000000,"text":"hello"}}`)

	hub, _, err := svc.dispatchTelegram(context.Background(), "1", &ParsedPayload{EventID: "tg1"}, body)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if hub == nil {
		t.Fatal("expected hub")
	}
	if hub.Content != "hello" {
		t.Errorf("content mismatch: %s", hub.Content)
	}
	if hub.Platform != "telegram" {
		t.Errorf("expected telegram, got %s", hub.Platform)
	}
}

func TestE2E_WebhookService_DispatchFeishu(t *testing.T) {
	db := setupChannelFullDB(t)
	acc := &model.FeishuAccount{
		AccountName: "FS", AppID: "a", AppSecret: "b",
		WebhookEnabled: true, AIAgentEnabled: false, Status: 1,
	}
	db.Create(acc)
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	body := []byte(`{"schema":"2.0","header":{"event_type":"im.message.receive_v1","app_id":"a","event_id":"e1","token":"v"},"event":{"sender":{"sender_id":{"open_id":"ou_001"}},"message":{"message_id":"om_1","chat_id":"oc_1","chat_type":"p2p","message_type":"text","content":"{\"text\":\"hi\"}"}}}`)

	hub, err := svc.dispatchFeishu(context.Background(), "1", &ParsedPayload{EventID: "e1"}, body)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if hub == nil {
		t.Fatal("expected hub")
	}
	if hub.Content != "hi" {
		t.Errorf("content mismatch: %s", hub.Content)
	}
	if hub.Platform != "feishu" {
		t.Errorf("expected feishu, got %s", hub.Platform)
	}
}

func TestE2E_WebhookService_DispatchFeishu_Challenge(t *testing.T) {
	db := setupChannelFullDB(t)
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	body := []byte(`{"challenge":"abc123","type":"url_verification"}`)
	hub, err := svc.dispatchFeishu(context.Background(), "1", &ParsedPayload{EventID: "ch1"}, body)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if hub != nil {
		t.Errorf("expected nil hub for challenge, got %+v", hub)
	}

	var cnt int64
	db.Model(&model.MessageHub{}).Where("platform = ?", "feishu").Count(&cnt)
	if cnt != 0 {
		t.Errorf("expected 0 hub, got %d", cnt)
	}
}

func TestE2E_WebhookService_ShouldTriggerAI_FourChannels(t *testing.T) {
	db := setupChannelFullDB(t)

	db.Create(&model.WeComAccount{
		CorpID: "w1", CorpSecret: "s", AgentID: 1,
		CallbackToken: "t", EncodingAESKey: "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789ABCDEFG",
		WebhookEnabled: true, AIAgentEnabled: true, Status: 1,
	})
	db.Create(&model.WhatsAppCloudAccount{
		AccountName: "WA", PhoneNumberID: "1", WhatsAppBusinessID: "W",
		AccessToken: "tk", WebhookEnabled: true, AIAgentEnabled: true, Status: 1,
	})
	db.Create(&model.TelegramAccount{
		AccountName: "TG", BotToken: "t",
		WebhookEnabled: true, AIAgentEnabled: true, Status: 1,
	})
	db.Create(&model.FeishuAccount{
		AccountName: "FS", AppID: "a", AppSecret: "b",
		WebhookEnabled: true, AIAgentEnabled: true, Status: 1,
	})

	svc := NewWebhookService(db)
	svc.SetSalesEngine(context.Background(), &SalesEngine{})
	defer svc.Stop(context.Background())

	cases := []struct {
		ch   WebhookChannel
		acc  string
		want bool
	}{
		{ChannelWeCom, "1", true},
		{ChannelWhatsapp, "1", true},
		{ChannelTelegram, "1", true},
		{ChannelFeishu, "1", true},
		{ChannelWeCom, "999", false},
		// 抖音等桥接渠道**没有**账号级 AI 开关（见 channelHasAIAgentSwitch），
		// 按设计一律放行（shouldTriggerAI 的 default 分支）。
		//
		// 历史沿革（勿回退）：原断言写 want=false，是 2026-07-21 初始导入时的旧行为；
		// 生产在 092f8cf1（2026-09-08「无账号级 AI 开关的渠道不在此拦截，
		// 避免误杀既有 AI 链路」）中刻意改为放行并新增 channelHasAIAgentSwitch，
		// 用例未同步才长期失败。
		{ChannelDouyin, "1", true},
		// 账号 ID 非法（不可解析 / 为 0）在任何渠道都应拦截
		{ChannelDouyin, "abc", false},
		{ChannelDouyin, "0", false},
	}
	for _, c := range cases {
		got := svc.shouldTriggerAI(context.Background(), c.ch, c.acc)
		if got != c.want {
			t.Errorf("shouldTriggerAI(%s, %s) = %v, want %v", c.ch, c.acc, got, c.want)
		}
	}
}

func init() {
	_ = time.Now
}

type capturedHTTP struct {
	URL    string
	Method string
	Body   string
	Host   string
}

type outboundCaptureTransport struct {
	target string
}

func (t *outboundCaptureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	target, err := url.Parse(t.target)
	if err != nil {
		return nil, err
	}
	out := req.Clone(req.Context())
	out.URL = &url.URL{
		Scheme:   target.Scheme,
		Host:     target.Host,
		Path:     req.URL.Path,
		RawQuery: req.URL.RawQuery,
	}
	out.Host = target.Host
	out.Header.Set("X-Orig-URL", req.URL.String())
	out.Header.Set("X-Orig-Host", req.URL.Host)
	return http.DefaultTransport.RoundTrip(out)
}

func withCaptureHTTPClient(t *testing.T) (*[]capturedHTTP, *int64, func()) {
	t.Helper()
	var (
		mu      sync.Mutex
		calls   []capturedHTTP
		counter int64
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		atomic.AddInt64(&counter, 1)
		mu.Lock()
		calls = append(calls, capturedHTTP{
			URL:    r.Header.Get("X-Orig-URL"),
			Method: r.Method,
			Body:   string(body),
			Host:   r.Header.Get("X-Orig-Host"),
		})
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		origHost := r.Header.Get("X-Orig-Host")
		switch {
		case strings.Contains(r.URL.Path, "tenant_access_token"):
			w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"t-mock-token","expire":7200}`))
		case strings.Contains(origHost, "graph.facebook.com"):
			w.Write([]byte(`{"messaging_product":"whatsapp","contacts":[{"input":"+86","wa_id":"+86"}],"messages":[{"id":"wamid.mock"}]}`))
		case strings.Contains(origHost, "open.feishu.cn"):
			w.Write([]byte(`{"code":0,"msg":"ok","data":{"message_id":"om_mock"}}`))
		default:
			w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
		}
	}))

	oldTransport := httpclient.Client.Transport
	httpclient.Client.Transport = &outboundCaptureTransport{target: srv.URL}
	restore := func() {
		httpclient.Client.Transport = oldTransport
		srv.Close()
	}
	return &calls, &counter, restore
}

func TestE2E_SendOutbound_Feishu_RealPath(t *testing.T) {
	calls, counter, restore := withCaptureHTTPClient(t)
	defer restore()
	// -count=N 隔离：固定 EventID 的出站认领（reply guard）需测试前后释放
	releaseReplyClaimForTest(t, "evt_1")
	db := setupChannelFullDB(t)
	now := time.Now()
	exp := now.Add(time.Hour)
	acc := &model.FeishuAccount{
		AccountName:       "FS-OB",
		AppID:             "cli_ob",
		AppSecret:         "sec",
		VerificationToken: "vt",
		EncryptKey:        "",
		AccessToken:       "pre-cached-token",
		TokenExpires:      &exp,
		WebhookEnabled:    true,
		AIAgentEnabled:    true,
		Status:            1,
	}
	db.Create(acc)

	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	hub := &model.MessageHub{
		Platform:       "feishu",
		AccountID:      fmt.Sprintf("%d", acc.ID),
		MsgID:          "om_in_1",
		Direction:      "inbound",
		MsgType:        "text",
		SenderID:       "ou_user_1",
		SenderName:     "客户甲",
		Content:        "原始消息",
		ConversationID: "oc_chat_1",
	}
	db.Create(hub)

	p := &ParsedPayload{
		EventID: "evt_1",
		Sender:  "ou_user_1",
		Content: "原始消息",
		ChatID:  "oc_chat_1",
	}
	svc.sendOutbound(context.Background(), ChannelFeishu, fmt.Sprintf("%d", acc.ID), p, "智能体回复 Feishu", hub, nil)

	if *counter < 1 {
		t.Fatalf("expected at least 1 outbound http call, got %d", *counter)
	}

	var outCnt int64
	db.Model(&model.MessageHub{}).Where("platform = ? AND direction = ?", "feishu", "outbound").Count(&outCnt)
	if outCnt < 1 {
		t.Errorf("expected feishu outbound in message_hub, got %d", outCnt)
	}

	var fsOut int64
	db.Model(&model.FeishuMessage{}).Where("account_id = ?", acc.ID).Count(&fsOut)
	if fsOut < 1 {
		t.Errorf("expected feishu_messages outbound record, got %d", fsOut)
	}
	sent := false
	for _, c := range *calls {
		if strings.Contains(c.URL, "im/v1/messages") {
			sent = true
			if !strings.Contains(c.Body, "智能体回复 Feishu") {
				t.Errorf("outbound feishu body missing content: %s", c.Body)
			}
		}
	}
	if !sent {
		t.Error("expected at least one im/v1/messages outbound http call")
	}
}

func TestE2E_SendOutbound_Telegram_RealPath(t *testing.T) {
	calls, counter, restore := withCaptureHTTPClient(t)
	defer restore()
	// -count=N 隔离：释放上一轮 "tg1" 的出站认领
	releaseReplyClaimForTest(t, "tg1")
	db := setupChannelFullDB(t)
	acc := &model.TelegramAccount{
		AccountName:    "TG-OB",
		BotToken:       "123:abc",
		WebhookEnabled: true,
		AIAgentEnabled: true,
		Status:         1,
	}
	db.Create(acc)
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	hub := &model.MessageHub{
		Platform:       "telegram",
		AccountID:      fmt.Sprintf("%d", acc.ID),
		MsgID:          "tg_1",
		Direction:      "inbound",
		MsgType:        "text",
		SenderID:       "67890",
		Content:        "原始 tg 消息",
		ConversationID: "12345",
	}
	db.Create(hub)

	p := &ParsedPayload{EventID: "tg1", Sender: "67890", Content: "原始", ChatID: "12345"}
	svc.sendOutbound(context.Background(), ChannelTelegram, fmt.Sprintf("%d", acc.ID), p, "智能体回复 TG", hub, nil)

	if *counter < 1 {
		t.Fatalf("expected at least 1 outbound, got %d", *counter)
	}
	sent := false
	for _, c := range *calls {
		if strings.Contains(c.URL, "api.telegram.org") && strings.Contains(c.URL, "sendMessage") {
			sent = true
			if !strings.Contains(c.Body, "智能体回复 TG") {
				t.Errorf("outbound tg body missing content: %s", c.Body)
			}
			if !strings.Contains(c.Body, "12345") {
				t.Errorf("outbound tg body missing chat_id: %s", c.Body)
			}
		}
	}
	if !sent {
		t.Error("expected one telegram sendMessage call")
	}
	var outCnt int64
	db.Model(&model.MessageHub{}).Where("platform = ? AND direction = ?", "telegram", "outbound").Count(&outCnt)
	if outCnt < 1 {
		t.Errorf("expected telegram outbound in hub, got %d", outCnt)
	}
}

func TestE2E_SendOutbound_WhatsApp_RealPath(t *testing.T) {
	calls, counter, restore := withCaptureHTTPClient(t)
	defer restore()
	// -count=N 隔离：释放上一轮 "w1" 的出站认领
	releaseReplyClaimForTest(t, "w1")
	db := setupChannelFullDB(t)
	acc := &model.WhatsAppCloudAccount{
		AccountName:        "WA-OB",
		PhoneNumberID:      "1234567890",
		WhatsAppBusinessID: "WABA1",
		AccessToken:        "EAAxxxx",
		WebhookEnabled:     true,
		AIAgentEnabled:     true,
		Status:             1,
	}
	db.Create(acc)
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	hub := &model.MessageHub{
		Platform:       "whatsapp",
		AccountID:      fmt.Sprintf("%d", acc.ID),
		MsgID:          "wamid.in",
		Direction:      "inbound",
		MsgType:        "text",
		SenderID:       "+8613800000001",
		Content:        "原始 wa 消息",
		ConversationID: "+8613800000001",
	}
	db.Create(hub)

	p := &ParsedPayload{EventID: "w1", Sender: "+8613800000001", Content: "原始", ChatID: "+8613800000001"}
	svc.sendOutbound(context.Background(), ChannelWhatsapp, fmt.Sprintf("%d", acc.ID), p, "智能体回复 WA", hub, nil)

	if *counter < 1 {
		t.Fatalf("expected at least 1 outbound, got %d", *counter)
	}
	sent := false
	for _, c := range *calls {
		if strings.Contains(c.URL, "graph.facebook.com") && strings.Contains(c.URL, "/messages") {
			sent = true
			if !strings.Contains(c.Body, "智能体回复 WA") {
				t.Errorf("outbound wa body missing content: %s", c.Body)
			}
			if !strings.Contains(c.Body, "+8613800000001") {
				t.Errorf("outbound wa body missing to phone: %s", c.Body)
			}
		}
	}
	if !sent {
		t.Error("expected one WhatsApp messages call")
	}
	var outCnt int64
	db.Model(&model.MessageHub{}).Where("platform = ? AND direction = ?", "whatsapp", "outbound").Count(&outCnt)
	if outCnt < 1 {
		t.Errorf("expected whatsapp outbound in hub, got %d", outCnt)
	}
}

func TestE2E_HandleJob_FourChannels_AIDisabled(t *testing.T) {
	cases := []struct {
		name    string
		channel WebhookChannel
		setup   func(db *gorm.DB) string
		body    []byte
		verify  func(t *testing.T, db *gorm.DB, accountID string)
	}{
		{
			name:    "WeCom",
			channel: ChannelWeCom,
			setup: func(db *gorm.DB) string {
				acc := &model.WeComAccount{
					CorpID: "wx", CorpSecret: "s", AgentID: 1,
					CallbackToken: "T", EncodingAESKey: "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789ABCDEFG",
					WebhookEnabled: true, AIAgentEnabled: false, Status: 1,
				}
				db.Create(acc)
				return fmt.Sprintf("%d", acc.ID)
			},
			body: []byte(`{"ToUserName":"wx","FromUserName":"user1","CreateTime":1700000000,"MsgType":"text","Content":"hi","MsgId":"m_w_1"}`),
			verify: func(t *testing.T, db *gorm.DB, accountID string) {
				var c int64
				db.Model(&model.UnifiedMessage{}).Where("platform = ?", "wecom").Count(&c)
				if c == 0 {
					t.Error("expected wecom unified_message")
				}
			},
		},
		{
			name:    "WhatsApp",
			channel: ChannelWhatsapp,
			setup: func(db *gorm.DB) string {
				acc := &model.WhatsAppCloudAccount{
					AccountName: "WA", PhoneNumberID: "1", WhatsAppBusinessID: "W",
					AccessToken: "tk", WebhookEnabled: true, AIAgentEnabled: false, Status: 1,
				}
				db.Create(acc)
				return fmt.Sprintf("%d", acc.ID)
			},
			body: []byte(`{"object":"whatsapp_business_account","entry":[{"id":"W","changes":[{"value":{"messages":[{"from":"+8613800000001","id":"w_in_1","timestamp":"1700000000","type":"text","text":{"body":"hi wa"}}],"contacts":[{"profile":{"name":"Alice"},"wa_id":"+8613800000001"}]},"field":"messages"}]}]}`),
			verify: func(t *testing.T, db *gorm.DB, accountID string) {
				var c int64
				db.Model(&model.MessageHub{}).Where("platform = ? AND direction = ?", "whatsapp", "inbound").Count(&c)
				if c == 0 {
					t.Error("expected whatsapp inbound hub")
				}
			},
		},
		{
			name:    "Telegram",
			channel: ChannelTelegram,
			setup: func(db *gorm.DB) string {
				acc := &model.TelegramAccount{
					AccountName: "TG", BotToken: "tok", WebhookEnabled: true, AIAgentEnabled: false, Status: 1,
				}
				db.Create(acc)
				return fmt.Sprintf("%d", acc.ID)
			},
			body: []byte(`{"update_id":1,"message":{"message_id":100,"from":{"id":67890,"first_name":"Bob"},"chat":{"id":12345,"type":"private"},"date":1700000000,"text":"hi tg"}}`),
			verify: func(t *testing.T, db *gorm.DB, accountID string) {
				var c int64
				db.Model(&model.MessageHub{}).Where("platform = ? AND direction = ?", "telegram", "inbound").Count(&c)
				if c == 0 {
					t.Error("expected telegram inbound hub")
				}
			},
		},
		{
			name:    "Feishu",
			channel: ChannelFeishu,
			setup: func(db *gorm.DB) string {
				acc := &model.FeishuAccount{
					AccountName: "FS", AppID: "a", AppSecret: "b",
					WebhookEnabled: true, AIAgentEnabled: false, Status: 1,
				}
				db.Create(acc)
				return fmt.Sprintf("%d", acc.ID)
			},
			body: []byte(`{"schema":"2.0","header":{"event_type":"im.message.receive_v1","app_id":"a","event_id":"e_1","token":"v"},"event":{"sender":{"sender_id":{"open_id":"ou_user_1"}},"message":{"message_id":"om_e_1","chat_id":"oc_e_1","chat_type":"p2p","message_type":"text","content":"{\"text\":\"hi fs\"}"}}}`),
			verify: func(t *testing.T, db *gorm.DB, accountID string) {
				var c int64
				db.Model(&model.MessageHub{}).Where("platform = ? AND direction = ?", "feishu", "inbound").Count(&c)
				if c == 0 {
					t.Error("expected feishu inbound hub")
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db := setupChannelFullDB(t)
			accountID := c.setup(db)
			svc := NewWebhookService(db)
			defer svc.Stop(context.Background())

			payload, err := svc.ParsePayload(context.Background(), c.channel, c.body)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			evt := &model.WebhookEvent{
				Platform:  string(c.channel),
				EventID:   payload.EventID,
				Processed: false,
				CreatedAt: time.Now(),
			}
			db.Create(evt)
			job := &webhookJob{
				event:   evt,
				raw:     c.body,
				channel: c.channel,
				account: accountID,
				payload: payload,
			}
			svc.handleJob(context.Background(), job)
			c.verify(t, db, accountID)
		})
	}
}

func TestE2E_WebhookService_ToUnifiedMessage_4Channels(t *testing.T) {
	db := setupChannelFullDB(t)
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	cases := []struct {
		ch       WebhookChannel
		platform model.Platform
	}{
		{ChannelWeCom, "wecom"},
		{ChannelWhatsapp, "whatsapp"},
		{ChannelTelegram, "telegram"},
		{ChannelFeishu, "feishu"},
	}
	for _, c := range cases {
		p := &ParsedPayload{
			EventID: "evt_" + string(c.ch),
			Sender:  "u_" + string(c.ch),
			Content: "hello " + string(c.ch),
			ChatID:  "chat_" + string(c.ch),
		}
		um := svc.ToUnifiedMessage(context.Background(), c.ch, "1", p)
		if um.Platform != c.platform {
			t.Errorf("[%s] platform mismatch: %s", c.ch, um.Platform)
		}
		if um.Content != "hello "+string(c.ch) {
			t.Errorf("[%s] content: %s", c.ch, um.Content)
		}
		if um.Status != model.MessageStatusPending {
			t.Errorf("[%s] status: %s", c.ch, um.Status)
		}
		if um.MessageID == "" {
			t.Errorf("[%s] message_id empty", c.ch)
		}
	}
}
