package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hivemtk-user/internal/channelbot/core"
)

func TestTelegram_AnswerInteraction_OK(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottest_token/answerCallbackQuery" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer srv.Close()

	c := &Client{token: "test_token", apiBase: srv.URL}
	c.BaseClient = core.NewBaseClient(core.WithTimeout(3 * time.Second))
	if err := c.AnswerInteraction(context.Background(), "cb-1", "已收到", true); err != nil {
		t.Fatalf("answer: %v", err)
	}
	if got["callback_query_id"] != "cb-1" {
		t.Fatalf("callback_query_id 丢失：%+v", got)
	}
	if got["text"] != "已收到" || got["show_alert"] != true {
		t.Fatalf("text/show_alert 未透传：%+v", got)
	}
}

func TestTelegram_AnswerInteraction_PlatformError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ok":false,"description":"query is too old"}`))
	}))
	defer srv.Close()

	c := &Client{token: "test_token", apiBase: srv.URL}
	c.BaseClient = core.NewBaseClient(core.WithTimeout(3 * time.Second))
	err := c.AnswerInteraction(context.Background(), "cb-old", "", false)
	if err == nil {
		t.Fatal("平台返回 ok=false 时必须报错，否则交互行会被错误标成 answered")
	}
}

// TestTelegram_AnswerInteraction_EmptyIDIsNoop 空 id 不请求平台（重投/缺字段时不打无意义请求）。
func TestTelegram_AnswerInteraction_EmptyIDIsNoop(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()
	c := &Client{token: "test_token", apiBase: srv.URL}
	c.BaseClient = core.NewBaseClient(core.WithTimeout(3 * time.Second))
	if err := c.AnswerInteraction(context.Background(), "", "", false); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if called {
		t.Fatal("空 interactionID 不应发起 HTTP 请求")
	}
}

// TestTelegram_CallbackToInbound_IsInteractionEvent 回调必须带 EventType，
// 否则中台会把它当普通文本落库（幽灵消息）。
func TestTelegram_CallbackToInbound_IsInteractionEvent(t *testing.T) {
	u := &Update{
		UpdateID: 99,
		CallbackQuery: &TGCallbackQuery{
			ID:   "cb-9",
			Data: "approve:42",
			From: &TGUser{ID: 7, FirstName: "张三"},
			Message: &TGCallbackMessage{
				MessageID: 555,
				Chat:      TGChat{ID: 8, Type: "private"},
			},
		},
	}
	in := u.ToInbound("1")
	if in == nil {
		t.Fatal("回调不应被丢弃")
	}
	if in.EventType != "interaction.callback" {
		t.Fatalf("EventType 应为 interaction.callback，实际 %q", in.EventType)
	}
	if in.Extra["interaction_id"] != "tg_cb_1_cb-9" {
		t.Fatalf("interaction_id 丢失：%+v", in.Extra)
	}
	if in.Extra["action"] != "approve:42" {
		t.Fatalf("action 丢失：%+v", in.Extra)
	}
	if in.Extra["platform_msg_id"] != "555" {
		t.Fatalf("platform_msg_id 丢失：%+v", in.Extra)
	}
}
