// quote_discount_test.go T-P6-04：折扣二次审批策略。
//
// 卡面三条判据（docs/replan-2026-09/新规划任务清单.md T-P6-04）：
//
//	① 阈值边界值测试（**含等于**）
//	② 高折扣无法通过低档审批放行（反向测试）
//	③ 策略可配、不改代码
//
// 本文件把"两道门"的形状钉死：同一版高折扣报价在 approval_requests 里留下**两行**，
// policy_key 各为 quote.send 与 quote.discount_high；拿着前者的批准点发送，只能把后者
// 开出来，绝不能出域。低折扣版本则永远只有一行 —— 多开一道门同样是错（审批人被拉去
// 批一件本不需要批的事，且第二道门很快就不被当回事了）。
//
// 夹具全部走真路：草稿经 T-P6-02 的生成腿造（明细落库后由发送腿自己读回来判档），
// 审批经 T-P3-01 的真服务开与裁决（"停在 pending"是表里那一行的状态，替身说了不算）。
package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// qsdTemplateLines 是 qsTemplateJSON 里那两行的 product_id 与自带折扣。
// 夹具按**位置**覆盖折扣，所以这两格必须与模板那份常量逐字对上。
var qsdTemplateLines = []struct {
	productID string
	disc      float64
}{
	{"p_seat", 10},
	{"p_impl", 5},
}

// qsdDraftDisc 造一版草稿，把每行的折扣按位置覆盖成 discs；discs 短于模板行数时，
// 剩下的行保留模板值。返回版本行主键。
//
// 为什么按位置而不是"传一个折扣"：AC① 的边界要落在**某一行**上，而卡面没说取哪一行；
// 只有能把高低两档摆到第一行/第二行两个位置上，才杀得掉"实现只看第一行"那一刀。
func qsdDraftDisc(t *testing.T, db *gorm.DB, discs ...float64) string {
	t.Helper()
	if len(discs) > len(qsdTemplateLines) {
		t.Fatalf("模板只有 %d 行，造不出 %d 种折扣", len(qsdTemplateLines), len(discs))
	}
	qssDraftSeq++
	oppID := fmt.Sprintf("opp_disc_%d", qssDraftSeq)
	qssSeedOpp(t, db, oppID, "one_1")
	gen, cfg, _ := qsSvc(t, db)
	cfg[qsTemplateKey] = qsTemplateJSON

	want := make([]float64, len(qsdTemplateLines))
	for i, l := range qsdTemplateLines {
		want[i] = l.disc
	}
	lines := make([]QuoteLineInput, 0, len(discs))
	for i, d := range discs {
		lines = append(lines, QuoteLineInput{
			ProductID: qsdTemplateLines[i].productID, DiscountPercent: qsF(d),
		})
		want[i] = d
	}

	view, err := gen.Generate(context.Background(), QuoteGenerateInput{
		OpportunityID: oppID, OneID: "one_1", TemplateCode: "std_annual", Lines: lines,
	})
	if err != nil {
		t.Fatalf("造草稿失败：%v", err)
	}
	if view.Status != model.QuoteStatusDraft {
		t.Fatalf("生成腿交出来的不是草稿：%s", view.Status)
	}
	qsdAssertDiscs(t, db, view.ID, want)
	return view.ID
}

// qsdAssertDiscs 把"库里那几行的折扣确实是夹具要的"钉成 Fatal。
// 本卡每一条判据都建立在折扣值上：夹具漂了后面的绿一文不值（红因写在夹具里，不写在断言里）。
func qsdAssertDiscs(t *testing.T, db *gorm.DB, rowID string, want []float64) {
	t.Helper()
	var rows []*model.QuoteLineItem
	if err := db.WithContext(context.Background()).
		Where("quote_row_id = ?", rowID).Order("line_no").Find(&rows).Error; err != nil {
		t.Fatalf("读回明细失败：%v", err)
	}
	if len(rows) != len(want) {
		t.Fatalf("明细行数=%d，期望 %d", len(rows), len(want))
	}
	for i, r := range rows {
		if r.DiscountPercent != want[i] {
			t.Fatalf("第 %d 行折扣=%v，期望 %v（夹具没落成想要的值，后面的判据全不成立）",
				i+1, r.DiscountPercent, want[i])
		}
	}
}

