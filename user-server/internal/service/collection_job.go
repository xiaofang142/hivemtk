// collection_job.go 催收任务：逾期识别 → 提醒外发 → 升级待办（T-P7-03 / N-6）。
//
// # 逾期口径（AC①，全系统只有这一处定义）
//
//	逾期 = 账单状态 ∈ model.BillStatusesChased（open / partial）
//	       ∧ due_at 非空
//	       ∧ now ≥ due_at + CollectionGraceDays × 24h
//
// 三件事分别住在三处，各自不能挪：
//   - **哪些状态算欠着**：`model.BillStatusesChased`（与 `PaymentStatusesCounted` 同族），
//     比较发生在 `BillRepository.ScanOverdue` 那一句 SQL 里，本文件不抄字面量；
//   - **账期本身**：`bills.due_at`，是合同事实、由派生那一笔带进来（见 service/bill.go 的
//     `BillDeriveInput.DueAt`），本任务**从不**替它猜一个日期；
//   - **宽限期与升级线**：下面那两个常量。
//
// 宽限期按 **24 小时滚动**算，不做"自然日取整"。这不是偷懒而是刻意：日历取整必须先选定
// 时区，而本仓在日期边界上栽过一次（PG 会话钉着 CST、Go 按宿主机时区格式化 ⇒ 同一笔账
// 在 UTC 16:00 之后与之前算出的"逾期天数"差一天）。这里全程只做 `time.Time` 的绝对时刻
// 比较，宿主时区改不动判据 —— 用例 `TestCollectionCutoffAppliesTheDocumentedGrace`
// 拿"同一瞬间换时区表示"钉住这条。
//
// # 四道必须同时成立的约束（与挽回 worker 那一族同构）
//
//  1. **两把独立的锁**：`FF_LTC_COLLECTION_JOB`（这条腿挂没挂）× `ltc.config` 的
//     `collection` 阶段档（这一轮开不开）。off 连 goroutine 都不起、一轮都不扫；
//     shadow 只选路不发不写；enforce 才真发。阶段档关着的每一轮一次查询都不发。
//     本文件是 `collection` 那一档的**第一个业务读者**（T-P3-06 交付它时全仓零读者）。
//     开关每轮现读，不在装配期抄快照：一键回滚要下一轮就咬得住。
//  2. **频控复用既有那道，不另起一套**（AC②）：DNC、发送前审批闸门（T-P3-07）、
//     按客户一小时的冷却，三判据全在 `ProactiveReachService.ReachByCustomer` 内部。
//     本任务在它之上只加一格"**同一张应收**多久催一次"（`CollectionReminderWindow`）——
//     那一格按账单号取锁，触达服务的窗按客户取，两把钥匙解决的是两件不同的事。
//     **同一轮里同一客户的多张逾期单合成一条消息**：不合并的话第二张永远撞那一小时的
//     冷却，报告里写着"失败"，真实世界是那个人只收到一条、另一张单根本没人提。
//  3. **取锁失败必须拒发（fail-closed）**：与触达服务自己的 `checkCooldown` 方向刻意相反。
//     那边的定位是"少打扰"，缓存故障时放行代价小；这边的锁定位是"同一张别催两遍"，
//     故障时放行就是重复外发。方向差异由用例钉住，免得下一个人"顺手统一"。
//  4. **升级收口**：越过 `CollectionEscalateAfterDays` 的单**不再自动催**，改投一条
//     `kind=collection_escalation` 的人工待办（待办中心的第三类分离视图，AC③ 与前端
//     `user-web/src/views/approvalTask` 那一路对上）。两档由那条线互斥地分开：
//     交给人之后系统还在按窗群发，人处理完回来发现客户又被机器人催了两轮。
//
// # 两件刻意没做
//
//   - **不改账单状态**：依赖面上只有 `ScanOverdue` 一格读，催收这条腿写不出
//     "把这单标成已付/作废"（端口用例 `TestCollectionJobPortSurfacesAreNarrow` 是这句话
//     唯一的硬保证）。结清只由回款入账那一条路推动（T-P7-02）。
//   - **不推商机、不触发复购**：账单结清 → 商机赢单 → 复购 SOP 是 T-P7-04 的链路，
//     在这里"顺手"推进商机状态会让赢单同时有两个驱动方。
//
// # 一条已知代价
//
// 身份按 `bills.opportunity_id → opportunities.customer_id / one_id` 现推（应收表上
// 没有客户列，判据见 `model/bill.go`），于是单轮内每行一次商机读。批上限是
// `LTC_COLLECTION_JOB_BATCH`（默认 20），且逾期行天然按客户聚簇，不做预取 join：
// 为个位数的批量给催收加一条跨表读口，代价是催收从此依赖两张表的连接形状。

