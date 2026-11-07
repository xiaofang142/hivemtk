package model

const (
	BridgeAckStatusDelivered = "delivered"
	BridgeAckStatusFailed    = "failed"

	// BridgeAckStatusUnknown CH-37b 刀4（R2/R6）：「动作已提交但结果不明」终态。
	//
	// 与 delivered（买家可见）/ failed（平台明确拒绝、可安全重试）严格区分：unknown
	// 表示桥端已把消息提交到平台但拿不到确定结果（平台成功但 ack 丢失、超时后迟到完成、
	// worker 崩溃回收竞争等）。因此 unknown 行**永不自动重发**：不进 ClaimPendingOutbound
	// 的 pending 查询、不被 exhaust/回收逻辑改写、不被预算重试；出路只有一条——
	// 人工/API 显式对账迁移到 delivered 或 failed（repository.ReconcileUnknownOutbound，
	// push_error 留操作来源）。落库形态：status='unknown' + push_error='ack_unknown'。
	// 无 DDL：message_hub.status 是无约束 varchar(20)，'unknown' 7 字符直接可用。
	BridgeAckStatusUnknown = "unknown"

	BridgeAckStatusAcked      = "acked"
	BridgeAckStatusDuplicate  = "duplicate"
	BridgeAckStatusNotFound   = "not_found"
	BridgeAckStatusNotInScope = "not_in_scope"
)

// IsBridgeAckTerminalStatus 详细 ack terminalStatus 的合法值判定（CH-37b 刀4 起三值）。
// delivered / failed / unknown 之外的值一律拒绝（协议错误），各入口（HTTP v1/v2、WS、
// service、repo）共用本判定，避免五处校验点各写一份词表后漂移。
func IsBridgeAckTerminalStatus(s string) bool {
	switch s {
	case BridgeAckStatusDelivered, BridgeAckStatusFailed, BridgeAckStatusUnknown:
		return true
	}
	return false
}
