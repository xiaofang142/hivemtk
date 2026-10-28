package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"hivemtk-user/internal/config"
	bizerr "hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// AssetMarketController 资产市场控制器
type AssetMarketController struct {
	localSvc  *service.LocalAssetService
	marketSvc *service.AssetMarketService
	agentSvc  *service.AIAgentService
}

// NewAssetMarketController 构造（service 由 router 注入）
func NewAssetMarketController(localSvc *service.LocalAssetService, marketSvc *service.AssetMarketService, agentSvc *service.AIAgentService) *AssetMarketController {
	return &AssetMarketController{
		localSvc:  localSvc,
		marketSvc: marketSvc,
		agentSvc:  agentSvc,
	}
}

func assetFail(c *gin.Context, err error) {
	var be *bizerr.BizError
	if errors.As(err, &be) {
		response.ErrorWithBusinessCode(c, be.Code, be.Message, gin.H{})
		return
	}
	response.ErrorWithBusinessCode(c, bizerr.CodeInternal, err.Error(), gin.H{})
}

func assetOK(c *gin.Context, data interface{}) {
	response.Success(c, data, "ok")
}

// rejectPlatformDisabled 平台集成关闭态下把"要打到平台侧的写操作"直接挡在门口：
// 403 + 一句照着做就能开通的话。不走 service，是因为那条路只会得到
// "平台未配置"的失败与一条 Error 日志——那是把"没启用"伪装成"坏了"。
// 返回 true 表示已经写好响应，调用方立即 return。
func rejectPlatformDisabled(c *gin.Context) bool {
	if config.PlatformEnabled() {
		return false
	}
	response.Error(c, http.StatusForbidden, "平台集成未启用（设 PLATFORM_ENABLED=true 并启动本地 platform-server 后可用）")
	return true
}

// @Summary      查询资产市场列表
// @Tags      asset-market
// @Router    /api/v1/asset-market/list [get]
// ListMarket GET /api/v1/asset-market/list
func (h *AssetMarketController) ListMarket(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	list, total, err := h.marketSvc.ListMarketAssets(c.Request.Context(),
		c.Query("asset_type"), c.Query("industry"), page, size)
	if err != nil {
		response.SuccessWithList(c, []map[string]any{}, 0)
		return
	}
	response.SuccessWithList(c, list, total)
}

// @Summary      按资产 ID 查询市场详情
// @Tags      asset-market
// @Router    /api/v1/asset-market/detail/{asset_id} [get]
// MarketDetail GET /api/v1/asset-market/detail/:asset_id
func (h *AssetMarketController) MarketDetail(c *gin.Context) {
	assetID := c.Param("asset_id")
	detail, err := h.marketSvc.GetMarketAssetDetail(c.Request.Context(), assetID)
	// 平台未接入时 service 可能返回 (空 map, nil)：空壳同样回落本地，否则页面只能报不存在。
	if err != nil || len(detail) == 0 {
		// 私域独立部署（未接平台）时市场接口不可用，但本机「我的资产」里可能已有
		// 同 asset_id 的资产。此时直接报平台不可用会让 /asset-market/detail/:id
		// 永远打不开；回落本地资产既保持页面可用，也不虚构平台数据。
		if h.localSvc != nil {
			if la, data, lerr := h.localSvc.LoadOne(c.Request.Context(), assetID); lerr == nil && la != nil {
				assetOK(c, gin.H{
					"asset_id":       la.AssetID,
					"name":           la.Name,
					"asset_type":     la.AssetType,
					"industry":       la.Industry,
					"latest_version": la.Version,
					"description":    "本地已同步资产（当前部署未接入资产市场）",
					"use_count":      la.UseCount,
					"source":         "local",
					"data_preview":   json.RawMessage(data),
				})
				return
			}
		}
		// err 可能为 nil（平台未接入返回空壳 + nil），不能用 err.Error()。
		assetFail(c, fmt.Errorf("资产不存在或不可用"))
		return
	}
	assetOK(c, detail)
}

// @Summary      参数错误
// @Tags      asset-market
// @Router    /api/v1/asset-market/purchase [post]
// Purchase POST /api/v1/asset-market/purchase
func (h *AssetMarketController) Purchase(c *gin.Context) {
	if rejectPlatformDisabled(c) {
		return
	}

	var body dto.PurchaseRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误")
		return
	}
	if err := h.localSvc.PurchaseAndSync(c.Request.Context(), body.AssetID); err != nil {
		assetFail(c, err)
		return
	}
	assetOK(c, gin.H{"message": "购买并同步成功"})
}

// @Summary      参数错误
// @Tags      asset-market
// @Router    /api/v1/asset-market/sync [post]
// Sync POST /api/v1/asset-market/sync
func (h *AssetMarketController) Sync(c *gin.Context) {
	if rejectPlatformDisabled(c) {
		return
	}

	var body dto.SyncRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误")
		return
	}
	if err := h.localSvc.SyncFromPlatform(c.Request.Context(), body.AssetID); err != nil {
		assetFail(c, err)
		return
	}
	assetOK(c, gin.H{"message": "同步成功"})
}

// @Summary      查询本地资产列表
// @Tags      asset-market
// @Router    /api/v1/local-assets [get]
// ListLocal GET /api/v1/local-assets
func (h *AssetMarketController) ListLocal(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	list, total, err := h.localSvc.List(c.Request.Context(), service.LocalAssetFilter{
		AssetType: c.Query("asset_type"),
		Industry:  c.Query("industry"),
		Source:    c.Query("source"),
		Keyword:   c.Query("keyword"),
		Page:      page,
		Size:      size,
	})
	if err != nil {
		assetFail(c, err)
		return
	}
	response.SuccessWithList(c, list, total)
}

