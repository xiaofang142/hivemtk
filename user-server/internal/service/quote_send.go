// quote_send.go T-P6-03：报价发送必经审批检查点。
//
// 一句话职责：把 T-P6-02 交出来的那一版草稿，在**有人（或策略）点过头之后**送到客户手上，
// 并在库里留下"是谁的哪一版、谁点的发送"这一条事实。
//
// 三条 AC 在本层的落点：
//
//	AC① 未审批的发送请求 100% 停在 pending —— 实现形态是"派发只有一处入口"：
//	     整条发送腿只在 `status == approved` 那一个分支里可达，pending / rejected /
//	     expired / 库里出现未知状态四条都只能返回处置或错误，够不到 `reach`。
//	     T-P6-04 之后那一处入口还多了一格前置：门序里**最后**那道门批完才走得到它。
//	AC② 审批阻塞不占用请求线程 —— 拿不到结论时**立刻返回**那条 pending 的主键，
//	     不睡、不轮询、不在进程内挂协程。续跑靠调用方带着这个号回来（或先问 OpenApproval）。
//	AC③ 发送成功回写 sales_events 的 opportunity_id / quote_id —— 这两列是 T-P2-04 预留的，
//	     本层是它们在生产路径上的**第一个写入方**（口径与两处文档同步改，见移交项）。
//
// 为什么不复用 SOP 那条 `reach_send` 节点（sop_reach_send.go）：两者过的门不同。
// 节点问的是"这条流程走到外发这一步，有人点头吗"，结论挂在 `sop_timers` 上、由调度器回读；
// 本层问的是"这一版报价可以发给客户了吗"，结论挂在报价版本行的主键上、由操作者带着审批号回来。
// 把两者合成一个节点类型会让"报价"这个 subject 依赖一条 SOP 执行记录存在，
// 而销售从工作台点发送时根本没有执行 —— 那会比现在更容易写出旁路。
// 相同的只有"发送必须经 `ProactiveReachService.ReachByCustomer`"这一条出口约束。
//
// 四处顺序判据（每一条都是某一次事故的形状）：
//
//	① 闸门在开审批之前：闸门关着却开出一条待办，审批人批的是"一件此刻根本不允许发生的事"。
//	② 完整性判据（收件人、行项目、话术）在认领之前：认领之后再发现没有正文，等于白抢一次跃迁。
//	     T-P6-04 把"行项目"那一格再往前挪到**开审批之前**（门序要从那几行的折扣算出来），
//	     收件人与话术两格仍留在认领之前。
//	③ 认领（draft→sent 的 CAS）在外发之前，而不是之后：崩溃窗口里的失败方向必须是
//	     "少发一条（可重试、库里读得出来）"，不能是"同一个人收到两遍同样的报价"。
//	     外发失败时把状态退回草稿；退不回去时错误里必须**同时**装着两件事
//	     （外发失败 + 状态没退回），只报一半会让人按错的那一半去重试。
//	④ 收件人在话术之前：话术要按**收件人**分桶（AC① 那次"重新解析"解析的是发给谁的那一份），
//	     先解析话术再认人，等于按操作者的分桶给收件人发东西。
//
// 收件人不从入参来（`QuoteSendInput` 里今天没有那一格，白名单用例钉着）：路径只有一条 ——
// 版本行 → opportunity_id → opportunities.one_id。理由与"正文/合计不从入参来"同一族：
// 批准的对象是"这一版可以出域"，它管不到"出给谁"；入参里能递收件人，
// 就是把一条给甲批的批准拿去发给乙这条路留在了契约上。
//
// 两道门（T-P6-04）：`quote.send` 之后可能还有 `quote.discount_high`（折扣达到
// `ltc.config.thresholds.discount_percent` 那一档，含等于；取的是**所有行里最大的那个折扣**）。
//
// 顺序是**串行**而不是并行：一次调用只开"当前该过的那一道"，前一道批完再点一次才开出下一道。
// 于是"高折扣版本拿着低档批准能做什么"只有一种答案 —— 把高档那条待办开出来，别的都做不了。
// 并行开两道则要让派发去问"这两道是否都已批准"，而那需要审批侧补一格"按 subject 读最新已裁决"
// 的读口；下面 `submitGate` 那条判据（不猜上次是不是批过、少带号只会多一条待办）比那扇窗值钱，
// 所以宁可让销售多点一次发送按钮。
//
// 代价如实记下：明细必须在**开审批之前**读（门序要从那几行的折扣算出来），
// 于是"一条明细都没有的版本"现在连待办都开不出来 —— 比 T-P6-03 那一版更严，方向朝严，
// 且它本来就是那条"须人工处置"的数据事故，不该去麻烦审批人。
//
// 话术在**发送这一刻**重新解析：T-P6-02 文件头写死的义务（那一版不持久化话术）。
// 生效版本在生成与发送之间被运营下线 ⇒ 这一版不许出去。
//
// 到期这一档本层**读时刻而不只读 status**（见 approvalWindowClosed）：报价竖在审批运行时
// off 的那一档下也开待办，而那一档没有清扫协程。残余一条如实记下：窗口关掉之后别人才批上的
// 那条批准，本层照样放行 —— 作废一个人的裁决不是发送腿的权限，正解在清扫器或 Decide 那一侧
// （T-P3-01 的定义侧，本卡不动），已登记为移交项。
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// —— 审批检查点的三个坐标 ——————————————————————————————————————————————

