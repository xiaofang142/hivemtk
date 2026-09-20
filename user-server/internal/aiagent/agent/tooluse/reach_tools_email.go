package tooluse

import (
	"context"
)

func (NoOpReachAdapter) SendEmail(ctx context.Context, to, subject, content string, attachments []string) (string, error) {
	return "", ErrAdapterNotConfigured
}

type ReachEmailSendTool struct {
	BaseTool
	deps ReachToolDeps
}

func NewReachEmailSendTool(deps ReachToolDeps) *ReachEmailSendTool {
	return &ReachEmailSendTool{
		BaseTool: BaseTool{
			NameVal:        "reach.email.send",
			RiskVal:        RiskHighWrite,
			CategoryVal:    CategoryReach,
			DescriptionVal: "发送邮件。支持附件。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"to":      {Type: "string", Description: "收件人邮箱（多个用逗号分隔）"},
					"subject": {Type: "string", Description: "邮件主题"},
					"content": {Type: "string", Description: "邮件内容（HTML 或纯文本）"},
					"attachments": {
						Type:        "array",
						Description: "附件 URL 列表",
						Items:       &ToolParam{Type: "string"},
					},
				},
				Required: []string{"to", "subject", "content"},
			},
		},
		deps: deps,
	}
}
