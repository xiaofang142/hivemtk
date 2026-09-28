package service

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/browser_automation/repository"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// 批20（§8.3-5 A5 / §8.3-14 A9 / §8.3-15 A10）D7 审批闭环三件套的契约锁。
// 一句话立项理由：一条布尔放行既不说「批的是哪份载荷」、也不说「批过」这件事发生过，
// 更不说「闸门在别处」——三件事都靠人脑补，就等于没有。

// captureGateLogRepo 命令日志的进程内替身（只服务本批的帧形状断言，不碰库）。
type captureGateLogRepo struct {
	mu   sync.Mutex
	rows []*model.BrowserCommandLog
}

func (c *captureGateLogRepo) Append(_ context.Context, e *model.BrowserCommandLog) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rows = append(c.rows, e)
	return nil
}

func (c *captureGateLogRepo) ListBySessionID(_ context.Context, _ uint) ([]*model.BrowserCommandLog, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*model.BrowserCommandLog, len(c.rows))
	copy(out, c.rows)
	return out, nil
}

func (c *captureGateLogRepo) PruneBefore(context.Context, time.Time, string) (int64, error) {
	return 0, nil
}

func (c *captureGateLogRepo) byAction(action string) []*model.BrowserCommandLog {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []*model.BrowserCommandLog
	for _, r := range c.rows {
		if r.Action == action {
			out = append(out, r)
		}
	}
	return out
}

func gatePayload(t *testing.T, row *model.BrowserCommandLog) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(row.Payload, &m); err != nil {
		t.Fatalf("%s 帧 payload 不是 JSON 对象: %v（原文 %s）", row.Action, err, row.Payload)
	}
	return m
}

