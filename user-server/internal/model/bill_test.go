// bill_test.go T-P7-01 账单模型的标签层与值域层判据（不连库）。
//
// 分工与 quotes / opportunities / approval_requests 三处先例一致：
//   - 本文件拦"标签写错、列名漂了、多带了一列不该在这张表的东西"；
//   - 真库里的列型、索引形状、AutoMigrate 幂等在 internal/pkg/db/bill_migration_test.go
//     （internal/model 引不动 testutil：testutil → internal/pkg/db → internal/model 成环）。
//
// 本卡两条 AC 在字段层的落点：
//
//	AC①「账单由已成交报价自动派生」
//	   → quote_row_id 上**带不带唯一索引**就是这条 AC 的物理形态：
//	     没有它，同一版报价被点两次"客户已接受"就静默多出两张应收；
//	     有它，第二张在库里插不进去，派生必须走"复用已有那张"这条路。
//	AC②「金额与报价合计对账一致」
//	   → 账单带 amount 而**不带**任何"已收/已付"列：应收的唯一算法是
//	     Σ 那一版的行净额（quoteSumAmount），而"已收"是 payments 求和的结果
//	     （T-P7-02 的家）。两处各存一份必漂，与"报价头不存合计"是同一条判据的第二次应用。
package model

import (
	"reflect"
	"strings"
	"testing"

	"gorm.io/gorm/schema"
)

// TestBillStatusVocabulary status 值域：四格，且**逾期不在里面**。
//
// 逾期是"查询时刻 due_at 与今天的比较"，不是一次状态跃迁：把它做成一格，
// 就需要有人每天把过期的行 UPDATE 一遍，而那天 cron 没跑 ⇒ 全部账单永远不逾期，
// 且库里看起来完全正常（催收侧读到的是"没有逾期"这个假事实）。
// 因此 T-P7-03 的扫描判据是 status IN (open, partial) ∧ due_at < now，
// 而 voided 是人工收口（开错了、或客户退单后不再主张），不参与扫描。
func TestBillStatusVocabulary(t *testing.T) {
	want := []string{"open", "partial", "paid", "voided"}
	if !reflect.DeepEqual(BillStatuses, want) {
		t.Fatalf("状态值域漂移：期望 %v，实际 %v（加/改一格要先改卡面 AC 与 P7-02/03/04 的跃迁表，不能顺手）", want, BillStatuses)
	}
	for _, s := range BillStatuses {
		if !BillStatusKnown(s) {
			t.Errorf("%q 在值域里却不被 BillStatusKnown 认：两判据分家", s)
		}
	}
	// 未知值回 false，且**不许把报价与商机的词认成账单状态**：三个域各有一套生命周期词，
	// 混用的后果不是报错而是"读成另一个域的中间态"。
	for _, bad := range []string{"", " open", "OPEN", "Overdue", "overdue", "unpaid",
		"pending", "draft", "sent", "accepted", "rejected", "expired", "won", "lost", "paid_partial"} {
		if BillStatusKnown(bad) {
			t.Errorf("未知状态 %q 被认成合法：值域校验失守", bad)
		}
	}
}

