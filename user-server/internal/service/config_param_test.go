package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func TestSeedConfigParams(t *testing.T) {
	gdb := testutil.NewTestDBOrSkip(t, &model.ConfigParam{}, &model.ConfigParamAuditLog{})
	if gdb == nil {
		t.Skip("no DB")
	}
	if err := SeedConfigParams(context.Background(), gdb); err != nil {
		t.Fatalf("Seed failed: %v", err)
	}
	var count int64
	gdb.Model(&model.ConfigParam{}).Count(&count)
	// 与定义源联动，而非写死数字：原先写死 109，新增 2 条参数后即失配。
	// 这样仍能捕获"参数被意外漏 seed"，但不会再因正常新增而误报。
	want := int64(len(DefaultParamDefs()))
	if count != want {
		t.Fatalf("seed 后 config_params 行数 = %d，期望 %d（DefaultParamDefs 定义条数）", count, want)
	}

	// 注意：这里刻意用 GlobalConfigParam() 而非自己 new 一个 service。
	// 本用例因此同时钉住两条不变式：
	//   ① seed 后各类型化读取（Duration/Float/Int/Bool/String）能取到定义值；
	//   ② SeedConfigParams 会把全局单例**重绑**到本次 seed 的实例上。
	// ② 曾经被破坏：SetGlobal 原用 sync.Once，第二次 seed 的新实例被静默丢弃，
	// 全局仍指向首个实例，而其按 key 负缓存把"表空时读到的 0"永久固化 →
	// 本用例读到全零。详见 TASKS_AUDIT_2026-09-16.md · TEST-05。
	svc := GlobalConfigParam()
	ctx := context.Background()

	if v := svc.GetDuration(ctx, "bridge", "polling_max_timeout", 0); v != 500*time.Second {
		t.Errorf("bridge.polling_max_timeout = %v, want 500s", v)
	}
	if v := svc.GetFloat(ctx, "knowledge", "similarity_threshold", 0); v != 0.5 {
		t.Errorf("knowledge.similarity_threshold = %v, want 0.5", v)
	}
	if v := svc.GetInt(ctx, "pagination", "page_max_size", 0); v != 100 {
		t.Errorf("pagination.page_max_size = %d, want 100", v)
	}
	if v := svc.GetBool(ctx, "smart_cs", "enable_auto_reply", false); !v {
		t.Errorf("smart_cs.enable_auto_reply = false, want true")
	}
	if v := svc.GetString(ctx, "knowledge", "embedding_dimension", "0"); v != "1024" {
		t.Errorf("knowledge.embedding_dimension = %s, want 1024", v)
	}
	// 条数取自定义源，别写死（历史上这里写 59，后来参数增至 111 却没同步）
	t.Logf("✅ Seed %d params + typed reads all pass", want)
}

func TestConfigParamUpdateResetAudit(t *testing.T) {
	gdb := testutil.NewTestDBOrSkip(t, &model.ConfigParam{}, &model.ConfigParamAuditLog{})
	if gdb == nil {
		t.Skip("no DB")
	}
	if err := SeedConfigParams(context.Background(), gdb); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	svc := NewConfigParamService(gdb)
	ctx := context.Background()

	if err := svc.UpdateValue(ctx, "bridge", "polling_default_timeout", "60", 1); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if v := svc.GetDuration(ctx, "bridge", "polling_default_timeout", 0); v != 60*time.Second {
		t.Errorf("after update = %v, want 60s", v)
	}

	if err := svc.ResetToDefault(ctx, "bridge", "polling_default_timeout", 1); err != nil {
		t.Fatalf("ResetToDefault: %v", err)
	}
	if v := svc.GetDuration(ctx, "bridge", "polling_default_timeout", 0); v != 30*time.Second {
		t.Errorf("after reset = %v, want 30s", v)
	}

	if err := svc.BulkResetGroup(ctx, "knowledge", 1); err != nil {
		t.Fatalf("BulkResetGroup: %v", err)
	}

	var logCount int64
	gdb.Model(&model.ConfigParamAuditLog{}).Count(&logCount)
	if logCount < 2 {
		t.Errorf("audit log count = %d, want >= 2", logCount)
	}

	t.Logf("✅ update/reset/bulk_reset/audit pass")
}

func TestFallbackNilDB(t *testing.T) {
	svc := &ConfigParamService{cache: make(map[string]paramEntry), loaded: make(map[string]bool)}
	ctx := context.Background()
	if v := svc.GetInt(ctx, "any", "missing", 42); v != 42 {
		t.Errorf("fallback int wrong")
	}
	if v := svc.GetFloat(ctx, "any", "missing", 3.14); v != 3.14 {
		t.Errorf("fallback float wrong")
	}
	if v := svc.GetBool(ctx, "any", "missing", true); !v {
		t.Errorf("fallback bool wrong")
	}
	if v := svc.GetString(ctx, "any", "missing", "hello"); v != "hello" {
		t.Errorf("fallback string wrong")
	}
	if v := svc.GetDuration(ctx, "any", "missing", 99*time.Second); v != 99*time.Second {
		t.Errorf("fallback duration wrong")
	}
	t.Logf("✅ nil DB fallback pass")
}

func TestDefaultParamDefsCount(t *testing.T) {
	defs := DefaultParamDefs()
	if len(defs) != 111 {
		t.Fatalf("want 110 defs, got %d", len(defs))
	}
	for i, d := range defs {
		if d.Group == "" || d.Key == "" || d.DefaultValue == "" {
			t.Errorf("def[%d] bad: group=%q key=%q default=%q", i, d.Group, d.Key, d.DefaultValue)
		}
	}
	t.Logf("✅ 106 default defs validated")
}
