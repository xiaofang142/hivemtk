// payment_collection_test.go T-P7-04：回款完成 → 赢单 → 复购跟进。
//
// 本层要证的七件事（AC①②③逐条有判据，摘掉任何一件不会让别件变红）：
//
//	AC① 唯一入口 = paid 全额触发走的是 MarkWonByCollection 那条路：商机行落 won，
//	     且赢率保持收口前的值（0.42 字面量 —— 重算了就会变，判据打在这一格）。
//	AC② partial 不触发 = 部分回款的 receipt.Collection 为空串，商机还在 open，
//	     旅程没动、跟进零条。
//	AC③ SOP 幂等 = 三个"第二次"都不重排跟进：已结账单的重放（Transited 为假）、
//	     商机已 won 时的触发（wonBefore 门）、冲销后重付（同一扇门）。
//	失败隔离 = 钩子报错时支付照样成功，结论记 "error: …"，商机行不动。
//	无关联 = 账单没挂商机时结论记 "skipped"，钱照记单照结。
//	没装钩子 = 结论记 "no-hook"，支付不受影响。
//	直达 = CompleteCollection 对不存在的行报 ErrOpportunityNotFound。
//
// 替身口径与 payment_test.go 一致：存储层全真库（bills + payments + opportunities），
// 钩子只在"让它失败"这一格用一个返回固定错误的函数；旅程缓存用 MemoryCache
// （customer_journey_test.go 同一口径，不碰全局缓存）。
package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// —— 夹具 ——————————————————————————————————————————————————————————————

// collectionDB 建回款腿 + 商机两张表：钩子要同时写这两边，少建一张就是遮住一次回查。
func collectionDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t, &model.Bill{}, &model.Payment{}, &model.Opportunity{})
}

// collectionStack 装一台"回款 + 商机 + 旅程 + 跟进"全接上的栈，钩子是默认组装。
// 旅程缓存是测试自建的 MemoryCache（t.Cleanup 关），结论打在三处落库/内存态上。
func collectionStack(t *testing.T, db *gorm.DB) (*PaymentService, *OpportunityService, *CustomerJourneyService, *FollowUpService) {
	t.Helper()
	pay := paySvc(t, db)
	opps := NewOpportunityService(repository.NewOpportunityRepositoryWithDB(db))
	opps.SetClock(func() time.Time { return oppClockBase })
	l2 := cache.NewMemoryCache()
	t.Cleanup(func() { l2.Close() })
	journey := NewCustomerJourneyServiceWithCache(l2)
	followup := NewFollowUpService(journey)
	pay.SetCollectionCompletedHook(WireCollectionToRepurchase(opps, journey, followup))
	return pay, opps, journey, followup
}

// collectionOpp 落一行在跑的商机（金额 1200.00、赢率 0.42 都是手写的字面量）。
func collectionOpp(t *testing.T, db *gorm.DB, id string) *model.Opportunity {
	t.Helper()
	repo := repository.NewOpportunityRepositoryWithDB(db)
	return seedOpportunity(t, repo, id, model.OpportunityStageProposal, model.OpportunityStatusOpen)
}

// collectionReminders 数派给某 owner 的待办跟进（SOP 排没排判据打在这里）。
func collectionReminders(followup *FollowUpService, owner string) []*Reminder {
	return followup.ListPending(context.Background(), owner, 100)
}

// —— AC① 全额触发：赢单 + 旅程 + 跟进 ———————————————————————————————————

