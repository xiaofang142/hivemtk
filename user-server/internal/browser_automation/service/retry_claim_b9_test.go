package service

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/browser_automation/repository"
)

// retry_claim_b9_test.go — 批9 重试归属门。
//
// 踩出它的真实现场：挂起重试存在库里（共享），Host 连接存在进程里（私有）。本机 Host
// 全程在线的 task=377，重试被另一个实例（8204）认领后在自己的空 registry 上找不到连接，
// session=429 直接判「browser host 未连接」并烧掉了唯一的 1/1 重试额度。三条判据方向：
// ① 本机有连接 → 只认领自己用户的行，别用户的 next_retry_at 必须原样留着；
// ② 本机一条连接都没有 → 本轮根本不去认领（不是认领后跑失败）；
// ③ provider 未装配 → 不加过滤（退化成改造前行为），绝不能变成「永远不重试」。

// --- ① + ③ 仓储层：白名单过滤 ---

func mkDueRetryTask(t *testing.T, b *reconcileBundle, userID uint, dueAt time.Time) *model.BrowserTask {
	t.Helper()
	task := &model.BrowserTask{
		Name: fmt.Sprintf("retry-claim-%d", time.Now().UnixNano()), TaskType: "one_shot",
		Status: "failed", Url: "https://example.com", UserID: userID, Platform: "fixture",
		TimeoutSec: 60, RetryOnFail: true, MaxRetryTimes: 1,
	}
	if err := b.taskRepo.Create(b.ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := b.taskRepo.SetNextRetryAt(b.ctx, task.ID, &dueAt); err != nil {
		t.Fatal(err)
	}
	return task
}

func TestClaimDueRetriesOwnerWhitelist(t *testing.T) {
	b := newReconcileDB(t)
	due := time.Now().Add(-time.Minute)
	mine := mkDueRetryTask(t, b, 771101, due)
	theirs := mkDueRetryTask(t, b, 771102, due)
	// 未到期行不得被认领（归属门之外，到期条件也得守住）
	later := mkDueRetryTask(t, b, 771101, time.Now().Add(time.Hour))
	// 真机腿收尾就靠这条：软删的任务不再被认领（设备腿显式跑完第二轮后软删，免得一小时后
	// 到期的挂起重试变成野会话）。软删排除由 gorm 的 DeletedAt 作用域在整条查询链上兜住，
	// 单侧摘 SQL 条件不可观测——变异要三处 Unscoped 才打红（电池 N11），所以这条锁的是
	// 端到端性质而不是那句条件。
	gone := mkDueRetryTask(t, b, 771101, due)
	if err := b.taskRepo.SoftDelete(b.ctx, gone.ID, 771101); err != nil {
		t.Fatal(err)
	}

	got, err := b.taskRepo.ClaimDueRetries(b.ctx, time.Now(), 10, []uint{771101})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != mine.ID {
		ids := []uint{}
		for _, g := range got {
			ids = append(ids, g.ID)
		}
		t.Fatalf("白名单实例只该认领自己用户的到期行，实得 ids=%v", ids)
	}
	if readTask(t, b, mine.ID).NextRetryAt != nil {
		t.Error("已认领的行 next_retry_at 未清空（会被重复认领）")
	}
	if readTask(t, b, theirs.ID).NextRetryAt == nil {
		t.Error("别用户的挂起重试被本机清掉了（跨实例抢领：额度白烧）")
	}
	if readTask(t, b, later.ID).NextRetryAt == nil {
		t.Error("未到期的行被认领")
	}

	// 归属正确的那一方随后应能正常认领自己那条
	got2, err := b.taskRepo.ClaimDueRetries(b.ctx, time.Now(), 10, []uint{771102})
	if err != nil {
		t.Fatal(err)
	}
	if len(got2) != 1 || got2[0].ID != theirs.ID {
		t.Fatalf("属主实例认领不到自己的重试，got=%d", len(got2))
	}
}

func TestClaimDueRetriesNoProviderKeepsLegacyBehaviour(t *testing.T) {
	b := newReconcileDB(t)
	due := time.Now().Add(-time.Minute)
	a := mkDueRetryTask(t, b, 771111, due)
	c := mkDueRetryTask(t, b, 771112, due)
	// nil / 空切片同义 = 不加过滤：装配遗漏只会退化成改造前行为，不会静默停掉重试
	got, err := b.taskRepo.ClaimDueRetries(b.ctx, time.Now(), 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("无过滤时应照旧认领全部到期行，实得 %d 条", len(got))
	}
	for _, g := range got {
		if g.ID != a.ID && g.ID != c.ID {
			t.Errorf("认领到不相干的行 %d", g.ID)
		}
	}
}

// --- ①b 状态门：只有仍处失败态的行才可能被认领（批13）---

// 这道门在 SQL 里长两处：Pluck 的筛选条件 + 条件更新的 WHERE。单独摘掉任一处这条测试都
// 不会红（另一处兜住），两处一起摘才红——同 deleted_at 条件那处的性质（文件头注释记过）。
// 所以这条测试锁的是「非 failed 态不被自动重跑」这个端到端性质，不是某一句 SQL。
func TestClaimDueRetriesOnlyFailedRowsAreClaimable(t *testing.T) {
	b := newReconcileDB(t)
	due := time.Now().Add(-time.Minute)
	user := uint(771121)
	control := mkDueRetryTask(t, b, user, due) // 造出来就是 failed 态：真实挂起重试所在的状态

	// 这四态都是「失败之后、到期之前」真实可达的落点：
	//   done / paused —— RunTask 明确放行执行（一个是重跑成功后的终态，一个是 Resume 前态），
	//     被认领等于把已成功的任务再跑一遍、把用户按下的暂停悄悄解除；
	//   archived / running —— RunTask 会拒，但认领那一步已经把字段置空，挂起被无声吞掉。
	others := map[string]*model.BrowserTask{}
	for _, st := range []string{"done", "paused", "archived", "running"} {
		tsk := mkDueRetryTask(t, b, user, due)
		if err := b.taskRepo.UpdateStatus(b.ctx, tsk.ID, st, ""); err != nil {
			t.Fatal(err)
		}
		others[st] = tsk
	}

	got, err := b.taskRepo.ClaimDueRetries(b.ctx, time.Now(), 10, []uint{user})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != control.ID {
		ids := []uint{}
		for _, g := range got {
			ids = append(ids, g.ID)
		}
		t.Fatalf("只该认领仍处 failed 的那条，实得 ids=%v（control=%d）", ids, control.ID)
	}
	if readTask(t, b, control.ID).NextRetryAt != nil {
		t.Error("failed 行认领后 next_retry_at 未清空")
	}
	for st, tsk := range others {
		if readTask(t, b, tsk.ID).NextRetryAt == nil {
			t.Errorf("%s 态的行被认领了（挂起已被置空）——该态不该自动重跑", st)
		}
	}
}

// --- ② 服务层：本机无连接就不去认领 ---

// claimSpyRepo 内嵌接口：只实现被考察的 ClaimDueRetries，其余方法一旦调用即 nil panic，
// 「扫描器顺手还做了别的」这种漂移会当场暴露而不是静默通过。
type claimSpyRepo struct {
	repository.BrowserTaskRepository
	calls [][]uint
}

func (s *claimSpyRepo) ClaimDueRetries(_ context.Context, _ time.Time, _ int, owners []uint) ([]*model.BrowserTask, error) {
	s.calls = append(s.calls, owners)
	return nil, nil
}

func TestScanDueRetriesOwnerGate(t *testing.T) {
	t.Run("本机有连接：白名单原样传给认领", func(t *testing.T) {
		spy := &claimSpyRepo{}
		f := NewFeedbackService(nil, spy)
		f.SetHostUsersProvider(func() []uint { return []uint{26, 42} })
		f.scanDueRetries(context.Background())
		if len(spy.calls) != 1 {
			t.Fatalf("应恰好认领一次，实得 %d 次", len(spy.calls))
		}
		if len(spy.calls[0]) != 2 || spy.calls[0][0] != 26 || spy.calls[0][1] != 42 {
			t.Errorf("白名单未透传，实得 %v", spy.calls[0])
		}
	})

	t.Run("本机零连接：本轮不认领", func(t *testing.T) {
		spy := &claimSpyRepo{}
		f := NewFeedbackService(nil, spy)
		f.SetHostUsersProvider(func() []uint { return []uint{} })
		f.scanDueRetries(context.Background())
		if len(spy.calls) != 0 {
			t.Fatalf("本机无 Host 连接却去认领了（%v）——认领必然以「host 未连接」烧额度", spy.calls)
		}
	})

	t.Run("provider 未装配：不加过滤照旧认领", func(t *testing.T) {
		spy := &claimSpyRepo{}
		f := NewFeedbackService(nil, spy)
		f.scanDueRetries(context.Background())
		if len(spy.calls) != 1 {
			t.Fatalf("装配遗漏不得停掉重试，实得认领次数 %d", len(spy.calls))
		}
		if spy.calls[0] != nil {
			t.Errorf("未装配时应传 nil，实得 %v", spy.calls[0])
		}
	})
}

// ConnectedUserIDs 口径：只报本进程在册连接，且永不为 nil（服务层用 len 判空，nil 亦可，
// 但返回空切片才与「本机无人」的语义一致）。
func TestRegistryConnectedUserIDs(t *testing.T) {
	r := NewHostRegistry()
	if got := r.ConnectedUserIDs(); got == nil || len(got) != 0 {
		t.Fatalf("空注册表应返回非 nil 空切片，实得 %#v", got)
	}
	c := &HostConn{UserID: 5}
	r.mu.Lock()
	r.conns[5] = c
	r.mu.Unlock()
	got := r.ConnectedUserIDs()
	if len(got) != 1 || got[0] != 5 {
		t.Errorf("在册连接未报出，实得 %v", got)
	}
}

// --- 装配锁：归属门必须真的长在启动路径上 ---

func TestRetryOwnerGateIsWired(t *testing.T) {
	src, err := os.ReadFile("../../router/browser_automation_routes.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	// 锁字面注入表达式本身（批8 教训：计数式锁挡不住「语句还在、参数被抽空」）
	if !strings.Contains(s, "feedbackSvc.SetHostUsersProvider(registry.ConnectedUserIDs)") {
		t.Error("归属门未装配到路由启动路径")
	}
	setAt := strings.Index(s, "feedbackSvc.SetHostUsersProvider(registry.ConnectedUserIDs)")
	scanAt := strings.Index(s, "feedbackSvc.StartRetryScanner(ctx)")
	if setAt < 0 || scanAt < 0 || setAt > scanAt {
		t.Errorf("注入必须早于扫描器启动（setAt=%d scanAt=%d）", setAt, scanAt)
	}
}
