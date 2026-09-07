package controller

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"hivemtk-user/internal/service/translation"
)

// WebhookController 多渠道 Webhook 控制器
// 对应 多渠道 Webhook
// 提供 9 个渠道的回调入口
type WebhookController struct {
	svc          *service.WebhookService
	waCloudSvc   *service.WhatsAppCloudService
	dtAppSvc     *service.DingTalkAppService
	feishuSvc    *service.FeishuService
	langResolver *translation.LangConfigResolver
}

// NewWebhookController 创建 Webhook 控制器
func NewWebhookController(svc *service.WebhookService) *WebhookController {
	return &WebhookController{
		svc: svc,
	}
}

// SetWhatsAppCloudService 注入 WhatsApp Cloud 账号服务（用于回调 URL 验证）
func (c *WebhookController) SetWhatsAppCloudService(svc *service.WhatsAppCloudService) {
	c.waCloudSvc = svc
}

// SetDingTalkAppService 注入钉钉应用账号服务（用于回调验签 + 入站收消息）
func (c *WebhookController) SetDingTalkAppService(svc *service.DingTalkAppService) {
	c.dtAppSvc = svc
}

// SetFeishuService 注入飞书账号服务（用于回调 URL 验证：比对 VerifyToken）
func (c *WebhookController) SetFeishuService(svc *service.FeishuService) {
	c.feishuSvc = svc
}

// SetLangResolver 注入多语言解析器（v1.2 出海方案）。
// 未注入时仍可正常工作，ctx 中语言走默认 zh 兜底。
func (c *WebhookController) SetLangResolver(r *translation.LangConfigResolver) {
	c.langResolver = r
}

// Stop 关闭后台 worker（用于测试或优雅退出）
func (c *WebhookController) Stop() {
	if c.svc != nil {
		c.svc.Stop(context.Background())
	}
}

// RegisterRoutes 注册路由（公开，不需要鉴权）
func (c *WebhookController) RegisterRoutes(r *gin.Engine) {
	wh := r.Group("/api/webhook")
	{
		wh.POST("/:channel/:account_id", c.Receive)
		wh.POST("/:channel", c.ReceiveWithoutAccount)
		wh.GET("/stats", c.Stats)
		wh.GET("/health", c.Health)
		wh.GET("/wecom/:account_id", c.WeComVerify)
		wh.GET("/feishu/:account_id", c.FeishuVerify)
		wh.GET("/whatsapp/:account_id", c.WhatsAppVerify)
		wh.GET("/dingtalk/:account_id", c.DingTalkVerify)
		wh.POST("/dingtalk/:account_id", c.DingTalkReceive)
	}
}

// Receive godoc
// @Summary      接收渠道 Webhook 回调
// @Description  通用渠道回调入口，按 channel 路由到不同处理逻辑
// @Tags         Webhook
// @Accept       json
// @Produce      json
// @Param        channel     path   string  true   "渠道：wechat/wecom/douyin/xiaohongshu/email"
// @Param        account_id  path   string  true   "账号 ID"
// @Param        body        body   object  true   "渠道原始 payload"
// @Success      200  {object}  response.Response  "处理成功"
// @Failure      400  {object}  response.Response  "参数错误"
// @Router       /api/webhook/{channel}/{account_id} [post]
func (c *WebhookController) Receive(ctx *gin.Context) {
	channel := service.WebhookChannel(strings.ToLower(ctx.Param("channel")))
	accountID := ctx.Param("account_id")

	body, err := service.ReadAll(ctx.Request.Body)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"accepted": false,
			"reason":   "read body: " + err.Error(),
		})
		return
	}

	reqCtx := middleware.InjectLangToCtx(ctx.Request.Context(), c.langResolver, "", 0)

	// QQ 开放平台 Op13 回调地址验证：需同步返回 Ed25519 签名，不走入队流程
	if channel == service.ChannelQQ {
		if handled, payload := c.svc.HandleQQCallbackChallenge(reqCtx, accountID, body); handled {
			ctx.JSON(http.StatusOK, payload)
			return
		}
	}

	if channel == service.ChannelFeishu {
		challenge, handled, verr := c.svc.HandleFeishuURLVerification(reqCtx, accountID, body)
		if handled {
			if verr != nil {
				ctx.String(http.StatusUnauthorized, "feishu url_verification failed: "+verr.Error())
				return
			}
			ctx.JSON(http.StatusOK, gin.H{"challenge": challenge})
			return
		}
	}

	req := &service.ReceiveRequest{
		Channel:   channel,
		AccountID: accountID,
		Body:      body,
		Headers:   extractHeaders(ctx),
		SourceIP:  ctx.ClientIP(),
		Query:     extractQuery(ctx),
	}

	result, _ := c.svc.Receive(reqCtx, req)
	status := http.StatusOK
	if result.QueueFull {
		ctx.Header("Retry-After", "30")
		ctx.JSON(http.StatusServiceUnavailable, result)
		return
	}
	if !result.Accepted {
		status = http.StatusBadRequest
		if result.VerifyFail {
			status = http.StatusUnauthorized
		}
		if result.RateLimit {
			status = http.StatusTooManyRequests
		}
	}
	ctx.JSON(status, result)
}

