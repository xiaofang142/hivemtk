// quote.go T-P6-03：报价域的 HTTP 出口（发送必经审批检查点 + 让那一格结论读得出来）。
//
// 本层只做三件事，且刻意不做第四件（同 controller/opportunity.go 的分工）：
//  1. **形状**：请求体 → service 的入参结构，以及 CLAUDE.md 那套 {code,data,message} 契约。
//     绑定一律拒收未知字段，理由在 generate 那一条上比商机侧更硬：报价的入参面一旦容得下
//     `content`（话术正文）或 `one_id`（收件人），AC① 的"话术必经版本/灰度"与
//     "收件人由报价自己说"两条判据就同时失效，而库里那行看起来与走正路的一模一样。
//  2. **状态码 + 分诊码**：四种处置（awaiting / sent / rejected / expired）与十三个哨兵
//     各自对应一种**修法不同**的失败。HTTP 状态码分不开那些同码不同病的（三个 409
//     的处置动作是"等审批人""重取这一版""去补行项目"），所以每个错误都随 `data.reason`
//     出一个机器可读的判据词。
//  3. **装配回显**：任一条腿没装配时相关端点回 503，且读口的 `approval_lookup` 说清
//     "没问"与"问了没有"是两件事。
//
// 本层**不做**的第四件：审批、闸门、乐观锁、金额、话术解析全在服务层，这里一处不重写；
// 也不猜"谁批的"——只有 `Operator` 这一格必须来自会话（sales_events.owner_id 的"是谁点的发送"），
// 拿不到就 401 关在门外，而不是递一个空串让服务层去拒（那会被翻成 400，
// 而该做的事是刷会话不是改载荷）。
//
// 两条接缝（QuoteViewReader / QuoteSender）是接口而不是具体类型，判据有两条：
// 出口能看见的方法集合是可钉的白名单（见测试的第七组），以及"点发送的人自己批"
// 这件事在类型上就该写不出来 —— 发接缝里没有 Decide / Submit。
package controller

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

// QuoteViewReader 读侧与生成侧的接缝。*service.QuoteService 天然满足。
//
// 只有三格业务方法：读一版、读链上最新、生成第一版。**没有** Revise / ViewAt / ListVersions，
// 也**没有**任何写状态的方法：还价与按版本回读今天没有出口（登记给 T-P6-04），
// 而 GET 里能改状态是比"多一个方法"更坏的那件事。
type QuoteViewReader interface {
	Available() bool
	Generate(ctx context.Context, in service.QuoteGenerateInput) (*service.QuoteView, error)
	View(ctx context.Context, id string) (*service.QuoteView, error)
	LatestView(ctx context.Context, quoteID string) (*service.QuoteView, error)
}

// QuoteSender 发送腿的接缝。*service.QuoteSendService 天然满足。
//
// 刻意不含 `Decide`：那一格在这里出现，"点发送的人自己批"就成了可能 ——
// 而这条链的全部意义就是发的人与批的人不是同一个人。
// `OpenApproval` 是纯读（找回自己那条待办），它是 AC② 那一档"停在 pending 可查"的落点。
type QuoteSender interface {
	Available() bool
	Send(ctx context.Context, in service.QuoteSendInput) (*service.QuoteSendResult, error)
	OpenApproval(ctx context.Context, quoteRowID string) (*model.ApprovalRequest, error)
}

var (
	_ QuoteViewReader = (*service.QuoteService)(nil)
	_ QuoteSender     = (*service.QuoteSendService)(nil)
)

// QuoteController 报价域控制器。两条腿各自可为 nil（未装配），缺哪条哪条回 503。
type QuoteController struct {
	reads QuoteViewReader
	sends QuoteSender
}

// NewQuoteController 构造。
func NewQuoteController(reads QuoteViewReader, sends QuoteSender) *QuoteController {
	return &QuoteController{reads: reads, sends: sends}
}

