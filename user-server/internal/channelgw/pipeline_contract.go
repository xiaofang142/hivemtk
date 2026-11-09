package channelgw

import (
	"time"

	"hivemtk-user/internal/service"
)

// MD-03 渠道公共契约：IngressPipeline 对外暴露的本地结果/入参类型。
//
// 目标（验收口径）：调用者（真实传输 adapter ws.go、测试 adapter、未来 webhook
// 渠道）实现/消费 IngressPipeline 时不再被迫 import 根 Service；service 层返回类型
// 只在 pipeline.go 的接缝内做一次性映射，唯一权威实现仍是 InboxIngressService，
// 这里不放行业务逻辑、不长期并存第二套实现。
//
// 兼容约束：字段名/JSON 标签与 service 侧 DTO 逐字段一致（缺省、空数组/null 的
// 协议区别不变），由 pipeline_contract_test.go 的 JSON 语义对照钉死；协议字段、
// ctx 租户、幂等/租约/错误语义均不变。
//
// 字段取舍按「真正消费者实际使用的字段」记录（MD-01 测绘口径）：
//   - 入站结果：per_event/triggered_ai/reason；每事件 accepted/human_locked/
//     queued_for_ai/session_id（ws.go 回帧 FrameAck 的 IngestResult 五字段同源）。
//   - 认领：ClaimOutbound 仍透出 ORM 行 model.MessageHub（接缝适配内部模型，
//     不全量改 ORM 实体；认领内容/关联键/租约 claimed_at/push_attempts 随行带出）。
//   - ack：AckOutbound 影响数量 + AckOutboundDetailed 逐项分类
//     （acked/failed/duplicate/not_found/not_in_scope）与 items 逐项回执。
//   - ctx/tenant 不降级为普通参数：ctx 仍是一等入参，租户继续由 ctx 承载，
//     nil 未装配报错（errPipelineNotConfigured）沿既有契约。

// IngestEventResult 入站逐事件结果（对应 service.InboxIngressResult 的暴露子集）。
type IngestEventResult struct {
	Accepted    bool   `json:"accepted"`
	HumanLocked bool   `json:"human_locked"`
	QueuedForAI bool   `json:"queued_for_ai"`
	SessionID   string `json:"session_id"`
	Reason      string `json:"reason,omitempty"`
}

// IngestBatchResult 批量入站结果（对应 service.InboxIngressBatchResult）。
type IngestBatchResult struct {
	PerEvent    []*IngestEventResult `json:"per_event"`
	TriggeredAI bool                 `json:"triggered_ai"`
	Reason      string               `json:"reason,omitempty"`
}

// OutboundAckInput v2 协议逐项 ack 入参（对应 service.BridgeOutboundAckInput，
// 无 JSON 标签的口径与 service 侧一致）。
type OutboundAckInput struct {
	MsgID          string
	ConversationID string
	Status         string
	Error          string
}

// OutboundAckItem 详细 ack 逐项分类回执（对应 service.AckOutboundItem）。
type OutboundAckItem struct {
	MsgID  string `json:"msg_id"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// AckOutboundResult 详细 ack 结果（对应 service.AckOutboundResult，全字段保留）。
type AckOutboundResult struct {
	AffectedCount     int               `json:"affected_count"`
	AckedItemsCount   int               `json:"acked_items_count"`
	FailedItemsCount  int               `json:"failed_items_count"`
	UnknownItemsCount int               `json:"unknown_items_count"`
	DuplicateCount    int               `json:"duplicate_count"`
	NotFoundCount     int               `json:"not_found_count"`
	NotInScopeCount   int               `json:"not_in_scope_count"`
	Items             []OutboundAckItem `json:"items"`
}

// AckOutboundClaimFence claim 栅栏入参（msg_id → 认领时所见的 claimed_at；
// 乐观并发语义同 service.AckOutboundClaimFence，值必须取自认领返回行的 DB 读值）。
type AckOutboundClaimFence map[string]time.Time

// toIngestBatchResult 接缝映射：service 批量入站结果 → 本地契约结果。
// nil 透传；PerEvent 的 nil/空数组/null 区别逐字节保留（长度与逐项 nil 位置不变）。
func toIngestBatchResult(r *service.InboxIngressBatchResult) *IngestBatchResult {
	if r == nil {
		return nil
	}
	out := &IngestBatchResult{
		TriggeredAI: r.TriggeredAI,
		Reason:      r.Reason,
	}
	if r.PerEvent != nil {
		out.PerEvent = make([]*IngestEventResult, len(r.PerEvent))
		for i, e := range r.PerEvent {
			if e == nil {
				continue
			}
			out.PerEvent[i] = &IngestEventResult{
				Accepted:    e.Accepted,
				HumanLocked: e.HumanLocked,
				QueuedForAI: e.QueuedForAI,
				SessionID:   e.SessionID,
				Reason:      e.Reason,
			}
		}
	}
	return out
}

// toAckOutboundResult 接缝映射：service 详细 ack 结果 → 本地契约结果。
// Items 的 nil/空数组区别保留。
func toAckOutboundResult(r *service.AckOutboundResult) *AckOutboundResult {
	if r == nil {
		return nil
	}
	out := &AckOutboundResult{
		AffectedCount:     r.AffectedCount,
		AckedItemsCount:   r.AckedItemsCount,
		FailedItemsCount:  r.FailedItemsCount,
		UnknownItemsCount: r.UnknownItemsCount,
		DuplicateCount:    r.DuplicateCount,
		NotFoundCount:     r.NotFoundCount,
		NotInScopeCount:   r.NotInScopeCount,
	}
	if r.Items != nil {
		out.Items = make([]OutboundAckItem, len(r.Items))
		for i, it := range r.Items {
			out.Items[i] = OutboundAckItem{MsgID: it.MsgID, Status: it.Status, Error: it.Error}
		}
	}
	return out
}

// toOutboundAckInputs 接缝映射：本地逐项 ack 入参 → service 入参（nil 透传）。
func toOutboundAckInputs(m map[string]OutboundAckInput) map[string]service.BridgeOutboundAckInput {
	if m == nil {
		return nil
	}
	out := make(map[string]service.BridgeOutboundAckInput, len(m))
	for k, v := range m {
		out[k] = service.BridgeOutboundAckInput{
			MsgID:          v.MsgID,
			ConversationID: v.ConversationID,
			Status:         v.Status,
			Error:          v.Error,
		}
	}
	return out
}
