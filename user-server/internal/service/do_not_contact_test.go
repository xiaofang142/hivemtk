package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

func setupDoNotContactServiceTestDB(t *testing.T) (*gorm.DB, *DoNotContactService) {
	database := testutil.NewTestDB(t,
		&model.CustomerDoNotContact{},
		&model.SmsUnsubscribe{},
	)
	repo := repository.NewCustomerDoNotContactRepository(database)
	svc := NewDoNotContactService(repo)
	return database, svc
}

// TestDoNotContact_IsBlocked_ThreeStates IsBlocked 三态：未命中 / 精确渠道 / 全局
func TestDoNotContact_IsBlocked_ThreeStates(t *testing.T) {
	db, svc := setupDoNotContactServiceTestDB(t)
	ctx := context.Background()

	if svc.IsBlocked(ctx, "one-a", "sms") {
		t.Error("Expected IsBlocked=false before any block")
	}

	if err := svc.Block(ctx, "one-a", "", model.DNCSourceManual); err != nil {
		t.Fatalf("Block global 失败: %v", err)
	}
	for _, ch := range []string{"sms", "email", "telegram"} {
		if !svc.IsBlocked(ctx, "one-a", ch) {
			t.Errorf("Expected IsBlocked=true on channel %s via global row", ch)
		}
	}

	if err := svc.Block(ctx, "one-b", "email", model.DNCSourceWebhook); err != nil {
		t.Fatalf("Block email 失败: %v", err)
	}
	if !svc.IsBlocked(ctx, "one-b", "email") {
		t.Error("Expected IsBlocked=true for exact channel row")
	}
	if svc.IsBlocked(ctx, "one-b", "sms") {
		t.Error("Expected IsBlocked=false on non-blocked channel")
	}
	if svc.IsBlocked(ctx, "one-c", "sms") {
		t.Error("Expected IsBlocked=false for unknown one_id")
	}

	_ = db
}

// TestDoNotContact_Block_Idempotent Block 幂等：重复写入仅一行
func TestDoNotContact_Block_Idempotent(t *testing.T) {
	db, svc := setupDoNotContactServiceTestDB(t)
	ctx := context.Background()

	if err := svc.Block(ctx, "one-idem", "", model.DNCSourceSMSKeyword); err != nil {
		t.Fatalf("首次 Block 失败: %v", err)
	}
	if err := svc.Block(ctx, "one-idem", "", model.DNCSourceManual); err != nil {
		t.Fatalf("重复 Block 失败: %v", err)
	}

	var count int64
	db.Model(&model.CustomerDoNotContact{}).Where("one_id = ?", "one-idem").Count(&count)
	if count != 1 {
		t.Errorf("Expected 1 row (idempotent), got %d", count)
	}
}

// TestDoNotContact_Unblock Unblock 后恢复放行
func TestDoNotContact_Unblock(t *testing.T) {
	_, svc := setupDoNotContactServiceTestDB(t)
	ctx := context.Background()

	if err := svc.Block(ctx, "one-unblock", "sms", model.DNCSourceManual); err != nil {
		t.Fatalf("Block 失败: %v", err)
	}
	if !svc.IsBlocked(ctx, "one-unblock", "sms") {
		t.Fatal("Expected blocked after Block")
	}

	if err := svc.Unblock(ctx, "one-unblock", "sms"); err != nil {
		t.Fatalf("Unblock 失败: %v", err)
	}
	if svc.IsBlocked(ctx, "one-unblock", "sms") {
		t.Error("Expected not blocked after Unblock")
	}
}

// TestDoNotContact_BlockFromPhone_FallbackKey 无法反查 one_id 时用 "phone:"+phone 归一键
func TestDoNotContact_BlockFromPhone_FallbackKey(t *testing.T) {
	db, svc := setupDoNotContactServiceTestDB(t)
	ctx := context.Background()

	svc.SetPhoneResolver(func(ctx context.Context, phone string) string { return "" })

	if err := svc.BlockFromPhone(ctx, "+86 138-0000-1111", model.DNCSourceSMSKeyword); err != nil {
		t.Fatalf("BlockFromPhone 失败: %v", err)
	}

	var rec model.CustomerDoNotContact
	if err := db.Where("one_id = ?", PhoneOneIDPrefix+"13800001111").First(&rec).Error; err != nil {
		t.Fatalf("Expected fallback key row: %v", err)
	}
	if rec.Channel != "" || rec.Source != model.DNCSourceSMSKeyword {
		t.Errorf("Unexpected row: channel=%q source=%q", rec.Channel, rec.Source)
	}

	if !svc.IsBlocked(ctx, PhoneOneIDPrefix+"13800001111", "sms") {
		t.Error("Expected fallback-key one_id blocked on sms")
	}
}

