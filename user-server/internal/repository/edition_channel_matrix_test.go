package repository

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/edition"
	"hivemtk-user/internal/pkg/testutil"
)

func TestEditionDomesticChannelCodesReturnsCopy(t *testing.T) {
	codes := edition.DomesticChannelCodes()
	if len(codes) == 0 {
		t.Fatal("国内渠道码集合不得为空")
	}
	original := codes[0]
	codes[0] = "tampered"
	if got := edition.DomesticChannelCodes()[0]; got != original {
		t.Fatalf("DomesticChannelCodes 必须返回副本: got=%q want=%q", got, original)
	}
	if !edition.IsDomesticChannel(original) {
		t.Fatalf("调用方修改副本不得影响单一事实源: %q", original)
	}
}

func TestEditionChannelRepositoryMatrix(t *testing.T) {
	db := testutil.NewTestDB(t, &model.Channel{})
	repo := NewChannelRepositoryWithDB(db)
	domestic := model.Channel{TenantID: 1, Name: "抖音", Code: "douyin", Type: "dm", Enabled: true}
	foreign := model.Channel{TenantID: 1, Name: "Telegram", Code: "edition-telegram", Type: "group", Enabled: true}
	otherTenant := model.Channel{TenantID: 2, Name: "企业微信", Code: "wecom", Type: "group", Enabled: true}
	for _, row := range []*model.Channel{&domestic, &foreign, &otherTenant} {
		if err := repo.Create(context.Background(), row); err != nil {
			t.Fatalf("seed channel: %v", err)
		}
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
			byID, err := repo.GetByID(ctx, 1, domestic.ID)
			if err != nil || (byID != nil) != tc.allow {
				t.Fatalf("GetByID domestic: got=%+v err=%v allow=%v", byID, err, tc.allow)
			}
			byCode, err := repo.GetByCode(ctx, domestic.Code)
			if err != nil || (byCode != nil) != tc.allow {
				t.Fatalf("GetByCode domestic: got=%+v err=%v allow=%v", byCode, err, tc.allow)
			}
			byTenantCode, err := repo.GetByTenantAndCode(ctx, 1, domestic.Code)
			if err != nil || (byTenantCode != nil) != tc.allow {
				t.Fatalf("GetByTenantAndCode domestic: got=%+v err=%v allow=%v", byTenantCode, err, tc.allow)
			}
			rows, err := repo.List(ctx, ChannelListOptions{TenantID: 1})
			if err != nil {
				t.Fatal(err)
			}
			foundDomestic, foundForeign := false, false
			for _, row := range rows {
				foundDomestic = foundDomestic || row.ID == domestic.ID
				foundForeign = foundForeign || row.ID == foreign.ID
			}
			if foundDomestic != tc.allow || !foundForeign {
				t.Fatalf("List version gate wrong: rows=%+v allowDomestic=%v", rows, tc.allow)
			}
		})
	}

	customCtx := edition.WithContext(context.Background(), edition.TypeCustom)
	if row, err := repo.GetByID(customCtx, 2, domestic.ID); err != nil || row != nil {
		t.Fatalf("跨租户 GetByID 泄漏: row=%+v err=%v", row, err)
	}
	if row, err := repo.GetByTenantAndCode(customCtx, 2, domestic.Code); err != nil || row != nil {
		t.Fatalf("跨租户 GetByTenantAndCode 泄漏: row=%+v err=%v", row, err)
	}
}
