package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/browser_automation/repository"
)

// Chunk2 红测：detectBlockedIfFatal 命中后的熔断写入。
// recordProfileBlocked 方法 + Executor.profileHealthRepo 字段尚不存在，编译失败即红。
type fakeProfileHealthRepo struct {
	repository.BrowserProfileHealthRepository
	blocked  []string
	reasons  []string
	recovery []string
}

func (f *fakeProfileHealthRepo) MarkBlocked(ctx context.Context, platform, reason string) error {
	f.blocked = append(f.blocked, platform)
	f.reasons = append(f.reasons, reason)
	return nil
}

func TestFuseOnDetectBlocked(t *testing.T) {
	fake := &fakeProfileHealthRepo{}
	e := &Executor{profileHealthRepo: fake}
	task := &model.BrowserTask{ID: 1, UserID: 26, Platform: "xiaohongshu"}
	session := &model.BrowserSession{ID: 11}
	if err := e.recordProfileBlocked(context.Background(), task, session, "IP存在风险"); err != nil {
		t.Fatalf("熔断写入失败: %v", err)
	}
	if len(fake.blocked) != 1 || fake.blocked[0] != "xiaohongshu" {
		t.Fatalf("应恰熔断一次且平台为 xiaohongshu，got %v", fake.blocked)
	}
	if len(fake.reasons) != 1 || fake.reasons[0] == "" {
		t.Fatalf("熔断原因必须留痕，got %v", fake.reasons)
	}
}
