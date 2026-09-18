package monitor

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"sort"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tracing"
)

// ErrNoDB 表示未初始化数据库句柄（测试/未连接场景）。
var ErrNoDB = errors.New("database not initialized")

const nodeHealthWindow = 24 * time.Hour

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
func HealthOverview(ctx context.Context) (*HealthOverviewData, error) {
	d := db.GetDB()
	if d == nil {
		return nil, ErrNoDB
	}
	h := &HealthOverviewData{GeneratedAt: time.Now()}

	since1h := time.Now().Add(-time.Hour)
	var inCount, outCount int64
	d.Model(&model.MessageHub{}).
		Where("direction = ? AND created_at >= ?", "inbound", since1h).
		Count(&inCount)
	h.InboundRatePerMin = float64(inCount) / 60
	d.Model(&model.MessageHub{}).
		Where("direction = ? AND created_at >= ?", "outbound", since1h).
		Count(&outCount)
	h.OutboundRatePerMin = float64(outCount) / 60

	d.Model(&model.MessageHub{}).
		Where("direction = ? AND status = ?", "outbound", "pending").
		Count(&h.PendingCount)

	var oldest struct {
		SentAt *time.Time
	}
	d.Model(&model.MessageHub{}).
		Where("direction = ? AND status = ?", "outbound", "pending").
		Select("min(sent_at) as sent_at").
		Scan(&oldest)
	if oldest.SentAt != nil {
		h.OldestPendingMin = int64(time.Since(*oldest.SentAt).Minutes())
	}

	d.Model(&model.MessageHub{}).
		Where("direction = ? AND status = ?", "outbound", "delivered").
		Count(&h.DeliveredCount)
	d.Model(&model.MessageHub{}).
		Where("direction = ? AND status = ?", "outbound", "failed").
		Count(&h.FailedCount)

	if err := d.Raw(`
		SELECT count(DISTINCT m.conversation_id)
		FROM message_hub m
		WHERE m.created_at > now() - interval '7 days'
		  AND NOT EXISTS (
			SELECT 1 FROM inbox_conversations ic
			WHERE ic.platform = m.platform
			  AND ic.account_id = m.account_id
			  AND ic.customer_id = CASE
				WHEN m.is_group AND m.conversation_id <> '' THEN m.conversation_id
				WHEN m.conversation_id <> '' AND (m.sender_id LIKE (m.conversation_id || ' %') OR m.receiver_id LIKE (m.conversation_id || ' %')) THEN m.conversation_id
				ELSE (CASE WHEN m.direction = 'inbound' THEN m.sender_id ELSE m.receiver_id END)
			  END
		  )
	`).Scan(&h.SyncGapCount).Error; err != nil {
		log.Printf("[monitor] HealthOverview sync_gap query failed: %v", err)
	}

	threshold := time.Now().Add(-15 * time.Minute)
	d.Model(&model.MessageHub{}).
		Where("direction = ? AND status = ? AND sent_at < ?", "outbound", "pending", threshold).
		Count(&h.StuckReachable)
	d.Model(&model.MessageHub{}).
		Where("direction = ? AND status = ?", "outbound", "failed").
		Count(&h.StuckUnreachable)

	d.Model(&model.MessageTrace{}).Count(&h.TotalTraces)
	d.Model(&model.MessageTrace{}).Where("status = ?", "abnormal").Count(&h.AbnormalCount)

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

// Anomalies 返回当前业务链路异常分组清单。
func Anomalies(ctx context.Context) (*AnomalyGroups, error) {
	d := db.GetDB()
	if d == nil {
		return nil, ErrNoDB
	}
	now := time.Now()
	threshold := now.Add(-15 * time.Minute)
	g := &AnomalyGroups{}

	type gapRow struct {
		ConversationID string
		Channel        string
		MessageCount   int64
	}
	var gaps []gapRow
	if err := d.Raw(`
		SELECT m.conversation_id,
		       (SELECT channel FROM message_trace mt WHERE mt.conversation_id = m.conversation_id ORDER BY id DESC LIMIT 1) AS channel,
		       count(*) AS message_count
		FROM message_hub m
		WHERE m.created_at > now() - interval '7 days'
		  AND NOT EXISTS (
			SELECT 1 FROM inbox_conversations ic
			WHERE ic.platform = m.platform
			  AND ic.account_id = m.account_id
		  AND ic.customer_id = CASE
			WHEN m.is_group AND m.conversation_id <> '' THEN m.conversation_id
			WHEN m.conversation_id <> '' AND (m.sender_id LIKE (m.conversation_id || ' %') OR m.receiver_id LIKE (m.conversation_id || ' %')) THEN m.conversation_id
			ELSE (CASE WHEN m.direction = 'inbound' THEN m.sender_id ELSE m.receiver_id END)
		  END
		  )
		GROUP BY m.conversation_id
	`).Scan(&gaps).Error; err != nil {
		log.Printf("monitor.Anomalies sync_gap: %v", err)
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
	if err := d.Raw(`
		SELECT h.conversation_id,
		       (SELECT channel FROM message_trace mt WHERE mt.conversation_id = h.conversation_id ORDER BY id DESC LIMIT 1) AS channel,
		       EXTRACT(EPOCH FROM (now() - h.sent_at))/60 AS age_min
		FROM message_hub h
		JOIN inbox_conversations i ON i.conversation_id = h.conversation_id
		WHERE h.direction = 'outbound' AND h.status = 'pending' AND h.sent_at < ?
	`, threshold).Scan(&reach).Error; err != nil {
		log.Printf("monitor.Anomalies stuck_reachable: %v", err)
	}
	if err := d.Raw(`
		SELECT h.conversation_id,
		       (SELECT channel FROM message_trace mt WHERE mt.conversation_id = h.conversation_id ORDER BY id DESC LIMIT 1) AS channel,
		       EXTRACT(EPOCH FROM (now() - h.sent_at))/60 AS age_min
		FROM message_hub h
		LEFT JOIN inbox_conversations i ON i.conversation_id = h.conversation_id
		WHERE h.direction = 'outbound' AND h.status = 'pending' AND h.sent_at < ?
		  AND i.conversation_id IS NULL
	`, threshold).Scan(&unreachStuck).Error; err != nil {
		log.Printf("monitor.Anomalies stuck_unreachable: %v", err)
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
	if err := d.Raw(`
		SELECT h.conversation_id,
		       (SELECT channel FROM message_trace mt WHERE mt.conversation_id = h.conversation_id ORDER BY id DESC LIMIT 1) AS channel,
		       EXTRACT(EPOCH FROM (now() - h.sent_at))/60 AS age_min
		FROM message_hub h
		WHERE h.direction = 'outbound' AND h.status = 'failed'
		  AND h.sent_at > now() - interval '7 days'
	`).Scan(&failed).Error; err != nil {
		log.Printf("monitor.Anomalies unreachable: %v", err)
	}
	for _, r := range failed {
		g.Unreachable = append(g.Unreachable, StuckRow{
			ConversationID: r.ConversationID, Channel: r.Channel, AgeMin: int64(r.AgeMin),
		})
	}

	nh, err := NodeHealthByChannel(ctx)
	if err == nil {
		for _, n := range nh {
			if n.AbnormalRate > 0.05 {
				g.NodeAbnormal = append(g.NodeAbnormal, NodeAbnormalRow{
					Channel: n.Channel, Node: n.Node, AbnormalRate: n.AbnormalRate,
				})
			}
		}
	}
	return g, nil
}

// ───────────────────────────────────────────────────────────────────────────
// 按渠道节点健康：多渠道 + 每节点的响应时间、异常率
// ───────────────────────────────────────────────────────────────────────────
type NodeHealth struct {
	Channel       string    `json:"channel"`
	Node          string    `json:"node"`
	NodeLabel     string    `json:"node_label"`
	Total         int64     `json:"total"`
	Abnormal      int64     `json:"abnormal"`
	AbnormalRate  float64   `json:"abnormal_rate"`
	AvgDurationMs int64     `json:"avg_duration_ms"`
	P95DurationMs int64     `json:"p95_duration_ms"`
	LastAbnormal  time.Time `json:"last_abnormal_at"`
}

func NodeHealthByChannel(ctx context.Context) ([]NodeHealth, error) {
	d := db.GetDB()
	if d == nil {
		return nil, ErrNoDB
	}
	since := time.Now().Add(-nodeHealthWindow)

	type aggRow struct {
		Channel      string
		Node         string
		Total        int64
		Abnormal     int64
		AvgMs        float64
		P95Ms        float64
		LastAbnormal sql.NullTime
	}
	var rows []aggRow
	if err := d.Raw(`
		SELECT channel,
		       node,
		       count(*) AS total,
		       COALESCE(SUM(CASE WHEN status='abnormal' THEN 1 ELSE 0 END),0) AS abnormal,
		       COALESCE(AVG(duration_ms),0) AS avg_ms,
		       COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms),0) AS p95_ms,
		       MAX(created_at) FILTER (WHERE status='abnormal') AS last_abnormal
		FROM message_trace
		WHERE created_at >= ?
		GROUP BY channel, node
	`, since).Scan(&rows).Error; err != nil {
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
			AvgDurationMs: int64(r.AvgMs),
			P95DurationMs: int64(r.P95Ms),
		}
		if r.Total > 0 {
			nh.AbnormalRate = float64(r.Abnormal) / float64(r.Total)
		}
		if r.LastAbnormal.Valid {
			nh.LastAbnormal = r.LastAbnormal.Time
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
	TraceID        string    `json:"trace_id"`
	ConversationID string    `json:"conversation_id"`
	Node           string    `json:"node"`
	NodeLabel      string    `json:"node_label"`
	NodeOrder      int       `json:"node_order"`
	Channel        string    `json:"channel"`
	Direction      string    `json:"direction"`
	MsgID          string    `json:"msg_id"`
	Input          string    `json:"input"`
	Output         string    `json:"output"`
	DurationMs     int64     `json:"duration_ms"`
	Expected       string    `json:"expected"`
	Status         string    `json:"status"`
	Abnormal       string    `json:"abnormal"`
	Error          string    `json:"error"`
	CreatedAt      time.Time `json:"created_at"`
	GapMs          *int64    `json:"gap_ms_from_prev"`
}

type LifecycleData struct {
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
	q := d.Model(&model.MessageTrace{})
	if conversationID != "" {
		q = q.Where("conversation_id = ?", conversationID)
	} else if traceID != "" {
		q = q.Where("trace_id = ?", traceID)
	} else {
		return nil, errors.New("conversation_id 或 trace_id 必填其一")
	}
	var traces []model.MessageTrace
	if err := q.Order("trace_id, node_order, id").
		Limit(limit * 12).
		Find(&traces).Error; err != nil {
		return nil, err
	}
	if len(traces) == 0 {
		return []LifecycleData{}, nil
	}

	groups := map[string][]model.MessageTrace{}
	order := []string{}
	for _, t := range traces {
		if _, ok := groups[t.TraceID]; !ok {
			order = append(order, t.TraceID)
		}
		groups[t.TraceID] = append(groups[t.TraceID], t)
	}

	out := make([]LifecycleData, 0, len(order))
	for _, tid := range order {
		items := groups[tid]
		lc := LifecycleData{TraceID: tid, ConversationID: items[0].ConversationID, Channel: items[0].Channel}
		var prev *LifecycleNode
		var ingestAt, deliveredAt *time.Time
		for _, t := range items {
			ln := LifecycleNode{
				TraceID:        tid,
				ConversationID: items[0].ConversationID,
				Node:           t.Node,
				NodeLabel:      tracing.NodeLabel(t.Node),
				NodeOrder:      t.NodeOrder,
				Channel:        t.Channel,
				Direction:      t.Direction,
				MsgID:          t.MsgID,
				Input:          t.Input,
				Output:         t.Output,
				DurationMs:     t.DurationMs,
				Expected:       t.Expected,
				Status:         t.Status,
				Abnormal:       t.Abnormal,
				Error:          t.Error,
				CreatedAt:      t.CreatedAt,
			}
			if prev != nil {
				g := t.CreatedAt.Sub(prev.CreatedAt).Milliseconds()
				ln.GapMs = &g
			}
			if t.Status == "abnormal" {
				lc.HasAbnormal = true
				if lc.AbnormalDetail == "" {
					lc.AbnormalDetail = tracing.NodeLabel(t.Node) + "：" + t.Abnormal
				}
			}
			if t.Node == "ingest" {
				at := t.CreatedAt
				ingestAt = &at
			}
			if t.Node == "delivered_ack" {
				at := t.CreatedAt
				deliveredAt = &at
			}
			lc.Nodes = append(lc.Nodes, ln)
			p := ln
			prev = &p
		}
		if ingestAt != nil && deliveredAt != nil {
			e2e := deliveredAt.Sub(*ingestAt).Milliseconds()
			lc.EndToEndMs = &e2e
		}
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
	TraceID        string    `json:"trace_id"`
	ConversationID string    `json:"conversation_id"`
	Channel        string    `json:"channel"`
	NodeCount      int64     `json:"node_count"`
	AbnormalCount  int64     `json:"abnormal_count"`
	FirstAt        time.Time `json:"first_at"`
	LastAt         time.Time `json:"last_at"`
}

// Traces 返回最近的 trace 轮次汇总。
func Traces(ctx context.Context, limit int) ([]TraceSummary, error) {
	d := db.GetDB()
	if d == nil {
		return nil, ErrNoDB
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	var recent []string
	if err := d.Model(&model.MessageTrace{}).
		Select("trace_id").
		Group("trace_id").
		Order("max(id) DESC").
		Limit(limit).
		Pluck("trace_id", &recent).Error; err != nil {
		return nil, err
	}
	if len(recent) == 0 {
		return []TraceSummary{}, nil
	}

	var rows []model.MessageTrace
	if err := d.Where("trace_id IN ?", recent).
		Order("trace_id, node_order, id").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	groups := map[string][]model.MessageTrace{}
	for _, t := range rows {
		groups[t.TraceID] = append(groups[t.TraceID], t)
	}
	out := make([]TraceSummary, 0, len(recent))
	for _, tid := range recent {
		rs := groups[tid]
		if len(rs) == 0 {
			continue
		}
		ts := TraceSummary{
			TraceID:        tid,
			ConversationID: rs[0].ConversationID,
			Channel:        rs[0].Channel,
			FirstAt:        rs[0].CreatedAt,
			LastAt:         rs[len(rs)-1].CreatedAt,
			NodeCount:      int64(len(rs)),
		}
		for _, r := range rs {
			if r.Status == "abnormal" {
				ts.AbnormalCount++
			}
		}
		out = append(out, ts)
	}
	return out, nil
}

// TraceTreeData 单条消息 / 单轮对话的完整链路（层级 span 已排序）。
type TraceTreeData struct {
	TraceID        string               `json:"trace_id"`
	ConversationID string               `json:"conversation_id"`
	Channel        string               `json:"channel"`
	AccountID      string               `json:"account_id"`
	Spans          []model.MessageTrace `json:"spans"`
}

// TraceTree 取一条消息 / 单轮对话的完整链路明细。
//   - traceID 优先；为空时用 msgID 反查其所属 trace；再为空用 conversationID 取最近一轮 trace。
func TraceTree(ctx context.Context, traceID, conversationID, msgID string) (*TraceTreeData, error) {
	d := db.GetDB()
	if d == nil {
		return nil, ErrNoDB
	}
	var tid string
	switch {
	case traceID != "":
		tid = traceID
	case msgID != "":
		var ids []string
		if err := d.Model(&model.MessageTrace{}).
			Where("msg_id = ?", msgID).
			Order("id DESC").Limit(1).
			Pluck("trace_id", &ids).Error; err != nil {
			return nil, err
		}
		if len(ids) > 0 {
			tid = ids[0]
		}
	default:
		if conversationID != "" {
			var ids []string
			if err := d.Model(&model.MessageTrace{}).
				Where("conversation_id = ?", conversationID).
				Order("id DESC").Limit(1).
				Pluck("trace_id", &ids).Error; err != nil {
				return nil, err
			}
			if len(ids) > 0 {
				tid = ids[0]
			}
		}
	}
	if tid == "" {
		return &TraceTreeData{}, nil
	}
	var rows []model.MessageTrace
	if err := d.Where("trace_id = ?", tid).
		Order("node_order ASC, turn_index ASC, id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	data := &TraceTreeData{TraceID: tid, Spans: rows}
	if len(rows) > 0 {
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
	Channel    string `json:"channel"`
	P50Ms      int64  `json:"p50_ms"`
	P95Ms      int64  `json:"p95_ms"`
	SampleSize int64  `json:"sample_size"`
}

// LifecycleLatencyByChannel 按渠道统计端到端时延（上报接入 → 送达确认）。
func LifecycleLatencyByChannel(ctx context.Context) ([]LifecycleLatency, error) {
	d := db.GetDB()
	if d == nil {
		return nil, ErrNoDB
	}

	type pair struct {
		Channel  string
		IngestAt time.Time
		DelivAt  time.Time
	}
	var pairs []pair
	if err := d.Raw(`
		SELECT i.channel, i.created_at AS ingest_at, o.created_at AS deliv_at
		FROM message_trace i
		JOIN message_trace o
		  ON o.trace_id = i.trace_id AND o.node = 'delivered_ack'
		WHERE i.node = 'ingest'
		  AND i.created_at > now() - interval '7 days'
		  AND o.created_at > i.created_at
	`).Scan(&pairs).Error; err != nil {
		return nil, err
	}
	byCh := map[string][]int64{}
	for _, p := range pairs {
		lat := p.DelivAt.Sub(p.IngestAt).Milliseconds()
		if lat < 0 {
			continue
		}
		byCh[p.Channel] = append(byCh[p.Channel], lat)
	}
	out := make([]LifecycleLatency, 0, len(byCh))
	for ch, lats := range byCh {
		sort.Slice(lats, func(i, j int) bool { return lats[i] < lats[j] })
		out = append(out, LifecycleLatency{
			Channel:    ch,
			P50Ms:      percentile(lats, 50),
			P95Ms:      percentile(lats, 95),
			SampleSize: int64(len(lats)),
		})
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
