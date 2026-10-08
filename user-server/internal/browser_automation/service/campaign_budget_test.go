package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/browser_automation/repository"
)

// 活动预算（Chunk4）接线用例：判据落在「写步有没有真的下发」+「库里的步行状态」
// +「审计帧归因串」上，不看 consumeCampaignActBudget 的返回值。

// campaignBudgetRepo 记录调用次数与入参，并可注入放行/拒绝/报错。
type campaignBudgetRepo struct {
	repository.BrowserTaskRepository
	calls   int
	lastIDs []uint
	ok      bool
	err     error
}

func (c *campaignBudgetRepo) TryConsumeCampaignActBudget(_ context.Context, taskID uint) (bool, error) {
	c.calls++
	c.lastIDs = append(c.lastIDs, taskID)
	if c.err != nil {
		return false, c.err
	}
	return c.ok, nil
}

// 1) 预算耗尽 ⇒ 写步一步不下发，红返并把归因写进审计帧。
func TestCampaignBudgetExhaustedBlocksWriteStep(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, happyReply)
	repo := &campaignBudgetRepo{ok: false}
	exec.SetTaskRepository(repo)
	task, session := bundle.seedTask(t, dedupeSteps, false)
	task.CampaignKey = "2026-10-autumn"
	task.CampaignActBudget = 2
	task.CampaignActUsed = 2
	step := dedupePostCommentStep(t)

	seq := 0
	ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
	t.Cleanup(cancel)
	status, errMsg, _ := exec.executeStepWithRetry(ctx, task, session, 1, step, nil, &seq, "https://www.xiaohongshu.com/explore/abc")

	if status != "failed" {
		t.Fatalf("预算耗尽应判红（运营上限被打到是配置事实，不是异常），实得 %q %q", status, errMsg)
	}
	if !strings.Contains(errMsg, "活动触达预算已耗尽") {
		t.Errorf("errMsg 应点名预算耗尽，实得 %q", errMsg)
	}
	if n := ext.countOf("comment_send"); n != 0 {
		t.Errorf("预算耗尽时评论不得真发出去，comment_send 实得 %d 次（全部动作：%s）", n, ext.actions())
	}
	if repo.calls != 1 {
		t.Errorf("写步应恰好扣减一次预算，实得 %d 次", repo.calls)
	}
	if len(repo.lastIDs) == 1 && repo.lastIDs[0] != task.ID {
		t.Errorf("扣减应打在当前任务上，实得 taskID=%d want %d", repo.lastIDs[0], task.ID)
	}

	var row model.BrowserStep
	if err := bundle.db.WithContext(ctx).Where("task_id = ? AND step_index = ?", task.ID, 1).First(&row).Error; err != nil {
		t.Fatalf("步行回读失败: %v", err)
	}
	if row.Status != "failed" || !strings.Contains(row.ErrorMsg, "预算已耗尽") {
		t.Errorf("步行应落 failed+预算文案，实得 %q / %q", row.Status, row.ErrorMsg)
	}

	var payload string
	if err := bundle.db.WithContext(ctx).Model(&model.BrowserCommandLog{}).
		Where("session_id = ? AND action = ?", session.ID, "post_comment").
		Order("seq asc").Pluck("payload", &payload).Error; err != nil {
		t.Fatalf("command_log 回读失败: %v", err)
	}
	if !strings.Contains(payload, "campaign_budget_exhausted") {
		t.Errorf("审计帧应带预算归因，实得 %q", payload)
	}
	if !strings.Contains(payload, "latency_ms") {
		t.Errorf("每步事件帧都应带 latency_ms（时间线最小公约数），实得 %q", payload)
	}
}

// 2) 未配置预算 ⇒ 一次库都不查，写步照旧发出去。
func TestCampaignBudgetUnsetSkipsQuery(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  func(*model.BrowserTask)
	}{
		{"无活动键", func(tk *model.BrowserTask) { tk.CampaignKey = "" }},
		{"预算为零", func(tk *model.BrowserTask) { tk.CampaignKey = "k"; tk.CampaignActBudget = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exec, ext, bundle := newWSE2E(t, happyReply)
			repo := &campaignBudgetRepo{ok: true}
			exec.SetTaskRepository(repo)
			task, session := bundle.seedTask(t, dedupeSteps, false)
			tc.mut(task)

			seq := 0
			ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
			t.Cleanup(cancel)
			if status, errMsg, _ := exec.executeStepWithRetry(ctx, task, session, 1, dedupePostCommentStep(t), nil, &seq, "https://www.xiaohongshu.com/explore/abc"); status != "success" {
				t.Fatalf("未配置预算应原样放行，实得 %q %q", status, errMsg)
			}
			if repo.calls != 0 {
				t.Errorf("不限任务不该为每条写步白付一次 UPDATE，实查 %d 次", repo.calls)
			}
			if ext.countOf("comment_send") != 1 {
				t.Errorf("comment_send 应恰好一次，实得 %d（全部动作：%s）", ext.countOf("comment_send"), ext.actions())
			}
		})
	}
}

