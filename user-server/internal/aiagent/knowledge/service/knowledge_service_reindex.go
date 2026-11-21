package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"hivemtk-user/internal/aiagent/knowledge/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

const (
	reindexExecutionTimeout    = 10 * time.Minute
	reindexFailureWriteTimeout = 5 * time.Second
)

// Reindex 重建指定文档的向量索引。复用已落库的分片内容重新向量化，不重新分块。
func (s *KnowledgeService) Reindex(ctx context.Context, productID string, docID uint64) error {
	workCtx, cancel := context.WithTimeout(ctx, reindexExecutionTimeout)
	defer cancel()
	doc, err := s.docRepo.GetByProductAndID(workCtx, productID, int64(docID))
	if err != nil {
		return err
	}
	owner, claimed, err := s.docRepo.ClaimForReindexWithLease(workCtx, productID, doc.ID, reindexExecutionTimeout+knowledgeOwnerLeaseMargin)
	if err != nil {
		return fmt.Errorf("文档 %d 认领重建失败: %w", doc.ID, err)
	}
	if !claimed {
		return fmt.Errorf("文档 %d 正由其他任务处理，无法重复认领", doc.ID)
	}
	chunks, err := s.chunkRepo.GetByDocumentID(workCtx, doc.ID)
	if err != nil {
		return s.markReindexFailed(workCtx, doc.ID, owner, err)
	}
	logger.Infof("[knowledge][Reindex] called doc=%d chunks=%d", doc.ID, len(chunks))
	if len(chunks) == 0 {

		content := ""
		logger.Infof("[knowledge][Reindex] zero-chunk re-pipeline doc=%d file=%s", doc.ID, doc.FilePath)
		if doc.FilePath != "" {
			if b, rerr := os.ReadFile(doc.FilePath); rerr == nil {
				content = string(b)
			}
		}
		if strings.TrimSpace(content) == "" {
			return s.markReindexFailed(workCtx, doc.ID, owner, errors.New("重建失败：0 分片且源内容不可读（源文件路径为空、文件缺失或文档内容为空）"))
		}
		meta := map[string]any{}
		if doc.Metadata != "" {
			_ = json.Unmarshal([]byte(doc.Metadata), &meta)
		}
		if err := s.processDocumentOwned(workCtx, doc.ID, doc.ProductID, owner, doc.FilePath,
			doc.FileName, content, doc.MimeType, doc.Title, doc.SourceType, meta); err != nil {
			return fmt.Errorf("文档 %d 重建失败: %w", doc.ID, err)
		}
		return nil
	}
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
	}
	embService, embCfg := s.resolveEmbeddingConfig(workCtx, productID)
	var embeddings [][]float32
	if s.reindexEmbed != nil {
		if embCfg == nil {
			embCfg = embService.DefaultConfig()
		}
		embeddings, err = s.reindexEmbed(workCtx, embService, embCfg, texts)
	} else {
		embeddings, err = embService.Embed(workCtx, embCfg, texts)
	}
	if err != nil {
		return s.markReindexFailed(workCtx, doc.ID, owner, err)
	}

	if err := persistValidatedChunkEmbeddings(embService, embCfg, chunks, embeddings, func(validChunks []model.KnowledgeChunk, validEmbeddings [][]float32) error {
		return s.chunkRepo.UpdateEmbeddingsBatchWithSourceForOwner(workCtx, doc.ID, owner, validChunks, validEmbeddings, "tei")
	}); err != nil {
		logger.Errorf("[knowledge][Reindex] 向量结果校验或持久化失败 doc=%d: %v", doc.ID, err)
		return s.markReindexFailed(workCtx, doc.ID, owner, err)
	}
	totalTokens := 0
	for _, chunk := range chunks {
		totalTokens += chunk.TokenCount
	}
	if err := s.docRepo.UpdateChunkStatsForOwner(workCtx, doc.ID, owner, len(chunks), totalTokens); err != nil {
		return s.markReindexFailed(workCtx, doc.ID, owner, err)
	}
	if err := s.docRepo.UpdateStatusForOwner(workCtx, doc.ID, owner, model.EmbedStatusIndexed, 100, ""); err != nil {
		return s.markReindexFailed(workCtx, doc.ID, owner, fmt.Errorf("更新文档索引状态失败: %w", err))
	}
	return nil
}

func (s *KnowledgeService) markReindexFailed(ctx context.Context, documentID uint64, owner string, cause error) error {
	cleanupCtx, cancel := reindexFailureContext(ctx)
	defer cancel()
	if err := s.docRepo.UpdateStatusForOwner(cleanupCtx, documentID, owner, model.EmbedStatusFailed, 0, cause.Error()); err != nil {
		logger.Errorf("[knowledge][Reindex] 标记重建失败状态错误 doc=%d: %v", documentID, err)
		return errors.Join(cause, fmt.Errorf("持久化失败状态失败: %w", err))
	}
	return cause
}

func reindexFailureContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(context.WithoutCancel(ctx), reindexFailureWriteTimeout)
}

// RebuildIndex 重建某产品下全部文档的向量索引。
func (s *KnowledgeService) RebuildIndex(ctx context.Context, productID string) error {
	// D-26：产品不存在时必须显式报错，不能静默成功——否则前端会误以为索引已重建。
	// GetRagProductByID 返回 "rag product not found"，controller 的 isNotFoundError 会映射为 404。
	if _, err := s.ragRepo.GetRagProductByID(ctx, productID); err != nil {
		return err
	}
	const pageSize = 500
	documentIDs, err := s.docRepo.ListProductDocumentIDsSnapshot(ctx, productID, pageSize)
	if err != nil {
		return fmt.Errorf("读取产品文档快照失败: %w", err)
	}
	var rebuildErrors []error
	for _, docID := range documentIDs {
		if err := s.Reindex(ctx, productID, docID); err != nil {
			rebuildErrors = append(rebuildErrors, fmt.Errorf("文档 %d: %w", docID, err))
		}
	}
	return errors.Join(rebuildErrors...)
}
