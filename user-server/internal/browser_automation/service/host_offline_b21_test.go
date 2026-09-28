// 传输层「命令从未上线」的归因收口。
//
// 现场：Host 在 prep 之后、send 之前掉线（D7 闸门一等多分钟，这一类是常态不是边角料），
// `Request` 在 `GetConn` 处就取不到连接——那一帧根本没写进 socket，点击必然没发生。
// 但执行侧此前只认扩展侧产出的三类「从未发生」token，传输层这条证据没人接：
// post_comment 照旧落 sent 台账 + 白跑 finalize，普通写步被记成 unattributed，
// 两者都在双发闸拦阻集合内 ⇒ 一次秒级断连把这条写永久锁死，只能人工去核对一条
// 「从未发生过的评论」。
//
// 两条腿一起才成立（缺一腿就是文本匹配式假修法）：
//
//	· 未上线 ⇒ 零副作用 ⇒ 台账留 prepared / 不记尝试 ⇒ 可安全重下发；
//	· 帧已写进 socket 之后连接才断 ⇒ 送达不可判 ⇒ 仍是「结果未知」，照旧交人工。
package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/browser_automation/model"

	"github.com/gorilla/websocket"
)

// newB21Host 只起「一条真 Host WS 连接 + 注册表」，不碰 DB 也不跑 Executor：
// 本文件要判的是传输层事实，Executor 的编排面在 T3/T4 才进场。
// 返回的 fake 扩展按 reply 脚本回包（hangFrame=收下命令但永不回包）。
//
// register=false 时连接只建成、不登记：Register 会带起 readLoop/pingLoop/probeServable
// 三条 goroutine（探针在注册当下就向这条 socket 写一帧），要判「注册表里有连接但写不动」
// 那一格就得自己造在册状态——留着 goroutine 会和测试侧的写并发，gorilla 直接 panic
// （见 TestWriteFailureIsNotNeverOnWire 注释）。
func newB21Host(t *testing.T, userID uint, register bool, reply func(action string, frame map[string]any) (map[string]any, string)) (*HostRegistry, *fakeExtension, *websocket.Conn) {
	t.Helper()
	reg := NewHostRegistry()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	wired := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("升级失败: %v", err)
			return
		}
		wired <- c
	}))
	t.Cleanup(srv.Close)
	dialer := websocket.Dialer{}
	clientConn, _, err := dialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/host-ws", nil)
	if err != nil {
		t.Fatalf("客户端连接失败: %v", err)
	}
	ext := &fakeExtension{conn: clientConn, reply: reply}
	go ext.pump(t)
	serverConn := <-wired
	if register {
		reg.Register(userID, "1.5.0-b21", 0, serverConn)
	}
	t.Cleanup(func() { _ = clientConn.Close() })
	return reg, ext, serverConn
}

// 1) 注册表里没有这个用户：一帧都没写出去，必须带「未上线」标记；
// 同时对外的「Host 不在线」引导不得因此消失（controller RunTask 靠
// errors.Is(err, ErrHostOffline) 转 409，锁在 controller/error_code_b10_test.go）。
func TestRequestWithoutConnIsNeverOnWire(t *testing.T) {
	reg := NewHostRegistry()
	_, err := reg.Request(context.Background(), 4242, time.Second, map[string]any{"action": "wait"})
	if err == nil {
		t.Fatal("无连接用户发命令必须报错")
	}
	if !errors.Is(err, ErrHostOffline) {
		t.Errorf("未上线仍须可判为 ErrHostOffline（对外 409 引导不变），got %v", err)
	}
	if !isCommandNeverOnWire(err) {
		t.Errorf("一帧都没写进 socket，必须带未上线标记，got %v", err)
	}
	if !isNeverExecuted(err) {
		t.Errorf("未上线必须落进「从未发生」集合，否则台账把没做的事记成一次尝试：%v", err)
	}
}

