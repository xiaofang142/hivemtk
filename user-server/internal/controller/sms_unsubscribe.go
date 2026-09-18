package controller

import (
	"crypto/subtle"
	"encoding/csv"
	"net/http"
	"os"
	"strconv"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// SmsUnsubscribeController 短信退订控制器
//
// 路由：
//   - POST /api/sms/webhook/inbound          上行短信 webhook（运营商推送用户回复）
//   - GET  /api/sms/unsubscribe/list         分页查询退订名单
//   - POST /api/sms/unsubscribe/manual-add   手动添加退订号码
//   - DELETE /api/sms/unsubscribe/:phone     删除退订号码（重新订阅）
//   - GET  /api/sms/unsubscribe/export       导出退订名单（CSV）
type SmsUnsubscribeController struct {
	svc *service.SmsUnsubscribeService
}

// NewSmsUnsubscribeController 创建短信退订控制器
func NewSmsUnsubscribeController(svc *service.SmsUnsubscribeService) *SmsUnsubscribeController {
	return &SmsUnsubscribeController{svc: svc}
}

// InboundSmsWebhookRequest 上行短信 webhook 请求体
//
// 兼容主流运营商 webhook 格式（阿里云/腾讯云/华为云）
type InboundSmsWebhookRequest struct {
	Phone      string `json:"phone" binding:"required"`
	Content    string `json:"content" binding:"required"`
	MessageID  string `json:"messageId"`
	SignName   string `json:"signName"`
	SendTime   string `json:"sendTime"`
	DestCode   string `json:"destCode"`
	SequenceId string `json:"sequenceId"`
}

// InboundSmsWebhook POST /api/sms/webhook/inbound
//
// 处理运营商推送的上行短信（用户回复）
// 自动识别退订关键词并加入退订名单
func (c *SmsUnsubscribeController) InboundSmsWebhook(ctx *gin.Context) {

	if want := os.Getenv("EMAIL_WEBHOOK_SECRET"); want != "" {
		if subtle.ConstantTimeCompare([]byte(ctx.GetHeader("X-Webhook-Secret")), []byte(want)) != 1 {
			ctx.String(http.StatusUnauthorized, "invalid webhook secret")
			return
		}
	}
	var req InboundSmsWebhookRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误："+err.Error())
		return
	}

	matched, err := c.svc.ProcessUnsubscribeReply(ctx.Request.Context(), req.Phone, req.Content, req.MessageID)
	if err != nil {
		response.ErrorFromDB(ctx, err, "处理上行短信失败："+err.Error())
		return
	}

	if matched == "" {
		response.Success(ctx, gin.H{
			"phone":        req.Phone,
			"content":      req.Content,
			"unsubscribed": false,
			"keyword":      "",
		}, "上行短信已接收，未命中退订关键词")
		return
	}

	response.Success(ctx, gin.H{
		"phone":        req.Phone,
		"content":      req.Content,
		"unsubscribed": true,
		"keyword":      matched,
	}, "已识别退订关键词并加入退订名单")
}

// SmsManualAddRequest 手动添加退订号码请求体
type SmsManualAddRequest struct {
	Phone  string `json:"phone" binding:"required"`
	Reason string `json:"reason"`
}

// ManualAdd POST /api/sms/unsubscribe/manual-add
//
// 后台手动添加退订号码（合规要求：用户电话/邮件申请退订时使用）
func (c *SmsUnsubscribeController) ManualAdd(ctx *gin.Context) {
	var req SmsManualAddRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误："+err.Error())
		return
	}

	if err := c.svc.UnsubscribePhone(ctx.Request.Context(), req.Phone, req.Reason, "", "manual"); err != nil {
		response.ErrorFromDB(ctx, err, "添加退订号码失败："+err.Error())
		return
	}

	response.Success(ctx, gin.H{"phone": req.Phone, "status": "unsubscribed"}, "添加成功")
}

// DeleteByPhone DELETE /api/sms/unsubscribe/:phone
//
// 删除退订号码（用户重新订阅时使用）
func (c *SmsUnsubscribeController) DeleteByPhone(ctx *gin.Context) {
	phone := ctx.Param("phone")
	if phone == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少 phone 参数")
		return
	}

	if err := c.svc.ResubscribePhone(ctx.Request.Context(), phone); err != nil {
		response.ErrorFromDB(ctx, err, "删除退订号码失败："+err.Error())
		return
	}

	response.Success(ctx, gin.H{"phone": phone, "status": "resubscribed"}, "删除成功")
}

// ListUnsubscribes GET /api/sms/unsubscribe/list
func (c *SmsUnsubscribeController) ListUnsubscribes(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))
	keyword := ctx.Query("keyword")

	records, total, err := c.svc.ListUnsubscribes(ctx.Request.Context(), page, limit, keyword)
	if err != nil {
		response.ErrorFromDB(ctx, err, "查询退订名单失败："+err.Error())
		return
	}

	response.SuccessWithPage(ctx, records, int64(page), int64(limit), total)
}

// ExportUnsubscribes GET /api/sms/unsubscribe/export
//
// 导出退订名单 CSV
func (c *SmsUnsubscribeController) ExportUnsubscribes(ctx *gin.Context) {
	records, err := c.svc.ListAllUnsubscribes(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "导出退订名单失败："+err.Error())
		return
	}

	ctx.Header("Content-Type", "text/csv; charset=utf-8")
	ctx.Header("Content-Disposition", "attachment; filename=sms_unsubscribes.csv")

	_, _ = ctx.Writer.Write([]byte{0xEF, 0xBB, 0xBF})

	w := csv.NewWriter(ctx.Writer)
	_ = w.Write([]string{"phone", "reason", "unsubscribed_at", "source_message_id", "keyword_matched"})
	for _, r := range records {
		_ = w.Write([]string{r.Phone, r.Reason, r.UnsubscribedAt.Format("2006-01-02 15:04:05"), r.SourceMessageID, r.KeywordMatched})
	}
	w.Flush()
}

// RegisterRoutes 注册路由
//
// 公开路由：上行短信 webhook（运营商推送，无 JWT，建议运维层加 IP 白名单 / 签名校验）
// 鉴权路由：名单管理 / 导出（后台管理）
func (c *SmsUnsubscribeController) RegisterRoutes(public *gin.RouterGroup, authed *gin.RouterGroup) {
	if public != nil {
		public.POST("/sms/webhook/inbound", c.InboundSmsWebhook)
	}
	if authed != nil {
		authed.GET("/sms/unsubscribe/list", c.ListUnsubscribes)
		authed.POST("/sms/unsubscribe/manual-add", c.ManualAdd)
		authed.DELETE("/sms/unsubscribe/:phone", c.DeleteByPhone)
		authed.GET("/sms/unsubscribe/export", c.ExportUnsubscribes)
	}
}
