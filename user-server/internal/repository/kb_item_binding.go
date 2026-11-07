package repository

import (
	"context"
	"errors"
	"fmt"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// KBItemBindingRepository 知识库成员绑定仓储（D-8 方案 A：kb_item_bindings 成员表）。
//
// 多态 item_id 没有数据库级外键，因此"内容是否还在"必须由本仓储向内容表回查；
// 内容表的标题列名不同（faq_entries.question / sop_templates.name /
// knowledge_documents.title），换列的映射也只在这里维护一份。
type KBItemBindingRepository struct {
	db *gorm.DB
}

func NewKBItemBindingRepository(db *gorm.DB) *KBItemBindingRepository {
	return &KBItemBindingRepository{db: db}
}

// NewKBItemBindingRepositoryDefault 默认构造（全局句柄；与传 GetDB() 同语义）。
// AV-04c 批次7：service/knowledge_base.go 装配收口——句柄解析收回仓储层。
func NewKBItemBindingRepositoryDefault() *KBItemBindingRepository {
	return NewKBItemBindingRepository(GetDB())
}

// itemModel 返回 item_type 对应的内容模型与标题列名。
func itemModel(itemType string) (any, string, error) {
	switch itemType {
	case model.KBItemFAQ:
		return &model.FAQEntry{}, "question", nil
	case model.KBItemSOP:
		return &model.SOPTemplate{}, "name", nil
	case model.KBItemRag:
		return &model.KnowledgeDocument{}, "title", nil
	}
	return nil, "", fmt.Errorf("未知的 item_type: %s", itemType)
}

// Create 绑定一条内容到知识库。
func (r *KBItemBindingRepository) Create(ctx context.Context, b *model.KBItemBinding) error {
	return r.db.WithContext(ctx).Create(b).Error
}

// Delete 解绑。未绑定时 rows=0，由 service 层决定语义。
func (r *KBItemBindingRepository) Delete(ctx context.Context, tenantID, kbID uint, itemType string, itemID uint64) (int64, error) {
	q := r.db.WithContext(ctx).Unscoped().Where(
		"tenant_id = ? AND kb_id = ? AND item_type = ? AND item_id = ?",
		tenantID, kbID, itemType, itemID)
	res := q.Delete(&model.KBItemBinding{})
	return res.RowsAffected, res.Error
}

// DeleteByIDs 按绑定主键硬删（读取侧发现内容已消失时的一致性修复）。
func (r *KBItemBindingRepository) DeleteByIDs(ctx context.Context, tenantID uint, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Unscoped().
		Where("tenant_id = ? AND id IN ?", tenantID, ids).
		Delete(&model.KBItemBinding{}).Error
}

// CountEffectiveByKB 统计某 KB 的绑定条数，只计内容仍存在（且未软删）的绑定。
//
// itemType 为空串时统计全部类型。逐类型走一条 `id IN (绑定子查询)` 计数：
// 三类型封顶三次查询，不做 N+1，也不用裸 SQL。
func (r *KBItemBindingRepository) CountEffectiveByKB(ctx context.Context, tenantID, kbID uint, itemType string) (int64, error) {
	types := []string{model.KBItemFAQ, model.KBItemSOP, model.KBItemRag}
	if itemType != "" {
		types = []string{itemType}
	}
	var total int64
	for _, t := range types {
		mdl, _, err := itemModel(t)
		if err != nil {
			return 0, err
		}
		sub := r.db.WithContext(ctx).Model(&model.KBItemBinding{}).Select("item_id").
			Where("tenant_id = ? AND kb_id = ? AND item_type = ?", tenantID, kbID, t)
		var n int64
		if err := r.db.WithContext(ctx).Model(mdl).
			Where("tenant_id = ? AND id IN (?)", tenantID, sub).
			Count(&n).Error; err != nil {
			return 0, err
		}
		total += n
	}
	return total, nil
}

// ListByKB 分页列出某 KB 的绑定行（itemType 为空串时列全部类型，按创建时间倒序）。
func (r *KBItemBindingRepository) ListByKB(ctx context.Context, tenantID, kbID uint, itemType string, page, pageSize int) ([]*model.KBItemBinding, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}
	q := r.db.WithContext(ctx).Model(&model.KBItemBinding{}).
		Where("tenant_id = ? AND kb_id = ?", tenantID, kbID)
	if itemType != "" {
		q = q.Where("item_type = ?", itemType)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*model.KBItemBinding
	err := q.Order("created_at DESC, id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return list, total, nil
	}
	return list, total, err
}

// Exists 判定某条绑定是否已存在（幂等/校验用）。
func (r *KBItemBindingRepository) Exists(ctx context.Context, tenantID, kbID uint, itemType string, itemID uint64) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.KBItemBinding{}).
		Where("tenant_id = ? AND kb_id = ? AND item_type = ? AND item_id = ?",
			tenantID, kbID, itemType, itemID).
		Count(&n).Error
	return n > 0, err
}

// ItemExists 判定内容行在同一租户下真实存在（软删行不算）。
func (r *KBItemBindingRepository) ItemExists(ctx context.Context, tenantID uint, itemType string, itemID uint64) (bool, error) {
	mdl, _, err := itemModel(itemType)
	if err != nil {
		return false, err
	}
	var n int64
	if err := r.db.WithContext(ctx).Model(mdl).
		Where("tenant_id = ? AND id = ?", tenantID, itemID).Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// TitlesByIDs 批量取内容标题，返回"仍存在的内容 id → 标题"。
// 调用方以返回的键集合判定存活：键缺失即内容已删（软删/硬删），
// 从而不必再单独回查一次存在性。
func (r *KBItemBindingRepository) TitlesByIDs(ctx context.Context, tenantID uint, itemType string, ids []uint64) (map[uint64]string, error) {
	out := map[uint64]string{}
	if len(ids) == 0 {
		return out, nil
	}
	mdl, col, err := itemModel(itemType)
	if err != nil {
		return nil, err
	}
	type row struct {
		ID    uint64
		Title string
	}
	var rows []row
	if err := r.db.WithContext(ctx).Model(mdl).
		Where("tenant_id = ? AND id IN ?", tenantID, ids).
		Select("id, " + col + " AS title").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ID] = r.Title
	}
	return out, nil
}
