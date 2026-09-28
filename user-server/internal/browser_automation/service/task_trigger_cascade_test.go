package service

// 契约：定时触发器的存活必须跟着宿主任务的生命周期走。
//
// 触发器自己就有两条前置（Create 只认 task_type=cron 的任务、RunTask 只认已发布的任务），
// 但任务的编辑与删除入口都不检查它们，于是抵达过两个「新建时拒绝、改一改却能拿到」的状态，
// 两条都在真实例上实测到：
//   - 删掉宿主任务：触发行还在、进程内条目照旧醒来，last_run_at 与 next_run_at 照常推进，
//     而每一次执行都拿到「任务不存在」——触发器列表读起来一切正常，实际一场都没跑；
//   - 把任务类型改成非 cron：触发器继续按分钟唤起这条已经不该被定时唤起的任务。
//
// 每条腿都验两侧：改走必须收掉（行没了 + 条目注销了），仍是 cron 必须不收——
// 后者不过就等于「编辑一次任务名字就把用户配好的定时配置抹掉」，那比原缺陷更先被用户撞上。

import (
	"context"
	"testing"

	"hivemtk-user/internal/browser_automation/model"
	barepo "hivemtk-user/internal/browser_automation/repository"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

const (
	cascadeOwner = uint(781999)
	// 每年 1 月 1 日：本次测试内不会真被唤起。taskSvc 传 nil，误触发就是 nil 解引用当场 panic。
	cascadeExpr = "0 0 1 1 *"
)

func newCascadeSvcs(t *testing.T) (*TaskService, *CronService, *gorm.DB, context.Context) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.BrowserTask{}, &model.BrowserSession{}, &model.BrowserCronTrigger{})
	if db == nil {
		t.Fatal("测试库不可达：级联回收的落库结论无法判定（不 Skip，跳过等于没锁）")
	}
	taskRepo := barepo.NewBrowserTaskRepositoryWithDB(db)
	cronSvc := NewCronService(barepo.NewBrowserCronTriggerRepositoryWithDB(db), taskRepo, nil)
	taskSvc := NewTaskService(taskRepo, barepo.NewBrowserSessionRepositoryWithDB(db), nil)
	taskSvc.SetTriggerRemover(cronSvc)
	return taskSvc, cronSvc, db, context.Background()
}

// seedCronTaskWithTrigger 建一条 cron 任务并挂上启用中的触发器（走被测的 Create，
// 连排程条目一起备好），返回任务。清理只负责删行：条目按本文件的判据应当已被注销。
func seedCronTaskWithTrigger(t *testing.T, cronSvc *CronService,
	db *gorm.DB, ctx context.Context) *model.BrowserTask {
	t.Helper()
	task := &model.BrowserTask{Name: "cascade-probe", TaskType: "cron", Status: "ready",
		Url: "https://example.com", UserID: cascadeOwner, Platform: "xiaohongshu",
		TimeoutSec: 120, Steps: []byte(`[{"action":"open_tab"}]`)}
	if err := barepo.NewBrowserTaskRepositoryWithDB(db).Create(ctx, task); err != nil {
		t.Fatalf("种子任务落库失败：%v", err)
	}
	t.Cleanup(func() {
		cleanCtx := context.Background()
		_ = db.WithContext(cleanCtx).Delete(&model.BrowserCronTrigger{}, "task_id = ?", task.ID)
		_ = db.WithContext(cleanCtx).Delete(&model.BrowserTask{}, task.ID)
	})
	tr, err := cronSvc.Create(ctx, cascadeOwner, task.ID, cascadeExpr, "Asia/Shanghai", true)
	if err != nil {
		t.Fatalf("种子触发器创建失败：%v", err)
	}
	if tr.NextRunAt == nil {
		t.Fatalf("先决条件坏了：启用中的触发器 next_run_at = nil，本文件无从判定「它停了没」")
	}
	return task
}

func triggerRow(db *gorm.DB, ctx context.Context, taskID uint) *model.BrowserCronTrigger {
	var tr model.BrowserCronTrigger
	if err := db.WithContext(ctx).Where("task_id = ?", taskID).First(&tr).Error; err != nil {
		return nil
	}
	return &tr
}

func entryRegistered(cronSvc *CronService, taskID uint) bool {
	cronSvc.mu.Lock()
	defer cronSvc.mu.Unlock()
	_, ok := cronSvc.entryIDs[taskID]
	return ok
}