package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// 开关与子参数名集中在此：装配日志、运维文档与实际读取的变量名同源。
const (
	// CollectionJobFlagEnv 主开关，取值 off|shadow|enforce（解析复用挽回 worker 那一份，
	// 同一个三态口径在两处各写一遍，早晚一处认 `true` 一处不认）。
	CollectionJobFlagEnv = "FF_LTC_COLLECTION_JOB"
	// CollectionJobBatchEnv 单轮扫描封顶（对应 ScanOverdue 的 limit）。
	CollectionJobBatchEnv = "LTC_COLLECTION_JOB_BATCH"
	// CollectionJobIntervalEnv 轮询间隔。
	CollectionJobIntervalEnv = "LTC_COLLECTION_JOB_INTERVAL"
)

// 催收口径的三个数。改任何一个都要同时改本文件头部那段口径文字，
// 且**不走环境变量**：宽限期与升级线是财务口径而不是运维旋钮，
// 一个能在启动参数里改掉的逾期定义，等于每个副本各有自己的逾期定义。
const (
	// CollectionGraceDays 宽限期：账期过后多少天才算逾期（也给客户留出"款在路上"的余量）。
	CollectionGraceDays = 3
	// CollectionEscalateAfterDays 升级线：逾期满多少天就不再自动催、改投人工待办。
	CollectionEscalateAfterDays = 14
	// CollectionReminderWindow 同一张应收两次自动提醒的最小间隔（锁的租约）。
	CollectionReminderWindow = 7 * 24 * time.Hour
	// CollectionEscalateWindow 同一张应收两次升级之间的最小间隔。
	//
	// 它兜的是 `HumanTaskService.Submit` 幂等边界之外的另一半：Submit 按"有没有**开放**
	// 待办"判重，人处理完（completed）之后那张单就"没人管"了。没有这把窗，
	// 一条被认真处理完的逾期单会在下一轮（默认 6 小时）又冒出一条同名待办。
	CollectionEscalateWindow = 30 * 24 * time.Hour
	// CollectionEscalateResponseWindow 升级待办的处理截止（sla_escalate_at）。
	// 必须由投递方给：`slaFor` 对 collection 类是必填格，空着整条投递被拒 ——
	// 没有截止的待办永远不会出现在逾期读数里，而这条待办要的恰恰是"有人来看"。
	CollectionEscalateResponseWindow = 3 * 24 * time.Hour
)

const (
	collectionJobDefaultBatch    = 20
	collectionJobMaxBatch        = 200
	collectionJobDefaultInterval = 6 * time.Hour
	// collectionJobMinInterval 下限：低于一轮的执行时间，同一张单会被两轮各领一次
	// （锁能拦住外发，但报告会开始大量出现 reminders_held）。
	collectionJobMinInterval = 30 * time.Minute
	// collectionBillPayloadRefPrefix 待办上"处理时去看哪里"的前缀，与账单读侧路由同源
	// （T-P7-02 的 GET /api/bill/:id）。
	collectionBillPayloadRefPrefix = "/api/bill/"
)

// 两把锁的键前缀分开是必要的：一张越过升级线的单同时落在两个窗里，
// 共用一把键的话"催过"与"升过"就再也分不开（先占哪个全看代码顺序）。
const (
	collectionRemindKeyPrefix   = "mtk:collection:remind:"
	collectionEscalateKeyPrefix = "mtk:collection:escalate:"
)

// collectionDateLayout 对外露出的日期一律这个形状（不带时区、不暗示精度）。
const collectionDateLayout = "2006-01-02"

// collectionBillScanner 本任务对账单存储的**全部**依赖：一格读。
//
// 没有 UpdateStatus、没有 Create、没有 Delete：催收这条腿不许改写凭证
// （判据与理由见端口用例）。
type collectionBillScanner interface {
	ScanOverdue(ctx context.Context, cutoff time.Time, limit int) (*repository.BillOverdueScan, error)
}

// collectionOpportunityReader 身份来路：应收上没有客户列，只认得商机。
type collectionOpportunityReader interface {
	GetByID(ctx context.Context, id string) (*model.Opportunity, error)
}

// collectionReachSender 唯一被允许的客户出口（同挽回 worker 与报价发送腿）。
type collectionReachSender interface {
	ReachByCustomer(ctx context.Context, req *ProactiveReachRequest) (*ProactiveReachResponse, error)
}

// collectionTaskSubmitter 升级待办的投递口（幂等由它自己按 open subject 判）。
type collectionTaskSubmitter interface {
	Submit(ctx context.Context, in HumanTaskSubmitInput) (*model.HumanTask, bool, error)
}

// collectionStageGate 阶段开关。*LTCConfigService 结构上即满足，走接口而不是具体类型
// 是为了让"每轮现读"这件事能被测试直接喂一个会改口的假件。
type collectionStageGate interface {
	StageActive(ctx context.Context, stage LTCStage) (bool, string)
}

