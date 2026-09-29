package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// TestSalesTraceChainEndToEnd T-P8-01 AC①：同一条商机的事件链必须端到端不断。
//
// convert（商机创建）→ quote 生成/审批/发送 → bill 派生 → 全额回款（结清）→
// CompleteCollection（赢单），最后按 ListByOpportunityID 读回，断言：
//  1. 事件按 (occurred_at, id) 有序即链序；
//  2. 序列恰为 [opportunity.created, quote.approval_requested, quote.sent,
//     bill.created, bill.settled(paid), opportunity.won]；
//  3. 每一跳的 opportunity_id 非空且全等（断链的形状就是某一跳没带上商机号）。
func TestSalesTraceChainEndToEnd(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	db := testutil.NewTestDB(t,
		&model.Opportunity{}, &model.Clue{}, &model.OperationLog{},
		&model.Quote{}, &model.QuoteLineItem{}, &model.ScriptLibrary{}, &model.ScriptVersion{},
		&model.ApprovalRequest{}, &model.SalesEvent{},
		&model.Bill{}, &model.Payment{},
	)
	events := repository.NewSalesEventRepositoryWithDB(db)

	// —— 第 1 跳：线索转商机（newConvertServiceWithDB 自带库，这里手装同体、共用 db）——
	oppRepo := repository.NewOpportunityRepositoryWithDB(db)
	conv := NewOpportunityConvertService(oppRepo, convGate{convGateOn()},
		NewOwnerAssigner(fixedRoster{ids: []string{"alpha"}}, oppRepo),
		repository.NewOperationLogRepositoryWithDB(db))
	conv.SetClock(func() time.Time { return now })
	conv.SetSalesTrace(events)
	convRes, err := conv.ConvertFromClue(ctx, OpportunityConversion{
		ClueID: "clue-chain-001", CustomerID: "cust-chain-001",
		OneID: "chan:chain001", LeadScore: 88, Confidence: 0.9, Amount: 80000,
	})
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if !convRes.Created {
		t.Fatalf("convert: want Created=true, got %+v", convRes)
	}
	oppID := convRes.Opportunity.ID

	// —— 第 2 跳：报价生成 → 审批 → 发送（复用 T-P6-03 的真库 harness）——
	gen, _, _ := qsSvc(t, db)
	gen.SetClock(func() time.Time { return now })
	view, err := gen.Generate(ctx, QuoteGenerateInput{
		OpportunityID: oppID, OneID: convRes.Opportunity.OneID, TemplateCode: "std_annual",
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	qss := qssSvcWithPolicy(t, db, nil)
	// 统一时钟：各 harness 自带固定时钟纪元不同（qs 9-19 / opp 9-20），
	// 不统一则 occurred_at 排序被测试时钟伪影污染（生产全走 time.Now，无此问题）。
	qss.svc.SetClock(func() time.Time { return now })
	first := qssMustSend(t, qss, ctx, qssSendInput(view.ID))
	// 生产形状：首发只开审批，版本行仍是 draft（pending 只存在于测试种子里），
	// 关键是 Disposition=awaiting 且拿到 ApprovalID。
	if first.Disposition != QuoteSendAwaiting || first.ApprovalID == "" {
		t.Fatalf("send#1: want awaiting+approval, got %+v", first)
	}
	qssApprove(t, qss, first.ApprovalID)
	second := qssMustSend(t, qss, ctx, qssSendWith(view.ID, first.ApprovalID))
	if second.Status != "sent" {
		t.Fatalf("send#2: want sent, got %q", second.Status)
	}

	// —— 第 3 跳：账单派生 ——
	billSvc := NewBillService(
		repository.NewBillRepositoryWithDB(db),
		repository.NewQuoteRepositoryWithDB(db),
	)
	billSvc.SetClock(func() time.Time { return now })
	billSvc.SetSalesTrace(events)
	due := now.Add(30 * 24 * time.Hour)
	billV, err := billSvc.DeriveFromQuote(ctx, BillDeriveInput{
		QuoteRowID: view.ID, DueAt: &due,
	})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}

	// —— 第 4 跳：全额回款（一次结清）——
	pay := paySvc(t, db)
	pay.SetClock(func() time.Time { return now })
	pay.SetSalesTrace(events)
	rec, err := pay.RecordPayment(ctx, RecordPaymentInput{
		BillID: billV.ID, ChannelRef: "chain-pay-001",
		Amount: billV.Amount, Currency: billV.Currency,
		Platform: "wechat", OrderID: "chain-order-001",
	})
	if err != nil {
		t.Fatalf("record payment: %v", err)
	}
	if rec.Settlement.Status != model.BillStatusPaid || !rec.Settlement.Transited {
		t.Fatalf("settlement: want paid+transited, got %+v", rec.Settlement)
	}

	// —— 第 5 跳：赢单收口（生产里由回款钩子调，这里直调同一入口）——
	oppSvc := NewOpportunityService(repository.NewOpportunityRepositoryWithDB(db))
	oppSvc.SetClock(func() time.Time { return now })
	oppSvc.SetSalesTrace(events)
	alreadyWon, _, err := oppSvc.CompleteCollection(ctx, oppID)
	if err != nil {
		t.Fatalf("complete collection: %v", err)
	}
	if alreadyWon {
		t.Fatalf("complete collection: want alreadyWon=false on first completion")
	}

	// —— 读链：断言有序、序列完整、商机号全等非空 ——
	chain, err := events.ListByOpportunityID(ctx, oppID)
	if err != nil {
		t.Fatalf("list chain: %v", err)
	}
	type hop struct{ typ, action, result string }
	want := []hop{
		{model.SalesEventTypeOpportunity, "created", ""},
		{model.SalesEventTypeQuote, "sent", ""},
		{model.SalesEventTypeBill, "created", ""},
		{model.SalesEventTypeBill, "settled", model.BillStatusPaid},
		{model.SalesEventTypeOpportunity, "won", ""},
	}
	if len(chain) != len(want) {
		for _, ev := range chain {
			t.Logf("got event: type=%s action=%s result=%s opp=%s", ev.EventType, ev.Action, ev.Result, ev.OpportunityID)
		}
		t.Fatalf("chain: want %d events, got %d", len(want), len(chain))
	}
	for i, ev := range chain {
		if ev.OpportunityID == "" {
			t.Fatalf("chain[%d] (%s/%s): opportunity_id 为空 —— 链在这里断了", i, ev.EventType, ev.Action)
		}
		if ev.OpportunityID != oppID {
			t.Fatalf("chain[%d] (%s/%s): opportunity_id=%q 与本链 %q 不一致", i, ev.EventType, ev.Action, ev.OpportunityID, oppID)
		}
		if ev.EventType != want[i].typ || ev.Action != want[i].action {
			t.Fatalf("chain[%d]: want (%s/%s), got (%s/%s)", i, want[i].typ, want[i].action, ev.EventType, ev.Action)
		}
		if want[i].result != "" && ev.Result != want[i].result {
			t.Fatalf("chain[%d]: want result=%q, got %q", i, want[i].result, ev.Result)
		}
	}
}
