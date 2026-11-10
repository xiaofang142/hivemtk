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
	"hivemtk-user/internal/aiagent/embedding"
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
	"hivemtk-user/internal/model"
	cronpkg "hivemtk-user/internal/pkg/cron"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/featureflag"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/tracing"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/platform"
	"hivemtk-user/internal/repository"
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

	DefaultListenHost = config.DefaultListenHost

	DefaultRedisPort = config.DefaultRedisPort
)

// resolveListenAddr 组装 HTTP 监听地址：host/port 传空串即取各自默认值。
// 单列成函数是为了让"两半各管一半、缺省与历史逐字节一致"可测（见 defaults_test.go）。
func resolveListenAddr(host, port string) string {
	if host == "" {
		host = DefaultListenHost
	}
	if port == "" {
		port = DefaultListenPort
	}
	return host + ":" + port
}

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

type redisPingerAdapter struct {
	client *redis.Client
}

func (a redisPingerAdapter) Ping(ctx context.Context) error {
	return a.client.Ping(ctx).Err()
}

// @title						HiveMtk 用户端 API
// @version					3.42.0
// @description				HiveMtk 用户端（私域独立部署）RESTful API。认证采用 JWT：请求头 Authorization: Bearer <token>。
// @description				仅当 ENABLE_SWAGGER=true 时，通过 http://localhost:8204/swagger/index.html 本地访问。
// @BasePath					/
// @securityDefinitions.apikey	BearerAuth
// @in							header
// @name						Authorization
// @description					Bearer JWT，格式：Bearer <token>
func main() {

	utils.LoadDotEnv(".env")

	logger.InitLogger(config.GetLoggingConfig())

	// TENANT_MODE 装配（001 多租户双形态）：必须在 db.InitDB/router.Setup 之前生效——
	// 十二个 callback/中间件/登录通道全部读 tenant.IsMultiTenant()，
	// 漏掉本调用则进程永远停在默认 off，环境变量静默失效（003 活体联调发现的装配缺口）。
	//
	// TENANT_MODE 拼错（既不是 off/on 也不是空）时 InitFromEnv 返回 error 并保持 off：
	// 此时**必须拒绝启动**，不能把 error 丢掉继续跑。丢掉的后果是 SaaS 部署静默降级成
	// 单机形态——所有租户的数据被写进默认租户(tenant_id=1)，而进程全程不报任何错，
	// 故障要到跨租户串数据时才暴露，那时已经写坏了一整批行。
	// 失败即退出（fail-closed），与 tenancyidx「缺注册表就红」同一口径。
	mode, err := tenant.InitFromEnv()
	if err != nil {
		log.Fatalf("[tenant] TENANT_MODE 装配失败，拒绝启动：%v", err)
	}
	if mode == tenant.ModeOn {
		logger.Info("[tenant] TENANT_MODE=on：SaaS 多租户形态已启用")
	} else {
		logger.Info("[tenant] TENANT_MODE=off：离线单机形态已启用")
	}

	featureflag.DefaultManager()
	logger.Info("User Server Starting")
	logger.Infof("IS_TEST_MODE env: %s", os.Getenv("IS_TEST_MODE"))

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
		defer func() { _ = redisClient.Close() }()
	}
	defer func() { _ = cache.CloseGlobalCache(context.Background()) }()

	db.InitDB()
	db.AutoMigrate()

	// W-6 工具审计落库：退出前排空异步队列（见 internal/app/tool_audit_wiring.go）。
	//
	// 为什么登记在这里而不是紧跟装配点（router.Setup 在更下面）：defer 是 LIFO，
	// 越晚登记的越早执行。审计写入是异步的，Close 之后再入队的条目就没有人消费了；
	// 而 scheduler / SOP dispatcher / 挽回 worker 都会调用工具，它们必须在 Close 之前停稳。
	// 登记点在各成分之前 ⇒ 本 Close 是最后一批执行的动作之一。
	// 未接旗时它是 no-op（读包级变量，登记时是否已构造无所谓）。
	defer app.CloseToolAuditPersistence()

	if gdb := db.GetDB(); gdb != nil {
		if err := service.SeedConfigParams(context.Background()); err != nil {
			logger.Errorf("[ConfigParam] seed failed: %v", err)
		} else {
			// 数量从种子现取。写死一个数字的话，种子涨上去之后它一直在撒谎——
			// 跟「登记了没人读」是同一类病：日志看着像证据，实际早就不准了。
			logger.Info("[ConfigParam] dynamic threshold params seeded (" + strconv.Itoa(len(service.DefaultParamDefs())) + " defaults)")
		}
		// 分组元数据（侧栏中文名/排序）单独 seed，失败只降级外观——页面上回落显示英文
		// group key，功能不残废，所以这里只记 error 不阻断启动。与 SeedConfigParams
		// 分开是为了不让「组表缺失」把参数值本身的 seed 一起判死（那会让每个只声明
		// &model.ConfigParam{} 的测试 fixture 全红，详见 service.SeedParamGroups 的注释）。
		if err := service.SeedParamGroups(context.Background(), repository.NewConfigParamRepository(gdb)); err != nil {
			logger.Errorf("[ConfigParam] 分组元数据 seed failed（管理端侧栏会回落成英文组名）: %v", err)
		} else {
			logger.Info("[ConfigParam] param group metadata seeded (" + strconv.Itoa(len(service.DefaultParamGroupDefs())) + " groups)")
		}
	}

	// misc 组接线上线：把参数中心的取值注入到 model/repository/middleware/pkg/ops 等
	// 无法反向依赖 service 的下层包。放在 seed 之后，这样刚改的值本次启动就生效；
	// 放在 if 之外，这样 seed 失败（DB 抖了）时各点位仍能拿到代码兜底值而不是没人注入。
	if wired := app.WireMiscConfigParams(); len(wired) > 0 {
		logger.Info("[ConfigParam] misc 组接线 " + strconv.Itoa(len(wired)) + " 条")
	}

	// confidence 组同理：点位分布在 service 本体与 service/humanize、service/confidence
	// 两个下层包，下层包不能反向 import service，只能由装配层注入。
	if wired := app.WireConfidenceConfigParams(); len(wired) > 0 {
		logger.Info("[ConfigParam] confidence 组接线 " + strconv.Itoa(len(wired)) + " 条")
	}

	// agent_llm 组同理：熔断四件套、投票一致阈值、客户端 HTTP 超时、Trace DB Sink
	// 停止截止，全都住在 internal/aiagent/llm 里，而这个包不能反向 import service。
	if wired := app.WireAgentLLMConfigParams(); len(wired) > 0 {
		logger.Info("[ConfigParam] agent_llm 组接线 " + strconv.Itoa(len(wired)) + " 条")
	}

	// agent_tool 组：并发上限、工具结果截断、ToolRouter 熔断阈值与冷却期，
	// 都在 internal/aiagent/agent/tooluse 里，那个包同样不能反向 import service。
	if wired := app.WireAgentToolConfigParams(); len(wired) > 0 {
		logger.Info("[ConfigParam] agent_tool 组接线 " + strconv.Itoa(len(wired)) + " 条")
	}

	// cache 组同理：内存缓存 LRU 上限、安装 memo、平台信息缓存、翻译缓存四样分别住在
	// internal/cache、internal/system/install、internal/controller、internal/aiagent/rag/retrieval，
	// 四个包都不反向 import service，只有装配层同时看得见它们。
	if wired := app.WireCacheConfigParams(); len(wired) > 0 {
		logger.Info("[ConfigParam] cache 组接线 " + strconv.Itoa(len(wired)) + " 条")
	}

	// telemetry + workflow 两组共八条：节点健康窗口与 Trace 缓冲住在 monitor / aiagent/llm，
	// 品牌词去重窗口住在 geo/service，Feature Flag 热加载周期住在 pkg/featureflag，
	// 四条 workflow 点位住在 service 本体。同样因为下层包不能反向 import service，统一装配。
	if wired := app.WireTelemetryWorkflowConfigParams(); len(wired) > 0 {
		logger.Info("[ConfigParam] telemetry/workflow 组接线 " + strconv.Itoa(len(wired)) + " 条")
	}

	if wired := app.WireInboxSalesSessionPaginationConfigParams(); len(wired) > 0 {
		logger.Info("[ConfigParam] inbox_sales/session/pagination 组接线 " + strconv.Itoa(len(wired)) + " 条")
	}

	// 最后一批：sales(1)+middleware(2)+knowledge(2) 五条走 provider 注入；
	// channelgw.ws_push_interval / wecom.quota_degrade 走「读取函数自己读参数中心」的
	// 直连范式，不需要装配（其中 ws_push_interval 原先的 key 与种子对不上，本轮已修正）。
	if wired := app.WireSalesMiddlewareKnowledgeWecomChannelGWConfigParams(); len(wired) > 0 {
		logger.Info("[ConfigParam] sales/middleware/knowledge 组接线 " + strconv.Itoa(len(wired)) + " 条")
	}

	// 阶段二 2a：全局请求体上限注入中间件；上传上限那条走 controller 直连范式。
	// body 上限在 router.Setup 里装配期读一次，所以它的种子标了 Restart=true。
	if wired := app.WireUploadBodyLimitConfigParams(); len(wired) > 0 {
		logger.Info("[ConfigParam] 上传/body 上限接线 " + strconv.Itoa(len(wired)) + " 条")
	}

	// frontend_ui 组平时只有前端读，后端一处不摸；本条是第一个例外——素材库上传口
	// 若写死 10MB，运维调大后前端放行、后端拦死。走注入而不是 controller 直连，
	// 因为 internal/content 整个子模块不 import internal/service。
	if wired := app.WireFrontendUIConfigParams(); len(wired) > 0 {
		logger.Info("[ConfigParam] frontend_ui 组接线 " + strconv.Itoa(len(wired)) + " 条")
	}

	// 阶段二 2c：锁与幂等 TTL。这一组是正确性参数而不是调优项，锁 TTL 写错的
	// 结果是死锁或重复处理，所以六条都在这里显式注入。放在 SeedConfigParams 的
	// if/else 之外是为了 seed 失败时各锁仍能托在兜底常量上正常跑。
	if wired := app.WireLockIdempotencyConfigParams(); len(wired) > 0 {
		logger.Info("[ConfigParam] 锁与幂等 TTL 接线 " + strconv.Itoa(len(wired)) + " 条")
	}

	// 渠道侧长度/条数上限与外部依赖超时。同样放 seed 的 if/else 之外：
	// 这几个读取点横跨 qq/telegram/core 三个子包，service 反向 import 它们，
	// 底层包接不了 config_params，只能由这一层注入。
	if wired := app.WireChannelBotConfigParams(); len(wired) > 0 {
		logger.Info("[ConfigParam] 渠道上限与外部依赖超时接线 " + strconv.Itoa(len(wired)) + " 条")
	}

	// 阶段三 3.1 第一批 P1：拟人度评估的其余边界、圈选一对、编辑锁 TTL、短期记忆
	// TTL。可见性趋势默认天数不走这里——internal/geo/service 本就能直连参数中心。
	if wired := app.WireP1ConfigParams(); len(wired) > 0 {
		logger.Info("[ConfigParam] P1 第一批接线 " + strconv.Itoa(len(wired)) + " 条")
	}

	// 全局 Embedding 提供商覆盖：一读一写两条缝。写侧若裸 SQL 直写 KV 表，
	// 就拿不到值校验、缓存失效与"谁改的"那条审计。
	if wired := app.WireEmbeddingConfigParams(); len(wired) > 0 {
		logger.Info("[ConfigParam] Embedding 全局覆盖接线 " + strconv.Itoa(len(wired)) + " 条")
	}

	// urls 组两条：cloud_url / geo_site_base_url 的读取点在 internal/config
	// （config 不能反向 import service），只能由装配层注入 provider。frontend_url
	// 走 service 直连范式，不在这里。
	if wired := app.WireURLsConfigParams(); len(wired) > 0 {
		logger.Info("[ConfigParam] urls 组接线 " + strconv.Itoa(len(wired)) + " 条")
	}
	// A11：云端版地址若仍等于厂商默认域名（且不是 env / 平台配置显式给的）⇒ 启动期响亮一行。
	// 放在接线之后：provider 已注入，这里读到的就是运行期真正会用的值。
	app.WarnIfCloudURLIsBuiltinDefault()

	// 访客会话 HMAC 密钥漏配 ⇒ embed 全链每请求 fail-closed（2026-11-10 回归实测）：
	// 同款"沉默失败"告警——运行期安全默认不动，只把根因搬到启动日志里。
	app.WarnIfVisitorTokenSecretMissing()

	// reporting 组：报表默认窗口天数。点位横跨 repository/controller/geo/
	// knowledge/ops 等不能反向 import service 的包，走 provider 注入。
	if wired := app.WireReportingConfigParams(); len(wired) > 0 {
		logger.Info("[ConfigParam] reporting 组接线 " + strconv.Itoa(len(wired)) + " 条")
	}

	service.InitDefaultStorageIfEmpty(repository.NewObsConfigRepositoryWithDB(db.GetDB()))
	service.BindAssetLoaderRepository(repository.NewAssetLoaderRepository(db.GetDB()))

	logger.Info("[DNC] customer_do_not_contact ready, sms_unsubscribes pending backfill via DoNotContactService.BackfillFromSMSUnsubscribe")

	if gdb := db.GetDB(); gdb != nil {
		utils.WarnErrKV("main.CreateIndexMTNode",
			gdb.Exec(`CREATE INDEX IF NOT EXISTS idx_mt_node_status_created ON message_trace (node, status, created_at)`).Error)
	}

	appCfg := config.GetAppConfig()
	service.SetAgentLoopTimeout(appCfg.Inference.LLM.TimeoutSeconds)

	// 先从 /run/secrets（K8s Secret/Vault Agent 挂载卷）预加载密钥文件为
	// env，再执行 InitFromEnv；已显式设置的 env 优先，不覆盖。
	secretsDir := os.Getenv("HIVEMTK_SECRETS_DIR")
	if secretsDir == "" {
		secretsDir = "/run/secrets"
	}
	if injected, err := secrets.LoadFiles(secretsDir); err != nil {
		logger.Warnf("[secrets] 加载 %s 密钥文件部分失败（继续启动）: %v", secretsDir, err)
	} else if injected > 0 {
		logger.Infof("[secrets] 从 %s 注入 %d 个密钥环境变量", secretsDir, injected)
	}

	if err := secrets.InitFromEnv(); err != nil {
		if config.IsDevelopmentEnv() {
			logger.Warnf("[secrets] MASTER_KEY 未配置（非生产环境降级明文存储）: %v", err)
		} else {
			logger.Errorf("[secrets] MASTER_KEY 未配置或无效，生产环境拒绝启动（设置 APP_ENV=development 或 GIN_MODE=debug 可临时降级）: %v", err)
			os.Exit(1)
		}
	}
	llm.InitGlobalDispatcherWithDB(llm.NewDispatcherFromConfig(appCfg), db.GetDB())

	// ── 功能模块化 · 平台垫资模型装配 ──────────────────────────────
	//
	// 平台垫资模型的装配分两段，顺序有硬依赖：
	//   ① 此处：只接用量回填 sink —— 必须在 LoadProvidersFromDB 之前，
	//      否则首次派发命中平台 provider 时会丢第一条明细。
	//   ② 平台配置就绪之后（loadPlatformLLMProviders）：先拉平台下发的 provider
	//      元信息（**不含 Key**），再从库里加载 provider。
	//      顺序反过来，本次下发的 provider 要等下一个周期才生效。
	//
	// 平台不可达时全部降级：自带 Key 的 provider 完全不受影响（零出域铁律）。
	// ① 只接 sink（不碰平台）：必须在 LoadProvidersFromDB 之前 ——
	//    首次派发就可能命中平台 provider，那时 sink 没接上会丢第一条明细。
	service.InstallPlatformUsageSink()

	// ② provider 的实际加载延后到平台配置就绪之后（见下方 loadPlatformLLMProviders）。
	//    若放在这一步，config.PlatformCfg 尚为 nil → 请求被拒 →
	//    平台 provider **永远拉不下来**，日志里只有一行 warn。
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

	// C2 推理降级档：启动时显式定档并记录，不修改 Dispatcher 运行时路由。
	//
	// 为什么放在 provider/路由装载之后：等 Dispatcher 就绪后再定档，日志顺序与
	// 运行时事实一致。档位输入的"云端是否已配置"按 config.yaml 的 cloud_providers
	// 口径判定（与 registerCloudProvidersFromConfig 一致）；从数据库加载的 provider
	// 走 Dispatcher 运行时回落链，本档位不参与改路由。探测只读 GET /health，
	// 不写库、不改配置。
	app.SelectAndRecordInferenceTier(
		context.Background(),
		app.BuildTierPolicyFromConfig(appCfg),
		app.NewHTTPInferenceProbe(3*time.Second),
	)

	service.InitIntentRecognizer(llm.GetGlobalDispatcher(), nil)
	logger.Info("[IntentRecognition] global instance initialized, dispatcher wired")

	llm.InitDefaultAlertHook(llm.NewInMemoryAlertSink(200))

	cacheJanitorCtx, cacheJanitorCancel := context.WithCancel(context.Background())
	defer cacheJanitorCancel()
	llm.GetGlobalDispatcher().StartCacheJanitor(cacheJanitorCtx, 60*time.Second)

	// 平台集成是可选本地组件：PLATFORM_ENABLED 未开启时整条链路不装配
	// （不读配置、不注册商户、不心跳、不发一个请求），本地资产与运行链路不受影响。
	//
	// 关态分两种，日志必须分开说：未设置（正常的纯本地部署）与**拼错**。
	// 拼错时若也报"未设置"，运维就会以为平台没接、往别的方向查——
	// 而真相是"开关名写错了"，那才是唯一能解释"我明明配了却不生效"的原因。
	//
	// 守卫的判真仍以 config.PlatformEnabled() 为准（判真方向朝"关"是刻意设计，
	// 见 internal/config/platform.go）；这里额外用 ResolvePlatformEnabled 区分
	// "未设置"与"拼错"，只为把话说准，不改判真结果。
	if !config.PlatformEnabled() {
		if _, known := config.ResolvePlatformEnabled(os.Getenv("PLATFORM_ENABLED")); known {
			logger.Info("[启动] 平台集成关闭（PLATFORM_ENABLED 未开启）：不加载平台配置、不注册商户、不心跳；" +
				"资产市场与平台上报类端点返回空数据，本地资产构建与运行不受影响")
		} else {
			logger.Warnf("[启动] 平台集成关闭：PLATFORM_ENABLED=%q 不是已知值（true/false/1/0/on/off/yes/no），"+
				"已按关闭处理。需要联平台请改成 true —— 拼错会让商户注册、资产市场、模块下发全部静默失效。",
				os.Getenv("PLATFORM_ENABLED"))
		}
	} else {
		if err := config.LoadPlatform("config/platform.yaml"); err != nil {
			logger.Errorf("平台配置加载失败（PlatformCfg 未初始化，商户上报/资产市场将不可用）：%v", err)
		} else {
			logger.Infof("[平台配置] api_url=%s", config.PlatformURL())
		}
		// 必须在 LoadPlatform 之后：InitSync 的注册协程要读 PlatformCfg
		if err := platform.InitSync(); err != nil {
			logger.Errorf("平台同步初始化失败：%v", err)
		}
		platform.StartHeartbeat(context.Background())
		// 必须在 LoadPlatform 之后：拉平台 provider 要读 config.PlatformCfg。
		// 放在 LoadPlatform 之前会得到「平台配置未初始化」，平台 provider 永远拉不到。
		if n, err := service.SyncPlatformProviders(context.Background()); err != nil {
			logger.Warnf("[llm-provider] 拉取平台 provider 失败（不影响自带 Key 使用）: %v", err)
		} else if n > 0 {
			logger.Infof("[llm-provider] 已同步 %d 个平台下发 provider", n)
			// 重载一次，让本次下发的 provider 立刻进路由表，
			// 否则要等 10 分钟的模块同步周期才生效。
			if err := llm.GetGlobalDispatcher().LoadProvidersFromDB(); err != nil {
				logger.Warnf("[llm-provider] 重载 provider 失败: %v", err)
			} else {
				logger.Info("[llm-provider] 平台 provider 已并入路由表")
			}
		}
	}
	middleware.InitInstallStatus()
	logger.Info("[启动] 初始化安装态检查器（只读本地 install.lock，无平台依赖）")

	install.SetAdminProbe(service.NewSystemUserService().GetFirstAdminUsername)

	if os.Getenv("GIN_MODE") == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery())

	// A13：可信反代名单 env TRUSTED_PROXIES 可覆盖（默认值仍为原私网名单，行为零变更），
	// 并挂上"带了 X-Forwarded-For 但对端不在名单里"的观测中间件 —— 反代一旦落在公网地址上，
	// gin 会静默把 ClientIP() 退化成反代 IP（全站一个限流桶、审计 IP 全写成同一个），
	// 这条中间件让它在第一分钟就有响声，而不是等"限流为什么这么怪"查起。
	if _, err := middleware.ConfigureTrustedProxies(r); err != nil {
		logger.Warnf("[启动] 可信反代名单装配失败（沿用 gin 默认语义，ClientIP 可能退化，请修 env TRUSTED_PROXIES）: %v", err)
	}

	tmplPath := filepath.Join("internal", "template", "*.html")
	if matches, err := filepath.Glob(tmplPath); err == nil && len(matches) > 0 {
		r.LoadHTMLGlob(tmplPath)
	} else {
		logger.Warnf("HTML 模板目录为空（%s），跳过 LoadHTMLGlob", tmplPath)
	}

	migrationRegistry := migration.NewMigrationRegistry()
	migrationSvc := migration.NewMigrationServiceDefault(migrationRegistry, migrations.RegisterMigrations)
	// 启动期迁移是平台级操作：没有 HTTP 请求 ctx 可绑，TENANT_MODE=on 时 upgrade_tasks
	// 的裸 Create / WaitForTask 的裸查询会被隔离 callback fail-closed（生产日志实证：
	// "[migration] ExecuteUpgrade 启动失败：[tenant] 表 upgrade_tasks 需要租户上下文但 ctx 未绑定（create）"）。
	// 走 tenant.WithSystemScope 这条平台级豁免通道（与 migration/备份/健康检查同类），
	// 不放宽任何带请求 ctx 的 HTTP 路径（controller/migration.go 传的是 request ctx）。
	migrationCtx := tenant.WithSystemScope(context.Background())
	if task, err := migrationSvc.ExecuteUpgrade(migrationCtx, "v1.0.0", "v1.0.0"); err != nil {
		logger.Errorf("[migration] ExecuteUpgrade 启动失败：%v", err)
	} else if task != nil {
		logger.Infof("[migration] 启动同步等待迁移完成（task_id=%d）", task.ID)
		if err := migrationSvc.WaitForTask(migrationCtx, task.ID, 60*time.Second); err != nil {
			logger.Errorf("[migration] 同步等待迁移超时或失败：%v", err)
		} else {
			logger.Info("[migration] 同步迁移完成，audit 表已就绪")
		}
	}

	failover := llm.InitGlobalFailover(llm.GetGlobalDispatcher())
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

	scheduler := service.InitSOPScheduler(nil)
	defer scheduler.Stop(context.Background())

	execDispatcher := service.InitSOPExecutionDispatcher(scheduler.SOPService(context.Background()), nil)
	defer execDispatcher.Stop(context.Background())
	execDispatcher.SetWSHub(context.Background(), websocket.GetHub())
	// W-2 Saga 补偿挂载：FF_LTC_SAGA_COMPENSATION 关时不注入，失败路径与今天一致。
	sopCompensationMgr := service.InitSOPCompensation(execDispatcher)
	if sopCompensationMgr != nil {
		// 灰度期观测：退出前把内存保留的补偿摘要打出来，作为"这条接线是否可默认开启"的证据来源。
		defer func() {
			s := sopCompensationMgr.Summary()
			logger.Infof("[saga] 退出前补偿摘要：plans=%d completed=%d failed=%d partial=%d nodes=%d failed_nodes=%d",
				s.TotalPlans, s.CompletedPlans, s.FailedPlans, s.PartialPlans, s.TotalNodes, s.FailedNodes)
		}()
	}

	outboxDispatcher := service.InitSOPOutboxDispatcher(execDispatcher)
	defer outboxDispatcher.Stop(context.Background())

	stuckDetector := service.InitSOPStuckDetector(execDispatcher)
	defer stuckDetector.Stop(context.Background())

	defer event.StopGlobal()

	if intentRec := service.GetIntentRecognizer(); intentRec != nil {
		intentRec.SetSOPService(context.Background(), scheduler.SOPService(context.Background()))
	}

	service.InitConfidenceAggregator(repository.NewThresholdPolicyRepositoryWithDB(db.GetDB()), repository.NewConfidenceSignalRepositoryWithDB(db.GetDB()), repository.NewConfidenceCalibrationRepositoryWithDB(db.GetDB()), service.NewLocalConfidenceEmbedder())
	service.InitHumanizeEvalService(nil)
	service.InitFeedbackCollector()
	logger.Info("[P0-3/4/5] confidence aggregator + humanize evaluator + feedback collector initialized")

	// R-8 合规审计：不装配时触达侧所有判定/提醒全走 nil-safe 退路（只打 WARN 不落库），
	// reach_compliance_log 一行不会有——审计面在代码里齐全、在库里不存在。
	complianceAudit := service.InitComplianceAuditLogger()
	defer complianceAudit.Stop()
	logger.Info("[R-8] 合规审计落库器已装配（reach_compliance_log 异步批量落盘）")

	traceLearningSvc := trace_learning.New(repository.NewTraceLearningRepository(db.GetDB()), llm.GetGlobalDispatcher(), trace_learning.DefaultConfig())
	trace_learning.SetGlobal(traceLearningSvc)
	traceLearningCron := trace_learning.NewCron(traceLearningSvc)
	traceLearningCron.Start(context.Background())
	defer traceLearningCron.Stop(context.Background())
	logger.Info("[trace_learning] 自学习闭环已装配（cron 每小时评估新 trace 并调整知识库权重）")

	feedbackComponents := service.InitFeedbackLoopComponents(llm.GetGlobalDispatcher(), embedding.NewLocalEmbedding(1024, 42))
	if feedbackComponents.Optimizer != nil {
		feedbackComponents.Optimizer.SetGateLLM(llm.GetGlobalDispatcher())
	}
	feedbackCron := service.NewFeedbackLoopCron(feedbackComponents)
	defer feedbackCron.Stop(context.Background())
	logger.Info("[P0-5] feedback loop cron started (5 tasks: monthly baseline / weekly dialogue / daily prompt / 6h bandit / 6h optimizer)")

	feedbackLearningCron := service.NewFeedbackLearningCron(repository.NewFeedbackLearningRepository(db.GetDB()))
	if feedbackLearningCron != nil {
		defer feedbackLearningCron.Stop(context.Background())
		logger.Info("[G7] feedback learning cron started (daily: extract profile + node conversion suggestions)")
	}

	service.InitMemorySystem(repository.NewMemoryRepositoryWithDB(db.GetDB()))

	// L-4 记忆衰减归档：此前只有实现、没有装配点（memory_items 与
	// customer_long_term_memory 的 invalid_at 永远不会被写，L2 事实只进不出）。
	// 这里按 1h 一轮接入，跨租户由 RunMemoryDecayAllTenants 经 tenant.Fanout 扇出。
	service.StartMemoryDecayLoop(context.Background(), repository.NewMemoryDecayRepository(db.GetDB()), time.Hour)
	logger.Info("[MemoryDecay] L-4 记忆衰减归档已装配（1h 一轮，按租户扇出）")

	rfmCron := service.NewCustomerRFMCron(nil)
	rfmCron.Start(context.Background())
	defer rfmCron.Stop(context.Background())
	logger.Info("[CustomerRFMCron] RFM 分层定时重算已装配")

	churnCron := service.NewChurnScoreCron(service.NewChurnScoreService(repository.NewChurnScoreRepository(db.GetDB())))
	churnCron.Start(context.Background())
	defer churnCron.Stop(context.Background())
	logger.Info("[ChurnScoreCron] BG/NBD 流失评分周批已装配")

	// W-5 挽回队列消费的装配点在这里（不在下面）：入队侧（RFM/Churn → Enqueue）早已就绪，
	// 缺的是把到期项发出去的人。**真正的启动挪到了 router.Setup 之后**，原因见那里。

	ragEvalCron := service.NewRagEvalCron()
	ragEvalCron.Start(context.Background())
	defer ragEvalCron.Stop(context.Background())
	logger.Info("[RagEvalCron] RAG 自动评测每日已装配")

	dialogueEvalCron := service.NewDialogueEvalCron()
	dialogueEvalCron.Start(context.Background())
	defer dialogueEvalCron.Stop(context.Background())
	logger.Info("[DialogueEvalCron] 对话级 gold 评测每日已装配")

	journeySleepCron := service.NewJourneySleepCron(nil)
	journeySleepCron.Start(context.Background())
	defer journeySleepCron.Stop(context.Background())
	logger.Info("[JourneySleepCron] 沉睡客户自动检测已装配")

	wecomQuotaResetCron := cronpkg.NewWeComQuotaResetCron(service.NewWeComAccountHealthService())
	wecomQuotaResetCron.Start(context.Background())
	defer wecomQuotaResetCron.Stop(context.Background())
	logger.Info("[WeComQuotaResetCron] 企微日配额每日重置已装配")

	snoozeCron := cronpkg.NewSnoozeRecoveryCron(func(ctx context.Context) (int64, error) {
		return tenant.Fanout(ctx, db.GetDB(), func(ctx context.Context, _ uint) (int64, error) {
			return service.NewCustomerServicePlusServiceFromGlobal().RecoverSnoozed(ctx)
		})
	})
	snoozeCron.Start()
	defer snoozeCron.Stop()
	logger.Info("[SnoozeCron] 会话暂缓到期恢复已装配")

	reportCron := cronpkg.NewSnoozeRecoveryCron(func(ctx context.Context) (int64, error) {
		hm := time.Now().Format("15:04")
		if hm >= "08:00" && hm < "08:30" {
			return tenant.Fanout(ctx, db.GetDB(), func(ctx context.Context, _ uint) (int64, error) {
				n, err := service.NewCustomerServicePlusServiceFromGlobal().SendScheduledReports(ctx)
				return int64(n), err
			})
		}
		return 0, nil
	})
	reportCron.Start()
	defer reportCron.Stop()
	logger.Info("[ReportCron] 定时邮件报表已装配")

	autoResolveCron := cronpkg.NewSnoozeRecoveryCron(func(ctx context.Context) (int64, error) {
		return tenant.Fanout(ctx, db.GetDB(), func(ctx context.Context, _ uint) (int64, error) {
			n, err := service.NewSessionChainServiceFromGlobal().RunAutoResolve(ctx)
			return int64(n), err
		})
	})
	autoResolveCron.Start()
	defer autoResolveCron.Stop()
	logger.Info("[AutoResolveCron] 自动解决 SLA 已装配")

	ruleCron := cronpkg.NewSnoozeRecoveryCron(func(ctx context.Context) (int64, error) {
		return tenant.Fanout(ctx, db.GetDB(), func(ctx context.Context, _ uint) (int64, error) {
			n, err := service.NewRuleEngineServiceFromGlobal().ProcessPendingRules(ctx)
			return int64(n), err
		})
	})
	ruleCron.Start()
	defer ruleCron.Stop()
	logger.Info("[RuleEngineCron] 自动化规则延迟执行已装配")

	service.StartSessionTTLCron()
	defer service.StopSessionTTLCron(context.Background())
	logger.Info("[SessionTTLCron] 会话 TTL 自动关闭 cron 已显式启动（替代原 init 副作用）")

	// msg_hourly_summary 唯一的写入者。service 包的 init 装配传的是 nil DB ⇒ repo 非空但
	// 句柄为空，每 5 分钟只留下一行 "invalid db"，水位线永不推进，汇总表从未产出过一行
	// （仪表盘的消息量因此一直走实时兜底）。与上一行同法改为显式装配。
	service.StartMessageHubSummaryAggCron(repository.NewMessageHubSummaryRepository(db.GetDB()))
	defer service.StopMessageHubSummaryAggCron(context.Background())
	logger.Info("[HubSummaryAggCron] message_hub 小时汇总已显式启动（替代 init 里的 nil DB 哑装配）")

	cronpkg.InitCron()
	logger.Info("[GEO InitCron] 定时任务已注册（SOV刷新/负面监控/信源同步/竞品爬虫，经 JobManager 统一管理）")

	alertNotifiers := []service.AlertNotifier{service.NewLogAlertNotifier()}
	if os.Getenv("ALERT_EMAIL_ENABLED") == "true" {
		if recps := os.Getenv("ALERT_EMAIL_RECIPIENTS"); recps != "" {
			recipients := strings.Split(recps, ",")
			emailSvc := service.NewEmailService()
			alertNotifiers = append(alertNotifiers, service.NewEmailAlertNotifier(emailSvc, recipients, 0))
			logger.Infof("[T8] alert email notifier enabled, recipients=%v", recipients)
		}
	}
	if webhookURL := os.Getenv("ALERT_WEBHOOK_URL"); webhookURL != "" {
		alertNotifiers = append(alertNotifiers, service.NewWebhookAlertNotifier(webhookURL))
		logger.Info("[T8] alert webhook notifier enabled")
	}
	alertChecker := service.NewAlertCheckerWithRepos(
		service.NewMetricsAlertProvider(),
		service.NewMultiNotifier(alertNotifiers...),
		60*time.Second,
		repository.NewAlertRuleRepositoryWithDB(db.GetDB()),
		repository.NewAlertHistoryRepositoryWithDB(db.GetDB()),
	)
	alertChecker.Start()
	defer alertChecker.Stop()
	logger.Infof("[T8] alert checker started (interval=60s, notifiers=%d)", len(alertNotifiers))

	registerEventSubscribers()

	// 阶段边界断点续跑：必须在 router.Setup 之前装配，
	// 否则 InitInferenceOrchestrator 构造推理闭环时拿不到 store。
	app.InitAgentCheckpointStore(db.GetDB())

	// SLO 跟踪器（internal/pkg/sla）：启动期显式登记三条 SLI 并注册指标。
	// 记录点分布在 service 层（编排器、人工待办），走 sla.Record 直达同一实例。
	app.InitSLO()

	// ── 功能模块化：模块下发同步（必须在 router.Setup 之前）────
	//
	// 顺序有硬依赖：router.Setup 会装上模块闸，而闸的判定读的是
	// tenant_modules 本地表。这张表只有一个写入方，就是这里的同步。
	//
	// 首轮同步是**同步执行**的（见 StartModuleSync 的注释）：闸装上之后、
	// 首轮同步完成之前存在一个「全放行」窗口，而那恰好是并发最高的启动瞬间。
	moduleSync := service.StartModuleSync(context.Background())
	defer moduleSync.Stop()
	// 同步闸就绪才服务请求（MD-02）：首轮同步在 StartModuleSync 内已同步执行，
	// 这里显式 WaitFirstSync 是把「模块闸的数据已就位」钉成调用点上的硬工序 ——
	// router.Setup 装上的模块闸读的是这张表，HTTP 开始收流量前必须等它落地。
	moduleSync.WaitFirstSync()

	// ── 站内消息：平台消息投递同步 ──────────────────────────────
	//
	// 平台运营方在平台后台发的消息，由本机周期拉取并镜像到本地
	// notifications 表（与模块下发同步同一模式：拉取而非推送 ——
	// 生产是单机部署，用户端没有公网回调地址）。
	//
	// 平台联动未启用（PLATFORM_ENABLED=false）时整个跳过，不发起任何
	// 平台请求 —— 离线单机部署不该在日志里留平台不可达的噪音。
	noticeSync := service.StartNoticeSync(context.Background())
	defer noticeSync.Stop()

	// ── 开户登记自动补偿扫描（PLT-03）──────────────────────────
	//
	// 开户上报失败此前被置 pending/failed 后再无人过问，补同步只有管理端
	// 「立即重试」按钮 —— 没人点按钮，合规登记/merchants 镜像/租户维度
	// 钱包订阅行就永远缺漏。本任务周期（10min，与模块同步对齐）扫描
	// pending/failed 逐行重报，与手动按钮并存不互斥。
	//
	// 平台未启用（PLATFORM_ENABLED=false）时整个不启动：不查库、不起协程、
	// 零出站请求 —— 离线部署没有"登记补偿"这个概念。
	regCompensation := service.StartTenantRegistrationCompensation(context.Background())
	defer regCompensation.Stop()

	// ── 内容安全闸：词库/策略/异步审查/处置 ──────────────────────
	//
	// 必须先于 router.Setup：路由挂载点（setupContentGuardRoutes）从全局登记处取运行时，
	// 取不到就整组回 503。装配同时启动异步审查 worker 并订阅
	// customer.message.received —— 业务链路只做一次插队，真正的扫描/外部审核/处置
	// 都在 worker 里，不会拖慢首响与流式输出。
	if contentGuard := app.InitContentGuardRuntime(db.GetDB()); contentGuard != nil {
		defer app.StopContentGuardRuntime(contentGuard)
	}

	// TB-01 意图→工具链绑定层：必须先于 router.Setup——router.Setup 内的
	// InitInferenceOrchestrator 将其注入规划器（nil 时规划器回落硬编码，零行为变化）。
	app.InitIntentChainRuntime(db.GetDB())

	// TB-05 改单申请单：必须先于 router.Setup（工具提供者在 router.Setup 内
	// 经 GetOrderChangeService 决定是否注册 order.modify_request 高风险工具）。
	app.InitOrderChangeRuntime(db.GetDB())

	router.Setup(r, db.GetDB())

	// W-5 挽回队列消费：装配落在 internal/app（见 recovery_worker_wiring.go），
	// 开关 FF_LTC_RECOVERY_WORKER=off（默认）时 Start 直接 no-op。
	//
	// 为什么必须排在 router.Setup **之后**：worker 的外发要挂 T-P3-07 的闸门，
	// 而闸门的裁决来源（W-1 白名单 checker）是 router.Setup 里的 InitGlobalToolExecutor
	// 才建的。排在前面不会 panic、也不会报错，只会留下一条"依赖未成立 ⇒ 不装门"的告警 ——
	// 也就是 cron 这条外发路径静默地不受审批约束。启动顺序在这里是**功能的一部分**，
	// 不是可挪动的细节。
	if recoveryWorker := app.InitRecoveryWorker(db.GetDB()); recoveryWorker != nil {
		defer recoveryWorker.Stop(context.Background())
	}

	// T-P7-03 催收腿：逾期扫描 → 提醒外发 → 升级待办。装配落在 internal/app
	// （见 collection_wiring.go），开关 FF_LTC_COLLECTION_JOB=off（默认）时协程不起，
	// 但实例照装 —— 观测端点 /api/manage/ltc/collection 要答得出"现在配的是哪一档"。
	//
	// 顺序约束与上一行**同一条**、也同样必须守住：必须在 router.Setup 之后。催收的提醒
	// 走的是 ReachByCustomer 这个唯一外发出口，它身上的审批闸门要读 W-1 的 checker，
	// 而那个 checker 是 router.Setup 里 InitGlobalToolExecutor 才建的。装在前面不会 panic，
	// 只会留下一条告警，然后每一条催收提醒都在没人批准的情况下出域。
	// （cmd/api 的 TestCollectionJobMountedAfterRouterSetup 把这两件事钉成断言。）
	if collectionJob := app.InitCollectionRuntime(db.GetDB()); collectionJob != nil {
		defer collectionJob.Stop(context.Background())
	}

	// Telegram polling 的分布式锁在关停时必须交回：polling 协程随进程消失后，
	// DB 里的 polling_owner 仍写着这个已死进程，60s 心跳陈旧窗口内启动的新实例会判定
	// 「锁被其他实例持有」而整轮不启动 polling —— 表现是重启后 Telegram 一条消息都不进，
	// 且要再重启一次才恢复。登记在持久层 Close 之后 ⇒ 本 defer 先执行，释放语句仍进得了库。
	defer service.StopAllTelegramPolling()

	// TG 群门控清扫器的租约同样要在关停时交回：cron_job_leases 里那一行若仍写着已死进程，
	// 其他实例要等租约陈旧窗口（3 分钟）才敢接手，期间没有一个人清理超时未验证的成员；
	// 反过来"协程还没退出就释放"会留下双跑窗——两个实例同时对真人移出群。
	// StopGateSweeper 内部就是按"先等退出、再释放"这个顺序写的，登记位置与上一行同一条
	// 约束（排在持久层 Close 之后 ⇒ LIFO 下先执行，释放语句仍进得了库）。
	defer service.StopGateSweeper(context.Background())

	addr := resolveListenAddr(os.Getenv("SERVER_HOST"), os.Getenv("PORT"))
	logger.Infof("营销后端服务启动于 %s", addr)
	// serveHTTP 按平台拆分:Unix 走 endless(零停机热重启),Windows 走标准 http
	// (见 serve_unix.go / serve_windows.go)
	if err := serveHTTP(addr, r); err != nil && !isGracefulShutdownErr(err) {
		panic("服务启动失败：" + err.Error())
	}
}

