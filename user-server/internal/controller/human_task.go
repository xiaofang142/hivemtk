// human_task.go 统一待办的 HTTP 出口（T-P3-03 / N-9，C3 裁定的"分离视图"共用同一套读口）。
//
// 本层只负责三件事，且刻意不做第四件：
//  1. **身份**：动作类端点必须落到具体的人（assignee_user_id 是坐席工作量的来源），
//     取不到身份就 401，而不是"以空串处理"—— 空串在库里等于"没人认领"，
//     一次凭空成功的认领会把那条待办从池子里摘掉而无人负责。
//  2. **形状**：查询参数 → service.HumanTaskListQuery，以及 CLAUDE.md 那套
//     {code,data,message} / {list,total} 契约。
//  3. **状态码**：服务侧五个 sentinel 各对应一个语义（400/403/404/409/503）。
//
// 校验、幂等、状态机全在服务层，这里一律不重写一遍：本层再"顺手兜一下"就会出现
// 两个口径（进程内调用方绕过 HTTP 校验），而 AC② 的幂等恰恰要求两条路径同一判据。
package controller

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

// HumanTaskController 待办中心 / 坐席收件箱共用控制器。
//
// svc 允许是 nil（human_task 未装配）：所有出口都会走 Available() 回 503，
// 这就是本卡天然的关闸 —— 不注入服务 ⇒ 端点在、答案诚实。
type HumanTaskController struct {
	svc *service.HumanTaskService
}

// NewHumanTaskController 构造。
func NewHumanTaskController(svc *service.HumanTaskService) *HumanTaskController {
	return &HumanTaskController{svc: svc}
}

// RegisterRoutes 挂到已鉴权的 /api 组下（路径按 CLAUDE.md 的 /api/{domain}/{resource}）。
func (c *HumanTaskController) RegisterRoutes(router *gin.RouterGroup) {
	g := router.Group("/human-tasks")
	{
		g.GET("", c.List)
		g.GET("/counts", c.Counts)
		g.GET("/:id", c.Get)
		g.POST("/:id/claim", c.Claim)
		g.POST("/:id/release", c.Release)
		g.POST("/:id/complete", c.Complete)
		g.POST("/:id/cancel", c.Cancel)
	}
}

// humanTaskHTTPPageSize 未显式给分页时的页大小。
//
// 上限（200）在仓储里夹，这里只给默认值：一次读全表那条路（Page/PageSize 都不给）
// 只服务进程内调用，HTTP 侧永远必须分页 —— 待办表是会长大的，不设默认就是
// "列表页某天后突然把整张表拉回来"。这就是仓储注释里点名的那处断言。
const humanTaskHTTPPageSize = 20

// humanTaskIDParamMaxLen 待办 id 的长度上限（生成格式 ht_<unixnano>_<seq> 用不到 30 字符）。
// 收窄是为了下面把 id 回显进提示里：无上限的回显等于给调用方一个免费的响应体放大器。
const humanTaskIDParamMaxLen = 64

// List GET /api/human-tasks
func (c *HumanTaskController) List(ctx *gin.Context) {
	if !c.svc.Available() {
		c.unavailable(ctx)
		return
	}
	q, ok := humanTaskQueryFromRequest(ctx)
	if !ok {
		return //  helper 已经回过 400/401
	}
	list, total, err := c.svc.List(ctx.Request.Context(), q)
	if err != nil {
		c.replyError(ctx, err)
		return
	}
	response.SuccessWithList(ctx, list, total)
}

// Counts GET /api/human-tasks/counts —— 未读数聚合（AC③）与每类逾期数（AC④）。
func (c *HumanTaskController) Counts(ctx *gin.Context) {
	if !c.svc.Available() {
		c.unavailable(ctx)
		return
	}
	counts, err := c.svc.Counts(ctx.Request.Context())
	if err != nil {
		c.replyError(ctx, err)
		return
	}
	response.Success(ctx, counts, "ok")
}

