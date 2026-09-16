package db

import (
	"fmt"
	"testing"

	geomodel "hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// TestAllModels_CoversModelsWithWritePaths 防止「模型有生产写入路径、却没登记建表」复发。
//
// 背景（2026-09-16 审计 DB-07）：GeoCrawlerVisit 是 29 个 geo 模型里**唯一没登记**
// 进 allModels() 的，而 internal/router/router.go 的 AICrawlerMonitor 回调会
// fire-and-forget 地写入它、且 error 被 `_ =` 丢弃 —— 全新部署不建
// geo_crawler_visits 表 ⇒ 统计永久为 0，且**日志里一行都没有**。
//
// 该缺陷不会在本地暴露：开发库里的表是历史遗留/测试误写留下的（见 TEST-06 同源问题），
// 只有全新部署才会现形。因此必须有这条用例替它守着。
//
// 新增模型时：只要它有**生产写入路径**，就把类型加进 mustCover。
func TestAllModels_CoversModelsWithWritePaths(t *testing.T) {
	mustCover := []any{
		&geomodel.GeoCrawlerVisit{},
	}

	registered := make(map[string]bool, 512)
	for _, m := range append(allModels(), ExtraModels()...) {
		registered[fmt.Sprintf("%T", m)] = true
	}

	for _, m := range mustCover {
		if !registered[fmt.Sprintf("%T", m)] {
			t.Errorf("模型 %T 有生产写入路径但未登记进 allModels()：全新部署不会建表，写入将静默失败", m)
		}
	}
}

// TestAutoMigrate_Complete 测试完整 AutoMigrate 不应 panic
func TestAutoMigrate_Complete(t *testing.T) {
	testDB := testutil.NewTestDB(t)
	if testDB == nil {
		t.Fatal("NewTestDB returned nil")
	}

	originalDB := DB
	DB = testDB
	defer func() {
		DB = originalDB
	}()

	db := AutoMigrate()
	if db == nil {
		t.Error("AutoMigrate returned nil")
	}
}

// TestAutoMigrate_Partial 测试部分模型迁移（直接调用 testDB.AutoMigrate）
func TestAutoMigrate_Partial(t *testing.T) {
	testDB := testutil.NewTestDB(t, &model.User{})
	if testDB == nil {
		t.Fatal("NewTestDB returned nil")
	}
	if err := testDB.AutoMigrate(&model.User{}); err != nil {
		t.Errorf("migrate User: %v", err)
	}
}

// TestAutoMigrate_MultipleTables 测试多表迁移
func TestAutoMigrate_MultipleTables(t *testing.T) {
	testDB := testutil.NewTestDB(t,
		&model.User{},
		&model.Account{},
		&model.Order{},
	)
	if testDB == nil {
		t.Fatal("NewTestDB returned nil")
	}
	if err := testDB.AutoMigrate(&model.User{}, &model.Account{}, &model.Order{}); err != nil {
		t.Errorf("migrate: %v", err)
	}
}

// TestAutoMigrate_Idempotent 测试迁移的幂等性
func TestAutoMigrate_Idempotent(t *testing.T) {
	testDB := testutil.NewTestDB(t, &model.User{})
	if testDB == nil {
		t.Fatal("NewTestDB returned nil")
	}
	if err := testDB.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := testDB.AutoMigrate(&model.User{}); err != nil {
		t.Errorf("second migrate failed: %v", err)
	}
}

// TestAutoMigrate_WithIndexes 测试带索引的迁移
func TestAutoMigrate_WithIndexes(t *testing.T) {
	testDB := testutil.NewTestDB(t, &model.User{}, &model.Account{})
	if testDB == nil {
		t.Fatal("NewTestDB returned nil")
	}
	if err := testDB.AutoMigrate(&model.User{}, &model.Account{}); err != nil {
		t.Errorf("migrate: %v", err)
	}
}

// TestAutoMigrate_NilDB 测试 nil DB 情况
func TestAutoMigrate_NilDB(t *testing.T) {
	originalDB := DB
	DB = nil
	defer func() {
		DB = originalDB
	}()

	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic for nil DB")
		}
	}()

	AutoMigrate()
}

// TestAutoMigrate_EmptyModel 测试空模型列表
func TestAutoMigrate_EmptyModel(t *testing.T) {
	testDB := testutil.NewTestDB(t)
	if testDB == nil {
		t.Fatal("NewTestDB returned nil")
	}
	if err := testDB.AutoMigrate(); err != nil {
		t.Errorf("migrate empty: %v", err)
	}
}

// TestAutoMigrate_LargeModel 测试多字段模型迁移
func TestAutoMigrate_LargeModel(t *testing.T) {
	testDB := testutil.NewTestDB(t, &model.User{}, &model.Account{}, &model.Order{}, &model.Message{})
	if testDB == nil {
		t.Fatal("NewTestDB returned nil")
	}
	if err := testDB.AutoMigrate(&model.User{}, &model.Account{}, &model.Order{}, &model.Message{}); err != nil {
		t.Errorf("migrate: %v", err)
	}
}

// TestAutoMigrate_ConcurrentAccess 测试并发迁移调用
func TestAutoMigrate_ConcurrentAccess(t *testing.T) {
	testDB := testutil.NewTestDB(t, &model.User{})
	if testDB == nil {
		t.Fatal("NewTestDB returned nil")
	}
	done := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func() {
			err := testDB.AutoMigrate(&model.User{})
			if err != nil {
				done <- err
				return
			}
			done <- nil
		}()
	}
	for i := 0; i < 3; i++ {
		if err := <-done; err != nil {
			t.Errorf("concurrent migrate: %v", err)
		}
	}
}

// TestAutoMigrate_VeryLargeNumberOfModels 测试较多模型同时迁移
func TestAutoMigrate_VeryLargeNumberOfModels(t *testing.T) {
	testDB := testutil.NewTestDB(t,
		&model.User{},
		&model.Account{},
		&model.Order{},
		&model.Message{},
		&model.Clue{},
		&model.DouyinCard{},
	)
	if testDB == nil {
		t.Fatal("NewTestDB returned nil")
	}
	if err := testDB.AutoMigrate(
		&model.User{},
		&model.Account{},
		&model.Order{},
		&model.Message{},
		&model.Clue{},
		&model.DouyinCard{},
	); err != nil {
		t.Errorf("migrate: %v", err)
	}
}
