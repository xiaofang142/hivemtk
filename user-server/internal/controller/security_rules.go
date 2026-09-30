package controller

import (
	"time"

	"github.com/gin-gonic/gin"

	bizerr "hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

// SecurityRulesController 安全规则配置接口：敏感词过滤 / 输出护栏 / 数据脱敏。
type SecurityRulesController struct {
	svc service.SecurityRulesService
}

// NewSecurityRulesController 构造安全规则配置控制器。
func NewSecurityRulesController(svc service.SecurityRulesService) *SecurityRulesController {
	return &SecurityRulesController{svc: svc}
}

// GetRules GET /api/security/rules
func (c *SecurityRulesController) GetRules(ctx *gin.Context) {
	if !c.svc.Available() {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeInternal, "安全规则存储不可用", gin.H{})
		return
	}
	rules, err := c.svc.Get(ctx.Request.Context())
	if err != nil {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeInternal, "读取安全规则失败", gin.H{})
		return
	}
	response.Success(ctx, rules, "ok")
}

type putSecurityRulesReq struct {
	SensitiveWords     map[string]int `json:"sensitive_words"`
	OutputGuardEnabled *bool          `json:"output_guard_enabled"`
	PIIMaskEnabled     *bool          `json:"pii_mask_enabled"`
}

// PutRules PUT /api/security/rules
func (c *SecurityRulesController) PutRules(ctx *gin.Context) {
	if !c.svc.Available() {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeInternal, "安全规则存储不可用", gin.H{})
		return
	}
	var req putSecurityRulesReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeParamInvalid, "参数错误", gin.H{})
		return
	}

	current, err := c.svc.Get(ctx.Request.Context())
	if err != nil {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeInternal, "读取安全规则失败", gin.H{})
		return
	}
	next := &model.SecurityRules{
		SensitiveWords:     req.SensitiveWords,
		OutputGuardEnabled: current.OutputGuardEnabled,
		PIIMaskEnabled:     current.PIIMaskEnabled,
		UpdatedAt:          time.Now(),
	}
	if req.OutputGuardEnabled != nil {
		next.OutputGuardEnabled = *req.OutputGuardEnabled
	}
	if req.PIIMaskEnabled != nil {
		next.PIIMaskEnabled = *req.PIIMaskEnabled
	}

	saved, err := c.svc.Put(ctx.Request.Context(), next)
	if err != nil {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeInternal, "保存安全规则失败", gin.H{})
		return
	}
	response.Success(ctx, saved, "ok")
}
