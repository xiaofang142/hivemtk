package app

import (
	"context"
	"os"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	knowledgesvc "hivemtk-user/internal/aiagent/knowledge/service"
	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/bridge"
	contentservice "hivemtk-user/internal/content/service"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
	"hivemtk-user/internal/service/reflux"
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

	memorySvc := service.NewDialogueMemoryService(dispatcher)

	intentSvc := service.GetIntentRecognizer()
	if intentSvc == nil {
		intentSvc = service.NewIntentRecognizer(dispatcher, nil)
	}

	sopSvc := service.NewSOPService(dispatcher)

	ragSearcher := knowledgesvc.NewRagSearcher()

	scriptLookup := service.NewScriptLookupAdapter(contentservice.NewScriptTemplateService(contentservice.NewScriptTemplateDeps(gormDB)))
	customerLookup := service.NewCustomerLookupAdapter(repository.NewCustomerRepository())

	engine := service.NewSalesEngine(
		dispatcher,
		intentSvc,
		memorySvc,
		sopSvc,
		ragSearcher,
		scriptLookup,
		customerLookup,
	)

	engine.SetFeedbackLearner(context.Background(), service.NewFeedbackLearner(repository.NewFeedbackRecordRepository(gormDB)))

	// FAQ 快速通道：命中标准答案就直接作答（零 LLM、零 Token、答案确定）。
	//
	// 为什么必须在这里注入：官网网页客服（HTTP /api/chat/public/*）走的正是
	// SalesEngine.Handle，而它早先从不查 FAQ —— FAQ 里明明有「HiveMTK 是什么？」
	// 这类标准答案，却因为整条 FAQ 通道没装配而一次都没被用上；问题只能落到
	// RAG 弱召回上 → 置信度不足 → 转人工 → 「暂无坐席在线」。
	faqSvc := service.NewFAQService()
	engine.SetFAQService(faqSvc)
	logger.Info("[agent] ✅ SalesEngine 已注入 FAQ 快速通道（命中标准答案直答）")

	confidenceAgg := service.GetConfidenceAggregator()
	if confidenceAgg == nil {
		confidenceAgg = service.InitConfidenceAggregator(repository.NewThresholdPolicyRepositoryWithDB(gormDB), repository.NewConfidenceSignalRepositoryWithDB(gormDB), repository.NewConfidenceCalibrationRepositoryWithDB(gormDB), service.NewLocalConfidenceEmbedder())
	}

	engine.SetConfidenceAggregator(context.Background(), confidenceAgg)

	confidenceAgg.StartConformalBackground(context.Background())

	llm.SetFanoutVoteEnabledGetter(func() bool {
		return service.GlobalConfigParam().GetBool(context.Background(), "agent_llm", "fanout_vote_enabled", false)
	})

	humanizeSvc := service.GetHumanizeEvalService()
	if humanizeSvc == nil {
		humanizeSvc = service.InitHumanizeEvalService(dispatcher)
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

	// 术语表缓存走**全局缓存**（cache 传 nil ⇒ NewGlossaryService 回退
	// cache.GetGlobalCache()），与 router/system_routes.go 里那个 controller
	// 侧的实例用同一个后端。
	//
	// 为什么不能自己 new 一个内存缓存：术语 CRUD 之后由 controller 侧那个实例
	// 失效缓存，而真正读缓存的是这里的 SalesEngine（LLM 输出后置校准）。
	// 两边不同后端 ⇒ 管理端改完术语，LLM 那条链仍读旧值，且没有任何报错。
	// 另一个理由是租户维度：缓存键现在带租户号，而租户号来自请求 ctx，
	// 两侧必须共用同一份键空间才能互相失效。
	glossarySvc := translation.NewGlossaryService(repository.NewGlossaryRepositoryWithDB(db.GetDB()), nil)
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
	o.SetSuggestionRepo(repository.NewAISuggestionRepositoryWithDB(gormDB))
	o.SetIdentityService(service.NewCustomerIdentityService())

	o.SetConfidenceAggregator(engine.ConfidenceAggregator())

	if gormDB != nil {
		// T1 低置信回流：异步收集器（有界队列 + 灰度开关 REFLUX_ENABLED，默认关）。
		// 归簇器随装配注入；采集失败只记日志，绝不影响问答主链路。
		refluxEnabled := os.Getenv("REFLUX_ENABLED") == "true"
		refluxRepo := repository.NewRefluxRepository(gormDB)
		collector := reflux.NewCollector(refluxRepo, 512, refluxEnabled)
		collector.SetClusterer(reflux.NewClusterer(refluxRepo))
		o.SetRefluxCollector(collector)

		dncRepo := repository.NewCustomerDoNotContactRepository(gormDB)
		dncSvc := service.NewDoNotContactService(dncRepo)
		o.SetDNCChecker(dncSvc)
	}

	// 订单草稿生产者：运行时由 router.Setup 里的 InitOrderDraftRuntime 装配，
	// 这里只负责"挂上"。拿不到运行时（旗子 FF_LTC_ORDER_DRAFT_DB=off）时什么都不挂：
	// 编排器的生产者字段保持零值 nil，HandleIncomingWithAgent 尾部那条 `!= nil` 分支
	// 因此根本不进（由 TestRunOrderDraftProduce_NilProducerIsNoop 锁定）。
	attachOrderDraftProducer(o, currentOrderDraftRuntime())

	// 会话人工待办生产者：全局底座由 router.Setup 里的 InitHumanTaskRuntime
	// 装配，这里只负责挂。没装配（拿不到 DB 句柄）时挂的是 nil，与"本处之前"逐字一致。
	attachHumanTaskProducer(o)

	// Bad Case 自动标记器：全局底座同样由 router.Setup 里的 InitBadCaseRuntime
	// 装配，这里只负责挂。没装配时挂的是 nil ⇒ 低质回答不留痕，回答路径与本处之前逐字一致。
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
