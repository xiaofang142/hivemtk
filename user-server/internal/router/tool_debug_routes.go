package router

import (
	"context"
	"strings"
	"time"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/app"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

func setupToolDebugRoutes(auth *gin.RouterGroup) {
	auth.GET("/agent/tools/list", handleToolList)
	auth.GET("/agent/tools/get", handleToolGet)
	auth.GET("/agent/tools/stats", handleToolStats)
	auth.GET("/agent/tools/audit", handleToolAudit)
	auth.GET("/agent/tools/cost", handleToolCost)
	auth.GET("/agent/tools/providers", handleToolProviders)

	admin := auth.Group("", middleware.AdminAuthMiddleware())
	{
		admin.POST("/agent/tools/execute", handleToolExecute)
		admin.POST("/agent/tools/circuit/reset", handleToolCircuitReset)
	}
}

func handleToolList(c *gin.Context) {
	registry := tooluse.GetGlobalRegistry()
	if registry == nil {
		response.Error(c, 503, "tool registry not initialized")
		return
	}
	category := c.Query("category")
	var tools []tooluse.Tool
	if category != "" {
		tools = registry.ListByCategory(tooluse.ToolCategory(category))
	} else {
		tools = registry.List()
	}
	out := make([]gin.H, 0, len(tools))
	for _, t := range tools {
		out = append(out, gin.H{
			"name":        t.Name(),
			"category":    string(t.Category()),
			"description": t.Description(),
			"parameters":  t.Parameters(),
		})
	}
	response.Success(c, gin.H{
		"total": len(out),
		"tools": out,
	}, "ok")
}

func handleToolGet(c *gin.Context) {
	registry := tooluse.GetGlobalRegistry()
	if registry == nil {
		response.Error(c, 503, "tool registry not initialized")
		return
	}
	name := c.Query("name")
	if name == "" {
		response.Error(c, 400, "name query parameter required")
		return
	}
	tool, err := registry.Get(name)
	if err != nil {
		response.Error(c, 404, err.Error())
		return
	}
	response.Success(c, gin.H{
		"name":        tool.Name(),
		"category":    string(tool.Category()),
		"description": tool.Description(),
		"parameters":  tool.Parameters(),
	}, "ok")
}

type toolExecuteRequest struct {
	ToolName   string         `json:"tool_name" binding:"required"`
	Args       map[string]any `json:"args"`
	AgentID    string         `json:"agent_id"`
	SessionID  string         `json:"session_id"`
	CustomerID string         `json:"customer_id"`
	Source     string         `json:"source"`
}

func handleToolExecute(c *gin.Context) {
	exec := tooluse.GetGlobalExecutor()
	if exec == nil {
		response.Error(c, 503, "tool executor not initialized")
		return
	}
	var req toolExecuteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "invalid request: "+err.Error())
		return
	}
	if req.Args == nil {
		req.Args = map[string]any{}
	}
	toolCtx := &tooluse.ToolContext{
		AgentID:    req.AgentID,
		SessionID:  req.SessionID,
		CustomerID: req.CustomerID,
		Source:     req.Source,
		AuditTrace: "manual:" + time.Now().Format("20060102T150405"),
	}
	if toolCtx.Source == "" {
		toolCtx.Source = "manual"
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 35*time.Second)
	defer cancel()

	router := app.GetGlobalToolRouter()
	var routeResult tooluse.RouteResult
	if router != nil {
		routeResult = router.Route(ctx, req.ToolName, req.Args, toolCtx)
	} else {
		tr, execErr := exec.ExecuteByName(ctx, req.ToolName, req.Args)
		routeResult = tooluse.RouteResult{Result: tr, Err: execErr}
	}

	logger.Infof("[tool-debug] manual execute tool=%s success=%v duration_ms=%d circuit_open=%v caller=%s",
		req.ToolName, routeResult.Result.Success, routeResult.Result.Timing.DurationMs, routeResult.CircuitOpen, toolCtx.AuditTrace)

	respErr := routeResult.Result.Error
	if respErr == "" && routeResult.Err != nil {
		respErr = routeResult.Err.Error()
	}
	resp := gin.H{
		"exec_success": routeResult.Result.Success,
		"tool_result":  routeResult.Result.Data,
		"error":        respErr,
	}
	if routeResult.CircuitOpen {
		resp["circuit_open"] = true
	}
	if routeResult.RateLimit {
		resp["rate_limited"] = true
	}
	response.Success(c, resp, "ok")
}

