package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/browser_automation/dto"
	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/browser_automation/platform"
	"hivemtk-user/internal/browser_automation/repository"
	"hivemtk-user/internal/pkg/utils/logger"
)

// StepParams 步骤扩展参数（wait/scroll/extract/type 等），落库到 params JSONB
type StepParams struct {
	Ms            int               `json:"ms,omitempty"`
	ClearFirst    bool              `json:"clear_first,omitempty"`
	SubmitOnEnter bool              `json:"submit_on_enter,omitempty"`
	Direction     string            `json:"direction,omitempty"`
	Amount        int               `json:"amount,omitempty"`
	Selector      string            `json:"selector,omitempty"`
	TimeoutMs     int               `json:"timeout_ms,omitempty"`
	Selectors     map[string]string `json:"selectors,omitempty"`
	// click_near 用
	Anchor     string `json:"anchor,omitempty"`
	ButtonText string `json:"button_text,omitempty"`
	// assert/query 用（洞察层）
	AssertKind string `json:"assert_kind,omitempty"` // contains_text / selector_exists
	QueryKind  string `json:"query_kind,omitempty"`  // text / exists / count / attr
	Attribute  string `json:"attribute,omitempty"`   // query attr 用：属性名（D2 贯通）
}

// StepRuntime 步骤编排里的错误处理策略
type StepRuntime struct {
	ContinueOnError bool `json:"continue_on_error"`
	RetryCount      int  `json:"retry_count"`
	RetryBackoffMs  int  `json:"retry_backoff_ms"`
}

// parsedStep 编排 JSON 里的一个步骤
type parsedStep struct {
	dto.StepItem
	ContinueOnError bool
	RetryCount      int
	RetryBackoffMs  int
}

// Executor 执行引擎：steps 解释 + Hand 调用 + Session 流转 + 停止/超时控制。
// 状态收口原则：session/step 状态只由 Executor 写（替代 v1 的 AfterFunc 直改状态）。
type Executor struct {
	hand        *Hand
	sessionRepo repository.BrowserSessionRepository
	stepRepo    repository.BrowserStepRepository
	cmdLogRepo  repository.BrowserCommandLogRepository
	brain       *BrainService
	feedback    *FeedbackService

	// relocateLLM A1 自愈 LLM 接缝（默认 defaultRelocateLLM；测试替换免真机 LLM）
	relocateLLM func(ctx context.Context, systemPrompt, prompt string) (relocateOutcome, error)

	stopMu       sync.Mutex
	stopRegistry map[uint]chan struct{} // sessionID → stopCh

	// confirmRegistry D7：sessionID → 待放行确认通道（仅在 post_comment 提交点前挂起时存在）。
	// 与 stopRegistry 同构但生命周期不同：stop 通道随 session 全程注册，确认通道只在等待期存在
	// ——「存在即挂起」使 ConfirmPending 无需额外状态位。
	confirmMu       sync.Mutex
	confirmRegistry map[uint]chan struct{}

	// 批16（A7）：台账写失败后的两张降级表，与上面两张同构但语义相反——它们是「不能再派发写」的记录。
	//   ledgerBroken: sessionID → 最初那次台账写失败的原因，随会话结束清除（会话级降级）；
	//   ledgerGaps:   "taskID|text_hash" → 不可逆点已跨越但库里没落成，跨会话保留到进程结束
	//                 （自动重试是同进程换新 session 跑同一任务，会话级降级挡不住它）。
	// 两张表都只在「台账写失败」这条罕见路径上增长，各自有界（gaps 见 ledgerGapCap）。
	ledgerMu       sync.Mutex
	ledgerBroken   map[uint]string
	ledgerGaps     map[string]bool
	ledgerGapOrder []string

	// R-A4（2026-09-19）：原 lastStepResult 字段已删——Executor 是进程级单例、
	// 多 session 并发触达，字段传值既是数据竞态又会跨 session 串包；
	// 回包改由 dispatchStep 返回值沿调用栈传递。
}

func NewExecutor(hand *Hand, sessionRepo repository.BrowserSessionRepository, stepRepo repository.BrowserStepRepository, brain *BrainService, feedback *FeedbackService) *Executor {
	return &Executor{
		hand:         hand,
		sessionRepo:  sessionRepo,
		stepRepo:     stepRepo,
		brain:        brain,
		feedback:     feedback,
		relocateLLM:  defaultRelocateLLM,
		stopRegistry: make(map[uint]chan struct{}),

		confirmRegistry: make(map[uint]chan struct{}),

		ledgerBroken: make(map[uint]string),
		ledgerGaps:   make(map[string]bool),
	}
}

// SetCommandLogRepository 日志仓储注入（路由装配可选——nil 时静默跳过埋点）
func (e *Executor) SetCommandLogRepository(r repository.BrowserCommandLogRepository) {
	e.cmdLogRepo = r
}

// appendCommandLog append-only 命令-事件日志（P8）。失败仅告警不阻断执行：
// 日志是审计增强，不能反过来拖垮业务执行路径。
// seq 由调用方传入（session 局部计数）——Executor 是多 session 共享单例，
// 共享计数器在并发 session 下是 data race 且 seq 跨 session 交错（P0-2 修复）。
func (e *Executor) appendCommandLog(ctx context.Context, sessionID, taskID, stepID uint, seq int, direction, action string, payload any, durationMs int64, ok bool) {
	if e.cmdLogRepo == nil {
		return
	}
	blob, err := json.Marshal(payload)
	if err != nil {
		blob = []byte(`{"marshal_error":"` + err.Error() + `"}`)
	}
	entry := &model.BrowserCommandLog{
		SessionID: sessionID, TaskID: taskID, StepID: stepID,
		Seq: seq, Direction: direction, Action: action,
		Payload: blob, DurationMs: durationMs, Ok: ok,
	}
	if err := e.cmdLogRepo.Append(ctx, entry); err != nil {
		logger.Errorf("[BrowserExec] 命令日志落库失败 session=%d seq=%d: %v", sessionID, entry.Seq, err) // P2-3 升级 error
	}
}

// SignalStop 手动中断（POST /sessions/:id/stop）
func (e *Executor) SignalStop(sessionID uint) bool {
	e.stopMu.Lock()
	ch, ok := e.stopRegistry[sessionID]
	e.stopMu.Unlock()
	if !ok {
		return false
	}
	select {
	case <-ch:
	default:
		close(ch)
	}
	return true
}

func (e *Executor) registerStop(sessionID uint) chan struct{} {
	ch := make(chan struct{})
	e.stopMu.Lock()
	e.stopRegistry[sessionID] = ch
	e.stopMu.Unlock()
	return ch
}

func (e *Executor) unregisterStop(sessionID uint) {
	e.stopMu.Lock()
	delete(e.stopRegistry, sessionID)
	e.stopMu.Unlock()
}

