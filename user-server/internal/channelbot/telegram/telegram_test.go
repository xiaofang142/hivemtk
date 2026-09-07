package telegram

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/channelbot/core"
)

// TestSplitMessage 覆盖长度拆分的所有边界场景
func TestSplitMessage(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		limit   int
		wantCnt int
		check   func(t *testing.T, chunks []string)
	}{
		{
			name:    "short text returns single chunk",
			in:      "hello",
			limit:   100,
			wantCnt: 1,
		},
		{
			name:    "long text splits at paragraph boundary",
			in:      strings.Repeat("段落内容。", 1000),
			limit:   500,
			wantCnt: 10,
			check: func(t *testing.T, chunks []string) {
				for i, c := range chunks {
					if len([]rune(c)) > 500 {
						t.Errorf("chunk[%d] len=%d exceeds limit 500", i, len([]rune(c)))
					}
				}
			},
		},
		{
			name:    "long text without paragraph splits at line boundary",
			in:      strings.Repeat("行内容\n", 1000),
			limit:   200,
			wantCnt: 20,
		},
		{
			name:    "long text without line breaks splits at sentence",
			in:      strings.Repeat("句子内容。", 2000),
			limit:   100,
			wantCnt: 100,
		},
		{
			name:    "no newline, no sentence, splits at space",
			in:      strings.Repeat("word ", 500),
			limit:   100,
			wantCnt: 25,
		},
		{
			name:    "no space at all (CJK), falls back to hard cut",
			in:      strings.Repeat("字", 5000),
			limit:   1000,
			wantCnt: 5,
		},
		{
			name:    "empty text returns empty chunks",
			in:      "",
			limit:   100,
			wantCnt: 0,
		},
		{
			name:    "trailing newlines trimmed",
			in:      "hello\n\n\n",
			limit:   100,
			wantCnt: 1,
			check: func(t *testing.T, chunks []string) {
				if chunks[0] != "hello" {
					t.Errorf("expected trimmed 'hello', got %q", chunks[0])
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitMessage(tc.in, tc.limit)
			if len(got) != tc.wantCnt {
				t.Errorf("len(chunks) = %d, want %d (got=%v)", len(got), tc.wantCnt, got)
			}
			if tc.check != nil {
				tc.check(t, got)
			}
			if len(got) > 0 && tc.in != "" {
				joined := strings.Join(got, "")
				if !strings.Contains(strings.TrimSpace(tc.in), strings.TrimSpace(joined[:min(50, len(joined))])) &&
					len(joined) < len(tc.in)-10 {
					t.Errorf("joined chunks len=%d, original len=%d (差值过大)", len(joined), len(tc.in))
				}
			}
		})
	}
}

