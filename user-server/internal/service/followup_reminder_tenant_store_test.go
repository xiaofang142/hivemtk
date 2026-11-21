package service

// followup_reminder_tenant_store_test.go FA-26/27：跟进提醒的租户授权矩阵与共享权威存储。
//
// 钉住的契约：
//  1. FA-26 授权：跨租户同 ownerID 隐藏存在性（读、完成、取消全口径）；
//     同租户不同负责人不可改写他人待办（完成/取消 403 语义），管理员豁免；
//     被拒操作零副作用（待办状态不变）。
//  2. FA-27 共享存储：实例 A（模拟 AI 工具链）创建的提醒，实例 B（HTTP 今日跟进）
//     同存储立即可见；"重启"（同存储新实例）后行仍在；完成状态跨实例一致。

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// newFollowupStoreSvc 三个实例共享同一测试库 = 模拟三条生产链
// （AI 工具 / HTTP 读侧 / 重启后的新实例）+ 同一权威存储。
func newFollowupStoreSvc(t *testing.T) (a, http, restarted *FollowUpService, db *gorm.DB) {
	t.Helper()
	db = testutil.NewTestDB(t, &model.FollowUpReminder{})
	store := repository.NewFollowUpReminderRepositoryWithDB(db)
	return NewFollowUpServiceWithStore(NewCustomerJourneyService(), store),
		NewFollowUpServiceWithStore(NewCustomerJourneyService(), store),
		NewFollowUpServiceWithStore(NewCustomerJourneyService(), store),
		db
}

func TestFollowUpTenantAndOwnerAuthorizationMatrix(t *testing.T) {
	svcA, svcHTTP, _, _ := newFollowupStoreSvc(t)
	ctxA := tenant.WithTenantID(context.Background(), 1)
	ctxB := tenant.WithTenantID(context.Background(), 2)

	// 租户 1 的销售 u1 排一条待办（模拟 AI 工具链实例创建）。
	r, err := svcA.Schedule(ctxA, "cust-1", "u1", ReminderQuoteFollowup, time.Hour, &ScheduleOptions{Title: "报价跟进"})
	if err != nil {
		t.Fatalf("排提醒失败：%v", err)
	}

	// 跨租户同 ownerID：读不可见、完成/取消=不存在（不泄露存在性）。
	if got := svcHTTP.ListPending(ctxB, "u1", 10); len(got) != 0 {
		t.Errorf("跨租户同 ownerID 不得读到他人租户待办，实际 %+v", got)
	}
	if err := svcHTTP.CompleteWithResultFor(ctxB, r.ID, "u1", false, FollowUpResultContacted, ""); err == nil ||
		!strings.Contains(err.Error(), "不存在") {
		t.Fatalf("跨租户完成必须报不存在（隐藏存在性），实际 %v", err)
	}
	if err := svcHTTP.CancelFor(ctxB, r.ID, "u1", false); err == nil ||
		!strings.Contains(err.Error(), "不存在") {
		t.Fatalf("跨租户取消必须报不存在，实际 %v", err)
	}

	// 同租户不同负责人：普通用户不可改写他人待办（403 语义）。
	if err := svcHTTP.CompleteWithResultFor(ctxA, r.ID, "u2", false, FollowUpResultContacted, ""); err == nil ||
		!errors.Is(err, ErrFollowUpForbidden) {
		t.Fatalf("同租户他人待办必须拒绝（403 语义），实际 %v", err)
	}

	// 被拒操作零副作用：待办仍是 pending（u2 的拒绝没有动状态）。
	list := svcHTTP.ListPending(ctxA, "u1", 10)
	if len(list) != 1 || list[0].ID != r.ID {
		t.Fatalf("被拒完成不得改写状态，实际 %v", list)
	}

	// 本人可完成。
	if err := svcHTTP.CompleteWithResultFor(ctxA, r.ID, "u1", false, FollowUpResultInterested, "聊好了"); err != nil {
		t.Fatalf("本人完成自己的待办应成功：%v", err)
	}
	// 重复完成被状态守卫拦下（A11 语义保持）。
	if err := svcHTTP.CompleteWithResultFor(ctxA, r.ID, "u1", false, FollowUpResultContacted, ""); err == nil ||
		!strings.Contains(err.Error(), "已处理") {
		t.Fatalf("重复完成必须被状态守卫拒绝，实际 %v", err)
	}

	// 管理员豁免：可完成他人待办（另排一条新待办验证，避免与上面状态守卫互相踩）。
	r2, err := svcA.Schedule(ctxA, "cust-2", "u1", ReminderReactivation, time.Hour, &ScheduleOptions{Title: "唤回"})
	if err != nil {
		t.Fatalf("排第二条提醒失败：%v", err)
	}
	if err := svcHTTP.CompleteWithResultFor(ctxA, r2.ID, "boss", true, FollowUpResultContacted, ""); err != nil {
		t.Fatalf("管理员可完成他人待办：%v", err)
	}
}

func TestFollowUpSharedStoreVisibleAcrossInstancesAndRestart(t *testing.T) {
	svcA, svcHTTP, svcRestarted, db := newFollowupStoreSvc(t)
	ctxA := tenant.WithTenantID(context.Background(), 1)

	if _, err := svcA.Schedule(ctxA, "cust-9", "u7", ReminderAfterSaleCare, time.Hour, &ScheduleOptions{Title: "回访"}); err != nil {
		t.Fatalf("排提醒失败：%v", err)
	}
	// 直接预置一条逾期行（业务读写一律走服务/仓储，预置数据允许测试直写）。
	due := time.Now().Add(-2 * time.Hour)
	overdueRow := &model.FollowUpReminder{ID: "rem_overdue_fa26", TenantID: 1, CustomerID: "cust-9", OwnerID: "u7",
		Type: "after_sale_care", Priority: 1, Title: "逾期回访", DueAt: due, CreatedAt: due, Status: "pending"}
	if err := db.Create(overdueRow).Error; err != nil {
		t.Fatalf("预置逾期行失败：%v", err)
	}

	// 实例 B（HTTP 读侧）立即可见（共享存储，非同进程内存）。
	pending := svcHTTP.ListPending(ctxA, "u7", 10)
	foundOverdue := false
	for _, r := range pending {
		if r.ID == "rem_overdue_fa26" {
			foundOverdue = true
		}
	}
	if !foundOverdue {
		t.Errorf("共享存储下实例 A 创建的提醒实例 B 必须立即可见，实际 %v", pending)
	}
	if got := svcHTTP.ListOverdue(ctxA, "u7"); len(got) != 1 || got[0].ID != "rem_overdue_fa26" {
		t.Errorf("逾期口径应只含预置逾期行，实际 %v", got)
	}

	// "重启"（全新实例、同一存储）：行仍在，取消状态跨实例一致。
	list := svcRestarted.ListPending(ctxA, "u7", 10)
	if len(list) != 2 {
		t.Fatalf("重启后行不得丢失，实际 %d 条", len(list))
	}
	if err := svcRestarted.CancelFor(ctxA, "rem_overdue_fa26", "u7", false); err != nil {
		t.Fatalf("重启实例取消应成功：%v", err)
	}
	after := svcHTTP.ListPending(ctxA, "u7", 10)
	if len(after) != 1 {
		t.Errorf("取消应跨实例一致：before=2 after=%d", len(after))
	}
}
