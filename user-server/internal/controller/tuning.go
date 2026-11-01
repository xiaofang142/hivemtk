package controller

import (
	"errors"
	"hivemtk-user/internal/dto"
	cursorpkg "hivemtk-user/internal/pkg/pagination"
	"hivemtk-user/internal/pkg/utils/pagination"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"hivemtk-user/internal/service/confidence"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

func safeDiv(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

// TuningController 统一管理 Controller
type TuningController struct {
	svc service.TuningService
}

// NewTuningController 构造
func NewTuningController(svc service.TuningService) *TuningController {
	return &TuningController{svc: svc}
}

// @Summary      信号列表
// @Tags      tuning
// @Router    /api/admin/tuning/confidence/signals [get]
// ListConfidenceSignals 信号列表
func (c *TuningController) ListConfidenceSignals(ctx *gin.Context) {
	page, pageSize, err := pagination.Parse(ctx)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	sessionID := ctx.Query("session_id")

	rows, total, err := c.svc.ListConfidenceSignals(ctx.Request.Context(), sessionID, page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, "list failed: "+err.Error())
		return
	}
	c.jsonList(ctx, rows, total, page, pageSize)
}

// @Summary      信号详情
// @Tags      tuning
// @Router    /api/admin/tuning/confidence/signals/{id} [get]
// GetConfidenceSignal 信号详情
func (c *TuningController) GetConfidenceSignal(ctx *gin.Context) {
	id := ctx.Param("id")
	row, err := c.svc.GetConfidenceSignal(ctx.Request.Context(), id)
	if err != nil {
		if errors.Is(err, service.ErrConfidenceSignalNotFound) {
			response.NotFoundError(ctx, "signal")
			return
		}
		response.ErrorFromDB(ctx, err, "get signal failed: "+err.Error())
		return
	}
	response.Success(ctx, row, "")
}

// @Summary      聚合统计
// @Tags      tuning
// @Router    /api/admin/tuning/confidence/signals/stats [get]
// StatsConfidenceSignals 聚合统计
func (c *TuningController) StatsConfidenceSignals(ctx *gin.Context) {
	days, _ := strconv.Atoi(ctx.DefaultQuery("days", "7"))
	if days < 1 {
		days = 7
	}
	since := time.Now().AddDate(0, 0, -days)

	rows, err := c.svc.StatsConfidenceSignals(ctx.Request.Context(), since)
	if err != nil {
		response.ErrorFromDB(ctx, err, "stats failed: "+err.Error())
		return
	}
	response.Success(ctx, gin.H{"days": days, "bands": rows}, "")
}

// @Summary      校准记录列表
// @Tags      tuning
// @Router    /api/admin/tuning/confidence/calibrations [get]
// ListCalibrations 校准记录列表
func (c *TuningController) ListCalibrations(ctx *gin.Context) {
	page, pageSize, err := pagination.Parse(ctx)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	signalType := ctx.Query("signal_type")

	rows, total, err := c.svc.ListCalibrations(ctx.Request.Context(), signalType, page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, "list failed: "+err.Error())
		return
	}
	c.jsonList(ctx, rows, total, page, pageSize)
}

// @Summary      策略列表
// @Tags      tuning
// @Router    /api/admin/tuning/confidence/policies [get]
// ListThresholdPolicies 策略列表
func (c *TuningController) ListThresholdPolicies(ctx *gin.Context) {
	rows, err := c.svc.ListThresholdPolicies(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "list failed: "+err.Error())
		return
	}
	response.Success(ctx, gin.H{"list": rows}, "")
}

