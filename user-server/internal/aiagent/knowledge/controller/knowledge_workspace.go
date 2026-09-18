package controller

import (
	"encoding/json"
	"fmt"
	knowledgemodel "hivemtk-user/internal/aiagent/knowledge/model"
	knowledgerepo "hivemtk-user/internal/aiagent/knowledge/repository"
	knowledgesvc "hivemtk-user/internal/aiagent/knowledge/service"
	"hivemtk-user/internal/pkg/utils/response"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// KnowledgeWorkspaceController 知识库工作台统一控制器
// 涵盖:文档导入(上传/文本/URL)、文档管理、检索、OpenAPI 数据源、统计
type KnowledgeWorkspaceController struct {
	kbService      *knowledgesvc.KnowledgeService
	openapiService OpenAPISourcePort
	statsService   *knowledgesvc.KnowledgeStatisticsService
}

func NewKnowledgeWorkspaceController(openapiPort OpenAPISourcePort) *KnowledgeWorkspaceController {
	return &KnowledgeWorkspaceController{
		kbService:      knowledgesvc.NewKnowledgeService(),
		openapiService: openapiPort,
		statsService:   knowledgesvc.NewKnowledgeStatisticsService(),
	}
}

func (ctrl *KnowledgeWorkspaceController) RegisterConnectors(router *gin.RouterGroup, connectorCtrl interface {
	List(ctx *gin.Context)
	Get(ctx *gin.Context)
	Save(ctx *gin.Context)
	Test(ctx *gin.Context)
	Pull(ctx *gin.Context)
}) {
	kb := router.Group("/knowledge")
	kb.GET("/connectors", connectorCtrl.List)
	kb.GET("/connectors/:source", connectorCtrl.Get)
	kb.PUT("/connectors/:source", connectorCtrl.Save)
	kb.POST("/connectors/:source/test", connectorCtrl.Test)
	kb.POST("/connectors/:source/pull", connectorCtrl.Pull)
}

// RegisterRoutes 注册路由
func (ctrl *KnowledgeWorkspaceController) RegisterRoutes(router *gin.RouterGroup) {
	kb := router.Group("/knowledge")
	{
		kb.POST("/import/upload", ctrl.UploadImport)
		kb.POST("/import/text", ctrl.TextImport)
		kb.POST("/import/url", ctrl.URLImport)
		kb.POST("/import/document", ctrl.DocumentImport)

		kb.GET("/documents", ctrl.ListDocuments)
		kb.GET("/documents/:id", ctrl.GetDocument)
		kb.GET("/documents/:id/progress", ctrl.GetDocumentProgress)
		kb.GET("/documents/:id/chunks", ctrl.GetDocumentChunks)
		kb.PUT("/documents/:id", ctrl.UpdateDocument)
		kb.DELETE("/documents/:id", ctrl.DeleteDocument)
		kb.POST("/documents/:id/reindex", ctrl.ReindexDocument)

		kb.POST("/products/:product_id/rebuild-index", ctrl.RebuildProductIndex)
		kb.GET("/products/:product_id/overview", ctrl.GetProductOverview)

		kb.POST("/search", ctrl.Search)

		kb.GET("/import-logs", ctrl.ListImportLogs)

		kb.GET("/openapi/sources", ctrl.ListOpenAPISources)
		kb.POST("/openapi/sources", ctrl.CreateOpenAPISource)
		kb.GET("/openapi/sources/:id", ctrl.GetOpenAPISource)
		kb.PUT("/openapi/sources/:id", ctrl.UpdateOpenAPISource)
		kb.DELETE("/openapi/sources/:id", ctrl.DeleteOpenAPISource)
		kb.POST("/openapi/sources/:id/sync", ctrl.SyncOpenAPISource)
		kb.POST("/openapi/sources/:id/test", ctrl.TestOpenAPISource)
		kb.POST("/openapi/sources/:id/toggle", ctrl.ToggleOpenAPISource)

		kb.POST("/:id/upload", ctrl.UploadImportToKB)
		kb.POST("/:id/import/url", ctrl.URLImportToKB)
		kb.POST("/:id/import/notion", ctrl.NotionImportToKB)
		kb.POST("/:id/import/feishu", ctrl.FeishuImportToKB)
		kb.POST("/:id/import/dingtalk", ctrl.DingtalkImportToKB)
		kb.POST("/:id/import/crm", ctrl.CRMImportToKB)
		kb.GET("/document-types", ctrl.ListDocumentTypes)

		kb.GET("/stats/overview", ctrl.GetOverviewStats)
		kb.GET("/stats/documents", ctrl.GetDocumentStats)
		kb.GET("/stats/searches", ctrl.GetSearchStats)
		kb.GET("/stats/imports", ctrl.GetImportStats)
		kb.GET("/stats/openapi", ctrl.GetOpenAPIStats)
	}
}

// UploadImport 上传文件导入
func (ctrl *KnowledgeWorkspaceController) UploadImport(c *gin.Context) {
	productID := c.PostForm("product_id")
	if productID == "" {
		response.Error(c, http.StatusBadRequest, "product_id 不能为空")
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		response.Error(c, http.StatusBadRequest, "文件上传失败: "+err.Error())
		return
	}
	defer func() { _ = file.Close() }()

	req := &knowledgesvc.ImportRequest{
		ProductID:  productID,
		SourceType: knowledgemodel.SourceTypeUpload,
		Title:      c.PostForm("title"),
		Category:   c.PostForm("category"),
		Operator:   ctrl.getOperator(c),
		IP:         c.ClientIP(),
		UserAgent:  c.Request.UserAgent(),
		File:       file,
		FileHeader: header,
		BatchNo:    c.PostForm("batch_no"),
	}
	if tagsJSON := c.PostForm("tags"); tagsJSON != "" {
		_ = json.Unmarshal([]byte(tagsJSON), &req.Tags)
	}
	if metaJSON := c.PostForm("metadata"); metaJSON != "" {
		var meta map[string]any
		if err := json.Unmarshal([]byte(metaJSON), &meta); err == nil {
			req.Metadata = meta
		}
	}

	result, err := ctrl.kbService.Import(c.Request.Context(), req)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, result, "文件已接收,处理已启动")
}