// CollectionRoundReport 单轮读数。
//
// 计数按**处置**分而不是按错误码分：排障时要回答的是"这一轮有多少张单被催了、
// 多少张因为已经升级/已被窗拦住/找不到人而没动"。
// 会覆盖这件事的形状只有一种：把三种"没发"塌成一个 failed（见用例
// TestCollectionBlockedReasonsAreCountedApart）。
type CollectionRoundReport struct {
	Mode        string    `json:"mode"`
	StartedAt   time.Time `json:"started_at"`
	StageReason string    `json:"stage_reason,omitempty"`
	// SkippedByStage 这一轮被阶段开关拦住时为 1：运维据此把"跑了但被开关拦住"
	// 与"协程压根没起"分开（两件完全不同的事，两种不同的查法）。
	SkippedByStage int `json:"skipped_by_stage"`

	Overdue   int   `json:"overdue"`
	Undated   int64 `json:"undated"`
	Truncated bool  `json:"truncated"`

	Reminded    int `json:"reminded"`
	Escalated   int `json:"escalated"`
	TasksReused int `json:"tasks_reused"`

	// 四类"这一轮到了但没动"：分别对应四种处置动作。
	RemindersHeld     int `json:"reminders_held"`
	EscalationHeld    int `json:"escalation_held"`
	SkippedNoIdentity int `json:"skipped_no_identity"`
	// SkippedNotOverdue 「扫出来的行我不认它逾期」。刻意不叫 no_due_date：账期未定
	// 那一格已经有 `Undated` 在数（那是运营要看的读数），这一格是**自检** ——
	// 仓储的查询条件与这里的判据分开了才可能非零。两种坏法（读不出账期 /
	// 其实还在宽限期内）处置动作相同（这一轮不碰它），所以合成一格，
	// 换来"`SkippedNotOverdue > 0` 是一个干净的告警条件"。
	SkippedNotOverdue int `json:"skipped_not_overdue"`

	BlockedByDNC      int    `json:"blocked_by_dnc"`
	BlockedByApproval int    `json:"blocked_by_approval"`
	BlockedByCooldown int    `json:"blocked_by_cooldown"`
	ClaimUnavailable  int    `json:"claim_unavailable"`
	Failed            int    `json:"failed"`
	WouldRemind       int    `json:"would_remind"`
	WouldEscalate     int    `json:"would_escalate"`
	ScanError         string `json:"scan_error,omitempty"`
	DependencyError   string `json:"dependency_error,omitempty"`
}

func (r *CollectionRoundReport) shadow() bool { return r.Mode == string(RecoveryWorkerModeShadow) }

// shadowSuffix 把"这一轮只是看了两眼"写在同一行日志尾部，而不是靠读者去对上方的 mode。
func (r *CollectionRoundReport) shadowSuffix() string {
	if !r.shadow() {
		return ""
	}
	return fmt.Sprintf(" （shadow：预计可催=%d 预计可升=%d，均未发出、均未落待办、未占窗）",
		r.WouldRemind, r.WouldEscalate)
}

// CollectionJob 催收任务。
type CollectionJob struct {
	bills    collectionBillScanner
	opps     collectionOpportunityReader
	reach    collectionReachSender
	tasks    collectionTaskSubmitter
	stages   collectionStageGate
	mode     RecoveryWorkerMode
	batch    int
	interval time.Duration
	// 两个窗口是分开的两把锁的租约，见上面的前缀注释。
	remindWindow   time.Duration
	escalateWindow time.Duration
	nowFunc        func() time.Time

	// 取锁与还锁。默认走全局缓存（Redis 或进程内），测试里换成内存实现。
	// 两格都注入：只测"占没占到"，测不出"没发出去要把窗还回来"那一格。
	setNX   func(ctx context.Context, key, token string, ttl time.Duration) (bool, error)
	release func(ctx context.Context, key, token string) (bool, error)

	stop      chan struct{}
	wg        sync.WaitGroup
	startOnce sync.Once

	mu          sync.RWMutex
	startedFlag bool
	last        *CollectionRoundReport
	// 累计读数：LastReport 一轮覆盖一轮，跨轮要看的只有这两个数（同挽回 worker 的学费）。
	remindedAll  atomic.Int64
	escalatedAll atomic.Int64
}

// NewCollectionJob 按环境变量装配模式与节奏，依赖全部由调用方给。
//
// 缺件时构造照旧成功，由 Available / RunOnce 报出来（与 bill / payment 两条腿同一形状）：
// 装配点要能在没配催收的环境里起来，但绝不能因此"静默不催"。
func NewCollectionJob(bills collectionBillScanner, opps collectionOpportunityReader,
	reach collectionReachSender, tasks collectionTaskSubmitter, stages collectionStageGate) *CollectionJob {
	return &CollectionJob{
		bills:    bills,
		opps:     opps,
		reach:    reach,
		tasks:    tasks,
		stages:   stages,
		mode:     parseRecoveryWorkerMode(os.Getenv(CollectionJobFlagEnv)),
		batch:    envIntOr(CollectionJobBatchEnv, collectionJobDefaultBatch, 1, collectionJobMaxBatch),
		interval: envDurationOr(CollectionJobIntervalEnv, collectionJobDefaultInterval),
		// 两个窗口的下限都由节奏推出来：轮询比窗还密的话，每一轮都在占锁-还锁上空转，
		// 报告里全是 reminders_held，看不出真实世界发生了什么。
		remindWindow:   CollectionReminderWindow,
		escalateWindow: CollectionEscalateWindow,
		nowFunc:        time.Now,
		setNX:          cacheClaimWithToken,
		release:        cacheReleaseClaim,
		stop:           make(chan struct{}),
	}
}

