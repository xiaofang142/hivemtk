// human_task_sla_job.go 转人工待办的 SLA 超时闭环（I2）。
//
// 背景：human_tasks 三类的 SLA 时刻在开放那一刻就写进了各自的列
// （会话判首响 sla_first_response_at、审批判裁决 sla_decide_at、催收判升级 sla_escalate_at），
// CountOverdueOpenByKind 也能把逾期数算出来 —— 但在此之前，全仓没有**任何**按节拍
// 扫这批逾期行并处置它们的东西：逾期只被"数"出来，没有被"办"。
// 于是值班看板上那个逾期数字不会自己变小，除非有人恰好去点开那条待办。
//
// 本文件补的就是"办"的那一半，三态开关见 app/human_task_sla_wiring.go（FF_LTC_HANDOFF_SLA_JOB）：
//
//	off（默认）  不装配：与交付前逐字节一致。
//	shadow      只扫只数：把"这一轮有几条逾期、几条该升级"算出来打日志，**一条不写**。
//	            这一档回答的问题是"逾期判定那段 SQL 在真表上算对了没有"。
//	enforce     真写：逾期即给责任人发站内提醒；逾期时间超过 SLA 时限本身（即已拖过一个
//	            完整的 SLA 窗口）再向管理员广播一条升级。**不新增 human_task kind**
//	            （值域三类是硬约束），逾期这个事实只体现在站内通知里。
//
// 为什么升级不新开一类待办：human_tasks 的值域被 uq_human_task_open 的部分唯一索引
// 与三类 SLA 列一起钉死，多插一个 kind 会让 CountOpenByKind / 待办中心的每处 switch
// 都得跟着长，而"逾期"本是**既有待办的一个属性**，不是一件新的待办事项。
package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

const (
	// DefaultHumanTaskSLAInterval SLA 扫描节拍。取 5 分钟对齐"待办中心给人看"这件事：
	// 会话首响 SLA 以分钟计，5 分钟的扫描延迟对"提醒"够用，又不至于让每轮都空转。
	DefaultHumanTaskSLAInterval = 5 * time.Minute

	// DefaultHumanTaskSLAWindow 同一待办的提醒去重窗口。必须显著大于扫描节拍：
	// 窗口若等于节拍，两次相邻扫描的边界抖动就会漏进一条重复提醒。
	DefaultHumanTaskSLAWindow = 30 * time.Minute

	// humanTaskSLABatchLimit 单轮扫描上限。必须有界：一次读的最坏耗时不能随表增长
	// （口径同审批清扫的 approvalSweepBatchLimit）。
	humanTaskSLABatchLimit = 200

	// humanTaskSLAEscalateFactor 升级倍率。逾期时长超过"原始 SLA 时长"这么多倍即升级。
	// 取 2 的含义：待办已经拖了整整一个 SLA 窗口还没动，才值得惊动管理员。
	humanTaskSLAEscalateFactor = 2

	// HumanTaskOverdueNotifyType 逾期提醒通知的类型（同时是去重键的一半）。
	HumanTaskOverdueNotifyType = "human_task_overdue"
	// HumanTaskOverdueEscalateNotifyType 逾期升级通知的类型。
	HumanTaskOverdueEscalateNotifyType = "human_task_overdue_escalation"
)

// HumanTaskSLAMode 扫描 worker 的档位（off 时根本不会构造出 worker，这里仍留常量，
// 让 Start 能对"被误构造的 off 实例"判 no-op）。
type HumanTaskSLAMode string

const (
	HumanTaskSLAModeOff     HumanTaskSLAMode = "off"
	HumanTaskSLAModeShadow  HumanTaskSLAMode = "shadow"
	HumanTaskSLAModeEnforce HumanTaskSLAMode = "enforce"
)

// HumanTaskSLAReport 一轮扫描的结果。
type HumanTaskSLAReport struct {
	At        time.Time `json:"at"`
	Overdue   int       `json:"overdue"`
	Reminded  int       `json:"reminded"`
	Escalated int       `json:"escalated"`
	Err       string    `json:"err,omitempty"`
}

