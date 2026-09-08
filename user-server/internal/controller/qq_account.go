package controller

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// QQAccountController QQ 机器人账号管理控制器
//
// 功能职责：
//   - QQ Bot 账号 CRUD（AppID / AppSecret / WebhookSecret）
//   - Webhook 地址推导（回调地址在 q.qq.com 管理端配置：/api/webhook/qq/{account_id}）
//   - 智能体开关：开启后 QQ 群 @ 与单聊入站消息自动触发智能体
//
// 设计说明：
//   - 遵循五层架构，经 service.QQService 访问数据
//   - AppSecret / WebhookSecret 是敏感信息，更新时不回显（响应中返回掩码）
type QQAccountController struct {
	svc *service.QQService
}

// NewQQAccountController 创建控制器
func NewQQAccountController(svc *service.QQService) *QQAccountController {
	if svc == nil {
		svc = service.NewQQService(nil)
	}
	return &QQAccountController{svc: svc}
}

// RegisterRoutes 注册路由
func (ctrl *QQAccountController) RegisterRoutes(router *gin.RouterGroup) {
	g := router.Group("/qq/accounts")
	{
		g.GET("", ctrl.List)
		g.GET("/:id", ctrl.Get)
		g.POST("", ctrl.Create)
		g.PUT("/:id", ctrl.Update)
		g.DELETE("/:id", ctrl.Delete)
		g.POST("/:id/test-send", ctrl.TestSend)
	}
}

