package service

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

var _ = json.Marshal

// dtEncryptForTest 已删除：它按「直接加密 JSON」构造夹具，与仓库实现互相印证造成假绿，
// 真实钉钉事件订阅回调（random16+len4+msg+receiveId）从未被这条路径覆盖。
// 官方布局的加密夹具见 dingtalk_official_contract_test.go 的 dtOfficialEncrypt。

func feishuEncryptForTest(encKey, plain string) (string, error) {
	key := []byte(encKey)
	if len(key) > 32 {
		key = key[:32]
	} else {
		pad := make([]byte, 32-len(key))
		key = append(key, pad...)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	pt := []byte(plain)
	pad := aes.BlockSize - len(pt)%aes.BlockSize
	pt = append(pt, bytes.Repeat([]byte{byte(pad)}, pad)...)
	iv := make([]byte, aes.BlockSize)
	ct := make([]byte, aes.BlockSize+len(pt))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ct[aes.BlockSize:], pt)
	copy(ct[:aes.BlockSize], iv)
	return base64.StdEncoding.EncodeToString(ct), nil
}

func allowAnyDingtalkHostForTest(t *testing.T) {
	t.Helper()
	orig := dingtalkWebhookHostAllowed
	dingtalkWebhookHostAllowed = func(u *url.URL) bool { return u != nil }
	t.Cleanup(func() { dingtalkWebhookHostAllowed = orig })
}

func TestSendOutbound_DingTalk_UsesSessionWebhook(t *testing.T) {
	allowAnyDingtalkHostForTest(t)
	// -count=N 隔离：固定 EventID 会被 sendOutbound 的 claim-before-confirm
	// 幂等守卫在上一轮认领并保留，第二轮需先释放。
	releaseReplyClaimForTest(t, "evt-dt-a")
	var mu sync.Mutex
	var captured []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		mu.Lock()
		captured = append(captured, m)
		mu.Unlock()
		w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer srv.Close()

	svc := &WebhookService{}
	expiredAt := time.Now().Add(time.Hour).Unix()
	hub := &model.MessageHub{
		Platform:       "dingtalk",
		ConversationID: "cid-dt-1",
		Direction:      "inbound",
		SentAt:         time.Now(),
		Extra: model.JSONMap{
			"session_webhook":            srv.URL,
			"session_webhook_expired_at": expiredAt,
		},
	}
	p := &ParsedPayload{EventID: "evt-dt-a", Sender: "staff-1", Content: "在吗"}
	svc.sendOutbound(context.Background(), ChannelDingTalk, "7", p, "您好，我是 AI 助手", hub, nil)

	mu.Lock()
	defer mu.Unlock()
	if len(captured) != 1 {
		t.Fatalf("期望经 sessionWebhook 发送 1 条回复，实际 %d", len(captured))
	}
	if captured[0]["msgtype"] != "text" {
		t.Errorf("msgtype expected text, got %v", captured[0]["msgtype"])
	}
	textMap, _ := captured[0]["text"].(map[string]any)
	if textMap == nil || textMap["content"] != "您好，我是 AI 助手" {
		t.Errorf("text.content 不符: %v", captured[0]["text"])
	}
}

// TestSendOutbound_DingTalk_MsExpiryTimestampSends 官方 sessionWebhookExpiredTime 为毫秒，
// 未过期的毫秒时间戳必须正常放行（防秒/毫秒口径混淆误杀）。
func TestSendOutbound_DingTalk_MsExpiryTimestampSends(t *testing.T) {
	allowAnyDingtalkHostForTest(t)
	releaseReplyClaimForTest(t, "e-ms")
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		w.Write([]byte(`{"errcode":0}`))
	}))
	defer srv.Close()

	svc := &WebhookService{}
	hub := &model.MessageHub{Platform: "dingtalk", ConversationID: "c-ms", SentAt: time.Now(),
		Extra: model.JSONMap{
			"session_webhook":            srv.URL,
			"session_webhook_expired_at": time.Now().Add(time.Hour).UnixMilli(),
		}}
	svc.sendOutbound(context.Background(), ChannelDingTalk, "7",
		&ParsedPayload{EventID: "e-ms", Sender: "s", Content: "hi"}, "reply", hub, nil)

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("未过期的毫秒时间戳应放行发送，实际 %d 次", calls)
	}
}

