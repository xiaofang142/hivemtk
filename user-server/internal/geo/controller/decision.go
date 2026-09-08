package controller

import (
	"context"
	"net/http"
	"strconv"

	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

type GeoDecisionController struct {
	chainRepo repository.GeoQueryChainRepository
	taskRepo  repository.GeoContentTaskRepository
	report    DecisionReportQuerier
}

// DecisionReportQuerier L4 结果指标查询端口（主域 clue 统计由装配层实现）
type DecisionReportQuerier interface {
	CountCapturedLeads(ctx context.Context) (int64, error)
}

func NewGeoDecisionController(chainRepo repository.GeoQueryChainRepository,
	taskRepo repository.GeoContentTaskRepository, report DecisionReportQuerier) *GeoDecisionController {
	return &GeoDecisionController{chainRepo: chainRepo, taskRepo: taskRepo, report: report}
}

// GetTasks 待处理补位任务列表（内容生产管线消费入口）
// GET /api/geo/decision/tasks?limit=50
func (c *GeoDecisionController) GetTasks(ctx *gin.Context) {
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "50"))
	tasks, err := c.taskRepo.ListPending(ctx.Request.Context(), limit)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "查询任务失败: "+err.Error())
		return
	}
	response.Success(ctx, gin.H{"list": tasks, "total": len(tasks)}, "ok")
}

// MarkTaskDone 标记任务完成
// POST /api/geo/decision/tasks/:id/done
func (c *GeoDecisionController) MarkTaskDone(ctx *gin.Context) {
	if err := c.taskRepo.MarkDone(ctx.Request.Context(), ctx.Param("id")); err != nil {
		response.Error(ctx, http.StatusInternalServerError, "更新失败: "+err.Error())
		return
	}
	response.Success(ctx, nil, "已完成")
}

// GetDecisionReport L4 决策链总览：
// 思维链规模 / 补位任务漏斗 / 捕获线索数（OneID→成交归因的前置层）
// GET /api/geo/decision/report
func (c *GeoDecisionController) GetDecisionReport(ctx *gin.Context) {
	ctxReq := ctx.Request.Context()
	pending, _ := c.taskRepo.ListPending(ctxReq, 200)
	doneN, _ := c.taskRepo.CountByStatus(ctxReq, "done")
	var leads int64
	if c.report != nil {
		leads, _ = c.report.CountCapturedLeads(ctxReq)
	}
	response.Success(ctx, gin.H{
		"tasks_pending":  len(pending),
		"tasks_done":     doneN,
		"leads_captured": leads,
	}, "ok")
}
