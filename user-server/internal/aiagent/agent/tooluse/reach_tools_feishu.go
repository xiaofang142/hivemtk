package tooluse

import (
	"context"
)

func (NoOpReachAdapter) SendFeishu(ctx context.Context, accountID, openID, content string) (string, error) {
	return "", ErrAdapterNotConfigured
}

// ReachFeishuSendTool 飞书 Open API 发送
type ReachFeishuSendTool struct {
	BaseTool
	deps ReachToolDeps
}

func NewReachFeishuSendTool(deps ReachToolDeps) *ReachFeishuSendTool {
	return &ReachFeishuSendTool{
		BaseTool: BaseTool{
			NameVal:        "reach.feishu.send",
			RiskVal:        RiskHighWrite,
			CategoryVal:    CategoryReach,
			DescriptionVal: "通过飞书 Open API 发送消息。receive_id 需为 open_id（ou_xxx）或 chat_id（oc_xxx）。限流 50 QPS 全局 + 5 QPS/用户。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"account_id": {Type: "string", Description: "飞书应用账号 ID（数字字符串）"},
					"open_id": {
						Type:        "string",
						Description: "接收者 ID：用户 open_id（ou_xxx）或群 chat_id（oc_xxx）或 email",
					},
					"content": {Type: "string", Description: "消息文本"},
					"msg_type": {
						Type:        "string",
						Description: "消息类型（默认 text，可选 text/post/image/interactive）",
						Enum:        []string{"text", "post", "image", "interactive"},
						Default:     "text",
					},

					"customer_id": {Type: "string", Description: "客户 ID（用于客户轨迹和限流维度）"},
					"operator_id": {Type: "string", Description: "操作员 ID（用于权限校验）"},
				},
				Required: []string{"account_id", "open_id", "content"},
			},
		},
		deps: deps,
	}
}
