package controller

import (
	"net/http"

	"hivemtk-user/internal/pkg/featureflag"

	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// BridgeCapabilitiesController 桥接通道能力查询端点。
//
// 原 router.go 中 bridgeWS.GET("/bridge/capabilities", ...) 的内联 handler
// 抽出至此 controller，符合五层架构——router 仅做 URL → controller 映射。
//
// 字段：
//   - sseEnabled: 来自 featureflag（FF_ENABLE_SSE_BRIDGE），由 feature flag 服务管理
//   - pollIntervalMs: 前端长轮询兜底间隔（与原硬编码 1500 一致）
//   - sseHeartbeatMs: SSE 心跳间隔（与原硬编码 15000 一致）
//
// 当前 controller 无 service/DB 依赖——纯配置读取。保留 struct 形态便于将来
// 注入 feature flag service / 配置中心（替换硬编码常量）。
type BridgeCapabilitiesController struct{}

// NewBridgeCapabilitiesController 创建桥接能力 controller。
func NewBridgeCapabilitiesController() *BridgeCapabilitiesController {
	return &BridgeCapabilitiesController{}
}

// @Summary      查询桥接通道能力清单（向后兼容形状）
// @Tags      bridge-capabilities
// @Router    /api/bridge/capabilities [get]
// GetCapabilities GET /api/bridge/capabilities。
//
// 响应体保持向后兼容：
//
//	{
//	  "sse_enabled":      true|false,
//	  "poll_interval_ms": 1500,
//	  "sse_heartbeat_ms": 15000
//	}
//
// 用 response.Envelope 直写裸 schema 而非 response.Success：此端点被前端桥接模块调用，
// 期望稳定的 schema（key 名 + 类型不变），不应受 response 包装层
// locale/i18n 影响（与平台约定一致）。
func (c *BridgeCapabilitiesController) GetCapabilities(ctx *gin.Context) {
	response.Envelope(ctx, http.StatusOK, gin.H{
		"sse_enabled":      featureflag.Get(featureflag.FF_ENABLE_SSE_BRIDGE).Bool(),
		"poll_interval_ms": 1500,
		"sse_heartbeat_ms": 15000,
	})
}
