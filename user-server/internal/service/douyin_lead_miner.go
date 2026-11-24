package service

// P0-c 收口：本文件原先承载 Bridge 专用 miner（DouyinLeadMiner/ProcessBridgeLead/
// mineDouyinGroupLead）、硬编码 leadMiningChannels map 与 RegisterLeadMiningChannel /
// isBridgeLeadMiningChannel。该双轨发掘已由各渠道 handler 落 hub 后的统一
// LeadDiscoveryService.Process → Service.Enqueue 取代，资格源改 channelgw.Default
// 的 MineMode 能力位投影（service.SetLeadMiningChannels）。
//
// 只保留与发掘判定无关的「群线索转私信」出站能力：渠道短名、冷却键、欢迎语投递
// 与归因回写。这些函数被 lead_miner_adapters.go 的 TriggerOutreach 调用，不读线索
// 资格表、不写线索库，删除会断掉已存在的主动触达链路。

import (
	"context"
	"runtime/debug"
	"strconv"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// douyinLeadAdapterForHub 按 hub 落库平台选适配器（D-01：TikTok 不能被记成抖音）。
// 仅用于适配器工厂测试与触达口径；发掘写库已统一走主 Service.process。
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
	if s == nil || !bridgeDMOutreachIdentityValid(channel, accountID, fromID) {
		logger.Ctx(ctx).Warn().Msg("bridge outreach rejected: channel or identity unconfirmed")
		return
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
			logger.Errorf("[panic-recover] %T: %v\n%s", r, r, string(debug.Stack()))
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
