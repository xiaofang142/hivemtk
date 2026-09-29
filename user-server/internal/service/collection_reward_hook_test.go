package service

import (
	"context"
	"errors"
	"math"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service/feedback_loop"
)

// —— 奖励钩子替身 ————————————————————————————————————————————

type rewardHookSpy struct {
	calls    int
	lastBill *model.Bill
	lastView SettlementView
	err      error
}

func (s *rewardHookSpy) hook() CollectionRewardHook {
	return func(_ context.Context, bill *model.Bill, view SettlementView) error {
		s.calls++
		s.lastBill = bill
		s.lastView = view
		return s.err
	}
}

type lostHookSpy struct {
	calls int
	last  *model.Opportunity
	err   error
}

func (s *lostHookSpy) hook() LostRewardHook {
	return func(_ context.Context, opp *model.Opportunity) error {
		s.calls++
		s.last = opp
		return s.err
	}
}

// TestPaymentRewardHook_PaidAndPartialFire 全额与部分回款都触发奖励钩子（T-P8-02，AC①）。
//
// partial 同样发 —— Result 携带 paid/partial，链上不断；
func TestPaymentRewardHook_PaidAndPartialFire(t *testing.T) {
	ctx := context.Background()

	db := paySetupDB(t)
	payBill(t, db, "bill-rw1", "q-rw1", "qr-rw1", 1000)
	svc := paySvc(t, db)
	spy := &rewardHookSpy{}
	svc.SetCollectionRewardHook(spy.hook())

	if _, err := svc.RecordPayment(ctx, payIn("bill-rw1", "ref-rw1", 1000)); err != nil {
		t.Fatalf("全额入账失败: %v", err)
	}
	if spy.calls != 1 {
		t.Fatalf("全额 paid 钩子 calls = %d want 1", spy.calls)
	}
	if spy.lastView.Status != model.BillStatusPaid {
		t.Errorf("钩子 settled.Status = %q want paid", spy.lastView.Status)
	}
	if spy.lastBill == nil || spy.lastBill.ID != "bill-rw1" {
		t.Errorf("钩子 bill 传参不对: %+v", spy.lastBill)
	}

	payBill(t, db, "bill-rw2", "q-rw2", "qr-rw2", 1000)
	if _, err := svc.RecordPayment(ctx, payIn("bill-rw2", "ref-rw2", 400)); err != nil {
		t.Fatalf("部分入账失败: %v", err)
	}
	if spy.calls != 2 {
		t.Fatalf("部分 partial 钩子 calls = %d want 2", spy.calls)
	}
	if spy.lastView.Status != model.BillStatusPartial {
		t.Errorf("钩子 settled.Status = %q want partial", spy.lastView.Status)
	}
}

// TestPaymentRewardHook_ReplaySilent 幂等重放不重触发（T-P8-02）。
func TestPaymentRewardHook_ReplaySilent(t *testing.T) {
	ctx := context.Background()
	db := paySetupDB(t)
	payBill(t, db, "bill-rw3", "q-rw3", "qr-rw3", 1000)
	svc := paySvc(t, db)
	spy := &rewardHookSpy{}
	svc.SetCollectionRewardHook(spy.hook())

	if _, err := svc.RecordPayment(ctx, payIn("bill-rw3", "ref-rw3", 1000)); err != nil {
		t.Fatalf("首笔入账失败: %v", err)
	}
	if _, err := svc.RecordPayment(ctx, payIn("bill-rw3", "ref-rw3", 1000)); err != nil {
		t.Fatalf("重放失败: %v", err)
	}
	if spy.calls != 1 {
		t.Errorf("重放后钩子 calls = %d want 1（Transited=false 不发）", spy.calls)
	}
}

// TestPaymentRewardHook_ErrorKeepsReceipt 钩子错不断支付（T-P8-02）。
func TestPaymentRewardHook_ErrorKeepsReceipt(t *testing.T) {
	ctx := context.Background()
	db := paySetupDB(t)
	payBill(t, db, "bill-rw4", "q-rw4", "qr-rw4", 1000)
	svc := paySvc(t, db)
	spy := &rewardHookSpy{err: errors.New("collector down")}
	svc.SetCollectionRewardHook(spy.hook())

	rcpt, err := svc.RecordPayment(ctx, payIn("bill-rw4", "ref-rw4", 1000))
	if err != nil {
		t.Fatalf("钩子错了支付也不该败: %v", err)
	}
	if rcpt.Settlement.Status != model.BillStatusPaid {
		t.Errorf("receipt status = %q want paid", rcpt.Settlement.Status)
	}
	if spy.calls != 1 {
		t.Errorf("钩子 calls = %d want 1（错了也算触发过）", spy.calls)
	}
}