func (e *Executor) stopFired(ch chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// stopChFor 读取 session 当前注册的 stop 通道（分发路径不穿 stopCh 参数，finalize 轮询需可中断）。
// 未注册返回 nil——channel(nil) 的接收永远阻塞，调用方须先判空。
func (e *Executor) stopChFor(sessionID uint) chan struct{} {
	e.stopMu.Lock()
	defer e.stopMu.Unlock()
	return e.stopRegistry[sessionID]
}

// ---- D7 写操作人工确认闸门（与 stop 同构，语义相反：stop 取消、confirm 放行）----

// SignalConfirm POST /sessions/:id/confirm 放行。返回 false=该 session 当前没有挂起的确认点
// （未开 require_confirm / 已过提交点 / 已结束）。先摘后关：同一通道只可能被 close 一次。
func (e *Executor) SignalConfirm(sessionID uint) bool {
	e.confirmMu.Lock()
	ch, ok := e.confirmRegistry[sessionID]
	if ok {
		delete(e.confirmRegistry, sessionID)
	}
	e.confirmMu.Unlock()
	if !ok {
		return false
	}
	close(ch)
	return true
}

// ConfirmPending 该 session 是否正停在确认闸门（读侧暴露给前端按钮可见性）
func (e *Executor) ConfirmPending(sessionID uint) bool {
	e.confirmMu.Lock()
	defer e.confirmMu.Unlock()
	_, ok := e.confirmRegistry[sessionID]
	return ok
}

// confirmOutcome D7 闸门三种出路（F4：终态语义必须可区分——「操作者主动不提交」和
// 「确认超时」在审计面同形记 failed，等于把一次正常的人工否决统计成系统故障）。
type confirmOutcome uint

const (
	confirmGranted confirmOutcome = iota
	confirmStoppedByUser
	confirmWaitTimedOut
)

// errConfirmAbortedByStop 挂起期间被 stop 中止的步错误原文。不复用「用户手动中断」那句
// 原文：步与审计面要写清「停在确认闸门、评论从未提交」，终态收口再按此常量精确认出
// 「这次失败的起因就是用户中断」→ 记 stopped。
// 批8 起 post_comment 与派生写步（type+回车 / click 发送按钮）共用此原文——F4 的精确匹配
// 只认这一条，措辞按步分叉就会把「闸门处中止」重新掉回 failed。
const errConfirmAbortedByStop = "写步等待人工确认期间被用户中止，评论未提交"

// waitForConfirm 挂起等人工放行，自带独立计时器（批8 解耦：不再让 execCtx.Done 兼职确认超时）。
// 返回值第二项是**哪条预算到头**的原文（「确认等待 600s」还是「任务执行预算掐断」）——
// 两者在审计面上必须可区分，否则人就会去调 timeout_sec 而真正该调的是 confirm_wait_sec。
// 调用点在不可逆提交点之前，非 confirmGranted 分支从未点击过任何按钮——失败可安全重下发，
// 不违「单次提交禁重试」红线（那是「已提交且结局未知」的专属纪律）。
// stopChFor 未注册时返回 nil，select 对 nil 通道分支永不就绪，与 ctx.Done 并存安全。
func (e *Executor) waitForConfirm(ctx context.Context, sessionID uint, wait time.Duration) (confirmOutcome, string) {
	ch := make(chan struct{})
	e.confirmMu.Lock()
	e.confirmRegistry[sessionID] = ch
	e.confirmMu.Unlock()
	defer func() {
		e.confirmMu.Lock()
		if cur, ok := e.confirmRegistry[sessionID]; ok && cur == ch {
			delete(e.confirmRegistry, sessionID)
		}
		e.confirmMu.Unlock()
	}()

	timer := time.NewTimer(wait)
	defer timer.Stop()
	stopCh := e.stopChFor(sessionID)
	select {
	case <-ch:
		return confirmGranted, ""
	case <-stopCh:
		return confirmStoppedByUser, "用户在确认闸门处中止"
	case <-timer.C:
		return confirmWaitTimedOut, fmt.Sprintf("人工确认等待 %ds", int(wait.Seconds()))
	case <-ctx.Done():
		// 执行预算（TimeoutSec）到头。批8 起它与确认预算是两条独立计时器，谁先到谁说话。
		return confirmWaitTimedOut, "任务执行预算用尽"
	}
}

// sendErrText 提交命令错误的落库文本（nil→空串）
func sendErrText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// cleanupSessionTab 会话收口时关闭自身 tab（F10）。脱离执行 ctx 的取消状态（超时/中止腿的
// ctx 此刻必然已 Done，用它发命令等于注定失败），幂等——tab 已被 close_tab 步或用户关掉时
// 扩展侧吞掉异常返回 ok。回收失败只降级为日志与审计行，绝不改判终态。
func (e *Executor) cleanupSessionTab(baseCtx context.Context, task *model.BrowserTask,
	session *model.BrowserSession, seq *int) {
	if session.ChromeTabID <= 0 {
		return // 没开过 tab（如 Host 离线即失败）——无从回收，也不该发命令
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(baseCtx), sessionTabCleanupBudget)
	defer cancel()
	start := time.Now()
	err := e.hand.closeTab(cctx, task.UserID, session.ChromeTabID)
	*seq++
	e.appendCommandLog(cctx, session.ID, task.ID, 0, *seq, "event", "session_tab_cleanup",
		map[string]any{"tab_id": session.ChromeTabID, "error": sendErrText(err)},
		time.Since(start).Milliseconds(), err == nil)
	if err != nil {
		logger.Warnf("[BrowserExec] session=%d tab=%d 收口回收失败（不影响终态）: %v",
			session.ID, session.ChromeTabID, err)
	}
}

// isInjectTimeout R26-2：识别扩展侧竞速错误（*_inject_timeout_*ms，见 primitives.js raceTimeout）。
// 语义=注入从未在页面执行→该命令零副作用，与「执行了但失败」「WS 超时结果未知」三分归一。
func isInjectTimeout(err error) bool {
	return err != nil && strings.Contains(err.Error(), "_inject_timeout_")
}

// isSendGateReject 扩展侧 comment_send 的可点性闸门在把坐标交给 CDP **之前**就把按钮判死
// （send_button_not_interactable: covered/zero_box/disabled，见 primitives.js injPostCommentSend）。
// 与注入超时同属「零副作用」一类：页面从未收到那次点击，所以台账停在 prepared、
// 不进 finalize 白轮、不自愈重发（换文本重选对一个「存在但不可点」的按钮没有依据）。
func isSendGateReject(err error) bool {
	return err != nil && strings.Contains(err.Error(), "send_button_not_interactable")
}

// ExecuteSession 执行一个 session（在独立 goroutine 中运行）。
// ctx 由调用方包上 task.TimeoutSec 超时。
func (e *Executor) ExecuteSession(ctx context.Context, task *model.BrowserTask, session *model.BrowserSession, steps []parsedStep) {
	stopCh := e.registerStop(session.ID)
	defer e.unregisterStop(session.ID)
	defer e.clearLedgerBroken(session.ID)

	_ = e.sessionRepo.UpdateStatus(ctx, session.ID, "active", "")
	_ = steps // Brain 模式忽略显式编排，由 LLM 生成

	var success, failed int
	sessionFailed := ""
	cmdSeq := 0 // session 局部命令日志计数（P0-2：跨 session 隔离，避免共享计数器 data race）

	if task.BrainMode {
		// Brain 模式：LLM 每轮看快照出 plan → 执行 → done 判定（loop_count 不适用）
		success, failed, sessionFailed = e.executeBrain(ctx, task, session, stopCh)
	} else {
		loopCount := task.LoopCount
		if loopCount < 1 {
			loopCount = 1
		}

	loop:
		for round := 0; round < loopCount; round++ {
			for i, step := range steps {
				// 手动中断
				if e.stopFired(stopCh) {
					sessionFailed = "用户手动中断"
					break loop
				}
				// ctx 超时（task.TimeoutSec）
				if ctx.Err() != nil {
					sessionFailed = fmt.Sprintf("执行超时（%ds）", task.TimeoutSec)
					break loop
				}
				// 步间间隔（可被 stop 打断）
				if i > 0 || round > 0 {
					if !sleepInterruptible(ctx, stopCh, humanizedDelay(task.DelayMs)) {
						sessionFailed = "用户手动中断"
						break loop
					}
				}
				// 轮次 >0 时重新 open_tab 的场景由显式 steps 表达；此处不隐式开 tab
				status, errMsg, _ := e.executeStepWithRetry(ctx, task, session, i, step, stopCh, &cmdSeq)
				switch status {
				case "success":
					success++
				case "skipped":
					// 批7：写步在自动重试轮撞见历史提交尝试 → 整步不下发。两种事实分开判：
					//   前一轮 verified → 目标已达成，本轮只是补完剩余步，不计成败也不中断；
					//   前一轮 sent/unattributed → 提交从未被证明，本轮防双发也无从证明，
					//     必须让本轮判红（否则「重试轮全绿」就是批6 要消灭的那类假绿），
					//     但不 break——后面的只读步照常跑完，现场证据越全越好判。
					if errMsg != "" {
						failed++
						if sessionFailed == "" {
							sessionFailed = errMsg
						}
					}
				case "failed":
					failed++
					if !step.ContinueOnError {
						sessionFailed = errMsg
						// 真机实测（session229：xhs 跳 website-login/error?error_code=300012
						// 「IP存在风险」）：风控页最常见的表现恰恰是「某步定位失败」，
						// 而旧代码在此直接 break 跳过了步后拦截检测，于是风控页被误报成
						// selector_timeout——归因错方向，人工就会去改选择器而不是换网络。
						// 失败收口前先过一次拦截判定，命中即用语义更准的风控归因替换原始错误。
						if blocked, reason := e.detectBlockedIfFatal(ctx, task, session, &cmdSeq); blocked {
							sessionFailed = reason
						}
						break loop
					}
				}
				// 拦截页检测（铁律 4）：成功步后页面可能已跳风控页——disconnect 类终止，全自动闭环
				if blocked, reason := e.detectBlockedIfFatal(ctx, task, session, &cmdSeq); blocked {
					sessionFailed = reason
					break loop
				}
			}
		}
	}

	finalStatus := "completed"
	if sessionFailed != "" {
		finalStatus = "failed"
		// F4：stopped=「用户主动叫停」，与「系统跑失败」是两类事实。D7 挂起期间被 stop
		// 中止同样归 stopped（原文常量精确匹配，不靠字符串模糊匹配）。
		if e.stopFired(stopCh) && (sessionFailed == "用户手动中断" || sessionFailed == errConfirmAbortedByStop) {
			finalStatus = "stopped"
		}
	}
	// R25 真机回归 P0 修复（session188 实测）：executeBrain 因 ctx 超时收敛返回后，
	// ctx 已 Done——用原 ctx 写终态会被 DB 驱动取消，session 永久停留 active（看门狗白兜）。
	// 终态收口必须用脱离取消的 context（独立时限见 timeouts.go，只保写库完成，不继承执行期取消）。
	writeCtx, cancelWrite := context.WithTimeout(context.WithoutCancel(ctx), sessionFinalWriteBudget)
	defer cancelWrite()
	_ = e.sessionRepo.UpdateStatus(writeCtx, session.ID, finalStatus, sessionFailed)
	_ = e.sessionRepo.UpdateMetrics(writeCtx, session.ID, success+failed, success, failed)

	// F10 会话收口必须回收自己打开的 tab。openTab 从不复用（tab-manager.js:6 每次 tabs.create），
	// 而 failed/stopped/超时 三条路径根本走不到编排里末尾的 close_tab 步——真机实测同一
	// Profile 累积 40 个泄漏 tab（cron 任务按周期无限增长，后台 tab 常驻还拖慢 SW）。
	// 「留着给用户看结果」不成立：证据已在审计包（快照/截图/command_log），且崩后
	// chrome_tab_id 本就失效（A9 结论：续跑唯一合法入口是重新 open_tab）。
	e.cleanupSessionTab(ctx, task, session, &cmdSeq)

	// Brain 模式：LLM 总结执行结果落 llm_summary（P2-2：completed 与 failed 都总结——失败归因同样是交付物）
	if e.brain != nil && task.BrainMode && (finalStatus == "completed" || finalStatus == "failed") {
		if summ := e.brain.SummarizeSession(writeCtx, task.ID, session.ID, task.BrainGoal, success, success+failed, string(session.ExtractedData)); summ != "" {
			_ = e.sessionRepo.UpdateArtifacts(writeCtx, session.ID, nil, "", summ)
		}
	}

	// Brain 总结 / 失败重试调度 / 通知 —— FeedbackService 内部异步
	if e.feedback != nil {
		e.feedback.OnSessionFinished(ctx, task, session, finalStatus, success, success+failed)
	}
}

// executeBrain Brain 模式执行循环：兜底开 tab → snapshot → LLM plan → 执行 → done 判定。
// 返回 (success, failed, sessionFailed)；sessionFailed 为空表示目标达成（done）。
// 容错：LLM 编排失败/snapshot 失败不直接终止——先尝试恢复 tab 再重试本轮，
// 连续 brainMaxFailures 次失败才放弃（LLM JSON 抖动与 Host 空闲断连都是瞬态）。
func (e *Executor) executeBrain(ctx context.Context, task *model.BrowserTask, session *model.BrowserSession, stopCh chan struct{}) (int, int, string) {
	if e.brain == nil {
		return 0, 0, "Brain 服务未装配"
	}
	success, failed := 0, 0
	stepIdx := 0
	exceeded := true
	consecutiveFails := 0
	// 执行历史摘要（给 LLM 的跨轮记忆：做过什么、当前进度），防止原地打转
	var history []string
	// reflect 跨轮状态（对标 browser-use evaluation/memory 回喂）
	var prevEvaluation, memory string
	cmdSeq := 0 // session 局部命令日志计数（P0-2）
	// P0-4：LLM 下发参数钳位状态
	consecutiveActionFails := 0
	judgeFailOpen := 0 // P1-2 连续 fail-open 计数
	// F6 历史压缩台账：滑窗溢出条目按动作折叠计数，台账首行回喂 LLM（零 LLM 成本的 browser-use 压缩等价）
	foldedHistory := map[string]int{}
	// P1-1：session 级 token 预算熔断（plan+judge 全计入）
	tokenUsed := 0
	tokenBudget := brainTokenBudget()
	// wall-clock 看门狗（R22）：ctx 取消链在某些 LLM/DB 调用栈不生效（session132 实测 11min+ active），
	// 以真实时钟兜底——超执行预算+30s 强制收敛，会话必有终态。
	// 批8：预算走 taskExecBudget（Brain 模式下 LLM 也能编排出写步，确认挂起同样要留出时长）
	deadline := time.Now().Add(taskExecBudget(task) + taskWatchdogGrace)

	for iter := 0; iter < maxBrainIterations; iter++ {
		if e.stopFired(stopCh) {
			return success, failed, "用户手动中断"
		}
		if ctx.Err() != nil || time.Now().After(deadline) {
			return success, failed, fmt.Sprintf("执行超时（%ds）", int(taskExecBudget(task).Seconds()))
		}
		if iter > 0 && !sleepInterruptible(ctx, stopCh, humanizedDelay(task.DelayMs)) {
			return success, failed, "用户手动中断"
		}
		// 兜底开 tab：LLM 首轮 plan 若未显式 open_tab，保证有页面可操作
		if session.ChromeTabID == 0 {
			tabID, _, err := e.hand.openTab(ctx, task.UserID, task.Url, false)
			if err != nil {
				return success, failed, "open_tab 失败: " + err.Error()
			}
			session.ChromeTabID = tabID
			_ = e.sessionRepo.UpdateChromeTabID(ctx, session.ID, tabID)
		}
		snap, _, err := e.hand.snapshot(ctx, task.UserID, session.ChromeTabID)
		if err != nil {
			// tab 可能在 LLM 思考间隙被 SW 空闲回收/用户关闭：重开一次再 snapshot
			logger.Warnf("[BrowserExec] brain snapshot 失败 session=%d tab=%d: %v，尝试重开 tab", session.ID, session.ChromeTabID, err)
			tabID, _, openErr := e.hand.openTab(ctx, task.UserID, task.Url, false)
			if openErr != nil {
				return success, failed, "snapshot 失败且重开 tab 失败: " + openErr.Error()
			}
			session.ChromeTabID = tabID
			_ = e.sessionRepo.UpdateChromeTabID(ctx, session.ID, tabID)
			cmdSeq++ // P2-1 自愈动作落审计
			e.appendCommandLog(ctx, session.ID, task.ID, 0, cmdSeq, "event", "open_tab_recovery", map[string]any{"reason": "snapshot_failed", "new_tab": tabID}, 0, true)
			if snap, _, err = e.hand.snapshot(ctx, task.UserID, session.ChromeTabID); err != nil {
				return success, failed, "snapshot 失败: " + err.Error()
			}
		}
		// reflect 状态（对标 browser-use MessageManager）：跨轮评估/记忆/历史。
		// F6 压缩视图：折叠台账非空时作为历史首行喂 LLM（滑窗只保近史，构成信息不丢）。
		histView := history
		if hdr := buildFoldHeader(foldedHistory); hdr != "" {
			histView = append([]string{hdr}, history...)
		}
		st := &reflectState{History: histView, PrevEvaluation: prevEvaluation, Memory: memory}
		stepsJSON, done, err := e.brain.GeneratePlanReflect(ctx, task.ID, session.ID, task.BrainGoal, taskPlatformID(task), snap, st)
		prevEvaluation, memory = st.PrevEvaluation, st.Memory
		tokenUsed += e.brain.LastPlanTokens() // P1-1 session 级 token 计量
		if tokenUsed > tokenBudget {
			return success, failed, "Token 预算耗尽（" + itoa(tokenUsed) + " > " + itoa(tokenBudget) + "）"
		}
		if err != nil {
			consecutiveFails++
			logger.Warnf("[BrowserExec] brain plan 连续失败 %d/%d session=%d: %v", consecutiveFails, brainMaxPlanFailures, session.ID, err)
			if consecutiveFails >= brainMaxPlanFailures {
				return success, failed, "LLM 编排失败: " + err.Error()
			}
			// 瞬态失败：回到循环顶部重新 snapshot（跳过本轮已耗时）
			continue
		}
		consecutiveFails = 0
		if done {
			// 独立 judge 验收（对标 browser-use judge）：agent 自称完成 ≠ 真完成。
			// F5（G14）：证据改为**重拍的页面真实快照**（不再是自报摘要）——验收员只信页面；
			// 快照重拍失败才降级回自报摘要（fail-soft：judge 增强不阻断主流程）。
			judgeCtx := ctx
			evidenceSnap, _, snapErr := e.hand.snapshot(ctx, task.UserID, session.ChromeTabID)
			var evidence string
			if snapErr == nil && evidenceSnap != "" {
				evidence = fmt.Sprintf("最新页面快照（独立复核证据，agent 无法伪造）：\n%s", truncateRunes(evidenceSnap, 16000, "\n…[证据快照已截断]"))
			} else {
				evidence = fmt.Sprintf("提取数据摘要:%s\n最近动作:%s", truncate(string(session.ExtractedData), 2048), strings.Join(lastN(history, 6), "; "))
			}
			approve, reason := e.brain.JudgeDone(judgeCtx, task.ID, session.ID, task.BrainGoal, evidence)
			tokenUsed += e.brain.LastAuxTokens() // D3/G3：judge 消耗计入 session 预算
			if tokenUsed > tokenBudget {
				return success, failed, "Token 预算耗尽（" + itoa(tokenUsed) + " > " + itoa(tokenBudget) + "）"
			}
			// P1-2：连续 fail-open 视为未通过（验收增强挂掉≠放行伪装成功）
			if reason == "judge_unavailable" {
				judgeFailOpen++
				if judgeFailOpen >= 2 {
					approve = false
					reason = "连续两次验收服务不可用，不放行"
				}
			} else {
				judgeFailOpen = 0
			}
			// P1-2：judge 结果落 command_log（direction=judge，审计可查）
			cmdSeq++
			e.appendCommandLog(ctx, session.ID, task.ID, 0, cmdSeq, "judge", "judge_done",
				map[string]any{"approve": approve, "reason": reason}, 0, approve)
			if approve {
				exceeded = false
				break
			}
			logger.Warnf("[BrowserExec] judge 拒绝 done（%s），继续循环 session=%d", reason, session.ID)
			prevEvaluation = "验收员拒绝了 done 判定（" + reason + "），目标未真正达成，继续执行"
			continue
		}
		var items []dto.StepItem
		if err := json.Unmarshal(stepsJSON, &items); err != nil {
			consecutiveFails++
			if consecutiveFails >= brainMaxPlanFailures {
				return success, failed, "LLM plan steps 解析失败: " + err.Error()
			}
			continue
		}
		// done=false 且 steps 空 = 无进展轮（模型只输出了思维链/空计划）：
		// 计入连续失败，避免 0 步死循环烧完 maxBrainIterations
		if len(items) == 0 {
			consecutiveFails++
			logger.Warnf("[BrowserExec] brain 空 plan（done=false steps=[]）连续 %d/%d session=%d", consecutiveFails, brainMaxPlanFailures, session.ID)
			if consecutiveFails >= brainMaxPlanFailures {
				return success, failed, "Brain 计划连续为空（模型未产出可执行步骤），目标未达成"
			}
			continue
		}

		abort := ""
		truncated := ""
		for si, it := range items {
			if abort != "" {
				break
			}
			if e.stopFired(stopCh) {
				abort = "用户手动中断"
				continue
			}
			if ctx.Err() != nil {
				abort = fmt.Sprintf("执行超时（%ds）", task.TimeoutSec)
				continue
			}
			if it.Action == "" {
				continue // LLM 偶发空步，跳过
			}
			if it.Action == "screenshot" {
				// G17：Brain 轮内拒绝 screenshot（captureVisibleTab 必然激活 tab 抢用户焦点，
				// 且 F2 前无法确认静默截错）——护栏已禁止 LLM 输出，这里是服务端硬闸（不信任模型）。
				logger.Warnf("[BrowserExec] brain 轮内 screenshot 被服务端拒绝（抢焦点）session=%d", session.ID)
				hist := "screenshot → 被拒绝（Brain 模式禁止抢焦点截图，请用 snapshot/markdown 观察）"
				history = appendHistoryBounded(history, hist, foldedHistory)
				continue
			}
			step := parsedStep{
				StepItem:        it,
				ContinueOnError: it.ContinueOnError,
				RetryCount:      it.RetryCount,
				RetryBackoffMs:  it.RetryBackoffMs,
			}
			// P0-4：LLM 幻觉参数服务端钳位
			step.RetryCount, step.RetryBackoffMs = clampBrainStepParams(step.RetryCount, step.RetryBackoffMs)
			status, errMsg, stepResult := e.executeStepWithRetry(ctx, task, session, stepIdx, step, stopCh, &cmdSeq)
			stepIdx++
			// 历史记录（F6 滑窗压缩：溢出最旧条折叠入账）：LLM 下轮能看到已做过的关键动作
			hist := step.Action + " " + step.Target
			if step.Action == "click" {
				hist += " → " + status
			}
			history = appendHistoryBounded(history, hist, foldedHistory)
			switch status {
			case "success":
				success++
				consecutiveActionFails = 0
			case "skipped":
				// 批7：Brain 重试轮里的写步跳过，按前一轮台账态分判（见 ExecuteSession 同分支说明）。
				// verified → 目标已达成，计成功让 LLM 继续收尾；未验证 → 计败并终止本轮，
				// 因为「发这条内容」这个目标在本轮已不可能再达成（重发即双发），继续烧 LLM 调用没有出路。
				if errMsg == "" {
					success++
					consecutiveActionFails = 0
				} else {
					failed++
					abort = errMsg
				}
			case "failed":
				failed++
				consecutiveActionFails++
				if consecutiveActionFails >= brainMaxActionFails {
					abort = "连续动作失败达上限（" + errMsg + "）"
				} else if !step.ContinueOnError {
					abort = errMsg
				}
			}
			// F6a（G15 先行项）：页面改变型动作成功且本轮仍有后续步 → 立即截断本轮。
			// 后续步引用的 @eN/CSS 属于旧页面快照，继续执行=错位（browser-use「页面变即截断」语义；
			// 下一迭代自然重拍快照，规划不丢上下文）。证据=open_tab 必然换页 / click 回包 navigated。
			if status == "success" && si < len(items)-1 && stepChangedPage(it.Action, stepResult) {
				truncated = it.Action
				break
			}
		}
		if truncated != "" {
			hist := truncated + " → 页面已跳转，本轮计划剩余步骤已跳过（下轮将基于最新快照重规划）"
			history = appendHistoryBounded(history, hist, foldedHistory)
			logger.Infof("[BrowserExec] brain 轮内 %s 引发页面变化，截断剩余步骤 session=%d", truncated, session.ID)
		}
		if abort != "" {
			// 步骤失败多为 tab 丢失（SW 空闲回收）：不清场，交给下一轮 snapshot 恢复逻辑；
			// 只有 open_tab 本身失败（Host 不可用）才终止
			if strings.HasPrefix(abort, "open_tab 失败") || strings.Contains(abort, "browser host 未连接") {
				return success, failed, abort
			}
			logger.Warnf("[BrowserExec] brain 轮内步骤失败 session=%d: %s（下轮 snapshot 自恢复）", session.ID, abort)
		}
		// 拦截页检测（铁律 4）：Brain 轮后同样检测——风控页命中即终止，全自动闭环
		if blocked, reason := e.detectBlockedIfFatal(ctx, task, session, &cmdSeq); blocked {
			return success, failed, reason
		}
		// 循环检测 nudge（对标 browser-use 循环指纹）：连续 3 轮同序列 → 注入换路径提示
		// G9 收口（R25）：文案走 brain_prompts 工厂（prompt 资产化铁律，禁止散落拼接）
		if stuck, seq := loopFingerprint(history); stuck {
			logger.Warnf("[BrowserExec] brain 循环检测命中 session=%d seq=%s，注入 nudge", session.ID, seq)
			prevEvaluation = strings.TrimSpace(BuildLoopNudge(strings.Split(seq, "|")))
		}
	}
	if exceeded {
		return success, failed, fmt.Sprintf("Brain 模式超过最大迭代次数（%d），目标未达成", maxBrainIterations)
	}
	return success, failed, ""
}

// executeStepWithRetry 单步执行（含 retry/backoff），返回 (finalStatus, errMsg, resultJSON)。
// resultJSON：成功时的原语回包（F6a 页面变化证据用），失败为 nil。
// seq：session 局部命令日志计数器（P0-2，调用方持有保证 session 内单调、跨 session 隔离）。
func (e *Executor) executeStepWithRetry(ctx context.Context, task *model.BrowserTask, session *model.BrowserSession, index int, step parsedStep, stopCh chan struct{}, seq *int) (string, string, json.RawMessage) {
	// 批7：写步判定先于落库——is_write 要作为事实随步行一起存，
	// 事后从 action 名字反推会把「type+回车提交」这类隐形写漏掉。
	// 批16（A11）：三态分类。effectUnknown（平台定位表取不到）与 effectWrite 同样进闸门，
	// 落库 is_write=true——判不出副作用时宣称「没有副作用」就是三道闸门一起消失。
	effect, writeWhy := classifyStepEffect(task, step)
	writeStep := effect.needsWriteGate()
	stepRow := &model.BrowserStep{
		SessionID: session.ID,
		TaskID:    task.ID,
		StepIndex: index,
		Action:    step.Action,
		Target:    step.Target,
		Value:     step.Value,
		Status:    "running",
		IsWrite:   writeStep,
	}
	if params, err := json.Marshal(buildStepParams(step)); err == nil && string(params) != "{}" {
		stepRow.Params = params
	}
	if err := e.stepRepo.BatchCreate(ctx, []*model.BrowserStep{stepRow}); err != nil {
		return "failed", "step 落库失败: " + err.Error(), nil
	}

	retries := step.RetryCount
	backoff := step.RetryBackoffMs
	if backoff <= 0 {
		backoff = 1000
	}
	// F2①（G11）：不可逆写原语服务端强制 retries=0——「提交成功但 verify 超时」是结果未知态，
	// 重试=可能双发（Postiz 接口级契约：不可逆变更 maximumAttempts:1）。不信任编排/LLM 传入的重试参数。
	if writeStep {
		retries = 0
	}
	// 批6（F11b）双发闸 + 批7 重试豁免：闸门必须在任何帧下发之前——prep/type 本身会改页面状态
	// （把草稿塞进输入框），不是「零副作用探测」。
	writeKey := ""
	if writeStep {
		// 批16（A7）：本会话台账已经写失败过一次 ⇒ 之后的写步一帧都不下发。
		// 闸门依据的是库里的台账，台账自己写不进去时「过闸」只是走过场——
		// 与其赌一次双发，不如把这一步变成可见、可人工重跑的红步。
		if why, broken := e.ledgerBrokenReason(session.ID); broken {
			msg := fmt.Sprintf("写步拒绝下发（%s）：%s——台账未落，本会话写能力已降级，请人工核对已下发的步后重跑", writeWhy, why)
			_ = e.stepRepo.UpdateResult(ctx, stepRow.ID, "failed", nil, 0, msg)
			logger.Warnf("[BrowserExec] %s session=%d idx=%d", msg, session.ID, index)
			return "failed", msg, nil
		}
		writeKey = writeStepKey(step)
		switch err := e.guardResubmit(ctx, task, writeKey, stepRow.ID); {
		case errors.Is(err, errRetrySkipped):
			prior := priorOfSkippedWrite(err)
			msg := fmt.Sprintf("写步跳过（%s）：%v——同文本已有提交尝试，重发即双发", writeWhy, prior)
			_ = e.stepRepo.UpdateResult(ctx, stepRow.ID, "skipped", nil, 0, msg)
			logger.Infof("[BrowserExec] %s session=%d idx=%d", msg, session.ID, index)
			if prior.verified() {
				return "skipped", "", nil
			}
			// 前一轮只到 sent/unattributed：提交从未被证明，而本轮既不重发（双发）也就无从证明。
			// 此时让整轮判绿就是批6 立项要消灭的那类假绿，所以把事实上抛给调用方计败。
			return "skipped", fmt.Sprintf(
				"重试轮防双发未重发，前一轮提交未验证（%v）——本轮无法证明内容已发布，请人工核对", prior), nil
		case err != nil:
			_ = e.stepRepo.UpdateResult(ctx, stepRow.ID, "failed", nil, 0, err.Error())
			return "failed", err.Error(), nil
		}
	}
	// 批8：D7 覆盖派生写。批7 把「type+回车 / click 发送按钮 / click_near 发送」认成写步之后，
	// 闸门却仍只长在 post_comment 原语内部——开了 require_confirm 的用户，这类隐形写照样被
	// 无条件发出去。此处按步挂起（post_comment 保留原语内的闸门：先 prep 填好正文再确认，
	// 让人看见将要发什么）。位置排在双发闸之后、任何命令帧之前：
	// 该跳过的步不该先问一遍再跳过，未放行则该步一帧都没下发。
	if writeStep && step.Action != "post_comment" && task.RequireConfirm {
		out, why := e.waitForConfirm(ctx, session.ID, confirmWaitBudget(task))
		if out != confirmGranted {
			msg := fmt.Sprintf("写步等待人工确认超时（%s），评论未提交", why)
			if out == confirmStoppedByUser {
				msg = errConfirmAbortedByStop
			}
			_ = e.stepRepo.UpdateResult(ctx, stepRow.ID, "failed", nil, 0, msg)
			return "failed", msg, nil
		}
	}
	var lastErr string
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			if !sleepInterruptible(ctx, stopCh, time.Duration(backoff*(1<<(attempt-1)))*time.Millisecond) {
				_ = e.stepRepo.UpdateResult(ctx, stepRow.ID, "failed", nil, 0, "用户手动中断")
				return "failed", "用户手动中断", nil
			}
		}
		start := time.Now()
		// P8 append-only 命令日志：命令帧先落（含步骤参数全文），回包/错误随 result 事件再落
		*seq++
		cmdPayload := map[string]any{"action": step.Action, "target": step.Target, "params": buildStepParams(step)}
		e.appendCommandLog(ctx, session.ID, task.ID, stepRow.ID, *seq, "command", step.Action, cmdPayload, 0, true)
		result, err := e.dispatchStep(ctx, task, session, step, stepRow)
		dur := time.Since(start).Milliseconds()
		if err == nil {
			// 批16（A7）：写步的「成功」必须以台账落成前提。命令确实下发了，但库里没有这次
			// 提交的凭据 ⇒ 下一轮无从得知它发生过 ⇒ 让整轮绿就是拿不可逆动作换一次好看的状态。
			var ledgerErr error
			if writeStep {
				ledgerErr = e.recordGenericWriteLedger(ctx, task.ID, session.ID, step, stepRow.ID, writeKey, nil)
			}
			if ledgerErr != nil {
				msg := fmt.Sprintf("写步已下发但台账未落（%v）——本轮判红：命令可能已生效，重发即双发，请人工核对该步结果", ledgerErr)
				logger.Warnf("[BrowserExec] %s session=%d idx=%d action=%s", msg, session.ID, index, step.Action)
				_ = e.stepRepo.UpdateResult(ctx, stepRow.ID, "failed", result, dur, msg)
				e.appendCommandLog(ctx, session.ID, task.ID, stepRow.ID, *seq, "event", step.Action, map[string]any{"error": msg}, dur, false)
				return "failed", msg, json.RawMessage(result)
			}
			_ = e.stepRepo.UpdateResult(ctx, stepRow.ID, "success", result, dur, "")
			e.appendCommandLog(ctx, session.ID, task.ID, stepRow.ID, *seq, "event", step.Action, map[string]any{"result": json.RawMessage(result)}, dur, true)
			return "success", "", json.RawMessage(result)
		}
		lastErr = err.Error()
		if writeStep {
			// 步本身已判败，台账再写不进去只加一句因由：缺口由进程内兜底与降级表接管（A7）。
			if ledgerErr := e.recordGenericWriteLedger(ctx, task.ID, session.ID, step, stepRow.ID, writeKey, err); ledgerErr != nil {
				lastErr = fmt.Sprintf("%s；且写台账未落（%v）——重跑本任务前请人工核对该步是否已生效", lastErr, ledgerErr)
			}
		}
		e.appendCommandLog(ctx, session.ID, task.ID, stepRow.ID, *seq, "event", step.Action, map[string]any{"error": lastErr}, dur, false)
		logger.Warnf("[BrowserExec] step 失败 session=%d idx=%d action=%s attempt=%d: %s", session.ID, index, step.Action, attempt, lastErr)
		// F7（G16）：重试按平台错误归因分线（MediaCrawler 处置矩阵语义）——
		// bad_body（内容被拒）重试无意义直接终止；refresh_token/disconnect 同样终止（交上层 session 级处置）；
		// 仅 retry（瞬态）继续退避重试。
		if attempt < retries && !stepErrRetryable(taskPlatformID(task), lastErr) {
			logger.Warnf("[BrowserExec] 错误分类为不可重试，终止步重试 session=%d action=%s", session.ID, step.Action)
			break
		}
	}
	_ = e.stepRepo.UpdateResult(ctx, stepRow.ID, "failed", nil, 0, lastErr)
	return "failed", lastErr, nil
}

