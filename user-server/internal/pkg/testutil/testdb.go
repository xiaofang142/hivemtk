// Package testutil 提供统一的 PostgreSQL 测试数据库辅助
// 本系统仅使用 PostgreSQL，测试环境必须连接真实 PG（不允许任何非 PG 数据库）。
//
// 连接配置来源（优先级）：
//  1. POSTGRES_TEST_DSN（完整连接串，显式覆盖一切）
//  2. POSTGRES_TEST_HOST / POSTGRES_TEST_PORT / POSTGRES_TEST_USER / POSTGRES_TEST_PASSWORD
//  3. 宿主机端口回落到 USER_POSTGRES_HOST_PORT / DB_PORT，再回落到
//     config.DefaultDBPortDev(8232) / config.DefaultDBPortDocker(8202) 的实际探测
//
// 端口探测存在的理由见 testDBPortCandidates 注释（避免硬编码 8202 导致本地假绿）。
//
// 性能口径（2026-11-05）：
//   - 进程级只建一个库；
//   - 每个用例开一个连接池，cleanup Close（隔离跨用例异步写）；
//   - 每个模型在进程内只 AutoMigrate 一次，后续用例用 TRUNCATE 清空；
//   - 租户索引规范化/登记表只在新模型加入时重跑；
//   - TESTDB_LEGACY=1 可切回旧的「每用例 DropTable+AutoMigrate」路径。
package testutil

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/pkg/dbstate"
	"hivemtk-user/internal/pkg/tenancyidx"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var dbnameRe = regexp.MustCompile(`dbname=[^\s]+`)

var (
	dsnHostRe = regexp.MustCompile(`host=([^\s]+)`)
	dsnPortRe = regexp.MustCompile(`port=([0-9]+)`)
)

var (
	procDBName    string
	procDBInit    sync.Once
	procDBInitErr error

	// procDBMaintConn 是持有槽位咨询锁的会话。必须包级持有：
	// ensureProcTestDB 返回后局部变量即不可达，GC 一旦回收连接，
	// 会话级咨询锁就随之释放，另一个测试进程会抢到同一槽位并
	// DROP DATABASE 正在使用的库（Makefile 里“去掉 -p 1 后 4 个包同时挂起”
	// 的历史形态与此一致）。连接活到进程退出，由 OS 关闭。
	// 只写不读是刻意的：SlotLockHeld 提供只读诊断，供测试断言它没被回收。
	procDBMaintConn *sql.Conn
)

// SlotLockHeld 报告本进程是否仍持有槽位咨询锁的维护连接。
// 仅供测试/诊断：连接一旦被 GC 回收，会话级咨询锁会提前释放，
// 并发测试进程可能撞到同一槽位库。
func SlotLockHeld() bool {
	return procDBMaintConn != nil
}

// endpoint 解析结果进程级缓存：同一进程里数百次 NewTestDB 不需要反复
// 做 2s 超时的 TCP 探测。失败不缓存，允许 PG 中途启动后重试。
var (
	endpointMu     sync.Mutex
	endpointHost   string
	endpointPort   string
	endpointCached bool
)

// schema 登记表：key 为实际表名（GORM 的 TableName() 结果）。
// 首次见到的表才 AutoMigrate；后续用例只 TRUNCATE。
var (
	schemaMu       sync.Mutex
	migratedTables = map[string]bool{}
)

// testDBPortCandidates 返回宿主机测试库端口候选（按优先级去重）。
//
// ⚠️ 为什么不能像原实现那样写死单一端口 8202：
//   - 8202 是 **容器内** 端口。宿主机映射端口由 docker-compose 的
//     `ports: "127.0.0.1:${USER_POSTGRES_HOST_PORT:-8202}:8202"` 决定，
//     本机 .env 覆盖为 8232（见 docs/PORT_REGISTRY.md §一 / §二）。
//   - 写死 8202 时，本机 `go test ./...` 连不上 → 依赖本辅助的 288 个测试文件
//     全部 t.Skipf → go test 退出 0，断言变假绿（同源问题，
//     只是触发点从 CI 换成了本地）。
//   - docs/PORT_REGISTRY.md 已把"硬编码 8202 但 ports.go 说 8232"列为缺陷模式，
//     并记录了 2026-09 CV 验收时因此产生大面积假失败的历史事故。
//
// 常量取自 config 包（DefaultDBPortDev / DefaultDBPortDocker），与 ports.go 同源，
// 避免文档与代码各说各话。
func testDBPortCandidates() []string {
	out := make([]string, 0, 5)
	seen := make(map[string]bool, 5)
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	add(os.Getenv("POSTGRES_TEST_PORT"))          // 显式覆盖（CI 注入 5432）
	add(os.Getenv("USER_POSTGRES_HOST_PORT"))     // 与 docker-compose / .env 同源
	add(os.Getenv("DB_PORT"))                     // 与 config.yaml 同源
	add(strconv.Itoa(config.DefaultDBPortDev))    // 8232 宿主机直连
	add(strconv.Itoa(config.DefaultDBPortDocker)) // 8202 容器内 / 默认映射
	return out
}

