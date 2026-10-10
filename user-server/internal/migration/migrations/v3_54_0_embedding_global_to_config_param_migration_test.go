package migrations

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// v3.54.0 搬的是**带着密文凭证的一份 JSON**，所以用例先把"存量库"造全：
// 遗留 KV 表 + 一行管理员配好的覆盖值 + 参数中心那一行（值还是种子默认）。
// 只造一边会让 Up 的空跑被读成搬运成功。

const legacyConfigKVDDL = `CREATE TABLE IF NOT EXISTS system_config_kv (
	key        VARCHAR(100) PRIMARY KEY,
	value      TEXT NOT NULL,
	created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
)`

// newEmbeddingMoveFixture 建两张表并清掉上次残留（同一影子库里键名唯一，脏行会让断言认错对象）。
func newEmbeddingMoveFixture(t *testing.T) *gorm.DB {
	t.Helper()
	db := testutil.NewTestDB(t, &model.ConfigParam{})
	if db == nil {
		t.Fatal("测试库不可达：数据搬家无法判定（不 Skip，跳过等于没锁）")
	}
	if err := db.Exec(legacyConfigKVDDL).Error; err != nil {
		t.Fatalf("建遗留 KV 表失败: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Exec(`DELETE FROM system_config_kv WHERE key IN (?, ?)`,
			embeddingLegacyKey, embeddingLegacyKeyTombstone).Error; err != nil {
			t.Errorf("清理遗留 KV 行失败: %v", err)
		}
		if err := db.Exec(`DELETE FROM config_params WHERE param_group = ? AND key = ?`,
			embeddingParamGroup, embeddingParamKey).Error; err != nil {
			t.Errorf("清理参数行失败: %v", err)
		}
	})
	return db
}

func putLegacyConfigKV(t *testing.T, db *gorm.DB, key, value string) {
	t.Helper()
	if err := db.Exec(`INSERT INTO system_config_kv (key, value, created_at, updated_at)
		VALUES (?, ?, NOW(), NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()`,
		key, value).Error; err != nil {
		t.Fatalf("写遗留 KV 行失败: %v", err)
	}
}

func readLegacyConfigKV(t *testing.T, db *gorm.DB, key string) (string, int64) {
	t.Helper()
	var value string
	// 无行时 Scan 返回 database/sql 的 ErrNoRows（gorm 的 ErrRecordNotFound 只在 First/Take 那层出现），
	// 这里"这一行不存在"本身就是待断言的合法状态，不能当读失败。
	err := db.Raw(`SELECT value FROM system_config_kv WHERE key = ?`, key).Row().Scan(&value)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("读遗留 KV 行失败: %v", err)
	}
	var count int64
	if err := db.Raw(`SELECT count(*) FROM system_config_kv WHERE key = ?`, key).
		Row().Scan(&count); err != nil {
		t.Fatalf("数遗留 KV 行失败: %v", err)
	}
	return value, count
}

func putEmbeddingParam(t *testing.T, db *gorm.DB, value string) {
	t.Helper()
	if err := db.Exec(`DELETE FROM config_params WHERE param_group = ? AND key = ?`,
		embeddingParamGroup, embeddingParamKey).Error; err != nil {
		t.Fatalf("清参数行失败: %v", err)
	}
	if err := db.Exec(`INSERT INTO config_params
		(param_group, key, name, description, value_type, param_value, default_value, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'string', ?, ?, NOW(), NOW())`,
		embeddingParamGroup, embeddingParamKey, embeddingParamName, embeddingParamDesc, value, value).Error; err != nil {
		t.Fatalf("写参数行失败: %v", err)
	}
}

func readEmbeddingParam(t *testing.T, db *gorm.DB) (string, int64) {
	t.Helper()
	var value string
	err := db.Raw(`SELECT param_value FROM config_params WHERE param_group = ? AND key = ?`,
		embeddingParamGroup, embeddingParamKey).Row().Scan(&value)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("读参数行失败: %v", err)
	}
	var count int64
	if err := db.Raw(`SELECT count(*) FROM config_params WHERE param_group = ? AND key = ?`,
		embeddingParamGroup, embeddingParamKey).Row().Scan(&count); err != nil {
		t.Fatalf("数参数行失败: %v", err)
	}
	return value, count
}

