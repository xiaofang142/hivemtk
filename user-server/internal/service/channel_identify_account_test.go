package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/edition"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// 账号级语义：重置账号 A 的识别码只改 A 行，同渠道账号 B 不受影响，
// 且不新建/覆写渠道级权威行；列表按账号聚合，账号归属字段非空。
func TestChannelIdentifyAccount_RotateIsAccountScoped(t *testing.T) {
	database := testutil.NewTestDB(t, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{})
	ctx := edition.WithContext(context.Background(), edition.TypeCustom)
	ch := model.Channel{TenantID: 1, Name: "抖音", Code: "douyin", Type: "dm", Enabled: true}
	if err := database.WithContext(ctx).Create(&ch).Error; err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	accA := model.ChannelAccount{TenantID: 1, ChannelID: ch.ID, Name: "a", IdentifyCode: "code-a", Status: 1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{}}
	accB := model.ChannelAccount{TenantID: 1, ChannelID: ch.ID, Name: "b", IdentifyCode: "code-b", Status: 1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{}}
	for _, a := range []*model.ChannelAccount{&accA, &accB} {
		if err := database.WithContext(ctx).Create(a).Error; err != nil {
			t.Fatalf("seed account: %v", err)
		}
	}
	acctRepo := repository.NewChannelAccountRepositoryWithDB(database)
	codeRepo := repository.NewChannelIdentifyCodeRepositoryWithDB(database)
	svc := NewChannelIdentifyServiceWithRepos(codeRepo, acctRepo)

	view, err := svc.CreateOrRotateForAccount(ctx, 1, ch.ID, accA.ID)
	if err != nil {
		t.Fatalf("rotate account A: %v", err)
	}
	if view.AccountID != accA.ID || view.AccountName != "a" || view.Code == "" || view.Code == "code-a" {
		t.Fatalf("账号级出参异常: %+v", view)
	}
	gotA, _ := acctRepo.GetByID(ctx, 1, accA.ID)
	if gotA.IdentifyCode != view.Code || gotA.PrevCode != "code-a" || gotA.PrevValidUntil == nil {
		t.Fatalf("账号 A 未写入新码/在途窗口: %+v", gotA)
	}
	gotB, _ := acctRepo.GetByID(ctx, 1, accB.ID)
	if gotB.IdentifyCode != "code-b" || gotB.PrevCode != "" {
		t.Fatalf("同渠道账号 B 被误改: %+v", gotB)
	}
	legacy, err := codeRepo.ListByTenant(ctx, 1)
	if err != nil || len(legacy) != 0 {
		t.Fatalf("账号级写入不应新建渠道级权威行: rows=%+v err=%v", legacy, err)
	}

	rows, err := svc.List(ctx, 1)
	if err != nil || len(rows) != 2 {
		t.Fatalf("账号级列表应返回 2 行: rows=%+v err=%v", rows, err)
	}
	for _, row := range rows {
		if row.AccountID == 0 || row.AccountName == "" {
			t.Errorf("列表行缺账号归属: %+v", row)
		}
	}
}

// 跨渠道/不存在账号必须 fail-closed，不能退化为取首条账号。
func TestChannelIdentifyAccount_RotateWrongChannelRejected(t *testing.T) {
	database := testutil.NewTestDB(t, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{})
	ctx := edition.WithContext(context.Background(), edition.TypeCustom)
	ch := model.Channel{TenantID: 1, Name: "QQ", Code: "qq", Type: "dm", Enabled: true}
	if err := database.WithContext(ctx).Create(&ch).Error; err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	acc := model.ChannelAccount{TenantID: 1, ChannelID: ch.ID, Name: "a", IdentifyCode: "code-a", Status: 1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{}}
	if err := database.WithContext(ctx).Create(&acc).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	svc := NewChannelIdentifyServiceWithRepos(
		repository.NewChannelIdentifyCodeRepositoryWithDB(database),
		repository.NewChannelAccountRepositoryWithDB(database),
	)
	if _, err := svc.CreateOrRotateForAccount(ctx, 1, ch.ID+999, acc.ID); err != ErrIdentifyAccountRequired {
		t.Fatalf("跨渠道账号应被拒绝, got err=%v", err)
	}
}
