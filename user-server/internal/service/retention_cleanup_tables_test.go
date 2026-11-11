// retention_cleanup_tables_test.go CC-04 刀2 判据用例：九表接入的 TTL 边界
// （窗内不动/窗外删除）、单轮上限触发格、幂等重跑 0 行、逐表 tenant 作用域负例
// （不误删他租户）、operation_logs 自动调度接线回归、种子↔规格一致性。
package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// retentionSeedRow 按表形态插一行探针数据（唯一键列逐行区分）。
func retentionSeedRow(t *testing.T, db *gorm.DB, ctx context.Context, table string, ts time.Time, n int) {
	t.Helper()
	var err error
	switch table {
	case (model.APILog{}).TableName():
		err = db.WithContext(ctx).Create(&model.APILog{CreatedAt: ts}).Error
	case (model.LLMRoutingLog{}).TableName():
		err = db.WithContext(ctx).Create(&model.LLMRoutingLog{Scenario: "s", Provider: "p", Model: "m", CreatedAt: ts}).Error
	case (model.ToolCallAudit{}).TableName():
		err = db.WithContext(ctx).Create(&model.ToolCallAudit{ToolName: "t", CreatedAt: ts}).Error
	case (model.FeatureFlagEvalLog{}).TableName():
		err = db.WithContext(ctx).Create(&model.FeatureFlagEvalLog{FlagKey: "f", CreatedAt: ts}).Error
	case (model.LayerDecisionLog{}).TableName():
		err = db.WithContext(ctx).Create(&model.LayerDecisionLog{Layer: "l", Reason: "r", CreatedAt: ts}).Error
	case (model.HandoffDecisionRecord{}).TableName():
		err = db.WithContext(ctx).Create(&model.HandoffDecisionRecord{
			DecisionID: fmt.Sprintf("d-%d", n), SessionID: "s", CustomerID: "c",
			Reason: "r", Confidence: 0.5, Threshold: 0.5, IntentType: "i",
			CustomerLevel: "normal", CreatedAt: ts,
		}).Error
	case (model.TraceEvent{}).TableName():
		err = db.WithContext(ctx).Create(&model.TraceEvent{
			TraceID: fmt.Sprintf("tr-%d", n), SpanID: fmt.Sprintf("sp-%d", n),
			Kind: "test", Service: "svc", Operation: "op", Timestamp: ts,
		}).Error
	case (model.PlatformLLMUsageRecord{}).TableName():
		err = db.WithContext(ctx).Create(&model.PlatformLLMUsageRecord{
			RequestID: fmt.Sprintf("rq-%d", n), Provider: "p", Model: "m",
			OccurredAt: ts, CreatedAt: ts,
		}).Error
	case (model.OperationLog{}).TableName():
		err = db.WithContext(ctx).Create(&model.OperationLog{
			UserID: 1, Username: "u", Action: "a", Module: "m", CreatedAt: ts,
		}).Error
	default:
		t.Fatalf("未登记表 %s", table)
	}
	if err != nil {
		t.Fatalf("seed %s row %d: %v", table, n, err)
	}
}

// retentionCountAt 按表的时间列数「某租户在某一精确种子时刻」的行数
// （system scope 跨租户数）。直接对种子时刻计数，避免用 now 做边界把
// 「窗内 -1h」行也算进「< now」侧的口径错。
func retentionCountAt(t *testing.T, db *gorm.DB, spec RetentionSpec, tenantID uint, ts time.Time) int64 {
	t.Helper()
	var n int64
	if err := db.WithContext(tenant.WithSystemScope(context.Background())).
		Model(spec.Model).
		Where("tenant_id = ? AND "+spec.TimeCol+" = ?", tenantID, ts).
		Count(&n).Error; err != nil {
		t.Fatalf("count %s: %v", spec.Table, err)
	}
	return n
}

func TestRetentionTableSpecs_RegisteredTables(t *testing.T) {
	specs := retentionTableSpecs()
	if len(specs) != 9 {
		t.Fatalf("规格数 = %d, want 9（八表 + operation_logs）", len(specs))
	}
	seen := map[string]bool{}
	for _, s := range specs {
		if s.Model == nil || s.TimeCol == "" || s.Days == nil || s.DefaultDays <= 0 {
			t.Errorf("规格 %s 字段不完整: %+v", s.Table, s)
		}
		if seen[s.Table] {
			t.Errorf("表 %s 重复登记", s.Table)
		}
		seen[s.Table] = true
	}
	for _, want := range []string{
		"api_logs", "llm_routing_logs", "tool_call_audits", "feature_flag_eval_logs",
		"layer_decision_logs", "handoff_decisions", "trace_events",
		"platform_llm_usage_records", "operation_logs",
	} {
		if !seen[want] {
			t.Errorf("表 %s 未接入", want)
		}
	}
	for _, s := range specs {
		if s.Table == "trace_events" && s.TimeCol != "timestamp" {
			t.Errorf("trace_events 度量列必须是 timestamp（无 CreatedAt），实际 %q", s.TimeCol)
		}
		if s.Table == "platform_llm_usage_records" && s.DefaultDays != 180 {
			t.Errorf("platform_llm_usage_records 兜底窗口 = %d, want 180（PLT-01 读端已上线仍从宽）", s.DefaultDays)
		}
	}
}

