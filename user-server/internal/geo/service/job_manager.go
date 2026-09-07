package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
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

	geoJobRunRetention = 30 * 24 * time.Hour
)

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
}

var specParser = cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// JobManager GEO 定时任务统一管理：
//  1. 同任务互斥防重入（上一轮未跑完则跳过本轮）
//  2. panic 恢复 + 超时控制
//  3. 每次运行落 geo_job_runs 历史（管理端可查）
//  4. cron 表达式可经管理端 API 调整并持久化到 GeoConfig.CronSpecs
type JobManager struct {
	mu      sync.RWMutex
	sched   JobScheduler
	runRepo repository.GeoJobRunRepository
	cfgRepo repository.GeoConfigRepository

	running map[string]*atomic.Bool
	specs   map[string]string
	entryID map[string]cron.EntryID
	setup   bool
}

var (
	jobManagerOnce sync.Once
	jobManager     *JobManager
)

// GetGeoJobManager 进程级单例。未 Setup 前也可手动触发任务（无调度注册）。
func GetGeoJobManager() *JobManager {
	jobManagerOnce.Do(func() {
		running := make(map[string]*atomic.Bool, len(geoJobDefs))
		specs := make(map[string]string, len(geoJobDefs))
		for _, d := range geoJobDefs {
			running[d.Name] = &atomic.Bool{}
			specs[d.Name] = d.DefaultSpec
		}
		jobManager = &JobManager{
			runRepo: repository.NewGeoJobRunRepository(),
			cfgRepo: repository.NewGeoConfigRepository(),
			running: running,
			specs:   specs,
			entryID: map[string]cron.EntryID{},
		}
	})
	return jobManager
}

// SetupGeoJobs 由 cron.InitCron 调用：清理僵尸 running 记录、
// 读取持久化调度配置并注册全部 GEO 任务。
func SetupGeoJobs(sched JobScheduler) {
	m := GetGeoJobManager()
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.runRepo.FailStaleRunning(context.Background()); err != nil {
		logger.Warn(fmt.Sprintf("[GEO Jobs] 清理僵尸运行记录失败: %v", err))
	}

	m.sched = sched
	m.setup = true

	persisted := m.loadPersistedSpecs()
	for _, d := range geoJobDefs {
		spec := d.DefaultSpec
		if s, ok := persisted[d.Name]; ok && m.validSpec(s) == nil {
			spec = s
		}
		m.specs[d.Name] = spec
		if err := m.registerLocked(d.Name, spec); err != nil {
			logger.Error(err, fmt.Sprintf("[GEO Jobs] 注册任务 %s 失败", d.Name))
		}
	}
	logger.Info("[GEO Jobs] 定时任务已注册：sov_refresh/negative_monitor/source_sync/crawler_monitor（调度可经 /geo/jobs API 调整）")
}