func cacheClaimWithToken(ctx context.Context, key, token string, ttl time.Duration) (bool, error) {
	return cache.GetGlobalCache().SetNX(ctx, key, token, ttl)
}

func cacheReleaseClaim(ctx context.Context, key, token string) (bool, error) {
	return cache.GetGlobalCache().ReleaseLock(ctx, key, token)
}

// Available 五条依赖齐了才叫能跑。
//
// 少任何一条都宁可不跑：reach 缺了还继续跑的话，升级那一半照样会落待办，
// 于是"催收开了"这件事在日志里成立，而客户一条消息都没收到过。
func (j *CollectionJob) Available() bool {
	return j != nil && j.bills != nil && j.opps != nil && j.reach != nil && j.tasks != nil && j.stages != nil
}

// SetClockForTest 注入时钟（催收的每一天都从这一格走）。
//
// 导出是为了让装配测试（app / router 包）也能钉住时间；生产不碰它。
func (j *CollectionJob) SetClockForTest(now func() time.Time) {
	if j == nil {
		return
	}
	if now == nil {
		now = time.Now
	}
	j.nowFunc = now
}

// Mode 当前生效模式。
func (j *CollectionJob) Mode() RecoveryWorkerMode {
	if j == nil {
		return RecoveryWorkerModeOff
	}
	return j.mode
}

// CollectionJobModeFromEnv 按**当前环境变量**回答"此刻配的是哪一档"。
//
// 只给"这台任务还没装配起来"的观测分支用：已装配时读数必须来自实例，因为装配期那次
// 解析可能被下限夹取修正过（间隔太短会被抬到 collectionJobMinInterval）。
// 这里复用挽回 worker 那一份三态解析器而不是再写一个 switch：同一个 off|shadow|enforce
// 口径在两处各写一遍，早晚一处认 `true` 一处不认。
func CollectionJobModeFromEnv() RecoveryWorkerMode {
	return parseRecoveryWorkerMode(os.Getenv(CollectionJobFlagEnv))
}

// Batch 单轮扫描封顶（观测端点回显用）。
//
// 三格节奏都只给读、不给改：这些数一旦能在运行时改掉，"这一轮为什么没催"就又多了一个
// 只有当场才知道答案的来源。
func (j *CollectionJob) Batch() int {
	if j == nil {
		return 0
	}
	return j.batch
}

// Interval 轮询间隔。
//
// 读的是**实例上生效的那一份**：Start 会把低于下限的间隔抬到 collectionJobMinInterval，
// 端点若回显 env 原文就会说成一个从没跑过的数。
func (j *CollectionJob) Interval() time.Duration {
	if j == nil {
		return 0
	}
	return j.interval
}

// Start 起轮询协程（幂等）。off 档连协程都不起。
func (j *CollectionJob) Start(ctx context.Context) {
	if j == nil {
		return
	}
	j.startOnce.Do(func() {
		if j.mode == RecoveryWorkerModeOff {
			logger.Infof("[CollectionJob] %s=off ⇒ 未启动：逾期应收不会被自动提醒，也不会升级人工"+
				"（要开始观察请改成 shadow，再改 enforce 放量；ltc.config 的 collection 阶段档是第二把锁）",
				CollectionJobFlagEnv)
			return
		}
		if !j.Available() {
			logger.Warnf("[CollectionJob] ⚠️ 依赖不齐 ⇒ 不启动（mode=%s available=false）", j.mode)
			return
		}
		if j.interval < collectionJobMinInterval {
			logger.Warnf("[CollectionJob] ⚠️ %s=%s 小于 %s ⇒ 抬到 %s（一轮没跑完下一轮就起）",
				CollectionJobIntervalEnv, j.interval, collectionJobMinInterval, collectionJobMinInterval)
			j.interval = collectionJobMinInterval
		}
		j.mu.Lock()
		j.startedFlag = true
		j.mu.Unlock()
		j.wg.Add(1)
		go j.loop(ctx)
		if !cache.GlobalIsRedis() {
			logger.Warnf("[CollectionJob] ⚠️ 全局缓存不是 Redis ⇒ 提醒窗只在单进程内有效，"+
				"多副本部署时同一个人会被各副本各催一次。放量前把 Redis 配上。mode=%s", j.mode)
		}
		logger.Infof("[CollectionJob] 已启动：mode=%s 间隔=%s 单轮上限=%d 宽限期=%d天 升级线=%d天 "+
			"提醒窗=%s 升级窗=%s（首轮在 %s 后才跑）",
			j.mode, j.interval, j.batch, CollectionGraceDays, CollectionEscalateAfterDays,
			j.remindWindow, j.escalateWindow, j.interval)
	})
}

