package metrics

import (
	"sync"
)

// BridgeMetrics 桥接指标聚合（懒初始化）
type BridgeMetrics struct {
	IngestTotal         *CounterVec
	IngestErrors        *CounterVec
	IngestDuration      *HistogramVec
	OutboxFetched       *CounterVec
	OutboxDuration      *HistogramVec
	OutboxAcked         *CounterVec
	AckDuration         *HistogramVec
	CircuitBreakerState *GaugeVec
	PendingAckSize      *GaugeVec
	PendingDeadLetters  *GaugeVec
	DLQTotal            *CounterVec
	EmergencyStop       *GaugeVec
	IdempotencyHits     *CounterVec
	PIIRedactions       *CounterVec
	// FrameAckUnknownStatus CH-37b 刀5：WS ack 帧 Status 非法值的 metrics 暴露面。
	// 数值与 channelgw.FrameAckUnknownStatusCount 同源（ws.go 接线处双写）——
	// 原子计数留在 channelgw 是因为打点在帧处理热路径，metrics 包不反向依赖 channelgw。
	FrameAckUnknownStatus *CounterVec
}

var (
	bridgeOnce sync.Once
	bridge     *BridgeMetrics
)

// GetBridge 获取桥接指标（懒初始化，全局单例）
func GetBridge() *BridgeMetrics {
	bridgeOnce.Do(func() {
		bridge = &BridgeMetrics{
			IngestTotal: NewCounter(
				"bridge_ingest_total",
				"Total bridge ingest requests (POST /api/bridge/ingest)",
				[]string{"channel", "agent_id"},
			),
			IngestErrors: NewCounter(
				"bridge_ingest_errors_total",
				"Total bridge ingest errors by error code",
				[]string{"channel", "error_code"},
			),
			IngestDuration: NewHistogram(
				"bridge_ingest_duration_ms",
				"Bridge ingest request duration in ms",
				[]string{"channel"},
				[]float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000},
			),
			OutboxFetched: NewCounter(
				"bridge_outbox_fetched_total",
				"Total outbox messages fetched (GET /api/bridge/outbox)",
				[]string{"channel", "agent_id"},
			),
			OutboxDuration: NewHistogram(
				"bridge_outbox_duration_ms",
				"Bridge outbox fetch request duration in ms",
				[]string{"channel"},
				[]float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000},
			),
			OutboxAcked: NewCounter(
				"bridge_outbox_acked_total",
				"Total outbox ack results by status",
				[]string{"channel", "status"},
			),
			AckDuration: NewHistogram(
				"bridge_ack_duration_ms",
				"Bridge ack request duration in ms",
				[]string{"channel"},
				[]float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000},
			),
			CircuitBreakerState: NewGauge(
				"bridge_circuit_breaker_state",
				"Circuit breaker state (0=CLOSED 1=HALF_OPEN 2=OPEN)",
				[]string{"channel"},
			),
			PendingAckSize: NewGauge(
				"bridge_pending_ack_size",
				"Current pendingAck queue size (waiting for ack)",
				[]string{"channel"},
			),
			PendingDeadLetters: NewGauge(
				"bridge_pending_dead_letters",
				"Current dead letters queue size (gave up retry)",
				[]string{"channel"},
			),
			DLQTotal: NewCounter(
				"bridge_dlq_total",
				"Total messages sent to DLQ (dead letter queue)",
				[]string{"channel", "reason"},
			),
			EmergencyStop: NewGauge(
				"bridge_emergency_stop",
				"Emergency stop status (0=active 1=stopped)",
				[]string{},
			),
			IdempotencyHits: NewCounter(
				"bridge_idempotency_hits_total",
				"Total idempotency hits (deduplication)",
				[]string{"endpoint"},
			),
			PIIRedactions: NewCounter(
				"bridge_pii_redactions_total",
				"Total PII redactions (sanitization)",
				[]string{"field"},
			),
			FrameAckUnknownStatus: NewCounter(
				"bridge_frame_ack_unknown_status_total",
				"Total WS ack frames with invalid Status (neither delivered/failed/unknown nor empty)",
				[]string{"channel"},
			),
		}
	})
	return bridge
}
