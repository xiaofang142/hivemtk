// sop_approval_resume.go 审批与 SOP 流程之间的桥（T-P3-02 / N-4 挂起恢复）
//
// 一句话职责：让 SOP 的 wait 节点能"等一次裁决"，并让一次裁决能"叫醒一个流程" ——
// 两边都不认识对方：审批侧只有一个 `ApprovalWaitNotifier` 出口（见 approval_request.go），
// 本文件是它唯一的 SOP 侧实现；流程侧不知道裁决长什么样，只知道 ExecutionData 里多了几个键。
//
// 为什么"挂起"不需要新机器：SOP 的 wait 节点早就有一套持久化等待
// （`sop_timers` + outbox 轮询器 + FOR UPDATE SKIP LOCKED 点火 + MarkFired CAS 抢占），
// 本卡做的只是给这套机器加一种等待对象：**从"等时间到"变成"等一个结论"**。
// 于是"进程重启后可续跑"不是本卡新保证，而是继承来的 —— 挂起状态全在库里两行记录上
// （executions.status=running + timers.status=pending），进程内存里一点不剩。
//
// 两条唤醒路径，只有一条管正确性：
//
//	推送（NotifyDecided）  裁决落库后把对应定时器的 wait_until 提前到此刻，
//	                      让下一轮轮询（默认 5s）立刻点火。丢了不影响任何结论。
//	拉取（ResolveOnFire）  定时器点火那一刻才回读审批行，把结论写进 ExecutionData。
//
// 为什么结论必须在**点火时**回读，而不是由推送方一路带进任务里：那样"通知丢没丢"会
// 变成"流程知不知道结果"，而通知是不可靠信道（进程可能在裁决那一刻正好不在）。
// 回读把两侧解耦成：库是唯一事实源，通知只省时间。
//
// 到期口径只有一个来源：定时器的 `wait_until` 就等于审批行自己的 `expires_at`，
// 于是"流程还要不要等"与"审批还等不等得到人"永远同时结束 —— 不会出现
// "流程已经往下走了、审批还挂在待办中心"这种两边各自合理的事。
// 审批定时器刻意**不设** `max_wait_at`：那会让同一行有两条到期路径
// （正常点火 与 max_wait 超期跳过），后者派发的是不带 payload 的 SkipWait 任务，
// 于是同一次等待可能被推进两次、且第二次读不到结论。到期即裁决，不需要第二把刀。
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
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// ApprovalSubjectTypeSOPNode 审批挂在 SOP 节点上时用的 subject_type。
//
// 这个值是**桥自己写死的**，不从图配置读：subject_type 在本表里同时是幂等键的一段，
// 也是本文件反查"该叫醒谁"的判据。让图作者自由填写就会出现"审批成功了但没人被叫醒"
// —— 那一行既没有 error 也没有日志可看，只能等流程自己超时。
// 图上要表达"审的是什么业务"请写 approval_policy_key（进幂等键、进审计）。
const ApprovalSubjectTypeSOPNode = "sop_node"

// approvalSubjectSep subject_id 的分隔符。
//
// 编码的左半段是十进制执行 ID（不含任何符号），所以任何不与数字冲突的分隔符都不会歧义；
// 仍选 "--" 而不选 ":" 是因为节点 ID 由图作者写、今天就有以 "t:" 之类开头的可能，
// 而 ":" 在日志里紧邻 RFC3339 时间戳，读起来像另一个字段的开头。
const approvalSubjectSep = "--"

// EncodeApprovalSubject 把"哪个执行的哪个节点"编成审批的 subject_id。
//
// 用 (执行 ID, 节点 ID) 而不是"业务对象 ID"作主体，是为了让同一个业务对象在不同流程
// 实例里的两次等待各自是一条审批。反过来说：同一实例同一节点重复进（图里有环）会
// **不**共用一条 —— 前一次已落终态，幂等查不到 pending，于是新建一条。
// 这是刻意的：一个环上的第二次外发是另一个决定，复用第一次的批准等于把闸门开成永久放行。
func EncodeApprovalSubject(executionID uint, nodeID string) string {
	return strconv.FormatUint(uint64(executionID), 10) + approvalSubjectSep + nodeID
}