func dialTestDB(host, port string) error {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 2*time.Second)
	if err != nil {
		return err
	}
	return conn.Close()
}

// resolveTestDBEndpoint 选出实际可建连的宿主机测试库端点。
//
// 返回 (host, port, err)。err 非 nil 表示所有候选都不可达，此时 port 为首个候选，
// 便于错误信息指向最可能的配置项。
func resolveTestDBEndpoint() (string, string, error) {
	endpointMu.Lock()
	defer endpointMu.Unlock()
	if endpointCached {
		return endpointHost, endpointPort, nil
	}
	host, port, err := resolveTestDBEndpointUncached()
	if err == nil {
		endpointHost, endpointPort, endpointCached = host, port, true
	}
	return host, port, err
}

func resolveTestDBEndpointUncached() (string, string, error) {
	// POSTGRES_TEST_DSN 显式给出完整连接串时以它为准，不再猜端口。
	if dsn := os.Getenv("POSTGRES_TEST_DSN"); dsn != "" {
		host := getEnvOr("POSTGRES_TEST_HOST", "127.0.0.1")
		port := strconv.Itoa(config.DefaultDBPortDocker)
		if m := dsnHostRe.FindStringSubmatch(dsn); m != nil {
			host = m[1]
		}
		if m := dsnPortRe.FindStringSubmatch(dsn); m != nil {
			port = m[1]
		}
		return host, port, dialTestDB(host, port)
	}

	host := getEnvOr("POSTGRES_TEST_HOST", "127.0.0.1")
	cands := testDBPortCandidates()
	var firstErr error
	for _, p := range cands {
		err := dialTestDB(host, p)
		if err == nil {
			return host, p, nil
		}
		if firstErr == nil {
			firstErr = fmt.Errorf("%s:%s: %w", host, p, err)
		}
	}
	return host, cands[0], firstErr
}

// testDBMaxOpenConns／testDBMaxIdleConns 给本辅助开出的每个句柄上连接池上界。
//
// 为什么必须有上界：database/sql 的 MaxOpenConns 默认是 **不限**，于是一次用例里同时
// 发出多少条查询就向服务器要多少条连接。实测（本机 8232 容器、`-race -p 1`、只跑
// internal/service/async_db_handle_probe_test.go 的三枚 300 轮探针，采样
// `count(*) from pg_stat_activity where datname like 'user_db_test%'`，每格采样前基线 0～1 条）：
//   - 不上界（HEAD 字节，两格复跑）：峰值 40 与 71 条；
//   - 上界 32：峰值 2 与 22 条。三枚探针在两格下都照旧 PASS，耗时同量级（上界没有把
//     扇出排队放大到看得出来的程度）。
//
// 为什么本地从来撞不上、CI 撞得上：开发容器把 max_connections 调成 500
// （docker-compose.yml:69），而 CI 的 services.postgres 只写了 health-cmd，整个 workflow
// 里 "max_connections" 出现 0 次 ⇒ 用镜像默认 100。于是 CI 里整包跑时探针的扇出先把连接
// 名额吃光，下一枚探针建句柄即红：`FATAL: sorry, too many clients already (SQLSTATE 53300)`
// —— CI 上曾复现：红因正落在 async_db_handle_probe_test.go:76（第三枚探针的
// NewTestDB 调用行）。这条"本地与 CI 的 PG 参数不同源"与 8202／8232 端口漂移是同一族
// （见 testDBPortCandidates 注释）：本地绿在这类形状上不构成依据。
//
// 32 的取法：真正封顶的是 MaxOpenConns（只留 MaxIdleConns=8、上界拿掉时峰值仍是 66），
// 数值取在 CI 名额 100 的 1/3 —— 同进程里先后存世的几个句柄要各自留位置（探针是
// fire-and-forget，上一条用例的协程要到下一条用例期间才归还连接）。它同时高于 pkg/db 的
// 缺省兜底 20（internal/pkg/db/db.go:74），免得"只有测试里才有的并发"被一个比生产还紧的
// 上界卡出生产上看不到的排队形状；生产 config 的默认值是另一个数 200
// （internal/config/server.go:80），那是线上单实例的池，不参与这里的取值论证。
// MaxIdleConns=8 只为少建几次后端，不承担上界职责。
const (
	testDBMaxOpenConns = 32
	testDBMaxIdleConns = 8
)

