package migrations

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// v3.53.0 搬的是**数据**，所以用例必须先造出"存量库"：遗留 KV 表 + 一行运维改过的值 +
// 参数中心那一行（值还是种子默认）。只造一边会让 Up 的空跑被读成搬运成功。

const legacyKVDDL = `CREATE TABLE IF NOT EXISTS system_kv_config (
	id          BIGSERIAL PRIMARY KEY,
	key         VARCHAR(128) NOT NULL UNIQUE,
	value       TEXT NOT NULL DEFAULT '',
	value_type  VARCHAR(20) NOT NULL DEFAULT 'json',
	description VARCHAR(500) DEFAULT '',
	updated_by  BIGINT DEFAULT 0,
	created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`

// newFailoverMoveFixture 建两张表并清掉上次残留（同一影子库里 key 唯一，脏行会让断言认错对象）。
func newFailoverMoveFixture(t *testing.T) *gorm.DB {
	t.Helper()
	db := testutil.NewTestDB(t, &model.ConfigParam{})
	if db == nil {
		t.Fatal("测试库不可达：数据搬家无法判定（不 Skip，跳过等于没锁）")
	}
	if err := db.Exec(legacyKVDDL).Error; err != nil {
		t.Fatalf("建遗留 KV 表失败: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Exec(`DELETE FROM system_kv_config WHERE key = ?`, failoverLegacyKey).Error; err != nil {
			t.Errorf("清理遗留 KV 行失败: %v", err)
		}
		if err := db.Exec(`DELETE FROM config_params WHERE param_group = ? AND key = ?`,
			failoverParamGroup, failoverParamKey).Error; err != nil {
			t.Errorf("清理参数行失败: %v", err)
		}
	})
	return db
}

func putLegacyKV(t *testing.T, db *gorm.DB, value string) {
	t.Helper()
	if err := db.Exec(`INSERT INTO system_kv_config (key, value, value_type, description)
		VALUES (?, ?, 'json', 'LLM Provider 降级策略配置')
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, description = EXCLUDED.description`,
		failoverLegacyKey, value).Error; err != nil {
		t.Fatalf("写遗留 KV 行失败: %v", err)
	}
}

func putFailoverParam(t *testing.T, db *gorm.DB, value string) {
	t.Helper()
	if err := db.Exec(`DELETE FROM config_params WHERE param_group = ? AND key = ?`,
		failoverParamGroup, failoverParamKey).Error; err != nil {
		t.Fatalf("清参数行失败: %v", err)
	}
	if err := db.Exec(`INSERT INTO config_params
		(param_group, key, name, description, value_type, param_value, default_value, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'string', ?, ?, NOW(), NOW())`,
		failoverParamGroup, failoverParamKey, failoverParamName, failoverParamDesc, value, value).Error; err != nil {
		t.Fatalf("写参数行失败: %v", err)
	}
}

func readFailoverParam(t *testing.T, db *gorm.DB) (string, int64) {
	t.Helper()
	var value string
	// 无行时 Scan 返回的是 database/sql 的 ErrNoRows（gorm 的 ErrRecordNotFound 只在
	// First/Take 那层出现），这里"没有参数行"本身就是要断言的合法状态，不能当读失败。
	if err := db.Raw(`SELECT param_value FROM config_params WHERE param_group = ? AND key = ?`,
		failoverParamGroup, failoverParamKey).Row().Scan(&value); err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("读参数行失败: %v", err)
	}
	var count int64
	if err := db.Raw(`SELECT count(*) FROM config_params WHERE param_group = ? AND key = ?`,
		failoverParamGroup, failoverParamKey).Row().Scan(&count); err != nil {
		t.Fatalf("数参数行失败: %v", err)
	}
	return value, count
}

func readLegacyKVDescription(t *testing.T, db *gorm.DB) string {
	t.Helper()
	var d string
	if err := db.Raw(`SELECT description FROM system_kv_config WHERE key = ?`, failoverLegacyKey).
		Row().Scan(&d); err != nil {
		t.Fatalf("读遗留 KV 说明失败: %v", err)
	}
	return d
}

func TestFailoverPolicyToConfigParamMigration_Meta(t *testing.T) {
	m := NewFailoverPolicyToConfigParamMigration(nil)
	if m.Version() != "v3.53.0" {
		t.Errorf("Version()=%q want=v3.53.0", m.Version())
	}
	if m.Name() == "" || m.Description() == "" {
		t.Error("Name/Description should not be empty")
	}
}

