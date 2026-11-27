package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/pkg/tenant"
)

// buildIncomingContextView 在编排器入口为单次入站构建不可变上下文快照。
// 构建器未装配时安全降级到原有有限历史路径；一旦装配，缺失权威元数据即
// fail-closed，避免把猜测的 scope 当成已验证事实。
func buildIncomingContextView(
	ctx context.Context,
	in *IncomingContext,
	sessionID string,
	builder *ConversationContextBuilder,
	now time.Time,
	providers ...ContextFactProvider,
) (*agentcontext.AgentConversationContext, error) {
	if builder == nil {
		return nil, nil
	}
	if in == nil {
		return nil, errors.New("入站上下文为空")
	}
	tid, ok := tenant.FromContext(ctx)
	if !ok || tid == 0 || tenant.IsSystemScope(ctx) {
		return nil, errors.New("上下文租户范围无效")
	}
	accountID, err := strconv.ParseUint(strings.TrimSpace(in.AccountID), 10, 64)
	if err != nil || accountID == 0 {
		return nil, errors.New("渠道账号 ID 不是有效正整数，无法构建上下文")
	}
	policyKind := agentcontext.SourcePolicyDM
	if in.IsGroup {
		policyKind = agentcontext.SourcePolicyGroup
	}
	sentAt := in.OriginalSentAt
	if sentAt.IsZero() {
		sentAt = now
	}
	visibility := in.Visibility
	if visibility == "" {
		visibility = agentcontext.VisibilityPublic
		if in.IsGroup {
			// 群聊缺权威可见性证明时 fail-closed，不能默认为公开。
			visibility = agentcontext.VisibilityUnknown
		}
	}
	current := agentcontext.ContextMessage{
		MessageID:     strings.TrimSpace(in.MessageID),
		Origin:        agentcontext.OriginMessageHub,
		HistorySource: agentcontext.HistorySourceMessageHub,
		Role:          "user",
		Direction:     "inbound",
		SenderID:      strings.TrimSpace(in.SenderID),
		SenderName:    strings.TrimSpace(in.SenderName),
		Content:       in.Content,
		SentAt:        sentAt,
		Visibility:    visibility,
		Audience:      append([]string{}, in.Audience...),
		CorrelationID: strings.TrimSpace(in.CorrelationID),
		SourceMetadata: map[string]string{
			"platform_msg_id": strings.TrimSpace(in.PlatformMsgID),
			"bus_session_id":  strings.TrimSpace(in.BusSessionID),
		},
	}
	if current.MessageID == "" {
		return nil, errors.New("当前消息缺少真实 ID，无法构建上下文")
	}
	var facts *AgentContextFacts
	if len(providers) > 0 && providers[0] != nil {
		var err error
		facts, err = providers[0].Load(ctx, in, sessionID, now)
		if err != nil {
			return nil, err
		}
	}
	return builder.Build(ctx, AgentContextBuilderInput{
		Scope: agentcontext.ConversationScope{
			TenantID:         tid,
			Platform:         strings.TrimSpace(string(in.Platform)),
			ChannelAccountID: uint(accountID),
			BusSessionID:     in.BusSessionID,
			BusLLMSessionID:  in.BusLLMSessionID,
			SessionID:        sessionID,
			ConversationID:   in.ConversationID,
			StablePeerID:     in.SenderID,
			IsGroup:          in.IsGroup,
			GroupID:          in.GroupID,
		},
		Current: current,
		Customer: func() *agentcontext.CustomerContext {
			if facts == nil {
				return nil
			}
			return facts.Customer
		}(),
		Lead: func() *agentcontext.LeadContext {
			if facts == nil {
				return nil
			}
			return facts.Lead
		}(),
		SharedBusiness: func() []agentcontext.BusinessFact {
			if facts == nil {
				return nil
			}
			return facts.SharedBusiness
		}(),
		OlderSummary: func() *agentcontext.MemorySummary {
			if facts == nil {
				return nil
			}
			return facts.OlderSummary
		}(),
		Participants: func() []agentcontext.Participant {
			if facts == nil {
				return nil
			}
			return facts.Participants
		}(),
		PromptVersion: "reply-chain-v1",
		PolicyKind:    string(policyKind),
		InputBudget: agentcontext.ContextBudget{
			HistoryTokens:      4096,
			MaxInputTokens:     8192,
			MaxOutputTokens:    4096,
			SafetyMarginTokens: 512,
		},
		OutputBudget: agentcontext.OutputBudget{MaxOutputTokens: 4096},
		Now:          now,
	})
}