const (
	// QuoteApprovalSubjectType 审批 subject 的类型。小写、无分隔符，过 ApprovalSubmitInput
	// 的字符集判据（[a-z0-9_.-]，那三个键都会进闸门基线的正则匹配面）。
	QuoteApprovalSubjectType = "quote"

	// QuoteSendPolicyKey 「把这一版发给客户」这道门。
	// 刻意不含版本、也不含报价编号：它们都在 subject_id 那一列里。
	QuoteSendPolicyKey = "quote.send"

	// QuoteDiscountPolicyKey 折扣达到阈值时追加的**第二道**门（T-P6-04）。
	// 与低档共用同一个 subject（同一张版本行主键），只靠这一格把两道裁决分开 ——
	// 判据的出处就是 model/approval_request.go:33：policy_key 必须进幂等键，
	// 否则"批了发送"会顺手把"批了这个折扣"也答掉，那是闸门被自己绕过。
	QuoteDiscountPolicyKey = "quote.discount_high"
)

// 发送腿的处置值。这四个串就是 HTTP 状态码的映射依据（awaiting→202、sent→200、
// 其余两档→409），集合由反射用例钉在 quote_send_test.go。
const (
	QuoteSendAwaiting = "awaiting" // 已开待办，本次不外发（AC② 的那一档）
	QuoteSendSent     = "sent"     // 已认领并交给出域出口
	QuoteSendRejected = "rejected" // 有人说了不
	QuoteSendExpired  = "expired"  // 没人说，窗口过去了
)

var (
	// ErrQuoteSendInputInvalid 入参不合法（调用方的事：改载荷）。
	ErrQuoteSendInputInvalid = errors.New("quote send: 入参不合法")
	// ErrQuoteSendNotDraft 这一版不在草稿位（已发出 / 已过期 / 已被接受）。
	ErrQuoteSendNotDraft = errors.New("quote send: 该版本不是可发送的草稿")
	// ErrQuoteSendApprovalMismatch 手里那条审批不是"这一版、这道门"的结论。
	// 三种来路各对应一次事故：拿别人的批准开门、拿另一道策略的批准开门、
	// 拿上一版的批准发新版 —— 全由同一格三列比对挡住。
	ErrQuoteSendApprovalMismatch = errors.New("quote send: 审批结论与本次发送对象不符")
	// ErrQuoteSendOutboundFailed 交给出域出口时失败（原始错因随 %w 带出）。
	ErrQuoteSendOutboundFailed = errors.New("quote send: 外发失败")
	// ErrQuoteSendStatusStuck 状态写不动：认领写不进去，或外发失败后连回滚都写不进去。
	// 单独成一个哨兵是因为**处置动作不同**：前者重试即可，后者要人工核对库里那一版。
	ErrQuoteSendStatusStuck = errors.New("quote send: 报价状态没能落到该落的位置")
	// ErrQuoteSendLinesMissing 版本行在库里而行项目一条都没有。
	// 这是数据事故而不是入参问题：T-P6-02 把"版本行已落库但行项目没跟上"写成了一条
	// 必须人工处置的错，本层是那句话的下游执行方 —— 不发一份合计 0.00 的价格文件。
	ErrQuoteSendLinesMissing = errors.New("quote send: 该版本没有行项目，合计无从算出")
	// ErrQuoteSendRecipientMissing 顺着报价找不到"发给谁"（商机行没了，或那条商机
	// 还没有 OneID）。它与 LinesMissing 分开成两个哨兵，因为修法不同：明细缺了要补那一版，
	// 而身份缺了要去补线索/商机 —— 发不出去的是**人**，不是那张价格文件。
	ErrQuoteSendRecipientMissing = errors.New("quote send: 这条报价找不到收件人（来源商机没有客户身份）")
	// ErrQuoteSendVerdictUnknown 审批行的 status 不在值域里（库里出现了未知状态）。
	// 不映射成任何处置值：未知状态既不是"批了"也不是"没批"，猜哪一侧都可能在猜错那天发出去。
	ErrQuoteSendVerdictUnknown = errors.New("quote send: 审批行的状态不在已知值域内")
)

// quoteSendStore 发送腿对存储的全部依赖。
//
// 与生成腿那份 quoteStore 的差别只有一格 UpdateStatus，而这一格正是两张卡的分界：
// 生成侧的类型里没有它，发送侧的类型里有它且**只有**它（方法集由反射用例钉成三个）。
// 于是"未过闸门就发出去"在两侧都不是被约定挡住的。
type quoteSendStore interface {
	GetByID(ctx context.Context, id string) (*model.Quote, error)
	UpdateStatus(ctx context.Context, id, from, to string) error
	ListLines(ctx context.Context, quoteRowID string) ([]*model.QuoteLineItem, error)
}

