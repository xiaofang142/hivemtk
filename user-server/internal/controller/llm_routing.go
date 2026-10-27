package controller

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/aiagent/llm"
	bizerr "hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

// LLMRoutingController LLM 路由控制器
type LLMRoutingController struct {
	routingService *service.LLMRoutingService
	failoverSvc    *service.LLMFailoverService
}

// NewLLMRoutingController 创建 LLM 路由控制器
func NewLLMRoutingController(routingService *service.LLMRoutingService) *LLMRoutingController {
	return &LLMRoutingController{routingService: routingService}
}

// SetFailover 注入熔断器中转服务（v3.7.0 补：Health 端点用于展示 circuit_open / last_error）
func (c *LLMRoutingController) SetFailover(f *service.LLMFailoverService) {
	c.failoverSvc = f
}

// @Summary      列出所有 LLM provider
// @Tags      l-l-m-routing
// @Router    /api/llm/models [get]
// ListModels 列出所有 LLM provider
//
// GET /api/llm/models
// 返回：[]LLMProviderInfo
func (c *LLMRoutingController) ListModels(ctx *gin.Context) {
	models := c.routingService.ListModels(ctx.Request.Context())
	ctx.JSON(http.StatusOK, gin.H{"code": 0, "data": models})
}

// @Summary      注册新 provider
// @Tags      l-l-m-routing
// @Router    /api/llm/models [post]
// CreateModel 注册新 provider
//
// POST /api/llm/models
// Body: LLMProviderInfo
func (c *LLMRoutingController) CreateModel(ctx *gin.Context) {
	var info service.LLMProviderInfo
	if err := ctx.ShouldBindJSON(&info); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid body: " + err.Error()})
		return
	}
	keyProvided := info.APIKey != ""
	if err := c.routingService.CreateModel(ctx.Request.Context(), info); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
		return
	}
	// 不回显提交的密钥：响应体会进访问日志/APM，原样回写等于把密钥二次落盘。
	// 改用 api_key_set 表达「有没有配」，与列表接口同一口径。
	info.APIKey = ""
	info.APIKeySet = keyProvided
	ctx.JSON(http.StatusOK, gin.H{"code": 0, "data": info})
}

// @Summary      获取单个 provider
// @Tags      l-l-m-routing
// @Router    /api/llm/models/{name} [get]
// GetModel 获取单个 provider
//
// GET /api/llm/models/:name
// :name 可为 provider name（如 "deepseek"）或 model name（如 "gpt-4o"）
func (c *LLMRoutingController) GetModel(ctx *gin.Context) {
	name := ctx.Param("name")
	if name == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "name is required"})
		return
	}
	models := c.routingService.ListModels(ctx.Request.Context())
	for _, m := range models {
		if m.Name == name || m.Model == name {
			ctx.JSON(http.StatusOK, gin.H{"code": 0, "data": m})
			return
		}
	}
	ctx.JSON(http.StatusOK, gin.H{"code": 0, "data": nil, "message": "model not found"})
}

// @Summary      更新 provider
// @Tags      l-l-m-routing
// @Router    /api/llm/models/{name} [put]
// UpdateModel 更新 provider
//
// PUT /api/llm/models/:name
// Body: LLMProviderInfo（name 字段被忽略，路径 :name 优先）
func (c *LLMRoutingController) UpdateModel(ctx *gin.Context) {
	name := ctx.Param("name")
	if name == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "name is required"})
		return
	}
	var info service.LLMProviderInfo
	if err := ctx.ShouldBindJSON(&info); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid body: " + err.Error()})
		return
	}
	if err := c.routingService.UpdateModel(ctx.Request.Context(), name, info); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"code": 0, "data": name})
}

// @Summary      删除 provider
// @Tags      l-l-m-routing
// @Router    /api/llm/models/{name} [delete]
// DeleteModel 删除 provider
//
// DELETE /api/llm/models/:name
// :name 可为 provider name 或 model name；先解析为 provider name 再执行删除
func (c *LLMRoutingController) DeleteModel(ctx *gin.Context) {
	name := ctx.Param("name")
	if name == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "name is required"})
		return
	}
	resolved := c.routingService.ResolveProviderName(name)
	if resolved == "" {
		ctx.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "model not found"})
		return
	}
	if err := c.routingService.DeleteModel(ctx.Request.Context(), resolved); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"code": 0, "data": resolved})
}

