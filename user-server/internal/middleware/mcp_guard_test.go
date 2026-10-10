package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
			"key_id":        c.GetUint(MCPCtxKeyID),
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
	resetMCPCaches()
}

// withKeySeam 替换 1:N 钥匙链的三个 seam（key 查询 / 凭证查询 / 鉴权审计），
// 并清空缓存与负缓存——同包用例共享进程，脏缓存是"换行不生效"的第一嫌疑人。
// legacy seam 一并钉成"查无凭证"，让本组用例只走钥匙表路径。
func withKeySeam(t *testing.T,
	keysByHash map[string]*model.MCPCredentialKey,
	credsByID map[uint]*model.MCPCredential,
	authAudits *[]*model.MCPCallAudit) {
	t.Helper()
	oldKey := mcpKeyLookup
	oldCredByID := mcpCredLookupByID
	oldCred := mcpCredLookup
	oldSink := mcpAuthAuditSink
	mcpKeyLookup = func(_ context.Context, keyHash string) (*model.MCPCredentialKey, error) {
		if key, ok := keysByHash[keyHash]; ok {
			return key, nil
		}
		return nil, gorm.ErrRecordNotFound
	}
	mcpCredLookupByID = func(_ context.Context, id uint) (*model.MCPCredential, error) {
		if cred, ok := credsByID[id]; ok {
			return cred, nil
		}
		return nil, gorm.ErrRecordNotFound
	}
	mcpCredLookup = func(_ context.Context, _ string) (*model.MCPCredential, error) {
		return nil, gorm.ErrRecordNotFound
	}
	if authAudits != nil {
		mcpAuthAuditSink = func(_ context.Context, row *model.MCPCallAudit) {
			*authAudits = append(*authAudits, row)
		}
	}
	t.Cleanup(func() {
		mcpKeyLookup = oldKey
		mcpCredLookupByID = oldCredByID
		mcpCredLookup = oldCred
		mcpAuthAuditSink = oldSink
	})
	resetMCPCaches()
}

