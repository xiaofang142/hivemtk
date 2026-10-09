// order_draft.go 订单草稿面向销售的 HTTP 出口。
//
// 在此之前这条竖对外的全部可读面只有一项观察端点（/api/agent/order-drafts/stats，
// 回答"这个进程装没装配草稿运行时"）。草稿本身产生了、躺在库里或内存里，销售却
// 没有任何入口看到它、确认它、取消它 —— 于是"AI 提取到购买意向"这件事的终点
// 是一行没人读的内存，最后一公里是断的。
//
// 本层只做四件事，且刻意不做第五件（业务判定全在 service）：
//  1. **关闸**：服务缺席 ⇒ 每条端点 503，绝不回一份空列表。空列表是一句业务结论
//     （"今天没有待确认草稿"），拿"本进程压根没装配"去支撑它，销售就会停止处理
//     本该处理的单，而运维从 200 里看不出任何异常。
//  2. **身份**：每条出口都要落到具体的人 —— 确认人/取消人会被写进草稿的元数据，
//     那是"这单是谁收尾的"唯一凭据，取不到身份就 401 而不是以空串透传。
//  3. **形状**：查询参数与请求体 → service 的入参类型，外加 {list,total} 契约。
//  4. **状态码**：服务侧三类哨兵各对应一个语义（404/409/500），加 400/401/503。
//
// 待确认草稿按**全组共享池**开放读与处理，owner_id 只是过滤键、不是权限键，与统一待办
// 的池子同一模型（理由见 List 的注释：AI 提取的草稿压根没有归属人）。
//
// 待确认草稿按**全组共享池**开放读与处理，owner_id 只是过滤键不是权限键，与统一待办
// 的池子同一模型（理由见 List 的注释）。
package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

// orderDraftHTTPListLimit 待确认列表的默认条数上限。
//
// 底座的 listPending 只有上限、没有 offset（三种底座同一形状），所以这里不假装支持
// 翻页，只回答"第一眼看得过来的那几条"。排序在服务层已经按置信度→金额→到期时间定好，
// 截断取的是头部，也就是最该先处理的那几条。
const orderDraftHTTPListLimit = 50

// orderDraftHTTPListLimitMax 显式传 limit 时的上界：一次读全表的入口不该由 query 参数打开。
const orderDraftHTTPListLimitMax = 200

// orderDraftIDParamMaxLen 草稿 id 的长度上限（生成格式 draft_<unixnano>_<seq> 用不到 30 字符）。
// 收窄是因为下面会把 id 回显进提示里：无上限的回显等于给调用方一个免费的响应体放大器。
const orderDraftIDParamMaxLen = 64

// orderDraftTextMaxLen 取消理由与备注的长度上限（与待办域同一口径，两处都是给人读的文本）。
const orderDraftTextMaxLen = 2000

// orderDraftPendingConflictIndex 部分唯一索引名：同一客户 + 同一产品只允许一条 pending 草稿。
// 改产品名会撞它，撞了是业务结论（那条已经有一张待确认草稿）而不是服务器故障，
// 所以要按索引名点名翻成 409；其余 duplicate key 仍按 500 走，不能顺手都判成冲突。
const orderDraftPendingConflictIndex = "uq_order_draft_pending"

// OrderDraftController 草稿列表 / 确认 / 取消 / 编辑控制器。
//
// svc 允许是 nil（旗子 off 时 app.OrderDraftServiceForHTTP() 就返回 nil）：
// 路由照挂、每个请求走 unavailable —— 关闸的形态是"不给底座"，不是"不挂路由"，
// 后者在前端表现为 404，会被读成"这个功能没做"。
//
// flagEnv 由 router 从装配层带进来（controller 不 import app，那是向下依赖反向）。
// 它出现在 503 的提示里：值班看到 503 该去改的那一把旗子，名字不该要他去猜。
type OrderDraftController struct {
	svc     *service.OrderDraftService
	flagEnv string
}

