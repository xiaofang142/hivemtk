package app

import (
	"context"
	"sync"
	"time"

	agent_runtime "hivemtk-user/internal/aiagent/agent/runtime"
	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

type memoryProviderAdapter struct {
	ms *service.MemorySystem
}

func (a *memoryProviderAdapter) LoadEpisodicMemory(ctx context.Context, sessionID, customerID string) (string, error) {
	if a == nil || a.ms == nil {
		return "", nil
	}
	return a.ms.BuildFullContext(ctx, sessionID, customerID)
}

var (
	globalInferenceOrchestrator *agent_runtime.CoreDataFlowOrchestrator
	inferenceOrchestratorOnce   sync.Once
)

// initInferenceOrchestrator 装配全局推理闭环编排器
//
// 调用方：router.Setup()（在 initGlobalToolExecutor 之后调用，
// 依赖 service.InitMemorySystem 已完成）
//
// 装配内容：
//   - 创建 InferenceCycle（含默认 4 阶段：感知/对齐/门禁/规划）
//   - 注入 EpisodicMemoryProvider（包装 MemorySystem）
//   - 创建 CoreDataFlowOrchestrator（暂不注入 AssetLoader/ToolRouter/Publisher，
//
// 由后续 -6b/-6c 逐步激活
func InitInferenceOrchestrator() {
	inferenceOrchestratorOnce.Do(func() {
		cycle := agent_runtime.NewInferenceCycle()

		if ms := service.GetMemorySystem(); ms != nil {
			cycle.SetMemoryProvider(&memoryProviderAdapter{ms: ms})
			logger.Info("[inference] ✅ EpisodicMemoryProvider 已注入（包装 MemorySystem.BuildFullContext）")
		} else {
			logger.Warn("[inference] ⚠️ MemorySystem 未初始化，推理闭环将跳过情境记忆读取")
		}

		globalInferenceOrchestrator = agent_runtime.NewCoreDataFlowOrchestrator(cycle, nil)
		logger.Info("[inference] ✅ CoreDataFlowOrchestrator 已装配（4 阶段推理闭环：感知→对齐→门禁→规划）")
	})
}

// GetInferenceOrchestrator 返回全局推理闭环编排器
//
// 未初始化时返回 nil（调用方需 nil-check）
func GetInferenceOrchestrator() *agent_runtime.CoreDataFlowOrchestrator {
	return globalInferenceOrchestrator
}

type inferenceRunRequest struct {
	ChannelType string `json:"channel_type" binding:"required"`
	CustomerID  string `json:"customer_id" binding:"required"`
	SessionID   string `json:"session_id"`
	Content     string `json:"content" binding:"required"`
	MessageType string `json:"message_type"`
	TraceID     string `json:"trace_id"`
}

// setupInferenceRoutes 注册推理闭环 API 路由
//
// 端点：
//   - POST /api/agent/inference/run    触发一次推理闭环（感知→对齐→门禁→规划）
//   - GET  /api/agent/inference/stats  查询编排器统计
//
// 调用方：router.Setup() 的 auth 路由组
func SetupInferenceRoutes(auth *gin.RouterGroup) {
	auth.POST("/agent/inference/run", handleInferenceRun)
	auth.GET("/agent/inference/stats", handleInferenceStats)
}

func handleInferenceRun(c *gin.Context) {
	orch := GetInferenceOrchestrator()
	if orch == nil {
		response.Error(c, 503, "inference orchestrator not initialized")
		return
	}

	var req inferenceRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "invalid request: "+err.Error())
		return
	}

	payload := agent_runtime.CustomerMessagePayload{
		ChannelType: req.ChannelType,
		CustomerID:  req.CustomerID,
		SessionID:   req.SessionID,
		Content:     req.Content,
		MessageType: req.MessageType,
		Timestamp:   time.Now(),
		TraceID:     req.TraceID,
	}
	if payload.MessageType == "" {
		payload.MessageType = "text"
	}
	if payload.TraceID == "" {
		payload.TraceID = "inference-" + payload.SessionID + "-" + payload.CustomerID
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	result, err := orch.Process(ctx, payload, nil)
	if err != nil {
		logger.Errorf("[inference_api] run failed: %v", err)
		response.Error(c, 500, err.Error())
		return
	}

	response.Success(c, gin.H{
		"session_id":       result.SessionID,
		"final_reply":      result.FinalReply,
		"handoff_to_human": result.HandoffToHuman,
		"handoff_reason":   result.HandoffReason,
		"tool_call_count":  result.ToolCallCount,
		"crisis_level":     result.CrisisLevel,
		"total_duration":   result.TotalDuration.String(),
	}, "ok")
}

