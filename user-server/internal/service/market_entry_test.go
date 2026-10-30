package service

// 模板/技能市场写侧闭环的服务层真库用例（specs/013 S-22）。
//
// 一条主线钉整个环：提审 → 审核通过 → 公开橱窗自动合并 → 买家下单
//（免费直通 / 付费待支付）→ 线下收款确认 → 我的已获得。
// 旁路钉住 fail-closed：未上架不可购买、驳回必须给理由、查无/越态/重复
// 各自的哨兵不落 500 口径（controller 映射的判据就是这些 errors.Is）。

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

type marketEntryFixture struct {
	svc    *MarketEntryService
	show   *TemplateMarketService
	buyer  int64
	admin  int64
	nextID int
}

func newMarketEntryFixture(t *testing.T) *marketEntryFixture {
	t.Helper()
	gdb := testutil.NewTestDB(t, &model.MarketCatalogEntry{}, &model.MarketEntryOrder{})
	if gdb == nil {
		t.Fatal("测试库不可达：闭环服务判不了（不 Skip，跳过等于没锁）")
	}
	repo := repository.NewMarketEntryRepository(gdb)
	f := &marketEntryFixture{
		svc:  NewMarketEntryServiceWith(repo, repository.NewMemoryTemplateMarketRepository()),
		show: NewTemplateMarketServiceWithDB(repo),
	}
	return f
}

func (f *marketEntryFixture) id(prefix string) string {
	f.nextID++
	return prefix + "-" + string(rune('a'+f.nextID-1))
}

func (f *marketEntryFixture) validInput() MarketEntryInput {
	return MarketEntryInput{
		Kind: "template", Name: "闭环用例卡片", Category: "channel_card",
		Summary: "发布-审核-支付闭环用例", Source: "internal/template/card_chat.html",
		Availability: "builtin", Tags: []string{"test", "闭环"}, PriceCents: 0,
	}
}

// TestMarketEntryServiceFullLoop 主线：发布 → 审核 → 橱窗合并 → 购买 → 收款确认 → 已获得。
func TestMarketEntryServiceFullLoop(t *testing.T) {
	f := newMarketEntryFixture(t)
	ctx := context.Background()

	// 1) 发布入口：提审落 pending，自动生成 ID。
	in := f.validInput()
	in.PriceCents = 9900
	in.ID = f.id("tpl-loop")
	row, err := f.svc.Submit(ctx, 101, in)
	if err != nil {
		t.Fatalf("提审失败: %v", err)
	}
	if row.Status != model.MarketEntryPending || row.SubmitterID != 101 {
		t.Fatalf("提审落点不对: status=%s submitter=%d", row.Status, row.SubmitterID)
	}

	// 未上架条目对橱窗不可见（detail 按「查无」口径）。
	if _, err := f.show.Get(ctx, row.ID); !errors.Is(err, ErrMarketEntryNotFound) {
		t.Fatalf("未上架条目详情应 ErrMarketEntryNotFound，实得 %v", err)
	}

	// 2) 审核台：通过 → approved，公开橱窗自动合并（列表/分类/详情三面）。
	approved, err := f.svc.Review(ctx, 1, row.ID, true, "")
	if err != nil {
		t.Fatalf("审核通过失败: %v", err)
	}
	if approved.Status != model.MarketEntryApproved || approved.EnabledAt == nil {
		t.Fatalf("上架落点不对: status=%s enabled_at=%v", approved.Status, approved.EnabledAt)
	}
	items, err := f.show.List(ctx, MarketQuery{Keyword: "闭环用例"})
	if err != nil || len(items) != 1 {
		t.Fatalf("橱窗应合并出 1 条，实得 %d 条 err=%v", len(items), err)
	}
	if items[0].ID != row.ID || items[0].PriceCents != 9900 {
		t.Fatalf("橱窗条目不对: %+v", items[0])
	}
	cats, err := f.show.Categories(ctx)
	if err != nil {
		t.Fatalf("分类读取失败: %v", err)
	}
	found := false
	for _, c := range cats {
		if c == "channel_card" {
			found = true
		}
	}
	if !found {
		t.Fatalf("分类清单应含 channel_card: %v", cats)
	}
	if got, err := f.show.Get(ctx, row.ID); err != nil || got == nil || got.Name != in.Name {
		t.Fatalf("已上架条目详情应命中: got=%+v err=%v", got, err)
	}

	// 3) 支付订单接线：付费单缺联系方式被拦；补齐后落待支付；重复下单幂等回显。
	if _, err := f.svc.CreateOrder(ctx, 202, row.ID, MarketEntryOrderInput{}); !errors.Is(err, ErrMarketOrderNoContact) {
		t.Fatalf("付费单缺联系方式应 ErrMarketOrderNoContact，实得 %v", err)
	}
	created, err := f.svc.CreateOrder(ctx, 202, row.ID, MarketEntryOrderInput{ContactPhone: "13800000001"})
	if err != nil {
		t.Fatalf("下单失败: %v", err)
	}
	if created.FreeDirect || created.Order.Status != model.MarketOrderPendingPayment || created.PaymentGuide == "" {
		t.Fatalf("付费单落点不对: %+v", created)
	}
	dup, err := f.svc.CreateOrder(ctx, 202, row.ID, MarketEntryOrderInput{ContactPhone: "13800000001"})
	if err != nil || !dup.Duplicate {
		t.Fatalf("重复下单应幂等回显: %+v err=%v", dup, err)
	}

	// 未确认收款前不算已获得。
	owned, err := f.svc.ListOwnedEntriesWithMeta(ctx, 202)
	if err != nil || len(owned) != 0 {
		t.Fatalf("待支付单不应进已获得: %v err=%v", owned, err)
	}

	// 4) 线下收款确认 → paid → 我的已获得。
	paid, err := f.svc.MarkPaid(ctx, 1, created.Order.OrderNo)
	if err != nil {
		t.Fatalf("收款确认失败: %v", err)
	}
	if paid.Status != model.MarketOrderPaid || paid.PaidAt == nil || paid.PaidBy != 1 {
		t.Fatalf("收款确认落点不对: %+v", paid)
	}
	owned, err = f.svc.ListOwnedEntriesWithMeta(ctx, 202)
	if err != nil || len(owned) != 1 || owned[0].Entry.ID != row.ID || owned[0].OrderNo != created.Order.OrderNo {
		t.Fatalf("已获得清单不对: %+v err=%v", owned, err)
	}
	// 已支付后重复确认 = 越态。
	if _, err := f.svc.MarkPaid(ctx, 1, created.Order.OrderNo); !errors.Is(err, ErrMarketOrderStateNotAllowed) {
		t.Fatalf("重复确认应 ErrMarketOrderStateNotAllowed，实得 %v", err)
	}
}

// TestMarketEntryServiceFreeEntryDirectGrant 免费条目 0 元直通（领取即获得）。
func TestMarketEntryServiceFreeEntryDirectGrant(t *testing.T) {
	f := newMarketEntryFixture(t)
	ctx := context.Background()
	in := f.validInput()
	in.ID = f.id("tpl-free")
	if _, err := f.svc.Submit(ctx, 101, in); err != nil {
		t.Fatalf("提审失败: %v", err)
	}
	if _, err := f.svc.Review(ctx, 1, in.ID, true, ""); err != nil {
		t.Fatalf("审核失败: %v", err)
	}
	res, err := f.svc.CreateOrder(ctx, 303, in.ID, MarketEntryOrderInput{})
	if err != nil {
		t.Fatalf("免费领取失败: %v", err)
	}
	if !res.FreeDirect || res.Order.Status != model.MarketOrderPaid || res.Order.AmountCents != 0 {
		t.Fatalf("0 元单应直通 paid: %+v", res)
	}
	owned, _ := f.svc.ListOwnedEntriesWithMeta(ctx, 303)
	if len(owned) != 1 || owned[0].Entry.ID != in.ID {
		t.Fatalf("领取后应已获得: %+v", owned)
	}
}

