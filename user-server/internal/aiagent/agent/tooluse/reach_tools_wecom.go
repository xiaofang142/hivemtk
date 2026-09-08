package tooluse

import (
	"context"
)

func (NoOpReachAdapter) SendWeCom(ctx context.Context, accountID, externalUserID, msgType, content string) (string, error) {
	return "", ErrAdapterNotConfigured
}

type ReachWeComSendTool struct {
	BaseTool
	deps ReachToolDeps
}

func NewReachWeComSendTool(deps ReachToolDeps) *ReachWeComSendTool {
	return &ReachWeComSendTool{
		BaseTool: BaseTool{
			NameVal:        "reach.wecom.send",
			CategoryVal:    CategoryReach,
			DescriptionVal: "通过企业微信向客户发送消息。支持 text/image/link/textcard 等消息类型。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"account_id":       {Type: "string", Description: "企微账号 ID"},
					"external_user_id": {Type: "string", Description: "外部联系人 ID"},
					"msg_type": {
						Type:        "string",
						Description: "消息类型",
						Enum:        []string{"text", "image", "link", "textcard", "markdown", "file"},
						Default:     "text",
					},
					"content": {Type: "string", Description: "消息内容（text 类型为文本，其他类型为 JSON 配置）"},
				},
				Required: []string{"account_id", "external_user_id", "content"},
			},
		},
		deps: deps,
	}
}
