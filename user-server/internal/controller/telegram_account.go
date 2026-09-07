package controller

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tgbot"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// TelegramAccountController Telegram 机器人账号管理控制器
//
// 功能职责：
//   - TG Bot 账号 CRUD（Bot Token / Webhook URL / Webhook Secret）
//   - Webhook 注册：调用 Telegram setWebhook 接口，把 Bot 推送给本系统的 /api/webhook/telegram/{account_id}
//   - 智能体开关：开启后，TG 入站消息和入群事件会自动触发 智能体流程（SalesEngine）
//
// 设计说明：
//   - 私域独立部署模式下，所有数据归属当前部署实例，不携带 merchant_id
//   - 通过 service.TelegramService 访问数据，遵循五层架构。
//   - Bot Token 是敏感信息，更新时不回显（响应中返回掩码）
type TelegramAccountController struct {
	svc *service.TelegramService
}

// NewTelegramAccountController 创建控制器
func NewTelegramAccountController(svc *service.TelegramService) *TelegramAccountController {
	if svc == nil {
		svc = service.NewTelegramService(nil)
	}
	return &TelegramAccountController{
		svc: svc,
	}
}

// RegisterRoutes 注册路由
func (ctrl *TelegramAccountController) RegisterRoutes(router *gin.RouterGroup) {
	g := router.Group("/telegram/accounts")
	{
		g.GET("", ctrl.List)
		g.GET("/:id", ctrl.Get)
		g.POST("", ctrl.Create)
		g.PUT("/:id", ctrl.Update)
		g.DELETE("/:id", ctrl.Delete)
		g.POST("/:id/register-webhook", ctrl.RegisterWebhook)
		g.GET("/:id/status", ctrl.Status)
		g.POST("/:id/test-send", ctrl.TestSend)
	}
}

