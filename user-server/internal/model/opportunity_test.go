// opportunity_test.go T-P4-01 商机模型的四条 AC。
//
// 本文件只断"标签层与值域层"的事实（不连库）：真库里的列类型、索引形状、
// AutoMigrate 幂等在 internal/pkg/db/opportunity_migration_test.go，
// 理由是 import 成环 —— internal/model 不能引 internal/pkg/testutil
// （testutil → internal/pkg/db → internal/model）。
//
// 与同域先例（approval_request_test.go / human_task_test.go）同一口径：
// 断言对象是**值域的内容**而不是"某个函数没 panic"。字段清单在这里钉一份、
// 在真库那侧钉一份，是因为这两类错法互不包含：
//   - 这里拦"标签写错/列名漂了/多带了一列评分"；
//   - 真库那侧拦"标签写了但 AutoMigrate 没建成 numeric / 建成了 float"。
package model

import (
	"reflect"
	"strings"
	"testing"
)

// TestOpportunityStageVocabulary AC① 的第一半：stage 只装"推进到哪一格"。
//
// 顺序有意义（漏斗按它排），所以逐字比切片而不是集合。
func TestOpportunityStageVocabulary(t *testing.T) {
	want := []string{"qualification", "needs_confirmed", "proposal", "negotiation"}
	if !reflect.DeepEqual(OpportunityStages, want) {
		t.Fatalf("阶段值域漂移：期望 %v，实际 %v（加/改一格须先改 C5/N-1 的裁定，不能顺手）", want, OpportunityStages)
	}
	// 阶段里绝不许出现终局词：一旦出现，"到了报价这一格"与"这单成了"就挤进同一个字段，
	// 漏斗最后一格与赢单率变成同一个数（AC① 要拦的正是这个塌缩）。
	for _, s := range OpportunityStages {
		for _, banned := range []string{"won", "lost", "closed", "canc", "dead"} {
			if strings.Contains(s, banned) {
				t.Errorf("stage %q 含终局词 %q：过程位置与生命周期终局必须分列", s, banned)
			}
		}
	}
	// 也不许含 "opportunity" 本身：那是 ltc.config 里**能力开关**的名字
	// （service.LTCStageOpportunity），不是商机自己会处于的阶段。
	// 两个字面值今天都在仓里，混用的后果是"开了商机能力 ⇒ 所有商机都进入一个叫
	// opportunity 的阶段"，而漏斗侧读到的那一格永远为 0。
	for _, s := range OpportunityStages {
		if s == "opportunity" {
			t.Errorf("stage 里出现了能力开关名 %q：两个词族必须分开（见 service/ltc_config.go 的 LTCKnownStages）", s)
		}
	}
	// StageIndex 是漏斗侧的单调性判据：认得的按序返回，不认得的必须是 -1 而不是 0。
	// 返回 0 会让"拼错的阶段名"被读成"处在第一格 qualification"，凭空给漏斗加一格。
	for i, s := range OpportunityStages {
		if got := OpportunityStageIndex(s); got != i {
			t.Errorf("OpportunityStageIndex(%q)=%d want %d", s, got, i)
		}
	}
	for _, bad := range []string{"", "unknown", "Won", "QUALIFICATION"} {
		if got := OpportunityStageIndex(bad); got != -1 {
			t.Errorf("未知阶段 %q 应返回 -1，实际 %d（0 会被读成第一格）", bad, got)
		}
	}
}

