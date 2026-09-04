package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	agent_runtime "hivemtk-user/internal/aiagent/agent/runtime"
	"hivemtk-user/internal/aiagent/llm"
	rag "hivemtk-user/internal/aiagent/rag/incremental"
	"hivemtk-user/internal/app"
	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/config"
	"hivemtk-user/internal/event"
	georepo "hivemtk-user/internal/geo/repository"
	geoservice "hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/migration/migrations"
	cronpkg "hivemtk-user/internal/pkg/cron"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/featureflag"
	"hivemtk-user/internal/pkg/tracing"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/platform"
	"hivemtk-user/internal/router"
	"hivemtk-user/internal/secrets"
	"hivemtk-user/internal/security"
	"hivemtk-user/internal/service"
	"hivemtk-user/internal/service/trace_learning"
	"hivemtk-user/internal/system/install"
	"hivemtk-user/internal/websocket"
)

// 端口兜底常量（单源：config 包的 ports.go / DEVELOPMENT.md §2.4 端口对照表）
// 这里仅做别名 re-export，便于直接通过 main.DefaultListenPort 引用而不必 import config
// 但任何调整必须改 config.DefaultListenPort / config.DefaultRedisPort。
const (
	DefaultListenPort = config.DefaultListenPort

	DefaultRedisPort = config.DefaultRedisPort
)

// buildRedisClient 依据环境变量构建 Redis 客户端。
// 仅当 REDIS_HOST 显式配置时返回非 nil；否则返回 nil，
// 此时保持进程内幂等守卫、健康检查 redis 显示 not_configured（与单实例默认行为一致）。
// 配置：REDIS_HOST / REDIS_PORT(默认 8203) / REDIS_PASSWORD / REDIS_DB。
// 多实例部署时，运维须将 REDIS_HOST 指向 Redis 服务，方能获得跨实例 exactly-once 保障。
func buildRedisClient() *redis.Client {
	host := os.Getenv("REDIS_HOST")
	if host == "" {
		return nil
	}
	port := os.Getenv("REDIS_PORT")
	if port == "" {
		port = DefaultRedisPort
	}
	dbNum := 0
	if v := os.Getenv("REDIS_DB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			dbNum = n
		}
	}
	return redis.NewClient(&redis.Options{
		Addr:     host + ":" + port,
		Password: os.Getenv("REDIS_PASSWORD"),
		DB:       dbNum,
	})
}

// redisPingerAdapter 适配 go-redis *redis.Client 以满足 router.Pinger 接口
// （go-redis 的 Ping 返回 *redis.StatusCmd，需转译为 error）。
type redisPingerAdapter struct {
	client *redis.Client
}

func (a redisPingerAdapter) Ping(ctx context.Context) error {
	return a.client.Ping(ctx).Err()
}