// 管理员配好的那份云端指向（含密文 api_key）必须逐字出现在参数中心那一行里。
// 这一格是整个迁移的存在理由：少了它，换读取口等于把已配好的提供商静默退回本地栈。
func TestEmbeddingGlobalOverrideToConfigParam_CarriesOperatorOverride(t *testing.T) {
	db := newEmbeddingMoveFixture(t)
	ctx := context.Background()
	const configured = `{"enabled":true,"base_url":"https://api.siliconflow.cn/v1","api_key":"gAAAAQdm1jOlwiZmfrick1","model":"BAAI/bge-m3"}`

	putLegacyConfigKV(t, db, embeddingLegacyKey, configured)
	putEmbeddingParam(t, db, `{"enabled":false,"base_url":"","api_key":"","model":""}`)

	if err := NewEmbeddingGlobalOverrideToConfigParamMigration(db).Up(ctx); err != nil {
		t.Fatalf("Up 失败: %v", err)
	}
	got, count := readEmbeddingParam(t, db)
	if count != 1 {
		t.Fatalf("参数行数 = %d，期望恰好 1 行（多出来的是重复搬运）", count)
	}
	if got != configured {
		t.Errorf("参数中心的值没有被旧行覆盖：got %s want %s", got, configured)
	}

	// 旧行换成墓碑名，值一个字都不许动：那串 api_key 是密文，也是回滚时唯一的凭据。
	if _, n := readLegacyConfigKV(t, db, embeddingLegacyKey); n != 0 {
		t.Errorf("旧键名 %s 在 Up 后应已不存在（换墓碑名）", embeddingLegacyKey)
	}
	tv, tn := readLegacyConfigKV(t, db, embeddingLegacyKeyTombstone)
	if tn != 1 {
		t.Fatalf("墓碑行应存在，实际 %d 行", tn)
	}
	if tv != configured {
		t.Errorf("墓碑行的值被改写：got %s want %s", tv, configured)
	}
}

// 幂等：换库、补跑都会重复执行。第二次既不能改值，也不能因主键冲突而报错。
func TestEmbeddingGlobalOverrideToConfigParam_Idempotent(t *testing.T) {
	db := newEmbeddingMoveFixture(t)
	ctx := context.Background()
	const configured = `{"enabled":true,"base_url":"https://dashscope.aliyuncs.com/compatible-mode/v1","api_key":"x","model":"text-embedding-v3"}`

	putLegacyConfigKV(t, db, embeddingLegacyKey, configured)
	putEmbeddingParam(t, db, `{"enabled":false}`)

	m := NewEmbeddingGlobalOverrideToConfigParamMigration(db)
	if err := m.Up(ctx); err != nil {
		t.Fatalf("首次 Up 失败: %v", err)
	}
	first, firstCount := readEmbeddingParam(t, db)
	if err := m.Up(ctx); err != nil {
		t.Fatalf("二次 Up 失败: %v", err)
	}
	second, secondCount := readEmbeddingParam(t, db)
	if first != second {
		t.Errorf("二次 Up 改写了值：first=%s second=%s", first, second)
	}
	if firstCount != secondCount || secondCount != 1 {
		t.Errorf("二次 Up 造出多余参数行：first=%d second=%d", firstCount, secondCount)
	}
	if _, n := readLegacyConfigKV(t, db, embeddingLegacyKeyTombstone); n != 1 {
		t.Errorf("二次 Up 后墓碑行应仍在 1 行")
	}
}

