package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
)

// G-3 工具后果分级接线（新规划任务清单 T-P3-05）
//
// tooluse 侧此前只有一个**死掉的**分级词汇：`feedback_decorator.go` 里的
// ToolRiskLevel(read|write|admin) 有 3 个工具实现，而唯一读者 ToolCallEvent.RiskLevel
// 在 FeedbackCollectorDecorator 里从未被赋值 ⇒ 元数据写进 DB 后没有任何消费方
// （app/feedback_sink_adapter.go 只在非空时写 metadata）。那是一副"看着有、其实不通"
// 的假配置，与本处要修的现象同源：`WhitelistPermissionChecker.defaultAllow=true`
// 让"白名单"名义上是授权、实现上是默认放行。
//
// 因此本处先把词汇迁到 readonly|low_write|high_write（判据=副作用落在谁的系统、能否撤回），
// 把 45 个生产工具的声明补齐，再挂一个**只判定不拦截**的观察层。
//
// 两态而非三态（与熔断/审批门的刻意差别）：
//
//	off（默认） 不挂判定层，装饰链与接线前一致
//	shadow      每次调用算一次判定并留痕；**代码里不存在拒绝路径**
//
// 三态（T-P9-01 起，与熔断/审批门对齐）：
//
//	off（默认） 不挂判定层，装饰链与接线前一致
//	shadow      每次调用算一次判定并留痕；**代码里不存在拒绝路径**
//	block       would_deny 变成真拦截（RiskGateBlockDecorator）：高危且未逐条授权的调用被拒，
//	            被拒记 tool_risk_denied（error 级）+ tool_risk_decision allowed=false；
//	            回滚 = 旗子置回 off/shadow，不动代码。
//
// 历史注记：T-P3-05 时代本处没有阻断态，`=enforce|block|true` 一律按 shadow 挂载并告警
// "这个构建里它拦不住任何东西"。P9 把阻断接上之后，这段告警逻辑已改为真进 block；
// 报告的 blocks_when_denied 随旗子走（block 态 true），"当前有没有真拦"以它为准。

const (
	// RiskGateFlagEnv 挂不挂判定层。名字里的 enforce 是卡面指定的（P9 会把它接成真拦截），
	// 今天它的合法值只有 off|shadow，其余真值形态一律降级为 shadow 并告警。
	RiskGateFlagEnv = "FF_TOOL_PERMISSION_ENFORCE"
	// riskRetainedEnv 保留窗内判定条数上限。计数不受它影响（见 MemoryRiskObserver）。
	riskRetainedEnv     = "TOOL_RISK_OBSERVED_RETAINED"
	riskRetainedDefault = 2000
)

type riskGateMode string

const (
	riskGateOff    riskGateMode = "off"
	riskGateShadow riskGateMode = "shadow"
	// riskGateBlock 阻断：would_deny 变成真拦截。回滚 = 把旗子置回 off/shadow。
	riskGateBlock riskGateMode = "block"
)

// 装配期写一次、HTTP 读，无锁前提与 circuitStateRef / approvalModeValue 相同。
var (
	riskModeValue   = string(riskGateOff)
	riskObserverRef *tooluse.MemoryRiskObserver
)

// parseRiskGateMode 解析旗子值。
//
// 认不出的值判 off（不挂链、不留痕）：挂一层没有 observer 消费的门只会白烧判定开销。
// 但**阻断语气的值**（enforce/block/active/true/1/on/yes）判 shadow 并告警，
// 不判 off —— 写出这些值的人显然想要拦截，静默不挂链等于把他的意图丢掉。
func parseRiskGateMode(raw string) riskGateMode {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "":
		return riskGateOff
	case "off", "false", "0", "no", "n", "none", "disabled":
		return riskGateOff
	case "shadow", "observe", "watch", "log", "report":
		return riskGateShadow
	case "enforce", "block", "active", "on", "yes", "y", "true", "1":
		logger.Warnf("[tool-risk] ⚠️ %s=%q 是阻断语气 ⇒ 按 block 挂载：高危且未逐条授权的调用会被真拦（T-P9-01）。"+
			"开之前先看 /api/agent/tools/risk 的 would_deny；一键回滚 = 把旗子置回 off/shadow。可用值：off|shadow|block",
			RiskGateFlagEnv, raw)
		return riskGateBlock
	}
	logger.Warnf("[tool-risk] %s=%q 无法识别 ⇒ 按 off 处理（分级判定层不挂链）；可用值：off|shadow|block", RiskGateFlagEnv, raw)
	return riskGateOff
}

