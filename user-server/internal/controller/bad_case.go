// bad_case.go G-2 Bad Case 闭环的 HTTP 出口（新规划任务清单 T-P8-03）。
//
// 本层只负责三件事（与 human_task 控制器同一分工，刻意不做第四件）：
//  1. **身份**：打标与撤销必须落到具体的人（labeler_id 是"这条结论谁下的"的唯一出处，
//     评测集会把这一列带下去）。取不到身份回 401，而不是"以空串处理"。
//  2. **形状**：查询参数 → service.BadCaseListQuery，以及 {code,data,message} /
//     {list,total} 契约。
//  3. **状态码**：服务侧 sentinel 各对应一个语义（400/404/409/503）。
//
// 门槛判断、状态机、幂等键全在服务层，这里一律不重写一遍。
package controller

import (
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

// BadCaseController 坏例队列与评测集导出的共用控制器。
//
// svc 允许是 nil（本竖未装配）：所有出口走 Available() 回 503 —— 这就是本卡天然的关闸。
type BadCaseController struct {
	svc *service.BadCaseService
}

// NewBadCaseController 构造。
func NewBadCaseController(svc *service.BadCaseService) *BadCaseController {
	return &BadCaseController{svc: svc}
}

// RegisterRoutes 挂到已鉴权的 /api 组下（路径按 CLAUDE.md 的 /api/{domain}/{resource}）。
func (c *BadCaseController) RegisterRoutes(router *gin.RouterGroup) {
	g := router.Group("/bad-cases")
	{
		g.GET("", c.List)
		g.GET("/stats", c.Stats)
		g.GET("/taxonomy", c.Taxonomy)
		g.GET("/:id", c.Get)
		g.POST("", c.CreateManual)
		g.POST("/:id/label", c.Label)
		g.POST("/:id/dismiss", c.Dismiss)
		g.POST("/export", c.Export)
	}
}

// badCaseIDParamMaxLen id 长度上限（生成格式 bc_<unixnano>_<seq> 用不到 30 字符）。
// 收窄是因为下面会把 id 回显进提示里：无上限的回显等于给调用方一个免费的响应体放大器。
const badCaseIDParamMaxLen = 64

// List GET /api/bad-cases
func (c *BadCaseController) List(ctx *gin.Context) {
	if !c.svc.Available() {
		c.unavailable(ctx)
		return
	}
	q, ok := badCaseQueryFromRequest(ctx)
	if !ok {
		return // helper 已经回过 400/401
	}
	list, total, err := c.svc.List(ctx.Request.Context(), q)
	if err != nil {
		c.replyError(ctx, err)
		return
	}
	response.SuccessWithList(ctx, list, total)
}

// Stats GET /api/bad-cases/stats —— 队列读数（已判率是本卡北极星）。
func (c *BadCaseController) Stats(ctx *gin.Context) {
	if !c.svc.Available() {
		c.unavailable(ctx)
		return
	}
	stats, err := c.svc.Stats(ctx.Request.Context())
	if err != nil {
		c.replyError(ctx, err)
		return
	}
	response.Success(ctx, stats, "ok")
}

// Taxonomy GET /api/bad-cases/taxonomy —— 把来源/状态/类目/责任层四张表交回前端。
//
// 为什么要一条端点：下拉框里的类目一旦在前端抄一份，库里加一个类目就会出现
// "库里判得出来、页面上选不到"，而这条链的下游是评测集，选不到就等于统计里少一层。
func (c *BadCaseController) Taxonomy(ctx *gin.Context) {
	response.Success(ctx, gin.H{
		"sources":    model.BadCaseSources,
		"statuses":   model.BadCaseStatuses,
		"labels":     model.BadCaseLabels,
		"fix_layers": model.BadCaseFixLayers,
		"label_layer": func() map[string]string {
			out := make(map[string]string, len(model.BadCaseLabelFixLayer))
			for k, v := range model.BadCaseLabelFixLayer {
				out[k] = v
			}
			return out
		}(),
	}, "ok")
}

// Get GET /api/bad-cases/:id
func (c *BadCaseController) Get(ctx *gin.Context) {
	if !c.svc.Available() {
		c.unavailable(ctx)
		return
	}
	id, ok := badCaseIDParam(ctx)
	if !ok {
		return
	}
	row, err := c.svc.Get(ctx.Request.Context(), id)
	if err != nil {
		c.replyError(ctx, err)
		return
	}
	if row == nil {
		// 服务侧"读不到"是 (nil, nil)，404 这个判断归本层。
		response.Error(ctx, http.StatusNotFound, "坏例记录不存在（id="+id+"）")
		return
	}
	response.Success(ctx, row, "ok")
}

// CreateManual POST /api/bad-cases —— 坐席补录一条（不等置信度判它低质）。
func (c *BadCaseController) CreateManual(ctx *gin.Context) {
	if !c.svc.Available() {
		c.unavailable(ctx)
		return
	}
	operator, ok := contextOperatorID(ctx)
	if !ok {
		response.Error(ctx, http.StatusUnauthorized, "补录必须带操作者身份（未登录或会话里没有 user id）")
		return
	}
	var body struct {
		SessionID      string  `json:"session_id"`
		MessageID      string  `json:"message_id"`
		IntentType     string  `json:"intent_type"`
		QueryText      string  `json:"query_text"`
		AnswerText     string  `json:"answer_text"`
		Confidence     float64 `json:"confidence"`
		RetrievedCount int     `json:"retrieved_count"`
		Reason         string  `json:"reason"`
	}
	if err := ctx.ShouldBindJSON(&body); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求体不是合法 JSON："+err.Error())
		return
	}
	row, err := c.svc.MarkManual(ctx.Request.Context(), service.BadCaseMarkInput{
		SessionID:      strings.TrimSpace(body.SessionID),
		MessageID:      strings.TrimSpace(body.MessageID),
		IntentType:     strings.TrimSpace(body.IntentType),
		QueryText:      body.QueryText,
		AnswerText:     body.AnswerText,
		Confidence:     body.Confidence,
		RetrievedCount: body.RetrievedCount,
		MarkReason:     body.Reason,
	}, operator)
	if err != nil {
		c.replyError(ctx, err)
		return
	}
	response.Success(ctx, row, "ok")
}

