package controller

// 025 平台批全链路模拟验收（无真实账号，用签名 fixture + httptest 平台模拟）。
//
// 覆盖：custom / slack / onebot / wecombot / wecomcs 五个渠道的
// 「HTTP 路由 → 验签 → 解析 → 入队 → dispatch → message_hub」链路。
// 真实账号项（T707/T806/T904）仍需在有号环境按 tasks.md 补跑。

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/channelbot/core"
	"hivemtk-user/internal/channelbot/wecombot"
	"hivemtk-user/internal/channelbot/wecomcs"
	"hivemtk-user/internal/model"
	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type imPlatformEnv struct {
	g   *gin.Engine
	svc *service.WebhookService
	db  *gorm.DB
}

func newIMPlatformEnv(t *testing.T) *imPlatformEnv {
	t.Helper()
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "")
	database := testutil.NewTestDB(t,
		&model.WebhookEvent{},
		&model.UnifiedMessage{},
		&model.MessageHub{},
		&model.InboxConversation{},
		&model.IntegrationAccount{},
		&model.CustomerSession{},
		&model.SessionMessage{},
		&model.AgentStatus{},
		&model.AISuggestion{},
		&model.FAQEntry{},
		&model.SOPTemplate{},
		&model.Customer{},
		&model.AIAgent{},
		&model.ChannelAgentBinding{},
		&model.ChannelEventAudit{},
		&model.SlackAccount{},
		&model.OneBotAccount{},
		&model.WeCombBotAccount{},
		&model.CustomAccount{},
		&model.WeComCSAccount{},
	)
	dbutil.SetTestDB(database)
	t.Cleanup(func() { dbutil.SetTestDB(nil) })

	svc := service.NewWebhookService()
	t.Cleanup(func() { svc.Stop(context.Background()) })
	gin.SetMode(gin.TestMode)
	g := gin.New()
	NewWebhookController(svc).RegisterRoutes(g)
	return &imPlatformEnv{g: g, svc: svc, db: database}
}

func (e *imPlatformEnv) post(t *testing.T, path string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.g.ServeHTTP(rec, req)
	return rec
}

func (e *imPlatformEnv) waitHub(t *testing.T, channel, content string) *model.MessageHub {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		var hub model.MessageHub
		err := e.db.Where("platform = ? AND content = ?", channel, content).First(&hub).Error
		if err == nil {
			return &hub
		}
		time.Sleep(80 * time.Millisecond)
	}
	t.Fatalf("等待 %s message_hub（content=%q）超时", channel, content)
	return nil
}

func (e *imPlatformEnv) countHub(t *testing.T, channel, content string) int64 {
	t.Helper()
	var n int64
	if err := e.db.Model(&model.MessageHub{}).Where("platform = ? AND content = ?", channel, content).Count(&n).Error; err != nil {
		t.Fatalf("count hub: %v", err)
	}
	return n
}

// ── custom ─────────────────────────────────────────────────────────────

func customSignature(secret string, body []byte) map[string]string {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	return map[string]string{"X-Hive-Signature": "t=" + ts + ",v1=" + hex.EncodeToString(mac.Sum(nil))}
}

