package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
)

// 开户登记自动补偿扫描（PLT-03）。
//
// ============================================================
// 为什么必须真的有这个任务
//
// RegisterTenant 上报失败只把 platform_sync 置 pending/failed，此后再无人过问；
// 补同步只有管理端「立即重试」按钮（RetryRegistrationSync）——没人点按钮，
// 失败就永远静默。后果不是"少一行日志"：合规登记、平台端 merchants 镜像、
// 租户维度的钱包/订阅/积分行（平台端以 (merchant_key, tenant_code) 建行）
// 全都挂在这份登记上，随时间缺漏，且界面上看不出任何差别。
//
// 本任务把「重报」从依赖人手变成周期兜底：扫描 pending/failed 逐行重报
// SubmitRegistrationToPlatform。与手动按钮并存不互斥（同一函数、同一状态推进）。
// ============================================================

// tenantRegCompensationInterval 补偿扫描周期。
//
// 取 10 分钟，与模块下发同步（moduleSyncInterval）对齐：登记补偿同样是
// "低频但要尽快收敛"的操作，10 分钟内失败的开户最迟 10 分钟后重报，
// 而更短的周期会把一次 HMAC 往返放大到每天 144+ 次。
const tenantRegCompensationInterval = 10 * time.Minute

// tenantRegCompensationBatch 单轮扫描的行数上限。
//
// 行数异常多（平台长时间不可达后恢复）时单轮有界，余下行下一轮接续，
// 避免一轮扫出无界长的串行 HMAC 往返。当前规模（个位数租户）远够。
const tenantRegCompensationBatch = 200

// 退避参数：base=一个扫描周期，指数 ×2，封顶 6 小时。
//
// 连续失败的租户不值得每个周期都撞一次平台：第 1 次失败后下轮（10min）照常重试，
// 之后 20min/40min/80min/… 最长 6h 一试。封顶取 6h 而不是更大——登记是合规义务，
// "最终补上"比"彻底安静"重要，6h 意味着持续故障期间每行每天仍有 4 次重报。
const (
	tenantRegBackoffBase    = tenantRegCompensationInterval
	tenantRegBackoffMax     = 6 * time.Hour
	tenantRegBackoffFailure = 5 // 每第 5 次连续失败才打一行 Warn（错误文本本身已落 platform_sync_err）
)

// regCompensationResult 一轮扫描的读数（测试断言与启动日志用）。
type regCompensationResult struct {
	// Scanned 取到的待补偿行数（pending+failed）
	Scanned int
	// Attempted 实际重报次数（扣掉退避未到点的跳过）
	Attempted int
	// Succeeded 重报成功并置 synced 的行数
	Succeeded int
	// BackoffSkipped 因连续失败退避未到点而跳过的行数
	BackoffSkipped int
	// ZeroEgress off 模式直返（未查库、未出站）
	ZeroEgress bool
}

// tenantRegCompensator 补偿扫描器：取行 → 逐行重报 → 记退避。
//
// 抽成结构体而不是塞进 runner：退避计数是**扫描器**的状态，与"定时器归谁停"
// 是两回事；测试也要能绕开定时器直接驱动单轮（真等 10 分钟不现实）。
//
// 失败计数沿现状选型留进程内存，不新增表/列（清单 §3.14 明示）：持久化的
// 失败留痕由既有 platform_sync_err 列承担（每次重报失败都会刷新，重启不丢），
// 内存计数只承担"这轮该不该试"的节流判断，丢了也只是退一步重头计数。
type tenantRegCompensator struct {
	svc *TenantService

	mu          sync.Mutex
	failCount   map[uint]int       // 租户 ID → 连续失败次数
	nextAttempt map[uint]time.Time // 租户 ID → 最早下次尝试时刻（指数退避）
	now         func() time.Time   // 测试注时；生产恒 time.Now
}