// TestCollectionHookFiresOnPaid 全额回款把账单结掉 ⇒ 商机 won、旅程进复购期、
// 跟进排一条 repurchase_reminder，且赢率保持收口前的 0.42（没重算）。
func TestCollectionHookFiresOnPaid(t *testing.T) {
	db := collectionDB(t)
	pay, _, journey, followup := collectionStack(t, db)
	payBill(t, db, "bill-paid", "q-paid", "row-paid", 400.00)
	collectionOpp(t, db, "opp-q-paid")

	rc, err := pay.RecordPayment(context.Background(), payIn("bill-paid", "ref-paid-1", 400.00))
	if err != nil {
		t.Fatalf("入账失败: %v", err)
	}
	if rc.Settlement.Status != model.BillStatusPaid {
		t.Fatalf("账单状态 = %s，要 paid（否则钩子触发条件不成立，用例白跑）", rc.Settlement.Status)
	}
	if rc.Collection != "ok" {
		t.Errorf(`receipt.Collection = %q，要 "ok"`, rc.Collection)
	}

	opp := readOpportunityRow(t, db, "opp-q-paid")
	if opp.Status != model.OpportunityStatusWon {
		t.Errorf("商机状态 = %s，要 won（唯一入口没走到）", opp.Status)
	}
	if opp.WinProbability != 0.42 {
		t.Errorf("赢率 = %v，要收口前的 0.42（终态不许重算）", opp.WinProbability)
	}

	st := journey.GetState(context.Background(), "cus_opp-q-paid")
	if st == nil || st.CurrentStage != StageRepurchase {
		t.Errorf("旅程阶段 = %v，要复购期", st)
	}

	got := collectionReminders(followup, "sales_a")
	if len(got) != 1 {
		t.Fatalf("跟进条数 = %d，要 1（多排是幂等破，零排是钩子没走到）", len(got))
	}
	if got[0].SOPName != "repurchase_reminder" {
		t.Errorf("跟进 SOPName = %q，要 repurchase_reminder（标签与种子模板对不上）", got[0].SOPName)
	}
}

// —— AC② 部分回款不触发 ——————————————————————————————————————————————

// TestCollectionHookSkipsPartial 部分回款 ⇒ 钩子没触发（空串），商机还在跑，
// 旅程与跟进零变化。
func TestCollectionHookSkipsPartial(t *testing.T) {
	db := collectionDB(t)
	pay, _, journey, followup := collectionStack(t, db)
	payBill(t, db, "bill-part", "q-part", "row-part", 400.00)
	collectionOpp(t, db, "opp-q-part")

	rc, err := pay.RecordPayment(context.Background(), payIn("bill-part", "ref-part-1", 150.00))
	if err != nil {
		t.Fatalf("入账失败: %v", err)
	}
	if rc.Settlement.Status != model.BillStatusPartial {
		t.Fatalf("账单状态 = %s，要 partial（否则 AC② 的前提不成立）", rc.Settlement.Status)
	}
	if rc.Collection != "" {
		t.Errorf(`receipt.Collection = %q，要空串（partial 不许触发赢单）`, rc.Collection)
	}
	if opp := readOpportunityRow(t, db, "opp-q-part"); opp.Status != model.OpportunityStatusOpen {
		t.Errorf("商机状态 = %s，要 open（partial 把单赢了就是提前结账）", opp.Status)
	}
	if st := journey.GetState(context.Background(), "cus_opp-q-part"); st != nil && st.CurrentStage == StageRepurchase {
		t.Errorf("旅程进了复购期：partial 不许切旅程")
	}
	if got := collectionReminders(followup, "sales_a"); len(got) != 0 {
		t.Errorf("跟进条数 = %d，要 0（partial 排了跟进就是骚扰）", len(got))
	}
}

// —— AC③ 三个"第二次"都不重排 —————————————————————————————————————————

// TestCollectionHookReplayDoesNotRefire 已结掉账单的重放 ⇒ Transited 为假，
// 钩子不触发，跟进还是那一条。
func TestCollectionHookReplayDoesNotRefire(t *testing.T) {
	db := collectionDB(t)
	pay, _, _, followup := collectionStack(t, db)
	payBill(t, db, "bill-replay", "q-replay", "row-replay", 400.00)
	collectionOpp(t, db, "opp-q-replay")

	first, err := pay.RecordPayment(context.Background(), payIn("bill-replay", "ref-replay-1", 400.00))
	if err != nil {
		t.Fatalf("首次入账失败: %v", err)
	}
	if first.Collection != "ok" {
		t.Fatalf(`首次 Collection = %q，要 "ok"（否则重放的前提不成立）`, first.Collection)
	}

	again, err := pay.RecordPayment(context.Background(), payIn("bill-replay", "ref-replay-1", 400.00))
	if err != nil {
		t.Fatalf("重放失败: %v", err)
	}
	if !again.Reused {
		t.Errorf("重放 Reused = false，要 true（幂等通路没认出同一笔）")
	}
	if again.Collection != "" {
		t.Errorf(`重放 Collection = %q，要空串（账单这次没跃迁，不许再触发）`, again.Collection)
	}
	if got := collectionReminders(followup, "sales_a"); len(got) != 1 {
		t.Errorf("跟进条数 = %d，要 1（重放又排了一条就是幂等破）", len(got))
	}
}

