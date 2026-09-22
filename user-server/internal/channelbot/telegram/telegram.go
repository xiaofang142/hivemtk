// Package telegram 封装 Telegram Bot API 的主动发消息与被动收消息（Webhook 验签/解析）。
// 纯协议层，零外部依赖，可独立开源。业务侧在通信层调用，不入侵核心代码。
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
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
	// WithBaseURL 注入的测试/代理地址优先于默认 apiBase（与 qq 客户端保持一致）。
	//
	// 历史缺陷：本客户端曾完全不读 BaseClient.BaseURL，于是 core.WithBaseURL
	// 这个文档写着"用于测试或代理"的选项在此**静默失效** —— 调用方以为已改地址、
	// 实际仍打 api.telegram.org。直接后果是 Telegram 群管控链路（telegram_gate.go）
	// 无法在测试中指向 httptest 服务端，happy path 长期零覆盖。
	if c.BaseURL != "" {
		c.apiBase = c.BaseURL
	}
	return c
}

func (c *Client) jsonHeaders() map[string]string {
	return map[string]string{"Content-Type": "application/json; charset=utf-8"}
}

// redactedError 抹掉凭证文本、但保留错误链（Unwrap），这样上游的 errors.Is/errors.As
// 分类不会因为脱敏而失效。
type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }

// scrubToken 从错误串里去掉 bot token。
//
// 官方把长期凭证放在 URL 路径里（/bot<token>/<method>），而标准库的 *url.Error 会把完整
// URL 写进 Error() ⇒ 一次网络抖动就把 token 连同 "Post …" 一起落到日志与观测链路，
// 出站错误串还会进 ChannelError.Raw 一路带到工单文案。
// 本包的 HTTP 出口只有 DoJSON 与 DownloadFile 两处，脱敏就收在这两处。
func (c *Client) scrubToken(err error) error {
	if err == nil || c.token == "" || !strings.Contains(err.Error(), c.token) {
		return err
	}
	return &redactedError{
		msg: strings.ReplaceAll(err.Error(), c.token, "<redacted-token>"),
		err: err,
	}
}

// DoJSON 覆写内嵌 BaseClient.DoJSON：唯一目的就是给错误串脱敏。
// 方法名与内嵌一致 ⇒ 本包所有 c.DoJSON 调用（含 SendMessage / setWebhook / getFile）都走这里。
func (c *Client) DoJSON(ctx context.Context, method, url string, body io.Reader, headers map[string]string) ([]byte, int, error) {
	b, status, err := c.BaseClient.DoJSON(ctx, method, url, body, headers)
	return b, status, c.scrubToken(err)
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
			// 去掉 Markdown 重试的这第二条也是普通请求：频控按账号计，连着两请求同样会撞 429，
			// 这条分支若不带上状态码与 retry_after，上层就退回到"从文案里猜"（审计 N-11③）。
			ra2 := 0
			if status2 == http.StatusTooManyRequests {
				ra2 = parseRetryAfter(respB2)
				if ra2 > 0 {
					wait = time.Duration(ra2)*time.Second + 200*time.Millisecond
				}
			}
			lastErr = apiErr(status2, ra2, fmt.Sprintf("tg send fallback status %d: %s", status2, string(respB2)))
			continue
		}
		if status == 429 {
			ra := parseRetryAfter(respB)
			if ra > 0 {
				wait = time.Duration(ra)*time.Second + 200*time.Millisecond
			}
			// 官方在 parameters.retry_after 里给的就是"还要等多久"，这里带上原值，
			// 上层据此排持久化重试（+200ms 只是本客户端内部循环的安全余量，不是渠道口径）。
			lastErr = apiErr(status, ra, fmt.Sprintf("tg send 429 (rate limited, retry_after=%ds): %s", ra, string(respB)))
			continue
		}
		if status >= 500 && status < 600 {
			lastErr = apiErr(status, 0, fmt.Sprintf("tg send status %d: %s", status, string(respB)))
			continue
		}
		return 0, apiErr(status, 0, fmt.Sprintf("tg send status %d: %s", status, string(respB)))
	}
	return 0, fmt.Errorf("tg send exhausted %d retries: %w", tgSendMaxRetries, lastErr)
}

