package service

import "hivemtk-user/internal/model"

// leadChannelWiringAllowsMining 是 LM-06 的双模块独立分发契约：
// 只有持久化的真实入站（非 AI 回声、非系统通知、非重复事件）才进入发掘；
// 自动回复模块开/关不影响本判定，反之亦然（两侧独立闸由 LEAD-03/CH-09 消费）。
func leadChannelWiringAllowsMining(hub *model.MessageHub, senderType string, duplicate bool) bool { //nolint:unused // LM-06 测试原型，实际入站链仍使用各自准入检查，尚未消费此函数。
	if hub == nil || duplicate || hub.Direction != "inbound" || hub.IsAIReply {
		return false
	}
	switch senderType {
	case "", "customer", "user", "other":
		return true
	default:
		return false
	}
}
