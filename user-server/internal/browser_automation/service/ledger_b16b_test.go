package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"gorm.io/gorm"

	"hivemtk-user/internal/browser_automation/dto"
	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/browser_automation/repository"
)

// ledger_b16b_test.go — 批16b：二次对抗审核（生产代码轴 + 规格轴）确认的四处收口。
//
// 立项依据不是「再加固一点」，而是审核跑出来的具体失效形态：
// ① 台账写入只看 `.Error`，而 0 行受影响时 Error 就是 nil（探针实测见 §7.9）——
//    「已记为尝试」可以是零行；
// ② 越点未落账的进程内兜底不跨重启，而挂起重试 `next_retry_at` 是**持久化**的：
//    重启后扫描器照样认领同一个任务、同一行文本，库里那条凭据从来没存在过；
// ③ 拦截/降级文案用执行 ctx 落库，超时腿上 ctx 恰好 Done ⇒ 台账走 WithoutCancel 活下来、
//    人看到的那句原因却丢了（行永远停在 running）；
// ④ 两处口径不许反噬：缺口集合只按**不同键**增长、闸门键永不为空串（否则「放行并记一条
//    永远查不到的兜底」就是假的）。

// cancelOnFindRepo 在双发闸查询的那一刻把 ctx 掐掉：这正是真机上最难看的一种时序
// （「闸门刚判完、执行预算恰好到期」），也是 ③ 的唯一确定性复现方式。
type cancelOnFindRepo struct {
	repository.BrowserStepRepository
	cancel context.CancelFunc
}

func (c *cancelOnFindRepo) FindSubmitAttempt(ctx context.Context, taskID uint, textHash string, excludeID uint) (*model.BrowserStep, error) {
	c.cancel() // 判定完成的瞬间执行 ctx 死亡，之后的落库都不该再指望它
	return nil, errLedgerDBDown
}

// ③ 拦截文案必须在 ctx 已死后仍落到步行上。
//
// 断言打的是**库里的行**，不是函数返回值：返回值今天也对（同一句文案），
// 但运维面板读的是 submit_state/status/error_msg 那一行——它才是「可见」的定义。
func TestBlockedWriteStepReasonSurvivesCanceledCtx(t *testing.T) {
	exec, _, bundle := newWSE2E(t, happyReply)
	ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
	t.Cleanup(cancel)
	exec.stepRepo = &cancelOnFindRepo{BrowserStepRepository: bundle.stepRepo, cancel: cancel}

	task, session := bundle.seedTask(t, threeStageSteps, false)
	steps, err := ParseSteps([]byte(threeStageSteps))
	if err != nil {
		t.Fatal(err)
	}
	seq := 0
	status, msg, _ := exec.executeStepWithRetry(ctx, task, session, 3, steps[3], nil, &seq)
	if status != "failed" || !strings.Contains(msg, "双发闸") {
		t.Fatalf("闸门结论=%q %q（want failed + 点名双发闸）", status, msg)
	}
	if ctx.Err() == nil {
		t.Fatal("夹具没生效：查询时未掐断 ctx，这条腿等于没测")
	}
	row := readStepState(t, bundle, session.ID, 3)
	if row.Status != "failed" {
		t.Errorf("步行 status=%s want failed——ctx 一死「为什么被拦」就只留在日志里，可见承诺不成立（err=%q）",
			row.Status, row.ErrorMsg)
	}
	if !strings.Contains(row.ErrorMsg, "双发闸") {
		t.Errorf("步行 error_msg 须带闸门原因，got %q", row.ErrorMsg)
	}
}

// ②-a 越点未落账 ⇒ 不得再挂自动重试（重启后无人守着的兜底不算闸门）。
func TestCrossedLedgerGapSuppressesAutoRetry(t *testing.T) {
	exec, ext, bundle, taskRepo := newLedgerGapFeedbackE2E(t)
	h := HashWriteText("测试评论正文")
	flaky := &flakyStepRepo{BrowserStepRepository: bundle.stepRepo,
		failUpdate: func(state, textHash string) bool {
			return textHash == h && state != model.StepSubmitPrepared
		}}
	exec.stepRepo = flaky

	task, session := bundle.seedTask(t, threeStageSteps, false)
	mustEnableRetry(t, taskRepo, task)
	steps, err := ParseSteps([]byte(threeStageSteps))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	if n := ext.countOf("comment_send"); n != 1 {
		t.Fatalf("comment_send=%d want 1（前置条件：不可逆点确实跨越）", n)
	}
	if !exec.HasCrossedLedgerGap(task.ID) {
		t.Fatal("前置条件不成立：缺口没记进兜底集合，本腿测不到东西")
	}
	fresh := reloadTask(t, taskRepo, task.ID)
	if fresh.NextRetryAt != nil {
		t.Errorf("挂起重试 next_retry_at=%v want NULL——缺口只活在本进程内存里，重启后扫描器照样认领，"+
			"而库里那条凭据从未写进去：这一跑就是双发", fresh.NextRetryAt)
	}
	if !strings.Contains(fresh.LastResult+fresh.ErrorMsg, "未挂起自动重试") {
		t.Errorf("「为什么不自动重试」必须写在任务行上（运维看不到日志），got last=%q err=%q",
			fresh.LastResult, fresh.ErrorMsg)
	}
}