func waitForGate(t *testing.T, cond func() bool, why string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal(why)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// 1) A5 审批必须绑载荷：哈希不符即 fail-closed（闸门留着、不提交、也不「再问一次」），
// 哈希相符才放行，且一次性消费（重复放行=无挂起点，绝不 double close）。
func TestSignalConfirmBindsPayload(t *testing.T) {
	e := newConfirmExecutor()
	h := startWaitConfirm(t, e, context.Background(), 7, time.Minute)
	hash := h.gate.payloadHash

	if got := e.SignalConfirm(7, "a-different-payload-hash"); got != VerdictMismatch {
		t.Errorf("错哈希放行=%v，want VerdictMismatch（载荷不符必须拒且可辨）", got)
	}
	if !e.ConfirmPending(7) {
		t.Fatal("拒一次放行不得撤掉闸门——用户改正哈希后仍应能放行")
	}
	select {
	case v := <-h.done:
		t.Fatalf("错哈希竟放行了：outcome=%v（不可逆提交点先于人工批准=D7 立项要消灭的形状）", v.outcome)
	default:
	}

	if got := e.SignalConfirm(7, hash); got != VerdictGranted {
		t.Errorf("对哈希放行=%v，want VerdictGranted", got)
	}
	select {
	case v := <-h.done:
		if v.outcome != confirmGranted {
			t.Errorf("放行后出路=%v want confirmGranted", v.outcome)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("放行后等待方未收敛")
	}
	if e.ConfirmPending(7) {
		t.Error("放行后应注销挂起通道（一次性消费）")
	}
	if got := e.SignalConfirm(7, hash); got != VerdictNoGate {
		t.Errorf("已消费后再放行=%v，want VerdictNoGate", got)
	}
}

// 2) A10 挂起态必须可查证：读侧要能拿到「正在等谁批、批的是哪份载荷、什么时候到期」，
// 否则前端只能拿一个布尔值盲签——那正是「审批没绑载荷」的客户端形状。
func TestPendingGateExposesAwaitingPayload(t *testing.T) {
	e := newConfirmExecutor()
	gate := &confirmGate{
		sessionID: 11, stepIndex: 2, payloadHash: "cafe1234",
		preview: "这条评论会原样发出去", expiresAt: time.Now().Add(30 * time.Second),
		ch: make(chan struct{}),
	}
	e.registerConfirmGate(gate)

	got, ok := e.PendingGate(11)
	if !ok {
		t.Fatal("注册后读侧查不到挂起闸门")
	}
	if got.PayloadHash != "cafe1234" || got.StepIndex != 2 || got.SessionID != 11 {
		t.Errorf("闸门身份字段丢失: %+v", got)
	}
	if !strings.Contains(got.Preview, "原样发出去") {
		t.Errorf("预览缺失，用户无从知道自己在批什么: %+v", got)
	}
	if !got.ExpiresAt.After(time.Now()) {
		t.Errorf("到期时间必须是未来时刻（跨进程判据要用它）: %+v", got.ExpiresAt)
	}
	if _, ok := e.PendingGate(12); ok {
		t.Error("别的 session 不得读到这条闸门（越权隔离）")
	}
}

// 3) A9 + A10 审计帧：挂起即落 d7_wait、收口即落 judge，两帧都只带哈希不带正文
// （I5 导出会把正文带进离线件），且 judge 要能自证「拒过几次错载荷」。
func TestConfirmGateFramesBindPayloadAndOmitBody(t *testing.T) {
	e := newConfirmExecutor()
	repo := &captureGateLogRepo{}
	e.SetCommandLogRepository(repo)

	const body = "这条正文绝不该出现在任何审计帧里"
	task := &model.BrowserTask{ID: 4, UserID: 1, RequireConfirm: true}
	session := &model.BrowserSession{ID: 11, TaskID: 4, UserID: 1}
	hash := HashWriteText(body)
	seq := 0
	done := make(chan confirmOutcome, 1)
	go func() {
		out, _ := e.awaitConfirmGate(context.Background(), task, session, 77, 2, body, hash, &seq, time.Minute)
		done <- out
	}()
	waitForGate(t, func() bool { return e.ConfirmPending(11) }, "闸门未进入挂起态")
	waitForGate(t, func() bool { return len(repo.byAction("d7_wait")) == 1 }, "挂起时未落 d7_wait 帧")

	wait := repo.byAction("d7_wait")[0]
	wp := gatePayload(t, wait)
	if wp["payload_hash"] != hash {
		t.Errorf("d7_wait 未绑定载荷哈希: %+v", wp)
	}
	if wp["step_index"] != float64(2) || wait.StepID != 77 {
		t.Errorf("d7_wait 未说明挂在第几步: %+v step_id=%d", wp, wait.StepID)
	}
	if ws, _ := wp["expires_at"].(string); ws == "" {
		t.Errorf("d7_wait 缺 expires_at（跨进程查证就无从判「未到期」）: %+v", wp)
	}
	if wait.Direction != "event" {
		t.Errorf("d7_wait direction=%s want event", wait.Direction)
	}

	// 错载荷放行一次：judge 帧要把这次拒绝留痕（否则「有人试过批别的」这件事只有攻击者知道）
	if e.SignalConfirm(11, "badsign0") != VerdictMismatch {
		t.Fatal("错哈希应判 mismatch")
	}
	if e.SignalConfirm(11, hash) != VerdictGranted {
		t.Fatal("对哈希应放行")
	}
	select {
	case out := <-done:
		if out != confirmGranted {
			t.Errorf("闸门出路=%v want confirmGranted", out)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("放行后闸门未收敛")
	}
	waitForGate(t, func() bool { return len(repo.byAction("d7_confirm")) == 1 }, "放行后未落 judge 帧")

	judge := repo.byAction("d7_confirm")[0]
	jp := gatePayload(t, judge)
	if judge.Direction != "judge" {
		t.Errorf("judge 帧 direction=%s want judge", judge.Direction)
	}
	if judge.Ok == nil || !*judge.Ok {
		t.Error("放行成功的 judge 帧 ok 必须为 true")
	}
	if jp["payload_hash"] != hash || jp["decision"] != "granted" {
		t.Errorf("judge 帧未记录被批准的载荷: %+v", jp)
	}
	if n, _ := jp["mismatch_attempts"].(float64); n != 1 {
		t.Errorf("judge 帧未统计错载荷放行次数，got %+v", jp["mismatch_attempts"])
	}
	if judge.Seq <= wait.Seq {
		t.Errorf("seq 必须单调：d7_wait=%d d7_confirm=%d", wait.Seq, judge.Seq)
	}

	// 复核补刀（findings#2）：judge 帧必须自证「批的是哪一步」——step_id 与 step_index 都要与
	// 它的 d7_wait 同值且非零。现状只锁 wait 帧，把 executor.go:507 的 stepID 或 :509 的
	// step_index 改成 0 也测不出来：审计流里「批准」这一行不再指回任何具体步，跨步归因断链。
	// 这两条断言存在的唯一目的就是杀掉「judge 帧步绑定被置 0」这个变异。
	if judge.StepID != wait.StepID || judge.StepID == 0 {
		t.Errorf("judge 帧 step_id=%d 必须与 wait 帧(%d)同值且非零（批准行要指回被挂起的那一步）",
			judge.StepID, wait.StepID)
	}
	if ji, _ := jp["step_index"].(float64); ji != wp["step_index"] || ji == 0 {
		t.Errorf("judge 帧 step_index=%v 必须与 wait 帧(%v)同值且非零", jp["step_index"], wp["step_index"])
	}

	// 正文零泄漏：整场审计帧里不得出现一个字的评论原文
	blob, err := json.Marshal(repo.rows)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), body) {
		t.Error("审计帧携带了评论正文（I5 导出会把正文带进离线件）")
	}

	// 超时腿也要落 judge 帧且 decision 不是 granted（「没人批」与「批了」在审计面必须可辨）
	e2 := newConfirmExecutor()
	repo2 := &captureGateLogRepo{}
	e2.SetCommandLogRepository(repo2)
	seq2 := 0
	out2, _ := e2.awaitConfirmGate(context.Background(), task, &model.BrowserSession{ID: 12}, 78, 3,
		body, hash, &seq2, 40*time.Millisecond)
	if out2 != confirmWaitTimedOut {
		t.Fatalf("短预算下出路=%v want confirmWaitTimedOut", out2)
	}
	rows := repo2.byAction("d7_confirm")
	if len(rows) != 1 {
		t.Fatalf("超时腿 judge 帧数=%d want 1（没人批也要留痕）", len(rows))
	}
	if d, _ := gatePayload(t, rows[0])["decision"].(string); d == "granted" || d == "" {
		t.Errorf("超时腿 decision=%q，不得与放行同形", d)
	}
	if rows[0].Ok == nil || *rows[0].Ok {
		t.Error("超时腿 judge 帧 ok 必须为 false")
	}
}

// 4) 三态归因：中止/超时的 decision 各自独立（F4 的审计面对偶——终态能分辨，帧也要能分辨）。
func TestConfirmGateDecisionPerOutgoing(t *testing.T) {
	e := newConfirmExecutor()
	repo := &captureGateLogRepo{}
	e.SetCommandLogRepository(repo)
	task := &model.BrowserTask{ID: 4, UserID: 1}
	session := &model.BrowserSession{ID: 13}
	e.registerStop(session.ID)
	seq := 0
	done := make(chan confirmOutcome, 1)
	go func() {
		out, _ := e.awaitConfirmGate(context.Background(), task, session, 79, 0, "x", "hash-x", &seq, time.Minute)
		done <- out
	}()
	waitForGate(t, func() bool { return e.ConfirmPending(session.ID) }, "闸门未挂起")
	e.SignalStop(session.ID)
	select {
	case out := <-done:
		if out != confirmStoppedByUser {
			t.Errorf("出路=%v want confirmStoppedByUser", out)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stop 未解除闸门")
	}
	waitForGate(t, func() bool { return len(repo.byAction("d7_confirm")) == 1 }, "中止腿未落 judge 帧")
	d, _ := gatePayload(t, repo.byAction("d7_confirm")[0])["decision"].(string)
	if d != "stopped" {
		t.Errorf("中止腿 decision=%q want stopped（与超时/放行都不得同形）", d)
	}
}

// 5) 服务层：放行结果必须区分四态，其中「闸门在别的进程」靠库里未到期 d7_wait 帧判出。
type gateSessRepo struct {
	repository.BrowserSessionRepository
	sess *model.BrowserSession
}

func (g *gateSessRepo) GetByID(_ context.Context, id, userID uint) (*model.BrowserSession, error) {
	if g.sess == nil || g.sess.ID != id || g.sess.UserID != userID {
		return nil, gorm.ErrRecordNotFound
	}
	return g.sess, nil
}

func TestConfirmStatusDistinguishesGateElsewhere(t *testing.T) {
	sess := &model.BrowserSession{ID: 21, UserID: 5, Status: "active"}
	e := newConfirmExecutor()
	s := NewSessionService(&gateSessRepo{sess: sess}, &exportStepRepo{}, e)

	// 库里有一条「刚刚写下、还没到期」的 d7_wait，但本进程内存里没有闸门
	fresh := &captureGateLogRepo{rows: []*model.BrowserCommandLog{{
		SessionID: 21, Seq: 1, Direction: "event", Action: "d7_wait", Ok: verdict(true),
		CreatedAt: time.Now(),
		Payload:   datatypes.JSON(`{"payload_hash":"aaaa","expires_at":"` + time.Now().Add(time.Minute).Format(time.RFC3339Nano) + `"}`),
	}}}
	s.SetCommandLogRepository(fresh)
	if got, _ := s.Confirm(context.Background(), 21, 5, "aaaa"); got.Status != ConfirmStatusOtherInstance {
		t.Errorf("闸门在别处却回 %v——与「根本没开闸门」同一句文案，人就会去改编排而不是换实例", got.Status)
	}

	// 同一条帧但已过期 ⇒ 回到「没有待确认的提交点」，不许常驻报「在别处」
	expired := &captureGateLogRepo{rows: []*model.BrowserCommandLog{{
		SessionID: 21, Seq: 1, Direction: "event", Action: "d7_wait", Ok: verdict(true),
		CreatedAt: time.Now().Add(-2 * time.Hour),
		Payload:   datatypes.JSON(`{"payload_hash":"aaaa","expires_at":"` + time.Now().Add(-time.Hour).Format(time.RFC3339Nano) + `"}`),
	}}}
	s2 := NewSessionService(&gateSessRepo{sess: sess}, &exportStepRepo{}, e)
	s2.SetCommandLogRepository(expired)
	if got, _ := s2.Confirm(context.Background(), 21, 5, "aaaa"); got.Status != ConfirmStatusNone {
		t.Errorf("已过期的 d7_wait 仍判「在别的实例」=%v（挂起协程早随进程没了）", got.Status)
	}

	// 真挂起 + 对哈希 ⇒ 放行；错哈希 ⇒ mismatch 且明确回载荷不符
	e.registerConfirmGate(&confirmGate{sessionID: 21, payloadHash: "bbbb", ch: make(chan struct{})})
	s3 := NewSessionService(&gateSessRepo{sess: sess}, &exportStepRepo{}, e)
	s3.SetCommandLogRepository(&captureGateLogRepo{})
	if got, _ := s3.Confirm(context.Background(), 21, 5, "cccc"); got.Status != ConfirmStatusMismatch {
		t.Errorf("错载荷=%v want mismatch", got.Status)
	}
	got, err := s3.Confirm(context.Background(), 21, 5, "bbbb")
	if err != nil || got.Status != ConfirmStatusGranted {
		t.Errorf("对载荷未放行: %+v %v", got, err)
	}

	// 载荷哈希是必填项：空串不得当成「用户批了全部」
	if got, _ := s3.Confirm(context.Background(), 21, 5, ""); got.Status == ConfirmStatusGranted {
		t.Error("空 payload_hash 不得判为放行（fail-open 就是没门）")
	}

	// 归属越权：别人的闸门放行不了，且不得透露「有没有闸门」
	if _, err := s3.Confirm(context.Background(), 21, 999, "bbbb"); err == nil {
		t.Error("越权放行必须报错")
	}
}

// 6) 读侧闸门详情同样受归属校验约束（预览里有正文，越权读到就是外泄）。
func TestConfirmGateReadIsOwnershipScoped(t *testing.T) {
	sess := &model.BrowserSession{ID: 31, UserID: 5, Status: "active"}
	e := newConfirmExecutor()
	e.registerConfirmGate(&confirmGate{sessionID: 31, payloadHash: "cccc", preview: "只有本人能看见", ch: make(chan struct{})})
	s := NewSessionService(&gateSessRepo{sess: sess}, &exportStepRepo{}, e)
	gate, pending, err := s.ConfirmGate(context.Background(), 31, 5)
	if err != nil || !pending || gate.Preview != "只有本人能看见" {
		t.Fatalf("本人读侧失败: %+v %v %v", gate, pending, err)
	}
	if _, pending, err := s.ConfirmGate(context.Background(), 31, 999); err == nil || pending {
		t.Errorf("越权读闸门详情不得成功: %v %v", pending, err)
	}
	if _, pending, _ := s.ConfirmGate(context.Background(), 31, 5); !pending {
		t.Fatal("闸门应仍在")
	}
}

// 7) 闸门的载荷永不为空：空载荷的闸门等于一扇匿名放行口，开闸前就地炸；
// 空载荷的放行请求也不接受（不带载荷的放行与空白支票同构，见控制器 400 那条腿）。
func TestGatePayloadMustNeverBeEmpty(t *testing.T) {
	e := newConfirmExecutor()
	task := &model.BrowserTask{ID: 4, UserID: 1, RequireConfirm: true}
	session := &model.BrowserSession{ID: 41, TaskID: 4, UserID: 1}
	repo := &captureGateLogRepo{}
	e.SetCommandLogRepository(repo)
	seq := 0
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("空载荷哈希竟开出了闸门——这份闸门任何一次放行都能背书")
			}
		}()
		e.awaitConfirmGate(context.Background(), task, session, 80, 0, "正文", "", &seq, time.Minute)
	}()
	if e.ConfirmPending(session.ID) {
		t.Error("炸在开闸前，不该留下半只闸门（挂起态泄漏=会话永久 active）")
	}
	if len(repo.rows) != 0 {
		t.Errorf("空载荷闸门不该留下任何审计帧，got %d 行", len(repo.rows))
	}

	e.registerConfirmGate(&confirmGate{sessionID: 42, payloadHash: "real-hash", ch: make(chan struct{})})
	if got := e.SignalConfirm(42, ""); got != VerdictMismatch {
		t.Errorf("空载荷放行=%v want VerdictMismatch（不带载荷的放行不认）", got)
	}
	if !e.ConfirmPending(42) {
		t.Error("拒一次空载荷不得撤闸门")
	}
}

