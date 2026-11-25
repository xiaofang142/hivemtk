package service

import (
	"context"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// compactSummary 生成会话历史摘要（D8 compaction）：把判定结论按结构化格式写入
// ConversationContext.Summary，供后续判定复用（被裁剪的早期消息仍可追溯）。
// 不额外调用 LLM——直接用已有判定结论，避免重复计费与造假摘要。
func compactSummary(jd *LeadJudgement) string {
	if jd == nil {
		return ""
	}
	parts := make([]string, 0, 4)
	if jd.Summary != "" {
		parts = append(parts, "诉求:"+strings.TrimSpace(jd.Summary))
	}
	if jd.LeadRole != "" {
		parts = append(parts, "角色:"+jd.LeadRole)
	}
	if len(jd.MatchedKeywords) > 0 {
		parts = append(parts, "关键词:"+strings.Join(jd.MatchedKeywords, "、"))
	}
	if jd.FollowupSuggestion != "" {
		parts = append(parts, "建议:"+strings.TrimSpace(jd.FollowupSuggestion))
	}
	return strings.Join(parts, " | ")
}

// saveCompactSummary 在判定成功后把摘要写回共享上下文（失败 fail-visible：记日志不中断）。
func saveCompactSummary(ctx context.Context, hub *model.MessageHub, accountID uint, jd *LeadJudgement) {
	conversationID := leadHistoryConversationID(hub)
	if hub == nil || accountID == 0 || conversationID == "" {
		return
	}
	summary := compactSummary(jd)
	if summary == "" {
		return
	}
	repo := repository.NewConversationContextRepository()
	conv, err := leadScopedConversation(ctx, repo, accountID, conversationID)
	if err != nil {
		logger.Warnf("[lead-mining] 历史摘要写入失败 account=%d conv=%s: %v", accountID, conversationID, err)
		return
	}
	conv.Summary = summary
	if err := repo.Upsert(ctx, conv); err != nil {
		logger.Warnf("[lead-mining] 历史摘要写入失败 account=%d conv=%s: %v", accountID, conversationID, err)
	}
}
