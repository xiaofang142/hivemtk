package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
)

func TestRecoveryQueueService_Enqueue_Default(t *testing.T) {
	svc := NewRecoveryQueueService()
	mock := newMockRecoveryRepo()
	svc.repo = mock

	item, err := svc.Enqueue(context.Background(), recoveryEnqueueArgs("c1", "u1", "a1", "", "", 0))
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}
	if item.Reason != "churn" {
		t.Errorf("expected default reason churn, got %s", item.Reason)
	}
	if item.Strategy != "sms_coupon" {
		t.Errorf("expected default strategy sms_coupon, got %s", item.Strategy)
	}
	if item.Priority != 5 {
		t.Errorf("expected default priority 5, got %d", item.Priority)
	}
}

func TestRecoveryQueueService_Enqueue_Custom(t *testing.T) {
	svc := NewRecoveryQueueService()
	svc.repo = newMockRecoveryRepo()

	item, err := svc.Enqueue(context.Background(), recoveryEnqueueArgs("c1", "u1", "a1", "complaint", "phone_call", 2))
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}
	if item.Reason != "complaint" {
		t.Errorf("expected complaint, got %s", item.Reason)
	}
	if item.Priority != 2 {
		t.Errorf("expected 2, got %d", item.Priority)
	}
}

func TestRecoveryQueueService_MarkAttempt(t *testing.T) {
	svc := NewRecoveryQueueService()
	mock := newMockRecoveryRepo()
	svc.repo = mock

	item, _ := svc.Enqueue(context.Background(), recoveryEnqueueArgs("c1", "u1", "a1", "churn", "sms", 5))
	if err := svc.MarkAttempt(context.Background(), item.ID, "sms", "delivered", "failed", 30*time.Second); err != nil {
		t.Fatalf("MarkAttempt failed: %v", err)
	}
	got, _ := mock.GetByID(context.Background(), item.ID)
	if got.Attempts != 1 {
		t.Errorf("expected attempts 1, got %d", got.Attempts)
	}
	if got.LastChannel != "sms" {
		t.Errorf("expected last_channel sms, got %s", got.LastChannel)
	}
	if got.Stage != "failed" {
		t.Errorf("expected stage failed, got %s", got.Stage)
	}
}

func TestRecoveryQueueService_MarkRecovered(t *testing.T) {
	svc := NewRecoveryQueueService()
	svc.repo = newMockRecoveryRepo()
	item, _ := svc.Enqueue(context.Background(), recoveryEnqueueArgs("c1", "u1", "a1", "churn", "sms", 5))
	if err := svc.MarkRecovered(context.Background(), item.ID, 99000); err != nil {
		t.Fatalf("MarkRecovered failed: %v", err)
	}
	got, _ := svc.repo.GetByID(context.Background(), item.ID)
	if got.Stage != model.RecoveryStageSucceed {
		t.Errorf("expected succeed, got %s", got.Stage)
	}
	if got.RecoveryValue != 99000 {
		t.Errorf("expected 99000, got %d", got.RecoveryValue)
	}
}

func TestRecoveryQueueService_Cancel(t *testing.T) {
	svc := NewRecoveryQueueService()
	svc.repo = newMockRecoveryRepo()
	item, _ := svc.Enqueue(context.Background(), recoveryEnqueueArgs("c1", "u1", "a1", "churn", "sms", 5))
	if err := svc.Cancel(context.Background(), item.ID); err != nil {
		t.Fatalf("Cancel failed: %v", err)
	}
	got, _ := svc.repo.GetByID(context.Background(), item.ID)
	if got.Stage != model.RecoveryStageCancelled {
		t.Errorf("expected cancelled, got %s", got.Stage)
	}
}

// recoveryEnqueueArgs 把旧的 6 位置参数写法转成结构体入参（既有断言只关心其中几个字段）
func recoveryEnqueueArgs(customerID, unifiedID, account, reason, strategy string, priority int) *RecoveryEnqueueInput {
	return &RecoveryEnqueueInput{
		CustomerID: customerID,
		UnifiedID:  unifiedID,
		Account:    account,
		Reason:     reason,
		Strategy:   strategy,
		Priority:   priority,
	}
}

type mockRecoveryRepo struct {
	items map[uint64]*model.RecoveryQueue
	next  uint64
	// failMarkAttemptOn 让指定 id 的台账写入失败，用来验证"记账失败不拖垮整轮"
	failMarkAttemptOn uint64
	lastLimit         int
}

