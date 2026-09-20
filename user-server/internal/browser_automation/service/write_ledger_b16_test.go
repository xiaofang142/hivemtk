package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/browser_automation/dto"
	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/browser_automation/repository"
)

// 批16（A7/A8/A11）：闸门所依据的记录本身可以静默失败——这是批6/批7 一整条防双发链路
// 唯一的「地基级」缺口。三处现状都朝「继续执行」的方向倒：
//   - recordSubmitState 写失败只 Warn（台账没落＝下一轮查不到尝试＝闸门整体消失）；
//   - guardResubmit 查询失败 fail-open 放行；
//   - isWriteStep 把取定位表的错误丢掉（表不可得 ⇒ 三条推导全不命中 ⇒ 写步被当成只读步）。
// 本批把它掰成 fail-close：宁可留下一条「可见、可人工重跑」的红步，也不留一次
// 「不可见、撤不回」的双发。断言口径与前几批一致——打到库里那一行和到线帧数，不打错误文案子串以外的事实。

// flakyStepRepo 只替两个台账方法，其余走真库：写失败/查失败都是本批的被测前提，
// 不能用「假装有个 repo」的整只 fake（那会把 BatchCreate/UpdateResult 的真相一起替掉）。
type flakyStepRepo struct {
	repository.BrowserStepRepository

	// failUpdate 返回 true 即这次 submit_state 写入失败（按 state+textHash 选靶，
	// 因为实现内部会重试，按调用序号选靶会把「重试后成功」误当成缺陷现场）
	failUpdate func(state, textHash string) bool
	failFind   error
}

func (f *flakyStepRepo) UpdateSubmitState(ctx context.Context, id uint, state, textHash string) error {
	if f.failUpdate != nil && f.failUpdate(state, textHash) {
		return errors.New("dial tcp 127.0.0.1:8232: connect: connection refused")
	}
	return f.BrowserStepRepository.UpdateSubmitState(ctx, id, state, textHash)
}

func (f *flakyStepRepo) FindSubmitAttempt(ctx context.Context, taskID uint, textHash string, excludeID uint) (*model.BrowserStep, error) {
	if f.failFind != nil {
		return nil, f.failFind
	}
	return f.BrowserStepRepository.FindSubmitAttempt(ctx, taskID, textHash, excludeID)
}

var errLedgerDBDown = errors.New("double submit guard query failed: connection reset by peer")

// twoWriteSteps 两条不同正文的 post_comment，中间夹一条只读步。
// 第一条带 continue_on_error：编排方明确要求「这步失败继续往下跑」，
// 这正是会话级降级唯一会被第二次派发到撞上的形状（否则整轮在第一条就终止，兜底无从表现）。
const twoWriteSteps = `[{"action":"open_tab","target":"https://www.xiaohongshu.com/explore"},
{"action":"post_comment","value":"第一条会被台账拖住的评论","continue_on_error":true},
{"action":"snapshot"},
{"action":"post_comment","value":"第二条应当被拒绝下发的评论"}]`

// 1) A7 本体：prepared 台账写不进去，就一帧都不准跨越不可逆提交点。
//
//	立项理由：prepared 不在拦阻集合内（model/step.go:52「提交可能发生」的态集合刻意排除它），
//	所以「prep 成功 + 台账没落」之后的 send 一旦发生，下一轮 FindSubmitAttempt 查空 → 双发。
func TestWSE2E_LedgerWriteFailureAbortsBeforeSend(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, happyReply)
	flaky := &flakyStepRepo{BrowserStepRepository: bundle.stepRepo,
		failUpdate: func(_, _ string) bool { return true }}
	exec.stepRepo = flaky

	task, session := bundle.seedTask(t, threeStageSteps, false)
	steps, err := ParseSteps([]byte(threeStageSteps))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	if n := ext.countOf("comment_prep"); n != 1 {
		t.Errorf("comment_prep=%d want 1（前置条件：填文本照常发生）", n)
	}
	if n := ext.countOf("comment_send"); n != 0 {
		t.Errorf("comment_send=%d want 0——台账写不进去还照发，就是拿不可逆动作赌 DB 手感", n)
	}
	if n := ext.countOf("comment_verify"); n != 0 {
		t.Errorf("comment_verify=%d want 0（没提交就不必回查）", n)
	}
	row := readStepState(t, bundle, session.ID, 3)
	if row.Status != "failed" {
		t.Errorf("status=%s want failed", row.Status)
	}
	if !strings.Contains(row.ErrorMsg, "台账") {
		t.Errorf("error_msg 须写明「台账未落」这个原因，got %q", row.ErrorMsg)
	}
}

