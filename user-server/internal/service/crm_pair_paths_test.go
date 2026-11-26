package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/edition"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// 读取、写入、导出和去重必须消费相同的账号×行业配对，包括已验证的来源别名。
func TestCRMAccountPairs_PublicReadWriteExportDedupe(t *testing.T) {
	db := testutil.NewTestDB(t, &model.Customer{})
	ctx := tenant.WithTenantID(context.Background(), 7)
	rows := []*model.Customer{
		{TenantID: 7, ID: "pair-a1", UnifiedID: "pair-a1", Name: "同名客户", SourceAccountID: "11", Industry: "A"},
		{TenantID: 7, ID: "pair-a2", UnifiedID: "pair-a2", Name: "同名客户", SourceAccountID: "code-a", Industry: "A"},
		{TenantID: 7, ID: "pair-b", UnifiedID: "pair-b", Name: "同名客户", SourceAccountID: "12", Industry: "B"},
		{TenantID: 7, ID: "pair-cross-a", UnifiedID: "pair-cross-a", Name: "同名客户", SourceAccountID: "11", Industry: "B"},
		{TenantID: 7, ID: "pair-cross-b", UnifiedID: "pair-cross-b", Name: "同名客户", SourceAccountID: "12", Industry: "A"},
	}
	if err := db.WithContext(ctx).Create(rows).Error; err != nil {
		t.Fatal(err)
	}
	perm := &CustomerPermissionService{customers: repository.NewCRMCustomerRepository(), accountRepo: crmPermissionAccountsForTest(), scopeSvc: NewBusinessScopeServiceWithReaders(nil, nil), authRepo: &authConfigStoreStub{rows: []model.ChannelAccountAuthorization{
		{UserID: 3, ChannelAccountID: 11, Role: "viewer", VisibleIndustryScope: `["A"]`},
		{UserID: 3, ChannelAccountID: 12, Role: "viewer", VisibleIndustryScope: `["B"]`},
	}}}
	scope, err := perm.Resolve(ctx, 3)
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"pair-a1": true, "pair-a2": true, "pair-b": true}
	for page := 1; page <= 4; page++ {
		got, total, err := perm.ListScoped(ctx, scope, repository.CustomerScopeFilter{Page: page, PageSize: 1})
		if err != nil || total != 3 || (page <= 3 && (len(got) != 1 || !allowed[got[0].ID])) || (page == 4 && len(got) != 0) {
			t.Fatalf("配对分页/计数错误 page=%d rows=%+v total=%d err=%v", page, got, total, err)
		}
	}
	for _, row := range rows {
		_, err := perm.GetByIDScoped(ctx, scope, row.ID)
		if (err == nil) != allowed[row.ID] {
			t.Errorf("详情配对错误 id=%s err=%v", row.ID, err)
		}
		if perm.AllowsWrite(scope, row.SourceAccountID, row.Industry, 0) != allowed[row.ID] {
			t.Errorf("写权限必须与读权限一致 id=%s", row.ID)
		}
	}
	export := &CustomerImportExportService{permission: perm, scopeRepo: repository.NewCRMCustomerRepository()}
	result, err := export.ExportByQuery(ctx, scope, "", "", 0, "tester")
	if err != nil || result.Total != 3 || len(result.Rows) != 3 {
		t.Fatalf("导出配对/计数错误 result=%+v err=%v", result, err)
	}
	for _, row := range result.Rows {
		if !allowed[row.ID] {
			t.Errorf("导出泄露未授权客户 %+v", row)
		}
	}
	dedupe := &CustomerDedupeService{permission: perm, scoped: repository.NewCRMCustomerRepository(), ruleSvc: &OneIDMergeRuleService{}}
	candidates, err := dedupe.FindDuplicates(ctx, DedupeScanOptions{Scope: scope})
	if err != nil || len(candidates) != 3 {
		t.Fatalf("去重应只组合三个可见客户，实际=%+v err=%v", candidates, err)
	}
	for _, pair := range candidates {
		if !allowed[pair.LeftID] || !allowed[pair.RightID] {
			t.Errorf("去重泄露未授权客户 %+v", pair)
		}
	}
	cross := &CRM360Service{permission: perm}
	for _, tc := range []struct {
		industry string
		wantIDs  map[string]bool
		wantRows map[string]int64
	}{
		{"", allowed, map[string]int64{"A/11": 1, "A/code-a": 1, "B/12": 1}},
		{"A", map[string]bool{"pair-a1": true, "pair-a2": true}, map[string]int64{"A/11": 1, "A/code-a": 1}},
		{"B", map[string]bool{"pair-b": true}, map[string]int64{"B/12": 1}},
	} {
		result, err := cross.CrossIndustrySearch(ctx, 7, scope, "", tc.industry, 1, 100)
		if err != nil || result.Total != int64(len(tc.wantIDs)) || len(result.Items) != len(tc.wantIDs) {
			t.Fatalf("行业检索总数必须保留账号配对 industry=%s result=%+v err=%v", tc.industry, result, err)
		}
		for _, item := range result.Items {
			if !tc.wantIDs[item.ID] {
				t.Errorf("行业检索泄露未授权客户 industry=%s item=%+v", tc.industry, item)
			}
		}
		gotRows := make(map[string]int64)
		for _, row := range result.CrossTab {
			gotRows[row.Industry+"/"+row.SourceAccountID] = row.Count
		}
		if !reflect.DeepEqual(gotRows, tc.wantRows) {
			t.Errorf("交叉统计不得包含未授权账号行业组合 industry=%s got=%v want=%v", tc.industry, gotRows, tc.wantRows)
		}
	}
}

