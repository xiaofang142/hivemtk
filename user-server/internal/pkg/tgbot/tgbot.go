// Package tgbot 封装 Telegram Bot API 调用。
//
// 设计说明：
//   - 本包仅提供无状态的工具函数（setWebhook / sendMessage / 创建邀请链接等）
//   - Bot 实例和长连接由调用方按需创建，避免在工具包中持有全局状态
//   - 严格遵守五层架构：工具层不调用 Service / Repository
package tgbot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"hivemtk-user/internal/channelbot/core"
	"hivemtk-user/internal/channelbot/telegram"
	"hivemtk-user/internal/config"
	"hivemtk-user/internal/pkg/httpclient"
	"hivemtk-user/internal/pkg/utils/logger"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"golang.org/x/net/proxy"
)

// InitTGBot 初始化 Bot API 客户端（长连接场景使用，例如 polling）
// 已不再被 webhook 模式使用，仅保留以兼容现有调用方
func InitTGBot(botToken string, groupID int64, proxyEnabled bool, proxyProto string, proxyHost string, proxyPort int) (*tgbotapi.BotAPI, error) {
	client := httpclient.New()

	if proxyEnabled {
		tgProxyURL, err := url.Parse(fmt.Sprintf("%s://%s:%d", proxyProto, proxyHost, proxyPort))
		if err != nil {
			return nil, fmt.Errorf("Failed to parse proxy: %s", err)
		}
		tgDialer, err := proxy.FromURL(tgProxyURL, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("Failed to obtain proxy dialer: %s", err)
		}
		tgTransport := &http.Transport{
			Dial: tgDialer.Dial,
		}
		client.Transport = tgTransport
	}
	var err error
	var Bot *tgbotapi.BotAPI
	Bot, err = tgbotapi.NewBotAPIWithClient(botToken, "https://api.telegram.org/bot%s/%s", client)
	if err != nil {
		return nil, err
	}
	if groupID != 0 {
		isAdmin, err := isBotAdmin(Bot, groupID)
		if err != nil {
			return nil, err
		}
		if !isAdmin {
			return nil, errors.New("机器人不是群组管理员")
		}
	}
	return Bot, nil
}

// SendTgMsg 通过既有的 Bot 实例发送消息
func SendTgMsg(Bot *tgbotapi.BotAPI, msgText string, TgID int64) error {
	msg := tgbotapi.NewMessage(TgID, msgText)
	msg.DisableWebPagePreview = true
	msg.ParseMode = "HTML"
	_, err := Bot.Send(msg)
	return err
}

// DeleteMsg 删除消息
func DeleteMsg(Bot *tgbotapi.BotAPI, chatID int64, msgID int) error {
	deleteConfig := tgbotapi.DeleteMessageConfig{
		ChatID:    chatID,
		MessageID: msgID,
	}
	if _, err := Bot.Request(deleteConfig); err != nil {
		return err
	}
	return nil
}

func isBotAdmin(Bot *tgbotapi.BotAPI, groupID int64) (bool, error) {
	admins, err := Bot.GetChatAdministrators(tgbotapi.ChatAdministratorsConfig{ChatConfig: tgbotapi.ChatConfig{ChatID: groupID}})
	if err != nil {
		return false, err
	}
	for _, admin := range admins {
		if admin.User.ID == Bot.Self.ID {
			if admin.CanInviteUsers {
				return true, nil
			}
			return false, errors.New("机器人没有邀请权限")
		}
	}
	return false, errors.New("机器人不是群组管理员")
}

// CreateInviteLink 通过 Bot 实例创建邀请链接
func CreateInviteLink(Bot *tgbotapi.BotAPI, inviteTgID int64, duration time.Duration) (string, error) {
	var resMap map[string]any
	inviteLinkConfig := tgbotapi.CreateChatInviteLinkConfig{
		ChatConfig:         tgbotapi.ChatConfig{ChatID: inviteTgID},
		Name:               "邀请进群",
		ExpireDate:         int(time.Now().Add(duration).Unix()),
		MemberLimit:        1,
		CreatesJoinRequest: false,
	}
	response, err := Bot.Request(inviteLinkConfig)
	if err != nil {
		return "", errors.New("创建邀请链接失败: 请求错误")
	}
	resMap = map[string]any{}
	if err := json.Unmarshal(response.Result, &resMap); err != nil {
		return "", errors.New("创建邀请链接失败: 响应解析错误")
	}
	link, ok := resMap["invite_link"].(string)
	if !ok {
		return "", errors.New("创建邀请链接失败: 响应缺少 invite_link 字段")
	}
	return link, nil
}

