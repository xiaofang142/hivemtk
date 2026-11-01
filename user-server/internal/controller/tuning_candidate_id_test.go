package controller

// 契约：PUT /api/admin/tuning/prompt/candidates/:id/status 的 id 非数字时，
// 必须在入口回 400「无效的候选ID」，且**不得**把 SQL 原文回传给调用方。
//
// 坑（2026-11-02 API 全量回归实测）：id 直接塞进 bigint 查询，PG 回
// `invalid input syntax for type bigint: "nonexistent" (SQLSTATE 22P02)`；
// 而 handler 的 fallbackMsg 写成 "update failed: "+err.Error()，ErrorFromDB 的
// 文本分派命中 "invalid input" ⇒ 400 且把整段 SQL 原文当作对外文案回显。

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestUpdatePromptCandidateStatus_RejectsNonNumericIDWithoutSQLLeak(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// svc 为 nil：非数字 id 必须在触达 service 之前就被拦下，
	// 若实现回退到调 service，这里会因 nil 指针 panic —— 正是要锁住的边界。
	ctrl := &TuningController{}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"status":"approved"}`)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/admin/tuning/prompt/candidates/nonexistent/status", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: "nonexistent"}}

	ctrl.UpdatePromptCandidateStatus(c)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非数字 id 应回 400，实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "SQLSTATE") || strings.Contains(rec.Body.String(), "bigint") {
		t.Fatalf("响应体不得包含 SQL 原文：%s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "无效的候选ID") {
		t.Fatalf("应给出可操作提示「无效的候选ID」，实际 %s", rec.Body.String())
	}
}
