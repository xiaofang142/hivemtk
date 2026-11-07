// Package service 知识库子域 —— 文档导入与异步处理
//
// 文档导入（保存文件 + 创建记录）+ 异步处理流水线（分片/向量化/入索引）。
package service

import (
	"context"
	"errors"
	"fmt"
	"hivemtk-user/internal/aiagent/knowledge/model"
	rag_core "hivemtk-user/internal/aiagent/rag/core"
	ragretrieval "hivemtk-user/internal/aiagent/rag/retrieval"
	"hivemtk-user/internal/etl"
	"hivemtk-user/internal/pkg/utils/async"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/storage"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"runtime/debug"
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

// DefaultMaxUploadFileSize 是参数中心 knowledge.max_upload_file_size 的兜底值（50MB）。
const DefaultMaxUploadFileSize int64 = 50 << 20

// maxUploadFileSizeProvider 由 internal/app 的参数装配层注入。
// 传 nil 表示不注入（装配顺序错时不该把兜底值顶掉）。
var maxUploadFileSizeProvider = func() int64 { return DefaultMaxUploadFileSize }

// SetMaxUploadFileSizeProvider 注入知识库上传大小上限读取器，仅装配层调用。
func SetMaxUploadFileSizeProvider(fn func() int64) {
	if fn != nil {
		maxUploadFileSizeProvider = fn
	}
}

// MaxUploadFileSize 返回生效中的知识库上传大小上限（字节）。
// 调大会同步抬高 io.LimitReader 的读取上限与 body_limit 中间件需要容忍的单文件体积。
func MaxUploadFileSize() int64 {
	n := maxUploadFileSizeProvider()
	if n <= 0 {
		return DefaultMaxUploadFileSize
	}
	return n
}

// ImportOptions 导入选项（零值＝与历史行为完全一致）。
type ImportOptions struct {
	// QASplit 上传后可选跑 QA 拆分（对标 FastGPT「上传文档可选 QA 增强」）。
	// 默认关：拆分要调 LLM、产候选行、需要人工审，属于运营显式选择的增强，
	// 不能因为「能力已装配」就默认对每篇上传扣一次模型调用。
	QASplit bool
}

// ImportDocument 导入文档:保存文件 + 创建记录(status=pending) + 异步处理
// （零选项等价于 ImportDocumentWithOptions(..., ImportOptions{})）
func (s *KnowledgeBaseService) ImportDocument(ctx context.Context, title string, file multipart.File, header *multipart.FileHeader) (*ImportDocumentResult, error) {
	return s.ImportDocumentWithOptions(ctx, title, file, header, ImportOptions{})
}

// ImportDocumentWithOptions 带选项的导入入口。
func (s *KnowledgeBaseService) ImportDocumentWithOptions(ctx context.Context, title string, file multipart.File, header *multipart.FileHeader, opts ImportOptions) (*ImportDocumentResult, error) {
	if header == nil {
		return nil, errors.New("文件不能为空")
	}
	if header.Size > MaxUploadFileSize() {
		return nil, fmt.Errorf("文件过大: %d 字节, 上限 %d MB", header.Size, MaxUploadFileSize()>>20)
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	allowed := map[string]bool{".pdf": true, ".docx": true, ".doc": true, ".txt": true, ".md": true}
	if !allowed[ext] {
		return nil, fmt.Errorf("不支持的文件类型: %s", ext)
	}

	uploadBase, _, _ := storage.LocalSource()
	// 租户分域：on 模式落 tenant_{id}/，与 LocalDriver 用同一个前缀函数（storage.TenantPathPrefix）；
	// off 模式前缀为空串，路径与升级前一致，存量文件原地可读。
	uploadDir := filepath.Join(uploadBase, storage.TenantPathPrefix(ctx), "knowledge-base", time.Now().Format("20060102"))
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

	if _, err := io.Copy(dst, io.LimitReader(file, MaxUploadFileSize()+1)); err != nil {
		_ = os.Remove(filePath)
		return nil, fmt.Errorf("写入文件失败: %w", err)
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(filePath)
		return nil, fmt.Errorf("写入文件失败: %w", err)
	}
	if size, _ := getFileSize(filePath); size > MaxUploadFileSize() {
		_ = os.Remove(filePath)
		return nil, fmt.Errorf("文件超过大小上限 %d MB", MaxUploadFileSize()>>20)
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
	if err := s.docs.Create(ctx, doc); err != nil {
		_ = os.Remove(filePath)
		return nil, fmt.Errorf("保存文档记录失败: %w", err)
	}

	async.RunWithTimeout(ctx, AsyncProcessingTimeout(), func(procCtx context.Context) {
		s.processDocumentAsyncWithQA(procCtx, doc.ID, filePath, opts.QASplit)
	})

	return &ImportDocumentResult{
		DocumentID: doc.ID,
		Title:      doc.Title,
		FilePath:   filePath,
		Status:     string(doc.Status),
	}, nil
}

// IngestLocalFile 连接器入口：把已在本地磁盘的文件导入知识库（复用
// ImportDocument 的落库+异步处理流程，绕过 multipart 上传）。
// ext 必须带点（如 ".pdf"）且在白名单内；size 为 0 时自动 stat。
func (s *KnowledgeBaseService) IngestLocalFile(ctx context.Context, title, filePath, ext string) (uint, error) {
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	ext = strings.ToLower(ext)
	allowed := map[string]bool{".pdf": true, ".docx": true, ".doc": true, ".txt": true, ".md": true}
	if !allowed[ext] {
		return 0, fmt.Errorf("不支持的文件类型: %s", ext)
	}
	size, err := getFileSize(filePath)
	if err != nil {
		return 0, fmt.Errorf("读取文件失败: %w", err)
	}
	if size > MaxUploadFileSize() {
		return 0, fmt.Errorf("文件过大: %d 字节, 上限 %d MB", size, MaxUploadFileSize()>>20)
	}
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(filePath), ext)
	}
	doc := &model.KBDocument{
		Title:    title,
		FilePath: filePath,
		FileSize: size,
		FileType: ext,
		Status:   model.KBDocumentStatusPending,
	}
	if err := s.docs.Create(ctx, doc); err != nil {
		return 0, fmt.Errorf("保存文档记录失败: %w", err)
	}
	async.RunWithTimeout(ctx, AsyncProcessingTimeout(), func(procCtx context.Context) {
		s.processDocumentAsync(procCtx, doc.ID, filePath)
	})
	return doc.ID, nil
}

// ProcessDocumentByID 对已存在（content 直存列里）的文档跑同一套异步处理链路
// （分段→向量化→索引）。爬虫与 QA 候选发布使用——爬得页面不走磁盘文件，
// content 直接写 kb_documents.content，因此不能复用 processDocumentAsync 的
// os.ReadFile 路径，但复用同一套 processor/vector/indexer 装配。
func (s *KnowledgeBaseService) ProcessDocumentByID(ctx context.Context, documentID uint) {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("[knowledge-base] ProcessDocumentByID panic doc=%d: %v", documentID, r)
			s.markDocumentFailed(ctx, documentID, fmt.Sprintf("处理异常: %v", r))
		}
	}()
	doc, err := s.docs.GetByID(ctx, documentID)
	if err != nil {
		logger.Errorf("[KBImport] ProcessDocumentByID 读文档失败 doc=%d: %v", documentID, err)
		return
	}
	s.processContent(ctx, doc, doc.Content)
}

