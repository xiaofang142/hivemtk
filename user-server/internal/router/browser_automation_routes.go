package router

import (
	"context"

	bactrl "hivemtk-user/internal/browser_automation/controller"
	_ "hivemtk-user/internal/browser_automation/platform/douyin"      // 平台适配器 init() 自注册（L3 注册表）
	_ "hivemtk-user/internal/browser_automation/platform/xiaohongshu" // 平台适配器 init() 自注册（L3 注册表）
	_ "hivemtk-user/internal/browser_automation/platform/xianyu"      // 平台适配器 init() 自注册（L3 注册表）
	barepo "hivemtk-user/internal/browser_automation/repository"
	basvc "hivemtk-user/internal/browser_automation/service"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	hrepo "hivemtk-user/internal/repository"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupBrowserAutomationRoutes 浏览器自动化路由（寄生式 Chrome Native Messaging）。
// 业务路由挂 auth（JWT）；Host WS 挂 engine（自带 token + 本地回环 IP 双层防护，不走 JWT）。
func SetupBrowserAutomationRoutes(auth *gin.RouterGroup, engine *gin.Engine, gormDB *gorm.DB) {
	// --- Repository ---
	taskRepo := barepo.NewBrowserTaskRepositoryWithDB(gormDB)
	sessionRepo := barepo.NewBrowserSessionRepositoryWithDB(gormDB)
	stepRepo := barepo.NewBrowserStepRepositoryWithDB(gormDB)
	cronRepo := barepo.NewBrowserCronTriggerRepositoryWithDB(gormDB)
	planRepo := barepo.NewBrowserLLMPlanRepositoryWithDB(gormDB)
	cmdLogRepo := barepo.NewBrowserCommandLogRepositoryWithDB(gormDB)
	kvRepo := hrepo.NewSystemConfigKVRepository()

	// --- Service（进程级单例：registry / hand）---
	registry := basvc.NewHostRegistry()
	hand := basvc.NewHand(registry)
	brainSvc := basvc.NewBrainService(planRepo)
	feedbackSvc := basvc.NewFeedbackService(sessionRepo, taskRepo)
	executor := basvc.NewExecutor(hand, sessionRepo, stepRepo, brainSvc, feedbackSvc)
	executor.SetCommandLogRepository(cmdLogRepo)
	taskSvc := basvc.NewTaskService(taskRepo, sessionRepo, executor)
	sessionSvc := basvc.NewSessionService(sessionRepo, stepRepo, executor)
	cronSvc := basvc.NewCronService(cronRepo, taskRepo, taskSvc)

	// 失败自动重试装配：FeedbackService → TaskService.RunTaskWithRetry（进程级一次性注入）
	basvc.SetRetryRunner(taskSvc.RunTaskWithRetry)

	// Host 断连清理钩子：该用户所有 running session 置 failed
	registry.SetDisconnectHook(func(ctx context.Context, userID uint) {
		sessions, err := sessionRepo.FailRunningByUser(ctx, userID, "Host 掉线（Chrome 断开或扩展重载）")
		if err != nil {
			logger.Warnf("[BrowserHost] 断连清理失败 user=%d: %v", userID, err)
			return
		}
		if len(sessions) > 0 {
			logger.Infof("[BrowserHost] 断连清理完成 user=%d failed_sessions=%d", userID, len(sessions))
		}
	})

	// --- Controller ---
	taskCtrl := bactrl.NewTaskController(taskSvc)
	sessionCtrl := bactrl.NewSessionController(sessionSvc)
	cronCtrl := bactrl.NewCronController(cronSvc)
	hostCtrl := bactrl.NewHostController(registry, kvRepo)
	platformCtrl := bactrl.NewPlatformController()

	// --- 业务路由 ---
	ba := auth.Group("/browser-automation")

	// 任务
	ba.POST("/tasks", taskCtrl.Create)
	ba.GET("/tasks", taskCtrl.List)
	ba.GET("/tasks/:id", taskCtrl.Get)
	ba.PUT("/tasks/:id", taskCtrl.Update)
	ba.DELETE("/tasks/:id", taskCtrl.Delete)
	ba.POST("/tasks/:id/publish", taskCtrl.Publish)
	ba.POST("/tasks/:id/run", taskCtrl.Run) // 异步，立即返回 session_id
	ba.POST("/tasks/:id/pause", taskCtrl.Pause)
	ba.POST("/tasks/:id/resume", taskCtrl.Resume)
	ba.POST("/tasks/:id/archive", taskCtrl.Archive)
	ba.PUT("/tasks/:id/dependency", taskCtrl.SetDependency)

	// Session（查询 + 中断）
	ba.GET("/sessions", sessionCtrl.List)
	ba.GET("/sessions/:id", sessionCtrl.Get)
	ba.GET("/sessions/:id/steps", sessionCtrl.ListSteps)
	ba.GET("/tasks/:id/sessions", sessionCtrl.ListByTask)
	ba.POST("/sessions/:id/stop", sessionCtrl.Stop)

	// Cron
	ba.GET("/cron", cronCtrl.List)
	ba.POST("/cron", cronCtrl.Create)
	ba.PUT("/cron/:id", cronCtrl.Update)
	ba.DELETE("/cron/:id", cronCtrl.Delete)
	ba.POST("/cron/:id/enable", cronCtrl.Enable)
	ba.POST("/cron/:id/disable", cronCtrl.Disable)

	// Host 状态：所有登录用户可读（普通用户只读自己，admin 全量——controller 内分流）
	ba.GET("/host/status", hostCtrl.GetStatus)

	// 平台注册表（L3）：前端平台选择器/能力矩阵/编排预设
	ba.GET("/platforms", platformCtrl.List)
	ba.GET("/platforms/:id/locators", platformCtrl.Locators)

	// Admin 专用
	baAdmin := ba.Group("")
	baAdmin.Use(middleware.AdminAuthMiddleware())
	baAdmin.POST("/host/token/reset", hostCtrl.ResetToken)

	// --- Host WebSocket（双层防护：token + 本地回环 IP；fail-closed）---
	engine.GET("/api/browser/host-ws", bactrl.NewHostWSHandler(registry, kvRepo).Handle)

	// 进程启动后台任务：恢复已启用触发器 + 保证存在 Host token
	ctx := context.Background()
	utils.SafeGo(ctx, "browser_automation.bootstrap", func(ctx context.Context) {
		basvc.EnsureHostTokenExists(ctx, kvRepo)
		cronSvc.RestoreAll(ctx)
	})
}
