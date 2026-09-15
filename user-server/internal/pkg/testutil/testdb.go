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

// NewTestDB 创建并初始化（或复用）当前进程的独立 PostgreSQL 测试数据库。
//
// 行为：
//  1. 首次调用时在维护库（postgres）创建 user_db_test_<pid> 独立库（先清理同名残留）。
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
		name := fmt.Sprintf("%s_%d", base, os.Getpid())
		_, port, _ := resolveTestDBEndpoint()
		maintDSN := dbnameRe.ReplaceAllString(getTestDSN(port), "dbname=postgres")
		m, err := gorm.Open(postgres.Open(maintDSN), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		if err != nil {
			procDBInitErr = err
			return
		}
		defer func() {
			if s, e := m.DB(); e == nil {
				_ = s.Close()
			}
		}()
		s, _ := m.DB()
		if _, e := s.Exec(fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, name)); e != nil {
			t.Logf("清理残留测试库提示 %s: %v", name, e)
		}
		if _, e := s.Exec(fmt.Sprintf(`CREATE DATABASE "%s"`, name)); e != nil {
			procDBInitErr = e
			return
		}
		procDBName = name
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
