package tooluse

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

type ReachAdapter interface {
	SendSMS(ctx context.Context, phone, content, templateID string, params map[string]string) (msgID string, err error)
	SendEmail(ctx context.Context, to, subject, content string, attachments []string) (msgID string, err error)
	SendWeCom(ctx context.Context, accountID, externalUserID, msgType, content string) (msgID string, err error)
	SendWeixin(ctx context.Context, openID, msgType, content string) (msgID string, err error)
	SendDouyin(ctx context.Context, accountID, openID, msgType, content string) (msgID string, err error)
	SendKuaishou(ctx context.Context, accountID, openID, msgType, content string) (msgID string, err error)
	SendXHS(ctx context.Context, accountID, openID, msgType, content string) (msgID string, err error)
	SendTikTok(ctx context.Context, accountID, openID, msgType, content string) (msgID string, err error)
	SendXianyu(ctx context.Context, accountID, openID, msgType, content string) (msgID string, err error)
	SendDingTalk(ctx context.Context, chatID, msgType, content string) (msgID string, err error)
	SendTelegram(ctx context.Context, accountID, chatID, content string) (msgID string, err error)
	SendWhatsApp(ctx context.Context, accountID, toPhone, content string) (msgID string, err error)
	SendFeishu(ctx context.Context, accountID, openID, content string) (msgID string, err error)
	SendWeb(ctx context.Context, sessionID, content string) (msgID string, err error)
	SendCard(ctx context.Context, channel, accountID, externalUserID, cardID string) (msgID string, err error)
	Recall(ctx context.Context, channel, msgID string) error
	AccountHealth(ctx context.Context, channel, accountID string) (*AccountHealthInfo, error)
	ListAccounts(ctx context.Context, channel string) ([]AccountInfo, error)
}

type AccountHealthInfo struct {
	AccountID   string `json:"account_id"`
	Channel     string `json:"channel"`
	Status      string `json:"status"`
	DailyQuota  int    `json:"daily_quota"`
	DailyUsed   int    `json:"daily_used"`
	DailyRemain int    `json:"daily_remain"`
	RiskLevel   string `json:"risk_level"`
	LastCheckAt string `json:"last_check_at"`
}

type AccountInfo struct {
	AccountID string `json:"account_id"`
	Channel   string `json:"channel"`
	Nickname  string `json:"nickname"`
	Status    string `json:"status"`
	IsHealthy bool   `json:"is_healthy"`
}

var ErrAdapterNotConfigured = errors.New("reach adapter not configured")

func (NoOpReachAdapter) SendCard(ctx context.Context, channel, accountID, externalUserID, cardID string) (string, error) {
	return "", ErrAdapterNotConfigured
}

func (NoOpReachAdapter) Recall(ctx context.Context, channel, msgID string) error {
	return ErrAdapterNotConfigured
}

func (NoOpReachAdapter) AccountHealth(ctx context.Context, channel, accountID string) (*AccountHealthInfo, error) {
	return nil, ErrAdapterNotConfigured
}

func (NoOpReachAdapter) ListAccounts(ctx context.Context, channel string) ([]AccountInfo, error) {
	return nil, ErrAdapterNotConfigured
}

type ReachToolDeps struct {
	Adapter      ReachAdapter
	Pipeline     ReachBatchPipelinePort
	DB           *gorm.DB
	SendPipeline ReachSendPipelinePort
}

func NewReachToolDeps() ReachToolDeps {
	return ReachToolDeps{
		Adapter: NoOpReachAdapter{},
	}
}

func dispatchToAdapter(ctx context.Context, adapter ReachAdapter, req *ReachSendRequest) (string, error) {
	if adapter == nil {
		return "", ErrAdapterNotConfigured
	}
	switch req.Channel {
	case "sms":
		return adapter.SendSMS(ctx, req.RecipientID, req.Content, req.TemplateID, req.Params)
	case "email":
		return adapter.SendEmail(ctx, req.RecipientID, req.Subject, req.Content, req.Attachments)
	case "wecom":
		return adapter.SendWeCom(ctx, req.AccountID, req.RecipientID, req.MsgType, req.Content)
	case "weixin":
		return adapter.SendWeixin(ctx, req.RecipientID, req.MsgType, req.Content)
	case "douyin", "douyin_web":
		return adapter.SendDouyin(ctx, req.AccountID, req.RecipientID, req.MsgType, req.Content)
	case "kuaishou", "kuaishou_web":
		return adapter.SendKuaishou(ctx, req.AccountID, req.RecipientID, req.MsgType, req.Content)
	case "xiaohongshu", "xhs", "xhs_web":
		return adapter.SendXHS(ctx, req.AccountID, req.RecipientID, req.MsgType, req.Content)
	case "tiktok", "tiktok_web":
		return adapter.SendTikTok(ctx, req.AccountID, req.RecipientID, req.MsgType, req.Content)
	case "xianyu", "xianyu_web":
		return adapter.SendXianyu(ctx, req.AccountID, req.RecipientID, req.MsgType, req.Content)
	case "dingtalk":
		return adapter.SendDingTalk(ctx, req.RecipientID, req.MsgType, req.Content)
	case "telegram":
		return adapter.SendTelegram(ctx, req.AccountID, req.RecipientID, req.Content)
	case "whatsapp":
		return adapter.SendWhatsApp(ctx, req.AccountID, req.RecipientID, req.Content)
	case "feishu":
		return adapter.SendFeishu(ctx, req.AccountID, req.RecipientID, req.Content)
	case "web":
		return adapter.SendWeb(ctx, req.RecipientID, req.Content)
	case "card":

		subchannel := "douyin"
		if req.Metadata != nil {
			if sc, ok := req.Metadata["subchannel"]; ok && sc != "" {
				subchannel = sc
			}
		}
		return adapter.SendCard(ctx, subchannel, req.AccountID, req.RecipientID, req.CardID)
	default:
		return "", fmt.Errorf("unknown channel: %s", req.Channel)
	}
}

