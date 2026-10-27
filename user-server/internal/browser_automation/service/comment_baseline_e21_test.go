package service

// comment_baseline_e21_test.go —— E21：提交前基线拿不到 = 拒绝提交。
//
// 被锁的行为：post_comment 的新旧判据依赖「提交前评论区里有几条同文本条目」。
// 旧实现在这里**降级**：取不到基线就按「页面上有没有这句话」判，并在证据里打
// recency_unproven=true。留痕不改变结论——回执照样可以落 verified=true，
// 于是 session644 那种假绿只是换了个说法（「有标签的假绿」）。
//
// 现形态：基线在**任何写动作之前**读（prep 之前；prep 只是在往输入框填字，
// 但它之后就进入有副作用的区段了），读不到就直接拒绝提交。拒绝的形状必须干净：
//   - 一条帧都不下发（comment_prep / comment_send 的 count 都是 0）——「拒绝提交」
//     这句话要有帧序证据，而不是只靠文案；
//   - 台账不留任何提交态（submit_state 为空）——否则下一次合法重发会被自己的台账拦死；
//   - 失败文案自证「点击从未发生」，让人敢直接重跑。

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/browser_automation/model"
)

// 基线读不出来（extract 报错）：必须在 prep 之前就停手，零写帧、零台账。
func TestWSE2E_BaselineUnavailableRefusesToSubmit(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, func(action string, frame map[string]any) (map[string]any, string) {
		if action == "extract" {
			return nil, "extract_failed: 页面主线程拥堵"
		}
		return happyReply(action, frame)
	})
	task, session := bundle.seedTask(t, threeStageSteps, false)
	steps, err := ParseSteps([]byte(threeStageSteps))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	got := bundle.reloadSession(t, session.ID)
	if got.Status != "failed" {
		t.Fatalf("终态=%s want failed（基线不可用时不许判绿）", got.Status)
	}
	if !strings.Contains(got.ErrorMsg, "提交前基线不可用") {
		t.Errorf("失败归因应点名基线不可用，实际: %s", got.ErrorMsg)
	}
	if !strings.Contains(got.ErrorMsg, "本次点击从未发生") {
		t.Errorf("失败归因应自证「未点击」（人才敢直接重跑），实际: %s", got.ErrorMsg)
	}
	// 「拒绝提交」要有帧序证据：prep 也不许发（发了就已经有副作用了）
	if n := ext.countOf("comment_prep"); n != 0 {
		t.Errorf("comment_prep 发生 %d 次，want 0：基线读不到时任何写侧动作都不许下发", n)
	}
	if n := ext.countOf("comment_send"); n != 0 {
		t.Errorf("comment_send 发生 %d 次，want 0", n)
	}
	// 台账：从来没进过提交态，下一次重跑必须能正常下发
	row := readStepState(t, bundle, session.ID, 3)
	if row.SubmitState != "" {
		t.Errorf("submit_state=%s want 空：一次没下发的拒绝不许在台账上留下尝试", row.SubmitState)
	}
	if _, err := bundle.stepRepo.FindSubmitAttempt(context.Background(), task.ID, row.TextHash, 0); !isNotFound(err) {
		t.Errorf("不该算提交尝试，got err=%v", err)
	}
	if row.Status != "failed" {
		t.Errorf("step status=%s want failed", row.Status)
	}
}

// 基线第一次读失败、第二次成功：不许把一个健康任务判死（读两遍的理由）。
func TestWSE2E_BaselineRetriedOnceBeforeGivingUp(t *testing.T) {
	var extractCalls int
	exec, ext, bundle := newWSE2E(t, func(action string, frame map[string]any) (map[string]any, string) {
		if action == "extract" {
			extractCalls++
			if extractCalls == 1 {
				return nil, "extract_failed: 瞬时抖动"
			}
		}
		return happyReply(action, frame)
	})
	task, session := bundle.seedTask(t, threeStageSteps, false)
	steps, err := ParseSteps([]byte(threeStageSteps))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	got := bundle.reloadSession(t, session.ID)
	if got.Status != "completed" {
		t.Fatalf("终态=%s（%s）want completed：基线是一次只读、可安全重读的计数", got.Status, got.ErrorMsg)
	}
	if n := ext.countOf("comment_send"); n != 1 {
		t.Errorf("comment_send=%d want 1（重读基线不许变成重发）", n)
	}
	row := readStepState(t, bundle, session.ID, 3)
	if row.SubmitState != model.StepSubmitVerified {
		t.Errorf("submit_state=%s want verified", row.SubmitState)
	}
}
