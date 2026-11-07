package service

// CH-37b 刀4（R2/R6）：message_hub 出站「结果不明（unknown）」终态——app 层实现与钉子测试。
//
// 语义（CH-37a 批准规格）：unknown = 动作已提交但结果不明（平台成功但 ack 丢失、超时后
// 迟到完成、worker 崩溃回收竞争等），与 delivered/failed 严格区分。三条硬保证：
//   ① unknown 行**永不自动重发**——不进 ClaimPendingOutbound 的 pending 查询、不被
//     exhaust/回收（30s 租约过期也不行）改写、不被 ClaimOutboundForPush/FetchOutboundUndelivered 取走；
//   ② 对账 = 显式迁移（ReconcileUnknownOutbound → delivered/failed），push_error 留操作来源；
//   ③ legacy 二值行为零变化（legacy ack 不翻 unknown 行；delivered/failed 翻转不写 push_error）。
//
// 手法同仓内出站测试：真 PG 8217 影子库 + 手动拨 claimed_at 模拟租约过期，不造网络桩。
// 租户测试腿沿用 *_tenant_test.go 惯例：ModeOn + 预置/回读走 system scope（隔离 callback
// 对无租户 ctx fail-closed）。

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

const unkPlatform = "b37b_unk"

func newUnknownFixture(t *testing.T) (*gorm.DB, *InboxIngressService) {
	t.Helper()
	// NewTestDB 对每个用例 TRUNCATE 清空（无需手工 DELETE；on 模式下 raw Exec 也会被
	// 租户 raw 守卫拒绝，本来就走不通）。
	database := testutil.NewTestDB(t, &model.MessageHub{})
	return database, NewInboxIngressService()
}

// unkScopeCtx 预置/直读用的 system scope：租户 off 模式直通，on 模式绕过隔离 callback
// （被测方法才带真实租户 ctx）。
func unkScopeCtx() context.Context {
	return tenant.WithSystemScope(context.Background())
}

func seedUnkRow(t *testing.T, db *gorm.DB, msgID, conv, status string, tenantID uint) uint {
	t.Helper()
	row := &model.MessageHub{
		TenantID:       tenantID,
		Platform:       unkPlatform,
		AccountID:      "acc1",
		ConversationID: conv,
		MsgID:          msgID,
		MsgType:        "text",
		Content:        "c-" + msgID,
		Direction:      "outbound",
		Status:         status,
	}
	if err := db.WithContext(unkScopeCtx()).Create(row).Error; err != nil {
		t.Fatalf("seed %s 失败: %v", msgID, err)
	}
	return row.ID
}

type unkRow struct {
	Status       string
	PushError    string
	PushAttempts int
	SentAt       time.Time
	ClaimedAt    *time.Time
}

func readUnkRow(t *testing.T, db *gorm.DB, msgID string) unkRow {
	t.Helper()
	var got unkRow
	err := db.WithContext(unkScopeCtx()).Model(&model.MessageHub{}).
		Select("status, push_error, push_attempts, sent_at, claimed_at").
		Where("platform = ? AND msg_id = ?", unkPlatform, msgID).
		Scan(&got).Error
	if err != nil {
		t.Fatalf("回读 %s 失败: %v", msgID, err)
	}
	return got
}

func unkIDOf(t *testing.T, db *gorm.DB, msgID string) uint {
	t.Helper()
	var id uint
	if err := db.WithContext(unkScopeCtx()).Model(&model.MessageHub{}).Select("id").
		Where("platform = ? AND msg_id = ?", unkPlatform, msgID).Scan(&id).Error; err != nil || id == 0 {
		t.Fatalf("取 %s 行 id 失败: id=%d err=%v", msgID, id, err)
	}
	return id
}

func msgIDsOf(rows []*model.MessageHub) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.MsgID)
	}
	return out
}