// 3) 预算充足 ⇒ 扣减一次并放行。
func TestCampaignBudgetConsumeAllowsWrite(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, happyReply)
	repo := &campaignBudgetRepo{ok: true}
	exec.SetTaskRepository(repo)
	task, session := bundle.seedTask(t, dedupeSteps, false)
	task.CampaignKey = "2026-10-autumn"
	task.CampaignActBudget = 10
	task.CampaignActUsed = 1

	seq := 0
	ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
	t.Cleanup(cancel)
	if status, errMsg, _ := exec.executeStepWithRetry(ctx, task, session, 1, dedupePostCommentStep(t), nil, &seq, "https://www.xiaohongshu.com/explore/abc"); status != "success" {
		t.Fatalf("预算充足应放行，实得 %q %q", status, errMsg)
	}
	if repo.calls != 1 {
		t.Errorf("应恰好扣减一次，实得 %d", repo.calls)
	}
	if len(repo.lastIDs) == 1 && repo.lastIDs[0] != task.ID {
		t.Errorf("扣减应打在当前任务，实得 %d want %d", repo.lastIDs[0], task.ID)
	}
	if ext.countOf("comment_send") != 1 {
		t.Errorf("comment_send 应恰好一次，实得 %d", ext.countOf("comment_send"))
	}
}

// 4) taskRepo 未接线 ⇒ fail-open（漏装 SetTaskRepository 不该让触达线停摆）。
func TestCampaignBudgetNilRepoFailsOpen(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, happyReply)
	task, session := bundle.seedTask(t, dedupeSteps, false)
	task.CampaignKey = "2026-10-autumn"
	task.CampaignActBudget = 1
	task.CampaignActUsed = 1

	seq := 0
	ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
	t.Cleanup(cancel)
	if status, errMsg, _ := exec.executeStepWithRetry(ctx, task, session, 1, dedupePostCommentStep(t), nil, &seq, "https://www.xiaohongshu.com/explore/abc"); status != "success" {
		t.Fatalf("taskRepo 未接线应 fail-open，实得 %q %q", status, errMsg)
	}
	if ext.countOf("comment_send") != 1 {
		t.Errorf("fail-open 仍应发出评论，实得 %d 次", ext.countOf("comment_send"))
	}
}

// 5) DB 报错 ⇒ fail-open（读侧故障不该变成写侧永久停摆）。
func TestCampaignBudgetRepoErrorFailsOpen(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, happyReply)
	exec.SetTaskRepository(&campaignBudgetRepo{err: errors.New("预算表不可读")})
	task, session := bundle.seedTask(t, dedupeSteps, false)
	task.CampaignKey = "2026-10-autumn"
	task.CampaignActBudget = 1

	seq := 0
	ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
	t.Cleanup(cancel)
	if status, errMsg, _ := exec.executeStepWithRetry(ctx, task, session, 1, dedupePostCommentStep(t), nil, &seq, "https://www.xiaohongshu.com/explore/abc"); status != "success" {
		t.Fatalf("预算查询报错应 fail-open，实得 %q %q", status, errMsg)
	}
	if ext.countOf("comment_send") != 1 {
		t.Errorf("fail-open 仍应发出评论，实得 %d 次", ext.countOf("comment_send"))
	}
}

// 6) 只读步不扣预算：额度是「触达」条数，读页面不该收费。
func TestCampaignBudgetSkipsReadOnlySteps(t *testing.T) {
	exec, _, bundle := newWSE2E(t, happyReply)
	repo := &campaignBudgetRepo{ok: true}
	exec.SetTaskRepository(repo)
	task, session := bundle.seedTask(t, dedupeSteps, false)
	task.CampaignKey = "2026-10-autumn"
	task.CampaignActBudget = 5

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
		t.Errorf("只读步不该消耗活动额度，实查 %d 次", repo.calls)
	}
}

