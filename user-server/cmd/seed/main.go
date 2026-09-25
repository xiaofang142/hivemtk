// Command seed 向 user-server 数据库写入演示种子数据，覆盖 11 个模块、30+ 张表。
//
// 用法：
//
// cd user-server && go run ./cmd/seed # 写入全部种子数据（幂等）
// cd user-server && go run ./cmd/seed --clean # 仅清空种子数据（按依赖逆序删除）
// cd user-server && go run ./cmd/seed --module=customers # 仅写入指定模块
// cd user-server && go run ./cmd/seed --list # 列出所有可用模块
//
// 种子数据写入路径（仅三条，无运行期自动播种）：
//  1. 本 cmd/seed 命令（开发演示种子，11 个模块；平台客服知识库的 rag_products 注册行也由它补齐）
//  2. **人工执行**的 SQL 种子：migrations/031_platform_cs_rag_seed.sql（平台客服 RAG 知识库
//     的 13 篇文档与分段）、032_industry_assets_local_seed.sql（行业资产包）、
//     033_industry_ai_agents_seed.sql（行业智能体）。这三份 .sql 没有任何自动执行路径
//     （compose 的 initdb.d 只挂 init-user-db.sql，Go 侧迁移器只跑 GORM AutoMigrate + Go 任务、
//     从不读 .sql，scripts/bootstrap.sh 只点名 027/028），要 `psql -v ON_ERROR_STOP=1 -f` 手动跑；
//     口径真值见 migrations/README.md
//  3. scripts/seed/*.py 批量灌入脚本（知识库分段、FAQ/SOP 与智能体绑定）
//
// 设计要点：
// 使用 GORM 模型直接写入，避免 SQL 列错位（与 platform-server/seed 一致）
// 幂等可重入：每张表先按特征条件清理 demo 数据再重新写入
// 按外键依赖顺序执行：A 用户 → B 客户 → C 会话 → D AI → E 触达 → F 线索 → G 资产 → H 消息 → I LLM → J 统计
// 演示数据集中在过去 30 天，所有状态枚举均有覆盖
//
// 模块清单（11 个）：
//
// A. users - 系统用户、坐席状态
// B. customers - 客户、标签、RFM、事件、长期记忆、挽回队列
// C. sessions - 客服会话、消息、AI建议、黑名单、快捷回复
// D. ai_agents - 智能体、绑定、对话记忆、意图、SOP、话术（+ 平台客服 RAG 产品注册行）
// E. reach - 触达Pipeline、任务、邮件、短信、卡片、收件箱
// F. clues - 线索、评分
// G. assets - 资产包、活码、短链、域名池
// H. messages - 统一消息、平台账号、消息中台
// I. llm_logs - LLM 路由日志、审计
// J. stats - 漏斗、销冠画像、账号健康度
// K. faq_sop - FAQ 知识库与 SOP 模板（电商客服方向）
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"hivemtk-user/internal/config"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormLogger "gorm.io/gorm/logger"
)

// Seeder 模块接口
type Seeder interface {
	Name() string
	Description() string
	Clean(database *gorm.DB) error
	Seed(database *gorm.DB, ctx *SeedContext) error
}

// SeedContext 跨模块共享的上下文（保存已写入的关键 ID 供后续模块引用）
type SeedContext struct {
	AdminUserID    uint
	CSUserIDs      []uint
	StaffUserIDs   []uint
	AgentStatusIDs []uint

	CustomerIDs         []string
	ChampionCustomerIDs []string
	ChurnCustomerIDs    []string
	TagIDs              []string

	SessionIDs         []string
	ActiveSessionIDs   []string
	ResolvedSessionIDs []string

	AIAgentIDs       []uint
	SOPAgentIDs      []uint
	RagProductIDs    []string
	ScriptLibraryIDs []uint

	ReachPipelineIDs []uint
	ReachJobIDs      []uint
	InboxConvIDs     []uint

	DomainPoolIDs  []int
	ShortLinkIDs   []uint
	LiveCodeIDs    []string
	AssetBundleIDs []string

	PlatformAccountIDs []uint
	UnifiedMessageIDs  []uint64

	LLMRoutingLogIDs []int64

	FunnelIDs       []uint64
	SalesPersonaIDs []uint64

	ClueIDs []string
}

// 所有注册的 seeder（按依赖顺序）
//
// 已实现 users/customers/sessions/ai_agents/reach/clues/assets/messages/llm_logs/stats；
// 新增 faq_sop（FAQ 知识库 + SOP 模板，电商客服方向，60+ FAQ / 30+ SOP）
// 全部 11 个模块均已就绪，按依赖顺序执行。
var allSeeders = []Seeder{
	&usersSeeder{},
	&customersSeeder{},
	&sessionsSeeder{},
	&aiAgentsSeeder{},
	&reachSeeder{},
	&cluesSeeder{},
	&assetsSeeder{},
	&messagesSeeder{},
	&llmLogsSeeder{},
	&statsSeeder{},
	&faqSopSeeder{},
}