func TestSendOutbound_DingTalk_ForeignHostRejected(t *testing.T) {
	releaseReplyClaimForTest(t, "e-ssrf")
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
	}))
	defer srv.Close()

	svc := &WebhookService{}
	hub := &model.MessageHub{Platform: "dingtalk", ConversationID: "c-ssrf",
		Extra: model.JSONMap{"session_webhook": srv.URL}}
	svc.sendOutbound(context.Background(), ChannelDingTalk, "7",
		&ParsedPayload{EventID: "e-ssrf", Sender: "s", Content: "hi"}, "reply", hub, nil)

	mu.Lock()
	defer mu.Unlock()
	if calls != 0 {
		t.Fatal("非钉钉官方域名的 sessionWebhook 必须拒绝发送（SSRF 防线）")
	}
}

func TestSendOutbound_DingTalk_MissingOrExpiredWebhook_NoCall(t *testing.T) {
	allowAnyDingtalkHostForTest(t)
	releaseReplyClaimForTest(t, "e1", "e2")
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		w.Write([]byte(`{"errcode":0}`))
	}))
	defer srv.Close()

	svc := &WebhookService{}

	hub1 := &model.MessageHub{Platform: "dingtalk", ConversationID: "c1", SentAt: time.Now()}
	svc.sendOutbound(context.Background(), ChannelDingTalk, "7",
		&ParsedPayload{EventID: "e1", Sender: "s", Content: "hi"}, "reply", hub1, nil)

	hub2 := &model.MessageHub{Platform: "dingtalk", ConversationID: "c2", SentAt: time.Now(),
		Extra: model.JSONMap{
			"session_webhook":            srv.URL,
			"session_webhook_expired_at": time.Now().Add(-time.Hour).Unix(),
		}}
	svc.sendOutbound(context.Background(), ChannelDingTalk, "7",
		&ParsedPayload{EventID: "e2", Sender: "s", Content: "hi"}, "reply", hub2, nil)

	mu.Lock()
	defer mu.Unlock()
	if calls != 0 {
		t.Fatalf("缺失/过期的 sessionWebhook 不应发起请求，实际 %d 次", calls)
	}
}