func TestTaskDeleteRemovesItsTriggerAndSchedule(t *testing.T) {
	taskSvc, cronSvc, db, ctx := newCascadeSvcs(t)
	task := seedCronTaskWithTrigger(t, cronSvc, db, ctx)

	if !entryRegistered(cronSvc, task.ID) {
		t.Fatalf("先决条件坏了：新建启用中的触发器没在 TaskManager 上留条目，注销腿无从判定")
	}
	if err := taskSvc.Delete(ctx, task.ID, cascadeOwner); err != nil {
		t.Fatalf("删除任务失败：%v", err)
	}
	if row := triggerRow(db, ctx, task.ID); row != nil {
		t.Errorf("任务已删，触发器行还在（id=%d enabled=%v next_run_at=%v）：它每次醒来都会执行一条不存在的任务，"+
			"并把 last_run_at 写成「刚刚」，列表页看上去完全正常", row.ID, row.Enabled, row.NextRunAt)
	}
	if entryRegistered(cronSvc, task.ID) {
		t.Errorf("任务已删，TaskManager 上仍挂着 task=%d 的排程条目", task.ID)
	}
	list, err := cronSvc.List(ctx, cascadeOwner)
	if err != nil {
		t.Fatalf("列表读取失败：%v", err)
	}
	for _, tr := range list {
		if tr.TaskID == task.ID {
			t.Errorf("触发器列表仍返回已删任务的触发器 id=%d", tr.ID)
		}
	}
}

func TestTaskTypeChangeAwayFromCronRemovesTrigger(t *testing.T) {
	taskSvc, cronSvc, db, ctx := newCascadeSvcs(t)
	task := seedCronTaskWithTrigger(t, cronSvc, db, ctx)

	updated, err := taskSvc.Update(ctx, task.ID, cascadeOwner, func(t *model.BrowserTask) error {
		t.TaskType = "one_shot"
		return nil
	})
	if err != nil {
		t.Fatalf("改类型失败：%v", err)
	}
	if updated.TaskType != "one_shot" {
		t.Fatalf("先决条件坏了：期望落库 one_shot，实得 %s", updated.TaskType)
	}
	if row := triggerRow(db, ctx, task.ID); row != nil {
		t.Errorf("任务已不是 cron，触发器还在（id=%d）：Create 明确拒绝非 cron 任务挂触发器，"+
			"改类型这条路却把同一状态送进了调度——「建时拦、改时放」等于没有判据", row.ID)
	}
	if entryRegistered(cronSvc, task.ID) {
		t.Errorf("任务已不是 cron，排程条目仍挂在 TaskManager 上")
	}
}

// 反向锁：留在 cron 的任务被编辑（只改名字）不许丢触发器。
// 这一腿挡的是「为了补级联把一切 Update 都当成类型改走」——那会静默抹掉用户配好的定时。
func TestTaskUpdateStillCronKeepsTrigger(t *testing.T) {
	taskSvc, cronSvc, db, ctx := newCascadeSvcs(t)
	task := seedCronTaskWithTrigger(t, cronSvc, db, ctx)

	if _, err := taskSvc.Update(ctx, task.ID, cascadeOwner, func(t *model.BrowserTask) error {
		t.Name = "cascade-probe-renamed"
		return nil
	}); err != nil {
		t.Fatalf("改名失败：%v", err)
	}
	row := triggerRow(db, ctx, task.ID)
	if row == nil {
		t.Fatal("只改了任务名字，触发器却没了：编辑任务不许顺手清掉用户配好的定时")
	}
	if !entryRegistered(cronSvc, task.ID) {
		t.Errorf("只改了任务名字，排程条目却被注销了（enabled=%v）", row.Enabled)
	}
	if row.NextRunAt == nil {
		t.Errorf("只改了任务名字，next_run_at 却被清成 NULL（界面会显示「未排程」）")
	}
}

// 没有触发器的任务也要能正常删除/改类型：回收问的是「别再响」，不是「它之前响过」。
func TestRemoveByTaskWithoutTriggerIsNoop(t *testing.T) {
	taskSvc, cronSvc, db, ctx := newCascadeSvcs(t)
	task := &model.BrowserTask{Name: "cascade-no-trigger", TaskType: "cron", Status: "ready",
		Url: "https://example.com", UserID: cascadeOwner, TimeoutSec: 120,
		Steps: []byte(`[{"action":"open_tab"}]`)}
	if err := barepo.NewBrowserTaskRepositoryWithDB(db).Create(ctx, task); err != nil {
		t.Fatalf("种子任务落库失败：%v", err)
	}
	t.Cleanup(func() {
		_ = db.WithContext(context.Background()).Delete(&model.BrowserTask{}, task.ID)
	})
	if err := cronSvc.RemoveByTask(ctx, task.ID); err != nil {
		t.Errorf("任务本就没有触发器，RemoveByTask 返回 %v，want nil", err)
	}
	if err := taskSvc.Delete(ctx, task.ID, cascadeOwner); err != nil {
		t.Errorf("无触发器的任务删除失败：%v", err)
	}
}
