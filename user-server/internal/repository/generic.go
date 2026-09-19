package repository

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/utils/logger"

	"gorm.io/gorm"
)

// orderClauseRe / identRe 是泛型仓库 orderBy 与条件列名的结构白名单。
// BaseRepository 当前无生产调用方（2026-09-19 审计全树 grep 证实），但它是
// 被反复复制的模板——预先把"裸 SQL 通道"焊死成"列名 + ASC/DESC"语法子集，
// 未来接入 HTTP 参数也不会构成注入面。非法输入回退默认排序并告警（fail-closed）。
var (
	orderClauseRe = regexp.MustCompile(`^[a-z_][a-z0-9_]*( (asc|desc))(,[ ]*[a-z_][a-z0-9_]*( (asc|desc)))*$`)
	identRe       = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)
)

func sanitizeOrder(orderBy, fallback string) string {
	v := strings.ToLower(strings.TrimSpace(orderBy))
	if v == "" {
		return fallback
	}
	if orderClauseRe.MatchString(v) {
		return v
	}
	logger.Warnf("[generic-repo] 非法 orderBy 被结构白名单拒绝，回退 %q: %q", fallback, orderBy)
	return fallback
}

var (
	dbInst *gorm.DB
)

// GetDB 返回全局 DB（仅供 platform 适配器等特殊场景使用）
// 五层架构内禁止调用。LLM 维度硬约束：禁止 service 直访 db，repository 是访问 db 的唯一出口。
func GetDB() *gorm.DB {
	if _db.GetDB() != nil {
		return _db.GetDB()
	}
	return dbInst
}

// BaseRepository 泛型基础 Repository
type BaseRepository[T any] struct {
	db *gorm.DB
}

// NewBaseRepository 创建基础 Repository 实例
func NewBaseRepository[T any](db *gorm.DB) *BaseRepository[T] {
	return &BaseRepository[T]{db: db}
}

func (r *BaseRepository[T]) withCtx(ctx context.Context) *gorm.DB {
	if ctx == nil {
		ctx = context.Background()
	}
	return r.db.WithContext(ctx)
}

// Create 创建记录
func (r *BaseRepository[T]) Create(ctx context.Context, entity *T) error {
	return r.withCtx(ctx).Create(entity).Error
}

// Update 更新记录
func (r *BaseRepository[T]) Update(ctx context.Context, entity *T) error {
	return r.withCtx(ctx).Save(entity).Error
}

// Delete 删除记录
func (r *BaseRepository[T]) Delete(ctx context.Context, id uint) error {
	var entity T
	return r.withCtx(ctx).Delete(&entity, id).Error
}

// GetByID 根据 ID 获取记录
func (r *BaseRepository[T]) GetByID(ctx context.Context, id uint) (*T, error) {
	var entity T
	err := r.withCtx(ctx).First(&entity, id).Error
	if err != nil {
		return nil, err
	}
	return &entity, nil
}

// GetByIDs 根据 ID 列表获取记录
func (r *BaseRepository[T]) GetByIDs(ctx context.Context, ids []uint) ([]*T, error) {
	var entities []*T
	err := r.withCtx(ctx).Where("id IN ?", ids).Find(&entities).Error
	if err != nil {
		return nil, err
	}
	return entities, nil
}

// GetList 获取列表（支持分页和排序）
func (r *BaseRepository[T]) GetList(ctx context.Context, page, pageSize int, orderBy string) ([]*T, int64, error) {
	var entities []*T
	var total int64

	query := r.withCtx(ctx).Model(new(T))

	err := query.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	orderBy = sanitizeOrder(orderBy, "created_at DESC")

	offset := (page - 1) * pageSize
	err = query.Offset(offset).Limit(pageSize).Order(orderBy).Find(&entities).Error
	if err != nil {
		return nil, 0, err
	}

	return entities, total, nil
}

