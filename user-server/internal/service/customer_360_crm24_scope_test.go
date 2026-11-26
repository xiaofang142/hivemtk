package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
)

func TestCustomer360Service_GetByCustomerIDScoped_EnforcesCRMAccountPair(t *testing.T) {
	db := setupCustomer360TestDB(t)
	if db == nil {
		t.Fatal("CRM-24 customer-record scope test requires PostgreSQL")
	}
	service := setupCustomer360Service(t, db)
	if err := db.Create(&model.Customer{
		ID:              "crm24-forbidden-customer",
		UnifiedID:       "crm24-forbidden-customer",
		Name:            "forbidden",
		SourceAccountID: "62002",
		Industry:        "B",
	}).Error; err != nil {
		t.Fatalf("seed customer failed: %v", err)
	}

	scope := CustomerDataScope{
		UserID:             7,
		RestrictedAccounts: true,
		AccountIDs:         []string{"62001"},
		AccountRules:       []CustomerAccountRule{{AccountID: "62001", Industries: []string{"A"}}},
	}
	_, err := service.GetCustomer360ByCustomerIDScoped(context.Background(), scope, "crm24-forbidden-customer")
	if err != ErrCRMCustomerForbidden {
		t.Fatalf("customer-record 360 must enforce CRM account pair, err=%v", err)
	}
}
