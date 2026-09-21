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
	auth.GET("/agent/tools/reach-gate", handleReachGateState)
	auth.GET("/agent/tools/risk", handleToolRiskReport)
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

	snap := app.GetToolAuditSnapshot()
	if isDBSource(c.Query("source")) {
		auditFromDB(c, snap, toolName, limit)
		return
	}

	memLogger := app.GetGlobalMemoryAuditLogger()
	if memLogger == nil {
		response.Success(c, gin.H{
			"source":      "memory",
			"entries":     []any{},
			"warning":     "memory audit logger not accessible (may be replaced by DB-backed implementation)",
			"total":       0,
			"persistence": toolAuditPersistenceEcho(snap),
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
		"total":       len(out),
		"entries":     out,
		"source":      "memory",
		"persistence": toolAuditPersistenceEcho(snap),
	}, "ok")
}

// isDBSource 认 ?source=db（大小写不敏感）。其它值一律走内存，
// 并在回显里留下实际生效的 source，避免运维以为看的是库。
func isDBSource(raw string) bool {
	return strings.EqualFold(strings.TrimSpace(raw), "db")
}

// toolAuditPersistenceEcho 落库接线状态回显（/audit 与 /cost 共用同一份口径）。
//
// 为什么默认路径也要回显：旗子没开时 ?source=db 会 503，而看默认内存响应的人只会得到
// "有一堆审计"这个印象，永远不知道它们重启就没了。
func toolAuditPersistenceEcho(snap app.ToolAuditSnapshot) gin.H {
	out := gin.H{
		"mode":        snap.Mode,
		"db_wired":    snap.Wired,
		"table":       snap.TableName,
		"flag_env":    app.ToolAuditFlagEnv,
		"db_handle":   snap.DBHandle,
		"mem_entries": snap.MemCapUsed,
	}
	if snap.HasStats {
		out["queue_size"] = snap.QueueSize
		out["db_stats"] = snap.DBStats
	}
	return out
}

// toolAuditDBBlockedReason 判定"DB 读侧现在能不能查"，返回非空即应回 503。
//
// 抽成纯函数：旗子在装配期读一次，测试进程里既没有 HTTP 也没有第二次装配，
// 走 handle 只能测到 off 那一支；把判定单拿出来才能断言各支的措辞。
//
// 三种"不能查"分开说，因为处置动作完全不同；措辞里一律带上 mode。旗子名用包级常量
// 而不用 snap.FlagEnv —— 后者是快照字段，一旦哪个构造路径忘了填，运维就会读到
// "设 =on 并重启"这种指不了任何地方的话（本卡的测试实测到了这个形状）。
func toolAuditDBBlockedReason(snap app.ToolAuditSnapshot) string {
	switch {
	case !snap.DBHandle && !snap.Wired:
		return "审计未落库（mode=" + snap.Mode + "）：设 " + app.ToolAuditFlagEnv + "=on 并重启；当前审计只在内存里，重启即丢"
	case !snap.DBHandle:
		return "mode=" + snap.Mode + " 且写侧已接线，但读侧句柄为 nil ⇒ 查不了库（查启动日志里的 [tool-audit] 告警）"
	case !snap.Wired:
		// 库里可能真有历史行（别的进程/更早一次接线写的），但本进程不产新行 ——
		// 与其回一份"看起来是审计记录"的东西，不如把口径说清楚。
		return "读侧有句柄但写侧未接线（mode=" + snap.Mode + "）⇒ ?source=db 只会查到既有行，本进程不落库；" +
			"要落库请设 " + app.ToolAuditFlagEnv + "=on 并重启"
	}
	return ""
}

// auditFromDB 从 tool_call_audits 读审计行。
func auditFromDB(c *gin.Context, snap app.ToolAuditSnapshot, toolName string, limit int) {
	if reason := toolAuditDBBlockedReason(snap); reason != "" {
		response.Error(c, 503, reason)
		return
	}
	rows, err := app.ToolAuditDBRecent(c.Request.Context(), toolName, limit)
	if err != nil {
		// 读失败不回空列表：空列表的含义是"没有审计"，与"查不动"是两回事。
		response.Error(c, 500, "读取持久化审计失败: "+err.Error())
		return
	}
	total, cntErr := app.ToolAuditDBCount(c.Request.Context())
	out := gin.H{
		"source":      "db",
		"total":       len(rows),
		"entries":     rows,
		"persistence": toolAuditPersistenceEcho(snap),
	}
	if cntErr == nil {
		out["persisted_total"] = total
	} else {
		out["persisted_total_error"] = cntErr.Error()
	}
	if snap.DBStats.DBRows == 0 && total > 0 {
		// 本进程一条没写、库里却有行 ⇒ 查得到东西但那是别的进程（或上一版）写的。
		// 不点明的话，多副本环境里"我的写入正常"会被这张表的既有数据佐证成假结论。
		out["note"] = "本进程落库计数为 0，以上行由其他进程/更早的启动写入"
	}
	response.Success(c, out, "ok")
}

