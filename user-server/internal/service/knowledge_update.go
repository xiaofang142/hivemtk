package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// KBDocumentChunk 文档切片（域内只读别名，gorm 行模型在 repository.KBDocumentChunkRow）
type KBDocumentChunk = repository.KBDocumentChunkRow

// KBDocumentChunkRow 仓储层行模型别名（沿用旧名，减少 diff）
type KBDocumentChunkRow = repository.KBDocumentChunkRow

type KnowledgeUpdateService struct {
	repo *repository.KBDocumentChunkRepository
}

// NewKnowledgeUpdateService 创建增量更新服务
func NewKnowledgeUpdateService() *KnowledgeUpdateService {
	return &KnowledgeUpdateService{repo: repository.NewKBDocumentChunkRepository(db.GetDB())}
}

// NewKnowledgeUpdateServiceWithDB 注入 DB（测试用）
func (s *KnowledgeUpdateService) WithDB(d *gorm.DB) *KnowledgeUpdateService {
	s.repo = repository.NewKBDocumentChunkRepository(d)
	return s
}

// UpdateDeltaResult 增量更新结果
type UpdateDeltaResult struct {
	DocumentID  uint      `json:"document_id"`
	TotalChunks int       `json:"total_chunks"`
	Unchanged   int       `json:"unchanged"`
	Added       int       `json:"added"`
	Removed     int       `json:"removed"`
	Updated     int       `json:"updated"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
}

// UpdateDocumentDelta 对指定文档执行增量切片更新
// 如果 chunks 表不存在或旧 chunks 为空，退化为全量重建
func (s *KnowledgeUpdateService) UpdateDocumentDelta(ctx context.Context, documentID uint, newContent string) (*UpdateDeltaResult, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("db 未初始化")
	}
	startedAt := time.Now()
	result := &UpdateDeltaResult{DocumentID: documentID, StartedAt: startedAt}

	newChunks := splitIntoChunks(newContent, 500)
	result.TotalChunks = len(newChunks)
	newHashes := make(map[string]int, len(newChunks))
	for i, c := range newChunks {
		h := contentHash(c)
		newHashes[h] = i
	}

	oldRows, err := s.repo.ListActiveByDocument(ctx, documentID)
	if err != nil {
		return nil, fmt.Errorf("查询旧 chunks: %w", err)
	}
	oldChunks := make([]KBDocumentChunk, len(oldRows))
	for i := range oldRows {
		oldChunks[i] = KBDocumentChunk(oldRows[i])
	}

	if len(oldChunks) == 0 {
		for i, c := range newChunks {
			chunk := KBDocumentChunkRow{
				DocumentID:   documentID,
				ChunkIndex:   i,
				ContentHash:  contentHash(c),
				ChunkContent: c,
				Status:       "active",
			}
			if err := s.repo.Create(ctx, &chunk); err != nil {
				logger.Warnf("[KBUpdate] 新建 chunk 失败 doc=%d idx=%d: %v", documentID, i, err)
			}
			result.Added++
		}
		result.FinishedAt = time.Now()
		return result, nil
	}

	oldHashSet := make(map[string]*KBDocumentChunk, len(oldChunks))
	for i := range oldChunks {
		oldHashSet[oldChunks[i].ContentHash] = &oldChunks[i]
	}

	toDelete := make([]uint64, 0)
	for _, oc := range oldChunks {
		if _, ok := newHashes[oc.ContentHash]; !ok {
			toDelete = append(toDelete, oc.ID)
		}
	}
	if len(toDelete) > 0 {
		if err := s.repo.MarkSuperseded(ctx, toDelete); err != nil {
			logger.Warnf("[KBUpdate] 标记 superseded 失败 doc=%d: %v", documentID, err)
		}
		result.Removed = len(toDelete)
	}

	newIdx := 0
	for _, c := range newChunks {
		h := contentHash(c)
		newIdx = newHashes[h]
		if _, existed := oldHashSet[h]; !existed {
			chunk := KBDocumentChunkRow{
				DocumentID:   documentID,
				ChunkIndex:   newIdx,
				ContentHash:  h,
				ChunkContent: c,
				Status:       "active",
			}
			if err := s.repo.Create(ctx, &chunk); err != nil {
				logger.Warnf("[KBUpdate] 新增 chunk 失败 doc=%d idx=%d: %v", documentID, newIdx, err)
			}
			result.Added++
		} else {
			result.Unchanged++
		}
	}

	result.FinishedAt = time.Now()
	logger.Infof("[KBUpdate] 增量更新完成 doc=%d unchanged=%d added=%d removed=%d duration=%s",
		documentID, result.Unchanged, result.Added, result.Removed,
		result.FinishedAt.Sub(startedAt).Round(time.Millisecond))
	return result, nil
}

func contentHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func splitIntoChunks(content string, windowSize int) []string {
	if windowSize <= 0 {
		windowSize = 500
	}
	runes := []rune(content)
	if len(runes) == 0 {
		return []string{}
	}
	var chunks []string
	for i := 0; i < len(runes); i += windowSize {
		end := i + windowSize
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[i:end]))
	}
	return chunks
}
