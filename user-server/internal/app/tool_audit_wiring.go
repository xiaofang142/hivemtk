package app

import (
	"context"
	"os"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// W-6 工具审计 DB 持久化接线（新规划任务清单 T-P1-08）
//
// tooluse.DBAuditLogger（异步批量落库 + 队列满降级）与 CompositeAuditLogger（内存+DB+告警）
// 自 v3.29 前后就写完了，单测也齐，但生产从来没构造过：executor 配置里的 AuditLogger
// 一直是 MemoryAuditLogger ⇒ 重启即丢、多副本各看各的 10000 条。
//
// 本卡接线时实测出的两件事，都写进了各自的修法位置，这里只留结论：
//  1. **表根本不存在**。写入方自带一个 AutoMigrateAuditTable，但它全仓零调用，
//     而建表清单（internal/pkg/db allModels()）里也没有这个模型 —— 开发库实测没有
//     tool_call_audits 表。只"接 logger"不"登记表"的话，第一批写入就会失败并整批
//     静默降级回内存，看起来接了、其实一行都没落。现按仓规把模型收敛进 model 包并登记。
//  2. **摘要截断会产出非法 UTF-8**。summarizeArgs/summarizeResult 原本按字节硬砍
//     （s[:200]），砍在中文字符中间；内存里无所谓，落 PG 的 TEXT 列是
//     `invalid byte sequence for encoding "UTF8"`（实测），且 CreateInBatches 整批提交
//     ⇒ 一条坏数据带走 100 条好审计。现改为 rune 边界回退（tooluse.cutBytes）。
//
// 两态而非三态（与同目录另四把旗子刻意不同，理由在此）：
//
//	off（默认）  沿用纯内存实现，装饰链与接线前逐字节一致
//	on           审计同时写内存与 DB（内存仍是 /agent/tools/audit 的默认数据源）
//
// 熔断/审批门/挽回 worker 做成 off|shadow|enforce，是因为它们会**改变请求结果**，
// 需要一个"只看不拦"的中间档。本旗子不改变任何工具调用的结果，只有"落不落库"两种状态，
// 硬造一个 shadow 档只会多一档无人能解释的语义。相应地，`true/1/on/yes` 这里**就认作 on**
// —— 它买到的是"重启后审计还在"，不是"客户请求被拒"。
//
// 计费（卡片标题里的另一半）不另建表：MemoryCostTracker 统计的次数/成功/失败/总耗时
// 全是 tool_call_audits 按 tool_name 聚合的子集，再存一份只会造出第二个事实源。
// DB 侧口径见 repository.ToolAuditRepository.CostAggregates，端点走 ?source=db。

const (
	// ToolAuditFlagEnv 是总开关；未设 = off。
	ToolAuditFlagEnv = "FF_TOOL_AUDIT_DB"
	// toolAuditQueueEnv 异步落库队列容量。队列满 ⇒ 该条改走内存（不落库），
	// 所以它是"突发写入会不会开始掉 DB 侧"的唯一可调旋钮。
	toolAuditQueueEnv = "TOOL_AUDIT_QUEUE_SIZE"

	toolAuditQueueDefault = 10000
	toolAuditQueueMax     = 200000

	toolAuditModeOff = "off"
	toolAuditModeOn  = "on"
)

// toolAuditRef 当前生效的落库接线状态；由 applyToolAuditPersistence 在装配期写一次。
//
// 读写无锁的前提与 circuitStateRef 相同：写入发生在 router.Setup() 里、
// executor 与 HTTP 服务启动之前。
var (
	toolAuditMode      = toolAuditModeOff
	toolAuditDBLogger  *tooluse.DBAuditLogger
	toolAuditQueueSize = toolAuditQueueDefault
	toolAuditRepo      *repository.ToolAuditRepository
)

// parseToolAuditMode 解析开关。认不出的值判 off 并告警（与其他旗子同口径：
// 宁可"没接上"也不"接成没人预期的样子"）。
func parseToolAuditMode(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "":
		return toolAuditModeOff
	case "off", "false", "0", "no", "n", "none", "disabled":
		return toolAuditModeOff
	case "on", "yes", "y", "enabled", "enable":
		return toolAuditModeOn
	}
	if b, err := strconv.ParseBool(v); err == nil {
		if b {
			return toolAuditModeOn
		}
		return toolAuditModeOff
	}
	logger.Warnf("[tool-audit] %s=%q 无法识别 ⇒ 按 off 处理（审计仍只进内存，重启即丢）；可用值：off|on", ToolAuditFlagEnv, raw)
	return toolAuditModeOff
}

