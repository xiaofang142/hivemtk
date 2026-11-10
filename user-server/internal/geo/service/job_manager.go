package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"

	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

// JobScheduler 调度器抽象（由 internal/pkg/cron.TaskManager 实现），
// 避免 geoservice → pkg/cron 反向依赖形成 import cycle。
type JobScheduler interface {
	AddTask(spec string, cmd func()) (cron.EntryID, error)
	RemoveTask(id cron.EntryID)
}

const (
	JobSOVRefresh      = "sov_refresh"
	JobNegativeMonitor = "negative_monitor"
	JobSourceSync      = "source_sync"
	JobCrawlerMonitor  = "crawler_monitor"
	JobKeywordPipeline = "keyword_pipeline"
	JobCitationDrift   = "citation_drift" // A10：引用漂移每日快照
	JobSiteAudit       = "site_audit"     // P0：启用站点月度体检 + 分数下降告警

	geoJobRunRetention = 30 * 24 * time.Hour
)

// jobKey 「任务 × 租户」维度的调度/互斥/记账键。
//
// 为什么必须含租户（二审 P1-2）：geo_config.cron_specs 是**逐租户持久化**的
// （UpdateSchedule 在请求租户 ctx 下只写本租户行），cron 条目、spec 快照与
// running 互斥位因此都是 (任务, 租户) 维度的。若退回按任务名单键：
//   - 各租户 spec 相同（最常见：都用默认值）时同时到点，抢到全局 running 位的
//     那个租户跑完之前，其余租户的同任务全部被跳过 ⇒ 只有先到者永远在跑；
//   - 手动触发（StartJob，经 Fanout 逐租户）会被任意一个在跑的租户挡住。
type jobKey struct {
	name   string
	tenant uint
}

// tenantIDFromCtx 取 ctx 绑定的租户；无绑定（off 模式 / 后台裸 ctx）按默认租户。
func tenantIDFromCtx(ctx context.Context) uint {
	if id, ok := tenant.FromContext(ctx); ok {
		return id
	}
	return tenant.DefaultTenantID
}

// lockKey 分布式锁键：含租户 —— 租户 A 的长任务不该把租户 B 的同名任务挡在门外。
func lockKey(jobName string, tenantID uint) string {
	return fmt.Sprintf("%s/%d", jobName, tenantID)
}

type geoJobDef struct {
	Name        string
	Description string
	DefaultSpec string
	Timeout     time.Duration
	Fn          func(ctx context.Context) (string, error)
}

var geoJobDefs = []geoJobDef{
	{
		Name:        JobSOVRefresh,
		Description: "SOV 刷新：采样关键词跑多引擎探针并聚合 daily_stats",
		DefaultSpec: "0 0 2 * * *",
		Timeout:     30 * time.Minute,
		Fn:          sovRefreshJob,
	},
	{
		Name:        JobNegativeMonitor,
		Description: "负面监控：品牌名 × 负面关键词探针扫描，命中写 geo_alerts",
		DefaultSpec: "0 */30 * * * *",
		Timeout:     15 * time.Minute,
		Fn:          negativeMonitorJob,
	},
	{
		Name:        JobSourceSync,
		Description: "信源目录同步：种子 URL 可达性检查并刷新 last_checked",
		DefaultSpec: "0 0 3 * * *",
		Timeout:     30 * time.Minute,
		Fn:          sourceCatalogSyncJob,
	},
	{
		Name:        JobCrawlerMonitor,
		Description: "竞品监控爬虫：关键词驱动抓取 HiveMTK 与竞品落地页",
		DefaultSpec: "0 0 */6 * * *",
		Timeout:     20 * time.Minute,
		Fn:          crawlerMonitorJob,
	},
	{
		Name:        JobKeywordPipeline,
		Description: "关键词批量管线：4业务线×技术栈种子→长尾/下拉展开→落库→有界LLM SEO（生成/评分/验证/探针）",
		DefaultSpec: "0 0 4 * * *",
		Timeout:     60 * time.Minute,
		Fn:          keywordPipelineJob,
	},
	{
		// A10（2026-10-24）：引用漂移快照。排 05:00（在 sov_refresh 02:00 之后）——
		// 快照的是"业务日已完成探针"的当日聚合，探针跑完才有得聚。
		Name:        JobCitationDrift,
		Description: "引用漂移快照：聚合当日探针引用 URL 计数写入 geo_citation_snapshots（diff 用的两个时点）",
		DefaultSpec: "0 0 5 * * *",
		Timeout:     15 * time.Minute,
		Fn:          citationDriftJob,
	},
	{
		// 月度复查：每月 1 日 06:00 给启用中的 geo_sites 各跑一次 rubric v3。
		// 结果落审计历史，分数较上次下降 >8 分写 geo_alerts（图3 的 geo-compare 闭环）。
		Name:        JobSiteAudit,
		Description: "站点月度体检：对启用站点跑 rubric v3 并落审计历史；分数下降 >8 分写告警",
		DefaultSpec: "0 0 6 1 * *",
		Timeout:     40 * time.Minute,
		Fn:          siteAuditJob,
	},
}

