package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"hivemtk-user/internal/aiagent/knowledge/model"
	"hivemtk-user/internal/aiagent/knowledge/repository"
	"hivemtk-user/internal/aiagent/llm"
	rag_core "hivemtk-user/internal/aiagent/rag/core"
	"hivemtk-user/internal/etl"
	"hivemtk-user/internal/pkg/utils/logger"
)

const reindexPageSize = 100

// Reindex 重建指定文档的向量索引。该入口同步完成所有步骤，并将失败
// 留在文档状态中，供调用方和运维查询。
func (s *KnowledgeService) Reindex(ctx context.Context, productID string, docID uint64) error {
	doc, err := s.docRepo.GetByProductAndID(ctx, productID, int64(docID))
	if err != nil {
		return err
	}
	if err := s.docRepo.BeginReindex(ctx, doc.ID); err != nil {
		return s.reindexFailure(ctx, doc.ID, fmt.Errorf("重建失败：记录尝试次数失败: %w", err))
	}
	chunks, err := s.chunkRepo.GetByDocumentID(ctx, doc.ID)
	if err != nil {
		return s.reindexFailure(ctx, doc.ID, fmt.Errorf("重建失败：读取分段失败: %w", err))
	}
	logger.Infof("[knowledge][Reindex] called doc=%d chunks=%d", doc.ID, len(chunks))

	if len(chunks) == 0 {
		chunks, err = s.buildReindexChunks(ctx, doc)
		if err != nil {
			return s.reindexFailure(ctx, doc.ID, err)
		}
	}
	if err := s.reindexChunks(ctx, doc, chunks); err != nil {
		return s.reindexFailure(ctx, doc.ID, err)
	}
	s.touchKBForContentChange(ctx, "document_reindex")
	return nil
}

// buildReindexChunks synchronously reconstructs chunks for a document whose
// source exists but whose chunk rows are missing. It intentionally does not
// call processDocumentAsync, so the successful API response cannot race the
// actual work.
func (s *KnowledgeService) buildReindexChunks(ctx context.Context, doc *model.KnowledgeDocument) ([]model.KnowledgeChunk, error) {
	if strings.TrimSpace(doc.FilePath) == "" {
		return nil, errors.New("重建失败：0 分片且源文件路径为空")
	}
	b, err := os.ReadFile(doc.FilePath)
	if err != nil {
		return nil, fmt.Errorf("重建失败：读取文件失败: %w", err)
	}
	content := string(b)
	if etl.IsImageExt(doc.FileName) {
		content, err = etl.DescribeImage(ctx, doc.FileName, b)
		if err != nil {
			return nil, fmt.Errorf("重建失败：图片处理失败: %w", err)
		}
	} else {
		extracted, parseErr := etl.ParseDocument(ctx, doc.FileName, b)
		if parseErr != nil {
			if !etl.IsPlainTextExt(doc.FileName) {
				return nil, fmt.Errorf("重建失败：文本提取失败（可能为扫描件或损坏文档）: %w", parseErr)
			}
			extracted = content
		}
		content = extracted
	}
	if strings.TrimSpace(content) == "" {
		return nil, errors.New("重建失败：文档内容为空（可能为扫描件或空文件）")
	}

	meta := map[string]any{}
	if doc.Metadata != "" {
		if err := json.Unmarshal([]byte(doc.Metadata), &meta); err != nil {
			return nil, fmt.Errorf("重建失败：文档元数据无效: %w", err)
		}
	}
	ragDoc := rag_core.Document{
		ID:      fmt.Sprintf("kb_doc_%d", doc.ID),
		Content: content,
		Metadata: map[string]any{
			"document_id": doc.ID,
			"product_id":  doc.ProductID,
			"title":       doc.Title,
			"source":      string(doc.SourceType),
		},
		CreatedAt: doc.CreatedAt,
	}
	for k, v := range meta {
		ragDoc.Metadata[k] = v
	}
	chunks, err := s.processor.ProcessDocument(ctx, ragDoc)
	if err != nil {
		return nil, fmt.Errorf("重建失败：分片失败: %w", err)
	}
	if len(chunks) == 0 {
		return nil, errors.New("重建失败：分片结果为空")
	}

	parentIDs, err := s.createChunkParents(ctx, doc.ID, doc.ProductID, len(chunks), func(i int) (string, int) {
		return chunks[i].Content, chunks[i].TokenCount
	})
	if err != nil {
		return nil, fmt.Errorf("重建失败：写入父块失败: %w", err)
	}

	chunkModels := make([]model.KnowledgeChunk, 0, len(chunks))
	for i, chunk := range chunks {
		hash := sha256.Sum256([]byte(chunk.Content))
		chunkMeta := map[string]any{
			"document_id": float64(doc.ID),
			"product_id":  doc.ProductID,
			"title":       doc.Title,
			"source":      string(doc.SourceType),
			"chunk_index": float64(i),
		}
		for k, v := range meta {
			chunkMeta[k] = v
		}
		metaBytes, _ := json.Marshal(chunkMeta)
		chunkModels = append(chunkModels, model.KnowledgeChunk{
			ParentChunkID: parentIDs[i],
			DocumentID:    doc.ID,
			ProductID:     doc.ProductID,
			ChunkIndex:    i,
			Content:       chunk.Content,
			ContentHash:   hex.EncodeToString(hash[:]),
			TokenCount:    chunk.TokenCount,
			CharCount:     len(chunk.Content),
			Metadata:      string(metaBytes),
		})
	}
	if err := s.chunkRepo.BatchCreate(ctx, chunkModels); err != nil {
		return nil, fmt.Errorf("重建失败：写入分段失败: %w", err)
	}
	return chunkModels, nil
}