func main() {
	// R48: .env 加载幂等兜底（utils 包 init 期已先行加载, 此处防未来时序变化）
	utils.LoadDotEnv(".env")

	logger.InitLogger(config.GetLoggingConfig())

	// FeatureFlag 初始化：注册所有灰度开关（含 FF_ENABLE_SSE_BRIDGE）
	featureflag.DefaultManager()
	logger.Info("User Server Starting")
	logger.Infof("IS_TEST_MODE env: %s", os.Getenv("IS_TEST_MODE"))

	// NetworkExposureGuard：私域部署基线护栏（v3 审计 P0-S1）
	if err := security.NewNetworkExposureGuard().Run(); err != nil {
		log.Fatalf("[SECURITY] %v", err)
	}
	log.Printf("[SECURITY] NetworkExposureGuard passed: PUBLIC_BASE_URL=%s, REQUIRE_PRIVATE_NETWORK=%v",
		os.Getenv("PUBLIC_BASE_URL"), os.Getenv("REQUIRE_PRIVATE_NETWORK"))

	redisClient := buildRedisClient()
	var healthPinger router.Pinger
	if redisClient != nil {
		agent_runtime.SetReplyGuardRedis(redisClient)
		healthPinger = redisPingerAdapter{client: redisClient}
	}
	router.SetHealthRedis(healthPinger)

	cache.InitGlobalCache(redisClient)
	if redisClient != nil {
		defer redisClient.Close()
	}
	defer cache.CloseGlobalCache(context.Background())

	db.InitDB()
	db.AutoMigrate()

	// [ConfigParam] 动态阈值参数表 — AutoMigrate + Seed 默认 59 条参数
	if gdb := db.GetDB(); gdb != nil {
		if err := service.SeedConfigParams(context.Background(), gdb); err != nil {
			logger.Errorf("[ConfigParam] seed failed: %v", err)
		} else {
			logger.Info("[ConfigParam] dynamic threshold params seeded (59 defaults)")
		}
	}

	// [Storage] 启动时 seed 默认 local 存储配置（obs_config 表为空时自动写入，私有化零云依赖）
	service.InitDefaultStorageIfEmpty(db.GetDB())

	logger.Info("[DNC] customer_do_not_contact ready, sms_unsubscribes pending backfill via DoNotContactService.BackfillFromSMSUnsubscribe")

	if gdb := db.GetDB(); gdb != nil {
		utils.WarnErrKV("main.CreateIndexMTNode",
			gdb.Exec(`CREATE INDEX IF NOT EXISTS idx_mt_node_status_created ON message_trace (node, status, created_at)`).Error)
	}

	appCfg := config.GetAppConfig()
	service.SetAgentLoopTimeout(appCfg.Inference.LLM.TimeoutSeconds)
	// T6（ChatbotX 负面教训应用）：LLM BYOK 密钥加密存储——MASTER_KEY 未配置时
	// 降级明文（与仓库降级哲学一致），配置后存量明文在 LoadProvidersFromDB 内自动迁移
	// T9(R55)：生产模式（GIN_MODE=release，默认）且 MASTER_KEY 缺失 → fail-fast。
	// 私域营销系统凭据明文落库属不可接受风险，宁拒绝启动不降级。
	if err := secrets.InitFromEnv(); err != nil {
		if os.Getenv("GIN_MODE") == "debug" {
			logger.Warnf("[secrets] MASTER_KEY 未配置（debug 模式降级明文存储）: %v", err)
		} else {
			logger.Errorf("[secrets] MASTER_KEY 未配置或无效，生产模式拒绝启动（GIN_MODE=debug 可临时降级）: %v", err)
			os.Exit(1)
		}
	}
	llm.InitGlobalDispatcherWithDB(llm.NewDispatcherFromConfig(appCfg), db.GetDB())

	if err := llm.GetGlobalDispatcher().LoadProvidersFromDB(); err != nil {
		logger.Errorf("[LLM] 从数据库加载 provider 失败：%v", err)
	} else {
		logger.Info("[LLM] 已从数据库加载持久化 provider 定义")
	}
	if err := llm.GetGlobalDispatcher().LoadRoutesFromDB(); err != nil {
		logger.Errorf("[LLM] 从数据库加载场景路由规则失败：%v", err)
	} else {
		logger.Info("[LLM] 已从数据库加载持久化场景路由规则")
	}

	service.InitIntentRecognizer(db.GetDB(), llm.GetGlobalDispatcher(), nil)
	logger.Info("[IntentRecognition] global instance initialized, dispatcher wired")

	llm.InitDefaultAlertHook(llm.NewInMemoryAlertSink(200))

	cacheJanitorCtx, cacheJanitorCancel := context.WithCancel(context.Background())
	defer cacheJanitorCancel()
	llm.GetGlobalDispatcher().StartCacheJanitor(cacheJanitorCtx, 60*time.Second)

	if err := platform.InitSync(); err != nil {
		logger.Errorf("平台同步初始化失败：%v", err)
	}

	if err := config.LoadPlatform("config/platform.yaml"); err != nil {
		logger.Errorf("平台配置加载失败（PlatformCfg 未初始化，商户上报/授权同步将不可用）：%v", err)
	} else {
		source := "platform.yaml 默认值"
		if v := os.Getenv("PLATFORM_URL"); v != "" {
			source = "PLATFORM_URL 环境变量"
		}
		logger.Infof("[平台配置] api_url=%s（来源：%s）", config.PlatformCfg.APIURL, source)
	}

	platformURL := ""
	if config.PlatformCfg != nil {
		platformURL = config.PlatformCfg.APIURL
	}
	if platformURL == "" {
		platformURL = os.Getenv("PLATFORM_API_URL")
	}
	if platformURL == "" {
		platformURL = os.Getenv("PLATFORM_URL")
	}
	if platformURL == "" {
		platformURL = config.DefaultPlatformAPI
	}
	middleware.InitLicenseChecker(platformURL, "")
	logger.Infof("[启动] 初始化上报检查器（install.lock + 3 分钟心跳 + 9 分钟容错）")

	install.SetAdminProbe(service.NewSystemUserService().GetFirstAdminUsername)
	platform.StartHeartbeat(context.Background())

	// M8：生产模式默认 Release；启动期 Debug 由 ENV (GIN_MODE=debug) 覆盖
	if os.Getenv("GIN_MODE") == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery())
	// 仅信任私网/回环反代（FRP/ 反向代理层 均部署在同机或内网），
	// 防止公网客户端伪造 X-Forwarded-For 绕过限流与防爆破（ClientIP 伪造）
	if err := r.SetTrustedProxies([]string{
		"127.0.0.0/8", "::1/128",
		"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
	}); err != nil {
		logger.Warnf("[启动] SetTrustedProxies 失败(回退默认): %v", err)
	}

	tmplPath := filepath.Join("internal", "template", "*.html")
	if matches, err := filepath.Glob(tmplPath); err == nil && len(matches) > 0 {
		r.LoadHTMLGlob(tmplPath)
	} else {
		logger.Warnf("HTML 模板目录为空（%s），跳过 LoadHTMLGlob", tmplPath)
	}

	migrationRegistry := migration.NewMigrationRegistry()
	migrationSvc := migration.NewMigrationServiceDefault(migrationRegistry, migrations.RegisterMigrations)
	if task, err := migrationSvc.ExecuteUpgrade(context.Background(), "v1.0.0", "v1.0.0"); err != nil {
		logger.Errorf("[migration] ExecuteUpgrade 启动失败：%v", err)
	} else if task != nil {
		logger.Infof("[migration] 启动同步等待迁移完成（task_id=%d）", task.ID)
		if err := migrationSvc.WaitForTask(context.Background(), task.ID, 60*time.Second); err != nil {
			logger.Errorf("[migration] 同步等待迁移超时或失败：%v", err)
		} else {
			logger.Info("[migration] 同步迁移完成，audit 表已就绪")
		}
	}

	failover := llm.InitGlobalFailover(llm.GetGlobalDispatcher(), db.GetDB())
	failover.Start(context.Background())
	defer failover.Stop()
	app.SetGlobalProviderFailover(failover)
	app.SetGlobalDispatcher(llm.GetGlobalDispatcher())
	logger.Info("[M-1] LLM Provider failover manager started (health check + circuit breaker)")

	traceBus := llm.InitGlobalTraceBus()
	defer traceBus.Stop()
	logger.Info("[M-3] global trace bus started")

	dbTraceSink := llm.NewDBTraceSink(db.GetDB())
	dbTraceSink.Start()
	defer dbTraceSink.Stop()
	traceBus.Subscribe(dbTraceSink)
	logger.Info("[M-3.1] trace DB sink subscribed (persists InMemoryTraceBus events to trace_events)")

	defer tracing.Stop()

	sseHub := service.InitGlobalSSEHub()
	defer sseHub.Stop(context.Background())
	logger.Info("[M-4] SSE dashboard hub started (6 topics: llm_calls/intent_recognition/rag_queries/agent_actions/humanize_scores/system_alerts)")

	scheduler := service.InitSOPScheduler(db.GetDB(), nil)
	defer scheduler.Stop(context.Background())

	execDispatcher := service.InitSOPExecutionDispatcher(db.GetDB(), scheduler.SOPService(context.Background()), nil)
	defer execDispatcher.Stop(context.Background())
	execDispatcher.SetWSHub(context.Background(), websocket.GetHub())

	outboxDispatcher := service.InitSOPOutboxDispatcher(db.GetDB(), execDispatcher)
	defer outboxDispatcher.Stop(context.Background())

	stuckDetector := service.InitSOPStuckDetector(db.GetDB(), execDispatcher)
	defer stuckDetector.Stop(context.Background())

	defer event.StopGlobal()

	if intentRec := service.GetIntentRecognizer(); intentRec != nil {
		intentRec.SetSOPService(context.Background(), scheduler.SOPService(context.Background()))
	}

	service.InitConfidenceAggregator(db.GetDB(), nil, nil)
	service.InitHumanizeEvalService(db.GetDB(), nil)
	service.InitFeedbackCollector(db.GetDB())
	logger.Info("[P0-3/4/5] confidence aggregator + humanize evaluator + feedback collector initialized")

	traceLearningSvc := trace_learning.New(db.GetDB(), llm.GetGlobalDispatcher(), trace_learning.DefaultConfig())
	trace_learning.SetGlobal(traceLearningSvc)
	traceLearningCron := trace_learning.NewCron(traceLearningSvc)
	traceLearningCron.Start(context.Background())
	defer traceLearningCron.Stop(context.Background())
	logger.Info("[trace_learning] 自学习闭环已装配（cron 每小时评估新 trace 并调整知识库权重）")

	// T1(R55) 自学习闭环真激活：此前 dispatcher 传 nil → PromptIterator 恒
	// ErrDispatcherNotConfig，日度 prompt 迭代在生产空转；gateLLM 未注入 →
	// 优化建议验证门 fail-closed。注入全局 Dispatcher 后两条链路恢复工作。
	feedbackComponents := service.InitFeedbackLoopComponents(db.GetDB(), llm.GetGlobalDispatcher(), nil)
	if feedbackComponents.Optimizer != nil {
		feedbackComponents.Optimizer.SetGateLLM(llm.GetGlobalDispatcher())
	}
	feedbackCron := service.NewFeedbackLoopCron(db.GetDB(), feedbackComponents)
	defer feedbackCron.Stop(context.Background())
	logger.Info("[P0-5] feedback loop cron started (5 tasks: monthly baseline / weekly dialogue / daily prompt / 6h bandit / 6h optimizer)")

	feedbackLearningCron := service.NewFeedbackLearningCron(db.GetDB())
	if feedbackLearningCron != nil {
		defer feedbackLearningCron.Stop(context.Background())
		logger.Info("[G7] feedback learning cron started (daily: extract profile + node conversion suggestions)")
	}

	service.InitMemorySystem(db.GetDB())

	// [R7] RFM 全量计算 cron：每日 04:00 CST（此前仅有手动端点，分层永不更新）
	rfmCron := service.NewCustomerRFMCron(nil)
	rfmCron.Start(context.Background())
	defer rfmCron.Stop(context.Background())
	logger.Info("[CustomerRFMCron] RFM 分层定时重算已装配")

	// [T7-R55] RAG 自动评测 cron：每日 03:40 CST（真实生产查询采样 + 真实检索 hit 判定）
	ragEvalCron := service.NewRagEvalCron()
	ragEvalCron.Start(context.Background())
	defer ragEvalCron.Stop(context.Background())
	logger.Info("[RagEvalCron] RAG 自动评测每日已装配")

	// [H4] 客户旅程沉睡自动检测 cron：每日 03:30 CST（此前 AutoDetectSleeping 零调用）
	journeySleepCron := service.NewJourneySleepCron(nil)
	journeySleepCron.Start(context.Background())
	defer journeySleepCron.Stop(context.Background())
	logger.Info("[JourneySleepCron] 沉睡客户自动检测已装配")

	// [M1] 企微账号日配额重置 cron：每日 00:05 CST（此前仅有手动端点，需每日人工触发）
	wecomQuotaResetCron := cronpkg.NewWeComQuotaResetCron(service.NewWeComAccountHealthService(db.GetDB()))
	wecomQuotaResetCron.Start(context.Background())
	defer wecomQuotaResetCron.Stop(context.Background())
	logger.Info("[WeComQuotaResetCron] 企微日配额每日重置已装配")

	// [R48 T3] 会话暂缓到期恢复 cron：每 5 分钟
	snoozeCron := cronpkg.NewSnoozeRecoveryCron(func(ctx context.Context) (int64, error) {
		return service.NewCustomerServicePlusServiceFromGlobal().RecoverSnoozed(ctx)
	})
	snoozeCron.Start()
	defer snoozeCron.Stop()
	logger.Info("[SnoozeCron] 会话暂缓到期恢复已装配")

	// [R48 T9] 定时邮件报表：每日 08:00 窗口（复用调度骨架）
	reportCron := cronpkg.NewSnoozeRecoveryCron(func(ctx context.Context) (int64, error) {
		hm := time.Now().Format("15:04")
		if hm >= "08:00" && hm < "08:30" {
			n, err := service.NewCustomerServicePlusServiceFromGlobal().SendScheduledReports(ctx)
			return int64(n), err
		}
		return 0, nil
	})
	reportCron.Start()
	defer reportCron.Stop()
	logger.Info("[ReportCron] 定时邮件报表已装配")

	// [R53 A2] 自动解决 SLA cron：每 10 分钟扫描无活动超时会话
	autoResolveCron := cronpkg.NewSnoozeRecoveryCron(func(ctx context.Context) (int64, error) {
		n, err := service.NewSessionChainServiceFromGlobal().RunAutoResolve(ctx)
		return int64(n), err
	})
	autoResolveCron.Start()
	defer autoResolveCron.Stop()
	logger.Info("[AutoResolveCron] 自动解决 SLA 已装配")

	// [R53 B] 自动化规则延迟执行复核 cron：每 2 分钟
	ruleCron := cronpkg.NewSnoozeRecoveryCron(func(ctx context.Context) (int64, error) {
		n, err := service.NewRuleEngineServiceFromGlobal().ProcessPendingRules(ctx)
		return int64(n), err
	})
	ruleCron.Start()
	defer ruleCron.Stop()
	logger.Info("[RuleEngineCron] 自动化规则延迟执行已装配")

	// [P0-FIX A] SessionTTLCron 优雅停止（cron 在 service 包 init() 自动启动，main 需负责 Stop）
	defer service.StopSessionTTLCron(context.Background())
	logger.Info("[SessionTTLCron] 会话 TTL 自动关闭 cron 已装配（service 包 init 自动启动，这里只注册 defer Stop）")

	// [GEO-AUTO] GEO 模块三定时任务（SOV 刷新 / 负面监控 / 信源同步）
	cronpkg.InitCron()
	logger.Info("[GEO InitCron] 定时任务已注册（SOV刷新/负面监控/信源同步/竞品爬虫，经 JobManager 统一管理）")

	// [T8] 告警规则检查器：每 60s 扫描启用规则并比对阈值
	//   默认 LogAlertNotifier；通过 env 追加 EmailAlertNotifier / WebhookAlertNotifier
	alertNotifiers := []service.AlertNotifier{service.NewLogAlertNotifier()}
	if os.Getenv("ALERT_EMAIL_ENABLED") == "true" {
		if recps := os.Getenv("ALERT_EMAIL_RECIPIENTS"); recps != "" {
			recipients := strings.Split(recps, ",")
			emailSvc := service.NewEmailService(db.GetDB())
			alertNotifiers = append(alertNotifiers, service.NewEmailAlertNotifier(emailSvc, recipients, 0))
			logger.Infof("[T8] alert email notifier enabled, recipients=%v", recipients)
		}
	}
	if webhookURL := os.Getenv("ALERT_WEBHOOK_URL"); webhookURL != "" {
		alertNotifiers = append(alertNotifiers, service.NewWebhookAlertNotifier(webhookURL))
		logger.Info("[T8] alert webhook notifier enabled")
	}
	alertChecker := service.NewAlertChecker(
		service.NewMetricsAlertProvider(),
		service.NewMultiNotifier(alertNotifiers...),
		60*time.Second,
	)
	alertChecker.Start()
        defer alertChecker.Stop()
        logger.Infof("[T8] alert checker started (interval=60s, notifiers=%d)", len(alertNotifiers))

	registerEventSubscribers()

	router.Setup(r, db.GetDB())

	port := os.Getenv("PORT")
	if port == "" {
		port = DefaultListenPort
	}
	addr := "0.0.0.0:" + port
	logger.Infof("营销后端服务启动于 %s", addr)
	// serveHTTP 按平台拆分：Unix 走 endless（零停机热重启），Windows 走标准 http
	// （见 serve_unix.go / serve_windows.go）
	if err := serveHTTP(addr, r); err != nil && !isGracefulShutdownErr(err) {
		panic("服务启动失败：" + err.Error())
	}
}

