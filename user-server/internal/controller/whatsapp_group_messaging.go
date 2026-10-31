package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/errhttp"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/pagination"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// GroupMessagingController WhatsApp 群发消息控制器
//
// 通过 service.ClueService 访问线索数据，遵循五层架构。
type GroupMessagingController struct {
	whatsappService *service.WhatsappService
	clueSvc         *service.ClueService
	messageQueue    *service.MessageQueueService
	templateService *service.WhatsAppTemplateService
	// queueRunning 群发队列互斥标记：同一时刻仅允许一个发送循环在跑
	queueRunning atomic.Bool
}

// NewGroupMessagingController 构造控制器。
//
// 依赖全部由**装配层**（router）注入，控制器不 import repository / gorm：
// 仓储属于 repository 包，controller 层碰它们会被 depguard 拦下
// （controller-layer 清单），而且那是五层架构的破口（Controller→Repository 跳层）。
// 装配点见 router/platform_routes.go 的 setupWhatsappRoutes。
func NewGroupMessagingController(
	whatsappSvc *service.WhatsappService,
	clueSvc *service.ClueService,
	messageQueue *service.MessageQueueService,
	templateService *service.WhatsAppTemplateService,
) *GroupMessagingController {
	return &GroupMessagingController{
		whatsappService: whatsappSvc,
		clueSvc:         clueSvc,
		messageQueue:    messageQueue,
		templateService: templateService,
	}
}

// whatsappCamelAliases 前端表单字段（camelCase）到接口契约（snake_case）的别名表。
//
// BulkMessaging.vue 直接提交 reactive 表单对象（sendForm/templateForm），字段名是
// templateId/leadIds/scheduleAt/isActive。不做这层归一化会有两个功能性缺口：
// 群发恒 400（缺 lead_ids/template_id）、模板开关 is_active 恒不生效。
var whatsappCamelAliases = map[string]string{
	"templateId": "template_id",
	"accountId":  "account_id",
	"leadIds":    "lead_ids",
	"scheduleAt": "schedule_at",
	"isActive":   "is_active",
}

// normalizeWhatsappCamelKeys 把请求体中的 camelCase 别名改写成 snake_case。
//
// 已显式提供 snake_case 键时以它为准（契约调用方不受影响）；非 JSON 体原样返回，
// 交给后续绑定逻辑报错。
func normalizeWhatsappCamelKeys(body []byte) []byte {
	var raw map[string]json.RawMessage
	if len(body) == 0 || json.Unmarshal(body, &raw) != nil {
		return body
	}
	found := false
	for camel, snake := range whatsappCamelAliases {
		v, ok := raw[camel]
		if !ok {
			continue
		}
		found = true
		if _, exists := raw[snake]; !exists {
			raw[snake] = v
		}
		delete(raw, camel)
	}
	if !found {
		return body
	}
	out, err := json.Marshal(raw)
	if err != nil {
		return body
	}
	return out
}

// replyTemplateError 模板相关错误的统一出口：未找到时给具体文案，
// 其余交给通用 DB 错误映射（否则只会回「记录不存在」）。
func (gmc *GroupMessagingController) replyTemplateError(c *gin.Context, err error, operation string) bool {
	if errhttp.IsNotFoundError(err) {
		response.NotFound(c, "模板不存在")
		return true
	}
	return HandleDBError(c, err, operation)
}

// readWhatsappJSON 读取请求体并做 camelCase 归一化。
func readWhatsappJSON(c *gin.Context) ([]byte, error) {
	body, err := c.GetRawData()
	if err != nil {
		return nil, err
	}
	return normalizeWhatsappCamelKeys(body), nil
}

