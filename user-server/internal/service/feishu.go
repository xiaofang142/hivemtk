package service

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/channelbot/core"
	"hivemtk-user/internal/channelbot/telegram"
	"hivemtk-user/internal/channelbot/whatsapp"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/httpclient"
	"hivemtk-user/internal/pkg/tgbot"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

type FeishuService struct {
	accountRepo *repository.FeishuAccountRepository
}

func NewFeishuService(db *gorm.DB) *FeishuService {
	if db == nil {
		return &FeishuService{}
	}
	r := repository.NewFeishuAccountRepository()
	r.SetDB(context.Background(), db)
	return &FeishuService{
		accountRepo: r,
	}
}

func (s *FeishuService) CreateAccount(ctx context.Context, input *model.FeishuAccount) (*model.FeishuAccount, error) {
	if input.AccountName == "" || input.AppID == "" || input.AppSecret == "" {
		return nil, errors.New("account_name, app_id, app_secret are required")
	}
	if input.Status == 0 {
		input.Status = 1
	}
	if err := s.accountRepo.Create(ctx, input); err != nil {
		return nil, err
	}
	return input, nil
}

func (s *FeishuService) UpdateAccount(ctx context.Context, acc *model.FeishuAccount) error {
	return s.accountRepo.Update(ctx, acc)
}

func (s *FeishuService) GetAccount(ctx context.Context, id uint) (*model.FeishuAccount, error) {
	return s.accountRepo.GetByID(ctx, id)
}

func (s *FeishuService) ListAccounts(ctx context.Context) ([]*model.FeishuAccount, error) {
	return s.accountRepo.GetAll(ctx)
}

func (s *FeishuService) DeleteAccount(ctx context.Context, id uint) error {
	return s.accountRepo.Delete(ctx, id)
}

func (s *FeishuService) GetSecretsByAccountID(ctx context.Context, accountID string) (appID, token, encryptKey string, err error) {
	if s.accountRepo == nil {
		return "", "", "", errors.New("db nil")
	}

	var id uint
	if _, scanErr := fmt.Sscanf(accountID, "%d", &id); scanErr == nil && id > 0 {
		if acc, gerr := s.accountRepo.GetByID(ctx, id); gerr == nil && acc != nil {
			return acc.AppID, acc.VerificationToken, acc.EncryptKey, nil
		}
	}
	if acc, gerr := s.accountRepo.GetFirstEnabled(ctx); gerr == nil && acc != nil {
		return acc.AppID, acc.VerificationToken, acc.EncryptKey, nil
	}
	acc, err := s.accountRepo.GetFirst(ctx)
	if err != nil {
		return "", "", "", err
	}
	return acc.AppID, acc.VerificationToken, acc.EncryptKey, nil
}

type FeishuIntegrationService struct {
	feishu        *FeishuService
	hub           *MessageHubService
	inbox         *InboxService
	feishuMsgRepo *repository.FeishuMessageRepository
}

func NewFeishuIntegrationService(db *gorm.DB) *FeishuIntegrationService {
	var msgRepo *repository.FeishuMessageRepository
	if db != nil {
		msgRepo = repository.NewFeishuMessageRepository()
		msgRepo.SetDB(context.Background(), db)
	}
	return &FeishuIntegrationService{
		feishu:        NewFeishuService(db),
		hub:           NewMessageHubServiceWithDB(db, nil),
		inbox:         NewInboxServiceWithDB(db),
		feishuMsgRepo: msgRepo,
	}
}

type FeishuIngestRequest struct {
	AccountID uint
	OpenID    string
	UnionID   string
	UserID    string
	Name      string
	MsgType   string
	Content   string
	MsgID     string
	ChatID    string
	ChatType  string
}

