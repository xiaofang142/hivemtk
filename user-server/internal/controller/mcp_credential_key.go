package controller

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// mcp_credential_key.go MCP 凭证钥匙（1:N）管理端点（L3：controller → service → repository）。
//
// 端点族：凭证名下钥匙的 列表 / 签发 / 启用 / 禁用 / 停用(吊销) / 删除 / 明文查看。
// 每个变更动作（含 reveal 这个敏感 GET）都落一条 mcp_credential_audit_logs——
// 全局 AuditMiddleware 拦不到 GET，reveal 不记账等于"谁看过钥匙"永远无答案。

var (
	mcpKeySvc = func() *service.McpKeyAdminService { return service.NewMcpKeyAdminService() }
	// mcpAdminAuditSvc 管理审计记账服务（controller 可测试替换）。
	mcpAdminAuditSvc = func() *service.MCPCredentialAuditLogAdminService {
		return service.NewMCPCredentialAuditLogAdminService()
	}
)

// mcpKeyRow 列表/详情出参：掩码形 + 状态，永不出明文（明文只走 reveal/签发响应）。
type mcpKeyRow struct {
	ID         uint       `json:"id"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	KeyMasked  string     `json:"key_masked"`
	CanReveal  bool       `json:"can_reveal"`
	ExpiresAt  *time.Time `json:"expires_at"`
	Expired    bool       `json:"expired"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

func mcpKeyOut(k *model.MCPCredentialKey) mcpKeyRow {
	return mcpKeyRow{
		ID: k.ID, Name: k.Name, Status: k.Status, KeyMasked: k.MaskedKey(),
		CanReveal: k.KeyEnc != "", ExpiresAt: k.ExpiresAt,
		Expired: k.KeyExpired(time.Now()), LastUsedAt: k.LastUsedAt, CreatedAt: k.CreatedAt,
	}
}

// mcpOperatorOf 从 JWT 上下文取操作人（system 管理员引导路径可能没有 username）。
func mcpOperatorOf(ctx *gin.Context) (userID *uint, username, ip string) {
	if v, ok := ctx.Get("user_id"); ok {
		if id, ok2 := v.(uint); ok2 && id != 0 {
			userID = &id
		}
	}
	if v, ok := ctx.Get("username"); ok {
		username, _ = v.(string)
	}
	if username == "" {
		username = "system"
	}
	return userID, username, ctx.ClientIP()
}

// mcpWriteAdminAudit 记一条管理审计（best-effort）。
func mcpWriteAdminAudit(ctx *gin.Context, action, targetType string, targetID uint, credentialID *uint, label string, detail gin.H) {
	svc := mcpAdminAuditSvc()
	if svc == nil || !svc.Ready() {
		return
	}
	userID, username, ip := mcpOperatorOf(ctx)
	var teamID uint
	if v, ok := ctx.Get("team_id"); ok {
		if id, ok2 := v.(uint); ok2 {
			teamID = id
		}
	}
	reqID := ""
	if v, ok := ctx.Get("trace_id"); ok {
		reqID, _ = v.(string)
	}
	svc.Record(ctx.Request.Context(), &model.MCPCredentialAuditLog{
		Action: action, TargetType: targetType, TargetID: targetID,
		CredentialID: credentialID, TargetLabel: label, Detail: mcpAuditDetail(detail),
		OperatorUserID: userID, OperatorUsername: username, IP: ip,
		TeamID: teamID, UserAgent: ctx.Request.UserAgent(), RequestID: reqID,
	})
}

func mcpAuditDetail(kv gin.H) string {
	b, err := json.Marshal(kv)
	if err != nil {
		return ""
	}
	return string(b)
}

// mcpKeySvcError 业务错误 → HTTP 状态码（越权按 404 处理，不泄露存在性）。
func mcpKeySvcError(err error) (int, string) {
	switch {
	case errors.Is(err, service.ErrMCPKeyNotFound):
		return http.StatusNotFound, "钥匙不存在"
	case errors.Is(err, service.ErrMCPKeyRevokedTerminal):
		return http.StatusConflict, err.Error()
	case errors.Is(err, service.ErrMCPKeyNoPlaintext):
		return http.StatusConflict, err.Error()
	case errors.Is(err, service.ErrMCPKeyCapReached):
		return http.StatusConflict, err.Error()
	case errors.Is(err, service.ErrMCPKeyStatusInvalid):
		return http.StatusBadRequest, err.Error()
	default:
		return http.StatusInternalServerError, err.Error()
	}
}

// ListMCPCredentialKeys GET /api/mcp/credentials/:id/keys
func ListMCPCredentialKeys(ctx *gin.Context) {
	svc := mcpKeySvc()
	if !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	credID := parseUintOrZero(ctx.Param("id"))
	if credID == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效 id")
		return
	}
	cred, err := svc.GetCredential(ctx.Request.Context(), credID)
	if err != nil {
		response.Error(ctx, http.StatusNotFound, "凭证不存在")
		return
	}
	if teamID, platform := mcpManageScope(ctx); !platform && cred.TeamID != teamID {
		response.Error(ctx, http.StatusForbidden, "无权操作其他团队的凭证")
		return
	}
	if teamID, platform := mcpManageScope(ctx); !platform && cred.TeamID != teamID {
		response.Error(ctx, http.StatusForbidden, "无权操作其他团队的凭证")
		return
	}
	keys, err := svc.ListByCredential(ctx.Request.Context(), credID)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "查询钥匙失败: "+err.Error())
		return
	}
	out := make([]mcpKeyRow, 0, len(keys))
	for i := range keys {
		out = append(out, mcpKeyOut(&keys[i]))
	}
	response.Success(ctx, gin.H{"list": out, "credential_id": credID, "client_id": cred.ClientID}, "ok")
}

