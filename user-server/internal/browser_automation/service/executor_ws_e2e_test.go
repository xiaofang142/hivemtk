package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/browser_automation/repository"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/gorilla/websocket"
)

// 批5 A 链路端到端：真 WebSocket 帧 + 真 DB 落库 + 真 Executor 步循环，
// 唯一替身是「扩展侧」（Chrome 不在此环境）。
// 补的正是 post_comment_finalize_test.go:45 当年记下的缺口——
// 「HostRegistry.Request 依赖真实 websocket 连接，此处不做网络桩」：
// 三段式顺序、F2① 禁重试、D7 闸门这些红线此前只有纯函数/源码静态锁兜着，
// 一旦帧格式或 req_id 关联出错，静态锁测不出来、真机又要人工参与。
// 本测试把「不可逆动作是否到达线、到达几次」变成可自动跑出来的事实。

const wsE2EUserID = uint(9301)

// fakeExtension 脚本化扩展侧：按 action 回 data 或 error，并全量记录收到的动作序列。
type fakeExtension struct {
	conn    *websocket.Conn
	writeMu sync.Mutex

	mu   sync.Mutex
	seen []string

	// reply 由用例给出：返回 (data, errMsg)，errMsg 非空即回 ok=false
	reply func(action string, frame map[string]any) (map[string]any, string)
}

func (f *fakeExtension) record(action string) {
	f.mu.Lock()
	f.seen = append(f.seen, action)
	f.mu.Unlock()
}

func (f *fakeExtension) actions() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.seen, ",")
}

func (f *fakeExtension) countOf(action string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, a := range f.seen {
		if a == action {
			n++
		}
	}
	return n
}

// pump 读命令帧→回结果帧。服务端 writeLoop 有写锁，客户端侧同样串行（本测试单 goroutine 发起）。
func (f *fakeExtension) pump(t *testing.T) {
	t.Helper()
	for {
		var frame map[string]any
		if err := f.conn.ReadJSON(&frame); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				t.Logf("fake extension 读帧结束: %v", err)
			}
			return
		}
		action, _ := frame["action"].(string)
		reqID, _ := frame["req_id"].(string)
		f.record(action)

		data, errMsg := map[string]any{}, ""
		if f.reply != nil {
			data, errMsg = f.reply(action, frame)
		}
		if errMsg == hangFrame {
			continue // 该命令故意不回包：模拟扩展侧假死
		}
		resp := map[string]any{"req_id": reqID, "ok": errMsg == "", "data": data}
		if errMsg != "" {
			resp["error"] = errMsg
		}
		f.writeMu.Lock()
		err := f.conn.WriteJSON(resp)
		f.writeMu.Unlock()
		if err != nil {
			return
		}
	}
}

// xhsNoteSnapshot 正常笔记详情页的 a11y 快照样本（不含任何拦截 marker，A2 收窄后的判定应放行）
const xhsNoteSnapshot = `- heading "春日穿搭分享" [ref=@e1]
- article [ref=@e2]
  - text "整体搭配很清爽，求链接"
- textbox "发表你的评论" [ref=@e3]
- button "发送" [ref=@e4]`

// newWSE2E 起一条真实的 Host WS 连接（httptest 升级 → registry.Register → 客户端 fake 扩展），
// 并把 Executor 接到真测试库（task/session/step/command_log 四表）。
func newWSE2E(t *testing.T, reply func(action string, frame map[string]any) (map[string]any, string)) (*Executor, *fakeExtension, *wsE2EDeps) {
	t.Helper()

	db := testutil.NewTestDB(t,
		&model.BrowserTask{}, &model.BrowserSession{},
		&model.BrowserStep{}, &model.BrowserCommandLog{},
	)
	if db == nil {
		t.Skip("测试库不可达")
	}

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
	reg.Register(wsE2EUserID, "1.5.0-e2e", os.Getpid(), serverConn)
	t.Cleanup(func() { _ = clientConn.Close() })

	bundle := &wsE2EDeps{
		sessionRepo: repository.NewBrowserSessionRepositoryWithDB(db),
		stepRepo:    repository.NewBrowserStepRepositoryWithDB(db),
		cmdLogRepo:  repository.NewBrowserCommandLogRepositoryWithDB(db),
		db:          db,
	}
	exec := NewExecutor(NewHand(reg), bundle.sessionRepo, bundle.stepRepo, nil, nil)
	exec.SetCommandLogRepository(bundle.cmdLogRepo)
	return exec, ext, bundle
}

