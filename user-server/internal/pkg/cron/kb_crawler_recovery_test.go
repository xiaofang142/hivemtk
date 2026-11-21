package cron

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	kbcontroller "hivemtk-user/internal/aiagent/knowledge/controller"
	kbrepo "hivemtk-user/internal/aiagent/knowledge/repository"
	kbservice "hivemtk-user/internal/aiagent/knowledge/service"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenancyidx"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// Opt-in，严格限定父任务提供的私有 socket 集群。绝不使用 testutil/drop database。
func fa12RecoveryDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("FA12_PG_DSN")
	if dsn == "" {
		t.Skip("FA12_PG_DSN 未设置：仅允许指定的合成 PostgreSQL16 集群")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil || cfg.Host != "/private/var/folders/xw/pd_rr_z54w38gq41yfm8h7sm0000gn/T/opencode/fa12-pg-u0tmyd4x" || cfg.Port != 55412 || cfg.User != "fa12_runner" || cfg.Database != "fa12_synthetic" {
		t.Fatal("拒绝连接未授权集群")
	}
	open := func(extra string) *gorm.DB {
		db, err := gorm.Open(postgres.Open(dsn+extra), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
		if err != nil {
			t.Fatal(err)
		}
		pool, _ := db.DB()
		t.Cleanup(func() { _ = pool.Close() })
		return db
	}
	base := open("")
	var identity struct {
		DB      string
		Runner  string
		Socket  bool
		Version int
	}
	if err := base.Raw("SELECT current_database() AS db, current_user AS runner, inet_server_addr() IS NULL AS socket, current_setting('server_version_num')::int AS version").Scan(&identity).Error; err != nil {
		t.Fatal(err)
	}
	if identity.DB != "fa12_synthetic" || identity.Runner != "fa12_runner" || !identity.Socket || identity.Version/10000 != 16 {
		t.Fatalf("集群身份不符: %+v", identity)
	}
	schema := os.Getenv("FA12_RECOVERY_SCHEMA")
	child := schema != ""
	if child {
		if len(schema) < 14 || schema[:14] != "fa12_recovery_" {
			t.Fatal("非法合成 schema")
		}
		if _, err := strconv.ParseInt(schema[14:], 10, 64); err != nil {
			t.Fatal(err)
		}
	} else {
		schema = fmt.Sprintf("fa12_recovery_%d", time.Now().UnixNano())
		if err := base.Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
			t.Fatal(err)
		}
		t.Setenv("FA12_RECOVERY_SCHEMA", schema)
	}
	db := open(" search_path=" + schema)
	if !child {
		if err := db.AutoMigrate(&model.KBCrawlerSource{}, &model.KBCrawlBatch{}, &model.KBDocument{}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			sys := db.WithContext(tenant.WithSystemScope(context.Background()))
			if err := sys.Migrator().DropTable(&model.KBDocument{}, &model.KBCrawlBatch{}, &model.KBCrawlerSource{}); err != nil {
				t.Error(err)
				return
			}
			if err := base.Exec(`DROP SCHEMA "` + schema + `"`).Error; err != nil {
				t.Error(err)
			}
		})
	}
	if err := tenancyidx.InitTenantRegistry(db); err != nil {
		t.Fatal(err)
	}
	tenancyidx.RegisterTenantCallbacks(db)
	old := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(old); tenancyidx.ResetRegistryForTest() })
	t.Logf("PostgreSQL=%d socket=true database=%s schema=%s pid=%d", identity.Version, identity.DB, schema, os.Getpid())
	return db
}

func fa12RecoverySource(t *testing.T, db *gorm.DB, tid uint) model.KBCrawlerSource {
	t.Helper()
	off := false
	src := model.KBCrawlerSource{TenantID: tid, KBID: 1, SourceType: "sitemap", StartURL: "http://127.0.0.1/sitemap.xml", SiteVerified: true, Status: model.KBCrawlerSourceStatusActive, AutoResync: &off, QASplitEnabled: &off, MaxPages: 1}
	if err := db.WithContext(tenant.WithTenantID(context.Background(), tid)).Create(&src).Error; err != nil {
		t.Fatal(err)
	}
	return src
}

func fa12RecoveryBatch(t *testing.T, db *gorm.DB, src model.KBCrawlerSource, status string, started *time.Time) model.KBCrawlBatch {
	t.Helper()
	batch := model.KBCrawlBatch{TenantID: src.TenantID, SourceID: src.ID, BatchNo: fmt.Sprint(time.Now().UnixNano()), Trigger: "manual", Status: status, StartedAt: started, CreatedAt: time.Now().Add(-time.Hour)}
	if err := db.WithContext(tenant.WithTenantID(context.Background(), src.TenantID)).Create(&batch).Error; err != nil {
		t.Fatal(err)
	}
	return batch
}

