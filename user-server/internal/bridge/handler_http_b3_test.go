package bridge

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"

	"github.com/gin-gonic/gin"
)

type fakeOutboxQuerier struct {
	rows []model.MessageHub

	lastSinceID uint64

	firstSinceID   uint64
	firstSinceSeen bool
}

func (f *fakeOutboxQuerier) FetchOutboundSince(_ context.Context, channel, accountID string, sinceID uint64, limit int) ([]model.MessageHub, error) {
	if !f.firstSinceSeen {
		f.firstSinceID = sinceID
		f.firstSinceSeen = true
	}
	f.lastSinceID = sinceID
	out := make([]model.MessageHub, 0, len(f.rows))
	for _, r := range f.rows {
		if r.Platform != channel || r.AccountID != accountID {
			continue
		}
		if uint64(r.ID) <= sinceID {
			continue
		}
		cp := r
		out = append(out, cp)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func b3Rows() []model.MessageHub {
	mk := func(id uint, conv, content string) model.MessageHub {
		return model.MessageHub{
			ID:             id,
			Platform:       "douyin",
			AccountID:      "acc_b3",
			ConversationID: conv,
			Direction:      "outbound",
			Status:         "pending",
			MsgType:        "text",
			Content:        content,
		}
	}
	return []model.MessageHub{mk(1, "conv_a", "m1"), mk(2, "conv_b", "m2"), mk(3, "conv_c", "m3")}
}

// TestOutboxDBFetcher_LastEventIDCursor 增量回放：只返回 id > 游标的 outbound
func TestOutboxDBFetcher_LastEventIDCursor(t *testing.T) {
	q := &fakeOutboxQuerier{rows: b3Rows()}
	f := &outboxDBFetcher{}
	f.SetQuerier(q)
	ctx := context.Background()

	events, newLastID, err := f.FetchOutboxSince(ctx, "douyin", "acc_b3", "1")
	if err != nil {
		t.Fatalf("FetchOutboxSince: %v", err)
	}
	if q.lastSinceID != 1 {
		t.Errorf("游标应透传 sinceID=1, got %d", q.lastSinceID)
	}
	if len(events) != 2 {
		t.Fatalf("应回放 id>1 的 2 条 missed outbound, got %d", len(events))
	}
	if events[0].ID != "2" || events[1].ID != "3" {
		t.Errorf("回放事件应为 id=2,3, got %s,%s", events[0].ID, events[1].ID)
	}
	if newLastID != "3" {
		t.Errorf("newLastID 应为最后一条 id=3, got %q", newLastID)
	}
	for _, ev := range events {
		if ev.Event != "new_outbound" || ev.Data["content"] == nil {
			t.Errorf("回放事件字段不完整: %+v", ev)
		}
	}
}

// TestOutboxDBFetcher_CursorSemantics 游标语义：空游标（首连）→ sinceID=0 全量补齐；
// 非法游标 → B8（2026-09-19）改 fail-closed：querier 根本不被调用（哨兵值不动）、
// 返回 0 行、游标原样回传——旧「退化为全量」会重放已投递消息致重复转发。
func TestOutboxDBFetcher_CursorSemantics(t *testing.T) {
	q := &fakeOutboxQuerier{rows: b3Rows()}
	f := &outboxDBFetcher{}
	f.SetQuerier(q)
	ctx := context.Background()

	events, _, err := f.FetchOutboxSince(ctx, "douyin", "acc_b3", "")
	if err != nil {
		t.Fatalf("no-cursor fetch: %v", err)
	}
	if q.lastSinceID != 0 || len(events) != 3 {
		t.Fatalf("无游标应 sinceID=0 全量返回 3 条, got since=%d n=%d", q.lastSinceID, len(events))
	}

	q.lastSinceID = 99 // 哨兵：非法游标下 querier 不应被触达
	events, newID, err := f.FetchOutboxSince(ctx, "douyin", "acc_b3", "not-a-number")
	if err != nil {
		t.Fatalf("invalid-cursor fetch: %v", err)
	}
	if q.lastSinceID != 99 {
		t.Errorf("非法游标不应触达 querier（哨兵被改写=%d）", q.lastSinceID)
	}
	if len(events) != 0 {
		t.Errorf("非法游标应 0 行，got %d", len(events))
	}
	if newID != "not-a-number" {
		t.Errorf("非法游标应原样回传, got %q", newID)
	}
}

func startB3SSEServer(q *fakeOutboxQuerier) *httptest.Server {
	gin.SetMode(gin.TestMode)
	h := NewBridgeIngestHandler(nil)
	h.SetOutboxQuerier(q)
	h.sseHandler.SetHeartbeat(50 * time.Millisecond)
	h.sseHandler.SetMaxDuration(600 * time.Millisecond)
	r := gin.New()
	r.GET("/api/bridge/outbox/sse", h.HandleOutboxSSE)
	srv := httptest.NewServer(r)
	return srv
}

// TestHandleOutboxSSE_LastEventIDHeaderReplay 断线重连：携带 Last-Event-ID 头
// 只回放缺失的 outbound（避免全量 reload）。
func TestHandleOutboxSSE_LastEventIDHeaderReplay(t *testing.T) {
	q := &fakeOutboxQuerier{rows: b3Rows()}
	srv := startB3SSEServer(q)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/bridge/outbox/sse?channel=douyin&account_id=acc_b3", nil)
	req.Header.Set("Last-Event-ID", "1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("sse request: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	for _, want := range []string{"id: 2\n", "id: 3\n"} {
		if !strings.Contains(bodyStr, want) {
			t.Errorf("响应缺少缺失事件 %s\nbody:\n%s", want, bodyStr)
		}
	}
	if strings.Contains(bodyStr, "id: 1\n") {
		t.Errorf("已投递事件 id=1 不应被回放（增量而非全量）\nbody:\n%s", bodyStr)
	}
	if q.firstSinceID != 1 {
		t.Errorf("服务端首次查询应以 Last-Event-ID=1 为游标, got %d", q.firstSinceID)
	}
}
