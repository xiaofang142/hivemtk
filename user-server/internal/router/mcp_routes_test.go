package router

import (
	"testing"

	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
)

// TestSetup_MCPAdminRoutes MCP 鉴权四件套的管理路由必须在注册表（2026-10-10，钥匙族 2026-10-11 扩）。
//
// 判据同时在两个方向拦回退：删路由（管理面失明）与加错前缀（/llm/mcp/... 死副本）都会红。
func TestSetup_MCPAdminRoutes(t *testing.T) {
	database := testutil.NewTestDB(t)
	dbutil.SetTestDB(database)
	defer dbutil.SetTestDB(nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	Setup(r, database)

	routes := r.Routes()
	got := map[string]bool{}
	deadLLM := false
	for _, route := range routes {
		if route.Method == "GET" && route.Path == "/api/mcp/audits" {
			got["GET /api/mcp/audits"] = true
		}
		if route.Method == "GET" && route.Path == "/api/mcp/skills" {
			got["GET /api/mcp/skills"] = true
		}
		if route.Method == "POST" && route.Path == "/api/mcp/skills" {
			got["POST /api/mcp/skills"] = true
		}
		if route.Method == "PUT" && route.Path == "/api/mcp/skills/:id" {
			got["PUT /api/mcp/skills/:id"] = true
		}
		if route.Method == "DELETE" && route.Path == "/api/mcp/skills/:id" {
			got["DELETE /api/mcp/skills/:id"] = true
		}
		if route.Method == "GET" && route.Path == "/api/mcp/skills/:id" {
			got["GET /api/mcp/skills/:id"] = true
		}
		// 1:N 钥匙族（2026-10-11）
		for _, m := range []string{"GET", "POST"} {
			if route.Method == m && route.Path == "/api/mcp/credentials/:id/keys" {
				got[m+" /api/mcp/credentials/:id/keys"] = true
			}
		}
		for _, m := range []string{"POST", "GET", "DELETE"} {
			if route.Method == m && route.Path == "/api/mcp/credentials/:id/keys/:keyId" ||
				route.Method == m && route.Path == "/api/mcp/credentials/:id/keys/:keyId/enable" ||
				route.Method == m && route.Path == "/api/mcp/credentials/:id/keys/:keyId/disable" ||
				route.Method == m && route.Path == "/api/mcp/credentials/:id/keys/:keyId/revoke" ||
				route.Method == m && route.Path == "/api/mcp/credentials/:id/keys/:keyId/reveal" {
				got[m+" /api/mcp/credentials/:id/keys/:keyId"] = true
			}
		}
		if route.Method == "GET" && route.Path == "/api/mcp/admin-audits" {
			got["GET /api/mcp/admin-audits"] = true
		}
		// 死副本防线：llm 组下的 mcp 路由（/api/llm/mcp/...）已被清除，
		// 不能再长回来（双路径=双鉴权口径，统一入口是四件套的第一件）。
		if route.Path == "/api/llm/mcp/credentials" {
			deadLLM = true
		}
	}
	for _, want := range []string{
		"GET /api/mcp/audits", "GET /api/mcp/skills", "POST /api/mcp/skills",
		"GET /api/mcp/skills/:id", "PUT /api/mcp/skills/:id", "DELETE /api/mcp/skills/:id",
		"GET /api/mcp/credentials/:id/keys", "POST /api/mcp/credentials/:id/keys",
		"POST /api/mcp/credentials/:id/keys/:keyId",   // enable/disable/revoke
		"GET /api/mcp/credentials/:id/keys/:keyId",    // reveal
		"DELETE /api/mcp/credentials/:id/keys/:keyId", // 删除
		"GET /api/mcp/admin-audits",
	} {
		if !got[want] {
			t.Errorf("MCP 管理路由缺失: %s", want)
		}
	}
	if deadLLM {
		t.Error("/api/llm/mcp/credentials 死副本回归：入口与权限口径必须唯一（authPlainAdmin 组）")
	}
}

// TestSetup_MCPEntryOnce /api/mcp 入口只有一条（MCPGuard 保护下的唯一 JSON-RPC 口）。
func TestSetup_MCPEntryOnce(t *testing.T) {
	database := testutil.NewTestDB(t)
	dbutil.SetTestDB(database)
	defer dbutil.SetTestDB(nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	Setup(r, database)

	count := 0
	for _, route := range r.Routes() {
		if route.Method == "POST" && route.Path == "/api/mcp" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("/api/mcp 入口应为 1 条，got %d", count)
	}
}
