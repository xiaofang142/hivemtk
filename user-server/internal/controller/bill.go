// bill.go T-P7-01：账单域的 HTTP 出口（把"这一版成交了"翻成一张应收）。
//
// 本层只做三件事（与 controller/quote.go 的分工同一条）：
//  1. **形状**：请求体 → service.BillDeriveInput。绑定拒收未知字段，且这里比报价侧更硬：
//     体里若能递进 `amount`，AC② 的"账单金额与报价合计一致"就当场失去对账对象 ——
//     服务层的入参结构里没有那一格（由反射用例钉成白名单），但宽容的绑定会把递进来的
//     金额**静默丢掉**，于是"我传了 5000 而系统按 369.99 记账"在调用方与系统两边都看起来是对的。
//  2. **状态码 + 分诊码**：八个哨兵各自对应一种**修法不同**的失败（改载荷 / 换行号 /
//     先去处置原账单 / 重读那一版再来），所以每个错误都随 `data.reason` 出一个机器可读判据词。
//     HTTP 状态码分不开其中三种 409，reason 分得开。
//  3. **装配回显**：派生腿没装配时回 503，data 不给空对象。
//
// 本层**不做**的事，逐条都有归宿而不是漏写：
//   - 不读账单、不列账单、不改账单状态（无 GET / 无 PUT / 无 /pay / 无 /void）：
//     结清与作废要读回款行，而回款在 T-P7-02；逾期与催收在 T-P7-03。
//     今天开出任何一格"把账单改成已收"，就是给财务台账开一条**不走回款**的捷径。
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
// 用反射钉住）：**没有**作废、标记已收、改状态，也没有任何读口。
// 那两格写状态的入口在 T-P7-02（回款累计到位才算结清），读口在 T-P7-03 的视图，
// 现在写出来就是没有判据的写口与没有读方的读口。
// （这一段刻意不写方法名字面量：bill_routes_test.go 的静态锁按**原文**扫本文件，
// 写了禁用词的用注释豁免自己，等于把那道门锁成只有注释能开的样子。）
type BillDeriver interface {
	Available() bool
	DeriveFromQuote(ctx context.Context, in service.BillDeriveInput) (*service.BillView, error)
}

var _ BillDeriver = (*service.BillService)(nil)

// BillController 账单域控制器。派生腿可为 nil（未装配），此时端点回 503。
type BillController struct {
	derive BillDeriver
}

// NewBillController 构造。
func NewBillController(derive BillDeriver) *BillController {
	return &BillController{derive: derive}
}

// Available 报告派生腿在不在（装配回显与测试用；nil 接收者同样答得出来，不 panic）。
func (c *BillController) Available() bool {
	return c != nil && c.derive != nil && c.derive.Available()
}

// RegisterRoutes 挂到**已鉴权**的 /api 组下（路径按 CLAUDE.md 的 /api/{domain}/{resource}）。
//
// 只有一条端点。为什么成交确认挂在 /api/bill 而不是 /api/quote/:id/accept：
// 报价控制器当年刻意没开 accept 那一条（controller/quote.go 的 RegisterRoutes 注释，
// 而 router/quote_routes_test.go 用 404 探针把它钉成了静态锁）。那句话今天仍然成立，
// 变的是它有了后果——点这一格的人同时在开一张应收。把端点放在账单域，
// 是让"这是记账动作"这件事写在 URL 上，而不是靠注释提醒。
func (c *BillController) RegisterRoutes(router *gin.RouterGroup) {
	g := router.Group("/bill")
	{
		g.POST("", c.Derive)
	}
}

const (
	// billRowIDMaxLen 版本行键（q_<unixnano>_<seq>）用不到 30；上限是为了把行号回显进提示里
	// 时有界，且 bills.id 会被抄进 payments.bill_id（varchar(64)，T-P7-02）——同宽是下限。
	billRowIDMaxLen = 64
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
		c.unavailable(ctx)
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
func (c *BillController) unavailable(ctx *gin.Context) {
	response.Error(ctx, http.StatusServiceUnavailable,
		"账单底座未装配（缺少 DB 句柄或路由挂在了装配之前），本次未派生任何账单",
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
		c.unavailable(ctx)
	case errors.Is(err, service.ErrBillInputInvalid):
		response.Error(ctx, http.StatusBadRequest, err.Error(), gin.H{"reason": billReasonInputInvalid})
	case errors.Is(err, service.ErrBillQuoteNotFound):
		response.Error(ctx, http.StatusNotFound, err.Error(), gin.H{"reason": billReasonNotFound})
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
