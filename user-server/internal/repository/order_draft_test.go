// order_draft_test.go T-P2-01：订单草稿仓储（表 order_drafts）。
//
// 本表是第一次落地（开工前草稿只在内存 map 里），所以这里的重点不是"读得到读不到"，
// 而是三件**只有真库才能证明**的事：
//  1. 部分唯一索引 uq_order_draft_pending 真的被 GORM 标签建出来了（pg_indexes 直查）；
//  2. ExpirePendingBatch / MutatePending 的"判态+改写"是一步，不是读—判—写三步；
//  3. PurgeTerminal 的删除集合封闭在终态内（pending/confirmed 绝不会被清掉）。
package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupOrderDraftTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t, &model.OrderDraft{})
}

// newDraftRow 造一行草稿。时间戳全部显式给出：本文件的断言一半在排序上，
// 依赖 GORM 的 autoCreateTime 会让"同一秒内插入的两行谁在前"变成随机答案。
func newDraftRow(id, customer, product, status string) *model.OrderDraft {
	now := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	return &model.OrderDraft{
		ID:          id,
		CustomerID:  customer,
		OwnerID:     "sales-1",
		ProductName: product,
		Quantity:    1,
		UnitPrice:   100,
		TotalAmount: 100,
		Confidence:  0.8,
		Source:      "ai_chat",
		Status:      status,
		CreatedAt:   now,
		UpdatedAt:   now,
		ExpiresAt:   now.Add(7 * 24 * time.Hour),
		Metadata:    model.JSONMap{},
	}
}

func TestOrderDraftRepo_NilHandle(t *testing.T) {
	repo := NewOrderDraftRepositoryWithDB(nil)
	if repo.Available() {
		t.Fatal("nil 句柄时 Available 应为 false")
	}
	ctx := context.Background()
	// 每个方法都要报错：本仓库的任何一条路都不允许"句柄没了 ⇒ 回空结果"，
	// 因为空草稿列表在销售工作台上是合法值，读侧分不出"没有"和"查不动"。
	if err := repo.Upsert(ctx, newDraftRow("d1", "c1", "P", model.OrderDraftStatusPending)); err == nil {
		t.Error("Upsert 应报错")
	}
	if _, err := repo.GetByID(ctx, "d1"); err == nil {
		t.Error("GetByID 应报错")
	}
	if _, err := repo.ListPendingByCustomer(ctx, "c1"); err == nil {
		t.Error("ListPendingByCustomer 应报错")
	}
	if _, err := repo.ListPending(ctx, "", time.Now(), 10); err == nil {
		t.Error("ListPending 应报错")
	}
	if _, err := repo.ListByCustomer(ctx, "c1"); err == nil {
		t.Error("ListByCustomer 应报错")
	}
	if _, err := repo.ListByOwner(ctx, "sales-1"); err == nil {
		t.Error("ListByOwner 应报错")
	}
	if _, err := repo.ExpirePendingBatch(ctx, time.Now(), 10); err == nil {
		t.Error("ExpirePendingBatch 应报错")
	}
	if _, err := repo.MutatePending(ctx, "d1", nil); err == nil {
		t.Error("MutatePending 应报错")
	}
	if _, err := repo.PurgeTerminal(ctx, time.Now(), 10); err == nil {
		t.Error("PurgeTerminal 应报错")
	}
	if _, err := repo.CountByStatus(ctx); err == nil {
		t.Error("CountByStatus 应报错")
	}
}