func (m *JobManager) loadPersistedSpecs() map[string]string {
	cfg, err := m.cfgRepo.Get()
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

func (m *JobManager) registerLocked(name, spec string) error {
	if m.sched == nil {
		return errors.New("调度器未初始化")
	}
	if err := m.validSpec(spec); err != nil {
		return err
	}
	if old, ok := m.entryID[name]; ok {
		m.sched.RemoveTask(old)
		delete(m.entryID, name)
	}
	id, err := m.sched.AddTask(spec, func() {
		m.StartJob(name, "cron")
	})
	if err != nil {
		return fmt.Errorf("注册任务 %s 失败: %w", name, err)
	}
	m.entryID[name] = id
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

// StartJob 异步触发一次任务运行。返回 (false, nil) 表示同任务正在运行被跳过。
func (m *JobManager) StartJob(name, trigger string) (bool, error) {
	def, ok := m.defByName(name)
	if !ok {
		return false, fmt.Errorf("未知任务: %s", name)
	}
	if !m.running[name].CompareAndSwap(false, true) {
		logger.Info(fmt.Sprintf("[GEO Jobs] 任务 %s 上一轮仍在运行，跳过本次触发（trigger=%s）", name, trigger))
		return false, nil
	}

	run := &model.GeoJobRun{
		JobName:   name,
		Trigger:   trigger,
		Status:    "running",
		StartedAt: time.Now(),
	}
	var runID uint
	if err := m.runRepo.Create(context.Background(), run); err != nil {
		logger.Error(err, fmt.Sprintf("[GEO Jobs] 任务 %s 写入运行记录失败（本次不记录历史）", name))
	} else {
		runID = run.ID
	}

	go m.execute(def, runID, trigger)
	return true, nil
}

func (m *JobManager) execute(def geoJobDef, runID uint, trigger string) {
	defer m.running[def.Name].Store(false)

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), def.Timeout)
	defer cancel()

	status, summary, errMsg := "success", "", ""
	func() {
		defer func() {
			if r := recover(); r != nil {
				status = "failed"
				errMsg = fmt.Sprintf("panic: %v", r)
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
		if err := m.runRepo.Finish(context.Background(), runID, status, runeTruncate(summary, 2000), runeTruncate(errMsg, 2000), duration); err != nil {
			logger.Error(err, fmt.Sprintf("[GEO Jobs] 任务 %s 回写运行记录失败", def.Name))
		}
	}
	logger.Info(fmt.Sprintf("[GEO Jobs] 任务 %s 完成（trigger=%s status=%s 用时=%dms）%s",
		def.Name, trigger, status, duration, summary))

	if err := m.runRepo.DeleteBefore(context.Background(), time.Now().Add(-geoJobRunRetention)); err != nil {
		logger.Warn(fmt.Sprintf("[GEO Jobs] 清理过期运行记录失败: %v", err))
	}
}

// UpdateSchedule 调整任务 cron 表达式并持久化到 GeoConfig.CronSpecs
func (m *JobManager) UpdateSchedule(name, spec string) error {
	def, ok := m.defByName(name)
	if !ok {
		return fmt.Errorf("未知任务: %s", name)
	}
	if err := m.validSpec(spec); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.setup {
		return errors.New("调度器未初始化，无法修改调度")
	}
	if err := m.registerLocked(name, spec); err != nil {
		return err
	}
	m.specs[name] = spec

	persisted := map[string]string{}
	if cfg, err := m.cfgRepo.Get(); err == nil && strings.TrimSpace(cfg.CronSpecs) != "" {
		_ = json.Unmarshal([]byte(cfg.CronSpecs), &persisted)
	}
	persisted[name] = spec
	blob, _ := json.Marshal(persisted)

	cfg, err := m.cfgRepo.Get()
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			logger.Warn(fmt.Sprintf("[GEO Jobs] 读取配置持久化调度失败: %v", err))
			return nil
		}
		cfg = &model.GeoConfig{Language: "zh"}
	}
	cfg.CronSpecs = string(blob)
	if err := m.cfgRepo.Update(cfg); err != nil {
		logger.Warn(fmt.Sprintf("[GEO Jobs] 持久化调度配置失败（本次会话内已生效，重启后回退默认）: %v", err))
	}
	logger.Info(fmt.Sprintf("[GEO Jobs] 任务 %s 调度已更新为 %q（默认 %q）", def.Name, spec, def.DefaultSpec))
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
	// 避免与 UpdateSchedule 的锁内持久化互相阻塞
	m.mu.RLock()
	out := make([]GeoJobInfo, 0, len(geoJobDefs))
	type pendingRun struct {
		idx  int
		name string
	}
	pending := make([]pendingRun, 0, len(geoJobDefs))
	for i, d := range geoJobDefs {
		info := GeoJobInfo{
			Name:        d.Name,
			Description: d.Description,
			Spec:        m.specs[d.Name],
			DefaultSpec: d.DefaultSpec,
			Scheduled:   m.setup,
			Running:     m.running[d.Name].Load(),
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

// Trigger 手动触发入口（controller 用）
func (m *JobManager) Trigger(name string) (bool, error) {
	return m.StartJob(name, "manual")
}

func (m *JobManager) defByName(name string) (geoJobDef, bool) {
	for _, d := range geoJobDefs {
		if d.Name == name {
			return d, true
		}
	}
	return geoJobDef{}, false
}
