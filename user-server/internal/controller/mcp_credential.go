package controller

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// mcp_credential.go MCP 凭证对管理端（ClientID + APIKey）
// DB 访问走 repository（L3 分层约束）。
// 签发：client_id = "mcp-" + 8hex（可手动指定）；api_key = "wgk_" + 32hex（明文只在创建响应出现一次，
// 库里只存 sha256）。吊销 = enabled=false 或删除，InvalidateMCPCredentialCache 即刻生效。
// 账户绑定：OwnerUserID 指向 system_users.id（NULL=未绑定），角色/权限/功能/业务/付费随后续 RBAC
// 从账户侧继承；支持自定义过期时间（默认给 defaultCredentialTTL）。

// defaultCredentialTTL 签发凭证的默认过期时长。
// 与仓库约定一致处：凭证是外部 AI Skill/Agent 的"长期钥匙"，但长期≠永久——
// 不给默认过期会让"忘了吊销"的钥匙永久有效，违规审计时无人能答"这把为什么还在"。
const defaultCredentialTTL = 90 * 24 * time.Hour

type mcpCredentialRow struct {
	ID           uint       `json:"id"`
	ClientID     string     `json:"client_id"`
	Name         string     `json:"name"`
	Enabled      bool       `json:"enabled"`
	OwnerUserID  *uint      `json:"owner_user_id"`
	ExpiresAt    *time.Time `json:"expires_at"`
	AllowedTools string     `json:"allowed_tools"`
	LastUsedAt   *time.Time `json:"last_used_at"`
	CreatedAt    time.Time  `json:"created_at"`
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
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]mcpCredentialRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, mcpCredentialRow{ID: r.ID, ClientID: r.ClientID, Name: r.Name, Enabled: r.Enabled,
			OwnerUserID: r.OwnerUserID, ExpiresAt: r.ExpiresAt, AllowedTools: r.AllowedTools,
			LastUsedAt: r.LastUsedAt, CreatedAt: r.CreatedAt})
	}
	response.Success(ctx, out, "ok")
}

// CreateMCPCredential POST /api/mcp/credentials
// body: {"name":"...","owner_user_id":1,"expires_at":"2027-01-01T00:00:00Z","ttl_days":90,"client_id":"my-agent"}
//   - client_id 可手动指定（"mcp-" 开头，8~64 字符，唯一）；缺省自动生成 "mcp-"+8hex。
//   - expires_at 与 ttl_days 二选一：都给了以 expires_at 为准；都不给默认 defaultCredentialTTL。
//   - owner_user_id 绑定账户（NULL=不绑定）；绑定不校验账户存在性回调侧由 RBAC P0 的
//     team 归属统一管理（本期只落绑定关系，解绑/净化在后。
func CreateMCPCredential(ctx *gin.Context) {
	if !mcpAdminSvc().Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	var req struct {
		Name        string     `json:"name"`
		ClientID    string     `json:"client_id"`
		OwnerUserID *uint      `json:"owner_user_id"`
		ExpiresAt   *time.Time `json:"expires_at"`
		TTLDays     int        `json:"ttl_days"`
		NoExpiry    bool       `json:"no_expiry"`
	}
	_ = ctx.ShouldBindJSON(&req)

	clientID := strings.TrimSpace(req.ClientID)
	if clientID == "" {
		clientID = "mcp-" + randHex(4)
	} else if len(clientID) < 4 || len(clientID) > 64 {
		response.Error(ctx, http.StatusBadRequest, "client_id 长度须 4~64")
		return
	}
	var expiresAt *time.Time
	if !req.NoExpiry && req.ExpiresAt == nil {
		days := req.TTLDays
		if days <= 0 {
			days = int(defaultCredentialTTL / (24 * time.Hour))
		}
		t := time.Now().Add(time.Duration(days) * 24 * time.Hour)
		expiresAt = &t
	} else {
		// 显式 no_expiry=true 或给了绝对时间都直接用（给了绝对时间时 ttl_days 忽略）。
		expiresAt = req.ExpiresAt
	}
	// 1:N 之后凭证行不再自带钥匙：先落凭证，再经 IssueKey 签发第一把钥匙
	// （sha256+密文双轨入 mcp_credential_keys，hash 同时镜像进凭证 legacy 列）。
	cred := model.MCPCredential{
		ClientID:    clientID,
		Name:        req.Name,
		Enabled:     true,
		OwnerUserID: req.OwnerUserID,
		ExpiresAt:   expiresAt,
	}
	if err := mcpAdminSvc().Create(ctx.Request.Context(), &cred); err != nil {
		response.Error(ctx, http.StatusInternalServerError, "创建失败: "+err.Error())
		return
	}
	_, apiKey, err := mcpKeySvc().IssueKey(ctx.Request.Context(), cred.ID, "初始密钥", expiresAt)
	if err != nil {
		// 钥匙没签出来的凭证是一把打不开的锁：回滚删除，保持"签发成功=钥匙可用"的原子性。
		_ = mcpAdminSvc().Delete(ctx.Request.Context(), cred.ID)
		response.Error(ctx, http.StatusInternalServerError, "签发钥匙失败: "+err.Error())
		return
	}
	mcpWriteAdminAudit(ctx, model.MCPAuditActionCredentialCreate, "credential", cred.ID, &cred.ID, clientID, gin.H{
		"name": cred.Name, "key_name": "初始密钥", "expires_at": expiresAt, "owner_user_id": req.OwnerUserID,
	})
	middleware.InvalidateMCPCredentialCache()
	response.Success(ctx, gin.H{
		"id": cred.ID, "client_id": clientID, "name": cred.Name,
		"api_key":  apiKey, // 明文仅此一次（此后可在钥匙列表 reveal）
		"api_hint": "API Key 明文只完整显示这一次，请立即保存；之后可在钥匙列表查看/复制",
	}, "创建成功")
}