// Get GET /api/human-tasks/:id
func (c *HumanTaskController) Get(ctx *gin.Context) {
	if !c.svc.Available() {
		c.unavailable(ctx)
		return
	}
	id, ok := humanTaskIDParam(ctx)
	if !ok {
		return
	}
	task, err := c.svc.Get(ctx.Request.Context(), id)
	if err != nil {
		c.replyError(ctx, err)
		return
	}
	if task == nil {
		// 服务侧"读不到"是 (nil, nil)，404 这个判断归本层。
		response.Error(ctx, http.StatusNotFound, "待办不存在（id="+id+"）")
		return
	}
	response.Success(ctx, task, "ok")
}

// Claim POST /api/human-tasks/:id/claim（仅会话类，服务侧拒其余两类）。
func (c *HumanTaskController) Claim(ctx *gin.Context) {
	c.doAction(ctx, "认领", c.svc.Claim)
}

// Release POST /api/human-tasks/:id/release（仅当前认领人）。
func (c *HumanTaskController) Release(ctx *gin.Context) {
	c.doAction(ctx, "释放", c.svc.Release)
}

// Complete POST /api/human-tasks/:id/complete（三类通用：坐席处理完 / 审批裁决 / 催收升级处理完）。
func (c *HumanTaskController) Complete(ctx *gin.Context) {
	c.doAction(ctx, "完成", c.svc.Complete)
}

// Cancel POST /api/human-tasks/:id/cancel  body: {"reason":"…"}（必须给理由）。
func (c *HumanTaskController) Cancel(ctx *gin.Context) {
	if !c.svc.Available() {
		c.unavailable(ctx)
		return
	}
	id, ok := humanTaskIDParam(ctx)
	if !ok {
		return
	}
	operator, ok := contextOperatorID(ctx)
	if !ok {
		response.Error(ctx, http.StatusUnauthorized, "撤销必须带操作者身份（未登录或会话里没有 user id）")
		return
	}
	// reason 用指针区分"没给这个字段"与"给了空串"：两种在服务侧都是 400，
	// 但一句缺失字段的原样错误比"必须给 reason"更难定位，所以这里自己判。
	var body struct {
		Reason *string `json:"reason"`
	}
	if err := ctx.ShouldBindJSON(&body); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求体不是合法 JSON："+err.Error())
		return
	}
	reason := ""
	if body.Reason != nil {
		reason = *body.Reason
	}
	task, err := c.svc.Cancel(ctx.Request.Context(), id, operator, reason)
	if err != nil {
		c.replyError(ctx, err)
		return
	}
	response.Success(ctx, task, "ok")
}

// humanTaskActionFn 三个无请求体动作的共同签名（service.Claim/Release/Complete 去掉接收者）。
type humanTaskActionFn func(ctx context.Context, id, operator string) (*model.HumanTask, error)

func (c *HumanTaskController) doAction(ctx *gin.Context, action string, fn humanTaskActionFn) {
	if !c.svc.Available() {
		c.unavailable(ctx)
		return
	}
	id, ok := humanTaskIDParam(ctx)
	if !ok {
		return
	}
	operator, ok := contextOperatorID(ctx)
	if !ok {
		response.Error(ctx, http.StatusUnauthorized,
			action+" 必须带操作者身份（未登录或会话里没有 user id）")
		return
	}
	task, err := fn(ctx.Request.Context(), id, operator)
	if err != nil {
		c.replyError(ctx, err)
		return
	}
	response.Success(ctx, task, "ok")
}

// unavailable 503 出口：不带任何数据字段。
//
// 空列表与 {"total_open":0} 都是一句业务结论（"人工清完了"），
// 而此刻的事实是"一次都没读到"。两种读法在值班手上的动作完全相反。
func (c *HumanTaskController) unavailable(ctx *gin.Context) {
	response.Error(ctx, http.StatusServiceUnavailable,
		"待办底座不可用（未装配或缺少 DB 句柄），本次未读到任何计数")
}

