// Package service 知识库子域 —— 核心服务入口
//
// 包含类型定义（ImportRequest）、Import 统一入口、文档管理（List/Get/GetProgress/
// GetChunks/Delete/Update）、检索（Search）与 embedding 配置解析。
package service

import (
	"context"
	"errors"
	"fmt"
	agent_runtime "hivemtk-user/internal/aiagent/agent/runtime"
	"hivemtk-user/internal/aiagent/knowledge/model"
	"hivemtk-user/internal/aiagent/knowledge/repository"
	"hivemtk-user/internal/aiagent/llm"
	ragretrieval "hivemtk-user/internal/aiagent/rag/retrieval"
	"hivemtk-user/internal/etl"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/utils/async"
	"hivemtk-user/internal/pkg/utils/logger"
	"mime/multipart"
	"os"
	"strconv"
	"sync/atomic"

	"time"

	"gorm.io/gorm"
)

// kbAnchor 内容变更失效锚端口：抬 KB 行 updated_at 的能力。
// 实现在核心 repository（KnowledgeBaseRepository.TouchAllEnabled），但 aiagent
// 不得 import 核心 repository（depguard aiagent-layer）——与 pkg/tenant 的
// activeTenantLoader 同款：包级默认 + 装配层注入（SetDefaultKBAnchor）。
type kbAnchor interface {
	TouchAllEnabled(ctx context.Context) error
}

// atomicValue 装配期一次写入的进程级锚实现（对 sync/atomic.Value 的薄封装，
// 与 pkg/tenant.activeTenantLoader 同款语义）。
type atomicValue struct{ v atomic.Value }

func (a *atomicValue) Store(x kbAnchor) { a.v.Store(x) }
func (a *atomicValue) Load() any        { return a.v.Load() }

var defaultKBAnchor atomicValue

// SetDefaultKBAnchor 装配层注入失效锚实现（app wiring 启动期调用一次）。
func SetDefaultKBAnchor(a kbAnchor) {
	if a != nil {
		defaultKBAnchor.Store(a)
	}
}

func anchorOrDefault() kbAnchor {
	if v, ok := defaultKBAnchor.Load().(kbAnchor); ok {
		return v
	}
	return nil
}

// 查询 DTO 的对外别名：controller 只依赖 service 层，不 import repository。
type (
	ListFilter          = repository.ListFilter
	ImportLogListFilter = repository.ImportLogListFilter
)

// KnowledgeService 知识库服务(统一入口)
type KnowledgeService struct {
	db            *gorm.DB
	processor     *etl.DocumentProcessor
	vectorizer    *ragretrieval.Vectorizer
	indexer       ragretrieval.IndexManagerInterface
	ragRepo       *repository.RagConfigRepository
	docRepo       *repository.KnowledgeDocumentRepository
	chunkRepo     *repository.KnowledgeChunkRepository
	parentRepo    *repository.KnowledgeChunkParentRepository
	importLogRepo *repository.KnowledgeImportLogRepository
	searchLogRepo *repository.KnowledgeSearchLogRepository
	kbRepo        kbAnchor
	embeddingSvc  *llm.EmbeddingService
	llmSvc        *llm.LLMService
	ragSearcher   *RagSearcher
}

// NewKnowledgeService 创建知识库服务
func NewKnowledgeService() *KnowledgeService {
	return newKnowledgeServiceWithDB(db.GetDB())
}

// NewKnowledgeServiceWithDB 创建带 DB 的知识库服务(用于测试)
func NewKnowledgeServiceWithDB(gdb *gorm.DB) *KnowledgeService {
	return newKnowledgeServiceWithDB(gdb)
}

func newKnowledgeServiceWithDB(gdb *gorm.DB) *KnowledgeService {
	return &KnowledgeService{
		db:            gdb,
		processor:     etl.NewDocumentProcessor(nil),
		vectorizer:    ragretrieval.NewVectorizer(EmbeddingDim(), nil),
		indexer:       nil,
		ragRepo:       repository.NewRagConfigRepository(gdb),
		docRepo:       repository.NewKnowledgeDocumentRepository(gdb),
		chunkRepo:     repository.NewKnowledgeChunkRepository(gdb),
		parentRepo:    repository.NewKnowledgeChunkParentRepository(gdb),
		importLogRepo: repository.NewKnowledgeImportLogRepository(gdb),
		searchLogRepo: repository.NewKnowledgeSearchLogRepository(gdb),
		kbRepo:        anchorOrDefault(),
		embeddingSvc:  llm.NewEmbeddingService(),
		llmSvc:        llm.NewLLMService(),
		ragSearcher:   NewRagSearcherWithDB(gdb),
	}
}