func TestDingTalkReceiveMessage_CapturesSessionWebhookAndTriggersAI(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.DingTalkAppAccount{}, &model.MessageHub{})

	aesKey := newDingTalkTestAESKey(t)

	acc := &model.DingTalkAppAccount{
		AppKey: "ak", AppSecret: "as", Token: "tok", AESKey: aesKey,
		InboundEnabled: true, Status: 1,
	}
	if err := db.Create(acc).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}

	_mc1 := cache.NewMemoryCache()
	defer _mc1.Close()
	ingress := NewInboxIngressServiceWithDB(db, _mc1)
	tr := &fakeAITrigger{}
	ingress.SetAITrigger(tr)
	webhookSvc := &WebhookService{ingressSvc: ingress}
	dtSvc := NewDingTalkAppService(db, webhookSvc)

	// 与 dtLiveRobotMsg 同一口径：createAt 现在是真实时序锚点，钉死在 2023 的夹具会被
	// 中台钩子3 判成历史堆积（只落库、不触发 AI）；会话 id 也必须一次一换，
	// 因为 message_hub 的会话查询按 conversation_id 收敛、不分账号。
	liveAt := time.Now().UnixMilli()
	convNonce := strconv.FormatInt(liveAt, 10)
	plain := `{"msgtype":"text","senderStaffId":"staff-9","conversationId":"cid-del-` + convNonce +
		`","msgId":"m-` + convNonce + `","createAt":` + strconv.FormatInt(liveAt, 10) +
		`,"text":{"content":"你好"},"sessionWebhook":"https://oapi.dingtalk.com/robot/send?access_token=xyz","sessionWebhookExpiredTime":1893456000000}`

	// 官方事件订阅布局（random16+len4+msg+receiveId）+ query 验签，
	// 与 dingtalk_official_contract_test.go 用的是同一套独立实现。
	enc := dtOfficialEncrypt(t, aesKey, plain, "dingCorpIdXYZ")
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	query := map[string]string{
		"signature": dtOfficialEventSign("tok", ts, "n1", enc),
		"timestamp": ts, "nonce": "n1",
	}
	envelope, _ := json.Marshal(map[string]string{"encrypt": enc})

	if err := dtSvc.ReceiveMessage(context.Background(), acc.ID, envelope, query, nil); err != nil {
		t.Fatalf("ReceiveMessage error: %v", err)
	}
	if tr.called != 1 {
		t.Fatalf("期望触发 AI 1 次，实际 %d", tr.called)
	}
	if tr.lastCust != "staff-9" {
		t.Errorf("发送者应归一到 senderStaffId=staff-9, got %q", tr.lastCust)
	}
	if tr.lastMeta == nil || tr.lastMeta.SessionWebhook != "https://oapi.dingtalk.com/robot/send?access_token=xyz" {
		t.Fatalf("sessionWebhook 应透传至 AI 触发链: %+v", tr.lastMeta)
	}
}

func TestHandleFeishuURLVerification_PlainChallengeEcho(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.FeishuAccount{})
	repo := repository.NewFeishuAccountRepository()
	repo.SetDB(context.Background(), db)

	acc := &model.FeishuAccount{AppID: "cli_x", AppSecret: "sec", VerificationToken: "vtok-1", EncryptKey: ""}
	if err := db.Create(acc).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}
	svc := &WebhookService{feishuRepo: repo}
	body, _ := json.Marshal(map[string]string{"challenge": "aj38fh", "token": "vtok-1", "type": "url_verification"})

	challenge, handled, err := svc.HandleFeishuURLVerification(context.Background(), "1", body)
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if challenge != "aj38fh" {
		t.Errorf("challenge echo mismatch: %q", challenge)
	}

	badBody, _ := json.Marshal(map[string]string{"challenge": "x", "token": "WRONG", "type": "url_verification"})
	if _, _, err := svc.HandleFeishuURLVerification(context.Background(), "1", badBody); err == nil {
		t.Fatal("token 错误时应拒绝")
	}

	normal := []byte(`{"header":{"event_type":"im.message.receive_v1"}}`)
	if _, handled, _ := svc.HandleFeishuURLVerification(context.Background(), "1", normal); handled {
		t.Fatal("普通事件不应被验证流程拦截")
	}
}

func TestVerify_FeishuSignatureFallbackToEncryptKey(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.FeishuAccount{})
	repo := repository.NewFeishuAccountRepository()
	repo.SetDB(context.Background(), db)

	const encKey = "test-encrypt-key-1234"
	acc := &model.FeishuAccount{AppID: "cli_y", AppSecret: "sec", VerificationToken: "v", EncryptKey: encKey}
	if err := db.Create(acc).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}
	svc := &WebhookService{feishuRepo: repo}

	body := []byte(`{"header":{"event_type":"im.message.receive_v1"}}`)
	ts, nonce := "1700000000", "nonce-abc"
	h := sha256.New()
	h.Write([]byte(encKey + ts + nonce))
	h.Write(body)
	sig := base64.StdEncoding.EncodeToString(h.Sum(nil))

	headers := map[string]string{"X-Lark-Signature": sig, "X-Lark-Timestamp": ts, "X-Lark-Nonce": nonce}
	ok, err := svc.Verify(context.Background(), ChannelFeishu, "1", body, headers, nil)
	if err != nil {
		t.Fatalf("Verify error: %v", err)
	}
	if !ok {
		t.Fatal("验签应回退 feishu_accounts.encrypt_key 并通过")
	}

	headers["X-Lark-Signature"] = "bogus-signature"
	if ok, _ := svc.Verify(context.Background(), ChannelFeishu, "1", body, headers, nil); ok {
		t.Fatal("错误签名不应通过")
	}
}