// 配对规则只能细化稳定账号集合，瞬时来源引用不能成为扩权凭据。
func TestCRMAccountPairs_StableAccountBoundary(t *testing.T) {
	db := testutil.NewTestDB(t, &model.Customer{})
	ctx := tenant.WithTenantID(context.Background(), 7)
	rows := []*model.Customer{
		{TenantID: 7, ID: "boundary-stable", UnifiedID: "boundary-stable", SourceAccountID: "11", Industry: "A"},
		{TenantID: 7, ID: "boundary-alias", UnifiedID: "boundary-alias", SourceAccountID: "code-a", Industry: "A"},
		{TenantID: 7, ID: "boundary-other", UnifiedID: "boundary-other", SourceAccountID: "12", Industry: "B"},
	}
	if err := db.WithContext(ctx).Create(rows).Error; err != nil {
		t.Fatal(err)
	}
	permission := &CustomerPermissionService{customers: repository.NewCRMCustomerRepository()}
	rules := []CustomerAccountRule{
		{AccountID: "11", SourceAccountIDs: []string{"11", "code-a"}, Industries: []string{"A"}},
		{AccountID: "12", SourceAccountIDs: []string{"12"}, Industries: []string{"B"}},
	}
	for _, tc := range []struct {
		name       string
		restricted bool
		accountIDs []string
		want       map[string]bool
	}{
		{"conflicting-boundary", true, []string{"acct-1"}, nil},
		{"empty-boundary", true, nil, nil},
		{"alias-is-not-stable-grant", true, []string{"code-a"}, nil},
		{"authorized-stable-key-keeps-alias", true, []string{"11"}, map[string]bool{"boundary-stable": true, "boundary-alias": true}},
		{"unrestricted-rule-only-compatibility", false, nil, map[string]bool{"boundary-stable": true, "boundary-alias": true, "boundary-other": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scope := CustomerDataScope{UserID: 3, RestrictedAccounts: tc.restricted, AccountIDs: tc.accountIDs, AccountRules: rules}
			for _, row := range rows {
				_, err := permission.GetByIDScoped(ctx, scope, row.ID)
				if (err == nil) != tc.want[row.ID] {
					t.Errorf("详情不得越过稳定账号集合 id=%s want=%t err=%v", row.ID, tc.want[row.ID], err)
				}
				if permission.AllowsWrite(scope, row.SourceAccountID, row.Industry, 0) != tc.want[row.ID] {
					t.Errorf("写入不得越过稳定账号集合 id=%s want=%t", row.ID, tc.want[row.ID])
				}
			}
			for _, requested := range [][]string{nil, {"11"}, {"code-a"}, {"12"}} {
				wantCount := 0
				for _, row := range rows {
					if tc.want[row.ID] && (len(requested) == 0 || containsCRMString(requested, row.SourceAccountID)) {
						wantCount++
					}
				}
				got, total, err := permission.ListScoped(ctx, scope, repository.CustomerScopeFilter{SourceAccountIDs: requested, Page: 1, PageSize: 100})
				if err != nil || total != int64(wantCount) || len(got) != wantCount {
					t.Errorf("SQL 过滤与计数不得复活越权规则 requested=%v rows=%+v total=%d want=%d err=%v", requested, got, total, wantCount, err)
				}
				for _, row := range got {
					if !tc.want[row.ID] {
						t.Errorf("SQL 返回未授权客户 id=%s", row.ID)
					}
				}
			}
		})
	}
}

