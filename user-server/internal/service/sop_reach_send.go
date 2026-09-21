// sop_reach_send.go T-P5-03：Active 出域的唯一编排出口（`reach_send` 节点）。
//
// 一句话职责：让"主动触达客户"这件事在 SOP 图里第一次**真的存在**，并且它存在的唯一形态
// 就是把发送整包交给 `ProactiveReachService.ReachByCustomer` —— 三判据（approval_request /
// checkCooldown / checkDoNotContact）里的后两条本来就在那里面，本文件不复制、也不改其选路。
//
// 为什么要新造一个节点类型而不是"给既有节点加个开关"：接线前实测 **Active 出域 = 零**。
// `MessageNodeBase.Execute`（greeting/inquire/…/message/action/send_offer 全靠它）只写
// `session_messages` 与商家 WS，节点上的 `Tools` 字段没有任何执行方读它（`sop.go` 只复制）。
// 于是"闸门串联"在当时的物理状态下是一句无法证伪的话 —— 没有出域，也就没有"出域被拦住"。
// 本文件把那件事变成可测的：有一条真的会出域的路，且它出域必须过三关。
//
// 三关的顺序（每一条都有用例钉住，顺序本身就是契约）：
//
//	① 幂等键 `reach_sent:<exec>:<node>`   —— 重跑/恢复重投不二次发送
//	② 内容非空                            —— 没内容就不去占用审批位（别让审批人批一条空气）
//	③ 图上审批腿（approval_request）       —— 没有结论就挂起；结论非 approved 就跳过
//	④ ReachByCustomer 内部：DNC → 闸门 → 冷却 → 选路后的渠道发送
//
// 为什么审批腿放在节点里、而 DNC/频控留在服务里：两者问的不是同一件事。
// 节点问的是"这条流程走到外发这一步，有人点头吗"（结论是**一次决定**，载体是 approval_requests
// 那一行 + 一枚 sop_timer，能挂起、能被待办中心看见）；服务问的是"这个收件人此刻能不能被发"
// （DNC 是永久事实、冷却是窗口、T-P3-07 那道门是运行期白名单）。把前者塞进后者会让挂起语义
// 无处安放（服务是同步函数，没有"停在库里等人"这一档），把后者搬进前者则每个新调用方都要重抄一遍。
//
// 挂起/恢复不新造机器：完全复用 T-P3-02 那座桥（`ExecuteApprovalWait` 建审批行 + 定时器，
// 点火时调度器把结论回读递回来）。本文件因此不认识调度器、也不认识审批服务。
//
// 刻意**不填** `req.Phone` / `req.Email`：`ReachByCustomer` 的两条显式收件人分支只过 DNC 与闸门、
// 不过 `checkCooldown`。节点交身份（customer_id + one_id）而不是交手机号，
// 就是为了让自己必然走那条三判据齐全的路 —— 这是"不改其选路逻辑"这条卡面约束下唯一能做到的口径。
//
// 五层归属：判定语义在 approval 与 proactive_reach 两个域；挂起载体在 sop_approval_resume.go；
// 本文件只负责"图里的那一步怎么把两者串起来"。
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// ActiveReachSender 客户侧出域的唯一出口（`*ProactiveReachService.ReachByCustomer` 天然满足）。
//
// 窄到一个方法是有意的：接口每多一个方法，就多一处"节点能绕过选路与三判据直接指定渠道"的口子。
// 与之配套的静态门见 sop_reach_send_test.go 的
// TestNodeExecutorFiles_HaveNoCustomerOutboundBeyondReachByCustomer。
type ActiveReachSender interface {
	ReachByCustomer(ctx context.Context, req *ProactiveReachRequest) (*ProactiveReachResponse, error)
}