// TextImport 文本导入
func (ctrl *KnowledgeWorkspaceController) TextImport(c *gin.Context) {
	var body struct {
		ProductID string         `json:"product_id" binding:"required"`
		Title     string         `json:"title"`
		Content   string         `json:"content" binding:"required"`
		Category  string         `json:"category"`
		Tags      []string       `json:"tags"`
		BatchNo   string         `json:"batch_no"`
		Metadata  map[string]any `json:"metadata"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}

	req := &knowledgesvc.ImportRequest{
		ProductID:  body.ProductID,
		SourceType: knowledgemodel.SourceTypeText,
		Title:      body.Title,
		Content:    body.Content,
		Category:   body.Category,
		Tags:       body.Tags,
		BatchNo:    body.BatchNo,
		Metadata:   body.Metadata,
		Operator:   ctrl.getOperator(c),
		IP:         c.ClientIP(),
		UserAgent:  c.Request.UserAgent(),
	}
	result, err := ctrl.kbService.Import(c.Request.Context(), req)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, result, "文本导入已启动")
}

// URLImport URL 导入
func (ctrl *KnowledgeWorkspaceController) URLImport(c *gin.Context) {
	var body struct {
		ProductID string         `json:"product_id" binding:"required"`
		URL       string         `json:"url" binding:"required"`
		Title     string         `json:"title"`
		Category  string         `json:"category"`
		Metadata  map[string]any `json:"metadata"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}

	req := &knowledgesvc.ImportRequest{
		ProductID:  body.ProductID,
		SourceType: knowledgemodel.SourceTypeURL,
		Title:      body.Title,
		SourceRef:  body.URL,
		Category:   body.Category,
		Metadata:   body.Metadata,
		Operator:   ctrl.getOperator(c),
		IP:         c.ClientIP(),
		UserAgent:  c.Request.UserAgent(),
	}
	result, err := ctrl.kbService.Import(c.Request.Context(), req)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, result, "URL 抓取已启动")
}