// 超过单页上限时仍由 SQL 完成配对和计数，导出跨页不能丢行。
func TestCRMAccountPairs_LargePaginationAndExport(t *testing.T) {
	db := testutil.NewTestDB(t, &model.Customer{})
	ctx := tenant.WithTenantID(context.Background(), 7)
	rows := make([]*model.Customer, 0, 506)
	for i := 0; i < 505; i++ {
		id := fmt.Sprintf("large-pair-%04d", i)
		rows = append(rows, &model.Customer{TenantID: 7, ID: id, UnifiedID: id, Name: id, SourceAccountID: "11", Industry: "A"})
	}
	rows = append(rows, &model.Customer{TenantID: 7, ID: "large-forbidden", UnifiedID: "large-forbidden", SourceAccountID: "11", Industry: "B"})
	if err := db.WithContext(ctx).CreateInBatches(rows, 100).Error; err != nil {
		t.Fatal(err)
	}
	perm := &CustomerPermissionService{customers: repository.NewCRMCustomerRepository()}
	scope := CustomerDataScope{UserID: 3, RestrictedAccounts: true, AccountIDs: []string{"11"}, AccountRules: []CustomerAccountRule{{AccountID: "11", Industries: []string{"A"}}}}
	got, total, err := perm.ListScoped(ctx, scope, repository.CustomerScopeFilter{Page: 2, PageSize: 500})
	if err != nil || total != 505 || len(got) != 5 {
		t.Fatalf("第二页应剩5条且total505 rows=%d total=%d err=%v", len(got), total, err)
	}
	result, err := (&CustomerImportExportService{permission: perm, scopeRepo: repository.NewCRMCustomerRepository()}).ExportByQuery(ctx, scope, "", "", 0, "tester")
	if err != nil || result.Total != 505 || len(result.Rows) != 505 {
		t.Fatalf("导出必须完整跨页 result=%+v err=%v", result, err)
	}
}