func sendViaPipeline(ctx context.Context, deps ReachToolDeps, req *ReachSendRequest) (string, *ReachSendResponse, error) {
	if deps.SendPipeline != nil {
		resp := deps.SendPipeline.Send(ctx, req)
		if !resp.Success {
			return "", resp, errors.New(resp.Error)
		}
		return resp.MessageID, resp, nil
	}

	complianceReminderHook(req.Channel, req.RecipientID)
	msgID, err := dispatchToAdapter(ctx, deps.Adapter, req)
	return msgID, &ReachSendResponse{
		Success:   err == nil,
		MessageID: msgID,
		Channel:   req.Channel,
		Error: func() string {
			if err != nil {
				return err.Error()
			}
			return ""
		}(),
		SentAt: time.Now(),
	}, err
}

func BuildReachTools(deps ReachToolDeps) []Tool {
	return []Tool{
		NewReachSMSSendTool(deps),
		NewReachEmailSendTool(deps),
		NewReachWeComSendTool(deps),
		NewReachWeixinSendTool(deps),
		NewReachDouyinSendTool(deps),
		NewReachKuaishouSendTool(deps),
		NewReachXHSSendTool(deps),
		NewReachDingTalkSendTool(deps),
		NewReachTelegramSendTool(deps),
		NewReachWhatsAppSendTool(deps),
		NewReachFeishuSendTool(deps),
		NewReachWebSendTool(deps),
		NewReachCardSendTool(deps),
		NewReachBatchTool(deps),
		NewReachScheduleTool(deps),
		NewReachRecallTool(deps),
		NewReachHealthTool(deps),
		NewReachHistoryTool(deps),
		NewReachTemplateApplyTool(deps),
		NewReachAccountListTool(deps),
	}
}

func RegisterReachTools(registry *ToolRegistry, deps ReachToolDeps) error {
	tools := BuildReachTools(deps)
	for _, t := range tools {
		if err := registry.Register(t); err != nil {
			return fmt.Errorf("注册触达工具 %s 失败：%w", t.Name(), err)
		}
	}
	return nil
}

func MustRegisterReachTools(registry *ToolRegistry, deps ReachToolDeps) {
	if err := RegisterReachTools(registry, deps); err != nil {
		panic(err)
	}
}

func (t *ReachSMSSendTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if err := ValidateRequired(args, []string{"phone"}); err != nil {
		return ErrorResult(t.Name(), err), err
	}
	phone, _ := GetStringArg(args, "phone")
	content := getArgString(args, "content")
	templateID := getArgString(args, "template_id")
	params := getArgStringMap(args, "params")

	if content == "" && templateID == "" {
		return ErrorResult(t.Name(), errors.New("content 和 template_id 至少需要一个")), errors.New("content 和 template_id 至少需要一个")
	}

	req := &ReachSendRequest{
		Channel:     "sms",
		RecipientID: phone,
		Content:     content,
		TemplateID:  templateID,
		Params:      params,
	}
	if custID, ok := args["customer_id"]; ok {
		req.CustomerID, _ = custID.(string)
	}
	if opID, ok := args["operator_id"]; ok {
		req.OperatorID, _ = opID.(string)
	}

	msgID, resp, err := sendViaPipeline(ctx, t.deps, req)
	if err != nil {
		return ErrorResult(t.Name(), err), err
	}
	return SuccessResult(t.Name(), map[string]any{
		"phone":         phone,
		"message_id":    msgID,
		"channel":       "sms",
		"sent_at":       time.Now().Format(time.RFC3339),
		"step_results":  resp.StepResults,
		"retry_count":   resp.RetryCount,
		"fallback_used": resp.FallbackUsed,
	}), nil
}

func (t *ReachEmailSendTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if err := ValidateRequired(args, []string{"to", "subject", "content"}); err != nil {
		return ErrorResult(t.Name(), err), err
	}
	to, _ := GetStringArg(args, "to")
	subject, _ := GetStringArg(args, "subject")
	content, _ := GetStringArg(args, "content")
	attachments := getArgStringSlice(args, "attachments")

	req := &ReachSendRequest{
		Channel:     "email",
		RecipientID: to,
		Subject:     subject,
		Content:     content,
		Attachments: attachments,
	}
	if custID, ok := args["customer_id"]; ok {
		req.CustomerID, _ = custID.(string)
	}

	msgID, resp, err := sendViaPipeline(ctx, t.deps, req)
	if err != nil {
		return ErrorResult(t.Name(), err), err
	}
	return SuccessResult(t.Name(), map[string]any{
		"to":            to,
		"subject":       subject,
		"message_id":    msgID,
		"channel":       "email",
		"sent_at":       time.Now().Format(time.RFC3339),
		"step_results":  resp.StepResults,
		"retry_count":   resp.RetryCount,
		"fallback_used": resp.FallbackUsed,
	}), nil
}