// replyError 把服务/仓储的 sentinel 翻成状态码。
//
// 409 与 500 必须分开：前者前端该刷新列表（这件事已被别人处理了），
// 后者不该 —— 拿 500 当"重试一下"会让前端在底座真挂的时候无限打圈。
func (c *HumanTaskController) replyError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrHumanTaskInputInvalid): // 与 repository 同一 sentinel（别名）
		response.Error(ctx, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrHumanTaskNotFound):
		response.Error(ctx, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrHumanTaskNotHolder):
		response.Error(ctx, http.StatusForbidden, err.Error())
	case errors.Is(err, service.ErrHumanTaskTransition),
		errors.Is(err, service.ErrHumanTaskLost),
		errors.Is(err, service.ErrHumanTaskOpenConflict):
		response.Error(ctx, http.StatusConflict, err.Error())
	default:
		logger.Warnf("[HumanTask] 未预期错误: %v", err)
		response.Error(ctx, http.StatusInternalServerError, "待办操作失败")
	}
}

// humanTaskQueryFromRequest 解析列表查询参数。返回 ok=false 表示已经回过错误响应。
func humanTaskQueryFromRequest(ctx *gin.Context) (service.HumanTaskListQuery, bool) {
	var q service.HumanTaskListQuery

	// kind/status 可重复给（?kind=a&kind=b）：坐席收件箱要"会话 + 催收"一类合并视图。
	// 空值丢掉而不是透传：?kind= 是"没选筛选器"的前端常态，把它判成 400 会把列表页打不开，
	// 而未知 kind（?kind=bogus）仍会一路传到仓储报错 —— 那条是业务结论，必须红。
	q.Kinds = humanTaskQueryValues(ctx, "kind")
	q.Statuses = humanTaskQueryValues(ctx, "status")

	switch assignee := strings.TrimSpace(ctx.Query("assignee")); {
	case assignee == "":
	case strings.EqualFold(assignee, "me"):
		// 取登录态身份，而不是信前端把自己 id 拼进 URL。
		op, ok := contextOperatorID(ctx)
		if !ok {
			response.Error(ctx, http.StatusUnauthorized, "assignee=me 需要登录态身份")
			return q, false
		}
		q.AssigneeUserID = op
	default:
		// 按他人 id 过滤不算越权：这套读口本来就是全池可见（待办中心与收件箱共用），
		// 未过滤的列表里已经含有别人名下的行。
		q.AssigneeUserID = assignee
	}

	page, size, ok := humanTaskPaging(ctx)
	if !ok {
		return q, false
	}
	q.Page, q.PageSize = page, size
	return q, true
}

func humanTaskQueryValues(ctx *gin.Context, key string) []string {
	var out []string
	for _, v := range ctx.QueryArray(key) {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// humanTaskPaging 解析分页。给了非法值直接 400，不做"那就当第一页"的静默纠正：
// 前端把 page 传成 0 是 bug，替它兜住只会让翻页永远停在第一页而没人发现。
func humanTaskPaging(ctx *gin.Context) (int, int, bool) {
	page, size := 1, humanTaskHTTPPageSize
	rawPage, rawSize := strings.TrimSpace(ctx.Query("page")), strings.TrimSpace(ctx.Query("page_size"))
	if rawPage != "" {
		n, err := strconv.Atoi(rawPage)
		if err != nil || n < 1 {
			response.Error(ctx, http.StatusBadRequest, "page 必须是从 1 起的整数")
			return 0, 0, false
		}
		page = n
	}
	if rawSize != "" {
		n, err := strconv.Atoi(rawSize)
		if err != nil || n < 1 {
			response.Error(ctx, http.StatusBadRequest, "page_size 必须是正整数")
			return 0, 0, false
		}
		size = n
	}
	return page, size, true
}

func humanTaskIDParam(ctx *gin.Context) (string, bool) {
	id := strings.TrimSpace(ctx.Param("id"))
	if id == "" {
		response.Error(ctx, http.StatusBadRequest, "待办 id 不能为空")
		return "", false
	}
	if utf8.RuneCountInString(id) > humanTaskIDParamMaxLen {
		response.Error(ctx, http.StatusBadRequest, "待办 id 过长")
		return "", false
	}
	return id, true
}
