package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

func TestBridgeLeadModule_DisabledTenantCannotStartWork(t *testing.T) {
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previous) })
	database := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &model.BridgeLeadTask{}, &model.TenantModule{})
	for _, id := range []uint{18, 19} {
		ctx := tenant.OnlyTenant(context.Background(), id)
		status := model.TenantModuleDisabled
		if id == 19 {
			status = model.TenantModuleActive
		}
		row := &model.TenantModule{TenantID: id, ModuleKey: model.ModuleCustomer, Status: status}
		if err := database.WithContext(ctx).Create(row).Error; err != nil {
			t.Fatal(err)
		}
		hub := &model.MessageHub{TenantID: id, Platform: "douyin", AccountID: "module-account", ConversationID: "module-conversation", MsgID: "module-inbound", Direction: "inbound", SenderID: "customer-a", Content: "采购咨询", BridgeLeadEligible: true, SentAt: time.Now()}
		if err := database.WithContext(ctx).Create(hub).Error; err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	worker := NewBridgeLeadWorker(repository.NewBridgeLeadTaskRepository(database), func(context.Context, *model.MessageHub) error { calls++; return nil })
	ctx18 := tenant.OnlyTenant(context.Background(), 18)
	if _, err := worker.RunTenant(ctx18); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := database.WithContext(ctx18).Model(&model.BridgeLeadTask{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 || calls != 0 {
		t.Fatalf("关闭模块不能补任务、认领或执行：任务%d，调用%d", count, calls)
	}
	if _, err := worker.RunTenant(tenant.OnlyTenant(context.Background(), 19)); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("另一个已开通租户必须保持正常执行")
	}
	if err := database.WithContext(ctx18).Model(&model.TenantModule{}).Where("module_key = ?", model.ModuleCustomer).Update("status", model.TenantModuleActive).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := worker.RunTenant(ctx18); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("重新开通后必须恢复尚未开始的持久任务")
	}
}

func TestBridgeLeadModule_UnconfirmedReadCannotStartWork(t *testing.T) {
	database := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &model.BridgeLeadTask{}, &model.TenantModule{})
	ctx := tenant.OnlyTenant(context.Background(), tenant.DefaultTenantID)
	hub := &model.MessageHub{TenantID: tenant.DefaultTenantID, Platform: "douyin", AccountID: "module-account", ConversationID: "module-conversation", MsgID: "module-fault", Direction: "inbound", SenderID: "customer-a", Content: "采购咨询", BridgeLeadEligible: true, SentAt: time.Now()}
	if err := database.WithContext(ctx).Create(hub).Error; err != nil {
		t.Fatal(err)
	}
	callback := "test:bridge-module-read-fault"
	if err := database.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "tenant_modules" {
			tx.AddError(errors.New("测试模块读取故障"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Callback().Query().Remove(callback) })
	calls := 0
	worker := NewBridgeLeadWorker(repository.NewBridgeLeadTaskRepository(database), func(context.Context, *model.MessageHub) error { calls++; return nil })
	if _, err := worker.RunTenant(ctx); err == nil {
		t.Error("模块读取故障必须显式拒绝新执行")
	}
	if calls != 0 {
		t.Error("不能在许可未知时调用矿工")
	}
}
