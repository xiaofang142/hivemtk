package service

// W-5 挽回队列消费 worker（新规划任务清单 T-P1-07）
//
// 缺的从来不是查询：`ListReadyForAttempt` 早写好了（repository/customer_rfm.go），
// 条件是 `stage='queued' AND attempts<max_attempts AND (next_attempt_at IS NULL OR <= now)`，
// 排序 `priority ASC, next_attempt_at ASC NULLS FIRST`。缺的是**没有任何调用方去消费它** ——
// 只有只读 HTTP 入口挂着它（controller/recovery_queue.go 的 ListReadyForAttempt），
// 于是队列只进不出：RFM 判定流失 → 入队 → 永久躺在那儿。
//
// 本文件补上那只消费者，并把"外发"这件事拆成四道必须同时成立的约束：
//
//  1. **显式开关**（AC③）：`FF_LTC_RECOVERY_WORKER`，三态 off|shadow|enforce。
//     off（默认）连 goroutine 都不起；shadow 只跑只选路不发的 dry-run；
//     enforce 才真发。布尔真值（`true`/`1`/`on`）一律只到 shadow —— 和 W-1 审批门
//     同一条口径：给真人发短信是不可撤回的动作，必须在 env 里写出 `enforce` 这个词。
//  2. **显式文案**：外发内容只认 `meta_json.content`。队列里**没有任何地方**存着文案
//     （Enqueue 过去不收文案，RFM 自动入队也不写，seed 写的 meta 只有 source/campaign），
//     所以 worker 也不会替它编一句 —— 没有文案的项跳过、不消耗尝试次数。
//     "系统自己想话说"不是本卡的范围，那是 AI 生成内容的合规问题，不该由一个 cron 顺带解决。
//  3. **频控与 DNC**（AC②）：交给 `ProactiveReachService.ReachByCustomer` 里既有的两道
//     （ReachByCustomer 里的 `filterDoNotContactChannels` / `checkCooldown`），worker 不另起一套判重。
//     T-P3-07 后同一出口还有第三道：发送前审批门 `ErrReachApprovalDenied`，处置见 processOne 的分支。
//  4. **单轮上限**（AC④）：`LTC_RECOVERY_WORKER_BATCH`，默认 20。
//
// 一条曾经写着的边界已在 T-P3-07 收口：本 worker 从前**不经过**任何审批门。
// W-1（internal/app/approval_wiring.go）挂在工具执行链的 buildHandler 上，而这里是 cron
// 直调 service，那条链压根不会被走到。现在的口径是：闸门落在 `ReachByCustomer` 内部
// （SetPreSendApprovalChecker，见 proactive_reach.go），所以 cron 与直接 API 两条非工具路径
// 自动同受约束，不需要各自的调用方记得接。判定键用客户身份（one_id 优先）而不是渠道账号 ——
// 本 worker 走的短信/邮箱渠道 accountID 恒为空，用它的白名单判了等于没判，这正是当年
// 拒绝在这里"顺手糊一个假闸门"的理由，那句话仍然成立。
//
// 另一件：**取锁失败必须拒绝发送（fail-closed）**。触达服务自己的冷却 `checkCooldown`
// 在缓存出错时是 fail-open（checkCooldown 里 err != nil 直接返回 true 照发），因为它的定位是
// "少打扰"；worker 的取锁定位是"同一项别发两遍"，缓存出错时放行就是重复外发，
// 所以这里刻意相反 —— 取不到锁的确定性结论就不发。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// 开关与子参数名集中在此：装配日志、运维文档和实际读取的变量名同源，不再各说各话。
const (
	// RecoveryWorkerFlagEnv 主开关，取值 off|shadow|enforce
	RecoveryWorkerFlagEnv = "FF_LTC_RECOVERY_WORKER"
	// RecoveryWorkerBatchEnv 单轮处理上限（AC④）
	RecoveryWorkerBatchEnv = "LTC_RECOVERY_WORKER_BATCH"
	// RecoveryWorkerIntervalEnv 轮询间隔
	RecoveryWorkerIntervalEnv = "LTC_RECOVERY_WORKER_INTERVAL"
	// RecoveryWorkerBackoffEnv 重试退避基数（同时也是无文案项的推后幅度）
	RecoveryWorkerBackoffEnv = "LTC_RECOVERY_WORKER_BACKOFF"
)