// TestAckUnknown_EntryAndPersistence 入口①：详细 ack terminalStatus=unknown（HTTP v2 与 WS
// 共用的 service 实现体）——pending → unknown，push_error='ack_unknown'，sent_at 照写
// （与桥端主动 ack failed 同口径）。
func TestAckUnknown_EntryAndPersistence(t *testing.T) {
	db, svc := newUnknownFixture(t)
	ctx := context.Background()
	seedUnkRow(t, db, "unk-in-1", "conv-1", "pending", 1)

	res, err := svc.AckOutboundDeliveredDetailed(ctx, unkPlatform, "acc1", []string{"unk-in-1"}, "", model.BridgeAckStatusUnknown, nil)
	if err != nil {
		t.Fatalf("ack unknown 失败: %v", err)
	}
	if res.AffectedCount != 1 || res.UnknownItemsCount != 1 {
		t.Fatalf("affected/unknown = %d/%d, want 1/1, res=%+v", res.AffectedCount, res.UnknownItemsCount, res)
	}
	if len(res.Items) != 1 || res.Items[0].Status != model.BridgeAckStatusUnknown {
		t.Fatalf("items = %+v, want 单行 status=unknown", res.Items)
	}

	row := readUnkRow(t, db, "unk-in-1")
	if row.Status != model.BridgeAckStatusUnknown {
		t.Errorf("status = %q, want unknown", row.Status)
	}
	if row.PushError != "ack_unknown" {
		t.Errorf("push_error = %q, want ack_unknown（unknown 来源痕迹）", row.PushError)
	}
	if row.SentAt.IsZero() {
		t.Errorf("sent_at 不应为零（unknown 翻转照写 sent_at）")
	}
}

// TestAckUnknown_InvalidTerminalStillRejected 入口守卫：三值之外的 terminalStatus 仍拒绝
// （service 层），且任何行不动。注意 "UNKNOWN"（大写）按原值严格拒绝——归一是 WS 帧入口
// （wsAckFrameStatus）的职责，service 层只认三值原词。
func TestAckUnknown_InvalidTerminalStillRejected(t *testing.T) {
	db, svc := newUnknownFixture(t)
	ctx := context.Background()
	seedUnkRow(t, db, "unk-inv-1", "conv-1", "pending", 1)

	for _, bad := range []string{"sent", "read", "UNKNOWN"} {
		res, err := svc.AckOutboundDeliveredDetailed(ctx, unkPlatform, "acc1", []string{"unk-inv-1"}, "", bad, nil)
		if err == nil {
			t.Fatalf("terminalStatus=%q 应报错，实际 res=%+v", bad, res)
		}
		if res != nil {
			t.Errorf("terminalStatus=%q 应返 nil result，实际 %+v", bad, res)
		}
	}
	row := readUnkRow(t, db, "unk-inv-1")
	if row.Status != "pending" {
		t.Errorf("拒绝后行不得翻转: status=%q", row.Status)
	}
}

