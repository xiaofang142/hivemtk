package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/browser_automation/dto"
	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/browser_automation/repository"
)

// ledger_b16c_test.go — 批16c：二次审核线（对抗审查子报告）点出的「闸门只对 effectWrite 有腿、
// 对 effectUnknown 没腿」这类覆盖缺口，逐条复验后补的八条腿。
//
// 复验口径（本批每一条都跑过，不采信报告原文）：
//   - 报告说「把 writeStep 改成 effect==effectWrite 后全套测试全绿」⇒ 复跑发现**钳 retries 那一格
//     确实有腿**（TestWSE2E_UnknownLocatorTableTreatedAsWrite 断 click 只到线 1 次），
//     但双发闸 / 降级拒绝 / D7 三格在 unknown 步上确实没有一条断言走过 ⇒ 三条腿补上；
//   - 「删掉 defer clearLedgerBroken 全绿」⇒ 成立：上一批⑥那条腿刻意不读降级表
//     （读了恒为空、永远绿），所以「清除」这个动作本身没有腿；
//   - 「isNeverExecuted 的 _inject_timeout_ 分支没腿」⇒ 成立（b7 只打了 _not_found）；
//   - 「FindSubmitAttempt 的 Order(id asc) 改成 desc 全绿」⇒ 成立：顺序是**确定性**承诺，
//     没有腿就是「同一份库两次判定可以给出两种闸门结论」；
//   - 「writeStepKey 丢掉 Anchor/ButtonText 全绿」⇒ 成立：现有腿只断「非空」，丢字段仍非空；
//   - gap-cap 的**淘汰方向**上一批已有腿（TestLedgerGapSetGrowsOnlyPerDistinctKey 断最新键必在
//     集合内），只有 512 那个字面量没腿——有界性才是安全论证，具体 N 不是，理由见 §7.10。

const orphanPlatform = "not_registered_platform_zz"

// 本文件各腿的执行 ctx 一律用 e2eExecBudget（推导见 executor_ws_e2e_test.go），不写死 60s：
// 60s 短于「数条命令 + 一次 D7 挂起 + 一次拦截探测」的合法预算之和，抢库时 load 80+ 就是负载红，
// 而负载红会被反向电池记成「变异已杀」——那是电池最坏的一种假绿。

func startSessionFor(t *testing.T, bundle *wsE2EDeps, task *model.BrowserTask) *model.BrowserSession {
	t.Helper()
	session := &model.BrowserSession{TaskID: task.ID, UserID: wsE2EUserID, Status: "created", Url: task.Url}
	if err := bundle.sessionRepo.Create(context.Background(), session); err != nil {
		t.Fatalf("第二个会话落库失败: %v", err)
	}
	return session
}

// ①（审核线 B1）定位表不可得的步必须过**双发闸**，不只是钳 retries。
//
// 批16 只证了「unknown 步不重试」，而闸门是四个独立 if：把条件收窄回 effect==effectWrite，
// unknown 步照样钳 0 重试、照样落 is_write，全套测试仍绿——于是重跑同一任务就是把一条
// 「不知道有没有副作用」的动作再发一次。这里用同一任务的第二个 session 复现重跑。
func TestUnknownEffectStepHoldsDoubleSendGate(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, happyReply)
	const stepsJSON = `[{"action":"open_tab","target":"https://example.com/orphan"},
{"action":"click","target":"button.submit-now"}]`
	task, session := bundle.seedTask(t, stepsJSON, false)
	task.Platform = orphanPlatform
	steps, err := ParseSteps([]byte(stepsJSON))
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)
	if n := ext.countOf("click"); n != 1 {
		t.Fatalf("第一轮 click 到线 %d want 1", n)
	}
	first := readStepState(t, bundle, session.ID, 1)
	if !first.IsWrite || first.SubmitState != model.StepSubmitSent {
		t.Fatalf("前置条件不成立：unknown 步没留下提交尝试（is_write=%v submit_state=%q）", first.IsWrite, first.SubmitState)
	}

	second := startSessionFor(t, bundle, task)
	exec.ExecuteSession(ctx, task, second, steps)
	if n := ext.countOf("click"); n != 1 {
		t.Errorf("同一任务的第二轮把 unknown 写步又下发了一次（累计 click=%d want 1）——双发闸只对 "+
			"effectWrite 生效就等于闸门没有：定位表取不到时恰恰不知道这一次有没有副作用", n)
	}
	row := readStepState(t, bundle, second.ID, 1)
	// 不是重试轮（没有 errRetrySkipped 那条豁免路径），闸门直接判死这一步——两种判法都算拦住，
	// 这条腿断的是「一帧都没再下发」+「红因写明了是哪道闸」。
	if row.Status != "failed" || !strings.Contains(row.ErrorMsg, "双发") {
		t.Errorf("第二轮该步应判「失败+双发原因」，got status=%q err=%q", row.Status, row.ErrorMsg)
	}
}

