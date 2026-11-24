package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/edition"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

func TestEditionChannelAccountAndDispatchMatrix(t *testing.T) {
	db := testutil.NewTestDB(t, &model.Channel{}, &model.ChannelAccount{})
	channelRepo := repository.NewChannelRepositoryWithDB(db)
	accountRepo := repository.NewChannelAccountRepositoryWithDB(db)
	domestic := model.Channel{TenantID: 1, Name: "抖音", Code: "douyin", Type: "dm", Enabled: true}
	if err := channelRepo.Create(context.Background(), &domestic); err != nil {
		t.Fatal(err)
	}
	account := model.ChannelAccount{
		TenantID: 1, ChannelID: domestic.ID, Name: "domestic-account", IdentifyCode: "edition-account-code",
		Status: 1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{},
	}
	if err := accountRepo.Create(edition.WithContext(context.Background(), edition.TypeCustom), &account); err != nil {
		t.Fatal(err)
	}
	accountSvc := NewChannelAccountServiceWithRepo(accountRepo)

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
			rows, err := accountSvc.ListOptions(ctx, 1, dto.ChannelAccountListQuery{ChannelID: domestic.ID})
			if err != nil {
				t.Fatalf("ListOptions error: %v", err)
			}
			if (len(rows) == 1) != tc.allow {
				t.Errorf("ListOptions 版本门错误: rows=%+v allow=%v", rows, tc.allow)
			}
			got, err := accountSvc.Get(ctx, 1, account.ID)
			if tc.allow {
				if err != nil || got == nil {
					t.Errorf("custom Get 应命中: got=%+v err=%v", got, err)
				}
			} else if err == nil || got != nil {
				t.Errorf("非 custom Get 必须 fail-closed: got=%+v err=%v", got, err)
			}
			dispatch := newTestChannelDispatchService(db)
			picked, err := dispatch.PickAccount(ctx, DispatchPickInput{TenantID: 1, ChannelID: domestic.ID, Channel: domestic.Code})
			if tc.allow {
				if err != nil || picked == nil || picked.AccountID != account.ID {
					t.Errorf("custom PickAccount 应命中: picked=%+v err=%v", picked, err)
				}
			} else if err == nil || picked != nil {
				t.Errorf("非 custom PickAccount 必须 fail-closed: picked=%+v err=%v", picked, err)
			}
		})
	}
}
