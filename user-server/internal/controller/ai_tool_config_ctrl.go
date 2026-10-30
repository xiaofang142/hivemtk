package controller

import (
	"errors"
	"net/http"
	"strconv"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// AIToolConfigController AI工具配置控制器
type AIToolConfigController struct {
	service *service.AIToolConfigService
}

// NewAIToolConfigController 创建AI工具配置控制器
func NewAIToolConfigController(service *service.AIToolConfigService) *AIToolConfigController {
	return &AIToolConfigController{service: service}
}

// @Summary      获取工具列表
// @Tags      a-i-tool-config
// @Router    /api/ai-tools [get]
// ListTools 获取工具列表
// GET /api/ai-tools?category=reach&enabled=true&page=1&page_size=20
func (c *AIToolConfigController) ListTools(ctx *gin.Context) {
	category := ctx.Query("category")
	enabledStr := ctx.Query("enabled")
	pageStr := ctx.DefaultQuery("page", "1")
	pageSizeStr := ctx.DefaultQuery("page_size", "20")

	page, _ := strconv.Atoi(pageStr)
	pageSize, _ := strconv.Atoi(pageSizeStr)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	var enabled *bool
	if enabledStr != "" {
		e := enabledStr == "true"
		enabled = &e
	}

	result, err := c.service.ListTools(ctx.Request.Context(), category, enabled, page, pageSize)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "获取工具列表失败: "+err.Error())
		return
	}
	response.Success(ctx, result, "ok")
}

// @Summary      获取工具详情
// @Tags      a-i-tool-config
// @Router    /api/ai-tools/{name} [get]
// GetTool 获取工具详情
// GET /api/ai-tools/:name
func (c *AIToolConfigController) GetTool(ctx *gin.Context) {
	name := ctx.Param("name")
	if name == "" {
		response.Error(ctx, http.StatusBadRequest, "工具名称不能为空")
		return
	}

	result, err := c.service.GetTool(ctx.Request.Context(), name)
	if err != nil {
		response.ErrorFromDB(ctx, err, "工具不存在")
		return
	}
	response.Success(ctx, result, "ok")
}

// @Summary      更新工具状态
// @Tags      a-i-tool-config
// @Router    /api/ai-tools/{name}/status [put]
// UpdateToolStatus 更新工具状态
// PUT /api/ai-tools/:name/status
func (c *AIToolConfigController) UpdateToolStatus(ctx *gin.Context) {
	name := ctx.Param("name")
	if name == "" {
		response.Error(ctx, http.StatusBadRequest, "工具名称不能为空")
		return
	}

	// is_enabled 必须显式给出：bool 零值会把「漏传字段」当成「关掉工具」，
	// 一个空 body 就能静默停用工具。
	var req struct {
		IsEnabled *bool `json:"is_enabled"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误")
		return
	}
	if req.IsEnabled == nil {
		response.Error(ctx, http.StatusBadRequest, "is_enabled 不能为空")
		return
	}

	if err := c.service.UpdateStatus(ctx.Request.Context(), name, *req.IsEnabled); err != nil {
		response.ErrorFromDB(ctx, err, "工具不存在")
		return
	}
	response.Success(ctx, nil, "success")
}

// @Summary      批量更新工具状态
// @Tags      a-i-tool-config
// @Router    /api/ai-tools/batch-status [post]
// BatchUpdateStatus 批量更新工具状态
// POST /api/ai-tools/batch-status
func (c *AIToolConfigController) BatchUpdateStatus(ctx *gin.Context) {
	var req struct {
		Tools     []string `json:"tools"`
		IsEnabled *bool    `json:"is_enabled"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误")
		return
	}

	if len(req.Tools) == 0 {
		response.Error(ctx, http.StatusBadRequest, "工具列表不能为空")
		return
	}
	if req.IsEnabled == nil {
		response.Error(ctx, http.StatusBadRequest, "is_enabled 不能为空")
		return
	}

	if err := c.service.BatchUpdateStatus(ctx.Request.Context(), req.Tools, *req.IsEnabled); err != nil {
		response.ErrorFromDB(ctx, err, "工具不存在")
		return
	}
	response.Success(ctx, nil, "success")
}

// @Summary      获取工具绑定的账号
// @Tags      a-i-tool-config
// @Router    /api/ai-tools/{name}/accounts [get]
// GetToolAccounts 获取工具绑定的账号
// GET /api/ai-tools/:name/accounts
func (c *AIToolConfigController) GetToolAccounts(ctx *gin.Context) {
	name := ctx.Param("name")
	if name == "" {
		response.Error(ctx, http.StatusBadRequest, "工具名称不能为空")
		return
	}

	result, err := c.service.GetToolAccounts(ctx.Request.Context(), name)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "获取绑定账号失败: "+err.Error())
		return
	}
	response.Success(ctx, result, "ok")
}

// @Summary      绑定账号到工具
// @Tags      a-i-tool-config
// @Router    /api/ai-tools/{name}/accounts [post]
// BindAccount 绑定账号到工具
// POST /api/ai-tools/:name/accounts
func (c *AIToolConfigController) BindAccount(ctx *gin.Context) {
	name := ctx.Param("name")
	if name == "" {
		response.Error(ctx, http.StatusBadRequest, "工具名称不能为空")
		return
	}

	var req struct {
		AccountType string `json:"account_type"`
		AccountID   string `json:"account_id"`
		IsPrimary   bool   `json:"is_primary"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误")
		return
	}

	if req.AccountType == "" || req.AccountID == "" {
		response.Error(ctx, http.StatusBadRequest, "账号类型和ID不能为空")
		return
	}

	if err := c.service.BindAccount(ctx.Request.Context(), name, req.AccountType, req.AccountID, req.IsPrimary); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.NotFound(ctx, "工具不存在")
			return
		}
		response.Error(ctx, http.StatusInternalServerError, "绑定失败: "+err.Error())
		return
	}
	response.Success(ctx, nil, "success")
}

// @Summary      解绑账号
// @Tags      a-i-tool-config
// @Router    /api/ai-tools/{name}/accounts/{account_type}/{account_id} [delete]
// UnbindAccount 解绑账号
// DELETE /api/ai-tools/:name/accounts/:account_type/:account_id
func (c *AIToolConfigController) UnbindAccount(ctx *gin.Context) {
	name := ctx.Param("name")
	accountType := ctx.Param("account_type")
	accountID := ctx.Param("account_id")

	if name == "" || accountType == "" || accountID == "" {
		response.Error(ctx, http.StatusBadRequest, "参数不完整")
		return
	}

	if err := c.service.UnbindAccount(ctx.Request.Context(), name, accountType, accountID); err != nil {
		response.ErrorFromDB(ctx, err, "绑定不存在")
		return
	}
	response.Success(ctx, nil, "success")
}