// RecoveryWorker* 默认值。
const (
	recoveryWorkerDefaultBatch    = 20
	recoveryWorkerDefaultInterval = 5 * time.Minute
	recoveryWorkerDefaultBackoff  = 24 * time.Hour
	// recoveryWorkerMaxBatch 与 repository.ListReadyForAttempt 的上限一致（超了它自己会回落 50）
	recoveryWorkerMaxBatch = 500
	// recoveryWorkerMinInterval 低于这个值时一轮还没跑完下一轮就起了
	recoveryWorkerMinInterval = 30 * time.Second
	// recoveryWorkerClaimTTL 单项取锁租约：进程中途崩掉时，最迟这么久后可被重新领取
	recoveryWorkerClaimTTL = 10 * time.Minute
	// recoveryCooldownRetryGap 冷却重试在冷却窗口之上再等的余量
	recoveryCooldownRetryGap = 5 * time.Minute
	// recoveryResultMaxLen last_result 列是 varchar(255)：超长会让台账写入本身报错，
	// 于是"记一笔失败"变成"这条彻底没人管了"。留出省略号余量。
	recoveryResultMaxLen = 240
)

const recoveryClaimKeyPrefix = "mtk:recovery:claim:"

// RecoveryWorkerMode worker 运行模式。
type RecoveryWorkerMode string

const (
	RecoveryWorkerModeOff     RecoveryWorkerMode = "off"
	RecoveryWorkerModeShadow  RecoveryWorkerMode = "shadow"
	RecoveryWorkerModeEnforce RecoveryWorkerMode = "enforce"
)

// recoveryReachSender worker 对触达服务的最小依赖面（便于测试替换）。
type recoveryReachSender interface {
	ReachByCustomer(ctx context.Context, req *ProactiveReachRequest) (*ProactiveReachResponse, error)
}

// RecoveryWorkerRoundReport 单轮处理结果（值类型，供日志与 LastReport 读取）。
//
// 字段刻意按"处置"而不是"结果码"分：排障时要回答的是"这一轮有多少条真发出去了、
// 多少条因为没文案/退订/冷却/抢锁失败而没发"，而不是一串错误码。
type RecoveryWorkerRoundReport struct {
	Mode                string    `json:"mode"`
	StartedAt           time.Time `json:"started_at"`
	Scanned             int       `json:"scanned"`
	Sent                int       `json:"sent"`
	SkippedNoContent    int       `json:"skipped_no_content"`
	BlockedByDNC        int       `json:"blocked_by_dnc"`
	BlockedByApproval   int       `json:"blocked_by_approval"`
	BlockedByCooldown   int       `json:"blocked_by_cooldown"`
	Failed              int       `json:"failed"`
	ClaimHeld           int       `json:"claim_held"`
	ClaimUnavailable    int       `json:"claim_unavailable"`
	WouldSend           int       `json:"would_send"`
	WouldFail           int       `json:"would_fail"`
	LedgerWriteFailures int       `json:"ledger_write_failures"`
	ListError           string    `json:"list_error,omitempty"`
}

// RecoveryQueueWorker 挽回队列消费者。
type RecoveryQueueWorker struct {
	queue    *RecoveryQueueService
	reach    recoveryReachSender
	mode     RecoveryWorkerMode
	batch    int
	interval time.Duration
	backoff  time.Duration
	claimTTL time.Duration
	nowFunc  func() time.Time

	// claim 取锁。默认走全局缓存的 SetNX；测试里换成内存实现。
	claim func(ctx context.Context, key string, ttl time.Duration) (bool, error)

	stop      chan struct{}
	wg        sync.WaitGroup
	startOnce sync.Once

	mu          sync.RWMutex
	startedFlag bool
	last        *RecoveryWorkerRoundReport
}

