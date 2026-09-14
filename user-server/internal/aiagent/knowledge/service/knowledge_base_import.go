// Package service 知识库子域 —— 文档导入与异步处理
//
// 文档导入（保存文件 + 创建记录）+ 异步处理流水线（分片/向量化/入索引）。
// 单一职责：所有"把外部文件变成可检索内容"的代码集中在此文件。
package service

import (
	"context"
	"errors"
	"fmt"
	"hivemtk-user/internal/aiagent/knowledge/model"
	rag_core "hivemtk-user/internal/aiagent/rag/core"
	ragretrieval "hivemtk-user/internal/aiagent/rag/retrieval"
	"hivemtk-user/internal/pkg/utils/async"
	"hivemtk-user/internal/pkg/utils/logger"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ImportDocumentResult 单文档导入结果
type ImportDocumentResult struct {
	DocumentID uint   `json:"document_id"`
	Title      string `json:"title"`
	FilePath   string `json:"file_path"`
	Status     string `json:"status"`
}

const MaxUploadFileSize int64 = 50 << 20

// ImportDocument 导入文档:保存文件 + 创建记录(status=pending) + 异步处理
func (s *KnowledgeBaseService) ImportDocument(ctx context.Context, title string, file multipart.File, header *multipart.FileHeader) (*ImportDocumentResult, error) {
	if header == nil {
		return nil, errors.New("文件不能为空")
	}
	if header.Size > MaxUploadFileSize {
		return nil, fmt.Errorf("文件过大: %d 字节, 上限 %d MB", header.Size, MaxUploadFileSize>>20)
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	allowed := map[string]bool{".pdf": true, ".docx": true, ".doc": true, ".txt": true, ".md": true}
	if !allowed[ext] {
		return nil, fmt.Errorf("不支持的文件类型: %s", ext)
	}

	uploadDir := filepath.Join("uploads", "knowledge-base", time.Now().Format("20060102"))
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		return nil, fmt.Errorf("创建上传目录失败: %w", err)
	}
	filename := uuid.New().String() + ext
	filePath := filepath.Join(uploadDir, filename)

	dst, err := os.Create(filePath)
	if err != nil {
		return nil, fmt.Errorf("创建文件失败: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, io.LimitReader(file, MaxUploadFileSize+1)); err != nil {
		_ = os.Remove(filePath)
		return nil, fmt.Errorf("写入文件失败: %w", err)
	}
	if size, _ := getFileSize(filePath); size > MaxUploadFileSize {
		_ = os.Remove(filePath)
		return nil, fmt.Errorf("文件超过大小上限 %d MB", MaxUploadFileSize>>20)
	}

	size, _ := getFileSize(filePath)

	if title == "" {
		title = strings.TrimSuffix(header.Filename, ext)
	}

	doc := &model.KBDocument{
		Title:    title,
		FilePath: filePath,
		FileSize: size,
		FileType: ext,
		Status:   model.KBDocumentStatusPending,
	}
	if err := s.db.WithContext(ctx).Create(doc).Error; err != nil {
		_ = os.Remove(filePath)
		return nil, fmt.Errorf("保存文档记录失败: %w", err)
	}

	async.RunWithTimeout(ctx, AsyncProcessingTimeout(), func(procCtx context.Context) {
		s.processDocumentAsync(procCtx, doc.ID, filePath)
	})

	return &ImportDocumentResult{
		DocumentID: doc.ID,
		Title:      doc.Title,
		FilePath:   filePath,
		Status:     string(doc.Status),
	}, nil
}

func (s *KnowledgeBaseService) processDocumentAsync(ctx context.Context, documentID uint, filePath string) {
	bgCtx := ctx
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("[knowledge-base] processDocumentAsync panic doc=%d: %v", documentID, r)
			s.markDocumentFailed(bgCtx, documentID, fmt.Sprintf("处理异常: %v", r))
		}
	}()

	_ = s.db.WithContext(bgCtx).Model(&model.KBDocument{}).
		Where("id = ?", documentID).
		Updates(map[string]any{"status": model.KBDocumentStatusProcessing, "error_msg": ""}).Error

	contentBytes, err := os.ReadFile(filePath)
	if err != nil {
		s.markDocumentFailed(bgCtx, documentID, fmt.Sprintf("读取文件失败: %v", err))
		return
	}
	content := string(contentBytes)

	ragDoc := rag_core.Document{
		ID:      fmt.Sprintf("kbdoc_%d", documentID),
		Content: content,
		Metadata: map[string]any{
			"document_id": documentID,
			"file_path":   filePath,
			"title":       filepath.Base(filePath),
		},
		CreatedAt: time.Now(),
	}

	chunks, err := s.processor.ProcessDocument(bgCtx, ragDoc)
	if err != nil {
		s.markDocumentFailed(bgCtx, documentID, fmt.Sprintf("分片失败: %v", err))
		return
	}

	if len(chunks) == 0 {
		s.markDocumentFailed(bgCtx, documentID, "文档分片结果为空")
		return
	}

	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
	}
	embeddings, err := s.vector.EmbedBatch(texts)
	if err != nil {
		s.markDocumentFailed(bgCtx, documentID, fmt.Sprintf("向量化失败: %v", err))
		return
	}

	idxChunks := make([]ragretrieval.Chunk, len(chunks))
	for i, c := range chunks {
		idxChunks[i] = ragretrieval.Chunk{
			ID:         c.ID,
			DocumentID: c.DocumentID,
			Content:    c.Content,
			Metadata:   c.Metadata,
			Embedding:  embeddings[i],
			Score:      c.Score,
			TokenCount: c.TokenCount,
		}
	}

	kbIndexID := fmt.Sprintf("kb_%d", documentID)
	if err := s.indexer.BuildIndex(bgCtx, kbIndexID, idxChunks); err != nil {
		s.markDocumentFailed(bgCtx, documentID, fmt.Sprintf("构建索引失败: %v", err))
		return
	}

	if err := s.db.WithContext(bgCtx).Model(&model.KBDocument{}).Where("id = ?", documentID).Updates(map[string]any{
		"status":      model.KBDocumentStatusIndexed,
		"chunk_count": len(chunks),
		"content":     content,
		"error_msg":   "",
	}).Error; err != nil {
		// 索引已建成但状态写失败：文档将滞留 processing，前端无法感知——必须落日志供运维介入
		logger.Errorf("[KBImport] 标记文档已索引失败 doc=%d: %v", documentID, err)
	}
}

func (s *KnowledgeBaseService) markDocumentFailed(ctx context.Context, documentID uint, errMsg string) {
	if err := s.db.WithContext(ctx).Model(&model.KBDocument{}).Where("id = ?", documentID).Updates(map[string]any{
		"status":    model.KBDocumentStatusFailed,
		"error_msg": errMsg,
	}).Error; err != nil {
		logger.Errorf("[KBImport] 标记文档失败状态失败 doc=%d (原错误: %s): %v", documentID, errMsg, err)
	}
}
