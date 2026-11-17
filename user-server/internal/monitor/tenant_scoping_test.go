package monitor

// 契约锁（TENANT_MODE=on）：监控大盘的每个读数都必须站在「调用方自己租户」的地基上。
//
// 为什么这一格要单独钉：本包 8 个入口全部收下一个 ctx，却一个都没把它交给 GORM
// ——查询一律跑在 `db.GetDB()` 这个无租户上下文的全局句柄上。saas 形态下隔离 callback
// 对租户表 fail-closed，而 HealthOverview/Anomalies 又把几乎每一条的错误吞掉，
// 于是 `/monitor/health` 与 `/monitor/anomalies` 在云上返回 200 + 全零：
// 运维看到的大盘是「链路很健康」，真相是「一条都没读出来」。
// off 模式直通，既有 monitoring 用例全跑在 context.Background() 上，看不出这个形态。
//
// 反向的失效也要钉住：一旦有人为了「让大盘有数」把 ctx 换成 system scope，
// 单租户的管理页就会把别的租户的消息量、卡死会话与 trace 明细一并算进来并显示给自己，
// 那比全零严重得多。所以断言取「各租户各读到各自的行」，不是「读到行」。

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

const (
	// 号段不与 browser 域各租户锁重叠（同库并行跑时共用行号会互相污染计数）。
	monTenantA = uint(91)
	monTenantB = uint(92)
	monChannel = "xiaohongshu"
)

func monitorTenantDB(t *testing.T) (*gorm.DB, context.Context, context.Context) {
	t.Helper()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	gdb := testutil.NewTestDB(t, &model.MessageHub{}, &model.MessageTrace{}, &model.InboxConversation{})
	if gdb == nil {
		t.Fatal("测试库不可达：大盘读数的租户归属无法判定（不 Skip，跳过等于没锁）")
	}
	// 本包的入口按生产形态取全局句柄，测试必须把隔离库接上去，否则读到 nil。
	db.SetTestDB(gdb)
	return gdb,
		tenant.WithTenantID(context.Background(), monTenantA),
		tenant.WithTenantID(context.Background(), monTenantB)
}

// seedHub 落一行中台消息。租户由隔离 callback 从 ctx 填——写侧也在这组锁的覆盖范围内：
// 若有人把写路径改成无 ctx，插入会 fail-closed，本函数即报错而非静默落进默认租户。
func seedHub(t *testing.T, gdb *gorm.DB, ctx context.Context, conv, direction, status string, sentAt time.Time) {
	t.Helper()
	row := &model.MessageHub{
		Platform: monChannel, AccountID: "acct-" + conv, MsgID: conv + "-" + direction + "-" + status,
		Direction: direction, Status: status, MsgType: "text",
		ConversationID: conv, SentAt: sentAt,
	}
	if err := gdb.WithContext(ctx).Create(row).Error; err != nil {
		t.Fatalf("落 %s/%s 消息失败: %v", direction, status, err)
	}
}

func seedTrace(t *testing.T, gdb *gorm.DB, ctx context.Context, traceID, conv, node, status string) {
	t.Helper()
	row := &model.MessageTrace{
		TraceID: traceID, ConversationID: conv, AccountID: "acct-" + conv,
		Channel: monChannel, Node: node, Direction: "inbound", Status: status,
		DurationMs: 10,
	}
	if err := gdb.WithContext(ctx).Create(row).Error; err != nil {
		t.Fatalf("落 trace 节点 %s 失败: %v", node, err)
	}
}

func seedInbox(t *testing.T, gdb *gorm.DB, ctx context.Context, conv string) {
	t.Helper()
	row := &model.InboxConversation{
		Platform: monChannel, AccountID: "acct-" + conv, CustomerID: "cust-" + conv,
		ConversationID: conv,
	}
	if err := gdb.WithContext(ctx).Create(row).Error; err != nil {
		t.Fatalf("落收件箱会话 %s 失败: %v", conv, err)
	}
}

