package service

import (
	"context"
	"net"
	"net/url"
	"os"

	"encoding/json"

	"errors"

	"fmt"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/content/model"

	reachmodel "hivemtk-user/internal/model"

	"strconv"

	"strings"

	"time"

	"bytes"
	"hivemtk-user/internal/pkg/urlguard"
	_type "hivemtk-user/internal/pkg/utils/type"
	"io"
	"net/http"

	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
)

func (s *MarketingFlowService) executeAction(ctx context.Context, node model.FlowNode, userID string, data map[string]any) (map[string]any, error) {
	actionType, ok := node.Config["action_type"].(string)
	if !ok {
		return nil, errors.New("动作类型未指定")
	}

	switch model.ActionType(actionType) {
	case model.ActionTypeSendMessage:
		return s.sendActionSendMessage(ctx, node.Config, userID, data)
	case model.ActionTypeAddTag:
		return s.sendActionAddTag(ctx, node.Config, userID, data)
	case model.ActionTypeRemoveTag:
		return s.sendActionRemoveTag(ctx, node.Config, userID, data)
	case model.ActionTypeAssignAgent:
		return s.sendActionAssignAgent(ctx, node.Config, userID, data)
	case model.ActionTypeCreateTask:
		return s.sendActionCreateTask(ctx, node.Config, userID, data)
	case model.ActionTypeWebhook:
		return s.sendActionWebhook(ctx, node.Config, userID, data)
	case model.ActionTypeSendEmail:
		return s.sendActionSendEmail(ctx, node.Config, userID, data)
	case model.ActionTypeSendSms:
		return s.sendActionSendSms(ctx, node.Config, userID, data)
	case model.ActionTypeUpdateLead:
		return s.sendActionUpdateLead(ctx, node.Config, userID, data)
	case model.ActionTypeCreateOrder:
		return s.sendActionCreateOrder(ctx, node.Config, userID, data)
	default:
		return nil, fmt.Errorf("未知的动作类型：%s", actionType)
	}
}

func (s *MarketingFlowService) sendActionSendMessage(ctx context.Context, config map[string]any, userID string, data map[string]any) (map[string]any, error) {

	platformName, _ := config["platform"].(string)
	accountID, _ := config["account_id"].(string)
	chatID, _ := config["chat_id"].(string)
	content, _ := config["content"].(string)

	if platformName == "" {
		return nil, errors.New("platform 未指定")
	}
	if accountID == "" {
		return nil, errors.New("account_id 未指定")
	}
	if chatID == "" {
		return nil, errors.New("chat_id 未指定")
	}
	if content == "" {
		return nil, errors.New("content 未指定")
	}

	return nil, fmt.Errorf("平台 %s 不支持服务端无头发送（CDP 自动回复通道已移除，请通过桥接模块下发）", platformName)
}

func (s *MarketingFlowService) sendActionAddTag(ctx context.Context, config map[string]any, userID string, data map[string]any) (map[string]any, error) {

	tagsRaw, ok := config["tags"].([]any)
	if !ok {
		return nil, errors.New("tags 配置格式错误")
	}

	tagSet := make(map[string]bool)
	for _, tag := range tagsRaw {
		if tagName, ok := tag.(string); ok && tagName != "" {
			tagSet[tagName] = true
		}
	}

	if len(tagSet) == 0 {
		return nil, errors.New("没有有效的标签")
	}

	var tagNames []string
	for tagName := range tagSet {
		tagNames = append(tagNames, tagName)
	}

	if err := s.userTagRepo.AddTags(ctx, userID, tagNames); err != nil {
		return nil, fmt.Errorf("添加标签失败：%w", err)
	}

	return map[string]any{
		"success":    true,
		"added_tags": tagNames,
		"user_id":    userID,
	}, nil
}

