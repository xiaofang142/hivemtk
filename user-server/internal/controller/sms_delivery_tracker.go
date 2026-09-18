package controller

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// SmsDeliveryTrackerController 短信到达率追踪控制器
type SmsDeliveryTrackerController struct {
	svc *service.SmsDeliveryTrackerService
}

// NewSmsDeliveryTrackerController 创建控制器
func NewSmsDeliveryTrackerController(svc *service.SmsDeliveryTrackerService) *SmsDeliveryTrackerController {
	return &SmsDeliveryTrackerController{svc: svc}
}

// GetMetrics godoc
// @Summary      短信到达率聚合指标
// @Description  按时间窗口统计送达/失败/黑名单/携号转网触达失败 + 按运营商分布
// @Tags         SMS Delivery
// @Produce      json
// @Param start query string false "起始时间（RFC3339 或）"
// @Param        end    query  string  false  "结束时间（默认 now）"
// @Success      200    {object}  service.DeliveryRateMetrics
// @Router       /api/sms/delivery/metrics [get]
func (c *SmsDeliveryTrackerController) GetMetrics(ctx *gin.Context) {
	if c.svc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "短信到达率服务未初始化")
		return
	}
	end := time.Now()
	start := end.Add(-1 * time.Hour)
	if s := ctx.Query("start"); s != "" {
		if t, err := parseFlexibleTime(s); err == nil {
			start = t
		} else {
			response.Error(ctx, http.StatusBadRequest, "start 时间格式错误")
			return
		}
	}
	if s := ctx.Query("end"); s != "" {
		if t, err := parseFlexibleTime(s); err == nil {
			end = t
		} else {
			response.Error(ctx, http.StatusBadRequest, "end 时间格式错误")
			return
		}
	}
	m, err := c.svc.GetDeliveryRateMetrics(ctx.Request.Context(), start, end)
	if err != nil {
		response.ErrorFromDB(ctx, err, "查询到达率失败："+err.Error())
		return
	}
	response.Success(ctx, m, "ok")
}

// ListPortability godoc
// @Summary      携号转网记录列表
// @Description  按时间倒序返回携号转网检测记录
// @Tags         SMS Delivery
// @Produce      json
// @Param        page   query  int  false  "页码（默认 1）"
// @Param        limit  query  int  false  "每页条数（默认 20）"
// @Param        phone  query  string  false  "按手机号过滤"
// @Success      200    {array}  map[string]interface{}
// @Router       /api/sms/delivery/portability [get]
func (c *SmsDeliveryTrackerController) ListPortability(ctx *gin.Context) {
	if c.svc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "短信到达率服务未初始化")
		return
	}
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))
	phone := ctx.Query("phone")

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 200 {
		limit = 20
	}

	records, total, err := c.svc.ListPortabilityRecords(ctx.Request.Context(), phone, page, limit)
	if err != nil {
		response.ErrorFromDB(ctx, err, "查询携号转网记录失败："+err.Error())
		return
	}
	response.SuccessWithPage(ctx, records, int64(page), int64(limit), total)
}

// GetCarrier godoc
// @Summary      查询单号当前运营商
// @Description  优先走缓存，未命中走号段识别
// @Tags         SMS Delivery
// @Produce      json
// @Param        phone  path  string  true  "手机号"
// @Success      200    {object}  map[string]any
// @Router       /api/sms/delivery/portability/{phone} [get]
func (c *SmsDeliveryTrackerController) GetCarrier(ctx *gin.Context) {
	if c.svc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "短信到达率服务未初始化")
		return
	}
	phone := ctx.Param("phone")
	if phone == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少 phone 参数")
		return
	}
	carrier := c.svc.GetCurrentCarrier(context.Background(), phone)
	response.Success(ctx, gin.H{
		"phone":   phone,
		"carrier": carrier,
	}, "ok")
}

// DetectCarrierByPrefix godoc
// @Summary      通过号段识别运营商（不走缓存）
// @Tags         SMS Delivery
// @Produce      json
// @Param        phone  path  string  true  "手机号"
// @Success      200    {object}  map[string]any
// @Router       /api/sms/delivery/carrier/{phone} [get]
func (c *SmsDeliveryTrackerController) DetectCarrierByPrefix(ctx *gin.Context) {
	phone := ctx.Param("phone")
	carrier := service.DetectCarrierFromPhone(phone)
	response.Success(ctx, gin.H{
		"phone":   phone,
		"carrier": carrier,
		"source":  "prefix",
	}, "ok")
}

// WebhookRequest 统一的运营商回执 webhook 载荷
//
// 兼容阿里云/腾讯云/华为云短信回执
type WebhookRequest struct {
	MessageID   string `json:"messageId" binding:"required"`
	Phone       string `json:"phone" binding:"required"`
	JobID       string `json:"jobId"`
	Provider    string `json:"provider"`
	Carrier     string `json:"carrier"`
	Status      string `json:"status"`
	ErrorCode   string `json:"errorCode"`
	ErrorMsg    string `json:"errorMsg"`
	SentAt      string `json:"sentAt"`
	DeliveredAt string `json:"deliveredAt"`
}

// Webhook godoc
// @Summary      统一运营商回执 webhook
// @Description  接收阿里云/腾讯云/华为云短信回执；自动检测携号转网 + 记录黑名单
// @Tags         SMS Delivery
// @Accept       json
// @Produce      json
// @Param        body  body      WebhookRequest  true  "回执"
// @Success      200   {object}  map[string]any
// @Router       /api/sms/delivery/webhook [post]
func (c *SmsDeliveryTrackerController) Webhook(ctx *gin.Context) {
	if c.svc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "短信到达率服务未初始化")
		return
	}
	var req WebhookRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误："+err.Error())
		return
	}
	report := &service.ProviderDeliveryReport{
		MessageID:   req.MessageID,
		Phone:       req.Phone,
		JobID:       req.JobID,
		Provider:    req.Provider,
		Carrier:     req.Carrier,
		Status:      req.Status,
		ErrorCode:   req.ErrorCode,
		ErrorMsg:    req.ErrorMsg,
		SentAt:      req.SentAt,
		DeliveredAt: req.DeliveredAt,
		RawPayload:  service.MarshalReport(req),
	}
	if err := c.svc.RecordFromProvider(ctx.Request.Context(), report); err != nil {
		response.ErrorFromDB(ctx, err, "记录回执失败："+err.Error())
		return
	}
	response.Success(ctx, gin.H{
		"messageId": req.MessageID,
		"phone":     req.Phone,
		"recorded":  true,
	}, "ok")
}

// RegisterRoutes 注册路由
func (c *SmsDeliveryTrackerController) RegisterRoutes(public *gin.RouterGroup, authed *gin.RouterGroup) {
	if public != nil {
		public.POST("/sms/delivery/webhook", c.Webhook)
	}
	if authed != nil {
		group := authed.Group("/sms/delivery")
		{
			group.GET("/metrics", c.GetMetrics)
			group.GET("/portability", c.ListPortability)
			group.GET("/portability/:phone", c.GetCarrier)
			group.GET("/carrier/:phone", c.DetectCarrierByPrefix)
		}
	}
}
