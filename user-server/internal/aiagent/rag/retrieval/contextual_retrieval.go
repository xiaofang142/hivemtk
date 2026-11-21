package ragretrieval

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// ContextualRetrievalEnhancer Anthropic Contextual Retrieval 索引期增强
type ContextualRetrievalEnhancer struct {
	db          *gorm.DB
	chatClient  LLMChatClient
	embedding   llm.EmbeddingServiceInterface
	batchSize   int
	maxCtxToken int
	ownerToken  string
}

// WithDocumentOwner fences contextual chunk writes to the active indexing claim.
func (e *ContextualRetrievalEnhancer) WithDocumentOwner(ownerToken string) *ContextualRetrievalEnhancer {
	e.ownerToken = ownerToken
	return e
}

type ContextualEnhancerConfig struct {
	BatchSize   int
	MaxCtxToken int
}

func DefaultContextualEnhancerConfig() *ContextualEnhancerConfig {
	return &ContextualEnhancerConfig{
		BatchSize:   10,
		MaxCtxToken: 100,
	}
}

// NewContextualRetrievalEnhancer 创建上下文增强器。
// chatClient / embedding 为 nil 时仅更新 contextual_context 列（不重新 embedding）。
func NewContextualRetrievalEnhancer(
	db *gorm.DB,
	chatClient LLMChatClient,
	embedding llm.EmbeddingServiceInterface,
	cfg *ContextualEnhancerConfig,
) *ContextualRetrievalEnhancer {
	if cfg == nil {
		cfg = DefaultContextualEnhancerConfig()
	}
	bs := cfg.BatchSize
	if bs <= 0 {
		bs = 10
	}
	mct := cfg.MaxCtxToken
	if mct <= 0 {
		mct = 100
	}
	return &ContextualRetrievalEnhancer{
		db:          db,
		chatClient:  chatClient,
		embedding:   embedding,
		batchSize:   bs,
		maxCtxToken: mct,
	}
}

// EnhanceDocument 对单个文档的所有 chunk 做上下文增强（索引期一次性调用）：
// 按 chunk_index 分批，对每个 chunk 调 LLM 生成 50-100 token 上下文，
// 增强内容 = ctx + "\n\n" + content，批量重新 embedding 后回写
// knowledge_chunks 的 contextual_context/embedding/embedding_id/embed_status。
// 单 chunk 失败仅记录日志，不阻断整批；文档无 chunk 时直接返回 nil。
func (e *ContextualRetrievalEnhancer) EnhanceDocument(ctx context.Context, documentID uint64) error {
	if e == nil || e.db == nil {
		return fmt.Errorf("contextual enhancer 未初始化")
	}

	var doc struct {
		Title  string `gorm:"column:title"`
		Source string `gorm:"column:source_ref"`
	}
	if err := e.db.Table("knowledge_documents").
		Select("title, source_ref").
		Where("id = ?", documentID).
		Scan(&doc).Error; err != nil {
		return fmt.Errorf("查询文档失败: %w", err)
	}

	var chunks []struct {
		ID         uint64 `gorm:"column:id"`
		ChunkIndex int    `gorm:"column:chunk_index"`
		Content    string `gorm:"column:content"`
	}
	if err := e.db.Table("knowledge_chunks").
		Select("id, chunk_index, content").
		Where("document_id = ?", documentID).
		Order("chunk_index ASC").
		Scan(&chunks).Error; err != nil {
		return fmt.Errorf("查询 chunks 失败: %w", err)
	}
	if len(chunks) == 0 {
		return nil
	}

	docSummary := fmt.Sprintf("文档标题: %s\n文档来源: %s\n总段数: %d",
		defaultIfEmpty(doc.Title, "（无标题）"),
		defaultIfEmpty(doc.Source, "（无来源）"),
		len(chunks))

	for i := 0; i < len(chunks); i += e.batchSize {
		end := i + e.batchSize
		if end > len(chunks) {
			end = len(chunks)
		}
		batch := chunks[i:end]

		type updateItem struct {
			id      uint64
			ctx     string
			content string
		}
		updates := make([]updateItem, 0, len(batch))
		embedInputs := make([]string, 0, len(batch))

		for _, c := range batch {
			ctxText, err := e.generateContext(ctx, docSummary, c.ChunkIndex, len(chunks), c.Content)
			if err != nil {
				ctxText = ""
			}
			enhancedContent := c.Content
			if ctxText != "" {
				enhancedContent = ctxText + "\n\n" + c.Content
			}
			updates = append(updates, updateItem{id: c.ID, ctx: ctxText, content: enhancedContent})
			embedInputs = append(embedInputs, enhancedContent)
		}

		var vectors [][]float32
		if e.embedding != nil {
			cfg := e.embedding.DefaultConfig()
			v, err := e.embedding.Embed(ctx, cfg, embedInputs)
			if err != nil {
				return fmt.Errorf("重新 embedding 失败: %w", err)
			}
			vectors = v
		}

		for i, u := range updates {
			if u.ctx == "" {
				continue
			}
			if e.embedding != nil && i < len(vectors) {
				vecLiteral := vecToPGString(vectors[i])
				if err := e.updateDocumentChunk(ctx, documentID, u.id, map[string]any{
					"contextual_context": u.ctx,
					"embedding":          gorm.Expr("?::vector", vecLiteral),
					"embedding_id":       fmt.Sprintf("ctx-%d", u.id),
					"embed_status":       "indexed",
					"updated_at":         time.Now(),
				}); err != nil {
					logger.Errorf("contextual_retrieval: update knowledge_chunks (with embedding) failed, chunk_id=%d: %v", u.id, err)
				}
			} else {
				if err := e.updateDocumentChunk(ctx, documentID, u.id, map[string]any{
					"contextual_context": u.ctx,
					"updated_at":         time.Now(),
				}); err != nil {
					logger.Errorf("contextual_retrieval: update knowledge_chunks (no embedding) failed, chunk_id=%d: %v", u.id, err)
				}
			}
		}
	}
	return nil
}

func (e *ContextualRetrievalEnhancer) updateDocumentChunk(ctx context.Context, documentID, chunkID uint64, updates map[string]any) error {
	if e.ownerToken == "" {
		return e.db.WithContext(ctx).Table("knowledge_chunks").
			Where("id = ? AND document_id = ?", chunkID, documentID).Updates(updates).Error
	}
	return e.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var doc model.KnowledgeDocument
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND reindex_owner_token = ? AND embed_status = ? AND reindex_lease_until > ?", documentID, e.ownerToken, model.EmbedStatusProcessing, time.Now()).
			First(&doc).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("contextual indexing owner lost")
		}
		if err != nil {
			return err
		}
		return tx.Table("knowledge_chunks").Where("id = ? AND document_id = ?", chunkID, documentID).Updates(updates).Error
	})
}

func (e *ContextualRetrievalEnhancer) generateContext(ctx context.Context, docSummary string, chunkIdx, totalChunks int, chunkContent string) (string, error) {
	if e.chatClient == nil {
		return "", nil
	}
	prompt := fmt.Sprintf(`<document>
%s
</document>

<chunk>
这是文档的第 %d 段（共 %d 段）:
%s
</chunk>

请用 50-100 token 简要描述该段在文档中的位置、主题和上下文关系。直接输出描述，不要其他内容。`,
		docSummary, chunkIdx+1, totalChunks, chunkContent)

	resp, err := e.chatClient.Chat(ctx, prompt, LLMChatOptions{
		Temperature: 0.0,
		MaxTokens:   150,
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp), nil
}

func defaultIfEmpty(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}