// NewTestDB 返回当前进程共享的测试库句柄，并保证本次 models 对应的表可见且为空。
//
// 行为：
//  1. 首次调用时在维护库（postgres）创建独立库（先清理同名残留）。
//     库名取自一组固定槽位（user_db_test_slot<N>），由会话级咨询锁分配，
//     因此库总数有界；详见 procTestDBSlots 注释（RISK-07）。
//  2. 每个用例开一个连接池（池上界 32/8），cleanup 时 Close；
//     不跨用例共享池——上一用例的异步 goroutine 不能写进下一用例的表。
//  3. 每个模型在进程内只 AutoMigrate 一次；之后每次调用对本次 models
//     执行 `TRUNCATE ... RESTART IDENTITY CASCADE`，语义等价旧实现的
//     “DROP 后重建空表”（含序列重置）。
//  4. 租户索引规范化/租户登记表只在新模型加入时重跑；用例自己 DROP/RENAME
//     过表时 TRUNCATE 会失败，此时回退 DROP+AutoMigrate 并重试。
//  5. 通过 t.Cleanup 还原 dbstate 并关闭本用例连接池。
//
// 优雅降级：当 PostgreSQL 测试库不可达时（如本地开发无 PG），
// 自动调用 t.Skipf 跳过本测试，保证 go test 不会因环境差异大面积失败。
// 强依赖真实 DB 的集成/E2E 测试在此场景下整体跳过，不影响纯单元测试。
// 注意：CI 环境（CI 环境变量非空）下改为 t.Fatalf —— 见函数体内注释。
//
// 注意：本辅助在创建成功后会自动 dbstate.Set(database) 并在 t.Cleanup 里还原为 nil。
//
// Service 层构造器已统一改为无参 + 全局
// dbUtil.GetDB()/repository.GetDB() 注入，不再有 WithDB 变体。若本辅助不注入全局，
// 每个用例都要手工 SetTestDB，漏一处就是 nil 指针 panic（已实测踩过：service 包
// 130+ 用例因遗漏而失败）。因此把注入收口到这里；已有手工 SetTestDB 的用例不受影响
// （重复设置幂等，Cleanup LIFO 先还原手工的 nil 再还原本辅助的 nil）。
//
// 参数类型是 testing.TB 而非 *testing.T：benchmark 同样需要测试库。若只接受
// *testing.T，基准测试就只能自带第 N 套 DSN 拼装（见 internal/repository/
// message_hub_inbox_outbound_p0_5_bench_test.go 的旧写法：默认端口 8202、
// 默认口令 postgres、还认第五个变量名 TEST_DATABASE_URL），结果是基准静默 Skip。
// 放宽为 testing.TB 后测试与基准共用同一引导入口；*testing.T 满足 testing.TB，
// 既有调用方无需改动。
func NewTestDB(t testing.TB, models ...any) *gorm.DB {
	t.Helper()

	// TESTDB_TIMING=1 时输出分阶段耗时，用于定位整套用例的建库成本。
	// 默认关闭，不改变任何行为。
	started := time.Now()
	last := started
	var tEndpoint, tEnsure, tOpen, tSettings, tDrop, tMigrate, tNormalize, tRegistry, tTruncate time.Duration
	newCount := 0

	host, port, dialErr := resolveTestDBEndpoint()
	if dialErr != nil {
		// ⚠️ CI 环境下禁止静默跳过。
		//
		// 背景：CI（.github/workflows/user-server-ci.yml）注入的是 POSTGRES_HOST/PORT/
		// USER/PASSWORD，而本辅助读取的是 POSTGRES_TEST_* 前缀 —— 两者变量名不匹配，
		// 于是 CI 里 PG 监听在 5432、这里却去连默认的 8202，DialTimeout 必然失败，
		// 依赖本辅助的 **288 个测试文件全部走 t.Skipf**，`go test` 依然退出 0。
		// 阻断式测试断言因此长期形同虚设（且掩盖过 4 个包的真实失败）。
		//
		// 现约定：CI 环境（GitHub Actions 自动设置 CI=true）下连不上测试库即 **Fatal**，
		// 让"没跑"这件事本身变成红色，而不是变成一句没人看的 SKIP。
		// 本地开发保留跳过语义，便于不装 PG 也能跑纯单元测试。
		if os.Getenv("CI") != "" {
			t.Fatalf("CI 环境下 PostgreSQL 测试库不可达（%s:%s）：%v。\n"+
				"不允许静默跳过（会产生假绿）。请检查 CI 是否设置了 POSTGRES_TEST_HOST / "+
				"POSTGRES_TEST_PORT / POSTGRES_TEST_USER / POSTGRES_TEST_PASSWORD。", host, port, dialErr)
		}
		t.Skipf("PostgreSQL 测试库不可达（已按优先级尝试 %v）：%v；按设计本测试可跳过",
			testDBPortCandidates(), dialErr)
		return nil
	}

	tEndpoint = time.Since(last)
	last = time.Now()

	ensureProcTestDB(t)
	tEnsure = time.Since(last)
	testDSN := dbnameRe.ReplaceAllString(getTestDSN(port), "dbname="+procDBName)

	legacy := os.Getenv("TESTDB_LEGACY") == "1"
	var database *gorm.DB
	database, tOpen, tSettings = openTestDB(t, testDSN)
	last = time.Now()

	if len(models) > 0 {
		if legacy {
			sysDB := systemScopeDB(database)
			for _, m := range models {
				if dropErr := sysDB.Migrator().DropTable(m); dropErr != nil {
					t.Logf("DropTable 提示 %T: %v", m, dropErr)
				}
			}
			tDrop = time.Since(last)
			last = time.Now()
			if migrateErr := sysDB.AutoMigrate(models...); migrateErr != nil {
				t.Fatalf("AutoMigrate 失败: %v", migrateErr)
			}
			tMigrate = time.Since(last)
			last = time.Now()
			// 租户唯一索引规范化（与 db.AutoMigrate 同一实现）：模型标签建的是单列
			// 唯一索引，隔离要求 (tenant_id, 原列)——不规范化则复合 ON CONFLICT 失配。
			if _, normErr := tenancyidx.Normalize(database); normErr != nil {
				t.Fatalf("租户唯一索引规范化失败: %v", normErr)
			}
			tNormalize = time.Since(last)
			last = time.Now()
			// 租户登记表（数据库内省：有 tenant_id 列的表），供隔离 callback 判定
			if regErr := tenancyidx.InitTenantRegistry(database); regErr != nil {
				t.Fatalf("租户登记表装配失败: %v", regErr)
			}
			tRegistry = time.Since(last)
			// 租户隔离 callback（与 db.AutoMigrate 同一实现）：off 模式直通，
			// 测试内 SetMode(on) 即获得注入语义（T028 越权套件依赖此通路）
			tenancyidx.RegisterTenantCallbacks(database)
		} else {
			st := ensureSchema(t, database, models)
			newCount = st.newCount
			tMigrate = st.migrate
			tNormalize = st.normalize
			tRegistry = st.registry
			tTruncate = st.truncate
		}
	}

	// 注入全局，供无参 Service 构造器（dbUtil.GetDB() / repository.GetDB()）取用。
	// 写 dbstate 而不是直接 db.SetTestDB：见 dbstate 包注释（import cycle）。
	dbstate.Set(database)
	t.Cleanup(func() { dbstate.Set(nil) })

	if os.Getenv("TESTDB_TIMING") != "" {
		fmt.Fprintf(os.Stderr,
			"[testdb-timing] models=%d new=%d endpoint=%s ensure=%s open=%s settings=%s drop=%s migrate=%s normalize=%s registry=%s truncate=%s total=%s\n",
			len(models), newCount, tEndpoint, tEnsure, tOpen, tSettings, tDrop, tMigrate, tNormalize, tRegistry, tTruncate, time.Since(started))
	}

	return database
}

