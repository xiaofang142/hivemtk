package controller

import (
	"errors"
	"net/http"
	"strconv"

	"hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type GeoDecisionController struct {
	svc *service.DecisionService
}

func NewGeoDecisionController(svc *service.DecisionService) *GeoDecisionController {
	return &GeoDecisionController{svc: svc}
}

// @Summary      待处理补位任务列表（内容生产管线消费入口）
// @Tags      geo-decision
// @Router    /api/geo/decision/tasks [get]
// GetTasks 待处理补位任务列表（内容生产管线消费入口）
// GET /api/geo/decision/tasks?limit=50
func (c *GeoDecisionController) GetTasks(ctx *gin.Context) {
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "50"))
	tasks, err := c.svc.ListTasks(ctx.Request.Context(), limit)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "查询任务失败: "+err.Error())
		return
	}
	response.Success(ctx, gin.H{"list": tasks, "total": len(tasks)}, "ok")
}

// @Summary POST /api/geo/decision/tasks/{id}/done
// @Tags      geo-decision
// @Router    /api/geo/decision/tasks/{id}/done [post]
// MarkTaskDone 标记任务完成
// POST /api/geo/decision/tasks/:id/done
func (c *GeoDecisionController) MarkTaskDone(ctx *gin.Context) {
	if err := c.svc.MarkDone(ctx.Request.Context(), ctx.Param("id")); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.Error(ctx, http.StatusNotFound, "补位任务不存在")
			return
		}
		response.Error(ctx, http.StatusInternalServerError, "更新失败: "+err.Error())
		return
	}
	response.Success(ctx, nil, "已完成")
}

// @Summary      L4 决策链总览
// @Tags      geo-decision
// @Router    /api/geo/decision/report [get]
// GetDecisionReport L4 决策链总览：
// 思维链规模 / 补位任务漏斗 / 捕获线索数（OneID→成交归因的前置层）
// GET /api/geo/decision/report
func (c *GeoDecisionController) GetDecisionReport(ctx *gin.Context) {
	report, err := c.svc.Report(ctx.Request.Context())
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "统计失败: "+err.Error())
		return
	}
	response.Success(ctx, report, "ok")
}