func (t *ReachWeComSendTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if err := ValidateRequired(args, []string{"account_id", "external_user_id", "content"}); err != nil {
		return ErrorResult(t.Name(), err), err
	}
	accountID, _ := GetStringArg(args, "account_id")
	externalUserID, _ := GetStringArg(args, "external_user_id")
	msgType := getArgString(args, "msg_type")
	if msgType == "" {
		msgType = "text"
	}
	content, _ := GetStringArg(args, "content")

	msgID, resp, err := sendViaPipeline(ctx, t.deps, &ReachSendRequest{
		Channel:     "wecom",
		AccountID:   accountID,
		RecipientID: externalUserID,
		MsgType:     msgType,
		Content:     content,
	})
	if err != nil {
		return ErrorResult(t.Name(), err), err
	}
	return SuccessResult(t.Name(), map[string]any{
		"account_id":       accountID,
		"external_user_id": externalUserID,
		"msg_type":         msgType,
		"message_id":       msgID,
		"channel":          "wecom",
		"sent_at":          time.Now().Format(time.RFC3339),
		"step_results":     resp.StepResults,
		"retry_count":      resp.RetryCount,
		"fallback_used":    resp.FallbackUsed,
	}), nil
}

func (t *ReachWeixinSendTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if err := ValidateRequired(args, []string{"open_id", "content"}); err != nil {
		return ErrorResult(t.Name(), err), err
	}
	openID, _ := GetStringArg(args, "open_id")
	msgType := getArgString(args, "msg_type")
	if msgType == "" {
		msgType = "text"
	}
	content, _ := GetStringArg(args, "content")

	msgID, resp, err := sendViaPipeline(ctx, t.deps, &ReachSendRequest{
		Channel:     "weixin",
		RecipientID: openID,
		MsgType:     msgType,
		Content:     content,
	})
	if err != nil {
		return ErrorResult(t.Name(), err), err
	}
	return SuccessResult(t.Name(), map[string]any{
		"open_id":       openID,
		"msg_type":      msgType,
		"message_id":    msgID,
		"channel":       "weixin",
		"sent_at":       time.Now().Format(time.RFC3339),
		"step_results":  resp.StepResults,
		"retry_count":   resp.RetryCount,
		"fallback_used": resp.FallbackUsed,
	}), nil
}

func (t *ReachDouyinSendTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if err := ValidateRequired(args, []string{"account_id", "open_id", "content"}); err != nil {
		return ErrorResult(t.Name(), err), err
	}
	accountID, _ := GetStringArg(args, "account_id")
	openID, _ := GetStringArg(args, "open_id")
	msgType := getArgString(args, "msg_type")
	if msgType == "" {
		msgType = "text"
	}
	content, _ := GetStringArg(args, "content")

	msgID, resp, err := sendViaPipeline(ctx, t.deps, &ReachSendRequest{
		Channel:     "douyin",
		AccountID:   accountID,
		RecipientID: openID,
		MsgType:     msgType,
		Content:     content,
	})
	if err != nil {
		return ErrorResult(t.Name(), err), err
	}
	return SuccessResult(t.Name(), map[string]any{
		"account_id":    accountID,
		"open_id":       openID,
		"msg_type":      msgType,
		"message_id":    msgID,
		"channel":       "douyin",
		"sent_at":       time.Now().Format(time.RFC3339),
		"step_results":  resp.StepResults,
		"retry_count":   resp.RetryCount,
		"fallback_used": resp.FallbackUsed,
	}), nil
}

func (t *ReachKuaishouSendTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if err := ValidateRequired(args, []string{"account_id", "open_id", "content"}); err != nil {
		return ErrorResult(t.Name(), err), err
	}
	accountID, _ := GetStringArg(args, "account_id")
	openID, _ := GetStringArg(args, "open_id")
	msgType := getArgString(args, "msg_type")
	if msgType == "" {
		msgType = "text"
	}
	content, _ := GetStringArg(args, "content")

	msgID, resp, err := sendViaPipeline(ctx, t.deps, &ReachSendRequest{
		Channel:     "kuaishou",
		AccountID:   accountID,
		RecipientID: openID,
		MsgType:     msgType,
		Content:     content,
	})
	if err != nil {
		return ErrorResult(t.Name(), err), err
	}
	return SuccessResult(t.Name(), map[string]any{
		"account_id":    accountID,
		"open_id":       openID,
		"msg_type":      msgType,
		"message_id":    msgID,
		"channel":       "kuaishou",
		"sent_at":       time.Now().Format(time.RFC3339),
		"step_results":  resp.StepResults,
		"retry_count":   resp.RetryCount,
		"fallback_used": resp.FallbackUsed,
	}), nil
}