// @Summary      测试 provider 连通性（独立路径，不污染全局路由/告警/熔断）
// @Tags      l-l-m-routing
// @Router    /api/llm/models/{name}/test [post]
// TestModel 测试 provider 连通性（独立路径，不污染全局路由/告警/熔断）
//
// POST /api/llm/models/:name/test
// Body: { "prompt": "...", "timeout_seconds": 60 }
// :name 可为 provider name 或 model name；先解析为 provider name 再执行测试
func (c *LLMRoutingController) TestModel(ctx *gin.Context) {
	name := ctx.Param("name")
	if name == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "name is required"})
		return
	}
	resolved := c.routingService.ResolveProviderName(name)
	if resolved == "" {
		ctx.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "model not found"})
		return
	}
	var req service.TestModelRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		req = service.TestModelRequest{}
	}
	req.Provider = resolved
	result, err := c.routingService.TestModel(ctx.Request.Context(), req)
	if err != nil {
		// 余额不足单独映射：它不是"模型坏了"，处置动作是"去充值"。
		// 笼统回 500 会让运营以为模型配错了，而商户看到的是"测试失败"。
		if llm.IsBalanceInsufficient(err) {
			response.ErrorWithBusinessCode(ctx, bizerr.CodeLLMBalanceInsufficient,
				"平台模型余额不足，请购买 LLM 套餐后重试", gin.H{})
			return
		}
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"code": 0, "data": result})
}

// @Summary      列出所有场景路由
// @Tags      l-l-m-routing
// @Router    /api/llm/scene-routing [get]
// ListStrategies 列出所有场景路由
//
// GET /api/llm/strategies
// 别名：GET /api/llm/scene-routing（兼容旧前端）
// 返回：[]ScenarioRoute
func (c *LLMRoutingController) ListStrategies(ctx *gin.Context) {
	routes := c.routingService.ListStrategies(ctx.Request.Context())
	ctx.JSON(http.StatusOK, gin.H{"code": 0, "data": routes})
}

// @Summary      批量更新场景路由
// @Tags      l-l-m-routing
// @Router    /api/llm/strategies [post]
// UpdateStrategies 批量更新场景路由
//
// PUT /api/llm/strategies
// Body: { "routes": [...], "operator": "...", "trace_id": "...", "commit_msg": "..." }
func (c *LLMRoutingController) UpdateStrategies(ctx *gin.Context) {
	var req service.UpdateStrategiesRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid body: " + err.Error()})
		return
	}
	if err := c.routingService.UpdateStrategies(ctx.Request.Context(), req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
		return
	}
	routes := c.routingService.ListStrategies(ctx.Request.Context())
	ctx.JSON(http.StatusOK, gin.H{"code": 0, "data": routes})
}

// @Summary      兼容旧接口（PUT /api/llm/scene-routing）
// @Tags      l-l-m-routing
// @Router    /api/llm/scene-routing [post]
// UpdateSceneRouting 兼容旧接口（PUT /api/llm/scene-routing）
//
// Body 兼容：{ routes: [...] } 或 { scenario: "...", provider: "...", fallbacks: [...] }
func (c *LLMRoutingController) UpdateSceneRouting(ctx *gin.Context) {
	// 请求体只能读一次：先按批量形状解析，不匹配再按单条解析。
	// 修前这里连着调了两次 ShouldBindJSON —— 第一次已把 body 读空，第二次必然
	// `invalid body: EOF`，于是"单条场景路由"这条兼容入口从来就没成功过（实测）。
	raw, readErr := io.ReadAll(io.LimitReader(ctx.Request.Body, 1<<20))
	if readErr != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "read body: " + readErr.Error()})
		return
	}

	var req service.UpdateStrategiesRequest
	if err := json.Unmarshal(raw, &req); err == nil && len(req.Routes) > 0 {
		if err := c.routingService.UpdateStrategies(ctx.Request.Context(), req); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
			return
		}
		ctx.JSON(http.StatusOK, gin.H{"code": 0, "data": req.Routes})
		return
	}

	var single service.LLMScenarioRoute
	if err := json.Unmarshal(raw, &single); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid body: " + err.Error()})
		return
	}
	if err := c.routingService.UpdateSingleScenarioRoute(ctx.Request.Context(), single); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"code": 0, "data": single})
}

