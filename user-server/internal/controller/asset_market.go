package controller

import (
	"encoding/json"
	"errors"
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

// MarketDetail GET /api/v1/asset-market/detail/:asset_id
func (h *AssetMarketController) MarketDetail(c *gin.Context) {
	detail, err := h.marketSvc.GetMarketAssetDetail(c.Request.Context(), c.Param("asset_id"))
	if err != nil {
		assetFail(c, err)
		return
	}
	assetOK(c, detail)
}

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

// DeleteLocal DELETE /api/v1/local-assets/:id
func (h *AssetMarketController) DeleteLocal(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := h.localSvc.SoftDelete(c.Request.Context(), id); err != nil {
		assetFail(c, err)
		return
	}
	assetOK(c, gin.H{"message": "删除成功"})
}

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

// MyPurchases GET /api/v1/asset-market/my-purchases
func (h *AssetMarketController) MyPurchases(c *gin.Context) {
	list, err := h.marketSvc.MyPurchases(c.Request.Context())
	if err != nil {
		assetFail(c, err)
		return
	}
	assetOK(c, list)
}

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
