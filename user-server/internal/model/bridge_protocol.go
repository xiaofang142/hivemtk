package model

import "hivemtk-user/internal/pkg/channelbus"

// 下行终态别名一律指向 channelbus：model 侧不再持有独立词表，
// 避免与线路协议单源漂移（G-01 / 二次审计 F-04）。
const (
	BridgeAckStatusDelivered = channelbus.StatusDelivered
	BridgeAckStatusFailed    = channelbus.StatusFailed

	// BridgeAckStatusUnknown CH-37b 刀4（R2/R6）：「动作已提交但结果不明」终态。
	//
	// 与 delivered（买家可见）/ failed（平台明确拒绝、可安全重试）严格区分：unknown
	// 表示桥端已把消息提交到平台但拿不到确定结果（平台成功但 ack 丢失、超时后迟到完成、
	// worker 崩溃回收竞争等）。因此 unknown 行**永不自动重发**：不进 ClaimPendingOutbound
	// 的 pending 查询、不被 exhaust/回收逻辑改写、不被预算重试；出路只有一条——
	// 人工/API 显式对账迁移到 delivered 或 failed (repository.ReconcileUnknownOutbound,
	// push_error 留操作来源)。落库形态：status='unknown' + push_error='ack_unknown'。
	// EC-05 (发送证据分级/未知处理): 当渠道或桥接无法提供可验证回执时应落为 unknown（“不提供回执的状态为未知”），
	// 不得伪装为 delivered。证据分级可理解为: local(pending/入队) → bridge_claimed(inflight+claimed_at) →
	// platform/report 终态 (delivered/failed/unknown); “买家可见/已读”若渠道不支持则不应断言 delivered，需 unknown 或待对账。
	// 无 DDL：message_hub.status 是无约束 varchar(20)，'unknown' 7 字符直接可用。
	BridgeAckStatusUnknown = channelbus.StatusUnknown

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
