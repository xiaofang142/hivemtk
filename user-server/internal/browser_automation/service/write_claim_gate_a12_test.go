package service

// 批20f（A12）服务层契约锁：存储层声明必须长在**下发任何帧之前**，且生命周期两半都对。
//
// 与仓储层那八条的分工（write_claim_a12_test.go）：仓储层证明「约束在库里、并发只有一个赢」；
// 本文件证明的是**接线**——executor 到底有没有在用它。这两件事很容易只剩一件：
// 只测仓储，「executor 从没调用过 ClaimWriteSlot」是全绿的；只测本文件，
// 「唯一索引其实没建」也是全绿的。所以两层各留各的腿。
//
// 六条腿各自钉一个失效面：
//   S1 别的腿已占坑 ⇒ 一帧都不下发（且这是**独立于读闸**的判定：台账里一条尝试都没有）
//   S2 从未跨过提交点 ⇒ 坑必须腾出来，且腾出来之后同任务重跑真能再走一遍
//   S3 跨过了提交点 ⇒ 坑必须留着（哪怕回查没见到、哪怕步判红）
//   S5 台账写失败 ⇒ 坑必须留着（此刻库里那行看着像「从未发生」，判据 1 单独用会放走双发）
//   S6 台账态读不出来 ⇒ 坑必须留着（判不出不许折向「没发生」）
//   S4 占坑不可判定（这里用「未接线」模拟）⇒ 拒绝下发，不许当成没这回事
//
// 手法沿用 executor_ws_e2e_test.go：真 WS 帧 + 测试库，唯一替身是扩展侧。

import (
	"context"
	"errors"
	"sync"
	"testing"

	"hivemtk-user/internal/browser_automation/model"

	"gorm.io/gorm"
)

const (
	a12OtherSession = uint(783001)
	a12OtherStep    = uint(783002)
	a12CommentText  = "测试评论正文"
)

// newA12Exec 起一套 e2e，并把扩展侧看到的每一帧同时记给调用方（b20c 的 runCommentSend 同手法）。
func newA12Exec(t *testing.T, reply func(action string, frame map[string]any) (map[string]any, string)) (*Executor, *wsE2EDeps, *capturedFrames) {
	t.Helper()
	frames := newCapturedFrames()
	exec, _, bundle := newWSE2E(t, func(action string, frame map[string]any) (map[string]any, string) {
		frames.note(action, frame)
		return reply(action, frame)
	})
	return exec, bundle, frames
}

// a12Run 跑一次两段式任务（open_tab → post_comment）。executor 与 session 由调用方备好：
// S1/S4 要在跑之前改接线，S2 要用同一个 executor 对同一个任务再开一轮。
func a12Run(t *testing.T, exec *Executor, task *model.BrowserTask, session *model.BrowserSession) {
	t.Helper()
	steps, err := ParseSteps([]byte(commentSendSteps))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)
}

// a12SessionFor 同一任务的第二个会话（=「另一条腿」在库里的形状）。
// 换任务是不行的：task_id 是声明键的一半，换了任务本来就各占各的坑，什么也证明不了。
func a12SessionFor(t *testing.T, bundle *wsE2EDeps, task *model.BrowserTask) *model.BrowserSession {
	t.Helper()
	s := &model.BrowserSession{TaskID: task.ID, UserID: wsE2EUserID, Status: "created", Url: task.Url}
	if err := bundle.sessionRepo.Create(context.Background(), s); err != nil {
		t.Fatalf("会话落库失败: %v", err)
	}
	return s
}

// claimOf 读回 (task, 正文) 这把坑当前的持有者；无人持有返回 nil。
func claimOf(t *testing.T, bundle *wsE2EDeps, taskID uint, text string) *model.BrowserWriteClaim {
	t.Helper()
	row, err := bundle.claimRepo.FindWriteClaim(context.Background(), taskID, HashWriteText(text))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		t.Fatalf("声明回读失败: %v", err)
	}
	return row
}