// ImportRequest 统一导入请求
type ImportRequest struct {
	ProductID  string
	SourceType model.SourceType
	Title      string
	Content    string
	SourceRef  string
	Category   string
	Tags       []string
	File       multipart.File
	FileHeader *multipart.FileHeader
	Operator   string
	IP         string
	UserAgent  string
	BatchNo    string
	Metadata   map[string]any `json:"metadata"`
}

// ImportResult 统一导入结果(知识库专用)
type KnowledgeImportResult struct {
	DocumentID uint64    `json:"document_id"`
	Title      string    `json:"title"`
	Status     string    `json:"status"`
	SourceType string    `json:"source_type"`
	CreatedAt  time.Time `json:"created_at"`
}

// Import 统一导入入口
func (s *KnowledgeService) Import(ctx context.Context, req *ImportRequest) (*KnowledgeImportResult, error) {
	if req.ProductID == "" {
		return nil, errors.New("product_id 不能为空")
	}

	product, err := s.ragRepo.GetRagProductByID(ctx, req.ProductID)
	if err != nil {
		return nil, fmt.Errorf("产品不存在: %w", err)
	}
	if product == nil {
		return nil, errors.New("产品不存在")
	}
	productNumericID := product.ID

	start := time.Now()
	var doc *model.KnowledgeDocument
	var err2 error

	switch req.SourceType {
	case model.SourceTypeUpload:
		doc, err2 = s.importUploadedFile(ctx, req, product, productNumericID)
	case model.SourceTypeText:
		doc, err2 = s.importText(ctx, req, product, productNumericID)
	case model.SourceTypeURL:
		doc, err2 = s.importFromURL(ctx, req, product, productNumericID)
	case model.SourceTypeOpenAPI:
		doc, err2 = s.importText(ctx, req, product, productNumericID)
	case model.SourceTypeBatch:
		doc, err2 = s.importText(ctx, req, product, productNumericID)
	default:
		return nil, fmt.Errorf("不支持的来源类型: %s", req.SourceType)
	}

	if err2 != nil {
		if logErr := s.logImport(ctx, req, 0, "failed", int(time.Since(start).Milliseconds()), err2.Error()); logErr != nil {
			logger.Warnf("[knowledge] 写导入日志失败（失败态）: %v", logErr)
		}
		return nil, err2
	}

	// 导入日志是审计与"导入统计"的唯一数据源，写失败必须留痕，
	// 否则接口照常成功而页面上日志/统计永远为空（D-29）。
	if logErr := s.logImport(ctx, req, doc.ID, "success", int(time.Since(start).Milliseconds()), ""); logErr != nil {
		logger.Warnf("[knowledge] 写导入日志失败（成功态）: %v", logErr)
	}

	async.RunWithTimeout(ctx, AsyncProcessingTimeout(), func(procCtx context.Context) {
		s.processDocumentAsync(procCtx, doc.ID, productNumericID, doc.FilePath, doc.FileName, req.Content, doc.MimeType, doc.Title, req.SourceType, req.Metadata)
	})

	return &KnowledgeImportResult{
		DocumentID: doc.ID,
		Title:      doc.Title,
		Status:     string(doc.EmbedStatus),
		SourceType: string(doc.SourceType),
		CreatedAt:  doc.CreatedAt,
	}, nil
}

// List 列出文档
func (s *KnowledgeService) List(ctx context.Context, filter repository.ListFilter) ([]*model.KnowledgeDocument, int64, error) {
	return s.docRepo.List(ctx, filter)
}

// Get 获取文档
//
// KnowledgeDocument.ProductID 是 string（与 RAG 产品 RagProduct.ID 同为 UUID），前端直接传入，无需 HashStringToInt64 转换。
func (s *KnowledgeService) Get(ctx context.Context, productID string, id int64) (*model.KnowledgeDocument, error) {
	return s.docRepo.GetByProductAndID(ctx, productID, id)
}

