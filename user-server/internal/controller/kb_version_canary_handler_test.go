// kb_version_canary_handler_test.go T-P2-05 三个管理端点的 HTTP 层用例。
//
// service 层已经把这些行为测过了，这里只补 service 测不到的那一层：路由**注册在哪个
// 路径上**、状态码与 {code,data,message} 信封、以及"省略 body"和"显式传 0"这两种调用方
// 写法经 binding 之后是否还分得开。走 RegisterRoutes 而不是直挂 handler，是因为
// `/:id` 与 `/:id/versions` 的共存只有真路由表能验（直挂的话路径写错照样绿）。
package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	ragcache "hivemtk-user/internal/aiagent/rag/cache"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func newKBHandlerRouter(t *testing.T) (*gin.Engine, *gorm.DB, uint) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	// rag_answer_cache 也要建：KBVersionInfo 会数各命名空间的行数，缺表就是 500。
	database := testutil.NewTestDB(t, &model.KnowledgeBase{}, &model.AgentKBBinding{}, &ragcache.RAGAnswerCache{})
	ctrl := &KnowledgeBaseController{svc: service.NewKnowledgeBaseService(database)}
	router := gin.New()
	// 与 router/service_routes.go 同一前缀挂法（/api/knowledge-bases）。
	ctrl.RegisterRoutes(router.Group("/api"))

	enabled := true
	kb := &model.KnowledgeBase{
		KBCode: "kb-handler-t-p2-05", Type: model.KnowledgeBaseTypeFAQ, Name: "端点库",
		OwnerType: model.KnowledgeBaseOwnerShared, Enabled: &enabled,
	}
	if err := ctrl.svc.CreateKB(context.Background(), kb); err != nil || kb.ID == 0 {
		t.Fatalf("建 KB 失败: %v (id=%d)", err, kb.ID)
	}
	return router, database, kb.ID
}

