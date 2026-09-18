package controller

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// FeishuAccountController 飞书机器人账号管理控制器
//
// 功能职责：
//   - 飞书机器人 App ID / App Secret / Verification Token / Encrypt Key CRUD
//   - Webhook 启用/智能体开关
//   - 私域独立部署模式下，所有数据归属当前部署实例
//   - 凭据（App Secret/Encrypt Key）不回显，返回掩码
//
// 修复：移除 repo 字段，所有数据操作通过 svc (FeishuService) 完成，
// 严格遵循五层架构 Controller → Service → Repository → Model
type FeishuAccountController struct {
	svc            *service.FeishuService
	integrationSvc *service.FeishuIntegrationService
}

// NewFeishuAccountController 创建控制器
func NewFeishuAccountController(svc *service.FeishuService, integrationSvc *service.FeishuIntegrationService) *FeishuAccountController {
	return &FeishuAccountController{
		svc:            svc,
		integrationSvc: integrationSvc,
	}
}

// RegisterRoutes 注册路由
func (ctrl *FeishuAccountController) RegisterRoutes(router *gin.RouterGroup) {
	g := router.Group("/feishu/accounts")
	{
		g.GET("", ctrl.List)
		g.GET("/:id", ctrl.Get)
		g.POST("", ctrl.Create)
		g.PUT("/:id", ctrl.Update)
		g.DELETE("/:id", ctrl.Delete)
		g.POST("/:id/test-send", ctrl.TestSend)
		g.GET("/:id/test-send", ctrl.TestSendQuery)
		g.POST("/:id/refresh-token", ctrl.RefreshToken)
		g.GET("/:id/refresh-token", ctrl.RefreshToken)
	}
}

// TestSendQuery 兼容 GET 请求的测试发送：从 query 读取 open_id 与 content，
// 用于前端探测或浏览器直接访问场景。
// GET /api/feishu/accounts/:id/test-send?open_id=xxx&content=hello
func (ctrl *FeishuAccountController) TestSendQuery(c *gin.Context) {
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
	openID := c.Query("open_id")
	content := c.Query("content")
	if openID == "" || content == "" {
		response.Success(c, gin.H{
			"account_id":        acc.ID,
			"account_name":      acc.AccountName,
			"app_secret_masked": maskFeishuSecret(acc.AppSecret),
			"test_send_ready":   true,
		}, "测试发送参数缺失，账号校验通过")
		return
	}
	integration := ctrl.integrationSvc
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	if err := integration.SendMessage(ctx, uint(id), openID, content, "open_id", ""); err != nil {
		response.ErrorFromDB(c, err, "发送失败", err.Error())
		return
	}
	response.Success(c, nil, "发送成功")
}