// detectBlockedIfFatal 拦截页检测（铁律 4 全自动闭环）：步后 snapshot 命中平台拦截判据
// → 按 ClassifyError 归因，disconnect 类风控直接终止（重试无意义），其余继续（交给重试/自愈）。
// 检测失败（snapshot 拿不到）不阻断主流程——检测是增强不是闸门。
// seq：session 局部命令日志计数（F6：探测帧必须进审计包——本轮真机取证只能靠服务端日志
// 时间线反推探测实耗，审计面上「检测到底跑没跑、跑了多久、看了什么」完全不可见）。
func (e *Executor) detectBlockedIfFatal(ctx context.Context, task *model.BrowserTask, session *model.BrowserSession, seq *int) (blocked bool, reason string) {
	p, err := platform.Get(taskPlatformID(task))
	if err != nil {
		return false, ""
	}
	if session.ChromeTabID <= 0 {
		// 本轮没开过 tab，或 close_tab 步已把 tab 回收（F10 后内存 id 归零）——没有页面可检。
		// 必须在写审计帧之前返回：拿 tab 0 去 snapshot 只会留一条注定失败的探测行污染审计包。
		return false, ""
	}
	var (
		start       = time.Now()
		snap        string
		pageURL     string
		snapErr     string
		probed      int
		blockedNow  bool
		selectorHit bool
	)
	defer func() {
		if seq == nil {
			return
		}
		*seq++
		// 审计用外层 ctx：探测预算可能已耗尽，落库不能被探测自身的超时带走。
		// ok=「探测这条命令本身成没成」，不是「有没有拦到」——旧实现传 blocked，于是每一页
		// 正常页面的探测都记成 ok=false（F11c 真机实测：审计包里 block_detect 全红，
		// 读包的人会以为检测链路坏了；拦截结论另有 blocked/reason 字段承载）。
		e.appendCommandLog(ctx, session.ID, task.ID, 0, *seq, "event", "block_detect", map[string]any{
			"url": pageURL, "snapshot_chars": len(snap), "snapshot_error": snapErr,
			"selectors_probed": probed, "detect_block_hit": blockedNow, "selector_hit": selectorHit,
			"blocked": blocked, "reason": reason,
		}, time.Since(start).Milliseconds(), snapErr == "")
	}()
	// 整段探测共用一个短预算：snapshot（真机 24 次实测 max 876ms）+ 弹层选择器逐个 query，
	// 若各自吃满 defaultCmdTimeout，失败步（本身已耗 30s）会被拖成 60s+，而且探测命令自身的
	// 超时还会计入 A5 僵尸判定，把「步失败」误升级成「Host 假死」。预算内拿不到证据即放行。
	pctx, cancel := context.WithTimeout(ctx, blockDetectBudget)
	defer cancel()
	snap, pageURL, err = e.hand.snapshot(pctx, task.UserID, session.ChromeTabID)
	if err != nil {
		snapErr = err.Error()
		return false, ""
	}
	// 真机实测（session234/235）：风控提示页只有标题+两个按钮，a11y 采集出的是空快照。
	// 旧闸门 `snap == ""` 于是把「URL 已经明写 website-login/error?error_code=300012」的
	// 拦截页判成「无数据」直接放行——URL 层判定不该依赖快照有没有内容。
	// 只有「快照和 URL 都拿不到」（旧扩展 / tab 已死）才算真无证据。
	if snap == "" && pageURL == "" {
		return false, ""
	}
	blockedNow = p.DetectBlock(pageURL, snap)
	// A2 容器层：URL 不变的弹层式拦截（如闲鱼 baxia 滑块）——风控专用选择器经 query
	// exists 下探。fail-soft：查询出错不作拦截证据（检测是增强不是闸门）。
	if !blockedNow {
		if bs, ok := p.(platform.BlockSelectorDeclarer); ok {
			for _, sel := range bs.BlockSelectors() {
				probed++
				res, qerr := e.hand.query(pctx, task.UserID, session.ChromeTabID, "exists", sel, "")
				if qerr == nil && res["exists"] == true {
					blockedNow = true
					selectorHit = true
					logger.Infof("[BrowserExec] 拦截容器命中 platform=%s selector=%s session=%d", p.Identifier(), sel, session.ID)
					break
				}
			}
		}
	}
	if !blockedNow {
		return false, ""
	}
	et := p.ClassifyError(pageURL + "\n" + snap)
	if selectorHit && et != platform.ErrDisconnect {
		// 风控专用容器命中即定性拦截（弹层文案可能不进快照结构行，分类不得漏回 Unknown）
		et = platform.ErrDisconnect
	}
	logger.Warnf("[BrowserExec] 平台拦截页命中 platform=%s errType=%s session=%d", p.Identifier(), et, session.ID)
	switch et {
	case platform.ErrDisconnect:
		return true, fmt.Sprintf("平台风控拦截（%s）：检测到验证码/风控页，账号需人工介入，会话终止", p.Identifier())
	default:
		// refresh_token/bad_body/retry 类：记录但不终止（bad_body 语义是内容被拒，页面上不会常驻）
		return false, ""
	}
}