// openTestDB 每个用例开一个独立连接池。
//
// 为什么不能共享一个进程级池：上一用例的异步 goroutine（如
// IntentRecognizer.saveRecord 的 fire-and-forget 写）会继续持有句柄，
// 把行写进下一用例已经 TRUNCATE 过的表，表现为“多一条记录”。
// 旧实现靠每个用例 cleanup Close 天然隔离；这里保留该语义，只把
// 真正贵的 AutoMigrate/Normalize 变成进程内一次。
func openTestDB(t testing.TB, testDSN string) (database *gorm.DB, open, settings time.Duration) {
	begin := time.Now()
	var err error
	database, err = gorm.Open(postgres.Open(testDSN), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("连接 PostgreSQL 测试库失败（dsn=%s）: %v", maskedDSN(testDSN), err)
	}
	open = time.Since(begin)

	begin = time.Now()
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("获取 sql.DB 失败: %v", err)
	}
	sqlDB.SetMaxOpenConns(testDBMaxOpenConns)
	sqlDB.SetMaxIdleConns(testDBMaxIdleConns)
	// statement_timeout/lock_timeout/synchronous_commit 已由 ensureProcTestDB
	// 在库级设置，所有新会话自动继承；这里只处理不能库级设置的
	// session_replication_role（与旧实现同一次尝试）。
	if _, execErr := sqlDB.Exec("CREATE EXTENSION IF NOT EXISTS vector"); execErr != nil {
		t.Logf("启用 pgvector 扩展提示（需 postgres 镜像含 vector 包）: %v", execErr)
	}
	if _, execErr := sqlDB.Exec("SET session_replication_role = 'replica'"); execErr != nil {
		t.Logf("禁用外键约束提示: %v", execErr)
	}
	settings = time.Since(begin)

	// cleanup Close 会等待在途查询结束/失败，挡住跨用例异步写。
	t.Cleanup(func() { _ = sqlDB.Close() })
	// 每个新句柄都要注册 callback（同一 Config 上幂等替换）。
	tenancyidx.RegisterTenantCallbacks(database)
	// 租户登记表是进程级全局状态；用例可能 ResetRegistryForTest 后
	// 再调 NewTestDB（db 包的 tenant_callback 套件就是这样）。登记表
	// 未装配时 on 模式会 fail-closed 拒绝一切操作，因此每次打开句柄都
	// 检查一次；已就绪时只是一次读锁，成本可忽略。
	if !tenancyidx.RegistryReady() {
		if regErr := tenancyidx.InitTenantRegistry(database); regErr != nil {
			t.Fatalf("租户登记表装配失败: %v", regErr)
		}
	}
	return database, open, settings
}