// apiErr 把一次失败响应拆成跨层可判读的事实。Raw 与本包改动前的错误串完全同形，
// 变化只在于状态码与 retry_after 不再要求上层从字符串里反解（审计 N-11①②）。
// 业务码不进表：官方明写 error_code "contents are subject to change in the future"。
func apiErr(status, retryAfterSec int, raw string) *core.APIError {
	return &core.APIError{
		Channel:    "telegram",
		StatusCode: status,
		RetryAfter: time.Duration(retryAfterSec) * time.Second,
		Raw:        raw,
	}
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

func min(a, b int) int { //nolint:unused //// 仅被 *_test.go 引用，生产路径未用
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
			lastErr = apiErr(status, ra, fmt.Sprintf("tg %s 429 (retry_after=%ds): %s", method, ra, string(respB)))
			continue
		}
		if status >= 500 && status < 600 {
			lastErr = apiErr(status, 0, fmt.Sprintf("tg %s status %d: %s", method, status, string(respB)))
			continue
		}
		return apiErr(status, 0, fmt.Sprintf("tg %s status %d: %s", method, status, string(respB)))
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

// MaxDownloadFileBytes 官方对 bot 可下载文件的上限：「The maximum file size to download is 20 MB」。
const MaxDownloadFileBytes int64 = 20 << 20

// TGFile 官方 File 对象（只声明本仓要用的字段）。
// file_unique_id 官方明示「Can't be used to download or reuse the file」⇒ 不取。
type TGFile struct {
	FileID   string `json:"file_id"`
	FilePath string `json:"file_path"`
	FileSize int64  `json:"file_size"`
}

// GetFile 用 file_id 换取下载路径。官方：file_path 是 Optional —— 没拿到路径就没有可下载的东西，
// 此时必须报错而不是返回空串（空串拼出的 URL 会打到 /file/bot<token>/ 这个不存在的接口上）。
func (c *Client) GetFile(ctx context.Context, fileID string) (*TGFile, error) {
	if strings.TrimSpace(fileID) == "" {
		return nil, fmt.Errorf("tg getFile: empty file_id")
	}
	b, err := json.Marshal(map[string]any{"file_id": fileID})
	if err != nil {
		return nil, fmt.Errorf("tg getFile marshal: %w", err)
	}
	api := fmt.Sprintf("%s/bot%s/getFile", c.apiBase, c.token)
	respB, status, err := c.DoJSON(ctx, http.MethodPost, api, bytes.NewReader(b), c.jsonHeaders())
	if err != nil {
		return nil, fmt.Errorf("tg getFile: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("tg getFile status %d: %s", status, string(respB))
	}
	var r struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		Result      TGFile `json:"result"`
	}
	if err := json.Unmarshal(respB, &r); err != nil {
		return nil, fmt.Errorf("tg getFile parse: %s", string(respB))
	}
	if !r.OK {
		// 只带 description：错误串会进日志，而 URL 里的 token 绝不能跟着进
		return nil, fmt.Errorf("tg getFile not ok: %s", r.Description)
	}
	if r.Result.FilePath == "" {
		return nil, fmt.Errorf("tg getFile 未返回 file_path（官方 Optional）")
	}
	return &r.Result, nil
}

// DownloadFile 下载 getFile 给出的路径。
//
// 不走 core.BaseClient.DoJSON：它 io.ReadAll 不设上限，而这里读的是渠道侧任意大的文件，
// 必须自己 LimitReader(max+1) 并据「正好读到 max+1」判定超限 —— 否则半截文件会被当成完整原件
// 存进长期存储（图片只渲染一半、压缩包直接损坏，且事后看不出少了一段）。
func (c *Client) DownloadFile(ctx context.Context, filePath string, maxBytes int64) (data []byte, contentType string, err error) {
	if err := checkTGFilePath(filePath); err != nil {
		return nil, "", err
	}
	if maxBytes <= 0 {
		maxBytes = MaxDownloadFileBytes
	}
	// file_path 官方形态是 "photos/file_1.jpg" 这样的相对路径，前缀按官方是
	// https://api.telegram.org/file/bot<token>/<file_path>（注意是 /file/bot…，
	// 与接口调用的 /bot<token>/<method> 不是同一路径段）。
	api := fmt.Sprintf("%s/file/bot%s/%s", c.apiBase, c.token, filePath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
	if err != nil {
		return nil, "", c.scrubToken(fmt.Errorf("tg download new request: %w", err))
	}
	cli := c.HTTPClient
	if cli == nil {
		return nil, "", fmt.Errorf("tg download: HTTPClient not initialized")
	}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, "", c.scrubToken(fmt.Errorf("tg download: %w", err))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("tg download status %d", resp.StatusCode)
	}
	buf, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("tg download read: %w", err)
	}
	if int64(len(buf)) > maxBytes {
		return nil, "", fmt.Errorf("tg download file larger than %d bytes", maxBytes)
	}
	return buf, resp.Header.Get("Content-Type"), nil
}

