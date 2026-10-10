package portcontract

import "context"

// LogisticsTrackStatus 物流轨迹节点状态
type LogisticsTrackStatus string

// LogisticsTrackView 单条物流轨迹节点（最新在前）
type LogisticsTrackView struct {
	Time        string `json:"time"`
	Status      string `json:"status"`
	Location    string `json:"location"`
	Description string `json:"description"`
}

// LogisticsTrackRequest 物流轨迹查询请求
type LogisticsTrackRequest struct {
	Platform   string
	OrderID    string
	TrackingNo string
	Carrier    string
}

// LogisticsTrackResult 物流轨迹查询结果（聚合本地状态 + 实时轨迹）
type LogisticsTrackResult struct {
	Found       bool                  `json:"found"`
	Realtime    bool                  `json:"realtime"`
	Configured  bool                  `json:"configured"`
	OrderStatus string                `json:"order_status"`
	ShipTime    string                `json:"ship_time"`
	TrackingNo  string                `json:"tracking_no"`
	Carrier     string                `json:"carrier"`
	Tracks      []*LogisticsTrackView `json:"tracks"`
	Notice      string                `json:"notice"`
}

// CourierClient 快递轨迹外部查询客户端（由具体承运商/聚合平台实现）
type CourierClient interface {
	Query(ctx context.Context, carrier, trackingNo string) ([]*LogisticsTrackView, error)
}

// LogisticsPort 物流查询端口（工具层依赖此端口，不关心具体实现）
type LogisticsPort interface {
	Track(ctx context.Context, req *LogisticsTrackRequest) (*LogisticsTrackResult, error)
}

// NoopLogisticsPort 空端口：永远返回“未配置”的兜底结果。
// 当业务工具未注入真实 LogisticsPort 时使用，保证工具可用而非崩溃。
type NoopLogisticsPort struct{}

// NewNoopLogisticsPort 构造空物流端口
func NewNoopLogisticsPort() *NoopLogisticsPort { return &NoopLogisticsPort{} }

// Track 空实现
func (p *NoopLogisticsPort) Track(_ context.Context, req *LogisticsTrackRequest) (*LogisticsTrackResult, error) {
	return &LogisticsTrackResult{
		TrackingNo: req.TrackingNo,
		Carrier:    req.Carrier,
		Configured: false,
		Notice:     "物流查询端口未注入（NoopLogisticsPort），无法查询物流轨迹。请在启动时注入 service.LogisticsPortAdapter。",
	}, nil
}