func TestOrderDraftRepo_UpsertRoundTrip(t *testing.T) {
	repo := NewOrderDraftRepositoryWithDB(setupOrderDraftTestDB(t))
	ctx := context.Background()

	row := newDraftRow("draft_rt_1", "cust-1", "光子嫩肤 3 次", model.OrderDraftStatusPending)
	row.OneID = "one-9"
	row.ProductID = "sku-9"
	row.Category = "医美"
	row.Quantity = 3
	row.UnitPrice = 760
	row.TotalAmount = 2280
	row.SourceText = "客户说想再做三次"
	row.IntentID = "intent-raw"
	row.Note = "客户偏好周末"
	row.CancelReason = ""
	row.Metadata = model.JSONMap{"category": "医美", "raw_intent": "想再做三次"}
	if err := repo.Upsert(ctx, row); err != nil {
		t.Fatalf("Upsert 失败：%v", err)
	}

	got, err := repo.GetByID(ctx, row.ID)
	if err != nil || got == nil {
		t.Fatalf("GetByID 期望命中，实际 (%v,%v)", got, err)
	}
	if got.CustomerID != "cust-1" || got.OneID != "one-9" || got.ProductID != "sku-9" {
		t.Errorf("标识列回读不一致：%+v", got)
	}
	// 中文产品名是本表最现实的写入风险（列宽/字符集），显式断言而不是"应该没问题"。
	if got.ProductName != "光子嫩肤 3 次" {
		t.Errorf("中文产品名应原样回读，实际 %q", got.ProductName)
	}
	if got.TotalAmount != 2280 || got.UnitPrice != 760 {
		t.Errorf("金额回读异常：%v / %v", got.UnitPrice, got.TotalAmount)
	}
	if got.Metadata["raw_intent"] != "想再做三次" {
		t.Errorf("metadata 应进 jsonb 并原样回读，实际 %+v", got.Metadata)
	}
	if !got.ExpiresAt.Equal(row.ExpiresAt) {
		t.Errorf("到期时间应原样回读，实际 %v vs %v", got.ExpiresAt, row.ExpiresAt)
	}

	// 同 ID 再写一次 = 更新而非插入（销售改价走的就是这条路）。
	row.Quantity = 5
	row.TotalAmount = 3800
	row.Status = model.OrderDraftStatusPending
	if err := repo.Upsert(ctx, row); err != nil {
		t.Fatalf("二次 Upsert 失败：%v", err)
	}
	again, err := repo.GetByID(ctx, row.ID)
	if err != nil || again == nil {
		t.Fatalf("二次读取期望命中，实际 (%v,%v)", again, err)
	}
	if again.Quantity != 5 || again.TotalAmount != 3800 {
		t.Errorf("同 ID 应更新而非新建，实际 %+v", again)
	}
	counts, err := repo.CountByStatus(ctx)
	if err != nil {
		t.Fatalf("CountByStatus 失败：%v", err)
	}
	if counts[model.OrderDraftStatusPending] != 1 {
		t.Errorf("同 ID 两次写应仍为 1 行，实际 %+v", counts)
	}

	missing, err := repo.GetByID(ctx, "不存在的草稿")
	if err != nil {
		t.Fatalf("不存在不是错误：%v", err)
	}
	if missing != nil {
		t.Errorf("不存在应回 (nil,nil)，实际 %+v", missing)
	}
}

// 空草稿/空 ID 必须报错：草稿 ID 是 sales_events.draft_id 的关联键，
// 一行 id=” 的草稿会以"看不见的幽灵行"形式永久留在库里。
func TestOrderDraftRepo_UpsertRejectsEmptyKey(t *testing.T) {
	repo := NewOrderDraftRepositoryWithDB(setupOrderDraftTestDB(t))
	ctx := context.Background()
	if err := repo.Upsert(ctx, nil); err == nil {
		t.Error("nil 草稿应报错")
	}
	blank := newDraftRow("", "c", "P", model.OrderDraftStatusPending)
	if err := repo.Upsert(ctx, blank); err == nil {
		t.Error("空 ID 应报错")
	}
}

// AC①/AC④ 的库侧前提：草稿 ID 是稳定业务键，且 sales_events.draft_id 能 JOIN 回来。
//
// 这条测试刻意做"重启"的等价动作：断开连接池、换新句柄再读。表里没有的东西
// 在这个动作下必然消失 —— 这正是开工前的实际状态（内存 map），所以 AC① 值得用真库钉。
func TestOrderDraftRepo_SurvivesHandleReopenAndJoinsSalesEvents(t *testing.T) {
	database := testutil.NewTestDB(t, &model.OrderDraft{}, &model.SalesEvent{})
	ctx := context.Background()

	row := newDraftRow("draft_reopen_1", "cust-reopen", "热玛吉", model.OrderDraftStatusPending)
	if err := NewOrderDraftRepositoryWithDB(database).Upsert(ctx, row); err != nil {
		t.Fatalf("Upsert 失败：%v", err)
	}
	// 事件流里带上这个 draft_id（口径同 service.stats.RecordOrderDraft）。
	ev := model.SalesEvent{
		EventType:   model.SalesEventTypeOrderDraft,
		DraftID:     row.ID,
		CustomerID:  row.CustomerID,
		OwnerID:     row.OwnerID,
		ProductName: row.ProductName,
		Amount:      row.TotalAmount,
		Action:      "created",
		OccurredAt:  row.CreatedAt,
	}
	if err := database.Create(&ev).Error; err != nil {
		t.Fatalf("写入销售事件失败：%v", err)
	}

	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("取底层 sql.DB 失败：%v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("关闭连接池失败：%v", err)
	}

	reopened, err := gorm.Open(database.Dialector, &gorm.Config{})
	if err != nil {
		t.Fatalf("重开连接失败：%v", err)
	}
	got, err := NewOrderDraftRepositoryWithDB(reopened).GetByID(ctx, row.ID)
	if err != nil || got == nil {
		t.Fatalf("重开连接后草稿应仍在，实际 (%v,%v)", got, err)
	}
	if got.Status != model.OrderDraftStatusPending {
		t.Errorf("重开后状态应不变，实际 %s", got.Status)
	}

	var joined struct {
		DraftID    string
		Product    string
		Status     string
		CustomerID string
	}
	err = reopened.WithContext(ctx).Table("sales_events AS e").
		Select("e.draft_id AS draft_id, d.product_name AS product, d.status AS status, d.customer_id AS customer_id").
		Joins("JOIN order_drafts d ON d.id = e.draft_id").
		Where("e.draft_id = ?", row.ID).
		Scan(&joined).Error
	if err != nil {
		t.Fatalf("JOIN 查询失败：%v", err)
	}
	if joined.DraftID != row.ID || joined.Product != "热玛吉" || joined.Status != model.OrderDraftStatusPending {
		t.Errorf("sales_events.draft_id 应能关联到草稿行，实际 %+v", joined)
	}
	if joined.CustomerID != "cust-reopen" {
		t.Errorf("JOIN 应带回草稿侧字段，实际 %+v", joined)
	}
}