// TestOpportunityStatusVocabulary AC① 的第二半：status 只装"这条还在不在跑"。
func TestOpportunityStatusVocabulary(t *testing.T) {
	want := []string{"open", "won", "lost", "cancelled"}
	if !reflect.DeepEqual(OpportunityStatuses, want) {
		t.Fatalf("状态值域漂移：期望 %v，实际 %v", want, OpportunityStatuses)
	}
	// 两族字面值必须互斥：交集非空 = 同一个字符串既是过程又是终局，
	// 于是 "status=x 且 stage=x" 的行两种解释都给得出，跃迁表怎么写都能自圆其说。
	for _, st := range OpportunityStatuses {
		if OpportunityStageIndex(st) != -1 {
			t.Errorf("状态 %q 同时是阶段：两族词表串味了", st)
		}
	}
	// open 是唯一未收口的状态；其余三个收口。
	if OpportunityClosed(OpportunityStatusOpen) {
		t.Error("open 被判成已收口：跃迁与清扫都会跳过所有在跑的商机")
	}
	for _, s := range []string{OpportunityStatusWon, OpportunityStatusLost, OpportunityStatusCancelled} {
		if !OpportunityClosed(s) {
			t.Errorf("%s 应判为已收口", s)
		}
	}
	// cancelled 不算 outcome（见 OpportunityOutcomes 的注释）：判据错了会把"误建"
	// 计进输单，赢率与丢单归因同时失真。
	if len(OpportunityOutcomes) != 2 {
		t.Errorf("outcome 集合应只含 won/lost（cancelled 不是结果），实际 %v", OpportunityOutcomes)
	}
	for _, o := range OpportunityOutcomes {
		if o == OpportunityStatusCancelled || o == OpportunityStatusOpen {
			t.Errorf("outcome 集合混进了 %q", o)
		}
	}
	// 未知状态一律判"未收口"：判据出错的方向必须是"继续管着这行"，
	// 而不是把一条在跑的商机当成已收口、从此没人再碰它。
	if OpportunityClosed("some_typo") {
		t.Error("未知状态被判成已收口")
	}
}

// TestOpportunityCarriesOnlyWinProbability AC① 的落点判据：C5 三评分命名空间分离。
//
// 商机只带 win_probability（"这个商机能不能成"）。把 lead_score（"这个客户值不值得跟进"）
// 或 confidence（"这次回答对不对"）抄进本表，就是把三个不同的条件概率放进一张表的
// 三个列里，然后自然会有人把它们乘起来算一个"综合分"—— C5 明令禁止的那一步。
func TestOpportunityCarriesOnlyWinProbability(t *testing.T) {
	st := reflect.TypeOf(Opportunity{})
	for _, banned := range []string{"confidence", "lead_score", "churn", "rfm", "intent_score", "score"} {
		for i := 0; i < st.NumField(); i++ {
			f := st.Field(i)
			col := columnName(f)
			if col == banned || strings.HasPrefix(col, banned+"_") {
				t.Errorf("商机表带了评分列 %q：C5 裁定三套评分禁止合并，%s 的家不在这里", col, banned)
			}
		}
	}
	if _, ok := st.FieldByName("WinProbability"); !ok {
		t.Fatal("商机表必须自带 win_probability：它是 C5 第三套评分的唯一落点")
	}
}

// TestOpportunitySchemaShape 卡面列清单逐字钉住（多一列少一列都红）。
//
// 只列"该有的必须有"不够——凭空多一列同样没人发现。故两侧都比：先逐名查存在，
// 再比字段总数。
func TestOpportunitySchemaShape(t *testing.T) {
	st := reflect.TypeOf(Opportunity{})
	wantFields := []string{
		"ID", "Code", "CustomerID", "OneID", "ClueID",
		"Stage", "Status", "Amount", "Currency", "WinProbability",
		"OwnerUserID", "ExpectedCloseAt", "LostReason",
		"CreatedAt", "UpdatedAt",
	}
	for _, name := range wantFields {
		if _, ok := st.FieldByName(name); !ok {
			t.Errorf("字段 %s 不存在（卡面列清单/仓储写集合会跟着漂）", name)
		}
	}
	if st.NumField() != len(wantFields) {
		t.Errorf("字段数 %d ≠ 卡面 %d：多了的列没人声明、少了的列仓储会写空", st.NumField(), len(wantFields))
	}
	if got := (Opportunity{}).TableName(); got != "opportunities" {
		t.Fatalf("表名 %q，期望 opportunities", got)
	}

	// stage/status 不能是指针：NULL 与 '' 会塌成两种"没赋值"的行，
	// 而这两种行都进不了跃迁、也进不了漏斗，等于凭空造孤儿。
	for _, name := range []string{"Stage", "Status", "CustomerID", "OwnerUserID", "Currency"} {
		if kindOf(t, st, name) == reflect.Ptr {
			t.Errorf("%s 一是指针：会出现既非合法值又非空串的行", name)
		}
	}
	// 目标关单日必须可空：未定关单日是常态，零值时间(0001-01-01)会被
	// P8 的"预期回款/逾期"聚合读成"早就过期了"。
	if kindOf(t, st, "ExpectedCloseAt") != reflect.Ptr {
		t.Error("ExpectedCloseAt 应可空：零值时间无法区分「没定关单日」和「公元 1 年就该关单」")
	}
	// 时间戳两列都在（卡面 timestamps）：新建商机数按 created_at 取窗口，
	// updated_at 是"多久没动过"的唯一凭据。
	for _, name := range []string{"CreatedAt", "UpdatedAt"} {
		if kindOf(t, st, name) != reflect.Struct {
			t.Errorf("%s 应为 time.Time", name)
		}
	}
}

