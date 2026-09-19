// knowledge_base_version_start_test.go T-P2-05（G-1）：新建 KB 的起始版本号到底是谁在兜。
//
// 这条测试是被变异电池逼出来的：把 CreateKB 里的 `kb.Version == 0 ⇒ 1` 摘掉后，原有全部
// 用例照样绿。原因不是断言太弱，而是那句归一**本来就是陪跑**（探针实测，非推断）：
//   - `ALTER TABLE … ALTER COLUMN version DROP DEFAULT` 之后，information_schema 的
//     column_default 变 NULL、pg_attrdef 少一条 ⇒ DDL 默认值确实没了；
//   - 此时**绕开 service** 只走仓储 `Select("*").Create`（struct.Version 仍是零值），
//     INSERT 既不报错、库里也读到 1；
//   - 该列是 NOT NULL，默认值又已摘除 ⇒ 这个 1 只可能来自客户端。
//
// ⇒ GORM 按 `gorm:"default:1"` 标签在写入侧自己填了默认值，与 DB 默认值无关。
//
// 所以要锁的是"新 KB 从 1 号起"这个**行为**（两路都验），而不是假装 service 那行是唯一来源。
// 顺带把 GORM 这条内部行为钉成断言：哪天升级 GORM 改了填值时机，本文件会红，
// 而不是让线上管理端默默多出一些 version=0 的库。
package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// dropKBVersionColumnDefault 摘掉 knowledge_bases.version 的列默认值并注册还原，
// 返回"库里确实没有默认值了"的实测判定（两条来源都读，避免单条查询空返回造成假判定）。
func dropKBVersionColumnDefault(t *testing.T, database *gorm.DB) bool {
	t.Helper()
	if err := database.Exec(`ALTER TABLE knowledge_bases ALTER COLUMN version DROP DEFAULT`).Error; err != nil {
		t.Fatalf("DROP DEFAULT 失败: %v", err)
	}
	t.Cleanup(func() {
		// 还原无条件执行：这张表是全包共享的进程级库，默认值停在"被摘掉"的状态会污染后续用例
		// （_LegacyRowsDefaultToOne 就是直读 information_schema 钉默认值的）。
		if err := database.Exec(`ALTER TABLE knowledge_bases ALTER COLUMN version SET DEFAULT 1`).Error; err != nil {
			t.Errorf("还原列默认值失败: %v（会污染同库后续用例）", err)
		}
	})
	var defaultRows int64
	if err := database.Raw(`SELECT COUNT(*) FROM information_schema.columns
		WHERE table_name = 'knowledge_bases' AND column_name = 'version'
		  AND column_default IS NOT NULL`).Scan(&defaultRows).Error; err != nil {
		t.Fatalf("读 column_default: %v", err)
	}
	var attrdefRows int64
	if err := database.Raw(`SELECT COUNT(*) FROM pg_attrdef d
		JOIN pg_class c ON c.oid = d.adrelid
		JOIN pg_attribute a ON a.attrelid = d.adrelid AND a.attnum = d.adnum
		WHERE c.relname = 'knowledge_bases' AND a.attname = 'version'`).Scan(&attrdefRows).Error; err != nil {
		t.Fatalf("读 pg_attrdef: %v", err)
	}
	return defaultRows == 0 && attrdefRows == 0
}

// newKBWithoutVersion 造一行"没带版本号"的新 KB（等价于运营在管理端点一下新建）。
func newKBWithoutVersion(code string) *model.KnowledgeBase {
	return &model.KnowledgeBase{
		KBCode: code, Type: model.KnowledgeBaseTypeFAQ, Name: "起号对照库",
		OwnerType: model.KnowledgeBaseOwnerShared, Enabled: boolPtr(true),
	}
}

func readKBVersion(t *testing.T, database *gorm.DB, id uint) int {
	t.Helper()
	var version int
	if err := database.Raw(`SELECT version FROM knowledge_bases WHERE id = ?`, id).
		Scan(&version).Error; err != nil {
		t.Fatalf("读 version: %v", err)
	}
	return version
}

func TestKBVersionStart_NewKBStartsAtOneOnBothPaths(t *testing.T) {
	database := setupKBCanaryDB(t)
	ctx := context.Background()
	svc := NewKnowledgeBaseService(database)

	if !dropKBVersionColumnDefault(t, database) {
		t.Fatal("列默认值没被摘掉 ⇒ 下面两条断言都退化成「DDL 兜的」，本用例什么都没测")
	}

	// 路径 1（对照，也是钉 GORM 行为的那条）：绕开 service 只走仓储。
	// 期望仍是 1 —— 若哪天 GORM 不再按 tag 客户端填默认值，这里会变 0 或直接 NOT NULL 报错，
	// 两种结果都会把本文件顶到眼前，而不是留一句"service 有归一所以没事"的口头保证。
	repoOnly := newKBWithoutVersion("kb-version-start-repo-only")
	if err := svc.repo.Create(ctx, repoOnly); err != nil {
		t.Fatalf("仓储 Create（默认值已摘）: %v", err)
	}
	if got := readKBVersion(t, database, repoOnly.ID); got != 1 {
		t.Errorf("绕开 service 只走仓储时 version=%d，期望 1（GORM 按 default:1 标签客户端填值）", got)
	}
	if repoOnly.Version != 1 {
		t.Errorf("仓储 Create 后 struct.Version=%d，期望 1（回填也要成立，调用方直接读这个值）", repoOnly.Version)
	}

	// 路径 2（契约）：走 service，同样必须从 1 号起。
	viaService := newKBWithoutVersion("kb-version-start-via-service")
	if err := svc.CreateKB(ctx, viaService); err != nil {
		t.Fatalf("CreateKB: %v", err)
	}
	if viaService.Version != 1 || readKBVersion(t, database, viaService.ID) != 1 {
		t.Errorf("CreateKB 应交出 1 号，got struct=%d db=%d",
			viaService.Version, readKBVersion(t, database, viaService.ID))
	}
}

func TestKBVersionStart_ExplicitVersionSurvivesTheFold(t *testing.T) {
	database := setupKBCanaryDB(t)
	ctx := context.Background()
	svc := NewKnowledgeBaseService(database)
	if !dropKBVersionColumnDefault(t, database) {
		t.Fatal("列默认值没被摘掉 ⇒ 显式号与默认号分不开，本用例什么都没测")
	}

	// 归一只管零值：显式带着版本号的建行不许被动过（迁移导入/双写场景要保号）。
	// 这条同时证明"路径 1 得到的 1"不是某种强制覆盖：能写 7 就说明写的是调用方给的值。
	kb := newKBWithoutVersion("kb-version-start-explicit")
	kb.Version = 7
	if err := svc.CreateKB(ctx, kb); err != nil {
		t.Fatalf("CreateKB: %v", err)
	}
	if kb.Version != 7 || readKBVersion(t, database, kb.ID) != 7 {
		t.Errorf("显式 version=7 不该被挪动，got struct=%d db=%d",
			kb.Version, readKBVersion(t, database, kb.ID))
	}
}
