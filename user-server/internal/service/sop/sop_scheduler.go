package sop

import (
	"context"
	"encoding/json"
	"errors"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"strconv"
	"sync"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service/audience"
)

// SOPScheduler SOP 自动调度器
// 对应 SYSTEM_AUDIT_REPORT_20260715_V3
// 负责：
//  1. 周期扫描 trigger_type=auto/schedule 的 SOP
//  2. 匹配目标客户并启动执行
//  3. 处理超时/卡死的执行
type SOPScheduler struct {
	svc       *SOPService
	agentRepo *repository.SopAgentRepository
	execRepo  *repository.SopExecutionRepository
	audience  *audience.AudienceSelector
	interval  time.Duration
	stopCh    chan struct{}
	running   bool
	mu        sync.Mutex
}

var (
	globalSOPScheduler *SOPScheduler
	schedulerOnce      sync.Once
)

// GetSOPScheduler 获取全局调度器
func GetSOPScheduler() *SOPScheduler {
	return globalSOPScheduler
}

// SOPService 返回内部 SOP 服务实例，用于跨模块联动（如 意图→SOP）
func (s *SOPScheduler) SOPService(ctx context.Context) *SOPService {
	return s.svc
}

// InitSOPScheduler 初始化并启动调度器
func InitSOPScheduler(dispatcher any) *SOPScheduler {
	schedulerOnce.Do(func() {
		svc := InitSOPService(nil)
		globalSOPScheduler = NewSOPScheduler(svc, soPSchedulerScanInterval())
		globalSOPScheduler.Start(context.Background())
	})
	return globalSOPScheduler
}

// defaultSOPSchedulerScanInterval 调度器扫描周期的代码兜底值，与参数中心
// `misc.sop_scheduler_interval` 的 DefaultValue（60 秒）一致。
//
// 口径与 utils.LongTimeout 相同但**独立取一份**：LongTimeout 是"外部调用该等多久"，
// 调度周期是"多久看一眼有没有到点的 SOP"，两者碰巧都是 60s 但语义不同，
// 共用一个常量会让"改超时"意外改掉调度频率。
const defaultSOPSchedulerScanInterval = 60 * time.Second

// sopSchedulerScanIntervalProvider 由装配层注入；未注入时回落默认值。
var sopSchedulerScanIntervalProvider = func() time.Duration { return defaultSOPSchedulerScanInterval }

// SetSOPSchedulerScanIntervalProvider 注入读取函数（装配层调用；测试可注入桩）。nil 视为不注入。
func SetSOPSchedulerScanIntervalProvider(fn func() time.Duration) {
	if fn != nil {
		sopSchedulerScanIntervalProvider = fn
	}
}

// soPSchedulerScanInterval 取当前扫描周期（非正数一律回落默认值）。
func soPSchedulerScanInterval() time.Duration {
	if d := sopSchedulerScanIntervalProvider(); d > 0 {
		return d
	}
	return defaultSOPSchedulerScanInterval
}

// NewSOPScheduler 构造调度器
func NewSOPScheduler(svc *SOPService, interval time.Duration) *SOPScheduler {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	var agentRepo *repository.SopAgentRepository
	var execRepo *repository.SopExecutionRepository
	var audSel *audience.AudienceSelector
	if repository.DBReady() {
		agentRepo = repository.NewSopAgentRepositoryDefault()
		execRepo = repository.NewSopExecutionRepositoryDefault()
		audSel = audience.NewAudienceSelectorWithSources(
			repository.NewCustomerRFMRepository(),
			repository.NewCustomerTagAssignmentRepository(),
			repository.NewChurnScoreRepositoryDefault(),
		)
	}
	return &SOPScheduler{
		svc:       svc,
		agentRepo: agentRepo,
		execRepo:  execRepo,
		audience:  audSel,
		interval:  interval,
		stopCh:    make(chan struct{}),
	}
}

// Start 启动调度器
func (s *SOPScheduler) Start(ctx context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	go s.loop(context.Background())
	logger.Infof("[SOPScheduler] 启动成功，调度间隔 %s", s.interval)
}

// Stop 停止调度器
func (s *SOPScheduler) Stop(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	s.running = false
	close(s.stopCh)
}

