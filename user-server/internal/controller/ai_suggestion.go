package controller

import (
	"errors"
	"fmt"

	"hivemtk-user/internal/pkg/errhttp"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// AISuggestionController AI建议控制器
type AISuggestionController struct {
	suggestionService *service.AISuggestionService
}

// NewAISuggestionController 创建AI建议控制器实例（service 由 router 注入）
func NewAISuggestionController(svc *service.AISuggestionService) *AISuggestionController {
	return &AISuggestionController{suggestionService: svc}
}

// @Summary      获取会话的 AI 建议（支持路径参数与 query 两种路由）
// @Tags      a-i-suggestion
// @Router    /api/customer-service/ai-suggestions [get]
// GetSuggestions 获取AI建议
//
// 同一 handler 挂两种路由：带路径参数的 `/ai-suggestions/:session_id`，
// 以及只带 query 的别名 `/ai-suggestions?session_id=...`
// （`frontend_aliases.go:216` / `:441`，e2e `deep_customer_sessions.sh:110` 用后者）。
// 只读 Param 时别名会拿空串查到空列表并回 200，看起来「没有建议」而不是「参数丢了」。
func (c *AISuggestionController) GetSuggestions(ctx *gin.Context) {
	sessionID := ctx.Param("session_id")
	if sessionID == "" {
		sessionID = ctx.Query("session_id")
	}
	if sessionID == "" {
		response.Error(ctx, http.StatusBadRequest, "session_id 不能为空")
		return
	}

	suggestions, err := c.suggestionService.GetSuggestions(ctx.Request.Context(), sessionID)
	if HandleDBError(ctx, err, "获取AI建议") {
		return
	}

	response.Success(ctx, suggestions, "获取成功")
}

// @Summary 使用AI建议
// @Tags      a-i-suggestion
// @Router    /api/ai-suggestions/{id}/use [post]
// UseSuggestion 使用AI建议
func (c *AISuggestionController) UseSuggestion(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的建议ID")
		return
	}

	agentID := getUserIDFromContext(ctx)
	if agentID == 0 {
		response.Error(ctx, http.StatusUnauthorized, "未登录")
		return
	}

	if err := c.suggestionService.UseSuggestion(ctx.Request.Context(), uint(id), agentID); err != nil {
		// 0 行 = id 不存在（或不属于本租户）。仓储已把它归一成 ErrRecordNotFound，
		// 这里只回「建议不存在（id=…）」，不要把 gorm 原文透给前端。
		if errors.Is(err, gorm.ErrRecordNotFound) || errhttp.IsNotFoundError(err) {
			response.NotFound(ctx, fmt.Sprintf("AI 建议不存在（id=%d）", id))
			return
		}
		HandleDBError(ctx, err, "标记 AI 建议已使用")
		return
	}

	response.Success(ctx, nil, "使用成功")
}
