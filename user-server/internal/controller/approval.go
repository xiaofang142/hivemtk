// approval.go 异步审批的 HTTP 出口（T-P3-04，待办中心"审/驳"那两颗按钮）。
//
// 为什么这一层必须存在：待办池里 kind=approval 那几行点"完成"只会把待办关掉，
// 而**审批行还停在 pending** —— 挂在它上面的流程不会被叫醒，下次同一对象入队又拿到
// 那条旧 pending。也就是说只给列表不给裁决口，等于给用户一个会把闸门拆掉的按钮。
// 所以本卡连后端一起做：详情 + 裁决两条，别的一条都不加（列表仍归 human-tasks，
// 见下）。
//
// 本层三件事，与 controller/human_task.go 同一分工：
//  1. **身份**：decided_by 是"人工放行率"这个数的归人依据，取不到身份就 401；
//  2. **权限**：裁决落在 middleware.ManagerOrAdminMiddleware 上（在 handler 之前），
//     所以缺 role 的请求先到 403，到不了本层的 401 判据；
//  3. **状态码**：服务侧 sentinel → 400/404/409/500。
//
// 刻意**没有** GET /api/approvals 列表口：待办中心要看的"等谁裁决"是待办池的事实，
// 两处各出一个列表就是第二个事实源（两张表在 pending/done 上可以各说各话）。
package controller

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

// ApprovalController 审批详情与人工裁决出口。
//
// svc 允许是 nil（审批运行时未装配，即 FF_LTC_APPROVAL_RESUME=off 或无 DB 句柄）：
// 所有出口走 Available() 回 503 —— 这一层的关闸与待办层同一个形状。
type ApprovalController struct {
	svc *service.ApprovalRequestService
}

// NewApprovalController 构造。
func NewApprovalController(svc *service.ApprovalRequestService) *ApprovalController {
	return &ApprovalController{svc: svc}
}

// RegisterRoutes 挂到已鉴权的 /api 组下（路径按 CLAUDE.md 的 /api/{domain}/{resource}）。
//
// 权限只加在裁决上：读口与待办池同一口径全池可见（坐席要能看懂自己名下那条待办的对象
// 是什么），而"能不能放行"是另一件事 —— 把两者混成一档会导致要么坐席看不到详情，
// 要么任何人都能批。
func (c *ApprovalController) RegisterRoutes(router *gin.RouterGroup) {
	g := router.Group("/approvals")
	{
		g.GET("/:id", c.Get)
		g.POST("/:id/decide", middleware.ManagerOrAdminMiddleware(), c.Decide)
	}
}

// approvalIDParamMaxLen 审批 id 长度上限（生成格式 apr_<unixnano>_<seq> 用不到 30 字符）。
// 收窄的理由与待办侧同源：下面会把 id 回显进提示里，无上限的回显就是免费的响应体放大器。
const approvalIDParamMaxLen = 64

// approvalNoteMaxLen 裁决意见的长度上限。
//
// 判错而不是夹断：这一列是审计里"为什么放/为什么拒"的原话，截一半留下的是
// 一句可能被读反的话（"折扣越权，但客户已经…" 被截成"折扣越权"）。
// 上限本身防的是"一次 POST 写进一兆文本"这种存储放大器。
const approvalNoteMaxLen = 2000

// approvalDetail 详情响应 = 审批行本体 + 服务端状态机的可去目标。
//
// 嵌指针而不是逐字段抄：抄一遍就等于把 resume_token 上那条 json:"-" 的防线换成
// "我记得没抄这一列"。AllowedTransitions 给的是服务端事实，前端不再自己抄一份
// "pending 才能点按钮"——状态机改了而前端没改，表现是按钮能点但必失败。
type approvalDetail struct {
	*model.ApprovalRequest
	AllowedTransitions []string `json:"allowed_transitions"`
}

// Get GET /api/approvals/:id
func (c *ApprovalController) Get(ctx *gin.Context) {
	if !c.svc.Available() {
		c.unavailable(ctx)
		return
	}
	id, ok := approvalIDParam(ctx)
	if !ok {
		return
	}
	row, err := c.svc.Get(ctx.Request.Context(), id)
	if err != nil {
		c.replyError(ctx, err)
		return
	}
	if row == nil {
		// 服务侧"读不到"是 (nil, nil)，404 这个判断归本层（同待办层）。
		response.Error(ctx, http.StatusNotFound, "审批不存在（id="+id+"）")
		return
	}
	response.Success(ctx, approvalDetail{row, c.svc.AllowedTransitions(row.Status)}, "ok")
}

