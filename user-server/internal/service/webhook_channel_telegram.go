package service

import (
	"context"

	"fmt"

	"strconv"

	"strings"

	"time"

	"hivemtk-user/internal/channelbot/telegram"

	"hivemtk-user/internal/model"

	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"

	"hivemtk-user/internal/cache"
)

type tgDispatchExtra struct {
	Mentioned      bool
	NewOpportunity bool
	GateHandled    bool // /start 网关验证已消费，不再触发销售智能体
}

const (
	ChannelDouyin WebhookChannel = "douyin"

	ChannelKuaishou WebhookChannel = "kuaishou"

	ChannelXiaohongshu WebhookChannel = "xiaohongshu"

	ChannelXianyu WebhookChannel = "xianyu"

	ChannelTiktok WebhookChannel = "tiktok"

	ChannelWechat WebhookChannel = "wechat"

	ChannelWeCom WebhookChannel = "wecom"

	ChannelDingTalk WebhookChannel = "dingtalk"

	ChannelWhatsapp WebhookChannel = "whatsapp"

	ChannelTelegram WebhookChannel = "telegram"

	ChannelFeishu WebhookChannel = "feishu"

	ChannelCustom WebhookChannel = "custom"
)

func (s *WebhookService) dispatchTelegram(ctx context.Context, accountID string, p *ParsedPayload, raw []byte) (*model.MessageHub, *tgDispatchExtra, error) {
	if s.db == nil {
		return nil, nil, nil
	}
	s.ensureReposFromDB(ctx)

	tgPayload, err := telegram.ParseUpdate(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("telegram parse: %w", err)
	}

	if tgPayload.Message != nil {
		text := tgPayload.Message.Text
		if text == "" {
			text = tgPayload.Message.Caption
		}
		if text != "" {
			p.Content = text
		}
		if tgPayload.Message.From != nil {
			p.Sender = strconv.FormatInt(tgPayload.Message.From.ID, 10)
		}
		if tgPayload.Message.Chat != nil {
			p.ChatID = strconv.FormatInt(tgPayload.Message.Chat.ID, 10)
		}
	}

	botUsername := s.getTelegramBotUsername(ctx, accountID)

	// 方案 A：入群申请（群开启 "申请加入" 后 Telegram 推送 chat_join_request）
	if tgPayload.ChatJoinRequest != nil && tgPayload.ChatJoinRequest.From != nil {
		accID, _ := strconv.ParseUint(accountID, 10, 64)
		if accID > 0 {
			s.tgGate.HandleJoinRequest(ctx, uint(accID), tgPayload.ChatJoinRequest)
		}
		return nil, nil, nil
	}

	// 成员状态变更（chat_member / my_chat_member）暂无业务，静默消费
	if tgPayload.ChatMember != nil || tgPayload.MyChatMember != nil {
		return nil, nil, nil
	}

	if tgPayload.Message != nil && len(tgPayload.Message.NewChatMembers) > 0 && tgPayload.Message.Chat != nil {
		chatID := tgPayload.Message.Chat.ID
		chatType := tgPayload.Message.Chat.Type
		chatTitle := tgPayload.Message.Chat.Title
		if chatType == "" {
			chatType = "group"
		}
		chatIDStr := fmt.Sprintf("%d", chatID)
		isGroup := chatType == "group" || chatType == "supergroup"

		var newMember *telegram.TGUser
		for i := range tgPayload.Message.NewChatMembers {
			if !tgPayload.Message.NewChatMembers[i].IsBot {

				member := tgPayload.Message.NewChatMembers[i]
				newMember = &telegram.TGUser{
					ID:        member.ID,
					FirstName: member.FirstName,
					Username:  member.Username,
					IsBot:     member.IsBot,
				}
				break
			}
		}
		if newMember != nil {
			// 方案 B：管控群先禁言（HandleNewMembers 内部判断网关配置，未配置直接跳过）
			accID, _ := strconv.ParseUint(accountID, 10, 64)
			if accID > 0 {
				s.tgGate.HandleNewMembers(ctx, uint(accID), chatID, []telegram.TGUser{*newMember})
			}

			senderIDStr := fmt.Sprintf("%d", newMember.ID)
			fromName := newMember.FirstName
			if newMember.Username != "" {
				fromName = newMember.Username
			}

			groupLabel := chatTitle
			if groupLabel == "" {
				groupLabel = chatIDStr
			}
			eventContent := fmt.Sprintf("[入群事件] 用户 %s (@%s) 加入群组 %s", newMember.FirstName, newMember.Username, groupLabel)
			hub := &model.MessageHub{
				Platform:       "telegram",
				AccountID:      accountID,
				MsgID:          fmt.Sprintf("tg_join_%d_%d", chatID, newMember.ID),
				Direction:      "inbound",
				SenderID:       senderIDStr,
				ConversationID: chatIDStr,
				MsgType:        "event",
				Content:        eventContent,
				SentAt:         time.Now(),
				IsGroup:        isGroup,
				GroupID:        chatIDStr,
			}
			if err := s.messageHubRepo.Create(ctx, hub); err != nil {
				if !strings.Contains(err.Error(), "UNIQUE") && !strings.Contains(err.Error(), "duplicate") {
					return nil, nil, err
				}
			}
			s.upsertInboxFromHub(ctx, hub, fromName)

			triggerMsg := fmt.Sprintf("新用户 %s (@%s) 刚加入群组「%s」。请以销售助手身份主动发起欢迎+销售开场白，引导用户了解我们的产品。",
				newMember.FirstName, newMember.Username, groupLabel)
			s.triggerTelegramJoinSales(ctx, accountID, chatIDStr, senderIDStr, triggerMsg)
			return hub, nil, nil
		}
	}

	if tgPayload.Message != nil && tgPayload.Message.LeftChatMember != nil && tgPayload.Message.Chat != nil {
		chatID := tgPayload.Message.Chat.ID
		chatIDStr := fmt.Sprintf("%d", chatID)
		left := tgPayload.Message.LeftChatMember
		senderIDStr := fmt.Sprintf("%d", left.ID)
		fromName := left.FirstName
		if left.Username != "" {
			fromName = left.Username
		}
		eventContent := fmt.Sprintf("[退群事件] 用户 %s (@%s) 离开群组", left.FirstName, left.Username)
		hub := &model.MessageHub{
			Platform:       "telegram",
			AccountID:      accountID,
			MsgID:          fmt.Sprintf("tg_left_%d_%d", chatID, left.ID),
			Direction:      "inbound",
			SenderID:       senderIDStr,
			ConversationID: chatIDStr,
			MsgType:        "event",
			Content:        eventContent,
			SentAt:         time.Now(),
			IsGroup:        true,
			GroupID:        chatIDStr,
		}
		if err := s.messageHubRepo.Create(ctx, hub); err != nil {
			if !strings.Contains(err.Error(), "UNIQUE") && !strings.Contains(err.Error(), "duplicate") {
				return nil, nil, err
			}
		}
		s.upsertInboxFromHub(ctx, hub, fromName)
		return hub, nil, nil
	}

	type tgMsg struct {
		msgID     int64
		chatID    int64
		chatType  string
		fromID    int64
		fromName  string
		username  string
		fromIsBot bool
		text      string
	}
	var picked *tgMsg
	if tgPayload.Message != nil && tgPayload.Message.From != nil && tgPayload.Message.Chat != nil {

		if tgPayload.Message.Text == "" && len(tgPayload.Message.NewChatMembers) == 0 && tgPayload.Message.LeftChatMember == nil && tgPayload.Message.NewChatTitle != "" {
			return nil, nil, nil
		}
		tm := &tgMsg{
			msgID:     tgPayload.Message.MessageID,
			chatID:    tgPayload.Message.Chat.ID,
			chatType:  tgPayload.Message.Chat.Type,
			fromID:    tgPayload.Message.From.ID,
			fromName:  tgPayload.Message.From.FirstName,
			username:  tgPayload.Message.From.Username,
			fromIsBot: tgPayload.Message.From.IsBot,
			text:      tgPayload.Message.Text,
		}
		if tm.chatType == "" {
			tm.chatType = "private"
		}
		picked = tm
	} else if tgPayload.EditedMessage != nil && tgPayload.EditedMessage.Chat != nil {
		tm := &tgMsg{
			msgID:    tgPayload.EditedMessage.MessageID,
			chatID:   tgPayload.EditedMessage.Chat.ID,
			chatType: tgPayload.EditedMessage.Chat.Type,
			fromID: func() int64 {
				if tgPayload.EditedMessage.From != nil {
					return tgPayload.EditedMessage.From.ID
				}
				return 0
			}(),
			fromName: func() string {
				if tgPayload.EditedMessage.From != nil {
					return tgPayload.EditedMessage.From.FirstName
				}
				return ""
			}(),
			username: func() string {
				if tgPayload.EditedMessage.From != nil {
					return tgPayload.EditedMessage.From.Username
				}
				return ""
			}(),
			fromIsBot: func() bool {
				if tgPayload.EditedMessage.From != nil {
					return tgPayload.EditedMessage.From.IsBot
				}
				return false
			}(),
			text: tgPayload.EditedMessage.Text,
		}
		if tm.chatType == "" {
			tm.chatType = "private"
		}
		picked = tm
	} else if tgPayload.CallbackQuery != nil && tgPayload.CallbackQuery.From != nil {
		chatID := int64(0)
		chatType := "private"
		if tgPayload.CallbackQuery.Message != nil {
			chatID = tgPayload.CallbackQuery.Message.Chat.ID
			chatType = tgPayload.CallbackQuery.Message.Chat.Type
		}
		picked = &tgMsg{
			msgID:     0,
			chatID:    chatID,
			chatType:  chatType,
			fromID:    tgPayload.CallbackQuery.From.ID,
			fromName:  tgPayload.CallbackQuery.From.FirstName,
			fromIsBot: tgPayload.CallbackQuery.From.IsBot,
			text:      "/callback " + tgPayload.CallbackQuery.Data,
		}
	}
	if picked == nil {
		return nil, nil, nil
	}
	chatIDStr := fmt.Sprintf("%d", picked.chatID)
	senderIDStr := fmt.Sprintf("%d", picked.fromID)
	hub := &model.MessageHub{
		Platform:       "telegram",
		AccountID:      accountID,
		MsgID:          fmt.Sprintf("tg_%d", picked.msgID),
		Direction:      "inbound",
		SenderID:       senderIDStr,
		ConversationID: chatIDStr,
		MsgType:        "text",
		Content:        picked.text,
		SentAt:         time.Now(),
		IsGroup:        picked.chatType == "group" || picked.chatType == "supergroup",
		GroupID:        chatIDStr,
	}
	if hub.Content == "" {
		hub.Content = "[" + picked.chatType + "]"
	}

	if err := tgPayload.Ingress(ctx, s.ingressHandler(ctx), accountID); err != nil {
		return nil, nil, err
	}
	s.upsertInboxFromHub(ctx, hub, picked.fromName)

	newOpportunity := false
	if picked.fromID != 0 && !picked.fromIsBot {
		groupTitle := ""
		if tgPayload.Message != nil && tgPayload.Message.Chat != nil {
			groupTitle = tgPayload.Message.Chat.Title
		}
		newOpportunity = s.mineTelegramGroupLead(context.Background(), hub, accountID, chatIDStr, groupTitle, senderIDStr, picked.username, picked.fromName, picked.text)
	}

	mentioned := isTelegramBotMentioned(picked.text, botUsername)
	if !mentioned && tgPayload.Message != nil && tgPayload.Message.ReplyToMessage != nil &&
		tgPayload.Message.ReplyToMessage.From != nil && tgPayload.Message.ReplyToMessage.From.IsBot {
		mentioned = true
	}

	// 网关验证：私聊 /start（带或不带 token）走激活流程，命中后不再触发销售智能体
	gateHandled := false
	if !picked.fromIsBot && picked.chatType == "private" && strings.HasPrefix(strings.TrimSpace(picked.text), "/start") {
		accID, _ := strconv.ParseUint(accountID, 10, 64)
		if accID > 0 {
			gateHandled = s.tgGate.HandleStartCommand(ctx, uint(accID), tgUserFromMessage(tgPayload), picked.text, picked.chatID)
		}
	}

	return hub, &tgDispatchExtra{Mentioned: mentioned, NewOpportunity: newOpportunity, GateHandled: gateHandled}, nil
}