// HumanTaskSLAWorker 待办逾期的节拍器。
type HumanTaskSLAWorker struct {
	svc      *HumanTaskService
	notifier *NotificationService
	mode     HumanTaskSLAMode
	interval time.Duration
	window   time.Duration

	startOnce sync.Once
	stop      chan struct{}
	wg        sync.WaitGroup

	mu           sync.RWMutex
	started      bool
	rounds       int64
	overdueAll   int64
	remindedAll  int64
	escalatedAll int64
}

// NewHumanTaskSLAWorker 构造。interval/window <= 0 走默认值，两处不各写一份默认。
// notifier 允许为 nil（shadow 档不需要它，enforce 档缺它只打日志不写）。
func NewHumanTaskSLAWorker(svc *HumanTaskService, notifier *NotificationService, mode HumanTaskSLAMode, interval, window time.Duration) *HumanTaskSLAWorker {
	if interval <= 0 {
		interval = DefaultHumanTaskSLAInterval
	}
	if window <= 0 {
		window = DefaultHumanTaskSLAWindow
	}
	return &HumanTaskSLAWorker{
		svc: svc, notifier: notifier, mode: mode,
		interval: interval, window: window,
		stop: make(chan struct{}),
	}
}

// Mode 生效档位（回显用）。
func (w *HumanTaskSLAWorker) Mode() string {
	if w == nil {
		return string(HumanTaskSLAModeOff)
	}
	return string(w.mode)
}

// Start 启动轮询协程（幂等）。off / svc 为 nil 时不启动并出声。
//
// 首轮在 interval 之后才跑，与审批/草稿清扫同口径：启动瞬间正有进程在重启，
// 这时做一轮全表扫描会与恢复期的读请求抢连接。
func (w *HumanTaskSLAWorker) Start(ctx context.Context) {
	if w == nil {
		return
	}
	w.startOnce.Do(func() {
		if w.mode == HumanTaskSLAModeOff {
			logger.Infof("[human-task-sla] mode=off ⇒ 逾期扫描 worker 不启动（待办逾期只被数、不被办）")
			return
		}
		if w.svc == nil {
			logger.Warnf("[human-task-sla] ⚠️ 未注入待办服务 ⇒ 逾期扫描 worker 不启动（逾期待办不会收到提醒）")
			return
		}
		w.mu.Lock()
		w.started = true
		w.mu.Unlock()
		w.wg.Add(1)
		go w.loop(ctx)
		logger.Infof("[human-task-sla] 逾期扫描 worker 已启动：mode=%s 间隔=%s 去重窗口=%s 单轮上限=%d（首轮在 %s 之后）",
			w.mode, w.interval, w.window, humanTaskSLABatchLimit, w.interval)
	})
}

// Stop 停止并等待进行中的那一轮收尾（幂等）。
func (w *HumanTaskSLAWorker) Stop(_ context.Context) {
	if w == nil {
		return
	}
	select {
	case <-w.stop:
		return
	default:
		close(w.stop)
	}
	w.wg.Wait()
	w.mu.Lock()
	w.started = false
	w.mu.Unlock()
}

func (w *HumanTaskSLAWorker) loop(ctx context.Context) {
	defer w.wg.Done()
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stop:
			return
		case <-ticker.C:
			w.RunOnce(ctx)
		}
	}
}

