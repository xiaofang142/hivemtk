package service

import (
	"context"
	"sync"
	"time"

	"hivemtk-user/internal/event"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
)

// ============================================================
// 运行时模块闸（FR-5.6）：定时任务 / Agent 工具 / EventBus 订阅
// 在**派发时刻**按模块开通状态动态过滤（不是装配期静态接线）。
//
// ============================================================
// 与路由闸（module_catalog.go IsEnabled）的三条根本差别
//
// ① 判定主体不同：路由闸判「**当前请求的这个租户**开通没有」（per-tenant）；
//    这里判「**整个实例上有没有任何一个租户**开通了该模块」（instance 级）。
//    理由：定时任务与事件订阅没有"请求方租户"这一说 —— 进程级循环为一个
//    租户跑，也为所有租户跑；只要还有租户在用，就该跑。
//
// ② 错误语义相反，**这是刻意的，勿"统一"**：
//      路由闸   → fail-**closed**：DB 抖动 → 拦（宁可误伤一次请求，也不能
//                 让未付费流量穿闸 —— 穿闸 = 商业模式静默失效）。
//      本闸     → fail-**open**：DB 抖动 → 放行 + 限频日志一次。
//    理由：后台任务被瞬时 DB 错误停掉的代价远高于多跑一轮 —— RFM 每日重算、
//    会话 TTL 清扫、GEO 探针漏一轮就是一天的数据空洞，且没人会立刻发现；
//    反过来多跑一轮的代价只是浪费一点计算。判错一次可恢复，漏跑一天不可恢复。
//    （route gating 见 middleware/module_gate.go 与 module_catalog.go ① ——
//     那边的 fail-closed 注释同样在，两边不许互相"抄"。）
//
// ③ 未映射 = 放行：路由闸的"未标注"形态是 ③（放行 + 记 WARN 进采集清单）；
//    这里的对应物是 workerModuleKey 查不到 → 直接放行。宁可漏闸一个后台任务，
//    也不能凭猜测把合规/运维任务闸死（见下表的 NOT mapped 清单）。
//
// ============================================================
// workerModuleKey：worker 名 → 模块 key（**只收高置信条目**）
//
// 新增条目前三问：
//   1. 这个 worker 停跑一天，业务会不会出现不可恢复的空洞？
//   2. 它的归属模块有没有歧义（一个 cron 横跨两个模块就不许进表）？
//   3. 它是不是合规/运维刚需（是 → 永远不许进表）？
//
// **明确不映射（fail-open-on-unknown，永不闸）**：
//   - feedback_learning / feedback_loop / bandit_reward_reflux：
//       学习回路归属有歧义（ai-sales / experiment 都沾边），闸错方向会
//       静默打断模型迭代，没人看得出来；
//   - sop_scheduler：SOP 扫描与执行链路横跨 customer/inbox/ai-sales，
//       归属不唯一 —— 调用点已挂 AllowWorker（sop_scheduler），未来要闸
//       只需在本表加一行，调用点不用动；
//   - alert dispatcher（alert_checker / alert_dispatcher）：运维告警通道，
//       闸掉 = 出事时没人被通知，**永不闸**；
//   - message_trace_cleanup：消息 trace 打码/清理是**合规**职责，
//       闸掉 = 合规窗口内留下未清理的明文，**永不闸**。
// ============================================================

// workerModuleKey worker 名 → 模块 key。
//
// key 是 worker 名（cron 任务名 / 循环名），不是模块 key：调用点写
// AllowWorker(ctx, "customer_rfm") 这样的业务名，未来映射变化只改这张表。
var workerModuleKey = map[string]string{
	// GEO 定时任务：// 用 system scope 读 cfgRepo.Get（First）——多租户下读到的是**跨租户 SetupGeoJobs 注册的
	// 全部任务名（sov_refresh / negative_monitor / source_sync /
	// crawler_monitor / keyword_pipeline），由 pkg/cron.InitCron 把
	// service.AllowWorker 注入为 geoservice 的任务闸。
	"sov_refresh":      model.ModuleGEO,
	"negative_monitor": model.ModuleGEO,
	"source_sync":      model.ModuleGEO,
	"crawler_monitor":  model.ModuleGEO,
	"keyword_pipeline": model.ModuleGEO,
	"citation_drift":   model.ModuleGEO,
	"site_audit":       model.ModuleGEO,

	// 客户中心 cron
	"customer_rfm":     model.ModuleCustomer, // customer_rfm_cron.go 每日 RFM 全量重算
	"customer_session": model.ModuleCustomer, // customer_session_cron.go 会话 TTL 清扫

	// 知识库 cron
	"rag_eval": model.ModuleKnowledge, // rag_eval_cron.go 每日 RAG 自动评测

	// AI 对话 cron
	"dialogue_eval": model.ModuleKnowledge, // dialogue_eval_cron.go 每日对话级 gold 评测（知识库/会话域）
}

