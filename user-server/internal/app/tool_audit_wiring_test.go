// tool_audit_wiring_test.go T-P1-08：工具审计 DB 落库的装配层实跑。
//
// 装配层单独设测沿用 T-P0-07 的教训：本卡的病灶恰恰是"tooluse 侧单测全绿、
// 生产装配点却从来没人调用"。只测 DBAuditLogger 自身永远测不到这一层，
// 因此下面每条都真的走 applyToolAuditPersistence，断言 config.AuditLogger
// 有没有被换掉、换成了什么。
//
// 卡面 AC：
//
//	①写操作后 DB 可查审计行（含耗时） → TestApplyToolAuditPersistenceOnWritesRows
//	②DB 不可用时降级内存且不报错中断 → TestApplyToolAuditPersistenceDegradesWhenTableGone
//	③调试 API 仍工作 → router 侧 TestToolAuditPersistenceEchoShape / TestToolAuditDBBlockedReasonBranches
//
// 另有一条不属于卡面但更要紧的：关旗时 executor 配置必须逐字回到接线前的样子
// （TestApplyToolAuditPersistenceOffLeavesExecutorUnchanged），否则"默认 off"只是注释里的承诺。
package app

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// restoreToolAuditState 保存/还原包级落库状态。
//
// 必须还原：applyToolAuditPersistence 会把 toolAuditDBLogger 起成一个带后台
// goroutine 的对象并写进包级变量，只靠 t.Setenv 复原环境变量不够（同包其它测试读同一组变量，
// 其中 InitGlobalToolExecutor 的装配测试还会读 memAuditLogger）。
func restoreToolAuditState(t *testing.T) {
	t.Helper()
	om, ol, oq, or := toolAuditMode, toolAuditDBLogger, toolAuditQueueSize, toolAuditRepo
	mem, cost := memAuditLogger, memCostTracker
	t.Cleanup(func() {
		if toolAuditDBLogger != nil {
			toolAuditDBLogger.Close()
		}
		toolAuditMode, toolAuditDBLogger, toolAuditQueueSize, toolAuditRepo = om, ol, oq, or
		memAuditLogger, memCostTracker = mem, cost
	})
}

// newAuditTestConfig 造一份与生产同形的 executor 配置（内存审计 + 内存计费）。
func newAuditTestConfig() tooluse.ToolExecutorConfig {
	memAuditLogger = tooluse.NewMemoryAuditLogger(1000)
	memCostTracker = tooluse.NewMemoryCostTracker()
	return tooluse.ToolExecutorConfig{
		AuditLogger: memAuditLogger,
		CostTracker: memCostTracker,
	}
}

// auditTestEntry 造一条带耗时的审计条目（i 参与耗时，便于断言 duration_ms 落库）。
func auditTestEntry(toolName string, i int) tooluse.AuditEntry {
	return tooluse.AuditEntry{
		TraceID:     "trace-" + toolName,
		ToolName:    toolName,
		Success:     i%2 == 0,
		Duration:    time.Duration(i+1) * 150 * time.Millisecond,
		ArgsSummary: "客户张三的订单备注",
		ExecutedAt:  time.Now(),
	}
}

func TestParseToolAuditMode(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"", toolAuditModeOff},
		{"off", toolAuditModeOff},
		{"false", toolAuditModeOff},
		{"0", toolAuditModeOff},
		{"no", toolAuditModeOff},
		{"disabled", toolAuditModeOff},
		{"on", toolAuditModeOn},
		{"ON", toolAuditModeOn},
		{" true ", toolAuditModeOn},
		{"1", toolAuditModeOn},
		{"yes", toolAuditModeOn},
		{"enabled", toolAuditModeOn},
		// 认不出的值判 off：本卡不改变任何工具调用的结果，但"以为在落库其实没有"
		// 依然要显式拒绝，不能猜。
		{"banana", toolAuditModeOff},
		{"shadow", toolAuditModeOff},
	}
	for _, c := range cases {
		if got := parseToolAuditMode(c.raw); got != c.want {
			t.Errorf("parseToolAuditMode(%q) = %s，期望 %s", c.raw, got, c.want)
		}
	}
}

// 关旗（默认）时不得构造任何对象，也不得改动 config.AuditLogger。
func TestApplyToolAuditPersistenceOffLeavesExecutorUnchanged(t *testing.T) {
	restoreToolAuditState(t)
	t.Setenv(ToolAuditFlagEnv, "")
	config := newAuditTestConfig()
	before := config.AuditLogger

	if mode := applyToolAuditPersistence(&config, nil); mode != toolAuditModeOff {
		t.Fatalf("未设旗子应回 off，实际 %s", mode)
	}
	if config.AuditLogger != before {
		t.Error("off 时 config.AuditLogger 被替换了 ⇒ 装饰链与接线前不等价")
	}
	if toolAuditDBLogger != nil {
		t.Error("off 时不应构造 DBAuditLogger")
	}
	snap := GetToolAuditSnapshot()
	if snap.Wired || snap.Mode != toolAuditModeOff {
		t.Errorf("off 快照异常：%+v", snap)
	}
}

