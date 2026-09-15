package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
)

// LongTermMemoryVectorRow pgvector 召回 SQL 扫描行
// 与 service.longTermMemoryRow 等价，由 repository 持有以避免 service 直接访问 DB
type LongTermMemoryVectorRow struct {
	ID         uint64
	CustomerID string
	MemoryType string
	Content    string
	Importance int
	Source     string
	Metadata   string
	CreatedAt  time.Time
	ExpiresAt  *time.Time
	Similarity float64
}

// MemoryRepository 4 层记忆系统仓储接口
type MemoryRepository interface {
	CreateMemoryItem(ctx context.Context, item *model.MemoryItem) error
	ListShortTermMemoryBySession(ctx context.Context, sessionID string, limit int) ([]model.MemoryItem, error)
	DeleteShortTermMemoryBySession(ctx context.Context, sessionID string) error
	CountShortTermMemoryBySession(ctx context.Context, sessionID string) (int64, error)
	PluckOldestShortTermMemoryIDs(ctx context.Context, sessionID string, limit int) ([]uint, error)
	DeleteMemoryItemsByIDs(ctx context.Context, ids []uint) error

	ListFacts(ctx context.Context, customerID string, limit int) ([]model.MemoryItem, error)
	ListFactsByKey(ctx context.Context, customerID, key string, limit int) ([]model.MemoryItem, error)
	ListFactsAsOf(ctx context.Context, customerID string, asOf time.Time, limit int) ([]model.MemoryItem, error)
	SoftInvalidateMemoryItemsByIDs(ctx context.Context, ids []uint, at time.Time) error
	GetLatestSummary(ctx context.Context, customerID string) (*model.MemoryItem, error)

	SaveSOPState(ctx context.Context, state *model.SOPStateMemory) error
	GetSOPStateBySession(ctx context.Context, sessionID string) (*model.SOPStateMemory, error)
	ListSOPStatesByCustomer(ctx context.Context, customerID string, limit int) ([]model.SOPStateMemory, error)

	CountBusinessMemoriesByCustomer(ctx context.Context, customerID string) (int64, error)
	PluckOldestBusinessMemoryIDs(ctx context.Context, customerID string, limit int) ([]uint, error)
	DeleteBusinessMemoriesByIDs(ctx context.Context, ids []uint) error
	CreateBusinessMemory(ctx context.Context, item *model.BusinessMemory) error
	ListBusinessMemories(ctx context.Context, customerID, memoryType string, limit int) ([]model.BusinessMemory, error)

	CreateLongTermMemory(ctx context.Context, item *model.CustomerLongTermMemory) error
	SaveLongTermMemory(ctx context.Context, item *model.CustomerLongTermMemory) error
	SearchLongTermMemoriesByVector(ctx context.Context, queryVecStr, customerID string, fetchN int) ([]LongTermMemoryVectorRow, error)
	ListLongTermMemoriesForFallback(ctx context.Context, customerID string, now time.Time) ([]model.CustomerLongTermMemory, error)
	ListLongTermMemories(ctx context.Context, customerID, memType string, limit int) ([]model.CustomerLongTermMemory, error)
	DeleteLongTermMemoryByID(ctx context.Context, id uint64) error

	DialectName(ctx context.Context) string
}

type memoryRepository struct {
	db *gorm.DB
}

// NewMemoryRepository 构造（无参，内部取库句柄）
func NewMemoryRepository() MemoryRepository {
	return &memoryRepository{db: _db.GetDB()}
}

// NewMemoryRepositoryWithDB 创建指定数据库连接的 MemoryRepository 实例（用于测试）
func NewMemoryRepositoryWithDB(db *gorm.DB) MemoryRepository {
	return &memoryRepository{db: db}
}

func (r *memoryRepository) CreateMemoryItem(ctx context.Context, item *model.MemoryItem) error {
	return r.db.WithContext(ctx).Create(item).Error
}

