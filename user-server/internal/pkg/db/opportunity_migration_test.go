// opportunity_migration_test.go T-P4-01 的 AC②③④ 在**真库**上的判据。
//
// 放在 internal/pkg/db 而不是 internal/model：建表事实源在这里（allModels() +
// GORM AutoMigrate），而 model 包 import 不了 testutil（成环）。
//
// 为什么标签层那份还不够：GORM 的 `type:numeric(12,2)` 写在标签上不等于库里真建成了
// numeric(12,2)——同一条标签在已有表上会被 AutoMigrate 的"列已存在就跳过"吞掉，
// 而 float 与 numeric 在读数上几乎没差别（只有累计与边界才露馅）。
// 所以 AC④ 的验收必须落到 information_schema，不能只落到 struct tag。
package db

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// opportunityWantColumns 卡面列清单在库里的真实列名（逐字比，多一列少一列都红）。
// 按列名升序写：读回来的是 `ORDER BY column_name`，两份顺序不一致会让"完全正确的
// 表"判成漂移，而用例的价值在于红的时候说的是缺了哪一列。
var opportunityWantColumns = []string{
	"amount", "clue_id", "code", "created_at", "currency", "customer_id",
	"expected_close_at", "id", "lost_reason", "one_id", "owner_user_id",
	"stage", "status", "updated_at", "version", "win_probability",
}

func TestOpportunityRegisteredInAllModels(t *testing.T) {
	found := false
	for _, m := range allModels() {
		if _, ok := m.(*model.Opportunity); ok {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Opportunity 未登记进 allModels()：全新部署不会建 opportunities 表，" +
			"而 T-P4-05 的写入点只会留一行日志")
	}
}

// TestOpportunityAutoMigrate_Idempotent AC③：建表可重跑。
//
// 断三件事，缺任一条都不算"幂等"：
//  1. 第二次 AutoMigrate 不报错（GORM 对已存在列是跳过，但新增索引/约束会重放）；
//  2. 第二次之后**列集合不变**（漂出多余列 = 每次启动都改一次 schema）；
//  3. 重跑期间**存量行不丢**（本表有真实业务行，重建表会把商机清零）。
func TestOpportunityAutoMigrate_Idempotent(t *testing.T) {
	db := testutil.NewTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	if err := db.AutoMigrate(&model.Opportunity{}); err != nil {
		t.Fatalf("首次 AutoMigrate 失败: %v", err)
	}
	first := opportunityColumns(t, db)
	if !reflect.DeepEqual(first, opportunityWantColumns) {
		t.Fatalf("列集合与卡面不符：\n  实际 %v\n  期望 %v", first, opportunityWantColumns)
	}

	// 存量行：第二次迁移之后必须还在，且字段值原样读回。
	seed := model.Opportunity{
		ID:          "opp_test_idempotent",
		Code:        "OPP-TEST-0001",
		CustomerID:  "cust-1",
		Stage:       model.OpportunityStageQualification,
		Status:      model.OpportunityStatusOpen,
		Amount:      1234.56,
		OwnerUserID: "sales-7",
	}
	if err := db.Create(&seed).Error; err != nil {
		t.Fatalf("写入测试行失败: %v", err)
	}

	if err := db.AutoMigrate(&model.Opportunity{}); err != nil {
		t.Fatalf("二次 AutoMigrate 应幂等: %v", err)
	}
	if got := opportunityColumns(t, db); !reflect.DeepEqual(got, first) {
		t.Errorf("二次迁移后列集合变了：\n  前 %v\n  后 %v", first, got)
	}
	if err := db.AutoMigrate(&model.Opportunity{}); err != nil {
		t.Fatalf("三次 AutoMigrate 应仍幂等: %v", err)
	}

	// 读回用独立零值 struct（复用 seed 会把已填字段并进 WHERE）。
	var got model.Opportunity
	if err := db.First(&got, "id = ?", seed.ID).Error; err != nil {
		t.Fatalf("重跑迁移后存量行丢失: %v", err)
	}
	if got.Amount != 1234.56 || got.Stage != model.OpportunityStageQualification || got.Status != model.OpportunityStatusOpen {
		t.Errorf("存量行字段被改写: %+v", got)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Errorf("两个时间戳应有值（卡面 timestamps）: %+v", got)
	}
	if got.Currency != model.OpportunityCurrencyDefault {
		t.Errorf("currency 应落默认币种 %q，实际 %q", model.OpportunityCurrencyDefault, got.Currency)
	}
	// expected_close_at 没赋值 ⇒ 库里必须是 NULL，不是 0001-01-01。
	var nullCount int64
	if err := db.Raw(`SELECT COUNT(*) FROM opportunities WHERE id = ? AND expected_close_at IS NULL`, seed.ID).
		Scan(&nullCount).Error; err != nil || nullCount != 1 {
		t.Errorf("未赋值的目标关单日必须是 NULL（零值时间会被逾期聚合读成早已过期）: err=%v n=%d", err, nullCount)
	}
}