// checkTGFilePath file_path 只有 Optional 与「相对 apiBase/file/bot<token>/ 的路径」两件事是确定的，
// 绝对路径或 ".." 会让拼出来的 URL 跑到别的资源上（下载腿的 base 可被代理配置改写时尤其要紧）。
func checkTGFilePath(p string) error {
	switch {
	case strings.TrimSpace(p) == "":
		return fmt.Errorf("tg download: empty file_path")
	case strings.HasPrefix(p, "/"), strings.HasPrefix(p, "http://"), strings.HasPrefix(p, "https://"):
		return fmt.Errorf("tg download: file_path 必须是相对路径，got %q", p)
	case strings.Contains(p, ".."):
		return fmt.Errorf("tg download: file_path 含上层跳转，got %q", p)
	}
	return nil
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

	// 富媒体容器（M-01）。官方互斥关系决定了下面的判定顺序，不是随手排的：
	// 「Message is an animation… when this field is set, the document field will also be
	// set」「live_photo… when this field is set, the photo field will also be set」
	// ⇒ animation 必须排在 document 前、live_photo 必须排在 photo 前，否则 GIF 会被判成文件、
	// 实况照片会被判成静态图（丢掉动态那一半，且工作台渲染成错的类型）。
	Animation *TGMediaRef      `json:"animation,omitempty"`
	Audio     *TGMediaRef      `json:"audio,omitempty"`
	Document  *TGMediaRef      `json:"document,omitempty"`
	LivePhoto *TGMediaRef      `json:"live_photo,omitempty"`
	Photo     []TGMediaRef     `json:"photo,omitempty"`
	Sticker   *TGMediaRef      `json:"sticker,omitempty"`
	Video     *TGMediaRef      `json:"video,omitempty"`
	VideoNote *TGMediaRef      `json:"video_note,omitempty"`
	Voice     *TGMediaRef      `json:"voice,omitempty"`
	Story     *json.RawMessage `json:"story,omitempty"`
}

// TGMediaRef 官方媒体对象（PhotoSize / Animation / Audio / Document / Video / VideoNote /
// Voice / Sticker）在本仓用到的公共子集：file_id 是唯一必需项（getFile 的入参），
// 其余按对象不同而可选。
//
// 官方 File 对象另带 file_unique_id，原文写明「Can't be used to download or reuse the file」
// ⇒ 不取。file_size 官方警告「It can be bigger than 2^31」⇒ 用 int64。
type TGMediaRef struct {
	FileID   string `json:"file_id"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
	FileSize int64  `json:"file_size,omitempty"`
	FileName string `json:"file_name,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
}

