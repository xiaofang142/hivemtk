package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	geomodel "hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func init() { gin.SetMode(gin.TestMode) }

func newGeoCRUDRouter(db *gorm.DB) *gin.Engine {
	r := gin.New()
	pusher := NewGeoPusherConfigController(db)
	r.GET("/pusher-configs", pusher.List)
	r.GET("/pusher-configs/:id", pusher.Get)
	r.POST("/pusher-configs", pusher.Create)
	r.PUT("/pusher-configs/:id", pusher.Update)
	r.DELETE("/pusher-configs/:id", pusher.Delete)

	tpl := NewGeoSchemaTemplateController(db)
	r.GET("/schema-templates", tpl.List)
	r.GET("/schema-templates/:id", tpl.Get)
	r.POST("/schema-templates", tpl.Create)
	r.PUT("/schema-templates/:id", tpl.Update)
	r.DELETE("/schema-templates/:id", tpl.Delete)
	return r
}

type envelope struct {
	Code    any            `json:"code"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data"`
}

func do(t *testing.T, r *gin.Engine, method, path, body string) (int, envelope) {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var env envelope
	if w.Body.Len() > 0 {
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("%s %s 响应非法 JSON: %s", method, path, w.Body.String())
		}
	}
	return w.Code, env
}

func TestGeoPusherConfigCRUD(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &geomodel.GeoPusherConfig{})
	r := newGeoCRUDRouter(db)

	status, env := do(t, r, "POST", "/pusher-configs",
		`{"platform":"test_pf","config_json":{"token":"x"},"daily_limit":5}`)
	if status != http.StatusOK {
		t.Fatalf("Create 应 200，得 %d: %+v", status, env)
	}
	id, _ := env.Data["id"].(string)
	if id == "" {
		t.Fatalf("Create 应回写 id，data=%v", env.Data)
	}

	// platform 唯一索引：重复创建应 400
	if status, _ := do(t, r, "POST", "/pusher-configs",
		`{"platform":"test_pf","config_json":{},"daily_limit":1}`); status != http.StatusBadRequest {
		t.Fatalf("重复 platform 应 400，得 %d", status)
	}
	// 非法 JSON → 400
	if status, _ := do(t, r, "POST", "/pusher-configs", `{oops`); status != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，得 %d", status)
	}
	// 未知 id → 404
	if status, _ := do(t, r, "GET", "/pusher-configs/no-such-id", ""); status != http.StatusNotFound {
		t.Fatalf("未知 id 应 404，得 %d", status)
	}

	status, env = do(t, r, "GET", "/pusher-configs/"+id, "")
	if status != http.StatusOK || env.Data["platform"] != "test_pf" {
		t.Fatalf("Get 失败: %d %+v", status, env)
	}

	// 非法 page/limit 会被钳制回默认值
	status, env = do(t, r, "GET", "/pusher-configs?page=0&limit=-3", "")
	if status != http.StatusOK {
		t.Fatalf("List 应 200，得 %d", status)
	}
	total, _ := env.Data["total"].(float64)
	if total < 1 {
		t.Fatalf("List total 应 >=1，得 %v", env.Data["total"])
	}
	if items, ok := env.Data["list"].([]any); !ok || len(items) < 1 {
		t.Fatalf("List 应含刚建配置: %v", env.Data["list"])
	}

	// Update 合并语义：body 只改 daily_limit，platform/config_json 保留
	if status, _ := do(t, r, "PUT", "/pusher-configs/"+id, `{"daily_limit":9}`); status != http.StatusOK {
		t.Fatalf("Update 应 200，得 %d", status)
	}
	_, env = do(t, r, "GET", "/pusher-configs/"+id, "")
	if env.Data["daily_limit"] != float64(9) || env.Data["platform"] != "test_pf" {
		t.Fatalf("Update 后字段不符: %v", env.Data)
	}

	if status, _ := do(t, r, "DELETE", "/pusher-configs/"+id, ""); status != http.StatusOK {
		t.Fatalf("Delete 应 200，得 %d", status)
	}
	if status, _ := do(t, r, "GET", "/pusher-configs/"+id, ""); status != http.StatusNotFound {
		t.Fatalf("软删后 Get 应 404，得 %d", status)
	}
}

func TestGeoSchemaTemplateCRUD(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &geomodel.GeoSchemaTemplate{})
	r := newGeoCRUDRouter(db)

	// 必填校验分支
	if status, _ := do(t, r, "POST", "/schema-templates",
		`{"schema_type":"FAQPage","template_json":{}}`); status != http.StatusBadRequest {
		t.Fatalf("缺 page_type 应 400，得 %d", status)
	}
	if status, _ := do(t, r, "POST", "/schema-templates",
		`{"page_type":"article","template_json":{}}`); status != http.StatusBadRequest {
		t.Fatalf("缺 schema_type 应 400，得 %d", status)
	}

	status, env := do(t, r, "POST", "/schema-templates",
		`{"page_type":"article","schema_type":"Article","template_json":{"@type":"Article"},"active":true}`)
	if status != http.StatusOK {
		t.Fatalf("Create 应 200，得 %d: %+v", status, env)
	}
	id, _ := env.Data["id"].(string)
	if id == "" {
		t.Fatalf("Create 应回写 id: %v", env.Data)
	}

	if status, _ := do(t, r, "GET", "/schema-templates/nope", ""); status != http.StatusNotFound {
		t.Fatalf("未知 id 应 404，得 %d", status)
	}
	status, env = do(t, r, "GET", "/schema-templates", "")
	if status != http.StatusOK {
		t.Fatalf("List 应 200，得 %d", status)
	}

	if status, _ := do(t, r, "PUT", "/schema-templates/"+id, `{"active":false}`); status != http.StatusOK {
		t.Fatalf("Update 应 200，得 %d", status)
	}
	_, env = do(t, r, "GET", "/schema-templates/"+id, "")
	if env.Data["active"] != false || env.Data["page_type"] != "article" {
		t.Fatalf("Update 合并语义不符: %v", env.Data)
	}

	if status, _ := do(t, r, "DELETE", "/schema-templates/"+id, ""); status != http.StatusOK {
		t.Fatalf("Delete 应 200，得 %d", status)
	}
	if status, _ := do(t, r, "GET", "/schema-templates/"+id, ""); status != http.StatusNotFound {
		t.Fatalf("删后 Get 应 404，得 %d", status)
	}
}