// dispatchStep 按动作分发到 Hand 原语。
// 回包（snapshot/extract/markdown/screenshot 等）作为返回值交 executeStepWithRetry 落库——
// 不挂 Executor 字段：Executor 是进程级单例、多 session 并发触达（R-A4 竞态修复）。
// stepRow：本步的 DB 行（含 ID），写原语分支用它落 submit_state 台账（批6/F11b）。
func (e *Executor) dispatchStep(ctx context.Context, task *model.BrowserTask, session *model.BrowserSession, step parsedStep, stepRow *model.BrowserStep) ([]byte, error) {
	userID := task.UserID
	tabID := session.ChromeTabID
	p := buildStepParams(step)

	switch step.Action {
	case "open_tab":
		openURL := step.Target
		if openURL == "" {
			openURL = task.Url // 编排未填 target 时兜底任务起始 URL
		}
		tabID, res, err := e.hand.openTab(ctx, userID, openURL, false) // active 恒 false
		if err != nil {
			return nil, err
		}
		session.ChromeTabID = tabID
		_ = e.sessionRepo.UpdateChromeTabID(ctx, session.ID, tabID)
		// page_loaded 原样透传（可能是 nil：老 Host 不回这个字段时如实记 null，
		// 不能把「不知道」写成 false，也不能反过来把 false 洗成 true）。
		return recordResultPayload(map[string]any{"chrome_tab_id": tabID, "page_loaded": res["loaded"]})
	case "click":
		res, err := e.hand.click(ctx, userID, tabID, step.Target)
		if err != nil {
			return nil, err
		}
		// channel 如实透传：cdp / dom_fallback / null（老扩展没这个字段）。
		// 兜底本身不是失败，但「一片绿里全是 dom_fallback」= trusted 通道死了，
		// 这一列就是用来发现它死了的（批14 实证形态）。
		return recordResultPayload(map[string]any{"navigated": res["navigated"] == true, "channel": res["channel"]})
	case "type":
		res, err := e.hand.typeText(ctx, userID, tabID, step.Target, step.Value, p.ClearFirst, p.SubmitOnEnter)
		if err != nil {
			return nil, err
		}
		return recordResultPayload(map[string]any{"channel": res["channel"]})
	case "click_near":
		// 以 Anchor CSS 为基准点击容器内指定文本的 button（发送/提交按钮无稳定 class 场景）
		res, err := e.hand.clickNear(ctx, userID, tabID, step.Anchor, step.ButtonText)
		if err != nil {
			return nil, err
		}
		return recordResultPayload(map[string]any{"clicked": res["clicked"] == true, "channel": res["channel"]})
	case "post_comment":
		// F2②（G11 正确版）：三段式拆分——prep（可重入）→ send（唯一不可逆点，F2① 已禁重试）
		// → verify 轮询 finalize（只读、可中断、可归因）。提交与验证彻底分离：
		// verify 超时 ≠ 重试提交（Postiz maximumAttempts:1 红线）；finalize 落库证据闭环。
		// 选择器四元组由平台适配器（L3）下发——扩展零平台知识（设计稿 P5）；
		// 未声明 post_comment 能力的平台在此 fails-loudly（P4 契约默认失败）。
		locs, err := platform.CommentLocatorsFor(ctx, taskPlatformID(task))
		if err != nil {
			return nil, err
		}
		// 双发闸在 executeStepWithRetry 下发任何帧之前已过（批7 起对全部写步统一生效）。
		textHash := writeStepKey(step)
		prepReq := map[string]any{
			"input_selector":   locs.InputSelector,
			"send_button_text": locs.SendButtonText,
		}
		if _, err := e.hand.commentPrep(ctx, userID, tabID, step.Value, prepReq); err != nil {
			// A1 自愈一次：输入框定位失效（改版）→ LLM 按快照重选；选不中或已自愈过 → 原样上抛
			if !e.healCommentInput(ctx, task, session, tabID, prepReq, err) {
				return nil, err
			}
			if _, err2 := e.hand.commentPrep(ctx, userID, tabID, step.Value, prepReq); err2 != nil {
				return nil, err2
			}
		}
		// prep 成功=文本已进输入框，点击仍未发生 → prepared（可安全重下发态，也是
		// 「闸门/中止腿从未提交」的正面证据）
		// 批16（A7）：这一行写不进去就不再往下走一格。prepared 不在拦阻集合内，send 之后
		// 库里若还是空的，下一轮（含同进程自动重试）就查不到任何尝试——那正是双发的形状。
		if err := e.recordSubmitState(ctx, task.ID, session.ID, stepRow.ID, model.StepSubmitPrepared, textHash, false); err != nil {
			return nil, fmt.Errorf("post_comment 未提交（prepared 台账未落，点击从未发生，本会话写能力已降级）: %w", err)
		}
		// D7 人工确认闸门：require_confirm=true 的任务在此挂起，等 POST /sessions/:id/confirm
		// 放行后才进不可逆提交点。挂起期间只有 prep（填文本，页面内可撤销、零平台副作用）；
		// 未放行即中止=从未提交，可安全重下发。
		// 批8：等待用 task.confirm_wait_sec 独立预算，不再吃 task.TimeoutSec（那条管自动化本身）。
		if task.RequireConfirm {
			out, why := e.waitForConfirm(ctx, session.ID, confirmWaitBudget(task))
			switch out {
			case confirmGranted:
			case confirmStoppedByUser:
				return nil, errors.New(errConfirmAbortedByStop)
			default: // confirmWaitTimedOut：确认预算或执行预算先到头，why 里写明是哪条
				return nil, fmt.Errorf("post_comment 等待人工确认超时（%s），评论未提交", why)
			}
		}
		// 不可逆提交点。send 结局分两类归因（R26-2 竞速超时使边界可判）：
		// ① *_inject_timeout=按钮定位注入未执行→点击从未发生→无副作用，直接判失败早返
		//    （不进 finalize 白轮 16s，也不污染证据——错误文本自证「未点击」可安全重下发）；
		// ② 其余任何结局（成功/出错/WS 超时）→点击可能已发生=结果未知→必须 finalize 回查：
		//    Postiz 心跳判因矩阵语义——超时≠未发生，回查是唯一合法归因路径，绝不重新提交。
		_, sendErr := e.hand.commentSend(ctx, userID, tabID, prepReq)
		if isInjectTimeout(sendErr) {
			return nil, fmt.Errorf("post_comment 未提交（页面注入拥堵，点击未发生）: %w", sendErr)
		}
		if isSendGateReject(sendErr) {
			return nil, fmt.Errorf("post_comment 未提交（发送按钮不可点，点击未发生）: %w", sendErr)
		}
		// 提交点已跨越：立即落 sent，不等 finalize 的结论。理由——「send 之后 execCtx 恰好到期」
		// 是最坏窗口（步被判超时、终态归因模糊），此时台账若还没写，重下发就没有任何拦阻。
		// 注入超时/闸门两个分支在上面提前返回、台账留在 prepared：那两支点击从未发生。
		// 批16（A7）：这次写失败=「越点未落账」，recordSubmitState 会同时记下进程内兜底缺口，
		// 本步最终判红（见下面 sentLedgerErr）——撤不回了，但至少不再有人替我们假设它没发生。
		sentLedgerErr := e.recordSubmitState(ctx, task.ID, session.ID, stepRow.ID, model.StepSubmitSent, textHash, true)
		// A1 自愈一次：send_button_not_found=按钮从未命中=点击从未发生（同 R26-2 归因），
		// 重发不违「单次提交禁重试」红线；其余错误结局未知，交 finalize 回查绝不重发。
		if sendErr != nil && e.healCommentSendButton(ctx, task, session, tabID, prepReq, sendErr) {
			_, sendErr = e.hand.commentSend(ctx, userID, tabID, prepReq)
		}
		verified, evidence := e.finalizeComment(ctx, userID, tabID, step.Value, locs, e.stopChFor(session.ID))
		// 回查结论落台账：verified 是唯一可对外宣称「已发布」的态；未见即 unattributed
		// （已尝试、归因不到）——unattributed 仍在双发闸的拦截集合内，交人来判。
		finalState := model.StepSubmitUnattributed
		if verified {
			finalState = model.StepSubmitVerified
		}
		finalLedgerErr := e.recordSubmitState(ctx, task.ID, session.ID, stepRow.ID, finalState, textHash, true)
		// finalize 证据落 extracted_data（追溯面板 + I4 续跑位点：重放可见「哪条评论已提交已验证」）
		e.mergeExtract(ctx, session, "post_comment", map[string]any{
			"text":       step.Value,
			"send_error": sendErrText(sendErr),
			"verified":   verified,
			"evidence":   evidence,
			"posted_at":  time.Now().Format(time.RFC3339),
		})
		// 台账缺口优先于「看起来成功」：verified 的回查证据是真的，但库里没有这次提交，
		// 下一轮的同文本重发就不会被拦——所以这一格不能给绿。判红不等于宣称失败，
		// 文案里把「回查见/未见」原样带上，人一眼能判该不该补这条台账。
		if ledgerErr := errors.Join(sentLedgerErr, finalLedgerErr); ledgerErr != nil {
			seen := "回查未见评论（结果未知）"
			if verified {
				seen = "回查已见评论（内容确已发布）"
			}
			return nil, fmt.Errorf("post_comment %s，但提交台账未落全（%v）——本会话写能力已降级，重跑本任务前请人工核对该评论是否已发布", seen, ledgerErr)
		}
		if verified {
			return recordResultPayload(map[string]any{"posted": true, "verified": true, "evidence": evidence})
		}
		// fails-loudly：提交结局未知或验证未见——归因「平台静默吞/审核中/渲染超时」，
		// 步判失败但不重试（重试=双发）。这是 R17/R19-6 实测形态的正式归宿。
		if sendErr != nil {
			return nil, fmt.Errorf("post_comment 提交命令异常（%v）且回查未见评论——结果未知，不重试防双发", sendErr)
		}
		return nil, fmt.Errorf("post_comment 已提交但验证未通过（可能被平台拦截/审核中），不重试防双发")
	case "assert":
		// 断言类原语（洞察层）：contains_text / selector_exists，失败即抛错（Playwright expect 语义）
		timeout := p.TimeoutMs
		if timeout <= 0 {
			timeout = 5000
		}
		return nil, e.hand.assert(ctx, userID, tabID, p.AssertKind, step.Value, timeout)
	case "query":
		// 只读洞察原语：text/exists/count/attr，返回数据不抛错（Midscene 洞察类语义）
		res, err := e.hand.query(ctx, userID, tabID, p.QueryKind, step.Target, p.Attribute)
		if err != nil {
			return nil, err
		}
		return recordResultPayload(res)
	case "snapshot":
		snap, pageURL, err := e.hand.snapshot(ctx, userID, tabID)
		if err != nil {
			return nil, err
		}
		_ = e.sessionRepo.UpdateSnapshot(ctx, session.ID, snap)
		return recordResultPayload(map[string]any{"snapshot_chars": len(snap), "snapshot": snap, "page_url": pageURL})
	case "markdown":
		res, err := e.hand.markdown(ctx, userID, tabID)
		if err != nil {
			return nil, err
		}
		md, _ := res["markdown"].(string)
		// 截断标志如实上抛（批9a）：markdown 是「快照太长时改用 markdown 取全文」的出口
		// （brain_budget 的截断提示就是这么写的），只记本地长度会把 64KiB 截断值当成整页，
		// 而 len() 是字节数、扩展侧 markdown_chars 是字符数，两者对中文差 3 倍。
		return recordResultPayload(map[string]any{
			"markdown":       md,
			"markdown_chars": res["markdown_chars"],
			"full_chars":     res["full_chars"],
			"truncated":      res["truncated"],
			"content_empty":  res["content_empty"],
		})
	case "screenshot":
		// G17 定稿（二验修正）：captureVisibleTab 只能截「当前激活 tab」且不报错——
		// 不激活直接截会静默截到用户正在看的页面（假内容），降级方案不成立。
		// 因此：激活是正确性必需，抢焦点的收口放在「频度」——护栏禁止 Brain 轮内使用
		// screenshot（观察用 snapshot/markdown），用户显式编排/终态留证时才会激活一次。
		b64, err := e.hand.screenshot(ctx, userID, tabID, true)
		if err != nil {
			return nil, err
		}
		if b64 != "" && e.feedback != nil {
			if url, err := e.feedback.SaveFinalScreenshot(ctx, session.ID, b64); err != nil {
				logger.Warnf("[BrowserExec] 截图落库失败 session=%d: %v", session.ID, err)
			} else if url != "" {
				session.FinalScreenshotURL = url
				_ = e.sessionRepo.UpdateArtifacts(ctx, session.ID, nil, url, "")
			}
		}
		return recordResultPayload(map[string]any{"screenshot_url": session.FinalScreenshotURL, "screenshot_b64_chars": len(b64)})
	case "wait":
		return nil, e.hand.waitFor(ctx, userID, tabID, p.Ms)
	case "wait_for_selector":
		timeout := p.TimeoutMs
		if timeout <= 0 {
			timeout = 10000
		}
		return nil, e.hand.waitForSelector(ctx, userID, tabID, p.Selector, timeout)
	case "scroll":
		return nil, e.hand.scroll(ctx, userID, tabID, p.Direction, p.Amount)
	case "extract":
		// Brain 兼容：LLM 常把单选择器放 target 而不是 selectors map——基座层归一（铁律 3：大模型驱动容错）
		selectors := p.Selectors
		if len(selectors) == 0 && strings.TrimSpace(step.Target) != "" {
			selectors = map[string]string{"target": strings.TrimSpace(step.Target)}
		}
		data, err := e.hand.extract(ctx, userID, tabID, selectors)
		if err != nil {
			return nil, err
		}
		// 合并本 session 各次 extract 到 extracted_data（追溯面板读这里）
		merged, ok := data["data"].(map[string]any)
		if !ok {
			merged = map[string]any{"raw": data}
		}
		e.mergeExtract(ctx, session, "", merged)
		return recordResultPayload(merged)
	case "close_tab":
		if err := e.hand.closeTab(ctx, userID, tabID); err != nil {
			return nil, err
		}
		// 内存 tab id 归零（DB 值保留作审计）：收口回收据此判定「已回收」，
		// 否则编排里的 close_tab + 收口清理会对同一 tab 发两帧。
		session.ChromeTabID = 0
		return nil, nil
	default:
		return nil, fmt.Errorf("未知动作: %s", step.Action)
	}
}

