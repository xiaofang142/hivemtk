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
package testutil

import (
	"database/sql"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/config"

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
)

// testDBPortCandidates 返回宿主机测试库端口候选（按优先级去重）。
//
// ⚠️ 为什么不能像原实现那样写死单一端口 8202：
//   - 8202 是 **容器内** 端口。宿主机映射端口由 docker-compose 的
//     `ports: "127.0.0.1:${USER_POSTGRES_HOST_PORT:-8202}:8202"` 决定，
//     本机 .env 覆盖为 8232（见 docs/PORT_REGISTRY.md §一 / §二）。
//   - 写死 8202 时，本机 `go test ./...` 连不上 → 依赖本辅助的 288 个测试文件
//     全部 t.Skipf → go test 退出 0，门禁变假绿（这正是 TEST-01 的同源问题，
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
// —— CI run 2026-09-22T09:57Z 的红因正落在 async_db_handle_probe_test.go:76（第三枚探针的
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

// NewTestDB 创建并初始化（或复用）当前进程的独立 PostgreSQL 测试数据库。
//
// 行为：
//  1. 首次调用时在维护库（postgres）创建独立库（先清理同名残留）。
//     库名取自一组固定槽位（user_db_test_slot<N>），由会话级咨询锁分配，
//     因此库总数有界；详见 procTestDBSlots 注释（RISK-07）。
//  2. 连接该库，启用 pgvector 扩展（向量字段必需）。
//  3. 在测试 session 内禁用外键约束（避免跨域共享模型引用时缺失依赖记录）。
//  4. 对传入的 models 执行 AutoMigrate（先 DROP 目标表再重建，幂等）。
//  5. 通过 t.Cleanup 注册清理：断开本测试连接（不 DROP 整个进程库，供同进程后续测试复用）。
//
// 优雅降级：当 PostgreSQL 测试库不可达时（如本地开发无 PG），
// 自动调用 t.Skipf 跳过本测试，保证 go test 不会因环境差异大面积失败。
// 强依赖真实 DB 的集成/E2E 测试在此场景下整体跳过，不影响纯单元测试。
// 注意：CI 环境（CI 环境变量非空）下改为 t.Fatalf —— 见函数体内注释。
//
// 注意：本辅助不调用 db.SetTestDB，由调用方按需将 DB 注入到全局 db.DB 或 Service。
//
// 参数类型是 testing.TB 而非 *testing.T：benchmark 同样需要测试库。若只接受
// *testing.T，基准测试就只能自带第 N 套 DSN 拼装（见 internal/repository/
// message_hub_inbox_outbound_p0_5_bench_test.go 的历史实现：默认端口 8202、
// 默认口令 postgres、还认第五个变量名 TEST_DATABASE_URL），结果是基准静默 Skip。
// 放宽为 testing.TB 后测试与基准共用同一引导入口；*testing.T 满足 testing.TB，
// 既有调用方无需改动。
func NewTestDB(t testing.TB, models ...any) *gorm.DB {
	t.Helper()

	host, port, dialErr := resolveTestDBEndpoint()
	if dialErr != nil {
		// ⚠️ 2026-09-16：CI 环境下禁止静默跳过。
		//
		// 背景：CI（.github/workflows/user-server-ci.yml）注入的是 POSTGRES_HOST/PORT/
		// USER/PASSWORD，而本辅助读取的是 POSTGRES_TEST_* 前缀 —— 两者变量名不匹配，
		// 于是 CI 里 PG 监听在 5432、这里却去连默认的 8202，DialTimeout 必然失败，
		// 依赖本辅助的 **288 个测试文件全部走 t.Skipf**，`go test` 依然退出 0。
		// 阻断式测试门禁因此长期形同虚设（且掩盖了 4 个包的真实失败）。
		//
		// 现约定：CI 环境（GitHub Actions 自动设置 CI=true）下连不上测试库即 **Fatal**，
		// 让"门禁没跑"这件事本身变成红色，而不是变成一句没人看的 SKIP。
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

	ensureProcTestDB(t)
	testDSN := dbnameRe.ReplaceAllString(getTestDSN(port), "dbname="+procDBName)
	database, err := gorm.Open(postgres.Open(testDSN), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("连接 PostgreSQL 测试库失败（dsn=%s）: %v", maskedDSN(testDSN), err)
	}

	if sqlDB, dbErr := database.DB(); dbErr == nil {
		sqlDB.SetMaxOpenConns(testDBMaxOpenConns)
		sqlDB.SetMaxIdleConns(testDBMaxIdleConns)
		if _, execErr := sqlDB.Exec("CREATE EXTENSION IF NOT EXISTS vector"); execErr != nil {
			t.Logf("启用 pgvector 扩展提示（需 postgres 镜像含 vector 包）: %v", execErr)
		}
		if _, execErr := sqlDB.Exec("SET session_replication_role = 'replica'"); execErr != nil {
			t.Logf("禁用外键约束提示: %v", execErr)
		}
	}

	if len(models) > 0 {
		for _, m := range models {
			if dropErr := database.Migrator().DropTable(m); dropErr != nil {
				t.Logf("DropTable 提示 %T: %v", m, dropErr)
			}
		}
		if migrateErr := database.AutoMigrate(models...); migrateErr != nil {
			t.Fatalf("AutoMigrate 失败: %v", migrateErr)
		}
	}

	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("获取 sql.DB 失败: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	return database
}

// procTestDBSlots 进程级测试库的槽位数。
//
// 2026-09-16（RISK-07 根因修复）：原实现按 `<base>_<pid>` 命名，**每个测试进程一个库
// 且从不 DROP** —— 实测累积 1505 个 / 19 GB（见 docs/architecture/TASKS_AUDIT_2026-09-16.md）。
//
// 为什么不能在进程退出时删：Go **没有**通用的"测试进程退出"回调。只有显式写了 TestMain
// 的包才有挂钩子的地方，而全仓 293 个使用 NewTestDB 的测试文件分布在 35 个包目录里，
// 其中**只有 5 个包有 TestMain** —— 靠 TestMain 兜不住。
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
func acquireTestDBSlot(conn *sql.DB, t testing.TB) (slot int, ok bool) {
	for i := 0; i < procTestDBSlots; i++ {
		var got bool
		if err := conn.QueryRow("SELECT pg_try_advisory_lock($1)", procTestDBLockBase+int64(i)).Scan(&got); err != nil {
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
		// ⚠️ 2026-09-16：此处原为硬编码 `"user_db_test_%d"`。而 NewTestDB 会把
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
		// s 刻意不 Close：会话级咨询锁随连接释放，连接必须活到进程结束，
		// 否则槽位会在测试跑到一半时被回收。进程退出时 OS 关 socket，PG 随即释放锁。

		slot, locked := acquireTestDBSlot(s, t)
		if locked {
			procDBName = fmt.Sprintf("%s_slot%d", base, slot)
		} else {
			// 槽位用尽：退回 PID 命名，宁可多一个可能成为孤儿的库，
			// 也不能让两个进程共用同一个库（会互相 DROP 掉对方的数据）。
			// 这种库由 make test-db-prune 兜底清理。
			t.Logf("测试库槽位（%d 个）已全部被占用，本次回退为 PID 命名", procTestDBSlots)
			procDBName = fmt.Sprintf("%s_%d", base, os.Getpid())
		}

		if _, e := s.Exec(fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, procDBName)); e != nil {
			t.Logf("清理残留测试库提示 %s: %v", procDBName, e)
		}
		if _, e := s.Exec(fmt.Sprintf(`CREATE DATABASE "%s"`, procDBName)); e != nil {
			procDBInitErr = e
			return
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
