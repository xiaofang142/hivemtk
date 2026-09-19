package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/browser_automation/repository"
	"hivemtk-user/internal/pkg/testutil"
)

// 批8 预算解耦契约锁：算术（纯函数）、发布闸门（真库）、装配位置（源码静态锁）三层各守一头。
//
// 解耦前的故障形态：确认等待「兼职」在 timeout_sec 上——timeout_sec=20 的任务，人还没看到
// 待确认就被掐死；timeout_sec=600 的任务，一次没点确认就白占 10 分钟执行预算。
// 所以「两条预算相加」这件事必须可算术验证，而「相加后的值真的被三处消费」必须可静态验证——
// 只改一处等于没改（外层看门狗先动手时，内层预算形同虚设）。

func TestTaskExecBudgetAddsConfirmWait(t *testing.T) {
	cases := []struct {
		name string
		task *model.BrowserTask
		want time.Duration
	}{
		{"无闸门=只看 timeout", &model.BrowserTask{TimeoutSec: 120}, 120 * time.Second},
		{"闸门+默认确认预算", &model.BrowserTask{TimeoutSec: 120, RequireConfirm: true}, 720 * time.Second},
		{"闸门+自定义确认预算", &model.BrowserTask{TimeoutSec: 120, RequireConfirm: true, ConfirmWaitSec: 30}, 150 * time.Second},
		// 0 值必须退化成默认而非「不等待」：老数据（列新加、默认 600 之外还有手工置 0 的）不能变成秒拒
		{"显式 0 退化成默认", &model.BrowserTask{TimeoutSec: 60, RequireConfirm: true, ConfirmWaitSec: 0}, 660 * time.Second},
		{"负数不退化成无限", &model.BrowserTask{TimeoutSec: 60, RequireConfirm: true, ConfirmWaitSec: -5}, 660 * time.Second},
		// 闸门关掉时确认预算不得生效（否则一个纯读任务也要多活 10 分钟才被判超时）
		{"关闸门时确认预算不参与", &model.BrowserTask{TimeoutSec: 60, RequireConfirm: false, ConfirmWaitSec: 900}, 60 * time.Second},
	}
	for _, c := range cases {
		if got := taskExecBudget(c.task); got != c.want {
			t.Errorf("%s: taskExecBudget=%v want %v", c.name, got, c.want)
		}
	}
	if got := confirmWaitBudget(nil); got != confirmWaitDefault {
		t.Errorf("nil 任务确认预算=%v want %v", got, confirmWaitDefault)
	}
}

// Publish 是进入可执行态的唯一门（cron/重试/直接落库都绕得开 dto binding），
// 所以区间校验必须落在这里，而不是只在 HTTP 入口。
func TestPublishClampsConfirmWaitSec(t *testing.T) {
	db := testutil.NewTestDB(t, &model.BrowserTask{}, &model.BrowserSession{})
	if db == nil {
		t.Skip("测试库不可达")
	}
	ctx := context.Background()
	repo := repository.NewBrowserTaskRepositoryWithDB(db)
	svc := NewTaskService(repo, repository.NewBrowserSessionRepositoryWithDB(db), nil)
	const userID = uint(772001)

	// GORM 的 default:600 会把零值改写成 600，越界值只能用裸 SQL 造（否则测不到真实脏数据形态）
	mk := func(confirmWaitSec int) *model.BrowserTask {
		task := &model.BrowserTask{Name: "b8-clamp", TaskType: "one_shot", Status: "draft",
			Url: "https://example.com", UserID: userID, Platform: "fixture",
			TimeoutSec: 120, Steps: []byte(`[{"action":"open_tab"}]`), RequireConfirm: true}
		if err := repo.Create(ctx, task); err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("UPDATE browser_tasks SET confirm_wait_sec = ? WHERE id = ?", confirmWaitSec, task.ID).Error; err != nil {
			t.Fatal(err)
		}
		return task
	}
	readStatus := func(id uint) string {
		var g model.BrowserTask
		if err := db.First(&g, id).Error; err != nil {
			t.Fatal(err)
		}
		return g.Status
	}

	for _, bad := range []int{901, -1, 100000} {
		task := mk(bad)
		if err := svc.Publish(ctx, task.ID, userID); err == nil || !strings.Contains(err.Error(), "confirm_wait_sec") {
			t.Errorf("confirm_wait_sec=%d 必须被 Publish 拦下，got err=%v", bad, err)
		}
		if got := readStatus(task.ID); got != "draft" {
			t.Errorf("越界值不得被放行成 %s", got)
		}
	}
	for _, good := range []int{600, 1, 900} {
		task := mk(good)
		if err := svc.Publish(ctx, task.ID, userID); err != nil {
			t.Errorf("confirm_wait_sec=%d 合法却被拒: %v", good, err)
		}
		if got := readStatus(task.ID); got != "ready" {
			t.Errorf("confirm_wait_sec=%d 应发布成功，got %s", good, got)
		}
	}
}

