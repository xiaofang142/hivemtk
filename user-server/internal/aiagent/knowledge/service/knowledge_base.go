// Package service 知识库子域 —— 核心 CRUD
package service

import (
	"context"
	"fmt"
	"hivemtk-user/internal/aiagent/knowledge/model"
	knowledgeRepo "hivemtk-user/internal/aiagent/knowledge/repository"
	ragretrieval "hivemtk-user/internal/aiagent/rag/retrieval"
	"hivemtk-user/internal/etl"
	chunkModel "hivemtk-user/internal/model"
	"os"

	"gorm.io/gorm"
)

// KnowledgeBaseService 知识库服务
//
// AV-04c 批次7 刀4：不再持有 *gorm.DB——文档读写全部经 docs 仓储，
// DeleteDocument 的事务接缝（AV-03 钉住的合法 Transaction 实参回调）经
// KBDocumentRepository.Transaction 仓储接缝保持原样，句柄解析收回仓储层。
type KnowledgeBaseService struct {
	docs      *knowledgeRepo.KBDocumentRepository
	processor *etl.DocumentProcessor
	indexer   ragretrieval.IndexManagerInterface
	vector    *ragretrieval.Vectorizer

	// qaSplit 可选的 QA 拆分服务（上传文档可选 QA 增强；未装配=nil）。
	qaSplit *QASplitService
	// qaEnqueue 投递口替身（仅测试注入；生产走 qaSplit.EnqueueDocument）。
	qaEnqueue func(docID, kbID uint)
}

// NewKnowledgeBaseService 创建知识库服务
// 默认 EmbeddingDim 维(bge-m3,本地 TEI 真实 embedding)
func NewKnowledgeBaseService() *KnowledgeBaseService {
	return newKnowledgeBaseService(knowledgeRepo.NewKBDocumentRepositoryDefault())
}

// NewKnowledgeBaseServiceWithDocs 仓储注入构造（装配方自有句柄/测试注入）。
func NewKnowledgeBaseServiceWithDocs(docs *knowledgeRepo.KBDocumentRepository) *KnowledgeBaseService {
	return newKnowledgeBaseService(docs)
}

func newKnowledgeBaseService(docs *knowledgeRepo.KBDocumentRepository) *KnowledgeBaseService {
	return &KnowledgeBaseService{
		docs:      docs,
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

	return s.docs.List(ctx, page, pageSize)
}

// GetDocument 获取文档
func (s *KnowledgeBaseService) GetDocument(ctx context.Context, id uint) (*model.KBDocument, error) {
	return s.docs.GetByID(ctx, id)
}

func (s *KnowledgeBaseService) DeleteDocument(ctx context.Context, id uint) error {
	var doc model.KBDocument
	// AV-03 钉住的合法事务接缝：Transaction 实参位置的回调原样保留（回调体逐字
	// 未动），句柄解析经 KBDocumentRepository.Transaction 收回仓储层。
	err := s.docs.Transaction(ctx, func(tx *gorm.DB) error {
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