func (t *ReachXHSSendTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if err := ValidateRequired(args, []string{"account_id", "open_id", "content"}); err != nil {
		return ErrorResult(t.Name(), err), err
	}
	accountID, _ := GetStringArg(args, "account_id")
	openID, _ := GetStringArg(args, "open_id")
	msgType := getArgString(args, "msg_type")
	if msgType == "" {
		msgType = "text"
	}
	content, _ := GetStringArg(args, "content")

	msgID, resp, err := sendViaPipeline(ctx, t.deps, &ReachSendRequest{
		Channel:     "xhs",
		AccountID:   accountID,
		RecipientID: openID,
		MsgType:     msgType,
		Content:     content,
	})
	if err != nil {
		return ErrorResult(t.Name(), err), err
	}
	return SuccessResult(t.Name(), map[string]any{
		"account_id":    accountID,
		"open_id":       openID,
		"msg_type":      msgType,
		"message_id":    msgID,
		"channel":       "xiaohongshu",
		"sent_at":       time.Now().Format(time.RFC3339),
		"step_results":  resp.StepResults,
		"retry_count":   resp.RetryCount,
		"fallback_used": resp.FallbackUsed,
	}), nil
}

func (t *ReachDingTalkSendTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if err := ValidateRequired(args, []string{"chat_id", "content"}); err != nil {
		return ErrorResult(t.Name(), err), err
	}
	chatID, _ := GetStringArg(args, "chat_id")
	msgType := getArgString(args, "msg_type")
	if msgType == "" {
		msgType = "text"
	}
	content, _ := GetStringArg(args, "content")

	msgID, resp, err := sendViaPipeline(ctx, t.deps, &ReachSendRequest{
		Channel:     "dingtalk",
		RecipientID: chatID,
		MsgType:     msgType,
		Content:     content,
	})
	if err != nil {
		return ErrorResult(t.Name(), err), err
	}
	return SuccessResult(t.Name(), map[string]any{
		"chat_id":       chatID,
		"msg_type":      msgType,
		"message_id":    msgID,
		"channel":       "dingtalk",
		"sent_at":       time.Now().Format(time.RFC3339),
		"step_results":  resp.StepResults,
		"retry_count":   resp.RetryCount,
		"fallback_used": resp.FallbackUsed,
	}), nil
}

// ReachCardSendTool 客户卡片（多渠道）
type ReachCardSendTool struct {
	BaseTool
	deps ReachToolDeps
}

func NewReachCardSendTool(deps ReachToolDeps) *ReachCardSendTool {
	return &ReachCardSendTool{
		BaseTool: BaseTool{
			NameVal:        "reach.card.send",
			RiskVal:        RiskHighWrite,
			CategoryVal:    CategoryReach,
			DescriptionVal: "发送客户卡片（如抖音卡片、快手卡片）。支持指定渠道和卡片模板。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"channel": {
						Type:        "string",
						Description: "目标渠道",
						Enum:        []string{"douyin", "kuaishou", "wecom", "weixin"},
					},
					"account_id":       {Type: "string", Description: "发送账号 ID"},
					"external_user_id": {Type: "string", Description: "接收方 ID"},
					"card_id":          {Type: "string", Description: "卡片模板 ID"},
				},
				Required: []string{"channel", "account_id", "external_user_id", "card_id"},
			},
		},
		deps: deps,
	}
}

func (t *ReachCardSendTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if err := ValidateRequired(args, []string{"channel", "account_id", "external_user_id", "card_id"}); err != nil {
		return ErrorResult(t.Name(), err), err
	}
	channel, _ := GetStringArg(args, "channel")
	accountID, _ := GetStringArg(args, "account_id")
	externalUserID, _ := GetStringArg(args, "external_user_id")
	cardID, _ := GetStringArg(args, "card_id")

	req := &ReachSendRequest{
		Channel:     "card",
		AccountID:   accountID,
		RecipientID: externalUserID,
		CardID:      cardID,
		Metadata:    map[string]string{"subchannel": channel},
	}
	if custID, ok := args["customer_id"]; ok {
		req.CustomerID, _ = custID.(string)
	}
	if opID, ok := args["operator_id"]; ok {
		req.OperatorID, _ = opID.(string)
	}

	msgID, resp, err := sendViaPipeline(ctx, t.deps, req)
	if err != nil {
		return ErrorResult(t.Name(), err), err
	}
	return SuccessResult(t.Name(), map[string]any{
		"channel":          channel,
		"account_id":       accountID,
		"external_user_id": externalUserID,
		"card_id":          cardID,
		"message_id":       msgID,
		"sent_at":          time.Now().Format(time.RFC3339),
		"step_results":     resp.StepResults,
		"retry_count":      resp.RetryCount,
		"fallback_used":    resp.FallbackUsed,
	}), nil
}

// BatchSendItem 批量触达条目
type BatchSendItem struct {
	CustomerID string         `json:"customer_id"`
	AccountID  string         `json:"account_id,omitempty"`
	Payload    map[string]any `json:"payload"`
}

// ReachBatchTool 批量触达
type ReachBatchTool struct {
	BaseTool
	deps ReachToolDeps
}

