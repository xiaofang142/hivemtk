package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

// ConversationContextController 共享对话上下文只读入口（T054 交叉引用展示数据源）。
// 员工授权在 Service 先于上下文读取校验；底层仓储继续强制租户隔离。
type ConversationContextController struct {
	svc *service.ConversationContextService
}

func NewConversationContextController(svc *service.ConversationContextService) *ConversationContextController {
	return &ConversationContextController{svc: svc}
}

func (c *ConversationContextController) Get(ctx *gin.Context) {
	userID := currentStaffUserID(ctx)
	if userID == 0 {
		response.Error(ctx, http.StatusUnauthorized, response.ErrUnauthorized)
		return
	}
	if _, ok := channelTenantID(ctx); !ok {
		return
	}
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
	// 员工和租户只取认证上下文，不接受查询参数中的身份或角色。
	var scopes []service.ConversationContextScope
	platform, groupID, isGroup := ctx.Query("platform"), ctx.Query("group_id"), ctx.Query("is_group")
	if platform != "" || groupID != "" || isGroup != "" {
		group, parseErr := strconv.ParseBool(isGroup)
		if platform == "" || parseErr != nil || (!group && groupID != "") {
			response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams)
			return
		}
		scopes = append(scopes, service.ConversationContextScope{Platform: platform, IsGroup: group, GroupID: groupID})
	}
	snap, err := c.svc.SnapshotForUser(ctx.Request.Context(), userID, uint(accountID), convID, scopes...)
	if err != nil {
		if errors.Is(err, service.ErrConversationContextForbidden) {
			response.Error(ctx, http.StatusForbidden, response.ErrPermissionDenied)
			return
		}
		if errors.Is(err, service.ErrConversationContextScope) {
			response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams)
			return
		}
		response.ErrorFromDB(ctx, err, "获取共享对话上下文失败")
		return
	}
	response.Success(ctx, snap, "ok")
}