// 2) 帧已写进 socket 之后连接才断：错误文本与 T1 同族（都落在「Host 未连接」上），
// 分类却必须相反——这一类送达不可判。同步点是 fake 扩展读到那一帧（record 先于回包），
// 读到之后测试侧才关闭连接，所以「已写出」是事实不是推测。
func TestConnDropAfterWriteIsNotNeverOnWire(t *testing.T) {
	reg, ext, _ := newB21Host(t, b21UserID, true, func(string, map[string]any) (map[string]any, string) {
		return nil, hangFrame // 收下命令但不回包：只留「写出」这一件事
	})
	go func() {
		if !waitFor(5*time.Second, func() bool { return ext.countOf("wait") >= 1 }) {
			return
		}
		_ = ext.conn.Close() // 服务端读循环报错 → HostConn.close → 在途 Request 从 conn.Done() 醒来
	}()
	_, err := reg.Request(context.Background(), b21UserID, 5*time.Second, map[string]any{"action": "wait"})
	if err == nil {
		t.Fatal("连接断开时命令必须有归因，不能假成")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("走到 Request 自己的超时=断连没被这帧观察到，夹具同步失效：%v", err)
	}
	if !errors.Is(err, ErrHostOffline) {
		t.Errorf("断连仍须可判为 ErrHostOffline，got %v", err)
	}
	if isCommandNeverOnWire(err) {
		t.Errorf("帧已写进 socket 后才发现断开，不得判成未上线（把「不知道有没有发出去」当成"+
			"「没发出去」重下发，双发就是这么来的）：%v", err)
	}
}

// 3) 命令有去无回（Host 假死）：R26-2 既有口径「超时≠未发生」，本批新增判据不得把它抢过去。
func TestHostCommandTimeoutIsNotNeverOnWire(t *testing.T) {
	reg, _, _ := newB21Host(t, b21UserID, true, func(string, map[string]any) (map[string]any, string) {
		return nil, hangFrame
	})
	_, err := reg.Request(context.Background(), b21UserID, 300*time.Millisecond, map[string]any{"action": "wait"})
	if err == nil {
		t.Fatal("扩展不回包时命令必须超时报错")
	}
	if !strings.Contains(err.Error(), "超时") {
		t.Fatalf("夹具应命中 Request 的命令超时分支，got %v", err)
	}
	if isCommandNeverOnWire(err) {
		t.Errorf("超时=结果未知，不得判成未上线：%v", err)
	}
	if isNeverExecuted(err) {
		t.Errorf("超时不得落进「从未发生」集合（R26-2 三分归一）：%v", err)
	}
}

// dropHostNow 把在册 Host 从注册表摘掉但不关 socket：之后的 Request 一律落在
// 「取不到连接」这一分支，等价于真机上 Host 进程已经没了、服务端还没感知的那一拍。
func dropHostNow(t *testing.T, exec *Executor) {
	t.Helper()
	conn, ok := exec.hand.registry.GetConn(wsE2EUserID)
	if !ok {
		t.Fatal("夹具里这条 Host 连接本应在册")
	}
	exec.hand.registry.unregister(wsE2EUserID, conn)
}

// 3b) 写进一个已经死掉的 socket：writeJSON 报错这一类**不算**未上线。
//
// 这是三类里最容易顺手纳进去的一类（文本也带「Host」，看起来和 T1 一回事），但 gorilla 的
// 契约是写失败之后连接状态未知——半帧送达是对端读出错、命令不执行，但也可能是写完才发现
// 连接已被对端关掉而命令其实跑完了。判成未上线=允许重下发，赌的就是那半帧没到。
//
// 夹具要造的是「连接在册、但 socket 已经关死」这一格，而它不能靠 Register 造：
// Register 会带起 readLoop/pingLoop/probeServable 三条 goroutine，于是
//
//   - readLoop 被 Close 叫醒 → defer c.close() → unregister，Request 可能先看到「取不到连接」
//     而不是写失败（本轮全量门禁就被这一拍抽中：实测 15/500、36/1000 次落在未上线分支）
//   - 三条 goroutine 与测试侧各持自己那份 writeMu、共用同一个 *websocket.Conn，两边同时写
//     就是 gorilla 的 concurrent write to websocket connection panic
//     （先一版按「Register 后换个 HostConn 占同一个 key」写，实测 1000 次抽中 1 次 panic）
//
// 收法：不 Register，手造一条没有 goroutine 的连接登记进表——没人 unregister，也没人并发写，
// Request 必定且只走到 writeJSON 这一步。
func TestWriteFailureIsNotNeverOnWire(t *testing.T) {
	reg, _, serverConn := newB21Host(t, b21UserID, false, func(string, map[string]any) (map[string]any, string) {
		return nil, hangFrame
	})
	conn := newHostConn(b21UserID, "1.5.0-b21", 0, serverConn, reg)
	reg.mu.Lock()
	reg.conns[b21UserID] = conn
	reg.mu.Unlock()
	if err := serverConn.Close(); err != nil { // 只关 socket：注册表面依旧是「有连接」
		t.Fatalf("关 socket 失败: %v", err)
	}
	_, err := reg.Request(context.Background(), b21UserID, time.Second, map[string]any{"action": "wait"})
	if err == nil {
		t.Fatal("写进已关闭的 socket 必须报错")
	}
	if !strings.Contains(err.Error(), "发送命令到 Host 失败") {
		t.Fatalf("夹具必须命中 writeJSON 失败分支，got %v", err)
	}
	if isCommandNeverOnWire(err) {
		t.Errorf("写失败时送达不可判，不得带未上线标记：%v", err)
	}
	if isNeverExecuted(err) {
		t.Errorf("写失败不得落进「从未发生」集合：%v", err)
	}
}