// 参数行还没被种子建出来（seed 失败/装配顺序变化）时，Up 要自己补一行，
// 而不是把旧值丢在地上等一次永远不会再来的重跑。
func TestEmbeddingGlobalOverrideToConfigParam_CreatesMissingParamRow(t *testing.T) {
	db := newEmbeddingMoveFixture(t)
	ctx := context.Background()
	const configured = `{"enabled":true,"base_url":"https://api.example.com/v1","api_key":"k","model":"m3"}`

	putLegacyConfigKV(t, db, embeddingLegacyKey, configured)
	if err := db.Exec(`DELETE FROM config_params WHERE param_group = ? AND key = ?`,
		embeddingParamGroup, embeddingParamKey).Error; err != nil {
		t.Fatalf("清参数行失败: %v", err)
	}

	if err := NewEmbeddingGlobalOverrideToConfigParamMigration(db).Up(ctx); err != nil {
		t.Fatalf("Up 失败: %v", err)
	}
	got, count := readEmbeddingParam(t, db)
	if count != 1 {
		t.Fatalf("参数行数 = %d，期望 1（缺行时应由迁移补建）", count)
	}
	if got != configured {
		t.Errorf("补建的参数行值 = %s，期望 %s", got, configured)
	}
}

// 全新库路径：没有旧行时不许无中生有，也不许把已有参数值刷没。
func TestEmbeddingGlobalOverrideToConfigParam_NoLegacyRowKeepsParam(t *testing.T) {
	db := newEmbeddingMoveFixture(t)
	ctx := context.Background()
	const seeded = `{"enabled":false,"base_url":"","api_key":"","model":""}`

	putEmbeddingParam(t, db, seeded)
	if err := NewEmbeddingGlobalOverrideToConfigParamMigration(db).Up(ctx); err != nil {
		t.Fatalf("Up 失败: %v", err)
	}
	if got, count := readEmbeddingParam(t, db); count != 1 || got != seeded {
		t.Errorf("无旧行时参数行应原样保留：count=%d value=%s", count, got)
	}

	// 旧行存在但值为空串：等同于"没配过"，同样不许写穿参数行。
	putLegacyConfigKV(t, db, embeddingLegacyKey, "")
	if err := NewEmbeddingGlobalOverrideToConfigParamMigration(db).Up(ctx); err != nil {
		t.Fatalf("空旧值 Up 失败: %v", err)
	}
	if got, _ := readEmbeddingParam(t, db); got != seeded {
		t.Errorf("旧值为空串时参数行应保持不变：got %s want %s", got, seeded)
	}
}

// 旧行存在但值为空串、且参数行还没建：不该补出一行"看起来能改、改了没人读"的参数。
func TestEmbeddingGlobalOverrideToConfigParam_EmptyLegacyDoesNotCreateRow(t *testing.T) {
	db := newEmbeddingMoveFixture(t)
	ctx := context.Background()

	putLegacyConfigKV(t, db, embeddingLegacyKey, "")
	if err := db.Exec(`DELETE FROM config_params WHERE param_group = ? AND key = ?`,
		embeddingParamGroup, embeddingParamKey).Error; err != nil {
		t.Fatalf("清参数行失败: %v", err)
	}
	if err := NewEmbeddingGlobalOverrideToConfigParamMigration(db).Up(ctx); err != nil {
		t.Fatalf("Up 失败: %v", err)
	}
	if _, count := readEmbeddingParam(t, db); count != 0 {
		t.Errorf("旧值为空串时不该补建参数行：count=%d，期望 0", count)
	}
}

// 墓碑位已被占（上一次迁移跑过、又被人为复制了一行）时，第三条语句必须让位而不是撞主键。
// 撞了就是启动期迁移失败，而这一格与"搬没搬成功"毫无关系。
func TestEmbeddingGlobalOverrideToConfigParam_TombstoneTakenIsNotAnError(t *testing.T) {
	db := newEmbeddingMoveFixture(t)
	ctx := context.Background()
	const configured = `{"enabled":true,"base_url":"https://api.example.com/v1","api_key":"k","model":"m3"}`

	putLegacyConfigKV(t, db, embeddingLegacyKey, configured)
	putLegacyConfigKV(t, db, embeddingLegacyKeyTombstone, `{"enabled":false}`)
	putEmbeddingParam(t, db, `{"enabled":false}`)

	if err := NewEmbeddingGlobalOverrideToConfigParamMigration(db).Up(ctx); err != nil {
		t.Fatalf("墓碑位被占时 Up 应跳过改名而非报错: %v", err)
	}
	if got, _ := readEmbeddingParam(t, db); got != configured {
		t.Errorf("参数值仍应被搬过来：got %s want %s", got, configured)
	}
	if _, n := readLegacyConfigKV(t, db, embeddingLegacyKey); n != 1 {
		t.Errorf("墓碑位被占时旧行应留在原键名上（改名让位），实际行数 = %d", n)
	}
}

