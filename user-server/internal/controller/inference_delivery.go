package controller

import (
	"context"
	"encoding/hex"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"hivemtk-user/internal/aiagent/agent/delivery"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

type InferenceDeliveryController struct {
	svc service.InferenceDeliveryService
}

func NewInferenceDeliveryController(svc service.InferenceDeliveryService) *InferenceDeliveryController {
	return &InferenceDeliveryController{svc: svc}
}

// RegisterRoutes 仅挂在已有鉴权及租户中间件保护的 /api 组内。
func (c *InferenceDeliveryController) RegisterRoutes(auth *gin.RouterGroup) {
	g := auth.Group("/v1/agent/inference/deliveries")
	g.GET("/:key", c.Get)
	g.POST("/:key/retry", c.Retry)
}

// Get 查询持久意向与外部受理/确认，HTTP 200 不表示已确认交付。
// @Summary 查询推理交付状态
// @Tags 智能体推理
// @Produce json
// @Param key path string true "交付幂等键"
// @Success 200 {object} delivery.Record
// @Failure 400,403,404,503 {object} map[string]interface{}
// @Router /api/v1/agent/inference/deliveries/{key} [get]
func (c *InferenceDeliveryController) Get(ctx *gin.Context) { c.readOrRetry(ctx, false) }

// Retry 重新授权并查证回执；未确认返回 202，确认返回 200，确定拒绝返回 409。
// @Summary 查证并重试推理交付
// @Tags 智能体推理
// @Produce json
// @Param key path string true "交付幂等键"
// @Success 200,202 {object} delivery.Record
// @Failure 400,403,404,409,503 {object} map[string]interface{}
// @Router /api/v1/agent/inference/deliveries/{key}/retry [post]
func (c *InferenceDeliveryController) Retry(ctx *gin.Context) { c.readOrRetry(ctx, true) }

func (c *InferenceDeliveryController) readOrRetry(ctx *gin.Context, retry bool) {
	key := ctx.Param("key")
	decoded, err := hex.DecodeString(key)
	if err != nil || len(key) != 64 || len(decoded) != 32 {
		response.Error(ctx, 400, response.ErrInferenceDeliveryKey)
		return
	}
	if c.svc == nil {
		response.Error(ctx, 503, response.ErrInferenceDeliveryUnavailable)
		return
	}
	requestCtx, cancel := context.WithTimeout(ctx.Request.Context(), 10*time.Second)
	defer cancel()
	var record *delivery.Record
	if retry {
		record, err = c.svc.Retry(requestCtx, key)
	} else {
		record, err = c.svc.Get(requestCtx, key)
	}
	if err != nil {
		switch {
		case errors.Is(err, delivery.ErrTenant):
			response.Error(ctx, 403, response.ErrInferenceDeliveryTenant)
		case errors.Is(err, delivery.ErrNotFound):
			response.Error(ctx, 404, response.ErrInferenceDeliveryNotFound)
		case errors.Is(err, delivery.ErrConflict):
			response.Error(ctx, 409, response.ErrInferenceDeliveryConflict)
		default:
			logger.Ctx(requestCtx).Warn().Err(err).Msg("[delivery_api] 账本查询或重试失败")
			response.Error(ctx, 503, response.ErrInferenceDeliveryUnavailable, record)
		}
		return
	}
	if record == nil {
		response.Error(ctx, 503, response.ErrInferenceDeliveryUnavailable)
		return
	}
	if retry && record.State == delivery.Rejected {
		response.Error(ctx, 409, response.ErrInferenceDeliveryRejected, record)
		return
	}
	if retry && record.State != delivery.Confirmed {
		response.Accepted(ctx, record, response.MsgInferenceDeliveryPending)
		return
	}
	response.Success(ctx, record, "ok")
}