// SendInviteJoinGroup 通过 Bot 实例发送邀请链接给用户
func SendInviteJoinGroup(Bot *tgbotapi.BotAPI, groupID int64, userTgID int64) error {
	inviteLinkDuration := time.Minute * 3
	inviteLink, err := CreateInviteLink(Bot, groupID, inviteLinkDuration)
	if err != nil {
		return err
	}
	sendText := fmt.Sprintf("<a href=\"%s\">点击进群(180秒内有效)</a>", inviteLink)
	msg := tgbotapi.NewMessage(userTgID, sendText)
	msg.DisableWebPagePreview = true
	msg.ParseMode = "HTML"
	_, err = Bot.Send(msg)

	return nil
}

// UnbanUser 通过 Bot 实例解封用户
func UnbanUser(Bot *tgbotapi.BotAPI, chatID int64, userID int64) error {
	unbanConfig := tgbotapi.UnbanChatMemberConfig{
		ChatMemberConfig: tgbotapi.ChatMemberConfig{
			ChatID: chatID,
			UserID: userID,
		},
		OnlyIfBanned: true,
	}
	_, err := Bot.Request(unbanConfig)
	return err
}

var defaultHTTPClient = &http.Client{
	Timeout:   15 * time.Second,
	Transport: config.GetProxyTransport(),
}

