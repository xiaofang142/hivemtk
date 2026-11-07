// feedback_learning_cron_test.go 反馈学习 cron 装配语义的契约冻结。
//
// AV-04c 批次5 刀2：构造期句柄判断改仓储 DBReady() 探针、SOP 兄弟仓储改包级
// Shared 构造（与注入连接共享同一条连接）。本文件钉死两件事：
//  1. 未装配（nil repo / repo 无连接）→ nil cron —— 原 repo.GetDB()==nil 语义；
//  2. 装配态 → cron 五路仓储全部就位（Shared 构造生效），Stop 优雅退出。
package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

func TestNewFeedbackLearningCron_NotConfigured(t *testing.T) {
	if c := NewFeedbackLearningCron(nil); c != nil {
		t.Errorf("nil repo 应返回 nil cron，实际 %T", c)
	}
	if c := NewFeedbackLearningCron(repository.NewFeedbackLearningRepository(nil)); c != nil {
		t.Errorf("无连接 repo 应返回 nil cron（原 repo.GetDB()==nil 语义），实际 %T", c)
	}
}

func TestNewFeedbackLearningCron_SharedConstructorsNilSafety(t *testing.T) {
	if r := repository.NewSopAgentRepositoryShared(nil); r != nil {
		t.Errorf("Shared(nil) 应返回 nil，实际 %T", r)
	}
	if r := repository.NewSopExecutionRepositoryShared(nil); r != nil {
		t.Errorf("Shared(nil) 应返回 nil，实际 %T", r)
	}
	// repo 无连接：与原 NewSopAgentRepository(repo.GetDB()) 同形 —— 返回带 nil
	// 连接的仓储（方法内部按未装配降级），不是 panic。
	repo := repository.NewFeedbackLearningRepository(nil)
	if r := repository.NewSopAgentRepositoryShared(repo); r == nil {
		t.Errorf("无连接 repo 的 Shared 构造应返回仓储对象（同原路径），实际 nil")
	}
}

func TestNewFeedbackLearningCron_ReadyAndStops(t *testing.T) {
	db := testutil.NewTestDB(t,
		&model.SessionMessage{},
		&model.SOPAgent{},
		&model.SOPExecution{},
		&model.SOPNodeTransition{},
		&model.SalesChampionProfileSnapshot{},
		&model.OptimizationSuggestion{},
	)
	c := NewFeedbackLearningCron(repository.NewFeedbackLearningRepository(db))
	if c == nil {
		t.Fatal("已装配 repo 应返回非 nil cron")
	}
	if c.sopRepo == nil {
		t.Fatal("cron 的 SOP 仓储未装配（Shared 构造未生效）")
	}
	if c.svc == nil || c.svc.feedbackRepo == nil || c.svc.sopAgentRepo == nil || c.svc.sopExecRepo == nil {
		t.Fatal("service 五路仓储装配不完整（Shared 构造未生效）")
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c.Stop(stopCtx)
}