// 2) A7 的降级面：同会话里后续写步在派发前就被拒绝，只读步照常跑完。
func TestWSE2E_LedgerWriteFailureDegradesRestOfSession(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, happyReply)
	firstHash := HashWriteText("第一条会被台账拖住的评论")
	flaky := &flakyStepRepo{BrowserStepRepository: bundle.stepRepo,
		failUpdate: func(_, textHash string) bool { return textHash == firstHash }}
	exec.stepRepo = flaky

	task, session := bundle.seedTask(t, twoWriteSteps, false)
	steps, err := ParseSteps([]byte(twoWriteSteps))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	if n := ext.countOf("snapshot"); n < 1 {
		t.Errorf("snapshot=%d want ≥1（降级只砍写能力，只读步不该被连坐）", n)
	}
	if row := readStepState(t, bundle, session.ID, 2); row.Status != "success" {
		t.Errorf("只读步 status=%s want success（写能力降级不得连带砍掉只读腿）：%s", row.Status, row.ErrorMsg)
	}
	if n := ext.countOf("comment_prep"); n != 1 {
		t.Errorf("comment_prep=%d want 1——第二个写步必须一帧都不下发（台账已不可信）", n)
	}
	if n := ext.countOf("comment_send"); n != 0 {
		t.Errorf("comment_send=%d want 0", n)
	}
	rows := bundle.steps(t, session.ID)
	if len(rows) != 4 {
		t.Fatalf("回读步数=%d want 4", len(rows))
	}
	second := rows[3]
	if second.Status != "failed" {
		t.Errorf("第二个写步 status=%s want failed", second.Status)
	}
	if !strings.Contains(second.ErrorMsg, "写能力已降级") {
		t.Errorf("第二个写步须写明降级原因，got %q", second.ErrorMsg)
	}
	if second.SubmitState != "" {
		t.Errorf("被拒绝下发的步不得有台账态，got %s", second.SubmitState)
	}
}

// 3) A7 最坏的窗口：越点已跨越、sent 却写不进去 ⇒ 库里那行停在 prepared（不在拦阻集合内）。
//
//	自动重试由同进程的 scheduleRetry 发起、换新 session 跑同一任务，所以会话级降级挡不住它，
//	必须有进程内兜底把「越点未落账」的 (task, 文本) 记下来。人工跨进程重跑挡不住（兜底不落库），
//	所以第 1 轮那条步必须判红并把「需人工核对」写进文案。
func TestWSE2E_SentLedgerGapStillBlocksRetryRound(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, happyReply)
	h := HashWriteText("测试评论正文")
	flaky := &flakyStepRepo{BrowserStepRepository: bundle.stepRepo,
		failUpdate: func(state, textHash string) bool {
			return textHash == h && state != model.StepSubmitPrepared
		}}
	exec.stepRepo = flaky

	task, first := bundle.seedTask(t, threeStageSteps, false)
	steps, err := ParseSteps([]byte(threeStageSteps))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	exec.ExecuteSession(ctx, task, first, steps)

	if n := ext.countOf("comment_send"); n != 1 {
		t.Fatalf("comment_send=%d want 1（前置条件：提交点确实跨越了）", n)
	}
	row1 := readStepState(t, bundle, first.ID, 3)
	if row1.SubmitState != model.StepSubmitPrepared {
		t.Fatalf("首轮台账=%s want prepared（前置条件：越点未落账的现场要复现出来）", row1.SubmitState)
	}
	if row1.Status != "failed" || !strings.Contains(row1.ErrorMsg, "人工") {
		t.Errorf("越点未落账必须判红并写「需人工核对」：status=%s err=%q", row1.Status, row1.ErrorMsg)
	}

	// 自动重试轮（同进程、新 session、库已恢复）：不得把同一条内容再发一遍
	task.RetryCount = 1
	exec.stepRepo = bundle.stepRepo
	second := &model.BrowserSession{TaskID: task.ID, UserID: wsE2EUserID, Status: "created", Url: task.Url}
	if err := bundle.sessionRepo.Create(ctx, second); err != nil {
		t.Fatal(err)
	}
	exec.ExecuteSession(ctx, task, second, steps)

	if n := ext.countOf("comment_send"); n != 1 {
		t.Errorf("重试轮后 comment_send=%d want 1——台账缺口挡不住重发就是双发", n)
	}
	if n := ext.countOf("comment_prep"); n != 1 {
		t.Errorf("重试轮后 comment_prep=%d want 1（拒绝=整步零帧，连 prep 都不该下）", n)
	}
	row2 := readStepState(t, bundle, second.ID, 3)
	if row2.Status == "skipped" {
		t.Error("缺口未知结局不得当成「已达成」静默跳过")
	}
	if !strings.Contains(row2.ErrorMsg, "台账") {
		t.Errorf("重试轮须写明被谁拦下，got %q", row2.ErrorMsg)
	}
}

