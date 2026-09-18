// Package service 知识库子域 —— 导入实现
//
// 多种来源（上传文件/纯文本/URL）的导入实现 + meta 序列化辅助。
// 单一职责：把外部来源（Upload/Text/URL/OpenAPI/Batch）转成 KnowledgeDocument 行。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hivemtk-user/internal/aiagent/knowledge/model"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

func metaToJSON(m map[string]any) string {
	if len(m) == 0 {
		return "{}"
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func (s *KnowledgeService) importUploadedFile(ctx context.Context, req *ImportRequest, product *model.RagProduct, productNumericID string) (*model.KnowledgeDocument, error) {
	if req.File == nil || req.FileHeader == nil {
		return nil, errors.New("文件不能为空")
	}
	if req.FileHeader.Size > MaxUploadFileSize {
		return nil, fmt.Errorf("文件过大: %d 字节, 上限 %d MB", req.FileHeader.Size, MaxUploadFileSize>>20)
	}
	ext := strings.ToLower(filepath.Ext(req.FileHeader.Filename))
	allowed := map[string]bool{".pdf": true, ".docx": true, ".doc": true, ".txt": true, ".md": true, ".html": true, ".json": true, ".csv": true}
	if !allowed[ext] {
		return nil, fmt.Errorf("不支持的文件类型: %s", ext)
	}

	uploadDir := filepath.Join("uploads", "knowledge", req.ProductID, time.Now().Format("20060102"))
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		return nil, fmt.Errorf("创建上传目录失败: %w", err)
	}
	filename := uuid.New().String() + ext
	filePath := filepath.Join(uploadDir, filename)
	dst, err := os.Create(filePath)
	if err != nil {
		return nil, fmt.Errorf("创建文件失败: %w", err)
	}
	defer func() { _ = dst.Close() }()

	size, err := io.Copy(dst, io.LimitReader(req.File, MaxUploadFileSize+1))
	if err != nil {
		_ = os.Remove(filePath)
		return nil, fmt.Errorf("写入文件失败: %w", err)
	}
	if size > MaxUploadFileSize {
		_ = os.Remove(filePath)
		return nil, fmt.Errorf("文件超过大小上限 %d MB", MaxUploadFileSize>>20)
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(filePath)
		return nil, fmt.Errorf("写入文件失败: %w", err)
	}

	title := req.Title
	if title == "" {
		title = strings.TrimSuffix(req.FileHeader.Filename, ext)
	}

	tagsJSON, _ := json.Marshal(req.Tags)
	doc := &model.KnowledgeDocument{

		ProductID:   productNumericID,
		SourceType:  req.SourceType,
		SourceRef:   req.SourceRef,
		Title:       title,
		FileName:    req.FileHeader.Filename,
		FilePath:    filePath,
		FileType:    ext,
		FileSize:    size,
		MimeType:    getMimeType(ext),
		EmbedStatus: model.EmbedStatusPending,
		Category:    req.Category,
		Tags:        string(tagsJSON),
		Metadata:    metaToJSON(req.Metadata),
		Status:      1,
	}
	if err := s.docRepo.Create(ctx, doc); err != nil {
		_ = os.Remove(filePath)
		return nil, fmt.Errorf("保存文档记录失败: %w", err)
	}
	return doc, nil
}

func (s *KnowledgeService) importText(ctx context.Context, req *ImportRequest, product *model.RagProduct, productNumericID string) (*model.KnowledgeDocument, error) {
	if strings.TrimSpace(req.Content) == "" {
		return nil, errors.New("内容不能为空")
	}
	if req.Title == "" {
		req.Title = "未命名文档_" + time.Now().Format("20060102150405")
	}
	tagsJSON, _ := json.Marshal(req.Tags)
	tmpDir := filepath.Join("uploads", "knowledge-text", req.ProductID, time.Now().Format("20060102"))
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return nil, fmt.Errorf("创建目录失败: %w", err)
	}
	textFile := filepath.Join(tmpDir, uuid.New().String()+".txt")
	if err := os.WriteFile(textFile, []byte(req.Content), 0644); err != nil {
		return nil, fmt.Errorf("写入文本失败: %w", err)
	}
	doc := &model.KnowledgeDocument{

		ProductID:   productNumericID,
		SourceType:  req.SourceType,
		SourceRef:   req.SourceRef,
		Title:       req.Title,
		FileName:    req.Title + ".txt",
		FilePath:    textFile,
		FileType:    ".txt",
		FileSize:    int64(len(req.Content)),
		MimeType:    "text/plain",
		EmbedStatus: model.EmbedStatusPending,
		Category:    req.Category,
		Tags:        string(tagsJSON),
		Metadata:    metaToJSON(req.Metadata),
		Status:      1,
	}
	if err := s.docRepo.Create(ctx, doc); err != nil {
		_ = os.Remove(textFile)
		return nil, fmt.Errorf("保存文档记录失败: %w", err)
	}
	return doc, nil
}

func (s *KnowledgeService) importFromURL(ctx context.Context, req *ImportRequest, product *model.RagProduct, productNumericID string) (*model.KnowledgeDocument, error) {
	if req.SourceRef == "" {
		return nil, errors.New("URL 不能为空")
	}
	if err := validateURL(req.SourceRef); err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(req.SourceRef)
	if err != nil {
		return nil, fmt.Errorf("抓取 URL 失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("URL 返回错误状态: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	content := string(body)
	content = stripHTML(content)

	title := req.Title
	if title == "" {
		title = filepath.Base(req.SourceRef)
		if idx := strings.Index(title, "?"); idx > 0 {
			title = title[:idx]
		}
	}
	tagsJSON, _ := json.Marshal(req.Tags)
	tmpDir := filepath.Join("uploads", "knowledge-url", req.ProductID, time.Now().Format("20060102"))
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return nil, fmt.Errorf("创建目录失败: %w", err)
	}
	textFile := filepath.Join(tmpDir, uuid.New().String()+".html")
	if err := os.WriteFile(textFile, []byte(content), 0644); err != nil {
		return nil, fmt.Errorf("写入URL内容失败: %w", err)
	}
	doc := &model.KnowledgeDocument{

		ProductID:   productNumericID,
		SourceType:  model.SourceTypeURL,
		SourceRef:   req.SourceRef,
		Title:       title,
		FileName:    title + ".html",
		FilePath:    textFile,
		FileType:    ".html",
		FileSize:    int64(len(content)),
		MimeType:    "text/html",
		EmbedStatus: model.EmbedStatusPending,
		Category:    req.Category,
		Tags:        string(tagsJSON),
		Metadata:    metaToJSON(req.Metadata),
		Status:      1,
	}
	if err := s.docRepo.Create(ctx, doc); err != nil {
		_ = os.Remove(textFile)
		return nil, fmt.Errorf("保存文档记录失败: %w", err)
	}
	return doc, nil
}