// quoteSendApproval 结论的来源。只要两格：问一次（Submit，幂等复用 pending）与按号读一条（Get）。
//
// 刻意不接 `*ApprovalRequestService` 的具体类型：本层用不到 Decide / ExpireOverdue / 待办投递，
// 而审批的裁决入口一旦被发送腿拿到，"发的人自己批"就成了可能。
type quoteSendApproval interface {
	Submit(ctx context.Context, in ApprovalSubmitInput) (*model.ApprovalRequest, bool, error)
	Get(ctx context.Context, id string) (*model.ApprovalRequest, error)
}

// quoteOpenApprovalReader 「这一版当前开着的那条待办」的读口（AC② 留下的号丢了之后的找回路）。
//
// 只有仓储上有这一格（ApprovalRequestService 刻意不包一层）：审批服务是写侧的门面，
// 而这里是纯读。多包一层会让"读自己的 pending"和"入队一条 pending"看起来是同一类动作。
type quoteOpenApprovalReader interface {
	GetPendingBySubject(ctx context.Context, subjectType, subjectID, policyKey string) (*model.ApprovalRequest, error)
}

// quoteReachSender 客户侧出域的唯一出口（与 SOP 外发节点同一口径：窄到一个方法）。
type quoteReachSender interface {
	ReachByCustomer(ctx context.Context, req *ProactiveReachRequest) (*ProactiveReachResponse, error)
}

// quoteEventWriter AC③ 的落点。只要 Create：发送腿不读事件流（那是看板的读侧）。
type quoteEventWriter interface {
	Create(ctx context.Context, ev *model.SalesEvent) error
}

// QuoteSendInput 一次发送的全部输入。
//
// **没有**正文 / 合计 / 收件人 / 显式联系方式 / 审批凭证字段（字段集合由反射用例钉成白名单）：
// 正文与合计从库里那些行算出（同 AC③ 的口径），收件人从库里那条商机算出（见文件头判据④），
// 显式 Phone/Email 会在外发服务里绕过冷却判据（见 sop_reach_send.go 文件头第 29-31 行），
// 把恢复凭证递进来等于邀请调用方去猜结论。
//
// 剩下三格里，Operator 是**唯一的"谁"**：它是审计里的操作者，与"发给谁"分属两格
// （sales_events.owner_id 与 quotes→opportunities.one_id），合成一格就读不出
// "销售甲把属于乙的报价发出去了"这件事。
type QuoteSendInput struct {
	QuoteRowID string // 必填：quotes.id（**版本行主键**，不是 quotes.quote_id 逻辑号）
	ApprovalID string // 可空：空 = 本次只负责开一条待办；非空 = 带着那条结论来派发
	Operator   string // 必填：谁点的发送，落进 sales_events.owner_id（varchar(64)）
}

// QuoteSendResult 一次发送的处置结果。
//
// 只有 Status 与 Disposition 两件事要区分：前者是库里那一版现在是什么，
// 后者是这一次调用做了什么。合成一个字段就没法表达"仍是草稿、但开了一条待办"。
type QuoteSendResult struct {
	Disposition   string     `json:"disposition"`
	ApprovalID    string     `json:"approval_id,omitempty"`
	Status        string     `json:"status"`
	SentAt        *time.Time `json:"sent_at,omitempty"`
	EventRecorded bool       `json:"event_recorded"`
}

// QuoteSendService 报价发送层。依赖全部注入，本层不自己去拿全局 DB。
type QuoteSendService struct {
	store quoteSendStore
	// opps 只用来回答"这条报价是谁的"（收件人身份的唯一的来源），
	// 端口沿用生成腿那份 opportunityReader：它只有 GetByID，本层拿不到改商机的任何一格。
	opps      opportunityReader
	approvals quoteSendApproval
	open      quoteOpenApprovalReader
	reach     quoteReachSender
	events    quoteEventWriter
	cfg       quoteConfigGetter
	scripts   QuoteScriptPort
	gate      LTCConfigReader
	now       func() time.Time
}

// NewQuoteSendService 构造。九个依赖全走参数（与生成腿的 Set 口径不同是有意的：
// 发送腿缺任何一个都发不出去，也没有"先跑起来再补"的中间态）。
//
// 缺件时构造照旧成功，由 Available / Send 报出来 —— 装配点要能在没有配置存储的
// 环境里起来，但绝不能因此"静默不发"。
func NewQuoteSendService(store quoteSendStore, opps opportunityReader, approvals quoteSendApproval,
	open quoteOpenApprovalReader, reach quoteReachSender, events quoteEventWriter, cfg quoteConfigGetter,
	scripts QuoteScriptPort, gate LTCConfigReader) *QuoteSendService {
	return &QuoteSendService{
		store: store, opps: opps, approvals: approvals, open: open, reach: reach,
		events: events, cfg: cfg, scripts: scripts, gate: gate, now: time.Now,
	}
}

// SetClock 注入时钟（sent_at 与事件发生时刻共用它）。
func (s *QuoteSendService) SetClock(now func() time.Time) {
	if s == nil {
		return
	}
	if now == nil {
		now = time.Now
	}
	s.now = now
}

