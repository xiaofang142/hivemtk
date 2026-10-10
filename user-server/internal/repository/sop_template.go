package repository

import (
	"context"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

type SOPTemplateRepository struct {
	db *gorm.DB
}

func NewSOPTemplateRepository(db *gorm.DB) *SOPTemplateRepository {
	return &SOPTemplateRepository{db: db}
}

// Create 新增 SOP 模板
func (r *SOPTemplateRepository) Create(ctx context.Context, tpl *model.SOPTemplate) error {
	return r.db.WithContext(ctx).Select("*").Create(tpl).Error
}

// GetByID 按 ID 查询
func (r *SOPTemplateRepository) GetByID(ctx context.Context, id uint) (*model.SOPTemplate, error) {
	var tpl model.SOPTemplate
	if err := r.db.WithContext(ctx).First(&tpl, id).Error; err != nil {
		return nil, err
	}
	return &tpl, nil
}

// ListEnabled 查询所有启用的 SOP 模板 (按 priority DESC 排序)
func (r *SOPTemplateRepository) ListEnabled(ctx context.Context, limit int) ([]model.SOPTemplate, error) {
	if limit <= 0 || limit > 10000 {
		limit = 10000
	}
	var tpls []model.SOPTemplate
	err := r.db.WithContext(ctx).
		Where("enabled = ?", true).
		Order("priority DESC, confidence DESC, id ASC").
		Limit(limit).
		Find(&tpls).Error
	return tpls, err
}

// MatchByIntent 按意图匹配 SOP 模板 (取最高优先级且启用) — 兼容旧签名 (agentID=0 表示共享/全局)
//
// 加 agentID 过滤
//   - agentID > 0: 仅匹配该智能体私有 (agent_id=X) + 共享 (agent_id IS NULL)
//   - agentID = 0: 全局共享 (向后兼容, 旧代码不传 agentID 也能跑)
func (r *SOPTemplateRepository) MatchByIntent(ctx context.Context, intent string) ([]model.SOPTemplate, error) {
	return r.MatchByIntentForAgent(ctx, intent, 0)
}

// MatchByIntentForAgent 按智能体隔离的意图匹配
//
// agentID = 0: 走旧路径 (全局共享, 向后兼容)
// agentID > 0: 匹配 (agent_id = agentID OR agent_id IS NULL) AND enabled
func (r *SOPTemplateRepository) MatchByIntentForAgent(ctx context.Context, intent string, agentID uint) ([]model.SOPTemplate, error) {
	if intent == "" {
		return nil, nil
	}
	var tpls []model.SOPTemplate
	q := r.db.WithContext(ctx).
		Where("intent = ? AND enabled = ?", intent, true)
	if agentID > 0 {
		q = q.Where("agent_id = ? OR agent_id IS NULL", agentID)
	}
	err := q.Order("priority DESC, confidence DESC, id ASC").
		Limit(10).
		Find(&tpls).Error
	return tpls, err
}

// MatchByIntentStage 按 (intent, stage) 精确匹配 SOP 模板 — 兼容旧签名
func (r *SOPTemplateRepository) MatchByIntentStage(ctx context.Context, intent, stage string) ([]model.SOPTemplate, error) {
	return r.MatchByIntentStageForAgent(ctx, intent, stage, 0)
}

// MatchByIntentStageForAgent 按 (intent, stage) + agentID 匹配 (隔离架构)
//
// agentID = 0: 走旧路径 (全局共享, 向后兼容)
// agentID > 0: 匹配 (agent_id = agentID OR agent_id IS NULL) AND enabled
func (r *SOPTemplateRepository) MatchByIntentStageForAgent(ctx context.Context, intent, stage string, agentID uint) ([]model.SOPTemplate, error) {
	if intent == "" || stage == "" {
		return nil, nil
	}
	var tpls []model.SOPTemplate
	q := r.db.WithContext(ctx).
		Where("intent = ? AND stage = ? AND enabled = ?", intent, stage, true)
	if agentID > 0 {
		q = q.Where("agent_id = ? OR agent_id IS NULL", agentID)
	}
	err := q.Order("priority DESC, confidence DESC, id ASC").
		Limit(10).
		Find(&tpls).Error
	return tpls, err
}

// MatchByAgent 按智能体严格 1:1 匹配 (: 强 1对1 改造)
//
// 行为:
//   - agentID == 0  -> 返回 (nil, nil)
//   - 仅匹配 enabled = true AND agent_id = ? 的 SOP
//   - 移除"空数组=全局"分支: 任何 agent 都必须显式绑定才能匹配
//
// SQL: WHERE enabled = true AND agent_id = ?  (走 idx_sop_agent_id 索引)
func (r *SOPTemplateRepository) MatchByAgent(ctx context.Context, agentID uint, intent, stage string) ([]model.SOPTemplate, error) {
	if agentID == 0 {
		return nil, nil
	}
	q := r.db.WithContext(ctx).
		Where("enabled = ? AND agent_id = ?", true, agentID)
	if intent != "" {
		q = q.Where("intent = ?", intent)
	}
	if stage != "" {
		q = q.Where("stage = ?", stage)
	}
	var tpls []model.SOPTemplate
	err := q.Order("priority DESC, confidence DESC, id ASC").
		Limit(10).
		Find(&tpls).Error
	return tpls, err
}

// ListShared 列出全部共享 SOP 模板 (agent_id IS NULL)
func (r *SOPTemplateRepository) ListShared(ctx context.Context, limit int) ([]model.SOPTemplate, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var tpls []model.SOPTemplate
	err := r.db.WithContext(ctx).
		Where("agent_id IS NULL AND enabled = ?", true).
		Order("id DESC").
		Limit(limit).
		Find(&tpls).Error
	return tpls, err
}

// ListByAgent 列出某智能体的 SOP 模板集合 (不参与打分, 仅做缓存预热 / 后台同步)
func (r *SOPTemplateRepository) ListByAgent(ctx context.Context, agentID uint, limit int) ([]model.SOPTemplate, error) {
	if agentID == 0 {
		return nil, nil
	}
	if limit <= 0 || limit > 10000 {
		limit = 5000
	}
	var tpls []model.SOPTemplate
	err := r.db.WithContext(ctx).
		Where("enabled = ? AND agent_id = ?", true, agentID).
		Order("id ASC").
		Limit(limit).
		Find(&tpls).Error
	return tpls, err
}

// IncrementHitCount 命中次数 +1
func (r *SOPTemplateRepository) IncrementHitCount(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).
		Model(&model.SOPTemplate{}).
		Where("id = ?", id).
		UpdateColumn("hit_count", gorm.Expr("hit_count + 1")).
		Error
}

