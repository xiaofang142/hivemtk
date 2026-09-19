package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/browser_automation/dto"
	"hivemtk-user/internal/browser_automation/model"
)

// 批7 写步属性化 + 重试豁免。断的还是「DB 里那一行」和「到线几帧」，不是错误文案。
// 与 ledger_ws_test.go（批6）同一套真 WS 帧 + 真测试库手法。

// 1) 纯函数表：写步判定 = 声明 ∪ 推导，且推导不许被声明撤销
func TestIsWriteStepAttribution(t *testing.T) {
	task := &model.BrowserTask{Platform: "xiaohongshu"}
	cases := []struct {
		name string
		step parsedStep
		want bool
	}{
		{"post_comment 恒为写", parsedStep{StepItem: dto.StepItem{Action: "post_comment", Value: "x"}}, true},
		{"type+回车+命中评论框=写", parsedStep{StepItem: dto.StepItem{Action: "type", Target: ".content-textarea", SubmitOnEnter: true, Value: "x"}}, true},
		{"type+回车+搜索框≠写（收窄口径）", parsedStep{StepItem: dto.StepItem{Action: "type", Target: "input.search", SubmitOnEnter: true, Value: "x"}}, false},
		{"type 不回车≠写", parsedStep{StepItem: dto.StepItem{Action: "type", Target: ".content-textarea", Value: "x"}}, false},
		{"click 命中发送按钮=写", parsedStep{StepItem: dto.StepItem{Action: "click", Target: "button.submit"}}, true},
		{"click 命中评论框≠写", parsedStep{StepItem: dto.StepItem{Action: "click", Target: ".content-textarea"}}, false},
		{"click_near 按钮文案=发送 即写", parsedStep{StepItem: dto.StepItem{Action: "click_near", Anchor: ".comment", ButtonText: "发送"}}, true},
		{"click_near 按钮文案是「取消」≠写", parsedStep{StepItem: dto.StepItem{Action: "click_near", Anchor: ".comment", ButtonText: "取消"}}, false},
		{"声明位把普通步标成写=写", parsedStep{StepItem: dto.StepItem{Action: "click", Target: "div.like", IsWrite: true}}, true},
		// 这一条是「推导赢声明」的红线：LLM 把发送按钮标 is_write=false 摘不掉闸门
		{"声明撤销不了推导", parsedStep{StepItem: dto.StepItem{Action: "click", Target: "button.submit", IsWrite: false}}, true},
		{"scroll 恒非写", parsedStep{StepItem: dto.StepItem{Action: "scroll", Direction: "down", Amount: 5}}, false},
	}
	for _, c := range cases {
		got, why := isWriteStep(task, c.step)
		if got != c.want {
			t.Errorf("%s: isWriteStep=%v want %v（why=%q）", c.name, got, c.want, why)
		}
		if got && why == "" {
			t.Errorf("%s: 判成写却不给归因 why", c.name)
		}
	}
}

// 2) 定位表匹配：整串或逗号候选段都算命中，空值永不命中
func TestLocatorMatches(t *testing.T) {
	list := ".content-textarea, p.content-input"
	for _, tc := range []struct {
		loc, target string
		want        bool
	}{
		{list, ".content-textarea", true},
		{list, "p.content-input", true},
		{list, "  .content-textarea  ", true},
		{list, list, true},
		{list, ".other", false},
		{list, "", false},
		{"", ".content-textarea", false},
	} {
		if got := locatorMatches(tc.loc, tc.target); got != tc.want {
			t.Errorf("locatorMatches(%q,%q)=%v want %v", tc.loc, tc.target, got, tc.want)
		}
	}
}

// 3) 台账键：有正文用正文，无正文（发送点击）用定位兜底且同一步稳定
func TestWriteStepKeyShape(t *testing.T) {
	withText := parsedStep{StepItem: dto.StepItem{Action: "type", Target: ".content-textarea", Value: "今天 真好吃"}}
	if got := writeStepKey(withText); got != HashWriteText("今天真好吃") {
		t.Errorf("有正文时键必须等于正文哈希，got %q", got)
	}
	clickA := parsedStep{StepItem: dto.StepItem{Action: "click", Target: "button.submit"}}
	clickB := parsedStep{StepItem: dto.StepItem{Action: "click", Target: "button.submit"}}
	clickC := parsedStep{StepItem: dto.StepItem{Action: "click", Target: "button.danger"}}
	ka, kb := writeStepKey(clickA), writeStepKey(clickB)
	if ka == "" || ka != kb {
		t.Errorf("无正文写步必须仍有稳定键：ka=%q kb=%q", ka, kb)
	}
	if ka == writeStepKey(clickC) {
		t.Error("不同发送目标撞键（会把别的点击也算成同一次提交）")
	}
	if ka == writeStepKey(parsedStep{StepItem: dto.StepItem{Action: "click_near", Target: "button.submit"}}) {
		t.Error("action 不同却撞键（click 与 click_near 是两次不同提交）")
	}
}