// tgUserFromMessage 提取 /start 命令发送者
func tgUserFromMessage(tgPayload *telegram.Update) *telegram.TGUser {
	if tgPayload.Message != nil && tgPayload.Message.From != nil {
		return tgPayload.Message.From
	}
	if tgPayload.EditedMessage != nil && tgPayload.EditedMessage.From != nil {
		return tgPayload.EditedMessage.From
	}
	if tgPayload.CallbackQuery != nil && tgPayload.CallbackQuery.From != nil {
		return tgPayload.CallbackQuery.From
	}
	return nil
}

const tgLeadOutreachCooldown = 30 * time.Minute

func (s *WebhookService) getTelegramBotUsername(ctx context.Context, accountID string) string {
	if s.telegramRepo == nil {
		return ""
	}
	accID, err := strconv.ParseUint(accountID, 10, 64)
	if err != nil || accID == 0 {
		return ""
	}
	acc, err := s.telegramRepo.GetByID(ctx, uint(accID))
	if err != nil || acc == nil {
		return ""
	}
	return strings.TrimSpace(acc.BotUsername)
}

func isTelegramBotMentioned(text, botUsername string) bool {
	uname := strings.TrimSpace(botUsername)
	if uname == "" {
		return false
	}
	needle := "@" + strings.ToLower(uname)
	lower := strings.ToLower(text)
	idx := strings.Index(lower, needle)
	if idx < 0 {
		return false
	}
	end := idx + len(needle)
	if end < len(lower) {
		next := lower[end]
		if (next >= 'a' && next <= 'z') || (next >= '0' && next <= '9') || next == '_' {
			return false
		}
	}
	return true
}

