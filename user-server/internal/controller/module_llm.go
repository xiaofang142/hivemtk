package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"
	bizerr "hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/platform"
	"hivemtk-user/internal/service"
)

// 本文件补齐「功能模块市场」在 user-web 里被直接调用、但路由文件里另有归属的那几条：
// 付款指引、监管告知、我的 LLM 余额/套餐/充值下单。
//
// 它们与 module_market.go 分文件的理由：那一支是"市场浏览与开通"，
// 这一支是"平台代付能力的账"。LLM 计量是收费模块（llm-cloud），查余额与
// 买套餐**要过闸** —— middleware/module_route_supplement.go 刻意把
// /api/llm/wallet|wallet/*|plans 归到 llm-cloud（上一版注释写的"不能被闸拦"
// 与补充表相矛盾，以补充表为准）。
//
// 这不会形成死锁，因为激活链路是另一条、且始终开着：
//
//	未订阅 → 4301 "去功能模块市场开通" → 市场与下单入口（/api/module-market/*、
//	POST /api/llm/recharge）**不在闸表里**，任何人都能点 → 下单 → 平台确认
//	收款 → 激活 llm-cloud → 钱包/套餐接口放行。
//
// 也就是说：wallet/plans 是"开通后才看账"的门，而"开通"这扇门从不被 llm-cloud
// 自己锁住 —— 买完就能看，不买就看到前端空态页（余额 0/无套餐）+4301 引导。

// @Summary      获取模块付费引导文案（平台配置）
// @Tags      module-market
// @Router    /api/module-market/payment-guide [get]
// PaymentGuide GET /api/module-market/payment-guide
//
// 文案来自平台端配置（site_contact_config），运营换联系方式后用户端自动跟着变。
// 平台不可达时给固定兜底文案 —— 这里绝不能返回空串：
// "订单已提交"后面没有联系方式，用户就不知道找谁，流程到此断掉。
func (c *ModuleMarketController) PaymentGuide(ctx *gin.Context) {
	svc := service.NewModuleCatalogService()
	guide, err := svc.PaymentGuide(ctx.Request.Context())
	if err != nil {
		moduleMarketFail(ctx, err)
		return
	}
	response.Success(ctx, gin.H{"payment_guide": guide}, "ok")
}

// @Summary      获取开户注册监管告知与协议版本
// @Tags      module-market
// @Router    /api/tenant/regulatory-notice [get]
// RegulatoryNotice GET /api/tenant/regulatory-notice
//
// 开户注册的监管告知与协议版本。未登录可取：注册页在用户还没有任何身份时
// 就要展示这段文本并要求勾选，文案与版本号由平台端统一下发，避免两端漂移。
func (c *ModuleMarketController) RegulatoryNotice(ctx *gin.Context) {
	svc := service.NewModuleCatalogService()
	n := svc.RegulatoryNotice(ctx.Request.Context())
	response.Success(ctx, gin.H{
		"notice":            n.Notice,
		"agreement_version": n.AgreementVersion,
		"required_fields":   n.RequiredFields,
	}, "ok")
}

// @Summary      平台未连接，无法查询 LLM 余额
// @Tags      module-market
// @Router    /api/llm/wallet [get]
// LLMWallet GET /api/llm/wallet
func (c *ModuleMarketController) LLMWallet(ctx *gin.Context) {
	w, err := service.NewLLMWalletService().Wallet(ctx.Request.Context())
	if err != nil {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeModulePlatformUnavail,
			"平台未连接，无法查询 LLM 余额", gin.H{})
		return
	}
	response.Success(ctx, w, "ok")
}

// @Summary      平台未连接，无法查询 LLM 流水
// @Tags      module-market
// @Router    /api/llm/wallet/txns [get]
// LLMWalletTxns GET /api/llm/wallet/txns
func (c *ModuleMarketController) LLMWalletTxns(ctx *gin.Context) {
	list, total, err := service.NewLLMWalletService().Txns(ctx.Request.Context(),
		pageOf(ctx), pageSizeOf(ctx))
	if err != nil {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeModulePlatformUnavail,
			"平台未连接，无法查询 LLM 流水", gin.H{})
		return
	}
	response.Success(ctx, gin.H{"list": list, "total": total}, "ok")
}

// @Summary      平台未连接，无法查看 LLM 套餐
// @Tags      module-market
// @Router    /api/llm/plans [get]
// LLMPlans GET /api/llm/plans
func (c *ModuleMarketController) LLMPlans(ctx *gin.Context) {
	list, err := service.NewLLMWalletService().Plans(ctx.Request.Context())
	if err != nil {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeModulePlatformUnavail,
			"平台未连接，无法查看 LLM 套餐", gin.H{})
		return
	}
	response.Success(ctx, gin.H{"list": list, "total": len(list)}, "ok")
}

// @Summary      订单已提交，请完成支付；支付成功后余额自动入账
// @Tags      module-market
// @Router    /api/llm/recharge [post]
// CreateLLMOrder POST /api/llm/recharge
//
// 购买平台 LLM 套餐（预付费 token 包）。与功能模块订单走**同一个** MarkPaid
// 入账路径（平台端 order_type=llm），只是订单行类型不同。
func (c *ModuleMarketController) CreateLLMOrder(ctx *gin.Context) {
	var in service.LLMOrderInput
	if err := ctx.ShouldBindJSON(&in); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：请检查字段类型与必填项")
		return
	}
	res, err := service.NewLLMWalletService().CreateOrder(withBuyerIP(ctx), in)
	if err != nil {
		moduleMarketFail(ctx, err)
		return
	}
	response.Success(ctx, res, "订单已提交，请完成支付；支付成功后余额自动入账")
}

var _ = platform.ModuleSnapshot{}
