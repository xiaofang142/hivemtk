package repository

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/edition"
	"hivemtk-user/internal/pkg/testutil"
)

// 账号级识别码语义：凭证候选源必须包含每个账号的当前码与在途旧码，
// 同时继续接受存量渠道级码（历史兼容）；过期旧码一律失效（fail-closed）。
func TestChannelIdentifyAccount_ValidCodesUnionAndGrace(t *testing.T) {
	database := testutil.NewTestDB(t, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{})
	ctx := edition.WithContext(context.Background(), edition.TypeCustom)
	ch := model.Channel{TenantID: 1, Name: "微信", Code: "personal_wx", Type: "dm", Enabled: true}
	if err := database.WithContext(ctx).Create(&ch).Error; err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	future := time.Now().Add(time.Minute)
	past := time.Now().Add(-time.Minute)
	accounts := []model.ChannelAccount{
		{TenantID: 1, ChannelID: ch.ID, Name: "a", IdentifyCode: "acct-a-new", PrevCode: "acct-a-old", PrevValidUntil: &future, Status: 1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{}},
		{TenantID: 1, ChannelID: ch.ID, Name: "b", IdentifyCode: "acct-b", PrevCode: "acct-b-expired", PrevValidUntil: &past, Status: 1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{}},
	}
	for i := range accounts {
		if err := database.WithContext(ctx).Create(&accounts[i]).Error; err != nil {
			t.Fatalf("seed account %d: %v", i, err)
		}
	}
	if err := NewChannelIdentifyCodeRepositoryWithDB(database).Upsert(ctx, &model.ChannelIdentifyCode{
		TenantID: 1, ChannelID: ch.ID, Code: "legacy-channel", Version: 1,
	}); err != nil {
		t.Fatalf("seed legacy code: %v", err)
	}

	codes, err := NewChannelIdentifyCodeRepositoryWithDB(database).ValidCodes(ctx)
	if err != nil {
		t.Fatalf("ValidCodes: %v", err)
	}
	want := map[string]bool{"acct-a-new": true, "acct-a-old": true, "acct-b": true, "legacy-channel": true}
	got := map[string]bool{}
	for _, c := range codes {
		got[c] = true
	}
	for c := range want {
		if !got[c] {
			t.Errorf("ValidCodes 缺少 %q，got=%v", c, codes)
		}
	}
	if got["acct-b-expired"] {
		t.Errorf("过期在途旧码必须失效，got=%v", codes)
	}
}

// 账号级在途旧码必须解析回同一账号，与 ValidCodes 的候选判定保持一致。
func TestChannelIdentifyAccount_GetByIdentifyCodeGrace(t *testing.T) {
	database := testutil.NewTestDB(t, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{})
	ctx := edition.WithContext(context.Background(), edition.TypeCustom)
	ch := model.Channel{TenantID: 1, Name: "QQ", Code: "qq", Type: "dm", Enabled: true}
	if err := database.WithContext(ctx).Create(&ch).Error; err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	future := time.Now().Add(time.Minute)
	acc := model.ChannelAccount{
		TenantID: 1, ChannelID: ch.ID, Name: "grace-account",
		IdentifyCode: "grace-new", PrevCode: "grace-old", PrevValidUntil: &future,
		Status: 1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{},
	}
	if err := database.WithContext(ctx).Create(&acc).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	repo := NewChannelAccountRepositoryWithDB(database)
	got, err := repo.GetByIdentifyCode(ctx, "grace-old")
	if err != nil || got == nil || got.ID != acc.ID {
		t.Fatalf("在途旧码应解析回同一账号: got=%+v err=%v", got, err)
	}
	got, err = repo.GetByIdentifyCode(ctx, "grace-new")
	if err != nil || got == nil || got.ID != acc.ID {
		t.Fatalf("当前码应解析回同一账号: got=%+v err=%v", got, err)
	}
}