func (s *WebhookService) tgLeadOutreachAllowed(ctx context.Context, accountID, chatID, senderID string) bool {
	key := "mtk:tg:outreach:" + accountID + ":" + chatID + ":" + senderID
	set, err := cache.GetGlobalCache().SetNX(ctx, key, "1", tgLeadOutreachCooldown)
	if err != nil {

		return true
	}
	return set
}

func (s *WebhookService) triggerTelegramJoinSales(ctx context.Context, accountID, chatID, senderID, triggerMsg string) {
	if s.salesEngine == nil {
		return
	}
	if !s.shouldTriggerAI(ctx, ChannelTelegram, accountID) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), utils.DefaultHTTPTimeout)
	defer cancel()

	req := &SalesRequest{
		SessionID:   "telegram:" + chatID,
		CustomerID:  senderID,
		OneID:       "telegram:" + senderID,
		UserMessage: triggerMsg,
		Platform:    "telegram",
		AutoExecute: true,
		Config:      DefaultSalesEngineConfig(),
	}

	req.Config.Persona = "你是 Telegram 群组里的销售助手。新用户加入群组时，主动发起一段简洁、亲切的欢迎+销售开场白，引导用户了解产品。回复不超过 80 字。"

	resp, err := s.salesEngine.Handle(ctx, req)
	if err != nil {
		logger.Errorf("[Webhook] TG 入群触发 智能体失败 account=%s chat=%s: %v", accountID, chatID, err)
		return
	}
	if resp == nil || resp.Reply == "" {
		return
	}
	if resp.TransferredToHuman {
		logger.Infof("[Webhook] TG 入群触发转人工: %s", resp.TransferReason)
		return
	}

	chatIDInt, _ := strconv.ParseInt(chatID, 10, 64)
	if chatIDInt == 0 {
		return
	}
	if s.tgIntegration == nil {
		s.tgIntegration = NewTelegramIntegrationService(s.db)
	}
	accID, err := strconv.ParseUint(accountID, 10, 64)
	if err != nil || accID == 0 {
		return
	}
	if err := s.tgIntegration.SendMessage(ctx, uint(accID), chatIDInt, resp.Reply); err != nil {
		logger.Errorf("[Webhook] TG 入群欢迎消息发送失败 account=%s chat=%s: %v", accountID, chatID, err)
	}
}

func (s *WebhookService) getTelegramWebhookSecret(ctx context.Context, accountID string) string {
	if s.telegramRepo == nil || s.db == nil {
		return ""
	}
	accID, err := strconv.ParseUint(accountID, 10, 64)
	if err != nil || accID == 0 {
		return ""
	}
	acc, err := s.telegramRepo.GetByID(ctx, uint(accID))
	if err != nil {
		return ""
	}
	return acc.WebhookSecret
}