// ②（审核线 B1）本会话台账已经写失败 ⇒ unknown 步也一帧都不许再下发。
func TestUnknownEffectStepRefusedAfterLedgerDegrade(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, happyReply)
	// 三条 unknown 写步，正文全为空 ⇒ 闸门键取自定位面。
	// continue_on_error 是这条腿的前提：没有它整轮在第一步就终止，降级永远撞不上第二次派发。
	const stepsJSON = `[{"action":"open_tab","target":"https://example.com/orphan"},
{"action":"click","target":"button.one","continue_on_error":true},
{"action":"click_near","anchor":".card","button_text":"发送","continue_on_error":true},
{"action":"click","target":"button.two"}]`
	task, session := bundle.seedTask(t, stepsJSON, false)
	task.Platform = orphanPlatform
	exec.stepRepo = &flakyStepRepo{BrowserStepRepository: bundle.stepRepo,
		failUpdate: func(_, _ string) bool { return true }}
	steps, err := ParseSteps([]byte(stepsJSON))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	if n := ext.countOf("click"); n != 1 {
		t.Errorf("click 到线 %d want 1（只许第一条下发，其后会话已降级）", n)
	}
	if n := ext.countOf("click_near"); n != 0 {
		t.Errorf("click_near 到线 %d want 0——降级判定只长在 effectWrite 上时 unknown 步照发，"+
			"而它这次的台账同样写不进去，下一轮无从得知它发生过", n)
	}
	if row := readStepState(t, bundle, session.ID, 1); !strings.Contains(row.ErrorMsg, "台账未落") {
		t.Errorf("step[1] 红因须点名台账未落，got %q", row.ErrorMsg)
	}
	if row := readStepState(t, bundle, session.ID, 2); row.Status != "failed" ||
		!strings.Contains(row.ErrorMsg, "拒绝下发") {
		t.Errorf("step[2] 应被降级拒绝（status=%q err=%q）", row.Status, row.ErrorMsg)
	}
}

// ③（审核线 B1）D7 人工确认闸门也必须长在 unknown 步上。
//
// 这条最坏：开了 require_confirm 的用户，unknown 步若绕过闸门，就是「在用户明确要求先看一眼的
// 平台上，未经确认把一个不知道有没有副作用的动作发出去」。
func TestUnknownEffectStepHoldsConfirmGate(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, happyReply)
	const stepsJSON = `[{"action":"open_tab","target":"https://example.com/orphan"},
{"action":"click","target":"button.submit-now"}]`
	task, session := bundle.seedTask(t, stepsJSON, true)
	task.Platform = orphanPlatform
	steps, err := ParseSteps([]byte(stepsJSON))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
		defer cancel()
		exec.ExecuteSession(ctx, task, session, steps)
		close(done)
	}()

	waitConfirmPending(t, exec, session.ID, true)
	if n := ext.countOf("click"); n != 0 {
		t.Fatalf("挂起期间 click 已到线 %d 次——unknown 副作用步绕过了人工确认闸门", n)
	}
	if !exec.SignalConfirm(session.ID) {
		t.Fatal("放行未命中挂起点")
	}
	select {
	case <-done:
	case <-time.After(e2eExecBudget):
		t.Fatal("放行后会话未在时限内收敛")
	}
	if n := ext.countOf("click"); n != 1 {
		t.Errorf("放行后 click=%d want 1", n)
	}
}

