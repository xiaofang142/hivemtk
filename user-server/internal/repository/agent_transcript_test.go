package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/secrets"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupAgentTranscriptCrypto(t *testing.T) {
	t.Helper()
	t.Setenv("MASTER_KEY", "0123456789abcdef0123456789abcdef")
	secrets.ResetForTest()
	if err := secrets.InitFromEnv(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(secrets.ResetForTest)
}

func newAgentTranscriptContractDB(t *testing.T) *gorm.DB {
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

func hashContract(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func contractInt(value any) int64 {
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return reflected.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return int64(reflected.Uint())
	default:
		return -1
	}
}

func TestAgentTranscriptRepositoryEncryptsPayloadAndHash(t *testing.T) {
	setupAgentTranscriptCrypto(t)
	database := newAgentTranscriptContractDB(t)
	var sql string
	var vars []any
	if err := database.Callback().Create().After("gorm:create").Register("test:capture_transcript", func(tx *gorm.DB) {
		sql = tx.Statement.SQL.String()
		vars = append([]any{}, tx.Statement.Vars...)
	}); err != nil {
		t.Fatal(err)
	}
	repo := NewAgentTranscriptRepositoryWithDB(database)
	expires := time.Now().Add(time.Hour)
	entry := &model.ScenarioTranscript{
		TenantID: 7, LLMSessionStateID: 11, SnapshotID: "snap-1", ExecutionScenarioID: "sales_generate",
		SequenceNo: 1, EntryType: model.TranscriptModelStep, RequestIdempotencyKey: "req-1", ExpiresAt: expires,
	}
	payload := "客户说预算五十万"
	if err := repo.CreateTranscript(tenant.WithTenantID(context.Background(), 7), entry, payload); err != nil {
		t.Fatal(err)
	}
	if len(entry.PayloadEnc) == 0 || string(entry.PayloadEnc) == payload {
		t.Fatalf("payload 必须加密存储：%q", entry.PayloadEnc)
	}
	if entry.PayloadHash != hashContract(payload) {
		t.Fatalf("payload hash 错误：%q", entry.PayloadHash)
	}
	if !strings.Contains(sql, "INSERT INTO \"scenario_transcripts\"") {
		t.Fatalf("未写入 transcript 表：%s", sql)
	}
	if strings.Contains(fmt.Sprint(vars), payload) {
		t.Fatal("SQL 参数不得含明文 payload")
	}
}

func TestAgentTranscriptRepositoryRejectsInvalidTranscript(t *testing.T) {
	setupAgentTranscriptCrypto(t)
	repo := NewAgentTranscriptRepositoryWithDB(newAgentTranscriptContractDB(t))
	ctx := tenant.WithTenantID(context.Background(), 7)
	base := model.ScenarioTranscript{TenantID: 7, LLMSessionStateID: 11, SnapshotID: "snap-1", ExecutionScenarioID: "sales_generate", SequenceNo: 1, EntryType: model.TranscriptModelStep, RequestIdempotencyKey: "req-1", ExpiresAt: time.Now().Add(time.Hour)}
	if err := repo.CreateTranscript(ctx, &base, ""); err == nil {
		t.Fatal("空 payload 必须拒绝")
	}
	missingKey := base
	missingKey.RequestIdempotencyKey = ""
	if err := repo.CreateTranscript(ctx, &missingKey, "payload"); err == nil {
		t.Fatal("缺少幂等键必须拒绝")
	}
	expired := base
	expired.ExpiresAt = time.Now().Add(-time.Hour)
	if err := repo.CreateTranscript(ctx, &expired, "payload"); err == nil {
		t.Fatal("过期 transcript 必须拒绝")
	}
}

func TestAgentTranscriptRepositoryCreateMapsDuplicateToConflict(t *testing.T) {
	setupAgentTranscriptCrypto(t)
	database := newAgentTranscriptContractDB(t)
	if err := database.Callback().Create().After("gorm:create").Register("test:force_transcript_duplicate", func(tx *gorm.DB) {
		tx.AddError(gorm.ErrDuplicatedKey)
	}); err != nil {
		t.Fatal(err)
	}
	repo := NewAgentTranscriptRepositoryWithDB(database)
	entry := &model.ScenarioTranscript{TenantID: 7, LLMSessionStateID: 11, SnapshotID: "snap-1", ExecutionScenarioID: "sales_generate", SequenceNo: 1, EntryType: model.TranscriptModelStep, RequestIdempotencyKey: "req-1", ExpiresAt: time.Now().Add(time.Hour)}
	if err := repo.CreateTranscript(tenant.WithTenantID(context.Background(), 7), entry, "payload"); !errors.Is(err, ErrAgentTranscriptConflict) {
		t.Fatalf("唯一冲突必须映射为 sentinel，实际 %v", err)
	}
	if entry.Generation != 1 {
		t.Fatalf("默认 generation 错误：%d", entry.Generation)
	}
}

func TestAgentTranscriptRepositoryListIsScoped(t *testing.T) {
	database := newAgentTranscriptContractDB(t)
	var sql string
	var vars []any
	if err := database.Callback().Query().After("gorm:query").Register("test:capture_transcript_list", func(tx *gorm.DB) {
		sql = tx.Statement.SQL.String()
		vars = append([]any{}, tx.Statement.Vars...)
	}); err != nil {
		t.Fatal(err)
	}
	repo := NewAgentTranscriptRepositoryWithDB(database)
	rows, err := repo.ListTranscripts(tenant.WithTenantID(context.Background(), 7), 11, 2, 5, 20)
	if err != nil {
		t.Fatal(err)
	}
	if rows == nil {
		t.Fatal("返回切片必须初始化")
	}
	for _, fragment := range []string{"tenant_id =", "llm_session_state_id =", "generation =", "sequence_no >=", "ORDER BY sequence_no ASC", "LIMIT"} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("list SQL 缺少 %q：%s", fragment, sql)
		}
	}
	if len(vars) < 5 || contractInt(vars[0]) != 7 || contractInt(vars[1]) != 11 || contractInt(vars[2]) != 2 || contractInt(vars[3]) != 5 || contractInt(vars[4]) != 20 {
		t.Fatalf("list 参数错误：%#v", vars)
	}
}

