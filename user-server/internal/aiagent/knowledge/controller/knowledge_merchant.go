package controller

import (
	"hivemtk-user/internal/aiagent/knowledge/service"
	"hivemtk-user/internal/pkg/utils/response"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// KnowledgeMerchantController 商户视角 RAG 增强控制器
// 对应审计：商户自部署场景的可视化管理 + 外部系统接入 + 检索调试
//
// 包含 6 类接口：
//  1. 批量导入（CSV/JSON）
//  2. 检索 Playground
//  3. 分段编辑（增/改/删/拆）
//  4. 反馈标注
//  5. API Token 管理
//  6. 外部系统接入（飞书/Notion/通用 JSON）
type KnowledgeMerchantController struct {
	svc *service.KnowledgeMerchantService
}

// NewKnowledgeMerchantController 创建控制器
func NewKnowledgeMerchantController() *KnowledgeMerchantController {
	return &KnowledgeMerchantController{
		svc: service.NewKnowledgeMerchantService(),
	}
}

// RegisterRoutes 注册路由
func (ctrl *KnowledgeMerchantController) RegisterRoutes(router *gin.RouterGroup) {
	g := router.Group("/knowledge-merchant")
	{
		g.POST("/batch/import", ctrl.BatchImport)
		g.POST("/batch/upload", ctrl.BatchUpload)

		g.POST("/playground", ctrl.Playground)

		g.GET("/documents/:id/chunks", ctrl.ListDocumentChunks)
		g.PUT("/chunks/:id", ctrl.UpdateChunk)
		g.DELETE("/chunks/:id", ctrl.DeleteChunk)
		g.POST("/chunks/:id/split", ctrl.SplitChunk)

		g.POST("/feedback", ctrl.SubmitFeedback)
		g.GET("/feedbacks", ctrl.ListFeedbacks)

		g.POST("/tokens", ctrl.CreateToken)
		g.GET("/tokens", ctrl.ListTokens)
		g.POST("/tokens/:id/revoke", ctrl.RevokeToken)

		g.GET("/external/jobs", ctrl.ListExternalJobs)
	}
}

// BatchImport JSON 体导入
func (ctrl *KnowledgeMerchantController) BatchImport(c *gin.Context) {
	var req service.BatchImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	if req.Operator == "" {
		req.Operator = c.GetString("operator")
	}
	result, err := ctrl.svc.BatchImport(c.Request.Context(), &req)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, result, "批量导入完成")
}

// BatchUpload 文件上传批量导入
func (ctrl *KnowledgeMerchantController) BatchUpload(c *gin.Context) {
	productID := c.PostForm("product_id")
	if productID == "" {
		response.Error(c, http.StatusBadRequest, "product_id 不能为空")
		return
	}
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		response.Error(c, http.StatusBadRequest, "文件错误: "+err.Error())
		return
	}
	defer func() { _ = file.Close() }()
	req := &service.BatchImportRequest{
		ProductID: productID,
		Operator:  c.PostForm("operator"),
		Format:    c.PostForm("format"),
		File:      file,
		FileHead:  header,
	}
	result, err := ctrl.svc.BatchImport(c.Request.Context(), req)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, result, "批量导入完成")
}

func (ctrl *KnowledgeMerchantController) Playground(c *gin.Context) {
	var req service.PlaygroundRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	result, err := ctrl.svc.Playground(c.Request.Context(), &req)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, result, "")
}

func (ctrl *KnowledgeMerchantController) ListDocumentChunks(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "无效的文档ID")
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	chunks, total, err := ctrl.svc.GetDocumentChunks(c.Request.Context(), id, page, pageSize, c.GetHeader("X-Knowledge-Token"))
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, gin.H{"items": chunks, "total": total, "page": page, "page_size": pageSize}, "")
}

