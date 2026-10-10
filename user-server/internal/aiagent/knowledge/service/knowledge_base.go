// Package service 知识库子域 —— 核心 CRUD
//
// 本文件仅保留：服务结构、构造函数、基础 CRUD (List/Get/Delete)。
package service

import (
	"context"
	"fmt"
	"hivemtk-user/internal/aiagent/knowledge/model"
	ragretrieval "hivemtk-user/internal/aiagent/rag/retrieval"
	"hivemtk-user/internal/etl"
	chunkModel "hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/repository/scope"
	"os"

	"gorm.io/gorm"
)

// KnowledgeBaseService 知识库服务
type KnowledgeBaseService struct {
	db        *gorm.DB
	processor *etl.DocumentProcessor
	indexer   ragretrieval.IndexManagerInterface
	vector    *ragretrieval.Vectorizer
}

// NewKnowledgeBaseService 创建知识库服务
// 默认 EmbeddingDim 维(bge-m3,本地 TEI 真实 embedding)
func NewKnowledgeBaseService() *KnowledgeBaseService {
	return &KnowledgeBaseService{
		db:        db.GetDB(),
		processor: etl.NewDocumentProcessor(nil),
		indexer:   ragretrieval.NewInMemoryIndexManager(EmbeddingDim()),
		vector:    ragretrieval.NewVectorizer(EmbeddingDim(), nil),
	}
}

// NewKnowledgeBaseServiceWithDB 创建带 DB 连接的知识库服务(用于测试)
func NewKnowledgeBaseServiceWithDB(gdb *gorm.DB) *KnowledgeBaseService {
	return &KnowledgeBaseService{
		db:        gdb,
		processor: etl.NewDocumentProcessor(nil),
		indexer:   ragretrieval.NewInMemoryIndexManager(EmbeddingDim()),
		vector:    ragretrieval.NewVectorizer(EmbeddingDim(), nil),
	}
}

// ListDocuments 列出文档
func (s *KnowledgeBaseService) ListDocuments(ctx context.Context, page, pageSize int) ([]*model.KBDocument, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = DefaultPageSize
	}

	var docs []*model.KBDocument
	var total int64
	q := s.db.WithContext(ctx).Model(&model.KBDocument{}).Scopes(scope.TeamScoped(ctx))
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	if err := q.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&docs).Error; err != nil {
		return nil, 0, err
	}
	return docs, total, nil
}

// GetDocument 获取文档
func (s *KnowledgeBaseService) GetDocument(ctx context.Context, id uint) (*model.KBDocument, error) {
	var doc model.KBDocument
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&doc).Error; err != nil {
		return nil, err
	}
	return &doc, nil
}

func (s *KnowledgeBaseService) DeleteDocument(ctx context.Context, id uint) error {
	var doc model.KBDocument
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Where("id = ?", id).First(&doc).Error; err != nil {
			return err
		}

		if err := tx.WithContext(ctx).Where("document_id = ?", id).
			Delete(&chunkModel.KnowledgeChunk{}).Error; err != nil {
			return fmt.Errorf("清理文档分段失败: %w", err)
		}
		return tx.WithContext(ctx).Where("id = ?", id).Delete(&model.KBDocument{}).Error
	})
	if err != nil {
		return err
	}
	if doc.FilePath != "" {
		_ = os.Remove(doc.FilePath)
	}
	_ = s.indexer.DropIndex(ctx, fmt.Sprintf("kb_%d", doc.ID))
	return nil
}