func (s *SOPScheduler) loop(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	s.tick(ctx)

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *SOPScheduler) tick(ctx context.Context) {
	if s.agentRepo == nil || s.execRepo == nil {
		return
	}
	// 模块闸（FR-5.6）：sop_scheduler 在 workerModuleKey 里**未映射**
	// （归属横跨 customer/inbox/ai-sales，见该表的 NOT mapped 清单），
	// 当前恒放行 —— 挂在这里的价值是未来要闸只需在表里加一行。
	if !allowWorker(context.Background(), "sop_scheduler") {
		logger.Info("[SOPScheduler] SOP 相关模块无任何租户开通，跳过本轮扫描")
		return
	}
	// 超时预算：**每租户一份 30s**，不是整个 Fanout 共享一份（二审 P2-5）。
	// `WithTimeout(DefaultHTTPTimeout)` 包在扇出外面 ⇒ 12 个租户共享 30s：
	// 先跑的租户耗光预算后，后面的租户一进回调就被 ctx.Done() 打断（扫到的人
	// 越多饿死越严重）。外层只保留可取消语义（Stop/退出时整体收束），超时移进
	// 每租户回调内。
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// 后台任务租户扇出（specs/001-saas-multi-tenant T020）。
	//
	// 为什么必须走 Fanout 而不是 system scope：tick 的三个子调用最终会落到
	// tryExecute→svc.Execute —— 这是**跨模块执行路径**，会真正为客户创建/写入
	// SOP 执行记录。system scope（tenant.WithSystemScope）虽能绕过 fail-closed 放行
	// 查询，但 fillCreateForTenant 对 system scope 直接放行给列默认值 1，于是本该
	// 归属租户 N 的执行会被串写到默认租户（写错租户）。Fanout 则在 on 模式下逐活跃
	// 租户注入 ctx（Query 加 WHERE tenant_id=N、Create 填 N），off 模式退化为单默认
	// 租户（等价既有行为）—— 与 churn_score_job / session_ttl_cron 同一形态。
	//
	// 上面的 AllowWorker 闸是**模块级、租户无关**判定，保持在扇出之外不随租户变。
	if _, err := tenant.Fanout(ctx, nil, func(ctx context.Context, _ uint) (int64, error) {
		ctx, cancel := context.WithTimeout(ctx, utils.DefaultHTTPTimeout)
		defer cancel()
		s.cleanupStuckExecutions(ctx)
		s.dispatchAutoSOPs(ctx)
		s.dispatchScheduledSOPs(ctx)
		return 0, nil
	}); err != nil {
		logger.Errorf("[SOPScheduler] 租户扇出执行失败: %v", err)
	}
}

func (s *SOPScheduler) cleanupStuckExecutions(ctx context.Context) {
	if s.execRepo == nil {
		return
	}
	threshold := time.Now().Add(-24 * time.Hour)
	rowsAffected, err := s.execRepo.CleanupStuck(ctx, threshold, SOPStatusRunning, SOPStatusFailed)
	if err != nil {
		logger.Errorf("[SOPScheduler] 清理超时执行失败: %v", err)
	} else if rowsAffected > 0 {
		logger.Infof("[SOPScheduler] 清理超时执行 %d 条", rowsAffected)
	}
}

func (s *SOPScheduler) dispatchAutoSOPs(ctx context.Context) {
	if s.agentRepo == nil {
		return
	}
	list, err := s.agentRepo.ListActiveByTriggerType(ctx, SOPTriggerAuto)
	if err != nil {
		logger.Errorf("[SOPScheduler] 查询 auto SOP 失败: %v", err)
		return
	}
	for _, agent := range list {
		s.tryExecute(ctx, agent)
	}
}

func (s *SOPScheduler) dispatchScheduledSOPs(ctx context.Context) {
	if s.agentRepo == nil {
		return
	}
	list, err := s.agentRepo.ListActiveByTriggerType(ctx, SOPTriggerSchedule)
	if err != nil {
		logger.Errorf("[SOPScheduler] 查询 schedule SOP 失败: %v", err)
		return
	}
	now := time.Now()
	for _, agent := range list {
		cfg := agent.TriggerConfig
		intervalMin, _ := cfg["interval_minutes"].(float64)
		if intervalMin <= 0 {
			intervalMin = 60
		}
		var lastRunAt *time.Time
		if v, ok := cfg["last_run_at"].(string); ok {
			t, err := time.Parse(time.RFC3339, v)
			if err == nil {
				lastRunAt = &t
			}
		}
		if lastRunAt != nil && now.Sub(*lastRunAt) < time.Duration(intervalMin)*time.Minute {
			continue
		}
		s.tryExecute(ctx, agent)
		updated := setJSONMapValue(agent.TriggerConfig, "last_run_at", now.UTC().Format(time.RFC3339))
		_ = s.agentRepo.UpdateTriggerConfig(ctx, agent.ID, updated)
	}
}

