package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

type outboxItem struct {
	Channel        string `json:"channel"`
	AccountID      string `json:"account_id"`
	ConversationID string `json:"conversation_id"`
	MsgID          string `json:"msg_id"`
	Content        string `json:"content"`
	Status         string `json:"status"`
}

type outboxStore struct {
	mu    sync.Mutex
	items map[string]*outboxItem
}

func newOutboxStore() *outboxStore {
	return &outboxStore{items: map[string]*outboxItem{}}
}

func outboxKey(channel, account, msgID string) string {
	return channel + "|" + account + "|" + msgID
}

func (s *outboxStore) push(channel, account, conv, msgID, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[outboxKey(channel, account, msgID)] = &outboxItem{
		Channel: channel, AccountID: account, ConversationID: conv,
		MsgID: msgID, Content: content, Status: "pending",
	}
}

func (s *outboxStore) list(channel, account string) []*outboxItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*outboxItem
	for _, v := range s.items {
		if v.Channel == channel && v.AccountID == account && v.Status == "pending" {
			out = append(out, v)
		}
	}
	return out
}

func (s *outboxStore) ack(channel, account string, msgIDs []string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	set := map[string]bool{}
	for _, m := range msgIDs {
		set[m] = true
	}
	n := 0
	for k, v := range s.items {
		if v.Channel == channel && v.AccountID == account && set[v.MsgID] {
			delete(s.items, k)
			n++
		}
	}
	return n
}

type mockIngestHandler struct {
	mu        sync.Mutex
	seen      map[string]bool
	accepted  int
	duplicate int
	self      int
	store     *outboxStore
}

func newMockIngestHandler(store *outboxStore) *mockIngestHandler {
	return &mockIngestHandler{seen: map[string]bool{}, store: store}
}

func (m *mockIngestHandler) handle(ctx context.Context, ev *model.MessageEvent) (*service.InboxIngressResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := &service.InboxIngressResult{}

	if m.seen[ev.EventID] {
		res.Accepted = false
		res.QueuedForAI = false
		res.Reason = fmt.Sprintf("msg_id already exists (mock): %s", ev.EventID)
		m.duplicate++
		return res, nil
	}
	if ch, ok := ev.Extra["content_hash"].(string); ok && ch != "" && m.seen[ch] {
		res.Accepted = true
		res.QueuedForAI = false
		res.Reason = fmt.Sprintf("content_hash already exists (platform outbound echo): %s", ch)
		m.duplicate++
		return res, nil
	}
	m.seen[ev.EventID] = true
	if ch, ok := ev.Extra["content_hash"].(string); ok && ch != "" {
		m.seen[ch] = true
	}
	res.Accepted = true
	m.accepted++

	if ev.SenderType == "self" || ev.SenderType == "agent" {
		m.self++
		res.QueuedForAI = false
		res.Reason = "sender_type=self; persisted only (平台自己发的不触发 AI)"
		return res, nil
	}

	parts := strings.SplitN(ev.SessionID, ":", 3)
	account := ""
	if len(parts) == 3 {
		account = parts[1]
	}
	res.QueuedForAI = true
	res.Reason = "batched; will be merged and triggered at batch end"
	obID := fmt.Sprintf("ob_%s", ev.EventID)
	m.store.push(ev.Channel, account, ev.ConversationID, obID, "[AI] 回复："+ev.Content)
	m.seen[obID] = true
	return res, nil
}

func newMockIngestHandlerWith(m *mockIngestHandler) *BridgeIngestHandler {
	return NewBridgeIngestHandlerWithMock(m.handle, nil)
}

func writeJSON(c *gin.Context, status int, v interface{}) {
	c.JSON(status, v)
}

