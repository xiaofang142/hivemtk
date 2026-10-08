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

// brainPlanner Executor 对 Brain 层的全部依赖（从具体 *BrainService 改接缝）。
// 动机不是解耦好看：*BrainService 让整条 executeBrain 循环在单测里无法驱动（plan 必然打真 LLM），
// 于是「闸门是否在落库前生效」这类事实只能停在谓词层。有了这一层，测试可以塞进脚本化 plan，
// 其余部件（真 WS、真 DB、真步循环）全部照旧。
type brainPlanner interface {
	GeneratePlanReflect(ctx context.Context, taskID, sessionID uint, goal, platformID, snapshot string, st *reflectState) (stepsJSON []byte, done bool, err error)
	LastPlanTokens() int
	LastPlanModel() string // Chunk4：审计帧每步的 llm_model（空串=该步没走 LLM，调用方不写该字段）
	LastAuxTokens() int
	JudgeDone(ctx context.Context, taskID, sessionID uint, goal, evidence string) (approve bool, reason string)
	SummarizeSession(ctx context.Context, taskID, sessionID uint, goal string, success, total int, extracts string) string
}

// Executor 执行引擎：steps 解释 + Hand 调用 + Session 流转 + 停止/超时控制。
// 状态收口原则：session/step 状态只由 Executor 写（替代 v1 的 AfterFunc 直改状态）。
type Executor struct {
	hand        *Hand
	sessionRepo repository.BrowserSessionRepository
	stepRepo    repository.BrowserStepRepository
	cmdLogRepo  repository.BrowserCommandLogRepository
	// writeClaimRepo 不可逆写的存储层独占声明。它**不**是台账的缓存或兜底——
	// 台账（stepRepo）回答「历史上发生过尝试没有」，本仓储回答「此刻谁有权去跨提交点」，
	// 后者只能由一条 INSERT 的原子性给出，所以 Executor 上不放任何内存态。
	// nil（未接线）在写步上是 fail-close，见 claimWriteSlot。
	writeClaimRepo repository.BrowserWriteClaimRepository
	brain          brainPlanner
	feedback       *FeedbackService

	// jevClient JEV 步决策客户端（nil=按环境变量即时构造；默认关闭，见 jev.go）。
	// 测试经 SetJevClient 注入 fake server 指向的客户端。
	jevClient *JevClient

	// outreachReceiptRepo 触达回执仓储（Chunk5：验收交付物 = 截图 + 帖子链接 + 文案快照）。
	// nil=未接线（回执全空，执行不受影响）——回执是增强层，落不落得住都不改「已发布」这个事实。
	outreachReceiptRepo repository.BrowserOutreachReceiptRepository

	// pendingReceipts sessionID → 待落回执行（Chunk5 收口期挂起，见 outreach_receipt.go）。
	// 只在「本会话已经发过触达」这一罕见路径上有内容，纯只读任务恒为空。
	receiptMu       sync.Mutex
	pendingReceipts map[uint][]*receiptPending

	// profileHealthRepo 主 Profile 健康熔断仓储（Chunk2：单主 Profile 健康监护）。
	// nil=未接线（熔断写入跳过，执行不受影响）——熔断是增强不是门禁。
	profileHealthRepo repository.BrowserProfileHealthRepository

	// outreachDedupeRepo 跨任务触达去重仓储（Chunk3：同一平台同一目标同一动作同一文案只触达一次）。
	// nil=未接线（去重检查跳过，执行不受影响）——去重是防扰民层，真安全网是写声明与台账。
	outreachDedupeRepo repository.BrowserOutreachDedupeRepository

	// taskRepo 任务仓储（Chunk4：活动触达条数预算的原子扣减落在这里）。
	// nil=未接线 ⇒ **不扣减也不拦停**（fail-open），与存量行为一致。
	// 之所以敢 fail-open：预算的失败形态是「多打扰了几次」，而双发的失败形态是「用户被重复骚扰
	// 且无法撤回」。预算若因接线缺失而把整条触达线锁死，那是更大的事故；真要收紧预算强度，
	// 该在 TaskService 启动门做（见 task.go 的 profileHealth 启动门范式）。
	taskRepo repository.BrowserTaskRepository

	// relocateLLM A1 自愈 LLM 接缝（默认 defaultRelocateLLM；测试替换免真机 LLM）
	relocateLLM func(ctx context.Context, systemPrompt, prompt string) (relocateOutcome, error)

	stopMu       sync.Mutex
	stopRegistry map[uint]chan struct{} // sessionID → stopCh

	// confirmRegistry D7：sessionID → 待放行闸门（仅在写步提交点前挂起时存在）。
	// 与 stopRegistry 同构但生命周期不同：stop 通道随 session 全程注册，确认通道只在等待期存在
	// ——「存在即挂起」使 ConfirmPending 无需额外状态位。
	// 存的不再是裸通道而是闸门自身——「等谁批、批的是哪份载荷、什么时候到期」
	// 必须和通道同生死，否则读侧只有一个布尔，放行就是一张空白支票。
	confirmMu       sync.Mutex
	confirmRegistry map[uint]*confirmGate

	// 台账写失败后的两张降级表，与上面两张同构但语义相反——它们是「不能再派发写」的记录。
	//   ledgerBroken: sessionID → 最初那次台账写失败的原因，随会话结束清除（会话级降级）；
	//   ledgerGaps:   "taskID|text_hash" → 提交尝试点已跨越但库里没落成，跨会话保留到进程结束
	//                 （自动重试是同进程换新 session 跑同一任务，会话级降级挡不住它）。
	// 两张表都只在「台账写失败」这条罕见路径上增长，各自有界（gaps 见 ledgerGapCap）。
	// gaps 不跨重启 ⇒ 反馈层挂重试前必须查它一次（B2，接线见 NewExecutor）。
	ledgerMu       sync.Mutex
	ledgerBroken   map[uint]string
	ledgerGaps     map[string]bool
	ledgerGapOrder []string

	// R-A4（2026-09-19）：原 lastStepResult 字段已删——Executor 是进程级单例、
	// 多 session 并发触达，字段传值既是数据竞态又会跨 session 串包；
	// 回包改由 dispatchStep 返回值沿调用栈传递。
}

func NewExecutor(hand *Hand, sessionRepo repository.BrowserSessionRepository, stepRepo repository.BrowserStepRepository, brain *BrainService, feedback *FeedbackService) *Executor {
	e := &Executor{
		hand:         hand,
		sessionRepo:  sessionRepo,
		stepRepo:     stepRepo,
		relocateLLM:  defaultRelocateLLM,
		stopRegistry: make(map[uint]chan struct{}),

		confirmRegistry: make(map[uint]*confirmGate),

		ledgerBroken: make(map[uint]string),
		ledgerGaps:   make(map[string]bool),

		pendingReceipts: make(map[uint][]*receiptPending),
	}
	// 不把 *BrainService(nil) 直接赋给接口字段：那会得到一个「非 nil 的 nil 接口」，
	// e.brain == nil 判假，随后 executeBrain 就在 nil receiver 上发起真调用。
	if brain != nil {
		e.brain = brain
	}
	e.feedback = feedback
	if feedback != nil {
		// 单点接线：缺口表长在 Executor 上、挂自动重试的动作在 FeedbackService 上，
		// 两边互相看不见。接在构造处而不是某个调用点——漏接线时「不重试」的抑制会整条静默消失，
		// 与A11 那条「把错误就地吞掉等于闸门不存在」是同一个失效形态。
		feedback.SetLedgerGapProvider(e.HasCrossedLedgerGap)
	}
	return e
}

