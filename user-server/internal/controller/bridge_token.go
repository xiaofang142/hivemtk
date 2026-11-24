package controller

import (
	"errors"
	"net/http"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// BridgeTokenController 桥接通道凭证管理（v3 BRIDGE_TOKEN_PROTOCOL 阶段一落地）
//
// GET  /api/bridge/token/status  —— 查询配置状态，并回显当前明文（便于管理员再次复制）
// POST /api/bridge/token/reset   —— 轮换：旧值转 PREV，生成新值返回明文
//
// 业务逻辑在 service.BridgeTokenService（controller 不直连 repository）。
type BridgeTokenController struct {
	svc *service.BridgeTokenService
}

func NewBridgeTokenController() *BridgeTokenController {
	return &BridgeTokenController{svc: service.NewBridgeTokenService()}
}

// GetBridgeTokenStatus godoc
// @Summary      桥接凭证状态
// @Description  返回主/灰度 token 是否已配置，并回显当前明文（便于管理员再次复制）
// @Tags         Bridge
// @Security     BearerAuth
// @Success      200 {object} response.Response
// @Router       /api/bridge/token/status [get]
func (c *BridgeTokenController) GetStatus(ctx *gin.Context) {
	response.Success(ctx, c.svc.Status(ctx.Request.Context()), "ok")
}

// ResetBridgeToken godoc
// @Summary      轮换桥接凭证
// @Description  旧 token 转入灰度位（PREV），生成并返回新 token 明文
// @Tags         Bridge
// @Security     BearerAuth
// @Success      200 {object} response.Response
// @Router       /api/bridge/token/reset [post]
func (c *BridgeTokenController) ResetBridgeToken(ctx *gin.Context) {
	newTok, err := c.svc.Reset(ctx.Request.Context())
	if err != nil {
		if errors.Is(err, service.ErrLegacyBridgeTokenRetired) {
			response.Error(ctx, http.StatusGone, service.ErrLegacyBridgeTokenRetired.Error())
			return
		}
		response.Error(ctx, http.StatusInternalServerError, "凭证操作失败")
		return
	}
	response.Success(ctx, gin.H{"token": newTok}, "轮换成功")
}