// DecodeApprovalSubject 从 subject 二元组还原 (执行 ID, 节点 ID)。
//
// 只认 ApprovalSubjectTypeSOPNode；其余（quote/reach_plan/…）返回 ok=false，
// 那是"这条审批没有流程挂在上面"的正常形状，不是错误。
// 节点 ID 允许含 "--"，故按**首个**分隔符切（strings.Cut 的语义正好）。
func DecodeApprovalSubject(subjectType, subjectID string) (uint, string, bool) {
	if subjectType != ApprovalSubjectTypeSOPNode {
		return 0, "", false
	}
	head, nodeID, ok := strings.Cut(subjectID, approvalSubjectSep)
	if !ok || nodeID == "" {
		return 0, "", false
	}
	id, err := strconv.ParseUint(head, 10, 64)
	if err != nil {
		return 0, "", false
	}
	return uint(id), nodeID, true
}

// 写进 ExecutionData 的审批产物键。前缀 "_" 与既有约定一致（_wait_event / _llm_decision
// 都是执行器产物而非业务数据），且这些键会随 executions.execution_data 落库，
// 下划线让"这是机器写的、不是客户填的"在库里可读。
const (
	// ApprovalOutcomeStatusKey 最终结论：approved / rejected / expired / pending / unreadable。
	// 值是字符串而不是布尔：分支条件写 `_approval_status eq approved` 比三分支布尔
	// 更能表达"拒了走 A、没人理走 B"，而 expired 与 rejected 必须分得开
	// （前者是流程该去催审批人，后者是业务该改方案）。
	ApprovalOutcomeStatusKey = "_approval_status"
	// ApprovalOutcomeAllowedKey 放行与否的便捷布尔位（= 状态是否 approved）。
	// 与状态并存不是冗余：条件节点只想拦一道时 `eq approved` 会把 expired 也拦下来，
	// 而那通常是对的；给出布尔位是为了让"只问批没批"这一种问法有一个不会写错的形式。
	ApprovalOutcomeAllowedKey = "_approval_allowed"
	// ApprovalOutcomeByKey 裁决来源（人工 ID / policy:auto / system:ttl）。
	ApprovalOutcomeByKey = "_approval_by"
	// ApprovalOutcomeNoteKey 裁决备注。
	ApprovalOutcomeNoteKey = "_approval_note"
	// ApprovalOutcomeIDKey 哪一条审批记录（人去对轨迹时用）。
	ApprovalOutcomeIDKey = "_approval_id"
	// ApprovalOutcomeTokenKey 恢复凭证。**留在 ExecutionData 里**是卡面要求的
	// "流程把它写进自己的 checkpoint"（model.ApprovalRequest.ResumeToken 同一口径）。
	ApprovalOutcomeTokenKey = "_approval_token"
	// ApprovalOutcomeDerivedKey 标记结论是推导出来的（"ttl" = 审批行仍 pending 但已过期，
	// 清扫还没来得及落库）。有这一格，运维就能区分"系统判的过期"与"库里已写的过期"。
	ApprovalOutcomeDerivedKey = "_approval_derived"
	// ApprovalOutcomeErrorKey 只在读不回结论时出现（原因见 approvalOutcomeFailure）。
	// 单开一格而不塞进 _approval_note：note 是**裁决人**写给人看的话，
	// 把 "read failed: dial tcp…" 混进去等于往业务备注里写系统日志。
	ApprovalOutcomeErrorKey = "_approval_error"
)

// approvalOutcomeUnreadable 读不回结论时写进流程的状态值。
//
// 刻意不复用 pending：pending 说的是"有人在等"，unreadable 说的是"我查不出来"。
// 两者都判不放行（fail-closed），但在诊断上是两个方向：前者要去催审批人，
// 后者要去查库和凭证。
const approvalOutcomeUnreadable = "unreadable"

// approvalTimerPayloadToken 定时器 payload 里存恢复凭证的键名。
const approvalTimerPayloadToken = "resume_token"

// approvalTimerPayloadApprovalID 定时器 payload 里存审批记录 ID 的键名（只为可读性，
// 唤醒匹配不用它 —— 见 NotifyDecided 的凭证比对）。
const approvalTimerPayloadApprovalID = "approval_id"

