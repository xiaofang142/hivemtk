package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"hivemtk-user/internal/model"
)

type leadReactJudgementStore interface {
	Available() bool
	ListRecentForReAct(ctx context.Context, platform string, accountIDs []string, conversationID, groupID, senderID string, isGroup bool, query string, limit int) ([]model.LeadMiningJudgement, error)
}

type leadReactSessionStore interface {
	Available() bool
	ListRecentForReAct(ctx context.Context, platform model.Platform, accountIDs []string, userID string, limit int) ([]*model.CustomerSession, int64, error)
	CountByStatusForReAct(ctx context.Context, platform model.Platform, accountIDs []string, userID string) (map[model.SessionStatus]int64, error)
}

// NewLeadReactResolver 装配生产只读取证器。依赖缺失时 fail-visible，不返回
// 会伪造证据的空实现。
func NewLeadReactResolver(judgements leadReactJudgementStore, sessions leadReactSessionStore) (LeadReactResolver, error) {
	if judgements == nil || sessions == nil || !judgements.Available() || !sessions.Available() {
		return LeadReactResolver{}, errors.New("ReAct resolver 依赖不完整")
	}
	return LeadReactResolver{
		HistoricalLeads: func(ctx context.Context, task LeadReactTask, query string) (string, error) {
			if !validLeadReactTaskContext(ctx, task) {
				return "", errLeadReactToolUnavailable
			}
			rows, err := judgements.ListRecentForReAct(ctx, task.Platform, task.AccountIDs, task.ConversationID, task.GroupID, task.SenderID, task.IsGroup, query, 20)
			if err != nil {
				return "", err
			}
			type leadSummary struct {
				JudgementID      string `json:"judgement_id"`
				CreatedAt        any    `json:"created_at"`
				LeadRole         string `json:"lead_role"`
				IntentScore      int    `json:"intent_score"`
				Matched          bool   `json:"matched"`
				ClueType         string `json:"clue_type"`
				Summary          string `json:"summary"`
				Reason           string `json:"reason"`
				Degraded         bool   `json:"degraded"`
				EvidenceComplete bool   `json:"evidence_complete"`
			}
			out := make([]leadSummary, 0, len(rows))
			for _, row := range rows {
				out = append(out, leadSummary{
					JudgementID: row.JudgementID, CreatedAt: row.CreatedAt,
					LeadRole: row.LeadRole, IntentScore: row.IntentScore, Matched: row.Matched,
					ClueType: row.ClueType, Summary: truncateLeadReactRunes(row.Summary, 500),
					Reason: truncateLeadReactRunes(row.Reason, 500), Degraded: row.Degraded,
					EvidenceComplete: row.EvidenceComplete,
				})
			}
			raw, err := json.Marshal(map[string]any{"items": out, "count": len(out)})
			if err != nil {
				return "", err
			}
			return string(raw), nil
		},
		Customer360: func(ctx context.Context, task LeadReactTask, query string) (string, error) {
			// query 只可作为展示提示，不能改变可信 user_id；账号/租户/平台均来自 task/ctx。
			if !validLeadReactTaskContext(ctx, task) || task.RawFallback {
				return "", errLeadReactToolUnavailable
			}
			userID := strings.TrimSpace(task.SenderID)
			if userID == "" {
				return "", errLeadReactToolUnavailable
			}
			recentSessions, total, err := sessions.ListRecentForReAct(ctx, model.Platform(task.Platform), task.AccountIDs, userID, 10)
			if err != nil {
				return "", err
			}
			statusCounts, err := sessions.CountByStatusForReAct(ctx, model.Platform(task.Platform), task.AccountIDs, userID)
			if err != nil {
				return "", err
			}
			userName := ""
			var lastSeen any
			type sessionSummary struct {
				SessionID       string `json:"session_id"`
				Status          string `json:"status"`
				HandlerType     string `json:"handler_type"`
				LastMessageAt   any    `json:"last_message_at,omitempty"`
				MessageCount    int    `json:"message_count"`
				AIReplyCount    int    `json:"ai_reply_count"`
				HumanReplyCount int    `json:"human_reply_count"`
			}
			recent := make([]sessionSummary, 0, len(recentSessions))
			for _, session := range recentSessions {
				if userName == "" {
					userName = session.UserName
				}
				if lastSeen == nil && session.LastMessageAt != nil {
					lastSeen = *session.LastMessageAt
				}
				recent = append(recent, sessionSummary{
					SessionID: session.SessionID, Status: string(session.Status), HandlerType: string(session.HandlerType),
					LastMessageAt: session.LastMessageAt, MessageCount: session.MessageCount,
					AIReplyCount: session.AIReplyCount, HumanReplyCount: session.HumanReplyCount,
				})
			}
			raw, err := json.Marshal(map[string]any{
				"user_id": userID, "user_name": userName, "session_count": total,
				"status_counts": statusCounts, "last_seen": lastSeen, "recent_sessions": recent,
			})
			if err != nil {
				return "", err
			}
			return string(raw), nil
		},
	}, nil
}