// TestOpportunityAmountAndProbabilityAreNumeric AC④。
//
// 断两句：①列型是 numeric(p,s)，不是 double precision / float；
// ②Go 侧仍是 float64（GORM 负责转换，仓内先例 sales_event.Amount 同此）。
// 第二句容易被漏掉：把 Go 类型换成 decimal 库的类型能过第一句，却会让所有
// 既有读这个字段的代码编译不过——那是另一件事，不该由本卡顺手改。
func TestOpportunityAmountAndProbabilityAreNumeric(t *testing.T) {
	st := reflect.TypeOf(Opportunity{})
	for _, tc := range []struct {
		field, scale string
	}{
		{"Amount", "numeric(12,2)"},
		{"WinProbability", "numeric(5,2)"},
	} {
		tag := gormTagOf(t, st, tc.field)
		if !strings.Contains(tag, "type:"+tc.scale) {
			t.Errorf("%s 列型不是 %s：%s（float 存金额/概率会有二进制误差累积，先例见 sales_event.Amount）", tc.field, tc.scale, tag)
		}
		for _, bad := range []string{"double precision", "real", "float"} {
			if strings.Contains(tag, bad) {
				t.Errorf("%s 标签里出现 %s：%s", tc.field, bad, tag)
			}
		}
		if k := kindOf(t, st, tc.field); k != reflect.Float64 {
			t.Errorf("%s 的 Go 类型应为 float64（GORM 自动转换），实际 %v", tc.field, k)
		}
	}
	// 量程对齐 ltc.config：阈值 win_probability 是 0–1 的概率（默认 0.50），
	// 所以本列的语义域同为 0–1。numeric(5,2) 只是容器宽度，不是值域承诺；
	// 把两处量程对上，比较时才不用换算（换算那一步迟早有人在两个地方各算一次）。
	if OpportunityWinProbabilityMax != 1.0 {
		t.Errorf("赢率上限应为 1.0（与 ltc.config 的 win_probability 阈值同量程），实际 %v", OpportunityWinProbabilityMax)
	}
}

// TestOpportunityHasNoTenantColumn AC②：X3 禁区（单商户模式，ADR-014 已 Simplified）。
//
// 标签层的判据；真库层那一份在 internal/pkg/db（按 information_schema 再判一次，
// 防"结构体没有但库里残留"）。
func TestOpportunityHasNoTenantColumn(t *testing.T) {
	st := reflect.TypeOf(Opportunity{})
	tenantish := []string{"tenant", "org_id", "corp", "workspace", "account_id", "namespace", "scope"}
	for i := 0; i < st.NumField(); i++ {
		col := columnName(st.Field(i))
		for _, banned := range tenantish {
			if strings.Contains(col, banned) {
				t.Errorf("列 %q 含租户味道的 %q：X3 禁区，隔离靠物理分库不是列过滤", col, banned)
			}
		}
	}
	// owner_user_id 是人不是租户：它必须在（卡面要求），但不能因此被当成隔离键。
	// 这一句拦的是"后来人把 owner 过滤当权限过滤用"——分配给谁不等于谁能看谁。
	if _, ok := st.FieldByName("OwnerUserID"); !ok {
		t.Fatal("卡面要求 owner_user_id")
	}
}

