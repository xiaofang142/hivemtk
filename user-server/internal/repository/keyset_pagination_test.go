package repository

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	_pagination "hivemtk-user/internal/pkg/pagination"
	"hivemtk-user/internal/pkg/testutil"
)

// keyset 翻页遍历：反复携带 next_cursor 取页，直到 next_cursor 为空。
// 返回按取回顺序拼接的 id 序列。pageSize 为每页大小。
func drainKeyset[T any](t *testing.T, first func(cursor string) ([]T, string, error), idOf func(T) uint64) []uint64 {
	t.Helper()
	var ids []uint64
	cursor := ""
	for pages := 0; ; pages++ {
		if pages > 50 {
			t.Fatalf("keyset drain exceeds 50 pages, possible infinite loop")
		}
		items, next, err := first(cursor)
		if err != nil {
			t.Fatalf("keyset page %d failed: %v", pages, err)
		}
		for _, it := range items {
			ids = append(ids, idOf(it))
		}
		if next == "" {
			return ids
		}
		cursor = next
	}
}

func TestCustomerRepository_ListKeyset(t *testing.T) {
	database := testutil.NewTestDB(t, &model.Customer{})
	db.SetTestDB(database)
	repo := NewCustomerRepository()
	ctx := context.Background()

	base := time.Now().Add(-time.Hour)
	for i := 0; i < 5; i++ {
		c := &model.Customer{
			Phone:     "138001380" + string(rune('0'+i)),
			CreatedAt: base.Add(time.Duration(i) * time.Minute),
		}
		if err := repo.Create(ctx, c); err != nil {
			t.Fatalf("seed customer %d: %v", i, err)
		}
	}

	seen := map[string]bool{}
	cursor := ""
	pages := 0
	for {
		list, total, next, err := repo.ListKeyset(ctx, cursor, 2, "")
		if err != nil {
			t.Fatalf("ListKeyset page %d: %v", pages, err)
		}
		if total != 5 {
			t.Fatalf("total = %d, want 5", total)
		}
		if len(list) > 2 {
			t.Fatalf("page size exceeded: got %d rows", len(list))
		}
		for _, c := range list {
			if seen[c.ID] {
				t.Fatalf("duplicate customer across pages: %s", c.ID)
			}
			seen[c.ID] = true
		}
		pages++
		if next == "" {
			break
		}
		cursor = next
		if pages > 10 {
			t.Fatalf("too many pages, possible cursor not advancing")
		}
	}
	if len(seen) != 5 {
		t.Fatalf("drained %d unique customers, want 5", len(seen))
	}

	if _, _, _, err := repo.ListKeyset(ctx, "not-a-valid-cursor", 2, ""); err == nil {
		t.Errorf("expected error for invalid cursor, got nil")
	}
}

func TestOperationLogRepository_ListKeyset(t *testing.T) {
	database := testutil.NewTestDB(t, &model.OperationLog{})
	repo := NewOperationLogRepositoryWithDB(database)
	ctx := context.Background()

	base := time.Now().Add(-time.Hour)
	for i := 0; i < 5; i++ {
		log := &model.OperationLog{
			UserID:    uint(100 + i),
			Action:    "test_action",
			Module:    "test_module",
			CreatedAt: base.Add(time.Duration(i) * time.Minute),
		}
		if err := repo.Create(ctx, log); err != nil {
			t.Fatalf("seed log %d: %v", i, err)
		}
	}

	ids := drainKeyset(t, func(cursor string) ([]*model.OperationLog, string, error) {
		list, _, next, err := repo.GetAllKeyset(ctx, cursor, 2, map[string]any{"action": "test_action"})
		return list, next, err
	}, func(l *model.OperationLog) uint64 { return uint64(l.ID) })

	if len(ids) != 5 {
		t.Fatalf("drained %d logs, want 5", len(ids))
	}
	for i := 1; i < len(ids); i++ {
		if ids[i-1] <= ids[i] {
			t.Fatalf("expected DESC id order across pages, got %v", ids)
		}
	}

	if _, _, _, err := repo.GetAllKeyset(ctx, "%%%bad%%%", 2, nil); err == nil {
		t.Errorf("expected error for invalid cursor, got nil")
	}
}

func TestSecurityAuditRepository_ListKeyset(t *testing.T) {
	database := testutil.NewTestDB(t, &model.SecurityAudit{})
	repo := NewSecurityAuditRepository()
	repo.SetDB(context.Background(), database)
	ctx := context.Background()

	base := time.Now().Add(-time.Hour)
	for i := 0; i < 5; i++ {
		a := &model.SecurityAudit{
			AuditName: "audit",
			Status:    "done",
			CreatedAt: base.Add(time.Duration(i) * time.Minute),
			StartedAt: base,
		}
		if err := repo.Create(ctx, a); err != nil {
			t.Fatalf("seed audit %d: %v", i, err)
		}
	}

	ids := drainKeyset(t, func(cursor string) ([]model.SecurityAudit, string, error) {
		list, _, next, err := repo.ListKeyset(ctx, cursor, 2)
		return list, next, err
	}, func(a model.SecurityAudit) uint64 { return uint64(a.ID) })

	if len(ids) != 5 {
		t.Fatalf("drained %d audits, want 5", len(ids))
	}
	for i := 1; i < len(ids); i++ {
		if ids[i-1] <= ids[i] {
			t.Fatalf("expected DESC id order across pages, got %v", ids)
		}
	}
}

func TestOperationLogCursorCompat_EncodeDecode(t *testing.T) {
	ts := time.Unix(1700000000, 500).UTC()
	cur := _pagination.EncodeCursor(ts, 42)
	gotTS, gotID, ok := _pagination.DecodeCursor(cur)
	if !ok {
		t.Fatalf("DecodeCursor failed for %s", cur)
	}
	if gotTS.UnixNano() != ts.UnixNano() || gotID != 42 {
		t.Fatalf("roundtrip mismatch: ts=%v id=%d", gotTS.UnixNano(), gotID)
	}
}
