package tooluse

import (
	"context"
)

func (NoOpReachAdapter) SendDouyin(ctx context.Context, accountID, openID, msgType, content string) (string, error) {
	return "", ErrAdapterNotConfigured
}

func (NoOpReachAdapter) SendKuaishou(ctx context.Context, accountID, openID, msgType, content string) (string, error) {
	return "", ErrAdapterNotConfigured
}

func (NoOpReachAdapter) SendXHS(ctx context.Context, accountID, openID, msgType, content string) (string, error) {
	return "", ErrAdapterNotConfigured
}

func (NoOpReachAdapter) SendTikTok(ctx context.Context, accountID, openID, msgType, content string) (string, error) {
	return "", ErrAdapterNotConfigured
}

func (NoOpReachAdapter) SendXianyu(ctx context.Context, accountID, openID, msgType, content string) (string, error) {
	return "", ErrAdapterNotConfigured
}

func (NoOpReachAdapter) SendDingTalk(ctx context.Context, chatID, msgType, content string) (string, error) {
	return "", ErrAdapterNotConfigured
}

type ReachDouyinSendTool struct {
	BaseTool
	deps ReachToolDeps
}

func NewReachDouyinSendTool(deps ReachToolDeps) *ReachDouyinSendTool {
	return &ReachDouyinSendTool{
		BaseTool: BaseTool{
			NameVal:        "reach.douyin.send",
			RiskVal:        RiskHighWrite,
			CategoryVal:    CategoryReach,
			DescriptionVal: "通过抖音私信向用户发送消息。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"account_id": {Type: "string", Description: "抖音账号 ID"},
					"open_id":    {Type: "string", Description: "用户 OpenID"},
					"msg_type": {
						Type:        "string",
						Description: "消息类型",
						Enum:        []string{"text", "image", "card", "video"},
						Default:     "text",
					},
					"content": {Type: "string", Description: "消息内容"},
				},
				Required: []string{"account_id", "open_id", "content"},
			},
		},
		deps: deps,
	}
}

type ReachKuaishouSendTool struct {
	BaseTool
	deps ReachToolDeps
}

func NewReachKuaishouSendTool(deps ReachToolDeps) *ReachKuaishouSendTool {
	return &ReachKuaishouSendTool{
		BaseTool: BaseTool{
			NameVal:        "reach.kuaishou.send",
			RiskVal:        RiskHighWrite,
			CategoryVal:    CategoryReach,
			DescriptionVal: "通过快手私信向用户发送消息。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"account_id": {Type: "string", Description: "快手账号 ID"},
					"open_id":    {Type: "string", Description: "用户 OpenID"},
					"msg_type": {
						Type:        "string",
						Description: "消息类型",
						Enum:        []string{"text", "image", "card", "video"},
						Default:     "text",
					},
					"content": {Type: "string", Description: "消息内容"},
				},
				Required: []string{"account_id", "open_id", "content"},
			},
		},
		deps: deps,
	}
}

type ReachXHSSendTool struct {
	BaseTool
	deps ReachToolDeps
}

func NewReachXHSSendTool(deps ReachToolDeps) *ReachXHSSendTool {
	return &ReachXHSSendTool{
		BaseTool: BaseTool{
			NameVal:        "reach.xhs.send",
			RiskVal:        RiskHighWrite,
			CategoryVal:    CategoryReach,
			DescriptionVal: "通过小红书私信向用户发送消息。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"account_id": {Type: "string", Description: "小红书账号 ID"},
					"open_id":    {Type: "string", Description: "用户 OpenID"},
					"msg_type": {
						Type:        "string",
						Description: "消息类型",
						Enum:        []string{"text", "image", "card"},
						Default:     "text",
					},
					"content": {Type: "string", Description: "消息内容"},
				},
				Required: []string{"account_id", "open_id", "content"},
			},
		},
		deps: deps,
	}
}

type ReachDingTalkSendTool struct {
	BaseTool
	deps ReachToolDeps
}

func NewReachDingTalkSendTool(deps ReachToolDeps) *ReachDingTalkSendTool {
	return &ReachDingTalkSendTool{
		BaseTool: BaseTool{
			NameVal:        "reach.dingtalk.send",
			RiskVal:        RiskHighWrite,
			CategoryVal:    CategoryReach,
			DescriptionVal: "通过钉钉机器人或群消息发送消息。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"chat_id": {Type: "string", Description: "钉钉会话 ID（群 ID 或机器人 webhook）"},
					"msg_type": {
						Type:        "string",
						Description: "消息类型",
						Enum:        []string{"text", "markdown", "action_card", "link"},
						Default:     "text",
					},
					"content": {Type: "string", Description: "消息内容"},
				},
				Required: []string{"chat_id", "content"},
			},
		},
		deps: deps,
	}
}