// 节点产物键。前缀 "_" 沿用既有约定：机器写的、不是客户填的（见 ApprovalOutcomeStatusKey 那条）。
const (
	// outputKeyReachSkip 跳过理由。值必须能区分 dnc / cooldown / already_sent / approval:<状态>：
	// 这四种"没发出去"的后续处置动作完全不同（改名单 / 等窗口 / 本就该跳过 / 去补授权）。
	outputKeyReachSkip = "_reach_skipped"
	// outputKeyReachChannel / outputKeyReachMessageID 发出去了什么，留在执行数据里可回查。
	outputKeyReachChannel   = "_reach_channel"
	outputKeyReachMessageID = "_reach_message_id"
	// reachSentSideEffectFmt 幂等键形状。与消息类节点的 `message_sent:` 同一段命名空间，
	// 不复用同一个前缀是因为两者的撤销语义不同（见 ReachSendExecutor.CompensationNote）。
	reachSentSideEffectFmt = "reach_sent:%d:%s"
	// reachNodeFieldContent / PreferredChannels 节点配置键名。
	//
	// 刻意没有 `subject`：本节点承载的是短信/站内那一类触达，主题字段今天只被 email 渠道读，
	// 而没有任何用例走过 email —— 与其留一条无人验的传参，不如等真要发 email 时连用例一起加
	// （见 §移交项）。
	reachNodeFieldContent          = "content"
	reachNodeFieldPreferredChannel = "preferred_channels"
	// reachNodeOneIDKey T-P5-02 的 Active 生命周期把它写进 execution_data。
	// 它在**本节点**只有一档用途：`customer_id` 缺失时给服务当查身份的回退键。
	// 不是发送前闸门的判定键 —— 那个取 customers 行上的 UnifiedID（权威只有一处，
	// 图里的一步改不动它；见 TestReachSend_GateKeyComesFromCustomerRowNotExecutionData）。
	reachNodeOneIDKey = "one_id"
)

var (
	// sopReachMu 保护全局 sender：写发生在装配期（router.Setup 的触达装配步），
	// 读发生在 worker 线程。口径与 approvalResumeMu 一致（同一条 -race 已跑过的路）。
	sopReachMu     sync.RWMutex
	sopReachSender ActiveReachSender
)

// SetSOPReachSender 装配 SOP 执行层的外发出口；传 nil 撤装。
//
// 用全局而不是给 ReachSendExecutor 注入字段，理由与桥一致（sop_approval_resume.go 文件头）：
// 执行器在 InitSOPExecutionDispatcher（cmd/api 启动早期）注册，而带闸门的外发服务
// 在 router.Setup 才构造。注册那一刻没有可注入的东西。
func SetSOPReachSender(s ActiveReachSender) {
	sopReachMu.Lock()
	sopReachSender = s
	sopReachMu.Unlock()
}

// GetSOPReachSender 读全局 sender；未装配返回 nil（调用方必须按"发不出去"处置）。
func GetSOPReachSender() ActiveReachSender {
	sopReachMu.RLock()
	defer sopReachMu.RUnlock()
	return sopReachSender
}

// ReachSendExecutor 外发节点执行器：图上唯一真的会把内容送到客户手上的节点。
type ReachSendExecutor struct{}

// NewReachSendExecutor 构造。它不持有任何依赖：sender 与审批桥都在 Execute 时读全局，
// 于是"注册期早于装配期"这个既有的启动顺序事实不会变成一条隐性的构造约束。
func NewReachSendExecutor() *ReachSendExecutor { return &ReachSendExecutor{} }

func (e *ReachSendExecutor) NodeType() string { return SOPNodeTypeReachSend }

func (e *ReachSendExecutor) IsAsync() bool { return false }

// CompensationNote 声明已出域的消息不撤回（与消息类节点同一口径）。
//
// 幂等键 `reach_sent:` 有意保留：撤销它等于允许重跑再发一条，
// 而"同一条流程对同一个人发两遍同样的话"正是外联闸门要防的事之一。
func (e *ReachSendExecutor) CompensationNote() string {
	return "外发已出域不撤回（同消息类节点）；幂等键 reach_sent: 有意保留，防重跑二次发送"
}