// recordResultPayload 序列化原语回包（R-A4：纯函数，经 dispatchStep 返回值传递，
// 不再挂 Executor 字段）。marshal 失败显式抛错——旧 recordResult 吞错会让步静默
// 落库空 result，追溯面板永远看不到内容。
func recordResultPayload(payload map[string]any) ([]byte, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	return json.Marshal(payload)
}

// mergeExtract 合并写 session.extracted_data（追溯面板读这里）。
// key 非空时嵌套在 key 名下（如 post_comment 证据），空时直接顶层合并（extract 原语语义）。
func (e *Executor) mergeExtract(ctx context.Context, session *model.BrowserSession, key string, payload map[string]any) {
	existing := map[string]any{}
	if len(session.ExtractedData) > 0 {
		_ = json.Unmarshal(session.ExtractedData, &existing)
	}
	if key != "" {
		// 同名 key 多次提交 → 追加为数组（评论可能一条任务发多条）
		prev, had := existing[key]
		if !had {
			existing[key] = []any{payload}
		} else if arr, ok := prev.([]any); ok {
			existing[key] = append(arr, payload)
		} else {
			existing[key] = []any{prev, payload}
		}
	} else {
		for k, v := range payload {
			existing[k] = v
		}
	}
	blob, err := json.Marshal(existing)
	if err == nil {
		_ = e.sessionRepo.UpdateExtractedData(ctx, session.ID, blob)
		session.ExtractedData = blob
	}
}

