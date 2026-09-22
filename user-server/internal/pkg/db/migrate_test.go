package db

import (
	"fmt"
	"testing"

	geomodel "hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	ragcachemodel "hivemtk-user/internal/aiagent/rag/cache"
	browsermodel "hivemtk-user/internal/browser_automation/model"
)

// TestAllModels_CoversModelsWithWritePaths 防止「模型有生产写入路径、却没登记建表」复发。
//
// 背景（2026-09-16 审计 DB-07）：GeoCrawlerVisit 是 29 个 geo 模型里**唯一没登记**
// 进 allModels() 的，而 internal/router/router.go 的 AICrawlerMonitor 回调会
// fire-and-forget 地写入它、且 error 被 `_ =` 丢弃 —— 全新部署不建
// geo_crawler_visits 表 ⇒ 统计永久为 0，且**日志里一行都没有**。
//
// 该缺陷不会在本地暴露：开发库里的表是历史遗留/测试误写留下的（见 TEST-06 同源问题），
// 只有全新部署才会现形。因此必须有这条用例替它守着。
//
// 新增模型时：只要它有**生产写入路径**，就把类型加进 mustCover。
func TestAllModels_CoversModelsWithWritePaths(t *testing.T) {
	// 注：KBDocumentChunkRow 与 TraceEvent 不在此列 —— 它们所在包 import 了
	// internal/pkg/db，无法反向在 migrate.go 登记，改由各自包的 init()
	// 调用 RegisterExtraModels（见 internal/repository/kb_document_chunk_repo.go
	// 与 internal/aiagent/llm/trace_context.go）。本测试包不 import 这两个包，
	// 其 init 不会执行，故在此断言会误报。
	mustCover := []any{
		&geomodel.GeoCrawlerVisit{},

		&model.AggregationWatermark{},
		&model.AlertHistory{},
		&model.AlertRule{},
		// ApprovalRequest：T-P3-01 新增，有 repository.approvalRequestRepo.Insert 这条
		// 生产写入路径（本卡虽未装配，登记建表与登记写入路径是两件事）。
		&model.ApprovalRequest{},
		&model.BanditRefluxLog{},
		// Bill：T-P7-01 新增，有 repository.billRepo.Create 这条生产写入路径
		// （报价被接受时派生应收）。列入 mustCover 而不是只信 allModels() 里那一行，
		// 是因为这张表的失败面恰好是"表没建、代码全对"：客户接受那一格照样落库，
		// 只有账单这一侧在日志里留一句话 —— 而它是给钱的那张纸。
		&model.Bill{},
		&model.ChurnScore{},
		&model.ClueEngagementEvent{},
		&model.ClueScore{},
		&model.ConfigParamAuditLog{},
		&model.CustomerChannel{},
		// HumanTask：T-P3-03 新增。列入 mustCover 而不是只信 allModels() 里那一行，
		// 是因为待办这张表的失败面恰好是"表没建、代码全对"：转人工那条投递只在日志里
		// 说一句"投递失败"，会话侧一切正常，而池子里永远没有行。
		&model.HumanTask{},
		&model.IntegrationTemplate{},
		&model.IntentExample{},
		&model.LLMRoutingLog{},
		&model.LoginEvent{},
		// OrderDraft：T-P2-01 新增，有 repository.Upsert 这条生产写入路径。
		// 不列进这里的后果正是本测试要拦的那类：allModels() 里少一行，
		// 全新部署不建表，而 service 侧只在日志里说一句话。
		&model.OrderDraft{},
		// Opportunity：T-P4-01 新增。本卡只有列与值域、还没有生产者，
		// 列进 mustCover 是为了拦"下张卡接了写入却没人回来登记"——
		// 建表登记这一行如果漂掉，T-P4-05 的建商机只会在日志里留一句话。
		&model.Opportunity{},
		&model.PasswordHistory{},
		// Payment：T-P7-02 新增，有 repository.paymentRepo.Create 这条生产写入路径
		// （订单 webhook 带回款格子时入账）。列进 mustCover 的理由比 bills 那条更硬：
		// 这张表没建出来时，"已收多少"恒等于 0，而账单会永远停在 open/partial ——
		// 那不是"少一张表"，是**欠额被记成已收的反面**（应收一直收不齐），
		// 且回款是回调驱动的，没有人在对面看着报错，只有重试三轮之后的一句 Warn。
		&model.Payment{},
		&model.RagMetricsDaily{},
		&model.RecoveryQueue{},
		&model.SecurityAlert{},
		&model.SystemConfigKV{},
		&model.ToolCallAudit{},
		&model.UserMFA{},
		&model.WorkflowExecution{},
		&model.WorkflowNodeExecution{},
		&model.WorkflowVersion{},

		&browsermodel.BrowserCommandLog{},
		&browsermodel.BrowserCronTrigger{},
		&browsermodel.BrowserLLMPlan{},
		&browsermodel.BrowserSession{},
		&browsermodel.BrowserStep{},
		&browsermodel.BrowserTask{},

		&ragcachemodel.RAGAnswerCache{},
	}

	registered := make(map[string]bool, 512)
	for _, m := range append(allModels(), ExtraModels()...) {
		registered[fmt.Sprintf("%T", m)] = true
	}

	for _, m := range mustCover {
		if !registered[fmt.Sprintf("%T", m)] {
			t.Errorf("模型 %T 有生产写入路径但未登记进 allModels()：全新部署不会建表，写入将静默失败", m)
		}
	}
}

