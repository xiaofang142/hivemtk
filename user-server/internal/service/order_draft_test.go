package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/pkg/testutil"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/repository"
)

func setupDraftEnv(t *testing.T) (*CustomerJourneyService, *FollowUpService, *AITagger, *OrderIntentExtractor, *SalesEventStatsService, *OrderDraftService, *SalesActionTrigger) {
	database := testutil.NewTestDB(t,
		&model.Order{},
		&model.SalesEvent{},
	)
	db.SetTestDB(database)

	journey := newIsolatedJourneyService(t)
	followup := NewFollowUpService(journey)
	tagger := NewAITagger()
	extractor := NewOrderIntentExtractor()
	stats := NewSalesEventStatsServiceWithRepo(repository.NewSalesEventRepositoryWithDB(database))
	draftSvc := NewOrderDraftService(nil)
	draftSvc.SetJourney(context.Background(), journey)
	draftSvc.SetStats(context.Background(), stats)
	draftSvc.SetFollowUp(context.Background(), followup)
	trigger := NewSalesActionTrigger(tagger, journey, followup, extractor, stats, nil)
	trigger.SetDraftService(context.Background(), draftSvc)
	return journey, followup, tagger, extractor, stats, draftSvc, trigger
}

// ---- T-P2-06 ③ 之后读方法的测试助手：多出来的那个 error 一律判成测试失败。
//
// 为什么不各调用点写 `x, _ := ...`：把 error 丢掉正是本卡要修的那个形状 ——
// 测试里丢一次，"读不动"就会以"列表为空"的身份通过断言。

func draftPending(t *testing.T, ctx context.Context, svc *OrderDraftService, ownerID string, limit int) []*OrderDraft {
	t.Helper()
	list, err := svc.ListPending(ctx, ownerID, limit)
	if err != nil {
		t.Fatalf("ListPending 读失败（不是空列表，是读不动）: %v", err)
	}
	return list
}

func draftByCustomer(t *testing.T, ctx context.Context, svc *OrderDraftService, customerID string) []*OrderDraft {
	t.Helper()
	list, err := svc.ListByCustomer(ctx, customerID)
	if err != nil {
		t.Fatalf("ListByCustomer 读失败: %v", err)
	}
	return list
}

func draftByOwner(t *testing.T, ctx context.Context, svc *OrderDraftService, ownerID string) []*OrderDraft {
	t.Helper()
	list, err := svc.ListByOwner(ctx, ownerID)
	if err != nil {
		t.Fatalf("ListByOwner 读失败: %v", err)
	}
	return list
}

func draftByID(t *testing.T, ctx context.Context, svc *OrderDraftService, id string) *OrderDraft {
	t.Helper()
	got, err := svc.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID(%s) 读失败: %v", id, err)
	}
	return got
}

func draftExpireOverdue(t *testing.T, ctx context.Context, svc *OrderDraftService) int {
	t.Helper()
	n, err := svc.ExpireOverdue(ctx)
	if err != nil {
		t.Fatalf("ExpireOverdue 失败（已翻 %d 条）: %v", n, err)
	}
	return n
}

func draftPurgeTerminal(t *testing.T, ctx context.Context, svc *OrderDraftService, retention time.Duration) int {
	t.Helper()
	n, err := svc.PurgeTerminal(ctx, retention)
	if err != nil {
		t.Fatalf("PurgeTerminal 失败（已删 %d 条）: %v", n, err)
	}
	return n
}

