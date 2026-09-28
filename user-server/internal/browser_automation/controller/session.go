package controller

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/browser_automation/dto"
	"hivemtk-user/internal/browser_automation/service"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// SessionController 会话控制器（查询 + 手动中断）
type SessionController struct {
	svc *service.SessionService
}

func NewSessionController(svc *service.SessionService) *SessionController {
	return &SessionController{svc: svc}
}

// sessionErrToResponse 会话域出口。
// 原先六个入口都是 `if err != nil { 404 会话不存在 }`——把「库读不动」和「这条会话不在」
// 压成同一个结论：用户看到「会话不存在」去找 id，监控把一次服务端故障记成用户误操作。
// 接上任务/触发器域共用的那份分流（baErrToResponse），404 的主语仍是「会话」。
func sessionErrToResponse(ctx *gin.Context, err error) {
	baErrToResponse(ctx, err, "会话不存在")
}

// List GET /browser-automation/sessions?status=&page=&limit=
func (c *SessionController) List(ctx *gin.Context) {
	var req dto.ListSessionReq
	if err := ctx.ShouldBindQuery(&req); err != nil {
		bindErrToResponse(ctx, err, &req)
		return
	}
	list, total, err := c.svc.ListByUser(ctx.Request.Context(), taskUserID(ctx), req.Status, req.Page, req.Limit)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "查询会话失败")
		return
	}
	response.SuccessWithList(ctx, list, total)
}

// Get GET /browser-automation/sessions/:id
func (c *SessionController) Get(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "invalid id")
		return
	}
	sess, err := c.svc.Get(ctx.Request.Context(), uint(id), taskUserID(ctx))
	if err != nil {
		sessionErrToResponse(ctx, err)
		return
	}
	sess.ConfirmPending = c.svc.ConfirmPending(uint(id)) // D7：是否正等人工放行
	response.Success(ctx, sess, "ok")
}

// ListSteps GET /browser-automation/sessions/:id/steps
func (c *SessionController) ListSteps(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "invalid id")
		return
	}
	steps, err := c.svc.ListSteps(ctx.Request.Context(), uint(id), taskUserID(ctx))
	if err != nil {
		sessionErrToResponse(ctx, err)
		return
	}
	response.SuccessWithList(ctx, steps, int64(len(steps)))
}

// ListLogs GET /browser-automation/sessions/:id/logs?direction=command|event|judge
// D1（G1 补口）：append-only 命令流审计查询（归属校验与 direction 过滤在 service）。
func (c *SessionController) ListLogs(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "invalid id")
		return
	}
	direction := ctx.DefaultQuery("direction", "")
	if direction != "" && direction != "command" && direction != "event" && direction != "judge" {
		response.Error(ctx, http.StatusBadRequest, "direction 仅支持 command/event/judge")
		return
	}
	logs, err := c.svc.ListCommandLogs(ctx.Request.Context(), uint(id), taskUserID(ctx), direction)
	if err != nil {
		sessionErrToResponse(ctx, err)
		return
	}
	response.SuccessWithList(ctx, logs, int64(len(logs)))
}

// ListByTask GET /browser-automation/tasks/:id/sessions
func (c *SessionController) ListByTask(ctx *gin.Context) {
	id, ok := parseID(ctx)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "50"))
	list, total, err := c.svc.ListByTask(ctx.Request.Context(), id, taskUserID(ctx), page, limit)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "查询会话失败")
		return
	}
	response.SuccessWithList(ctx, list, total)
}

// Export GET /browser-automation/sessions/:id/export
// I5：session 全量审计包（会话+步流水+命令流+LLM 成本账）单请求归并，
// 前端「导出审计包」按钮直接落盘 JSON——铁律 4 审计链的离线归档面。
func (c *SessionController) Export(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "invalid id")
		return
	}
	sess, steps, logs, plans, digests, err := c.svc.SessionExport(ctx.Request.Context(), uint(id), taskUserID(ctx))
	if err != nil {
		sessionErrToResponse(ctx, err)
		return
	}
	ctx.Header("Content-Disposition", "attachment; filename=browser_session_"+ctx.Param("id")+"_audit.json")
	response.Success(ctx, gin.H{
		"session": sess, "steps": steps, "command_log": logs, "llm_plans": plans,
		// 命令流被按界裁掉之后，这一份是「裁走了哪些行」的唯一凭据。
		// 没有它，command_log 为空这一件事有两种解释，而导出包说不出是哪一种。
		"audit_digests": digests,
		"exported_at":   time.Now().Format(time.RFC3339),
	}, "ok")
}

