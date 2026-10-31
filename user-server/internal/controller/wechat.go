package controller

import (
	"context"
	"fmt"
	"net/http"
	"runtime/debug"
	"strconv"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// WechatController 微信公众号控制器
type WechatController struct {
	svc        *service.WechatService
	ingressSvc *service.InboxIngressService
}

// NewWechatController 创建微信公众号控制器
func NewWechatController(svc *service.WechatService) *WechatController {
	return &WechatController{svc: svc}
}

// SetIngressSvc 设置 InboxIngress 管道（用于消息进入智能体流程）
func (c *WechatController) SetIngressSvc(ingress *service.InboxIngressService) {
	c.ingressSvc = ingress
}

// RegisterRoutes 注册路由
func (c *WechatController) RegisterRoutes(auth *gin.RouterGroup) {

	auth.GET("/wechat/accounts", c.ListAccounts)
	auth.POST("/wechat/accounts", c.CreateAccount)
	auth.PUT("/wechat/accounts/:id", c.UpdateAccount)
	auth.DELETE("/wechat/accounts/:id", c.DeleteAccount)

	auth.POST("/wechat/send", c.SendMessage)
}

// RegisterWebhookRoutes 注册 webhook 路由（不需要认证）
func (c *WechatController) RegisterWebhookRoutes(r *gin.RouterGroup) {

	r.GET("/webhook/wechat/:account_id", c.VerifyURL)

	r.POST("/webhook/wechat/:account_id", c.ReceiveMessage)
}

// @Summary      查询微信账号列表
// @Tags      wechat
// @Router    /api/wechat/accounts [get]
// ListAccounts 列出公众号账号
func (c *WechatController) ListAccounts(ctx *gin.Context) {
	accounts, err := c.svc.ListAccounts(ctx.Request.Context())
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "查询失败: "+err.Error())
		return
	}
	response.Success(ctx, accounts, "查询成功")
}

type createWechatAccountReq struct {
	AppID          string `json:"app_id" binding:"required"`
	AppSecret      string `json:"app_secret" binding:"required"`
	OriginalID     string `json:"original_id,omitempty"`
	Token          string `json:"token" binding:"required"`
	EncodingAESKey string `json:"encoding_aes_key,omitempty"`
	AgentID        string `json:"agent_id,omitempty"`
}

// @Summary      创建微信账号
// @Tags      wechat
// @Router    /api/wechat/accounts [post]
func (c *WechatController) CreateAccount(ctx *gin.Context) {
	var req createWechatAccountReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：请检查字段类型与必填项")
		return
	}

	acc := &model.WechatAccount{
		AppID:          req.AppID,
		AppSecret:      req.AppSecret,
		OriginalID:     req.OriginalID,
		Token:          req.Token,
		EncodingAESKey: req.EncodingAESKey,
		AgentID:        req.AgentID,
		Status:         "active",
	}
	if err := c.svc.CreateAccount(ctx.Request.Context(), acc); err != nil {
		response.Error(ctx, http.StatusInternalServerError, "创建失败: "+err.Error())
		return
	}
	response.Success(ctx, nil, "创建成功")
}

// @Summary      更新微信账号
// @Tags      wechat
// @Router    /api/wechat/accounts/{id} [put]
// UpdateAccount 更新公众号账号
func (c *WechatController) UpdateAccount(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 ID")
		return
	}

	var req createWechatAccountReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：请检查字段类型与必填项")
		return
	}

	acc := &model.WechatAccount{
		ID:             uint(id),
		AppID:          req.AppID,
		AppSecret:      req.AppSecret,
		OriginalID:     req.OriginalID,
		Token:          req.Token,
		EncodingAESKey: req.EncodingAESKey,
		AgentID:        req.AgentID,
		Status:         "active",
	}
	if err := c.svc.UpdateAccount(ctx.Request.Context(), acc); err != nil {
		response.ErrorFromDB(ctx, err, "更新失败: "+err.Error())
		return
	}
	response.Success(ctx, nil, "更新成功")
}

// @Summary      删除微信账号
// @Tags      wechat
// @Router    /api/wechat/accounts/{id} [delete]
// DeleteAccount 删除公众号账号
func (c *WechatController) DeleteAccount(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 ID")
		return
	}
	if err := c.svc.DeleteAccount(ctx.Request.Context(), uint(id)); err != nil {
		response.ErrorFromDB(ctx, err, "删除失败: "+err.Error())
		return
	}
	response.Success(ctx, nil, "删除成功")
}