func handleInferenceStats(c *gin.Context) {
	orch := GetInferenceOrchestrator()
	if orch == nil {
		response.Error(c, 503, "inference orchestrator not initialized")
		return
	}
	response.Success(c, gin.H{
		"stats": orch.GetStats(),
	}, "ok")
}

var (
	globalPermissionChecker     *tooluse.WhitelistPermissionChecker
	globalPermissionCheckerOnce sync.Once
)

// SetGlobalPermissionChecker 注入全局权限检查器
// 调用方：tool_executor_wiring.go 在创建 ToolExecutor 时同步注入
func SetGlobalPermissionChecker(pc *tooluse.WhitelistPermissionChecker) {
	globalPermissionChecker = pc
}

// GetGlobalPermissionChecker 获取全局权限检查器
// 未初始化时惰性创建一个默认实例（defaultAllow=true，向后兼容）
// 返回 nil 的情况：理论上不会发生（惰性初始化保证非 nil）
func GetGlobalPermissionChecker() *tooluse.WhitelistPermissionChecker {
	globalPermissionCheckerOnce.Do(func() {
		if globalPermissionChecker == nil {
			globalPermissionChecker = tooluse.NewWhitelistPermissionChecker()
		}
	})
	return globalPermissionChecker
}

// setupToolPermissionRoutes 注册工具权限白名单管理路由
//
// 端点：
//   - GET    /api/agent/tools/permission/default           查询默认放行策略
//   - PUT    /api/agent/tools/permission/default           设置默认放行策略
//   - GET    /api/agent/tools/permission/global            查询全局白名单
//   - POST   /api/agent/tools/permission/global            追加全局白名单工具
//   - GET    /api/agent/tools/permission/agents            列出已配置白名单的 Agent
//   - GET    /api/agent/tools/permission/agents/:agent_id  查询指定 Agent 的白名单
//   - POST   /api/agent/tools/permission/agents/:agent_id  设置指定 Agent 的白名单（覆盖式）
//   - DELETE /api/agent/tools/permission/agents/:agent_id  移除指定 Agent 的白名单配置
func SetupToolPermissionRoutes(auth *gin.RouterGroup) {
	auth.GET("/tools/permission/default", handleGetPermissionDefault)
	auth.PUT("/tools/permission/default", handleSetPermissionDefault)
	auth.GET("/tools/permission/global", handleGetGlobalWhitelist)
	auth.POST("/tools/permission/global", handleAddGlobalWhitelist)
	auth.GET("/tools/permission/agents", handleListConfiguredAgents)
	auth.GET("/tools/permission/agents/:agent_id", handleGetAgentWhitelist)
	auth.POST("/tools/permission/agents/:agent_id", handleSetAgentWhitelist)
	auth.DELETE("/tools/permission/agents/:agent_id", handleRemoveAgentWhitelist)
}

func handleGetPermissionDefault(c *gin.Context) {
	pc := GetGlobalPermissionChecker()
	if pc == nil {
		response.Error(c, 503, "permission checker not initialized")
		return
	}
	response.Success(c, gin.H{
		"default_allow": pc.GetDefaultAllow(),
	}, "ok")
}

type setPermissionDefaultRequest struct {
	DefaultAllow bool `json:"default_allow"`
}