// schemaTiming 是 ensureSchema 的分阶段读数（TESTDB_TIMING 用）。
type schemaTiming struct {
	newCount  int
	migrate   time.Duration
	normalize time.Duration
	registry  time.Duration
	truncate  time.Duration
}

// ensureSchema 保证本次 models 的表存在且为空。
//
// 快路径：模型已在进程内登记过 → 只 TRUNCATE（一次多表语句，含序列重置）。
// 慢路径：出现新模型 → AutoMigrate 新模型，再重跑一次租户索引/登记表。
// 回退：TRUNCATE 失败（用例自己 DROP/RENAME 过表）→ DROP+AutoMigrate 本次
// models、重跑租户装配、再 TRUNCATE 一次。
func ensureSchema(t testing.TB, database *gorm.DB, models []any) schemaTiming {
	schemaMu.Lock()
	defer schemaMu.Unlock()

	sysDB := systemScopeDB(database)
	var st schemaTiming
	tables := make([]string, 0, len(models))
	seenTables := make(map[string]bool, len(models))
	newModels := make([]any, 0, len(models))
	newTables := make([]string, 0, len(models))

	for _, m := range models {
		table, err := tableNameFor(database, m)
		if err != nil {
			t.Fatalf("解析模型表名失败 %T: %v", m, err)
		}
		if seenTables[table] {
			continue
		}
		seenTables[table] = true
		tables = append(tables, table)
		if !migratedTables[table] {
			newModels = append(newModels, m)
			newTables = append(newTables, table)
		}
	}
	st.newCount = len(newModels)

	if len(newModels) > 0 {
		begin := time.Now()
		if err := sysDB.AutoMigrate(newModels...); err != nil {
			t.Fatalf("AutoMigrate 失败: %v", err)
		}
		st.migrate = time.Since(begin)

		st.normalize, st.registry = refreshTenancyLocked(t, database)
		for _, table := range newTables {
			migratedTables[table] = true
		}
	}

	begin := time.Now()
	if err := truncateTables(sysDB, tables); err != nil {
		t.Logf("TRUNCATE 本次模型失败，回退 DROP+AutoMigrate: %v", err)
		for _, m := range models {
			if dropErr := sysDB.Migrator().DropTable(m); dropErr != nil {
				t.Logf("回退 DropTable 提示 %T: %v", m, dropErr)
			}
		}
		if migrateErr := sysDB.AutoMigrate(models...); migrateErr != nil {
			t.Fatalf("回退 AutoMigrate 失败: %v", migrateErr)
		}
		for _, table := range tables {
			migratedTables[table] = true
		}
		norm, reg := refreshTenancyLocked(t, database)
		st.normalize += norm
		st.registry += reg
		if truncateErr := truncateTables(sysDB, tables); truncateErr != nil {
			t.Fatalf("回退后 TRUNCATE 仍失败: %v", truncateErr)
		}
	}
	st.truncate = time.Since(begin)
	return st
}

