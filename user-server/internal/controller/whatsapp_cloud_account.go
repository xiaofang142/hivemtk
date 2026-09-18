package controller

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// WhatsAppCloudAccountController WhatsApp Cloud API 账号管理控制器
//
// 功能职责：
//   - 商业账号（Phone Number ID / WABA ID / Access Token / App Secret）CRUD
//   - Webhook 验证 Token 配置
//   - Webhook 启用/智能体开关
//   - 私域独立部署模式下，所有数据归属当前部署实例
//   - 凭据（Access Token / App Secret）不回显，返回掩码
//
// 修复：移除 repo 字段，所有数据操作通过 svc (WhatsAppCloudService) 完成，
// 严格遵循五层架构 Controller → Service → Repository → Model
type WhatsAppCloudAccountController struct {
	svc            *service.WhatsAppCloudService
	integrationSvc *service.WhatsAppCloudIntegrationService
}

// NewWhatsAppCloudAccountController 创建控制器
func NewWhatsAppCloudAccountController(svc *service.WhatsAppCloudService, integrationSvc *service.WhatsAppCloudIntegrationService) *WhatsAppCloudAccountController {
	return &WhatsAppCloudAccountController{
		svc:            svc,
		integrationSvc: integrationSvc,
	}
}

// RegisterRoutes 注册路由
func (ctrl *WhatsAppCloudAccountController) RegisterRoutes(router *gin.RouterGroup) {
	g := router.Group("/whatsapp-cloud/accounts")
	{
		g.GET("", ctrl.List)
		g.GET("/:id", ctrl.Get)
		g.POST("", ctrl.Create)
		g.PUT("/:id", ctrl.Update)
		g.DELETE("/:id", ctrl.Delete)
		g.POST("/:id/test-send", ctrl.TestSend)
	}
}

