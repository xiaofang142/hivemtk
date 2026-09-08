package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupWebhookTestDB(t *testing.T) *gorm.DB {
	return testutil.NewTestDB(t,
		&model.WebhookEvent{},
		&model.UnifiedMessage{},
		&model.IntegrationAccount{},
	)
}

// TestContentHashMsgID_StableContract 锚定前后端共享的回环去重哈希契约。
//
// 该值被前端 types.js::contentHash 镜像（FNV-1a 32 位 + mh: 前缀 + 输入 channel|trim(content)，不含 conversationID）。
// AI 出站 MsgID 即此值；前端扫描到平台 AI 回显重新上报时携带相同 content_hash，
// 后端 GetByMsgID 才能命中（钩子2）→ 幂等跳过。算法任何漂移都会让回环防护断裂，
// 故用此测试作为契约锚点（跨语言一致性由前端单测对相同输入断言同一值保证）。
func TestContentHashMsgID_StableContract(t *testing.T) {

	const channel, conv, content = "douyin", "c1", "你好"
	got := ContentHashMsgID(channel, conv, content)
	if got != "mh:00550fed" {
		t.Fatalf("ContentHashMsgID 契约值漂移: got=%s want=mh:00550fed", got)
	}
	if ContentHashMsgID("xhs", conv, content) == got {
		t.Fatalf("channel 不同应哈希不同")
	}
	if ContentHashMsgID(channel, conv, "你好吗") == got {
		t.Fatalf("content 不同应哈希不同")
	}
	if ContentHashMsgID(channel, "c2", content) != got {
		t.Fatalf("conversationID 不参与哈希：不同 conv 应哈希相同")
	}
	if ContentHashMsgID(channel, conv, "  你好  ") != got {
		t.Fatalf("首尾空白应被 trim，不影响哈希")
	}
}

func TestWebhookService_ParsePayload_BasicKeys(t *testing.T) {
	s := &WebhookService{}
	body := []byte(`{"event_id":"e1","event_type":"message","content":"hi","sender":"u1","chat_id":"c1"}`)
	p, err := s.ParsePayload(context.Background(), ChannelCustom, body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.EventID != "e1" || p.EventType != "message" {
		t.Errorf("unexpected: %+v", p)
	}
	if p.Content != "hi" || p.Sender != "u1" || p.ChatID != "c1" {
		t.Errorf("unexpected fields: %+v", p)
	}
}

func TestWebhookService_ParsePayload_AliasKeys(t *testing.T) {
	s := &WebhookService{}
	cases := []struct {
		name string
		body string
		want string
	}{
		{"wechat_xml_alias", `{"MsgId":"wx1","MsgType":"text","Content":"hi","FromUserName":"u1"}`, "wx1"},
		{"event_alias", `{"event":"user.created","id":"42"}`, "user.created"},
		{"type_alias", `{"type":"ORDER_PAID","id":"99"}`, "ORDER_PAID"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := s.ParsePayload(context.Background(), ChannelCustom, []byte(c.body))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if p.EventID == "" && p.EventType == "" {
				t.Errorf("expected alias match for %s", c.name)
			}
		})
	}
}

func TestWebhookService_ParsePayload_Invalid(t *testing.T) {
	s := &WebhookService{}
	_, err := s.ParsePayload(context.Background(), ChannelCustom, []byte("not json"))
	if err == nil {
		t.Error("expected error for invalid json")
	}
}

func TestWebhookService_ParsePayload_EmptyKeys(t *testing.T) {
	s := &WebhookService{}
	p, _ := s.ParsePayload(context.Background(), ChannelCustom, []byte(`{}`))
	if p == nil {
		t.Fatal("expected payload")
	}
	if p.EventID != "" || p.Content != "" {
		t.Errorf("expected empty, got %+v", p)
	}
}

