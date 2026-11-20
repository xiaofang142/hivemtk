// Package service 知识库子域 —— 核心 CRUD
package service

import (
	"context"
	"fmt"
	"hivemtk-user/internal/aiagent/knowledge/model"
	knowledgeRepo "hivemtk-user/internal/aiagent/knowledge/repository"
	ragretrieval "hivemtk-user/internal/aiagent/rag/retrieval"
	"hivemtk-user/internal/etl"
	"os"
)

// KnowledgeBaseService 知识库服务
//
// 文档读写与级联删除事务均经 docs 仓储，服务只编排文件与索引清理。
type KnowledgeBaseService struct {
	docs      kbDocumentStore
	processor *etl.DocumentProcessor
	indexer   ragretrieval.IndexManagerInterface
	vector    *ragretrieval.Vectorizer

	// qaSplit 可选的 QA 拆分服务（上传文档可选 QA 增强；未装配=nil）。
	qaSplit *QASplitService
	// qaEnqueue 投递口替身（仅测试注入；生产走 qaSplit.EnqueueDocument）。
	qaEnqueue func(docID, kbID uint)
}

// kbDocumentStore 是知识库处理链所需的最小文档仓储接缝。
// 生产实现为 KBDocumentRepository；独立测试可用受控替身验证状态与上下文契约。
type kbDocumentStore interface {
	Create(context.Context, *model.KBDocument) error
	GetByID(context.Context, uint) (*model.KBDocument, error)
	List(context.Context, int, int) ([]*model.KBDocument, int64, error)
	UpdateStatusFields(context.Context, uint, map[string]any) error
	DeleteWithChunks(context.Context, uint) (*model.KBDocument, error)
}

// NewKnowledgeBaseService 创建知识库服务
// 默认 EmbeddingDim 维(bge-m3,本地 TEI 真实 embedding)
func NewKnowledgeBaseService() *KnowledgeBaseService {
	return newKnowledgeBaseService(knowledgeRepo.NewKBDocumentRepositoryDefault())
}

// NewKnowledgeBaseServiceWithDocs 仓储注入构造（装配方自有句柄/测试注入）。
func NewKnowledgeBaseServiceWithDocs(docs *knowledgeRepo.KBDocumentRepository) *KnowledgeBaseService {
	return newKnowledgeBaseServiceWithDocumentStore(docs)
}

func newKnowledgeBaseService(docs *knowledgeRepo.KBDocumentRepository) *KnowledgeBaseService {
	return newKnowledgeBaseServiceWithDocumentStore(docs)
}

func newKnowledgeBaseServiceWithDocumentStore(docs kbDocumentStore) *KnowledgeBaseService {
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
	doc, err := s.docs.DeleteWithChunks(ctx, id)
	if err != nil {
		return err
	}
	if doc.FilePath != "" {
		_ = os.Remove(doc.FilePath)
	}
	_ = s.indexer.DropIndex(ctx, fmt.Sprintf("kb_%d", doc.ID))
	return nil
}