func TestAgentTranscriptRepositoryToolIdempotency(t *testing.T) {
	setupAgentTranscriptCrypto(t)
	repo := NewAgentTranscriptRepositoryWithDB(newAgentTranscriptContractDB(t))
	ctx := tenant.WithTenantID(context.Background(), 7)
	highWrite := &model.ToolInvocation{TenantID: 7, TranscriptID: 3, ToolName: "send_message", RiskClass: model.ToolRiskHighWrite, Status: model.ToolInvocationPlanned, AuthorizationHash: "auth", RequestHash: hashContract("args")}
	if err := repo.CreateToolInvocation(ctx, highWrite, "args"); err == nil {
		t.Fatal("high_write 工具必须带幂等键")
	}
	highWrite.ToolIdempotencyKey = "tool-key-1"
	if err := repo.CreateToolInvocation(ctx, highWrite, "args"); err != nil {
		t.Fatal(err)
	}
	if highWrite.AttemptCount != 1 {
		t.Fatalf("首次 attempt_count 错误：%d", highWrite.AttemptCount)
	}
}

func TestAgentTranscriptRepositorySettlesToolResultEncrypted(t *testing.T) {
	setupAgentTranscriptCrypto(t)
	database := newAgentTranscriptContractDB(t)
	var sql string
	if err := database.Callback().Update().After("gorm:update").Register("test:capture_tool_settle", func(tx *gorm.DB) {
		sql = tx.Statement.SQL.String()
		tx.RowsAffected = 1
	}); err != nil {
		t.Fatal(err)
	}
	repo := NewAgentTranscriptRepositoryWithDB(database)
	tool := &model.ToolInvocation{ID: 5, TenantID: 7, TranscriptID: 3, Status: model.ToolInvocationConfirmed, ResultHash: "should-overwrite", ExternalReceiptID: "receipt-1", LastErrorCode: "should-clear"}
	result := "工具已发送"
	if err := repo.SettleToolInvocation(tenant.WithTenantID(context.Background(), 7), tool, model.ToolInvocationDispatched, result); err != nil {
		t.Fatal(err)
	}
	if len(tool.ResultEnc) == 0 || string(tool.ResultEnc) == result {
		t.Fatalf("tool result 必须加密：%q", tool.ResultEnc)
	}
	if tool.ResultHash != hashContract(result) || tool.ExternalReceiptID != "receipt-1" {
		t.Fatalf("tool hash/receipt 错误：%+v", tool)
	}
	for _, fragment := range []string{"tenant_id =", "id =", "status =", "result_enc", "result_hash"} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("settle SQL 缺少 %q：%s", fragment, sql)
		}
	}
}

func TestAgentTranscriptRepositoryUnknownHighWriteCannotSettle(t *testing.T) {
	setupAgentTranscriptCrypto(t)
	repo := NewAgentTranscriptRepositoryWithDB(newAgentTranscriptContractDB(t))
	tool := &model.ToolInvocation{ID: 5, TenantID: 7, TranscriptID: 3, ToolName: "send_message", RiskClass: model.ToolRiskHighWrite, Status: model.ToolInvocationUnknown, ToolIdempotencyKey: "tool-1", AuthorizationHash: "auth", RequestHash: hashContract("args")}
	if err := repo.SettleToolInvocation(tenant.WithTenantID(context.Background(), 7), tool, model.ToolInvocationUnknown, "result"); !errors.Is(err, ErrAgentTranscriptConflict) {
		t.Fatalf("unknown 高写工具禁止盲确认，实际 %v", err)
	}
}

func TestAgentTranscriptRepositoryDeleteExpiredTouchesBothTables(t *testing.T) {
	database := newAgentTranscriptContractDB(t)
	statements := make([]string, 0)
	if err := database.Callback().Delete().After("gorm:delete").Register("test:capture_transcript_expiry", func(tx *gorm.DB) {
		statements = append(statements, tx.Statement.SQL.String())
		tx.RowsAffected = 2
	}); err != nil {
		t.Fatal(err)
	}
	repo := NewAgentTranscriptRepositoryWithDB(database)
	deleted, err := repo.DeleteExpired(tenant.WithTenantID(context.Background(), 7), time.Now(), 50)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 {
		t.Fatalf("清理计数错误：%d", deleted)
	}
	joined := strings.Join(statements, "\n")
	for _, fragment := range []string{"scenario_transcripts", "tool_invocations", "tenant_id =", "expires_at <="} {
		if !strings.Contains(joined, fragment) {
			t.Fatalf("TTL 清理缺少 %q：%s", fragment, joined)
		}
	}
}
