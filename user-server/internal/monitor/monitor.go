package monitor

import (
	"context"
	"errors"
	"sort"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tracing"
	"hivemtk-user/internal/repository"
)

// ErrNoDB 表示未初始化数据库句柄（测试/未连接场景）。
var ErrNoDB = errors.New("database not initialized")

// DefaultNodeHealthWindow 节点健康统计窗口的代码兜底（参数中心 telemetry.node_health_window
// 未配置时用）。
const DefaultNodeHealthWindow = 24 * time.Hour

var nodeHealthWindowProvider func() time.Duration

// SetNodeHealthWindowProvider 注入节点健康统计窗口；传 nil 视为不注入。
func SetNodeHealthWindowProvider(fn func() time.Duration) { nodeHealthWindowProvider = fn }

// NodeHealthWindow 生效的统计窗口。非正值一律回落兜底——0 会把 since 推到当下，
// 聚合出的健康率恒为空列表，看上去像"所有渠道都没心跳"。
func NodeHealthWindow() time.Duration {
	if p := nodeHealthWindowProvider; p != nil {
		if d := p(); d > 0 {
			return d
		}
	}
	return DefaultNodeHealthWindow
}

// ───────────────────────────────────────────────────────────────────────────
// 业务生命周期健康概览（区别于系统监控：聚焦核心链路而非 CPU/内存）。
// ───────────────────────────────────────────────────────────────────────────
type HealthOverviewData struct {
	GeneratedAt        time.Time `json:"generated_at"`
	InboundRatePerMin  float64   `json:"inbound_rate_per_min"`
	OutboundRatePerMin float64   `json:"outbound_rate_per_min"`
	PendingCount       int64     `json:"pending_count"`
	OldestPendingMin   int64     `json:"oldest_pending_min"`
	DeliveredCount     int64     `json:"delivered_count"`
	FailedCount        int64     `json:"failed_count"`
	SyncGapCount       int64     `json:"sync_gap_count"`
	StuckReachable     int64     `json:"stuck_reachable"`
	StuckUnreachable   int64     `json:"stuck_unreachable"`
	TotalTraces        int64     `json:"total_traces"`
	AbnormalCount      int64     `json:"abnormal_count"`
	TracePublished     int64     `json:"trace_published"`
	TraceDropped       int64     `json:"trace_dropped"`
}

// HealthOverview 汇总核心业务链路健康指标。
//
// ctx 必须进 GORM：本函数过去收下 ctx 却把它留在原地，所有查询跑在无租户上下文的
// 全局句柄上，saas 形态下每条都被隔离 callback fail-closed，而错误除 sync_gap 一条外
// 全被丢弃 ⇒ /monitor/health 返回 200 + 全零，大盘把「一条都没读出来」显示成「一切正常」。
// 同理，这里的错误一律上抛而不是继续往下攒：一个查不动的库不该交出一版健康的读数。
func HealthOverview(ctx context.Context) (*HealthOverviewData, error) {
	d := db.GetDB()
	if d == nil {
		return nil, ErrNoDB
	}
	d = d.WithContext(ctx)
	h := &HealthOverviewData{GeneratedAt: time.Now()}

	since1h := time.Now().Add(-time.Hour)
	var inCount, outCount int64
	if err := d.Model(&model.MessageHub{}).
		Where("direction = ? AND created_at >= ?", "inbound", since1h).
		Count(&inCount).Error; err != nil {
		return nil, err
	}
	h.InboundRatePerMin = float64(inCount) / 60
	if err := d.Model(&model.MessageHub{}).
		Where("direction = ? AND created_at >= ?", "outbound", since1h).
		Count(&outCount).Error; err != nil {
		return nil, err
	}
	h.OutboundRatePerMin = float64(outCount) / 60

	if err := d.Model(&model.MessageHub{}).
		Where("direction = ? AND status = ?", "outbound", "pending").
		Count(&h.PendingCount).Error; err != nil {
		return nil, err
	}

	var oldest struct {
		SentAt *time.Time
	}
	if err := d.Model(&model.MessageHub{}).
		Where("direction = ? AND status = ?", "outbound", "pending").
		Select("min(sent_at) as sent_at").
		Scan(&oldest).Error; err != nil {
		return nil, err
	}
	if oldest.SentAt != nil {
		h.OldestPendingMin = int64(time.Since(*oldest.SentAt).Minutes())
	}

	if err := d.Model(&model.MessageHub{}).
		Where("direction = ? AND status = ?", "outbound", "delivered").
		Count(&h.DeliveredCount).Error; err != nil {
		return nil, err
	}
	if err := d.Model(&model.MessageHub{}).
		Where("direction = ? AND status = ?", "outbound", "failed").
		Count(&h.FailedCount).Error; err != nil {
		return nil, err
	}

	triad := syncGapTriadExpr()
	missingInbox := d.Select("1").Table("inbox_conversations ic").
		Where("ic.platform = m.platform AND ic.account_id = m.account_id AND ic.customer_id = " + triad)
	if err := d.Table("message_hub m").
		Where("m.created_at > now() - interval '7 days'").
		Where("NOT EXISTS (?)", missingInbox).
		Distinct("m.conversation_id").
		Count(&h.SyncGapCount).Error; err != nil {
		return nil, err
	}

	threshold := time.Now().Add(-15 * time.Minute)
	if err := d.Model(&model.MessageHub{}).
		Where("direction = ? AND status = ? AND sent_at < ?", "outbound", "pending", threshold).
		Count(&h.StuckReachable).Error; err != nil {
		return nil, err
	}
	if err := d.Model(&model.MessageHub{}).
		Where("direction = ? AND status = ?", "outbound", "failed").
		Count(&h.StuckUnreachable).Error; err != nil {
		return nil, err
	}

	if err := d.Model(&model.MessageTrace{}).Count(&h.TotalTraces).Error; err != nil {
		return nil, err
	}
	if err := d.Model(&model.MessageTrace{}).Where("status = ?", "abnormal").
		Count(&h.AbnormalCount).Error; err != nil {
		return nil, err
	}

	pub, drop := tracing.Stats()
	h.TracePublished = pub
	h.TraceDropped = drop
	return h, nil
}

