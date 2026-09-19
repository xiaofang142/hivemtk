package llm

import (
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// LogRoutingDecision 用 map 走 Table().Create()，map 不携带 schema，
// GORM 的 autoCreateTime 因此不生效。生产库的 created_at 原本靠 DDL 的
// DEFAULT NOW() 兜底，而启动期 AutoMigrate 按模型 tag（无 default）把该默认值
// 抹掉了 —— 于是新行 created_at 全为 NULL，成本/时延类统计查询静默漏数。
// 该测试钉住「写入方必须自带 created_at」，不再依赖 DB 默认值。
func TestLogRoutingDecision_CreatedAtIsPersisted(t *testing.T) {
	db := testutil.NewTestDB(t, &model.LLMRoutingLog{})
	setAuditDB(db)
	defer setAuditDB(nil)
	ResetTokenSourceStats()
	defer ResetTokenSourceStats()

	traceID := "trace-created-at-" + t.Name()
	LogRoutingDecision(t.Context(), &LogEntry{
		TraceID:     traceID,
		Scenario:    ScenarioSOPReply,
		Source:      SourceDispatch,
		TokenSource: TokenSourceActual,
	})

	var got struct{ CreatedAt *time.Time }
	if err := db.Table("llm_routing_logs").
		Select("created_at").
		Where("trace_id = ?", traceID).
		Scan(&got).Error; err != nil {
		t.Fatalf("读回路由日志失败: %v", err)
	}
	if got.CreatedAt == nil {
		t.Fatal("created_at 落库为 NULL：map 插入不会触发 autoCreateTime，必须显式写入")
	}
	if d := time.Since(*got.CreatedAt); d > time.Minute || d < -time.Minute {
		t.Fatalf("created_at 偏离写入时刻过多: %v (diff=%s)", *got.CreatedAt, d)
	}
}
