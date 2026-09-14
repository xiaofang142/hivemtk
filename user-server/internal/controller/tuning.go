package controller

import (
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

// GetConfidenceSignal 信号详情
func (c *TuningController) GetConfidenceSignal(ctx *gin.Context) {
	id := ctx.Param("id")
	row, err := c.svc.GetConfidenceSignal(ctx.Request.Context(), id)
	if err != nil {
		response.NotFoundError(ctx, "signal")
		return
	}
	response.Success(ctx, row, "")
}

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

// ListThresholdPolicies 策略列表
func (c *TuningController) ListThresholdPolicies(ctx *gin.Context) {
	rows, err := c.svc.ListThresholdPolicies(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "list failed: "+err.Error())
		return
	}
	response.Success(ctx, gin.H{"list": rows}, "")
}

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

// ListChampionBaselines 基线列表
func (c *TuningController) ListChampionBaselines(ctx *gin.Context) {
	rows, err := c.svc.ListChampionBaselines(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "list failed: "+err.Error())
		return
	}
	response.Success(ctx, gin.H{"list": rows}, "")
}

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

// UpdatePromptCandidateStatus 更新候选状态
func (c *TuningController) UpdatePromptCandidateStatus(ctx *gin.Context) {
	id := ctx.Param("id")
	status := ctx.Query("status")
	if status == "" {
		response.Error(ctx, http.StatusBadRequest, "status required")
		return
	}
	if err := c.svc.UpdatePromptCandidateStatus(ctx.Request.Context(), id, status); err != nil {
		response.ErrorFromDB(ctx, err, "update failed: "+err.Error())
		return
	}
	response.Success(ctx, gin.H{"id": id, "status": status}, "")
}

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
