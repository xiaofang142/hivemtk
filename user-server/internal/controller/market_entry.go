package controller

import (
	"errors"

	"github.com/gin-gonic/gin"
	bizerr "hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

// MarketEntryController 模板/技能市场写侧闭环（specs/013 S-22：发布-审核-支付）。
//
// 三档：
//   - 提审/我的/下单：登录档（发布者与买家是登录用户）；
//   - 审核台/收款确认：admin 档（路由侧 AdminAuthMiddleware 收口）；
//   - 橱窗只读（List/Categories/Detail）在 TemplateMarketController，不在此处。
type MarketEntryController struct {
	svc *service.MarketEntryService
}

// NewMarketEntryController 构造。
func NewMarketEntryController() *MarketEntryController {
	return &MarketEntryController{svc: service.NewMarketEntryService()}
}

// marketEntryFail 写侧错误 → 业务码映射（查无/状态/重复类不落 500）。
func marketEntryFail(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrMarketEntryNotFoundInDB), errors.Is(err, service.ErrMarketOrderNotFound):
		response.ErrorWithBusinessCode(ctx, bizerr.CodeNotFound, err.Error(), gin.H{})
	case errors.Is(err, service.ErrMarketEntryStateNotAllowed), errors.Is(err, service.ErrMarketOrderStateNotAllowed),
		errors.Is(err, service.ErrMarketOrderDuplicate), errors.Is(err, service.ErrMarketEntryDuplicate):
		response.ErrorWithBusinessCode(ctx, bizerr.CodeConflict, err.Error(), gin.H{})
	case errors.Is(err, service.ErrMarketOrderNoContact),
		errors.Is(err, service.ErrMarketEntryInvalidParam):
		response.ErrorWithBusinessCode(ctx, bizerr.CodeParamInvalid, err.Error(), gin.H{})
	default:
		response.ErrorWithBusinessCode(ctx, bizerr.CodeInternal, err.Error(), gin.H{})
	}
}

// Submit POST /api/template-market/entries —— 发布入口：提审目录条目（进审核台）。
func (c *MarketEntryController) Submit(ctx *gin.Context) {
	var in service.MarketEntryInput
	if err := ctx.ShouldBindJSON(&in); err != nil {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeParamInvalid, "参数解析失败："+err.Error(), gin.H{})
		return
	}
	row, err := c.svc.Submit(ctx.Request.Context(), int64(getUserIDFromContext(ctx)), in)
	if err != nil {
		marketEntryFail(ctx, err)
		return
	}
	response.Success(ctx, row, "已提交审核")
}

// MySubmissions GET /api/template-market/my/submissions —— 我的发布（提审记录与状态）。
func (c *MarketEntryController) MySubmissions(ctx *gin.Context) {
	rows, err := c.svc.ListMySubmissions(ctx.Request.Context(), int64(getUserIDFromContext(ctx)))
	if err != nil {
		marketEntryFail(ctx, err)
		return
	}
	response.Success(ctx, gin.H{"list": rows, "total": len(rows)}, "ok")
}

// MyOrders GET /api/template-market/my/orders —— 我的订单（含待支付/已支付/已取消）。
func (c *MarketEntryController) MyOrders(ctx *gin.Context) {
	rows, err := c.svc.ListMyOrders(ctx.Request.Context(), int64(getUserIDFromContext(ctx)))
	if err != nil {
		marketEntryFail(ctx, err)
		return
	}
	response.Success(ctx, gin.H{"list": rows, "total": len(rows)}, "ok")
}

// MyOwned GET /api/template-market/my/owned —— 我的已获得（订单已支付的条目）。
func (c *MarketEntryController) MyOwned(ctx *gin.Context) {
	rows, err := c.svc.ListOwnedEntriesWithMeta(ctx.Request.Context(), int64(getUserIDFromContext(ctx)))
	if err != nil {
		marketEntryFail(ctx, err)
		return
	}
	response.Success(ctx, gin.H{"list": rows, "total": len(rows)}, "ok")
}

