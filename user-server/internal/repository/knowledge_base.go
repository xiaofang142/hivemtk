package repository

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

type KnowledgeBaseRepository struct {
	db *gorm.DB
}

func NewKnowledgeBaseRepository(db *gorm.DB) *KnowledgeBaseRepository {
	return &KnowledgeBaseRepository{db: db}
}

// NewKnowledgeBaseRepositoryDefault 默认构造（全局句柄；与传 GetDB() 同语义）。
// AV-04c 批次7：service/knowledge_base.go 装配收口——句柄解析收回仓储层，
// service 不再经 repository.GetDB() 伸手拿句柄。
func NewKnowledgeBaseRepositoryDefault() *KnowledgeBaseRepository {
	return NewKnowledgeBaseRepository(GetDB())
}

// Create 新增知识库
func (r *KnowledgeBaseRepository) Create(ctx context.Context, kb *model.KnowledgeBase) error {
	return r.db.WithContext(ctx).Select("*").Create(kb).Error
}

// CreateWithBinding 事务创建知识库并写入 owner 智能体绑定：
// owner_type=private 场景下两者必须同生共死，事务边界收敛在仓储层。
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

// ListByAgent 列出某智能体可用的知识库（私有直属 + 已绑定的 shared，去重、id DESC）。
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

// CountByAgent 统计指定智能体名下的知识库数量。
// 口径与 ListByAgent 严格一致（私有直属 + 共享且已绑定）；agentID == 0 返回 0，
// 与 ListByAgent 的空参短路口径相同。
func (r *KnowledgeBaseRepository) CountByAgent(ctx context.Context, agentID uint) (int64, error) {
	if agentID == 0 {
		return 0, nil
	}

	subQuery := r.db.WithContext(ctx).
		Model(&model.AgentKBBinding{}).
		Select("kb_id").
		Where("agent_id = ? AND enabled = ?", agentID, true)

	var count int64
	err := r.db.WithContext(ctx).Model(&model.KnowledgeBase{}).
		Where("enabled = ?", true).
		Where("owner_agent_id = ? OR (owner_type = ? AND id IN (?))",
			agentID, model.KnowledgeBaseOwnerShared, subQuery).
		Count(&count).Error
	if err != nil {
		return 0, err
	}
	return count, nil
}

// Update 更新知识库
func (r *KnowledgeBaseRepository) Update(ctx context.Context, id uint, kb *model.KnowledgeBase) error {
	return _db.EnsureAffected(r.db.WithContext(ctx).Model(&model.KnowledgeBase{}).
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
		}))
}

// TouchAllEnabled 内容变更失效锚（C-11）：把启用中的 KB 行 updated_at 抬到当前时刻。
//
// rag_answer_cache 的命中校验拿 knowledge_bases.updated_at 与缓存行 kb_updated_at 比先后；
// 文档/分块内容变更若不抬这列，答案缓存对内容永久失明——命中校验永远判"没变过"，
// 客户拿到的还是旧知识的答案。与 UpdateVersionCanary 的"刻意不碰 updated_at"相对：
// 那是版本命名空间切换（旧版本的缓存行要留作回滚），这是同一命名空间内答案依据的
// 内容已变（必须判陈）。
//
// 文档/分块按 RagProduct 归属，缓存按 agent 的 KB 行命名空间，两者之间没有精确映射表；
// 这里选择对本租户全部启用 KB 过度失效：文档变更是低频管理动作，答案查询是高频热路径，
// 宁可多算一次也不留脏读。租户边界由 WithContext 的隔离 callback 兜底（bgCtx 由请求
// ctx 派生，带租户）。
func (r *KnowledgeBaseRepository) TouchAllEnabled(ctx context.Context) error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.KnowledgeBase{}).
		Where("enabled = ?", true).
		Update("updated_at", time.Now()).Error
}

// UpdateVersionCanary 只写版本/灰度三列，且刻意不碰 updated_at：必须用
// UpdateColumns（跳过自动时间戳与 hooks）——rag_answer_cache 的命中校验拿本表
// updated_at 与缓存行的 kb_updated_at 比先后，若切版本 bump 了 updated_at，
// 会把另一个版本的缓存行整批删空，回滚当场从「换个指针」退化成「重灌数据」。
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

// AnswerCacheRowsByVersion 统计某 KB 在 rag_answer_cache 里各 prompt_version
// 命名空间的行数。读的是 rag 模块持有的表：版本口径 a/c 的物理载体就是这张表的
// 命名空间维度，「两个版本共存」只有在这里才看得见。
// 只读、参数绑定；kbID 传十进制主键字符串（与 ragcache 写入口径一致）。
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

// Delete 删除知识库（命中 0 行即不存在：已删/别租户）
func (r *KnowledgeBaseRepository) Delete(ctx context.Context, id uint) error {
	return _db.EnsureAffected(r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.KnowledgeBase{}))
}
