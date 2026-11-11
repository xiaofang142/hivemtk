package metrics

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
)

// bridge_metrics 落库 sink。
//
// 分层：本包只做"采样 + 序列化"，落库交给实现了 BridgeMetricWriter 的仓储
// （生产实现是 `repository.BridgeMetricsRepository`），pkg 层不直接持库句柄写表。
//
// 租户口径：bridge_metrics 是指标时间序列，维度是 (metric_name, labels, ts)，
// 没有租户归属语义。写入走**系统域**显式豁免：on 模式下普通 ctx 没有租户绑定，
// 隔离 callback 会 fail-closed，而这条链路既不该被某个租户看到、也不该按租户重复跑。

// BridgeMetricWriter bridge_metrics 的写入口（由仓储实现）。
type BridgeMetricWriter interface {
	WriteBridgeMetrics(ctx context.Context, rows []model.BridgeMetricRow) error
}

// BridgeMetricsSink 把当前指标快照落库到 bridge_metrics 表。
type BridgeMetricsSink struct {
	w BridgeMetricWriter
}

// NewBridgeMetricsSink 创建指标落库 sink
func NewBridgeMetricsSink(w BridgeMetricWriter) *BridgeMetricsSink {
	return &BridgeMetricsSink{w: w}
}

// Flush 把注册表中所有指标当前值追加写入 bridge_metrics 表（一批，追加式时间序列）。
//
// 直方图写两行（count / sum，用 labels.agg 区分）；counter / gauge 各写一行。
func (s *BridgeMetricsSink) Flush() error {
	if s == nil || s.w == nil {
		return nil
	}
	samples := CollectSamples()
	if len(samples) == 0 {
		return nil
	}
	rows := make([]model.BridgeMetricRow, 0, len(samples))
	now := time.Now()
	for _, smp := range samples {
		rows = append(rows, model.BridgeMetricRow{
			MetricName: smp.Name,
			Labels:     labelsToJSON(smp.LabelKeys, smp.Labels, smp.Agg),
			Value:      smp.Value,
			MetricType: smp.Type,
			TS:         now,
		})
	}
	return s.w.WriteBridgeMetrics(tenant.WithSystemScope(context.Background()), rows)
}

func labelsToJSON(keys, values []string, agg string) string {
	m := make(map[string]string, len(keys)+1)
	for i, k := range keys {
		if i < len(values) {
			m[k] = values[i]
		}
	}
	if agg != "" {
		m["agg"] = agg
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// StartBridgeMetricsSink 启动定期落库后台任务（每 interval 一次 Flush）。
// interval <= 0 时使用默认 60s。返回 stop 函数（可安全多次调用）。
// 注意：生产装配里尚未启用（需先确定 bridge_metrics 的保留期/归档策略，否则无界增长）。
func StartBridgeMetricsSink(w BridgeMetricWriter, interval time.Duration) (stop func()) {
	if w == nil {
		return func() {}
	}
	if interval <= 0 {
		interval = 60 * time.Second
	}
	sink := NewBridgeMetricsSink(w)
	done := make(chan struct{})
	var once sync.Once

	// 长驻落库循环：utils.SafeGo 收口 panic，防指标批量写库 panic 击穿进程
	utils.SafeGo(context.Background(), "metrics.bridgeSink", func(_ context.Context) {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				if err := sink.Flush(); err != nil {
					logger.Warnf("[metrics-sink] bridge_metrics 落库失败: %v", err)
				}
			case <-done:
				return
			}
		}
	})

	return func() { once.Do(func() { close(done) }) }
}
