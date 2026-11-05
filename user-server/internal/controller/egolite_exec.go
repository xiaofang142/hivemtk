package controller

// ego-lite 执行域与会话域 HTTP 层（2026-11-05 二次审计）。
// 原先是 router/egolite_routes.go 的内联闭包，现全部收进控制器，只调 service。

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/browsercore"
	"hivemtk-user/internal/egolite"
	"hivemtk-user/internal/pkg/utils/response"
)

// ---------------------------------------------------------------- 执行域

// StartExecution POST /egolite/executions
func (h *EgoLiteController) StartExecution(c *gin.Context) {
	var req egolite.StartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Envelope(c, 400, gin.H{"code": 1, "message": err.Error()})
		return
	}
	e, err := h.exec.Start(c.Request.Context(), currentUID(c), req)
	if err != nil {
		status := 400
		switch err {
		case egolite.ErrConcurrencyGate:
			status = 429
		case egolite.ErrAssetVersionConflict, egolite.ErrFlowScanGate:
			// 409：资源当前状态冲突（版本已更新/扫描门未过），非入参格式错
			status = 409
		}
		response.Envelope(c, status, gin.H{"code": 1, "message": err.Error()})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": e})
}

// ListExecutions GET /egolite/executions
func (h *EgoLiteController) ListExecutions(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	list, total, err := h.exec.ListByUser(c.Request.Context(), currentUID(c), page, size)
	if err != nil {
		response.Envelope(c, 500, gin.H{"code": 1, "message": egoErrorMessage(err, "服务暂时不可用，请稍后重试")})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": gin.H{"list": list, "total": total}})
}

// GetExecution GET /egolite/executions/:id
func (h *EgoLiteController) GetExecution(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	e, err := h.exec.GetForUser(c.Request.Context(), uint(id), currentUID(c))
	if err != nil {
		response.Envelope(c, 404, gin.H{"code": 1, "message": "执行不存在"})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": e})
}

// ListExecutionEvents GET /egolite/executions/:id/events
func (h *EgoLiteController) ListExecutionEvents(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	// 归属校验先于读事件（越权与不存在同响应）
	if _, err := h.exec.GetForUser(c.Request.Context(), uint(id), currentUID(c)); err != nil {
		response.Envelope(c, 404, gin.H{"code": 1, "message": "执行不存在"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	// 增量游标：after_id 传上次收到的最大事件 id，只回新增（Monitor 3s 轮询不重拉全量）
	afterID, _ := strconv.ParseUint(c.DefaultQuery("after_id", "0"), 10, 64)
	events, err := h.exec.ListEventsAfter(c.Request.Context(), uint(id), uint(afterID), limit)
	if err != nil {
		response.Envelope(c, 500, gin.H{"code": 1, "message": egoErrorMessage(err, "服务暂时不可用，请稍后重试")})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": gin.H{"list": events}})
}

// StreamExecution GET /egolite/executions/:id/stream（S-13 SSE）
func (h *EgoLiteController) StreamExecution(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	streamEgoExecution(c, h.exec, currentUID(c), uint(id))
}

// RedispatchExecution POST /egolite/executions/:id/redispatch
func (h *EgoLiteController) RedispatchExecution(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	// runner 不在线时 Start 会把执行留在 created（待下发）；这是用户上线后的唯一重发口
	e, err := h.exec.Redispatch(c.Request.Context(), currentUID(c), uint(id))
	if err != nil {
		status := 400
		if err == egolite.ErrExecStateConflict {
			status = 409
		}
		response.Envelope(c, status, gin.H{"code": 1, "message": err.Error()})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": e, "message": "已重发（runner 不在线时保留在待下发态）"})
}

// AbortExecution POST /egolite/executions/:id/abort
func (h *EgoLiteController) AbortExecution(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	e, err := h.exec.Abort(c.Request.Context(), currentUID(c), uint(id), "用户停止")
	if err != nil {
		status := 400
		if err == egolite.ErrExecStateConflict {
			status = 409
		}
		response.Envelope(c, status, gin.H{"code": 1, "message": err.Error()})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": e})
}

// ConfirmExecution POST /egolite/executions/:id/confirm
func (h *EgoLiteController) ConfirmExecution(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	var body struct {
		Allow bool `json:"allow"`
	}
	_ = c.ShouldBindJSON(&body)
	e, err := h.exec.Confirm(c.Request.Context(), currentUID(c), uint(id), body.Allow)
	if err != nil {
		status := 400
		if err == egolite.ErrExecStateConflict {
			status = 409
		}
		response.Envelope(c, status, gin.H{"code": 1, "message": err.Error()})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": e})
}

// ---------------------------------------------------------------- 会话域（M1/M2 ops 轮次协议）

// OpenSession POST /egolite/sessions
func (h *EgoLiteController) OpenSession(c *gin.Context) {
	var body struct {
		Goal          string   `json:"goal"`
		Engine        string   `json:"engine"`
		SpaceName     string   `json:"space_name"`
		Domains       []string `json:"domains"`
		WriteConfirm  string   `json:"write_confirm"`
		TimeoutSec    int      `json:"timeout_sec"`
		MaxRounds     int      `json:"max_rounds"`
		MaxActions    int      `json:"max_actions"`
		ConfirmTTLSec int      `json:"confirm_ttl_sec"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Envelope(c, 400, gin.H{"code": 1, "message": err.Error()})
		return
	}
	pol := browsercore.DefaultPolicy()
	pol.Domains = body.Domains
	if body.WriteConfirm != "" {
		pol.WriteConfirm = body.WriteConfirm
	}
	if body.TimeoutSec > 0 {
		pol.TimeoutSec = body.TimeoutSec
	}
	if body.MaxRounds > 0 {
		pol.MaxRounds = body.MaxRounds
	}
	if body.MaxActions > 0 {
		pol.MaxActions = body.MaxActions
	}
	if body.ConfirmTTLSec > 0 {
		pol.ConfirmTTLSec = body.ConfirmTTLSec
	}
	sess, err := h.session.Open(c.Request.Context(), browsercore.OpenRequest{
		UserID: currentUID(c), Engine: body.Engine, Mode: browsercore.ModeOps,
		Goal: body.Goal, SpaceName: body.SpaceName, CreatedFrom: "ui", Policy: pol,
	})
	if err != nil {
		response.Envelope(c, egoSessionStatus(err), gin.H{"code": 1, "message": err.Error()})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": sess})
}

// ListSessions GET /egolite/sessions
func (h *EgoLiteController) ListSessions(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	list, total, err := h.session.List(c.Request.Context(), currentUID(c), page, size)
	if err != nil {
		response.Envelope(c, 500, gin.H{"code": 1, "message": egoErrorMessage(err, "服务暂时不可用，请稍后重试")})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": gin.H{"list": list, "total": total}})
}

// GetSession GET /egolite/sessions/:id
func (h *EgoLiteController) GetSession(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	sess, err := h.session.Get(c.Request.Context(), currentUID(c), uint(id))
	if err != nil {
		response.Envelope(c, egoSessionStatus(err), gin.H{"code": 1, "message": err.Error()})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": sess})
}

// SessionRound POST /egolite/sessions/:id/round
func (h *EgoLiteController) SessionRound(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	var body struct {
		Ops     []browsercore.Op        `json:"ops"`
		Observe browsercore.ObserveSpec `json:"observe"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Envelope(c, 400, gin.H{"code": 1, "message": err.Error()})
		return
	}
	res, err := h.session.Act(c.Request.Context(), browsercore.RoundRequest{
		UserID: currentUID(c), SessionID: uint(id), Ops: body.Ops, Observe: body.Observe,
	})
	if err != nil {
		response.Envelope(c, egoSessionStatus(err), gin.H{"code": 1, "message": err.Error()})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": res, "message": "已派发本轮；用 GET /sessions/:id/rounds 或 /observe 取结果"})
}

// SessionObserve POST /egolite/sessions/:id/observe
func (h *EgoLiteController) SessionObserve(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	var body struct {
		Round int `json:"round"`
	}
	_ = c.ShouldBindJSON(&body)
	info, err := h.session.Observe(c.Request.Context(), browsercore.ObserveRequest{
		UserID: currentUID(c), SessionID: uint(id), Round: body.Round,
	})
	if err != nil {
		response.Envelope(c, egoSessionStatus(err), gin.H{"code": 1, "message": err.Error()})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": info})
}

// SessionRounds GET /egolite/sessions/:id/rounds
func (h *EgoLiteController) SessionRounds(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	list, err := h.session.Rounds(c.Request.Context(), currentUID(c), uint(id), limit)
	if err != nil {
		response.Envelope(c, egoSessionStatus(err), gin.H{"code": 1, "message": err.Error()})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": gin.H{"list": list}})
}

// ExportSession GET /egolite/sessions/:id/export（审计导出 T22）
func (h *EgoLiteController) ExportSession(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	isAdmin := c.GetString("role") == "admin"
	switch c.DefaultQuery("format", "json") {
	case "csv":
		b, err := h.gov.ExportSessionCSV(c.Request.Context(), currentUID(c), uint(id), isAdmin)
		if err != nil {
			response.Envelope(c, egoSessionStatus(err), gin.H{"code": 1, "message": err.Error()})
			return
		}
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=ego-session-%d.csv", id))
		c.Data(200, "text/csv; charset=utf-8", b)
	default:
		exp, err := h.gov.ExportSession(c.Request.Context(), currentUID(c), uint(id), isAdmin)
		if err != nil {
			response.Envelope(c, egoSessionStatus(err), gin.H{"code": 1, "message": err.Error()})
			return
		}
		response.Envelope(c, 200, gin.H{"code": 0, "data": exp})
	}
}

// SessionConfirm POST /egolite/sessions/:id/confirm
func (h *EgoLiteController) SessionConfirm(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	var body struct {
		Allow bool `json:"allow"`
	}
	_ = c.ShouldBindJSON(&body)
	sess, err := h.session.Confirm(c.Request.Context(), currentUID(c), uint(id), body.Allow)
	if err != nil {
		response.Envelope(c, egoSessionStatus(err), gin.H{"code": 1, "message": err.Error()})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": sess})
}

// SessionAbort POST /egolite/sessions/:id/abort
func (h *EgoLiteController) SessionAbort(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	var body struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&body)
	sess, err := h.session.Abort(c.Request.Context(), currentUID(c), uint(id), body.Reason)
	if err != nil {
		response.Envelope(c, egoSessionStatus(err), gin.H{"code": 1, "message": err.Error()})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": sess})
}

// SaveSessionAsFlow POST /egolite/sessions/:id/save-as-flow
func (h *EgoLiteController) SaveSessionAsFlow(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	var body struct {
		Name string `json:"name"`
	}
	_ = c.ShouldBindJSON(&body)
	f, report, err := h.session.SaveAsFlow(c.Request.Context(), currentUID(c), uint(id), body.Name)
	if err != nil {
		response.Envelope(c, egoSessionStatus(err), gin.H{"code": 1, "message": err.Error(), "data": gin.H{"flow": f, "scan_report": report}})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": gin.H{"flow": f, "scan_report": report}})
}

// FinishSession POST /egolite/sessions/:id/finish
func (h *EgoLiteController) FinishSession(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	var body struct {
		Keep []string `json:"keep"`
	}
	_ = c.ShouldBindJSON(&body)
	sess, err := h.session.Finish(c.Request.Context(), currentUID(c), uint(id), body.Keep)
	if err != nil {
		response.Envelope(c, egoSessionStatus(err), gin.H{"code": 1, "message": err.Error()})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": sess})
}

// egoSessionStatus 通用会话错误 → HTTP 状态（与既有执行域口径一致）
func egoSessionStatus(err error) int {
	switch {
	case errors.Is(err, browsercore.ErrNotFound), errors.Is(err, egolite.ErrSessionNotFound):
		return 404
	case errors.Is(err, browsercore.ErrGate):
		return 429
	case errors.Is(err, browsercore.ErrStateConflict), errors.Is(err, egolite.ErrEgoRunnerOffline):
		return 409
	case errors.Is(err, browsercore.ErrPolicyViolation), errors.Is(err, browsercore.ErrEngineUnavailable):
		return 400
	case errors.Is(err, egolite.ErrQuotaExceeded):
		return 429
	default:
		return 400
	}
}
