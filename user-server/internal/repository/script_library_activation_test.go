package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// #896 回归：POST /api/script-library/{id}/expire 对不存在 id 原样回 200
// 「话术已过期下线」——UpdateScriptActivation 不校验 RowsAffected，GORM 命中
// 0 行不报错。修复后 0 行必须报 ErrRecordNotFound，controller 据此映射 404。
func TestScriptLibraryRepo_UpdateScriptActivation_NotFound(t *testing.T) {
	database := testutil.NewTestDB(t, &model.ScriptLibrary{})
	repo := NewScriptLibraryRepository(database)
	ctx := context.Background()

	err := repo.UpdateScriptActivation(ctx, 999999, 0, "expired", nil)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("对不存在的话术更新状态应报 ErrRecordNotFound，实际: %v", err)
	}
}

// 存在的话术正常激活/过期不受影响（守卫不得误伤）。
func TestScriptLibraryRepo_UpdateScriptActivation_ExistingRow(t *testing.T) {
	database := testutil.NewTestDB(t, &model.ScriptLibrary{})
	repo := NewScriptLibraryRepository(database)
	ctx := context.Background()

	row := &model.ScriptLibrary{Title: "过期测试话术", Category: "greeting", Content: "您好", Status: "active"}
	if err := database.Create(row).Error; err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	now := time.Now()
	if err := repo.UpdateScriptActivation(ctx, row.ID, 0, "expired", &now); err != nil {
		t.Fatalf("存在行的过期应成功: %v", err)
	}
	var updated model.ScriptLibrary
	if err := database.First(&updated, row.ID).Error; err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	if updated.Status != "expired" {
		t.Fatalf("status 应为 expired，实际 %q", updated.Status)
	}
}