func TestRetentionCleanupCron_WiredWithNineSpecs(t *testing.T) {
	// operation_log.DeleteOldLogs 自动调度接线回归：看门狗带上九表规格
	// （人工端点保留不动，repo.DeleteOldLogs 仍是它的路径）。
	if retentionCleanupCron == nil || retentionCleanupCron.cleaner == nil {
		t.Fatal("init() 未装配 retentionCleanupCron")
	}
	specs := retentionCleanupCron.cleaner.specs
	if len(specs) != 9 {
		t.Fatalf("cron 规格数 = %d, want 9", len(specs))
	}
	hasOpLog := false
	for _, s := range specs {
		if s.Table == (model.OperationLog{}).TableName() {
			hasOpLog = true
		}
	}
	if !hasOpLog {
		t.Error("cron 规格缺 operation_logs：自动调度未接线（人工端点之外的周期清理路径）")
	}
}

func TestRetentionCleaner_NineTables_TTLBoundaryAndIdempotent(t *testing.T) {
	db := testutil.NewTestDB(t,
		&model.APILog{}, &model.LLMRoutingLog{}, &model.ToolCallAudit{},
		&model.FeatureFlagEvalLog{}, &model.LayerDecisionLog{},
		&model.HandoffDecisionRecord{}, &model.TraceEvent{},
		&model.PlatformLLMUsageRecord{}, &model.OperationLog{})
	if db == nil {
		t.Fatal("测试库不可达")
	}
	cleaner := NewRetentionCleaner(repository.NewRetentionCleanupRepoWithDB(db), retentionTableSpecs())

	now := time.Now()
	outAt := now.Add(-400 * 24 * time.Hour) // 九表窗口（30~180d）之外的公共窗外时刻
	inAt := now.Add(-1 * time.Hour)         // 九表窗口之内的公共窗内时刻

	specs := retentionTableSpecs()
	for i, s := range specs {
		retentionSeedRow(t, db, context.Background(), s.Table, outAt, 1000+i) // 窗外
		retentionSeedRow(t, db, context.Background(), s.Table, inAt, 2000+i)  // 窗内
	}

	// 第一轮：逐表窗外删 1、窗内留 1。
	results := cleaner.RunOnce(context.Background())
	if len(results) != 9 {
		t.Fatalf("结果数 = %d, want 9", len(results))
	}
	for _, r := range results {
		if r.Err != nil || r.Skipped != "" {
			t.Errorf("表 %s: Err=%v Skipped=%q, want 均空", r.Table, r.Err, r.Skipped)
		}
		if r.Deleted != 1 {
			t.Errorf("表 %s 第一轮删除 = %d, want 1（窗外一行）", r.Table, r.Deleted)
		}
	}
	for _, s := range specs {
		if got := retentionCountAt(t, db, s, 1, inAt); got != 1 {
			t.Errorf("表 %s 窗内行被误删 = %d, want 保留 1（窗内不动）", s.Table, got)
		}
		if got := retentionCountAt(t, db, s, 1, outAt); got != 0 {
			t.Errorf("表 %s 窗外行未删 = %d, want 0（窗外删除）", s.Table, got)
		}
	}

	// 第二轮：逐表 0 行（删除天然幂等）。
	results2 := cleaner.RunOnce(context.Background())
	for _, r := range results2 {
		if r.Deleted != 0 {
			t.Errorf("表 %s 幂等重跑删除 = %d, want 0", r.Table, r.Deleted)
		}
	}
}