var specParser = cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// JobManager GEO 定时任务统一管理：
//  1. 同任务互斥防重入（上一轮未跑完则跳过本轮）
//  2. panic 恢复 + 超时控制
//  3. 每次运行落 geo_job_runs 历史（管理端可查）
//  4. cron 表达式可经管理端 API 调整并持久化到 GeoConfig.CronSpecs
//  5. 分布式锁：多实例部署时防止同一任务重复执行
type JobManager struct {
	mu      sync.RWMutex
	sched   JobScheduler
	runRepo repository.GeoJobRunRepository
	cfgRepo repository.GeoConfigRepository
	// lockRepo 分布式锁接缝（AV-04c 批次3：原 *gorm.DB 字段改仓储注入，
	// 句柄解析留在 repository.GeoJobLockRepository；nil = 未装配 = 不参与锁，
	// 与改前 m.db == nil 的短路语义一致）。
	lockRepo repository.GeoJobLockRepository

	// runMu 只护 running 这张**动态键表**（键含租户，运行期才会出现新键）。
	// 与 m.mu 分开：ListJobs 读 running 时可能已持 m.mu.RLock，混用同一把锁
	// 会出现 RLock 内再 Lock 的自锁。
	runMu   sync.Mutex
	running map[jobKey]*atomic.Bool
	specs   map[jobKey]string
	entryID map[jobKey]cron.EntryID
	setup   bool
}

var (
	jobManagerOnce sync.Once
	jobManager     *JobManager
)

// GetGeoJobManager 进程级单例。未 Setup 前也可手动触发任务（无调度注册）。
func GetGeoJobManager() *JobManager {
	jobManagerOnce.Do(func() {
		running := make(map[jobKey]*atomic.Bool)
		specs := make(map[jobKey]string, len(geoJobDefs))
		for _, d := range geoJobDefs {
			// 默认租户的默认 spec 先占位：Setup 之前（无持久化行）ListJobs 也能
			// 给出可读的默认调度；其余租户的键在 SetupGeoJobs/UpdateSchedule 出现。
			specs[jobKey{name: d.Name, tenant: tenant.DefaultTenantID}] = d.DefaultSpec
		}
		jobManager = &JobManager{
			runRepo: repository.NewGeoJobRunRepository(),
			cfgRepo: repository.NewGeoConfigRepository(),
			running: running,
			specs:   specs,
			entryID: map[jobKey]cron.EntryID{},
		}
	})
	return jobManager
}

