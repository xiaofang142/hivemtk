// email_drain_worker_test.go 排水节拍的契约：这一轮真的会跑、跑坏了不会带走进程。
package email

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"

	"github.com/google/uuid"
)

// waitUntil 轮询到条件成立；用固定 sleep 的话，机器一忙就红（同仓 R13 的教训）。
func waitUntil(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待超时（%s）：%s", timeout, what)
}

func TestEmailDrainWorker_RunOnceDrainsDueEmail(t *testing.T) {
	database := setupEmailDrainTestDB(t)
	rec := &recorder{}
	svc := newDrainService(t, database, rec)
	createPendingSend(t, database, "tick@example.com", ptr(time.Now().Add(-time.Hour)))

	w := NewEmailDrainWorker(svc, time.Hour) // 节拍取一小时 ⇒ 本轮只能由 RunOnce 触发
	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce 失败: %v", err)
	}
	if strings.Join(rec.called(), ",") != "tick@example.com" {
		t.Errorf("尝试投递 = %v，期望 RunOnce 把到期邮件投出去", rec.called())
	}
	if got := w.Rounds(); got != 1 {
		t.Errorf("Rounds = %d，期望 1", got)
	}
}

func TestEmailDrainWorker_NilServiceRunOnceReportsError(t *testing.T) {
	setupEmailDrainTestDB(t)
	w := NewEmailDrainWorker(nil, time.Hour)
	err := w.RunOnce(context.Background())
	if err == nil {
		t.Fatal("未注入服务时 RunOnce 仍报成功 ⇒ 排水停摆会没人知道")
	}
	if !strings.Contains(err.Error(), "未注入") {
		t.Errorf("错误文案 = %q，期望说明是装配缺件", err.Error())
	}
	if w.Rounds() != 0 {
		t.Errorf("缺件那轮仍记了轮次（%d）⇒ 轮次读数会把「没干活」算成「干过活」", w.Rounds())
	}
	// Start 同理：不启动协程，但必须出声，不能装成一个已经在跑的节拍器。
	w.Start(context.Background())
	w.Stop(context.Background())
}

// TestEmailDrainWorker_PanicDoesNotKillLoop 一轮 panic 不能崩进程，也不能让节拍停摆。
//
// 排水体走的是真实 DB + SMTP 路径，而它跑在裸协程里：未捕获的 panic 会带走整个服务
// （同仓 cron 侧 5393b8dc 修的就是这一格，口径必须一致）。
//
// 本用例第一版断的是"panic 之后立刻重投这一封"，实测红：那条腿不属于这里 ——
// panic 发生在认领之后，行已经离开 pending，要等 EmailSendingStaleAfter 的回捞窗口才重投。
// 这是有意的：一行稳定 panic 的数据若立刻回捞，会把排水循环变成热循环（每 30s 崩一次），
// 而"这一封没投出去"远不如"整个服务的出站全停"严重。立刻重投那条腿由
// TestProcessPendingEmails_ReclaimsCrashedSendingRow 用回拨认领时刻的方式覆盖。
func TestEmailDrainWorker_PanicDoesNotKillLoop(t *testing.T) {
	database := setupEmailDrainTestDB(t)
	var mu sync.Mutex
	attempts := 0
	svc := NewEmailSendService()
	svc.SetEmailUnsubscribeRepository(repository.NewEmailUnsubscribeRepository(database))
	svc.deliver = func(_ context.Context, _ *model.EmailSend) error {
		mu.Lock()
		defer mu.Unlock()
		attempts++
		if attempts == 1 {
			panic("排水用例注入的 panic")
		}
		return nil
	}
	id := createPendingSend(t, database, "panic1@example.com", ptr(time.Now().Add(-time.Hour)))

	w := NewEmailDrainWorker(svc, 10*time.Millisecond)
	w.Start(context.Background())
	defer w.Stop(context.Background())

	// 节拍必须活过那一轮 panic：轮次继续增长（而不是停在 1）。
	waitUntil(t, 5*time.Second, "panic 之后节拍仍在跑", func() bool { return w.Rounds() >= 3 })

	mu.Lock()
	got := attempts
	mu.Unlock()
	if got != 1 {
		t.Errorf("panic 轮之后又尝试投递 %d 次，期望仍是 1 次（该行要等回捞窗口才重投，不能热循环）", got)
	}
	if s := readSendStatus(t, database, id); s != model.EmailStatusSending {
		t.Errorf("panic 那封的 status = %d，期望留在 sending(%d) 等回捞（既不谎报已发送，也不静默丢弃）",
			s, model.EmailStatusSending)
	}
}