// CreateMCPCredentialKey POST /api/mcp/credentials/:id/keys
// body: {"name":"jenkins-bot","expires_at":"2027-01-01T00:00:00Z"} 或 {"ttl_days":90} / {"no_expiry":true}
// 明文只在本次响应出现一次；之后靠 reveal（明文已加密落库）显示/复制。
func CreateMCPCredentialKey(ctx *gin.Context) {
	svc := mcpKeySvc()
	if !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	credID := parseUintOrZero(ctx.Param("id"))
	if credID == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效 id")
		return
	}
	cred, err := svc.GetCredential(ctx.Request.Context(), credID)
	if err != nil {
		response.Error(ctx, http.StatusNotFound, "凭证不存在")
		return
	}
	if teamID, platform := mcpManageScope(ctx); !platform && cred.TeamID != teamID {
		response.Error(ctx, http.StatusForbidden, "无权操作其他团队的凭证")
		return
	}
	if teamID, platform := mcpManageScope(ctx); !platform && cred.TeamID != teamID {
		response.Error(ctx, http.StatusForbidden, "无权操作其他团队的凭证")
		return
	}
	var req struct {
		Name      string     `json:"name"`
		ExpiresAt *time.Time `json:"expires_at"`
		TTLDays   int        `json:"ttl_days"`
		NoExpiry  bool       `json:"no_expiry"`
	}
	_ = ctx.ShouldBindJSON(&req)
	// 过期缺省口径与凭证签发一致：默认 defaultCredentialTTL，no_expiry=true 才是永不过期。
	expiresAt := req.ExpiresAt
	if !req.NoExpiry && req.ExpiresAt == nil {
		days := req.TTLDays
		if days <= 0 {
			days = int(defaultCredentialTTL / (24 * time.Hour))
		}
		t := time.Now().Add(time.Duration(days) * 24 * time.Hour)
		expiresAt = &t
	}
	name := req.Name
	if name == "" {
		name = "密钥-" + time.Now().Format("01021504")
	}
	key, plaintext, err := svc.IssueKey(ctx.Request.Context(), credID, name, expiresAt)
	if err != nil {
		code, msg := mcpKeySvcError(err)
		response.Error(ctx, code, "签发失败: "+msg)
		return
	}
	middleware.InvalidateMCPCredentialCache()
	mcpWriteAdminAudit(ctx, model.MCPAuditActionKeyCreate, "key", key.ID, &credID, cred.ClientID, gin.H{
		"key_name": name, "key_masked": key.MaskedKey(), "expires_at": expiresAt,
	})
	response.Success(ctx, gin.H{
		"key":      mcpKeyOut(key),
		"api_key":  plaintext, // 明文仅此一次（此后走 reveal）
		"api_hint": "API Key 明文只完整显示这一次，之后可在列表中查看/复制已存储的明文",
	}, "签发成功")
}

// mcpKeyStatusAction 启用/禁用/停用三个动作的公共壳。
func mcpKeyStatusAction(ctx *gin.Context, to string, action string) {
	svc := mcpKeySvc()
	if !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	credID := parseUintOrZero(ctx.Param("id"))
	keyID := parseUintOrZero(ctx.Param("keyId"))
	if credID == 0 || keyID == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效 id")
		return
	}
	cred, err := svc.GetCredential(ctx.Request.Context(), credID)
	if err != nil {
		response.Error(ctx, http.StatusNotFound, "凭证不存在")
		return
	}
	if teamID, platform := mcpManageScope(ctx); !platform && cred.TeamID != teamID {
		response.Error(ctx, http.StatusForbidden, "无权操作其他团队的凭证")
		return
	}
	key, err := svc.SetKeyStatus(ctx.Request.Context(), credID, keyID, to)
	if err != nil {
		code, msg := mcpKeySvcError(err)
		response.Error(ctx, code, msg)
		return
	}
	middleware.InvalidateMCPCredentialCache()
	mcpWriteAdminAudit(ctx, action, "key", keyID, &credID, cred.ClientID, gin.H{
		"key_name": key.Name, "status": to,
	})
	response.Success(ctx, gin.H{"key": mcpKeyOut(key)}, "已更新")
}

// EnableMCPCredentialKey POST /api/mcp/credentials/:id/keys/:keyId/enable（disabled→active）
func EnableMCPCredentialKey(ctx *gin.Context) {
	mcpKeyStatusAction(ctx, model.MCPKeyStatusActive, model.MCPAuditActionKeyEnable)
}

