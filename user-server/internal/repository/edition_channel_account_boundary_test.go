package repository

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/edition"
	"hivemtk-user/internal/pkg/testutil"
)

func TestEditionChannelAccountBoundaryMatrix(t *testing.T) {
	database := testutil.NewTestDB(t, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{})
	channelRepo := NewChannelRepositoryWithDB(database)
	accountRepo := NewChannelAccountRepositoryWithDB(database)
	codeRepo := NewChannelIdentifyCodeRepositoryWithDB(database)

	domestic := model.Channel{TenantID: 1, Name: "抖音", Code: "douyin", Type: "dm", Enabled: true}
	foreign := model.Channel{TenantID: 1, Name: "Telegram", Code: "telegram", Type: "group", Enabled: true}
	mixedForeign := model.Channel{TenantID: 1, Name: "WhatsApp", Code: "whatsapp", Type: "dm", Enabled: true}
	for _, row := range []*model.Channel{&domestic, &foreign, &mixedForeign} {
		if err := channelRepo.Create(context.Background(), row); err != nil {
			t.Fatalf("seed channel: %v", err)
		}
	}
	accounts := []model.ChannelAccount{
		{TenantID: 1, ChannelID: foreign.ID, Name: "foreign-1", IdentifyCode: "boundary-f1", Status: 1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{}},
		{TenantID: 1, ChannelID: domestic.ID, Name: "domestic-1", IdentifyCode: "boundary-mixed", Status: 1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{}},
		{TenantID: 1, ChannelID: mixedForeign.ID, Name: "mixed-foreign", IdentifyCode: "boundary-mixed-foreign", Status: 1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{}},
		{TenantID: 1, ChannelID: domestic.ID, Name: "domestic-2", IdentifyCode: "boundary-domestic-2", Status: 1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{}},
		{TenantID: 1, ChannelID: foreign.ID, Name: "foreign-2", IdentifyCode: "boundary-f2", Status: 1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{}},
	}
	seedCtx := edition.WithContext(context.Background(), edition.TypeCustom)
	for i := range accounts {
		if err := accountRepo.Create(seedCtx, &accounts[i]); err != nil {
			t.Fatalf("seed account %d: %v", i, err)
		}
	}
	for _, rec := range []*model.ChannelIdentifyCode{
		{TenantID: 1, ChannelID: domestic.ID, Code: "boundary-domestic-authority", Version: 1},
		{TenantID: 1, ChannelID: mixedForeign.ID, Code: "boundary-mixed", Version: 1},
	} {
		if err := codeRepo.Upsert(context.Background(), rec); err != nil {
			t.Fatalf("seed authority code: %v", err)
		}
	}

	t.Run("pagination applies gate before limit", func(t *testing.T) {
		ctx := edition.WithContext(context.Background(), edition.TypeOpensource)
		first, err := accountRepo.List(ctx, ChannelAccountListOptions{TenantID: 1, Limit: 2})
		if err != nil || len(first) != 2 || first[0].ID != accounts[0].ID || first[1].ID != accounts[2].ID {
			t.Fatalf("opensource 第一页必须在 gate 后分页: rows=%+v err=%v", first, err)
		}
		second, err := accountRepo.List(ctx, ChannelAccountListOptions{TenantID: 1, Limit: 2, Offset: 2})
		if err != nil || len(second) != 1 || second[0].ID != accounts[4].ID {
			t.Fatalf("opensource 第二页顺序错误: rows=%+v err=%v", second, err)
		}
		custom := edition.WithContext(context.Background(), edition.TypeCustom)
		rows, err := accountRepo.List(custom, ChannelAccountListOptions{TenantID: 1, Limit: 2})
		if err != nil || len(rows) != 2 || rows[0].ID != accounts[0].ID || rows[1].ID != accounts[1].ID {
			t.Fatalf("custom 分页不应过滤国内账号: rows=%+v err=%v", rows, err)
		}
	})

	t.Run("blocked domestic authority does not create false ambiguity", func(t *testing.T) {
		opensource := edition.WithContext(context.Background(), edition.TypeOpensource)
		got, err := accountRepo.GetByIdentifyCode(opensource, "boundary-domestic-authority")
		if err != nil || got != nil {
			t.Fatalf("opensource 国内权威码应直接不可解析而非假歧义: got=%+v err=%v", got, err)
		}
		custom := edition.WithContext(context.Background(), edition.TypeCustom)
		got, err = accountRepo.GetByIdentifyCode(custom, "boundary-domestic-authority")
		if got != nil || !errors.Is(err, ErrAmbiguousIdentifyCode) {
			t.Fatalf("custom 同渠道多账号仍应判歧义: got=%+v err=%v", got, err)
		}
	})

	t.Run("mixed blocked account code falls through to allowed authority code", func(t *testing.T) {
		opensource := edition.WithContext(context.Background(), edition.TypeOpensource)
		got, err := accountRepo.GetByIdentifyCode(opensource, "boundary-mixed")
		if err != nil || got == nil || got.ID != accounts[2].ID {
			t.Fatalf("已禁国内账号码不得遮蔽国外权威码: got=%+v err=%v", got, err)
		}
	})
}