func TestFailoverPolicyToConfigParamMigration_NilDB(t *testing.T) {
	m := NewFailoverPolicyToConfigParamMigration(nil)
	if err := m.Up(context.Background()); err == nil {
		t.Error("nil db Up() 应返回错误")
	}
	if err := m.Down(context.Background()); err == nil {
		t.Error("nil db Down() 应返回错误")
	}
}

// 核心一格：运维手改过的旧值必须出现在参数中心那一行里。
// 这一格是整个迁移的存在理由——少了它，换读取口等于把存量覆盖值静默清零。
func TestFailoverPolicyToConfigParamMigration_CarriesOperatorOverride(t *testing.T) {
	db := newFailoverMoveFixture(t)
	ctx := context.Background()
	const overridden = `{"config":{"failure_threshold":2},"scenarios":{"sop_reply":["slow-provider"]}}`

	putLegacyKV(t, db, overridden)
	putFailoverParam(t, db, `{"config":{"failure_threshold":5}}`)

	if err := NewFailoverPolicyToConfigParamMigration(db).Up(ctx); err != nil {
		t.Fatalf("Up 失败: %v", err)
	}
	got, count := readFailoverParam(t, db)
	if count != 1 {
		t.Fatalf("参数行数 = %d，期望恰好 1 行（多出来的是重复搬运）", count)
	}
	if got != overridden {
		t.Errorf("参数中心的值没有被旧行覆盖：got %s", got)
	}
	if desc := readLegacyKVDescription(t, db); !strings.HasPrefix(desc, "已废弃") {
		t.Errorf("旧行说明应标为已废弃，got %q", desc)
	}
	// 旧行的值本身不许被动过：它是这次搬家的出处，也是回滚时的唯一凭据。
	var legacyValue string
	if err := db.Raw(`SELECT value FROM system_kv_config WHERE key = ?`, failoverLegacyKey).
		Row().Scan(&legacyValue); err != nil {
		t.Fatalf("读旧行值失败: %v", err)
	}
	if legacyValue != overridden {
		t.Errorf("旧行值被改写：got %s want %s", legacyValue, overridden)
	}
}

// 幂等：迁移记录之外还会有重复执行（换库、补跑），第二次必须是零效果。
func TestFailoverPolicyToConfigParamMigration_Idempotent(t *testing.T) {
	db := newFailoverMoveFixture(t)
	ctx := context.Background()
	const overridden = `{"config":{"failure_threshold":2}}`

	putLegacyKV(t, db, overridden)
	putFailoverParam(t, db, `{"config":{"failure_threshold":5}}`)

	m := NewFailoverPolicyToConfigParamMigration(db)
	if err := m.Up(ctx); err != nil {
		t.Fatalf("首次 Up 失败: %v", err)
	}
	first, firstCount := readFailoverParam(t, db)
	if err := m.Up(ctx); err != nil {
		t.Fatalf("二次 Up 失败: %v", err)
	}
	second, secondCount := readFailoverParam(t, db)
	if first != second {
		t.Errorf("二次 Up 改写了值：first=%s second=%s", first, second)
	}
	if firstCount != secondCount || secondCount != 1 {
		t.Errorf("二次 Up 造出多余参数行：first=%d second=%d", firstCount, secondCount)
	}
	if desc := readLegacyKVDescription(t, db); !strings.HasPrefix(desc, "已废弃") {
		t.Errorf("二次 Up 后旧行说明应仍是已废弃，got %q", desc)
	}
}

// 参数行还没被种子建出来（seed 失败/装配顺序变化）时，Up 要自己补一行，
// 而不是把旧值丢在地上等一次永远不会再来的重跑。
func TestFailoverPolicyToConfigParamMigration_CreatesMissingParamRow(t *testing.T) {
	db := newFailoverMoveFixture(t)
	ctx := context.Background()
	const overridden = `{"config":{"failure_threshold":7}}`

	putLegacyKV(t, db, overridden)
	if err := db.Exec(`DELETE FROM config_params WHERE param_group = ? AND key = ?`,
		failoverParamGroup, failoverParamKey).Error; err != nil {
		t.Fatalf("清参数行失败: %v", err)
	}

	if err := NewFailoverPolicyToConfigParamMigration(db).Up(ctx); err != nil {
		t.Fatalf("Up 失败: %v", err)
	}
	got, count := readFailoverParam(t, db)
	if count != 1 {
		t.Fatalf("参数行数 = %d，期望 1（缺行时应由迁移补建）", count)
	}
	if got != overridden {
		t.Errorf("补建的参数行值 = %s，期望 %s", got, overridden)
	}
}

