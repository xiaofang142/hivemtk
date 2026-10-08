package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/browser_automation/repository"
)

// 触达去重（Chunk3）接线用例：判据落在**库里的步行**与**命令日志**上，
// 不看 resolveOutreachDedupe 的返回值——那层有单测，接线漏了它照样全绿。

// dedupeHitRepo 命中固定四元组的去重替身。
type dedupeHitRepo struct {
	repository.BrowserOutreachDedupeRepository
	records []*model.BrowserOutreachDedupe
	recErr  error
}

func (f *dedupeHitRepo) FindOutreachHit(_ context.Context, platform, targetURL, action, copyHash string) (*model.BrowserOutreachDedupe, error) {
	if platform == "xiaohongshu" && targetURL == "https://www.xiaohongshu.com/explore/abc" && action == "post_comment" && copyHash != "" {
		return &model.BrowserOutreachDedupe{Platform: platform, TargetURL: targetURL, Action: action, CopyHash: copyHash, TaskID: 999}, nil
	}
	return nil, nil
}

func (f *dedupeHitRepo) RecordOutreachSend(_ context.Context, rec *model.BrowserOutreachDedupe) error {
	if f.recErr != nil {
		return f.recErr
	}
	f.records = append(f.records, rec)
	return nil
}

const dedupeSteps = `[{"action":"open_tab","target":"https://www.xiaohongshu.com/explore"},
{"action":"post_comment","value":"测试评论正文"}]`

func dedupePostCommentStep(t *testing.T) parsedStep {
	t.Helper()
	steps, err := ParseSteps([]byte(dedupeSteps))
	if err != nil {
		t.Fatal(err)
	}
	return steps[1]
}

// 1) 命中 ⇒ 整步 skipped 且「绿返」（errMsg 空 = 不计成败），一帧写命令都没下发。
func TestOutreachDedupeHitSkipsWriteStepGreen(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, happyReply)
	exec.SetOutreachDedupeRepository(&dedupeHitRepo{})
	task, session := bundle.seedTask(t, dedupeSteps, false)
	step := dedupePostCommentStep(t)

	seq := 0
	ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
	t.Cleanup(cancel)
	status, errMsg, _ := exec.executeStepWithRetry(ctx, task, session, 1, step, nil, &seq, "")

	if status != "skipped" {
		t.Fatalf("去重命中应整步 skipped，实得 %q %q", status, errMsg)
	}
	if errMsg != "" {
		t.Errorf("去重命中须绿返（errMsg 空），实得 %q——否则调用方会把整轮判红", errMsg)
	}
	for _, sent := range []string{"comment_prep", "comment_send", "comment_verify"} {
		if n := ext.countOf(sent); n != 0 {
			t.Errorf("去重命中后不应下发 %s，实得 %d 次（全部动作：%s）", sent, n, ext.actions())
		}
	}

	// 库里那一行必须是 skipped（不是 failed），审计面才读得出「没发」而不是「发失败了」
	var row model.BrowserStep
	if err := bundle.db.WithContext(ctx).Where("task_id = ? AND step_index = ?", task.ID, 1).First(&row).Error; err != nil {
		t.Fatalf("步行回读失败: %v", err)
	}
	if row.Status != "skipped" {
		t.Errorf("步行状态=%q want skipped", row.Status)
	}
	if !strings.Contains(row.ErrorMsg, "触达去重") {
		t.Errorf("步行原因应点名触达去重，实得 %q", row.ErrorMsg)
	}

	// 去重帧必须进审计包：跳过是观测到的决策，不是沉默
	var payload string
	if err := bundle.db.WithContext(ctx).Model(&model.BrowserCommandLog{}).
		Where("session_id = ? AND action = ?", session.ID, "post_comment").
		Order("seq asc").Pluck("payload", &payload).Error; err != nil {
		t.Fatalf("command_log 回读失败: %v", err)
	}
	if !strings.Contains(payload, "outreach_already_sent") {
		t.Errorf("审计帧应带去重归因 outreach_already_sent，实得 %q", payload)
	}
}

