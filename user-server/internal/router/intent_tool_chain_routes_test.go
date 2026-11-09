package router

// intent_tool_chain_routes_test.go TB-01：意图→工具链绑定 HTTP 接线条。
//
// 与 approval_routes_test.go 同一套跑法（真库 + 真 handler + 真 AdminAuthMiddleware，
// 只把 JWT 层换成"往上下文塞 role"），钉三件事：
//  1. 五条路由真的注册在生产 setupIntentRoutes 里（路径漂移即红）；
//  2. 管理端闸在链上：匿名 401、已登录非管理员 403（拒绝来自中间件而非 handler）；
//  3. 管理端真库往返：CRUD + vocab 在 HTTP 上真的通（服务层用例各自绿，
//     中间断一环——controller 忘接 svc / wiring 没装——就是"配了但规划器看不见"）。

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/app"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func intentChainTestBody(intent string) string {
	return `{"name":"物流链","intent_code":"` + intent + `","steps":[{"tool_name":"order.lookup","args":{"customer_id":"{{customer_id}}"},"priority":1,"enabled":true},{"tool_name":"logistics.track","args":{"channel":"{{channel_type}}"},"priority":2,"enabled":true}],"reply_hint":"先查订单再查物流","enabled":true}`
}

func intentChainDo(engine http.Handler, method, path, role, body string) *httptest.ResponseRecorder {
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if role != "" {
		req.Header.Set("X-Test-Role", role)
	}
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w
}

// TestIntentToolChainRoutes 覆盖注册/闸/真库往返（单测试函数：InitIntentChainRuntime
// 是 sync.Once，多测试函数会串测试库）。
func TestIntentToolChainRoutes(t *testing.T) {
	db := testutil.NewTestDB(t, &model.IntentToolChain{})
	app.InitIntentChainRuntime(db)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api", func(c *gin.Context) {
		if role := c.GetHeader("X-Test-Role"); role != "" {
			c.Set("role", role)
		}
		c.Next()
	})
	setupIntentRoutes(api, db)

	// ① 注册 + 匿名 401
	have := map[string]bool{}
	for _, ri := range r.Routes() {
		have[ri.Method+" "+ri.Path] = true
	}
	for _, want := range []string{
		"GET /api/intent/tool-chains",
		"GET /api/intent/tool-chains/vocab",
		"POST /api/intent/tool-chains",
		"PUT /api/intent/tool-chains/:id",
		"DELETE /api/intent/tool-chains/:id",
	} {
		if !have[want] {
			t.Fatalf("路由表缺 %s", want)
		}
	}
	if w := intentChainDo(r, "POST", "/api/intent/tool-chains", "", intentChainTestBody("order_status")); w.Code != http.StatusUnauthorized {
		t.Fatalf("匿名应 401, got %d", w.Code)
	}
	// ② 非 admin 403
	if w := intentChainDo(r, "POST", "/api/intent/tool-chains", "agent", intentChainTestBody("order_status")); w.Code != http.StatusForbidden {
		t.Fatalf("非管理员应 403, got %d", w.Code)
	}

	// ③ admin 真库往返
	// Create → 200
	w := intentChainDo(r, "POST", "/api/intent/tool-chains", "admin", intentChainTestBody("order_status"))
	if w.Code != http.StatusOK {
		t.Fatalf("admin create 应 200, got %d body=%s", w.Code, w.Body.String())
	}
	var created struct {
		Code int `json:"code"`
		Data struct {
			ID uint `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	if created.Code != 0 || created.Data.ID == 0 {
		t.Fatalf("create 响应异常: %s", w.Body.String())
	}

	// 重复创建 → 409
	if w := intentChainDo(r, "POST", "/api/intent/tool-chains", "admin", intentChainTestBody("order_status")); w.Code != http.StatusConflict {
		t.Fatalf("重复意图应 409, got %d", w.Code)
	}

	// List → 200 恰 1 行
	w = intentChainDo(r, "GET", "/api/intent/tool-chains?intent_code=order_status", "admin", "")
	if w.Code != http.StatusOK {
		t.Fatalf("list 应 200, got %d", w.Code)
	}
	var listed struct {
		Code int `json:"code"`
		Data struct {
			List  []map[string]any `json:"list"`
			Total int64            `json:"total"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &listed)
	if listed.Code != 0 || listed.Data.Total != 1 || len(listed.Data.List) != 1 {
		t.Fatalf("list 应恰 1 行: %s", w.Body.String())
	}

	// Vocab → 200 含意图/变量/工具三项
	w = intentChainDo(r, "GET", "/api/intent/tool-chains/vocab", "admin", "")
	if w.Code != http.StatusOK {
		t.Fatalf("vocab 应 200, got %d", w.Code)
	}
	var vocab struct {
		Code int `json:"code"`
		Data struct {
			Intents      []string `json:"intents"`
			TemplateVars []string `json:"template_vars"`
			Tools        []string `json:"tools"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &vocab)
	if vocab.Code != 0 || len(vocab.Data.Intents) == 0 || len(vocab.Data.TemplateVars) == 0 {
		t.Fatalf("vocab 数据不全: %s", w.Body.String())
	}

	// Update（改意图 → refund）→ 200
	if w := intentChainDo(r, "PUT", "/api/intent/tool-chains/1", "admin", intentChainTestBody("refund")); w.Code != http.StatusOK {
		t.Fatalf("update 应 200, got %d body=%s", w.Code, w.Body.String())
	}
	// Delete → 200
	if w := intentChainDo(r, "DELETE", "/api/intent/tool-chains/1", "admin", ""); w.Code != http.StatusOK {
		t.Fatalf("delete 应 200, got %d", w.Code)
	}
	// 删除后再 list → total=0
	w = intentChainDo(r, "GET", "/api/intent/tool-chains", "admin", "")
	_ = json.Unmarshal(w.Body.Bytes(), &listed)
	if listed.Data.Total != 0 {
		t.Fatalf("删除后 total 应 0, got %d", listed.Data.Total)
	}
}
