package model

import "time"

// AfterSale 售后单（客服侧发起，动作回写电商；本系统只记录状态，不履约）。
//
// 这是客服系统对"订单"唯一允许写入的形态。订单本身的创建/支付/履约由外部电商负责，
// 客服只在此记录"退款/退货/换货"请求并跟踪其状态（状态由电商通过 Webhook 回写）。
//
// EC-14 (M14, future / not implemented here): 当前模型与流程不包含逆向物流/验收/换货履约所需证据，
// 例如：退货运单/取件预约/承运商信息、仓库收货与质检(inspection/QC)结果、验收证据引用、
// 换货原SKU→新SKU/补寄SKU映射、补寄出库/发运信息、以及用于“仓库签收/重复验收事件”去重的
// 外部事件幂等键（event_id/provider_event_id）。设计原则（来自 M14）：
//   - 同意退货 ≠ 收到货 ≠ 质检通过 ≠ 退款；收货/质检/换货发运必须分别有证据与可观察状态。
//   - 重复仓库签收/重复出库事件不得重复退款或重复发货（需事件幂等/唯一约束/对账）。
//   - 当前类型字段 Type 仅表达意图(refund/return/exchange, 见 agent tool)，不表达逆向阶段机。
//
// TODO(EC-10/M10/M13, not implemented): 需纳入统一业务操作账本与幂等/对账：
//   - 稳定幂等键（按店铺+订单+售后意图+参数指纹），防重复提交与重试重复扣款；
//   - 外部调用应走持久化 attempt/lease + 事务性 outbox，避免“本地已落库后远端调用失败/超时”语义不清；
//   - 退款/资金需资金预占/余额对账与资源守卫（RV03），区分受理/处理中/资金成功/失败，禁止吞错为成功；
//   - 与 OrderChangeRequest 一样，最终应以 operation_id 关联审计与证据链（兼容现有关联）。
type AfterSale struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID            uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Platform      string    `gorm:"type:varchar(50);index" json:"platform"`
	OrderID       string    `gorm:"type:varchar(100);index" json:"order_id"`
	CustomerPhone string    `gorm:"type:varchar(50);index" json:"customer_phone"`
	CustomerName  string    `gorm:"type:varchar(100)" json:"customer_name"`
	Type          string    `gorm:"type:varchar(50)" json:"type"` // refund | return | exchange (agent tool contract); EC-14 will require finer reverse/exchange stage evidence elsewhere
	Reason        string    `gorm:"type:text" json:"reason"`
	Amount        int64     `gorm:"type:bigint;default:0" json:"amount"` // 最小货币单位（分）；币种需与订单/渠道一致（EC-13 待补充显式 currency 字段与校验）
	Status        string    `gorm:"type:varchar(50);default:'pending'" json:"status"` // 建议语义（EC-13 目标）：pending=本地立案(未确认远端受理)；accepted/processing/funded/failed/unknown=由远端回写或人工对账填充；当前为宽松字符串，尚未强制枚举
	ExternalID    string    `gorm:"type:varchar(100)" json:"external_id"` // 远端平台售后/退款单号（有则表示远端已受理或已创建引用；无不等于成功）
	CreatedAt     time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (AfterSale) TableName() string { return "after_sales" }