// NewRecoveryQueueWorker 按环境变量装配 worker。
//
// 模式在构造时定死（和 W-1 的闸门一样不做热切）：热切要连同已在跑的轮次一起处理，
// 而"这一轮是按 shadow 还是 enforce 发的"必须从头到尾一致，否则 shadow 轮跑到一半
// 切 enforce 会发出去一半。改模式请改 env 重启。
func NewRecoveryQueueWorker(queue *RecoveryQueueService, reach recoveryReachSender) *RecoveryQueueWorker {
	mode := parseRecoveryWorkerMode(os.Getenv(RecoveryWorkerFlagEnv))
	backoff := envDurationOr(RecoveryWorkerBackoffEnv, recoveryWorkerDefaultBackoff)
	// 退避基数必须盖过触达冷却窗口：否则下一条到期了、冷却键却还没过期，
	// 每次到期都只换来一次 cooldown 拒绝，白耗一轮还看不出问题。
	minBackoff := reachCooldownWindow + recoveryCooldownRetryGap
	if backoff < minBackoff {
		logger.Warnf("[RecoveryWorker] ⚠️ %s=%s 小于触达冷却窗口（%s）⇒ 抬到 %s，"+
			"否则每次到期都会先撞冷却、白耗一轮",
			RecoveryWorkerBackoffEnv, backoff, reachCooldownWindow, minBackoff)
		backoff = minBackoff
	}
	interval := envDurationOr(RecoveryWorkerIntervalEnv, recoveryWorkerDefaultInterval)
	if interval < recoveryWorkerMinInterval {
		logger.Warnf("[RecoveryWorker] ⚠️ %s=%s 小于 %s ⇒ 抬到 %s（一轮没跑完下一轮就起，同一条会被两轮领）",
			RecoveryWorkerIntervalEnv, interval, recoveryWorkerMinInterval, recoveryWorkerMinInterval)
		interval = recoveryWorkerMinInterval
	}
	return &RecoveryQueueWorker{
		queue:    queue,
		reach:    reach,
		mode:     mode,
		batch:    envIntOr(RecoveryWorkerBatchEnv, recoveryWorkerDefaultBatch, 1, recoveryWorkerMaxBatch),
		interval: interval,
		backoff:  backoff,
		claimTTL: recoveryWorkerClaimTTL,
		nowFunc:  time.Now,
		claim:    cacheSetNXClaim,
		stop:     make(chan struct{}),
	}
}

func cacheSetNXClaim(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return cache.GetGlobalCache().SetNX(ctx, key, "1", ttl)
}

// parseRecoveryWorkerMode 解析开关值；认不得的值一律 off（并告警），
// 布尔真值只到 shadow，理由见文件头。
func parseRecoveryWorkerMode(raw string) RecoveryWorkerMode {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "":
		return RecoveryWorkerModeOff
	case "off", "false", "0", "no", "n", "none", "disabled":
		return RecoveryWorkerModeOff
	case "shadow", "observe", "watch", "log", "report", "dry_run", "dryrun":
		return RecoveryWorkerModeShadow
	case "enforce", "block", "active", "on_send", "send":
		return RecoveryWorkerModeEnforce
	case "on", "yes", "y", "true", "1":
		logger.Warnf("[RecoveryWorker] ⚠️ %s=%q 是布尔真值：不足以表达\"给这批流失客户真发短信\"⇒ 按 shadow 处理。"+
			"要真发请显式写 enforce，可用值：off|shadow|enforce", RecoveryWorkerFlagEnv, raw)
		return RecoveryWorkerModeShadow
	}
	if b, err := strconv.ParseBool(v); err == nil {
		if b {
			logger.Warnf("[RecoveryWorker] ⚠️ %s=%q 解析为布尔真值 ⇒ 按 shadow 处理（真发须显式写 enforce）",
				RecoveryWorkerFlagEnv, raw)
			return RecoveryWorkerModeShadow
		}
		return RecoveryWorkerModeOff
	}
	logger.Warnf("[RecoveryWorker] %s=%q 无法识别 ⇒ 按 off 处理（worker 不启动）；可用值：off|shadow|enforce",
		RecoveryWorkerFlagEnv, raw)
	return RecoveryWorkerModeOff
}