// TestBillSchemaShape 卡面列清单逐字钉住（多一列少一列都红）。
//
// 卡面写的是 bill(bill_id, quote_id, opportunity_id, amount, due_at, status)；
// 本表在卡面之外只多三格，每格都有下游消费者：
//   - quote_row_id：派生的幂等键（AC①），卡面的 quote_id 是逻辑号、跨版本重复出现，
//     拿它当幂等键等于"一张报价单只能开一张账单"，而客户可以接受过 v1 又接受 v2；
//   - currency：金额不带币种不可算（与 quotes.currency 同判据，且账单是给客户看的那张纸）；
//   - created_at / updated_at：账龄与"这张单多久没动过"的读法。
func TestBillSchemaShape(t *testing.T) {
	st := reflect.TypeOf(Bill{})
	wantFields := []string{
		"ID", "QuoteID", "QuoteRowID", "OpportunityID", "Amount",
		"Currency", "DueAt", "Status", "CreatedAt", "UpdatedAt",
	}
	for _, name := range wantFields {
		if _, ok := st.FieldByName(name); !ok {
			t.Errorf("字段 %s 不存在（卡面列清单/仓储写集合会跟着漂）", name)
		}
	}
	if st.NumField() != len(wantFields) {
		t.Errorf("字段数 %d ≠ 声明的 %d：多了的列没人声明、少了的列仓储会写空", st.NumField(), len(wantFields))
	}
	if got := (Bill{}).TableName(); got != "bills" {
		t.Fatalf("表名 %q，期望 bills", got)
	}

	// 主键是**自生成的字符串**而不是自增 uint：与 quotes.id 同型，理由也同 ——
	// 这把键会被抄进 payments.bill_id（T-P7-02），自增号在多实例下会撞。
	if kindOf(t, st, "ID") != reflect.String {
		t.Errorf("ID 应为字符串行键，实际 %v", kindOf(t, st, "ID"))
	}
	if tag := gormTagOf(t, st, "ID"); !strings.Contains(tag, "primaryKey") {
		t.Errorf("ID 缺 primaryKey 标签：%s", tag)
	}
	// 金额与报价行净额同型同宽（numeric(14,2)）：对账一致这条 AC 要求两边
	// 在库里就是同一个量程，"账单列宽 12 而报价行 14"会在大额那一单才炸。
	if tag := gormTagOf(t, st, "Amount"); !strings.Contains(tag, "numeric(14,2)") {
		t.Errorf("amount 应为 numeric(14,2)（与 quote_line_items.amount 同型）：%s", tag)
	}
	if kindOf(t, st, "Amount") != reflect.Float64 {
		t.Errorf("Amount 应为 float64（与报价行同型，两侧同一个舍入算法），实际 %v", kindOf(t, st, "Amount"))
	}
	// 账期可空：卡面没给账期来源（报价模板只有 valid_days = 报价有效期，不是付款条件），
	// 凭空补一个"默认 30 天"会被运营读成合同条款。空值的后果必须点名：
	// 不参与逾期扫描（P7-03），而不是"永远不过期"这种静默语义。
	if kindOf(t, st, "DueAt") != reflect.Ptr {
		t.Error("DueAt 应可空：零值时间(0001-01-01) 会被逾期扫描读成「早就过期了几千年」")
	}
	for _, name := range []string{"QuoteID", "QuoteRowID", "OpportunityID", "Status", "Currency"} {
		if kindOf(t, st, name) == reflect.Ptr {
			t.Errorf("%s 一是指针：会出现既非合法值又非空串的行", name)
		}
	}
	for _, name := range []string{"CreatedAt", "UpdatedAt"} {
		if kindOf(t, st, name) != reflect.Struct {
			t.Errorf("%s 应为 time.Time", name)
		}
	}
}

// TestBillKeyedToExactlyOneQuoteVersion AC① 的物理形态。
func TestBillKeyedToExactlyOneQuoteVersion(t *testing.T) {
	st := reflect.TypeOf(Bill{})

	rowTag := gormTagOf(t, st, "QuoteRowID")
	if uniqueIndexNamed(rowTag) == "" {
		t.Fatal("quote_row_id 没有命名的唯一索引：同一版报价可以被派生出第二张账单而库里插得进去（AC① 失守）")
	}
	if got := uniqueIndexNamed(rowTag); got != "uq_bills_quote_row" {
		t.Errorf("唯一索引名 %q，期望 uq_bills_quote_row（索引名要能被 pg_index 用例点名）", got)
	}
	// 逻辑报价号**不能**唯一：v1 与 v2 共用一个 quote_id，两张账单可以都存在
	// （客户先接 v1、还价后又接 v2 时，v1 那张要靠 voided 收口而不是靠插不进去）。
	if qidTag := gormTagOf(t, st, "QuoteID"); strings.Contains(qidTag, "unique") {
		t.Errorf("quote_id 被建成了唯一索引，版本链当场作废（一张单只能有一版被接受）：%s", qidTag)
	} else if !strings.Contains(qidTag, "index") {
		t.Errorf("quote_id 应有普通索引：「这张报价单开了几张账单」是第一读法：%s", qidTag)
	}
	if tag := gormTagOf(t, st, "OpportunityID"); !strings.Contains(tag, "index") {
		t.Errorf("opportunity_id 应有索引：客户 360 视图与回款看板按商机捞账：%s", tag)
	}
	// status 刻意**不建索引**：本卡没有任何按状态捞的读方（逾期扫描是 P7-03 的查询，
	// 它的形状是 status IN (…) ∧ due_at < now ⇒ 那时按 due_at 建，与 quotes 不预留索引同判据）。
	if tag := gormTagOf(t, st, "Status"); strings.Contains(tag, "index") {
		t.Errorf("status 在本卡就建了索引，而今日零个按状态捞的读方（预留索引与预留列同罪）：%s", tag)
	}
	// due_at 同样不预留：消费者在 P7-03，那条查询今天还不存在。
	if tag := gormTagOf(t, st, "DueAt"); strings.Contains(tag, "index") {
		t.Errorf("due_at 的索引该由它的第一个读方（逾期扫描）带来，不是现在：%s", tag)
	}
}

