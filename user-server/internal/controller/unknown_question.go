// unknown_question_controller.go 未知问题运营控制器（T1，五层 L2）。
//
// 设计文档：docs/architecture/design/T1-confidence-reflux.md
// 风格对齐 controller/faq.go：RegisterRoutes 分组注册、pagination.Parse、
// response.Success / Error / SuccessWithPage、swag godoc。
package controller

import (
	"errors"
	"net/http"
	"strconv"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/pagination"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service/reflux"

	"github.com/gin-gonic/gin"
)

// UnknownQuestionController 未知问题控制器
type UnknownQuestionController struct {
	svc *reflux.RefluxService
}

// NewUnknownQuestionController 构造
func NewUnknownQuestionController(svc *reflux.RefluxService) *UnknownQuestionController {
	return &UnknownQuestionController{svc: svc}
}

// RegisterRoutes 注册路由（挂 /api/unknown-questions）
func (c *UnknownQuestionController) RegisterRoutes(router *gin.RouterGroup) {
	g := router.Group("/unknown-questions")
	{
		g.GET("", c.List)
		g.GET("/stats", c.Stats)
		g.GET("/:id", c.Get)
		g.POST("/:id/submit", c.Submit)
		g.POST("/:id/review", c.Review)
		g.GET("/clusters", c.ClusterList)
		g.GET("/clusters/:id", c.ClusterGet)
		g.POST("/clusters/:id/review", c.ClusterReview)
	}
}

func (c *UnknownQuestionController) requireSvc(ctx *gin.Context) bool {
	if c.svc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "回流服务未装配")
		return false
	}
	return true
}

// List godoc
// @Summary      未知问题分页列表
// @Tags         unknown-questions
// @Param        status   query string false "状态 draft|pending_review|adopted|ignored|merged"
// @Param        source   query string false "来源 low_confidence|handoff|bad_case|csat_negative|no_match"
// @Param        keyword  query string false "问句模糊"
// @Router       /api/unknown-questions [get]
func (c *UnknownQuestionController) List(ctx *gin.Context) {
	if !c.requireSvc(ctx) {
		return
	}
	page, pageSize, err := pagination.Parse(ctx)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	q := map[string]any{}
	if v := ctx.Query("status"); v != "" {
		q["status"] = v
	}
	if v := ctx.Query("source"); v != "" {
		q["source"] = v
	}
	if v := ctx.Query("keyword"); v != "" {
		q["question_like"] = v
	}
	if v := ctx.Query("agent_id"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			q["agent_id"] = n
		}
	}
	rows, total, err := c.svc.ListQuestions(ctx.Request.Context(), q, page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.SuccessWithPage(ctx, rows, int64(page), int64(pageSize), total)
}

// Stats godoc
// @Summary      未知问题统计
// @Router       /api/unknown-questions/stats [get]
func (c *UnknownQuestionController) Stats(ctx *gin.Context) {
	if !c.requireSvc(ctx) {
		return
	}
	data, err := c.svc.Stats(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, data, "ok")
}

// Get godoc
// @Summary      未知问题详情（含审核流水）
// @Router       /api/unknown-questions/{id} [get]
func (c *UnknownQuestionController) Get(ctx *gin.Context) {
	if !c.requireSvc(ctx) {
		return
	}
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误")
		return
	}
	data, err := c.svc.QuestionDetail(ctx.Request.Context(), id)
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}
	response.Success(ctx, data, "ok")
}

// Submit godoc
// @Summary      提交审核 draft→pending_review
// @Router       /api/unknown-questions/{id}/submit [post]
func (c *UnknownQuestionController) Submit(ctx *gin.Context) {
	if !c.requireSvc(ctx) {
		return
	}
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误")
		return
	}
	if err := c.svc.SubmitQuestion(ctx.Request.Context(), id, operatorOf(ctx)); err != nil {
		if err == reflux.ErrIllegalTransition {
			response.Error(ctx, http.StatusBadRequest, err.Error())
			return
		}
		// 0 行迁移（id 不存在 / 状态已被改走）不再冒充成功，按"这条没了"回 404。
		if isNotFoundError(err) {
			response.NotFound(ctx, "未知问题不存在或状态已变更")
			return
		}
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, gin.H{"id": id, "status": model.UQStatusPendingReview}, "ok")
}

// Review godoc
// @Summary      单条审核 adopt|ignore|merge
// @Router       /api/unknown-questions/{id}/review [post]
func (c *UnknownQuestionController) Review(ctx *gin.Context) {
	if !c.requireSvc(ctx) {
		return
	}
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误")
		return
	}
	var req reflux.AdoptRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	data, err := c.svc.ReviewQuestion(ctx.Request.Context(), id, &req, operatorOf(ctx))
	if err != nil {
		// D-39：目标不存在是 404，不是 400「入参错误」——此前两者都回 400，
		// 客户端无法区分"这条未知问题已被删/换租户"与"body 写错了"。
		if errors.Is(err, reflux.ErrNotFound) {
			response.Error(ctx, http.StatusNotFound, err.Error())
			return
		}
		if errors.Is(err, reflux.ErrIllegalTransition) {
			response.Error(ctx, http.StatusBadRequest, err.Error())
			return
		}
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, data, "ok")
}

// ClusterList godoc
// @Summary      聚类簇分页（按 total_frequency 降序）
// @Router       /api/unknown-questions/clusters [get]
func (c *UnknownQuestionController) ClusterList(ctx *gin.Context) {
	if !c.requireSvc(ctx) {
		return
	}
	page, pageSize, err := pagination.Parse(ctx)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	q := map[string]any{}
	if v := ctx.Query("status"); v != "" {
		q["status"] = v
	}
	rows, total, err := c.svc.ListClusters(ctx.Request.Context(), q, page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.SuccessWithPage(ctx, rows, int64(page), int64(pageSize), total)
}

// ClusterGet godoc
// @Summary      簇详情（成员 + 样本 + 流水）
// @Router       /api/unknown-questions/clusters/{id} [get]
func (c *UnknownQuestionController) ClusterGet(ctx *gin.Context) {
	if !c.requireSvc(ctx) {
		return
	}
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误")
		return
	}
	data, err := c.svc.ClusterDetail(ctx.Request.Context(), id)
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}
	response.Success(ctx, data, "ok")
}

// ClusterReview godoc
// @Summary      簇级审核 adopt|ignore（adopt 对全簇生效生成一条 FAQ）
// @Router       /api/unknown-questions/clusters/{id}/review [post]
func (c *UnknownQuestionController) ClusterReview(ctx *gin.Context) {
	if !c.requireSvc(ctx) {
		return
	}
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误")
		return
	}
	var req reflux.AdoptRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	data, err := c.svc.ReviewCluster(ctx.Request.Context(), id, &req, operatorOf(ctx))
	if err != nil {
		// D-39：簇不存在 → 404（与单条审核同口径）。
		if errors.Is(err, reflux.ErrNotFound) {
			response.Error(ctx, http.StatusNotFound, err.Error())
			return
		}
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, data, "ok")
}

func operatorOf(ctx *gin.Context) string {
	if v := ctx.GetString("operator_id"); v != "" {
		return v
	}
	return ctx.GetString("user_id")
}