// Label POST /api/bad-cases/:id/label  body: {"label":"…","note":"…"}
func (c *BadCaseController) Label(ctx *gin.Context) {
	if !c.svc.Available() {
		c.unavailable(ctx)
		return
	}
	id, ok := badCaseIDParam(ctx)
	if !ok {
		return
	}
	operator, ok := contextOperatorID(ctx)
	if !ok {
		response.Error(ctx, http.StatusUnauthorized, "打标必须带操作者身份（未登录或会话里没有 user id）")
		return
	}
	var body struct {
		Label string  `json:"label"`
		Note  *string `json:"note"`
	}
	if err := ctx.ShouldBindJSON(&body); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求体不是合法 JSON："+err.Error())
		return
	}
	note := ""
	if body.Note != nil {
		note = *body.Note
	}
	row, err := c.svc.Label(ctx.Request.Context(), id, operator, strings.TrimSpace(body.Label), note)
	if err != nil {
		c.replyError(ctx, err)
		return
	}
	response.Success(ctx, row, "ok")
}

// Dismiss POST /api/bad-cases/:id/dismiss  body: {"reason":"…"}（必须给理由）
func (c *BadCaseController) Dismiss(ctx *gin.Context) {
	if !c.svc.Available() {
		c.unavailable(ctx)
		return
	}
	id, ok := badCaseIDParam(ctx)
	if !ok {
		return
	}
	operator, ok := contextOperatorID(ctx)
	if !ok {
		response.Error(ctx, http.StatusUnauthorized, "撤销必须带操作者身份（未登录或会话里没有 user id）")
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
		reason = *body.Reason
	}
	row, err := c.svc.Dismiss(ctx.Request.Context(), id, operator, reason)
	if err != nil {
		c.replyError(ctx, err)
		return
	}
	response.Success(ctx, row, "ok")
}