// Stop 停止并等当前轮收尾（幂等）。
func (j *CollectionJob) Stop(_ context.Context) {
	if j == nil {
		return
	}
	select {
	case <-j.stop:
	default:
		close(j.stop)
	}
	j.wg.Wait()
	j.mu.Lock()
	j.startedFlag = false
	j.mu.Unlock()
	logger.Info("[CollectionJob] 已停止")
}

// Running 轮询协程是否在跑。
func (j *CollectionJob) Running() bool {
	if j == nil {
		return false
	}
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.startedFlag
}

// LastReport 最近一轮**跑完的**读数（拷贝；还没跑完过任何一轮时回 nil）。
func (j *CollectionJob) LastReport() *CollectionRoundReport {
	if j == nil {
		return nil
	}
	j.mu.RLock()
	defer j.mu.RUnlock()
	if j.last == nil {
		return nil
	}
	copied := *j.last
	return &copied
}

// RemindedTotal 跨轮累计：真发出去的提醒条数。
func (j *CollectionJob) RemindedTotal() int64 {
	if j == nil {
		return 0
	}
	return j.remindedAll.Load()
}

// EscalatedTotal 跨轮累计：新建的升级待办条数。
func (j *CollectionJob) EscalatedTotal() int64 {
	if j == nil {
		return 0
	}
	return j.escalatedAll.Load()
}

// setLast 发布一轮读数。锁只订得住这个指针，订不住它指向的那格 ——
// 所以调用方必须等 report 填完（含 logRound）再发布，不能"先挂上去再一路往上写"：
// 观测端点是另一个协程在读，写的一侧不持锁就是数据竞争（判据与用例
// TestCollectionLastReportIsNeverAHalfRound 一对一，同挽回 worker 的发布位）。
func (j *CollectionJob) setLast(r *CollectionRoundReport) {
	j.mu.Lock()
	j.last = r
	j.mu.Unlock()
}

func (j *CollectionJob) loop(ctx context.Context) {
	defer j.wg.Done()
	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()
	for {
		select {
		case <-j.stop:
			return
		case <-ticker.C:
			if _, err := j.RunOnce(ctx); err != nil {
				logger.Ctx(ctx).Error().Err(err).Msg("[CollectionJob] 本轮执行失败")
			}
		}
	}
}

// RunOnce 跑一轮：判开关 → 扫逾期 → 逐张分派（升级 / 归组待催）→ 按客户合并发。
//
// 返回的 error 只代表"这一轮整体没跑起来"（依赖不齐、扫描失败）；
// 单条的失败都记在报告里 —— 一张读不到的商机不该让整条催收停摆，
// 但一次扫不动的库必须出声（把库故障读成"今天没人逾期"是这一族最贵的假绿）。
func (j *CollectionJob) RunOnce(ctx context.Context) (*CollectionRoundReport, error) {
	if j == nil {
		return nil, errors.New("collection job: 任务未装配")
	}
	now := j.nowFunc()
	report := &CollectionRoundReport{Mode: string(j.mode), StartedAt: now}

	if j.mode == RecoveryWorkerModeOff {
		report.DependencyError = "worker mode off"
		j.setLast(report)
		return report, nil
	}
	if !j.Available() {
		report.DependencyError = "五条依赖（账单读口/商机读口/触达/待办/阶段开关）未装配齐"
		j.setLast(report)
		return report, fmt.Errorf("collection job: %s", report.DependencyError)
	}
	if on, reason := j.stages.StageActive(ctx, LTCStageCollection); !on {
		// 阶段关着 ⇒ 连扫描都不发：那一档的语义是"催收这件事没开"，
		// 不是"开了但只观察"（那是 shadow 档的活，两档塌成一格的那天，运营就再也
		// 分不清"没开"与"在观察"，而这两种状态的排查方向相反）。
		report.SkippedByStage = 1
		report.StageReason = reason
		j.setLast(report)
		return report, nil
	}

	scan, err := j.bills.ScanOverdue(ctx, collectionCutoff(now), j.batch)
	if err != nil {
		report.ScanError = err.Error()
		j.setLast(report)
		return report, fmt.Errorf("collection job: 逾期扫描失败: %w", err)
	}
	if scan == nil {
		report.ScanError = "扫描回了 nil 读数"
		j.setLast(report)
		return report, errors.New("collection job: 扫描回了 nil 读数（既不是空也不是错，不能采信）")
	}
	report.Overdue = len(scan.Overdue)
	report.Undated = scan.Undated
	report.Truncated = scan.Truncated

	// 第一遍：逐张判"升不升级"，并把要催的按客户归组。
	// 分两遍而不是一边判一边发，是因为"一个客户一条消息"这件事要看完整组才知道。
	groups := make(map[string]*collectionRemindGroup)
	var groupOrder []string
	for _, bill := range scan.Overdue {
		// 两条"扫出来但不该催"的判据各占一格，且都**不带 else**：正常链路上走不到
		// （仓储那条查询就带着这两个条件），它们非零只说明一件事 —— 查询与这里的口径分开了。
		// 为什么不"信仓储"：逾期天数是这一路唯一的输出，宽限期一旦在两处各写一份，
		// 挪一边的后果是在宽限期内催款（这一族最贵的一次打扰），以及对读不出账期的行取天数直接 panic。
		if bill == nil || bill.DueAt == nil {
			report.SkippedNotOverdue++
			continue
		}
		overdueFor := now.Sub(*bill.DueAt)
		if overdueFor < CollectionGraceDays*24*time.Hour {
			report.SkippedNotOverdue++
			continue
		}
		who, err := j.resolveIdentity(ctx, bill)
		if err != nil {
			report.Failed++
			continue
		}
		if who.empty() {
			report.SkippedNoIdentity++
			continue
		}
		if overdueFor >= CollectionEscalateAfterDays*24*time.Hour {
			j.escalate(ctx, bill, who, now, report)
			continue
		}
		var winKey, winToken string
		if !j.shadow() {
			winKey = collectionRemindKeyPrefix + bill.ID
			winToken = collectionToken()
			claimed, cerr := j.setNX(ctx, winKey, winToken, j.remindWindow)
			if cerr != nil {
				// fail-closed，方向与触达自己的冷却刻意相反（理由见文件头第 3 条）。
				report.ClaimUnavailable++
				continue
			}
			if !claimed {
				report.RemindersHeld++
				continue
			}
		}
		custKey := who.key()
		g := groups[custKey]
		if g == nil {
			g = &collectionRemindGroup{who: who, token: map[string]string{}}
			groups[custKey] = g
			groupOrder = append(groupOrder, custKey)
		}
		g.bills = append(g.bills, bill)
		if winKey != "" {
			g.token[winKey] = winToken
		}
	}

	// 第二遍：一个客户一条。
	for _, k := range groupOrder {
		j.sendReminder(ctx, groups[k], now, report)
	}

	j.logRound(report)
	j.setLast(report)
	return report, nil
}

