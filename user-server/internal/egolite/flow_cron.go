package egolite

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/model"
	pkgcron "hivemtk-user/internal/pkg/cron"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"

	cronv3 "github.com/robfig/cron/v3"
)

// EgoFlowCronService 流程定时触发器（specs/016 S10；调度模式照抄
// browser_automation/service/cron.go，物理独立——不许跨域 import，cron 工具函数
// 在本包内同源副本，漂移由 ValidateCronExpr 向量测试钉死）。
//
// 纪律（与 browser cron 同源 + ego 腿特有）：
//   - 只允许挂**纯读流程**（writes 为空）——决策点 6：ego 腿全自动写长期受限，
//     无人值守写流程会在 waiting_confirm 卡到 confirm TTL 判停（fail-close 正确但无业务价值）；
//   - 触发前置 runner 在线（离线跳过不落行——避免 created 态堆积）；
//   - Enabled 显式 false 不被 GORM 零值陷阱翻转（缺省语义在 Upsert 入参指针上）。

// EgoFlowCronFire 定时触发的执行出口（router 装配层注入：EnsureOnline + flow 复查 +
// execSvc.Start）。解耦理由与 tooluse.SetBrowserTaskRunner 同款——service 不反向依赖装配面。
type EgoFlowCronFire func(ctx context.Context, userID, flowID uint) error

type EgoFlowCronService struct {
	cronRepo EgoFlowCronRepository
	flowRepo EgoFlowRepository
	fire     EgoFlowCronFire

	mu       sync.Mutex
	entryIDs map[uint]cronv3.EntryID
}

func NewEgoFlowCronService(cronRepo EgoFlowCronRepository, flowRepo EgoFlowRepository, fire EgoFlowCronFire) *EgoFlowCronService {
	return &EgoFlowCronService{cronRepo: cronRepo, flowRepo: flowRepo, fire: fire, entryIDs: make(map[uint]cronv3.EntryID)}
}

// UpsertRequest 挂载/更新请求（enabled 为 nil ⇒ 默认 true——Create 语义；显式 false 原样落库）
type UpsertCronRequest struct {
	CronExpr string
	TimeZone string
	Enabled  *bool
}

// Upsert 挂载/更新触发器（flow 归属校验 + 纯读闸 + 表达式预检 → applySchedule 唯一出口）
func (s *EgoFlowCronService) Upsert(ctx context.Context, userID, flowID uint, req UpsertCronRequest) (*model.EgoliteFlowCron, error) {
	f, err := s.flowRepo.GetForUser(ctx, flowID, userID)
	if err != nil {
		return nil, fmt.Errorf("流程不存在或不属于你")
	}
	var writes []string
	if err := json.Unmarshal(f.WritesJSON, &writes); err == nil && len(writes) > 0 {
		return nil, fmt.Errorf("声明了写动作的流程不能挂定时触发器（无人值守写会被 confirm 闸判停；决策点 6）")
	}
	if err := ValidateCronExpr(req.CronExpr, req.TimeZone); err != nil {
		return nil, err
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	tr := &model.EgoliteFlowCron{
		FlowID: flowID, UserID: userID,
		CronExpr: strings.TrimSpace(req.CronExpr), TimeZone: strings.TrimSpace(req.TimeZone),
		Enabled: enabled,
	}
	s.unregister(flowID)
	final, err := s.cronRepo.UpsertByFlow(ctx, tr)
	if err != nil {
		return nil, err
	}
	s.applySchedule(ctx, final)
	return final, nil
}

// Delete 摘除触发器（注销调度 + 删行）
func (s *EgoFlowCronService) Delete(ctx context.Context, userID, flowID uint) error {
	s.unregister(flowID)
	return s.cronRepo.DeleteByFlow(ctx, flowID, userID)
}

// Get 查询单条（用户面）
func (s *EgoFlowCronService) Get(ctx context.Context, userID, flowID uint) (*model.EgoliteFlowCron, error) {
	tr, err := s.cronRepo.GetByFlow(ctx, flowID)
	if err != nil {
		return nil, err
	}
	if tr.UserID != userID {
		// 同响应防枚举（域内归属校验同纪律）
		return nil, ErrFlowCronNotFound
	}
	return tr, nil
}

// applySchedule 触发器调度状态的唯一出口：按 enabled 注册/注销，并把 next_run_at
// 同步成「按当前表达式算出的下一次」（停用=NULL）。Upsert 与 RestoreAll 都走这里。
func (s *EgoFlowCronService) applySchedule(ctx context.Context, tr *model.EgoliteFlowCron) {
	s.unregister(tr.FlowID)
	if !tr.Enabled {
		s.storeTimes(ctx, tr.ID, nil, nil)
		return
	}
	s.register(tr)
	s.storeTimes(ctx, tr.ID, nextRunAt(tr.CronExpr, tr.TimeZone, time.Now()), nil)
}

// triggerOnce 一次触发（调度回调的全部副作用都收敛在这里）。
//
// 无请求 ctx ⇒ 先 system scope 复读触发器行（避开 enabled/Delete 的竞态窗口），
// 再钉回归属租户做业务段。
//
// 「单次触发，不重投」是刻意的：失败（并发闸满 / runner 离线 / 流程被改红）只记日志、
// 记账照常推进到下一次——补投属于另一套语义（错过的点是否该补、补几次、补到哪一刻），
// 需要单独立 spec；本函数绝不在失败路径上再调一次 fire。
func (s *EgoFlowCronService) triggerOnce(flowID uint) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cur, err := s.cronRepo.GetByFlow(tenant.WithSystemScope(ctx), flowID)
	if err != nil || cur == nil || !cur.Enabled {
		return
	}
	ctx = tenant.OnlyTenant(ctx, cur.TenantID)
	now := time.Now()
	// last=本次触发时刻，next=本次之后的下一次
	_ = s.cronRepo.UpdateTimes(ctx, cur.ID, nextRunAt(cur.CronExpr, cur.TimeZone, now), &now)
	if s.fire == nil {
		logger.Warnf("[EgoFlowCron] flow=%d 触发跳过: 未注入执行出口（装配缺失）", cur.FlowID)
		return
	}
	if err := s.fire(ctx, cur.UserID, cur.FlowID); err != nil {
		// 单次触发跳过，调度器继续（不 panic 不重投）；下次触发按表达式照常到来
		logger.Warnf("[EgoFlowCron] flow=%d 触发跳过: %v", cur.FlowID, err)
	}
}

