package router

import (
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// setupCRMRoutes CRM 主数据路由（仅映射 + 依赖装配，无内联闭包）。
// 原 RBAC 守卫 RoutePolicyGuard / TenantMiddleware 的挂载顺序不变；
// 数据权限作为 Service 层新过滤叠加，不触碰原授权表。
func setupCRMRoutes(auth *gin.RouterGroup, gormDB *gorm.DB) {
	ctl := controller.NewCRMController()
	// T042：Phase 3 归档入口的负责人推送出口由本装配点注入 LeadNotifyService。
	// 这是 T031 ↔ T041/T042 的接口对齐点：原 CRM 路由不自建通知逻辑。
	ctl.SetClueNotifier(service.NewLeadNotifyService())
	if gormDB != nil {
		ctl.SetProfileService(service.NewCustomer360Service(repository.NewClueRepositoryWithDB(gormDB)))
	}

	g := auth.Group("/manage/crm")
	{
		// 客户主数据 CRUD
		g.GET("/customers", ctl.ListCustomers)
		g.GET("/customers/:id", ctl.GetCustomer)
		g.POST("/customers", ctl.CreateCustomer)
		g.PUT("/customers/:id", ctl.UpdateCustomer)
		g.DELETE("/customers/:id", ctl.DeleteCustomer)

		// 去重与合并审计
		g.POST("/dedupe/scan", ctl.DedupeScan)
		g.POST("/merge", ctl.Merge)
		g.GET("/merge/audit", ctl.MergeAudit)

		// 导入导出
		g.POST("/import", ctl.Import)
		g.GET("/export", ctl.Export)

		// 360 / 跨账号行业交叉检索 / AI
		g.GET("/360/:id", ctl.View360)
		g.GET("/cross-industry", ctl.CrossIndustrySearch)
		g.POST("/ai/analyze", ctl.AIAnalyze)

		// 补缺实体（联系人/供应商/服务商 完整 CRUD）
		g.GET("/contacts", ctl.ListContacts)
		g.POST("/contacts", ctl.CreateContact)
		g.PUT("/contacts/:id", ctl.UpdateContact)
		g.DELETE("/contacts/:id", ctl.DeleteContact)
		g.GET("/suppliers", ctl.ListSuppliers)
		g.POST("/suppliers", ctl.CreateSupplier)
		g.PUT("/suppliers/:id", ctl.UpdateSupplier)
		g.DELETE("/suppliers/:id", ctl.DeleteSupplier)
		g.GET("/service-providers", ctl.ListServiceProviders)
		g.POST("/service-providers", ctl.CreateServiceProvider)
		g.PUT("/service-providers/:id", ctl.UpdateServiceProvider)
		g.DELETE("/service-providers/:id", ctl.DeleteServiceProvider)

		// US2 T041 归档对接（客户侧关联 + 负责人推送，本任务只做桩）
		g.POST("/clue/archive", ctl.ArchiveClue)
	}
}
