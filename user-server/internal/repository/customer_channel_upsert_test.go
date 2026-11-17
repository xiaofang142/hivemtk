package repository

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func newCCRepo(t *testing.T) (*ChannelOverviewRepository, *gorm.DB) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.CustomerChannel{}, &model.OperationLog{})
	return NewChannelOverviewRepository(db), db
}

// TestCustomerChannel_SameOneIDTwoAccounts 同一 OneID 在两个平台账号下各自独立成行。
func TestCustomerChannel_SameOneIDTwoAccounts(t *testing.T) {
	repo, db := newCCRepo(t)
	ctx := context.Background()

	for _, acc := range []string{"acc-1", "acc-2"} {
		err := repo.UpsertCustomerChannel(ctx, &model.CustomerChannel{
			TenantID: 1, OneID: "phone:123", Channel: "telegram",
			AccountID: acc, ChannelUserID: "u-" + acc,
		})
		if err != nil {
			t.Fatalf("%s upsert: %v", acc, err)
		}
	}
	var n int64
	db.Model(&model.CustomerChannel{}).Where("one_id = ?", "phone:123").Count(&n)
	if n != 2 {
		t.Fatalf("两账号应并存 2 行，实际 %d", n)
	}
}

// TestCustomerChannel_UpsertIdempotent 同键重复写入不新增行。
func TestCustomerChannel_UpsertIdempotent(t *testing.T) {
	repo, db := newCCRepo(t)
	ctx := context.Background()

	cc := func(name string) *model.CustomerChannel {
		return &model.CustomerChannel{
			TenantID: 1, OneID: "phone:9", Channel: "feishu",
			AccountID: "acc", ChannelUserID: "u9", ChannelName: name,
		}
	}
	if err := repo.UpsertCustomerChannel(ctx, cc("first")); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := repo.UpsertCustomerChannel(ctx, cc("second")); err != nil {
		t.Fatalf("second: %v", err)
	}
	var rows []model.CustomerChannel
	db.Where("one_id = ?", "phone:9").Find(&rows)
	if len(rows) != 1 {
		t.Fatalf("重复 upsert 应只有 1 行，实际 %d", len(rows))
	}
	if rows[0].ChannelName != "second" {
		t.Fatalf("重复 upsert 应刷新字段，实际 %q", rows[0].ChannelName)
	}
}

// TestCustomerChannel_EmptyAccountNormalized account_id 空白必须归一为 ”，否则身份锚查不到。
func TestCustomerChannel_EmptyAccountNormalized(t *testing.T) {
	repo, db := newCCRepo(t)
	ctx := context.Background()

	if err := repo.UpsertCustomerChannel(ctx, &model.CustomerChannel{
		TenantID: 1, OneID: "phone:7", Channel: "wecom", AccountID: "  ", ChannelUserID: "u7",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	var row model.CustomerChannel
	if err := db.Where("one_id = ?", "phone:7").First(&row).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if row.AccountID != "" {
		t.Fatalf("account_id 应归一为 ''，实际 %q", row.AccountID)
	}
}

// TestCustomerChannel_IdentityAnchorWinsOverNewOneID 身份锚命中时沿用旧 one_id，
// 新 one_id 只记账不覆盖（覆盖会把历史消息挪到另一个客户名下）。
func TestCustomerChannel_IdentityAnchorWinsOverNewOneID(t *testing.T) {
	repo, db := newCCRepo(t)
	ctx := context.Background()

	if err := repo.UpsertCustomerChannel(ctx, &model.CustomerChannel{
		TenantID: 1, OneID: "phone:old", Channel: "telegram", AccountID: "acc", ChannelUserID: "same-user",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := repo.UpsertCustomerChannel(ctx, &model.CustomerChannel{
		TenantID: 1, OneID: "phone:new", Channel: "telegram", AccountID: "acc", ChannelUserID: "same-user",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	var rows []model.CustomerChannel
	db.Where("channel_user_id = ?", "same-user").Find(&rows)
	if len(rows) != 1 {
		t.Fatalf("同一平台账号不得写出第二行，实际 %d", len(rows))
	}
	if rows[0].OneID != "phone:old" {
		t.Fatalf("应沿用旧 one_id，实际 %q", rows[0].OneID)
	}
	var logs int64
	db.Model(&model.OperationLog{}).Where("action = ?", "customer_channel.identity_conflict").Count(&logs)
	if logs != 1 {
		t.Fatalf("冲突必须记一条 operation_log，实际 %d", logs)
	}
}

// TestCustomerChannel_UpsertRevivesSoftDeleted 软删行能被重新认领（复活），而不是插第二行。
func TestCustomerChannel_UpsertRevivesSoftDeleted(t *testing.T) {
	repo, db := newCCRepo(t)
	ctx := context.Background()

	if err := repo.UpsertCustomerChannel(ctx, &model.CustomerChannel{
		TenantID: 1, OneID: "phone:revive", Channel: "qq", AccountID: "acc", ChannelUserID: "u-revive",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.Where("channel_user_id = ?", "u-revive").Delete(&model.CustomerChannel{}).Error; err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	if err := repo.UpsertCustomerChannel(ctx, &model.CustomerChannel{
		TenantID: 1, OneID: "phone:revive", Channel: "qq", AccountID: "acc", ChannelUserID: "u-revive",
		ChannelName: "revived", LastSeenAt: time.Now(),
	}); err != nil {
		t.Fatalf("revive upsert: %v", err)
	}

	var rows []model.CustomerChannel
	db.Where("channel_user_id = ?", "u-revive").Find(&rows)
	if len(rows) != 1 {
		t.Fatalf("复活后应只有 1 行活行，实际 %d", len(rows))
	}
	if rows[0].ChannelName != "revived" {
		t.Fatalf("复活应刷新字段，实际 %q", rows[0].ChannelName)
	}
}
