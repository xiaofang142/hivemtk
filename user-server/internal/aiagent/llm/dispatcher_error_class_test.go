package llm

import (
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// classifyErrorClass 是读取侧 errorClassExpr（repository/llm_usage_report.go）的写入侧镜像。
// 本表钉住两侧单点对齐的关键词口径：任一侧改词表/顺序而不同步另一侧，这里必须红。
func TestClassifyErrorClass_MirrorsReadSideExpr(t *testing.T) {
	cases := []struct {
		name    string
		success bool
		errMsg  string
		want    string
	}{
		{"成功行恒空串", true, "status=401 invalid_api_key", ""},
		{"auth 无效密钥", false, "Invalid Authentication: invalid_api_key provided", "auth"},
		{"auth status=401", false, "request failed: status=401", "auth"},
		{"auth unauthorized", false, "UNAUTHORIZED access", "auth"},
		{"rate_limit 429", false, "error: status=429 too many requests", "rate_limit"},
		{"rate_limit quota", false, "insufficient_quota: you exceeded your current quota", "rate_limit"},
		{"timeout", false, "context deadline exceeded", "timeout"},
		{"timeout timed out", false, "Post \"http://x\": request timed out", "timeout"},
		{"upstream 5xx", false, "status=503 service overloaded", "upstream"},
		{"upstream conn refused", false, "dial tcp: connection refused", "upstream"},
		{"upstream no such host", false, "dial tcp: no such host", "upstream"},
		{"顺序钉住：auth 先于 timeout（错误信息同时含两族关键词）", false, "invalid_api_key after timeout", "auth"},
		{"未知归 unknown", false, "something exploded weirdly", "unknown"},
		{"空错误信息归 unknown", false, "", "unknown"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classifyErrorClass(c.success, c.errMsg); got != c.want {
				t.Fatalf("classifyErrorClass(success=%v, msg=%q) = %q, want %q", c.success, c.errMsg, got, c.want)
			}
		})
	}
}

// 落库行必须带 error_class（map 插入不经过 GORM schema，列值完全取决于 row map）。
// 镜像 TestLogRoutingDecision_CreatedAtIsPersisted 的口径：写入方自带该列，不靠 DB 默认值。
func TestLogRoutingDecision_ErrorClassIsPersisted(t *testing.T) {
	db := testutil.NewTestDB(t, &model.LLMRoutingLog{})
	setAuditDB(db)
	defer setAuditDB(nil)
	ResetTokenSourceStats()
	defer ResetTokenSourceStats()

	// trace_id 列 varchar(64)：前缀必须短，"trace-…-" + t.Name() 会超长（整行插入失败仅 Warn）。
	failTraceID := "trace-ecf-" + t.Name()[len("TestLogRoutingDecision_"):]
	LogRoutingDecision(t.Context(), &LogEntry{
		TraceID:     failTraceID,
		Scenario:    ScenarioSOPReply,
		Source:      SourceDispatch,
		TokenSource: TokenSourceActual,
		Success:     false,
		ErrorMsg:    "request failed: status=401 invalid_api_key",
	})
	okTraceID := "trace-eco-" + t.Name()[len("TestLogRoutingDecision_"):]
	LogRoutingDecision(t.Context(), &LogEntry{
		TraceID:     okTraceID,
		Scenario:    ScenarioSOPReply,
		Source:      SourceDispatch,
		TokenSource: TokenSourceActual,
		Success:     true,
	})

	var failRow struct{ ErrorClass string }
	if err := db.Table("llm_routing_logs").
		Select("error_class").
		Where("trace_id = ?", failTraceID).
		Scan(&failRow).Error; err != nil {
		t.Fatalf("读回失败行路由日志失败: %v", err)
	}
	if failRow.ErrorClass != "auth" {
		t.Fatalf("失败行 error_class = %q, want auth（读取侧表达式对非空 error_class 直取，写入侧必须标对）", failRow.ErrorClass)
	}

	var okRow struct{ ErrorClass string }
	if err := db.Table("llm_routing_logs").
		Select("error_class").
		Where("trace_id = ?", okTraceID).
		Scan(&okRow).Error; err != nil {
		t.Fatalf("读回成功行路由日志失败: %v", err)
	}
	if okRow.ErrorClass != "" {
		t.Fatalf("成功行 error_class = %q, want 空串（与 llm_usage_daily 存量口径一致）", okRow.ErrorClass)
	}
}