// ApprovalResumeBridge T-P3-02 的桥：既是审批侧的唤醒出口，也是 SOP 侧的结论回读器。
//
// 依赖只有两件：审批服务（读结论）与 sop_timers 仓储（找等待者）。刻意不持有调度器 ——
// 唤醒靠"把定时器提前到期"，点火与派发仍归 outbox 轮询器（见 MarkDueNow 的注释）。
// 少一根依赖线，装配顺序就没有先后要求，也就不会出现"桥要先于调度器构造但拿不到句柄"
// 这类只在启动期存在、跑起来永远不复现的问题。
type ApprovalResumeBridge struct {
	svc       *ApprovalRequestService
	timerRepo *repository.SOPTimerRepository
}

// NewApprovalResumeBridge 构造。svc 为 nil 时返回 nil（本桥不做"没有审批服务也能跑"的降级：
// 那等于让闸门变成一个安静的直通管道）。db 为 nil 时 timerRepo 为 nil，
// 于是只保留回读能力、失去提前唤醒（结论仍会由自然到期送达，只是慢）。
func NewApprovalResumeBridge(svc *ApprovalRequestService, db *gorm.DB) *ApprovalResumeBridge {
	if svc == nil {
		return nil
	}
	b := &ApprovalResumeBridge{svc: svc}
	if db != nil {
		b.timerRepo = repository.NewSOPTimerRepository(db)
	}
	return b
}

var (
	// approvalResumeMu 保护全局桥：写发生在装配期（router.Setup → app.InitApprovalRuntime），
	// 读发生在 worker 线程执行 wait 节点时。口径同 SOPExecutionDispatcher.compensationMu。
	approvalResumeMu sync.RWMutex
	approvalResume   *ApprovalResumeBridge
)

// SetApprovalResumeBridge 装配全局桥；传 nil 撤掉（回到"图里的审批等待节点判失败"那一档）。
//
// 用全局而不是给 WaitExecutor 注入字段：执行器在 InitSOPExecutionDispatcher 里注册，
// 而审批服务在 router.Setup 的装配步才构造，注册那一刻没有可注入的东西。
// 全仓对同一形状已有先例（GetSOPExecutionDispatcher / GetAssetResolver），
// 与其为它单开一条注入通道，不如沿用已被 -race 跑过的这一条。
func SetApprovalResumeBridge(b *ApprovalResumeBridge) {
	approvalResumeMu.Lock()
	approvalResume = b
	approvalResumeMu.Unlock()
}

// GetApprovalResumeBridge 读全局桥；未装配返回 nil。
func GetApprovalResumeBridge() *ApprovalResumeBridge {
	approvalResumeMu.RLock()
	defer approvalResumeMu.RUnlock()
	return approvalResume
}

// NotifyDecided 实现 ApprovalWaitNotifier：一条审批落定后把等它的定时器提前到期。
//
// 无返回值是接口定的（见 ApprovalWaitNotifier）：人的裁决已经落库，绝不能因为
// "叫不醒某个流程"而回滚那次裁决。因此这里所有失败都只出声。
//
// 匹配条件用**恢复凭证**而不是 (execution,node)：后者足够叫醒正确的流程，但
// 一个环上的第二次等待与第一次是同 node 同执行，凭证能让"只叫醒等这一条的那次"。
// 凭证为空（auto-approve 的记录永无 pending 定时器）时匹配不上任何行，正是想要的。
func (b *ApprovalResumeBridge) NotifyDecided(ctx context.Context, req *model.ApprovalRequest) {
	if b == nil || req == nil || b.timerRepo == nil {
		return
	}
	if req.ResumeToken == "" {
		return
	}
	executionID, nodeID, ok := DecodeApprovalSubject(req.SubjectType, req.SubjectID)
	if !ok {
		return
	}
	timers, err := b.timerRepo.FindPendingByExecutionAndNode(ctx, executionID, nodeID)
	if err != nil {
		logger.Ctx(ctx).Warn().Err(err).
			Uint("execution_id", executionID).
			Str("node_id", nodeID).
			Str("approval_id", req.ID).
			Msg("[approval-resume] 反查等待中的定时器失败 ⇒ 流程将等到自然到期才读结论")
		return
	}
	now := time.Now()
	for i := range timers {
		t := &timers[i]
		if t.WaitEvent != WaitEventApproval {
			continue
		}
		if payloadString(t.Payload, approvalTimerPayloadToken) != req.ResumeToken {
			continue
		}
		rows, err := b.timerRepo.MarkDueNow(ctx, t.ID, now)
		if err != nil {
			logger.Ctx(ctx).Warn().Err(err).Uint("timer_id", t.ID).
				Msg("[approval-resume] 提前到期失败 ⇒ 该流程留待自然到期")
			continue
		}
		if rows == 0 {
			// 不是失败：轮询器已经点了这一枚（或它已被跳过/死信）。
			logger.Ctx(ctx).Debug().Uint("timer_id", t.ID).
				Msg("[approval-resume] 定时器已不在 pending，无需提前")
			continue
		}
		logger.Ctx(ctx).Info().
			Uint("timer_id", t.ID).
			Uint("execution_id", executionID).
			Str("node_id", nodeID).
			Str("approval_id", req.ID).
			Str("status", req.Status).
			Msg("[approval-resume] 裁决已落，定时器提前到期（下一轮轮询点火并回读结论）")
	}
}