// 装配位置静态锁：解耦后的预算必须被三处同时消费（执行 ctx、外层看门狗、Brain 真钟期限），
// 终态写库必须脱离 execCtx（否则超时任务的收口写必然失败——而那正是最需要留证据的一次），
// 对账器必须挂进启动后台任务（只在请求路径里跑等于永不收敛）。
func TestBudgetDecoupleWiringLocks(t *testing.T) {
	taskSrc := readSrc(t, "task.go")
	for _, want := range []string{
		`utils.SafeGoDetached(ctx, "browser_automation.run", taskExecBudget(t)+taskWatchdogGrace`,
		"execCtx, cancel := context.WithTimeout(runCtx, taskExecBudget(t))",
	} {
		if !strings.Contains(taskSrc, want) {
			t.Errorf("task.go 未按独立预算装配，缺：%s", want)
		}
	}
	if strings.Contains(taskSrc, "time.Duration(t.TimeoutSec)*time.Second") {
		t.Error("task.go 仍有一处直接用 TimeoutSec 计预算（解耦未收口）")
	}
	execSrc := readSrc(t, "executor.go")
	if !strings.Contains(execSrc, "deadline := time.Now().Add(taskExecBudget(task) + taskWatchdogGrace)") {
		t.Error("Brain 真钟期限未改用解耦后的预算（LLM 编排的确认挂起会被旧期限掐死）")
	}
	if n := strings.Count(execSrc, "context.WithoutCancel("); n < 2 {
		t.Errorf("收口/清理写库需脱离 execCtx（终态写 + tab 回收两处），got %d", n)
	}
	if !strings.Contains(execSrc, "context.WithoutCancel(ctx), sessionFinalWriteBudget") {
		t.Error("executor 终态收口写库未走 WithoutCancel+独立预算")
	}
	feedbackSrc := readSrc(t, "feedback.go")
	if !strings.Contains(feedbackSrc, "context.WithoutCancel(ctx), sessionFinalWriteBudget") {
		t.Error("OnSessionFinished 终态写库未脱离已取消的执行 ctx")
	}

	dtoSrc := readSrc(t, "../dto/task.go")
	if n := strings.Count(dtoSrc, `confirm_wait_sec" binding:"omitempty,min=1,max=900"`); n != 2 {
		t.Errorf("Create/Update 两处入口都应带区间绑定，got %d", n)
	}
	ctrlSrc := readSrc(t, "../controller/task.go")
	if n := strings.Count(ctrlSrc, "ConfirmWaitSec"); n < 2 {
		t.Errorf("controller 必须在建/改两条路径都映射该列（漏一条=界面填了却不生效），got %d", n)
	}
	// 只数出现次数会被「if false 包住赋值」蒙过去（列名还在、语义已死），故另锁指针守卫本身。
	if !strings.Contains(ctrlSrc, "if req.ConfirmWaitSec != nil {") {
		t.Error("Update 路径丢了 nil 守卫：要么改不动确认预算，要么把 0 当合法值盖进库里")
	}
	routesSrc := readSrc(t, "../../router/browser_automation_routes.go")
	if !strings.Contains(routesSrc, "basvc.StartStaleTaskReconcile(ctx, taskRepo, sessionRepo)") {
		t.Error("对账器未挂进启动后台任务（砖化 running 没人收敛）")
	}
}
