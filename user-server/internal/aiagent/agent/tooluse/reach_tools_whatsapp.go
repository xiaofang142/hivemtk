package tooluse

import (
	"context"
)

func (NoOpReachAdapter) SendWhatsApp(ctx context.Context, accountID, toPhone, content string) (string, error) {
	return "", ErrAdapterNotConfigured
}

// ReachWhatsAppSendTool WhatsApp Cloud API 发送
type ReachWhatsAppSendTool struct {
	BaseTool
	deps ReachToolDeps
}

func NewReachWhatsAppSendTool(deps ReachToolDeps) *ReachWhatsAppSendTool {
	return &ReachWhatsAppSendTool{
		BaseTool: BaseTool{
			NameVal:        "reach.whatsapp.send",
			RiskVal:        RiskHighWrite,
			CategoryVal:    CategoryReach,
			DescriptionVal: "通过 WhatsApp Cloud API 发送消息。主动触达需使用 Meta 审批模板；24h 客服窗口内可发自由文本。限流 5 msg/s/号。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"account_id": {Type: "string", Description: "WhatsApp Cloud 账号 ID（数字字符串）"},
					"to_phone":   {Type: "string", Description: "目标手机号（E.164 国际格式，如 +8613800138000）"},
					"content":    {Type: "string", Description: "消息文本"},
					"template_id": {
						Type:        "string",
						Description: "模板消息名（marketing/utility 模板需 Meta 审批；非必填，24h 窗口内可发自由文本）",
					},
					"params": {
						Type:        "object",
						Description: "模板参数（key-value，对应模板 {{1}} {{2}} 等占位符）",
						Properties:  map[string]ToolParam{},
					},

					"customer_id": {Type: "string", Description: "客户 ID（用于客户轨迹和限流维度）"},
					"operator_id": {Type: "string", Description: "操作员 ID（用于权限校验）"},
				},
				Required: []string{"account_id", "to_phone", "content"},
			},
		},
		deps: deps,
	}
}