// TestBuildInlineKeyboard 覆盖内联键盘构建
func TestBuildInlineKeyboard(t *testing.T) {
	cases := []struct {
		name string
		in   [][]InlineButton
		want map[string]any
	}{
		{
			name: "empty returns nil",
			in:   nil,
			want: nil,
		},
		{
			name: "single row, single button callback",
			in: [][]InlineButton{
				{{Text: "确认", CallbackData: "ok"}},
			},
			want: map[string]any{
				"inline_keyboard": [][]map[string]string{
					{{"text": "确认", "callback_data": "ok"}},
				},
			},
		},
		{
			name: "single row, single button url",
			in: [][]InlineButton{
				{{Text: "官网", URL: "https://example.com"}},
			},
			want: map[string]any{
				"inline_keyboard": [][]map[string]string{
					{{"text": "官网", "url": "https://example.com"}},
				},
			},
		},
		{
			name: "URL+Callback 同时存在 → 优先 Callback（URL 被忽略）",
			in: [][]InlineButton{
				{{Text: "A", URL: "https://a", CallbackData: "a_data"}},
			},
			want: map[string]any{
				"inline_keyboard": [][]map[string]string{
					{{"text": "A", "callback_data": "a_data"}},
				},
			},
		},
		{
			name: "Text 为空的按钮被跳过",
			in: [][]InlineButton{
				{{Text: "", CallbackData: "x"}, {Text: "OK", CallbackData: "y"}},
			},
			want: map[string]any{
				"inline_keyboard": [][]map[string]string{
					{{"text": "OK", "callback_data": "y"}},
				},
			},
		},
		{
			name: "单行按钮数 > 8 → 截断到 8",
			in: [][]InlineButton{
				makeRowWithNButtons(10),
			},
			want: map[string]any{
				"inline_keyboard": [][]map[string]string{
					makeKeyboardRow(8),
				},
			},
		},
		{
			name: "总行数 > 100 → 截断到 100",
			in: func() [][]InlineButton {
				rows := make([][]InlineButton, 110)
				for i := range rows {
					rows[i] = []InlineButton{{Text: "R", CallbackData: "r"}}
				}
				return rows
			}(),
			want: func() map[string]any {
				rows := make([][]map[string]string, 100)
				for i := range rows {
					rows[i] = []map[string]string{{"text": "R", "callback_data": "r"}}
				}
				return map[string]any{"inline_keyboard": rows}
			}(),
		},
		{
			name: "所有按钮都被过滤后（空 Text / 无 URL 无 Callback）→ 返回 nil",
			in: [][]InlineButton{
				{{Text: ""}, {Text: "no_data"}},
			},
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildInlineKeyboard(tc.in)
			if tc.want == nil {
				if got != nil {
					t.Errorf("got %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Errorf("got nil, want %v", tc.want)
				return
			}
			gotRows, ok := got["inline_keyboard"].([][]map[string]string)
			if !ok {
				t.Errorf("inline_keyboard type %T, want [][]map[string]string", got["inline_keyboard"])
				return
			}
			wantRows := tc.want["inline_keyboard"].([][]map[string]string)
			if len(gotRows) != len(wantRows) {
				t.Errorf("rows len = %d, want %d", len(gotRows), len(wantRows))
				return
			}
			for i := range gotRows {
				if len(gotRows[i]) != len(wantRows[i]) {
					t.Errorf("row[%d] len = %d, want %d", i, len(gotRows[i]), len(wantRows[i]))
				}
			}
		})
	}
}

func makeRowWithNButtons(n int) []InlineButton {
	row := make([]InlineButton, n)
	for i := 0; i < n; i++ {
		row[i] = InlineButton{Text: "B", CallbackData: "x"}
	}
	return row
}

func makeKeyboardRow(n int) []map[string]string {
	row := make([]map[string]string, n)
	for i := 0; i < n; i++ {
		row[i] = map[string]string{"text": "B", "callback_data": "x"}
	}
	return row
}

// TestParseRetryAfter 覆盖 429 响应解析
func TestParseRetryAfter(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		{"valid 5", `{"ok":false,"parameters":{"retry_after":5}}`, 5},
		{"valid 0", `{"ok":false,"parameters":{"retry_after":0}}`, 0},
		{"missing parameters", `{"ok":false,"description":"Too Many Requests"}`, 0},
		{"empty body", ``, 0},
		{"malformed json", `{not json`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseRetryAfter([]byte(tc.body))
			if got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

// TestSendMessage_RetryOn429 模拟 TG 429 限流，验证 retry_after 退避重试
func TestSendMessage_RetryOn429(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"parameters":{"retry_after":1},"description":"Too Many Requests"}`))
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":42}}`))
	}))
	defer srv.Close()

	c := &Client{
		token:   "test_token",
		apiBase: srv.URL,
	}
	c.BaseClient = core.NewBaseClient(core.WithTimeout(3 * time.Second))

	id, err := c.SendMessage(t.Context(), 123, "hi", SendMessageOptions{ParseMode: ""})
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if id != 42 {
		t.Errorf("got id=%d, want 42", id)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("got %d attempts, want 3 (2 429 + 1 success)", got)
	}
}

// TestSendMessage_NoRetryOn400 模拟 400 业务错误，验证不重试直接返回
func TestSendMessage_NoRetryOn400(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`))
	}))
	defer srv.Close()

	c := &Client{token: "test_token", apiBase: srv.URL}
	c.BaseClient = core.NewBaseClient(core.WithTimeout(2 * time.Second))

	_, err := c.SendMessage(t.Context(), 123, "hi")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("expected 400 error, got: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("expected 1 attempt (no retry on 4xx), got %d", got)
	}
}

// TestSendMessage_RetryOn5xx 模拟 502 错误，验证指数退避重试
func TestSendMessage_RetryOn5xx(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 2 {
			w.WriteHeader(502)
			_, _ = w.Write([]byte(`<html>502 Bad Gateway</html>`))
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":100}}`))
	}))
	defer srv.Close()

	c := &Client{token: "test_token", apiBase: srv.URL}
	c.BaseClient = core.NewBaseClient(core.WithTimeout(3 * time.Second))

	id, err := c.SendMessage(t.Context(), 123, "hi")
	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
	if id != 100 {
		t.Errorf("got id=%d, want 100", id)
	}
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Errorf("expected 2 attempts (1 502 + 1 success), got %d", got)
	}
}

// TestSendMessage_SplitLongMessage 长消息被切分多次发送
func TestSendMessage_SplitLongMessage(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	}))
	defer srv.Close()

	c := &Client{token: "test_token", apiBase: srv.URL}
	c.BaseClient = core.NewBaseClient(core.WithTimeout(2 * time.Second))

	long := strings.Repeat("字", 8000)
	_, err := c.SendMessage(t.Context(), 123, long)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got < 2 {
		t.Errorf("expected ≥2 send calls (split), got %d", got)
	}
}