// Decide POST /api/approvals/:id/decide  body: {"verdict":"approved|rejected","note":"…"}
func (c *ApprovalController) Decide(ctx *gin.Context) {
	if !c.svc.Available() {
		c.unavailable(ctx)
		return
	}
	id, ok := approvalIDParam(ctx)
	if !ok {
		return
	}
	decidedBy, ok := contextOperatorID(ctx)
	if !ok {
		response.Error(ctx, http.StatusUnauthorized,
			"裁决必须带操作者身份（未登录或会话里没有 user id）")
		return
	}
	// verdict 用字符串而不是 bool：批准与驳回是两个方向，不是一个开关。
	// 用 true/false 的话"没给字段"会静默变成"点了驳回"，那是一次凭空的拒绝。
	var body struct {
		Verdict string `json:"verdict"`
		Note    string `json:"note"`
	}
	if err := ctx.ShouldBindJSON(&body); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求体不是合法 JSON："+err.Error())
		return
	}
	if utf8.RuneCountInString(body.Note) > approvalNoteMaxLen {
		response.Error(ctx, http.StatusBadRequest, "裁决意见过长（上限 2000 字）")
		return
	}

	row, err := c.svc.Decide(ctx.Request.Context(), id,
		service.ApprovalVerdict(strings.TrimSpace(body.Verdict)), decidedBy, body.Note)
	if err != nil {
		// 409 单独立一条：落败方真正要回答的是"那到底批了没有"，
		// 所以把当前行一起带回（服务侧本来就返回了它），而不是让人再去刷一次列表。
		if errors.Is(err, service.ErrApprovalAlreadyDecided) {
			response.Error(ctx, http.StatusConflict, err.Error(), approvalDetail{row, c.svc.AllowedTransitions(row.Status)})
			return
		}
		c.replyError(ctx, err)
		return
	}
	response.Success(ctx, approvalDetail{row, c.svc.AllowedTransitions(row.Status)}, "ok")
}

// unavailable 503 出口：不附带任何审批形状的数据。
// "查不到"与"一次都没读到"是两句不同的话，后者不能伪装成前者。
func (c *ApprovalController) unavailable(ctx *gin.Context) {
	response.Error(ctx, http.StatusServiceUnavailable,
		"审批底座不可用（运行时未装配或缺少 DB 句柄），本次未读到任何记录")
}

// replyError 把服务侧 sentinel 翻成状态码。
func (c *ApprovalController) replyError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrApprovalInputInvalid):
		response.Error(ctx, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrApprovalNotFound):
		response.Error(ctx, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrApprovalIllegalTransition):
		// 刻意不给 409：409 在前端的读法是"这条被别人处理了，刷新一下"，
		// 而这一句是"状态机表里根本没有这条路"—— 刷新多少次都会拿到同一个答复。
		// 它与 ErrApprovalAlreadyDecided 分成两个 sentinel 的理由就在这里（见服务侧注释），
		// 合成一个就是把"表被改坏了"当成"没人处理审批"来查。
		logger.Errorf("[Approval] 状态机表与裁决请求不一致（改表/改代码所致）: %v", err)
		response.Error(ctx, http.StatusInternalServerError, "审批状态机配置异常，本次裁决未执行")
	default:
		logger.Warnf("[Approval] 未预期错误: %v", err)
		response.Error(ctx, http.StatusInternalServerError, "审批操作失败")
	}
}

func approvalIDParam(ctx *gin.Context) (string, bool) {
	id := strings.TrimSpace(ctx.Param("id"))
	if id == "" {
		response.Error(ctx, http.StatusBadRequest, "审批 id 不能为空")
		return "", false
	}
	if utf8.RuneCountInString(id) > approvalIDParamMaxLen {
		response.Error(ctx, http.StatusBadRequest, "审批 id 过长")
		return "", false
	}
	return id, true
}
