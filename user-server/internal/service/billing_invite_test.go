package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// billing_invite_test.go 计费组合出单 + 邀请码原子消耗的落库用例（PG，CI 强制）。

func newBillingSvcDB(t *testing.T) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.BillingPlan{}, &model.TeamSubscription{},
		&model.BillingRecord{}, &model.Team{}, &model.SystemUser{}, &model.InviteCode{})
	dbutil.SetTestDB(db)
	t.Cleanup(func() { dbutil.SetTestDB(nil) })
}

// TestBilling_PerSeatRecord 组合计费出单：per_seat 方案按"人头单价×当期成员数×月数"计，
// 与 flat 的"月价×月数"是两条独立口径。
func TestBilling_PerSeatRecord(t *testing.T) {
	newBillingSvcDB(t)
	ctx := context.Background()
	svc := NewBillingAdminService()
	if !svc.Ready() {
		t.Fatal("计费服务未就绪")
	}

	if err := dbutil.GetDB().Create(&model.Team{Name: "计费团队", Status: model.TeamStatusActive}).Error; err != nil {
		t.Fatalf("铺团队失败: %v", err)
	}
	// 3 名成员
	for i := 0; i < 3; i++ {
		if err := dbutil.GetDB().Create(&model.SystemUser{Username: "u" + string(rune('a'+i)), Role: model.SystemUserRoleUser, TeamID: 1, Enabled: true}).Error; err != nil {
			t.Fatalf("铺成员失败: %v", err)
		}
	}
	plan := &model.BillingPlan{Code: "seat-test", Name: "按人头测试", PriceMode: "per_seat", PerSeatCents: 1000, Status: model.BillingPlanStatusActive}
	if err := dbutil.GetDB().Create(plan).Error; err != nil {
		t.Fatalf("铺方案失败: %v", err)
	}

	rec, err := svc.CreateRecord(ctx, 1, plan.ID, 2, 0, "")
	if err != nil {
		t.Fatalf("出单失败: %v", err)
	}
	// 1000 分/人 × 3 人 × 2 月 = 6000 分
	if rec.AmountCents != 6000 {
		t.Errorf("per_seat 应按 人头×成员数×月数 计，got %d want 6000", rec.AmountCents)
	}
	if rec.Status != model.BillingStatusPending {
		t.Errorf("新账单应为应收，got %s", rec.Status)
	}

	// 标记支付
	if err := svc.MarkRecordPaid(ctx, rec.ID); err != nil {
		t.Fatalf("标记支付失败: %v", err)
	}
	rows, total, err := svc.ListRecords(ctx, 1, model.BillingStatusPaid, 1, 10)
	if err != nil || total != 1 {
		t.Errorf("已支付筛选应恰 1 条，got %d err=%v", total, err)
	}
	_ = rows
}

// TestInviteCode_ConsumeAtomic 邀请码三判（状态/次数/过期）+ 原子扣次：
// max_uses=1 的码第二次消耗必须失败——注册并发抢码不加这个闸就是批量灌号口子。
func TestInviteCode_ConsumeAtomic(t *testing.T) {
	newBillingSvcDB(t)
	ctx := context.Background()
	repo := repository.NewInviteCodeRepo()

	inv, err := repo.Generate(ctx, 1, 1, nil, nil)
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if _, err := repo.Consume(ctx, inv.Code); err != nil {
		t.Fatalf("首次消耗应成功: %v", err)
	}
	if _, err := repo.Consume(ctx, inv.Code); err == nil {
		t.Error("次数用尽的码第二次消耗必须失败")
	}

	// 过期码
	past := time.Now().Add(-time.Hour)
	expired, err := repo.Generate(ctx, 1, 0, &past, nil)
	if err != nil {
		t.Fatalf("生成过期码失败: %v", err)
	}
	if _, err := repo.Consume(ctx, expired.Code); err == nil {
		t.Error("过期码消耗必须失败")
	}

	// 停用码
	dis, err := repo.Generate(ctx, 1, 0, nil, nil)
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if err := repo.Disable(ctx, dis.ID); err != nil {
		t.Fatalf("停用失败: %v", err)
	}
	if _, err := repo.Consume(ctx, dis.Code); err == nil {
		t.Error("停用码消耗必须失败")
	}
}
