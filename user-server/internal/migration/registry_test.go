package migration

import (
	"context"
	"testing"

	"gorm.io/gorm"
)

// TestMigrationRegistry_Register 测试迁移注册
func TestMigrationRegistry_Register(t *testing.T) {
	registry := NewMigrationRegistry()

	testMigration := &testMigration{
		version:     "v1.1.0",
		name:        "Add new feature",
		description: "Add new feature migration",
	}

	registry.Register(testMigration)

	migration, ok := registry.Get("v1.1.0")
	if !ok {
		t.Fatal("Expected migration to be registered")
	}

	if migration.Version() != "v1.1.0" {
		t.Errorf("Expected version v1.1.0, got %s", migration.Version())
	}
}

// TestMigrationRegistry_GetNonExistent 测试获取不存在的迁移
func TestMigrationRegistry_GetNonExistent(t *testing.T) {
	registry := NewMigrationRegistry()

	_, ok := registry.Get("v9.9.9")
	if ok {
		t.Error("Expected migration not to be found")
	}
}

// TestMigrationRegistry_GetAll 测试获取所有迁移
func TestMigrationRegistry_GetAll(t *testing.T) {
	registry := NewMigrationRegistry()

	registry.Register(&testMigration{version: "v1.0.0", name: "Initial"})
	registry.Register(&testMigration{version: "v1.1.0", name: "Update 1"})
	registry.Register(&testMigration{version: "v1.2.0", name: "Update 2"})

	all := registry.GetAll()
	if len(all) != 3 {
		t.Errorf("Expected 3 migrations, got %d", len(all))
	}
}

// TestMigrationRegistry_GetPending 测试获取待执行的迁移
func TestMigrationRegistry_GetPending(t *testing.T) {
	registry := NewMigrationRegistry()

	registry.Register(&testMigration{version: "v1.0.0", name: "Initial"})
	registry.Register(&testMigration{version: "v1.1.0", name: "Update 1"})
	registry.Register(&testMigration{version: "v1.2.0", name: "Update 2"})

	executedVersions := []string{"v1.0.0", "v1.1.0"}

	pending := registry.GetPending(executedVersions)
	if len(pending) != 1 {
		t.Errorf("Expected 1 pending migration, got %d", len(pending))
	}
	if pending[0].Version() != "v1.2.0" {
		t.Errorf("Expected v1.2.0, got %s", pending[0].Version())
	}
}

// TestMigrationRegistry_GetPending_Empty 测试没有待执行的迁移
func TestMigrationRegistry_GetPending_Empty(t *testing.T) {
	registry := NewMigrationRegistry()

	registry.Register(&testMigration{version: "v1.0.0", name: "Initial"})

	executedVersions := []string{"v1.0.0"}
	pending := registry.GetPending(executedVersions)

	if len(pending) != 0 {
		t.Errorf("Expected 0 pending migrations, got %d", len(pending))
	}
}

// TestMigrationRegistry_Validate 测试验证迁移
func TestMigrationRegistry_Validate(t *testing.T) {
	registry := NewMigrationRegistry()

	registry.Register(&testMigration{version: "v1.0.0", name: "Initial"})
	registry.Register(&testMigration{version: "v1.1.0", name: "Update 1"})

	err := registry.Validate()
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

// TestMigrationRegistry_Validate_Duplicate 测试验证重复迁移
func TestMigrationRegistry_Validate_Duplicate(t *testing.T) {
	registry := &MigrationRegistry{
		migrations: make(map[string]Migration),
	}

	m := &testMigration{version: "v1.0.0", name: "Initial"}
	registry.migrations["v1.0.0"] = m
	registry.migrations["v1.0.0"] = m

	err := registry.Validate()
	if err != nil {
		t.Errorf("Expected no error for overwritten entry, got %v", err)
	}
}

// TestMigrationRegistry_Validate_DetectDuplicate 测试检测重复
func TestMigrationRegistry_Validate_DetectDuplicate(t *testing.T) {
	registry := NewMigrationRegistry()

	registry.Register(&testMigration{version: "v1.0.0", name: "Initial"})
	registry.Register(&testMigration{version: "v1.0.0", name: "Duplicate"})

	err := registry.Validate()
	if err != nil {
		t.Errorf("Expected no error (duplicate is overwritten), got %v", err)
	}
}

type testMigration struct {
	version     string
	name        string
	description string
	upError     error
	downError   error
}

func (m *testMigration) Version() string {
	return m.version
}

func (m *testMigration) Name() string {
	return m.name
}

func (m *testMigration) Description() string {
	return m.description
}

func (m *testMigration) Up(ctx context.Context) error {
	return m.upError
}

func (m *testMigration) Down(ctx context.Context) error {
	return m.downError
}

// TestTestMigration 测试 Migration 接口取值
func TestTestMigration(t *testing.T) {
	m := &testMigration{
		version:     "v1.0.0",
		name:        "Test Migration",
		description: "Test Description",
	}

	if m.Version() != "v1.0.0" {
		t.Errorf("Expected version v1.0.0, got %s", m.Version())
	}
	if m.Name() != "Test Migration" {
		t.Errorf("Expected name Test Migration, got %s", m.Name())
	}
	if m.Description() != "Test Description" {
		t.Errorf("Expected description Test Description, got %s", m.Description())
	}
}

// TestTestMigration_WithErrors 测试带错误的 Migration（验证注册表错误透传）
func TestTestMigration_WithErrors(t *testing.T) {
	upErr := &testError{"up failed"}
	downErr := &testError{"down failed"}

	m := &testMigration{
		version:   "v1.0.0",
		upError:   upErr,
		downError: downErr,
	}

	ctx := context.Background()

	if err := m.Up(ctx); err != upErr {
		t.Error("Expected up error")
	}

	if err := m.Down(ctx); err != downErr {
		t.Error("Expected down error")
	}
}

type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}