// shadow 包的读法单独一格：mode 判定散在两个方法里早晚写反一次。
func (j *CollectionJob) shadow() bool { return j.mode == RecoveryWorkerModeShadow }

// collectionRemindGroup 一个客户这一轮要催的那批单，连同它占下的那些窗。
type collectionRemindGroup struct {
	who   collectionIdentity
	bills []*model.Bill
	// key → token：发失败时按这把 token 还锁（还错人比不还可糟）。
	// shadow 档这里恒空 ⇒ 还锁天然空转，不需要一处"是否观察档"的分支去记着它。
	token map[string]string
}

// collectionIdentity 从商机上取到的那两把客户钥匙。
type collectionIdentity struct {
	customerID string
	oneID      string
}

func (i collectionIdentity) empty() bool { return i.customerID == "" && i.oneID == "" }

// key 与客户 360 视图同取向：one_id 优先（它是跨渠道归一后的那个人），
// 退回 customer_id。用错这把钥匙的后果是同一封催款信按"两个身份"各发一遍。
func (i collectionIdentity) key() string {
	if i.oneID != "" {
		return i.oneID
	}
	return i.customerID
}

func (j *CollectionJob) resolveIdentity(ctx context.Context, bill *model.Bill) (collectionIdentity, error) {
	var none collectionIdentity
	// 应收上没有客户列（判据见 model/bill.go），唯一合法的来路是那张商机。
	if strings.TrimSpace(bill.OpportunityID) == "" {
		return none, errors.New("collection: 应收行上没有商机来路")
	}
	opp, err := j.opps.GetByID(ctx, bill.OpportunityID)
	if err != nil {
		return none, fmt.Errorf("collection: 读商机 %s 失败: %w", bill.OpportunityID, err)
	}
	if opp == nil {
		return none, nil
	}
	return collectionIdentity{customerID: opp.CustomerID, oneID: opp.OneID}, nil
}

// sendReminder 一个客户一条。失败时的处置按三种"没发出去"分开走。
func (j *CollectionJob) sendReminder(ctx context.Context, g *collectionRemindGroup, now time.Time, report *CollectionRoundReport) {
	if g == nil || len(g.bills) == 0 {
		return
	}
	req := &ProactiveReachRequest{
		CustomerID: g.who.customerID,
		OneID:      g.who.oneID,
		Content:    collectionReminderBody(g.bills, now),
		Subject:    "应收款项到期提醒",
	}
	shadow := j.shadow()
	// 观察档走到同一个出口、只把 DryRun 拧上去：这样"会被 DNC 拦"与"会被闸门拦"
	// 这两种答案在 shadow 里读得到，而在 enforce 里同样是这一条路。
	req.DryRun = shadow
	if _, err := j.reach.ReachByCustomer(ctx, req); err != nil {
		j.countBlocked(err, report)
		j.returnWindow(ctx, g, err, report)
		return
	}
	if shadow {
		// 预计值与真值分列记：把 shadow 的"本可催"并进 reminded，累计读数就再也
		// 回答不了"到底发出去了几条"，而那是这一族唯一要给运营看的数。
		report.WouldRemind++
		return
	}
	report.Reminded++
	j.remindedAll.Add(1)
}