// riskSink 在内存计数之外补一条结构化日志。
//
// 只给 would_deny 的那批打日志：非高危调用占绝大多数，逐条打等于把日志刷满噪音，
// 而累计量由 MemoryRiskObserver 负责，不靠日志行数。
type riskSink struct{ obs *tooluse.MemoryRiskObserver }

func (s riskSink) Observe(ctx context.Context, d tooluse.RiskDecision) {
	s.obs.Observe(ctx, d)
	if !d.WouldDeny {
		return
	}
	logger.Ctx(ctx).Info().
		Str("event", "tool_risk_decision").
		Str("mode", riskModeValue).
		Str("tool_name", d.ToolName).
		Str("risk_level", string(d.Level)).
		Bool("risk_declared", d.Declared).
		Str("agent_id", d.AgentID).
		Str("caller_id", d.CallerID).
		Bool("allowed", d.Allowed).
		Bool("would_deny", true).
		Bool("agent_wildcard", d.AgentWildcard).
		Str("reason", d.Reason).
		Msg("工具后果分级判定：若转阻断会被拦下的调用")
}

// applyRiskGate 按旗子把分级判定挂进 executor 配置。
//
// 调用方：InitGlobalToolExecutor（NewToolExecutor 之前）。off 时显式清空两个字段 +
// 把包内引用归零，使"关旗 = 与接线前完全一致"成为可断言的性质。
//
// grants 取全局 WhitelistPermissionChecker：判定只读它的 agentWhitelist 一张表
// （见 tooluse.AgentGrantReader 的注释），不借它的 Check —— 后者有全局白名单、
// "*" 与 defaultAllow 三级兜底，会把"没人拦"读成"被授权"。
func applyRiskGate(config *tooluse.ToolExecutorConfig) riskGateMode {
	mode := parseRiskGateMode(os.Getenv(RiskGateFlagEnv))
	riskModeValue = string(mode)
	if mode == riskGateOff {
		config.RiskGrants = nil
		config.RiskObserver = nil
		config.RiskEnforce = false
		riskObserverRef = nil
		return mode
	}

	retained := riskRetainedDefault
	if n, ok := envInt("[tool-risk]", riskRetainedEnv, 10, 100000); ok {
		retained = n
	}
	obs := tooluse.NewMemoryRiskObserver(retained)
	grants := tooluse.AgentGrantReader(GetGlobalPermissionChecker())

	config.RiskGrants = grants
	config.RiskObserver = riskSink{obs: obs}
	config.RiskEnforce = mode == riskGateBlock
	riskObserverRef = obs

	if mode == riskGateBlock {
		logger.Infof("[tool-risk] 🛑 工具后果分级阻断已接线（保留窗 %d 条判定）：高危且未逐条授权的调用会被真拦，"+
			"被拒记 tool_risk_denied（error 级）+ tool_risk_decision allowed=false。一键回滚 = 旗子置回 off/shadow。", retained)
	} else {
		logger.Infof("[tool-risk] ✅ 工具后果分级判定已接线，模式=%s（保留窗 %d 条判定，累计计数不受上限影响；"+
			"本层无拒绝路径 ⇒ 放行结果与接线前一致）", mode, retained)
	}
	// 这里不写"高危有几个、门内有几个"：那组数字随工具集合漂，抄进日志就会变成第二处事实源。
	// 启动日志只指路，数字由 /api/agent/tools/risk 每次实算。
	logger.Infof("[tool-risk] 观察计数的落库状态见同一次启动里 startToolRiskStatsPersistence 的日志行；" +
		"转阻断前看 /api/agent/tools/risk 的 would_deny 与 observations_persisted（后者=false 时不能跨重启对比）")
	return mode
}