type wsE2EDeps struct {
	sessionRepo repository.BrowserSessionRepository
	stepRepo    repository.BrowserStepRepository
	cmdLogRepo  repository.BrowserCommandLogRepository
	db          *gorm.DB
}

// seedTask 落一条任务 + 一个 session，返回内存对象供 ExecuteSession 使用。
// delay_ms=0：步间拟人延迟在本测试里不是被测对象（humanize_test.go 已覆盖），留零保快。
func (b *wsE2EDeps) seedTask(t *testing.T, stepsJSON string, requireConfirm bool) (*model.BrowserTask, *model.BrowserSession) {
	t.Helper()
	ctx := context.Background()
	task := &model.BrowserTask{
		Name: "e2e-xhs-三段式", TaskType: "one_shot", Status: "ready",
		Url: "https://www.xiaohongshu.com/explore", Platform: "xiaohongshu",
		Steps: datatypes.JSON(stepsJSON), UserID: wsE2EUserID,
		LoopCount: 1, DelayMs: 0, TimeoutSec: 60, RequireConfirm: requireConfirm,
	}
	if err := b.db.WithContext(ctx).Create(task).Error; err != nil {
		t.Fatalf("任务落库失败: %v", err)
	}
	session := &model.BrowserSession{TaskID: task.ID, UserID: wsE2EUserID, Status: "created", Url: task.Url}
	if err := b.sessionRepo.Create(ctx, session); err != nil {
		t.Fatalf("会话落库失败: %v", err)
	}
	return task, session
}

func (b *wsE2EDeps) reloadSession(t *testing.T, id uint) *model.BrowserSession {
	t.Helper()
	s, err := b.sessionRepo.GetByID(context.Background(), id, wsE2EUserID)
	if err != nil {
		t.Fatalf("会话回读失败: %v", err)
	}
	return s
}

func (b *wsE2EDeps) steps(t *testing.T, sessionID uint) []*model.BrowserStep {
	t.Helper()
	list, err := b.stepRepo.ListBySessionID(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("步骤回读失败: %v", err)
	}
	return list
}

// commandActions 回读一个 session 的全部命令日志 action（按 seq 升序）。
func (b *wsE2EDeps) commandActions(t *testing.T, sessionID uint) []string {
	t.Helper()
	var rows []string
	if err := b.db.WithContext(context.Background()).Model(&model.BrowserCommandLog{}).
		Where("session_id = ?", sessionID).Order("seq asc").Pluck("action", &rows).Error; err != nil {
		t.Fatalf("command_log 回读失败: %v", err)
	}
	return rows
}

