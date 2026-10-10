package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"strconv"

	"github.com/gin-gonic/gin"
)

// mcp_credential_test.go 凭证签发/更新语义（默认过期、自定义、永不过期、改 ID、清过期）。
//
// 无库（db 未装配）⇒ 503（Ready 判据下探到 db 句柄）；有库 ⇒ 全链语义。

func mcpCredentialTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api/mcp")
	g.GET("/credentials", ListMCPCredentials)
	g.POST("/credentials", CreateMCPCredential)
	g.PUT("/credentials/:id", UpdateMCPCredential)
	g.DELETE("/credentials/:id", DeleteMCPCredential)
	return r
}

func setupMCPCredentialTestDB(t *testing.T) {
	t.Helper()
	database := testutil.NewTestDB(t, &model.MCPCredential{})
	db.SetTestDB(database)
	t.Cleanup(func() { db.SetTestDB(nil) })
}

func credentialPost(t *testing.T, r *gin.Engine, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func credentialPut(t *testing.T, r *gin.Engine, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

type credResp struct {
	Data struct {
		ID       uint   `json:"id"`
		ClientID string `json:"client_id"`
		APIKey   string `json:"api_key"`
	} `json:"data"`
}

func TestCreateMCPCredential_DefaultTTL(t *testing.T) {
	setupMCPCredentialTestDB(t)
	r := mcpCredentialTestRouter()

	w := credentialPost(t, r, "/api/mcp/credentials", `{"name":"默认过期"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("签发应 200，got %d body=%s", w.Code, w.Body.String())
	}
	var out credResp
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if out.Data.ClientID == "" || !strings.HasPrefix(out.Data.ClientID, "mcp-") {
		t.Errorf("自动 client_id 应 mcp- 开头，got %q", out.Data.ClientID)
	}
	if !strings.HasPrefix(out.Data.APIKey, "wgk_") {
		t.Errorf("API Key 应 wgk_ 开头，got %q", out.Data.APIKey)
	}

	// 列表回读应带默认 90 天过期与空 scope
	w = get(t, r, "/api/mcp/credentials")
	if !strings.Contains(w.Body.String(), out.Data.ClientID) {
		t.Errorf("列表应含新凭证，body=%s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "expires_at") {
		t.Errorf("列表应带 expires_at 字段，body=%s", w.Body.String())
	}
}

func TestCreateMCPCredential_CustomIDAndNoExpiry(t *testing.T) {
	setupMCPCredentialTestDB(t)
	r := mcpCredentialTestRouter()

	// 手动指定 client_id + 永不过期
	w := credentialPost(t, r, "/api/mcp/credentials", `{"name":"x","client_id":"my-own-id","no_expiry":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("签发应 200，got %d body=%s", w.Code, w.Body.String())
	}
	var out credResp
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if out.Data.ClientID != "my-own-id" {
		t.Errorf("手动 client_id 应原样保留，got %q", out.Data.ClientID)
	}
}

func TestCreateMCPCredential_CustomExpiryRejectsPast(t *testing.T) {
	setupMCPCredentialTestDB(t)
	r := mcpCredentialTestRouter()

	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	body := `{"name":"x","expires_at":"` + past + `"}`
	w := credentialPost(t, r, "/api/mcp/credentials", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("过期时间早于当前应 400，got %d body=%s", w.Code, w.Body.String())
	}
}

func TestUpdateMCPCredential_ChangeIDAndClearExpiry(t *testing.T) {
	setupMCPCredentialTestDB(t)
	r := mcpCredentialTestRouter()

	w := credentialPost(t, r, "/api/mcp/credentials", `{"name":"x"}`)
	var created credResp
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	id := created.Data.ID

	// 改 ID + 改 scope
	w = credentialPut(t, r, "/api/mcp/credentials/"+strconv.FormatUint(uint64(id), 10), `{"client_id":"renamed","allowed_tools":"customer.read"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("改 ID 应 200，got %d body=%s", w.Code, w.Body.String())
	}
	w = get(t, r, "/api/mcp/credentials")
	if !strings.Contains(w.Body.String(), "renamed") {
		t.Errorf("改 ID 后列表应含新 ID，body=%s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "customer.read") {
		t.Errorf("scope 应写入，body=%s", w.Body.String())
	}

	// 清过期（永不过期）
	w = credentialPut(t, r, "/api/mcp/credentials/"+strconv.FormatUint(uint64(id), 10), `{"clear_expires":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("清过期应 200，got %d body=%s", w.Code, w.Body.String())
	}
}

func TestMCPCredential_Unavailable503WithoutDB(t *testing.T) {
	db.SetTestDB(nil)
	r := mcpCredentialTestRouter()

	w := credentialPost(t, r, "/api/mcp/credentials", `{"name":"x"}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("无库签发应 503，got %d body=%s", w.Code, w.Body.String())
	}
	w = get(t, r, "/api/mcp/credentials")
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("无库列表应 503，got %d", w.Code)
	}
}
