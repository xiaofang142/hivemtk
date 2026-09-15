package monitor

import (
	"net/http"
	"strconv"

	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// RegisterRoutes 在调用方给定的路由组上注册监控端点。
//
// 鉴权由**调用方**负责：路由组必须已挂载 JWTAuthMiddleware（见
// internal/router/router.go 中 `auth.Use(middleware.JWTAuthMiddleware())` 之后的调用点）。
// 本包不自带鉴权中间件，切勿把本函数接到未受保护的路由组上。
//
// 历史缺陷（已修）：调用点曾位于 `auth.Use(JWTAuthMiddleware())` **之前**，
// 而 gin 的 RouterGroup.Use 只在路由注册时快照 handler 链，对先注册的路由不生效，
// 导致下列 7 个接口长期匿名可访问，且响应体含会话链路明细（MessageTrace）。
//
// 注意：UI 已迁至 user-web 前端（src/views/system/TraceMonitor.vue），
// 本包仅暴露 JSON 数据接口，由前端调用渲染。
func RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/monitor/health", healthHandler)
	rg.GET("/monitor/anomalies", anomaliesHandler)
	rg.GET("/monitor/node-health", nodeHealthHandler)
	rg.GET("/monitor/latency", latencyHandler)
	rg.GET("/monitor/lifecycle", lifecycleHandler)
	rg.GET("/monitor/traces", tracesHandler)
	rg.GET("/monitor/trace-tree", traceTreeHandler)
}

func healthHandler(c *gin.Context) {
	h, err := HealthOverview(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, h, "success")
}

func anomaliesHandler(c *gin.Context) {
	a, err := Anomalies(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, a, "success")
}

func nodeHealthHandler(c *gin.Context) {
	nh, err := NodeHealthByChannel(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, gin.H{"nodes": nh, "window": nodeHealthWindow.String()}, "success")
}

func latencyHandler(c *gin.Context) {
	l, err := LifecycleLatencyByChannel(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, l, "success")
}

func lifecycleHandler(c *gin.Context) {
	conv := c.Query("conversation_id")
	tid := c.Query("trace_id")
	limit := 20
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	lc, err := Lifecycle(c.Request.Context(), conv, tid, limit)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	flat := make([]LifecycleNode, 0)
	for _, round := range lc {
		flat = append(flat, round.Nodes...)
	}
	response.Success(c, flat, "success")
}

func tracesHandler(c *gin.Context) {
	limit := 50
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	ts, err := Traces(c.Request.Context(), limit)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, ts, "success")
}

func traceTreeHandler(c *gin.Context) {
	tid := c.Query("trace_id")
	conv := c.Query("conversation_id")
	msg := c.Query("msg_id")
	tree, err := TraceTree(c.Request.Context(), tid, conv, msg)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, tree, "success")
}
