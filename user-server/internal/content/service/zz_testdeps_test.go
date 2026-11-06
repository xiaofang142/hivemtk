package service

// 测试装配口（仅测试编译，不进生产扫描集）。
//
// AV-04c：service 生产文件不再持 *gorm.DB 形参——原 NewXxxDeps(db) 装配工厂
// 收口到装配处（internal/router、internal/app、cmd/syncscripts 直构 Deps 结构体）。
// 本文件用真实 DB 句柄在测试侧构造同一批依赖包，句柄只出现在 _test.go；
// 生产对应物见 router/frontend_aliases.go、router/business_routes.go、
// router/content_routes.go、app/sales_engine_factory.go、cmd/syncscripts/main.go。
//
// 本文件仅测试编译（_test.go），与 controller 包 zz_testhelpers_test.go 同一惯例。

import (
	contentrepo "hivemtk-user/internal/content/repository"
	cdprepo "hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

func newAIContentDeps(db *gorm.DB) AIContentDeps {
	return AIContentDeps{
		RecordRepo:   contentrepo.NewAIGenerationRecordRepository(db),
		TemplateRepo: contentrepo.NewPromptTemplateRepository(db),
	}
}

func newPromptTemplateDeps(db *gorm.DB) PromptTemplateDeps {
	return PromptTemplateDeps{TemplateRepo: contentrepo.NewPromptTemplateRepository(db)}
}

func newBatchOperationDeps(db *gorm.DB) BatchOperationDeps {
	return BatchOperationDeps{
		BatchOpRepo: contentrepo.NewBatchOperationRepository(db),
		ClueRepo:    cdprepo.NewClueRepositoryWithDB(db),
		UserRepo:    cdprepo.NewUserRepositoryWithDB(db),
	}
}

func newMarketingFlowDeps(db *gorm.DB) MarketingFlowDeps {
	return MarketingFlowDeps{
		FlowRepo:         contentrepo.NewMarketingFlowRepository(db),
		ExecutionRepo:    contentrepo.NewFlowExecutionRepository(db),
		UserTagRepo:      cdprepo.NewUserTagRepositoryWithDB(db),
		OrderRepo:        cdprepo.NewOrderRepositoryWithDB(db),
		ClueRepo:         cdprepo.NewClueRepositoryWithDB(db),
		SessionRepo:      cdprepo.NewCustomerSessionRepositoryWithDB(db),
		AgentRepo:        cdprepo.NewAgentStatusRepositoryWithDB(db),
		OperationLogRepo: cdprepo.NewOperationLogRepositoryWithDB(db),
	}
}

func newMaterialDeps(db *gorm.DB) MaterialDeps {
	return MaterialDeps{
		CategoryRepo: contentrepo.NewMaterialCategoryRepository(db),
		MaterialRepo: contentrepo.NewMaterialRepository(db),
		ObsService:   NewObsConfigServiceAdapter(cdprepo.NewObsConfigRepositoryWithDB(db)),
	}
}

func newTemplateMarketDeps(db *gorm.DB) TemplateMarketDeps {
	return TemplateMarketDeps{
		TemplateRepo: contentrepo.NewMarketTemplateRepository(db),
		DownloadRepo: contentrepo.NewMarketTemplateDownloadRepository(db),
	}
}
