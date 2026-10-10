package app

import (
	"context"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	knowledgesvc "hivemtk-user/internal/aiagent/knowledge/service"
	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/bridge"
	"hivemtk-user/internal/cache"
	contentservice "hivemtk-user/internal/content/service"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
	"hivemtk-user/internal/service/translation"

	"gorm.io/gorm"
)

// buildSalesEngine 构建智能体销冠引擎（真实依赖注入）
// 调用方：router.Setup()
//
// 意图识别实例统一复用全局单例（main.go 中 InitIntentRecognizer 初始化），
// 与 /api/intent/* 直连路由共享：
//   - 同一份 dispatcher / db / cache
//   - 同一份 SOP service 联动
//   - 同一份 IntentEnabled 开关
//
// 避免双实例导致 SOP 联动只在直连路由生效、销售引擎调用不生效的分裂问题。
func BuildSalesEngine(gormDB *gorm.DB) *service.SalesEngine {
	dispatcher := llm.GetGlobalDispatcher()

	memorySvc := service.NewDialogueMemoryService(gormDB, dispatcher)

	intentSvc := service.GetIntentRecognizer()
	if intentSvc == nil {
		intentSvc = service.NewIntentRecognizer(gormDB, dispatcher, nil)
	}

	sopSvc := service.NewSOPService(gormDB, dispatcher)

	ragSearcher := knowledgesvc.NewRagSearcher()

	scriptLookup := service.NewScriptLookupAdapter(contentservice.NewScriptTemplateService())
	customerLookup := service.NewCustomerLookupAdapter(repository.NewCustomerRepository())

	engine := service.NewSalesEngine(
		db.GetDB(),
		dispatcher,
		intentSvc,
		memorySvc,
		sopSvc,
		ragSearcher,
		scriptLookup,
		customerLookup,
	)

	engine.SetFeedbackLearner(context.Background(), service.NewFeedbackLearner(gormDB))

	confidenceAgg := service.GetConfidenceAggregator()
	if confidenceAgg == nil {
		confidenceAgg = service.InitConfidenceAggregator(gormDB, service.NewLocalConfidenceEmbedder())
	}

	engine.SetConfidenceAggregator(context.Background(), confidenceAgg)

	confidenceAgg.StartConformalBackground(context.Background())

	llm.SetFanoutVoteEnabledGetter(func() bool {
		return service.GlobalConfigParam().GetBool(context.Background(), "agent_llm", "fanout_vote_enabled", false)
	})

	humanizeSvc := service.GetHumanizeEvalService()
	if humanizeSvc == nil {
		humanizeSvc = service.InitHumanizeEvalService(gormDB, dispatcher)
	}
	engine.SetHumanizeEvaluator(context.Background(), humanizeSvc)
	service.SetHumanizeRegenerateDispatcher(dispatcher)

	toolExec := tooluse.GetGlobalExecutor()
	if toolExec != nil {
		engine.SetToolExecutor(context.Background(), NewToolExecutorAdapter(toolExec))
		logger.Info("[agent] ✅ SalesEngine 已注入 ToolExecutor（Agent Loop 已启用）")
	} else {
		logger.Warn("[agent] SalesEngine 未注入 ToolExecutor（globalExecutor 未初始化，走原 9 步流水线）")
	}

	if pc := GetGlobalPermissionChecker(); pc != nil {
		engine.SetPermissionChecker(pc)
		logger.Info("[agent] ✅ SalesEngine 已注入工具权限检查器（按 Agent 白名单执行期放行）")
	}

	glossarySvc := translation.NewGlossaryService(repository.NewGlossaryRepositoryWithDB(db.GetDB()), cache.NewMemoryCache())
	engine.SetGlossaryRenderer(glossarySvc)
	engine.SetOutputCalibrator(glossarySvc)
	logger.Info("[agent] ✅ SalesEngine 已注入回复语言链路（术语表 + 后置校准）")

	return engine
}

