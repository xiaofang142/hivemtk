package service

// 批19：依赖归属门（IDOR）。Create 与 SetDependency 是同一条不变式的两条入口——
// 「depends_on_task_id 由请求方任意指定，所以必须验它属于当前用户」——而只有
// SetDependency 查了（task.go:227）。Create 把请求里的 DependsOnTaskID 直接写库，
// 于是任何人都能让自己的任务挂在别人的任务 id 上，两条后果：
//  1. 读侧外泄：执行时 checkDependency 用 GetLatestByTaskID/HasSuccess（都只按 task_id 查，
//     不带 user 过滤）读那条别人的会话，并把它的状态原样写进 409 的错误文案
//     （「前置任务最近一次执行状态为 %s」）——一个可枚举 id 的跨用户状态探针；
//  2. 写侧脏数据：这条依赖永远不可能被合法满足，且绕过了检环。
//
// 断四件事：① Create 拒绝别人的前置任务且不落库；② 「不存在」与「不是你的」文案完全一致
// （否则又是一台存在性探针）；③ 自己的前置任务照常放行（反向锁，防止判据写成一律拒绝）；
// ④ 已经躺在库里的历史越权行（修复前建的）在执行时也不许把别人的状态透出来。

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gorm.io/gorm"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/browser_automation/repository"
	"hivemtk-user/internal/pkg/testutil"
)

const (
	b19Owner = uint(781401)
	b19Alien = uint(781402)
)

func newB19Svc(t *testing.T) (*TaskService, context.Context) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.BrowserTask{}, &model.BrowserSession{})
	if db == nil {
		// 不 Skip：读不到库的归属锁等于没锁（这条判据本身就是安全判据）
		t.Fatal("测试库不可达：本文件的归属锁无法判定")
	}
	return NewTaskService(
		repository.NewBrowserTaskRepositoryWithDB(db),
		repository.NewBrowserSessionRepositoryWithDB(db), nil,
	), context.Background()
}

// b19Seed 直接走仓储造行：被种子任务的用途之一正是「修复前就已经在库里的那种越权行」，
// 它必须能在服务层校验之外存在。
func b19Seed(t *testing.T, svc *TaskService, ctx context.Context, userID uint, name string, dependsOn *uint, mode string) *model.BrowserTask {
	t.Helper()
	task := &model.BrowserTask{
		Name: name, TaskType: "one_shot", Status: "ready",
		Url: "https://example.com", UserID: userID, Platform: "xiaohongshu",
		TimeoutSec: 120, Steps: []byte(`[{"action":"open_tab","target":"https://example.com"}]`),
		DependsOnTaskID: dependsOn, DependsOnMode: mode,
	}
	if err := svc.taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("种子任务 %s 落库失败：%v", name, err)
	}
	return task
}

func TestB19CreateRejectsDependencyOnOthersTask(t *testing.T) {
	svc, ctx := newB19Svc(t)
	alien := b19Seed(t, svc, ctx, b19Alien, "b19-alien", nil, "")

	dep := alien.ID
	task := &model.BrowserTask{
		Name: "b19-create-foreign", TaskType: "one_shot", Url: "https://example.com",
		Platform: "xiaohongshu", TimeoutSec: 120,
		Steps:           []byte(`[{"action":"open_tab","target":"https://example.com"}]`),
		DependsOnTaskID: &dep, DependsOnMode: "all_done",
	}
	err := svc.Create(ctx, b19Owner, task)
	var ii *InvalidInputError
	if !errors.As(err, &ii) {
		t.Fatalf("挂在别人任务上的依赖被接受了：err=%v want *InvalidInputError", err)
	}
	// 不落库才算拦住：Create 走到一半返回错误时，行必须不存在
	if _, got := svc.taskRepo.GetByID(ctx, task.ID, b19Owner); !errors.Is(got, gorm.ErrRecordNotFound) {
		t.Errorf("拒绝却已落库（id=%d），got err=%v want record not found", task.ID, got)
	}
}

