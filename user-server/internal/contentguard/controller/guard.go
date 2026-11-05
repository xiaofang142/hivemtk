// Package controller 内容安全闸（ContentGuard）的 HTTP 出口。
//
// 本层只做三件事（与 bad_case / human_task 控制器同一分工）：
//  1. 参数绑定与形状校验（page/pageSize 夹取、id 解析、枚举白名单）；
//  2. 调 service 门面，把 sentinel error 映射成语义化状态码；
//  3. 按 CLAUDE.md 的响应契约输出 {code,data,message} / {list,total}。
//
// 判定、阈值、幂等、处置一律不在这里重写一遍——那会让"页面显示拦了、线上却没拦"
// 成为可能。
package controller

import (
	"errors"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/contentguard/model"
	"hivemtk-user/internal/contentguard/service"
	bizerr "hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"
)

// GuardController 内容审查管理端控制器。
//
// svc 允许为 nil（运行时未装配）：所有出口走 Available() 回 503——这是本处天然的关闸。
type GuardController struct {
	svc *service.Runtime
}

// NewGuardController 构造。
func NewGuardController(svc *service.Runtime) *GuardController {
	return &GuardController{svc: svc}
}

// RegisterRoutes 挂到**已鉴权 + 租户 admin** 的组下（路径：/api/manage/content-guard/*）。
//
// 为什么整组都在 admin 档：改词库与解冻是"能否让某个人的内容被放行/被拦"的权力，
// 与"这页能不能打开"无关，普通坐席不该有。
func (c *GuardController) RegisterRoutes(router *gin.RouterGroup) {
	g := router.Group("/manage/content-guard")
	{
		g.GET("/words", c.ListWords)
		g.POST("/words", c.UpsertWord)
		g.POST("/words/import", c.ImportWords)
		g.DELETE("/words/:id", c.DeleteWord)

		g.GET("/policies", c.ListPolicies)
		g.PUT("/policies", c.SavePolicy)

		g.GET("/records", c.ListRecords)
		g.GET("/records/:id", c.GetRecord)
		g.POST("/records/:id/review", c.ReviewRecord)

		g.GET("/sanctions", c.ListSanctions)
		g.POST("/sanctions", c.ApplySanction)
		g.POST("/sanctions/:id/release", c.ReleaseSanction)

		g.POST("/scan", c.Scan)
		g.GET("/stats", c.Stats)
	}
}

func (c *GuardController) available(ctx *gin.Context) bool {
	if c.svc != nil && c.svc.Available() {
		return true
	}
	response.ErrorWithBusinessCode(ctx, bizerr.CodeInternal, "内容审查底座未装配", gin.H{})
	return false
}

// ── 词库 ───────────────────────────────────────────────────────────────

// ListWords GET /api/manage/content-guard/words
func (c *GuardController) ListWords(ctx *gin.Context) {
	if !c.available(ctx) {
		return
	}
	q := service.WordQuery{
		ListKind: strings.TrimSpace(ctx.Query("list_kind")),
		Category: strings.TrimSpace(ctx.Query("category")),
		Keyword:  strings.TrimSpace(ctx.Query("keyword")),
		Page:     queryInt(ctx, "page", 1),
		PageSize: queryInt(ctx, "page_size", 20),
	}
	if raw := ctx.Query("enabled"); raw != "" {
		v := raw == "true" || raw == "1"
		q.Enabled = &v
	}
	rows, total, err := c.svc.ListWords(ctx.Request.Context(), q)
	if err != nil {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeInternal, "查询词库失败", gin.H{})
		return
	}
	response.Success(ctx, gin.H{"list": rows, "total": total}, "ok")
}

type wordReq struct {
	ListKind string `json:"list_kind"`
	Word     string `json:"word"`
	Category string `json:"category"`
	Severity int    `json:"severity"`
	Enabled  *bool  `json:"enabled"`
	Remark   string `json:"remark"`
}

// UpsertWord POST /api/manage/content-guard/words
func (c *GuardController) UpsertWord(ctx *gin.Context) {
	if !c.available(ctx) {
		return
	}
	var req wordReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeParamInvalid, "参数错误", gin.H{})
		return
	}
	saved, err := c.svc.UpsertWord(ctx.Request.Context(), toWordInput(req))
	if err != nil {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeParamInvalid, err.Error(), gin.H{})
		return
	}
	response.Success(ctx, saved, "ok")
}

type importWordsReq struct {
	Words []wordReq `json:"words"`
}