// ResolveOnFire 点火时回读结论，返回要写进 ExecutionData 的产物。
//
// 由调度器的 TimerFired 分支调用（sop_dispatcher.go），返回值赋给 result.Output，
// 于是既有的一次写入点就够用：handleNodeSuccess 把 Output 并进 ExecutionData 后
// **才**算下一跳（nextNode 读的就是这份数据）⇒ 分支路由看得到结论，不需要新机器。
//
// 非审批等待（task.WaitEvent != approval）返回 nil，调用方行为与改动前逐字节一致。
// 一旦是审批等待，返回值**恒非空**（失败也返回 unreadable）：没有任何一条路径能让流程
// 卡在这里。卡死比误拒贵得多 —— 误拒可以被人工重新发起，卡死的流程连"它卡住了"
// 都要等到巡检发现。
func (b *ApprovalResumeBridge) ResolveOnFire(ctx context.Context, task *dispatchTask) model.JSONMap {
	if task == nil || task.WaitEvent != WaitEventApproval {
		return nil
	}
	if b == nil {
		// 装配被撤（旗子关掉）而库里还有在等的定时器：这是切档的已知代价，
		// 出声并按"未获批准"推进，而不是无声卡住。
		logger.Ctx(ctx).Warn().
			Uint("execution_id", task.ExecutionID).
			Str("node_id", task.NodeID).
			Msg("[approval-resume] 桥未装配 ⇒ 审批等待按未获批准推进")
		return approvalOutcomeFailure("bridge_not_wired")
	}
	token := payloadString(task.WaitPayload, approvalTimerPayloadToken)
	if token == "" {
		logger.Ctx(ctx).Warn().
			Uint("execution_id", task.ExecutionID).
			Str("node_id", task.NodeID).
			Msg("[approval-resume] 定时器 payload 里没有恢复凭证 ⇒ 无法回读结论，按未获批准推进")
		return approvalOutcomeFailure("missing_token")
	}
	row, _, err := b.svc.ByResumeToken(ctx, token)
	// 顺序是**先看行、再看 err**，不能反过来。
	//
	// ByResumeToken 的第二个返回值与 err 回答的是另一个问题："现在能不能恢复"。
	// 它对 rejected / expired 这类终态行返回 (行, false, ErrApprovalResumeNotPending)。
	// 若按 err 优先判失败，一次正常的**人工拒绝**就会在流程里读成 unreadable：
	// 分支条件 `_approval_status eq rejected` 永不命中，流程被推到默认分支上，
	// 而库里、日志里一切都"看起来正常"。本卡要的是"这件事最后怎么样了"，
	// 只要行读到了，它就是结论；err 只在**行也没读到**时才是失败原因。
	if row == nil {
		if err != nil {
			logger.Ctx(ctx).Error().Err(err).
				Uint("execution_id", task.ExecutionID).
				Str("node_id", task.NodeID).
				Msg("[approval-resume] 回读审批结论失败 ⇒ 按未获批准推进（不重试：重试者没有新信息）")
			return approvalOutcomeFailure("read_failed")
		}
		// 凭证查不到行：本表没有删除路径，理论上只可能是有人手工删过行。
		logger.Ctx(ctx).Error().
			Uint("execution_id", task.ExecutionID).
			Str("node_id", task.NodeID).
			Msg("[approval-resume] 恢复凭证指向的审批记录不存在 ⇒ 按未获批准推进")
		return approvalOutcomeFailure("row_missing")
	}
	if err != nil {
		// 行拿到了，err 就只是"不可恢复"那条业务判据（终态）。它不进产物：
		// 结论与"为什么不能恢复"混在一格，读报告的人会以为出了故障。
		logger.Ctx(ctx).Debug().Err(err).
			Str("approval_id", row.ID).
			Msg("[approval-resume] 审批行已不可恢复，结论照常送达流程")
	}

	derived := ""
	// 到期与落库之间必然有一段空隙：定时器与审批行共用同一个时刻，谁先被看到取决于
	// 轮询与清扫谁先跑。这一段里"pending"已经不是事实 —— 事实是"没能等到裁决"。
	// 在这里推导，是为了让 AC③ 不依赖清扫节拍（清扫没装配时流程也不会读到一个假 pending）。
	if row.Status == model.ApprovalStatusPending && row.ExpiresAt != nil && !time.Now().Before(*row.ExpiresAt) {
		derived = "ttl"
	}
	outcome := approvalOutcomeFromRow(row, derived)
	logger.Ctx(ctx).Info().
		Uint("execution_id", task.ExecutionID).
		Str("node_id", task.NodeID).
		Str("approval_id", row.ID).
		Str("approval_status", fmt.Sprint(outcome[ApprovalOutcomeStatusKey])).
		Msg("[approval-resume] 已回读审批结论并写进执行数据")
	return outcome
}