// 4) A8：闸门查询失败改 fail-close。批7 的「人工重跑仍然硬拦」是同一条方向的口径。
func TestWSE2E_GuardQueryFailureFailsClosed(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, happyReply)
	exec.stepRepo = &flakyStepRepo{BrowserStepRepository: bundle.stepRepo, failFind: errLedgerDBDown}

	task, session := bundle.seedTask(t, threeStageSteps, false)
	steps, err := ParseSteps([]byte(threeStageSteps))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	if n := ext.countOf("comment_prep"); n != 0 {
		t.Errorf("comment_prep=%d want 0——查不到尝试就照发，等于把闸门交给 DB 手感", n)
	}
	if n := ext.countOf("open_tab"); n != 1 {
		t.Errorf("open_tab=%d want 1（fail-close 只关写，不关整条链路）", n)
	}
	row := readStepState(t, bundle, session.ID, 3)
	if row.Status != "failed" {
		t.Errorf("status=%s want failed", row.Status)
	}
	if !strings.Contains(row.ErrorMsg, "双发闸") {
		t.Errorf("error_msg 须点名是哪道闸，got %q", row.ErrorMsg)
	}
	if row.SubmitState != "" {
		t.Errorf("没下发就不该记尝试，got %s", row.SubmitState)
	}
}

// 5) A11：定位表不可得 ⇒ 副作用分类为「未知」，处置等同不可逆写。
//
//	今天的行为是把 err 丢掉（write_ledger.go:123 locs, _ := …）：三条推导全体不命中，
//	只剩 post_comment 与显式声明 ⇒ retries=0、双发闸、D7 三道同时静默消失。
//	收窄条件同批立：只报错才算未知，单纯没命中 locator 仍算只读（批7 的交互搜索腿口径不动）。
func TestWSE2E_UnknownLocatorTableTreatedAsWrite(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, func(action string, frame map[string]any) (map[string]any, string) {
		if action == "click" {
			return nil, "click_timeout_waiting_ack"
		}
		return happyReply(action, frame)
	})
	const stepsJSON = `[{"action":"open_tab","target":"https://example.com/orphan"},
{"action":"click","target":"button.whatever","retry_count":2,"retry_backoff_ms":50}]`
	task, session := bundle.seedTask(t, stepsJSON, false)
	task.Platform = "not_registered_platform_zz"
	steps, err := ParseSteps([]byte(stepsJSON))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	if n := ext.countOf("click"); n != 1 {
		t.Errorf("click 到线 %d 次 want 1——定位表不可得时不得宣称「这一下没有副作用」，retries 必须钳 0", n)
	}
	row := readStepState(t, bundle, session.ID, 1)
	if !row.IsWrite {
		t.Error("is_write 必须落库为真（分类未知＝按最坏处置，事后要能看出是被降级判的）")
	}
	if row.SubmitState != model.StepSubmitUnattributed {
		t.Errorf("submit_state=%s want unattributed（WS 超时=结果未知，须算提交尝试）", row.SubmitState)
	}
}