// finishStep 步行终态落库（唯一写入口）。
//
// 为什么不能沿用执行 ctx：超时/中止腿上 ctx 此刻必然已 Done，用它的 UPDATE 会被 DB 驱动取消，
// 于是这一行永远停在 running——「写步失败是可见、可人工重跑的」这条承诺就地失效，
// 运维在面板上看不到「这一步为什么被拦」，只剩一条日志。R25 只对 session/task 行做了这个
// 处理（sessionFinalWriteBudget），步行是同一缺陷的第三半。
// 失败必须上报（旧写法是 `_ =`）：session/task 行有 stale_reconcile 兜底收敛，步行没有。
func (e *Executor) finishStep(ctx context.Context, stepRowID uint, status string, result []byte, durationMs int64, msg string) {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stepFinalWriteBudget)
	defer cancel()
	if err := e.stepRepo.UpdateResult(writeCtx, stepRowID, status, result, durationMs, msg); err != nil {
		logger.Errorf("[BrowserExec] 步终态落库失败 step=%d status=%s（面板将看不到这一步的结论，需从日志核对）: %v", stepRowID, status, err)
	}
}

// SetCommandLogRepository 日志仓储注入（路由装配可选——nil 时静默跳过埋点）
func (e *Executor) SetCommandLogRepository(r repository.BrowserCommandLogRepository) {
	e.cmdLogRepo = r
}

// SetWriteClaimRepository 存储层写声明闸门注入。
// 与上面的日志注入**相反**，这里 nil 不是「可选」而是「闸门没接好」：写步会一律拒绝下发
// （见 claimWriteSlot）。宁可把任务跑成一屏红步，也不开一条没有独占裁决的写路径——
// 少接一行装配是可见的、可当天补的，一次双发是不可见、撤不回的。
func (e *Executor) SetWriteClaimRepository(r repository.BrowserWriteClaimRepository) {
	e.writeClaimRepo = r
}

// SetJevClient JEV 客户端注入（测试/未来 kv 接线用；nil=按环境变量即时构造）。
func (e *Executor) SetJevClient(c *JevClient) {
	e.jevClient = c
}

// SetProfileHealthRepo 主 Profile 健康仓储注入（路由装配可选——nil 时熔断写入跳过）。
func (e *Executor) SetProfileHealthRepo(r repository.BrowserProfileHealthRepository) {
	e.profileHealthRepo = r
}

// SetOutreachDedupeRepository 触达去重仓储注入（路由装配可选——nil 时去重检查跳过）。
func (e *Executor) SetOutreachDedupeRepository(r repository.BrowserOutreachDedupeRepository) {
	e.outreachDedupeRepo = r
}

// SetTaskRepository 任务仓储注入（Chunk4：活动触达条数预算扣减；nil 时不扣减）。
func (e *Executor) SetTaskRepository(r repository.BrowserTaskRepository) {
	e.taskRepo = r
}

