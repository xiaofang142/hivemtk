package controller

import (
	"net/http"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// ClientDeviceController 商户电脑设备信标控制器（任务三）
//
// 登录态轻量口：SaaS 形态下 user-web 每次页面加载发一帧，user-server
// 从请求侧采集出口 IP + User-Agent 合并入快照，供心跳上报 client_machine。
// 离线单机形态服务层静默拒收（返回 recorded=false），不报错。
type ClientDeviceController struct {
	svc *service.ClientDeviceService
}

// NewClientDeviceController 构造
func NewClientDeviceController() *ClientDeviceController {
	return &ClientDeviceController{svc: service.NewClientDeviceService()}
}

// Report POST /api/client-device/report
// @Summary 商户电脑设备信标
// @Description SaaS 形态下 user-web 登录态上报浏览器设备快照（屏幕/时区/语言），
// @Description 服务端采集请求出口 IP 与 User-Agent；心跳协程取最近一帧随帧上报平台。
// @Tags 设备信标
// @Accept json
// @Produce json
// @Param request body object true "设备信息"
// @Success 200 {object} object{data=object{recorded=bool}}
// @Router /api/client-device/report [post]
func (c *ClientDeviceController) Report(ctx *gin.Context) {
	var req struct {
		Platform   string `json:"platform"`
		Screen     string `json:"screen"`
		Timezone   string `json:"timezone"`
		Language   string `json:"language"`
		TenantCode string `json:"tenant_code"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}
	rep := &service.ClientDeviceReport{
		Platform:   req.Platform,
		Screen:     req.Screen,
		Timezone:   req.Timezone,
		Language:   req.Language,
		TenantCode: req.TenantCode,
		UA:         ctx.Request.UserAgent(),
		IP:         ctx.ClientIP(),
	}
	recorded, err := c.svc.Report(ctx, rep)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, gin.H{"recorded": recorded}, "ok")
}