// RunOnce 跑一轮逾期扫描，返回本轮结果。导出的理由是"测的那份就是生产跑的那份"：
// 只靠节拍触发的循环在测试里等不到，去重与升级判据就没法按行为测。
func (w *HumanTaskSLAWorker) RunOnce(ctx context.Context) *HumanTaskSLAReport {
	report := &HumanTaskSLAReport{At: loadHumanTaskNowFn()()}
	if w == nil || w.svc == nil {
		report.Err = "待办服务未注入"
		return report
	}
	if w.mode == HumanTaskSLAModeOff {
		return report // off：既不数也不办
	}

	// 逾期判定口径与 Counts 的逾期读数同源：OverdueOnly 让每类只认自己那一列。
	// OverdueAt 不填 —— service.List 会用同一个时钟 seam 补上，两处读数不会各说各话。
	tasks, _, err := w.svc.List(ctx, HumanTaskListQuery{
		OverdueOnly: true,
		Page:        1,
		PageSize:    humanTaskSLABatchLimit,
	})
	if err != nil {
		report.Err = err.Error()
		logger.Errorf("[human-task-sla] 逾期扫描失败：%v", err)
	} else {
		report.Overdue = len(tasks)
	}

	if w.mode == HumanTaskSLAModeEnforce {
		for _, t := range tasks {
			if t == nil {
				continue
			}
			if w.remind(ctx, t, report.At) {
				report.Reminded++
			}
			if humanTaskShouldEscalate(t, report.At) && w.escalate(ctx, t, report.At) {
				report.Escalated++
			}
		}
	}

	w.mu.Lock()
	w.rounds++
	w.overdueAll += int64(report.Overdue)
	w.remindedAll += int64(report.Reminded)
	w.escalatedAll += int64(report.Escalated)
	w.mu.Unlock()

	if w.mode == HumanTaskSLAModeShadow {
		if report.Overdue > 0 {
			logger.Warnf("[human-task-sla] shadow：本轮扫到 %d 条逾期待办（其中 %d 条已过升级线）——只算不写",
				report.Overdue, humanTaskCountEscalatable(tasks, report.At))
		}
		return report
	}
	if report.Reminded > 0 || report.Escalated > 0 {
		logger.Warnf("[human-task-sla] 逾期处置：提醒 %d 条、升级 %d 条（本轮逾期 %d 条）",
			report.Reminded, report.Escalated, report.Overdue)
	}
	return report
}

// remind 给责任人就该逾期待办发一条站内提醒；去重窗口内发过则跳过。返回是否真写了。
func (w *HumanTaskSLAWorker) remind(ctx context.Context, t *model.HumanTask, now time.Time) bool {
	if w.notifier == nil {
		logger.Warnf("[human-task-sla] 无通知服务 ⇒ 待办 %s 的逾期提醒未写", t.ID)
		return false
	}
	link := humanTaskNotifyLink(t.ID)
	if w.remindedRecently(ctx, HumanTaskOverdueNotifyType, link, now) {
		return false
	}
	n := &model.Notification{
		UserID:  humanTaskAssigneeUserID(t.AssigneeUserID),
		Type:    HumanTaskOverdueNotifyType,
		Title:   "待办逾期提醒",
		Content: fmt.Sprintf("待办《%s》已超过%s时限仍未处理，请尽快认领处理。", t.Title, humanTaskKindSLALabel(t.Kind)),
		Link:    link,
	}
	if err := w.notifier.Create(ctx, n); err != nil {
		logger.Errorf("[human-task-sla] 写逾期提醒失败（待办 %s）：%v", t.ID, err)
		return false
	}
	return true
}

// escalate 逾期时间已拖过整整一个 SLA 窗口时，向管理员广播一条升级通知。
// 去重口径与 remind 同（type 不同，故两条互不遮挡）。
func (w *HumanTaskSLAWorker) escalate(ctx context.Context, t *model.HumanTask, now time.Time) bool {
	if w.notifier == nil {
		logger.Warnf("[human-task-sla] 无通知服务 ⇒ 待办 %s 的逾期升级未写", t.ID)
		return false
	}
	link := humanTaskNotifyLink(t.ID)
	if w.remindedRecently(ctx, HumanTaskOverdueEscalateNotifyType, link, now) {
		return false
	}
	n := &model.Notification{
		UserID:  0, // 升级面向管理员全体：责任人已经拖过一整个 SLA 窗口，不能再只提醒他
		Type:    HumanTaskOverdueEscalateNotifyType,
		Title:   "待办逾期升级",
		Content: fmt.Sprintf("待办《%s》逾期时间已超过%s时限本身，责任人未响应，请立即介入。", t.Title, humanTaskKindSLALabel(t.Kind)),
		Link:    link,
	}
	if err := w.notifier.Create(ctx, n); err != nil {
		logger.Errorf("[human-task-sla] 写逾期升级失败（待办 %s）：%v", t.ID, err)
		return false
	}
	return true
}