// processContent content → 分片 → 向量化 → 入索引 → 状态推进
// （与 processDocumentAsync 的读文件后半段共用同一套 processor/vector/indexer）。
func (s *KnowledgeBaseService) processContent(ctx context.Context, doc *model.KBDocument, content string) {
	documentID := doc.ID
	if err := s.docs.UpdateStatusFields(ctx, documentID, map[string]any{
		"status": model.KBDocumentStatusProcessing, "error_msg": "",
	}); err != nil {
		// 状态没推进就继续跑没有任何意义：文档会一直停在 pending，重试也无从下手。
		logger.Errorf("[KBImport] 标记文档处理中失败 doc=%d: %v", documentID, err)
	}

	ragDoc := rag_core.Document{
		ID:      fmt.Sprintf("kbdoc_%d", documentID),
		Content: content,
		Metadata: map[string]any{
			"document_id": documentID,
			"title":       doc.Title,
		},
		CreatedAt: time.Now(),
	}

	chunks, err := s.processor.ProcessDocument(ctx, ragDoc)
	if err != nil {
		s.markDocumentFailed(ctx, documentID, fmt.Sprintf("分片失败: %v", err))
		return
	}

	if len(chunks) == 0 {
		s.markDocumentFailed(ctx, documentID, "文档分片结果为空")
		return
	}

	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
	}
	embeddings, err := s.vector.EmbedBatch(texts)
	if err != nil {
		s.markDocumentFailed(ctx, documentID, fmt.Sprintf("向量化失败: %v", err))
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
	if err := s.indexer.BuildIndex(ctx, kbIndexID, idxChunks); err != nil {
		s.markDocumentFailed(ctx, documentID, fmt.Sprintf("构建索引失败: %v", err))
		return
	}

	if err := s.docs.UpdateStatusFields(ctx, documentID, map[string]any{
		"status":      model.KBDocumentStatusIndexed,
		"chunk_count": len(chunks),
		"content":     content,
		"error_msg":   "",
	}); err != nil {
		// 索引已建成但状态写失败：文档将滞留 processing，前端无法感知——必须落日志供运维介入
		logger.Errorf("[KBImport] 标记文档已索引失败 doc=%d: %v", documentID, err)
	}
}

