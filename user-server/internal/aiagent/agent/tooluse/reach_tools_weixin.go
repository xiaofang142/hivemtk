package tooluse

import (
	"context"
)

func (NoOpReachAdapter) SendWeixin(ctx context.Context, openID, msgType, content string) (string, error) {
	return "", ErrAdapterNotConfigured
}

type ReachWeixinSendTool struct {
	BaseTool
	deps ReachToolDeps
}

func NewReachWeixinSendTool(deps ReachToolDeps) *ReachWeixinSendTool {
	return &ReachWeixinSendTool{
		BaseTool: BaseTool{
			NameVal:        "reach.weixin.send",
			RiskVal:        RiskHighWrite,
			CategoryVal:    CategoryReach,
			DescriptionVal: "通过微信公众号向用户发送消息（客服消息）。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"open_id": {Type: "string", Description: "用户 OpenID"},
					"msg_type": {
						Type:        "string",
						Description: "消息类型",
						Enum:        []string{"text", "image", "news", "template"},
						Default:     "text",
					},
					"content": {Type: "string", Description: "消息内容"},
				},
				Required: []string{"open_id", "content"},
			},
		},
		deps: deps,
	}
}
