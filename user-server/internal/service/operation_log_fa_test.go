package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
)

// faOperationLogRepo 内存 fake，实现 OperationLogRepository 全部方法。
type faOperationLogRepo struct {
	logs            map[uint]*model.OperationLog
	nextID          uint
	updateNewCalled int
}

func newFAOperationLogRepo() *faOperationLogRepo {
	return &faOperationLogRepo{logs: map[uint]*model.OperationLog{}, nextID: 1}
}

func (r *faOperationLogRepo) Create(ctx context.Context, l *model.OperationLog) error {
	if l.ID == 0 {
		l.ID = r.nextID
		r.nextID++
	} else if l.ID >= r.nextID {
		// FA-AUDITLOG-01：显式 ID 也要推进 nextID，否则后续 rollback 日志（ID=0）
		// 会被分配同一个 ID 覆盖原始日志，制造 new_value 被篡改的假象。
		r.nextID = l.ID + 1
	}
	cp := *l
	r.logs[l.ID] = &cp
	return nil
}

func (r *faOperationLogRepo) GetByID(ctx context.Context, id uint) (*model.OperationLog, error) {
	l, ok := r.logs[id]
	if !ok {
		return nil, nil
	}
	cp := *l
	return &cp, nil
}

func (r *faOperationLogRepo) GetAll(ctx context.Context, page, pageSize int, filters map[string]any) ([]*model.OperationLog, int64, error) {
	return nil, 0, nil
}
func (r *faOperationLogRepo) GetAllKeyset(ctx context.Context, cursor string, pageSize int, filters map[string]any) ([]*model.OperationLog, int64, string, error) {
	return nil, 0, "", nil
}
func (r *faOperationLogRepo) GetByUserID(ctx context.Context, userID uint, page, pageSize int) ([]*model.OperationLog, int64, error) {
	return nil, 0, nil
}
func (r *faOperationLogRepo) DeleteOldLogs(ctx context.Context, beforeDate time.Time) error {
	return nil
}
func (r *faOperationLogRepo) DeleteByIDs(ctx context.Context, ids []uint) (int64, error) {
	return 0, nil
}
func (r *faOperationLogRepo) UpdateNewValue(ctx context.Context, id uint, newValue string) error {
	r.updateNewCalled++
	return nil
}

func (r *faOperationLogRepo) count() int { return len(r.logs) }

// FA-AUDITLOG-01：回滚不得覆盖原 new_value 证据，须追加诚实审计快照记录。
func TestFAAuditlog_RollbackPreservesEvidence(t *testing.T) {
	ctx := context.Background()
	repo := newFAOperationLogRepo()
	orig := &model.OperationLog{
		ID: 1, UserID: 7, Username: "alice", Action: "update", Module: "crm",
		Resource: "customer", ResourceID: "c-1", OldValue: `{"status":"new"}`,
		NewValue: `{"status":"paid"}`,
	}
	if err := repo.Create(ctx, orig); err != nil {
		t.Fatal(err)
	}
	svc := &OperationLogService{logRepo: repo}

	rb, err := svc.Rollback(ctx, 1, "bob")
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	// 原 new_value 证据未被覆盖（UpdateNewValue 不应被调用）。
	if repo.updateNewCalled != 0 {
		t.Fatalf("UpdateNewValue 不应被调用，实际 %d 次（原证据被覆盖）", repo.updateNewCalled)
	}
	kept, _ := repo.GetByID(ctx, 1)
	if kept.NewValue != orig.NewValue {
		t.Fatalf("原日志 new_value 被篡改: got %q want %q", kept.NewValue, orig.NewValue)
	}
	// 追加了一条 rollback 审计记录，值语义正确。
	if repo.count() != 2 {
		t.Fatalf("应有原日志+rollback 两条，实际 %d", repo.count())
	}
	if rb.Action != "rollback" || rb.OldValue != orig.NewValue || rb.NewValue != orig.OldValue {
		t.Fatalf("rollback 记录值错误: %+v", rb)
	}
	if !containsZh(rb.Detail, "不含业务数据恢复") {
		t.Fatalf("rollback 文案未声明不含业务数据恢复: %q", rb.Detail)
	}
}

func TestFAAuditlog_RollbackNoOldValueRejected(t *testing.T) {
	ctx := context.Background()
	repo := newFAOperationLogRepo()
	if err := repo.Create(ctx, &model.OperationLog{ID: 2, Action: "create", NewValue: "x"}); err != nil {
		t.Fatal(err)
	}
	svc := &OperationLogService{logRepo: repo}
	if _, err := svc.Rollback(ctx, 2, "bob"); err == nil {
		t.Fatal("无可回滚旧值应报错")
	}
	if repo.count() != 1 {
		t.Fatalf("失败不应新增审计行，实际 %d", repo.count())
	}
}

func containsZh(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