// TestBillCarriesNoPaymentTotals "已收多少"不落这张表（同一条判据的第二次应用）。
//
// payment 行是回款的事实源（T-P7-02），账单上的已收额只能是它的求和结果。
// 存了这份冗余的后果：一次入账改了 payments 没改 bills，
// 两张表各自"看起来对"，而对账（AC② 的延伸）恰好就是要比这两个数。
func TestBillCarriesNoPaymentTotals(t *testing.T) {
	st := reflect.TypeOf(Bill{})
	for i := 0; i < st.NumField(); i++ {
		col := columnName(st.Field(i))
		for _, banned := range []string{"paid", "payment", "received", "collected", "refunded"} {
			if strings.Contains(col, banned) {
				t.Errorf("bills 带了回款列 %q：已收额的家在 payments，是求和结果不是列（判据见用例注释）", col)
			}
		}
	}
}

// TestBillCarriesNoApprovalState 账单不是审批主体（第三次应用同一判据）。
//
// approval_requests 是审批的唯一事实源；报价发送那一格的审批主体是 quote，
// 账单推送客户属 P7 之后的高危动作（新规划 §风险表：账单推送=高），
// 那时它作为**新的 subject_type** 进审批表，而不是在 bills 上开列。
func TestBillCarriesNoApprovalState(t *testing.T) {
	st := reflect.TypeOf(Bill{})
	for i := 0; i < st.NumField(); i++ {
		col := columnName(st.Field(i))
		if strings.Contains(col, "approval") || strings.Contains(col, "approved") {
			t.Errorf("bills 带了审批列 %q：审批的唯一事实源是 approval_requests", col)
		}
	}
}

// TestBillHasNoTenantColumn X3 禁区（单商户，ADR-014 已 Simplified）：隔离靠物理分库不是列过滤。
func TestBillHasNoTenantColumn(t *testing.T) {
	st := reflect.TypeOf(Bill{})
	tenantish := []string{"tenant", "org_id", "corp", "workspace", "account_id", "namespace", "scope"}
	for i := 0; i < st.NumField(); i++ {
		col := columnName(st.Field(i))
		for _, banned := range tenantish {
			if strings.Contains(col, banned) {
				t.Errorf("bills 的列 %q 含租户味道的 %q：X3 禁区", col, banned)
			}
		}
	}
}

// TestBillKeyColumnWidths 标识列宽度取向：抄得出、比得了。
func TestBillKeyColumnWidths(t *testing.T) {
	st := reflect.TypeOf(Bill{})
	for _, tc := range []struct{ field, want string }{
		{"QuoteID", "varchar(64)"},
		{"OpportunityID", "varchar(64)"},
		{"Status", "varchar(16)"},
		{"Currency", "varchar(3)"},
	} {
		if tag := gormTagOf(t, st, tc.field); !strings.Contains(tag, tc.want) {
			t.Errorf("%s 宽度取向不对（期望 %s）：%s", tc.field, tc.want, tag)
		}
	}
	if tag := gormTagOf(t, st, "Currency"); !strings.Contains(tag, "default:") {
		t.Errorf("currency 必须有 DB 默认值：金额不带币种不可算，且给存量表补列时缺默认会直接失败：%s", tag)
	}
	// 与 quotes 的默认币种同一个字面值：两处各写一份迟早有一边改成 EUR。
	if BillCurrencyDefault != QuoteCurrencyDefault {
		t.Errorf("账单默认币种 %q ≠ 报价默认币种 %q：同一份钱的两张纸不该有两个默认", BillCurrencyDefault, QuoteCurrencyDefault)
	}
	// 引用键与它指向的列同型：quote_row_id 指的是 quotes.id（text）。
	if tag := gormTagOf(t, st, "QuoteRowID"); !strings.Contains(tag, "type:text") {
		t.Errorf("quote_row_id 应与 quotes.id 同型（text）：%s", tag)
	}
}

