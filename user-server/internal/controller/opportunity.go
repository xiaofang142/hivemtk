// opportunity.go 商机域的 HTTP 出口（新规划任务清单 T-P4-04）。
//
// 本层只做三件事，且刻意不做第四件：
//  1. **形状**：请求体 → service.OpportunityEdit / 跃迁入参，以及 CLAUDE.md 那套
//     {code,data,message} 契约。绑定一律**拒收未知字段**（见 opportunityBindJSON），
//     因为派生量（win_probability）与身份列（id/code/customer_id）在服务层的输入结构里
//     根本没有格子 —— 默认的"丢弃未知字段"会让 `{"win_probability":0.99}` 静默成功，
//     调用方以为改了、库里没改，两边对同一行各持一套账。
//  2. **状态码 + 分诊码**：服务层四个 sentinel 各对应一种**修法不同**的失败
//     （改请求 / 改流程认知 / 先回退状态 / 去查数据），HTTP 状态码只有 400/404/409 三档，
//     分不开那四种。所以 409 一律随 `data.reason` 出一个机器可读的判据词，
//     前端与排查的人靠它分流，而不是靠 `err.Error()` 里的中文（文案会被改写、会被翻译）。
//  3. **装配回显**：底座没装配时读口回 503 而不是空对象 —— `{}` 与 `[]` 都是一句
//     业务结论（"这行没有可做的动作"），而此刻的事实是"一次都没读"。
//
// 本层**不做**的第四件：校验、状态机、幂等、乐观锁全在服务层，这里一处不重写。
// 也不取 actor：OpportunityService 的六个入口没有一个是带操作者参数的（"谁做的这次跃迁"
// 归哪张表还没拍板，见规划文档 T-P4-03 的欠账条目）。在这里 `c.Get("user_id")`
// 再把它丢掉，等于假装本层记了审计 —— 真接线那一步会发现 HTTP 侧"已经有 actor"，
// 于是审计表建出来是空的。路由挂在 JWT 组下这件事由 router.go 保证，不是这里的判据。
//
// 为什么没有"列出商机"这一条：见 /rules 的响应面与 docs/replan-2026-09 的 T-P4-04 段 ——
// 仓储的三个 ListBy* 不返回总数（T-P4-02 的口径），而 CLAUDE.md 的列表契约要求
// `{list,total}`；`total` 只能是估算或页内行数，两者都会被读成"这商机池就这么大"。
// 补 total 要动仓储落点，登记给需要列表的那张卡，不在这里偷偷降级判据。
package controller

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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

// OpportunityController 商机域控制器。
//
// svc 允许是 nil（商机竖未装配）：除 /rules 之外的每个出口都回 503，
// 这就是本卡天然的关闸 —— 端点在、答案诚实。
type OpportunityController struct {
	svc *service.OpportunityService
}

// NewOpportunityController 构造。
func NewOpportunityController(svc *service.OpportunityService) *OpportunityController {
	return &OpportunityController{svc: svc}
}

// RegisterRoutes 挂到**已鉴权**的 /api 组下（路径按 CLAUDE.md 的 /api/{domain}/{resource}）。
//
// 表里没有能写 won 的一条：服务层的边表按 (来源, 起点) → 终点 三元存，
// 只有 collection_completed 这一来源放行 won，而"来源"是**代码里的参数**、不是请求体字段。
// 一旦这里开出 POST /:id/won（哪怕要求带 cause），任何登录用户都能自称"回款完成了"，
// 那张三元表当场退化回二元表，AC③ 就只剩注释了。
func (c *OpportunityController) RegisterRoutes(router *gin.RouterGroup) {
	g := router.Group("/opportunity")
	{
		g.GET("/rules", c.Rules)
		g.GET("/:id", c.Get)
		g.GET("/:id/moves", c.Moves)
		g.PUT("/:id", c.Edit)
		g.POST("/:id/stage", c.MoveStage)
		g.POST("/:id/lost", c.MarkLost)
		g.POST("/:id/cancel", c.Cancel)
		g.POST("/:id/reopen", c.Reopen)
	}
}

const (
	// opportunityIDParamMaxLen 商机 id 的长度上限（形如 opp_<unixnano>_<seq>，用不到 30）。
	// 收窄是为了下面把 id 回显进提示里：无上限的回显等于给调用方一个免费的响应体放大器。
	opportunityIDParamMaxLen = 64
	// opportunityBodyMaxBytes 请求体上限：一份整编辑不到 200 字节，
	// 留 4KB 是给自由文本原因的余量。读满即拒，不让它进 JSON 解析器。
	opportunityBodyMaxBytes = 4 << 10
	// opportunityErrMaxRunes 透出的底层错误文本长度上限（同上，回显要有界）。
	opportunityErrMaxRunes = 200
)

