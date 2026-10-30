package repository

// 模板/技能市场写侧闭环仓储的真库用例（specs/013 S-22）。
//
// 锁的不是 CRUD（GORM 不会错），而是三条**契约**：
//  1. market_catalog_entries 必须是「平台级、无 tenant_id」——公开橱窗匿名读
//     是它的存在理由（锚点见 router/cross_tenant_scan_test.go）；
//     租户 on 模式下匿名 ctx 读它不得被租户闸拦死。
//  2. market_entry_orders 的租户隔离：on 模式下买家订单读写都圈在自己租户内。
//  3. 条件跃迁（UpdateEntryStatus/UpdateOrderStatus）的乐观并发语义：
//     Where 带 from 状态，0 行 = 别人先动 = 返回 false，绝不覆盖写。

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

func marketEntryRepoDB(t *testing.T) (*MarketEntryRepository, context.Context) {
	t.Helper()
	gdb := testutil.NewTestDB(t, &model.MarketCatalogEntry{}, &model.MarketEntryOrder{})
	if gdb == nil {
		t.Fatal("测试库不可达：闭环仓储契约判不了（不 Skip，跳过等于没锁）")
	}
	return NewMarketEntryRepository(gdb), context.Background()
}

func marketSeedEntry(id, status string, price int64) *model.MarketCatalogEntry {
	return &model.MarketCatalogEntry{
		ID: id, Kind: "template", Name: "条目 " + id, Category: "channel_card",
		Summary: "真库用例条目", Source: "internal/template/card_chat.html",
		Availability: "builtin", Tags: "[]", Status: status, PriceCents: price,
		SubmitterID: 9101,
	}
}

// TestMarketEntryRepositoryCatalogIsPlatformLevel 目录表是平台级共享橱窗：
// 租户 on 模式下、ctx 未绑定租户（匿名公开橱窗的形状）读写照常可用。
func TestMarketEntryRepositoryCatalogIsPlatformLevel(t *testing.T) {
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	repo, _ := marketEntryRepoDB(t) // 匿名 ctx：不绑租户
	anonymous := context.Background()

	if err := repo.CreateEntry(anonymous, marketSeedEntry("repo-plat-1", model.MarketEntryPending, 0)); err != nil {
		t.Fatalf("匿名 ctx 写平台级目录条目失败（被租户闸拦=橱窗全废）: %v", err)
	}
	row, err := repo.GetEntry(anonymous, "repo-plat-1")
	if err != nil || row == nil {
		t.Fatalf("匿名 ctx 读目录条目失败: row=%+v err=%v", row, err)
	}
	rows, err := repo.ListApprovedEntries(anonymous)
	if err != nil {
		t.Fatalf("匿名 ctx 读已上架条目失败: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("pending 条目不应出现在已上架清单，实得 %d 条", len(rows))
	}
	if _, err := repo.GetEntry(anonymous, "no-such"); err != nil {
		t.Errorf("查无应返回 (nil,nil) 而不是错误，实得 %v", err)
	}
}

// TestMarketEntryRepositoryConditionalTransit 条件跃迁：from 状态不匹配 0 行=false。
func TestMarketEntryRepositoryConditionalTransit(t *testing.T) {
	repo, ctx := marketEntryRepoDB(t)
	if err := repo.CreateEntry(ctx, marketSeedEntry("repo-transit", model.MarketEntryPending, 0)); err != nil {
		t.Fatalf("落库失败: %v", err)
	}

	ok, err := repo.UpdateEntryStatus(ctx, "repo-transit", model.MarketEntryApproved, model.MarketEntryDisabled, nil)
	if err != nil {
		t.Fatalf("条件跃迁执行失败: %v", err)
	}
	if ok {
		t.Fatal("from=approved 不匹配 pending 行，应返回 false（0 行）")
	}
	row, _ := repo.GetEntry(ctx, "repo-transit")
	if row.Status != model.MarketEntryPending {
		t.Fatalf("不匹配的条件跃迁不得改状态，实得 %s", row.Status)
	}

	ok, err = repo.UpdateEntryStatus(ctx, "repo-transit", model.MarketEntryPending, model.MarketEntryApproved,
		map[string]any{"reviewed_by": int64(7)})
	if err != nil || !ok {
		t.Fatalf("匹配的条件跃迁应成功: ok=%v err=%v", ok, err)
	}
	row, _ = repo.GetEntry(ctx, "repo-transit")
	if row.Status != model.MarketEntryApproved || row.ReviewedBy != 7 {
		t.Fatalf("跃迁后状态/审核人不对: status=%s reviewed_by=%d", row.Status, row.ReviewedBy)
	}

	// 幂等重放（第二个人再审一次）：0 行=false。
	ok, err = repo.UpdateEntryStatus(ctx, "repo-transit", model.MarketEntryPending, model.MarketEntryRejected, nil)
	if err != nil {
		t.Fatalf("重放执行失败: %v", err)
	}
	if ok {
		t.Fatal("同 from 状态已被消费，重放应返回 false")
	}
}

// TestMarketEntryRepositoryOrderTenantIsolation 订单租户隔离：
// on 模式下 A 租户买家的单，B 租户的读与写都摸不到。
func TestMarketEntryRepositoryOrderTenantIsolation(t *testing.T) {
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	repo, _ := marketEntryRepoDB(t)
	ctxA := tenant.WithTenantID(context.Background(), 71)
	ctxB := tenant.WithTenantID(context.Background(), 72)

	order := &model.MarketEntryOrder{
		OrderNo: "TMREPOISO1", EntryID: "repo-iso", EntryName: "隔离条目",
		BuyerID: 9201, AmountCents: 9900, Status: model.MarketOrderPendingPayment,
		ContactPhone: "13800000000",
	}
	if err := repo.CreateOrder(ctxA, order); err != nil {
		t.Fatalf("A 租户建单失败: %v", err)
	}

	got, err := repo.GetOrderByNo(ctxA, "TMREPOISO1")
	if err != nil || got == nil || got.BuyerID != 9201 {
		t.Fatalf("A 租户读自己的单失败: got=%+v err=%v", got, err)
	}
	if b, _ := repo.GetOrderByNo(ctxB, "TMREPOISO1"); b != nil {
		t.Fatalf("B 租户读到了 A 租户的订单（租户隔离失效）: %+v", b)
	}

	ok, err := repo.UpdateOrderStatus(ctxB, "TMREPOISO1", model.MarketOrderPendingPayment, model.MarketOrderCancelled, nil)
	if err != nil {
		t.Fatalf("跨租户条件更新不应报错（应 0 行）: %v", err)
	}
	if ok {
		t.Fatal("跨租户订单更新不得生效")
	}
	if cnt, err := repo.CountOrdersByBuyerAndEntry(ctxA, 9201, "repo-iso"); err != nil || cnt != 1 {
		t.Fatalf("活跃单计数应=1: cnt=%d err=%v", cnt, err)
	}
	if paid, err := repo.ListPaidEntriesByBuyer(ctxA, 9201); err != nil || len(paid) != 0 {
		t.Fatalf("待支付单不应算已获得: %v err=%v", paid, err)
	}
}
