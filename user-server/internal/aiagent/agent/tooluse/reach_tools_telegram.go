package tooluse

import (
	"context"
)

func (NoOpReachAdapter) SendTelegram(ctx context.Context, accountID, chatID, content string) (string, error) {
	return "", ErrAdapterNotConfigured
}

// ReachTelegramSendTool Telegram Bot API 发送
type ReachTelegramSendTool struct {
	BaseTool
	deps ReachToolDeps
}

func NewReachTelegramSendTool(deps ReachToolDeps) *ReachTelegramSendTool {
	return &ReachTelegramSendTool{
		BaseTool: BaseTool{
			NameVal:        "reach.telegram.send",
			RiskVal:        RiskHighWrite,
			CategoryVal:    CategoryReach,
			DescriptionVal: "通过 Telegram Bot API 发送消息。支持私聊（chat_id 为正）和群组（chat_id 为负）。限流 1 QPS/chat + 30 msg/s 全局。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"account_id": {Type: "string", Description: "TG 机器人账号 ID（数字字符串）"},
					"chat_id": {
						Type:        "string",
						Description: "目标 chat_id：私聊为正（如 123456789），群组为负（如 -1001234567890）",
					},
					"content": {Type: "string", Description: "消息文本（最长 4096 字符，超过会被 Telegram API 拒绝）"},

					"customer_id": {Type: "string", Description: "客户 ID（用于客户轨迹和限流维度）"},
					"operator_id": {Type: "string", Description: "操作员 ID（用于权限校验）"},
				},
				Required: []string{"account_id", "chat_id", "content"},
			},
		},
		deps: deps,
	}
}