type whatsAppCloudAccountVO struct {
	ID                 uint       `json:"id"`
	AccountName        string     `json:"account_name"`
	PhoneNumberID      string     `json:"phone_number_id"`
	WhatsAppBusinessID string     `json:"whatsapp_business_id"`
	AccessTokenMasked  string     `json:"access_token_masked"`
	VerifyToken        string     `json:"verify_token"`
	AppSecretMasked    string     `json:"app_secret_masked"`
	WebhookEnabled     bool       `json:"webhook_enabled"`
	AIAgentEnabled     bool       `json:"ai_agent_enabled"`
	LastSyncAt         *time.Time `json:"last_sync_at"`
	LastErrorAt        *time.Time `json:"last_error_at"`
	LastErrorMsg       string     `json:"last_error_msg"`
	Status             int        `json:"status"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

func toWhatsAppCloudVO(a *model.WhatsAppCloudAccount) *whatsAppCloudAccountVO {
	return &whatsAppCloudAccountVO{
		ID:                 a.ID,
		AccountName:        a.AccountName,
		PhoneNumberID:      a.PhoneNumberID,
		WhatsAppBusinessID: a.WhatsAppBusinessID,
		AccessTokenMasked:  maskFeishuSecret(a.AccessToken),
		VerifyToken:        a.VerifyToken,
		AppSecretMasked:    maskFeishuSecret(a.AppSecret),
		WebhookEnabled:     a.WebhookEnabled,
		AIAgentEnabled:     a.AIAgentEnabled,
		LastSyncAt:         a.LastSyncAt,
		LastErrorAt:        a.LastErrorAt,
		LastErrorMsg:       a.LastErrorMsg,
		Status:             a.Status,
		CreatedAt:          a.CreatedAt,
		UpdatedAt:          a.UpdatedAt,
	}
}

// List 列出所有 WhatsApp Cloud 账号
func (ctrl *WhatsAppCloudAccountController) List(c *gin.Context) {
	accs, err := ctrl.svc.ListAccounts(c.Request.Context())
	if err != nil {
		response.ErrorFromDB(c, err, "查询失败", err.Error())
		return
	}
	out := make([]*whatsAppCloudAccountVO, 0, len(accs))
	for _, a := range accs {
		out = append(out, toWhatsAppCloudVO(a))
	}
	response.Success(c, out, "查询成功")
}

// Get 获取单个 WhatsApp Cloud 账号
func (ctrl *WhatsAppCloudAccountController) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "ID 错误", err.Error())
		return
	}
	acc, err := ctrl.svc.GetAccount(c.Request.Context(), uint(id))
	if err != nil {
		response.Error(c, http.StatusNotFound, "账号不存在", err.Error())
		return
	}
	if !guardChannelAccountOwnership(c, acc.OwnerUserID) {
		return
	}
	response.Success(c, toWhatsAppCloudVO(acc), "查询成功")
}

type whatsAppCloudCreateReq struct {
	AccountName        string `json:"account_name" binding:"required"`
	PhoneNumberID      string `json:"phone_number_id" binding:"required"`
	WhatsAppBusinessID string `json:"whatsapp_business_id" binding:"required"`
	AccessToken        string `json:"access_token" binding:"required"`
	VerifyToken        string `json:"verify_token"`
	AppSecret          string `json:"app_secret"`
	WebhookEnabled     bool   `json:"webhook_enabled"`
	AIAgentEnabled     bool   `json:"ai_agent_enabled"`
}

// Create 创建 WhatsApp Cloud 账号
func (ctrl *WhatsAppCloudAccountController) Create(c *gin.Context) {
	var req whatsAppCloudCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误", err.Error())
		return
	}
	acc := &model.WhatsAppCloudAccount{
		AccountName:        req.AccountName,
		PhoneNumberID:      req.PhoneNumberID,
		WhatsAppBusinessID: req.WhatsAppBusinessID,
		AccessToken:        req.AccessToken,
		VerifyToken:        req.VerifyToken,
		AppSecret:          req.AppSecret,
		WebhookEnabled:     req.WebhookEnabled,
		AIAgentEnabled:     req.AIAgentEnabled,
		Status:             1,
		OwnerUserID:        currentStaffUserID(c),
	}
	out, err := ctrl.svc.CreateAccount(c.Request.Context(), acc)
	if err != nil {
		response.ErrorFromDB(c, err, "创建失败", err.Error())
		return
	}
	response.Success(c, toWhatsAppCloudVO(out), "创建成功")
}

type whatsAppCloudUpdateReq struct {
	AccountName    *string `json:"account_name"`
	AccessToken    *string `json:"access_token"`
	VerifyToken    *string `json:"verify_token"`
	AppSecret      *string `json:"app_secret"`
	WebhookEnabled *bool   `json:"webhook_enabled"`
	AIAgentEnabled *bool   `json:"ai_agent_enabled"`
	Status         *int    `json:"status"`
}

// Update 更新 WhatsApp Cloud 账号
func (ctrl *WhatsAppCloudAccountController) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "ID 错误", err.Error())
		return
	}
	var req whatsAppCloudUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误", err.Error())
		return
	}
	acc, err := ctrl.svc.GetAccount(c.Request.Context(), uint(id))
	if err != nil {
		response.Error(c, http.StatusNotFound, "账号不存在", err.Error())
		return
	}
	if !guardChannelAccountOwnership(c, acc.OwnerUserID) {
		return
	}
	if req.AccountName != nil {
		acc.AccountName = *req.AccountName
	}
	if req.AccessToken != nil && *req.AccessToken != "" {
		acc.AccessToken = *req.AccessToken
	}
	if req.VerifyToken != nil {
		acc.VerifyToken = *req.VerifyToken
	}
	if req.AppSecret != nil && *req.AppSecret != "" {
		acc.AppSecret = *req.AppSecret
	}
	if req.WebhookEnabled != nil {
		acc.WebhookEnabled = *req.WebhookEnabled
	}
	if req.AIAgentEnabled != nil {
		acc.AIAgentEnabled = *req.AIAgentEnabled
	}
	if req.Status != nil {
		acc.Status = *req.Status
	}
	if err := ctrl.svc.UpdateAccount(c.Request.Context(), acc); err != nil {
		response.ErrorFromDB(c, err, "更新失败", err.Error())
		return
	}
	response.Success(c, toWhatsAppCloudVO(acc), "更新成功")
}

// Delete 删除 WhatsApp Cloud 账号
func (ctrl *WhatsAppCloudAccountController) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "ID 错误", err.Error())
		return
	}
	acc, err := ctrl.svc.GetAccount(c.Request.Context(), uint(id))
	if err != nil {
		response.Error(c, http.StatusNotFound, "账号不存在", err.Error())
		return
	}
	if !guardChannelAccountOwnership(c, acc.OwnerUserID) {
		return
	}
	if err := ctrl.svc.DeleteAccount(c.Request.Context(), uint(id)); err != nil {
		response.ErrorFromDB(c, err, "删除失败", err.Error())
		return
	}
	response.Success(c, nil, "删除成功")
}

type whatsAppCloudTestSendReq struct {
	ToPhone string `json:"to_phone" binding:"required"`
	Content string `json:"content" binding:"required"`
}

// TestSend 测试发送文本消息
func (ctrl *WhatsAppCloudAccountController) TestSend(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "ID 错误", err.Error())
		return
	}
	acc, accErr := ctrl.svc.GetAccount(c.Request.Context(), uint(id))
	if accErr != nil {
		response.Error(c, http.StatusNotFound, "账号不存在", accErr.Error())
		return
	}
	if !guardChannelAccountOwnership(c, acc.OwnerUserID) {
		return
	}
	var req whatsAppCloudTestSendReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误", err.Error())
		return
	}
	integration := ctrl.integrationSvc
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	if err := integration.SendMessage(ctx, uint(id), req.ToPhone, req.Content); err != nil {
		response.ErrorFromDB(c, err, "发送失败", err.Error())
		return
	}
	response.Success(c, nil, "发送成功")
}