// runningFor 取（或首次创建）「任务 × 租户」的互斥位。键是动态的（租户由
// Fanout/注册闭包给出），必须在 runMu 下惰性创建，不能在构造期预铺。
func (m *JobManager) runningFor(k jobKey) *atomic.Bool {
	m.runMu.Lock()
	defer m.runMu.Unlock()
	if b, ok := m.running[k]; ok {
		return b
	}
	b := &atomic.Bool{}
	m.running[k] = b
	return b
}

// runningState 只读快照（键不存在视为未在跑）。
func (m *JobManager) runningState(k jobKey) bool {
	m.runMu.Lock()
	defer m.runMu.Unlock()
	if b, ok := m.running[k]; ok {
		return b.Load()
	}
	return false
}

// SetJobManagerLockRepo 注入分布式锁仓储（装配点 internal/pkg/cron.InitCron）。
// nil 视为不注入：不参与多实例互斥（单实例语义），与改前未 SetJobManagerDB 时
// m.db == nil 的短路一致。
func (m *JobManager) SetJobManagerLockRepo(repo repository.GeoJobLockRepository) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lockRepo = repo
}

// jobModuleGate GEO 任务的模块闸：每次 tick 派发前判断「本实例上是否
// 至少有一个租户开通了对应模块」（FR-5.6 派发时刻动态过滤）。
//
// 为什么是注入而不是 geoservice 直接 import internal/service：本包的依赖
// 面刻意保持在 geo/* + pkg 叶子包（见 JobScheduler 注释里的同类理由），
// 反向 import internal/service 会把整棵 service 依赖树拖进来。注入点在
// internal/pkg/cron.InitCron（那里同时 import 了 service 与 geoservice），
// 与 SetSOPSchedulerScanIntervalProvider 同一装配形态。
//
// 默认恒放行：未装配 = 不闸（fail-open，与 service.AllowWorker 对
// 未映射 worker 的口径一致 —— 宁可多跑一轮，不可静默停摆）。
var jobModuleGate = func(_ context.Context, _ string) bool { return true }

// SetGeoJobModuleGate 注入任务闸（装配期调用一次；nil 视为不注入）。
// fn 收到的是 **worker 名**（geoJobDef.Name），模块 key 映射归
// service.workerModuleKey 管 —— 本包不认模块概念。
func SetGeoJobModuleGate(fn func(ctx context.Context, workerName string) bool) {
	if fn == nil {
		return
	}
	jobModuleGate = fn
}

// SetupGeoJobs 由 cron.InitCron 调用：清理僵尸 running 记录、
// 读取持久化调度配置并注册全部 GEO 任务。
func SetupGeoJobs(sched JobScheduler) {
	m := GetGeoJobManager()
	m.mu.Lock()
	defer m.mu.Unlock()

	// 平台级启动兜底：geo_job_runs 是租户表，进程重启后残留的 running 记录要跨
	// 全租户清理；本函数由 cron.InitCron 启动期调用，context.Background() 无租户
	// 绑定，TENANT_MODE=on 时 FailStaleRunning（update）被隔离 callback fail-closed
	// （生产日志实证："[GEO Jobs] 清理僵尸运行记录失败"）。走 tenant.WithSystemScope
	// 平台级豁免通道（与 migration/健康检查同类）。
	if err := m.runRepo.FailStaleRunning(tenant.WithSystemScope(context.Background())); err != nil {
		logger.Warn(fmt.Sprintf("[GEO Jobs] 清理僵尸运行记录失败: %v", err))
	}

	m.sched = sched
	m.setup = true

	// 逐租户读取并注册调度（specs/001-saas-multi-tenant T020，二审 P1-2）。
	//
	// 用 system scope 读 cfgRepo.Get（First）——多租户下读到的是**跨租户
	// union 的首行**（= 默认租户的 geo_config），于是租户 2 经 UpdateSchedule
	// 持久化的 cron 表达式在重启后即丢。改为 Fanout 逐活跃租户读**本租户**行，
	// 并按 (任务, 租户) 各自注册：cron 闭包钉住注册时的租户，派发时只跑该租户
	// （见 registerLocked / jobKey 注释）。off 模式 Fanout 退化为单默认租户，
	// 注册数量与旧版本一致。
	if _, err := tenant.Fanout(context.Background(), nil, func(tctx context.Context, tid uint) (int64, error) {
		persisted := m.loadPersistedSpecs(tctx)
		for _, d := range geoJobDefs {
			spec := d.DefaultSpec
			if s, ok := persisted[d.Name]; ok && m.validSpec(s) == nil {
				spec = s
			}
			m.specs[jobKey{name: d.Name, tenant: tid}] = spec
			if err := m.registerLocked(d.Name, spec, tid); err != nil {
				logger.Error(err, fmt.Sprintf("[GEO Jobs] 注册任务 %s（租户 %d）失败", d.Name, tid))
			}
		}
		return 0, nil
	}); err != nil {
		// fail-closed（Fanout 枚举器未装配等）：绝不回落成"只注册默认租户"——
		// 那会把漏装配伪装成 GEO 调度正常。宁可本轮不注册，日志必须响亮。
		logger.Error(err, "[GEO Jobs] 逐租户注册调度失败：本轮不注册任何 GEO 任务")
		return
	}
	logger.Info("[GEO Jobs] 定时任务已按租户注册（调度可经 /geo/jobs API 调整）")
}

