package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// mcp_audit_test.go MCP 审计 / Skill 管理端点分诊测试。
//
// 口径（与 sales_workbench 503 三件套同源）：
//   - 无库环境（db.GetDB()==nil）：service Ready()=false ⇒ 全部端点 503，且
//     503 的 message 指回"服务未就绪"，不误报成"查过了、没有"；
//   - 有库环境（PG 测试库就位）：审计创建→列表可回读，Skill CRUD 全链走通。
// 两端互补，无库时 CI 也真跑（不会"有库才测、无库就跳"）。

func setupMCPAuditTestDB(t *testing.T) {
	t.Helper()
	database := testutil.NewTestDB(t,
		&model.MCPCallAudit{},
		&model.MCPSkill{},
		&model.MCPCredential{},
	)
	db.SetTestDB(database)
	t.Cleanup(func() { db.SetTestDB(nil) })
}

func mcpAuditTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api/manage")
	g.GET("/mcp/audits", ListMCPCallAudits)
	g.GET("/mcp/skills", ListMCPSkills)
	g.GET("/mcp/skills/:id", GetMCPSkill)
	g.POST("/mcp/skills", CreateMCPSkill)
	g.PUT("/mcp/skills/:id", UpdateMCPSkill)
	g.DELETE("/mcp/skills/:id", DeleteMCPSkill)
	return r
}

func get(t *testing.T, r *gin.Engine, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, path, nil)
	r.ServeHTTP(w, req)
	return w
}

// TestMCPAuditEndpoints_Unavailable503 无库时全部端点 503 且不假空。
func TestMCPAuditEndpoints_Unavailable503(t *testing.T) {
	db.SetTestDB(nil)
	r := mcpAuditTestRouter()

	for _, path := range []string{
		"/api/manage/mcp/audits",
		"/api/manage/mcp/skills",
		"/api/manage/mcp/skills/1",
	} {
		w := get(t, r, path)
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s 应 503，got %d body=%s", path, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), `"data":{}`) || strings.Contains(w.Body.String(), `"data":[]`) {
			t.Errorf("%s 的 503 不该带像结果的 data（'没装配'≠'查过了没有'），body=%s", path, w.Body.String())
		}
	}

	// 写口同样 503
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/manage/mcp/skills",
		bytes.NewBufferString(`{"name":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("创建 Skill 无库应 503，got %d", w.Code)
	}
}

// TestMCPSkillCRUD_Happy 有库时 CRUD 全链走通。
func TestMCPSkillCRUD_Happy(t *testing.T) {
	setupMCPAuditTestDB(t)
	r := mcpAuditTestRouter()

	// 创建
	body := `{"name":"demo-skill","description":"演示 Skill","allowed_tools":"customer.read","doc_url":"skills/hivemtk/demo"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/manage/mcp/skills",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("创建 Skill 应 200，got %d body=%s", w.Code, w.Body.String())
	}
	var created struct {
		Data struct {
			ID   uint   `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil || created.Data.ID == 0 {
		t.Fatalf("创建响应应带 id，got %s", w.Body.String())
	}
	id := created.Data.ID

	// 列表回读
	w = get(t, r, "/api/manage/mcp/skills")
	if w.Code != http.StatusOK {
		t.Fatalf("列表应 200，got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "demo-skill") {
		t.Errorf("列表应含刚创建的 Skill，body=%s", w.Body.String())
	}

	// 详情
	w = get(t, r, "/api/manage/mcp/skills/"+strconv.FormatUint(uint64(id), 10))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "demo-skill") {
		t.Errorf("详情应可读，got %d body=%s", w.Code, w.Body.String())
	}

	// 更新
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPut, "/api/manage/mcp/skills/"+strconv.FormatUint(uint64(id), 10),
		bytes.NewBufferString(`{"enabled":false}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("更新应 200，got %d body=%s", w.Code, w.Body.String())
	}

	// 删除
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodDelete, "/api/manage/mcp/skills/"+strconv.FormatUint(uint64(id), 10), nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("删除应 200，got %d", w.Code)
	}
	w = get(t, r, "/api/manage/mcp/skills/"+strconv.FormatUint(uint64(id), 10))
	if w.Code != http.StatusNotFound {
		t.Errorf("删除后详情应 404，got %d body=%s", w.Code, w.Body.String())
	}
}

// TestListMCPCallAudits_Happy 审计写入后列表可回读。
func TestListMCPCallAudits_Happy(t *testing.T) {
	setupMCPAuditTestDB(t)
	svc := newAuditRecordSvc()
	fut := true
	svc.Record(t.Context(), &model.MCPCallAudit{
		ClientID: "mcp-test", Method: "tools/call", Tool: "echo",
		Allowed: true, OK: true, ArgsHash: "abc", LatencyMS: 12, IP: "127.0.0.1",
	})
	svc.Record(t.Context(), &model.MCPCallAudit{
		ClientID: "mcp-test", Method: "tools/call", Tool: "reach.send",
		Allowed: false, OK: false, ErrorMsg: "越权",
	})
	_ = fut

	r := mcpAuditTestRouter()
	w := get(t, r, "/api/manage/mcp/audits?client_id=mcp-test")
	if w.Code != http.StatusOK {
		t.Fatalf("审计列表应 200，got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			List  []model.MCPCallAudit `json:"list"`
			Total int64                `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析失败: %v body=%s", err, w.Body.String())
	}
	if resp.Data.Total != 2 || len(resp.Data.List) != 2 {
		t.Errorf("应有 2 条审计，got total=%d len=%d", resp.Data.Total, len(resp.Data.List))
	}

	// allowed 过滤
	w = get(t, r, "/api/manage/mcp/audits?allowed=0")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "越权") {
		t.Errorf("allowed=0 过滤应命中被拒行，got %d body=%s", w.Code, w.Body.String())
	}
}

func newAuditRecordSvc() *service.MCPCallAuditAdminService {
	return service.NewMCPCallAuditAdminService()
}
