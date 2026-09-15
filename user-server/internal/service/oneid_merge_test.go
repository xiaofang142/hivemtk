package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

func setupMergeTestDB(t *testing.T) repository.CustomerRepository {
	t.Helper()
	// 以下模型全部为 MergeCustomers 事务实际改写的表，缺一即整笔事务回滚：
	//   customers / customer_sessions / customer_events（主档与事件）
	//   customer_channels / customer_do_not_contact / csat_surveys / clues /
	//   script_exposure_logs（ReassignOneID 家族，见 service/customer.go:346-383）
	//   operation_logs（合并审计日志）
	database := testutil.NewTestDB(t,
		&model.Customer{},
		&model.CustomerSession{},
		&model.CustomerEvent{},
		&model.CustomerChannel{},
		&model.CustomerDoNotContact{},
		&model.CSATSurvey{},
		&model.Clue{},
		&model.ScriptExposureLog{},
	)
	db.SetTestDB(database)
	repo := repository.NewCustomerRepository()
	return repo
}

// TestMergeCustomers_KeepsPrimaryUnifiedID 回归：合并严禁重算主档案 OneID（核心不变量）
func TestMergeCustomers_KeepsPrimaryUnifiedID(t *testing.T) {
	repo := setupMergeTestDB(t)
	ctx := context.Background()

	primary := &model.Customer{
		Phone:     "13800138000",
		Email:     "primary@example.com",
		Tags:      "[]",
		ChurnRisk: "low",
	}
	if err := repo.Create(ctx, primary); err != nil {
		t.Fatalf("create primary: %v", err)
	}
	primaryOneID := primary.UnifiedID
	if primaryOneID == "" {
		t.Fatal("primary UnifiedID 为空")
	}

	secondary := &model.Customer{
		Phone:         "13900139000",
		XiaohongshuID: "xhs_sec_123",
		Tags:          "[]",
		ChurnRisk:     "low",
	}
	if err := repo.Create(ctx, secondary); err != nil {
		t.Fatalf("create secondary: %v", err)
	}

	svc := NewCustomerService()
	if err := svc.MergeCustomers(ctx, primary.ID, secondary.ID); err != nil {
		t.Fatalf("MergeCustomers: %v", err)
	}

	got, err := repo.GetByID(ctx, primary.ID)
	if err != nil {
		t.Fatalf("get primary after merge: %v", err)
	}
	if got.UnifiedID != primaryOneID {
		t.Fatalf("主档案 UnifiedID 在合并后被改变: 期望 %s 实际 %s（违反 OneID 永不重算铁律）", primaryOneID, got.UnifiedID)
	}

	var remain int64
	if err := db.GetDB().WithContext(ctx).Model(&model.Customer{}).Where("id = ?", secondary.ID).Count(&remain).Error; err != nil {
		t.Fatalf("count secondary: %v", err)
	}
	if remain != 0 {
		t.Fatalf("期望副档案被物理删除，但仍查到 %d 条（id=%s）", remain, secondary.ID)
	}

	if got.XiaohongshuID != "xhs_sec_123" {
		t.Fatalf("副档案小红书 ID 未回填到主档案: %s", got.XiaohongshuID)
	}
}

// TestMergeCustomers_SessionReassign 回归：合并后会话按 OneID 聚合到主档案
func TestMergeCustomers_SessionReassign(t *testing.T) {
	repo := setupMergeTestDB(t)
	ctx := context.Background()

	primary := &model.Customer{Phone: "13800138001", Tags: "[]", ChurnRisk: "low"}
	if err := repo.Create(ctx, primary); err != nil {
		t.Fatalf("create primary: %v", err)
	}
	secondary := &model.Customer{Phone: "13900139001", Tags: "[]", ChurnRisk: "low"}
	if err := repo.Create(ctx, secondary); err != nil {
		t.Fatalf("create secondary: %v", err)
	}

	sess := &model.CustomerSession{OneID: secondary.UnifiedID, UserID: "u_sec"}
	if err := db.GetDB().WithContext(ctx).Create(sess).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}

	svc := NewCustomerService()
	if err := svc.MergeCustomers(ctx, primary.ID, secondary.ID); err != nil {
		t.Fatalf("MergeCustomers: %v", err)
	}

	var cnt int64
	if err := db.GetDB().WithContext(ctx).Model(&model.CustomerSession{}).
		Where("one_id = ?", primary.UnifiedID).Count(&cnt).Error; err != nil {
		t.Fatalf("count session: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("期望 1 条会话聚合到主档案 OneID，实际 %d", cnt)
	}
}

// TestFindByIdentity_Xiaohongshu 回归：FindByIdentity 应能通过小红书 ID 识别客户
func TestFindByIdentity_Xiaohongshu(t *testing.T) {
	repo := setupMergeTestDB(t)
	ctx := context.Background()

	want := &model.Customer{XiaohongshuID: "xhs_find_456", Tags: "[]", ChurnRisk: "low"}
	if err := repo.Create(ctx, want); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := repo.FindByIdentity(ctx, "", "", "", "", "xhs_find_456")
	if err != nil {
		t.Fatalf("FindByIdentity(xhs): %v", err)
	}
	if got == nil || got.ID != want.ID {
		t.Fatalf("小红书 ID 未被 FindByIdentity 识别")
	}
}