// loadPersistedSpecs 读 GeoConfig.CronSpecs（geo_config 是租户表）。
// ctx 必须已绑定租户（Fanout 回调注入）：on 模式下读到的是**本租户**行，
// off 模式直通。不再用 system scope —— 那会读成跨租户 union 的首行。
func (m *JobManager) loadPersistedSpecs(ctx context.Context) map[string]string {
	cfg, err := m.cfgRepo.Get(ctx)
	if err != nil || strings.TrimSpace(cfg.CronSpecs) == "" {
		return nil
	}
	out := map[string]string{}
	if err := json.Unmarshal([]byte(cfg.CronSpecs), &out); err != nil {
		logger.Warn(fmt.Sprintf("[GEO Jobs] CronSpecs 解析失败，使用默认调度: %v", err))
		return nil
	}
	return out
}

func (m *JobManager) registerLocked(name, spec string, tid uint) error {
	if m.sched == nil {
		return errors.New("调度器未初始化")
	}
	if err := m.validSpec(spec); err != nil {
		return err
	}
	def, ok := m.defByName(name)
	if !ok {
		return fmt.Errorf("未知任务: %s", name)
	}
	k := jobKey{name: name, tenant: tid}
	if old, ok := m.entryID[k]; ok {
		m.sched.RemoveTask(old)
		delete(m.entryID, k)
	}
	id, err := m.sched.AddTask(spec, func() {
		// 模块闸（FR-5.6）：**每次派发时**查一次（不是注册时定死）——
		// 租户随时可能开通/关停 GEO，闸必须跟着开关实时走。未装配/未映射/
		// DB 错误 → jobModuleGate 内部 fail-open 放行。闸是模块级、租户无关
		// 判定，所以每条租户条目各问一次（同一轮重复放行只多几行日志）。
		if !jobModuleGate(context.Background(), name) {
			logger.Info(fmt.Sprintf("[GEO Jobs] 任务 %s 本轮跳过：GEO 模块无任何租户开通（模块闸）", name))
			return
		}
		// 本条目在注册时已钉住租户 tid：cron spec 是逐租户持久化的，派发也只跑
		// 该租户。**不能**在这里再做全租户扇出 —— 否则任一租户到点都会把全部
		// 租户的外部采集重跑一遍（重复调 LLM/抓取 API）。
		m.startJobForTenant(def, tenant.WithTenantID(context.Background(), tid), "cron", tid)
	})
	if err != nil {
		return fmt.Errorf("注册任务 %s 失败: %w", name, err)
	}
	m.entryID[k] = id
	return nil
}