// remindedRecently 问"这个 (type, link) 在去重窗口内发过没有"。查询失败按"发过"处理：
// 宁少发一条也不给坐席刷重复提醒，且失败已出声（fail-quiet 而不是 fail-open 到刷屏）。
func (w *HumanTaskSLAWorker) remindedRecently(ctx context.Context, ntype, link string, now time.Time) bool {
	recent, err := w.notifier.HasRecentByTypeLink(ctx, ntype, link, now.Add(-w.window))
	if err != nil {
		logger.Errorf("[human-task-sla] 去重查询失败（type=%s link=%s）⇒ 本轮不重复写：%v", ntype, link, err)
		return true
	}
	return recent
}

// humanTaskShouldEscalate 判定逾期是否已拖过整整一个 SLA 窗口。
//
// 窗口长度取"该行自己那一列的截止时刻 − 开放时刻"，于是三类待办各自按自己的
// SLA 尺度衡量，不需要在这里再维护一份时长表。取不到截止或开放时刻就不升级。
func humanTaskShouldEscalate(t *model.HumanTask, now time.Time) bool {
	deadline, ok := humanTaskSLADeadline(t)
	if !ok {
		return false
	}
	span := deadline.Sub(t.CreatedAt)
	if span <= 0 {
		return false
	}
	return !now.Before(deadline.Add(span * time.Duration(humanTaskSLAEscalateFactor-1)))
}

// humanTaskCountEscalatable shadow 档回显用：数一数本轮有几条已过升级线。
func humanTaskCountEscalatable(tasks []*model.HumanTask, now time.Time) int {
	n := 0
	for _, t := range tasks {
		if t != nil && humanTaskShouldEscalate(t, now) {
			n++
		}
	}
	return n
}

// humanTaskSLADeadline 取该待办自己那一档的 SLA 截止时刻（每类只认自己那一列）。
func humanTaskSLADeadline(t *model.HumanTask) (time.Time, bool) {
	if t == nil {
		return time.Time{}, false
	}
	switch t.Kind {
	case model.HumanTaskKindConversationHandoff:
		if t.SlaFirstResponseAt != nil {
			return *t.SlaFirstResponseAt, true
		}
	case model.HumanTaskKindApproval:
		if t.SlaDecideAt != nil {
			return *t.SlaDecideAt, true
		}
	case model.HumanTaskKindCollectionEscalation:
		if t.SlaEscalateAt != nil {
			return *t.SlaEscalateAt, true
		}
	}
	return time.Time{}, false
}

// humanTaskKindSLALabel 三类 SLA 各自量的是什么（提醒文案用）。
func humanTaskKindSLALabel(kind string) string {
	switch kind {
	case model.HumanTaskKindConversationHandoff:
		return "首次响应"
	case model.HumanTaskKindApproval:
		return "裁决"
	case model.HumanTaskKindCollectionEscalation:
		return "催收升级"
	default:
		return "处理"
	}
}

// humanTaskNotifyLink 提醒/升级的去重链接（同时是前端点开待办的深链）。
func humanTaskNotifyLink(id string) string { return "/human-tasks/" + id }

// humanTaskAssigneeUserID 把待办的字符串责任人解析成通知的 uint 用户 id。
// 解析不出（空串 / 非数字）一律回 0 = 全体可见：一条逾期提醒发错人比没人看到更坏。
func humanTaskAssigneeUserID(raw string) uint {
	n, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0
	}
	return uint(n)
}

// Running 轮询协程是否在跑。
func (w *HumanTaskSLAWorker) Running() bool {
	if w == nil {
		return false
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.started
}

// Rounds 已完成的轮次数（含失败轮）。
func (w *HumanTaskSLAWorker) Rounds() int64 {
	if w == nil {
		return 0
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.rounds
}

// OverdueTotal 累计扫到的逾期条数（装配日志退出时回显用）。
func (w *HumanTaskSLAWorker) OverdueTotal() int64 {
	if w == nil {
		return 0
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.overdueAll
}

// RemindedTotal 累计发出的逾期提醒数。
func (w *HumanTaskSLAWorker) RemindedTotal() int64 {
	if w == nil {
		return 0
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.remindedAll
}

// EscalatedTotal 累计发出的逾期升级数。
func (w *HumanTaskSLAWorker) EscalatedTotal() int64 {
	if w == nil {
		return 0
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.escalatedAll
}
