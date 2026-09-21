// quote_test.go T-P6-01 报价模型（版本链）的标签层与值域层判据。
//
// 分工与同域先例（opportunity_test.go / approval_request_test.go）一致：
//   - 本文件拦"标签写错、列名漂了、多带了一列不该在这张表的东西"（不连库）；
//   - 真库里的列型、索引形状、AutoMigrate 幂等在 internal/pkg/db/quote_migration_test.go
//     （internal/model 引不动 testutil：testutil → internal/pkg/db → internal/model 成环）。
//
// 本卡两条 AC 在字段层的落点：
//
//	AC①「同一 quote_id 多版本共存、版本单调递增、旧版不可变」
//	   → (quote_id, version) 上**带不带唯一索引**就是这条 AC 的物理形态：
//	     没有它，"版本"只是一个谁都能写歪的整数；有它，重复版本在库里插不进去。
//	AC②「客户还价→新版本由系统生成而非覆盖」
//	   → source_id 一列：新版本必须能说出自己是**从哪一版长出来的**。
//	     只有 version 没有 source_id 的话，"链"退化成"序列"，
//	     而 LTC-12 要的是可回溯的谈判过程（v3 是不是真的接在 v2 之后）。
package model

import (
	"reflect"
	"strings"
	"testing"
)

// TestQuoteStatusVocabulary status 的值域：五格，各自语义独立，且不许混进金额或阶段词。
//
// 逐字比切片（顺序是生命周期顺序：draft→sent 之后才谈得上 accepted/rejected/expired），
// 但**不**提供 StatusIndex —— 报价的跃迁合法性由 T-P6-03 的检查点判，
// 那判据要连审批状态一起看，抄在这层只会分家。
func TestQuoteStatusVocabulary(t *testing.T) {
	want := []string{"draft", "sent", "accepted", "rejected", "expired"}
	if !reflect.DeepEqual(QuoteStatuses, want) {
		t.Fatalf("状态值域漂移：期望 %v，实际 %v（加/改一格要先改卡面 AC 与 P6-03 的跃迁表，不能顺手）", want, QuoteStatuses)
	}
	for _, s := range QuoteStatuses {
		if !QuoteStatusKnown(s) {
			t.Errorf("%q 在值域里却不被 QuoteStatusKnown 认：两判据分家", s)
		}
	}
	// 未知值必须回 false 而不是"近似认成某个值"：大小写与空格是运营手填的常态，
	// 把它们规范化掉等于让"DRAFT"与"draft"变成同一个状态，而审批侧看到的是两种人。
	for _, bad := range []string{"", " unknown", "DRAFT", "Draft", "won", "lost", "cancelled"} {
		if QuoteStatusKnown(bad) {
			t.Errorf("未知状态 %q 被认成合法：值域校验失守", bad)
		}
	}
	// 报价不许自带成交终局：accepted 是"客户接了这版报价"，
	// 回款与关单在商机（OpportunityStatusWon/Lost）与账单域（P7）各有事实源。
	// 混进来的后果与 T-P4-01 拦 stage 那条同形：两张表各算一遍赢单率，数字迟早不等。
	for _, s := range QuoteStatuses {
		for _, banned := range []string{"won", "lost", "paid", "order", "invoice"} {
			if strings.Contains(s, banned) {
				t.Errorf("status %q 含越界词 %q：报价只到「客户点头」为止", s, banned)
			}
		}
	}
}

