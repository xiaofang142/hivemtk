package controller

// 测试装配口：controller 构造器已改为只收 service，测试统一从这里拿真实依赖。
// 生产装配在 internal/router；本文件仅测试编译（_test.go）。
//
// AV-04c：service 包不再持 *gorm.DB 形参（NewXxxDeps(db) 工厂已收口到装配处），
// 测试侧直构 Deps 结构体，句柄只出现在 _test.go。

import (
	contentrepo "hivemtk-user/internal/content/repository"
	"hivemtk-user/internal/content/service"
	"hivemtk-user/internal/pkg/db"
	cdprepo "hivemtk-user/internal/repository"
)

func newTestMarketingFlowController() *MarketingFlowController {
	return NewMarketingFlowController(service.NewMarketingFlowService(service.MarketingFlowDeps{
		FlowRepo:         contentrepo.NewMarketingFlowRepository(db.GetDB()),
		ExecutionRepo:    contentrepo.NewFlowExecutionRepository(db.GetDB()),
		UserTagRepo:      cdprepo.NewUserTagRepositoryWithDB(db.GetDB()),
		OrderRepo:        cdprepo.NewOrderRepositoryWithDB(db.GetDB()),
		ClueRepo:         cdprepo.NewClueRepositoryWithDB(db.GetDB()),
		SessionRepo:      cdprepo.NewCustomerSessionRepositoryWithDB(db.GetDB()),
		AgentRepo:        cdprepo.NewAgentStatusRepositoryWithDB(db.GetDB()),
		OperationLogRepo: cdprepo.NewOperationLogRepositoryWithDB(db.GetDB()),
	}))
}

func newTestAIContentController() *AIContentController {
	return NewAIContentController(
		service.NewAIContentService(service.AIContentDeps{
			RecordRepo:   contentrepo.NewAIGenerationRecordRepository(db.GetDB()),
			TemplateRepo: contentrepo.NewPromptTemplateRepository(db.GetDB()),
		}),
		service.NewPromptTemplateService(service.PromptTemplateDeps{
			TemplateRepo: contentrepo.NewPromptTemplateRepository(db.GetDB()),
		}),
	)
}

func newTestTemplateMarketController() *TemplateMarketController {
	return NewTemplateMarketController(service.NewTemplateMarketService(service.TemplateMarketDeps{
		TemplateRepo: contentrepo.NewMarketTemplateRepository(db.GetDB()),
		DownloadRepo: contentrepo.NewMarketTemplateDownloadRepository(db.GetDB()),
	}))
}

func newTestScriptTemplateController() *ScriptTemplateController {
	return NewScriptTemplateController(
		service.NewScriptTemplateService(service.ScriptTemplateDeps{
			TemplateRepo: contentrepo.NewScriptTemplateRepository(db.GetDB()),
			CategoryRepo: contentrepo.NewScriptCategoryRepository(db.GetDB()),
		}),
		service.NewScriptTemplateSyncService(service.ScriptTemplateSyncDeps{
			TemplateRepo: contentrepo.NewScriptTemplateRepository(db.GetDB()),
		}),
	)
}

func newTestBatchOperationController() *BatchOperationController {
	return NewBatchOperationController(service.NewBatchOperationService(service.BatchOperationDeps{
		BatchOpRepo: contentrepo.NewBatchOperationRepository(db.GetDB()),
		ClueRepo:    cdprepo.NewClueRepositoryWithDB(db.GetDB()),
		UserRepo:    cdprepo.NewUserRepositoryWithDB(db.GetDB()),
	}))
}
