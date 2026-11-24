package portcontract

import "context"

// OrderView 订单只读视图（工具层投影，避免工具直接依赖 service/model 具体实现）。
// 客服系统里的订单是「外部电商同步进来的只读镜像」。EC-07 要求调用方区分镜像时间与实时结果：
// 本视图字段均来自 mirror（external_orders），不代表平台实时权威；请结合 MirrorUpdatedAt 判断新鲜度。
type OrderView struct {
	Platform      string `json:"platform"`
	OrderID       string `json:"order_id"`
	OrderNo       string `json:"order_no"`
	UserName      string `json:"user_name"`
	UserPhone     string `json:"user_phone"`
	TotalAmount   int64  `json:"total_amount"`
	PayAmount     int64  `json:"pay_amount"`
	DiscountAmount int64 `json:"discount_amount,omitempty"`
	Status        string `json:"status"`
	PayTime       string `json:"pay_time"`
	ShipTime      string `json:"ship_time"`
	CompleteTime  string `json:"complete_time"`
	Items         string `json:"items"`
	// ShippingAddr 为镜像中的收货地址文本（可能含 PII，按需脱敏展示由上层负责）。
	ShippingAddr string `json:"shipping_addr,omitempty"`
	// MirrorUpdatedAt 镜像最后更新时间（RFC3339），用于区分“镜像时点”与“实时平台状态”。
	MirrorUpdatedAt string `json:"mirror_updated_at,omitempty"`
	// DataSource 固定为 external_order_mirror，提示本数据非实时平台查询结果。
	DataSource string `json:"data_source"`
}

// OrderPort 订单查询端口（客服只读镜像，绝不创建/履约电商订单）。
//
// 设计依据：客服系统不是电商。订单数据由外部电商/OMS 通过集成（拉取+Webhook）
// 同步进 user_db.orders（model.ExternalOrder），客服只查询、不写商业订单。
// 这是 agent 工具层访问订单的唯一合法通道。
type OrderPort interface {
	LookupByOrderID(ctx context.Context, platform, orderID string) (*OrderView, error)
	LookupByCustomer(ctx context.Context, phone, name string) ([]*OrderView, error)
}
