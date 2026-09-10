// Package telegram 封装 Telegram Bot API 的主动发消息与被动收消息（Webhook 验签/解析）。
// 纯协议层，零外部依赖，可独立开源。业务侧在通信层调用，不入侵核心代码。
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/channelbot/core"
	"hivemtk-user/internal/pkg/utils/logger"
)

const (
	defaultAPIBase           = "https://api.telegram.org"
	TGMessageMaxLength       = 4096
	TGInlineRowsMax          = 100
	TGInlineButtonsPerRowMax = 8
	tgSendMaxRetries         = 3
	tgSendInitialWait        = 200 * time.Millisecond
	tgSendMaxWait            = 5 * time.Second
)

// Client Telegram Bot API 客户端
type Client struct {
	core.BaseClient
	token   string
	apiBase string
}

// NewClient 创建 Telegram 客户端
func NewTelegramClient(token string, opts ...core.ClientOption) *Client {
	c := &Client{token: token, apiBase: defaultAPIBase}
	c.BaseClient = core.NewBaseClient(opts...)
	return c
}

func (c *Client) jsonHeaders() map[string]string {
	return map[string]string{"Content-Type": "application/json; charset=utf-8"}
}

// SendMessageOptions 主动发消息的可选参数（opts 可变参；零值表示不设置）
type SendMessageOptions struct {
	ReplyToMessageID          int64
	InlineKeyboard            [][]InlineButton
	DisableWebPreview         bool
	ParseMode                 string
	DisableMarkdownConversion bool
}

// InlineButton 内联按钮（URL 与 CallbackData 互斥；同时存在时优先 CallbackData）
type InlineButton struct {
	Text         string
	CallbackData string
	URL          string
}

func (c *Client) SendMessage(ctx context.Context, chatID int64, text string, opts ...SendMessageOptions) (int64, error) {
	opt := SendMessageOptions{}
	if len(opts) > 0 {
		opt = opts[0]
	}
	chunks := splitMessage(text, TGMessageMaxLength)
	if len(chunks) == 0 {
		return 0, fmt.Errorf("empty text")
	}
	var firstID int64
	for i, chunk := range chunks {
		perOpt := opt
		if i > 0 {
			perOpt.ReplyToMessageID = 0
			perOpt.InlineKeyboard = nil
			perOpt.DisableMarkdownConversion = true
		}
		msgID, err := c.sendSingle(ctx, chatID, chunk, perOpt)
		if err != nil {
			return firstID, fmt.Errorf("tg send chunk %d/%d failed: %w", i+1, len(chunks), err)
		}
		if i == 0 {
			firstID = msgID
		}
	}
	return firstID, nil
}

func (c *Client) sendSingle(ctx context.Context, chatID int64, text string, opt SendMessageOptions) (int64, error) {
	body := text
	parseMode := opt.ParseMode
	if parseMode == "" {
		parseMode = "HTML"
	}
	if !opt.DisableMarkdownConversion {
		body = markdownToTelegramHTML(text)
	}
	payload := map[string]any{
		"chat_id":    chatID,
		"text":       body,
		"parse_mode": parseMode,
	}
	if opt.ReplyToMessageID > 0 {
		payload["reply_to_message_id"] = opt.ReplyToMessageID
	}
	if opt.DisableWebPreview {
		payload["disable_web_page_preview"] = true
	}
	if kb := buildInlineKeyboard(opt.InlineKeyboard); kb != nil {
		payload["reply_markup"] = kb
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("tg send marshal: %w", err)
	}
	url := fmt.Sprintf("%s/bot%s/sendMessage", c.apiBase, c.token)

	var lastErr error
	wait := tgSendInitialWait
	for attempt := 0; attempt < tgSendMaxRetries; attempt++ {
		if attempt > 0 {
			if err := sleepCtx(ctx, wait); err != nil {
				return 0, err
			}
			wait *= 2
			if wait > tgSendMaxWait {
				wait = tgSendMaxWait
			}
		}
		respB, status, err := c.DoJSON(ctx, http.MethodPost, url, bytes.NewReader(b), c.jsonHeaders())
		if err != nil {
			lastErr = fmt.Errorf("tg send: %w", err)
			continue
		}
		if status == 200 {
			return parseSendMessageID(respB), nil
		}
		if status == 400 && !opt.DisableMarkdownConversion && strings.Contains(strings.ToLower(string(respB)), "parse entities") {
			payload2 := map[string]any{"chat_id": chatID, "text": text}
			b2, err := json.Marshal(payload2)
			if err != nil {
				return 0, fmt.Errorf("tg send fallback marshal: %w", err)
			}
			respB2, status2, err2 := c.DoJSON(ctx, http.MethodPost, url, bytes.NewReader(b2), c.jsonHeaders())
			if err2 != nil {
				lastErr = fmt.Errorf("tg send fallback: %w", err2)
				continue
			}
			if status2 == 200 {
				return parseSendMessageID(respB2), nil
			}
			lastErr = fmt.Errorf("tg send fallback status %d: %s", status2, string(respB2))
			continue
		}
		if status == 429 {
			ra := parseRetryAfter(respB)
			if ra > 0 {
				wait = time.Duration(ra)*time.Second + 200*time.Millisecond
			}
			lastErr = fmt.Errorf("tg send 429 (rate limited, retry_after=%ds): %s", ra, string(respB))
			continue
		}
		if status >= 500 && status < 600 {
			lastErr = fmt.Errorf("tg send status %d: %s", status, string(respB))
			continue
		}
		return 0, fmt.Errorf("tg send status %d: %s", status, string(respB))
	}
	return 0, fmt.Errorf("tg send exhausted %d retries: %w", tgSendMaxRetries, lastErr)
}