func envDurationOr(key string, def time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	if d, err := time.ParseDuration(raw); err == nil && d > 0 {
		return d
	}
	logger.Warnf("[RecoveryWorker] %s=%q 不是合法时长 ⇒ 用默认 %s", key, raw, def)
	return def
}

func envIntOr(key string, def, min, max int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		logger.Warnf("[RecoveryWorker] %s=%q 不是整数 ⇒ 用默认 %d", key, raw, def)
		return def
	}
	if n < min || n > max {
		logger.Warnf("[RecoveryWorker] %s=%d 超出 [%d,%d] ⇒ 用默认 %d", key, n, min, max, def)
		return def
	}
	return n
}

// Start 启动轮询（幂等）。off 模式什么都不起 —— AC③ 要的正是这个分支。
func (w *RecoveryQueueWorker) Start(ctx context.Context) {
	w.startOnce.Do(func() {
		if w.mode == RecoveryWorkerModeOff {
			logger.Infof("[RecoveryWorker] %s=off ⇒ 未启动：挽回队列只进不出，到期项不外发，"+
				"入口仍只有只读 HTTP（要开始消费请把值改成 shadow 观察一轮，再 enforce 放量）",
				RecoveryWorkerFlagEnv)
			return
		}
		if w.reach == nil {
			logger.Warnf("[RecoveryWorker] ⚠️ 未注入触达服务 ⇒ 不启动（mode=%s）", w.mode)
			return
		}
		w.mu.Lock()
		w.startedFlag = true
		w.mu.Unlock()
		w.wg.Add(1)
		go w.loop(ctx)
		if !cache.GlobalIsRedis() {
			logger.Warnf("[RecoveryWorker] ⚠️ 全局缓存不是 Redis（当前是进程内内存缓存）⇒ "+
				"取锁只在单进程内有效；多副本部署时同一条队列项会被各副本各领一次。"+
				"多副本跑请把 Redis 配上（redis 配置见 .env）。mode=%s", w.mode)
		}
		logger.Infof("[RecoveryWorker] 已启动：mode=%s 间隔=%s 单轮上限=%d 退避基数=%s 取锁租约=%s "+
			"（首轮在 %s 后才跑，启动瞬间不发消息）",
			w.mode, w.interval, w.batch, w.backoff, w.claimTTL, w.interval)
	})
}

// Stop 停止（幂等，等待进行中的轮次收尾）。
func (w *RecoveryQueueWorker) Stop(_ context.Context) {
	select {
	case <-w.stop:
		return
	default:
		close(w.stop)
	}
	w.wg.Wait()
	w.mu.Lock()
	w.startedFlag = false
	w.mu.Unlock()
	logger.Info("[RecoveryWorker] 已停止")
}

// Running worker 的轮询协程是否在跑（AC③ 的断言点）。
func (w *RecoveryQueueWorker) Running() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.startedFlag
}

// Mode 当前生效模式。
func (w *RecoveryQueueWorker) Mode() RecoveryWorkerMode { return w.mode }

// LastReport 最近一轮的处置统计；从未跑过则为 nil。
func (w *RecoveryQueueWorker) LastReport() *RecoveryWorkerRoundReport {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.last == nil {
		return nil
	}
	copied := *w.last
	return &copied
}

func (w *RecoveryQueueWorker) setLast(r *RecoveryWorkerRoundReport) {
	w.mu.Lock()
	w.last = r
	w.mu.Unlock()
}