func fa12RecoverySourceBatchCount(t *testing.T, db *gorm.DB, ctx context.Context, sourceID uint) int64 {
	t.Helper()
	var count int64
	if err := db.WithContext(ctx).Model(&model.KBCrawlBatch{}).Where("source_id = ?", sourceID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}

func fa12RecoveryService(db *gorm.DB) *kbservice.CrawlerService {
	return kbservice.NewCrawlerServiceWithRepos(kbrepo.NewKBCrawlerRepository(db), nil)
}

func fa12ReadBatch(t *testing.T, db *gorm.DB, batch model.KBCrawlBatch) model.KBCrawlBatch {
	t.Helper()
	var got model.KBCrawlBatch
	if err := db.WithContext(tenant.WithTenantID(context.Background(), batch.TenantID)).First(&got, batch.ID).Error; err != nil {
		t.Fatal(err)
	}
	return got
}

// 经过真实 HTTP controller；请求返回/取消之后仍携带原租户和 trace 进入仓储。
func TestFA12Recovery_ControllerSyncAndRetry(t *testing.T) {
	for _, action := range []string{"sync", "retry"} {
		t.Run(action, func(t *testing.T) {
			db := fa12RecoveryDB(t)
			src := fa12RecoverySource(t, db, 7)
			svc := fa12RecoveryService(db)
			ctrl := kbcontroller.NewKBCrawlerController(svc, nil)
			path := "/kb/crawler/sources/" + fmt.Sprint(src.ID) + "/sync"
			if action == "retry" {
				old := fa12RecoveryBatch(t, db, src, "failed", nil)
				path = "/kb/crawler/batches/" + fmt.Sprint(old.ID) + "/retry"
			}
			observed := make(chan context.Context, 8)
			if err := db.Callback().Query().Before("gorm:query").Register("fa12:observe", func(tx *gorm.DB) {
				if tx.Statement.Table == "kb_crawl_batches" {
					select {
					case observed <- tx.Statement.Context:
					default:
					}
				}
			}); err != nil {
				t.Fatal(err)
			}
			engine := gin.New()
			ctrl.RegisterRoutes(engine.Group(""))
			ctx, cancel := context.WithCancel(logger.WithTraceID(tenant.WithTenantID(context.Background(), 7), "fa12-"+action))
			req := httptest.NewRequest("POST", path, nil).WithContext(ctx)
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)
			cancel()
			if rec.Code != 200 {
				t.Fatalf("HTTP=%d %s", rec.Code, rec.Body.String())
			}
			// Retry 的旧批次读取是同步入口，忽略它；后台查询必须独立继续执行。
			deadline := time.NewTimer(3 * time.Second)
			defer deadline.Stop()
			for {
				select {
				case got := <-observed:
					if got == ctx {
						continue
					}
					id, ok := tenant.FromContext(got)
					if !ok || id != 7 || tenant.IsSystemScope(got) || logger.TraceIDFromContext(got) != "fa12-"+action || got.Err() != nil {
						t.Fatalf("后台上下文丢失: tenant=%d trace=%q err=%v", id, logger.TraceIDFromContext(got), got.Err())
					}
					// 等实际失败终态，确保后台 goroutine 在清理连接前结束。
					fa12AwaitTerminal(t, db, src.ID)
					return
				case <-deadline.C:
					t.Fatal("controller 未执行后台批次")
				}
			}
		})
	}
}

func fa12AwaitTerminal(t *testing.T, db *gorm.DB, sourceID uint) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		var got model.KBCrawlBatch
		if err := db.WithContext(tenant.WithTenantID(context.Background(), 7)).Where("source_id = ?", sourceID).Order("id DESC").First(&got).Error; err != nil {
			t.Fatal(err)
		}
		if got.Status == "failed" && got.FinishedAt != nil {
			var src model.KBCrawlerSource
			if err := db.WithContext(tenant.WithTenantID(context.Background(), 7)).First(&src, sourceID).Error; err != nil {
				t.Fatal(err)
			}
			if src.LastBatchID == got.BatchNo {
				return
			}
		}
		select {
		case <-tick.C:
		case <-deadline:
			t.Fatalf("未持久终态: %+v", got)
		}
	}
}

// 持久 pending/过期 running 从既有 scanner 入口恢复；活跃 running 与其它租户保持原样。
func TestFA12Recovery_ScannerPersistentQueue(t *testing.T) {
	db := fa12RecoveryDB(t)
	a, b := fa12RecoverySource(t, db, 7), fa12RecoverySource(t, db, 8)
	old, fresh := time.Now().Add(-time.Hour), time.Now()
	pending := fa12RecoveryBatch(t, db, a, "pending", nil)
	stale := fa12RecoveryBatch(t, db, a, "running", &old)
	legacy := fa12RecoveryBatch(t, db, a, "running", nil)
	active := fa12RecoveryBatch(t, db, a, "running", &fresh)
	other := fa12RecoveryBatch(t, db, b, "pending", nil)
	if err := KBSourceDueScanner(fa12RecoveryService(db))(tenant.WithTenantID(context.Background(), 7)); err != nil {
		t.Fatal(err)
	}
	for _, batch := range []model.KBCrawlBatch{pending, stale, legacy} {
		got := fa12ReadBatch(t, db, batch)
		if got.Status != "failed" || got.FinishedAt == nil {
			t.Errorf("持久任务未消费: %+v", got)
		}
	}
	if got := fa12ReadBatch(t, db, active); got.Status != "running" || !got.StartedAt.Equal(fresh.Truncate(time.Microsecond)) {
		t.Errorf("抢占活跃任务: %+v", got)
	}
	if got := fa12ReadBatch(t, db, other); got.Status != "pending" {
		t.Errorf("跨租户消费: %+v", got)
	}
	if err := KBSourceDueScanner(fa12RecoveryService(db))(tenant.WithTenantID(context.Background(), 8)); err != nil {
		t.Fatal(err)
	}
	if got := fa12ReadBatch(t, db, other); got.Status != "failed" {
		t.Errorf("租户8未恢复: %+v", got)
	}
}