// ListWithFilter 分页+过滤 (前端 SOP 模板管理页面使用)
//
// 新增 AgentID 过滤 (nil=不过滤, &0=仅共享, &X=仅该智能体)
func (r *SOPTemplateRepository) ListWithFilter(ctx context.Context, filter SOPTemplateListParams) ([]model.SOPTemplate, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.SOPTemplate{})
	if filter.Keyword != "" {
		kw := "%" + filter.Keyword + "%"
		q = q.Where("name LIKE ? OR template LIKE ?", kw, kw)
	}
	if filter.Intent != "" {
		q = q.Where("intent = ?", filter.Intent)
	}
	if filter.Stage != "" {
		q = q.Where("stage = ?", filter.Stage)
	}
	if filter.Enabled != nil {
		q = q.Where("enabled = ?", *filter.Enabled)
	}
	if filter.AgentID != nil {
		if *filter.AgentID == 0 {
			q = q.Where("agent_id IS NULL")
		} else {
			q = q.Where("agent_id = ?", *filter.AgentID)
		}
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var tpls []model.SOPTemplate
	offset := (filter.Page - 1) * filter.PageSize
	if offset < 0 {
		offset = 0
	}
	err := q.Order("priority DESC, confidence DESC, id ASC").
		Offset(offset).Limit(filter.PageSize).
		Find(&tpls).Error
	return tpls, total, err
}

// Update 更新 SOP 模板
func (r *SOPTemplateRepository) Update(ctx context.Context, id uint, tpl *model.SOPTemplate) error {
	return r.db.WithContext(ctx).Model(&model.SOPTemplate{}).
		Where("id = ?", id).
		Select("*").Updates(tpl).Error
}

// Delete 删除 SOP 模板
func (r *SOPTemplateRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.SOPTemplate{}).Error
}

type SOPTemplateListParams struct {
	Keyword  string
	Intent   string
	Stage    string
	Enabled  *bool
	AgentID  *uint
	Page     int
	PageSize int
}