func NewReachBatchTool(deps ReachToolDeps) *ReachBatchTool {
	return &ReachBatchTool{
		BaseTool: BaseTool{
			NameVal:        "reach.batch",
			RiskVal:        RiskHighWrite,
			CategoryVal:    CategoryReach,
			DescriptionVal: "通过触达 Pipeline 批量发送消息。需要指定 pipeline_id，会为每个 customer 创建一个 job。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"pipeline_id": {Type: "string", Description: "触达 Pipeline ID"},
					"channel": {
						Type:        "string",
						Description: "渠道（不填则用 pipeline 配置）",
						Enum:        []string{"sms", "email", "wecom", "weixin", "douyin", "kuaishou", "xiaohongshu", "dingtalk", "telegram", "whatsapp", "feishu", "card"},
					},
					"items": {
						Type:        "array",
						Description: "批量触达条目列表",
						Items: &ToolParam{
							Type: "object",
							Properties: map[string]ToolParam{
								"customer_id": {Type: "string", Description: "客户 ID"},
								"account_id":  {Type: "string", Description: "账号 ID（可选）"},
								"payload":     {Type: "object", Description: "消息载荷"},
							},
						},
					},
				},
				Required: []string{"pipeline_id", "items"},
			},
		},
		deps: deps,
	}
}

func (t *ReachBatchTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if t.deps.Pipeline == nil {
		return ErrorResult(t.Name(), errors.New("batch 工具需要 Pipeline 依赖")), errors.New("batch 工具需要 Pipeline 依赖")
	}
	if err := ValidateRequired(args, []string{"pipeline_id", "items"}); err != nil {
		return ErrorResult(t.Name(), err), err
	}
	pipelineIDStr, _ := GetStringArg(args, "pipeline_id")
	pipelineID := parseUint(pipelineIDStr)
	channel := getArgString(args, "channel")

	items := getArgInterfaceSlice(args, "items")
	if len(items) == 0 {
		return ErrorResult(t.Name(), errors.New("items 不能为空")), errors.New("items 不能为空")
	}

	jobIDs := make([]string, 0, len(items))
	failed := make([]string, 0)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, item := range items {
		itemMap, ok := item.(map[string]any)
		if !ok {
			failed = append(failed, "invalid item")
			continue
		}
		customerID := getArgString(itemMap, "customer_id")
		accountID := getArgString(itemMap, "account_id")

		payload := getArgMap(itemMap, "payload")
		if customerID == "" {
			failed = append(failed, "missing customer_id")
			continue
		}
		wg.Add(1)
		go func(cid, aid string, p map[string]any) {
			defer wg.Done()
			req := &ReachJobRequest{
				PipelineID: pipelineID,
				Channel:    channel,
				CustomerID: cid,
				AccountID:  aid,
				Payload:    p,
			}
			job, err := t.deps.Pipeline.EnqueueJob(ctx, req)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed = append(failed, fmt.Sprintf("%s: %v", cid, err))
				return
			}
			jobIDs = append(jobIDs, fmt.Sprintf("%d", job.ID))
		}(customerID, accountID, payload)
	}
	wg.Wait()

	success := len(jobIDs)
	return SuccessResult(t.Name(), map[string]any{
		"pipeline_id":    pipelineID,
		"total":          len(items),
		"success_count":  success,
		"failed_count":   len(failed),
		"job_ids":        jobIDs,
		"failed_details": failed,
	}), nil
}

// ReachScheduleTool 定时触达
type ReachScheduleTool struct {
	BaseTool
	deps ReachToolDeps
}

func NewReachScheduleTool(deps ReachToolDeps) *ReachScheduleTool {
	return &ReachScheduleTool{
		BaseTool: BaseTool{
			NameVal:        "reach.schedule",
			RiskVal:        RiskHighWrite,
			CategoryVal:    CategoryReach,
			DescriptionVal: "定时触发触达任务。通过 Pipeline 在指定时间执行。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"pipeline_id": {Type: "string", Description: "触达 Pipeline ID"},
					"channel":     {Type: "string", Description: "渠道"},
					"customer_id": {Type: "string", Description: "客户 ID"},
					"account_id":  {Type: "string", Description: "账号 ID"},
					"run_at":      {Type: "string", Description: "执行时间（RFC3339 格式）"},
					"payload":     {Type: "object", Description: "消息载荷"},
				},
				Required: []string{"pipeline_id", "customer_id", "run_at", "payload"},
			},
		},
		deps: deps,
	}
}