// TestUnknownRow_InvisibleToAllResendPaths 防重发自证（规格硬保证①）：unknown 行对全部
// 重发/回收路径不可见——含 30s 认领租约过期场景与到界 exhaust 场景。
func TestUnknownRow_InvisibleToAllResendPaths(t *testing.T) {
	db, svc := newUnknownFixture(t)
	ctx := context.Background()

	// --- 3a 认领：pending 行可领，unknown 行不可领 ---
	seedUnkRow(t, db, "unk-claim-p", "conv-1", "pending", 1)
	seedUnkRow(t, db, "unk-claim-u", "conv-1", model.BridgeAckStatusUnknown, 1)
	rows, err := svc.ClaimPendingOutbound(ctx, unkPlatform, "acc1", 10)
	if err != nil {
		t.Fatalf("ClaimPendingOutbound 失败: %v", err)
	}
	if len(rows) != 1 || rows[0].MsgID != "unk-claim-p" {
		t.Fatalf("认领应只返回 pending 行，实际 %v", msgIDsOf(rows))
	}
	// 此刻 unk-claim-p 已 inflight（push_attempts=1）。

	// --- 3b 30s 租约过期：inflight 行 ack unknown 后，租约到点也不得被回收重发 ---
	if _, err := svc.AckOutboundDeliveredDetailed(ctx, unkPlatform, "acc1", []string{"unk-claim-p"}, "", model.BridgeAckStatusUnknown, nil); err != nil {
		t.Fatalf("ack unknown 失败: %v", err)
	}
	// 拨旧 claimed_at：若回收谓词误含 unknown 行，下一次认领就会把它翻回 pending。
	past := time.Now().Add(-2 * InboxOutboundClaimTimeout)
	if err := db.WithContext(unkScopeCtx()).Model(&model.MessageHub{}).
		Where("platform = ? AND msg_id = ?", unkPlatform, "unk-claim-p").
		Update("claimed_at", past).Error; err != nil {
		t.Fatalf("拨旧 claimed_at 失败: %v", err)
	}
	rows, err = svc.ClaimPendingOutbound(ctx, unkPlatform, "acc1", 10)
	if err != nil {
		t.Fatalf("二次认领失败: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("unknown 行被回收重发（30s 租约场景失守）: %v", msgIDsOf(rows))
	}
	row := readUnkRow(t, db, "unk-claim-p")
	if row.Status != model.BridgeAckStatusUnknown || row.PushError != "ack_unknown" {
		t.Errorf("租约过期后行被改写: %+v", row)
	}
	if row.PushAttempts != 1 {
		t.Errorf("push_attempts = %d, want 1（回收不得清零/累加预算）", row.PushAttempts)
	}

	// --- 3c 到界 exhaust：unknown 行即使 push_attempts 到界、claimed_at 到期也不得被 exhaust 成 failed ---
	seedUnkRow(t, db, "unk-exh-u", "conv-1", model.BridgeAckStatusUnknown, 1)
	if err := db.WithContext(unkScopeCtx()).Model(&model.MessageHub{}).
		Where("platform = ? AND msg_id = ?", unkPlatform, "unk-exh-u").
		Updates(map[string]any{
			"push_attempts": repository.MaxOutboundPushAttempts,
			"claimed_at":    past,
			"push_error":    "ack_unknown", // 直接 seed 的行补上 unknown 翻转的来源痕迹
		}).Error; err != nil {
		t.Fatalf("预置到界行失败: %v", err)
	}
	if _, err := svc.ClaimPendingOutbound(ctx, unkPlatform, "acc1", 10); err != nil {
		t.Fatalf("触发 exhaust 的认领失败: %v", err)
	}
	row = readUnkRow(t, db, "unk-exh-u")
	if row.Status != model.BridgeAckStatusUnknown || row.PushError != "ack_unknown" {
		t.Errorf("exhaust 改写了 unknown 行: %+v", row)
	}

	// --- 3d FetchOutboundUndelivered：unknown 行不在欠交付集合 ---
	seedUnkRow(t, db, "unk-fetch-p", "conv-1", "pending", 1)
	seedUnkRow(t, db, "unk-fetch-u", "conv-1", model.BridgeAckStatusUnknown, 1)
	owed, err := svc.hubRepo.FetchOutboundUndelivered(ctx, unkPlatform, "acc1", InboxOutboundClaimTimeout, 200)
	if err != nil {
		t.Fatalf("FetchOutboundUndelivered 失败: %v", err)
	}
	sawPending := false
	for _, o := range owed {
		if o.MsgID == "unk-fetch-u" {
			t.Errorf("unknown 行进了欠交付集合（会被重投）")
		}
		if o.MsgID == "unk-fetch-p" {
			sawPending = true
		}
	}
	if !sawPending {
		t.Errorf("对照组 pending 行应在欠交付集合（夹具自检）")
	}

	// --- 3e ClaimOutboundForPush（SSE 即时下推门）：unknown 行必须 false ---
	ok, err := svc.hubRepo.ClaimOutboundForPush(ctx, uint64(unkIDOf(t, db, "unk-fetch-u")), InboxOutboundClaimTimeout)
	if err != nil {
		t.Fatalf("ClaimOutboundForPush 失败: %v", err)
	}
	if ok {
		t.Errorf("unknown 行被 SSE 即时下推路径认领（会重发）")
	}
	// 对照：pending 行走同一谓词必须能领（证明 3e 的 false 是谓词判的，不是夹具坏的）。
	if ok, err := svc.hubRepo.ClaimOutboundForPush(ctx, uint64(unkIDOf(t, db, "unk-fetch-p")), InboxOutboundClaimTimeout); err != nil || !ok {
		t.Errorf("pending 行应可被 SSE 下推认领: ok=%v err=%v", ok, err)
	}
}

// TestUnknownRow_LegacyAckNoFlip 兼容④：legacy 二值 ack 对 unknown 行零触碰——
// legacy AckOutboundDelivered（status IN pending/inflight 谓词）匹配不到 unknown 行；
// 详细 ack delivered 对 unknown 行按 duplicate 分类、不翻转。
func TestUnknownRow_LegacyAckNoFlip(t *testing.T) {
	db, svc := newUnknownFixture(t)
	ctx := context.Background()
	seedUnkRow(t, db, "unk-leg-1", "conv-1", model.BridgeAckStatusUnknown, 1)

	n, err := svc.AckOutboundDelivered(ctx, unkPlatform, "acc1", []string{"unk-leg-1"})
	if err != nil {
		t.Fatalf("legacy ack 失败: %v", err)
	}
	if n != 0 {
		t.Fatalf("legacy ack 不得翻转 unknown 行: affected=%d", n)
	}

	res, err := svc.AckOutboundDeliveredDetailed(ctx, unkPlatform, "acc1", []string{"unk-leg-1"}, "", model.BridgeAckStatusDelivered, nil)
	if err != nil {
		t.Fatalf("详细 delivered ack 失败: %v", err)
	}
	if res.AffectedCount != 0 {
		t.Errorf("详细 delivered ack 不得翻转 unknown 行: affected=%d", res.AffectedCount)
	}
	sawDuplicate := false
	for _, it := range res.Items {
		if it.Status != model.BridgeAckStatusDuplicate {
			t.Errorf("unknown 行的迟到 delivered ack 应归 duplicate, got %q", it.Status)
		}
		if it.Status == model.BridgeAckStatusDuplicate {
			sawDuplicate = true
		}
	}
	if !sawDuplicate {
		t.Errorf("回执里应有 duplicate 分类项")
	}
	row := readUnkRow(t, db, "unk-leg-1")
	if row.Status != model.BridgeAckStatusUnknown {
		t.Errorf("行被迟到 ack 翻转: %+v", row)
	}
}

// TestLegacyTwoValue_Unchanged legacy 二值回归：delivered/failed 翻转语义与列值集逐格不变
// （failed 翻转不写 push_error——terminalAckUpdates 只对 unknown 附加 push_error）。
func TestLegacyTwoValue_Unchanged(t *testing.T) {
	db, svc := newUnknownFixture(t)
	ctx := context.Background()
	seedUnkRow(t, db, "unk-leg-d", "conv-1", "pending", 1)
	seedUnkRow(t, db, "unk-leg-f", "conv-1", "pending", 1)

	res, err := svc.AckOutboundDeliveredDetailed(ctx, unkPlatform, "acc1", []string{"unk-leg-d"}, "", model.BridgeAckStatusDelivered, nil)
	if err != nil || res.AffectedCount != 1 || res.AckedItemsCount != 1 || res.UnknownItemsCount != 0 {
		t.Fatalf("legacy delivered ack 读数异常: res=%+v err=%v", res, err)
	}
	row := readUnkRow(t, db, "unk-leg-d")
	if row.Status != model.BridgeAckStatusDelivered || row.PushError != "" {
		t.Errorf("delivered 翻转行为变化: %+v", row)
	}

	res, err = svc.AckOutboundDeliveredDetailed(ctx, unkPlatform, "acc1", []string{"unk-leg-f"}, "", model.BridgeAckStatusFailed, nil)
	if err != nil || res.FailedItemsCount != 1 || res.UnknownItemsCount != 0 {
		t.Fatalf("legacy failed ack 读数异常: res=%+v err=%v", res, err)
	}
	row = readUnkRow(t, db, "unk-leg-f")
	if row.Status != model.BridgeAckStatusFailed || row.PushError != "" {
		t.Errorf("failed 翻转行为变化（push_error 必须仍为空）: %+v", row)
	}
}

// TestReconcileUnknownOutbound_TwoTargets 对账出口：unknown → delivered / failed 各一，
// push_error 留操作来源，unknown 计数随之收敛；守卫腿：非法目标、空 operator、非 unknown 行
// 全部拒绝或 0 行；重复对账幂等 0 行。
func TestReconcileUnknownOutbound_TwoTargets(t *testing.T) {
	db, svc := newUnknownFixture(t)
	ctx := context.Background()
	idD := seedUnkRow(t, db, "unk-rec-d", "conv-1", model.BridgeAckStatusUnknown, 1)
	idF := seedUnkRow(t, db, "unk-rec-f", "conv-1", model.BridgeAckStatusUnknown, 1)
	seedUnkRow(t, db, "unk-rec-t", "conv-1", "pending", 1)

	// 守卫：unknown 目标 / 非法目标 / 空 operator。
	if _, err := svc.ReconcileUnknownOutbound(ctx, unkPlatform, "acc1", []uint{idD}, model.BridgeAckStatusUnknown, "admin_1"); err == nil {
		t.Errorf("target=unknown 应拒绝（unknown→unknown 无意义）")
	}
	if _, err := svc.ReconcileUnknownOutbound(ctx, unkPlatform, "acc1", []uint{idD}, "sent", "admin_1"); err == nil {
		t.Errorf("target=sent 应拒绝")
	}
	if _, err := svc.ReconcileUnknownOutbound(ctx, unkPlatform, "acc1", []uint{idD}, model.BridgeAckStatusDelivered, "  "); err == nil {
		t.Errorf("空 operator 应拒绝（对账必须留操作来源）")
	}

	var before int64
	if err := db.WithContext(unkScopeCtx()).Model(&model.MessageHub{}).
		Where("platform = ? AND status = ?", unkPlatform, model.BridgeAckStatusUnknown).
		Count(&before).Error; err != nil {
		t.Fatalf("计数失败: %v", err)
	}

	if n, err := svc.ReconcileUnknownOutbound(ctx, unkPlatform, "acc1", []uint{idD}, model.BridgeAckStatusDelivered, "admin_1"); err != nil || n != 1 {
		t.Fatalf("对账 delivered 失败: n=%d err=%v", n, err)
	}
	if n, err := svc.ReconcileUnknownOutbound(ctx, unkPlatform, "acc1", []uint{idF}, model.BridgeAckStatusFailed, "api:ops_7"); err != nil || n != 1 {
		t.Fatalf("对账 failed 失败: n=%d err=%v", n, err)
	}

	rowD := readUnkRow(t, db, "unk-rec-d")
	if rowD.Status != model.BridgeAckStatusDelivered || rowD.PushError != "reconciled_unknown:admin_1" {
		t.Errorf("delivered 对账行异常: %+v", rowD)
	}
	rowF := readUnkRow(t, db, "unk-rec-f")
	if rowF.Status != model.BridgeAckStatusFailed || rowF.PushError != "reconciled_unknown:api:ops_7" {
		t.Errorf("failed 对账行异常: %+v", rowF)
	}
	// 非 unknown 行（pending）不得被对账触碰。
	rowT := readUnkRow(t, db, "unk-rec-t")
	if rowT.Status != "pending" || rowT.PushError != "" {
		t.Errorf("pending 行被对账触碰: %+v", rowT)
	}

	var after int64
	if err := db.WithContext(unkScopeCtx()).Model(&model.MessageHub{}).
		Where("platform = ? AND status = ?", unkPlatform, model.BridgeAckStatusUnknown).
		Count(&after).Error; err != nil {
		t.Fatalf("计数失败: %v", err)
	}
	if after != before-2 {
		t.Errorf("unknown 计数未收敛: before=%d after=%d", before, after)
	}

	// repo 计数方法（channel/account 维）与全局口径一致。
	if n, err := svc.hubRepo.CountOutboundUnknown(ctx, unkPlatform, "acc1"); err != nil || n != after {
		t.Errorf("CountOutboundUnknown = %d err=%v, want %d", n, err, after)
	}
	// 幂等：重复对账已迁移行 → 0 行，不报错。
	if n, err := svc.ReconcileUnknownOutbound(ctx, unkPlatform, "acc1", []uint{idD}, model.BridgeAckStatusDelivered, "admin_1"); err != nil || n != 0 {
		t.Errorf("重复对账应 0 行: n=%d err=%v", n, err)
	}
}

// TestReconcileUnknownOutbound_TenantScope 租户作用域强制：on 模式下 ctx 绑租户 A，
// 跨租户（B）的 unknown 行 0 行可迁移、0 行可计数；本租户行正常迁移。
func TestReconcileUnknownOutbound_TenantScope(t *testing.T) {
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() {
		tenant.ResetRegistryForTest()
		tenant.SetMode(tenant.ModeOff)
	})
	db, svc := newUnknownFixture(t)
	const (
		tenantA = uint(731)
		tenantB = uint(732)
	)
	idA := seedUnkRow(t, db, "unk-tn-a", "conv-a", model.BridgeAckStatusUnknown, tenantA)
	seedUnkRow(t, db, "unk-tn-b", "conv-b", model.BridgeAckStatusUnknown, tenantB)

	ctxA := tenant.WithTenantID(context.Background(), tenantA)
	// 计数先行：ctxA 只看见本租户 A 的 unknown 行（B 行存在但不可见）。
	if n, err := svc.hubRepo.CountOutboundUnknown(ctxA, unkPlatform, "acc1"); err != nil || n != 1 {
		t.Fatalf("租户作用域计数 = %d err=%v, want 1（仅本租户可见）", n, err)
	}
	if n, err := svc.ReconcileUnknownOutbound(ctxA, unkPlatform, "acc1", []uint{idA}, model.BridgeAckStatusDelivered, "admin_a"); err != nil || n != 1 {
		t.Fatalf("本租户对账失败: n=%d err=%v", n, err)
	}
	rowA := readUnkRow(t, db, "unk-tn-a")
	if rowA.Status != model.BridgeAckStatusDelivered {
		t.Errorf("本租户行未迁移: %+v", rowA)
	}
	rowB := readUnkRow(t, db, "unk-tn-b")
	if rowB.Status != model.BridgeAckStatusUnknown {
		t.Errorf("跨租户行被对账触碰（越界）: %+v", rowB)
	}
	// 越界显式腿：只传 B 租户行 id，A 的 ctx → 0 行。
	if n, err := svc.ReconcileUnknownOutbound(ctxA, unkPlatform, "acc1", []uint{unkIDOf(t, db, "unk-tn-b")}, model.BridgeAckStatusDelivered, "admin_a"); err != nil || n != 0 {
		t.Errorf("跨租户对账应 0 行: n=%d err=%v", n, err)
	}
	// 对账后 A 租户已无 unknown 行；B 行不可见 → ctxA 计数 0（B 行仍在库，直读可证）。
	if n, err := svc.hubRepo.CountOutboundUnknown(ctxA, unkPlatform, "acc1"); err != nil || n != 0 {
		t.Errorf("ctxA 计数 = %d err=%v, want 0", n, err)
	}
	if rowB := readUnkRow(t, db, "unk-tn-b"); rowB.Status != model.BridgeAckStatusUnknown {
		t.Errorf("B 行必须仍在库且保持 unknown: %+v", rowB)
	}
}

