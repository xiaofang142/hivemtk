// kb_version_canary_test.go T-P2-05（G-1 知识库版本 + 灰度）的仓储层用例。
//
// 本文件只测三件"看代码看不出来、但将来一定会绊到人"的事：
//  1. 补列后存量行的默认值真是 1（不是 NULL、不是 0）——"未开灰度逐条一致"（AC③）
//     的根基就在这一个数上；列可空性也顺手钉住，可空的 int 读回 NULL 是转换错误。
//  2. UpdateVersionCanary **不 bump updated_at**（AC② 的命门：knowledge_bases.updated_at
//     是 rag_answer_cache 的失效信号，bump 一次就把另一个版本的缓存行整批删掉）。
//     同时用对照断言证明"走通用 Update 就会 bump"，即这条性质不是碰巧成立。
//  3. 只动三列，别的一概不碰（防止有人日后把版本写进通用 Update 的列映射里）。
package repository

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// kbVerOwnerPrefix 给本文件一个专属 kb_code 前缀：NewTestDB 用的是包级共享测试库，
// knowledge_bases 里同类型行会随其它用例增长，"恰好 N 行"式的断言必须靠唯一前缀隔离。
const kbVerOwnerPrefix = "kb-ver-t-p2-05-"

func setupKBVersionDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t, &model.KnowledgeBase{})
}

func kbVerCode(name string) string { return kbVerOwnerPrefix + name }

// insertLegacyKB 用**裸 SQL 且不带新列**造一行，模拟"升级前就存在的 KB"：
// 新列由 DB 默认值补齐。这与 AutoMigrate 给已有表补列的效果同构
// （PG 对常量 DEFAULT 不重写表，存量行读到的就是默认值）。
func insertLegacyKB(t *testing.T, database *gorm.DB, code string) uint {
	t.Helper()
	if err := database.Exec(`
		INSERT INTO knowledge_bases (kb_code, type, name, owner_type, enabled)
		VALUES (?, 'faq', ?, 'shared', true)`, code, "存量库-"+code).Error; err != nil {
		t.Fatalf("裸 SQL 造存量行失败: %v", err)
	}
	var id uint
	if err := database.Raw(`SELECT id FROM knowledge_bases WHERE kb_code = ?`, code).
		Scan(&id).Error; err != nil {
		t.Fatalf("回读存量行 id 失败: %v", err)
	}
	if id == 0 {
		t.Fatal("存量行没插进去")
	}
	return id
}

