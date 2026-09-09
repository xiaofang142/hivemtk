package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// FeedbackLoopRepository 反馈闭环域仓储（反馈学习相关管理面读模型）
//
// 覆盖：反馈事件(FeedbackEvent)、销冠对话(ChampionDialogue)、
// Prompt 候选(PromptCandidate)、Bandit 臂(BanditArm) 的查询。
// 命名按业务域（feedback_loop），不按管理角色或优先级。
type FeedbackLoopRepository struct {
	db *gorm.DB
}

// GetDB 暴露底层连接（仅供 trace_learning 聚合等只读协作方使用，不用于 service 写路径）。
func (r *FeedbackLoopRepository) GetDB() *gorm.DB {
	if r == nil {
		return nil
	}
	return r.db
}

// NewFeedbackLoopRepository 构造（无参，内部取库句柄，遵循本包其他仓储约定）
func NewFeedbackLoopRepository() *FeedbackLoopRepository {
	return &FeedbackLoopRepository{db: db.GetDB()}
}

// ListFeedbackEvents 反馈事件分页列表
func (r *FeedbackLoopRepository) ListFeedbackEvents(ctx context.Context, sessionID, signalKey string, page, pageSize int) ([]model.FeedbackEvent, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.FeedbackEvent{})
	if sessionID != "" {
		q = q.Where("session_id = ?", sessionID)
	}
	if signalKey != "" {
		q = q.Where("signal_key = ?", signalKey)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.FeedbackEvent
	if err := q.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// FeedbackEventStat 反馈事件聚合结果
type FeedbackEventStat struct {
	SignalKey   string  `json:"signal_key"`
	Count       int64   `json:"count"`
	TotalReward float64 `json:"total_reward"`
}

// StatsFeedbackEvents 按信号键聚合
func (r *FeedbackLoopRepository) StatsFeedbackEvents(ctx context.Context, since time.Time) ([]FeedbackEventStat, error) {
	var rows []FeedbackEventStat
	if err := r.db.WithContext(ctx).
		Model(&model.FeedbackEvent{}).
		Select("signal_key, COUNT(*) as count, SUM(reward) as total_reward").
		Where("created_at > ?", since).
		Group("signal_key").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ListChampionDialogues 销冠对话分页列表
//
// 注意：ChampionDialogue 模型无 intent/industry 列，原 controller 的这两个过滤为潜在 bug，
// 此处保留原始 Where 以不改变运行行为（仅在对应参数传入时触发）。
func (r *FeedbackLoopRepository) ListChampionDialogues(ctx context.Context, intent, industry string, page, pageSize int) ([]model.ChampionDialogue, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.ChampionDialogue{})
	if intent != "" {
		q = q.Where("intent = ?", intent)
	}
	if industry != "" {
		q = q.Where("industry = ?", industry)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.ChampionDialogue
	if err := q.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// ListPromptCandidates Prompt 候选分页列表
func (r *FeedbackLoopRepository) ListPromptCandidates(ctx context.Context, status string, page, pageSize int) ([]model.PromptCandidate, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.PromptCandidate{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.PromptCandidate
	if err := q.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// UpdatePromptCandidateStatus 更新 Prompt 候选状态
func (r *FeedbackLoopRepository) UpdatePromptCandidateStatus(ctx context.Context, id, status string) error {
	now := time.Now()
	return r.db.WithContext(ctx).
		Model(&model.PromptCandidate{}).
		Where("id = ?", id).
		Updates(map[string]any{"status": status, "updated_at": now}).Error
}

// ListBanditArms Bandit 臂分页列表
func (r *FeedbackLoopRepository) ListBanditArms(ctx context.Context, experimentID, sopID string, page, pageSize int) ([]model.BanditArm, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.BanditArm{})
	if experimentID != "" {
		q = q.Where("experiment_id = ?", experimentID)
	}
	if sopID != "" {
		q = q.Where("sop_id = ?", sopID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.BanditArm
	if err := q.Order("experiment_id DESC, arm_key ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// NewFeedbackLoopRepositoryWithDB 使用指定 *gorm.DB 构造（供 service 构造函数与测试使用）
func NewFeedbackLoopRepositoryWithDB(db *gorm.DB) *FeedbackLoopRepository {
	return &FeedbackLoopRepository{db: db}
}

// FeedbackSignalUpsert 反馈信号 upsert 参数
type FeedbackSignalUpsert struct {
	SessionID         string
	CustomerID        string
	SOPID             uint
	Variant           string
	PromptCandidateID uint
	Reward            float64
	BreakdownJSON     string
}

// PersistFeedback 事务：写 feedback_event + upsert feedback_signal
func (r *FeedbackLoopRepository) PersistFeedback(ctx context.Context, event *model.FeedbackEvent, sig FeedbackSignalUpsert) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(event).Error; err != nil {
			return fmt.Errorf("create feedback_event: %w", err)
		}
		return upsertFeedbackSignal(tx, sig)
	})
}

func upsertFeedbackSignal(tx *gorm.DB, sig FeedbackSignalUpsert) error {

	var breakdown model.JSONMap
	if err := json.Unmarshal([]byte(sig.BreakdownJSON), &breakdown); err != nil {
		breakdown = model.JSONMap{}
	}

	newSig := model.FeedbackSignal{
		SessionID:         sig.SessionID,
		CustomerID:        sig.CustomerID,
		SOPID:             sig.SOPID,
		Variant:           sig.Variant,
		PromptCandidateID: sig.PromptCandidateID,
		AggregatedReward:  sig.Reward,
		SignalCount:       1,
		SignalBreakdown:   breakdown,
		Outcome:           model.FeedbackSignalOutcomePending,
	}
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "session_id"}},
		DoNothing: true,
	}).Create(&newSig).Error; err != nil {
		return fmt.Errorf("upsert signal insert: %w", err)
	}

	var existing model.FeedbackSignal
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("session_id = ?", sig.SessionID).
		First(&existing).Error; err != nil {
		return fmt.Errorf("upsert signal select for update: %w", err)
	}

	if newSig.ID == 0 {
		existing.AggregatedReward += sig.Reward
		existing.SignalCount += 1
		if existing.SignalBreakdown == nil {
			existing.SignalBreakdown = model.JSONMap{}
		}
		for k, v := range breakdown {
			if cur, ok := existing.SignalBreakdown[k].(float64); ok {
				if nv, ok := v.(float64); ok {
					existing.SignalBreakdown[k] = cur + nv
					continue
				}
			}
			existing.SignalBreakdown[k] = v
		}
		return tx.Save(&existing).Error
	}
	return nil
}

// ListPendingSuggestions 查询待审核建议（priority >= 给定阈值）
func (r *FeedbackLoopRepository) ListPendingSuggestions(ctx context.Context, minPriority int) ([]model.OptimizationSuggestion, error) {
	var rows []model.OptimizationSuggestion
	err := r.db.WithContext(ctx).
		Where("status = ? AND priority >= ?", model.SuggestionStatusPending, minPriority).
		Find(&rows).Error
	return rows, err
}

// MarkSuggestionApplied 标记建议为已应用
func (r *FeedbackLoopRepository) MarkSuggestionApplied(ctx context.Context, id uint, appliedAt time.Time) error {
	return r.db.WithContext(ctx).
		Model(&model.OptimizationSuggestion{}).
		Where("id = ?", id).
		Updates(map[string]any{"status": model.SuggestionStatusApplied, "applied_at": appliedAt}).Error
}

func (r *FeedbackLoopRepository) CloneSOPAndCreateABTest(ctx context.Context, sopID uint, nameSuffix string, experimentTag string) error {
	return r.CloneSOPAndCreateABTestMutated(ctx, sopID, nameSuffix, experimentTag, nil)
}

// CloneSOPAndCreateABTestMutated 同 CloneSOPAndCreateABTest，另支持 Service 层注入图变异器
func (r *FeedbackLoopRepository) CloneSOPAndCreateABTestMutated(
	ctx context.Context, sopID uint, nameSuffix, experimentTag string,
	mutate func(model.JSONMap) model.JSONMap,
) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var original model.SOPAgent
		if err := tx.First(&original, sopID).Error; err != nil {
			return fmt.Errorf("fetch sop %d: %w", sopID, err)
		}
		clone := original
		clone.ID = 0
		clone.Name = original.Name + nameSuffix
		clone.IsActive = false
		clone.ExecutionCount = 0
		clone.SuccessCount = 0
		if mutate != nil {
			if mutated := mutate(clone.SOPGraph); mutated != nil {
				clone.SOPGraph = mutated
			}
		}
		if err := tx.Create(&clone).Error; err != nil {
			return fmt.Errorf("create variant sop: %w", err)
		}
		expID := fmt.Sprintf("sop_%d_%d", sopID, time.Now().UnixNano())
		now := time.Now()
		abTest := model.PromptABTest{
			ExperimentID:   expID,
			ExperimentType: model.BanditExperimentTypeSOPVariant,
			SOPID:          sopID,
			Name:           original.Name + nameSuffix,
			ArmKeys:        model.JSONArray{"arm_a_original", "arm_b_variant"},
			Config:         model.JSONMap{"tag": experimentTag},
			Status:         model.PromptABTestStatusRunning,
			StartedAt:      &now,
		}
		if err := tx.Create(&abTest).Error; err != nil {
			return fmt.Errorf("create ab test: %w", err)
		}
		arms := []model.BanditArm{
			{ExperimentID: expID, ExperimentType: model.BanditExperimentTypeSOPVariant, ArmKey: "arm_a_original", SOPID: sopID, Variant: "A", Status: model.BanditArmStatusExploring},
			{ExperimentID: expID, ExperimentType: model.BanditExperimentTypeSOPVariant, ArmKey: "arm_b_variant", SOPID: clone.ID, Variant: "B", Status: model.BanditArmStatusExploring},
		}
		if err := tx.Create(&arms).Error; err != nil {
			return fmt.Errorf("create bandit arms: %w", err)
		}
		return nil
	})
}