// qsdGateThreshold 只换阈值那一格（主开关与阶段开关保持 qsGateOn 的样子）：
// AC③ 要的是"改一个配置值就换档"，所以换的必须只有那一格。
func qsdGateThreshold(p *qssParts, threshold float64) {
	cfg := qsGateOn()
	cfg.Thresholds.DiscountPercent = threshold
	p.svc.SetGate(qsGate{cfg})
}

// qsdPassFirstGate 走完"点发送 → 批掉低档"这两步，回低档那条审批号。
//
// 它顺手钉住两道门不会并成一格：第一条待办的 policy_key 必须是 quote.send。
// 高折扣版本的**第二次** Send 若复用那一行，本卡的整套语义就从"两道独立的裁决"
// 退化成"一次裁决管两件事"，而那是 model/approval_request.go:33 明令不许的形状。
func qsdPassFirstGate(t *testing.T, db *gorm.DB, p *qssParts, ctx context.Context, rowID string) string {
	t.Helper()
	first := qssMustSend(t, p, ctx, qssSendInput(rowID))
	if first.Disposition != QuoteSendAwaiting {
		t.Fatalf("第一次发送的处置=%s，期望 %s", first.Disposition, QuoteSendAwaiting)
	}
	if got := qssApprovalRow(t, db, first.ApprovalID).PolicyKey; got != QuoteSendPolicyKey {
		t.Fatalf("第一条待办的 policy_key=%s，期望 %s", got, QuoteSendPolicyKey)
	}
	qssApprove(t, p, first.ApprovalID)
	return first.ApprovalID
}

// qsdPolicy 是一张按 policy_key 分别作答的自动放行策略，并记下被问的顺序。
//
// 分键作答是 AC② 最硬的那一格：一张"什么都不批"的策略证不了"两道门各自被问了一次"，
// 一张"什么都批"的策略又会让"高折扣被放行"分不清是实现的问题还是策略的问题。
type qsdPolicy struct {
	asked []string
	allow func(policyKey string) bool
}

func (p *qsdPolicy) AutoApproves(_ context.Context, in ApprovalSubmitInput) (bool, string) {
	p.asked = append(p.asked, in.PolicyKey)
	if p.allow == nil {
		return false, ""
	}
	if p.allow(in.PolicyKey) {
		return true, "策略自动放行"
	}
	return false, ""
}

// TestQuoteDiscount_AtThresholdNeedsSecondApproval AC① 的"含等于"那一半 + AC② 的正身：
// 折扣**正好等于**阈值（15.00 = 默认阈值）就要第二道门；低档批下来也只把第二道门开出来，
// 三件事必须同时成立：没外发、库里仍是草稿、新待办落在另一格 policy_key 上。
func TestQuoteDiscount_AtThresholdNeedsSecondApproval(t *testing.T) {
	db := qssSetupDB(t)
	rowID := qsdDraftDisc(t, db, 15.00)
	p := qssSvc(t, db) // 不改任何代码：默认阈值就是 15
	ctx := context.Background()

	low := qsdPassFirstGate(t, db, p, ctx, rowID)

	second := qssMustSend(t, p, ctx, qssSendWith(rowID, low))
	if p.reach.calls != 0 {
		t.Fatalf("拿着低档批准把 15%% 折扣的版本发出去了 %d 次", p.reach.calls)
	}
	if second.Disposition != QuoteSendAwaiting {
		t.Fatalf("低档批完之后的处置=%s，期望 %s（高折扣还差一道门）", second.Disposition, QuoteSendAwaiting)
	}
	if second.ApprovalID == low {
		t.Fatalf("第二道门复用了低档那条审批 %s ⇒ 两道门在库里无法区分", low)
	}
	high := qssApprovalRow(t, db, second.ApprovalID)
	if high.PolicyKey != QuoteDiscountPolicyKey {
		t.Errorf("第二条待办的 policy_key=%s，期望 %s", high.PolicyKey, QuoteDiscountPolicyKey)
	}
	if high.SubjectID != rowID {
		t.Errorf("第二条待办的 subject_id=%s，期望版本行 %s", high.SubjectID, rowID)
	}
	if got := qssStatus(t, db, rowID); got != model.QuoteStatusDraft {
		t.Errorf("状态=%s，期望仍是 %s", got, model.QuoteStatusDraft)
	}
	if n := qssCountApprovals(t, db); n != 2 {
		t.Errorf("审批行数=%d，期望 2（低档一条、高档一条）", n)
	}

	qssApprove(t, p, second.ApprovalID)
	third, err := p.svc.Send(ctx, qssSendWith(rowID, second.ApprovalID))
	if err != nil {
		t.Fatalf("两道门都批过之后发送失败：%v", err)
	}
	if third.Disposition != QuoteSendSent {
		t.Fatalf("处置=%s，期望 %s", third.Disposition, QuoteSendSent)
	}
	if p.reach.calls != 1 {
		t.Errorf("外发计数=%d，期望 1（第二道门批完才该出域，且只出一次）", p.reach.calls)
	}
	if got := qssStatus(t, db, rowID); got != model.QuoteStatusSent {
		t.Errorf("状态=%s，期望 %s", got, model.QuoteStatusSent)
	}
	if n := qssCountApprovals(t, db); n != 2 {
		t.Errorf("发出去之后审批行数=%d，期望仍是 2（派发不该再开待办）", n)
	}
}