// 6) A11 的收窄边界（反向腿）：定位表正常可得时，未命中 locator 的步仍是只读——
//
//	批7 刻意收窄的那条口径（交互搜索腿不得被判写）不许被本批改宽。
func TestWSE2E_RegisteredPlatformNonMatchingStepStaysReadOnly(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, func(action string, frame map[string]any) (map[string]any, string) {
		if action == "click" {
			return nil, "click_timeout_waiting_ack"
		}
		return happyReply(action, frame)
	})
	const stepsJSON = `[{"action":"open_tab","target":"https://www.xiaohongshu.com/explore"},
{"action":"click","target":"div.nav-search","retry_count":1,"retry_backoff_ms":20}]`
	task, session := bundle.seedTask(t, stepsJSON, false)
	steps, _ := ParseSteps([]byte(stepsJSON))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	if n := ext.countOf("click"); n != 2 {
		t.Errorf("click 到线 %d want 2（平台已注册、定位表在手，未命中发送位就仍是可重试只读步）", n)
	}
	if row := readStepState(t, bundle, session.ID, 1); row.IsWrite || row.SubmitState != "" {
		t.Errorf("该步不该进写台账：is_write=%v submit_state=%s", row.IsWrite, row.SubmitState)
	}
}

// 7) 纯函数面：三态分类的完整表（含「声明位永远算写」「post_comment 恒为写」两条不受表影响的口径）
func TestClassifyStepEffectThreeStates(t *testing.T) {
	orphan := &model.BrowserTask{Platform: "not_registered_platform_zz"}
	known := &model.BrowserTask{Platform: "xiaohongshu"}
	cases := []struct {
		name string
		task *model.BrowserTask
		step parsedStep
		want stepEffect
	}{
		{"表不可得+click=未知", orphan, parsedStep{StepItem: dto.StepItem{Action: "click", Target: "div.any"}}, effectUnknown},
		{"表不可得+type回车=未知", orphan, parsedStep{StepItem: dto.StepItem{Action: "type", Target: "input.x", SubmitOnEnter: true, Value: "v"}}, effectUnknown},
		{"表不可得+click_near带文案=未知", orphan, parsedStep{StepItem: dto.StepItem{Action: "click_near", Anchor: ".c", ButtonText: "发送"}}, effectUnknown},
		{"表不可得+click_near无文案=只读", orphan, parsedStep{StepItem: dto.StepItem{Action: "click_near", Anchor: ".c"}}, effectNone},
		{"表不可得+type不回车=只读", orphan, parsedStep{StepItem: dto.StepItem{Action: "type", Target: "input.x", Value: "v"}}, effectNone},
		{"表不可得+scroll=只读", orphan, parsedStep{StepItem: dto.StepItem{Action: "scroll", Direction: "down", Amount: 3}}, effectNone},
		{"表不可得+open_tab=只读", orphan, parsedStep{StepItem: dto.StepItem{Action: "open_tab", Target: "https://x"}}, effectNone},
		{"表不可得+post_comment 仍是写", orphan, parsedStep{StepItem: dto.StepItem{Action: "post_comment", Value: "x"}}, effectWrite},
		{"表不可得+声明 is_write 仍是写", orphan, parsedStep{StepItem: dto.StepItem{Action: "query", Target: "s", IsWrite: true}}, effectWrite},
		{"表在手+命中发送位=写", known, parsedStep{StepItem: dto.StepItem{Action: "click", Target: "button.submit"}}, effectWrite},
		{"表在手+未命中=只读", known, parsedStep{StepItem: dto.StepItem{Action: "click", Target: "div.like"}}, effectNone},
		{"表在手+搜索腿回车=只读（批7 收窄口径不动）", known, parsedStep{StepItem: dto.StepItem{Action: "type", Target: "input.search", SubmitOnEnter: true, Value: "x"}}, effectNone},
	}
	for _, c := range cases {
		got, why := classifyStepEffect(c.task, c.step)
		if got != c.want {
			t.Errorf("%s: classifyStepEffect=%d want %d（why=%q）", c.name, got, c.want, why)
		}
		if c.want != effectNone && why == "" {
			t.Errorf("%s: 进了写闸门的步必须给归因 why", c.name)
		}
		if (got != effectNone) != c.want.needsWriteGate() {
			t.Errorf("%s: 三态与「要不要过写闸门」不自洽", c.name)
		}
	}
}
