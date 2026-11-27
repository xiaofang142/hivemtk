package monitor

import (
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tracing"
)

// LifecycleCheckStage 是单条 trace 中一个生命周期阶段的观测结果。
type LifecycleCheckStage struct {
	Node     string `json:"node"`
	Label    string `json:"label"`
	Order    int    `json:"order"`
	Required bool   `json:"required"`
	Present  bool   `json:"present"`
	Abnormal bool   `json:"abnormal"`
}

// LifecycleChecklist 把"这条消息走完哪几步"从人眼读 Span 变成可断言的结构。
// 生产实证：微信消息有 11 个 Span 却在 ai_result 失败；抖音会话只有 ingest +
// inbox_sync 两个 Span 就卡住 1099 分钟。缺 ai_trigger_decision 是后者的可观测盲点。
type LifecycleChecklist struct {
	Stages   []LifecycleCheckStage `json:"stages"`
	Missing  []string              `json:"missing"`
	Complete bool                  `json:"complete"`
}

// CheckLifecycleStages 按固定节点顺序核对一条 trace 的阶段覆盖。
// ai_trigger_decision / inbox_sync 是入站必达节点；后续投递节点按触发结果条件必达。
func CheckLifecycleStages(spans []model.MessageTrace) LifecycleChecklist {
	present := make(map[string]*model.MessageTrace, len(spans))
	for i := range spans {
		span := &spans[i]
		present[span.Node] = span
	}
	required := map[string]bool{
		tracing.NodeIngest:            true,
		tracing.NodeAITriggerDecision: true,
		tracing.NodeInboxSync:         true,
	}
	order := []string{
		tracing.NodeIngest,
		tracing.NodeAITriggerDecision,
		tracing.NodeInboxSync,
		tracing.NodeAIDispatch,
		tracing.NodeAIResult,
		tracing.NodeOutboundEnqueue,
		tracing.NodeDownlinkFetch,
		tracing.NodeDeliveredAck,
	}
	out := LifecycleChecklist{Stages: make([]LifecycleCheckStage, 0, len(order))}
	for _, node := range order {
		stage := LifecycleCheckStage{
			Node:     node,
			Label:    tracing.NodeLabel(node),
			Order:    tracing.NodeOrder(node),
			Required: required[node],
		}
		if span, ok := present[node]; ok {
			stage.Present = true
			stage.Abnormal = span.Abnormal != "" || span.Status == tracing.StatusFailed || span.Status == tracing.StatusAbnormal
		} else if stage.Required {
			out.Missing = append(out.Missing, node)
		}
		out.Stages = append(out.Stages, stage)
	}
	out.Complete = len(out.Missing) == 0
	return out
}
