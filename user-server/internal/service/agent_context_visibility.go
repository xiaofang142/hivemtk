package service

import (
	"strings"
	"time"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/model"
)

// VisibilityOmission 记录一条消息未被纳入群聊上下文的原因。
type VisibilityOmission struct {
	MessageID string
	Reason    string
}

// VisibilityFilterResult 保持原始消息顺序，只返回对当前成员可证明可见的消息。
type VisibilityFilterResult struct {
	Visible []agentcontext.ContextMessage
	Omitted []VisibilityOmission
}

// FilterVisibleGroupMessages 按消息可见性和成员入退群区间执行 fail-closed 过滤。
// 当前消息由调用方单独保留；本函数只处理历史消息，未知可见性永远不会授权。
func FilterVisibleGroupMessages(
	messages []agentcontext.ContextMessage,
	memberships []model.ConversationMembership,
	scope agentcontext.ConversationScope,
	memberID string,
) VisibilityFilterResult {
	result := VisibilityFilterResult{
		Visible: []agentcontext.ContextMessage{},
		Omitted: []VisibilityOmission{},
	}
	if !validGroupVisibilityScope(scope) {
		omitAllMessages(&result, messages, "invalid_scope")
		return result
	}
	memberID = strings.TrimSpace(memberID)
	if memberID == "" {
		omitAllMessages(&result, messages, "missing_member")
		return result
	}

	for _, message := range messages {
		reason, visible := groupMessageVisible(message, memberships, scope, memberID)
		if visible {
			result.Visible = append(result.Visible, message)
			continue
		}
		result.Omitted = append(result.Omitted, VisibilityOmission{
			MessageID: message.MessageID,
			Reason:    reason,
		})
	}
	return result
}

func validGroupVisibilityScope(scope agentcontext.ConversationScope) bool {
	return scope.TenantID > 0 &&
		strings.TrimSpace(scope.Platform) != "" &&
		scope.ChannelAccountID > 0 &&
		scope.IsGroup &&
		strings.TrimSpace(scope.GroupID) != ""
}

func groupMessageVisible(
	message agentcontext.ContextMessage,
	memberships []model.ConversationMembership,
	scope agentcontext.ConversationScope,
	memberID string,
) (string, bool) {
	switch message.Visibility {
	case agentcontext.VisibilityPublic:
		return "", true
	case agentcontext.VisibilityPrivate:
		if audienceContains(message.Audience, memberID) {
			return "", true
		}
		return "audience_denied", false
	case agentcontext.VisibilityMember:
		if audienceContains(message.Audience, memberID) {
			return "", true
		}
		start, end, ok := messageVisibilityInterval(message)
		if !ok {
			return "invalid_visibility_interval", false
		}
		for _, membership := range memberships {
			if membershipCovers(membership, scope, memberID, start, end) {
				return "", true
			}
		}
		return "membership_unproven", false
	case agentcontext.VisibilityUnknown:
		return "visibility_unproven", false
	default:
		return "invalid_visibility", false
	}
}

func messageVisibilityInterval(message agentcontext.ContextMessage) (time.Time, time.Time, bool) {
	start := message.SentAt
	if message.VisibleFrom != nil {
		start = *message.VisibleFrom
	}
	end := message.SentAt
	if message.VisibleTo != nil {
		end = *message.VisibleTo
	}
	return start, end, !end.Before(start)
}

func membershipCovers(
	membership model.ConversationMembership,
	scope agentcontext.ConversationScope,
	memberID string,
	messageStart time.Time,
	messageEnd time.Time,
) bool {
	if membership.TenantID != scope.TenantID ||
		!strings.EqualFold(strings.TrimSpace(membership.Platform), strings.TrimSpace(scope.Platform)) ||
		membership.ChannelAccountID != scope.ChannelAccountID ||
		strings.TrimSpace(membership.GroupID) != strings.TrimSpace(scope.GroupID) ||
		strings.TrimSpace(membership.MemberID) != memberID ||
		strings.TrimSpace(membership.EvidenceSource) == "" ||
		strings.TrimSpace(membership.EvidenceRef) == "" {
		return false
	}
	if membership.JoinedAt != nil && membership.JoinedAt.After(messageStart) {
		return false
	}
	if membership.LeftAt == nil {
		return true
	}
	if messageEnd.After(messageStart) {
		return !membership.LeftAt.Before(messageEnd)
	}
	return membership.LeftAt.After(messageStart)
}

func audienceContains(audience []string, memberID string) bool {
	for _, candidate := range audience {
		if strings.TrimSpace(candidate) == memberID {
			return true
		}
	}
	return false
}

func omitAllMessages(result *VisibilityFilterResult, messages []agentcontext.ContextMessage, reason string) {
	for _, message := range messages {
		result.Omitted = append(result.Omitted, VisibilityOmission{
			MessageID: message.MessageID,
			Reason:    reason,
		})
	}
}
