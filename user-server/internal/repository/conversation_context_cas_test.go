package repository

import (
	"context"
	"errors"
	"strings"
	"testing"

	"hivemtk-user/internal/model"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newConversationContextContractDB(t *testing.T) *gorm.DB {
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

func TestConversationContextUpsertUsesVersionCAS(t *testing.T) {
	database := newConversationContextContractDB(t)
	var capturedSQL string
	var capturedVars []any
	if err := database.Callback().Update().After("gorm:update").Register("test:capture_conversation_context_cas", func(tx *gorm.DB) {
		capturedSQL = tx.Statement.SQL.String()
		capturedVars = append([]any{}, tx.Statement.Vars...)
		tx.RowsAffected = 1
	}); err != nil {
		t.Fatal(err)
	}
	repo := NewConversationContextRepositoryWithDB(database)
	m := &model.ConversationContext{ID: 42, ChannelAccountID: 7, ExternalConversationID: "conv-1", Version: 3, SummaryVersion: 2}
	if err := repo.Upsert(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"WHERE tenant_id =", "AND id =", "AND version =", "version ="} {
		if !strings.Contains(capturedSQL, fragment) {
			t.Fatalf("CAS SQL 缺少 %q：%s", fragment, capturedSQL)
		}
	}
	if len(capturedVars) < 3 {
		t.Fatalf("CAS 参数不足：%#v", capturedVars)
	}
	whereVars := capturedVars[len(capturedVars)-3:]
	if whereVars[0] != uint(1) || whereVars[1] != int64(42) || whereVars[2] != int64(3) {
		t.Fatalf("CAS 租户/ID/期望版本参数错误：%#v", whereVars)
	}
	if m.Version != 4 {
		t.Fatalf("CAS 成功后必须递增内存版本：got %d, want 4", m.Version)
	}
}

func TestConversationContextUpsertPersistsVersionIncrement(t *testing.T) {
	database := newConversationContextContractDB(t)
	var capturedSQL string
	if err := database.Callback().Update().After("gorm:update").Register("test:capture_conversation_context_version_write", func(tx *gorm.DB) {
		capturedSQL = tx.Statement.SQL.String()
		tx.RowsAffected = 1
	}); err != nil {
		t.Fatal(err)
	}
	repo := NewConversationContextRepositoryWithDB(database)
	m := &model.ConversationContext{ID: 42, ChannelAccountID: 7, ExternalConversationID: "conv-1", Version: 1}
	if err := repo.Upsert(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(capturedSQL, `"version"=`) || !strings.Contains(capturedSQL, "AND version =") {
		t.Fatalf("CAS 必须同时更新 version 列，否则下次比较永远落后：%s", capturedSQL)
	}
	if m.Version != 2 {
		t.Fatalf("内存版本未递增：got %d", m.Version)
	}
}

func TestConversationContextUpsertReturnsVersionConflict(t *testing.T) {
	database := newConversationContextContractDB(t)
	if err := database.Callback().Update().After("gorm:update").Register("test:force_conversation_context_conflict", func(tx *gorm.DB) {
		tx.RowsAffected = 0
	}); err != nil {
		t.Fatal(err)
	}
	repo := NewConversationContextRepositoryWithDB(database)
	m := &model.ConversationContext{ID: 42, ChannelAccountID: 7, ExternalConversationID: "conv-1", Version: 3}
	if err := repo.Upsert(context.Background(), m); !errors.Is(err, ErrConversationContextVersionConflict) {
		t.Fatalf("0 行更新必须返回版本冲突，实际 %v", err)
	}
	if m.Version != 3 {
		t.Fatalf("冲突时不得修改调用方版本：got %d", m.Version)
	}
}

func TestConversationContextCreateMapsDuplicateToVersionConflict(t *testing.T) {
	database := newConversationContextContractDB(t)
	var createdVersion int64
	if err := database.Callback().Create().After("gorm:create").Register("test:force_conversation_context_duplicate", func(tx *gorm.DB) {
		if m, ok := tx.Statement.Dest.(*model.ConversationContext); ok {
			createdVersion = m.Version
		}
		tx.AddError(gorm.ErrDuplicatedKey)
	}); err != nil {
		t.Fatal(err)
	}
	repo := NewConversationContextRepositoryWithDB(database)
	m := &model.ConversationContext{ChannelAccountID: 7, ExternalConversationID: "conv-1"}
	if err := repo.Upsert(context.Background(), m); !errors.Is(err, ErrConversationContextVersionConflict) {
		t.Fatalf("首建唯一冲突必须返回版本冲突供上层重读，实际 %v", err)
	}
	if createdVersion != 1 || m.SummaryVersion != 1 {
		t.Fatalf("首建默认版本错误：version=%d summary_version=%d", createdVersion, m.SummaryVersion)
	}
}

func TestConversationContextUpsertRejectsNil(t *testing.T) {
	repo := NewConversationContextRepositoryWithDB(newConversationContextContractDB(t))
	if err := repo.Upsert(context.Background(), nil); err == nil {
		t.Fatal("空上下文必须拒绝")
	}
}