// approvalOutcomeFromRow 把一条审批记录摊成 ExecutionData 的产物。
//
// 入参是行本身而不是六个同类型字符串：六个 string 的形参顺序写错不会有任何编译错误，
// 而这一格的值最终决定流程走哪条分支。
func approvalOutcomeFromRow(row *model.ApprovalRequest, derived string) model.JSONMap {
	out := model.JSONMap{
		ApprovalOutcomeStatusKey:  row.Status,
		ApprovalOutcomeAllowedKey: row.Status == model.ApprovalStatusApproved,
		ApprovalOutcomeIDKey:      row.ID,
		ApprovalOutcomeTokenKey:   row.ResumeToken,
	}
	if row.DecidedBy != "" {
		out[ApprovalOutcomeByKey] = row.DecidedBy
	}
	if row.DecisionNote != "" {
		out[ApprovalOutcomeNoteKey] = row.DecisionNote
	}
	if derived != "" {
		// 推导出的 expired 还没落到行上，行里的 decided_by 仍是空 ⇒ 覆盖式补上，
		// 否则流程侧读到 "status=expired" 而来源一格空白，看不出这是超时不是人拒。
		out[ApprovalOutcomeStatusKey] = model.ApprovalStatusExpired
		out[ApprovalOutcomeAllowedKey] = false
		out[ApprovalOutcomeByKey] = model.ApprovalDecidedByTTL
		out[ApprovalOutcomeDerivedKey] = derived
	}
	return out
}

// approvalOutcomeFailure 读不回结论时的产物：状态 unreadable、**不放行**。
//
// fail-closed 的取舍：读库失败时放行 = 一次 DB 抖动把审批闸门打开；
// 判未获批准 = 这一次动作没做成，人工可重发。前者的代价没人付得起。
func approvalOutcomeFailure(reason string) model.JSONMap {
	return model.JSONMap{
		ApprovalOutcomeStatusKey:  approvalOutcomeUnreadable,
		ApprovalOutcomeAllowedKey: false,
		ApprovalOutcomeErrorKey:   reason,
	}
}

// approvalNodeWait 一次审批等待的挂起参数（SubmitForNode 的结果）。
type approvalNodeWait struct {
	// NeedWait false = 不需要挂起（策略当场放行，或幂等读到的那一行早已落终态）。
	NeedWait  bool
	WaitUntil time.Time
	Outcome   model.JSONMap
	Request   *model.ApprovalRequest
}