// ④（审核线 B3）降级表必须随会话收口清空。
//
// 上一批⑥刻意不读这张表（收口时会清空，读了就是恒为空的假断言），所以「清除」这个动作本身
// 没有腿：删掉 defer 后表随会话数无界增长，且同一 session 行被再认领时会被上一轮失败永久钉死。
// 这条腿反过来——它必须在「会话已经收口」之后读表，才能证到清除。
func TestLedgerDegradeClearedWhenSessionEnds(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, happyReply)
	const stepsJSON = `[{"action":"open_tab","target":"https://www.xiaohongshu.com/explore"},
{"action":"post_comment","value":"会被台账拖住的正文"}]`
	task, session := bundle.seedTask(t, stepsJSON, false)
	exec.stepRepo = &flakyStepRepo{BrowserStepRepository: bundle.stepRepo,
		failUpdate: func(_, _ string) bool { return true }}
	steps, err := ParseSteps([]byte(stepsJSON))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	// 前置：本会话确实进过降级表（prepared 写失败拦在跨越之前，所以一帧 send 都不该到线）。
	if n := ext.countOf("comment_send"); n != 0 {
		t.Fatalf("前置条件不成立：comment_send 到线 %d want 0", n)
	}
	// 收口发生在 ExecuteSession 返回时（defer），所以此刻读表必须读到「已清」。
	if why, broken := exec.ledgerBrokenReason(session.ID); broken {
		t.Errorf("会话收口后仍在降级表里（reason=%s）——删掉 defer clearLedgerBroken 时全套测试照样绿："+
			"表随会话数无界增长，同 ID 行再认领还会被上一轮的失败钉死", why)
	}
	if n := len(exec.ledgerBroken); n != 0 {
		t.Errorf("收口后降级表长度=%d want 0", n)
	}
}

// ⑤（审核线 A6）*_inject_timeout_ 必须算「从未发生」：不记尝试、下一轮可安全重下发。
//
// 上一批只有 b7 那条腿打了 _not_found 分支。摘掉 inject_timeout 分支后，注入超时会被记成
// unattributed 尝试 ⇒ 下一轮同文本被闸门跳过：一次「页面根本没收到」的失败永久堵死这条内容。
func TestInjectTimeoutStepStaysReDispatchable(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, func(action string, frame map[string]any) (map[string]any, string) {
		if action == "click" {
			return nil, "click_inject_timeout_15000ms"
		}
		return happyReply(action, frame)
	})
	const stepsJSON = `[{"action":"open_tab","target":"https://www.xiaohongshu.com/explore"},
{"action":"click","target":"button.submit"}]`
	task, session := bundle.seedTask(t, stepsJSON, false)
	steps, err := ParseSteps([]byte(stepsJSON))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	first := readStepState(t, bundle, session.ID, 1)
	if first.SubmitState != "" {
		t.Errorf("注入超时步的 submit_state=%q want 空——页面从未收到这一击，记成提交尝试就是把可重下发堵死", first.SubmitState)
	}
	second := startSessionFor(t, bundle, task)
	exec.ExecuteSession(ctx, task, second, steps)
	if n := ext.countOf("click"); n != 2 {
		t.Errorf("累计 click=%d want 2（两轮各一次）——第一条判「从未发生」却仍被闸门拦下，就是分支被摘掉的样子", n)
	}
	if row := readStepState(t, bundle, second.ID, 1); row.Status == "skipped" {
		t.Errorf("第二轮该步被双发闸跳过了（err=%q）", row.ErrorMsg)
	}
}

