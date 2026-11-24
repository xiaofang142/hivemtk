package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

// ConversationContextController 共享对话上下文只读入口（T054 交叉引用展示数据源）。
// 数据范围由 ConversationContextService 底层仓储按租户上下文强制过滤（缺租户 fail-closed）。
type ConversationContextController struct {
	svc *service.ConversationContextService
}

func NewConversationContextController(svc *service.ConversationContextService) *ConversationContextController {
	return &ConversationContextController{svc: svc}
}

func (c *ConversationContextController) Get(ctx *gin.Context) {
	accountID, err := strconv.ParseUint(ctx.Query("channel_account_id"), 10, 64)
	if err != nil || accountID == 0 {
		response.Error(ctx, http.StatusBadRequest, "channel_account_id 非法")
		return
	}
	convID := strings.TrimSpace(ctx.Query("conversation_id"))
	if convID == "" {
		response.Error(ctx, http.StatusBadRequest, "conversation_id 不能为空")
		return
	}
	// 租户边界完全由 auth 组的 TenantMiddleware 上下文提供；不接受客户端自称 tenant_id，
	// 缺失租户时底层仓储 fail-closed 拒绝（宪法原则 I / 多租户隔离）。
	snap, err := c.svc.Snapshot(ctx.Request.Context(), uint(accountID), convID)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取共享对话上下文失败")
		return
	}
	response.Success(ctx, snap, "ok")
}
