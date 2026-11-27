package repository

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestSessionMessageRepositoryListByCorrelationIDsContract(t *testing.T) {
	database, err := gorm.Open(
		postgres.New(postgres.Config{DSN: "host=127.0.0.1 port=8217 user=contract dbname=contract sslmode=disable"}),
		&gorm.Config{DryRun: true, DisableAutomaticPing: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	var capturedSQL string
	var capturedVars []any
	if err := database.Callback().Query().After("gorm:query").Register("test:capture_session_correlation", func(tx *gorm.DB) {
		capturedSQL = tx.Statement.SQL.String()
		capturedVars = append([]any{}, tx.Statement.Vars...)
	}); err != nil {
		t.Fatal(err)
	}
	repo := NewSessionMessageRepositoryWithDB(database)

	rows, err := repo.ListByCorrelationIDs(context.Background(), 71, []string{"corr-1", "", "corr-2"}, 25)
	if err != nil {
		t.Fatal(err)
	}
	if rows == nil {
		t.Fatal("返回切片必须初始化，不能为 nil")
	}

	sql := capturedSQL
	for _, fragment := range []string{"tenant_id =", "correlation_id IN", "ORDER BY id ASC LIMIT"} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("SQL 缺少 %q：%s", fragment, sql)
		}
	}
	wantVars := []any{uint(71), "corr-1", "corr-2", 25}
	if !reflect.DeepEqual(capturedVars, wantVars) {
		t.Fatalf("租户、去重 correlation 或 limit 参数错误：实际 %#v，期望 %#v", capturedVars, wantVars)
	}
}

func TestSessionMessageRepositoryListByCorrelationIDsRejectsMissingScope(t *testing.T) {
	database, err := gorm.Open(
		postgres.New(postgres.Config{DSN: "host=127.0.0.1 port=8217 user=contract dbname=contract sslmode=disable"}),
		&gorm.Config{DryRun: true, DisableAutomaticPing: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewSessionMessageRepositoryWithDB(database)

	if _, err := repo.ListByCorrelationIDs(context.Background(), 0, []string{"corr-1"}, 10); err == nil {
		t.Fatal("缺租户必须 fail-closed，不能退化为跨租户查询")
	}
	rows, err := repo.ListByCorrelationIDs(context.Background(), 71, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if rows == nil || len(rows) != 0 {
		t.Fatalf("空 correlation 集合必须返回初始化空切片，实际 %#v", rows)
	}
}