// 8) 复核补刀（findings#1）：预览截断常量 d7PreviewRunes 必须由「真实构造路径」绑定。
// 现存腿（TestPendingGateExposesAwaitingPayload / TestConfirmGateReadIsOwnershipScoped）都手搭
// &confirmGate{preview:…}，executor.go:448 把常量翻成 0 时它们照样绿——而那样审批人只剩一个
// 8 位指纹可看，正是 A5 要消灭的盲签形状。本腿走 awaitConfirmGate 唯一入口，喂一段远超预算的
// 正文，断言闸门预览是原文的非空前缀（截断后不得只剩省略号）。这条腿专杀「d7PreviewRunes→0」。
func TestAwaitConfirmGatePreviewTruncatesSubmittedText(t *testing.T) {
	e := newConfirmExecutor()
	task := &model.BrowserTask{ID: 4, UserID: 1, RequireConfirm: true}
	session := &model.BrowserSession{ID: 51, TaskID: 4, UserID: 1}
	// 明显长于 d7PreviewRunes 的原文：走的必然是 truncateRunes 的截断分支。
	submitted := strings.Repeat("这条评论会原样发出去核对", 20) // 12×20=240 runes > 200
	hash := HashWriteText(submitted)
	seq := 0
	done := make(chan confirmOutcome, 1)
	go func() {
		out, _ := e.awaitConfirmGate(context.Background(), task, session, 88, 1, submitted, hash, &seq, time.Minute)
		done <- out
	}()
	waitForGate(t, func() bool { return e.ConfirmPending(session.ID) }, "真实构造路径未挂起闸门")

	got, ok := e.PendingGate(session.ID)
	if !ok {
		t.Fatal("真实构造路径读不到挂起闸门")
	}
	const marker = "…"
	if got.Preview == "" {
		t.Fatalf("真实构造路径预览为空：d7PreviewRunes=%d 被截没了，审批人只剩指纹可看（盲签）", d7PreviewRunes)
	}
	if !strings.HasSuffix(got.Preview, marker) {
		t.Errorf("超长正文的预览应以截断标注收尾，证明走的确实是截断路径，got %q", got.Preview)
	}
	core := strings.TrimSuffix(got.Preview, marker)
	if core == "" {
		t.Fatalf("预览去掉标注后为空（=常量被翻成 0 的形状：只剩一个省略号）：审批人看不见任何正文，got %q", got.Preview)
	}
	if !strings.HasPrefix(submitted, core) {
		t.Errorf("预览不是提交文本的前缀（内容错位）：got %q", got.Preview)
	}

	e.SignalConfirm(session.ID, hash)
	<-done
}

