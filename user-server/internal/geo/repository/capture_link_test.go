// capture_link_test.go 承接链接仓储的租户、配额与首响契约。
//
// code 是公开入口自证键：必须全局唯一，否则公开入口无法判定租户；
// 配额必须在同一行上原子递增，不能用“先读后写”让并发请求穿透。
package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	geomodel "hivemtk-user/internal/geo/model"
	mainmodel "hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

const (
	gclTenantA = uint(401)
	gclTenantB = uint(402)
)

func gclSetupDB(t *testing.T) *gorm.DB {
	t.Helper()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() {
		tenant.ResetRegistryForTest()
		tenant.SetMode(tenant.ModeOff)
	})
	db := testutil.NewTestDB(t, &geomodel.GeoCaptureLink{}, &mainmodel.Clue{}, &mainmodel.CustomerSession{})
	if db == nil {
		t.Fatal("测试库不可达：承接租户契约判不了")
	}
	return db
}

func TestGeoCaptureLinkRepository_TenantAndQuota(t *testing.T) {
	db := gclSetupDB(t)
	repo := NewGeoCaptureLinkRepositoryWithDB(db)
	ctxA := tenant.WithTenantID(context.Background(), gclTenantA)
	ctxB := tenant.WithTenantID(context.Background(), gclTenantB)
	day := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)

	link := &geomodel.GeoCaptureLink{Code: "promo2026", Title: "演示", SourceID: "geo_link:promo2026"}
	if err := repo.Create(ctxA, link); err != nil {
		t.Fatalf("租户 A 创建承接链接失败: %v", err)
	}
	if link.TenantID != gclTenantA {
		t.Fatalf("Create 未填租户: %d", link.TenantID)
	}
	// 同一个 code 在另一个租户必须被全局唯一索引拒绝。
	if err := repo.Create(ctxB, &geomodel.GeoCaptureLink{Code: "promo2026"}); err == nil {
		t.Fatal("code 跨租户重复却写入了：公开入口无法判定归属")
	}

	for i := 0; i < 2; i++ {
		ok, err := repo.ReserveAsk(ctxA, link.ID, day, 2)
		if err != nil || !ok {
			t.Fatalf("第 %d 次 FAQ 配额应通过: ok=%v err=%v", i+1, ok, err)
		}
	}
	if ok, err := repo.ReserveAsk(ctxA, link.ID, day, 2); err != nil || ok {
		t.Fatalf("超限后仍放行: ok=%v err=%v", ok, err)
	}
	if ok, err := repo.ReserveAsk(ctxA, link.ID, day.AddDate(0, 0, 1), 2); err != nil || !ok {
		t.Fatalf("次日配额未重置: ok=%v err=%v", ok, err)
	}

	if ok, err := repo.ReserveLead(ctxB, link.ID, day, 1); err != nil || ok {
		t.Fatalf("租户 B 不应操作租户 A 的链接: ok=%v err=%v", ok, err)
	}
	if ok, err := repo.ReserveLead(ctxA, link.ID, day, 1); err != nil || !ok {
		t.Fatalf("首次留资配额应通过: ok=%v err=%v", ok, err)
	}
	if ok, _ := repo.ReserveLead(ctxA, link.ID, day, 1); ok {
		t.Fatal("留资超限后仍放行")
	}
	if err := repo.ReleaseLead(ctxA, link.ID, day); err != nil {
		t.Fatalf("回滚留资计数失败: %v", err)
	}
	if ok, err := repo.ReserveLead(ctxA, link.ID, day, 1); err != nil || !ok {
		t.Fatalf("回滚后名额未释放: ok=%v err=%v", ok, err)
	}

	got, err := repo.GetByCode(ctxA, "promo2026")
	if err != nil || got.ID != link.ID {
		t.Fatalf("按 code 读取失败: %+v err=%v", got, err)
	}
	if _, err := repo.GetByCode(ctxB, "promo2026"); err == nil {
		t.Fatal("租户 B 读到了租户 A 的承接链接")
	}
}

func TestGeoCaptureLinkRepository_ResponseStats(t *testing.T) {
	db := gclSetupDB(t)
	repo := NewGeoCaptureLinkRepositoryWithDB(db)
	ctxA := tenant.WithTenantID(context.Background(), gclTenantA)
	ctxB := tenant.WithTenantID(context.Background(), gclTenantB)
	source := "geo_link:resp"

	link := &geomodel.GeoCaptureLink{Code: "resp", SourceID: source}
	if err := repo.Create(ctxA, link); err != nil {
		t.Fatalf("创建链接失败: %v", err)
	}

	clueA := &mainmodel.Clue{Account: "13800000001", SourceID: source, OneID: "one-a", Type: 18}
	if err := db.WithContext(ctxA).Create(clueA).Error; err != nil {
		t.Fatalf("写线索 A 失败: %v", err)
	}
	replyAt := time.Unix(clueA.CreateTime, 0).Add(120 * time.Second)
	if err := db.WithContext(ctxA).Create(&mainmodel.CustomerSession{
		SessionID: "sess-a", OneID: "one-a", FirstHumanReplyAt: &replyAt,
	}).Error; err != nil {
		t.Fatalf("写会话 A 失败: %v", err)
	}

	clueB := &mainmodel.Clue{Account: "13800000002", SourceID: source, OneID: "one-b", Type: 18}
	if err := db.WithContext(ctxA).Create(clueB).Error; err != nil {
		t.Fatalf("写线索 B 失败: %v", err)
	}
	if err := db.WithContext(ctxA).Create(&mainmodel.CustomerSession{SessionID: "sess-b", OneID: "one-b"}).Error; err != nil {
		t.Fatalf("写无首响会话失败: %v", err)
	}

	// 无会话的线索不计 matched，否则“未接入”会被误报成“零延迟”。
	if err := db.WithContext(ctxA).Create(&mainmodel.Clue{Account: "13800000003", SourceID: source, OneID: "", Type: 18}).Error; err != nil {
		t.Fatalf("写无线索会话失败: %v", err)
	}

	// 另一个租户的同 source 线索不得混入。
	clueOther := &mainmodel.Clue{Account: "13900000001", SourceID: source, OneID: "one-other", Type: 18}
	if err := db.WithContext(ctxB).Create(clueOther).Error; err != nil {
		t.Fatalf("写租户 B 线索失败: %v", err)
	}
	otherReply := time.Unix(clueOther.CreateTime, 0).Add(5 * time.Second)
	if err := db.WithContext(ctxB).Create(&mainmodel.CustomerSession{SessionID: "sess-other", OneID: "one-other", FirstHumanReplyAt: &otherReply}).Error; err != nil {
		t.Fatalf("写租户 B 会话失败: %v", err)
	}

	stats, err := repo.ResponseStats(ctxA, source)
	if err != nil {
		t.Fatalf("首响统计失败: %v", err)
	}
	if stats.Matched != 2 || stats.Replied != 1 {
		t.Fatalf("首响匹配口径不对: %+v", stats)
	}
	if stats.AvgSeconds < 119 || stats.AvgSeconds > 121 {
		t.Fatalf("首响耗时不对: %+v", stats)
	}

	empty, err := repo.ResponseStats(ctxA, fmt.Sprintf("%s-none", source))
	if err != nil {
		t.Fatalf("空统计失败: %v", err)
	}
	if empty.Matched != 0 {
		t.Fatalf("不存在的来源不应有匹配: %+v", empty)
	}
}