// countBlocked 三种"没发出去"分开记（合成一个 failed 的那格见用例）：
// 退订要长期停、闸门要补授权、冷却只要下一轮再来。
func (j *CollectionJob) countBlocked(err error, report *CollectionRoundReport) {
	switch {
	case errors.Is(err, ErrDoNotContact):
		report.BlockedByDNC++
	case errors.Is(err, ErrReachApprovalDenied):
		report.BlockedByApproval++
	case errors.Is(err, ErrReachCooldown):
		report.BlockedByCooldown++
	default:
		report.Failed++
	}
}

// returnWindow 发不出去就把窗还掉，退订除外。
//
// 还窗的理由：一次渠道抖动占掉七天，等于那张单七天没人催，而日志此后不再重复这件事。
// 不还不退订的理由正相反：那是一个**永久**的意愿表达，重跑一轮就再打扰一次，
// 而"退订的人每天被机器人催"是这一族最贵的一条投诉。
// 闸门拒与冷却要还：两者都是"这一刻不行"，不是"这件事结了"。
func (j *CollectionJob) returnWindow(ctx context.Context, g *collectionRemindGroup, sendErr error, report *CollectionRoundReport) {
	if errors.Is(sendErr, ErrDoNotContact) {
		return
	}
	for key, token := range g.token {
		if _, err := j.release(ctx, key, token); err != nil {
			logger.Ctx(ctx).Warn().Err(err).Str("key", key).
				Msg("[CollectionJob] ⚠️ 提醒键没还掉：这张单在当前窗口内不会被再催，到期后自愈")
		}
	}
}

// escalate 越过升级线：投一条人工待办，并**不再自动催**（两档互斥由那条线保证）。
func (j *CollectionJob) escalate(ctx context.Context, bill *model.Bill, who collectionIdentity, now time.Time, report *CollectionRoundReport) {
	if j.shadow() {
		report.WouldEscalate++
		return
	}
	key := collectionEscalateKeyPrefix + bill.ID
	token := collectionToken()
	claimed, err := j.setNX(ctx, key, token, j.escalateWindow)
	if err != nil {
		report.ClaimUnavailable++
		return
	}
	if !claimed {
		report.EscalationHeld++
		return
	}
	days := collectionOverdueDays(bill, now)
	sla := now.Add(CollectionEscalateResponseWindow)
	_, created, serr := j.tasks.Submit(ctx, HumanTaskSubmitInput{
		Kind:        model.HumanTaskKindCollectionEscalation,
		SubjectType: HumanTaskSubjectCollectionCase,
		// subject_id 是账单号：待办必须钉在**那一张应收**上。钉商机会让同一商机的
		// 两张逾期单共用一条待办（而催的是两笔钱），钉报价则跨版本链没有唯一含义。
		SubjectID:  bill.ID,
		Title:      fmt.Sprintf("催收升级：应收单 %s 逾期 %d 天", bill.ID, days),
		Reason:     collectionEscalateReason(bill, now, days),
		PayloadRef: collectionBillPayloadRefPrefix + bill.ID,
		// 待办上要能直接看到是谁的钱：接手的人第一动作是"找到这个客户"，
		// 让他从账单号再跳两跳去拼身份的代价是没人跳（于是待办被当成"又一条系统噪音"关掉）。
		OneID:    who.oneID,
		SlaDueAt: &sla,
	})
	if serr != nil {
		// 待办没落成 = 这次升级等于没发生，占着窗只会让它静默消失三十天。
		if _, rerr := j.release(ctx, key, token); rerr != nil {
			logger.Ctx(ctx).Warn().Err(rerr).Str("key", key).
				Msg("[CollectionJob] ⚠️ 升级键没还掉：这条待办的重试要等窗自己过期")
		}
		report.Failed++
		return
	}
	if created {
		report.Escalated++
		j.escalatedAll.Add(1)
		return
	}
	// created=false 是"这件事已经有人在等"，不是失败，也不还窗。
	report.TasksReused++
}

func (j *CollectionJob) logRound(r *CollectionRoundReport) {
	logger.Infof("[CollectionJob] 本轮结束 mode=%s 逾期=%d 账期未定=%d 截断=%t 已催=%d 已升级=%d "+
		"提醒窗内=%d 升级窗内=%d 待办复用=%d 无身份=%d 不认逾期=%d 退订=%d 闸门=%d 冷却=%d "+
		"取锁不可用=%d 失败=%d%s",
		r.Mode, r.Overdue, r.Undated, r.Truncated, r.Reminded, r.Escalated,
		r.RemindersHeld, r.EscalationHeld, r.TasksReused, r.SkippedNoIdentity, r.SkippedNotOverdue,
		r.BlockedByDNC, r.BlockedByApproval, r.BlockedByCooldown,
		r.ClaimUnavailable, r.Failed, r.shadowSuffix())
}