// TestUnknownRace_ReconcileVsAck_SingleWinner 并发⑥：unknown 行上对账与（迟到）ack / 另一路
// 对账竞争——单胜者。两格：
//  1. 同一 unknown 行 reconcile(delivered) vs reconcile(failed) 并发 → 总迁移行数恰 1，
//     终态 ∈ {delivered, failed}（PG 行锁 + WHERE status='unknown' 重评裁定）；
//  2. 同一 pending 行 ack(unknown) vs ack(delivered) 并发 → 总翻转恰 1，终态 ∈ {unknown, delivered}
//     （unknown 与既有二值 ack 同一谓词集合，胜负由 FOR UPDATE 行锁裁定，绝不双写）。
func TestUnknownRace_ReconcileVsAck_SingleWinner(t *testing.T) {
	db, svc := newUnknownFixture(t)
	ctx := context.Background()

	// --- 格1：对账 vs 对账 ---
	idR := seedUnkRow(t, db, "unk-race-r", "conv-1", model.BridgeAckStatusUnknown, 1)
	var total atomic.Int64
	var wg sync.WaitGroup
	start1 := make(chan struct{})
	for _, target := range []string{model.BridgeAckStatusDelivered, model.BridgeAckStatusFailed} {
		wg.Add(1)
		go func(target string) {
			defer wg.Done()
			<-start1
			n, _ := svc.ReconcileUnknownOutbound(ctx, unkPlatform, "acc1", []uint{idR}, target, "race_admin")
			total.Add(n)
		}(target)
	}
	close(start1)
	wg.Wait()
	if got := total.Load(); got != 1 {
		t.Fatalf("对账竞争总迁移行数 = %d, want 1（单胜者）", got)
	}
	row := readUnkRow(t, db, "unk-race-r")
	if row.Status != model.BridgeAckStatusDelivered && row.Status != model.BridgeAckStatusFailed {
		t.Fatalf("对账后终态异常: %q", row.Status)
	}

	// --- 格2：ack unknown vs ack delivered ---
	seedUnkRow(t, db, "unk-race-a", "conv-1", "pending", 1)
	total.Store(0)
	start2 := make(chan struct{})
	for _, st := range []string{model.BridgeAckStatusUnknown, model.BridgeAckStatusDelivered} {
		wg.Add(1)
		go func(st string) {
			defer wg.Done()
			<-start2
			res, err := svc.AckOutboundDeliveredDetailed(ctx, unkPlatform, "acc1", []string{"unk-race-a"}, "", st, nil)
			if err == nil && res != nil {
				total.Add(int64(res.AffectedCount))
			}
		}(st)
	}
	close(start2)
	wg.Wait()
	if got := total.Load(); got != 1 {
		t.Fatalf("ack 竞争总翻转行数 = %d, want 1（单胜者）", got)
	}
	row = readUnkRow(t, db, "unk-race-a")
	if row.Status != model.BridgeAckStatusUnknown && row.Status != model.BridgeAckStatusDelivered {
		t.Fatalf("ack 竞争后终态异常: %q", row.Status)
	}
}