// TestSendMessage_ExhaustedRetries 持续 5xx 失败应耗尽重试次数并返回错误
func TestSendMessage_ExhaustedRetries(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(503)
		_, _ = w.Write([]byte(`Service Unavailable`))
	}))
	defer srv.Close()

	c := &Client{token: "test_token", apiBase: srv.URL}
	c.BaseClient = core.NewBaseClient(core.WithTimeout(5 * time.Second))

	_, err := c.SendMessage(t.Context(), 123, "hi")
	if err == nil {
		t.Fatal("expected exhausted error, got nil")
	}
	if !strings.Contains(err.Error(), "exhausted 3 retries") {
		t.Errorf("expected exhausted error, got: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("expected 3 attempts (max), got %d", got)
	}
}

// ChatJoinRequest / chat_member 更新必须能被 ParseUpdate 正确解析（此前这两个字段
// 缺失导致方案 A 入口完全失灵，update 被静默丢弃）
func TestParseUpdateChatJoinRequest(t *testing.T) {
	raw := []byte(`{
		"update_id": 1001,
		"chat_join_request": {
			"chat": {"id": -1001234567890, "title": "测试群", "type": "supergroup"},
			"from": {"id": 555, "first_name": "小芳", "username": "xf_user", "is_bot": false},
			"user_chat_id": 555
		}
	}`)
	u, err := ParseUpdate(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if u.ChatJoinRequest == nil {
		t.Fatal("ChatJoinRequest 未解析")
	}
	if u.ChatJoinRequest.From == nil || u.ChatJoinRequest.From.ID != 555 {
		t.Fatalf("from 错误: %+v", u.ChatJoinRequest.From)
	}
	if u.ChatJoinRequest.Chat.ID != -1001234567890 {
		t.Fatalf("chat id 错误: %d", u.ChatJoinRequest.Chat.ID)
	}
}

func TestParseUpdateChatMemberUpdated(t *testing.T) {
	raw := []byte(`{
		"update_id": 1002,
		"chat_member": {
			"chat": {"id": -100123, "type": "supergroup"},
			"from": {"id": 1, "first_name": "a"},
			"old_chat_member": {"status": "left", "user": {"id": 9}},
			"new_chat_member": {"status": "restricted", "user": {"id": 9}, "until_date": 1800000000}
		}
	}`)
	u, err := ParseUpdate(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if u.ChatMember == nil || u.ChatMember.NewChatMember == nil {
		t.Fatal("ChatMember 未解析")
	}
	if u.ChatMember.NewChatMember.Status != "restricted" {
		t.Fatalf("status: %s", u.ChatMember.NewChatMember.Status)
	}
}

func TestParseUpdateMessageStillWorks(t *testing.T) {
	raw := []byte(`{"update_id": 1003, "message": {"message_id": 7, "from": {"id": 42, "first_name": "u"}, "chat": {"id": 42, "type": "private"}, "text": "/start abc"}}`)
	u, err := ParseUpdate(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if u.Message == nil || u.Message.Text != "/start abc" {
		t.Fatalf("message 解析回退失败: %+v", u.Message)
	}
}

// RestrictChatMember 权限全关 + UnrestrictChatMember 恢复：方案 B 禁言/解禁的协议正确性
func TestRestrictAndUnrestrictPayload(t *testing.T) {
	var muPayload map[string]any
	var unPayload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		if strings.Contains(r.URL.Path, "restrictChatMember") {
			if strings.Contains(string(body), `"can_send_messages":true`) {
				_ = json.Unmarshal(body, &unPayload)
			} else {
				_ = json.Unmarshal(body, &muPayload)
			}
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer srv.Close()

	c := &Client{token: "t", apiBase: srv.URL}
	c.BaseClient = core.NewBaseClient(core.WithTimeout(5 * time.Second))

	if err := c.RestrictChatMember(t.Context(), -100, 42, 0); err != nil {
		t.Fatalf("restrict: %v", err)
	}
	if muPayload["until_date"] == float64(0) || muPayload["until_date"] == nil {
		t.Errorf("永久禁言应带兜底 until_date: %v", muPayload["until_date"])
	}
	if perms, ok := muPayload["permissions"].(map[string]any); !ok || perms["can_send_messages"] != false {
		t.Errorf("禁言权限应全关: %v", muPayload["permissions"])
	}

	if err := c.UnrestrictChatMember(t.Context(), -100, 42); err != nil {
		t.Fatalf("unrestrict: %v", err)
	}
	if perms, ok := unPayload["permissions"].(map[string]any); !ok || perms["can_send_messages"] != true {
		t.Errorf("解禁应恢复发言权限: %v", unPayload["permissions"])
	}
}