func TestKnowledgeBaseVersionColumns_LegacyRowsDefaultToOne(t *testing.T) {
	database := setupKBVersionDB(t)
	ctx := context.Background()
	code := kbVerCode("legacy")
	id := insertLegacyKB(t, database, code)

	repo := NewKnowledgeBaseRepository(database)
	got, err := repo.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil {
		t.Fatal("GetByID 返回 nil")
	}
	if got.Version != 1 {
		t.Errorf("存量行 version 必须是 1（折算成今天的缓存键 v1），got %d", got.Version)
	}
	if got.CanaryPercent != 0 {
		t.Errorf("存量行 canary_percent 必须是 0（默认不放量），got %d", got.CanaryPercent)
	}
	if got.CanaryEnabled != nil && *got.CanaryEnabled {
		t.Error("存量行 canary_enabled 不得为 true")
	}

	type colShape struct {
		ColumnName    string `gorm:"column:column_name"`
		IsNullable    string `gorm:"column:is_nullable"`
		DataType      string `gorm:"column:data_type"`
		ColumnDefault string `gorm:"column:column_default"`
	}
	var shapes []colShape
	if err := database.Raw(`
		SELECT column_name, is_nullable, data_type, COALESCE(column_default, '') AS column_default
		FROM information_schema.columns
		WHERE table_name = 'knowledge_bases'
		  AND column_name IN ('version', 'canary_enabled', 'canary_percent')
		ORDER BY column_name`).Scan(&shapes).Error; err != nil {
		t.Fatalf("information_schema: %v", err)
	}
	if len(shapes) != 3 {
		t.Fatalf("三列没长齐，got %d 列: %+v", len(shapes), shapes)
	}
	for _, s := range shapes {
		// 整数两列必须 NOT NULL：可空 ⇒ 任何一行存成 NULL 就让 GORM 读回时报
		// "converting NULL to int"，整张 KB 列表跟着打不开。
		if s.ColumnName != "canary_enabled" && s.IsNullable != "NO" {
			t.Errorf("%s 必须 NOT NULL，got is_nullable=%s", s.ColumnName, s.IsNullable)
		}
		switch s.ColumnName {
		case "version":
			// bigint 不是笔误：GORM 把 Go 的 int 映射成 int8。想改成 integer 得先在
			// model 上写 type:integer，而 AutoMigrate 不会替已有表改列型 ⇒ 别在这里
			// 单方面放宽期望。
			if s.DataType != "bigint" {
				t.Errorf("version 期望 bigint（GORM 的 int→int8），got %s", s.DataType)
			}
			if s.ColumnDefault != "1" {
				t.Errorf("version 默认值期望 1，got %q（改这个默认值要先回去核 AC③ 的折算规则）", s.ColumnDefault)
			}
		case "canary_percent":
			if s.DataType != "bigint" {
				t.Errorf("canary_percent 期望 bigint，got %s", s.DataType)
			}
			if s.ColumnDefault != "0" {
				t.Errorf("canary_percent 默认值期望 0，got %q", s.ColumnDefault)
			}
		}
	}
}

func TestKnowledgeBaseUpdateVersionCanary_KeepsUpdatedAt(t *testing.T) {
	database := setupKBVersionDB(t)
	ctx := context.Background()
	repo := NewKnowledgeBaseRepository(database)

	enabled := true
	kb := &model.KnowledgeBase{
		KBCode:    kbVerCode("bump"),
		Type:      model.KnowledgeBaseTypeFAQ,
		Name:      "版本切换库",
		OwnerType: model.KnowledgeBaseOwnerShared,
		Enabled:   &enabled,
	}
	if err := repo.Create(ctx, kb); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// 把 updated_at 钉到一个过去的时刻，之后所有比较都对着这个锚点做。
	pinned := time.Now().Add(-72 * time.Hour).UTC().Truncate(time.Second)
	if err := database.Exec(`UPDATE knowledge_bases SET updated_at = ? WHERE id = ?`,
		pinned, kb.ID).Error; err != nil {
		t.Fatalf("钉住 updated_at 失败: %v", err)
	}

	if err := repo.UpdateVersionCanary(ctx, kb.ID, 3, true, 20); err != nil {
		t.Fatalf("UpdateVersionCanary: %v", err)
	}

	var after struct {
		UpdatedAt     time.Time `gorm:"column:updated_at"`
		Version       int       `gorm:"column:version"`
		CanaryEnabled bool      `gorm:"column:canary_enabled"`
		CanaryPercent int       `gorm:"column:canary_percent"`
		Name          string    `gorm:"column:name"`
		Enabled       bool      `gorm:"column:enabled"`
	}
	if err := database.Raw(`
		SELECT updated_at, version, canary_enabled, canary_percent, name, enabled
		FROM knowledge_bases WHERE id = ?`, kb.ID).Scan(&after).Error; err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if after.Version != 3 || !after.CanaryEnabled || after.CanaryPercent != 20 {
		t.Errorf("三列没写对: version=%d canary_enabled=%v canary_percent=%d",
			after.Version, after.CanaryEnabled, after.CanaryPercent)
	}
	if !after.UpdatedAt.Equal(pinned) {
		t.Errorf("版本写入不得 bump updated_at（一 bump 就把另一版本的缓存行整批删掉，AC② 当场失效）：锚点 %v got %v",
			pinned, after.UpdatedAt)
	}
	// 顺带钉住"只动三列"：通用字段一个都没被顺手改掉。
	if after.Name != "版本切换库" || !after.Enabled {
		t.Errorf("版本写入越界改了别的列: name=%q enabled=%v", after.Name, after.Enabled)
	}

	// 对照：走通用 Update 就会 bump。这条断言的作用是让"UpdateColumns 才不 bump"
	// 成为事实而不是注释——谁日后把版本并进通用 Update 的列映射，就会在这里露出来。
	if err := repo.Update(ctx, kb.ID, &model.KnowledgeBase{
		KBCode: kb.KBCode, Type: kb.Type, Name: kb.Name, OwnerType: kb.OwnerType, Enabled: &enabled,
	}); err != nil {
		t.Fatalf("通用 Update: %v", err)
	}
	var bumped time.Time
	if err := database.Raw(`SELECT updated_at FROM knowledge_bases WHERE id = ?`, kb.ID).
		Scan(&bumped).Error; err != nil {
		t.Fatalf("回读 updated_at 失败: %v", err)
	}
	if !bumped.After(pinned) {
		t.Errorf("通用 Update 理应 bump updated_at（对照断言），got %v 仍等于锚点 %v", bumped, pinned)
	}
	// 通用 Update 的列映射里没有版本三列 ⇒ 它不该把刚设好的版本冲掉。
	var keep struct {
		Version       int  `gorm:"column:version"`
		CanaryPercent int  `gorm:"column:canary_percent"`
		CanaryEnabled bool `gorm:"column:canary_enabled"`
	}
	if err := database.Raw(`SELECT version, canary_percent, canary_enabled FROM knowledge_bases WHERE id = ?`, kb.ID).
		Scan(&keep).Error; err != nil {
		t.Fatalf("回读版本列失败: %v", err)
	}
	if keep.Version != 3 || keep.CanaryPercent != 20 || !keep.CanaryEnabled {
		t.Errorf("通用 Update 不该动版本三列，got %+v", keep)
	}
}

