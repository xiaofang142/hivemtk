package service

// 契约：next_run_at 这一列只有 applySchedule 一个写入方，且四个入口结论一致。
//
// 曾经的形态：这一列只在 cron 触发回调里写，而且和 last_run_at 一起写成同一个 now——
// 第一次触发前恒 NULL，触发后恒等于「上一次触发的时刻」。Cron 列表页那一列因此永远读出
// 错的值，而时区能力（CRON_TZ=）恰好只在它上面有可见证据。
// cron_next_run_test.go 锁的是「算得对」，本文件锁的是「写得到、清得掉、换表达式跟着换」：
//   - 启用 → 落库为按当前表达式算出的下一次（不是 NULL、不是 now）；
//   - 停用 → 落库为 NULL。留着上一次的排程，界面就显示「还有 3 天要跑」，而它不会再跑；
//   - 改表达式 → 跟着新表达式重算（否则「改表达式」又成了漏写的那个入口）；
//   - 启停/改表达式都不许把 last_run_at 抹掉（UpdateTimes 一次写这一对时刻，
//     调用方要保住的值必须自己读回来传，见 storeNextRun 的批注）。
//
// SetEnabled 会往进程级 TaskManager 注册真实条目，这一条躲不掉，所以：表达式取「每年 1 月 1 日 /
// 2 月 2 日」这种本次测试内不会触发的挂钟排程（taskSvc 传 nil，真触发就是 nil 解引用当场 panic，
// 比任何断言都响）；期望值按「现在之后的第一个挂钟匹配瞬间」现场算而不是写死年份，否则夹具就是一枚
// 日历引信；收尾成对还原（停用→注销条目→删行）。

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/browser_automation/model"
	barepo "hivemtk-user/internal/browser_automation/repository"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

const (
	cronLifeOwner = uint(781430)
	yearlyJanExpr = "0 0 1 1 *" // 每年 1 月 1 日 00:00
	yearlyFebExpr = "0 0 2 2 *" // 每年 2 月 2 日 00:00
	cronLifeTZ    = "Asia/Shanghai"
)

func newCronLifeSvc(t *testing.T) (*CronService, *gorm.DB, context.Context) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.BrowserTask{}, &model.BrowserSession{}, &model.BrowserCronTrigger{})
	if db == nil {
		t.Fatal("测试库不可达：本文件的启停/改写落库结论无法判定（不 Skip，跳过等于没锁）")
	}
	// taskSvc 传 nil：四个入口的记账路径都不碰 RunTask，只有真触发才碰（见文件头）。
	svc := NewCronService(
		barepo.NewBrowserCronTriggerRepositoryWithDB(db),
		barepo.NewBrowserTaskRepositoryWithDB(db), nil,
	)
	return svc, db, context.Background()
}

// seedCronLifeTrigger 经任务外键建一行触发器（GetByID 要 JOIN browser_tasks 才认这条归属），
// 初始 enabled=false：注册动作必须由测试显式触发，夹具自己不往 TaskManager 塞条目。
func seedCronLifeTrigger(t *testing.T, svc *CronService, db *gorm.DB, ctx context.Context,
	expr string, lastRun *time.Time) *model.BrowserCronTrigger {
	t.Helper()
	task := &model.BrowserTask{Name: "cron-life-" + expr, TaskType: "cron", Status: "ready",
		Url: "https://example.com", UserID: cronLifeOwner, Platform: "xiaohongshu",
		TimeoutSec: 120, Steps: []byte(`[{"action":"open_tab"}]`)}
	if err := barepo.NewBrowserTaskRepositoryWithDB(db).Create(ctx, task); err != nil {
		t.Fatalf("种子任务落库失败：%v", err)
	}
	tr := &model.BrowserCronTrigger{TaskID: task.ID, CronExpr: expr, TimeZone: cronLifeTZ,
		Enabled: false, LastRunAt: lastRun}
	if err := barepo.NewBrowserCronTriggerRepositoryWithDB(db).Create(ctx, tr); err != nil {
		t.Fatalf("种子触发器落库失败：%v", err)
	}
	// 归还全局状态：先停用（走 svc，条目才真被注销）再删行
	t.Cleanup(func() {
		cleanCtx := context.Background()
		_ = svc.SetEnabled(cleanCtx, tr.ID, cronLifeOwner, false)
		_ = db.WithContext(cleanCtx).Delete(&model.BrowserCronTrigger{}, tr.ID)
		_ = db.WithContext(cleanCtx).Delete(&model.BrowserTask{}, task.ID)
	})
	return tr
}

