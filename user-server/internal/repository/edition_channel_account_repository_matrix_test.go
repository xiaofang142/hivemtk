package repository

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/edition"
	"hivemtk-user/internal/pkg/testutil"
)

func TestEditionChannelAccountRepositoryMatrix(t *testing.T) {
	database := testutil.NewTestDB(t, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{})
	channelRepo := NewChannelRepositoryWithDB(database)
	accountRepo := NewChannelAccountRepositoryWithDB(database)
	codeRepo := NewChannelIdentifyCodeRepositoryWithDB(database)

	domestic := model.Channel{TenantID: 1, Name: "抖音", Code: "douyin", Type: "dm", Enabled: true}
	foreign := model.Channel{TenantID: 1, Name: "Telegram", Code: "telegram", Type: "group", Enabled: true}
	authorityDomestic := model.Channel{TenantID: 1, Name: "企业微信", Code: "wecom", Type: "group", Enabled: true}
	for _, row := range []*model.Channel{&domestic, &foreign, &authorityDomestic} {
		if err := channelRepo.Create(context.Background(), row); err != nil {
			t.Fatalf("seed channel: %v", err)
		}
	}
	accounts := []model.ChannelAccount{
		{TenantID: 1, ChannelID: domestic.ID, Name: "domestic", IdentifyCode: "edition-domestic-code", Status: 1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{}},
		{TenantID: 1, ChannelID: foreign.ID, Name: "foreign", IdentifyCode: "edition-foreign-code", Status: 1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{}},
		{TenantID: 1, ChannelID: authorityDomestic.ID, Name: "domestic-authority", IdentifyCode: "edition-domestic-authority-account", Status: 1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{}},
		{TenantID: 2, ChannelID: foreign.ID, Name: "other-tenant", IdentifyCode: "edition-other-tenant-code", Status: 1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{}},
	}
	orphan := model.ChannelAccount{TenantID: 1, ChannelID: 999999, Name: "legacy-orphan", IdentifyCode: "edition-orphan-code", Status: 1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{}}
	if err := database.Create(&orphan).Error; err != nil {
		t.Fatalf("seed legacy orphan: %v", err)
	}
	seedCtx := edition.WithContext(context.Background(), edition.TypeCustom)
	for i := range accounts {
		if err := accountRepo.Create(seedCtx, &accounts[i]); err != nil {
			t.Fatalf("seed account %d: %v", i, err)
		}
	}
	if err := codeRepo.Upsert(context.Background(), &model.ChannelIdentifyCode{
		TenantID: 1, ChannelID: authorityDomestic.ID, Code: "edition-domestic-authority-code", Version: 1,
	}); err != nil {
		t.Fatalf("seed authority code: %v", err)
	}

	for _, tc := range []struct {
		name    string
		version string
		allow   bool
	}{
		{name: "opensource", version: edition.TypeOpensource, allow: false},
		{name: "custom", version: edition.TypeCustom, allow: true},
		{name: "unknown", version: "bogus", allow: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := edition.WithContext(context.Background(), tc.version)
			got, err := accountRepo.GetByID(ctx, 1, accounts[0].ID)
			if err != nil || (got != nil) != tc.allow {
				t.Errorf("GetByID domestic: got=%+v err=%v allow=%v", got, err, tc.allow)
			}
			orphanGot, err := accountRepo.GetByID(ctx, 1, orphan.ID)
			if err != nil || (orphanGot != nil) != tc.allow {
				t.Errorf("GetByID orphan: got=%+v err=%v allow=%v", orphanGot, err, tc.allow)
			}
			rows, err := accountRepo.List(ctx, ChannelAccountListOptions{TenantID: 1})
			if err != nil {
				t.Errorf("List error: %v", err)
			} else {
				foundDomestic, foundForeign := false, false
				for _, row := range rows {
					foundDomestic = foundDomestic || row.ID == accounts[0].ID || row.ID == accounts[2].ID
					foundForeign = foundForeign || row.ID == accounts[1].ID
				}
				if foundDomestic != tc.allow || !foundForeign {
					t.Errorf("List gate wrong: rows=%+v allowDomestic=%v", rows, tc.allow)
				}
			}
			byIDs, err := accountRepo.GetByIDs(ctx, 1, []uint{accounts[0].ID, accounts[1].ID})
			if err != nil {
				t.Errorf("GetByIDs error: %v", err)
			} else {
				foundDomestic, foundForeign := false, false
				for _, row := range byIDs {
					foundDomestic = foundDomestic || row.ID == accounts[0].ID
					foundForeign = foundForeign || row.ID == accounts[1].ID
				}
				if foundDomestic != tc.allow || !foundForeign {
					t.Errorf("GetByIDs gate wrong: rows=%+v allowDomestic=%v", byIDs, tc.allow)
				}
			}
			byCode, err := accountRepo.GetByIdentifyCode(ctx, "edition-domestic-code")
			if err != nil || (byCode != nil) != tc.allow {
				t.Errorf("GetByIdentifyCode redundant domestic: got=%+v err=%v allow=%v", byCode, err, tc.allow)
			}
			byAuthority, err := accountRepo.GetByIdentifyCode(ctx, "edition-domestic-authority-code")
			if err != nil || (byAuthority != nil) != tc.allow {
				t.Errorf("GetByIdentifyCode authority domestic: got=%+v err=%v allow=%v", byAuthority, err, tc.allow)
			}
			foreignByCode, err := accountRepo.GetByIdentifyCode(ctx, "edition-foreign-code")
			if err != nil || foreignByCode == nil || foreignByCode.ID != accounts[1].ID {
				t.Errorf("国外渠道 GetByIdentifyCode 不得误伤: got=%+v err=%v", foreignByCode, err)
			}
			otherTenant, err := accountRepo.GetByIdentifyCode(ctx, "edition-other-tenant-code")
			if err != nil || otherTenant == nil || otherTenant.TenantID != 2 {
				t.Errorf("system scope 跨租户国外账号解析被破坏: got=%+v err=%v", otherTenant, err)
			}
		})
	}

	customCtx := edition.WithContext(context.Background(), edition.TypeCustom)
	if row, err := accountRepo.GetByID(customCtx, 2, accounts[0].ID); err != nil || row != nil {
		t.Errorf("跨租户 GetByID 泄漏: row=%+v err=%v", row, err)
	}
	if rows, err := accountRepo.GetByIDs(customCtx, 1, []uint{accounts[3].ID}); err != nil || len(rows) != 0 {
		t.Errorf("跨租户 GetByIDs 泄漏: rows=%+v err=%v", rows, err)
	}
}
