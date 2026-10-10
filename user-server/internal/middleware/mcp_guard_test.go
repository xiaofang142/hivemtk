package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// MCPGuard 的用例全部走 seam（mcpTokenLookup / bridgeCredentialCandidates）：
// 真来源是 system_config_kv，需要库句柄，而这里要判的是「配了哪一种凭证」这个组合，
// 六个分支在 CI 里必须都真跑过，不能靠"有库就测、没库就跳"。

func withSeams(t *testing.T, kv map[string]string, bridgeCreds []string) {
	t.Helper()
	oldKV := mcpTokenLookup
	oldBridge := bridgeCredentialCandidates
	mcpTokenLookup = func(_ context.Context, key string) (string, error) {
		if v, ok := kv[key]; ok {
			return v, nil
		}
		return "", nil
	}
	bridgeCredentialCandidates = func() []string { return bridgeCreds }
	t.Cleanup(func() {
		mcpTokenLookup = oldKV
		bridgeCredentialCandidates = oldBridge
	})
}

func newMCPProbe() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/mcp", MCPGuard(), func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func postMCP(t *testing.T, r *gin.Engine, headers map[string]string) (int, bool) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/mcp", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	r.ServeHTTP(rec, req)
	body := rec.Body.String()
	reached := rec.Code == http.StatusOK
	if reached && body != "" {
		t.Errorf("放行分支不该带响应体，got=%q", body)
	}
	return rec.Code, reached
}

func TestMCPGuard_DedicatedTokenLocksOutBridgeToken(t *testing.T) {
	t.Setenv("MCP_TOKEN", "")
	t.Setenv("MCP_TOKEN_PREV", "")
	withSeams(t, map[string]string{"mcp_token": "dedicated-1"}, []string{"bridge-1"})
	r := newMCPProbe()

	if code, ok := postMCP(t, r, nil); ok || code != http.StatusUnauthorized {
		t.Errorf("不带凭证应 401，got code=%d reached=%v", code, ok)
	}
	if code, ok := postMCP(t, r, map[string]string{"X-MCP-Token": "dedicated-1"}); !ok || code != http.StatusOK {
		t.Errorf("X-MCP-Token 命中专用凭证应放行，got code=%d reached=%v", code, ok)
	}
	if code, ok := postMCP(t, r, map[string]string{"Authorization": "Bearer dedicated-1"}); !ok || code != http.StatusOK {
		t.Errorf("Authorization Bearer 形状应等价放行，got code=%d reached=%v", code, ok)
	}
	// 本用例的主角：配了专用凭证后，扩展那份凭证换不到工具调用权限。
	if code, ok := postMCP(t, r, map[string]string{"X-Bridge-Token": "bridge-1"}); ok || code != http.StatusUnauthorized {
		t.Errorf("已配专用凭证时 X-Bridge-Token 仍被放行——拆分只是形式，got code=%d reached=%v", code, ok)
	}
	if code, ok := postMCP(t, r, map[string]string{"X-MCP-Token": "wrong"}); ok || code != http.StatusUnauthorized {
		t.Errorf("错值应 401，got code=%d reached=%v", code, ok)
	}
}

func TestMCPGuard_FallsBackToBridgeCredential(t *testing.T) {
	t.Setenv("MCP_TOKEN", "")
	t.Setenv("MCP_TOKEN_PREV", "")
	withSeams(t, map[string]string{}, []string{"bridge-1"})
	r := newMCPProbe()

	if code, ok := postMCP(t, r, map[string]string{"X-Bridge-Token": "bridge-1"}); !ok || code != http.StatusOK {
		t.Errorf("未配专用凭证时应回落桥接凭证（存量部署不能当场 401），got code=%d reached=%v", code, ok)
	}
	if code, ok := postMCP(t, r, map[string]string{"X-MCP-Token": "bridge-1"}); !ok || code != http.StatusOK {
		t.Errorf("回落分支下 X-MCP-Token 也应能带凭证，got code=%d reached=%v", code, ok)
	}
	if code, ok := postMCP(t, r, map[string]string{"X-Bridge-Token": "nope"}); ok || code != http.StatusUnauthorized {
		t.Errorf("回落分支下错值仍须 401，got code=%d reached=%v", code, ok)
	}
}

func TestMCPGuard_NoCredentialFailsClosedEvenWithOffSwitch(t *testing.T) {
	// 这一格钉的是把 /api/mcp 从桥接组里摘出来的**理由**：
	// BRIDGE_INGEST_AUTH=off 只关桥接上报那条门，绝不能顺带放开工具调用。
	t.Setenv("MCP_TOKEN", "")
	t.Setenv("MCP_TOKEN_PREV", "")
	t.Setenv("BRIDGE_INGEST_AUTH", "off")
	withSeams(t, map[string]string{}, nil)
	r := newMCPProbe()

	if code, ok := postMCP(t, r, nil); ok || code != http.StatusServiceUnavailable {
		t.Errorf("两种凭证都没有时必须 503 fail-closed（off 开关不许接管本入口），got code=%d reached=%v", code, ok)
	}
}