func TestKnowledgeBaseUpdateVersionCanary_ClampsInvalidNumbers(t *testing.T) {
	database := setupKBVersionDB(t)
	ctx := context.Background()
	repo := NewKnowledgeBaseRepository(database)

	cases := []struct {
		name        string
		inVersion   int
		inPercent   int
		wantVersion int
		wantPercent int
	}{
		// 0/负数一律折算回 1（= 今天的键），而不是把非法号写进库里等运行时再兜。
		{"zero", 0, 30, 1, 30},
		{"negative", -7, 30, 1, 30},
		{"negative_percent", 2, -5, 2, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code := kbVerCode("clamp-" + tc.name)
			enabled := true
			kb := &model.KnowledgeBase{
				KBCode: code, Type: model.KnowledgeBaseTypeFAQ, Name: tc.name,
				OwnerType: model.KnowledgeBaseOwnerShared, Enabled: &enabled,
			}
			if err := repo.Create(ctx, kb); err != nil {
				t.Fatalf("Create: %v", err)
			}
			if err := repo.UpdateVersionCanary(ctx, kb.ID, tc.inVersion, true, tc.inPercent); err != nil {
				t.Fatalf("UpdateVersionCanary: %v", err)
			}
			var got struct {
				Version       int `gorm:"column:version"`
				CanaryPercent int `gorm:"column:canary_percent"`
			}
			if err := database.Raw(`SELECT version, canary_percent FROM knowledge_bases WHERE id = ?`, kb.ID).
				Scan(&got).Error; err != nil {
				t.Fatalf("回读: %v", err)
			}
			if got.Version != tc.wantVersion || got.CanaryPercent != tc.wantPercent {
				t.Errorf("入参 version=%d percent=%d ⇒ 期望 (%d,%d)，got %+v",
					tc.inVersion, tc.inPercent, tc.wantVersion, tc.wantPercent, got)
			}
		})
	}
}