func newTenantRegCompensator() *tenantRegCompensator {
	return &tenantRegCompensator{
		svc:         NewTenantService(),
		failCount:   map[uint]int{},
		nextAttempt: map[uint]time.Time{},
		now:         time.Now,
	}
}

// ScanOnce 执行一轮补偿扫描。
//
// off 模式（PLATFORM_ENABLED 未开启）直接返回 ZeroEgress：不查库、不构造任何
// 请求 —— 离线部署的登记本来就是 skipped，一个出站帧都不许有（与
// SubmitRegistrationToPlatform 的 off 分支同一开关，但扫描器必须**先于取库**判，
// 否则 off 模式会把 pending/failed 行错误地改写成 skipped）。
func (c *tenantRegCompensator) ScanOnce(ctx context.Context) (regCompensationResult, error) {
	if !config.PlatformEnabled() {
		return regCompensationResult{ZeroEgress: true}, nil
	}
	rows, err := c.svc.tenantRepo.ListForRegistrationRetry(ctx, tenantRegCompensationBatch)
	if err != nil {
		return regCompensationResult{}, err
	}
	res := regCompensationResult{Scanned: len(rows)}
	now := c.now()
	for _, t := range rows {
		if !c.backoffAllows(t.ID, now) {
			res.BackoffSkipped++
			continue
		}
		res.Attempted++
		// admin 入参传 nil：该参数从未被消费（2026-11-10 二审已核
		// SubmitRegistrationToPlatform 全文，payload 只由租户行现值 + install
		// 身份构成）。收窄签名属签名清理债务，不掺进本刀；nil 与手动路径
		// 传零值 SystemUser 语义完全一致。
		_ = c.svc.SubmitRegistrationToPlatform(ctx, t, nil)
		// 提交函数的失败分支自吞错误恒返 nil（注册路径"失败不回滚"的历史口径，
		// 返回值语义不能为本刀擅改）。成败以行上状态读回判定：synced=成功；
		// 其余（failed/pending）按失败记账，错误文本顺带取自 platform_sync_err
		// ——它正是持久化的失败留痕，重启不丢。
		after, gerr := c.svc.tenantRepo.GetByID(ctx, t.ID)
		if gerr != nil {
			c.recordFailure(t.ID, now, gerr)
			continue
		}
		if after.PlatformSync == model.TenantSyncSynced {
			c.recordSuccess(t.ID)
			res.Succeeded++
			continue
		}
		c.recordFailure(t.ID, now, errors.New(after.PlatformSyncErr))
	}
	if res.Attempted > 0 {
		logger.Infof("[reg-compensation] 补偿扫描一轮：待补 %d，重报 %d，成功 %d，退避跳过 %d",
			res.Scanned, res.Attempted, res.Succeeded, res.BackoffSkipped)
	}
	return res, nil
}

// backoffAllows 判断该租户的退避是否已到点。
func (c *tenantRegCompensator) backoffAllows(id uint, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	next, ok := c.nextAttempt[id]
	return !ok || !now.Before(next)
}

// recordFailure 连续失败计数 +1，并按指数退避排定下次尝试。
//
// 日志节流：每次失败 SubmitRegistrationToPlatform 已自带一行 Warn（含错误文本，
// 且 platform_sync_err 落库），这里只在首败与每第 5 次连续失败时补一行带退避
// 信息的 Warn，避免持续故障期间每 10 分钟刷两条重复告警。
func (c *tenantRegCompensator) recordFailure(id uint, now time.Time, err error) {
	c.mu.Lock()
	c.failCount[id]++
	count := c.failCount[id]
	delay := tenantRegBackoffBase << uint(count-1)
	if delay > tenantRegBackoffMax || delay <= 0 { // <=0 防移位溢出回绕
		delay = tenantRegBackoffMax
	}
	c.nextAttempt[id] = now.Add(delay)
	c.mu.Unlock()
	if count == 1 || count%tenantRegBackoffFailure == 0 {
		logger.Warnf("[reg-compensation] 租户 %d 开户登记连续重报失败 %d 次，下次尝试 %s 后（错误已落 platform_sync_err）: %v",
			id, count, delay, err)
	}
}