func (w *RecoveryQueueWorker) loop(ctx context.Context) {
	defer w.wg.Done()
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-w.stop:
			return
		case <-ticker.C:
			if _, err := w.RunOnce(ctx); err != nil {
				logger.Ctx(ctx).Error().Err(err).Msg("[RecoveryWorker] 本轮执行失败")
			}
		}
	}
}

// RunOnce 跑一轮：取到期项 → 逐条处置 → 汇总留痕。
//
// 返回的 error 只代表"这一轮整体没跑起来"（列不到到期项），单条的失败都记在报告里。
func (w *RecoveryQueueWorker) RunOnce(ctx context.Context) (*RecoveryWorkerRoundReport, error) {
	report := &RecoveryWorkerRoundReport{Mode: string(w.mode), StartedAt: w.nowFunc()}
	if w.queue == nil {
		report.ListError = "queue service not wired"
		w.setLast(report)
		return report, errors.New("recovery worker: 队列服务未注入")
	}
	if w.mode == RecoveryWorkerModeOff {
		report.ListError = "worker mode off"
		w.setLast(report)
		return report, nil
	}
	items, err := w.queue.ListReadyForAttempt(ctx, w.batch)
	if err != nil {
		report.ListError = err.Error()
		w.setLast(report)
		return report, fmt.Errorf("recovery worker: 拉取到期项失败: %w", err)
	}
	report.Scanned = len(items)
	for _, item := range items {
		w.processItem(ctx, item, report)
	}
	w.setLast(report)
	w.logRound(report)
	return report, nil
}

func (w *RecoveryQueueWorker) logRound(r *RecoveryWorkerRoundReport) {
	logger.Infof("[RecoveryWorker] 本轮结束 mode=%s 到期=%d 已发=%d 无文案跳过=%d 退订终止=%d "+
		"冷却推后=%d 失败记账=%d 锁被占=%d 取锁不可用=%d 台账写失败=%d%s",
		r.Mode, r.Scanned, r.Sent, r.SkippedNoContent, r.BlockedByDNC, r.BlockedByCooldown,
		r.Failed, r.ClaimHeld, r.ClaimUnavailable, r.LedgerWriteFailures, r.shadowSuffix())
}

func (r *RecoveryWorkerRoundReport) shadowSuffix() string {
	if r.Mode != string(RecoveryWorkerModeShadow) {
		return ""
	}
	return fmt.Sprintf(" （shadow：预计可发=%d 预计失败=%d，均**未发出、未记账**）", r.WouldSend, r.WouldFail)
}