// TestQuoteSchemaShape 卡面列清单逐字钉住（多一列少一列都红）。
func TestQuoteSchemaShape(t *testing.T) {
	st := reflect.TypeOf(Quote{})
	wantFields := []string{
		"ID", "QuoteID", "OpportunityID", "Version", "Status", "SourceID",
		"ValidUntil", "Currency", "CreatedAt", "UpdatedAt",
	}
	for _, name := range wantFields {
		if _, ok := st.FieldByName(name); !ok {
			t.Errorf("字段 %s 不存在（卡面列清单/仓储写集合会跟着漂）", name)
		}
	}
	if st.NumField() != len(wantFields) {
		t.Errorf("字段数 %d ≠ 卡面 %d：多了的列没人声明、少了的列仓储会写空", st.NumField(), len(wantFields))
	}
	if got := (Quote{}).TableName(); got != "quotes" {
		t.Fatalf("表名 %q，期望 quotes", got)
	}

	// 版本是**有符号 int64**且 not null + default 成对（与 opportunities.version 同标签形状，
	// 但理由不同，别照抄注释）：商机那一列是 CAS 计数器，本列是链上的固定位置、一生只写一次。
	//   - 不用 uint：无符号的"回绕"是一个合法值，一旦回绕，两个不同版本会相等，
	//     而 (quote_id, version) 唯一索引正是 AC① 的唯一硬保证 —— 判据不能给回绕留位置。
	//   - not null：PG 里 NULL 互不相等，允许 NULL 版本等于让同一版能插两行，AC① 当场作废。
	//   - default：给**存量表**补列时 PG 要求可填值，缺它 AutoMigrate 直接失败
	//     （真库那侧的补列用例在 internal/pkg/db/quote_migration_test.go）。
	if kindOf(t, st, "Version") != reflect.Int64 {
		t.Errorf("Version 应为 int64，实际 %v", kindOf(t, st, "Version"))
	}
	if tag := gormTagOf(t, st, "Version"); !strings.Contains(tag, "not null") || !strings.Contains(tag, "default:") {
		t.Errorf("version 缺 `not null` + `default:` 这一对：%s", tag)
	}
	for _, name := range []string{"QuoteID", "OpportunityID", "Status", "Currency"} {
		if kindOf(t, st, name) == reflect.Ptr {
			t.Errorf("%s 一是指针：会出现既非合法值又非空串的行", name)
		}
	}
	// 有效期可空是语义而非偷懒：不设有效期是合法报价，
	// 零值时间(0001-01-01) 会被"过期报价"的聚合读成"早就过期了"（与商机 expected_close_at 同判据）。
	if kindOf(t, st, "ValidUntil") != reflect.Ptr {
		t.Error("ValidUntil 应可空：零值时间无法区分「没设有效期」和「公元 1 年就过期」")
	}
	for _, name := range []string{"CreatedAt", "UpdatedAt"} {
		if kindOf(t, st, name) != reflect.Struct {
			t.Errorf("%s 应为 time.Time", name)
		}
	}
}

// TestQuoteVersionChainShape AC①②在标签上的形状，逐条都是"少了就能静默写歪"的那种。
func TestQuoteVersionChainShape(t *testing.T) {
	st := reflect.TypeOf(Quote{})

	// (quote_id, version) 复合唯一索引：AC① 的唯一硬保证。
	// 判据要同时看到"同一个索引名"与"两列都在里面"，所以逐字段读标签比字符串。
	qidTag := gormTagOf(t, st, "QuoteID")
	verTag := gormTagOf(t, st, "Version")
	name := uniqueIndexNamed(qidTag)
	if name == "" {
		t.Fatal("quote_id 没有命名的唯一索引：不命名就无法与 version 组成复合键（GORM 会各建一个单列索引）")
	}
	if !strings.Contains(verTag, "uniqueIndex:"+name) {
		t.Errorf("version 没进 %s：单列唯一会变成「一个版本号全站只能用一次」，两版报价同号插不进第二行", name)
	}
	if strings.Contains(qidTag, "uniqueIndex\"") || strings.HasSuffix(strings.TrimSpace(qidTag), "uniqueIndex") {
		t.Error("quote_id 上不许另有单列唯一索引：同一报价的第二版本身就该重复它")
	}

	// 链的来路：source_id 空串 = 第一版（不是"未知"，所以它必须可存空且不带 NOT NULL）。
	srcTag := gormTagOf(t, st, "SourceID")
	if !strings.Contains(srcTag, "type:text") {
		t.Errorf("source_id 应与 quotes.id 同型（text）：%s", srcTag)
	}
	if !strings.Contains(srcTag, "index") {
		t.Errorf("source_id 应有索引：反查「这一版被谁改走了」是谈判过程的唯一读法，没索引等于没打算让人查：%s", srcTag)
	}
	// 行键本身：主键必须是 text（与商机/订单草稿同一先例 —— 它会被抄进
	// quote_line_items.quote_row_id 与后续 bill 的引用列，自增 serial 会随库迁移漂号）。
	if tag := gormTagOf(t, st, "ID"); !(strings.Contains(tag, "type:text") && strings.Contains(tag, "primaryKey")) {
		t.Errorf("quotes.id 应为 text 主键：%s", tag)
	}
}