// TestQuoteDiscount_BelowThresholdSendsOnFirstApproval AC① 的另一半：差一分就不该有第二道门。
//
// "只开一道"要有独立判据，否则"永远开两道"那一刀（把判据写成常量 true）能通过上一格用例：
// 上一格只看第二道门在不在，不看它不该在的时候也在。
func TestQuoteDiscount_BelowThresholdSendsOnFirstApproval(t *testing.T) {
	db := qssSetupDB(t)
	rowID := qsdDraftDisc(t, db, 14.99)
	p := qssSvc(t, db)
	ctx := context.Background()

	low := qsdPassFirstGate(t, db, p, ctx, rowID)

	res, err := p.svc.Send(ctx, qssSendWith(rowID, low))
	if err != nil {
		t.Fatalf("低折扣版本发送失败：%v", err)
	}
	if res.Disposition != QuoteSendSent {
		t.Fatalf("处置=%s，期望 %s（14.99 < 15 不该有第二道门）", res.Disposition, QuoteSendSent)
	}
	if p.reach.calls != 1 {
		t.Errorf("外发计数=%d，期望 1", p.reach.calls)
	}
	if n := qssCountApprovals(t, db); n != 1 {
		t.Errorf("审批行数=%d，期望 1（多开一道门=把不需要裁决的事送进待办中心）", n)
	}
	var highRows int64
	if err := db.WithContext(ctx).Model(&model.ApprovalRequest{}).
		Where("policy_key = ?", QuoteDiscountPolicyKey).Count(&highRows).Error; err != nil {
		t.Fatalf("数高档待办失败：%v", err)
	}
	if highRows != 0 {
		t.Errorf("库里出现 %d 条 %s，期望 0", highRows, QuoteDiscountPolicyKey)
	}
}

// TestQuoteDiscount_RetryWithLowApprovalDoesNotLeak 批过的那条低档审批可以反复拿来点：
// 每次都必须原地开/复用第二道门，既不外发也不堆垃圾待办。
//
// 这一格杀的是"第二次带号走到 dispatch"那一刀：实现若把"带号且 status=approved"直接当放行，
// 第一遍被 awaiting 挡住（因为先算门序），把 awaiting 那一支改坏之后第二遍就发出去了。
func TestQuoteDiscount_RetryWithLowApprovalDoesNotLeak(t *testing.T) {
	db := qssSetupDB(t)
	rowID := qsdDraftDisc(t, db, 30)
	p := qssSvc(t, db)
	ctx := context.Background()

	low := qsdPassFirstGate(t, db, p, ctx, rowID)

	first := qssMustSend(t, p, ctx, qssSendWith(rowID, low))
	again := qssMustSend(t, p, ctx, qssSendWith(rowID, low))
	if again.ApprovalID != first.ApprovalID {
		t.Errorf("重复点击开出第二条高档待办（%s → %s），期望复用 %s",
			first.ApprovalID, again.ApprovalID, first.ApprovalID)
	}
	if p.reach.calls != 0 {
		t.Errorf("外发计数=%d，期望 0", p.reach.calls)
	}
	if n := qssCountApprovals(t, db); n != 2 {
		t.Errorf("审批行数=%d，期望 2（两条各一道门，重复点击不该加长待办中心）", n)
	}
	if got := qssStatus(t, db, rowID); got != model.QuoteStatusDraft {
		t.Errorf("状态=%s，期望 %s", got, model.QuoteStatusDraft)
	}
}