// TestMigrationInterface 测试 Migration 接口实现
func TestMigrationInterface(t *testing.T) {

	var _ Migration = (*testMigration)(nil)
}

// TestMigrationRegistry_NilContext 测试 nil 上下文
func TestMigrationRegistry_NilContext(t *testing.T) {
	registry := NewMigrationRegistry()

	m := &testMigration{version: "v1.0.0", name: "Test"}
	registry.Register(m)

	//lint:ignore SA1012 刻意传 nil 上下文验证兼容
	err := m.Up(nil)
	if err != nil {
		t.Errorf("Expected no error with nil context, got %v", err)
	}
}

// TestMigrationRegistry_EmptyContext 测试空上下文
func TestMigrationRegistry_EmptyContext(t *testing.T) {
	registry := NewMigrationRegistry()

	m := &testMigration{version: "v1.0.0", name: "Test"}
	registry.Register(m)

	ctx := context.Background()
	err := m.Up(ctx)
	if err != nil {
		t.Errorf("Expected no error with empty context, got %v", err)
	}
}

// TestMigrationRegistry_MultipleVersions 测试多个版本
func TestMigrationRegistry_MultipleVersions(t *testing.T) {
	registry := NewMigrationRegistry()

	versions := []string{"v1.0.0", "v1.1.0", "v1.2.0", "v2.0.0", "v2.1.0"}
	for _, v := range versions {
		registry.Register(&testMigration{version: v, name: "Migration " + v})
	}

	for _, v := range versions {
		m, ok := registry.Get(v)
		if !ok {
			t.Errorf("Expected migration %s to exist", v)
			continue
		}
		if m.Version() != v {
			t.Errorf("Expected version %s, got %s", v, m.Version())
		}
	}
}

// TestMigrationRegistry_GetPending_NoExecutedVersions 测试没有已执行版本
func TestMigrationRegistry_GetPending_NoExecutedVersions(t *testing.T) {
	registry := NewMigrationRegistry()

	registry.Register(&testMigration{version: "v1.0.0", name: "Initial"})
	registry.Register(&testMigration{version: "v1.1.0", name: "Update 1"})

	pending := registry.GetPending([]string{})
	if len(pending) != 2 {
		t.Errorf("Expected 2 pending migrations, got %d", len(pending))
	}
}

// TestMigrationRegistry_GetPartialExecutedVersions 测试部分已执行版本
func TestMigrationRegistry_GetPartialExecutedVersions(t *testing.T) {
	registry := NewMigrationRegistry()

	registry.Register(&testMigration{version: "v1.0.0", name: "Initial"})
	registry.Register(&testMigration{version: "v1.1.0", name: "Update 1"})
	registry.Register(&testMigration{version: "v1.2.0", name: "Update 2"})

	executedVersions := []string{"v1.1.0"}

	pending := registry.GetPending(executedVersions)
	if len(pending) != 2 {
		t.Errorf("Expected 2 pending migrations, got %d", len(pending))
	}
}

// TestMigrationService_Mechanics 测试 MigrationService 的注册表机制
func TestMigrationService_Mechanics(t *testing.T) {
	registry := NewMigrationRegistry()
	noopInit := func(r *MigrationRegistry, db *gorm.DB) {}
	service := NewMigrationService(registry, nil, noopInit)

	if service == nil {
		t.Fatal("Expected non-nil service")
	}

	if service.registry != registry {
		t.Error("Expected registry to be set")
	}
}