// finalizeComment F2② finalize：提交后的只读验证轮询（Postiz pending→checkPostStatus→finalize 语义）。
// 短超时多次 comment_verify——每次都是只读命令，可安全重试/可中断；绝不重新提交。
// 轮询窗口 = 首验 6s + 复核 2×5s（覆盖小红书评论异步审核回显的实测节奏），stopCh 关闭即放弃。
// 返回 (verified, evidence)：evidence=最后一次验证回包（含命中容器数/条目文本节选）。
func (e *Executor) finalizeComment(ctx context.Context, userID uint, tabID int, text string, locs platform.CommentLocators, stopCh chan struct{}) (bool, map[string]any) {
	rounds := []int{6000, 5000, 5000}
	var last map[string]any
	for i, timeoutMs := range rounds {
		if ctx.Err() != nil || (stopCh != nil && e.stopFired(stopCh)) {
			return false, last
		}
		res, err := e.hand.commentVerify(ctx, userID, tabID, text, locs.CommentContainer, locs.CommentItemText, timeoutMs)
		if err != nil {
			logger.Warnf("[BrowserExec] comment_verify 第%d轮出错（继续轮询）user=%d tab=%d: %v", i+1, userID, tabID, err)
			continue
		}
		last = res
		if verified, _ := res["verified"].(bool); verified {
			return true, res
		}
	}
	return false, last
}