// 2) 未命中 ⇒ 正常走完写链路，且 verified 后落去重行（键沿用检查点冻结的那份）。
func TestOutreachDedupeMissRecordsRowAfterVerified(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, happyReply)
	repo := &missDedupeRepo{}
	exec.SetOutreachDedupeRepository(repo)
	task, session := bundle.seedTask(t, dedupeSteps, false)
	step := dedupePostCommentStep(t)

	seq := 0
	ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
	t.Cleanup(cancel)
	// pageURL 给现货：这条腿上不必为补 URL 多打一次 snapshot
	status, errMsg, _ := exec.executeStepWithRetry(ctx, task, session, 1, step, nil, &seq, "https://www.xiaohongshu.com/explore/abc")

	if status != "success" {
		t.Fatalf("未命中应正常发送并成功，实得 %q %q", status, errMsg)
	}
	if ext.countOf("comment_send") != 1 {
		t.Fatalf("未命中时应下发一次 comment_send，实得 %d 次（全部动作：%s）", ext.countOf("comment_send"), ext.actions())
	}
	if len(repo.records) != 1 {
		t.Fatalf("verified 后应落 1 条去重行，实得 %d 条", len(repo.records))
	}
	got := repo.records[0]
	if got.Platform != "xiaohongshu" || got.TargetURL != "https://www.xiaohongshu.com/explore/abc" ||
		got.Action != "post_comment" || got.CopyHash != writeStepKey(step) {
		t.Errorf("去重行四元组不符：%+v（want hash=%s）", got, writeStepKey(step))
	}
	if got.TaskID != task.ID || got.SessionID != session.ID {
		t.Errorf("去重行归属错：task=%d session=%d want %d/%d", got.TaskID, got.SessionID, task.ID, session.ID)
	}
}

// missDedupeRepo 永不命中的去重替身（带可选落库失败注入）。
type missDedupeRepo struct {
	repository.BrowserOutreachDedupeRepository
	records []*model.BrowserOutreachDedupe
	recErr  error
}

func (f *missDedupeRepo) FindOutreachHit(_ context.Context, _, _, _, _ string) (*model.BrowserOutreachDedupe, error) {
	return nil, nil
}

func (f *missDedupeRepo) RecordOutreachSend(_ context.Context, rec *model.BrowserOutreachDedupe) error {
	if f.recErr != nil {
		return f.recErr
	}
	f.records = append(f.records, rec)
	return nil
}

// 3) 去重行落库失败不得改判：内容已发布是事实，去重行只是优化层。
func TestOutreachDedupeRecordFailureStaysGreen(t *testing.T) {
	exec, _, bundle := newWSE2E(t, happyReply)
	exec.SetOutreachDedupeRepository(&missDedupeRepo{recErr: errors.New("去重表不可写")})
	task, session := bundle.seedTask(t, dedupeSteps, false)
	step := dedupePostCommentStep(t)

	seq := 0
	ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
	t.Cleanup(cancel)
	status, errMsg, _ := exec.executeStepWithRetry(ctx, task, session, 1, step, nil, &seq, "https://www.xiaohongshu.com/explore/abc")

	if status != "success" {
		t.Fatalf("去重行写不进去只是优化层退化，步仍应成功，实得 %q %q", status, errMsg)
	}
}

// 4) 去重层未接线时全程 fail-open：既有写步行为一字不变。
func TestOutreachDedupeNilRepoFailOpen(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, happyReply)
	task, session := bundle.seedTask(t, dedupeSteps, false)
	step := dedupePostCommentStep(t)

	seq := 0
	ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
	t.Cleanup(cancel)
	status, errMsg, _ := exec.executeStepWithRetry(ctx, task, session, 1, step, nil, &seq, "")

	if status != "success" {
		t.Fatalf("未接线去重不应影响写步，实得 %q %q", status, errMsg)
	}
	if ext.countOf("comment_send") != 1 {
		t.Errorf("未接线时 comment_send 应恰好一次，实得 %d", ext.countOf("comment_send"))
	}
}

// 5) 只读步绝不走去重：去重键含 writeKey，只读步的 writeKey 为空，
// 若闸门装错位置就会把每个只读步都查一遍库（并可能因 action 不同而误命中）。
func TestOutreachDedupeSkipsReadOnlySteps(t *testing.T) {
	exec, _, bundle := newWSE2E(t, happyReply)
	repo := &countingDedupeRepo{}
	exec.SetOutreachDedupeRepository(repo)
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
	if repo.calls != 0 {
		t.Errorf("只读步不应查去重库，实得 %d 次查询", repo.calls)
	}
}

type countingDedupeRepo struct {
	repository.BrowserOutreachDedupeRepository
	calls int
}

func (c *countingDedupeRepo) FindOutreachHit(_ context.Context, _, _, _, _ string) (*model.BrowserOutreachDedupe, error) {
	c.calls++
	return nil, nil
}

func (c *countingDedupeRepo) RecordOutreachSend(_ context.Context, _ *model.BrowserOutreachDedupe) error {
	return nil
}
