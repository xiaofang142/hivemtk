package testutil

import (
	"testing"
	"time"
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

// 父子探针：验证 TRUNCATE ... CASCADE 会清空引用父表的子表。
type testdbProbeParent struct {
	ID   uint   `gorm:"primaryKey"`
	Name string `gorm:"size:64"`
}

func (testdbProbeParent) TableName() string { return "testdb_schema_probe_parent" }

type testdbProbeChild struct {
	ID       uint              `gorm:"primaryKey"`
	ParentID uint              `gorm:"index"`
	Parent   testdbProbeParent `gorm:"foreignKey:ParentID"`
}

func (testdbProbeChild) TableName() string { return "testdb_schema_probe_child" }

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

// ResetSchema 必须把测试自建的列/索引一并移除，恢复模型定义的形状。
func TestResetSchemaRemovesExtraColumn(t *testing.T) {
	db := NewTestDB(t, &testdbProbeA{})
	if err := db.Exec(`ALTER TABLE testdb_schema_probe_a ADD COLUMN extra text`).Error; err != nil {
		t.Fatalf("加列: %v", err)
	}
	if !db.Migrator().HasColumn(&testdbProbeA{}, "extra") {
		t.Fatalf("前置不成立：extra 列未建出")
	}

	ResetSchema(t, db, &testdbProbeA{})
	if db.Migrator().HasColumn(&testdbProbeA{}, "extra") {
		t.Fatalf("ResetSchema 后 extra 列仍存在")
	}
}

// 显式钉住 TRUNCATE ... CASCADE 的语义：只请求父表时，引用它的子表也会被清空。
// 这比旧实现 DropTable（遇外键常失败）隔离更强，是本轮有意的行为变化。
func TestTruncateCascadeClearsDependentTable(t *testing.T) {
	db := NewTestDB(t, &testdbProbeParent{}, &testdbProbeChild{})
	if err := db.Create(&testdbProbeParent{ID: 1, Name: "p"}).Error; err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	if err := db.Create(&testdbProbeChild{ID: 1, ParentID: 1}).Error; err != nil {
		t.Fatalf("seed child: %v", err)
	}

	// 第二次只请求父表；TRUNCATE 父表带 CASCADE，子表应同步清空。
	db = NewTestDB(t, &testdbProbeParent{})
	var parents, children int64
	if err := db.Model(&testdbProbeParent{}).Count(&parents).Error; err != nil {
		t.Fatalf("count parent: %v", err)
	}
	if err := db.Model(&testdbProbeChild{}).Count(&children).Error; err != nil {
		t.Fatalf("count child: %v", err)
	}
	if parents != 0 || children != 0 {
		t.Fatalf("TRUNCATE CASCADE 后应为空，got parent=%d child=%d", parents, children)
	}
}

func TestTimeoutSettingValidation(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "60s"},
		{"30s", "30s"},
		{"5min", "5min"},
		{"500ms", "500ms"},
		{"1h", "1h"},
		{"0", "0"},
		{"1.5s", "60s"},
		{"-1s", "60s"},
		{"x'; DROP TABLE users;--", "60s"},
	}
	for _, tc := range cases {
		t.Setenv("TESTDB_STATEMENT_TIMEOUT", tc.in)
		if got := timeoutSetting(t, "TESTDB_STATEMENT_TIMEOUT", "60s"); got != tc.want {
			t.Errorf("timeoutSetting(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCloseTimeoutParsing(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"", 30 * time.Second},
		{"5s", 5 * time.Second},
		{"1m", time.Minute},
		{"bad", 30 * time.Second},
		{"0s", 30 * time.Second},
	}
	for _, tc := range cases {
		t.Setenv("TESTDB_CLOSE_TIMEOUT", tc.in)
		if got := closeTimeout(); got != tc.want {
			t.Errorf("closeTimeout(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