// opportunityReason* 是错误响应里 data.reason 的取值集合。它们与 HTTP 状态码一起构成完整判据：
// 状态码给"这一类请求要不要重发"，reason 给"具体改哪一处"。
const (
	opportunityReasonInputInvalid = "input_invalid"
	opportunityReasonNotFound     = "not_found"
	opportunityReasonStaleVersion = "stale_version"
	opportunityReasonClosed       = "closed"
	opportunityReasonTransition   = "transition_illegal"
	opportunityReasonStateInvalid = "state_invalid"
	opportunityReasonInternal     = "internal"
)

// Rules GET /api/opportunity/rules —— 这套状态机的**唯一对外说明书**。
//
// 前端按它渲染按钮，就不去自己抄第二份状态机（抄的那一份迟早与这里分家，
// 而分家之后"哪个按钮能点"取决于两份表里更新的那一份）。
// 本端点不碰库，因此未装配时照答 —— 机器规则与 DB 句柄无关。
//
// @Summary      商机状态机与值域（机器规则）
// @Description  阶段顺序、状态值域、每个 (阶段,状态) 当前可请求的动作、以及不经 HTTP 暴露的机器动作
// @Tags         Opportunity
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  response.Response  "成功"
// @Router       /api/opportunity/rules [get]
func (c *OpportunityController) Rules(ctx *gin.Context) {
	response.Success(ctx, opportunityRulesView(), "ok")
}

// opportunityRulesView 纯函数化的规则视图：路由 handler 里的分支（未装配、终态行）
// 在真实装配下很难凑齐，走 handler 只能测到最顺的那一支。
func opportunityRulesView() gin.H {
	moves := make(map[string]map[string][]service.OpportunityMove, len(model.OpportunityStatuses))
	for _, status := range model.OpportunityStatuses {
		perStage := make(map[string][]service.OpportunityMove, len(model.OpportunityStages))
		for _, stage := range model.OpportunityStages {
			// 终态行返回 nil slice，序列化成 null 会被前端读成"没读到"而不是"没有动作"，
			// 所以这里必须给出**空数组**。
			list := service.AllowedOpportunityMoves(stage, status)
			if list == nil {
				list = []service.OpportunityMove{}
			}
			perStage[stage] = list
		}
		moves[status] = perStage
	}
	closed := make([]string, 0, len(model.OpportunityStatuses))
	for _, status := range model.OpportunityStatuses {
		if model.OpportunityClosed(status) {
			closed = append(closed, status)
		}
	}
	return gin.H{
		"stage_order":   model.OpportunityStages,
		"status_values": model.OpportunityStatuses,
		// 分两列给读方是因为两张表的口径各自独立成数：漏斗按 stage、闭环率按 status。
		"closed_statuses":   closed,
		"moves":             moves,
		"server_only_moves": service.ServerOnlyOpportunityMoves(),
		// 整份编辑的四格；省略即清掉（PUT 不是 PATCH），依据见 service.OpportunityEdit 的注释。
		"editable_fields": []string{"amount", "currency", "owner_user_id", "expected_close_at"},
		"win_probability": gin.H{
			"derived":  true,
			"writable": false,
			"formula": "round2(base(阶段) − 无归属 0.10 − 已过预计关单日未收口 0.15)，下限 0.05；" +
				"base 是先验刻度不是标定值，依据见 internal/service/opportunity.go 文件头",
		},
		"terminal_statuses": gin.H{
			"won":       "死态：钱已经真实发生，不许回退",
			"cancelled": "死态：这一行根本不该存在，要改回去得重建",
			"lost":      "可回退（reopen）：当时没买是会被时间推翻的事实",
		},
	}
}

// Get GET /api/opportunity/:id
//
// @Summary      读取单条商机
// @Tags         Opportunity
// @Produce      json
// @Security     BearerAuth
// @Param        id   path   string  true  "商机业务主键"
// @Success      200  {object}  response.Response  "成功"
// @Failure      400  {object}  response.Response  "id 形状不合法"
// @Failure      404  {object}  response.Response  "商机不存在"
// @Failure      503  {object}  response.Response  "底座未装配"
// @Router       /api/opportunity/{id} [get]
func (c *OpportunityController) Get(ctx *gin.Context) {
	if !c.svc.Available() {
		c.unavailable(ctx)
		return
	}
	id, ok := opportunityIDParam(ctx)
	if !ok {
		return
	}
	row, err := c.svc.Get(ctx.Request.Context(), id)
	if err != nil {
		c.replyError(ctx, err)
		return
	}
	if row == nil {
		response.Error(ctx, http.StatusNotFound, "商机不存在（id="+id+"）",
			gin.H{"reason": opportunityReasonNotFound})
		return
	}
	response.Success(ctx, row, "ok")
}