// appendCommandLog append-only 命令-事件日志（P8）。失败仅告警不阻断执行：
// 日志是审计增强，不能反过来拖垮业务执行路径。
// seq 由调用方传入（session 局部计数）——Executor 是多 session 共享单例，
// 共享计数器在并发 session 下是 data race 且 seq 跨 session 交错（P0-2 修复）。
// ok 是这一帧自带的结论，三态（见 model.BrowserCommandLog.Ok）：nil=此帧不携带结论，
// 下发帧恒为 nil——下发那一刻「成/败」都还没发生，填任何一个值都是替读者编一个结论。
func (e *Executor) appendCommandLog(ctx context.Context, sessionID, taskID, stepID uint, seq int, direction, action string, payload any, durationMs int64, ok *bool) {
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

// verdict 把一个布尔观测升成审计帧的三态结论。名字刻意不叫 ok()：调用点写 verdict(err == nil)
// 才读得出「这是这一帧的结论」，而不是「这次成功了」。
func verdict(v bool) *bool { return &v }

// auditEventPayload 写步回包进审计流前的收口：丢掉 evidence 键。
// comment_verify 的回包含「页面上命中那条评论的文本节选」（primitives.js evidenceOf），
// 而命中项就是我们自己发出去的正文——写进 browser_steps.result / extracted_data 是追溯面板
// 要的凭据，写进 append-only 命令流则是把用户内容带进 I5 离线导出件。审计流要的是结论
// （已受理/已确认/未确认），内容证据另有其位。
func auditEventPayload(result []byte) []byte {
	if len(result) == 0 {
		return result
	}
	var m map[string]any
	if err := json.Unmarshal(result, &m); err != nil {
		return result // 非对象回包（数组/标量）原样进流：收口只针对带 evidence 的那一类
	}
	if _, has := m["evidence"]; !has {
		return result
	}
	delete(m, "evidence")
	blob, err := json.Marshal(m)
	if err != nil {
		return result
	}
	return blob
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

// PendingConfirmGate 闸门的对外可见身份：一次挂起「在等谁批、批的是哪份载荷、
// 什么时候到期」必须可查证。Preview 只在读侧（归属校验之后）出现，绝不进审计帧。
type PendingConfirmGate struct {
	SessionID   uint      `json:"session_id"`
	StepIndex   int       `json:"step_index"`
	PayloadHash string    `json:"payload_hash"`
	Preview     string    `json:"preview"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// confirmGate 一次挂起的全部事实。载荷哈希在写下时定死、闸门存续期间不许改——
// 「批的是哪份载荷」有唯一答案，比对才有意义（A5 的反面教材：LangGraph interrupt 对
// resume 值完全不校验，等于任何一次放行都可为任何一份载荷背书）。
type confirmGate struct {
	ch          chan struct{}
	sessionID   uint
	stepIndex   int
	payloadHash string
	preview     string
	expiresAt   time.Time
	// budget 挂起时给定的名义预算：超时归因要说「等了多久」，用 time.Until(expiresAt) 在
	// 到期那一刻恒等于 0，等于把归因文案写成谎。
	budget time.Duration

	// mismatchAttempts 拿错载荷来要的放行次数。放行不撤闸门，所以这个数只能由闸门自己记，
	// 事后无从重建；judge 帧收口时把它一起落下去（A9：拒过谁来，和批没批同样值得审计）。
	mu               sync.Mutex
	mismatchAttempts int
}

func (g *confirmGate) attempts() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.mismatchAttempts
}

func (g *confirmGate) info() PendingConfirmGate {
	return PendingConfirmGate{
		SessionID: g.sessionID, StepIndex: g.stepIndex, PayloadHash: g.payloadHash,
		Preview: g.preview, ExpiresAt: g.expiresAt,
	}
}

// ConfirmVerdict 放行请求的结论。三态而不是 bool：
// 「没有闸门」「批错了东西」「批对了」是三件不同的事，压成一句文案就等于让运维去猜。
// 命名前缀 Verdict 与闸门自己的出路 confirmOutcome 分开——两者都叫「granted」时，
// 「放行命中了」和「等待方被放行」会在读码时互相顶替。
type ConfirmVerdict uint8

const (
	VerdictNoGate   ConfirmVerdict = iota // 该 session 当前没有挂起闸门
	VerdictGranted                        // 载荷相符，已放行（一次性消费）
	VerdictMismatch                       // 有闸门，但批的不是这份载荷——已拒且闸门仍在
)

// SignalConfirm POST /sessions/:id/confirm 放行。payloadHash 是**被批准的那份载荷**的指纹，
// 由读侧（GET /sessions/:id/confirm-gate）取得后原样带回；不符即 VerdictMismatch——
// 不撤闸门、不下发、只计数（fail-closed，且「再问一次」拿不到放行）。
// 先摘后关：同一通道只可能被 close 一次，第二次请求落到 VerdictNoGate。
// 空请求不需要特判：闸门的哈希在开闸时已保证非空（见 awaitConfirmGate 的前置），
// 所以「没带载荷」天然落进不等式这一侧。
func (e *Executor) SignalConfirm(sessionID uint, payloadHash string) ConfirmVerdict {
	e.confirmMu.Lock()
	g, ok := e.confirmRegistry[sessionID]
	if !ok {
		e.confirmMu.Unlock()
		return VerdictNoGate
	}
	if payloadHash != g.payloadHash {
		g.mu.Lock()
		g.mismatchAttempts++
		g.mu.Unlock()
		e.confirmMu.Unlock()
		return VerdictMismatch
	}
	delete(e.confirmRegistry, sessionID)
	e.confirmMu.Unlock()
	close(g.ch)
	return VerdictGranted
}

// ConfirmPending 该 session 是否正停在确认闸门（读侧暴露给前端按钮可见性）
func (e *Executor) ConfirmPending(sessionID uint) bool {
	e.confirmMu.Lock()
	defer e.confirmMu.Unlock()
	_, ok := e.confirmRegistry[sessionID]
	return ok
}

// PendingGate 读取挂起闸门的详情（第二返回值 false=没有在等人工放行）。
func (e *Executor) PendingGate(sessionID uint) (PendingConfirmGate, bool) {
	e.confirmMu.Lock()
	defer e.confirmMu.Unlock()
	g, ok := e.confirmRegistry[sessionID]
	if !ok {
		return PendingConfirmGate{}, false
	}
	return g.info(), true
}

// registerConfirmGate 登记一次挂起并返回它——与 unregister 配对，且「同一 session 只可能有一个
// 在等的闸门」由这里唯一裁决（后到者覆盖：闸门是每用户 1 个 running session 的串行面，
// 真出现覆盖说明上游串行闸已失效，覆盖本身留在那条旧通道的最终归宿里，不额外报错）。
func (e *Executor) registerConfirmGate(g *confirmGate) *confirmGate {
	e.confirmMu.Lock()
	e.confirmRegistry[g.sessionID] = g
	e.confirmMu.Unlock()
	return g
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
// 起 post_comment 与派生写步（type+回车 / click 发送按钮）共用此原文——F4 的精确匹配
// 只认这一条，措辞按步分叉就会把「闸门处中止」重新掉回 failed。
const errConfirmAbortedByStop = "写步等待人工确认期间被用户中止，评论未提交"

// waitForConfirm 挂起等人工放行，自带独立计时器（解耦：不再让 execCtx.Done 兼职确认超时）。
// 前置：闸门已由 registerConfirmGate 挂上注册表——**登记早于落帧、落帧早于等待**，
// 反过来排会在「帧已写、注册表还空着」的窗口里把一次真放行答成「没有待确认的提交点」。
// 返回值第二项是**哪条预算到头**的原文（「确认等待 600s」还是「任务执行预算掐断」）——
// 两者在审计面上必须可区分，否则人就会去调 timeout_sec 而真正该调的是 confirm_wait_sec。
// 调用点在不可逆提交点之前，非 confirmGranted 分支从未点击过任何按钮——失败可安全重下发，
// 不违「单次提交禁重试」红线（那是「已提交且结局未知」的专属纪律）。
// stopChFor 未注册时返回 nil，select 对 nil 通道分支永不就绪，与 ctx.Done 并存安全。
func (e *Executor) waitForConfirm(ctx context.Context, gate *confirmGate) (confirmOutcome, string) {
	defer func() {
		e.confirmMu.Lock()
		if cur, ok := e.confirmRegistry[gate.sessionID]; ok && cur == gate {
			delete(e.confirmRegistry, gate.sessionID)
		}
		e.confirmMu.Unlock()
	}()

	wait := time.Until(gate.expiresAt)
	if wait < 0 {
		wait = 0
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	stopCh := e.stopChFor(gate.sessionID)
	select {
	case <-gate.ch:
		return confirmGranted, ""
	case <-stopCh:
		return confirmStoppedByUser, "用户在确认闸门处中止"
	case <-timer.C:
		return confirmWaitTimedOut, fmt.Sprintf("人工确认等待 %ds", int(gate.budget.Seconds()))
	case <-ctx.Done():
		// 执行预算（TimeoutSec）到头。它与确认预算是两条独立计时器，谁先到谁说话。
		return confirmWaitTimedOut, "任务执行预算用尽"
	}
}

// gateDecision 把闸门出路翻成审计面上的词（F4 的对偶：终态能分辨「谁否决了这次提交」，
// 帧也必须分辨——granted/stopped/timeout 三词在 command_log 里各占一类，不许合并）。
func gateDecision(out confirmOutcome) string {
	switch out {
	case confirmGranted:
		return "granted"
	case confirmStoppedByUser:
		return "stopped"
	default:
		return "timeout"
	}
}

// d7PreviewRunes 待批内容预览的截断长度：闸门读侧要让人「看见自己批的是什么」（A5 的前提），
// 但一个 8000 字的长评论不该整段塞进弹窗——预览是给人核对用的，不是正文的第二份副本。
const d7PreviewRunes = 200

// D7 闸门的两类审计帧动作名。写侧在这里、读侧在 session.go 的 gateElsewhere——
// 两处各写一遍字面量，就会有一个拼错的常量悄悄把跨进程查证变成死代码（帧一直在落，没人查得到）。
const (
	gateWaitFrame    = "d7_wait"    // event：闸门开过、批的是这份载荷、到这个时候
	gateConfirmFrame = "d7_confirm" // judge：闸门收口，decision 分辨 granted/stopped/timeout
)

// 写链路上「试过了 / 接了 / 平台确认了」三件事的另两个名字。
// 三态不是给一列改名，而是每一态各有一帧可指：
//   - command 帧（ok=nil）= attempted：这一条命令被写给了 Host，此刻没有任何结论；
//     comment_send 单独成帧，是因为不可逆点埋在 post_comment 那一步内部——只有步级帧的审计流
//     分不清「填好文本没点出去」与「点出去了不知道成没成」，而这两者的处置完全相反。
//   - event 帧 = accepted：Host 回执到手且无错。
//   - judge 帧 write_confirm = confirmed：服务端收紧回查裁出的结论（state 带 verified /
//     unattributed）。它是审计流里唯一有资格回答「评论发出去了吗」的一帧，
//     且只带载荷哈希——正文证据在 extracted_data 与步结果里，不进离线导出件（I5）。
const (
	commentSendAction = "comment_send"
	writeConfirmFrame = "write_confirm"
)

// awaitConfirmGate D7 闸门的唯一入口（A5 绑载荷 + A9 留痕 + A10 可跨进程查证）。
// 三件事必须同生同死，所以不做成调用方各自记得加的三步——漏一步的形状与漏一个闸门同构：
//   - 等待之前落 d7_wait 帧（登记之后）：这是「闸门确实开过、批的是这份载荷、到什么时候」的唯一落盘凭据。
//     进程内 map 在重启/多副本下什么都不是（A10 立项理由），而帧在库里。
//   - 载荷只带哈希不带正文：I5 审计导出会把 payload 原样带进离线件，评论正文不该住在那儿。
//   - 出路收口时落 d7_confirm 帧（judge 类）：连「没人批、超时了」都要留一行，
//     否则审计面上「用户拒了」与「用户没来」又折成一句。
func (e *Executor) awaitConfirmGate(ctx context.Context, task *model.BrowserTask,
	session *model.BrowserSession, stepID uint, stepIndex int, preview, payloadHash string,
	seq *int, wait time.Duration) (confirmOutcome, string) {
	// 绑一份「空载荷」的闸门等于没绑：任何人都能以「我也批了这份」放行。调用方的键来自
	// writeStepKey（永不为空，见 ledger_b16b_test.go ④-b），这里只把那条前提变成硬前置——
	// 前提哪天被绕过，是编排 bug，该炸在原地而不是悄悄开一扇匿名放行口。
	if payloadHash == "" {
		panic("awaitConfirmGate: 空载荷哈希不得开闸门")
	}
	gate := &confirmGate{
		ch: make(chan struct{}), sessionID: session.ID, stepIndex: stepIndex,
		payloadHash: payloadHash, preview: truncateRunes(preview, d7PreviewRunes, "…"),
		expiresAt: time.Now().Add(wait), budget: wait,
	}
	// 登记必须先于落帧：帧一到，读侧（GET /sessions/:id/confirm-gate 与跨进程查证）就认为
	// 有人能放行，而此刻注册表还空着的话，一次真放行会被答成「没有待确认的提交点」。
	e.registerConfirmGate(gate)
	*seq++
	e.appendCommandLog(ctx, session.ID, task.ID, stepID, *seq, "event", gateWaitFrame, map[string]any{
		"payload_hash": payloadHash, "step_index": stepIndex,
		"expires_at": gate.expiresAt.Format(time.RFC3339Nano),
		"wait_sec":   int(wait.Seconds()),
	}, 0, verdict(true))

	start := time.Now()
	out, why := e.waitForConfirm(ctx, gate)
	waited := time.Since(start).Milliseconds()

	*seq++
	e.appendCommandLog(ctx, session.ID, task.ID, stepID, *seq, "judge", gateConfirmFrame, map[string]any{
		"payload_hash":      payloadHash,
		"step_index":        stepIndex,
		"decision":          gateDecision(out),
		"mismatch_attempts": gate.attempts(),
		"waited_ms":         waited,
	}, waited, verdict(out == confirmGranted))
	return out, why
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
		time.Since(start).Milliseconds(), verdict(err == nil))
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

// isNeverDispatched 扩展侧 CDP **attach 阶段**就没成功 ⇒ 一条输入事件都没下发过 ⇒ 零副作用。
//
// 为什么必须单独一类：cdp/input.js 的 withDebugger 是「先 attach、再跑 fn(target)」。
// attach 撞自己的 deadline（cdp/input.js 的 CDP_ATTACH_DEADLINE_MS）时 fn 压根没被调用，
// 页面上不可能发生过点击/键入——它与 cdp_send_deadline / click_unacked / cdp_command_deadline
// 那一族有本质区别：后三者都意味着**事件已入队进渲染进程**（结局未知，不可重发）。
//
// 漏判的代价是实打实的双发闸误伤：若把它当「结果未知」，台账落 unattributed，
// 该文本从此进拦阻集合（write_ledger.go guardResubmit 连 unattributed 一起拦），
// 用户看清页面重跑时会被告知「已有提交尝试」——而实际一次都没发出去。
// 真机依据：session627-629 的 comment_prep/comment_send 曾整段卡死，修复后 deadline 首次
// 成为可达路径，这个归类必须同时落到位，否则修好挂死反而锁死文本。
func isNeverDispatched(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// 只认 attach 两处；send/command 那一族是「已入队」，绝不能并进来。
	return strings.Contains(msg, "cdp_attach_deadline")
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
				status, errMsg, _ := e.executeStepWithRetry(ctx, task, session, i, step, stopCh, &cmdSeq, "")
				switch status {
				case "success":
					success++
				case "skipped":
					// 写步在自动重试轮撞见历史提交尝试 → 整步不下发。两种事实分开判：
					//   前一轮 verified → 目标已达成，本轮只是补完剩余步，不计成败也不中断；
					//   前一轮 sent/unattributed → 提交从未被证明，本轮防双发也无从证明，
					//     必须让本轮判红（否则「重试轮全绿」就是双发闸要消灭的那类假绿），
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
							// 熔断写入：同平台新任务启动门 + 人工恢复（best-effort，不改终止语义）
							_ = e.recordProfileBlocked(ctx, task, session, reason)
						}
						break loop
					}
				}
				// 拦截页检测（铁律 4）：成功步后页面可能已跳风控页——disconnect 类终止，全自动闭环
				if blocked, reason := e.detectBlockedIfFatal(ctx, task, session, &cmdSeq); blocked {
					sessionFailed = reason
					// 熔断写入：同平台新任务启动门 + 人工恢复（best-effort，不改终止语义）
					_ = e.recordProfileBlocked(ctx, task, session, reason)
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
	// Chunk5 触达回执：拍验收截图 + 落本会话的回执行。
	// **必须在 cleanupSessionTab 之前**：tab 一回收，captureVisibleTab 就只能截到用户
	// 自己的页面（静默假内容，比没有更坏），顺序颠倒等于回执永远拍不到真页面。
	e.captureSessionReceipts(writeCtx, task, session, &cmdSeq)
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
	// Chunk4：预算可由任务字段 TokenBudget 覆盖（0=用默认常量），取数唯一入口 taskTokenBudget。
	tokenUsed := 0
	tokenBudget := taskTokenBudget(task)
	// JEV session 级熔断/独立预算（Executor 进程级单例不放计数，随 task 结束丢弃）
	jevSess := &jevSessionState{}
	// wall-clock 看门狗（R22）：ctx 取消链在某些 LLM/DB 调用栈不生效（session132 实测 11min+ active），
	// 以真实时钟兜底——超执行预算+30s 强制收敛，会话必有终态。
	// 预算走 taskExecBudget（Brain 模式下 LLM 也能编排出写步，确认挂起同样要留出时长）
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
			tabID, _, err := e.hand.openTab(ctx, task.UserID, task.Url, false, 0)
			if err != nil {
				return success, failed, "open_tab 失败: " + err.Error()
			}
			session.ChromeTabID = tabID
			_ = e.sessionRepo.UpdateChromeTabID(ctx, session.ID, tabID)
		}
		snap, pageURL, err := e.hand.snapshot(ctx, task.UserID, session.ChromeTabID)
		if err != nil {
			// tab 可能在 LLM 思考间隙被 SW 空闲回收/用户关闭：重开一次再 snapshot
			logger.Warnf("[BrowserExec] brain snapshot 失败 session=%d tab=%d: %v，尝试重开 tab", session.ID, session.ChromeTabID, err)
			tabID, _, openErr := e.hand.openTab(ctx, task.UserID, task.Url, false, 0)
			if openErr != nil {
				return success, failed, "snapshot 失败且重开 tab 失败: " + openErr.Error()
			}
			session.ChromeTabID = tabID
			_ = e.sessionRepo.UpdateChromeTabID(ctx, session.ID, tabID)
			cmdSeq++ // P2-1 自愈动作落审计
			e.appendCommandLog(ctx, session.ID, task.ID, 0, cmdSeq, "event", "open_tab_recovery", map[string]any{"reason": "snapshot_failed", "new_tab": tabID}, 0, verdict(true))
			if snap, pageURL, err = e.hand.snapshot(ctx, task.UserID, session.ChromeTabID); err != nil {
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
		// JEV 优先、Brain 回退（设计步骤四 §2：planRound 内决策；JEV 未启用/失败即原路径）。
		planOut, err := e.planRound(ctx, task, session, snap, pageURL, st, history, &cmdSeq, jevSess)
		stepsJSON, done, terminal := planOut.stepsJSON, planOut.done, planOut.terminal
		if terminal != "" {
			return success, failed, terminal
		}
		prevEvaluation, memory = st.PrevEvaluation, st.Memory
		tokenUsed += planOut.brainTokens // P1-1 session 级 token 计量（Brain 口径；JEV 走独立预算）
		jevSess.tokensUsed += planOut.jevTokens
		if jevSess.tokensUsed > jevSessionTokenBudget {
			jevSess.off = true // JEV 独立预算超限：本 session 剩余轮次直走 Brain
		}
		if planOut.jevAttempted && !planOut.jevOK {
			jevSess.fails++
			if jevSess.fails >= jevMaxSessionFails {
				jevSess.off = true // JEV 连续失败熔断：剩余轮次直走 Brain
			}
		}
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
				map[string]any{"approve": approve, "reason": reason}, 0, verdict(approve))
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
			// Brain 的 steps 是 LLM 原文 Unmarshal 出来的，REST 那条 oneof 校验在这条路上
			// 一行都不跑——落库前先过服务端闸门（G17 的 screenshot 闸是它的一条已存在的腿）。
			if rej := brainPlanStepRejection(it.Action); rej != "" {
				logger.Warnf("[BrowserExec] brain 轮内步骤被服务端拒绝 session=%d: %s", session.ID, rej)
				history = appendHistoryBounded(history, rej, foldedHistory)
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
			status, errMsg, stepResult := e.executeStepWithRetry(ctx, task, session, stepIdx, step, stopCh, &cmdSeq, pageURL)
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
				// Brain 重试轮里的写步跳过，按前一轮台账态分判（见 ExecuteSession 同分支说明）。
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
			// 熔断写入：同平台新任务启动门 + 人工恢复（best-effort，不改终止语义）
			_ = e.recordProfileBlocked(ctx, task, session, reason)
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
// pageURL：调用方手上的当前页 URL 现货（brain 模式传本轮 snapshot 的 URL；显式编排无现货传 ""，
// 由 resolveOutreachDedupe 按需快照补齐）。只用于触达去重键的归一化，查不到即 fail-open 放行。
func (e *Executor) executeStepWithRetry(ctx context.Context, task *model.BrowserTask, session *model.BrowserSession, index int, step parsedStep, stopCh chan struct{}, seq *int, pageURL string) (string, string, json.RawMessage) {
	// 写步判定先于落库——is_write 要作为事实随步行一起存，
	// 事后从 action 名字反推会把「type+回车提交」这类隐形写漏掉。
	// 三态分类。effectUnknown（平台定位表取不到）与 effectWrite 同样进闸门，
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
	// 双发闸 + 重试豁免：闸门必须在任何帧下发之前——prep/type 本身会改页面状态
	// （把草稿塞进输入框），不是「零副作用探测」。
	writeKey := ""
	// outreachCtx：写步去重键（命中跳过时为 nil，未命中但已备好键时非 nil）。
	// 下传给 dispatchStep，供 verified 后落去重行——检查与插入之间页面可能跳转，
	// 键必须在检查点冻结，不能等到提交后重新取 URL/重算 hash。
	var outreachCtx *outreachDedupeCtx
	if writeStep {
		// 本会话台账已经写失败过一次 ⇒ 之后的写步一帧都不下发。
		// 闸门依据的是库里的台账，台账自己写不进去时「过闸」只是走过场——
		// 与其赌一次双发，不如把这一步变成可见、可人工重跑的红步。
		if why, broken := e.ledgerBrokenReason(session.ID); broken {
			msg := fmt.Sprintf("写步拒绝下发（%s）：%s——台账未落，本会话写能力已降级，请人工核对已下发的步后重跑", writeWhy, why)
			e.finishStep(ctx, stepRow.ID, "failed", nil, 0, msg)
			logger.Warnf("[BrowserExec] %s session=%d idx=%d", msg, session.ID, index)
			return "failed", msg, nil
		}
		writeKey = writeStepKey(step)
		// 跨任务触达去重（Chunk3）：同平台 + 同归一化目标页 + 同文案已触达过 ⇒ 本步不下发。
		// 位置排在 writeKey 备齐之后、双发闸之前：去重是「更外层」的扰民拦截，先于会话内
		// 台账判据生效；命中即整步跳过并绿返（目标本就已达成，重发只会二次扰民）。
		// 去重库未接线 / 快照失败 / 查库报错一律 fail-open 放行，绝不因去重拦停发送。
		oc, dupHit, _ := resolveOutreachDedupe(ctx, e, task, step, writeKey, pageURL, session.ChromeTabID)
		if dupHit {
			msg := fmt.Sprintf("触达去重跳过（%s）：%s 已有同文案触达记录，跨任务防重复打扰", writeWhy, oc.targetURL)
			e.finishStep(ctx, stepRow.ID, "skipped", nil, 0, msg)
			*seq++
			e.appendCommandLog(ctx, session.ID, task.ID, stepRow.ID, *seq, "event", step.Action,
				mergeAuditMeta(map[string]any{
					"dedupe_hit": true, "target_url": oc.targetURL, "reason": "outreach_already_sent",
				}, e.stepAuditMeta(0)), 0, verdict(true))
			logger.Infof("[BrowserExec] %s session=%d idx=%d", msg, session.ID, index)
			// 绿返：去重命中意味着内容早已触达，本轮无需再证明什么，不计成败、不中断。
			return "skipped", "", nil
		}
		outreachCtx = oc
		switch err := e.guardResubmit(ctx, task, writeKey, stepRow.ID); {
		case errors.Is(err, errRetrySkipped):
			prior := priorOfSkippedWrite(err)
			msg := fmt.Sprintf("写步跳过（%s）：%v——同文本已有提交尝试，重发即双发", writeWhy, prior)
			e.finishStep(ctx, stepRow.ID, "skipped", nil, 0, msg)
			logger.Infof("[BrowserExec] %s session=%d idx=%d", msg, session.ID, index)
			if prior.verified() {
				return "skipped", "", nil
			}
			// 前一轮只到 sent/unattributed：提交从未被证明，而本轮既不重发（双发）也就无从证明。
			// 此时让整轮判绿就是双发闸要消灭的那类假绿，所以把事实上抛给调用方计败。
			return "skipped", fmt.Sprintf(
				"重试轮防双发未重发，前一轮提交未验证（%v）——本轮无法证明内容已发布，请人工核对", prior), nil
		case err != nil:
			e.finishStep(ctx, stepRow.ID, "failed", nil, 0, err.Error())
			return "failed", err.Error(), nil
		}
		// 读闸放行 ≠ 独占。上面的查询依据的是台账，「两条腿同时过闸、彼此都查空」
		// 是并发的常态形状而非意外，所以裁决权必须下推到库里唯一约束的那一次 INSERT。
		// 位置刻意排在 D7 挂起之前：挂起一分钟，另一条腿在这一分钟里过读闸、下发、提交，
		// 正是本卡要消灭的那次双发——人工确认的等待期是并发窗口最宽的一刻，不是最窄的。
		if err := e.claimWriteSlot(ctx, task.ID, session.ID, stepRow.ID, writeKey); err != nil {
			msg := fmt.Sprintf("写步拒绝下发（%s）：%v", writeWhy, err)
			e.finishStep(ctx, stepRow.ID, "failed", nil, 0, msg)
			logger.Warnf("[BrowserExec] %s session=%d idx=%d", msg, session.ID, index)
			return "failed", msg, nil
		}
		// 腾坑挂在本步收尾（executeStepWithRetry 是步级作用域），不是会话级：
		// 坑留到会话结束会把「这一步从未跨提交点、下一会话应当能重跑」也一并留死。
		defer func() { e.releaseWriteSlot(ctx, task.ID, session.ID, stepRow.ID, writeKey) }()
		// Chunk4 活动触达条数预算：这一条是「本步真的会发出去」的最后一刻，故扣减排在此处——
		// 排在占坑之后，被双发闸/D7 确认挡掉的步不消费额度（它们最终没发出去）。
		// 预算耗尽 = 配置事实，不是异常：判红 + 拒绝下发 + 说明缺什么，让用户去调预算而不是重跑。
		if !e.consumeCampaignActBudget(ctx, task) {
			msg := fmt.Sprintf("写步未下发（%s）：%s", writeWhy, campaignBudgetExhaustedMsg(task))
			e.finishStep(ctx, stepRow.ID, "failed", nil, 0, msg)
			e.appendCommandLog(ctx, session.ID, task.ID, stepRow.ID, *seq, "event", step.Action,
				mergeAuditMeta(map[string]any{
					"reason": "campaign_budget_exhausted", "campaign_key": task.CampaignKey,
					"campaign_act_budget": task.CampaignActBudget, "campaign_act_used": task.CampaignActUsed,
				}, e.stepAuditMeta(0)), 0, verdict(false))
			return "failed", msg, nil
		}
	}
	// D7 覆盖派生写。把「type+回车 / click 发送按钮 / click_near 发送」认成写步之后，
	// 闸门却仍只长在 post_comment 原语内部——开了 require_confirm 的用户，这类隐形写照样被
	// 无条件发出去。此处按步挂起（post_comment 保留原语内的闸门：先 prep 填好正文再确认，
	// 让人看见将要发什么）。位置排在双发闸之后、任何命令帧之前：
	// 该跳过的步不该先问一遍再跳过，未放行则该步一帧都没下发。
	if writeStep && step.Action != "post_comment" && task.RequireConfirm {
		preview := step.Value
		if preview == "" {
			preview = strings.TrimSpace(step.Action + " " + step.Target)
		}
		out, why := e.awaitConfirmGate(ctx, task, session, stepRow.ID, index, preview, writeKey, seq, confirmWaitBudget(task))
		if out != confirmGranted {
			msg := fmt.Sprintf("写步等待人工确认超时（%s），评论未提交", why)
			if out == confirmStoppedByUser {
				msg = errConfirmAbortedByStop
			}
			e.finishStep(ctx, stepRow.ID, "failed", nil, 0, msg)
			return "failed", msg, nil
		}
	}
	var lastErr string
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			if !sleepInterruptible(ctx, stopCh, retryBackoffDelay(backoff, attempt)) {
				e.finishStep(ctx, stepRow.ID, "failed", nil, 0, "用户手动中断")
				return "failed", "用户手动中断", nil
			}
		}
		start := time.Now()
		// P8 append-only 命令日志：命令帧先落（含步骤参数全文），回包/错误随 result 事件再落。
		// 命令帧 ok=nil（/ A2）：写下这一行时 Host 连回执都还没有，甚至可能
		// 根本没进 socket——旧实现传字面量 true，于是审计包里每条「下发」都自带一个 ✓，
		// 而那个 ✓ 在这一行上什么也不指。它 attempted，不 succeeded。
		*seq++
		cmdPayload := map[string]any{"action": step.Action, "target": step.Target, "params": buildStepParams(step)}
		// Chunk4：写步的下发帧带上活动预算水位（配置了活动预算时）。读审计包的人据此能回答
		// 「当时这次触达是第几次、还剩几条」，而不必去猜时间戳落在哪次运行里。
		// 未配置预算则整段不出现（不写 0/0 的假水位）。
		if writeStep {
			if snap := campaignBudgetSnapshot(task); snap != nil {
				for k, v := range snap {
					cmdPayload[k] = v
				}
			}
		}
		e.appendCommandLog(ctx, session.ID, task.ID, stepRow.ID, *seq, "command", step.Action, cmdPayload, 0, nil)
		result, err := e.dispatchStep(ctx, task, session, step, stepRow, seq, outreachCtx)
		dur := time.Since(start).Milliseconds()
		// 回包帧自己占一个号：dispatchStep 内部还会落 d7_*/comment_send/write_confirm 帧，
		// 沿用「一问一答同序号」的旧写法会让步级 event 与它们同号（审计面上两条同号帧，
		// 按 seq 还原执行序的读者会看到一次动作凭空分成两叉）。
		*seq++
		if err == nil {
			// 写步的「成功」必须以台账落成前提。命令确实下发了，但库里没有这次
			// 提交的凭据 ⇒ 下一轮无从得知它发生过 ⇒ 让整轮绿就是拿不可逆动作换一次好看的状态。
			var ledgerErr error
			if writeStep {
				ledgerErr = e.recordGenericWriteLedger(ctx, task.ID, session.ID, step, stepRow.ID, writeKey, nil)
			}
			if ledgerErr != nil {
				msg := fmt.Sprintf("写步已下发但台账未落（%v）——本轮判红：命令可能已生效，重发即双发，请人工核对该步结果", ledgerErr)
				logger.Warnf("[BrowserExec] %s session=%d idx=%d action=%s", msg, session.ID, index, step.Action)
				e.finishStep(ctx, stepRow.ID, "failed", result, dur, msg)
				e.appendCommandLog(ctx, session.ID, task.ID, stepRow.ID, *seq, "event", step.Action,
					mergeAuditMeta(map[string]any{"error": msg}, e.stepAuditMeta(dur)), dur, verdict(false))
				return "failed", msg, json.RawMessage(result)
			}
			e.finishStep(ctx, stepRow.ID, "success", result, dur, "")
			e.appendCommandLog(ctx, session.ID, task.ID, stepRow.ID, *seq, "event", step.Action,
				mergeAuditMeta(map[string]any{"result": json.RawMessage(auditEventPayload(result))}, e.stepAuditMeta(dur)),
				dur, verdict(true))
			return "success", "", json.RawMessage(result)
		}
		lastErr = err.Error()
		if writeStep {
			// 步本身已判败，台账再写不进去只加一句因由：缺口由进程内兜底与降级表接管（A7）。
			if ledgerErr := e.recordGenericWriteLedger(ctx, task.ID, session.ID, step, stepRow.ID, writeKey, err); ledgerErr != nil {
				lastErr = fmt.Sprintf("%s；且写台账未落（%v）——重跑本任务前请人工核对该步是否已生效", lastErr, ledgerErr)
			}
		}
		e.appendCommandLog(ctx, session.ID, task.ID, stepRow.ID, *seq, "event", step.Action,
			mergeAuditMeta(map[string]any{"error": lastErr}, e.stepAuditMeta(dur)), dur, verdict(false))
		logger.Warnf("[BrowserExec] step 失败 session=%d idx=%d action=%s attempt=%d: %s", session.ID, index, step.Action, attempt, lastErr)
		// F7（G16）：重试按平台错误归因分线（MediaCrawler 处置矩阵语义）——
		// bad_body（内容被拒）重试无意义直接终止；refresh_token/disconnect 同样终止（交上层 session 级处置）；
		// 仅 retry（瞬态）继续退避重试。
		if attempt < retries && !stepErrRetryable(taskPlatformID(task), lastErr) {
			logger.Warnf("[BrowserExec] 错误分类为不可重试，终止步重试 session=%d action=%s", session.ID, step.Action)
			break
		}
	}
	e.finishStep(ctx, stepRow.ID, "failed", nil, 0, lastErr)
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
		}, time.Since(start).Milliseconds(), verdict(snapErr == ""))
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
// stepRow：本步的 DB 行（含 ID），写原语分支用它落 submit_state 台账。
// seq：session 局部命令号——post_comment 的 D7 闸门在分支内部，它要落的 d7_wait/d7_confirm 帧
// 与外层命令帧共用同一个号段（计数器不下放给闸门，否则两处各自编号必然撞号）。
// outreachCtx：写步去重键（executeStepWithRetry 的去重检查点冻结），verified 后落去重行用；
// 非写步恒为 nil，落库入口自行判空短路。
func (e *Executor) dispatchStep(ctx context.Context, task *model.BrowserTask, session *model.BrowserSession, step parsedStep, stepRow *model.BrowserStep, seq *int, outreachCtx *outreachDedupeCtx) ([]byte, error) {
	userID := task.UserID
	tabID := session.ChromeTabID
	p := buildStepParams(step)

	switch step.Action {
	case "open_tab":
		openURL := step.Target
		if openURL == "" {
			openURL = task.Url // 编排未填 target 时兜底任务起始 URL
		}
		tabID, res, err := e.hand.openTab(ctx, userID, openURL, false, step.TimeoutMs) // active 恒 false
		if err != nil {
			return nil, err
		}
		session.ChromeTabID = tabID
		_ = e.sessionRepo.UpdateChromeTabID(ctx, session.ID, tabID)
		// page_loaded 原样透传（可能是 nil：老 Host 不回这个字段时如实记 null，
		// 不能把「不知道」写成 false，也不能反过来把 false 洗成 true）。
		// load_wait_ms/title/create_ms 一并留档：真机 session611 真站 open_tab 往返 26159ms
		// 而 step.duration 已把整段吞掉，缺扩展侧「等加载花了多久」就无法把慢归因到
		// tabs.create+waitForLoad 还是链路；session619-621 的 30s 黑盒更是在
		// tabs.create 挂死时连 create 耗时都拿不到——create_ms 就是那案的归因字段。
		// title 用来确认打开的确实是目标页。字段缺席（老 Host 不回）时原样不写，不编造 0。
		payload := map[string]any{"chrome_tab_id": tabID, "page_loaded": res["loaded"]}
		if v, ok := res["load_wait_ms"]; ok && v != nil {
			payload["load_wait_ms"] = v
		}
		if v, ok := res["title"]; ok && v != nil {
			payload["title"] = v
		}
		if v, ok := res["create_ms"]; ok && v != nil {
			payload["create_ms"] = v
		}
		return recordResultPayload(payload)
	case "click":
		// 写步才请求点后身份复核。判据取 stepRow.IsWrite 而不是在这里重算
		// classifyStepEffect：那一列是本步落库时写死的事实，重试轮、双发闸、D7 都读它——
		// 在这里重算就等于允许「闸门认它是写、请求复核时认它不是写」这种裂脑存在。
		res, err := e.hand.click(ctx, userID, tabID, step.Target, stepRow.IsWrite)
		if err != nil {
			return nil, err
		}
		// channel 如实透传：cdp / dom_fallback / null（老扩展没这个字段）。
		// 兜底本身不是失败，但「一片绿里全是 dom_fallback」= trusted 通道死了，
		// 这一列就是用来发现它死了的（实证形态）。
		// identity_checked 同口径：true=这次写步点击真的过了点后复核；null=旧扩展没这个概念。
		// 不加这一列，「复核跑过」与「复核从没被请求」在库里长得一样（立项理由）。
		return recordResultPayload(map[string]any{
			"navigated": res["navigated"] == true, "channel": res["channel"],
			"identity_checked": res["identity_checked"],
		})
	case "type":
		res, err := e.hand.typeText(ctx, userID, tabID, step.Target, step.Value, p.ClearFirst, p.SubmitOnEnter)
		if err != nil {
			return nil, err
		}
		return recordResultPayload(map[string]any{"channel": res["channel"]})
	case "click_near":
		// 以 Anchor CSS 为基准点击容器内指定文本的 button（发送/提交按钮无稳定 class 场景）
		res, err := e.hand.clickNear(ctx, userID, tabID, step.Anchor, step.ButtonText, stepRow.IsWrite)
		if err != nil {
			return nil, err
		}
		return recordResultPayload(map[string]any{
			"clicked": res["clicked"] == true, "channel": res["channel"],
			"identity_checked": res["identity_checked"],
		})
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
		// 双发闸在 executeStepWithRetry 下发任何帧之前已过（起对全部写步统一生效）。
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
		// 这一行写不进去就不再往下走一格。prepared 不在拦阻集合内，send 之后
		// 库里若还是空的，下一轮（含同进程自动重试）就查不到任何尝试——那正是双发的形状。
		if err := e.recordSubmitState(ctx, task.ID, session.ID, stepRow.ID, model.StepSubmitPrepared, textHash, false); err != nil {
			return nil, fmt.Errorf("post_comment 未提交（prepared 台账未落，点击从未发生，本会话写能力已降级）: %w", err)
		}
		// D7 人工确认闸门：require_confirm=true 的任务在此挂起，等 POST /sessions/:id/confirm
		// 放行后才进不可逆提交点。挂起期间只有 prep（填文本，页面内可撤销、零平台副作用）；
		// 未放行即中止=从未提交，可安全重下发。
		// 等待用 task.confirm_wait_sec 独立预算，不再吃 task.TimeoutSec（那条管自动化本身）。
		if task.RequireConfirm {
			out, why := e.awaitConfirmGate(ctx, task, session, stepRow.ID, stepRow.StepIndex,
				step.Value, textHash, seq, confirmWaitBudget(task))
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
		// 每次真实提交都成对落两帧：command 帧=attempted 且不带结论，
		// event 帧=accepted。自愈重发的那一次同样落帧——「全场只提交一次」是运行期红线，
		// 不是审计期红线：审计流要如实有几场就几场，藏起来等于让读包的人猜。
		// sendRes=最后一次 comment_send 回包，扩展的点后复核结论从它身上取；
		// 判据是回包里的 identity_checked 而不是「我们请求过复核」——请求发出去 ≠ 复核跑成了，
		// 旧扩展或复核给不出结论时如实记 null（click / click_near 同口径）。
		var sendRes map[string]any
		sendOnce := func() error {
			*seq++
			e.appendCommandLog(ctx, session.ID, task.ID, stepRow.ID, *seq, "command", commentSendAction,
				map[string]any{"action": commentSendAction, "tab_id": tabID, "payload_hash": textHash}, 0, nil)
			start := time.Now()
			res, err := e.hand.commentSend(ctx, userID, tabID, prepReq, stepRow.IsWrite)
			*seq++
			e.appendCommandLog(ctx, session.ID, task.ID, stepRow.ID, *seq, "event", commentSendAction,
				map[string]any{"error": sendErrText(err)}, time.Since(start).Milliseconds(), verdict(err == nil))
			// 留最后一次回包：自愈重发过的那一次才是这一步的结论，前一次的复核结论已经不作数
			sendRes = res
			return err
		}
		sendErr := sendOnce()
		// ① 那一类在传输层还有第三种形态——这一帧根本没写进 socket（D7 等多分钟，
		// Host 掉在等待里是常态）。此处刻意**不**就地重发一次：判据成立即 Host 已经没了，
		// 微秒级的第二次调用只会拿到同一条错误；恢复归任务级重试（台账留 prepared ⇒ 不在
		// 拦阻集合 ⇒ 下一轮重跑会重新 prep、重新过一次闸门，那是更严而不是更松的路径）。
		if isCommandNeverOnWire(sendErr) {
			return nil, fmt.Errorf("post_comment 未提交（命令未上线，点击从未发生，可安全重下发）: %w", sendErr)
		}
		if isInjectTimeout(sendErr) {
			return nil, fmt.Errorf("post_comment 未提交（页面注入拥堵，点击未发生）: %w", sendErr)
		}
		if isSendGateReject(sendErr) {
			return nil, fmt.Errorf("post_comment 未提交（发送按钮不可点，点击未发生）: %w", sendErr)
		}
		if isNeverDispatched(sendErr) {
			return nil, fmt.Errorf("post_comment 未提交（CDP 未 attach，事件从未下发，可安全重下发）: %w", sendErr)
		}
		// 提交点已跨越：立即落 sent，不等 finalize 的结论。理由——「send 之后 execCtx 恰好到期」
		// 是最坏窗口（步被判超时、终态归因模糊），此时台账若还没写，重下发就没有任何拦阻。
		// 上面三个早返分支（未上线/注入超时/闸门拒点）台账都留在 prepared：那三支点击从未发生。
		// 这次写失败=「越点未落账」，recordSubmitState 会同时记下进程内兜底缺口，
		// 本步最终判红（见下面 sentLedgerErr）——撤不回了，但至少不再有人替我们假设它没发生。
		sentLedgerErr := e.recordSubmitState(ctx, task.ID, session.ID, stepRow.ID, model.StepSubmitSent, textHash, true)
		// A1 自愈一次：send_button_not_found=按钮从未命中=点击从未发生（同 R26-2 归因），
		// 重发不违「单次提交禁重试」红线；其余错误结局未知，交 finalize 回查绝不重发。
		if sendErr != nil && e.healCommentSendButton(ctx, task, session, tabID, prepReq, sendErr) {
			sendErr = sendOnce()
		}
		verified, evidence := e.finalizeComment(ctx, userID, tabID, step.Value, locs, e.stopChFor(session.ID))
		// 回查结论落台账：verified 是唯一可对外宣称「已发布」的态；未见即 unattributed
		// （已尝试、归因不到）——unattributed 仍在双发闸的拦截集合内，交人来判。
		finalState := model.StepSubmitUnattributed
		if verified {
			finalState = model.StepSubmitVerified
		}
		finalLedgerErr := e.recordSubmitState(ctx, task.ID, session.ID, stepRow.ID, finalState, textHash, true)
		// 审计流的第三态：跨过提交点后，无论回查见没见都要落一帧结论。
		// 落在台账写之后、ledgerErr 早返之前——「库里台账没写全」是 ledger 的问题，
		// 不改变「平台侧回查见了/没见」这个已经观测到的事实，两件事各自留痕。
		*seq++
		e.appendCommandLog(ctx, session.ID, task.ID, stepRow.ID, *seq, "judge", writeConfirmFrame, map[string]any{
			"payload_hash": textHash,
			"step_index":   stepRow.StepIndex,
			"state":        finalState,
		}, 0, verdict(verified))
		// finalize 证据落 extracted_data（追溯面板 + I4 续跑位点：重放可见「哪条评论已提交已验证」）
		e.mergeExtract(ctx, session, "post_comment", map[string]any{
			"text":       step.Value,
			"send_error": sendErrText(sendErr),
			"verified":   verified,
			"evidence":   evidence,
			"posted_at":  time.Now().Format(time.RFC3339),
		})
		// Chunk5 回执挂起（不落库，收口时一起落，见 captureSessionReceipts）：
		// 放在 mergeExtract 之后、两条结论分支之前——verified 与 unattributed 都是
		// 「已经发出去了」的事实，只有一边留回执的话，验收面就成了替平台做裁决。
		e.enqueueOutreachReceipt(ctx, task, session, step, stepRow, outreachCtx, evidence, verified)
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
			// Chunk3 跨任务触达去重落行：published 是事实，行落不落得住不改这个事实
			// （warn-only）。行在，下一个任务撞见同目标同文案就会跳过。
			recordOutreachDedupeSend(ctx, e, task, session, outreachCtx)
			return recordResultPayload(map[string]any{
				"posted": true, "verified": true, "evidence": evidence,
				// finalize 只回答「我的文字上去没」，identity_checked 回答的是
				// 「点下去的是不是发送按钮」——两问各自留痕。复核没过（element_moved）而回查见了评论时，
				// 这一列是 null、send_error 在 extracted_data 里，绿得有据可查，不是把结论洗掉。
				"identity_checked": sendRes["identity_checked"],
			})
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
		// 截断标志如实上抛：markdown 是「快照太长时改用 markdown 取全文」的出口
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