// TestOpportunityNumericColumnsAreReallyNumeric AC④ 的真库判据。
//
// 量程/精度按 numeric_precision / numeric_scale 断，而不是按"data_type=numeric"断：
// numeric 而无精度 = PG 走未约束的 numeric，float 的误差问题会以另一种形式回来
// （任意精度 + 隐式舍入规则），而聚合侧看不出来。
func TestOpportunityNumericColumnsAreReallyNumeric(t *testing.T) {
	db := testutil.NewTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	if err := db.AutoMigrate(&model.Opportunity{}); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}
	for _, tc := range []struct {
		column        string
		wantPrecision int
		wantScale     int
	}{
		{"amount", 12, 2},         // AC④：金额 NUMERIC(12,2)，先例 sales_events.amount
		{"win_probability", 5, 2}, // 卡面 numeric(5,2)，值域 0–1（对齐 ltc.config 阈值）
	} {
		var dataType string
		var precision, scale int
		err := db.Raw(`SELECT data_type, numeric_precision, numeric_scale
			FROM information_schema.columns
			WHERE table_name = 'opportunities' AND column_name = ?`, tc.column).
			Row().Scan(&dataType, &precision, &scale)
		if err != nil {
			t.Fatalf("列 %s 不存在或读不到: %v", tc.column, err)
		}
		if dataType != "numeric" {
			t.Errorf("列 %s 类型 %q，期望 numeric（float/double 存金额会累积二进制误差）", tc.column, dataType)
		}
		if precision != tc.wantPrecision || scale != tc.wantScale {
			t.Errorf("列 %s 精度 numeric(%d,%d)，期望 (%d,%d)", tc.column, precision, scale, tc.wantPrecision, tc.wantScale)
		}
	}

	// 整表不许有任何一列是浮点型：只有这两列该是数值，写错类型的第三列同样危险，
	// 而"顺手加个 float 的比例列"是本表最可能长出来的东西。
	var floatCols []string
	if err := db.Raw(`SELECT column_name FROM information_schema.columns
		WHERE table_name = 'opportunities'
		  AND data_type IN ('double precision', 'real', 'float4', 'float8')`).
		Scan(&floatCols).Error; err != nil {
		t.Fatalf("查浮点列失败: %v", err)
	}
	if len(floatCols) > 0 {
		t.Errorf("商机表出现浮点列 %v：金额与概率一律 numeric", floatCols)
	}
}

// TestOpportunityHasNoTenantColumnAtDBLevel AC② 的真库判据：X3 禁区。
func TestOpportunityHasNoTenantColumnAtDBLevel(t *testing.T) {
	db := testutil.NewTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	if err := db.AutoMigrate(&model.Opportunity{}); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}
	for _, col := range opportunityColumns(t, db) {
		for _, banned := range []string{"tenant", "org_id", "corp", "workspace", "account_id", "namespace"} {
			if strings.Contains(col, banned) {
				t.Errorf("库里出现租户列 %q（含 %q）：隔离靠物理分库，加列过滤 = 重开 ADR-014 已 Simplified 的那道口子", col, banned)
			}
		}
	}
}

