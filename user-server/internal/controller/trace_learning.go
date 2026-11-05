package controller

import (
	"strconv"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service/trace_learning"

	"github.com/gin-gonic/gin"
)

// TraceLearningController 追踪自学习 API（手动触发评估 + 查询打分/权重）
type TraceLearningController struct {
	svc *trace_learning.Service
}

// NewTraceLearningController 构造控制器
func NewTraceLearningController(svc *trace_learning.Service) *TraceLearningController {
	return &TraceLearningController{svc: svc}
}

// @Summary      手动触发 trace 自学习评估（调知识库权重）
// @Tags      trace-learning
// @Router    /api/monitor/trace-eval/trigger [post]
// TriggerEval POST /api/monitor/trace-eval/trigger?hours=0&limit=20&dry=false
// 手动触发：扫描未评估的 trace，批量打分并调整知识库权重。
//   - hours：opt-in 时间窗（小时）；默认 0=评估全部未评估 trace（不漏评）。>0 时仅该时间窗内。
//   - dry=true：仅评分 + 预览计划调整，不调权、不写审计（安全评估自学习质量，返回 previews）。
func (c *TraceLearningController) TriggerEval(ctx *gin.Context) {
	hours, _ := strconv.Atoi(ctx.DefaultQuery("hours", "0"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))
	dry, _ := strconv.ParseBool(ctx.DefaultQuery("dry", "false"))
	res, err := c.svc.RunBatch(ctx.Request.Context(), hours, limit, dry)
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	resp := gin.H{"processed": res.Processed}
	if dry {
		resp["previews"] = res.Previews
	}
	response.Success(ctx, resp, "ok")
}

// @Summary      查询自学习打分审计记录
// @Tags      trace-learning
// @Router    /api/monitor/trace-eval/logs [get]
// EvalLogs GET /api/monitor/trace-eval/logs?limit=50
// 查询最近打分审计记录（供前端展示「自学习打分」）。
func (c *TraceLearningController) EvalLogs(ctx *gin.Context) {
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "50"))
	logs, err := c.svc.Logs(ctx.Request.Context(), limit)
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, logs, "ok")
}

// @Summary      查询知识库权重排行（自学习影响）
// @Tags      trace-learning
// @Router    /api/monitor/knowledge-weights [get]
// KnowledgeWeights GET /api/monitor/knowledge-weights?limit=50
// 查询知识库权重排行（权重偏离 1.0 最大的 chunk，供前端展示「自学习影响」）。
func (c *TraceLearningController) KnowledgeWeights(ctx *gin.Context) {
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "50"))
	rows, err := c.svc.TopWeights(ctx.Request.Context(), limit)
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, rows, "ok")
}
