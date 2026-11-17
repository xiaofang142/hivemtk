package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/repository"
)

func TestFAQPolicy_CandidateCacheIsTenantScoped(t *testing.T) {
	database := testutil.NewTestDB(t, &model.FAQEntry{})
	oldMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(oldMode) })
	enabled, agentID := true, uint(7)
	for _, id := range []uint{18, 19} {
		ctx := tenant.WithTenantID(context.Background(), id)
		entry := &model.FAQEntry{TenantID: id, AgentID: &agentID, Enabled: &enabled,
			Question: "项目是什么", Answer: map[uint]string{18: "租户甲标准答案", 19: "租户乙标准答案"}[id], Keywords: []string{"项目", "是什么"}, Confidence: 0.99}
		if err := repository.NewFAQRepository(database).Create(ctx, entry); err != nil {
			t.Fatal(err)
		}
	}
	svc := NewFAQServiceWithRepo(repository.NewFAQRepository(database))
	for _, id := range []uint{18, 19} {
		matches, err := svc.MatchByAgent(tenant.WithTenantID(context.Background(), id), agentID, "项目是什么", 1)
		if err != nil || len(matches) != 1 {
			t.Fatalf("本租户应有答案：%v", err)
		}
		if matches[0].Entry.Answer != map[uint]string{18: "租户甲标准答案", 19: "租户乙标准答案"}[id] {
			t.Fatal("同名智能体不得复用另租户的候选缓存")
		}
	}
	if matches, err := svc.MatchByAgent(context.Background(), agentID, "项目是什么", 1); err == nil || len(matches) != 0 {
		t.Fatal("SaaS缺租户不能借已有缓存绕过数据库隔离")
	}
}

type faqPolicyDelayedSnapshotRepo struct {
	faqFastPathRepo
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *faqPolicyDelayedSnapshotRepo) ListCandidates(ctx context.Context, _ uint, _ int) ([]model.FAQEntry, error) {
	first := false
	r.once.Do(func() { first = true })
	if !first {
		return []model.FAQEntry{{Question: "项目是什么", Answer: "已更新答案", Enabled: cs03BoolPtr(true), Confidence: 0.99}}, nil
	}
	close(r.started)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-r.release:
		return []model.FAQEntry{{Question: "项目是什么", Answer: "已经失效的旧答案", Enabled: cs03BoolPtr(true), Confidence: 0.99}}, nil
	}
}

func (r *faqPolicyDelayedSnapshotRepo) ListByAgent(ctx context.Context, id uint, limit int) ([]model.FAQEntry, error) {
	return r.ListCandidates(ctx, id, limit)
}

func TestFAQPolicy_InvalidationCannotBeOverwrittenByOldSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name   string
		warmup bool
		all    bool
	}{
		{name: "懒加载期间单桶失效"},
		{name: "懒加载期间全桶失效", all: true},
		{name: "预热期间失效", warmup: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			repo := &faqPolicyDelayedSnapshotRepo{started: make(chan struct{}), release: make(chan struct{})}
			svc := NewFAQServiceWithRepo(repo)
			done := make(chan error, 1)
			var old []model.FAQEntry
			utils.SafeGo(ctx, "test.faqInvalidation", func(ctx context.Context) {
				var err error
				if tc.warmup {
					err = svc.WarmupCache(ctx, 7)
				} else {
					old, err = svc.cachedEntries(ctx, 7)
				}
				done <- err
			})
			select {
			case <-repo.started:
			case <-ctx.Done():
				t.Fatal("仓储快照读取没有开始")
			}
			if tc.all {
				svc.InvalidateAllCache()
			} else {
				svc.InvalidateCache(7)
			}
			close(repo.release)
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("失效后旧读取没有结束")
			}
			if len(old) != 0 {
				t.Fatal("已经失效的在途快照不能作为可用快答候选返回")
			}
			fresh, err := svc.cachedEntries(ctx, 7)
			if err != nil || len(fresh) != 1 || fresh[0].Answer != "已更新答案" {
				t.Fatal("旧读取不得在失效之后重新污染缓存")
			}
		})
	}
}