// S1 别的腿占着坑 ⇒ 本腿一帧都不下发。
//
// 前置刻意让 browser_steps 一条尝试都没有：读闸（FindSubmitAttempt）此刻查空、会放行，
// 于是「拦下来」只能归因到存储层声明。反过来，若实现把占坑写成「先查台账再决定要不要占」，
// 这条腿就会红——要的就是这个红。
func TestWriteClaimGateRefusesBeforeAnyFrame(t *testing.T) {
	exec, bundle, frames := newA12Exec(t, happyReply)
	task, session := bundle.seedTask(t, commentSendSteps, false)
	if _, err := bundle.claimRepo.ClaimWriteSlot(context.Background(),
		task.ID, a12OtherSession, a12OtherStep, HashWriteText(a12CommentText)); err != nil {
		t.Fatalf("预置他人声明失败: %v", err)
	}

	a12Run(t, exec, task, session)
	if n := frames.countOf("comment_send"); n != 0 {
		t.Fatalf("comment_send 到线 %d want 0——别的腿已占坑，本腿仍把提交发了出去，即双发", n)
	}
	row := readStepState(t, bundle, session.ID, 1)
	if row.Status != "failed" {
		t.Fatalf("status=%s want failed（被存储层闸门拦下必须判红，不许静默跳过）", row.Status)
	}
	if row.SubmitState != "" {
		t.Fatalf("submit_state=%q want 空：从未下发的步不该留下尝试凭据", row.SubmitState)
	}
	claim := claimOf(t, bundle, task.ID, a12CommentText)
	if claim == nil || claim.StepRowID != a12OtherStep {
		t.Fatalf("拦下时动到了别人的坑：claim=%v", claim)
	}
}

// S2 从未跨过提交点（发送按钮定位注入超时=点击从未发生）⇒ 坑必须腾出来，
// 且下一轮同任务同文本要真能再走一遍。这一半是 A12 唯一的代价：不释放就等于把声明表
// 变成永久黑名单，比原状（读闸 + 进程内兜底）更糟——一次浮层遮挡会把唯一正确的处置拦死。
//
// 扩展侧刻意做成「只有第一次 comment_send 失败」：两轮共用一个 fake，若第二轮也照样失败，
// 判的就成了扩展而不是键位（上一轮就是这么红的：重跑轮停在 prepared，看不出是没腾坑还是没点着）。
func TestWriteClaimReleasedWhenNeverCrossed(t *testing.T) {
	var mu sync.Mutex
	sendSeen := 0
	exec, bundle, frames := newA12Exec(t, func(action string, frame map[string]any) (map[string]any, string) {
		if action == "comment_send" {
			mu.Lock()
			sendSeen++
			first := sendSeen == 1
			mu.Unlock()
			if first {
				return nil, "comment_send_inject_timeout_: 页面注入拥堵，点击从未发生"
			}
		}
		return happyReply(action, frame)
	})
	task, session := bundle.seedTask(t, commentSendSteps, false)
	a12Run(t, exec, task, session)
	if n := frames.countOf("comment_send"); n != 1 {
		t.Fatalf("前置不成立：comment_send 到线 %d want 1（本条要真跑过一次「点了但没点到」）", n)
	}
	if row := readStepState(t, bundle, session.ID, 1); row.SubmitState != model.StepSubmitPrepared {
		t.Fatalf("首轮 submit_state=%s want prepared（前置不成立：本条要的是「点过但没点到」的形状）", row.SubmitState)
	}
	if claim := claimOf(t, bundle, task.ID, a12CommentText); claim != nil {
		t.Fatalf("从未提交的步把坑占死了（step=%d session=%d）：下一次同文本重跑会被永久拦下",
			claim.StepRowID, claim.SessionID)
	}

	// 只断「行没了」不够：键位没腾=没释放。用同一任务再开一轮，必须正常走到提交。
	session2 := a12SessionFor(t, bundle, task)
	a12Run(t, exec, task, session2)
	if n := readStepState(t, bundle, session2.ID, 1).SubmitState; n != model.StepSubmitVerified {
		t.Fatalf("重跑轮 submit_state=%s want verified：释放之后同文本这一轮没走通", n)
	}
	if n := frames.countOf("comment_send"); n != 2 {
		t.Fatalf("comment_send 累计到线 %d want 2（重跑轮必须真的又点了一次，否则 verified 是首轮剩的）", n)
	}
}

// S3 跨过提交点 ⇒ 坑留着。这里让回查（comment_verify）见不到那条评论——步判红、
// 台账落 unattributed，两种「不漂亮」的结局都不构成腾坑的理由：内容可能已经在平台上了。
func TestWriteClaimKeptWhenCrossed(t *testing.T) {
	exec, bundle, frames := newA12Exec(t, func(action string, frame map[string]any) (map[string]any, string) {
		if action == "comment_verify" {
			return map[string]any{"found": false, "matched": ""}, ""
		}
		return happyReply(action, frame)
	})
	task, session := bundle.seedTask(t, commentSendSteps, false)
	a12Run(t, exec, task, session)
	if n := frames.countOf("comment_send"); n != 1 {
		t.Fatalf("comment_send 到线 %d want 1（前置：本条要跑过一次真提交）", n)
	}
	row := readStepState(t, bundle, session.ID, 1)
	if row.SubmitState != model.StepSubmitUnattributed {
		t.Fatalf("submit_state=%s want unattributed（前置不成立：回查未见应记成已尝试）", row.SubmitState)
	}
	claim := claimOf(t, bundle, task.ID, a12CommentText)
	if claim == nil {
		t.Fatal("已尝试的步把坑腾出去了：声明的生命周期比台账短，双发就只剩台账一列在守")
	}
	if claim.StepRowID != row.ID {
		t.Fatalf("持有者不是这一步（claim=%d want %d）：按行释放会删错行", claim.StepRowID, row.ID)
	}
}

