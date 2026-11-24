package repository

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/edition"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func TestEditionChannelAccountWriteGateMatrix(t *testing.T) {
	newDB := func(t *testing.T) (*gorm.DB, ChannelAccountRepository, model.Channel, model.Channel, model.ChannelAccount, model.ChannelAccount) {
		t.Helper()
		database := testutil.NewTestDB(t, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{})
		channelRepo := NewChannelRepositoryWithDB(database)
		accountRepo := NewChannelAccountRepositoryWithDB(database)
		domestic := model.Channel{TenantID: 1, Name: "抖音", Code: "douyin", Type: "dm", Enabled: true}
		foreign := model.Channel{TenantID: 1, Name: "Telegram", Code: "telegram", Type: "group", Enabled: true}
		for _, row := range []*model.Channel{&domestic, &foreign} {
			if err := channelRepo.Create(context.Background(), row); err != nil {
				t.Fatal(err)
			}
		}
		domesticAccount := model.ChannelAccount{TenantID: 1, ChannelID: domestic.ID, Name: "domestic", IdentifyCode: "write-domestic", Status: 1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{}}
		foreignAccount := model.ChannelAccount{TenantID: 1, ChannelID: foreign.ID, Name: "foreign", IdentifyCode: "write-foreign", Status: 1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{}}
		seedCtx := edition.WithContext(context.Background(), edition.TypeCustom)
		for _, row := range []*model.ChannelAccount{&domesticAccount, &foreignAccount} {
			if err := accountRepo.Create(seedCtx, row); err != nil {
				t.Fatal(err)
			}
		}
		return database, accountRepo, domestic, foreign, domesticAccount, foreignAccount
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
		t.Run(tc.name+"/create", func(t *testing.T) {
			database, repo, domestic, foreign, _, _ := newDB(t)
			ctx := edition.WithContext(context.Background(), tc.version)
			domesticNew := model.ChannelAccount{TenantID: 1, ChannelID: domestic.ID, Name: "new-domestic", IdentifyCode: "write-new-domestic", Status: 1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{}}
			err := repo.Create(ctx, &domesticNew)
			if (err == nil) != tc.allow {
				t.Fatalf("Create domestic gate wrong: err=%v allow=%v", err, tc.allow)
			}
			if !tc.allow {
				var count int64
				database.Model(&model.ChannelAccount{}).Where("id = ?", domesticNew.ID).Count(&count)
				if count != 0 {
					t.Fatalf("被拒 Create 不得落库: count=%d", count)
				}
			}
			foreignNew := model.ChannelAccount{TenantID: 1, ChannelID: foreign.ID, Name: "new-foreign", IdentifyCode: "write-new-foreign", Status: 1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{}}
			if err := repo.Create(ctx, &foreignNew); err != nil {
				t.Fatalf("国外渠道 Create 不得误伤: %v", err)
			}
			orphan := model.ChannelAccount{TenantID: 1, ChannelID: 999999, Name: "orphan", IdentifyCode: "write-orphan", Status: 1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{}}
			if err := repo.Create(ctx, &orphan); err == nil {
				t.Fatal("孤儿渠道 Create 必须拒绝")
			}
		})

		t.Run(tc.name+"/update-target-and-migration", func(t *testing.T) {
			database, repo, domestic, _, domesticAccount, foreignAccount := newDB(t)
			ctx := edition.WithContext(context.Background(), tc.version)
			err := repo.Update(ctx, 1, domesticAccount.ID, map[string]any{"name": "hacked"})
			if tc.allow {
				if err != nil {
					t.Fatalf("custom 更新国内账号应允许: %v", err)
				}
			} else {
				if err != nil {
					t.Fatalf("禁用目标行应保持 missing-row 语义: %v", err)
				}
				var got model.ChannelAccount
				database.First(&got, domesticAccount.ID)
				if got.Name != domesticAccount.Name {
					t.Fatalf("禁用目标行被修改: %+v", got)
				}
			}

			err = repo.Update(ctx, 1, foreignAccount.ID, map[string]any{"channel_id": domestic.ID})
			if (err == nil) != tc.allow {
				t.Fatalf("Update channel_id 迁移门错误: err=%v allow=%v", err, tc.allow)
			}
			var got model.ChannelAccount
			database.First(&got, foreignAccount.ID)
			if tc.allow {
				if got.ChannelID != domestic.ID {
					t.Fatalf("custom 迁移应生效: %+v", got)
				}
			} else if got.ChannelID != foreignAccount.ChannelID {
				t.Fatalf("禁用迁移不得改变数据: %+v", got)
			}
		})

		t.Run(tc.name+"/delete-and-bulk-write", func(t *testing.T) {
			database, repo, domestic, _, domesticAccount, foreignAccount := newDB(t)
			ctx := edition.WithContext(context.Background(), tc.version)
			deleteTarget := model.ChannelAccount{TenantID: 1, ChannelID: domestic.ID, Name: "delete-target", IdentifyCode: "write-delete-target", Status: 1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{}}
			if err := repo.Create(edition.WithContext(context.Background(), edition.TypeCustom), &deleteTarget); err != nil {
				t.Fatal(err)
			}
			if err := repo.Delete(ctx, 1, deleteTarget.ID); err != nil {
				t.Fatalf("Delete 不应改变 missing-row 语义: %v", err)
			}
			var count int64
			database.Model(&model.ChannelAccount{}).Where("id = ?", deleteTarget.ID).Count(&count)
			if (count == 0) != tc.allow {
				t.Fatalf("Delete domestic gate wrong: count=%d allow=%v", count, tc.allow)
			}

			if err := repo.SetIdentifyCodeByChannel(ctx, 1, domestic.ID, "write-reset-domestic"); err != nil {
				t.Fatalf("SetIdentifyCodeByChannel error: %v", err)
			}
			var domesticGot model.ChannelAccount
			database.First(&domesticGot, domesticAccount.ID)
			if tc.allow {
				if domesticGot.IdentifyCode != "write-reset-domestic" {
					t.Fatalf("custom 识别码更新应生效: %+v", domesticGot)
				}
			} else if domesticGot.IdentifyCode != domesticAccount.IdentifyCode {
				t.Fatalf("禁用识别码更新不得改变数据: %+v", domesticGot)
			}

			if err := repo.Delete(ctx, 1, foreignAccount.ID); err != nil {
				t.Fatalf("国外账号 Delete 不得误伤: %v", err)
			}
			database.Model(&model.ChannelAccount{}).Where("id = ?", foreignAccount.ID).Count(&count)
			if count != 0 {
				t.Fatalf("国外账号 Delete 未生效: count=%d", count)
			}

			if err := repo.DeleteByChannel(ctx, 1, domestic.ID); err != nil {
				t.Fatalf("DeleteByChannel error: %v", err)
			}
			database.Model(&model.ChannelAccount{}).Where("id = ?", domesticAccount.ID).Count(&count)
			if (count == 0) != tc.allow {
				t.Fatalf("DeleteByChannel domestic gate wrong: count=%d allow=%v", count, tc.allow)
			}
		})
	}
}
