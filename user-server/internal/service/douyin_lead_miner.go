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
	// 抖音/TikTok 共用同一套入站解析，落库平台已按渠道区分（D-01）；
	// 线索也必须按 hub.Platform 选适配器，否则 TikTok 群线索会记成抖音类型。
	return MineUnifiedLead(ctx, s, hub, douyinLeadAdapterForHub(hub), accountID, groupID, groupTitle, fromID, fromName, "", text)
}

func douyinLeadAdapterForHub(hub *model.MessageHub) ChannelLeadAdapter {
	if hub != nil && hub.Platform == string(ChannelTiktok) {
		return bridgeLeadAdapterForChannel(string(ChannelTiktok))
	}
	return DouyinLeadAdapter{}
}

// bridgeDMKeyShort 私信冷却键 / 会话 ID / 幂等键里的渠道短名。
// 抖音必须保持历史 "dy"，否则线上已写入的冷却键会失效（等于重置一次骚扰冷却）。
func bridgeDMKeyShort(channel string) string {
	switch channel {
	case string(ChannelDouyin), "":
		return "dy"
	case string(ChannelTiktok):
		return "tt"
	case "xiaohongshu":
		return "xhs"
	case "kuaishou":
		return "ks"
	case "xianyu":
		return "xy"
	default:
		return channel
	}
}

// triggerBridgeDMOutreach 群线索转私信。渠道必须由调用方带进来：
// 此前写死 douyin，TikTok/小红书/快手/闲鱼的商机会被投进抖音出站队列（D-01）。
func (s *WebhookService) triggerBridgeDMOutreach(ctx context.Context, channel, accountID, fromID, groupID, groupTitle string, score int, originalText string) {
	if strings.TrimSpace(channel) == "" {
		channel = string(ChannelDouyin)
	}
	short := bridgeDMKeyShort(channel)

	if !dmOutreachAllowed(ctx, "mtk:"+short+":dm_outreach:"+accountID+":"+fromID) {
		logger.Debugf("[BridgeDM-Outreach] 冷却中，跳过 channel=%s account=%s user=%s", channel, accountID, fromID)
		return
	}

	dmMsg := BuildDouyinDMWelcome(groupTitle, originalText)
	dmConvID := short + "_dm_" + fromID

	if err := DeliverBridgeOutbound(ctx, channel, accountID, dmConvID, "text", dmMsg, ""); err != nil {
		logger.Warnf("[BridgeDM-Outreach] 私信发送失败 channel=%s account=%s user=%s: %v", channel, accountID, fromID, err)
		return
	}

	logger.Infof("[BridgeDM-Outreach] 群线索转私信成功 channel=%s account=%s user=%s score=%d group=%s",
		channel, accountID, fromID, score, groupID)

	s.annotateBridgeDMOutreachEvent(ctx, channel, accountID, fromID, groupID, dmConvID, score, dmMsg)
}

// annotateBridgeDMOutreachEvent 把「群转私」的归因信息写回已入队的那一行 message_hub。
//
// 原先是再插一行同内容、同 conversation_id 的记录：
//  1. 不写 status 时 gorm 默认 pending，桥接 outbox 会把这条私信再取走发第二遍；
//  2. 即便标成 sent，ListByConversation 只按 conversation_id 过滤，
//     会话流里仍会出现两条一模一样的私信气泡。
func (s *WebhookService) annotateBridgeDMOutreachEvent(ctx context.Context, channel, accountID, fromID, groupID, convID string, score int, msg string) {
	if s.messageHubRepo == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[panic-recover] %T: %v\n%s", r, r, string(debug.Stack()))
		}
	}()

	extra := model.JSONMap{
		"scenario":     "group_to_dm",
		"trigger":      "high_intent_lead",
		"intent_score": score,
		"source_group": groupID,
		"channel":      channel,
	}

	row, err := s.messageHubRepo.GetByPlatformAccountMsgID(ctx, channel, accountID, ContentHashMsgID(channel, convID, msg))
	if err != nil || row == nil || row.ID == 0 {
		// 入队行读不到时退回独立留痕行，status 必须显式 sent，否则又会进 outbox 被发第二遍。
		fallback := &model.MessageHub{
			Platform:       channel,
			AccountID:      accountID,
			MsgID:          bridgeDMKeyShort(channel) + "_dm_outreach_" + strconv.FormatInt(time.Now().UnixNano(), 10),
			Direction:      "outbound",
			Status:         "sent",
			MsgType:        "text",
			SenderID:       accountID,
			ReceiverID:     fromID,
			Content:        msg,
			ConversationID: convID,
			SentAt:         time.Now(),
			IsAIReply:      true,
			AIAgent:        "lead_outreach",
			Extra:          extra,
		}
		if cerr := s.messageHubRepo.Create(ctx, fallback); cerr != nil {
			logger.Warnf("[BridgeDM-Outreach] 记录私信事件失败: %v", cerr)
		}
		return
	}

	row.ReceiverID = fromID
	row.IsAIReply = true
	row.AIAgent = "lead_outreach"
	row.Extra = extra
	if uerr := s.messageHubRepo.Update(ctx, row); uerr != nil {
		logger.Warnf("[BridgeDM-Outreach] 回写私信归因失败: %v", uerr)
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