// SubmitForNode 由 wait 节点的审批档调用：为"这个执行的这个节点"入队一次审批，
// 并回答"要不要挂起、挂到什么时候"。
//
// 顺序上先 Submit 再由调用方建定时器，反过来不行：定时器必须带凭证，而凭证在行里。
// 若 Submit 成功、建定时器失败，结果是"有一条待办、没有等待者" —— 审批人批完之后
// 没人被叫醒，但流程也没挂（节点判失败、执行按失败收口）。这个失败方向是可接受的：
// 它不会放行危险动作，也不会把执行永久吊在 running。
func (b *ApprovalResumeBridge) SubmitForNode(ctx context.Context, ec *ExecutionContext) (*approvalNodeWait, error) {
	if b == nil || b.svc == nil {
		return nil, fmt.Errorf("approval_resume: 桥未装配")
	}
	if ec == nil || ec.Execution == nil || ec.Node == nil {
		return nil, fmt.Errorf("approval_resume: 执行上下文不完整")
	}
	policyKey, _ := ec.Node.Config["approval_policy_key"].(string)
	policyKey = strings.TrimSpace(policyKey)
	if policyKey == "" {
		return nil, fmt.Errorf("approval_resume: 审批等待节点 %s 缺 approval_policy_key（不能默认放行，也不能默认某个策略名）", ec.Node.ID)
	}
	ttlSeconds, _ := ec.Node.Config["approval_ttl_seconds"].(float64)

	row, _, err := b.svc.Submit(ctx, ApprovalSubmitInput{
		SubjectType: ApprovalSubjectTypeSOPNode,
		SubjectID:   EncodeApprovalSubject(ec.Execution.ID, ec.Node.ID),
		PolicyKey:   policyKey,
		TTL:         time.Duration(ttlSeconds * float64(time.Second)),
	})
	if err != nil {
		return nil, err
	}

	outcome := approvalOutcomeFromRow(row, "")
	if row.Status != model.ApprovalStatusPending {
		// 已经有结论：auto-approve 的同步退化态（C2 的 A 形状）或幂等读到的既存终态。
		// 挂起在这里就地消失 —— 调用方原地拿结果，不需要轮询，也不需要定时器。
		return &approvalNodeWait{NeedWait: false, Outcome: outcome, Request: row}, nil
	}
	if row.ExpiresAt == nil {
		// 写入侧的不变量（pending 必带到期时刻）被破坏 = 库里有人手工改过这一行。
		return nil, fmt.Errorf("approval_resume: 审批记录 %s 是 pending 但没有 expires_at", row.ID)
	}
	return &approvalNodeWait{NeedWait: true, WaitUntil: *row.ExpiresAt, Outcome: outcome, Request: row}, nil
}