// TestQuoteCarriesNoTotals 合计只有一处事实源：行项目。
//
// 卡面 AC③（T-P6-02）要的是"行项目金额合计与落库一致"。一旦报价头上存一份
// total_amount，就有两种漂法：改了行项目忘了改头（报表读到旧数）、
// 或改了头而行项目没动（合计与明细不符，而**两边都能自证清白**）。
// 所以现在就把这列禁在 schema 里，P6-02 只能从明细算 —— 这不是省事，是消灭第二个事实源。
func TestQuoteCarriesNoTotals(t *testing.T) {
	st := reflect.TypeOf(Quote{})
	for i := 0; i < st.NumField(); i++ {
		col := columnName(st.Field(i))
		for _, banned := range []string{"total", "subtotal", "amount", "sum", "discount"} {
			if col == banned || strings.HasPrefix(col, banned+"_") || strings.HasSuffix(col, "_"+banned) {
				t.Errorf("quotes 带了金额列 %q：合计的家在 quote_line_items，存两份必漂（判据见本用例注释）", col)
			}
		}
	}
}

// TestQuoteCarriesNoApprovalState 审批状态不落这张表（同一句判据的第二次应用）。
//
// approval_requests 已经是审批的唯一事实源（T-P3-01），本卡 AC② 的"必经审批"属 P6-03。
// 若报价自己存一份 approval_status/approved_by，就有两处说"批过了"，
// 而撤销审批只会改其中一处 —— 那正是 G13 补盲时钉过的形状。
func TestQuoteCarriesNoApprovalState(t *testing.T) {
	st := reflect.TypeOf(Quote{})
	for i := 0; i < st.NumField(); i++ {
		col := columnName(st.Field(i))
		if strings.Contains(col, "approval") || strings.Contains(col, "approved") {
			t.Errorf("quotes 带了审批列 %q：审批的唯一事实源是 approval_requests", col)
		}
	}
}

// TestQuoteHasNoTenantColumn X3 禁区（单商户，ADR-014 已 Simplified）：隔离靠物理分库不是列过滤。
func TestQuoteHasNoTenantColumn(t *testing.T) {
	for _, st := range []reflect.Type{reflect.TypeOf(Quote{}), reflect.TypeOf(QuoteLineItem{})} {
		tenantish := []string{"tenant", "org_id", "corp", "workspace", "account_id", "namespace", "scope"}
		for i := 0; i < st.NumField(); i++ {
			col := columnName(st.Field(i))
			for _, banned := range tenantish {
				if strings.Contains(col, banned) {
					t.Errorf("%s 的列 %q 含租户味道的 %q：X3 禁区", st.Name(), col, banned)
				}
			}
		}
	}
}

// TestQuoteKeyColumnWidths 标识列宽度由上下游决定，不是随手定的。
//
//   - quote_id / opportunity_id：与 opportunities.id 的引用宽度对齐，且 opportunity_id
//     会被原样抄进 sales_events（那里 varchar(64)）。本表比它宽 ⇒ 事件写入那一步才炸，
//     而且是 CreateInBatches 整批回滚（T-P1-08 在 tool_call_audits.trace_id 上的同形缺陷）。
//   - status：值域可枚举 ⇒ 定宽 varchar(16)，与 opportunities.status 同宽。
//   - currency：ISO 4217 三位码，varchar(3) + DB 默认值（金额不带币种不可算）。
func TestQuoteKeyColumnWidths(t *testing.T) {
	st := reflect.TypeOf(Quote{})
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
		t.Errorf("currency 必须有 DB 默认值：金额不带币种不可算，且存量表补列时 NOT NULL 无默认会直接失败：%s", tag)
	}
	// opportunity_id 是查询维度（"这个商机报过几版"）⇒ 建索引；source_id 同理（见上）。
	if tag := gormTagOf(t, st, "OpportunityID"); !strings.Contains(tag, "index") {
		t.Errorf("opportunity_id 应有索引：报价的第一读法是按商机列出它的版本链：%s", tag)
	}
}

// TestQuoteJSONNamesMatchColumns 序列化键名与列名逐一对齐（API 与库不许各说各话）。
func TestQuoteJSONNamesMatchColumns(t *testing.T) {
	for _, st := range []reflect.Type{reflect.TypeOf(Quote{}), reflect.TypeOf(QuoteLineItem{})} {
		for i := 0; i < st.NumField(); i++ {
			f := st.Field(i)
			col := columnName(f)
			jsonTag := f.Tag.Get("json")
			name := strings.Split(jsonTag, ",")[0]
			if name == "-" {
				continue
			}
			if name == "" {
				t.Errorf("%s.%s 没有 json 键：出参会变成 Go 字段名驼峰，与列名两套", st.Name(), f.Name)
				continue
			}
			if name != col {
				t.Errorf("%s.%s 的 json 键 %q ≠ 列名 %q", st.Name(), f.Name, name, col)
			}
		}
	}
}

