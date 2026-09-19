package router

import (
	"context"
	"sort"
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
	auth.GET("/agent/tools/circuit", handleToolCircuitState)
	auth.GET("/agent/tools/approval", handleToolApprovalState)
	auth.GET("/agent/tools/providers", handleToolProviders)

	admin := auth.Group("", middleware.AdminAuthMiddleware())
	{
		admin.POST("/agent/tools/execute", handleToolExecute)
		admin.POST("/agent/tools/circuit/reset", handleToolCircuitReset)
		admin.POST("/agent/tools/approval/whitelist", handleToolApprovalWhitelist)
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

// handleToolCircuitState 读取工具熔断的接线状态与判定累计（T-P1-04）。
//
// 为什么要一次读两套：本项目有两处互不相干的"熔断"——
//   - executor 装饰链上的 tooluse.CircuitBreakerRegistry（本卡接上，按工具累计连续失败）
//   - ToolRouter 内部的 r.circuit（早已存在，用于失败工具短路/切换）
//
// 两者独立计量、独立冷却。排障时只看其中一套会得出相反结论，故本端点把两套一起摊开，
// 并显式标出 executor 侧是否真的接上了（wired=false 时其余字段全是空值，别当成"没有工具出问题"）。
func handleToolCircuitState(c *gin.Context) {
	mode, cfg, registry, decisions := app.GetToolCircuitSnapshot()

	out := gin.H{
		"mode":             mode,
		"wired":            registry != nil,
		"executor_circuit": []gin.H{},
		"config": gin.H{
			"failure_threshold":      cfg.FailureThreshold,
			"base_cooldown":          cfg.BaseCooldown.String(),
			"max_cooldown":           cfg.MaxCooldown.String(),
			"backoff_multiplier":     cfg.BackoffMultiplier,
			"half_open_max_attempts": cfg.HalfOpenMaxAttempts,
		},
		"env_hint": "FF_TOOL_CIRCUIT_BREAKER=off|shadow|enforce（默认 off；true/on 一律按 shadow 处理，不直接取得拦截能力）",
	}
	if registry != nil {
		states := registry.AllStates()
		list := make([]gin.H, 0, len(states))
		for name, st := range states {
			list = append(list, gin.H{
				"tool_name":         name,
				"state":             st.State.String(),
				"consecutive_fails": st.ConsecutiveFails,
				"open_count":        st.OpenCount,
			})
		}
		sort.Slice(list, func(i, j int) bool {
			return list[i]["tool_name"].(string) < list[j]["tool_name"].(string)
		})
		out["executor_circuit"] = list
	}
	if decisions != nil {
		rep := decisions.Report()
		per := make([]gin.H, 0, len(rep.PerTool))
		for _, st := range rep.PerTool {
			per = append(per, gin.H{
				"tool_name":              st.ToolName,
				"total":                  st.Total,
				"would_block":            st.WouldBlock,
				"last_state":             st.LastState.String(),
				"last_consecutive_fails": st.LastFails,
			})
		}
		out["decision_report"] = gin.H{
			"total":                rep.Total,
			"would_block":          rep.WouldBlock,
			"would_block_rate_pct": rep.WouldBlockRatePct,
			"per_tool":             per,
		}
	}
	response.Success(c, out, "ok")
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
	// 两套熔断都要清：只清 ToolRouter 那套的话，运维点了"重置"后 executor 侧仍会
	// 继续返回 ErrCircuitOpen，接口却回答"circuit breaker reset"——这是假复位。
	_, _, registry, _ := app.GetToolCircuitSnapshot()
	if registry != nil {
		registry.ResetTool(req.ToolName)
	}
	logger.Infof("[tool-debug] circuit reset tool=%s executor_side_circuit=%t by caller=%s",
		req.ToolName, registry != nil, c.ClientIP())
	response.Success(c, gin.H{
		"tool_name":              req.ToolName,
		"router_circuit_reset":   true,
		"executor_circuit_reset": registry != nil,
	}, "circuit breaker reset")
}

// toolApprovalWhitelistRequest 冷触达审批白名单授权请求。
type toolApprovalWhitelistRequest struct {
	ToolName  string `json:"tool_name" binding:"required"`
	AccountID string `json:"account_id" binding:"required"`

	// ExpiresAt RFC3339；留空 = 永不过期。
	ExpiresAt string `json:"expires_at"`
	Revoke    bool   `json:"revoke"`
}

// approvalStatePayload 把审批门快照渲染成端点响应体（不含判定报告）。
//
// 四件事必须一起看清，否则这份报告会被读反：
//   - mode/wired：闸门有没有挂上执行链（FF_LTC_APPROVAL_GATE）
//   - whitelist_flag_on：白名单有没有生效（approval.FlagKey，另一把旗子）
//   - blocks_when_denied：这个模式下"拒"会不会真的传下去（只有 block 为 true）
//   - by_reason：would_deny 是"闸门没开"还是"账号没被批准"
//
// would_deny 与"实际被拦"是两回事：shadow 态它照样增长却一单不拦；block 态它仍然
// 包含被刹车豁免的 disabled_by_flag 那一类。所以两者不能互相代替，字段也刻意不合并。
//
// 抽成纯函数是为了让 block 态那段回显**可测**：闸门模式在装配期读一次，
// 测试进程里既没有 HTTP 启动也没有第二次装配，走 handle 只能测到 off 那一支。
// 于是把"给定快照 → 该回显哪些字段"单独拿出来，用构造出的快照直接断言。
func approvalStatePayload(snap app.ApprovalGateSnapshot) gin.H {
	out := gin.H{
		"mode":               snap.Mode,
		"wired":              snap.Wired,
		"global_checker_set": snap.GlobalCheckerSet,
		"blocks_when_denied": snap.BlocksWhenDenied,
		"flags": gin.H{
			"gate":              snap.GateFlagEnv,
			"whitelist":         snap.WhitelistFlagKey,
			"whitelist_env":     snap.WhitelistFlagEnv,
			"whitelist_flag_on": snap.WhitelistFlagOn,
		},
		"whitelist_active_entries": snap.WhitelistActiveEntries,
		"env_hint": app.ApprovalGateFlagEnv + "=off|shadow|block（shadow 只记录判定、冷触达照常外发；" +
			"block 才真的拒，且需同时开白名单旗子 " + snap.WhitelistFlagEnv +
			"；闸门模式在装配期读一次，改完须重启）",
		"reading_hint": "would_deny = 切阻断后会被拦的次数（与实不实际拦无关）。by_reason 里 disabled_by_flag 占多数时，" +
			"结论是「白名单旗子没开」，不是「账号没被批准」；block 态这一类还会被刹车放行",
	}
	if snap.BlocksWhenDenied && !snap.WhitelistFlagOn {
		out["brake_engaged"] = true
		out["brake_note"] = "block 态但白名单旗子未开 ⇒ reason=disabled_by_flag 的拒绝不拦，当前实际等价于 shadow；" +
			"要真拦请开白名单旗子，并先确认 whitelist_active_entries 已经灌好（block + 0 条 = 全部冷触达被拒）"
	}
	return out
}

// handleToolApprovalState 读取冷触达审批门的接线状态与判定累计（T-P1-05 观察 / T-P1-06 三态）。
// 回显口径见 approvalStatePayload。
func handleToolApprovalState(c *gin.Context) {
	snap, decisions := app.GetApprovalSnapshot()
	out := approvalStatePayload(snap)
	if decisions != nil {
		rep := decisions.Report()
		per := make([]gin.H, 0, len(rep.PerTool))
		for _, st := range rep.PerTool {
			per = append(per, gin.H{
				"tool_name":   st.ToolName,
				"total":       st.Total,
				"would_deny":  st.WouldDeny,
				"last_reason": st.LastReason,
			})
		}
		out["decision_report"] = gin.H{
			"total":               rep.Total,
			"would_deny":          rep.WouldDeny,
			"would_deny_rate_pct": rep.WouldDenyRatePct,
			"by_reason":           rep.ByReason,
			"per_tool":            per,
		}
	}
	response.Success(c, out, "ok")
}

// handleToolApprovalWhitelist 给 (tool, account) 授权/撤权。
//
// 授权内容只落进程内存、重启即空：这不是偷懒，是刻意的下限——
// 持久化授权要过审批流与留痕表（T-P3 的审批闸门域），在这里偷偷写一张表反而会造出
// 第二个授权来源。
//
// 后果按模式分：shadow 态授权不改变任何行为（本来就放行），它的作用是让报告能区分
// "这个账号真的该放"与"只是没人给他开过"；block 态这里就是**唯一的放行开关**，
// revoke 一按，该账号下一次冷触达立刻被拒（且重启后条目清零 ⇒ 全拒）。
func handleToolApprovalWhitelist(c *gin.Context) {
	var req toolApprovalWhitelistRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "invalid request: "+err.Error())
		return
	}
	toolName := strings.TrimSpace(req.ToolName)
	accountID := strings.TrimSpace(req.AccountID)
	if toolName == "" || accountID == "" {
		response.Error(c, 400, "tool_name and account_id required")
		return
	}
	var expiresAt time.Time
	if s := strings.TrimSpace(req.ExpiresAt); s != "" {
		parsed, err := time.Parse(time.RFC3339, s)
		if err != nil {
			response.Error(c, 400, "expires_at 需为 RFC3339，如 2026-10-01T00:00:00Z")
			return
		}
		expiresAt = parsed
	}
	if !app.ApprovalWhitelistMutate(toolName, accountID, expiresAt, req.Revoke) {
		response.Error(c, 503, "审批门未接线（先设 "+app.ApprovalGateFlagEnv+"=shadow|block 再重启服务）")
		return
	}
	expires := "never"
	if !expiresAt.IsZero() {
		expires = expiresAt.UTC().Format(time.RFC3339)
	}
	logger.Infof("[tool-debug] approval whitelist mutate tool=%s account=%s revoke=%t expires=%s by caller=%s",
		toolName, accountID, req.Revoke, expires, c.ClientIP())
	response.Success(c, gin.H{
		"tool_name":  toolName,
		"account_id": accountID,
		"revoked":    req.Revoke,
		"expires_at": expires,
		"persisted":  false,
		"note":       "白名单仅存于进程内存，重启即空；需持久授权请走审批闸门域，勿依赖本端点",
	}, "ok")
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
