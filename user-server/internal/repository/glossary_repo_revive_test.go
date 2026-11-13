package repository

// glossary_repo_revive_test.go 软删后同 term_id 重建的原位复活契约。
//
// 背景（2026-11-13 K-4 台账轮逐路由 curl 实测复现）：
//   DELETE /api/glossaries/:term_id → 200「删除成功」后，同 term_id 再 POST
//   /api/glossaries → 409 DUPLICATE_ENTRY_3003「术语已存在」，而 GET/List
//   只看活跃行永远查不到——软删行仍占 term_id 唯一索引，同 term_id 永久
//   无法重建。geo site repo Create 早有同款「原位复活」模式，本仓术语表
//   漏了这条路径，本测试钉死之。
//
// 判据：
//  ① 建 → 删 → 同 term_id 重建：Create 不报 dup（复活成功）；
//  ② 复活行字段被新值覆盖（category/translations/preserve/status）；
//  ③ GetByTermID 能查到（deleted_at 已清）；
//  ④ 原有活跃行直接 Create 不受影响（无软删行时空操作走原路径）。
import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func TestGlossaryRepository_CreateRevivesSoftDeletedSameTermID(t *testing.T) {
	db := testutil.NewTestDB(t, &model.Glossary{})
	if db == nil {
		t.Fatal("测试库不可达：复活契约判不了（不 Skip，跳过等于没锁）")
	}
	repo := NewGlossaryRepositoryWithDB(db)
	ctx := context.Background()

	// ④ 对照：活跃行直接 Create 走原路径
	fresh := &model.Glossary{TermID: "repo-revive-fresh", Category: "probe",
		Translations: model.JSONMap{"en": "fresh"}}
	if err := repo.Create(ctx, fresh); err != nil {
		t.Fatalf("对照 Create 失败: %v", err)
	}

	// ① 建 → 删
	first := &model.Glossary{TermID: "repo-revive-term", Category: "probe",
		Translations: model.JSONMap{"en": "v1"}, Status: "active"}
	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("seed 术语失败: %v", err)
	}
	if err := repo.Delete(ctx, first.TermID); err != nil {
		t.Fatalf("seed 删除失败: %v", err)
	}

	// ① 同 term_id 重建：不应报 dup
	revived := &model.Glossary{TermID: "repo-revive-term", Category: "probe2",
		Preserve: true, Translations: model.JSONMap{"en": "v2", "zh": "复活"},
		Pattern: "k4", Status: "draft"}
	if err := repo.Create(ctx, revived); err != nil {
		if strings.Contains(err.Error(), "已存在") || strings.Contains(err.Error(), "duplicate") {
			t.Fatalf("① 同 term_id 重建应复活而非 dup，got: %v", err)
		}
		t.Fatalf("① 重建失败: %v", err)
	}

	// ② 字段被新值覆盖
	if revived.Category != "probe2" || revived.Preserve != true ||
		revived.Translations["en"] != "v2" || revived.Pattern != "k4" || revived.Status != "draft" {
		t.Fatalf("② 复活行字段应被新值覆盖: %+v", revived)
	}

	// ③ GetByTermID 能查到
	got, err := repo.GetByTermID(ctx, "repo-revive-term")
	if err != nil {
		t.Fatalf("③ GetByTermID 失败: %v", err)
	}
	if got.DeletedAt.Valid {
		t.Fatalf("③ 复活行 deleted_at 应为空，got valid=%v at %v", got.DeletedAt.Valid, got.DeletedAt.Time)
	}
	if got.Translations["zh"] != "复活" {
		t.Fatalf("③ 复活行 translations 应为新值，got %v", got.Translations)
	}

	// ④ 对照行仍在（复活路径没误伤活跃行）
	if _, err := repo.GetByTermID(ctx, "repo-revive-fresh"); err != nil {
		t.Fatalf("④ 对照活跃行应仍在: %v", err)
	}
}