// @Summary 获取线索库中的群体
// @Tags      group-messaging
// @Router    /api/whatsapp/lead-groups [get]
// 获取线索库中的群体
func (gmc *GroupMessagingController) GetLeadGroups(c *gin.Context) {
	page, limit, err := pagination.Parse(c)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	clues, total, err := gmc.clueSvc.GetClueAllList(c.Request.Context(), 7)
	if err != nil {
		response.ErrorFromDB(c, err, "获取线索失败", err.Error())
		return
	}

	startIndex := (page - 1) * limit
	if startIndex >= int(total) {
		startIndex = int(total)
	}
	endIndex := startIndex + limit
	if endIndex > int(total) {
		endIndex = int(total)
	}

	leads := make([]map[string]any, 0)
	for i := startIndex; i < endIndex && i < len(clues); i++ {
		clue := clues[i]
		leads = append(leads, map[string]any{
			"id":      clue.ID,
			"name":    clue.Name,
			"phone":   clue.Account,
			"email":   "",
			"company": clue.Address,
			"source":  "whatsapp",
			"score":   80,
			"status":  "new",
		})
	}

	response.Success(c, gin.H{
		"data":  leads,
		"total": int(total),
		"page":  page,
		"limit": limit,
	}, "获取线索成功")
}

// @Summary      消息已添加到发送队列
// @Tags      group-messaging
// @Router    /api/whatsapp/group-messaging/send [post]
// 选择群体并群发消息
func (gmc *GroupMessagingController) SelectGroupAndSendMessage(c *gin.Context) {
	var req struct {
		LeadIDs    []string          `json:"lead_ids" binding:"required"`
		TemplateID string            `json:"template_id" binding:"required"`
		ScheduleAt *time.Time        `json:"schedule_at"`
		Variables  map[string]string `json:"variables"`
	}

	body, err := readWhatsappJSON(c)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "请求参数错误：无法读取请求体")
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		response.Error(c, http.StatusBadRequest, "请求参数错误：请求体不是合法的 JSON 对象")
		return
	}
	if len(req.LeadIDs) == 0 || req.TemplateID == "" {
		response.Error(c, http.StatusBadRequest, "请求参数错误：lead_ids（线索 ID 列表）与 template_id（模板 ID）均为必填")
		return
	}

	template, err := gmc.templateService.GetTemplate(c.Request.Context(), req.TemplateID)
	if gmc.replyTemplateError(c, err, "获取消息模板") {
		return
	}

	if !template.IsActive {
		response.Error(c, http.StatusBadRequest, "模板未激活", "模板未激活")
		return
	}

	// 按选中 ID 精确取线索：前端选择器列的是全类型线索，
	// 早先按单一线索类型（7=企业微信）过滤会把其它来源的选中项静默丢掉，
	// 结果「发送成功、count=0」。
	clues, err := gmc.clueSvc.GetByIDs(c.Request.Context(), req.LeadIDs)
	if err != nil {
		response.ErrorFromDB(c, err, "获取线索失败")
		return
	}
	byID := make(map[string]*model.Clue, len(clues))
	for _, clue := range clues {
		byID[clue.ID] = clue
	}

	leads := make([]map[string]any, 0, len(req.LeadIDs))
	skipped := make([]string, 0)
	for _, leadID := range req.LeadIDs {
		clue, ok := byID[leadID]
		if !ok {
			skipped = append(skipped, leadID)
			continue
		}
		phone := strings.TrimSpace(clue.Account)
		if phone == "" {
			// 没有手机号就无法投递，混进队列只会变成必然失败的记录。
			skipped = append(skipped, leadID)
			continue
		}
		leads = append(leads, map[string]any{
			"id":      clue.ID,
			"name":    clue.Name,
			"phone":   phone,
			"email":   "",
			"company": clue.Address,
			"source":  "whatsapp",
		})
	}
	if len(leads) == 0 {
		response.Error(c, http.StatusBadRequest, "没有可发送的线索：所选线索不存在或缺少手机号（account）")
		return
	}

	var messages []model.QueuedMessage
	for _, lead := range leads {
		leadMap := map[string]any{
			"name":    lead["name"],
			"phone":   lead["phone"],
			"email":   lead["email"],
			"company": lead["company"],
			"source":  lead["source"],
		}
		personalizedContent := gmc.personalizeMessage(template.Content, leadMap, req.Variables)

		message := model.QueuedMessage{
			ID:          generateMessageID(),
			LeadID:      lead["id"].(string),
			PhoneNumber: lead["phone"].(string),
			Content:     personalizedContent,
			TemplateID:  template.ID,
			ScheduleAt:  req.ScheduleAt,
			CreatedAt:   time.Now(),
		}

		messages = append(messages, message)
	}

	queueID, err := gmc.messageQueue.AddBatch(c.Request.Context(), messages)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "添加到队列失败", "添加到队列失败")
		return
	}

	if req.ScheduleAt == nil || req.ScheduleAt.Before(time.Now()) {
		// 同一队列禁止并发重复处理（重复提交会导致重复扣发）
		if !gmc.queueRunning.CompareAndSwap(false, true) {
			response.Error(c, http.StatusConflict, "已有群发队列在处理中，请稍后再试", "已有群发队列在处理中，请稍后再试")
			return
		}
		bgCtx := tenant.BackgroundWithTenant(c.Request.Context())
		utils.SafeGo(bgCtx, "whatsapp.group_messaging", func(ctx context.Context) {
			defer gmc.queueRunning.Store(false)
			gmc.processMessageQueue(ctx, queueID)
		})
	}

	response.Success(c, gin.H{
		"queue_id": queueID,
		"count":    len(messages),
		"skipped":  len(skipped),
	}, "消息已添加到发送队列")
}