// ImportWords POST /api/manage/content-guard/words/import
func (c *GuardController) ImportWords(ctx *gin.Context) {
	if !c.available(ctx) {
		return
	}
	var req importWordsReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeParamInvalid, "参数错误", gin.H{})
		return
	}
	if len(req.Words) == 0 {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeParamInvalid, "words 不能为空", gin.H{})
		return
	}
	inputs := make([]service.WordInput, 0, len(req.Words))
	for _, w := range req.Words {
		inputs = append(inputs, toWordInput(w))
	}
	n, err := c.svc.ImportWords(ctx.Request.Context(), inputs)
	if err != nil {
		logger.Errorf("contentguard import words failed: %v", err)
		response.ErrorWithBusinessCode(ctx, bizerr.CodeInternal, "导入词库失败，请稍后重试", gin.H{})
		return
	}
	response.Success(ctx, gin.H{"imported": n}, "ok")
}

// DeleteWord DELETE /api/manage/content-guard/words/:id
func (c *GuardController) DeleteWord(ctx *gin.Context) {
	if !c.available(ctx) {
		return
	}
	id, ok := parseUint(ctx, "id")
	if !ok {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeParamInvalid, "id 非法", gin.H{})
		return
	}
	if err := c.svc.DeleteWord(ctx.Request.Context(), uint(id)); err != nil {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeInternal, "删除失败", gin.H{})
		return
	}
	response.Success(ctx, gin.H{"id": id}, "ok")
}

func toWordInput(req wordReq) service.WordInput {
	return service.WordInput{
		ListKind: req.ListKind,
		Word:     req.Word,
		Category: req.Category,
		Severity: req.Severity,
		Enabled:  req.Enabled,
		Remark:   req.Remark,
	}
}

// ── 策略 ───────────────────────────────────────────────────────────────

// ListPolicies GET /api/manage/content-guard/policies
func (c *GuardController) ListPolicies(ctx *gin.Context) {
	if !c.available(ctx) {
		return
	}
	rows, err := c.svc.ListPolicies(ctx.Request.Context())
	if err != nil {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeInternal, "查询策略失败", gin.H{})
		return
	}
	response.Success(ctx, gin.H{"list": rows, "total": len(rows)}, "ok")
}

type policyReq struct {
	SubjectType       string `json:"subject_type"`
	Enabled           *bool  `json:"enabled"`
	Scenes            string `json:"scenes"`
	BlockSeverity     int    `json:"block_severity"`
	ReviewSeverity    int    `json:"review_severity"`
	BlockAction       string `json:"block_action"`
	EscalatePermanent int    `json:"escalate_permanent"`
	TempFreezeMinutes int    `json:"temp_freeze_minutes"`
	WindowMinutes     int    `json:"window_minutes"`
	APISampleRate     int    `json:"api_sample_rate"`
	APIEnabled        *bool  `json:"api_enabled"`
}

// SavePolicy PUT /api/manage/content-guard/policies
func (c *GuardController) SavePolicy(ctx *gin.Context) {
	if !c.available(ctx) {
		return
	}
	var req policyReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeParamInvalid, "参数错误", gin.H{})
		return
	}
	operatorID := operatorIDFrom(ctx)
	saved, err := c.svc.SavePolicy(ctx.Request.Context(), service.PolicyInput{
		SubjectType:       req.SubjectType,
		Enabled:           req.Enabled,
		Scenes:            req.Scenes,
		BlockSeverity:     req.BlockSeverity,
		ReviewSeverity:    req.ReviewSeverity,
		BlockAction:       req.BlockAction,
		EscalatePermanent: req.EscalatePermanent,
		TempFreezeMinutes: req.TempFreezeMinutes,
		WindowMinutes:     req.WindowMinutes,
		APISampleRate:     req.APISampleRate,
		APIEnabled:        req.APIEnabled,
		OperatorID:        operatorID,
	})
	if err != nil {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeParamInvalid, err.Error(), gin.H{})
		return
	}
	response.Success(ctx, saved, "ok")
}

// ── 审查记录 ───────────────────────────────────────────────────────────

// ListRecords GET /api/manage/content-guard/records
func (c *GuardController) ListRecords(ctx *gin.Context) {
	if !c.available(ctx) {
		return
	}
	q := service.RecordQuery{
		SubjectType:  strings.TrimSpace(ctx.Query("subject_type")),
		SubjectID:    strings.TrimSpace(ctx.Query("subject_id")),
		Scene:        strings.TrimSpace(ctx.Query("scene")),
		Verdict:      strings.TrimSpace(ctx.Query("verdict")),
		ManualStatus: strings.TrimSpace(ctx.Query("manual_status")),
		TraceID:      strings.TrimSpace(ctx.Query("trace_id")),
		Page:         queryInt(ctx, "page", 1),
		PageSize:     queryInt(ctx, "page_size", 20),
	}
	rows, total, err := c.svc.ListRecords(ctx.Request.Context(), q)
	if err != nil {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeInternal, "查询审查记录失败", gin.H{})
		return
	}
	response.Success(ctx, gin.H{"list": rows, "total": total}, "ok")
}