// TestCollectionHookReversalThenRepaySkipsSOP 冲销把账单打回 partial（不触发），
// 换新流水号重付结掉 ⇒ 触发但商机已 won，结论 "skipped"，跟进不增加。
func TestCollectionHookReversalThenRepaySkipsSOP(t *testing.T) {
	db := collectionDB(t)
	pay, _, _, followup := collectionStack(t, db)
	payBill(t, db, "bill-rev", "q-rev", "row-rev", 400.00)
	collectionOpp(t, db, "opp-q-rev")
	ctx := context.Background()

	if rc, err := pay.RecordPayment(ctx, payIn("bill-rev", "ref-rev-1", 400.00)); err != nil || rc.Collection != "ok" {
		t.Fatalf("首次入账 = %+v, err = %v，要 ok（否则链条的起点不成立）", rc, err)
	}
	revIn := payIn("bill-rev", "ref-rev-1", 400.00)
	revIn.Status = model.PaymentStatusReversed
	rev, err := pay.RecordPayment(ctx, revIn)
	if err != nil {
		t.Fatalf("冲销失败: %v", err)
	}
	if rev.Collection != "" {
		t.Errorf(`冲销 Collection = %q，要空串（partial/退回都不许触发赢单）`, rev.Collection)
	}

	repay, err := pay.RecordPayment(ctx, payIn("bill-rev", "ref-rev-2", 400.00))
	if err != nil {
		t.Fatalf("重付失败: %v", err)
	}
	if repay.Settlement.Status != model.BillStatusPaid {
		t.Fatalf("重付后账单 = %s，要 paid（否则触发条件不成立）", repay.Settlement.Status)
	}
	if repay.Collection != "skipped" {
		t.Errorf(`重付 Collection = %q，要 "skipped"（商机早已 won，SOP 不重排）`, repay.Collection)
	}
	if got := collectionReminders(followup, "sales_a"); len(got) != 1 {
		t.Errorf("跟进条数 = %d，要 1（冲销重付又排一条就是 AC③ 破）", len(got))
	}
	if n := payRows(t, db); n != 2 {
		t.Errorf("回款行数 = %d，要 2（两笔真钱各记一行，重付不许复活旧行）", n)
	}
}

// —— 失败隔离 / 无关联 / 没装钩子 ——————————————————————————————————————

// TestCollectionHookErrorDoesNotFailPayment 钩子报错 ⇒ 支付照样成功，
// 结论记 "error: …"，商机行不动（钩子炸在赢单之前）。
func TestCollectionHookErrorDoesNotFailPayment(t *testing.T) {
	db := collectionDB(t)
	pay := paySvc(t, db)
	boom := errors.New("boom-hook")
	pay.SetCollectionCompletedHook(func(ctx context.Context, bill *model.Bill) error { return boom })
	payBill(t, db, "bill-boom", "q-boom", "row-boom", 400.00)
	collectionOpp(t, db, "opp-q-boom")

	rc, err := pay.RecordPayment(context.Background(), payIn("bill-boom", "ref-boom-1", 400.00))
	if err != nil {
		t.Fatalf("钩子炸了不许败支付，入账却失败: %v", err)
	}
	if rc.Settlement.Status != model.BillStatusPaid {
		t.Errorf("账单 = %s，要 paid（钩子失败不许回滚钱和单）", rc.Settlement.Status)
	}
	if !strings.HasPrefix(rc.Collection, "error: ") {
		t.Errorf(`Collection = %q，要 "error: …" 前缀（失败要留痕）`, rc.Collection)
	}
	if opp := readOpportunityRow(t, db, "opp-q-boom"); opp.Status != model.OpportunityStatusOpen {
		t.Errorf("商机 = %s，要 open（钩子炸在赢单之前，行不许动）", opp.Status)
	}
}