// TestMarkLost_FiresLostHookOnce 丢单真跃迁触发一次，重放不再触发（T-P8-02，AC②）。
func TestMarkLost_FiresLostHookOnce(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newOpportunityServiceWithDB(t)
	row := seedOpportunity(t, repo, "opp-lost1", model.OpportunityStageProposal, model.OpportunityStatusOpen)
	spy := &lostHookSpy{}
	svc.SetLostRewardHook(spy.hook())

	if _, err := svc.MarkLost(ctx, row.ID, row.Version, "比价输了"); err != nil {
		t.Fatalf("MarkLost 失败: %v", err)
	}
	if spy.calls != 1 {
		t.Fatalf("丢单钩子 calls = %d want 1", spy.calls)
	}
	if spy.last == nil || spy.last.ID != row.ID {
		t.Errorf("钩子 opp 传参不对: %+v", spy.last)
	}
	// 重放：已 lost 的行被收口守卫拦下，钩子不再触发。
	if _, err := svc.MarkLost(ctx, row.ID, row.Version+1, "再输一次"); err == nil {
		t.Fatalf("已 lost 重放入口该拒")
	}
	if spy.calls != 1 {
		t.Errorf("重放后钩子 calls = %d want 1", spy.calls)
	}
}

// TestMarkLost_HookErrorKeepsLost 钩子错不断丢单（T-P8-02）。
func TestMarkLost_HookErrorKeepsLost(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newOpportunityServiceWithDB(t)
	row := seedOpportunity(t, repo, "opp-lost2", model.OpportunityStageProposal, model.OpportunityStatusOpen)
	spy := &lostHookSpy{err: errors.New("collector down")}
	svc.SetLostRewardHook(spy.hook())

	got, err := svc.MarkLost(ctx, row.ID, row.Version, "价格没谈拢")
	if err != nil {
		t.Fatalf("钩子错了丢单也不该败: %v", err)
	}
	if got.Status != model.OpportunityStatusLost {
		t.Errorf("status = %q want lost", got.Status)
	}
}

// TestWireCollectionReward_PaidPersistsScaledReward 端到端：全额回款 → feedback_events
// 落一行 collection 事件，reward 按金额缩放（T-P8-02，AC①）。
func TestWireCollectionReward_PaidPersistsScaledReward(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewTestDB(t,
		&model.Bill{}, &model.Payment{}, &model.Opportunity{},
		&model.FeedbackEvent{}, &model.FeedbackSignal{},
	)
	oppRepo := repository.NewOpportunityRepositoryWithDB(db)
	oppSvc := NewOpportunityService(oppRepo)
	seedOpportunity(t, oppRepo, "opp-q-w1", model.OpportunityStageProposal, model.OpportunityStatusOpen)
	payBill(t, db, "bill-wire1", "q-w1", "qr-w1", 1000)

	cfg := feedbackloop.DefaultFeedbackCollectorConfig()
	collector := feedbackloop.NewFeedbackCollector(db, cfg)

	svc := paySvc(t, db)
	svc.SetCollectionRewardHook(WireCollectionReward(oppSvc, collector))

	if _, err := svc.RecordPayment(ctx, payIn("bill-wire1", "ref-wire1", 1000)); err != nil {
		t.Fatalf("入账失败: %v", err)
	}
	collector.Stop() // 显式一次：Stop 排空队列且非幂等，不用 defer 再调

	var ev model.FeedbackEvent
	if err := db.Where("session_id = ?", "collection:bill-wire1").First(&ev).Error; err != nil {
		t.Fatalf("feedback_events 里没有 collection 事件: %v", err)
	}
	// Reward 列是 decimal(6,3)：库侧三位小数是既有精度（全信号共用），断言按库精度比。
	want := math.Round(0.5*math.Log10(1+1000)*1000) / 1000
	if ev.Reward < want-1e-9 || ev.Reward > want+1e-9 {
		t.Errorf("event reward = %v want %v", ev.Reward, want)
	}
	if ev.SignalKey != "collection" {
		t.Errorf("signal_key = %q want collection", ev.SignalKey)
	}
}

// TestWireCollectionReward_NoOpportunitySkips 无商机关联的账单：reward 缺席，支付照成功。
func TestWireCollectionReward_NoOpportunitySkips(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewTestDB(t,
		&model.Bill{}, &model.Payment{}, &model.Opportunity{},
		&model.FeedbackEvent{}, &model.FeedbackSignal{},
	)
	oppRepo := repository.NewOpportunityRepositoryWithDB(db)
	oppSvc := NewOpportunityService(oppRepo)
	// 账单挂一个库里不存在的商机（遗留脏行形状）。
	row := payBill(t, db, "bill-wire2", "q-w2", "qr-w2", 500)
	row.OpportunityID = "opp-missing"
	if err := db.Save(row).Error; err != nil {
		t.Fatalf("改账单商机关联失败: %v", err)
	}

	collector := feedbackloop.NewFeedbackCollector(db, feedbackloop.DefaultFeedbackCollectorConfig())
	svc := paySvc(t, db)
	svc.SetCollectionRewardHook(WireCollectionReward(oppSvc, collector))

	rcpt, err := svc.RecordPayment(ctx, payIn("bill-wire2", "ref-wire2", 500))
	if err != nil {
		t.Fatalf("无商机账单的支付也不该败: %v", err)
	}
	if rcpt.Settlement.Status != model.BillStatusPaid {
		t.Errorf("status = %q want paid", rcpt.Settlement.Status)
	}
	collector.Stop()

	var n int64
	db.Model(&model.FeedbackEvent{}).Where("session_id = ?", "collection:bill-wire2").Count(&n)
	if n != 0 {
		t.Errorf("无商机账单落了 %d 条 reward 事件 want 0", n)
	}
}
