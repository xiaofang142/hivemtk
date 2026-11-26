package repository

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupCRMCrossSearchTestDB(t *testing.T) (*gorm.DB, context.Context) {
	t.Helper()
	database := testutil.NewTestDB(t,
		&model.Customer{},
		&model.Contact{},
		&model.Supplier{},
		&model.ServiceProvider{},
		&model.Company{},
		&model.CustomerCompanyRelation{},
	)
	prev := db.GetDB()
	db.SetTestDB(database)
	t.Cleanup(func() { db.SetTestDB(prev) })
	return database, tenant.WithTenantID(context.Background(), 1)
}

func seedCrossCustomer(t *testing.T, database *gorm.DB, ctx context.Context, id, name, account, industry string) {
	t.Helper()
	c := &model.Customer{
		TenantID: 1, ID: id, UnifiedID: "phone:" + id, Name: name,
		SourceAccountID: account, Industry: industry,
	}
	if err := database.WithContext(ctx).Create(c).Error; err != nil {
		t.Fatalf("seed customer %s: %v", id, err)
	}
}

func seedCrossContact(t *testing.T, database *gorm.DB, ctx context.Context, name, customerID string) {
	t.Helper()
	c := &model.Contact{TenantID: 1, Name: name, CustomerID: customerID, Source: "import", SourceRef: name}
	if err := database.WithContext(ctx).Create(c).Error; err != nil {
		t.Fatalf("seed contact %s: %v", name, err)
	}
}

// CRM-21：关联实体必须收敛到可见客户集合，账号范围外的联系人不得泄漏。
func TestCRMCrossSearch_ScopedEntities(t *testing.T) {
	database, ctx := setupCRMCrossSearchTestDB(t)
	seedCrossCustomer(t, database, ctx, "c-in", "客户甲", "acc-a", "教育")
	seedCrossCustomer(t, database, ctx, "c-out", "客户乙", "acc-b", "电商")
	seedCrossContact(t, database, ctx, "联系人甲", "c-in")
	seedCrossContact(t, database, ctx, "联系人乙", "c-out")

	repo := NewCRMCrossSearchRepository()
	filter := CRMCrossSearchFilter{
		EntityType: "contact",
		CustomerScopeFilter: CustomerScopeFilter{
			SourceAccountIDs: []string{"acc-a"}, RestrictedAccounts: true,
		},
	}
	items, total, err := repo.ListScoped(ctx, filter)
	if err != nil {
		t.Fatalf("ListScoped: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].Name != "联系人甲" {
		t.Fatalf("账号范围外实体必须被排除: total=%d items=%+v", total, items)
	}
	if items[0].SourceAccountID != "acc-a" {
		t.Fatalf("来源账号必须由关联客户回查: %+v", items[0])
	}
}

// CRM-21：全量统计（total/cross_tab）与当前页严格分开，不能用当前页行数冒充总数。
func TestCRMCrossSearch_FullPopulationCrossTab(t *testing.T) {
	database, ctx := setupCRMCrossSearchTestDB(t)
	seedCrossCustomer(t, database, ctx, "c-a", "客户甲", "acc-a", "教育")
	seedCrossCustomer(t, database, ctx, "c-b", "客户乙", "acc-b", "电商")
	for _, name := range []string{"甲一", "甲二", "甲三"} {
		seedCrossContact(t, database, ctx, name, "c-a")
	}
	seedCrossContact(t, database, ctx, "乙一", "c-b")

	repo := NewCRMCrossSearchRepository()
	filter := CRMCrossSearchFilter{
		EntityType: "contact",
		CustomerScopeFilter: CustomerScopeFilter{
			RestrictedAccounts: true, SourceAccountIDs: []string{"acc-a", "acc-b"},
			Page: 1, PageSize: 1,
		},
	}
	items, total, err := repo.ListScoped(ctx, filter)
	if err != nil {
		t.Fatalf("ListScoped: %v", err)
	}
	if total != 4 || len(items) != 1 {
		t.Fatalf("total 必须为全量 4，items 只含当前页 1 条: total=%d items=%d", total, len(items))
	}

	counts, err := repo.CountIndustryAccount(ctx, filter)
	if err != nil {
		t.Fatalf("CountIndustryAccount: %v", err)
	}
	byIndustry := map[string]int64{}
	for _, row := range counts {
		byIndustry[row.Industry] += row.Count
	}
	if byIndustry["教育"] != 3 || byIndustry["电商"] != 1 {
		t.Fatalf("交叉统计必须取全量而非当前页: %+v", byIndustry)
	}
}

// CRM-21：不支持的实体类型显式报错；空授权范围 fail-closed（无结果）。
func TestCRMCrossSearch_UnsupportedAndFailClosed(t *testing.T) {
	database, ctx := setupCRMCrossSearchTestDB(t)
	seedCrossCustomer(t, database, ctx, "c-in", "客户甲", "acc-a", "教育")
	seedCrossContact(t, database, ctx, "联系人甲", "c-in")

	repo := NewCRMCrossSearchRepository()
	if _, _, err := repo.ListScoped(ctx, CRMCrossSearchFilter{EntityType: "unknown"}); err == nil {
		t.Fatal("未知实体类型必须返回错误，不得静默退化为客户全量")
	}

	deny := CRMCrossSearchFilter{
		EntityType:          "contact",
		CustomerScopeFilter: CustomerScopeFilter{RestrictedAccounts: true},
	}
	items, total, err := repo.ListScoped(ctx, deny)
	if err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("空账号授权必须 fail-closed: err=%v total=%d items=%+v", err, total, items)
	}
}