// NewOrderDraftController 构造。
func NewOrderDraftController(svc *service.OrderDraftService, flagEnv string) *OrderDraftController {
	return &OrderDraftController{svc: svc, flagEnv: flagEnv}
}

// RegisterRoutes 挂到已鉴权的 /api 组下（路径按 /api/manage/{resource} 规范）。
func (c *OrderDraftController) RegisterRoutes(router *gin.RouterGroup) {
	g := router.Group("/manage/order-drafts")
	{
		g.GET("", c.List)
		g.GET("/:id", c.Get)
		g.PATCH("/:id", c.Edit)
		g.POST("/:id/confirm", c.Confirm)
		g.POST("/:id/cancel", c.Cancel)
	}
}

// List GET /api/manage/order-drafts
//
// 三种视图靠参数选，而不是三条路径：读法完全相同（都走服务层现有的读口），
// 差别只在过滤键。缺省是"全部待确认草稿"，即整组共享的那一池。
//
//	?owner_id=<id>    只看某个人名下（过滤键；all|* 等价于不给）
//	?customer_id=<id> 该客户的全部草稿（含已确认/已取消/已过期的历史）
//	?limit=<1..200>   仅对待确认视图生效
//
// 缺省为什么不设成"我名下的"：AI 提取出的草稿由服务层填 owner_id="system"（编排器建草稿
// 时不带坐席 id），按人过滤会把这一整批草稿从每个人的列表里抹掉，只剩 stats 端点上
// 一个计数。按人看自己那份是销售工作台（SalesWorkbenchService）的职责；
// 这条 HTTP 出口要回答的是"池子里现在有哪些单等着人处理"。
func (c *OrderDraftController) List(ctx *gin.Context) {
	if c.svc == nil {
		c.unavailable(ctx)
		return
	}
	if _, ok := orderDraftOperator(ctx, "查看草稿列表"); !ok {
		return
	}

	if customerID := strings.TrimSpace(ctx.Query("customer_id")); customerID != "" {
		c.listByCustomer(ctx, customerID)
		return
	}

	ownerID := strings.TrimSpace(ctx.Query("owner_id"))
	if ownerID == "all" || ownerID == "*" {
		ownerID = ""
	}
	limit, ok := orderDraftListLimit(ctx)
	if !ok {
		return
	}
	list, err := c.svc.ListPending(ctx.Request.Context(), ownerID, limit)
	if err != nil {
		c.replyError(ctx, err)
		return
	}
	response.Success(ctx, gin.H{
		"list":  list,
		"total": len(list),
		"limit": limit,
		// truncated 而不是 total：底座这个读口没有 offset，也没有配套的行数统计，
		// 所以 total 只到"本次返回了几条"，够不够要看 truncated 才知道后面还有没有。
		"truncated":  len(list) == limit,
		"view":       "pool_pending",
		"owner_id":   ownerID,
		"pending":    true,
		"store_kind": c.svc.StoreKind(),
	}, "ok")
}

// listByCustomer 客户维度的读法：服务层这个口返回该客户的全部草稿（含终态），
// 所以响应里的 pending 必须是 false —— 前端把它当待办计数用的话，
// 一张已取消的草稿会被算成"还有一单要处理"。
func (c *OrderDraftController) listByCustomer(ctx *gin.Context, customerID string) {
	list, err := c.svc.ListByCustomer(ctx.Request.Context(), customerID)
	if err != nil {
		c.replyError(ctx, err)
		return
	}
	response.Success(ctx, gin.H{
		"list":        list,
		"total":       len(list),
		"view":        "customer",
		"customer_id": customerID,
		"pending":     false,
		"store_kind":  c.svc.StoreKind(),
	}, "ok")
}

