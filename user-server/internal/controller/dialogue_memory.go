package controller

import (
	"net/http"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/pagination"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// DialogueMemoryController 对话记忆控制器
type DialogueMemoryController struct {
	svc *service.DialogueMemoryService
}

// NewDialogueMemoryController 创建对话记忆控制器
func NewDialogueMemoryController(svc *service.DialogueMemoryService) *DialogueMemoryController {
	return &DialogueMemoryController{svc: svc}
}

// AppendMessageRequest 追加消息
type AppendMessageRequest struct {
	SessionID  string `json:"session_id" binding:"required"`
	CustomerID string `json:"customer_id"`
	Role       string `json:"role" binding:"required"`
	Content    string `json:"content" binding:"required"`
}

// AppendMessage 追加消息
func (c *DialogueMemoryController) AppendMessage(ctx *gin.Context) {
	var req AppendMessageRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	err := c.svc.AppendMessage(ctx.Request.Context(), req.SessionID, req.CustomerID, dto.Message{
		Role:      req.Role,
		Content:   req.Content,
		Timestamp: time.Now(),
	})
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, nil, "追加成功")
}

// ShortTerm 短期记忆
func (c *DialogueMemoryController) ShortTerm(ctx *gin.Context) {
	sessionID := ctx.Query("session_id")
	if sessionID == "" {
		response.Error(ctx, http.StatusBadRequest, "session_id 必填")
		return
	}
	msgs, err := c.svc.GetShortTermMemory(ctx.Request.Context(), sessionID)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, msgs, "查询成功")
}

// LongTerm 长期记忆
func (c *DialogueMemoryController) LongTerm(ctx *gin.Context) {
	sessionID := ctx.Query("session_id")
	mem, err := c.svc.GetLongTermMemory(ctx.Request.Context(), sessionID)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, mem, "查询成功")
}

// UpdateKeyFactsRequest 更新事实
type UpdateKeyFactsRequest struct {
	SessionID string            `json:"session_id" binding:"required"`
	Facts     map[string]string `json:"facts" binding:"required"`
}

// UpdateKeyFacts 更新关键事实
func (c *DialogueMemoryController) UpdateKeyFacts(ctx *gin.Context) {
	var req UpdateKeyFactsRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	err := c.svc.UpdateKeyFacts(ctx.Request.Context(), req.SessionID, req.Facts)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, nil, "更新成功")
}

// RecordObjectionRequest 记录异议
type RecordObjectionRequest struct {
	SessionID     string `json:"session_id" binding:"required"`
	ObjectionType string `json:"objection_type" binding:"required"`
	Content       string `json:"content" binding:"required"`
}

// RecordObjection 记录异议
func (c *DialogueMemoryController) RecordObjection(ctx *gin.Context) {
	var req RecordObjectionRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	err := c.svc.RecordObjection(ctx.Request.Context(), req.SessionID, req.ObjectionType, req.Content)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, nil, "记录成功")
}

// UpdatePurchaseIntentRequest 购买意向
type UpdatePurchaseIntentRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	Level     string `json:"level" binding:"required"`
}

// UpdatePurchaseIntent 更新购买意向
func (c *DialogueMemoryController) UpdatePurchaseIntent(ctx *gin.Context) {
	var req UpdatePurchaseIntentRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	err := c.svc.UpdatePurchaseIntent(ctx.Request.Context(), req.SessionID, req.Level)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, nil, "更新成功")
}

// RecordIntentRequest 意图轨迹
type RecordIntentRequest struct {
	SessionID  string `json:"session_id" binding:"required"`
	IntentType string `json:"intent_type" binding:"required"`
}

// RecordIntent 记录意图
func (c *DialogueMemoryController) RecordIntent(ctx *gin.Context) {
	var req RecordIntentRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	err := c.svc.RecordIntent(ctx.Request.Context(), req.SessionID, req.IntentType)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, nil, "记录成功")
}

// RecordSOPRequest 记录SOP
type RecordSOPRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	SOPName   string `json:"sop_name" binding:"required"`
}

// RecordSOP 记录 SOP
func (c *DialogueMemoryController) RecordSOP(ctx *gin.Context) {
	var req RecordSOPRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	err := c.svc.RecordSOP(ctx.Request.Context(), req.SessionID, req.SOPName)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, nil, "记录成功")
}

// BuildContext 上下文
func (c *DialogueMemoryController) BuildContext(ctx *gin.Context) {
	sessionID := ctx.Query("session_id")
	customerID := ctx.Query("customer_id")
	s, err := c.svc.BuildContext(ctx.Request.Context(), sessionID, customerID)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{"context": s}, "查询成功")
}

// Stats 客户会话列表
func (c *DialogueMemoryController) Stats(ctx *gin.Context) {
	customerID := ctx.Query("customer_id")
	_, limit, err := pagination.Parse(ctx)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	mems, total, err := c.svc.ListByCustomerID(ctx.Request.Context(), customerID, limit)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.SuccessWithPage(ctx, mems, 1, int64(limit), total)
}
