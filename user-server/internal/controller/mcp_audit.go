package controller

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// mcp_audit.go MCP 调用审计管理端点（L3：controller → service → repository）。
//
// 与 mcp_credential.go 同层同风格：controller 只做参数解析与响应组装，
// 数据访问全在 service/repository，不直连 DB。

// mcpAuditAdminSvc 审计服务句柄（controller 可测试替换）。
var mcpAuditAdminSvc = func() *service.MCPCallAuditAdminService { return service.NewMCPCallAuditAdminService() }

// ListMCPCallAudits GET /api/manage/mcp/audits?client_id=&tool=&allowed=&page=&size=
//
// allowed 三态：缺省=不限；"1"/"0" 分别只查允许/被拒（值与 Allowed 列一致）。
func ListMCPCallAudits(ctx *gin.Context) {
	svc := mcpAuditAdminSvc()
	if svc == nil || !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "审计服务未就绪")
		return
	}
	page, _ := strconv.Atoi(ctx.Query("page"))
	size, _ := strconv.Atoi(ctx.Query("size"))
	var allowed *bool
	switch ctx.Query("allowed") {
	case "1", "true":
		v := true
		allowed = &v
	case "0", "false":
		v := false
		allowed = &v
	}
	rows, total, err := svc.List(ctx.Request.Context(),
		ctx.Query("client_id"), ctx.Query("tool"), allowed, page, size)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "查询审计失败: "+err.Error())
		return
	}
	type row struct {
		ID           uint       `json:"id"`
		ClientID     string     `json:"client_id"`
		CredentialID *uint      `json:"credential_id"`
		Method       string     `json:"method"`
		Tool         string     `json:"tool"`
		Allowed      bool       `json:"allowed"`
		OK           bool       `json:"ok"`
		ErrorMsg     string     `json:"error_msg"`
		ArgsHash     string     `json:"args_hash"`
		LatencyMS    int64      `json:"latency_ms"`
		IP           string     `json:"ip"`
		CreatedAt    time.Time  `json:"created_at"`
	}
	out := make([]row, 0, len(rows))
	for _, r := range rows {
		out = append(out, row{ID: r.ID, ClientID: r.ClientID, CredentialID: r.CredentialID,
			Method: r.Method, Tool: r.Tool, Allowed: r.Allowed, OK: r.OK,
			ErrorMsg: r.ErrorMsg, ArgsHash: r.ArgsHash, LatencyMS: r.LatencyMS,
			IP: r.IP, CreatedAt: r.CreatedAt})
	}
	response.Success(ctx, gin.H{"list": out, "total": total}, "ok")
}

// =============================================================================
// Skill 注册表管理
// =============================================================================

// mcpSkillAdminSvc Skill 服务句柄（controller 可测试替换）。
var mcpSkillAdminSvc = func() *service.MCPSkillAdminService { return service.NewMCPSkillAdminService() }

// ListMCPSkills GET /api/manage/mcp/skills
func ListMCPSkills(ctx *gin.Context) {
	svc := mcpSkillAdminSvc()
	if svc == nil || !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "Skill 服务未就绪")
		return
	}
	rows, err := svc.List(ctx.Request.Context(), 200)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "查询 Skill 失败: "+err.Error())
		return
	}
	if rows == nil {
		rows = []model.MCPSkill{}
	}
	response.Success(ctx, gin.H{"list": rows}, "ok")
}

// GetMCPSkill GET /api/manage/mcp/skills/:id
func GetMCPSkill(ctx *gin.Context) {
	svc := mcpSkillAdminSvc()
	if svc == nil || !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "Skill 服务未就绪")
		return
	}
	id := parseUintOrZero(ctx.Param("id"))
	if id == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效 id")
		return
	}
	row, err := svc.GetByID(ctx.Request.Context(), id)
	if err != nil {
		response.Error(ctx, http.StatusNotFound, "Skill 不存在")
		return
	}
	response.Success(ctx, gin.H{"detail": row}, "ok")
}