// TestQuoteDiscount_HighGateCannotPassLowRow AC② 的反方向：低折扣版本不许拿高档审批派发。
//
// 判据不是"那道门不存在所以报错"，而是"它确实在、也被批了，仍然不开这一版"。
// 夹具走真 Submit + 真 Decide：这条形状在生产里造得出来（审批 API 收 subject_type/policy_key 入参，
// 只要有人给同一个版本行递一条高折扣待办并批掉，实现少比一列就等于"批了折扣就放行了发送"）。
func TestQuoteDiscount_HighGateCannotPassLowRow(t *testing.T) {
	db := qssSetupDB(t)
	rowID := qsdDraftDisc(t, db, 5)
	p := qssSvc(t, db)
	ctx := context.Background()

	high, created, err := p.appro.Submit(ctx, ApprovalSubmitInput{
		SubjectType: QuoteApprovalSubjectType, SubjectID: rowID, PolicyKey: QuoteDiscountPolicyKey,
	})
	if err != nil || !created {
		t.Fatalf("造一条高档审批失败：created=%v err=%v", created, err)
	}
	if _, err := p.appro.Decide(ctx, high.ID, ApprovalApprove, "boss_a", "折扣可以给"); err != nil {
		t.Fatalf("裁决失败：%v", err)
	}

	_, err = p.svc.Send(ctx, qssSendWith(rowID, high.ID))
	if !errors.Is(err, ErrQuoteSendApprovalMismatch) {
		t.Fatalf("低折扣版本用高档批准应报 %v，实际 %v", ErrQuoteSendApprovalMismatch, err)
	}
	if p.reach.calls != 0 {
		t.Errorf("外发计数=%d，期望 0", p.reach.calls)
	}
}

// TestQuoteDiscount_ThresholdIsConfigurable AC③：同一份折扣，改配置值就换档，代码一行不动。
//
// 刻意把"等于"摆在**非默认**阈值上（20 = 20）：只在默认 15 上测等于，
// 判据写成 `maxDiscount >= 15` 那把刀（把配置读进来却不用）能整批通过本卡。
func TestQuoteDiscount_ThresholdIsConfigurable(t *testing.T) {
	cases := []struct {
		name      string
		threshold float64
		disc      float64
		wantHigh  bool
	}{
		{"阈值高于折扣_只要一道门", 30, 20, false},
		{"阈值等于折扣_要两道门", 20, 20, true},
		{"阈值低于折扣_要两道门", 15, 20, true},
		{"阈值 25 折扣 20_只要一道门", 25, 20, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := qssSetupDB(t)
			rowID := qsdDraftDisc(t, db, tc.disc)
			p := qssSvc(t, db)
			qsdGateThreshold(p, tc.threshold)
			ctx := context.Background()

			low := qsdPassFirstGate(t, db, p, ctx, rowID)
			res := qssMustSend(t, p, ctx, qssSendWith(rowID, low))

			if tc.wantHigh {
				if res.Disposition != QuoteSendAwaiting || p.reach.calls != 0 {
					t.Fatalf("阈值 %v、折扣 %v：处置=%s 外发=%d，期望 %s 且不外发",
						tc.threshold, tc.disc, res.Disposition, p.reach.calls, QuoteSendAwaiting)
				}
				if got := qssApprovalRow(t, db, res.ApprovalID).PolicyKey; got != QuoteDiscountPolicyKey {
					t.Errorf("第二道门 policy_key=%s，期望 %s", got, QuoteDiscountPolicyKey)
				}
				return
			}
			if res.Disposition != QuoteSendSent || p.reach.calls != 1 {
				t.Fatalf("阈值 %v、折扣 %v：处置=%s 外发=%d，期望 %s 且外发一次",
					tc.threshold, tc.disc, res.Disposition, p.reach.calls, QuoteSendSent)
			}
		})
	}
}