// TestQuoteLineItemShape 行项目：身份是"哪一版的第几行"，不是自己有一个号。
//
// 复合主键 (quote_row_id, line_no) 是"旧版不可变"的物理化：
// 一行明细**只能**属于某个具体版本行，改价就必须是新的一版 —— 想原地改都指不到它。
// 反面对照：给行项目一个 surrogate id，P6-02/03 迟早会有人写着"更新第 3 行"，
// 而 v1 与 v3 的第 3 行在数据库里是同一行可更新的记录，谈判历史当场蒸发。
func TestQuoteLineItemShape(t *testing.T) {
	st := reflect.TypeOf(QuoteLineItem{})
	wantFields := []string{
		"QuoteRowID", "LineNo", "ProductID", "Title", "Quantity",
		"UnitPrice", "DiscountPercent", "Amount", "CreatedAt",
	}
	for _, name := range wantFields {
		if _, ok := st.FieldByName(name); !ok {
			t.Errorf("字段 %s 不存在", name)
		}
	}
	if st.NumField() != len(wantFields) {
		t.Errorf("字段数 %d ≠ 卡面 %d", st.NumField(), len(wantFields))
	}
	if got := (QuoteLineItem{}).TableName(); got != "quote_line_items" {
		t.Fatalf("表名 %q，期望 quote_line_items", got)
	}
	// 两个主键列都要标 primaryKey，且不许有第三个 surrogate id。
	for _, name := range []string{"QuoteRowID", "LineNo"} {
		if tag := gormTagOf(t, st, name); !strings.Contains(tag, "primaryKey") {
			t.Errorf("%s 应是复合主键的一部分：%s", name, tag)
		}
	}
	// 引用的是版本行的 id 而不是 quote_id：后者跨版本共享，
	// 拿它当键会把 v1 的明细读进 v2（"两版报价共用行项目"是一种看起来很合理的错）。
	if tag := gormTagOf(t, st, "QuoteRowID"); !strings.Contains(tag, "type:text") {
		t.Errorf("quote_row_id 应与 quotes.id 同型（text）：%s", tag)
	}
	// 金额三列全 numeric；float 存钱会在合计里攒出二进制误差（P6-02 AC③ 正是合计）。
	for _, tc := range []struct{ field, scale string }{
		{"Quantity", "numeric(12,2)"},
		{"UnitPrice", "numeric(14,2)"},
		{"DiscountPercent", "numeric(5,2)"},
		{"Amount", "numeric(14,2)"},
	} {
		tag := gormTagOf(t, st, tc.field)
		if !strings.Contains(tag, "type:"+tc.scale) {
			t.Errorf("%s 列型不是 %s：%s（量程取向：单价/行额与订单草稿的 numeric(14,2) 同宽，折扣是百分比故 5,2）", tc.field, tc.scale, tag)
		}
		for _, bad := range []string{"double precision", "real", "float"} {
			if strings.Contains(tag, bad) {
				t.Errorf("%s 标签里出现 %s：%s", tc.field, bad, tag)
			}
		}
	}
	// 行项目不带币种：一张报价一个币种，逐行各存一份的话"混币报价"就成了合法形状，
	// 而合计（P6-02 AC③）在混币下没有定义。
	for i := 0; i < st.NumField(); i++ {
		if col := columnName(st.Field(i)); col == "currency" {
			t.Error("quote_line_items 不该有 currency 列：币种在报价头上，逐行各存一份会让合计失去定义")
		}
	}
	// product_id 引用商品目录（rag_products.id 是 varchar(64)/size:64），
	// 但**必须同时存 title 快照**：目录会改名改价，报价是要能事后重放的凭证。
	if tag := gormTagOf(t, st, "ProductID"); !strings.Contains(tag, "varchar(64)") {
		t.Errorf("product_id 应与 rag_products.id 同宽（64）：%s", tag)
	}
	if tag := gormTagOf(t, st, "Title"); !strings.Contains(tag, "type:text") {
		t.Errorf("title 应是 text 快照（不随目录改名而变）：%s", tag)
	}
}

// uniqueIndexNamed 从 gorm 标签里取出**命了名**的唯一索引名（uniqueIndex:name 形状）。
// 匿名 uniqueIndex 不算：两个匿名索引会被 GORM 各建成单列索引，组不成复合键。
func uniqueIndexNamed(tag string) string {
	for _, part := range strings.Split(tag, ";") {
		if strings.HasPrefix(part, "uniqueIndex:") {
			return strings.TrimPrefix(part, "uniqueIndex:")
		}
	}
	return ""
}
