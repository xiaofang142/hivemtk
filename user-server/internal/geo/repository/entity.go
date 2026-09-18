package repository

import (
	"context"

	"hivemtk-user/internal/geo/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// GeoEntityRepository 实体 + 关系仓储
// EntityRepository 是同接口别名（service/entity_extractor.go 使用的名字）
type GeoEntityRepository interface {
	Create(ctx context.Context, e *model.GeoEntity) error
	BatchCreate(ctx context.Context, entities []*model.GeoEntity) error
	GetByID(ctx context.Context, id uint) (*model.GeoEntity, error)
	List(ctx context.Context, search string, entityType string, page, limit int) ([]*model.GeoEntity, int64, error)
	GetRelations(ctx context.Context, entityID uint) ([]*model.GeoEntityRelation, error)
	GetRelationGraph(ctx context.Context, entityID uint, depth int) ([]*model.GeoEntityRelation, []*model.GeoEntity, error)
	UpsertRelation(ctx context.Context, rel *model.GeoEntityRelation) error
	BatchUpsertRelations(ctx context.Context, rels []*model.GeoEntityRelation) error
	// CreateRelation entity_extractor.go 使用的名称，同 UpsertRelation
	CreateRelation(ctx context.Context, rel *model.GeoEntityRelation) error
}

// EntityRepository service/entity_extractor.go 使用的别名
type EntityRepository = GeoEntityRepository

type geoEntityRepo struct{ db *gorm.DB }

func NewGeoEntityRepository() GeoEntityRepository {
	return &geoEntityRepo{db: _db.GetDB()}
}
func NewGeoEntityRepositoryWithDB(db *gorm.DB) GeoEntityRepository {
	return &geoEntityRepo{db: db}
}

func (r *geoEntityRepo) Create(ctx context.Context, e *model.GeoEntity) error {
	return r.db.WithContext(ctx).Create(e).Error
}

func (r *geoEntityRepo) BatchCreate(ctx context.Context, entities []*model.GeoEntity) error {
	if len(entities) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).CreateInBatches(entities, 100).Error
}

func (r *geoEntityRepo) GetByID(ctx context.Context, id uint) (*model.GeoEntity, error) {
	var e model.GeoEntity
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&e).Error
	return &e, err
}

func (r *geoEntityRepo) List(ctx context.Context, search string, entityType string, page, limit int) ([]*model.GeoEntity, int64, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	var list []*model.GeoEntity
	var total int64
	q := r.db.WithContext(ctx).Model(&model.GeoEntity{})
	if search != "" {
		q = q.Where("name ILIKE ? OR description ILIKE ?", "%"+search+"%", "%"+search+"%")
	}
	if entityType != "" {
		q = q.Where("type = ?", entityType)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Offset((page - 1) * limit).Limit(limit).Order("created_at DESC").Find(&list).Error
	return list, total, err
}

func (r *geoEntityRepo) GetRelations(ctx context.Context, entityID uint) ([]*model.GeoEntityRelation, error) {
	var list []*model.GeoEntityRelation
	err := r.db.WithContext(ctx).
		Where("entity_a_id = ? OR entity_b_id = ?", entityID, entityID).
		Find(&list).Error
	return list, err
}

func (r *geoEntityRepo) GetRelationGraph(ctx context.Context, entityID uint, depth int) ([]*model.GeoEntityRelation, []*model.GeoEntity, error) {
	if depth <= 0 {
		depth = 1
	}
	seen := map[uint]bool{entityID: true}
	relSeen := map[uint]bool{}
	var rels []*model.GeoEntityRelation
	frontier := []uint{entityID}
	for h := 0; h < depth && len(frontier) > 0; h++ {
		var hop []*model.GeoEntityRelation
		if err := r.db.WithContext(ctx).
			Where("entity_a_id IN ? OR entity_b_id IN ?", frontier, frontier).
			Find(&hop).Error; err != nil {
			return nil, nil, err
		}
		next := []uint{}
		for _, rel := range hop {
			if relSeen[rel.ID] {
				continue
			}
			relSeen[rel.ID] = true
			rels = append(rels, rel)
			for _, id := range []uint{rel.EntityAID, rel.EntityBID} {
				if !seen[id] {
					seen[id] = true
					next = append(next, id)
				}
			}
		}
		frontier = next
	}
	var entities []*model.GeoEntity
	if len(seen) > 1 {
		ids := make([]uint, 0, len(seen))
		for id := range seen {
			ids = append(ids, id)
		}
		if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&entities).Error; err != nil {
			return rels, nil, err
		}
	}
	return rels, entities, nil
}

func (r *geoEntityRepo) UpsertRelation(ctx context.Context, rel *model.GeoEntityRelation) error {
	return r.db.WithContext(ctx).
		Where("entity_a_id = ? AND entity_b_id = ? AND relation = ?", rel.EntityAID, rel.EntityBID, rel.Relation).
		Assign(model.GeoEntityRelation{Relation: rel.Relation}).
		FirstOrCreate(rel).Error
}

func (r *geoEntityRepo) CreateRelation(ctx context.Context, rel *model.GeoEntityRelation) error {
	return r.UpsertRelation(ctx, rel)
}

func (r *geoEntityRepo) BatchUpsertRelations(ctx context.Context, rels []*model.GeoEntityRelation) error {
	for _, rl := range rels {
		if err := r.UpsertRelation(ctx, rl); err != nil {
			return err
		}
	}
	return nil
}
