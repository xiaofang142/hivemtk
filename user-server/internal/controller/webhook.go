package controller

import (
	"context"
	"crypto/subtle"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"hivemtk-user/internal/service/translation"
)

// WebhookController 多渠道 Webhook 控制器
// 对应 多渠道 Webhook
// 通用路由 /api/webhook/:channel[/:account_id] 只受理 service.webhookInboundCapable
// 里那些确有入站适配器的渠道；钉钉/微信公众号各有专用回调入口，不走通用路由。
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
	if c.svc != nil {
		wh.Use(middleware.WebhookTenantBinding(c.svc.ResolveWebhookAccountTenant))
	}
	{
		wh.POST("/:channel/:account_id", c.Receive)
		wh.POST("/:channel", c.ReceiveWithoutAccount)
		// 队列水位（待处理事件数、积压长度）是运营内部指标，不属于渠道回调面：
		// /health 留给外部探活（只有一个 ok），stats 改到超管可读——
		// 判据是"这条口的读者是谁"：渠道方与监控脚本只读健康位，水位是给后台看板用的。
		// TenantMiddleware 不能省：uni_webhook_events 是租户表，on 模式下没有租户绑定
		// 的 Count 会被隔离 callback 拒绝，而 PendingCount 把错误丢进返回值外，
		// 于是这条口在超管面前永远报 0（看着"没有积压"的库其实堵满了）。
		wh.GET("/stats", middleware.JWTAuthMiddleware(), middleware.TenantMiddleware(), middleware.AdminAuthMiddleware(), c.Stats)
		wh.GET("/health", c.Health)
		wh.GET("/wecom/:account_id", c.WeComVerify)
		wh.GET("/feishu/:account_id", c.FeishuVerify)
		wh.GET("/whatsapp/:account_id", c.WhatsAppVerify)
		wh.GET("/dingtalk/:account_id", c.DingTalkVerify)
		wh.POST("/dingtalk/:account_id", c.DingTalkReceive)
	}
}

// bindCallbackTenant 渠道回调的租户绑定（specs/001-saas-multi-tenant T022）。
//
// on 模式下由 URL 的 (channel, account_id) 自证归属：解析不出（账号不存在，或
// 同一个键在两个租户各有一行）就回 404 并报安全告警，返回 ok=false。
// 绝不回落默认租户 —— 那会把 A 的客户消息落进 B 的收件箱并驱动 B 的 AI 回复。
// off 模式原样透传 reqCtx（写入走列默认值 1，与既有行为等价）。
//
// 组级 WebhookTenantBinding 已经对同一条链做过一次；这里再调一次是因为回绝
// 必须发生在控制器（它才知道该回 404 而不是 500），而解析函数命中已绑定时
// 直接复用，不会多跑一趟 SQL。
func (c *WebhookController) bindCallbackTenant(ctx *gin.Context, reqCtx context.Context, channel service.WebhookChannel, accountID string) (context.Context, bool) {
	if !tenant.IsMultiTenant() || c.svc == nil {
		return reqCtx, true
	}
	tid, terr := c.svc.ResolveTenantByChannelAccount(ctx.Request.Context(), channel, accountID)
	if terr != nil {
		log.Printf("[SECURITY] webhook 租户解析失败 channel=%s account=%s ip=%s: %v",
			channel, accountID, ctx.ClientIP(), terr)
		response.Envelope(ctx, http.StatusNotFound, gin.H{"accepted": false, "reason": "unknown account"})
		return reqCtx, false
	}
	return tenant.WithTenantID(reqCtx, tid), true
}