// SetGate 换掉闸门读口。与生成腿各自持一份是刻意的：同一份配置的两次读，
// 中间翻开的开关在两侧看到的可以不同，而那正是"生成时开着、发送时关着"的日常场景。
func (s *QuoteSendService) SetGate(reader LTCConfigReader) {
	if s == nil {
		return
	}
	s.gate = reader
}

// Available 报告能不能发送。闸门配置**不在**这一条里（nil 那份按"关"处理，
// 那是运营的选择不是故障），但 gate 这个端口缺件是装配事故，必须报得出来。
func (s *QuoteSendService) Available() bool {
	return s != nil && s.store != nil && s.opps != nil && s.approvals != nil && s.open != nil &&
		s.reach != nil && s.events != nil && s.cfg != nil && s.scripts != nil && s.gate != nil
}

// OpenApproval 读一版报价当前开着的那条待办；没有开放审批回 (nil, nil)。
//
// 存在的理由很具体：Send 返回里那个审批号一旦丢了，调用方只能重开一条待办
// （见测试 SendAfterDecision 那一例）。有了这一格，"停在 pending"才是**可查**的事实。
// 它不校验版本行是否存在：读的是审批表，且"这一版存不存在"由发送那一步判。
//
// 两道门之后（T-P6-04）它按 [quote.send, quote.discount_high] 的顺序查，取先命中的那一条。
// 这里**不**去读报价行算门序：第二道门的待办只可能在第一道已经出结论之后才被开出来
// （串行判据见 Send），所以"低档还开着"时第一查就命中，"低档批完、高档刚开出来"这个
// 最容易丢号的窗口里第一查落空、第二查命中 —— 两格谁在场都答得对，而不用先知道折扣有多大。
// 残余一种形状如实记下：若哪天有人把第二道门批掉、又有一次不带号的调用把第一道重新开出来，
// 这一格会先读到那条新开的低档待办。方向是"多等一道"，不是"少一道"。
func (s *QuoteSendService) OpenApproval(ctx context.Context, quoteRowID string) (*model.ApprovalRequest, error) {
	if !s.Available() {
		return nil, ErrQuoteServiceUnavailable
	}
	id := strings.TrimSpace(quoteRowID)
	if id == "" {
		return nil, fmt.Errorf("%w: 版本行号为空（开放审批是按版本行查的，逻辑号会跨版本串结论）", ErrQuoteSendInputInvalid)
	}
	for _, policyKey := range quoteGateKeys() {
		appr, err := s.open.GetPendingBySubject(ctx, QuoteApprovalSubjectType, id, policyKey)
		if err != nil {
			return nil, fmt.Errorf("quote send: 查 %s 的 %s 开放审批失败：%w", id, policyKey, err)
		}
		if appr != nil {
			return appr, nil
		}
	}
	return nil, nil
}

// Send 走一次发送。见文件头的顺序判据。
//
// 返回值的形状刻意是"处置 + 结果"而不是 error 表达等待：awaiting / rejected / expired
// 都是本次调用的正常结论，把它们上抛会让调用方写出 retry，把一次待审批变成三次轮询。
// 只有真故障（行读不出、审批号查无此号、结论与对象不符、外发失败）才回 error。
func (s *QuoteSendService) Send(ctx context.Context, in QuoteSendInput) (*QuoteSendResult, error) {
	if !s.Available() {
		return nil, ErrQuoteServiceUnavailable
	}
	rowID, err := in.normalize()
	if err != nil {
		return nil, err
	}
	if detail := quoteStageGateDetail(ctx, s.gate); detail != "" {
		return nil, fmt.Errorf("%w: 拦下原因 %s ⇒ 不开审批、不外发", ErrQuoteGateClosed, detail)
	}

	row, err := s.store.GetByID(ctx, rowID)
	if err != nil {
		return nil, fmt.Errorf("quote send: 读版本行 %s 失败：%w", rowID, err)
	}
	if row == nil {
		return nil, fmt.Errorf("%w: 行键 %s", ErrQuoteVersionMissing, rowID)
	}
	if row.Status != model.QuoteStatusDraft {
		return nil, fmt.Errorf("%w: %s 当前是 %s（已经发出去的那一版不会重发，同一个人收到两遍同样的报价）",
			ErrQuoteSendNotDraft, row.ID, row.Status)
	}

	// 明细在开审批**之前**读：这一版要过几道门是从那几行的折扣算出来的。门序没算出来就 Submit，
	// 待办会开在不该开的那一格上（低档被批掉之后，高档那一格根本没人要求过 ⇒ 第二道门形同不存在）。
	// 读一次往下传给 dispatch，派发那一步不再读第二遍（两遍之间被人改过行 ⇒ 判据与正文两头空）。
	lines, err := s.store.ListLines(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("quote send: 读 %s 的行项目失败：%w", row.ID, err)
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("%w: 版本行 %s 在库里而明细一条都没有（合计会算成 0.00 发给客户）"+
			"⇒ 这是 T-P6-02 点名的那条数据事故，须人工处置，本次不开审批、不认领、不外发", ErrQuoteSendLinesMissing, row.ID)
	}
	gates := quoteGateSequence(ctx, s.gate, lines)

	if in.ApprovalID == "" {
		return s.walkGates(ctx, row, lines, gates, 0, "", in)
	}
	appr, err := s.carriedApproval(ctx, row.ID, in.ApprovalID, gates)
	if err != nil {
		return nil, err
	}
	if appr.Status != model.ApprovalStatusApproved {
		return s.notApprovedResult(appr, row.Status)
	}
	// 手里这条已经批了：只有它是门序里**最后一道**时才派发，否则本次的处置是"把下一道开出来"。
	return s.walkGates(ctx, row, lines, gates, gateIndex(gates, appr.PolicyKey)+1, appr.ID, in)
}