// Stop POST /browser-automation/sessions/:id/stop
func (c *SessionController) Stop(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "invalid id")
		return
	}
	var req dto.StopSessionReq
	_ = ctx.ShouldBindJSON(&req) // reason 可选
	ok, err := c.svc.Stop(ctx.Request.Context(), uint(id), taskUserID(ctx), req.Reason)
	if err != nil {
		sessionErrToResponse(ctx, err)
		return
	}
	if !ok {
		response.Success(ctx, gin.H{"stopped": false}, "会话不在运行中")
		return
	}
	response.Success(ctx, gin.H{"stopped": true}, "中断信号已发送")
}

// ConfirmGate GET /browser-automation/sessions/:id/confirm-gate
// D7 读侧详情：放行前必须看得见「等的是第几步、将要提交什么、到什么时候」，
// 并把 payload_hash 带回去给 Confirm。归属校验在 service（预览含正文，越权读到就是外泄）。
func (c *SessionController) ConfirmGate(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "invalid id")
		return
	}
	gate, pending, err := c.svc.ConfirmGate(ctx.Request.Context(), uint(id), taskUserID(ctx))
	if err != nil {
		sessionErrToResponse(ctx, err)
		return
	}
	data := gin.H{"pending": pending}
	if pending {
		data["gate"] = gate
	}
	response.Success(ctx, data, "ok")
}

// Confirm POST /browser-automation/sessions/:id/confirm
// D7：放行 require_confirm 闸门上的写操作提交点。放行**绑载荷**（A5）——请求必须带
// GET confirm-gate 取到的 payload_hash，哈希不符即 409 且闸门原样留着：一条不带载荷的放行
// 与一张空白支票同构（批准的内容可以换，也可以被下一个挂起点消费掉）。
// 四态分流（A10）：放行 / 没有闸门 / 载荷不符 / 闸门在另一个进程——最后一种与第三种
// 都要与「没开闸门」分开，否则用户会去改编排，而该改的是实例数。
func (c *SessionController) Confirm(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "invalid id")
		return
	}
	var req dto.ConfirmSessionReq
	_ = ctx.ShouldBindJSON(&req) // 缺字段在下面单独判 400，不混进「参数错误」模板
	hash := strings.TrimSpace(req.PayloadHash)
	if hash == "" {
		response.Error(ctx, http.StatusBadRequest,
			"缺少 payload_hash：放行必须指明批准的是哪份载荷（先取 GET /sessions/:id/confirm-gate）")
		return
	}
	res, err := c.svc.Confirm(ctx.Request.Context(), uint(id), taskUserID(ctx), hash)
	if err != nil {
		sessionErrToResponse(ctx, err)
		return
	}
	writeConfirmResult(ctx, res)
}

// writeConfirmResult 服务层四态 → HTTP 语义（抽出来是为了让四条映射在 HTTP 面全部可断言：
// granted/mismatch 两条需要进程内真有闸门，控制器测试造不出来，而「闸门在别处」与「没有闸门」
// 的文案差异恰恰是最不该被写反的一条）。
// data.status 带机器可读的态名：response.Error 走 int 码时会把 409 一律折成 DUPLICATE_ENTRY_3003
// （实测过），前端要靠文案 substring 才能分辨是哪一种「没放行」——那是把结论建在措辞上。
// 错误码表（internal/pkg/utils）在本批车道外，不为两个态去扩公共码域，改在 data 里给稳定判别位。
func writeConfirmResult(ctx *gin.Context, res service.ConfirmResult) {
	confirmed := res.Status == service.ConfirmStatusGranted
	data := gin.H{"confirmed": confirmed, "status": string(res.Status)}
	if res.Status == service.ConfirmStatusMismatch && res.Gate != nil {
		data["gate"] = res.Gate // 不符时把当前挂起的载荷带回，前端照着重新高亮该批哪一份
	}
	switch res.Status {
	case service.ConfirmStatusMismatch, service.ConfirmStatusOtherInstance:
		response.Error(ctx, http.StatusConflict, confirmMessage(res.Status), data)
	default:
		// 放行成功与「本就没有待确认的提交点」都是 200：后者是查出来的事实，不是错误。
		response.Success(ctx, data, confirmMessage(res.Status))
	}
}

func confirmMessage(status service.ConfirmStatus) string {
	switch status {
	case service.ConfirmStatusGranted:
		return "已确认放行"
	case service.ConfirmStatusMismatch:
		return "放行的载荷与挂起中的提交内容不一致，未放行——请重新查看待确认内容"
	case service.ConfirmStatusOtherInstance:
		return "确认闸门挂在另一个服务进程上，本次放行未生效——请刷新会话或确认实例数（多副本需共享挂起态）"
	default:
		return "该会话当前没有待确认的提交点"
	}
}
