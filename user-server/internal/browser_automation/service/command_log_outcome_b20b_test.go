package service

// 契约锁（A2）：审计流里「下发了」「Host 接了」「平台确认了」必须永不共列。
//
// 现状的两处混装（都在 browser_command_log 那一列 `ok` 上）：
//  1. **命令帧的 ok 是常量 true**（executor.go 写 command 行的那一行传的就是字面量 true）。
//     于是审计包/监控面板里每个"下发"都自带一个 ✓，而 ✓ 在这行上没有任何含义。
//     之后这个 ✓ 更是**可证为假**的：GetConn 未命中时命令帧根本没写进 socket
//     （ErrCommandNeverOnWire），可日志上那条 command 行照样是绿的。
//  2. **第三种事实压根没进流**：post_comment 的收紧回查结论（verified / unattributed）
//     只落在 browser_steps.submit_state 与 extracted_data 两处**可变行**上；
//     append-only 的命令流里，写步的 event 帧 ok=true 说的是「Host 回执无错」=accepted，
//     读包的人却会当成「评论已发布」。审计面要的恰恰是后者。
//
// 同行口径（§8.3 第 2 行）：GitHub Checks 用 status×conclusion、Stripe 用 processing≠succeeded，
// 都是"跑"与"成"两轴；OTel 更直接——无结论时那个字段 MUST 留 Unset，而不是填一个看着像结论的值。
// 所以本批的修法不是给 ok 换名字，而是：让它可以为空（此帧无结论），并补一帧真有结论的 judge。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/browser_automation/repository"
	"hivemtk-user/internal/pkg/testutil"
)

// b20bConfirmFrame 手写常量：故意不引用生产侧的同名常量——两边一起漂移时这里要单独红一次
// （口径同 dto/retry_range_parity_b20e_test.go 里那份手抄动作表）。
const b20bConfirmFrame = "write_confirm"

func b20bRows(t *testing.T, b *wsE2EDeps, sessionID uint) []*model.BrowserCommandLog {
	t.Helper()
	logs, err := b.cmdLogRepo.ListBySessionID(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("命令日志回读失败: %v", err)
	}
	return logs
}

func b20bPayload(t *testing.T, row *model.BrowserCommandLog) map[string]any {
	t.Helper()
	var p map[string]any
	if err := json.Unmarshal(row.Payload, &p); err != nil {
		t.Fatalf("%s 帧 payload 解析失败: %v（原文 %s）", row.Action, err, row.Payload)
	}
	return p
}

// 三条腿共用的驱动器：跑完整三段式（读 + 写）后回读审计流。
func b20bRun(t *testing.T, reply func(string, map[string]any) (map[string]any, string), stepsJSON string) ([]*model.BrowserCommandLog, uint) {
	t.Helper()
	exec, _, bundle := newWSE2E(t, reply)
	task, session := bundle.seedTask(t, stepsJSON, false)
	steps, err := ParseSteps([]byte(stepsJSON))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)
	return b20bRows(t, bundle, session.ID), session.ID
}

// 一、命令帧不得携带结论：ok 必须是空，而不是"看着像成功"。
func TestB20BCommandFramesCarryNoVerdict(t *testing.T) {
	logs, _ := b20bRun(t, happyReply, threeStageSteps)
	var cmd, event int
	for _, l := range logs {
		switch l.Direction {
		case "command":
			cmd++
			if l.Ok != nil {
				t.Errorf("seq=%d 的 command 帧带了结论 ok=%v：下发这一刻什么都不知道，✓ 是凭空来的",
					l.Seq, *l.Ok)
			}
		case "event":
			event++
			// event 帧是回包，理应有结论；这里只验"非空"，真/假由各帧自己的腿判
			if l.Ok == nil {
				t.Errorf("seq=%d 的 event 帧 ok 为空：回包已经到手，不得声称无结论", l.Seq)
			}
		}
	}
	if cmd == 0 || event == 0 {
		t.Fatalf("审计流形状不对：command=%d event=%d，两轴都得有样本才谈得上分列", cmd, event)
	}
}

