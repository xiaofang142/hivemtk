package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

type douyinWebhookPayload struct {
	EventType string `json:"event_type"`
	Data      struct {
		Message struct {
			MessageID   string `json:"message_id"`
			Content     string `json:"content"`
			ContentType string `json:"content_type"`
			CreateTime  string `json:"create_time"`
		} `json:"message"`
		From struct {
			UserID   string `json:"user_id"`
			NickName string `json:"nick_name"`
			SecUID   string `json:"sec_uid"`
			Avatar   string `json:"avatar"`
		} `json:"from"`
		To struct {
			GroupID   string `json:"group_id"`
			GroupName string `json:"group_name"`
			UserID    string `json:"user_id"`
		} `json:"to"`
		Conversation struct {
			ConversationID string `json:"conversation_id"`
			Type           string `json:"type"`
		} `json:"conversation"`
	} `json:"data"`
}

func (s *WebhookService) dispatchDouyin(ctx context.Context, accountID string, p *ParsedPayload, raw []byte) (*model.MessageHub, *tgDispatchExtra, error) {
	if s.lazyDB() == nil {
		return nil, nil, nil
	}
	s.ensureReposFromDB(ctx)

	var payload douyinWebhookPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		logger.Debugf("[DouyinWebhook] 解析 payload 失败: %v, 尝试使用通用解析", err)
		return s.dispatchDouyinGeneric(ctx, accountID, p, raw)
	}

	if p.Content == "" && payload.Data.Message.Content != "" {
		p.Content = payload.Data.Message.Content
	}
	if p.Sender == "" && payload.Data.From.UserID != "" {
		p.Sender = payload.Data.From.UserID
	}

	isGroup := payload.Data.Conversation.Type == "group" || payload.Data.To.GroupID != ""
	groupID := payload.Data.To.GroupID
	groupTitle := payload.Data.To.GroupName
	convID := payload.Data.Conversation.ConversationID
	if convID == "" {
		if isGroup {
			convID = groupID
		} else {
			convID = "dy_dm_" + p.Sender
		}
	}

	msgID := "dy_" + payload.Data.Message.MessageID

	if payload.Data.Message.MessageID == "" {
		msgID = "dy_" + ContentHashMsgID("douyin", "", p.Content)
	}

	senderName := payload.Data.From.NickName
	if senderName == "" {
		senderName = p.Sender
	}

	hub := &model.MessageHub{
		Platform:       "douyin",
		AccountID:      accountID,
		MsgID:          msgID,
		Direction:      "inbound",
		SenderID:       p.Sender,
		SenderName:     senderName,
		ConversationID: convID,
		MsgType:        "text",
		Content:        p.Content,
		SentAt:         time.Now(),
		IsGroup:        isGroup,
		GroupID:        groupID,
	}

	if hub.Content == "" {
		hub.Content = "[douyin message]"
	}

	if err := s.messageHubRepo.Create(ctx, hub); err != nil {
		if !strings.Contains(err.Error(), "UNIQUE") && !strings.Contains(err.Error(), "duplicate") {
			logger.Warnf("[DouyinWebhook] 写入 MessageHub 失败: %v", err)
		}
	}

	s.upsertInboxFromHub(ctx, hub, senderName)

	newOpportunity := false
	if p.Sender != "" && !strings.HasPrefix(p.Sender, "bot_") {
		newOpportunity = s.mineDouyinGroupLead(ctx, hub, accountID, groupID, groupTitle, p.Sender, senderName, p.Content)
		if newOpportunity {
			logger.Infof("[DouyinWebhook] 群线索触发 account=%s user=%s score content=%s", accountID, p.Sender, p.Content)
		}
	}

	return hub, &tgDispatchExtra{NewOpportunity: newOpportunity}, nil
}

func (s *WebhookService) dispatchDouyinGeneric(ctx context.Context, accountID string, p *ParsedPayload, raw []byte) (*model.MessageHub, *tgDispatchExtra, error) {
	if p.Sender == "" {
		return nil, nil, nil
	}
	isGroup := p.ChatID != "" && strings.HasPrefix(p.ChatID, "group_")
	groupID := p.ChatID

	content := p.Content
	if content == "" {
		content = "[douyin generic event]"
	}

	hub := &model.MessageHub{
		Platform:       "douyin",
		AccountID:      accountID,
		MsgID:          "dy_generic_" + ContentHashMsgID("douyin", p.Sender, content),
		Direction:      "inbound",
		SenderID:       p.Sender,
		ConversationID: p.ChatID,
		MsgType:        "text",
		Content:        content,
		SentAt:         time.Now(),
		IsGroup:        isGroup,
		GroupID:        groupID,
	}

	if hub.Content == "" {
		hub.Content = "[douyin generic event]"
	}

	if err := s.messageHubRepo.Create(ctx, hub); err != nil {
		if !strings.Contains(err.Error(), "UNIQUE") && !strings.Contains(err.Error(), "duplicate") {
			logger.Warnf("[DouyinWebhook] 通用解析写入失败: %v", err)
		}
	}
	s.upsertInboxFromHub(ctx, hub, p.Sender)

	newOpportunity := false
	if p.Sender != "" {
		newOpportunity = s.mineDouyinGroupLead(ctx, hub, accountID, groupID, "", p.Sender, p.Sender, p.Content)
	}

	return hub, &tgDispatchExtra{NewOpportunity: newOpportunity}, nil
}