// ListRunningABTestsByType 查询指定实验类型中 running 状态的 A/B 测试
func (r *FeedbackLoopRepository) ListRunningABTestsByType(ctx context.Context, experimentType string) ([]model.PromptABTest, error) {
	var rows []model.PromptABTest
	err := r.db.WithContext(ctx).
		Where("status = ? AND experiment_type = ?", model.PromptABTestStatusRunning, experimentType).
		Find(&rows).Error
	return rows, err
}

// ListRunningABTests 查询所有 running 状态的 A/B 测试
func (r *FeedbackLoopRepository) ListRunningABTests(ctx context.Context) ([]model.PromptABTest, error) {
	var rows []model.PromptABTest
	err := r.db.WithContext(ctx).
		Where("status = ?", model.PromptABTestStatusRunning).
		Find(&rows).Error
	return rows, err
}

// UpdateABTestFields 按主键更新 A/B 测试指定字段
func (r *FeedbackLoopRepository) UpdateABTestFields(ctx context.Context, id uint, fields map[string]any) error {
	return r.db.WithContext(ctx).
		Model(&model.PromptABTest{}).
		Where("id = ?", id).
		Updates(fields).Error
}

// UpdateABTestByExperimentID 按 experiment_id 更新 A/B 测试指定字段
// 用于 FeedbackLoopCron Bandit 收敛后标记实验为 completed 并记录 winner_arm
func (r *FeedbackLoopRepository) UpdateABTestByExperimentID(ctx context.Context, experimentID string, fields map[string]any) error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).
		Model(&model.PromptABTest{}).
		Where("experiment_id = ?", experimentID).
		Updates(fields).Error
}

