package controller

import (
	"context"
	"io"
	"net/http"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/aiagent/mcp"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

type MCPController struct{}

// NewMCPController 构造 MCP controller。
func NewMCPController() *MCPController {
	return &MCPController{}
}

// Handle POST /api/mcp。
//
// 流程：
//  1. 读 request body（限制最大 1MB，超出按 400 处理）
//  2. 从 MCPGuard 注入的上下文取凭证作用域（client_id / credential_id / allowed_tools）
//  3. 新建 mcp.Server（per-request），挂 scope 过滤与审计钩子
//  4. 调用 HandleRequest 拿 JSON-RPC 响应字节
//  5. 写 Content-Type: application/json + 200 OK
//
// 错误处理：
//   - body 读取失败 → 400 + 错误响应
//   - mcp 内部错误：服务端已在 HandleRequest 内部返回 JSON-RPC error 响应，
//     此处不重复报错（HTTP 层仅保证 200 OK + JSON-RPC 语义错误码）。
func (c *MCPController) Handle(ctx *gin.Context) {
	const maxBody = 1 << 20
	body, err := io.ReadAll(io.LimitReader(ctx.Request.Body, maxBody+1))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "read body: " + err.Error()})
		return
	}
	if int64(len(body)) > maxBody {
		ctx.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
		return
	}

	mcpSrv := mcp.NewServer(tooluse.GetGlobalRegistry())

	// 凭证作用域（MCPGuard 写入的上下文键；单值链路取到空串=不限制）。
	clientID, _ := ctx.Get(middleware.MCPCtxClientID)
	credentialID, _ := ctx.Get(middleware.MCPCtxCredentialID)
	allowedTools, _ := ctx.Get(middleware.MCPCtxAllowedTools)
	if s, _ := allowedTools.(string); s != "" {
		mcpSrv.SetAllowedTools(s)
	}

	// 审计钩子：仅凭证对链路（clientID 非空）才落审计行——单值链路没有"哪一行凭证"，
	// 记了也无法对账到签发记录，反而把两套鉴权混在一张表里。
	if cid, _ := clientID.(string); cid != "" {
		keyID, _ := ctx.Get(middleware.MCPCtxKeyID)
		mcpSrv.SetAuditSink(func(rec mcp.AuditRecord) {
			rec.ClientID = cid
			c.auditWith(ctx.Request.Context(), rec, ctx.ClientIP(), credentialID, keyID)
		})
	}

	resp, err := mcpSrv.HandleRequest(ctx.Request.Context(), body)
	if err != nil {
		logger.Ctx(ctx.Request.Context()).Warn().Err(err).Msg("[MCP] handle request failed")
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx.Header("Content-Type", "application/json")
	ctx.Status(http.StatusOK)
	if _, werr := ctx.Writer.Write(resp); werr != nil {
		logger.Ctx(ctx.Request.Context()).Warn().Err(werr).Msg("[MCP] write response")
	}
}

// auditWith 审计落库（best-effort，失败只记日志不阻断工具调用）。
// credentialID/keyID 用于把审计行与签发记录对上（client_id 可被人工改，id 才是稳定键；
// 1:N 之后同一凭证可有多把钥匙，"哪把钥匙干的"只有 key_id 能回答）。
func (c *MCPController) auditWith(ctx context.Context, rec mcp.AuditRecord, ip string, credentialID, keyID any) {
	row := &model.MCPCallAudit{
		ClientID:  rec.ClientID,
		Method:    rec.Method,
		Tool:      rec.Tool,
		Allowed:   rec.Allowed,
		OK:        rec.OK,
		ErrorMsg:  rec.ErrorMsg,
		ArgsHash:  rec.ArgsHash,
		LatencyMS: rec.LatencyMS,
		IP:        ip,
		Event:     rec.Event,
	}
	if row.Event == "" {
		row.Event = "call"
	}
	if id, ok := credentialID.(uint); ok && id != 0 {
		row.CredentialID = &id
	}
	if id, ok := keyID.(uint); ok && id != 0 {
		row.KeyID = &id
	}
	svc := service.NewMCPCallAuditAdminService()
	if svc == nil || !svc.Ready() {
		return
	}
	svc.Record(ctx, row)
}