// DisableMCPCredentialKey POST /api/mcp/credentials/:id/keys/:keyId/disable（active→disabled，可逆）
func DisableMCPCredentialKey(ctx *gin.Context) {
	mcpKeyStatusAction(ctx, model.MCPKeyStatusDisabled, model.MCPAuditActionKeyDisable)
}

// SuspendMCPCredentialKey POST /api/mcp/credentials/:id/keys/:keyId/suspend
// 系统级暂停（欠费/违规）；与管理员手动 disable 分离——审计能看清"谁停的"。
func SuspendMCPCredentialKey(ctx *gin.Context) {
	mcpKeyStatusAction(ctx, model.MCPKeyStatusSuspended, model.MCPAuditActionKeySuspend)
}

// RevokeMCPCredentialKey POST /api/mcp/credentials/:id/keys/:keyId/revoke（终态，不可逆）
func RevokeMCPCredentialKey(ctx *gin.Context) {
	mcpKeyStatusAction(ctx, model.MCPKeyStatusRevoked, model.MCPAuditActionKeyRevoke)
}

// RevealMCPCredentialKey GET /api/mcp/credentials/:id/keys/:keyId/reveal
// 敏感读：返回明文并强制落审计（谁、什么时候、看了哪把钥匙）。
func RevealMCPCredentialKey(ctx *gin.Context) {
	svc := mcpKeySvc()
	if !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	credID := parseUintOrZero(ctx.Param("id"))
	keyID := parseUintOrZero(ctx.Param("keyId"))
	if credID == 0 || keyID == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效 id")
		return
	}
	cred, err := svc.GetCredential(ctx.Request.Context(), credID)
	if err != nil {
		response.Error(ctx, http.StatusNotFound, "凭证不存在")
		return
	}
	if teamID, platform := mcpManageScope(ctx); !platform && cred.TeamID != teamID {
		response.Error(ctx, http.StatusForbidden, "无权操作其他团队的凭证")
		return
	}
	plain, err := svc.RevealKey(ctx.Request.Context(), credID, keyID)
	if err != nil {
		code, msg := mcpKeySvcError(err)
		response.Error(ctx, code, msg)
		return
	}
	mcpWriteAdminAudit(ctx, model.MCPAuditActionKeyReveal, "key", keyID, &credID, cred.ClientID, gin.H{
		"note": "明文被查看", "can_reveal": true,
	})
	response.Success(ctx, gin.H{"api_key": plain}, "ok")
}

// DeleteMCPCredentialKey DELETE /api/mcp/credentials/:id/keys/:keyId
func DeleteMCPCredentialKey(ctx *gin.Context) {
	svc := mcpKeySvc()
	if !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	credID := parseUintOrZero(ctx.Param("id"))
	keyID := parseUintOrZero(ctx.Param("keyId"))
	if credID == 0 || keyID == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效 id")
		return
	}
	cred, err := svc.GetCredential(ctx.Request.Context(), credID)
	if err != nil {
		response.Error(ctx, http.StatusNotFound, "凭证不存在")
		return
	}
	if teamID, platform := mcpManageScope(ctx); !platform && cred.TeamID != teamID {
		response.Error(ctx, http.StatusForbidden, "无权操作其他团队的凭证")
		return
	}
	key, err := svc.DeleteKey(ctx.Request.Context(), credID, keyID)
	if err != nil {
		code, msg := mcpKeySvcError(err)
		response.Error(ctx, code, msg)
		return
	}
	middleware.InvalidateMCPCredentialCache()
	mcpWriteAdminAudit(ctx, model.MCPAuditActionKeyDelete, "key", keyID, &credID, cred.ClientID, gin.H{
		"key_name": key.Name, "key_masked": key.MaskedKey(), "status": key.Status,
	})
	response.Success(ctx, gin.H{"ok": true}, "已删除")
}

// ListMCPCredentialAuditLogs GET /api/mcp/admin-audits?action=&target_type=&credential_id=&page=&size=
func ListMCPCredentialAuditLogs(ctx *gin.Context) {
	svc := mcpAdminAuditSvc()
	if svc == nil || !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "审计服务未就绪")
		return
	}
	page := parseIntOr(ctx.Query("page"), 1)
	size := parseIntOr(ctx.Query("size"), 20)
	var credID *uint
	if v := parseUintOrZero(ctx.Query("credential_id")); v > 0 {
		credID = &v
	}
	rows, total, err := svc.List(ctx.Request.Context(), ctx.Query("action"), ctx.Query("target_type"), credID,
		ctx.Query("operator"), ctx.Query("start"), ctx.Query("end"), parseUintOrZero(ctx.Query("team_id")), page, size)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "查询管理审计失败: "+err.Error())
		return
	}
	if rows == nil {
		rows = []model.MCPCredentialAuditLog{}
	}
	response.Success(ctx, gin.H{"list": rows, "total": total}, "ok")
}

// parseIntOr 解析整数查询参数，非法时取默认值。
func parseIntOr(s string, def int) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
	}
	if s == "" || n <= 0 {
		return def
	}
	return n
}
