// followup.go 跟进提醒控制器（五层 L2，A11）
//
// 背景：FollowUpService 此前只有内部装配（旅程触发器/AI 工具端口经
// portcontract 调用），ListPending/GetDailyCalendar 等读写口零 HTTP 暴露，
// 销售的"今日跟进"在前端没有任何入口（改进清单 A11）。本控制器补五条端点。
//
// 服务来自 app.JourneyFollowUpForHTTP() 借草稿竖运行时（进程内内存态），
// 未装配为 nil ⇒ 503 而非空壳 200 —— 与 sales-workbench 同口径：没装配和
// 没数据是两件事，混成 200+空列表会被读成"你今天没跟进"。
package controller

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// FollowUpController 跟进提醒控制器
type FollowUpController struct {
	svc *service.FollowUpService
}

// NewFollowUpController 构造（服务由 router 从装配层借入，
// 与 NewSalesWorkbenchController 同一形态：控制器不自己 new 服务）。
func NewFollowUpController(svc *service.FollowUpService) *FollowUpController {
	return &FollowUpController{svc: svc}
}

// requireOwner 取并校验 owner_id：service 层 owner_id="" 等于不过滤全员，
// 端点又是"销售看自己的跟进"，缺参必须 400 而不是默默放大可见范围。
func requireOwner(ctx *gin.Context) (string, bool) {
	owner := ctx.Query("owner_id")
	if owner == "" {
		response.Error(ctx, http.StatusBadRequest, "参数错误")
		return "", false
	}
	return owner, true
}

// GetToday GET /api/followups/today?owner_id=&date=2006-01-02
//
// date 可选（缺省今天），格式错 400 —— 不做静默兜底，垃圾日期进读侧
// 只会产"空日历"这种被当成没跟进的假象。
func (c *FollowUpController) GetToday(ctx *gin.Context) {
	if c.svc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "跟进服务未装配")
		return
	}
	owner, ok := requireOwner(ctx)
	if !ok {
		return
	}
	date := time.Now()
	if raw := ctx.Query("date"); raw != "" {
		parsed, err := time.Parse("2006-01-02", raw)
		if err != nil {
			response.Error(ctx, http.StatusBadRequest, "参数错误")
			return
		}
		date = parsed
	}
	list := c.svc.GetDailyCalendar(ctx.Request.Context(), owner, date)
	response.Success(ctx, gin.H{
		"date": date.Format("2006-01-02"),
		"list": list,
	}, "ok")
}

// GetPending GET /api/followups/pending?owner_id=&limit=50
//
// limit 可选（缺省 50），给了就必须 1..500：跟 service 的 limit≤0=不过滤
// 语义对不上（那会把全员全量拉回来），所以在这里拦掉。
func (c *FollowUpController) GetPending(ctx *gin.Context) {
	if c.svc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "跟进服务未装配")
		return
	}
	owner, ok := requireOwner(ctx)
	if !ok {
		return
	}
	limit := 50
	if raw := ctx.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 500 {
			response.Error(ctx, http.StatusBadRequest, "参数错误")
			return
		}
		limit = n
	}
	list := c.svc.ListPending(ctx.Request.Context(), owner, limit)
	response.Success(ctx, gin.H{"list": list}, "ok")
}

// GetOverdue GET /api/followups/overdue?owner_id=
func (c *FollowUpController) GetOverdue(ctx *gin.Context) {
	if c.svc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "跟进服务未装配")
		return
	}
	owner, ok := requireOwner(ctx)
	if !ok {
		return
	}
	list := c.svc.ListOverdue(ctx.Request.Context(), owner)
	response.Success(ctx, gin.H{"list": list}, "ok")
}

// followupResults 合法跟进结果（与 service.FollowUpResultInfo 的键一一对应，
// 缺一个都会让"能完成但旅程不推进"的半成功出现）。
var followupResults = map[string]bool{
	"contacted":   true,
	"interested":  true,
	"quoted":      true,
	"converted":   true,
	"rejected":    true,
	"lost":        true,
	"no_response": true,
}

// writeErr 把 service 写口错误翻译成 HTTP 状态码：
// 不存在→404、已处理（状态守卫）→409、其余→500。用错误串匹配是因为
// service 层沿用 fmt.Errorf 无哨兵错误（存量形态，本卡不改其签名）。
func writeErr(ctx *gin.Context, err error, action string) {
	switch {
	case strings.Contains(err.Error(), "不存在"):
		response.Error(ctx, http.StatusNotFound, err.Error())
	case strings.Contains(err.Error(), "已处理"):
		response.Error(ctx, http.StatusConflict, err.Error())
	default:
		response.Error(ctx, http.StatusInternalServerError, action+"失败")
	}
}

// Complete POST /api/followups/:id/complete  body {result, note}
//
// result 可选（缺省 contacted，与 service.Complete 默认一致）、非法→400；
// 完成会推进旅程并记销售事件，因此 service 侧有"已处理"守卫，这里映射 409。
func (c *FollowUpController) Complete(ctx *gin.Context) {
	if c.svc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "跟进服务未装配")
		return
	}
	id := ctx.Param("id")
	if id == "" {
		response.Error(ctx, http.StatusBadRequest, "参数错误")
		return
	}
	var body struct {
		Result string `json:"result"`
		Note   string `json:"note"`
	}
	_ = ctx.ShouldBindJSON(&body) // 空体合法（result 缺省 contacted）
	result := body.Result
	if result == "" {
		result = string(service.FollowUpResultContacted)
	}
	if !followupResults[result] {
		response.Error(ctx, http.StatusBadRequest, "参数错误")
		return
	}
	err := c.svc.CompleteWithResult(ctx.Request.Context(), id, service.FollowUpResult(result), body.Note)
	if err != nil {
		writeErr(ctx, err, "完成跟进")
		return
	}
	response.Success(ctx, gin.H{"id": id, "status": "done"}, "ok")
}

// Cancel POST /api/followups/:id/cancel
func (c *FollowUpController) Cancel(ctx *gin.Context) {
	if c.svc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "跟进服务未装配")
		return
	}
	id := ctx.Param("id")
	if id == "" {
		response.Error(ctx, http.StatusBadRequest, "参数错误")
		return
	}
	if err := c.svc.Cancel(ctx.Request.Context(), id); err != nil {
		writeErr(ctx, err, "取消跟进")
		return
	}
	response.Success(ctx, gin.H{"id": id, "status": "canceled"}, "ok")
}
