package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/tracing"

	"gorm.io/gorm"
)

// TraceObservationRepository 收口测量查询，未测值不进入耗时样本。
type TraceObservationRepository struct{ database *gorm.DB }

func NewTraceObservationRepository(database *gorm.DB) *TraceObservationRepository {
	return &TraceObservationRepository{database: database}
}

func (r *TraceObservationRepository) scoped(ctx context.Context) (*gorm.DB, error) {
	if r == nil || r.database == nil {
		return nil, gorm.ErrInvalidDB
	}
	if ctx == nil || tenant.IsSystemScope(ctx) {
		return nil, errors.New("trace observation requires tenant scope")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id := tenant.WriteTenantID(ctx)
	if id == 0 {
		return nil, errors.New("trace observation requires a confirmed tenant")
	}
	return r.database.WithContext(tenant.OnlyTenant(ctx, id)).Model(&model.MessageTrace{}).
		Where("tenant_id = ?", id), nil
}

type TraceNodeMeasurement struct {
	Channel       string
	Node          string
	Total         int64
	Abnormal      int64
	MeasuredCount int64
	AvgMs         *int64
	P95Ms         *int64
	LastAbnormal  *time.Time
}

func (r *TraceObservationRepository) NodeMeasurements(ctx context.Context, since time.Time) ([]TraceNodeMeasurement, error) {
	q, err := r.scoped(ctx)
	if err != nil {
		return nil, err
	}
	// 显式实测且非负才参与分位数，历史节点仍进入总数与覆盖率分母。
	const measured = "duration_measured AND timing_version > 0 AND duration_ms >= 0"
	type aggregate struct {
		Channel       string
		Node          string
		Total         int64
		Abnormal      int64
		MeasuredCount int64
		AvgMs         *float64
		P95Ms         *float64
		LastAbnormal  *time.Time
	}
	rows := []aggregate{}
	err = q.Select("channel, node, count(*) AS total, "+
		"count(*) FILTER (WHERE status = 'abnormal') AS abnormal, "+
		"count(*) FILTER (WHERE "+measured+") AS measured_count, "+
		"AVG(duration_ms) FILTER (WHERE "+measured+") AS avg_ms, "+
		"percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms) FILTER (WHERE "+measured+") AS p95_ms, "+
		"MAX(created_at) FILTER (WHERE status = 'abnormal') AS last_abnormal").
		Where("created_at >= ?", since).Group("channel, node").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]TraceNodeMeasurement, 0, len(rows))
	for _, row := range rows {
		item := TraceNodeMeasurement{Channel: row.Channel, Node: row.Node, Total: row.Total, Abnormal: row.Abnormal, MeasuredCount: row.MeasuredCount, LastAbnormal: row.LastAbnormal}
		if row.AvgMs != nil {
			v := int64(*row.AvgMs)
			item.AvgMs = &v
		}
		if row.P95Ms != nil {
			v := int64(*row.P95Ms)
			item.P95Ms = &v
		}
		out = append(out, item)
	}
	return out, nil
}

// TraceObservationGroupKey 轮次只能在完整作用域中关联；历史记录按范围保留，但不推算耗时。
func TraceObservationGroupKey(row model.MessageTrace) string {
	identity := row.TurnID
	if identity == "" {
		identity = "legacy:" + row.TraceID
	}
	data, _ := json.Marshal([]any{row.TenantID, row.Channel, row.AccountID, row.ConversationID, identity})
	return string(data)
}

func TraceHasOperationTime(row model.MessageTrace) bool {
	return row.TimingVersion > 0 && row.DurationMeasured && row.StartedAt != nil && row.CompletedAt != nil &&
		row.DurationMs >= 0 && !row.CompletedAt.Before(*row.StartedAt)
}

func TraceIsDeliveredTransition(row model.MessageTrace) bool {
	if row.Node != tracing.NodeDeliveredAck || row.Status != tracing.StatusOk || row.MsgID == "" || !TraceHasOperationTime(row) {
		return false
	}
	var output struct {
		Status  string `json:"status"`
		Applied bool   `json:"transition_applied"`
	}
	return json.Unmarshal([]byte(row.Output), &output) == nil && output.Status == "delivered" && output.Applied
}

// TraceMeasuredEndToEnd 只取同轮首次实际ACK，重复回执/unknown/failed不能覆盖成功时点。
// 返回服务端接入操作开始到ACK处理完成，不等同平台已读或客户端采集耗时。
func TraceMeasuredEndToEnd(rows []model.MessageTrace) *int64 {
	var group string
	var started, completed *time.Time
	for _, row := range rows {
		if row.TurnID == "" || row.TenantID == 0 || row.Channel == "" || row.AccountID == "" || row.ConversationID == "" {
			return nil
		}
		key := TraceObservationGroupKey(row)
		if group == "" {
			group = key
		} else if key != group {
			return nil
		}
		if row.Node == tracing.NodeIngest && row.Direction != "outbound" && row.Status == tracing.StatusOk && TraceHasOperationTime(row) {
			if started == nil || row.StartedAt.Before(*started) {
				started = row.StartedAt
			}
		}
		if TraceIsDeliveredTransition(row) {
			if completed == nil || row.CompletedAt.Before(*completed) {
				completed = row.CompletedAt
			}
		}
	}
	if started == nil || completed == nil || completed.Before(*started) {
		return nil
	}
	v := completed.Sub(*started).Milliseconds()
	return &v
}

type TraceLifecycleMeasurement struct {
	Channel    string
	Samples    []int64
	TotalTurns int64
}

