package controller

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"hivemtk-user/internal/model"
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

func (c *WechatController) CreateAccount(ctx *gin.Context) {
	var req createWechatAccountReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误: "+err.Error())
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

// UpdateAccount 更新公众号账号
func (c *WechatController) UpdateAccount(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 ID")
		return
	}

	var req createWechatAccountReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误: "+err.Error())
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
		response.Error(ctx, http.StatusInternalServerError, "更新失败: "+err.Error())
		return
	}
	response.Success(ctx, nil, "更新成功")
}

// DeleteAccount 删除公众号账号
func (c *WechatController) DeleteAccount(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 ID")
		return
	}
	if err := c.svc.DeleteAccount(ctx.Request.Context(), uint(id)); err != nil {
		response.Error(ctx, http.StatusInternalServerError, "删除失败: "+err.Error())
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

func (c *WechatController) SendMessage(ctx *gin.Context) {
	var req sendWechatMsgReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误: "+err.Error())
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

	acc, err := c.svc.GetAccount(ctx.Request.Context(), uint(accountID))
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

	acc, err := c.svc.GetAccount(ctx.Request.Context(), uint(accountID))
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

	body, err := io.ReadAll(ctx.Request.Body)
	if err != nil {
		ctx.String(http.StatusBadRequest, "read body failed")
		return
	}

	msg, err := c.svc.ParseIncomingMessage(body)
	if err != nil {
		ctx.String(http.StatusBadRequest, "parse xml failed")
		return
	}

	if err := c.svc.SaveIncomingMessage(ctx.Request.Context(), uint(accountID), msg, body); err != nil {
		// 入站消息落库失败不能吞：微信侧不重推，丢了就永久丢
		logger.Errorf("wechat: SaveIncomingMessage 失败 account=%d msg_type=%s: %v", accountID, msg.MsgType, err)
	}

	ctx.String(http.StatusOK, c.svc.BuildEmptyReply())

	go c.handleIncomingMessage(uint(accountID), msg)
}

func (c *WechatController) handleIncomingMessage(accountID uint, msg *service.WechatIncomingMessage) {

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("[Wechat] handleIncomingMessage panic recovered: account=%d from=%s panic=%v",
				accountID, msg.FromUserName, r)
		}
	}()

	if c.ingressSvc != nil {
		event := &model.MessageEvent{
			Channel:        "wechat",
			ConversationID: fmt.Sprintf("wechat:%d:%s", accountID, msg.FromUserName),
			SessionID:      fmt.Sprintf("wechat:%d:%s", accountID, msg.FromUserName),
			EventID:        msg.MsgID,
			SenderType:     model.SenderTypeCustomer,
			SenderID:       msg.FromUserName,
			SenderName:     "",
			MsgType:        msg.MsgType,
			Content:        msg.Content,
			Timestamp:      time.Unix(msg.CreateTime, 0),
			Extra: map[string]any{
				"account_id": fmt.Sprintf("%d", accountID),
				"to_user":    msg.ToUserName,
			},
		}

		if _, err := c.ingressSvc.HandleIngressMessage(ctx, event); err != nil {
			logger.Errorf("[Wechat] Ingress 处理失败: %v", err)
		}
	}

	logger.Infof("[Wechat] 收到消息: account=%d from=%s type=%s content=%s",
		accountID, msg.FromUserName, msg.MsgType, msg.Content)
}