func (ctrl *KnowledgeMerchantController) UpdateChunk(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "无效的分段ID")
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	if err := ctrl.svc.UpdateChunk(c.Request.Context(), &service.UpdateChunkRequest{ChunkID: id, Content: body.Content, Token: c.GetHeader("X-Knowledge-Token")}); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, nil, "更新成功")
}

func (ctrl *KnowledgeMerchantController) DeleteChunk(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "无效的分段ID")
		return
	}
	if err := ctrl.svc.DeleteChunk(c.Request.Context(), id, c.GetHeader("X-Knowledge-Token")); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, nil, "删除成功")
}

func (ctrl *KnowledgeMerchantController) SplitChunk(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "无效的分段ID")
		return
	}
	var req service.SplitChunkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	req.ChunkID = id
	if err := ctrl.svc.SplitChunk(c.Request.Context(), &req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, nil, "拆分成功")
}

func (ctrl *KnowledgeMerchantController) SubmitFeedback(c *gin.Context) {
	var req service.SubmitFeedbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	if req.Operator == "" {
		req.Operator = c.GetString("operator")
	}
	if err := ctrl.svc.SubmitFeedback(c.Request.Context(), &req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, nil, "反馈已记录")
}

func (ctrl *KnowledgeMerchantController) ListFeedbacks(c *gin.Context) {
	productID := c.Query("product_id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	rating, _ := strconv.Atoi(c.DefaultQuery("rating", "0"))
	filter := 999
	if rating >= -1 && rating <= 1 {
		filter = rating
	}
	list, total, err := ctrl.svc.ListFeedbacks(c.Request.Context(), &service.ListFeedbacksRequest{
		ProductID: productID,
		Page:      page,
		PageSize:  pageSize,
		Rating:    filter,
	})
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, gin.H{"items": list, "total": total, "page": page, "page_size": pageSize}, "")
}

func (ctrl *KnowledgeMerchantController) CreateToken(c *gin.Context) {
	var req service.CreateTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	if req.CreatedBy == "" {
		req.CreatedBy = c.GetString("operator")
	}
	tok, err := ctrl.svc.CreateToken(c.Request.Context(), &req)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, tok, "Token 已创建，请妥善保存明文（仅显示一次）")
}

func (ctrl *KnowledgeMerchantController) ListTokens(c *gin.Context) {
	productID := c.Query("product_id")
	list, err := ctrl.svc.ListTokens(c.Request.Context(), productID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, gin.H{"items": list}, "")
}

func (ctrl *KnowledgeMerchantController) RevokeToken(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "无效的 Token ID")
		return
	}
	if err := ctrl.svc.RevokeToken(c.Request.Context(), id); err != nil {
		response.ErrorFromDB(c, err, "吊销Token失败")
		return
	}
	response.Success(c, nil, "已吊销")
}

func (ctrl *KnowledgeMerchantController) ExternalImport(c *gin.Context) {
	var req service.ExternalImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "请求参数错误")
		return
	}
	req.Token = c.GetHeader("X-Knowledge-Token")
	if req.Token == "" {
		req.Token = c.Query("token")
	}
	if req.Operator == "" {
		req.Operator = c.GetString("operator")
	}
	if req.Source == "" {
		response.Error(c, http.StatusBadRequest, "source 不能为空")
		return
	}
	if req.ProductID == "" {
		response.Error(c, http.StatusBadRequest, "product_id 不能为空")
		return
	}
	resp, err := ctrl.svc.ExternalImport(c.Request.Context(), &req)
	if err != nil {
		response.ErrorFromDB(c, err, "外部系统导入失败")
		return
	}
	response.Success(c, resp, "")
}

func (ctrl *KnowledgeMerchantController) ListExternalJobs(c *gin.Context) {
	productID := c.Query("product_id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	list, total, err := ctrl.svc.ListExternalJobs(c.Request.Context(), productID, page, pageSize)
	if err != nil {
		response.ErrorFromDB(c, err, "获取外部导入任务列表失败")
		return
	}
	response.Success(c, gin.H{"items": list, "total": total, "page": page, "page_size": pageSize}, "")
}