// 旗子开了却拿不到 DB 句柄 ⇒ 判 off 并回显 off，
// 不留下"看着开了其实没落库"的中间态（那正是本卡开工时那个资产的状态）。
func TestApplyToolAuditPersistenceNilDBFallsBackToOff(t *testing.T) {
	restoreToolAuditState(t)
	t.Setenv(ToolAuditFlagEnv, "on")
	config := newAuditTestConfig()
	before := config.AuditLogger

	if mode := applyToolAuditPersistence(&config, nil); mode != toolAuditModeOff {
		t.Fatalf("nil DB 时应回 off，实际 %s", mode)
	}
	if config.AuditLogger != before {
		t.Error("nil DB 时不应替换 AuditLogger")
	}
	if GetToolAuditSnapshot().Wired {
		t.Error("nil DB 时快照不应显示已接线")
	}
	if GetToolAuditSnapshot().Mode != toolAuditModeOff {
		t.Error("nil DB 退化后 mode 必须跟着回 off，否则端点会显示 on 却一条不写")
	}
}

// AC③（阈值走配置）：队列容量可覆盖，越界值回退默认而不是照单全收。
func TestApplyToolAuditPersistenceQueueSizeFromEnv(t *testing.T) {
	restoreToolAuditState(t)
	testDB := testutil.NewTestDB(t, &model.ToolCallAudit{})
	cases := []struct {
		raw  string
		want int
	}{
		{"", toolAuditQueueDefault},
		{"5", 5},
		{"0", toolAuditQueueDefault},      // 0 队列 = 每条都降级，判越界
		{"-1", toolAuditQueueDefault},     // 同上
		{"300000", toolAuditQueueDefault}, // 超上限
		{"abc", toolAuditQueueDefault},
	}
	for _, c := range cases {
		t.Setenv(ToolAuditFlagEnv, "on")
		t.Setenv(toolAuditQueueEnv, c.raw)
		config := newAuditTestConfig()
		if mode := applyToolAuditPersistence(&config, testDB); mode != toolAuditModeOn {
			t.Fatalf("queue=%q 时应为 on，实际 %s", c.raw, mode)
		}
		snap := GetToolAuditSnapshot()
		if snap.QueueSize != c.want {
			t.Errorf("TOOL_AUDIT_QUEUE_SIZE=%q ⇒ QueueSize=%d，期望 %d", c.raw, snap.QueueSize, c.want)
		}
		if toolAuditDBLogger != nil {
			toolAuditDBLogger.Close()
			toolAuditDBLogger = nil
		}
	}
}