// Moves GET /api/opportunity/:id/moves —— 这一行**现在**能请求哪些动作。
//
// @Summary      列出某条商机当前可请求的动作
// @Description  与 /rules 同一张边表的单行视图；不含赢单（那是回款完成触发的，不是按钮）
// @Tags         Opportunity
// @Produce      json
// @Security     BearerAuth
// @Param        id   path   string  true  "商机业务主键"
// @Success      200  {object}  response.Response  "成功"
// @Failure      404  {object}  response.Response  "商机不存在"
// @Failure      503  {object}  response.Response  "底座未装配"
// @Router       /api/opportunity/{id}/moves [get]
func (c *OpportunityController) Moves(ctx *gin.Context) {
	if !c.svc.Available() {
		c.unavailable(ctx)
		return
	}
	id, ok := opportunityIDParam(ctx)
	if !ok {
		return
	}
	row, err := c.svc.Get(ctx.Request.Context(), id)
	if err != nil {
		c.replyError(ctx, err)
		return
	}
	if row == nil {
		response.Error(ctx, http.StatusNotFound, "商机不存在（id="+id+"）",
			gin.H{"reason": opportunityReasonNotFound})
		return
	}
	list := service.AllowedOpportunityMoves(row.Stage, row.Status)
	if list == nil {
		list = []service.OpportunityMove{}
	}
	response.Success(ctx, gin.H{
		"id": row.ID, "stage": row.Stage, "status": row.Status, "moves": list,
	}, "ok")
}

// opportunityVersionBody 只带期望版本的动作（cancel / reopen）。
//
// Version 用指针：**缺字段与 0 是两件事**。新行的合法期望版本就是 0，
// 用 int 接再判 ==0 等于把"没带版本"当成"以 v0 为准"，
// 于是并发时第一条永远撞不上、后面每条都报错。
type opportunityVersionBody struct {
	Version *int64 `json:"version"`
}

// opportunityStageBody 阶段跃迁入参。
type opportunityStageBody struct {
	Version *int64 `json:"version"`
	ToStage string `json:"to_stage"`
}

// opportunityLostBody 输单入参：原因必填（服务层判空与长度，这里不重复判）。
type opportunityLostBody struct {
	Version *int64 `json:"version"`
	Reason  string `json:"reason"`
}

// opportunityEditBody 整份编辑：内嵌服务层的输入结构，不另开一份字段清单。
//
// 内嵌而不是逐字段抄一遍的理由是**分家风险**：这里多一份清单，
// 服务层加一格时只有一侧会加，而"少一格的 PUT 会静默丢掉那一格的改动"（整份替换语义）
// 恰好让这种遗漏以"数据莫名回退"的形式出现。
type opportunityEditBody struct {
	service.OpportunityEdit
	Version *int64 `json:"version"`
}

// Edit PUT /api/opportunity/:id —— 整份改写金额/币种/归属/预计关单日。
//
// @Summary      整份改写商机的可编辑四格
// @Description  PUT 语义＝整份替换：省略 expected_close_at 等于清掉它。赢率由本层按新事实重算，不接受传入
// @Tags         Opportunity
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id     path   string                  true  "商机业务主键"
// @Param        body   body   opportunityEditBody     true  "金额/币种/归属/预计关单日 + 期望版本"
// @Success      200    {object}  response.Response  "成功"
// @Failure      400    {object}  response.Response  "入参不合法（含未知字段、缺 version）"
// @Failure      404    {object}  response.Response  "商机不存在"
// @Failure      409    {object}  response.Response  "版本过期 / 已收口 / 这一行本身越界"
// @Failure      503    {object}  response.Response  "底座未装配"
// @Router       /api/opportunity/{id} [put]
func (c *OpportunityController) Edit(ctx *gin.Context) {
	if !c.svc.Available() {
		c.unavailable(ctx)
		return
	}
	id, ok := opportunityIDParam(ctx)
	if !ok {
		return
	}
	var body opportunityEditBody
	if !c.bindJSON(ctx, &body) {
		return
	}
	version, ok := c.requireVersion(ctx, body.Version)
	if !ok {
		return
	}
	row, err := c.svc.Edit(ctx.Request.Context(), id, version, body.OpportunityEdit)
	c.replyRow(ctx, row, err, "改写商机")
}