func (r *memoryRepository) ListShortTermMemoryBySession(ctx context.Context, sessionID string, limit int) ([]model.MemoryItem, error) {
	var items []model.MemoryItem
	err := r.db.WithContext(ctx).
		Where("layer = ? AND session_id = ?", model.MemoryLayerShortTerm, sessionID).
		Order("created_at DESC").Limit(limit).
		Find(&items).Error
	return items, err
}

func (r *memoryRepository) DeleteShortTermMemoryBySession(ctx context.Context, sessionID string) error {
	return r.db.WithContext(ctx).Unscoped().
		Where("layer = ? AND session_id = ?", model.MemoryLayerShortTerm, sessionID).
		Delete(&model.MemoryItem{}).Error
}

func (r *memoryRepository) CountShortTermMemoryBySession(ctx context.Context, sessionID string) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.MemoryItem{}).
		Where("layer = ? AND session_id = ?", model.MemoryLayerShortTerm, sessionID).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *memoryRepository) PluckOldestShortTermMemoryIDs(ctx context.Context, sessionID string, limit int) ([]uint, error) {
	var ids []uint
	if err := r.db.WithContext(ctx).Model(&model.MemoryItem{}).
		Where("layer = ? AND session_id = ?", model.MemoryLayerShortTerm, sessionID).
		Order("created_at ASC").Limit(limit).
		Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *memoryRepository) DeleteMemoryItemsByIDs(ctx context.Context, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Unscoped().Where("id IN ?", ids).Delete(&model.MemoryItem{}).Error
}

func (r *memoryRepository) ListFacts(ctx context.Context, customerID string, limit int) ([]model.MemoryItem, error) {
	var items []model.MemoryItem
	err := r.db.WithContext(ctx).
		Where("layer = ? AND customer_id = ? AND item_type LIKE ?", model.MemoryLayerLongTerm, customerID, "fact:%").
		Order("importance DESC, created_at DESC").Limit(limit).
		Find(&items).Error
	return items, err
}

func (r *memoryRepository) ListFactsByKey(ctx context.Context, customerID, key string, limit int) ([]model.MemoryItem, error) {
	var items []model.MemoryItem
	err := r.db.WithContext(ctx).
		Where("layer = ? AND customer_id = ? AND item_type = ?", model.MemoryLayerLongTerm, customerID, "fact:"+key).
		Order("created_at DESC").Limit(limit).
		Find(&items).Error
	return items, err
}

func (r *memoryRepository) ListFactsAsOf(ctx context.Context, customerID string, asOf time.Time, limit int) ([]model.MemoryItem, error) {
	var items []model.MemoryItem
	err := r.db.WithContext(ctx).
		Where(`layer = ? AND customer_id = ? AND item_type LIKE ?
			AND COALESCE(valid_from, created_at) <= ?
			AND (invalid_at IS NULL OR invalid_at > ?)`,
			model.MemoryLayerLongTerm, customerID, "fact:%", asOf, asOf).
		Order("importance DESC, created_at DESC").Limit(limit).
		Find(&items).Error
	return items, err
}

func (r *memoryRepository) SoftInvalidateMemoryItemsByIDs(ctx context.Context, ids []uint, at time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.MemoryItem{}).
		Where("id IN ?", ids).Update("invalid_at", at).Error
}

