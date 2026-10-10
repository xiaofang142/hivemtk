package controller

import (
	"bytes"
	"encoding/csv"
	"net/http"
	"strconv"
	"time"

	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// mcp_audit_export.go MCP 审计 CSV 导出（提交组6 A-4，2026-10-11）。
//
// 与列表同源筛选（start/end/team_id/client_id/tool/allowed/event 等）；
// 上限 10000 行（防一次性拖库把管理端打满）；明文钥匙与参数原文永不入导出
// （表里本来就没有——args_hash 是摘要，detail 是掩码形）。

const mcpAuditExportCap = 10000

func mcpCSVWrite(ctx *gin.Context, filename string, header []string, rows [][]string) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write(header)
	for _, r := range rows {
		_ = w.Write(r)
	}
	w.Flush()
	ctx.Header("Content-Disposition", "attachment; filename="+filename)
	ctx.Data(http.StatusOK, "text/csv; charset=utf-8", buf.Bytes())
}

// ExportMCPCallAudits GET /api/mcp/audits/export
func ExportMCPCallAudits(ctx *gin.Context) {
	svc := mcpAuditAdminSvc()
	if svc == nil || !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "审计服务未就绪")
		return
	}
	var allowed *bool
	switch ctx.Query("allowed") {
	case "1", "true":
		v := true
		allowed = &v
	case "0", "false":
		v := false
		allowed = &v
	}
	rows, _, err := svc.List(ctx.Request.Context(),
		ctx.Query("client_id"), ctx.Query("tool"), allowed, ctx.Query("event"),
		ctx.Query("start"), ctx.Query("end"), parseUintOrZero(ctx.Query("team_id")),
		1, mcpAuditExportCap)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "导出失败: "+err.Error())
		return
	}
	out := make([][]string, 0, len(rows))
	for _, r := range rows {
		event := r.Event
		if event == "" {
			event = "call"
		}
		keyID := ""
		if r.KeyID != nil {
			keyID = strconv.FormatUint(uint64(*r.KeyID), 10)
		}
		out = append(out, []string{
			strconv.FormatUint(uint64(r.ID), 10),
			r.CreatedAt.Format("2006-01-02 15:04:05"),
			r.ClientID,
			event,
			r.Method,
			r.Tool,
			boolCSV(r.Allowed),
			boolCSV(r.OK),
			r.ErrorMsg,
			r.IP,
			r.UserAgent,
			r.RequestID,
			strconv.FormatUint(uint64(r.TeamID), 10),
			keyID,
		})
	}
	mcpCSVWrite(ctx, "mcp-call-audits-"+time.Now().Format("20060102150405")+".csv",
		[]string{"id", "created_at", "client_id", "event", "method", "tool", "allowed", "ok",
			"error_msg", "ip", "user_agent", "request_id", "team_id", "key_id"},
		out)
}

// ExportMCPCredentialAuditLogs GET /api/mcp/admin-audits/export
func ExportMCPCredentialAuditLogs(ctx *gin.Context) {
	svc := mcpAdminAuditSvc()
	if svc == nil || !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "审计服务未就绪")
		return
	}
	var credID *uint
	if v := parseUintOrZero(ctx.Query("credential_id")); v > 0 {
		credID = &v
	}
	rows, _, err := svc.List(ctx.Request.Context(), ctx.Query("action"), ctx.Query("target_type"), credID,
		ctx.Query("operator"), ctx.Query("start"), ctx.Query("end"),
		parseUintOrZero(ctx.Query("team_id")), 1, mcpAuditExportCap)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "导出失败: "+err.Error())
		return
	}
	out := make([][]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, []string{
			strconv.FormatUint(uint64(r.ID), 10),
			r.CreatedAt.Format("2006-01-02 15:04:05"),
			r.Action,
			r.TargetType,
			strconv.FormatUint(uint64(r.TargetID), 10),
			r.TargetLabel,
			r.OperatorUsername,
			r.IP,
			r.UserAgent,
			r.RequestID,
			strconv.FormatUint(uint64(r.TeamID), 10),
			r.Detail,
		})
	}
	mcpCSVWrite(ctx, "mcp-admin-audits-"+time.Now().Format("20060102150405")+".csv",
		[]string{"id", "created_at", "action", "target_type", "target_id", "target_label",
			"operator", "ip", "user_agent", "request_id", "team_id", "detail"},
		out)
}

func boolCSV(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