// @Summary      创建/更新策略
// @Tags      tuning
// @Router    /api/admin/tuning/confidence/policies [put]
// UpsertThresholdPolicy 创建/更新策略
func (c *TuningController) UpsertThresholdPolicy(ctx *gin.Context) {
	var body dto.ThresholdPolicyRequest
	if err := ctx.ShouldBindJSON(&body); err != nil {
		response.Error(ctx, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	policy := confidence.ToThresholdPolicyModel(&body)
	if err := c.svc.UpsertThresholdPolicy(ctx.Request.Context(), policy); err != nil {
		response.ErrorFromDB(ctx, err, "save failed: "+err.Error())
		return
	}
	response.Success(ctx, policy, "")
}

// @Summary      评分列表
// @Tags      tuning
// @Router    /api/admin/tuning/humanize/scores [get]
// ListHumanizeScores 评分列表
func (c *TuningController) ListHumanizeScores(ctx *gin.Context) {
	page, pageSize, err := pagination.Parse(ctx)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	sessionID := ctx.Query("session_id")
	passed := ctx.Query("passed")

	var passedPtr *bool
	if passed == "true" {
		v := true
		passedPtr = &v
	} else if passed == "false" {
		v := false
		passedPtr = &v
	}

	rows, total, err := c.svc.ListHumanizeScores(ctx.Request.Context(), sessionID, passedPtr, page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, "list failed: "+err.Error())
		return
	}
	c.jsonList(ctx, rows, total, page, pageSize)
}

// @Summary      拟人度统计
// @Tags      tuning
// @Router    /api/admin/tuning/humanize/scores/stats [get]
// StatsHumanizeScores 拟人度统计
func (c *TuningController) StatsHumanizeScores(ctx *gin.Context) {
	days, _ := strconv.Atoi(ctx.DefaultQuery("days", "7"))
	if days < 1 {
		days = 7
	}
	since := time.Now().AddDate(0, 0, -days)

	stat, err := c.svc.StatsHumanizeScores(ctx.Request.Context(), since)
	if err != nil {
		response.ErrorFromDB(ctx, err, "stats failed: "+err.Error())
		return
	}
	response.Success(ctx, gin.H{
		"days":         days,
		"avg_score":    stat.AvgScore,
		"passed_count": stat.Passed,
		"failed_count": stat.Failed,
		"total_count":  stat.Total,
		"pass_rate":    safeDiv(float64(stat.Passed), float64(stat.Total)),
	}, "")
}

// @Summary      基线列表
// @Tags      tuning
// @Router    /api/admin/tuning/humanize/baselines [get]
// ListChampionBaselines 基线列表
func (c *TuningController) ListChampionBaselines(ctx *gin.Context) {
	rows, err := c.svc.ListChampionBaselines(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "list failed: "+err.Error())
		return
	}
	response.Success(ctx, gin.H{"list": rows}, "")
}

// @Summary      事件列表
// @Tags      tuning
// @Router    /api/admin/tuning/feedback/events [get]
// ListFeedbackEvents 事件列表
func (c *TuningController) ListFeedbackEvents(ctx *gin.Context) {
	page, pageSize, err := pagination.Parse(ctx)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	sessionID := ctx.Query("session_id")
	signalKey := ctx.Query("signal_key")
	cursor := cursorpkg.Cursor(ctx.Query("cursor"))

	rows, total, err := c.svc.ListFeedbackEvents(ctx.Request.Context(), sessionID, signalKey, page, pageSize, cursor)
	if err != nil {
		response.ErrorFromDB(ctx, err, "list failed: "+err.Error())
		return
	}
	c.jsonListWithCursor(ctx, rows, total, page, pageSize)
}

// @Summary      反馈事件统计
// @Tags      tuning
// @Router    /api/admin/tuning/feedback/events/stats [get]
// StatsFeedbackEvents 反馈事件统计
func (c *TuningController) StatsFeedbackEvents(ctx *gin.Context) {
	days, _ := strconv.Atoi(ctx.DefaultQuery("days", "7"))
	if days < 1 {
		days = 7
	}
	since := time.Now().AddDate(0, 0, -days)

	rows, err := c.svc.StatsFeedbackEvents(ctx.Request.Context(), since)
	if err != nil {
		response.ErrorFromDB(ctx, err, "stats failed: "+err.Error())
		return
	}
	response.Success(ctx, gin.H{"days": days, "signals": rows}, "")
}

// @Summary      销冠对话列表
// @Tags      tuning
// @Router    /api/admin/tuning/feedback/dialogues [get]
// ListChampionDialogues 销冠对话列表
func (c *TuningController) ListChampionDialogues(ctx *gin.Context) {
	page, pageSize, err := pagination.Parse(ctx)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	intent := ctx.Query("intent")
	industry := ctx.Query("industry")
	cursor := cursorpkg.Cursor(ctx.Query("cursor"))

	rows, total, err := c.svc.ListChampionDialogues(ctx.Request.Context(), intent, industry, page, pageSize, cursor)
	if err != nil {
		response.ErrorFromDB(ctx, err, "list failed: "+err.Error())
		return
	}
	c.jsonListWithCursor(ctx, rows, total, page, pageSize)
}