// ───────────────────────────────────────────────────────────────────────────
// 异常项（业务生命周期异常，非系统异常）——结构化分组，供前端分区块渲染。
// ───────────────────────────────────────────────────────────────────────────
type SyncGapRow struct {
	ConversationID string `json:"conversation_id"`
	Channel        string `json:"channel"`
	MessageCount   int64  `json:"message_count"`
}

type StuckRow struct {
	ConversationID string `json:"conversation_id"`
	Channel        string `json:"channel"`
	AgeMin         int64  `json:"age_min"`
}

type NodeAbnormalRow struct {
	Channel      string  `json:"channel"`
	Node         string  `json:"node"`
	AbnormalRate float64 `json:"abnormal_rate"`
}

// AnomalyGroups 按异常类别分组，前端对应 5 个折叠区块。
type AnomalyGroups struct {
	SyncGap          []SyncGapRow      `json:"sync_gap"`
	StuckReachable   []StuckRow        `json:"stuck_reachable"`
	StuckUnreachable []StuckRow        `json:"stuck_unreachable"`
	Unreachable      []StuckRow        `json:"unreachable"`
	NodeAbnormal     []NodeAbnormalRow `json:"node_abnormal"`
}

// Trim 把每个分组截断到 n 条，供 handler 兑现 `?limit=` 入参。
//
// 说明：原先 handler 完全忽略 limit，前端传 20 也照样收到全量（一个卡死会话
// 群就能回几百条），列表页靠前端自行 slice 兜底 —— 契约与实现不一致。
// n <= 0 时不做任何裁剪。
func (g *AnomalyGroups) Trim(n int) {
	if g == nil || n <= 0 {
		return
	}
	if len(g.SyncGap) > n {
		g.SyncGap = g.SyncGap[:n]
	}
	if len(g.StuckReachable) > n {
		g.StuckReachable = g.StuckReachable[:n]
	}
	if len(g.StuckUnreachable) > n {
		g.StuckUnreachable = g.StuckUnreachable[:n]
	}
	if len(g.Unreachable) > n {
		g.Unreachable = g.Unreachable[:n]
	}
	if len(g.NodeAbnormal) > n {
		g.NodeAbnormal = g.NodeAbnormal[:n]
	}
}

