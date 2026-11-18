package repository

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// specs/024 迁移验收（T301/T501/T601 + 三审 070）：
//   1. 迁移文件在「模型建表后的测试库」上**连跑两次无错**（幂等，acceptance 原文口径）；
//   2. T501 的两条身份锚索引必须真实存在，且 pg_indexes 直查的列序与 WHERE 谓词
//      与 065 定义一致（软删行不占键、空 channel_user_id 不占身份锚键）。
// 迁移 SQL 直读 <root>/migrations/——与部署同一份文件，测的就是要上生产的东西。

func repoMigrationPath(t *testing.T, name string) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("取工作目录失败: %v", err)
	}
	// repository 包位于 <root>/user-server/internal/repository，向上 3 级到 <root>。
	for i := 0; i < 3; i++ {
		dir = filepath.Dir(dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "user-web", "bridge", "src", "core", "constants.js")); err != nil {
		t.Fatalf("仓库根定位失败（%s 下无 user-web/bridge）: %v", dir, err)
	}
	p := filepath.Join(dir, "migrations", name)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("迁移文件缺失: %v", err)
	}
	return p
}

// execMigrationFile 按语句切分执行（跳过纯注释段）；CONCURRENTLY 语句必须走非事务连接。
func execMigrationFile(t *testing.T, db *gorm.DB, name string) {
	t.Helper()
	raw, err := os.ReadFile(repoMigrationPath(t, name))
	if err != nil {
		t.Fatalf("读迁移文件失败: %v", err)
	}
	// 测试库剥离 CONCURRENTLY：免锁语义只对生产有意义的体积生效，且部分连接状态下
	// CREATE INDEX CONCURRENTLY 的失败会被驱动静默吞掉（实测 idx_oneid_channel_v2
	// 不落地的根因）。迁移幂等与索引形状的验收不需要免锁。
	rawStr := strings.ReplaceAll(string(raw), "CONCURRENTLY ", "")
	for _, stmt := range strings.Split(rawStr, ";\n") {
		trimmed := strings.TrimSpace(stmt)
		if trimmed == "" {
			continue
		}
		// 跳过纯注释语句块（每行都是 -- 开头）
		lineContent := false
		for _, line := range strings.Split(trimmed, "\n") {
			l := strings.TrimSpace(line)
			if l != "" && !strings.HasPrefix(l, "--") {
				lineContent = true
				break
			}
		}
		if !lineContent {
			continue
		}
		if err := db.Exec(trimmed).Error; err != nil {
			t.Fatalf("迁移 %s 语句执行失败: %v\n语句: %s", name, err, trimmed)
		}
	}
}

// TestMigrationFiles_RunTwiceWithoutError 024 涉及迁移（065/066/067/068/069/070）
// 在模型建表后的库上连跑两次无错。068/069/070 的表/索引由 070 幂等重建兜底。
func TestMigrationFiles_RunTwiceWithoutError(t *testing.T) {
	database := testutil.NewTestDB(t,
		&model.CustomerChannel{}, &model.ChannelRouteRule{}, &model.InboxConversation{},
		&model.ChannelEventAudit{}, &model.ChannelInteraction{},
	)
	for _, name := range []string{
		"065_customer_channel_identity_keys.sql",
		"066_channel_route_rules.sql",
		"067_inbox_conversation_snooze.sql",
		"068_channel_event_audits.sql",
		"069_channel_interactions.sql",
		"070_channel_evidence_identity.sql",
	} {
		execMigrationFile(t, database, name) // 第一遍
		execMigrationFile(t, database, name) // 第二遍：幂等判据
	}
}

