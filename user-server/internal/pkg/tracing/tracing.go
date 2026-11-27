package tracing

import (
	"context"
	"encoding/hex"
	"fmt"
	"hivemtk-user/internal/pkg/textutil"
	"sync"
	"sync/atomic"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/protocolhash"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/trace"
	"hivemtk-user/internal/pkg/utils/logger"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ToolTraceEvent 工具层一次调用（agent 一轮 / 单次工具）的可观测事件。
// 由 tooluse 包在 observer 中点出，tracing 包负责落库（见 ReportToolCall）。
type ToolTraceEvent struct {
	Kind             string
	TraceID          string
	AgentID          string
	SessionID        string
	CustomerID       string
	CallerID         string
	ToolName         string
	TurnIndex        int
	Input            any
	Output           any
	Error            string
	DurationMs       int64
	Status           string
	StartedAt        *time.Time
	CompletedAt      *time.Time
	DurationMeasured bool
}

// ===== 生命周期节点（固定顺序，用于画链路图 + 计算节点间时延） =====
const (
	NodeIngest            = "ingest"
	NodeAITriggerDecision = "ai_trigger_decision"
	NodeAIDispatch        = "ai_dispatch"
	NodeAIResult          = "ai_result"
	NodeOutboundEnqueue   = "outbound_enqueue"
	NodeInboxSync         = "inbox_sync"
	NodeDownlinkFetch     = "downlink_fetch"
	NodeDeliveredAck      = "delivered_ack"
	NodeAgentTurn         = model.NodeAgentTurn
	NodeToolCall          = model.NodeToolCall
)

var nodeOrder = map[string]int{
	NodeIngest:            1,
	NodeAITriggerDecision: 2,
	NodeAIDispatch:        2,
	NodeAIResult:          2,
	NodeOutboundEnqueue:   3,
	NodeInboxSync:         4,
	NodeDownlinkFetch:     5,
	NodeDeliveredAck:      6,
}

// NodeOrder 节点在生命周期中的固定位置（未知节点排末尾）。
func NodeOrder(node string) int {
	if o, ok := nodeOrder[node]; ok {
		return o
	}
	return 99
}

var nodeLabels = map[string]string{
	NodeIngest:            "消息上报接入",
	NodeAITriggerDecision: "AI 触发决策",
	NodeAIDispatch:        "AI 处理编排",
	NodeAIResult:          "生成结果安全出口",
	NodeOutboundEnqueue:   "AI 回复出站入队",
	NodeInboxSync:         "收件箱同步",
	NodeDownlinkFetch:     "下行出库拉取",
	NodeDeliveredAck:      "送达确认",
	NodeAgentTurn:         "Agent 一轮推理",
	NodeToolCall:          "工具调用",
}

// NodeLabel 节点中文名（UI 展示）。
func NodeLabel(node string) string {
	if l, ok := nodeLabels[node]; ok {
		return l
	}
	return node
}

var (
	spanCh    chan pendingSpan
	sinkOnce  sync.Once
	dropped   int64
	published int64
	stopCh    chan struct{}
	stopDone  chan struct{}
)

const sinkBuffer = 8192

type pendingSpan struct {
	tenantID         uint
	carrier          *Carrier
	traceID          string
	conversationID   string
	accountID        string
	channel          string
	node             string
	nodeOrder        int
	direction        string
	msgID            string
	input            any
	output           any
	durationMs       int64
	expected         string
	status           string
	abnormal         string
	errorStr         string
	parentNode       string
	spanKind         string
	turnIndex        int
	toolName         string
	agentID          string
	turnID           string
	answerAttempt    int
	deliveryAttempt  int
	startedAt        *time.Time
	completedAt      *time.Time
	durationMeasured bool
}

// Init 启动后台批量落库 worker。
// 必须在进程启动时调用一次（通常在 router 初始化 DB 之后）。
// 工具层 observer 由 router 在 Init 后通过 tooluse.ToolTraceSink = tracing.ReportToolCall 接线，
// 从而以观察者模式自动采集 agent 多轮 / 多工具，且不引入 tracing↔tooluse 的循环依赖。
func Init(d *gorm.DB) {
	sinkOnce.Do(func() {
		spanCh = make(chan pendingSpan, sinkBuffer)
		stopCh = make(chan struct{})
		stopDone = make(chan struct{})
		go flushLoop(d)
	})
}

func flushLoop(d *gorm.DB) {
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	batch := make([]pendingSpan, 0, 200)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if d == nil {
			d = db.GetDB()
		}
		if d == nil {
			batch = batch[:0]
			return
		}
		flushSpans(d, batch)
		batch = batch[:0]
	}
	for {
		select {
		case span := <-spanCh:
			batch = append(batch, span)
			if len(batch) >= 200 {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-stopCh:
			for {
				select {
				case span := <-spanCh:
					batch = append(batch, span)
					if len(batch) >= 200 {
						flush()
					}
				default:
					flush()
					close(stopDone)
					return
				}
			}
		}
	}
}

// flushSpans 按租户分组落库：隔离 callback 以 ctx 绑定的租户覆盖 tenant_id，
// 混租户的批次必须拆组、每组一个 ctx；无归属的行保持无绑定 ctx，
// on 模式由 callback fail-closed（与业务写同一口径，绝不静默落到默认租户）。
func flushSpans(d *gorm.DB, spans []pendingSpan) {
	groups := make(map[uint][]*model.MessageTrace)
	for _, p := range spans {
		groups[p.tenantID] = append(groups[p.tenantID], toModelFromPending(p))
	}
	for tenantID, rows := range groups {
		if tenantID == 0 {
			if tenant.IsMultiTenant() {
				// 公开路由（健康检查等）没有租户归属：多租户下落库必被隔离 callback
				// 拒绝，逐批打错误日志只会淹没真正的问题——直接跳过，绝不落默认租户。
				logger.Debugf("tracing.flush: skip %d unbound spans", len(rows))
				continue
			}
			tenantID = tenant.DefaultTenantID // 单租户：显式绑定默认租户，与中间件口径一致
		}
		ctx := tenant.WithTenantID(context.Background(), tenantID)
		if err := d.WithContext(ctx).CreateInBatches(rows, 200).Error; err != nil {
			logger.Errorf("tracing.flush: batch insert failed (tenant=%d): %v", tenantID, err)
		}
	}
}

// Stop 优雅停止后台落库 worker：排空缓冲区并做最后一次批量落库，确保进程退出不丢 trace。
// 幂等：重复调用安全；未 Init（spanCh 为 nil）时直接返回。
func Stop() {
	if spanCh == nil {
		return
	}
	select {
	case <-stopCh:
	default:
		close(stopCh)
	}
	select {
	case <-stopDone:
	case <-time.After(3 * time.Second):
	}
}

// Publish 非阻塞投递一条 span。缓冲满则丢弃并计数，绝不阻塞调用方。
func Publish(p pendingSpan) {
	if spanCh == nil {
		if d := db.GetDB(); d != nil {
			_ = d.WithContext(tenant.WithTenantID(context.Background(), p.tenantID)).
				Create(toModelFromPending(p)).Error
		}
		return
	}
	atomic.AddInt64(&published, 1)
	select {
	case spanCh <- p:
	default:
		atomic.AddInt64(&dropped, 1)
	}
}

// Stats 返回投递统计（运维/健康采集用）。
// 注意：不可使用命名返回值，否则 &published 会取到局部返回变量（恒为 0），
// 导致 Stats() 永远返回 0 —— 监控面板看到的 trace 投递/丢弃率将失真。
func Stats() (int64, int64) {
	return atomic.LoadInt64(&published), atomic.LoadInt64(&dropped)
}

// dbCtx 保护传给 GORM 的 ctx：nil 会让隔离 callback 在 ctx.Value 上 panic。
func dbCtx(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

type carrierKey struct{}

// Carrier 一次业务处理的归属信息，随 context 透传。
type Carrier struct {
	TenantID        uint
	TraceID         string
	ConversationID  string
	AccountID       string
	Channel         string
	MsgID           string
	Direction       string
	TurnID          string
	AnswerAttempt   int
	DeliveryAttempt int
}

// NewCarrier 构造新载体（自动生成 trace_id）。
func NewCarrier(channel, account, conv string) *Carrier {
	return &Carrier{
		TraceID:        GenerateTraceID(),
		Channel:        channel,
		AccountID:      account,
		ConversationID: conv,
	}
}

// WithCarrier 把载体注入 context。
func WithCarrier(ctx context.Context, c *Carrier) context.Context {
	return context.WithValue(ctx, carrierKey{}, c)
}

// CarrierFromContext 从 context 取出载体；无载体时降级为仅含请求级 trace_id 的空载体。
func CarrierFromContext(ctx context.Context) *Carrier {
	if ctx != nil {
		if c, ok := ctx.Value(carrierKey{}).(*Carrier); ok && c != nil {
			return c
		}
		if tid := trace.TraceIDFromContext(ctx); tid != "" {
			return &Carrier{TraceID: tid}
		}
	}
	return nil
}

// TraceIDFromContext 统一从 context 取 trace_id。
// 优先 Carrier（稳定 tr-xxx），fallback 到 logger 注入的 trace_id。
// 替代分散的裸 logger.TraceIDFromContext(ctx) 调用（15 处已统一）。
func TraceIDFromContext(ctx context.Context) string {
	if c := CarrierFromContext(ctx); c != nil && c.TraceID != "" {
		return c.TraceID
	}
	if ctx != nil {
		if tid := trace.TraceIDFromContext(ctx); tid != "" {
			return tid
		}
	}
	return ""
}

// Child 复制会话/渠道维度（保留同一轮 trace_id），用于子 span 挂在同一个 trace 下。
func (c *Carrier) Child() *Carrier {
	if c == nil {
		return &Carrier{TraceID: GenerateTraceID()}
	}
	cp := *c
	return &cp
}

// WithMsgID 返回携带 msg_id 的副本。
func (c *Carrier) WithMsgID(id string) *Carrier {
	cp := c.Child()
	cp.MsgID = id
	return cp
}

type recalledChunksKey struct{}

// InitRecalledChunks 在业务入口初始化召回 chunk 容器（应在调用 AI 编排前）。
func InitRecalledChunks(ctx context.Context) context.Context {
	if _, ok := ctx.Value(recalledChunksKey{}).(*[]string); ok {
		return ctx
	}
	s := make([]string, 0, 8)
	return context.WithValue(ctx, recalledChunksKey{}, &s)
}

// RecordRecalledChunks 累积记录本次召回的 chunk（多次 RAG 检索可叠加）。
func RecordRecalledChunks(ctx context.Context, ids []string) {
	if len(ids) == 0 {
		return
	}
	v, ok := ctx.Value(recalledChunksKey{}).(*[]string)
	if !ok {
		return
	}
	for _, id := range ids {
		if id != "" {
			*v = append(*v, id)
		}
	}
}

// RecalledChunksOf 取出本次业务涉及的所有召回 chunk（未去重，调用方按需去重）。
func RecalledChunksOf(ctx context.Context) []string {
	if v, ok := ctx.Value(recalledChunksKey{}).(*[]string); ok {
		return *v
	}
	return nil
}

// ===== 流式 Span API（业务侧优雅、低侵入） =====
//
// 用法：
//
//	defer tracing.Start(ctx, tracing.NodeAIDispatch).Input(req).Expected("生成回复").End(output, err)
//
// End 第二个返回值即业务逻辑返回的 error；耗时自动统计。整行即一个节点的埋点，无需手动
// 拼 NodeSpan、无需关心落库——全部由异步 sink 处理。
type Span struct {
	ctx       context.Context
	carrier   *Carrier
	node      string
	order     int
	kind      string
	parent    string
	turn      int
	tool      string
	agent     string
	direction string
	msgID     string
	input     any
	output    any
	expected  string
	abnormal  string
	start     time.Time
}

// Start 开启一个生命周期节点 span（自动继承 context 中的载体与耗时起点）。
func Start(ctx context.Context, node string) *Span {
	return &Span{
		ctx:     ctx,
		carrier: CarrierFromContext(ctx).Child(),
		node:    node,
		order:   NodeOrder(node),
		kind:    model.SpanKindLifecycle,
		start:   time.Now(),
	}
}

// Input 设置入参（任意可 JSON 序列化对象）。
func (s *Span) Input(v any) *Span { s.input = v; return s }

// Output 设置出参（链式可选，也可在 End 时传入）。
func (s *Span) Output(v any) *Span { s.output = v; return s }

// Expected 设置预期结果描述。
func (s *Span) Expected(text string) *Span { s.expected = text; return s }

// MsgID 设置关联消息 ID。
func (s *Span) MsgID(id string) *Span { s.msgID = id; return s }

// Direction 设置方向（inbound/outbound）。
func (s *Span) Direction(d string) *Span { s.direction = d; return s }

// TraceID 显式覆盖 trace_id（如出站复用入站 trace）。
func (s *Span) TraceID(id string) *Span {
	if s.carrier == nil {
		s.carrier = &Carrier{}
	}
	s.carrier.TraceID = id
	return s
}

// Kind 设置层级种类（lifecycle/agent_turn/tool_call）。
func (s *Span) Kind(k string) *Span { s.kind = k; return s }

// Parent 设置父节点名。
func (s *Span) Parent(p string) *Span { s.parent = p; return s }

// Turn 设置 agent 轮次序号。
func (s *Span) Turn(i int) *Span { s.turn = i; return s }

// Tool 设置工具名。
func (s *Span) Tool(name string) *Span { s.tool = name; return s }

// Agent 设置智能体 ID。
func (s *Span) Agent(id string) *Span { s.agent = id; return s }

// Abnormal 设置该 span 异常时的原因详情（空 abnormal 时若发生错误则回退为错误信息），
// 会被持久化到 message_trace.abnormal 列，在监控 Lifecycle / trace-tree 中展示。
func (s *Span) Abnormal(reason string) *Span { s.abnormal = reason; return s }

// End 结束 span 并异步投递。output/err 一般来自业务逻辑返回值。
func (s *Span) End(output any, err error) {
	dur := time.Since(s.start).Milliseconds()
	status := "ok"
	var errStr string
	if err != nil {
		status = "abnormal"
		errStr = ErrStr(err)
	}
	if s.output == nil {
		s.output = output
	}
	Publish(s.toPending(dur, status, errStr))
}

func (s *Span) toPending(dur int64, status, errStr string) pendingSpan {
	c := s.carrier
	if c == nil {
		c = &Carrier{}
	}
	tid := c.TraceID
	if tid == "" && s.ctx != nil {
		tid = trace.TraceIDFromContext(s.ctx)
	}
	kind := s.kind
	if kind == "" {
		kind = model.SpanKindLifecycle
	}
	parent := s.parent
	if parent == "" && kind != model.SpanKindLifecycle {
		parent = NodeAIDispatch
	}
	ab := s.abnormal
	if ab == "" && errStr != "" {
		ab = errStr
	}
	tenantID, _ := tenant.FromContext(s.ctx)
	started := s.start
	completed := time.Now()
	msgID := s.msgID
	if msgID == "" {
		msgID = c.MsgID
	}
	return pendingSpan{
		tenantID:         tenantID,
		carrier:          c,
		traceID:          tid,
		conversationID:   c.ConversationID,
		accountID:        c.AccountID,
		channel:          c.Channel,
		node:             s.node,
		nodeOrder:        s.order,
		direction:        s.direction,
		msgID:            msgID,
		input:            s.input,
		output:           s.output,
		durationMs:       dur,
		expected:         s.expected,
		status:           status,
		abnormal:         ab,
		errorStr:         errStr,
		parentNode:       parent,
		spanKind:         kind,
		turnIndex:        s.turn,
		toolName:         s.tool,
		agentID:          s.agent,
		turnID:           c.TurnID,
		answerAttempt:    c.AnswerAttempt,
		deliveryAttempt:  c.DeliveryAttempt,
		startedAt:        &started,
		completedAt:      &completed,
		durationMeasured: !s.start.IsZero() && dur >= 0,
	}
}

func toModelFromPending(p pendingSpan) *model.MessageTrace {
	started, completed, measured := observationTimes(p.startedAt, p.completedAt, p.durationMeasured, p.durationMs)
	return &model.MessageTrace{
		TenantID:         p.tenantID,
		TraceID:          p.traceID,
		ConversationID:   p.conversationID,
		AccountID:        p.accountID,
		Channel:          p.channel,
		Node:             p.node,
		NodeOrder:        p.nodeOrder,
		Direction:        p.direction,
		MsgID:            p.msgID,
		Input:            tracePayloadJSON(p.input),
		Output:           tracePayloadJSON(p.output),
		DurationMs:       p.durationMs,
		TurnID:           p.turnID,
		AnswerAttempt:    p.answerAttempt,
		DeliveryAttempt:  p.deliveryAttempt,
		TimingVersion:    TimingVersion,
		DurationMeasured: measured,
		StartedAt:        started,
		CompletedAt:      completed,
		Expected:         textutil.TruncateText(p.expected, textutil.DefaultMaxBytes),
		Status:           p.status,
		Abnormal:         textutil.TruncateText(safeTraceText(p.abnormal), textutil.DefaultMaxBytes),
		Error:            textutil.TruncateText(safeTraceText(p.errorStr), textutil.DefaultMaxBytes),
		SpanKind:         p.spanKind,
		ParentNode:       p.parentNode,
		TurnIndex:        p.turnIndex,
		ToolName:         p.toolName,
		AgentID:          p.agentID,
	}
}

// ===== 兼容旧接口：RecordNode（仍可用，内部走异步 sink） =====
//
// 业务重构后推荐使用流式 Span API；此处保留以兼容既有调用点与单测。
type NodeSpan struct {
	TraceID          string
	ConversationID   string
	AccountID        string
	Channel          string
	Node             string
	NodeOrder        int
	Direction        string
	MsgID            string
	Input            any
	Output           any
	DurationMs       int64
	Expected         string
	Status           string
	Abnormal         string
	Error            string
	StartedAt        *time.Time
	CompletedAt      *time.Time
	DurationMeasured bool
	TurnID           string
	AnswerAttempt    int
	DeliveryAttempt  int
}

// 节点状态常量（兼容既有调用点 tracing.StatusOk / tracing.StatusAbnormal）。
const (
	StatusOk       = "ok"
	StatusAbnormal = "abnormal"
	StatusFailed   = "failed"
	StatusSkipped  = "skipped"
)

// RecordNodeContext 保留兼容直写的截止时间；异步模式成功仅表示已接收，非已落库。
func RecordNodeContext(ctx context.Context, span NodeSpan) error {
	if ctx == nil || tenant.IsSystemScope(ctx) {
		return fmt.Errorf("controlled tracing requires a tenant context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	tenantID := tenant.WriteTenantID(ctx)
	if tenantID == 0 {
		return fmt.Errorf("controlled tracing requires a confirmed tenant")
	}
	carrier := CarrierFromContext(ctx)
	if carrier != nil && carrier.TenantID != 0 && carrier.TenantID != tenantID {
		return fmt.Errorf("controlled tracing carrier tenant mismatch")
	}
	p := nodeToPending(ctx, span)
	p.tenantID = tenantID
	if spanCh == nil {
		return writeNodeContext(ctx, p)
	}
	// 有界sink沿用原租户投递，绝不为等待容量而阻塞业务。
	atomic.AddInt64(&published, 1)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case spanCh <- p:
	default:
		atomic.AddInt64(&dropped, 1)
		return fmt.Errorf("controlled tracing capacity exhausted")
	}
	return nil
}

func writeNodeContext(ctx context.Context, p pendingSpan) error {
	if ctx == nil || tenant.IsSystemScope(ctx) {
		return fmt.Errorf("controlled tracing requires a tenant context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	tenantID := tenant.WriteTenantID(ctx)
	if tenantID == 0 || p.tenantID != tenantID {
		return fmt.Errorf("controlled tracing tenant mismatch")
	}
	database := db.GetDB()
	if database == nil {
		return gorm.ErrInvalidDB
	}
	err := database.WithContext(tenant.OnlyTenant(ctx, tenantID)).Create(toModelFromPending(p)).Error
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

// RecordNode 保留旧节点写入语义。
func RecordNode(ctx context.Context, span NodeSpan) {
	Publish(nodeToPending(ctx, span))
}

func nodeToPending(ctx context.Context, span NodeSpan) pendingSpan {
	carrier := CarrierFromContext(ctx)
	if carrier == nil {
		carrier = &Carrier{}
	}
	if span.TraceID == "" {
		span.TraceID = carrier.TraceID
	}
	if span.ConversationID == "" {
		span.ConversationID = carrier.ConversationID
	}
	if span.AccountID == "" {
		span.AccountID = carrier.AccountID
	}
	if span.Channel == "" {
		span.Channel = carrier.Channel
	}
	if span.NodeOrder == 0 {
		span.NodeOrder = NodeOrder(span.Node)
	}
	if span.Status == "" {
		span.Status = StatusOk
	}
	if span.TurnID == "" {
		span.TurnID = carrier.TurnID
	}
	if span.AnswerAttempt == 0 {
		span.AnswerAttempt = carrier.AnswerAttempt
	}
	if span.DeliveryAttempt == 0 {
		span.DeliveryAttempt = carrier.DeliveryAttempt
	}
	if span.MsgID == "" {
		span.MsgID = carrier.MsgID
	}
	tenantID, _ := tenant.FromContext(ctx)
	return pendingSpan{
		tenantID:         tenantID,
		carrier:          carrier,
		traceID:          span.TraceID,
		conversationID:   span.ConversationID,
		accountID:        span.AccountID,
		channel:          span.Channel,
		node:             span.Node,
		nodeOrder:        span.NodeOrder,
		direction:        span.Direction,
		msgID:            span.MsgID,
		input:            span.Input,
		output:           span.Output,
		durationMs:       span.DurationMs,
		expected:         span.Expected,
		status:           span.Status,
		abnormal:         span.Abnormal,
		errorStr:         span.Error,
		spanKind:         model.SpanKindLifecycle,
		turnID:           span.TurnID,
		answerAttempt:    span.AnswerAttempt,
		deliveryAttempt:  span.DeliveryAttempt,
		startedAt:        span.StartedAt,
		completedAt:      span.CompletedAt,
		durationMeasured: span.DurationMeasured,
	}
}

// ReportToolCall 由工具层 observer（tooluse.ToolTraceSink）调用，将一次工具/轮次事件转为层级 span。
// 在 router 启动时接线：tooluse.ToolTraceSink = tracing.ReportToolCall。
func ReportToolCall(ctx context.Context, ev ToolTraceEvent) {
	carrier := CarrierFromContext(ctx)
	if carrier == nil {
		carrier = &Carrier{}
	}
	tid := ev.TraceID
	if tid == "" {
		tid = carrier.TraceID
	}
	if tid == "" && ctx != nil {
		tid = trace.TraceIDFromContext(ctx)
	}
	kind := ev.Kind
	if kind == "" {
		kind = model.SpanKindToolCall
	}
	status := ev.Status
	if status == "" {
		if ev.Error != "" {
			status = "abnormal"
		} else {
			status = "ok"
		}
	}
	tenantID, _ := tenant.FromContext(ctx)
	Publish(pendingSpan{
		tenantID:         tenantID,
		carrier:          carrier,
		traceID:          tid,
		conversationID:   carrier.ConversationID,
		accountID:        carrier.AccountID,
		channel:          carrier.Channel,
		node:             kind,
		nodeOrder:        NodeOrder(NodeAIDispatch),
		direction:        "outbound",
		input:            ev.Input,
		output:           ev.Output,
		durationMs:       ev.DurationMs,
		status:           status,
		abnormal:         ev.Error,
		errorStr:         ev.Error,
		parentNode:       NodeAIDispatch,
		spanKind:         kind,
		turnIndex:        ev.TurnIndex,
		toolName:         ev.ToolName,
		agentID:          ev.AgentID,
		msgID:            carrier.MsgID,
		turnID:           carrier.TurnID,
		answerAttempt:    carrier.AnswerAttempt,
		deliveryAttempt:  carrier.DeliveryAttempt,
		startedAt:        ev.StartedAt,
		completedAt:      ev.CompletedAt,
		durationMeasured: ev.DurationMeasured,
	})
}

// RecordDownlinkFetchBatch 兼容没有操作计时的旧调用，不从写库时间反推认领耗时。
func RecordDownlinkFetchBatch(ctx context.Context, channel, accountID string, hubs []*model.MessageHub) {
	RecordDownlinkClaimBatch(ctx, channel, accountID, hubs, nil, nil)
}

// RecordDownlinkClaimBatch 每个认领尝试独立可见；去重不参与业务投递决策。
func RecordDownlinkClaimBatch(
	ctx context.Context,
	channel, accountID string,
	hubs []*model.MessageHub,
	started, completed *time.Time,
) {
	tenantID, _ := tenant.FromContext(ctx)
	if tenantID == 0 {
		if tenant.IsMultiTenant() {
			return
		}
		tenantID = tenant.DefaultTenantID
	}
	ctx = tenant.WithTenantID(dbCtx(ctx), tenantID)
	type claimKey struct {
		tenantID                                  uint
		channel, accountID, conversationID, msgID string
		attempt                                   int
	}
	type observation struct {
		hub     model.MessageHub
		carrier *Carrier
		key     claimKey
	}
	seen := make(map[claimKey]struct{}, len(hubs))
	batch := make([]observation, 0, len(hubs))
	msgIDs := make([]string, 0, len(hubs))
	for _, h := range hubs {
		if h == nil || h.MsgID == "" || (h.TenantID != 0 && h.TenantID != tenantID) {
			continue
		}
		copyHub := *h
		copyHub.TenantID = tenantID
		if copyHub.Platform == "" {
			copyHub.Platform = channel
		}
		if copyHub.AccountID == "" {
			copyHub.AccountID = accountID
		}
		if copyHub.Direction == "" {
			copyHub.Direction = "outbound"
		}
		if copyHub.Platform != channel || copyHub.AccountID != accountID {
			continue
		}
		c := CarrierForHub(ctx, &copyHub)
		if c.TraceID == "" {
			c.TraceID = TraceIDFromContext(ctx)
		}
		key := claimKey{tenantID, channel, accountID, h.ConversationID, h.MsgID, h.PushAttempts}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		batch = append(batch, observation{copyHub, c, key})
		msgIDs = append(msgIDs, h.MsgID)
	}
	if len(batch) == 0 {
		return
	}
	existing := make(map[claimKey]struct{}, len(batch))
	if d := db.GetDB(); d != nil {
		rows := []model.MessageTrace{}
		err := d.WithContext(ctx).Model(&model.MessageTrace{}).
			Select("tenant_id, channel, account_id, conversation_id, msg_id, delivery_attempt").
			Where("tenant_id = ? AND node = ? AND channel = ? AND account_id = ? AND msg_id IN ?",
				tenantID, NodeDownlinkFetch, channel, accountID, msgIDs).
			Find(&rows).Error
		if err != nil {
			logger.Warnf("[tracing] claim observation dedup unavailable: %v", err)
		} else {
			for _, row := range rows {
				existing[claimKey{row.TenantID, row.Channel, row.AccountID, row.ConversationID, row.MsgID, row.DeliveryAttempt}] = struct{}{}
			}
		}
	}
	for _, item := range batch {
		if _, ok := existing[item.key]; ok {
			continue
		}
		h, c := item.hub, item.carrier
		var duration int64
		if started != nil && completed != nil {
			duration = completed.Sub(*started).Milliseconds()
		}
		RecordNode(WithCarrier(ctx, c), NodeSpan{
			TraceID:          c.TraceID,
			ConversationID:   h.ConversationID,
			AccountID:        accountID,
			Channel:          channel,
			Node:             NodeDownlinkFetch,
			NodeOrder:        NodeOrder(NodeDownlinkFetch),
			Direction:        "outbound",
			MsgID:            h.MsgID,
			StartedAt:        started,
			CompletedAt:      completed,
			DurationMs:       duration,
			DurationMeasured: started != nil && completed != nil,
			Output:           map[string]any{"claimed": h.ID != 0 && h.ClaimedAt != nil, "delivery_attempt": h.PushAttempts},
			Expected:         "从桥接出站队列取出待下发消息",
			Status:           "ok",
		})
	}
}

// GenerateTraceID 生成全局唯一 trace_id（带 tr- 前缀，>=20 字符）。
func GenerateTraceID() string {
	return "tr-" + uuid.New().String()
}

// NodeTraceID 由会话 + 账号派生稳定的 inbound trace_id（同一会话多次上报共享）。
func NodeTraceID(conversationID, accountID string) string {
	raw := fmt.Sprintf("%s|%s|inbound", conversationID, accountID)
	sum := sha1Sum(raw)
	return "tr-" + sum
}

// LinkInboundTraceID 入站沿用同会话的 inbound trace_id（派生稳定值），保证同一会话多次上报共享 trace。
func LinkInboundTraceID(ctx context.Context, conversationID string) string {
	id := NodeTraceID(conversationID, "")
	if c := CarrierFromContext(ctx); c != nil {
		c.TraceID = id
	}
	return id
}

// LinkOutboundTraceID 出站复用同会话最近一次 inbound 的 trace_id，关联「客户消息↔AI回复」。
func LinkOutboundTraceID(ctx context.Context, conversationID string) string {
	if conversationID == "" {
		id := GenerateTraceID()
		if c := CarrierFromContext(ctx); c != nil {
			c.TraceID = id
		}
		return id
	}
	if d := db.GetDB(); d != nil {
		var t model.MessageTrace
		err := d.WithContext(dbCtx(ctx)).Model(&model.MessageTrace{}).
			Where("conversation_id = ? AND direction = ? AND node = ?", conversationID, "inbound", NodeIngest).
			Order("id DESC").Limit(1).
			Pluck("trace_id", &t.TraceID).Error
		if err == nil && t.TraceID != "" {
			if c := CarrierFromContext(ctx); c != nil {
				c.TraceID = t.TraceID
			}
			return t.TraceID
		}
	}
	id := NodeTraceID(conversationID, "")
	if c := CarrierFromContext(ctx); c != nil {
		c.TraceID = id
	}
	return id
}

// ErrStr 把 error 转为可存储字符串（nil → ""）。
func ErrStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// StartSpan 轻量计时器（兼容旧用法：t := tracing.StartSpan(); ...; t.ElapsedMs()）。
type SpanTimer struct{ start time.Time }

// StartSpan 开始计时。
func StartSpan() *SpanTimer { return &SpanTimer{start: time.Now()} }

// ElapsedMs 返回自 StartSpan 以来的毫秒数。
func (t *SpanTimer) ElapsedMs() int64 { return time.Since(t.start).Milliseconds() }

// sha1Sum 供 trace_id 稳定派生（非安全用途）：算法沿用既有 trace_id 口径，
// 不用于口令/签名；通过 protocolhash 显式标注"协议/历史口径兼容"用途。
func sha1Sum(s string) string {
	sum := protocolhash.LegacyProtocolSHA1([]byte(s))
	return hex.EncodeToString(sum[:])
}