// 缺表必须安静跳过：这套部署可能从没写过全局 embedding 覆盖，也可能参数中心还没建起来，
// 报错会让启动期的同步迁移直接失败。
func TestEmbeddingGlobalOverrideToConfigParam_MissingTableIsNoop(t *testing.T) {
	db := testutil.NewTestDB(t, &model.ConfigParam{})
	if db == nil {
		t.Fatal("测试库不可达：缺表分支无法判定")
	}
	if err := db.Exec(`DROP TABLE IF EXISTS system_config_kv`).Error; err != nil {
		t.Fatalf("删遗留 KV 表失败: %v", err)
	}
	m := NewEmbeddingGlobalOverrideToConfigParamMigration(db)
	ctx := context.Background()
	if err := m.Up(ctx); err != nil {
		t.Errorf("缺 system_config_kv 时 Up 应跳过而非报错: %v", err)
	}
	if err := m.Down(ctx); err != nil {
		t.Errorf("缺 system_config_kv 时 Down 应跳过而非报错: %v", err)
	}
}

// Down 只把键名换回来、不回滚参数值：把活数据覆盖回搬来之前的快照会吃掉升级后的改动。
func TestEmbeddingGlobalOverrideToConfigParam_DownKeepsParamValue(t *testing.T) {
	db := newEmbeddingMoveFixture(t)
	ctx := context.Background()
	const configured = `{"enabled":true,"base_url":"https://api.example.com/v1","api_key":"k","model":"m3"}`

	putLegacyConfigKV(t, db, embeddingLegacyKey, configured)
	putEmbeddingParam(t, db, `{"enabled":false}`)
	m := NewEmbeddingGlobalOverrideToConfigParamMigration(db)
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up 失败: %v", err)
	}
	// 升级之后管理员又在参数中心改了一次：Down 不许动这一格。
	const later = `{"enabled":true,"base_url":"https://second.example.com/v1","api_key":"k2","model":"m4"}`
	if err := db.Exec(`UPDATE config_params SET param_value = $1 WHERE param_group = $2 AND key = $3`,
		later, embeddingParamGroup, embeddingParamKey).Error; err != nil {
		t.Fatalf("改参数值失败: %v", err)
	}

	if err := m.Down(ctx); err != nil {
		t.Fatalf("Down 失败: %v", err)
	}
	if got, _ := readEmbeddingParam(t, db); got != later {
		t.Errorf("Down 不应回滚参数值：got %s want %s", got, later)
	}
	if v, n := readLegacyConfigKV(t, db, embeddingLegacyKey); n != 1 || v != configured {
		t.Errorf("Down 后旧行应回到原键名与原值：rows=%d value=%s", n, v)
	}
	if _, n := readLegacyConfigKV(t, db, embeddingLegacyKeyTombstone); n != 0 {
		t.Errorf("Down 后墓碑键名不应残留，实际 %d 行", n)
	}
}

func TestEmbeddingGlobalOverrideToConfigParam_Meta(t *testing.T) {
	m := NewEmbeddingGlobalOverrideToConfigParamMigration(nil)
	if m.Version() != "v3.54.0" {
		t.Errorf("Version()=%q want=v3.54.0", m.Version())
	}
	if m.Name() == "" || m.Description() == "" {
		t.Error("Name/Description should not be empty")
	}
}

func TestEmbeddingGlobalOverrideToConfigParam_NilDB(t *testing.T) {
	m := NewEmbeddingGlobalOverrideToConfigParamMigration(nil)
	if err := m.Up(context.Background()); err == nil {
		t.Error("nil db Up() 应返回错误")
	}
	if err := m.Down(context.Background()); err == nil {
		t.Error("nil db Down() 应返回错误")
	}
}