func (m *JobManager) validSpec(spec string) error {
	if strings.TrimSpace(spec) == "" {
		return errors.New("cron 表达式不能为空")
	}
	if _, err := specParser.Parse(spec); err != nil {
		return fmt.Errorf("cron 表达式非法（6 段含秒或 @every）: %w", err)
	}
	return nil
}

// StartJob 异步触发一次任务运行（**未指定租户**的入口：管理端 Trigger、
// 手动触发等）。on 模式经 Fanout 逐活跃租户各跑一次（每租户一条独立运行记录、
// 独立 running 互斥位与分布式锁）；off 模式退化为单默认租户。
//
// 返回 (false, nil) 表示没有任何租户被启动（全部因在跑/被他实例锁定而跳过）；
// (false, err) 表示任务名未知或租户枚举不可用（fail-closed，不猜测租户集合）。
// cron 派发走 registerLocked 闭包里的 startJobForTenant（单租户），不经本方法，
// 因此不存在双重扇出。
func (m *JobManager) StartJob(name, trigger string) (bool, error) {
	def, ok := m.defByName(name)
	if !ok {
		return false, fmt.Errorf("未知任务: %s", name)
	}
	started := false
	if _, err := tenant.Fanout(context.Background(), nil, func(ctx context.Context, tid uint) (int64, error) {
		if m.startJobForTenant(def, ctx, trigger, tid) {
			started = true
		}
		return 0, nil
	}); err != nil {
		return false, err
	}
	return started, nil
}

// startJobForTenant 单租户启动：running 互斥 → 分布式锁（键含租户）→ 在**本租户**
// ctx 下落运行记录 → 异步执行。调用方 ctx 必须已绑定 tid（cron 闭包 / Fanout 回调）。
// 返回 true 表示本次真的启动了。
func (m *JobManager) startJobForTenant(def geoJobDef, ctx context.Context, trigger string, tid uint) bool {
	k := jobKey{name: def.Name, tenant: tid}
	flag := m.runningFor(k)
	if !flag.CompareAndSwap(false, true) {
		logger.Info(fmt.Sprintf("[GEO Jobs] 任务 %s（租户 %d）上一轮仍在运行，跳过本次触发（trigger=%s）", def.Name, tid, trigger))
		return false
	}

	// 分布式锁：多实例部署时防止同一任务（同一租户）重复执行。
	// handle 非 nil 表示本次真的拿到了会话级 advisory lock —— 它扣住一条独占连接，
	// 必须在任务结束时用同一条连接解锁（见 releaseDistributedLock 与
	// repository/job_lock.go 的 2026-11-10 修法说明）。
	var lockHandle *_db.AdvisoryLockHandle
	if m.lockRepo != nil {
		handle, locked, err := m.acquireDistributedLock(def.Name, tid, def.Timeout)
		switch {
		case err != nil:
			logger.Warn(fmt.Sprintf("[GEO Jobs] 任务 %s（租户 %d）获取分布式锁失败: %v", def.Name, tid, err))
		case !locked:
			logger.Info(fmt.Sprintf("[GEO Jobs] 任务 %s（租户 %d）已被其他实例锁定，跳过本次触发（trigger=%s）", def.Name, tid, trigger))
			flag.Store(false)
			return false
		default:
			lockHandle = handle
		}
	}

	// 兜底：拿锁之后若在交给 execute 之前出现任何非预期退出（Create panic 等），
	// 必须把锁还回去 —— 否则又是一次锁泄漏，而这正是本轮修的缺陷。
	handedOff := false
	if lockHandle != nil {
		defer func() {
			if !handedOff {
				m.releaseDistributedLock(lockHandle)
				flag.Store(false)
			}
		}()
	}

	run := &model.GeoJobRun{
		JobName:   def.Name,
		Trigger:   trigger,
		Status:    "running",
		StartedAt: time.Now(),
	}
	// 运行记录在**本租户** ctx 下创建：geo_job_runs 是租户表，隔离 callback 的
	// Create 会填充 tenant_id —— 记账分租户，各租户在 /geo/jobs/runs 只看到自己
	// 的历史（用 system scope 一律落列默认值 1，等于只有默认租户有历史）。
	// 记账失败仅日志：历史缺口不应阻断业务任务本身。
	var runID uint
	if err := m.runRepo.Create(ctx, run); err != nil {
		logger.Error(err, fmt.Sprintf("[GEO Jobs] 任务 %s（租户 %d）写入运行记录失败（本次不记录历史）", def.Name, tid))
	} else {
		runID = run.ID
	}

	// 先把锁的所有权交接出去，再起 goroutine：否则 execute 若瞬间跑完并已释放，
	// 上面的兜底 defer 会以为"没交接"而重复释放（Release 幂等，但语义上不该发生）。
	handedOff = true
	go m.execute(def, runID, trigger, ctx, tid, flag, lockHandle)
	return true
}

