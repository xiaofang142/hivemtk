package service

// 行为锁：Brain 轮内的闸门是在「落库之前」生效的，不是执行失败之后。
//
// 谓词层的测试（brain_action_gate_b19h_test.go）只能证明判得对，证不出一条更要紧的事实：
// 被拒的动作**一行数据都不该留下**。旧实现里幻觉动作会先建一条 browser_steps 行、白烧一次
// CDP 往返、再在 dispatchStep 的 default 上失败——用户在编排面板看到的是 40 条看不懂的红灯，
// 而不是「模型吐了个服务端不认识的动作」。这一格只能靠真循环 + 真库回读，
// 所以本文件把 executeBrain 接上脚本化 plan（seam 见 brainPlanner），其余全真：
// 真 WS 帧、真 fake 扩展、真四张表。
//
// 三条腿分别锁：全幻觉（0 行落库）/ 混合（合法那步照常落库、幻觉不占步号）/
// screenshot（G17 那条已存在的拒绝，折进统一闸门后行为一字不差）。

import (
	"context"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/browser_automation/model"
)

// b19hStubBrain 脚本化 plan：plans 用尽后宣布 done（judge 由桩直接放行，不是本批的靶子）。
type b19hStubBrain struct {
	plans []string
	calls int
}

func (s *b19hStubBrain) GeneratePlanReflect(_ context.Context, _, _ uint, _, _, _ string, _ *reflectState) ([]byte, bool, error) {
	i := s.calls
	s.calls++
	if i >= len(s.plans) {
		return nil, true, nil
	}
	return []byte(s.plans[i]), false, nil
}

func (s *b19hStubBrain) LastPlanTokens() int   { return 0 }
func (s *b19hStubBrain) LastPlanModel() string { return "" }
func (s *b19hStubBrain) LastAuxTokens() int    { return 0 }

func (s *b19hStubBrain) JudgeDone(_ context.Context, _, _ uint, _, _ string) (bool, string) {
	return true, "stub 放行"
}

func (s *b19hStubBrain) SummarizeSession(_ context.Context, _, _ uint, _ string, _, _ int, _ string) string {
	return ""
}

// b19hRunBrain 起一条真 WS + 真库的 Brain 会话，跑完回读步表与会话行。
func b19hRunBrain(t *testing.T, plans []string) (*fakeExtension, []*model.BrowserStep, *model.BrowserSession) {
	t.Helper()
	exec, ext, bundle := newWSE2E(t, happyReply)
	task, session := bundle.seedTask(t, "[]", false)
	ctx := context.Background()
	task.BrainMode = true
	task.BrainGoal = "给第一篇笔记评论一句夸奖"
	if err := bundle.db.WithContext(ctx).Model(&model.BrowserTask{}).
		Where("id = ?", task.ID).
		Updates(map[string]any{"brain_mode": true, "brain_goal": task.BrainGoal}).Error; err != nil {
		t.Fatalf("任务改 Brain 模式失败: %v", err)
	}
	exec.brain = &b19hStubBrain{plans: plans}

	runCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	exec.ExecuteSession(runCtx, task, session, nil)

	return ext, bundle.steps(t, session.ID), bundle.reloadSession(t, session.ID)
}

func TestB19HBrainGateNeverPersistsRejectedSteps(t *testing.T) {
	ext, steps, sess := b19hRunBrain(t, []string{
		`[{"action":"hover","target":"@e1"},{"action":"发送评论","target":"@e2"}]`,
	})
	if len(steps) != 0 {
		got := make([]string, 0, len(steps))
		for _, s := range steps {
			got = append(got, s.Action+":"+s.Status)
		}
		t.Errorf("落库 %d 条步骤（%v），want 0：被拒的动作连一行都不该留下", len(steps), got)
	}
	if strings.Contains(ext.actions(), "hover") {
		t.Errorf("幻觉动作被下发到了扩展侧（帧序列 %s）：闸门没在派发前挡住", ext.actions())
	}
	// 会话本身要正常收敛（done→judge→completed），不能因为「本轮没有可执行步」判红：
	// 拒的是那一步，不是这次会话——红在这里会把一次模型抖动放大成用户看到的任务失败。
	if sess.Status != "completed" {
		t.Errorf("终态=%s（%s），want completed", sess.Status, sess.ErrorMsg)
	}
}

func TestB19HBrainGateKeepsLegalStepsInMixedPlan(t *testing.T) {
	_, steps, sess := b19hRunBrain(t, []string{
		`[{"action":"download_page"},{"action":"snapshot"}]`,
	})
	if len(steps) != 1 {
		t.Fatalf("落库 %d 条步骤，want 1（幻觉被拒、snapshot 照常执行）", len(steps))
	}
	if steps[0].Action != "snapshot" || steps[0].Status != "success" {
		t.Errorf("落库步骤=%s/%s，want snapshot/success", steps[0].Action, steps[0].Status)
	}
	// 步号从 0 起：幻觉步不许悄悄占一个 step_index，否则面板上的步骤序号会跳号
	if steps[0].StepIndex != 0 {
		t.Errorf("合法步 step_index=%d want 0", steps[0].StepIndex)
	}
	if sess.TotalSteps != 1 || sess.SuccessSteps != 1 {
		t.Errorf("会话计数 total=%d success=%d want 1/1", sess.TotalSteps, sess.SuccessSteps)
	}
}

// G17 的 screenshot 闸是这条统一闸门里最早的一条腿：折进谓词后，
// 拒绝文案与「不落库、不下发」的行为必须逐字保持。
func TestB19HBrainGateStillRejectsScreenshot(t *testing.T) {
	ext, steps, _ := b19hRunBrain(t, []string{`[{"action":"screenshot"}]`})
	if len(steps) != 0 {
		t.Errorf("Brain 轮内 screenshot 落了 %d 条步骤，want 0：G17 抢焦点闸失效", len(steps))
	}
	if strings.Contains(ext.actions(), "screenshot") {
		t.Errorf("screenshot 帧被下发（%s）：会激活 tab 抢用户焦点", ext.actions())
	}
}