// systemScopeDB 让 schema 维护（AutoMigrate/DropTable/TRUNCATE）走系统作用域。
// 用例可能在 NewTestDB 之前就把租户模式切成 on（如 TestAbExperimentOnModeTenantCtx），
// 此时 on 模式的 raw SQL 守卫会拒绝引用租户表的 DDL；schema 维护是装配操作，
// 必须显式声明系统作用域（与 tenancyidx.Normalize 的处理一致）。
func systemScopeDB(database *gorm.DB) *gorm.DB {
	ctx := context.Background()
	if database != nil && database.Statement != nil && database.Statement.Context != nil {
		ctx = database.Statement.Context
	}
	return database.WithContext(tenant.WithSystemScope(ctx))
}

// refreshTenancyLocked 重跑租户唯一索引规范化/登记表装配；调用方需持有 schemaMu。
func refreshTenancyLocked(t testing.TB, database *gorm.DB) (normalize, registry time.Duration) {
	begin := time.Now()
	// 租户唯一索引规范化（与 db.AutoMigrate 同一实现）：模型标签建的是单列
	// 唯一索引，隔离要求 (tenant_id, 原列)——不规范化则复合 ON CONFLICT 失配。
	if _, err := tenancyidx.Normalize(database); err != nil {
		t.Fatalf("租户唯一索引规范化失败: %v", err)
	}
	normalize = time.Since(begin)

	begin = time.Now()
	// 租户登记表（数据库内省：有 tenant_id 列的表），供隔离 callback 判定。
	if err := tenancyidx.InitTenantRegistry(database); err != nil {
		t.Fatalf("租户登记表装配失败: %v", err)
	}
	registry = time.Since(begin)

	// 租户隔离 callback（与 db.AutoMigrate 同一实现）：off 模式直通，
	// 测试内 SetMode(on) 即获得注入语义（T028 越权套件依赖此通路）。
	tenancyidx.RegisterTenantCallbacks(database)
	return normalize, registry
}

// RefreshTenancy 供测试内绕过 NewTestDB 直接改 schema 后调用：
// 裸 CREATE TABLE / 生产 registry 迁移之后，重新内省租户表并规范化唯一索引。
func RefreshTenancy(t testing.TB, database *gorm.DB) {
	t.Helper()
	if database == nil {
		t.Fatalf("RefreshTenancy: database is nil")
	}
	schemaMu.Lock()
	defer schemaMu.Unlock()
	refreshTenancyLocked(t, database)
}

// tableNameFor 用 GORM 自己的命名策略解析模型对应的实际表名，
// 与 AutoMigrate/DropTable 使用同一套 TableName() 规则。
func tableNameFor(database *gorm.DB, model any) (string, error) {
	stmt := &gorm.Statement{DB: database}
	if err := stmt.Parse(model); err != nil {
		return "", err
	}
	if stmt.Schema == nil || stmt.Schema.Table == "" {
		return "", fmt.Errorf("模型没有可解析的表名")
	}
	return stmt.Schema.Table, nil
}