// ---- 口径与文案（都是纯函数：给定输入必给出同一个答案，事后能回答"当时说了什么"）----

// collectionCutoff 逾期判据里那个"账期早于此刻才算"的此刻（宽限期在这一步扣掉）。
//
// 只有 Add：不用 AddDate —— 后者按日历走，跨时区时同一瞬间会算出两个 cutoff
// （判据与用例 TestCollectionCutoffAppliesTheDocumentedGrace 一对一对应）。
func collectionCutoff(now time.Time) time.Time {
	return now.Add(-CollectionGraceDays * 24 * time.Hour)
}

// collectionOverdueDays 那张单逾期了几天（整数除法向下取整：差满一天不算多一天）。
//
// 入参的成立条件由调用方保证 —— RunOnce 那两条自检挡掉了"读不出账期"与"还在宽限期内"，
// 所以这里能拿到的一定是一张算得出天数的单。刻意不再判第二遍（也不判负数）：
// 同一件事在三处各判一份，改一处漏两处是必然；而催款正文那一行
// （collectionReminderBody 里的 DueAt.Format）本来就没有这道判据 —— 一处判一处不判，
// 读代码的人会以为判着的那处防的是真风险。
func collectionOverdueDays(bill *model.Bill, now time.Time) int {
	return int(now.Sub(*bill.DueAt) / (24 * time.Hour))
}

// collectionReminderBody 催款正文：账单行与此刻的纯函数。
//
// 三条各自的理由：
//   - **合计按币种分开列**：同一个人的两张单理论上可以不同币种（报价侧允许），
//     混成一个总数就是给人发一张算错的账单；
//   - **金额走 collectionMoney**：浮点尾巴出现在催款信里是最贵的显示 bug ——
//     客户照着那个数付款，财务照着凭证对账；
//   - **不提内部主键**：报价行号对客户没有含义，写进去只是把内部形状露出去。
func collectionReminderBody(bills []*model.Bill, now time.Time) string {
	var lines []string
	totals := map[string]float64{}
	for _, b := range bills {
		if b == nil || b.DueAt == nil {
			continue
		}
		cur := strings.TrimSpace(b.Currency)
		if cur == "" {
			cur = model.BillCurrencyDefault
		}
		totals[cur] += b.Amount
		lines = append(lines, fmt.Sprintf("· 应收单 %s，金额 %s %s，账期 %s，已逾期 %d 天",
			b.ID, collectionMoney(b.Amount), cur,
			b.DueAt.UTC().Format(collectionDateLayout), collectionOverdueDays(b, now)))
	}
	currencies := make([]string, 0, len(totals))
	for cur := range totals {
		currencies = append(currencies, cur)
	}
	sort.Strings(currencies)
	parts := make([]string, 0, len(currencies))
	for _, cur := range currencies {
		parts = append(parts, collectionMoney(totals[cur])+" "+cur)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("您在我们的 %d 笔应收已逾期未付（合计 %s）：",
		len(lines), strings.Join(parts, " + ")))
	for _, l := range lines {
		sb.WriteString("\n")
		sb.WriteString(l)
	}
	sb.WriteString("\n如已完成付款请忽略本提醒；有疑问请直接回复这条消息。")
	return sb.String()
}

func collectionEscalateReason(bill *model.Bill, now time.Time, days int) string {
	cur := strings.TrimSpace(bill.Currency)
	if cur == "" {
		cur = model.BillCurrencyDefault
	}
	return fmt.Sprintf("应收单 %s（来路报价 %s / 商机 %s）金额 %s %s，账期 %s，截至 %s 已逾期 %d 天，"+
		"越过升级线 %d 天 ⇒ 自动提醒已停止，需人工介入。",
		bill.ID, bill.QuoteID, bill.OpportunityID, collectionMoney(bill.Amount), cur,
		bill.DueAt.UTC().Format(collectionDateLayout), now.UTC().Format(collectionDateLayout),
		days, CollectionEscalateAfterDays)
}

// collectionMoney 按分固定两位。刻意不复用 formatAudienceFloat（那个是 -1 精度，
// 它的场景是配置读数，不是要人照付的数）。
func collectionMoney(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64)
}

// collectionTokenSeq 与纳秒拼出的锁 token：ReleaseLock 只删"值等于我这把 token"的键，
// token 撞车会让一次失败的发出去把**别人**那把窗还掉。
var collectionTokenSeq atomic.Uint64

func collectionToken() string {
	return strconv.FormatUint(collectionTokenSeq.Add(1), 10) + "-" +
		strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
}