// TestAutoMigrate_Complete 测试完整 AutoMigrate 不应 panic
func TestAutoMigrate_Complete(t *testing.T) {
	testDB := testutil.NewTestDB(t)
	if testDB == nil {
		t.Fatal("NewTestDB returned nil")
	}

	originalDB := DB
	DB = testDB
	defer func() {
		DB = originalDB
	}()

	db := AutoMigrate()
	if db == nil {
		t.Error("AutoMigrate returned nil")
	}
}

// TestAutoMigrate_Partial 测试部分模型迁移（直接调用 testDB.AutoMigrate）
func TestAutoMigrate_Partial(t *testing.T) {
	testDB := testutil.NewTestDB(t, &model.User{})
	if testDB == nil {
		t.Fatal("NewTestDB returned nil")
	}
	if err := testDB.AutoMigrate(&model.User{}); err != nil {
		t.Errorf("migrate User: %v", err)
	}
}

// TestAutoMigrate_MultipleTables 测试多表迁移
func TestAutoMigrate_MultipleTables(t *testing.T) {
	testDB := testutil.NewTestDB(t,
		&model.User{},
		&model.Account{},
		&model.Order{},
	)
	if testDB == nil {
		t.Fatal("NewTestDB returned nil")
	}
	if err := testDB.AutoMigrate(&model.User{}, &model.Account{}, &model.Order{}); err != nil {
		t.Errorf("migrate: %v", err)
	}
}

// TestAutoMigrate_Idempotent 测试迁移的幂等性
func TestAutoMigrate_Idempotent(t *testing.T) {
	testDB := testutil.NewTestDB(t, &model.User{})
	if testDB == nil {
		t.Fatal("NewTestDB returned nil")
	}
	if err := testDB.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := testDB.AutoMigrate(&model.User{}); err != nil {
		t.Errorf("second migrate failed: %v", err)
	}
}

// TestAutoMigrate_WithIndexes 测试带索引的迁移
func TestAutoMigrate_WithIndexes(t *testing.T) {
	testDB := testutil.NewTestDB(t, &model.User{}, &model.Account{})
	if testDB == nil {
		t.Fatal("NewTestDB returned nil")
	}
	if err := testDB.AutoMigrate(&model.User{}, &model.Account{}); err != nil {
		t.Errorf("migrate: %v", err)
	}
}

// TestAutoMigrate_NilDB 测试 nil DB 情况
func TestAutoMigrate_NilDB(t *testing.T) {
	originalDB := DB
	DB = nil
	defer func() {
		DB = originalDB
	}()

	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic for nil DB")
		}
	}()

	AutoMigrate()
}

// TestAutoMigrate_EmptyModel 测试空模型列表
func TestAutoMigrate_EmptyModel(t *testing.T) {
	testDB := testutil.NewTestDB(t)
	if testDB == nil {
		t.Fatal("NewTestDB returned nil")
	}
	if err := testDB.AutoMigrate(); err != nil {
		t.Errorf("migrate empty: %v", err)
	}
}