// TestMarketEntryServiceFailClosed 写侧 fail-closed 面：查无/越态/校验/查重。
func TestMarketEntryServiceFailClosed(t *testing.T) {
	f := newMarketEntryFixture(t)
	ctx := context.Background()

	// 校验：kind/availability 值域、空名、空来源、负价。
	bad := f.validInput()
	bad.Kind = "nonsense"
	if _, err := f.svc.Submit(ctx, 101, bad); !errors.Is(err, ErrMarketEntryInvalidParam) {
		t.Fatalf("kind 值域应拦: %v", err)
	}
	bad = f.validInput()
	bad.Name = " "
	if _, err := f.svc.Submit(ctx, 101, bad); !errors.Is(err, ErrMarketEntryInvalidParam) {
		t.Fatalf("空名应拦: %v", err)
	}
	bad = f.validInput()
	bad.Source = ""
	if _, err := f.svc.Submit(ctx, 101, bad); !errors.Is(err, ErrMarketEntryInvalidParam) {
		t.Fatalf("空来源应拦（目录条目必须指向事实源）: %v", err)
	}
	bad = f.validInput()
	bad.PriceCents = -1
	if _, err := f.svc.Submit(ctx, 101, bad); !errors.Is(err, ErrMarketEntryInvalidParam) {
		t.Fatalf("负价应拦: %v", err)
	}
	// ID 字符集：大写/下划线/越界字符不放行（ID 进公开 URL）。
	bad = f.validInput()
	bad.ID = "Bad_ID!"
	if _, err := f.svc.Submit(ctx, 101, bad); !errors.Is(err, ErrMarketEntryInvalidParam) {
		t.Fatalf("非法 ID 应拦: %v", err)
	}
	// 与进程内种子撞名（目录主键全库唯一，含内置命名空间）。
	seed := f.validInput()
	seed.ID = "card-douyin"
	if _, err := f.svc.Submit(ctx, 101, seed); !errors.Is(err, ErrMarketEntryDuplicate) {
		t.Fatalf("与种子撞名应 ErrMarketEntryDuplicate: %v", err)
	}

	// 查无：审核/购买乱敲 ID。
	if _, err := f.svc.Review(ctx, 1, "no-such", true, ""); !errors.Is(err, ErrMarketEntryNotFoundInDB) {
		t.Fatalf("审核查无应 ErrMarketEntryNotFoundInDB: %v", err)
	}
	if _, err := f.svc.CreateOrder(ctx, 202, "no-such", MarketEntryOrderInput{}); !errors.Is(err, ErrMarketEntryNotFoundInDB) {
		t.Fatalf("购买查无应 ErrMarketEntryNotFoundInDB: %v", err)
	}

	// 未上架（pending）不可购买。
	in := f.validInput()
	in.ID = f.id("tpl-pending")
	in.PriceCents = 5000 // 定价条目：后面的撤单环节需要待支付态（免费单 0 元直通没有撤单环节）
	if _, err := f.svc.Submit(ctx, 101, in); err != nil {
		t.Fatalf("提审失败: %v", err)
	}
	if _, err := f.svc.CreateOrder(ctx, 202, in.ID, MarketEntryOrderInput{}); !errors.Is(err, ErrMarketEntryStateNotAllowed) {
		t.Fatalf("未上架条目不可购买: %v", err)
	}

	// 驳回必须给理由；驳回后不在橱窗；同提交人复提回 pending。
	if _, err := f.svc.Review(ctx, 1, in.ID, false, "  "); !errors.Is(err, ErrMarketEntryInvalidParam) {
		t.Fatalf("空理由驳回应拦: %v", err)
	}
	if _, err := f.svc.Review(ctx, 1, in.ID, false, "来源指向的实现不存在"); err != nil {
		t.Fatalf("驳回失败: %v", err)
	}
	rejected, _ := f.svc.ListForReview(ctx, model.MarketEntryRejected)
	if len(rejected) != 1 || rejected[0].RejectReason == "" {
		t.Fatalf("驳回单应带理由: %+v", rejected)
	}
	if items, _ := f.show.List(ctx, MarketQuery{Keyword: "闭环用例卡片"}); len(items) != 0 {
		t.Fatalf("驳回条目不应在橱窗: %v", items)
	}
	if _, err := f.svc.Review(ctx, 1, in.ID, true, ""); !errors.Is(err, ErrMarketEntryStateNotAllowed) {
		t.Fatalf("rejected 直审 approved 应越态拦截: %v", err)
	}
	resubIn := f.validInput()
	resubIn.ID = in.ID // 同 ID 复提（驳回→改完→重新提审走同一入口）
	resubIn.PriceCents = 5000
	resub, err := f.svc.Submit(ctx, 101, resubIn)
	if err != nil {
		t.Fatalf("复提失败: %v", err)
	}
	if resub.ID != in.ID || resub.Status != model.MarketEntryPending {
		t.Fatalf("复提应回 pending: %+v", resub)
	}

	// 下架/复架：approved → disabled → approved；下架后橱窗与购买同时关。
	if _, err := f.svc.Review(ctx, 1, in.ID, true, ""); err != nil {
		t.Fatalf("复审失败: %v", err)
	}
	if _, err := f.svc.Disable(ctx, 1, in.ID); err != nil {
		t.Fatalf("下架失败: %v", err)
	}
	if items, _ := f.show.List(ctx, MarketQuery{Keyword: "闭环用例卡片"}); len(items) != 0 {
		t.Fatalf("下架条目不应在橱窗: %v", items)
	}
	if _, err := f.svc.CreateOrder(ctx, 202, in.ID, MarketEntryOrderInput{}); !errors.Is(err, ErrMarketEntryStateNotAllowed) {
		t.Fatalf("下架条目不可购买: %v", err)
	}
	if _, err := f.svc.Enable(ctx, 1, in.ID); err != nil {
		t.Fatalf("复架失败: %v", err)
	}
	if got, _ := f.show.Get(ctx, in.ID); got == nil {
		t.Fatal("复架后橱窗应恢复")
	}

	// 买家撤单：自己的待支付单可撤；别人的单按「查无」口径（不泄露存在性）。
	created, err := f.svc.CreateOrder(ctx, 202, in.ID, MarketEntryOrderInput{ContactPhone: "13900000000"})
	if err != nil {
		t.Fatalf("下单失败: %v", err)
	}
	if _, err := f.svc.CancelOrder(ctx, 999, created.Order.OrderNo); !errors.Is(err, ErrMarketOrderNotFound) {
		t.Fatalf("撤别人的单应按查无: %v", err)
	}
	cancelled, err := f.svc.CancelOrder(ctx, 202, created.Order.OrderNo)
	if err != nil || cancelled.Status != model.MarketOrderCancelled {
		t.Fatalf("撤单失败: %+v err=%v", cancelled, err)
	}
	// 撤单后可重新下单（活跃单计数不再挡）。
	again, err := f.svc.CreateOrder(ctx, 202, in.ID, MarketEntryOrderInput{ContactPhone: "13900000000"})
	if err != nil || again.Duplicate {
		t.Fatalf("撤单后应可重新下单: %+v err=%v", again, err)
	}
}