// ---- D-2：would_deny 统计落库 ------------------------------------------------
//
// 观察计数原本只活在 MemoryRiskObserver 里，重启归零 —— 而"切 default-deny 前先攒
// 一轮生产流量"要求的恰恰是跨重启的累计。这里做两件事：启动期 load、运行期周期 flush。
//
// 存储复用 platform_config_kv（实例级 KV，已有表、已进 AutoMigrate），不新建表：
//   - 语义匹配：工具风险统计是全实例共享的运营计数，不属于任何租户；
//   - 迁移目录正被并行会话修改，新建表会引入编号冲突风险，而现成表零迁移成本。
//
// 写入是**绝对累计值**（内存 = 启动读回的历史 + 本进程新增），不是增量：
// 增量语义下一次 load 就要和库里做加法，一旦某次 flush 重试会重复累加；
// 绝对值语义下重复写是幂等的。代价是**多副本不聚合**：同一 key 最后写入者覆盖，
// 因此该读数只保证"单实例跨重启"，不是集群总和；多副本部署需要另做分实例 key 或合并。
//
// 不接 DB 的情形（句柄缺失/读回失败/JSON 损坏）一律**关闭本轮落库**而不是带着空
// 计数去写：那会把上一轮历史覆盖成一晚上的新数据，比"没落库"更难发现。

const (
	// ToolRiskStatsFlushEnv flush 周期。默认 30s：工具调用频率远高于此，
	// 但统计是运营证据、不是计费，崩溃时最多丢一个周期。
	ToolRiskStatsFlushEnv = "TOOL_RISK_STATS_FLUSH_INTERVAL"
	// ToolRiskStatsKVKey platform_config_kv 里的键名（key 全局唯一，实例一份）。
	ToolRiskStatsKVKey = "agent_tool_risk_stats_v1"

	toolRiskStatsFlushDefault = 30 * time.Second
	toolRiskStatsFlushMin     = time.Second
	toolRiskStatsFlushMax     = time.Hour
	toolRiskStatsVersion      = 1
	toolRiskStatsWriteTimeout = 10 * time.Second
)

// toolRiskStatStore 是持久化面需要的最小 KV 能力，repository.PlatformConfigKVRepository
// 直接满足。抽成接口是为了测试能用内存假仓储，不依赖 PG。
type toolRiskStatStore interface {
	Available() bool
	Get(ctx context.Context, key string) (string, error)
	Upsert(ctx context.Context, key, value string) (string, error)
}

// toolRiskStatsPayload 是落库的 JSON 形状。rows 即"最小表结构"的 KV 版：
// tool_name, decision, reason, count, last_seen_at（另带 level/declared/wildcard 以还原报告）。
type toolRiskStatsPayload struct {
	Version   int                   `json:"version"`
	UpdatedAt time.Time             `json:"updated_at"`
	Rows      []tooluse.RiskStatRow `json:"rows"`
}

var (
	toolRiskStatsMu        sync.Mutex
	toolRiskStatsCancel    context.CancelFunc
	toolRiskStatsDone      chan struct{}
	toolRiskStatsObserver  *tooluse.MemoryRiskObserver
	toolRiskStatsStoreRef  toolRiskStatStore
	toolRiskStatsActive    bool
	toolRiskStatsRestored  int
	toolRiskStatsLastFlush time.Time
	toolRiskStatsLastErr   string
)