// Get GET /api/manage/order-drafts/:id
func (c *OrderDraftController) Get(ctx *gin.Context) {
	if c.svc == nil {
		c.unavailable(ctx)
		return
	}
	if _, ok := orderDraftOperator(ctx, "查看草稿"); !ok {
		return
	}
	draft, ok := c.loadDraft(ctx)
	if !ok {
		return
	}
	response.Success(ctx, draft, "ok")
}

// Confirm POST /api/manage/order-drafts/:id/confirm —— 一键确认成单。
//
// 先读一次草稿是为了把"不存在"报成 404 而不是 500；并发不在这里管：
// 读到 pending 不等于确认时还 pending，"同一意向只建一张单"靠的是 store 的
// mutateIfPending，本层不重复实现一遍（重复的那一份还更弱）。
func (c *OrderDraftController) Confirm(ctx *gin.Context) {
	if c.svc == nil {
		c.unavailable(ctx)
		return
	}
	operator, ok := orderDraftOperator(ctx, "确认草稿")
	if !ok {
		return
	}
	draft, ok := c.loadDraft(ctx)
	if !ok {
		return
	}

	result, err := c.svc.Confirm(ctx.Request.Context(), draft.ID, operator)
	if err != nil {
		if result != nil && result.Draft != nil && result.Draft.Status == service.DraftStatusConfirmed {
			// 草稿已翻成 confirmed、订单却没建出来。这条必须是 500 且把两件事都说清：
			// 回 409 会让销售再点一次（那次可能真建出一张单，于是同一意向两张单），
			// 只回"确认失败"会让人以为草稿还在待确认池里。
			logger.Warnf("[OrderDraft] ❌ 草稿 %s 已置为 confirmed 但订单创建失败，需人工补建：%v", draft.ID, err)
			response.Error(ctx, http.StatusInternalServerError,
				"订单创建失败，但草稿已置为 confirmed（不会自动重试，需人工补建订单）: "+err.Error())
			return
		}
		c.replyError(ctx, err)
		return
	}

	message := "ok"
	if result.OrderProvisional {
		// 临时订单号不能只靠 data 里的布尔字段说话：前端多半直接展示 order_id。
		// 这一句让"这个号在 orders 表里不存在"跟着值一起被读走。
		message = "草稿已确认，但未拿到订单服务 ⇒ 订单号是本进程临时生成的，orders 表里没有这一行"
	}
	response.Success(ctx, result, message)
}

// Cancel POST /api/manage/order-drafts/:id/cancel  body: {"reason":"…"}
//
// reason 必填：取消理由是"哪些产品/价格/阶段容易被取消"这一整套分析唯一的原料，
// 空串让它整条失效。用指针区分"没给这个字段"与"给了空串"，两种都是 400，
// 但一句明确的原话比"缺失字段"更好定位。
func (c *OrderDraftController) Cancel(ctx *gin.Context) {
	if c.svc == nil {
		c.unavailable(ctx)
		return
	}
	operator, ok := orderDraftOperator(ctx, "取消草稿")
	if !ok {
		return
	}
	draft, ok := c.loadDraft(ctx)
	if !ok {
		return
	}

	var body struct {
		Reason *string `json:"reason"`
	}
	if err := ctx.ShouldBindJSON(&body); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求体不是合法 JSON："+err.Error())
		return
	}
	reason := ""
	if body.Reason != nil {
		reason = strings.TrimSpace(*body.Reason)
	}
	if reason == "" {
		response.Error(ctx, http.StatusBadRequest, "取消草稿必须给理由（reason），它同时是取消原因分析的原料")
		return
	}
	if utf8.RuneCountInString(reason) > orderDraftTextMaxLen {
		response.Error(ctx, http.StatusBadRequest,
			"取消理由过长（"+strconv.Itoa(utf8.RuneCountInString(reason))+" 字符，上限 "+
				strconv.Itoa(orderDraftTextMaxLen)+"）")
		return
	}

	if err := c.svc.Cancel(ctx.Request.Context(), draft.ID, reason, operator); err != nil {
		c.replyError(ctx, err)
		return
	}
	c.replyDraftAfterWrite(ctx, draft.ID)
}

