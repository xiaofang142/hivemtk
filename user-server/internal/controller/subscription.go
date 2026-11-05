package controller

import (
	"errors"

	"github.com/gin-gonic/gin"
	bizerr "hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

// SubscriptionController 订阅状态与续费（003 T018/T022 用户端）
//
// 两条路由都必须在订阅闸白名单内（/api/subscription/*）——
// 墙页依赖它们拿状态、下单，若它们自己 4305 就是死循环（oracle P1-12）。
type SubscriptionController struct {
	svc *service.SubscriptionMirrorService
}

// NewSubscriptionController 构造
func NewSubscriptionController() *SubscriptionController {
	return &SubscriptionController{svc: service.NewSubscriptionMirrorService()}
}

func subscriptionFail(ctx *gin.Context, err error) {
	var be *bizerr.BizError
	if errors.As(err, &be) {
		response.ErrorWithBusinessCode(ctx, be.Code, be.Message, gin.H{})
		return
	}
	switch {
	case errors.Is(err, service.ErrPlatformUnavailable):
		response.ErrorWithBusinessCode(ctx, bizerr.CodeModulePlatformUnavail,
			"平台未连接，无法办理续费。请在系统设置中配置平台地址后重试。", gin.H{})
	default:
		logger.Errorf("subscription service error: %v", err)
		response.ErrorWithBusinessCode(ctx, bizerr.CodeInternal, "操作失败，请稍后重试", gin.H{})
	}
}

// @Summary      查询订阅状态（先回源刷新镜像，失败回落本地镜像）
// @Tags      subscription
// @Router    /api/subscription/status [get]
// Status GET /api/subscription/status
//
// 先按 TTL 回源平台刷新**本租户**镜像，再读镜像 + 本地惰性判定（与平台
// CurrentStatus 同款）。回源失败只 Warn、照常返回镜像最后已知态 ——
// 平台不可达时续费墙不会因为断网打不开。
//
// 回源是必需的：后台周期同步只覆盖默认租户，其他租户的镜像只在它自己触发
// 同步时才更新；平台确认收款后没有别的通道会通知本租户（见 Refresh 注释）。
func (c *SubscriptionController) Status(ctx *gin.Context) {
	if err := c.svc.Refresh(ctx.Request.Context()); err != nil {
		logger.Warnf("[subscription] 状态回源刷新失败（沿用镜像最后已知态）: %v", err)
	}
	snap, err := c.svc.Snapshot(ctx.Request.Context())
	if err != nil {
		subscriptionFail(ctx, err)
		return
	}
	response.Success(ctx, snap, "ok")
}

// @Summary POST /api/subscription/renew
// @Tags      subscription
// @Router    /api/subscription/renew [post]
// Renew POST /api/subscription/renew
//
// 续费下单（代理平台 MerchantCreate type=subscription）→ 返回 payment 参数。
// expired 态放行（平台修订 #8：续费是恢复通道）。
func (c *SubscriptionController) Renew(ctx *gin.Context) {
	var in struct {
		ClientOrderNo string `json:"client_order_no"`
		ContactName   string `json:"contact_name"`
		ContactPhone  string `json:"contact_phone"`
		ContactEmail  string `json:"contact_email"`
		Remark        string `json:"remark"`
	}
	if err := ctx.ShouldBindJSON(&in); err != nil {
		response.Error(ctx, 400, "参数错误: "+err.Error())
		return
	}
	res, err := c.svc.Renew(withBuyerIP(ctx),
		in.ClientOrderNo, in.ContactName, in.ContactPhone, in.ContactEmail, in.Remark)
	if err != nil {
		subscriptionFail(ctx, err)
		return
	}
	msg := "续费订单已提交，请完成支付"
	if res.Duplicate {
		msg = "续费订单已存在（幂等命中），请勿重复下单"
	}
	response.Success(ctx, res, msg)
}