// @Summary      列出路由变更审计历史
// @Tags      l-l-m-routing
// @Router    /api/llm/audit [get]
// ListAuditHistory 列出路由变更审计历史
//
// GET /api/llm/audit?scenario=xxx&limit=50
func (c *LLMRoutingController) ListAuditHistory(ctx *gin.Context) {
	scenario := ctx.Query("scenario")
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "50"))
	rows, err := c.routingService.ListAuditHistory(ctx.Request.Context(), scenario, limit)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"code": 0, "data": rows})
}

// @Summary      进程内实时 provider 维度统计
// @Tags      l-l-m-routing
// @Router    /api/llm/stats [get]
// Stats 进程内实时 provider 维度统计
//
// GET /api/llm/stats
func (c *LLMRoutingController) Stats(ctx *gin.Context) {
	stats := c.routingService.Stats(ctx.Request.Context())
	ctx.JSON(http.StatusOK, gin.H{"code": 0, "data": stats})
}

// @Summary      跨进程历史统计（按 window）
// @Tags      l-l-m-routing
// @Router    /api/llm/usage [get]
// Usage 跨进程历史统计（按 window）
//
// GET /api/llm/usage?window=today|week|month|all
func (c *LLMRoutingController) Usage(ctx *gin.Context) {
	window := ctx.DefaultQuery("window", "all")
	summary, err := c.routingService.Usage(ctx.Request.Context(), window)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error(), "trace_id": logger.TraceIDFromContext(ctx.Request.Context())})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"code": 0, "data": summary})
}

// @Summary      用量 + 成本统计（前端 cost-stats tab 使用）
// @Tags      l-l-m-routing
// @Router    /api/llm/cost-stats [get]
// CostStats 用量 + 成本统计（前端 cost-stats tab 使用）
//
// GET /api/llm/cost-stats?window=...
// 复用 Usage 接口但返回前端期望的 {monthlyCost, byModel} 形态
func (c *LLMRoutingController) CostStats(ctx *gin.Context) {
	window := ctx.DefaultQuery("window", "month")
	summary, err := c.routingService.Usage(ctx.Request.Context(), window)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}
	out := gin.H{
		"window":         summary.WindowLabel,
		"monthly_cost":   summary.TotalCost,
		"total_tokens":   summary.TotalTokens,
		"total_calls":    summary.TotalCalls,
		"by_scenario":    summary.ByScenario,
		"by_provider":    summary.ByProvider,
		"by_model":       summary.ByProvider,
		"enabled_models": summary.EnabledModels,
		"active_models":  summary.ActiveModels,
	}
	ctx.JSON(http.StatusOK, gin.H{"code": 0, "data": out})
}

// @Summary      兜底配置查询（兼容旧前端）
// @Tags      l-l-m-routing
// @Router    /api/llm/fallback [get]
// FallbackConfig 兜底配置查询（兼容旧前端）
//
// GET /api/llm/fallback
// 返回当前所有场景路由的 fallback 列表（前端 fallback tab 使用）
func (c *LLMRoutingController) FallbackConfig(ctx *gin.Context) {
	routes := c.routingService.ListStrategies(ctx.Request.Context())
	out := make([]gin.H, 0, len(routes))
	for _, r := range routes {
		out = append(out, gin.H{
			"scenario":  r.Scenario,
			"provider":  r.Provider,
			"fallbacks": r.Fallbacks,
			"version":   r.Version,
		})
	}
	ctx.JSON(http.StatusOK, gin.H{"code": 0, "data": out})
}

