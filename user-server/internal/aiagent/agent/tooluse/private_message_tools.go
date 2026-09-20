package tooluse

import (
	"context"
	"strconv"
	"time"

	"hivemtk-user/internal/aiagent/agent/portcontract"
	"hivemtk-user/internal/model"
)

// PrivateMessageToolDeps 私信工具依赖
//
// 所有方法统一走 portcontract.SessionPort。
type PrivateMessageToolDeps struct {
	Session portcontract.SessionPort
}

// NewPrivateMessageToolDepsWithPort 创建带 Session Port 注入的私信工具依赖。
//
// 在装配期调用：
//
//	sessionPort := service.NewSessionPortAdapter(service.NewCustomerSessionService())
//	deps := tooluse.NewPrivateMessageToolDepsWithPort(sessionPort)
func NewPrivateMessageToolDepsWithPort(session portcontract.SessionPort) PrivateMessageToolDeps {
	return PrivateMessageToolDeps{Session: session}
}

// BuildPrivateMessageTools 构造全部 3 个私信工具（不注册到 Registry）
//
// deps.Session 为 nil 时工具以 "port not injected" 应答（fail-closed）。
// 调用方：PrivateMessageToolProvider.Provide()
func BuildPrivateMessageTools(deps PrivateMessageToolDeps) []Tool {
	port := deps.Session
	return []Tool{
		&openPrivateSessionTool{sessionPort: port},
		&readPrivateSessionTool{sessionPort: port},
		&sendPrivateMessageTool{sessionPort: port},
	}
}

// RegisterPrivateMessageTools 注册私信工具（CategoryPrivateMessage）
func RegisterPrivateMessageTools(registry *ToolRegistry, deps PrivateMessageToolDeps) error {
	tools := BuildPrivateMessageTools(deps)
	for _, t := range tools {
		if err := registry.Register(t); err != nil {
			return err
		}
	}
	return nil
}

type openPrivateSessionTool struct {
	BaseTool
	sessionPort portcontract.SessionPort
}

func (t *openPrivateSessionTool) Name() string             { return "pm.session.open" }
func (t *openPrivateSessionTool) RiskLevel() ToolRiskLevel { return RiskLowWrite }
func (t *openPrivateSessionTool) Category() ToolCategory {
	return CategoryPrivateMessage
}
func (t *openPrivateSessionTool) Description() string {
	return "主动开启一个私信会话（一对一对话域），用于智能体主动链接/唤起用户。返回 session_id 供后续 pm.message.send 使用。"
}
func (t *openPrivateSessionTool) Parameters() ToolParameters {
	return ToolParameters{
		Type: "object",
		Properties: map[string]ToolParam{
			"platform":   {Type: "string", Description: "渠道（web/tg/wecom/feishu 等）"},
			"account_id": {Type: "string", Description: "渠道账号 ID"},
			"user_id":    {Type: "string", Description: "客户 OneID / 渠道用户 ID"},
			"user_name":  {Type: "string", Description: "客户昵称（可选）"},
			"user_phone": {Type: "string", Description: "客户手机号（可选）"},
			"user_email": {Type: "string", Description: "客户邮箱（可选）"},
		},
		Required: []string{"platform", "account_id", "user_id"},
	}
}
func (t *openPrivateSessionTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	start := time.Now()
	platform, _ := args["platform"].(string)
	accountID, _ := args["account_id"].(string)
	userID, _ := args["user_id"].(string)
	if err := ValidateRequired(args, []string{"platform", "account_id", "user_id"}); err != nil {
		return ErrorResult(t.Name(), err), nil
	}
	userName, _ := args["user_name"].(string)
	userPhone, _ := args["user_phone"].(string)
	userEmail, _ := args["user_email"].(string)
	if t.sessionPort == nil {
		return ErrorResult(t.Name(), errStr("SessionPort not injected")), nil
	}
	session, err := t.sessionPort.CreateSession(ctx, &portcontract.CreateSessionInput{
		Platform:  platform,
		AccountID: accountID,
		UserID:    userID,
		UserName:  userName,
		UserPhone: userPhone,
		UserEmail: userEmail,
	})
	if err != nil {
		return ErrorResult(t.Name(), err), nil
	}
	return SuccessResult(t.Name(), map[string]any{
		"session_id": session.SessionID,
		"status":     session.Status,
	}).withTiming(t.Name(), start), nil
}

type readPrivateSessionTool struct {
	BaseTool
	sessionPort portcontract.SessionPort
}