type qqAccountVO struct {
	ID              uint       `json:"id"`
	AccountName     string     `json:"account_name"`
	AppID           string     `json:"app_id"`
	AppSecretMasked string     `json:"app_secret_masked"`
	WebhookURL      string     `json:"webhook_url"`
	WebhookEnabled  bool       `json:"webhook_enabled"`
	AIAgentEnabled  bool       `json:"ai_agent_enabled"`
	LastSyncAt      *time.Time `json:"last_sync_at"`
	LastErrorAt     *time.Time `json:"last_error_at"`
	LastErrorMsg    string     `json:"last_error_msg"`
	Status          int        `json:"status"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func toQQAccountVO(acc *model.QQAccount) qqAccountVO {
	return qqAccountVO{
		ID:              acc.ID,
		AccountName:     acc.AccountName,
		AppID:           acc.AppID,
		AppSecretMasked: maskBotToken(acc.AppSecret),
		WebhookURL:      acc.WebhookURL,
		WebhookEnabled:  acc.WebhookEnabled,
		AIAgentEnabled:  acc.AIAgentEnabled,
		LastSyncAt:      acc.LastSyncAt,
		LastErrorAt:     acc.LastErrorAt,
		LastErrorMsg:    acc.LastErrorMsg,
		Status:          acc.Status,
		CreatedAt:       acc.CreatedAt,
		UpdatedAt:       acc.UpdatedAt,
	}
}

// List 列表
func (ctrl *QQAccountController) List(c *gin.Context) {
	accs, err := ctrl.svc.ListAccounts(context.Background())
	if err != nil {
		response.ErrorFromDB(c, err, "获取列表失败", err.Error())
		return
	}
	list := make([]qqAccountVO, 0, len(accs))
	for _, acc := range accs {
		list = append(list, toQQAccountVO(acc))
	}
	response.SuccessWithList(c, list, int64(len(list)))
}

// Get 详情
func (ctrl *QQAccountController) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "无效的账号ID", err.Error())
		return
	}
	acc, err := ctrl.svc.GetAccount(context.Background(), uint(id))
	if err != nil {
		response.Error(c, http.StatusNotFound, "账号不存在", err.Error())
		return
	}
	if !guardChannelAccountOwnership(c, acc.OwnerUserID) {
		return
	}
	response.Success(c, toQQAccountVO(acc), "获取成功")
}

type qqAccountCreateReq struct {
	AccountName    string `json:"account_name" binding:"required"`
	AppID          string `json:"app_id" binding:"required"`
	AppSecret      string `json:"app_secret" binding:"required"`
	WebhookSecret  string `json:"webhook_secret"`
	WebhookURL     string `json:"webhook_url"`
	WebhookEnabled bool   `json:"webhook_enabled"`
	AIAgentEnabled bool   `json:"ai_agent_enabled"`
	Status         int    `json:"status"`
}

// Create 创建
func (ctrl *QQAccountController) Create(c *gin.Context) {
	var req qqAccountCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误", err.Error())
		return
	}
	if req.Status == 0 {
		req.Status = 1
	}
	acc := &model.QQAccount{
		AccountName:    req.AccountName,
		AppID:          req.AppID,
		AppSecret:      req.AppSecret,
		WebhookSecret:  req.WebhookSecret,
		WebhookURL:     req.WebhookURL,
		WebhookEnabled: req.WebhookEnabled,
		AIAgentEnabled: req.AIAgentEnabled,
		Status:         req.Status,
		OwnerUserID:    currentStaffUserID(c),
	}
	if _, err := ctrl.svc.CreateAccount(context.Background(), acc); err != nil {
		response.ErrorFromDB(c, err, "创建失败", err.Error())
		return
	}
	response.Success(c, toQQAccountVO(acc), "创建成功")
}

// Update 更新（AppSecret / WebhookSecret 为空时保留原值）
func (ctrl *QQAccountController) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "无效的账号ID", err.Error())
		return
	}
	acc, err := ctrl.svc.GetAccount(context.Background(), uint(id))
	if err != nil {
		response.Error(c, http.StatusNotFound, "账号不存在", err.Error())
		return
	}
	if !guardChannelAccountOwnership(c, acc.OwnerUserID) {
		return
	}
	var req qqAccountCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误", err.Error())
		return
	}
	acc.AccountName = req.AccountName
	if req.AppID != "" {
		acc.AppID = req.AppID
	}
	// secret 从不回显（VO 掩码），前端编辑回填空串：空值=保留原值
	if req.AppSecret != "" {
		acc.AppSecret = req.AppSecret
	}
	if req.WebhookSecret != "" {
		acc.WebhookSecret = req.WebhookSecret
	}
	if req.WebhookURL != "" {
		acc.WebhookURL = req.WebhookURL
	}
	acc.WebhookEnabled = req.WebhookEnabled
	acc.AIAgentEnabled = req.AIAgentEnabled
	if req.Status != 0 {
		acc.Status = req.Status
	}
	if err := ctrl.svc.UpdateAccount(context.Background(), acc); err != nil {
		response.ErrorFromDB(c, err, "更新失败", err.Error())
		return
	}
	response.Success(c, toQQAccountVO(acc), "更新成功")
}

// Delete 删除
func (ctrl *QQAccountController) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "无效的账号ID", err.Error())
		return
	}
	acc, err := ctrl.svc.GetAccount(context.Background(), uint(id))
	if err != nil {
		response.Error(c, http.StatusNotFound, "账号不存在", err.Error())
		return
	}
	if !guardChannelAccountOwnership(c, acc.OwnerUserID) {
		return
	}
	if err := ctrl.svc.DeleteAccount(context.Background(), uint(id)); err != nil {
		response.ErrorFromDB(c, err, "删除失败", err.Error())
		return
	}
	response.Success(c, nil, "删除成功")
}

// TestSend 测试向指定 openid 发送一条消息，验证 AppID/AppSecret 可用性
// POST /api/qq/accounts/:id/test-send
// body: {"target_type": "group"|"c2c", "target_openid": "G0xxx", "text": "测试消息"}
func (ctrl *QQAccountController) TestSend(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "无效的账号ID", err.Error())
		return
	}
	acc, err := ctrl.svc.GetAccount(context.Background(), uint(id))
	if err != nil {
		response.Error(c, http.StatusNotFound, "账号不存在", err.Error())
		return
	}
	if !guardChannelAccountOwnership(c, acc.OwnerUserID) {
		return
	}
	var req struct {
		TargetType   string `json:"target_type" binding:"required"`
		TargetOpenID string `json:"target_openid" binding:"required"`
		Text         string `json:"text" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误", err.Error())
		return
	}
	integration := service.NewQQIntegrationService(nil)
	if err := integration.SendMessage(context.Background(), acc.ID, req.TargetOpenID, "", req.Text); err != nil {
		response.ErrorFromDB(c, err, "发送失败（注意：QQ 主动消息需用户允许且受频控，建议在群里 @机器人 后用被动回复）", err.Error())
		return
	}
	response.Success(c, gin.H{"ok": true}, "发送成功")
}

// deriveQQWebhookURL 推导 QQ webhook 回调地址（供前端展示）
func deriveQQWebhookURL(accountID uint) string {
	if base := config.GetPublicBaseURL(); base != "" {
		return strings.TrimRight(base, "/") + "/api/webhook/qq/" + strconv.FormatUint(uint64(accountID), 10)
	}
	return ""
}