func (w *RecoveryQueueWorker) processItem(ctx context.Context, item *model.RecoveryQueue, r *RecoveryWorkerRoundReport) {
	if item == nil {
		return
	}
	msg, err := parseRecoveryMeta(item.MetaJSON)
	if err != nil || msg.Content == "" {
		w.skipWithoutContent(ctx, item, r, err)
		return
	}

	req := &ProactiveReachRequest{
		CustomerID:        item.CustomerID,
		OneID:             item.UnifiedID,
		Content:           msg.Content,
		Subject:           msg.Subject,
		TemplateID:        msg.TemplateID,
		Params:            msg.Params,
		PreferredChannels: msg.PreferredChannels,
	}

	if w.mode == RecoveryWorkerModeShadow {
		w.shadowPass(ctx, item, req, r)
		return
	}

	claimed, claimErr := w.tryClaim(ctx, item.ID)
	if claimErr != nil {
		// fail-closed：见文件头。宁可不发，也不能在"不知道有没有人正在发"的时候发。
		r.ClaimUnavailable++
		logger.Ctx(ctx).Warn().Err(claimErr).Uint64("recovery_id", item.ID).
			Msg("[RecoveryWorker] 取锁不可用 ⇒ 本条本轮不处理（缓存不是 Redis 或已宕）")
		return
	}
	if !claimed {
		r.ClaimHeld++
		return
	}

	resp, sendErr := w.reach.ReachByCustomer(ctx, req)
	switch {
	case sendErr == nil:
		r.Sent++
		attempt := item.Attempts + 1
		stage, delay := w.afterSendStage(item, attempt)
		// 文案里不含收件人，日志只记 customer_id 与渠道；手机号/邮箱不进日志。
		logger.Ctx(ctx).Info().Uint64("recovery_id", item.ID).
			Str("customer_id", item.CustomerID).Str("channel", resp.Channel).
			Int("attempt", attempt).Int("max_attempts", item.MaxAttempts).Str("stage", stage).
			Msg("[RecoveryWorker] 挽回触达已发出")
		w.recordAttempt(ctx, r, item.ID, resp.Channel, "sent:"+resp.MessageID, stage, delay)

	case errors.Is(sendErr, ErrDoNotContact):
		// 终止：退订不会自己过期，重试只会一直撞同一道墙，且每撞一次就多一次外发尝试记录。
		r.BlockedByDNC++
		logger.Ctx(ctx).Warn().Err(sendErr).Uint64("recovery_id", item.ID).
			Str("customer_id", item.CustomerID).
			Msg("[RecoveryWorker] 命中全局退订 ⇒ 本条终止（cancelled，不再重试）")
		w.recordAttempt(ctx, r, item.ID, "", "blocked_do_not_contact", model.RecoveryStageCancelled, 0)

	case errors.Is(sendErr, ErrReachApprovalDenied):
		// 授权是**可补的**（与退订相反）⇒ 既不终止也不烧尝试次数：留在 queued，按 worker
		// 自己的节奏再来一次。这里刻意不写 last_result：与冷却分支同一条口径——什么都没发出去
		// 就不往外发台账里落字，被拦的原因由日志与本行计数承载。
		//
		// 走 default 会怎样：attempts 被三次烧光 → stage=failed，一个字节都没发出去的客户
		// 被记成"挽回失败"，而失败计数正是排障时用来发现真实发送故障的信号。
		r.BlockedByApproval++
		logger.Ctx(ctx).Warn().Err(sendErr).Uint64("recovery_id", item.ID).
			Str("customer_id", item.CustomerID).
			Msg("[RecoveryWorker] 命中发送前审批门 ⇒ 本条本轮不发、不记尝试（补授权后仍可发）")
		w.deferOnly(ctx, r, item, w.backoff, "blocked_approval")

	case errors.Is(sendErr, ErrReachCooldown):
		// 什么都没发出去 ⇒ 不消耗尝试次数，只推离队首（同无文案项，见 DeferAttempt 注释）。
		r.BlockedByCooldown++
		w.deferOnly(ctx, r, item, reachCooldownWindow+recoveryCooldownRetryGap, "blocked_cooldown")

	default:
		r.Failed++
		attempt := item.Attempts + 1
		stage := model.RecoveryStageQueued
		if attempt >= item.MaxAttempts {
			stage = model.RecoveryStageFailed
		}
		logger.Ctx(ctx).Warn().Err(sendErr).Uint64("recovery_id", item.ID).
			Str("customer_id", item.CustomerID).Int("attempt", attempt).Str("stage", stage).
			Msg("[RecoveryWorker] 触达失败 ⇒ 记一笔尝试并按退避重试")
		w.recordAttempt(ctx, r, item.ID, "", "error:"+sendErr.Error(), stage, w.backoffFor(attempt))
	}
}

