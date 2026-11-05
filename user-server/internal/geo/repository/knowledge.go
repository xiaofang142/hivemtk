package repository

import (
	"context"
	"strings"
	"time"

	"hivemtk-user/internal/geo/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// GeoKnowledgeDocumentRepository GEO 知识库文档仓储接口
type GeoKnowledgeDocumentRepository interface {
	Create(ctx context.Context, doc *model.GeoKnowledgeDocument) error
	Update(ctx context.Context, doc *model.GeoKnowledgeDocument) error
	GetByID(ctx context.Context, id string) (*model.GeoKnowledgeDocument, error)
	GetList(ctx context.Context) ([]*model.GeoKnowledgeDocument, error)
	// GetPublicList 只返回公开文档（公开承接页 FAQ 用）。
	GetPublicList(ctx context.Context) ([]*model.GeoKnowledgeDocument, error)
	// ListFiltered 按标题/正文关键词与来源等级过滤（keyword、sourceLevel 为空表示不过滤）
	ListFiltered(ctx context.Context, keyword, sourceLevel string) ([]*model.GeoKnowledgeDocument, error)
	Delete(ctx context.Context, id string) error
}

type geoKnowledgeDocRepo struct {
	db *gorm.DB
}

func NewGeoKnowledgeDocumentRepository() GeoKnowledgeDocumentRepository {
	return &geoKnowledgeDocRepo{db: _db.GetDB()}
}

// NewGeoKnowledgeDocumentRepositoryWithDB 创建指定数据库连接的实例（用于测试）
func NewGeoKnowledgeDocumentRepositoryWithDB(db *gorm.DB) GeoKnowledgeDocumentRepository {
	return &geoKnowledgeDocRepo{db: db}
}

func (r *geoKnowledgeDocRepo) Create(ctx context.Context, doc *model.GeoKnowledgeDocument) error {
	return r.db.WithContext(ctx).Create(doc).Error
}

func (r *geoKnowledgeDocRepo) Update(ctx context.Context, doc *model.GeoKnowledgeDocument) error {
	res := r.db.WithContext(ctx).Model(&model.GeoKnowledgeDocument{}).
		Where("id = ?", doc.ID).
		Updates(map[string]any{
			"title":        doc.Title,
			"content":      doc.Content,
			"doc_type":     doc.DocType,
			"metadata":     doc.Metadata,
			"source_level": doc.SourceLevel,
			"source_url":   doc.SourceURL,
			"is_public":    doc.IsPublic,
			"updated_at":   time.Now(),
		})
	if res.Error != nil {
		return res.Error
	}
	// 0 行说明文档已删除或不属于本租户：不能当作保存成功。
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *geoKnowledgeDocRepo) GetByID(ctx context.Context, id string) (*model.GeoKnowledgeDocument, error) {
	var doc model.GeoKnowledgeDocument
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&doc).Error
	return &doc, err
}

// GetList 返回最近的文档（上限 1000，避免全表读进内存；检索/列表都走这里）。
func (r *geoKnowledgeDocRepo) GetList(ctx context.Context) ([]*model.GeoKnowledgeDocument, error) {
	var docs []*model.GeoKnowledgeDocument
	err := r.db.WithContext(ctx).Order("created_at DESC").Limit(1000).Find(&docs).Error
	return docs, err
}

// ListFiltered 按关键词（标题/正文模糊）与来源等级过滤，避免把 400+ 全量文档原样吐给前端后由浏览器过滤
func (r *geoKnowledgeDocRepo) GetPublicList(ctx context.Context) ([]*model.GeoKnowledgeDocument, error) {
	var docs []*model.GeoKnowledgeDocument
	err := r.db.WithContext(ctx).Where("is_public = ?", true).
		Order("created_at DESC").Limit(1000).Find(&docs).Error
	return docs, err
}

func (r *geoKnowledgeDocRepo) ListFiltered(ctx context.Context, keyword, sourceLevel string) ([]*model.GeoKnowledgeDocument, error) {
	var docs []*model.GeoKnowledgeDocument
	q := r.db.WithContext(ctx).Model(&model.GeoKnowledgeDocument{})
	if kw := strings.TrimSpace(keyword); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("title ILIKE ? OR content ILIKE ?", like, like)
	}
	if lv := strings.TrimSpace(sourceLevel); lv != "" {
		q = q.Where("source_level = ?", lv)
	}
	err := q.Order("created_at DESC").Limit(1000).Find(&docs).Error
	return docs, err
}

func (r *geoKnowledgeDocRepo) Delete(ctx context.Context, id string) error {
	res := r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.GeoKnowledgeDocument{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