// 二、写步必须留下一帧「平台确认了没有」。
func TestB20BWriteStepEmitsConfirmJudgeFrame(t *testing.T) {
	body := "测试评论正文"
	logs, _ := b20bRun(t, happyReply, threeStageSteps)
	var frames []*model.BrowserCommandLog
	var sendSeq int
	for _, l := range logs {
		if l.Action == b20bConfirmFrame {
			frames = append(frames, l)
		}
		if l.Direction == "command" && l.Action == "comment_send" {
			sendSeq = l.Seq
		}
	}
	if len(frames) != 1 {
		t.Fatalf("%s 帧数=%d want 1（写步一次提交一帧结论；多帧=重放把结论也重放）", b20bConfirmFrame, len(frames))
	}
	f := frames[0]
	if f.Direction != "judge" {
		t.Errorf("确认帧 direction=%s want judge（它不是 Host 的回包，是服务端裁的结论）", f.Direction)
	}
	if f.Ok == nil || !*f.Ok {
		t.Errorf("回查命中时确认帧 ok 必须为 true，got %v", f.Ok)
	}
	if sendSeq == 0 || f.Seq <= sendSeq {
		t.Errorf("确认帧 seq=%d 必须晚于提交命令 seq=%d（先提交后确认，反了就是编出来的一帧）", f.Seq, sendSeq)
	}
	p := b20bPayload(t, f)
	if p["payload_hash"] != HashWriteText(body) {
		t.Errorf("确认帧未绑定被确认的那份载荷：%+v", p)
	}
	if p["state"] != model.StepSubmitVerified {
		t.Errorf("确认帧 state=%v want %s", p["state"], model.StepSubmitVerified)
	}
	// 正文零泄漏（I5 导出把整列审计帧带走，写台账 A9 同口径）
	blob, err := json.Marshal(logs)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), body) {
		t.Error("审计帧携带了评论正文：确认帧只能带哈希与结论")
	}
}

// 三、回查未见 ⇒ 同一帧改口，且不得与"已确认"同形。
// unattributed 是这批里最有用的一格：旧审计流里这一场和上一场长得一模一样
// （写步的 event 帧照样是 ok=true 的"成功"，因为 Host 确实回执了），差别只在可变行里。
func TestB20BUnverifiedWriteConfirmFrameSaysSo(t *testing.T) {
	reply := func(action string, frame map[string]any) (map[string]any, string) {
		if action == "comment_verify" {
			return map[string]any{"verified": false, "matched": 0}, ""
		}
		return happyReply(action, frame)
	}
	logs, _ := b20bRun(t, reply, threeStageSteps)
	var frames []*model.BrowserCommandLog
	for _, l := range logs {
		if l.Action == b20bConfirmFrame {
			frames = append(frames, l)
		}
	}
	if len(frames) != 1 {
		t.Fatalf("%s 帧数=%d want 1", b20bConfirmFrame, len(frames))
	}
	f := frames[0]
	if f.Ok == nil || *f.Ok {
		t.Errorf("回查未命中时确认帧 ok 不得为真/不得为空，got %v", f.Ok)
	}
	if p := b20bPayload(t, f); p["state"] != model.StepSubmitUnattributed {
		t.Errorf("state=%v want %s（「试过但归因不到」与「没成功」都不许与已确认同形）",
			p["state"], model.StepSubmitUnattributed)
	}
}

// 四、反向锁：读步不得冒出确认帧。
// 每个 markdown/snapshot 都补一帧"未确认"会把审计流刷成噪音，
// 而"确认"这个词一旦对读步有意义，读包的人就分不清哪一帧是写操作的结论了。
func TestB20BReadOnlyStepsEmitNoConfirmFrame(t *testing.T) {
	const readSteps = `[{"action":"open_tab","target":"https://www.xiaohongshu.com/explore"},
{"action":"markdown"}]`
	logs, _ := b20bRun(t, happyReply, readSteps)
	for _, l := range logs {
		if l.Action == b20bConfirmFrame {
			t.Errorf("纯读任务出现了 %s 帧（direction=%s ok=%v）：确认态只对写步存在", b20bConfirmFrame, l.Direction, l.Ok)
		}
	}
	if len(logs) == 0 {
		t.Fatal("纯读任务一条审计帧都没有：那不是「没噪音」，那是埋点整块没跑")
	}
}

// 五、三态得能真的过库：nil 写进去必须是 NULL，读回来还是 nil，而不是被折成 false。
// 这条最容易死在"列还是 NOT NULL"上——gorm 建表按模型标签走，模型改成 *bool 但库里
// 存量列没跟着 ALTER 的话，新代码第一次写 nil 就报错；所以本腿之外还要一条迁移测试
// （v3_44_0 那份：把 ok 摘成 NOT NULL 的旧形状再 Up 回来）。
func TestB20BOkTriStateSurvivesRoundTrip(t *testing.T) {
	db := testutil.NewTestDB(t, &model.BrowserCommandLog{})
	if db == nil {
		t.Fatal("测试库不可达：三态过库这条腿跳过等于没锁")
	}
	repo := repository.NewBrowserCommandLogRepositoryWithDB(db)
	yes, no := true, false
	for i, tc := range []*bool{nil, &yes, &no} {
		row := &model.BrowserCommandLog{
			SessionID: 4242, TaskID: 1, Seq: i + 1,
			Direction: "command", Action: "click", Ok: tc,
		}
		if err := repo.Append(context.Background(), row); err != nil {
			t.Fatalf("第 %d 帧落库失败（ok=%v）：%v", i+1, tc, err)
		}
	}
	got, err := repo.ListBySessionID(context.Background(), 4242)
	if err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("回读 %d 行 want 3", len(got))
	}
	want := []*bool{nil, &yes, &no}
	for i, g := range got {
		switch {
		case want[i] == nil && g.Ok != nil:
			t.Errorf("第 %d 行：无结论被库折成了 %v（NULL 保不住，三态就退化成两态）", i+1, *g.Ok)
		case want[i] != nil && g.Ok == nil:
			t.Errorf("第 %d 行：结论 %v 丢了，读回 nil", i+1, *want[i])
		case want[i] != nil && g.Ok != nil && *want[i] != *g.Ok:
			t.Errorf("第 %d 行：ok 往返变了 %v → %v", i+1, *want[i], *g.Ok)
		}
	}
}