// TestCollectionHookSkipsBillWithoutOpportunity 账单没关联商机 ⇒ 钩子主动跳过。
//
// 这条不用真账单走全链：账单仓储在 Create 时就拒掉空 OpportunityID（空串会让应收
// 在报表里凭空消失），"无关联账单结到 paid"在生产到不了钩子。这里直调钩子，
// 只证跳过分支本身（钱照记单照结是 fire 侧的结论记账，已由其它用例覆盖）。
func TestCollectionHookSkipsBillWithoutOpportunity(t *testing.T) {
	hook := WireCollectionToRepurchase(nil, nil, nil)
	err := hook(context.Background(), &model.Bill{ID: "bill-noopp", OpportunityID: "  "})
	if !errors.Is(err, ErrCollectionHookSkipped) {
		t.Errorf("err = %v，要 errors.Is ErrCollectionHookSkipped（没商机可赢，只能跳过）", err)
	}
	if !errors.Is(err, ErrCollectionHookSkippedNoOpportunity) {
		t.Errorf("err = %v，要包住 ErrCollectionHookSkippedNoOpportunity（原因要精确）", err)
	}
}

// TestCollectionHookAbsentRecordsNoHook 没装钩子 ⇒ 结论 "no-hook"，支付不受影响。
func TestCollectionHookAbsentRecordsNoHook(t *testing.T) {
	db := collectionDB(t)
	pay := paySvc(t, db) // 刻意不装钩子
	payBill(t, db, "bill-nohook", "q-nohook", "row-nohook", 400.00)

	rc, err := pay.RecordPayment(context.Background(), payIn("bill-nohook", "ref-nohook-1", 400.00))
	if err != nil {
		t.Fatalf("入账失败: %v", err)
	}
	if rc.Settlement.Status != model.BillStatusPaid {
		t.Errorf("账单 = %s，要 paid", rc.Settlement.Status)
	}
	if rc.Collection != "no-hook" {
		t.Errorf(`Collection = %q，要 "no-hook"（触发条件到了但没人装）`, rc.Collection)
	}
}

// —— CompleteCollection 直达 ——————————————————————————————————————————

// TestOpportunityCompleteCollection 收口语义：不存在的行报 404；在跑的行落 won
// 且赢率不动；已 won 的行只返回门，不写库（version 不涨）。
func TestOpportunityCompleteCollection(t *testing.T) {
	db := collectionDB(t)
	opps := NewOpportunityService(repository.NewOpportunityRepositoryWithDB(db))
	opps.SetClock(func() time.Time { return oppClockBase })
	ctx := context.Background()

	if _, _, err := opps.CompleteCollection(ctx, "opp-missing"); !errors.Is(err, ErrOpportunityNotFound) {
		t.Errorf("不存在的行 err = %v，要 ErrOpportunityNotFound", err)
	}

	before := collectionOpp(t, db, "opp-cc1")
	alreadyWon, after, err := opps.CompleteCollection(ctx, "opp-cc1")
	if err != nil {
		t.Fatalf("收口失败: %v", err)
	}
	if alreadyWon {
		t.Errorf("alreadyWon = true，要 false（这一行是第一次赢）")
	}
	if after.Status != model.OpportunityStatusWon {
		t.Errorf("收口后 = %s，要 won", after.Status)
	}
	if after.WinProbability != 0.42 {
		t.Errorf("赢率 = %v，要 0.42（终态不重算）", after.WinProbability)
	}

	versionAfterFirst := readOpportunityRow(t, db, "opp-cc1").Version
	_ = before
	alreadyWon2, _, err := opps.CompleteCollection(ctx, "opp-cc1")
	if err != nil {
		t.Fatalf("重放收口失败: %v", err)
	}
	if !alreadyWon2 {
		t.Errorf("重放 alreadyWon = false，要 true（行早已是 won）")
	}
	if v := readOpportunityRow(t, db, "opp-cc1").Version; v != versionAfterFirst {
		t.Errorf("重放后 version = %d，要 %d（幂等重放零改写）", v, versionAfterFirst)
	}
}
