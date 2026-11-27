package service

import (
	"context"
	"errors"
	"strconv"
	"strings"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/model"
)

// ensureIncomingDisposition 在入站落库后登记逐消息处置台账。
// 群聊以 group_id 为会话根，单聊以 conversation_id / 稳定 SenderID 为根；
// 缺任一身份均 fail-closed，不把猜测的消息塞进回复水位。
func ensureIncomingDisposition(
	ctx context.Context,
	svc *ConversationDispositionService,
	in *IncomingContext,
	blind string,
) (*model.ConversationMessageDisposition, error) {
	if svc == nil || in == nil {
		return nil, errors.New("逐消息处置服务或入站上下文为空")
	}
	accountID, err := strconv.ParseUint(strings.TrimSpace(in.AccountID), 10, 64)
	if err != nil || accountID == 0 {
		return nil, errors.New("渠道账号 ID 无效")
	}
	conversationID := strings.TrimSpace(in.ConversationID)
	if in.IsGroup {
		conversationID = strings.TrimSpace(in.GroupID)
	} else if conversationID == "" {
		conversationID = strings.TrimSpace(in.SenderID)
	}
	if conversationID == "" {
		return nil, ErrConversationDispositionScope
	}
	messageID := strings.TrimSpace(in.MessageID)
	if messageID == "" {
		messageID = strings.TrimSpace(in.PlatformMsgID)
	}
	if messageID == "" {
		return nil, ErrConversationDispositionScope
	}
	return svc.Ensure(ctx, model.ConversationMessageDisposition{
		Platform:             string(in.Platform),
		ChannelAccountID:     uint(accountID),
		ConversationID:       conversationID,
		MessageID:            messageID,
		MessageCursor:        messageID,
		BusLLMSessionIDBlind: strings.TrimSpace(blind),
		Status:               model.ConversationDispositionPending,
		GroupID:              in.GroupID,
	})
}

// incomingDispositionRef 从权威入站元数据还原逐消息台账定位键。
func incomingDispositionRef(in *IncomingContext) (ConversationDispositionRef, error) {
	if in == nil {
		return ConversationDispositionRef{}, ErrConversationDispositionScope
	}
	accountID, err := strconv.ParseUint(strings.TrimSpace(in.AccountID), 10, 64)
	if err != nil || accountID == 0 {
		return ConversationDispositionRef{}, ErrConversationDispositionScope
	}
	conversationID := strings.TrimSpace(in.ConversationID)
	if in.IsGroup {
		conversationID = strings.TrimSpace(in.GroupID)
	} else if conversationID == "" {
		conversationID = strings.TrimSpace(in.SenderID)
	}
	messageID := strings.TrimSpace(in.MessageID)
	if messageID == "" {
		messageID = strings.TrimSpace(in.PlatformMsgID)
	}
	if conversationID == "" || messageID == "" {
		return ConversationDispositionRef{}, ErrConversationDispositionScope
	}
	return ConversationDispositionRef{
		Platform: string(in.Platform), ChannelAccountID: uint(accountID),
		ConversationID: conversationID, MessageID: messageID,
	}, nil
}

// markIncomingDisposition 在投递出口更新逐消息状态；台账缺失时只记日志，不改变投递决策。
func markIncomingDisposition(
	ctx context.Context,
	svc *ConversationDispositionService,
	in *IncomingContext,
	transition DispositionTransition,
) error {
	if svc == nil || in == nil {
		return nil
	}
	ref, err := incomingDispositionRef(in)
	if err != nil {
		return err
	}
	_, err = svc.TransitionCurrent(ctx, ref, transition)
	return err
}

// incomingDispositionBlind 从已构建的上下文快照取 scope hash 作为逐消息台账的关联键。
// 快照未装配时由调用方按 scope 确定性派生，避免使用可变昵称。
func incomingDispositionBlind(in *IncomingContext, view *agentcontext.AgentConversationContext) string {
	if view != nil && view.LLMConversation.ScopeHash != "" {
		return view.LLMConversation.ScopeHash
	}
	if in == nil {
		return ""
	}
	accountID, err := strconv.ParseUint(strings.TrimSpace(in.AccountID), 10, 64)
	if err != nil || accountID == 0 {
		return ""
	}
	scope := agentcontext.ConversationScope{
		TenantID: 0, Platform: string(in.Platform), ChannelAccountID: uint(accountID),
		ConversationID: in.ConversationID, StablePeerID: in.SenderID,
		IsGroup: in.IsGroup, GroupID: in.GroupID, BusLLMSessionID: in.BusLLMSessionID,
	}
	identity, err := agentcontext.DeriveScopeIdentity(scope)
	if err != nil {
		return ""
	}
	return identity.ScopeHash
}
