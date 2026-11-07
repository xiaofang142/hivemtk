package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/aiagent/knowledge/service"
	"hivemtk-user/internal/pkg/kbrelease"
	"hivemtk-user/internal/pkg/utils/response"
)

// operatorName 取当前操作者名称，用于 created_by / operator 审计字段。
// JWT 中间件写入的 key 是 username / user_id，而历史代码读的是 "operator"（全仓无人 Set 过），
// 导致 Token 的「创建人」列与反馈的「操作员」列恒为空。这里按 username → operator → user#id 兜底。
func operatorName(c *gin.Context) string {
	if v := strings.TrimSpace(c.GetString("username")); v != "" {
		return v
	}
	if v := strings.TrimSpace(c.GetString("operator")); v != "" {
		return v
	}
	if uid := c.GetUint("user_id"); uid > 0 {
		return fmt.Sprintf("user#%d", uid)
	}
	return ""
}

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
		req.Operator = operatorName(c)
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
		// 面向界面文案：直接说该做什么，而不是回字段名（2026-10-31 逐页点验）
		response.Error(c, http.StatusBadRequest, "请选择目标知识库（产品）后再上传导入")
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

// replyChunkWriteError 三个"就地改语料"写入口（改 / 删 / 拆）共用的错误出口。
//
// 只为本处新增的那一条判据开一个 409 分支，其余错误沿用本控制器既有的 400 口径。
// 这两码在这里不是粗细之别而是**下一步动作之别**：400 是"请求体写错了，改完再点"，
// 409 是"这个库已进发布制，这条路根本不提供，请改提一条变更走审批"。
// 把后者一起报成 400，运营会反复改正文再点保存，而改多少次结果都一样。
func replyChunkWriteError(c *gin.Context, err error) {
	if errors.Is(err, kbrelease.ErrGovernedDirectWrite) {
		response.Error(c, http.StatusConflict, err.Error())
		return
	}
	// D-30：分段不存在要回 404。原先一律 400 INVALID_PARAM_1001，DELETE 路径还会被
	// 内部错误掩码成「操作失败，请稍后重试」（gorm 的 "record not found" 命中泄露标记），
	// 调用方既分不清是参数错还是目标已经没了，也拿不到可行动的提示。
	if isNotFoundError(err) {
		response.Error(c, http.StatusNotFound, "分段不存在")
		return
	}
	response.Error(c, http.StatusBadRequest, err.Error())
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
		replyChunkWriteError(c, err)
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
		replyChunkWriteError(c, err)
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
		replyChunkWriteError(c, err)
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
		req.Operator = operatorName(c)
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
	// 默认 999 表示「不按评价过滤」；此前默认 "0" 会把不带 rating 的请求当成“只查一般”。
	rating, _ := strconv.Atoi(c.DefaultQuery("rating", "999"))
	keyword := c.Query("keyword")
	filter := 999
	if rating >= -1 && rating <= 1 {
		filter = rating
	}
	list, total, err := ctrl.svc.ListFeedbacks(c.Request.Context(), &service.ListFeedbacksRequest{
		ProductID: productID,
		Page:      page,
		PageSize:  pageSize,
		Rating:    filter,
		Keyword:   keyword,
	})
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	stats, err := ctrl.svc.FeedbackStats(c.Request.Context(), productID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, gin.H{
		"items": list, "total": total, "page": page, "page_size": pageSize,
		"stats": gin.H{"total": stats.Total, "good": stats.Good, "neutral": stats.Neutral, "bad": stats.Bad},
	}, "")
}

func (ctrl *KnowledgeMerchantController) CreateToken(c *gin.Context) {
	var req service.CreateTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	if req.CreatedBy == "" {
		req.CreatedBy = operatorName(c)
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
		req.Operator = operatorName(c)
	}
	// source / product_id 的必填校验放在 service 里，且必须在 token 鉴权之后执行，
	// 否则公开入口会在认证前先回 400（D-32）。
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