// shadowPass 观察轮：走 dry-run，只统计不写台账。
//
// ProactiveReachRequest.DryRun 在 ReachByCustomer 里短路于 cooldown 的 SetNX **之前**
// （ReachByCustomer 里 DryRun 分支在 checkCooldown 之前），所以整条路只有读、没有写，
// 也不会占用冷却窗口 —— 这是 shadow 能反复跑的前提。
func (w *RecoveryQueueWorker) shadowPass(ctx context.Context, item *model.RecoveryQueue, req *ProactiveReachRequest, r *RecoveryWorkerRoundReport) {
	req.DryRun = true
	resp, err := w.reach.ReachByCustomer(ctx, req)
	if err != nil {
		r.WouldFail++
		kind := "other"
		switch {
		case errors.Is(err, ErrDoNotContact):
			kind = "do_not_contact"
			r.BlockedByDNC++
		case strings.Contains(err.Error(), "no channel identity"):
			kind = "no_channel_identity"
		case strings.Contains(err.Error(), "no active account"):
			kind = "no_active_account"
		}
		logger.Ctx(ctx).Warn().Err(err).Uint64("recovery_id", item.ID).
			Str("customer_id", item.CustomerID).Str("would_fail_reason", kind).
			Msg("[RecoveryWorker] shadow：本条转 enforce 后会失败（未发出、未记账）")
		return
	}
	r.WouldSend++
	logger.Ctx(ctx).Info().Uint64("recovery_id", item.ID).
		Str("customer_id", item.CustomerID).Str("channel", resp.Channel).Str("strategy", resp.Strategy).
		Msg("[RecoveryWorker] shadow：本条转 enforce 后会外发（未发出、未记账）")
}

// afterSendStage 发成功之后怎么摆这条队列项。
//
// 还有下次机会 ⇒ 留在 queued，按退避再来一次（挽回本就是多次触达，最多 max_attempts 次）。
// 机会用尽 ⇒ running。它是"已触达、等结果"的既有语义：既不在到期集里
// （ListReadyForAttempt 只要 queued），也仍被 GetActiveByCustomerID 认作活跃
// （queued|running 都算），所以客户不会因此被重复入队，等 MarkRecovered 收尾。
// 刻意不写 succeed：发出去 ≠ 挽回了。
func (w *RecoveryQueueWorker) afterSendStage(item *model.RecoveryQueue, attempt int) (string, time.Duration) {
	if attempt >= item.MaxAttempts {
		return model.RecoveryStageRunning, 0
	}
	return model.RecoveryStageQueued, w.backoffFor(attempt)
}

// backoffFor 指数退避：base × 2^(尝试次数-1)。
func (w *RecoveryQueueWorker) backoffFor(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	shift := attempt - 1
	if shift > 6 {
		shift = 6 // 防止位运算溢出成负数（≈ 64×base 已经足够长）
	}
	return w.backoff << shift
}

// recoveryClaimKey 单项取锁键。抽出来是为了让测试能断言"键按项隔离"，
// 而不是把键格式抄一份在测试里（抄的那份一旦漂移就是假绿）。
func recoveryClaimKey(id uint64) string {
	return fmt.Sprintf("%s%d", recoveryClaimKeyPrefix, id)
}

// tryClaim 领取单项。
//
// 不主动释放：领完之后台账已经把这条推出了到期集（排到 next_attempt_at 或转终态），
// 再放锁只会让另一副本在同一条上再来一次。真正需要放锁的情形是"台账写失败"，
// 而那时恰恰必须让租约挡着 —— 消息已经发出去了，重发比漏发更糟。
func (w *RecoveryQueueWorker) tryClaim(ctx context.Context, id uint64) (bool, error) {
	if w.claim == nil {
		return false, errors.New("claim 未装配")
	}
	return w.claim(ctx, recoveryClaimKey(id), w.claimTTL)
}

// recordAttempt 写台账；写失败只计数不抛出（本轮已发生的动作不会因为记账失败而回滚，
// 下一条也该继续处理 —— 一条记账失败不该让整轮停摆）。
func (w *RecoveryQueueWorker) recordAttempt(ctx context.Context, r *RecoveryWorkerRoundReport, id uint64, channel, result, stage string, delay time.Duration) {
	if err := w.queue.MarkAttempt(ctx, id, channel, truncateRecoveryResult(result), stage, delay); err != nil {
		r.LedgerWriteFailures++
		logger.Ctx(ctx).Error().Err(err).Uint64("recovery_id", id).Str("stage", stage).
			Msg("[RecoveryWorker] 台账写入失败 ⇒ 该条下次到期仍会被领（本条已发的不会重发：取锁租约还在）")
	}
}