// acquireDistributedLock 尝试获取分布式锁（基于数据库 advisory lock）
// 判读：pg_try_advisory_lock 是会话级锁，SQL 不读写任何租户表行，
// 不触达租户表 —— 裸 Background 不构成隔离 callback 触点，故保持不动。
// 锁键含租户（lockKey）：调度是逐租户的，租户 A 的任务不该挡住租户 B。
//
// 返回值 handle 是**持有该锁的那条独占连接**，调用方必须原样交回
// releaseDistributedLock —— 会话级锁只能由加锁的那个会话解锁。
// SQL 形态与陷阱见 internal/pkg/db/advisory_lock.go。
func (m *JobManager) acquireDistributedLock(jobName string, tenantID uint, timeout time.Duration) (*_db.AdvisoryLockHandle, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return m.lockRepo.Acquire(ctx, advisoryLockKey(lockKey(jobName, tenantID)))
}

// releaseDistributedLock 释放在同一条连接上持有的分布式锁，并归还那条连接。
// 判读同 acquireDistributedLock：pg_advisory_unlock 不触达租户表，裸 Background 保持不动。
//
// 解锁失败（含 unlock 返回 false = 解锁落错会话）不再静默：那是锁泄漏的
// 直接症状（2026-11-10 修前的形态），必须留痕。
func (m *JobManager) releaseDistributedLock(handle *_db.AdvisoryLockHandle) {
	if handle == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := handle.Release(ctx); err != nil {
		logger.Error(err, fmt.Sprintf("[GEO Jobs] 释放分布式锁失败（锁键=%d）：该锁可能滞留到本进程连接断开", handle.Key()))
	}
}

// advisoryLockKey 把锁键字符串压成 int64（advisory lock 收 int64）。
// 用 FNV-1a 而不是内置 hash：同一进程内稳定，且不依赖随机种子。
func advisoryLockKey(key string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	return int64(h.Sum64())
}