func (t *ReachScheduleTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if t.deps.Pipeline == nil {
		return ErrorResult(t.Name(), errors.New("schedule 工具需要 Pipeline 依赖")), errors.New("schedule 工具需要 Pipeline 依赖")
	}
	if err := ValidateRequired(args, []string{"pipeline_id", "customer_id", "run_at", "payload"}); err != nil {
		return ErrorResult(t.Name(), err), err
	}
	pipelineIDStr, _ := GetStringArg(args, "pipeline_id")
	pipelineID := parseUint(pipelineIDStr)
	channel := getArgString(args, "channel")
	customerID, _ := GetStringArg(args, "customer_id")
	accountID := getArgString(args, "account_id")
	runAtStr, _ := GetStringArg(args, "run_at")
	payload := getArgStringMap(args, "payload")

	runAt, err := time.Parse(time.RFC3339, runAtStr)
	if err != nil {
		return ErrorResult(t.Name(), fmt.Errorf("run_at 格式错误：%w", err)), fmt.Errorf("run_at 格式错误：%w", err)
	}
	if runAt.Before(time.Now()) {
		return ErrorResult(t.Name(), errors.New("run_at 不能早于当前时间")), errors.New("run_at 不能早于当前时间")
	}

	payloadMap := map[string]any{}
	for k, v := range payload {
		payloadMap[k] = v
	}

	req := &ReachJobRequest{
		PipelineID: pipelineID,
		Channel:    channel,
		CustomerID: customerID,
		AccountID:  accountID,
		Payload:    payloadMap,
		RunAt:      &runAt,
	}
	job, err := t.deps.Pipeline.EnqueueJob(ctx, req)
	if err != nil {
		return ErrorResult(t.Name(), err), err
	}
	return SuccessResult(t.Name(), map[string]any{
		"job_id":      job.ID,
		"pipeline_id": pipelineID,
		"customer_id": customerID,
		"channel":     channel,
		"run_at":      runAt.Format(time.RFC3339),
		"state":       job.State,
	}), nil
}

// ReachRecallTool 撤回消息
type ReachRecallTool struct {
	BaseTool
	deps ReachToolDeps
}

func NewReachRecallTool(deps ReachToolDeps) *ReachRecallTool {
	return &ReachRecallTool{
		BaseTool: BaseTool{
			NameVal:        "reach.recall",
			RiskVal:        RiskLowWrite,
			CategoryVal:    CategoryReach,
			DescriptionVal: "撤回已发送的消息。并非所有渠道都支持撤回。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"channel": {
						Type:        "string",
						Description: "渠道",
						Enum:        []string{"sms", "email", "wecom", "weixin", "douyin", "kuaishou", "xiaohongshu", "dingtalk", "telegram", "whatsapp", "feishu", "card"},
					},
					"message_id": {Type: "string", Description: "消息 ID"},
				},
				Required: []string{"channel", "message_id"},
			},
		},
		deps: deps,
	}
}

func (t *ReachRecallTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if err := ValidateRequired(args, []string{"channel", "message_id"}); err != nil {
		return ErrorResult(t.Name(), err), err
	}
	channel, _ := GetStringArg(args, "channel")
	msgID, _ := GetStringArg(args, "message_id")

	if err := t.deps.Adapter.Recall(ctx, channel, msgID); err != nil {
		return ErrorResult(t.Name(), err), err
	}
	return SuccessResult(t.Name(), map[string]any{
		"channel":     channel,
		"message_id":  msgID,
		"recalled":    true,
		"recalled_at": time.Now().Format(time.RFC3339),
	}), nil
}

// ReachHealthTool 账号健康度查询
type ReachHealthTool struct {
	BaseTool
	deps ReachToolDeps
}

func NewReachHealthTool(deps ReachToolDeps) *ReachHealthTool {
	return &ReachHealthTool{
		BaseTool: BaseTool{
			NameVal:        "reach.health",
			RiskVal:        RiskReadonly,
			CategoryVal:    CategoryReach,
			DescriptionVal: "查询账号健康度。返回账号状态、剩余配额、风险等级等信息。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"channel": {
						Type:        "string",
						Description: "渠道",
						Enum:        []string{"sms", "email", "wecom", "weixin", "douyin", "kuaishou", "xiaohongshu", "dingtalk", "telegram", "whatsapp", "feishu", "card"},
					},
					"account_id": {Type: "string", Description: "账号 ID（不填则查询渠道下所有账号摘要）"},
				},
				Required: []string{"channel"},
			},
		},
		deps: deps,
	}
}

func (t *ReachHealthTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if err := ValidateRequired(args, []string{"channel"}); err != nil {
		return ErrorResult(t.Name(), err), err
	}
	channel, _ := GetStringArg(args, "channel")
	accountID := getArgString(args, "account_id")

	info, err := t.deps.Adapter.AccountHealth(ctx, channel, accountID)
	if err != nil {
		return ErrorResult(t.Name(), err), err
	}
	return SuccessResult(t.Name(), info), nil
}

// ReachHistoryTool 触达历史查询
type ReachHistoryTool struct {
	BaseTool
	deps ReachToolDeps
}

func NewReachHistoryTool(deps ReachToolDeps) *ReachHistoryTool {
	return &ReachHistoryTool{
		BaseTool: BaseTool{
			NameVal:        "reach.history",
			RiskVal:        RiskReadonly,
			CategoryVal:    CategoryReach,
			DescriptionVal: "查询触达历史。可按渠道、客户 ID、状态筛选。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"channel":     {Type: "string", Description: "渠道（可选）"},
					"customer_id": {Type: "string", Description: "客户 ID（可选）"},
					"state": {
						Type:        "string",
						Description: "任务状态",
						Enum:        []string{"pending", "running", "success", "failed", "canceled", "retrying", "rate_limited"},
					},
					"page":      {Type: "integer", Description: "页码（默认 1）", Default: 1},
					"page_size": {Type: "integer", Description: "每页数量（默认 20）", Default: 20},
				},
			},
		},
		deps: deps,
	}
}

