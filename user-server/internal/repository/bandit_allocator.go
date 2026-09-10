package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GetRunningPromptABTestBySOPNode 查询指定 SOP 节点 running 状态的 Prompt A/B 测试
//
// 未找到时返回 gorm.ErrRecordNotFound（调用方按需转换为业务错误或默认值）
func (r *FeedbackLoopRepository) GetRunningPromptABTestBySOPNode(ctx context.Context, sopID uint, nodeID string) (*model.PromptABTest, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrRecordNotFound
	}
	var test model.PromptABTest
	err := r.db.WithContext(ctx).
		Where("sop_id = ? AND sop_node_id = ? AND status = ?", sopID, nodeID, model.PromptABTestStatusRunning).
		First(&test).Error
	if err != nil {
		return nil, err
	}
	return &test, nil
}

// GetBanditArmByExperimentAndKey 查询指定实验 + arm_key 的 bandit arm
//
// 未找到时返回 gorm.ErrRecordNotFound
func (r *FeedbackLoopRepository) GetBanditArmByExperimentAndKey(ctx context.Context, experimentID, armKey string) (*model.BanditArm, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrRecordNotFound
	}
	var arm model.BanditArm
	err := r.db.WithContext(ctx).
		Where("experiment_id = ? AND arm_key = ?", experimentID, armKey).
		First(&arm).Error
	if err != nil {
		return nil, err
	}
	return &arm, nil
}

// UpdateBanditArmReward 更新臂的奖励（成功/失败）
//
// 成功：alpha += 1, success_trials += 1
// 失败：beta += 1
// 通用：total_trials += 1, sum_reward += reward, avg_reward 重算, updated_at 刷新
func (r *FeedbackLoopRepository) UpdateBanditArmReward(ctx context.Context, experimentID, armKey string, success bool, reward float64) error {
	if r == nil || r.db == nil {
		return nil
	}
	updates := map[string]any{
		"total_trials": gorm.Expr("total_trials + 1"),
		"sum_reward":   gorm.Expr("sum_reward + ?", reward),
		"avg_reward":   gorm.Expr("CASE WHEN total_trials + 1 > 0 THEN (sum_reward + ?) / (total_trials + 1) ELSE 0 END", reward),
		"updated_at":   time.Now(),
	}
	if success {
		updates["alpha"] = gorm.Expr("alpha + 1")
		updates["success_trials"] = gorm.Expr("success_trials + 1")
	} else {
		updates["beta"] = gorm.Expr("beta + 1")
	}
	return r.db.WithContext(ctx).Model(&model.BanditArm{}).
		Where("experiment_id = ? AND arm_key = ?", experimentID, armKey).
		Updates(updates).Error
}

// PromoteBanditArmWinner 事务：提升胜出臂 + 淘汰其他臂
//
//  1. winner → status=promoted, promoted_at=NOW()
//  2. 其他 → status=retired, retired_at=NOW()
func (r *FeedbackLoopRepository) PromoteBanditArmWinner(ctx context.Context, experimentID, winnerKey string) error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		if err := tx.Model(&model.BanditArm{}).
			Where("experiment_id = ? AND arm_key = ?", experimentID, winnerKey).
			Updates(map[string]any{
				"status":      model.BanditArmStatusPromoted,
				"promoted_at": now,
				"updated_at":  now,
			}).Error; err != nil {
			return fmt.Errorf("promote winner: %w", err)
		}
		if err := tx.Model(&model.BanditArm{}).
			Where("experiment_id = ? AND arm_key != ?", experimentID, winnerKey).
			Updates(map[string]any{
				"status":     model.BanditArmStatusRetired,
				"retired_at": now,
				"updated_at": now,
			}).Error; err != nil {
			return fmt.Errorf("retire losers: %w", err)
		}
		return nil
	})
}

// ListActiveBanditArms 查询指定实验中处于 exploring/exploiting 状态的臂
//
// 用于 Thompson Sampling 采样前的臂列表加载
func (r *FeedbackLoopRepository) ListActiveBanditArms(ctx context.Context, experimentID string) ([]*model.BanditArm, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	var arms []*model.BanditArm
	err := r.db.WithContext(ctx).
		Where("experiment_id = ? AND status IN ?", experimentID, []string{model.BanditArmStatusExploring, model.BanditArmStatusExploiting}).
		Find(&arms).Error
	if err != nil {
		return nil, err
	}
	return arms, nil
}

