// Package whatsapp 封装 WhatsApp Business Cloud API 的主动发消息与被动收消息（Webhook 验签/解析）。
// 采用官方合规路线（Meta Graph API），纯协议层，零外部依赖，可独立开源。
// 业务侧在通信层调用，不入侵核心代码。
package whatsapp

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"hivemtk-user/internal/channelbot/core"
)

const defaultGraphBase = "https://graph.facebook.com"
const defaultAPIVersion = "v21.0"

// CloudClient WhatsApp Cloud API 客户端（官方合规）
type CloudClient struct {
	core.BaseClient
	phoneID     string
	accessToken string
	apiVersion  string
}

// NewCloudClient 创建 WhatsApp Cloud 客户端
func NewCloudClient(phoneID, accessToken string, opts ...core.ClientOption) *CloudClient {
	c := &CloudClient{phoneID: phoneID, accessToken: accessToken, apiVersion: defaultAPIVersion}
	c.BaseClient = core.NewBaseClient(opts...)
	return c
}

func (c *CloudClient) apiBase() string {
	return defaultGraphBase + "/" + c.apiVersion
}

func (c *CloudClient) authHeaders() map[string]string {
	return map[string]string{
		"Authorization": "Bearer " + c.accessToken,
		"Content-Type":  "application/json",
	}
}

// SendText 主动发文本（24h 客服窗口内免费）
func (c *CloudClient) SendText(ctx context.Context, to, text string) (string, error) {
	url := fmt.Sprintf("%s/%s/messages", c.apiBase(), c.phoneID)
	body := map[string]any{
		"messaging_product": "whatsapp",
		"to":                to,
		"type":              "text",
		"text":              map[string]string{"body": text},
	}
	b, _ := json.Marshal(body)
	respB, status, err := c.DoJSON(ctx, http.MethodPost, url, bytes.NewReader(b), c.authHeaders())
	if err != nil {
		return "", fmt.Errorf("wa send: %w", err)
	}
	if status != 200 && status != 201 {
		return "", fmt.Errorf("wa send status %d: %s", status, string(respB))
	}
	return extractWAID(respB)
}

// SendTemplate 主动发模板消息（客服窗口外必须走审核过的模板）
func (c *CloudClient) SendTemplate(ctx context.Context, to, name, language string, components []core.TemplateComponent) (string, error) {
	url := fmt.Sprintf("%s/%s/messages", c.apiBase(), c.phoneID)
	tmpl := map[string]any{"name": name, "language": map[string]string{"code": language}}
	if len(components) > 0 {
		tmpl["components"] = components
	}
	body := map[string]any{
		"messaging_product": "whatsapp",
		"to":                to,
		"type":              "template",
		"template":          tmpl,
	}
	b, _ := json.Marshal(body)
	respB, status, err := c.DoJSON(ctx, http.MethodPost, url, bytes.NewReader(b), c.authHeaders())
	if err != nil {
		return "", fmt.Errorf("wa template: %w", err)
	}
	if status != 200 && status != 201 {
		return "", fmt.Errorf("wa template status %d: %s", status, string(respB))
	}
	return extractWAID(respB)
}

func extractWAID(respB []byte) (string, error) {
	var r struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	_ = json.Unmarshal(respB, &r)
	if len(r.Messages) > 0 {
		return r.Messages[0].ID, nil
	}
	return "", nil
}

// VerifySubscribe 处理 Webhook 订阅的 GET 校验；校验通过返回 challenge
func VerifySubscribe(mode, token, challenge, verifyToken string) (string, bool) {
	if strings.EqualFold(mode, "subscribe") && core.SecureEqual(token, verifyToken) {
		return challenge, true
	}
	return "", false
}

// VerifyWebhook 校验 X-Hub-Signature-256（HMAC-SHA256）。
// appSecret 为空表示未配置，跳过验签（与项目既有行为一致）。
func VerifyWebhook(appSecret string, body []byte, signature string) bool {
	if appSecret == "" {
		return true
	}
	signature = strings.TrimPrefix(signature, "sha256=")
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return core.SecureEqual(expected, signature)
}

