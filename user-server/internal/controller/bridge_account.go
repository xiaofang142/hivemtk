package controller

import (
	"errors"
	"net/http"

	"hivemtk-user/internal/bridge"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// BridgeAccountController 桥接账号管理（抖音/小红书/TikTok 网页私信）
//
//	GET  /api/bridge/accounts  —— 列出当前用户的桥接账号（含在线状态）
//	POST /api/bridge/send      —— 人工座席经桥接代发消息
//
// 业务判定（渠道归一、归属、租户钉死、在线门、投递编排）在 bridge 域服务，
// 本层只做参数绑定与状态码映射。
type BridgeAccountController struct{}

func NewBridgeAccountController() *BridgeAccountController {
	return &BridgeAccountController{}
}

// RegisterRoutes 注册桥接账号相关路由（在已鉴权的 auth 组下调用）
func (ctrl *BridgeAccountController) RegisterRoutes(auth *gin.RouterGroup) {
	auth.GET("/bridge/accounts", ctrl.List)
	auth.POST("/bridge/send", ctrl.SendManual)
}

// @Summary      列出当前用户的全部桥接账号
// @Tags      bridge-account
// @Router    /api/bridge/accounts [get]
// List 列出当前用户的全部桥接账号
func (ctrl *BridgeAccountController) List(c *gin.Context) {
	uidAny, _ := c.Get("user_id")
	uid, _ := uidAny.(uint)
	views, err := bridge.ListAccounts(c.Request.Context(), uid)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, bridge.ErrBridgeAccountRepoNotReady) {
			status = http.StatusServiceUnavailable
		}
		response.Error(c, status, err.Error())
		return
	}
	response.Success(c, gin.H{"accounts": views}, "ok")
}

type bridgeSendReq struct {
	Channel        string `json:"channel"`
	AccountID      string `json:"account_id"`
	ConversationID string `json:"conversation_id"`
	Content        string `json:"content"`
}

// @Summary      桥接账号手动发送消息
// @Tags      bridge-account
// @Router    /api/bridge/send [post]
func (ctrl *BridgeAccountController) SendManual(c *gin.Context) {
	var req bridgeSendReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	channel := bridge.NormalizeBridgeChannel(req.Channel)
	if !bridge.IsBridgeChannel(channel) || req.AccountID == "" || req.ConversationID == "" || req.Content == "" {
		response.Error(c, http.StatusBadRequest, "invalid params: channel/account_id/conversation_id/content required")
		return
	}
	uidAny, _ := c.Get("user_id")
	uid, _ := uidAny.(uint)
	if err := bridge.SendManualMessage(c.Request.Context(), uid, channel, req.AccountID, req.ConversationID, req.Content); err != nil {
		switch {
		case errors.Is(err, bridge.ErrBridgeAccountNotOwned):
			response.Error(c, http.StatusForbidden, err.Error())
		case errors.Is(err, bridge.ErrBridgeAccountOffline):
			response.Error(c, http.StatusConflict, err.Error())
		case errors.Is(err, bridge.ErrBridgeAccountRepoNotReady):
			response.Error(c, http.StatusServiceUnavailable, err.Error())
		default:
			response.ErrorFromDB(c, err, "操作失败，请稍后重试")
		}
		return
	}
	response.Success(c, gin.H{"ok": true}, "ok")
}