// 六、失败出路的回包帧必须说「败」：ok=false，既不是 nil 更不是 true。
//
// 第一条腿对 event 帧只断「非空」（它自己的注释写着「真/假由各帧自己的腿判」），
// 而"各帧自己的腿"在这一格里从来没立过：三条 b20b 腿都走 happyReply，
// b21/b20c 腿只判错误归类——`err != nil` 那条出路写下的 ok 全仓没有任何一条腿读过。
// 于是 executor.go 里那两处（comment_send 回包帧的 `verdict(err == nil)`、
// 步失败回包帧的 `verdict(false)`）换成常量 true 也不会红一次，
// 而 accepted 轴重新退化成常量正是 A2 立项要消灭的形状（C 相电池 M7/M8 实跑即如此）。
//
// 本腿走**真 dispatchStep 的失败出路**：扩展侧对 comment_send 回
// `send_button_not_interactable`（闸门在把坐标交给 CDP 之前就把按钮判死 ⇒ 零副作用、
// 不进 finalize 白轮、不自愈重发 ⇒ 帧数确定），于是那一路上落两帧：
//   - comment_send 的 event 帧：下发有回音、回音是败；
//   - 步级 post_comment 的 event 帧：这一步判败。
//
// 两帧都断 ok 非空且为 false，并先断 payload 里的 error 文本非空——后者是「这一帧确实是
// 一帧失败回包」的前提：不钉它，上面那句就成了一次断在成功回包上的假绿。
// 不往仓储里手工塞行再造期望：两帧都由 ExecuteSession 真跑落库。
func TestB20BFailedDispatchEventFramesSayFailed(t *testing.T) {
	// 帧名手写常量（口径同 b20bConfirmFrame：两边一起漂移时这里要单独红一次）。
	const (
		sendFrame  = "comment_send"
		writeFrame = "post_comment"
	)
	exec, ext, bundle := newWSE2E(t, func(action string, frame map[string]any) (map[string]any, string) {
		if action == sendFrame {
			return nil, "send_button_not_interactable: covered"
		}
		return happyReply(action, frame)
	})
	task, session := bundle.seedTask(t, threeStageSteps, false)
	steps, err := ParseSteps([]byte(threeStageSteps))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	// 前提：那次提交确实到过线（一帧），否则下面的「失败回包」是凭空等的。
	if n := ext.countOf(sendFrame); n != 1 {
		t.Fatalf("%s 到线 %d 次 want 1：命令根本没出去，本腿断的不是「回包说败」", sendFrame, n)
	}

	var sendFrames, stepFrames []*model.BrowserCommandLog
	for _, l := range b20bRows(t, bundle, session.ID) {
		if l.Direction != "event" {
			continue
		}
		switch l.Action {
		case sendFrame:
			sendFrames = append(sendFrames, l)
		case writeFrame:
			stepFrames = append(stepFrames, l)
		}
	}
	b20bMustBeFailedFrame(t, sendFrame, sendFrames)
	b20bMustBeFailedFrame(t, writeFrame, stepFrames)
}

// b20bMustBeFailedFrame 断「这一组里恰好一帧、它是失败回包、且 ok 明确为 false」。
// 帧数不对时停机（Fatalf）：那说明本腿断的对象不存在，绿与红都没有意义。
func b20bMustBeFailedFrame(t *testing.T, action string, list []*model.BrowserCommandLog) {
	t.Helper()
	if len(list) != 1 {
		t.Fatalf("%s 的 event 帧数=%d want 1：%+v —— 失败出路没落这一帧（或落了多次），本腿无对象",
			action, len(list), list)
	}
	f := list[0]
	p := b20bPayload(t, f)
	if e, _ := p["error"].(string); e == "" {
		t.Fatalf("%s 的 event 帧 payload 里没有 error 文本（%+v）：那不是一帧失败回包，"+
			"下面断 ok 会断错对象", action, p)
	}
	if f.Ok == nil {
		t.Errorf("%s 的 event 帧 ok 为空：回音已经到手（而且是个败），不得声称无结论", action)
		return
	}
	if *f.Ok {
		t.Errorf("%s 的 event 帧 ok=true：这一帧的 payload 写着 error=%q，"+
			"结论轴被写成了常量 ✓（A2 拆开的「下发了」与「成了」又并回一列）", action, p["error"])
	}
}
