package service

import (
	"context"
	"encoding/json"
	"errors"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"strconv"
	"sync"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
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
	audience  *AudienceSelector
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
func InitSOPScheduler(db *gorm.DB, dispatcher any) *SOPScheduler {
	schedulerOnce.Do(func() {
		svc := InitSOPService(db, nil)
		globalSOPScheduler = NewSOPScheduler(svc, db, utils.LongTimeout)
		globalSOPScheduler.Start(context.Background())
	})
	return globalSOPScheduler
}

// NewSOPScheduler 构造调度器
func NewSOPScheduler(svc *SOPService, db *gorm.DB, interval time.Duration) *SOPScheduler {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	var agentRepo *repository.SopAgentRepository
	var execRepo *repository.SopExecutionRepository
	if db != nil {
		agentRepo = repository.NewSopAgentRepository(db)
		execRepo = repository.NewSopExecutionRepository(db)
	}
	return &SOPScheduler{
		svc:       svc,
		agentRepo: agentRepo,
		execRepo:  execRepo,
		audience:  NewAudienceSelectorWithDB(db),
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
	ctx, cancel := context.WithTimeout(ctx, utils.DefaultHTTPTimeout)
	defer cancel()

	s.cleanupStuckExecutions(ctx)

	s.dispatchAutoSOPs(ctx)

	s.dispatchScheduledSOPs(ctx)
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

	const maxRunningPerSOP = 50
	if count >= maxRunningPerSOP {
		logger.Warnf("[SOPScheduler] SOP %d 已在跑 %d 个，超过阈值 %d，跳过本轮调度", agent.ID, count, maxRunningPerSOP)
		return
	}

	customers, err := s.resolveTargets(ctx, agent)
	if err != nil {
		logger.Errorf("[SOPScheduler] SOP %d 取名单失败，本轮零开工: %v", agent.ID, err)
		return
	}
	// 阈值是在本轮开始时读的；圈选一轮最多可回 MaxAudienceLimit 人，所以这里按剩余额度截一次，
	// 否则 maxRunningPerSOP 只在"上一轮已经跑满"时才生效，一轮就能把并发从 0 顶到 500。
	if budget := maxRunningPerSOP - int(count); len(customers) > budget {
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
// 两条都没配时返回空名单。旧实现在这里回退到 agent.CreatedBy，而 CreatedBy 是**运营账号**，
// 于是"配置漏填"这个动作的后果是给配置者本人发一轮 SOP（T-P5-01 AC① 的坏例）。
func (s *SOPScheduler) resolveTargets(ctx context.Context, agent model.SOPAgent) ([]string, error) {
	if ids, _ := extractCustomerIDs(agent.TriggerConfig); len(ids) > 0 {
		return ids, nil
	}
	cfg, declared := parseAudienceConfig(agent.TriggerConfig)
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
	if confirmed, _ := agent.TriggerConfig[triggerKeyAudienceConfirm].(bool); !confirmed {
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
func (s *SOPScheduler) recordAudiencePreview(ctx context.Context, agent model.SOPAgent, sel *AudienceSelection) {
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
	out[triggerKeyAudiencePreview] = preview
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
		agent.ID, len(sel.CustomerIDs), triggerKeyAudienceConfirm)
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