// AC①：接上之后，真的能在库里查到审计行，且耗时字段（CS-58）落了库。
func TestApplyToolAuditPersistenceOnWritesRows(t *testing.T) {
	restoreToolAuditState(t)
	t.Setenv(ToolAuditFlagEnv, "on")
	t.Setenv(toolAuditQueueEnv, "")
	testDB := testutil.NewTestDB(t, &model.ToolCallAudit{})
	config := newAuditTestConfig()

	if mode := applyToolAuditPersistence(&config, testDB); mode != toolAuditModeOn {
		t.Fatalf("应为 on，实际 %s", mode)
	}
	if _, ok := config.AuditLogger.(*tooluse.CompositeAuditLogger); !ok {
		t.Fatalf("on 时 config.AuditLogger 应为 CompositeAuditLogger，实际 %T", config.AuditLogger)
	}

	ctx := context.Background()
	before, err := ToolAuditDBCount(ctx)
	if err != nil {
		t.Fatalf("CountAll 失败：%v", err)
	}
	const toolName = "wiring.test.persist"
	for i := 0; i < 3; i++ {
		config.AuditLogger.Log(ctx, auditTestEntry(toolName, i))
	}
	toolAuditDBLogger.Close()
	toolAuditDBLogger = nil

	after, err := ToolAuditDBCount(ctx)
	if err != nil {
		t.Fatalf("CountAll 失败：%v", err)
	}
	if after-before != 3 {
		t.Fatalf("落库行数增量期望 3，实际 %d（before=%d after=%d，stats=%+v）",
			after-before, before, after, GetToolAuditSnapshot().DBStats)
	}

	rows, err := ToolAuditDBRecent(ctx, toolName, 10)
	if err != nil {
		t.Fatalf("Recent 失败：%v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("按工具名过滤期望 3 行，实际 %d", len(rows))
	}
	for _, row := range rows {
		if row.DurationMs <= 0 {
			t.Errorf("耗时应落库（CS-58），实际 %d", row.DurationMs)
		}
	}

	// 计费同源：DB 口径能重算出同样的次数与总耗时
	costs, err := ToolAuditDBCostAggregates(ctx)
	if err != nil {
		t.Fatalf("CostAggregates 失败：%v", err)
	}
	var found bool
	for _, row := range costs {
		if row.ToolName != toolName {
			continue
		}
		found = true
		if row.TotalCalls != 3 {
			t.Errorf("聚合次数期望 3，实际 %d", row.TotalCalls)
		}
		if row.TotalDurationMs != 150+300+450 {
			t.Errorf("聚合总耗时期望 900，实际 %d", row.TotalDurationMs)
		}
		if row.SuccessCalls != 2 || row.FailedCalls != 1 {
			t.Errorf("成功/失败期望 2/1，实际 %d/%d", row.SuccessCalls, row.FailedCalls)
		}
		if rate := row.SuccessRate(); rate < 0.66 || rate > 0.67 {
			t.Errorf("成功率期望 ≈0.667，实际 %v", rate)
		}
	}
	if !found {
		t.Errorf("聚合结果里没有 %s：%+v", toolName, costs)
	}
}

// AC②：表不在了（DB 不可用的代表形态）⇒ 写入整批降级到内存，调用方不报错、不中断，
// 且降级量在 Stats 里看得见。
//
// 为什么用"删表"而不是"关掉 PG"：后者要么影响同库其它测试，要么根本测不到
// 写失败分支；删表能精确复现"接了线但落不了库"这一整条真实故障路径。
func TestApplyToolAuditPersistenceDegradesWhenTableGone(t *testing.T) {
	restoreToolAuditState(t)
	t.Setenv(ToolAuditFlagEnv, "on")
	testDB := testutil.NewTestDB(t, &model.ToolCallAudit{})
	if err := testDB.Migrator().DropTable(&model.ToolCallAudit{}); err != nil {
		t.Fatalf("删表失败：%v", err)
	}
	config := newAuditTestConfig()
	if mode := applyToolAuditPersistence(&config, testDB); mode != toolAuditModeOn {
		t.Fatalf("应为 on（装配期无从知道表没了），实际 %s", mode)
	}

	ctx := context.Background()
	func() {
		// Log 不该 panic，也不该把错误抛回调用方 —— 兜一层 recover，
		// 让"抛回来"这件事以测试失败的形式暴露。
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("DB 写失败时审计降级路径 panic：%v", r)
			}
		}()
		for i := 0; i < 2; i++ {
			config.AuditLogger.Log(ctx, auditTestEntry("wiring.test.degrade", i))
		}
	}()
	toolAuditDBLogger.Close()
	stats := toolAuditDBLogger.Stats() // 必须在把包级引用清空前读，off 态快照不含 stats
	toolAuditDBLogger = nil

	if stats.DBRows != 0 {
		t.Errorf("表已不存在，DBRows 应为 0，实际 %d", stats.DBRows)
	}
	if stats.FellBack != 2 || stats.FailBatches == 0 {
		t.Errorf("期望 2 条未落库 + 至少 1 个失败批次，实际 %+v", stats)
	}
	// 内存里恰好 2 条：复合器的内存腿收下一次；DB 失败**不得**再往内存塞第二份。
	// 这一条是实测逼出来的（第一版把 memAuditLogger 同时当 fallback，故障期每行重复一次，
	// 10000 条环形缓冲按 2 倍速被吃、Count() 直接翻倍）。
	if got := memAuditLogger.Count(); got != 2 {
		t.Errorf("降级路径上内存应恰好 2 条（不重复、不丢失），实际 %d", got)
	}
	// 调用方视角：AuditLogger.Log 全程无错误返回，工具调用链路不受影响。
	if _, err := ToolAuditDBCount(ctx); err == nil {
		t.Error("表已删除，读侧应报错（把故障摊开），而不是回 0 行")
	}
}