// TestQuoteDiscount_MaxLineDecidesTier 多行取**最大**那一行的折扣，不是第一行、不是最后一条、
// 不是合计、也不是平均。
//
// 四格各自的杀法不同：[10,20] 阈值 20 杀"只看第一行"与"取平均"；[20,10] 同一阈值杀"只看最后一行"；
// [10,15] 阈值 16 杀"把折扣相加"（25 ≥ 16 会误开第二道门）。
func TestQuoteDiscount_MaxLineDecidesTier(t *testing.T) {
	cases := []struct {
		name      string
		discs     []float64
		threshold float64
		wantHigh  bool
	}{
		{"高折扣在第二行", []float64{10, 20}, 20, true},
		{"高折扣在第一行", []float64{20, 10}, 20, true},
		{"两行相加才超阈值", []float64{10, 15}, 16, false},
		{"最高行正好压线", []float64{3, 16}, 16, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := qssSetupDB(t)
			rowID := qsdDraftDisc(t, db, tc.discs...)
			p := qssSvc(t, db)
			qsdGateThreshold(p, tc.threshold)
			ctx := context.Background()

			low := qsdPassFirstGate(t, db, p, ctx, rowID)
			res := qssMustSend(t, p, ctx, qssSendWith(rowID, low))

			if tc.wantHigh && res.Disposition != QuoteSendAwaiting {
				t.Fatalf("折扣 %v 阈值 %v：处置=%s，期望 %s（取最大行应落在高档）",
					tc.discs, tc.threshold, res.Disposition, QuoteSendAwaiting)
			}
			if !tc.wantHigh && res.Disposition != QuoteSendSent {
				t.Fatalf("折扣 %v 阈值 %v：处置=%s，期望 %s（取最大行不该落进高档）",
					tc.discs, tc.threshold, res.Disposition, QuoteSendSent)
			}
		})
	}
}

// TestQuoteDiscount_ZeroThresholdMeansAnyDiscount 阈值为 0 时是"任何折扣都要审批"，
// 不是"零折扣也要审批"（ltc_config.go:199 那格 ZeroLegal 的注释就是这个意思）。
//
// 判据若写成 `max >= threshold`，阈值 0 会把**每一版原价报价**都送进第二道门 ——
// 那是把闸门焊死，销售按不动发送键，而配置面上它看起来完全合法。
//
// 两格都把**两行**一起摆上桌：模板自带的第二行是 5%，只覆盖第一行的话"原价"那格
// 其实还留着 5% 的折扣（夹具自己就不成立，判据再对也测不到东西）。
func TestQuoteDiscount_ZeroThresholdMeansAnyDiscount(t *testing.T) {
	cases := []struct {
		name     string
		discs    []float64
		wantHigh bool
	}{
		{"阈值0_一分钱折扣也要批", []float64{0.01, 0.01}, true},
		{"阈值0_原价不算折扣", []float64{0, 0}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := qssSetupDB(t)
			rowID := qsdDraftDisc(t, db, tc.discs...)
			p := qssSvc(t, db)
			qsdGateThreshold(p, 0)
			ctx := context.Background()

			low := qsdPassFirstGate(t, db, p, ctx, rowID)
			res := qssMustSend(t, p, ctx, qssSendWith(rowID, low))

			if tc.wantHigh && res.Disposition != QuoteSendAwaiting {
				t.Fatalf("折扣 %v：处置=%s，期望 %s", tc.discs, res.Disposition, QuoteSendAwaiting)
			}
			if !tc.wantHigh && res.Disposition != QuoteSendSent {
				t.Fatalf("折扣 %v：处置=%s，期望 %s（原价报价不该被第二道门拦下）",
					tc.discs, res.Disposition, QuoteSendSent)
			}
		})
	}
}