func (t *readPrivateSessionTool) Name() string             { return "pm.session.read" }
func (t *readPrivateSessionTool) RiskLevel() ToolRiskLevel { return RiskReadonly }
func (t *readPrivateSessionTool) Category() ToolCategory {
	return CategoryPrivateMessage
}
func (t *readPrivateSessionTool) Description() string {
	return "读取指定私信会话的消息历史，供智能体理解对话上下文。返回消息列表与总数。"
}
func (t *readPrivateSessionTool) Parameters() ToolParameters {
	return ToolParameters{
		Type: "object",
		Properties: map[string]ToolParam{
			"session_id": {Type: "string", Description: "私信会话 ID"},
			"page":       {Type: "integer", Description: "页码（默认 1）"},
			"page_size":  {Type: "integer", Description: "每页条数（默认 20，最大 100）"},
		},
		Required: []string{"session_id"},
	}
}
func (t *readPrivateSessionTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	start := time.Now()
	sessionID, _ := args["session_id"].(string)
	if err := ValidateRequired(args, []string{"session_id"}); err != nil {
		return ErrorResult(t.Name(), err), nil
	}
	page := 1
	pageSize := 20
	if v, ok := args["page"]; ok {
		if n, err := strconv.Atoi(toStr(v)); err == nil && n > 0 {
			page = n
		}
	}
	if v, ok := args["page_size"]; ok {
		if n, err := strconv.Atoi(toStr(v)); err == nil && n > 0 && n <= 100 {
			pageSize = n
		}
	}
	if t.sessionPort == nil {
		return ErrorResult(t.Name(), errStr("SessionPort not injected")), nil
	}
	messages, total, err := t.sessionPort.GetMessages(sessionID, page, pageSize)
	if err != nil {
		return ErrorResult(t.Name(), err), nil
	}
	return SuccessResult(t.Name(), map[string]any{
		"session_id": sessionID,
		"total":      total,
		"messages":   messages,
	}).withTiming(t.Name(), start), nil
}

type sendPrivateMessageTool struct {
	BaseTool
	sessionPort portcontract.SessionPort
}

func (t *sendPrivateMessageTool) Name() string             { return "pm.message.send" }
func (t *sendPrivateMessageTool) RiskLevel() ToolRiskLevel { return RiskHighWrite }
func (t *sendPrivateMessageTool) Category() ToolCategory {
	return CategoryPrivateMessage
}
func (t *sendPrivateMessageTool) Description() string {
	return "在指定私信会话中发送一条消息（智能体回复用户 / 主动触达写入）。支持文本与媒体，实时推送访客端。"
}
func (t *sendPrivateMessageTool) Parameters() ToolParameters {
	return ToolParameters{
		Type: "object",
		Properties: map[string]ToolParam{
			"session_id":   {Type: "string", Description: "私信会话 ID"},
			"content":      {Type: "string", Description: "消息内容"},
			"content_type": {Type: "string", Description: "消息类型（text/image 等，默认 text）"},
			"media_url":    {Type: "string", Description: "媒体 URL（content_type 非 text 时必填）"},
			"sender_type":  {Type: "string", Description: "发送方：ai（智能体）/ agent（人工）", Enum: []string{"ai", "agent"}},
			"sender_id":    {Type: "string", Description: "发送方 ID（可选）"},
			"sender_name":  {Type: "string", Description: "发送方昵称（可选）"},
		},
		Required: []string{"session_id", "content", "sender_type"},
	}
}
func (t *sendPrivateMessageTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	start := time.Now()
	if err := ValidateRequired(args, []string{"session_id", "content", "sender_type"}); err != nil {
		return ErrorResult(t.Name(), err), nil
	}
	sessionID, _ := args["session_id"].(string)
	content, _ := args["content"].(string)
	senderType, _ := args["sender_type"].(string)
	if senderType != "ai" && senderType != "agent" {
		return ErrorResult(t.Name(), errInvalidSenderType), nil
	}
	contentType, _ := args["content_type"].(string)
	if contentType == "" {
		contentType = string(model.MessageTypeText)
	}
	mediaURL, _ := args["media_url"].(string)
	senderID, _ := args["sender_id"].(string)
	senderName, _ := args["sender_name"].(string)
	if t.sessionPort == nil {
		return ErrorResult(t.Name(), errStr("SessionPort not injected")), nil
	}
	msg, err := t.sessionPort.SendMessage(ctx, &portcontract.SendMessageInput{
		SessionID:   sessionID,
		SenderType:  senderType,
		SenderID:    senderID,
		Content:     content,
		ContentType: contentType,
	})
	if err != nil {
		return ErrorResult(t.Name(), err), nil
	}
	_ = mediaURL
	_ = senderName
	return SuccessResult(t.Name(), map[string]any{
		"message_id":  msg.ID,
		"session_id":  msg.SessionID,
		"sender_type": msg.SenderType,
	}).withTiming(t.Name(), start), nil
}

var errInvalidSenderType = errStr("sender_type 必须为 ai 或 agent")

func errStr(s string) error { return &simpleErr{s} }

type simpleErr struct{ msg string }

func (e *simpleErr) Error() string { return e.msg }

func toStr(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	default:
		return ""
	}
}
