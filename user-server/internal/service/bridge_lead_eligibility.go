package service

import (
	"strings"
	"time"

	"hivemtk-user/internal/model"
)

func bridgeLeadEventEligible(event *model.MessageEvent, accountID string, notice bool, now time.Time) bool {
	if event == nil || notice || !isBridgeLeadMiningChannel(event.Channel) {
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
