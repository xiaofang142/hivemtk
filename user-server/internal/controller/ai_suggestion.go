package controller

import (
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// AISuggestionController AI建议控制器
type AISuggestionController struct {
	suggestionService *service.AISuggestionService
}

// NewAISuggestionController 创建AI建议控制器实例
func NewAISuggestionController() *AISuggestionController {
	return &AISuggestionController{
		suggestionService: service.NewAISuggestionService(),
	}
}

// GetSuggestions 获取AI建议
func (c *AISuggestionController) GetSuggestions(ctx *gin.Context) {
	sessionID := ctx.Param("session_id")
	suggestions, err := c.suggestionService.GetSuggestions(ctx.Request.Context(), sessionID)
	if HandleDBError(ctx, err, "获取AI建议") {
		return
	}

	response.Success(ctx, suggestions, "获取成功")
}

// UseSuggestion 使用AI建议
func (c *AISuggestionController) UseSuggestion(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的建议ID")
		return
	}

	agentID := getUserIDFromContext(ctx)

	if err := c.suggestionService.UseSuggestion(ctx.Request.Context(), uint(id), agentID); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(ctx, nil, "使用成功")
}