// 即使上游误带 SystemScope，恢复扫描也只能读取指定租户。
func TestFA12Recovery_ScannerRemovesSystemScope(t *testing.T) {
	db := fa12RecoveryDB(t)
	src := fa12RecoverySource(t, db, 7)
	batch := fa12RecoveryBatch(t, db, src, "pending", nil)
	var bypass atomic.Bool
	if err := db.Callback().Query().Before("gorm:query").Register("fa12:system_scope", func(tx *gorm.DB) {
		if tenant.IsSystemScope(tx.Statement.Context) {
			bypass.Store(true)
		}
	}); err != nil {
		t.Fatal(err)
	}
	ctx := tenant.WithSystemScope(tenant.WithTenantID(context.Background(), 7))
	if err := KBSourceDueScanner(fa12RecoveryService(db))(ctx); err != nil {
		t.Fatal(err)
	}
	if bypass.Load() {
		t.Error("恢复/源扫描继承了系统豁免")
	}
	if got := fa12ReadBatch(t, db, batch); got.Status != "failed" {
		t.Fatalf("未完成恢复: %+v", got)
	}
}

// 队首超过一页的软删源孤儿必须终结，让后续扫描能到达有效批次。
func TestFA12Recovery_OrphanQueueProgress(t *testing.T) {
	db := fa12RecoveryDB(t)
	ctxA, ctxB := tenant.WithTenantID(context.Background(), 7), tenant.WithTenantID(context.Background(), 8)
	repo := kbrepo.NewKBCrawlerRepository(db)
	a, b := fa12RecoverySource(t, db, 7), fa12RecoverySource(t, db, 8)
	old, fresh := time.Now().Add(-time.Hour), time.Now().Truncate(time.Microsecond)
	var orphans []model.KBCrawlBatch
	for i := range 105 {
		status, started := "pending", (*time.Time)(nil)
		if i%3 == 1 {
			status, started = "running", &old
		}
		if i%3 == 2 {
			status = "running"
		}
		orphans = append(orphans, fa12RecoveryBatch(t, db, a, status, started))
	}
	active := fa12RecoveryBatch(t, db, a, "running", &fresh)
	other := fa12RecoveryBatch(t, db, b, "pending", nil)
	validSource := fa12RecoverySource(t, db, 7)
	valid := fa12RecoveryBatch(t, db, validSource, "pending", nil)
	if err := repo.DeleteSource(ctxA, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteSource(ctxB, b.ID); err != nil {
		t.Fatal(err)
	}
	svc := fa12RecoveryService(db)
	scan := KBSourceDueScanner(svc)
	for pass := range 3 {
		if err := scan(ctxA); err != nil {
			t.Fatal(err)
		}
		t.Logf("scan=%d first_orphan=%s valid=%s", pass+1, fa12ReadBatch(t, db, orphans[0]).Status, fa12ReadBatch(t, db, valid).Status)
	}
	failed := 0
	for _, batch := range orphans {
		got := fa12ReadBatch(t, db, batch)
		if got.Status == "failed" && got.FinishedAt != nil && strings.Contains(got.ErrorMsg, "爬取源不存在") {
			failed++
		}
	}
	if failed != len(orphans) {
		t.Errorf("孤儿未终结: failed=%d total=%d", failed, len(orphans))
	}
	if got := fa12ReadBatch(t, db, valid); got.Status != "failed" || got.FinishedAt == nil || !strings.Contains(got.ErrorMsg, "sitemap") {
		t.Errorf("有效批次被孤儿饥饿阻塞: %+v", got)
	}
	listed, total, err := svc.ListBatches(ctxA, a.ID, 1, 200)
	if err != nil || total != 106 {
		t.Fatalf("批次错误不可查询: total=%d err=%v", total, err)
	}
	listedFailed := 0
	for _, got := range listed {
		if got.Status == "failed" && strings.Contains(got.ErrorMsg, "爬取源不存在") {
			listedFailed++
		}
	}
	if listedFailed != 105 {
		t.Errorf("查询孤儿错误结果不足: %d", listedFailed)
	}
	// scanner 不会选中活跃 running，直接执行同样不能抢占它。
	if err := svc.RunBatch(ctxA, active.ID); err == nil {
		t.Error("活跃孤儿不应允许执行")
	}
	if got := fa12ReadBatch(t, db, active); got.Status != "running" || !got.StartedAt.Equal(fresh) || got.FinishedAt != nil {
		t.Errorf("抢占活跃孤儿: %+v", got)
	}
	if err := svc.RunBatch(ctxA, other.ID); err == nil {
		t.Error("跨租户执行未拒绝")
	}
	if got := fa12ReadBatch(t, db, other); got.Status != "pending" || got.StartedAt != nil {
		t.Errorf("跨租户消费孤儿: %+v", got)
	}
	if err := scan(ctxB); err != nil {
		t.Fatal(err)
	}
	if got := fa12ReadBatch(t, db, other); got.Status != "failed" || !strings.Contains(got.ErrorMsg, "爬取源不存在") {
		t.Errorf("租户8孤儿未恢复: %+v", got)
	}
}

// 源查询的真实 PostgreSQL 临时错误不能变成不存在；保持原状态供下一次恢复。
func TestFA12Recovery_OrphanSourceQueryTransient(t *testing.T) {
	for _, status := range []string{"pending", "running"} {
		for _, deleted := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/deleted=%t", status, deleted), func(t *testing.T) {
				db := fa12RecoveryDB(t)
				ctx := tenant.WithTenantID(context.Background(), 7)
				src := fa12RecoverySource(t, db, 7)
				old := time.Now().Add(-time.Hour).Truncate(time.Microsecond)
				var started *time.Time
				if status == "running" {
					started = &old
				}
				batch := fa12RecoveryBatch(t, db, src, status, started)
				if deleted {
					if err := kbrepo.NewKBCrawlerRepository(db).DeleteSource(ctx, src.ID); err != nil {
						t.Fatal(err)
					}
				}
				var injected atomic.Bool
				if err := db.Callback().Query().Before("gorm:query").Register("fa12:source_transient", func(tx *gorm.DB) {
					if tx.Statement.Table == "kb_crawler_sources" && injected.CompareAndSwap(false, true) {
						// SQL 本身由私有合成 PG 执行，返回可重试的 serialization_failure。
						tx.AddError(db.WithContext(tx.Statement.Context).Exec("DO $$ BEGIN RAISE EXCEPTION 'fa12 transient source read' USING ERRCODE = '40001'; END $$").Error)
					}
				}); err != nil {
					t.Fatal(err)
				}
				svc := fa12RecoveryService(db)
				err := svc.RunBatch(ctx, batch.ID)
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) || pgErr.Code != "40001" {
					t.Errorf("源读取临时错误丢失/误报不存在: %v", err)
				}
				got := fa12ReadBatch(t, db, batch)
				if got.Status != status || got.FinishedAt != nil || got.ErrorMsg != "" || (started == nil && got.StartedAt != nil) || (started != nil && (got.StartedAt == nil || !got.StartedAt.Equal(old))) {
					t.Fatalf("临时错误误终结或更改认领: %+v", got)
				}
				if _, err := svc.ResumeBatches(ctx); err != nil {
					t.Fatal(err)
				}
				got = fa12ReadBatch(t, db, batch)
				if got.Status != "failed" || got.FinishedAt == nil {
					t.Errorf("临时错误后无法恢复: %+v", got)
				}
				if deleted && !strings.Contains(got.ErrorMsg, "爬取源不存在") {
					t.Errorf("确认软删后未保存原因: %+v", got)
				}
				if !deleted && !strings.Contains(got.ErrorMsg, "sitemap") {
					t.Errorf("有效源未到达真实执行路径: %+v", got)
				}
			})
		}
	}
}