// TestMarketEntryServiceOrderTenantScope 租户 on：我的订单/已获得圈在本租户。
func TestMarketEntryServiceOrderTenantScope(t *testing.T) {
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	f := newMarketEntryFixture(t)

	ctxA := tenant.WithTenantID(context.Background(), 81)
	ctxB := tenant.WithTenantID(context.Background(), 82)

	in := f.validInput()
	in.ID = f.id("tpl-tenant")
	in.PriceCents = 0
	if _, err := f.svc.Submit(ctxA, 101, in); err != nil {
		t.Fatalf("提审失败: %v", err)
	}
	if _, err := f.svc.Review(ctxA, 1, in.ID, true, ""); err != nil {
		t.Fatalf("审核失败: %v", err)
	}
	// 目录是平台级：B 租户的买家同样能购买（共享橱窗的语义锚点）。
	if _, err := f.svc.CreateOrder(ctxB, 404, in.ID, MarketEntryOrderInput{}); err != nil {
		t.Fatalf("B 租户买家领取共享条目失败: %v", err)
	}
	// B 的已获得里有、A 的买家没有。
	if owned, _ := f.svc.ListOwnedEntriesWithMeta(ctxB, 404); len(owned) != 1 {
		t.Fatalf("B 买家应已获得 1 条: %v", owned)
	}
	if owned, _ := f.svc.ListOwnedEntriesWithMeta(ctxA, 404); len(owned) != 0 {
		t.Fatalf("A 租户同 ID 买家不应看到 B 的获得: %v", owned)
	}
}