func (gmc *GroupMessagingController) personalizeMessage(templateContent string, lead map[string]any, globalVars map[string]string) string {
	data := map[string]any{
		"name":    lead["name"],
		"phone":   lead["phone"],
		"email":   lead["email"],
		"company": lead["company"],
		"source":  lead["source"],
	}

	for k, v := range globalVars {
		data[k] = v
	}

	tmpl, err := template.New("message").Parse(templateContent)
	if err != nil {
		return templateContent
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, data); err != nil {
		return templateContent
	}

	return result.String()
}

func (gmc *GroupMessagingController) processMessageQueue(ctx context.Context, queueID string) {
	messages := gmc.messageQueue.GetQueue(ctx, queueID)

	for i, message := range messages {
		if i > 0 {
			time.Sleep(1 * time.Second)
		}

		success := gmc.sendMessageToWhatsApp(ctx, message)

		gmc.messageQueue.UpdateStatus(ctx, queueID, message.ID, success)

		if !success {
			gmc.recordSendFailure(message)
		}
	}
}

func (gmc *GroupMessagingController) sendMessageToWhatsApp(ctx context.Context, message model.QueuedMessage) bool {
	if gmc.whatsappService == nil {
		logger.Errorf("WhatsApp 服务未初始化")
		return false
	}

	accounts, err := gmc.whatsappService.ListAccounts(ctx)
	if err != nil || len(accounts) == 0 {
		logger.Errorf("获取WhatsApp账号失败或无可用账号: %v", err)
		gmc.persistSendFailure(ctx, message, "无可用账号")
		return false
	}

	var account *model.WhatsappAccount
	for _, a := range accounts {
		if a.Status == model.WhatsappStatusOnline || a.Status == model.WhatsappStatusPending {
			account = a
			break
		}
	}
	if account == nil {
		account = accounts[0]
	}

	hasSession, _ := gmc.whatsappService.LoginStatus(ctx, account.ID)
	if !hasSession {
		_, loginErr := gmc.whatsappService.StartLogin(ctx, account.ID, 30*time.Second)
		if loginErr != nil {
			logger.Errorf("启动 WhatsApp 登录失败: %v", loginErr)
			gmc.persistSendFailure(ctx, message, "账号未登录: "+loginErr.Error())
			return false
		}
		gmc.persistSendFailure(ctx, message, "账号尚未扫码登录，消息已入队待补发")
		return false
	}

	toJid := formatWhatsAppJID(message.PhoneNumber)
	if _, err := gmc.whatsappService.SendTextMessage(ctx, account.ID, toJid, message.Content); err != nil {
		logger.Errorf("发送 WhatsApp 消息失败: %v", err)
		gmc.persistSendFailure(ctx, message, err.Error())
		return false
	}
	gmc.persistSendSuccess(ctx, message)
	return true
}

func formatWhatsAppJID(phone string) string {
	phone = strings.TrimSpace(phone)
	phone = strings.ReplaceAll(phone, "+", "")
	phone = strings.ReplaceAll(phone, " ", "")
	return fmt.Sprintf("%s@s.whatsapp.net", phone)
}