// recordSuccess 重报成功：清掉该租户的退避记账。
func (c *tenantRegCompensator) recordSuccess(id uint) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.failCount, id)
	delete(c.nextAttempt, id)
}

// tenantRegCompensationRunner 开户登记补偿扫描循环。
//
// 结构沿 module_sync_runner（首轮同步执行 + SafeGo ticker + Stop 有确切归属），
// 便于单测用 WaitFirstSync 驱动首轮、用短周期语义直呼 ScanOnce 驱动后续轮次。
type tenantRegCompensationRunner struct {
	cancel    context.CancelFunc
	done      chan struct{}
	firstDone chan struct{}
}

// StartTenantRegistrationCompensation 启动开户登记补偿扫描后台任务。
//
// 首轮**同步执行**（不是丢进 goroutine）：与模块同步同一理由 —— 服务刚起来
// 的时刻正是"上次进程没跑完的失败行"最该被补上的时刻；首轮失败不阻断启动
// （平台不可达是常态，失败行退避后下轮再试）。
//
// off 模式（PLATFORM_ENABLED 未开启）整个不启动：不查库、不起协程，
// 返回的 runner 两个通道均已关闭（Stop 幂等可用）。离线部署没有"登记补偿"
// 这个概念，也没有任何一条出站路径可走。
func StartTenantRegistrationCompensation(parent context.Context) *tenantRegCompensationRunner {
	if !config.PlatformEnabled() {
		logger.Info("[reg-compensation] 平台未启用（离线部署），登记补偿扫描不启动")
		r := &tenantRegCompensationRunner{
			done:      make(chan struct{}),
			firstDone: make(chan struct{}),
		}
		close(r.done)
		close(r.firstDone)
		return r
	}

	ctx, cancel := context.WithCancel(parent)
	// 扫描在请求之外（Background 派生）：挂 SystemScope 声明系统作用域，
	// 与 module-sync 同一模式 —— tenants 本身是全局表，此包裹属防御性
	// （扫描若未来扩到租户表也不会依赖请求残留的租户绑定）。
	ctx = tenant.WithSystemScope(ctx)
	r := &tenantRegCompensationRunner{
		cancel:    cancel,
		done:      make(chan struct{}),
		firstDone: make(chan struct{}),
	}
	comp := newTenantRegCompensator()

	// 首轮扫描：失败不阻断启动（与模块同步同一口径）
	if _, err := comp.ScanOnce(ctx); err != nil {
		logger.Warnf("[reg-compensation] 首轮扫描失败（退避后下轮再试）: %v", err)
	}
	close(r.firstDone)

	utils.SafeGo(ctx, "tenant.reg.compensation.loop", func(ctx context.Context) {
		defer close(r.done)
		ticker := time.NewTicker(tenantRegCompensationInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := comp.ScanOnce(ctx); err != nil {
					logger.Warnf("[reg-compensation] 周期扫描失败: %v", err)
				}
			}
		}
	})

	logger.Infof("[reg-compensation] 开户登记补偿扫描已启动，周期 %s", tenantRegCompensationInterval)
	return r
}

// Stop 停止扫描循环并等待退出
func (r *tenantRegCompensationRunner) Stop() {
	if r == nil {
		return
	}
	if r.cancel != nil {
		r.cancel()
	}
	select {
	case <-r.done:
	case <-time.After(3 * time.Second):
		logger.Warn("[reg-compensation] 停止等待超时（3s），协程可能仍在收尾")
	}
}

// WaitFirstSync 等待首轮扫描完成（测试用）
func (r *tenantRegCompensationRunner) WaitFirstSync() { <-r.firstDone }