// GetProgress 获取处理进度
func (s *KnowledgeService) GetProgress(ctx context.Context, documentID uint64) (map[string]any, error) {
	doc, err := s.docRepo.GetByID(ctx, documentID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id":             doc.ID,
		"title":          doc.Title,
		"embed_status":   doc.EmbedStatus,
		"embed_progress": doc.EmbedProgress,
		"chunk_count":    doc.ChunkCount,
		"total_tokens":   doc.TotalTokens,
		"error_msg":      doc.ErrorMsg,
		"last_index_at":  doc.LastIndexAt,
	}, nil
}

// GetChunks 获取分段
func (s *KnowledgeService) GetChunks(ctx context.Context, documentID uint64) ([]model.KnowledgeChunk, error) {
	return s.chunkRepo.GetByDocumentID(ctx, documentID)
}

// Delete 删除文档(级联清理 chunks + pgvector)
func (s *KnowledgeService) Delete(ctx context.Context, productID string, id int64) error {
	doc, err := s.docRepo.GetByProductAndID(ctx, productID, id)
	if err != nil {
		return err
	}
	uid := uint64(id)
	if err := s.chunkRepo.DeleteByDocumentID(ctx, uid); err != nil {
		logger.Errorf("[knowledge] 删除分段失败: %v", err)
	}
	// 父块与子块同生命周期：子块删了父块留着就是一组永远回拼不到的孤儿行。
	if s.parentRepo != nil {
		if err := s.parentRepo.DeleteByDocumentID(ctx, uid); err != nil {
			logger.Errorf("[knowledge] 删除父块失败: %v", err)
		}
	}
	if doc.FilePath != "" {
		_ = os.Remove(doc.FilePath)
	}
	if err := s.docRepo.Delete(ctx, uid); err != nil {
		return err
	}

	agent_runtime.PublishKnowledgeDocumentDelete(ctx, productID, uint(id), 0)
	// 分块已删，答案缓存的依据变了——抬 KB 行 updated_at 判陈缓存（C-11）。
	s.touchKBForContentChange(ctx, "document_delete")

	return nil
}

// Update 更新文档元信息
func (s *KnowledgeService) Update(ctx context.Context, doc *model.KnowledgeDocument) error {
	if doc.ID == 0 {
		return errors.New("文档 ID 不能为空")
	}
	if err := s.docRepo.Update(ctx, doc); err != nil {
		return err
	}

	agent_runtime.PublishKnowledgeDocumentUpdate(ctx, doc.ProductID, uint(doc.ID), "", 0)
	// 标题等元信息会进引用与回复，同样属于答案依据——一并判陈（C-11）。
	s.touchKBForContentChange(ctx, "document_update")

	return nil
}

// touchKBForContentChange 内容变更后抬 KB 行 updated_at（rag_answer_cache 失效锚，C-11）。
//
// best-effort：失败只记日志不阻断内容操作——失效锚写不进去的后果是缓存多陈旧一阵，
// 反向阻断导入/删除则把低频管理动作变成了不可用。nil 库（测试态）静默跳过。
func (s *KnowledgeService) touchKBForContentChange(ctx context.Context, event string) {
	if s == nil || s.kbRepo == nil {
		return
	}
	if err := s.kbRepo.TouchAllEnabled(ctx); err != nil {
		logger.Errorf("[knowledge] 内容变更失效锚写失败（%s，答案缓存可能短暂陈旧）: %v", event, err)
	}
}

// ListImportLogs 列出导入日志
func (s *KnowledgeService) ListImportLogs(ctx context.Context, filter repository.ImportLogListFilter) ([]model.KnowledgeImportLog, int64, error) {
	return s.importLogRepo.List(ctx, filter)
}

func (s *KnowledgeService) resolveProductByID(ctx context.Context, productID string) *model.RagProduct {
	if s.ragRepo == nil {
		return nil
	}
	products, err := s.ragRepo.ListRagProducts(ctx)
	if err != nil || len(products) == 0 {
		return nil
	}
	for _, p := range products {
		if p.ID == productID {
			return p
		}
	}
	return nil
}

func (s *KnowledgeService) resolveEmbeddingConfig(ctx context.Context, numericProductID string) (*llm.EmbeddingService, *llm.EmbeddingConfig) {
	if prod := s.resolveProductByID(ctx, numericProductID); prod != nil && prod.EmbeddingProviderConfig.BaseURL != "" {
		dim := prod.EmbeddingProviderConfig.Dimension
		if dim == 0 {
			dim = EmbeddingDim()
		}
		cfg := &llm.EmbeddingConfig{
			APIType:        prod.EmbeddingProviderConfig.APIType,
			BaseURL:        prod.EmbeddingProviderConfig.BaseURL,
			Model:          prod.EmbeddingProviderConfig.Model,
			APIKey:         prod.EmbeddingProviderConfig.APIKey,
			Dimension:      dim,
			AllowFallback:  false,
			RequestTimeout: DefaultRequestTimeoutSeconds(),
			MaxRetries:     2,
		}
		return llm.NewEmbeddingServiceWithConfig(cfg), cfg
	}
	return s.embeddingSvc, nil
}

