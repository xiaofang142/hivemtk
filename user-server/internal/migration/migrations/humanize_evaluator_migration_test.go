package migrations

import (
	"context"
	"testing"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupHumanizeMigrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t)
}

// TestHumanizeMigration_Version 验证元信息
func TestHumanizeMigration_Version(t *testing.T) {
	m := NewHumanizeEvaluatorMigration(nil)
	if m.Version() != "v2.9.1" {
		t.Errorf("Version()=%q want=v2.9.1", m.Version())
	}
	if m.Name() == "" {
		t.Error("Name() should not be empty")
	}
	if m.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

// TestHumanizeMigration_NilDB nil db 返回错误
func TestHumanizeMigration_NilDB(t *testing.T) {
	m := NewHumanizeEvaluatorMigration(nil)
	if err := m.Up(context.Background()); err == nil {
		t.Errorf("nil db Up() 应返回错误")
	}
	if err := m.Down(context.Background()); err == nil {
		t.Errorf("nil db Down() 应返回错误")
	}
}

// TestHumanizeMigration_UpCreatesAllTables 集成测试：Up 创建所有 5 张表
func TestHumanizeMigration_UpCreatesAllTables(t *testing.T) {
	db := setupHumanizeMigrationTestDB(t)

	m := NewHumanizeEvaluatorMigration(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}

	expectedTables := []string{
		"humanize_scores",
		"humanize_dimensions",
		"champion_baselines",
		"champion_phrases",
		"ab_test_stats",
	}
	for _, table := range expectedTables {
		var exists bool
		if err := db.Raw(`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = ?)`, table).Scan(&exists).Error; err != nil || !exists {
			t.Errorf("表 %s 应存在 after Up(): exists=%v err=%v", table, exists, err)
		}
	}
}

// TestHumanizeMigration_UpIdempotent 集成测试：Up 幂等
func TestHumanizeMigration_UpIdempotent(t *testing.T) {
	db := setupHumanizeMigrationTestDB(t)

	m := NewHumanizeEvaluatorMigration(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("first Up() failed: %v", err)
	}
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("second Up() should be idempotent, got: %v", err)
	}
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("third Up() should be idempotent, got: %v", err)
	}
}

// TestHumanizeMigration_Down 集成测试：Down 不删模型持有的 5 张表（降级销毁在用数据）
func TestHumanizeMigration_Down(t *testing.T) {
	db := setupHumanizeMigrationTestDB(t)

	m := NewHumanizeEvaluatorMigration(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}
	if err := m.Down(context.Background()); err != nil {
		t.Fatalf("Down() failed: %v", err)
	}

	preservedTables := []string{
		"humanize_scores",
		"humanize_dimensions",
		"champion_baselines",
		"champion_phrases",
		"ab_test_stats",
	}
	for _, table := range preservedTables {
		var exists bool
		_ = db.Raw(`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = ?)`, table).Scan(&exists)
		if !exists {
			t.Errorf("Down() 后表 %s 应保留：降级删它等于销毁当前代码在用的数据", table)
		}
	}
}

// TestHumanizeMigration_DownIdempotent 集成测试：Down 幂等（重复不报错）
func TestHumanizeMigration_DownIdempotent(t *testing.T) {
	db := setupHumanizeMigrationTestDB(t)

	m := NewHumanizeEvaluatorMigration(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}
	if err := m.Down(context.Background()); err != nil {
		t.Fatalf("first Down() failed: %v", err)
	}
	if err := m.Down(context.Background()); err != nil {
		t.Fatalf("second Down() should be idempotent, got: %v", err)
	}
}

