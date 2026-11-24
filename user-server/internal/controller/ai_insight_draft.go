package controller

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

type AIInsightDraftController struct {
	svc *service.AIInsightDraftService
}

func NewAIInsightDraftController() *AIInsightDraftController {
	return &AIInsightDraftController{svc: service.NewAIInsightDraftService()}
}

func (ctrl *AIInsightDraftController) RegisterRoutes(r *gin.RouterGroup) {
	g := r.Group("/ai-insights")
	{
		g.POST("/drafts", ctrl.Create)
		g.POST("/drafts/generate", ctrl.GenerateStub)
		g.GET("/drafts", ctrl.List)
		g.GET("/drafts/:id", ctrl.Get)
		g.POST("/drafts/:id/confirm", ctrl.Confirm)
		g.POST("/drafts/:id/reject", ctrl.Reject)
	}
}

func (ctrl *AIInsightDraftController) actor(c *gin.Context) uint {
	if v, ok := c.Get("user_id"); ok {
		switch n := v.(type) {
		case uint:
			return n
		case int:
			return uint(n)
		case int64:
			return uint(n)
		}
	}
	return 0
}

func (ctrl *AIInsightDraftController) recordOp(c *gin.Context, action, resourceID, detail string) {
	uid := ctrl.actor(c)
	uname, _ := c.Get("username")
	username, _ := uname.(string)
	tid := uint(1)
	if id, ok := tenant.FromContext(c.Request.Context()); ok {
		tid = id
	}
	_ = service.NewOperationLogService().Create(c.Request.Context(), &model.OperationLog{
		TenantID: tid, UserID: uid, Username: username,
		Action: action, Module: "crm_ai_insight", Resource: "ai_insight_draft",
		ResourceID: resourceID, Detail: detail, IP: c.ClientIP(), UserAgent: c.Request.UserAgent(),
	})
}

func (ctrl *AIInsightDraftController) Create(c *gin.Context) {
	var req model.AIInsightDraft
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "请求参数错误")
		return
	}
	if tid, ok := tenant.FromContext(c.Request.Context()); ok {
		req.TenantID = tid
	}
	if err := ctrl.svc.Create(c.Request.Context(), &req); err != nil {
		response.ErrorFromDB(c, err, "创建AI洞察草稿失败")
		return
	}
	response.Success(c, req, "创建成功")
}

func (ctrl *AIInsightDraftController) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	items, total, err := ctrl.svc.List(c.Request.Context(), c.Query("entity_type"), c.Query("entity_id"), c.Query("status"), page, pageSize)
	if err != nil {
		response.ErrorFromDB(c, err, "获取AI洞察草稿失败")
		return
	}
	response.SuccessWithPage(c, items, int64(page), int64(pageSize), total)
}

func (ctrl *AIInsightDraftController) Get(c *gin.Context) {
	item, err := ctrl.svc.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.ErrorFromDB(c, err, "获取AI洞察草稿失败")
		return
	}
	response.Success(c, item, "ok")
}

type aiInsightReviewReq struct {
	ReviewNote string `json:"review_note"`
}

func (ctrl *AIInsightDraftController) Confirm(c *gin.Context) {
	id := c.Param("id")
	var req aiInsightReviewReq
	_ = c.ShouldBindJSON(&req)
	result, err := ctrl.svc.Confirm(c.Request.Context(), id, ctrl.actor(c), req.ReviewNote)
	if err != nil {
		response.ErrorFromDB(c, err, "确认失败")
		return
	}
	detailMap := map[string]any{
		"action": "confirm", "draft_id": id, "review_note": req.ReviewNote,
		"actor_id": ctrl.actor(c), "applied": false,
	}
	if result != nil && result.Draft != nil {
		detailMap["status"] = result.Draft.Status
		detailMap["entity_type"] = result.Draft.EntityType
		detailMap["entity_id"] = result.Draft.EntityID
		detailMap["insight_type"] = result.Draft.InsightType
		detailMap["confidence"] = result.Draft.Confidence
		detailMap["evidence_count"] = evidenceCount(result.Draft.EvidenceJSON)
		detailMap["has_source_refs"] = len(bytesTrimSpace(result.Draft.SourceRefsJSON)) > 2
	}
	if result != nil {
		detailMap["applied"] = result.Applied
		if result.AppliedAction != nil {
			detailMap["applied_action"] = result.AppliedAction
		}
	}
	detail := "确认AI洞察草稿"
	if b, mErr := json.Marshal(detailMap); mErr == nil {
		detail = string(b)
	}
	ctrl.recordOp(c, "confirm", id, detail)
	response.Success(c, result, "已确认")
}

func (ctrl *AIInsightDraftController) Reject(c *gin.Context) {
	id := c.Param("id")
	var req aiInsightReviewReq
	_ = c.ShouldBindJSON(&req)
	draft, getErr := ctrl.svc.Get(c.Request.Context(), id)
	if err := ctrl.svc.Reject(c.Request.Context(), id, ctrl.actor(c), req.ReviewNote); err != nil {
		response.ErrorFromDB(c, err, "拒绝失败")
		return
	}
	detailMap := map[string]any{
		"action": "reject", "draft_id": id, "review_note": req.ReviewNote,
		"actor_id": ctrl.actor(c), "status": "rejected",
	}
	if getErr == nil && draft != nil {
		detailMap["entity_type"] = draft.EntityType
		detailMap["entity_id"] = draft.EntityID
		detailMap["insight_type"] = draft.InsightType
		detailMap["confidence"] = draft.Confidence
		detailMap["evidence_count"] = evidenceCount(draft.EvidenceJSON)
	}
	detail := "拒绝AI洞察草稿"
	if b, mErr := json.Marshal(detailMap); mErr == nil {
		detail = string(b)
	}
	ctrl.recordOp(c, "reject", id, detail)
	response.Success(c, gin.H{"id": id, "status": "rejected"}, "已拒绝")
}

func evidenceCount(raw json.RawMessage) int {
	b := bytesTrimSpace(raw)
	if len(b) == 0 || string(b) == "[]" || string(b) == "{}" || string(b) == "null" {
		return 0
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(b, &arr); err == nil {
		return len(arr)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(b, &obj); err == nil {
		return len(obj)
	}
	return 0
}

func bytesTrimSpace(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}

func (ctrl *AIInsightDraftController) GenerateStub(c *gin.Context) {
	var req service.AIInsightGenerateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "请求参数错误")
		return
	}
	item, err := ctrl.svc.GenerateStub(c.Request.Context(), req)
	if err != nil {
		response.ErrorFromDB(c, err, "生成AI洞察草稿失败")
		return
	}
	ctrl.recordOp(c, "generate_stub", item.DraftID, "生成只读AI洞察草稿stub")
	response.Success(c, item, "生成成功")
}