// @Summary      候选列表
// @Tags      tuning
// @Router    /api/admin/tuning/prompt/candidates [get]
// ListPromptCandidates 候选列表
func (c *TuningController) ListPromptCandidates(ctx *gin.Context) {
	page, pageSize, err := pagination.Parse(ctx)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	status := ctx.Query("status")
	cursor := cursorpkg.Cursor(ctx.Query("cursor"))

	rows, total, err := c.svc.ListPromptCandidates(ctx.Request.Context(), status, page, pageSize, cursor)
	if err != nil {
		response.ErrorFromDB(ctx, err, "list failed: "+err.Error())
		return
	}
	c.jsonListWithCursor(ctx, rows, total, page, pageSize)
}

// @Summary      更新候选状态
// @Tags      tuning
// @Router    /api/admin/tuning/prompt/candidates/{id}/status [put]
// UpdatePromptCandidateStatus 更新候选状态
func (c *TuningController) UpdatePromptCandidateStatus(ctx *gin.Context) {
	id := ctx.Param("id")
	// status 先看 body，再回落 query。
	// 前端 feedbackLoop/Panel.vue:642 是 `updatePromptCandidateStatus(row.id, {status:'approved'})`
	// ——axios 把第二个参数放进 **body**。只读 query 的话，"批准/驳回"按钮稳定 400
	// 「status required」，候选池永远停在待审（实测两形态一致）。
	status := ctx.Query("status")
	if status == "" {
		var body struct {
			Status string `json:"status"`
		}
		_ = ctx.ShouldBindJSON(&body)
		status = body.Status
	}
	if status == "" {
		response.Error(ctx, http.StatusBadRequest, "status required")
		return
	}
	// id 必须是数字主键：此前直接把字符串塞进 bigint 查询，PG 回
	// `invalid input syntax for type bigint: "x" (SQLSTATE 22P02)`，而下面那句
	// fallbackMsg 又把 err.Error() 拼进对外文案 → SQL 原文泄漏给调用方
	// （2026-11-02 API 全量回归实测）。非法 id 属于入参错误，在入口就拦。
	if _, err := strconv.ParseUint(id, 10, 64); err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的候选ID")
		return
	}
	if err := c.svc.UpdatePromptCandidateStatus(ctx.Request.Context(), id, status); err != nil {
		// 不再把 err.Error() 拼进 fallbackMsg：那是 SQL 原文通道。
		response.ErrorFromDB(ctx, err, "更新候选状态失败")
		return
	}
	response.Success(ctx, gin.H{"id": id, "status": status}, "")
}

// @Summary      臂列表
// @Tags      tuning
// @Router    /api/admin/tuning/bandit/arms [get]
// ListBanditArms 臂列表
func (c *TuningController) ListBanditArms(ctx *gin.Context) {
	page, pageSize, err := pagination.Parse(ctx)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	experimentID := ctx.Query("experiment_id")
	sopID := ctx.Query("sop_id")
	cursor := cursorpkg.Cursor(ctx.Query("cursor"))

	rows, total, err := c.svc.ListBanditArms(ctx.Request.Context(), experimentID, sopID, page, pageSize, cursor)
	if err != nil {
		response.ErrorFromDB(ctx, err, "list failed: "+err.Error())
		return
	}
	c.jsonListWithCursor(ctx, rows, total, page, pageSize)
}

// @Summary      低质样本列表
// @Tags      tuning
// @Router    /api/admin/tuning/humanize/low-quality [get]
// ListLowQualitySamples 低质样本列表
func (c *TuningController) ListLowQualitySamples(ctx *gin.Context) {
	page, pageSize, err := pagination.Parse(ctx)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	sampleType := ctx.Query("sample_type")

	rows, total, err := c.svc.ListLowQualitySamples(ctx.Request.Context(), sampleType, page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, "list failed: "+err.Error())
		return
	}
	c.jsonList(ctx, rows, total, page, pageSize)
}

func (c *TuningController) jsonList(ctx *gin.Context, list any, total int64, page, pageSize int) {
	response.Success(ctx, gin.H{
		"list":  list,
		"total": total,
		"page":  page,
		"size":  pageSize,
	}, "")
}

// jsonListWithCursor 在列表响应中附带 next_cursor（keyset 分页翻页游标，空串表示无更多数据）
func (c *TuningController) jsonListWithCursor(ctx *gin.Context, list any, total int64, page, pageSize int) {
	next := cursorpkg.NextCursor(list, pageSize)
	response.Success(ctx, gin.H{
		"list":        list,
		"total":       total,
		"page":        page,
		"size":        pageSize,
		"next_cursor": string(next),
	}, "")
}
