package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

// LeadContextViewProvider 为单次线索判定构建与回复链同构的不可变上下文快照。
//
// 参数全部来自权威入站元数据（MessageHub / webhook），不得来自消息正文或渠道
// Extra 的非白名单键。accountID 必须是数值渠道账号 ID（不是识别码/账号名），
// messageID 必须是 MessageHub.MsgID，否则 fail-closed 返回错误。
// 返回的 scope 与 view.Scope 同源；缺 scope 时调用方从 view.Scope 取值。
type LeadContextViewProvider func(
	ctx context.Context,
	platform string,
	accountID string,
	conversationID string,
	groupID string,
	senderID string,
	messageID string,
	isGroup bool,
) (*agentcontext.AgentConversationContext, *agentcontext.ConversationScope, error)

// SetContextViewProvider 注入线索链统一上下文提供方；nil 时保持既有降级行为
// （判定仍走有限历史路径），单测/未装配不需要任何 DB 依赖。
func (s *Service) SetContextViewProvider(provider LeadContextViewProvider) {
	if s == nil {
		return
	}
	s.contextViewProviderMu.Lock()
	s.contextViewProvider = provider
	s.contextViewProviderMu.Unlock()
}

func (s *Service) leadContextViewProvider() LeadContextViewProvider {
	if s == nil {
		return nil
	}
	s.contextViewProviderMu.RLock()
	defer s.contextViewProviderMu.RUnlock()
	return s.contextViewProvider
}

// NewLeadContextViewProvider 由装配点用统一上下文构建器构造线索链 provider。
//
// 线索链没有回复链那样现成的 BusLLMSessionID，这里按 DeriveScopeIdentity 从
// 冻结作用域派生确定性的 canonical session（同一租户/平台/账号/会话 ⇒ 同一 key），
// 让 lead_detect / lead_judge 的 LLM 请求绑定到同一会话而不是按消息新建。
//
// 可选的 factProviders 透传给 buildIncomingContextView，让线索链也能消费与回复链
// 同一批结构化业务事实（例如另一智能体写入的备注/标签，经分级后进入 ContextView）。
// builder 为 nil 时返回 nil provider（保持既有行为）。
func NewLeadContextViewProvider(builder *ConversationContextBuilder, factProviders ...ContextFactProvider) LeadContextViewProvider {
	if builder == nil {
		return nil
	}
	return func(
		ctx context.Context,
		platform string,
		accountID string,
		conversationID string,
		groupID string,
		senderID string,
		messageID string,
		isGroup bool,
	) (*agentcontext.AgentConversationContext, *agentcontext.ConversationScope, error) {
		tid, ok := tenant.FromContext(ctx)
		if !ok || tid == 0 || tenant.IsSystemScope(ctx) {
			return nil, nil, errors.New("线索上下文缺少有效租户范围")
		}
		accountText := strings.TrimSpace(accountID)
		accountNum, err := strconv.ParseUint(accountText, 10, 64)
		if err != nil || accountNum == 0 {
			return nil, nil, errors.New("线索上下文渠道账号 ID 不是有效正整数")
		}
		seed := agentcontext.ConversationScope{
			TenantID:         tid,
			Platform:         strings.TrimSpace(platform),
			ChannelAccountID: uint(accountNum),
			ConversationID:   strings.TrimSpace(conversationID),
			GroupID:          strings.TrimSpace(groupID),
			StablePeerID:     strings.TrimSpace(senderID),
			IsGroup:          isGroup,
		}
		identity, err := agentcontext.DeriveScopeIdentity(seed)
		if err != nil {
			return nil, nil, err
		}
		in := &IncomingContext{
			Platform:        model.Platform(seed.Platform),
			AccountID:       accountText,
			SenderID:        seed.StablePeerID,
			MessageID:       strings.TrimSpace(messageID),
			IsGroup:         isGroup,
			GroupID:         seed.GroupID,
			ConversationID:  seed.ConversationID,
			Direction:       "inbound",
			BusLLMSessionID: identity.BusLLMSessionID,
		}
		view, err := buildIncomingContextView(ctx, in, "", builder, time.Now(), factProviders...)
		if err != nil || view == nil {
			return nil, nil, err
		}
		scope := view.Scope
		return view, &scope, nil
	}
}
