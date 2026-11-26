package repository

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

// CRM-24: account grants are OR-of-AND clauses. A grant for acct-1/A must not
// combine with a separate grant for acct-2/B to expose acct-1/B or acct-2/A.
func TestCRMCustomerRepository_ListScoped_AccountRulesKeepPairsAndCounts(t *testing.T) {
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previous) })
	db := testutil.NewTestDB(t, &model.Customer{})
	for i, row := range []model.Customer{
		{ID: "pair-1-a", SourceAccountID: "acct-1", Industry: "A"},
		{ID: "pair-1-b", SourceAccountID: "acct-1", Industry: "B"},
		{ID: "pair-2-a", SourceAccountID: "acct-2", Industry: "A"},
		{ID: "pair-2-b", SourceAccountID: "acct-2", Industry: "B"},
	} {
		row.TenantID, row.UnifiedID = crmScopeTenantA, row.ID
		row.CreatedAt = time.Unix(int64(100-i), 0)
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	ctx := tenant.WithTenantID(context.Background(), crmScopeTenantA)
	repo := NewCRMCustomerRepository()
	filter := CustomerScopeFilter{
		RestrictedAccounts: true,
		AccountRules: []CustomerAccountScopeRule{
			{SourceAccountIDs: []string{"acct-1"}, Industries: []string{"A"}},
			{SourceAccountIDs: []string{"acct-2"}, Industries: []string{"B"}},
		},
		Page:     1,
		PageSize: 1,
	}

	rows, total, err := repo.ListScoped(ctx, filter)
	if err != nil || total != 2 || len(rows) != 1 || rows[0].SourceAccountID != "acct-1" || rows[0].Industry != "A" {
		t.Fatalf("配对授权第一页应只返回 acct-1/A，实际 rows=%+v,total=%d,err=%v", rows, total, err)
	}

	filter.Page = 2
	rows, total, err = repo.ListScoped(ctx, filter)
	if err != nil || total != 2 || len(rows) != 1 || rows[0].SourceAccountID != "acct-2" || rows[0].Industry != "B" {
		t.Fatalf("配对授权第二页应只返回 acct-2/B，实际 rows=%+v,total=%d,err=%v", rows, total, err)
	}
}
