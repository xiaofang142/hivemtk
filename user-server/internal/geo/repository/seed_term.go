package repository

import (
	"hivemtk-user/internal/geo/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GeoSeedTermRepository GEO 关键词种子词条（数据库驱动的 TechStackAttrs 替代）
type GeoSeedTermRepository interface {
	// ListActive 按 sort 升序列出全部启用词条（pipeline 与管理端共用）
	ListActive() ([]*model.GeoSeedTerm, error)
	// UpsertBatch 按 term 批量 upsert（首次自动播种用，幂等）
	UpsertBatch(terms []*model.GeoSeedTerm) (int, error)
	// Count 统计词条总数（空表判定用）
	Count() (int64, error)
	// DeleteByTerm 按词条名删除
	DeleteByTerm(term string) error
}

type geoSeedTermRepo struct {
	db *gorm.DB
}

// NewGeoSeedTermRepository 构造 repository（使用默认 DB）
func NewGeoSeedTermRepository() GeoSeedTermRepository {
	return &geoSeedTermRepo{db: _db.GetDB()}
}

// NewGeoSeedTermRepositoryWithDB 构造 repository（使用指定 DB，用于路由注入和测试）
func NewGeoSeedTermRepositoryWithDB(db *gorm.DB) GeoSeedTermRepository {
	return &geoSeedTermRepo{db: db}
}

func (r *geoSeedTermRepo) ListActive() ([]*model.GeoSeedTerm, error) {
	var terms []*model.GeoSeedTerm
	if err := r.db.Where("active = ?", true).Order("\"sort\" ASC, term ASC").Find(&terms).Error; err != nil {
		return nil, err
	}
	return terms, nil
}

func (r *geoSeedTermRepo) UpsertBatch(terms []*model.GeoSeedTerm) (int, error) {
	if len(terms) == 0 {
		return 0, nil
	}
	// 同一批内按 term 去重（同一词可能归属多条业务线），否则 PG 报
	// ON CONFLICT DO UPDATE cannot affect row a second time (21000)
	seen := make(map[string]struct{}, len(terms))
	deduped := terms[:0]
	for _, t := range terms {
		if t == nil {
			continue
		}
		if _, ok := seen[t.Term]; ok {
			continue
		}
		seen[t.Term] = struct{}{}
		deduped = append(deduped, t)
	}
	if err := r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "term"}},
		DoUpdates: clause.AssignmentColumns([]string{"category", "business_line", "source", "active", "sort", "updated_at"}),
	}).Create(&deduped).Error; err != nil {
		return 0, err
	}
	return len(deduped), nil
}

func (r *geoSeedTermRepo) Count() (int64, error) {
	var n int64
	if err := r.db.Model(&model.GeoSeedTerm{}).Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

func (r *geoSeedTermRepo) DeleteByTerm(term string) error {
	return r.db.Where("term = ?", term).Delete(&model.GeoSeedTerm{}).Error
}
