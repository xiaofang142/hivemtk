package db

import (
	"fmt"
	"hivemtk-user/internal/config"
	"os"
	"sync"
	"time"

	gormLogger "gorm.io/gorm/logger"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// DB 的跨包存取只走 GetDB／SetTestDB／InitDB 这三扇门，对它的每次读和每次写都在 dbMu 下。
// 要收口的理由是这些访问跨 goroutine：写方是测试里 296 处 `SetTestDB(`（180 个文件，全在
// `*_test.go` 里，用例各自换库；生产路径 0 处），读方
// 除请求协程外还有 cron 的 tick 体与 fire-and-forget 的异步体——裸读同一个地址就是数据竞争，
// -race 已在这条上红过三处站点（TriggerCSATOnClose／fallbackVersionOf／DispatchSessionEventAsync），
// 前两处靠"把句柄解析挪到 spawning 之前"回避，但 cron 与循环体没法挪，第四处（MaybeSendAwayReply
// 的异步体在 GetConfig 里逐次 new 仓储）连挪都挪不动（见其 sync.Once 单例），故竞争本身在 accessor
// 里收口。包外对 DB 的直读 0 处（四个导入别名 dbutil／dbUtil／_db／pdb 一并核过，命中的
// 3 行全是 gorm 的 `(*gorm.DB).DB()` 方法与注释）。本包内仍直读直写的只剩
// migrate.go 的建表路径与本包测试的自恢复赋值：前者只由 cmd/* 在起协程之前走一次，后者都排在
// 任何 spawn 之前，同一 goroutine 内不构成并发访问。
var (
	dbMu sync.RWMutex
	DB   *gorm.DB
)

func InitDB() {
	var db *gorm.DB
	var err error

	appConfig := config.GetAppConfig()

	pgPassword := appConfig.Database.Postgres.Password
	if pgPassword == "" {
		pgPassword = os.Getenv("POSTGRES_PASSWORD")
	}
	if pgPassword == "" {
		panic("数据库连接密码缺失：配置文件未保留 password 字段，必须由运行时环境变量 POSTGRES_PASSWORD 注入")
	}

	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%d sslmode=%s TimeZone=Asia/Shanghai",
		appConfig.Database.Postgres.Host,
		appConfig.Database.Postgres.User,
		pgPassword,
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

	poolConfig := appConfig.Database.Pool
	if poolConfig.MaxIdleConns == 0 {
		poolConfig = config.DefaultPoolConfig
	}

	if poolConfig.MaxOpenConns == 0 {
		poolConfig.MaxOpenConns = 20
	}
	if poolConfig.ConnMaxLifetime == 0 {
		poolConfig.ConnMaxLifetime = int((30 * time.Minute).Seconds())
	}

	sqlDB, err := db.DB()
	if err != nil {
		panic(fmt.Sprintf("Failed to get database instance: %v", err))
	}

	sqlDB.SetMaxIdleConns(poolConfig.MaxIdleConns)
	sqlDB.SetMaxOpenConns(poolConfig.MaxOpenConns)
	sqlDB.SetConnMaxIdleTime(time.Duration(poolConfig.ConnMaxIdleTime) * time.Second)
	sqlDB.SetConnMaxLifetime(time.Duration(poolConfig.ConnMaxLifetime) * time.Second)

	dbMu.Lock()
	DB = db
	dbMu.Unlock()
}

func GetDB() *gorm.DB {
	dbMu.RLock()
	defer dbMu.RUnlock()
	return DB
}

// SetTestDB 设置测试数据库（仅用于测试）
func SetTestDB(testDB *gorm.DB) {
	dbMu.Lock()
	defer dbMu.Unlock()
	DB = testDB
}