// DocumentImport 企业级文档导入 API
// 面向企业其他系统：直接以 JSON 推送文档内容（纯文本/Markdown/HTML 片段）+ 附加字段，
// 由知识库完成切片、向量化与入库。典型场景：把某客户订单信息、工单、合同等作为
// 带 metadata（如 order_id、customer_id、channel）的知识写入向量库，供智能体检索使用。
func (ctrl *KnowledgeWorkspaceController) DocumentImport(c *gin.Context) {
	var body struct {
		ProductID string         `json:"product_id" binding:"required"`
		Title     string         `json:"title" binding:"required"`
		Content   string         `json:"content" binding:"required"`
		Format    string         `json:"format"`
		Category  string         `json:"category"`
		Tags      []string       `json:"tags"`
		BatchNo   string         `json:"batch_no"`
		Metadata  map[string]any `json:"metadata"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}

	req := &knowledgesvc.ImportRequest{
		ProductID:  body.ProductID,
		SourceType: knowledgemodel.SourceTypeText,
		Title:      body.Title,
		Content:    body.Content,
		Category:   body.Category,
		Tags:       body.Tags,
		BatchNo:    body.BatchNo,
		Metadata:   body.Metadata,
		Operator:   ctrl.getOperator(c),
		IP:         c.ClientIP(),
		UserAgent:  c.Request.UserAgent(),
	}
	result, err := ctrl.kbService.Import(c.Request.Context(), req)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, result, "文档已接收，切片/向量化/入库处理已启动")
}

// ListDocuments 列出文档
func (ctrl *KnowledgeWorkspaceController) ListDocuments(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	productID := ctrl.resolveProductID(c.Query("product_id"))

	filter := knowledgerepo.ListFilter{
		ProductID:   productID,
		EmbedStatus: c.Query("embed_status"),
		SourceType:  c.Query("source_type"),
		Category:    c.Query("category"),
		Keyword:     c.Query("keyword"),
		Page:        page,
		PageSize:    pageSize,
	}
	docs, total, err := ctrl.kbService.List(c.Request.Context(), filter)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, gin.H{
		"items":     docs,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// GetDocument 获取文档
func (ctrl *KnowledgeWorkspaceController) GetDocument(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "无效的文档ID")
		return
	}
	productID := ctrl.resolveProductID(c.Query("product_id"))
	doc, err := ctrl.kbService.Get(c.Request.Context(), productID, int64(id))
	if err != nil {
		response.Error(c, http.StatusNotFound, err.Error())
		return
	}
	response.Success(c, doc, "获取成功")
}

// GetDocumentProgress 获取处理进度
func (ctrl *KnowledgeWorkspaceController) GetDocumentProgress(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "无效的文档ID")
		return
	}
	progress, err := ctrl.kbService.GetProgress(c.Request.Context(), id)
	if err != nil {
		response.Error(c, http.StatusNotFound, err.Error())
		return
	}
	response.Success(c, progress, "获取成功")
}

// GetDocumentChunks 获取分段
func (ctrl *KnowledgeWorkspaceController) GetDocumentChunks(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "无效的文档ID")
		return
	}
	chunks, err := ctrl.kbService.GetChunks(c.Request.Context(), id)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, gin.H{"items": chunks, "total": len(chunks)}, "获取成功")
}

// UpdateDocument 更新文档元信息
func (ctrl *KnowledgeWorkspaceController) UpdateDocument(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "无效的文档ID")
		return
	}
	var body struct {
		Title    string   `json:"title"`
		Category string   `json:"category"`
		Tags     []string `json:"tags"`
		Priority int      `json:"priority"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	doc, err := ctrl.kbService.Get(c.Request.Context(), "", int64(id))
	if err != nil {
		response.Error(c, http.StatusNotFound, err.Error())
		return
	}
	if body.Title != "" {
		doc.Title = body.Title
	}
	if body.Category != "" {
		doc.Category = body.Category
	}
	if body.Tags != nil {
		tagsJSON, _ := json.Marshal(body.Tags)
		doc.Tags = string(tagsJSON)
	}
	if body.Priority > 0 {
		doc.Priority = body.Priority
	}
	if err := ctrl.kbService.Update(c.Request.Context(), doc); err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, doc, "更新成功")
}