func (s *KnowledgeService) reindexChunks(ctx context.Context, doc *model.KnowledgeDocument, chunks []model.KnowledgeChunk) error {
	if len(chunks) == 0 {
		return errors.New("重建失败：没有可向量化的分段")
	}
	texts := make([]string, len(chunks))
	for i, chunk := range chunks {
		texts[i] = chunk.Content
	}

	embService, embCfg := s.resolveEmbeddingConfig(ctx, doc.ProductID)
	if embCfg == nil {
		embCfg = embService.DefaultConfig()
	}
	var embeddings [][]float32
	source := llm.EmbedSourceTEI
	var err error
	if s.reindexEmbed != nil {
		embeddings, err = s.reindexEmbed(ctx, embService, embCfg, texts)
	} else {
		embeddings, source, err = embService.EmbedWithSource(ctx, embCfg, texts)
	}
	if err != nil {
		return fmt.Errorf("重建失败：向量化失败: %w", err)
	}
	filteredChunks, filteredEmbeddings := filterValidEmbeddings(embService, embCfg, chunks, embeddings)
	if len(filteredChunks) != len(chunks) || len(filteredEmbeddings) != len(chunks) {
		return fmt.Errorf("重建失败：向量维度校验失败，期望 %d 条，得到 %d 条", len(chunks), len(filteredChunks))
	}
	if err := s.persistChunkEmbeddingsWithSource(ctx, filteredChunks, filteredEmbeddings, source); err != nil {
		return fmt.Errorf("重建失败：持久化向量失败: %w", err)
	}

	totalTokens := 0
	for _, chunk := range filteredChunks {
		totalTokens += chunk.TokenCount
	}
	if err := s.docRepo.CompleteReindex(ctx, doc.ID, len(filteredChunks), totalTokens); err != nil {
		return fmt.Errorf("重建失败：写入最终文档状态失败: %w", err)
	}
	return nil
}

func (s *KnowledgeService) reindexFailure(ctx context.Context, docID uint64, cause error) error {
	if cause == nil {
		return nil
	}
	statusErr := s.docRepo.UpdateStatus(ctx, docID, model.EmbedStatusFailed, 0, cause.Error())
	if statusErr != nil {
		logger.Errorf("[knowledge][Reindex] 标记失败状态错误 doc=%d: %v", docID, statusErr)
		return errors.Join(cause, fmt.Errorf("写入失败状态: %w", statusErr))
	}
	return cause
}

// RebuildIndex 重建某产品下全部文档的向量索引。
func (s *KnowledgeService) RebuildIndex(ctx context.Context, productID string) error {
	if _, err := s.ragRepo.GetRagProductByID(ctx, productID); err != nil {
		return err
	}
	boundary, err := s.docRepo.GetReindexBoundary(ctx, productID)
	if err != nil || boundary == nil {
		return err
	}

	var firstErr error
	var after *repository.ReindexCursor
	for {
		docs, pageErr := s.docRepo.ListReindexPage(ctx, productID, *boundary, after, reindexPageSize)
		if pageErr != nil {
			if firstErr == nil {
				firstErr = pageErr
			}
			break
		}
		if len(docs) == 0 {
			break
		}
		for _, doc := range docs {
			if reindexErr := s.Reindex(ctx, productID, doc.ID); reindexErr != nil && firstErr == nil {
				firstErr = reindexErr
			}
		}
		last := docs[len(docs)-1]
		after = &repository.ReindexCursor{CreatedAt: last.CreatedAt, ID: last.ID}
		if len(docs) < reindexPageSize {
			break
		}
	}
	return firstErr
}