// MoveStage POST /api/opportunity/:id/stage —— 阶段跃迁（向前恰好一步、向后任意步）。
//
// @Summary      推进或回退商机阶段
// @Description  同格不算跃迁（否则"再点一次"就能凭空把 version 涨一格）；赢率随阶段重算
// @Tags         Opportunity
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id     path   string                true  "商机业务主键"
// @Param        body   body   opportunityStageBody  true  "目标阶段 + 期望版本"
// @Success      200    {object}  response.Response  "成功"
// @Failure      400    {object}  response.Response  "阶段字面值不在值域里 / 缺 version"
// @Failure      404    {object}  response.Response  "商机不存在"
// @Failure      409    {object}  response.Response  "状态机不允许 / 已收口 / 版本过期 / 这一行越界"
// @Failure      503    {object}  response.Response  "底座未装配"
// @Router       /api/opportunity/{id}/stage [post]
func (c *OpportunityController) MoveStage(ctx *gin.Context) {
	if !c.svc.Available() {
		c.unavailable(ctx)
		return
	}
	id, ok := opportunityIDParam(ctx)
	if !ok {
		return
	}
	var body opportunityStageBody
	if !c.bindJSON(ctx, &body) {
		return
	}
	version, ok := c.requireVersion(ctx, body.Version)
	if !ok {
		return
	}
	row, err := c.svc.MoveStage(ctx.Request.Context(), id, version, strings.TrimSpace(body.ToStage))
	c.replyRow(ctx, row, err, "阶段跃迁")
}

// MarkLost POST /api/opportunity/:id/lost —— 判输，必须带原因。
//
// @Summary      标记商机输单（必须给原因）
// @Description  原因是丢单归因的唯一可读列；收口后赢率冻结，不再重算
// @Tags         Opportunity
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id     path   string               true  "商机业务主键"
// @Param        body   body   opportunityLostBody  true  "输单原因 + 期望版本"
// @Success      200    {object}  response.Response  "成功"
// @Failure      400    {object}  response.Response  "原因为空或超长 / 缺 version"
// @Failure      404    {object}  response.Response  "商机不存在"
// @Failure      409    {object}  response.Response  "已收口 / 版本过期 / 这一行越界"
// @Failure      503    {object}  response.Response  "底座未装配"
// @Router       /api/opportunity/{id}/lost [post]
func (c *OpportunityController) MarkLost(ctx *gin.Context) {
	if !c.svc.Available() {
		c.unavailable(ctx)
		return
	}
	id, ok := opportunityIDParam(ctx)
	if !ok {
		return
	}
	var body opportunityLostBody
	if !c.bindJSON(ctx, &body) {
		return
	}
	version, ok := c.requireVersion(ctx, body.Version)
	if !ok {
		return
	}
	row, err := c.svc.MarkLost(ctx.Request.Context(), id, version, body.Reason)
	c.replyRow(ctx, row, err, "标记输单")
}

// Cancel POST /api/opportunity/:id/cancel —— 作废（误建或重复）。
//
// 不写 lost_reason：那个字段是丢单归因的口径，把误建混进去，
// 看板会拿去给销售团队定一条根本不存在问题的改进项。
//
// @Summary      作废商机（误建或重复）
// @Tags         Opportunity
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id     path   string                  true  "商机业务主键"
// @Param        body   body   opportunityVersionBody  true  "期望版本"
// @Success      200    {object}  response.Response  "成功"
// @Failure      400    {object}  response.Response  "缺 version"
// @Failure      404    {object}  response.Response  "商机不存在"
// @Failure      409    {object}  response.Response  "已收口 / 版本过期 / 这一行越界"
// @Failure      503    {object}  response.Response  "底座未装配"
// @Router       /api/opportunity/{id}/cancel [post]
func (c *OpportunityController) Cancel(ctx *gin.Context) {
	c.versionOnlyAction(ctx, "作废", c.svc.Cancel)
}