// TestOpportunityIndexesOnlyForNamedQueries 索引只跟着**已命名的查询方**走。
//
// 三个建索引（customer_id / stage / owner_user_id）的下家是 T-P4-02 卡面点名的
// "按客户/阶段/负责人查询"，created_at 的下家是 T-P4-04 列表端点的倒序，
// code 是唯一约束而非查询优化。其余列刻意不建：没有查询方的预留索引只有写放大，
// 且"有没有用"在今天根本无法验证（T-P2-04 在 sales_events 上同此口径）。
//
// 本用例把"未建"也做成可断言的状态：将来谁按 one_id 查商机，必须带着自己的下家
// 来改这里的期望集合，而不是提前把索引铺在暗处。
func TestOpportunityIndexesOnlyForNamedQueries(t *testing.T) {
	db := testutil.NewTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	if err := db.AutoMigrate(&model.Opportunity{}); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}
	if err := db.Exec(`INSERT INTO opportunities (id, code, stage, status) VALUES ('i','C1','qualification','open')`).Error; err != nil {
		t.Fatalf("插入索引断言用行失败: %v", err)
	}

	type idx struct {
		Cols string
		Uniq bool
		Pkey bool
	}
	var indexes []idx
	if err := db.Raw(`SELECT
			(SELECT string_agg(a.attname, '+' ORDER BY k.ord)
			   FROM unnest(i.indkey) WITH ORDINALITY AS k(attnum, ord)
			   JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = k.attnum) AS cols,
			i.indisunique AS uniq, i.indisprimary AS pkey
		FROM pg_index i WHERE i.indrelid = 'opportunities'::regclass`).
		Scan(&indexes).Error; err != nil {
		t.Fatalf("查 pg_index 失败: %v", err)
	}

	got := map[string]bool{}
	for _, i := range indexes {
		got[i.Cols] = i.Pkey || i.Uniq || true // 列集合都记下来，下面分别判
		if i.Cols == "code" && !i.Uniq {
			t.Error("code 上的索引必须唯一：对外编号撞了没人发现")
		}
		if i.Cols == "id" && !i.Pkey {
			t.Error("id 必须是主键")
		}
	}
	for _, want := range []string{"id", "code", "customer_id", "stage", "owner_user_id", "created_at"} {
		if !got[want] {
			t.Errorf("缺少期望索引 %s（T-P4-02/T-P4-04 的查询下家会退化成全表扫）", want)
		}
	}
	for _, none := range []string{"one_id", "clue_id", "status", "amount", "win_probability", "expected_close_at", "lost_reason", "version", "updated_at"} {
		if got[none] {
			t.Errorf("列 %s 上出现了预留索引：没有已命名的查询方，本卡不建", none)
		}
	}
}

func opportunityColumns(t *testing.T, db *gorm.DB) []string {
	t.Helper()
	var cols []string
	if err := db.Raw(`SELECT column_name FROM information_schema.columns
		WHERE table_name = 'opportunities' ORDER BY column_name`).Scan(&cols).Error; err != nil {
		t.Fatalf("查列集合失败: %v", err)
	}
	sort.Strings(cols)
	return cols
}

