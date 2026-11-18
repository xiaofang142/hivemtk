// Package onebot 封装 OneBot v11（NapCat/Lagrange）协议：事件解析与 HTTP API 调用。
//
// 纯协议层：只依赖 channelbot/core 与标准库。反向 WebSocket 的连接管理在
// service/onebot_ws.go（需要账号仓储与生命周期），协议解析仍复用本包。
package onebot

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"hivemtk-user/internal/channelbot/core"
	"hivemtk-user/internal/model"
)

// Segment OneBot 消息段（CQ 码的 JSON 形态）。
type Segment struct {
	Type string         `json:"type"`
	Data map[string]any `json:"data"`
}

// Event OneBot v11 上报事件。
type Event struct {
	Time          int64     `json:"time"`
	SelfID        int64     `json:"self_id"`
	PostType      string    `json:"post_type"`    // message | notice | request | meta_event
	MessageType   string    `json:"message_type"` // private | group
	SubType       string    `json:"sub_type"`
	MessageID     int64     `json:"message_id"`
	UserID        int64     `json:"user_id"`
	GroupID       int64     `json:"group_id"`
	RawMessage    string    `json:"raw_message"`
	Message       []Segment `json:"message"`
	NoticeType    string    `json:"notice_type"`
	RequestType   string    `json:"request_type"`
	MetaEventType string    `json:"meta_event_type"`
	OperatorID    int64     `json:"operator_id"`
	TargetID      int64     `json:"target_id"`
	Comment       string    `json:"comment"`
	// MessageSender 发送者资料（群名片优先由调用方回退昵称）。
	Sender struct {
		UserID   int64  `json:"user_id"`
		Nickname string `json:"nickname"`
		Card     string `json:"card"`
	} `json:"sender"`
}

// Client OneBot HTTP API 客户端。
type Client struct {
	core.BaseClient
	accessToken string
}

func NewClient(baseURL, accessToken string, opts ...core.ClientOption) *Client {
	all := append([]core.ClientOption{core.WithBaseURL(strings.TrimRight(baseURL, "/"))}, opts...)
	return &Client{BaseClient: core.NewBaseClient(all...), accessToken: accessToken}
}

// ParseEvent 解析上报 JSON。
func ParseEvent(raw []byte) (*Event, error) {
	var e Event
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil, fmt.Errorf("onebot parse event: %w", err)
	}
	if e.PostType == "" {
		return nil, fmt.Errorf("onebot parse event: missing post_type")
	}
	return &e, nil
}

