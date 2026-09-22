// bill.go T-P7-01 + T-P7-02：账单域的 HTTP 出口（写：把"这一版成交了"翻成一张应收；
// 读：把那张应收"收了多少、还欠多少"答出来）。
//
// 本层只做四件事（与 controller/quote.go 的分工同一条）：
//  1. **形状**：请求体 → service.BillDeriveInput。绑定拒收未知字段，且这里比报价侧更硬：
//     体里若能递进 `amount`，AC② 的"账单金额与报价合计一致"就当场失去对账对象 ——
//     服务层的入参结构里没有那一格（由反射用例钉成白名单），但宽容的绑定会把递进来的
//     金额**静默丢掉**，于是"我传了 5000 而系统按 369.99 记账"在调用方与系统两边都看起来是对的。
//     读侧的形状更窄：入参只有 URL 上的那把键，所以这一格的全部工作就是"有界、非空、原样透传"。
//  2. **状态码 + 分诊码**：八个哨兵各自对应一种**修法不同**的失败（改载荷 / 换行号 /
//     先去处置原账单 / 重读那一版再来），所以每个错误都随 `data.reason` 出一个机器可读判据词。
//     HTTP 状态码分不开其中三种 409，reason 分得开。
//  3. **装配回显**：两条腿各自没装配时各自回 503（派生缺的多、对账读缺的少是两种真实故障），
//     data 不给空对象。
//  4. **不把求和搬到这一层**：settled / outstanding 来自 service.PaymentService.Statement，
//     本层一行加法都不写。理由与 bills 上不存"已收"同源：多一处算就是多一份事实，
//     而 AC② 要对的正是这个数。
//
// 本层**不做**的事，逐条都有归宿而不是漏写：
//   - 不列全表（无 GET /api/bill）、不改账单（无 PUT）、不改状态（无 /pay / /void / /status）：
//     结清与作废只能由回款行累加出来（T-P7-02 的服务层），逾期与催收在 T-P7-03。
//     今天开出任何一格"把账单改成已收"，就是给财务台账开一条**不走回款**的捷径。
//     两条 GET 都必须带一把键 —— 键是本表目前唯一的归属判据（bills 没有租户列，
//     判据见 model/bill.go），没有键的读在补上归属列之前等于"任何人可看全公司的应收"。
//   - 不做闸门：报价的出域必经审批（T-P6-03 已守在那条腿上），而"确认已成交"
//     是内部记账动作，不往域外送任何东西。
//   - 不记"谁确认的"到库里：bills 没有那一格（限制与服务层的同一句话同源，
//     补它属 T-P8-01 的埋点范围）。本层能做的是**要求有身份**并把它写进审计日志行，
//     否则事后连一处可查的线索都没有。
//
// 幂等不在本层：重复点确认由服务层合成同一个结果（复用那一行），响应面上靠 `reused`
// 这一格把"我刚开了一张应收"与"这张早就在了"分开 —— 本层不加锁、不判重，
// 因为那两处判据都在库里，在这里再写一遍就是第三个事实源。
package controller

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

// BillDeriver 派生腿的接缝。*service.BillService 天然满足。
//
// 只有 Available 与 DeriveFromQuote 两格（方法集合由 TestBillController_SeamIsExactlyTheDocumentedPair
// 用反射钉住）：**没有**作废、标记已收、改状态，也没有任何读口——读口在下面的另一把接缝上，
// 两把分开是因为它们由**两个装配点**装（app.InitBillRuntime 与 app.InitPaymentRuntime），
// 合成一把就只能一起有或一起没有，而"派生能跑、对账读不到"是一种会真实发生的半装配。
// 写状态的那一格入口仍然只在回款累加那一侧（service.PaymentService.applySettlement），
// 从 HTTP 到它没有任何通路（判据见 bill_routes_test.go 的 404 探针与这族 needles）。
// （这一段刻意不写方法名字面量：bill_routes_test.go 的静态锁按**原文**扫本文件，
// 写了禁用词的用注释豁免自己，等于把那道门锁成只有注释能开的样子。）
type BillDeriver interface {
	Available() bool
	DeriveFromQuote(ctx context.Context, in service.BillDeriveInput) (*service.BillView, error)
}

