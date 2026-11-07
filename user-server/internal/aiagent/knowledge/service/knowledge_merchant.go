package service

import (
	"hivemtk-user/internal/aiagent/knowledge/repository"
)

// KnowledgeMerchantService 商户自部署场景的 RAG 核心增强服务
// （可视化管理 + 外部系统导入 + 检索调试 + 分段编辑 + 反馈）。
//
// 子能力按文件拆分：
//  1. BatchImportService    批量导入（CSV/JSON）             → knowledge_merchant_batch.go
//  2. PlaygroundService     检索 Playground（不同 topK / 阈值 / 调参） → knowledge_merchant_playground.go
//  3. ChunkEditService      分段编辑（增/改/删/重切）         → knowledge_merchant_chunk.go
//  4. FeedbackService       反馈标注                         → knowledge_merchant_feedback.go
//  5. TokenService          外部 API Token 管理              → knowledge_merchant_token.go
//  6. ExternalImportService 外部系统接入（飞书/Notion/通用 JSON） → knowledge_merchant_external.go
//
// AV-04c 批次7 刀4：不再持有 *gorm.DB / GetDB 出口——九仓储构造收口
// （Default 自取全局句柄 / WithDeps 显式注入），句柄不出仓储层。
type KnowledgeMerchantService struct {
	kbService     *KnowledgeService
	ragSearch     *RagSearcher
	docRepo       *repository.KnowledgeDocumentRepository
	chunkRepo     *repository.KnowledgeChunkRepository
	prodRepo      *repository.RagConfigRepository
	searchLogRepo *repository.KnowledgeSearchLogRepository
	feedbackRepo  *repository.KnowledgeFeedbackRepository
	tokenRepo     *repository.KnowledgeAPITokenRepository
	externalRepo  *repository.ExternalImportJobRepository
}

// KnowledgeMerchantDeps 仓储依赖集合（装配点/测试显式注入；替代 *gorm.DB 形参）。
type KnowledgeMerchantDeps struct {
	KBService     *KnowledgeService
	RagSearch     *RagSearcher
	DocRepo       *repository.KnowledgeDocumentRepository
	ChunkRepo     *repository.KnowledgeChunkRepository
	ProdRepo      *repository.RagConfigRepository
	SearchLogRepo *repository.KnowledgeSearchLogRepository
	FeedbackRepo  *repository.KnowledgeFeedbackRepository
	TokenRepo     *repository.KnowledgeAPITokenRepository
	ExternalRepo  *repository.ExternalImportJobRepository
}

// dbReady 句柄就绪探针：与原 `s.db == nil` 同语义（docRepo 与原 db 字段同源同刻构造）。
func (s *KnowledgeMerchantService) dbReady() bool {
	return s.docRepo != nil && s.docRepo.Connected()
}

// NewKnowledgeMerchantService 创建商户视角 RAG 服务（各仓储 Default 构造自取全局句柄）。
func NewKnowledgeMerchantService() *KnowledgeMerchantService {
	return newKnowledgeMerchantService(KnowledgeMerchantDeps{
		KBService:     NewKnowledgeService(),
		RagSearch:     NewRagSearcher(),
		DocRepo:       repository.NewKnowledgeDocumentRepositoryDefault(),
		ChunkRepo:     repository.NewKnowledgeChunkRepositoryDefault(),
		ProdRepo:      repository.NewRagConfigRepositoryDefault(),
		SearchLogRepo: repository.NewKnowledgeSearchLogRepositoryDefault(),
		FeedbackRepo:  repository.NewKnowledgeFeedbackRepositoryDefault(),
		TokenRepo:     repository.NewKnowledgeAPITokenRepositoryDefault(),
		ExternalRepo:  repository.NewExternalImportJobRepositoryDefault(),
	})
}

// NewKnowledgeMerchantServiceWithDeps 仓储注入构造（装配方自有句柄/测试注入；
// 零值 deps 等价原 WithDB(nil) 的无库语义）。
func NewKnowledgeMerchantServiceWithDeps(deps KnowledgeMerchantDeps) *KnowledgeMerchantService {
	return newKnowledgeMerchantService(deps)
}

func newKnowledgeMerchantService(deps KnowledgeMerchantDeps) *KnowledgeMerchantService {
	return &KnowledgeMerchantService{
		kbService:     deps.KBService,
		ragSearch:     deps.RagSearch,
		docRepo:       deps.DocRepo,
		chunkRepo:     deps.ChunkRepo,
		prodRepo:      deps.ProdRepo,
		searchLogRepo: deps.SearchLogRepo,
		feedbackRepo:  deps.FeedbackRepo,
		tokenRepo:     deps.TokenRepo,
		externalRepo:  deps.ExternalRepo,
	}
}

func (s *KnowledgeMerchantService) ensureReposFromDB() {
	if !s.dbReady() {
		return
	}
	if s.searchLogRepo == nil {
		s.searchLogRepo = repository.NewKnowledgeSearchLogRepositoryDefault()
	}
	if s.feedbackRepo == nil {
		s.feedbackRepo = repository.NewKnowledgeFeedbackRepositoryDefault()
	}
	if s.tokenRepo == nil {
		s.tokenRepo = repository.NewKnowledgeAPITokenRepositoryDefault()
	}
	if s.externalRepo == nil {
		s.externalRepo = repository.NewExternalImportJobRepositoryDefault()
	}
}