// 部分唯一索引必须真实存在。
//
// GORM 标签写错（拼错 where、priority 少一个）时 AutoMigrate 不会报错，只会静默不建索引，
// 于是"同客户同产品两条 pending"这条约束只剩进程内判据 —— 多副本下失效。
// 所以这里直查 pg_indexes，而不是只断言"第二次写入报错"（后者在建不出索引时也可能因
// 别的原因失败，证明力不够）。
func TestOrderDraftRepo_PendingUniqueIndexExists(t *testing.T) {
	database := setupOrderDraftTestDB(t)
	var def string
	err := database.Raw(
		"SELECT indexdef FROM pg_indexes WHERE tablename = 'order_drafts' AND indexname = 'uq_order_draft_pending'",
	).Scan(&def).Error
	if err != nil {
		t.Fatalf("查询 pg_indexes 失败：%v", err)
	}
	if def == "" {
		t.Fatal("uq_order_draft_pending 未被创建：GORM 的 uniqueIndex+where 标签没生效")
	}
	// 只断言"含 where 且含 'pending'"而不整串比对：PG 会把谓词规范化成
	// `where ((status)::text = 'pending'::text)`，逐字比对等于把 PG 版本的输出格式钉进测试。
	def = strings.ToLower(def)
	for _, want := range []string{"unique", "customer_id", "product_name", "where", "'pending'"} {
		if !strings.Contains(def, strings.ToLower(want)) {
			t.Errorf("索引定义缺少 %q，实际：%s", want, def)
		}
	}
}

func TestOrderDraftRepo_PendingUniqueConflict(t *testing.T) {
	repo := NewOrderDraftRepositoryWithDB(setupOrderDraftTestDB(t))
	ctx := context.Background()

	first := newDraftRow("draft_u1", "cust-u", "光子嫩肤", model.OrderDraftStatusPending)
	if err := repo.Upsert(ctx, first); err != nil {
		t.Fatalf("首行写入失败：%v", err)
	}

	dup := newDraftRow("draft_u2", "cust-u", "光子嫩肤", model.OrderDraftStatusPending)
	err := repo.Upsert(ctx, dup)
	if !errors.Is(err, ErrOrderDraftPendingConflict) {
		t.Fatalf("同客户同产品第二条 pending 应返回冲突哨兵，实际：%v", err)
	}
	// 冲突行必须真的没进去（否则"判重失败"会以两条重复草稿出现在销售工作台上）。
	if got, e := repo.GetByID(ctx, dup.ID); e != nil || got != nil {
		t.Errorf("冲突行不应落库，实际 (%v,%v)", got, e)
	}

	// 部分索引的另一半：状态不是 pending 就不占坑 —— 首行取消后同产品可再建。
	if _, e := repo.MutatePending(ctx, first.ID, func(m *model.OrderDraft) {
		m.Status = model.OrderDraftStatusCancelled
	}); e != nil {
		t.Fatalf("取消首行失败：%v", e)
	}
	if err := repo.Upsert(ctx, dup); err != nil {
		t.Errorf("首行已是终态，同产品再建 pending 应成功，实际：%v", err)
	}

	// 不同客户 / 不同产品都不该撞约束。
	other := newDraftRow("draft_u3", "cust-other", "光子嫩肤", model.OrderDraftStatusPending)
	if err := repo.Upsert(ctx, other); err != nil {
		t.Errorf("不同客户同产品不应冲突，实际：%v", err)
	}
	otherProduct := newDraftRow("draft_u4", "cust-u", "热玛吉", model.OrderDraftStatusPending)
	if err := repo.Upsert(ctx, otherProduct); err != nil {
		t.Errorf("同客户不同产品不应冲突，实际：%v", err)
	}
}

