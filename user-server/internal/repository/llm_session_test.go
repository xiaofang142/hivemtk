package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newLLMSessionContractDB(t *testing.T) *gorm.DB {
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

func testLLMSessionQuery() LLMSessionStateQuery {
	return LLMSessionStateQuery{
		Platform: "telegram", ChannelAccountID: 3, LLMConversationID: "llmconv-1", ScopeHash: "scope-1",
		BlindIndexes: []string{"blind-new", "blind-old"}, ExecutionScenarioID: "sales_generate",
		Provider: "openai", Model: "gpt-test", Protocol: "responses", Region: "global",
	}
}

func TestLLMSessionStateRepositoryLookupIsFullyScoped(t *testing.T) {
	database := newLLMSessionContractDB(t)
	var capturedSQL string
	var capturedVars []any
	if err := database.Callback().Query().After("gorm:query").Register("test:capture_llm_session_lookup", func(tx *gorm.DB) {
		capturedSQL = tx.Statement.SQL.String()
		capturedVars = append([]any{}, tx.Statement.Vars...)
	}); err != nil {
		t.Fatal(err)
	}
	repo := NewLLMSessionStateRepositoryWithDB(database)
	item, err := repo.GetLatest(tenant.WithTenantID(context.Background(), 7), testLLMSessionQuery())
	if err != nil {
		t.Fatal(err)
	}
	if item != nil {
		t.Fatalf("DryRun 未命中应返回 nil，实际 %+v", item)
	}
	for _, fragment := range []string{"tenant_id =", "platform =", "channel_account_id =", "llm_conversation_id =", "scope_hash =", "bus_llm_session_id_blind IN", "execution_scenario_id =", "provider =", "model =", "protocol =", "region =", "ORDER BY generation DESC"} {
		if !strings.Contains(capturedSQL, fragment) {
			t.Fatalf("lookup 缺少范围条件 %q：%s", fragment, capturedSQL)
		}
	}
	if len(capturedVars) < 12 || capturedVars[0] != uint(7) || capturedVars[1] != "telegram" || capturedVars[2] != uint(3) {
		t.Fatalf("lookup 租户/平台/账号参数错误：%#v", capturedVars)
	}
}

func TestLLMSessionStateRepositoryRejectsIncompleteLookup(t *testing.T) {
	repo := NewLLMSessionStateRepositoryWithDB(newLLMSessionContractDB(t))
	query := testLLMSessionQuery()
	query.BlindIndexes = nil
	if _, err := repo.GetLatest(tenant.WithTenantID(context.Background(), 7), query); !errors.Is(err, ErrLLMSessionStateScope) {
		t.Fatalf("缺 blind index 必须 fail-closed，实际 %v", err)
	}
	query = testLLMSessionQuery()
	query.ScopeHash = ""
	if _, err := repo.GetLatest(tenant.WithTenantID(context.Background(), 7), query); !errors.Is(err, ErrLLMSessionStateScope) {
		t.Fatalf("缺 scope hash 必须 fail-closed，实际 %v", err)
	}
}

func TestLLMSessionStateRepositoryUpdateUsesCASAndPersistsVersion(t *testing.T) {
	database := newLLMSessionContractDB(t)
	var capturedSQL string
	var capturedVars []any
	if err := database.Callback().Update().After("gorm:update").Register("test:capture_llm_session_cas", func(tx *gorm.DB) {
		capturedSQL = tx.Statement.SQL.String()
		capturedVars = append([]any{}, tx.Statement.Vars...)
		tx.RowsAffected = 1
	}); err != nil {
		t.Fatal(err)
	}
	repo := NewLLMSessionStateRepositoryWithDB(database)
	item := &model.LLMSessionState{ID: 42, TenantID: 7, Version: 2, ProviderContinuationRefEnc: []byte("enc-ref"), LastMessageCursor: "hub:9"}
	if err := repo.Update(tenant.WithTenantID(context.Background(), 7), item); err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"\"version\"=", "provider_continuation_ref_enc", "last_message_cursor", "WHERE tenant_id =", "id =", "version ="} {
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

func TestLLMSessionStateRepositoryUpdateReturnsConflict(t *testing.T) {
	database := newLLMSessionContractDB(t)
	if err := database.Callback().Update().After("gorm:update").Register("test:force_llm_session_conflict", func(tx *gorm.DB) {
		tx.RowsAffected = 0
	}); err != nil {
		t.Fatal(err)
	}
	repo := NewLLMSessionStateRepositoryWithDB(database)
	item := &model.LLMSessionState{ID: 42, Version: 2}
	if err := repo.Update(tenant.WithTenantID(context.Background(), 7), item); !errors.Is(err, ErrLLMSessionStateConflict) {
		t.Fatalf("0 行更新必须返回冲突，实际 %v", err)
	}
	if item.Version != 2 {
		t.Fatalf("冲突时不得修改版本：got %d", item.Version)
	}
}

func TestLLMSessionStateRepositoryCreateMapsDuplicate(t *testing.T) {
	database := newLLMSessionContractDB(t)
	if err := database.Callback().Create().After("gorm:create").Register("test:force_llm_session_duplicate", func(tx *gorm.DB) {
		tx.AddError(gorm.ErrDuplicatedKey)
	}); err != nil {
		t.Fatal(err)
	}
	repo := NewLLMSessionStateRepositoryWithDB(database)
	item := &model.LLMSessionState{TenantID: 7}
	if err := repo.Create(tenant.WithTenantID(context.Background(), 7), item); !errors.Is(err, ErrLLMSessionStateConflict) {
		t.Fatalf("首建唯一冲突必须映射为冲突，实际 %v", err)
	}
	if item.Generation != 1 || item.Version != 1 {
		t.Fatalf("首建默认 generation/version 错误：%+v", item)
	}
}

func TestLLMSessionStateRepositoryNextGenerationIsScoped(t *testing.T) {
	database := newLLMSessionContractDB(t)
	var capturedSQL string
	if err := database.Callback().Query().After("gorm:query").Register("test:capture_llm_session_generation", func(tx *gorm.DB) {
		capturedSQL = tx.Statement.SQL.String()
	}); err != nil {
		t.Fatal(err)
	}
	repo := NewLLMSessionStateRepositoryWithDB(database)
	if _, err := repo.NextGeneration(tenant.WithTenantID(context.Background(), 7), testLLMSessionQuery()); err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"SELECT \"generation\"", "ORDER BY generation DESC", "LIMIT", "tenant_id =", "bus_llm_session_id_blind IN", "execution_scenario_id =", "scope_hash ="} {
		if !strings.Contains(capturedSQL, fragment) {
			t.Fatalf("generation 查询缺少 %q：%s", fragment, capturedSQL)
		}
	}
}

func TestLLMSessionStateRepositoryDeleteExpiredIsTenantScoped(t *testing.T) {
	database := newLLMSessionContractDB(t)
	var capturedSQL string
	var capturedVars []any
	if err := database.Callback().Delete().After("gorm:delete").Register("test:capture_llm_session_expiry", func(tx *gorm.DB) {
		capturedSQL = tx.Statement.SQL.String()
		capturedVars = append([]any{}, tx.Statement.Vars...)
		tx.RowsAffected = 3
	}); err != nil {
		t.Fatal(err)
	}
	repo := NewLLMSessionStateRepositoryWithDB(database)
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	deleted, err := repo.DeleteExpired(tenant.WithTenantID(context.Background(), 7), now, 100)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 3 {
		t.Fatalf("清理数量错误：got %d", deleted)
	}
	for _, fragment := range []string{"DELETE FROM \"llm_session_states\"", "tenant_id =", "expires_at <=", "LIMIT"} {
		if !strings.Contains(capturedSQL, fragment) {
			t.Fatalf("清理 SQL 缺少 %q：%s", fragment, capturedSQL)
		}
	}
	if len(capturedVars) < 3 || capturedVars[0] != uint(7) {
		t.Fatalf("清理租户参数错误：%#v", capturedVars)
	}
	foundLimit := false
	for _, value := range capturedVars {
		if value == 100 {
			foundLimit = true
			break
		}
	}
	if !foundLimit {
		t.Fatalf("清理 limit 未进入 SQL 参数：%#v", capturedVars)
	}
}
