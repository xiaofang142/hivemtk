package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func buildRouteRuleRouter(tenantID uint, database *gorm.DB) *gin.Engine {
	ctrl := NewChannelRouteRuleController(
		service.NewChannelRouteRuleService(repository.NewChannelRouteRuleRepository(database)),
	)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.TenantContextKey, tenantID)
		c.Next()
	})
	r.GET("/api/channel-route-rules", ctrl.List)
	r.POST("/api/channel-route-rules", ctrl.Create)
	r.PUT("/api/channel-route-rules/:id", ctrl.Update)
	r.DELETE("/api/channel-route-rules/:id", ctrl.Delete)
	return r
}

func doRouteJSON(r *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestChannelRouteRuleAPI(t *testing.T) {
	database := testutil.NewTestDBOrSkip(t, &model.ChannelRouteRule{})
	r := buildRouteRuleRouter(10, database)

	// create
	w := doRouteJSON(r, http.MethodPost, "/api/channel-route-rules", map[string]any{
		"channel":    "telegram",
		"event_type": "message.received",
		"match":      `{"extra.tag":"vip"}`,
		"target":     "agent",
		"target_ref": "sales-v2",
		"priority":   10,
		"enabled":    true,
		"note":       "VIP 客户走销售智能体",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("create 期望 200，实际 %d：%s", w.Code, w.Body.String())
	}
	var created struct {
		Data model.ChannelRouteRule `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	if created.Data.ID == 0 {
		t.Fatalf("创建应返回自增 id：%s", w.Body.String())
	}
	ruleID := created.Data.ID

	// list
	w = doRouteJSON(r, http.MethodGet, "/api/channel-route-rules", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list 期望 200，实际 %d", w.Code)
	}

	// update
	w = doRouteJSON(r, http.MethodPut, "/api/channel-route-rules/"+routeIDStr(ruleID), map[string]any{
		"channel": "telegram", "event_type": "message.*", "target": "discard", "priority": 5,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("update 期望 200，实际 %d：%s", w.Code, w.Body.String())
	}

	// 非法目标必须 400（写入期拒掉，而不是让规则静默失效）
	w = doRouteJSON(r, http.MethodPost, "/api/channel-route-rules", map[string]any{
		"channel": "telegram", "target": "not_a_target",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("未知目标应 400，实际 %d：%s", w.Code, w.Body.String())
	}

	// 非法 Match 必须 400
	w = doRouteJSON(r, http.MethodPost, "/api/channel-route-rules", map[string]any{
		"channel": "telegram", "target": "pipeline", "match": `{"a":`,
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法 Match 应 400，实际 %d：%s", w.Code, w.Body.String())
	}

	// delete
	w = doRouteJSON(r, http.MethodDelete, "/api/channel-route-rules/"+routeIDStr(ruleID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("delete 期望 200，实际 %d：%s", w.Code, w.Body.String())
	}
	// 再删同一条 → 404
	w = doRouteJSON(r, http.MethodDelete, "/api/channel-route-rules/"+routeIDStr(ruleID), nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("重复删除应 404，实际 %d", w.Code)
	}
}

// TestChannelRouteRuleAPI_CrossTenant404 跨租户改/删必须 404（而不是 403 泄露「存在但无权」）。
func TestChannelRouteRuleAPI_CrossTenant404(t *testing.T) {
	database := testutil.NewTestDBOrSkip(t, &model.ChannelRouteRule{})
	rA := buildRouteRuleRouter(11, database) // 租户 11 建规则
	w := doRouteJSON(rA, http.MethodPost, "/api/channel-route-rules", map[string]any{
		"channel": "feishu", "target": "discard",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		Data model.ChannelRouteRule `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	// 换成租户 12（同一 DB，因为 testutil 复用连接）
	rB := buildRouteRuleRouter(12, database)
	w = doRouteJSON(rB, http.MethodPut, "/api/channel-route-rules/"+routeIDStr(created.Data.ID), map[string]any{
		"channel": "feishu", "target": "agent",
	})
	if w.Code != http.StatusNotFound {
		t.Fatalf("跨租户更新应 404，实际 %d：%s", w.Code, w.Body.String())
	}
	w = doRouteJSON(rB, http.MethodDelete, "/api/channel-route-rules/"+routeIDStr(created.Data.ID), nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("跨租户删除应 404，实际 %d：%s", w.Code, w.Body.String())
	}
	// 租户 B 的列表里不能出现租户 A 的规则
	w = doRouteJSON(rB, http.MethodGet, "/api/channel-route-rules", nil)
	if bytes.Contains(w.Body.Bytes(), []byte(`"feishu"`)) {
		t.Fatalf("租户 B 列表出现租户 A 的规则：%s", w.Body.String())
	}
}

func routeIDStr(v uint) string {
	b, _ := json.Marshal(v)
	return string(b)
}