// Reopen POST /api/opportunity/:id/reopen —— 把误判输的商机拉回在跑。
//
// 只有 lost 能回退：won 背后是真发生的钱，cancelled 的含义是"根本不该有这一行"。
//
// @Summary      把输单商机拉回在跑（清空输单原因并重算赢率）
// @Tags         Opportunity
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id     path   string                  true  "商机业务主键"
// @Param        body   body   opportunityVersionBody  true  "期望版本"
// @Success      200    {object}  response.Response  "成功"
// @Failure      400    {object}  response.Response  "缺 version"
// @Failure      404    {object}  response.Response  "商机不存在"
// @Failure      409    {object}  response.Response  "这一行不是输单态 / 版本过期 / 越界"
// @Failure      503    {object}  response.Response  "底座未装配"
// @Router       /api/opportunity/{id}/reopen [post]
func (c *OpportunityController) Reopen(ctx *gin.Context) {
	c.versionOnlyAction(ctx, "回退为在跑", c.svc.Reopen)
}

// —— 出口公共设施 ——————————————————————————————————————

// versionOnlyAction cancel/reopen 的共同骨架（两者只差调哪个方法，
// 而方法签名与服务层那两个入口一致，所以直接传方法值、不再包一层结构体）。
func (c *OpportunityController) versionOnlyAction(ctx *gin.Context, action string,
	fn func(ctx context.Context, id string, version int64) (*model.Opportunity, error)) {
	if !c.svc.Available() {
		c.unavailable(ctx)
		return
	}
	id, ok := opportunityIDParam(ctx)
	if !ok {
		return
	}
	var body opportunityVersionBody
	if !c.bindJSON(ctx, &body) {
		return
	}
	version, ok := c.requireVersion(ctx, body.Version)
	if !ok {
		return
	}
	row, err := fn(ctx.Request.Context(), id, version)
	c.replyRow(ctx, row, err, action)
}

// opportunityIDParam 取路径里的商机主键。返回 ok=false 表示已经回过错误响应。
func opportunityIDParam(ctx *gin.Context) (string, bool) {
	id := strings.TrimSpace(ctx.Param("id"))
	if id == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少商机 id",
			gin.H{"reason": opportunityReasonInputInvalid})
		return "", false
	}
	if len(id) > opportunityIDParamMaxLen {
		// 只回显前若干字符：整段回显一个 10KB 的"id"等于给调用方一个免费的响应体放大器。
		response.Error(ctx, http.StatusBadRequest,
			"商机 id 长度 "+strconv.Itoa(len(id))+" 超上限 "+strconv.Itoa(opportunityIDParamMaxLen)+"（"+opportunityEllipsis(id)+"）",
			gin.H{"reason": opportunityReasonInputInvalid})
		return "", false
	}
	return id, true
}

func opportunityEllipsis(v string) string {
	if utf8.RuneCountInString(v) <= 16 {
		return v
	}
	return string([]rune(v)[:16]) + "…"
}

// bindJSON 严格绑定：拒收未知字段，并对空请求体单独出声。
//
// 返回 ok=false 表示已经回过 400。上限先卡在读这一层，
// 不让一份超大 body 进到 JSON 解析器里再决定拒收。
func (c *OpportunityController) bindJSON(ctx *gin.Context, target any) bool {
	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, opportunityBodyMaxBytes)
	dec := json.NewDecoder(ctx.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		msg := err.Error()
		// 体积那一刀要单列：MaxBytesError 混进"形状不对"里，运维读到的是"调用方字段写错了"，
		// 实际发生的是"这一格的请求体被封顶了" —— 两种情形的处置动作完全不同。
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			response.Error(ctx, http.StatusBadRequest,
				"请求体超过 "+strconv.Itoa(opportunityBodyMaxBytes)+" 字节上限：本接口的每一次改写都是几十字节的小整行，容不下更大的体",
				gin.H{"reason": opportunityReasonInputInvalid})
			return false
		}
		if errors.Is(err, io.EOF) || strings.Contains(msg, "EOF") {
			response.Error(ctx, http.StatusBadRequest,
				"请求体为空：这些动作都要带 version（乐观锁的期望版本；新行的期望版本是 0，不是\"不传\"）",
				gin.H{"reason": opportunityReasonInputInvalid})
			return false
		}
		response.Error(ctx, http.StatusBadRequest, "请求体形状不对："+opportunityTrimRunes(msg, opportunityErrMaxRunes),
			gin.H{"reason": opportunityReasonInputInvalid})
		return false
	}
	return true
}

func opportunityTrimRunes(v string, max int) string {
	if utf8.RuneCountInString(v) <= max {
		return v
	}
	return string([]rune(v)[:max]) + "…"
}