func TestCustomFullChain(t *testing.T) {
	e := newIMPlatformEnv(t)
	acc := &model.CustomAccount{AccountName: "custom-sim", Secret: "cus" + "tom" + "-secret", CallbackURL: "https://example.com/cb", Status: 1}
	if err := e.db.Create(acc).Error; err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"event_id":"ce1","channel_msg_id":"cm1","conversation_id":"conv-1","sender":{"id":"u1","name":"张三"},"msg_type":"text","content":"custom-hi","timestamp":1700000000,"is_group":false}`)
	path := fmt.Sprintf("/api/webhook/custom/%d", acc.ID)

	// 错签必须 401 且不落库。
	bad := map[string]string{"X-Hive-Signature": "t=" + strconv.FormatInt(time.Now().Unix(), 10) + ",v1=deadbeef"}
	if rec := e.post(t, path, body, bad); rec.Code != http.StatusUnauthorized {
		t.Fatalf("错签应 401，got %d %s", rec.Code, rec.Body.String())
	}
	var events int64
	e.db.Model(&model.WebhookEvent{}).Where("event_id = ?", "ce1").Count(&events)
	if events != 0 {
		t.Fatalf("错签请求不应落 webhook_events，got %d", events)
	}

	if rec := e.post(t, path, body, customSignature("custom-secret", body)); rec.Code != http.StatusOK {
		t.Fatalf("正确签名应 200，got %d %s", rec.Code, rec.Body.String())
	}
	e.waitHub(t, model.ChannelCustom, "custom-hi")

	// 同 event_id 重投：幂等，不产生第二行。
	e.post(t, path, body, customSignature("custom-secret", body))
	time.Sleep(300 * time.Millisecond)
	if n := e.countHub(t, model.ChannelCustom, "custom-hi"); n != 1 {
		t.Fatalf("同 event_id 重投应幂等，message_hub=%d", n)
	}
}

// ── Slack ─────────────────────────────────────────────────────────────

func slackHeaders(secret string, body []byte) map[string]string {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v0:" + ts + ":" + string(body)))
	return map[string]string{
		"X-Slack-Request-Timestamp": ts,
		"X-Slack-Signature":         "v0=" + hex.EncodeToString(mac.Sum(nil)),
	}
}

func TestSlackFullChain(t *testing.T) {
	e := newIMPlatformEnv(t)
	acc := &model.SlackAccount{AccountName: "slack-sim", TeamID: "T1", BotToken: "xoxb-sim", SigningSecret: "slack-secret", Status: 1}
	if err := e.db.Create(acc).Error; err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/webhook/slack/%d", acc.ID)

	// url_verification 先验签再回 challenge。
	challenge := []byte(`{"type":"url_verification","challenge":"chal-1"}`)
	rec := e.post(t, path, challenge, slackHeaders("slack-secret", challenge))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "chal-1") {
		t.Fatalf("url_verification 应回 challenge，got %d %s", rec.Code, rec.Body.String())
	}

	body := []byte(`{"type":"event_callback","event_id":"Ev-sim-1","team_id":"T1","event":{"type":"message","user":"U1","text":"slack-hi","channel":"C1","ts":"1700000000.0001","channel_type":"im"}}`)
	if rec := e.post(t, path, body, slackHeaders("slack-secret", body)); rec.Code != http.StatusOK {
		t.Fatalf("Slack 事件应 200，got %d %s", rec.Code, rec.Body.String())
	}
	e.waitHub(t, model.ChannelSlack, "slack-hi")

	// 机器人自身消息必须忽略（不落库）。
	bot := []byte(`{"type":"event_callback","event_id":"Ev-sim-2","event":{"type":"message","bot_id":"B1","text":"slack-bot","channel":"C1","ts":"1700000000.0002"}}`)
	e.post(t, path, bot, slackHeaders("slack-secret", bot))
	time.Sleep(300 * time.Millisecond)
	if n := e.countHub(t, model.ChannelSlack, "slack-bot"); n != 0 {
		t.Fatalf("bot 自身消息不应落库，got %d", n)
	}
}

// ── OneBot ────────────────────────────────────────────────────────────

func TestOneBotFullChain(t *testing.T) {
	e := newIMPlatformEnv(t)
	acc := &model.OneBotAccount{AccountName: "onebot-sim", SelfID: "10001", AccessToken: "ob-token", Status: 1}
	if err := e.db.Create(acc).Error; err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/webhook/onebot/%d", acc.ID)
	body := []byte(`{"time":1700000000,"self_id":10001,"post_type":"message","message_type":"group","message_id":88,"group_id":777,"user_id":42,"message":[{"type":"text","data":{"text":"onebot-hi"}}],"sender":{"nickname":"张三"}}`)

	if rec := e.post(t, path, body, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("缺 token 应 401，got %d %s", rec.Code, rec.Body.String())
	}
	auth := map[string]string{"Authorization": "Bearer ob-token"}
	if rec := e.post(t, path, body, auth); rec.Code != http.StatusOK {
		t.Fatalf("正确 token 应 200，got %d %s", rec.Code, rec.Body.String())
	}
	hub := e.waitHub(t, model.ChannelOneBot, "onebot-hi")
	if !hub.IsGroup || hub.GroupID != "777" || hub.SenderID != "42" {
		t.Fatalf("OneBot hub 映射错误: %+v", hub)
	}

	// 撤回 notice → message.deleted，不产生 hub 消息行。
	recall := []byte(`{"time":1700000001,"self_id":10001,"post_type":"notice","notice_type":"group_recall","group_id":777,"user_id":42,"operator_id":42}`)
	if rec := e.post(t, path, recall, auth); rec.Code != http.StatusOK {
		t.Fatalf("撤回事件应 200，got %d %s", rec.Code, rec.Body.String())
	}
	time.Sleep(300 * time.Millisecond)
	var audits int64
	e.db.Model(&model.ChannelEventAudit{}).Where("channel = ? AND event_type = ?", model.ChannelOneBot, model.EventTypeMessageDeleted).Count(&audits)
	if audits == 0 {
		t.Fatal("撤回事件应留 channel_event_audits 证据")
	}
}

// ── 企微智能机器人 ─────────────────────────────────────────────────────

func TestWeCombBotFullChain(t *testing.T) {
	e := newIMPlatformEnv(t)
	rawKey := make([]byte, 32)
	for i := range rawKey {
		rawKey[i] = byte(i + 3)
	}
	key := strings.TrimRight(base64.StdEncoding.EncodeToString(rawKey), "=")
	acc := &model.WeCombBotAccount{AccountName: "wecombot-sim", Token: "wb-t" + "oken", AESKey: key, WebhookURL: "https://example.com/hook", Status: 1}
	if err := e.db.Create(acc).Error; err != nil {
		t.Fatal(err)
	}
	plain := []byte(`{"msgtype":"text","msgid":"wm1","chatid":"chat-1","from":{"userid":"u1","name":"李四"},"text":{"content":"wecombot-hi"},"create_time":1700000000}`)
	cipher, err := wecombot.Encrypt(key, "corp", plain)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"encrypt": cipher})
	ts, nonce := strconv.FormatInt(time.Now().Unix(), 10), "9"
	parts := []string{"wb-token", ts, nonce, cipher}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "")))
	path := fmt.Sprintf("/api/webhook/wecombot/%d?msg_signature=%s&timestamp=%s&nonce=%s",
		acc.ID, hex.EncodeToString(sum[:]), ts, nonce)

	if rec := e.post(t, path, body, nil); rec.Code != http.StatusOK {
		t.Fatalf("wecombot 回调应 200，got %d %s", rec.Code, rec.Body.String())
	}
	e.waitHub(t, model.ChannelWeCombBot, "wecombot-hi")
}

// ── 企业微信客服（回调 + 增量拉取） ──────────────────────────────────

func TestWeComCSFullChain(t *testing.T) {
	e := newIMPlatformEnv(t)
	acc := &model.WeComCSAccount{AccountName: "wecomcs-sim", CorpID: "corp", Secret: "sec", OpenKfID: "kf1", Token: "cs-t" + "oken", Status: 1}
	if err := e.db.Create(acc).Error; err != nil {
		t.Fatal(err)
	}
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			fmt.Fprint(w, `{"errcode":0,"access_token":"tok","expires_in":7200}`)
		case "/cgi-bin/kf/sync_msg":
			fmt.Fprint(w, `{"errcode":0,"next_cursor":"c2","has_more":0,"msg_list":[{"msgid":"m-cs-1","external_userid":"u1","send_time":1700000000,"origin":3,"msgtype":"text","text":{"content":"wecomcs-hi"}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer platform.Close()
	e.svc.SetWeComCSClientFactoryForTest(func() *wecomcs.Client { return wecomcs.NewClient(core.WithBaseURL(platform.URL)) })

	cipher := "callback-cipher"
	body := []byte(`<xml><Encrypt><![CDATA[` + cipher + `]]></Encrypt><MsgType><![CDATA[event]]></MsgType></xml>`)
	ts, nonce := strconv.FormatInt(time.Now().Unix(), 10), "7"
	parts := []string{"cs-token", ts, nonce, cipher}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "")))
	path := fmt.Sprintf("/api/webhook/wecomcs/%d?msg_signature=%s&timestamp=%s&nonce=%s",
		acc.ID, hex.EncodeToString(sum[:]), ts, nonce)

	if rec := e.post(t, path, body, nil); rec.Code != http.StatusOK {
		t.Fatalf("wecomcs 回调应 200，got %d %s", rec.Code, rec.Body.String())
	}
	e.waitHub(t, model.ChannelWeComCS, "wecomcs-hi")
}
