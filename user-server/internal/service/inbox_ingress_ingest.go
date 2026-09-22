package service

import (
	"context"
	"time"
	"unicode"

	"errors"

	"hivemtk-user/internal/model"

	"strings"

	"gorm.io/gorm"
)

type IngressDecision struct {
	Blocked    bool
	IsSelfEcho bool
	IsDup      bool
	Reason     string
}

func (s *InboxIngressService) resolveSenderKey(event *model.MessageEvent) string {
	if event.SenderName != "" {
		return event.SenderName
	}
	if event.SenderID != "" {
		return event.SenderID
	}
	if event.ConversationID != "" {
		return event.ConversationID
	}
	return "unknown"
}

func resolveAccountID(event *model.MessageEvent) string {
	if event.Extra != nil {
		if v, ok := event.Extra["account_id"].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func channelMsgIDOf(event *model.MessageEvent) string {
	if event == nil || event.Extra == nil {
		return ""
	}
	id, _ := event.Extra["channel_msg_id"].(string)
	if id == "" || strings.HasPrefix(id, "wa-out-") || strings.HasPrefix(id, "tg-out-") {
		return ""
	}
	return id
}

func (s *InboxIngressService) senderKeyForDedup(event *model.MessageEvent) string {
	sk := s.resolveSenderKey(event)
	if event.SenderType == "self" || event.SenderType == "agent" {
		if acc := resolveAccountID(event); acc != "" {
			sk = acc
		}
	}
	return sk
}

func (s *InboxIngressService) senderDefinitelyDiffers(event *model.MessageEvent, ob *model.MessageHub) bool {
	if event == nil || ob == nil {
		return false
	}
	switch event.SenderType {
	case "self", "agent":
		return false
	}
	inSender := strings.TrimSpace(event.SenderID)
	outSender := strings.TrimSpace(ob.SenderID)
	if inSender == "" || outSender == "" {
		return false
	}
	return inSender != outSender
}

func (s *InboxIngressService) interceptInbound(ctx context.Context, event *model.MessageEvent) (*IngressDecision, error) {
	if s.hubRepo == nil {

		return &IngressDecision{}, nil
	}
	content := strings.TrimSpace(event.Content)
	if content == "" || event.Channel == "" {

		return &IngressDecision{}, nil
	}

	chanMsgID := channelMsgIDOf(event)
	if chanMsgID != "" && s.hubRepo != nil {
		accID := resolveAccountID(event)
		if event.ConversationID != "" && accID != "" {

			if _, err := s.hubRepo.GetOutgoingByPlatformMsgIDInConv(ctx, event.Channel, accID, event.ConversationID, chanMsgID); err == nil {
				return &IngressDecision{Blocked: true, IsSelfEcho: true, Reason: "self-echo(platform msg_id exact match)"}, nil
			}
		} else if _, err := s.hubRepo.GetOutgoingByPlatformMsgID(ctx, event.Channel, accID, chanMsgID); err == nil {
			return &IngressDecision{Blocked: true, IsSelfEcho: true, Reason: "self-echo(platform msg_id exact match)"}, nil
		}
	}

	if ob, oerr := s.hubRepo.GetOutboundByPlatformSenderContentConv(ctx, event.Channel, event.SenderName, content, event.ConversationID); oerr == nil && ob != nil && !s.senderDefinitelyDiffers(event, ob) {
		return &IngressDecision{Blocked: true, IsSelfEcho: true, Reason: "self-echo(matched outbound by platform+sender_name+content)"}, nil
	}

	if event.ConversationID != "" && s.hubRepo != nil {
		rows, rerr := s.hubRepo.ListRecentOutboundInConv(ctx, event.Channel, resolveAccountID(event), event.ConversationID, time.Now().Add(-2*time.Hour), 20)
		if rerr == nil && len(rows) > 0 {
			norm := normalizeEchoText(content)
			if norm != "" {
				for i := range rows {
					if normalizeEchoText(rows[i].Content) == norm {
						return &IngressDecision{Blocked: true, IsSelfEcho: true, Reason: "self-echo(recent outbound normalized match)"}, nil
					}
				}
			}
		}
	}

	// 内容窗口去重只对「平台不给稳定消息 ID」的渠道有独立价值。带 channel_msg_id 的事件
	// 必须跳过：客户连发两条同样内容（或一次推两张图片、占位正文同为 "[图片]"）是两条真实
	// 消息，按内容相同就拦会让第二条既不入库也不回复（N-14 实测）。平台 at-least-once 重投
	// 本来由 message_hub 的 (platform, msg_id, conversation_id) 唯一索引 + isDuplicateKey 幂等兜底。
	if s.cache != nil && chanMsgID == "" {
		dedupHash := ContentHashWithSender(event.Channel, s.senderKeyForDedup(event), content)
		dupKey := InboxSenderContentDedupKey + dedupHash
		if ok, derr := s.cache.SetNX(ctx, dupKey, "1", InboxContentDedupTTL); derr == nil && !ok {
			return &IngressDecision{Blocked: true, IsDup: true, Reason: "duplicate(channel+sender+content) within window"}, nil
		}
	}

	return &IngressDecision{}, nil
}

func isDuplicateKey(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "commit unexpectedly resulted in rollback") ||
		errors.Is(err, gorm.ErrDuplicatedKey)
}

func groupNameOf(event *model.MessageEvent) string {
	if event == nil {
		return ""
	}
	if event.Extra != nil {
		if v, ok := event.Extra["group_name"]; ok {
			if s, _ := v.(string); s != "" {
				return s
			}
		}
	}
	return ""
}

func normalizeEchoText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