// walkGates 从第 from 道门起逐道入队，直到某一道给出"还没批"的结论，或者门序走完去派发。
//
// 循环而不是"只问第一道"：自动放行策略那一档下（审批运行时 off 或策略当场点头），
// 一次调用要把整条门序走完才有意义 —— 停在第一道的话，销售得在策略已经两道都点头之后再点两次，
// 而那两次里的每一次都会重新开一条待办（不带号的判据就是"多一条待办"）。
//
// 传下去的审批号是**最后一道门**那一条：外发失败退回草稿时提示"凭同一条审批可再发一次"，
// 指的必须是拿来能真的再发一次的那条号（低档那条已批的号再点一次只会重开高档待办）。
func (s *QuoteSendService) walkGates(ctx context.Context, row *model.Quote, lines []*model.QuoteLineItem,
	gates []string, from int, priorApprovalID string, in QuoteSendInput) (*QuoteSendResult, error) {
	lastID := priorApprovalID
	for i := from; i < len(gates); i++ {
		appr, err := s.submitGate(ctx, row.ID, gates[i])
		if err != nil {
			return nil, err
		}
		if appr.Status != model.ApprovalStatusApproved {
			return s.notApprovedResult(appr, row.Status)
		}
		lastID = appr.ID
	}
	return s.dispatch(ctx, row, lastID, lines, in)
}

// approvalWindowClosed 审批窗口关掉了没有（比较口径与仓储那次清扫同源：`expires_at <= now`）。
//
// 为什么本层要自己看一眼时刻而不只看 status：报价竖的审批服务在审批运行时 off 的那一档下
// 也能开待办（不然销售的按钮点了没地方去），而那一档没有清扫协程在跑，
// 过期的 pending 会永远停在 pending —— 只看 status 就会一直回"还在等"。
//
// 只读不写：把 pending 落成 expired 是 `ExpireOverdue` 那一条唯一的写路径。发送腿若也去改
// 那一行，库里就有两个地方在写审批状态，而"是谁把它改成过期的"正是事后要回答的那件事。
// expires_at 为 NULL = 这条没有窗口，永远算"还在等"（与清扫只挑非 NULL 那一条一致）。
func approvalWindowClosed(expiresAt *time.Time, now time.Time) bool {
	return expiresAt != nil && !now.Before(*expiresAt)
}

// quoteGateKeys 门序**可能**长成的样子（按要过的顺序）。恒含低档；不含第二道的判据在下面。
func quoteGateKeys() []string {
	return []string{QuoteSendPolicyKey, QuoteDiscountPolicyKey}
}

// quoteGateSequence 这一版要过的门，按顺序。低档永远要过；折扣达到阈值时追加高档一道。
//
// 判据只有这一个出口（用例按"两道都自动放行"那一格反证它被走到两次）：
// 阈值取 `ltc.config.thresholds.discount_percent`，比较**含等于** —— 卡面 AC① 点名的就是那一格，
// 而"达到"与"超过"的差别在 15% 那一档上会直接决定一笔生意走不走第二次裁决。
func quoteGateSequence(ctx context.Context, reader LTCConfigReader, lines []*model.QuoteLineItem) []string {
	gates := []string{QuoteSendPolicyKey}
	if quoteNeedsHighDiscountApproval(quoteMaxDiscountPercent(lines), quoteDiscountThreshold(ctx, reader)) {
		gates = append(gates, QuoteDiscountPolicyKey)
	}
	return gates
}

// quoteMaxDiscountPercent 取所有行里**最大**的那个折扣，不是合计、不是平均、不是第一行。
//
// 判据是"这笔生意让出去的幅度有多大"，而一版报价里最狠的那一行就是那个幅度：
// 取合计会让三行各 5% 变成 15%（客户其实一分没多拿），取平均会把一行 40% 摊平到看不见。
func quoteMaxDiscountPercent(lines []*model.QuoteLineItem) float64 {
	var max float64
	for _, l := range lines {
		if l != nil && l.DiscountPercent > max {
			max = l.DiscountPercent
		}
	}
	return max
}

// quoteNeedsHighDiscountApproval 这一版要不要第二道裁决。
//
// `max > 0` 那一臂不是防御性判空，是阈值 0 那一档的语义：ltc_config.go 里 discount_percent 的
// ZeroLegal 写的就是「0 = 任何折扣都要审批」，而不是「原价也要审批」。少了那一臂，
// 阈值 0 会把每一版原价报价都送进第二道门 —— 配置面上完全合法，效果是发送键从此按不动。
func quoteNeedsHighDiscountApproval(maxDiscount, threshold float64) bool {
	return maxDiscount > 0 && maxDiscount >= threshold
}