// startToolRiskStatsPersistence 启动期读回历史 + 起周期 flush。由 InitGlobalToolExecutor
// 在 applyRiskGate 之后调用（observer 由那里创建；mode=off 时传 nil，直接返回）。
//
// 返回是否接上。未接上不影响判定与报告，只是 observations_persisted=false。
func startToolRiskStatsPersistence(obs *tooluse.MemoryRiskObserver, store toolRiskStatStore, interval time.Duration) bool {
	stopToolRiskStatsLoop()
	if obs == nil {
		clearToolRiskStatsState()
		return false
	}
	if store == nil || !store.Available() {
		clearToolRiskStatsState()
		logger.Warnf("[tool-risk] ⚠️ would_deny 统计未接落库（DB 句柄不可用）：计数只在进程内存里，重启归零；" +
			"切 default-deny 前需要 /api/agent/tools/risk 的 observations_persisted=true")
		return false
	}
	if interval <= 0 {
		interval = toolRiskStatsFlushDefault
	}

	loadCtx, cancelLoad := context.WithTimeout(context.Background(), toolRiskStatsWriteTimeout)
	restored, err := loadToolRiskStats(loadCtx, obs, store)
	cancelLoad()
	if err != nil {
		// 读失败时**不写**：本进程看到的是空计数，一旦 flush 就会覆盖库里的历史。
		// 宁可这一轮不落库，也不要制造"统计从今天重新开始"的假象。
		clearToolRiskStatsState()
		logger.Warnf("[tool-risk] ⚠️ 启动期读回 would_deny 统计失败 ⇒ 本轮不落库（避免覆盖历史），计数只在内存：%v", err)
		return false
	}

	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	toolRiskStatsMu.Lock()
	toolRiskStatsObserver = obs
	toolRiskStatsStoreRef = store
	toolRiskStatsActive = true
	toolRiskStatsRestored = restored
	toolRiskStatsCancel = cancel
	toolRiskStatsDone = done
	toolRiskStatsLastErr = ""
	toolRiskStatsMu.Unlock()

	// 长驻 flush 循环：utils.SafeGo 收口 panic，防统计落库链 panic 击穿进程
	utils.SafeGo(runCtx, "toolrisk.statsFlush", func(c context.Context) {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				flushCtx, flushCancel := context.WithTimeout(c, toolRiskStatsWriteTimeout)
				err := flushToolRiskStats(flushCtx, obs, store)
				flushCancel()
				recordToolRiskStatsFlush(err)
				if err != nil {
					logger.Warnf("[tool-risk] ⚠️ would_deny 统计 flush 失败（下次重试，内存计数不清零）：%v", err)
				}
			}
		}
	})

	logger.Infof("[tool-risk] ✅ would_deny 统计已接落库：key=%s 周期=%s 启动读回 %d 条计数（重启不再归零）",
		ToolRiskStatsKVKey, interval, restored)
	return true
}

// toolRiskStatsFlushInterval 解析 flush 周期。写错（如 "30" 而不是 "30s"）回落默认并告警，
// 与仓内其它时长旗子同口径。
func toolRiskStatsFlushInterval() time.Duration {
	raw := strings.TrimSpace(os.Getenv(ToolRiskStatsFlushEnv))
	if raw == "" {
		return toolRiskStatsFlushDefault
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < toolRiskStatsFlushMin || d > toolRiskStatsFlushMax {
		logger.Warnf("[tool-risk] %s=%q 不是合法时长（如 30s/5m，允许 [%s,%s]）⇒ 沿用默认 %s",
			ToolRiskStatsFlushEnv, raw, toolRiskStatsFlushMin, toolRiskStatsFlushMax, toolRiskStatsFlushDefault)
		return toolRiskStatsFlushDefault
	}
	return d
}

// loadToolRiskStats 从 KV 读回累计并合并进 obs。空值（没落过库）返回 0, nil。
func loadToolRiskStats(ctx context.Context, obs *tooluse.MemoryRiskObserver, store toolRiskStatStore) (int, error) {
	if obs == nil || store == nil || !store.Available() {
		return 0, nil
	}
	raw, err := store.Get(ctx, ToolRiskStatsKVKey)
	if err != nil {
		return 0, err
	}
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	var payload toolRiskStatsPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return 0, fmt.Errorf("解析 %s 失败: %w", ToolRiskStatsKVKey, err)
	}
	obs.RestoreStats(payload.Rows)
	return len(payload.Rows), nil
}