// 4) 推导出来的写步同样被强制 retries=0：LLM/编排给 retry_count=3 也只准下一帧
func TestWSE2E_DerivedWriteStepForcesZeroRetries(t *testing.T) {
	// "timeout" 命中平台 ErrRetry 分类——若 retries 没被钳成 0，这一步会退避重试到 4 帧
	exec, ext, bundle := newWSE2E(t, func(action string, frame map[string]any) (map[string]any, string) {
		if action == "type" {
			return nil, "type_timeout_waiting_ack"
		}
		return happyReply(action, frame)
	})
	const stepsJSON = `[{"action":"open_tab","target":"https://www.xiaohongshu.com/explore"},
{"action":"type","target":".content-textarea","value":"回车即提交的评论","submit_on_enter":true,"retry_count":3,"retry_backoff_ms":100}]`
	task, session := bundle.seedTask(t, stepsJSON, false)
	steps, err := ParseSteps([]byte(stepsJSON))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	if n := ext.countOf("type"); n != 1 {
		t.Errorf("type 到线 %d 次 want 1——写步 retries 必须服务端钳 0（retry_count=3 是编排/LLM 给的）", n)
	}
	row := readStepState(t, bundle, session.ID, 1)
	if !row.IsWrite {
		t.Error("is_write 必须落库（推导结果不入库就等于没有推导）")
	}
	if row.SubmitState != model.StepSubmitUnattributed {
		t.Errorf("WS 超时=结果未知，必须记 unattributed，got %s", row.SubmitState)
	}
	if row.TextHash != HashWriteText("回车即提交的评论") {
		t.Errorf("text_hash=%q 未落到本步正文", row.TextHash)
	}
}

// 5) 元素从未命中＝这一步没发生＝台账留空（记成尝试会把一次干净的失败变成永久拦阻）
func TestWSE2E_WriteStepNeverExecutedLeavesNoLedger(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, func(action string, frame map[string]any) (map[string]any, string) {
		if action == "type" {
			return nil, "element_not_found: .content-textarea"
		}
		return happyReply(action, frame)
	})
	const stepsJSON = `[{"action":"open_tab","target":"https://www.xiaohongshu.com/explore"},
{"action":"type","target":".content-textarea","value":"从未落地的评论","submit_on_enter":true}]`
	task, session := bundle.seedTask(t, stepsJSON, false)
	steps, _ := ParseSteps([]byte(stepsJSON))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	if n := ext.countOf("type"); n != 1 {
		t.Fatalf("type=%d want 1", n)
	}
	row := readStepState(t, bundle, session.ID, 1)
	if row.SubmitState != "" {
		t.Errorf("注入从未执行却记了 %s——下一次合法重发会被自己的台账拦死", row.SubmitState)
	}
	if _, err := bundle.stepRepo.FindSubmitAttempt(ctx, task.ID, row.TextHash, 0); !isNotFound(err) {
		t.Errorf("不该算提交尝试，got err=%v", err)
	}
}

// 6) 成功的非 post_comment 写步记 sent，且绝不记 verified（没有回查通路就不准宣称已发布）
func TestWSE2E_DerivedWriteStepEndsSent(t *testing.T) {
	exec, _, bundle := newWSE2E(t, happyReply)
	const stepsJSON = `[{"action":"open_tab","target":"https://www.xiaohongshu.com/explore"},
{"action":"type","target":".content-textarea","value":"一次成功的回车提交","submit_on_enter":true}]`
	task, session := bundle.seedTask(t, stepsJSON, false)
	steps, _ := ParseSteps([]byte(stepsJSON))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	row := readStepState(t, bundle, session.ID, 1)
	if row.SubmitState != model.StepSubmitSent {
		t.Errorf("submit_state=%s want sent（type+回车没有回查通路，不许出现 verified）", row.SubmitState)
	}
	if row.Status != "success" {
		t.Errorf("status=%s want success", row.Status)
	}
	// sent 在拦阻集合内：这条腿的下一次同文本下发必须被查出
	if _, err := bundle.stepRepo.FindSubmitAttempt(ctx, task.ID, row.TextHash, 0); err != nil {
		t.Errorf("sent 必须算提交尝试，got err=%v", err)
	}
}

