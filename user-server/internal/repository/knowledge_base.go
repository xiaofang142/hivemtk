package repository

import (
	"context"
	"errors"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// KnowledgeBaseRepository 知识库主表仓储
type KnowledgeBaseRepository struct {
	db *gorm.DB
}

// NewKnowledgeBaseRepository 创建仓储
func NewKnowledgeBaseRepository(db *gorm.DB) *KnowledgeBaseRepository {
	return &KnowledgeBaseRepository{db: db}
}

// Create 新增知识库
func (r *KnowledgeBaseRepository) Create(ctx context.Context, kb *model.KnowledgeBase) error {
	return r.db.WithContext(ctx).Select("*").Create(kb).Error
}

// CreateWithBinding 事务创建知识库并写入 owner 智能体绑定
//
// 用于 owner_type=private 场景: knowledge_bases 与 agent_kb_bindings 必须同生共死,
// 事务边界收敛在仓储层, service 不直接操作 DB。
func (r *KnowledgeBaseRepository) CreateWithBinding(ctx context.Context, kb *model.KnowledgeBase, binding *model.AgentKBBinding) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(kb).Error; err != nil {
			return err
		}

		binding.KBID = kb.ID
		return tx.Create(binding).Error
	})
}

// GetByID 按 ID 查询
func (r *KnowledgeBaseRepository) GetByID(ctx context.Context, id uint) (*model.KnowledgeBase, error) {
	var kb model.KnowledgeBase
	if err := r.db.WithContext(ctx).First(&kb, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &kb, nil
}

// GetByCode 按 kb_code 查询 (业务唯一码)
func (r *KnowledgeBaseRepository) GetByCode(ctx context.Context, code string) (*model.KnowledgeBase, error) {
	var kb model.KnowledgeBase
	if err := r.db.WithContext(ctx).Where("kb_code = ?", code).First(&kb).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &kb, nil
}

// ListFilter 列表查询过滤条件
type KBListFilter struct {
	Type         string
	OwnerType    string
	OwnerAgentID *uint
	Enabled      *bool
	Limit        int
	Offset       int
}

// List 列出知识库 (带可选过滤)
func (r *KnowledgeBaseRepository) List(ctx context.Context, filter KBListFilter) ([]model.KnowledgeBase, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.KnowledgeBase{})
	if filter.Type != "" {
		q = q.Where("type = ?", filter.Type)
	}
	if filter.OwnerType != "" {
		q = q.Where("owner_type = ?", filter.OwnerType)
	}
	if filter.OwnerAgentID != nil {
		q = q.Where("owner_agent_id = ?", *filter.OwnerAgentID)
	}
	if filter.Enabled != nil {
		q = q.Where("enabled = ?", *filter.Enabled)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var kbs []model.KnowledgeBase
	limit := filter.Limit
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	if err := q.Order("id DESC").Limit(limit).Offset(filter.Offset).Find(&kbs).Error; err != nil {
		return nil, 0, err
	}
	return kbs, total, nil
}

// ListByType 按类型列出 (faq/rag/sop)
func (r *KnowledgeBaseRepository) ListByType(ctx context.Context, kbType string, limit int) ([]model.KnowledgeBase, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var kbs []model.KnowledgeBase
	err := r.db.WithContext(ctx).
		Where("type = ? AND enabled = ?", kbType, true).
		Order("id DESC").
		Limit(limit).
		Find(&kbs).Error
	return kbs, err
}

// ListByAgent 列出某智能体可用的知识库 (shared + agent 私有)
//
// 业务规则: 一个智能体可用的知识库 = 该智能体私有的 + 已 binding 的 shared
//   - 私有: owner_agent_id = X AND enabled = true
//   - 共享: owner_type = 'shared' AND enabled = true AND 存在 agent_kb_bindings (agent_id=X, kb_id=knowledge_bases.id, enabled=true)
//
// 返回: 去重后的 []*model.KnowledgeBase, 按 id DESC 排序
func (r *KnowledgeBaseRepository) ListByAgent(ctx context.Context, agentID uint) ([]model.KnowledgeBase, error) {
	if agentID == 0 {
		return nil, nil
	}

	var kbs []model.KnowledgeBase
	subQuery := r.db.WithContext(ctx).
		Model(&model.AgentKBBinding{}).
		Select("kb_id").
		Where("agent_id = ? AND enabled = ?", agentID, true)
	err := r.db.WithContext(ctx).
		Where("enabled = ?", true).
		Where("owner_agent_id = ? OR (owner_type = ? AND id IN (?))",
			agentID, model.KnowledgeBaseOwnerShared, subQuery).
		Order("id DESC").
		Find(&kbs).Error
	if err != nil {
		return nil, err
	}
	return kbs, nil
}

// ListShared 列出全部共享知识库 (owner_type=shared)
func (r *KnowledgeBaseRepository) ListShared(ctx context.Context, kbType string) ([]model.KnowledgeBase, error) {
	q := r.db.WithContext(ctx).Where("owner_type = ? AND enabled = ?", model.KnowledgeBaseOwnerShared, true)
	if kbType != "" {
		q = q.Where("type = ?", kbType)
	}
	var kbs []model.KnowledgeBase
	err := q.Order("id DESC").Find(&kbs).Error
	return kbs, err
}

// Update 更新知识库
func (r *KnowledgeBaseRepository) Update(ctx context.Context, id uint, kb *model.KnowledgeBase) error {
	return r.db.WithContext(ctx).Model(&model.KnowledgeBase{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"name":           kb.Name,
			"description":    kb.Description,
			"type":           kb.Type,
			"owner_type":     kb.OwnerType,
			"owner_agent_id": kb.OwnerAgentID,
			"enabled":        kb.Enabled,
			"member_count":   kb.MemberCount,
			"doc_count":      kb.DocCount,
		}).Error
}

// UpdateVersionCanary 只写版本/灰度三列，且**刻意不碰 updated_at**。
//
// 为什么必须是 UpdateColumns 而不是上面的 Update（也不能顺手复用后者）：
// rag_answer_cache 的命中校验拿本表 updated_at 与缓存行写入时记录的 kb_updated_at
// 比先后，只要本表 updated_at 变晚就把那条缓存行删掉（rag/cache/service.go 的
// fresh()）。"切版本"若 bump 了 updated_at，就会把**另一个版本**的缓存行整批删空，
// 回滚当场从"换个指针"退化成"重灌数据"——T-P2-05 的 AC② 就死在这一行上。
// UpdateColumns 同时跳过 GORM 的自动时间戳与 hooks，这里要的就是这个副作用。
func (r *KnowledgeBaseRepository) UpdateVersionCanary(ctx context.Context, id uint, version int, canaryEnabled bool, canaryPercent int) error {
	if version < 1 {
		version = 1
	}
	if canaryPercent < 0 {
		canaryPercent = 0
	}
	return r.db.WithContext(ctx).Model(&model.KnowledgeBase{}).
		Where("id = ?", id).
		UpdateColumns(map[string]any{
			"version":        version,
			"canary_enabled": canaryEnabled,
			"canary_percent": canaryPercent,
		}).Error
}

// AnswerCacheRowsByVersion 统计某 KB 在 rag_answer_cache 里各 prompt_version 命名空间的行数。
//
// 读的是 rag 模块持有的表（跨模块），原因见 model.KnowledgeBase 版本口径 a/c：版本的
// 物理载体就是这张表的命名空间维度，"两个版本共存"只有在这里才看得见。
// 只读、参数绑定、不写；kbID 传十进制主键字符串（与 ragcache 写入口径一致）。
func (r *KnowledgeBaseRepository) AnswerCacheRowsByVersion(ctx context.Context, kbID string) (map[string]int64, error) {
	type row struct {
		PromptVersion string
		N             int64
	}
	var rows []row
	if err := r.db.WithContext(ctx).Table("rag_answer_cache").
		Select("prompt_version, COUNT(*) AS n").
		Where("kb_id = ?", kbID).
		Group("prompt_version").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(rows))
	for _, x := range rows {
		out[x.PromptVersion] = x.N
	}
	return out, nil
}

// Delete 删除知识库
func (r *KnowledgeBaseRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.KnowledgeBase{}).Error
}

// CountByAgent 统计某智能体拥有多少私有 KB
func (r *KnowledgeBaseRepository) CountByAgent(ctx context.Context, agentID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.KnowledgeBase{}).
		Where("owner_agent_id = ?", agentID).
		Count(&count).Error
	return count, err
}