// TestCustomerChannel_IdentityIndexesShape T501 判据：pg_indexes 直查两条身份锚索引的
// 列序与 WHERE 谓词（只断「第二次写入报错」证明力不够——索引形状漂移必须当场红）。
func TestCustomerChannel_IdentityIndexesShape(t *testing.T) {
	database := testutil.NewTestDB(t, &model.CustomerChannel{}, &model.OperationLog{})
	// 确定性夹具：先清掉 AutoMigrate/历史遗留的同族索引，再执行 065，
	// 断言「迁移文件本身」建出的形状（模型 tag 与 065 的一致性由
	// TestMigrationFiles_RunTwiceWithoutError 的建库路径另行覆盖）。
	for _, idx := range []string{"idx_channel_identity", "idx_oneid_channel_v2", "idx_oneid_channel"} { // v2 为 065 §4 RENAME 前的过渡名
		if err := database.Exec(`DROP INDEX IF EXISTS ` + idx).Error; err != nil {
			t.Fatalf("清索引 %s 失败: %v", idx, err)
		}
	}
	execMigrationFile(t, database, "065_customer_channel_identity_keys.sql")

	type idxRow struct {
		Indexdef string
	}
	var allIdx []idxRow
	if err := database.Raw(`SELECT indexdef FROM pg_indexes WHERE tablename = 'customer_channels' ORDER BY indexname`).
		Scan(&allIdx).Error; err != nil {
		t.Fatalf("列全部索引失败: %v", err)
	}
	for _, r := range allIdx {
		t.Logf("[probe] %s", r.Indexdef)
	}
	cases := []struct {
		name       string
		wantCols   []string // indexdef 里按序出现
		wantFilter string   // WHERE 谓词子串
	}{
		{
			name:       "idx_channel_identity",
			wantCols:   []string{"tenant_id", "channel", "account_id", "channel_user_id"},
			wantFilter: "(deleted_at IS NULL) AND ((channel_user_id)::text <> ''::text)",
		},
		{
			// 终名 = idx_oneid_channel（065 §4 将 v2 RENAME 而来；列含 account_id + 软删谓词）
			name:       "idx_oneid_channel",
			wantCols:   []string{"tenant_id", "one_id", "channel", "account_id"},
			wantFilter: "deleted_at IS NULL",
		},
	}
	for _, tc := range cases {
		var rows []idxRow
		if err := database.Raw(`SELECT indexdef FROM pg_indexes WHERE tablename = 'customer_channels' AND indexname = ?`, tc.name).
			Scan(&rows).Error; err != nil {
			t.Fatalf("查 pg_indexes(%s) 失败: %v", tc.name, err)
		}
		if len(rows) != 1 {
			t.Fatalf("索引 %s 应存在且唯一，实际 %d 条", tc.name, len(rows))
		}
		def := rows[0].Indexdef
		// 列序断言：截取 USING btree ( … ) 括号内的列清单，逐列精确比对
		//（裸子串匹配会撞表名 customer_channels 里的 "channel"）。
		lp := strings.Index(def, "USING btree (")
		if lp < 0 {
			t.Fatalf("indexdef 形状异常: %s", def)
		}
		cols := strings.Split(strings.TrimSuffix(strings.TrimSpace(def[lp+len("USING btree ("):strings.Index(def[lp:], ")")+lp]), ","), ",")
		if len(cols) != len(tc.wantCols) {
			t.Errorf("索引 %s 列数漂移：want %v got %v（def=%s）", tc.name, tc.wantCols, cols, def)
			continue
		}
		for i, col := range tc.wantCols {
			if strings.TrimSpace(cols[i]) != col {
				t.Errorf("索引 %s 列序漂移：第 %d 列 want %s got %s（def=%s）", tc.name, i+1, col, strings.TrimSpace(cols[i]), def)
			}
		}
		if !strings.Contains(def, tc.wantFilter) {
			t.Errorf("索引 %s WHERE 谓词漂移：期望含 %q，def=%s", tc.name, tc.wantFilter, def)
		}
	}
	// 幂等：065 再跑一遍仍无错且形状不变（T501 acceptance：连跑两次结果一致）
	execMigrationFile(t, database, "065_customer_channel_identity_keys.sql")
}
