package service

import (
	"context"
	"log"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

func (s *WebhookService) mineDouyinGroupLead(ctx context.Context, hub *model.MessageHub, accountID, groupID, groupTitle, fromID, fromName, text string) (newOpportunity bool) {

	return MineUnifiedLead(ctx, s, hub, DouyinLeadAdapter{}, accountID, groupID, groupTitle, fromID, fromName, "", text)
}

func (s *WebhookService) triggerDouyinDMOutreach(ctx context.Context, accountID, fromID, groupID, groupTitle string, score int, originalText string) {
	dySvc := NewDouyinIntegrationService(s.lazyDB())
	if dySvc == nil {
		return
	}

	convKey := "mtk:dy:dm_sent:" + accountID + ":" + fromID
	if !dySvc.DMOutreachAllowed(ctx, accountID, fromID) {
		logger.Debugf("[DyDM-Outreach] 冷却中，跳过 account=%s user=%s", accountID, fromID)
		return
	}

	dmMsg := BuildDouyinDMWelcome(groupTitle, originalText)
	dmConvID := "dy_dm_" + fromID

	if err := dySvc.SendMessage(ctx, accountID, dmConvID, dmMsg); err != nil {
		logger.Warnf("[DyDM-Outreach] 私信发送失败 account=%s user=%s: %v", accountID, fromID, err)
		return
	}

	logger.Infof("[DyDM-Outreach] 群线索转私信成功 account=%s user=%s score=%d group=%s",
		accountID, fromID, score, groupID)

	s.recordDouyinDMOutreachEvent(ctx, accountID, fromID, groupID, score, dmMsg)
	_ = convKey
}

func (s *WebhookService) recordDouyinDMOutreachEvent(ctx context.Context, accountID, fromID, groupID string, score int, msg string) {
	if s.messageHubRepo == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[panic-recover] %T: %v\n%s", r, r, string(debug.Stack()))
		}
	}()

	hub := &model.MessageHub{
		Platform:       "douyin",
		AccountID:      accountID,
		MsgID:          "dy_dm_outreach_" + strconv.FormatInt(time.Now().UnixNano(), 10),
		Direction:      "outbound",
		MsgType:        "text",
		SenderID:       accountID,
		ReceiverID:     fromID,
		Content:        msg,
		ConversationID: "dy_dm_" + fromID,
		IsGroup:        false,
		SentAt:         time.Now(),
		IsAIReply:      true,
		AIAgent:        "lead_outreach",
		Extra: model.JSONMap{
			"scenario":     "group_to_dm",
			"trigger":      "high_intent_lead",
			"intent_score": score,
			"source_group": groupID,
			"channel":      "douyin",
		},
	}
	if err := s.messageHubRepo.Create(ctx, hub); err != nil {
		logger.Warnf("[DyDM-Outreach] 记录私信事件失败: %v", err)
	}
}

func (s *WebhookService) DouyinLeadMiner() func(ctx context.Context, ev *model.MessageEvent) {
	return func(ctx context.Context, ev *model.MessageEvent) {
		if ev == nil {
			return
		}
		channel := strings.ToLower(strings.TrimSpace(ev.Channel))

		if !isBridgeLeadMiningChannel(channel) {
			return
		}
		if strings.TrimSpace(ev.Content) == "" {
			return
		}
		if ev.SenderType == "agent" || ev.SenderType == "self" {
			return
		}

		accountID := ""
		groupName := ""
		if ev.Extra != nil {
			if v, ok := ev.Extra["account_id"]; ok {
				accountID = toString(v)
			}
			if v, ok := ev.Extra["group_name"]; ok {
				groupName = toString(v)
			}
		}

		hub := &model.MessageHub{
			Platform:       channel,
			AccountID:      accountID,
			MsgID:          ev.EventID,
			Direction:      "inbound",
			MsgType:        ev.MsgType,
			SenderID:       ev.SenderID,
			SenderName:     ev.SenderName,
			ReceiverID:     ev.ReceiverID,
			Content:        ev.Content,
			ConversationID: ev.ConversationID,
			IsGroup:        ev.IsGroup,
			GroupID:        ev.GroupID,
			Extra: model.JSONMap{
				"source":     "bridge",
				"group_name": groupName,
				"account_id": accountID,
			},
		}

		adapter := bridgeLeadAdapterForChannel(channel)
		MineUnifiedLead(ctx, s, hub, adapter, accountID, ev.GroupID, groupName, ev.SenderID, ev.SenderName, "", ev.Content)
	}
}

var leadMiningChannels = map[string]bool{
	"douyin": true, "tiktok": true, "kuaishou": true,
	"xiaohongshu": true, "xianyu": true,
}

var unsupportedLeadMiningChannels = map[string]string{
	"weibo":    "微博线索挖掘需要 Chrome 扩展 + Bridge 协议 + 微博平台 API",
	"taobao":   "淘宝线索挖掘需要 Chrome 扩展 + Bridge 协议",
	"pdd":      "拼多多线索挖掘需要 Chrome 扩展 + Bridge 协议",
	"jd":       "京东线索挖掘需要 Chrome 扩展 + Bridge 协议",
	"bilibili": "B站线索挖掘需要 Chrome 扩展 + Bridge 协议",
}

// RegisterLeadMiningChannel 注册一个 Bridge 协议的线索挖掘渠道（供外部模块扩展）
func RegisterLeadMiningChannel(channel string) { leadMiningChannels[channel] = true }

// GetUnsupportedLeadMiningReason 返回未支持渠道的原因（给 API 调用方友好提示）
func GetUnsupportedLeadMiningReason(channel string) string {
	return unsupportedLeadMiningChannels[channel]
}

func isBridgeLeadMiningChannel(channel string) bool {
	return leadMiningChannels[channel]
}

func toString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