// 并发恢复孤儿沿用 CAS；只有一个 worker 能认领并写入终态。
func TestFA12Recovery_OrphanConcurrentClaim(t *testing.T) {
	db := fa12RecoveryDB(t)
	ctx := tenant.WithTenantID(context.Background(), 7)
	src := fa12RecoverySource(t, db, 7)
	batch := fa12RecoveryBatch(t, db, src, "pending", nil)
	if err := kbrepo.NewKBCrawlerRepository(db).DeleteSource(ctx, src.ID); err != nil {
		t.Fatal(err)
	}
	var reads, claims, finishes atomic.Int32
	ready, release := make(chan struct{}, 2), make(chan struct{})
	if err := db.Callback().Query().After("gorm:query").Register("fa12:orphan_barrier", func(tx *gorm.DB) {
		if tx.Statement.Table == "kb_crawl_batches" && reads.Add(1) <= 2 {
			ready <- struct{}{}
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Update().After("gorm:update").Register("fa12:orphan_claims", func(tx *gorm.DB) {
		if fields, ok := tx.Statement.Dest.(map[string]any); ok && tx.Statement.Table == "kb_crawl_batches" && tx.Error == nil && tx.RowsAffected == 1 {
			if fields["status"] == "running" {
				claims.Add(1)
			}
			if fields["status"] == "failed" {
				finishes.Add(1)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	for range 2 {
		utils.SafeGo(ctx, "fa12-orphan-claim", func(ctx context.Context) { defer wg.Done(); _ = fa12RecoveryService(db).RunBatch(ctx, batch.ID) })
	}
	for range 2 {
		select {
		case <-ready:
		case <-time.After(3 * time.Second):
			close(release)
			wg.Wait()
			t.Fatal("孤儿 worker 未到达认领屏障")
		}
	}
	close(release)
	wg.Wait()
	got := fa12ReadBatch(t, db, batch)
	if claims.Load() != 1 || finishes.Load() != 1 || got.Status != "failed" || got.FinishedAt == nil {
		t.Fatalf("孤儿重复/未认领终结: claims=%d finishes=%d batch=%+v", claims.Load(), finishes.Load(), got)
	}
	t.Logf("orphan workers=2 claims=%d finishes=%d", claims.Load(), finishes.Load())
}

// 孤儿终态写失败或令牌已变化时不能声称完成；过期后仍由实际恢复入口重试。
func TestFA12Recovery_OrphanFinishRecovery(t *testing.T) {
	for _, fault := range []string{"write_error", "new_claim"} {
		t.Run(fault, func(t *testing.T) {
			db := fa12RecoveryDB(t)
			ctx := logger.WithTraceID(tenant.WithTenantID(context.Background(), 7), "fa12-orphan-finish")
			src := fa12RecoverySource(t, db, 7)
			batch := fa12RecoveryBatch(t, db, src, "pending", nil)
			repo := kbrepo.NewKBCrawlerRepository(db)
			if err := repo.DeleteSource(ctx, src.ID); err != nil {
				t.Fatal(err)
			}
			var injected atomic.Bool
			var cleanupCtx context.Context
			newToken := time.Now().Add(time.Second).Truncate(time.Microsecond)
			if err := db.Callback().Update().Before("gorm:update").Register("fa12:orphan_finish_fault", func(tx *gorm.DB) {
				fields, ok := tx.Statement.Dest.(map[string]any)
				if !ok || tx.Statement.Table != "kb_crawl_batches" || fields["status"] != "failed" || !injected.CompareAndSwap(false, true) {
					return
				}
				cleanupCtx = tx.Statement.Context
				if fault == "write_error" {
					tx.AddError(db.WithContext(ctx).Exec("DO $$ BEGIN RAISE EXCEPTION 'fa12 transient finish write' USING ERRCODE = '40001'; END $$").Error)
				} else {
					// 在真实完成 UPDATE 前模拟另一认领令牌；原完成语句必须匹配0行。
					tx.AddError(db.WithContext(ctx).Model(&model.KBCrawlBatch{}).Where("id = ?", batch.ID).Update("started_at", newToken).Error)
				}
			}); err != nil {
				t.Fatal(err)
			}
			svc := fa12RecoveryService(db)
			err := svc.RunBatch(ctx, batch.ID)
			if err == nil {
				t.Fatal("终态失败/失效令牌被当作完成")
			}
			if fault == "write_error" {
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) || pgErr.Code != "40001" {
					t.Errorf("未保留终态持久化错误: %v", err)
				}
			} else if !strings.Contains(err.Error(), "认领已失效") {
				t.Errorf("未报告完成栅栏拒绝: %v", err)
			}
			if cleanupCtx == nil {
				t.Fatal("孤儿未进入完成写")
			}
			if deadline, ok := cleanupCtx.Deadline(); !ok || time.Until(deadline) > 10*time.Second || tenant.WriteTenantID(cleanupCtx) != 7 || tenant.IsSystemScope(cleanupCtx) || logger.TraceIDFromContext(cleanupCtx) != "fa12-orphan-finish" {
				t.Errorf("终态上下文缺少有界期限/租户/trace: %v", cleanupCtx)
			}
			got := fa12ReadBatch(t, db, batch)
			if got.Status != "running" || got.FinishedAt != nil {
				t.Fatalf("失败完成写误终结: %+v", got)
			}
			if fault == "new_claim" && !got.StartedAt.Equal(newToken) {
				t.Errorf("旧worker覆盖新令牌: %+v", got)
			}
			// 仅修改本测试合成行以模拟35min失效，不等待真实租约期。
			old := time.Now().Add(-time.Hour)
			if err := db.WithContext(ctx).Model(&model.KBCrawlBatch{}).Where("id = ?", batch.ID).Update("started_at", old).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := svc.ResumeBatches(ctx); err != nil {
				t.Fatal(err)
			}
			if got := fa12ReadBatch(t, db, batch); got.Status != "failed" || got.FinishedAt == nil || !strings.Contains(got.ErrorMsg, "爬取源不存在") {
				t.Fatalf("失效后孤儿无法再次恢复: %+v", got)
			}
		})
	}
}

// 前100条恢复任务不含合法到期源时，scanner 不得为其队尾持久批次重复建单。
func TestFA12Recovery_DueSourceExistingBatchBeyondRecoveryPage(t *testing.T) {
	for _, tc := range []struct {
		name          string
		status        string
		nilAutoResync bool
	}{
		{name: "pending-auto-true", status: "pending"},
		{name: "expired-running-auto-true", status: "running"},
		{name: "pending-auto-nil", status: "pending", nilAutoResync: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fa12DueSourceBeyondPage(t, tc.status, tc.nilAutoResync)
		})
	}
}

func fa12DueSourceBeyondPage(t *testing.T, status string, nilAutoResync bool) {
	t.Helper()
	db := fa12RecoveryDB(t)
	ctx := tenant.WithTenantID(context.Background(), 7)
	repo := kbrepo.NewKBCrawlerRepository(db)
	blocker := fa12RecoverySource(t, db, 7)
	for range 100 {
		fa12RecoveryBatch(t, db, blocker, "pending", nil)
	}
	if err := repo.DeleteSource(ctx, blocker.ID); err != nil {
		t.Fatal(err)
	}
	// scanner 必须识别的是真实到期业务源：active、verified、auto_resync=true、未曾爬取。
	autoResync := true
	target := fa12RecoverySource(t, db, 7)
	if err := repo.UpdateSourceFields(ctx, target.ID, map[string]any{"auto_resync": autoResync}); err != nil {
		t.Fatal(err)
	}
	if nilAutoResync {
		if err := repo.UpdateSourceFields(ctx, target.ID, map[string]any{"auto_resync": nil}); err != nil {
			t.Fatal(err)
		}
	}
	targetRow, err := repo.GetSourceByID(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if (nilAutoResync && targetRow.AutoResync != nil) || (!nilAutoResync && (targetRow.AutoResync == nil || !*targetRow.AutoResync)) {
		t.Fatalf("数据库auto_resync不符: %+v", targetRow)
	}
	if target.LastCrawlAt != nil || !target.SiteVerified || target.Status != model.KBCrawlerSourceStatusActive {
		t.Fatalf("fixture 不是合法 due source: %+v", target)
	}
	var started *time.Time
	if status == "running" {
		old := time.Now().Add(-time.Hour)
		started = &old
	}
	existing := fa12RecoveryBatch(t, db, target, status, started)
	if existing.ID <= 100 {
		t.Fatalf("目标批次应位于恢复页外: id=%d", existing.ID)
	}
	scan := KBSourceDueScanner(fa12RecoveryService(db))
	if err := scan(ctx); err != nil {
		t.Fatal(err)
	}
	countAfterFirstScan := fa12RecoverySourceBatchCount(t, db, ctx, target.ID)
	t.Logf("first scanner: preexisting_batch=%d source_batch_count=%d (recovery page=100)", existing.ID, countAfterFirstScan)
	if countAfterFirstScan != 1 {
		t.Errorf("恢复页外已有pending仍重复建单: count=%d，期望1", countAfterFirstScan)
	}
	// 下一轮恢复页能取到原批次并执行；即使其失败，本轮也不额外创建批次。
	if err := scan(ctx); err != nil {
		t.Fatal(err)
	}
	got := fa12ReadBatch(t, db, existing)
	if got.Status != model.KBCrawlBatchStatusFailed || got.FinishedAt == nil {
		t.Errorf("后续扫描未处理原批次: %+v", got)
	}
	if count := fa12RecoverySourceBatchCount(t, db, ctx, target.ID); count != 1 {
		t.Errorf("后续扫描产生重复批次: count=%d，期望1", count)
	}
}

// 活跃running应阻止本租户due source新建；相同 source_id 的其它租户批次不可阻止。
func TestFA12Recovery_DueSourceOpenBatchTenantScope(t *testing.T) {
	t.Run("active-running", func(t *testing.T) {
		db := fa12RecoveryDB(t)
		ctx := tenant.WithTenantID(context.Background(), 7)
		repo := kbrepo.NewKBCrawlerRepository(db)
		autoResync := true
		src := fa12RecoverySource(t, db, 7)
		if err := repo.UpdateSourceFields(ctx, src.ID, map[string]any{"auto_resync": autoResync}); err != nil {
			t.Fatal(err)
		}
		fresh := time.Now().UTC().Truncate(time.Microsecond)
		active := fa12RecoveryBatch(t, db, src, "running", &fresh)
		if err := KBSourceDueScanner(fa12RecoveryService(db))(ctx); err != nil {
			t.Fatal(err)
		}
		if count := fa12RecoverySourceBatchCount(t, db, ctx, src.ID); count != 1 {
			t.Fatalf("活跃running期间又建批次: count=%d", count)
		}
		got := fa12ReadBatch(t, db, active)
		if got.Status != "running" || got.StartedAt == nil || !got.StartedAt.Equal(fresh) {
			t.Fatalf("活跃批次被抢占: %+v", got)
		}
	})

	t.Run("other-tenant-does-not-hide-due", func(t *testing.T) {
		db := fa12RecoveryDB(t)
		ctx := tenant.WithTenantID(context.Background(), 7)
		repo := kbrepo.NewKBCrawlerRepository(db)
		autoResync := true
		src := fa12RecoverySource(t, db, 7)
		if err := repo.UpdateSourceFields(ctx, src.ID, map[string]any{"auto_resync": autoResync}); err != nil {
			t.Fatal(err)
		}
		// 构造跨租户同 source_id 的合成行，确保租户约束而非 source_id 偶然隔离。
		foreign := model.KBCrawlBatch{TenantID: 8, SourceID: src.ID, BatchNo: "foreign-pending", Trigger: "manual", Status: "pending"}
		if err := db.WithContext(tenant.WithTenantID(context.Background(), 8)).Create(&foreign).Error; err != nil {
			t.Fatal(err)
		}
		if err := KBSourceDueScanner(fa12RecoveryService(db))(ctx); err != nil {
			t.Fatal(err)
		}
		if count := fa12RecoverySourceBatchCount(t, db, ctx, src.ID); count != 1 {
			t.Fatalf("其它租户任务影响当前租户 due 决策: 租户7行数=%d", count)
		}
		got := fa12ReadBatch(t, db, foreign)
		if got.Status != "pending" {
			t.Fatalf("当前租户改动了租户8批次: %+v", got)
		}
		// 直接service查询也应剥除上游SystemScope，缺租户必须拒绝。
		svc := fa12RecoveryService(db)
		if open, err := svc.HasPendingOrRunningBatch(tenant.WithSystemScope(ctx), src.ID); err != nil || open {
			t.Errorf("系统豁免导致读取其它租户未完成批次: open=%t err=%v", open, err)
		}
		if _, err := svc.HasPendingOrRunningBatch(context.Background(), src.ID); err == nil {
			t.Error("无租户存在性查询未fail closed")
		}
	})
}

// 已有批次存在性查询发生暂态数据库错误时，scanner 必须返回错误且不新建批次。
func TestFA12Recovery_DueSourceLookupErrorFailsClosed(t *testing.T) {
	db := fa12RecoveryDB(t)
	ctx := tenant.WithTenantID(context.Background(), 7)
	repo := kbrepo.NewKBCrawlerRepository(db)
	autoResync := true
	src := fa12RecoverySource(t, db, 7)
	if err := repo.UpdateSourceFields(ctx, src.ID, map[string]any{"auto_resync": autoResync}); err != nil {
		t.Fatal(err)
	}
	var batchQueries atomic.Int32
	var injected atomic.Bool
	if err := db.Callback().Query().Before("gorm:query").Register("fa12:due_lookup_transient", func(tx *gorm.DB) {
		if tx.Statement.Table == "kb_crawl_batches" && batchQueries.Add(1) == 2 && injected.CompareAndSwap(false, true) {
			// 第一次是 ResumeBatches 的队列页查询；第二次是 due source 的存在性查询。
			tx.AddError(db.WithContext(tx.Statement.Context).Exec("DO $$ BEGIN RAISE EXCEPTION 'fa12 transient open batch lookup' USING ERRCODE = '40001'; END $$").Error)
		}
	}); err != nil {
		t.Fatal(err)
	}
	err := KBSourceDueScanner(fa12RecoveryService(db))(ctx)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "40001" {
		t.Errorf("存在性查询错误未fail closed/未保留PG错误: %v", err)
	}
	if !injected.Load() {
		t.Fatal("测试未注入到存在性查询路径")
	}
	if count := fa12RecoverySourceBatchCount(t, db, ctx, src.ID); count != 0 {
		t.Fatalf("查询错误时仍创建批次: count=%d", count)
	}
	if pgErr != nil {
		t.Logf("tenant=7 source=%d lookup_error=SQLSTATE %s created_batches=0", src.ID, pgErr.Code)
	}
}

// 两个 worker 先同时读 pending，再竞争认领。必须只有一个 UPDATE 真的获得行。
func TestFA12Recovery_ConcurrentClaim(t *testing.T) {
	db := fa12RecoveryDB(t)
	src := fa12RecoverySource(t, db, 7)
	batch := fa12RecoveryBatch(t, db, src, "pending", nil)
	var queries, claims atomic.Int32
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	if err := db.Callback().Query().After("gorm:query").Register("fa12:claim_barrier", func(tx *gorm.DB) {
		if tx.Statement.Table == "kb_crawl_batches" && queries.Add(1) <= 2 {
			ready <- struct{}{}
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Update().After("gorm:update").Register("fa12:claim_count", func(tx *gorm.DB) {
		if fields, ok := tx.Statement.Dest.(map[string]any); ok && tx.Statement.Table == "kb_crawl_batches" && fields["status"] == "running" && tx.Error == nil && tx.RowsAffected == 1 {
			claims.Add(1)
		}
	}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	for range 2 {
		go func() {
			defer wg.Done()
			_ = fa12RecoveryService(db).RunBatch(tenant.WithTenantID(context.Background(), 7), batch.ID)
		}()
	}
	for range 2 {
		select {
		case <-ready:
		case <-time.After(3 * time.Second):
			close(release)
			wg.Wait()
			t.Fatal("worker 未到达认领屏障")
		}
	}
	close(release)
	wg.Wait()
	if claims.Load() != 1 {
		t.Fatalf("实际 PostgreSQL 重复认领，count=%d", claims.Load())
	}
	t.Logf("two workers, successful claims=%d terminal=%s", claims.Load(), fa12ReadBatch(t, db, batch).Status)
}

// 真实租户 callback 隔离同 URL 文档；更新0行应被当作失败返回给处理链。
func TestFA12Recovery_DocumentTenantAndMissingUpdate(t *testing.T) {
	db := fa12RecoveryDB(t)
	repo := kbrepo.NewKBCrawlerRepository(db)
	ctxA, ctxB := tenant.WithTenantID(context.Background(), 7), tenant.WithTenantID(context.Background(), 8)
	a := model.KBDocument{TenantID: 7, Title: "https://example.test/page", FileType: ".html", Status: model.KBDocumentStatusFailed, Content: "tenant A"}
	b := model.KBDocument{TenantID: 8, Title: a.Title, FileType: a.FileType, Status: model.KBDocumentStatusFailed, Content: "tenant B"}
	if err := db.WithContext(ctxA).Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctxB).Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		ctx     context.Context
		id      uint
		content string
	}{{ctxA, a.ID, "tenant A"}, {ctxB, b.ID, "tenant B"}} {
		got, err := repo.FindHTMLDocByTitle(tc.ctx, a.Title)
		if err != nil || got.ID != tc.id || got.Content != tc.content {
			t.Fatalf("同URL文档串租户: got=%+v err=%v", got, err)
		}
	}
	if err := repo.UpdateDocumentFields(ctxA, b.ID, map[string]any{"status": model.KBDocumentStatusIndexed}); err == nil {
		t.Error("跨租户 UPDATE 0行必须返回错误")
	}
	if err := kbrepo.NewKBDocumentRepository(db).UpdateStatusFields(ctxA, b.ID, map[string]any{"status": model.KBDocumentStatusIndexed}); err == nil {
		t.Error("索引状态 UPDATE 0行必须返回错误")
	}
	got, err := repo.GetDocumentByID(ctxB, b.ID)
	if err != nil || got.Status != model.KBDocumentStatusFailed {
		t.Fatalf("租户B被污染: got=%+v err=%v", got, err)
	}
	if _, err := repo.GetDocumentByID(context.Background(), a.ID); err == nil {
		t.Fatal("无租户查询未 fail closed")
	}
}

// 完成写必须匹配本次 started_at；其它租户、旧令牌都不能结束新 worker。
func TestFA12Recovery_CompletionFence(t *testing.T) {
	db := fa12RecoveryDB(t)
	repo := kbrepo.NewKBCrawlerRepository(db)
	src := fa12RecoverySource(t, db, 7)
	old := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
	batch := fa12RecoveryBatch(t, db, src, "running", &old)
	ctx := tenant.WithTenantID(context.Background(), 7)
	now := time.Now().UTC().Truncate(time.Microsecond)
	ok, err := repo.ClaimBatchByStartedAt(ctx, batch.ID, now, now.Add(-35*time.Minute))
	if err != nil || !ok {
		t.Fatalf("过期认领失败: %v", err)
	}
	for _, tc := range []struct {
		ctx   context.Context
		token time.Time
	}{{ctx, old}, {tenant.WithTenantID(context.Background(), 8), now}} {
		ok, err := repo.FinishClaimedBatch(tc.ctx, batch.ID, tc.token, map[string]any{"status": "succeeded"})
		if err != nil || ok {
			t.Fatalf("旧/跨租户令牌不能完成: ok=%v err=%v", ok, err)
		}
	}
	if got := fa12ReadBatch(t, db, batch); got.Status != "running" || !got.StartedAt.Equal(now) {
		t.Fatalf("栅栏未保留新认领: %+v", got)
	}
	ok, err = repo.FinishClaimedBatch(ctx, batch.ID, now, map[string]any{"status": "failed", "finished_at": time.Now()})
	if err != nil || !ok {
		t.Fatalf("匹配令牌未完成: %v", err)
	}
}

// 同一个测试二进制的独立 OS 进程：先停在已持久 running，再 kill；新 PID 经 cron 恢复。
func TestFA12Recovery_ProcessRestart(t *testing.T) {
	mode := os.Getenv("FA12_RECOVERY_CHILD")
	db := fa12RecoveryDB(t)
	if mode != "" {
		if mode == "claim" {
			if err := db.Callback().Update().After("gorm:commit_or_rollback_transaction").Register("fa12:claimed", func(tx *gorm.DB) {
				if f, ok := tx.Statement.Dest.(map[string]any); ok && tx.Statement.Table == "kb_crawl_batches" && f["status"] == "running" && tx.RowsAffected == 1 && tx.Error == nil {
					if err := os.WriteFile(os.Getenv("FA12_RECOVERY_READY"), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
						panic(err)
					}
					select {}
				}
			}); err != nil {
				t.Fatal(err)
			}
			id, _ := strconv.ParseUint(os.Getenv("FA12_RECOVERY_BATCH"), 10, 64)
			if err := fa12RecoveryService(db).RunBatch(tenant.WithTenantID(context.Background(), 7), id); err != nil {
				t.Fatal(err)
			}
		} else if err := KBSourceDueScanner(fa12RecoveryService(db))(tenant.WithTenantID(context.Background(), 7)); err != nil {
			t.Fatal(err)
		}
		return
	}
	src := fa12RecoverySource(t, db, 7)
	batch := fa12RecoveryBatch(t, db, src, "pending", nil)
	ready := t.TempDir() + "/claimed"
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(exe, "-test.run=^TestFA12Recovery_ProcessRestart$", "-test.v")
	child.Env = append(os.Environ(), "FA12_RECOVERY_CHILD=claim", "FA12_RECOVERY_READY="+ready, "FA12_RECOVERY_BATCH="+fmt.Sprint(batch.ID))
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	killed := false
	defer func() {
		if !killed {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	}()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		select {
		case <-tick.C:
		case <-timer.C:
			t.Fatal("子进程未持久认领")
		}
	}
	got := fa12ReadBatch(t, db, batch)
	if got.Status != "running" || got.StartedAt == nil {
		t.Fatalf("kill 前未持久 running: %+v", got)
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	killed = true
	// 不等待35分钟：只将自己合成行的认领时刻改为过期，明确测试过期恢复条件。
	if err := db.WithContext(tenant.WithTenantID(context.Background(), 7)).Model(&model.KBCrawlBatch{}).Where("id = ?", batch.ID).Update("started_at", time.Now().Add(-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	restart := exec.Command(exe, "-test.run=^TestFA12Recovery_ProcessRestart$", "-test.v")
	restart.Env = append(os.Environ(), "FA12_RECOVERY_CHILD=recover")
	output, err := restart.CombinedOutput()
	t.Logf("killed pid=%d; recovery process output:\n%s", child.Process.Pid, output)
	if err != nil {
		t.Fatal(err)
	}
	got = fa12ReadBatch(t, db, batch)
	if got.Status != "failed" || got.FinishedAt == nil {
		t.Fatalf("新进程未通过 cron 恢复持久任务: %+v", got)
	}
}