// DeleteDocument 删除文档
func (ctrl *KnowledgeWorkspaceController) DeleteDocument(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "无效的文档ID")
		return
	}
	productID := ctrl.resolveProductID(c.Query("product_id"))
	if err := ctrl.kbService.Delete(c.Request.Context(), productID, int64(id)); err != nil {
		if isNotFoundError(err) {
			response.Error(c, http.StatusNotFound, err.Error())
			return
		}
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, nil, "删除成功")
}

// ReindexDocument 重建单文档索引
func (ctrl *KnowledgeWorkspaceController) ReindexDocument(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "无效的文档ID")
		return
	}
	productID := ctrl.resolveProductID(c.Query("product_id"))
	if err := ctrl.kbService.Reindex(c.Request.Context(), productID, uint64(id)); err != nil {
		if isNotFoundError(err) {
			response.Error(c, http.StatusNotFound, err.Error())
			return
		}
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, nil, "重建任务已启动")
}

// RebuildProductIndex 重建产品级索引
//
// 知识库 product_id 现已统一为 RagProduct.ID(string UUID)，直接使用，无需哈希映射。
func (ctrl *KnowledgeWorkspaceController) RebuildProductIndex(c *gin.Context) {
	productIDStr := strings.TrimSpace(c.Param("product_id"))
	if productIDStr == "" {
		response.Error(c, http.StatusBadRequest, "产品ID不能为空")
		return
	}
	productID := productIDStr
	if err := ctrl.kbService.RebuildIndex(c.Request.Context(), productID); err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, nil, "重建任务已启动")
}

// GetProductOverview 获取产品级总览
//
// 知识库 product_id 现已统一为 string(UUID)，直接透传即可（不再做哈希映射）。
func (ctrl *KnowledgeWorkspaceController) GetProductOverview(c *gin.Context) {
	productIDStr := strings.TrimSpace(c.Param("product_id"))
	if productIDStr == "" {
		response.Error(c, http.StatusBadRequest, "产品ID不能为空")
		return
	}
	productID := productIDStr
	overview, err := ctrl.statsService.GetOverview(c.Request.Context(), productID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, overview, "获取成功")
}