func buildStepParams(step parsedStep) StepParams {
	p := StepParams{
		Ms:            step.Ms,
		ClearFirst:    step.ClearFirst,
		SubmitOnEnter: step.SubmitOnEnter,
		Direction:     step.Direction,
		Amount:        step.Amount,
		Selector:      step.Selector,
		TimeoutMs:     step.TimeoutMs,
		Selectors:     step.Selectors,
		Anchor:        step.Anchor,
		ButtonText:    step.ButtonText,
		AssertKind:    step.AssertKind,
		QueryKind:     step.QueryKind,
		Attribute:     step.Attribute,
	}
	return p
}

// taskPlatformID 任务归属平台：task.platform 列是权威（R20 起为显式字段）；
// 空值走缺省 xiaohongshu 保证存量任务兼容（AccountID 序号映射已废弃）
func taskPlatformID(task *model.BrowserTask) string {
	if p := strings.TrimSpace(task.Platform); p != "" {
		return p
	}
	return "xiaohongshu"
}

// ParseSteps 编排 JSON → parsedStep 数组（task.steps 为空返回空切片）
func ParseSteps(raw []byte) ([]parsedStep, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var items []dto.StepItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("steps JSON 解析失败: %w", err)
	}
	out := make([]parsedStep, 0, len(items))
	for _, it := range items {
		out = append(out, parsedStep{
			StepItem:        it,
			ContinueOnError: it.ContinueOnError,
			RetryCount:      it.RetryCount,
			RetryBackoffMs:  it.RetryBackoffMs,
		})
	}
	return out, nil
}