type sendWechatMsgReq struct {
	AccountID uint   `json:"account_id" binding:"required"`
	OpenID    string `json:"open_id" binding:"required"`
	MsgType   string `json:"msg_type" binding:"required"`
	Content   string `json:"content" binding:"required"`
}

// @Summary      发送微信消息
// @Tags      wechat
// @Router    /api/wechat/send [post]
func (c *WechatController) SendMessage(ctx *gin.Context) {
	var req sendWechatMsgReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：请检查字段类型与必填项")
		return
	}

	msgID, err := c.svc.SendCustomMessage(ctx.Request.Context(), req.AccountID, req.OpenID, req.MsgType, req.Content)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "发送失败: "+err.Error())
		return
	}
	response.Success(ctx, gin.H{
		"message_id": msgID,
		"channel":    "wechat",
		"status":     "sent",
	}, "发送成功")
}

// bindWebhookTenant 公众号回调（公网、无登录态）的租户绑定。
//
// 公众号这一路的账号身份是 wechat_accounts 的主键，与通用回调那条 (渠道, 账号串)
// 不是同一个标识空间，所以自证只能按 id 反查这张表（判据见 WechatService.ResolveAccountTenant）。
// 这一步必须在读账号**之前**：on 模式下没有租户绑定的 ctx 读租户表会被隔离 callback
// 直接拒，于是每一次微信回调都落在 "account not found" 上——签名本来是对的，
// 配置却永远验证不过去，消息一条也进不了收件箱。
//
// 解析不出（行不存在/库不可用）回 ok=false，由调用方按既有的 404 分支回绝；
// 绝不回落默认租户。off 模式原样透传。
func (c *WechatController) bindWebhookTenant(ctx *gin.Context, accountID uint) (context.Context, bool) {
	reqCtx := ctx.Request.Context()
	if !tenant.IsMultiTenant() {
		return reqCtx, true
	}
	if tid, bound := tenant.FromContext(reqCtx); bound && tid != 0 {
		return reqCtx, true
	}
	tid, err := c.svc.ResolveAccountTenant(reqCtx, accountID)
	if err != nil {
		logger.Warnf("[Wechat] 回调租户解析失败 account=%d path=%s ip=%s: %v",
			accountID, ctx.FullPath(), ctx.ClientIP(), err)
		return reqCtx, false
	}
	if tid == 0 {
		return reqCtx, false
	}
	bound := tenant.OnlyTenant(reqCtx, tid)
	ctx.Request = ctx.Request.WithContext(bound)
	return bound, true
}

// @Summary      微信服务器配置验证
// @Tags      wechat
// @Router    /api/webhook/wechat/{account_id} [get]
// VerifyURL 微信服务器配置验证
// GET /api/webhook/wechat/{account_id}?signature=xxx&timestamp=xxx&nonce=xxx&echostr=xxx
func (c *WechatController) VerifyURL(ctx *gin.Context) {
	accountIDStr := ctx.Param("account_id")
	accountID, err := strconv.ParseUint(accountIDStr, 10, 64)
	if err != nil {
		ctx.String(http.StatusBadRequest, "invalid account_id")
		return
	}

	signature := ctx.Query("signature")
	timestamp := ctx.Query("timestamp")
	nonce := ctx.Query("nonce")
	echostr := ctx.Query("echostr")

	reqCtx, ok := c.bindWebhookTenant(ctx, uint(accountID))
	if !ok {
		ctx.String(http.StatusNotFound, "account not found")
		return
	}

	acc, err := c.svc.GetAccount(reqCtx, uint(accountID))
	if err != nil {
		ctx.String(http.StatusNotFound, "account not found")
		return
	}
	if acc.Token == "" {
		ctx.String(http.StatusBadRequest, "token not configured")
		return
	}

	if c.svc.VerifySignature(acc.Token, signature, timestamp, nonce) {
		ctx.String(http.StatusOK, echostr)
		return
	}
	ctx.String(http.StatusForbidden, "signature verification failed")
}

