package tooluse

import (
	"context"
)

func (NoOpReachAdapter) SendSMS(ctx context.Context, phone, content, templateID string, params map[string]string) (string, error) {
	return "", ErrAdapterNotConfigured
}

type ReachSMSSendTool struct {
	BaseTool
	deps ReachToolDeps
}

func NewReachSMSSendTool(deps ReachToolDeps) *ReachSMSSendTool {
	return &ReachSMSSendTool{
		BaseTool: BaseTool{
			NameVal:        "reach.sms.send",
			RiskVal:        RiskHighWrite,
			CategoryVal:    CategoryReach,
			DescriptionVal: "发送短信。支持模板短信（传 template_id + params）和直发（传 content）。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"phone":       {Type: "string", Description: "手机号"},
					"content":     {Type: "string", Description: "短信内容（直发模式）"},
					"template_id": {Type: "string", Description: "短信模板 ID（模板模式）"},
					"params": {
						Type:        "object",
						Description: "模板参数（key-value）",
						Properties:  map[string]ToolParam{},
					},
				},
				Required: []string{"phone"},
			},
		},
		deps: deps,
	}
}