// TestOpportunityAutoMigrate_BackfillsVersionOnExistingTable 给存量表补 version 列这条路径，
// 今天必须真跑一遍（T-P4-02）。
//
// 为什么单独立一条：T-P4-01 已经把 opportunities 登记进 allModels()，所以真实升级路径不是
// "新建一张空表"，而是"表已经在了、行也已经在了，AutoMigrate 走 ADD COLUMN 分支"。
// PG 的 `ADD COLUMN ... NOT NULL` 不带默认值会当场失败（老行没有值可填），而只带
// `default:0` 不带 `not null` 又会让老行留下 NULL —— 两种坏法在**空表**上都看不出来，
// 开发库里的这张表恰恰是空的。
//
// 建表语句照 T-P4-01 那次交付的实测形状手写（不是从今天的 struct 少抄一列）：
// 抄错等于把"当年的形状"也改了，那样这条用例证明的就不是升级路径而是现状。
func TestOpportunityAutoMigrate_BackfillsVersionOnExistingTable(t *testing.T) {
	testDB := testutil.NewTestDB(t)
	if testDB == nil {
		t.Fatal("测试库不可达")
	}
	if err := testDB.Exec(`DROP TABLE IF EXISTS opportunities`).Error; err != nil {
		t.Fatalf("清表失败: %v", err)
	}
	const t40DDL = `CREATE TABLE opportunities (
		id text NOT NULL PRIMARY KEY,
		code varchar(32),
		customer_id varchar(64),
		one_id text,
		clue_id varchar(36),
		stage varchar(32),
		status varchar(16),
		amount numeric(12,2),
		currency varchar(3) DEFAULT 'CNY',
		win_probability numeric(5,2),
		owner_user_id varchar(64),
		expected_close_at timestamptz,
		lost_reason text,
		created_at timestamptz,
		updated_at timestamptz
	)`
	if err := testDB.Exec(t40DDL).Error; err != nil {
		t.Fatalf("按 T-P4-01 形状建表失败: %v", err)
	}
	// 两行存量数据：没有它们，ADD COLUMN NOT NULL 一律成功，本用例就白写。
	for _, id := range []string{"opp_old_1", "opp_old_2"} {
		if err := testDB.Exec(
			`INSERT INTO opportunities (id, code, stage, status, amount) VALUES (?, ?, 'qualification', 'open', 100)`,
			id, "C-"+id).Error; err != nil {
			t.Fatalf("写入存量行失败: %v", err)
		}
	}

	if err := testDB.AutoMigrate(&model.Opportunity{}); err != nil {
		t.Fatalf("给存量表补 version 列失败（not null 与 default 少一个就是这个报错）: %v", err)
	}

	var nullable, def, dataType string
	if err := testDB.Raw(`SELECT is_nullable FROM information_schema.columns
		WHERE table_name='opportunities' AND column_name='version'`).
		Scan(&nullable).Error; err != nil {
		t.Fatalf("查 is_nullable 失败: %v", err)
	}
	if err := testDB.Raw(`SELECT COALESCE(column_default,'') FROM information_schema.columns
		WHERE table_name='opportunities' AND column_name='version'`).
		Scan(&def).Error; err != nil {
		t.Fatalf("查 column_default 失败: %v", err)
	}
	if err := testDB.Raw(`SELECT data_type FROM information_schema.columns
		WHERE table_name='opportunities' AND column_name='version'`).
		Scan(&dataType).Error; err != nil {
		t.Fatalf("查 data_type 失败: %v", err)
	}
	if nullable != "NO" {
		t.Errorf("补出来的 version 可空（=%q）：NULL 读进 int64 会在查询当场报错", nullable)
	}
	if !strings.HasPrefix(def, "0") {
		t.Errorf("version 默认值 %q：没有 0 这个默认值，补列那一步本身就过不去", def)
	}
	// 有符号整型且够宽：CAS 的比较是 `=` 而不是 `<`，但回绕成负数会让"老读到的那版"
	// 与"库里的新版"在 int4 上撞成同一个值（同 T-P4-01 对主键不用 serial 的同一类理由）。
	if dataType != "bigint" {
		t.Errorf("version 列类型 %q，期望 bigint", dataType)
	}

	// 老行必须能被读回来，且读到的版本是 0（= "这一列存在之前它一次都没改过"）。
	// 这里刻意用一个独立零值 struct 读，不复用上面插入时那个实例。
	var back model.Opportunity
	if err := testDB.Where("id = ?", "opp_old_1").First(&back).Error; err != nil {
		t.Fatalf("补列后读存量行失败（NULL 读不进 int64 就在这里红）: %v", err)
	}
	if back.Version != 0 {
		t.Errorf("存量行的 version = %d，期望 0：非零意味着补列时被填上了某个真实版本号，CAS 会漏掉第一次改写", back.Version)
	}
	if back.Amount != 100 || back.Status != model.OpportunityStatusOpen {
		t.Errorf("补列顺带动了别的列：amount=%v status=%q（AutoMigrate 只做 ADD COLUMN 才叫幂等）", back.Amount, back.Status)
	}

	// 再跑两次：补列之后再重跑必须是 no-op（幂等可重跑这条 AC 从 T-P4-01 延续到本卡）。
	for i := 0; i < 2; i++ {
		if err := testDB.AutoMigrate(&model.Opportunity{}); err != nil {
			t.Fatalf("补列后再跑第 %d 次 AutoMigrate 失败: %v", i+1, err)
		}
	}
	cols := opportunityColumns(t, testDB)
	if len(cols) != len(opportunityWantColumns) {
		t.Errorf("存量表升级后的列数 %d ≠ %d", len(cols), len(opportunityWantColumns))
	}
}
