package controller

import (
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"hivemtk-user/internal/bridge"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

type BridgeOutboxWakeController struct {
	broker *service.BridgeOutboxWakeBroker
}

func NewBridgeOutboxWakeController(broker *service.BridgeOutboxWakeBroker) *BridgeOutboxWakeController {
	return &BridgeOutboxWakeController{broker: broker}
}

func (c *BridgeOutboxWakeController) Stream(ctx *gin.Context) {
	if c == nil || c.broker == nil {
		response.Envelope(ctx, http.StatusServiceUnavailable, gin.H{"status": "error"})
		return
	}
	channel := bridge.NormalizeBridgeChannel(ctx.Query("channel"))
	accountID := ctx.Query("account_id")
	invalidAccount := accountID == "" || accountID == "default" || len(accountID) > 100 || strings.TrimSpace(accountID) != accountID
	if !bridge.IsBridgeChannel(channel) || invalidAccount || strings.IndexFunc(accountID, unicode.IsControl) >= 0 {
		response.Envelope(ctx, http.StatusBadRequest, gin.H{"status": "error", "message": "bridge account scope required"})
		return
	}
	boundCtx, err := bridge.BridgeTenantForRequest(ctx.Request, channel, accountID)
	if err != nil {
		response.Envelope(ctx, http.StatusBadRequest, gin.H{"status": "error", "message": "bridge tenant scope unavailable"})
		return
	}
	if _, bound := tenant.FromContext(boundCtx); !bound && !tenant.IsMultiTenant() {
		boundCtx = tenant.ContextWithDefault(boundCtx)
	}
	err = c.broker.Stream(boundCtx, channel, accountID, ctx.Writer, 15*time.Second, 5*time.Minute)
	if err != nil && !ctx.Writer.Written() {
		response.Envelope(ctx, http.StatusServiceUnavailable, gin.H{"status": "error", "message": "outbox wake unavailable"})
	}
}
