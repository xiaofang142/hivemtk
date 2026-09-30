package repository

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// Chunk2 红测：主 Profile 健康监护。BrowserProfileHealth 表 + 仓储尚不存在，
// 本文件编译失败即红；实现后应全绿。
func profileHealthRepo(t *testing.T) (BrowserProfileHealthRepository, *gorm.DB) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.BrowserProfileHealth{})
	if db == nil {
		t.Fatal("测试库不可达：熔断标记写不进库等于没熔断（不 Skip）")
	}
	return NewBrowserProfileHealthRepositoryWithDB(db), db
}

func TestProfileHealthCRUD(t *testing.T) {
	ctx := context.Background()
	repo, _ := profileHealthRepo(t)

	// 未知平台：无行（调用方按“未熔断”处理，不报错）。
	if _, err := repo.GetByPlatform(ctx, "xiaohongshu"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("未知平台应返回 ErrRecordNotFound，got %v", err)
	}

	// 熔断写入：幂等（连标两次仍一行）。
	if err := repo.MarkBlocked(ctx, "xiaohongshu", "snapshot blocked marker"); err != nil {
		t.Fatalf("MarkBlocked 失败: %v", err)
	}
	if err := repo.MarkBlocked(ctx, "xiaohongshu", "snapshot blocked marker"); err != nil {
		t.Fatalf("MarkBlocked 幂等失败: %v", err)
	}
	got, err := repo.GetByPlatform(ctx, "xiaohongshu")
	if err != nil {
		t.Fatalf("GetByPlatform 失败: %v", err)
	}
	if !got.Blocked || got.Reason == "" || got.BlockedAt == nil {
		t.Fatalf("熔断行字段不对: %+v", got)
	}

	// 人工恢复：清标记不清行。
	if err := repo.MarkRecovered(ctx, "xiaohongshu"); err != nil {
		t.Fatalf("MarkRecovered 失败: %v", err)
	}
	got, err = repo.GetByPlatform(ctx, "xiaohongshu")
	if err != nil {
		t.Fatalf("恢复后读取失败: %v", err)
	}
	if got.Blocked || got.BlockedAt != nil {
		t.Fatalf("恢复后标记应清除: %+v", got)
	}
}