func (s *MarketingFlowService) sendActionRemoveTag(ctx context.Context, config map[string]any, userID string, data map[string]any) (map[string]any, error) {
	if s.userTagRepo == nil {
		return nil, errors.New("用户标签仓库未初始化")
	}
	if userID == "" {
		return nil, errors.New("user_id 未指定")
	}

	var tagNames []string
	switch tagsRaw := config["tags"].(type) {
	case []any:
		seen := make(map[string]bool)
		for _, tag := range tagsRaw {
			if tagName, ok := tag.(string); ok && tagName != "" && !seen[tagName] {
				seen[tagName] = true
				tagNames = append(tagNames, tagName)
			}
		}
	case []string:
		seen := make(map[string]bool)
		for _, tagName := range tagsRaw {
			if tagName != "" && !seen[tagName] {
				seen[tagName] = true
				tagNames = append(tagNames, tagName)
			}
		}
	default:
		return nil, errors.New("tags 配置格式错误，应为字符串数组")
	}

	if len(tagNames) == 0 {
		return nil, errors.New("没有有效的待移除标签")
	}

	if err := s.userTagRepo.RemoveTags(ctx, userID, tagNames); err != nil {
		return nil, fmt.Errorf("移除标签失败：%w", err)
	}

	return map[string]any{
		"success":      true,
		"removed_tags": tagNames,
		"user_id":      userID,
	}, nil
}

func (s *MarketingFlowService) sendActionAssignAgent(ctx context.Context, config map[string]any, userID string, data map[string]any) (map[string]any, error) {
	if s.sessionRepo == nil {
		return nil, errors.New("会话仓库未初始化")
	}
	if s.agentRepo == nil {
		return nil, errors.New("客服状态仓库未初始化")
	}

	var sessionID uint
	var sessionUserID string

	if sid, ok := config["session_id"].(string); ok && sid != "" {
		id, err := strconv.ParseUint(sid, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("session_id 格式无效：%w", err)
		}
		sessionID = uint(id)
	} else {

		if sidRaw, ok := data["session_id"]; ok {
			switch v := sidRaw.(type) {
			case string:
				if id, err := strconv.ParseUint(v, 10, 64); err == nil {
					sessionID = uint(id)
				}
			case float64:
				sessionID = uint(v)
			}
		}

		if sessionID == 0 {
			if userID == "" {
				return nil, errors.New("未指定 session_id 且 user_id 为空，无法定位会话")
			}
			session, err := s.sessionRepo.GetActiveByUserID(ctx, userID)
			if err != nil {
				return nil, fmt.Errorf("未找到用户的活跃会话：%w", err)
			}
			sessionID = session.ID
			sessionUserID = session.UserID
		}
	}

	var agentID uint
	var agentName string

	if aidRaw, ok := config["agent_id"]; ok {
		switch v := aidRaw.(type) {
		case float64:
			agentID = uint(v)
		case string:
			if v != "" {
				id, err := strconv.ParseUint(v, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("agent_id 格式无效：%w", err)
				}
				agentID = uint(id)
			}
		}
	}

	if agentID == 0 {
		agents, err := s.agentRepo.GetOnlineAgents(ctx)
		if err != nil {
			return nil, fmt.Errorf("获取在线客服失败：%w", err)
		}
		if len(agents) == 0 {
			return nil, errors.New("当前没有可用的在线客服")
		}

		agentID = agents[0].AgentID
		agentName = agents[0].AgentName
	} else {

		status, err := s.agentRepo.GetByAgentID(ctx, agentID)
		if err != nil {
			return nil, fmt.Errorf("查询客服状态失败：%w", err)
		}
		agentName = status.AgentName
		if status.Status != "online" && status.Status != "busy" {
			return nil, fmt.Errorf("客服 %d 当前状态为 %s，无法分配", agentID, status.Status)
		}
		if status.ActiveSessions >= status.MaxSessions {
			return nil, fmt.Errorf("客服 %d 当前活跃会话数已达上限 %d", agentID, status.MaxSessions)
		}
	}

	if err := s.sessionRepo.AssignAgent(ctx, sessionID, agentID, agentName); err != nil {
		return nil, fmt.Errorf("分配客服失败：%w", err)
	}

	utils.WarnErrKV("marketing.flow.IncrementActiveSessions",
		s.agentRepo.IncrementActiveSessions(ctx, agentID),
		"agent_id", strconv.FormatUint(uint64(agentID), 10))

	return map[string]any{
		"success":    true,
		"session_id": sessionID,
		"agent_id":   agentID,
		"agent_name": agentName,
		"user_id":    firstNonEmpty(sessionUserID, userID),
	}, nil
}