// 编辑不得挪动 created_at / 生命周期：draftWriteColumns 排除这两列的用意。
//
// 若把 created_at 也纳入更新列（Select("*") 的默认行为），一次改价就会把草稿的
// 7 天寿命重置一次 —— 工作台上的"陈旧草稿"于是永远不会到期。
func TestOrderDraftRepo_EditDoesNotRebirthDraft(t *testing.T) {
	repo := NewOrderDraftRepositoryWithDB(setupOrderDraftTestDB(t))
	ctx := context.Background()

	origin := newDraftRow("draft_age", "cust-age", "水光针", model.OrderDraftStatusPending)
	origin.CreatedAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	origin.ExpiresAt = time.Date(2026, 1, 8, 0, 0, 0, 0, time.UTC)
	if err := repo.Upsert(ctx, origin); err != nil {
		t.Fatalf("写入失败：%v", err)
	}

	snapshot := *origin
	snapshot.CreatedAt = time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC) // 恶意/意外带上的新创建时间
	snapshot.Quantity = 9
	if err := repo.Upsert(ctx, &snapshot); err != nil {
		t.Fatalf("更新失败：%v", err)
	}

	got, err := repo.GetByID(ctx, origin.ID)
	if err != nil || got == nil {
		t.Fatalf("读取失败：(%v,%v)", got, err)
	}
	if !got.CreatedAt.Equal(origin.CreatedAt) {
		t.Errorf("created_at 不应被更新改写：%v vs %v", got.CreatedAt, origin.CreatedAt)
	}
	if got.Quantity != 9 {
		t.Errorf("业务列应正常更新，实际 %d", got.Quantity)
	}
}

func TestOrderDraftRepo_ListPendingFilterAndOrder(t *testing.T) {
	repo := NewOrderDraftRepositoryWithDB(setupOrderDraftTestDB(t))
	ctx := context.Background()
	base := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	now := base.Add(3 * time.Hour)

	type spec struct {
		id      string
		owner   string
		conf    float64
		amount  float64
		expires time.Time
		status  string
	}
	specs := []spec{
		// 高置信优先
		{id: "l1", owner: "a", conf: 0.9, amount: 100, expires: base.Add(24 * time.Hour), status: model.OrderDraftStatusPending},
		// 同置信比金额（金额判据在到期时间之前，所以 l2 排在 l3 前）
		{id: "l2", owner: "b", conf: 0.9, amount: 500, expires: base.Add(24 * time.Hour), status: model.OrderDraftStatusPending},
		// 同置信同金额比到期（越早越前）
		{id: "l3", owner: "a", conf: 0.9, amount: 100, expires: base.Add(4 * time.Hour), status: model.OrderDraftStatusPending},
		// 低置信
		{id: "l4", owner: "a", conf: 0.5, amount: 9999, expires: base.Add(24 * time.Hour), status: model.OrderDraftStatusPending},
		// 已到期 → 不出现（判据是 expires_at > now，等号在 ExpirePendingBatch 那边算到期）
		{id: "l5", owner: "a", conf: 0.99, amount: 9999, expires: base.Add(-time.Hour), status: model.OrderDraftStatusPending},
		// 非 pending → 不出现
		{id: "l6", owner: "a", conf: 0.99, amount: 9999, expires: base.Add(24 * time.Hour), status: model.OrderDraftStatusConfirmed},
	}
	for _, s := range specs {
		row := newDraftRow(s.id, "cust-"+s.id, "产品-"+s.id, s.status)
		row.OwnerID = s.owner
		row.Confidence = s.conf
		row.UnitPrice = s.amount
		row.TotalAmount = s.amount
		row.ExpiresAt = s.expires
		if err := repo.Upsert(ctx, row); err != nil {
			t.Fatalf("写入 %s 失败：%v", s.id, err)
		}
	}

	all, err := repo.ListPending(ctx, "", now, 0)
	if err != nil {
		t.Fatalf("ListPending 失败：%v", err)
	}
	var ids []string
	for _, r := range all {
		ids = append(ids, r.ID)
	}
	want := []string{"l2", "l3", "l1", "l4"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Errorf("排序期望 %v，实际 %v", want, ids)
	}

	ownerA, err := repo.ListPending(ctx, "a", now, 0)
	if err != nil {
		t.Fatalf("按销售过滤失败：%v", err)
	}
	for _, r := range ownerA {
		if r.OwnerID != "a" {
			t.Errorf("owner 过滤漏了：%+v", r)
		}
	}
	if len(ownerA) != 3 {
		t.Errorf("owner=a 期望 3 条，实际 %d", len(ownerA))
	}

	limited, err := repo.ListPending(ctx, "", now, 2)
	if err != nil || len(limited) != 2 {
		t.Fatalf("limit=2 期望 2 条，实际 %d（err=%v）", len(limited), err)
	}
	if limited[0].ID != "l2" {
		t.Errorf("limit 应截断排序前缀，实际首条 %s", limited[0].ID)
	}
}