// quoteDiscountThreshold 读那一格阈值。
//
// 读不到（端口给不出配置）时取 0 而不是取默认那份 15：发送腿没有"没有策略"这一档，
// 只能往严的那一侧倒（0 = 任何折扣都要第二道门）。今天这一格走不到：同一份 reader
// 在 gate 那一步就会把 nil/降级那份判成"阶段关着"而整条发送被拒 —— 留着是因为
// "读不到就当没有阈值"这种默认值将来一定会被人踩，写下来比不写贵不了多少。
func quoteDiscountThreshold(ctx context.Context, reader LTCConfigReader) float64 {
	if reader == nil {
		return 0
	}
	cfg := reader.Config(ctx)
	if cfg == nil {
		return 0
	}
	return cfg.Thresholds.DiscountPercent
}

// gateIndex 这条 policy_key 在门序里的第几格；不在则 -1。
func gateIndex(gates []string, policyKey string) int {
	for i, g := range gates {
		if g == policyKey {
			return i
		}
	}
	return -1
}

// notApprovedResult 把一条"没有放行"的结论换成本层的处置。只在 status != approved 时调用。
//
// 未知状态（库里出现值域外的 status）报 error 而不是猜一个处置：它既不是"批了"也不是"没批"，
// 猜哪一侧都可能在猜错那天把该拦的放出去、或把该发的焊死。
func (s *QuoteSendService) notApprovedResult(appr *model.ApprovalRequest, rowStatus string) (*QuoteSendResult, error) {
	switch appr.Status {
	case model.ApprovalStatusPending:
		if approvalWindowClosed(appr.ExpiresAt, s.now()) {
			// 库里那一行还写着 pending，但那句话已经过期了：本层的处置是 expired，
			// 且**不去改那一行**（落终态是清扫那一方唯一的写路径，见 helper 的注释）。
			return &QuoteSendResult{
				Disposition: QuoteSendExpired, ApprovalID: appr.ID, Status: rowStatus,
			}, nil
		}
		return &QuoteSendResult{
			Disposition: QuoteSendAwaiting, ApprovalID: appr.ID, Status: rowStatus,
		}, nil
	case model.ApprovalStatusRejected:
		// 库里那一版**留**在草稿：报价生命周期的 rejected 说的是"客户拒了这份报价"，
		// 不是"内部审批没通过"。两件事合成一个值之后，"被自己人拦下"的报价就再也发不出去了。
		// 两道门都走这一格：被第二道拦下时，低档那条批准仍然留在库里（它批的是另一件事）。
		return &QuoteSendResult{
			Disposition: QuoteSendRejected, ApprovalID: appr.ID, Status: rowStatus,
		}, nil
	case model.ApprovalStatusExpired:
		return &QuoteSendResult{
			Disposition: QuoteSendExpired, ApprovalID: appr.ID, Status: rowStatus,
		}, nil
	default:
		return nil, fmt.Errorf("%w: 审批 %s 的 status=%q 既不是放行也不是拒绝（未知状态一律不发）",
			ErrQuoteSendVerdictUnknown, appr.ID, appr.Status)
	}
}

// submitGate 入队一道门并回那条结论（幂等：同一 (subject, policy) 的 pending 复用同一行）。
//
// **不**在这里猜"上次是不是批过"：审批侧没有"按 subject 读最新已裁决"那一格读口，
// 补一格的代价是让任何一次没带号的调用都能沿用旧结论（同一版可以反复发出去）。
// 少带号的后果因此只会是多一条待办 —— 更严，不会更松。
//
// 已批准的那一条也会被这里读出来：自动放行策略那档下 Submit 当场就回 approved（同步退化态），
// 于是门序能在同一次调用里往下走一格 —— 策略答的是两道门各自的"可以"，见下面的 policy_key。
func (s *QuoteSendService) submitGate(ctx context.Context, rowID, policyKey string) (*model.ApprovalRequest, error) {
	appr, _, err := s.approvals.Submit(ctx, ApprovalSubmitInput{
		SubjectType: QuoteApprovalSubjectType, SubjectID: rowID, PolicyKey: policyKey,
	})
	if err != nil {
		return nil, fmt.Errorf("quote send: 为版本 %s 的 %s 入队审批失败：%w", rowID, policyKey, err)
	}
	if appr == nil {
		// Submit 的 (nil, false, nil) 不存在（要么给行要么给错），走到这里就是实现漂了：
		// 按"没拿到批准"处置，不发。
		return nil, fmt.Errorf("%w: Submit 对版本 %s 的 %s 返回了空记录 ⇒ 本次不外发",
			ErrQuoteSendVerdictUnknown, rowID, policyKey)
	}
	return appr, nil
}