// BillReader 对账读腿的接缝。*service.PaymentService 天然满足。
//
// 三格里只有一格是读口的判据重点：**没有**仓储级的行读（两格都叫 Statement*，返回的是
// "收了多少、还欠多少"这个结论，不是 bills 与 payments 的行）。为什么要卡在结论这一层：
// 一旦这层能拿到行，它就会顺手自己加一遍 —— 而"已收"这个数只允许有一个算法
// （service/payment.go 的 statementOfBill），多一处就是 AC② 对不上账的那天。
// 也**没有**分页、排序、按客户/按状态捞：账单没有租户列（model/bill.go），
// 没有键的读口在本域等于"任何人可看全公司的应收"。
type BillReader interface {
	Available() bool
	Statement(ctx context.Context, billID string) (*service.BillStatementView, error)
	StatementsOfQuote(ctx context.Context, quoteID string) ([]*service.BillStatementView, error)
}

var (
	_ BillDeriver = (*service.BillService)(nil)
	_ BillReader  = (*service.PaymentService)(nil)
)

// BillController 账单域控制器。两条腿各自可为 nil（未装配），缺哪条那一条的端点回 503。
type BillController struct {
	derive BillDeriver
	read   BillReader
}

// NewBillController 构造。两把接缝都从参数进来：装配点给什么就是什么，
// 本层不从全局偷偷取第三把（那会让"路由挂在了装配之前"这一类错只在生产里显形）。
func NewBillController(derive BillDeriver, read BillReader) *BillController {
	return &BillController{derive: derive, read: read}
}

// Available 报告**派生腿**在不在（装配回显与测试用；nil 接收者同样答得出来，不 panic）。
//
// 名字不带 leg 是历史原因（T-P7-01 时只有一条腿），K68 那一格变异注的正是"它有没有真的问服务本身"。
// 读腿的可读性走 canRead()：它只在本层内部用于关闸，装配日志在 router 层直接问服务实例。
func (c *BillController) Available() bool {
	return c != nil && c.derive != nil && c.derive.Available()
}

// canRead 报告读腿在不在（判据与 Available 同一条：必须问服务本身，不能只看指针非空）。
func (c *BillController) canRead() bool {
	return c != nil && c.read != nil && c.read.Available()
}

// RegisterRoutes 挂到**已鉴权**的 /api 组下（路径按 CLAUDE.md 的 /api/{domain}/{resource}）。
//
// 三条端点：一条写、两条读，两把键各一。为什么成交确认挂在 /api/bill 而不是 /api/quote/:id/accept：
// 报价控制器当年刻意没开 accept 那一条（controller/quote.go 的 RegisterRoutes 注释，
// 而 router/quote_routes_test.go 用 404 探针把它钉成了静态锁）。那句话今天仍然成立，
// 变的是它有了后果——点这一格的人同时在开一张应收。把端点放在账单域，
// 是让"这是记账动作"这件事写在 URL 上，而不是靠注释提醒。
//
// 读侧两条的分工：`/:id` 答"这张单收了多少"，`/of-quote/:quote_id` 答"这张报价单开过哪几张单"
// （一条链上可能有多个版本各自成交过一次，对账要读的是那一串）。
func (c *BillController) RegisterRoutes(router *gin.RouterGroup) {
	g := router.Group("/bill")
	{
		g.POST("", c.Derive)
		g.GET("/:id", c.View)
		g.GET("/of-quote/:quote_id", c.OfQuote)
	}
}

const (
	// billRowIDMaxLen 版本行键（q_<unixnano>_<seq>）用不到 30；上限是为了把行号回显进提示里
	// 时有界，且 bills.id 会被抄进 payments.bill_id（varchar(64)，T-P7-02）——同宽是下限。
	billRowIDMaxLen = 64
	// billKeyMaxLen URL 路径上那两把键（账单号 / 报价逻辑号）的上限。
	//
	// 写成"等于上面那个"而不是另填一个 64：bills.id / bills.quote_id / quotes.id 三把键列
	// 同为 varchar(64)，宽度由同一个建表口径决定。分开写两个常量的那天，它们就会各自漂。
	billKeyMaxLen = billRowIDMaxLen
	// billBodyMaxBytes 请求体上限：这一格的入参只有一个行号，1KB 已是百倍余量。
	// 比报价侧（8KB，装得下一张行项目覆盖清单）窄一档是刻意的：入参面窄到一行字符串，
	// 体积上限就该跟着窄，否则"上限"这个词在这里没有任何含义。
	billBodyMaxBytes = 1 << 10
	// billErrMaxRunes 透出的绑定错误文本长度上限（回显必须有界）。
	billErrMaxRunes = 200
)

