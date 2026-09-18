package service

import (
	"context"

	"fmt"

	"hivemtk-user/internal/dto"

	"hivemtk-user/internal/model"

	"hivemtk-user/internal/pkg/utils/logger"
)

func (e *SalesEngine) shouldTransferToHuman(ctx context.Context, intent *dto.RecognizeResult, mem *model.DialogueMemory, req *SalesRequest) (bool, string) {
	if intent == nil {
		return false, ""
	}

	if intent.Confidence >= 0.7 &&
		intent.IntentType != IntentComplaint &&
		intent.IntentType != IntentChurn {
		return false, ""
	}

	if e.confidenceAggregator != nil {
		return e.shouldTransferByConfidence(ctx, intent, mem, req)
	}

	switch intent.IntentType {
	case IntentChurn, IntentComplaint:
		return true, e.transferReason(ctx, intent, mem)
	}
	return false, ""
}

func (e *SalesEngine) shouldTransferByConfidence(ctx context.Context, intent *dto.RecognizeResult, mem *model.DialogueMemory, req *SalesRequest) (bool, string) {
	if e.confidenceAggregator == nil {
		return false, ""
	}

	in := &dto.SignalCollectionInput{
		SessionID:     req.SessionID,
		CustomerID:    req.CustomerID,
		Text:          req.UserMessage,
		IntentType:    intent.IntentType,
		RawIntentConf: intent.Confidence,

		CustomerLevel:     inferCustomerLevelFromReq(req),
		AgentAvailability: 0.5,
		LastTurns:         extractLastTurnsFromMem(mem),
	}

	dec, err := e.confidenceAggregator.Aggregate(ctx, in)
	if err != nil {

		logger.Ctx(ctx).Warn().Err(err).Msg("[sales] confidence aggregate failed, fallback to static rule")
		switch intent.IntentType {
		case IntentChurn, IntentComplaint:
			return true, e.transferReason(ctx, intent, mem)
		}
		return false, ""
	}

	decisionLabel := ""
	switch dec.DecisionBand {
	case dto.BandHandoff:
		decisionLabel = "transfer"
	case dto.BandLLMFallback:
		decisionLabel = "llm_fallback"
	case dto.BandReview:
		decisionLabel = "low_confidence_hold"
	default:
		decisionLabel = "auto_reply"
	}
	_ = decisionLabel

	if dec.VetoTriggered != "" {
		return true, "一票否决: " + dec.VetoTriggered
	}

	switch dec.DecisionBand {
	case dto.BandHandoff:
		return true, fmt.Sprintf("低置信度转人工 (aggregated=%.2f < threshold=%.2f)", dec.AggregatedConf, dec.DynamicThreshold)
	case dto.BandLLMFallback, dto.BandReview:

		return false, ""
	default:
		return false, ""
	}
}

func inferCustomerLevelFromReq(req *SalesRequest) string {
	if req == nil || req.Config == nil {
		return "normal"
	}
	if req.Config.CustomerLevel != "" {
		return req.Config.CustomerLevel
	}
	return "normal"
}

func extractLastTurnsFromMem(mem *model.DialogueMemory) []string {
	if mem == nil || len(mem.KeyFacts) == 0 {
		return nil
	}
	out := make([]string, 0, 3)
	for k, v := range mem.KeyFacts {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, k+": "+s)
			if len(out) >= 3 {
				break
			}
		}
	}
	return out
}

func (e *SalesEngine) transferReason(ctx context.Context, intent *dto.RecognizeResult, mem *model.DialogueMemory) string {
	if intent != nil {
		switch intent.IntentType {
		case IntentChurn:
			return "客户出现流失倾向，建议人工介入挽留"
		case IntentComplaint:
			return "客户正在投诉，需要人工处理"
		}
	}
	if mem != nil && mem.MessageCount > 30 {
		return "对话轮数过多，建议人工接管"
	}
	return "触发转人工规则"
}

func truncateForReply(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "…"
}

func customerNameOf(c *model.Customer) string {
	if c == nil {
		return ""
	}
	return c.Phone
}

// ChannelMessage 渠道无关的入站消息（WebhookService → SalesEngine 桥接）
type ChannelMessage struct {
	Channel      string `json:"channel"`
	AccountID    string `json:"account_id"`
	ExternalUser string `json:"external_user"`
	Nickname     string `json:"nickname"`
	Content      string `json:"content"`
	MsgType      string `json:"msg_type"`
	ChatID       string `json:"chat_id"`
	IsGroup      bool   `json:"is_group"`
	MediaURL     string `json:"media_url"`
	RawData      string `json:"raw_data"`
	ReceivedAt   int64  `json:"received_at"`
}

func (e *SalesEngine) normalizeChannelMessage(ctx context.Context, msg *ChannelMessage) (content, sessionID, customerID string) {

	if msg.MediaURL != "" {
		switch msg.MsgType {
		case "image":
			content = "[图片] " + msg.Content
		case "voice":
			content = "[语音] " + msg.Content
		case "video":
			content = "[视频] " + msg.Content
		case "file":
			content = "[文件] " + msg.Content
		default:
			content = msg.Content
		}
	} else {
		content = msg.Content
	}

	if msg.ChatID != "" {
		sessionID = msg.Channel + ":" + msg.ChatID
	} else {
		sessionID = msg.Channel + ":" + msg.AccountID + ":" + msg.ExternalUser
	}

	customerID = msg.Channel + ":" + msg.ExternalUser
	return
}