// buildSmartOrchestrator 构建智能体统一编排器
// 调用方：router.Setup()
// 设计：以 SalesEngine 为核心，套一层 SmartCSOrchestrator 编排壳，实现
//
//	"LLM 能力 + 智能体 协作体"：高置信度自动回复，低置信度转人工 + 推送建议
//	座席可随时接管智能体会话（人机协同）
//
// 配置：使用 DefaultOrchestratorConfig（置信度 0.7 / 自动回复开 / 自动连续上限 10）
//
// 调优记录：9B 4-bit 在 RAG 短问答上 confidence 评估均值 ~0.55-0.65，
// 默认 0.7 阈值导致 80% 业务问答被判定为"低置信度"转人工。降到 0.5 让 AI 接管更多场景。
func BuildSmartOrchestrator(engine *service.SalesEngine, kbRepo *repository.KnowledgeBaseRepository, gormDB *gorm.DB) *service.SmartCSOrchestrator {
	cfg := service.DefaultOrchestratorConfig()
	cfg.ConfidenceThreshold = 0.5
	o := service.NewSmartCSOrchestrator(engine, cfg, kbRepo)
	o.SetIdentityService(service.NewCustomerIdentityService())

	o.SetConfidenceAggregator(engine.ConfidenceAggregator())

	if gormDB != nil {
		dncRepo := repository.NewCustomerDoNotContactRepository(gormDB)
		dncSvc := service.NewDoNotContactService(dncRepo)
		o.SetDNCChecker(dncSvc)
	}

	// 订单草稿生产者（T-P2-06）：运行时由 router.Setup 里的 InitOrderDraftRuntime 装配，
	// 这里只负责"挂上"。拿不到运行时（旗子 FF_LTC_ORDER_DRAFT_DB=off）时什么都不挂：
	// 编排器的生产者字段保持零值 nil，HandleIncomingWithAgent 尾部那条 `!= nil` 分支
	// 因此根本不进（由 TestRunOrderDraftProduce_NilProducerIsNoop 锁定）。
	attachOrderDraftProducer(o, currentOrderDraftRuntime())

	// 会话人工待办生产者（T-P3-03）：全局底座由 router.Setup 里的 InitHumanTaskRuntime
	// 装配，这里只负责挂。没装配（拿不到 DB 句柄）时挂的是 nil，与"本卡之前"逐字一致。
	attachHumanTaskProducer(o)

	// Bad Case 自动标记器（T-P8-03）：全局底座同样由 router.Setup 里的 InitBadCaseRuntime
	// 装配，这里只负责挂。没装配时挂的是 nil ⇒ 低质回答不留痕，回答路径与本卡之前逐字一致。
	attachBadCaseMarker(o)

	// 语义答案缓存：四样零件都在，缺的一直是装配（详见 faq_cache_wiring.go）。
	// 参数 cache.faq_answer_enabled 默认 false，关着时不挂 ⇒ 与挂载前逐字一致。
	attachFAQAnswerCache(o, gormDB)
	return o
}

func RegisterAgentReachTools(gormDB *gorm.DB) {
	adapter := bridge.NewBridgeReachAdapter(NewIntegrationReachAdapterFromDB(gormDB), GetBridgeIngressSvc())
	deps := NewReachToolDepsWithAdapter(gormDB, adapter)

	if err := tooluse.RegisterReachTools(tooluse.GetGlobalRegistry(), deps); err != nil {
		logger.Errorf("[agent] 注册触达工具失败（reach.web.send 等将不可用）：%v", err)
		return
	}
	logger.Info("[agent] ✅ 触达工具（含 reach.web.send 网页客服）已真实接入全局注册中心")
}

// 私信工具（pm.session.open/read/message.send）不在这里注册：
// 生产装配走 app.PrivateMessageToolProvider.Provide()（tool_provider_wiring.go），
// 由 registerAllAgentToolsViaProviders 统一批量注册。本文件里曾有一个
// RegisterAgentPrivateMessageTools 做同一件事并在注释里自称"调用方：router.Setup()"，
// 实测全仓（含测试）零调用点，已删；留着只会让人以为 pm.* 有两条装配路径。