// CreateOrder POST /api/template-market/entries/:id/order —— 下单（免费直通/付费待支付）。
func (c *MarketEntryController) CreateOrder(ctx *gin.Context) {
	var in service.MarketEntryOrderInput
	if err := ctx.ShouldBindJSON(&in); err != nil {
		// 无 body 的下单（免费条目领取）合法：按零值处理。
		in = service.MarketEntryOrderInput{}
	}
	res, err := c.svc.CreateOrder(ctx.Request.Context(), int64(getUserIDFromContext(ctx)), ctx.Param("id"), in)
	if err != nil {
		marketEntryFail(ctx, err)
		return
	}
	response.Success(ctx, res, "下单成功")
}

// CancelOrder POST /api/template-market/orders/:order_no/cancel —— 买家撤单。
func (c *MarketEntryController) CancelOrder(ctx *gin.Context) {
	row, err := c.svc.CancelOrder(ctx.Request.Context(), int64(getUserIDFromContext(ctx)), ctx.Param("order_no"))
	if err != nil {
		marketEntryFail(ctx, err)
		return
	}
	response.Success(ctx, row, "订单已取消")
}

// ── 审核台（admin 档）──────────────────────────────────────────────────────

// ReviewList GET /api/template-market/admin/entries?status= —— 审核台队列。
func (c *MarketEntryController) ReviewList(ctx *gin.Context) {
	rows, err := c.svc.ListForReview(ctx.Request.Context(), ctx.Query("status"))
	if err != nil {
		marketEntryFail(ctx, err)
		return
	}
	response.Success(ctx, gin.H{"list": rows, "total": len(rows)}, "ok")
}

// marketEntryReviewAction 审核台动作值域。
const marketEntryReviewAction = "approve|reject|disable|enable"

// ReviewAction POST /api/template-market/admin/entries/:id/review —— 审核/下架/复架。
// body: {action, reason}
func (c *MarketEntryController) ReviewAction(ctx *gin.Context) {
	var in struct {
		Action string `json:"action"`
		Reason string `json:"reason"`
	}
	if err := ctx.ShouldBindJSON(&in); err != nil {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeParamInvalid, "参数解析失败："+err.Error(), gin.H{})
		return
	}
	id := ctx.Param("id")
	operator := int64(getUserIDFromContext(ctx))
	var (
		row any
		err error
	)
	switch in.Action {
	case "approve":
		row, err = c.svc.Review(ctx.Request.Context(), operator, id, true, "")
	case "reject":
		row, err = c.svc.Review(ctx.Request.Context(), operator, id, false, in.Reason)
	case "disable":
		row, err = c.svc.Disable(ctx.Request.Context(), operator, id)
	case "enable":
		row, err = c.svc.Enable(ctx.Request.Context(), operator, id)
	default:
		response.ErrorWithBusinessCode(ctx, bizerr.CodeParamInvalid,
			"action 必须是 "+marketEntryReviewAction, gin.H{})
		return
	}
	if err != nil {
		marketEntryFail(ctx, err)
		return
	}
	response.Success(ctx, row, "已处理")
}

// AdminOrders GET /api/template-market/admin/orders?status= —— 管理端订单列表。
func (c *MarketEntryController) AdminOrders(ctx *gin.Context) {
	rows, err := c.svc.ListAllOrders(ctx.Request.Context(), ctx.Query("status"))
	if err != nil {
		marketEntryFail(ctx, err)
		return
	}
	response.Success(ctx, gin.H{"list": rows, "total": len(rows)}, "ok")
}

// MarkPaid POST /api/template-market/admin/orders/:order_no/mark-paid —— 收款确认。
func (c *MarketEntryController) MarkPaid(ctx *gin.Context) {
	row, err := c.svc.MarkPaid(ctx.Request.Context(), int64(getUserIDFromContext(ctx)), ctx.Param("order_no"))
	if err != nil {
		marketEntryFail(ctx, err)
		return
	}
	response.Success(ctx, row, "已确认收款")
}
