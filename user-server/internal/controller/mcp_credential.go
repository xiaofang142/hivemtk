package controller

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"

	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// mcp_credential.go MCP 凭证对管理端（ClientID + APIKey）
// DB 访问走 repository（L3 分层约束）。
// 签发：client_id = "mcp-" + 8hex；api_key = "wgk_" + 32hex（明文只在创建响应出现一次，
// 库里只存 sha256）。吊销 = enabled=false 或删除，InvalidateMCPCredentialCache 即刻生效。

type mcpCredentialRow struct {
	ID         uint       `json:"id"`
	ClientID   string     `json:"client_id"`
	Name       string     `json:"name"`
	Enabled    bool       `json:"enabled"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

// ListMCPCredentials GET /api/mcp/credentials
var mcpAdminSvc = func() *service.McpCredentialAdminService { return service.NewMcpCredentialAdminService() }

func ListMCPCredentials(ctx *gin.Context) {
	if !mcpAdminSvc().Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	rows, err := mcpAdminSvc().List(ctx.Request.Context(), 200)
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	out := make([]mcpCredentialRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, mcpCredentialRow{ID: r.ID, ClientID: r.ClientID, Name: r.Name, Enabled: r.Enabled, LastUsedAt: r.LastUsedAt, CreatedAt: r.CreatedAt})
	}
	response.Success(ctx, out, "ok")
}

// CreateMCPCredential POST /api/mcp/credentials  body: {"name":"..."}
func CreateMCPCredential(ctx *gin.Context) {
	if !mcpAdminSvc().Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	_ = ctx.ShouldBindJSON(&req)

	clientID := "mcp-" + randHex(4)
	apiKey := "wgk_" + randHex(16)
	sum := sha256.Sum256([]byte(apiKey))
	cred := model.MCPCredential{
		ClientID:   clientID,
		Name:       req.Name,
		APIKeyHash: hex.EncodeToString(sum[:]),
		Enabled:    true,
	}
	if err := mcpAdminSvc().Create(ctx.Request.Context(), &cred); err != nil {
		response.Error(ctx, http.StatusInternalServerError, "创建失败: "+err.Error())
		return
	}
	// 新建也要清缓存：门那侧的缓存按 client_id 单键存"当时全库唯一归属"，
	// 第二个租户签发同名的 client_id 会让旧租户的条目把新凭证继续判成歧义/停用。
	middleware.InvalidateMCPCredentialCache()
	response.Success(ctx, gin.H{
		"id": cred.ID, "client_id": clientID, "name": cred.Name,
		"api_key":  apiKey, // 明文仅此一次
		"api_hint": "API Key 只显示这一次，请立即保存",
	}, "创建成功")
}

// UpdateMCPCredential PUT /api/mcp/credentials/:id  body: {"enabled":bool} 或 {"name":"..."}
func UpdateMCPCredential(ctx *gin.Context) {
	id := parseUintOrZero(ctx.Param("id"))
	if id == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效 id")
		return
	}
	var req struct {
		Enabled *bool   `json:"enabled"`
		Name    *string `json:"name"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	updates := map[string]any{}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if len(updates) == 0 {
		response.Error(ctx, http.StatusBadRequest, "无可更新字段")
		return
	}
	if err := mcpAdminSvc().Update(ctx.Request.Context(), id, updates); err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	middleware.InvalidateMCPCredentialCache()
	response.Success(ctx, gin.H{"ok": true}, "已更新")
}

// DeleteMCPCredential DELETE /api/mcp/credentials/:id
func DeleteMCPCredential(ctx *gin.Context) {
	id := parseUintOrZero(ctx.Param("id"))
	if id == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效 id")
		return
	}
	if err := mcpAdminSvc().Delete(ctx.Request.Context(), id); err != nil {
		// 不存在 ≠ 服务故障：ErrorFromDB 会把 gorm.ErrRecordNotFound 映射成 404（A16）
		response.ErrorFromDB(ctx, err, "MCP 凭据不存在")
		return
	}
	middleware.InvalidateMCPCredentialCache()
	response.Success(ctx, gin.H{"ok": true}, "已删除")
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