// @Summary      接收微信消息
// @Tags      wechat
// @Router    /api/webhook/wechat/{account_id} [post]
// ReceiveMessage 接收微信消息
// POST /api/webhook/wechat/{account_id}
// 安全：必须校验微信签名，防伪造消息注入（与 GET VerifyURL 一致）
func (c *WechatController) ReceiveMessage(ctx *gin.Context) {
	accountIDStr := ctx.Param("account_id")
	accountID, err := strconv.ParseUint(accountIDStr, 10, 64)
	if err != nil {
		ctx.String(http.StatusBadRequest, "invalid account_id")
		return
	}

	reqCtx, ok := c.bindWebhookTenant(ctx, uint(accountID))
	if !ok {
		ctx.String(http.StatusNotFound, "account not found")
		return
	}

	acc, err := c.svc.GetAccount(reqCtx, uint(accountID))
	if err != nil {
		ctx.String(http.StatusNotFound, "account not found")
		return
	}
	if acc.Token == "" {
		ctx.String(http.StatusBadRequest, "token not configured")
		return
	}
	signature := ctx.Query("signature")
	timestamp := ctx.Query("timestamp")
	nonce := ctx.Query("nonce")
	if !c.svc.VerifySignature(acc.Token, signature, timestamp, nonce) {
		ctx.String(http.StatusForbidden, "signature verification failed")
		return
	}

	body, err := readWechatBody(ctx.Request.Body)
	if err != nil {
		ctx.String(http.StatusBadRequest, "read body failed")
		return
	}

	msg, err := c.svc.ParseIncomingMessage(body)
	if err != nil {
		ctx.String(http.StatusBadRequest, "parse xml failed")
		return
	}

	if err := c.svc.SaveIncomingMessage(reqCtx, uint(accountID), msg, body); err != nil {
		// 入站消息落库失败不能吞：微信侧不重推，丢了就永久丢
		logger.Errorf("wechat: SaveIncomingMessage 失败 account=%d msg_type=%s: %v", accountID, msg.MsgType, err)
	}

	ctx.String(http.StatusOK, c.svc.BuildEmptyReply())

	go c.handleIncomingMessage(reqCtx, uint(accountID), msg)
}

func (c *WechatController) handleIncomingMessage(base context.Context, accountID uint, msg *service.WechatIncomingMessage) {

	// BackgroundWithTenant：请求 ctx 会随响应返回被取消，而异步体要继续写库；
	// 但它带着本条回调的租户归属，必须留住——丢掉就等于在 on 模式下用无绑定 ctx
	// 写租户表，隔离 callback 拒掉，消息进了库却永远进不了收件箱（只在日志里留一行 Warn）。
	ctx, cancel := context.WithTimeout(tenant.BackgroundWithTenant(base), 30*time.Second)
	defer cancel()

	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("[Wechat] handleIncomingMessage panic recovered: account=%d from=%s panic=%v\n%s",
				accountID, msg.FromUserName, r, debug.Stack())
		}
	}()

	msgType, content, mediaExtra, ok := msg.NormalizeInbound()
	if !ok {
		// 事件推送（subscribe/unsubscribe/CLICK/SCAN…）与解不出正文的畸形消息不进收件箱：
		// 落一行空 content 的 hub 既污染会话，又会驱动一次注定没有输入的 AI 回复（抖音 N-07 同源）。
		logger.Infof("[Wechat] 非消息推送已跳过: account=%d from=%s type=%s event=%s",
			accountID, msg.FromUserName, msg.MsgType, msg.Event)
		return
	}

	if c.ingressSvc != nil {
		event := &model.MessageEvent{
			Channel:        "wechat",
			ConversationID: fmt.Sprintf("wechat:%d:%s", accountID, msg.FromUserName),
			SessionID:      fmt.Sprintf("wechat:%d:%s", accountID, msg.FromUserName),
			EventID:        msg.DedupKey(accountID),
			SenderType:     model.SenderTypeCustomer,
			SenderID:       msg.FromUserName,
			SenderName:     "",
			MsgType:        msgType,
			Content:        content,
			Timestamp:      time.Unix(msg.CreateTime, 0),
			Extra: map[string]any{
				"account_id": fmt.Sprintf("%d", accountID),
				"to_user":    msg.ToUserName,
				// 官方 MsgId 是这条消息的稳定主键：中台据此跳过内容窗口去重，并参与出站回环识别。
				"channel_msg_id": msg.MsgID,
			},
		}
		for k, v := range mediaExtra {
			event.Extra[k] = v
		}

		if _, err := c.ingressSvc.HandleIngressMessage(ctx, event); err != nil {
			logger.Errorf("[Wechat] Ingress 处理失败: %v", err)
		} else if msg.MediaID != "" {
			// M-01：用户来信的 MediaId 是临时素材（官方只承诺上传侧 3 天，来信侧未公布时限），
			// 当场换成长期 URL 才留得住；pic_url/media_id 已在 Extra 里留痕，转存失败不丢引用。
			c.svc.PersistInboundMediaAsync(ctx, accountID, event.EventID, msg.MediaID)
		}
	}

	logger.Infof("[Wechat] 收到消息: account=%d from=%s type=%s content=%s",
		accountID, msg.FromUserName, msgType, content)
}
