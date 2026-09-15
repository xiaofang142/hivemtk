package repository

import (
	"time"

	"hivemtk-user/internal/geo/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// GeoPushRecordRepository 蜘蛛推送记录仓储
type GeoPushRecordRepository interface {
	CreateBatch(records []*model.GeoPushRecord) error
	GetByArticle(articleID string) ([]*model.GeoPushRecord, error)
	GetByPlatform(platform string, limit int) ([]*model.GeoPushRecord, error)
	GetList(page, limit int, platform, articleID string) ([]*model.GeoPushRecord, int64, error)
	UpdateVerified(id string, verified bool) error
	CountByPlatform(platform string, since time.Time) (int64, error)
}

type geoPushRecordRepo struct {
	db *gorm.DB
}

func NewGeoPushRecordRepository() GeoPushRecordRepository {
	return &geoPushRecordRepo{db: _db.GetDB()}
}

func NewGeoPushRecordRepositoryWithDB(db *gorm.DB) GeoPushRecordRepository {
	return &geoPushRecordRepo{db: db}
}

func (r *geoPushRecordRepo) CreateBatch(records []*model.GeoPushRecord) error {
	if len(records) == 0 {
		return nil
	}
	return r.db.CreateInBatches(records, 200).Error
}

func (r *geoPushRecordRepo) GetByArticle(articleID string) ([]*model.GeoPushRecord, error) {
	var list []*model.GeoPushRecord
	err := r.db.Where("article_id = ?", articleID).Order("pushed_at DESC").Find(&list).Error
	return list, err
}

func (r *geoPushRecordRepo) GetByPlatform(platform string, limit int) ([]*model.GeoPushRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	var list []*model.GeoPushRecord
	err := r.db.Where("platform = ?", platform).Order("pushed_at DESC").Limit(limit).Find(&list).Error
	return list, err
}

func (r *geoPushRecordRepo) GetList(page, limit int, platform, articleID string) ([]*model.GeoPushRecord, int64, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 50
	}
	q := r.db.Model(&model.GeoPushRecord{})
	if platform != "" {
		q = q.Where("platform = ?", platform)
	}
	if articleID != "" {
		q = q.Where("article_id = ?", articleID)
	}
	var total int64
	q.Count(&total)
	var list []*model.GeoPushRecord
	err := q.Order("pushed_at DESC").Offset((page - 1) * limit).Limit(limit).Find(&list).Error
	return list, total, err
}

func (r *geoPushRecordRepo) UpdateVerified(id string, verified bool) error {
	now := time.Now()
	return r.db.Model(&model.GeoPushRecord{}).Where("id = ?", id).Updates(map[string]any{
		"verified":    verified,
		"verified_at": now,
	}).Error
}

func (r *geoPushRecordRepo) CountByPlatform(platform string, since time.Time) (int64, error) {
	var count int64
	err := r.db.Model(&model.GeoPushRecord{}).
		Where("platform = ? AND pushed_at >= ?", platform, since).
		Count(&count).Error
	return count, err
}

// gorm import needed for Expr (used elsewhere)
var _ = gorm.Expr