// GetListByCondition 根据条件获取列表（支持自定义查询条件）
func (r *BaseRepository[T]) GetListByCondition(ctx context.Context, page, pageSize int, condition map[string]any, orderBy string) ([]*T, int64, error) {
	var entities []*T
	var total int64

	query := r.withCtx(ctx).Model(new(T))

	for key, value := range condition {
		if !identRe.MatchString(strings.ToLower(key)) {
			logger.Warnf("[generic-repo] 非法条件列名被标识符白名单拒绝: %q", key)
			continue
		}
		query = query.Where(key+" = ?", value)
	}

	err := query.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	orderBy = sanitizeOrder(orderBy, "created_at DESC")

	offset := (page - 1) * pageSize
	err = query.Offset(offset).Limit(pageSize).Order(orderBy).Find(&entities).Error
	if err != nil {
		return nil, 0, err
	}

	return entities, total, nil
}

// GetListByQuery 根据自定义查询条件获取列表
func (r *BaseRepository[T]) GetListByQuery(ctx context.Context, page, pageSize int, queryFunc func(*gorm.DB) *gorm.DB, orderBy string) ([]*T, int64, error) {
	var entities []*T
	var total int64

	query := queryFunc(r.withCtx(ctx).Model(new(T)))

	err := query.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	orderBy = sanitizeOrder(orderBy, "created_at DESC")

	offset := (page - 1) * pageSize
	err = query.Offset(offset).Limit(pageSize).Order(orderBy).Find(&entities).Error
	if err != nil {
		return nil, 0, err
	}

	return entities, total, nil
}

// Exists 检查记录是否存在
func (r *BaseRepository[T]) Exists(ctx context.Context, id uint) (bool, error) {
	var entity T
	err := r.withCtx(ctx).Select("id").First(&entity, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Count 获取总记录数
func (r *BaseRepository[T]) Count(ctx context.Context) (int64, error) {
	var count int64
	err := r.withCtx(ctx).Model(new(T)).Count(&count).Error
	return count, err
}

// CountByCondition 根据条件获取记录数
func (r *BaseRepository[T]) CountByCondition(ctx context.Context, condition map[string]any) (int64, error) {
	var count int64
	query := r.withCtx(ctx).Model(new(T))
	for key, value := range condition {
		if !identRe.MatchString(strings.ToLower(key)) {
			logger.Warnf("[generic-repo] 非法条件列名被标识符白名单拒绝: %q", key)
			continue
		}
		query = query.Where(key+" = ?", value)
	}
	err := query.Count(&count).Error
	return count, err
}

// FindByField 根据字段值查找记录
func (r *BaseRepository[T]) FindByField(ctx context.Context, field string, value any) ([]*T, error) {
	var entities []*T
	err := r.withCtx(ctx).Where(field+" = ?", value).Find(&entities).Error
	if err != nil {
		return nil, err
	}
	return entities, nil
}

// FindOneByField 根据字段值查找单条记录
func (r *BaseRepository[T]) FindOneByField(ctx context.Context, field string, value any) (*T, error) {
	var entity T
	err := r.withCtx(ctx).Where(field+" = ?", value).First(&entity).Error
	if err != nil {
		return nil, err
	}
	return &entity, nil
}

// UpdateField 更新单个字段
func (r *BaseRepository[T]) UpdateField(ctx context.Context, id uint, field string, value any) error {
	var entity T
	return r.withCtx(ctx).Model(&entity).Where("id = ?", id).Update(field, value).Error
}

// UpdateFields 更新多个字段
func (r *BaseRepository[T]) UpdateFields(ctx context.Context, id uint, fields map[string]any) error {
	var entity T
	return r.withCtx(ctx).Model(&entity).Where("id = ?", id).Updates(fields).Error
}

// DeleteByField 根据字段值删除记录
func (r *BaseRepository[T]) DeleteByField(ctx context.Context, field string, value any) error {
	var entity T
	return r.withCtx(ctx).Where(field+" = ?", value).Delete(&entity).Error
}

// BatchCreate 批量创建记录
func (r *BaseRepository[T]) BatchCreate(ctx context.Context, entities []*T) error {
	return r.withCtx(ctx).Create(&entities).Error
}

// Transaction 执行事务
func (r *BaseRepository[T]) Transaction(ctx context.Context, fn func(*gorm.DB) error) error {
	return r.withCtx(ctx).Transaction(fn)
}

// DB 获取底层 DB 对象（仅限 Repository 内部使用，不应跨层调用）
func (r *BaseRepository[T]) DB(ctx context.Context) *gorm.DB {
	return r.withCtx(ctx)
}

// Log 打印日志
func (r *BaseRepository[T]) Log(ctx context.Context, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	logger.Info(msg)
}