func (s *SOPScheduler) tryExecute(ctx context.Context, agent model.SOPAgent) {
	if s.execRepo == nil {
		return
	}
	count, err := s.execRepo.CountBySOPIDAndStatus(ctx, agent.ID, SOPStatusRunning)
	if err != nil {
		logger.Errorf("[SOPScheduler] 检查运行中执行失败: %v", err)
		return
	}

	// 上限在本轮开始时读一次，下面按剩余额度截名单，两处必须用同一个值，
	// 否则会出现"判了没超限却把名单截短"或"截了名单却仍按满额开工"的错位。
	runningCap := maxRunningPerSOP()
	if int64(runningCap) <= count {
		logger.Warnf("[SOPScheduler] SOP %d 已在跑 %d 个，超过阈值 %d，跳过本轮调度", agent.ID, count, runningCap)
		return
	}

	customers, err := s.resolveTargets(ctx, agent)
	if err != nil {
		logger.Errorf("[SOPScheduler] SOP %d 取名单失败，本轮零开工: %v", agent.ID, err)
		return
	}
	// 阈值是在本轮开始时读的；圈选一轮最多可回 MaxAudienceLimit 人，所以这里按剩余额度截一次，
	// 否则 maxRunningPerSOP 只在"上一轮已经跑满"时才生效，一轮就能把并发从 0 顶到 500。
	if budget := runningCap - int(count); len(customers) > budget {
		logger.Warnf("[SOPScheduler] SOP %d 名单 %d 人超在本轮剩余额度 %d，只开工前 %d 人", agent.ID, len(customers), budget, budget)
		customers = customers[:budget]
	}

	for _, cid := range customers {

		if running, derr := s.execRepo.CountRunningBySOPAndCustomer(ctx, agent.ID, cid, SOPStatusRunning); derr == nil && running > 0 {
			logger.Infof("[SOPScheduler] SOP %d 客户 %s 已有 %d 个运行中执行，跳过本轮", agent.ID, cid, running)
			continue
		}
		req := &dto.ExecuteRequest{
			SOPID:      agent.ID,
			CustomerID: cid,
			SessionID:  "scheduler-" + fmtUintSafe(agent.ID),
			Input: map[string]any{
				"_trigger": "scheduler",
			},
		}
		exec, err := s.svc.Execute(ctx, req)
		if err != nil {
			logger.Errorf("[SOPScheduler] 启动 SOP %d 失败: %v", agent.ID, err)
			continue
		}
		logger.Infof("[SOPScheduler] 启动 SOP %d -> ExecutionID=%d Customer=%s", agent.ID, exec.ID, cid)
	}
}

// resolveTargets 决定本轮开工的名单。两条通道，互不回落：
//
//	静态通道 customer_ids   —— 手填名单，行为与 T-P5-01 之前逐字节一致。
//	圈选通道 audience       —— 按条件实时取；未经 audience_confirmed 只做预览、零开工。
//
// 两条都没配时返回空名单。若在这里回退到 agent.CreatedBy，而 CreatedBy 是**运营账号**，
// 于是"配置漏填"这个动作的后果是给配置者本人发一轮 SOP（T-P5-01 AC① 的坏例）。
func (s *SOPScheduler) resolveTargets(ctx context.Context, agent model.SOPAgent) ([]string, error) {
	if ids, _ := extractCustomerIDs(agent.TriggerConfig); len(ids) > 0 {
		return ids, nil
	}
	cfg, declared := audience.ParseTriggerConfig(agent.TriggerConfig)
	if !declared {
		logger.Warnf("[SOPScheduler] SOP %d 既无静态名单也无圈选条件，本轮零开工（不再回退创建者 %d）", agent.ID, agent.CreatedBy)
		return nil, nil
	}
	if s.audience == nil {
		return nil, errors.New("圈选源未装配（调度器无 DB）")
	}
	sel, err := s.audience.Select(ctx, cfg)
	if err != nil {
		return nil, err
	}
	for _, reason := range sel.Reasons {
		logger.Warnf("[SOPScheduler] SOP %d 圈选条件空手: %s", agent.ID, reason)
	}
	if confirmed, _ := agent.TriggerConfig[audience.TriggerKeyAudienceConfirm].(bool); !confirmed {
		s.recordAudiencePreview(ctx, agent, sel)
		return nil, nil
	}
	if sel.Truncated {
		logger.Warnf("[SOPScheduler] SOP %d 圈选在上限处被截断，实际命中更多人", agent.ID)
	}
	return sel.CustomerIDs, nil
}