// truncateTables 清空本次 models 的表并重置自增序列。
// RESTART IDENTITY 与旧实现 DROP+CREATE 的序列重置语义一致；
// CASCADE 处理用例之间遗留的外键依赖。
func truncateTables(database *gorm.DB, tables []string) error {
	if len(tables) == 0 {
		return nil
	}
	quoted := make([]string, len(tables))
	for i, table := range tables {
		quoted[i] = `"` + strings.ReplaceAll(table, `"`, `""`) + `"`
	}
	return database.Exec("TRUNCATE TABLE " + strings.Join(quoted, ", ") + " RESTART IDENTITY CASCADE").Error
}

// procTestDBSlots 进程级测试库的槽位数。
//
// 原实现按 `<base>_<pid>` 命名，**每个测试进程一个库
// 且从不 DROP** —— 实测累积 1505 个 / 19 GB。
//
// 为什么不能在进程退出时删：Go **没有**通用的"测试进程退出"回调。只有显式写了 TestMain
// 的包才有挂钩子的地方，而全仓 786 个使用 NewTestDB 的测试文件分布在 61 个包目录里，
// 其中**只有 8 个包有 TestMain** —— 靠 TestMain 兜不住。
//
// 因此改为「固定槽位 + PostgreSQL 会话级咨询锁」：库总数由**历史运行次数**降为
// **槽位数**（有界）。会话级咨询锁在连接断开时由 PG 自动释放，
// 所以测试进程正常退出、崩溃、被 kill 都能自动回收槽位 —— 不需要任何退出钩子。
const procTestDBSlots = 32

// procTestDBLockBase 咨询锁的键空间基准（"mtk" + 0x00），
// 避免与业务代码或其他工具的咨询锁意外撞键。
const procTestDBLockBase = 0x6d746b00

// acquireTestDBSlot 在维护库连接上依次尝试获取槽位咨询锁，返回首个拿到的槽位。
// 全部被占用时返回 ok=false（同一台机器上并发跑了多套 go test）。
func acquireTestDBSlot(conn *sql.Conn, t testing.TB) (slot int, ok bool) {
	for i := 0; i < procTestDBSlots; i++ {
		var got bool
		if err := conn.QueryRowContext(context.Background(),
			"SELECT pg_try_advisory_lock($1)", procTestDBLockBase+int64(i)).Scan(&got); err != nil {
			t.Logf("获取测试库槽位 %d 的咨询锁失败: %v", i, err)
			continue
		}
		if got {
			return i, true
		}
	}
	return 0, false
}

