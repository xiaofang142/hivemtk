package service

import (
	"strings"
	"time"

	"hivemtk-user/internal/model"
)

func bridgeLeadEventEligible(event *model.MessageEvent, accountID string, notice bool, now time.Time) bool {
	// P0-c：资格源改为 channelgw.Default 的 MineMode 投影（service.SetLeadMiningChannels），
	// 不再读两处硬编码 map。投影未装配（单测/异常时序）时不阻断，保持 legacy 行为。
	if event == nil || notice || (len(LeadMiningChannels()) > 0 && !leadMiningChannelCapable(event.Channel)) {
		return false
	}
	if event.SenderType != "customer" && event.SenderType != "user" {
		return false
	}
	if event.IsAIReply || strings.TrimSpace(event.Content) == "" || event.EventID == "" ||
		event.SenderID == "" || event.ConversationID == "" || accountID == "" || accountID == "default" {
		return false
	}
	if event.Timestamp.IsZero() || event.Timestamp.Before(now.Add(-InboxReplyWindow)) || event.Timestamp.After(now.Add(InboxBackfillFutureTolerance)) {
		return false
	}
	return true
}
