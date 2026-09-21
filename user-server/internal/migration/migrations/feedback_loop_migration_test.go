package migrations

import (
	"context"
	"fmt"
	"testing"

	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

var _ migration.Migration = (*FeedbackLoopMigration)(nil)

func setupFeedbackLoopMigrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t)
}

func dropDependencyTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	_ = db.Exec(`DROP TABLE IF EXISTS sop_agents, script_templates CASCADE`).Error
}

// TestFeedbackLoopMigration_Version 验证元信息
func TestFeedbackLoopMigration_Version(t *testing.T) {
	m := NewFeedbackLoopMigration(nil)
	if m.Version() != "v3.0.0" {
		t.Errorf("Version()=%q want=v3.0.0", m.Version())
	}
	if m.Name() == "" {
		t.Error("Name() should not be empty")
	}
	if m.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

// TestFeedbackLoopMigration_NilDB nil db 返回错误
func TestFeedbackLoopMigration_NilDB(t *testing.T) {
	m := NewFeedbackLoopMigration(nil)
	if err := m.Up(context.Background()); err == nil {
		t.Errorf("nil db Up() 应返回错误")
	}
	if err := m.Down(context.Background()); err == nil {
		t.Errorf("nil db Down() 应返回错误")
	}
}

// TestFeedbackLoopMigration_UpCreatesAllTables 集成测试：Up 创建所有 6 张新表
//
// 注意：sop_agents/script_templates 表需要预先存在（v1.x 创建），
// 此处先手动创建以测试 ALTER 逻辑
func TestFeedbackLoopMigration_UpCreatesAllTables(t *testing.T) {
	db := setupFeedbackLoopMigrationTestDB(t)
	if err := db.Exec(`
		CREATE TABLE sop_agents (
			id BIGSERIAL PRIMARY KEY,
			name VARCHAR(100) NOT NULL,
			scenario VARCHAR(50) NOT NULL,
			is_active BOOLEAN DEFAULT TRUE,
			sop_graph JSONB NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)
	`).Error; err != nil {
		t.Fatalf("pre-create sop_agents: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE script_templates (
			id BIGSERIAL PRIMARY KEY,
			category VARCHAR(50) NOT NULL,
			title VARCHAR(200) NOT NULL,
			content TEXT NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)
	`).Error; err != nil {
		t.Fatalf("pre-create script_templates: %v", err)
	}

	m := NewFeedbackLoopMigration(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}

	expectedTables := []string{
		"feedback_events",
		"feedback_signals",
		"champion_dialogues",
		"prompt_candidates",
		"bandit_arms",
		"prompt_ab_tests",
	}
	for _, table := range expectedTables {
		var exists bool
		if err := db.Raw(`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = ?)`, table).Scan(&exists).Error; err != nil || !exists {
			t.Errorf("表 %s 应存在 after Up(): exists=%v err=%v", table, exists, err)
		}
	}
}

// TestFeedbackLoopMigration_UpIdempotent 集成测试：Up 幂等
func TestFeedbackLoopMigration_UpIdempotent(t *testing.T) {
	db := setupFeedbackLoopMigrationTestDB(t)
	_ = db.Exec(`CREATE TABLE sop_agents (id BIGSERIAL PRIMARY KEY, name VARCHAR(100))`).Error
	_ = db.Exec(`CREATE TABLE script_templates (id BIGSERIAL PRIMARY KEY, title VARCHAR(200))`).Error

	m := NewFeedbackLoopMigration(db)
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

// TestFeedbackLoopMigration_Down 集成测试：Down 只回退列/索引，6 张模型持有的表不得被删
func TestFeedbackLoopMigration_Down(t *testing.T) {
	db := setupFeedbackLoopMigrationTestDB(t)
	dropDependencyTables(t, db)
	_ = db.Exec(`CREATE TABLE sop_agents (id BIGSERIAL PRIMARY KEY, name VARCHAR(100))`).Error
	_ = db.Exec(`CREATE TABLE script_templates (id BIGSERIAL PRIMARY KEY, title VARCHAR(200))`).Error

	m := NewFeedbackLoopMigration(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}
	if err := m.Down(context.Background()); err != nil {
		t.Fatalf("Down() failed: %v", err)
	}

	preservedTables := []string{
		"feedback_events",
		"feedback_signals",
		"champion_dialogues",
		"prompt_candidates",
		"bandit_arms",
		"prompt_ab_tests",
	}
	for _, table := range preservedTables {
		var exists bool
		_ = db.Raw(`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = ?)`, table).Scan(&exists)
		if !exists {
			t.Errorf("Down() 后表 %s 应保留：降级删它等于销毁当前代码在用的数据", table)
		}
	}
}

// TestFeedbackLoopMigration_DownIdempotent 集成测试：Down 幂等
func TestFeedbackLoopMigration_DownIdempotent(t *testing.T) {
	db := setupFeedbackLoopMigrationTestDB(t)
	dropDependencyTables(t, db)
	_ = db.Exec(`CREATE TABLE sop_agents (id BIGSERIAL PRIMARY KEY, name VARCHAR(100))`).Error
	_ = db.Exec(`CREATE TABLE script_templates (id BIGSERIAL PRIMARY KEY, title VARCHAR(200))`).Error

	m := NewFeedbackLoopMigration(db)
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

// TestFeedbackLoopMigration_UpThenDownThenUp 集成测试：Up → Down → Up 循环
func TestFeedbackLoopMigration_UpThenDownThenUp(t *testing.T) {
	db := setupFeedbackLoopMigrationTestDB(t)
	dropDependencyTables(t, db)
	_ = db.Exec(`CREATE TABLE sop_agents (id BIGSERIAL PRIMARY KEY, name VARCHAR(100))`).Error
	_ = db.Exec(`CREATE TABLE script_templates (id BIGSERIAL PRIMARY KEY, title VARCHAR(200))`).Error

	m := NewFeedbackLoopMigration(db)
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
	_ = db.Raw(`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'feedback_events')`).Scan(&exists)
	if !exists {
		t.Errorf("Up → Down → Up 后 feedback_events 应存在")
	}
}

// TestFeedbackLoopMigration_InsertFeedbackEvent 集成测试：feedback_events 可写入
func TestFeedbackLoopMigration_InsertFeedbackEvent(t *testing.T) {
	db := setupFeedbackLoopMigrationTestDB(t)
	dropDependencyTables(t, db)
	_ = db.Exec(`CREATE TABLE sop_agents (id BIGSERIAL PRIMARY KEY, name VARCHAR(100))`).Error
	_ = db.Exec(`CREATE TABLE script_templates (id BIGSERIAL PRIMARY KEY, title VARCHAR(200))`).Error

	m := NewFeedbackLoopMigration(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}

	if err := db.Exec(`
		INSERT INTO feedback_events
			(event_id, session_id, customer_id, sop_id, event_type, signal_key,
			 signal_value, weight, reward, ai_reply, customer_msg)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10, $11)
	`,
		"evt-test-1", "sess-1", "cust-1", 1, "explicit", "like",
		`{"v":true}`, 1.0, 1.0, "ai reply", "customer msg",
	).Error; err != nil {
		t.Errorf("插入 feedback_events 失败: %v", err)
	}

	var count int64
	_ = db.Raw(`SELECT COUNT(*) FROM feedback_events WHERE event_id = 'evt-test-1'`).Scan(&count)
	if count != 1 {
		t.Errorf("插入后应可查询到 1 条, got %d", count)
	}
}

// TestFeedbackLoopMigration_InsertFeedbackSignal 集成测试：feedback_signals 可写入
func TestFeedbackLoopMigration_InsertFeedbackSignal(t *testing.T) {
	db := setupFeedbackLoopMigrationTestDB(t)
	dropDependencyTables(t, db)
	_ = db.Exec(`CREATE TABLE sop_agents (id BIGSERIAL PRIMARY KEY, name VARCHAR(100))`).Error
	_ = db.Exec(`CREATE TABLE script_templates (id BIGSERIAL PRIMARY KEY, title VARCHAR(200))`).Error

	m := NewFeedbackLoopMigration(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}

	if err := db.Exec(`
		INSERT INTO feedback_signals
			(session_id, customer_id, sop_id, variant, aggregated_reward,
			 signal_count, signal_breakdown, outcome, is_champion)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9)
	`,
		"sess-sig-1", "cust-1", 1, "A", 2.5, 3,
		`{"like":1,"conversion":1}`, "success", true,
	).Error; err != nil {
		t.Errorf("插入 feedback_signals 失败: %v", err)
	}

	var count int64
	_ = db.Raw(`SELECT COUNT(*) FROM feedback_signals WHERE session_id = 'sess-sig-1'`).Scan(&count)
	if count != 1 {
		t.Errorf("插入后应可查询到 1 条, got %d", count)
	}
}

// TestFeedbackLoopMigration_InsertChampionDialogue 集成测试：champion_dialogues 可写入（含 pgvector）
func TestFeedbackLoopMigration_InsertChampionDialogue(t *testing.T) {
	db := setupFeedbackLoopMigrationTestDB(t)
	dropDependencyTables(t, db)
	_ = db.Exec(`CREATE TABLE sop_agents (id BIGSERIAL PRIMARY KEY, name VARCHAR(100))`).Error
	_ = db.Exec(`CREATE TABLE script_templates (id BIGSERIAL PRIMARY KEY, title VARCHAR(200))`).Error

	m := NewFeedbackLoopMigration(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}

	vec := make([]float64, 1024)
	for i := range vec {
		vec[i] = 0.1
	}
	vecStr := "["
	for i, v := range vec {
		if i > 0 {
			vecStr += ","
		}
		vecStr += fmt.Sprintf("%f", v)
	}
	vecStr += "]"

	if err := db.Exec(`
		INSERT INTO champion_dialogues
			(dialogue_fingerprint, session_id, customer_id, scenario,
			 customer_msg, champion_reply, embedding, cluster_id, reward, conversion_achieved)
		VALUES ($1, $2, $3, $4, $5, $6, $7::vector, $8, $9, $10)
	`,
		"fp-test-1", "sess-ch-1", "cust-1", "closing",
		"客户消息", "销冠回复", vecStr, 1, 3.0, true,
	).Error; err != nil {
		t.Errorf("插入 champion_dialogues 失败: %v", err)
	}

	var count int64
	_ = db.Raw(`SELECT COUNT(*) FROM champion_dialogues WHERE dialogue_fingerprint = 'fp-test-1'`).Scan(&count)
	if count != 1 {
		t.Errorf("插入后应可查询到 1 条, got %d", count)
	}
}

// TestFeedbackLoopMigration_InsertPromptCandidate 集成测试：prompt_candidates 可写入
func TestFeedbackLoopMigration_InsertPromptCandidate(t *testing.T) {
	db := setupFeedbackLoopMigrationTestDB(t)
	dropDependencyTables(t, db)
	_ = db.Exec(`CREATE TABLE sop_agents (id BIGSERIAL PRIMARY KEY, name VARCHAR(100))`).Error
	_ = db.Exec(`CREATE TABLE script_templates (id BIGSERIAL PRIMARY KEY, title VARCHAR(200))`).Error

	m := NewFeedbackLoopMigration(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}

	if err := db.Exec(`
		INSERT INTO prompt_candidates
			(sop_node_id, sop_id, scenario, version, title, system_prompt, user_prompt_template, status, alpha, beta)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`,
		"node_1", 1, "sop_reply", "v1.0", "test",
		"sys prompt", "user prompt template",
		"active", 2.0, 2.0,
	).Error; err != nil {
		t.Errorf("插入 prompt_candidates 失败: %v", err)
	}

	var count int64
	_ = db.Raw(`SELECT COUNT(*) FROM prompt_candidates WHERE title = 'test'`).Scan(&count)
	if count != 1 {
		t.Errorf("插入后应可查询到 1 条, got %d", count)
	}
}

// TestFeedbackLoopMigration_InsertBanditArm 集成测试：bandit_arms 可写入
func TestFeedbackLoopMigration_InsertBanditArm(t *testing.T) {
	db := setupFeedbackLoopMigrationTestDB(t)
	dropDependencyTables(t, db)
	_ = db.Exec(`CREATE TABLE sop_agents (id BIGSERIAL PRIMARY KEY, name VARCHAR(100))`).Error
	_ = db.Exec(`CREATE TABLE script_templates (id BIGSERIAL PRIMARY KEY, title VARCHAR(200))`).Error

	m := NewFeedbackLoopMigration(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}

	if err := db.Exec(`
		INSERT INTO bandit_arms
			(experiment_id, experiment_type, arm_key, alpha, beta, total_trials, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`,
		"exp-test-1", "prompt", "arm_a", 2.0, 2.0, 0, "exploring",
	).Error; err != nil {
		t.Errorf("插入 bandit_arms 失败: %v", err)
	}

	var count int64
	_ = db.Raw(`SELECT COUNT(*) FROM bandit_arms WHERE experiment_id = 'exp-test-1'`).Scan(&count)
	if count != 1 {
		t.Errorf("插入后应可查询到 1 条, got %d", count)
	}
}

// TestFeedbackLoopMigration_InsertPromptABTest 集成测试：prompt_ab_tests 可写入
func TestFeedbackLoopMigration_InsertPromptABTest(t *testing.T) {
	db := setupFeedbackLoopMigrationTestDB(t)
	dropDependencyTables(t, db)
	_ = db.Exec(`CREATE TABLE sop_agents (id BIGSERIAL PRIMARY KEY, name VARCHAR(100))`).Error
	_ = db.Exec(`CREATE TABLE script_templates (id BIGSERIAL PRIMARY KEY, title VARCHAR(200))`).Error

	m := NewFeedbackLoopMigration(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}

	if err := db.Exec(`
		INSERT INTO prompt_ab_tests
			(experiment_id, experiment_type, sop_id, name, arm_keys, config, status)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7)
	`,
		"abtest-1", "prompt", 1, "test ab",
		`["arm_0","arm_1"]`, `{"min_samples":100}`, "running",
	).Error; err != nil {
		t.Errorf("插入 prompt_ab_tests 失败: %v", err)
	}

	var count int64
	_ = db.Raw(`SELECT COUNT(*) FROM prompt_ab_tests WHERE experiment_id = 'abtest-1'`).Scan(&count)
	if count != 1 {
		t.Errorf("插入后应可查询到 1 条, got %d", count)
	}
}

// TestFeedbackLoopMigration_SOPAgentsUseBanditColumn sop_agents 新增 use_bandit 字段
func TestFeedbackLoopMigration_SOPAgentsUseBanditColumn(t *testing.T) {
	db := setupFeedbackLoopMigrationTestDB(t)
	dropDependencyTables(t, db)
	_ = db.Exec(`CREATE TABLE sop_agents (id BIGSERIAL PRIMARY KEY, name VARCHAR(100))`).Error
	_ = db.Exec(`CREATE TABLE script_templates (id BIGSERIAL PRIMARY KEY, title VARCHAR(200))`).Error

	m := NewFeedbackLoopMigration(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}

	var hasColumn bool
	_ = db.Raw(`SELECT EXISTS (
		SELECT 1 FROM information_schema.columns
		WHERE table_name = 'sop_agents' AND column_name = 'use_bandit'
	)`).Scan(&hasColumn).Error
	if !hasColumn {
		t.Errorf("sop_agents 应有 use_bandit 列")
	}

	if err := db.Exec(`INSERT INTO sop_agents (name, use_bandit) VALUES ('test', TRUE)`).Error; err != nil {
		t.Errorf("写入 sop_agents.use_bandit 失败: %v", err)
	}
}

// TestFeedbackLoopMigration_ScriptTemplatesExtendedColumns script_templates 新增 5 个字段
func TestFeedbackLoopMigration_ScriptTemplatesExtendedColumns(t *testing.T) {
	db := setupFeedbackLoopMigrationTestDB(t)
	dropDependencyTables(t, db)
	_ = db.Exec(`CREATE TABLE sop_agents (id BIGSERIAL PRIMARY KEY, name VARCHAR(100))`).Error
	_ = db.Exec(`CREATE TABLE script_templates (id BIGSERIAL PRIMARY KEY, title VARCHAR(200))`).Error

	m := NewFeedbackLoopMigration(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}

	expectedColumns := []string{
		"source",
		"effectiveness_score",
		"trigger_keywords",
		"journey_stage",
		"champion_dialogue_id",
	}
	for _, col := range expectedColumns {
		var hasColumn bool
		_ = db.Raw(`
			SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = 'script_templates' AND column_name = ?
			)
		`, col).Scan(&hasColumn).Error
		if !hasColumn {
			t.Errorf("script_templates 应有 %s 列", col)
		}
	}

	if err := db.Exec(`
		INSERT INTO script_templates
			(title, source, effectiveness_score, trigger_keywords, journey_stage, champion_dialogue_id)
		VALUES ($1, $2, $3, $4, $5, $6)
	`,
		"test script", "champion_extract", 0.85,
		"下单,优惠", "decide", 1,
	).Error; err != nil {
		t.Errorf("写入 script_templates 扩展字段失败: %v", err)
	}
}

// TestFeedbackLoopMigration_IndexesCreated 集成测试：关键索引创建
func TestFeedbackLoopMigration_IndexesCreated(t *testing.T) {
	db := setupFeedbackLoopMigrationTestDB(t)
	dropDependencyTables(t, db)
	_ = db.Exec(`CREATE TABLE sop_agents (id BIGSERIAL PRIMARY KEY, name VARCHAR(100))`).Error
	_ = db.Exec(`CREATE TABLE script_templates (id BIGSERIAL PRIMARY KEY, title VARCHAR(200))`).Error

	m := NewFeedbackLoopMigration(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}

	expectedIndexes := []string{
		"idx_feedback_events_session",
		"idx_feedback_signals_sop_variant",
		"idx_champion_dialogues_cluster",
		"idx_prompt_candidates_node",
		"idx_bandit_arms_experiment",
		"idx_prompt_ab_tests_status",
		"idx_script_templates_source",
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

// TestFeedbackLoopMigration_BanditArmsUniqueConstraint bandit_arms 唯一约束（experiment_id + arm_key）
func TestFeedbackLoopMigration_BanditArmsUniqueConstraint(t *testing.T) {
	db := setupFeedbackLoopMigrationTestDB(t)
	dropDependencyTables(t, db)
	_ = db.Exec(`CREATE TABLE sop_agents (id BIGSERIAL PRIMARY KEY, name VARCHAR(100))`).Error
	_ = db.Exec(`CREATE TABLE script_templates (id BIGSERIAL PRIMARY KEY, title VARCHAR(200))`).Error

	m := NewFeedbackLoopMigration(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}

	if err := db.Exec(`
		INSERT INTO bandit_arms (experiment_id, experiment_type, arm_key, alpha, beta, status)
		VALUES ('exp-unique', 'prompt', 'arm_a', 2, 2, 'exploring')
	`).Error; err != nil {
		t.Fatalf("第一次插入失败: %v", err)
	}
	err := db.Exec(`
		INSERT INTO bandit_arms (experiment_id, experiment_type, arm_key, alpha, beta, status)
		VALUES ('exp-unique', 'prompt', 'arm_a', 3, 3, 'exploring')
	`).Error
	if err == nil {
		t.Errorf("重复 (experiment_id, arm_key) 一约束拒绝, got nil")
	} else {
		t.Logf("唯一约束生效, 拒绝重复插入: %v", err)
	}
}
