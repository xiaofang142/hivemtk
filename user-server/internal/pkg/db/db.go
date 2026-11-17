package db

import (
	"database/sql"
	"fmt"
	"hivemtk-user/internal/config"
	"hivemtk-user/internal/pkg/dbstate"
	"os"
	"strings"
	"sync"
	"time"

	gormLogger "gorm.io/gorm/logger"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// DB 的跨包存取只走 GetDB／SetTestDB／InitDB 三扇门，对它的每次读和每次写都在 dbMu 下。
// 这些访问跨 goroutine（cron tick 体、fire-and-forget 异步体、测试里的 SetTestDB），
// 裸读同一地址是数据竞争，故在 accessor 收口。包内仍直读直写的只剩
// migrate.go 的建表路径（只由 cmd 启动时走一次）与本包测试的自恢复赋值。
var (
	dbMu sync.RWMutex
	DB   *gorm.DB
)

func InitDB() {
	var db *gorm.DB
	var err error

	appConfig := config.GetAppConfig()

	// 2026-11-17 审计 P3-18 收口：应用连接账号与 PG 超管分离。
	// DB_USER 存在 ⇒ 应用以最小权限角色连接（口令取 DB_APP_PASSWORD），
	// POSTGRES_PASSWORD 保留为超管（维护脚本/bootstrap 用）；不设则行为与旧版一致。
	pgUser := appConfig.Database.Postgres.User
	pgPassword := appConfig.Database.Postgres.Password
	if v := os.Getenv("DB_USER"); v != "" {
		pgUser = v
		if v2 := os.Getenv("DB_APP_PASSWORD"); v2 != "" {
			pgPassword = v2
		}
	}
	if pgPassword == "" {
		pgPassword = os.Getenv("POSTGRES_PASSWORD")
	}
	if pgPassword == "" {
		panic("数据库连接密码缺失：配置文件未保留 password 字段，必须由运行时环境变量 POSTGRES_PASSWORD 注入")
	}

	// libpq keyword/value 格式：含空格/引号的口令必须单引号包裹（' 转义为 \'），
	// 否则 DSN 解析错位导致 SASL 认证失败（宿主机 PGPASSWORD 走 libpq 直连不受影响）。
	dsn := fmt.Sprintf("host=%s user=%s password='%s' dbname=%s port=%d sslmode=%s TimeZone=Asia/Shanghai",
		appConfig.Database.Postgres.Host,
		pgUser,
		strings.ReplaceAll(pgPassword, "'", "\\'"),
		appConfig.Database.Postgres.DBName,
		appConfig.Database.Postgres.Port,
		appConfig.Database.Postgres.SSLMode,
	)

	logLevel := gormLogger.Warn
	if os.Getenv("APP_ENV") == "development" || os.Getenv("GIN_MODE") == "debug" {
		logLevel = gormLogger.Info
	}
	db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger:                                   gormLogger.Default.LogMode(logLevel),
		DisableForeignKeyConstraintWhenMigrating: true,
	})

	if err != nil {
		panic(fmt.Sprintf("Failed to connect to database: %v", err))
	}

	poolConfig := normalizePool(appConfig.Database.Pool)

	sqlDB, err := db.DB()
	if err != nil {
		panic(fmt.Sprintf("Failed to get database instance: %v", err))
	}

	applyPool(sqlDB, poolConfig)

	dbMu.Lock()
	DB = db
	dbstate.Set(db)
	dbMu.Unlock()
}

// normalizePool 把配置里留空的池参数补成兜底值（MaxOpenConns 补 0 会被当作"不限"，故必须补兜底值）。
func normalizePool(poolConfig config.PoolConfig) config.PoolConfig {
	if poolConfig.MaxIdleConns == 0 {
		poolConfig = config.DefaultPoolConfig
	}

	if poolConfig.MaxOpenConns == 0 {
		poolConfig.MaxOpenConns = 20
	}
	if poolConfig.ConnMaxLifetime == 0 {
		poolConfig.ConnMaxLifetime = int((30 * time.Minute).Seconds())
	}
	return poolConfig
}

// applyPool 把池参数写进 database/sql 句柄。
// 这几行不可挪走或包进条件：配置文件写的值与真正生效的值是两件事，漏设会让
// 连接数回落到 Go 默认的 2 而无任何报错。
func applyPool(sqlDB *sql.DB, poolConfig config.PoolConfig) {
	sqlDB.SetMaxIdleConns(poolConfig.MaxIdleConns)
	sqlDB.SetMaxOpenConns(poolConfig.MaxOpenConns)
	sqlDB.SetConnMaxIdleTime(time.Duration(poolConfig.ConnMaxIdleTime) * time.Second)
	sqlDB.SetConnMaxLifetime(time.Duration(poolConfig.ConnMaxLifetime) * time.Second)
}

func GetDB() *gorm.DB {
	// 跨包只认 dbstate：testutil.NewTestDB 写它；InitDB 也写它。
	// 不回落到本包 DB —— 那条回落曾让 NewTestDB cleanup 关掉连接后，
	// 包级 DB 仍指向已关闭句柄，后续 NilDB 用例拿到 "sql: database is closed"。
	return dbstate.Get()
}

// SetTestDB 设置测试数据库（仅用于测试）
func SetTestDB(testDB *gorm.DB) {
	dbstate.Set(testDB)
	dbMu.Lock()
	defer dbMu.Unlock()
	DB = testDB
}