func handleToolCost(c *gin.Context) {
	snap := app.GetToolAuditSnapshot()
	if isDBSource(c.Query("source")) {
		if reason := toolAuditDBBlockedReason(snap); reason != "" {
			response.Error(c, 503, reason)
			return
		}
		rows, err := app.ToolAuditDBCostAggregates(c.Request.Context())
		if err != nil {
			response.Error(c, 500, "读取持久化计费聚合失败: "+err.Error())
			return
		}
		stats := make([]gin.H, 0, len(rows))
		for _, row := range rows {
			// 键与 tooluse.CostStats 逐一对齐：消费方不该因为换了数据源就要改解析。
			stats = append(stats, gin.H{
				"tool_name":         row.ToolName,
				"total_calls":       row.TotalCalls,
				"success_calls":     row.SuccessCalls,
				"failed_calls":      row.FailedCalls,
				"total_duration_ms": row.TotalDurationMs,
				"success_rate":      row.SuccessRate(),
				"avg_duration_ms":   row.AvgDurationMs(),
			})
		}
		response.Success(c, gin.H{
			"source":      "db",
			"total":       len(stats),
			"stats":       stats,
			"persistence": toolAuditPersistenceEcho(snap),
			"note": "DB 口径来自 tool_call_audits 按 tool_name 聚合（不受内存 10000 条上限与重启影响），" +
				"与内存口径在长时间运行后必然不等，差异本身就是丢失量",
		}, "ok")
		return
	}

	memTracker := app.GetGlobalMemoryCostTracker()
	if memTracker == nil {
		response.Success(c, gin.H{
			"source":      "memory",
			"stats":       []any{},
			"warning":     "memory cost tracker not accessible",
			"total":       0,
			"persistence": toolAuditPersistenceEcho(snap),
		}, "ok")
		return
	}
	stats := memTracker.Stats()
	response.Success(c, gin.H{
		"source":      "memory",
		"total":       len(stats),
		"stats":       stats,
		"persistence": toolAuditPersistenceEcho(snap),
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

// reachGateStatePayload 外发闸门的回显口径（T-P3-07）。
//
// 与 approvalStatePayload 同形，但三个读数是 reach 专属的、缺一不可：
//   - whitelist_entries_for_reach：授权表两条路径共用，只看总数会把"工具侧批了 5 个"
//     读成"外发也批了"，而 block 态下真实结果可能是所有外发都被拒；
//   - attached_services：几个外发装配点拿到了钩子。漏接一个 = 留一条盲区，
//     而这个数字是唯一能从进程内部看出来的东西；
//   - dependency_unmet：mode 不是 off 却没装上门，只有一种成因（W-1 没接线），
//     不点名的话端点上就是"旗子开了却没生效"这种查不出所以然的形状。
func reachGateStatePayload(snap app.ReachGateSnapshot) gin.H {
	out := gin.H{
		"mode":                        snap.Mode,
		"wired":                       snap.Wired,
		"blocks_when_denied":          snap.BlocksWhenDenied,
		"dependency_unmet":            snap.DependencyUnmet,
		"dependency_flag_env":         snap.DependencyFlagEnv,
		"reach_tool_key":              snap.ReachToolKey,
		"attached_services":           snap.AttachedServices,
		"whitelist_active_entries":    snap.WhitelistActiveEntries,
		"whitelist_entries_for_reach": snap.WhitelistEntriesForReach,
		// T-P5-04：durable 放量档。三个读数一起给，是因为"档位写成了 halt/whitelist"
		// 与"这一档现在真的在拦"是两件事（后者还要 env=block），只看 mode 会把前者读成后者。
		"rollout_mode":              snap.RolloutMode,
		"rollout_whitelist_entries": snap.RolloutWhitelistEntries,
		"rollout_degraded":          snap.RolloutDegraded,
		"flags": gin.H{
			"gate":              snap.GateFlagEnv,
			"whitelist_env":     snap.WhitelistFlagEnv,
			"whitelist_flag_on": snap.WhitelistFlagOn,
		},
		"env_hint": app.ReachGateFlagEnv + "=off|shadow|block（shadow 只记 would_deny、外发照常；" +
			"block 才真的拒，且需同时开白名单旗子 " + snap.WhitelistFlagEnv +
			"；依赖 " + snap.DependencyFlagEnv + "=shadow|block，装配期各读一次，改完须重启）",
		"reading_hint": "would_deny = 切阻断后会被拦的外发次数（与实不实际拦无关），判定键是客户身份 " +
			"（one_id → customer_id → 渠道:收件人），不是恒空的 account_id。by_reason 里 disabled_by_flag " +
			"占多数时结论是「白名单旗子没开」，不是「对象没被批准」；block 态这一类还会被刹车放行。" +
			"白名单与工具门共用，所以要看 whitelist_entries_for_reach 而不是总数",
	}
	if snap.DependencyUnmet {
		out["dependency_note"] = "本门未装：W-1 的裁决来源不存在 ⇒ 先开 " + snap.DependencyFlagEnv +
			"=shadow|block 再开 " + snap.GateFlagEnv + "；没有裁决来源的闸门只能恒放或恒拒，两种都长得像在拦"
	}
	if snap.BlocksWhenDenied && !snap.WhitelistFlagOn {
		out["brake_engaged"] = true
		out["brake_note"] = "block 态但白名单旗子未开 ⇒ reason=disabled_by_flag 的拒绝不拦，当前实际等价于 shadow；" +
			"要真拦请开白名单旗子，并先按 tool=" + snap.ReachToolKey + " 灌好授权"
	}
	if snap.BlocksWhenDenied && snap.WhitelistFlagOn && snap.WhitelistEntriesForReach == 0 {
		out["no_grant_for_reach"] = true
		out["grant_warning"] = "block 态且 reach 名下有效授权=0 ⇒ 所有非工具外发都会被拒（denied_default）。" +
			"放量前用 POST /agent/tools/approval/whitelist 以 tool=" + snap.ReachToolKey +
			"、account_id=客户身份 灌入；该表只在进程内存里，重启即空"
	}
	// T-P5-04：durable 档位与 env 旗子的关系单独一句。少了它，"库里已写 whitelist"
	// 与"灰度已经在拦"在端点上长同一个样。
	if note := app.ReachRolloutCouplingNote(snap); note != "" {
		out["rollout_note"] = note
	}
	return out
}

// handleReachGateState 读取非工具外发闸门的接线状态与判定累计（T-P3-07）。
// 回显口径见 reachGateStatePayload。
func handleReachGateState(c *gin.Context) {
	snap, decisions := app.GetReachGateSnapshot()
	out := reachGateStatePayload(snap)
	// 观测块无论装没装门都要出现：未接线是"这个数现在没有"，不是"这个数不用看"。
	out["rollout_observation"] = app.ObserveReachRollout(snap, decisions)
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

// handleToolRiskReport 输出工具后果分级报告（T-P3-05 AC③，P9 转阻断前的评审材料）。
//
// 与 /agent/tools/approval 的分工：那份读的是"这次冷触达有没有被批准"（按工具名
// 启发式圈定范围、按账号判定），这份读的是"这个工具的后果能不能撤回"（按声明分级、
// 按 Agent 自己的白名单找授权依据）。两个端点的 would_deny 数不相等是预期的，
// 差别本身（尤其 high_write_in_approval_gate）就是 G-3 要补的盲区大小。
//
// 旗子关着时也可以读：静态声明面与授权面不依赖观察层，P9 评审恰恰要在开旗之前看它。
func handleToolRiskReport(c *gin.Context) {
	report := app.GetToolRiskReport()
	if report.Total == 0 {
		// 注册中心为空 = 工具链还没装配（启动顺序问题），不是"45 个工具都低风险"。
		// 不显式区分的话，一份 total=0 的报告会被读成"没有任何高危工具需要管控"。
		response.Error(c, 503, "tool registry not initialized：分级报告需要已装配的工具注册中心")
		return
	}
	response.Success(c, report, "ok")
}

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