func containsStr(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// happyReply 正常回包脚本：一次 comment_verify 即命中（finalize 首轮收敛，测试不等 16s 轮询窗）
func happyReply(action string, frame map[string]any) (map[string]any, string) {
	switch action {
	case "open_tab":
		return map[string]any{"chrome_tab_id": 777}, ""
	case "snapshot":
		return map[string]any{"snapshot": xhsNoteSnapshot, "url": "https://www.xiaohongshu.com/explore/abc"}, ""
	case "markdown":
		return map[string]any{"markdown": "# 春日穿搭分享\n\n整体搭配很清爽"}, ""
	case "comment_prep":
		return map[string]any{"prepared": true}, ""
	case "comment_send":
		return map[string]any{"sent": true}, ""
	case "comment_verify":
		return map[string]any{"verified": true, "matched": 1, "text": "测试评论正文"}, ""
	case "query":
		return map[string]any{"exists": false}, ""
	default:
		return map[string]any{}, ""
	}
}

const threeStageSteps = `[{"action":"open_tab","target":"https://www.xiaohongshu.com/explore"},
{"action":"snapshot"},
{"action":"markdown"},
{"action":"post_comment","value":"测试评论正文"}]`

// 1) 读链路 + 写链路全跑通：步序、tab_id 回写、快照落库、finalize 证据、命令日志计数、终态
func TestWSE2E_ReadThenCommentThreeStage(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, happyReply)
	task, session := bundle.seedTask(t, threeStageSteps, false)

	steps, err := ParseSteps([]byte(threeStageSteps))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	got := bundle.reloadSession(t, session.ID)
	if got.Status != "completed" {
		t.Errorf("终态=%s（%s），want completed", got.Status, got.ErrorMsg)
	}
	if got.ChromeTabID != 777 {
		t.Errorf("chrome_tab_id 未回写: %d", got.ChromeTabID)
	}
	if got.TotalSteps != 4 || got.SuccessSteps != 4 || got.FailedSteps != 0 {
		t.Errorf("步计数 total=%d success=%d failed=%d, want 4/4/0", got.TotalSteps, got.SuccessSteps, got.FailedSteps)
	}
	if !strings.Contains(got.Snapshot, "发表你的评论") {
		t.Error("snapshot 未落库")
	}

	// 帧序列（去掉 detectBlockedIfFatal 的额外 snapshot 探针与 F3 注册探针后应恰为编排语义序列；
	// 两类帧各自有专测正向断言其存在，这里只是不把它们算进编排序）
	var wire []string
	for _, a := range strings.Split(ext.actions(), ",") {
		if a != "snapshot" && a != "tab_exists" {
			wire = append(wire, a)
		}
	}
	want := "open_tab,markdown,comment_prep,comment_send,comment_verify,close_tab"
	if strings.Join(wire, ",") != want {
		t.Errorf("帧序列=%s want %s", strings.Join(wire, ","), want)
	}
	// F10 收口回收：编排里没有 close_tab 步，末尾这一帧必须来自会话收口（真机实测泄漏
	// 40 个 tab 的成因就是「失败路径永不执行 close_tab 步」）
	if n := ext.countOf("close_tab"); n != 1 {
		t.Errorf("会话收口 close_tab 帧=%d want 1（tab 泄漏回归位）", n)
	}
	if rows := bundle.commandActions(t, session.ID); !containsStr(rows, "session_tab_cleanup") {
		t.Errorf("收口未落审计行，command_log actions=%v", rows)
	}
	// 唯一不可逆点：整场只发一次
	if n := ext.countOf("comment_send"); n != 1 {
		t.Errorf("comment_send 发生 %d 次，红线=1", n)
	}

	var evidence map[string]any
	if err := json.Unmarshal(got.ExtractedData, &evidence); err != nil {
		t.Fatalf("extracted_data 解析失败: %v", err)
	}
	list, _ := evidence["post_comment"].([]any)
	if len(list) != 1 {
		t.Fatalf("post_comment 证据条数=%d want 1", len(list))
	}
	entry, _ := list[0].(map[string]any)
	if entry["verified"] != true || entry["text"] != "测试评论正文" {
		t.Errorf("证据内容异常: %+v", entry)
	}

	// P8 命令日志：command + event 双帧落库，条数应 ≥ 步数×2
	logs, err := bundle.cmdLogRepo.ListBySessionID(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("命令日志回读失败: %v", err)
	}
	if len(logs) < 8 {
		t.Errorf("命令日志条数=%d，command/event 双帧应 ≥8", len(logs))
	}

	// F3：注册探针（零副作用 tab_exists）通过后，host/status 必须带「可服务」证据
	if n := ext.countOf("tab_exists"); n != 1 {
		t.Errorf("注册探针 tab_exists 到线 %d 次 want 1", n)
	}
	st := exec.hand.registry.MyStatus(wsE2EUserID)
	if st["servable"] != true || st["last_cmd_ok_at"] == nil {
		t.Errorf("Host 状态缺可服务证据：%+v", st)
	}

	// F6：拦截探测帧必须进审计包，且 seq 在 session 内唯一单调
	var detectSeqs []int
	for _, l := range logs {
		if l.Action == "block_detect" {
			detectSeqs = append(detectSeqs, l.Seq)
			var payload map[string]any
			if err := json.Unmarshal(l.Payload, &payload); err != nil {
				t.Fatalf("block_detect payload 解析失败: %v", err)
			}
			if payload["url"] == "" || payload["snapshot_chars"] == nil {
				t.Errorf("block_detect 审计面缺证据字段：%s", l.Payload)
			}
			// F11c：探测成功但未拦截时 ok 必须为真——旧实现把 ok 当「有没有拦到」传，
			// 正常页面的审计包里每一帧都是红的，读包的人据此误判检测链路坏了。
			if payload["blocked"] == false && !l.Ok {
				t.Errorf("干净页面的探测被记成失败：%s", l.Payload)
			}
		}
	}
	if len(detectSeqs) == 0 {
		t.Fatal("命令日志无 block_detect 帧：探测未进审计包")
	}
	for i, seq := range logs {
		if i == 0 {
			continue
		}
		// command/event 成对共用一个 seq（一问一答同序号），故全局只要求不减；
		// command 帧之间必须严格递增——探测帧若不占号，审计面就会出现两条同号 command。
		if seq.Seq < logs[i-1].Seq {
			t.Fatalf("seq 倒退：%+v 前一条 %d", seq, logs[i-1].Seq)
		}
		if seq.Direction == "command" && seq.Seq <= logs[i-1].Seq {
			t.Fatalf("command 帧 seq 未递增：%+v 前一条 %d", seq, logs[i-1].Seq)
		}
	}
}