// execute 在**单租户**上下文里跑一次任务体并回写运行记录。
// ctx 已绑定本租户（cron 闭包 / StartJob 的 Fanout 回调），tid 与之一致。
func (m *JobManager) execute(def geoJobDef, runID uint, trigger string, ctx context.Context, tid uint, flag *atomic.Bool, lockHandle *_db.AdvisoryLockHandle) {
	defer flag.Store(false)
	defer m.releaseDistributedLock(lockHandle)

	start := time.Now()
	// 每租户一份超时预算：任务级 def.Timeout 叠在本租户 ctx 上，先跑的租户不会
	// 把后面的租户饿死（与 sop_scheduler tick 的 P2-5 修法同一口径）。
	ctx, cancel := context.WithTimeout(ctx, def.Timeout)
	defer cancel()

	// 运行记录回写（Finish）直接用本租户 ctx：geo_job_runs 是租户表，这里
	// 包 system scope 会把 Create/Finish 都摊成跨租户、行落列默认值 1（记账串租户）。
	// def.Fn(ctx) 同样拿到本租户 ctx —— 各 job 入口不再自己补 system scope
	// （见 scheduler.go / keyword_pipeline.go 的判读注释；on 模式下裸 ctx 会被
	// 隔离 callback fail-closed，那是漏装配的兜底不是常态路径）。
	status, summary, errMsg := "success", "", ""
	func() {
		defer func() {
			if r := recover(); r != nil {
				status = "failed"
				errMsg = fmt.Sprintf("panic: %v\n%s", r, debug.Stack())
				logger.Error(fmt.Errorf("%s", errMsg), fmt.Sprintf("[GEO Jobs] 任务 %s panic（trigger=%s）", def.Name, trigger))
			}
		}()
		s, err := def.Fn(ctx)
		summary = s
		if err != nil {
			status = "failed"
			errMsg = err.Error()
			logger.Error(err, fmt.Sprintf("[GEO Jobs] 任务 %s 执行失败（trigger=%s）", def.Name, trigger))
		}
	}()

	duration := time.Since(start).Milliseconds()
	if runID > 0 {
		if err := m.runRepo.Finish(ctx, runID, status, runeTruncate(summary, 2000), runeTruncate(errMsg, 2000), duration); err != nil {
			logger.Error(err, fmt.Sprintf("[GEO Jobs] 任务 %s 回写运行记录失败", def.Name))
		}
	}
	logger.Info(fmt.Sprintf("[GEO Jobs] 任务 %s（租户 %d）完成（trigger=%s status=%s 用时=%dms）%s",
		def.Name, tid, trigger, status, duration, summary))

	// 过期运行记录清理是**跨租户**的保留期维护（30 天），与本租户记账无关：
	// 用 system scope 豁免通道，否则 on 模式下只能清到当前租户这一份。
	if err := m.runRepo.DeleteBefore(tenant.WithSystemScope(ctx), time.Now().Add(-geoJobRunRetention)); err != nil {
		logger.Warn(fmt.Sprintf("[GEO Jobs] 清理过期运行记录失败: %v", err))
	}
}

// UpdateSchedule 调整任务 cron 表达式并持久化到 GeoConfig.CronSpecs
//
// ctx 由管理端 HTTP handler 透传请求 ctx（geo_config 是租户表，on 模式下读写
// 需要租户绑定）：调度注册与持久化都落在**本租户** —— 注册 (任务, 本租户) 的
// cron 条目、写本租户的 geo_config.cron_specs 行。off 模式/无绑定按默认租户
// （等价既有行为）。
func (m *JobManager) UpdateSchedule(ctx context.Context, name, spec string) error {
	def, ok := m.defByName(name)
	if !ok {
		return fmt.Errorf("未知任务: %s", name)
	}
	if err := m.validSpec(spec); err != nil {
		return err
	}
	tid := tenantIDFromCtx(ctx)

	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.setup {
		return errors.New("调度器未初始化，无法修改调度")
	}
	if err := m.registerLocked(name, spec, tid); err != nil {
		return err
	}
	m.specs[jobKey{name: name, tenant: tid}] = spec

	persisted := map[string]string{}
	if cfg, err := m.cfgRepo.Get(ctx); err == nil && strings.TrimSpace(cfg.CronSpecs) != "" {
		_ = json.Unmarshal([]byte(cfg.CronSpecs), &persisted)
	}
	persisted[name] = spec
	blob, _ := json.Marshal(persisted)

	cfg, err := m.cfgRepo.Get(ctx)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			logger.Warn(fmt.Sprintf("[GEO Jobs] 读取配置持久化调度失败: %v", err))
			return nil
		}
		cfg = &model.GeoConfig{Language: "zh"}
	}
	cfg.CronSpecs = string(blob)
	if err := m.cfgRepo.Update(ctx, cfg); err != nil {
		logger.Warn(fmt.Sprintf("[GEO Jobs] 持久化调度配置失败（本次会话内已生效，重启后回退默认）: %v", err))
	}
	logger.Info(fmt.Sprintf("[GEO Jobs] 任务 %s（租户 %d）调度已更新为 %q（默认 %q）", def.Name, tid, spec, def.DefaultSpec))
	return nil
}

