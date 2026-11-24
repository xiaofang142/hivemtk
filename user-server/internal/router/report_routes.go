// report_routes.go LLM 观测报表路由（T3）。
//
// 设计文档：docs/architecture/design/T3-observability-reports.md
// 读侧走 auth 组（登录态），租户维度由 GORM 隔离 callback 强制（表带 tenant_id 列）。
// 聚合定时任务：每 5 分钟增量（窗口 10 分钟覆盖乱序），每天 00:10 重算前一天；
// advisory lock + 幂等 upsert 保证多实例安全。
package router

import (
	"context"

	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/pkg/cron"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupReportRoutes LLM 报表读侧 + 聚合任务注册
func SetupReportRoutes(auth *gin.RouterGroup, gormDB *gorm.DB) {
	reportSvc := service.NewReportService(repository.NewLLMUsageReportRepository(gormDB))
	reportCtrl := controller.NewReportController(reportSvc)

	auth.GET("/reports/llm/overview", reportCtrl.GetOverview)
	auth.GET("/reports/llm/timeseries", reportCtrl.GetTimeseries)
	auth.GET("/reports/llm/breakdown", reportCtrl.GetBreakdown)
	auth.GET("/reports/llm/errors", reportCtrl.GetErrors)

	// 聚合任务（TaskManager 自带 SkipIfStillRunning + Recover；advisory lock 防多实例）
	// FA-REPORT-01：后台定时扫描须以 system scope 运行（覆盖所有租户，不能在 on 模式
	// 下被租户隔离挡住），且失败必须告警——不再静默丢弃退出码。
	mgr := cron.GetTaskManager()
	scanCtx := tenant.WithSystemScope(context.Background())
	runAgg := func(name string, fn func(context.Context) error) {
		if err := fn(scanCtx); err != nil {
			logger.Errorf("[Report] %s failed: %v", name, err)
		}
	}
	if _, err := mgr.AddTask("0 */5 * * * *", func() {
		runAgg("AggregateRecent", reportSvc.AggregateRecent)
	}); err != nil {
		logger.Errorf("[Report] register AggregateRecent task failed: %v", err)
	}
	if _, err := mgr.AddTask("0 10 0 * * *", func() {
		runAgg("RunDailyRecalc", reportSvc.RunDailyRecalc)
	}); err != nil {
		logger.Errorf("[Report] register RunDailyRecalc task failed: %v", err)
	}
}