// 2) D7 闸门真实挂起：prep 已到线、send 未至线、放行后才提交，且整场 send 恰好一次
func TestWSE2E_D7GateHoldsSendUntilConfirmed(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, happyReply)
	task, session := bundle.seedTask(t, threeStageSteps, true)

	steps, err := ParseSteps([]byte(threeStageSteps))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		exec.ExecuteSession(ctx, task, session, steps)
		close(done)
	}()

	waitConfirmPending(t, exec, session.ID, true)
	if !waitFor(3*time.Second, func() bool { return ext.countOf("comment_prep") >= 1 }) {
		t.Fatal("comment_prep 未到达扩展侧")
	}
	if n := ext.countOf("comment_send"); n != 0 {
		t.Fatalf("挂起期间 comment_send 已到线 %d 次——闸门失效（不可逆动作先于确认）", n)
	}
	if !exec.SignalConfirm(session.ID) {
		t.Fatal("放行未命中挂起点")
	}
	waitConfirmPending(t, exec, session.ID, false)

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("放行后会话未在时限内收敛")
	}
	if n := ext.countOf("comment_send"); n != 1 {
		t.Errorf("comment_send=%d want 1", n)
	}
	got := bundle.reloadSession(t, session.ID)
	if got.Status != "completed" {
		t.Errorf("终态=%s（%s），want completed", got.Status, got.ErrorMsg)
	}
}

// 3) D7 闸门中止即零提交：确认点前手动中断，线上一条 comment_send 都不许出现
// （这是 D7 的立项理由本身——「未确认=无副作用=可安全重下发」，必须能在无真机情况下跑出来）
func TestWSE2E_D7AbortBeforeConfirmNeverSends(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, happyReply)
	task, session := bundle.seedTask(t, threeStageSteps, true)

	steps, err := ParseSteps([]byte(threeStageSteps))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		exec.ExecuteSession(ctx, task, session, steps)
		close(done)
	}()

	waitConfirmPending(t, exec, session.ID, true)
	if !waitFor(3*time.Second, func() bool { return ext.countOf("comment_prep") >= 1 }) {
		t.Fatal("comment_prep 未到达扩展侧")
	}
	if !exec.SignalStop(session.ID) {
		t.Fatal("中断未命中运行中会话")
	}

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("中断后会话未在时限内收敛")
	}
	if n := ext.countOf("comment_send"); n != 0 {
		t.Errorf("中止后 comment_send 到线 %d 次，want 0（评论绝不能已提交）", n)
	}
	got := bundle.reloadSession(t, session.ID)
	// F4：挂起期间被用户 stop = 「操作者主动不提交」，终态必须记 stopped 而非 failed
	// （两者在审计面同形时，一次正常的人工否决会被统计成系统故障）
	if got.Status != "stopped" {
		t.Errorf("终态=%s（%s），want stopped", got.Status, got.ErrorMsg)
	}
	if !strings.Contains(got.ErrorMsg, "评论未提交") {
		t.Errorf("终态归因应说明未提交，got %q", got.ErrorMsg)
	}
	// 中止路径不得留下待确认通道（否则 ConfirmPending 永久误报）
	if exec.ConfirmPending(session.ID) {
		t.Error("中止后确认通道应注销")
	}
	// F10：中止腿是最容易漏回收的路径（编排末尾的 close_tab 步根本执行不到），
	// 收口必须把这一轮开的 tab 关掉——否则 cron 任务每次中止都留一个常驻后台 tab。
	if n := ext.countOf("close_tab"); n != 1 {
		t.Errorf("中止后收口 close_tab 到线 %d 次 want 1", n)
	}
}

