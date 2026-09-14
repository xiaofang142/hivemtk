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

func NewGroupMessagingController(
	whatsappSvc *service.WhatsappService,
	clueSvc *service.ClueService,
	messageQueue *service.MessageQueueService,
	templateService *service.WhatsAppTemplateService,
) *GroupMessagingController {
	if whatsappSvc == nil {
		whatsappSvc = service.NewWhatsappService()
	}
	if clueSvc == nil {
		clueSvc = service.NewClueService()
	}
	if templateService == nil {
		templateService = service.NewWhatsAppTemplateService(nil)
	}
	if messageQueue == nil {
		messageQueue = service.NewMessageQueueService(nil)
	}
	return &GroupMessagingController{
		whatsappService: whatsappSvc,
		clueSvc:         clueSvc,
		messageQueue:    messageQueue,
		templateService: templateService,
	}
}

// 获取线索库中的群体
func (gmc *GroupMessagingController) GetLeadGroups(c *gin.Context) {
	page, limit, err := pagination.Parse(c)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	clues, total, err := gmc.clueSvc.GetClueAllList(context.Background(), 7)
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

// 选择群体并群发消息
func (gmc *GroupMessagingController) SelectGroupAndSendMessage(c *gin.Context) {
	var req struct {
		LeadIDs    []string          `json:"lead_ids" binding:"required"`
		TemplateID string            `json:"template_id" binding:"required"`
		ScheduleAt *time.Time        `json:"schedule_at"`
		Variables  map[string]string `json:"variables"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误", err.Error())
		return
	}

	template, err := gmc.templateService.GetTemplate(context.Background(), req.TemplateID)
	if HandleDBError(c, err, "获取消息模板") {
		return
	}

	if !template.IsActive {
		response.Error(c, http.StatusBadRequest, "模板未激活", "模板未激活")
		return
	}

	leads := make([]map[string]any, 0)

	allClues, _, err := gmc.clueSvc.GetClueAllList(context.Background(), 7)
	if err != nil {
		logger.Errorf("获取WhatsApp线索失败: %v", err)
		for _, leadID := range req.LeadIDs {
			leads = append(leads, map[string]any{
				"id":      leadID,
				"name":    "Unknown",
				"phone":   leadID,
				"email":   "unknown@example.com",
				"company": "Unknown Company",
				"source":  "whatsapp",
			})
		}
	} else {
		for _, clue := range allClues {
			clueIDStr := clue.ID
			for _, reqLeadID := range req.LeadIDs {
				if clueIDStr == reqLeadID {
					leads = append(leads, map[string]any{
						"id":      clueIDStr,
						"name":    clue.Name,
						"phone":   clue.Account,
						"email":   "",
						"company": clue.Address,
						"source":  "whatsapp",
					})
					break
				}
			}
		}
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

	queueID, err := gmc.messageQueue.AddBatch(context.Background(), messages)
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
		utils.SafeGo(context.Background(), "whatsapp.group_messaging", func(ctx context.Context) {
			defer gmc.queueRunning.Store(false)
			gmc.processMessageQueue(queueID)
		})
	}

	response.Success(c, gin.H{
		"queue_id": queueID,
		"count":    len(messages),
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

func (gmc *GroupMessagingController) processMessageQueue(queueID string) {
	messages := gmc.messageQueue.GetQueue(context.Background(), queueID)

	for i, message := range messages {
		if i > 0 {
			time.Sleep(1 * time.Second)
		}

		success := gmc.sendMessageToWhatsApp(message)

		gmc.messageQueue.UpdateStatus(context.Background(), queueID, message.ID, success)

		if !success {
			gmc.recordSendFailure(message)
		}
	}
}

func (gmc *GroupMessagingController) sendMessageToWhatsApp(message model.QueuedMessage) bool {
	if gmc.whatsappService == nil {
		logger.Errorf("WhatsApp 服务未初始化")
		return false
	}

	accounts, err := gmc.whatsappService.ListAccounts(context.Background())
	if err != nil || len(accounts) == 0 {
		logger.Errorf("获取WhatsApp账号失败或无可用账号: %v", err)
		gmc.persistSendFailure(message, "无可用账号")
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

	hasSession, _ := gmc.whatsappService.LoginStatus(context.Background(), account.ID)
	if !hasSession {
		_, loginErr := gmc.whatsappService.StartLogin(context.Background(), account.ID, 30*time.Second)
		if loginErr != nil {
			logger.Errorf("启动 WhatsApp 登录失败: %v", loginErr)
			gmc.persistSendFailure(message, "账号未登录: "+loginErr.Error())
			return false
		}
		gmc.persistSendFailure(message, "账号尚未扫码登录，消息已入队待补发")
		return false
	}

	toJid := formatWhatsAppJID(message.PhoneNumber)
	if _, err := gmc.whatsappService.SendTextMessage(context.Background(), account.ID, toJid, message.Content); err != nil {
		logger.Errorf("发送 WhatsApp 消息失败: %v", err)
		gmc.persistSendFailure(message, err.Error())
		return false
	}
	gmc.persistSendSuccess(message)
	return true
}

func formatWhatsAppJID(phone string) string {
	phone = strings.TrimSpace(phone)
	phone = strings.ReplaceAll(phone, "+", "")
	phone = strings.ReplaceAll(phone, " ", "")
	return fmt.Sprintf("%s@s.whatsapp.net", phone)
}

func (gmc *GroupMessagingController) persistSendSuccess(message model.QueuedMessage) {
	gmc.messageQueue.RecordGroupMessage(context.Background(), message, "sent", "")
}

func (gmc *GroupMessagingController) persistSendFailure(message model.QueuedMessage, reason string) {
	gmc.messageQueue.RecordGroupMessage(context.Background(), message, "failed", reason)
}

func (gmc *GroupMessagingController) recordSendFailure(message model.QueuedMessage) {
	logger.Errorf("消息发送失败: ID=%s, Phone=%s, Content=%s", message.ID, message.PhoneNumber, message.Content)
}

// 获取发送状态
func (gmc *GroupMessagingController) GetMessageStatus(c *gin.Context) {
	queueID := c.Param("queue_id")

	status := gmc.messageQueue.GetStatus(context.Background(), queueID)

	response.Success(c, gin.H{
		"queue_id": queueID,
		"status":   status,
	}, "获取状态成功")
}

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

	templates, err := gmc.templateService.GetTemplates(context.Background(), category, isActive)
	if err != nil {
		response.ErrorFromDB(c, err, "获取模板失败", err.Error())
		return
	}

	response.Success(c, gin.H{"data": templates}, "获取模板成功")
}

// GetTemplateByID 获取消息模板详情
func (gmc *GroupMessagingController) GetTemplateByID(c *gin.Context) {
	templateID := c.Param("id")
	if templateID == "" {
		response.Error(c, http.StatusBadRequest, "模板ID不能为空")
		return
	}

	template, err := gmc.templateService.GetTemplate(context.Background(), templateID)
	if HandleDBError(c, err, "获取消息模板") {
		return
	}

	response.Success(c, template, "获取模板成功")
}

// 创建消息模板
func (gmc *GroupMessagingController) CreateTemplate(c *gin.Context) {
	var req model.WhatsappMessageTemplate
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误", err.Error())
		return
	}

	template, err := gmc.templateService.CreateTemplate(context.Background(), &req)
	if HandleDBError(c, err, "创建模板") {
		return
	}

	response.Success(c, template, "创建模板成功")
}

// 更新消息模板
func (gmc *GroupMessagingController) UpdateTemplate(c *gin.Context) {
	id := c.Param("id")
	body, err := c.GetRawData()
	if err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误", err.Error())
		return
	}
	var req model.WhatsappMessageTemplate
	if err := json.Unmarshal(body, &req); err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误", err.Error())
		return
	}

	existingTemplate, err := gmc.templateService.GetTemplate(context.Background(), id)
	if HandleDBError(c, err, "获取模板") {
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

	updated, err := gmc.templateService.UpdateTemplate(context.Background(), existingTemplate)
	if HandleDBError(c, err, "更新模板") {
		return
	}

	response.Success(c, updated, "更新模板成功")
}

// 删除消息模板
func (gmc *GroupMessagingController) DeleteTemplate(c *gin.Context) {
	id := c.Param("id")

	if HandleDBError(c, gmc.templateService.DeleteTemplate(context.Background(), id), "删除模板") {
		return
	}

	response.Success(c, nil, "删除模板成功")
}

// 获取发送记录
func (gmc *GroupMessagingController) GetSendRecords(c *gin.Context) {
	page, limit, err := pagination.Parse(c)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	allStatuses := gmc.messageQueue.ListAllStatuses(context.Background())

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

	template, err := gmc.templateService.GetTemplate(context.Background(), req.TemplateID)
	if HandleDBError(c, err, "获取消息模板") {
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
		clues, _, err := gmc.clueSvc.GetWhatsappClues(context.Background())
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
		clues, _, err := gmc.clueSvc.GetWhatsappClues(context.Background())
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

	queueID, err := gmc.messageQueue.AddBatch(context.Background(), messages)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "添加到队列失败", "添加到队列失败")
		return
	}

	if !gmc.queueRunning.CompareAndSwap(false, true) {
		response.Error(c, http.StatusConflict, "已有群发队列在处理中，请稍后再试", "已有群发队列在处理中，请稍后再试")
		return
	}
	utils.SafeGo(context.Background(), "whatsapp.bulk_send", func(ctx context.Context) {
		defer gmc.queueRunning.Store(false)
		gmc.processMessageQueue(queueID)
	})

	response.Success(c, gin.H{
		"id":       queueID,
		"queue_id": queueID,
		"count":    len(messages),
	}, "批量发送任务已启动")
}

// GetJobProgress 批量发送进度（BulkMatrix.vue 轮询依赖）
// GET /api/whatsapp/jobs/:id/progress
func (gmc *GroupMessagingController) GetJobProgress(c *gin.Context) {
	jobID := c.Param("id")
	status := gmc.messageQueue.GetStatus(context.Background(), jobID)

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