func (gmc *GroupMessagingController) persistSendSuccess(ctx context.Context, message model.QueuedMessage) {
	gmc.messageQueue.RecordGroupMessage(ctx, message, "sent", "")
}

func (gmc *GroupMessagingController) persistSendFailure(ctx context.Context, message model.QueuedMessage, reason string) {
	gmc.messageQueue.RecordGroupMessage(ctx, message, "failed", reason)
}

func (gmc *GroupMessagingController) recordSendFailure(message model.QueuedMessage) {
	// 失败排查只需可回查的 ID + 可定位的打码号码；正文截断到 120 rune，防整页客户内容进日志
	content := message.Content
	if rs := []rune(content); len(rs) > 120 {
		content = string(rs[:120]) + "…"
	}
	logger.Errorf("消息发送失败: ID=%s, Phone=%s, Content=%s", message.ID, utils.MaskPhone(message.PhoneNumber), content)
}

// @Summary 获取发送状态
// @Tags      group-messaging
// @Router    /api/whatsapp/group-messaging/status/{queue_id} [get]
// 获取发送状态
func (gmc *GroupMessagingController) GetMessageStatus(c *gin.Context) {
	queueID := c.Param("queue_id")

	status := gmc.messageQueue.GetStatus(c.Request.Context(), queueID)

	response.Success(c, gin.H{
		"queue_id": queueID,
		"status":   status,
	}, "获取状态成功")
}

// @Summary 获取消息模板列表
// @Tags      group-messaging
// @Router    /api/whatsapp/templates [get]
// 获取消息模板列表
func (gmc *GroupMessagingController) GetTemplates(c *gin.Context) {
	category := c.Query("category")
	isActiveStr := c.Query("is_active")

	var isActive *bool
	if isActiveStr != "" {
		active, err := strconv.ParseBool(isActiveStr)
		if err == nil {
			isActive = &active
		}
	}

	templates, err := gmc.templateService.GetTemplates(c.Request.Context(), category, isActive)
	if err != nil {
		response.ErrorFromDB(c, err, "获取模板失败", err.Error())
		return
	}

	response.Success(c, gin.H{"data": templates}, "获取模板成功")
}

// @Summary 获取消息模板详情
// @Tags      group-messaging
// @Router    /api/whatsapp/templates/{id} [get]
// GetTemplateByID 获取消息模板详情
func (gmc *GroupMessagingController) GetTemplateByID(c *gin.Context) {
	templateID := c.Param("id")
	if templateID == "" {
		response.Error(c, http.StatusBadRequest, "模板ID不能为空")
		return
	}

	template, err := gmc.templateService.GetTemplate(c.Request.Context(), templateID)
	if gmc.replyTemplateError(c, err, "获取消息模板") {
		return
	}

	response.Success(c, template, "获取模板成功")
}

// @Summary 创建消息模板
// @Tags      group-messaging
// @Router    /api/whatsapp/templates [post]
// 创建消息模板
func (gmc *GroupMessagingController) CreateTemplate(c *gin.Context) {
	// IsActive 用指针承接：模板表的 is_active 是 bool，无法区分「未传」与「显式 false」，
	// 而未传时应默认启用。内层嵌入的 model 字段同名会被外层遮蔽，正好各取所需。
	var req struct {
		model.WhatsappMessageTemplate
		IsActive *bool `json:"is_active"`
	}
	body, err := readWhatsappJSON(c)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "请求参数错误：无法读取请求体")
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		response.Error(c, http.StatusBadRequest, "请求参数错误：请求体不是合法的 JSON 对象")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		response.Error(c, http.StatusBadRequest, "请求参数错误：模板名称（name）不能为空")
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		response.Error(c, http.StatusBadRequest, "请求参数错误：模板内容（content）不能为空")
		return
	}

	newTemplate := req.WhatsappMessageTemplate
	if req.IsActive != nil {
		newTemplate.IsActive = *req.IsActive
	} else {
		newTemplate.IsActive = true
	}

	template, err := gmc.templateService.CreateTemplate(c.Request.Context(), &newTemplate)
	if HandleDBError(c, err, "创建模板") {
		return
	}

	response.Success(c, template, "创建模板成功")
}