// 3b) 编排自带 close_tab 时不得双发：收口回收以「内存 tab id 归零」为准，
// 重复 close_tab 会让扩展侧第二次 remove 报错，把干净收口污染成审计失败行。
func TestWSE2E_ExplicitCloseTabStepIsNotDoubledByCleanup(t *testing.T) {
	const stepsJSON = `[{"action":"open_tab","target":"https://www.xiaohongshu.com/explore"},
{"action":"snapshot"},
{"action":"markdown"},
{"action":"post_comment","value":"测试评论正文"},
{"action":"close_tab"}]`
	exec, ext, bundle := newWSE2E(t, happyReply)
	task, session := bundle.seedTask(t, stepsJSON, false)

	steps, err := ParseSteps([]byte(stepsJSON))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	if n := ext.countOf("close_tab"); n != 1 {
		t.Errorf("close_tab 到线 %d 次 want 1（编排步 + 收口双发）", n)
	}
	if rows := bundle.commandActions(t, session.ID); containsStr(rows, "session_tab_cleanup") {
		t.Errorf("已显式关 tab 仍落收口审计行，actions=%v", rows)
	}
	got := bundle.reloadSession(t, session.ID)
	if got.Status != "completed" {
		t.Errorf("终态=%s（%s），want completed", got.Status, got.ErrorMsg)
	}
	// DB 里的 chrome_tab_id 是审计事实（这一轮用过哪个 tab），不随内存归零而清空
	if got.ChromeTabID != 777 {
		t.Errorf("chrome_tab_id 审计值被清: %d want 777", got.ChromeTabID)
	}
}

// 4) 回包 ok=false 的失败贯通：prep 定位失败（selector_miss token）在无自愈接缝时
// 原样上抛为步失败——验证错误字符串确实穿过 WS 边界（不是被 data 字段吞掉），
// 且写原语步级禁重试在线帧层面成立（comment_prep 只到线一次，F2①）。
func TestWSE2E_HostErrorPropagatesToStep(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, func(action string, _ map[string]any) (map[string]any, string) {
		if action == "comment_prep" {
			return nil, "comment_input_not_found"
		}
		return happyReply(action, nil)
	})
	// 关闭 A1 自愈接缝：本用例测「错误穿线 + 不重发」，自愈回路由 executor_selfheal_test.go 专测
	exec.relocateLLM = nil
	task, session := bundle.seedTask(t, threeStageSteps, false)

	steps, err := ParseSteps([]byte(threeStageSteps))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	got := bundle.reloadSession(t, session.ID)
	if got.Status != "failed" {
		t.Fatalf("终态=%s want failed", got.Status)
	}
	if !strings.Contains(got.ErrorMsg, "comment_input_not_found") {
		t.Errorf("扩展侧错误文本未穿线: %q", got.ErrorMsg)
	}
	list := bundle.steps(t, session.ID)
	if len(list) != 4 || list[3].Status != "failed" {
		t.Fatalf("步落库异常: %+v", list)
	}
	if n := ext.countOf("comment_prep"); n != 1 {
		t.Errorf("comment_prep 到线 %d 次 want 1（写原语禁重试=不重发注入）", n)
	}
	if n := ext.countOf("comment_send"); n != 0 {
		t.Errorf("prep 失败后 comment_send 到线 %d 次 want 0（定位未成功不得提交）", n)
	}
}

//  5. 风控页导致的步失败必须归因成「平台风控拦截」，而不是裸 selector_timeout
//     （真机 session229 实测：xhs 未登录/IP 风控跳 website-login/error?error_code=300012，
//     旧代码在 break loop 前不做拦截检测 → 风控被误报成选择器问题，归因方向整个错掉）
const xhsRiskPageURL = "https://www.xiaohongshu.com/website-login/error?redirectPath=https://www.xiaohongshu.com/explore&error_code=300012"