func TestOrderDraftRepo_ListByCustomerAndOwner(t *testing.T) {
	repo := NewOrderDraftRepositoryWithDB(setupOrderDraftTestDB(t))
	ctx := context.Background()
	base := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)

	// 同一客户三条（含终态），创建时间不同
	for i, st := range []string{model.OrderDraftStatusPending, model.OrderDraftStatusCancelled, model.OrderDraftStatusConfirmed} {
		row := newDraftRow(fmt.Sprintf("bc-%d", i), "cust-bc", "产品", st)
		row.CreatedAt = base.Add(time.Duration(i) * time.Hour)
		row.UpdatedAt = base.Add(time.Duration(i) * time.Hour)
		if err := repo.Upsert(ctx, row); err != nil {
			t.Fatalf("写入失败：%v", err)
		}
	}
	// 另一个客户一条，不该混进来
	other := newDraftRow("bc-other", "cust-zzz", "产品", model.OrderDraftStatusPending)
	if err := repo.Upsert(ctx, other); err != nil {
		t.Fatalf("写入失败：%v", err)
	}

	byCustomer, err := repo.ListByCustomer(ctx, "cust-bc")
	if err != nil {
		t.Fatalf("ListByCustomer 失败：%v", err)
	}
	if len(byCustomer) != 3 {
		t.Fatalf("期望 3 条（含终态），实际 %d", len(byCustomer))
	}
	// 创建时间倒序 ⇒ bc-2, bc-1, bc-0
	if byCustomer[0].ID != "bc-2" || byCustomer[2].ID != "bc-0" {
		t.Errorf("应按创建时间倒序，实际 %s..%s", byCustomer[0].ID, byCustomer[2].ID)
	}

	// ListByOwner：pending 必须排在终态之前（工作台第一屏只看待办）
	row := newDraftRow("bo-newer-terminal", "cust-bo", "产品", model.OrderDraftStatusCancelled)
	row.OwnerID = "owner-bo"
	row.CreatedAt = base.Add(10 * time.Hour) // 比 pending 那条更晚创建
	if err := repo.Upsert(ctx, row); err != nil {
		t.Fatalf("写入失败：%v", err)
	}
	pendingRow := newDraftRow("bo-pending", "cust-bo2", "产品", model.OrderDraftStatusPending)
	pendingRow.OwnerID = "owner-bo"
	pendingRow.CreatedAt = base
	if err := repo.Upsert(ctx, pendingRow); err != nil {
		t.Fatalf("写入失败：%v", err)
	}
	byOwner, err := repo.ListByOwner(ctx, "owner-bo")
	if err != nil {
		t.Fatalf("ListByOwner 失败：%v", err)
	}
	if len(byOwner) != 2 {
		t.Fatalf("期望 2 条，实际 %d", len(byOwner))
	}
	if byOwner[0].ID != "bo-pending" {
		t.Errorf("pending 应优先于更晚创建的终态草稿，实际首条 %s", byOwner[0].ID)
	}

	// 空客户 / 空销售 = 0 条且不是错误
	if rows, err := repo.ListByCustomer(ctx, "不存在的客户"); err != nil || len(rows) != 0 {
		t.Errorf("不存在客户期望 (0,nil)，实际 (%d,%v)", len(rows), err)
	}
}

