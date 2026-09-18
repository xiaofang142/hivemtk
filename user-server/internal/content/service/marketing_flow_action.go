package service

import (
	"context"
	"net"
	"net/url"
	"os"

	"encoding/json"

	"errors"

	"fmt"

	"hivemtk-user/internal/content/model"

	reachmodel "hivemtk-user/internal/model"

	"strconv"

	"strings"

	"time"

	"bytes"
	_type "hivemtk-user/internal/pkg/utils/type"
	"io"
	"net/http"
	"net/smtp"

	"hivemtk-user/internal/pkg/utils"
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

	if err := smsSenderFunc(phone, content); err != nil {
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

	client := &http.Client{
		Timeout: 30 * time.Second,
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

	smtpHost, _ := config["smtp_host"].(string)
	smtpUser, _ := config["smtp_user"].(string)
	smtpPass, _ := config["smtp_pass"].(string)

	if smtpHost == "" {
		return nil, errors.New("SMTP 主机未配置，请在动作配置或环境变量中设置 smtp_host")
	}
	if smtpUser == "" || smtpPass == "" {
		return nil, errors.New("SMTP 用户名或密码未配置")
	}

	var smtpPort int
	switch v := config["smtp_port"].(type) {
	case int:
		smtpPort = v
	case float64:
		smtpPort = int(v)
	default:
		smtpPort = 587
	}

	to, ok := config["to"].(string)
	if !ok || to == "" {
		return nil, errors.New("收件人邮箱未指定")
	}

	subject, _ := config["subject"].(string)
	if subject == "" {
		subject = "营销邮件"
	}

	body, _ := config["body"].(string)
	if body == "" {
		return nil, errors.New("邮件正文未指定")
	}

	from, _ := config["from"].(string)
	if from == "" {
		from = smtpUser
	}

	mime := "MIME-version: 1.0\r\n"
	contentType := "Content-Type: text/html; charset=\"UTF-8\"\r\n"
	if isHTML, ok := config["is_html"].(bool); ok && !isHTML {
		contentType = "Content-Type: text/plain; charset=\"UTF-8\"\r\n"
	}

	mailHeader := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\n%s%s\r\n",
		strings.NewReplacer("\r", "", "\n", "").Replace(from),
		strings.NewReplacer("\r", "", "\n", "").Replace(to),
		strings.NewReplacer("\r", "", "\n", "").Replace(subject),
		mime, contentType)
	message := mailHeader + body

	addr := fmt.Sprintf("%s:%d", smtpHost, smtpPort)
	auth := smtp.PlainAuth("", smtpUser, smtpPass, smtpHost)

	err := smtp.SendMail(addr, auth, from, []string{to}, []byte(message))
	if err != nil {
		return nil, fmt.Errorf("发送邮件失败：%w", err)
	}

	return map[string]any{
		"sent": true,
		"to":   to,
	}, nil
}

func validateWebhookURL(raw string) error {

	if os.Getenv("MARKETING_WEBHOOK_ALLOW_INSECURE") == "true" {
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
