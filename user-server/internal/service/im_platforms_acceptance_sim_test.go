package service

// specs/025 T707/T806/T904：真实账号验收任务的**模拟信号验收**。
//
// 三个 🔵 任务要的是「客户发消息 → 收件箱可见 → AI 回复送达」这条完整事实。
// 入站腿（回调 → 验签 → message_hub）已由 controller 包的
// webhook_im_platforms_fullchain_test.go 用真实路由 + 真实库钉住；这里补的是
// **出站回复腿**：sendOutbound（AI 回复的真实入口）→ 渠道 sender → 平台收件。
//
// 平台侧一律用 httptest 假平台（或反向 WS 回环）替代真实账号，断言平台实际收到的
// 报文内容；真实账号到位后按 tasks.md 的验收清单再补一条人工回放。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/channelbot/core"
	"hivemtk-user/internal/channelbot/wecomcs"
	"hivemtk-user/internal/model"
	dbutil "hivemtk-user/internal/pkg/db"

	"github.com/gorilla/websocket"
)

// acceptanceDB 建库并注册三个验收场景需要的表（账号表 + 落库/回复链路表）。
func acceptanceDB(t *testing.T) *WebhookService {
	t.Helper()
	db := senderTestDB(t)
	// 四个新账号表由 025 平台批新增，senderTestDB 的公共清单还没收录，
	// 这里补注册（表结构与生产一致，都是 AutoMigrate 出来的）。
	if err := db.AutoMigrate(
		&model.WeComCSAccount{}, &model.OneBotAccount{}, &model.SlackAccount{},
		&model.ChannelEventAudit{},
	); err != nil {
		t.Fatalf("迁移验收表: %v", err)
	}
	dbutil.SetTestDB(db)
	t.Cleanup(func() { dbutil.SetTestDB(nil) })

	svc := NewWebhookService()
	t.Cleanup(func() { svc.Stop(context.Background()) })
	return svc
}

// snapshotCalls 拷贝一份已记录的 HTTP 回落路径（诊断用，避免与断言互锁）。
func snapshotCalls(mu *sync.Mutex, calls *[]string) []string {
	mu.Lock()
	defer mu.Unlock()
	return append([]string(nil), *calls...)
}

// replyFor 构造一条「AI 回复」所需的入站事实（hub 行 + payload）。
func replyFor(channel, eventID, conversationID, sender string) (*ParsedPayload, *model.MessageHub) {
	p := &ParsedPayload{EventID: eventID, Sender: sender, ChatID: conversationID, Content: "在吗"}
	hub := &model.MessageHub{
		Platform:       channel,
		ConversationID: conversationID,
		SenderID:       sender,
		Direction:      "inbound",
		MsgType:        model.MsgTypeText,
	}
	return p, hub
}

// ── T707 企业微信客服：回复走 send_msg 且带回 external_userid / open_kfid ──

