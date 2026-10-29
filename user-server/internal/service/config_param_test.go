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
	if err := SeedConfigParams(context.Background()); err != nil {
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
	if err := SeedConfigParams(context.Background()); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	svc := NewConfigParamService()
	ctx := context.Background()

	// 样本参数从 bridge.polling_default_timeout 换成 bridge.online_grace_window：
	// 前者已于 2026-10-12 下架（服务端没有长轮询实现，见 seeds 的 DeprecatedReason），
	// 而写入已下架参数会被 ErrParamDeprecated 拒掉——那正是下架护栏在起作用，
	// 但它不该让这条「更新/重置/审计」的基线用例失去样本。默认值仍是 30s，
	// 所以下面两条断言的期望值一字未改。
	if err := svc.UpdateValue(ctx, "bridge", "online_grace_window", "60", 1); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if v := svc.GetDuration(ctx, "bridge", "online_grace_window", 0); v != 60*time.Second {
		t.Errorf("after update = %v, want 60s", v)
	}

	if err := svc.ResetToDefault(ctx, "bridge", "online_grace_window", 1); err != nil {
		t.Fatalf("ResetToDefault: %v", err)
	}
	if v := svc.GetDuration(ctx, "bridge", "online_grace_window", 0); v != 30*time.Second {
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
//
// 2026-10-10（第八次，阶段三 3.1 第二批）：+7 = webhook 组的并发、限流与重试七件套。
//   - 七个常量原先散在 webhook_dedup.go / webhook.go / webhook_ai.go 三处，且本已有
//     一层 env 覆盖（webhookEnvInt）。现在是 env > 参数中心 > 常量兜底，env 保留为
//     部署层应急开关（改 env 要重启，参数中心改完即生效）。
//   - 生效时机分两类，故 Restart 不同：dedup_ttl / rate_limit / rate_burst /
//     max_retries 是每请求读（令牌桶按 key 懒构造，桶用完即丢，下一个 key 就是新值）；
//     worker_count / queue_size / reply_concurrency 在 NewWebhookService 里
//     make(chan, N) 与起 goroutine，只对下次启动生效，三条都标 Restart=true。
//   - max_retries 与 agent_llm.max_retries 同名不同义（那一条管 LLM 调用本身的重试，
//     本条目管渠道出网投递），Description 里写明了区分，免得运维在页面上看混。
//
// 2026-10-10（第九次，阶段三 3.1 第三批）：+6 = sop 组的调度器并发三项 + Saga 补偿三项。
//   - dispatcher_worker_count / dispatcher_queue_capacity / dispatcher_llm_concurrency：
//     DefaultSOPDispatcherConfig() 里原本写死的 16/1000/4。
//   - compensation_max_attempts / compensation_ttl / compensation_max_plans_kept：
//     DefaultCompensationConfig() 里原本写死的 3/30s/512。补偿链路受
//     FF_LTC_SAGA_COMPENSATION 开关控制（默认关闭），开关开启时这些值才真的被读。
//   - 六条全部是构造期读一次，故全标 Restart=true：make(chan,N) 与 goroutine 数
//     在 NewSOPExecutionDispatcher 里定型，改完只对下次启动的进程生效。
//   - 调度器没纳入参数中心的 MaxAttempts/InitialBackoff/MaxBackoff/BackoffMultiplier
//     四项只从字面量提成常量（DefaultSOPDispatcher*），语义不变。
//
// 2026-10-10（第十次，阶段三 3.1 第四批）：+11 = domain_health 4 条 + rag_health 7 条。
//   - domain_health：score_healthy / score_switch_threshold / consecutive_failure_limit
//     / check_timeout。这四条是"看一眼分数就做决定"的类型，改错的结果不是性能问题而是
//     "该切的没切"。除 check_timeout 在 http.Client 构造时定型（Restart=true）外都每请求读。
//   - rag_health：default_window + 六维权重。六个权重按总和归一化后使用——
//     report.Score 是六个 WeightedScore 直接相加（rag_health.go:132-136），不归一化
//     的话运营把某一项调成 3 就会让总分超出 0-100、scoreToGrade 的分档全部错位。
//     归一化后每个旋钮的语义就是干净的"相对比重"。
//   - 六项全读不到或全被写成 0 时回落常量权重：那时 report.Weight 会是 0，
//     任何维度看起来都是 0 分，比百分比失准更难排查。
//
// 2026-10-10（第十一次，阶段三 3.1 第五批）：+22 = feedback 组，反馈学习闭环全部旋钮。
//   - bandit 6：样本量三档（转利用/晋升/后验采样）+ 三个比例（探索下限/流量上限/收敛阈值）。
//   - collector 3：队列容量 / 刷盘间隔 / 批量。信号权重表 DefaultSignalWeights（12 键）
//     刻意不入库——那是一张双射表，拆成 12 条参数后运维要看全貌反而更难。
//   - analyzer 5 / iterator 4 / optimizer 4：销冠分析、Prompt 迭代、自动优化。
//   - 22 条全部构造期读一次，故全标 Restart=true：五个组件的配置在
//     InitFeedbackLoopComponents / InitFeedbackCollector 里组装完就固化。
//   - iterator_negative_reward_threshold 的默认是负数（-0.5），读取侧用 fbOrAnyFloat
//     而不是 fbOrPosFloat：0 的语义是「关掉这道门限」，不是「没配过」。
//
// 2026-10-11（第十二次，阶段 3.3）：+19 = frontend_ws 12 + frontend_ui 7，前端自己的阈值。
//   - 这批的后端没有读取点：常量活在 user-web/src/utils/{agentSocket,chatSocket,
//     journeyTracker}.js 与若干 views/ 里，前端经 GET /api/config/runtime 拉取值。
//     controller/config_param_runtime.go 的 runtimePublicGroups 就是前端的注册表，
//     scripts/check-config-param-readpoints.py 按 group.key 粒度核对该份清单
//     （pagination.page_max_size / page_default_size 在排除清单里，它们后端确实没人读）。
//   - frontend_ui.material_max_size_mb 是前端提示用的副本，与后端
//     misc.upload_max_size_mb（2a，真正生效）必须一致，Description 写明了这个耦合。
//
// 2026-10-11（第十三次，G1 门禁反向核对）：+2 = G1 抓出的两条孤儿读取点。
//   - agent_llm.geo_pipeline_batch_per_run：keyword_pipeline.go:30 一直在读，
//     注释里也写着「可经 agent_llm.geo_pipeline_batch_per_run 调整」，但种子里
//     没有这一条 ⇒ 每次读都 miss、每次回落 fallback，运维在页面上看不到旋钮。
//   - bridge.sse_max_stream_duration：sse.go:71 同理，注释声称「更早就已 DB 驱动」
//     但种子没登记。
//     这正是 1.8 在 channelgw/ws.go 上栽过的形状（key 写错），只是这次压根没登记。
//     单向的「种子 → 读取点」核对抓不到它：读点存在，形状上是绿的。
//
// 2026-10-11（第十四次，G4/G1 补接）：+1 = memory.l1_window_size。
//
//	G4（scripts/check-hardcode-tunable.py）扫命名常量时抓出三个未入库旋钮，
//	逐个核实后：两个是既有的参数中心兜底/语义常量（HTTPIngestMaxMessages 的
//	bridge.ingest_max_messages 早已登记、OpportunityWinProbabilityMax 是 0~1
//	概率的语义上限不该入库），只有 L1WindowSize 是真漏接，本批补上。
//
// 2026-10-11（第十五次，验收逐条过剩下的 14 条未接线）：-3 +5 = 217。
//
//	净变化看着只有 +2，实际是把「参数中心页上有 14 个改了没人读的旋钮」
//	降到了 11 个：
//	  - 下架 wecom.error_rate_degrade。原先标未接线的理由是「成功率判定是写死的
//	    三档，单一阈值表达不了递减分档」。这句话前半句成立、结论不成立：
//	    「表达不了递减分档」的正解是拆成三条独立参数，而不是放弃。
//	    拆成 success_rate_critical/degrade/warn 三条后档位可配、递减关系由
//	    runtimeWeComSuccessRateTiers 用 clamp 强制，运维乱序填值也不会把扣分
//	    方向搞反。附带发现该参数本身就名不副实：它叫「错误率」而代码判定用的是
//	    「成功率」，互补数拿一个当另一个用会得出方向相反的结论。
//	  - 下架 confidence.persona_default_threshold / persona_max_retry。
//	    实现 persona_evaluator.go 已随 9a5f716c 整体删除，这两条永远不会有读取点；
//	    等价能力是 humanize（其四档已在 3.1a 接线）。留着只会在参数中心页上
//	    摆两个永远生效不了的旋钮，诱导后来人去查一个已经删掉的实现。
//
// 2026-10-20（R5-3）：+1 = 240。
//
//	基线已是 239（远端 7bba3ed1 已把 28317b94 的两条——reporting.default_window_days、
//	ratelimit.path_quotas——补进锚点）。本卡在此之上新增 1 条 `urls.public_base_url`：
//	渠道 webhook 回调基址与短链绝对地址此前只有 env PUBLIC_BASE_URL /
//	config.yaml external.public_base_url 两个来源，SaaS 下每个租户各自的回调域名
//	只能改部署文件（等于一租户一份 config）。收进参数中心后由
//	internal/config/server.go 的 GetPublicBaseURLFor 按请求 ctx 读，租户覆盖才真正生效。
//
// 2026-10-22：+1 = 241。新增 `platform.api_host`（平台端 API 地址）：
//
//	出网目标从 env/config.yaml 收进参数中心，改完在管理端生效（60s TTL），
//	不用改部署文件重启。它的默认值留空是刻意的（空 = 没有平台可连，
//	与 PLATFORM_ENABLED 关态语义一致），见下面 EmptyDefaultRatchet 的豁免清单。
//
// D-7（2026-10-23）：chat_ws_ping_period / chat_ws_pong_wait / chat_ws_write_wait
// 三条已下架种子随死代码删除 ⇒ 241 → 238。
// 2026-10-23 content_guard 组新增 max_scan_runes 种子 ⇒ 238 → 239。
// 2026-10-23 reach 组新增 compliance_policy_json 种子 ⇒ 239 → 240。
// 2026-10-24 并行任务新增 bridge.selector_health_degraded_below ⇒ 240 → 241。
// 2026-10-29 S-20（007 §3.6 余量）新增 telemetry.dashboard_default_window_hours +
// telemetry.channel_online_degraded_below ⇒ 241 → 243。
const defaultParamDefsWant = 243

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
// 新增一格就得在这里解释一次，否则这道门对自由文本的放行会变成又一个"改了没人读"的入口。
//
// 2026-10-10：清单现在是**空的**。曾经里面挂着 lead.industry_profile（"一段行业自由描述，
// 空 = 未填行业画像"），但 PR#40 顺手给它填了一个默认值（"通用零售/服务咨询：…"），
// 于是这条豁免变成了谎话——而本用例另一半断言"豁免清单里的每一条现在必须真的还是空默认"
// 当场就红了。这正是 ratchet 该干的事：豁免面只允许被**主动撤掉**，不允许悄悄失效。
//
// 保留这份清单而不是删掉整个用例：上面那个 for 还在负责"出现新的空默认就报警"，
// 删掉这道门等于把"空默认"这件事重新变成没人看管的入口。
//
// 2026-10-20（R5-3）：清单重新有一条 —— `urls.public_base_url`。它与被撤掉的
// lead.industry_profile 方向相反：那条是"豁免写着空才是对的，结果有人给它填了值"，
// 这条是"填值才是例外"。公网基址的正确来源本来就是部署侧
// （env PUBLIC_BASE_URL / config.yaml external.public_base_url），参数中心这一格
// 只在"这个租户要用自己的回调域"时才有值；给它任何默认域名都会让参数行反过来
// 压过部署配置，所以空是设计意图，不是漏填。
//
// 2026-10-22：再加一条 `platform.api_host`。理由同上但方向更硬 ——
// 它是**平台级**参数（这台部署连哪个平台是部署级事实，租户写一律拒绝），
// 空值的含义是"没有平台可连"，与 PLATFORM_ENABLED 关态一致；回落一个具体
// 域名只会让请求打向黑洞、把日志刷成 Error，换不来任何功能。
func TestDefaultParamDefsEmptyDefaultRatchet(t *testing.T) {
	allowedEmptyDefault := map[string]bool{
		"urls.public_base_url": true,
		"platform.api_host":    true,
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
// `UNIQUE (param_group, key)`，模型标签（Group priority:1 / Key priority:2）与之一致，
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
