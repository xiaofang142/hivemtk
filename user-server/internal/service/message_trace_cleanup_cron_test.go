package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

func TestMaskPII_PhoneAndEmail(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"手机号打码", "请联系 13812345678 谢谢", "请联系 138****78 谢谢"},
		{"邮箱打码", "发送到 john.doe@example.com 即可", "发送到 j***@example.com 即可"},
		{"混合", "手机13812345678邮箱a-b.c@test.org", "手机138****78邮箱a***@test.org"},
		{"座机不打码", "电话 010-88886666", "电话 010-88886666"},
		{"短数字不打码", "订单号 12345678", "订单号 12345678"},
		{"空串", "", ""},
	}
	for _, c := range cases {
		if got := MaskPII(c.in); got != c.want {
			t.Errorf("%s: MaskPII(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestMaskPII_Idempotent(t *testing.T) {
	in := "手机 13912345678，邮箱 user@example.com"
	once := MaskPII(in)
	twice := MaskPII(once)
	if once != twice {
		t.Errorf("PII 打码不幂等: once=%q twice=%q", once, twice)
	}
}

func setupTraceCleanupTestDB(t *testing.T) *gorm.DB {
	return testutil.NewTestDB(t, &model.MessageTrace{})
}

func newSilentCleanupTask(database *gorm.DB) *MessageTraceCleanupTask {
	task := NewMessageTraceCleanupTask(repository.NewMessageTraceCleanupRepoWithDB(database))
	return task
}

func TestTraceCleanup_TTLAndPII(t *testing.T) {
	database := setupTraceCleanupTestDB(t)
	task := newSilentCleanupTask(database)

	now := time.Now()
	rows := []*model.MessageTrace{

		{TraceID: "tr-old", Node: "ingest", Input: "旧正文 13812345678", Output: "旧输出", CreatedAt: now.Add(-95 * 24 * time.Hour)},

		{TraceID: "tr-mid", Node: "ingest", Input: "手机 13812345678 邮箱 bob@corp.com", Output: "正常回复", CreatedAt: now.Add(-60 * 24 * time.Hour)},

		{TraceID: "tr-new", Node: "ingest", Input: "手机 13812345678", Output: "新回复", CreatedAt: now.Add(-10 * 24 * time.Hour)},
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatalf("seed rows: %v", err)
	}

	masked, nulled, err := task.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if nulled != 1 {
		t.Errorf("ttl_nulled = %d, want 1", nulled)
	}
	if masked < 1 {
		t.Errorf("pii_masked = %d, want >= 1", masked)
	}

	var oldRow, midRow, newRow model.MessageTrace
	if err := database.Where("trace_id = ?", "tr-old").First(&oldRow).Error; err != nil {
		t.Fatal(err)
	}

	if oldRow.Input != "" || oldRow.Output != "" {
		t.Errorf("90 天前正文未清除: input=%q output=%q", oldRow.Input, oldRow.Output)
	}
	if oldRow.Node != "ingest" || oldRow.TraceID != "tr-old" {
		t.Error("TTL 清理不得破坏结构字段")
	}

	if err := database.Where("trace_id = ?", "tr-mid").First(&midRow).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(midRow.Input, "13812345678") || !strings.Contains(midRow.Input, "138****78") {
		t.Errorf("60 天前正文未正确打码: %q", midRow.Input)
	}
	if strings.Contains(midRow.Input, "bob@corp.com") || !strings.Contains(midRow.Input, "b***@corp.com") {
		t.Errorf("60 天前邮箱未正确打码: %q", midRow.Input)
	}
	if !strings.Contains(midRow.Output, "正常回复") {
		t.Errorf("无 PII 正文不应被改动: %q", midRow.Output)
	}

	if err := database.Where("trace_id = ?", "tr-new").First(&newRow).Error; err != nil {
		t.Fatal(err)
	}
	if newRow.Input != "手机 13812345678" {
		t.Errorf("30 天内正文被误处理: %q", newRow.Input)
	}

	masked2, nulled2, err := task.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce #2: %v", err)
	}
	if masked2 != 0 || nulled2 != 0 {
		t.Errorf("重复执行应零改动: masked=%d nulled=%d", masked2, nulled2)
	}
}

func TestTraceCleanup_BatchBoundaryRowsAllProcessed(t *testing.T) {
	database := setupTraceCleanupTestDB(t)
	task := newSilentCleanupTask(database)

	now := time.Now()
	total := traceMaskBatchSize + 37
	rows := make([]*model.MessageTrace, 0, total)
	for i := 0; i < total; i++ {
		rows = append(rows, &model.MessageTrace{
			TraceID:   "tr-batch",
			Node:      "ingest",
			Input:     "手机 13812345678",
			CreatedAt: now.Add(-45 * 24 * time.Hour),
		})
	}
	if err := database.CreateInBatches(rows, 200).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	masked, _, err := task.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if masked != int64(total) {
		t.Errorf("跨批次打码行数 = %d, want %d（分批边界丢行）", masked, total)
	}
}
