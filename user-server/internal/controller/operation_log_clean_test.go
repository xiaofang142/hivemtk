package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

// #825 回归：POST /api/operation-logs/clean 是破坏性接口，后端 binding 字段为
// before_date（snake_case）。历史前端传 { beforeDate } 时字段被静默忽略，
// 任意日期都回落默认 90 天窗口——"没报错"掩盖了参数没生效。
//
// 守卫语义：请求体既没有 before_date 也没有 days（两键全缺）时必须 400，
// 拒绝靠猜的默认清理；真缺省（空 body {}）维持原有 90 天文档行为不变。
func TestOperationLogController_CleanLogs_RejectsUnknownPayloadKeys(t *testing.T) {
	ctrl, router := setupOperationLogController(t)
	_ = ctrl
	router.POST("/operation-logs/clean", ctrl.CleanLogs)

	// 修复前：{ beforeDate } 被静默忽略 → 200 按 90 天清理。
	req, _ := http.NewRequest("POST", "/operation-logs/clean",
		bytes.NewBufferString(`{"beforeDate":"2000-01-01"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("仅携带未知键 beforeDate 的破坏性清理应 400（防止静默按默认窗口删数据），实际 %d body=%s", w.Code, w.Body.String())
	}
}

// 真正携带 before_date 的请求不受守卫影响，正常按日期清理。
func TestOperationLogController_CleanLogs_AcceptsSnakeCaseDate(t *testing.T) {
	ctrl, router := setupOperationLogController(t)
	_ = ctrl
	router.POST("/operation-logs/clean", ctrl.CleanLogs)

	req, _ := http.NewRequest("POST", "/operation-logs/clean",
		bytes.NewBufferString(`{"before_date":"2000-01-01"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("before_date 合法日期应 200，实际 %d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("2000-01-01")) {
		t.Errorf("响应应回显清理截点日期，body=%s", w.Body.String())
	}
}

// days 路径不受守卫影响。
func TestOperationLogController_CleanLogs_AcceptsDays(t *testing.T) {
	ctrl, router := setupOperationLogController(t)
	_ = ctrl
	router.POST("/operation-logs/clean", ctrl.CleanLogs)

	req, _ := http.NewRequest("POST", "/operation-logs/clean",
		bytes.NewBufferString(`{"days":30}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("days 合法值应 200，实际 %d body=%s", w.Code, w.Body.String())
	}
}