// 9) 复核补刀（findings#4）：d7_wait 帧落盘的 expires_at 必须是「这一只闸门由预算推出的到期点」，
// 而不是一个写死的大数（executor.go:498 若被改成 time.Now().Add(24h)）。现状写侧只断言非空串，
// 读侧又只吃测试种子的帧，所以「假 24h 到期」既能写下也无人核对——跨进程查证会照着一只早已凉透
// 的闸门继续判「在别的实例」。本腿把「写下的期望」与「内存真值」AND 起来：既要落在预算窗口内，
// 也要等于读侧详情里的 gate.ExpiresAt。专杀「expires_at 写成固定 24h」。
func TestAwaitConfirmGateFrameWritesRealDeadline(t *testing.T) {
	e := newConfirmExecutor()
	repo := &captureGateLogRepo{}
	e.SetCommandLogRepository(repo)
	task := &model.BrowserTask{ID: 4, UserID: 1, RequireConfirm: true}
	session := &model.BrowserSession{ID: 52, TaskID: 4, UserID: 1}
	const wait = 45 * time.Second // 远小于 24h：把「假 24h」与「真预算」拉开到可判
	body := "预算到期核对"
	hash := HashWriteText(body)
	before := time.Now()
	seq := 0
	done := make(chan confirmOutcome, 1)
	go func() {
		out, _ := e.awaitConfirmGate(context.Background(), task, session, 89, 1, body, hash, &seq, wait)
		done <- out
	}()
	waitForGate(t, func() bool { return len(repo.byAction("d7_wait")) == 1 }, "真实构造路径未落 d7_wait 帧")

	ws, _ := gatePayload(t, repo.byAction("d7_wait")[0])["expires_at"].(string)
	if ws == "" {
		t.Fatal("d7_wait 帧缺 expires_at")
	}
	written, err := time.Parse(time.RFC3339Nano, ws)
	if err != nil {
		t.Fatalf("expires_at 不是 RFC3339Nano: %v", err)
	}
	// ① 与「本闸门预算推出的到期点」对齐：before+wait 附近，绝不许漂到 24h 之外。
	lo, hi := before.Add(wait).Add(-2*time.Second), before.Add(wait).Add(5*time.Second)
	if written.Before(lo) || written.After(hi) {
		t.Errorf("帧 expires_at=%s 不在预算到期窗口 [%s,%s)——写下的期望没跟着 wait 走（如被改成固定 24h）",
			ws, lo.Format(time.RFC3339), hi.Format(time.RFC3339))
	}
	// ② 与读侧详情里的内存真值 AND：写下去的必须就是那只活闸门的到期时刻。
	got, ok := e.PendingGate(session.ID)
	if !ok {
		t.Fatal("闸门未挂起，无法比对内存真值")
	}
	if d := written.Sub(got.ExpiresAt); d < -2*time.Second || d > 2*time.Second {
		t.Errorf("帧 expires_at 与闸门内存 ExpiresAt 相差 %v（写下的与内存里的是两回事）", d)
	}

	e.SignalConfirm(session.ID, hash)
	<-done
}

