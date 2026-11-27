package service

import (
	"strings"

	agentcontext "hivemtk-user/internal/aiagent/context"
)

const (
	// GroupReplyReasonContextUnverified 表示群回复缺少 audience 已验证的上下文快照，
	// 或快照来源策略为 unproven；此时任何群回复都必须静默放弃。
	GroupReplyReasonContextUnverified = "group_context_unverified"
	groupVisibilityProofVerified      = "group_audienceVerified"
)

// GroupReplyDecision 是群回复发送前的机器可读判定；不携带任何原文。
type GroupReplyDecision struct {
	Allowed bool     `json:"allowed"`
	Reasons []string `json:"reasons,omitempty"`
}

// CheckGroupReplyGate 在群回复真正进入保存/投递前执行：
//  1. 群聊必须带 audience 已验证的不可变上下文快照，否则 fail-closed；
//  2. 输出不得包含其他群成员私有事实、手机号、邮箱或订单号；
//  3. 单聊不套用群聊公开范围约束。
func CheckGroupReplyGate(
	isGroup bool,
	view *agentcontext.AgentConversationContext,
	currentSenderID string,
	reply string,
) GroupReplyDecision {
	if !isGroup {
		return GroupReplyDecision{Allowed: true}
	}
	if view == nil || view.Provenance.VisibilityProof != groupVisibilityProofVerified {
		return GroupReplyDecision{Reasons: []string{GroupReplyReasonContextUnverified}}
	}

	policy := GroupOutputPolicy{}
	privateFacts := make([]string, 0, 4)
	collectPrivate := func(facts []agentcontext.BusinessFact) {
		for _, fact := range facts {
			if fact.SubjectType != "group_member" || fact.SubjectID == currentSenderID {
				continue
			}
			if value := strings.TrimSpace(fact.Value); value != "" {
				privateFacts = append(privateFacts, value)
			}
		}
	}
	collectPrivate(view.SharedBusiness)
	if view.Customer != nil {
		collectPrivate(view.Customer.Facts)
	}
	if view.Lead != nil {
		collectPrivate(view.Lead.Facts)
	}
	for _, msg := range view.Recent {
		if msg.SenderID != currentSenderID && msg.Visibility == agentcontext.VisibilityPrivate {
			if content := strings.TrimSpace(msg.Content); content != "" {
				privateFacts = append(privateFacts, content)
			}
		}
	}
	policy.PrivateFacts = privateFacts

	decision := NewGroupOutputPrivacyGuard().Check(reply, policy)
	if decision.Allowed {
		return GroupReplyDecision{Allowed: true}
	}
	return GroupReplyDecision{Reasons: decision.Reasons}
}