// toolModuleKey Agent 工具名 → 模块 key（**保守**：只收归属毫无疑问的工具，
// 查不到 = 照常展示给 LLM）。
//
// 逐条核对过注册名（tooluse/browser_tools.go、AGENT_TOOLS.md · reach(20)）：
//   - browser_*：只有这三个浏览器自动化工具，全部归 browser-automation；
//   - reach.wecom.send：企业微信出站，归 im-wecom。
//
// 刻意**不**映射的：
//   - GEO：全仓没有注册任何 GEO 的 Agent 工具（GEO 能力全在 controller 层），
//     所以 ModuleGEO 在这张表里没有条目 —— 不是漏了；
//   - reach.weixin.send（个人微信）≠ 企业微信，不许挂到 im-wecom；
//   - reach.telegram/whatsapp/feishu/dingtalk.send 属 im-intl，但 spec 未授权
//     该映射，按"未映射 = 展示"保守处理；
//   - 其余 reach / customer / knowledge / business 工具归属有多个候选或属
//     基础能力，一律不映射。
var toolModuleKey = map[string]string{
	"browser_open_task":   model.ModuleBrowserAutomation,
	"browser_task_status": model.ModuleBrowserAutomation,
	"browser_task_list":   model.ModuleBrowserAutomation,
	"reach.wecom.send":    model.ModuleIMWecom,
}

// resolveWorkerModuleKey 把「worker 名或模块 key」统一解析成模块 key。
//
//   - "" → ""（未标注 = 放行）
//   - 命中 workerModuleKey → 映射后的模块 key
//   - 本身就是一个合法模块 key（24 模块目录内）→ 原样返回
//   - 其余（未映射的 worker 名）→ "" = 放行（fail-open-on-unknown）
//
// 依赖 model.OwnerModuleCategory 的"未知返回空串"语义区分 worker 名与
// 模块 key：worker 名（sov_refresh、customer_rfm…）都不在模块目录里，
// 模块 key（geo、customer…）都在，两者不会互相误判。
func resolveWorkerModuleKey(nameOrKey string) string {
	if nameOrKey == "" {
		return ""
	}
	if k, ok := workerModuleKey[nameOrKey]; ok {
		return k
	}
	if model.OwnerModuleCategory(nameOrKey) != "" {
		return nameOrKey
	}
	return ""
}

// ---------------------------------------------------------- fail-open 日志

var (
	gateSkipLogMu   sync.Mutex
	gateSkipLogSeen = map[string]time.Time{}
)

// gateSkipLogInterval 同一 key 的跳过/降级日志的最小间隔。
//
// 为什么不是"只记一次"：限频窗口能让**新的**故障在 5 分钟后重新露头，
// 而永久 once 会让第一次抖动之后的所有故障静默 —— 那等于把 fail-open
// 变成 fail-silent。cron 每分钟一跳，5 分钟一条已经足够稀。
const gateSkipLogInterval = 5 * time.Minute

// logGateSkipOnce 限频日志（同一 key 每 gateSkipLogInterval 最多记一条）。
func logGateSkipOnce(tag, key, format string, args ...any) {
	gateSkipLogMu.Lock()
	defer gateSkipLogMu.Unlock()
	if t, ok := gateSkipLogSeen[tag+"|"+key]; ok && time.Since(t) < gateSkipLogInterval {
		return
	}
	gateSkipLogSeen[tag+"|"+key] = time.Now()
	logger.Warnf(format, args...)
}

