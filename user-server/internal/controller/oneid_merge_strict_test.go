package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
)

// FA-44：未知字段必须明确 400，不能静默忽略后前端把未被支持的能力显示为已保存。
func TestFA44_SaveMergeRulesRejectsUnknownField(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := testutil.NewTestDB(t, &model.SystemConfigKV{})
	db.SetTestDB(database)

	ctrl := NewCustomerOneIDController()
	router := gin.New()
	router.POST("/api/oneid/merge-rules", ctrl.SaveMergeRules)

	body := map[string]any{
		"built_in": []any{},
		"custom": []any{
			map[string]any{"name": "x", "fields": []string{"phone"}, "enabled": true},
		},
		"strategy":              map[string]any{"primary_rule": "latest_active", "conflict_behavior": "auto_merge"},
		"unsupported_field_xyz": "should-be-rejected",
	}
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/oneid/merge-rules", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("未知字段应返回 400，实际 %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "请求参数错误") {
		t.Fatalf("错误信息应说明参数不合法: %s", w.Body.String())
	}
}