// RegisterRoutes 挂到**已鉴权**的 /api 组下（路径按 CLAUDE.md 的 /api/{domain}/{resource}）。
//
// 没有 `POST /:id/won` 那一条：接受报价（accepted）是**客户**的动作，不是销售的按钮，
// 而它的判据属于 P7（回款派生）。这里开出任何写 accepted 的口，
// 就等于把"客户点了同意"这件事变成"销售点了同意"。
func (c *QuoteController) RegisterRoutes(router *gin.RouterGroup) {
	g := router.Group("/quote")
	{
		g.POST("", c.Generate)
		g.GET("/latest/:quoteID", c.Latest)
		g.GET("/:id", c.Get)
		g.POST("/:id/send", c.Send)
	}
}

const (
	// quoteIDParamMaxLen 行键（q_<unixnano>_<seq>）与逻辑号（QT-<b32>-<b32>）都用不到 30；
	// 上限是为了下面把 id 回显进提示里：无上限的回显等于一个免费的响应体放大器。
	quoteIDParamMaxLen = 64
	// quoteBodyMaxBytes 请求体上限。比商机侧（4KB）宽一档，因为生成入参带一张行项目数组；
	// 一份 20 行的覆盖清单约 2KB，8KB 留的是余量而不是许可。
	quoteBodyMaxBytes = 8 << 10
	// quoteErrMaxRunes 透出的底层错误文本长度上限（回显要有界）。
	quoteErrMaxRunes = 200
)

// quoteReason* 错误响应里 data.reason 的取值集合。与 HTTP 状态码一起构成完整判据：
// 状态码给"这一类请求要不要重发"，reason 给"具体改哪一处"。
const (
	quoteReasonUnauthenticated  = "unauthenticated"
	quoteReasonInputInvalid     = "input_invalid"
	quoteReasonNotFound         = "not_found"
	quoteReasonGateClosed       = "gate_closed"
	quoteReasonNotDraft         = "not_draft"
	quoteReasonApprovalMismatch = "approval_mismatch"
	quoteReasonApprovalNotFound = "approval_not_found"
	quoteReasonApprovalRejected = "approval_rejected"
	quoteReasonApprovalExpired  = "approval_expired"
	quoteReasonRecipientMissing = "recipient_missing"
	quoteReasonLinesMissing     = "lines_missing"
	quoteReasonScriptNA         = "script_unavailable"
	quoteReasonTemplateMissing  = "template_missing"
	quoteReasonTemplateInvalid  = "template_invalid"
	quoteReasonStatusStuck      = "status_stuck"
	quoteReasonOutboundFailed   = "outbound_failed"
	quoteReasonVerdictUnknown   = "verdict_unknown"
	quoteReasonUnavailable      = "unavailable"
	quoteReasonInternal         = "internal"
)

// approval_lookup 那一格的四个取值。见 quoteReadView 的注释。
const (
	quoteApprovalFound      = "found"
	quoteApprovalNone       = "none"
	quoteApprovalReadFailed = "read_failed"
	quoteApprovalNotAsked   = "unavailable"
)

// Generate POST /api/quote —— 按模板落一版草稿，返回里的 id 就是后面要发送的那一版。
//
// @Summary      由商机生成一版报价草稿
// @Description  话术取自生效版本、金额取自模板，两者都不是入参；未过闸门/没有生效话术时一行都不写
// @Tags         Quote
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body   body    service.QuoteGenerateInput  true  "商机号 + 模板代号 + 可选币种/有效期/行覆盖"
// @Success      200    {object}  response.Response  "成功（data 为报价视图，id 是版本行键）"
// @Failure      400    {object}  response.Response  "入参不合法（含未知字段：正文与收件人不是入参）"
// @Failure      404    {object}  response.Response  "来源商机不存在"
// @Failure      409    {object}  response.Response  "闸门未开 / 模板未配或读不出 / 话术无生效版本"
// @Failure      503    {object}  response.Response  "底座未装配"
// @Router       /api/quote [post]
func (c *QuoteController) Generate(ctx *gin.Context) {
	if !c.readsAvailable() {
		c.unavailable(ctx, "报价")
		return
	}
	var in service.QuoteGenerateInput
	if !c.bindJSON(ctx, &in) {
		return
	}
	view, err := c.reads.Generate(ctx.Request.Context(), in)
	if err != nil {
		c.replyError(ctx, err, "生成报价")
		return
	}
	if view == nil {
		// 服务层"要么给视图要么给错"，走到这里就是实现漂了：按失败报，不回一个空对象。
		c.replyError(ctx, errors.New("生成报价返回了空结果而没有给错误"), "生成报价")
		return
	}
	response.Success(ctx, view, "ok")
}

