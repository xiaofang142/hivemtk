package tooluse

import "context"

// LeadMiningQualityTool 是 LM-PR6 的 ReAct 工具化判定/质量查询 stub。
// 最小接入：注册到全局 ToolRegistry 后，Agent 可发现该工具；
// Execute 当前返回占位结果，待后续接入 lead_mining service 查询与反馈闭环。
type LeadMiningQualityTool struct {
	BaseTool
	QueryFunc func(ctx context.Context, args map[string]any) (ToolResult, error)
}

func NewLeadMiningQualityTool(queryFunc func(ctx context.Context, args map[string]any) (ToolResult, error)) *LeadMiningQualityTool {
	return &LeadMiningQualityTool{
		BaseTool: BaseTool{
			NameVal:        "lead_mining_quality",
			CategoryVal:    CategoryBusiness,
			DescriptionVal: "查询线索发掘质量看板/判定反馈状态（LM-PR6 stub）。输入 action=quality_stats|judgement_detail，days/judgement_id 可选。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"action":       {Type: "string", Description: "quality_stats 或 judgement_detail", Enum: []string{"quality_stats", "judgement_detail"}},
					"days":         {Type: "integer", Description: "质量看板统计天数，默认7"},
					"judgement_id": {Type: "string", Description: "判定记录ID，action=judgement_detail 时必填"},
				},
				Required: []string{"action"},
			},
			RiskVal: RiskReadonly,
		},
		QueryFunc: queryFunc,
	}
}

func (t *LeadMiningQualityTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if t.QueryFunc != nil {
		return t.QueryFunc(ctx, args)
	}
	return ToolResult{
		Success: true,
		Data: map[string]any{
			"status":  "stub",
			"message": "lead_mining_quality tool stub registered; wire QueryFunc to lead mining service for full LM-PR6 ReAct integration",
			"args":    args,
		},
	}, nil
}

// RegisterLeadMiningQualityStub 注册默认 stub 到全局注册中心（LM-PR6 最小接入）。
// 未注入 QueryFunc 时 Execute 返回占位结果；后续应在 agent bootstrap 中注入真实查询函数。
func RegisterLeadMiningQualityStub() error {
	return RegisterGlobal(NewLeadMiningQualityTool(nil))
}