// 纯函数层：闸门口径的取值与文案。
func TestTaskTokenBudgetPrefersTaskField(t *testing.T) {
	if got := taskTokenBudget(&model.BrowserTask{TokenBudget: 12345}); got != 12345 {
		t.Errorf("任务字段应覆盖默认常量，实得 %d", got)
	}
	if got, want := taskTokenBudget(&model.BrowserTask{}), brainTokenBudget(); got != want {
		t.Errorf("TokenBudget=0 应回落默认常量，实得 %d want %d", got, want)
	}
	if got := taskTokenBudget(nil); got != brainTokenBudget() {
		t.Errorf("nil 任务不应 panic，应回落默认常量，实得 %d", got)
	}
}

func TestCampaignActBudgetLimited(t *testing.T) {
	for _, tc := range []struct {
		name string
		task *model.BrowserTask
		want bool
	}{
		{"键+额度齐全", &model.BrowserTask{CampaignKey: "k", CampaignActBudget: 1}, true},
		{"无活动键", &model.BrowserTask{CampaignActBudget: 1}, false},
		{"额度为零", &model.BrowserTask{CampaignKey: "k"}, false},
		{"nil", nil, false},
	} {
		if got := campaignActBudgetLimited(tc.task); got != tc.want {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestCampaignBudgetExhaustedMsgNamesActivity(t *testing.T) {
	msg := campaignBudgetExhaustedMsg(&model.BrowserTask{CampaignKey: "2026-10-autumn", CampaignActUsed: 50, CampaignActBudget: 50})
	for _, want := range []string{"2026-10-autumn", "50/50", "未下发"} {
		if !strings.Contains(msg, want) {
			t.Errorf("拒绝文案应含 %q，实得 %q", want, msg)
		}
	}
}

func TestCampaignBudgetSnapshotNilWhenUnlimited(t *testing.T) {
	if got := campaignBudgetSnapshot(&model.BrowserTask{}); got != nil {
		t.Errorf("未配置预算时快照应为 nil（而不是 0/0 让人误读），实得 %+v", got)
	}
	snap := campaignBudgetSnapshot(&model.BrowserTask{CampaignKey: "k", CampaignActBudget: 9, CampaignActUsed: 3})
	if snap["campaign_key"] != "k" || snap["campaign_act_budget"] != 9 || snap["campaign_act_used"] != 3 {
		t.Errorf("水位快照字段不符：%+v", snap)
	}
}

// mergeAuditMeta 不许覆盖调用方已写的同名字段（否则会把归因串洗掉）。
func TestMergeAuditMetaKeepsCallerFields(t *testing.T) {
	out := mergeAuditMeta(map[string]any{"reason": "campaign_budget_exhausted"}, map[string]any{"latency_ms": int64(7)})
	if out["reason"] != "campaign_budget_exhausted" {
		t.Errorf("调用方字段被覆盖了：%+v", out)
	}
	if out["latency_ms"] != int64(7) {
		t.Errorf("meta 应并入：%+v", out)
	}
	if got := mergeAuditMeta(nil, map[string]any{"latency_ms": int64(1)}); got["latency_ms"] != int64(1) {
		t.Errorf("nil payload 应自动建表：%+v", got)
	}
}

// namedBrain 带模型名的 brainPlanner 替身（只用于审计元数据取值）。
type namedBrain struct {
	fakeBrain
	modelName string
}

func (n *namedBrain) LastPlanModel() string { return n.modelName }

// 时间线元数据：显式编排（brain=nil）只给延迟，Brain 模式才补模型与 token。
func TestStepAuditMeta(t *testing.T) {
	plain := &Executor{}
	meta := plain.stepAuditMeta(12)
	if _, ok := meta["latency_ms"]; !ok {
		t.Errorf("延迟恒应写，实得 %+v", meta)
	}
	if _, ok := meta["llm_model"]; ok {
		t.Errorf("非 Brain 模式不得编造模型名，实得 %+v", meta)
	}

	noModel := &Executor{brain: &namedBrain{modelName: ""}}
	if _, ok := noModel.stepAuditMeta(3)["llm_model"]; ok {
		t.Errorf("模型名为空应整组不写（空串会被读成「模型名叫空」）")
	}

	named := &Executor{brain: &namedBrain{modelName: "qwen-plus"}}
	meta = named.stepAuditMeta(3)
	if meta["llm_model"] != "qwen-plus" || meta["llm_tokens"] != 0 {
		t.Errorf("Brain 模式应带 llm_model/llm_tokens，实得 %+v", meta)
	}
}
