package repository

import (
	"context"
	"errors"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newConversationDispositionContractDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(
		postgres.New(postgres.Config{DSN: "host=127.0.0.1 port=8217 user=contract dbname=contract sslmode=disable"}),
		&gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)},
	)
	if err != nil {
		t.Fatal(err)
	}
	return database
}

func TestConversationDispositionRepositorySaveUsesCAS(t *testing.T) {
	database := newConversationDispositionContractDB(t)
	var capturedSQL string
	var capturedVars []any
	if err := database.Callback().Update().After("gorm:update").Register("test:capture_disposition_cas", func(tx *gorm.DB) {
		capturedSQL = tx.Statement.SQL.String()
		capturedVars = append([]any{}, tx.Statement.Vars...)
		tx.RowsAffected = 1
	}); err != nil {
		t.Fatal(err)
	}
	repo := NewConversationDispositionRepositoryWithDB(database)
	item := &model.ConversationMessageDisposition{
		ID: 42, TenantID: 7, Platform: "telegram", ChannelAccountID: 3,
		ConversationID: "dm-1", MessageID: "hub:1", MessageCursor: "hub:1",
		BusLLMSessionIDBlind: "blind-1", Status: model.ConversationDispositionProcessing, Version: 2,
	}
	if err := repo.Save(tenant.WithTenantID(context.Background(), 7), item); err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"WHERE tenant_id =", "AND id =", "AND version ="} {
		if !strings.Contains(capturedSQL, fragment) {
			t.Fatalf("CAS SQL 缺少 %q：%s", fragment, capturedSQL)
		}
	}
	if len(capturedVars) < 3 {
		t.Fatalf("CAS 参数缺失：%#v", capturedVars)
	}
	tail := capturedVars[len(capturedVars)-3:]
	if tail[0] != uint(7) || tail[1] != uint(42) || tail[2] != int64(2) {
		t.Fatalf("CAS 租户/ID/期望版本参数错误：%#v", tail)
	}
	if item.Version != 3 {
		t.Fatalf("CAS 成功后必须递增内存版本：got %d", item.Version)
	}
}

func TestConversationDispositionRepositorySaveReturnsConflict(t *testing.T) {
	database := newConversationDispositionContractDB(t)
	if err := database.Callback().Update().After("gorm:update").Register("test:force_disposition_conflict", func(tx *gorm.DB) {
		tx.RowsAffected = 0
	}); err != nil {
		t.Fatal(err)
	}
	repo := NewConversationDispositionRepositoryWithDB(database)
	item := &model.ConversationMessageDisposition{ID: 42, Version: 2}
	err := repo.Save(tenant.WithTenantID(context.Background(), 7), item)
	if !errors.Is(err, ErrConversationDispositionVersionConflict) {
		t.Fatalf("0 行更新必须返回版本冲突，实际 %v", err)
	}
	if item.Version != 2 {
		t.Fatalf("冲突时不得修改版本：got %d", item.Version)
	}
}

func TestConversationDispositionRepositoryCreateMapsDuplicate(t *testing.T) {
	database := newConversationDispositionContractDB(t)
	if err := database.Callback().Create().After("gorm:create").Register("test:force_disposition_duplicate", func(tx *gorm.DB) {
		tx.AddError(gorm.ErrDuplicatedKey)
	}); err != nil {
		t.Fatal(err)
	}
	repo := NewConversationDispositionRepositoryWithDB(database)
	item := &model.ConversationMessageDisposition{Platform: "telegram", ChannelAccountID: 3, ConversationID: "dm-1", MessageID: "hub:1"}
	err := repo.Save(tenant.WithTenantID(context.Background(), 7), item)
	if !errors.Is(err, ErrConversationDispositionVersionConflict) {
		t.Fatalf("首建唯一冲突必须映射为版本冲突，实际 %v", err)
	}
}

func TestConversationDispositionRepositoryListIsTenantScoped(t *testing.T) {
	database := newConversationDispositionContractDB(t)
	var capturedSQL string
	var capturedVars []any
	if err := database.Callback().Query().After("gorm:query").Register("test:capture_disposition_scope", func(tx *gorm.DB) {
		capturedSQL = tx.Statement.SQL.String()
		capturedVars = append([]any{}, tx.Statement.Vars...)
	}); err != nil {
		t.Fatal(err)
	}
	repo := NewConversationDispositionRepositoryWithDB(database)
	items, err := repo.List(tenant.WithTenantID(context.Background(), 7), 7, "telegram", 3, "dm-1")
	if err != nil {
		t.Fatal(err)
	}
	if items == nil {
		t.Fatal("返回切片必须初始化")
	}
	for _, fragment := range []string{"tenant_id =", "platform =", "channel_account_id =", "conversation_id ="} {
		if !strings.Contains(capturedSQL, fragment) {
			t.Fatalf("查询缺少范围条件 %q：%s", fragment, capturedSQL)
		}
	}
	if len(capturedVars) < 4 || capturedVars[0] != uint(7) || capturedVars[1] != "telegram" || capturedVars[2] != uint(3) || capturedVars[3] != "dm-1" {
		t.Fatalf("租户范围参数错误：%#v", capturedVars)
	}
}