// @Summary      场景列表（与 strategies 同义）
// @Tags      l-l-m-routing
// @Router    /api/llm/scenarios [get]
// ListScenarios 场景列表（与 strategies 同义）
//
// GET /api/llm/scenarios
func (c *LLMRoutingController) ListScenarios(ctx *gin.Context) {
	routes := c.routingService.ListStrategies(ctx.Request.Context())
	models := c.routingService.ListModels(ctx.Request.Context())
	modelOf := make(map[string]string, len(models))
	for _, m := range models {
		modelOf[m.Name] = m.Model
	}
	out := make([]gin.H, 0, len(routes))
	for _, r := range routes {
		out = append(out, gin.H{
			"scenario":  r.Scenario,
			"provider":  r.Provider,
			"model":     modelOf[r.Provider],
			"weight":    r.Weight,
			"fallbacks": r.Fallbacks,
			"version":   r.Version,
		})
	}
	ctx.JSON(http.StatusOK, gin.H{"code": 0, "data": out})
}

// @Summary      整体健康度
// @Tags      l-l-m-routing
// @Router    /api/llm/health [get]
// Health 整体健康度
//
// GET /api/llm/health
// 返回：{status, providers: [{name, enabled, healthy, circuit_open, error_count, last_error, ...}]}
//
// v3.7.0 补：集成 ProviderFailover 熔断器状态，前端看板可显示：
//   - circuit_open：当前是否处于熔断状态
//   - error_count：累计失败次数
//   - last_error：最近一次失败原因
func (c *LLMRoutingController) Health(ctx *gin.Context) {
	models := c.routingService.ListModels(ctx.Request.Context())
	providers := make([]gin.H, 0, len(models))
	healthyCount := 0
	for _, m := range models {
		healthy := m.Enabled && m.BaseURL != ""
		circuitOpen := false
		errorCount := 0
		lastError := ""
		if c.failoverSvc != nil && c.failoverSvc.Ready() {
			circuitOpen = c.failoverSvc.IsCircuitOpen(m.Name)
			health := c.failoverSvc.GetHealth(m.Name)
			if health != nil {
				errorCount = health.ConsecutiveFailures
				lastError = health.LastError
				if circuitOpen {
					healthy = false
				}
			}
		}
		if healthy {
			healthyCount++
		}
		providers = append(providers, gin.H{
			"name":         m.Name,
			"enabled":      m.Enabled,
			"healthy":      healthy,
			"avg_latency":  m.AvgLatencyMs,
			"quality":      m.QualityScore,
			"circuit_open": circuitOpen,
			"error_count":  errorCount,
			"last_error":   lastError,
			"vendor":       m.Vendor,
			"base_url":     m.BaseURL,
		})
	}
	status := "ok"
	if len(models) == 0 {
		status = "empty"
	} else if healthyCount == 0 {
		status = "down"
	}
	ctx.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"status":            status,
			"total_providers":   len(models),
			"healthy_providers": healthyCount,
			"providers":         providers,
		},
	})
}

// @Summary      场景聚合统计（Usage 别名）
// @Tags      l-l-m-routing
// @Router    /api/llm/scenario-stats [get]
// ScenarioStats 场景聚合统计（Usage 别名）
//
// GET /api/llm/scenario-stats?window=today|week|month|all
func (c *LLMRoutingController) ScenarioStats(ctx *gin.Context) {
	window := ctx.DefaultQuery("window", "month")
	summary, err := c.routingService.Usage(ctx.Request.Context(), window)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"window":       summary.WindowLabel,
			"by_scenario":  summary.ByScenario,
			"total_calls":  summary.TotalCalls,
			"total_tokens": summary.TotalTokens,
			"total_cost":   summary.TotalCost,
		},
	})
}