// Execute 按"幂等 → 内容 → 审批 → 三判据发送"四步走，任何一步没过后一步都不执行。
//
// 返回值一律用 NodeExecResult 表达处置，不把 error 上抛：调度器拿到 error 会走
// "重试到次数耗尽"那条路，而这里每一种拒绝（退订 / 冷却 / 没批）重试都不改变结论。
func (e *ReachSendExecutor) Execute(ctx context.Context, ec *ExecutionContext) (*NodeExecResult, error) {
	if ec == nil || ec.Execution == nil || ec.Node == nil {
		return nil, errors.New("reach_send: 执行上下文不完整")
	}
	nodeID := ec.Node.ID
	sentKey := fmt.Sprintf(reachSentSideEffectFmt, ec.Execution.ID, nodeID)

	if hasSideEffect(ec.Execution, sentKey) {
		logger.Ctx(ctx).Info().
			Str("node_id", nodeID).
			Str("side_effect", sentKey).
			Msg("reach already sent, skipping (idempotent)")
		return &NodeExecResult{
			Status: NodeStatusSkipped,
			Output: model.JSONMap{outputKeyReachSkip: "already_sent"},
		}, nil
	}

	content := strings.TrimSpace(renderPromptTemplate(reachNodeStringConfig(ec.Node.Config, reachNodeFieldContent), ec.ExecutionData))
	if content == "" {
		return reachSendFailure(nodeID, "节点未配置 content（或模板渲染后为空），未占用审批位"), nil
	}

	// —— 审批腿：手里有结论就按结论办，没有就去问一次并挂起 ——
	//
	// 结论从 `ec.ApprovalOutcome` 来（调度器在审批定时器点火那一刻回读后递进来，
	// 见 sop_dispatcher.go 的 TimerFired 分支）。它非空即代表"这件事已经有人答过"，
	// 于是这里**绝不再入队**：给一条已落终态的等待再建一行 pending，
	// 就是"批一次→发不出→再批一次"的永动机。
	outcome := ec.ApprovalOutcome
	if outcome == nil {
		b := GetApprovalResumeBridge()
		if b == nil {
			return reachSendFailure(nodeID,
				"reach_send: 审批运行时未装配（旗子 off / DB 句柄缺失），未放行"), nil
		}
		res, err := b.ExecuteApprovalWait(ctx, ec)
		if err != nil || res == nil {
			return res, err
		}
		if res.Status != NodeStatusCompleted {
			// Waiting（挂起等裁决）与 Failed（入队失败/无库）都意味着"这次没拿到批准"⇒ 零外发。
			return res, nil
		}
		// C2 的同步退化态（策略当场放行）：结论在这一次的 Output 里，带下去做回显。
		outcome = res.Output
	}
	if status := outcomeString(outcome); status != model.ApprovalStatusApproved {
		logger.Ctx(ctx).Info().
			Str("node_id", nodeID).
			Str("approval_status", status).
			Msg("reach send not approved, skipping")
		return &NodeExecResult{
			Status: NodeStatusSkipped,
			Output: model.JSONMap{outputKeyReachSkip: "approval:" + status},
		}, nil
	}

	// —— 发送腿：三判据在服务内部，本节点只负责把身份交出去 ——
	sender := GetSOPReachSender()
	if sender == nil {
		// 与审批运行时同一口径：不能把关的实现必须把门关上，且不能长得像"已经发过了"。
		return reachSendFailure(nodeID, "reach_send: 外发服务未装配（装配点未接住装了闸门的实例），未发送"), nil
	}

	req := &ProactiveReachRequest{
		CustomerID: ec.CustomerID,
		// OneID 只在 CustomerID 为空时才是服务侧的查身份回退档（`loadCustomer` 先查主键）；
		// 判定键**不取这里的值** —— 它取客户行上的 UnifiedID，见文件头那条归因口径。
		OneID:             reachNodeStringConfig(ec.ExecutionData, reachNodeOneIDKey),
		Content:           content,
		PreferredChannels: reachNodeStringSliceConfig(ec.Node.Config, reachNodeFieldPreferredChannel),
	}
	resp, err := sender.ReachByCustomer(ctx, req)
	if err == nil {
		out := model.JSONMap{}
		for k, v := range outcome {
			out[k] = v // 把结论回显进执行数据：下游 condition 能按它分支，事后也能回查是谁批的
		}
		if resp != nil {
			out[outputKeyReachChannel] = resp.Channel
			out[outputKeyReachMessageID] = resp.MessageID
		}
		latency := time.Since(ec.StartedAt).Milliseconds()
		logger.Ctx(ctx).Info().
			Str("node_id", nodeID).
			Uint("execution_id", ec.Execution.ID).
			Str("channel", reachOutString(out, outputKeyReachChannel)).
			Int64("latency_ms", latency).
			Msg("reach send executed")
		return &NodeExecResult{
			Status:      NodeStatusCompleted,
			Output:      out,
			SideEffects: []string{sentKey},
		}, nil
	}

	// 哨兵错误决定处置，不看错误文案：文案会改，处置不会。
	switch {
	case errors.Is(err, ErrDoNotContact):
		return &NodeExecResult{
			Status: NodeStatusSkipped,
			Output: model.JSONMap{outputKeyReachSkip: "dnc"},
		}, nil
	case errors.Is(err, ErrReachCooldown):
		// 冷却窗是 60 分钟量级，而重试退避是秒级：按失败重试只会把三次机会在两秒内烧完，
		// 最后仍是一条没发出去的外发。跳过 = 本轮不碰这个人，下一轮触发再试。
		return &NodeExecResult{
			Status: NodeStatusSkipped,
			Output: model.JSONMap{outputKeyReachSkip: "cooldown"},
		}, nil
	case errors.Is(err, ErrReachApprovalDenied):
		// 已经过了图上的审批腿却被发送前闸门拦下：判失败、不重试、**绝不回头再挂一次审批**。
		return reachSendFailure(nodeID, err.Error()), nil
	default:
		// 渠道故障（网络/凭据/对端限流）留重试：与消息类节点的解析失败同一处置。
		return &NodeExecResult{
			Status:       NodeStatusFailed,
			ErrorMessage: fmt.Sprintf("reach send failed: %v", err),
			Retryable:    true,
		}, nil
	}
}