// flushToolRiskStats 把当前累计写成绝对快照。空计数不写：省一次无意义 upsert，
// 也避免"启动后还没流量就把库里的历史刷成空"。
func flushToolRiskStats(ctx context.Context, obs *tooluse.MemoryRiskObserver, store toolRiskStatStore) error {
	if obs == nil || store == nil || !store.Available() {
		return nil
	}
	rows := obs.StatRows()
	if len(rows) == 0 {
		return nil
	}
	payload := toolRiskStatsPayload{Version: toolRiskStatsVersion, UpdatedAt: time.Now(), Rows: rows}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = store.Upsert(ctx, ToolRiskStatsKVKey, string(raw))
	return err
}

func recordToolRiskStatsFlush(err error) {
	toolRiskStatsMu.Lock()
	defer toolRiskStatsMu.Unlock()
	if err == nil {
		toolRiskStatsLastFlush = time.Now()
		toolRiskStatsLastErr = ""
		return
	}
	toolRiskStatsLastErr = err.Error()
}

func stopToolRiskStatsLoop() {
	toolRiskStatsMu.Lock()
	cancel, done := toolRiskStatsCancel, toolRiskStatsDone
	toolRiskStatsCancel, toolRiskStatsDone = nil, nil
	toolRiskStatsActive = false
	toolRiskStatsMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

// clearToolRiskStatsState 清掉 observer/store 引用（保留 lastFlush/lastErr 供诊断）。
//
// 启动失败时必须清：否则 Close 会把上一轮的 observer 当成本轮的再 flush 一次，
// 而它已经没有周期任务了 —— 一次"失败启动 + 退出"就会写出两份语义不明的快照。
func clearToolRiskStatsState() {
	toolRiskStatsMu.Lock()
	defer toolRiskStatsMu.Unlock()
	toolRiskStatsObserver = nil
	toolRiskStatsStoreRef = nil
	toolRiskStatsRestored = 0
}

// FlushToolRiskStats 立即落一次（测试与运维手工触发用）。未接线时 no-op。
func FlushToolRiskStats(ctx context.Context) error {
	toolRiskStatsMu.Lock()
	obs, store, active := toolRiskStatsObserver, toolRiskStatsStoreRef, toolRiskStatsActive
	toolRiskStatsMu.Unlock()
	if !active {
		return nil
	}
	err := flushToolRiskStats(ctx, obs, store)
	recordToolRiskStatsFlush(err)
	return err
}

// CloseToolRiskStatsPersistence 停掉周期 flush 并做最后一次落库。
//
// 当前没有挂到 main 的 defer 上（只提供入口）：周期默认 30s，正常退出最多丢一个周期；
// 需要零丢失的部署应在进程退出路径调用它（与 CloseToolAuditPersistence 同形）。
func CloseToolRiskStatsPersistence() {
	stopToolRiskStatsLoop()
	toolRiskStatsMu.Lock()
	obs, store := toolRiskStatsObserver, toolRiskStatsStoreRef
	toolRiskStatsMu.Unlock()
	clearToolRiskStatsState()
	if obs == nil || store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), toolRiskStatsWriteTimeout)
	defer cancel()
	if err := flushToolRiskStats(ctx, obs, store); err != nil {
		logger.Warnf("[tool-risk] ⚠️ 退出前最后一次 would_deny 统计落库失败：%v", err)
	}
}