// Anomalies 返回当前业务链路异常分组清单。
//
// ctx 进 GORM 的理由与 HealthOverview 同一条，且这里更糟：四类异常逐条 log 后继续，
// 结果是一张「没有异常」的空表——而空表在监控页上就是绿灯。
func Anomalies(ctx context.Context) (*AnomalyGroups, error) {
	d := db.GetDB()
	if d == nil {
		return nil, ErrNoDB
	}
	d = d.WithContext(ctx)
	now := time.Now()
	threshold := now.Add(-15 * time.Minute)
	g := &AnomalyGroups{}

	type gapRow struct {
		ConversationID string
		Channel        string
		MessageCount   int64
	}
	var gaps []gapRow
	triad2 := syncGapTriadExpr()
	missingInbox2 := d.Select("1").Table("inbox_conversations ic").
		Where("ic.platform = m.platform AND ic.account_id = m.account_id AND ic.customer_id = " + triad2)
	if err := d.Table("message_hub m").
		Select("m.conversation_id, "+
			"(SELECT channel FROM message_trace mt WHERE mt.conversation_id = m.conversation_id ORDER BY id DESC LIMIT 1) AS channel, "+
			"count(*) AS message_count").
		Where("m.created_at > now() - interval '7 days'").
		Where("NOT EXISTS (?)", missingInbox2).
		Group("m.conversation_id").
		Scan(&gaps).Error; err != nil {
		return nil, err
	}
	for _, r := range gaps {
		g.SyncGap = append(g.SyncGap, SyncGapRow(r))
	}

	type stuckRow struct {
		ConversationID string
		Channel        string
		AgeMin         float64
	}
	var reach, unreachStuck []stuckRow
	stuckSel := "h.conversation_id, " +
		"(SELECT channel FROM message_trace mt WHERE mt.conversation_id = h.conversation_id ORDER BY id DESC LIMIT 1) AS channel, " +
		"EXTRACT(EPOCH FROM (now() - h.sent_at))/60 AS age_min"
	if err := d.Table("message_hub h").
		Select(stuckSel).
		Joins("JOIN inbox_conversations i ON i.conversation_id = h.conversation_id").
		Where("h.direction = 'outbound' AND h.status = 'pending' AND h.sent_at < ?", threshold).
		Scan(&reach).Error; err != nil {
		return nil, err
	}
	if err := d.Table("message_hub h").
		Select(stuckSel).
		Joins("LEFT JOIN inbox_conversations i ON i.conversation_id = h.conversation_id").
		Where("h.direction = 'outbound' AND h.status = 'pending' AND h.sent_at < ?", threshold).
		Where("i.conversation_id IS NULL").
		Scan(&unreachStuck).Error; err != nil {
		return nil, err
	}
	for _, r := range reach {
		g.StuckReachable = append(g.StuckReachable, StuckRow{
			ConversationID: r.ConversationID, Channel: r.Channel, AgeMin: int64(r.AgeMin),
		})
	}
	for _, r := range unreachStuck {
		g.StuckUnreachable = append(g.StuckUnreachable, StuckRow{
			ConversationID: r.ConversationID, Channel: r.Channel, AgeMin: int64(r.AgeMin),
		})
	}

	var failed []stuckRow
	if err := d.Table("message_hub h").
		Select(stuckSel).
		Where("h.direction = 'outbound' AND h.status = 'failed' AND h.sent_at > now() - interval '7 days'").
		Scan(&failed).Error; err != nil {
		return nil, err
	}
	for _, r := range failed {
		g.Unreachable = append(g.Unreachable, StuckRow{
			ConversationID: r.ConversationID, Channel: r.Channel, AgeMin: int64(r.AgeMin),
		})
	}

	nh, err := NodeHealthByChannel(ctx)
	if err != nil {
		return nil, err
	}
	for _, n := range nh {
		if n.AbnormalRate > 0.05 {
			g.NodeAbnormal = append(g.NodeAbnormal, NodeAbnormalRow{
				Channel: n.Channel, Node: n.Node, AbnormalRate: n.AbnormalRate,
			})
		}
	}
	return g, nil
}

// ───────────────────────────────────────────────────────────────────────────
// 按渠道节点健康：多渠道 + 每节点的响应时间、异常率
// ───────────────────────────────────────────────────────────────────────────
type NodeHealth struct {
	Channel             string    `json:"channel"`
	Node                string    `json:"node"`
	NodeLabel           string    `json:"node_label"`
	Total               int64     `json:"total"`
	Abnormal            int64     `json:"abnormal"`
	AbnormalRate        float64   `json:"abnormal_rate"`
	AvgDurationMs       *int64    `json:"avg_duration_ms"`
	P95DurationMs       *int64    `json:"p95_duration_ms"`
	MeasuredCount       int64     `json:"measured_count"`
	MeasurementCoverage float64   `json:"measurement_coverage"`
	LastAbnormal        time.Time `json:"last_abnormal_at"`
}

