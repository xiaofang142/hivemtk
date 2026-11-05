package testutil

import (
	"testing"
)

// 这两个模型只在本文件使用：验证进程级 schema 复用/按需迁移/掉表回退。
type testdbProbeA struct {
	ID   uint   `gorm:"primaryKey"`
	Name string `gorm:"size:64"`
}

func (testdbProbeA) TableName() string { return "testdb_schema_probe_a" }

type testdbProbeB struct {
	ID   uint   `gorm:"primaryKey"`
	Name string `gorm:"size:64"`
}

func (testdbProbeB) TableName() string { return "testdb_schema_probe_b" }

// 槽位咨询锁必须由包级连接持有：局部变量被 GC 后连接关闭、锁提前释放，
// 另一个测试进程会抢到同一槽位并 DROP 正在使用的库。
func TestMaintenanceConnRetained(t *testing.T) {
	NewTestDB(t)
	if !SlotLockHeld() {
		t.Fatal("槽位维护连接未保留（GC 后咨询锁会提前释放）")
	}
}

// 同一模型二次调用：schema 复用，上一次写入的行被清空；连接池按用例隔离。
func TestNewTestDBReusesSchemaAndClearsRows(t *testing.T) {
	db1 := NewTestDB(t, &testdbProbeA{})
	if err := db1.Create(&testdbProbeA{Name: "leak"}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	db2 := NewTestDB(t, &testdbProbeA{})
	if db1 == db2 {
		t.Fatalf("每个用例应使用独立连接池（隔离跨用例异步写），got 同一指针")
	}
	var n int64
	if err := db2.Model(&testdbProbeA{}).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("上一次用例写入的行没有被清空：count=%d", n)
	}
}

// 第二次调用携带新模型：新表按需建出来，旧模型的行照旧清空。
func TestNewTestDBMigratesNewModelOnDemand(t *testing.T) {
	db := NewTestDB(t, &testdbProbeA{})
	if err := db.Create(&testdbProbeA{Name: "stale"}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	db = NewTestDB(t, &testdbProbeA{}, &testdbProbeB{})
	if !db.Migrator().HasTable(&testdbProbeB{}) {
		t.Fatalf("新增模型 %T 的表不存在", &testdbProbeB{})
	}
	var n int64
	if err := db.Model(&testdbProbeA{}).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("旧模型的行没有被清空：count=%d", n)
	}
}

// 用例自己 DROP 表后再调用：TRUNCATE 失败应回退到 DROP+AutoMigrate 并把表重建出来。
func TestNewTestDBRecoversDroppedTable(t *testing.T) {
	db := NewTestDB(t, &testdbProbeA{})
	if err := db.Migrator().DropTable(&testdbProbeA{}); err != nil {
		t.Fatalf("drop: %v", err)
	}

	db = NewTestDB(t, &testdbProbeA{})
	if !db.Migrator().HasTable(&testdbProbeA{}) {
		t.Fatalf("回退后表 %T 仍未重建", &testdbProbeA{})
	}
	var n int64
	if err := db.Model(&testdbProbeA{}).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("重建后的表不是空表：count=%d", n)
	}
}