// billReason* 错误响应里 data.reason 的取值集合。与 HTTP 状态码一起构成完整判据。
const (
	billReasonUnauthenticated     = "unauthenticated"
	billReasonInputInvalid        = "input_invalid"
	billReasonNotFound            = "not_found"
	billReasonNotSent             = "not_sent"
	billReasonNotLatestVersion    = "not_latest_version"
	billReasonChainAlreadyPresent = "chain_already_accepted"
	billReasonLinesMissing        = "lines_missing"
	billReasonStatusStuck         = "status_stuck"
	billReasonUnavailable         = "unavailable"
	billReasonInternal            = "internal"
)

// 两条腿各自的 503 文案。分成两条而不是复用一句"底座未装配"：读到 503 的人要立刻知道
// 该去催哪一个装配点——派生缺是 app.InitBillRuntime，对账读缺是 app.InitPaymentRuntime，
// 而这两句在运维手里的动作完全不同（前者"开不出应收"，后者"账单一直有"）。
const (
	msgDeriveUnavailable = "账单底座未装配（缺少 DB 句柄或路由挂在了装配之前），本次未派生任何账单"
	msgReadUnavailable   = "对账底座未装配（缺少 DB 句柄或路由挂在了装配之前），本次未读到任何账单"
)

// billDeriveBody 派生入参：**只有**版本行号这一格。
//
// 不内嵌 service.BillDeriveInput 而另写一个结构，是为了让"体里能递进来什么"这一格
// 在 HTTP 边界上独立可读（Swagger 生成的 schema 就是这一格）；字段名与 json tag
// 必须与服务层同形，TestBillController_BodyCarriesOnlyTheRowKey 逐格试的是这个集合的补集。
type billDeriveBody struct {
	QuoteRowID string `json:"quote_row_id"`
}

// Derive POST /api/bill —— 确认这一版报价成交，并开出那张应收。
//
// 200 有两种来源（新开 / 复用已有），差在那一格的 reused 上；四种失败各一档状态码。
//
// @Summary      由已成交报价派生账单
// @Description  只有链上最新且已发出的一版能被确认；金额取自该版行项目合计，不是入参；同版重复确认复用已有账单（data.reused=true）
// @Tags         Bill
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body   body    billDeriveBody              true  "版本行主键 quotes.id（不是跨版本重复出现的逻辑号）"
// @Success      200    {object}  response.Response  "成功（data 为账单视图，reused 区分新开与复用）"
// @Failure      400    {object}  response.Response  "入参不合法（含未知字段：金额、状态、币种、账期都不是入参）"
// @Failure      401    {object}  response.Response  "会话里没有操作者身份"
// @Failure      404    {object}  response.Response  "该版报价不存在"
// @Failure      409    {object}  response.Response  "该版未发出 / 不是链上最新 / 该链已有一次成交 / 无行项目 / 状态写不动"
// @Failure      503    {object}  response.Response  "底座未装配"
// @Router       /api/bill [post]
func (c *BillController) Derive(ctx *gin.Context) {
	if !c.Available() {
		c.unavailable(ctx, msgDeriveUnavailable)
		return
	}
	// 身份取不到就到此为止：本层不读报价、不开账单。
	// 反面的形状是让服务层去判空——那一路会把"你没登录"翻成 400（改载荷），
	// 而更坏的是账单已经开出去了，事后连"谁点的"这一行日志都拼不出来。
	operator, ok := contextOperatorID(ctx)
	if !ok {
		response.Error(ctx, http.StatusUnauthorized,
			"确认成交必须带操作者身份（未登录或会话里没有 user id），本次未派生任何账单",
			gin.H{"reason": billReasonUnauthenticated})
		return
	}
	var body billDeriveBody
	if !c.bindJSON(ctx, &body) {
		return
	}
	rowID := strings.TrimSpace(body.QuoteRowID)
	if rowID == "" {
		response.Error(ctx, http.StatusBadRequest,
			"缺少 quote_row_id：账单钉在**版本行**上，报价逻辑号跨版本重复出现，拿它派生等于一张报价单只能开一张账单",
			gin.H{"reason": billReasonInputInvalid})
		return
	}
	if len(rowID) > billRowIDMaxLen {
		response.Error(ctx, http.StatusBadRequest,
			"quote_row_id 长度 "+strconv.Itoa(len(rowID))+" 超上限 "+strconv.Itoa(billRowIDMaxLen)+"（"+opportunityEllipsis(rowID)+"）",
			gin.H{"reason": billReasonInputInvalid})
		return
	}

	view, err := c.derive.DeriveFromQuote(ctx.Request.Context(), service.BillDeriveInput{QuoteRowID: rowID})
	if err != nil {
		c.replyError(ctx, err, rowID)
		return
	}
	if view == nil {
		// 服务层"要么给视图要么给错"，走到这里就是实现漂了：按失败报，不回一个空对象。
		c.replyError(ctx, errors.New("派生账单返回了空结果而没有给错误"), rowID)
		return
	}
	// 审计行：库里今天没有"谁确认的"这一格（见文件头），这一行是那件事唯一的去处。
	// 行号与账单号都用 %q：它们是从请求体与库里来的文本，裸 %s 能把一次调用劈成两行日志。
	logger.Infof("[Bill] 用户 %s 确认报价行 %q 成交 ⇒ 账单 %q（金额 %s %s，%s）",
		operator, rowID, view.ID, strconv.FormatFloat(view.Amount, 'f', 2, 64), view.Currency,
		map[bool]string{true: "复用已有那一张", false: "新开"}[view.Reused])
	response.Success(ctx, view, "ok")
}