func TestWebhookService_ParsePayload_NestedJSON(t *testing.T) {
	s := &WebhookService{}
	body := []byte(`{"data":{"event_id":"nested1","content":"deep"}}`)
	p, err := s.ParsePayload(context.Background(), ChannelCustom, body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.Extra == nil {
		t.Error("expected Extra")
	}
}

func TestWebhookService_VerifyHMAC_OK(t *testing.T) {
	secret := "test-secret"
	body := []byte(`{"hello":"world"}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))
	hdr := map[string]string{"Signature": sig}
	if !verifyHMAC(secret, body, hdr, "Signature") {
		t.Error("expected verify ok")
	}
}

func TestWebhookService_VerifyHMAC_WithPrefix(t *testing.T) {
	secret := "test"
	body := []byte(`{}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	hdr := map[string]string{"X-Signature": sig}
	if !verifyHMAC(secret, body, hdr, "X-Signature") {
		t.Error("expected verify ok with prefix")
	}
}

func TestWebhookService_VerifyHMAC_WrongSecret(t *testing.T) {
	body := []byte(`{}`)
	hdr := map[string]string{"Signature": "abc123"}
	if verifyHMAC("real", body, hdr, "Signature") {
		t.Error("expected fail with wrong secret")
	}
}

func TestWebhookService_VerifyHMAC_NoHeader(t *testing.T) {
	if verifyHMAC("real", []byte("{}"), map[string]string{}, "Signature") {
		t.Error("expected fail with no header")
	}
}

func TestWebhookService_VerifyHMAC_EmptySecret(t *testing.T) {
	if verifyHMAC("", []byte("{}"), map[string]string{"Signature": "x"}, "Signature") {
		t.Error("expected fail with empty secret")
	}
}

func TestWebhookService_VerifyHMAC_MultiHeaders(t *testing.T) {
	secret := "k"
	body := []byte(`{"a":1}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))
	hdr := map[string]string{"X-Hub-Signature-256": sig}
	if !verifyHMAC(secret, body, hdr, "X-Signature", "X-Hub-Signature-256") {
		t.Error("expected multi header match")
	}
}

func TestWebhookService_VerifyWechat_OK(t *testing.T) {
	token := "tk"
	ts, nonce := "123", "abc"
	parts := []string{token, ts, nonce}
	sort.Strings(parts)
	h := sha1Hex([]byte(strings.Join(parts, "")))
	hdr := map[string]string{
		"X-Wechat-Timestamp": ts,
		"X-Wechat-Nonce":     nonce,
		"X-Wechat-Signature": h,
	}
	if !verifyWechat(token, []byte("{}"), hdr) {
		t.Error("expected verify ok")
	}
}

func TestWebhookService_VerifyWechat_Missing(t *testing.T) {
	if verifyWechat("tk", []byte("{}"), map[string]string{"X-Wechat-Signature": "x"}) {
		t.Error("expected fail with missing ts/nonce")
	}
}

func TestWebhookService_VerifyWechat_EmptyToken(t *testing.T) {
	if verifyWechat("", []byte("{}"), map[string]string{"X-Wechat-Signature": "x"}) {
		t.Error("expected fail with empty token")
	}
}

func TestWebhookService_VerifyWechat_WrongSig(t *testing.T) {
	if verifyWechat("tk", []byte("{}"), map[string]string{
		"X-Wechat-Timestamp": "1", "X-Wechat-Nonce": "2", "X-Wechat-Signature": "deadbeef",
	}) {
		t.Error("expected fail with wrong sig")
	}
}

func TestWebhookService_Receive_EmptyBody(t *testing.T) {
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "true")
	s := NewWebhookService(setupWebhookTestDB(t))
	defer s.Stop(context.Background())
	r, err := s.Receive(context.Background(), &ReceiveRequest{Channel: ChannelCustom, AccountID: "a1", Body: nil})
	if err != nil {
		t.Fatalf("recv: %v", err)
	}
	if r.Accepted {
		t.Error("expected rejected for empty body")
	}
}

func TestWebhookService_Receive_NoAccount(t *testing.T) {
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "true")
	s := NewWebhookService(setupWebhookTestDB(t))
	defer s.Stop(context.Background())
	r, _ := s.Receive(context.Background(), &ReceiveRequest{Channel: ChannelCustom, Body: []byte("{}")})
	if r.Accepted {
		t.Error("expected rejected for missing account")
	}
}

func TestWebhookService_Receive_Custom_NoSecret(t *testing.T) {
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "true")
	db := setupWebhookTestDB(t)
	s := NewWebhookService(db)
	defer s.Stop(context.Background())
	body := []byte(`{"event_id":"e1","event_type":"message","content":"hi"}`)
	r, err := s.Receive(context.Background(), &ReceiveRequest{Channel: ChannelCustom, AccountID: "a1", Body: body})
	if err != nil {
		t.Fatalf("recv: %v", err)
	}
	if !r.Accepted {
		t.Errorf("expected accepted, got %+v", r)
	}
	if r.EventID != "e1" {
		t.Errorf("expected event_id e1, got %s", r.EventID)
	}
}

func TestWebhookService_Receive_Duplicate(t *testing.T) {
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "true")
	db := setupWebhookTestDB(t)
	s := NewWebhookService(db)
	defer s.Stop(context.Background())
	body := []byte(`{"event_id":"dup1","event_type":"message","content":"hi"}`)
	r1, _ := s.Receive(context.Background(), &ReceiveRequest{Channel: ChannelCustom, AccountID: "a1", Body: body})
	if !r1.Accepted || r1.Duplicate {
		t.Errorf("first: %+v", r1)
	}
	r2, _ := s.Receive(context.Background(), &ReceiveRequest{Channel: ChannelCustom, AccountID: "a1", Body: body})
	if !r2.Accepted || !r2.Duplicate {
		t.Errorf("second expected duplicate, got %+v", r2)
	}
}

func TestWebhookService_Receive_GeneratedEventID(t *testing.T) {
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "true")
	db := setupWebhookTestDB(t)
	s := NewWebhookService(db)
	defer s.Stop(context.Background())
	body := []byte(`{"content":"hi"}`)
	r, _ := s.Receive(context.Background(), &ReceiveRequest{Channel: ChannelCustom, AccountID: "a1", Body: body})
	if !r.Accepted {
		t.Errorf("expected accepted, got %+v", r)
	}
	if r.EventID == "" {
		t.Error("expected generated event_id")
	}
	if !strings.HasPrefix(r.EventID, "evt_") {
		t.Errorf("expected evt_ prefix, got %s", r.EventID)
	}
}

func TestWebhookService_Receive_DefaultEventType(t *testing.T) {
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "true")
	db := setupWebhookTestDB(t)
	s := NewWebhookService(db)
	defer s.Stop(context.Background())
	eventID := fmt.Sprintf("e1-%d", time.Now().UnixNano())
	body := []byte(fmt.Sprintf(`{"event_id":"%s","content":"hi"}`, eventID))
	r, _ := s.Receive(context.Background(), &ReceiveRequest{Channel: ChannelCustom, AccountID: "a1", Body: body})
	if !r.Accepted {
		t.Errorf("expected accepted, got %+v", r)
	}
	if r.EventType != "unknown" {
		t.Errorf("expected unknown, got %s", r.EventType)
	}
}

func TestWebhookService_Receive_InvalidJSON(t *testing.T) {
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "true")
	db := setupWebhookTestDB(t)
	s := NewWebhookService(db)
	defer s.Stop(context.Background())
	r, _ := s.Receive(context.Background(), &ReceiveRequest{Channel: ChannelCustom, AccountID: "a1", Body: []byte("not json")})
	if r.Accepted {
		t.Error("expected rejected")
	}
}

func TestWebhookService_Receive_HMAC_Douyin(t *testing.T) {
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "true")
	db := setupWebhookTestDB(t)
	db.Create(&model.IntegrationAccount{Platform: "douyin", APISecret: "secret123", Status: 1})
	s := NewWebhookService(db)
	defer s.Stop(context.Background())

	body := []byte(`{"event_id":"d1","content":"hi"}`)
	mac := hmac.New(sha256.New, []byte("secret123"))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))
	hdr := map[string]string{"X-Douyin-Signature": sig}
	r, err := s.Receive(context.Background(), &ReceiveRequest{Channel: ChannelDouyin, AccountID: "a1", Body: body, Headers: hdr})
	if err != nil {
		t.Fatalf("recv: %v", err)
	}
	if !r.Accepted {
		t.Errorf("expected accepted, got %+v", r)
	}
}

func TestWebhookService_Receive_HMAC_BadSig(t *testing.T) {
	db := setupWebhookTestDB(t)
	db.Create(&model.IntegrationAccount{Platform: "douyin", APISecret: "secret123", Status: 1})
	s := NewWebhookService(db)
	defer s.Stop(context.Background())

	body := []byte(`{"event_id":"d1"}`)
	hdr := map[string]string{"X-Douyin-Signature": "badsig"}
	r, _ := s.Receive(context.Background(), &ReceiveRequest{Channel: ChannelDouyin, AccountID: "a1", Body: body, Headers: hdr})
	if r.Accepted {
		t.Error("expected rejected for bad sig")
	}
	if !r.VerifyFail {
		t.Error("expected verify_fail")
	}
}

func TestWebhookService_Receive_HMAC_Kuaishou(t *testing.T) {
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "true")
	db := setupWebhookTestDB(t)
	db.Create(&model.IntegrationAccount{Platform: "kuaishou", APISecret: "ks_secret", Status: 1})
	s := NewWebhookService(db)
	defer s.Stop(context.Background())

	body := []byte(`{"event_id":"k1"}`)
	mac := hmac.New(sha256.New, []byte("ks_secret"))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))
	hdr := map[string]string{"X-Signature": sig}
	r, _ := s.Receive(context.Background(), &ReceiveRequest{Channel: ChannelKuaishou, AccountID: "a1", Body: body, Headers: hdr})
	if !r.Accepted {
		t.Errorf("expected accepted, got %+v", r)
	}
}

func TestWebhookService_Receive_HMAC_Xiaohongshu(t *testing.T) {
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "true")
	db := setupWebhookTestDB(t)
	db.Create(&model.IntegrationAccount{Platform: "xiaohongshu", APISecret: "xhs", Status: 1})
	s := NewWebhookService(db)
	defer s.Stop(context.Background())

	body := []byte(`{"event_id":"x1"}`)
	mac := hmac.New(sha256.New, []byte("xhs"))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))
	hdr := map[string]string{"X-Hub-Signature-256": "sha256=" + sig}
	r, _ := s.Receive(context.Background(), &ReceiveRequest{Channel: ChannelXiaohongshu, AccountID: "a1", Body: body, Headers: hdr})
	if !r.Accepted {
		t.Errorf("expected accepted, got %+v", r)
	}
}

func TestWebhookService_Receive_Wechat(t *testing.T) {
	s := &WebhookService{}
	body := []byte(`{"event_id":"w1","msg_signature":"x","timestamp":"1","nonce":"2"}`)
	hdr := map[string]string{"X-Wechat-Timestamp": "1", "X-Wechat-Nonce": "2", "X-Wechat-Signature": "x"}
	r, _ := s.Receive(context.Background(), &ReceiveRequest{Channel: ChannelWechat, AccountID: "a1", Body: body, Headers: hdr})

	// fail-closed：secret 未配置时必须拒绝验签（与其他渠道一致）
	if r.Accepted {
		t.Errorf("expected rejected (secret 未配置时 fail-closed), got %+v", r)
	}
}

func TestWebhookService_Receive_WeCom(t *testing.T) {
	s := &WebhookService{}
	body := []byte(`{"msg_signature":"x","timestamp":"1","nonce":"2"}`)
	r, _ := s.Receive(context.Background(), &ReceiveRequest{Channel: ChannelWeCom, AccountID: "a1", Body: body})
	if r.Accepted {
		t.Error("expected rejected (no token configured)")
	}
}

func TestWebhookService_Receive_RateLimit(t *testing.T) {
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "true")
	db := setupWebhookTestDB(t)
	s := NewWebhookService(db)
	defer s.Stop(context.Background())
	key := "custom:rl-test"
	b := &tokenBucket{capacity: 5, refillRate: 0, tokens: 0, lastRefill: time.Now()}
	s.mu.Lock()
	s.rlBuckets[key] = b
	s.mu.Unlock()
	r, _ := s.Receive(context.Background(), &ReceiveRequest{Channel: ChannelCustom, AccountID: "rl-test", Body: []byte(`{"event_id":"rl1","content":"hi"}`)})
	if r.Accepted {
		t.Error("expected rate limited")
	}
	if !r.RateLimit {
		t.Errorf("expected rate_limited flag, got %+v", r)
	}
}

func TestWebhookService_ToUnifiedMessage(t *testing.T) {
	s := &WebhookService{}
	p := &ParsedPayload{EventID: "e1", Content: "hi", Sender: "u1", ChatID: "c1"}
	um := s.ToUnifiedMessage(context.Background(), ChannelDouyin, "a1", p)
	if um.Platform != model.PlatformDouyin {
		t.Errorf("expected douyin platform, got %s", um.Platform)
	}
	if um.Content != "hi" {
		t.Errorf("expected content hi, got %s", um.Content)
	}
	if um.MessageID == "" {
		t.Error("expected message id")
	}
	if um.Status != model.MessageStatusPending {
		t.Errorf("expected pending status")
	}
}

func TestWebhookService_TruncateForStore(t *testing.T) {
	s := &WebhookService{}
	short := []byte("hello")
	if s.TruncateForStore(context.Background(), short) != "hello" {
		t.Error("short should not truncate")
	}
	long := make([]byte, 70*1024)
	for i := range long {
		long[i] = 'a'
	}
	out := s.TruncateForStore(context.Background(), long)
	if len(out) <= 64*1024 {
		t.Errorf("expected truncation, got len=%d", len(out))
	}
	if !strings.Contains(out, "truncated") {
		t.Error("expected truncated marker")
	}
}

func TestWebhookService_GenerateEventID_Stable(t *testing.T) {
	s := &WebhookService{}
	body := []byte(`{"a":1}`)
	id1 := s.generateEventID(context.Background(), ChannelCustom, "a1", body)
	id2 := s.generateEventID(context.Background(), ChannelCustom, "a1", body)
	if id1 != id2 {
		t.Error("expected same id for same body")
	}
	id3 := s.generateEventID(context.Background(), ChannelCustom, "a2", body)
	if id1 == id3 {
		t.Error("expected different id for different account")
	}
}

func TestWebhookService_DispatchToUnified_InsertsRow(t *testing.T) {
	db := setupWebhookTestDB(t)
	s := NewWebhookService(db)
	defer s.Stop(context.Background())
	um := &model.UnifiedMessage{MessageID: "msg_x", Platform: "douyin", Content: "hi"}
	if err := s.dispatchToUnified(context.Background(), um); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	var n int64
	db.Model(&model.UnifiedMessage{}).Count(&n)
	if n != 1 {
		t.Errorf("expected 1 row, got %d", n)
	}
}

func TestWebhookService_HandleJob_MarksProcessed(t *testing.T) {
	db := setupWebhookTestDB(t)
	s := NewWebhookService(db)
	defer s.Stop(context.Background())
	evt := &model.WebhookEvent{Platform: "custom", EventID: "h1", EventType: "message", RawData: "{}", Processed: false}
	db.Create(evt)
	body := []byte(`{"event_id":"h1","content":"hi","sender":"u1","chat_id":"c1"}`)
	job := &webhookJob{event: evt, raw: body, header: nil}
	s.handleJob(context.Background(), job)

	var got model.WebhookEvent
	db.First(&got, evt.ID)
	if !got.Processed {
		t.Error("expected processed")
	}
}

func TestWebhookService_HandleJob_BadJSON(t *testing.T) {
	db := setupWebhookTestDB(t)
	s := NewWebhookService(db)
	defer s.Stop(context.Background())
	evt := &model.WebhookEvent{Platform: "custom", EventID: "h2", EventType: "message"}
	db.Create(evt)
	job := &webhookJob{event: evt, raw: []byte("bad"), header: nil}
	s.handleJob(context.Background(), job)

	var got model.WebhookEvent
	db.First(&got, evt.ID)
	if got.Processed {
		t.Error("expected not processed")
	}
}

func TestWebhookService_QueueLen(t *testing.T) {
	s := NewWebhookService(setupWebhookTestDB(t))
	defer s.Stop(context.Background())
	if s.QueueLen(context.Background()) != 0 {
		t.Errorf("expected empty queue, got %d", s.QueueLen(context.Background()))
	}
}

func TestWebhookService_PendingCount(t *testing.T) {
	db := setupWebhookTestDB(t)
	s := NewWebhookService(db)
	defer s.Stop(context.Background())
	db.Create(&model.WebhookEvent{Platform: "c", EventID: "p1", EventType: "e", Processed: false})
	db.Create(&model.WebhookEvent{Platform: "c", EventID: "p2", EventType: "e", Processed: true})
	if got := s.PendingCount(context.Background()); got != 1 {
		t.Errorf("expected 1 pending, got %d", got)
	}
}

func TestWebhookService_Dedup_ExpiresAfterTTL(t *testing.T) {
	// -count=N 隔离：isDuplicate 直接读写全局缓存键 mtk:webhook:dedup:<eventID>
	//（TTL 5 分钟），先清理上一轮残留再断言。
	cleanupWebhookDedupKeysForTest(t, "dup-evt-A", "dup-evt-B")
	s := &WebhookService{}
	if s.isDuplicate(context.Background(), "dup-evt-A") {
		t.Error("expected first occurrence not duplicate")
	}
	if !s.isDuplicate(context.Background(), "dup-evt-A") {
		t.Error("expected second occurrence duplicate")
	}
	if s.isDuplicate(context.Background(), "dup-evt-B") {
		t.Error("expected distinct eventID not duplicate")
	}
}

func TestWebhookService_Dedup_EmptyID(t *testing.T) {
	s := &WebhookService{}
	if s.isDuplicate(context.Background(), "") {
		t.Error("empty id should not be duplicate")
	}
}

func TestWebhookService_TokenBucket(t *testing.T) {
	b := &tokenBucket{capacity: 5, refillRate: 1, tokens: 5, lastRefill: time.Now()}
	for i := 0; i < 5; i++ {
		if !b.allow(context.Background()) {
			t.Errorf("expected allow at iter %d", i)
		}
	}
	if b.allow(context.Background()) {
		t.Error("expected reject after exhaustion")
	}
}

func TestWebhookService_AllChannels(t *testing.T) {
	s := &WebhookService{}
	for _, ch := range []WebhookChannel{
		ChannelDouyin, ChannelKuaishou, ChannelXiaohongshu, ChannelXianyu,
		ChannelTiktok, ChannelWechat, ChannelWeCom,
		ChannelWhatsapp, ChannelTelegram, ChannelCustom,
	} {
		_, _ = s.Verify(context.Background(), ch, "a1", []byte("{}"), map[string]string{}, map[string]string{})
	}
}

func TestWebhookService_PayloadSize_Small(t *testing.T) {
	s := &WebhookService{}
	body, _ := json.Marshal(map[string]any{"a": 1, "b": "test"})
	p, err := s.ParsePayload(context.Background(), ChannelCustom, body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p == nil {
		t.Fatal("expected payload")
	}
}

func TestWebhookService_PayloadSize_Large(t *testing.T) {
	s := &WebhookService{}
	large := map[string]any{}
	for i := 0; i < 1000; i++ {
		large[fmtKey(i)] = "value"
	}
	body, _ := json.Marshal(large)
	p, err := s.ParsePayload(context.Background(), ChannelCustom, body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p == nil {
		t.Fatal("expected payload")
	}
}

func fmtKey(i int) string {
	return "key_" + string(rune('a'+i%26)) + "_" + string(rune('0'+i/26%10))
}

// TestWebhookInsecureWebhookGuard W-1 验签绕过防护：
// production 环境下 ALLOW_INSECURE_WEBHOOK=true 必须拒绝启动；dev/test/未设置保持现状。
func TestWebhookInsecureWebhookGuard(t *testing.T) {
	cases := []struct {
		name          string
		appEnv        string
		mode          string
		allowInsecure string
		wantFatal     bool
	}{
		{"production+开关开启_拒绝", "production", "", "true", true},
		{"MODE=production别名_拒绝", "", "production", "true", true},
		{"production大小写不敏感_拒绝", "Production", "", "true", true},
		{"production但开关未开_放行", "production", "", "false", false},
		{"production且未设置变量_放行", "production", "", "", false},
		{"development开启_放行", "development", "", "true", false},
		{"test开启_放行", "test", "", "true", false},
		{"未设置环境开启_放行(dev现状)", "", "", "true", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := insecureWebhookStartupError(tc.appEnv, tc.mode, tc.allowInsecure)
			if gotFatal := err != nil; gotFatal != tc.wantFatal {
				t.Fatalf("wantFatal=%v, got err=%v", tc.wantFatal, err)
			}
			if !tc.wantFatal && err == nil {
				return
			}
			if tc.wantFatal && !strings.Contains(err.Error(), "ALLOW_INSECURE_WEBHOOK") {
				t.Errorf("fatal 指引应包含变量名，got: %v", err)
			}
		})
	}
}