func (s *MarketingFlowService) sendActionCreateTask(ctx context.Context, config map[string]any, userID string, data map[string]any) (map[string]any, error) {
	if s.operationLogRepo == nil {
		return nil, errors.New("操作日志仓库未初始化")
	}

	title, _ := config["title"].(string)
	if title == "" {
		return nil, errors.New("task title 未指定")
	}

	description, _ := config["description"].(string)
	module, _ := config["module"].(string)
	if module == "" {
		module = "marketing_flow"
	}
	resourceID, _ := config["resource_id"].(string)

	var assigneeID uint
	if assigneeRaw, ok := config["assignee_id"]; ok {
		switch v := assigneeRaw.(type) {
		case float64:
			assigneeID = uint(v)
		case string:
			if v != "" {
				if id, err := strconv.ParseUint(v, 10, 64); err == nil {
					assigneeID = uint(id)
				}
			}
		}
	}

	if assigneeID == 0 {
		if uid, err := strconv.ParseUint(userID, 10, 64); err == nil {
			assigneeID = uint(uid)
		}
	}
	if assigneeID == 0 {
		return nil, errors.New("无法确定任务责任人：assignee_id 与 user_id 均无效")
	}

	detailMap := map[string]any{
		"title":       title,
		"description": description,
		"user_id":     userID,
		"source":      "marketing_flow",
	}
	for k, v := range data {

		if !strings.HasPrefix(k, "_") {
			detailMap[k] = v
		}
	}
	detailJSON, err := json.Marshal(detailMap)
	if err != nil {
		return nil, fmt.Errorf("任务详情序列化失败：%w", err)
	}

	logEntry := &reachmodel.OperationLog{
		UserID:     assigneeID,
		Action:     "create",
		Module:     module,
		Resource:   "task",
		ResourceID: resourceID,
		Detail:     string(detailJSON),
		NewValue:   title,
	}

	if err := s.operationLogRepo.Create(ctx, logEntry); err != nil {
		return nil, fmt.Errorf("创建任务失败：%w", err)
	}

	return map[string]any{
		"success":     true,
		"task_id":     logEntry.ID,
		"title":       title,
		"assignee_id": assigneeID,
		"resource_id": resourceID,
	}, nil
}

func (s *MarketingFlowService) sendActionSendSms(ctx context.Context, config map[string]any, userID string, data map[string]any) (map[string]any, error) {
	// FA-20：发送前先复核流程状态（撤回/暂停对在途执行也生效），再交给注册的发送器
	// ——注册实现（internal/service）里还有统一判定链（DNC→审批→合规）。
	if err := s.ensureFlowActiveAtSend(ctx); err != nil {
		return nil, err
	}
	if smsSenderFunc == nil {
		return nil, errors.New("短信发送器未注册，请确认 internal/service 包已调用 SetSmsSender")
	}

	phone, _ := config["phone"].(string)
	if phone == "" {

		if p, ok := data["phone"].(string); ok {
			phone = p
		} else if p, ok := data["user_phone"].(string); ok {
			phone = p
		}
	}
	if phone == "" {
		return nil, errors.New("phone 未指定")
	}

	content, _ := config["content"].(string)
	if content == "" {
		return nil, errors.New("content 未指定")
	}

	if err := smsSenderFunc(ctx, phone, content); err != nil {
		return nil, fmt.Errorf("发送短信失败：%w", err)
	}

	return map[string]any{
		"success": true,
		"phone":   phone,
		"content": content,
		"user_id": userID,
	}, nil
}

func (s *MarketingFlowService) sendActionUpdateLead(ctx context.Context, config map[string]any, userID string, data map[string]any) (map[string]any, error) {
	if s.clueRepo == nil {
		return nil, errors.New("线索仓库未初始化")
	}

	clueID, _ := config["clue_id"].(string)
	if clueID == "" {
		if id, ok := data["clue_id"].(string); ok {
			clueID = id
		} else if id, ok := data["lead_id"].(string); ok {
			clueID = id
		}
	}
	if clueID == "" {
		return nil, errors.New("clue_id 未指定")
	}

	fieldsRaw, ok := config["fields"].(map[string]any)
	if !ok || len(fieldsRaw) == 0 {
		return nil, errors.New("fields 配置格式错误或为空")
	}

	allowedFields := map[string]bool{
		"name":      true,
		"city":      true,
		"address":   true,
		"desc":      true,
		"is_verify": true,
		"type":      true,
		"source_id": true,
		"account":   true,
	}
	updates := make(map[string]any)
	for k, v := range fieldsRaw {
		if !allowedFields[k] {
			continue
		}

		if k == "is_verify" || k == "type" {
			switch vv := v.(type) {
			case float64:
				updates[k] = int64(vv)
			case int:
				updates[k] = int64(vv)
			case int64:
				updates[k] = vv
			case string:
				if n, err := strconv.ParseInt(vv, 10, 64); err == nil {
					updates[k] = n
				}
			default:
				return nil, fmt.Errorf("字段 %s 的值类型不合法", k)
			}
			continue
		}
		updates[k] = v
	}

	if len(updates) == 0 {
		return nil, errors.New("没有有效的可更新字段（请检查字段名是否在白名单内）")
	}

	if err := s.clueRepo.UpdateByID(ctx, clueID, updates); err != nil {
		return nil, fmt.Errorf("更新线索失败：%w", err)
	}

	return map[string]any{
		"success": true,
		"clue_id": clueID,
		"updates": updates,
		"user_id": userID,
	}, nil
}