func TestOrderDraftRepo_ListPendingByCustomerOnlyPending(t *testing.T) {
	repo := NewOrderDraftRepositoryWithDB(setupOrderDraftTestDB(t))
	ctx := context.Background()
	for i, st := range []string{model.OrderDraftStatusPending, model.OrderDraftStatusExpired, model.OrderDraftStatusConfirmed} {
		row := newDraftRow(fmt.Sprintf("pbc-%d", i), "cust-pbc", fmt.Sprintf("产品%d", i), st)
		if err := repo.Upsert(ctx, row); err != nil {
			t.Fatalf("写入失败：%v", err)
		}
	}
	// 到期但仍是 pending 的行必须在候选集里 —— 去重判据不能漏它，
	// 否则新意向会去撞 uq_order_draft_pending（该索引不看 expires_at）。
	overdue := newDraftRow("pbc-overdue", "cust-pbc", "产品X", model.OrderDraftStatusPending)
	overdue.ExpiresAt = time.Now().Add(-time.Hour)
	if err := repo.Upsert(ctx, overdue); err != nil {
		t.Fatalf("写入过期 pending 失败：%v", err)
	}

	rows, err := repo.ListPendingByCustomer(ctx, "cust-pbc")
	if err != nil {
		t.Fatalf("ListPendingByCustomer 失败：%v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("期望 2 条 pending（含已到期），实际 %d：%+v", len(rows), rows)
	}
	if rows[0].ID > rows[1].ID {
		t.Errorf("应按创建时间升序（同秒时按 id 兜底），实际 %s, %s", rows[0].ID, rows[1].ID)
	}
}

func TestOrderDraftRepo_ExpirePendingBatch(t *testing.T) {
	repo := NewOrderDraftRepositoryWithDB(setupOrderDraftTestDB(t))
	ctx := context.Background()
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

	mk := func(id string, status string, expires time.Time) {
		row := newDraftRow(id, "cust-"+id, "产品", status)
		row.ExpiresAt = expires
		row.CreatedAt = now.Add(-24 * time.Hour)
		row.UpdatedAt = now.Add(-24 * time.Hour)
		if err := repo.Upsert(ctx, row); err != nil {
			t.Fatalf("写入 %s 失败：%v", id, err)
		}
	}
	mk("e-overdue-1", model.OrderDraftStatusPending, now.Add(-time.Hour))
	mk("e-overdue-2", model.OrderDraftStatusPending, now.Add(-time.Minute))
	mk("e-fresh", model.OrderDraftStatusPending, now.Add(time.Hour))
	mk("e-confirmed", model.OrderDraftStatusConfirmed, now.Add(-time.Hour))
	mk("e-cancelled", model.OrderDraftStatusCancelled, now.Add(-time.Hour))

	flipped, err := repo.ExpirePendingBatch(ctx, now, 10)
	if err != nil {
		t.Fatalf("ExpirePendingBatch 失败：%v", err)
	}
	if len(flipped) != 2 {
		t.Fatalf("期望翻 2 条，实际 %d：%+v", len(flipped), flipped)
	}
	for _, d := range flipped {
		if d.Status != model.OrderDraftStatusExpired {
			t.Errorf("返回行应已翻成 expired（供上层发事件），实际 %s", d.Status)
		}
	}
	// 返回行的 UpdatedAt 是内存里刷的，证明不了库里的值 —— 单独回读一次才算证据。
	flippedRow, err := repo.GetByID(ctx, "e-overdue-1")
	if err != nil || flippedRow == nil {
		t.Fatalf("回读失败：(%v,%v)", flippedRow, err)
	}
	if flippedRow.Status != model.OrderDraftStatusExpired || !flippedRow.UpdatedAt.Equal(now) {
		t.Errorf("过期翻转应落库（status+updated_at），实际 %+v", flippedRow)
	}

	// 二次调用应为 0 条：翻转必须真的落库，否则"无界增长"只是从内存搬到 DB。
	again, err := repo.ExpirePendingBatch(ctx, now, 10)
	if err != nil || len(again) != 0 {
		t.Fatalf("二次扫描期望 0 条，实际 %d（err=%v）", len(again), err)
	}
	for _, id := range []string{"e-fresh", "e-confirmed", "e-cancelled"} {
		got, err := repo.GetByID(ctx, id)
		if err != nil || got == nil {
			t.Fatalf("读取 %s 失败：(%v,%v)", id, got, err)
		}
		if got.Status == model.OrderDraftStatusExpired {
			t.Errorf("%s 不该被过期扫描改动，实际 %s", id, got.Status)
		}
	}
}

// 单轮上限的可控性：limit 必须真的限制一次改写的行数（长事务 = 工作台读请求被挡）。
func TestOrderDraftRepo_ExpirePendingBatchRespectsLimit(t *testing.T) {
	repo := NewOrderDraftRepositoryWithDB(setupOrderDraftTestDB(t))
	ctx := context.Background()
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		row := newDraftRow(fmt.Sprintf("lim-%d", i), fmt.Sprintf("c-%d", i), "产品", model.OrderDraftStatusPending)
		row.ExpiresAt = now.Add(-time.Hour)
		if err := repo.Upsert(ctx, row); err != nil {
			t.Fatalf("写入失败：%v", err)
		}
	}
	batch, err := repo.ExpirePendingBatch(ctx, now, 2)
	if err != nil {
		t.Fatalf("失败：%v", err)
	}
	if len(batch) != 2 {
		t.Fatalf("limit=2 期望翻 2 条，实际 %d", len(batch))
	}
	rest, err := repo.ExpirePendingBatch(ctx, now, 2)
	if err != nil || len(rest) != 2 {
		t.Fatalf("第二轮期望再翻 2 条，实际 %d（err=%v）", len(rest), err)
	}
	last, err := repo.ExpirePendingBatch(ctx, now, 2)
	if err != nil || len(last) != 1 {
		t.Fatalf("第三轮期望翻 1 条（收尾），实际 %d（err=%v）", len(last), err)
	}
	none, err := repo.ExpirePendingBatch(ctx, now, 2)
	if err != nil || len(none) != 0 {
		t.Fatalf("扫完应为 0 条，实际 %d（err=%v）", len(none), err)
	}
}

