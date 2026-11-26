package repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

// C02：送达记录必须追加而不是整列覆盖；用真实 JSONB SQL 验证，
// 内存 helper 测试不能证明生产仓储行为。
func TestLeadNotifyAppendDeliveryRecords_DBKeepsHistory(t *testing.T) {
	tenant.SetMode(tenant.ModeOff)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	db := testutil.NewTestDB(t, &model.LeadMiningNotifyRoute{})
	if db == nil {
		t.Fatal("测试库不可达：送达记录追加语义未验证")
	}
	ctx := context.Background()
	repo := NewLeadMiningNotifyRouteRepository()
	route := &model.LeadMiningNotifyRoute{
		TenantID: tenant.DefaultTenantID, RouteID: "append-history-001",
		Scope: "tenant", DeliveryRecords: `[{"clue_id":"old","success":true}]`,
	}
	if err := db.Create(route).Error; err != nil {
		t.Fatalf("预置路由失败: %v", err)
	}
	if err := repo.AppendDeliveryRecords(ctx, route.RouteID, `[{"clue_id":"new","success":false}]`); err != nil {
		t.Fatalf("追加送达记录失败: %v", err)
	}
	var got model.LeadMiningNotifyRoute
	if err := db.Where("route_id = ?", route.RouteID).First(&got).Error; err != nil {
		t.Fatalf("回读路由失败: %v", err)
	}
	var records []map[string]any
	if err := json.Unmarshal([]byte(got.DeliveryRecords), &records); err != nil {
		t.Fatalf("送达记录不是合法 JSON: %v raw=%s", err, got.DeliveryRecords)
	}
	if len(records) != 2 || records[0]["clue_id"] != "old" || records[1]["clue_id"] != "new" {
		t.Fatalf("历史被覆盖或新记录丢失: %s", got.DeliveryRecords)
	}
}

// TestLeadNotifyAppendDeliveryRecords_DBMissingRouteFails 虚拟/已删除路由写 0 行必须失败。
func TestLeadNotifyAppendDeliveryRecords_DBMissingRouteFails(t *testing.T) {
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOff)
	t.Cleanup(func() { tenant.SetMode(previous) })
	db := testutil.NewTestDB(t, &model.LeadMiningNotifyRoute{})
	if db == nil {
		t.Fatal("测试库不可达：零行写入语义未验证")
	}
	repo := NewLeadMiningNotifyRouteRepository()
	err := repo.AppendDeliveryRecords(context.Background(), "account-owner-fallback", `[{"success":false,"error":"no_recipient"}]`)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("不存在的路由追加送达记录应返回 ErrRecordNotFound，实际 err=%v", err)
	}
}