// 4) 普通写步（非 post_comment）：未上线那一帧不得记成「已尝试」。
//
// 记成 unattributed 的代价是具体的：它在双发闸拦阻集合里，下一轮同文本重下发会被判
// 「防双发跳过」——可这一帧从未上线，跳过就是把一个从未发生过的动作永久挂起来等人。
func TestGenericWriteStepNotDeliveredLeavesNoAttempt(t *testing.T) {
	const stepsJSON = `[{"action":"open_tab","target":"https://example.com/orphan"},
{"action":"click","target":"button.submit-now"}]`
	var exec *Executor
	exec, ext, bundle := newWSE2E(t, func(action string, frame map[string]any) (map[string]any, string) {
		data, errMsg := happyReply(action, frame)
		if action == "open_tab" {
			dropHostNow(t, exec) // 答完 open_tab 就掉线：下一步的 click 一帧都发不出去
		}
		return data, errMsg
	})
	task, session := bundle.seedTask(t, stepsJSON, false)
	task.Platform = orphanPlatform
	steps, err := ParseSteps([]byte(stepsJSON))
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	if n := ext.countOf("click"); n != 0 {
		t.Fatalf("Host 已离线却仍有 %d 帧 click 到达扩展（动作序列 %s）——夹具失效", n, ext.actions())
	}
	row := readStepState(t, bundle, session.ID, 1)
	if row.Status != "failed" {
		t.Errorf("离线写步必须判败，got %q", row.Status)
	}
	if row.SubmitState != "" {
		t.Errorf("一帧未出的写步把 %q 记进了台账——「已尝试」可以是零帧，重下发会被双发闸拦下", row.SubmitState)
	}
	if !strings.Contains(row.ErrorMsg, "未连接") {
		t.Errorf("步仍应因 Host 离线判败，err=%q", row.ErrorMsg)
	}
}

// 5) post_comment 的 send 腿：D7 放行之后 Host 已掉线 ⇒ 早返、台账留 prepared、不进 finalize。
func TestPostCommentSendNotDeliveredKeepsPrepared(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, happyReply)
	task, session := bundle.seedTask(t, threeStageSteps, true)
	steps, err := ParseSteps([]byte(threeStageSteps))
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
	gate, _ := exec.PendingGate(session.ID)
	// 闸门等待以分钟计，Host 掉在这段等待里是常态：放行前把连接摘掉。
	dropHostNow(t, exec)
	if got := exec.SignalConfirm(session.ID, gate.PayloadHash); got != VerdictGranted {
		t.Fatalf("放行未命中：verdict=%v", got)
	}
	select {
	case <-done:
	case <-time.After(e2eExecBudget):
		t.Fatal("执行未在预算内收尾")
	}

	row := readStepState(t, bundle, session.ID, 3)
	if row.Status != "failed" {
		t.Errorf("未上线的提交必须判败，got %q", row.Status)
	}
	if row.SubmitState != model.StepSubmitPrepared {
		t.Errorf("提交从未发生，台账必须留在 %q，got %q——sent/unattributed 都在双发闸拦阻集合内，"+
			"一次秒级断连就会把这条评论永久锁死", model.StepSubmitPrepared, row.SubmitState)
	}
	if strings.Contains(row.ErrorMsg, "结果未知") {
		t.Errorf("错误文案不得把它说成结果未知（运维据此决定要不要人工核对）：%q", row.ErrorMsg)
	}
	if !strings.Contains(row.ErrorMsg, "未提交") {
		t.Errorf("错误文案须点名「未提交」，got %q", row.ErrorMsg)
	}
	// finalize 是「提交之后回查」，点击从未发生就没有要回查的东西：一帧都不该发。
	if n := ext.countOf("comment_verify"); n != 0 {
		t.Errorf("未上线的提交仍去 finalize 回查 %d 次", n)
	}
	var s model.BrowserSession
	if err := bundle.db.WithContext(context.Background()).First(&s, session.ID).Error; err != nil {
		t.Fatalf("会话回读失败: %v", err)
	}
	if strings.Contains(string(s.ExtractedData), "post_comment") {
		t.Errorf("从未发生的提交不该留下 finalize 证据面：%s", s.ExtractedData)
	}
}

const b21UserID = uint(9377)