func TestOrderDraftRepo_MutatePendingCAS(t *testing.T) {
	repo := NewOrderDraftRepositoryWithDB(setupOrderDraftTestDB(t))
	ctx := context.Background()

	row := newDraftRow("m1", "cust-m", "产品", model.OrderDraftStatusPending)
	if err := repo.Upsert(ctx, row); err != nil {
		t.Fatalf("写入失败：%v", err)
	}

	applied, err := repo.MutatePending(ctx, "m1", func(m *model.OrderDraft) {
		m.Status = model.OrderDraftStatusConfirmed
		now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
		m.ConfirmedAt = &now
		m.OrderID = "ord-1"
	})
	if err != nil || !applied {
		t.Fatalf("首次 mutate 应生效，实际 (%v,%v)", applied, err)
	}
	got, err := repo.GetByID(ctx, "m1")
	if err != nil || got == nil {
		t.Fatalf("读取失败：(%v,%v)", got, err)
	}
	if got.Status != model.OrderDraftStatusConfirmed || got.OrderID != "ord-1" || got.ConfirmedAt == nil {
		t.Errorf("mutate 应整行落库，实际 %+v", got)
	}

	// 已非 pending ⇒ 第二次必须落败且不改写（两个销售同时点确认时的第二个人）。
	applied2, err := repo.MutatePending(ctx, "m1", func(m *model.OrderDraft) {
		m.Status = model.OrderDraftStatusCancelled
		m.OrderID = "被改坏了"
	})
	if err != nil {
		t.Fatalf("二次 mutate 报错：%v", err)
	}
	if applied2 {
		t.Error("非 pending 行 mutate 不应生效")
	}
	after, err := repo.GetByID(ctx, "m1")
	if err != nil || after == nil {
		t.Fatalf("读取失败：(%v,%v)", after, err)
	}
	if after.Status != model.OrderDraftStatusConfirmed || after.OrderID != "ord-1" {
		t.Errorf("落败的 mutate 不应改动任何列，实际 %+v", after)
	}

	// 不存在的行：applied=false 且不是错误（Confirm 据此报"草稿不存在"）。
	applied3, err := repo.MutatePending(ctx, "查无此稿", func(m *model.OrderDraft) { m.Status = model.OrderDraftStatusConfirmed })
	if err != nil || applied3 {
		t.Errorf("不存在期望 (false,nil)，实际 (%v,%v)", applied3, err)
	}

	// fn 为 nil 且不存在的行：applied=false 且不是错误
	applied4, err := repo.MutatePending(ctx, "不存在但传 nil", nil)
	if err != nil || applied4 {
		t.Errorf("空 fn + 不存在期望 (false,nil)，实际 (%v,%v)", applied4, err)
	}

	// 空 fn 打在仍 pending 的行上：加锁动作照常完成、写回原值，因此 applied=true。
	// 这条是 Edit 传空更新集时的路径，语义必须是"成功但什么都没变"，不能是"失败"。
	untouched := newDraftRow("m2", "cust-m2", "产品", model.OrderDraftStatusPending)
	if err := repo.Upsert(ctx, untouched); err != nil {
		t.Fatalf("写入失败：%v", err)
	}
	applied5, err := repo.MutatePending(ctx, "m2", nil)
	if err != nil || !applied5 {
		t.Fatalf("空 fn 命中 pending 行期望 (true,nil)，实际 (%v,%v)", applied5, err)
	}
	if got, e := repo.GetByID(ctx, "m2"); e != nil || got == nil || got.Status != model.OrderDraftStatusPending {
		t.Errorf("空 fn 不应改动状态，实际 (%v,%v)", got, e)
	}
}

func TestOrderDraftRepo_PurgeTerminal(t *testing.T) {
	repo := NewOrderDraftRepositoryWithDB(setupOrderDraftTestDB(t))
	ctx := context.Background()
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-90 * 24 * time.Hour)

	// old/terminal → 删；recent/terminal → 留；old/pending → 留；old/confirmed → 绝不留删
	// （confirmed 是成单证据链，sales_events.draft_id 指向它）
	seed := func(id, status string, updatedAt time.Time) {
		row := newDraftRow(id, "cust-"+id, "产品", status)
		row.CreatedAt = updatedAt.Add(-time.Hour)
		row.UpdatedAt = updatedAt
		row.ExpiresAt = updatedAt
		if err := repo.Upsert(ctx, row); err != nil {
			t.Fatalf("写入 %s 失败：%v", id, err)
		}
	}
	seed("purge-cancelled-old", model.OrderDraftStatusCancelled, cutoff.Add(-time.Hour))
	seed("purge-expired-old", model.OrderDraftStatusExpired, cutoff.Add(-time.Minute))
	seed("purge-expired-recent", model.OrderDraftStatusExpired, now)
	seed("purge-pending-old", model.OrderDraftStatusPending, cutoff.Add(-time.Hour))
	seed("purge-confirmed-old", model.OrderDraftStatusConfirmed, cutoff.Add(-24*time.Hour))

	deleted, err := repo.PurgeTerminal(ctx, cutoff, 100)
	if err != nil {
		t.Fatalf("PurgeTerminal 失败：%v", err)
	}
	if deleted != 2 {
		t.Errorf("期望删 2 行，实际 %d", deleted)
	}
	for id, wantGone := range map[string]bool{
		"purge-cancelled-old":  true,
		"purge-expired-old":    true,
		"purge-expired-recent": false,
		"purge-pending-old":    false,
		"purge-confirmed-old":  false,
	} {
		got, err := repo.GetByID(ctx, id)
		if err != nil {
			t.Fatalf("读取 %s 失败：%v", id, err)
		}
		if wantGone && got != nil {
			t.Errorf("%s 应被清理", id)
		}
		if !wantGone && got == nil {
			t.Errorf("%s 不该被清理（不在终态集合内）", id)
		}
	}

	// 再清一次应为 0（幂等，且证明"清理"确实落库而不是内存记账）
	again, err := repo.PurgeTerminal(ctx, cutoff, 100)
	if err != nil || again != 0 {
		t.Errorf("二次清理期望 0，实际 (%d,%v)", again, err)
	}
}

func TestOrderDraftRepo_PurgeTerminalRespectsLimit(t *testing.T) {
	repo := NewOrderDraftRepositoryWithDB(setupOrderDraftTestDB(t))
	ctx := context.Background()
	now := time.Now()
	before := now.Add(time.Hour)
	for i := 0; i < 5; i++ {
		row := newDraftRow(fmt.Sprintf("pl-%d", i), fmt.Sprintf("c-%d", i), "产品", model.OrderDraftStatusExpired)
		row.UpdatedAt = now.Add(-2 * time.Hour)
		if err := repo.Upsert(ctx, row); err != nil {
			t.Fatalf("写入失败：%v", err)
		}
	}
	n, err := repo.PurgeTerminal(ctx, before, 3)
	if err != nil || n != 3 {
		t.Fatalf("limit=3 期望删 3 行，实际 (%d,%v)", n, err)
	}
	n2, err := repo.PurgeTerminal(ctx, before, 3)
	if err != nil || n2 != 2 {
		t.Fatalf("第二轮期望删 2 行，实际 (%d,%v)", n2, err)
	}
	n3, err := repo.PurgeTerminal(ctx, before, 3)
	if err != nil || n3 != 0 {
		t.Fatalf("第三轮期望 0 行，实际 (%d,%v)", n3, err)
	}
}

func TestOrderDraftRepo_CountByStatus(t *testing.T) {
	repo := NewOrderDraftRepositoryWithDB(setupOrderDraftTestDB(t))
	ctx := context.Background()
	expect := map[string]int{
		model.OrderDraftStatusPending:   2,
		model.OrderDraftStatusConfirmed: 1,
		model.OrderDraftStatusExpired:   3,
	}
	i := 0
	for status, n := range expect {
		for k := 0; k < n; k++ {
			i++
			row := newDraftRow(fmt.Sprintf("cnt-%d", i), fmt.Sprintf("c-%d", i), "产品", status)
			if err := repo.Upsert(ctx, row); err != nil {
				t.Fatalf("写入失败：%v", err)
			}
		}
	}
	counts, err := repo.CountByStatus(ctx)
	if err != nil {
		t.Fatalf("CountByStatus 失败：%v", err)
	}
	if len(counts) != len(expect) {
		t.Errorf("期望 %d 个状态桶，实际 %+v", len(expect), counts)
	}
	for status, n := range expect {
		if counts[status] != int64(n) {
			t.Errorf("状态 %s 期望 %d，实际 %d", status, n, counts[status])
		}
	}
	// 未出现的状态应是 0 而非缺键导致误读（statusCounts 的消费方直接取 map）。
	if counts[model.OrderDraftStatusCancelled] != 0 {
		t.Errorf("cancelled 期望 0，实际 %d", counts[model.OrderDraftStatusCancelled])
	}
}