func (s *FeishuIntegrationService) IngestMessage(ctx context.Context, req *FeishuIngestRequest) (*model.MessageHub, *model.InboxConversation, error) {
	if s.feishuMsgRepo == nil {
		return nil, nil, errors.New("db nil")
	}
	if req.MsgType == "" {
		req.MsgType = "text"
	}
	if req.ChatType == "" {
		req.ChatType = "p2p"
	}
	if req.MsgID == "" {
		req.MsgID = fmt.Sprintf("feishu-%d", time.Now().UnixNano())
	}
	convID := req.ChatID
	if convID == "" {
		convID = fmt.Sprintf("feishu-%d-%s", req.AccountID, req.OpenID)
	}
	hubMsg, err := s.hub.Push(ctx, &PushMessageRequest{
		Platform:       "feishu",
		AccountID:      fmt.Sprintf("%d", req.AccountID),
		MsgID:          req.MsgID,
		Direction:      "inbound",
		MsgType:        req.MsgType,
		SenderID:       req.OpenID,
		SenderName:     req.Name,
		Content:        req.Content,
		ConversationID: convID,
		IsGroup:        req.ChatType == "group",
		GroupID:        req.ChatID,
		SentAt:         timePtr(time.Now()),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("hub push: %w", err)
	}
	var conv *model.InboxConversation
	if hubMsg != nil {
		conv, err = s.inbox.UpsertFromHubMessage(ctx, hubMsg)
		if err != nil {
			return hubMsg, nil, fmt.Errorf("inbox upsert: %w", err)
		}
	}
	return hubMsg, conv, nil
}

// SendMessage 发送飞书文本消息。
// conversationID：入站会话 ID（chat_id，oc_ 开头）。非空时出站记录落同一会话，
// 保证钩子3 方向判定/去重/Recheck 与入站一致；为空时回退旧格式 feishu-{account}-{openID}。
func (s *FeishuIntegrationService) SendMessage(ctx context.Context, accountID uint, openID, content, receiveIDType, conversationID string) error {
	if s.feishuMsgRepo == nil {
		return errors.New("db nil")
	}
	acc, err := s.feishu.GetAccount(ctx, accountID)
	if err != nil {
		return fmt.Errorf("get feishu account: %w", err)
	}
	tk, err := s.getAccessToken(ctx, acc)
	if err != nil {

		logger.Errorf("[feishu] 拉 token 失败（accountID=%d）: %v", accountID, err)
		return errors.New("get feishu access token failed")
	}
	idType := receiveIDType
	if idType == "" {
		idType = "open_id"
	}
	chatType := "p2p"
	if idType == "open_chat_id" {
		chatType = "group"
	}

	body := map[string]any{
		"receive_id": openID,
		"msg_type":   "text",
		"content":    feishuTextContentJSON(content),
	}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, "POST",
		"https://open.feishu.cn/open-apis/im/v1/messages?receive_id_type="+idType,
		bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+tk)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := httpclient.Client.Do(req)
	if err != nil {
		return fmt.Errorf("send feishu msg: %w", err)
	}
	defer resp.Body.Close()
	respB, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		now := time.Now()
		acc.LastErrorAt = &now
		acc.LastErrorMsg = string(respB)
		if uErr := s.feishu.UpdateAccount(ctx, acc); uErr != nil {
		// 持久化失败只影响下次重启前的自愈，记日志留痕
		logger.Warnf("[feishu] 新 token 持久化失败 account=%d: %v", acc.ID, uErr)
	}
		return fmt.Errorf("feishu api status %d: %s", resp.StatusCode, string(respB))
	}
	outMsg := &model.FeishuMessage{
		AccountID: accountID,
		MsgID:     fmt.Sprintf("feishu-out-%d", time.Now().UnixNano()),
		ChatID:    openID,
		ChatType:  chatType,
		SenderID:  openID,
		MsgType:   "text",
		Content:   content,
		Direction: "outbound",
	}
	if err := s.feishuMsgRepo.Create(ctx, outMsg); err != nil {
		logger.Errorf("[Feishu] 出站消息落库失败 msg_id=%s: %v", outMsg.MsgID, err)
	}
	outConv := conversationID
	if outConv == "" {
		outConv = fmt.Sprintf("feishu-%d-%s", accountID, openID)
	}
	hubMsg, _ := s.hub.Push(ctx, &PushMessageRequest{
		Platform:       "feishu",
		AccountID:      fmt.Sprintf("%d", accountID),
		MsgID:          outMsg.MsgID,
		Direction:      "outbound",
		MsgType:        "text",
		SenderID:       fmt.Sprintf("%d", accountID),
		ReceiverID:     openID,
		Content:        content,
		ConversationID: outConv,
		IsAIReply:      true,
		AIAgent:        "sales_engine",
		SentAt:         timePtr(time.Now()),
	})
	if hubMsg != nil {
		if _, err := s.inbox.UpsertFromHubMessage(ctx, hubMsg); err != nil {
			logger.Warnf("[feishu] upsert outbound to inbox failed: %v", err)
		}
	}
	return nil
}