// carriedApproval 按号读回调用方递来的那条结论，并核对它确实是"这一版、门序里的某一道"。
//
// 三列里 policy_key 那一格从"等于低档"变成"在门序里"：这是本卡唯一放宽的一处，
// 而放宽的方向被下面那道派发判据原地咬住 —— 门序里的**非最后**一道批完也不派发，
// 派发只认最后那道。所以"另一道策略的批准"今天有两种：不该出现在这一版上的（报 mismatch），
// 和该出现但只算半程的（开出下一道）。
func (s *QuoteSendService) carriedApproval(ctx context.Context, rowID, approvalID string,
	gates []string) (*model.ApprovalRequest, error) {
	appr, err := s.approvals.Get(ctx, approvalID)
	if err != nil {
		return nil, fmt.Errorf("quote send: 读审批 %s 失败：%w", approvalID, err)
	}
	if appr == nil {
		return nil, fmt.Errorf("%w: %s（拿不到结论 ⇒ 不外发）", ErrApprovalNotFound, approvalID)
	}
	if appr.SubjectType != QuoteApprovalSubjectType || appr.SubjectID != rowID ||
		gateIndex(gates, appr.PolicyKey) < 0 {
		return nil, fmt.Errorf("%w: 审批 %s 是 (subject_type=%s, subject_id=%s, policy_key=%s) 的结论，"+
			"而本次要发的是 (%s, %s)，它要过的门按顺序是 [%s] ⇒ 三列必须逐一对上，少比一列就是给别的对象开门",
			ErrQuoteSendApprovalMismatch, appr.ID,
			appr.SubjectType, appr.SubjectID, appr.PolicyKey,
			QuoteApprovalSubjectType, rowID, strings.Join(gates, " → "))
	}
	return appr, nil
}

// dispatch 派发：门序里**最后一道**已经批过之后，才走得进这一格（AC① 的那一处唯一入口）。
//
// 顺序仍是"补齐完整性判据（收件人、话术）→ 认领 → 出域 → 回写事件"。
// 明细由调用方带进来（上面刚读过，门序就是从它算的）：再读一遍会让"按哪几行算的档"
// 与"发出去的那份合计"之间开一个窗口，而那一窗口里改一次行就能让低档批准配上一份更贵的正文。
func (s *QuoteSendService) dispatch(ctx context.Context, row *model.Quote, approvalID string,
	lines []*model.QuoteLineItem, in QuoteSendInput) (*QuoteSendResult, error) {
	oneID, err := s.resolveRecipient(ctx, row)
	if err != nil {
		return nil, err
	}
	script, err := resolveQuoteScript(ctx, s.cfg, s.scripts, oneID)
	if err != nil {
		return nil, err
	}
	content := quoteSendContent(row, quoteSumAmount(lines), script)

	// —— 认领：draft→sent 的 CAS（WHERE id = ? AND status = 'draft'）——
	if err := s.store.UpdateStatus(ctx, row.ID, model.QuoteStatusDraft, model.QuoteStatusSent); err != nil {
		return nil, fmt.Errorf("%w: 认领 %s（draft→sent）没落库，本次不外发（并发对手可能已先认领这一版）：%v",
			ErrQuoteSendStatusStuck, row.ID, err)
	}

	resp, rerr := s.reach.ReachByCustomer(ctx, &ProactiveReachRequest{
		// 只交身份、不交 Phone/Email：显式收件人在外发服务里走的是绕过冷却判据的那两条分支。
		OneID:   oneID,
		Content: content,
	})
	if rerr != nil {
		outboundErr := fmt.Errorf("%w: 版本 %s 交给出域出口失败：%w", ErrQuoteSendOutboundFailed, row.ID, rerr)
		if berr := s.store.UpdateStatus(ctx, row.ID, model.QuoteStatusSent, model.QuoteStatusDraft); berr != nil {
			// 两件事必须同时在场、且都能被 errors.Is 分到：只报"外发失败"会让人以为能安全
			// 重试（库里已是 sent，重试会被 NotDraft 挡掉）；只报"状态写坏了"会让人以为
			// 东西还在自己手上（其实这一版该发而没发出去）。
			return nil, fmt.Errorf("%w ⇒ 且状态没退回草稿（库里这一版仍是 sent 而客户没收到，须人工核对，别盲目重试）：%w ⇒ %w",
				outboundErr, ErrQuoteSendStatusStuck, berr)
		}
		// 退回成功：这一版还是草稿、结论也还在（同一次批准对应同一次动作），
		// 渠道恢复后凭同一个审批号再点一次即可，不必再麻烦审批人。
		return nil, fmt.Errorf("%w ⇒ 已退回草稿，凭同一条审批 %s 可再发一次", outboundErr, approvalID)
	}

	now := s.now()
	recorded := true
	ev := &model.SalesEvent{
		EventType:     model.SalesEventTypeQuote,
		OpportunityID: row.OpportunityID,
		QuoteID:       row.QuoteID,
		OwnerID:       in.Operator,
		OccurredAt:    now,
	}
	if err := s.events.Create(ctx, ev); err != nil {
		recorded = false
		logger.Ctx(ctx).Warn().
			Str("quote_row_id", row.ID).
			Str("approval_id", approvalID).
			Err(err).
			Msg("quote send: 外发已出域但销售事件没写进去，AC③ 的审计链在这一版上缺一条")
	}
	logger.Ctx(ctx).Info().
		Str("quote_row_id", row.ID).
		Str("approval_id", approvalID).
		Str("channel", reachChannel(resp)).
		Str("message_id", reachMessageID(resp)).
		Bool("event_recorded", recorded).
		Msg("quote send dispatched")

	return &QuoteSendResult{
		Disposition: QuoteSendSent, ApprovalID: approvalID,
		Status: model.QuoteStatusSent, SentAt: &now, EventRecorded: recorded,
	}, nil
}