func TestMCPGuard_EnvRotationAcceptsPrevToken(t *testing.T) {
	t.Setenv("MCP_TOKEN", "new-1")
	t.Setenv("MCP_TOKEN_PREV", "old-1")
	withSeams(t, map[string]string{}, nil)
	r := newMCPProbe()

	if code, ok := postMCP(t, r, map[string]string{"X-MCP-Token": "new-1"}); !ok || code != http.StatusOK {
		t.Errorf("新值应放行，got code=%d reached=%v", code, ok)
	}
	if code, ok := postMCP(t, r, map[string]string{"X-MCP-Token": "old-1"}); !ok || code != http.StatusOK {
		t.Errorf("轮换期旧值应继续可用（否则一次改配置就把全部调用方踢下线），got code=%d reached=%v", code, ok)
	}
}

// =============================================================================
// 凭证对链路（X-Client-Id + X-API-Key）：过期 / 无效 / scope 注入
// =============================================================================

// pairProbe 凭证对链路的探针：回显 MCP 上下文键，证明 scope 注入到了请求。
func pairProbe() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/mcp", MCPGuard(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"client_id":     c.GetString(MCPCtxClientID),
			"credential_id": c.GetUint(MCPCtxCredentialID),
			"allowed":       c.GetString(MCPCtxAllowedTools),
		})
	})
	return r
}

// credFixture 构造一条 mcp_credentials 行（hash=sha256 的 "k"）。
func credFixture(expiresAt *time.Time, allowedTools string) *model.MCPCredential {
	sum := sha256.Sum256([]byte("k"))
	return &model.MCPCredential{
		ID:           7,
		ClientID:     "mcp-test-1",
		APIKeyHash:   hex.EncodeToString(sum[:]),
		Enabled:      true,
		OwnerUserID:  ptrUint(3),
		ExpiresAt:    expiresAt,
		AllowedTools: allowedTools,
	}
}

func ptrUint(v uint) *uint { return &v }

// withCredSeam 替换 mcpCredLookup 为固定返回（成对还原，防同包用例互相污染）。
func withCredSeam(t *testing.T, rows map[string]*model.MCPCredential) {
	t.Helper()
	old := mcpCredLookup
	mcpCredLookup = func(_ context.Context, clientID string) (*model.MCPCredential, error) {
		if row, ok := rows[clientID]; ok {
			return row, nil
		}
		return nil, gorm.ErrRecordNotFound
	}
	t.Cleanup(func() { mcpCredLookup = old })
	// 清缓存：上一条用例可能已把同名 clientID 缓存成 enabled=false，不清则新行永远走不到。
	mcpCredMu.Lock()
	mcpCredCache = map[string]mcpCredCacheEntry{}
	mcpCredMu.Unlock()
}

func TestMCPGuard_CredentialPair_ValidInjectsScope(t *testing.T) {
	fut := time.Now().Add(24 * time.Hour)
	withSeams(t, nil, nil)
	withCredSeam(t, map[string]*model.MCPCredential{"mcp-test-1": credFixture(&fut, "customer.read,reach.send")})
	r := pairProbe()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/mcp", nil)
	req.Header.Set("X-Client-Id", "mcp-test-1")
	req.Header.Set("X-API-Key", "k")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("有效凭证应放行，got %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"mcp-test-1", "customer.read,reach.send"} {
		if !strings.Contains(body, want) {
			t.Errorf("scope 未注入上下文，缺 %q，body=%s", want, body)
		}
	}
}

func TestMCPGuard_CredentialPair_ExpiredRefused(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	withSeams(t, nil, nil)
	withCredSeam(t, map[string]*model.MCPCredential{"mcp-test-1": credFixture(&past, "")})
	r := pairProbe()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/mcp", nil)
	req.Header.Set("X-Client-Id", "mcp-test-1")
	req.Header.Set("X-API-Key", "k")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("过期凭证必须 401，got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "过期") {
		t.Errorf("过期 vs 无效要分开措辞，got body=%s", rec.Body.String())
	}
}

func TestMCPGuard_CredentialPair_BadKeyRefused(t *testing.T) {
	fut := time.Now().Add(24 * time.Hour)
	withSeams(t, nil, nil)
	withCredSeam(t, map[string]*model.MCPCredential{"mcp-test-1": credFixture(&fut, "")})
	r := pairProbe()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/mcp", nil)
	req.Header.Set("X-Client-Id", "mcp-test-1")
	req.Header.Set("X-API-Key", "wrong-key")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("错误 API Key 必须 401，got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "过期") {
		t.Errorf("错密钥不该报'过期'，got body=%s", rec.Body.String())
	}
}

func TestMCPGuard_CredentialPair_NoExpiryStaysValid(t *testing.T) {
	// 存量行没有 expired_at=NULL：兼容语义 = 永不过期。
	withSeams(t, nil, nil)
	withCredSeam(t, map[string]*model.MCPCredential{"mcp-test-1": credFixture(nil, "")})
	r := pairProbe()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/mcp", nil)
	req.Header.Set("X-Client-Id", "mcp-test-1")
	req.Header.Set("X-API-Key", "k")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("无过期时间的存量凭证应保持可用，got %d body=%s", rec.Code, rec.Body.String())
	}
}