// @Summary 更新消息模板
// @Tags      group-messaging
// @Router    /api/whatsapp/templates/{id} [put]
// 更新消息模板
func (gmc *GroupMessagingController) UpdateTemplate(c *gin.Context) {
	id := c.Param("id")
	rawBody, err := c.GetRawData()
	if err != nil {
		response.Error(c, http.StatusBadRequest, "请求参数错误：无法读取请求体")
		return
	}
	body := normalizeWhatsappCamelKeys(rawBody)
	var req model.WhatsappMessageTemplate
	if err := json.Unmarshal(body, &req); err != nil {
		response.Error(c, http.StatusBadRequest, "请求参数错误：请求体不是合法的 JSON 对象")
		return
	}

	existingTemplate, err := gmc.templateService.GetTemplate(c.Request.Context(), id)
	if gmc.replyTemplateError(c, err, "获取模板") {
		return
	}

	// PATCH 语义：model 绑定的是非指针 bool，需按原始 key 判断 is_active 是否显式提供，
	// 未提供时保留原值，防止漏字段把模板意外停用
	var rawKeys map[string]json.RawMessage
	_ = json.Unmarshal(body, &rawKeys)

	existingTemplate.Name = req.Name
	existingTemplate.Content = req.Content
	existingTemplate.Category = req.Category
	if _, ok := rawKeys["is_active"]; ok {
		existingTemplate.IsActive = req.IsActive
	}
	existingTemplate.Description = req.Description

	updated, err := gmc.templateService.UpdateTemplate(c.Request.Context(), existingTemplate)
	if HandleDBError(c, err, "更新模板") {
		return
	}

	response.Success(c, updated, "更新模板成功")
}

// @Summary 删除消息模板
// @Tags      group-messaging
// @Router    /api/whatsapp/templates/{id} [delete]
// 删除消息模板
func (gmc *GroupMessagingController) DeleteTemplate(c *gin.Context) {
	id := c.Param("id")

	if gmc.replyTemplateError(c, gmc.templateService.DeleteTemplate(c.Request.Context(), id), "删除模板") {
		return
	}

	response.Success(c, nil, "删除模板成功")
}

// @Summary 获取发送记录
// @Tags      group-messaging
// @Router    /api/whatsapp/group-messaging/records [get]
// 获取发送记录
func (gmc *GroupMessagingController) GetSendRecords(c *gin.Context) {
	page, limit, err := pagination.Parse(c)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	allStatuses := gmc.messageQueue.ListAllStatuses(c.Request.Context())

	statuses := make([]map[string]any, 0, len(allStatuses))
	for _, status := range allStatuses {
		updatedAt := status.Updated
		if updatedAt.IsZero() {
			updatedAt = status.Created
		}
		statuses = append(statuses, map[string]any{
			"id":           status.QueueID,
			"queue_id":     status.QueueID,
			"templateName": "批量消息",
			"totalCount":   status.Total,
			"sentCount":    status.Sent,
			"failedCount":  status.Failed,
			"status":       status.Status,
			"createdAt":    status.Created.Format("2006-01-02 15:04:05"),
			"updatedAt":    updatedAt.Format("2006-01-02 15:04:05"),
		})
	}

	startIndex := (page - 1) * limit
	if startIndex >= len(statuses) {
		startIndex = len(statuses)
	}
	endIndex := startIndex + limit
	if endIndex > len(statuses) {
		endIndex = len(statuses)
	}

	pagedStatuses := statuses[startIndex:endIndex]

	response.Success(c, gin.H{
		"data":  pagedStatuses,
		"total": len(statuses),
		"page":  page,
		"limit": limit,
	}, "获取发送记录成功")
}