// Edit PATCH /api/manage/order-drafts/:id —— 确认前改价/改数量/改产品名/加备注。
//
// 四个字段全是指针（service.DraftUpdates 的形状），"没给"与"给了零值"在这里分开：
// 服务层对 nil 什么都不做，对 quantity<=0 也什么都不做 —— 后者会让前端传 0 想清零时
// 拿到一句 200 而什么都没变，所以这类值在本层判 400，不透传给服务层静默忽略。
func (c *OrderDraftController) Edit(ctx *gin.Context) {
	if c.svc == nil {
		c.unavailable(ctx)
		return
	}
	if _, ok := orderDraftOperator(ctx, "编辑草稿"); !ok {
		return
	}
	draft, ok := c.loadDraft(ctx)
	if !ok {
		return
	}

	var updates service.DraftUpdates
	if err := ctx.ShouldBindJSON(&updates); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求体不是合法 JSON："+err.Error())
		return
	}
	if updates.ProductName == nil && updates.Quantity == nil && updates.UnitPrice == nil && updates.Note == nil {
		response.Error(ctx, http.StatusBadRequest,
			"没有任何要改的字段（可改的只有 product_name / quantity / unit_price / note）")
		return
	}
	if msg, bad := orderDraftUpdatesRejected(&updates); bad {
		response.Error(ctx, http.StatusBadRequest, msg)
		return
	}

	if err := c.svc.Edit(ctx.Request.Context(), draft.ID, updates); err != nil {
		c.replyError(ctx, err)
		return
	}
	c.replyDraftAfterWrite(ctx, draft.ID)
}

// replyDraftAfterWrite 写动作成功后回读整条草稿。
//
// 回读而不是回"200 空体"：改价要看到重算后的 total_amount，取消要看到落库的
// cancel_reason —— 前端拿这两个数才能当场自证"改到位了"。回读失败按故障报（500），
// 但已经生效的写不会因此被撤回，所以 message 里要说清"改动已生效"。
func (c *OrderDraftController) replyDraftAfterWrite(ctx *gin.Context, id string) {
	fresh, err := c.svc.GetByID(ctx.Request.Context(), id)
	if err != nil {
		logger.Warnf("[OrderDraft] 写成功但回读失败（草稿 %s）：%v", id, err)
		response.Error(ctx, http.StatusInternalServerError, "改动已生效，但回读草稿失败："+err.Error())
		return
	}
	response.Success(ctx, fresh, "ok")
}

// loadDraft 解析 id 并读一次草稿：不存在 ⇒ 404，读不动 ⇒ 走 replyError。
func (c *OrderDraftController) loadDraft(ctx *gin.Context) (*service.OrderDraft, bool) {
	id, ok := orderDraftIDParam(ctx)
	if !ok {
		return nil, false
	}
	draft, err := c.svc.GetByID(ctx.Request.Context(), id)
	if err != nil {
		c.replyError(ctx, err)
		return nil, false
	}
	if draft == nil {
		// 服务层"读不到"是 (nil, nil)，404 这个判断归本层（与待办控制器同一分工）。
		response.Error(ctx, http.StatusNotFound, "订单草稿不存在（id="+id+"）")
		return nil, false
	}
	return draft, true
}

func orderDraftOperator(ctx *gin.Context, action string) (string, bool) {
	operator, ok := contextOperatorID(ctx)
	if !ok {
		response.Error(ctx, http.StatusUnauthorized,
			action+" 必须带操作者身份（未登录或会话里没有 user id）")
		return "", false
	}
	return operator, true
}

