package controller

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

type MerchantConnectionController struct {
	svc service.MerchantConnectionService
}

func NewMerchantConnectionController(svc service.MerchantConnectionService) *MerchantConnectionController {
	return &MerchantConnectionController{svc: svc}
}

// Status 返回 200 的安全摘要，非法参数 400，缺少租户 403，仓储故障 503。
// @Router /api/v1/integrations/merchant-connections [get]
func (c *MerchantConnectionController) Status(ctx *gin.Context) {
	merchantConnectionResponseHeaders(ctx)
	out, err := c.svc.Status(ctx.Request.Context(), dto.MerchantConnectionTarget{
		Provider: ctx.Query("provider"), ShopID: ctx.Query("shop_id"),
	})
	if err != nil {
		merchantConnectionError(ctx, err)
		return
	}
	response.Success(ctx, out, "ok")
}

// Start 返回 200 授权 URL。适配器未就绪返回 503，非法参数 400，缺少租户 403。
// @Router /api/v1/integrations/merchant-connections/start [post]
func (c *MerchantConnectionController) Start(ctx *gin.Context) {
	merchantConnectionResponseHeaders(ctx)
	var input dto.MerchantConnectionTarget
	if !bindMerchantConnection(ctx, &input) {
		return
	}
	out, err := c.svc.Start(ctx.Request.Context(), currentUID(ctx), input)
	if err != nil {
		merchantConnectionError(ctx, err)
		return
	}
	response.Success(ctx, out, "ok")
}

// Complete 返回 200 连接摘要，过期或重放返回 409，交换失败 502。
// @Router /api/v1/integrations/merchant-connections/complete [post]
func (c *MerchantConnectionController) Complete(ctx *gin.Context) {
	merchantConnectionResponseHeaders(ctx)
	var input dto.MerchantConnectionCompleteRequest
	if !bindMerchantConnection(ctx, &input) {
		return
	}
	out, err := c.svc.Complete(ctx.Request.Context(), currentUID(ctx), input)
	if err != nil {
		merchantConnectionError(ctx, err)
		return
	}
	response.Success(ctx, out, "ok")
}

// Refresh 续期指定凭证代次，代次冲突返回 409。
// @Router /api/v1/integrations/merchant-connections/refresh [post]
func (c *MerchantConnectionController) Refresh(ctx *gin.Context) {
	merchantConnectionResponseHeaders(ctx)
	var input dto.MerchantConnectionRefreshRequest
	if !bindMerchantConnection(ctx, &input) {
		return
	}
	out, err := c.svc.Refresh(ctx.Request.Context(), currentUID(ctx), input)
	if err != nil {
		merchantConnectionError(ctx, err)
		return
	}
	response.Success(ctx, out, "ok")
}

// Revoke 停止本地凭证使用，提供方吊销未确认。
// @Router /api/v1/integrations/merchant-connections/revoke [post]
func (c *MerchantConnectionController) Revoke(ctx *gin.Context) {
	merchantConnectionResponseHeaders(ctx)
	var input dto.MerchantConnectionTarget
	if !bindMerchantConnection(ctx, &input) {
		return
	}
	out, err := c.svc.Revoke(ctx.Request.Context(), currentUID(ctx), input)
	if err != nil {
		merchantConnectionError(ctx, err)
		return
	}
	response.Success(ctx, out, "ok")
}

func merchantConnectionResponseHeaders(ctx *gin.Context) {
	ctx.Header("Cache-Control", "no-store")
	ctx.Header("Referrer-Policy", "no-referrer")
}

func bindMerchantConnection(ctx *gin.Context, input any) bool {
	mediaType, _, err := mime.ParseMediaType(ctx.Request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || ctx.Request.URL.RawQuery != "" {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams)
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 8*1024))
	decoder.DisallowUnknownFields()
	if decoder.Decode(input) != nil || decoder.Decode(new(any)) != io.EOF {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams)
		return false
	}
	return true
}

func merchantConnectionError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrMerchantConnectionTenant):
		response.Error(ctx, http.StatusForbidden, response.ErrPermissionDenied)
	case errors.Is(err, service.ErrMerchantConnectionInput):
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams)
	case errors.Is(err, service.ErrMerchantConnectionState):
		response.Error(ctx, http.StatusConflict, response.ErrInvalidParams)
	case errors.Is(err, service.ErrMerchantProviderUnavailable):
		response.Error(ctx, http.StatusServiceUnavailable, response.ErrBusinessError, gin.H{"reason_code": "pending_live_validation"})
	case errors.Is(err, service.ErrMerchantConnectionExchange):
		response.Error(ctx, http.StatusBadGateway, response.ErrBusinessError)
	default:
		response.Error(ctx, http.StatusServiceUnavailable, response.ErrBusinessError)
	}
}