// GeoJobInfo 管理端任务视图
type GeoJobInfo struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Spec        string           `json:"spec"`
	DefaultSpec string           `json:"default_spec"`
	Scheduled   bool             `json:"scheduled"`
	Running     bool             `json:"running"`
	LatestRun   *model.GeoJobRun `json:"latest_run"`
}

// ListJobs 全部任务 + 最新一次运行状态
func (m *JobManager) ListJobs(ctx context.Context) []GeoJobInfo {
	// 锁内只做快照（specs/running/setup），DB IO（runRepo.Latest）移到锁外，
	// 避免与 UpdateSchedule 的锁内持久化互相阻塞。
	// 视图按请求租户取：specs/running 都是 (任务, 租户) 维度；无绑定（off 模式
	// /裸 ctx）按默认租户。
	tid := tenantIDFromCtx(ctx)
	m.mu.RLock()
	out := make([]GeoJobInfo, 0, len(geoJobDefs))
	type pendingRun struct {
		idx  int
		name string
	}
	pending := make([]pendingRun, 0, len(geoJobDefs))
	for i, d := range geoJobDefs {
		spec := m.specs[jobKey{name: d.Name, tenant: tid}]
		if strings.TrimSpace(spec) == "" {
			spec = d.DefaultSpec // 该租户还没持久化过自己的 spec
		}
		info := GeoJobInfo{
			Name:        d.Name,
			Description: d.Description,
			Spec:        spec,
			DefaultSpec: d.DefaultSpec,
			Scheduled:   m.setup,
			Running:     m.runningState(jobKey{name: d.Name, tenant: tid}),
		}
		out = append(out, info)
		pending = append(pending, pendingRun{idx: i, name: d.Name})
	}
	m.mu.RUnlock()

	for _, p := range pending {
		if run, err := m.runRepo.Latest(ctx, p.name); err == nil {
			out[p.idx].LatestRun = run
		}
	}
	return out
}

// ListRuns 运行历史分页
func (m *JobManager) ListRuns(ctx context.Context, jobName string, page, limit int) ([]*model.GeoJobRun, int64, error) {
	if jobName != "" {
		if _, ok := m.defByName(jobName); !ok {
			return nil, 0, fmt.Errorf("未知任务: %s", jobName)
		}
	}
	return m.runRepo.List(ctx, jobName, page, limit)
}

// Trigger 手动触发入口（仅管理端：对全部活跃租户各跑一次）。
func (m *JobManager) Trigger(name string) (bool, error) {
	return m.StartJob(name, "manual")
}

// TriggerForTenant 当前请求租户手动触发。普通用户页面上的「刷新/立即执行」
// 必须走这里：调用 Trigger 会扇出到所有租户，等于用 A 的点击消耗 B 的额度
// 并写 B 的数据。运行体脱离请求取消，但保留租户绑定。
func (m *JobManager) TriggerForTenant(ctx context.Context, name string) (bool, error) {
	def, ok := m.defByName(name)
	if !ok {
		return false, fmt.Errorf("未知任务: %s", name)
	}
	tid, ok := tenant.FromContext(ctx)
	if !ok {
		if tenant.IsMultiTenant() {
			return false, errors.New("当前请求缺少租户上下文")
		}
		tid = tenant.DefaultTenantID
	}
	return m.startJobForTenant(def, tenant.WithTenantID(context.Background(), tid), "manual", tid), nil
}

func (m *JobManager) defByName(name string) (geoJobDef, bool) {
	for _, d := range geoJobDefs {
		if d.Name == name {
			return d, true
		}
	}
	return geoJobDef{}, false
}