func (s *KnowledgeBaseService) processDocumentAsync(ctx context.Context, documentID uint, filePath string) {
	s.processDocumentAsyncWithQA(ctx, documentID, filePath, false)
}

// processDocumentAsyncWithQA 处理流水线 + 可选 QA 拆分投递（P1 观察项②：
// 上传文档可选 QA 增强，与爬虫侧 T4 的 qa_split_enabled 同一套 QASplitService）。
func (s *KnowledgeBaseService) processDocumentAsyncWithQA(ctx context.Context, documentID uint, filePath string, qaSplit bool) {
	bgCtx := ctx
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("[knowledge-base] processDocumentAsync panic doc=%d: %v\n%s", documentID, r, debug.Stack())
			s.markDocumentFailed(bgCtx, documentID, fmt.Sprintf("处理异常: %v", r))
		}
	}()

	if err := s.docs.UpdateStatusFields(bgCtx, documentID, map[string]any{
		"status": model.KBDocumentStatusProcessing, "error_msg": "",
	}); err != nil {
		// 状态没推进就继续跑没有任何意义：文档会一直停在 pending，重试也无从下手。
		logger.Errorf("[KBImport] 标记文档处理中失败 doc=%d: %v", documentID, err)
	}

	contentBytes, err := os.ReadFile(filePath)
	if err != nil {
		s.markDocumentFailed(bgCtx, documentID, fmt.Sprintf("读取文件失败: %v", err))
		return
	}
	// G13：这条上传链路此前直接把原始字节当正文（PDF/docx 的二进制垃圾原样进索引，
	// 且永不失败）。与 knowledge_service_pipeline 同一口径：走 etl.ParseDocument
	// （纯文本直通内置、结构化文档可走外挂解析），提取失败按文档不可读 markFailed，
	// 只有纯文本类才允许退化为原始字节。
	content, parseErr := etl.ParseDocument(bgCtx, filepath.Base(filePath), contentBytes)
	if parseErr != nil {
		if !etl.IsPlainTextExt(filePath) {
			s.markDocumentFailed(bgCtx, documentID, fmt.Sprintf("文本提取失败（可能为扫描件或损坏文档）: %v", parseErr))
			return
		}
		logger.Warnf("[KBImport] 纯文本提取异常 doc=%d: %v，退化为原始内容", documentID, parseErr)
		content = string(contentBytes)
	}

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

	if err := s.docs.UpdateStatusFields(bgCtx, documentID, map[string]any{
		"status":      model.KBDocumentStatusIndexed,
		"chunk_count": len(chunks),
		"content":     content,
		"error_msg":   "",
	}); err != nil {
		// 索引已建成但状态写失败：文档将滞留 processing，前端无法感知——必须落日志供运维介入
		logger.Errorf("[KBImport] 标记文档已索引失败 doc=%d: %v", documentID, err)
	}

	// QA 增强是**附加**动作：拆分失败不影响文档已入库这一事实（拆分有自己的候选表与人工审）。
	if qaSplit {
		s.maybeEnqueueQASplit(bgCtx, documentID)
	}
}

// maybeEnqueueQASplit 投递 QA 拆分（未装配拆分服务时静默跳过并记日志）。
// 投递口单独成字段：单测用替身断言「开关打开才投递」，不必起 LLM。
func (s *KnowledgeBaseService) maybeEnqueueQASplit(ctx context.Context, documentID uint) {
	if s.qaSplit == nil && s.qaEnqueue == nil {
		logger.Warnf("[KBImport] 请求了 QA 增强但拆分服务未装配，跳过 doc=%d", documentID)
		return
	}
	if s.qaEnqueue != nil {
		s.qaEnqueue(documentID, 0)
		return
	}
	s.qaSplit.EnqueueDocument(documentID, 0)
}

// SetQASplitService 注入 QA 拆分服务（上传文档可选 QA 增强；crawler 侧同款）。
func (s *KnowledgeBaseService) SetQASplitService(qa *QASplitService) {
	s.qaSplit = qa
}

func (s *KnowledgeBaseService) markDocumentFailed(ctx context.Context, documentID uint, errMsg string) {
	if err := s.docs.UpdateStatusFields(ctx, documentID, map[string]any{
		"status":    model.KBDocumentStatusFailed,
		"error_msg": errMsg,
	}); err != nil {
		logger.Errorf("[KBImport] 标记文档失败状态失败 doc=%d (原错误: %s): %v", documentID, errMsg, err)
	}
}