func NodeHealthByChannel(ctx context.Context) ([]NodeHealth, error) {
	d := db.GetDB()
	if d == nil {
		return nil, ErrNoDB
	}
	since := time.Now().Add(-NodeHealthWindow())
	rows, err := repository.NewTraceObservationRepository(d).NodeMeasurements(ctx, since)
	if err != nil {
		return nil, err
	}

	out := make([]NodeHealth, 0, len(rows))
	for _, r := range rows {
		nh := NodeHealth{
			Channel:       r.Channel,
			Node:          r.Node,
			NodeLabel:     tracing.NodeLabel(r.Node),
			Total:         r.Total,
			Abnormal:      r.Abnormal,
			AvgDurationMs: r.AvgMs,
			P95DurationMs: r.P95Ms,
			MeasuredCount: r.MeasuredCount,
		}
		if r.Total > 0 {
			nh.AbnormalRate = float64(r.Abnormal) / float64(r.Total)
			nh.MeasurementCoverage = float64(r.MeasuredCount) / float64(r.Total)
		}
		if r.LastAbnormal != nil {
			nh.LastAbnormal = *r.LastAbnormal
		}
		out = append(out, nh)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Channel != out[j].Channel {
			return out[i].Channel < out[j].Channel
		}
		return tracing.NodeOrder(out[i].Node) < tracing.NodeOrder(out[j].Node)
	})
	return out, nil
}

// ───────────────────────────────────────────────────────────────────────────
// 单会话 / 单轮 生命周期视图（核心：节点入参/出参/响应时间/预期/异常 + 节点间时延）
// ───────────────────────────────────────────────────────────────────────────
type LifecycleNode struct {
	TurnID           string     `json:"turn_id"`
	AnswerAttempt    int        `json:"answer_attempt"`
	DeliveryAttempt  int        `json:"delivery_attempt"`
	DurationMeasured bool       `json:"duration_measured"`
	TimingVersion    int        `json:"timing_version"`
	StartedAt        *time.Time `json:"started_at"`
	CompletedAt      *time.Time `json:"completed_at"`
	TraceID          string     `json:"trace_id"`
	ConversationID   string     `json:"conversation_id"`
	Node             string     `json:"node"`
	NodeLabel        string     `json:"node_label"`
	NodeOrder        int        `json:"node_order"`
	Channel          string     `json:"channel"`
	Direction        string     `json:"direction"`
	MsgID            string     `json:"msg_id"`
	Input            string     `json:"input"`
	Output           string     `json:"output"`
	DurationMs       int64      `json:"duration_ms"`
	Expected         string     `json:"expected"`
	Status           string     `json:"status"`
	Abnormal         string     `json:"abnormal"`
	Error            string     `json:"error"`
	CreatedAt        time.Time  `json:"created_at"`
	GapMs            *int64     `json:"gap_ms_from_prev"`
}

type LifecycleData struct {
	TurnID         string          `json:"turn_id"`
	AccountID      string          `json:"account_id"`
	TraceID        string          `json:"trace_id"`
	ConversationID string          `json:"conversation_id"`
	Channel        string          `json:"channel"`
	Nodes          []LifecycleNode `json:"nodes"`
	EndToEndMs     *int64          `json:"end_to_end_ms"`
	HasAbnormal    bool            `json:"has_abnormal"`
	AbnormalDetail string          `json:"abnormal_detail,omitempty"`
}