// readCronLifeTrigger 每次用独立零值 struct 读回：复用已填充的 struct 再 First() 会把旧字段
// 并进 WHERE（见 gorm dest struct 复用陷阱），那样读出的 NULL 不代表库里的值。
func readCronLifeTrigger(t *testing.T, db *gorm.DB, ctx context.Context, id uint) *model.BrowserCronTrigger {
	t.Helper()
	var got model.BrowserCronTrigger
	if err := db.WithContext(ctx).Where("id = ?", id).First(&got).Error; err != nil {
		t.Fatalf("读回触发器 %d 失败：%v", id, err)
	}
	return &got
}

// wantNextFire 独立复算「expr 挂钟语义下、from 之后的第一个瞬间」：
// 不走 nextRunAt（拿被测函数算期望＝自证），只在候选年里挑第一个晚于 from 的挂钟匹配瞬间。
func wantNextFire(t *testing.T, month, day int, from time.Time) time.Time {
	t.Helper()
	loc, err := time.LoadLocation(cronLifeTZ)
	if err != nil {
		t.Skipf("本机无 %s 时区数据：%v", cronLifeTZ, err)
	}
	inLoc := from.In(loc)
	for year := inLoc.Year(); year <= inLoc.Year()+2; year++ {
		cand := time.Date(year, time.Month(month), day, 0, 0, 0, 0, loc)
		if cand.After(from) {
			return cand.UTC()
		}
	}
	t.Fatalf("%d-%d 的挂钟排程在 %v 之后两个年内都不出现（夹具挑得不对）", month, day, from)
	return time.Time{}
}

func TestEnableWritesNextRunAndDisableWritesNull(t *testing.T) {
	svc, db, ctx := newCronLifeSvc(t)
	last := time.Now().Add(-72 * time.Hour).Truncate(time.Second)
	tr := seedCronLifeTrigger(t, svc, db, ctx, yearlyJanExpr, &last)

	if err := svc.SetEnabled(ctx, tr.ID, cronLifeOwner, true); err != nil {
		t.Fatalf("启用失败：%v", err)
	}
	on := readCronLifeTrigger(t, db, ctx, tr.ID)
	if on.NextRunAt == nil {
		t.Fatal("启用后 next_run_at 仍是 NULL：这一列没被写入，列表页显示「—」")
	}
	if want := wantNextFire(t, 1, 1, on.CreatedAt); !on.NextRunAt.UTC().Equal(want) {
		t.Errorf("启用后 next_run_at = %v want %v（%s 在该时区的下一次）", on.NextRunAt.UTC(), want, yearlyJanExpr)
	}
	if !on.NextRunAt.After(time.Now()) {
		t.Errorf("启用后 next_run_at = %v 不在现在之后（写成 now 就是这一类的读数）", *on.NextRunAt)
	}
	// 反向锁：启停不许把上一次执行时刻抹掉
	if on.LastRunAt == nil || !on.LastRunAt.Truncate(time.Second).Equal(last) {
		t.Errorf("启用把 last_run_at 改成了 %v，want 保持 %v", on.LastRunAt, last)
	}

	if err := svc.SetEnabled(ctx, tr.ID, cronLifeOwner, false); err != nil {
		t.Fatalf("停用失败：%v", err)
	}
	off := readCronLifeTrigger(t, db, ctx, tr.ID)
	if off.NextRunAt != nil {
		t.Errorf("停用后 next_run_at = %v，want NULL：留着它界面就显示「还有一场要跑」，而它不会再跑", *off.NextRunAt)
	}
	if off.LastRunAt == nil || !off.LastRunAt.Truncate(time.Second).Equal(last) {
		t.Errorf("停用把 last_run_at 改成了 %v，want 保持 %v", off.LastRunAt, last)
	}
}