func TestDispatchFeishu_DecryptsEncryptedEvent(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.FeishuAccount{}, &model.MessageHub{}, &model.InboxConversation{})
	repo := repository.NewFeishuAccountRepository()
	repo.SetDB(context.Background(), db)

	const encKey = "feishu-test-key-00000000000000000000000"
	acc := &model.FeishuAccount{AppID: "cli_z", AppSecret: "sec", EncryptKey: encKey}
	if err := db.Create(acc).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}

	inner := `{"header":{"event_type":"im.message.receive_v1","event_id":"fs-enc-1"},"event":{"sender":{"sender_id":{"open_id":"ou_9"}},"message":{"message_id":"om_1","chat_id":"oc_g1","chat_type":"group","message_type":"text","content":"{\"text\":\"加密消息测试\"}"}}}`
	enc, err := feishuEncryptForTest(encKey, inner)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	raw, _ := json.Marshal(map[string]string{"encrypt": enc})

	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())
	p := &ParsedPayload{EventID: "fs-enc-1"}
	hub, derr := svc.dispatchFeishu(context.Background(), "1", p, raw)
	if derr != nil {
		t.Fatalf("dispatchFeishu error: %v", derr)
	}
	if hub == nil {
		t.Fatal("加密事件应解密入库")
	}
	if !strings.Contains(hub.Content, "加密消息测试") {
		t.Errorf("解密后内容不符: %q", hub.Content)
	}
	if p.Sender != "ou_9" || p.Content != "加密消息测试" {
		t.Errorf("payload 未回填: sender=%q content=%q", p.Sender, p.Content)
	}
}

func TestFeishuTextContentJSON_IsStringifiedJSON(t *testing.T) {
	out := feishuTextContentJSON("第一行\n第二行")

	if !strings.HasPrefix(out, "{\"text\":") {
		t.Fatalf("content 必须是字符串化 JSON，got %q", out)
	}
	var back map[string]string
	if err := json.Unmarshal([]byte(out), &back); err != nil {
		t.Fatalf("应可反序列化: %v", err)
	}
	if back["text"] != "第一行\n第二行" {
		t.Errorf("文本往返不一致: %q", back["text"])
	}
}

func TestDecryptFeishuEvent_OfficialPrefixFormat(t *testing.T) {
	raw := bytes.Repeat([]byte{0x37}, 32)
	key43 := base64.StdEncoding.WithPadding(base64.NoPadding).EncodeToString(raw)
	if len(key43) != 43 {
		t.Fatalf("测试前置失败: key len=%d", len(key43))
	}

	payload := []byte(`{"challenge":"abc","type":"url_verification","token":"t"}`)

	block, _ := aes.NewCipher(raw)

	frame := make([]byte, 0, 20+len(payload)+aes.BlockSize)
	for i := 0; i < 16; i++ {
		frame = append(frame, byte(i*7+1))
	}
	frame = binary.BigEndian.AppendUint32(frame, uint32(len(payload)))
	frame = append(frame, payload...)
	pad := aes.BlockSize - len(frame)%aes.BlockSize
	frame = append(frame, bytes.Repeat([]byte{byte(pad)}, pad)...)

	iv := bytes.Repeat([]byte{0xA5}, aes.BlockSize)
	ctBody := make([]byte, len(frame))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ctBody, frame)
	encrypted := base64.StdEncoding.EncodeToString(append(append([]byte{}, iv...), ctBody...))

	got, err := DecryptFeishuEvent(key43, encrypted)
	if err != nil {
		t.Fatalf("官方格式解密失败: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("解密内容不符:\n got=%q\nwant=%q", got, payload)
	}
}