// ---------------------------------------------------------- 任意租户判定

// AnyTenantEnabled 模块在**本实例上是否对任一租户可用**（instance 级判定）。
//
// 判定顺序（并集，任一为真即放行）：
//
//	① 进程内 per-tenant 缓存：最近活跃租户的状态快照（module_catalog 的
//	   gateTenantState），命中 true 直接返回，不打 DB —— 关停刚生效时有
//	   最多 gateCacheTTL 的窗口，与路由闸的缓存窗口同源同宽；
//	② DB：tenant_modules 里 **跨租户 union**（SystemScope 读，隔离 callback
//	   对 SystemScope 直通 = 不带 WHERE tenant_id）的可用行计数。
//	   行语义复用 model.TenantModuleUsable（status=active 且未过期），
//	   由 repository.UsableCount 落成索引 COUNT —— 勿在 SQL 与
//	   TenantModuleUsable 两侧各写一份判据而不互相对账。
//	③ DB 计数为 0 时补一次全量读区分两种空：
//	   - 表里**一行都没有**（首启 / 从未同步）→ 与 IsEnabled ① 同口径，
//	     只放行基础模块（这正是"OFF 模式单租户下与 IsEnabled 逐 key 等价"
//	     的那一半；bootstrapOffline 落地后基础 key 恒为 true）；
//	   - 表里有行但该模块无可用行（未开通/已撤销/已过期）→ false。
//
// **任何错误 → true（fail-open）+ 限频日志**（文件头教义 ②：后台任务被
// 瞬时 DB 错误停掉比多跑一轮贵得多；这与路由闸的 fail-closed 是反的，
// 是刻意的 —— 见文件头 ②）。
func (s *ModuleGateService) AnyTenantEnabled(ctx context.Context, moduleKey string) bool {
	if moduleKey == "" {
		return true // 未映射/未标注：放行（教义 ③）
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// ① 进程内缓存并集（快路径）
	if s.anyCachedTenantEnabled(moduleKey) {
		return true
	}

	// ② 跨租户 union 计数。SystemScope 必挂：on 模式下裸 ctx（后台循环的
	//    Background）会被隔离 callback fail-closed —— 那会让本闸在 on 模式
	//    永远报错 → 永远 fail-open，等于没有闸。SystemScope 让
	//    scopeQueryForTenant 直通，读到的是全租户并集。
	repo := s.modules()
	if repo == nil {
		logGateSkipOnce("anytenant", moduleKey,
			"[module-runtime-gate] 模块 %s 判定失败：仓储未注入，按 fail-open 放行（与路由闸 fail-closed 相反，见 module_runtime_filter.go 文件头②）", moduleKey)
		return true
	}
	sysCtx := tenant.WithSystemScope(ctx)
	now := time.Now()
	n, err := repo.UsableCount(sysCtx, moduleKey, now)
	if err != nil {
		logGateSkipOnce("anytenant", moduleKey,
			"[module-runtime-gate] 模块 %s 可用行统计失败: %v → 按 fail-open 放行（后台任务不停摆；与路由闸 fail-closed 相反，见文件头②）", moduleKey, err)
		return true
	}
	if n > 0 {
		return true
	}

	// ③ 计数为 0：区分「整表空」与「有行但不可用」
	rows, err := repo.ListAll(sysCtx)
	if err != nil {
		logGateSkipOnce("anytenant", moduleKey,
			"[module-runtime-gate] 模块 %s 空态判读失败: %v → 按 fail-open 放行", moduleKey, err)
		return true
	}
	if len(rows) == 0 {
		// 整表空：与 IsEnabled ① 单口径（基础模块=免费常开，收费模块拦）
		return isBaseModuleKey(moduleKey)
	}
	return false
}

// anyCachedTenantEnabled 进程内 per-tenant 缓存的并集读。
//
// 只读、不刷新：刷新是 IsEnabled 的职责（per-tenant + TTL）。这里最多
// 沿用 gateCacheTTL 内的陈旧快照 —— 与路由闸共用同一 TTL，两边的
// "开通/关停生效延迟"因此是同一个数。
func (s *ModuleGateService) anyCachedTenantEnabled(moduleKey string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, st := range s.tenants {
		if st == nil || st.m == nil {
			continue
		}
		if st.m[moduleKey] {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------- 三个派发点入口

// AllowWorker 定时任务/事件循环的模块闸入口。
//
// nameOrKey 既接受 worker 名（workerModuleKey 里的 cron/循环名），也直接
// 接受模块 key；解析规则见 resolveWorkerModuleKey。"" 或未映射 → 恒 true。
//
// ctx 语义：worker 检查是**进程级**的，调用方传 context.Background() 即可
// （gateTenantOf 对裸 Background 落 DefaultTenantID，而本判定是 DB 级
// union，不看 ctx 里的租户）。请求上下文（工具过滤）传真实 ctx 也正确。
func AllowWorker(ctx context.Context, nameOrKey string) bool {
	key := resolveWorkerModuleKey(nameOrKey)
	if key == "" {
		return true
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return NewModuleGateService().AnyTenantEnabled(ctx, key)
}

// WrapSubscriber EventBus 订阅包装：模块关闭时跳过 handler（限频日志）。
//
// moduleKey 传 "" = 纯结构透传（恒放行）—— InitEventBus 用它把订阅点
// 先挂上闸，未来有可归属的 topic 只需把 "" 换成模块 key。
//
// 返回 nil 表示"消费成功"：事件总线把 handler 返回的 error 记为投递失败，
// 而"模块关了所以不处理"不是失败，不能让它污染投递统计。
func WrapSubscriber(moduleKey string, h event.Handler) event.Handler {
	if h == nil {
		return nil
	}
	return func(evt event.Event) error {
		if !AllowWorker(context.Background(), moduleKey) {
			logGateSkipOnce("subscriber", moduleKey,
				"[module-runtime-gate] 事件 %s 被跳过：模块 %s 无任何租户开通（限频日志，每 %s 至多一条）",
				evt.Topic, moduleKey, gateSkipLogInterval)
			return nil
		}
		return h(evt)
	}
}

// FilterToolsByKey 按**单个**模块 key 过滤工具清单（该清单整体属于该模块时用）。
//
// moduleKey 为空 → 原样返回（no-op）。模块对任一租户可用 → 全保留；
// 否则整份清单丢弃（返回 nil）。逐工具混合清单请用 FilterToolsByModule。
func FilterToolsByKey(ctx context.Context, moduleKey string, tools []AgentToolDef) []AgentToolDef {
	if moduleKey == "" || len(tools) == 0 {
		return tools
	}
	if !AllowWorker(ctx, moduleKey) {
		logGateSkipOnce("tools", moduleKey,
			"[module-runtime-gate] 工具清单按模块 %s 整体裁剪：无任何租户开通", moduleKey)
		return nil
	}
	return tools
}

// FilterToolsByModule 按 toolModuleKey **逐工具**过滤（会话工具清单装配点用）。
//
// 口径：
//   - 未映射的工具 → 照常展示（保守：漏展示一个工具比多展示一个更难发现）；
//   - 同一模块的多个工具只判定一次（每请求每模块至多一次 DB 计数）；
//   - 判定吃 AllowWorker 同一条 fail-open 语义：DB 抖动 → 工具照常展示。
func FilterToolsByModule(ctx context.Context, tools []AgentToolDef) []AgentToolDef {
	if len(tools) == 0 {
		return tools
	}
	enabled := make(map[string]bool, 4)
	out := make([]AgentToolDef, 0, len(tools))
	dropped := false
	for _, t := range tools {
		mk, ok := toolModuleKey[t.Name]
		if !ok {
			out = append(out, t)
			continue
		}
		on, seen := enabled[mk]
		if !seen {
			on = AllowWorker(ctx, mk)
			enabled[mk] = on
			if !on {
				logGateSkipOnce("tools", mk,
					"[module-runtime-gate] 模块 %s 工具对本请求隐藏：无任何租户开通（限频日志）", mk)
			}
		}
		if on {
			out = append(out, t)
		} else {
			dropped = true
		}
	}
	if !dropped {
		return tools // 零裁剪：原样返回，避免下游拿到换过底层数组的切片
	}
	return out
}
