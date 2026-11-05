package controller

// 测试装配口：controller 构造器已改为只收 service，测试统一从这里拿真实依赖。
// 生产装配在 internal/router；本文件仅测试编译（_test.go）。

import (
	"hivemtk-user/internal/content/service"
	"hivemtk-user/internal/pkg/db"
)

func newTestMarketingFlowController() *MarketingFlowController {
	return NewMarketingFlowController(service.NewMarketingFlowService(service.NewMarketingFlowDeps(db.GetDB())))
}

func newTestAIContentController() *AIContentController {
	return NewAIContentController(
		service.NewAIContentService(service.NewAIContentDeps(db.GetDB())),
		service.NewPromptTemplateService(service.NewPromptTemplateDeps(db.GetDB())),
	)
}

func newTestTemplateMarketController() *TemplateMarketController {
	return NewTemplateMarketController(service.NewTemplateMarketService(service.NewTemplateMarketDeps(db.GetDB())))
}

func newTestScriptTemplateController() *ScriptTemplateController {
	return NewScriptTemplateController(
		service.NewScriptTemplateService(service.NewScriptTemplateDeps(db.GetDB())),
		service.NewScriptTemplateSyncService(service.NewScriptTemplateSyncDeps(db.GetDB())),
	)
}

func newTestBatchOperationController() *BatchOperationController {
	return NewBatchOperationController(service.NewBatchOperationService(service.NewBatchOperationDeps(db.GetDB())))
}