// ExecuteApprovalWait wait 节点审批档的完整执行，由 WaitExecutor 委托进来（唯一调用点）。
//
// 委托而不是把代码写进 WaitExecutor：那样 WaitExecutor 要同时依赖审批服务与调度器，
// 而它在执行器注册期构造（那时审批服务还不存在）。现在 WaitExecutor 只多四行，
// 审批的全部知识留在本文件。
//
// 挂起动作 = 插一行 sop_timers（status=pending, wait_until=审批行的 expires_at）+ 返回
// NodeStatusWaiting。**全程不 sleep、不占协程、不等 channel**：worker 拿到 Waiting 就去做
// 下一件事，执行行留在库里（AC① 的形状）。
//
// 建新定时器之前先删掉本节点遗留的 pending 定时器：wait 节点重跑（重试、恢复重投）会
// 各留一行，两行都会点火、都会派发一个推进任务，而调度器不校验 task.NodeID 与
// exec.CurrentNode 是否还一致 ⇒ 同一次等待把流程推进两次。
// 通用 timer 档位上这个隐患今天也在（不在本卡范围，见移交项）；审批档位先在自己这条路上堵住。
func (b *ApprovalResumeBridge) ExecuteApprovalWait(ctx context.Context, ec *ExecutionContext) (*NodeExecResult, error) {
	wait, err := b.SubmitForNode(ctx, ec)
	if err != nil {
		// 入队失败**不放行**：闸门读不出结论时的失败方向必须是"这次动作没拿到批准"。
		// 判失败且不重试（Retryable:false）—— 重试的通常是同一个入参同一个错，
		// 只会把一次配置错误放大成一阵重试风暴。
		logger.Ctx(ctx).Error().Err(err).
			Str("node_id", ec.Node.ID).
			Msg("[approval-resume] 审批入队失败 ⇒ 节点判失败（未放行）")
		return &NodeExecResult{
			Status:       NodeStatusFailed,
			ErrorMessage: fmt.Sprintf("approval wait: 审批入队失败: %v", err),
			Retryable:    false,
		}, nil
	}
	if !wait.NeedWait {
		// C2 的同步退化态：策略当场放行，节点直接完成，不产生任何等待状态。
		return &NodeExecResult{Status: NodeStatusCompleted, Output: wait.Outcome}, nil
	}
	if b.timerRepo == nil {
		// 没有库就没有等待的载体。此时**不能**判完成（那等于放行），也不能假装挂起
		// （那等于流程凭空停在原地、且库里查不到任何在等的东西）。
		return &NodeExecResult{
			Status:       NodeStatusFailed,
			ErrorMessage: "approval wait: 未接入 DB，无法持久化等待（挂起必须有载体，不能就地放行）",
			Retryable:    false,
		}, nil
	}

	if _, derr := b.timerRepo.DeletePendingByExecutionAndNode(ctx, ec.Execution.ID, ec.Node.ID); derr != nil {
		// 只出声：清不掉的旧行会再点一次火，而点火方（ResolveOnFire）读的是同一份结论，
		// 多出来的那次推进才是真问题 —— 那一面由下面的新建行本身不依赖清理成功来兜。
		logger.Ctx(ctx).Warn().Err(derr).
			Str("node_id", ec.Node.ID).
			Msg("[approval-resume] 清理遗留 pending 定时器失败")
	}

	now := time.Now()
	timer := &model.SOPTimer{
		ExecutionID: ec.Execution.ID,
		NodeID:      ec.Node.ID,
		WaitEvent:   WaitEventApproval,
		WaitUntil:   wait.WaitUntil,
		Status:      sopTimerStatusPending,
		ExpiresAt:   &wait.WaitUntil,
		// MaxWaitAt 刻意留 NULL：见文件头"到期即裁决，不需要第二把刀"。
		Payload: model.JSONMap{
			"trace_id":                     ec.TraceID,
			"customer_id":                  ec.CustomerID,
			"session_id":                   ec.SessionID,
			"attempt":                      ec.Attempt,
			"expires_at":                   wait.WaitUntil.Format(time.RFC3339),
			approvalTimerPayloadToken:      wait.Request.ResumeToken,
			approvalTimerPayloadApprovalID: wait.Request.ID,
		},
	}
	if err := b.timerRepo.Create(ctx, timer); err != nil {
		logger.Ctx(ctx).Error().Err(err).
			Str("node_id", ec.Node.ID).
			Msg("[approval-resume] 写入审批等待定时器失败")
		return &NodeExecResult{
			Status:       NodeStatusFailed,
			ErrorMessage: fmt.Sprintf("approval wait: 建等待定时器失败: %v", err),
			Retryable:    true, // 瞬时写失败，与通用 timer 档同一处置
		}, nil
	}
	logger.Ctx(ctx).Info().
		Uint("timer_id", timer.ID).
		Str("node_id", ec.Node.ID).
		Str("approval_id", wait.Request.ID).
		Time("wait_until", wait.WaitUntil).
		Dur("waited_so_far", now.Sub(wait.Request.CreatedAt)).
		Msg("[approval-resume] 流程已挂起等裁决（不占线程，重启后由库里这两行续跑）")

	return &NodeExecResult{
		Status:    NodeStatusWaiting,
		Output:    wait.Outcome,
		WaitUntil: &wait.WaitUntil,
		WaitEvent: WaitEventApproval,
	}, nil
}

func payloadString(payload model.JSONMap, key string) string {
	if payload == nil {
		return ""
	}
	v, _ := payload[key].(string)
	return v
}