// ReceiveWithoutAccount godoc
// @Summary      接收渠道 Webhook 回调（无 account_id）
// @Description  某些渠道不需要账号上下文，如飞书事件订阅
// @Tags         Webhook
// @Accept       json
// @Produce      json
// @Param        channel  path   string  true  "渠道"
// @Param        body     body   object  true  "原始 payload"
// @Success      200  {object}  response.Response  "成功"
// @Router       /api/webhook/{channel} [post]
func (c *WebhookController) ReceiveWithoutAccount(ctx *gin.Context) {
	channel := service.WebhookChannel(strings.ToLower(ctx.Param("channel")))

	body, err := service.ReadAll(ctx.Request.Body)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"accepted": false,
			"reason":   "read body: " + err.Error(),
		})
		return
	}

	accountID := extractAccountID(body)

	reqCtx := middleware.InjectLangToCtx(ctx.Request.Context(), c.langResolver, "", 0)

	req := &service.ReceiveRequest{
		Channel:   channel,
		AccountID: accountID,
		Body:      body,
		Headers:   extractHeaders(ctx),
		SourceIP:  ctx.ClientIP(),
		Query:     extractQuery(ctx),
	}

	result, _ := c.svc.Receive(reqCtx, req)
	status := http.StatusOK
	if !result.Accepted {
		status = http.StatusBadRequest
	}
	ctx.JSON(status, result)
}

// WeComVerify 企微 URL 验证（GET 挑战）
// 路由: GET /api/webhook/wecom/{account_id}?msg_signature=...&timestamp=...&nonce=...&echostr=...
func (c *WebhookController) WeComVerify(ctx *gin.Context) {
	accountID := ctx.Param("account_id")
	sig := ctx.Query("msg_signature")
	ts := ctx.Query("timestamp")
	nonce := ctx.Query("nonce")
	echo := ctx.Query("echostr")

	if sig == "" || ts == "" || nonce == "" || echo == "" {
		ctx.String(http.StatusBadRequest, "missing signature params")
		return
	}

	token, aesKey, err := c.svc.GetWeComSecrets(context.Background(), accountID)
	if err != nil || token == "" {
		ctx.String(http.StatusUnauthorized, "account not found or token missing")
		return
	}
	plain, err := service.VerifyURL(token, aesKey, sig, ts, nonce, echo)
	if err != nil {
		ctx.String(http.StatusUnauthorized, "verify failed: "+err.Error())
		return
	}
	ctx.String(http.StatusOK, plain)
}

// FeishuVerify 飞书 URL 验证（GET 挑战）
// 路由: GET /api/webhook/feishu/{account_id}?challenge=...&token=...&type=url_verification
//
// 飞书 URL 验证：先校验 query 的 token 与账号存储的 VerificationToken 一致，
// 一致才回显 challenge（与 WhatsAppVerify / DingTalkVerify 同模式，避免任意第三方
// 伪造挑战完成回调地址绑定）。
// FeishuVerify godoc
// @Summary      飞书回调 URL 验证
// @Description  飞书事件订阅 URL 配置时调用，回显 challenge 完成验证
// @Tags         Webhook
// @Produce      json
// @Param        account_id  path   string  true  "账号 ID"
// @Param        challenge   query  string  true  "飞书 challenge"
// @Param        token       query  string  true  "VerificationToken"
// @Success      200  {object}  map[string]string  "验证通过"
// @Router       /api/webhook/feishu/{account_id} [get]
func (c *WebhookController) FeishuVerify(ctx *gin.Context) {
	accountIDStr := ctx.Param("account_id")
	accountID, err := strconv.ParseUint(accountIDStr, 10, 64)
	if err != nil {
		ctx.String(http.StatusBadRequest, "invalid account_id")
		return
	}
	challenge := ctx.Query("challenge")
	if challenge == "" {
		ctx.String(http.StatusBadRequest, "missing challenge")
		return
	}
	token := ctx.Query("token")
	if token == "" {
		ctx.String(http.StatusBadRequest, "missing token")
		return
	}
	if c.feishuSvc == nil {
		ctx.String(http.StatusServiceUnavailable, "feishu service not configured")
		return
	}
	acc, err := c.feishuSvc.GetAccount(ctx.Request.Context(), uint(accountID))
	if err != nil || acc == nil {
		ctx.String(http.StatusNotFound, "account not found")
		return
	}

	if acc.VerificationToken == "" {
		ctx.String(http.StatusForbidden, "account not configured for verification")
		return
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(acc.VerificationToken)) != 1 {
		ctx.String(http.StatusForbidden, "verification failed")
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"challenge": challenge})
}

