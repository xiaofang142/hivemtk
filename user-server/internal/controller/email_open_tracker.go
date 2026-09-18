package controller

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strconv"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// EmailOpenTrackerController 邮件打开率追踪控制器
type EmailOpenTrackerController struct {
	svc *service.EmailOpenTrackerService
}

// NewEmailOpenTrackerController 创建控制器
func NewEmailOpenTrackerController(svc *service.EmailOpenTrackerService) *EmailOpenTrackerController {
	return &EmailOpenTrackerController{svc: svc}
}

func verifyWebhookSharedSecret(ctx *gin.Context) bool {
	want := os.Getenv("EMAIL_WEBHOOK_SECRET")
	if want == "" {
		return true
	}
	got := ctx.GetHeader("X-Webhook-Secret")
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func (c *EmailOpenTrackerController) PostmarkWebhook(ctx *gin.Context) {
	if !verifyWebhookSharedSecret(ctx) {
		ctx.String(http.StatusUnauthorized, "invalid webhook secret")
		return
	}
	if c.svc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "邮件打开追踪服务未初始化")
		return
	}
	var evt service.PostmarkOpenEvent
	if err := ctx.ShouldBindJSON(&evt); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误："+err.Error())
		return
	}
	if err := c.svc.RecordPostmarkEvent(ctx.Request.Context(), &evt); err != nil {
		response.ErrorFromDB(ctx, err, "记录事件失败："+err.Error())
		return
	}
	response.Success(ctx, gin.H{"recorded": true, "type": evt.RecordType}, "ok")
}

// SendCloudWebhook POST /api/email/track/webhook/sendcloud
func (c *EmailOpenTrackerController) SendCloudWebhook(ctx *gin.Context) {
	if !verifyWebhookSharedSecret(ctx) {
		ctx.String(http.StatusUnauthorized, "invalid webhook secret")
		return
	}
	if c.svc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "邮件打开追踪服务未初始化")
		return
	}
	var evt service.SendCloudOpenEvent
	if err := ctx.ShouldBindJSON(&evt); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误："+err.Error())
		return
	}
	if err := c.svc.RecordSendCloudEvent(ctx.Request.Context(), &evt); err != nil {
		response.ErrorFromDB(ctx, err, "记录事件失败："+err.Error())
		return
	}
	response.Success(ctx, gin.H{"recorded": true, "event": evt.Event}, "ok")
}

// GetOpenMetrics GET /api/email/track/open-metrics?job_id=xxx&total_sent=100
func (c *EmailOpenTrackerController) GetOpenMetrics(ctx *gin.Context) {
	if c.svc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "邮件打开追踪服务未初始化")
		return
	}
	jobID := ctx.Query("job_id")
	if jobID == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少 job_id 参数")
		return
	}
	totalSent, _ := strconv.ParseInt(ctx.DefaultQuery("total_sent", "0"), 10, 64)
	m, err := c.svc.GetOpenRateMetrics(ctx.Request.Context(), jobID, totalSent)
	if err != nil {
		response.ErrorFromDB(ctx, err, "查询打开率失败："+err.Error())
		return
	}
	response.Success(ctx, m, "ok")
}

// RegisterRoutes 注册路由
//
// 公开路由：webhook（SMTP 推送，无 JWT）；打开像素由 EmailTrackingController
// 在 /api/email/track/open/:token 统一提供，本控制器不再重复注册 pixel 路由。
// 鉴权路由：打开率指标（后台管理）
func (c *EmailOpenTrackerController) RegisterRoutes(public *gin.RouterGroup, authed *gin.RouterGroup) {
	if public != nil {
		public.POST("/email/track/webhook/postmark", c.PostmarkWebhook)
		public.POST("/email/track/webhook/sendcloud", c.SendCloudWebhook)
	}
	if authed != nil {
		authed.GET("/email/track/open-metrics", c.GetOpenMetrics)
	}
}