// WebhookStatus WhatsApp statuses 回执（T3，ChatbotX 模式移植）。
// Meta 文档：出站消息状态回执按 wamid 推送，status ∈ sent/delivered/read/failed/deleted。
type WebhookStatus struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	Timestamp   string `json:"timestamp"`
	RecipientID string `json:"recipient_id"`
	Errors      []struct {
		Code    int    `json:"code"`
		Title   string `json:"title"`
		Message string `json:"message"`
	} `json:"errors"`
}

// WebhookEvent Meta webhook 推送结构（精简版）
type WebhookEvent struct {
	Object string `json:"object"`
	Entry  []struct {
		ID      string `json:"id"`
		Changes []struct {
			Field string `json:"field"`
			Value struct {
				Metadata struct {
					PhoneNumberID      string `json:"phone_number_id"`
					DisplayPhoneNumber string `json:"display_phone_number"`
				} `json:"metadata"`
				Contacts []struct {
					Profile struct {
						Name string `json:"name"`
					} `json:"profile"`
					WAID string `json:"wa_id"`
				} `json:"contacts"`
				Messages []struct {
					From      string `json:"from"`
					ID        string `json:"id"`
					Timestamp string `json:"timestamp"`
					Type      string `json:"type"`
					Text      struct {
						Body string `json:"body"`
					} `json:"text"`
					Image    *WAMedia `json:"image,omitempty"`
					Audio    *WAMedia `json:"audio,omitempty"`
					Video    *WAMedia `json:"video,omitempty"`
					Document *struct {
						WAMedia
						Filename string `json:"filename"`
					} `json:"document,omitempty"`
					Sticker *WAMedia `json:"sticker,omitempty"`
				} `json:"messages"`
				Statuses []WebhookStatus `json:"statuses"`
			} `json:"value"`
		} `json:"changes"`
	} `json:"entry"`
}

// ParseWebhook 解析 Meta webhook body
func ParseWebhook(body []byte) (*WebhookEvent, error) {
	var ev WebhookEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return nil, fmt.Errorf("wa parse webhook: %w", err)
	}
	return &ev, nil
}

// FirstMessage 取第一条消息（若无则返回 ok=false）
func (e *WebhookEvent) FirstMessage() (from, msgID, msgType, content, name string, ts int64, ok bool) {
	for _, ent := range e.Entry {
		for _, ch := range ent.Changes {
			if len(ch.Value.Messages) == 0 {
				continue
			}
			m := ch.Value.Messages[0]
			contactName := ""
			for _, c := range ch.Value.Contacts {
				if c.WAID == m.From {
					contactName = c.Profile.Name
					break
				}
			}
			tsVal, _ := strconv.ParseInt(m.Timestamp, 10, 64)
			return m.From, m.ID, m.Type, m.Text.Body, contactName, tsVal, true
		}
	}
	return "", "", "", "", "", 0, false
}

// WAMedia WhatsApp 媒体消息公共字段（image/audio/video/document/sticker 共用）
type WAMedia struct {
	MimeType string `json:"mime_type"`
	SHA256   string `json:"sha256"`
	MediaID  string `json:"id"`
}

// WAMessageRef 单条消息的媒体引用视图（供 service 层按消息提取 media_id/mime）。
// Type 取值与 Meta webhook 的 message.type 一致。
type WAMessageRef struct {
	Type     string
	Image    *WAMedia
	Audio    *WAMedia
	Video    *WAMedia
	Sticker  *WAMedia
	Document *struct {
		WAMedia
		Filename string `json:"filename"`
	}
}