func (t *ReachHistoryTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if t.deps.Pipeline == nil {
		return ErrorResult(t.Name(), errors.New("history 工具需要 Pipeline 依赖")), errors.New("history 工具需要 Pipeline 依赖")
	}
	channel := getArgString(args, "channel")
	state := getArgString(args, "state")
	page, _ := GetIntArg(args, "page")
	if page <= 0 {
		page = 1
	}
	pageSize, _ := GetIntArg(args, "page_size")
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}

	jobs, total, err := t.deps.Pipeline.ListJobs(ctx, channel, state, page, pageSize)
	if err != nil {
		return ErrorResult(t.Name(), err), err
	}
	totalPages := int64(0)
	if pageSize > 0 {
		totalPages = (total + int64(pageSize) - 1) / int64(pageSize)
	}
	return SuccessResult(t.Name(), map[string]any{
		"jobs":        jobs,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": totalPages,
	}), nil
}

// ReachTemplateApplyTool 模板应用
type ReachTemplateApplyTool struct {
	BaseTool
	deps ReachToolDeps
}

func NewReachTemplateApplyTool(deps ReachToolDeps) *ReachTemplateApplyTool {
	return &ReachTemplateApplyTool{
		BaseTool: BaseTool{
			NameVal:        "reach.template.apply",
			RiskVal:        RiskReadonly,
			CategoryVal:    CategoryReach,
			DescriptionVal: "应用模板参数生成最终消息内容。支持 {{var}} 占位符替换。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"template": {Type: "string", Description: "模板内容（含 {{var}} 占位符）"},
					"params": {
						Type:        "object",
						Description: "替换参数（key-value）",
						Properties:  map[string]ToolParam{},
					},
				},
				Required: []string{"template", "params"},
			},
		},
		deps: deps,
	}
}

func (t *ReachTemplateApplyTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if err := ValidateRequired(args, []string{"template", "params"}); err != nil {
		return ErrorResult(t.Name(), err), err
	}
	tmpl, _ := GetStringArg(args, "template")
	params := getArgStringMap(args, "params")

	result := tmpl
	for k, v := range params {
		placeholder := fmt.Sprintf("{{%s}}", k)
		result = strings.ReplaceAll(result, placeholder, v)

		placeholder2 := fmt.Sprintf("{{ %s }}", k)
		result = strings.ReplaceAll(result, placeholder2, v)
	}

	remaining := findUnreplacedPlaceholders(result)
	return SuccessResult(t.Name(), map[string]any{
		"template":                tmpl,
		"params":                  params,
		"content":                 result,
		"unreplaced_placeholders": remaining,
	}), nil
}

func findUnreplacedPlaceholders(s string) []string {
	var placeholders []string
	i := 0
	for i < len(s) {
		if s[i] == '{' && i+1 < len(s) && s[i+1] == '{' {

			end := strings.Index(s[i+2:], "}}")
			if end < 0 {
				break
			}
			placeholder := strings.TrimSpace(s[i+2 : i+2+end])
			placeholders = append(placeholders, placeholder)
			i = i + 2 + end + 2
		} else {
			i++
		}
	}
	return placeholders
}

// ReachAccountListTool 账号列表查询
type ReachAccountListTool struct {
	BaseTool
	deps ReachToolDeps
}

func NewReachAccountListTool(deps ReachToolDeps) *ReachAccountListTool {
	return &ReachAccountListTool{
		BaseTool: BaseTool{
			NameVal:        "reach.account.list",
			RiskVal:        RiskReadonly,
			CategoryVal:    CategoryReach,
			DescriptionVal: "查询指定渠道下的可用账号列表（含健康状态）。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"channel": {
						Type:        "string",
						Description: "渠道（不填则返回所有渠道）",
						Enum:        []string{"sms", "email", "wecom", "weixin", "douyin", "kuaishou", "xiaohongshu", "dingtalk", "telegram", "whatsapp", "feishu", "card"},
					},
				},
			},
		},
		deps: deps,
	}
}

func (t *ReachAccountListTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	channel := getArgString(args, "channel")

	accounts, err := t.deps.Adapter.ListAccounts(ctx, channel)
	if err != nil {
		return ErrorResult(t.Name(), err), err
	}

	healthy := 0
	for _, a := range accounts {
		if a.IsHealthy {
			healthy++
		}
	}
	return SuccessResult(t.Name(), map[string]any{
		"channel":       channel,
		"accounts":      accounts,
		"total":         len(accounts),
		"healthy_count": healthy,
	}), nil
}

