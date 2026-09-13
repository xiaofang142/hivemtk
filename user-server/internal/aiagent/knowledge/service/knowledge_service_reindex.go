package service

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"

	"hivemtk-user/internal/aiagent/knowledge/model"
	"hivemtk-user/internal/aiagent/knowledge/repository"
	"hivemtk-user/internal/pkg/utils/async"
	"hivemtk-user/internal/pkg/utils/logger"
)

// Reindex 重建指定文档的向量索引。复用已落库的分片内容重新向量化，不重新分块。
func (s *KnowledgeService) Reindex(ctx context.Context, productID string, docID uint64) error {
	doc, err := s.docRepo.GetByProductAndID(ctx, productID, int64(docID))
	if err != nil {
		return err
	}
	chunks, err := s.chunkRepo.GetByDocumentID(ctx, doc.ID)
	if err != nil {
		return err
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
			_ = s.docRepo.UpdateStatus(ctx, doc.ID, model.EmbedStatusFailed, 0,
				"重建失败：0 分片且源内容不可读（文件缺失或为空）")
			return nil
		}
		meta := map[string]any{}
		if doc.Metadata != "" {
			_ = json.Unmarshal([]byte(doc.Metadata), &meta)
		}
		async.RunWithTimeout(ctx, AsyncProcessingTimeout(), func(procCtx context.Context) {
			s.processDocumentAsync(procCtx, doc.ID, doc.ProductID, doc.FilePath,
				doc.FileName, content, doc.MimeType, doc.Title, doc.SourceType, meta)
		})
		return nil
	}
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
	}
	embService, embCfg := s.resolveEmbeddingConfig(ctx, productID)
	embeddings, err := embService.Embed(ctx, embCfg, texts)
	if err != nil {
		_ = s.docRepo.UpdateStatus(ctx, doc.ID, model.EmbedStatusFailed, 0, err.Error())
		return err
	}

	chunks, embeddings = filterValidEmbeddings(embService, embCfg, chunks, embeddings)
	if err := s.chunkRepo.UpdateEmbeddingsBatch(ctx, chunks, embeddings); err != nil {
		return err
	}
	now := time.Now()
	doc.EmbedStatus = model.EmbedStatusIndexed
	doc.LastIndexAt = &now
	if err := s.docRepo.Update(ctx, doc); err != nil {
		// 向量已重建但文档状态未落库：文档滞留旧状态，运维无从判断——必须落日志
		logger.Errorf("[knowledge][Reindex] 更新文档索引状态失败 doc=%d: %v", doc.ID, err)
	}
	return nil
}

// RebuildIndex 重建某产品下全部文档的向量索引。
func (s *KnowledgeService) RebuildIndex(ctx context.Context, productID string) error {
	docs, _, err := s.docRepo.List(ctx, repository.ListFilter{ProductID: productID, PageSize: 1000})
	if err != nil {
		return err
	}
	var firstErr error
	for _, d := range docs {
		if err := s.Reindex(ctx, productID, d.ID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
