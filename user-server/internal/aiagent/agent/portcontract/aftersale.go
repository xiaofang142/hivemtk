package portcontract

import "context"

// 售后类型
const (
	AfterSaleRefund = "refund"
)

// 售后状态（本系统侧记录，真实状态由电商回写）
const (
	AfterSalePending = "pending"
)

// AfterSaleRequest 发起售后请求（客服侧发起，动作回写电商）。
// 这是客服系统对"订单"唯一允许写入的形态：写的是「售后请求」，不是「订单本身」。
type AfterSaleRequest struct {
	Platform      string `json:"platform"`
	OrderID       string `json:"order_id"`
	CustomerPhone string `json:"customer_phone"`
	CustomerName  string `json:"customer_name"`
	Type          string `json:"type"`
	Reason        string `json:"reason"`
	Amount        int64  `json:"amount"`
}

// AfterSaleView 售后只读视图
type AfterSaleView struct {
	ID            uint   `json:"id"`
	Platform      string `json:"platform"`
	OrderID       string `json:"order_id"`
	CustomerPhone string `json:"customer_phone"`
	CustomerName  string `json:"customer_name"`
	Type          string `json:"type"`
	Reason        string `json:"reason"`
	Amount        int64  `json:"amount"`
	Status        string `json:"status"`
	ExternalID    string `json:"external_id"`
}

// AfterSalePort 售后端口（客服唯一允许对订单"写"的入口：发起售后 → 回写电商）。
//
// 与 OrderPort 的关系：OrderPort 只读（查单），AfterSalePort 是客服侧唯一的"写"通道，
// 且写的是售后单、由电商执行落地，本系统只记录状态。二者共同构成"客服系统该有的订单能力"。
type AfterSalePort interface {
	Create(ctx context.Context, req *AfterSaleRequest) (*AfterSaleView, error)
	Query(ctx context.Context, platform, orderID, customerPhone string) ([]*AfterSaleView, error)
}