// GetRecord GET /api/manage/content-guard/records/:id
func (c *GuardController) GetRecord(ctx *gin.Context) {
	if !c.available(ctx) {
		return
	}
	id, ok := parseUint(ctx, "id")
	if !ok {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeParamInvalid, "id 非法", gin.H{})
		return
	}
	rec, err := c.svc.GetRecord(ctx.Request.Context(), id)
	if err != nil {
		c.writeRecordErr(ctx, err)
		return
	}
	response.Success(ctx, rec, "ok")
}

type reviewReq struct {
	Status string `json:"status"`
}

// ReviewRecord POST /api/manage/content-guard/records/:id/review
func (c *GuardController) ReviewRecord(ctx *gin.Context) {
	if !c.available(ctx) {
		return
	}
	id, ok := parseUint(ctx, "id")
	if !ok {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeParamInvalid, "id 非法", gin.H{})
		return
	}
	var req reviewReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeParamInvalid, "参数错误", gin.H{})
		return
	}
	rec, err := c.svc.ReviewRecord(ctx.Request.Context(), id, strings.TrimSpace(req.Status), operatorIDFrom(ctx))
	if err != nil {
		c.writeRecordErr(ctx, err)
		return
	}
	response.Success(ctx, rec, "ok")
}

func (c *GuardController) writeRecordErr(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrRecordNotFound):
		response.ErrorWithBusinessCode(ctx, bizerr.CodeNotFound, "审查记录不存在", gin.H{})
	case errors.Is(err, service.ErrReviewStatusInvalid):
		response.ErrorWithBusinessCode(ctx, bizerr.CodeParamInvalid, "status 只支持 confirmed / overturned", gin.H{})
	case errors.Is(err, service.ErrSanctionNotFound):
		response.ErrorWithBusinessCode(ctx, bizerr.CodeNotFound, "关联处置不存在", gin.H{})
	default:
		response.ErrorWithBusinessCode(ctx, bizerr.CodeInternal, "操作失败", gin.H{})
	}
}

// ── 处置 ───────────────────────────────────────────────────────────────

// ListSanctions GET /api/manage/content-guard/sanctions
func (c *GuardController) ListSanctions(ctx *gin.Context) {
	if !c.available(ctx) {
		return
	}
	q := service.SanctionQuery{
		SubjectType: strings.TrimSpace(ctx.Query("subject_type")),
		SubjectID:   strings.TrimSpace(ctx.Query("subject_id")),
		Kind:        strings.TrimSpace(ctx.Query("kind")),
		Level:       strings.TrimSpace(ctx.Query("level")),
		ActiveOnly:  ctx.Query("active_only") == "true" || ctx.Query("active_only") == "1",
		Page:        queryInt(ctx, "page", 1),
		PageSize:    queryInt(ctx, "page_size", 20),
	}
	rows, total, err := c.svc.ListSanctions(ctx.Request.Context(), q)
	if err != nil {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeInternal, "查询处置失败", gin.H{})
		return
	}
	response.Success(ctx, gin.H{"list": rows, "total": total}, "ok")
}

type sanctionReq struct {
	SubjectType     string `json:"subject_type"`
	SubjectID       string `json:"subject_id"`
	Kind            string `json:"kind"`
	Level           string `json:"level"`
	Reason          string `json:"reason"`
	DurationMinutes int    `json:"duration_minutes"`
}

// ApplySanction POST /api/manage/content-guard/sanctions
func (c *GuardController) ApplySanction(ctx *gin.Context) {
	if !c.available(ctx) {
		return
	}
	var req sanctionReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeParamInvalid, "参数错误", gin.H{})
		return
	}
	operatorID := operatorIDFrom(ctx)
	row, err := c.svc.ApplySanction(ctx.Request.Context(), service.ApplyInput{
		SubjectType:     strings.TrimSpace(req.SubjectType),
		SubjectID:       strings.TrimSpace(req.SubjectID),
		Kind:            strings.TrimSpace(req.Kind),
		Level:           strings.TrimSpace(req.Level),
		Reason:          req.Reason,
		Source:          "manual",
		OperatorID:      operatorID,
		DurationMinutes: req.DurationMinutes,
	})
	if err != nil {
		c.writeSanctionErr(ctx, err)
		return
	}
	response.Success(ctx, row, "ok")
}