// UpdateBanditArmLastSampled 更新臂的 last_sampled_at（异步标记采样时间）
//
// 用于 markSampledAsync：不阻塞主链路，单字段更新
func (r *FeedbackLoopRepository) UpdateBanditArmLastSampled(ctx context.Context, experimentID, armKey string, sampledAt time.Time) error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.BanditArm{}).
		Where("experiment_id = ? AND arm_key = ?", experimentID, armKey).
		Update("last_sampled_at", sampledAt).Error
}

// ListRewardRefluxEvents 查询 (since, until] 窗口内 reward != 0 的反馈事件（回流扫描源）
func (r *FeedbackLoopRepository) ListRewardRefluxEvents(ctx context.Context, since, until time.Time) ([]model.FeedbackEvent, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("feedback loop repository not initialized")
	}
	var events []model.FeedbackEvent
	err := r.db.WithContext(ctx).
		Where("created_at > ? AND created_at <= ? AND reward != 0", since, until).
		Order("id ASC").Limit(500).
		Find(&events).Error
	return events, err
}

// CountRefluxLogsByEventID 统计事件已回流次数（台账防重复）
func (r *FeedbackLoopRepository) CountRefluxLogsByEventID(ctx context.Context, eventID string) (int64, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("feedback loop repository not initialized")
	}
	var cnt int64
	err := r.db.WithContext(ctx).Model(&model.BanditRefluxLog{}).
		Where("event_id = ?", eventID).Count(&cnt).Error
	return cnt, err
}

// CreateRefluxLog 幂等写入回流台账（event_id 冲突时忽略）
func (r *FeedbackLoopRepository) CreateRefluxLog(ctx context.Context, log *model.BanditRefluxLog) error {
	if r == nil || r.db == nil {
		return errors.New("feedback loop repository not initialized")
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(log).Error
}

// ResolveArmForPromptCandidate 按 prompt candidate 解析运行中实验臂
//
// JOIN prompt_ab_tests 限定 running 状态；未命中返回 gorm.ErrRecordNotFound 语义由调用方处理
func (r *FeedbackLoopRepository) ResolveArmForPromptCandidate(ctx context.Context, promptCandidateID uint) (*model.BanditArm, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrRecordNotFound
	}
	var arm model.BanditArm
	err := r.db.WithContext(ctx).
		Joins("JOIN prompt_ab_tests t ON t.experiment_id = bandit_arms.experiment_id AND t.status = 'running'").
		Where("bandit_arms.prompt_candidate_id = ? AND bandit_arms.experiment_type = ?", promptCandidateID, model.BanditExperimentTypePrompt).
		First(&arm).Error
	if err != nil {
		return nil, err
	}
	return &arm, nil
}

// ListRunningABTestsBySOP 按 SOP 查询 running 的 SOP 变体实验（升序取前 2 条）
func (r *FeedbackLoopRepository) ListRunningABTestsBySOP(ctx context.Context, sopID uint) ([]model.PromptABTest, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("feedback loop repository not initialized")
	}
	var tests []model.PromptABTest
	err := r.db.WithContext(ctx).
		Where("status = ? AND experiment_type = ? AND sop_id = ?", "running", model.BanditExperimentTypeSOPVariant, sopID).
		Order("id ASC").Limit(2).
		Find(&tests).Error
	return tests, err
}

// GetBanditArmByExperimentAndSOP 按实验 + SOP 查询臂
func (r *FeedbackLoopRepository) GetBanditArmByExperimentAndSOP(ctx context.Context, experimentID string, sopID uint) (*model.BanditArm, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrRecordNotFound
	}
	var arm model.BanditArm
	err := r.db.WithContext(ctx).
		Where("experiment_id = ? AND sop_id = ?", experimentID, sopID).
		First(&arm).Error
	if err != nil {
		return nil, err
	}
	return &arm, nil
}
