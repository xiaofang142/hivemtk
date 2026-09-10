package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
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

	stopMu       sync.Mutex
	stopRegistry map[uint]chan struct{} // sessionID → stopCh

	// lastStepResult dispatchStep 捕获的原语回包（snapshot/extract/markdown/screenshot），
	// executeStepWithRetry 落库到 step.result 后清空。非并发安全：仅 Executor 单 goroutine 触达。
	lastStepResult []byte
}

func NewExecutor(hand *Hand, sessionRepo repository.BrowserSessionRepository, stepRepo repository.BrowserStepRepository, brain *BrainService, feedback *FeedbackService) *Executor {
	return &Executor{
		hand:         hand,
		sessionRepo:  sessionRepo,
		stepRepo:     stepRepo,
		brain:        brain,
		feedback:     feedback,
		stopRegistry: make(map[uint]chan struct{}),
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

// ExecuteSession 执行一个 session（在独立 goroutine 中运行）。
// ctx 由调用方包上 task.TimeoutSec 超时。
func (e *Executor) ExecuteSession(ctx context.Context, task *model.BrowserTask, session *model.BrowserSession, steps []parsedStep) {
	stopCh := e.registerStop(session.ID)
	defer e.unregisterStop(session.ID)

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
				status, errMsg := e.executeStepWithRetry(ctx, task, session, i, step, stopCh, &cmdSeq)
				switch status {
				case "success":
					success++
				case "failed":
					failed++
					if !step.ContinueOnError {
						sessionFailed = errMsg
						break loop
					}
				}
				// 拦截页检测（铁律 4）：成功步后页面可能已跳风控页——disconnect 类终止，全自动闭环
				if blocked, reason := e.detectBlockedIfFatal(ctx, task, session); blocked {
					sessionFailed = reason
					break loop
				}
			}
		}
	}

	finalStatus := "completed"
	if sessionFailed != "" {
		finalStatus = "failed"
		if e.stopFired(stopCh) && sessionFailed == "用户手动中断" {
			finalStatus = "stopped"
		}
	}
	_ = e.sessionRepo.UpdateStatus(ctx, session.ID, finalStatus, sessionFailed)
	_ = e.sessionRepo.UpdateMetrics(ctx, session.ID, success+failed, success, failed, 0)

	// Brain 模式：LLM 总结执行结果落 llm_summary（P2-2：completed 与 failed 都总结——失败归因同样是交付物）
	if e.brain != nil && task.BrainMode && (finalStatus == "completed" || finalStatus == "failed") {
		if summ := e.brain.SummarizeSession(ctx, task.BrainGoal, success, success+failed, string(session.ExtractedData), session.ConsoleErrors); summ != "" {
			_ = e.sessionRepo.UpdateArtifacts(ctx, session.ID, nil, "", summ, "")
		}
	}

	// Brain 总结 / 失败重试调度 / 通知 —— FeedbackService 内部异步
	if e.feedback != nil {
		e.feedback.OnSessionFinished(context.Background(), task, session, finalStatus, success, success+failed)
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
	// P1-1：session 级 token 预算熔断（plan+judge 全计入）
	tokenUsed := 0
	tokenBudget := brainTokenBudget()
	// wall-clock 看门狗（R22）：ctx 取消链在某些 LLM/DB 调用栈不生效（session132 实测 11min+ active），
	// 以真实时钟兜底——超 TimeoutSec+30s 强制收敛，会话必有终态
	deadline := time.Now().Add(time.Duration(task.TimeoutSec)*time.Second + 30*time.Second)

	for iter := 0; iter < maxBrainIterations; iter++ {
		if e.stopFired(stopCh) {
			return success, failed, "用户手动中断"
		}
		if ctx.Err() != nil || time.Now().After(deadline) {
			return success, failed, fmt.Sprintf("执行超时（%ds）", task.TimeoutSec)
		}
		if iter > 0 && !sleepInterruptible(ctx, stopCh, humanizedDelay(task.DelayMs)) {
			return success, failed, "用户手动中断"
		}
		// 兜底开 tab：LLM 首轮 plan 若未显式 open_tab，保证有页面可操作
		if session.ChromeTabID == 0 {
			tabID, err := e.hand.openTab(ctx, task.UserID, task.Url, false)
			if err != nil {
				return success, failed, "open_tab 失败: " + err.Error()
			}
			session.ChromeTabID = tabID
			_ = e.sessionRepo.UpdateChromeTabID(ctx, session.ID, tabID)
		}
		snap, err := e.hand.snapshot(ctx, task.UserID, session.ChromeTabID)
		if err != nil {
			// tab 可能在 LLM 思考间隙被 SW 空闲回收/用户关闭：重开一次再 snapshot
			logger.Warnf("[BrowserExec] brain snapshot 失败 session=%d tab=%d: %v，尝试重开 tab", session.ID, session.ChromeTabID, err)
			tabID, openErr := e.hand.openTab(ctx, task.UserID, task.Url, false)
			if openErr != nil {
				return success, failed, "snapshot 失败且重开 tab 失败: " + openErr.Error()
			}
			session.ChromeTabID = tabID
			_ = e.sessionRepo.UpdateChromeTabID(ctx, session.ID, tabID)
			cmdSeq++ // P2-1 自愈动作落审计
			e.appendCommandLog(ctx, session.ID, task.ID, 0, cmdSeq, "event", "open_tab_recovery", map[string]any{"reason": "snapshot_failed", "new_tab": tabID}, 0, true)
			if snap, err = e.hand.snapshot(ctx, task.UserID, session.ChromeTabID); err != nil {
				return success, failed, "snapshot 失败: " + err.Error()
			}
		}
		// reflect 状态（对标 browser-use MessageManager）：跨轮评估/记忆/历史
		st := &reflectState{History: history, PrevEvaluation: prevEvaluation, Memory: memory}
		stepsJSON, done, err := e.brain.GeneratePlanReflect(ctx, task.ID, task.BrainGoal, taskPlatformID(task), snap, st)
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
			// 独立 judge 验收（对标 browser-use judge）：agent 自称完成 ≠ 真完成
			judgeCtx := ctx
			finalState := fmt.Sprintf("提取数据摘要:%s\n最近动作:%s", truncate(string(session.ExtractedData), 2048), strings.Join(lastN(history, 6), "; "))
			approve, reason := e.brain.JudgeDone(judgeCtx, task.BrainGoal, finalState)
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
		for _, it := range items {
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
			step := parsedStep{
				StepItem:        it,
				ContinueOnError: it.ContinueOnError,
				RetryCount:      it.RetryCount,
				RetryBackoffMs:  it.RetryBackoffMs,
			}
			// P0-4：LLM 幻觉参数服务端钳位
			step.RetryCount, step.RetryBackoffMs = clampBrainStepParams(step.RetryCount, step.RetryBackoffMs)
			status, errMsg := e.executeStepWithRetry(ctx, task, session, stepIdx, step, stopCh, &cmdSeq)
			stepIdx++
			// 历史记录（截断防膨胀）：LLM 下轮能看到已做过的关键动作
			hist := step.Action + " " + step.Target
			if step.Action == "click" {
				hist += " → " + status
			}
			if len(history) < 24 {
				history = append(history, hist)
			} else {
				copy(history, history[1:])
				history[len(history)-1] = hist
			}
			switch status {
			case "success":
				success++
				consecutiveActionFails = 0
			case "failed":
				failed++
				consecutiveActionFails++
				if consecutiveActionFails >= brainMaxActionFails {
					abort = "连续动作失败达上限（" + errMsg + "）"
				} else if !step.ContinueOnError {
					abort = errMsg
				}
			}
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
		if blocked, reason := e.detectBlockedIfFatal(ctx, task, session); blocked {
			return success, failed, reason
		}
		// 循环检测 nudge（对标 browser-use 循环指纹）：连续 3 轮同序列 → 注入换路径提示
		if stuck, seq := loopFingerprint(history); stuck {
			logger.Warnf("[BrowserExec] brain 循环检测命中 session=%d seq=%s，注入 nudge", session.ID, seq)
			prevEvaluation = "检测到你在原地打转（最近 3 轮动作序列相同），本轮必须换路径"
		}
	}
	if exceeded {
		return success, failed, fmt.Sprintf("Brain 模式超过最大迭代次数（%d），目标未达成", maxBrainIterations)
	}
	return success, failed, ""
}

// executeStepWithRetry 单步执行（含 retry/backoff），返回 (finalStatus, errMsg)。
// seq：session 局部命令日志计数器（P0-2，调用方持有保证 session 内单调、跨 session 隔离）。
func (e *Executor) executeStepWithRetry(ctx context.Context, task *model.BrowserTask, session *model.BrowserSession, index int, step parsedStep, stopCh chan struct{}, seq *int) (string, string) {
	stepRow := &model.BrowserStep{
		SessionID: session.ID,
		TaskID:    task.ID,
		StepIndex: index,
		Action:    step.Action,
		Target:    step.Target,
		Value:     step.Value,
		Status:    "running",
	}
	if params, err := json.Marshal(buildStepParams(step)); err == nil && string(params) != "{}" {
		stepRow.Params = params
	}
	if err := e.stepRepo.BatchCreate(ctx, []*model.BrowserStep{stepRow}); err != nil {
		return "failed", "step 落库失败: " + err.Error()
	}

	retries := step.RetryCount
	backoff := step.RetryBackoffMs
	if backoff <= 0 {
		backoff = 1000
	}
	var lastErr string
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			if !sleepInterruptible(ctx, stopCh, time.Duration(backoff*(1<<(attempt-1)))*time.Millisecond) {
				_ = e.stepRepo.UpdateResult(ctx, stepRow.ID, "failed", nil, 0, "用户手动中断")
				return "failed", "用户手动中断"
			}
		}
		start := time.Now()
		// P8 append-only 命令日志：命令帧先落（含步骤参数全文），回包/错误随 result 事件再落
		*seq++
		cmdPayload := map[string]any{"action": step.Action, "target": step.Target, "params": buildStepParams(step)}
		e.appendCommandLog(ctx, session.ID, task.ID, stepRow.ID, *seq, "command", step.Action, cmdPayload, 0, true)
		err := e.dispatchStep(ctx, task, session, step)
		dur := time.Since(start).Milliseconds()
		if err == nil {
			_ = e.stepRepo.UpdateResult(ctx, stepRow.ID, "success", e.lastStepResult, dur, "")
			e.appendCommandLog(ctx, session.ID, task.ID, stepRow.ID, *seq, "event", step.Action, map[string]any{"result": json.RawMessage(e.lastStepResult)}, dur, true)
			e.lastStepResult = nil
			return "success", ""
		}
		lastErr = err.Error()
		e.appendCommandLog(ctx, session.ID, task.ID, stepRow.ID, *seq, "event", step.Action, map[string]any{"error": lastErr}, dur, false)
		logger.Warnf("[BrowserExec] step 失败 session=%d idx=%d action=%s attempt=%d: %s", session.ID, index, step.Action, attempt, lastErr)
	}
	_ = e.stepRepo.UpdateResult(ctx, stepRow.ID, "failed", nil, 0, lastErr)
	return "failed", lastErr
}

