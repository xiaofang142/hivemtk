package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// setupMultiEntityDedupeDB 覆盖三类实体 + 合并审计所需表。
func setupMultiEntityDedupeDB(t *testing.T) *gorm.DB {
	t.Helper()
	database := testutil.NewTestDB(t,
		&model.Customer{}, &model.CustomerEvent{}, &model.CustomerTag{}, &model.CustomerSession{},
		&model.CustomerChannel{}, &model.CustomerDoNotContact{}, &model.CSATSurvey{},
		&model.Clue{}, &model.ScriptExposureLog{}, &model.OperationLog{},
		&model.ContactConsentIdentityLink{}, &model.ContactConsentEvent{}, &model.ContactPreference{},
		&model.Contact{}, &model.Supplier{}, &model.ServiceProvider{}, &model.CustomerMerge{},
	)
	db.SetTestDB(database)
	return database
}

func entityContactInfo(t *testing.T, phone, email string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"phone": phone, "email": email})
	if err != nil {
		t.Fatalf("序列化实体联系方式失败: %v", err)
	}
	return string(raw)
}

func addContactForDedupe(ctx context.Context, t *testing.T, name, customerID, info string) *model.Contact {
	t.Helper()
	row := &model.Contact{Name: name, CustomerID: customerID, ContactInfo: info, Role: "采购"}
	if err := repository.NewContactRepository().Create(ctx, row); err != nil {
		t.Fatalf("创建联系人夹具失败: %v", err)
	}
	return row
}

func addSupplierForDedupe(ctx context.Context, t *testing.T, name, customerID, info string) *model.Supplier {
	t.Helper()
	row := &model.Supplier{Name: name, CustomerID: customerID, ContactInfo: info, Role: "供货"}
	if err := repository.NewSupplierRepository().Create(ctx, row); err != nil {
		t.Fatalf("创建供应商夹具失败: %v", err)
	}
	return row
}

func addProviderForDedupe(ctx context.Context, t *testing.T, name, customerID, info string) *model.ServiceProvider {
	t.Helper()
	row := &model.ServiceProvider{Name: name, CustomerID: customerID, ContactInfo: info, Role: "实施"}
	if err := repository.NewServiceProviderRepository().Create(ctx, row); err != nil {
		t.Fatalf("创建服务商夹具失败: %v", err)
	}
	return row
}

func createDedupeCustomer(t *testing.T, id, name, account, phone string) *model.Customer {
	t.Helper()
	c := &model.Customer{ID: id, Name: name, SourceAccountID: account, Phone: phone, Tags: "[]", ChurnRisk: "low"}
	if err := repository.NewCustomerRepository().Create(context.Background(), c); err != nil {
		t.Fatalf("创建客户夹具失败: %v", err)
	}
	return c
}

func hasEntityCandidate(rows []DuplicateCandidate, kind, leftID, rightID string) bool {
	for _, row := range rows {
		if row.EntityType == kind && row.LeftID == leftID && row.RightID == rightID {
			return true
		}
	}
	return false
}

// CRM-06 正例：供应商实体相同手机号命中规则，返回实体类型与命中字段；负例：不同手机号的供应商不成对。
func TestMultiEntityDedupe_SupplierPhoneHitAndNonDuplicate(t *testing.T) {
	setupMultiEntityDedupeDB(t)
	ctx := tenant.WithTenantID(context.Background(), 1)
	createDedupeCustomer(t, "dd-c-1", "客户甲", "acct-a", "")
	createDedupeCustomer(t, "dd-c-2", "客户乙", "acct-a", "")
	a := addSupplierForDedupe(ctx, t, "供应商甲", "dd-c-1", entityContactInfo(t, "13800000001", ""))
	b := addSupplierForDedupe(ctx, t, "供应商乙", "dd-c-2", entityContactInfo(t, "13800000001", ""))
	c := addSupplierForDedupe(ctx, t, "无关供应商", "dd-c-2", entityContactInfo(t, "13800000009", ""))

	rows, err := NewCustomerDedupeService().FindEntitiesDuplicates(ctx, DedupeEntityScanOptions{
		Scope: CustomerDataScope{AllAccounts: true, UserID: 7}, EntityTypes: []string{DedupeEntitySupplier},
	})
	if err != nil {
		t.Fatalf("供应商去重扫描失败: %v", err)
	}
	if !hasEntityCandidate(rows, DedupeEntitySupplier, fmt.Sprint(a.ID), fmt.Sprint(b.ID)) {
		t.Fatalf("期望相同手机号的供应商候选成对返回，实际=%+v", rows)
	}
	if hasEntityCandidate(rows, DedupeEntitySupplier, fmt.Sprint(a.ID), fmt.Sprint(c.ID)) {
		t.Fatalf("不同手机号的供应商不应成为重复候选: %+v", rows)
	}
}