// Export POST /api/bad-cases/export  body: {"labels":[…],"limit":N} → 一份评测集
func (c *BadCaseController) Export(ctx *gin.Context) {
	if !c.svc.Available() {
		c.unavailable(ctx)
		return
	}
	var body struct {
		Labels []string `json:"labels"`
		Limit  int      `json:"limit"`
	}
	// 请求体可空：一次"全选导出"就是 POST 无 body。只有给了却不合法才报错。
	if ctx.Request.ContentLength != 0 {
		if err := ctx.ShouldBindJSON(&body); err != nil {
			response.Error(ctx, http.StatusBadRequest, "请求体不是合法 JSON："+err.Error())
			return
		}
	}
	if body.Limit < 0 {
		response.Error(ctx, http.StatusBadRequest, "limit 不能为负")
		return
	}
	set, err := c.svc.ExportEvalSet(ctx.Request.Context(), body.Labels, body.Limit)
	if err != nil {
		c.replyError(ctx, err)
		return
	}
	response.Success(ctx, set, "ok")
}

// unavailable 503 出口：不带任何数据字段。
//
// 空列表与 {"by_status":{…:0}} 都是一句业务结论（"队列清完了"），
// 而此刻的事实是"一次都没读到"。两种读法在值班手上的动作完全相反。
func (c *BadCaseController) unavailable(ctx *gin.Context) {
	response.Error(ctx, http.StatusServiceUnavailable,
		"Bad Case 底座不可用（未装配或缺少 DB 句柄），本次未读到任何计数")
}

// replyError 把服务/仓储的 sentinel 翻成状态码。
//
// 409 与 500 必须分开：前者前端该刷新队列（这条已被别人判了），后者不该 ——
// 拿 500 当"重试一下"会让前端在底座真挂的时候无限打圈。
//
// 入参非法只认 service 那一个名字：仓储的 sentinel 与它是同一个变量
// （见 service.ErrBadCaseInputInvalid 的赋值），再写一条 errors.Is 是死分支，
// 而死分支会诱使后来人以为存在两个错要分别处置。
func (c *BadCaseController) replyError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrBadCaseInputInvalid):
		response.Error(ctx, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrBadCaseNotFound):
		response.Error(ctx, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrBadCaseTransition),
		errors.Is(err, service.ErrBadCaseNothingToExport):
		response.Error(ctx, http.StatusConflict, err.Error())
	default:
		logger.Warnf("[BadCase] 未预期错误: %v", err)
		response.Error(ctx, http.StatusInternalServerError, "坏例操作失败")
	}
}

// badCaseQueryFromRequest 解析列表查询参数。返回 ok=false 表示已经回过错误响应。
func badCaseQueryFromRequest(ctx *gin.Context) (service.BadCaseListQuery, bool) {
	var q service.BadCaseListQuery
	// 可重复给（?source=a&source=b）：坐席队列要"系统报的 + 自己补录的"合并视图。
	// 空值丢掉而不是透传：?label= 是"没选筛选器"的前端常态，判成 400 会把队列打不开，
	// 而未知值（?label=bogus）仍会一路传到仓储报错 —— 那条是业务结论，必须红。
	q.Sources = badCaseQueryValues(ctx, "source")
	q.Statuses = badCaseQueryValues(ctx, "status")
	q.Labels = badCaseQueryValues(ctx, "label")
	q.FixLayer = strings.TrimSpace(ctx.Query("fix_layer"))

	page, size, ok := badCasePaging(ctx)
	if !ok {
		return q, false
	}
	q.Page, q.PageSize = page, size
	return q, true
}

func badCaseQueryValues(ctx *gin.Context, key string) []string {
	var out []string
	for _, v := range ctx.QueryArray(key) {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// badCasePaging 解析分页。给了非法值直接 400，不做"那就当第一页"的静默纠正：
// 前端把 page 传成 0 是 bug，替它兜住只会让翻页永远停在第一页而没人发现。
func badCasePaging(ctx *gin.Context) (int, int, bool) {
	page, size := 1, service.BadCaseDefaultPageSize
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

func badCaseIDParam(ctx *gin.Context) (string, bool) {
	id := strings.TrimSpace(ctx.Param("id"))
	if id == "" {
		response.Error(ctx, http.StatusBadRequest, "坏例 id 不能为空")
		return "", false
	}
	if utf8.RuneCountInString(id) > badCaseIDParamMaxLen {
		response.Error(ctx, http.StatusBadRequest, "坏例 id 过长")
		return "", false
	}
	return id, true
}