// requireVersion 缺字段的判点在 HTTP 侧而不是服务侧：服务层签名收 int64，
// 传不进来只能是"调用方忘了"，而忘了的默认值（0）恰好是新行的合法版本 ——
// 那种默认会让第一次并发改写悄悄通过，正是乐观锁要拦的那件事。
func (c *OpportunityController) requireVersion(ctx *gin.Context, v *int64) (int64, bool) {
	if v == nil {
		response.Error(ctx, http.StatusBadRequest,
			"缺少 version：写入口必须带调用方看到的那一格的版本（乐观锁的期望版本，新行是 0）",
			gin.H{"reason": opportunityReasonInputInvalid})
		return 0, false
	}
	return *v, true
}

// unavailable 503：带 reason，但绝不带空对象。
//
// 判据不是"错误响应里不许有 data"，而是"不许有**看起来像结果**的 data"：
// `{}`、`[]`、`null` 都会被前端长成一句"查过了，没有"，而此刻的事实是"一次都没查"。
// reason=unavailable 与 400/404/409 用的是同一套分诊词表 —— 503 与 500 在网关日志里
// 长得一样，一个该等装配、一个该查故障，只靠状态码分不开那两种动作。
func (c *OpportunityController) unavailable(ctx *gin.Context) {
	response.Error(ctx, http.StatusServiceUnavailable,
		"商机底座未装配（缺少 DB 句柄或路由挂在了装配之前），本次未读到任何数据",
		gin.H{"reason": "unavailable"})
}

// replyRow 写动作与读动作共同的收尾：err 优先，其次 nil 行判 404，最后回行。
func (c *OpportunityController) replyRow(ctx *gin.Context, row *model.Opportunity, err error, action string) {
	if err != nil {
		c.replyError(ctx, err)
		return
	}
	if row == nil {
		response.Error(ctx, http.StatusNotFound, action+"：商机不存在",
			gin.H{"reason": opportunityReasonNotFound})
		return
	}
	response.Success(ctx, row, "ok")
}

// replyError 把服务/仓储的 sentinel 翻成 (状态码, reason)。
//
// 顺序是判据的一部分：StaleVersion 与 Closed 都可能在同一行上成立（别人先收了口），
// 先判版本就永远只说"刷新重试"，而事实是"这单已经没了，刷新也没用"。
// 反过来先判 Closed 会把"你手里那份是旧的"说成"已收口"，两种都不算错，
// 但只有一种会让人去查数据 —— 这里选择让 Closed 先出，因为它的修复动作更贵、
// 误报的代价是"多查一次数据"而不是"少查一次数据"。
func (c *OpportunityController) replyError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrOpportunityStateInvalid):
		// 先于 Closed：库里那一行本身越界时，"它是不是终态"这个问题没有可信答案。
		c.writeConflict(ctx, http.StatusConflict, opportunityReasonStateInvalid, err)
	case errors.Is(err, service.ErrOpportunityClosed):
		c.writeConflict(ctx, http.StatusConflict, opportunityReasonClosed, err)
	case errors.Is(err, service.ErrOpportunityStaleVersion):
		c.writeConflict(ctx, http.StatusConflict, opportunityReasonStaleVersion, err)
	case errors.Is(err, service.ErrOpportunityTransitionIllegal):
		c.writeConflict(ctx, http.StatusConflict, opportunityReasonTransition, err)
	case errors.Is(err, service.ErrOpportunityInputInvalid):
		response.Error(ctx, http.StatusBadRequest, err.Error(), gin.H{"reason": opportunityReasonInputInvalid})
	case errors.Is(err, service.ErrOpportunityNotFound):
		response.Error(ctx, http.StatusNotFound, err.Error(), gin.H{"reason": opportunityReasonNotFound})
	default:
		// 500 的文案**不透出**底层错误串：仓储报错可能带着 SQL 片段与列名。
		// 运维要看的那一份在日志里，与响应体不同通道，两个通道各有各的受众。
		logger.Warnf("[Opportunity] 未预期错误: %v", err)
		response.Error(ctx, http.StatusInternalServerError, "商机操作失败（底座或数据异常）",
			gin.H{"reason": opportunityReasonInternal})
	}
}

func (c *OpportunityController) writeConflict(ctx *gin.Context, status int, reason string, err error) {
	if reason == opportunityReasonStaleVersion {
		response.Error(ctx, status, err.Error()+"（重取这一行后再改，不要直接重发同一份请求）",
			gin.H{"reason": reason})
		return
	}
	response.Error(ctx, status, err.Error(), gin.H{"reason": reason})
}