// TestHealthOverviewIsTenantScoped A/B 各有自己的 pending 与 trace，大盘必须各报各的数。
func TestHealthOverviewIsTenantScoped(t *testing.T) {
	gdb, ctxA, ctxB := monitorTenantDB(t)
	stale := time.Now().Add(-time.Hour)
	// A：2 条出站 pending + 1 条入站；B：3 条出站 pending。
	seedHub(t, gdb, ctxA, "conv-a1", "outbound", "pending", stale)
	seedHub(t, gdb, ctxA, "conv-a2", "outbound", "pending", stale)
	seedHub(t, gdb, ctxA, "conv-a1", "inbound", "received", time.Now())
	seedHub(t, gdb, ctxB, "conv-b1", "outbound", "pending", stale)
	seedHub(t, gdb, ctxB, "conv-b2", "outbound", "pending", stale)
	seedHub(t, gdb, ctxB, "conv-b3", "outbound", "pending", stale)
	// trace：A 1 条、B 2 条（TotalTraces 是 MessageTrace 计数）。
	seedTrace(t, gdb, ctxA, "tr-a1", "conv-a1", "ingest", "ok")
	seedTrace(t, gdb, ctxB, "tr-b1", "conv-b1", "ingest", "ok")
	seedTrace(t, gdb, ctxB, "tr-b2", "conv-b2", "ingest", "abnormal")

	hA, err := HealthOverview(ctxA)
	if err != nil {
		t.Fatalf("租户 A 的大盘读数失败: %v", err)
	}
	if hA.PendingCount != 2 {
		t.Errorf("A 的 pending_count = %d，期望 2（B 的 3 条不能进来，自己的 2 条也不能丢）", hA.PendingCount)
	}
	if hA.TotalTraces != 1 {
		t.Errorf("A 的 total_traces = %d，期望 1", hA.TotalTraces)
	}
	if hA.AbnormalCount != 0 {
		t.Errorf("A 的 abnormal_count = %d，期望 0（B 的异常节点不能算进 A）", hA.AbnormalCount)
	}

	hB, err := HealthOverview(ctxB)
	if err != nil {
		t.Fatalf("租户 B 的大盘读数失败: %v", err)
	}
	if hB.PendingCount != 3 {
		t.Errorf("B 的 pending_count = %d，期望 3", hB.PendingCount)
	}
	if hB.TotalTraces != 2 {
		t.Errorf("B 的 total_traces = %d，期望 2", hB.TotalTraces)
	}
	if hB.AbnormalCount != 1 {
		t.Errorf("B 的 abnormal_count = %d，期望 1", hB.AbnormalCount)
	}
}

// TestHealthOverviewFailsOnUnboundContext 未绑租户时必须报错，不能回一版全零。
// 全零在运维眼里是「链路健康」，与「读不出来」完全不可区分——这正是修复前的生产形态。
func TestHealthOverviewFailsOnUnboundContext(t *testing.T) {
	_, ctxA, _ := monitorTenantDB(t)
	// 先让 A 有一条数据：否则「报错」可能被误读成「库是空的」。
	seedHub(t, db.GetDB(), ctxA, "conv-a9", "outbound", "pending", time.Now())
	if _, err := HealthOverview(context.Background()); err == nil {
		t.Fatal("裸 ctx 的大盘读数未被拒绝：fail-closed 的错误被吞掉，运维会看到一版全零健康大盘")
	}
}

// TestAnomaliesIsTenantScoped 异常清单的每一条都带着会话号与卡死时长，跨租户漏出去
// 就是别人的客户线索；而 fail-closed 全吞又让它变成「没有异常」。两个方向都要钉。
func TestAnomaliesIsTenantScoped(t *testing.T) {
	gdb, ctxA, ctxB := monitorTenantDB(t)
	stale := time.Now().Add(-30 * time.Minute)
	// A：1 条可达且卡死的出站 pending（有收件箱会话）。
	seedInbox(t, gdb, ctxA, "conv-a1")
	seedHub(t, gdb, ctxA, "conv-a1", "outbound", "pending", stale)
	// B：1 条不可达且卡死的 pending（无收件箱会话）+ 2 条 failed。
	seedHub(t, gdb, ctxB, "conv-b1", "outbound", "pending", stale)
	seedHub(t, gdb, ctxB, "conv-b2", "outbound", "failed", time.Now())
	seedHub(t, gdb, ctxB, "conv-b3", "outbound", "failed", time.Now())

	gA, err := Anomalies(ctxA)
	if err != nil {
		t.Fatalf("租户 A 的异常清单失败: %v", err)
	}
	if len(gA.StuckReachable) != 1 || gA.StuckReachable[0].ConversationID != "conv-a1" {
		t.Errorf("A 的 stuck_reachable = %+v，期望恰好 conv-a1 一条", gA.StuckReachable)
	}
	if len(gA.Unreachable) != 0 {
		t.Errorf("A 的 unreachable = %+v，期望 0 条（B 的 failed 不能进来）", gA.Unreachable)
	}

	gB, err := Anomalies(ctxB)
	if err != nil {
		t.Fatalf("租户 B 的异常清单失败: %v", err)
	}
	if len(gB.StuckReachable) != 0 {
		t.Errorf("B 的 stuck_reachable = %+v，期望 0 条（A 的卡死会话不能进来）", gB.StuckReachable)
	}
	if len(gB.Unreachable) != 2 {
		t.Errorf("B 的 unreachable = %d 条，期望 2", len(gB.Unreachable))
	}
	if len(gB.StuckUnreachable) != 1 {
		t.Errorf("B 的 stuck_unreachable = %d 条，期望 1", len(gB.StuckUnreachable))
	}
}

