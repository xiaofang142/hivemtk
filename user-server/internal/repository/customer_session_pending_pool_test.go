// customer_session_pending_pool_test.go 待处理池读口与消息送达回写的仓储契约。
//
// AV-04c 批次5 刀1：这批查询原先写在 service（customer_session.go /
// customer_session_scope.go 经 sessionRepo.GetDB 直拼），下沉时把谓词、排序、
// 分页、租户语义钉死在这里，防止下沉过程或后续改动悄悄改口径。
package repository

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func pendingPoolSetupDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t, &model.CustomerSession{}, &model.SessionMessage{})
}

// pendingSeed 造一条会话。withLastMsg 控制 last_message_at 有无（COALESCE
// 排序的两组输入都要能覆盖）；createdOffset 控制 created_at。
func pendingSeed(t *testing.T, db *gorm.DB, tenantID uint, tag string, status model.SessionStatus, agentID uint, priority int, createdOffset time.Duration, withLastMsg bool) *model.CustomerSession {
	t.Helper()
	now := time.Now()
	row := &model.CustomerSession{
		TenantID:  tenantID,
		SessionID: "sess_pool_" + tag,
		Platform:  model.PlatformDouyin,
		AccountID: "acct_pool_" + tag,
		UserID:    "u_pool_" + tag,
		Status:    status,
		Priority:  priority,
		AgentID:   agentID,
		CreatedAt: now.Add(createdOffset),
	}
	if withLastMsg {
		tm := now.Add(-30 * time.Minute)
		row.LastMessageAt = &tm
	}
	if err := db.WithContext(tenant.WithSystemScope(context.Background())).Create(row).Error; err != nil {
		t.Fatalf("预置会话失败: %v", err)
	}
	return row
}

func pendingStatuses() []model.SessionStatus {
	return []model.SessionStatus{
		model.SessionStatusPending,
		model.SessionStatusAIHandling,
		model.SessionStatusWaiting,
	}
}