func (s *KnowledgeService) persistChunkEmbeddings(ctx context.Context, chunks []model.KnowledgeChunk, embeddings [][]float32) error {
	return s.persistChunkEmbeddingsWithSource(ctx, chunks, embeddings, llm.EmbedSourceTEI)
}

func (s *KnowledgeService) persistChunkEmbeddingsWithSource(ctx context.Context, chunks []model.KnowledgeChunk, embeddings [][]float32, source string) error {
	return s.chunkRepo.UpdateEmbeddingsBatchWithSource(ctx, chunks, embeddings, source)
}

// EmbedAndPersistChunks 向量化指定分片并写入 knowledge_chunks.embedding（per-product 配置优先）。
// 供分段编辑（更新/重切）后重新向量化使用，确保向量与内容一致。
func (s *KnowledgeService) EmbedAndPersistChunks(ctx context.Context, numericProductID string, chunks []model.KnowledgeChunk) error {
	embService, embCfg := s.resolveEmbeddingConfig(ctx, numericProductID)
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
	}
	embeddings, source, err := embService.EmbedWithSource(ctx, embCfg, texts)
	if err != nil {
		return fmt.Errorf("向量化失败: %w", err)
	}

	chunks, embeddings = filterValidEmbeddings(embService, embCfg, chunks, embeddings)
	return s.persistChunkEmbeddingsWithSource(ctx, chunks, embeddings, source)
}

// Search 检索知识库
func (s *KnowledgeService) Search(ctx context.Context, productID string, query string, topK int, threshold float64) ([]model.KnowledgeChunk, error) {

	if s.ragSearcher == nil {
		return nil, errors.New("rag searcher 未初始化")
	}

	// D-27：必须走 SearchIndex 带上 product_id 过滤。此前调用的是全库检索 Search，
	// 再把请求的 productID 回填到每条结果上——别的产品的文档会被贴上本产品标签返回，
	// 前端"产品级检索"看到的其实是跨产品内容（串数据）。
	ragChunks, err := s.ragSearcher.SearchIndex(ctx, productID, query, topK, nil)
	if err != nil {
		return nil, fmt.Errorf("混合检索失败: %w", err)
	}
	out := make([]model.KnowledgeChunk, 0, len(ragChunks))
	for _, c := range dedupeMerchantChunks(ragChunks) {
		out = append(out, knowledgeChunkFromMerchant(productID, c))
	}
	return out, nil
}

// dedupeMerchantChunks 去掉混合检索里重复命中的同一分段。
// 向量路与 BM25 路命中同一段时会产生两条一模一样的记录，前端会重复展示。
func dedupeMerchantChunks(chunks []MerchantRAGChunk) []MerchantRAGChunk {
	if len(chunks) < 2 {
		return chunks
	}
	seen := make(map[string]struct{}, len(chunks))
	out := make([]MerchantRAGChunk, 0, len(chunks))
	for _, c := range chunks {
		key := "doc:" + strconv.FormatUint(c.DocumentID, 10) + ":" + c.Content
		if c.ID != 0 {
			key = "id:" + strconv.FormatUint(c.ID, 10)
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, c)
	}
	return out
}

// knowledgeChunkFromMerchant 把检索层命中映射成接口返回的分段。
//
// D-27：ProductID 用调用方请求的产品，DocumentID/ID/SimilarityScore/Weight 必须取命中本身的字段——
// 只回填 product_id 而丢掉命中元信息会让前端拿到「同一产品的两条一样的结果」和恒为 0 的相关度。
func knowledgeChunkFromMerchant(productID string, c MerchantRAGChunk) model.KnowledgeChunk {
	weight := c.Weight
	if weight <= 0 {
		weight = 1
	}
	return model.KnowledgeChunk{
		ID:              c.ID,
		DocumentID:      c.DocumentID,
		ProductID:       productID,
		Content:         c.Content,
		SimilarityScore: c.Score,
		Weight:          weight,
	}
}