func startMockServer() (*mockIngestHandler, string, chan error, func()) {
	errCh := make(chan error, 1)
	gin.SetMode(gin.TestMode)
	store := newOutboxStore()
	m := newMockIngestHandler(store)
	h := newMockIngestHandlerWith(m)
	engine := gin.New()

	engine.POST("/api/bridge/ingest", h.HandleHTTPIngest)

	engine.GET("/api/bridge/outbox", func(c *gin.Context) {
		channel := c.Query("channel")
		account := c.Query("account_id")
		items := store.list(channel, account)
		writeJSON(c, http.StatusOK, map[string]interface{}{"status": "ok", "messages": items})
	})

	engine.POST("/api/bridge/outbox/ack", func(c *gin.Context) {
		var body struct {
			MsgIDs []string `json:"msg_ids"`
		}
		_ = c.ShouldBindJSON(&body)
		channel := c.Query("channel")
		account := c.Query("account_id")
		n := store.ack(channel, account, body.MsgIDs)
		writeJSON(c, http.StatusOK, map[string]interface{}{"status": "ok", "acked": n})
	})

	srv := httptest.NewServer(engine)
	stop := func() { srv.Close() }
	return m, srv.URL, errCh, stop
}

func doIngest(t *testing.T, base, channel, account, conv, eventID, senderType, content string) HTTPIngestResponse {
	t.Helper()
	req := HTTPIngestRequest{
		Channel:        channel,
		AccountID:      account,
		ConversationID: conv,
		Messages: []*HTTPIngestMessage{{
			EventID:        eventID,
			Channel:        channel,
			AccountID:      account,
			ConversationID: conv,
			SenderType:     senderType,
			SenderID:       "u1",
			SenderName:     "用户",
			MsgType:        "text",
			Content:        content,
			Timestamp:      time.Now().Unix(),
		}},
	}
	body, _ := json.Marshal(req)
	url := fmt.Sprintf("%s/api/bridge/ingest?channel=%s&account_id=%s&conversation_id=%s", base, channel, account, conv)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("ingest 请求失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ingest 状态码=%d", resp.StatusCode)
	}
	var res HTTPIngestResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("解析 ingest 响应失败: %v", err)
	}
	return res
}

// 回环去重·钩子2 兜底：AI 出站 MsgID=ob_<eventID>，前端扫描到平台回显重新上报时
// 携带 content_hash=ob_<eventID>（与后端 ContentHashMsgID 同源），即便 event_id 不同，
// 也应被幂等跳过，不二次触发 AI、不重复入下发队列。
func TestBridgeHTTP_OutboundEcho_SkippedByContentHash(t *testing.T) {
	m, base, _, stop := startMockServer()
	defer stop()

	got := doIngest(t, base, ChannelDouyinWeb, "1", "c1", "evt-loop", "customer", "你好")
	if !got.Ingested[0].AIHandled {
		t.Fatalf("用户消息应触发 AI")
	}
	if len(m.store.list(ChannelDouyinWeb, "1")) != 1 {
		t.Fatalf("AI 回复应进入下发队列")
	}

	req := HTTPIngestRequest{
		Channel:        ChannelDouyinWeb,
		AccountID:      "1",
		ConversationID: "c1",
		Messages: []*HTTPIngestMessage{{
			EventID:        "dom-echo-123",
			Channel:        ChannelDouyinWeb,
			AccountID:      "1",
			ConversationID: "c1",
			SenderType:     "customer",
			SenderID:       "u1",
			SenderName:     "用户",
			MsgType:        "text",
			Content:        "你好",
			Timestamp:      time.Now().Unix(),
			ContentHash:    "ob_evt-loop",
		}},
	}
	body, _ := json.Marshal(req)
	url := fmt.Sprintf("%s/api/bridge/ingest?channel=%s&account_id=%s&conversation_id=%s", base, ChannelDouyinWeb, "1", "c1")
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("echo ingest 失败: %v", err)
	}
	var got2 HTTPIngestResponse
	_ = json.NewDecoder(resp.Body).Decode(&got2)
	resp.Body.Close()

	if got2.Ingested[0].AIHandled {
		t.Fatalf("回显消息不应二次触发 AI（回环防护失效）")
	}
	if !strings.Contains(got2.Ingested[0].Reason, "content_hash") {
		t.Fatalf("回显消息应被 content_hash 兜底跳过, 实际 reason=%s", got2.Ingested[0].Reason)
	}
	if len(m.store.list(ChannelDouyinWeb, "1")) != 1 {
		t.Fatalf("回显不应重复入下发队列, 实际 %d", len(m.store.list(ChannelDouyinWeb, "1")))
	}
}