// TestHumanizeMigration_InsertHumanizeScore 集成测试：humanize_scores 可写入
func TestHumanizeMigration_InsertHumanizeScore(t *testing.T) {
	db := setupHumanizeMigrationTestDB(t)

	m := NewHumanizeEvaluatorMigration(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}

	if err := db.Exec(`
		INSERT INTO humanize_scores
			(score_id, session_id, customer_id, message_id, persona, industry, platform, intent,
			 customer_message, ai_reply, final_reply, evaluator_type, sample_strategy,
			 naturalness, conciseness, empathy, professionalism, persuasiveness,
			 total_score, threshold, distance_to_champion, passed, attempt_count,
			 llm_model, llm_latency_ms, reason_json)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8,
		        $9, $10, $11, $12, $13,
		        $14, $15, $16, $17, $18,
		        $19, $20, $21, $22, $23,
		        $24, $25, $26)
	`,
		"hs-test-1", "sess-1", "cust-1", "msg-1", "美妆顾问", "美妆", "wechat", "ask_product",
		"产品怎么样？", "这款产品很好。", "这款产品很好。", "rule", "full",
		0.850, 0.900, 0.800, 0.850, 0.800,
		0.840, 0.850, 0.050, true, 1,
		"", 0, "{}",
	).Error; err != nil {
		t.Errorf("插入 humanize_scores 失败: %v", err)
	}

	var count int64
	_ = db.Raw(`SELECT COUNT(*) FROM humanize_scores WHERE score_id = 'hs-test-1'`).Scan(&count)
	if count != 1 {
		t.Errorf("插入后应可查询到 1 条, got %d", count)
	}
}

// TestHumanizeMigration_InsertHumanizeDimensions 集成测试：humanize_dimensions 可写入
func TestHumanizeMigration_InsertHumanizeDimensions(t *testing.T) {
	db := setupHumanizeMigrationTestDB(t)

	m := NewHumanizeEvaluatorMigration(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}

	dimensions := []struct {
		dim    string
		score  float64
		weight float64
	}{
		{"naturalness", 0.850, 0.25},
		{"conciseness", 0.900, 0.15},
		{"empathy", 0.800, 0.20},
		{"professionalism", 0.850, 0.20},
		{"persuasiveness", 0.800, 0.20},
	}
	for _, d := range dimensions {
		if err := db.Exec(`
			INSERT INTO humanize_dimensions (score_id, dimension, score, weight, reason)
			VALUES ($1, $2, $3, $4, $5)
		`, "hs-test-1", d.dim, d.score, d.weight, "").Error; err != nil {
			t.Errorf("插入 humanize_dimensions(%s) 失败: %v", d.dim, err)
		}
	}

	var count int64
	_ = db.Raw(`SELECT COUNT(*) FROM humanize_dimensions WHERE score_id = 'hs-test-1'`).Scan(&count)
	if count != 5 {
		t.Errorf("应插入 5 条维度记录, got %d", count)
	}
}

// TestHumanizeMigration_InsertChampionBaseline 集成测试：champion_baselines 可写入
func TestHumanizeMigration_InsertChampionBaseline(t *testing.T) {
	db := setupHumanizeMigrationTestDB(t)

	m := NewHumanizeEvaluatorMigration(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}

	if err := db.Exec(`
		INSERT INTO champion_baselines
			(persona, industry, intent, naturalness, conciseness, empathy, professionalism, persuasiveness,
			 sample_count, sample_stddev, period_start, period_end, version, enabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8,
		        $9, $10, NOW() - INTERVAL '7 days', NOW(), 1, TRUE)
	`,
		"美妆顾问", "美妆", "ask_product",
		0.880, 0.900, 0.850, 0.870, 0.860,
		100, 0.050,
	).Error; err != nil {
		t.Errorf("插入 champion_baselines 失败: %v", err)
	}

	var count int64
	_ = db.Raw(`SELECT COUNT(*) FROM champion_baselines WHERE persona = '美妆顾问'`).Scan(&count)
	if count != 1 {
		t.Errorf("插入后应可查询到 1 条, got %d", count)
	}
}