func reachSendFailure(nodeID, msg string) *NodeExecResult {
	return &NodeExecResult{
		Status:       NodeStatusFailed,
		ErrorMessage: msg,
		Retryable:    false,
	}
}

// outcomeString 从审批结论产物里取状态；读不出即空串（空串不等于任何放行值）。
func outcomeString(outcome model.JSONMap) string {
	return reachOutString(outcome, ApprovalOutcomeStatusKey)
}

func reachOutString(m model.JSONMap, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

// reachNodeStringConfig 取节点配置里的字符串项。
//
// 入参是 map[string]any 而不是 model.JSONMap：dto.SOPNode.Config 与 model.JSONMap
// 虽然底层同形，却是两个类型（后者是命名类型），混用会变成每处都要写一次转换。
func reachNodeStringConfig(cfg map[string]any, key string) string {
	if cfg == nil {
		return ""
	}
	v, _ := cfg[key].(string)
	return v
}

// reachNodeStringSliceConfig 图配置来自 JSON，数组一定是 []any，不是 []string。
// 逐项过滤而不是整体断言：一个非字符串项不该让整条外发链断掉。
func reachNodeStringSliceConfig(cfg map[string]any, key string) []string {
	if cfg == nil {
		return nil
	}
	raw, ok := cfg[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