// Lifecycle 还原某会话（或某轮 trace_id）的完整业务链路。
// 优先按 conversation_id；若提供 traceID 则按轮次过滤。limit 限制返回的最近日程数。
func Lifecycle(ctx context.Context, conversationID, traceID string, limit int) ([]LifecycleData, error) {
	d := db.GetDB()
	if d == nil {
		return nil, ErrNoDB
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	traces, err := repository.NewTraceObservationRepository(d).LifecycleRows(ctx, conversationID, traceID, limit)
	if err != nil {
		return nil, err
	}
	if len(traces) == 0 {
		return []LifecycleData{}, nil
	}

	groups := map[string][]model.MessageTrace{}
	order := []string{}
	for _, t := range traces {
		key := repository.TraceObservationGroupKey(t)
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], t)
	}

	out := make([]LifecycleData, 0, len(order))
	for _, key := range order {
		items := groups[key]
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].NodeOrder != items[j].NodeOrder {
				return items[i].NodeOrder < items[j].NodeOrder
			}
			if items[i].AnswerAttempt != items[j].AnswerAttempt {
				return items[i].AnswerAttempt < items[j].AnswerAttempt
			}
			return items[i].ID < items[j].ID
		})
		lc := LifecycleData{TraceID: items[0].TraceID, TurnID: items[0].TurnID, AccountID: items[0].AccountID, ConversationID: items[0].ConversationID, Channel: items[0].Channel}
		var prev *LifecycleNode
		for _, t := range items {
			ln := LifecycleNode{
				TraceID:          t.TraceID,
				TurnID:           t.TurnID,
				AnswerAttempt:    t.AnswerAttempt,
				DeliveryAttempt:  t.DeliveryAttempt,
				DurationMeasured: t.DurationMeasured,
				TimingVersion:    t.TimingVersion,
				StartedAt:        t.StartedAt,
				CompletedAt:      t.CompletedAt,
				ConversationID:   items[0].ConversationID,
				Node:             t.Node,
				NodeLabel:        tracing.NodeLabel(t.Node),
				NodeOrder:        t.NodeOrder,
				Channel:          t.Channel,
				Direction:        t.Direction,
				MsgID:            t.MsgID,
				Input:            t.Input,
				Output:           t.Output,
				DurationMs:       t.DurationMs,
				Expected:         t.Expected,
				Status:           t.Status,
				Abnormal:         t.Abnormal,
				Error:            t.Error,
				CreatedAt:        t.CreatedAt,
			}
			if prev != nil && prev.CompletedAt != nil && ln.StartedAt != nil && ln.DurationMeasured && prev.DurationMeasured &&
				ln.TimingVersion > 0 && prev.TimingVersion > 0 && ln.AnswerAttempt == prev.AnswerAttempt &&
				ln.DeliveryAttempt == prev.DeliveryAttempt && ln.MsgID != "" && ln.MsgID == prev.MsgID {
				g := ln.StartedAt.Sub(*prev.CompletedAt).Milliseconds()
				if g >= 0 {
					ln.GapMs = &g
				}
			}
			if t.Status == "abnormal" {
				lc.HasAbnormal = true
				if lc.AbnormalDetail == "" {
					lc.AbnormalDetail = tracing.NodeLabel(t.Node) + "：" + t.Abnormal
				}
			}
			lc.Nodes = append(lc.Nodes, ln)
			p := ln
			prev = &p
		}
		lc.EndToEndMs = repository.TraceMeasuredEndToEnd(items)
		out = append(out, lc)
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ───────────────────────────────────────────────────────────────────────────
// 追踪列表（按轮次聚合，便于从总览下钻）
// ───────────────────────────────────────────────────────────────────────────
type TraceSummary struct {
	TurnID         string     `json:"turn_id"`
	AccountID      string     `json:"account_id"`
	TraceID        string     `json:"trace_id"`
	ConversationID string     `json:"conversation_id"`
	Channel        string     `json:"channel"`
	NodeCount      int64      `json:"node_count"`
	AbnormalCount  int64      `json:"abnormal_count"`
	FirstAt        *time.Time `json:"first_at"`
	LastAt         *time.Time `json:"last_at"`
}

// Traces 返回最近的 trace 轮次汇总。
func Traces(ctx context.Context, limit int) ([]TraceSummary, error) {
	d := db.GetDB()
	if d == nil {
		return nil, ErrNoDB
	}
	rows, err := repository.NewTraceObservationRepository(d).Summaries(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]TraceSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, TraceSummary{TraceID: row.TraceID, TurnID: row.TurnID, AccountID: row.AccountID,
			ConversationID: row.ConversationID, Channel: row.Channel, NodeCount: row.NodeCount,
			AbnormalCount: row.AbnormalCount, FirstAt: row.FirstAt, LastAt: row.LastAt})
	}
	return out, nil
}

// TraceTreeData 单条消息 / 单轮对话的完整链路（层级 span 已排序）。
type TraceTreeData struct {
	TurnID         string               `json:"turn_id"`
	TraceID        string               `json:"trace_id"`
	ConversationID string               `json:"conversation_id"`
	Channel        string               `json:"channel"`
	AccountID      string               `json:"account_id"`
	Spans          []model.MessageTrace `json:"spans"`
}

