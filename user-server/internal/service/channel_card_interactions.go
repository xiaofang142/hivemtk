package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// channel_card_interactions.go 卡片发送侧的交互上下文登记（specs/024 T211）。
//
// 只解决一件事：客户点了我们发的卡片后，系统要能回答「点的是哪条卡片、哪个动作」。
// 所以在发送成功后把 (channel, interaction_id, platform_msg_id, actions) 落一行 pending；
// 点击回调（T209）按 interaction_id 命中再 MarkAnswered。
//
// 约定：
//   - 一张卡片一行（不是一颗按钮一行）：按钮集合整体放在 Payload.actions 里，
//     Telegram 的 callback_data 只需携带这 16 字节 id，稳在 64 字节上限内；
//   - interaction_id 在发送前生成（callback_data 必须先写进键盘），发送失败不登记；
//   - TTL 取 config_param `channel_interaction_ttl_days`（默认 7 天）。

const (
	cardInteractionTTLGroup    = "misc"
	cardInteractionTTLKey      = "channel_interaction_ttl_days"
	cardInteractionTTLFallback = 7
)

// newCardInteractionID 生成卡片交互 id（`ci_` + 16 hex = 19 字节）。
// 用 crypto/rand 而不是自增：id 会作为 callback_data 出现在客户设备上，
// 自增号会泄露发送量，也无法在多实例下无冲突生成。
func newCardInteractionID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return "ci_" + hex.EncodeToString(b[:])
}

// cardInteractionTTL 取卡片交互的有效期（天 → time.Duration）。
func cardInteractionTTL(ctx context.Context) time.Duration {
	days := GlobalConfigParam().GetInt(ctx, cardInteractionTTLGroup, cardInteractionTTLKey, cardInteractionTTLFallback)
	if days <= 0 {
		days = cardInteractionTTLFallback
	}
	return time.Duration(days) * 24 * time.Hour
}

// cardActionButtons 返回卡片里真正可交互的按钮（有 Action）及其序号。
// URL 按钮是纯跳转，不产生回调，不需要登记。
func cardActionButtons(card model.RichCard) []string {
	actions := make([]string, 0, len(card.Buttons))
	for _, btn := range card.Buttons {
		if btn.Action != "" && btn.URL == "" {
			actions = append(actions, btn.Action)
		}
	}
	return actions
}

// planCardInteraction 为一颗卡片分配交互 id；没有可交互按钮返回空串（无需登记）。
func planCardInteraction(card model.RichCard) string {
	if len(cardActionButtons(card)) == 0 {
		return ""
	}
	return newCardInteractionID()
}

// registerCardInteraction 发送成功后登记交互上下文。
//
// 失败只记日志：卡片已经送达客户手里，登记失败不该让发送被判失败
// （那会触发重试 → 客户收到重复卡片），代价只是这次点击查不到上下文。
func (s *WebhookService) registerCardInteraction(ctx context.Context, channel WebhookChannel, accountID, platformMsgID, interactionID string, card model.RichCard, hubMsg *model.MessageHub) {
	if s == nil || s.cardInteractionRepo == nil || interactionID == "" || platformMsgID == "" {
		return
	}
	actions := cardActionButtons(card)
	if len(actions) == 0 {
		return
	}
	payload := model.JSONMap{"actions": actions}
	if card.Title != "" {
		payload["title"] = card.Title
	}
	row := &model.ChannelInteraction{
		Channel:       string(channel),
		AccountID:     accountID,
		InteractionID: interactionID,
		PlatformMsgID: platformMsgID,
		Action:        actions[0],
		Payload:       payload,
		Status:        model.ChannelInteractionPending,
	}
	if hubMsg != nil {
		row.ConversationID = hubMsg.ConversationID
		// MessageHub 的租户键是 uint，交互表是 string（平台事件带字符串租户键）；
		// 这里只做十进制转写，不做业务映射。
		row.TenantID = strconv.FormatUint(uint64(hubMsg.TenantID), 10)
	}
	expires := time.Now().Add(cardInteractionTTL(ctx))
	row.ExpiresAt = &expires
	if _, err := s.cardInteractionRepo.CreateIdempotent(ctx, row); err != nil {
		logger.Ctx(ctx).Warn().Err(err).Str("channel", string(channel)).
			Str("platform_msg_id", platformMsgID).Str("interaction_id", interactionID).
			Msg("[Outbound] 卡片交互上下文登记失败")
	}
}
