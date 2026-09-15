package service

import (
	"context"
	"fmt"
	"log"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

const (
	tgDMOutreachCooldown = 60 * time.Minute
	tgDMOutreachMinScore = 60
)

type TelegramDMOutreachService struct {
	svc *WebhookService
}

func NewTelegramDMOutreachService(svc *WebhookService) *TelegramDMOutreachService {
	return &TelegramDMOutreachService{svc: svc}
}

func (s *WebhookService) triggerTGDMOutreach(ctx context.Context, accountID, fromID, groupID, groupTitle string, score int, originalText string) {
	userID, err := strconv.ParseInt(fromID, 10, 64)
	if err != nil || userID == 0 {
		logger.Warnf("[TG-DM-Outreach] 无法解析用户ID fromID=%s", fromID)
		return
	}
	outreach := NewTelegramDMOutreachService(s)
	outreach.TriggerDMOutreach(ctx, accountID, userID, groupID, groupTitle, score, score >= tgDMOutreachMinScore, originalText)
}

func (s *TelegramDMOutreachService) TriggerDMOutreach(ctx context.Context, accountID string, userID int64, groupID, groupTitle string, intentScore int, isOpportunity bool, originalText string) {
	if s == nil || s.svc == nil {
		return
	}
	if intentScore < tgDMOutreachMinScore {
		return
	}
	if !isOpportunity {
		return
	}

	allowed := s.svc.tgLeadOutreachAllowed(ctx, accountID, groupID, strconv.FormatInt(userID, 10))
	if !allowed {
		return
	}

	if !s.dmOutreachCooldownAllowed(ctx, accountID, userID, groupID) {
		return
	}

	template := s.buildDMWelcomeTemplate(groupTitle, originalText)
	if s.svc.tgIntegration == nil && s.svc.lazyDB() != nil {
		s.svc.tgIntegration = NewTelegramIntegrationService(s.svc.lazyDB())
	}
	if s.svc.tgIntegration == nil {
		logger.Warnf("[TG-DM-Outreach] tgIntegration 未初始化，跳过私信发送 account=%s user=%d group=%s",
			accountID, userID, groupID)
		return
	}
	if err := s.svc.tgIntegration.SendMessage(ctx, parseAccountID(accountID), userID, template); err != nil {
		logger.Warnf("[TG-DM-Outreach] 私信发送失败 account=%s user=%d group=%s: %v",
			accountID, userID, groupID, err)
		return
	}

	logger.Infof("[TG-DM-Outreach] 群线索转私信成功 account=%s user=%d group=%s score=%d",
		accountID, userID, groupID, intentScore)

	s.recordDMOutreachEvent(ctx, accountID, userID, groupID, intentScore)
}

func (s *TelegramDMOutreachService) dmOutreachCooldownAllowed(ctx context.Context, accountID string, userID int64, groupID string) bool {
	key := "mtk:tg:dm_outreach:" + accountID + ":" + strconv.FormatInt(userID, 10) + ":" + groupID
	set, err := cache.GetGlobalCache().SetNX(ctx, key, "1", tgDMOutreachCooldown)
	if err != nil {
		logger.Warnf("[TG-DM-Outreach] DM 冷却检查失败（放行）account=%s user=%d group=%s: %v",
			accountID, userID, groupID, err)
		return true
	}
	if !set {
		logger.Infof("[TG-DM-Outreach] DM 冷却期内，拦截本次触达 account=%s user=%d group=%s",
			accountID, userID, groupID)
	}
	return set
}

func (s *TelegramDMOutreachService) buildDMWelcomeTemplate(groupTitle, originalText string) string {
	groupLabel := strings.TrimSpace(groupTitle)
	if groupLabel == "" {
		groupLabel = "相关群组"
	}

	templates := []string{
		fmt.Sprintf("你好！我看到你在「%s」群组里提到了产品需求，想进一步了解你的业务场景。方便简单介绍一下吗？", groupLabel),
		fmt.Sprintf("Hi! I noticed your message in the \"%s\" group. I think our solution might be a good fit for your needs. Would you like to explore further?", groupLabel),
		fmt.Sprintf("你好！在「%s」看到你的发言，我是做这方面产品的，可以聊聊你的具体需求吗？", groupLabel),
	}

	idx := 0
	if len(originalText) > 0 && isChinese(originalText) {
		idx = 2
	} else if len(originalText) > 0 && isEnglish(originalText) {
		idx = 1
	}

	return templates[idx]
}

func (s *TelegramDMOutreachService) recordDMOutreachEvent(ctx context.Context, accountID string, userID int64, groupID string, score int) {
	if s.svc == nil || s.svc.messageHubRepo == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[panic-recover] %T: %v\n%s", r, r, string(debug.Stack()))
		}
	}()

	hub := &model.MessageHub{
		Platform:       "telegram",
		AccountID:      accountID,
		MsgID:          fmt.Sprintf("tg_dm_outreach_%d_%d_%s", time.Now().UnixNano(), userID, groupID),
		Direction:      "outbound",
		MsgType:        "text",
		SenderID:       accountID,
		ReceiverID:     strconv.FormatInt(userID, 10),
		Content:        fmt.Sprintf("[DM Outreach] Score=%d Group=%s", score, groupID),
		ConversationID: strconv.FormatInt(userID, 10),
		IsGroup:        false,
		GroupID:        "",
		SentAt:         time.Now(),
		IsAIReply:      true,
		AIAgent:        "lead_outreach",
		Extra: model.JSONMap{
			"scenario":     "group_to_dm",
			"trigger":      "high_intent_lead",
			"intent_score": score,
			"source_group": groupID,
		},
	}

	if err := s.svc.messageHubRepo.Create(ctx, hub); err != nil {
		logger.Warnf("[TG-DM-Outreach] 记录私信事件失败: %v", err)
	}
}

func isChinese(text string) bool {
	for _, r := range text {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

func isEnglish(text string) bool {
	for _, r := range text {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return true
		}
	}
	return false
}

func parseAccountID(accountID string) uint {
	accID, err := strconv.ParseUint(accountID, 10, 64)
	if err != nil || accID == 0 {
		return 0
	}
	return uint(accID)
}