// S5 台账写失败 ⇒ 坑留着，哪怕库里那行看着像「从未发生」。
//
// 判据 1（submit_state 不在拦阻集合）在这里是**成立**的：prepared 写成、sent 写失败，
// 于是「按判据 1 腾坑」恰好会把一次真实公开提交在库里的唯一凭据删掉——那正是本条要拦的失效。
// 用 prepared 做靶心而不是「整条链都失败」：后者两半都红，看不出是哪一格在守。
func TestWriteClaimKeptWhenLedgerWriteFailed(t *testing.T) {
	exec, bundle, frames := newA12Exec(t, happyReply)
	// 只让 prepared 之后的写失败：prepared 是「未跨越」的那一格，它必须照常写成，
	// 否则库里连 prepared 都没有，本条判的就成了「整张台账没了」另一种形状。
	exec.stepRepo = &flakyStepRepo{
		BrowserStepRepository: bundle.stepRepo,
		failUpdate:            func(state, _ string) bool { return state != model.StepSubmitPrepared },
	}
	task, session := bundle.seedTask(t, commentSendSteps, false)

	a12Run(t, exec, task, session)
	if n := frames.countOf("comment_send"); n != 1 {
		t.Fatalf("comment_send 到线 %d want 1（前置：提交确实跨过了那一帧）", n)
	}
	row := readStepState(t, bundle, session.ID, 1)
	if row.SubmitState != model.StepSubmitPrepared {
		t.Fatalf("submit_state=%s want prepared（前置不成立：本条要的是「跨了但库里没记下」的形状）", row.SubmitState)
	}
	claim := claimOf(t, bundle, task.ID, a12CommentText)
	if claim == nil {
		t.Fatal("台账写失败却把坑腾了：这一次公开提交在库里只剩这一行声明作凭据，" +
			"腾出去＝下一轮同文本既能过读闸（查不到尝试）又能过存储层（没人居中），双发当场成立")
	}
	if claim.StepRowID != row.ID {
		t.Fatalf("持有者不是这一步（claim=%d want %d）", claim.StepRowID, row.ID)
	}
}

// S6 台账态回读失败 ⇒ 坑留着。「判不出」在这一格不许折向「当没发生」：
// 折过去的后果是「读一次失败就把别人（其实是自己）正跨着的提交重新变成可重发」。
// 与 S3 的区别是触发源：S3 是台账明写「已尝试」，本条是台账读不出来——两条各守一个判据，
// 少一条就会有一格注码活下来（摘掉拦阻集合循环只红 S3，摘掉 err 分支只红本条）。
func TestWriteClaimKeptWhenStateReadFailed(t *testing.T) {
	exec, bundle, frames := newA12Exec(t, happyReply)
	exec.stepRepo = &flakyStepRepo{BrowserStepRepository: bundle.stepRepo, failStateOf: errStateReadBack}
	task, session := bundle.seedTask(t, commentSendSteps, false)

	a12Run(t, exec, task, session)
	if n := frames.countOf("comment_send"); n != 1 {
		t.Fatalf("comment_send 到线 %d want 1（前置：本条要跑过一次真提交）", n)
	}
	claim := claimOf(t, bundle, task.ID, a12CommentText)
	if claim == nil {
		t.Fatal("台账态读不出来却把坑腾了：判不出被当成了「没发生」，而保守的一侧是留着")
	}
}

// S4 存储层闸门不可判定（这里用「没接线」模拟任何让占坑拿不到结论的情况）⇒ 拒绝下发。
// 免检的条件只能是「这一步不是写步」，不能是「闸门没接好」——批16（A11）那条口径的
// 又一次应用：判不出来不等于没有，拿判不出来换一次放行，赌的是不可逆动作。
func TestWriteClaimGateFailCloseWhenUnwired(t *testing.T) {
	exec, bundle, frames := newA12Exec(t, happyReply)
	task, session := bundle.seedTask(t, commentSendSteps, false)
	exec.SetWriteClaimRepository(nil)

	a12Run(t, exec, task, session)
	if n := frames.countOf("comment_send"); n != 0 {
		t.Fatalf("闸门不可判定时仍下发了 %d 帧 want 0", n)
	}
	if row := readStepState(t, bundle, session.ID, 1); row.Status != "failed" {
		t.Fatalf("status=%s want failed——闸门不可用必须判红，不是悄悄少跑一步", row.Status)
	}
}