// sendActionCreateOrder 由营销流程触发创建一笔销售/订单记录（草稿态）。
//
// EC-31 / M31 边界（事实写入与营销链路解耦）：
//   - 本函数创建的订单仅是营销链路产出的「销售草稿/待支付单」，不是已付款事实；
//     任何收入、净额归因、漏斗转化都必须以真实支付/退款事件为准，不得把此处创建的
//     pending 订单当作已成交；
//   - 营销链路不得直接写入支付成功、退款成功等资金事实；资金事实必须由订单/支付/
//     退款链路独立写入并回查，营销仅消费已确认的事实做归因；
//   - 退款扣减必须按真实退款事件只扣一次，重复回调/并发撤回不得重复扣减；
//   - 无回执/未知状态一律保留 unknown，不得把点击/打开/草稿冒充因果增长。
func (s *MarketingFlowService) sendActionCreateOrder(ctx context.Context, config map[string]any, userID string, data map[string]any) (map[string]any, error) {
	if s.orderRepo == nil {
		return nil, errors.New("订单仓库未初始化")
	}

	price, _ := config["price"].(string)
	if price == "" {

		if p, ok := config["price"].(float64); ok {
			price = strconv.FormatFloat(p, 'f', -1, 64)
		}
	}
	if price == "" {
		return nil, errors.New("price 未指定")
	}

	var tgID int64
	switch v := config["tg_id"].(type) {
	case float64:
		tgID = int64(v)
	case string:
		if v != "" {
			id, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("tg_id 格式无效：%w", err)
			}
			tgID = id
		}
	}
	if tgID == 0 {

		if v, ok := data["tg_id"].(float64); ok {
			tgID = int64(v)
		} else if v, ok := data["tg_id"].(string); ok && v != "" {
			if id, err := strconv.ParseInt(v, 10, 64); err == nil {
				tgID = id
			}
		}
	}
	if tgID == 0 {
		return nil, errors.New("tg_id 未指定")
	}

	accountID, _ := config["account_id"].(string)
	if accountID == "" {
		if v, ok := data["account_id"].(string); ok {
			accountID = v
		}
	}
	if accountID == "" {
		return nil, errors.New("account_id 未指定")
	}

	var statusInt int
	switch v := config["status"].(type) {
	case float64:
		statusInt = int(v)
	case string:
		if v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				statusInt = n
			}
		}
	}

	order := &reachmodel.Order{
		Price:     price,
		TgID:      tgID,
		AccountID: accountID,
		Status:    _type.OrderStatusType(statusInt),
	}

	if err := s.orderRepo.Create(ctx, order); err != nil {
		return nil, fmt.Errorf("创建订单失败：%w", err)
	}

	return map[string]any{
		"success":    true,
		"order_id":   order.ID,
		"price":      price,
		"tg_id":      tgID,
		"account_id": accountID,
		"status":     int(order.Status),
		"user_id":    userID,
	}, nil
}