// Search 检索知识库
// 兼容字段名:threshold / similarity_threshold, top_k / limit
func (ctrl *KnowledgeWorkspaceController) Search(c *gin.Context) {
	var body struct {
		ProductID string  `json:"product_id" binding:"required"`
		Query     string  `json:"query" binding:"required"`
		TopK      int     `json:"top_k"`
		Limit     int     `json:"limit"`
		Threshold float64 `json:"threshold"`
		SimThres  float64 `json:"similarity_threshold"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	if body.Threshold <= 0 {
		body.Threshold = body.SimThres
	}
	if body.TopK <= 0 {
		body.TopK = body.Limit
	}
	if body.TopK <= 0 {
		body.TopK = 5
	}
	if body.Threshold <= 0 {
		body.Threshold = 0.6
	}
	productID := body.ProductID
	start := time.Now()
	chunks, err := ctrl.kbService.Search(c.Request.Context(), productID, body.Query, body.TopK, body.Threshold)
	latencyMs := int(time.Since(start).Milliseconds())
	hit := 0
	if chunks != nil {
		hit = 1
	}
	_ = ctrl.statsService.LogSearch(c.Request.Context(), &knowledgemodel.KnowledgeSearchLog{
		ProductID:           productID,
		Query:               body.Query,
		TopK:                body.TopK,
		SimilarityThreshold: body.Threshold,
		ResultCount:         len(chunks),
		LatencyMs:           latencyMs,
		Hit:                 hit,
		Source:              "api",
	})
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, gin.H{
		"items":      chunks,
		"total":      len(chunks),
		"latency_ms": latencyMs,
	}, "检索成功")
}

// ListImportLogs 列出导入日志
func (ctrl *KnowledgeWorkspaceController) ListImportLogs(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	productID := ctrl.resolveProductID(c.Query("product_id"))

	docs, total, err := ctrl.kbService.ListImportLogs(c.Request.Context(), knowledgerepo.ImportLogListFilter{
		ProductID: productID,
		BatchNo:   c.Query("batch_no"),
		Page:      page,
		PageSize:  pageSize,
	})
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, gin.H{
		"items":     docs,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

func (ctrl *KnowledgeWorkspaceController) requireOpenAPIPort(c *gin.Context) bool {
	if ctrl.openapiService == nil {
		response.Error(c, http.StatusServiceUnavailable, "OpenAPI 数据源服务未装配")
		return false
	}
	return true
}

// ListOpenAPISources 列出 OpenAPI 数据源
func (ctrl *KnowledgeWorkspaceController) ListOpenAPISources(c *gin.Context) {
	if !ctrl.requireOpenAPIPort(c) {
		return
	}
	productID := ctrl.resolveProductID(c.Query("product_id"))
	sources, err := ctrl.openapiService.ListSources(c.Request.Context(), productID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, gin.H{"items": sources, "total": len(sources)}, "获取成功")
}

// CreateOpenAPISource 创建数据源
func (ctrl *KnowledgeWorkspaceController) CreateOpenAPISource(c *gin.Context) {
	if !ctrl.requireOpenAPIPort(c) {
		return
	}
	var src knowledgemodel.KnowledgeOpenAPISource
	if err := c.ShouldBindJSON(&src); err != nil {
		response.Error(c, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}

	if src.ProductID == "" {
		if pID := c.PostForm("product_id"); pID != "" {
			src.ProductID = pID
		}
	}
	if err := ctrl.openapiService.CreateSource(c.Request.Context(), &src); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, src, "创建成功")
}

// GetOpenAPISource 获取数据源
func (ctrl *KnowledgeWorkspaceController) GetOpenAPISource(c *gin.Context) {
	if !ctrl.requireOpenAPIPort(c) {
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "无效的数据源ID")
		return
	}
	productID := ctrl.resolveProductID(c.Query("product_id"))
	src, err := ctrl.openapiService.GetSource(c.Request.Context(), productID, int64(id))
	if err != nil {
		response.Error(c, http.StatusNotFound, err.Error())
		return
	}
	response.Success(c, src, "获取成功")
}

// UpdateOpenAPISource 更新数据源
func (ctrl *KnowledgeWorkspaceController) UpdateOpenAPISource(c *gin.Context) {
	if !ctrl.requireOpenAPIPort(c) {
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "无效的数据源ID")
		return
	}
	var src knowledgemodel.KnowledgeOpenAPISource
	if err := c.ShouldBindJSON(&src); err != nil {
		response.Error(c, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	src.ID = id

	if err := ctrl.openapiService.UpdateSource(c.Request.Context(), &src); err != nil {
		if isNotFoundError(err) {
			response.Error(c, http.StatusNotFound, err.Error())
			return
		}
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, src, "更新成功")
}

// DeleteOpenAPISource 删除数据源
func (ctrl *KnowledgeWorkspaceController) DeleteOpenAPISource(c *gin.Context) {
	if !ctrl.requireOpenAPIPort(c) {
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "无效的数据源ID")
		return
	}
	productID := ctrl.resolveProductID(c.Query("product_id"))
	if err := ctrl.openapiService.DeleteSource(c.Request.Context(), productID, int64(id)); err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, nil, "删除成功")
}

// SyncOpenAPISource 同步数据源
func (ctrl *KnowledgeWorkspaceController) SyncOpenAPISource(c *gin.Context) {
	if !ctrl.requireOpenAPIPort(c) {
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "无效的数据源ID")
		return
	}
	productID := ctrl.resolveProductID(c.Query("product_id"))
	result, err := ctrl.openapiService.SyncSource(c.Request.Context(), productID, int64(id))
	if err != nil {
		if isNotFoundError(err) {
			response.Error(c, http.StatusNotFound, err.Error())
			return
		}
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, result, "同步完成")
}

// TestOpenAPISource 测试连接
func (ctrl *KnowledgeWorkspaceController) TestOpenAPISource(c *gin.Context) {
	if !ctrl.requireOpenAPIPort(c) {
		return
	}
	var src knowledgemodel.KnowledgeOpenAPISource
	if err := c.ShouldBindJSON(&src); err != nil {
		response.Error(c, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	result, _ := ctrl.openapiService.TestConnection(c.Request.Context(), &src)
	response.Success(c, result, "测试完成")
}

// ToggleOpenAPISource 启用/禁用
func (ctrl *KnowledgeWorkspaceController) ToggleOpenAPISource(c *gin.Context) {
	if !ctrl.requireOpenAPIPort(c) {
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "无效的数据源ID")
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	_ = c.ShouldBindJSON(&body)
	productID := ctrl.resolveProductID(c.Query("product_id"))
	if err := ctrl.openapiService.ToggleEnabled(c.Request.Context(), productID, int64(id), body.Enabled); err != nil {
		if isNotFoundError(err) {
			response.Error(c, http.StatusNotFound, err.Error())
			return
		}
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, nil, "切换成功")
}

// GetOverviewStats 总览统计
func (ctrl *KnowledgeWorkspaceController) GetOverviewStats(c *gin.Context) {
	productID := ctrl.resolveProductID(c.Query("product_id"))
	overview, err := ctrl.statsService.GetOverview(c.Request.Context(), productID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, overview, "获取成功")
}

// GetDocumentStats 文档维度统计
func (ctrl *KnowledgeWorkspaceController) GetDocumentStats(c *gin.Context) {
	productID := ctrl.resolveProductID(c.Query("product_id"))
	days, _ := strconv.Atoi(c.DefaultQuery("days", "30"))
	if days <= 0 {
		days = 30
	}
	data, err := ctrl.statsService.GetDocumentStats(c.Request.Context(), productID, days)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, data, "获取成功")
}

// GetSearchStats 检索维度统计
func (ctrl *KnowledgeWorkspaceController) GetSearchStats(c *gin.Context) {
	productID := ctrl.resolveProductID(c.Query("product_id"))
	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))
	if days <= 0 {
		days = 7
	}
	data, err := ctrl.statsService.GetSearchStats(c.Request.Context(), productID, days)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, data, "获取成功")
}

// GetImportStats 导入维度统计
func (ctrl *KnowledgeWorkspaceController) GetImportStats(c *gin.Context) {
	productID := ctrl.resolveProductID(c.Query("product_id"))
	days, _ := strconv.Atoi(c.DefaultQuery("days", "30"))
	if days <= 0 {
		days = 30
	}
	data, err := ctrl.statsService.GetImportStats(c.Request.Context(), productID, days)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, data, "获取成功")
}

// GetOpenAPIStats OpenAPI 同步统计
func (ctrl *KnowledgeWorkspaceController) GetOpenAPIStats(c *gin.Context) {
	productID := ctrl.resolveProductID(c.Query("product_id"))
	data, err := ctrl.statsService.GetOpenAPIStats(c.Request.Context(), productID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, data, "获取成功")
}

func (ctrl *KnowledgeWorkspaceController) getOperator(c *gin.Context) string {
	if v, ok := c.Get("user_id"); ok {
		return fmt.Sprintf("%v", v)
	}
	if v, ok := c.Get("username"); ok {
		return fmt.Sprintf("%v", v)
	}
	return "anonymous"
}

func (ctrl *KnowledgeWorkspaceController) resolveProductID(raw string) string {
	return raw
}

// UploadImportToKB POST /api/knowledge/:id/upload
func (ctrl *KnowledgeWorkspaceController) UploadImportToKB(c *gin.Context) {
	productID := c.Param("id")
	if productID == "" {
		response.Error(c, http.StatusBadRequest, "KB ID 不能为空")
		return
	}
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		response.Error(c, http.StatusBadRequest, "文件上传失败: "+err.Error())
		return
	}
	defer func() { _ = file.Close() }()

	title := c.PostForm("title")
	if title == "" {
		title = header.Filename
	}
	req := &knowledgesvc.ImportRequest{
		ProductID:  productID,
		SourceType: knowledgemodel.SourceTypeUpload,
		Title:      title,
		Category:   c.PostForm("type"),
		Operator:   ctrl.getOperator(c),
		IP:         c.ClientIP(),
		UserAgent:  c.Request.UserAgent(),
		File:       file,
		FileHeader: header,
	}
	if metaJSON := c.PostForm("metadata"); metaJSON != "" {
		var meta map[string]any
		if err := json.Unmarshal([]byte(metaJSON), &meta); err == nil {
			req.Metadata = meta
		}
	}
	result, err := ctrl.kbService.Import(c.Request.Context(), req)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, result, "文件已接收,处理已启动")
}

func (ctrl *KnowledgeWorkspaceController) connectorImportToKB(c *gin.Context, source string) {
	productID := c.Param("id")
	if productID == "" {
		response.Error(c, http.StatusBadRequest, "KB ID 不能为空")
		return
	}
	var body struct {
		URL      string         `json:"url"`
		Title    string         `json:"title"`
		Content  string         `json:"content"`
		Category string         `json:"category"`
		Metadata map[string]any `json:"metadata"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	if body.Metadata == nil {
		body.Metadata = map[string]any{}
	}
	body.Metadata["connector"] = source

	req := &knowledgesvc.ImportRequest{
		ProductID: productID,
		Title:     body.Title,
		Category:  body.Category,
		Metadata:  body.Metadata,
		Operator:  ctrl.getOperator(c),
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
	}
	if body.URL != "" {
		req.SourceType = knowledgemodel.SourceTypeURL
		req.SourceRef = body.URL
		if req.Title == "" {
			req.Title = body.URL
		}
	} else if body.Content != "" {
		req.SourceType = knowledgemodel.SourceTypeText
		req.Content = body.Content
		if req.Title == "" {
			req.Title = source + " 导入"
		}
	} else {
		response.Error(c, http.StatusBadRequest, "需要 url 或 content 字段（"+source+" 连接器凭据化对接由外部导入任务承载）")
		return
	}
	result, err := ctrl.kbService.Import(c.Request.Context(), req)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, result, source+" 导入已启动")
}