const riskBlockedSteps = `[{"action":"open_tab","target":"https://www.xiaohongshu.com/explore"},
{"action":"wait_for_selector","selector":"#root","timeout_ms":1000}]`

func TestWSE2E_RiskPageOnFailedStepAttributesAsBlock(t *testing.T) {
	// 第 1 次 snapshot = open_tab 成功后的步后检测（必须是正常页，否则在用例开始前就终止，
	// 测不到「失败收口前」这条新路径）；第 2 次起才是失败步后探测到的风控页。
	var snapCount int
	exec, ext, bundle := newWSE2E(t, func(action string, _ map[string]any) (map[string]any, string) {
		switch action {
		case "open_tab":
			return map[string]any{"chrome_tab_id": 777}, ""
		case "wait_for_selector":
			return nil, "selector_timeout: #root"
		case "snapshot":
			snapCount++
			if snapCount == 1 {
				return map[string]any{"snapshot": xhsNoteSnapshot, "url": "https://www.xiaohongshu.com/explore"}, ""
			}
			// 真机 session235 实测形态：风控页 a11y 采集为空快照，只有 URL 带拦截证据
			return map[string]any{"snapshot": "", "url": xhsRiskPageURL}, ""
		default:
			return happyReply(action, nil)
		}
	})
	exec.relocateLLM = nil
	task, session := bundle.seedTask(t, riskBlockedSteps, false)

	steps, err := ParseSteps([]byte(riskBlockedSteps))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	if n := ext.countOf("wait_for_selector"); n != 1 {
		t.Fatalf("wait_for_selector 到线 %d 次 want 1（=0 说明用例在第一页就被旧检测拦走，没测到失败收口路径）", n)
	}
	got := bundle.reloadSession(t, session.ID)
	if got.Status != "failed" {
		t.Fatalf("终态 %s want failed", got.Status)
	}
	if !strings.Contains(got.ErrorMsg, "平台风控拦截") {
		t.Errorf("错误归因 = %q，want 含「平台风控拦截」（拦截判定必须在步失败收口前跑到）", got.ErrorMsg)
	}
	if strings.Contains(got.ErrorMsg, "selector_timeout") {
		t.Errorf("风控页不得报成 selector_timeout: %q", got.ErrorMsg)
	}
	if n := ext.countOf("snapshot"); n != 2 {
		t.Errorf("snapshot 到线 %d 次 want 2（成功步后 1 次 + 失败收口前 1 次）", n)
	}
}

//  6. 拦截探测必须有独立短预算：探测命令自身不得把「步失败」升级成 A5 Host 假死自愈
//     （真机 session236 实测：open_tab 已耗满 30s 命令超时，失败收口前的探测再叠一条满额
//     30s 命令 → 单步失败 60s+，且连续 2 条超时触发 shutdown 帧断连，把慢页面误判成 Host 假死）
const hangFrame = "\x00__hang__"

