package controller

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

// SSEDashboardController SSE 驾驶舱控制器
type SSEDashboardController struct {
	hub *service.SSEHub
}

// NewSSEDashboardController 创建 SSE 驾驶舱控制器
func NewSSEDashboardController() *SSEDashboardController {
	return &SSEDashboardController{
		hub: service.InitGlobalSSEHub(),
	}
}

// WithHub 注入自定义 Hub（测试用）
func (c *SSEDashboardController) WithHub(hub *service.SSEHub) *SSEDashboardController {
	c.hub = hub
	return c
}

// Stream SSE 流连接
// GET /api/dashboard/sse?topics=llm_calls,intent_recognition
func (c *SSEDashboardController) Stream(ctx *gin.Context) {
	if c.hub == nil || c.hub.Stopped(ctx.Request.Context()) {
		response.Error(ctx, http.StatusServiceUnavailable, "SSE hub not available")
		return
	}

	topicsStr := ctx.Query("topics")
	topics := service.ParseTopics(topicsStr)
	if len(topics) == 0 {
		response.Error(ctx, http.StatusBadRequest, "topics parameter required (e.g. ?topics=llm_calls,intent_recognition)")
		return
	}

	clientID := uuid.NewString()
	clientIP := ctx.ClientIP()
	client := service.NewSSEClient(clientID, clientIP, topics)

	if err := c.hub.Register(ctx.Request.Context(), client); err != nil {
		response.Error(ctx, http.StatusTooManyRequests, fmt.Sprintf("register failed: %v", err))
		return
	}
	defer c.hub.Unregister(ctx.Request.Context(), clientID)

	service.SSEStreamHandler(ctx, c.hub, client)
}

// ListClients 列出所有 SSE 客户端
// GET /api/dashboard/clients
func (c *SSEDashboardController) ListClients(ctx *gin.Context) {
	if c.hub == nil {
		response.Success(ctx, []any{}, "no hub")
		return
	}
	clients := c.hub.ListClients(ctx.Request.Context())
	response.SuccessWithList(ctx, clients, int64(len(clients)))
}

// BroadcastRequest 广播事件请求
type BroadcastRequest struct {
	Topic     string `json:"topic" binding:"required"`
	EventType string `json:"event_type" binding:"required"`
	Data      any    `json:"data"`
	TraceID   string `json:"trace_id,omitempty"`
}

// Broadcast 广播事件（管理员测试用）
// POST /api/dashboard/broadcast
func (c *SSEDashboardController) Broadcast(ctx *gin.Context) {
	if c.hub == nil || c.hub.Stopped(ctx.Request.Context()) {
		response.Error(ctx, http.StatusServiceUnavailable, "SSE hub not available")
		return
	}
	var req BroadcastRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	if !service.IsValidSSETopic(req.Topic) {
		response.Error(ctx, http.StatusBadRequest, "invalid topic: "+req.Topic)
		return
	}
	c.hub.Publish(ctx.Request.Context(), service.SSEEvent{
		Topic:     req.Topic,
		EventType: req.EventType,
		Data:      req.Data,
		TraceID:   req.TraceID,
		Timestamp: time.Now(),
	})
	response.Success(ctx, map[string]any{
		"client_count": c.hub.GetClientCount(ctx.Request.Context()),
		"topic":        req.Topic,
	}, "broadcast success")
}

// ListTopics 返回可用的 SSE topic 列表
// GET /api/dashboard/topics
func (c *SSEDashboardController) ListTopics(ctx *gin.Context) {
	topics := []map[string]any{
		{
			"name":        service.SSETopicLLMCalls,
			"description": "LLM 调用事件（chat completion / embedding / tool call）",
		},
		{
			"name":        service.SSETopicIntentRecogn,
			"description": "意图识别事件（rule / llm / hybrid）",
		},
		{
			"name":        service.SSETopicRAGQueries,
			"description": "RAG 检索事件（向量检索 / 关键词检索 / 混合检索）",
		},
		{
			"name":        service.SSETopicAgentActions,
			"description": "智能体动作事件（ReAct loop / tool execution）",
		},
		{
			"name":        service.SSETopicHumanizeScores,
			"description": "拟人度评分事件（rule_scorer / llm_scorer / feedback）",
		},
		{
			"name":        service.SSETopicSystemAlerts,
			"description": "系统告警事件（provider failover / circuit open / SLA breach）",
		},
	}
	response.SuccessWithList(ctx, topics, int64(len(topics)))
}

// Stats SSE Hub 统计
// GET /api/dashboard/stats
// Deprecated: 该接口已废弃，请使用 /api/manage/dashboard_sse_stats（dashboard_sse_stats 服务）。
func (c *SSEDashboardController) Stats(ctx *gin.Context) {
	logger.Warnf("[Deprecated] /api/dashboard/stats 已废弃，请使用 /api/manage/dashboard_sse_stats (dashboard_sse_stats 服务)")
	if c.hub == nil {
		response.Success(ctx, map[string]any{
			"client_count": 0,
			"stopped":      true,
		}, "no hub")
		return
	}
	stats := map[string]any{
		"client_count":    c.hub.GetClientCount(ctx.Request.Context()),
		"stopped":         c.hub.Stopped(ctx.Request.Context()),
		"max_conn_per_ip": service.SSEMaxConnPerIP,
		"buffer_size":     service.SSEClientBufferSize,
		"heartbeat_sec":   int(service.SSEHeartbeatInterval / time.Second),
		"available_topics": []string{
			service.SSETopicLLMCalls,
			service.SSETopicIntentRecogn,
			service.SSETopicRAGQueries,
			service.SSETopicAgentActions,
			service.SSETopicHumanizeScores,
			service.SSETopicSystemAlerts,
		},
	}
	response.Success(ctx, stats, "ok")
}