// ⑥（审核线 A7）步重试必须按**指数**退避。
func TestRetryBackoffGrowsExponentially(t *testing.T) {
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 200 * time.Millisecond},
		{2, 400 * time.Millisecond},
		{3, 800 * time.Millisecond},
		{4, 1600 * time.Millisecond},
	}
	for _, c := range cases {
		if got := retryBackoffDelay(200, c.attempt); got != c.want {
			t.Errorf("attempt=%d 退避=%v want %v（退化成线性后，同一批失败的重试节奏翻倍变密，风控面看得见）",
				c.attempt, got, c.want)
		}
	}
	// 执行循环必须真的用这个函数：只测纯函数、循环里另写一份算式，就是又一条没有牙的腿。
	if src := readSrc(t, "executor.go"); !strings.Contains(src, "retryBackoffDelay(backoff, attempt)") {
		t.Error("executeStepWithRetry 未走 retryBackoffDelay（就地算式钉不住，退避形状会漂移）")
	}
}

// ⑦（审核线 A8）双发闸回查必须取**最早**那次提交尝试。
//
// 同一 (task,文本) 可以有多条尝试行（人工删过一行、降级期间重复落账）。顺序一旦不确定，
// 同一份库两次判定就能给出两种结论（一次跳过、一次放行）——闸门的事实来源必须只有一种读法。
func TestFindSubmitAttemptReturnsEarliestAttempt(t *testing.T) {
	_, _, bundle, _ := newLedgerGapFeedbackE2E(t)
	repo := repository.NewBrowserStepRepositoryWithDB(bundle.db)
	ctx := context.Background()
	const textHash = "b16c1234"
	taskID := uint(960101)

	rows := []*model.BrowserStep{
		{SessionID: 960102, TaskID: taskID, StepIndex: 1, Action: "post_comment",
			Status: "success", SubmitState: model.StepSubmitVerified, TextHash: textHash},
		{SessionID: 960103, TaskID: taskID, StepIndex: 1, Action: "post_comment",
			Status: "running", SubmitState: model.StepSubmitSent, TextHash: textHash},
	}
	for _, r := range rows {
		if err := repo.BatchCreate(ctx, []*model.BrowserStep{r}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := repo.FindSubmitAttempt(ctx, taskID, textHash, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != rows[0].ID {
		t.Errorf("回查取到了后落的那条（id=%d want %d）——取最新会把「早已 verified」的凭据盖掉，"+
			"闸门结论随插入顺序漂移", got.ID, rows[0].ID)
	}
	if got.SubmitState != model.StepSubmitVerified {
		t.Errorf("取到的那条 submit_state=%q want verified，否则跳过判据会退化成红", got.SubmitState)
	}
	// excludeID 指向最早那条 ⇒ 只剩较新的尝试，仍须查得到（闸门不因排除而漏判）
	again, err := repo.FindSubmitAttempt(ctx, taskID, textHash, rows[0].ID)
	if err != nil {
		t.Fatalf("排除最早一条后应仍查到较新尝试，got %v", err)
	}
	if again.ID != rows[1].ID {
		t.Errorf("excludeID 语义跑偏：got id=%d want %d", again.ID, rows[1].ID)
	}
}

// ⑧（审核线 B4）闸门键必须含 Anchor/ButtonText：无正文写步的同一性由定位面决定。
//
// 丢掉这两个字段后键仍然非空（现有腿只断非空），但「点赞按钮」和「发送按钮」会塌成同一个键：
// 一处点过之后，另一个发送位再想发就被当成双发跳过；同一文本在两个位点的两次提交也不再是两个事实。
func TestWriteStepKeyDistinguishesLocatorFacets(t *testing.T) {
	base := dto.StepItem{Action: "click_near", Target: "t", Anchor: ".a", ButtonText: "发送"}
	keyOf := func(mut func(*dto.StepItem)) string {
		it := base
		mut(&it)
		return writeStepKey(parsedStep{StepItem: it})
	}
	same := keyOf(func(*dto.StepItem) {})
	variants := map[string]string{
		"换锚点":   keyOf(func(s *dto.StepItem) { s.Anchor = ".b" }),
		"换按钮文案": keyOf(func(s *dto.StepItem) { s.ButtonText = "回复" }),
		"换动作":   keyOf(func(s *dto.StepItem) { s.Action = "click" }),
		"换目标":   keyOf(func(s *dto.StepItem) { s.Target = "t2" }),
	}
	for name, k := range variants {
		if k == same {
			t.Errorf("%s 后闸门键没变——两个不同的提交位点塌成一个键，双发判定就此失真", name)
		}
		if k == "" {
			t.Errorf("%s 后键为空串", name)
		}
	}
	// 空白归一：文案只差空格仍是同一个键（宁可多拦不可漏拦，与 HashWriteText 同口径）
	if keyOf(func(s *dto.StepItem) { s.ButtonText = "发 送" }) != same {
		t.Error("按钮文案的空格差异应归一为同一键（与 HashWriteText 口径一致）")
	}
}

// ⑨（审核线对批16 M7 等价类的保留意见）等价类的**前提**必须有锁。
//
// 批16 把 M7（sent 落账点不再标 crossed）判成等价类，理由是一条控制流事实：sent 写与终态写
// 之间没有任何早返，且两次写用同一个 textHash，所以「sent 失败」要么被终态写补成一条库里的
// 提交尝试（DB 闸门照拦），要么两次一起失败（同键去重后仍记一条兜底缺口）。
// 当时这个判断是对的，但**没有任何东西在那条早返被写进来的当天变红**——spec 只写了「有寿命、
// 插入早返必须重跑」，而没人会在改码那天去翻 spec。这条腿把前提本身钉住：
// 两个落账点之间一旦长出 return/break/continue，等价类立即失效，M7 必须重判。
func TestSentToFinalLedgerWritePathHasNoEarlyReturn(t *testing.T) {
	src := readSrc(t, "executor.go")
	i := strings.Index(src, "sentLedgerErr := e.recordSubmitState(")
	j := strings.Index(src, "finalLedgerErr := e.recordSubmitState(")
	if i < 0 || j < 0 || j < i {
		t.Fatalf("锚点没找到（两个落账点改名或换文件了？M7 的等价类论证要一起改）: i=%d j=%d", i, j)
	}
	mid := stripComments(src[i:j])
	for _, kw := range []string{"return", "break", "continue"} {
		if strings.Contains(mid, kw) {
			t.Errorf("sent 与终态两次台账写之间出现了 %s——M7 的等价类（摘掉 sent 侧 crossed 不改任何可观测行为）"+
				"就此失效：sent 失败再也不是「必然被终态写补上」，双发闸会漏掉整类越点未落账的提交", kw)
		}
	}
	// 前提的第二半：两次写同键。键一旦分叉，终态写就补不上 sent 那一格。
	for _, anchor := range []string{src[i:], src[j:]} {
		if !strings.HasSuffix(firstLine(anchor), "textHash, true)") {
			t.Error("两个落账点的实参尾巴不再是同一个 textHash——同键是「终态能补上 sent」的前提，M7 等价类失效")
		}
	}
}

// firstLine 取锚点那一整行（锁只认这一行的实参尾巴；换行写法要红，那是锁该管的）。
func firstLine(s string) string {
	if k := strings.IndexByte(s, '\n'); k >= 0 {
		return strings.TrimSpace(s[:k])
	}
	return strings.TrimSpace(s)
}

// stripComments 去掉行注释：锁要盯的是控制流，不是「注释里出现了 return 这个词」。
func stripComments(s string) string {
	var out strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if k := strings.Index(line, "//"); k >= 0 {
			line = line[:k]
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.String()
}
