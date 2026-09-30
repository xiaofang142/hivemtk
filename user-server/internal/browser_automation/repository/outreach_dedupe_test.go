package repository

import (
	"context"
	"testing"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/pkg/testutil"
)

func newOutreachDedupeRepo(t *testing.T) (BrowserOutreachDedupeRepository, any) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.BrowserOutreachDedupe{})
	if db == nil {
		t.Fatal("测试数据库不可用")
	}
	return NewBrowserOutreachDedupeRepositoryWithDB(db), db
}

func TestOutreachDedupeFindMiss(t *testing.T) {
	repo, _ := newOutreachDedupeRepo(t)
	got, err := repo.FindOutreachHit(context.Background(), "xiaohongshu", "https://x.test/p/1", "post_comment", "abc123")
	if err != nil {
		t.Fatalf("未命中应返回 nil,nil，got err=%v", err)
	}
	if got != nil {
		t.Fatalf("未命中应返回 nil，got %+v", got)
	}
}

func TestOutreachDedupeRecordThenHit(t *testing.T) {
	repo, _ := newOutreachDedupeRepo(t)
	ctx := context.Background()
	rec := &model.BrowserOutreachDedupe{
		Platform:  "xiaohongshu",
		TargetURL: "https://x.test/p/1",
		Action:    "post_comment",
		CopyHash:  "abc123",
		TaskID:    7,
		SessionID: 11,
	}
	if err := repo.RecordOutreachSend(ctx, rec); err != nil {
		t.Fatalf("Record 应成功，got %v", err)
	}
	got, err := repo.FindOutreachHit(ctx, "xiaohongshu", "https://x.test/p/1", "post_comment", "abc123")
	if err != nil {
		t.Fatalf("命中查询应成功，got %v", err)
	}
	if got == nil || got.TaskID != 7 || got.SessionID != 11 {
		t.Fatalf("命中应返回写入行，got %+v", got)
	}
}

func TestOutreachDedupeRecordIdempotent(t *testing.T) {
	repo, _ := newOutreachDedupeRepo(t)
	ctx := context.Background()
	mk := func() *model.BrowserOutreachDedupe {
		return &model.BrowserOutreachDedupe{
			Platform: "douyin", TargetURL: "https://d.test/v/9",
			Action: "post_comment", CopyHash: "zzz",
		}
	}
	if err := repo.RecordOutreachSend(ctx, mk()); err != nil {
		t.Fatalf("首次 Record 应成功，got %v", err)
	}
	if err := repo.RecordOutreachSend(ctx, mk()); err != nil {
		t.Fatalf("重复 Record 应幂等成功（唯一键冲突吞掉），got %v", err)
	}
}