// kbHandlerCall 发一次请求并解出信封，返回 HTTP 状态码、body.code 与 body.data。
//
// body.code 用 `any` 收：CLAUDE.md 写的失败信封是数字（`{"code":400,...}`），而
// `response.Error` 今天实际吐的是字符串枚举（`"INVALID_PARAM_1001"`、`"NOT_FOUND_1002"`，
// 见 utils/error_code.go）。这个差异不是本卡引入的，本卡只测 HTTP 状态码与 data，
// 不替这条口径站队——所以这里两种类型都收得下，别让解码本身成为假红的来源。
func kbHandlerCall(t *testing.T, router *gin.Engine, method, path, body string) (int, any, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var envelope struct {
		Code    any            `json:"code"`
		Message string         `json:"message"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("%s %s 响应不是信封: %v body=%s", method, path, err, w.Body.String())
	}
	return w.Code, envelope.Code, envelope.Data
}

func kbHandlerUpdatedAt(t *testing.T, database *gorm.DB, id uint) time.Time {
	t.Helper()
	var out time.Time
	if err := database.Raw(`SELECT updated_at FROM knowledge_bases WHERE id = ?`, id).Scan(&out).Error; err != nil {
		t.Fatalf("读 updated_at: %v", err)
	}
	return out
}

func TestKBVersionHandler_CanaryRoundTrip(t *testing.T) {
	router, database, kbID := newKBHandlerRouter(t)
	base := "/api/knowledge-bases/" + strconv.FormatUint(uint64(kbID), 10)
	path := base + "/canary"

	// percent 显式传 0 必须进得来：*int + binding:"required" 只判 nil。若把字段写成
	// 非指针 int，gin 会把"没传"和"传 0"混成同一个零值，这条就是防它的。
	code, bizCode, data := kbHandlerCall(t, router, http.MethodPut, path, `{"enabled":true,"percent":0}`)
	if code != http.StatusOK || bizCode != float64(0) {
		t.Fatalf("percent=0 应放行（配好参数但不放量），got http=%d code=%v", code, bizCode)
	}
	if data["canary_percent"] != float64(0) || data["canary_enabled"] != true {
		t.Errorf("回显不对: %+v", data)
	}

	code, bizCode, data = kbHandlerCall(t, router, http.MethodPut, path, `{"enabled":true,"percent":30}`)
	if code != http.StatusOK || bizCode != float64(0) || data["canary_percent"] != float64(30) {
		t.Fatalf("合法放量参数没落库回显: http=%d code=%v data=%+v", code, bizCode, data)
	}
	if _, ok := data["rows_by_namespace"]; !ok {
		t.Errorf("回显缺 rows_by_namespace（运营靠它判断灰度是否真的分流了）: %+v", data)
	}
	// 配灰度不许 bump updated_at，否则"先把参数配好"这一步就把现存缓存全失效掉了。
	before := kbHandlerUpdatedAt(t, database, kbID)
	kbHandlerCall(t, router, http.MethodPut, path, `{"enabled":true,"percent":40}`)
	if got := kbHandlerUpdatedAt(t, database, kbID); !got.Equal(before) {
		t.Errorf("PUT /canary 不该动 updated_at：%v ⇒ %v", before, got)
	}

	// 越界比例：controller 必须先判 400，不能让 service 的校验错经 ErrorFromDB 变 500。
	for _, body := range []string{`{"enabled":true,"percent":101}`, `{"enabled":true,"percent":-1}`} {
		if c, _, _ := kbHandlerCall(t, router, http.MethodPut, path, body); c != http.StatusBadRequest {
			t.Errorf("%s 应 400，got %d", body, c)
		}
	}
	// 缺字段：enabled 与 percent 都是必填指针。
	for _, body := range []string{`{"percent":50}`, `{"enabled":true}`, `{}`} {
		if c, _, _ := kbHandlerCall(t, router, http.MethodPut, path, body); c != http.StatusBadRequest {
			t.Errorf("%s 应 400（required 指针为 nil），got %d", body, c)
		}
	}
	if c, _, _ := kbHandlerCall(t, router, http.MethodPut, "/api/knowledge-bases/abc/canary", `{"enabled":true,"percent":5}`); c != http.StatusBadRequest {
		t.Errorf("非数字 ID 应 400，got %d", c)
	}
}

func TestKBVersionHandler_SwitchAndInfo(t *testing.T) {
	router, database, kbID := newKBHandlerRouter(t)
	base := "/api/knowledge-bases/" + strconv.FormatUint(uint64(kbID), 10)

	// 空 body = "把灰度版本转正"。ShouldBindJSON 对空体回 EOF，不单独放行这一条的话，
	// 运营最常用的"什么都不填直接切"会拿到 400。
	code, bizCode, data := kbHandlerCall(t, router, http.MethodPost, base+"/version", ``)
	if code != http.StatusOK || bizCode != float64(0) {
		t.Fatalf("空 body 转正应成功，got http=%d code=%v", code, bizCode)
	}
	if data["version"] != float64(2) || data["stable_namespace"] != "v2" || data["canary_namespace"] != "v3" {
		t.Errorf("转正后命名空间回显不对: %+v", data)
	}
	if enabled, ok := data["canary_enabled"].(bool); !ok || enabled {
		t.Errorf("转正必须顺手清灰度，回显却还带着: %+v", data)
	}
	if data["canary_percent"] != float64(0) {
		t.Errorf("转正必须把比例清零，got %+v", data)
	}
	// 切版本不得 bump updated_at —— 它一前进，cache/service.fresh() 就把另一版本的行删了。
	if got := kbHandlerUpdatedAt(t, database, kbID); got.IsZero() {
		t.Fatal("updated_at 读成零值")
	}

	// 显式回滚到 1 号；负版本号判 400。
	code, _, data = kbHandlerCall(t, router, http.MethodPost, base+"/version", `{"version":1}`)
	if code != http.StatusOK || data["version"] != float64(1) || data["stable_namespace"] != "v1" {
		t.Fatalf("回滚到 1 号没生效: http=%d data=%+v", code, data)
	}
	if c, _, _ := kbHandlerCall(t, router, http.MethodPost, base+"/version", `{"version":-2}`); c != http.StatusBadRequest {
		t.Errorf("负版本号应 400，got %d", c)
	}

	// GET /versions 与写接口的回显同源；不存在的 KB 判 404 而不是 200+空对象。
	code, _, data = kbHandlerCall(t, router, http.MethodGet, base+"/versions", ``)
	if code != http.StatusOK || data["kb_id"] != float64(kbID) {
		t.Fatalf("版本信息读取不对: http=%d data=%+v", code, data)
	}
	// 这条同时钉住默认态：测试进程没设 FF_LTC_KB_CANARY ⇒ 必须是 off。
	if mode, ok := data["mode"].(string); !ok || mode != "off" {
		t.Errorf("默认旗子态应是 off，got %v", data["mode"])
	}
	if c, _, _ := kbHandlerCall(t, router, http.MethodGet, "/api/knowledge-bases/999999999/versions", ``); c != http.StatusNotFound {
		t.Errorf("不存在的 KB 应 404，got %d", c)
	}
}