// WhatsAppVerify 处理 Meta 回调 URL 验证挑战（GET）
// 路由: GET /api/webhook/whatsapp/{account_id}
// Meta 在「Webhook 配置」中填入回调 URL 后会发送 GET 校验：
//
//	hub.mode=subscribe & hub.verify_token=<配置的 token> & hub.challenge=<随机串>
//
// 仅当 hub.verify_token 与账号存储的 VerifyToken 一致时才回显 hub.challenge。
// WhatsAppVerify godoc
// @Summary      WhatsApp Cloud 回调 URL 验证
// @Description  Meta 配置 Webhook 时调用，校验 verify_token 后回显 challenge
// @Tags         Webhook
// @Produce      plain
// @Param        account_id      path  string  true  "账号 ID"
// @Param        hub.mode        query string true  "固定 subscribe"
// @Param        hub.verify_token query string true  "校验 token"
// @Param        hub.challenge   query string true  "回显 challenge"
// @Success      200  {string}  string  "challenge"
// @Router       /api/webhook/whatsapp/{account_id} [get]
func (c *WebhookController) WhatsAppVerify(ctx *gin.Context) {
	accountIDStr := ctx.Param("account_id")
	accountID, err := strconv.ParseUint(accountIDStr, 10, 64)
	if err != nil {
		ctx.String(http.StatusBadRequest, "invalid account_id")
		return
	}
	if c.waCloudSvc == nil {
		ctx.String(http.StatusServiceUnavailable, "whatsapp cloud service not configured")
		return
	}
	acc, err := c.waCloudSvc.GetAccount(ctx.Request.Context(), uint(accountID))
	if err != nil || acc == nil {
		ctx.String(http.StatusNotFound, "account not found")
		return
	}
	mode := ctx.Query("hub.mode")
	token := ctx.Query("hub.verify_token")
	challenge := ctx.Query("hub.challenge")
	if mode != "subscribe" || subtle.ConstantTimeCompare([]byte(token), []byte(acc.VerifyToken)) != 1 {
		ctx.String(http.StatusForbidden, "verification failed")
		return
	}
	ctx.String(http.StatusOK, challenge)
}

// DingTalkVerify 钉钉回调 URL 验证（GET）
// 路由: GET /api/webhook/dingtalk/{account_id}?signature=...&timestamp=...&nonce=...&echostr=...
// 钉钉对配置的回调地址发起 GET，携带 signature/timestamp/nonce/echostr。
// 本地用 token 计算 signature 比对；一致则回显 echostr（配置了 AESKey 时先解密）。
func (c *WebhookController) DingTalkVerify(ctx *gin.Context) {
	accountIDStr := ctx.Param("account_id")
	accountID, err := strconv.ParseUint(accountIDStr, 10, 64)
	if err != nil {
		ctx.String(http.StatusBadRequest, "invalid account_id")
		return
	}
	if c.dtAppSvc == nil {
		ctx.String(http.StatusServiceUnavailable, "dingtalk app service not configured")
		return
	}
	signature := ctx.Query("signature")
	timestamp := ctx.Query("timestamp")
	nonce := ctx.Query("nonce")
	echostr := ctx.Query("echostr")
	if signature == "" || timestamp == "" || nonce == "" || echostr == "" {
		ctx.String(http.StatusBadRequest, "missing signature params")
		return
	}
	plain, err := c.dtAppSvc.VerifyCallback(ctx.Request.Context(), uint(accountID), signature, timestamp, nonce, echostr)
	if err != nil {
		ctx.String(http.StatusUnauthorized, "verify failed: "+err.Error())
		return
	}
	ctx.String(http.StatusOK, plain)
}