// TestQuoteDiscount_AutoApproveAsksBothGates 自动放行策略必须**两道门各问一次**。
//
// 这是 AC② 在"同步退化态"那一档下的形态：策略能放行低档不代表它能替公司答"这个折扣可以给"。
// 判据落在两处：策略被问到的 policy_key 序列，以及"只批低档"时那一版仍然留在草稿、一条都没出去。
func TestQuoteDiscount_AutoApproveAsksBothGates(t *testing.T) {
	cases := []struct {
		name      string
		allow     func(policyKey string) bool
		wantSent  bool
		wantAsked []string
	}{
		{
			name:      "两道都自动放行_原地发出去",
			allow:     func(string) bool { return true },
			wantSent:  true,
			wantAsked: []string{QuoteSendPolicyKey, QuoteDiscountPolicyKey},
		},
		{
			name:      "只放行低档_停在第二道门",
			allow:     func(k string) bool { return k == QuoteSendPolicyKey },
			wantSent:  false,
			wantAsked: []string{QuoteSendPolicyKey, QuoteDiscountPolicyKey},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := qssSetupDB(t)
			rowID := qsdDraftDisc(t, db, 40) // 默认阈值 15，稳稳落进高档
			pol := &qsdPolicy{allow: tc.allow}
			p := qssSvcWithPolicy(t, db, pol)
			ctx := context.Background()

			res, err := p.svc.Send(ctx, qssSendInput(rowID))
			if err != nil {
				t.Fatalf("Send 失败：%v", err)
			}
			if got := fmt.Sprint(pol.asked); got != fmt.Sprint(tc.wantAsked) {
				t.Errorf("策略被问的 policy_key 序列=%s，期望 %s", got, tc.wantAsked)
			}
			if tc.wantSent {
				if res.Disposition != QuoteSendSent || p.reach.calls != 1 {
					t.Fatalf("处置=%s 外发=%d，期望 %s 且外发一次",
						res.Disposition, p.reach.calls, QuoteSendSent)
				}
				return
			}
			if res.Disposition != QuoteSendAwaiting || p.reach.calls != 0 {
				t.Fatalf("处置=%s 外发=%d，期望 %s 且不外发",
					res.Disposition, p.reach.calls, QuoteSendAwaiting)
			}
			if got := qssStatus(t, db, rowID); got != model.QuoteStatusDraft {
				t.Errorf("状态=%s，期望 %s", got, model.QuoteStatusDraft)
			}
			if got := qssApprovalRow(t, db, res.ApprovalID).PolicyKey; got != QuoteDiscountPolicyKey {
				t.Errorf("留下的待办 policy_key=%s，期望 %s", got, QuoteDiscountPolicyKey)
			}
		})
	}
}

// TestQuoteDiscount_OpenApprovalFollowsOpenGate 开放待办的读口要跟着门序走。
//
// 低档批掉、高档刚开出来的那个窗口里，OpenApproval 若只查 quote.send 会回"没有开放审批"，
// 而调用方手里那条审批号正是丢失的那一格 —— 那时它唯一能做的就是重开一条待办（文件头写过：
// 少带号的后果是多一条待办，但那不该由读口的失明造成）。
func TestQuoteDiscount_OpenApprovalFollowsOpenGate(t *testing.T) {
	db := qssSetupDB(t)
	rowID := qsdDraftDisc(t, db, 25)
	p := qssSvc(t, db)
	ctx := context.Background()

	if appr, err := p.svc.OpenApproval(ctx, rowID); err != nil || appr != nil {
		t.Fatalf("一次都没点过时读到 %+v / %v，期望 nil / nil", appr, err)
	}

	first := qssMustSend(t, p, ctx, qssSendInput(rowID))
	open, err := p.svc.OpenApproval(ctx, rowID)
	if err != nil || open == nil || open.ID != first.ApprovalID {
		t.Fatalf("低档待办开着时读到 %+v / %v，期望那条 %s", open, err, first.ApprovalID)
	}
	if open.PolicyKey != QuoteSendPolicyKey {
		t.Fatalf("读到的那条落在 %s，期望 %s", open.PolicyKey, QuoteSendPolicyKey)
	}

	qssApprove(t, p, first.ApprovalID)
	second := qssMustSend(t, p, ctx, qssSendWith(rowID, first.ApprovalID))
	open, err = p.svc.OpenApproval(ctx, rowID)
	if err != nil || open == nil {
		t.Fatalf("高档待办开着时读取出错：%+v / %v", open, err)
	}
	if open.ID != second.ApprovalID {
		t.Errorf("读到的是 %s，期望当前开着的那条高档待办 %s", open.ID, second.ApprovalID)
	}
	if open.PolicyKey != QuoteDiscountPolicyKey {
		t.Errorf("policy_key=%s，期望 %s", open.PolicyKey, QuoteDiscountPolicyKey)
	}
}