func newMockRecoveryRepo() *mockRecoveryRepo {
	return &mockRecoveryRepo{
		items: make(map[uint64]*model.RecoveryQueue),
		next:  1,
	}
}

func (m *mockRecoveryRepo) Create(ctx context.Context, item *model.RecoveryQueue) error {
	if item.CustomerID == "" {
		return errEmpty{msg: "customer_id"}
	}
	for _, v := range m.items {
		if v.CustomerID == item.CustomerID && (v.Stage == model.RecoveryStageQueued || v.Stage == model.RecoveryStageRunning) {
			return errAlreadyQueued{msg: "客户已在挽回队列中"}
		}
	}
	item.ID = m.next
	m.next++
	m.items[item.ID] = item
	return nil
}

func (m *mockRecoveryRepo) Update(ctx context.Context, item *model.RecoveryQueue) error {
	m.items[item.ID] = item
	return nil
}

func (m *mockRecoveryRepo) GetByID(ctx context.Context, id uint64) (*model.RecoveryQueue, error) {
	if v, ok := m.items[id]; ok {
		return v, nil
	}
	return nil, errNotFound{msg: "队列项不存在"}
}

func (m *mockRecoveryRepo) GetActiveByCustomerID(ctx context.Context, customerID string) (*model.RecoveryQueue, error) {
	for _, v := range m.items {
		if v.CustomerID == customerID && (v.Stage == model.RecoveryStageQueued || v.Stage == model.RecoveryStageRunning) {
			return v, nil
		}
	}
	return nil, errNotFound{msg: "无活跃队列"}
}

func (m *mockRecoveryRepo) ListByStage(ctx context.Context, stage string, page, pageSize int) ([]*model.RecoveryQueue, int64, error) {
	var out []*model.RecoveryQueue
	for _, v := range m.items {
		if stage == "" || v.Stage == stage {
			out = append(out, v)
		}
	}
	return out, int64(len(out)), nil
}

func (m *mockRecoveryRepo) countReady(ctx context.Context, t testing.TB, now time.Time, limit int) int {
	t.Helper()
	out, err := m.ListReadyForAttempt(ctx, now, limit)
	if err != nil {
		t.Fatalf("ListReadyForAttempt: %v", err)
	}
	return len(out)
}

func (m *mockRecoveryRepo) ListReadyForAttempt(ctx context.Context, now time.Time, limit int) ([]*model.RecoveryQueue, error) {
	m.lastLimit = limit
	var out []*model.RecoveryQueue
	for _, v := range m.items {
		if v.Stage == model.RecoveryStageQueued && v.Attempts < v.MaxAttempts {
			if v.NextAttemptAt == nil || !v.NextAttemptAt.After(now) {
				out = append(out, v)
			}
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *mockRecoveryRepo) MarkAttempt(ctx context.Context, id uint64, channel, result string, nextAt *time.Time) error {
	if id == m.failMarkAttemptOn {
		return errNotFound{msg: "台账写入失败（测试注入）"}
	}
	if v, ok := m.items[id]; ok {
		v.Attempts++
		v.LastChannel = channel
		v.LastResult = result
		v.LastAttemptAt = &time.Time{}
		*v.LastAttemptAt = time.Now()
		v.NextAttemptAt = nextAt
		return nil
	}
	return errNotFound{msg: "队列项不存在"}
}

func (m *mockRecoveryRepo) MarkStage(ctx context.Context, id uint64, stage string) error {
	if v, ok := m.items[id]; ok {
		v.Stage = stage
		return nil
	}
	return errNotFound{msg: "队列项不存在"}
}

func (m *mockRecoveryRepo) CountByStage(ctx context.Context) (map[string]int64, error) {
	out := make(map[string]int64)
	for _, v := range m.items {
		out[v.Stage]++
	}
	return out, nil
}

func (m *mockRecoveryRepo) Delete(ctx context.Context, id uint64) error {
	delete(m.items, id)
	return nil
}

type errEmpty struct{ msg string }

func (e errEmpty) Error() string { return e.msg }

type errAlreadyQueued struct{ msg string }

func (e errAlreadyQueued) Error() string { return e.msg }

type errNotFound struct{ msg string }

func (e errNotFound) Error() string { return e.msg }
