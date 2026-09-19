package service

// 批10b 契约锁：域内「前置条件不满足」这一类结论必须带类型。
// 断的不是文案而是类型——文案会被透传成 4xx 的 body.message（前端直接弹它），
// 而 controller 的映射只有拿到类型才分得出该回 409、400 还是真的 500。
// 这一层丢类型 = 用户点错一次按钮，服务端记一条内部错误。

import (
	"context"
	"errors"
	"strings"
	"testing"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/browser_automation/repository"
	"hivemtk-user/internal/pkg/testutil"
)

func newB10bSvc(t *testing.T) (*TaskService, context.Context, uint) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.BrowserTask{}, &model.BrowserSession{})
	if db == nil {
		t.Skip("测试库不可达")
	}
	const userID = uint(781201)
	svc := NewTaskService(repository.NewBrowserTaskRepositoryWithDB(db),
		repository.NewBrowserSessionRepositoryWithDB(db), nil)
	return svc, context.Background(), userID
}

func mkB10bTask(t *testing.T, svc *TaskService, ctx context.Context, userID uint, status string) *model.BrowserTask {
	t.Helper()
	task := &model.BrowserTask{Name: "b10b-" + status, TaskType: "one_shot", Status: status,
		Url: "https://example.com", UserID: userID, Platform: "fixture",
		TimeoutSec: 120, Steps: []byte(`[{"action":"open_tab"}]`)}
	if err := svc.taskRepo.Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	return task
}

func TestRunTaskRejectsDraftAsStateConflict(t *testing.T) {
	svc, ctx, userID := newB10bSvc(t)
	task := mkB10bTask(t, svc, ctx, userID, "draft")

	_, err := svc.RunTask(ctx, task.ID, userID, 0)
	if err == nil {
		t.Fatal("draft 任务被执行了（批13 的状态门只管自动重试，手工执行这条也得拦）")
	}
	var sc *StateConflictError
	if !errors.As(err, &sc) {
		t.Fatalf("draft 执行的结论没带类型，controller 只能回 500：%T %v", err, err)
	}
	if !strings.Contains(sc.Msg, "publish") {
		t.Errorf("文案该把原因交给前端：%q", sc.Msg)
	}
	// 同一条结论不能同时是「忙」——否则前端会把它分流成等一等
	if errors.Is(err, ErrUserBusy) || errors.Is(err, ErrTaskRunning) {
		t.Errorf("draft 被判成忙/重复触发：%v", err)
	}
}

func TestStateAndInputPreconditionsCarryTypes(t *testing.T) {
	svc, ctx, userID := newB10bSvc(t)

	// 暂停只对 running 成立：draft 上点暂停是状态问题
	running := mkB10bTask(t, svc, ctx, userID, "running")
	draft := mkB10bTask(t, svc, ctx, userID, "draft")
	ready := mkB10bTask(t, svc, ctx, userID, "ready")

	var sc *StateConflictError
	if err := svc.Pause(ctx, draft.ID, userID); !errors.As(err, &sc) {
		t.Errorf("draft 暂停 → %T %v，want *StateConflictError", err, err)
	}
	if _, err := svc.Resume(ctx, ready.ID, userID); !errors.As(err, &sc) {
		t.Errorf("ready 恢复 → %T %v，want *StateConflictError", err, err)
	}
	if err := svc.Publish(ctx, running.ID, userID); !errors.As(err, &sc) {
		t.Errorf("running 再发布 → %T %v，want *StateConflictError", err, err)
	}
	if err := svc.Archive(ctx, running.ID, userID); !errors.As(err, &sc) {
		t.Errorf("running 归档 → %T %v，want *StateConflictError", err, err)
	}

	// 依赖自环是请求本身不合法，和「目标状态不对」不是一回事
	var ii *InvalidInputError
	if err := svc.SetDependency(ctx, draft.ID, userID, &draft.ID, ""); !errors.As(err, &ii) {
		t.Errorf("自依赖 → %T %v，want *InvalidInputError", err, err)
	}
	dep := draft.ID + 99999
	if err := svc.SetDependency(ctx, draft.ID, userID, &dep, ""); !errors.As(err, &ii) {
		t.Errorf("前置任务不存在 → %T %v，want *InvalidInputError", err, err)
	}
	// 反向也要成立：状态类结论不能被误标成入参类（否则 400/409 会串味）
	if err := svc.Pause(ctx, draft.ID, userID); errors.As(err, &ii) {
		t.Errorf("draft 暂停被判成入参不合法：%v", err)
	}
}