// TestQuoteDiscount_RejectionAtSecondGateKeepsDraft 第二道门被拒时，那一版留在草稿、不出域，
// 且低档那条批准**不会**在重试时被当成足够的结论。
//
// 单独一格是因为这里有一个真实的误判面：库里同时存在"已批的低档"与"被拒的高档"，
// 任何"读最新一条已裁决"的实现都会挑到两者之一，而挑错那一边的方向是把该拦的放出去。
func TestQuoteDiscount_RejectionAtSecondGateKeepsDraft(t *testing.T) {
	db := qssSetupDB(t)
	rowID := qsdDraftDisc(t, db, 50)
	p := qssSvc(t, db)
	ctx := context.Background()

	low := qsdPassFirstGate(t, db, p, ctx, rowID)
	second := qssMustSend(t, p, ctx, qssSendWith(rowID, low))
	if _, err := p.appro.Decide(ctx, second.ApprovalID, ApprovalReject, "boss_a", "这个价不能报"); err != nil {
		t.Fatalf("裁决失败：%v", err)
	}

	res, err := p.svc.Send(ctx, qssSendWith(rowID, second.ApprovalID))
	if err != nil {
		t.Fatalf("带被拒结论的发送应回处置而不是错误：%v", err)
	}
	if res.Disposition != QuoteSendRejected {
		t.Fatalf("处置=%s，期望 %s", res.Disposition, QuoteSendRejected)
	}
	if p.reach.calls != 0 {
		t.Errorf("外发计数=%d，期望 0", p.reach.calls)
	}
	if got := qssStatus(t, db, rowID); got != model.QuoteStatusDraft {
		t.Errorf("状态=%s，期望 %s", got, model.QuoteStatusDraft)
	}

	// 重开一次：拿低档那条已批的号再来，只能重新开出一条高档待办，不能派发。
	again := qssMustSend(t, p, ctx, qssSendWith(rowID, low))
	if again.Disposition != QuoteSendAwaiting {
		t.Fatalf("拿低档批准重试的处置=%s，期望 %s", again.Disposition, QuoteSendAwaiting)
	}
	if p.reach.calls != 0 {
		t.Errorf("重试后外发计数=%d，期望 0", p.reach.calls)
	}
}

// TestQuoteDiscount_PolicyKeyNamesAreTheDocumentedOnes 两道门的键名钉在卡面与注释里的那两个串。
//
// 名字漂了不会有任何用例会红（两道门各自自洽），但待办中心、审批池的筛选与
// model/approval_request.go:34 那句举例会全部指不到东西上 —— 所以按字符串本身判。
func TestQuoteDiscount_PolicyKeyNamesAreTheDocumentedOnes(t *testing.T) {
	if QuoteDiscountPolicyKey != "quote.discount_high" {
		t.Errorf("policy_key=%q，卡面与 approval_request 注释里用的是 quote.discount_high", QuoteDiscountPolicyKey)
	}
	if QuoteDiscountPolicyKey == QuoteSendPolicyKey {
		t.Error("两道门用了同一个 policy_key ⇒ 幂等键合流，批一道等于批两道")
	}
}