// ②-b 对照腿：没跨越（prepared 写失败）时重试照常挂起。
//
// 这条是上一条的反向保险：把「不重试」写成「只要台账失败就永不重试」照样能绿第一条，
// 但那是把 A7 变成停摆开关。prepared 未跨越 ⇒ 重跑是安全的 ⇒ 必须仍然自动重试。
func TestPreparedLedgerFailureStillSchedulesRetry(t *testing.T) {
	exec, ext, bundle, taskRepo := newLedgerGapFeedbackE2E(t)
	flaky := &flakyStepRepo{BrowserStepRepository: bundle.stepRepo,
		failUpdate: func(_, _ string) bool { return true }}
	exec.stepRepo = flaky

	task, session := bundle.seedTask(t, threeStageSteps, false)
	mustEnableRetry(t, taskRepo, task)
	steps, err := ParseSteps([]byte(threeStageSteps))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	if n := ext.countOf("comment_send"); n != 0 {
		t.Fatalf("comment_send=%d want 0（prepared 写失败必须拦在跨越之前）", n)
	}
	if exec.HasCrossedLedgerGap(task.ID) {
		t.Fatal("prepared 未跨越，不该进缺口集合（crossed 判据失效）")
	}
	fresh := reloadTask(t, taskRepo, task.ID)
	if fresh.NextRetryAt == nil {
		t.Errorf("未跨越的失败必须照旧挂起重试，got next_retry_at=NULL（把降级写成永久停摆）")
	}
}

// ②-c runRetry 侧的第二道：本进程已知缺口时，认领到的重试也不许起跑。
func TestRunRetryRefusesTaskWithLedgerGap(t *testing.T) {
	_, _, bundle, _ := newLedgerGapFeedbackE2E(t)
	// retryRunnerFn 是包级单例（同二进制内共享）：不钉住前置状态，第二条「未装配」断言
	// 就是在赌别的用例没装配过它。
	defer SetRetryRunner(retryRunnerFn)
	SetRetryRunner(nil)
	task := &model.BrowserTask{ID: 960002, UserID: wsE2EUserID, RetryCount: 0}
	feedback := NewFeedbackService(bundle.sessionRepo, repository.NewBrowserTaskRepositoryWithDB(bundle.db))
	feedback.SetLedgerGapProvider(func(id uint) bool { return id == task.ID })
	if err := feedback.runRetry(context.Background(), task, 1); err == nil {
		t.Error("有缺口时 runRetry 仍放行了重试")
	} else if !strings.Contains(err.Error(), "台账") {
		t.Errorf("拒绝原因须点名台账缺口，got %v", err)
	}
	feedback2 := NewFeedbackService(bundle.sessionRepo, repository.NewBrowserTaskRepositoryWithDB(bundle.db))
	if err := feedback2.runRetry(context.Background(), task, 1); err == nil ||
		!strings.Contains(err.Error(), "retry runner 未装配") {
		t.Errorf("未注入 provider 时的行为不该被这次改动改变（仍应报未装配），got %v", err)
	}
}