// @Summary      批量发送任务已启动
// @Tags      group-messaging
// @Router    /api/whatsapp/bulk-send [post]
// BulkSend 模板批量发送（BulkMatrix.vue 依赖）
// POST /api/whatsapp/bulk-send {template_id, audience{type,segment_id}, rate_per_minute, variables}
func (gmc *GroupMessagingController) BulkSend(c *gin.Context) {
	var req struct {
		TemplateID string `json:"template_id" binding:"required"`
		Audience   struct {
			Type      string `json:"type"`
			SegmentID string `json:"segment_id"`
		} `json:"audience"`
		RatePerMinute int               `json:"rate_per_minute"`
		Variables     map[string]string `json:"variables"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误", err.Error())
		return
	}

	template, err := gmc.templateService.GetTemplate(c.Request.Context(), req.TemplateID)
	if gmc.replyTemplateError(c, err, "获取消息模板") {
		return
	}
	if !template.IsActive {
		response.Error(c, http.StatusBadRequest, "模板未激活", "模板未激活")
		return
	}

	var leads []map[string]any
	switch req.Audience.Type {
	case "segment":
		if req.Audience.SegmentID == "" {
			response.Error(c, http.StatusBadRequest, "请选择目标分群")
			return
		}
		clues, _, err := gmc.clueSvc.GetWhatsappClues(c.Request.Context())
		if err != nil {
			response.ErrorFromDB(c, err, "获取线索失败")
			return
		}
		for _, clue := range clues {
			leads = append(leads, map[string]any{
				"id": clue.ID, "name": clue.Name, "phone": clue.Account,
				"email": "", "company": clue.Address, "source": "whatsapp",
			})
		}
	default: // all / clue / csv 均按全部 WhatsApp 线索发送
		clues, _, err := gmc.clueSvc.GetWhatsappClues(c.Request.Context())
		if err != nil {
			response.ErrorFromDB(c, err, "获取线索失败")
			return
		}
		for _, clue := range clues {
			leads = append(leads, map[string]any{
				"id": clue.ID, "name": clue.Name, "phone": clue.Account,
				"email": "", "company": clue.Address, "source": "whatsapp",
			})
		}
	}
	if len(leads) == 0 {
		response.Error(c, http.StatusBadRequest, "目标群体为空，无可发送对象")
		return
	}

	messages := make([]model.QueuedMessage, 0, len(leads))
	for _, lead := range leads {
		content := gmc.personalizeMessage(template.Content, lead, req.Variables)
		messages = append(messages, model.QueuedMessage{
			ID:          generateMessageID(),
			LeadID:      lead["id"].(string),
			PhoneNumber: lead["phone"].(string),
			Content:     content,
			TemplateID:  template.ID,
			CreatedAt:   time.Now(),
		})
	}

	queueID, err := gmc.messageQueue.AddBatch(c.Request.Context(), messages)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "添加到队列失败", "添加到队列失败")
		return
	}

	if !gmc.queueRunning.CompareAndSwap(false, true) {
		response.Error(c, http.StatusConflict, "已有群发队列在处理中，请稍后再试", "已有群发队列在处理中，请稍后再试")
		return
	}
	bgCtx := tenant.BackgroundWithTenant(c.Request.Context())
	utils.SafeGo(bgCtx, "whatsapp.bulk_send", func(ctx context.Context) {
		defer gmc.queueRunning.Store(false)
		gmc.processMessageQueue(ctx, queueID)
	})

	response.Success(c, gin.H{
		"id":       queueID,
		"queue_id": queueID,
		"count":    len(messages),
	}, "批量发送任务已启动")
}

// @Summary 批量发送进度（BulkMatrix.vue 轮询依赖）
// @Tags      group-messaging
// @Router    /api/whatsapp/jobs/{id}/progress [get]
// GetJobProgress 批量发送进度（BulkMatrix.vue 轮询依赖）
// GET /api/whatsapp/jobs/:id/progress
func (gmc *GroupMessagingController) GetJobProgress(c *gin.Context) {
	jobID := c.Param("id")
	status := gmc.messageQueue.GetStatus(c.Request.Context(), jobID)

	total := status.Total
	percentage := 0.0
	if total > 0 {
		percentage = float64(status.Sent+status.Failed) * 100 / float64(total)
	}
	done := status.Status == "completed"

	response.Success(c, gin.H{
		"id":         jobID,
		"status":     map[bool]string{true: "done", false: "running"}[done],
		"percentage": percentage,
		"stats": gin.H{
			"sent":   status.Sent,
			"failed": status.Failed,
			"total":  status.Total,
		},
		"recent": []any{},
	}, "获取进度成功")
}

func generateMessageID() string {
	return fmt.Sprintf("msg_%d", time.Now().UnixNano())
}