// recordAudiencePreview 把这一轮的名单写回 trigger_config.audience_preview 交人工过目。
// RK-6：首轮不外发。确认位由人在 SOP 更新接口里置 audience_confirmed=true，
// 调度器**从不**替它写这个键（预览轮只加 preview 键，保留其余内容原样回写）。
func (s *SOPScheduler) recordAudiencePreview(ctx context.Context, agent model.SOPAgent, sel *audience.AudienceSelection) {
	if s.agentRepo == nil {
		return
	}
	preview := map[string]any{
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"count":        len(sel.CustomerIDs),
		"customer_ids": sel.CustomerIDs,
		"truncated":    sel.Truncated,
		"reasons":      sel.Reasons,
	}
	out := agent.TriggerConfig
	if out == nil {
		out = model.JSONMap{}
	}
	// 就地改这份 map：dispatchScheduledSOPs 稍后会把同一个 map 整体回写（带 last_run_at），
	// 只写库不改这里的话，schedule 型的预览会在同一次 tick 里被覆盖掉。
	out[audience.TriggerKeyAudiencePreview] = preview
	// 不用 setJSONMapValue：那个 helper 只能塞字符串值，预览是对象，塞进去会变 "{}"。
	b, err := json.Marshal(out)
	if err != nil {
		logger.Errorf("[SOPScheduler] SOP %d 预览序列化失败: %v", agent.ID, err)
		return
	}
	if err := s.agentRepo.UpdateTriggerConfig(ctx, agent.ID, string(b)); err != nil {
		logger.Errorf("[SOPScheduler] SOP %d 预览写回失败: %v", agent.ID, err)
		return
	}
	logger.Infof("[SOPScheduler] SOP %d 首轮圈选只出预览：%d 人；人工在 trigger_config 置 %s=true 后才会开工",
		agent.ID, len(sel.CustomerIDs), audience.TriggerKeyAudienceConfirm)
}

func extractCustomerIDs(cfg model.JSONMap) ([]string, error) {
	if cfg == nil {
		return nil, nil
	}
	raw, ok := cfg["customer_ids"]
	if !ok {
		return nil, nil
	}
	switch v := raw.(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out, nil
	}
	return nil, nil
}

func setJSONMapValue(m model.JSONMap, key, value string) string {
	if m == nil {
		m = model.JSONMap{}
	}
	m[key] = value
	b, _ := json.Marshal(m)
	return string(b)
}

func fmtUintSafe(v uint) string {
	return strconv.FormatUint(uint64(v), 10)
}

// DefaultMaxRunningPerSOP 单个 SOP 模板同时运行的最大实例数的代码兜底
// （参数中心 workflow.max_running_per_sop 未配置时用）。
const DefaultMaxRunningPerSOP = 50

var maxRunningPerSOPProvider func() int

// SetMaxRunningPerSOPProvider 注入单 SOP 最大并发；传 nil 视为不注入。
func SetMaxRunningPerSOPProvider(fn func() int) { maxRunningPerSOPProvider = fn }

// maxRunningPerSOP 生效的单 SOP 最大并发。非正值一律回落兜底——0 会让
// count>=0 恒成立，调度器对所有 SOP 永久停摆。
func maxRunningPerSOP() int {
	if p := maxRunningPerSOPProvider; p != nil {
		if d := p(); d > 0 {
			return d
		}
	}
	return DefaultMaxRunningPerSOP
}

// ProbeMaxRunningPerSOP 导出当前生效的单 SOP 最大并发（仅供装配层测试断言读取口）。
func ProbeMaxRunningPerSOP() int { return maxRunningPerSOP() }

// ProbeSchedulerScanInterval 读当前扫描周期（跨包用例的读入口）。
func ProbeSchedulerScanInterval() time.Duration { return soPSchedulerScanInterval() }

// SwapSchedulerScanIntervalProvider 替换 provider 并返回恢复函数（用例 save/restore 用）。
func SwapSchedulerScanIntervalProvider(fn func() time.Duration) func() {
	if fn == nil {
		return func() {}
	}
	old := sopSchedulerScanIntervalProvider
	sopSchedulerScanIntervalProvider = fn
	return func() { sopSchedulerScanIntervalProvider = old }
}