// URLImportToKB POST /api/knowledge/:id/import/url
func (ctrl *KnowledgeWorkspaceController) URLImportToKB(c *gin.Context) {
	ctrl.connectorImportToKB(c, "url")
}

// NotionImportToKB POST /api/knowledge/:id/import/notion
func (ctrl *KnowledgeWorkspaceController) NotionImportToKB(c *gin.Context) {
	ctrl.connectorImportToKB(c, "notion")
}

// FeishuImportToKB POST /api/knowledge/:id/import/feishu
func (ctrl *KnowledgeWorkspaceController) FeishuImportToKB(c *gin.Context) {
	ctrl.connectorImportToKB(c, "feishu")
}

// DingtalkImportToKB POST /api/knowledge/:id/import/dingtalk
func (ctrl *KnowledgeWorkspaceController) DingtalkImportToKB(c *gin.Context) {
	ctrl.connectorImportToKB(c, "dingtalk")
}

// CRMImportToKB POST /api/knowledge/:id/import/crm
func (ctrl *KnowledgeWorkspaceController) CRMImportToKB(c *gin.Context) {
	ctrl.connectorImportToKB(c, "crm")
}

// ListDocumentTypes GET /api/knowledge/document-types — 支持的文档类型枚举
func (ctrl *KnowledgeWorkspaceController) ListDocumentTypes(c *gin.Context) {
	response.Success(c, gin.H{"list": []gin.H{
		{"type": "markdown", "name": "Markdown", "extensions": []string{".md", ".markdown"}},
		{"type": "pdf", "name": "PDF", "extensions": []string{".pdf"}},
		{"type": "docx", "name": "Word", "extensions": []string{".doc", ".docx"}},
		{"type": "html", "name": "HTML", "extensions": []string{".html", ".htm"}},
		{"type": "url", "name": "网页 URL", "extensions": []string{}},
		{"type": "text", "name": "纯文本", "extensions": []string{".txt"}},
		{"type": "notion", "name": "Notion", "extensions": []string{}},
		{"type": "feishu", "name": "飞书文档", "extensions": []string{}},
		{"type": "dingtalk", "name": "钉钉文档", "extensions": []string{}},
		{"type": "crm", "name": "CRM 数据", "extensions": []string{}},
	}, "total": 10}, "ok")
}