func TestEmailDrainWorker_FirstTickAfterInterval(t *testing.T) {
	database := setupEmailDrainTestDB(t)
	rec := &recorder{}
	svc := newDrainService(t, database, rec)
	createPendingSend(t, database, "first-tick@example.com", ptr(time.Now().Add(-time.Hour)))

	w := NewEmailDrainWorker(svc, 200*time.Millisecond)
	w.Start(context.Background())
	if n := w.Rounds(); n != 0 {
		t.Errorf("Start 之后立即跑了 %d 轮，期望首轮等满间隔（启动瞬间做全表 UPDATE 会与恢复期读请求抢锁）", n)
	}
	waitUntil(t, 5*time.Second, "首轮在间隔之后发生", func() bool { return w.Rounds() >= 1 })
	w.Stop(context.Background())

	// Stop 必须真的让协程退出去：记下停止时的轮次，再等三个间隔，读数不得增长。
	stoppedAt := w.Rounds()
	time.Sleep(650 * time.Millisecond)
	if n := w.Rounds(); n != stoppedAt {
		t.Errorf("Stop 之后轮次仍从 %d 涨到 %d ⇒ 协程没停", stoppedAt, n)
	}
	if len(rec.called()) == 0 {
		t.Error("节拍跑过但没有一次投递尝试 ⇒ 循环与排水判据没接上")
	}
}

// TestEmailDrainWorker_StartStopIdempotent 重复 Start 只有一条协程、重复 Stop 不 panic。
//
// 装配层的 Init 可被重复调用（见 email_runtime_wiring.go），所以"起两条节拍器"是真实风险；
// 这里能证到的部分是：两次 Start 之后仍能正常 Stop 且轮次随即冻结 ——
// Stop 内部 wait 全部协程，若还有第二条在跑就会一直挂着（超时即红）。
func TestEmailDrainWorker_StartStopIdempotent(t *testing.T) {
	database := setupEmailDrainTestDB(t)
	rec := &recorder{}
	svc := newDrainService(t, database, rec)
	createPendingSend(t, database, "idem@example.com", ptr(time.Now().Add(-time.Hour)))

	w := NewEmailDrainWorker(svc, 10*time.Millisecond)
	w.Start(context.Background())
	w.Start(context.Background())
	waitUntil(t, 5*time.Second, "至少跑过一轮", func() bool { return w.Rounds() >= 1 })
	w.Stop(context.Background())
	w.Stop(context.Background())
	frozen := w.Rounds()
	time.Sleep(50 * time.Millisecond)
	if n := w.Rounds(); n != frozen {
		t.Errorf("重复 Stop 之后轮次仍在增长（%d → %d）", frozen, n)
	}
}

// errOnClaimRepo 只有一条腿会失败的仓库替身：认领那一步的失败必须透出来。
type errOnClaimRepo struct {
	repository.EmailSendRepository
	probe error
}

func (errOnClaimRepo) ReclaimStaleSending(context.Context, time.Time) (int64, error) { return 0, nil }
func (errOnClaimRepo) ExpireStalePending(context.Context, time.Time) (int64, error)  { return 0, nil }
func (r errOnClaimRepo) ClaimDueForUpdate(context.Context, time.Time, int) ([]*model.EmailSend, error) {
	return nil, r.probe
}
func (errOnClaimRepo) UpdateStatus(context.Context, uuid.UUID, int) error { return nil }
func (errOnClaimRepo) Create(context.Context, *model.EmailSend) error     { return nil }
func (errOnClaimRepo) GetByID(context.Context, uuid.UUID) (*model.EmailSend, error) {
	return nil, nil
}
func (errOnClaimRepo) List(context.Context) ([]*model.EmailSend, error) { return nil, nil }
func (errOnClaimRepo) Delete(context.Context, uuid.UUID) error          { return nil }

func TestEmailDrainWorker_RunOnceSurfacesRepoFailure(t *testing.T) {
	setupEmailDrainTestDB(t)
	svc := NewEmailSendService()
	probe := errors.New("测试探针：认领失败")
	svc.repo = errOnClaimRepo{probe: probe}
	w := NewEmailDrainWorker(svc, time.Hour)
	err := w.RunOnce(context.Background())
	if err == nil {
		t.Fatal("认领失败被吞掉 ⇒ 排水停摆与正常空轮在日志之外看不出差别")
	}
	if !errors.Is(err, probe) && !strings.Contains(err.Error(), probe.Error()) {
		t.Errorf("错误 = %v，期望透传仓库侧失败本身", err)
	}
	if w.Rounds() != 1 {
		t.Errorf("失败轮的轮次 = %d，期望仍记 1 轮（轮次回答的是「节拍跑过没有」）", w.Rounds())
	}
	var nilSvc *EmailDrainWorker
	if err := nilSvc.RunOnce(context.Background()); err == nil {
		t.Error("nil worker 的 RunOnce 报成功 ⇒ 空指针形状被藏起来了")
	}
}