// TestOpportunityKeyColumnWidths 标识列的宽度取向：与下游/上游列对齐，不是随手定的。
func TestOpportunityKeyColumnWidths(t *testing.T) {
	st := reflect.TypeOf(Opportunity{})
	// owner_user_id / customer_id 会被原样写进 sales_events（那里是 varchar(64)）：
	// 本表比它宽 = 事件写入那一步才炸，而且是整批回滚（T-P1-08 的同形缺陷）。
	if tag := gormTagOf(t, st, "OwnerUserID"); !strings.Contains(tag, "varchar(64)") {
		t.Errorf("owner_user_id 应与 sales_events.owner_id 同宽：%s", tag)
	}
	if tag := gormTagOf(t, st, "CustomerID"); !strings.Contains(tag, "varchar(64)") {
		t.Errorf("customer_id 应与 sales_events.customer_id 同宽：%s", tag)
	}
	// one_id 由渠道标识派生（phone:/telegram: 前缀 + 外部 ID），长度不由本仓决定 ⇒ text。
	// 它也不写进任何定宽列，所以这里的取向与上面两列不矛盾。
	if tag := gormTagOf(t, st, "OneID"); !strings.Contains(tag, "type:text") {
		t.Errorf("one_id 应为 text（上游决定长度）：%s", tag)
	}
	// clue_id 指向 clues.id（uuid，varchar(36)）：定宽取上游的真实宽度。
	if tag := gormTagOf(t, st, "ClueID"); !strings.Contains(tag, "varchar(36)") {
		t.Errorf("clue_id 应与 clues.id 同宽：%s", tag)
	}
	// 主键用业务键 text：opportunity_id 会散进 sales_events / quotes / bills，
	// 自增 serial 在多实例下不唯一，也不可作为对外引用（同 order_drafts / approval_requests）。
	if tag := gormTagOf(t, st, "ID"); !strings.Contains(tag, "primaryKey") {
		t.Errorf("id 应为主键：%s", tag)
	}
	// code 唯一索引**不带谓词**：与 approval_requests.resume_token 相反——那里
	// "合法地可以有很多空值"，这里 code 是必填，空值不该积累。
	if tag := gormTagOf(t, st, "Code"); !strings.Contains(tag, "uniqueIndex") {
		t.Errorf("code 必须唯一（对外编号不能撞）：%s", tag)
	}
}

// TestOpportunityJSONNamesMatchColumns 对外 JSON 字段名必须与库里的列名一一对应。
//
// 为什么单独一条：列名由字段名派生（GORM 的 snake_case），JSON 名由标签决定，
// **两者可以各改各的**。把 Stage 的标签写成 json:"status" 之后，建表、真库断言、
// 仓储写集合全部照绿，只有 T-P4-04 的响应体里 stage 与 status 互换 ——
// 而那是"把在跑的商机显示成已赢单"级别的失真，且没人会去怀疑标签。
func TestOpportunityJSONNamesMatchColumns(t *testing.T) {
	st := reflect.TypeOf(Opportunity{})
	for i := 0; i < st.NumField(); i++ {
		f := st.Field(i)
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			t.Errorf("字段 %s 没有对外 JSON 名（响应体里会整个消失）", f.Name)
			continue
		}
		want := toSnake(f.Name)
		if name != want {
			t.Errorf("字段 %s：json 名 %q ≠ 列名 %q（API 与库会各说一套）", f.Name, name, want)
		}
	}
}

// toSnake 复刻 GORM NamingStrategy 对本表字段名的处理（含 ID 这类全大写缩写）。
func toSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			prev := s[i-1]
			// 连续大写（…ID、…OneID 的尾段）不再补下划线：ID → id 而非 _i_d。
			if !(prev >= 'A' && prev <= 'Z') || (i+1 < len(s) && s[i+1] >= 'a' && s[i+1] <= 'z') {
				b.WriteByte('_')
			}
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}

// columnName 取字段的真实列名：显式 column: 标签优先，否则按 json 标签，
// 再退到 GORM 的 snake_case 命名。
func columnName(f reflect.StructField) string {
	if tag := f.Tag.Get("gorm"); tag != "" {
		for _, part := range strings.Split(tag, ";") {
			if strings.HasPrefix(part, "column:") {
				return strings.TrimPrefix(part, "column:")
			}
		}
	}
	if j := f.Tag.Get("json"); j != "" {
		return strings.Split(j, ",")[0]
	}
	return f.Name
}