// isGracefulShutdownErr 判定 ListenAndServe 返回的错误是否为优雅关闭路径的正常产物。
// endless 收到 SIGTERM/SIGINT 后关闭 listener，Serve() 会返回
// "accept tcp ...: use of closed network connection"（endless 未包装为 ErrServerClosed），
// 若不加区分地 panic，会把正常关停变成异常退出(exit≠0)并打印误导性 panic 栈。
func isGracefulShutdownErr(err error) bool {
	return errors.Is(err, http.ErrServerClosed) ||
		strings.Contains(err.Error(), "use of closed network connection")
}

// registerEventSubscribers 注册 Event Bus 订阅者
//
// 启动阶段调用,两个订阅者开始监听:
//   - agent_runtime.EventSubscriber   → customer.message.received（仅 AGENT_RUNTIME_BUS_ENABLED=true 时启用）
//   - rag.IncrementalIndexer.Handle    → knowledge.document.changed
//
// 当前 loader / bridge 均为 nil,使用降级实现(后续任务 2/3 替换)
func registerEventSubscribers() {
	bus := event.GetGlobalBus()
	if bus == nil {
		logger.Warn("[event] global bus is nil, subscriptions skipped")
		return
	}

	if os.Getenv("AGENT_RUNTIME_BUS_ENABLED") == "true" {
		rt := agent_runtime.NewAgentRuntime(nil, nil, nil)
		agentHandler := agent_runtime.NewEventSubscriber(rt)
		bus.Subscribe(event.TopicCustomerMessageReceived, agentHandler)
		logger.Info("[event] subscribed: customer.message.received -> agent_runtime (AGENT_RUNTIME_BUS_ENABLED=true)")
	} else {
		logger.Info("[event] customer.message.received 总线订阅关闭(默认)：由同步主链路处理，避免僵尸订阅者与双触发地雷")
	}

	indexer := rag.NewIncrementalIndexer(nil, nil, db.GetDB())
	bus.Subscribe(event.TopicKnowledgeDocumentChanged, indexer.Handle)
	logger.Info("[event] subscribed: knowledge.document.changed -> rag.IncrementalIndexer")

	// v3 GEO 决策链化：inbox 侧思维链回填（仅对已绑定 OneID 的 GEO 归因链生效）
	chainSync := geoservice.NewInboxChainSync(georepo.NewGeoQueryChainRepository(db.GetDB()))
	bus.Subscribe(event.TopicCustomerMessageReceived, func(evt event.Event) error {
		if p, ok := evt.Payload.(event.CustomerMessagePayload); ok {
			go func() {
				defer func() { _ = recover() }()
				chainSync.HandleCustomerMessage(context.Background(), p.CustomerID, p.Content)
			}()
		}
		return nil
	})
	logger.Info("[event] subscribed: customer.message.received -> geo inbox chain sync")
}