// Receive godoc
// @Summary      接收渠道 Webhook 回调
// @Description  通用渠道回调入口，按 channel 路由到不同处理逻辑
// @Tags         Webhook
// @Accept       json
// @Produce      json
// @Param        channel     path   string  true   "渠道：wecom/whatsapp/telegram/qq/feishu/douyin/tiktok（其余渠道有专用入口或无服务端回调，通用路由返回 400）"
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
		response.Envelope(ctx, http.StatusBadRequest, gin.H{
			"accepted": false,
			"reason":   "read body: " + err.Error(),
		})
		return
	}

	reqCtx := middleware.InjectLangToCtx(ctx.Request.Context(), c.langResolver, "", 0)

	// 租户解析链（specs/001-saas-multi-tenant T022）：on 模式下按 (channel, account_id)
	// 解析归属租户并绑定 ctx；解析失败 fail-closed（404 + 安全告警），绝不落默认租户。
	// off 模式：数据写入走列默认值 1，等价现状。
	reqCtx, bound := c.bindCallbackTenant(ctx, reqCtx, channel, accountID)
	if !bound {
		return
	}

	// QQ 开放平台 Op13 回调地址验证：需同步返回 Ed25519 签名，不走入队流程
	if channel == service.ChannelQQ {
		if handled, payload := c.svc.HandleQQCallbackChallenge(reqCtx, accountID, body); handled {
			response.Envelope(ctx, http.StatusOK, payload)
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
			response.Envelope(ctx, http.StatusOK, gin.H{"challenge": challenge})
			return
		}
	}

	// 抖音开放平台「保存回调地址」握手：必须把 challenge 原样放进响应体，
	// 否则控制台那一步就过不去，后续 im_receive_msg 一条都收不到（批G-2）。
	// 只挂在 douyin 上：TikTok 是否需要同样的回显尚未取到可引用原文（审计 §16.3），
	// 不拿别家的契约往它身上套。
	if channel == service.ChannelDouyin {
		challenge, handled, verr := c.svc.HandleDouyinURLVerification(reqCtx, accountID, body, extractHeaders(ctx))
		if handled {
			if verr != nil {
				ctx.String(http.StatusUnauthorized, "douyin url_verification failed: "+verr.Error())
				return
			}
			ctx.Data(http.StatusOK, "application/json; charset=utf-8", challenge)
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
		response.Envelope(ctx, http.StatusServiceUnavailable, result)
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
	response.Envelope(ctx, status, result)
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
		response.Envelope(ctx, http.StatusBadRequest, gin.H{
			"accepted": false,
			"reason":   "read body: " + err.Error(),
		})
		return
	}

	accountID := extractAccountID(body)

	reqCtx := middleware.InjectLangToCtx(ctx.Request.Context(), c.langResolver, "", 0)

	// 这条口没有 account_id 路径段，归属只能由载荷里的 account_id 与渠道名自证；
	// 与通用入口同一套判据、同一个 fail-closed 后果。
	reqCtx, bound := c.bindCallbackTenant(ctx, reqCtx, channel, accountID)
	if !bound {
		return
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
	if !result.Accepted {
		status = http.StatusBadRequest
	}
	response.Envelope(ctx, status, result)
}

// @Summary      企微 URL 验证（GET 挑战）
// @Tags      webhook
// @Router    /api/webhook/wecom/{account_id} [get]
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

	token, aesKey, err := c.svc.GetWeComSecrets(ctx.Request.Context(), accountID)
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
	response.Envelope(ctx, http.StatusOK, gin.H{"challenge": challenge})
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

// @Summary      钉钉回调 URL 验证（GET）
// @Tags      webhook
// @Router    /api/webhook/dingtalk/{account_id} [get]
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

// @Summary      钉钉回调收消息（POST）
// @Tags      webhook
// @Router    /api/webhook/dingtalk/{account_id} [post]
// DingTalkReceive 钉钉回调收消息（POST）
// 路由: POST /api/webhook/dingtalk/{account_id}
// 请求体为 {"encrypt":"..."}（配置 AESKey 时）或明文消息 JSON。
// 解析后入消息中台并经统一 AI 派发管线触发智能体。
func (c *WebhookController) DingTalkReceive(ctx *gin.Context) {
	accountIDStr := ctx.Param("account_id")
	accountID, err := strconv.ParseUint(accountIDStr, 10, 64)
	if err != nil {
		response.Envelope(ctx, http.StatusBadRequest, gin.H{"accepted": false, "reason": "invalid account_id"})
		return
	}
	if c.dtAppSvc == nil {
		response.Envelope(ctx, http.StatusServiceUnavailable, gin.H{"accepted": false, "reason": "dingtalk app service not configured"})
		return
	}
	body, err := service.ReadAll(ctx.Request.Body)
	if err != nil {
		response.Envelope(ctx, http.StatusBadRequest, gin.H{"accepted": false, "reason": "read body: " + err.Error()})
		return
	}
	reqCtx := middleware.InjectLangToCtx(ctx.Request.Context(), c.langResolver, "", 0)
	if err := c.dtAppSvc.ReceiveMessage(reqCtx, uint(accountID), body, extractQuery(ctx), extractHeaders(ctx)); err != nil {
		response.Envelope(ctx, http.StatusBadRequest, gin.H{"accepted": false, "reason": err.Error()})
		return
	}
	response.Envelope(ctx, http.StatusOK, gin.H{"accepted": true})
}

// @Summary      Webhook 队列统计（待处理/队列长度）
// @Tags      webhook
// @Router    /api/webhook/stats [get]
// Stats 统计
func (c *WebhookController) Stats(ctx *gin.Context) {
	pending := c.svc.PendingCount(ctx.Request.Context())
	queueLen := c.svc.QueueLen(ctx.Request.Context())
	response.Success(ctx, gin.H{
		"pending_events": pending,
		"queue_length":   queueLen,
	}, "ok")
}

// @Summary      健康检查
// @Tags      webhook
// @Router    /api/webhook/health [get]
// Health 健康检查
func (c *WebhookController) Health(ctx *gin.Context) {
	response.Success(ctx, gin.H{"status": "ok"}, "ok")
}

func extractHeaders(ctx *gin.Context) map[string]string {
	headers := make(map[string]string)
	for _, k := range []string{
		"X-Signature", "Signature", "X-Hub-Signature-256",
		"X-Douyin-Signature", "X-Lark-Signature",
		// TikTok Business 事件回调（官方把 t=/s= 两段放在同一个头里）。
		// 不列进白名单的话 service 层永远看不到签名，真实回调会以「缺头」被拒，
		// 而 service 单测自己构造 headers 照样全绿 —— 批A 钉钉踩过的同一个洞。
		"TikTok-Signature",
		"X-Wechat-Timestamp", "X-Wechat-Nonce", "X-Wechat-Signature",
		"X-Telegram-Bot-Api-Secret-Token",
		// QQ 开放平台 Ed25519 验签头（webhook 事件推送必带）
		"X-Signature-Ed25519", "X-Signature-Timestamp",
		// 钉钉企业内部机器人明文回调验签头（官方就叫 timestamp / sign）
		"Timestamp", "Sign",
	} {
		if v := ctx.GetHeader(k); v != "" {
			headers[k] = v
		}
	}
	return headers
}

func extractQuery(ctx *gin.Context) map[string]string {
	q := make(map[string]string)
	for _, k := range []string{"signature", "msg_signature", "timestamp", "nonce", "echostr", "challenge", "token", "type"} {
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