func (r *TraceObservationRepository) LifecycleMeasurements(ctx context.Context, since time.Time) ([]TraceLifecycleMeasurement, error) {
	q, err := r.scoped(ctx)
	if err != nil {
		return nil, err
	}
	rows := []model.MessageTrace{}
	// 只载入配对需要的元数据，不读输入正文/回复全文，不做会话trace的多对多JOIN。
	err = q.Select("tenant_id, trace_id, channel, account_id, conversation_id, turn_id, node, direction, status, msg_id, started_at, completed_at, duration_ms, duration_measured, timing_version, output").
		Where("created_at >= ? AND node IN ?", since, []string{tracing.NodeIngest, tracing.NodeDeliveredAck}).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	groups := make(map[string][]model.MessageTrace)
	for _, row := range rows {
		key := TraceObservationGroupKey(row)
		groups[key] = append(groups[key], row)
	}
	channels := make(map[string]*TraceLifecycleMeasurement)
	for _, group := range groups {
		hasIngest := false
		for _, row := range group {
			if row.Node == tracing.NodeIngest {
				hasIngest = true
				break
			}
		}
		if !hasIngest {
			continue
		}
		channel := group[0].Channel
		if channels[channel] == nil {
			channels[channel] = &TraceLifecycleMeasurement{Channel: channel, Samples: []int64{}}
		}
		channels[channel].TotalTurns++
		if duration := TraceMeasuredEndToEnd(group); duration != nil {
			channels[channel].Samples = append(channels[channel].Samples, *duration)
		}
	}
	out := make([]TraceLifecycleMeasurement, 0, len(channels))
	for _, row := range channels {
		sort.Slice(row.Samples, func(i, j int) bool { return row.Samples[i] < row.Samples[j] })
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Channel < out[j].Channel })
	return out, nil
}

func (r *TraceObservationRepository) LifecycleRows(ctx context.Context, conversationID, traceID string, limit int) ([]model.MessageTrace, error) {
	q, err := r.scoped(ctx)
	if err != nil {
		return nil, err
	}
	if conversationID == "" && traceID == "" {
		return nil, fmt.Errorf("conversation_id or trace_id required")
	}
	if conversationID != "" {
		q = q.Where("conversation_id = ?", conversationID)
	}
	if traceID != "" {
		q = q.Where("trace_id = ?", traceID)
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows := []model.MessageTrace{}
	err = q.Order("id DESC").Limit(limit * 12).Find(&rows).Error
	return rows, err
}

type TraceObservationScope struct {
	TurnID    string
	Channel   string
	AccountID string
}

// TraceTreeRows 先以消息或最近记录定位同轮和完整范围，再读该轮，不把会话trace当轮次。
func (r *TraceObservationRepository) TraceTreeRows(ctx context.Context, traceID, conversationID, msgID string, scopes ...TraceObservationScope) ([]model.MessageTrace, error) {
	q, err := r.scoped(ctx)
	if err != nil {
		return nil, err
	}
	scope := TraceObservationScope{}
	if len(scopes) > 0 {
		scope = scopes[0]
	}
	if traceID == "" && conversationID == "" && msgID == "" && scope.TurnID == "" {
		return []model.MessageTrace{}, nil
	}
	if traceID != "" {
		q = q.Where("trace_id = ?", traceID)
	}
	if conversationID != "" {
		q = q.Where("conversation_id = ?", conversationID)
	}
	if msgID != "" {
		q = q.Where("msg_id = ?", msgID)
	}
	if scope.TurnID != "" {
		q = q.Where("turn_id = ?", scope.TurnID)
	}
	if scope.Channel != "" {
		q = q.Where("channel = ?", scope.Channel)
	}
	if scope.AccountID != "" {
		q = q.Where("account_id = ?", scope.AccountID)
	}
	var anchor model.MessageTrace
	if err := q.Order("id DESC").First(&anchor).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return []model.MessageTrace{}, nil
		}
		return nil, err
	}
	q, err = r.scoped(ctx)
	if err != nil {
		return nil, err
	}
	q = q.Where("tenant_id = ? AND channel = ? AND account_id = ? AND conversation_id = ?", anchor.TenantID, anchor.Channel, anchor.AccountID, anchor.ConversationID)
	if anchor.TurnID != "" {
		q = q.Where("turn_id = ?", anchor.TurnID)
	} else {
		q = q.Where("trace_id = ? AND turn_id = ''", anchor.TraceID)
	}
	rows := []model.MessageTrace{}
	err = q.Order("node_order, answer_attempt, turn_index, delivery_attempt, id").Find(&rows).Error
	return rows, err
}

type TraceObservationSummary struct {
	TraceID        string
	TurnID         string
	ConversationID string
	AccountID      string
	Channel        string
	NodeCount      int64
	AbnormalCount  int64
	FirstAt        *time.Time
	LastAt         *time.Time
}

// Summaries 聚合完整范围+轮次；历史trace仅在无turn时作为兼容分组，不读取正文。
func (r *TraceObservationRepository) Summaries(ctx context.Context, limit int) ([]TraceObservationSummary, error) {
	q, err := r.scoped(ctx)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows := []TraceObservationSummary{}
	const operation = "duration_measured AND timing_version > 0 AND duration_ms >= 0 AND started_at IS NOT NULL AND completed_at >= started_at"
	err = q.Select("MIN(trace_id) AS trace_id, turn_id, conversation_id, account_id, channel, count(*) AS node_count, " +
		"count(*) FILTER (WHERE status = 'abnormal') AS abnormal_count, " +
		"MIN(started_at) FILTER (WHERE " + operation + ") AS first_at, " +
		"MAX(completed_at) FILTER (WHERE " + operation + ") AS last_at").
		Group("tenant_id, channel, account_id, conversation_id, turn_id, CASE WHEN turn_id = '' THEN trace_id ELSE '' END").
		Order("MAX(id) DESC").Limit(limit).Scan(&rows).Error
	return rows, err
}