// 全新库路径：没有旧行（或旧值为空）时不许无中生有，也不许把已有参数值刷成空串。
func TestFailoverPolicyToConfigParamMigration_NoLegacyRowKeepsParam(t *testing.T) {
	db := newFailoverMoveFixture(t)
	ctx := context.Background()
	const seeded = `{"config":{"failure_threshold":5}}`

	putFailoverParam(t, db, seeded)

	if err := NewFailoverPolicyToConfigParamMigration(db).Up(ctx); err != nil {
		t.Fatalf("Up 失败: %v", err)
	}
	got, count := readFailoverParam(t, db)
	if count != 1 || got != seeded {
		t.Errorf("无旧行时参数行应原样保留：count=%d value=%s", count, got)
	}

	// 旧行存在但值为空串：等同于"没有覆盖"，同样不许写穿参数行。
	putLegacyKV(t, db, "")
	if err := NewFailoverPolicyToConfigParamMigration(db).Up(ctx); err != nil {
		t.Fatalf("空旧值 Up 失败: %v", err)
	}
	if got, _ := readFailoverParam(t, db); got != seeded {
		t.Errorf("旧值为空串时参数行应保持不变：got %s want %s", got, seeded)
	}
}

// 旧行存在但值为空串、且参数行还没建：不许用空串补出一行"看起来能改、改了没人读"的参数。
func TestFailoverPolicyToConfigParamMigration_EmptyLegacyDoesNotCreateRow(t *testing.T) {
	db := newFailoverMoveFixture(t)
	ctx := context.Background()

	putLegacyKV(t, db, "")
	if err := db.Exec(`DELETE FROM config_params WHERE param_group = ? AND key = ?`,
		failoverParamGroup, failoverParamKey).Error; err != nil {
		t.Fatalf("清参数行失败: %v", err)
	}
	if err := NewFailoverPolicyToConfigParamMigration(db).Up(ctx); err != nil {
		t.Fatalf("Up 失败: %v", err)
	}
	if _, count := readFailoverParam(t, db); count != 0 {
		t.Errorf("旧值为空串时不该补建参数行：count=%d，期望 0", count)
	}
}

// 缺表必须安静跳过：这套部署可能从没跑过 p1，也可能参数中心还没建起来，
// 报错会让启动期的同步等待直接失败（ExecuteUpgrade 失败是 ERROR 日志 + 迁移任务 failed）。
func TestFailoverPolicyToConfigParamMigration_MissingTableIsNoop(t *testing.T) {
	db := testutil.NewTestDB(t, &model.ConfigParam{})
	if db == nil {
		t.Fatal("测试库不可达：缺表分支无法判定")
	}
	if err := db.Exec(`DROP TABLE IF EXISTS system_kv_config`).Error; err != nil {
		t.Fatalf("删遗留 KV 表失败: %v", err)
	}
	m := NewFailoverPolicyToConfigParamMigration(db)
	ctx := context.Background()
	if err := m.Up(ctx); err != nil {
		t.Errorf("缺 system_kv_config 时 Up 应跳过而非报错: %v", err)
	}
	if err := m.Down(ctx); err != nil {
		t.Errorf("缺 system_kv_config 时 Down 应跳过而非报错: %v", err)
	}
}

// Down 只摘废弃说明、不回滚参数值：把活数据覆盖回搬来之前的快照会吃掉升级后的改动。
func TestFailoverPolicyToConfigParamMigration_DownKeepsParamValue(t *testing.T) {
	db := newFailoverMoveFixture(t)
	ctx := context.Background()
	const overridden = `{"config":{"failure_threshold":2}}`

	putLegacyKV(t, db, overridden)
	putFailoverParam(t, db, `{"config":{"failure_threshold":5}}`)
	m := NewFailoverPolicyToConfigParamMigration(db)
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up 失败: %v", err)
	}
	if err := m.Down(ctx); err != nil {
		t.Fatalf("Down 失败: %v", err)
	}
	if got, _ := readFailoverParam(t, db); got != overridden {
		t.Errorf("Down 不应回滚参数值：got %s want %s", got, overridden)
	}
	if desc := readLegacyKVDescription(t, db); strings.HasPrefix(desc, "已废弃") {
		t.Errorf("Down 后旧行说明应摘掉废弃标记，got %q", desc)
	}
}
