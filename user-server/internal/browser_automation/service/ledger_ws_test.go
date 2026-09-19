package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/browser_automation/repository"
	"hivemtk-user/internal/pkg/testutil"
)

// 批6（F11b）写台账测试。手法与 executor_ws_e2e_test.go 同一档：真 WS 帧 + 真测试库落库，
// 断的是「DB 里那一行的 submit_state」和「不可逆帧到线几次」——不是错误文案的字符串匹配。
// 双发闸（本批立项理由）必须能在无真机情况下跑出来：第二次执行连 comment_prep 都不许到线。

// ledgerVerifyTrue 默认回包已含 verified=true；此处只需 happyReply。

func readStepState(t *testing.T, bundle *wsE2EDeps, sessionID uint, idx int) model.BrowserStep {
	t.Helper()
	for _, s := range bundle.steps(t, sessionID) {
		if s.StepIndex == idx {
			return *s
		}
	}
	t.Fatalf("session=%d 里没有 step_index=%d 的行", sessionID, idx)
	return model.BrowserStep{}
}

// 1) 纯函数：正文归一化与键值形状
func TestHashWriteTextShape(t *testing.T) {
	if got := HashWriteText("  今天 真好吃 \n"); got == "" || len(got) != 8 {
		t.Errorf("归一化后应为 8 位 hex，got %q", got)
	}
	if HashWriteText("今天真好吃") != HashWriteText(" 今天 真好吃 \n") {
		t.Error("同一篇评论仅空白不同 → 必须同键（否则双发闸漏拦）")
	}
	if HashWriteText("今天真好吃") == HashWriteText("明天真好吃") {
		t.Error("不同正文撞键")
	}
	if HashWriteText("   ") != "" {
		t.Error("空正文不应产生键（无内容可比对）")
	}
}

// 2) 成功链路终态必须是 verified（唯一可宣称「已发布」的态）
func TestWSE2E_LedgerEndsVerified(t *testing.T) {
	exec, _, bundle := newWSE2E(t, happyReply)
	task, session := bundle.seedTask(t, threeStageSteps, false)
	steps, err := ParseSteps([]byte(threeStageSteps))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	row := readStepState(t, bundle, session.ID, 3)
	if row.SubmitState != model.StepSubmitVerified {
		t.Errorf("submit_state=%s want verified（status=%s）", row.SubmitState, row.Status)
	}
	if row.TextHash != HashWriteText("测试评论正文") {
		t.Errorf("text_hash=%q 未落到本步正文", row.TextHash)
	}
	// 非写步不得留台账（prepared 之外的空态是常态，别把读步也记成提交尝试）
	if r := readStepState(t, bundle, session.ID, 0); r.SubmitState != "" {
		t.Errorf("open_tab 步不应有 submit_state：%s", r.SubmitState)
	}
}

// 3) 注入超时=点击从未发生：台账必须停在 prepared，且不得进 finalize 白轮
func TestWSE2E_SendInjectTimeoutStaysPrepared(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, func(action string, frame map[string]any) (map[string]any, string) {
		if action == "comment_send" {
			return nil, "comment_send_inject_timeout_15000ms"
		}
		return happyReply(action, frame)
	})
	task, session := bundle.seedTask(t, threeStageSteps, false)
	steps, err := ParseSteps([]byte(threeStageSteps))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	row := readStepState(t, bundle, session.ID, 3)
	if row.SubmitState != model.StepSubmitPrepared {
		t.Errorf("submit_state=%s want prepared——写成 sent 就是「没点过却记成可能已发」", row.SubmitState)
	}
	if row.Status != "failed" {
		t.Errorf("status=%s want failed", row.Status)
	}
	if n := ext.countOf("comment_verify"); n != 0 {
		t.Errorf("注入超时后仍进 finalize，comment_verify 到线 %d 次 want 0", n)
	}
	// prepared 不在拦阻集合内：这条腿的重下发是合法的（点击从未发生）
	if _, err := bundle.stepRepo.FindSubmitAttempt(context.Background(), task.ID, row.TextHash, 999999); !isNotFound(err) {
		t.Errorf("prepared 不该算提交尝试，got err=%v", err)
	}
}

// 4) 回查未见 → unattributed（且必须落在拦阻集合里：宁可人来判，不可自动双发）
func TestWSE2E_VerifyMissRecordsUnattributed(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, func(action string, frame map[string]any) (map[string]any, string) {
		if action == "comment_verify" {
			return map[string]any{"verified": false, "posted": false, "reason": "comment_not_rendered"}, ""
		}
		return happyReply(action, frame)
	})
	task, session := bundle.seedTask(t, threeStageSteps, false)
	steps, err := ParseSteps([]byte(threeStageSteps))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	row := readStepState(t, bundle, session.ID, 3)
	if row.SubmitState != model.StepSubmitUnattributed {
		t.Errorf("submit_state=%s want unattributed", row.SubmitState)
	}
	if n := ext.countOf("comment_send"); n != 1 {
		t.Errorf("comment_send=%d want 1（回查未见也不得重发）", n)
	}
	if _, err := bundle.stepRepo.FindSubmitAttempt(context.Background(), task.ID, row.TextHash, 999999); err != nil {
		t.Errorf("unattributed 必须在拦阻集合内，got err=%v", err)
	}
}