// TestAutoMigrate_LargeModel 测试多字段模型迁移
func TestAutoMigrate_LargeModel(t *testing.T) {
	testDB := testutil.NewTestDB(t, &model.User{}, &model.Account{}, &model.Order{}, &model.Message{})
	if testDB == nil {
		t.Fatal("NewTestDB returned nil")
	}
	if err := testDB.AutoMigrate(&model.User{}, &model.Account{}, &model.Order{}, &model.Message{}); err != nil {
		t.Errorf("migrate: %v", err)
	}
}

// 并发迁移用例的夹具表：三张互不引用、列极少的窄表，
// 让并发 DDL 只各自持自己那张 relation 上的锁。
type concMigFixtureA struct {
	ID   uint `gorm:"primaryKey"`
	Name string
}

func (concMigFixtureA) TableName() string { return "zz_concmig_a" }

type concMigFixtureB struct {
	ID   uint `gorm:"primaryKey"`
	Name string
}

func (concMigFixtureB) TableName() string { return "zz_concmig_b" }

type concMigFixtureC struct {
	ID   uint `gorm:"primaryKey"`
	Name string
}

func (concMigFixtureC) TableName() string { return "zz_concmig_c" }

// concMigCase 一条并发迁移输入：表名只用于把报错定位到具体那张表。
type concMigCase struct {
	table string
	model interface{}
}

// TestAutoMigrate_ConcurrentDistinctModels 并发迁移互不引用的表：既不得报错，也不得被 -race 抓到竞态。
//
// 为什么不再是"3 个协程迁同一个模型"（原 TestAutoMigrate_ConcurrentAccess）：
// gorm 每跑一次 AutoMigrate 都会对缓存里那个 *Schema 无锁改写 Indexes
// （实测栈 schema.(*Schema).ParseIndexes index.go:74 ↔ migrator/migrator.go:140），
// 同一个句柄上并发迁同一个模型属于"必撞 -race"的断言：本地
// `-race -count=40` 复现 3/40 红，CI run 486 的 core job 是同一条栈。
// 生产侧不存在这一形态——pkg/db 的 AutoMigrate() 是 for 循环逐模型串行
// （migrate.go:424），多实例同时启动时撞已存在的表另由
// isTolerableMigrateError + createTableFallback 承接，因此这条用例守的是
// "并发用同一句柄建不同的表"这一层 gorm 真给了的保证。
func TestAutoMigrate_ConcurrentDistinctModels(t *testing.T) {
	testDB := testutil.NewTestDB(t, &model.User{})
	if testDB == nil {
		t.Fatal("NewTestDB returned nil")
	}
	fixtures := []concMigCase{
		{"zz_concmig_a", &concMigFixtureA{}},
		{"zz_concmig_b", &concMigFixtureB{}},
		{"zz_concmig_c", &concMigFixtureC{}},
	}

	type result struct {
		table string
		err   error
	}
	// release 是起跑栅栏：三个协程都阻塞在这里，close 之后同一刻进 AutoMigrate，
	// 把并发窗口拉到最大（否则快的那个先跑完，用例退化成串行还不自知）。
	release := make(chan struct{})
	done := make(chan result, len(fixtures))
	for _, f := range fixtures {
		go func() {
			<-release
			done <- result{table: f.table, err: testDB.AutoMigrate(f.model)}
		}()
	}
	close(release)

	for range fixtures {
		r := <-done
		if r.err != nil {
			t.Errorf("并发迁移 %s: %v", r.table, r.err)
		}
	}
}

// TestAutoMigrate_VeryLargeNumberOfModels 测试较多模型同时迁移
func TestAutoMigrate_VeryLargeNumberOfModels(t *testing.T) {
	testDB := testutil.NewTestDB(t,
		&model.User{},
		&model.Account{},
		&model.Order{},
		&model.Message{},
		&model.Clue{},
		&model.DouyinCard{},
	)
	if testDB == nil {
		t.Fatal("NewTestDB returned nil")
	}
	if err := testDB.AutoMigrate(
		&model.User{},
		&model.Account{},
		&model.Order{},
		&model.Message{},
		&model.Clue{},
		&model.DouyinCard{},
	); err != nil {
		t.Errorf("migrate: %v", err)
	}
}