// detectBlockedIfFatal 拦截页检测（铁律 4 全自动闭环）：步后 snapshot 命中平台拦截判据
// → 按 ClassifyError 归因，disconnect 类风控直接终止（重试无意义），其余继续（交给重试/自愈）。
// 检测失败（snapshot 拿不到）不阻断主流程——检测是增强不是闸门。
func (e *Executor) detectBlockedIfFatal(ctx context.Context, task *model.BrowserTask, session *model.BrowserSession) (blocked bool, reason string) {
	p, err := platform.Get(taskPlatformID(task))
	if err != nil {
		return false, ""
	}
	snap, err := e.hand.snapshot(ctx, task.UserID, session.ChromeTabID)
	if err != nil || snap == "" {
		return false, ""
	}
	if !p.DetectBlock(snap) {
		return false, ""
	}
	et := p.ClassifyError(snap)
	logger.Warnf("[BrowserExec] 平台拦截页命中 platform=%s errType=%s session=%d", p.Identifier(), et, session.ID)
	switch et {
	case platform.ErrDisconnect:
		return true, fmt.Sprintf("平台风控拦截（%s）：检测到验证码/风控页，账号需人工介入，会话终止", p.Identifier())
	default:
		// refresh_token/bad_body/retry 类：记录但不终止（bad_body 语义是内容被拒，页面上不会常驻）
		return false, ""
	}
}