// 改表达式这一入口：列表页那一列必须跟着新表达式走。
// 旧实现只在触发回调写，所以「改了表达式」在下次触发之前对这一列毫无影响——
// 三个写入入口（新建/改表达式/启停）里任意一个漏写，本腿就是唯一能发现它的。
func TestUpdateExprRewritesNextRun(t *testing.T) {
	svc, db, ctx := newCronLifeSvc(t)
	tr := seedCronLifeTrigger(t, svc, db, ctx, yearlyJanExpr, nil)
	if err := svc.SetEnabled(ctx, tr.ID, cronLifeOwner, true); err != nil {
		t.Fatalf("启用失败：%v", err)
	}
	before := readCronLifeTrigger(t, db, ctx, tr.ID)
	if before.NextRunAt == nil {
		t.Fatal("先决条件坏了：启用后没写入 next_run_at，本腿无从比较")
	}

	updated, err := svc.Update(ctx, tr.ID, cronLifeOwner, yearlyFebExpr, cronLifeTZ)
	if err != nil {
		t.Fatalf("改表达式失败：%v", err)
	}
	// 响应体里也得带着新时刻（前端拿更新返回值直接刷新那一列）
	if updated.NextRunAt == nil {
		t.Errorf("Update 返回的触发器 next_run_at = nil，界面这一列得刷新才显示")
	}
	after := readCronLifeTrigger(t, db, ctx, tr.ID)
	if want := wantNextFire(t, 2, 2, after.CreatedAt); after.NextRunAt == nil || !after.NextRunAt.UTC().Equal(want) {
		t.Errorf("改表达式后 next_run_at = %v want %v（%s 的下一次）", after.NextRunAt, want, yearlyFebExpr)
	}
	if after.NextRunAt != nil && after.NextRunAt.Equal(*before.NextRunAt) {
		t.Errorf("改表达式后时刻没动（%v）：说明这一列仍只有触发回调一个写入方", *after.NextRunAt)
	}
	// 表达式本身也得落库，否则「展示对了但跑的是旧的」
	if after.CronExpr != yearlyFebExpr {
		t.Errorf("落库表达式 = %q want %q", after.CronExpr, yearlyFebExpr)
	}
}

// 注销入口的账：Delete 之后全局 TaskManager 里不该还留着这条的条目。
// 锁的是「删了还在跑」——Delete 只 unregister(tr.TaskID)，而 entryIDs 按 task_id 记账，
// 一旦哪天注册与记账的键不同源（比如改用 trigger id），这里就会留下一个每次触发都查不到行、
// 于是静默 return 的僵尸条目。
func TestDeleteUnregistersTaskEntry(t *testing.T) {
	svc, db, ctx := newCronLifeSvc(t)
	tr := seedCronLifeTrigger(t, svc, db, ctx, yearlyJanExpr, nil)
	if err := svc.SetEnabled(ctx, tr.ID, cronLifeOwner, true); err != nil {
		t.Fatalf("启用失败：%v", err)
	}
	svc.mu.Lock()
	_, registered := svc.entryIDs[tr.TaskID]
	svc.mu.Unlock()
	if !registered {
		t.Fatalf("先决条件坏了：启用后 entryIDs[%d] 里没有条目，本腿无从判定注销", tr.TaskID)
	}
	if err := svc.Delete(ctx, tr.ID, cronLifeOwner); err != nil {
		t.Fatalf("删除失败：%v", err)
	}
	svc.mu.Lock()
	_, still := svc.entryIDs[tr.TaskID]
	svc.mu.Unlock()
	if still {
		t.Errorf("删除后 entryIDs[%d] 仍有条目：触发器已不在库里，条目却还挂在 TaskManager 上", tr.TaskID)
	}
}
