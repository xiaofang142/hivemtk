package controller

import (
	"strconv"

	bizerr "hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// SecurityAuditController 安全审计接口：列表 / 立即审计 / 明细。
type SecurityAuditController struct {
	svc *service.SecurityAuditService
}

// NewSecurityAuditController 构造安全审计控制器
func NewSecurityAuditController(svc *service.SecurityAuditService) *SecurityAuditController {
	return &SecurityAuditController{svc: svc}
}

type runAuditReq struct {
	AuditName string `json:"audit_name"`
}

// ListSecurityAudits GET /api/security/audit/list
func (c *SecurityAuditController) ListSecurityAudits(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "10"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 10
	}

	if cursor, limit, useCursor := utils.ParseCursorParams(ctx, pageSize); useCursor {
		list, total, nextCursor, err := c.svc.ListAuditsKeyset(ctx.Request.Context(), cursor, limit)
		if err != nil {
			response.ErrorWithBusinessCode(ctx, bizerr.CodeInternal, "获取审计列表失败", gin.H{})
			return
		}
		response.Success(ctx, gin.H{
			"list":        list,
			"total":       total,
			"page_size":   limit,
			"next_cursor": nextCursor,
		}, "ok")
		return
	}

	list, total, err := c.svc.ListAudits(ctx.Request.Context(), page, pageSize)
	if err != nil {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeInternal, "获取审计列表失败", gin.H{})
		return
	}
	response.SuccessWithPage(ctx, list, int64(page), int64(pageSize), total)
}

// RunSecurityAudit POST /api/security/audit
func (c *SecurityAuditController) RunSecurityAudit(ctx *gin.Context) {
	var req runAuditReq
	_ = ctx.ShouldBindJSON(&req)
	audit, err := c.svc.RunAudit(ctx.Request.Context(), req.AuditName)
	if err != nil {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeInternal, "执行审计失败", gin.H{})
		return
	}
	response.Success(ctx, audit, "ok")
}

// GetSecurityAudit GET /api/security/audit/:id
func (c *SecurityAuditController) GetSecurityAudit(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeParamInvalid, "无效的审计ID", gin.H{})
		return
	}
	audit, err := c.svc.GetAuditDetail(ctx.Request.Context(), uint(id))
	if err != nil {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeNotFound, "审计记录不存在", gin.H{})
		return
	}
	response.Success(ctx, audit, "ok")
}
