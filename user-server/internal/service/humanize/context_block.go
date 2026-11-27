package humanize

import (
	"strings"

	agentcontext "hivemtk-user/internal/aiagent/context"
)

// BuildHumanizeContextBlock 把统一上下文渲染成拟人评估/重生可消费的结构化块。
// 只消费可证明可见的历史与 confirmed 事实；建议、摘要、待核实旧备注不得
// 被复述为既成事实。
func BuildHumanizeContextBlock(view *agentcontext.AgentConversationContext) string {
	if view == nil {
		return "【会话上下文】无可用结构化上下文。"
	}
	var sb strings.Builder
	sb.WriteString("【会话上下文】\n")
	if view.Scope.IsGroup {
		sb.WriteString("来源：群聊；仅使用本群当前可见消息，成员私有信息不得作为事实。\n")
	} else {
		sb.WriteString("来源：一对一单聊；历史为同一客户的连续跟进。\n")
	}
	if view.Current.MessageID != "" {
		sb.WriteString("当前消息ID：" + view.Current.MessageID + "\n")
	}
	history := humanizeVisibleRecent(view)
	if len(history) > 0 {
		sb.WriteString("近期上下文（按时间顺序）：\n")
		for _, msg := range history {
			sb.WriteString("- ")
			sb.WriteString(msg.SenderID)
			sb.WriteString("：")
			sb.WriteString(msg.Content)
			sb.WriteString("\n")
		}
	}
	confirmed := humanizeConfirmedFacts(view)
	if len(confirmed) > 0 {
		sb.WriteString("已确认事实（可作事实陈述）：\n")
		for _, fact := range confirmed {
			sb.WriteString("- ")
			sb.WriteString(fact.Key)
			sb.WriteString("：")
			sb.WriteString(fact.Value)
			sb.WriteString("\n")
		}
	}
	if len(history) == 0 && len(confirmed) == 0 {
		sb.WriteString("无可用的可证明历史或已确认事实。\n")
	}
	return sb.String()
}

func humanizeVisibleRecent(view *agentcontext.AgentConversationContext) []agentcontext.ContextMessage {
	out := make([]agentcontext.ContextMessage, 0, len(view.Recent))
	for _, msg := range view.Recent {
		if !msg.Visibility.IsProven() {
			continue
		}
		if view.Scope.IsGroup && msg.Visibility == agentcontext.VisibilityPrivate && msg.SenderID != view.Current.SenderID {
			continue
		}
		out = append(out, msg)
	}
	if len(out) > 8 {
		out = out[len(out)-8:]
	}
	return out
}

func humanizeConfirmedFacts(view *agentcontext.AgentConversationContext) []agentcontext.BusinessFact {
	candidates := make([]agentcontext.BusinessFact, 0, len(view.SharedBusiness))
	if view.Customer != nil {
		candidates = append(candidates, view.Customer.Facts...)
	}
	if view.Lead != nil {
		candidates = append(candidates, view.Lead.Facts...)
	}
	candidates = append(candidates, view.SharedBusiness...)
	out := make([]agentcontext.BusinessFact, 0, len(candidates))
	seen := make(map[string]bool, len(candidates))
	for _, fact := range candidates {
		if !fact.Grade.CanStateAsFact() {
			continue
		}
		if view.Scope.IsGroup && fact.SubjectType == "group_member" && fact.SubjectID != view.Current.SenderID {
			continue
		}
		key := fact.Key + "\x00" + fact.Value
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, fact)
	}
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}