// dispatchStep 按动作分发到 Hand 原语
func (e *Executor) dispatchStep(ctx context.Context, task *model.BrowserTask, session *model.BrowserSession, step parsedStep) error {
	userID := task.UserID
	tabID := session.ChromeTabID
	p := buildStepParams(step)
	e.lastStepResult = nil

	switch step.Action {
	case "open_tab":
		openURL := step.Target
		if openURL == "" {
			openURL = task.Url // 编排未填 target 时兜底任务起始 URL
		}
		tabID, err := e.hand.openTab(ctx, userID, openURL, false) // active 恒 false
		if err != nil {
			return err
		}
		session.ChromeTabID = tabID
		_ = e.sessionRepo.UpdateChromeTabID(ctx, session.ID, tabID)
		return e.recordResult(map[string]any{"chrome_tab_id": tabID})
	case "click":
		return e.hand.click(ctx, userID, tabID, step.Target)
	case "type":
		return e.hand.typeText(ctx, userID, tabID, step.Target, step.Value, p.ClearFirst, p.SubmitOnEnter)
	case "click_near":
		// 以 Anchor CSS 为基准点击容器内指定文本的 button（发送/提交按钮无稳定 class 场景）
		return e.hand.clickNear(ctx, userID, tabID, step.Anchor, step.ButtonText)
	case "post_comment":
		// 一站式发评论：扩展侧定位输入框→CDP trusted 键入→trusted 点发送→就地验证渲染。
		// 选择器四元组由平台适配器（L3）下发——扩展零平台知识（设计稿 P5）；
		// 未声明 post_comment 能力的平台在此 fails-loudly（P4 契约默认失败）。
		locs, err := platform.CommentLocatorsFor(ctx, taskPlatformID(task))
		if err != nil {
			return err
		}
		res, err := e.hand.postComment(ctx, userID, tabID, step.Value, map[string]any{
			"input_selector":    locs.InputSelector,
			"send_button_text":  locs.SendButtonText,
			"comment_container": locs.CommentContainer,
			"comment_item_text": locs.CommentItemText,
		})
		if err != nil {
			return err
		}
		return e.recordResult(map[string]any{"posted": res["posted"], "verified": res["verified"]})
	case "assert":
		// 断言类原语（洞察层）：contains_text / selector_exists，失败即抛错（Playwright expect 语义）
		timeout := p.TimeoutMs
		if timeout <= 0 {
			timeout = 5000
		}
		return e.hand.assert(ctx, userID, tabID, p.AssertKind, step.Value, timeout)
	case "query":
		// 只读洞察原语：text/exists/count/attr，返回数据不抛错（Midscene 洞察类语义）
		res, err := e.hand.query(ctx, userID, tabID, p.QueryKind, step.Target)
		if err != nil {
			return err
		}
		return e.recordResult(res)
	case "snapshot":
		snap, err := e.hand.snapshot(ctx, userID, tabID)
		if err != nil {
			return err
		}
		_ = e.sessionRepo.UpdateTitleAndSnapshot(ctx, session.ID, "", snap)
		return e.recordResult(map[string]any{"snapshot_chars": len(snap), "snapshot": snap})
	case "markdown":
		md, err := e.hand.markdown(ctx, userID, tabID)
		if err != nil {
			return err
		}
		return e.recordResult(map[string]any{"markdown_chars": len(md), "markdown": md})
	case "screenshot":
		b64, err := e.hand.screenshot(ctx, userID, tabID, true)
		if err != nil {
			return err
		}
		if b64 != "" && e.feedback != nil {
			if url, err := e.feedback.SaveFinalScreenshot(ctx, session.ID, b64); err != nil {
				logger.Warnf("[BrowserExec] 截图落库失败 session=%d: %v", session.ID, err)
			} else if url != "" {
				session.FinalScreenshotURL = url
				_ = e.sessionRepo.UpdateArtifacts(ctx, session.ID, nil, url, "", "")
			}
		}
		return e.recordResult(map[string]any{"screenshot_url": session.FinalScreenshotURL, "screenshot_b64_chars": len(b64)})
	case "wait":
		return e.hand.waitFor(ctx, userID, tabID, p.Ms)
	case "wait_for_selector":
		timeout := p.TimeoutMs
		if timeout <= 0 {
			timeout = 10000
		}
		return e.hand.waitForSelector(ctx, userID, tabID, p.Selector, timeout)
	case "scroll":
		return e.hand.scroll(ctx, userID, tabID, p.Direction, p.Amount)
	case "extract":
		// Brain 兼容：LLM 常把单选择器放 target 而不是 selectors map——基座层归一（铁律 3：大模型驱动容错）
		selectors := p.Selectors
		if len(selectors) == 0 && strings.TrimSpace(step.Target) != "" {
			selectors = map[string]string{"target": strings.TrimSpace(step.Target)}
		}
		data, err := e.hand.extract(ctx, userID, tabID, selectors)
		if err != nil {
			return err
		}
		// 合并本 session 各次 extract 到 extracted_data（追溯面板读这里）
		merged, ok := data["data"].(map[string]any)
		if !ok {
			merged = map[string]any{"raw": data}
		}
		existing := map[string]any{}
		if len(session.ExtractedData) > 0 {
			_ = json.Unmarshal(session.ExtractedData, &existing)
		}
		for k, v := range merged {
			existing[k] = v
		}
		blob, err := json.Marshal(existing)
		if err == nil {
			_ = e.sessionRepo.UpdateExtractedData(ctx, session.ID, blob)
			session.ExtractedData = blob
		}
		return e.recordResult(merged)
	case "close_tab":
		return e.hand.closeTab(ctx, userID, tabID)
	default:
		return fmt.Errorf("未知动作: %s", step.Action)
	}
}

// recordResult 缓存原语回包供 executeStepWithRetry 落库到 step.result
func (e *Executor) recordResult(payload map[string]any) error {
	if len(payload) == 0 {
		return nil
	}
	if blob, err := json.Marshal(payload); err == nil {
		e.lastStepResult = blob
	}
	return nil
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
