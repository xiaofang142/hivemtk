package controller

import (
	"strconv"
	"strings"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

type LeadMiningController struct{}

func NewLeadMiningController() *LeadMiningController {
	return &LeadMiningController{}
}

// @Summary      获取线索挖掘配置
// @Tags      lead-mining
// @Router    /api/lead-mining/config [get]
func (c *LeadMiningController) GetConfig(ctx *gin.Context) {
	cfg, err := service.GetLeadMiningConfig(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, cfg, "success")
}

// @Summary      获取线索挖掘运行状态
// @Tags      lead-mining
// @Router    /api/lead-mining/status [get]
func (c *LeadMiningController) GetStatus(ctx *gin.Context) {
	status := service.GetStatus()
	response.Success(ctx, status, "success")
}

// @Summary      请求体不能为空：至少提供一个配置字段（如 keywords、requirement）
// @Tags      lead-mining
// @Router    /api/lead-mining/config [post]
func (c *LeadMiningController) SaveConfig(ctx *gin.Context) {
	var cfg dto.LeadMiningConfig
	if err := ctx.ShouldBindJSON(&cfg); err != nil {
		response.Error(ctx, 400, err.Error())
		return
	}
	// 防御空 body {} 全零值覆盖配置
	if isEmptyLeadMiningConfig(&cfg) {
		response.Error(ctx, 400, "请求体不能为空：至少提供一个配置字段（如 keywords、requirement）")
		return
	}
	if err := service.SaveLeadMiningConfig(ctx.Request.Context(), &cfg); err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, cfg, "success")
}

// isEmptyLeadMiningConfig 判断请求是否未携带任何业务字段
func isEmptyLeadMiningConfig(cfg *dto.LeadMiningConfig) bool {
	return !cfg.Enabled && len(cfg.Keywords) == 0 && len(cfg.Tags) == 0 &&
		cfg.Requirement == "" && len(cfg.Channels) == 0 &&
		cfg.MinIntentScore == 0 && cfg.Model == ""
}

// GetJudgements 返回线索发掘判定记录（最小只读，LM-PR5）。
func (c *LeadMiningController) GetJudgements(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))
	platform := ctx.Query("platform")
	accountID := ctx.Query("account_id")
	leadRole := ctx.Query("lead_role")
	var matched *bool
	if v := ctx.Query("matched"); v != "" {
		b := v == "1" || v == "true"
		matched = &b
	}
	uid := getUserIDFromContext(ctx)
	scope, _ := service.NewBusinessScopeService().Resolve(ctx.Request.Context(), uid)
	var accounts []string
	if !scope.AllAccounts {
		ids, _ := scope.AllowedSourceAccountIDs()
		accounts = ids
	}
	items, total, err := service.ListLeadMiningJudgementsScoped(ctx.Request.Context(), accounts, page, limit, platform, accountID, leadRole, matched)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取判定记录失败")
		return
	}
	response.Success(ctx, gin.H{"list": items, "total": total, "page": page, "limit": limit}, "ok")
}

// GetClueStats 返回线索发掘统计（角色/状态/渠道/会话类型，LM-PR5）。
func (c *LeadMiningController) GetClueStats(ctx *gin.Context) {
	stats, err := service.NewClueServiceDefault(nil).GetLeadMiningStats(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取线索统计失败")
		return
	}
	response.Success(ctx, stats, "ok")
}

// GetJudgementDetail 返回判定记录回放详情（LM-PR6）。
func (c *LeadMiningController) GetJudgementDetail(ctx *gin.Context) {
	judgementID := strings.TrimSpace(ctx.Param("judgement_id"))
	uid := getUserIDFromContext(ctx)
	scope, _ := service.NewBusinessScopeService().Resolve(ctx.Request.Context(), uid)
	var accounts []string
	if !scope.AllAccounts {
		ids, _ := scope.AllowedSourceAccountIDs()
		accounts = ids
	}
	rec, err := service.GetLeadMiningJudgementDetailScoped(ctx.Request.Context(), judgementID, accounts)
	if err != nil {
		if service.IsRecordNotFound(err) {
			response.Error(ctx, 404, "判定记录不存在")
			return
		}
		response.ErrorFromDB(ctx, err, "获取判定详情失败")
		return
	}
	response.Success(ctx, rec, "ok")
}

// SubmitJudgementFeedback 标记判定误报/有效（LM-PR6）。
func (c *LeadMiningController) SubmitJudgementFeedback(ctx *gin.Context) {
	judgementID := strings.TrimSpace(ctx.Param("judgement_id"))
	var req struct {
		FeedbackStatus string `json:"feedback_status"`
		FeedbackReason string `json:"feedback_reason"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, 400, "请求参数错误")
		return
	}
	by := ""
	if v, ok := ctx.Get("user_id"); ok {
		by = strings.TrimSpace(strings.ReplaceAll(strings.TrimSpace(toString(v)), " ", ""))
	}
	if by == "" {
		if v, ok := ctx.Get("username"); ok {
			by = toString(v)
		}
	}
	if err := service.SubmitLeadMiningJudgementFeedback(ctx.Request.Context(), judgementID, req.FeedbackStatus, req.FeedbackReason, by); err != nil {
		if service.IsRecordNotFound(err) {
			response.Error(ctx, 404, "判定记录不存在")
			return
		}
		response.Error(ctx, 400, err.Error())
		return
	}
	response.Success(ctx, gin.H{"judgement_id": judgementID, "feedback_status": req.FeedbackStatus}, "ok")
}

// GetQualityStats 返回线索发掘质量看板（LM-PR6/LM-13：按员工可见账号收口）。
func (c *LeadMiningController) GetQualityStats(ctx *gin.Context) {
	days, _ := strconv.Atoi(ctx.DefaultQuery("days", "7"))
	uid := getUserIDFromContext(ctx)
	scope, _ := service.NewBusinessScopeService().Resolve(ctx.Request.Context(), uid)
	var accounts []string
	if !scope.AllAccounts {
		ids, _ := scope.AllowedSourceAccountIDs()
		accounts = ids
	}
	stats, err := service.GetLeadMiningQualityStatsScoped(ctx.Request.Context(), accounts, days)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取质量看板失败")
		return
	}
	response.Success(ctx, stats, "ok")
}

func toString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case uint:
		return strconv.FormatUint(uint64(t), 10)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case uint64:
		return strconv.FormatUint(t, 10)
	case float64:
		return strconv.FormatInt(int64(t), 10)
	default:
		return ""
	}
}
