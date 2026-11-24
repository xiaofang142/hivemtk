package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

var leadMiningClueAllowedStatuses = map[string]bool{
	"new": true, "candidate": true, "qualified": true, "following": true,
	"won": true, "lost": true, "invalid": true, "duplicate": true,
	"false_positive": true, "valid_lead": true, // backward compat with existing feedback
}

var leadMiningClueTransitions = map[string]map[string]bool{
	"new":            {"candidate": true, "qualified": true, "following": true, "invalid": true, "duplicate": true, "false_positive": true, "lost": true},
	"candidate":      {"qualified": true, "following": true, "invalid": true, "duplicate": true, "false_positive": true, "lost": true, "new": true},
	"qualified":      {"following": true, "won": true, "lost": true, "invalid": true, "duplicate": true, "false_positive": true},
	"following":      {"won": true, "lost": true, "invalid": true, "duplicate": true, "qualified": true},
	"won":            {"following": true, "lost": true},
	"lost":           {"following": true, "qualified": true, "invalid": true},
	"invalid":        {"new": true, "candidate": true},
	"duplicate":      {"new": true},
	"false_positive": {"new": true, "candidate": true},
	"valid_lead":     {"qualified": true, "following": true, "won": true, "lost": true},
}

func normalizeLeadMiningClueStatus(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "new"
	}
	return s
}

func CanTransitionLeadMiningClueStatus(from, to string) bool {
	from = normalizeLeadMiningClueStatus(from)
	to = normalizeLeadMiningClueStatus(to)
	if from == to {
		return true
	}
	if !leadMiningClueAllowedStatuses[to] {
		return false
	}
	allowed, ok := leadMiningClueTransitions[from]
	if !ok {
		return true // unknown legacy status: allow explicit admin action
	}
	return allowed[to]
}

// UpdateLeadMiningClueStatus updates clue status with state-machine validation.
func (s *ClueService) UpdateLeadMiningClueStatus(ctx context.Context, id, toStatus, reason, operator string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("线索 ID 不能为空")
	}
	toStatus = normalizeLeadMiningClueStatus(toStatus)
	if !leadMiningClueAllowedStatuses[toStatus] {
		return fmt.Errorf("不支持的状态: %s", toStatus)
	}
	clue, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	from := normalizeLeadMiningClueStatus(clue.Status)
	if !CanTransitionLeadMiningClueStatus(from, toStatus) {
		return fmt.Errorf("状态不允许从 %s 转为 %s", from, toStatus)
	}
	updates := map[string]any{
		"status":     toStatus,
		"updated_at": time.Now().Unix(),
	}
	if strings.TrimSpace(reason) != "" {
		updates["desc"] = strings.TrimSpace(clue.Desc + "\n[状态变更] " + from + "->" + toStatus + ": " + strings.TrimSpace(reason))
	}
	// Keep feedback fields aligned for backward-compatible statuses.
	switch toStatus {
	case "false_positive":
		updates["feedback_status"] = "false_positive"
		updates["feedback_reason"] = strings.TrimSpace(reason)
		updates["feedback_by"] = strings.TrimSpace(operator)
		updates["feedback_at"] = time.Now().Unix()
	case "valid_lead", "qualified", "following", "won":
		if clue.FeedbackStatus == "false_positive" || clue.FeedbackStatus == "" || clue.FeedbackStatus == "none" {
			updates["feedback_status"] = "valid"
			updates["feedback_by"] = strings.TrimSpace(operator)
			updates["feedback_at"] = time.Now().Unix()
			if strings.TrimSpace(reason) != "" {
				updates["feedback_reason"] = strings.TrimSpace(reason)
			}
		}
	}
	return s.repo.UpdateByID(ctx, id, updates)
}

// MergeLeadMiningClue marks source clue as duplicate and appends evidence into target.
func (s *ClueService) MergeLeadMiningClue(ctx context.Context, targetID, sourceID, reason, operator string) error {
	targetID = strings.TrimSpace(targetID)
	sourceID = strings.TrimSpace(sourceID)
	if targetID == "" || sourceID == "" {
		return errors.New("target_id/source_id 不能为空")
	}
	if targetID == sourceID {
		return errors.New("target_id/source_id 不能相同")
	}
	target, err := s.repo.GetByID(ctx, targetID)
	if err != nil {
		return err
	}
	source, err := s.repo.GetByID(ctx, sourceID)
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	targetDesc := strings.TrimSpace(target.Desc)
	note := fmt.Sprintf("[合并线索] %s -> %s", sourceID, targetID)
	if strings.TrimSpace(reason) != "" {
		note += ": " + strings.TrimSpace(reason)
	}
	if operator != "" {
		note += " by=" + strings.TrimSpace(operator)
	}
	if targetDesc != "" {
		targetDesc += "\n" + note
	} else {
		targetDesc = note
	}
	targetUpdates := map[string]any{
		"desc":       targetDesc,
		"updated_at": now,
	}
	// keep higher intent score / opportunity
	if source.IntentScore > target.IntentScore {
		targetUpdates["intent_score"] = source.IntentScore
		targetUpdates["is_opportunity"] = source.IsOpportunity
	}
	if strings.TrimSpace(target.Summary) == "" && strings.TrimSpace(source.Summary) != "" {
		targetUpdates["summary"] = strings.TrimSpace(source.Summary)
	}
	if err := s.repo.UpdateByID(ctx, targetID, targetUpdates); err != nil {
		return err
	}
	sourceUpdates := map[string]any{
		"status":     "duplicate",
		"updated_at": now,
		"desc":       strings.TrimSpace(source.Desc + "\n[已合并到] " + targetID),
	}
	return s.repo.UpdateByID(ctx, sourceID, sourceUpdates)
}

// MarkLeadMiningClueConverted marks a clue as converted/qualified follow-up.
func (s *ClueService) MarkLeadMiningClueConverted(ctx context.Context, id, operator string) error {
	return s.UpdateLeadMiningClueStatus(ctx, id, "qualified", "转商机/已确认有效线索", operator)
}

// ListLeadMiningClueStats returns extended stats for lead-mining workbench.
func (s *ClueService) ListLeadMiningClueStats(ctx context.Context) (map[string]any, error) {
	stats, err := s.repo.GetLeadMiningStats(ctx)
	if err != nil {
		return nil, err
	}
	if stats == nil {
		stats = map[string]any{}
	}
	// Add convenience totals if repository already has them; keep backward-compatible.
	return stats, nil
}

// GetLeadMiningClueByID returns clue by id for management actions.
func (s *ClueService) GetLeadMiningClueByID(ctx context.Context, id string) (*model.Clue, error) {
	return s.repo.GetByID(ctx, strings.TrimSpace(id))
}

// EnsureLeadMiningClueRepoAccess is a compile-time guard for expected repo capabilities.
var _ interface {
	GetByID(ctx context.Context, id string) (*model.Clue, error)
	UpdateByID(ctx context.Context, id string, updates map[string]any) error
	GetLeadMiningStats(ctx context.Context) (map[string]any, error)
} = (repository.ClueRepository)(nil)