// 落 message_hub.msg_type 的四类媒体取值（与 model.MsgType* 一致；本包不依赖 model，
// 因为归一层是各渠道适配器共同的下游，反向依赖会把协议层拖进业务模型）。
const (
	KindImage = "image"
	KindVideo = "video"
	KindAudio = "audio"
	KindFile  = "file"
	KindText  = "text"
)

// TGInbound 一条消息归一化后的落库形状：类型、正文（含媒体占位符）、待转存的媒体引用。
type TGInbound struct {
	MsgType string
	Content string
	Media   []TGMediaRef
}

// mediaKind 官方容器 → 本仓类型 + 占位符。返回 ok=false 表示这条没有可展示的媒体。
//
// 顺序即官方互斥口径（见 TGMessage 内注释）：animation 先于 document、live_photo 先于 photo。
func (m *TGMessage) mediaKind() (kind, placeholder string, refs []TGMediaRef, ok bool) {
	switch {
	case m.Animation != nil && m.Animation.FileID != "":
		return KindVideo, "[动图]", []TGMediaRef{*m.Animation}, true
	case m.LivePhoto != nil && m.LivePhoto.FileID != "":
		return KindVideo, "[实况照片]", []TGMediaRef{*m.LivePhoto}, true
	case m.Video != nil && m.Video.FileID != "":
		return KindVideo, "[视频]", []TGMediaRef{*m.Video}, true
	case m.VideoNote != nil && m.VideoNote.FileID != "":
		return KindVideo, "[圆视频]", []TGMediaRef{*m.VideoNote}, true
	case m.Voice != nil && m.Voice.FileID != "":
		return KindAudio, "[语音]", []TGMediaRef{*m.Voice}, true
	case m.Audio != nil && m.Audio.FileID != "":
		return KindAudio, "[音频]", []TGMediaRef{*m.Audio}, true
	case m.Sticker != nil && m.Sticker.FileID != "":
		return KindImage, "[表情]", []TGMediaRef{*m.Sticker}, true
	case len(m.Photo) > 0:
		// photo 是「同一张图的多个可用尺寸」（Array of PhotoSize），不是多张图 ⇒ 只取最大那张，
		// 存四份同源字节纯属浪费，且工作台只需要一张能看的。
		return KindImage, "[图片]", []TGMediaRef{largestPhotoSize(m.Photo)}, true
	case m.Document != nil && m.Document.FileID != "":
		name := m.Document.FileName
		if name == "" {
			return KindFile, "[文件]", []TGMediaRef{*m.Document}, true
		}
		return KindFile, "[文件] " + name, []TGMediaRef{*m.Document}, true
	case m.Story != nil:
		// 转发故事：官方 Story 对象只保证 chat/date，其媒体能否按 file_id 取回未在本仓验证过
		// ⇒ 只留可见占位符、不谎称有可下载的原件（media_url 留空即工作台不会渲染破图）。
		return KindText, "[转发故事]", nil, true
	default:
		return "", "", nil, false
	}
}

// largestPhotoSize 取 photo 数组里最大的一档：优先官方 file_size，缺失时退回像素面积
// （PhotoSize 的 file_size 是 Optional，而 width/height 是必填）。
func largestPhotoSize(sizes []TGMediaRef) TGMediaRef {
	best := sizes[0]
	var bestScore int64 = -1
	for _, s := range sizes {
		score := s.FileSize
		if score == 0 {
			score = int64(s.Width) * int64(s.Height)
		}
		if score > bestScore {
			best, bestScore = s, score
		}
	}
	return best
}

