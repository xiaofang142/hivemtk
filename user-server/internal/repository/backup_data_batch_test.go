package repository

// backup_data_batch_test.go G-6：DumpClues 分批拉取口径——跨 batchSize=1000 边界
// 不丢不重（修复前是单条全表 Find，clues 增长后即内存炸弹）。

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func TestDumpClues_BatchedAcrossBoundary(t *testing.T) {
	database := testutil.NewTestDB(t, &model.Clue{})
	ctx := context.Background()
	const total = 1005 // 跨 1000 批边界：验证循环终止且无遗漏
	batch := make([]map[string]any, 0, total)
	for i := 0; i < total; i++ {
		batch = append(batch, map[string]any{
			"tenant_id": 1, "id": fmt.Sprintf("c%04d", i), "source_id": "s",
			"account": "a", "type": 1, "is_verify": 0, "name": "n",
			"city": "", "address": "", "desc": "",
		})
	}
	if err := database.Table("clues").Create(&batch).Error; err != nil {
		t.Fatalf("种 clues 失败: %v", err)
	}
	repo := NewBackupDataRepositoryWithDB(database)
	data, err := repo.DumpClues(ctx)
	if err != nil {
		t.Fatalf("DumpClues 失败: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatalf("备份 JSON 非法: %v", err)
	}
	if len(rows) != total {
		t.Fatalf("分批 Dump 应返回 %d 行，实际 %d", total, len(rows))
	}
}