func TestWSE2E_BlockDetectProbeIsBudgetCappedAndCannotKillHost(t *testing.T) {
	// 闲鱼是唯一声明 BlockSelectors 的平台：正常页（URL/快照都不命中判据）→ 必然下探弹层选择器，
	// 让 selector 查询假死，正是「探测无上限则吃满 defaultCmdTimeout×N」的形态。
	exec, ext, bundle := newWSE2E(t, func(action string, _ map[string]any) (map[string]any, string) {
		switch action {
		case "snapshot":
			return map[string]any{
				"snapshot": `- heading "闲置好物 九成新"`,
				"url":      "https://www.goofish.com/item?id=123456",
			}, ""
		case "query":
			return nil, hangFrame
		default:
			return happyReply(action, nil)
		}
	})
	task, session := bundle.seedTask(t, `[{"action":"open_tab","target":"https://www.goofish.com/item?id=123456"}]`, false)
	task.Platform = "xianyu"
	session.ChromeTabID = 777

	start := time.Now()
	seq := 0
	blocked, reason := exec.detectBlockedIfFatal(context.Background(), task, session, &seq)
	elapsed := time.Since(start)

	if blocked || reason != "" {
		t.Errorf("探测假死必须 fail-soft（检测是增强不是闸门）：blocked=%v reason=%q", blocked, reason)
	}
	if n := ext.countOf("query"); n == 0 {
		t.Fatalf("query 未到线，用例没走到弹层下探分支（平台=%s）", task.Platform)
	}
	if elapsed > blockDetectBudget+3*time.Second {
		t.Errorf("探测耗时 %v 超预算 %v：整段探测未共用一个 ctx（无预算时每条 query 吃满 30s）", elapsed, blockDetectBudget)
	}
	// 预算内取消走 ctx.Done 分支 → 不计入 cmdTimeouts → 连接不被判死。
	if st := exec.hand.registry.MyStatus(wsE2EUserID); st["online"] != true {
		t.Errorf("探测超时应归零于自身预算，不得触发 Host 判死/自愈：%+v", st)
	}
	// F6：假死探测同样要留审计帧——「检测跑没跑、看了几个选择器、耗多久」必须在审计面可见
	logs, err := bundle.cmdLogRepo.ListBySessionID(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("命令日志回读失败: %v", err)
	}
	var row *model.BrowserCommandLog
	for _, l := range logs {
		if l.Action == "block_detect" {
			row = l
		}
	}
	if row == nil {
		t.Fatal("探测帧未进审计包（无 block_detect 行）")
	}
	var payload map[string]any
	if err := json.Unmarshal(row.Payload, &payload); err != nil {
		t.Fatalf("payload 解析失败: %v", err)
	}
	if payload["selectors_probed"].(float64) < 1 || payload["blocked"] != false {
		t.Errorf("探测审计字段失真：%s", row.Payload)
	}
}

//  7. F3「注册在线」不等于「可服务」：注册探针（零副作用 tab_exists）无回包即提前判病，
//     不等用户任务去撞 2×30s（真机 session237/238/253 三次实测形态）
func TestWSE2E_RegisterProbeWithoutReplySelfHealsEarly(t *testing.T) {
	exec, ext, _ := newWSE2E(t, func(action string, _ map[string]any) (map[string]any, string) {
		if action == "tab_exists" {
			return nil, hangFrame // 应用面有去无回，WS 传输面正常
		}
		return happyReply(action, nil)
	})
	if !waitFor(3*time.Second, func() bool { return ext.countOf("tab_exists") >= 1 }) {
		t.Fatal("注册探针未到线，本用例没走到 F3 分支")
	}
	// 探针预算内无回包 → 服务端下发 shutdown 帧并断开，触发 host 侧重连自愈
	ok := waitFor(hostServProbeTimeout+5*time.Second, func() bool {
		return exec.hand.registry.MyStatus(wsE2EUserID)["online"] != true
	})
	if !ok {
		t.Fatal("注册探针无回包时未提前判死：仍然只能等用户任务烧 2×30s")
	}
}

//  8. F3 反向半：探针回包但带错误（极旧 bundle 不认 tab_exists）也不得判死——
//     「有无回包」才是可服务判据，据此判死会把可用 Host 拖进重启活锁
func TestWSE2E_RegisterProbeErrorReplyStaysOnline(t *testing.T) {
	exec, _, _ := newWSE2E(t, func(action string, _ map[string]any) (map[string]any, string) {
		if action == "tab_exists" {
			return nil, "unknown_action_v4: tab_exists"
		}
		return happyReply(action, nil)
	})
	time.Sleep(hostServProbeTimeout + time.Second)
	st := exec.hand.registry.MyStatus(wsE2EUserID)
	if st["online"] != true {
		t.Fatalf("有回包即证明链路可服务，不得判死：%+v", st)
	}
	if st["servable"] != true {
		t.Errorf("回包应记为可服务证据：%+v", st)
	}
}

func waitConfirmPending(t *testing.T, e *Executor, sessionID uint, want bool) {
	t.Helper()
	ok := waitFor(5*time.Second, func() bool { return e.ConfirmPending(sessionID) == want })
	if !ok {
		t.Fatalf("ConfirmPending 未变为 %v", want)
	}
}

// waitFor 轮询等待条件成立（不引入新依赖，测试内自重）
func waitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}