// CRM-06 正例：联系人/服务商分别按邮箱、标识适配。
func TestMultiEntityDedupe_ContactAndServiceProviderAdapters(t *testing.T) {
	setupMultiEntityDedupeDB(t)
	ctx := tenant.WithTenantID(context.Background(), 1)
	createDedupeCustomer(t, "dd-c-3", "客户丙", "acct-a", "")
	createDedupeCustomer(t, "dd-c-4", "客户丁", "acct-a", "")
	ca := addContactForDedupe(ctx, t, "联系人甲", "dd-c-3", entityContactInfo(t, "", "buyer@example.test"))
	cb := addContactForDedupe(ctx, t, "联系人乙", "dd-c-4", entityContactInfo(t, "", "buyer@example.test"))
	pa := addProviderForDedupe(ctx, t, "服务商甲", "dd-c-3", entityContactInfo(t, "13900000001", ""))
	pb := addProviderForDedupe(ctx, t, "服务商乙", "dd-c-4", entityContactInfo(t, "13900000001", ""))

	rows, err := NewCustomerDedupeService().FindEntitiesDuplicates(ctx, DedupeEntityScanOptions{
		Scope: CustomerDataScope{AllAccounts: true, UserID: 7}, EntityTypes: []string{DedupeEntityContact, DedupeEntityServiceProvider},
	})
	if err != nil {
		t.Fatalf("联系人/服务商去重扫描失败: %v", err)
	}
	if !hasEntityCandidate(rows, DedupeEntityContact, fmt.Sprint(ca.ID), fmt.Sprint(cb.ID)) {
		t.Fatalf("期望相同邮箱的联系人候选: %+v", rows)
	}
	if !hasEntityCandidate(rows, DedupeEntityServiceProvider, fmt.Sprint(pa.ID), fmt.Sprint(pb.ID)) {
		t.Fatalf("期望相同手机号的服务商候选: %+v", rows)
	}
}

// CRM-06 反例：同手机号但承担供应商/服务商不同业务角色，只给 review 且带角色冲突说明，不自动抹去角色。
func TestMultiEntityDedupe_RoleCoexistenceNotAutoMerged(t *testing.T) {
	setupMultiEntityDedupeDB(t)
	ctx := tenant.WithTenantID(context.Background(), 1)
	createDedupeCustomer(t, "dd-c-5", "客户戊", "acct-a", "")
	createDedupeCustomer(t, "dd-c-6", "客户己", "acct-a", "")
	s := addSupplierForDedupe(ctx, t, "张三-供货", "dd-c-5", entityContactInfo(t, "13700000001", ""))
	p := addProviderForDedupe(ctx, t, "张三-实施", "dd-c-6", entityContactInfo(t, "13700000001", ""))

	rows, err := NewCustomerDedupeService().FindEntitiesDuplicates(ctx, DedupeEntityScanOptions{
		Scope: CustomerDataScope{AllAccounts: true, UserID: 7},
	})
	if err != nil {
		t.Fatalf("角色并存扫描失败: %v", err)
	}
	found := false
	for _, row := range rows {
		if row.EntityType == DedupeEntitySupplier && row.LeftID == fmt.Sprint(s.ID) && row.RightID == fmt.Sprint(p.ID) {
			found = true
			if row.Action != "review" || !strings.Contains(row.Reason, "角色") {
				t.Fatalf("跨角色同手机号必须 review 且提示角色冲突: %+v", row)
			}
		}
	}
	if !found {
		t.Fatalf("期望跨角色候选仍返回供人工判断: %+v", rows)
	}
}

// CRM-06 反例：越权客户下的实体不得出现在候选；不支持实体类型显式标记。
func TestMultiEntityDedupe_ScopeAndUnsupportedMarking(t *testing.T) {
	setupMultiEntityDedupeDB(t)
	ctx := tenant.WithTenantID(context.Background(), 1)
	createDedupeCustomer(t, "dd-c-7", "客户庚", "acct-allowed", "")
	createDedupeCustomer(t, "dd-c-8", "客户辛", "acct-denied", "")
	a := addSupplierForDedupe(ctx, t, "可见供应商甲", "dd-c-7", entityContactInfo(t, "13600000001", ""))
	a2 := addSupplierForDedupe(ctx, t, "可见供应商乙", "dd-c-7", entityContactInfo(t, "13600000001", ""))
	b := addSupplierForDedupe(ctx, t, "越权供应商", "dd-c-8", entityContactInfo(t, "13600000001", ""))

	rows, err := NewCustomerDedupeService().FindEntitiesDuplicates(ctx, DedupeEntityScanOptions{
		Scope: CustomerDataScope{UserID: 7, RestrictedAccounts: true, AccountIDs: []string{"acct-allowed"}},
	})
	if err != nil {
		t.Fatalf("受限范围扫描失败: %v", err)
	}
	for _, row := range rows {
		if row.LeftID == fmt.Sprint(b.ID) || row.RightID == fmt.Sprint(b.ID) {
			t.Fatalf("越权客户下实体不得出现在候选: %+v", row)
		}
	}
	if !hasEntityCandidate(rows, DedupeEntitySupplier, fmt.Sprint(a.ID), fmt.Sprint(a2.ID)) {
		t.Fatalf("可见范围内相同手机号的供应商应成对返回，实际=%+v", rows)
	}

	unsupported, err := NewCustomerDedupeService().FindEntitiesDuplicates(ctx, DedupeEntityScanOptions{
		Scope: CustomerDataScope{AllAccounts: true, UserID: 7}, EntityTypes: []string{"Company"},
	})
	if err != nil {
		t.Fatalf("不支持实体扫描失败: %v", err)
	}
	if len(unsupported) != 1 || unsupported[0].Unsupported != "Company" || unsupported[0].Action != "unsupported" {
		t.Fatalf("不支持实体必须显式标记: %+v", unsupported)
	}
}