// @Summary      模型类型聚合（local / cloud）
// @Tags      l-l-m-routing
// @Router    /api/llm/model-type-stats [get]
// ModelTypeStats 模型类型聚合（local / cloud）
//
// GET /api/llm/model-type-stats?window=today|week|month|all
// 当前实现：从 in-memory stats + 场景聚合结果按 provider 推导。
// 后续可在 llm_routing_logs 表加 model_type 字段后改走 SQL 聚合。
func (c *LLMRoutingController) ModelTypeStats(ctx *gin.Context) {
	window := ctx.DefaultQuery("window", "month")
	summary, err := c.routingService.Usage(ctx.Request.Context(), window)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}
	models := c.routingService.ListModels(ctx.Request.Context())
	providerToType := make(map[string]string, len(models))
	for _, m := range models {
		isLocal := m.BaseURL == "" ||
			strings.Contains(m.BaseURL, "localhost") ||
			strings.Contains(m.BaseURL, "127.0.0.1") ||
			strings.Contains(strings.ToLower(m.Vendor), "self") ||
			strings.HasPrefix(m.Name, "default")
		if isLocal {
			providerToType[m.Name] = "local"
		} else {
			providerToType[m.Name] = "cloud"
		}
	}
	localCalls, cloudCalls := int64(0), int64(0)
	localTokens, cloudTokens := int64(0), int64(0)
	localCost, cloudCost := 0.0, 0.0
	for _, p := range summary.ByProvider {
		if providerToType[p.Provider] == "local" {
			localCalls += p.CallCount
			localTokens += p.TotalTokens
			localCost += p.TotalCost
		} else {
			cloudCalls += p.CallCount
			cloudTokens += p.TotalTokens
			cloudCost += p.TotalCost
		}
	}
	total := localCalls + cloudCalls
	selfSuf := 0.0
	if total > 0 {
		selfSuf = float64(localCalls) / float64(total)
	}
	ctx.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"window": window,
			"by_provider_type": []gin.H{
				{
					"model_type":   "local",
					"call_count":   localCalls,
					"total_tokens": localTokens,
					"total_cost":   localCost,
				},
				{
					"model_type":   "cloud",
					"call_count":   cloudCalls,
					"total_tokens": cloudTokens,
					"total_cost":   cloudCost,
				},
			},
			"self_sufficiency": selfSuf,
		},
	})
}

// @Summary      出域告警（"应该本地推理但出域到云端"）
// @Tags      l-l-m-routing
// @Router    /api/llm/egress-alerts [get]
// EgressAlerts 出域告警（"应该本地推理但出域到云端"）
//
// GET /api/llm/egress-alerts
// 当前实现：列出标记为"local"但有非空 base_url 的 provider（潜在出域配置错误）。
// 后续可接 LLMEgressAuditor 做更精细审计。
func (c *LLMRoutingController) EgressAlerts(ctx *gin.Context) {
	models := c.routingService.ListModels(ctx.Request.Context())
	alerts := make([]gin.H, 0)
	for _, m := range models {
		base := strings.ToLower(m.BaseURL)
		isLocalHint := strings.HasPrefix(m.Name, "default") || m.Vendor == "self_hosted"
		isRemoteHost := base != "" && !strings.Contains(base, "localhost") &&
			!strings.Contains(base, "127.0.0.1") && !strings.Contains(base, "0.0.0.0")
		if isLocalHint && isRemoteHost {
			alerts = append(alerts, gin.H{
				"severity":    "high",
				"alert_type":  "local_egress",
				"provider":    m.Name,
				"base_url":    m.BaseURL,
				"enabled":     m.Enabled,
				"description": "本地推理 provider 配置了外部 base_url，可能导致数据出域",
			})
		}
	}
	ctx.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"alerts": alerts,
			"count":  len(alerts),
		},
	})
}

// @Summary      出域审计（同 EgressAlerts，别名）
// @Tags      l-l-m-routing
// @Router    /api/llm/egress-audit [get]
// EgressAudit 出域审计（同 EgressAlerts，别名）
//
// GET /api/llm/egress-audit?window=hour
func (c *LLMRoutingController) EgressAudit(ctx *gin.Context) {
	c.EgressAlerts(ctx)
}

// @Summary      按日用量聚合（P1-4 模型管理面看板）
// @Tags      l-l-m-routing
// @Router    /api/llm/usage/daily [get]
// DailyUsage GET /api/llm/usage/daily?window=month&group_by=day_scenario
func (c *LLMRoutingController) DailyUsage(ctx *gin.Context) {
	window := ctx.DefaultQuery("window", "month")
	groupBy := ctx.DefaultQuery("group_by", "day_scenario")
	points, err := c.routingService.DailyUsage(ctx.Request.Context(), window, groupBy)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"code": 0, "data": points})
}