// 10) 复核补刀（findings#3）：跨进程归因取「最新一帧 d7_wait」（session.go:142 的 l.Seq > latest.Seq）。
// 现存种子每会话恰有一帧，把 > 翻成 < 也测不出来——一旦最新帧未到期而旧帧已凉，归因会跟着旧帧
// 说「不在别处」（漏判），或反过来被一条三年前的旧帧永久钉死成「在别的实例」（误判）。本腿两种子
// 各测一头：新帧未到期+旧帧过期 ⇒ gate_on_another_instance；新帧过期+旧帧未到期 ⇒ no_gate。
// 两头任一被 < 变异翻错都当场红，专杀「l.Seq > latest.Seq → <」。
func TestGateElsewhereFollowsNewestWaitFrame(t *testing.T) {
	sess := &model.BrowserSession{ID: 61, UserID: 7, Status: "active"}
	e := newConfirmExecutor()
	future := time.Now().Add(time.Minute).Format(time.RFC3339Nano)
	past := time.Now().Add(-time.Hour).Format(time.RFC3339Nano)
	waitFrame := func(seq int, exp string) *model.BrowserCommandLog {
		return &model.BrowserCommandLog{
			SessionID: 61, Seq: seq, Direction: "event", Action: "d7_wait", Ok: verdict(true),
			CreatedAt: time.Now(),
			Payload:   datatypes.JSON(`{"payload_hash":"hh","expires_at":"` + exp + `"}`),
		}
	}

	// 头一：Seq=1 旧帧已过期、Seq=2 新帧未到期 ⇒ 归因跟最新帧 = gate_on_another_instance。
	// 本进程无闸门（SignalConfirm=NoGate），归因全权交给 gateElsewhere 的「取最新帧」判定。
	s := NewSessionService(&gateSessRepo{sess: sess}, &exportStepRepo{}, e)
	s.SetCommandLogRepository(&captureGateLogRepo{rows: []*model.BrowserCommandLog{
		waitFrame(1, past), waitFrame(2, future),
	}})
	if got, _ := s.Confirm(context.Background(), 61, 7, "hh"); got.Status != ConfirmStatusOtherInstance {
		t.Errorf("新帧未到期/旧帧过期时归因=%v，want gate_on_another_instance（应跟最新帧；被 > → < 翻错则此处红）", got.Status)
	}

	// 头二：Seq=1 旧帧未到期、Seq=2 新帧已过期 ⇒ 不判在别处 = no_gate（挂起协程早已随进程凉掉）。
	s2 := NewSessionService(&gateSessRepo{sess: sess}, &exportStepRepo{}, e)
	s2.SetCommandLogRepository(&captureGateLogRepo{rows: []*model.BrowserCommandLog{
		waitFrame(1, future), waitFrame(2, past),
	}})
	if got, _ := s2.Confirm(context.Background(), 61, 7, "hh"); got.Status != ConfirmStatusNone {
		t.Errorf("新帧过期/旧帧未到期时归因=%v，want no_gate（不得被旧帧永久钉成在别处；被 > → < 翻错则此处红）", got.Status)
	}
}
