package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/middleware"
	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
)

// 全局请求体封顶必须挂在**路由装配处**，而不是只停在中间件包里：
// 中间件写完不等于生效，gin 的引擎级 Use 只对注册在它之后的路由起作用。
// 这里断言的正是那层装配（Setup 里的一次 r.Use + 一次 MaxMultipartMemory 赋值）。

// TestSetupCapsOversizedBodyBeforeAuth 超限请求应当在鉴权之前就被 413 拦下。
// 选 `/api/users`（auth 组 + admin 组，无令牌本应 401）做靶子：
// 拿到 413 而非 401，同时证明「已挂到全局链」与「挂在 JWT 之前」两件事。
func TestSetupCapsOversizedBodyBeforeAuth(t *testing.T) {
	database := testutil.NewTestDB(t)
	dbutil.SetTestDB(database)
	defer dbutil.SetTestDB(nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	Setup(r, database)

	registered := map[string]bool{}
	for _, ri := range r.Routes() {
		if ri.Method == http.MethodPost {
			registered[ri.Path] = true
		}
	}
	if !registered["/api/users"] {
		t.Fatalf("POST /api/users 未注册，靶子失效（换个真实存在的内部口再来）")
	}

	const limit = middleware.DefaultMaxJSONBodyMB * 1024 * 1024
	body := `{"marker":1,"padding":"` + strings.Repeat("a", limit+1024) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if req.ContentLength <= limit {
		t.Fatalf("夹具没铺到超限: ContentLength=%d limit=%d", req.ContentLength, limit)
	}
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("超限 POST /api/users 返回 %d，期望 413 ⇒ 全局封顶没挂上或挂在 JWT 之后（body=%s）",
			w.Code, truncateBody(w.Body.String()))
	}
}

// TestSetupKeepsNormalSizedBodyReachable 对照组：未超限的请求不得被全局封顶误伤，
// 同一路径无令牌时仍应回到既有的 401 语义。
func TestSetupKeepsNormalSizedBodyReachable(t *testing.T) {
	database := testutil.NewTestDB(t)
	dbutil.SetTestDB(database)
	defer dbutil.SetTestDB(nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	Setup(r, database)

	req := httptest.NewRequest(http.MethodPost, "/api/users", strings.NewReader(`{"username":"probe"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code == http.StatusRequestEntityTooLarge {
		t.Errorf("几十十字节的 body 被判 413 ⇒ 全局封顶把正常载荷也拦掉了")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("未带令牌的正常请求应仍是 401，实际 %d（body=%s）", w.Code, truncateBody(w.Body.String()))
	}
}

// TestSetupConfiguresMultipartMemory 上传通道（素材库 / 知识库文档 / 聊天媒体）走 multipart，
// 其内存占用由引擎的 MaxMultipartMemory 决定 —— 装配处必须真的设过，否则等于沿用 gin 的 32MB 默认。
func TestSetupConfiguresMultipartMemory(t *testing.T) {
	database := testutil.NewTestDB(t)
	dbutil.SetTestDB(database)
	defer dbutil.SetTestDB(nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	if r.MaxMultipartMemory == middleware.MaxMultipartMemoryBytes() {
		t.Fatalf("夹具前置不成立：新引擎的 MaxMultipartMemory 已等于目标值 %d，断言将无牙",
			r.MaxMultipartMemory)
	}
	Setup(r, database)

	if r.MaxMultipartMemory != middleware.MaxMultipartMemoryBytes() {
		t.Errorf("MaxMultipartMemory=%d，期望 %d ⇒ 装配处没设，multipart 仍按引擎默认吃内存",
			r.MaxMultipartMemory, middleware.MaxMultipartMemoryBytes())
	}
}

func truncateBody(s string) string {
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}
