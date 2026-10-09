package app

import (
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"
)

// TestInitBusinessNotifier 装—撤—再装：db 为 nil 时必须**清空**全局
// （上一份实例留在槽里会被生产者继续拿去写），拿到 DB 即装配。
// 带库那一半走 testutil 的 PostgreSQL 测试库，本机不可达时 Skip、CI 连不上 Fatal。
func TestInitBusinessNotifier(t *testing.T) {
	before := service.GlobalNotifier()
	t.Cleanup(func() { service.SetGlobalNotifier(before) })

	InitBusinessNotifier(nil)
	if service.GlobalNotifier() != nil {
		t.Fatal("无 DB 句柄应把全局清空（生产者全部 no-op）")
	}

	db := testutil.NewTestDB(t, &model.Notification{})
	InitBusinessNotifier(db)
	if service.GlobalNotifier() == nil {
		t.Fatal("拿到 DB 应装配通知口")
	}

	InitBusinessNotifier(nil)
	if service.GlobalNotifier() != nil {
		t.Fatal("再次传 nil 应撤掉（后写覆盖语义）")
	}
}
