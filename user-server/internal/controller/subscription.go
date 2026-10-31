package controller

import (
	"errors"

	"github.com/gin-gonic/gin"
	bizerr "hivemtk-user/internal/domain/errors"
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
		response.ErrorWithBusinessCode(ctx, bizerr.CodeInternal, err.Error(), gin.H{})
	}
}

// @Summary      查询订阅状态（本地镜像，不回源平台）
// @Tags      subscription
// @Router    /api/subscription/status [get]
// Status GET /api/subscription/status
//
// 读镜像 + 本地惰性判定（与平台 CurrentStatus 同款），不回源平台 ——
// 平台不可达时镜像最后已知态照常可读，续费墙不会因为断网打不开。
func (c *SubscriptionController) Status(ctx *gin.Context) {
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