type feishuAccountVO struct {
	ID                uint       `json:"id"`
	AccountName       string     `json:"account_name"`
	AppID             string     `json:"app_id"`
	AppSecretMasked   string     `json:"app_secret_masked"`
	VerificationToken string     `json:"verification_token"`
	EncryptKeyMasked  string     `json:"encrypt_key_masked"`
	WebhookEnabled    bool       `json:"webhook_enabled"`
	AIAgentEnabled    bool       `json:"ai_agent_enabled"`
	LastSyncAt        *time.Time `json:"last_sync_at"`
	LastErrorAt       *time.Time `json:"last_error_at"`
	LastErrorMsg      string     `json:"last_error_msg"`
	Status            int        `json:"status"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

func toFeishuVO(a *model.FeishuAccount) *feishuAccountVO {
	return &feishuAccountVO{
		ID:                a.ID,
		AccountName:       a.AccountName,
		AppID:             a.AppID,
		AppSecretMasked:   maskFeishuSecret(a.AppSecret),
		VerificationToken: a.VerificationToken,
		EncryptKeyMasked:  maskFeishuSecret(a.EncryptKey),
		WebhookEnabled:    a.WebhookEnabled,
		AIAgentEnabled:    a.AIAgentEnabled,
		LastSyncAt:        a.LastSyncAt,
		LastErrorAt:       a.LastErrorAt,
		LastErrorMsg:      a.LastErrorMsg,
		Status:            a.Status,
		CreatedAt:         a.CreatedAt,
		UpdatedAt:         a.UpdatedAt,
	}
}

// List 列出所有飞书账号
func (ctrl *FeishuAccountController) List(c *gin.Context) {
	accs, err := ctrl.svc.ListAccounts(c.Request.Context())
	if err != nil {
		response.ErrorFromDB(c, err, "查询失败", err.Error())
		return
	}
	out := make([]*feishuAccountVO, 0, len(accs))
	for _, a := range accs {
		out = append(out, toFeishuVO(a))
	}
	response.Success(c, out, "查询成功")
}

// Get 获取单个飞书账号
func (ctrl *FeishuAccountController) Get(c *gin.Context) {
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
	response.Success(c, toFeishuVO(acc), "查询成功")
}

type feishuCreateReq struct {
	AccountName       string `json:"account_name" binding:"required"`
	AppID             string `json:"app_id" binding:"required"`
	AppSecret         string `json:"app_secret" binding:"required"`
	VerificationToken string `json:"verification_token"`
	EncryptKey        string `json:"encrypt_key"`
	WebhookEnabled    bool   `json:"webhook_enabled"`
	AIAgentEnabled    bool   `json:"ai_agent_enabled"`
}

// Create 创建飞书账号
func (ctrl *FeishuAccountController) Create(c *gin.Context) {
	var req feishuCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误", err.Error())
		return
	}
	acc := &model.FeishuAccount{
		AccountName:       req.AccountName,
		AppID:             req.AppID,
		AppSecret:         req.AppSecret,
		VerificationToken: req.VerificationToken,
		EncryptKey:        req.EncryptKey,
		WebhookEnabled:    req.WebhookEnabled,
		AIAgentEnabled:    req.AIAgentEnabled,
		Status:            1,
		OwnerUserID:       currentStaffUserID(c),
	}
	out, err := ctrl.svc.CreateAccount(c.Request.Context(), acc)
	if err != nil {
		response.ErrorFromDB(c, err, "创建失败", err.Error())
		return
	}
	response.Success(c, toFeishuVO(out), "创建成功")
}

type feishuUpdateReq struct {
	AccountName       *string `json:"account_name"`
	AppSecret         *string `json:"app_secret"`
	VerificationToken *string `json:"verification_token"`
	EncryptKey        *string `json:"encrypt_key"`
	WebhookEnabled    *bool   `json:"webhook_enabled"`
	AIAgentEnabled    *bool   `json:"ai_agent_enabled"`
	Status            *int    `json:"status"`
}

// Update 更新飞书账号
func (ctrl *FeishuAccountController) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "ID 错误", err.Error())
		return
	}
	var req feishuUpdateReq
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
	if req.AppSecret != nil && *req.AppSecret != "" {
		acc.AppSecret = *req.AppSecret
		acc.AccessToken = ""
		acc.TokenExpires = nil
	}
	if req.VerificationToken != nil {
		acc.VerificationToken = *req.VerificationToken
	}
	if req.EncryptKey != nil && *req.EncryptKey != "" {
		acc.EncryptKey = *req.EncryptKey
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
	response.Success(c, toFeishuVO(acc), "更新成功")
}

// Delete 删除飞书账号
func (ctrl *FeishuAccountController) Delete(c *gin.Context) {
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

type feishuTestSendReq struct {
	OpenID  string `json:"open_id" binding:"required"`
	Content string `json:"content" binding:"required"`
}

// TestSend 测试发送文本消息
func (ctrl *FeishuAccountController) TestSend(c *gin.Context) {
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
	var req feishuTestSendReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误", err.Error())
		return
	}
	integration := ctrl.integrationSvc
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	if err := integration.SendMessage(ctx, acc.ID, req.OpenID, req.Content, "open_id", ""); err != nil {
		response.ErrorFromDB(c, err, "发送失败", err.Error())
		return
	}
	response.Success(c, nil, "发送成功")
}

// RefreshToken 刷新 access_token
func (ctrl *FeishuAccountController) RefreshToken(c *gin.Context) {
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
	integration := ctrl.integrationSvc
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	if err := integration.RefreshAccessToken(ctx, acc); err != nil {
		response.ErrorFromDB(c, err, "刷新失败", err.Error())
		return
	}
	response.Success(c, toFeishuVO(acc), "刷新成功")
}

func maskFeishuSecret(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "****"
	}
	return fmt.Sprintf("%s****%s", s[:4], s[len(s)-4:])
}