func (s *FeishuIntegrationService) getAccessToken(ctx context.Context, acc *model.FeishuAccount) (string, error) {
	if acc.AccessToken != "" && acc.TokenExpires != nil && time.Now().Before(*acc.TokenExpires) {
		return acc.AccessToken, nil
	}
	body := map[string]string{
		"app_id":     acc.AppID,
		"app_secret": acc.AppSecret,
	}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, "POST",
		"https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal",
		bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := httpclient.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int    `json:"expire"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.Code != 0 {

		acc.AccessToken = ""
		acc.TokenExpires = nil
		logger.Errorf("[feishu] 拉 token 失败 code=%d msg=%s（已清空旧 token）", out.Code, out.Msg)
		return "", fmt.Errorf("feishu token code=%d: %s", out.Code, out.Msg)
	}
	expires := time.Now().Add(time.Duration(out.Expire-300) * time.Second)
	acc.AccessToken = out.TenantAccessToken
	acc.TokenExpires = &expires
	if uErr := s.feishu.UpdateAccount(ctx, acc); uErr != nil {
		// 持久化失败只影响下次重启前的自愈，记日志留痕
		logger.Warnf("[feishu] 新 token 持久化失败 account=%d: %v", acc.ID, uErr)
	}
	return out.TenantAccessToken, nil
}

func (s *FeishuIntegrationService) RefreshAccessToken(ctx context.Context, acc *model.FeishuAccount) error {
	if acc == nil {
		return errors.New("account nil")
	}
	acc.AccessToken = ""
	acc.TokenExpires = nil
	_, err := s.getAccessToken(ctx, acc)
	return err
}

func (s *FeishuIntegrationService) GetAccessTokenForTest(ctx context.Context, acc *model.FeishuAccount) (string, error) {
	return s.getAccessToken(ctx, acc)
}

type TelegramService struct {
	accRepo *repository.TelegramAccountRepository
}

func NewTelegramService(db *gorm.DB) *TelegramService {
	r := repository.NewTelegramAccountRepository()
	if db != nil {
		r.SetDB(context.Background(), db)
	}
	return &TelegramService{accRepo: r}
}

func (s *TelegramService) CreateAccount(ctx context.Context, acc *model.TelegramAccount) (*model.TelegramAccount, error) {
	if acc.AccountName == "" || acc.BotToken == "" {
		return nil, errors.New("account_name and bot_token are required")
	}
	if acc.Status == 0 {
		acc.Status = 1
	}

	// 1. 先落库，拿到自增 ID（后续推导 webhook_url 需要 acc.ID）
	if err := s.accRepo.Create(ctx, acc); err != nil {
		return nil, err
	}

	// 2. 自动获取 bot_username（异步尝试，失败不阻断创建）
	if acc.BotUsername == "" {
		if uname, gerr := tgbot.GetBotUsername(acc.BotToken); gerr == nil && uname != "" {
			acc.BotUsername = uname
		}
	}

	// 3. 自动生成 webhook_secret（无需用户填写）
	if acc.WebhookSecret == "" {
		acc.WebhookSecret = GenTGWebhookSecret()
	}

	// 4. 推导 webhook_url：优先用户显式填写，其次 config.GetPublicBaseURL
	resolvedURL, hasPublic := ResolveTelegramWebhookURL(acc)
	if resolvedURL != "" {
		acc.WebhookURL = resolvedURL
		acc.WebhookEnabled = true
		acc.AIAgentEnabled = true
	}

	// 5. 回写自动填充的字段（bot_username / webhook_secret / webhook_url / webhook_enabled / ai_agent_enabled）
	if err := s.accRepo.Update(ctx, acc); err != nil {
		logger.Warnf("[TG] 账号 %d(%s) 回写自动填充字段失败: %v", acc.ID, acc.AccountName, err)
	}

	// 6. 异步注册 webhook 或降级 polling（goroutine 不阻断 HTTP 响应）
	if resolvedURL != "" && hasPublic {
		// 有公网域名 → goroutine 调 setWebhook
		go func() {
			if err := tgbot.SetWebhook(acc.BotToken, acc.WebhookURL, acc.WebhookSecret); err != nil {
				logger.Warnf("[TG] 账号 %d(%s) 异步 setWebhook 失败: %v (可在 UI 手动重试)", acc.ID, acc.AccountName, err)
				now := time.Now()
				acc.LastErrorAt = &now
				acc.LastErrorMsg = err.Error()
				_ = s.accRepo.Update(context.Background(), acc)
			} else {
				logger.Infof("[TG] 账号 %d(%s) 异步 setWebhook 成功: %s", acc.ID, acc.AccountName, acc.WebhookURL)
				now := time.Now()
				acc.LastSyncAt = &now
				acc.LastErrorAt = nil
				acc.LastErrorMsg = ""
				_ = s.accRepo.Update(context.Background(), acc)
			}
		}()
	} else {
		// 无公网域名 → 自动降级 polling（StartTelegramPolling 内部会抢占分布式锁，幂等）
		go func() {
			StartTelegramPolling(acc)
			logger.Infof("[TG] 账号 %d(%s) 无 public_base_url / 显式 webhook_url，自动降级为 polling 模式", acc.ID, acc.AccountName)
		}()
	}

	return acc, nil
}

func (s *TelegramService) GetAccount(ctx context.Context, id uint) (*model.TelegramAccount, error) {
	return s.accRepo.GetByID(ctx, id)
}

func (s *TelegramService) ListAccounts(ctx context.Context) ([]*model.TelegramAccount, error) {
	return s.accRepo.GetAll(ctx)
}

func (s *TelegramService) UpdateAccount(ctx context.Context, acc *model.TelegramAccount) error {
	return s.accRepo.Update(ctx, acc)
}

func (s *TelegramService) DeleteAccount(ctx context.Context, id uint) error {
	return s.accRepo.Delete(ctx, id)
}

func (s *TelegramService) GetSecretsByAccountID(ctx context.Context, accountID string) (botToken, webhookSecret string, err error) {
	if s.accRepo == nil {
		return "", "", errors.New("db nil")
	}
	var id uint
	if _, scanErr := fmt.Sscanf(accountID, "%d", &id); scanErr == nil && id > 0 {
		if acc, gerr := s.accRepo.GetByID(ctx, id); gerr == nil && acc != nil {
			return acc.BotToken, acc.WebhookSecret, nil
		}
	}
	if acc, gerr := s.accRepo.GetFirstEnabled(ctx); gerr == nil && acc != nil {
		return acc.BotToken, acc.WebhookSecret, nil
	}
	acc, err := s.accRepo.GetFirst(ctx)
	if err != nil {
		return "", "", err
	}
	return acc.BotToken, acc.WebhookSecret, nil
}

type TelegramIntegrationService struct {
	tg    *TelegramService
	hub   *MessageHubService
	inbox *InboxService
}

func NewTelegramIntegrationService(db *gorm.DB) *TelegramIntegrationService {
	return &TelegramIntegrationService{
		tg:    NewTelegramService(db),
		hub:   NewMessageHubServiceWithDB(db, nil),
		inbox: NewInboxServiceWithDB(db),
	}
}

type TelegramIngestRequest struct {
	AccountID  uint
	ChatID     int64
	FromID     int64
	FromName   string
	Username   string
	MsgType    string
	Content    string
	MsgID      int64
	IsGroup    bool
	GroupTitle string
}

func (s *TelegramIntegrationService) IngestMessage(ctx context.Context, req *TelegramIngestRequest) (*model.MessageHub, *model.InboxConversation, error) {
	if s.tg == nil {
		return nil, nil, errors.New("db nil")
	}
	if req.MsgType == "" {
		req.MsgType = "text"
	}
	chatIDStr := fmt.Sprintf("%d", req.ChatID)
	fromIDStr := fmt.Sprintf("%d", req.FromID)
	msgIDStr := fmt.Sprintf("tg_%d", req.MsgID)
	convID := chatIDStr
	hubMsg, err := s.hub.Push(ctx, &PushMessageRequest{
		Platform:       "telegram",
		AccountID:      fmt.Sprintf("%d", req.AccountID),
		MsgID:          msgIDStr,
		Direction:      "inbound",
		MsgType:        req.MsgType,
		SenderID:       fromIDStr,
		SenderName:     req.FromName,
		Content:        req.Content,
		ConversationID: convID,
		IsGroup:        req.IsGroup,
		GroupID:        chatIDStr,
		SentAt:         timePtr(time.Now()),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("hub push: %w", err)
	}
	var conv *model.InboxConversation
	if hubMsg != nil {
		conv, err = s.inbox.UpsertFromHubMessage(ctx, hubMsg)
		if err != nil {
			return hubMsg, nil, fmt.Errorf("inbox upsert: %w", err)
		}
	}
	return hubMsg, conv, nil
}

func (s *TelegramIntegrationService) SendMessage(ctx context.Context, accountID uint, chatID int64, content string) error {
	if s.tg == nil {
		return errors.New("db nil")
	}
	acc, err := s.tg.GetAccount(ctx, accountID)
	if err != nil {
		return fmt.Errorf("get tg account: %w", err)
	}
	cli := telegram.NewTelegramClient(acc.BotToken, core.WithHTTPClient(httpclient.Client))

	messageID, err := cli.SendMessage(ctx, chatID, content)
	if err != nil {
		now := time.Now()
		acc.LastErrorAt = &now
		acc.LastErrorMsg = err.Error()
		if uErr := s.tg.UpdateAccount(ctx, acc); uErr != nil {
		// 持久化失败只影响下次重启前的自愈，记日志留痕
		logger.Warnf("[tg] 新 token 持久化失败 account=%d: %v", acc.ID, uErr)
	}
		return fmt.Errorf("send tg msg: %w", err)
	}
	chatIDStr := fmt.Sprintf("%d", chatID)
	msgID := fmt.Sprintf("tg-out-%d", time.Now().UnixNano())
	if messageID > 0 {
		msgID = fmt.Sprintf("tg-%d", messageID)
	}
	hubMsg, _ := s.hub.Push(ctx, &PushMessageRequest{
		Platform:       "telegram",
		AccountID:      fmt.Sprintf("%d", accountID),
		MsgID:          msgID,
		Direction:      "outbound",
		MsgType:        "text",
		SenderID:       fmt.Sprintf("%d", accountID),
		ReceiverID:     chatIDStr,
		Content:        content,
		ConversationID: chatIDStr,
		IsAIReply:      true,
		AIAgent:        "sales_engine",
		SentAt:         timePtr(time.Now()),
	})
	if hubMsg != nil {
		if _, err := s.inbox.UpsertFromHubMessage(ctx, hubMsg); err != nil {
			logger.Warnf("[feishu] upsert outbound to inbox failed: %v", err)
		}
	}
	return nil
}

func (s *TelegramIntegrationService) SendCard(ctx context.Context, accountID uint, chatID int64, card *model.RichCard) error {
	if s.tg == nil {
		return errors.New("db nil")
	}
	if card == nil {
		return errors.New("card 为空")
	}
	acc, err := s.tg.GetAccount(ctx, accountID)
	if err != nil {
		return fmt.Errorf("get tg account: %w", err)
	}
	cli := telegram.NewTelegramClient(acc.BotToken, core.WithHTTPClient(httpclient.Client))
	text := buildTelegramCardText(card)
	kb := buildTelegramCardKeyboard(card)
	if _, err := cli.SendMessage(ctx, chatID, text, telegram.SendMessageOptions{
		ParseMode:                 "HTML",
		DisableMarkdownConversion: true,
		InlineKeyboard:            kb,
	}); err != nil {
		now := time.Now()
		acc.LastErrorAt = &now
		acc.LastErrorMsg = err.Error()
		if uErr := s.tg.UpdateAccount(ctx, acc); uErr != nil {
		// 持久化失败只影响下次重启前的自愈，记日志留痕
		logger.Warnf("[tg] 新 token 持久化失败 account=%d: %v", acc.ID, uErr)
	}
		return fmt.Errorf("send tg card: %w", err)
	}
	chatIDStr := fmt.Sprintf("%d", chatID)
	hubMsg, _ := s.hub.Push(ctx, &PushMessageRequest{
		Platform:       "telegram",
		AccountID:      fmt.Sprintf("%d", accountID),
		MsgID:          fmt.Sprintf("tg-card-out-%d", time.Now().UnixNano()),
		Direction:      "outbound",
		MsgType:        "card",
		SenderID:       fmt.Sprintf("%d", accountID),
		ReceiverID:     chatIDStr,
		Content:        card.Title,
		ConversationID: chatIDStr,
		IsAIReply:      true,
		AIAgent:        "sales_engine",
		SentAt:         timePtr(time.Now()),
	})
	if hubMsg != nil {
		if _, err := s.inbox.UpsertFromHubMessage(ctx, hubMsg); err != nil {
			logger.Warnf("[feishu] upsert outbound to inbox failed: %v", err)
		}
	}
	return nil
}

func buildTelegramCardText(card *model.RichCard) string {
	var b strings.Builder
	b.WriteString("<b>")
	b.WriteString(escapeHTML(card.Title))
	b.WriteString("</b>")
	if card.Subtitle != "" {
		b.WriteString("\n")
		b.WriteString(escapeHTML(card.Subtitle))
	}
	if card.Description != "" {
		b.WriteString("\n\n")
		b.WriteString(escapeHTML(card.Description))
	}
	if len(card.Fields) > 0 {
		b.WriteString("\n")
		for k, v := range card.Fields {
			b.WriteString("\n• ")
			b.WriteString(escapeHTML(k))
			b.WriteString(": ")
			b.WriteString(escapeHTML(v))
		}
	}
	return b.String()
}

func buildTelegramCardKeyboard(card *model.RichCard) [][]telegram.InlineButton {
	if len(card.Buttons) == 0 {
		return nil
	}
	rows := make([][]telegram.InlineButton, 0, len(card.Buttons))
	for _, btn := range card.Buttons {
		if btn.Text == "" {
			continue
		}
		rows = append(rows, []telegram.InlineButton{{Text: btn.Text, URL: btn.URL}})
	}
	if len(rows) == 0 {
		return nil
	}
	return rows
}

func escapeHTML(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func subtleConstantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	res := 0
	for i := 0; i < len(a); i++ {
		res |= int(a[i]) ^ int(b[i])
	}
	return res == 0
}

type WhatsAppCloudService struct {
	accRepo *repository.WhatsAppCloudAccountRepository
}

func NewWhatsAppCloudService(db *gorm.DB) *WhatsAppCloudService {
	if db == nil {
		return &WhatsAppCloudService{}
	}
	r := repository.NewWhatsAppCloudAccountRepository()
	r.SetDB(context.Background(), db)
	return &WhatsAppCloudService{accRepo: r}
}

func (s *WhatsAppCloudService) CreateAccount(ctx context.Context, acc *model.WhatsAppCloudAccount) (*model.WhatsAppCloudAccount, error) {
	if acc.AccountName == "" || acc.PhoneNumberID == "" || acc.AccessToken == "" {
		return nil, errors.New("account_name, phone_number_id, access_token are required")
	}
	if acc.Status == 0 {
		acc.Status = 1
	}
	if err := s.accRepo.Create(ctx, acc); err != nil {
		return nil, err
	}
	return acc, nil
}

func (s *WhatsAppCloudService) GetAccount(ctx context.Context, id uint) (*model.WhatsAppCloudAccount, error) {
	return s.accRepo.GetByID(ctx, id)
}

func (s *WhatsAppCloudService) GetAccountByPhone(ctx context.Context, phoneID string) (*model.WhatsAppCloudAccount, error) {
	return s.accRepo.GetByPhoneNumberID(ctx, phoneID)
}

func (s *WhatsAppCloudService) ListAccounts(ctx context.Context) ([]*model.WhatsAppCloudAccount, error) {
	return s.accRepo.GetAll(ctx)
}

func (s *WhatsAppCloudService) UpdateAccount(ctx context.Context, acc *model.WhatsAppCloudAccount) error {
	return s.accRepo.Update(ctx, acc)
}

func (s *WhatsAppCloudService) DeleteAccount(ctx context.Context, id uint) error {
	return s.accRepo.Delete(ctx, id)
}

func (s *WhatsAppCloudService) GetSecretsByAccountID(ctx context.Context, accountID string) (token, appSecret string, err error) {
	if s.accRepo == nil {
		return "", "", errors.New("db nil")
	}
	var id uint
	if _, scanErr := fmt.Sscanf(accountID, "%d", &id); scanErr == nil && id > 0 {
		if acc, gerr := s.accRepo.GetByID(ctx, id); gerr == nil && acc != nil {
			return acc.AccessToken, acc.AppSecret, nil
		}
	}
	if acc, gerr := s.accRepo.GetFirstEnabled(ctx); gerr == nil && acc != nil {
		return acc.AccessToken, acc.AppSecret, nil
	}
	acc, err := s.accRepo.GetFirst(ctx)
	if err != nil {
		return "", "", err
	}
	return acc.AccessToken, acc.AppSecret, nil
}

func VerifyWhatsAppSignature(appSecret string, body []byte, signature string) bool {
	if appSecret == "" {
		return true
	}
	signature = strings.TrimPrefix(signature, "sha256=")
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return subtleConstantTimeEqual(expected, signature)
}

type WhatsAppCloudIntegrationService struct {
	wa    *WhatsAppCloudService
	hub   *MessageHubService
	inbox *InboxService
}

func NewWhatsAppCloudIntegrationService(db *gorm.DB) *WhatsAppCloudIntegrationService {
	return &WhatsAppCloudIntegrationService{
		wa:    NewWhatsAppCloudService(db),
		hub:   NewMessageHubServiceWithDB(db, nil),
		inbox: NewInboxServiceWithDB(db),
	}
}

type WhatsAppIngestRequest struct {
	AccountID    uint
	PhoneFrom    string
	CustomerName string
	MsgType      string
	Content      string
	MsgID        string
	ChatID       string
}

func (s *WhatsAppCloudIntegrationService) IngestMessage(ctx context.Context, req *WhatsAppIngestRequest) (*model.MessageHub, *model.InboxConversation, error) {
	if s.wa == nil {
		return nil, nil, errors.New("db nil")
	}
	if req.MsgType == "" {
		req.MsgType = "text"
	}
	if req.ChatID == "" {
		req.ChatID = req.PhoneFrom
	}
	hubMsg, err := s.hub.Push(ctx, &PushMessageRequest{
		Platform:       "whatsapp",
		AccountID:      fmt.Sprintf("%d", req.AccountID),
		MsgID:          req.MsgID,
		Direction:      "inbound",
		MsgType:        req.MsgType,
		SenderID:       req.PhoneFrom,
		SenderName:     req.CustomerName,
		Content:        req.Content,
		ConversationID: req.ChatID,
		SentAt:         timePtr(time.Now()),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("hub push: %w", err)
	}
	var conv *model.InboxConversation
	if hubMsg != nil {
		conv, err = s.inbox.UpsertFromHubMessage(ctx, hubMsg)
		if err != nil {
			return hubMsg, nil, fmt.Errorf("inbox upsert: %w", err)
		}
	}
	return hubMsg, conv, nil
}

func (s *WhatsAppCloudIntegrationService) SendMessage(ctx context.Context, accountID uint, toPhone, content string) error {
	if s.wa == nil {
		return errors.New("db nil")
	}
	acc, err := s.wa.GetAccount(ctx, accountID)
	if err != nil {
		return fmt.Errorf("get wa account: %w", err)
	}
	cli := whatsapp.NewCloudClient(acc.PhoneNumberID, acc.AccessToken, core.WithHTTPClient(httpclient.Client))

	wamid, err := cli.SendText(ctx, toPhone, content)
	if err != nil {
		now := time.Now()
		acc.LastErrorAt = &now
		acc.LastErrorMsg = err.Error()
		if uErr := s.wa.UpdateAccount(ctx, acc); uErr != nil {
		// 持久化失败只影响下次重启前的自愈，记日志留痕
		logger.Warnf("[wa] 新 token 持久化失败 account=%d: %v", acc.ID, uErr)
	}
		return fmt.Errorf("send wa msg: %w", err)
	}
	msgID := wamid
	if msgID == "" {
		msgID = fmt.Sprintf("wa-out-%d", time.Now().UnixNano())
	}
	hubMsg, _ := s.hub.Push(ctx, &PushMessageRequest{
		Platform:       "whatsapp",
		AccountID:      fmt.Sprintf("%d", accountID),
		MsgID:          msgID,
		Direction:      "outbound",
		MsgType:        "text",
		SenderID:       fmt.Sprintf("%d", accountID),
		ReceiverID:     toPhone,
		Content:        content,
		ConversationID: toPhone,
		IsAIReply:      true,
		AIAgent:        "sales_engine",
		SentAt:         timePtr(time.Now()),
	})
	if hubMsg != nil {
		if _, err := s.inbox.UpsertFromHubMessage(ctx, hubMsg); err != nil {
			logger.Warnf("[feishu] upsert outbound to inbox failed: %v", err)
		}
	}
	return nil
}

func DecryptFeishuEvent(encryptKey, encrypted string) ([]byte, error) {
	if encryptKey == "" {
		return nil, errors.New("encrypt_key empty")
	}
	key, err := base64.StdEncoding.DecodeString(encryptKey)
	if err != nil {

		if k2, e2 := base64.RawStdEncoding.DecodeString(encryptKey); e2 == nil && len(k2) == 32 {
			key = k2
			err = nil
		}
	}
	if err != nil || len(key) != 32 {

		kb := []byte(encryptKey)
		if len(kb) > 32 {
			kb = kb[:32]
		} else {
			pad := make([]byte, 32-len(kb))
			kb = append(kb, pad...)
		}
		key = kb
	}
	enc, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(enc)%aes.BlockSize != 0 || len(enc) < aes.BlockSize {
		return nil, errors.New("invalid encrypted length")
	}
	iv := enc[:aes.BlockSize]
	ciphertext := enc[aes.BlockSize:]
	mode := cipher.NewCBCDecrypter(block, iv)
	plain := make([]byte, len(ciphertext))
	mode.CryptBlocks(plain, ciphertext)
	padLen := int(plain[len(plain)-1])
	if padLen < 1 || padLen > aes.BlockSize || padLen > len(plain) {
		return nil, errors.New("invalid padding")
	}
	plain = plain[:len(plain)-padLen]

	if len(plain) > 20 {
		n := binary.BigEndian.Uint32(plain[16:20])
		if int(n) == len(plain)-20 {
			return plain[20:], nil
		}
	}
	return plain, nil
}

func timePtr(t time.Time) *time.Time { return &t }

func feishuTextContentJSON(text string) string {
	b, _ := json.Marshal(map[string]string{"text": text})
	return string(b)
}