// 待处理池口径：只含 pending/ai_handling/waiting；resolved/closed/human_handling 不进池。
func TestCustomerSessionRepository_ListPending_StatusFilterAndOrder(t *testing.T) {
	db := pendingPoolSetupDB(t)
	repo := NewCustomerSessionRepositoryWithDB(db)
	ctx := context.Background()

	pendingSeed(t, db, tenant.DefaultTenantID, "p1", model.SessionStatusPending, 0, 1, -3*time.Hour, false)
	pendingSeed(t, db, tenant.DefaultTenantID, "p3", model.SessionStatusPending, 0, 3, -1*time.Hour, false)
	pendingSeed(t, db, tenant.DefaultTenantID, "ai2", model.SessionStatusAIHandling, 0, 2, -2*time.Hour, false)
	pendingSeed(t, db, tenant.DefaultTenantID, "w0", model.SessionStatusWaiting, 0, 0, -30*time.Minute, false)
	pendingSeed(t, db, tenant.DefaultTenantID, "r9", model.SessionStatusResolved, 0, 9, -10*time.Minute, false)
	pendingSeed(t, db, tenant.DefaultTenantID, "h9", model.SessionStatusHumanHandling, 0, 9, -5*time.Minute, false)

	sessions, err := repo.ListPending(ctx, pendingStatuses(), 0, 100)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(sessions) != 4 {
		t.Fatalf("待处理池应只含 4 条（pending×2 + ai_handling + waiting），实际 %d", len(sessions))
	}
	for _, s := range sessions {
		if s.Status == model.SessionStatusResolved || s.Status == model.SessionStatusClosed || s.Status == model.SessionStatusHumanHandling {
			t.Errorf("resolved/closed/human_handling 不应出现在待处理池: %v", s.Status)
		}
	}
	// 排序：priority DESC → 同优先级按 COALESCE(last_message_at, created_at) ASC。
	if sessions[0].Priority != 3 || sessions[1].Priority != 2 || sessions[2].Priority != 1 || sessions[3].Priority != 0 {
		t.Errorf("priority DESC 排序被改动: %d %d %d %d", sessions[0].Priority, sessions[1].Priority, sessions[2].Priority, sessions[3].Priority)
	}

	// 同优先级内 COALESCE 语义：有 last_message_at 用它（较新者在前），
	// 否则回退 created_at（该行 created_at 更早，应排后面）。
	newer := pendingSeed(t, db, tenant.DefaultTenantID, "co1", model.SessionStatusPending, 0, 5, -1*time.Hour, true)
	older := pendingSeed(t, db, tenant.DefaultTenantID, "co2", model.SessionStatusPending, 0, 5, -2*time.Hour, false)
	samePrio, err := repo.ListPending(ctx, pendingStatuses(), 0, 100)
	if err != nil {
		t.Fatalf("ListPending(同优先级): %v", err)
	}
	if len(samePrio) != 6 || samePrio[0].Priority != 5 || samePrio[1].Priority != 5 {
		t.Fatalf("前两条应为优先级 5 的两条，实际 %d 条: %d/%d", len(samePrio), samePrio[0].Priority, samePrio[1].Priority)
	}
	if samePrio[0].ID != older.ID || samePrio[1].ID != newer.ID {
		t.Errorf("COALESCE 排序失真（ASC：等待最久在前）：期望 %s（回退 created_at，更旧）在前、%s（有 last_message_at，较新）在后，实际 %s/%s",
			older.SessionID, newer.SessionID, samePrio[0].SessionID, samePrio[1].SessionID)
	}

	count, err := repo.CountByStatuses(ctx, pendingStatuses())
	if err != nil {
		t.Fatalf("CountByStatuses: %v", err)
	}
	if count != 6 {
		t.Errorf("计数应与列表同口径（6），实际 %d", count)
	}

	// 分页：offset 生效，与全量列表同排序。
	page2, err := repo.ListPending(ctx, pendingStatuses(), 4, 100)
	if err != nil {
		t.Fatalf("ListPending(page2): %v", err)
	}
	if len(page2) != 2 || page2[0].ID != samePrio[4].ID {
		t.Errorf("offset 分页与全量列表不同口径: len=%d first=%s", len(page2), page2[0].SessionID)
	}
}