// View GET /api/bill/:id —— 这张应收收了多少、还欠多少。
//
// 读的是**结论**（service 层求过和的那份），不是 bills 与 payments 的行：
// 这一格若能把行读出来，前端就会在这里加一遍总，而"已收"只允许有一个算法。
//
// 200 只有一种来源；三种失败各一档状态码，其中"没这张单"与"底座没装"必须分开——
// 前者是键给错了，后者是没人接库，两者的修法连主语都不一样。
//
// @Summary      读一张账单的对账视图
// @Description  返回该应收的金额、已收、未收与逐笔回款行；已收由回款行求和得出，账单上不存第二个数字
// @Tags         Bill
// @Produce      json
// @Security     BearerAuth
// @Param        id     path    string  true  "账单号 bills.id（不是报价号，也不接受空串）"
// @Success      200    {object}  response.Response  "成功（data 为对账视图，payments 恒为数组）"
// @Failure      400    {object}  response.Response  "路径上的账单号为空或超宽"
// @Failure      401    {object}  response.Response  "未鉴权（本表没有租户列，读单必须落在鉴权链内）"
// @Failure      404    {object}  response.Response  "该账单号没有对应的应收"
// @Failure      500    {object}  response.Response  "底座或数据异常（不透出底层错误串）"
// @Failure      503    {object}  response.Response  "对账腿未装配"
// @Router       /api/bill/{id} [get]
func (c *BillController) View(ctx *gin.Context) {
	if !c.canRead() {
		c.unavailable(ctx, msgReadUnavailable)
		return
	}
	billID, ok := c.pathKey(ctx, ctx.Param("id"), "账单号")
	if !ok {
		return
	}
	st, err := c.read.Statement(ctx.Request.Context(), billID)
	if err != nil {
		c.replyReadError(ctx, err, billID)
		return
	}
	if st == nil {
		// 与派生侧同一条判据：服务层"要么给视图要么给错"，走到这里就是实现漂了。
		// 按失败报，绝不回一个空的 200 —— 那会被读成"这张单金额为 0、已收 0"。
		c.replyReadError(ctx, errors.New("读账单返回了空结果而没有给错误"), billID)
		return
	}
	response.Success(ctx, st, "ok")
}

// OfQuote GET /api/bill/of-quote/:quote_id —— 这张报价单开过的全部应收。
//
// 为什么要有第二条入口：一条报价链上可以有多个版本各自成交过一次（改价后又被接受），
// 而账单钉在**版本行**上。对账的人手上通常只有报价号（客户口中的"那张单子"），
// 只给 `/:id` 的话，他必须先知道账单号——那等于把这一族最常见的问法挡在门外。
//
// @Summary      按报价读它开过的全部账单
// @Description  一条报价链的多个版本各自成交时，这里给出那一串应收及各自的已收/未收；没有一张时 list 为空数组而不是 null
// @Tags         Bill
// @Produce      json
// @Security     BearerAuth
// @Param        quote_id  path    string  true  "报价逻辑号 quotes.quote_id（跨版本重复出现的那一把，不是版本行号）"
// @Success      200       {object}  response.Response  "成功（data.list 为对账视图数组，data.count 与其同长）"
// @Failure      400      {object}  response.Response  "路径上的报价号为空或超宽"
// @Failure      401      {object}  response.Response  "未鉴权"
// @Failure      500      {object}  response.Response  "底座或数据异常"
// @Failure      503      {object}  response.Response  "对账腿未装配"
// @Router       /api/bill/of-quote/{quote_id} [get]
func (c *BillController) OfQuote(ctx *gin.Context) {
	if !c.canRead() {
		c.unavailable(ctx, msgReadUnavailable)
		return
	}
	quoteID, ok := c.pathKey(ctx, ctx.Param("quote_id"), "报价号")
	if !ok {
		return
	}
	views, err := c.read.StatementsOfQuote(ctx.Request.Context(), quoteID)
	if err != nil {
		c.replyReadError(ctx, err, quoteID)
		return
	}
	if views == nil {
		// 服务层今天给的是 make 出来的空切片；这一格守的是"哪天它改成直接 return nil"。
		// null 与 [] 在前端是两件事：前者是"没读到"，后者是"这张单子确实一张应收都没开"。
		views = []*service.BillStatementView{}
	}
	response.Success(ctx, gin.H{"list": views, "count": len(views)}, "ok")
}

