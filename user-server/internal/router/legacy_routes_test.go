package router

// D-10（选 B）：/api/knowledge 顶层域名已下线，老客户端不该再拿到 NoRoute 的 404，
// 而应拿到 410 Gone + 明确的迁移指引。这里钉两件事：
//
//  一、410 是墓碑路由的稳定契约：状态码、message 文案、data.migrate_to 三者缺一不可
//     （老客户端有的读 message，有的读结构化字段）。
//  二、墓碑不能反过来遮蔽仍在服务的权威路由：/api/knowledge-bases/:id/items 必须
//     照旧出现在真实路由表里，且匿名请求拿到的是身份守卫的 401 而不是 410。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/controller"
)

// newLegacyRoutesTestEngine 只挂墓碑路由：这一条腿不需要库，任何机器上都跑。
func newLegacyRoutesTestEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	setupLegacyRoutes(engine.Group("/api"))
	return engine
}

// TestKnowledgeLegacyTopLevelReturnsGone 匿名打 /api/knowledge 与历史搜索口：410 + 迁移提示。
func TestKnowledgeLegacyTopLevelReturnsGone(t *testing.T) {
	engine := newLegacyRoutesTestEngine()

	for _, path := range []string{"/api/knowledge", "/api/knowledge/search"} {
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))

		if w.Code != http.StatusGone {
			t.Fatalf("匿名 GET %s 应 410 Gone，实得 %d / %s", path, w.Code, strings.TrimSpace(w.Body.String()))
		}

		var body struct {
			Code    any    `json:"code"`
			Message string `json:"message"`
			Data    struct {
				MigrateTo string `json:"migrate_to"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("GET %s 的响应体不是 JSON：%v / %s", path, err, w.Body.String())
		}
		// 文案要能被人读到：老客户端至少能从 message 里看出"废弃"与迁移目标。
		for _, needle := range []string{"已废弃", controller.KnowledgeMigrationTarget} {
			if !strings.Contains(body.Message, needle) {
				t.Errorf("GET %s 的 message 缺 %q：%q", path, needle, body.Message)
			}
		}
		// 结构化字段要能被告机器读到。
		if body.Data.MigrateTo != controller.KnowledgeMigrationTarget {
			t.Errorf("GET %s 的 data.migrate_to 应为 %q，实得 %q", path, controller.KnowledgeMigrationTarget, body.Data.MigrateTo)
		}
		// N-5：正式错误码而非 UNKNOWN_1000——按码分流的客户端要能认出"该换地址了"。
		if body.Code != "LEGACY_GONE_4100" {
			t.Errorf("GET %s 的 code 应为 LEGACY_GONE_4100，实得 %v", path, body.Code)
		}
	}
}

// TestKnowledgeLegacyRoutesDoNotShadowLiveKBItems 在真实装配上对账：
// 墓碑路由在表里、权威的 /api/knowledge-bases/:id/items 也在表里且行为未被改写。
func TestKnowledgeLegacyRoutesDoNotShadowLiveKBItems(t *testing.T) {
	engine := gateEngine(t)

	live := map[string]bool{}
	for _, ri := range engine.Routes() {
		live[ri.Method+" "+ri.Path] = true
	}
	for _, key := range []string{
		"GET /api/knowledge",
		"GET /api/knowledge/search",
		"GET /api/knowledge-bases/:id/items",
	} {
		if !live[key] {
			t.Fatalf("真实路由表里缺 %s：墓碑路由没挂上，或权威口被挤掉了", key)
		}
	}

	// 权威口保持原行为：匿名应被 JWT 守卫拦成 401，绝不能是 410/404。
	code, body := gateCall(t, engine, http.MethodGet, "/api/knowledge-bases/1/items", "")
	if code == http.StatusGone {
		t.Fatalf("GET /api/knowledge-bases/1/items 被墓碑路由改写成了 410：%s", strings.TrimSpace(body))
	}
	if code == http.StatusNotFound {
		t.Fatalf("GET /api/knowledge-bases/1/items 返回 404：权威路由没注册或路径形状变了")
	}
	if code != http.StatusUnauthorized {
		t.Errorf("匿名 GET /api/knowledge-bases/1/items 应保持 401（身份守卫），实得 %d / %s",
			code, strings.TrimSpace(body))
	}

	// 同一张真实路由表上，墓碑口仍是 410 + 迁移提示。
	code, body = gateCall(t, engine, http.MethodGet, "/api/knowledge", "")
	if code != http.StatusGone {
		t.Fatalf("真实装配下匿名 GET /api/knowledge 应 410 Gone，实得 %d / %s", code, strings.TrimSpace(body))
	}
	if !strings.Contains(body, controller.KnowledgeMigrationTarget) {
		t.Errorf("410 响应体缺迁移目标 %q：%s", controller.KnowledgeMigrationTarget, strings.TrimSpace(body))
	}
}