// ① 台账写入：0 行受影响必须算失败（探针实测：不存在 id / 软删行都是 Error=nil）。
func TestLedgerUpdateOnMissingRowFails(t *testing.T) {
	_, _, bundle, _ := newLedgerGapFeedbackE2E(t)
	repo := repository.NewBrowserStepRepositoryWithDB(bundle.db)
	ctx := context.Background()

	row := &model.BrowserStep{SessionID: 960001, TaskID: 960002, StepIndex: 1,
		Action: "post_comment", Status: "running"}
	if err := repo.BatchCreate(ctx, []*model.BrowserStep{row}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateSubmitState(ctx, row.ID, model.StepSubmitSent, "abcd1234"); err != nil {
		t.Fatalf("正常路径不该报错：%v", err)
	}

	ghost := row.ID + 999999
	if err := repo.UpdateSubmitState(ctx, ghost, model.StepSubmitSent, "abcd1234"); err == nil {
		t.Error("不存在 id 的台账写返回 nil——「已记为尝试」可以是零行")
	}
	// 软删（BrowserStep 带 DeletedAt ⇒ gorm 自动加 deleted_at IS NULL ⇒ 该 UPDATE 命中 0 行）
	if err := bundle.db.WithContext(ctx).Delete(&model.BrowserStep{}, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateSubmitState(ctx, row.ID, model.StepSubmitSent, "abcd9999"); err == nil {
		t.Error("软删行的台账写返回 nil——软删行同时也不在闸门查询范围内，两处口径必须一致")
	}
	if _, err := repo.FindSubmitAttempt(ctx, 960002, "abcd1234", 0); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("前置条件：软删后闸门查不到这条尝试（want record not found），got %v", err)
	}
}

// ④-a 缺口集合只按**不同键**增长（「只在写失败时增长、正常路径恒为 0」这句论证的前提）。
func TestLedgerGapSetGrowsOnlyPerDistinctKey(t *testing.T) {
	e := &Executor{}
	e.ledgerGaps = map[string]bool{}
	for i := 0; i < 5; i++ {
		e.rememberLedgerGap(7, "samehash")
	}
	if got := len(e.ledgerGapOrder); got != 1 {
		t.Errorf("同一 (task,文本) 记 5 次后集合长度=%d want 1（重复插入必须被去重，否则 512 上限测的是重复次数）", got)
	}
	for i := 0; i < ledgerGapCap+50; i++ {
		e.rememberLedgerGap(7, fmt.Sprintf("h%04d", i))
	}
	if got := len(e.ledgerGapOrder); got != ledgerGapCap {
		t.Errorf("集合长度=%d want 上限 %d（有界性是本集合唯一的安全论证）", got, ledgerGapCap)
	}
	if !e.ledgerGapHas(7, "h0099") {
		t.Error("最新写入的缺口必须在集合内（淘汰方向要留新弃旧）")
	}
}

// ④-b 闸门键永不为空串：`textHash == ""` 的早返不得成为写步的放行口。
func TestWriteStepKeyNeverEmpty(t *testing.T) {
	// 编排面能造出的最空形态：无正文、无定位、动作名也空。
	empties := []parsedStep{
		{StepItem: dto.StepItem{Action: "", Value: "   "}},
		{StepItem: dto.StepItem{Action: "click"}},
		{StepItem: dto.StepItem{Action: "click_near"}},
		{StepItem: dto.StepItem{Action: "post_comment", Value: "\n\t "}},
		{StepItem: dto.StepItem{Action: "type", Value: "  ", SubmitOnEnter: true}},
	}
	for i, s := range empties {
		if writeStepKey(s) == "" {
			t.Errorf("第 %d 个形态的闸门键是空串——guardResubmit 会就地放行且兜底记不住", i)
		}
	}
}

// ⑤ 通用写步（非 post_comment）「命令成功但台账没落」不得报 success。
//
// 补测理由（规格轴审核点出）：§8.3-12 写了「台账没落成的写步不得再报 success」，
// 而本批原有腿的 flaky 选靶全打在 post_comment 的三段式上（或打在查询路径），
// `executor.go` 里通用写步那条分支从来没被任何一条断言走过。
func TestGenericWriteStepLedgerFailureJudgedRed(t *testing.T) {
	exec, ext, bundle, _ := newLedgerGapFeedbackE2E(t)
	const clickSteps = `[{"action":"open_tab","target":"https://www.xiaohongshu.com/explore"},
{"action":"click","target":"button.submit"}]`
	key := writeStepKey(parsedStep{StepItem: dto.StepItem{Action: "click", Target: "button.submit"}})
	flaky := &flakyStepRepo{BrowserStepRepository: bundle.stepRepo,
		failUpdate: func(_, textHash string) bool { return textHash == key }}
	exec.stepRepo = flaky

	task, session := bundle.seedTask(t, clickSteps, false)
	steps, err := ParseSteps([]byte(clickSteps))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	if n := ext.countOf("click"); n != 1 {
		t.Fatalf("click 到线 %d want 1（前置条件：命令确实下发成功了）", n)
	}
	row := readStepState(t, bundle, session.ID, 1)
	if row.Status == "success" {
		t.Error("命令成功 + 台账没落 = 步报绿——闸门依据的记录不存在，下一轮必然双发")
	}
	if !strings.Contains(row.ErrorMsg, "台账") {
		t.Errorf("红因须点名台账，got %q", row.ErrorMsg)
	}
	if !row.IsWrite {
		t.Error("命中注册发送位的步必须落 is_write=true")
	}
	if !exec.HasCrossedLedgerGap(task.ID) {
		t.Error("通用写步的越点未落账必须进缺口兜底集合（否则同进程重试轮照样放行）")
	}
}

// ⑥ 台账写的退避重试是**承重**的：一次抖动失败、第二次成功 ⇒ 不留缺口、不降级，
// 后面的写步照常下发。
//
// 补测理由：§7.8 用「重试能把缺口变成没缺口」论证缺口率≈0，而把 `ledgerWriteAttempts`
// 改成 1 时全套测试照样绿——那句论证当时没有腿守着。
// 断言口径：不读降级表（`ExecuteSession` 末尾的 `defer clearLedgerBroken` 会让它恒为空，
// 读它就是又一条永远绿的断言），改读**第二个写步还发不发**——那才是降级在真机上唯一的表现形态。
func TestTransientLedgerWriteFailureRecoversWithoutGap(t *testing.T) {
	exec, ext, bundle, _ := newLedgerGapFeedbackE2E(t)
	const twoComments = `[{"action":"open_tab","target":"https://www.xiaohongshu.com/explore"},
{"action":"post_comment","value":"第一条正文","continue_on_error":true},
{"action":"post_comment","value":"第二条正文"}]`
	var calls int
	flaky := &flakyStepRepo{BrowserStepRepository: bundle.stepRepo,
		failUpdate: func(_, _ string) bool {
			calls++
			return calls == 1 // 只有第一次尝试失败：真机上最常见的形态就是行锁抖动
		}}
	exec.stepRepo = flaky

	task, session := bundle.seedTask(t, twoComments, false)
	steps, err := ParseSteps([]byte(twoComments))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	if calls < 2 {
		t.Fatalf("夹具只被调了 %d 次台账写，本腿没测到重试路径", calls)
	}
	if n := ext.countOf("comment_send"); n != 2 {
		t.Errorf("comment_send=%d want 2——一次抖动就掐掉整会话的写能力，说明退避重试不承重", n)
	}
	if exec.HasCrossedLedgerGap(task.ID) {
		t.Error("重试后成功的写不该留缺口")
	}
	for _, idx := range []int{1, 2} {
		if row := readStepState(t, bundle, session.ID, idx); row.Status != "success" {
			t.Errorf("step[%d] status=%s want success：%s", idx, row.Status, row.ErrorMsg)
		}
	}
}

// --- 夹具 ---

// newLedgerGapFeedbackE2E 在标准 E2E 夹具上接一个**真** FeedbackService（走 NewExecutor，
// 让「执行器与反馈服务的接线」本身进入被测面——手工塞字段会测不到漏接线这个失效形态）。
func newLedgerGapFeedbackE2E(t *testing.T) (*Executor, *fakeExtension, *wsE2EDeps, repository.BrowserTaskRepository) {
	t.Helper()
	exec, ext, bundle := newWSE2E(t, happyReply)
	taskRepo := repository.NewBrowserTaskRepositoryWithDB(bundle.db)
	feedback := NewFeedbackService(bundle.sessionRepo, taskRepo)
	rebuilt := NewExecutor(exec.hand, bundle.sessionRepo, bundle.stepRepo, nil, feedback)
	rebuilt.SetCommandLogRepository(bundle.cmdLogRepo)
	return rebuilt, ext, bundle, taskRepo
}

func mustEnableRetry(t *testing.T, taskRepo repository.BrowserTaskRepository, task *model.BrowserTask) {
	t.Helper()
	task.RetryOnFail = true
	task.MaxRetryTimes = 1
	if err := taskRepo.Update(context.Background(), task); err != nil {
		t.Fatal(err)
	}
}

func reloadTask(t *testing.T, taskRepo repository.BrowserTaskRepository, id uint) *model.BrowserTask {
	t.Helper()
	fresh, err := taskRepo.GetByID(context.Background(), id, wsE2EUserID)
	if err != nil {
		t.Fatalf("任务回读失败: %v", err)
	}
	return fresh
}