type releaseReq struct {
	Reason string `json:"reason"`
}

// ReleaseSanction POST /api/manage/content-guard/sanctions/:id/release
func (c *GuardController) ReleaseSanction(ctx *gin.Context) {
	if !c.available(ctx) {
		return
	}
	id, ok := parseUint(ctx, "id")
	if !ok {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeParamInvalid, "id 非法", gin.H{})
		return
	}
	var req releaseReq
	_ = ctx.ShouldBindJSON(&req)
	row, err := c.svc.ReleaseSanction(ctx.Request.Context(), id, operatorIDFrom(ctx), strings.TrimSpace(req.Reason))
	if err != nil {
		c.writeSanctionErr(ctx, err)
		return
	}
	response.Success(ctx, row, "ok")
}

func (c *GuardController) writeSanctionErr(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrSanctionNotFound):
		response.ErrorWithBusinessCode(ctx, bizerr.CodeNotFound, "处置不存在", gin.H{})
	case errors.Is(err, service.ErrSanctionSubjectRequired):
		response.ErrorWithBusinessCode(ctx, bizerr.CodeParamInvalid, "subject_id 不能为空", gin.H{})
	case errors.Is(err, service.ErrSanctionKindInvalid),
		errors.Is(err, service.ErrSanctionLevelInvalid),
		errors.Is(err, service.ErrRuntimeUnavailable):
		response.ErrorWithBusinessCode(ctx, bizerr.CodeParamInvalid, err.Error(), gin.H{})
	default:
		response.ErrorWithBusinessCode(ctx, bizerr.CodeInternal, "操作失败", gin.H{})
	}
}

// ── 试算与统计 ─────────────────────────────────────────────────────────

type scanReq struct {
	SubjectType string `json:"subject_type"`
	SubjectID   string `json:"subject_id"`
	Scene       string `json:"scene"`
	Content     string `json:"content"`
}

// Scan POST /api/manage/content-guard/scan
//
// 同步试算：只返回结论，不入队、不留记录、不处置。运营调词库时用它验证口径。
func (c *GuardController) Scan(ctx *gin.Context) {
	if !c.available(ctx) {
		return
	}
	var req scanReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeParamInvalid, "参数错误", gin.H{})
		return
	}
	if req.SubjectType == "" {
		req.SubjectType = model.SubjectUser
	}
	if req.Scene == "" {
		req.Scene = model.SceneInboundMessage
	}
	assessment, err := c.svc.ScanNow(ctx.Request.Context(), service.ScanRequest{
		SubjectType: req.SubjectType,
		SubjectID:   req.SubjectID,
		Scene:       req.Scene,
		Content:     req.Content,
	})
	if err != nil {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeParamInvalid, err.Error(), gin.H{})
		return
	}
	response.Success(ctx, gin.H{
		"verdict":      assessment.Verdict,
		"action":       assessment.Action,
		"freeze_level": assessment.FreezeLevel,
		"max_severity": assessment.MaxSeverity,
		"hits":         assessment.Hits,
		"skipped":      assessment.Skipped,
		"sources":      assessment.Sources,
		"latency_ms":   assessment.LatencyMs,
	}, "ok")
}

// Stats GET /api/manage/content-guard/stats
func (c *GuardController) Stats(ctx *gin.Context) {
	if !c.available(ctx) {
		return
	}
	response.Success(ctx, c.svc.Stats(ctx.Request.Context()), "ok")
}

// ── 工具 ───────────────────────────────────────────────────────────────

func queryInt(ctx *gin.Context, key string, fallback int) int {
	raw := strings.TrimSpace(ctx.Query(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}

func parseUint(ctx *gin.Context, key string) (uint64, bool) {
	raw := strings.TrimSpace(ctx.Param(key))
	if raw == "" || len(raw) > 20 {
		return 0, false
	}
	v, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// operatorIDFrom 取当前操作者 ID（令牌里写入，取不到按 0 = 匿名记录，不阻断操作）。
func operatorIDFrom(ctx *gin.Context) uint {
	v, ok := ctx.Get("user_id")
	if !ok {
		return 0
	}
	switch id := v.(type) {
	case uint:
		return id
	case int:
		return uint(id)
	case float64:
		return uint(id)
	case uint64:
		return uint(id)
	}
	return 0
}