// 同一客户下的三个关联实体分别执行账号配对，越权旧记录也不能被更新或删除。
func TestCRMAccountPairs_LinkedEntities(t *testing.T) {
	db := testutil.NewTestDB(t, &model.Customer{}, &model.Contact{}, &model.Supplier{}, &model.ServiceProvider{}, &model.CustomerMerge{})
	ctx := tenant.WithTenantID(context.Background(), 7)
	cust := &model.Customer{TenantID: 7, ID: "entity-pair-parent", UnifiedID: "entity-pair-parent", SourceAccountID: "11", Industry: "A"}
	if err := db.WithContext(ctx).Create(cust).Error; err != nil {
		t.Fatal(err)
	}
	a, b := uint(11), uint(12)
	for i, account := range []*uint{&a, &b} {
		id := int64(i + 1)
		for _, row := range []any{&model.Contact{ID: id, TenantID: 7, CustomerID: cust.ID, Name: "相同姓名", ChannelAccountID: account, ContactInfo: "{}"}, &model.Supplier{ID: id, TenantID: 7, CustomerID: cust.ID, Name: "相同姓名", ChannelAccountID: account, ContactInfo: "{}"}, &model.ServiceProvider{ID: id, TenantID: 7, CustomerID: cust.ID, Name: "相同姓名", ChannelAccountID: account, ContactInfo: "{}"}} {
			if err := db.WithContext(ctx).Create(row).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	perm := &CustomerPermissionService{customers: repository.NewCRMCustomerRepository()}
	scope := CustomerDataScope{UserID: 3, RestrictedAccounts: true, AccountIDs: []string{"11", "12"}, AccountRules: []CustomerAccountRule{{AccountID: "11", Industries: []string{"A"}}, {AccountID: "12", Industries: []string{"B"}}}}
	svc := &CRMCustomerService{permission: perm, accounts: crmPermissionAccountsForTest(), contacts: repository.NewContactRepository(), suppliers: repository.NewSupplierRepository(), providers: repository.NewServiceProviderRepository()}
	for _, kind := range []int{CRMEntityContact, CRMEntitySupplier, CRMEntityServiceProvider} {
		for page := 1; page <= 2; page++ {
			got, total, err := svc.ListEntities(ctx, scope, kind, cust.ID, page, 1)
			var ids []int64
			switch rows := got.(type) {
			case []*model.Contact:
				for _, row := range rows {
					ids = append(ids, row.ID)
				}
			case []*model.Supplier:
				for _, row := range rows {
					ids = append(ids, row.ID)
				}
			case []*model.ServiceProvider:
				for _, row := range rows {
					ids = append(ids, row.ID)
				}
			default:
				t.Fatalf("未知关联实体返回类型 %T", got)
			}
			if err != nil || total != 1 || (page == 1 && !reflect.DeepEqual(ids, []int64{1})) || (page == 2 && len(ids) != 0) {
				t.Errorf("关联实体配对必须在count之前过滤 kind=%d page=%d rows=%+v total=%d err=%v", kind, page, got, total, err)
			}
		}
	}
	viewSvc := &CRM360Service{permission: perm, contacts: svc.contacts, suppliers: svc.suppliers, providers: svc.providers, merges: repository.NewCustomerMergeRepository()}
	view, err := viewSvc.View(ctx, scope, cust.ID)
	if err != nil || len(view.Contacts) != 1 || len(view.Suppliers) != 1 || len(view.ServiceProviders) != 1 {
		t.Errorf("360关联实体不能泄漏其他账号 view=%+v err=%v", view, err)
	}
	dedupe := &CustomerDedupeService{permission: perm, contacts: svc.contacts, suppliers: svc.suppliers, providers: svc.providers, ruleSvc: &OneIDMergeRuleService{}}
	pairs, err := dedupe.FindEntitiesDuplicates(ctx, DedupeEntityScanOptions{Scope: scope})
	if err != nil || len(pairs) != 3 {
		t.Errorf("实体去重只应包含3个可见实体的组合 count=%d err=%v", len(pairs), err)
	}
	for _, pair := range pairs {
		if pair.LeftID != "1" || pair.RightID != "1" {
			t.Errorf("实体去重泄漏其他账号 %+v", pair)
		}
	}
	for _, kind := range []int{CRMEntityContact, CRMEntitySupplier, CRMEntityServiceProvider} {
		if _, err := svc.UpdateEntity(ctx, scope, kind, 2, CRMEntitySave{Name: "越权修改"}); !errors.Is(err, ErrCRMCustomerForbidden) {
			t.Errorf("不得更新越权旧实体 kind=%d err=%v", kind, err)
		}
		if err := svc.DeleteEntity(ctx, scope, kind, 2); !errors.Is(err, ErrCRMCustomerForbidden) {
			t.Errorf("不得删除越权旧实体 kind=%d err=%v", kind, err)
		}
		if _, err := svc.UpdateEntity(ctx, scope, kind, 1, CRMEntitySave{Name: "合法修改"}); err != nil {
			t.Errorf("已授权实体应能更新 kind=%d err=%v", kind, err)
		}
	}
}

// 无账号旧记录可继承已授权父客户；显式 0 和改挂时隐式保留的账号仍要校验。
func TestCRMAccountPairs_EntityLegacyZeroAndReparent(t *testing.T) {
	db := testutil.NewTestDB(t, &model.Customer{}, &model.Contact{}, &model.Supplier{}, &model.ServiceProvider{})
	ctx := tenant.WithTenantID(context.Background(), 7)
	customers := []model.Customer{
		{TenantID: 7, ID: "parent-a", UnifiedID: "parent-a", SourceAccountID: "11", Industry: "A"},
		{TenantID: 7, ID: "parent-b", UnifiedID: "parent-b", SourceAccountID: "12", Industry: "B"},
	}
	if err := db.WithContext(ctx).Create(&customers).Error; err != nil {
		t.Fatal(err)
	}
	a, zero := uint(11), uint(0)
	for i, account := range []*uint{&a, nil, &zero} {
		id := int64(i + 1)
		for _, row := range []any{
			&model.Contact{ID: id, TenantID: 7, CustomerID: "parent-a", Name: "原值", ChannelAccountID: account, ContactInfo: "{}"},
			&model.Supplier{ID: id, TenantID: 7, CustomerID: "parent-a", Name: "原值", ChannelAccountID: account, ContactInfo: "{}"},
			&model.ServiceProvider{ID: id, TenantID: 7, CustomerID: "parent-a", Name: "原值", ChannelAccountID: account, ContactInfo: "{}"},
		} {
			if err := db.WithContext(ctx).Create(row).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	permission := &CustomerPermissionService{customers: repository.NewCRMCustomerRepository()}
	scope := CustomerDataScope{UserID: 3, RestrictedAccounts: true, AccountIDs: []string{"11", "12"}, AccountRules: []CustomerAccountRule{{AccountID: "11", Industries: []string{"A"}}, {AccountID: "12", Industries: []string{"B"}}}}
	svc := &CRMCustomerService{permission: permission, accounts: crmPermissionAccountsForTest(), contacts: repository.NewContactRepository(), suppliers: repository.NewSupplierRepository(), providers: repository.NewServiceProviderRepository()}
	for _, kind := range []int{CRMEntityContact, CRMEntitySupplier, CRMEntityServiceProvider} {
		got, total, err := svc.ListEntities(ctx, scope, kind, "parent-a", 1, 100)
		if err != nil || total != 2 || reflect.ValueOf(got).Len() != 2 {
			t.Fatalf("只允许显式合法账号及 nil 旧记录，kind=%d got=%+v total=%d err=%v", kind, got, total, err)
		}
		for _, input := range []CRMEntitySave{
			{Name: "不应写入", ChannelAccountID: &zero},
			{Name: "不应改挂", CustomerID: "parent-b"},
		} {
			if _, err := svc.UpdateEntity(ctx, scope, kind, 1, input); !errors.Is(err, ErrCRMCustomerForbidden) {
				t.Errorf("最终账号与父客户配对不合法必须拒绝 kind=%d input=%+v err=%v", kind, input, err)
			}
		}
		var parent, name string
		var account *uint
		switch kind {
		case CRMEntityContact:
			row, err := svc.contacts.GetByID(ctx, 1)
			if err != nil || row == nil {
				t.Fatalf("读取联系人失败 row=%+v err=%v", row, err)
			}
			parent, name, account = row.CustomerID, row.Name, row.ChannelAccountID
		case CRMEntitySupplier:
			row, err := svc.suppliers.GetByID(ctx, 1)
			if err != nil || row == nil {
				t.Fatalf("读取供应商失败 row=%+v err=%v", row, err)
			}
			parent, name, account = row.CustomerID, row.Name, row.ChannelAccountID
		default:
			row, err := svc.providers.GetByID(ctx, 1)
			if err != nil || row == nil {
				t.Fatalf("读取服务商失败 row=%+v err=%v", row, err)
			}
			parent, name, account = row.CustomerID, row.Name, row.ChannelAccountID
		}
		if parent != "parent-a" || name != "原值" || account == nil || *account != a {
			t.Errorf("拒绝后实体与关系必须不变 kind=%d parent=%s name=%s account=%v", kind, parent, name, account)
		}
		if _, err := svc.UpdateEntity(ctx, scope, kind, 3, CRMEntitySave{Name: "零账号"}); !errors.Is(err, ErrCRMCustomerForbidden) {
			t.Errorf("旧实体显式零账号不能修改 kind=%d err=%v", kind, err)
		}
		if _, err := svc.UpdateEntity(ctx, scope, kind, 2, CRMEntitySave{Name: "合法旧记录", CustomerID: "parent-b"}); err != nil {
			t.Errorf("nil 账号旧记录应继承合法目标父客户 kind=%d err=%v", kind, err)
		}
	}
}

// 真实授权表到账号引用解析再到 SQL 查询：轮换只改变瞬时引用，撤权立即改变可见集合。
func TestCRMAccountPairs_ProductionResolveRotationAndRevoke(t *testing.T) {
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previousMode) })
	database := testutil.NewTestDB(t, &model.Customer{}, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{}, &model.ChannelAccountAuthorization{})
	ctx := edition.WithContext(tenant.WithTenantID(context.Background(), 7), edition.TypeCustom)
	for _, id := range []uint{11, 12, 13} {
		if err := database.WithContext(ctx).Create(&model.Channel{ID: id, TenantID: 7, Code: fmt.Sprintf("crm24-%d", id), Name: "CRM", Type: "web"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	accounts := []model.ChannelAccount{
		{ID: 11, TenantID: 7, ChannelID: 11, IdentifyCode: "stale-a", DataScope: "{}"},
		{ID: 12, TenantID: 7, ChannelID: 12, IdentifyCode: "code-b", DataScope: "{}"},
		{ID: 13, TenantID: 7, ChannelID: 13, IdentifyCode: "shared-one", DataScope: "{}"},
		{ID: 14, TenantID: 7, ChannelID: 13, IdentifyCode: "shared-two", DataScope: "{}"},
	}
	future, past := time.Now().Add(time.Hour), time.Now().Add(-time.Hour)
	codes := []model.ChannelIdentifyCode{
		{TenantID: 7, ChannelID: 11, Code: "code-a", PrevCode: "in-flight-a", PrevValidUntil: &future},
		{TenantID: 7, ChannelID: 12, Code: "code-b"},
		{TenantID: 7, ChannelID: 13, Code: "shared-code"},
	}
	grants := []model.ChannelAccountAuthorization{
		{TenantID: 7, UserID: 3, ChannelAccountID: 11, Role: "owner", VisibleIndustryScope: `["A"]`},
		{TenantID: 7, UserID: 3, ChannelAccountID: 12, Role: "viewer", VisibleIndustryScope: `["B"]`},
		{TenantID: 7, UserID: 3, ChannelAccountID: 13, Role: "viewer", VisibleIndustryScope: `[]`},
		{TenantID: 7, UserID: 3, ChannelAccountID: 99, Role: "viewer", VisibleIndustryScope: `[]`},
	}
	for _, rows := range []any{&accounts, &codes, &grants} {
		if err := database.WithContext(ctx).Create(rows).Error; err != nil {
			t.Fatal(err)
		}
	}
	self, other := uint(3), uint(4)
	rows := []*model.Customer{
		{ID: "stable", SourceAccountID: "11", Industry: "A", OwnerUserID: &self},
		{ID: "current", SourceAccountID: "code-a", Industry: "A", OwnerUserID: &self},
		{ID: "in-flight", SourceAccountID: "in-flight-a", Industry: "A", OwnerUserID: &self},
		{ID: "next", SourceAccountID: "rotated-a", Industry: "A", OwnerUserID: &self},
		{ID: "other-owner", SourceAccountID: "11", Industry: "A", OwnerUserID: &other},
		{ID: "viewer", SourceAccountID: "code-b", Industry: "B", OwnerUserID: &other},
		{ID: "cross-industry", SourceAccountID: "12", Industry: "A", OwnerUserID: &self},
		{ID: "stale", SourceAccountID: "stale-a", Industry: "A", OwnerUserID: &self},
		{ID: "ambiguous", SourceAccountID: "shared-code", Industry: "A", OwnerUserID: &self},
		{ID: "unknown", SourceAccountID: "unknown-alias", Industry: "A", OwnerUserID: &self},
		{ID: "orphan", SourceAccountID: "99", Industry: "A", OwnerUserID: &self},
	}
	for _, row := range rows {
		row.TenantID, row.UnifiedID, row.Name = 7, row.ID, row.ID
	}
	if err := database.WithContext(ctx).Create(rows).Error; err != nil {
		t.Fatal(err)
	}
	permission := &CustomerPermissionService{
		authRepo: repository.NewChannelAccountAuthorizationRepository(), accountRepo: repository.NewChannelAccountRepositoryWithDB(database),
		scopeSvc: NewBusinessScopeServiceWithReaders(nil, nil), customers: repository.NewCRMCustomerRepository(),
	}
	assertVisible := func(want map[string]bool) {
		t.Helper()
		scope, err := permission.Resolve(ctx, 3)
		if err != nil || !reflect.DeepEqual(scope.AccountIDs, []string{"11", "12", "13"}) {
			// 撤权后不再包含账号 11，所有键仍须是数字主键。
			if err != nil || !reflect.DeepEqual(scope.AccountIDs, []string{"12", "13"}) {
				t.Fatalf("生产解析必须仅返回有效数字 ACL，scope=%+v err=%v", scope, err)
			}
		}
		got, total, err := permission.ListScoped(ctx, scope, repository.CustomerScopeFilter{Page: 1, PageSize: 100})
		if err != nil || total != int64(len(want)) || len(got) != len(want) {
			t.Fatalf("生产解析的 SQL 可见集合不一致 rows=%+v total=%d want=%v err=%v", got, total, want, err)
		}
		for _, row := range rows {
			_, readErr := permission.GetByIDScoped(ctx, scope, row.ID)
			owner := uint(0)
			if row.OwnerUserID != nil {
				owner = *row.OwnerUserID
			}
			if (readErr == nil) != want[row.ID] || permission.AllowsWrite(scope, row.SourceAccountID, row.Industry, owner) != want[row.ID] {
				t.Errorf("生产解析的详情/写权限不一致 id=%s want=%v err=%v", row.ID, want[row.ID], readErr)
			}
		}
	}
	assertVisible(map[string]bool{"stable": true, "current": true, "in-flight": true, "viewer": true})
	if err := database.WithContext(ctx).Model(&model.ChannelIdentifyCode{}).Where("channel_id = ?", 11).
		Updates(map[string]any{"code": "rotated-a", "prev_code": "code-a", "prev_valid_until": past}).Error; err != nil {
		t.Fatal(err)
	}
	assertVisible(map[string]bool{"stable": true, "next": true, "viewer": true})
	if err := database.WithContext(ctx).Where("user_id = ? AND channel_account_id = ?", 3, 11).Delete(&model.ChannelAccountAuthorization{}).Error; err != nil {
		t.Fatal(err)
	}
	assertVisible(map[string]bool{"viewer": true})
	permission.scopeSvc = NewBusinessScopeServiceWithReaders(nil, fakeBusinessScopeUserReader{user: &model.SystemUser{Role: model.SystemUserRoleAdmin}})
	admin, err := permission.Resolve(ctx, 3)
	if err != nil || !admin.AllAccounts {
		t.Fatalf("管理员全量作用域必须保留 scope=%+v err=%v", admin, err)
	}
	got, total, err := permission.ListScoped(ctx, admin, repository.CustomerScopeFilter{Page: 1, PageSize: 100})
	if err != nil || total != int64(len(rows)) || len(got) != len(rows) {
		t.Fatalf("管理员应读取本租户全部客户 rows=%d total=%d err=%v", len(got), total, err)
	}
}