// ToolRiskStatsPersisted 报告本轮进程的观察计数是否已接持久化。
// 报告的 observations_persisted 直接取这个值。
func ToolRiskStatsPersisted() bool {
	toolRiskStatsMu.Lock()
	defer toolRiskStatsMu.Unlock()
	return toolRiskStatsActive
}

// ToolRiskStatsSnapshot 是落库接线的诊断快照（运维端点可后续挂载，不改变任何行为）。
type ToolRiskStatsSnapshot struct {
	Persisted    bool      `json:"persisted"`
	StoreKey     string    `json:"store_key"`
	RestoredRows int       `json:"restored_rows"`
	LastFlushAt  time.Time `json:"last_flush_at,omitempty"`
	LastError    string    `json:"last_error,omitempty"`
}

// GetToolRiskStatsSnapshot 返回当前落库状态。Persisted=false 时报告里的 counts
// 只代表本进程，切 default-deny 前不能拿它当跨重启证据。
func GetToolRiskStatsSnapshot() ToolRiskStatsSnapshot {
	toolRiskStatsMu.Lock()
	defer toolRiskStatsMu.Unlock()
	return ToolRiskStatsSnapshot{
		Persisted:    toolRiskStatsActive,
		StoreKey:     ToolRiskStatsKVKey,
		RestoredRows: toolRiskStatsRestored,
		LastFlushAt:  toolRiskStatsLastFlush,
		LastError:    toolRiskStatsLastErr,
	}
}

// applyWhitelistSeed 启动期加载白名单 seed（D-2）。
//
// 只在路径非空时才碰文件系统；路径设置了但文件不存在由 loader 静默跳过。
// 解析失败只告警、不改任何策略（尤其不改 defaultAllow），因为半份 seed 比没有更危险。
func applyWhitelistSeed() {
	path := strings.TrimSpace(os.Getenv(tooluse.WhitelistSeedFileEnv))
	if path == "" {
		return
	}
	res, err := tooluse.ApplyWhitelistSeedFile(GetGlobalPermissionChecker(), path)
	if err != nil {
		logger.Warnf("[permission] ⚠️ 白名单 seed 加载失败，保持启动默认（defaultAllow 未改）：%v", err)
		return
	}
	if !res.Loaded {
		return // 文件不存在：静默跳过是契约的一部分
	}
	logger.Infof("[permission] ✅ 白名单 seed 已加载：global=%d 条 agents=%d 个 agent_tools=%d 条（defaultAllow 未改）",
		res.GlobalTools, res.Agents, res.AgentTools)
}

// GetToolRiskReport 组装 AC③ 的分级报告：静态声明面（每次实算，重启不丢）+
// 授权面（读当前 Agent 白名单）+ 动态观察面（进程内，未接线时计数全 0 且 mode=off）。
//
// 授权面**不看旗子**：读的是全局 WhitelistPermissionChecker，与判定层挂没挂无关。
// 若在 off 时把 grants 传 nil，报告里 "0 个 Agent 配过白名单" 和 "没读授权数据"
// 就长得一样，而 P9 评审恰恰要在旗子还关着的时候拿这份数据判断能不能开。
// mode/blocks_when_denied 因此才是"观察层与阻断能力"的口径，不是"这份报告可不可信"。
func GetToolRiskReport() tooluse.RiskReport {
	return tooluse.BuildRiskReport(
		tooluse.GetGlobalRegistry(),
		tooluse.AgentGrantReader(GetGlobalPermissionChecker()),
		riskObserverRef,
		riskModeValue,
		// 阻断能力 = 旗子是不是 block：block 态 RiskGateBlockDecorator 真拦，
		// shadow/off 态无拒绝路径。回滚即改旗子，不动代码。
		riskModeValue == string(riskGateBlock),
		RiskGateFlagEnv,
		// D-2：false 时 counts 只代表本进程，不能拿来跨重启对比。
		ToolRiskStatsPersisted(),
	)
}