func handleSetPermissionDefault(c *gin.Context) {
	pc := GetGlobalPermissionChecker()
	if pc == nil {
		response.Error(c, 503, "permission checker not initialized")
		return
	}
	var req setPermissionDefaultRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "invalid request: "+err.Error())
		return
	}
	pc.SetDefaultAllow(req.DefaultAllow)
	logger.Infof("[permission] default_allow set to %v", req.DefaultAllow)
	response.Success(c, gin.H{"default_allow": req.DefaultAllow}, "ok")
}

func handleGetGlobalWhitelist(c *gin.Context) {
	pc := GetGlobalPermissionChecker()
	if pc == nil {
		response.Error(c, 503, "permission checker not initialized")
		return
	}
	tools := pc.ListGlobalWhitelist()
	response.Success(c, gin.H{
		"tools": tools,
		"count": len(tools),
	}, "ok")
}

type addGlobalWhitelistRequest struct {
	Tools []string `json:"tools" binding:"required"`
}

func handleAddGlobalWhitelist(c *gin.Context) {
	pc := GetGlobalPermissionChecker()
	if pc == nil {
		response.Error(c, 503, "permission checker not initialized")
		return
	}
	var req addGlobalWhitelistRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "invalid request: "+err.Error())
		return
	}
	pc.AddGlobalWhitelist(req.Tools)
	logger.Infof("[permission] global whitelist added %d tools", len(req.Tools))
	response.Success(c, gin.H{
		"tools": pc.ListGlobalWhitelist(),
		"count": len(pc.ListGlobalWhitelist()),
	}, "ok")
}

func handleListConfiguredAgents(c *gin.Context) {
	pc := GetGlobalPermissionChecker()
	if pc == nil {
		response.Error(c, 503, "permission checker not initialized")
		return
	}
	agents := pc.ListConfiguredAgents()
	response.Success(c, gin.H{
		"agents": agents,
		"count":  len(agents),
	}, "ok")
}

func handleGetAgentWhitelist(c *gin.Context) {
	pc := GetGlobalPermissionChecker()
	if pc == nil {
		response.Error(c, 503, "permission checker not initialized")
		return
	}
	agentID := c.Param("agent_id")
	if agentID == "" {
		response.Error(c, 400, "agent_id required")
		return
	}
	tools := pc.ListAgentWhitelist(agentID)
	response.Success(c, gin.H{
		"agent_id": agentID,
		"tools":    tools,
		"count":    len(tools),
	}, "ok")
}

type setAgentWhitelistRequest struct {
	Tools []string `json:"tools" binding:"required"`
}

func handleSetAgentWhitelist(c *gin.Context) {
	pc := GetGlobalPermissionChecker()
	if pc == nil {
		response.Error(c, 503, "permission checker not initialized")
		return
	}
	agentID := c.Param("agent_id")
	if agentID == "" {
		response.Error(c, 400, "agent_id required")
		return
	}
	var req setAgentWhitelistRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "invalid request: "+err.Error())
		return
	}
	pc.SetAgentWhitelist(agentID, req.Tools)
	logger.Infof("[permission] agent=%s whitelist set (%d tools)", agentID, len(req.Tools))
	response.Success(c, gin.H{
		"agent_id": agentID,
		"tools":    pc.ListAgentWhitelist(agentID),
		"count":    len(pc.ListAgentWhitelist(agentID)),
	}, "ok")
}

func handleRemoveAgentWhitelist(c *gin.Context) {
	pc := GetGlobalPermissionChecker()
	if pc == nil {
		response.Error(c, 503, "permission checker not initialized")
		return
	}
	agentID := c.Param("agent_id")
	if agentID == "" {
		response.Error(c, 400, "agent_id required")
		return
	}
	pc.RemoveAgentWhitelist(agentID)
	logger.Infof("[permission] agent=%s whitelist removed (fallback to default policy)", agentID)
	response.Success(c, gin.H{
		"agent_id": agentID,
	}, "whitelist removed, fallback to default policy")
}