type telegramAccountVO struct {
	ID             uint       `json:"id"`
	AccountName    string     `json:"account_name"`
	BotUsername    string     `json:"bot_username"`
	BotTokenMasked string     `json:"bot_token_masked"`
	WebhookURL     string     `json:"webhook_url"`
	WebhookEnabled bool       `json:"webhook_enabled"`
	AIAgentEnabled bool       `json:"ai_agent_enabled"`
	LastSyncAt     *time.Time `json:"last_sync_at"`
	LastErrorAt    *time.Time `json:"last_error_at"`
	LastErrorMsg   string     `json:"last_error_msg"`
	Status         int        `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func toTelegramAccountVO(acc *model.TelegramAccount) telegramAccountVO {
	return telegramAccountVO{
		ID:             acc.ID,
		AccountName:    acc.AccountName,
		BotUsername:    acc.BotUsername,
		BotTokenMasked: maskBotToken(acc.BotToken),
		WebhookURL:     acc.WebhookURL,
		WebhookEnabled: acc.WebhookEnabled,
		AIAgentEnabled: acc.AIAgentEnabled,
		LastSyncAt:     acc.LastSyncAt,
		LastErrorAt:    acc.LastErrorAt,
		LastErrorMsg:   acc.LastErrorMsg,
		Status:         acc.Status,
		CreatedAt:      acc.CreatedAt,
		UpdatedAt:      acc.UpdatedAt,
	}
}

func maskBotToken(token string) string {
	if token == "" {
		return ""
	}
	if len(token) <= 8 {
		return strings.Repeat("*", len(token))
	}
	return token[:4] + strings.Repeat("*", len(token)-8) + token[len(token)-4:]
}

// List 列表
func (ctrl *TelegramAccountController) List(c *gin.Context) {
	accs, err := ctrl.svc.ListAccounts(context.Background())
	if err != nil {
		response.ErrorFromDB(c, err, "获取列表失败", err.Error())
		return
	}
	list := make([]telegramAccountVO, 0, len(accs))
	for _, acc := range accs {
		list = append(list, toTelegramAccountVO(acc))
	}
	response.SuccessWithList(c, list, int64(len(list)))
}

// Get 详情
func (ctrl *TelegramAccountController) Get(c *gin.Context) {
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
	response.Success(c, toTelegramAccountVO(acc), "获取成功")
}

type telegramAccountCreateReq struct {
	AccountName    string `json:"account_name" binding:"required"`
	BotToken       string `json:"bot_token" binding:"omitempty"`   // Create 时必填，Update 时留空=保持原值
	BotUsername    string `json:"bot_username" binding:"omitempty"` // optional：后端自动通过 getMe 填充
	WebhookURL     string `json:"webhook_url" binding:"omitempty"`  // optional：后端通过 public_base_url 自动推导
	WebhookSecret  string `json:"webhook_secret" binding:"omitempty"` // optional：后端自动生成
	WebhookEnabled *bool  `json:"webhook_enabled" binding:"omitempty"` // optional：后端根据有无公网自动设值
	AIAgentEnabled *bool  `json:"ai_agent_enabled" binding:"omitempty"` // optional：后端默认开启
	Status         *int   `json:"status" binding:"omitempty"`         // optional：默认 1（正常）
}

// Create 创建
// 简化：用户只需填 account_name + bot_token，后端自动：
//   - 调 getMe 填充 bot_username
//   - 生成 webhook_secret
//   - 通过 config.GetPublicBaseURL 推导 webhook_url
//   - 有公网则异步 setWebhook，无公网则自动降级 polling
func (ctrl *TelegramAccountController) Create(c *gin.Context) {
	var req telegramAccountCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误", err.Error())
		return
	}
	if req.BotToken == "" {
		response.Error(c, http.StatusBadRequest, "bot_token 必填", "")
		return
	}
	if vErr := tgbot.ValidateBotToken(req.BotToken); vErr != nil {
		response.Error(c, http.StatusBadRequest, "Bot Token 格式错误", vErr.Error())
		return
	}

	// 只取用户显式传入的字段；webhook/webhook_secret/webhook_enabled/ai_agent_enabled/bot_username 均由 service 自动填充
	acc := &model.TelegramAccount{
		AccountName: req.AccountName,
		BotToken:    req.BotToken,
		OwnerUserID: currentStaffUserID(c),
	}
	if req.BotUsername != "" {
		acc.BotUsername = req.BotUsername
	}
	if req.WebhookURL != "" {
		acc.WebhookURL = req.WebhookURL
	}
	if req.WebhookSecret != "" {
		acc.WebhookSecret = req.WebhookSecret
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

	created, err := ctrl.svc.CreateAccount(context.Background(), acc)
	if err != nil {
		response.ErrorFromDB(c, err, "创建失败", err.Error())
		return
	}
	response.Success(c, toTelegramAccountVO(created), "创建成功（Bot Token 验证 + Webhook/Polling 配置正在后台自动处理中）")
}

// Update 更新（Bot Token 为空时保持原值）
func (ctrl *TelegramAccountController) Update(c *gin.Context) {
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
	var req telegramAccountCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误", err.Error())
		return
	}
	if req.BotToken != "" {
		if vErr := tgbot.ValidateBotToken(req.BotToken); vErr != nil {
			response.Error(c, http.StatusBadRequest, "Bot Token 格式错误", vErr.Error())
			return
		}
	}
	acc.AccountName = req.AccountName
	if req.BotToken != "" {
		acc.BotToken = req.BotToken
	}
	if req.BotUsername != "" {
		acc.BotUsername = req.BotUsername
	}
	// webhook_secret 从不回显（VO 掩码），前端编辑表单回填空串：空值=保留原值，防止每次编辑把 secret 清空
	if req.WebhookSecret != "" {
		acc.WebhookSecret = req.WebhookSecret
	}
	if req.WebhookURL != "" {
		acc.WebhookURL = req.WebhookURL
	}
	// 指针类型：非 nil 才覆盖（nil = 保留原值）
	if req.WebhookEnabled != nil {
		acc.WebhookEnabled = *req.WebhookEnabled
	}
	if req.AIAgentEnabled != nil {
		acc.AIAgentEnabled = *req.AIAgentEnabled
	}
	if req.Status != nil {
		acc.Status = *req.Status
	}
	if err := ctrl.svc.UpdateAccount(context.Background(), acc); err != nil {
		response.ErrorFromDB(c, err, "更新失败", err.Error())
		return
	}
	response.Success(c, toTelegramAccountVO(acc), "更新成功")
}

// Delete 删除
func (ctrl *TelegramAccountController) Delete(c *gin.Context) {
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

// RegisterWebhook 调用 Telegram setWebhook 接口注册 webhook
// POST /api/telegram/accounts/:id/register-webhook
// body: {"webhook_url": "https://your-domain/api/webhook/telegram/{id}"}（可省略，省略时优先按 public_base_url / 请求 host 推导）
func (ctrl *TelegramAccountController) RegisterWebhook(c *gin.Context) {
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
	if acc.BotToken == "" {
		response.Error(c, http.StatusBadRequest, "BotToken 未配置", "")
		return
	}
	var req struct {
		WebhookURL string `json:"webhook_url"`
	}
	_ = c.ShouldBindJSON(&req)
	if req.WebhookURL != "" {
		acc.WebhookURL = req.WebhookURL
	}
	if acc.WebhookURL == "" {
		acc.WebhookURL = deriveTelegramWebhookURL(c, uint(id))
	}
	if acc.WebhookURL == "" {
		response.Error(c, http.StatusBadRequest, "WebhookURL 无法推导（请配置 external.public_base_url 或在请求 body 中显式传 webhook_url）", "")
		return
	}
	if vErr := service.ValidateTelegramWebhookURL(acc.WebhookURL); vErr != nil {
		now := time.Now()
		acc.LastErrorAt = &now
		acc.LastErrorMsg = "webhook URL 校验失败: " + vErr.Error()
		_ = ctrl.svc.UpdateAccount(context.Background(), acc)
		response.Error(c, http.StatusBadRequest, "WebhookURL 格式不合法", vErr.Error())
		return
	}
	if acc.WebhookSecret == "" {
		acc.WebhookSecret = service.GenTGWebhookSecret()
	}

	if err := tgbot.SetWebhook(acc.BotToken, acc.WebhookURL, acc.WebhookSecret); err != nil {
		now := time.Now()
		acc.LastErrorAt = &now
		acc.LastErrorMsg = err.Error()
		_ = ctrl.svc.UpdateAccount(context.Background(), acc)
		// 把 Telegram 原始报错翻译成可操作的提示（token 无效/URL 非 https 等），
		// 直接作为 message 返回给前端 toast 展示
		friendly := tgbot.FriendlyTGAPIError(err)
		response.Error(c, http.StatusBadRequest, friendly.Error(), err.Error())
		return
	}

	now := time.Now()
	acc.LastSyncAt = &now
	acc.LastErrorAt = nil
	acc.LastErrorMsg = ""
	acc.WebhookEnabled = true
	if acc.BotUsername == "" {
		if uname, gerr := tgbot.GetBotUsername(acc.BotToken); gerr == nil && uname != "" {
			acc.BotUsername = uname
		}
	}
	if err := ctrl.svc.UpdateAccount(context.Background(), acc); err != nil {
		response.ErrorFromDB(c, err, "保存状态失败", err.Error())
		return
	}
	service.StopTelegramPolling(acc.ID)
	response.Success(c, toTelegramAccountVO(acc), "Webhook 注册成功")
}

func deriveTelegramWebhookURL(c *gin.Context, accountID uint) string {
	return deriveTelegramWebhookURLWithBase(c, accountID, config.GetPublicBaseURL())
}

func deriveTelegramWebhookURLWithBase(c *gin.Context, accountID uint, publicBase string) string {
	if publicBase != "" {
		return strings.TrimRight(publicBase, "/") + fmt.Sprintf("/api/webhook/telegram/%d", accountID)
	}
	scheme := "https"
	if h := c.GetHeader("X-Forwarded-Proto"); h != "" {
		scheme = h
	} else if c != nil && c.Request != nil && c.Request.TLS == nil {
		scheme = "http"
	}
	host := ""
	if c != nil {
		host = c.GetHeader("X-Forwarded-Host")
		if host == "" && c.Request != nil {
			host = c.Request.Host
		}
	}
	return fmt.Sprintf("%s://%s/api/webhook/telegram/%d", scheme, host, accountID)
}

// Status 校验 Bot Token 与 webhook 注册状态（无需改动账号）
// GET /api/telegram/accounts/:id/status
func (ctrl *TelegramAccountController) Status(c *gin.Context) {
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
	bot, botErr := tgbot.GetMe(acc.BotToken)
	whInfo, whErr := tgbot.GetWebhookInfo(acc.BotToken)
	resp := gin.H{
		"account_id":       acc.ID,
		"account_name":     acc.AccountName,
		"bot_token_masked": maskBotToken(acc.BotToken),
		"ai_agent_enabled": acc.AIAgentEnabled,
		"status":           acc.Status,
		"webhook_enabled":  acc.WebhookEnabled,
		"webhook_url":      acc.WebhookURL,
		"last_sync_at":     acc.LastSyncAt,
		"last_error_at":    acc.LastErrorAt,
		"last_error_msg":   acc.LastErrorMsg,
		"bot":              bot,
		"bot_error":        errToStr(botErr),
		"webhook_info":     whInfo,
		"webhook_error":    errToStr(whErr),
		"polling_mode":     service.IsTelegramPollingEnabled(),
		"polling_owner":    acc.PollingOwner,
		"polling_heartbeat_at": acc.PollingHeartbeatAt,
	}
	response.Success(c, resp, "获取状态成功")
}

func errToStr(e error) string {
	if e == nil {
		return ""
	}
	return e.Error()
}

// TestSend 测试向指定 chat_id 发送一条消息，验证 Bot Token 可用性
// POST /api/telegram/accounts/:id/test-send
// body: {"chat_id": 123456, "text": "测试消息"}
func (ctrl *TelegramAccountController) TestSend(c *gin.Context) {
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
		ChatID int64  `json:"chat_id" binding:"required"`
		Text   string `json:"text" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误", err.Error())
		return
	}
	if err := tgbot.SendMessage(acc.BotToken, req.ChatID, req.Text); err != nil {
		friendly := tgbot.FriendlyTGAPIError(err)
		response.Error(c, http.StatusBadRequest, friendly.Error(), err.Error())
		return
	}
	response.Success(c, gin.H{"ok": true}, "发送成功")
}