func orderDraftIDParam(ctx *gin.Context) (string, bool) {
	id := strings.TrimSpace(ctx.Param("id"))
	if id == "" {
		response.Error(ctx, http.StatusBadRequest, "草稿 id 不能为空")
		return "", false
	}
	if utf8.RuneCountInString(id) > orderDraftIDParamMaxLen {
		response.Error(ctx, http.StatusBadRequest, "草稿 id 过长")
		return "", false
	}
	return id, true
}

// orderDraftListLimit 解析 limit。给了非法值直接 400，不做"那就用默认值"的静默纠正：
// 前端把 limit 传成 0 是 bug，替它兜住只会让列表永远停在默认条数而没人发现。
func orderDraftListLimit(ctx *gin.Context) (int, bool) {
	raw := strings.TrimSpace(ctx.Query("limit"))
	if raw == "" {
		return orderDraftHTTPListLimit, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > orderDraftHTTPListLimitMax {
		response.Error(ctx, http.StatusBadRequest,
			"limit 必须是 1 到 "+strconv.Itoa(orderDraftHTTPListLimitMax)+" 的整数")
		return 0, false
	}
	return n, true
}

// orderDraftUpdatesRejected 逐项校验改价请求。返回 bad=true 时 msg 已可回给前端。
func orderDraftUpdatesRejected(u *service.DraftUpdates) (string, bool) {
	if u.ProductName != nil {
		name := strings.TrimSpace(*u.ProductName)
		if name == "" {
			return "product_name 不能是空串（要改产品名就给新名字）", true
		}
		*u.ProductName = name
	}
	if u.Quantity != nil && *u.Quantity < 1 {
		return "quantity 至少为 1（要取消这张草稿请走 cancel，不是把数量改成 0）", true
	}
	if u.UnitPrice != nil && *u.UnitPrice < 0 {
		return "unit_price 不能为负（负单价会做出现金流对不上的单）", true
	}
	if u.Note != nil && utf8.RuneCountInString(*u.Note) > orderDraftTextMaxLen {
		return "note 过长（上限 " + strconv.Itoa(orderDraftTextMaxLen) + " 字符）", true
	}
	return "", false
}

// unavailable 503 出口：不带任何数据字段。
//
// 空列表与 {"total":0} 都是一句业务结论（"今天没有待确认草稿"），
// 而此刻的事实是"一次都没读到"：旗子 off 时根本没有草稿运行时，
// order_drafts 连写入方都没有。两种读法在销售手上的动作完全相反。
func (c *OrderDraftController) unavailable(ctx *gin.Context) {
	response.Error(ctx, http.StatusServiceUnavailable,
		"订单草稿底座不可用（本进程未装配草稿运行时，开关 "+c.flagEnv+
			" 为 off 或值无法识别；档位只在装配期读一次，改完须重启）")
}

// replyError 把服务层的哨兵翻成状态码。
//
// 404 与 409 必须分开：前者前端该把这一条从列表里摘掉，后者该刷新（这条已被别人
// 确认或取消了）。500 留给真正的意外 —— 拿 500 当"重试一下"会让前端在底座真挂时打圈。
func (c *OrderDraftController) replyError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrOrderDraftNotFound):
		response.Error(ctx, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrOrderDraftNotPending), errors.Is(err, service.ErrOrderDraftExpired):
		response.Error(ctx, http.StatusConflict, err.Error())
	case isOrderDraftPendingConflict(err):
		// 部分唯一索引（同一客户 + 同一产品只能有一条 pending）撞车：
		// 这是"那个产品已经有一张待确认草稿了"的业务结论，不是服务器故障。
		response.Error(ctx, http.StatusConflict,
			"该客户名下同一产品已有一张待确认草稿，改成这个名字会与之冲突："+err.Error())
	default:
		logger.Warnf("[OrderDraft] 未预期错误: %v", err)
		response.Error(ctx, http.StatusInternalServerError, "订单草稿操作失败")
	}
}

func isOrderDraftPendingConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "duplicate key") &&
		strings.Contains(msg, orderDraftPendingConflictIndex)
}