// VerifyToken 校验上报凭证：Authorization: Bearer <token> 或 ?access_token=。
// token 为空表示账号未配置凭证（由调用方决定是否放行/拒绝）。
func VerifyToken(token, header, query string) bool {
	if token == "" {
		return false
	}
	got := strings.TrimSpace(header)
	if strings.HasPrefix(strings.ToLower(got), "bearer ") {
		got = strings.TrimSpace(got[len("bearer "):])
	}
	if got == "" {
		got = query
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

// MessageText 拼接消息段中的文本（CQ 码字符串不可信，统一从结构化 message 取）。
func (e *Event) MessageText() string {
	if len(e.Message) == 0 {
		return e.RawMessage
	}
	var b strings.Builder
	for _, seg := range e.Message {
		switch seg.Type {
		case "text":
			if s, ok := seg.Data["text"].(string); ok {
				b.WriteString(s)
			}
		case "at":
			// @ 提及本身不进正文；群聊里由发送侧补 @。
		case "image":
			b.WriteString("[图片]")
		case "face":
			b.WriteString("[表情]")
		case "record":
			b.WriteString("[语音]")
		case "video":
			b.WriteString("[视频]")
		case "file":
			b.WriteString("[文件]")
		default:
			b.WriteString("[" + seg.Type + "]")
		}
	}
	if b.Len() == 0 {
		return e.RawMessage
	}
	return b.String()
}

// MessageTypeOf 归一消息类型。
func (e *Event) MessageTypeOf() string {
	if len(e.Message) == 0 {
		return model.MsgTypeText
	}
	for _, seg := range e.Message {
		switch seg.Type {
		case "image":
			return model.MsgTypeImage
		case "record":
			return model.MsgTypeAudio
		case "video":
			return model.MsgTypeVideo
		case "file":
			return model.MsgTypeFile
		}
	}
	return model.MsgTypeText
}

// ToInbound 把上报事件映射为统一入站结构。
//
// post_type 映射：
//
//	message private/group        → message.received
//	notice group_recall          → message.deleted
//	notice group_increase/decrease → group.member_joined / group.member_left
//	request / meta_event         → nil（service 只做旁路事件通知，不自动同意）
func (e *Event) ToInbound(accountID string) *core.InboundMessage {
	switch e.PostType {
	case "message":
		return e.messageInbound(accountID)
	case "notice":
		switch e.NoticeType {
		case "group_recall":
			return &core.InboundMessage{
				Platform:       model.ChannelOneBot,
				AccountID:      accountID,
				MessageID:      "onebot_recall_" + strconv.FormatInt(e.MessageID, 10),
				ConversationID: strconv.FormatInt(e.GroupID, 10),
				SenderID:       strconv.FormatInt(e.UserID, 10),
				Content:        "",
				MsgType:        model.MsgTypeText,
				IsGroup:        true,
				GroupID:        strconv.FormatInt(e.GroupID, 10),
				EventType:      model.EventTypeMessageDeleted,
				Timestamp:      e.Time,
				Extra:          map[string]any{"channel_msg_id": strconv.FormatInt(e.MessageID, 10)},
			}
		case "group_increase", "group_decrease":
			et := model.EventTypeGroupMemberJoined
			if e.NoticeType == "group_decrease" {
				et = model.EventTypeGroupMemberLeft
			}
			return &core.InboundMessage{
				Platform:       model.ChannelOneBot,
				AccountID:      accountID,
				MessageID:      "onebot_" + e.NoticeType + "_" + strconv.FormatInt(e.Time, 10) + "_" + strconv.FormatInt(e.UserID, 10),
				ConversationID: strconv.FormatInt(e.GroupID, 10),
				SenderID:       strconv.FormatInt(e.UserID, 10),
				Content:        "",
				MsgType:        model.MsgTypeText,
				IsGroup:        true,
				GroupID:        strconv.FormatInt(e.GroupID, 10),
				EventType:      et,
				Timestamp:      e.Time,
			}
		}
	}
	return nil
}

func (e *Event) messageInbound(accountID string) *core.InboundMessage {
	senderName := e.Sender.Card
	if senderName == "" {
		senderName = e.Sender.Nickname
	}
	isGroup := e.MessageType == "group"
	conv := strconv.FormatInt(e.UserID, 10)
	groupID := ""
	if isGroup {
		conv = strconv.FormatInt(e.GroupID, 10)
		groupID = conv
	}
	msgID := strconv.FormatInt(e.MessageID, 10)
	return &core.InboundMessage{
		Platform:       model.ChannelOneBot,
		AccountID:      accountID,
		MessageID:      "onebot_" + msgID,
		ConversationID: conv,
		SenderID:       strconv.FormatInt(e.UserID, 10),
		SenderName:     senderName,
		Content:        e.MessageText(),
		MsgType:        e.MessageTypeOf(),
		IsGroup:        isGroup,
		GroupID:        groupID,
		Timestamp:      e.Time,
		Extra: map[string]any{
			"channel_msg_id": msgID,
			"self_id":        strconv.FormatInt(e.SelfID, 10),
			"message_type":   e.MessageType,
		},
	}
}

// apiResp OneBot API 响应外壳。
type apiResp struct {
	Status  string          `json:"status"`
	RetCode int             `json:"retcode"`
	Data    json.RawMessage `json:"data"`
	Echo    string          `json:"echo"`
	Message string          `json:"message"`
	Wording string          `json:"wording"`
}

func (c *Client) call(ctx context.Context, action string, params map[string]any, echo string) (json.RawMessage, error) {
	payload := map[string]any{"action": action, "params": params, "echo": echo}
	body, _ := json.Marshal(payload)
	headers := map[string]string{}
	if c.accessToken != "" {
		headers["Authorization"] = "Bearer " + c.accessToken
	}
	raw, status, err := c.DoJSONBytes(ctx, "POST", c.BaseURL+"/"+action, body, headers)
	if err != nil {
		return nil, err
	}
	var r apiResp
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, &core.APIError{Channel: model.ChannelOneBot, StatusCode: status,
			Raw: fmt.Sprintf("onebot %s decode: %v body=%s", action, err, truncate(string(raw), 200))}
	}
	if r.RetCode != 0 || r.Status == "failed" {
		return nil, &core.APIError{Channel: model.ChannelOneBot, StatusCode: status,
			Code: strconv.Itoa(r.RetCode), Raw: fmt.Sprintf("onebot %s: retcode=%d msg=%s", action, r.RetCode, r.Message+" "+r.Wording)}
	}
	return r.Data, nil
}

// SendPrivateMsg 私聊发送，返回平台 message_id。
func (c *Client) SendPrivateMsg(ctx context.Context, userID int64, msg string) (int64, error) {
	return c.send(ctx, "send_private_msg", map[string]any{"user_id": userID, "message": msg})
}

// SendGroupMsg 群聊发送，返回平台 message_id。
func (c *Client) SendGroupMsg(ctx context.Context, groupID int64, msg string) (int64, error) {
	return c.send(ctx, "send_group_msg", map[string]any{"group_id": groupID, "message": msg})
}

func (c *Client) send(ctx context.Context, action string, params map[string]any) (int64, error) {
	data, err := c.call(ctx, action, params, "hivemtk-"+action)
	if err != nil {
		return 0, err
	}
	var d struct {
		MessageID int64 `json:"message_id"`
	}
	if err := json.Unmarshal(data, &d); err != nil {
		return 0, fmt.Errorf("onebot %s: decode data: %w", action, err)
	}
	return d.MessageID, nil
}

// EscapeCQ 转义 CQ 码控制字符，防止正文里的 [CQ:...] 被协议端当指令执行。
func EscapeCQ(s string) string {
	r := strings.NewReplacer("&", "&amp;", "[", "&#91;", "]", "&#93;")
	return r.Replace(s)
}

// SplitText 按上限切分长文（按 rune 计，保留换行边界优先）。
func SplitText(s string, limit int) []string {
	if limit <= 0 {
		limit = 2000
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return []string{s}
	}
	var out []string
	for len(runes) > limit {
		cut := limit
		for i := limit - 1; i > limit/2; i-- {
			if runes[i] == '\n' {
				cut = i + 1
				break
			}
		}
		out = append(out, string(runes[:cut]))
		runes = runes[cut:]
	}
	if len(runes) > 0 {
		out = append(out, string(runes))
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// BuildWSPayload 反向 WS 发送动作帧（与 HTTP API 同形，供 service 写入连接）。
func BuildWSPayload(action string, params map[string]any, echo string) []byte {
	b, _ := json.Marshal(map[string]any{"action": action, "params": params, "echo": echo})
	return bytes.TrimSpace(b)
}