func splitMessage(text string, limit int) []string {
	if limit <= 0 {
		limit = TGMessageMaxLength
	}
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return []string{}
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return []string{text}
	}
	var out []string
	for len(runes) > limit {
		split := -1
		for i := limit; i > 0; i-- {
			if i+1 < len(runes) && runes[i] == '\n' && runes[i+1] == '\n' {
				split = i + 2
				break
			}
		}
		if split == -1 {
			for i := limit; i > 0; i-- {
				if runes[i] == '\n' {
					split = i + 1
					break
				}
			}
		}
		if split == -1 {
			for i := limit; i > 0; i-- {
				if isSentenceSep(runes[i]) {
					split = i + 1
					break
				}
			}
		}
		if split == -1 {
			for i := limit; i > 0; i-- {
				if runes[i] == ' ' {
					split = i + 1
					break
				}
			}
		}
		if split <= 0 {
			split = limit
		}
		head := strings.TrimRight(string(runes[:split]), " \n")
		if head == "" {
			head = string(runes[:split])
		}
		out = append(out, head)
		runes = runes[split:]
	}
	if len(runes) > 0 {
		out = append(out, strings.TrimRight(string(runes), " \n"))
	}
	return out
}

func isSentenceSep(r rune) bool {
	switch r {
	case '。', '！', '？', '\n', '.', '!', '?':
		return true
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func buildInlineKeyboard(rows [][]InlineButton) map[string]any {
	if len(rows) == 0 {
		return nil
	}
	out := make([][]map[string]string, 0, len(rows))
	for i, row := range rows {
		if i >= TGInlineRowsMax {
			break
		}
		btnRow := make([]map[string]string, 0, len(row))
		for j, btn := range row {
			if j >= TGInlineButtonsPerRowMax {
				break
			}
			if btn.Text == "" {
				continue
			}
			entry := map[string]string{"text": btn.Text}
			if btn.CallbackData != "" {
				entry["callback_data"] = btn.CallbackData
			} else if btn.URL != "" {
				entry["url"] = btn.URL
			} else {
				continue
			}
			btnRow = append(btnRow, entry)
		}
		if len(btnRow) > 0 {
			out = append(out, btnRow)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return map[string]any{"inline_keyboard": out}
}

func parseRetryAfter(body []byte) int {
	var r struct {
		Parameters struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	_ = json.Unmarshal(body, &r)
	return r.Parameters.RetryAfter
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

var (
	tgMdInlineCode = regexp.MustCompile("`([^`]+)`")
	tgMdBold       = regexp.MustCompile(`\*\*([^*\n]+?)\*\*`)
	tgMdItalic     = regexp.MustCompile(`\*([^*\n]+?)\*`)
	tgMdLink       = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
)

func markdownToTelegramHTML(md string) string {
	s := html.EscapeString(md)
	s = tgMdInlineCode.ReplaceAllString(s, "<code>$1</code>")
	s = tgMdBold.ReplaceAllString(s, "<b>$1</b>")
	s = tgMdItalic.ReplaceAllString(s, "<i>$1</i>")
	s = tgMdLink.ReplaceAllString(s, `<a href="$2">$1</a>`)
	return s
}

func parseSendMessageID(body []byte) int64 {
	var r struct {
		OK     bool `json:"ok"`
		Result struct {
			MessageID int64 `json:"message_id"`
		} `json:"result"`
	}
	if json.Unmarshal(body, &r) == nil && r.OK {
		return r.Result.MessageID
	}
	return 0
}

// SetWebhook 注册被动收消息回调；secret 用于 X-Telegram-Bot-Api-Secret-Token 验签
//
// allowed_updates 与 GetUpdates 保持一致：覆盖全部 Update 类型（含 channel_post /
// edited_channel_post / inline_query），避免被用作战道管理员或 inline 模式时静默丢消息。
func (c *Client) SetWebhook(ctx context.Context, url, secret string) error {
	api := fmt.Sprintf("%s/bot%s/setWebhook", c.apiBase, c.token)
	payload := map[string]any{
		"url": url,
		"allowed_updates": []string{
			"message",
			"edited_message",
			"channel_post",
			"edited_channel_post",
			"callback_query",
			"inline_query",
			"chosen_inline_result",
			"chat_member",
			"my_chat_member",
			"chat_join_request",
		},
	}
	if secret != "" {
		payload["secret_token"] = secret
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("tg setWebhook marshal: %w", err)
	}
	respB, status, err := c.DoJSON(ctx, http.MethodPost, api, bytes.NewReader(b), c.jsonHeaders())
	if err != nil {
		return fmt.Errorf("tg setWebhook: %w", err)
	}
	if status != 200 {
		return fmt.Errorf("tg setWebhook status %d: %s", status, string(respB))
	}
	return nil
}

// DeleteWebhook 删除 webhook
func (c *Client) DeleteWebhook(ctx context.Context) error {
	api := fmt.Sprintf("%s/bot%s/deleteWebhook", c.apiBase, c.token)
	respB, status, err := c.DoJSON(ctx, http.MethodPost, api, bytes.NewReader([]byte("{}")), c.jsonHeaders())
	if err != nil {
		return fmt.Errorf("tg deleteWebhook: %w", err)
	}
	if status != 200 {
		return fmt.Errorf("tg deleteWebhook status %d: %s", status, string(respB))
	}
	return nil
}

func VerifyWebhook(secret, headerSecret string) bool {
	if secret == "" {

		if os.Getenv("ALLOW_INSECURE_TELEGRAM_WEBHOOK") == "true" {
			logger.Warnf("[telegram] ALLOW_INSECURE_TELEGRAM_WEBHOOK=true 启用，跳过 secret 校验")
			return true
		}
		return false
	}
	return core.SecureEqual(secret, headerSecret)
}

// callMethod 调用任意 Bot API 方法（群管理类接口专用；非 200 返回错误体）。
//
// 可靠性保证：本机到 api.telegram.org 的 TLS 握手在跨境网络下动辄 2~9 秒，
// 单次调用失败率不可忽略。禁言/解禁/踢出这类管理动作一旦静默失败，用户
// 体验直接断裂（入群无人管、验证通过仍被禁言），因此这里对 429/5xx/网络
// 错误做与 sendSingle 同款的指数退避重试；4xx（权限/参数类）不重试，立即返回。
func (c *Client) callMethod(ctx context.Context, method string, payload map[string]any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("tg %s marshal: %w", method, err)
	}
	api := fmt.Sprintf("%s/bot%s/%s", c.apiBase, c.token, method)

	var lastErr error
	wait := tgSendInitialWait
	for attempt := 0; attempt < tgSendMaxRetries; attempt++ {
		if attempt > 0 {
			if err := sleepCtx(ctx, wait); err != nil {
				return err
			}
			wait *= 2
			if wait > tgSendMaxWait {
				wait = tgSendMaxWait
			}
		}
		respB, status, err := c.DoJSON(ctx, http.MethodPost, api, bytes.NewReader(b), c.jsonHeaders())
		if err != nil {
			lastErr = fmt.Errorf("tg %s: %w", method, err)
			continue
		}
		if status == 200 {
			return nil
		}
		if status == 429 {
			ra := parseRetryAfter(respB)
			if ra > 0 {
				wait = time.Duration(ra)*time.Second + 200*time.Millisecond
			}
			lastErr = fmt.Errorf("tg %s 429 (retry_after=%ds): %s", method, ra, string(respB))
			continue
		}
		if status >= 500 && status < 600 {
			lastErr = fmt.Errorf("tg %s status %d: %s", method, status, string(respB))
			continue
		}
		return fmt.Errorf("tg %s status %d: %s", method, status, string(respB))
	}
	return fmt.Errorf("tg %s exhausted %d retries: %w", method, tgSendMaxRetries, lastErr)
}

// ApproveChatJoinRequest 批准入群申请（方案 A）
func (c *Client) ApproveChatJoinRequest(ctx context.Context, chatID, userID int64) error {
	return c.callMethod(ctx, "approveChatJoinRequest", map[string]any{"chat_id": chatID, "user_id": userID})
}

// DeclineChatJoinRequest 拒绝入群申请（方案 A 超时清理）
func (c *Client) DeclineChatJoinRequest(ctx context.Context, chatID, userID int64) error {
	return c.callMethod(ctx, "declineChatJoinRequest", map[string]any{"chat_id": chatID, "user_id": userID})
}

// RestrictChatMember 禁言（方案 B：权限全关；untilDate=0 表示永久）
func (c *Client) RestrictChatMember(ctx context.Context, chatID, userID int64, untilDate int64) error {
	if untilDate <= 0 {
		untilDate = int64(time.Now().Add(365 * 24 * time.Hour).Unix())
	}
	perms := map[string]any{
		"can_send_messages":         false,
		"can_send_polls":            false,
		"can_send_other_messages":   false,
		"can_add_web_page_previews": false,
		"can_change_info":           false,
		"can_invite_users":          false,
		"can_pin_messages":          false,
	}
	return c.callMethod(ctx, "restrictChatMember", map[string]any{
		"chat_id":     chatID,
		"user_id":     userID,
		"permissions": perms,
		"until_date":  untilDate,
	})
}

// tgFullSendPerms 全量发送权限：restrictChatMember 的 permissions 是"未列出的字段一律
// 按 false 处理"，解禁时若只放开文本权限，成员将不能发图/文件/投票/回应（观感=仍被压）。
func tgFullSendPerms() map[string]any {
	return map[string]any{
		"can_send_messages":         true,
		"can_send_audios":           true,
		"can_send_documents":        true,
		"can_send_photos":           true,
		"can_send_videos":           true,
		"can_send_video_notes":      true,
		"can_send_voice_notes":      true,
		"can_send_media_messages":   true,
		"can_send_polls":            true,
		"can_send_other_messages":   true,
		"can_add_web_page_previews": true,
		"can_react_to_messages":     true,
		"can_invite_users":          true,
	}
}

// UnrestrictChatMember 解除禁言（恢复全权限）
func (c *Client) UnrestrictChatMember(ctx context.Context, chatID, userID int64) error {
	// 关键：必须传 until_date。Telegram 语义——不传（0）且权限放开会被解释为
	// "受限至永久"，成员停留在 restricted；官方规则"距当前 <30 秒视为永久"，
	// 故取 now+60s，Telegram 到点自动解除限制（状态回到 member）。
	return c.callMethod(ctx, "restrictChatMember", map[string]any{
		"chat_id":     chatID,
		"user_id":     userID,
		"permissions": tgFullSendPerms(),
		"until_date":  time.Now().Add(60 * time.Second).Unix(),
	})
}

// BanChatMember 踢出成员（untilDate=0 永久拉黑；传过去的时间可只踢不拉黑）
func (c *Client) BanChatMember(ctx context.Context, chatID, userID, untilDate int64) error {
	return c.callMethod(ctx, "banChatMember", map[string]any{"chat_id": chatID, "user_id": userID, "until_date": untilDate})
}

// UnbanChatMember 解除拉黑（banOnlyIfBanned=true 时仅对被拉黑者生效）
func (c *Client) UnbanChatMember(ctx context.Context, chatID, userID int64) error {
	return c.callMethod(ctx, "unbanChatMember", map[string]any{"chat_id": chatID, "user_id": userID, "only_if_banned": true})
}

// GetChatMember 查询成员状态
func (c *Client) GetChatMember(ctx context.Context, chatID, userID int64) (map[string]any, error) {
	b, err := json.Marshal(map[string]any{"chat_id": chatID, "user_id": userID})
	if err != nil {
		return nil, fmt.Errorf("tg getChatMember marshal: %w", err)
	}
	api := fmt.Sprintf("%s/bot%s/getChatMember", c.apiBase, c.token)
	respB, status, err := c.DoJSON(ctx, http.MethodPost, api, bytes.NewReader(b), c.jsonHeaders())
	if err != nil {
		return nil, fmt.Errorf("tg getChatMember: %w", err)
	}
	if status != 200 {
		return nil, fmt.Errorf("tg getChatMember status %d: %s", status, string(respB))
	}
	var r struct {
		OK     bool           `json:"ok"`
		Result map[string]any `json:"result"`
	}
	if err := json.Unmarshal(respB, &r); err != nil || !r.OK {
		return nil, fmt.Errorf("tg getChatMember parse: %s", string(respB))
	}
	return r.Result, nil
}

// CreateChatInviteLink 创建一次性入群邀请链接（createsJoinRequest 可与方案 A 配合）
func (c *Client) CreateChatInviteLink(ctx context.Context, chatID int64, createsJoinRequest bool) (string, error) {
	payload := map[string]any{"chat_id": chatID}
	if createsJoinRequest {
		payload["creates_join_request"] = true
	} else {
		payload["member_limit"] = 1
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("tg createChatInviteLink marshal: %w", err)
	}
	api := fmt.Sprintf("%s/bot%s/createChatInviteLink", c.apiBase, c.token)
	respB, status, err := c.DoJSON(ctx, http.MethodPost, api, bytes.NewReader(b), c.jsonHeaders())
	if err != nil {
		return "", fmt.Errorf("tg createChatInviteLink: %w", err)
	}
	if status != 200 {
		return "", fmt.Errorf("tg createChatInviteLink status %d: %s", status, string(respB))
	}
	var r struct {
		OK     bool `json:"ok"`
		Result struct {
			InviteLink string `json:"invite_link"`
		} `json:"result"`
	}
	if err := json.Unmarshal(respB, &r); err != nil || !r.OK {
		return "", fmt.Errorf("tg createChatInviteLink parse: %s", string(respB))
	}
	return r.Result.InviteLink, nil
}

// Update Telegram webhook 推送的 Update 结构（精简版）
type Update struct {
	UpdateID        int64                `json:"update_id"`
	Message         *TGMessage           `json:"message"`
	EditedMessage   *TGMessage           `json:"edited_message"`
	ChannelPost     *TGMessage           `json:"channel_post"`
	CallbackQuery   *TGCallbackQuery     `json:"callback_query,omitempty"`
	ChatJoinRequest *TGChatJoinRequest   `json:"chat_join_request,omitempty"`
	ChatMember      *TGChatMemberUpdated `json:"chat_member,omitempty"`
	MyChatMember    *TGChatMemberUpdated `json:"my_chat_member,omitempty"`
}

// TGChatJoinRequest 加群申请（群组开启 "申请加入" 后由 Telegram 推送）
type TGChatJoinRequest struct {
	Chat       *TGChat       `json:"chat"`
	From       *TGUser       `json:"from"`
	UserChatID int64         `json:"user_chat_id"` // 用户与 Bot 的私聊 chat_id
	InviteLink *TGInviteLink `json:"invite_link,omitempty"`
}

// TGInviteLink 邀请链接信息
type TGInviteLink struct {
	InviteLink string  `json:"invite_link"`
	Creator    *TGUser `json:"creator,omitempty"`
}

// TGChatMemberUpdated 成员状态变更（joined/kicked/restricted 等）
type TGChatMemberUpdated struct {
	Chat          *TGChat       `json:"chat"`
	From          *TGUser       `json:"from"`
	NewChatMember *TGChatMember `json:"new_chat_member"`
	OldChatMember *TGChatMember `json:"old_chat_member,omitempty"`
}

// TGChatMember 成员状态与权限
type TGChatMember struct {
	Status          string  `json:"status"` // creator/administrator/member/restricted/left/kicked
	User            *TGUser `json:"user"`
	UntilDate       int64   `json:"until_date,omitempty"`
	CanSendMessages *bool   `json:"can_send_messages,omitempty"`
	IsMember        *bool   `json:"is_member,omitempty"`
}

// TGCallbackQuery 回调查询
type TGCallbackQuery struct {
	ID      string             `json:"id"`
	Data    string             `json:"data"`
	From    *TGUser            `json:"from"`
	Message *TGCallbackMessage `json:"message,omitempty"`
}

// TGCallbackMessage 回调所属消息（精简）
type TGCallbackMessage struct {
	Chat TGChat `json:"chat"`
}

// TGMessage 消息
type TGMessage struct {
	MessageID      int64      `json:"message_id"`
	From           *TGUser    `json:"from"`
	Chat           *TGChat    `json:"chat"`
	Text           string     `json:"text"`
	Caption        string     `json:"caption"`
	Date           int64      `json:"date"`
	Entities       []TGEntity `json:"entities,omitempty"`
	ReplyToMessage *TGMessage `json:"reply_to_message,omitempty"`
	NewChatMembers []TGUser   `json:"new_chat_members,omitempty"`
	LeftChatMember *TGUser    `json:"left_chat_member,omitempty"`
	NewChatTitle   string     `json:"new_chat_title,omitempty"`
}

// TGEntity 消息内格式化实体（mention / text_mention / bot_command 等），用于精确识别 @提及
type TGEntity struct {
	Type   string  `json:"type"`
	Offset int     `json:"offset"`
	Length int     `json:"length"`
	User   *TGUser `json:"user,omitempty"`
}

// TGUser 用户
type TGUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
	IsBot     bool   `json:"is_bot"`
}

// TGChat 会话
type TGChat struct {
	ID       int64  `json:"id"`
	Type     string `json:"type"`
	Title    string `json:"title"`
	UserName string `json:"username"`
}

// ParseUpdate 解析 Telegram webhook body
func ParseUpdate(body []byte) (*Update, error) {
	var u Update
	if err := json.Unmarshal(body, &u); err != nil {
		return nil, fmt.Errorf("tg parse update: %w", err)
	}
	return &u, nil
}

// ToInbound 归一化为 core.InboundMessage（accountID 由调用方填充）
func (u *Update) ToInbound(accountID string) *core.InboundMessage {
	msg := u.Message
	if msg == nil {
		msg = u.EditedMessage
	}
	if msg == nil {
		msg = u.ChannelPost
	}
	if msg == nil {
		if u.CallbackQuery != nil && u.CallbackQuery.From != nil {
			cb := u.CallbackQuery
			chatID := ""
			chatType := "private"
			isGroup := false
			if cb.Message != nil {
				chatID = strconv.FormatInt(cb.Message.Chat.ID, 10)
				chatType = cb.Message.Chat.Type
				isGroup = chatType == "group" || chatType == "supergroup"
			}
			return &core.InboundMessage{
				Platform:       "telegram",
				AccountID:      accountID,
				MessageID:      "tg_cb_" + cb.ID,
				ConversationID: chatID,
				SenderID:       strconv.FormatInt(cb.From.ID, 10),
				SenderName:     cb.From.FirstName,
				Content:        "/callback " + cb.Data,
				MsgType:        "text",
				IsGroup:        isGroup,
				GroupID:        chatID,
			}
		}
		return nil
	}
	content := msg.Text
	if content == "" {
		content = msg.Caption
	}
	name := ""
	var senderID string
	if msg.From != nil {
		name = msg.From.FirstName
		if msg.From.Username != "" {
			name = msg.From.Username
		}
		senderID = strconv.FormatInt(msg.From.ID, 10)
	}
	var chatID, groupID, groupName string
	var isGroup bool
	if msg.Chat != nil {
		chatID = strconv.FormatInt(msg.Chat.ID, 10)
		groupID = chatID
		groupName = msg.Chat.Title
		isGroup = msg.Chat.Type == "group" || msg.Chat.Type == "supergroup"
	}
	return &core.InboundMessage{
		Platform:       "telegram",
		AccountID:      accountID,
		MessageID:      "tg_" + strconv.FormatInt(msg.MessageID, 10),
		ConversationID: chatID,
		SenderID:       senderID,
		SenderName:     name,
		Content:        content,
		MsgType:        "text",
		IsGroup:        isGroup,
		GroupID:        groupID,
		GroupName:      groupName,
		Timestamp:      msg.Date,
	}
}

// Ingress 把解析后的 TG 入站消息经消息中台统一处理（构造 MessageEvent → 调用 HandleIngressMessage）。
// 渠道特有预处理：TG 用 update_id 作为 EventID 幂等键（同一 Update 重投时 update_id 不变，
// 中台据 EventID 落库 message_hub 并依赖唯一约束去重）。
// 调用方负责在调用前完成 webhook 验签；群入退群事件等系统事件由上层 dispatch 单独处理，不走本方法。
func (u *Update) Ingress(ctx context.Context, h core.IngressHandler, accountID string) error {
	if h == nil {
		return nil
	}
	inbound := u.ToInbound(accountID)
	if inbound == nil {
		return nil
	}
	event := inbound.ToMessageEvent(accountID)
	if u.UpdateID != 0 {
		event.EventID = "tg_upd_" + strconv.FormatInt(u.UpdateID, 10)
	}
	return h.HandleIngressMessage(ctx, event)
}