// resolveRecipient 顺着报价找出收件人：版本行 → opportunity_id → opportunities.one_id。
//
// 三档各有一句不同的话：链本身没落值（数据事故）、商机行读不出（存储的事，可以重试）、
// 商机行没了或那一格空着（还没认到人）。第三档最容易被误当成 bug：一条从线索转来、
// 客户还没留过联系方式的商机，确实发不了报价 —— 那是事实，不是本层的缺陷，
// 所以它必须回一句能照着做的话，而不是回一条"外发服务返回身份不存在"。
func (s *QuoteSendService) resolveRecipient(ctx context.Context, row *model.Quote) (string, error) {
	oppID := strings.TrimSpace(row.OpportunityID)
	if oppID == "" {
		return "", fmt.Errorf("%w: 版本行 %s 的 opportunity_id 是空的（T-P6-02 的生成腿不会产出这种行，"+
			"它只能是手工插进去的）⇒ 一列都不写、不外发", ErrQuoteSendRecipientMissing, row.ID)
	}
	opp, err := s.opps.GetByID(ctx, oppID)
	if err != nil {
		return "", fmt.Errorf("quote send: 读报价 %s 的来源商机 %s 失败：%w", row.ID, oppID, err)
	}
	if opp == nil {
		return "", fmt.Errorf("%w: 商机 %s 已经不在了（报价那一行还挂着它），版本 %s 无处可发 ⇒ 一列都不写、不外发",
			ErrQuoteSendRecipientMissing, oppID, row.ID)
	}
	oneID := strings.TrimSpace(opp.OneID)
	if oneID == "" {
		return "", fmt.Errorf("%w: 商机 %s 的 one_id 还没落值（客户身份没认到），版本 %s 无处可发 ⇒ 一列都不写、不外发",
			ErrQuoteSendRecipientMissing, oppID, row.ID)
	}
	return oneID, nil
}

// normalize 收口入参：去空白、判必填、按**落库列宽**判 Operator。
//
// 宽度判在这里而不是让 PG 报：owner_id 是 varchar(64)，超长要在客户已经收到报价之后
// 才炸出来的话，错的就是一个不可回滚的位置（与 T-P6-02 的 numeric 溢出同一族判据）。
func (in *QuoteSendInput) normalize() (string, error) {
	in.QuoteRowID = strings.TrimSpace(in.QuoteRowID)
	in.ApprovalID = strings.TrimSpace(in.ApprovalID)
	in.Operator = strings.TrimSpace(in.Operator)

	if in.QuoteRowID == "" {
		return "", fmt.Errorf("%w: 版本行号为空（发送对象必须指到具体某一版）", ErrQuoteSendInputInvalid)
	}
	if in.Operator == "" {
		return "", fmt.Errorf("%w: 操作者为空（sales_events.owner_id 没有值就无法回答是谁点的发送）", ErrQuoteSendInputInvalid)
	}
	if len(in.Operator) > quoteEventOwnerMaxLen {
		return "", fmt.Errorf("%w: 操作者长度 %d 超 %d（列宽 varchar(64)）",
			ErrQuoteSendInputInvalid, len(in.Operator), quoteEventOwnerMaxLen)
	}
	return in.QuoteRowID, nil
}

// quoteEventOwnerMaxLen sales_events.owner_id 的列宽（model.SalesEvent 那个 varchar(64)）。
const quoteEventOwnerMaxLen = 64

// quoteSendContent 拼出交给客户的正文：生效话术 + 从库里那些行算出的合计 + 是哪一版。
//
// 版本行键与"第 N 版"都要在正文里：客户手上会有多版，谈判记录里只有一行 quotes
// 却说不清发出去的是哪一版，事后就对不上账。
func quoteSendContent(row *model.Quote, total float64, script QuoteScript) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(script.Content))
	b.WriteString("\n\n——\n报价单 ")
	b.WriteString(row.ID)
	fmt.Fprintf(&b, "｜第 %d 版｜合计 %s %.2f", row.Version, row.Currency, total)
	if row.ValidUntil != nil {
		// 有效期按 UTC 格式化：宿主机时区（本地）与库内 CST 会话两套口径下，
		// 同一条记录给客户看到的日期不该变（日期边界裂脑那条老账）。
		fmt.Fprintf(&b, "｜有效期至 %s", row.ValidUntil.UTC().Format("2006-01-02"))
	}
	return b.String()
}

func reachChannel(resp *ProactiveReachResponse) string {
	if resp == nil {
		return ""
	}
	return resp.Channel
}

func reachMessageID(resp *ProactiveReachResponse) string {
	if resp == nil {
		return ""
	}
	return resp.MessageID
}