// GetPromptABTest 根据 ID 获取 A/B 测试
func (r *FeedbackLoopRepository) GetPromptABTest(ctx context.Context, id uint) (*model.PromptABTest, error) {
	var test model.PromptABTest
	if err := r.db.WithContext(ctx).First(&test, id).Error; err != nil {
		return nil, err
	}
	return &test, nil
}

// CountFeedbackSignalsByVariant 统计指定 SOP + variant 的反馈信号数
func (r *FeedbackLoopRepository) CountFeedbackSignalsByVariant(ctx context.Context, sopID uint, variant string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&model.FeedbackSignal{}).
		Where("sop_id = ? AND variant = ?", sopID, variant).
		Count(&count).Error
	return count, err
}

// CountFeedbackSignalsByVariantAndOutcome 统计指定 SOP + variant + outcome 的反馈信号数
func (r *FeedbackLoopRepository) CountFeedbackSignalsByVariantAndOutcome(ctx context.Context, sopID uint, variant string, outcome string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&model.FeedbackSignal{}).
		Where("sop_id = ? AND variant = ? AND outcome = ?", sopID, variant, outcome).
		Count(&count).Error
	return count, err
}

// CountFeedbackEventsByVariant 统计指定 SOP + variant 的反馈事件数
func (r *FeedbackLoopRepository) CountFeedbackEventsByVariant(ctx context.Context, sopID uint, variant string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&model.FeedbackEvent{}).
		Where("sop_id = ? AND variant = ?", sopID, variant).
		Count(&count).Error
	return count, err
}

