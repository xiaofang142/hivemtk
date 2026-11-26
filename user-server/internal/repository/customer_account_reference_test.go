package repository

import (
	"context"
	"reflect"
	"strconv"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/edition"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

// CRM 来源引用只能归属唯一现存账号；权威码、在途旧码和冗余码冲突均不能任选一条。
func TestCRMAccountSourceReferences_UniqueCurrentPreviousAndExpired(t *testing.T) {
	db := testutil.NewTestDB(t, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{})
	ctx := edition.WithContext(tenant.WithSystemScope(context.Background()), edition.TypeCustom)
	accounts := []model.ChannelAccount{
		{ID: 63001, TenantID: 63, ChannelID: 631, IdentifyCode: "expired", DataScope: "{}"},
		{ID: 63002, TenantID: 63, ChannelID: 632, IdentifyCode: "legacy-current", DataScope: "{}"},
		{ID: 63003, TenantID: 63, ChannelID: 633, IdentifyCode: "unused-3", DataScope: "{}"},
		{ID: 63004, TenantID: 63, ChannelID: 633, IdentifyCode: "unused-4", DataScope: "{}"},
		{ID: 63005, TenantID: 64, ChannelID: 634, IdentifyCode: "collision", DataScope: "{}"},
		{ID: 63006, TenantID: 63, ChannelID: 635, IdentifyCode: "unused-6", DataScope: "{}"},
	}
	past, future := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	codes := []model.ChannelIdentifyCode{
		{TenantID: 63, ChannelID: 631, Code: "current", PrevCode: "expired", PrevValidUntil: &past},
		{TenantID: 63, ChannelID: 633, Code: "ambiguous-channel"},
		{TenantID: 63, ChannelID: 635, Code: "collision", PrevCode: "in-flight", PrevValidUntil: &future},
	}
	for _, id := range []uint{631, 632, 633, 634, 635} {
		if err := db.WithContext(ctx).Create(&model.Channel{ID: id, Code: "crm-reference-" + strconv.FormatUint(uint64(id), 10), Name: "CRM", Type: "web"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.WithContext(ctx).Create(&accounts).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Create(&codes).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewChannelAccountRepositoryWithDB(db).(CRMAccountReferenceReader)
	refs, err := repo.GetCRMSourceReferences(ctx, 63, []uint{63001, 63002, 63003, 63004, 63005, 63006, 63999})
	if err != nil {
		t.Fatal(err)
	}
	want := map[uint][]string{
		63001: {"63001", "current"}, 63002: {"63002", "legacy-current"},
		63003: {"63003"}, 63004: {"63004"}, 63006: {"63006", "in-flight"},
	}
	if !reflect.DeepEqual(refs, want) {
		t.Fatalf("来源应唯一归属且拒绝过期码/多账号渠道码/跨租户冲突/孤儿授权，期望=%v 实际=%v", want, refs)
	}
	// 重置码后重新解析：旧引用立即失效，ACL 数字主键保持不变。
	if err := db.WithContext(ctx).Model(&model.ChannelIdentifyCode{}).Where("channel_id = ?", 631).
		Updates(map[string]any{"code": "rotated", "prev_code": "current", "prev_valid_until": past}).Error; err != nil {
		t.Fatal(err)
	}
	refs, err = repo.GetCRMSourceReferences(ctx, 63, []uint{63001})
	if err != nil || !reflect.DeepEqual(refs[63001], []string{"63001", "rotated"}) {
		t.Fatalf("重置后只能保留现行引用，实际=%v err=%v", refs, err)
	}
}

// 权威渠道即使尚无账号行，也不能将其现行/在途码回退给别的账号的冗余字段。
func TestCRMAccountSourceReferences_OrphanAuthorityBlocksLegacyAlias(t *testing.T) {
	for _, tc := range []struct {
		name       string
		tenantID   uint
		isPrevious bool
	}{
		{name: "same-tenant-current", tenantID: 63},
		{name: "cross-tenant-current", tenantID: 64},
		{name: "same-tenant-previous", tenantID: 63, isPrevious: true},
		{name: "cross-tenant-previous", tenantID: 64, isPrevious: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := testutil.NewTestDB(t, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{})
			ctx := edition.WithContext(tenant.WithSystemScope(context.Background()), edition.TypeCustom)
			channels := []model.Channel{
				{ID: 631, TenantID: 63, Code: "legacy-channel", Name: "CRM", Type: "web"},
				{ID: 632, TenantID: tc.tenantID, Code: "authority-channel", Name: "CRM", Type: "web"},
			}
			account := model.ChannelAccount{ID: 63001, TenantID: 63, ChannelID: 631, IdentifyCode: "claimed-code", DataScope: "{}"}
			authority := model.ChannelIdentifyCode{TenantID: tc.tenantID, ChannelID: 632, Code: "claimed-code"}
			if tc.isPrevious {
				future := time.Now().Add(time.Hour)
				authority.Code, authority.PrevCode, authority.PrevValidUntil = "new-code", "claimed-code", &future
			}
			for _, rows := range []any{&channels, &account, &authority} {
				if err := db.WithContext(ctx).Create(rows).Error; err != nil {
					t.Fatal(err)
				}
			}
			repo := NewChannelAccountRepositoryWithDB(db).(CRMAccountReferenceReader)
			refs, err := repo.GetCRMSourceReferences(ctx, 63, []uint{63001})
			if err != nil || !reflect.DeepEqual(refs[63001], []string{"63001"}) {
				t.Fatalf("无账号的权威归属必须阻断其他账号旧别名，实际=%v err=%v", refs, err)
			}
		})
	}
}