func handleToolStats(c *gin.Context) {
	registry := tooluse.GetGlobalRegistry()
	exec := tooluse.GetGlobalExecutor()
	router := app.GetGlobalToolRouter()

	data := gin.H{
		"registry_total":     0,
		"executor_available": 0,
	}
	if registry != nil {
		data["registry_total"] = registry.Count()
	}
	if exec != nil {
		data["executor_available"] = len(exec.ListAvailableTools())
	}
	if router != nil {
		data["router_stats"] = router.GetStats()
	} else {
		data["router_stats"] = nil
		data["router_warning"] = "ToolRouter not initialized"
	}
	response.Success(c, data, "ok")
}

func handleToolAudit(c *gin.Context) {
	limit := 100
	if l := c.Query("limit"); l != "" {
		if n, err := atoiSafe(l); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 1000 {
		limit = 1000
	}
	toolName := c.Query("tool")

	exec := tooluse.GetGlobalExecutor()
	if exec == nil {
		response.Error(c, 503, "tool executor not initialized")
		return
	}

	memLogger := app.GetGlobalMemoryAuditLogger()
	if memLogger == nil {
		response.Success(c, gin.H{
			"entries": []any{},
			"warning": "memory audit logger not accessible (may be replaced by DB-backed implementation)",
			"total":   0,
		}, "ok")
		return
	}
	entries := memLogger.Entries()
	out := make([]tooluse.AuditEntry, 0, limit)
	for i := len(entries) - 1; i >= 0 && len(out) < limit; i-- {
		e := entries[i]
		if toolName != "" && e.ToolName != toolName {
			continue
		}
		out = append(out, e)
	}
	response.Success(c, gin.H{
		"total":   len(out),
		"entries": out,
	}, "ok")
}

func handleToolCost(c *gin.Context) {
	memTracker := app.GetGlobalMemoryCostTracker()
	if memTracker == nil {
		response.Success(c, gin.H{
			"stats":   []any{},
			"warning": "memory cost tracker not accessible",
			"total":   0,
		}, "ok")
		return
	}
	stats := memTracker.Stats()
	response.Success(c, gin.H{
		"total": len(stats),
		"stats": stats,
	}, "ok")
}

type toolCircuitResetRequest struct {
	ToolName string `json:"tool_name" binding:"required"`
}

func handleToolCircuitReset(c *gin.Context) {
	router := app.GetGlobalToolRouter()
	if router == nil {
		response.Error(c, 503, "tool router not initialized")
		return
	}
	var req toolCircuitResetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "invalid request: "+err.Error())
		return
	}
	if strings.TrimSpace(req.ToolName) == "" {
		response.Error(c, 400, "tool_name required")
		return
	}
	router.ResetCircuit(req.ToolName)
	logger.Infof("[tool-debug] circuit reset tool=%s by caller=%s", req.ToolName, c.ClientIP())
	response.Success(c, gin.H{
		"tool_name": req.ToolName,
	}, "circuit breaker reset")
}

func atoiSafe(s string) (int, error) {
	if s == "" {
		return 0, errInvalidInteger
	}
	var n int
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, errInvalidInteger
		}
		n = n*10 + int(ch-'0')
	}
	return n, nil
}

var errInvalidInteger = &simpleError{"invalid integer"}

type simpleError struct{ msg string }

func (e *simpleError) Error() string { return e.msg }

func handleToolProviders(c *gin.Context) {
	if app.GetGlobalProviderRegistry() == nil {
		response.Error(c, 503, "provider registry not initialized")
		return
	}
	results := app.GetGlobalProviderRegistry().Results()
	providers := app.GetGlobalProviderRegistry().ListProviders()

	providerInfo := make([]gin.H, 0, len(providers))
	resultMap := make(map[string]tooluse.ProviderRegistrationResult, len(results))
	for _, r := range results {
		resultMap[r.ProviderName] = r
	}

	for _, p := range providers {
		r, hasResult := resultMap[p.Name()]
		info := gin.H{
			"provider_name": p.Name(),
			"category":      string(p.Category()),
			"description":   p.Description(),
		}
		if hasResult {
			info["registered_tools"] = r.RegisteredTools
			info["skipped_tools"] = r.SkippedTools
			info["tool_count"] = r.ToolCount
			info["skipped"] = r.Skipped
			info["skipped_reason"] = r.SkippedReason
			info["err"] = r.Err
			info["duration_ms"] = r.Duration.Milliseconds()
		} else {
			info["registered_tools"] = []string{}
			info["tool_count"] = 0
			info["note"] = "no registration result (registered after last RegisterAll)"
		}
		providerInfo = append(providerInfo, info)
	}

	response.Success(c, gin.H{
		"total_providers":  len(providers),
		"registered_count": app.GetGlobalProviderRegistry().Count(),
		"results":          providerInfo,
	}, "ok")
}