// CountFeedbackEventsByVariantAndSignalKey 统计指定 SOP + variant + signal_key 的反馈事件数
func (r *FeedbackLoopRepository) CountFeedbackEventsByVariantAndSignalKey(ctx context.Context, sopID uint, variant string, signalKey string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&model.FeedbackEvent{}).
		Where("sop_id = ? AND variant = ? AND signal_key = ?", sopID, variant, signalKey).
		Count(&count).Error
	return count, err
}

// RollbackABTest 事务：回滚 A/B 测试（test 状态→rolled_back，arms 状态→retired）
func (r *FeedbackLoopRepository) RollbackABTest(ctx context.Context, testID uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var test model.PromptABTest
		if err := tx.First(&test, testID).Error; err != nil {
			return err
		}
		now := time.Now()
		if err := tx.Model(&model.PromptABTest{}).Where("id = ?", testID).
			Updates(map[string]any{"status": model.PromptABTestStatusRolledBack, "ended_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.BanditArm{}).
			Where("experiment_id = ?", test.ExperimentID).
			Updates(map[string]any{"status": model.BanditArmStatusRetired, "retired_at": now}).Error; err != nil {
			return err
		}
		return nil
	})
}

// GetSuggestionAuditFields 读取建议的审计字段（evidence_data），供门禁审计读写。
func (r *FeedbackLoopRepository) GetSuggestionAuditFields(ctx context.Context, sugID uint) (*model.OptimizationSuggestion, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("feedback loop repository not initialized")
	}
	var row model.OptimizationSuggestion
	if err := r.db.WithContext(ctx).Select("id", "evidence_data").First(&row, sugID).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// UpdateSuggestionEvidenceData 更新建议的 evidence_data 审计字段。
func (r *FeedbackLoopRepository) UpdateSuggestionEvidenceData(ctx context.Context, sugID uint, ev model.JSONMap) error {
	if r == nil || r.db == nil {
		return errors.New("feedback loop repository not initialized")
	}
	return r.db.WithContext(ctx).Model(&model.OptimizationSuggestion{}).
		Where("id = ?", sugID).
		Update("evidence_data", ev).Error
}

// ListTopEvaluatedTraces 取评分最高的非 bad trace（golden 集取样源）。
func (r *FeedbackLoopRepository) ListTopEvaluatedTraces(ctx context.Context, limit int) ([]TopEvaluatedTrace, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("feedback loop repository not initialized")
	}
	var logs []TopEvaluatedTrace
	err := r.db.WithContext(ctx).
		Table("trace_eval_log").
		Select("trace_id, score").
		Where("bad = ?", false).
		Order("score DESC").
		Limit(limit).
		Scan(&logs).Error
	return logs, err
}

// TopEvaluatedTrace golden 集取样行
type TopEvaluatedTrace struct {
	TraceID string `gorm:"column:trace_id"`
	Score   int    `gorm:"column:score"`
}