// 7) 重试豁免本体：自动重试轮里已尝试过的写步整步跳过（零帧），任务把剩下的只读步跑完
func TestWSE2E_RetryRoundSkipsAttemptedWriteStep(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, happyReply)
	task, first := bundle.seedTask(t, threeStageSteps, false)
	steps, err := ParseSteps([]byte(threeStageSteps))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	exec.ExecuteSession(ctx, task, first, steps)
	if n := ext.countOf("comment_prep"); n != 1 {
		t.Fatalf("首轮 comment_prep=%d want 1（前置条件）", n)
	}

	// 自动重试轮：TaskService 在重试下发前会把 task.RetryCount 递增（feedback.scheduleRetry 路径）
	task.RetryCount = 1
	second := &model.BrowserSession{TaskID: task.ID, UserID: wsE2EUserID, Status: "created", Url: task.Url}
	if err := bundle.sessionRepo.Create(ctx, second); err != nil {
		t.Fatal(err)
	}
	exec.ExecuteSession(ctx, task, second, steps)

	if n := ext.countOf("comment_prep"); n != 1 {
		t.Errorf("重试轮 comment_prep 到线 %d 次 want 1（跳过=整步不下发）", n)
	}
	row := readStepState(t, bundle, second.ID, 3)
	if row.Status != "skipped" {
		t.Errorf("重试轮该写步应记 skipped，got %s（%s）", row.Status, row.ErrorMsg)
	}
	if !strings.Contains(row.ErrorMsg, "跳过") {
		t.Errorf("跳过原因必须写进 error_msg 供人判，got %q", row.ErrorMsg)
	}
	// 立项理由：豁免的意义是让重试补完剩余环节。若这里还是 failed，重试永远出不了循环。
	if got := bundle.reloadSession(t, second.ID); got.Status != "completed" {
		t.Errorf("重试轮终态=%s want completed（只读步应照常跑完）：%s", got.Status, got.ErrorMsg)
	}
	// 跳过不许计成失败：metrics 里 failed 必须为 0
	if got := bundle.reloadSession(t, second.ID); got.FailedSteps != 0 {
		t.Errorf("failed_steps=%d want 0（skipped 不算失败）", got.FailedSteps)
	}
	// 首轮台账不许被第二轮改写
	if got := readStepState(t, bundle, first.ID, 3); got.SubmitState != model.StepSubmitVerified {
		t.Errorf("首轮台账被改写：%s", got.SubmitState)
	}
}

// 8) 人工重跑（RetryCount=0）仍然硬拦：豁免只给自动重试，人要看到「被拦下」这个事实
func TestWSE2E_ManualRerunStillFailsLoudly(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, happyReply)
	task, first := bundle.seedTask(t, threeStageSteps, false)
	steps, err := ParseSteps([]byte(threeStageSteps))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	exec.ExecuteSession(ctx, task, first, steps)

	second := &model.BrowserSession{TaskID: task.ID, UserID: wsE2EUserID, Status: "created", Url: task.Url}
	if err := bundle.sessionRepo.Create(ctx, second); err != nil {
		t.Fatal(err)
	}
	exec.ExecuteSession(ctx, task, second, steps) // task.RetryCount 保持 0

	row := readStepState(t, bundle, second.ID, 3)
	if row.Status == "skipped" {
		t.Error("人工重跑不得静默跳过——被闸门拦下是必须呈现的事实")
	}
	if !strings.Contains(row.ErrorMsg, "拒绝执行") {
		t.Errorf("人工重跑应拒绝执行，got status=%s err=%q", row.Status, row.ErrorMsg)
	}
	if n := ext.countOf("comment_prep"); n != 1 {
		t.Errorf("comment_prep=%d want 1", n)
	}
}

//  9. F-N4 本体（Brain 模式换下标重投）：同一条评论落在不同 step_index 上仍必须被拦。
//     旧键带 step_index，这一条会漏闸——批7 立项理由之一。
func TestWSE2E_IndexDriftStillBlocked(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, happyReply)
	const firstSteps = `[{"action":"open_tab","target":"https://www.xiaohongshu.com/explore"},
{"action":"post_comment","value":"换下标也拦得住的评论"}]`
	task, first := bundle.seedTask(t, firstSteps, false)
	steps, err := ParseSteps([]byte(firstSteps))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	exec.ExecuteSession(ctx, task, first, steps)
	if n := ext.countOf("comment_send"); n != 1 {
		t.Fatalf("首轮 comment_send=%d want 1", n)
	}

	// 第二轮：同一条评论，但前面多插两步 → post_comment 从 index=1 漂到 index=3
	// （Brain 模式每轮 stepIdx++ 的真实形状）
	const secondSteps = `[{"action":"open_tab","target":"https://www.xiaohongshu.com/explore"},
{"action":"snapshot"},
{"action":"markdown"},
{"action":"post_comment","value":"换下标也拦得住的评论"}]`
	steps2, err := ParseSteps([]byte(secondSteps))
	if err != nil {
		t.Fatal(err)
	}
	second := &model.BrowserSession{TaskID: task.ID, UserID: wsE2EUserID, Status: "created", Url: task.Url}
	if err := bundle.sessionRepo.Create(ctx, second); err != nil {
		t.Fatal(err)
	}
	exec.ExecuteSession(ctx, task, second, steps2)

	if n := ext.countOf("comment_send"); n != 1 {
		t.Errorf("comment_send 到线 %d 次 want 1——同文本换下标重投就是 Brain 模式的漏闸口", n)
	}
	row := readStepState(t, bundle, second.ID, 3)
	if !strings.Contains(row.ErrorMsg, "拒绝执行") {
		t.Errorf("漂移后的写步应被拦下，got status=%s err=%q", row.Status, row.ErrorMsg)
	}
}