// register 注册到项目 TaskManager（秒级 spec），触发时以归属用户身份 fire。
func (s *EgoFlowCronService) register(tr *model.EgoliteFlowCron) {
	flowID := tr.FlowID
	mgr := pkgcron.GetTaskManager()
	entry, err := mgr.AddTask(toSixField(tr.CronExpr, tr.TimeZone), func() {
		s.triggerOnce(flowID)
	})
	s.mu.Lock()
	if err == nil {
		s.entryIDs[flowID] = entry
	}
	s.mu.Unlock()
	if err != nil {
		logger.Warnf("[EgoFlowCron] 注册失败 flow=%d expr=%s: %v", flowID, tr.CronExpr, err)
	}
}

func (s *EgoFlowCronService) unregister(flowID uint) {
	s.mu.Lock()
	entry, ok := s.entryIDs[flowID]
	if ok {
		delete(s.entryIDs, flowID)
	}
	s.mu.Unlock()
	if ok {
		pkgcron.GetTaskManager().RemoveTask(entry)
	}
}

// RestoreAll 进程启动后恢复全部 enabled 触发器（browser cron 同款：平台级启动恢复扫
// **全租户**行，必须走 system scope 豁免——TENANT_MODE=on 时裸 Background 会被
// 隔离 callback fail-closed；记账段 UpdateTimes 同在此 scope 内）
func (s *EgoFlowCronService) RestoreAll(ctx context.Context) {
	ctx = tenant.WithSystemScope(ctx)
	list, err := s.cronRepo.ListEnabled(ctx)
	if err != nil {
		logger.Warnf("[EgoFlowCron] 恢复失败: %v", err)
		return
	}
	for _, tr := range list {
		s.applySchedule(ctx, tr)
	}
}

func (s *EgoFlowCronService) storeTimes(ctx context.Context, id uint, next *time.Time, last *time.Time) {
	// 记账失败不影响调度结论（browser cron storeNextRun 同纪律）
	_ = s.cronRepo.UpdateTimes(ctx, id, next, last)
}

// ---------- cron 工具函数（browser_automation/service/cron.go 同源副本；禁跨域 import） ----------

// toSixField 5 段 cron → 项目 TaskManager 的 6 段秒级；
// timeZone 非空时前缀 CRON_TZ=（robfig v3 原生支持，G20：调度不依赖服务器本地时区）。
func toSixField(expr, timeZone string) string {
	expr = strings.TrimSpace(expr)
	fields := strings.Fields(expr)
	if len(fields) == 5 {
		expr = "0 " + expr
	}
	if tz := strings.TrimSpace(timeZone); tz != "" {
		return "CRON_TZ=" + tz + " " + expr
	}
	return expr
}

// cronParser 6 段秒级解析器（与 TaskManager 配置一致，用于提前校验）
var cronParser = cronv3.NewParser(cronv3.Second | cronv3.Minute | cronv3.Hour | cronv3.Dom | cronv3.Month | cronv3.Dow | cronv3.Descriptor)

// ValidateCronExpr 校验 5 段 cron 表达式 + 时区合法性（timeZone 空=服务器本地）
func ValidateCronExpr(expr, timeZone string) error {
	tz := strings.TrimSpace(timeZone)
	if tz != "" {
		if _, err := time.LoadLocation(tz); err != nil {
			return fmt.Errorf("时区无效（需 IANA 名，如 Asia/Shanghai）: %w", err)
		}
	}
	if _, err := cronParser.Parse(toSixField(expr, tz)); err != nil {
		return fmt.Errorf("cron 表达式无效（需 5 段「分 时 日 月 周」，如 */5 * * * *）: %w", err)
	}
	return nil
}

// nextRunAt from 之后的第一个触发时刻（与注册 spec 同源解析，展示值不与实际触发时刻分家）
func nextRunAt(expr, timeZone string, from time.Time) *time.Time {
	sched, err := cronParser.Parse(toSixField(expr, strings.TrimSpace(timeZone)))
	if err != nil {
		return nil
	}
	next := sched.Next(from)
	return &next
}
