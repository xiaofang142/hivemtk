package tooluse

import (
	"context"
)

func (NoOpReachAdapter) SendWeb(ctx context.Context, sessionID, content string) (string, error) {
	return "", ErrAdapterNotConfigured
}

// ReachWebSendTool 网页客服发送（WebSocket 实时推送访客会话）
type ReachWebSendTool struct {
	BaseTool
	deps ReachToolDeps
}

func NewReachWebSendTool(deps ReachToolDeps) *ReachWebSendTool {
	return &ReachWebSendTool{
		BaseTool: BaseTool{
			NameVal:        "reach.web.send",
			RiskVal:        RiskHighWrite,
			CategoryVal:    CategoryReach,
			DescriptionVal: "通过网页客服渠道（WebSocket）向指定访客会话实时推送消息。消息落库并以「客服」身份展示给访客，不暴露 AI 标识。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"session_id": {Type: "string", Description: "访客会话 ID（对应 customer_sessions.session_id）"},
					"content":    {Type: "string", Description: "推送消息内容"},
				},
				Required: []string{"session_id", "content"},
			},
		},
		deps: deps,
	}
}