// TestDoNotContact_BlockFromPhone_ResolveOneID 能反查时写真实 OneID
func TestDoNotContact_BlockFromPhone_ResolveOneID(t *testing.T) {
	db, svc := setupDoNotContactServiceTestDB(t)
	ctx := context.Background()

	svc.SetPhoneResolver(func(ctx context.Context, phone string) string { return "unified-real" })

	if err := svc.BlockFromPhone(ctx, "13900002222", model.DNCSourceSMSKeyword); err != nil {
		t.Fatalf("BlockFromPhone 失败: %v", err)
	}

	var count int64
	db.Model(&model.CustomerDoNotContact{}).Where("one_id = ?", "unified-real").Count(&count)
	if count != 1 {
		t.Errorf("Expected 1 row with real one_id, got %d", count)
	}
}

// TestDoNotContact_BackfillFromSMSUnsubscribe 回填幂等 + phone→one_id 反查
func TestDoNotContact_BackfillFromSMSUnsubscribe(t *testing.T) {
	db, svc := setupDoNotContactServiceTestDB(t)
	ctx := context.Background()

	seeds := []model.SmsUnsubscribe{
		{Phone: "13800138001", Reason: "r1"},
		{Phone: "13800138002", Reason: "r2"},
		{Phone: "13800138003", Reason: "r3"},
	}
	for i := range seeds {
		if err := db.Create(&seeds[i]).Error; err != nil {
			t.Fatalf("seed sms_unsubscribe 失败: %v", err)
		}
	}

	svc.SetSmsUnsubscribeRepo(repository.NewSmsUnsubscribeRepository(db))
	svc.SetPhoneResolver(func(ctx context.Context, phone string) string {
		if phone == "13800138001" {
			return "unified-x"
		}
		return ""
	})

	added, err := svc.BackfillFromSMSUnsubscribe(ctx)
	if err != nil {
		t.Fatalf("Backfill 失败: %v", err)
	}
	if added != 3 {
		t.Errorf("Expected 3 rows added on first backfill, got %d", added)
	}

	var count int64
	db.Model(&model.CustomerDoNotContact{}).Count(&count)
	if count != 3 {
		t.Errorf("Expected 3 dnc rows, got %d", count)
	}

	var unifiedCount, fallbackCount int64
	db.Model(&model.CustomerDoNotContact{}).Where("one_id = ?", "unified-x").Count(&unifiedCount)
	db.Model(&model.CustomerDoNotContact{}).Where("one_id LIKE ?", PhoneOneIDPrefix+"%").Count(&fallbackCount)
	if unifiedCount != 1 {
		t.Errorf("Expected 1 real-one_id row, got %d", unifiedCount)
	}
	if fallbackCount != 2 {
		t.Errorf("Expected 2 fallback-key rows, got %d", fallbackCount)
	}

	added2, err := svc.BackfillFromSMSUnsubscribe(ctx)
	if err != nil {
		t.Fatalf("二次 Backfill 失败: %v", err)
	}
	if added2 != 0 {
		t.Errorf("Expected 0 rows on second (idempotent) backfill, got %d", added2)
	}
	db.Model(&model.CustomerDoNotContact{}).Count(&count)
	if count != 3 {
		t.Errorf("Expected still 3 dnc rows after re-backfill, got %d", count)
	}
}

// TestDoNotContact_SmsUnsubscribe_Integration 短信退订成功后自动同步全局表
func TestDoNotContact_SmsUnsubscribe_Integration(t *testing.T) {
	database := testutil.NewTestDB(t,
		&model.CustomerDoNotContact{},
		&model.SmsUnsubscribe{},
	)

	smsRepo := repository.NewSmsUnsubscribeRepository(database)
	smsSvc := NewSmsUnsubscribeService(smsRepo)
	dncSvc := NewDoNotContactService(repository.NewCustomerDoNotContactRepository(database))
	dncSvc.SetPhoneResolver(func(ctx context.Context, phone string) string { return "" })
	smsSvc.SetDoNotContact(dncSvc)

	ctx := context.Background()
	phone := "13700003000"
	if err := smsSvc.UnsubscribePhone(ctx, phone, "用户回复TD", "msg-1", "TD"); err != nil {
		t.Fatalf("UnsubscribePhone 失败: %v", err)
	}

	key := PhoneOneIDPrefix + phone
	if !dncSvc.IsBlocked(ctx, key, "sms") {
		t.Error("Expected sms channel blocked via global table after UnsubscribePhone")
	}
	var rec model.CustomerDoNotContact
	if err := database.Where("one_id = ?", key).First(&rec).Error; err != nil {
		t.Fatalf("查询全局退订行失败: %v", err)
	}
	if rec.Source != model.DNCSourceSMSKeyword {
		t.Errorf("Expected source=sms_keyword, got %s", rec.Source)
	}

	if err := smsSvc.UnsubscribePhone(ctx, phone, "再次退订", "msg-2", "退订"); err != nil {
		t.Fatalf("二次 UnsubscribePhone 失败: %v", err)
	}
	var count int64
	database.Model(&model.CustomerDoNotContact{}).Where("one_id = ?", key).Count(&count)
	if count != 1 {
		t.Errorf("Expected 1 global row (idempotent), got %d", count)
	}
}