// resetMCPCaches 清空钥匙/凭证缓存与 client_id 负缓存（用例间隔离的唯一正道）。
func resetMCPCaches() {
	mcpCredMu.Lock()
	mcpCredCache = map[string]mcpCredCacheEntry{}
	mcpCredMu.Unlock()
	mcpCredNegPair = sync.Map{}
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

// =============================================================================
// 1:N 钥匙表路径（2026-10-11）：多把钥匙各自的状态/过期独立生效，鉴权失败落审计
// =============================================================================

// keyFixture 构造一把钥匙行（hash 由明文 keyName 推导，与请求头里的 X-API-Key 对齐）。
func keyFixture(id, credID uint, keyName, status string, expiresAt *time.Time) *model.MCPCredentialKey {
	sum := sha256.Sum256([]byte(keyName))
	return &model.MCPCredentialKey{
		ID:           id,
		CredentialID: credID,
		Name:         "key-" + keyName,
		KeyHash:      hex.EncodeToString(sum[:]),
		Status:       status,
		ExpiresAt:    expiresAt,
	}
}

func TestMCPGuard_KeyTable_StatusesAndAudit(t *testing.T) {
	fut := time.Now().Add(24 * time.Hour)
	past := time.Now().Add(-time.Hour)
	cred := credFixture(&fut, "customer.read")
	cred.ID = 7

	keys := map[string]*model.MCPCredentialKey{
		hashOf("ka"): keyFixture(11, 7, "ka", model.MCPKeyStatusActive, nil),
		hashOf("kb"): keyFixture(12, 7, "kb", model.MCPKeyStatusDisabled, nil),
		hashOf("kc"): keyFixture(13, 7, "kc", model.MCPKeyStatusRevoked, nil),
		hashOf("kd"): keyFixture(14, 7, "kd", model.MCPKeyStatusActive, &past),
	}
	var audits []*model.MCPCallAudit
	withSeams(t, nil, nil)
	withKeySeam(t, keys, map[uint]*model.MCPCredential{7: cred}, &audits)
	r := pairProbe()

	// ① active 钥匙放行，且 key_id 注入上下文（1:N 之后"哪把钥匙干的"全靠它对账）。
	code, body := postMCPBody(t, r, map[string]string{"X-Client-Id": "mcp-test-1", "X-API-Key": "ka"})
	if code != http.StatusOK || !strings.Contains(body, `"key_id":11`) {
		t.Errorf("active 钥匙应放行且 key_id=11，got code=%d body=%s", code, body)
	}

	// ② 禁用钥匙：401 且措辞点名"禁用"（与"无效"分开，运维下一步完全不同）。
	code, body = postMCPBody(t, r, map[string]string{"X-Client-Id": "mcp-test-1", "X-API-Key": "kb"})
	if code != http.StatusUnauthorized || !strings.Contains(body, "禁用") {
		t.Errorf("disabled 钥匙应 401 且点名禁用，got code=%d body=%s", code, body)
	}

	// ③ 吊销（终态）：401 且措辞点名"停用"。
	code, body = postMCPBody(t, r, map[string]string{"X-Client-Id": "mcp-test-1", "X-API-Key": "kc"})
	if code != http.StatusUnauthorized || !strings.Contains(body, "停用") {
		t.Errorf("revoked 钥匙应 401 且点名停用，got code=%d body=%s", code, body)
	}

	// ④ 钥匙级独立过期：钥匙状态 active 也会被 ExpiresAt 拦下。
	code, body = postMCPBody(t, r, map[string]string{"X-Client-Id": "mcp-test-1", "X-API-Key": "kd"})
	if code != http.StatusUnauthorized || !strings.Contains(body, "过期") {
		t.Errorf("过期钥匙应 401 且报过期，got code=%d body=%s", code, body)
	}

	// ⑤ 三次失败都要落 event=auth 审计行，且能对回钥匙 id。
	if len(audits) != 3 {
		t.Fatalf("三次失败应各落一条审计，got %d： %+v", len(audits), audits)
	}
	wantKeys := map[uint]bool{12: true, 13: true, 14: true}
	for _, a := range audits {
		if a.Event != "auth" || a.ClientID != "mcp-test-1" || a.OK {
			t.Errorf("审计行口径错: %+v", a)
		}
		if a.KeyID == nil || !wantKeys[*a.KeyID] {
			t.Errorf("审计行未对回钥匙 id: %+v", a)
		}
	}
}

func TestMCPGuard_KeyTable_UnknownKeyFallsToLegacy(t *testing.T) {
	// 钥匙表未命中的钥匙落 legacy 兜底；legacy 也查无此凭证时按"无效"拒绝
	// 并写负缓存（防同一 client_id 反复撞门）。
	fut := time.Now().Add(24 * time.Hour)
	var audits []*model.MCPCallAudit
	withSeams(t, nil, nil)
	withKeySeam(t, map[string]*model.MCPCredentialKey{}, map[uint]*model.MCPCredential{}, &audits)
	withCredSeam(t, map[string]*model.MCPCredential{"mcp-test-1": credFixture(&fut, "")})
	r := pairProbe()

	// ① 钥匙表未命中 → legacy 命中（hash 列 = sha256("k")）→ 放行，key_id=0。
	code, body := postMCPBody(t, r, map[string]string{"X-Client-Id": "mcp-test-1", "X-API-Key": "k"})
	if code != http.StatusOK {
		t.Fatalf("legacy 兜底路径应放行，got code=%d body=%s", code, body)
	}
	if !strings.Contains(body, `"key_id":0`) {
		t.Errorf("legacy 命中不经过钥匙表，key_id 应为 0，body=%s", body)
	}

	// ② legacy 也比不上的钥匙：401 无效措辞 + 审计行（event=auth，key_id 缺省）。
	code, body = postMCPBody(t, r, map[string]string{"X-Client-Id": "mcp-test-1", "X-API-Key": "wrong"})
	if code != http.StatusUnauthorized || strings.Contains(body, "过期") {
		t.Errorf("错钥匙应 401 且不报过期，got code=%d body=%s", code, body)
	}
	if len(audits) != 1 {
		t.Fatalf("一次失败应落一条审计，got %d", len(audits))
	}
}

func hashOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// postMCPBody 与 postMCP 同型，但回传响应体（钥匙链路要断言措辞与注入的 key_id）。
func postMCPBody(t *testing.T, r *gin.Engine, headers map[string]string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/mcp", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}