// TestBillJSONNamesMatchColumns 序列化键名与列名逐一对齐（API 与库不许各说各话）。
//
// 基准**必须**独立于 json 标签：包内那个 columnName(f) 助手在没有 column: 标签时
// 退回 json 标签，拿它比 json 标签就是拿被断言的那一侧当基准 —— 把
// `json:"quote_row_id"` 改成 `json:"quoteRowId"` 之后两边一起漂，用例照样绿
// （变异格 K08 实测抓到的正是这条：一条看着在断言、其实看不见被测性质的锁）。
func TestBillJSONNamesMatchColumns(t *testing.T) {
	st := reflect.TypeOf(Bill{})
	table := Bill{}.TableName()
	for i := 0; i < st.NumField(); i++ {
		f := st.Field(i)
		col := billColumnName(f, table)
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			t.Errorf("Bill.%s 没有 json 键：出参会变成 Go 字段名驼峰，与列名两套", f.Name)
			continue
		}
		if name != col {
			t.Errorf("Bill.%s 的 json 键 %q ≠ 列名 %q", f.Name, name, col)
		}
	}
}

// billColumnName 算出 AutoMigrate 会把这一列落成什么名字：
// 有显式 column: 标签就用它，否则走 GORM 自己的命名策略（建表时调的就是 ColumnName，
// 连表名一起传进去，因为它的单复数与缩写处理要看上下文）。
func billColumnName(f reflect.StructField, table string) string {
	if tag := f.Tag.Get("gorm"); tag != "" {
		for _, part := range strings.Split(tag, ";") {
			if strings.HasPrefix(part, "column:") {
				return strings.TrimPrefix(part, "column:")
			}
		}
	}
	return schema.NamingStrategy{}.ColumnName(table, f.Name)
}

// TestBillStatusTransitionsAreDeclared 跃迁表在模型层声明、在服务层执行（P7-02/04 消费）。
//
// 判据不是"表存在"，而是这几条具体的边在不在。本表在 T-P7-02 被**放宽过一次**，
// 放宽的方向与理由要一起留在这里，否则下一个人会以为这是随手加的：
//
//	T-P7-01 交付时写死了"paid 是终态（出边为空）"，那句的完整前提是
//	"没有任何一处能算出已收多少" —— 那时 payments 还不存在，"改一下 paid"
//	只能凭人手，而人手改凭证正是那句注释要挡的东西。
//	T-P7-02 把结清判据实现成 Σ 计入结清的回款行，于是 status 成了那个和的**函数**：
//	一笔钱被渠道冲销之后，欠额回来了而账单还写着已结清，那才是"两头对不上"。
//	所以回退边是这张表现在必需的，而"改一格要走 CAS"这件事仍然成立。
//
// 仍然禁的两侧：voided 无路可回（作废是收口，要重开就派生新的一张，留下两张说过程），
// paid→voided 不许（结清过的应收要作废，等于用状态盖掉一次真实的收付历史）。
func TestBillStatusTransitionsAreDeclared(t *testing.T) {
	want := []struct {
		from, to string
		ok       bool
	}{
		{BillStatusOpen, BillStatusPartial, true},
		{BillStatusOpen, BillStatusPaid, true},
		{BillStatusPartial, BillStatusPaid, true},
		{BillStatusOpen, BillStatusVoided, true},
		{BillStatusPartial, BillStatusVoided, true},
		{BillStatusPaid, BillStatusVoided, false},    // 结清的账单不许事后作废
		{BillStatusVoided, BillStatusOpen, false},    // 作废不可逆
		{BillStatusVoided, BillStatusPaid, false},    // 作废不可逆
		{BillStatusVoided, BillStatusPartial, false}, // 作废不可逆
		// —— 回款冲销要能把钱退回到"还欠着"：下面三条边由 T-P7-02 开 ——
		{BillStatusPaid, BillStatusPartial, true}, // 冲掉一部分
		{BillStatusPartial, BillStatusOpen, true}, // 冲到一笔不剩
		{BillStatusPaid, BillStatusOpen, true},    // 全额冲销（同一笔钱来过又全走了）
	}
	for _, tc := range want {
		if got := BillStatusCanTransit(tc.from, tc.to); got != tc.ok {
			t.Errorf("跃迁 %s→%s 应为 %v，实际 %v", tc.from, tc.to, tc.ok, got)
		}
	}
	// voided 是唯一终态：出边为空。
	if n := len(BillStatusNext(BillStatusVoided)); n != 0 {
		t.Errorf("voided 是终态却有 %d 条出边：终态有出边等于没有终态", n)
	}
	// paid 不再要求出边为空（见上面的放宽理由），但它**不许自环**：
	// 零位移跃迁在仓储层就被拒，这里若放行为是两张脸。
	if BillStatusCanTransit(BillStatusPaid, BillStatusPaid) {
		t.Error("paid→paid 被认成合法跃迁")
	}
}