// TestHumanizeMigration_InsertChampionPhrase 集成测试：champion_phrases 可写入
func TestHumanizeMigration_InsertChampionPhrase(t *testing.T) {
	db := setupHumanizeMigrationTestDB(t)

	m := NewHumanizeEvaluatorMigration(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}

	var baselineID int64
	if err := db.Exec(`
		INSERT INTO champion_baselines
			(persona, industry, intent, naturalness, conciseness, empathy, professionalism, persuasiveness,
			 sample_count, version, enabled)
		VALUES ('test', 'test', 'test', 0.85, 0.85, 0.85, 0.85, 0.85, 10, 1, TRUE)
	`).Error; err != nil {
		t.Fatalf("插入 baseline 失败: %v", err)
	}
	_ = db.Raw(`SELECT id FROM champion_baselines WHERE persona = 'test' ORDER BY id DESC LIMIT 1`).Scan(&baselineID)

	phrases := []struct {
		phrase string
		tfidf  float64
		tf     int
		df     int
		ptype  string
		rank   int
	}{
		{"下单试试", 1.83258, 3, 2, "action", 1},
		{"成分保湿", 1.60944, 2, 2, "professional", 2},
		{"优惠活动", 1.09861, 1, 3, "persuasion", 3},
	}
	for _, p := range phrases {
		if err := db.Exec(`
			INSERT INTO champion_phrases (baseline_id, phrase, tfidf_score, tf, df, phrase_type, rank)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, baselineID, p.phrase, p.tfidf, p.tf, p.df, p.ptype, p.rank).Error; err != nil {
			t.Errorf("插入 champion_phrases(%s) 失败: %v", p.phrase, err)
		}
	}

	var count int64
	_ = db.Raw(`SELECT COUNT(*) FROM champion_phrases WHERE baseline_id = ?`, baselineID).Scan(&count)
	if count != 3 {
		t.Errorf("应插入 3 条短语, got %d", count)
	}
}

// TestHumanizeMigration_InsertABTestStat 集成测试：ab_test_stats 可写入
func TestHumanizeMigration_InsertABTestStat(t *testing.T) {
	db := setupHumanizeMigrationTestDB(t)

	m := NewHumanizeEvaluatorMigration(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}

	for _, g := range []struct {
		group  string
		mean   float64
		stddev float64
	}{
		{"control", 0.7500, 0.0500},
		{"treatment", 0.8800, 0.0400},
	} {
		if err := db.Exec(`
			INSERT INTO ab_test_stats
				(experiment_id, group_name, sample_size, mean_score, median_score, stddev_score,
				 mann_whitney_u, mann_whitney_p, cohens_d,
				 bootstrap_ci_low, bootstrap_ci_high,
				 significant, effect_size_label, winner)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		`,
			"exp-001", g.group, 30, g.mean, g.mean, g.stddev,
			120, 0.0010, -1.8500,
			0.0800, 0.1800,
			true, "large", "treatment",
		).Error; err != nil {
			t.Errorf("插入 ab_test_stats(%s) 失败: %v", g.group, err)
		}
	}

	var count int64
	_ = db.Raw(`SELECT COUNT(*) FROM ab_test_stats WHERE experiment_id = 'exp-001'`).Scan(&count)
	if count != 2 {
		t.Errorf("应插入 2 条统计记录, got %d", count)
	}
}

// TestHumanizeMigration_IndexesCreated 集成测试：所有索引创建
func TestHumanizeMigration_IndexesCreated(t *testing.T) {
	db := setupHumanizeMigrationTestDB(t)

	m := NewHumanizeEvaluatorMigration(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}

	expectedIndexes := []string{
		"idx_humanize_session",
		"idx_humanize_persona",
		"idx_humanize_score",
		"idx_hd_score",
		"idx_champion_pii",
		"idx_cp_baseline",
		"idx_abstat_exp",
	}
	for _, idx := range expectedIndexes {
		var exists bool
		err := db.Raw(`
			SELECT EXISTS (
				SELECT 1 FROM pg_indexes WHERE indexname = ?
			)
		`, idx).Scan(&exists).Error
		if err != nil || !exists {
			t.Errorf("索引 %s 应存在: exists=%v err=%v", idx, exists, err)
		}
	}
}

// TestHumanizeMigration_UpThenDownThenUp 集成测试：Up → Down → Up 循环
func TestHumanizeMigration_UpThenDownThenUp(t *testing.T) {
	db := setupHumanizeMigrationTestDB(t)

	m := NewHumanizeEvaluatorMigration(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("first Up() failed: %v", err)
	}
	if err := m.Down(context.Background()); err != nil {
		t.Fatalf("Down() failed: %v", err)
	}
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("second Up() after Down() failed: %v", err)
	}

	var exists bool
	_ = db.Raw(`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'humanize_scores')`).Scan(&exists)
	if !exists {
		t.Errorf("Up → Down → Up 后 humanize_scores 应存在")
	}
}

// TestHumanizeMigration_ReasonJSON 集成测试：reason_json 字段支持 JSONB
func TestHumanizeMigration_ReasonJSON(t *testing.T) {
	db := setupHumanizeMigrationTestDB(t)

	m := NewHumanizeEvaluatorMigration(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}

	reasonJSON := `{"naturalness":"无 AI 痕迹词","conciseness":"字数适中","empathy":"含共情词","professionalism":"含专业词","persuasiveness":"有行动召唤"}`
	if err := db.Exec(`
		INSERT INTO humanize_scores
			(score_id, session_id, customer_id, ai_reply, evaluator_type, sample_strategy,
			 naturalness, conciseness, empathy, professionalism, persuasiveness,
			 total_score, threshold, passed, reason_json)
		VALUES ($1, $2, $3, $4, $5, $6,
		        $7, $8, $9, $10, $11,
		        $12, $13, $14, $15::jsonb)
	`,
		"hs-json-1", "sess-1", "cust-1", "测试回复", "rule", "full",
		0.850, 0.900, 0.800, 0.850, 0.800,
		0.840, 0.850, true, reasonJSON,
	).Error; err != nil {
		t.Errorf("插入含 reason_json 的 humanize_scores 失败: %v", err)
	}

	var reason string
	if err := db.Raw(`SELECT reason_json->>'naturalness' FROM humanize_scores WHERE score_id = 'hs-json-1'`).Scan(&reason).Error; err != nil {
		t.Errorf("查询 reason_json 失败: %v", err)
	}
	if reason != "无 AI 痕迹词" {
		t.Errorf("reason_json->>'naturalness' = %q want '无 AI 痕迹词'", reason)
	}
}

// TestHumanizeMigration_DecimalPrecision 集成测试：DECIMAL(4,3) 精度正确
func TestHumanizeMigration_DecimalPrecision(t *testing.T) {
	db := setupHumanizeMigrationTestDB(t)

	m := NewHumanizeEvaluatorMigration(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}

	if err := db.Exec(`
		INSERT INTO humanize_scores
			(score_id, session_id, customer_id, ai_reply, evaluator_type, sample_strategy,
			 naturalness, conciseness, empathy, professionalism, persuasiveness,
			 total_score, threshold, passed)
		VALUES ('hs-prec-1', 'sess-1', 'cust-1', '测试', 'rule', 'full',
		        0.851, 0.852, 0.853, 0.854, 0.855,
		        0.853, 0.850, true)
	`).Error; err != nil {
		t.Fatalf("插入失败: %v", err)
	}

	var nat, con, emp, pro, per, total float64
	_ = db.Raw(`
		SELECT naturalness, conciseness, empathy, professionalism, persuasiveness, total_score
		FROM humanize_scores WHERE score_id = 'hs-prec-1'
	`).Row().Scan(&nat, &con, &emp, &pro, &per, &total)

	if !approxEqualFloat(nat, 0.851) {
		t.Errorf("naturalness=%v want 0.851", nat)
	}
	if !approxEqualFloat(con, 0.852) {
		t.Errorf("conciseness=%v want 0.852", con)
	}
	if !approxEqualFloat(total, 0.853) {
		t.Errorf("total_score=%v want 0.853", total)
	}
}