func (s *MarketingFlowService) sendActionWebhook(ctx context.Context, config map[string]any, userID string, data map[string]any) (map[string]any, error) {
	url, ok := config["url"].(string)
	if !ok || url == "" {
		return nil, errors.New("webhook URL 未指定")
	}
	if err := validateWebhookURL(url); err != nil {
		return nil, err
	}

	method, _ := config["method"].(string)
	if method == "" {
		method = "POST"
	}

	var bodyReader io.Reader
	if len(data) > 0 {
		body, err := json.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("JSON 序列化失败：%w", err)
		}
		bodyReader = bytes.NewBuffer(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("创建 HTTP 请求失败：%w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Marketing-Flow-Webhook/1.0")

	if headers, ok := config["headers"].(map[string]any); ok {
		for k, v := range headers {
			if strVal, ok := v.(string); ok {
				req.Header.Set(k, strVal)
			}
		}
	}

	req.Header.Set("X-Flow-User-ID", userID)

	// SSRF 收口（2026-11-15 审计 M-1）：内网拦截从「parse 期 DNS 解析」升级为
	// 「拨号期复校」（urlguard.GuardedTransport，与 kb_crawler 同一单一事实源）。
	// validateWebhookURL 的解析期校验保留作快速失败，但它挡不住 DNS rebinding——
	// 校验与建连之间目标域名可重解析到内网 IP；拨号期 Control 回调对 rebinding、
	// 重定向、CGNAT/云元数据段全部生效。
	//
	// 开发期豁免（MARKETING_WEBHOOK_ALLOW_INSECURE）必须同时覆盖这两层：豁免只在
	// validateWebhookURL 里生效的话，本地联调把 webhook 指向 127.0.0.1 仍会在拨号期
	// 被拒 —— 策略一致、实现两处，漏一处就是「配置放行但永远调不通」。
	var transport http.RoundTripper = urlguard.GuardedTransport()
	if insecureWebhookBypassAllowed() {
		transport = http.DefaultTransport
	}
	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("发送 Webhook 请求失败：%w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败：%w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("webhook 返回错误状态码：%d, 响应：%s", resp.StatusCode, string(respBody))
	}

	var result map[string]any
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &result); err != nil {

			return map[string]any{
				"status_code": resp.StatusCode,
				"body":        string(respBody),
			}, nil
		}
		result["status_code"] = resp.StatusCode
		return result, nil
	}

	return map[string]any{
		"status_code": resp.StatusCode,
	}, nil
}

func (s *MarketingFlowService) sendActionSendEmail(ctx context.Context, config map[string]any, userID string, data map[string]any) (map[string]any, error) {
	// FA-20：邮件动作与短信同一条链——先复核流程状态，再交给注册的发送器
	// （internal/service 的注册实现先过统一判定链才拨号）。本层不再直接接触 SMTP：
	// 未注册发送器 = fail-closed，不允许任何绕过闸口的直发通道。
	if err := s.ensureFlowActiveAtSend(ctx); err != nil {
		return nil, err
	}
	if emailSenderFunc == nil {
		return nil, errors.New("邮件发送器未注册：营销流程邮件必须经过统一发送闸口，禁止直连 SMTP")
	}

	cfg, err := buildMarketingFlowEmailConfig(config)
	if err != nil {
		return nil, err
	}

	if err := emailSenderFunc(ctx, cfg); err != nil {
		return nil, fmt.Errorf("发送邮件失败：%w", err)
	}

	return map[string]any{
		"sent": true,
		"to":   cfg.To,
	}, nil
}

// insecureWebhookBypassAllowed 判定 SSRF 校验是否可豁免。
//
// 豁免必须同时满足「显式设了开关」与「进程处于开发姿态」（APP_ENV/MODE/GIN_MODE 判定，
// 与 ALLOW_INSECURE_WEBHOOK、secrets MASTER_KEY 护栏复用同一个 config.IsDevelopmentEnv）。
// 只看开关 ⇒ 一个既不在 .env-example 也不在文档里的变量能把 SSRF 闸门整个关掉，
// 且在进程上留了生产环境也照样静默生效。
func insecureWebhookBypassAllowed() bool {
	if os.Getenv("MARKETING_WEBHOOK_ALLOW_INSECURE") != "true" {
		return false
	}
	return config.IsDevelopmentEnv()
}

func validateWebhookURL(raw string) error {

	if insecureWebhookBypassAllowed() {
		logger.Warnf("[MarketingFlow] MARKETING_WEBHOOK_ALLOW_INSECURE=true 且当前为开发环境，跳过 webhook URL 的 https/内网校验 url=%s", raw)
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return errors.New("webhook URL 格式无效")
	}
	if u.Scheme != "https" {
		return errors.New("webhook URL 必须为 https://")
	}
	host := u.Hostname()
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("webhook 域名解析失败: %w", err)
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return errors.New("webhook 目标指向内网地址，已拒绝")
		}
	}
	return nil
}