// —— 关闸与入参 ——————————————————————————————————————————

// bindJSON 严格绑定：拒收未知字段 + 请求体封顶（本条端点必须带体，空体算 400）。
func (c *BillController) bindJSON(ctx *gin.Context, target any) bool {
	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, billBodyMaxBytes)
	dec := json.NewDecoder(ctx.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		c.replyBindError(ctx, err)
		return false
	}
	return true
}

// replyBindError 体积、空体、形状三种来路分开说（同报价侧：混成一格时运维读到的是"调用方写错了"，
// 实际发生的是"这一格被封顶了"，两种情形的处置动作完全不同）。
func (c *BillController) replyBindError(ctx *gin.Context, err error) {
	msg := err.Error()
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		response.Error(ctx, http.StatusBadRequest,
			"请求体超过 "+strconv.Itoa(billBodyMaxBytes)+" 字节上限：这一格的入参只有一个版本行号，容不下更大的体",
			gin.H{"reason": billReasonInputInvalid})
		return
	}
	if errors.Is(err, io.EOF) {
		response.Error(ctx, http.StatusBadRequest,
			"请求体为空：派生账单至少要带 quote_row_id",
			gin.H{"reason": billReasonInputInvalid})
		return
	}
	response.Error(ctx, http.StatusBadRequest, "请求体形状不对："+opportunityTrimRunes(msg, billErrMaxRunes),
		gin.H{"reason": billReasonInputInvalid})
}

// unavailable 503：带 reason，但绝不带空对象（判据同 quoteController.unavailable）。
//
// 两条腿共用这一个出口：此刻的事实对两边是同一句话（"我们一次都没做"），差别只在
// 后半句说清楚是哪条腿没做——所以文案由调用方给。分成两个方法的话，其中一个
// 迟早被改成"回 404 也算没读到"，而 404 在这里是说谎：路由在、服务在，只是没接上库。
func (c *BillController) unavailable(ctx *gin.Context, msg string) {
	response.Error(ctx, http.StatusServiceUnavailable, msg,
		gin.H{"reason": billReasonUnavailable})
}

// replyError 把服务/仓储的 sentinel 翻成 (状态码, reason)。
//
// 顺序是判据的一部分，两处要紧：
// ① 未装配先判，因为它意味着其余一切判据都没跑；
// ② 入参不合法先于"不存在"：`{"quote_row_id":""}` 报成"报价不存在"会把人送去查数据。
//
// 三种 409 各给各的 reason：状态码相同而修法完全不同 ——
// not_sent 是"这一版还没给客户看过"（该去发送），not_latest_version 是"客户手上是另一版"
// （该去确认最新那一版），chain_already_accepted 是"这条链已经成交过"
// （该先去处置原来那张账单，而不是再点一次）。
func (c *BillController) replyError(ctx *gin.Context, err error, rowID string) {
	switch {
	case errors.Is(err, service.ErrBillServiceUnavailable):
		c.unavailable(ctx, msgDeriveUnavailable)
	case errors.Is(err, service.ErrBillInputInvalid):
		response.Error(ctx, http.StatusBadRequest, err.Error(), gin.H{"reason": billReasonInputInvalid})
	case errors.Is(err, service.ErrBillQuoteNotFound):
		c.replyNotFound(ctx, err)
	case errors.Is(err, service.ErrBillQuoteNotSent):
		response.Error(ctx, http.StatusConflict, err.Error(), gin.H{"reason": billReasonNotSent})
	case errors.Is(err, service.ErrBillNotLatestVersion):
		response.Error(ctx, http.StatusConflict, err.Error(), gin.H{"reason": billReasonNotLatestVersion})
	case errors.Is(err, service.ErrBillChainAlreadyAccepted):
		response.Error(ctx, http.StatusConflict, err.Error(), gin.H{"reason": billReasonChainAlreadyPresent})
	case errors.Is(err, service.ErrBillLinesMissing):
		response.Error(ctx, http.StatusConflict, err.Error(), gin.H{"reason": billReasonLinesMissing})
	case errors.Is(err, service.ErrBillStatusStuck):
		response.Error(ctx, http.StatusConflict, err.Error(), gin.H{"reason": billReasonStatusStuck})
	default:
		// 500 的文案**不透出**底层错误串：派生这条腿的报错会带着索引名、列名与 SQL 片段。
		logger.Warnf("[Bill] 派生账单 %q 遇到未预期错误: %v", rowID, err)
		response.Error(ctx, http.StatusInternalServerError, "派生账单失败（底座或数据异常），本次未确认任何成交",
			gin.H{"reason": billReasonInternal})
	}
}