func TestB19ForeignAndMissingDependencyShareOneMessage(t *testing.T) {
	svc, ctx := newB19Svc(t)
	alien := b19Seed(t, svc, ctx, b19Alien, "b19-alien-msg", nil, "")

	newDep := func(id uint) *model.BrowserTask {
		return &model.BrowserTask{
			Name: "b19-msg", TaskType: "one_shot", Url: "https://example.com",
			Platform: "xiaohongshu", TimeoutSec: 120,
			Steps:           []byte(`[{"action":"open_tab","target":"https://example.com"}]`),
			DependsOnTaskID: &id, DependsOnMode: "all_done",
		}
	}
	foreign := svc.Create(ctx, b19Owner, newDep(alien.ID))
	missingID := alien.ID + 999999
	// 夹具前置必须自己成立：这条 id 真读不到，"missing" 那条腿才在测 missing。
	// 测试库是整进程共用一张表（不 truncate），id 空间被别人写过什么不由我控。
	if _, e := svc.taskRepo.GetByIDAnyUser(ctx, missingID); !errors.Is(e, gorm.ErrRecordNotFound) {
		t.Fatalf("夹具前置不成立：id=%d 竟可读回（err=%v）", missingID, e)
	}
	missing := svc.Create(ctx, b19Owner, newDep(missingID))
	if foreign == nil || missing == nil {
		t.Fatalf("两种非法前置都必须红：foreign=%v missing=%v", foreign, missing)
	}
	// 文案一模一样才不构成存在性探针（「这条 id 是别人的」本身就是可枚举的答复）
	if foreign.Error() != missing.Error() {
		t.Errorf("文案分成了两类，跨用户存在性可被判读：foreign=%q missing=%q",
			foreign.Error(), missing.Error())
	}
}

func TestB19CreateKeepsOwnDependency(t *testing.T) {
	svc, ctx := newB19Svc(t)
	own := b19Seed(t, svc, ctx, b19Owner, "b19-own-dep", nil, "")
	// 反向锁的前置得真满足：前置任务跑成 completed，all_done 才算过得去，
	// 否则这一条断言测的是「从未执行过」那条正常分支，归属门加没加都照样红。
	if err := svc.sessionRepo.Create(ctx, &model.BrowserSession{
		TaskID: own.ID, UserID: b19Owner, Status: "completed", Url: own.Url,
	}); err != nil {
		t.Fatal(err)
	}

	dep := own.ID
	task := &model.BrowserTask{
		Name: "b19-create-own", TaskType: "one_shot", Url: "https://example.com",
		Platform: "xiaohongshu", TimeoutSec: 120,
		Steps:           []byte(`[{"action":"open_tab","target":"https://example.com"}]`),
		DependsOnTaskID: &dep, DependsOnMode: "all_done",
	}
	if err := svc.Create(ctx, b19Owner, task); err != nil {
		t.Fatalf("自己的前置任务被误拦：%v", err)
	}
	got, err := svc.taskRepo.GetByID(ctx, task.ID, b19Owner)
	if err != nil {
		t.Fatal(err)
	}
	if got.DependsOnTaskID == nil || *got.DependsOnTaskID != own.ID {
		t.Errorf("依赖没落库：got=%v want %d", got.DependsOnTaskID, own.ID)
	}
	if err := svc.checkDependency(ctx, got); err != nil {
		t.Errorf("同用户依赖在执行前置校验里也不该被拦：%v", err)
	}
}

func TestB19RunTimeCheckDoesNotLeakOthersSessionStatus(t *testing.T) {
	svc, ctx := newB19Svc(t)
	alien := b19Seed(t, svc, ctx, b19Alien, "b19-alien-run", nil, "")
	// 越权历史行：alien 任务已经跑成 completed，攻击者任务（修复前建的）依赖它
	if err := svc.sessionRepo.Create(ctx, &model.BrowserSession{
		TaskID: alien.ID, UserID: b19Alien, Status: "completed", Url: alien.Url,
	}); err != nil {
		t.Fatal(err)
	}
	attacker := b19Seed(t, svc, ctx, b19Owner, "b19-legacy-foreign-dep", &alien.ID, "all_done")

	err := svc.checkDependency(ctx, attacker)
	if err == nil {
		t.Fatal("跨用户前置被当作满足：别人的执行结果参与了我的调度判定")
	}
	// 结论只能是「这条依赖不成立」，不许带出别人的状态或「从未执行过」这类可判别读数
	msg := err.Error()
	for _, leak := range []string{"completed", "failed", "running", "前置任务最近一次执行状态"} {
		if strings.Contains(msg, leak) {
			t.Errorf("错误文案透出他人会话读数 %q：%q", leak, msg)
		}
	}
}