// 10) 步行必须自带 is_write 落库列（审计与豁免都读它；不入库=推导结果事后不可考）
func TestWSE2E_ReadStepPersistsIsWriteFlag(t *testing.T) {
	exec, _, bundle := newWSE2E(t, happyReply)
	task, session := bundle.seedTask(t, threeStageSteps, false)
	steps, err := ParseSteps([]byte(threeStageSteps))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	// steps() 是从库里重新读出来的行——这里的 IsWrite 只能是落库值，不是内存值
	list := bundle.steps(t, session.ID)
	if len(list) != 4 {
		t.Fatalf("回读步数=%d want 4", len(list))
	}
	for _, s := range list {
		want := s.Action == "post_comment"
		if s.IsWrite != want {
			t.Errorf("step=%d action=%s is_write=%v want %v", s.StepIndex, s.Action, s.IsWrite, want)
		}
	}
}

// 11) 豁免的边界：前一轮只到 unattributed（提交从未被证明）时，跳过不许换来一个绿的会话。
//
// 与 7) 成对：7) 里前一轮 verified，跳过是「目标已达成、本轮只补剩余步」，判绿诚实；
// 这里前一轮 unattributed，本轮什么都没证明——若也判绿，任务快照就会写着「第 2 轮成功」，
// 而评论到底发没发没人知道。那正是批6 Leg X 立项要消灭的那类假绿，所以必须判红。
func TestWSE2E_RetryRoundSkipOfUnverifiedWriteStillFails(t *testing.T) {
	verifyMiss := func(action string, frame map[string]any) (map[string]any, string) {
		if action == "comment_verify" {
			return map[string]any{"verified": false, "posted": false, "reason": "comment_not_rendered"}, ""
		}
		return happyReply(action, frame)
	}
	exec, ext, bundle := newWSE2E(t, verifyMiss)
	task, first := bundle.seedTask(t, threeStageSteps, false)
	steps, err := ParseSteps([]byte(threeStageSteps))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	exec.ExecuteSession(ctx, task, first, steps)
	if got := readStepState(t, bundle, first.ID, 3); got.SubmitState != model.StepSubmitUnattributed {
		t.Fatalf("首轮台账=%s want unattributed（前置条件不成立，本测试无意义）", got.SubmitState)
	}
	prepFirst := ext.countOf("comment_prep")

	task.RetryCount = 1
	second := &model.BrowserSession{TaskID: task.ID, UserID: wsE2EUserID, Status: "created", Url: task.Url}
	if err := bundle.sessionRepo.Create(ctx, second); err != nil {
		t.Fatal(err)
	}
	exec.ExecuteSession(ctx, task, second, steps)

	if n := ext.countOf("comment_prep"); n != prepFirst {
		t.Errorf("重试轮 comment_prep 到线 %d，首轮 %d want 不变（跳过=整步零帧）", n, prepFirst)
	}
	// 判红不是因为整轮没跑：open_tab 到线必须增长，证明只读前缀照常重放完了才收口
	if n := ext.countOf("open_tab"); n != 2 {
		t.Errorf("open_tab 到线 %d want 2（重试轮应照常重放只读步）", n)
	}
	if row := readStepState(t, bundle, second.ID, 3); row.Status != "skipped" {
		t.Errorf("重试轮该写步 status=%s want skipped（步行为仍如实记「未下发」）：%s", row.Status, row.ErrorMsg)
	}
	got := bundle.reloadSession(t, second.ID)
	if got.Status != "failed" {
		t.Errorf("重试轮终态=%s want failed（未验证的提交不能靠跳过换绿）：%s", got.Status, got.ErrorMsg)
	}
	if !strings.Contains(got.ErrorMsg, "无法证明") {
		t.Errorf("会话文案须写明判红原因，got %q", got.ErrorMsg)
	}
	if got.FailedSteps != 1 {
		t.Errorf("failed_steps=%d want 1（未证明的跳过计入失败）", got.FailedSteps)
	}
	// 首轮台账必须原样留着：重试轮不得把 unattributed 就地「洗」成别的态
	if again := readStepState(t, bundle, first.ID, 3); again.SubmitState != model.StepSubmitUnattributed {
		t.Errorf("首轮台账被改成 %s", again.SubmitState)
	}
}