// applyToolAuditPersistence 把 DB 落库挂进 executor 配置，由 InitGlobalToolExecutor 调用。
//
// 返回生效模式（供启动日志与端点回显）。off 时**一个对象都不构造**，config.AuditLogger
// 保持调用方传入的纯内存实现 —— 这是"关旗逐字等价于接线前"的可断言形态。
// on 但拿不到 DB 句柄时不静默退化：照常告警并回 off，因为"以为在落库、其实没有"
// 恰是本卡开工时那个资产的状态，不能再造一次。
func applyToolAuditPersistence(config *tooluse.ToolExecutorConfig, gormDB *gorm.DB) string {
	toolAuditRepo = repository.NewToolAuditRepository(gormDB)
	toolAuditMode = parseToolAuditMode(os.Getenv(ToolAuditFlagEnv))
	if toolAuditMode != toolAuditModeOn {
		toolAuditDBLogger = nil
		toolAuditQueueSize = 0
		return toolAuditModeOff
	}
	if gormDB == nil {
		logger.Warnf("[tool-audit] ⚠️ %s=on 但 DB 句柄为 nil ⇒ 本进程退回纯内存审计（重启即丢）", ToolAuditFlagEnv)
		toolAuditDBLogger = nil
		toolAuditQueueSize = 0
		toolAuditMode = toolAuditModeOff
		return toolAuditModeOff
	}
	queueSize := toolAuditQueueDefault
	if n, ok := envInt("[tool-audit]", toolAuditQueueEnv, 1, toolAuditQueueMax); ok {
		queueSize = n
	}
	// fallback 刻意传 nil，而不是内存 logger：本装配用的是 CompositeAuditLogger，
	// 内存那一腿在 DB 那一腿之前就已经把同一条 entry 收下了。再把它当降级通道传进来，
	// DB 每失败一次内存里就多一份重复行 —— 实测降级 2 条后 Count() 是 4。
	// 重复的代价不止是 /audit 里看到两行：环形缓冲 10000 条在故障期间会按 2 倍速被吃掉，
	// 而且靠 Count() 判断"这周有多少调用"会直接翻倍。
	// AC②"DB 不可用时降级内存"由复合器的内存腿兑现，与 fallback 无关。
	dbLogger := tooluse.NewDBAuditLogger(gormDB, queueSize, nil)
	config.AuditLogger = tooluse.NewCompositeAuditLogger(memAuditLogger, dbLogger, nil)
	toolAuditDBLogger = dbLogger
	toolAuditQueueSize = queueSize
	return toolAuditModeOn
}

// ToolAuditSnapshot 落库接线状态（端点回显用）。
//
// 旗子名不放进快照：它是个常量，作为快照字段就有"哪条构造路径忘了填"的可能，
// 而端点会把它拼进给运维的指令里（"设 X=on 并重启"）。消费方直接读 ToolAuditFlagEnv。
type ToolAuditSnapshot struct {
	Mode       string
	Wired      bool
	TableName  string
	QueueSize  int
	DBHandle   bool
	DBStats    tooluse.DBAuditStats
	HasStats   bool
	MemCapUsed int
}

// GetToolAuditSnapshot 返回当前落库接线状态；未装配时 Mode 为 off、Wired 为 false。
func GetToolAuditSnapshot() ToolAuditSnapshot {
	snap := ToolAuditSnapshot{
		Mode:      toolAuditMode,
		Wired:     toolAuditDBLogger != nil,
		TableName: model.ToolCallAudit{}.TableName(),
	}
	if toolAuditDBLogger != nil {
		snap.DBStats = toolAuditDBLogger.Stats()
		snap.HasStats = true
		snap.QueueSize = toolAuditQueueSize
	}
	snap.DBHandle = toolAuditRepo.Available()
	if memAuditLogger != nil {
		snap.MemCapUsed = memAuditLogger.Count()
	}
	return snap
}

// ToolAuditDBRecent 从 DB 读最近的审计行；仓储不可用时返回错误（端点据此回 503，
// 而不是回一个空列表让人误读成"没有审计"）。
func ToolAuditDBRecent(ctx context.Context, toolName string, limit int) ([]model.ToolCallAudit, error) {
	return toolAuditRepo.Recent(ctx, toolName, limit)
}

// ToolAuditDBCount 返回持久化审计总行数与仓储是否可用。
func ToolAuditDBCount(ctx context.Context) (int64, error) {
	return toolAuditRepo.CountAll(ctx)
}

// ToolAuditDBCostAggregates 从持久化审计行重算各工具调用量/耗时。
func ToolAuditDBCostAggregates(ctx context.Context) ([]repository.ToolAuditCostRow, error) {
	return toolAuditRepo.CostAggregates(ctx)
}

// ToolAuditDBAvailable 报告 DB 读侧是否可用（有句柄且旗子已接）。
func ToolAuditDBAvailable() bool { return toolAuditRepo.Available() }

// CloseToolAuditPersistence 进程退出前排空落库队列。
//
// 不排空的后果很具体：flushInterval 是 1s，崩溃/正常退出前最多有 1s 的批（上限
// batchSize=100 条，队列里还可能更多）留在内存通道里没写出去 —— 而"耐久"正是本卡目的。
// 由 cmd/api/main.go defer 调用；off 时无对象可关，直接返回。
func CloseToolAuditPersistence() {
	if toolAuditDBLogger == nil {
		return
	}
	toolAuditDBLogger.Close()
	logger.Infof("[tool-audit] 落库队列已排空：%+v", toolAuditDBLogger.Stats())
}