func TestWeComCSAcceptanceSim(t *testing.T) {
	svc := acceptanceDB(t)

	var mu sync.Mutex
	var sent []map[string]any
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			fmt.Fprint(w, `{"errcode":0,"access_token":"tok-cs","expires_in":7200}`)
		case "/cgi-bin/kf/send_msg":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			sent = append(sent, body)
			mu.Unlock()
			fmt.Fprint(w, `{"errcode":0,"errmsg":"ok","msgid":"s-1"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer platform.Close()
	svc.SetWeComCSClientFactoryForTest(func() *wecomcs.Client {
		return wecomcs.NewClient(core.WithBaseURL(platform.URL))
	})

	acc := &model.WeComCSAccount{
		AccountName: "cs-sim", CorpID: "corp-sim", Secret: "sec-sim",
		OpenKfID: "kf-sim", Token: "cb-token", Status: 1,
	}
	if err := dbutil.GetDB().Create(acc).Error; err != nil {
		t.Fatalf("种账号: %v", err)
	}

	releaseReplyClaimForTest(t, "evt-cs-sim")
	p, hub := replyFor(model.ChannelWeComCS, "evt-cs-sim", "conv-cs", "ext-user-1")
	sentOK, err := svc.sendOutbound(context.Background(), ChannelWeComCS, fmt.Sprint(acc.ID), p, "客服答复", hub, nil)
	if !sentOK || err != nil {
		t.Fatalf("sendOutbound = %v, %v", sentOK, err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 1 {
		t.Fatalf("平台收到 %d 条 send_msg，want 1", len(sent))
	}
	body := sent[0]
	if body["touser"] != "ext-user-1" || body["open_kfid"] != "kf-sim" {
		t.Fatalf("send_msg 收件人错误: %v", body)
	}
	text, _ := body["text"].(map[string]any)
	if text == nil || text["content"] != "客服答复" {
		t.Fatalf("send_msg 正文错误: %v", body)
	}
}

// ── T806 OneBot：群/私聊各一条走反向 WS；断开后回落 HTTP；重连后继续走 WS ──

func TestOneBotAcceptanceSim(t *testing.T) {
	svc := acceptanceDB(t)

	var httpCallsMu sync.Mutex
	var httpCalls []string
	httpAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpCallsMu.Lock()
		httpCalls = append(httpCalls, r.URL.Path)
		httpCallsMu.Unlock()
		fmt.Fprint(w, `{"status":"ok","retcode":0,"data":{"message_id":1}}`)
	}))
	defer httpAPI.Close()

	acc := &model.OneBotAccount{
		AccountName: "ob-sim", SelfID: "10001",
		HTTPBaseURL: httpAPI.URL, AccessToken: "ob-token", Status: 1,
	}
	if err := dbutil.GetDB().Create(acc).Error; err != nil {
		t.Fatalf("种账号: %v", err)
	}

	// 反向 WS：协议端（这里的测试客户端）连上来，服务端把连接登记进管理器。
	// 出站帧方向是「服务端 → 协议端」，所以断言必须在**客户端侧**读——
	// 在服务端 conn 上读只能读到协议端发来的东西，永远等不到出站调用。
	registered := make(chan *OneBotWSConn, 4)
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	wsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		// 必须留下 Register 的返回值：Unregister 只在表内连接指针相同时才摘除，
		// 传 nil 是摘不掉的（管理器会认为「这不是我登记的那个」）。
		registered <- svc.OneBotWSManager().Register(acc.ID, acc.SelfID, conn)
	}))
	defer wsSrv.Close()

	// readFrame 在协议端侧读一帧出站调用；超时即失败，并带上 HTTP 回落诊断
	// （帧没到但 httpCalls 有记录 ⇒ 服务端悄悄回落了 HTTP，是另一种缺陷）。
	readFrame := func(c *websocket.Conn, what string) map[string]any {
		t.Helper()
		_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
		var msg map[string]any
		if err := c.ReadJSON(&msg); err != nil {
			calls := snapshotCalls(&httpCallsMu, &httpCalls)
			t.Fatalf("%s: 5s 内未收到反向 WS 帧（httpCalls=%v）: %v", what, calls, err)
		}
		return msg
	}
	waitConnected := func(want bool) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for svc.OneBotWSManager().Connected(acc.ID) != want && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if svc.OneBotWSManager().Connected(acc.ID) != want {
			t.Fatalf("反向 WS 连接状态未变成 connected=%v", want)
		}
	}

	wsURL := "ws" + strings.TrimPrefix(wsSrv.URL, "http")
	client, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("协议端建连: %v", err)
	}
	defer client.Close()
	waitConnected(true)
	wsConn := <-registered

	// 群聊：group_id 来自 hub，动作必须是 send_group_msg。
	releaseReplyClaimForTest(t, "evt-ob-group")
	pGroup, hubGroup := replyFor(model.ChannelOneBot, "evt-ob-group", "777", "42")
	hubGroup.GroupID = "777"
	if ok, err := svc.sendOutbound(context.Background(), ChannelOneBot, fmt.Sprint(acc.ID), pGroup, "群答复", hubGroup, nil); !ok || err != nil {
		t.Fatalf("群聊 sendOutbound = %v, %v", ok, err)
	}
	got := readFrame(client, "群聊")
	if got["action"] != "send_group_msg" {
		t.Fatalf("群聊 action = %v", got["action"])
	}
	if params, _ := got["params"].(map[string]any); params["group_id"] != float64(777) {
		t.Fatalf("群聊 params = %v", got["params"])
	}

	// 私聊：无 group_id 时用 user_id，动作是 send_private_msg。
	releaseReplyClaimForTest(t, "evt-ob-private")
	pPrivate, hubPrivate := replyFor(model.ChannelOneBot, "evt-ob-private", "42", "42")
	if ok, err := svc.sendOutbound(context.Background(), ChannelOneBot, fmt.Sprint(acc.ID), pPrivate, "私聊答复", hubPrivate, nil); !ok || err != nil {
		t.Fatalf("私聊 sendOutbound = %v, %v", ok, err)
	}
	got = readFrame(client, "私聊")
	if got["action"] != "send_private_msg" {
		t.Fatalf("私聊 action = %v", got["action"])
	}
	if params, _ := got["params"].(map[string]any); params["user_id"] != float64(42) {
		t.Fatalf("私聊 params = %v", got["params"])
	}

	// 断开协议端：管理器注销后出站必须回落 HTTP（不能静默丢消息）。
	client.Close()
	svc.OneBotWSManager().Unregister(acc.ID, wsConn)
	waitConnected(false)

	releaseReplyClaimForTest(t, "evt-ob-fallback")
	pFb, hubFb := replyFor(model.ChannelOneBot, "evt-ob-fallback", "777", "42")
	hubFb.GroupID = "777"
	if ok, err := svc.sendOutbound(context.Background(), ChannelOneBot, fmt.Sprint(acc.ID), pFb, "断线回落", hubFb, nil); !ok || err != nil {
		t.Fatalf("断线回落 sendOutbound = %v, %v", ok, err)
	}
	if calls := snapshotCalls(&httpCallsMu, &httpCalls); len(calls) != 1 || calls[0] != "/send_group_msg" {
		t.Fatalf("断开后未回落 HTTP: %v", calls)
	}

	// 协议端重连：登记新连接后出站必须重新走 WS（T806 判据「断线重连后可继续发」）。
	client2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("协议端重连: %v", err)
	}
	defer client2.Close()
	<-registered
	waitConnected(true)

	releaseReplyClaimForTest(t, "evt-ob-reconnect")
	pRe, hubRe := replyFor(model.ChannelOneBot, "evt-ob-reconnect", "42", "42")
	if ok, err := svc.sendOutbound(context.Background(), ChannelOneBot, fmt.Sprint(acc.ID), pRe, "重连后答复", hubRe, nil); !ok || err != nil {
		t.Fatalf("重连后 sendOutbound = %v, %v", ok, err)
	}
	got = readFrame(client2, "重连后")
	if got["action"] != "send_private_msg" {
		t.Fatalf("重连后 action = %v", got["action"])
	}
	if calls := snapshotCalls(&httpCallsMu, &httpCalls); len(calls) != 1 {
		t.Fatalf("重连后仍走 HTTP（应走 WS）: %v", calls)
	}
}

// ── T904 Slack：回复带 thread_ts 落在原消息线程 ──

func TestSlackAcceptanceSim(t *testing.T) {
	svc := acceptanceDB(t)

	var mu sync.Mutex
	var posted []map[string]any
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat.postMessage" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer xoxb-sim" {
			t.Errorf("Authorization = %q", got)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		posted = append(posted, body)
		mu.Unlock()
		fmt.Fprint(w, `{"ok":true,"ts":"1700000000.0002"}`)
	}))
	defer platform.Close()
	svc.SetSlackAPIBaseForTest(platform.URL)

	acc := &model.SlackAccount{
		AccountName: "slack-sim", TeamID: "T1", BotToken: "xoxb-sim",
		SigningSecret: "s3cret", Status: 1,
	}
	if err := dbutil.GetDB().Create(acc).Error; err != nil {
		t.Fatalf("种账号: %v", err)
	}

	releaseReplyClaimForTest(t, "Ev-sim-reply")
	p, hub := replyFor(model.ChannelSlack, "Ev-sim-reply", "C-sim", "U-sim")
	hub.Extra = map[string]any{"thread_ts": "1700000000.0001"}
	if ok, err := svc.sendOutbound(context.Background(), ChannelSlack, fmt.Sprint(acc.ID), p, "Slack 答复", hub, nil); !ok || err != nil {
		t.Fatalf("sendOutbound = %v, %v", ok, err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(posted) != 1 {
		t.Fatalf("平台收到 %d 条 chat.postMessage，want 1", len(posted))
	}
	body := posted[0]
	if body["channel"] != "C-sim" {
		t.Fatalf("channel = %v", body["channel"])
	}
	if body["thread_ts"] != "1700000000.0001" {
		t.Fatalf("thread_ts = %v（线程回复必须沿用入站线程）", body["thread_ts"])
	}
	if text, _ := body["text"].(string); text != "Slack 答复" {
		t.Fatalf("text = %v", body["text"])
	}
}
