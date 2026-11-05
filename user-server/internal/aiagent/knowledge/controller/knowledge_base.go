package controller

import (
	"hivemtk-user/internal/aiagent/knowledge/service"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type KnowledgeBaseController struct {
	kbService *service.KnowledgeBaseService
}

func NewKnowledgeBaseController() *KnowledgeBaseController {
	return &KnowledgeBaseController{
		kbService: service.NewKnowledgeBaseService(),
	}
}

// SetQASplitService 注入 QA 拆分服务（装配层在创建 QASplitService 之后调用）。
func (ctrl *KnowledgeBaseController) SetQASplitService(qa *service.QASplitService) {
	ctrl.kbService.SetQASplitService(qa)
}

func (ctrl *KnowledgeBaseController) RegisterRoutes(router *gin.RouterGroup) {
	kb := router.Group("/rag")
	{
		kb.POST("/import", ctrl.ImportKnowledgeBase)
		kb.GET("/documents", ctrl.ListDocuments)
		kb.GET("/documents/:id", ctrl.GetDocument)
		kb.DELETE("/documents/:id", ctrl.DeleteDocument)
	}
}

// ImportKnowledgeBase 导入知识库文件:保存文件 + 入库(status=pending) + 异步分片/向量化/入索引
func (ctrl *KnowledgeBaseController) ImportKnowledgeBase(c *gin.Context) {
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		response.Error(c, http.StatusBadRequest, "Failed to get file: "+err.Error())
		return
	}
	defer func() { _ = file.Close() }()

	title := c.PostForm("title")
	// 可选 QA 增强（对标 FastGPT「上传文档可选 QA 增强」）：默认关，仅显式传
	// qa_split=true/1 时在文档入库后投递拆分任务。
	qaSplit := c.PostForm("qa_split") == "true" || c.PostForm("qa_split") == "1"

	result, err := ctrl.kbService.ImportDocumentWithOptions(c.Request.Context(), title, file, header,
		service.ImportOptions{QASplit: qaSplit})
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, result, "文件已接收,处理已启动")
}

// ListDocuments 列出知识库文档
func (ctrl *KnowledgeBaseController) ListDocuments(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	docs, total, err := ctrl.kbService.ListDocuments(c.Request.Context(), page, pageSize)
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

// GetDocument 获取文档详情
func (ctrl *KnowledgeBaseController) GetDocument(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "无效的文档ID")
		return
	}

	doc, err := ctrl.kbService.GetDocument(c.Request.Context(), uint(id))
	if err != nil {
		if isNotFoundError(err) {
			response.Error(c, http.StatusNotFound, "文档不存在")
			return
		}
		logger.Errorf("[knowledge] 获取文档失败 id=%d: %v", id, err)
		response.Error(c, http.StatusInternalServerError, "获取文档失败，请稍后重试")
		return
	}

	response.Success(c, doc, "获取成功")
}

// DeleteDocument 删除文档
func (ctrl *KnowledgeBaseController) DeleteDocument(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "无效的文档ID")
		return
	}

	if err := ctrl.kbService.DeleteDocument(c.Request.Context(), uint(id)); err != nil {
		if isNotFoundError(err) {
			response.Error(c, http.StatusNotFound, "文档不存在")
			return
		}
		logger.Errorf("[knowledge] 删除文档失败 id=%d: %v", id, err)
		response.Error(c, http.StatusInternalServerError, "删除文档失败，请稍后重试")
		return
	}

	response.Success(c, nil, "删除成功")
}