// DingTalkReceive 钉钉回调收消息（POST）
// 路由: POST /api/webhook/dingtalk/{account_id}
// 请求体为 {"encrypt":"..."}（配置 AESKey 时）或明文消息 JSON。
// 解析后入消息中台并经统一 AI 派发管线触发智能体。
func (c *WebhookController) DingTalkReceive(ctx *gin.Context) {
	accountIDStr := ctx.Param("account_id")
	accountID, err := strconv.ParseUint(accountIDStr, 10, 64)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"accepted": false, "reason": "invalid account_id"})
		return
	}
	if c.dtAppSvc == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"accepted": false, "reason": "dingtalk app service not configured"})
		return
	}
	body, err := service.ReadAll(ctx.Request.Body)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"accepted": false, "reason": "read body: " + err.Error()})
		return
	}
	reqCtx := middleware.InjectLangToCtx(ctx.Request.Context(), c.langResolver, "", 0)
	if err := c.dtAppSvc.ReceiveMessage(reqCtx, uint(accountID), body); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"accepted": false, "reason": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"accepted": true})
}

// Stats 统计
func (c *WebhookController) Stats(ctx *gin.Context) {
	pending := c.svc.PendingCount(context.Background())
	queueLen := c.svc.QueueLen(context.Background())
	response.Success(ctx, gin.H{
		"pending_events": pending,
		"queue_length":   queueLen,
	}, "ok")
}

// Health 健康检查
func (c *WebhookController) Health(ctx *gin.Context) {
	response.Success(ctx, gin.H{"status": "ok"}, "ok")
}

func extractHeaders(ctx *gin.Context) map[string]string {
	headers := make(map[string]string)
	for _, k := range []string{
		"X-Signature", "Signature", "X-Hub-Signature-256",
		"X-Douyin-Signature", "X-Lark-Signature",
		"X-Wechat-Timestamp", "X-Wechat-Nonce", "X-Wechat-Signature",
		"X-Telegram-Bot-Api-Secret-Token",
		// QQ 开放平台 Ed25519 验签头（webhook 事件推送必带）
		"X-Signature-Ed25519", "X-Signature-Timestamp",
	} {
		if v := ctx.GetHeader(k); v != "" {
			headers[k] = v
		}
	}
	return headers
}

func extractQuery(ctx *gin.Context) map[string]string {
	q := make(map[string]string)
	for _, k := range []string{"msg_signature", "timestamp", "nonce", "echostr", "challenge", "token", "type"} {
		if v := ctx.Query(k); v != "" {
			q[k] = v
		}
	}
	return q
}

func extractAccountID(body []byte) string {
	s := string(body)
	idx := strings.Index(s, "\"account_id\"")
	if idx < 0 {
		idx = strings.Index(s, "\"accountId\"")
	}
	if idx < 0 {
		return ""
	}
	rest := s[idx:]
	colon := strings.Index(rest, ":")
	if colon < 0 {
		return ""
	}
	rest = rest[colon+1:]
	rest = strings.TrimSpace(rest)
	if !strings.HasPrefix(rest, "\"") {
		return ""
	}
	rest = rest[1:]
	end := strings.Index(rest, "\"")
	if end < 0 {
		return ""
	}
	return rest[:end]
}

// SetSalesEngine 注入 智能体引擎（在 main.go 中调用）
func (c *WebhookController) SetSalesEngine(e *service.SalesEngine) {
	if c.svc != nil {
		c.svc.SetSalesEngine(context.Background(), e)
	}
}

// SetSmartOrchestrator 注入智能体统一编排器（在 router 中调用）
// 注入后 Webhook 入站消息会走 SmartCSOrchestrator.HandleIncoming 9 步编排
func (c *WebhookController) SetSmartOrchestrator(o *service.SmartCSOrchestrator) {
	if c.svc != nil {
		c.svc.SetSmartOrchestrator(context.Background(), o)
	}
}

// SetAgentBindingService 注入渠道智能体绑定服务（在 router 中调用）
// 注入后 triggerSalesEngine/triggerSmartOrchestrator 会先按 (channel_type, account_id)
// 加载绑定的智能体上下文，再调用 SalesEngine.HandleWithAgent 按智能体配置执行
func (c *WebhookController) SetAgentBindingService(svc *service.ChannelAgentBindingService) {
	if c.svc != nil {
		c.svc.SetAgentBindingService(context.Background(), svc)
	}
}