// TestAnomaliesFailsOnUnboundContext 同 HealthOverview：吞错的空清单在这里等于「一切正常」。
func TestAnomaliesFailsOnUnboundContext(t *testing.T) {
	gdb, ctxA, _ := monitorTenantDB(t)
	// 先落一行 A 的卡死数据：否则「报错」可能被误读成「库是空的」，
	// 更可能被 db==nil 的 ErrNoDB 蒙过（那是句柄缺失，不是租户拒绝）。
	seedHub(t, gdb, ctxA, "conv-a8", "outbound", "failed", time.Now())
	err := func() error {
		_, e := Anomalies(context.Background())
		return e
	}()
	if err == nil {
		t.Fatal("裸 ctx 的异常清单未被拒绝：查询失败会渲染成一张空异常表，而不是报错")
	}
	if errors.Is(err, ErrNoDB) {
		t.Fatalf("断言落在句柄缺失上，不是租户拒绝: %v", err)
	}
}

// TestTraceViewsAreTenantScoped trace 明细里是节点入参/出参正文，是最不能跨租户的一层。
func TestTraceViewsAreTenantScoped(t *testing.T) {
	gdb, ctxA, ctxB := monitorTenantDB(t)
	seedTrace(t, gdb, ctxA, "tr-a1", "conv-a1", "ingest", "ok")
	seedTrace(t, gdb, ctxA, "tr-a1", "conv-a1", "delivered_ack", "ok")
	seedTrace(t, gdb, ctxB, "tr-b1", "conv-b1", "ingest", "abnormal")
	seedTrace(t, gdb, ctxB, "tr-b1", "conv-b1", "inbox_sync", "ok")
	seedTrace(t, gdb, ctxB, "tr-b1", "conv-b1", "delivered_ack", "ok")

	tsA, err := Traces(ctxA, 50)
	if err != nil {
		t.Fatalf("租户 A 的 trace 列表失败: %v", err)
	}
	if len(tsA) != 1 || tsA[0].TraceID != "tr-a1" || tsA[0].NodeCount != 2 {
		t.Errorf("A 的 trace 列表 = %+v，期望恰好 tr-a1 一轮 2 节点", tsA)
	}
	tsB, err := Traces(ctxB, 50)
	if err != nil {
		t.Fatalf("租户 B 的 trace 列表失败: %v", err)
	}
	if len(tsB) != 1 || tsB[0].TraceID != "tr-b1" {
		t.Errorf("B 的 trace 列表 = %+v，期望恰好 tr-b1 一轮", tsB)
	}

	nhA, err := NodeHealthByChannel(ctxA)
	if err != nil {
		t.Fatalf("租户 A 的节点健康失败: %v", err)
	}
	var totalA int64
	for _, n := range nhA {
		if n.Channel == monChannel {
			totalA += n.Total
		}
	}
	if totalA != 2 {
		t.Errorf("A 的节点健康总数 = %d，期望 2（B 的 3 个节点不能进来）", totalA)
	}

	lcB, err := Lifecycle(ctxB, "conv-b1", "", 5)
	if err != nil {
		t.Fatalf("租户 B 的生命周期视图失败: %v", err)
	}
	if len(lcB) != 1 {
		t.Fatalf("B 应还原自己的一轮链路，实际 %d 轮", len(lcB))
	}
	// A 用同一个会话号去查：查不到才是对的（会话号不是归属凭证，租户谓词才是）。
	lcA, err := Lifecycle(ctxA, "conv-b1", "", 5)
	if err != nil {
		t.Fatalf("租户 A 的跨租户查询应当返回空而不是报错: %v", err)
	}
	if len(lcA) != 0 {
		t.Errorf("A 读到了 B 的会话链路 %d 轮", len(lcA))
	}

	if _, err := Traces(context.Background(), 50); err == nil {
		t.Error("裸 ctx 的 trace 列表未被拒绝")
	}
}

// TestLifecycleLatencyIsTenantScoped 自连接的两端必须同租户：否则 A 的 ingest
// 会拿去和 B 的同 trace_id 送达确认配对，算出一条两边都不存在的链路时延。
func TestLifecycleLatencyIsTenantScoped(t *testing.T) {
	gdb, ctxA, ctxB := monitorTenantDB(t)
	// 两个租户共用同一个 trace_id：断言的才是租户谓词，而不是「号恰好不撞」。
	seedTrace(t, gdb, ctxA, "tr-shared", "conv-a1", "ingest", "ok")
	seedTrace(t, gdb, ctxB, "tr-shared", "conv-b1", "delivered_ack", "ok")

	if lat, err := LifecycleLatencyByChannel(ctxA); err != nil {
		t.Fatalf("租户 A 的端到端时延失败: %v", err)
	} else if len(lat) != 1 || lat[0].SampleSize != 0 || lat[0].P50Ms != nil || lat[0].MeasurementCoverage != 0 {
		t.Errorf("A 保留自己的缺测覆盖率，但有效样本必须为0且分位数未知（B不能配入）：%+v", lat)
	}
	if _, err := LifecycleLatencyByChannel(context.Background()); err == nil {
		t.Error("裸 ctx 的端到端时延未被拒绝")
	}
}