// TraceTree 取一条消息 / 单轮对话的完整链路明细。
//   - traceID 优先；为空时用 msgID 反查其所属 trace；再为空用 conversationID 取最近一轮 trace。
func TraceTree(ctx context.Context, traceID, conversationID, msgID string, scopes ...repository.TraceObservationScope) (*TraceTreeData, error) {
	d := db.GetDB()
	if d == nil {
		return nil, ErrNoDB
	}
	rows, err := repository.NewTraceObservationRepository(d).TraceTreeRows(ctx, traceID, conversationID, msgID, scopes...)
	if err != nil {
		return nil, err
	}
	data := &TraceTreeData{Spans: rows}
	if len(rows) > 0 {
		data.TraceID = rows[0].TraceID
		data.TurnID = rows[0].TurnID
		data.ConversationID = rows[0].ConversationID
		data.Channel = rows[0].Channel
		data.AccountID = rows[0].AccountID
	}
	return data, nil
}

// ───────────────────────────────────────────────────────────────────────────
// 端到端时延（按渠道）：ingest → delivered_ack 的 p50 / p95
// ───────────────────────────────────────────────────────────────────────────
type LifecycleLatency struct {
	Channel             string  `json:"channel"`
	P50Ms               *int64  `json:"p50_ms"`
	P95Ms               *int64  `json:"p95_ms"`
	SampleSize          int64   `json:"sample_size"`
	TotalTurns          int64   `json:"total_turns"`
	MeasurementCoverage float64 `json:"measurement_coverage"`
}

// LifecycleLatencyByChannel 按渠道统计端到端时延（上报接入 → 送达确认）。
func LifecycleLatencyByChannel(ctx context.Context) ([]LifecycleLatency, error) {
	d := db.GetDB()
	if d == nil {
		return nil, ErrNoDB
	}

	rows, err := repository.NewTraceObservationRepository(d).LifecycleMeasurements(ctx, time.Now().Add(-7*24*time.Hour))
	if err != nil {
		return nil, err
	}
	out := make([]LifecycleLatency, 0, len(rows))
	for _, row := range rows {
		item := LifecycleLatency{Channel: row.Channel, SampleSize: int64(len(row.Samples)), TotalTurns: row.TotalTurns}
		if len(row.Samples) > 0 {
			p50, p95 := percentile(row.Samples, 50), percentile(row.Samples, 95)
			item.P50Ms, item.P95Ms = &p50, &p95
		}
		if row.TotalTurns > 0 {
			item.MeasurementCoverage = float64(item.SampleSize) / float64(row.TotalTurns)
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Channel < out[j].Channel })
	return out, nil
}

// ───────────────────────────────────────────────────────────────────────────
// 清理：删除 7 天前的 trace（与 message_hub 保留策略一致）
// ───────────────────────────────────────────────────────────────────────────
func PurgeOld(ctx context.Context, olderThan time.Duration) (int64, error) {
	d := db.GetDB()
	if d == nil {
		return 0, ErrNoDB
	}
	// 绑 ctx：这条删除过去是全实例口径——saas 形态下任一调用方一次调用会清掉**所有租户**
	// 的 trace。当前生产无调用方（见 model-list.md 待决策项），补上钩子前先把它钉在租户内。
	d = d.WithContext(ctx)
	cut := time.Now().Add(-olderThan)
	res := d.Where("created_at < ?", cut).Delete(&model.MessageTrace{})
	return res.RowsAffected, res.Error
}

func percentile(sorted []int64, p int) int64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := (p * len(sorted)) / 100
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// syncGapTriadExpr 会话归属三元表达式（monitor 与 repository 各有一份同构 SQL，保持字面一致）
func syncGapTriadExpr() string {
	return "(CASE" +
		" WHEN m.is_group AND m.conversation_id <> '' THEN m.conversation_id" +
		" WHEN m.conversation_id <> '' AND (m.sender_id LIKE (m.conversation_id || ' %') OR m.receiver_id LIKE (m.conversation_id || ' %')) THEN m.conversation_id" +
		" ELSE (CASE WHEN m.direction = 'inbound' THEN m.sender_id ELSE m.receiver_id END)" +
		" END)"
}
