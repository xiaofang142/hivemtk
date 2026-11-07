package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
)

type FeedbackLearningRepository struct {
	db *gorm.DB
}

func NewFeedbackLearningRepository(db *gorm.DB) *FeedbackLearningRepository {
	return &FeedbackLearningRepository{db: db}
}

// DBReady 就绪探针：底层连接是否已注入（不触库）。service 用它替代
// repo.GetDB() == nil 的句柄判断——句柄判断收回仓储内部，service 不再经
// GetDB 转手（trace_learning/adjuster.go 同款收口）。
func (r *FeedbackLearningRepository) DBReady() bool {
	return r != nil && r.db != nil
}

// NewSopAgentRepositoryShared 构造与反馈学习仓储**共享同一条连接**的
// SopAgentRepository。service 侧唯一需求是「用同一条连接派生兄弟仓储」
// （原 service 经 repo.GetDB() 取句柄转手 NewSopAgentRepository）——把连接
// 共享收口成包级构造函数（不持 ctx、也不触库），service 不再取句柄。
// 与 NewTraceLearningRepositoryShared（feedback_loop.go，批次4 先例）同族。
// repo 为 nil 时返回 nil（调用方按「仓储未装配」处理）。
func NewSopAgentRepositoryShared(flRepo *FeedbackLearningRepository) *SopAgentRepository {
	if flRepo == nil {
		return nil
	}
	return NewSopAgentRepository(flRepo.db)
}

// NewSopExecutionRepositoryShared 构造与反馈学习仓储**共享同一条连接**的
// SopExecutionRepository（同 NewSopAgentRepositoryShared 的收口理由）。
func NewSopExecutionRepositoryShared(flRepo *FeedbackLearningRepository) *SopExecutionRepository {
	if flRepo == nil {
		return nil
	}
	return NewSopExecutionRepository(flRepo.db)
}

// ListAIMessagesByPeriod 查询指定时间段内的 AI 回复消息（sender_type=ai，
// created_at 升序；limit<=0 时不限制）。
func (r *FeedbackLearningRepository) ListAIMessagesByPeriod(ctx context.Context, start, end time.Time, limit int) ([]model.SessionMessage, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("feedback learning repository not initialized")
	}
	q := r.db.WithContext(ctx).Model(&model.SessionMessage{}).
		Where("sender_type = ? AND created_at BETWEEN ? AND ?", "ai", start, end).
		Order("created_at ASC")
	q = applyListLimit(q, limit)
	var messages []model.SessionMessage
	if err := q.Find(&messages).Error; err != nil {
		return nil, err
	}
	return messages, nil
}

// ListCustomerMessagesBySessions 查询指定 sessionIDs 内的客户消息；sessionIDs 为空返回 nil。
func (r *FeedbackLearningRepository) ListCustomerMessagesBySessions(ctx context.Context, sessionIDs []string, start, end time.Time) ([]model.SessionMessage, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("feedback learning repository not initialized")
	}
	if len(sessionIDs) == 0 {
		return nil, nil
	}
	var msgs []model.SessionMessage
	err := r.db.WithContext(ctx).Model(&model.SessionMessage{}).
		Where("session_id IN ? AND sender_type = ? AND created_at BETWEEN ? AND ?",
			sessionIDs, "user", start, end).
		Order("created_at ASC").
		Find(&msgs).Error
	if err != nil {
		return nil, err
	}
	return msgs, nil
}

// CreateNodeTransition 创建节点流转记录（t 为 nil 时直接返回 nil）
func (r *FeedbackLearningRepository) CreateNodeTransition(ctx context.Context, t *model.SOPNodeTransition) error {
	if r == nil || r.db == nil {
		return errors.New("feedback learning repository not initialized")
	}
	if t == nil {
		return nil
	}
	return r.db.WithContext(ctx).Create(t).Error
}

// ListNodeTransitionsBySOPAndVariant 查询节点流转记录（variant 非空时过滤）。
func (r *FeedbackLearningRepository) ListNodeTransitionsBySOPAndVariant(ctx context.Context, sopID uint, variant string) ([]model.SOPNodeTransition, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("feedback learning repository not initialized")
	}
	q := r.db.WithContext(ctx).Model(&model.SOPNodeTransition{}).
		Where("sop_id = ?", sopID)
	if variant != "" {
		q = q.Where("variant = ?", variant)
	}
	var transitions []model.SOPNodeTransition
	// 无界兜底：默认 500；已达上限时调用方需分页重取。
	q = applyListLimit(q.Order("created_at ASC"), 500)
	if err := q.Find(&transitions).Error; err != nil {
		return nil, err
	}
	return transitions, nil
}

// CreateSuggestionsInBatches 批量创建优化建议（suggestions 为空时直接返回 nil）。
func (r *FeedbackLearningRepository) CreateSuggestionsInBatches(ctx context.Context, suggestions []model.OptimizationSuggestion, batchSize int) error {
	if r == nil || r.db == nil {
		return errors.New("feedback learning repository not initialized")
	}
	if len(suggestions) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).CreateInBatches(suggestions, batchSize).Error
}

// ListPendingSuggestions 列出待审核建议（sopID>0 时过滤；limit<=0 默认 50）。
func (r *FeedbackLearningRepository) ListPendingSuggestions(ctx context.Context, sopID uint, limit int) ([]model.OptimizationSuggestion, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("feedback learning repository not initialized")
	}
	if limit <= 0 {
		limit = 50
	}
	q := r.db.WithContext(ctx).Model(&model.OptimizationSuggestion{}).
		Where("status = ?", model.SuggestionStatusPending).
		Order("priority DESC, generated_at DESC").
		Limit(limit)
	if sopID > 0 {
		q = q.Where("sop_id = ?", sopID)
	}
	var list []model.OptimizationSuggestion
	if err := q.Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// UpdateSuggestionFields 按 ID 白名单更新建议字段（审核通过/驳回走这条）。
// 命中 0 行必须报不存在：建议已被并发删除或属于别的租户时，
// 静默回「已生效」会让运营误以为策略已改。
func (r *FeedbackLearningRepository) UpdateSuggestionFields(ctx context.Context, suggestionID uint, fields map[string]any) error {
	if r == nil || r.db == nil {
		return errors.New("feedback learning repository not initialized")
	}
	res := r.db.WithContext(ctx).Model(&model.OptimizationSuggestion{}).
		Where("id = ?", suggestionID).
		Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// CreateProfileSnapshot 创建画像快照
func (r *FeedbackLearningRepository) CreateProfileSnapshot(ctx context.Context, snapshot *model.SalesChampionProfileSnapshot) error {
	if r == nil || r.db == nil {
		return errors.New("feedback learning repository not initialized")
	}
	return r.db.WithContext(ctx).Create(snapshot).Error
}

// ListLatestProfileSnapshots 列出最新画像快照（staffID>0 / scenario 非空时过滤）。
func (r *FeedbackLearningRepository) ListLatestProfileSnapshots(ctx context.Context, staffID uint, scenario string, limit int) ([]model.SalesChampionProfileSnapshot, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("feedback learning repository not initialized")
	}
	q := r.db.WithContext(ctx).Model(&model.SalesChampionProfileSnapshot{}).
		Order("generated_at DESC").
		Limit(limit)
	if staffID > 0 {
		q = q.Where("staff_id = ?", staffID)
	}
	if scenario != "" {
		q = q.Where("scenario = ?", scenario)
	}
	var list []model.SalesChampionProfileSnapshot
	if err := q.Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}