// 重构后：ingest 立即返回，不再同步长轮询 AI 回复；AI 回复进入下发队列（outbox）。
func TestBridgeHTTP_ImmediateReturn_NoLongPoll(t *testing.T) {
	m, base, errCh, stop := startMockServer()
	defer stop()
	select {
	case e := <-errCh:
		t.Fatalf("mock server error: %v", e)
	default:
	}

	got := doIngest(t, base, ChannelDouyinWeb, "1", "c1", "evt-1", "customer", "你好")

	if len(got.Ingested) != 1 || !got.Ingested[0].Accepted {
		t.Fatalf("未接受消息: %+v", got)
	}
	if !got.Ingested[0].AIHandled {
		t.Fatalf("用户消息未触发 AI: %+v", got)
	}
	items := m.store.list(ChannelDouyinWeb, "1")
	if len(items) != 1 {
		t.Fatalf("AI 回复应进入下发队列, 实际: %d", len(items))
	}
}

// 自己发的消息不应触发 AI、也不应进入下发队列。
func TestBridgeHTTP_SelfNoAI(t *testing.T) {
	m, base, _, stop := startMockServer()
	defer stop()

	got := doIngest(t, base, ChannelXHSWeb, "1", "c1", "evt-self", "self", "我刚发的")
	if got.Ingested[0].AIHandled {
		t.Fatalf("自己消息不应触发 AI")
	}
	if len(m.store.list(ChannelXHSWeb, "1")) != 0 {
		t.Fatalf("自己消息不应进入下发队列")
	}
}

// 事件级去重：同一 event_id 二次上报应判重，且不重复进入下发队列。
func TestBridgeHTTP_Dedup(t *testing.T) {
	m, base, _, stop := startMockServer()
	defer stop()

	_ = doIngest(t, base, ChannelTikTok, "1", "c1", "evt-dup", "customer", "hello")
	got2 := doIngest(t, base, ChannelTikTok, "1", "c1", "evt-dup", "customer", "hello")
	if len(got2.Ingested) != 1 || !got2.Ingested[0].Duplicate {
		t.Fatalf("重复事件未判重: %+v", got2)
	}
	if !strings.Contains(got2.Ingested[0].Reason, "already exists") {
		t.Fatalf("判重原因不符: %s", got2.Ingested[0].Reason)
	}
	if len(m.store.list(ChannelTikTok, "1")) != 1 {
		t.Fatalf("去重后下发队列应为 1 条, 实际: %d", len(m.store.list(ChannelTikTok, "1")))
	}
}

// 注册表桥接渠道都能正常上报（2026-11-12 收口：从「五端」扩到注册表全量十渠道，
// 微博/B站/京东/个人微信/拼多多同批纳入——渠道注册一处，mock e2e 全量覆盖）。
func TestBridgeHTTP_MultiChannel(t *testing.T) {
	m, base, _, stop := startMockServer()
	defer stop()

	channels := []string{
		ChannelDouyinWeb, ChannelXHSWeb, ChannelTikTok, ChannelKuaishouWeb, ChannelXianyuWeb,
		ChannelJDWeb, ChannelBilibiliWeb, ChannelWeiboWeb, ChannelPersonalWX, ChannelPDDWeb,
	}
	for i, ch := range channels {
		got := doIngest(t, base, ch, "1", fmt.Sprintf("c%d", i+1),
			fmt.Sprintf("evt-ch-%d", i), "customer", "hi")
		if len(got.Ingested) != 1 || !got.Ingested[0].Accepted {
			t.Fatalf("渠道 %s 上报失败: %+v", ch, got)
		}
	}
	if m.accepted != len(channels) {
		t.Fatalf("应接受 %d 条, 实际 %d", len(channels), m.accepted)
	}
}

