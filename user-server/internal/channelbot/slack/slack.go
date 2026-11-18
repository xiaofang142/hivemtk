// Package slack 封装 Slack Events API 验签与 chat.postMessage。
//
// 纯协议层：只依赖 channelbot/core 与标准库。
package slack

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/channelbot/core"
)

const (
	defaultAPIBase = "https://slack.com/api"
	// SignatureWindow Slack 官方要求时间戳在 5 分钟内。
	SignatureWindow = 5 * time.Minute
)

// VerifySignature 校验 Slack 请求签名。
//
// 官方口径：signature = "v0=" + hex(HMAC_SHA256(signing_secret, "v0:"+timestamp+":"+body))。
func VerifySignature(signingSecret, timestamp string, body []byte, signature string, now time.Time) bool {
	if signingSecret == "" || timestamp == "" || signature == "" {
		return false
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(timestamp), 10, 64)
	if err != nil {
		return false
	}
	if now.IsZero() {
		now = time.Now()
	}
	if d := now.Sub(time.Unix(ts, 0)); d > SignatureWindow || d < -SignatureWindow {
		return false
	}
	mac := hmac.New(sha256.New, []byte(signingSecret))
	mac.Write([]byte("v0:" + timestamp + ":"))
	mac.Write(body)
	expected := "v0=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(strings.TrimSpace(signature)))
}

// Client Slack Web API 客户端。
type Client struct {
	core.BaseClient
	botToken string
}

func NewClient(botToken string, opts ...core.ClientOption) *Client {
	all := append([]core.ClientOption{core.WithBaseURL(defaultAPIBase)}, opts...)
	return &Client{BaseClient: core.NewBaseClient(all...), botToken: botToken}
}

type apiResp struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error"`
	TS      string `json:"ts"`
	Channel string `json:"channel"`
}

func (c *Client) call(ctx context.Context, method string, payload map[string]any) (*apiResp, error) {
	body, _ := json.Marshal(payload)
	headers := map[string]string{
		"Authorization": "Bearer " + c.botToken,
		"Content-Type":  "application/json; charset=utf-8",
	}
	raw, status, err := c.DoJSONBytes(ctx, "POST", c.BaseURL+"/"+method, body, headers)
	if err != nil {
		return nil, err
	}
	var r apiResp
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, &core.APIError{Channel: "slack", StatusCode: status,
			Raw: fmt.Sprintf("slack %s decode: %v", method, err)}
	}
	if !r.OK {
		return nil, &core.APIError{Channel: "slack", StatusCode: status, Code: r.Error,
			Raw: fmt.Sprintf("slack %s: %s (status=%d)", method, r.Error, status)}
	}
	return &r, nil
}

// PostMessage 发送消息；threadTS 非空时回复进同一线程。
func (c *Client) PostMessage(ctx context.Context, channel, text, threadTS string) (string, error) {
	payload := map[string]any{"channel": channel, "text": text}
	if threadTS != "" {
		payload["thread_ts"] = threadTS
	}
	r, err := c.call(ctx, "chat.postMessage", payload)
	if err != nil {
		return "", err
	}
	return r.TS, nil
}

// ConversationsOpen 打开与用户的私聊会话，返回 channel id。
func (c *Client) ConversationsOpen(ctx context.Context, users string) (string, error) {
	r, err := c.call(ctx, "conversations.open", map[string]any{"users": users})
	if err != nil {
		return "", err
	}
	return r.Channel, nil
}

// Envelope Events API 外层信封。
type Envelope struct {
	Type      string `json:"type"`
	Challenge string `json:"challenge"`
	EventID   string `json:"event_id"`
	TeamID    string `json:"team_id"`
	Event     struct {
		Type        string `json:"type"`
		Subtype     string `json:"subtype"`
		BotID       string `json:"bot_id"`
		User        string `json:"user"`
		Text        string `json:"text"`
		Channel     string `json:"channel"`
		TS          string `json:"ts"`
		ThreadTS    string `json:"thread_ts"`
		ChannelType string `json:"channel_type"`
		DeletedTS   string `json:"deleted_ts"`
	} `json:"event"`
}

// ParseEnvelope 解析回调信封。
func ParseEnvelope(raw []byte) (*Envelope, error) {
	var e Envelope
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil, fmt.Errorf("slack parse envelope: %w", err)
	}
	return &e, nil
}

// IsBotMessage 机器人自己的消息必须忽略，否则会自问自答。
func (e *Envelope) IsBotMessage() bool {
	return e.Event.BotID != "" || e.Event.Subtype == "bot_message"
}