func callBotAPI(botToken, method string, params url.Values) (map[string]any, error) {
	if botToken == "" {
		return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN 未配置,无法调用 Telegram Bot API")
	}
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/%s", botToken, method)
	resp, err := defaultHTTPClient.PostForm(apiURL, params)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("调用 %s 失败: %w", method, err)
	}
	body, _ := io.ReadAll(resp.Body)
	var result struct {
		OK          bool           `json:"ok"`
		Result      map[string]any `json:"result"`
		Description string         `json:"description"`
		ErrorCode   int            `json:"error_code"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if !result.OK {
		return nil, fmt.Errorf("Telegram API 错误(%d): %s", result.ErrorCode, result.Description)
	}
	return result.Result, nil
}

func maskToken(t string) string {
	if len(t) <= 8 {
		return "***"
	}
	return t[:4] + "****" + t[len(t)-4:]
}

// Telegram Bot Token 格式约束（依据 Telegram Bot API 官方文档）：
//   - 形如 `<bot_id>:<token>`，例如 `123456789:AAEhBOweik6ad9JhbY4M9M3PqkfK1M3Pqkf`
//   - bot_id：6~10 位数字
//   - ":" 分隔符（1 个）
//   - token：35 位字符，范围 [A-Za-z0-9_-]
//   - 总长：43~46（不含 "bot" 前缀，纯 token 字段）
//
// ValidateBotToken 校验 Bot Token 格式（仅做语法校验，不调用 Telegram API）。
// 用于 Create/Update controller 入口预校验，避免用户填错格式后 getMe 报错信息不友好。
// 返回的 error 携带人类可读的原因，可直接 4xx 回包。
func ValidateBotToken(token string) error {
	if token == "" {
		return fmt.Errorf("bot_token 不能为空")
	}
	colonIdx := strings.IndexByte(token, ':')
	if colonIdx <= 0 {
		return fmt.Errorf("bot_token 格式错误：缺少 ':' 分隔符（应形如 <bot_id>:<token>）")
	}
	if strings.Count(token, ":") != 1 {
		return fmt.Errorf("bot_token 格式错误：':' 出现多次")
	}
	botID := token[:colonIdx]
	tok := token[colonIdx+1:]

	for i := 0; i < len(botID); i++ {
		if botID[i] < '0' || botID[i] > '9' {
			return fmt.Errorf("bot_token 格式错误：bot_id 部分含非数字字符")
		}
	}
	if len(botID) < 6 || len(botID) > 10 {
		return fmt.Errorf("bot_token 格式错误：bot_id 长度 %d 不在 6~10 范围内", len(botID))
	}

	if len(tok) != 35 {
		return fmt.Errorf("bot_token 格式错误：token 长度 %d 不等于 35（Telegram 官方 token 固定 35 位），请到 @BotFather 重新复制完整 token", len(tok))
	}
	for i := 0; i < len(tok); i++ {
		c := tok[i]
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '-') {
			return fmt.Errorf("bot_token 格式错误：第 %d 位字符 %q 不在 [A-Za-z0-9_-] 范围内", i+1, c)
		}
	}
	return nil
}

// FriendlyTGAPIError 把 Telegram Bot API 原始报错翻译成运维可读的中文提示+修复建议。
// 兜底原样返回，绝不静默吞掉。
func FriendlyTGAPIError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "401"):
		return fmt.Errorf("Bot Token 无效（Telegram 返回 401 Unauthorized）。请到 Telegram 的 @BotFather → /mybots → API Token 重新生成并完整粘贴")
	case strings.Contains(msg, "404"):
		return fmt.Errorf("Bot 不存在（Telegram 返回 404 Not Found）。Token 可能已被 revoke，请到 @BotFather 重新生成")
	case strings.Contains(msg, "https url"):
		return fmt.Errorf("Webhook URL 必须是 https:// 开头（Telegram 强制要求），请修改后再试")
	case strings.Contains(msg, "Failed to resolve host") || strings.Contains(msg, "name resolution"):
		return fmt.Errorf("Telegram 无法解析 Webhook 域名（DNS 失败）：确认域名已生效、公网可访问，且必须是公网域名（不能用内网地址）")
	case strings.Contains(msg, "getaddrinfo") || strings.Contains(msg, "no such host") || strings.Contains(msg, "connection refused"):
		return fmt.Errorf("Webhook 地址不可达：Telegram 服务器无法访问该域名，请检查域名解析/证书/反向代理")
	case strings.Contains(msg, "429"):
		return fmt.Errorf("请求过于频繁（Telegram 限流），请稍后重试")
	}
	return err
}

// SetWebhook 注册 Telegram Webhook
// webhookURL 形如：https://your-domain/api/webhook/telegram/{account_id}
// secret 由 Telegram 在 X-Telegram-Bot-Api-Secret-Token 头中回传，用于验签。
// 统一走 channelbot/telegram.Client（与出站发消息同一套 Bot API 实现）。
func SetWebhook(botToken, webhookURL, secret string) error {
	if err := telegram.NewTelegramClient(botToken, core.WithProxyTransport(config.GetProxyTransport())).SetWebhook(context.Background(), webhookURL, secret); err != nil {
		logger.Errorf("TG SetWebhook 失败 bot=%s err=%v", maskToken(botToken), err)
		return err
	}
	logger.Infof("TG SetWebhook 成功 bot=%s", maskToken(botToken))
	return nil
}

// DeleteWebhook 删除 Telegram Webhook（账号禁用 / 删除时清理陈旧回调）。
// 统一走 channelbot/telegram.Client。
func DeleteWebhook(botToken string) error {
	if err := telegram.NewTelegramClient(botToken, core.WithProxyTransport(config.GetProxyTransport())).DeleteWebhook(context.Background()); err != nil {
		logger.Errorf("TG DeleteWebhook 失败 bot=%s err=%v", maskToken(botToken), err)
		return err
	}
	logger.Infof("TG DeleteWebhook 成功 bot=%s", maskToken(botToken))
	return nil
}

// SendMessage 直接通过 bot_token 发送文本消息（无状态），用于 智能体流程出站回复。
// 统一走 channelbot/telegram.Client，该客户端内部会把 AI 生成的 Markdown
// 转换为 Telegram HTML（parse_mode=HTML），与出站 AI 回复保持一致的渲染效果。
func SendMessage(botToken string, chatID int64, text string) error {
	if _, err := telegram.NewTelegramClient(botToken, core.WithProxyTransport(config.GetProxyTransport())).SendMessage(context.Background(), chatID, text); err != nil {
		logger.Errorf("TG SendMessage 失败 bot=%s chat=%d err=%v", maskToken(botToken), chatID, err)
		return err
	}
	logger.Infof("TG SendMessage 成功 bot=%s chat=%d", maskToken(botToken), chatID)
	return nil
}

// GetMe 获取 Bot 自身信息（用于校验 Bot Token 可用性）
func GetMe(botToken string) (map[string]any, error) {
	return callBotAPI(botToken, "getMe", url.Values{})
}

// GetBotUsername 取机器人 @username（由 BotFather 分配），用于群内「@机器人 才回复」的提及识别。
// 返回空串表示暂无（Token 无效或未配置 username），调用方应降级为「仅回复-被回复机器人消息」。
func GetBotUsername(botToken string) (string, error) {
	resp, err := GetMe(botToken)
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", fmt.Errorf("empty getMe response")
	}
	if ok, _ := resp["ok"].(bool); !ok {
		if desc, _ := resp["description"].(string); desc != "" {
			return "", fmt.Errorf("getMe failed: %s", desc)
		}
		return "", fmt.Errorf("getMe failed")
	}
	result, _ := resp["result"].(map[string]any)
	if result == nil {
		return "", fmt.Errorf("getMe result missing")
	}
	name, _ := result["username"].(string)
	return name, nil
}

// GetWebhookInfo 获取当前 webhook 配置（url、pending_update_count、last_error 等），
// 用于校验 webhook 是否已正确注册。
func GetWebhookInfo(botToken string) (map[string]any, error) {
	return callBotAPI(botToken, "getWebhookInfo", url.Values{})
}

// GetUpdates 通过 long polling 拉取更新（用于无公网 webhook 时的 fallback 方案）
//
// 参数：
//   - offset: 下次期望接收的 update_id（= 已处理的最大 update_id + 1）；0 表示从最早未确认的 update 开始
//   - limit: 单次返回最大 update 数（1-100）
//   - timeout: 长轮询等待秒数（建议 25s 以避开通用代理 30s 超时）
//
// 返回：每条 update 的原始 JSON 数组（保留原貌，便于直接转交给 webhook 入口处理）
func GetUpdates(ctx context.Context, botToken string, offset int64, limit, timeout int) ([]json.RawMessage, error) {
	if botToken == "" {
		return nil, fmt.Errorf("bot token is empty")
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	if timeout < 0 {
		timeout = 0
	}
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates", botToken)
	form := url.Values{}
	if offset > 0 {
		form.Set("offset", fmt.Sprintf("%d", offset))
	}
	form.Set("limit", fmt.Sprintf("%d", limit))
	form.Set("timeout", fmt.Sprintf("%d", timeout))
	form.Set("allowed_updates", `["message","edited_message","channel_post","edited_channel_post","callback_query","my_chat_member","chat_member","chat_join_request","inline_query"]`)

	client := &http.Client{
		Timeout:   time.Duration(timeout+10) * time.Second,
		Transport: config.GetProxyTransport(),
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("构造 getUpdates 请求失败: %w", err)
	}
	req.Body = nil
	req.URL.RawQuery = form.Encode()
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("getUpdates 失败: %w", err)
	}
	if resp.StatusCode == http.StatusConflict {
		return nil, fmt.Errorf("getUpdates 返回 409 Conflict（同 token 另一实例正在 polling）")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("getUpdates 返回 %d: %s", resp.StatusCode, string(body))
	}
	var result struct {
		OK          bool              `json:"ok"`
		Result      []json.RawMessage `json:"result"`
		Description string            `json:"description"`
		ErrorCode   int               `json:"error_code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析 getUpdates 响应失败: %w", err)
	}
	if !result.OK {
		return nil, fmt.Errorf("getUpdates 失败(%d): %s", result.ErrorCode, result.Description)
	}
	return result.Result, nil
}
