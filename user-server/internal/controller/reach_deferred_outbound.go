package controller

import (
	"net/http"
	"strconv"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// ReachDeferredOutboundController D02：主动触达延迟出站管理口（查询/取消）
type ReachDeferredOutboundController struct {
	queue *service.PersistentQuietHoursQueue
}

// NewReachDeferredOutboundController 构造
func NewReachDeferredOutboundController(q *service.PersistentQuietHoursQueue) *ReachDeferredOutboundController {
	return &ReachDeferredOutboundController{queue: q}
}

// List 延迟出站列表（状态/渠道过滤 + 分页）
// GET /api/reach/deferred-outbound?status=pending&channel=wecom&page=1&page_size=20
func (ctrl *ReachDeferredOutboundController) List(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "20"))
	rows, total, err := ctrl.queue.ListDeferredOutbounds(
		ctx.Request.Context(),
		ctx.Query("channel"),
		ctx.Query("status"),
		page, pageSize,
	)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"list": rows, "total": total}, "查询成功")
}

// Cancel 取消一条待发的延迟出站（仅 pending 可取消）
// POST /api/reach/deferred-outbound/:id/cancel
func (ctrl *ReachDeferredOutboundController) Cancel(ctx *gin.Context) {
	id64, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil || id64 == 0 {
		response.Error(ctx, http.StatusBadRequest, "参数错误: id 必须为正整数")
		return
	}
	if err := ctrl.queue.CancelDeferredOutbound(ctx.Request.Context(), uint(id64)); err != nil {
		if err.Error() == "record not found" {
			response.Error(ctx, http.StatusNotFound, "记录不存在或不可取消（仅 pending 可取消）")
			return
		}
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"id": id64}, "已取消")
}