func (t *ReachTelegramSendTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if err := ValidateRequired(args, []string{"account_id", "chat_id", "content"}); err != nil {
		return ErrorResult(t.Name(), err), err
	}
	accountID, _ := GetStringArg(args, "account_id")
	chatID, _ := GetStringArg(args, "chat_id")
	content, _ := GetStringArg(args, "content")
	if content == "" {
		return ErrorResult(t.Name(), errors.New("content 不能为空")), errors.New("content 不能为空")
	}

	req := &ReachSendRequest{
		Channel:     "telegram",
		AccountID:   accountID,
		RecipientID: chatID,
		Content:     content,
	}
	if custID, ok := args["customer_id"]; ok {
		req.CustomerID, _ = custID.(string)
	}
	if opID, ok := args["operator_id"]; ok {
		req.OperatorID, _ = opID.(string)
	}

	msgID, resp, err := sendViaPipeline(ctx, t.deps, req)
	if err != nil {
		return ErrorResult(t.Name(), err), err
	}
	return SuccessResult(t.Name(), map[string]any{
		"account_id":    accountID,
		"chat_id":       chatID,
		"message_id":    msgID,
		"channel":       "telegram",
		"sent_at":       time.Now().Format(time.RFC3339),
		"step_results":  resp.StepResults,
		"retry_count":   resp.RetryCount,
		"fallback_used": resp.FallbackUsed,
	}), nil
}

func (t *ReachWhatsAppSendTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if err := ValidateRequired(args, []string{"account_id", "to_phone", "content"}); err != nil {
		return ErrorResult(t.Name(), err), err
	}
	accountID, _ := GetStringArg(args, "account_id")
	toPhone, _ := GetStringArg(args, "to_phone")
	content, _ := GetStringArg(args, "content")
	templateID := getArgString(args, "template_id")
	params := getArgStringMap(args, "params")
	if content == "" && templateID == "" {
		return ErrorResult(t.Name(), errors.New("content 和 template_id 至少需要一个")), errors.New("content 和 template_id 至少需要一个")
	}

	req := &ReachSendRequest{
		Channel:     "whatsapp",
		AccountID:   accountID,
		RecipientID: toPhone,
		Content:     content,
		TemplateID:  templateID,
		Params:      params,
	}
	if custID, ok := args["customer_id"]; ok {
		req.CustomerID, _ = custID.(string)
	}
	if opID, ok := args["operator_id"]; ok {
		req.OperatorID, _ = opID.(string)
	}

	msgID, resp, err := sendViaPipeline(ctx, t.deps, req)
	if err != nil {
		return ErrorResult(t.Name(), err), err
	}
	return SuccessResult(t.Name(), map[string]any{
		"account_id":    accountID,
		"to_phone":      toPhone,
		"template_id":   templateID,
		"message_id":    msgID,
		"channel":       "whatsapp",
		"sent_at":       time.Now().Format(time.RFC3339),
		"step_results":  resp.StepResults,
		"retry_count":   resp.RetryCount,
		"fallback_used": resp.FallbackUsed,
	}), nil
}

func (t *ReachFeishuSendTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if err := ValidateRequired(args, []string{"account_id", "open_id", "content"}); err != nil {
		return ErrorResult(t.Name(), err), err
	}
	accountID, _ := GetStringArg(args, "account_id")
	openID, _ := GetStringArg(args, "open_id")
	content, _ := GetStringArg(args, "content")
	msgType := getArgString(args, "msg_type")
	if msgType == "" {
		msgType = "text"
	}
	if content == "" {
		return ErrorResult(t.Name(), errors.New("content 不能为空")), errors.New("content 不能为空")
	}

	req := &ReachSendRequest{
		Channel:     "feishu",
		AccountID:   accountID,
		RecipientID: openID,
		MsgType:     msgType,
		Content:     content,
	}
	if custID, ok := args["customer_id"]; ok {
		req.CustomerID, _ = custID.(string)
	}
	if opID, ok := args["operator_id"]; ok {
		req.OperatorID, _ = opID.(string)
	}

	msgID, resp, err := sendViaPipeline(ctx, t.deps, req)
	if err != nil {
		return ErrorResult(t.Name(), err), err
	}
	return SuccessResult(t.Name(), map[string]any{
		"account_id":    accountID,
		"open_id":       openID,
		"msg_type":      msgType,
		"message_id":    msgID,
		"channel":       "feishu",
		"sent_at":       time.Now().Format(time.RFC3339),
		"step_results":  resp.StepResults,
		"retry_count":   resp.RetryCount,
		"fallback_used": resp.FallbackUsed,
	}), nil
}

func (t *ReachWebSendTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if err := ValidateRequired(args, []string{"session_id", "content"}); err != nil {
		return ErrorResult(t.Name(), err), err
	}
	sessionID, _ := GetStringArg(args, "session_id")
	content, _ := GetStringArg(args, "content")

	req := &ReachSendRequest{
		Channel:     "web",
		RecipientID: sessionID,
		Content:     content,
	}
	if custID, ok := args["customer_id"]; ok {
		req.CustomerID, _ = custID.(string)
	}

	msgID, resp, err := sendViaPipeline(ctx, t.deps, req)
	if err != nil {
		return ErrorResult(t.Name(), err), err
	}
	return SuccessResult(t.Name(), map[string]any{
		"session_id":    sessionID,
		"message_id":    msgID,
		"channel":       "web",
		"sent_at":       time.Now().Format(time.RFC3339),
		"step_results":  resp.StepResults,
		"retry_count":   resp.RetryCount,
		"fallback_used": resp.FallbackUsed,
	}), nil
}