// CreateMCPSkill POST /api/manage/mcp/skills
// body: {"name":"...","description":"...","credential_id":1,"allowed_tools":"a,b","doc_url":"...","guardrails":"..."}
// 保存时若 (skill.allowed_tools, credential_id) 均给出，则把工具集同步进绑定凭证的 scope
// —— Skill 期望什么，凭证就只放行什么（Skill 是使用面，凭证是执行面，两者必须一致）。
func CreateMCPSkill(ctx *gin.Context) {
	svc := mcpSkillAdminSvc()
	if svc == nil || !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "Skill 服务未就绪")
		return
	}
	var req struct {
		Name         string `json:"name"`
		Description  string `json:"description"`
		CredentialID *uint  `json:"credential_id"`
		AllowedTools string `json:"allowed_tools"`
		DocURL       string `json:"doc_url"`
		Guardrails   string `json:"guardrails"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求体解析失败: "+err.Error())
		return
	}
	if req.Name == "" {
		response.Error(ctx, http.StatusBadRequest, "name 必填")
		return
	}
	row := &model.MCPSkill{
		Name:         req.Name,
		Description:  req.Description,
		CredentialID: req.CredentialID,
		AllowedTools: req.AllowedTools,
		DocURL:       req.DocURL,
		Guardrails:   req.Guardrails,
		Enabled:      true,
	}
	if err := svc.Create(ctx.Request.Context(), row); err != nil {
		response.Error(ctx, http.StatusInternalServerError, "创建 Skill 失败: "+err.Error())
		return
	}
	syncCredentialScope(ctx.Request.Context(), req.CredentialID, req.AllowedTools)
	response.Success(ctx, gin.H{"id": row.ID, "name": row.Name}, "创建成功")
}

// UpdateMCPSkill PUT /api/manage/mcp/skills/:id
// 同 Create 语义；AllowedTools 变更同样回写绑定凭证的 scope。
func UpdateMCPSkill(ctx *gin.Context) {
	svc := mcpSkillAdminSvc()
	if svc == nil || !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "Skill 服务未就绪")
		return
	}
	id := parseUintOrZero(ctx.Param("id"))
	if id == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效 id")
		return
	}
	var req struct {
		Name         *string `json:"name"`
		Description  *string `json:"description"`
		CredentialID *uint   `json:"credential_id"`
		AllowedTools *string `json:"allowed_tools"`
		DocURL       *string `json:"doc_url"`
		Guardrails   *string `json:"guardrails"`
		Enabled      *bool   `json:"enabled"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求体解析失败: "+err.Error())
		return
	}
	updates := map[string]any{}
	if req.Name != nil {
		if *req.Name == "" {
			response.Error(ctx, http.StatusBadRequest, "name 不能为空")
			return
		}
		updates["name"] = *req.Name
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}
	if req.CredentialID != nil {
		updates["credential_id"] = *req.CredentialID
	}
	if req.AllowedTools != nil {
		updates["allowed_tools"] = *req.AllowedTools
	}
	if req.DocURL != nil {
		updates["doc_url"] = *req.DocURL
	}
	if req.Guardrails != nil {
		updates["guardrails"] = *req.Guardrails
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}
	if len(updates) == 0 {
		response.Error(ctx, http.StatusBadRequest, "无可更新字段")
		return
	}
	if err := svc.Update(ctx.Request.Context(), id, updates); err != nil {
		response.Error(ctx, http.StatusInternalServerError, "更新 Skill 失败: "+err.Error())
		return
	}
	if req.AllowedTools != nil {
		// 更新后与当前绑定凭证不一致时，以新允许集为准回写凭证 scope。
		cur, err := svc.GetByID(ctx.Request.Context(), id)
		if err == nil {
			syncCredentialScope(ctx.Request.Context(), cur.CredentialID, *req.AllowedTools)
		}
	}
	response.Success(ctx, gin.H{"ok": true}, "已更新")
}

// DeleteMCPSkill DELETE /api/manage/mcp/skills/:id
func DeleteMCPSkill(ctx *gin.Context) {
	svc := mcpSkillAdminSvc()
	if svc == nil || !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "Skill 服务未就绪")
		return
	}
	id := parseUintOrZero(ctx.Param("id"))
	if id == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效 id")
		return
	}
	if err := svc.Delete(ctx.Request.Context(), id); err != nil {
		response.Error(ctx, http.StatusInternalServerError, "删除 Skill 失败: "+err.Error())
		return
	}
	response.Success(ctx, gin.H{"ok": true}, "已删除")
}

// syncCredentialScope 把 Skill 的工具集同步进绑定凭证的 scope。
//
// best-effort：凭证操作失败只记日志不上抛（Skill 记录已入库，scope 下次保存
// 可再覆盖；而凭证 scope 为空=不限制兜底，不会把 Skill 打到"想用不能用"）。
func syncCredentialScope(ctx context.Context, credentialID *uint, allowedTools string) {
	if credentialID == nil || *credentialID == 0 || allowedTools == "" {
		return
	}
	svc := mcpAdminSvc()
	if svc == nil || !svc.Ready() {
		return
	}
	if err := svc.Update(ctx, *credentialID, map[string]any{"allowed_tools": allowedTools}); err != nil {
		logger.Ctx(ctx).Warn().Err(err).Msgf("同步 Skill 工具集到凭证失败 credential=%d", *credentialID)
	}
	middleware.InvalidateMCPCredentialCache()
}