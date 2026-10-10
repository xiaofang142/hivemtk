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
//
// 2026-10-10（第四次，阶段二 2c）：+7 = 新建的 `lock` 组。其中 6 条接上了真实的锁
// TTL 读取点（inbox_human / inbox_pending / inbox_content_dedup / ingest_lock /
// ai_processing / message_hub_idem），第 7 条 `lock.ai_lock_ttl` 如实保留「未接线」
// 标注：tryAcquireAILock / ReleaseAILock / IsSessionAIBusy 三个函数整条生产链路都
// 没人调用（源文件自带 //nolint:unused），真正在跑的并发闸是 ai_processing_ttl 那条。
// 这一组与其它组的区别是性质：锁 TTL 写错 = 死锁或重复处理，是正确性参数不是调优项。
//
// 2026-10-10（第五次，阶段二 2d）：+10。前 5 条 `channelbot` 组是渠道侧长度/条数上限与
// 外部依赖超时（qq_message_max_len / tg_message_max_length / tg_inline_rows_max /
// tg_inline_buttons_per_row_max / http_timeout）；接下来 3 条 `sse` 组是业务 SSE Hub
// 自己的参数（heartbeat_interval / max_conn_per_ip / client_buffer_size），与更早 DB 化
// 的 bridge.sse_* 是两套东西——那两条管 bridge 长连接，这组管业务 SSE Hub；最后 2 条补
// 的是 bridge/sse.go 里同文件却仍写死的回放 backlog 与总线缓冲。
// 其中 channelbot.http_timeout / sse.client_buffer_size / bridge.sse_bus_buffer_size
// 三条标 Restart=true：读取点在构造函数里，改完要重启才对已建立的连接生效。
// 2026-10-10（第六次，D12 收尾两批）：+2。
//   - `agent_llm.provider_failover_policy`：LLM 降级策略整份 JSON 从遗留表
//     system_kv_config 搬进来（v3.53.0 带值），读路径改走本服务的 60s 缓存。
//   - `embedding.global_override`：全局 Embedding 提供商覆盖从遗留表 system_config_kv
//     搬进来（v3.54.0 带值），写路径终于有了来路审计。
//
// 同一趟还发现常量本身已经漂移过：`lead` 组的 llm_refine_enabled / industry_profile
// （38ea7489）加进种子时没同步这里，所以 134 → 实际 136 → 138 一次对平。
// 顺带说明下面那条循环判据为什么改成"只有 string 允许空默认值"：
// 计数那格的 Fatalf 一直先炸，循环从没跑到过，industry_profile 的空默认因此被藏了很久。
//
// 2026-10-10（第七次，阶段三 3.1 第一批）：+9 = 拟人度评估剩下三个边界 + 圈选一对 +
// 编辑锁 TTL + 短期记忆 TTL + 可见性趋势默认天数。
//   - confidence.humanize_boundary_low / boundary_high / sample_rate / max_retry：
//     DefaultThreshold 早在 1.2 就已接上，这四条是与它同一套判据的其余旋钮
//     （阈值定「谁算不达标」、边界定「哪些算边缘样本值得送 LLM」、采样率定「其中送
//     多少」、重试定「重写几轮」），一直散在 humanize/service.go 里没人读参数中心。
//     三个 float 的合法区间并不相同：boundary_low 是 [0,1)、boundary_high 是 (0,1]、
//     sample_rate 是 [0,1]（0 = 一个都不送 LLM，是合法语义）。
//   - sales.audience_default_limit / audience_max_limit：成对出现，只接一个会让另一个
//     仍是编译期常量。默认值决定「运营没填 limit 时一轮圈多少人」，上限是并发闸。
//   - misc.edit_lock_ttl：这把锁没有后台清理线程，TTL 同时是持锁人异常退出后的自愈时间，
//     所以调它是在协作灵敏度与坐席被锁时长之间取舍，不是纯调优。
//   - memory.l1_ttl_hours：短期记忆存活秒数，同时是多轮召回的窗口。
//   - geo.default_visibility_days：拆分原 `q.Days <= 0 || q.Days > 365` 那一个条件
//     得来的两个语义——没给走参数中心，给超了仍夹 365（查询成本闸，不随默认值放大）。
const defaultParamDefsWant = 147

func TestDefaultParamDefsCount(t *testing.T) {
	defs := DefaultParamDefs()
	// 判据与提示语同源：上一版条件里是 111、失败消息里写 "want 110"、成功日志里写
	// "106 validated"，三处各自漂移 ⇒ 真红了也读不出当前到底几条。
	if len(defs) != defaultParamDefsWant {
		t.Fatalf("want %d defs, got %d", defaultParamDefsWant, len(defs))
	}
	for i, d := range defs {
		if d.Group == "" || d.Key == "" {
			t.Errorf("def[%d] bad: group=%q key=%q", i, d.Group, d.Key)
		}
		// 空默认值只对自由文本合法：int/float/bool/duration 的空默认读不出兜底值，
		// 而"重置为默认"会把这一格变成"未配置"——那是两种不同的动作。
		if d.DefaultValue == "" && d.ValueType != "string" {
			t.Errorf("def[%d] bad: %s.%s 默认值为空而 value_type=%q（非自由文本必须有可解析的默认值）",
				i, d.Group, d.Key, d.ValueType)
		}
	}
	t.Logf("✅ %d default defs validated", defaultParamDefsWant)
}

// 空默认值的豁免面钉成一条具名清单，而不是"凡是 string 都放行"：
// 今天只有一格真的没有有意义的默认（lead.industry_profile 是一段行业自由描述，
// 空 = 未填行业画像，读侧本来就按未配置处理）。清单有上界，新增一格就得在这里
// 解释一次，否则这道门对自由文本的放行会变成又一个"改了没人读"的入口。
func TestDefaultParamDefsEmptyDefaultRatchet(t *testing.T) {
	allowedEmptyDefault := map[string]bool{
		"lead.industry_profile": true,
	}
	seen := map[string]bool{}
	for _, d := range DefaultParamDefs() {
		if d.DefaultValue != "" {
			continue
		}
		name := d.Group + "." + d.Key
		seen[name] = true
		if !allowedEmptyDefault[name] {
			t.Errorf("%s 的默认值为空但不在豁免清单里：要么给一个真有意义的默认值，要么在这里写明为什么空才是对的", name)
		}
	}
	for name := range allowedEmptyDefault {
		if !seen[name] {
			t.Errorf("豁免清单里的 %s 已经不再有空默认值：这条豁免该撤掉了", name)
		}
	}
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