// Inbound 归一化这条消息：文本消息取 text，媒体消息取 caption + 占位符。
//
// 官方 caption 的适用范围是「animation, audio, document, paid media, photo, video or voice」，
// 所以媒体那条的正文本来就在 caption 里；占位符一并留着，客户写了说明也看得出「这里有个文件」。
func (m *TGMessage) Inbound() TGInbound {
	if kind, placeholder, refs, ok := m.mediaKind(); ok {
		return TGInbound{MsgType: kind, Content: strings.TrimSpace(m.Caption) + placeholder, Media: refs}
	}
	// 无媒体容器时退回文本；caption 兜底是给「本仓没承载的媒体形态」（如 paid_media）留的：
	// 那种消息官方仍会带 caption，丢了就等于客户说了一句话而没人看见。
	content := m.Text
	if content == "" {
		content = m.Caption
	}
	return TGInbound{MsgType: KindText, Content: content}
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

// AnyMessage 按官方互斥优先序取出本 Update 携带的那条消息（message → edited_message → channel_post）。
// 与 ToInbound 头部原本三级 if 同序；抽出来是为了让「落库哪条消息」与「按哪个键回填媒体」是同一个判断。
func (u *Update) AnyMessage() *TGMessage {
	if u.Message != nil {
		return u.Message
	}
	if u.EditedMessage != nil {
		return u.EditedMessage
	}
	return u.ChannelPost
}

// HubMsgID 这条 Update 落 message_hub 时的 msg_id（= 中台 EventID），与 Ingress 的赋值同序：
// update_id 优先（官方对同一 Update 的重投保持 update_id 不变，天然幂等键），
// 退化到消息自身的 tg_<message_id>。
//
// 媒体回填按这个键找行 ⇒ 它必须与 Ingress 写进去的一致；两处各算一套就会一个键写、另一个键读，
// 长期 URL 永远落不回那行（N-10 在 WhatsApp 侧的同款缺陷）。
//
// accountID 必须进键：官方 update_id 与 message_id 都是**单个 bot 自己**的计数，而 message_hub
// 唯一键是 (platform, msg_id, conversation_id) 不含账号 ⇒ 两个 bot 各自数到同一个号、
// 又落在同一个会话（同一用户在两个 bot 的私聊里 chat id 就是他的 user id）时，
// 第二条消息会被中台当重复事件跳过（实测：日志 "钩子2：msg_id 已存在，幂等跳过" +
// 媒体回填 record not found，客户的话整条蒸发）。与出站 telegramOutboundHubMsgID 同口径。
func (u *Update) HubMsgID(accountID string) string {
	if u.UpdateID != 0 {
		return "tg_upd_" + accountID + "_" + strconv.FormatInt(u.UpdateID, 10)
	}
	if m := u.AnyMessage(); m != nil && m.MessageID != 0 {
		return "tg_" + accountID + "_" + strconv.FormatInt(m.MessageID, 10)
	}
	// 按钮回调没有 message_id（回调消息只带 chat），与 ToInbound 的 MessageID 同键，
	// 否则退化成 dispatch 里的 "tg_0"，与中台落库的行永远对不上。
	if u.CallbackQuery != nil && u.CallbackQuery.ID != "" {
		return "tg_cb_" + accountID + "_" + u.CallbackQuery.ID
	}
	return ""
}

func (u *Update) ToInbound(accountID string) *core.InboundMessage {
	msg := u.AnyMessage()
	if msg == nil {
		if u.CallbackQuery != nil && u.CallbackQuery.From != nil {
			cb := u.CallbackQuery
			chatID := ""
			isGroup := false
			if cb.Message != nil {
				chatID = strconv.FormatInt(cb.Message.Chat.ID, 10)
				chatType := cb.Message.Chat.Type
				isGroup = chatType == "group" || chatType == "supergroup"
			}
			return &core.InboundMessage{
				Platform:       "telegram",
				AccountID:      accountID,
				MessageID:      "tg_cb_" + accountID + "_" + cb.ID,
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
	inb := msg.Inbound()
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
		MessageID:      "tg_" + accountID + "_" + strconv.FormatInt(msg.MessageID, 10),
		ConversationID: chatID,
		SenderID:       senderID,
		SenderName:     name,
		Content:        inb.Content,
		MsgType:        inb.MsgType,
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
	if id := u.HubMsgID(accountID); id != "" {
		event.EventID = id
	}
	return h.HandleIngressMessage(ctx, event)
}