func ensureProcTestDB(t testing.TB) {
	procDBInit.Do(func() {
		// 基名与 getTestDSN 同源（POSTGRES_TEST_DBNAME，默认 user_db_test）。
		//
		// ⚠️ 此处不能硬编码 `"user_db_test_%d"`。而 NewTestDB 会把
		// getTestDSN() 里的 dbname 整体替换成 procDBName（见 NewTestDB 内
		// `dbnameRe.ReplaceAllString(..., "dbname="+procDBName)`），
		// 于是 POSTGRES_TEST_DBNAME **设了也不生效** —— 典型"配置项名承诺了它不做的事"。
		// 现改为共用同一基名，两边不再各说各话。
		base := getEnvOr("POSTGRES_TEST_DBNAME", "user_db_test")
		_, port, _ := resolveTestDBEndpoint()
		maintDSN := dbnameRe.ReplaceAllString(getTestDSN(port), "dbname=postgres")
		m, err := gorm.Open(postgres.Open(maintDSN), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		if err != nil {
			procDBInitErr = err
			return
		}
		s, err := m.DB()
		if err != nil {
			procDBInitErr = err
			return
		}
		conn, err := s.Conn(context.Background())
		if err != nil {
			procDBInitErr = err
			return
		}
		// 包级持有：局部变量 GC 后连接关闭，会话级咨询锁会提前释放（见变量注释）。
		procDBMaintConn = conn

		slot, locked := acquireTestDBSlot(conn, t)
		if locked {
			procDBName = fmt.Sprintf("%s_slot%d", base, slot)
		} else {
			// 槽位用尽：退回 PID 命名，宁可多一个可能成为孤儿的库，
			// 也不能让两个进程共用同一个库（会互相 DROP 掉对方的数据）。
			// 这种库由 make test-db-prune 兜底清理。
			t.Logf("测试库槽位（%d 个）已全部被占用，本次回退为 PID 命名", procTestDBSlots)
			procDBName = fmt.Sprintf("%s_%d", base, os.Getpid())
		}

		ctx := context.Background()
		if _, e := conn.ExecContext(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, procDBName)); e != nil {
			t.Logf("清理残留测试库提示 %s: %v", procDBName, e)
		}
		if _, e := conn.ExecContext(ctx, fmt.Sprintf(`CREATE DATABASE "%s"`, procDBName)); e != nil {
			procDBInitErr = e
			return
		}

		// 测试库级设置：
		//   - synchronous_commit=off：DDL/DML 提交不再等 WAL fsync，直接消掉
		//     整套用例在 macOS 磁盘上的主要尾延迟；
		//   - statement_timeout/lock_timeout：旧的逐连接 SET 不保证命中
		//     DDL 所在连接，库级设置覆盖所有新会话，失败快于挂死。
		// 仅作用于本进程租用的测试库，失败只记日志（普通用户可能无权限）。
		for _, stmt := range []string{
			fmt.Sprintf(`ALTER DATABASE "%s" SET synchronous_commit = off`, procDBName),
			fmt.Sprintf(`ALTER DATABASE "%s" SET lock_timeout = '15s'`, procDBName),
			fmt.Sprintf(`ALTER DATABASE "%s" SET statement_timeout = '60s'`, procDBName),
		} {
			if _, e := conn.ExecContext(ctx, stmt); e != nil {
				t.Logf("设置测试库参数提示（%s）: %v", stmt, e)
			}
		}
	})
	if procDBInitErr != nil {
		t.Fatalf("初始化进程级测试库失败: %v\n"+
			"提示：本机 PG 端口/密码与 CI 默认值不同时，请显式导出测试连接变量，例如\n"+
			"  POSTGRES_TEST_PORT=8232 POSTGRES_TEST_PASSWORD=<hivemtk/.env 的 POSTGRES_PASSWORD> go test ./...\n"+
			"（端口候选见 testDBPortCandidates；密码不回落到 .env，避免把密钥写进代码）",
			procDBInitErr)
	}
}

// NewTestDBOrSkip 在 PostgreSQL 测试库不可达时跳过测试（t.Skipf），否则同 NewTestDB。
//
// 适用场景：本地/CI 无 PG 时，让不强制依赖真实 DB 的测试优雅跳过；
// 强依赖 DB 的测试仍使用 NewTestDB（不可达则 fail）。
func NewTestDBOrSkip(t testing.TB, models ...any) *gorm.DB {
	t.Helper()

	if _, _, err := resolveTestDBEndpoint(); err != nil {
		t.Skipf("PostgreSQL 测试库不可达（已按优先级尝试 %v）：%v；按设计本测试可跳过",
			testDBPortCandidates(), err)
		return nil
	}
	return NewTestDB(t, models...)
}

// getTestDSN 组装测试库连接串。port 由 resolveTestDBEndpoint 解析后传入，
// 避免此处再出现一份"默认端口"从而与 .env / ports.go 各说各话。
func getTestDSN(port string) string {
	if v := os.Getenv("POSTGRES_TEST_DSN"); v != "" {
		return v
	}
	host := getEnvOr("POSTGRES_TEST_HOST", "127.0.0.1")
	user := getEnvOr("POSTGRES_TEST_USER", "admin")
	password := getEnvOr("POSTGRES_TEST_PASSWORD", os.Getenv("POSTGRES_PASSWORD"))
	if password == "" {
		// 兜底：CI 的容器密码。本地 .env 的密码不同，届时会走到下面的
		// 认证失败分支并给出明确提示（不在此硬编码本地密码 —— 那是密钥）。
		password = "password123"
	}
	dbname := getEnvOr("POSTGRES_TEST_DBNAME", "user_db_test")
	sslmode := getEnvOr("POSTGRES_TEST_SSLMODE", "disable")
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s TimeZone=Asia/Shanghai",
		host, port, user, password, dbname, sslmode)
}

func getEnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func maskedDSN(dsn string) string {
	out := []byte(dsn)
	for i := 0; i < len(out)-8; i++ {
		if string(out[i:i+9]) == "password=" {
			j := i + 9
			for j < len(out) && out[j] != ' ' {
				out[j] = '*'
				j++
			}
			break
		}
	}
	return string(out)
}
