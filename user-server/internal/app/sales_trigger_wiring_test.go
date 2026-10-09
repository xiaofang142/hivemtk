package app

import (
	"context"
	"testing"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/service"
)

// TestParseSalesTriggerEnabled 钉住项11b 那把开关的解析口径：
// 认不出的值必须落回 off（fail-closed）——这是一把会**多开四类副作用**的开关，
// 把错值读成"开"比读成"关"坏得多。
func TestParseSalesTriggerEnabled(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{"", false},
		{"off", false},
		{"false", false},
		{"0", false},
		{"no", false},
		{"disabled", false},
		{"true", true},
		{"1", true},
		{"yes", true},
		{"y", true},
		{"on", true},
		{" ON ", true},
		{"maybe", false}, // 不识别 ⇒ 关
		{"onfire", false},
	}
	for _, c := range cases {
		if got := parseSalesTriggerEnabled(c.raw); got != c.want {
			t.Errorf("parseSalesTriggerEnabled(%q) = %v, want %v", c.raw, got, c.want)
		}
	}
}

// salesTriggerResp 一条能被意向提取器认出产品与价格的 AI 谈单响应。
// 文案与 service 侧既有用例同源（光子嫩肤 3 次 2280 元）。
func salesTriggerResp() *service.SalesResponse {
	return &service.SalesResponse{
		Reply:  "好的，光子嫩肤 3 次套餐 2280 元，预约周六可以吗？",
		Intent: &dto.RecognizeResult{IntentType: service.IntentPurchase, IntentName: "准备购买", Confidence: 0.92},
	}
}

// TestOrderDraftProduceFunc_TriggerTakesOver 项11b 的互斥分发：
// triggerEnabled=true ⇒ 由触发器接管（历史里有 1 条 sales_response）；
// triggerEnabled=false ⇒ 走老路径只建草稿（历史 0 条）。
// 两条分支**互斥**是这条用例的判据本身 —— 若有人把 return 删掉改成顺序执行，
// CreateFromIntent 的"同客户同产品 pending 合并"会把 3 次累加成 6 次（假订单数据），
// 而这道断言（历史条数 + 草稿数量/数量）当场就会红。
func TestOrderDraftProduceFunc_TriggerTakesOver(t *testing.T) {
	ctx := context.Background()
	custID, ownerID := "cust_trigger_wire", "sales_trigger_wire"

	// 装配口径与生产一致：触发器**恒装配**（SetDraftService/SetTrigger 那两跳在 Init 里
	// 就落位了），开关只决定它是否接管分发 —— 两者混为一谈就是把"接线"和"开闸"同一件事。
	newRT := func(enabled bool) *OrderDraftRuntime {
		svc := service.NewOrderDraftService(nil)
		extractor := service.NewOrderIntentExtractor()
		trigger := service.NewSalesActionTrigger(nil, nil, nil, extractor, nil, nil)
		// 与 InitOrderDraftRuntime 同一跳：触发器建草稿要拿到草稿服务。
		// 漏了这一跳，接管档会"跑完触发器却一张草稿都没有"——正是 11b 要消灭的那种假接线。
		trigger.SetDraftService(context.Background(), svc)
		return &OrderDraftRuntime{svc: svc, extractor: extractor, trigger: trigger, triggerEnabled: enabled}
	}

	// 开：触发器接管 ⇒ 建了草稿，且触发器确实跑过（history=1）。
	rt := newRT(true)
	orderDraftProduceFunc(rt)(ctx, custID, ownerID, salesTriggerResp())

	pending, err := rt.svc.ListPending(ctx, ownerID, 0)
	if err != nil {
		t.Fatalf("读待确认草稿失败: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("触发器接管后应有 1 个待确认草稿，实际 %d", len(pending))
	}
	if pending[0].Quantity != 3 {
		t.Errorf("数量应为 3（同一条响应只被处理一次），实际 %d —— 若是 6 说明两条分支串联执行了", pending[0].Quantity)
	}
	if n := len(rt.trigger.GetHistory(ctx, custID, 10)); n != 1 {
		t.Errorf("接管档应留下 1 条触发记录，实际 %d", n)
	}

	// 关（默认）：老路径只建草稿，触发器不参与 ⇒ history 必须为 0。
	off := newRT(false)
	orderDraftProduceFunc(off)(ctx, custID, ownerID, salesTriggerResp())

	pending2, err := off.svc.ListPending(ctx, ownerID, 0)
	if err != nil {
		t.Fatalf("读待确认草稿失败: %v", err)
	}
	if len(pending2) != 1 {
		t.Fatalf("关闭档也应有 1 个待确认草稿，实际 %d", len(pending2))
	}
	if pending2[0].Quantity != 3 {
		t.Errorf("关闭档数量应为 3，实际 %d", pending2[0].Quantity)
	}
	if n := len(off.trigger.GetHistory(ctx, custID, 10)); n != 0 {
		t.Errorf("关闭档触发器不该接管分发，实际留下 %d 条记录 —— 分发不互斥就会重复建单/重复排跟进", n)
	}
}
