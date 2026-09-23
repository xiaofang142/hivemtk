// bill_chased_test.go T-P7-03：把"哪些账单参与催收"这一格钉成单一事实源。
//
// 为什么单开一个用例而不是并进球值域那条：BillStatusesChased 的失效方向与
// BillStatuses 完全不同 —— 值域少一格是"开不出这种账单"，而催收集少一格是
// "有一类欠钱的人永远不被扫到"，后者不报错、不红、只在三个月后被发现。
// 与 model.PaymentStatusesCounted 同一族（那一格守的是"哪些回款算已收"，这一格守的是
// "哪些欠额算该催"），三条断言各挡一种坏法写在下面。
package model

import (
	"testing"
)

// TestBillChasedStatusesCoverUnsettledOnly 催收集 = 还没收清的那两格，且只那两格。
func TestBillChasedStatusesCoverUnsettledOnly(t *testing.T) {
	want := []string{BillStatusOpen, BillStatusPartial}
	if len(BillStatusesChased) != len(want) {
		t.Fatalf("催收集大小 %d ≠ %d（成员必须逐条点名）", len(BillStatusesChased), len(want))
	}
	for i := range want {
		if BillStatusesChased[i] != want[i] {
			t.Errorf("催收集第 %d 格 = %q，期望 %q（顺序也是契约：SQL 的 IN 列表按它生成）",
				i, BillStatusesChased[i], want[i])
		}
	}
	// paid / voided 必须在集外：已结清的继续催是骚扰，已作废的继续催是向不存在的主张要钱。
	for _, s := range []string{BillStatusPaid, BillStatusVoided} {
		for _, c := range BillStatusesChased {
			if c == s {
				t.Errorf("催收集里有 %s：这张应收已经不再被主张，逾期扫描不该再碰它", s)
			}
		}
	}
}

// TestBillChasedStatusesAreInBillDomain 催收集不得含值域外的字符串。
//
// 这条看起来是废话，但它挡的正是最贵的一种：有人往催收集里塞一个 "overdue"
// （本表刻意没有那一格，理由见 billStatusTransitions 的头注释）。塞进去之后
// SQL 照跑、零命中照回，而"没有任何逾期单"这句话当场变成假话 —— 与 PaymentStatusesCounted
// 那条同源，只是这边的假事实方向是"少催"，谁也看不见。
func TestBillChasedStatusesAreInBillDomain(t *testing.T) {
	for _, c := range BillStatusesChased {
		if !BillStatusKnown(c) {
			t.Errorf("催收集里的 %q 不在账单值域里（库里那一格永远匹配不上它）", c)
		}
	}
}

// TestBillChasedStatusesAreNotMutableBy callers 值域切片被调用方改写的后果要说清。
//
// 这条不测"能不能改"（Go 里包级切片人人可改，测不动），测的是**本包之外没有写入方**：
// 催收集与 BillStatuses / PaymentStatusesCounted 同一条判据 —— 它是被 SQL 直接消费的，
// 任何"顺手补一格"的调用方都必须改这里并过本用例，而不是在自己那处拼一份列表。
func TestBillChasedStatusesAreNotMutatedInThisPackage(t *testing.T) {
	before := append([]string(nil), BillStatusesChased...)
	_ = BillChasedStatusKnown(BillStatusOpen)
	for i := range before {
		if BillStatusesChased[i] != before[i] {
			t.Fatalf("读一次成员就改写了催收集（第 %d 格 %q → %q）", i, before[i], BillStatusesChased[i])
		}
	}
}

// TestBillChasedStatusKnownRejectsUnknown 判定函数不得把未知值放行。
func TestBillChasedStatusKnownRejectsUnknown(t *testing.T) {
	for _, s := range []string{"", "OPEN", "open ", "overdue", BillStatusPaid, "  "} {
		if BillChasedStatusKnown(s) {
			t.Errorf("BillChasedStatusKnown(%q) = true：逐字比、不规范化（与 BillStatusKnown 同一条口径）", s)
		}
	}
	for _, s := range BillStatusesChased {
		if !BillChasedStatusKnown(s) {
			t.Errorf("BillChasedStatusKnown(%q) = false：自己生成的集合里点不出自己的成员", s)
		}
	}
}