func TestRetentionCleaner_NineTables_TenantScopeNegative(t *testing.T) {
	db := testutil.NewTestDB(t,
		&model.APILog{}, &model.LLMRoutingLog{}, &model.ToolCallAudit{},
		&model.FeatureFlagEvalLog{}, &model.LayerDecisionLog{},
		&model.HandoffDecisionRecord{}, &model.TraceEvent{},
		&model.PlatformLLMUsageRecord{}, &model.OperationLog{})
	if db == nil {
		t.Fatal("测试库不可达")
	}

	// on 模式 + 活跃租户仅 B：逐表验证「不误删他租户」——A 租户窗外行本轮不删、
	// B 租户窗外行删、B 租户窗内行不动。
	const tenantA, tenantB = 2001, 2002
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	tenant.SetActiveTenantLoader(func(ctx context.Context) ([]uint, error) {
		return []uint{tenantB}, nil
	})
	t.Cleanup(func() { tenant.SetActiveTenantLoader(nil) })

	cleaner := NewRetentionCleaner(repository.NewRetentionCleanupRepoWithDB(db), retentionTableSpecs())
	now := time.Now()
	outAt := now.Add(-400 * 24 * time.Hour)
	inAt := now.Add(-1 * time.Hour)

	specs := retentionTableSpecs()
	for i, s := range specs {
		retentionSeedRow(t, db, tenant.WithTenantID(context.Background(), tenantA), s.Table, outAt, 3000+i) // A 窗外
		retentionSeedRow(t, db, tenant.WithTenantID(context.Background(), tenantB), s.Table, outAt, 4000+i) // B 窗外
		retentionSeedRow(t, db, tenant.WithTenantID(context.Background(), tenantB), s.Table, inAt, 5000+i)  // B 窗内
	}

	results := cleaner.RunOnce(context.Background())
	if len(results) != 9 {
		t.Fatalf("结果数 = %d, want 9", len(results))
	}
	for _, r := range results {
		if r.Err != nil || r.Skipped != "" {
			t.Errorf("表 %s: Err=%v Skipped=%q, want 均空", r.Table, r.Err, r.Skipped)
		}
		if r.Deleted != 1 {
			t.Errorf("表 %s 删除 = %d, want 1（只删本租户窗外行）", r.Table, r.Deleted)
		}
	}
	for _, s := range specs {
		if got := retentionCountAt(t, db, s, tenantA, outAt); got != 1 {
			t.Errorf("表 %s 他租户（A）窗外行被误删 = %d, want 保留 1（逐租户扇出隔离）", s.Table, got)
		}
		if got := retentionCountAt(t, db, s, tenantA, inAt); got != 0 {
			t.Errorf("表 %s 他租户（A）不应有窗内行 = %d, want 0（夹具只种了窗外行）", s.Table, got)
		}
		if got := retentionCountAt(t, db, s, tenantB, outAt); got != 0 {
			t.Errorf("表 %s 本租户窗外行未删 = %d, want 0", s.Table, got)
		}
		if got := retentionCountAt(t, db, s, tenantB, inAt); got != 1 {
			t.Errorf("表 %s 本租户窗内行被误删 = %d, want 保留 1", s.Table, got)
		}
	}
}

func TestRetentionCleaner_NineSpecsRealTableBatchLimitDrains(t *testing.T) {
	db := testutil.NewTestDB(t, &model.APILog{})
	if db == nil {
		t.Fatal("测试库不可达")
	}
	specs := retentionTableSpecs()
	cleaner := NewRetentionCleaner(repository.NewRetentionCleanupRepoWithDB(db), specs[:1]) // api_logs
	cleaner.batchLimit = 2                                                                  // 注入小批上限，5 行需 3 条语句（2+2+1）

	now := time.Now()
	for i := 0; i < 5; i++ {
		if err := db.Create(&model.APILog{CreatedAt: now.Add(-400 * 24 * time.Hour)}).Error; err != nil {
			t.Fatalf("seed row %d: %v", i, err)
		}
	}

	results := cleaner.RunOnce(context.Background())
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("结果异常: %+v", results)
	}
	if results[0].Deleted != 5 {
		t.Fatalf("单轮上限触发后应循环删完 = %d, want 5（2+2+1 三条语句）", results[0].Deleted)
	}
	var left int64
	if err := db.Model(&model.APILog{}).Count(&left).Error; err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatalf("残留行数 = %d, want 0", left)
	}
	again := cleaner.RunOnce(context.Background())
	if again[0].Deleted != 0 {
		t.Fatalf("幂等重跑应删 0 行，实际 %d", again[0].Deleted)
	}
}

func TestRetentionSeeds_MatchSpecsParity(t *testing.T) {
	// 种子↔规格一致性：retention 组恰 9 条、键集合与九表一一对应、
	// DefaultValue 与规格 DefaultDays 逐值相等（改一处要改两处的回归网）。
	want := map[string]string{
		"api_logs_days":                   "30",
		"llm_routing_logs_days":           "90",
		"tool_call_audits_days":           "90",
		"feature_flag_eval_logs_days":     "30",
		"layer_decision_logs_days":        "30",
		"handoff_decisions_days":          "90",
		"trace_events_days":               "30",
		"platform_llm_usage_records_days": "180",
		"operation_logs_days":             "90",
	}
	got := map[string]string{}
	for _, d := range DefaultParamDefs() {
		if d.Group == "retention" {
			got[d.Key] = d.DefaultValue
			if d.ValueType != "int" {
				t.Errorf("retention.%s ValueType = %q, want int", d.Key, d.ValueType)
			}
		}
	}
	if len(got) != len(want) {
		t.Fatalf("retention 组种子数 = %d, want %d: %v", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("retention.%s DefaultValue = %q, want %q", k, got[k], v)
		}
	}
}