// —— 读侧的入参与分诊 ——————————————————————————————————

// pathKey 把 URL 上的那把键清洗成服务层要的样。
//
// 三条判据各挡一种坏法：
//   - 空串（`/api/bill/%20` 这种"看着有其实没有"）必须在这一层挡下：透到服务层会撞上
//     它自己的入参哨兵，而那句错误文本的主语是"bill_id"，调用方在 URL 上找不到这个词；
//     更坏的是万一服务层哪天放松了那一格，空键就退化成"读全表第一行"。
//   - 超宽：键列同为 varchar(64)，超长必是拿错了东西（把整段 URL 或一串正文抄进来了），
//     而回显必须有界。
//   - 除此之外**不做任何清洗**：不转大小写、不补前缀、不去下划线。账单号是库里那把，
//     在这一层"顺手规范一下"会让一次本来能命中的读变成 404，而 404 说的是"没有这张单"。
func (c *BillController) pathKey(ctx *gin.Context, raw, label string) (string, bool) {
	key := strings.TrimSpace(raw)
	if key == "" {
		response.Error(ctx, http.StatusBadRequest,
			label+"为空：这一条端点必须指名读哪一张应收，空键会退化成一次没有条件的读",
			gin.H{"reason": billReasonInputInvalid})
		return "", false
	}
	if len(key) > billKeyMaxLen {
		response.Error(ctx, http.StatusBadRequest,
			label+" 长度 "+strconv.Itoa(len(key))+" 超上限 "+strconv.Itoa(billKeyMaxLen)+"（"+opportunityEllipsis(key)+"）",
			gin.H{"reason": billReasonInputInvalid})
		return "", false
	}
	return key, true
}

// replyNotFound 两把键都是调用方递进来的，服务层的原文里带着它，原样透出才修得动。
func (c *BillController) replyNotFound(ctx *gin.Context, err error) {
	response.Error(ctx, http.StatusNotFound, err.Error(), gin.H{"reason": billReasonNotFound})
}

// replyReadError 读侧的哨兵分诊。
//
// 只有四档，且**没有 409**：读不会撞并发、不会撞状态机，任何"你先去处置一下再来"
// 在这里都是把一次查询写成了一个流程。顺序与派生侧同一条（未装配先判，
// 因为那一支意味着其余判据都没跑）。
//
// 与派生侧共用 replyNotFound / unavailable 两个出口：同一个事实只该有一种 JSON 形状，
// 两处各写一遍的话，其中一处改了 reason 就是"同一个 404 在两条端点上名字不同"。
func (c *BillController) replyReadError(ctx *gin.Context, err error, key string) {
	switch {
	case errors.Is(err, service.ErrPaymentServiceUnavailable):
		c.unavailable(ctx, msgReadUnavailable)
	case errors.Is(err, service.ErrPaymentInputInvalid):
		response.Error(ctx, http.StatusBadRequest, err.Error(), gin.H{"reason": billReasonInputInvalid})
	case errors.Is(err, service.ErrPaymentBillNotFound):
		c.replyNotFound(ctx, err)
	default:
		// 同派生侧：读这条腿的报错会带着表名与 SQL 片段（statementOfBill 里两处求和失败）。
		logger.Warnf("[Bill] 读账单/对账 %q 遇到未预期错误: %v", key, err)
		response.Error(ctx, http.StatusInternalServerError, "读账单失败（底座或数据异常），本次未返回任何对账结果",
			gin.H{"reason": billReasonInternal})
	}
}
