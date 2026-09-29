package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/ops/service"
	"hivemtk-user/internal/pkg/utils/response"
)

// LtcRatesController LTC 三率控制器（T-P8-05）。
//
// 范式与 conversion_funnel.go 同一格：参数解析 + 调 service +
// response.Success / response.Error 包装，本层不碰口径。
type LtcRatesController struct {
	svc *service.LtcRatesService
}

// NewLtcRatesController 创建 LTC 三率控制器（生产装配：默认构造走全局连接）。
func NewLtcRatesController() *LtcRatesController {
	return &LtcRatesController{svc: service.NewLtcRatesService()}
}

// GetLtcRates GET /api/ltc-rates（+ /api/manage 别名）：LTC 三率。
func (c *LtcRatesController) GetLtcRates(ctx *gin.Context) {
	rates, err := c.svc.GetLtcRates(ctx.Request.Context())
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "LTC 三率查询失败: "+err.Error())
		return
	}
	response.Success(ctx, rates, "LTC 三率查询成功")
}
