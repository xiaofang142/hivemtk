package service

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
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

// defaultParamDefsWant 参数定义的条数锚点。
//
// 它不验任何行为，只钉一件事：**新增或删掉一条参数定义时必须有人看见**。
// 定义被漏掉一条时别处不会红 —— seed 那条用例比的是"库里的行数 vs 定义条数"，
// 两边一起少就永远成立，所以这里留一个必须手动改的数（改之前先确认新增那条确实
// 该进 DefaultParamDefs()，而不是"顺手多加了一个"）。
// 2026-09-20（T-P3-03）：+1 = `human_task.handoff_first_response_minutes`。
// 2026-09-28：+2 = `bridge.outbound_orphan_ttl` / `bridge.outbound_orphan_dry_run`（桥接出站孤儿结算的阈值与"只报数"闸门）。
//
// 2026-10-10（第一次）：回退到 114。曾按工作区里那条尚未入库的 `cache.faq_answer_enabled`
// （种子 + app/faq_cache_wiring.go 都还在途）把本锚点顶到 115，结果 HEAD 上的
// `config_param_seeds.go` 只有 114 条，干净检出直接红——把别人在途的条数算进
// 自己的锚点，等于替那条尚未发生的提交背书。
//
// 2026-10-10（第二次）：回到 115。上面那次入库已经发生——`cache.faq_answer_enabled`
// 的种子与装配点 `app/faq_cache_wiring.go` 都进了 master，于是本锚点第二次亮红。
// 这正是它该干的事：条数变化必须有人看见，而不是悄悄漂过去。红灯与修复分属两条提交，
// 免得后来人分不清这条到底是谁加的。
//
// 2026-10-10（第三次，阶段二 2a）：+2 = `misc.upload_max_size_mb` /
// `middleware.max_json_body_mb`，把上传上限的三份副本与全局请求体上限收进参数中心。
const defaultParamDefsWant = 117

func TestDefaultParamDefsCount(t *testing.T) {
	defs := DefaultParamDefs()
	// 判据与提示语同源：上一版条件里是 111、失败消息里写 "want 110"、成功日志里写
	// "106 validated"，三处各自漂移 ⇒ 真红了也读不出当前到底几条。
	if len(defs) != defaultParamDefsWant {
		t.Fatalf("want %d defs, got %d", defaultParamDefsWant, len(defs))
	}
	for i, d := range defs {
		if d.Group == "" || d.Key == "" || d.DefaultValue == "" {
			t.Errorf("def[%d] bad: group=%q key=%q default=%q", i, d.Group, d.Key, d.DefaultValue)
		}
	}
	t.Logf("✅ %d default defs validated", defaultParamDefsWant)
}

// 参数唯一性的口径是 **(param_group, key) 复合**，不是单列 key。
//
// 历史坑：model 上那条名字看起来像复合唯一的索引 idx_group_key，在存量库里实际是
// `UNIQUE (key)` 单列。两个 group 用同名 key 时，SeedConfigParams 的第二条 Create
// 撞唯一索引，而它只 logger.Warnf 后 continue —— 表现是"这条参数在控制台上永远
// 不存在"，读侧一路走代码里的 fallback，运维改不动、也没人报警。
//
// 现已闭环：migration v3.47.0 把 idx_group_key 从 `UNIQUE (key)` 重建为
// `UNIQUE (param_group, key)`，模型标签（Group priority:1 / Key priority:2）与之���致，
// 种子插入失败也不再静默（SeedConfigParams 汇总成 error 返回）。跨 group 同名 key
// 现在是**合法**的两条参数行，`bridge.max_tokens` 与 `misc.max_tokens` 互不冲突。
//
// 所以本守卫改成盯复合唯一本身：组内重名才是真的坏数据，
// 因为 GetByGroupKey / UpdateValue 都按 (group,key) 定位，重名会让更新打到错误的那一行。
func TestDefaultParamDefsGroupKeyUnique(t *testing.T) {
	seen := make(map[string]string, len(DefaultParamDefs()))
	var dupes []string
	for _, d := range DefaultParamDefs() {
		ident := d.Group + "/" + d.Key
		if prev, ok := seen[ident]; ok {
			dupes = append(dupes, fmt.Sprintf("%s（与 %s 冲突，读取侧按 (group,key) 定位会打到错误行）", ident, prev))
		}
		seen[ident] = ident
	}
	if len(dupes) > 0 {
		sort.Strings(dupes)
		t.Errorf("参数 (param_group, key) 组内重名：%s", strings.Join(dupes, "; "))
	}
}

// 审计行必须带上 group。只记 key 的审计在复合唯一下是歧义的：
// 看到 `max_tokens` 被改成 800，无法判断改的是哪一组的那个参数。
func TestConfigParamAuditLogCarriesGroup(t *testing.T) {
	fields := reflect.TypeOf(model.ConfigParamAuditLog{})
	got, ok := fields.FieldByName("ParamGroup")
	if !ok {
		t.Fatal("ConfigParamAuditLog 缺 ParamGroup 字段：复合唯一下审计只记 key 无法区分是哪一组的参数")
	}
	if col := got.Tag.Get("gorm"); !strings.Contains(col, "column:param_group") {
		t.Errorf("ParamGroup 的 gorm 标签应显式指定 column:param_group，实际 %q", col)
	}
}
