package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/identity"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

// LeadController US2 线索管理端统一入口（/api/manage/leads/*）。
// 仅解析入参、调 Service、组响应；不碰 DB、不 import repository/gorm。
type LeadController struct {
	rules    *service.LeadRuleService
	evidence *service.LeadEvidenceService
	scoped   *service.LeadScopedListService
	archive  *service.LeadClueArchiveService
	routes   *service.LeadMiningNotifyRouteService
}

func NewLeadController(
	rules *service.LeadRuleService,
	evidence *service.LeadEvidenceService,
	scoped *service.LeadScopedListService,
	archive *service.LeadClueArchiveService,
	routes *service.LeadMiningNotifyRouteService,
) *LeadController {
	return &LeadController{rules: rules, evidence: evidence, scoped: scoped, archive: archive, routes: routes}
}

// leadRuleRequest 三粒度规则入参（写在 controller 内，避免冗余 _dto.go 文件）。
type leadRuleRequest struct {
	ID                int64    `json:"id"`
	Scope             string   `json:"scope"`
	ScopeID           uint     `json:"scope_id"`
	Keywords          []string `json:"keywords"`
	SemanticKeywords  []string `json:"semantic_keywords"`
	SystemDescription string   `json:"system_description"`
	Enabled           bool     `json:"enabled"`
}

func (c *LeadController) ListRules(ctx *gin.Context) {
	items, err := c.rules.List(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取线索规则失败")
		return
	}
	response.Success(ctx, items, "ok")
}

func (c *LeadController) SaveRule(ctx *gin.Context) {
	var req leadRuleRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误")
		return
	}
	m := &model.LeadMiningRule{
		ID: req.ID, Scope: req.Scope, ScopeID: req.ScopeID,
		Keywords: req.Keywords, SemanticKeywords: req.SemanticKeywords,
		SystemDescription: req.SystemDescription, Enabled: req.Enabled,
	}
	if err := c.rules.Save(ctx.Request.Context(), m); err != nil {
		response.Error(ctx, http.StatusBadRequest, "保存线索规则失败")
		return
	}
	response.Success(ctx, m, "ok")
}

func (c *LeadController) DeleteRule(ctx *gin.Context) {
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(ctx, http.StatusBadRequest, "规则ID非法")
		return
	}
	if err := c.rules.Delete(ctx.Request.Context(), id); err != nil {
		response.ErrorFromDB(ctx, err, "删除线索规则失败")
		return
	}
	response.Success(ctx, gin.H{"id": id}, "ok")
}

func (c *LeadController) ListNotifyRoutes(ctx *gin.Context) {
	items, err := c.routes.List(ctx.Request.Context(), ctx.Query("channel"), ctx.Query("account_id"))
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取通知路由失败")
		return
	}
	response.Success(ctx, items, "ok")
}

func (c *LeadController) CreateNotifyRoute(ctx *gin.Context) {
	var m model.LeadMiningNotifyRoute
	if err := ctx.ShouldBindJSON(&m); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误")
		return
	}
	if tid, ok := tenant.FromContext(ctx.Request.Context()); ok {
		m.TenantID = tid
	}
	if err := c.routes.Create(ctx.Request.Context(), &m); err != nil {
		response.ErrorFromDB(ctx, err, "创建通知路由失败")
		return
	}
	response.Success(ctx, m, "ok")
}

func (c *LeadController) DeleteNotifyRoute(ctx *gin.Context) {
	if err := c.routes.Delete(ctx.Request.Context(), ctx.Param("route_id")); err != nil {
		response.ErrorFromDB(ctx, err, "删除通知路由失败")
		return
	}
	response.Success(ctx, gin.H{"route_id": ctx.Param("route_id")}, "ok")
}

func (c *LeadController) ListScopedMessages(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))
	var matched *bool
	if v := ctx.Query("matched"); v != "" {
		b := v == "1" || v == "true"
		matched = &b
	}
	items, total, err := c.scoped.ListMessages(ctx.Request.Context(), getUserIDFromContext(ctx), page, limit,
		ctx.Query("platform"), ctx.Query("lead_role"), matched)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取权限内消息失败")
		return
	}
	response.Success(ctx, gin.H{"list": items, "total": total}, "ok")
}

func (c *LeadController) ListScopedCustomers(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))
	items, total, err := c.scoped.ListCustomers(ctx.Request.Context(), getUserIDFromContext(ctx), page, limit, ctx.Query("keyword"))
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取权限内客户失败")
		return
	}
	response.Success(ctx, gin.H{"list": items, "total": total}, "ok")
}

func (c *LeadController) GetJudgementHits(ctx *gin.Context) {
	judgementID := strings.TrimSpace(ctx.Param("judgement_id"))
	if judgementID == "" {
		response.Error(ctx, http.StatusBadRequest, "judgement_id 不能为空")
		return
	}
	hits, err := c.evidence.Hits(ctx.Request.Context(), judgementID)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取判定证据失败")
		return
	}
	response.Success(ctx, hits, "ok")
}

type leadArchiveRequest struct {
	ClueID          string               `json:"clue_id"`
	MessageID       string               `json:"message_id"`
	JudgementID     string               `json:"judgement_id"`
	SourceChannel   string               `json:"source_channel"`
	SourceAccountID string               `json:"source_account_id"`
	CustomerID      string               `json:"customer_id"`
	Identifiers     identity.Identifiers `json:"identifiers"`
	Name            string               `json:"name"`
	Industry        string               `json:"industry"`
	OwnerUserID     uint                 `json:"owner_user_id"`
	UserInfo        map[string]any       `json:"user_info"`
	SourceHitIDs    []string             `json:"source_hit_ids"`
	Role            string               `json:"role"`
	Summary         string               `json:"summary"`
	EvidenceJSON    string               `json:"evidence_json"`
}

func (c *LeadController) Archive(ctx *gin.Context) {
	var req leadArchiveRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误")
		return
	}
	if strings.TrimSpace(req.ClueID) == "" {
		response.Error(ctx, http.StatusBadRequest, "clue_id 不能为空")
		return
	}
	result, err := c.archive.Archive(ctx.Request.Context(), service.LeadClueArchiveInput{
		ClueID: req.ClueID, MessageID: req.MessageID, JudgementID: req.JudgementID,
		SourceChannel: req.SourceChannel, SourceAccountID: req.SourceAccountID,
		CustomerID: req.CustomerID, Identifiers: req.Identifiers,
		Name: req.Name, Industry: req.Industry, OwnerUserID: req.OwnerUserID,
		UserInfo: model.JSONPayload(req.UserInfo), SourceHitIDs: model.JSONStrings(req.SourceHitIDs),
		Role: req.Role, Summary: req.Summary, EvidenceJSON: req.EvidenceJSON,
	})
	if err != nil {
		response.ErrorFromDB(ctx, err, "归档线索失败")
		return
	}
	response.Success(ctx, result, "ok")
}