func (r *memoryRepository) GetLatestSummary(ctx context.Context, customerID string) (*model.MemoryItem, error) {
	var item model.MemoryItem
	err := r.db.WithContext(ctx).
		Where("layer = ? AND customer_id = ? AND item_type = ?", model.MemoryLayerLongTerm, customerID, "summary").
		Order("created_at DESC").First(&item).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func (r *memoryRepository) SaveSOPState(ctx context.Context, state *model.SOPStateMemory) error {
	return r.db.WithContext(ctx).Save(state).Error
}

func (r *memoryRepository) GetSOPStateBySession(ctx context.Context, sessionID string) (*model.SOPStateMemory, error) {
	var state model.SOPStateMemory
	err := r.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("updated_at DESC").First(&state).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &state, nil
}

func (r *memoryRepository) ListSOPStatesByCustomer(ctx context.Context, customerID string, limit int) ([]model.SOPStateMemory, error) {
	var list []model.SOPStateMemory
	err := r.db.WithContext(ctx).
		Where("customer_id = ?", customerID).
		Order("updated_at DESC").Limit(limit).Find(&list).Error
	return list, err
}

func (r *memoryRepository) CountBusinessMemoriesByCustomer(ctx context.Context, customerID string) (int64, error) {
	var count int64
	r.db.WithContext(ctx).Model(&model.BusinessMemory{}).
		Where("customer_id = ?", customerID).Count(&count)
	return count, nil
}

func (r *memoryRepository) PluckOldestBusinessMemoryIDs(ctx context.Context, customerID string, limit int) ([]uint, error) {
	var ids []uint
	r.db.WithContext(ctx).Model(&model.BusinessMemory{}).
		Where("customer_id = ?", customerID).
		Order("importance ASC, created_at ASC").Limit(limit).
		Pluck("id", &ids)
	return ids, nil
}

func (r *memoryRepository) DeleteBusinessMemoriesByIDs(ctx context.Context, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Unscoped().Where("id IN ?", ids).Delete(&model.BusinessMemory{}).Error
}

func (r *memoryRepository) CreateBusinessMemory(ctx context.Context, item *model.BusinessMemory) error {
	return r.db.WithContext(ctx).Create(item).Error
}

func (r *memoryRepository) ListBusinessMemories(ctx context.Context, customerID, memoryType string, limit int) ([]model.BusinessMemory, error) {
	q := r.db.WithContext(ctx).Model(&model.BusinessMemory{}).Where("customer_id = ?", customerID)
	if memoryType != "" {
		q = q.Where("memory_type = ?", memoryType)
	}
	var list []model.BusinessMemory
	err := q.Order("importance DESC, created_at DESC").Limit(limit).Find(&list).Error
	return list, err
}

func (r *memoryRepository) CreateLongTermMemory(ctx context.Context, item *model.CustomerLongTermMemory) error {
	return r.db.WithContext(ctx).Create(item).Error
}

func (r *memoryRepository) SaveLongTermMemory(ctx context.Context, item *model.CustomerLongTermMemory) error {
	return r.db.WithContext(ctx).Save(item).Error
}

func (r *memoryRepository) SearchLongTermMemoriesByVector(ctx context.Context, queryVecStr, customerID string, fetchN int) ([]LongTermMemoryVectorRow, error) {
	sql := `
		SELECT id, customer_id, memory_type, content, importance, source, metadata, created_at, expires_at,
		       1 - (embedding <=> ?::vector) as similarity
		FROM customer_long_term_memory
		WHERE customer_id = ? AND (expires_at IS NULL OR expires_at > NOW())
		ORDER BY embedding <=> ?::vector
		LIMIT ?
	`
	var rows []LongTermMemoryVectorRow
	if err := r.db.WithContext(ctx).Raw(sql, queryVecStr, customerID, queryVecStr, fetchN).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *memoryRepository) ListLongTermMemoriesForFallback(ctx context.Context, customerID string, now time.Time) ([]model.CustomerLongTermMemory, error) {
	var items []model.CustomerLongTermMemory
	if err := r.db.WithContext(ctx).
		Where("customer_id = ? AND (expires_at IS NULL OR expires_at > ?)", customerID, now).
		Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (r *memoryRepository) ListLongTermMemories(ctx context.Context, customerID, memType string, limit int) ([]model.CustomerLongTermMemory, error) {
	q := r.db.WithContext(ctx).Model(&model.CustomerLongTermMemory{}).Where("customer_id = ?", customerID)
	if memType != "" {
		q = q.Where("memory_type = ?", memType)
	}
	var list []model.CustomerLongTermMemory
	err := q.Order("importance DESC, created_at DESC").Limit(limit).Find(&list).Error
	return list, err
}

func (r *memoryRepository) DeleteLongTermMemoryByID(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Delete(&model.CustomerLongTermMemory{}, id).Error
}

func (r *memoryRepository) DialectName(ctx context.Context) string {
	return r.db.Dialector.Name()
}