// 数据隔离口径：坐席只看「自己名下 + 未分配池」；计数与列表同谓词。
func TestCustomerSessionRepository_ListPendingForAgent_PoolAndOwnOnly(t *testing.T) {
	db := pendingPoolSetupDB(t)
	repo := NewCustomerSessionRepositoryWithDB(db)
	ctx := context.Background()

	mine := pendingSeed(t, db, tenant.DefaultTenantID, "mine", model.SessionStatusPending, 5, 1, -1*time.Hour, false)
	pendingSeed(t, db, tenant.DefaultTenantID, "other", model.SessionStatusPending, 9, 2, -1*time.Hour, false) // 他人名下
	pool := pendingSeed(t, db, tenant.DefaultTenantID, "pool", model.SessionStatusPending, 0, 3, -1*time.Hour, false)
	pendingSeed(t, db, tenant.DefaultTenantID, "w9", model.SessionStatusWaiting, 9, 4, -1*time.Hour, false)

	sessions, err := repo.ListPendingForAgent(ctx, pendingStatuses(), 5, 0, 100)
	if err != nil {
		t.Fatalf("ListPendingForAgent: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("坐席 5 应看到自己名下 + 未分配池共 2 条，实际 %d", len(sessions))
	}
	ids := map[uint]bool{}
	for _, s := range sessions {
		ids[s.ID] = true
	}
	if !ids[mine.ID] || !ids[pool.ID] {
		t.Errorf("应含自己名下与未分配池，实际 ids=%v", ids)
	}

	count, err := repo.CountByStatusesForAgent(ctx, pendingStatuses(), 5)
	if err != nil {
		t.Fatalf("CountByStatusesForAgent: %v", err)
	}
	if count != 2 {
		t.Errorf("隔离计数应与隔离列表同口径（2），实际 %d", count)
	}

	adminCount, err := repo.CountByStatuses(ctx, pendingStatuses())
	if err != nil {
		t.Fatalf("CountByStatuses: %v", err)
	}
	if adminCount != 4 {
		t.Errorf("管理员口径应见全部 4 条，实际 %d", adminCount)
	}
}

// 租户对照：on 模式下待处理池读口不跨租户（隔离 callback 作用于同一条仓储句柄）。
func TestCustomerSessionRepository_PendingPool_TenantIsolated(t *testing.T) {
	db := pendingPoolSetupDB(t)
	tenantA, tenantB := uint(81), uint(82)
	pendingSeed(t, db, tenantA, "ta", model.SessionStatusPending, 0, 1, -1*time.Hour, false)
	pendingSeed(t, db, tenantB, "tb", model.SessionStatusPending, 0, 1, -1*time.Hour, false)

	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() {
		tenant.ResetRegistryForTest()
		tenant.SetMode(tenant.ModeOff)
	})

	repo := NewCustomerSessionRepositoryWithDB(db)
	ctxA := tenant.WithTenantID(context.Background(), tenantA)

	aRows, err := repo.ListPending(ctxA, pendingStatuses(), 0, 100)
	if err != nil {
		t.Fatalf("ListPending(ctxA): %v", err)
	}
	if len(aRows) != 1 {
		t.Fatalf("租户 A 应只见自己 1 条，实际 %d", len(aRows))
	}
	for _, r := range aRows {
		if r.TenantID != tenantA {
			t.Errorf("结果混入他租户行: tenant_id=%d", r.TenantID)
		}
	}

	aCount, err := repo.CountByStatusesForAgent(ctxA, pendingStatuses(), 5)
	if err != nil {
		t.Fatalf("CountByStatusesForAgent(ctxA): %v", err)
	}
	if aCount != 1 {
		t.Errorf("租户 A 隔离计数应为 1，实际 %d", aCount)
	}
}

// 送达回写：只动 delivered_at（单字段白名单），其余字段不被顺带改写。
func TestSessionMessageRepository_MarkMessageDelivered(t *testing.T) {
	db := pendingPoolSetupDB(t)
	repo := NewSessionMessageRepositoryWithDB(db)
	ctx := context.Background()

	msg := &model.SessionMessage{
		TenantID:   tenant.DefaultTenantID,
		SessionID:  "sess_deliver_marker",
		Content:    "hello",
		SenderType: "agent",
	}
	if err := db.Create(msg).Error; err != nil {
		t.Fatalf("预置消息失败: %v", err)
	}

	at := time.Now()
	if err := repo.MarkMessageDelivered(ctx, msg.ID, at); err != nil {
		t.Fatalf("MarkMessageDelivered: %v", err)
	}

	var got model.SessionMessage
	if err := db.First(&got, msg.ID).Error; err != nil {
		t.Fatalf("回读消息失败: %v", err)
	}
	if got.DeliveredAt == nil {
		t.Fatal("delivered_at 应被写入")
	}
	if !got.DeliveredAt.Truncate(time.Second).Equal(at.Truncate(time.Second)) {
		t.Errorf("delivered_at 应为回写时刻：期望 %v，实际 %v", at, got.DeliveredAt)
	}
	if got.Content != "hello" || got.SenderType != "agent" {
		t.Errorf("送达回写不应改写其他字段: content=%q sender=%q", got.Content, got.SenderType)
	}

	// 不存在的消息：0 行不报错（与原 Update 语义一致，错误由调用方丢弃）。
	if err := repo.MarkMessageDelivered(ctx, 999999, at); err != nil {
		t.Errorf("对不存在消息的送达回写不应报错（原语义 0 行静默）: %v", err)
	}
}
