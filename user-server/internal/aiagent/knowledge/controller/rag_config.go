package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"hivemtk-user/internal/aiagent/knowledge/model"
	"hivemtk-user/internal/aiagent/knowledge/service"
	sysmodel "hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

type RagConfigController struct {
	service *service.RagConfigService
}

func NewRagConfigController(service *service.RagConfigService) *RagConfigController {
	return &RagConfigController{service: service}
}

func (ctrl *RagConfigController) RegisterRoutes(router *gin.RouterGroup) {
	rag := router.Group("/rag-config")
	{
		rag.POST("/products", ctrl.CreateRagProduct)
		rag.GET("/products", ctrl.ListRagProducts)
		rag.GET("/products/:id", ctrl.GetRagProduct)
		rag.PUT("/products/:id", ctrl.UpdateRagProduct)
		rag.DELETE("/products/:id", ctrl.DeleteRagProduct)

		rag.GET("/accounts/config", ctrl.GetAccountConfig)
		rag.PUT("/accounts/config", ctrl.UpdateAccountConfig)

		rag.POST("/process-message", ctrl.ProcessMessage)

		rag.POST("/query", ctrl.QueryRAG)
	}
}

func (ctrl *RagConfigController) CreateRagProduct(c *gin.Context) {
	var req model.RagProduct
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	product, err := ctrl.service.CreateRagProduct(c.Request.Context(), &req)
	if err != nil {
		if isNotFoundError(err) {
			response.Error(c, http.StatusNotFound, err.Error())
			return
		}
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, product, "RAG product created successfully")
}

func (ctrl *RagConfigController) ListRagProducts(c *gin.Context) {
	pageStr := c.DefaultQuery("page", "1")
	page, err := strconv.Atoi(pageStr)
	if err != nil {
		page = 1
	}

	pageSizeStr := c.DefaultQuery("page_size", "20")
	pageSize, err := strconv.Atoi(pageSizeStr)
	if err != nil {
		pageSize = 20
	}

	products, err := ctrl.service.ListRagProducts(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	start := (page - 1) * pageSize
	end := start + pageSize
	if start > len(products) {
		start = len(products)
	}
	if end > len(products) {
		end = len(products)
	}

	pagedProducts := products[start:end]

	response.SuccessWithPage(c, pagedProducts, int64(page), int64(pageSize), int64(len(products)))
}

func (ctrl *RagConfigController) GetRagProduct(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		response.Error(c, http.StatusBadRequest, "Product ID is required")
		return
	}

	product, err := ctrl.service.GetRagProduct(c.Request.Context(), id)
	if err != nil {
		if isNotFoundError(err) {
			response.Error(c, http.StatusNotFound, err.Error())
			return
		}
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	if product == nil {
		response.NotFound(c, "Product not found")
		return
	}

	response.Success(c, product, "Product retrieved successfully")
}

func (ctrl *RagConfigController) UpdateRagProduct(c *gin.Context) {
	id := c.Param("id")

	var req model.RagProduct
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	req.ID = id

	err := ctrl.service.UpdateRagProduct(c.Request.Context(), &req)
	if err != nil {
		if isNotFoundError(err) {
			response.Error(c, http.StatusNotFound, err.Error())
			return
		}
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, nil, "RAG product updated successfully")
}

func (ctrl *RagConfigController) DeleteRagProduct(c *gin.Context) {
	id := c.Param("id")

	err := ctrl.service.DeleteRagProduct(c.Request.Context(), id)
	if err != nil {
		// 0 行不等于服务故障：删一条已经没了的记录要回 404（与 A2/A6 同口径），
		// 修前这里一律 500，且把 "record not found" 原文漏给前端。
		if isNotFoundError(err) {
			response.Error(c, http.StatusNotFound, "RAG 产品不存在")
			return
		}
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, nil, "RAG product deleted successfully")
}

func (ctrl *RagConfigController) GetAccountConfig(c *gin.Context) {
	accountID := c.Query("account_id")
	platform := c.Query("platform")

	if accountID == "" || platform == "" {
		response.Success(c, nil, "No account config found")
		return
	}

	config, err := ctrl.service.GetAccountConfig(c.Request.Context(), accountID, platform)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, config, "Account config retrieved successfully")
}