// quoteReadView 读端点的响应物：视图 + 这一版当前开着的审批。
//
// 内嵌 *QuoteView 而不是抄一遍字段：抄的那一份与服务层那份会分家，
// 而分家之后"报价单上有什么"取决于两边各自更新到哪一步。
//
// `approval_lookup` 与 `open_approval` 分两格是因为"没有开放审批"与"没去问"
// 是两个必须能分开的事实：前者是业务结论（这一版没人审），
// 后者是装配/故障结论。只给 open_approval 的话，null 同时装着那两件事，
// 而前端对它们的渲染正好相反（一个是"等审批人"，一个是"这系统起不来"）。
type quoteReadView struct {
	*service.QuoteView
	OpenApproval   *quoteOpenApproval `json:"open_approval,omitempty"`
	ApprovalLookup string             `json:"approval_lookup"`
}

// quoteOpenApproval 开放审批对外的那几格。
//
// 逐格挑而不是直接返回 *model.ApprovalRequest：那一行里有 ResumeToken。
// 今天它靠 `json:"-"` 出不了门，但"整行交出去"这个写法会把安全性押在下一位改模型的人
// 记得给新加的凭证列打标签上 —— 而凭证泄漏的响应面是每一次读。
type quoteOpenApproval struct {
	ApprovalID string     `json:"approval_id"`
	Status     string     `json:"status"`
	PolicyKey  string     `json:"policy_key"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	DecidedBy  string     `json:"decided_by,omitempty"`
	DecidedAt  *time.Time `json:"decided_at,omitempty"`
}

// Get GET /api/quote/:id —— 按版本行键读一版。
//
// @Summary      读取单版报价（含这一版当前开着的审批）
// @Tags         Quote
// @Produce      json
// @Security     BearerAuth
// @Param        id     path    string  true  "版本行主键 quotes.id"
// @Success      200    {object}  response.Response  "成功"
// @Failure      400    {object}  response.Response  "id 形状不合法"
// @Failure      404    {object}  response.Response  "报价版本不存在"
// @Failure      503    {object}  response.Response  "底座未装配"
// @Router       /api/quote/{id} [get]
func (c *QuoteController) Get(ctx *gin.Context) {
	id, ok := c.readParam(ctx)
	if !ok {
		return
	}
	view, err := c.reads.View(ctx.Request.Context(), id)
	c.replyView(ctx, view, err)
}

// Latest GET /api/quote/latest/:quoteID —— 链上版本号最大的那一版。
//
// 存在的理由很具体：sales_events.quote_id 与发给客户的那句正文里带的都是**逻辑号**，
// 而审批与发送要的是版本行键。只有按行键读的话，事后从一条事件回到"当时发出去的是哪一版"
// 就只能去翻日志。
//
// @Summary      读取某张报价单的最新版本
// @Tags         Quote
// @Produce      json
// @Security     BearerAuth
// @Param        quoteID  path    string  true  "报价逻辑号 quotes.quote_id"
// @Success      200      {object}  response.Response  "成功"
// @Failure      400      {object}  response.Response  "编号形状不合法"
// @Failure      404      {object}  response.Response  "报价链不存在"
// @Failure      503      {object}  response.Response  "底座未装配"
// @Router       /api/quote/latest/{quoteID} [get]
func (c *QuoteController) Latest(ctx *gin.Context) {
	if !c.readsAvailable() {
		c.unavailable(ctx, "报价")
		return
	}
	quoteID, ok := c.pathParam(ctx, "quoteID", "报价编号")
	if !ok {
		return
	}
	view, err := c.reads.LatestView(ctx.Request.Context(), quoteID)
	c.replyView(ctx, view, err)
}

// quoteSendBody 发送入参：**只有**审批号这一格。
//
// 不内嵌 service.QuoteSendInput 是刻意的：那一格里有 QuoteRowID 与 Operator。
// 前者从请求体进来就等于 `POST /quote/A/send {"quote_row_id":"B"}` —— 路径与体各说一个对象，
// 而批准是按对象给的，两次读其中任一份都能给别的报价开门；
// 后者从体里进来等于"谁点的发送"由调用方自报，审计列当场作废。
type quoteSendBody struct {
	ApprovalID string `json:"approval_id"`
}

// Send POST /api/quote/:id/send —— 走一次发送（两阶段：先开待办，批完凭号再来）。
//
// 四种处置各回一格：200 已发出、202 已开待办在等结论、409 有人说了不、409 没人说窗口过去了。
// 202 那一档是 AC② 的形状：本次请求到此结束，不睡、不轮询，续跑靠调用方带着 approval_id 回来
// （或者先 GET 这一版看 open_approval）。
//
// @Summary      发送一版报价（未过审批则只开一条待办）
// @Description  审批未放行时回 202 并带回审批号，本次不外发；放行后重新解析生效话术、认领 draft→sent 再交给出域出口
// @Tags         Quote
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id     path    string            true  "版本行主键 quotes.id"
// @Param        body   body    quoteSendBody     false "可选：手里已有那条结论时带上审批号"
// @Success      200    {object}  response.Response  "已发送"
// @Success      202    {object}  response.Response  "已开待办，等待裁决（data.disposition=awaiting）"
// @Failure      400    {object}  response.Response  "id 或请求体形状不合法"
// @Failure      401    {object}  response.Response  "会话里没有操作者身份"
// @Failure      404    {object}  response.Response  "报价版本不存在 / 审批号查无此号"
// @Failure      409    {object}  response.Response  "闸门未开 / 非草稿 / 审批与对象不符 / 无收件人 / 无行项目 / 话术失效 / 状态写不动"
// @Failure      502    {object}  response.Response  "外发失败且状态已退回草稿"
// @Failure      503    {object}  response.Response  "底座未装配"
// @Router       /api/quote/{id}/send [post]
func (c *QuoteController) Send(ctx *gin.Context) {
	if !c.sendsAvailable() {
		c.unavailable(ctx, "报价发送")
		return
	}
	id, ok := c.sendParam(ctx)
	if !ok {
		return
	}
	// 操作者取不到就到此为止：本层不查库、不开待办。
	// 反面的形状是"让服务层去判空"——那一路会把"你没登录"翻成 400（改载荷），
	// 而该做的事是刷会话；更坏的是审批已经入队，审批人批的是一件没人能重来的事。
	operator, ok := contextOperatorID(ctx)
	if !ok {
		response.Error(ctx, http.StatusUnauthorized,
			"发送报价必须带操作者身份（未登录或会话里没有 user id）",
			gin.H{"reason": quoteReasonUnauthenticated})
		return
	}
	var body quoteSendBody
	if !c.bindOptionalJSON(ctx, &body) {
		return
	}

	res, err := c.sends.Send(ctx.Request.Context(), service.QuoteSendInput{
		QuoteRowID: id, ApprovalID: strings.TrimSpace(body.ApprovalID), Operator: operator,
	})
	if err != nil {
		c.replyError(ctx, err, "发送报价")
		return
	}
	if res == nil {
		c.replyError(ctx, errors.New("发送报价返回了空结果而没有给错误"), "发送报价")
		return
	}
	switch res.Disposition {
	case service.QuoteSendSent:
		response.Success(ctx, res, "ok")
	case service.QuoteSendAwaiting:
		// 202 而不是 200：这一版既没发出去也没被拒，本次调用只完成了一半（开出了那条待办）。
		response.Accepted(ctx, res, "已提交审批，等待裁决")
	case service.QuoteSendRejected:
		response.Error(ctx, http.StatusConflict,
			"这条报价的审批被人说了不，本次未外发（库里那一版仍是草稿：内部驳回不等于客户拒收）",
			gin.H{"reason": quoteReasonApprovalRejected, "approval_id": res.ApprovalID, "disposition": res.Disposition})
	case service.QuoteSendExpired:
		response.Error(ctx, http.StatusConflict,
			"这条报价的审批到期了（没人说，窗口过去了），本次未外发",
			gin.H{"reason": quoteReasonApprovalExpired, "approval_id": res.ApprovalID, "disposition": res.Disposition})
	default:
		// 未知处置值不回任何状态码档位：猜哪一档都可能猜出"其实没发生"的那件事。
		logger.Warnf("[Quote] 未知处置值 %q: %v", res.Disposition, *res)
		response.Error(ctx, http.StatusInternalServerError, "发送报价返回了未知的处置值",
			gin.H{"reason": quoteReasonVerdictUnknown})
	}
}

// —— 关闸与入参 ——————————————————————————————————————————

func (c *QuoteController) readsAvailable() bool { return c.reads != nil && c.reads.Available() }

func (c *QuoteController) sendsAvailable() bool { return c.sends != nil && c.sends.Available() }

// pathParam 读一个路径参数并收住形状：去空白、判空、按上限判长度（回显必须截断）。
func (c *QuoteController) pathParam(ctx *gin.Context, name, label string) (string, bool) {
	v := strings.TrimSpace(ctx.Param(name))
	if v == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少"+label,
			gin.H{"reason": quoteReasonInputInvalid})
		return "", false
	}
	if len(v) > quoteIDParamMaxLen {
		response.Error(ctx, http.StatusBadRequest,
			label+" 长度 "+strconv.Itoa(len(v))+" 超上限 "+strconv.Itoa(quoteIDParamMaxLen)+"（"+opportunityEllipsis(v)+"）",
			gin.H{"reason": quoteReasonInputInvalid})
		return "", false
	}
	return v, true
}

// readParam 读端口的关闸：先看腿在不在，再收形状。
func (c *QuoteController) readParam(ctx *gin.Context) (string, bool) {
	if !c.readsAvailable() {
		c.unavailable(ctx, "报价")
		return "", false
	}
	return c.pathParam(ctx, "id", "报价版本行键")
}

// sendParam 同上，走发送腿那一道。
func (c *QuoteController) sendParam(ctx *gin.Context) (string, bool) {
	return c.pathParam(ctx, "id", "报价版本行键")
}

// bindOptionalJSON 允许**空体**的绑定（只给发送那一条用）。
//
// 空体在这里是合法入参而不是"忘了传"：发送的第一阶段就是"手里还没有结论"，
// `curl -X POST .../send` 不写体与写 `{}` 必须是同一个动作。
// 反面的做法是让调用方写 `{"approval_id":""}` —— 那等于允许"用一个空串自称带了结论"，
// 而两种写法走的是不同分支（前者开待办、后者按号读），一种语法两种语义正是旁路的形状。
// EOF 之外的一切（含未知字段、超体积、形状不对）仍然照原样拒。
func (c *QuoteController) bindOptionalJSON(ctx *gin.Context, target any) bool {
	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, quoteBodyMaxBytes)
	dec := json.NewDecoder(ctx.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		if errors.Is(err, io.EOF) {
			return true
		}
		return c.replyBindError(ctx, err)
	}
	return true
}

// replyBindError 两条绑定共用的报错出口：体积、空体、形状三种来路分开说。
func (c *QuoteController) replyBindError(ctx *gin.Context, err error) bool {
	msg := err.Error()
	// 体积那一刀要单列：MaxBytesError 混进"形状不对"里，运维读到的是"调用方字段写错了"，
	// 实际发生的是"这一格的请求体被封顶了" —— 两种情形的处置动作完全不同。
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		response.Error(ctx, http.StatusBadRequest,
			"请求体超过 "+strconv.Itoa(quoteBodyMaxBytes)+" 字节上限：一次生成最多是一张行项目覆盖清单，容不下更大的体",
			gin.H{"reason": quoteReasonInputInvalid})
		return false
	}
	if errors.Is(err, io.EOF) {
		response.Error(ctx, http.StatusBadRequest,
			"请求体为空：生成报价至少要带 opportunity_id 与 template_code",
			gin.H{"reason": quoteReasonInputInvalid})
		return false
	}
	response.Error(ctx, http.StatusBadRequest, "请求体形状不对："+opportunityTrimRunes(msg, quoteErrMaxRunes),
		gin.H{"reason": quoteReasonInputInvalid})
	return false
}

// bindJSON 严格绑定：拒收未知字段 + 请求体封顶（生成那条必须带体，空体算 400）。
//
// 与商机那一路同一套判据，两句提示文案各写各的：这里"未知字段"的代价不是"改了没生效"，
// 而是"话术/收件人/金额可以从体里递进来"，所以提示里要点名这一格为什么窄。
func (c *QuoteController) bindJSON(ctx *gin.Context, target any) bool {
	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, quoteBodyMaxBytes)
	dec := json.NewDecoder(ctx.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return c.replyBindError(ctx, err)
	}
	return true
}

// unavailable 503：带 reason，但绝不带空对象（判据同 opportunityController.unavailable）。
func (c *QuoteController) unavailable(ctx *gin.Context, what string) {
	response.Error(ctx, http.StatusServiceUnavailable,
		what+"底座未装配（缺少 DB 句柄或路由挂在了装配之前），本次未读到任何数据",
		gin.H{"reason": quoteReasonUnavailable})
}

// replyView 读端点共同收尾：视图 + 开放审批。
//
// 审批那一格读失败**不**把整个读请求打成错误：报价单在库里，客户等着看，
// 而"这一版有没有人审"只是附在旁边的第二件事。读失败要说成 read_failed，不许静默成 none。
func (c *QuoteController) replyView(ctx *gin.Context, view *service.QuoteView, err error) {
	if err != nil {
		c.replyError(ctx, err, "读取报价")
		return
	}
	if view == nil {
		response.Error(ctx, http.StatusNotFound, "报价不存在", gin.H{"reason": quoteReasonNotFound})
		return
	}
	out := quoteReadView{QuoteView: view, ApprovalLookup: quoteApprovalNotAsked}
	if c.sendsAvailable() {
		appr, aerr := c.sends.OpenApproval(ctx.Request.Context(), view.ID)
		switch {
		case aerr != nil:
			out.ApprovalLookup = quoteApprovalReadFailed
			logger.Warnf("[Quote] 读 %s 的开放审批失败: %v", view.ID, aerr)
		case appr == nil:
			out.ApprovalLookup = quoteApprovalNone
		default:
			out.ApprovalLookup = quoteApprovalFound
			out.OpenApproval = &quoteOpenApproval{
				ApprovalID: appr.ID, Status: appr.Status, PolicyKey: appr.PolicyKey,
				ExpiresAt: appr.ExpiresAt, DecidedBy: appr.DecidedBy, DecidedAt: appr.DecidedAt,
			}
		}
	}
	response.Success(ctx, out, "ok")
}

// replyError 把服务/仓储的 sentinel 翻成 (状态码, reason)。
//
// 顺序是判据的一部分，三处尤其要紧：
// ① Stuck+Outbound 的复合错必须先判，且不许落到 502 那一档 —— 502 的说法是
//
//	"渠道坏了，重试即可"，而这一格重试会被自己那行 sent 挡掉，照着 502 的语义重试的人
//	会在"库里说发过、客户没收到"上得出"已经发过了"。
//
// ② 未装配（两条腿各自可能缺件）先判，因为它意味着其余一切判据都没跑。
// ③ 入参不合法先于"不存在"：`{"currency":"人民币"}` 报成"报价不存在"会把人送去查数据。
func (c *QuoteController) replyError(ctx *gin.Context, err error, action string) {
	switch {
	case errors.Is(err, service.ErrQuoteSendOutboundFailed) && errors.Is(err, service.ErrQuoteSendStatusStuck):
		logger.Warnf("[Quote] %s：外发失败且状态没退回草稿，需人工核对 —— %v", action, err)
		response.Error(ctx, http.StatusInternalServerError,
			action+"失败，且报价状态没能退回草稿：这一版**该发而没发出去**，须人工核对后再动，不要盲目重发",
			gin.H{"reason": quoteReasonStatusStuck})
	case errors.Is(err, service.ErrQuoteServiceUnavailable):
		c.unavailable(ctx, "报价")
	case errors.Is(err, service.ErrQuoteSendInputInvalid), errors.Is(err, service.ErrQuoteInputInvalid):
		response.Error(ctx, http.StatusBadRequest, err.Error(), gin.H{"reason": quoteReasonInputInvalid})
	case errors.Is(err, service.ErrQuoteSendStatusStuck):
		response.Error(ctx, http.StatusConflict, err.Error(), gin.H{"reason": quoteReasonStatusStuck})
	case errors.Is(err, service.ErrQuoteSendOutboundFailed):
		response.Error(ctx, http.StatusBadGateway, err.Error(), gin.H{"reason": quoteReasonOutboundFailed})
	case errors.Is(err, service.ErrQuoteSendVerdictUnknown):
		logger.Warnf("[Quote] %s：结论未知 —— %v", action, err)
		response.Error(ctx, http.StatusInternalServerError, action+"失败（审批行的状态不在已知值域内，一律不放行）",
			gin.H{"reason": quoteReasonVerdictUnknown})
	case errors.Is(err, service.ErrApprovalNotFound):
		response.Error(ctx, http.StatusNotFound, err.Error(), gin.H{"reason": quoteReasonApprovalNotFound})
	case errors.Is(err, service.ErrQuoteSendApprovalMismatch):
		response.Error(ctx, http.StatusConflict, err.Error(), gin.H{"reason": quoteReasonApprovalMismatch})
	case errors.Is(err, service.ErrQuoteGateClosed):
		response.Error(ctx, http.StatusConflict, err.Error(), gin.H{"reason": quoteReasonGateClosed})
	case errors.Is(err, service.ErrQuoteSendNotDraft):
		response.Error(ctx, http.StatusConflict, err.Error(), gin.H{"reason": quoteReasonNotDraft})
	case errors.Is(err, service.ErrQuoteSendRecipientMissing):
		response.Error(ctx, http.StatusConflict, err.Error(), gin.H{"reason": quoteReasonRecipientMissing})
	case errors.Is(err, service.ErrQuoteSendLinesMissing):
		response.Error(ctx, http.StatusConflict, err.Error(), gin.H{"reason": quoteReasonLinesMissing})
	case errors.Is(err, service.ErrQuoteScriptUnavailable):
		response.Error(ctx, http.StatusConflict, err.Error(), gin.H{"reason": quoteReasonScriptNA})
	case errors.Is(err, service.ErrQuoteTemplateMissing):
		response.Error(ctx, http.StatusConflict, err.Error(), gin.H{"reason": quoteReasonTemplateMissing})
	case errors.Is(err, service.ErrQuoteTemplateInvalid):
		response.Error(ctx, http.StatusConflict, err.Error(), gin.H{"reason": quoteReasonTemplateInvalid})
	case errors.Is(err, service.ErrQuoteOpportunityMissing), errors.Is(err, service.ErrQuoteVersionMissing):
		response.Error(ctx, http.StatusNotFound, err.Error(), gin.H{"reason": quoteReasonNotFound})
	default:
		// 500 的文案**不透出**底层错误串：仓储报错可能带着 SQL 片段与列名。
		logger.Warnf("[Quote] %s 未预期错误: %v", action, err)
		response.Error(ctx, http.StatusInternalServerError, action+"失败（底座或数据异常）",
			gin.H{"reason": quoteReasonInternal})
	}
}