// sleepInterruptible 可被 ctx 取消 / stopCh 打断的睡眠；返回 false 表示被中断
func sleepInterruptible(ctx context.Context, stopCh chan struct{}, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-stopCh:
		return false
	case <-t.C:
		return true
	}
}

// humanizedDelay 步间节奏抖动（铁律 2 模拟人工）：base ±30% 均匀随机。
// 机器执行步间间隔恒定是可检测的自动化特征；真人在页面停留时间是长尾分布。
// base<=0 返回 0（不引入计划外等待）。
func humanizedDelay(base int) time.Duration {
	if base <= 0 {
		return 0
	}
	span := int(float64(base) * 0.3)
	if span <= 0 {
		return time.Duration(base) * time.Millisecond
	}
	jitter := rand.Intn(2*span+1) - span // [-span, span]
	return time.Duration(base+jitter) * time.Millisecond
}

// lastN 取 history 末 n 条（judge 摘要用）
func lastN(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// ---- F6（G15 余项）历史压缩：滑动窗口 + 确定性折叠台账 ----
// browser-use 用额外 LLM 调用压缩历史（25 步/40k→6k）；我们取等价语义的零成本形态：
// 溢出窗口的最旧条目不静默丢弃，而是计入折叠台账（按动作计数），台账首行喂 LLM——
// 保留"总共做过什么"的构成信息（防重复无效动作），逐条细节永久可查 command_log。

// historyWindow Brain 历史滑动窗口容量
const historyWindow = 24

// appendHistoryBounded 容量约束下追加历史：溢出时最旧一条计入 folded 台账（键=动作名）。
// folded 允许为 nil（judge/摘要等只读路径复用本函数记账时不折叠）。
func appendHistoryBounded(history []string, entry string, folded map[string]int) []string {
	if len(history) >= historyWindow {
		oldest := history[0]
		history = history[1:]
		if folded != nil {
			folded[historyActionOf(oldest)]++
		}
	}
	return append(history, entry)
}

// historyActionOf 提取历史行动作词（行格式 "<action> <target>[ → 状态]"；折叠行以 "[" 前缀开头不计动作）
func historyActionOf(line string) string {
	if strings.HasPrefix(line, "[") {
		return "compacted"
	}
	if i := strings.IndexByte(line, ' '); i > 0 {
		return line[:i]
	}
	return line
}

// buildFoldHeader 折叠台账首行，如 "[已折叠 37 步: click×21 type×9 extract×7]"；台账为空返回 ""。
func buildFoldHeader(folded map[string]int) string {
	if len(folded) == 0 {
		return ""
	}
	total := 0
	keys := make([]string, 0, len(folded))
	for k, v := range folded {
		total += v
		if v > 0 {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s×%d", k, folded[k]))
	}
	return fmt.Sprintf("[已折叠 %d 步: %s]", total, strings.Join(parts, " "))
}

// stepChangedPage F6a 证据判定：open_tab 必然换页；click 以扩展回包 navigated 为准；
// click_near/type 无换页证据不截断（保守——误截只多一轮快照，不错截）。
func stepChangedPage(action string, result json.RawMessage) bool {
	if action == "open_tab" {
		return true
	}
	if action == "click" && len(result) > 0 {
		var r struct {
			Navigated bool `json:"navigated"`
		}
		if json.Unmarshal(result, &r) == nil {
			return r.Navigated
		}
	}
	return false
}

// stepErrRetryable 步失败是否值得再试（F7/G16 + R-A3 fail-closed，2026-09-19）：
// 仅显式归因 retry（瞬态：超时/元素未就绪）继续退避重试；bad_body/refresh_token/
// disconnect 终止（语义同前）；ErrUnknown（判据未命中）同样终止——未知错误盲目重试
// 会把归因缺口放大成风控信号，正确路径是打点归因后显式扩表（AWS SDK「全 Unknown 不重试」语义）。
// 平台未注册时维持兼容放行（无判据可用，不归 fail-closed 范畴）。
func stepErrRetryable(platformID, errText string) bool {
	p, err := platform.Get(platformID)
	if err != nil {
		return true
	}
	return p.ClassifyError(errText) == platform.ErrRetry
}