// 并发上报全部接受，不丢消息。
func TestBridgeHTTP_Concurrent(t *testing.T) {
	m, base, _, stop := startMockServer()
	defer stop()

	const n = 12
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got := doIngest(t, base, ChannelDouyinWeb, "1", "c1",
				fmt.Sprintf("evt-conc-%d", i), "customer", fmt.Sprintf("m%d", i))
			if len(got.Ingested) != 1 || !got.Ingested[0].Accepted {
				errs <- fmt.Errorf("并发上报失败 idx=%d: %+v", i, got)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	if m.accepted != n {
		t.Fatalf("并发应接受 %d 条, 实际 %d", n, m.accepted)
	}
	if len(m.store.list(ChannelDouyinWeb, "1")) != n {
		t.Fatalf("并发应进入下发队列 %d 条, 实际 %d", n, len(m.store.list(ChannelDouyinWeb, "1")))
	}
}

// 三通道完整闭环：上报 → 下发队列 → 拉取 → 确认 → 队列清空。
// 2026-11-12 收口：除 douyin 基线外，微博/B站两个目标渠道同批钉住同一条闭环。
func TestBridgeOutboxAck_RoundTrip(t *testing.T) {
	for _, ch := range []string{ChannelDouyinWeb, ChannelWeiboWeb, ChannelBilibiliWeb} {
		t.Run(ch, func(t *testing.T) {
			_, base, _, stop := startMockServer()
			defer stop()

			_ = doIngest(t, base, ch, "1", "c1", "evt-rt-"+ch, "customer", "在吗")
			resp, err := http.Get(base + "/api/bridge/outbox?channel=" + ch + "&account_id=1")
			if err != nil {
				t.Fatalf("GET outbox 失败: %v", err)
			}
			var ob struct {
				Status   string       `json:"status"`
				Messages []outboxItem `json:"messages"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&ob)
			resp.Body.Close()
			if len(ob.Messages) != 1 {
				t.Fatalf("outbox 应返回 1 条待发, 实际 %d", len(ob.Messages))
			}
			msgID := ob.Messages[0].MsgID

			ackBody, _ := json.Marshal(map[string]interface{}{"msg_ids": []string{msgID}})
			aresp, err := http.Post(base+"/api/bridge/outbox/ack?channel="+ch+"&account_id=1",
				"application/json", bytes.NewReader(ackBody))
			if err != nil {
				t.Fatalf("POST ack 失败: %v", err)
			}
			var ares struct {
				Status string `json:"status"`
				Acked  int    `json:"acked"`
			}
			_ = json.NewDecoder(aresp.Body).Decode(&ares)
			aresp.Body.Close()
			if ares.Acked != 1 {
				t.Fatalf("ack 应标记 1 条, 实际 %d", ares.Acked)
			}

			resp2, _ := http.Get(base + "/api/bridge/outbox?channel=" + ch + "&account_id=1")
			var ob2 struct {
				Messages []outboxItem `json:"messages"`
			}
			_ = json.NewDecoder(resp2.Body).Decode(&ob2)
			resp2.Body.Close()
			if len(ob2.Messages) != 0 {
				t.Fatalf("ack 后 outbox 应清空, 实际 %d", len(ob2.Messages))
			}
		})
	}
}

// 越权 ack：标记非本账号 msg_id 不应生效（归属校验）。
func TestBridgeOutboxAck_ScopeIsolation(t *testing.T) {
	m, base, _, stop := startMockServer()
	defer stop()

	_ = doIngest(t, base, ChannelDouyinWeb, "1", "c1", "evt-iso", "customer", "hi")
	items := m.store.list(ChannelDouyinWeb, "1")
	if len(items) != 1 {
		t.Fatalf("应进入下发队列 1 条, 实际 %d", len(items))
	}
	msgID := items[0].MsgID

	ackBody, _ := json.Marshal(map[string]interface{}{"msg_ids": []string{msgID}})
	aresp, _ := http.Post(base+"/api/bridge/outbox/ack?channel="+ChannelDouyinWeb+"&account_id=2",
		"application/json", bytes.NewReader(ackBody))
	var ares struct {
		Acked int `json:"acked"`
	}
	_ = json.NewDecoder(aresp.Body).Decode(&ares)
	aresp.Body.Close()
	if ares.Acked != 0 {
		t.Fatalf("越权 ack 不应生效, 实际 %d", ares.Acked)
	}
	if len(m.store.list(ChannelDouyinWeb, "1")) != 1 {
		t.Fatalf("越权 ack 不应清除原账号消息")
	}
}