// 快照形状：表名/旗子名必须是运维能照着去查的原文，且未装配时不得谎报 wired。
func TestGetToolAuditSnapshotShape(t *testing.T) {
	restoreToolAuditState(t)
	t.Setenv(ToolAuditFlagEnv, "")
	var config tooluse.ToolExecutorConfig
	if mode := applyToolAuditPersistence(&config, nil); mode != toolAuditModeOff {
		t.Fatalf("off 期望，实际 %s", mode)
	}
	snap := GetToolAuditSnapshot()
	if snap.TableName != "tool_call_audits" {
		t.Errorf("TableName 回显异常：%s（端点把它原样摊给运维，运维照着查表）", snap.TableName)
	}
	if ToolAuditFlagEnv != "FF_TOOL_AUDIT_DB" {
		t.Errorf("旗子名漂移：文档/部署脚本按 FF_TOOL_AUDIT_DB 写，代码却读 %s", ToolAuditFlagEnv)
	}
	if snap.DBHandle {
		t.Error("nil 句柄时 DBHandle 不应为真")
	}
	if snap.HasStats {
		t.Error("off 时不该有 DBStats")
	}

	memAuditLogger = tooluse.NewMemoryAuditLogger(10)
	memAuditLogger.Log(context.Background(), auditTestEntry("wiring.test.snapshot", 0))
	if got := GetToolAuditSnapshot().MemCapUsed; got != 1 {
		t.Errorf("MemCapUsed 期望 1，实际 %d", got)
	}
}

// 读侧独立于写旗：关旗时只要拿得到 DB 句柄，`?source=db` 这条路照样能查。
//
// 为什么要单独锁这一条：applyToolAuditPersistence 里 `toolAuditRepo` 是在 off 分支
// **之前**赋的，看起来像"顺手写早了一行"，其实是刻意的——运维要先能回答"这张表在不在、
// 现在有多少行"才敢开写旗；而本卡开工时的状态正是"没人知道表存不存在"（它不存在）。
// 把那一行挪到 off 分支之后就等于收回这个能力，且收回得很安静（端点只会改报 503）。
func TestToolAuditReadSideWorksWhileWriteFlagOff(t *testing.T) {
	restoreToolAuditState(t)
	db := testutil.NewTestDB(t, &model.ToolCallAudit{})
	t.Setenv(ToolAuditFlagEnv, "off")
	config := newAuditTestConfig()

	if mode := applyToolAuditPersistence(&config, db); mode != toolAuditModeOff {
		t.Fatalf("off 期望，实际 %s", mode)
	}
	if !ToolAuditDBAvailable() {
		t.Fatal("有库句柄时读侧应可用（开旗前先要查得到表在不在）")
	}
	if snap := GetToolAuditSnapshot(); !snap.DBHandle || snap.Wired {
		t.Errorf("快照应 DBHandle=true 且 Wired=false，实际 %+v", snap)
	}
	total, err := ToolAuditDBCount(context.Background())
	if err != nil {
		t.Fatalf("off 时 CountAll 不该报错：%v", err)
	}
	if total != 0 {
		t.Errorf("新库应有 0 行，实际 %d", total)
	}
	if _, err := ToolAuditDBRecent(context.Background(), "", 10); err != nil {
		t.Errorf("off 时读最近审计不该报错：%v", err)
	}
}

// 未接线时 Close 必须是 no-op（main.go 无条件 defer 它），且可重入。
func TestCloseToolAuditPersistenceWhenOffIsNoOp(t *testing.T) {
	restoreToolAuditState(t)
	t.Setenv(ToolAuditFlagEnv, "off")
	var config tooluse.ToolExecutorConfig
	if mode := applyToolAuditPersistence(&config, nil); mode != toolAuditModeOff {
		t.Fatalf("off 期望，实际 %s", mode)
	}
	CloseToolAuditPersistence()
	CloseToolAuditPersistence()
}

// 仓储层对 nil 句柄要报错而不是 panic（端点据此回 503，而不是回一份空列表）。
func TestToolAuditRepoNilHandleErrors(t *testing.T) {
	restoreToolAuditState(t)
	t.Setenv(ToolAuditFlagEnv, "off")
	var config tooluse.ToolExecutorConfig
	applyToolAuditPersistence(&config, nil)

	ctx := context.Background()
	if _, err := ToolAuditDBRecent(ctx, "", 10); err == nil {
		t.Error("nil 句柄时 Recent 应报错")
	}
	if _, err := ToolAuditDBCount(ctx); err == nil {
		t.Error("nil 句柄时 CountAll 应报错")
	}
	if _, err := ToolAuditDBCostAggregates(ctx); err == nil {
		t.Error("nil 句柄时 CostAggregates 应报错")
	}
	if ToolAuditDBAvailable() {
		t.Error("nil 句柄时 Available 应为 false")
	}

	// limit<=0 必须报错：GORM 的 Limit(0) 会原样翻成 PG 的 LIMIT 0（恒空结果集），
	// 那样"查不动"会被读成"没有审计"。
	testDB := testutil.NewTestDB(t, &model.ToolCallAudit{})
	toolAuditRepo = repository.NewToolAuditRepository(testDB)
	if _, err := ToolAuditDBRecent(ctx, "", 0); err == nil {
		t.Error("limit=0 应报错而不是返回空列表")
	}
	if _, err := ToolAuditDBRecent(ctx, "", -5); err == nil {
		t.Error("limit<0 应报错")
	}
}