// 5) 双发闸本体（本批立项理由）：同一任务同一文本第二次执行，连 comment_prep 都不许到线
func TestWSE2E_SecondRunIsBlockedBeforeAnyFrame(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, happyReply)
	task, first := bundle.seedTask(t, threeStageSteps, false)
	steps, err := ParseSteps([]byte(threeStageSteps))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	exec.ExecuteSession(ctx, task, first, steps)

	if n := ext.countOf("comment_send"); n != 1 {
		t.Fatalf("首轮 comment_send=%d want 1（前置条件不成立，后面断言无意义）", n)
	}

	second := &model.BrowserSession{TaskID: task.ID, UserID: wsE2EUserID, Status: "created", Url: task.Url}
	if err := bundle.sessionRepo.Create(ctx, second); err != nil {
		t.Fatal(err)
	}
	exec.ExecuteSession(ctx, task, second, steps)

	if n := ext.countOf("comment_prep"); n != 1 {
		t.Errorf("comment_prep 到线 %d 次 want 1——闸门必须前置于 prep（prep 会往输入框塞草稿，不是零副作用探测）", n)
	}
	if n := ext.countOf("comment_send"); n != 1 {
		t.Errorf("comment_send 到线 %d 次 want 1（双发红线）", n)
	}
	row := readStepState(t, bundle, second.ID, 3)
	if row.Status != "failed" || !strings.Contains(row.ErrorMsg, "拒绝执行") {
		t.Errorf("第二轮该步应被拦下：status=%s err=%q", row.Status, row.ErrorMsg)
	}
	if row.SubmitState != "" {
		t.Errorf("被拦下的一轮不得写台账（会污染下一次判定）：%s", row.SubmitState)
	}
	// 首轮那条 verified 行必须还是 verified（拦阻路径不许改写历史台账）
	if got := readStepState(t, bundle, first.ID, 3); got.SubmitState != model.StepSubmitVerified {
		t.Errorf("首轮台账被改写：%s", got.SubmitState)
	}
}

//  6. FindSubmitAttempt 的范围：换任务/换文本不拦，prepared 不拦，其余三态都拦；
//     而「同一任务、同一文本、不同 step_index」必须拦（批7 F-N4：Brain 模式每轮递增下标，
//     带下标的键等于没有键）。
//     （漏一个条件=该拦的没拦（双发）或多一个条件=不该拦的拦了（正常任务跑不动），两头都要测到）
func TestFindSubmitAttemptScoping(t *testing.T) {
	db := testutil.NewTestDB(t,
		&model.BrowserTask{}, &model.BrowserSession{},
		&model.BrowserStep{}, &model.BrowserCommandLog{},
	)
	if db == nil {
		t.Skip("测试库不可达")
	}
	repo := repository.NewBrowserStepRepositoryWithDB(db)
	ctx := context.Background()
	const taskID = uint(4401)
	h := HashWriteText("同一条评论")
	mk := func(stepIdx int, state string, text string) uint {
		row := &model.BrowserStep{SessionID: 1, TaskID: taskID, StepIndex: stepIdx,
			Action: "post_comment", Value: text, Status: "failed", SubmitState: state, TextHash: HashWriteText(text)}
		if err := repo.BatchCreate(ctx, []*model.BrowserStep{row}); err != nil {
			t.Fatal(err)
		}
		return row.ID
	}
	mk(3, model.StepSubmitPrepared, "同一条评论")
	if _, err := repo.FindSubmitAttempt(ctx, taskID, h, 0); !isNotFound(err) {
		t.Errorf("prepared 不算尝试，got err=%v", err)
	}
	id := mk(3, model.StepSubmitUnattributed, "同一条评论")
	if _, err := repo.FindSubmitAttempt(ctx, taskID, h, 0); err != nil {
		t.Errorf("unattributed 必须算尝试: %v", err)
	}
	// 排除自身（同一行不能把自己查成「历史尝试」）
	if _, err := repo.FindSubmitAttempt(ctx, taskID, h, id); !isNotFound(err) {
		t.Errorf("excludeID 未生效: %v", err)
	}
	if _, err := repo.FindSubmitAttempt(ctx, taskID+1, h, 0); !isNotFound(err) {
		t.Error("跨任务不得互拦")
	}
	// 尝试记在别的 step_index 上仍算尝试（F-N4：键里不再有下标）。
	// 端到端那一面由 TestWSE2E_IndexDriftStillBlocked 守，这里只守 SQL 条件本身。
	mk(7, model.StepSubmitSent, "只在别的下标投过")
	if _, err := repo.FindSubmitAttempt(ctx, taskID, HashWriteText("只在别的下标投过"), 0); err != nil {
		t.Errorf("尝试记在 index=7 上，index=3 的下发也必须查到：漏闸就是这么来的（%v）", err)
	}
	if _, err := repo.FindSubmitAttempt(ctx, taskID, HashWriteText("另一条评论"), 0); !isNotFound(err) {
		t.Error("不同文本不得互拦")
	}
	for _, st := range []string{model.StepSubmitSent, model.StepSubmitVerified} {
		mk(5, st, "另一条评论")
		if _, err := repo.FindSubmitAttempt(ctx, taskID, HashWriteText("另一条评论"), 0); err != nil {
			t.Errorf("态 %s 必须算尝试: %v", st, err)
		}
	}
	// 空键永不拦（编排里 value 为空的写步本就 fails-loudly 在前置校验，闸不该再拦一次）
	if _, err := repo.FindSubmitAttempt(ctx, taskID, "", 0); !isNotFound(err) {
		t.Errorf("空 text_hash 不应产生命中: %v", err)
	}
}

func isNotFound(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }
