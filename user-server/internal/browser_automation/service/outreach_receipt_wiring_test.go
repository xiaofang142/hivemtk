package service

// 触达回执（Chunk5）接线用例：判据落在库里那张 browser_outreach_receipts 表上
// ——「verified 与 unattributed 都留痕」「只读步零回执」「接线缺失不判红」。

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/browser_automation/repository"
)

// recordingReceiptRepo 记录每次落库的完整回执行，可注入错误。
type recordingReceiptRepo struct {
	repository.BrowserOutreachReceiptRepository
	records []*model.BrowserOutreachReceipt
	err     error
}

func (r *recordingReceiptRepo) RecordOutreachReceipt(_ context.Context, rec *model.BrowserOutreachReceipt) error {
	if r.err != nil {
		return r.err
	}
	r.records = append(r.records, rec)
	return nil
}

// 1) post_comment verified ⇒ 收口后落 1 行回执，三样交付物齐（帖子链接/文案原文/截图）。
func TestOutreachReceiptRecordsVerifiedSend(t *testing.T) {
	exec, _, bundle := newWSE2E(t, happyReply)
	repo := &recordingReceiptRepo{}
	exec.SetOutreachReceiptRepository(repo)
	task, session := bundle.seedTask(t, dedupeSteps, false)

	steps, err := ParseSteps([]byte(dedupeSteps))
	if err != nil {
		t.Fatal(err)
	}
	step := dedupePostCommentStep(t)
	seq := 0
	ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
	t.Cleanup(cancel)
	// 先跑 open_tab：回执的帖子链接在去重键没冻结时靠快照兜底，而快照需要活的 tab
	if status, errMsg, _ := exec.executeStepWithRetry(ctx, task, session, 0, steps[0], nil, &seq, ""); status != "success" {
		t.Fatalf("open_tab 步应成功，实得 %q %q", status, errMsg)
	}
	// 显式路径没有 pageURL 现货（传 ""），这正是回执必须自己取链接的那条腿
	if status, errMsg, _ := exec.executeStepWithRetry(ctx, task, session, 1, step, nil, &seq, ""); status != "success" {
		t.Fatalf("happyReply 下写步应成功，实得 %q %q", status, errMsg)
	}

	exec.captureSessionReceipts(ctx, task, session, &seq)

	if len(repo.records) != 1 {
		t.Fatalf("verified 触达应恰好落 1 行回执，实得 %d 行", len(repo.records))
	}
	rec := repo.records[0]
	if !rec.Verified {
		t.Errorf("回查已见评论，回执行应 verified=true")
	}
	if rec.TargetURL != "https://www.xiaohongshu.com/explore/abc" {
		t.Errorf("回执行应带帖子链接，实得 %q", rec.TargetURL)
	}
	if rec.CopySnapshot != step.Value {
		t.Errorf("回执行应带文案原文快照，实得 %q want %q", rec.CopySnapshot, step.Value)
	}
	if rec.CopyHash == "" {
		t.Error("回执行应带 copy_hash 以便与去重行 join")
	}
	if rec.TaskID != task.ID || rec.SessionID != session.ID {
		t.Errorf("回执行归属错位：task=%d session=%d want task=%d session=%d", rec.TaskID, rec.SessionID, task.ID, session.ID)
	}
	if len(rec.Evidence) == 0 {
		t.Error("回执行应带回查证据（comment_verify 回包）")
	}
}

// 2) 只读步 ⇒ 零回执、零截图（否则验收面变成噪声，噪声会让人不再看它）。
func TestOutreachReceiptSkipsReadOnlySteps(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, happyReply)
	repo := &recordingReceiptRepo{}
	exec.SetOutreachReceiptRepository(repo)
	task, session := bundle.seedTask(t, dedupeSteps, false)

	steps, err := ParseSteps([]byte(dedupeSteps))
	if err != nil {
		t.Fatal(err)
	}
	seq := 0
	ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
	t.Cleanup(cancel)
	if status, errMsg, _ := exec.executeStepWithRetry(ctx, task, session, 0, steps[0], nil, &seq, ""); status != "success" {
		t.Fatalf("open_tab 步应成功，实得 %q %q", status, errMsg)
	}
	exec.captureSessionReceipts(ctx, task, session, &seq)

	if len(repo.records) != 0 {
		t.Errorf("只读步不该产回执，实得 %d 行", len(repo.records))
	}
	if strings.Contains(ext.actions(), "screenshot") {
		t.Errorf("无触达时不该抢焦点截图（全部动作：%s）", ext.actions())
	}
}

// 3) 未接线 ⇒ 写步仍绿（回执是增强层，判红会让人重跑一次已经真发出去的触达）。
func TestOutreachReceiptNilRepoFailsOpen(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, happyReply)
	task, session := bundle.seedTask(t, dedupeSteps, false)

	seq := 0
	ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
	t.Cleanup(cancel)
	if status, errMsg, _ := exec.executeStepWithRetry(ctx, task, session, 1, dedupePostCommentStep(t), nil, &seq, "https://www.xiaohongshu.com/explore/abc"); status != "success" {
		t.Fatalf("回执仓储未接线应照常绿返，实得 %q %q", status, errMsg)
	}
	if ext.countOf("comment_send") != 1 {
		t.Errorf("评论应已发出，实得 %d 次", ext.countOf("comment_send"))
	}
	// 收口也不该炸
	exec.captureSessionReceipts(ctx, task, session, &seq)
}

// 4) 落库报错 ⇒ 仍绿（warn-only）。
func TestOutreachReceiptRecordErrorStaysGreen(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, happyReply)
	exec.SetOutreachReceiptRepository(&recordingReceiptRepo{err: context.DeadlineExceeded})
	task, session := bundle.seedTask(t, dedupeSteps, false)

	seq := 0
	ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
	t.Cleanup(cancel)
	if status, errMsg, _ := exec.executeStepWithRetry(ctx, task, session, 1, dedupePostCommentStep(t), nil, &seq, "https://www.xiaohongshu.com/explore/abc"); status != "success" {
		t.Fatalf("回执落库失败不得判红，实得 %q %q", status, errMsg)
	}
	if ext.countOf("comment_send") != 1 {
		t.Errorf("评论应已发出，实得 %d 次", ext.countOf("comment_send"))
	}
}

// 5) 同一会话两条触达 ⇒ 两行回执（一条回执代表不了「这次活动发了什么」）。
func TestOutreachReceiptOneRowPerTouch(t *testing.T) {
	exec, _, bundle := newWSE2E(t, happyReply)
	repo := &recordingReceiptRepo{}
	exec.SetOutreachReceiptRepository(repo)
	task, session := bundle.seedTask(t, dedupeSteps, false)

	first := dedupePostCommentStep(t)
	second := dedupePostCommentStep(t)
	second.Value = second.Value + "（第二条）"

	seq := 0
	ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
	t.Cleanup(cancel)
	for i, st := range []parsedStep{first, second} {
		if status, errMsg, _ := exec.executeStepWithRetry(ctx, task, session, 1, st, nil, &seq, "https://www.xiaohongshu.com/explore/abc"); status != "success" {
			t.Fatalf("第 %d 条触达应成功，实得 %q %q", i+1, status, errMsg)
		}
	}
	exec.captureSessionReceipts(ctx, task, session, &seq)

	if len(repo.records) != 2 {
		t.Fatalf("两条触达应落 2 行回执，实得 %d", len(repo.records))
	}
	if repo.records[0].CopyHash == repo.records[1].CopyHash {
		t.Error("两条文案不同的触达 copy_hash 应不同（否则验收面分不清是哪条）")
	}
}

// 6) 收口幂等：第二次收口不重复落（否则重试腿会把同一会话的回执翻倍）。
func TestOutreachReceiptCaptureIsIdempotent(t *testing.T) {
	exec, _, bundle := newWSE2E(t, happyReply)
	repo := &recordingReceiptRepo{}
	exec.SetOutreachReceiptRepository(repo)
	task, session := bundle.seedTask(t, dedupeSteps, false)

	seq := 0
	ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
	t.Cleanup(cancel)
	if status, _, _ := exec.executeStepWithRetry(ctx, task, session, 1, dedupePostCommentStep(t), nil, &seq, "https://www.xiaohongshu.com/explore/abc"); status != "success" {
		t.Fatal("写步应成功")
	}
	exec.captureSessionReceipts(ctx, task, session, &seq)
	exec.captureSessionReceipts(ctx, task, session, &seq)

	if len(repo.records) != 1 {
		t.Errorf("收口重复调用应只落 1 行，实得 %d", len(repo.records))
	}
}

// 7) 回查未见评论（unattributed）⇒ 照样落回执但 verified=false。
// 「平台侧归因不到」与「没发出去」是两件事：回执只留绿的等于替平台做了裁决，
// 而双发闸正是因为分不清才把人叫来。
func TestOutreachReceiptRecordsUnattributedSend(t *testing.T) {
	unseen := func(action string, frame map[string]any) (map[string]any, string) {
		if action == "comment_verify" {
			return map[string]any{"verified": false, "matched": 0}, ""
		}
		return happyReply(action, frame)
	}
	exec, ext, bundle := newWSE2E(t, unseen)
	repo := &recordingReceiptRepo{}
	exec.SetOutreachReceiptRepository(repo)
	task, session := bundle.seedTask(t, dedupeSteps, false)

	steps, err := ParseSteps([]byte(dedupeSteps))
	if err != nil {
		t.Fatal(err)
	}
	seq := 0
	ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
	t.Cleanup(cancel)
	for i, st := range []parsedStep{steps[0], dedupePostCommentStep(t)} {
		if status, _, _ := exec.executeStepWithRetry(ctx, task, session, i, st, nil, &seq, ""); status != "success" && status != "failed" {
			t.Fatalf("第 %d 步状态异常：%q", i, status)
		}
	}
	exec.captureSessionReceipts(ctx, task, session, &seq)

	if len(repo.records) != 1 {
		t.Fatalf("unattributed 触达也应落 1 行回执，实得 %d 行", len(repo.records))
	}
	if repo.records[0].Verified {
		t.Error("回查未见评论，回执行不得标 verified=true")
	}
	if ext.countOf("comment_send") != 1 {
		t.Errorf("评论确实发出去过（这正是要留痕的事），comment_send 实得 %d 次", ext.countOf("comment_send"))
	}
}