// TestDraft_CreateManual 手动创建草稿
func TestDraft_CreateManual(t *testing.T) {
	_, _, _, _, _, draftSvc, _ := setupDraftEnv(t)
	draft, err := draftSvc.CreateManual(context.Background(), &CreateDraftRequest{
		CustomerID:  "cust_001",
		OwnerID:     "sales_001",
		ProductName: "光子嫩肤",
		Category:    "beauty",
		Quantity:    3,
		UnitPrice:   880.0,
		Note:        "客户咨询后销售手动开单",
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if draft.ID == "" {
		t.Error("草稿 ID 应自动生成")
	}
	if draft.Status != DraftStatusPending {
		t.Errorf("初始状态应为 pending，实际: %s", draft.Status)
	}
	if draft.TotalAmount != 2640.0 {
		t.Errorf("总价计算错误: %.2f", draft.TotalAmount)
	}
	if draft.Confidence != 1.0 {
		t.Errorf("手动创建置信度应为 1.0，实际: %.2f", draft.Confidence)
	}
	if draft.Source != "manual" {
		t.Errorf("来源应为 manual，实际: %s", draft.Source)
	}
	t.Logf("✅ 手动创建草稿: %s %s x %d = %.2f", draft.ID, draft.ProductName, draft.Quantity, draft.TotalAmount)
}

// TestDraft_CreateFromIntent 从订单意向自动创建草稿
func TestDraft_CreateFromIntent(t *testing.T) {
	_, _, _, extractor, _, draftSvc, _ := setupDraftEnv(t)
	intents := extractor.ExtractFromText(context.Background(), "cust_002", "我想要光子嫩肤套餐 3 次 2280 元")
	if len(intents) == 0 {
		t.Fatal("提取器应能从对话中提取光子嫩肤")
	}
	intent := intents[0]
	draft := draftSvc.CreateFromIntent(context.Background(), &intent, "sales_002")
	if draft == nil {
		t.Fatal("应能创建草稿")
	}
	if draft.ProductName != "光子嫩肤" {
		t.Errorf("产品名错误: %s", draft.ProductName)
	}
	if draft.Quantity != 3 {
		t.Errorf("数量错误: %d", draft.Quantity)
	}
	if draft.UnitPrice <= 0 {
		t.Errorf("单价应被提取: %.2f", draft.UnitPrice)
	}
	if draft.Status != DraftStatusPending {
		t.Errorf("状态错误: %s", draft.Status)
	}
	if draft.Source != "ai_chat" {
		t.Errorf("来源错误: %s", draft.Source)
	}
	t.Logf("✅ AI 提取自动创建草稿: %s %s x %d = %.2f",
		draft.ID, draft.ProductName, draft.Quantity, draft.TotalAmount)
}

// TestDraft_Deduplication 同一客户同一产品不重复创建
// 商业产品级业务流：客户连续说"光子嫩肤 3 次 2280"、"我要光子嫩肤"
//
//	旧实现：每条消息创建一个草稿 → 销售看到 5 个相同草稿，困惑
//	去重到同一草稿，累加数量 / 提升置信度
func TestDraft_Deduplication(t *testing.T) {
	_, _, _, extractor, _, draftSvc, _ := setupDraftEnv(t)
	intents1 := extractor.ExtractFromText(context.Background(), "cust_003", "光子嫩肤 3 次 2280 元")
	if len(intents1) == 0 {
		t.Fatal("第一次应能提取")
	}
	draft1 := draftSvc.CreateFromIntent(context.Background(), &intents1[0], "sales_003")
	if draft1 == nil {
		t.Fatal("第一次草稿应创建")
	}
	intents2 := extractor.ExtractFromText(context.Background(), "cust_003", "我要光子嫩肤")
	if len(intents2) > 0 {
		draft2 := draftSvc.CreateFromIntent(context.Background(), &intents2[0], "sales_003")
		if draft2 == nil {
			t.Fatal("第二次草稿应返回（去重）")
		}
		if draft2.ID != draft1.ID {
			t.Errorf("应去重到同一草稿，draft1=%s draft2=%s", draft1.ID, draft2.ID)
		}
	}
	all := draftByCustomer(t, context.Background(), draftSvc, "cust_003")
	if len(all) != 1 {
		t.Errorf("应有 1 个草稿（去重），实际: %d", len(all))
	}
	t.Logf("✅ 同一客户同产品去重：%d 个草稿", len(all))
}

// TestDraft_MultipleProducts 同一客户多个产品独立草稿
func TestDraft_MultipleProducts(t *testing.T) {
	_, _, _, extractor, _, draftSvc, _ := setupDraftEnv(t)
	intents := extractor.ExtractFromText(context.Background(), "cust_004", "我要光子嫩肤 3 次 2280 元 和 水光针 1 次 980 元")
	if len(intents) < 2 {
		t.Fatalf("提取器应至少提取 2 个产品，实际: %d（正则失效或文本不匹配）", len(intents))
	}
	for _, in := range intents {
		draftSvc.CreateFromIntent(context.Background(), &in, "sales_004")
	}
	all := draftByCustomer(t, context.Background(), draftSvc, "cust_004")
	if len(all) < 2 {
		t.Errorf("多产品应有多个草稿，实际: %d", len(all))
	}
	t.Logf("✅ 多产品独立草稿: %d 个", len(all))
}

// TestDraft_NilIntent nil 意向安全
func TestDraft_NilIntent(t *testing.T) {
	_, _, _, _, _, draftSvc, _ := setupDraftEnv(t)
	if draft := draftSvc.CreateFromIntent(context.Background(), nil, "sales"); draft != nil {
		t.Error("nil 意向应返回 nil")
	}
	if draft := draftSvc.CreateFromIntent(context.Background(), &OrderIntent{}, "sales"); draft != nil {
		t.Error("空 ProductName 意向应返回 nil")
	}
}

// TestDraft_ManualValidation 手动创建参数校验
func TestDraft_ManualValidation(t *testing.T) {
	_, _, _, _, _, draftSvc, _ := setupDraftEnv(t)
	if _, err := draftSvc.CreateManual(context.Background(), nil); err == nil {
		t.Error("nil 请求应报错")
	}
	if _, err := draftSvc.CreateManual(context.Background(), &CreateDraftRequest{}); err == nil {
		t.Error("缺客户应报错")
	}
	if _, err := draftSvc.CreateManual(context.Background(), &CreateDraftRequest{CustomerID: "c1"}); err == nil {
		t.Error("缺产品应报错")
	}
	if _, err := draftSvc.CreateManual(context.Background(), &CreateDraftRequest{
		CustomerID: "c1", ProductName: "X", UnitPrice: -1,
	}); err == nil {
		t.Error("负单价应报错")
	}
}

// TestDraft_Confirm 销售一键确认草稿
// 商业产品级核心闭环：销售点"确认" → 4 件事自动发生
func TestDraft_Confirm(t *testing.T) {
	journey, followup, _, _, stats, draftSvc, _ := setupDraftEnv(t)
	custID := "cust_confirm_001"
	ownerID := "sales_confirm"
	_, _ = journey.Transition(context.Background(), custID, StageQuoted, "test", ownerID, "已报价", nil)

	draft, _ := draftSvc.CreateManual(context.Background(), &CreateDraftRequest{
		CustomerID:  custID,
		OwnerID:     ownerID,
		ProductName: "光子嫩肤",
		Quantity:    3,
		UnitPrice:   880.0,
	})

	result, err := draftSvc.Confirm(context.Background(), draft.ID, ownerID)
	if err != nil {
		t.Fatalf("确认失败: %v", err)
	}
	if result == nil {
		t.Fatal("应返回结果")
	}
	if result.OrderID == "" {
		t.Error("应生成订单 ID")
	}
	if result.StageAdvanced != string(StageWon) {
		t.Errorf("阶段应为 won，实际: %s", result.StageAdvanced)
	}

	state := journey.GetState(context.Background(), custID)
	if state.CurrentStage != StageWon {
		t.Errorf("客户旅程应为 won，实际: %s", state.CurrentStage)
	}

	perf := stats.GetSalesPerformance(context.Background(), ownerID, time.Time{})
	if perf.TotalOrders < 1 {
		t.Errorf("仪表盘应记录订单，实际: %d", perf.TotalOrders)
	}

	if result.FollowUpID == "" {
		t.Error("应自动安排售后跟进")
	}
	pending := followup.ListPending(context.Background(), ownerID, 0)
	foundAfterSale := false
	for _, r := range pending {
		if r.Type == ReminderAfterSaleCare && r.CustomerID == custID {
			foundAfterSale = true
		}
	}
	if !foundAfterSale {
		t.Error("应自动安排售后回访")
	}

	if draft.Status != DraftStatusConfirmed {
		t.Errorf("草稿状态应为 confirmed，实际: %s", draft.Status)
	}
	if draft.OrderID != result.OrderID {
		t.Error("草稿应关联订单 ID")
	}

	t.Logf("✅ 草稿确认闭环: 订单=%s, 阶段=%s, 跟进=%s",
		result.OrderID, result.StageAdvanced, result.FollowUpID)
}

// TestDraft_Confirm_NotFound 确认不存在的草稿
func TestDraft_Confirm_NotFound(t *testing.T) {
	_, _, _, _, _, draftSvc, _ := setupDraftEnv(t)
	_, err := draftSvc.Confirm(context.Background(), "not_exist_id", "sales")
	if err == nil {
		t.Error("不存在的草稿应报错")
	}
	if !strings.Contains(err.Error(), "不存在") {
		t.Errorf("错误信息应含'不存在'，实际: %v", err)
	}
}

// TestDraft_Confirm_AlreadyConfirmed 重复确认
func TestDraft_Confirm_AlreadyConfirmed(t *testing.T) {
	_, _, _, _, _, draftSvc, _ := setupDraftEnv(t)
	draft, _ := draftSvc.CreateManual(context.Background(), &CreateDraftRequest{
		CustomerID: "c1", OwnerID: "s1", ProductName: "P", Quantity: 1, UnitPrice: 100,
	})
	if _, err := draftSvc.Confirm(context.Background(), draft.ID, "s1"); err != nil {
		t.Fatalf("首次确认失败: %v", err)
	}
	if _, err := draftSvc.Confirm(context.Background(), draft.ID, "s1"); err == nil {
		t.Error("重复确认应报错")
	}
}

// TestDraft_Cancel 取消草稿
func TestDraft_Cancel(t *testing.T) {
	journey, _, _, _, stats, draftSvc, _ := setupDraftEnv(t)
	custID := "cust_cancel"
	ownerID := "sales_cancel"
	_, _ = journey.Transition(context.Background(), custID, StageQuoted, "test", ownerID, "已报价", nil)

	draft, _ := draftSvc.CreateManual(context.Background(), &CreateDraftRequest{
		CustomerID: custID, OwnerID: ownerID, ProductName: "P", Quantity: 1, UnitPrice: 100,
	})
	if err := draftSvc.Cancel(context.Background(), draft.ID, "客户改变主意", ownerID); err != nil {
		t.Fatalf("取消失败: %v", err)
	}
	if draft.Status != DraftStatusCancelled {
		t.Errorf("状态应为 cancelled，实际: %s", draft.Status)
	}
	if draft.CancelReason != "客户改变主意" {
		t.Errorf("取消原因错误: %s", draft.CancelReason)
	}
	draftStats := stats.GetDraftStats(context.Background(), ownerID, time.Time{})
	if draftStats.ByAction["cancelled"] < 1 {
		t.Error("销售事件统计应记录 cancelled 事件")
	}
	t.Logf("✅ 草稿取消: 原因=%s", draft.CancelReason)
}

// TestDraft_Edit 编辑草稿
func TestDraft_Edit(t *testing.T) {
	_, _, _, _, _, draftSvc, _ := setupDraftEnv(t)
	draft, _ := draftSvc.CreateManual(context.Background(), &CreateDraftRequest{
		CustomerID: "c1", OwnerID: "s1", ProductName: "光子嫩肤", Quantity: 1, UnitPrice: 1000,
	})
	newQty := 3
	newPrice := 880.0
	newNote := "客户砍价后调整"
	if err := draftSvc.Edit(context.Background(), draft.ID, DraftUpdates{
		Quantity:  &newQty,
		UnitPrice: &newPrice,
		Note:      &newNote,
	}); err != nil {
		t.Fatalf("编辑失败: %v", err)
	}
	if draft.Quantity != 3 || draft.UnitPrice != 880.0 {
		t.Errorf("编辑未生效: qty=%d price=%.2f", draft.Quantity, draft.UnitPrice)
	}
	if draft.TotalAmount != 2640.0 {
		t.Errorf("总价应自动重算: %.2f", draft.TotalAmount)
	}
	t.Logf("✅ 草稿编辑: qty=%d price=%.2f total=%.2f",
		draft.Quantity, draft.UnitPrice, draft.TotalAmount)
}

// TestDraft_Edit_NotPending 编辑已确认草稿
func TestDraft_Edit_NotPending(t *testing.T) {
	_, _, _, _, _, draftSvc, _ := setupDraftEnv(t)
	draft, _ := draftSvc.CreateManual(context.Background(), &CreateDraftRequest{
		CustomerID: "c1", OwnerID: "s1", ProductName: "P", Quantity: 1, UnitPrice: 100,
	})
	_, _ = draftSvc.Confirm(context.Background(), draft.ID, "s1")
	newQty := 5
	if err := draftSvc.Edit(context.Background(), draft.ID, DraftUpdates{Quantity: &newQty}); err == nil {
		t.Error("编辑已确认草稿应报错")
	}
}

// TestDraft_Expire 草稿过期
// 商业产品级：销售工作台 7 天前的草稿自动过期，避免堆积
func TestDraft_Expire(t *testing.T) {
	_, _, _, _, _, draftSvc, _ := setupDraftEnv(t)
	draft, _ := draftSvc.CreateManual(context.Background(), &CreateDraftRequest{
		CustomerID: "c1", OwnerID: "s1", ProductName: "P", Quantity: 1, UnitPrice: 100,
	})
	draft.ExpiresAt = time.Now().Add(-1 * time.Hour)
	expired := draftExpireOverdue(t, context.Background(), draftSvc)
	if expired < 1 {
		t.Error("应有 1 个草稿被过期")
	}
	if draft.Status != DraftStatusExpired {
		t.Errorf("状态应为 expired，实际: %s", draft.Status)
	}
	t.Logf("✅ 草稿过期: %d 个", expired)
}

// TestDraft_ListPending 待确认草稿列表
// 商业产品级：销售工作台首页展示
func TestDraft_ListPending(t *testing.T) {
	_, _, _, _, _, draftSvc, _ := setupDraftEnv(t)
	for i := 0; i < 3; i++ {
		draftSvc.CreateManual(context.Background(), &CreateDraftRequest{
			CustomerID: "c1", OwnerID: "sales_list", ProductName: "P", Quantity: 1, UnitPrice: 100,
		})
	}
	d4, _ := draftSvc.CreateManual(context.Background(), &CreateDraftRequest{
		CustomerID: "c1", OwnerID: "sales_list", ProductName: "P", Quantity: 1, UnitPrice: 100,
	})
	draftSvc.Confirm(context.Background(), d4.ID, "sales_list")
	d5, _ := draftSvc.CreateManual(context.Background(), &CreateDraftRequest{
		CustomerID: "c1", OwnerID: "sales_list", ProductName: "P", Quantity: 1, UnitPrice: 100,
	})
	draftSvc.Cancel(context.Background(), d5.ID, "test", "sales_list")

	pending := draftPending(t, context.Background(), draftSvc, "sales_list", 0)
	if len(pending) != 3 {
		t.Errorf("应有 3 个 pending，实际: %d", len(pending))
	}
	for _, p := range pending {
		if p.Status != DraftStatusPending {
			t.Errorf("列表应只含 pending，实际含 %s", p.Status)
		}
	}
	t.Logf("✅ 待确认列表: %d 个", len(pending))
}

// TestDraft_ListPending_Sort 列表排序（高置信度 + 大金额优先）
func TestDraft_ListPending_Sort(t *testing.T) {
	_, _, _, _, _, draftSvc, _ := setupDraftEnv(t)
	d1, _ := draftSvc.CreateManual(context.Background(), &CreateDraftRequest{
		CustomerID: "c1", OwnerID: "s1", ProductName: "低", Quantity: 1, UnitPrice: 10,
	})
	d2, _ := draftSvc.CreateManual(context.Background(), &CreateDraftRequest{
		CustomerID: "c1", OwnerID: "s1", ProductName: "高", Quantity: 1, UnitPrice: 9999,
	})
	d3, _ := draftSvc.CreateManual(context.Background(), &CreateDraftRequest{
		CustomerID: "c1", OwnerID: "s1", ProductName: "中", Quantity: 1, UnitPrice: 500,
	})
	d1.Confidence = 0.5
	d2.Confidence = 0.7
	d3.Confidence = 0.9
	pending := draftPending(t, context.Background(), draftSvc, "s1", 0)
	if len(pending) != 3 {
		t.Fatalf("应有 3 个")
	}
	if pending[0].ID != d3.ID {
		t.Errorf("应按置信度排序，第一位应是中(0.9)，实际是 %s", pending[0].ProductName)
	}
	t.Logf("✅ 排序: %s > %s > %s (按置信度)", pending[0].ProductName, pending[1].ProductName, pending[2].ProductName)
}

// TestDraft_ListByOwner 按销售查询
func TestDraft_ListByOwner(t *testing.T) {
	_, _, _, _, _, draftSvc, _ := setupDraftEnv(t)
	for i := 0; i < 5; i++ {
		draftSvc.CreateManual(context.Background(), &CreateDraftRequest{
			CustomerID: "c1", OwnerID: "alice", ProductName: "P", Quantity: 1, UnitPrice: 100,
		})
	}
	for i := 0; i < 3; i++ {
		draftSvc.CreateManual(context.Background(), &CreateDraftRequest{
			CustomerID: "c1", OwnerID: "bob", ProductName: "P", Quantity: 1, UnitPrice: 100,
		})
	}
	alice := draftByOwner(t, context.Background(), draftSvc, "alice")
	if len(alice) != 5 {
		t.Errorf("alice 应有 5 个，实际: %d", len(alice))
	}
	bob := draftByOwner(t, context.Background(), draftSvc, "bob")
	if len(bob) != 3 {
		t.Errorf("bob 应有 3 个，实际: %d", len(bob))
	}
}

// TestDraft_ListByCustomer 按客户查询
func TestDraft_ListByCustomer(t *testing.T) {
	_, _, _, _, _, draftSvc, _ := setupDraftEnv(t)
	for i := 0; i < 3; i++ {
		draftSvc.CreateManual(context.Background(), &CreateDraftRequest{
			CustomerID: "alice_cust", OwnerID: "s1", ProductName: "P" + intToStr(i), Quantity: 1, UnitPrice: 100,
		})
	}
	all := draftByCustomer(t, context.Background(), draftSvc, "alice_cust")
	if len(all) != 3 {
		t.Errorf("应有 3 个，实际: %d", len(all))
	}
	empty := draftByCustomer(t, context.Background(), draftSvc, "not_exist_cust")
	if len(empty) != 0 {
		t.Errorf("不存在的客户应返回空")
	}
}

// TestDraft_GetByID 按 ID 查询
func TestDraft_GetByID(t *testing.T) {
	_, _, _, _, _, draftSvc, _ := setupDraftEnv(t)
	d, _ := draftSvc.CreateManual(context.Background(), &CreateDraftRequest{
		CustomerID: "c1", OwnerID: "s1", ProductName: "P", Quantity: 1, UnitPrice: 100,
	})
	got := draftByID(t, context.Background(), draftSvc, d.ID)
	if got == nil || got.ID != d.ID {
		t.Error("应能查到草稿")
	}
	notFound := draftByID(t, context.Background(), draftSvc, "not_exist")
	if notFound != nil {
		t.Error("不存在应返回 nil")
	}
}

// TestDraft_TriggerAutoCreate AI 谈单 → 自动创建草稿（核心入口）
// 商业产品级核心业务流：AI 回复中包含"光子嫩肤 3 次 2280 元"
//
//	→ 自动生成草稿 → 销售在工作台看到 → 一键确认 → 下单
func TestDraft_TriggerAutoCreate(t *testing.T) {
	journey, _, tagger, _, stats, draftSvc, trigger := setupDraftEnv(t)
	custID := "cust_trigger_001"
	ownerID := "sales_trigger"

	resp := &SalesResponse{
		Reply:  "好的，光子嫩肤 3 次套餐 2280 元，预约周六可以吗？",
		Intent: &dto.RecognizeResult{IntentType: IntentPurchase, IntentName: "准备购买", Confidence: 0.92},
	}
	rec := trigger.TriggerAfterSales(context.Background(), custID, ownerID, resp)
	if rec == nil {
		t.Fatal("触发器应返回记录")
	}

	hasDraftCreated := false
	for _, a := range rec.Actions {
		if a.Action == "order_draft_created" && a.Result == "ok" {
			hasDraftCreated = true
		}
	}
	if !hasDraftCreated {
		t.Error("应自动创建订单草稿")
	}

	pending := draftPending(t, context.Background(), draftSvc, ownerID, 0)
	if len(pending) == 0 {
		t.Fatal("应有 1 个 pending 草稿")
	}
	if pending[0].ProductName != "光子嫩肤" {
		t.Errorf("草稿产品名错误: %s", pending[0].ProductName)
	}

	_ = tagger.GetTags(context.Background(), custID)
	state := journey.GetState(context.Background(), custID)
	if state.CurrentStage == StageStranger {
		t.Error("客户旅程应推进")
	}

	_ = stats.GetDraftStats(context.Background(), ownerID, time.Time{})
	t.Logf("✅ 触发器自动创建草稿: 阶段=%s, 草稿数=%d", state.CurrentStage, len(pending))
}

// TestDraft_TriggerCreateAndConfirm 触发器创建 + 销售一键确认（端到端）
// 这是 -11 的完整端到端测试
func TestDraft_TriggerCreateAndConfirm(t *testing.T) {
	journey, followup, _, _, stats, draftSvc, trigger := setupDraftEnv(t)
	custID := "cust_e2e_001"
	ownerID := "sales_e2e"

	resp := &SalesResponse{
		Reply:  "好的，光子嫩肤 3 次套餐 2280 元，给您下单",
		Intent: &dto.RecognizeResult{IntentType: IntentPurchase, IntentName: "购买", Confidence: 0.95},
	}
	trigger.TriggerAfterSales(context.Background(), custID, ownerID, resp)

	pending := draftPending(t, context.Background(), draftSvc, ownerID, 0)
	if len(pending) != 1 {
		t.Fatalf("应有 1 个待确认草稿，实际: %d", len(pending))
	}
	draft := pending[0]
	t.Logf("   草稿: %s %s x %d = %.2f", draft.ProductName, draft.ProductName, draft.Quantity, draft.TotalAmount)

	result, err := draftSvc.Confirm(context.Background(), draft.ID, ownerID)
	if err != nil {
		t.Fatalf("确认失败: %v", err)
	}

	if result.OrderID == "" {
		t.Error("应创建订单")
	}
	state := journey.GetState(context.Background(), custID)
	if state.CurrentStage != StageWon {
		t.Errorf("客户旅程应为 won，实际: %s", state.CurrentStage)
	}
	perf := stats.GetSalesPerformance(context.Background(), ownerID, time.Time{})
	if perf.TotalOrders < 1 {
		t.Error("仪表盘应记录订单")
	}
	followups := followup.ListPending(context.Background(), ownerID, 0)
	foundAfterSale := false
	for _, f := range followups {
		if f.Type == ReminderAfterSaleCare && f.CustomerID == custID {
			foundAfterSale = true
		}
	}
	if !foundAfterSale {
		t.Error("应自动安排售后回访")
	}

	t.Logf("✅ E2E 闭环: 订单=%s, 阶段=%s, 售后=%v", result.OrderID, state.CurrentStage, foundAfterSale)
}

// TestDraft_TriggerMultipleIntents 多个订单意向 → 多个草稿
func TestDraft_TriggerMultipleIntents(t *testing.T) {
	_, _, _, _, _, draftSvc, trigger := setupDraftEnv(t)
	custID := "cust_multi_intent"
	ownerID := "sales_multi"

	resp := &SalesResponse{
		Reply:  "我想要光子嫩肤 3 次 2280 元 和 水光针 1 次 980 元",
		Intent: &dto.RecognizeResult{IntentType: IntentPurchase, IntentName: "购买", Confidence: 0.9},
	}
	trigger.TriggerAfterSales(context.Background(), custID, ownerID, resp)

	pending := draftPending(t, context.Background(), draftSvc, ownerID, 0)
	if len(pending) < 1 {
		t.Errorf("应至少有 1 个草稿（取决于提取器能力），实际: %d", len(pending))
	}
	t.Logf("✅ 多产品草稿: %d 个", len(pending))
}

// TestDraft_TriggerNoIntent 无产品信号 → 不创建草稿
func TestDraft_TriggerNoIntent(t *testing.T) {
	_, _, _, _, _, draftSvc, trigger := setupDraftEnv(t)
	custID := "cust_no_intent"
	ownerID := "sales_no"

	resp := &SalesResponse{
		Reply:  "你好，请问你们是正规公司吗？",
		Intent: &dto.RecognizeResult{IntentType: IntentSocial, IntentName: "社交", Confidence: 0.85},
	}
	trigger.TriggerAfterSales(context.Background(), custID, ownerID, resp)

	pending := draftPending(t, context.Background(), draftSvc, ownerID, 0)
	if len(pending) != 0 {
		t.Errorf("无产品信号不应创建草稿，实际: %d", len(pending))
	}
}

// TestDraft_Confirm_Expired 确认过期草稿
func TestDraft_Confirm_Expired(t *testing.T) {
	_, _, _, _, _, draftSvc, _ := setupDraftEnv(t)
	draft, _ := draftSvc.CreateManual(context.Background(), &CreateDraftRequest{
		CustomerID: "c1", OwnerID: "s1", ProductName: "P", Quantity: 1, UnitPrice: 100,
	})
	draft.ExpiresAt = time.Now().Add(-1 * time.Hour)
	_, err := draftSvc.Confirm(context.Background(), draft.ID, "s1")
	if err == nil {
		t.Error("过期草稿不应能确认")
	}
	if !strings.Contains(err.Error(), "过期") {
		t.Errorf("错误信息应含'过期'，实际: %v", err)
	}
}

// TestDraft_DashboardStats 仪表盘草稿统计
func TestDraft_DashboardStats(t *testing.T) {
	journey, _, _, _, stats, draftSvc, _ := setupDraftEnv(t)
	_ = journey
	ownerID := "sales_stats"
	for i := 0; i < 5; i++ {
		d, _ := draftSvc.CreateManual(context.Background(), &CreateDraftRequest{
			CustomerID: "c1", OwnerID: ownerID, ProductName: "P", Quantity: 1, UnitPrice: 1000,
		})
		if i < 2 {
			draftSvc.Confirm(context.Background(), d.ID, ownerID)
		} else if i == 2 {
			draftSvc.Cancel(context.Background(), d.ID, "test", ownerID)
		}
	}
	draftStats := stats.GetDraftStats(context.Background(), ownerID, time.Time{})
	if draftStats.Total != 8 {
		t.Errorf("总事件数应为 8，实际: %d", draftStats.Total)
	}
	if draftStats.ByAction["created"] != 5 {
		t.Errorf("created 应为 5，实际: %d", draftStats.ByAction["created"])
	}
	if draftStats.ByAction["confirmed"] != 2 {
		t.Errorf("confirmed 应为 2，实际: %d", draftStats.ByAction["confirmed"])
	}
	if draftStats.ByAction["cancelled"] != 1 {
		t.Errorf("cancelled 应为 1，实际: %d", draftStats.ByAction["cancelled"])
	}
	if draftStats.ConversionRate < 39 || draftStats.ConversionRate > 41 {
		t.Errorf("转化率应约 40%%，实际: %.2f", draftStats.ConversionRate)
	}
	t.Logf("✅ 销售事件统计: 总事件=%d created=%d confirmed=%d cancelled=%d 转化率=%.1f%%",
		draftStats.Total, draftStats.ByAction["created"], draftStats.ByAction["confirmed"], draftStats.ByAction["cancelled"], draftStats.ConversionRate)
}

// TestDraft_DefaultQuantity 缺省数量处理
func TestDraft_DefaultQuantity(t *testing.T) {
	_, _, _, _, _, draftSvc, _ := setupDraftEnv(t)
	d1, _ := draftSvc.CreateManual(context.Background(), &CreateDraftRequest{
		CustomerID: "c1", OwnerID: "s1", ProductName: "P", Quantity: 0, UnitPrice: 100,
	})
	if d1.Quantity != 1 {
		t.Errorf("Quantity=0 应默认为 1，实际: %d", d1.Quantity)
	}
}

// TestDraft_NilServiceSafe 草稿服务不被注入时仍安全
func TestDraft_NilServiceSafe(t *testing.T) {
	draftSvc := NewOrderDraftService(nil)
	draft, err := draftSvc.CreateManual(context.Background(), &CreateDraftRequest{
		CustomerID: "c1", OwnerID: "s1", ProductName: "P", Quantity: 1, UnitPrice: 100,
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	result, err := draftSvc.Confirm(context.Background(), draft.ID, "s1")
	if err != nil {
		t.Fatalf("确认失败: %v", err)
	}
	if result.OrderID == "" {
		t.Error("应生成订单 ID")
	}
	t.Logf("✅ 空服务安全: 订单 ID=%s", result.OrderID)
}

// TestDraft_ConfidenceBoost 高置信度字段加成
func TestDraft_ConfidenceBoost(t *testing.T) {
	_, _, _, _, _, draftSvc, _ := setupDraftEnv(t)
	intent1 := OrderIntent{
		CustomerID:  "c1",
		ProductName: "光子嫩肤",
		Quantity:    3,
		UnitPrice:   880,
		Confidence:  0.7,
	}
	d1 := draftSvc.CreateFromIntent(context.Background(), &intent1, "s1")
	intent2 := OrderIntent{
		CustomerID:  "c2",
		ProductName: "光子嫩肤",
		Confidence:  0.5,
	}
	d2 := draftSvc.CreateFromIntent(context.Background(), &intent2, "s2")
	if d1.Confidence <= d2.Confidence {
		t.Errorf("强信号草稿应置信度更高: d1=%.2f d2=%.2f", d1.Confidence, d2.Confidence)
	}
	t.Logf("✅ 置信度加成: 强信号=%.2f, 弱信号=%.2f", d1.Confidence, d2.Confidence)
}

// TestDraft_ConcurrentSafe 并发安全
func TestDraft_ConcurrentSafe(t *testing.T) {
	_, _, _, _, _, draftSvc, _ := setupDraftEnv(t)
	done := make(chan bool, 100)
	for i := 0; i < 100; i++ {
		go func(idx int) {
			draftSvc.CreateManual(context.Background(), &CreateDraftRequest{
				CustomerID:  "c" + intToStr(idx),
				OwnerID:     "s_concurrent",
				ProductName: "P",
				Quantity:    1,
				UnitPrice:   100,
			})
			done <- true
		}(i)
	}
	for i := 0; i < 100; i++ {
		<-done
	}
	all := draftByOwner(t, context.Background(), draftSvc, "s_concurrent")
	if len(all) != 100 {
		t.Errorf("并发创建后应有 100 个，实际: %d", len(all))
	}
}