// MediaRefs 遍历 webhook 中所有媒体消息，返回引用列表（按出现顺序）。
func (e *WebhookEvent) MediaRefs() []WAMessageRef {
	var out []WAMessageRef
	for _, ent := range e.Entry {
		for _, ch := range ent.Changes {
			for i := range ch.Value.Messages {
				m := &ch.Value.Messages[i]
				if m.Type == "text" || m.Type == "" {
					continue
				}
				ref := WAMessageRef{
					Type:     m.Type,
					Image:    m.Image,
					Audio:    m.Audio,
					Video:    m.Video,
					Sticker:  m.Sticker,
					Document: m.Document,
				}
				out = append(out, ref)
			}
		}
	}
	return out
}

// MediaRef 提取第一条媒体消息的引用（media_id + mime_type + 文件名）。
// 非媒体消息返回 ok=false。
func (e *WebhookEvent) MediaRef() (mediaID, mimeType, filename string, ok bool) {
	for _, ent := range e.Entry {
		for _, ch := range ent.Changes {
			for i := range ch.Value.Messages {
				msg := &ch.Value.Messages[i]
				var ref *WAMedia
				switch msg.Type {
				case "image":
					ref = msg.Image
				case "audio":
					ref = msg.Audio
				case "video":
					ref = msg.Video
				case "sticker":
					ref = msg.Sticker
				case "document":
					if msg.Document != nil {
						return msg.Document.MediaID, msg.Document.MimeType, msg.Document.Filename, true
					}
				}
				if ref != nil && ref.MediaID != "" {
					return ref.MediaID, ref.MimeType, "", true
				}
			}
		}
	}
	return "", "", "", false
}

// ToInbound 归一化为 core.InboundMessage（accountID 由调用方填充）
func (e *WebhookEvent) ToInbound(accountID string) *core.InboundMessage {
	from, msgID, msgType, content, name, ts, ok := e.FirstMessage()
	if !ok {
		return nil
	}
	return &core.InboundMessage{
		Platform:       "whatsapp",
		AccountID:      accountID,
		MessageID:      msgID,
		ConversationID: from,
		SenderID:       from,
		SenderName:     name,
		Content:        content,
		MsgType:        msgType,
		Timestamp:      ts,
	}
}

// Ingress 把解析后的 WA 入站消息经消息中台统一处理（构造 MessageEvent → 调用 HandleIngressMessage）。
// 渠道特有预处理：非文本消息（image/audio/video/document）内容映射为占位符，
// 与原 dispatch 行为一致；msg.ID 作为 EventID 幂等键。
// 遍历 webhook 中的所有消息（entry/changes/messages 可能出现多条），避免漏收。
func (e *WebhookEvent) Ingress(ctx context.Context, h core.IngressHandler, accountID string) error {
	if h == nil {
		return nil
	}
	var firstErr error
	for _, ent := range e.Entry {
		for _, ch := range ent.Changes {
			for _, m := range ch.Value.Messages {
				from := m.From
				msgType := m.Type
				content := m.Text.Body
				name := ""
				for _, c := range ch.Value.Contacts {
					if c.WAID == from {
						name = c.Profile.Name
						break
					}
				}
				if content == "" {
					switch msgType {
					case "image":
						content = "[图片]"
					case "audio":
						content = "[语音]"
					case "video":
						content = "[视频]"
					case "document":
						content = "[文件]"
					default:
						content = "[" + msgType + "]"
					}
				}
				ts, _ := strconv.ParseInt(m.Timestamp, 10, 64)
				inbound := &core.InboundMessage{
					Platform:       "whatsapp",
					AccountID:      accountID,
					MessageID:      m.ID,
					ConversationID: from,
					SenderID:       from,
					SenderName:     name,
					Content:        content,
					MsgType:        msgType,
					Timestamp:      ts,
				}
				if err := h.HandleIngressMessage(ctx, inbound.ToMessageEvent(accountID)); err != nil {
					if firstErr == nil {
						firstErr = err
					}
				}
			}
		}
	}
	return firstErr
}