func (w *RecoveryQueueWorker) deferOnly(ctx context.Context, r *RecoveryWorkerRoundReport, item *model.RecoveryQueue, delay time.Duration, reason string) {
	if err := w.queue.DeferAttempt(ctx, item.ID, delay); err != nil {
		r.LedgerWriteFailures++
		logger.Ctx(ctx).Error().Err(err).Uint64("recovery_id", item.ID).Str("reason", reason).
			Msg("[RecoveryWorker] 推后失败 ⇒ 该条仍在队首（不消耗尝试次数）")
		return
	}
	logger.Ctx(ctx).Info().Uint64("recovery_id", item.ID).Str("customer_id", item.CustomerID).
		Str("reason", reason).Dur("deferred_by", delay).
		Msg("[RecoveryWorker] 本条推后重试（不消耗尝试次数）")
}

func (w *RecoveryQueueWorker) skipWithoutContent(ctx context.Context, item *model.RecoveryQueue, r *RecoveryWorkerRoundReport, parseErr error) {
	r.SkippedNoContent++
	reason := "no_content"
	if parseErr != nil {
		reason = "bad_meta_json"
	}
	logger.Ctx(ctx).Warn().Uint64("recovery_id", item.ID).Str("customer_id", item.CustomerID).
		Str("reason", reason).Err(parseErr).
		Msg("[RecoveryWorker] 队列项没有可用文案 ⇒ 跳过（worker 不代为编文案），推离队首")
	w.deferOnly(ctx, r, item, w.backoff, reason)
}

func truncateRecoveryResult(s string) string {
	if len(s) <= recoveryResultMaxLen {
		return s
	}
	// 按字节砍会劈开 UTF-8 字符，PG 直接报 invalid byte sequence ⇒ 回退到合法边界。
	cut := recoveryResultMaxLen
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// RecoveryMessage 队列项 meta_json 里的外发文案约定。
//
// 只有这些键被 worker 读取；其它键（seed 写的 source/campaign 之类）原样保留、互不影响。
// content 是唯一必填项：没有它就等于"这条还没准备好要发什么"，worker 跳过而不是替它想一句。
type RecoveryMessage struct {
	Content           string            `json:"content,omitempty"`
	Subject           string            `json:"subject,omitempty"`
	TemplateID        string            `json:"template_id,omitempty"`
	Params            map[string]string `json:"params,omitempty"`
	PreferredChannels []string          `json:"preferred_channels,omitempty"`
}

func (m *RecoveryMessage) isEmpty() bool {
	return m.Content == "" && m.Subject == "" && m.TemplateID == "" &&
		len(m.Params) == 0 && len(m.PreferredChannels) == 0
}

// parseRecoveryMeta 解析 meta_json。空串/非法 JSON 都返回一个空文案（后者带 error），
// 调用方按"没有可用文案"处理 —— 解析失败绝不能被当成"内容为空所以跳过"之外的第二种情况，
// 否则会有一条坏 JSON 让该项永久卡在队首。
func parseRecoveryMeta(raw string) (*RecoveryMessage, error) {
	if strings.TrimSpace(raw) == "" {
		return &RecoveryMessage{}, nil
	}
	var m RecoveryMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return &RecoveryMessage{}, fmt.Errorf("meta_json 解析失败: %w", err)
	}
	return &m, nil
}

// encodeRecoveryMeta 写入 meta_json；全空时返回空串，让 GORM 省略该列、落到列默认值 '{}'。
func encodeRecoveryMeta(m *RecoveryMessage) (string, error) {
	if m == nil || m.isEmpty() {
		return "", nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("meta_json 序列化失败: %w", err)
	}
	return string(b), nil
}
