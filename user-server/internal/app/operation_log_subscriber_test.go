package app

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/event"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupSubscriberTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.OperationLog{},
	)
	db.SetTestDB(database)
	return database
}

// TestOperationLogSubscriber_Handle 正常处理事件
func TestOperationLogSubscriber_Handle(t *testing.T) {
	database := setupSubscriberTestDB(t)
	subscriber := NewOperationLogSubscriber(newTestLogRepo(database))

	evt := event.Event{
		Topic: event.TopicOperationLog,
		Payload: event.OperationLogPayload{
			UserID:     1,
			Username:   "admin",
			Action:     "create",
			Module:     "user",
			Resource:   "user",
			ResourceID: "100",
			OldValue:   nil,
			NewValue:   map[string]any{"name": "test"},
			IP:         "127.0.0.1",
		},
	}

	err := subscriber.Handle(evt)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	var log model.OperationLog
	if err := database.First(&log, 1).Error; err != nil {
		t.Fatalf("expected log entry in DB, got error: %v", err)
	}

	if log.UserID != 1 {
		t.Errorf("expected UserID 1, got %d", log.UserID)
	}
	if log.Username != "admin" {
		t.Errorf("expected Username 'admin', got '%s'", log.Username)
	}
	if log.Action != "create" {
		t.Errorf("expected Action 'create', got '%s'", log.Action)
	}
	if log.Module != "user" {
		t.Errorf("expected Module 'user', got '%s'", log.Module)
	}
	if log.Resource != "user" {
		t.Errorf("expected Resource 'user', got '%s'", log.Resource)
	}
	if log.ResourceID != "100" {
		t.Errorf("expected ResourceID '100', got '%s'", log.ResourceID)
	}
	if log.IP != "127.0.0.1" {
		t.Errorf("expected IP '127.0.0.1', got '%s'", log.IP)
	}
	if log.NewValue == "" {
		t.Error("expected non-empty NewValue JSON")
	}
}

// TestOperationLogSubscriber_Handle_WrongPayloadType 载荷类型不匹配：no-op，不报错
func TestOperationLogSubscriber_Handle_WrongPayloadType(t *testing.T) {
	database := setupSubscriberTestDB(t)
	subscriber := NewOperationLogSubscriber(newTestLogRepo(database))

	evt := event.Event{
		Topic:   event.TopicOperationLog,
		Payload: "wrong-type-payload",
	}

	err := subscriber.Handle(evt)
	if err != nil {
		t.Errorf("expected nil error for wrong payload type, got %v", err)
	}

	var count int64
	database.Model(&model.OperationLog{}).Count(&count)
	if count != 0 {
		t.Errorf("expected 0 log entries, got %d", count)
	}
}

// TestOperationLogSubscriber_Handle_NilPayload nil 载荷
func TestOperationLogSubscriber_Handle_NilPayload(t *testing.T) {
	database := setupSubscriberTestDB(t)
	subscriber := NewOperationLogSubscriber(newTestLogRepo(database))

	evt := event.Event{
		Topic:   event.TopicOperationLog,
		Payload: nil,
	}

	err := subscriber.Handle(evt)
	if err != nil {
		t.Errorf("expected nil error for nil payload, got %v", err)
	}

	var count int64
	database.Model(&model.OperationLog{}).Count(&count)
	if count != 0 {
		t.Errorf("expected 0 log entries, got %d", count)
	}
}

// TestOperationLogSubscriber_EndToEnd 端到端：通过 EventBus 发布，订阅者写入 DB
func TestOperationLogSubscriber_EndToEnd(t *testing.T) {
	database := setupSubscriberTestDB(t)

	bus := event.New(1, 16)
	defer bus.Stop()

	subscriber := NewOperationLogSubscriber(newTestLogRepo(database))
	bus.Subscribe(event.TopicOperationLog, subscriber.Handle)

	bus.Publish(event.Event{
		Topic: event.TopicOperationLog,
		Payload: event.OperationLogPayload{
			UserID:     42,
			Username:   "operator",
			Action:     "delete",
			Module:     "user",
			Resource:   "user",
			ResourceID: "42",
			OldValue:   map[string]any{"id": 42, "name": "deleted-user"},
			NewValue:   nil,
			IP:         "10.0.0.1",
		},
	})

	waitForLogCondition(t, func() bool {
		var count int64
		database.Model(&model.OperationLog{}).Count(&count)
		return count == 1
	}, 500*time.Millisecond)

	var log model.OperationLog
	if err := database.First(&log).Error; err != nil {
		t.Fatalf("expected log entry: %v", err)
	}

	if log.UserID != 42 {
		t.Errorf("expected UserID 42, got %d", log.UserID)
	}
	if log.Action != "delete" {
		t.Errorf("expected Action 'delete', got '%s'", log.Action)
	}
	if log.OldValue == "" {
		t.Error("expected non-empty OldValue JSON")
	}
}

func newTestLogRepo(database *gorm.DB) *testLogRepo {
	return &testLogRepo{db: database}
}

type testLogRepo struct {
	db *gorm.DB
}

func (r *testLogRepo) Create(ctx context.Context, log *model.OperationLog) error {
	return r.db.WithContext(ctx).Create(log).Error
}

func (r *testLogRepo) GetByID(ctx context.Context, id uint) (*model.OperationLog, error) {
	var log model.OperationLog
	err := r.db.First(&log, id).Error
	return &log, err
}

func (r *testLogRepo) GetAll(ctx context.Context, page, pageSize int, filters map[string]any) ([]*model.OperationLog, int64, error) {
	return nil, 0, nil
}

func (r *testLogRepo) GetByUserID(ctx context.Context, userID uint, page, pageSize int) ([]*model.OperationLog, int64, error) {
	return nil, 0, nil
}

func (r *testLogRepo) DeleteOldLogs(ctx context.Context, beforeDate time.Time) error {
	return nil
}

func (r *testLogRepo) DeleteByIDs(ctx context.Context, ids []uint) (int64, error) {
	return 0, nil
}

func (r *testLogRepo) UpdateNewValue(ctx context.Context, id uint, newValue string) error {
	return r.db.WithContext(ctx).Model(&model.OperationLog{}).
		Where("id = ?", id).
		Update("new_value", newValue).Error
}

func waitForLogCondition(t *testing.T, cond func() bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
}
