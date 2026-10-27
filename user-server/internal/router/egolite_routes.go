package router

import (
	"context"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"hivemtk-user/internal/egolite"
	hrepo "hivemtk-user/internal/repository"
)

// SetupEgoLiteRoutes ego-lite 腿路由（设计文档 S3/S6/S7/S9；调研 §6.5 鉴权定案）。
//
// 端点总表：
//   - GET  /api/egolite/runner-ws          runner WS（eg_ token，免 JWT）
//   - GET  /api/egolite/runner/status      连接状态（用户看自己 / admin 全量）
//   - POST /api/egolite/runner/token/reset eg_ token 轮换（admin）
//   - POST /api/egolite/flows              保存草稿（保存即扫描）
//   - GET  /api/egolite/flows              流程列表
//   - GET  /api/egolite/flows/:id          流程详情（含扫描报告）
//   - POST /api/egolite/flows/:id/rescan   重新扫描
//   - POST /api/egolite/flows/:id/publish  发布版本
//   - POST /api/egolite/flows/:id/submit   提交平台审核
//   - POST /api/egolite/executions         下发执行（draft=flow 试跑 / market=资产执行）
//   - GET  /api/egolite/executions         执行列表
//   - GET  /api/egolite/executions/:id     执行详情
//   - GET  /api/egolite/executions/:id/events 事件日志（Monitor 页面数据源，T4）
//   - POST /api/egolite/executions/:id/abort   停止
//   - POST /api/egolite/executions/:id/confirm 用户确认/拒绝（写操作闸）
//
// 后台任务：确认 TTL + 硬超时看门狗（60s 扫描周期）。
func SetupEgoLiteRoutes(auth *gin.RouterGroup, engine *gin.Engine, gormDB *gorm.DB) {
	kvRepo := hrepo.NewPlatformConfigKVRepositoryWithDB(gormDB)
	registry := egolite.NewEgoRunnerRegistry()

	execRepo := egolite.NewEgoExecutionRepositoryWithDB(gormDB)
	evRepo := egolite.NewEgoEventRepositoryWithDB(gormDB)
	flowRepo := egolite.NewEgoFlowRepositoryWithDB(gormDB)

	execSvc := egolite.NewEgoExecutionService(execRepo, evRepo, flowRepo, registry)
	flowSvc := egolite.NewEgoFlowService(flowRepo, execSvc)

	// 断连钩子：该用户全部非终态执行判 failed（FailActiveByUser）。
	// 独立实现实例——绝不复用 host-ws 的 FailRunningByUser（那是 browser_sessions 语义）。
	registry.SetDisconnectHook(func(ctx context.Context, userID uint) {
		execSvc.OnRunnerDisconnect(ctx, userID)
	})

	// 启动兜底：KV 无 eg_ token 则自动生成（fail-closed 不卡死首次部署，host-ws 先例）
	egolite.EnsureEgoTokenExists(context.Background(), kvRepo)

	// runner WS（不过 JWT——token 即身份，host-ws 同形状）
	wsHandler := egolite.NewEgoRunnerWSHandler(registry, kvRepo)
	wsHandler.AttachEventSink(execSvc)
	engine.GET("/api/egolite/runner-ws", wsHandler.Handle)

	// 看门狗（60s 扫描：confirm TTL + 硬超时；WatchdogSweep 幂等，扫不到就空转）
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			_, _ = execSvc.WatchdogSweep(ctx)
			cancel()
		}
	}()

	eg := auth.Group("/egolite")
	{
		// ---- runner 通道 ----
		eg.GET("/runner/status", func(c *gin.Context) {
			userID := currentUID(c)
			isAdmin := c.GetString("role") == "admin"
			response2(c, registry.StatusFor(userID, isAdmin), "ok")
		})
		eg.POST("/runner/token/reset", func(c *gin.Context) {
			if c.GetString("role") != "admin" {
				c.JSON(403, gin.H{"code": 1, "msg": "仅管理员可轮换 runner token"})
				return
			}
			tok, err := egolite.RotateEgoToken(c.Request.Context(), kvRepo, 0)
			if err != nil {
				c.JSON(500, gin.H{"code": 1, "msg": "轮换失败: " + err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": gin.H{"token": tok},
				"msg": "已重置；旧 token 仍可用，直到下一次重置"})
		})

		// ---- 流程域 ----
		eg.POST("/flows", func(c *gin.Context) {
			var req egolite.SaveRequest
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(400, gin.H{"code": 1, "msg": err.Error()})
				return
			}
			flowID := uint(0)
			if v := c.Query("id"); v != "" {
				if n, err := strconv.ParseUint(v, 10, 32); err == nil {
					flowID = uint(n)
				}
			}
			f, report, err := flowSvc.Save(c.Request.Context(), currentUID(c), flowID, req)
			if err != nil {
				c.JSON(400, gin.H{"code": 1, "msg": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": gin.H{"flow": f, "scan_report": report}})
		})
		eg.GET("/flows", func(c *gin.Context) {
			page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
			size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
			list, total, err := flowRepo.ListByUser(c.Request.Context(), currentUID(c), page, size)
			if err != nil {
				c.JSON(500, gin.H{"code": 1, "msg": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": gin.H{"list": list, "total": total}})
		})
		eg.GET("/flows/:id", func(c *gin.Context) {
			id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
			f, err := flowRepo.GetForUser(c.Request.Context(), uint(id), currentUID(c))
			if err != nil {
				c.JSON(404, gin.H{"code": 1, "msg": "流程不存在"})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": f})
		})
		eg.POST("/flows/:id/rescan", func(c *gin.Context) {
			id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
			report, err := flowSvc.Rescan(c.Request.Context(), currentUID(c), uint(id))
			if err != nil {
				c.JSON(400, gin.H{"code": 1, "msg": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": report})
		})
		eg.POST("/flows/:id/publish", func(c *gin.Context) {
			id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
			f, err := flowSvc.Publish(c.Request.Context(), currentUID(c), uint(id))
			if err != nil {
				c.JSON(400, gin.H{"code": 1, "msg": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": f})
		})
		eg.POST("/flows/:id/submit", func(c *gin.Context) {
			id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
			f, err := flowSvc.Submit(c.Request.Context(), currentUID(c), uint(id))
			if err != nil {
				c.JSON(400, gin.H{"code": 1, "msg": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": f})
		})

		// ---- 执行域 ----
		eg.POST("/executions", func(c *gin.Context) {
			var req egolite.StartRequest
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(400, gin.H{"code": 1, "msg": err.Error()})
				return
			}
			e, err := execSvc.Start(c.Request.Context(), currentUID(c), req)
			if err != nil {
				status := 400
				if err == egolite.ErrConcurrencyGate {
					status = 429
				}
				c.JSON(status, gin.H{"code": 1, "msg": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": e})
		})
		eg.GET("/executions", func(c *gin.Context) {
			page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
			size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
			list, total, err := execRepo.ListByUser(c.Request.Context(), currentUID(c), page, size)
			if err != nil {
				c.JSON(500, gin.H{"code": 1, "msg": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": gin.H{"list": list, "total": total}})
		})
		eg.GET("/executions/:id", func(c *gin.Context) {
			id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
			e, err := execRepo.GetForUser(c.Request.Context(), uint(id), currentUID(c))
			if err != nil {
				c.JSON(404, gin.H{"code": 1, "msg": "执行不存在"})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": e})
		})
		eg.GET("/executions/:id/events", func(c *gin.Context) {
			id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
			// 归属校验先于读事件（越权与不存在同响应）
			if _, err := execRepo.GetForUser(c.Request.Context(), uint(id), currentUID(c)); err != nil {
				c.JSON(404, gin.H{"code": 1, "msg": "执行不存在"})
				return
			}
			limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
			events, err := evRepo.ListByExecution(c.Request.Context(), uint(id), limit)
			if err != nil {
				c.JSON(500, gin.H{"code": 1, "msg": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": gin.H{"list": events}})
		})
		eg.POST("/executions/:id/abort", func(c *gin.Context) {
			id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
			e, err := execSvc.Abort(c.Request.Context(), currentUID(c), uint(id), "用户停止")
			if err != nil {
				status := 400
				if err == egolite.ErrExecStateConflict {
					status = 409
				}
				c.JSON(status, gin.H{"code": 1, "msg": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": e})
		})
		eg.POST("/executions/:id/confirm", func(c *gin.Context) {
			id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
			var body struct {
				Allow bool `json:"allow"`
			}
			_ = c.ShouldBindJSON(&body)
			e, err := execSvc.Confirm(c.Request.Context(), currentUID(c), uint(id), body.Allow)
			if err != nil {
				status := 400
				if err == egolite.ErrExecStateConflict {
					status = 409
				}
				c.JSON(status, gin.H{"code": 1, "msg": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": e})
		})
	}
}

// currentUID 从 JWT 注入的 claims 取 user_id（host.go GetStatus 同取法 c.GetUint）
func currentUID(c *gin.Context) uint {
	return c.GetUint("user_id")
}

// response2 统一响应形状（response.Success 别名，避免 import 环）
func response2(c *gin.Context, data any, msg string) {
	c.JSON(200, gin.H{"code": 0, "message": msg, "data": data})
}