func main() {
	var (
		cleanOnly  = flag.Bool("clean", false, "仅清空所有种子数据，不写入")
		moduleFlag = flag.String("module", "", "仅执行指定模块（逗号分隔，如 customers,sessions）")
		listFlag   = flag.Bool("list", false, "列出所有可用模块后退出")
	)
	flag.Parse()

	if *listFlag {
		fmt.Println("可用种子数据模块（按执行顺序）：")
		for i, s := range allSeeders {
			fmt.Printf("  %2d. %-12s - %s\n", i+1, s.Name(), s.Description())
		}
		return
	}

	database := initDB()

	targets := selectSeeders(*moduleFlag)

	ctx := &SeedContext{}

	if *cleanOnly {
		log.Println("=== 开始清空种子数据（按依赖逆序）===")
		for i := len(targets) - 1; i >= 0; i-- {
			s := targets[i]
			log.Printf("[CLEAN] %s ...", s.Name())
			if err := s.Clean(database); err != nil {
				log.Printf("  ✗ 清空失败: %v", err)
			} else {
				log.Printf("  ✓ 已清空")
			}
		}
		log.Println("=== 清空完成 ===")
		return
	}

	log.Println("=== 阶段1：清空旧种子数据（按依赖逆序）===")
	for i := len(targets) - 1; i >= 0; i-- {
		s := targets[i]
		if err := s.Clean(database); err != nil {
			log.Printf("  [CLEAN] %s 失败: %v", s.Name(), err)
		}
	}

	log.Println("=== 阶段2：写入种子数据（按依赖顺序）===")
	var success, failed int
	for _, s := range targets {
		log.Printf("[SEED] %s ...", s.Name())
		if err := s.Seed(database, ctx); err != nil {
			log.Printf("  ✗ 失败: %v", err)
			failed++
			continue
		}
		log.Printf("  ✓ 完成 - %s", s.Description())
		success++
	}

	log.Printf("=== 种子数据写入完成：成功 %d / 失败 %d / 总计 %d ===", success, failed, len(targets))
}

// initDB 初始化数据库连接（独立初始化，不走全局 db.InitDB 避免影响其他依赖）
func initDB() *gorm.DB {
	cfg := config.GetAppConfig()
	pg := cfg.Database.Postgres
	if pg.Host == "" {
		log.Fatalf("[FATAL] 缺少 config.yaml 或 database.postgres.host 为空；config.yaml 由本仓库直接提供" +
			"（user-server/config.yaml，仓库里没有 config.yaml.example，无需 cp），" +
			"按 DEVELOPMENT.md §2.4 核对端口（dev 本机 PG=8232，docker=8202），" +
			"或用 DB_HOST / DB_PORT 环境变量覆盖")
	}
	if v := os.Getenv("POSTGRES_PASSWORD"); v != "" {
		pg.Password = v
	}
	if pg.Password == "" {
		log.Fatalf("[FATAL] 缺少 POSTGRES_PASSWORD 环境变量（合规基线 §7.2，密码不落配置文件）")
	}
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%d sslmode=%s TimeZone=Asia/Shanghai",
		pg.Host, pg.User, pg.Password, pg.DBName, pg.Port, pg.SSLMode)
	database, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: gormLogger.Default.LogMode(gormLogger.Warn),
	})
	if err != nil {
		log.Fatalf("数据库连接失败（host=%s port=%d db=%s）: %v", pg.Host, pg.Port, pg.DBName, err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		log.Fatalf("获取 SQL DB 失败: %v", err)
	}
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(50)
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	log.Printf("数据库已连接：host=%s port=%d db=%s", pg.Host, pg.Port, pg.DBName)
	return database
}

// selectSeeders 根据命令行参数选择要执行的 seeder
func selectSeeders(moduleFlag string) []Seeder {
	if moduleFlag == "" {
		return allSeeders
	}
	names := strings.Split(moduleFlag, ",")
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[strings.TrimSpace(n)] = true
	}
	var picked []Seeder
	for _, s := range allSeeders {
		if want[s.Name()] {
			picked = append(picked, s)
		}
	}
	if len(picked) == 0 {
		fmt.Fprintf(os.Stderr, "未找到匹配的模块：%s\n可用模块：\n", moduleFlag)
		for _, s := range allSeeders {
			fmt.Fprintf(os.Stderr, "  - %s\n", s.Name())
		}
		os.Exit(1)
	}
	return picked
}