func isGracefulShutdownErr(err error) bool {
	return errors.Is(err, http.ErrServerClosed) ||
		strings.Contains(err.Error(), "use of closed network connection")
}

func registerEventSubscribers() {
	bus := event.GetGlobalBus()
	if bus == nil {
		logger.Warn("[event] global bus is nil, subscriptions skipped")
		return
	}

	if os.Getenv("AGENT_RUNTIME_BUS_ENABLED") == "true" {
		rt := agent_runtime.NewAgentRuntime(nil, nil, nil)
		// 模块闸包装（FR-5.6）：ai-agent 模块在本实例上对任一租户开通才消费
		// 客户消息事件。注意本订阅默认 env 关闭（见下方 else 注释），包装是
		// 为"打开它时也在闸内"预备的挂点。
		agentHandler := service.WrapSubscriber(model.ModuleAIAgent, agent_runtime.NewEventSubscriber(rt))
		bus.Subscribe(event.TopicCustomerMessageReceived, agentHandler)
		logger.Info("[event] subscribed: customer.message.received -> agent_runtime (AGENT_RUNTIME_BUS_ENABLED=true)")
	} else {
		logger.Info("[event] customer.message.received 总线订阅关闭(默认)：由同步主链路处理，避免僵尸订阅者与双触发地雷")
	}

	indexer := rag.NewIncrementalIndexer(nil, nil, db.GetDB())
	bus.Subscribe(event.TopicKnowledgeDocumentChanged, indexer.Handle)
	logger.Info("[event] subscribed: knowledge.document.changed -> rag.IncrementalIndexer")

	chainSync := geoservice.NewInboxChainSync(georepo.NewGeoQueryChainRepositoryWithDB(db.GetDB()))
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