// @Summary      按 ID 查询本地资产
// @Tags      asset-market
// @Router    /api/v1/local-assets/{id} [get]
// GetLocal GET /api/v1/local-assets/:id
func (h *AssetMarketController) GetLocal(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	la, data, err := h.localSvc.Get(c.Request.Context(), id)
	if err != nil {
		assetFail(c, err)
		return
	}
	var parsed any
	if len(data) > 0 {
		_ = json.Unmarshal(data, &parsed)
	}
	assetOK(c, gin.H{"asset": la, "data": parsed})
}

// @Summary      JSON 解析失败
// @Tags      asset-market
// @Router    /api/v1/local-assets [post]
// CreateLocal POST /api/v1/local-assets
func (h *AssetMarketController) CreateLocal(c *gin.Context) {
	body, _ := io.ReadAll(c.Request.Body)
	var in service.CreateAssetInput
	if err := json.Unmarshal(body, &in); err != nil {
		response.Error(c, http.StatusBadRequest, "JSON 解析失败")
		return
	}
	if len(in.Data) == 0 {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(body, &raw); err == nil {
			if d, ok := raw["data"]; ok {
				in.Data = d
			}
		}
	}
	la, err := h.localSvc.CreateManual(c.Request.Context(), &in)
	if err != nil {
		assetFail(c, err)
		return
	}
	assetOK(c, la)
}

// @Summary      JSON 解析失败
// @Tags      asset-market
// @Router    /api/v1/local-assets/{id} [put]
// UpdateLocal PUT /api/v1/local-assets/:id
func (h *AssetMarketController) UpdateLocal(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	body, _ := io.ReadAll(c.Request.Body)
	var in service.UpdateAssetInput
	if err := json.Unmarshal(body, &in); err != nil {
		response.Error(c, http.StatusBadRequest, "JSON 解析失败")
		return
	}
	if len(in.Data) == 0 {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(body, &raw); err == nil {
			if d, ok := raw["data"]; ok {
				in.Data = d
			}
		}
	}
	if err := h.localSvc.Update(c.Request.Context(), id, &in); err != nil {
		assetFail(c, err)
		return
	}
	assetOK(c, gin.H{"message": "更新成功"})
}

// @Summary      删除本地资产
// @Tags      asset-market
// @Router    /api/v1/local-assets/{id} [delete]
// DeleteLocal DELETE /api/v1/local-assets/:id
func (h *AssetMarketController) DeleteLocal(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := h.localSvc.SoftDelete(c.Request.Context(), id); err != nil {
		assetFail(c, err)
		return
	}
	assetOK(c, gin.H{"message": "删除成功"})
}

// @Summary      参数错误
// @Tags      asset-market
// @Router    /api/v1/local-assets/{id}/toggle-active [put]
// ToggleActive PUT /api/v1/local-assets/:id/toggle-active
func (h *AssetMarketController) ToggleActive(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var body dto.ToggleActiveRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误")
		return
	}
	if err := h.localSvc.ToggleActive(c.Request.Context(), id, body.Active); err != nil {
		assetFail(c, err)
		return
	}
	assetOK(c, gin.H{"message": "操作成功"})
}

// @Summary      查询本地资产同步日志
// @Tags      asset-market
// @Router    /api/v1/local-assets/sync-log [get]
// SyncLog GET /api/v1/local-assets/sync-log
func (h *AssetMarketController) SyncLog(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	list, err := h.localSvc.GetSyncLog(c.Request.Context(), c.Query("asset_id"), limit)
	if err != nil {
		assetFail(c, err)
		return
	}
	assetOK(c, list)
}

// @Summary      参数错误
// @Tags      asset-market
// @Router    /api/v1/asset-market/report-usage [post]
// ReportUsage POST /api/asset-market/report-usage
// 将本地累计使用次数回传平台（闭环：本地使用 → 平台统计）。
func (h *AssetMarketController) ReportUsage(c *gin.Context) {
	if rejectPlatformDisabled(c) {
		return
	}

	var body dto.ReportUsageRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误")
		return
	}
	if err := h.localSvc.ReportUsage(c.Request.Context(), body.AssetID); err != nil {
		assetFail(c, err)
		return
	}
	assetOK(c, gin.H{"message": "上报成功"})
}

// @Summary      查询我的资产购买记录
// @Tags      asset-market
// @Router    /api/v1/asset-market/my-purchases [get]
// MyPurchases GET /api/v1/asset-market/my-purchases
func (h *AssetMarketController) MyPurchases(c *gin.Context) {
	list, err := h.marketSvc.MyPurchases(c.Request.Context())
	if err != nil {
		assetFail(c, err)
		return
	}
	assetOK(c, list)
}

// @Summary      参数错误
// @Tags      asset-market
// @Router    /api/v1/asset-market/bind [post]
// BindToAgent POST /api/v1/asset-market/bind
// 将平台同步下来的资产（local_assets.AssetID，即平台 AssetCode）绑定到智能体，
// 使其成为该智能体的资产包（运行期 ResolveSystemPrompt 回退消费），闭合
// 「平台下发资产包 → 商户同步 → 绑定 → 运行时消费」全链路。
func (h *AssetMarketController) BindToAgent(c *gin.Context) {
	var body struct {
		AssetID string `json:"asset_id"`
		AgentID uint   `json:"agent_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误")
		return
	}
	if body.AssetID == "" || body.AgentID == 0 {
		response.Error(c, http.StatusBadRequest, "asset_id 与 agent_id 必填")
		return
	}
	if err := h.agentSvc.BindAssetBundle(c.Request.Context(), body.AgentID, body.AssetID); err != nil {
		assetFail(c, err)
		return
	}
	assetOK(c, gin.H{"message": "绑定成功"})
}