// UpdateMCPCredential PUT /api/mcp/credentials/:id
// body 可含：{"enabled":bool,"name":"...","client_id":"...","owner_user_id":1,
//
//	"expires_at":"2027-01-01T00:00:00Z","allowed_tools":"customer.read,reach.send"}
//
// client_id 修改后旧的立即失效（缓存失效），外部调用方必须换新 ID。
func UpdateMCPCredential(ctx *gin.Context) {
	id := parseUintOrZero(ctx.Param("id"))
	if id == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效 id")
		return
	}
	var req struct {
		Enabled      *bool      `json:"enabled"`
		Name         *string    `json:"name"`
		ClientID     *string    `json:"client_id"`
		OwnerUserID  *uint      `json:"owner_user_id"`
		ExpiresAt    *time.Time `json:"expires_at"`
		ClearExpires bool       `json:"clear_expires"`
		AllowedTools *string    `json:"allowed_tools"`
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
	if req.ClientID != nil {
		cid := strings.TrimSpace(*req.ClientID)
		if len(cid) < 4 || len(cid) > 64 {
			response.Error(ctx, http.StatusBadRequest, "client_id 长度须 4~64")
			return
		}
		updates["client_id"] = cid
	}
	if req.OwnerUserID != nil {
		updates["owner_user_id"] = *req.OwnerUserID
	}
	if req.ClearExpires {
		// 显式清除过期=永不过期。与"没传"（保持现状）是两件事，
		// 靠独立布尔区分——null 与缺省在 JSON 里等价，不能只靠指针判。
		updates["expires_at"] = nil
	} else if req.ExpiresAt != nil {
		if req.ExpiresAt.Before(time.Now()) {
			response.Error(ctx, http.StatusBadRequest, "expires_at 不能早于当前时间（续期请传未来时间）")
			return
		}
		updates["expires_at"] = *req.ExpiresAt
	}
	if req.AllowedTools != nil {
		updates["allowed_tools"] = *req.AllowedTools
	}
	if len(updates) == 0 {
		response.Error(ctx, http.StatusBadRequest, "无可更新字段")
		return
	}
	if err := mcpAdminSvc().Update(ctx.Request.Context(), id, updates); err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	mcpWriteAdminAudit(ctx, model.MCPAuditActionCredentialUpdate, "credential", id, &id, "", gin.H{
		"fields": updates,
	})
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
	// 审计标签尽力取（db 未装配时 keysvc 未就绪，跳过即可——删除本身由
	// mcpAdminSvc 的 repo 报错兜底，不能让标签查询的空服务 panic 掉主流程）。
	var clientID string
	if ks := mcpKeySvc(); ks.Ready() {
		if cred, err := ks.GetCredential(ctx.Request.Context(), id); err == nil {
			clientID = cred.ClientID
		}
	}
	if err := mcpAdminSvc().Delete(ctx.Request.Context(), id); err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	mcpWriteAdminAudit(ctx, model.MCPAuditActionCredentialDelete, "credential", id, &id, clientID, gin.H{
		"client_id": clientID, "cascade_keys": true,
	})
	middleware.InvalidateMCPCredentialCache()
	response.Success(ctx, gin.H{"ok": true}, "已删除")
}

// randHex 随机 hex（client_id 自动生成的最后一个使用者；API Key 明文生成
// 已随签发职责移入 service.generateMCPKeyPlaintext，与 hash/密文双轨落库同事务语义）。
func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