func (ctrl *RagConfigController) UpdateAccountConfig(c *gin.Context) {
	var req sysmodel.PlatformAccountConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	err := ctrl.service.UpdateAccountConfig(c.Request.Context(), &req)
	if err != nil {
		if isNotFoundError(err) {
			response.Error(c, http.StatusNotFound, err.Error())
			return
		}
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, nil, "Configuration updated successfully")
}

func (ctrl *RagConfigController) ProcessMessage(c *gin.Context) {
	var req struct {
		Platform  string `json:"platform" binding:"required"`
		AccountID string `json:"account_id" binding:"required"`
		Message   string `json:"message" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	reply, err := ctrl.service.ProcessMessage(c.Request.Context(), req.Platform, req.AccountID, req.Message)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, gin.H{"reply": reply}, "Message processed successfully")
}

// RAGQueryRequest RAG查询请求
type RAGQueryRequest struct {
	Query        string         `json:"query" binding:"required"`
	RAGProductID string         `json:"rag_product_id" binding:"required"`
	Context      map[string]any `json:"context,omitempty"`
}

// RAGQueryResponse RAG查询响应
type RAGQueryResponse struct {
	Answer     string         `json:"answer"`
	References []any          `json:"references,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

func (ctrl *RagConfigController) QueryRAG(c *gin.Context) {
	var req RAGQueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	if req.Query == "" {
		response.Error(c, http.StatusBadRequest, "query 不能为空")
		return
	}

	if req.RAGProductID == "" {
		response.Error(c, http.StatusBadRequest, "rag_product_id 不能为空")
		return
	}

	product, err := ctrl.service.GetRagProduct(c.Request.Context(), req.RAGProductID)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "Failed to get RAG product: "+err.Error())
		return
	}

	if product == nil {
		response.Error(c, http.StatusNotFound, "RAG product not found")
		return
	}

	topK := 5
	if v, ok := req.Context["top_k"]; ok {
		switch n := v.(type) {
		case float64:
			if int(n) > 0 {
				topK = int(n)
			}
		case int:
			if n > 0 {
				topK = n
			}
		}
	}

	queryResp, err := ctrl.service.QueryKnowledgeBase(c.Request.Context(), req.RAGProductID, req.Query, topK)
	if err != nil {
		ctrl.respondWithFallback(c, product, req)
		return
	}

	references := make([]any, 0, len(queryResp.References))
	for _, src := range queryResp.References {
		references = append(references, map[string]any{
			"document_id": src.DocumentID,
			"chunk_id":    src.ID,
			"content":     src.Content,
			"score":       src.Score,
		})
	}

	metadata := map[string]any{
		"product_name": product.Name,
		"product_id":   req.RAGProductID,
		"query":        req.Query,
		"timestamp":    time.Now().Unix(),
		"top_k":        topK,
		"source_count": len(references),
	}

	queryResponse := RAGQueryResponse{
		Answer:     queryResp.Answer,
		References: references,
		Metadata:   metadata,
	}

	response.Success(c, queryResponse, "RAG query completed successfully")
}

func (ctrl *RagConfigController) respondWithFallback(c *gin.Context, product *model.RagProduct, req RAGQueryRequest) {
	answer := product.SystemPrompt
	if answer == "" {
		answer = product.Description
	}
	if answer == "" {
		answer = fmt.Sprintf("已收到查询: %s。该 RAG 产品 [%s] 当前无可用知识库,后续将为该产品补充文档后即可获得完整回复。", req.Query, product.Name)
	}
	queryResponse := RAGQueryResponse{
		Answer:     answer,
		References: []any{},
		Metadata: map[string]any{
			"product_name": product.Name,
			"product_id":   req.RAGProductID,
			"query":        req.Query,
			"timestamp":    time.Now().Unix(),
			"fallback":     true,
		},
	}
	response.Success(c, queryResponse, "RAG query completed with fallback")
}
