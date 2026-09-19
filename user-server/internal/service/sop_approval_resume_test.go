// sop_approval_resume_test.go T-P3-02：审批挂起/恢复的可执行证明。
//
// 三条 AC 各自对应一组用例，且都可判别（把实现改坏，必须有对应用例变红）：
//
//	AC① 不阻塞请求线程        → TestWaitExecutor_ApprovalSuspendNeverBlocks
//	                             （32 个执行并发挂起，TTL 1 小时，整批必须在秒级返回）
//	AC② 进程重启后可续跑      → TestApprovalResume_EndToEnd_*（跨步骤要用的对象一律
//	                             重新构造：桥、清扫器、调度器、执行器都换一遍，等于换进程）
//	AC③ 过期不卡死流程        → TestApprovalResume_EndToEnd_ExpiresWithoutDecision
//
// 测试库口径：testutil.NewTestDB 会先 **DropTable 再 AutoMigrate** 本次列出的模型，
// 所以同一个二进制里的"全表计数"是可信的（上一批数据已被这张表自己的重建清掉）。
package service

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// —— 夹具 ————————————————————————————————————————————————

// approvalResumeFixture 一套带真库的审批运行时：服务 + 桥 + 已挂载的两条唤醒出口。
//
// 全局桥与 notifier 都在 t.Cleanup 归零：二者都是进程级状态，留着会让同一二进制里
// 后面的用例读到一个"以为自己装配了"的桥（本仓 internal/service 的测试共享进程，
// 这类串味已经踩过一次，见 reply_guard 认领表那条）。
func approvalResumeFixture(t *testing.T, policy AutoApprovalPolicy) (*ApprovalRequestService, *ApprovalResumeBridge, *gorm.DB) {
	t.Helper()
	db := testutil.NewTestDB(t,
		&model.ApprovalRequest{},
		&model.SOPExecution{},
		&model.SOPTimer{},
		&model.SOPAgent{},
		&model.SOPExecEvent{},
	)
	svc := NewApprovalRequestService(repository.NewApprovalRequestRepositoryWithDB(db), policy)
	b := NewApprovalResumeBridge(svc, db)
	if b == nil {
		t.Fatal("NewApprovalResumeBridge 传了服务却返回 nil")
	}
	svc.SetWaitNotifier(b)
	SetApprovalResumeBridge(b)
	t.Cleanup(func() {
		SetApprovalResumeBridge(nil)
		svc.SetWaitNotifier(nil)
	})
	return svc, b, db
}

// newSOPExecution 落一行 running 的执行，返回**库里那一行**（含自增 ID）。
func newSOPExecution(t *testing.T, db *gorm.DB, agentID uint, customerID, currentNode string) *model.SOPExecution {
	t.Helper()
	exec := &model.SOPExecution{
		SOPID:         agentID,
		CustomerID:    customerID,
		SessionID:     "sess-" + customerID,
		Status:        SOPStatusRunning,
		CurrentNode:   currentNode,
		StartedAt:     time.Now(),
		ExecutionData: model.JSONMap{},
	}
	if err := db.WithContext(context.Background()).Create(exec).Error; err != nil {
		t.Fatalf("建执行失败: %v", err)
	}
	return exec
}

// approvalWaitNode 一个审批等待节点。ttlSeconds 直接进 approval_ttl_seconds。
func approvalWaitNode(id, policyKey string, ttlSeconds float64) *dto.SOPNode {
	cfg := map[string]any{
		"wait_event":           WaitEventApproval,
		"approval_policy_key":  policyKey,
		"approval_ttl_seconds": ttlSeconds,
	}
	return &dto.SOPNode{ID: id, Type: SOPNodeTypeWait, Next: []string{"gate"}, Config: cfg}
}

func execCtxFor(exec *model.SOPExecution, node *dto.SOPNode) *ExecutionContext {
	return &ExecutionContext{
		Execution:     exec,
		Node:          node,
		CustomerID:    exec.CustomerID,
		SessionID:     exec.SessionID,
		ExecutionData: exec.ExecutionData,
		Input:         exec.ExecutionData,
		TraceID:       "trace-" + exec.CustomerID,
		StartedAt:     time.Now(),
	}
}

func loadTimerByID(t *testing.T, db *gorm.DB, id uint) model.SOPTimer {
	t.Helper()
	// 独立零值 struct：复用已填充的再 First() 会把旧字段并进 WHERE（本仓踩过一次）。
	var fresh model.SOPTimer
	if err := db.First(&fresh, id).Error; err != nil {
		t.Fatalf("重读定时器失败: %v", err)
	}
	return fresh
}

// countRows 按 WHERE 统计一张表。写成一处，免得每个用例各拼一遍链式调用。
func countRows(t *testing.T, db *gorm.DB, dest any, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.Model(dest).Where(query, args...).Count(&n).Error; err != nil {
		t.Fatalf("统计失败（%s）: %v", query, err)
	}
	return n
}

// —— subject 编码（纯函数）————————————————————————————————

func TestApprovalSubjectEncodeDecode(t *testing.T) {
	cases := []struct {
		name     string
		typ      string
		subject  string
		wantID   uint
		wantNode string
		wantOK   bool
	}{
		{"常规", ApprovalSubjectTypeSOPNode, "42--w1", 42, "w1", true},
		{"节点ID含分隔符", ApprovalSubjectTypeSOPNode, "7--a--b--c", 7, "a--b--c", true},
		{"大执行ID", ApprovalSubjectTypeSOPNode, "9007199254740993--n", 9007199254740993, "n", true},
		{"别人的 subject 不叫醒", "quote", "42--w1", 0, "", false},
		{"没有分隔符", ApprovalSubjectTypeSOPNode, "42", 0, "", false},
		{"分隔符后为空", ApprovalSubjectTypeSOPNode, "42--", 0, "", false},
		{"左半段不是数字", ApprovalSubjectTypeSOPNode, "abc--w1", 0, "", false},
		{"负数", ApprovalSubjectTypeSOPNode, "-1--w1", 0, "", false},
		{"空串", "", "", 0, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, node, ok := DecodeApprovalSubject(tc.typ, tc.subject)
			if ok != tc.wantOK || id != tc.wantID || node != tc.wantNode {
				t.Errorf("DecodeApprovalSubject(%q,%q)=(%d,%q,%v)，期望 (%d,%q,%v)",
					tc.typ, tc.subject, id, node, ok, tc.wantID, tc.wantNode, tc.wantOK)
			}
		})
	}

	// 往返：编码再解码必须拿回原值。节点 ID 里带 "--" 也要能原样回来 —— 这是
	// strings.Cut（按首个分隔符切）与 strings.Split 的关键差别，后者会把 ID 劈碎。
	for _, nodeID := range []string{"w1", "a--b", "--", "n:2", "x--y--z"} {
		gotID, gotNode, ok := DecodeApprovalSubject(ApprovalSubjectTypeSOPNode, EncodeApprovalSubject(7, nodeID))
		if !ok || gotID != 7 || gotNode != nodeID {
			t.Errorf("往返失败：node=%q ⇒ (%d,%q,%v)", nodeID, gotID, gotNode, ok)
		}
	}
}

// —— AC①：挂起不阻塞 ————————————————————————————————————

// TestWaitExecutor_ApprovalSuspendNeverBlocks AC① 的那一格证明。
//
// 建执行放在协程**外面**：要计时的是挂起本身，不是建行的开销；放在里面会让
// "耗时超标"这个判据混进与审批无关的噪声。
func TestWaitExecutor_ApprovalSuspendNeverBlocks(t *testing.T) {
	_, _, db := approvalResumeFixture(t, nil)
	ctx := context.Background()
	e := NewWaitExecutor(&SOPNodeExecutorDeps{DB: db})

	const n = 32
	const ttl = 3600.0 // 1 小时：任何"在进程内等条件满足"的写法都会立刻体现在耗时上
	node := approvalWaitNode("w1", "quote.send", ttl)

	ids := make([]uint, n)
	execCtxs := make([]*ExecutionContext, n)
	for i := 0; i < n; i++ {
		exec := newSOPExecution(t, db, 1, "ac1-"+strconv.Itoa(i), "w1")
		ids[i] = exec.ID
		execCtxs[i] = execCtxFor(exec, node)
	}

	type outcome struct {
		result *NodeExecResult
		err    error
		cost   time.Duration
	}
	results := make([]outcome, n)
	var start sync.WaitGroup
	var run sync.WaitGroup
	start.Add(1)
	for i := 0; i < n; i++ {
		run.Add(1)
		go func(i int) {
			defer run.Done()
			start.Wait() // 尽量让 32 个真正同时进入，而不是排着队进
			t0 := time.Now()
			res, err := e.Execute(ctx, execCtxs[i])
			results[i] = outcome{result: res, err: err, cost: time.Since(t0)}
		}(i)
	}
	wall := time.Now()
	start.Done()
	run.Wait()
	elapsed := time.Since(wall)

	var slowest time.Duration
	for i, o := range results {
		if o.err != nil {
			t.Errorf("第 %d 个挂起返回了 error：%v", i, o.err)
		}
		if o.result == nil {
			t.Fatalf("第 %d 个没有结果", i)
		}
		if o.result.Status != NodeStatusWaiting {
			t.Errorf("第 %d 个状态=%s，期望 %s", i, o.result.Status, NodeStatusWaiting)
		}
		if o.result.WaitEvent != WaitEventApproval {
			t.Errorf("第 %d 个 wait_event=%s，期望 %s", i, o.result.WaitEvent, WaitEventApproval)
		}
		// 单次耗时上限 2s：挂起只做两次 INSERT。真等到 1 小时后的路径不可能满足这个界，
		// 一个 sleep(waitSeconds) 的实现也不可能。
		if o.cost > 2*time.Second {
			t.Errorf("第 %d 个挂起耗时 %v ⇒ 有在进程内等的嫌疑（AC①）", i, o.cost)
		}
		if o.cost > slowest {
			slowest = o.cost
		}
	}

	// 每个执行恰好一条审批 + 一枚等待载体：并发下不串台、不重复。
	var wantApprovals, wantTimers int64
	for _, id := range ids {
		wantApprovals += countRows(t, db, &model.ApprovalRequest{},
			"subject_type = ? AND subject_id = ? AND status = ?",
			ApprovalSubjectTypeSOPNode, EncodeApprovalSubject(id, "w1"), model.ApprovalStatusPending)
		wantTimers += countRows(t, db, &model.SOPTimer{},
			"execution_id = ? AND node_id = ? AND wait_event = ? AND status = ?",
			id, "w1", WaitEventApproval, sopTimerStatusPending)
	}
	if wantApprovals != n || wantTimers != n {
		t.Errorf("挂起 %d 个应留下 %d 条审批 + %d 条定时器，实际 (%d,%d)",
			n, n, n, wantApprovals, wantTimers)
	}
	t.Logf("AC①：%d 路并发挂起墙钟 %v，最长单次 %v；库里 %d 条 pending 审批 / %d 条 pending 定时器",
		n, elapsed, slowest, wantApprovals, wantTimers)
}

// —— 挂起的形状：到期时刻同源 ————————————————————————————

func TestWaitExecutor_ApprovalWaitUntilEqualsApprovalExpiry(t *testing.T) {
	_, _, db := approvalResumeFixture(t, nil)
	ctx := context.Background()
	e := NewWaitExecutor(&SOPNodeExecutorDeps{DB: db})

	exec := newSOPExecution(t, db, 1, "ac-source", "w1")
	res, err := e.Execute(ctx, execCtxFor(exec, approvalWaitNode("w1", "quote.send", 600)))
	if err != nil || res == nil || res.Status != NodeStatusWaiting {
		t.Fatalf("挂起失败：%v %+v", err, res)
	}

	var timer model.SOPTimer
	if err := db.Where("execution_id = ? AND node_id = ?", exec.ID, "w1").First(&timer).Error; err != nil {
		t.Fatalf("查定时器失败: %v", err)
	}
	var row model.ApprovalRequest
	if err := db.Where("subject_id = ?", EncodeApprovalSubject(exec.ID, "w1")).First(&row).Error; err != nil {
		t.Fatalf("查审批行失败: %v", err)
	}
	if row.ExpiresAt == nil {
		t.Fatal("前提不成立：pending 的审批行必须自带到期时刻")
	}
	// 同一事实源：定时器的到期时刻就是审批行的 expires_at（精确到微秒；PG 的 timestamptz
	// 精度低于 Go 的 monotonic clock，故比到 µs）。
	if !timer.WaitUntil.Truncate(time.Microsecond).Equal(row.ExpiresAt.Truncate(time.Microsecond)) {
		t.Errorf("到期时刻不同源：timer=%v approval=%v", timer.WaitUntil, *row.ExpiresAt)
	}
	if got := payloadString(timer.Payload, approvalTimerPayloadToken); got != row.ResumeToken {
		t.Errorf("payload 凭证=%q，审批行=%q", got, row.ResumeToken)
	}
	if got := payloadString(timer.Payload, approvalTimerPayloadApprovalID); got != row.ID {
		t.Errorf("payload 审批 ID=%q，审批行=%q", got, row.ID)
	}
	if timer.MaxWaitAt != nil {
		t.Error("审批定时器不该有 max_wait_at：那会给同一次等待第二条到期路径（跳过档不带 payload，读不到结论）")
	}
	if timer.ExpiresAt == nil || !timer.ExpiresAt.Truncate(time.Microsecond).Equal(*row.ExpiresAt) {
		t.Error("expires_at 应与 wait_until 同源，便于库里一眼看出这枚定时器几点放弃")
	}
}

// TestWaitExecutor_ApprovalResuspendReplacesPendingTimer 同一节点重跑不得叠加等待载体。
//
// 重跑是常态（节点重试、进程恢复重投），而两枚 pending 定时器会各点一次火、各派发一次
// 推进任务，调度器又不校验 task.NodeID 与 exec.CurrentNode 是否还一致 ⇒ 同一次等待把
// 流程推进两次。审批档位在自己这条路上先堵住：建新载体之前先删本节点的遗留 pending。
//
// 顺带钉住挂起的幂等面：三次挂起共用**同一条** pending 审批（同一 subject + 同一 policy
// 且未落终态），待办中心里只有一条待办，审批人不会看到三份同样的事。
func TestWaitExecutor_ApprovalResuspendReplacesPendingTimer(t *testing.T) {
	_, _, db := approvalResumeFixture(t, nil)
	ctx := context.Background()
	e := NewWaitExecutor(&SOPNodeExecutorDeps{DB: db})

	exec := newSOPExecution(t, db, 1, "ac-resuspend", "w1")
	node := approvalWaitNode("w1", "quote.send", 600)
	var token string
	for i := 0; i < 3; i++ {
		res, err := e.Execute(ctx, execCtxFor(exec, node))
		if err != nil || res == nil {
			t.Fatalf("第 %d 次挂起失败：%v %+v", i, err, res)
		}
		if res.Status != NodeStatusWaiting {
			t.Fatalf("第 %d 次挂起状态=%s，期望 %s", i, res.Status, NodeStatusWaiting)
		}
		var tm model.SOPTimer
		if err := db.Where("execution_id = ? AND node_id = ? AND status = ?",
			exec.ID, "w1", sopTimerStatusPending).Find(&tm).Error; err != nil {
			t.Fatalf("第 %d 次挂起后查 pending 定时器失败: %v", i, err)
		}
		got := payloadString(tm.Payload, approvalTimerPayloadToken)
		if token == "" {
			token = got
		} else if got != token {
			t.Errorf("第 %d 次挂起换了凭证：%q ≠ %q ⇒ 待办与等待载体的对应关系会漂", i, got, token)
		}
	}
	if n := countRows(t, db, &model.SOPTimer{},
		"execution_id = ? AND node_id = ? AND status = ?", exec.ID, "w1", sopTimerStatusPending); n != 1 {
		t.Errorf("三次挂起留下 %d 枚 pending 定时器，期望 1", n)
	}
	if n := countRows(t, db, &model.ApprovalRequest{},
		"subject_type = ? AND subject_id = ?", ApprovalSubjectTypeSOPNode, EncodeApprovalSubject(exec.ID, "w1")); n != 1 {
		t.Errorf("三次挂起留下 %d 条审批记录，期望 1（幂等键必须收住）", n)
	}
}

// —— C2 的退化形式：策略当场放行 ——————————————————————————

func TestWaitExecutor_ApprovalAutoApproveCompletesInline(t *testing.T) {
	pol := &recordingPolicy{allow: true, reason: "低风险账号"}
	_, _, db := approvalResumeFixture(t, pol)
	ctx := context.Background()
	e := NewWaitExecutor(&SOPNodeExecutorDeps{DB: db})

	exec := newSOPExecution(t, db, 1, "ac-auto", "w1")
	res, err := e.Execute(ctx, execCtxFor(exec, approvalWaitNode("w1", "quote.send", 600)))
	if err != nil {
		t.Fatalf("执行失败：%v", err)
	}
	// C2 的退化形式：策略当场放行 ⇒ 节点直接完成，不产生任何等待载体。
	if res.Status != NodeStatusCompleted {
		t.Fatalf("状态=%s，期望 %s（auto-approve 不该挂起）", res.Status, NodeStatusCompleted)
	}
	if res.Output[ApprovalOutcomeStatusKey] != model.ApprovalStatusApproved {
		t.Errorf("产物状态=%v，期望 approved", res.Output[ApprovalOutcomeStatusKey])
	}
	if res.Output[ApprovalOutcomeAllowedKey] != true {
		t.Error("_approval_allowed 必须是 true：下游分支就靠这一格放行")
	}
	if n := countRows(t, db, &model.SOPTimer{}, "execution_id = ?", exec.ID); n != 0 {
		t.Errorf("auto-approve 留了 %d 枚定时器，期望 0", n)
	}
	var row model.ApprovalRequest
	if err := db.Where("subject_id = ?", EncodeApprovalSubject(exec.ID, "w1")).First(&row).Error; err != nil {
		t.Fatalf("查审批行失败: %v", err)
	}
	if row.ResumeToken != "" {
		t.Error("auto-approve 的记录不该有恢复凭证（没人要凭它续跑）")
	}
	if row.DecidedBy != model.ApprovalDecidedByPolicy {
		t.Errorf("decided_by=%q，期望 %q：自动放行必须与人工裁决分得开", row.DecidedBy, model.ApprovalDecidedByPolicy)
	}
}

// —— 未装配 / 配置不合法：关门，不放行 ————————————————————

func TestWaitExecutor_ApprovalFailsClosedWhenNotWired(t *testing.T) {
	_, _, db := approvalResumeFixture(t, nil)
	SetApprovalResumeBridge(nil) // 刻意撤桥：图里用了审批等待节点，而运行时没装配
	ctx := context.Background()
	e := NewWaitExecutor(&SOPNodeExecutorDeps{DB: db})

	exec := newSOPExecution(t, db, 1, "ac-nowire", "w1")
	res, err := e.Execute(ctx, execCtxFor(exec, approvalWaitNode("w1", "quote.send", 600)))
	if err != nil {
		t.Fatalf("不该返回 error（失败由状态表达）：%v", err)
	}
	if res.Status != NodeStatusFailed {
		t.Fatalf("状态=%s，期望 %s：不能把关的闸门必须把门关上", res.Status, NodeStatusFailed)
	}
	if res.Retryable {
		t.Error("未装配不是瞬时故障，重试只会把一次配置错误放大成一阵风暴")
	}
	if n := countRows(t, db, &model.ApprovalRequest{}, "1=1"); n != 0 {
		t.Errorf("撤桥后不该产生审批行，实际 %d 条", n)
	}
	if n := countRows(t, db, &model.SOPTimer{}, "1=1"); n != 0 {
		t.Errorf("撤桥后不该产生等待载体，实际 %d 条", n)
	}
}

func TestWaitExecutor_ApprovalRejectsBadNodeConfig(t *testing.T) {
	_, _, db := approvalResumeFixture(t, nil)
	ctx := context.Background()
	e := NewWaitExecutor(&SOPNodeExecutorDeps{DB: db})

	// 缺 policy_key：不能默认一个策略名（默认哪个都等于替图作者做了一次授权决定）。
	node := &dto.SOPNode{ID: "w1", Type: SOPNodeTypeWait, Config: map[string]any{"wait_event": WaitEventApproval}}
	res, err := e.Execute(ctx, execCtxFor(newSOPExecution(t, db, 1, "ac-badcfg", "w1"), node))
	if err != nil {
		t.Fatalf("不该返回 error：%v", err)
	}
	if res.Status != NodeStatusFailed {
		t.Errorf("缺 approval_policy_key 应判失败，实际 %s", res.Status)
	}
	if !strings.Contains(res.ErrorMessage, "approval_policy_key") {
		t.Errorf("失败原因应点名缺哪个配置，实际：%q", res.ErrorMessage)
	}

	// TTL 越界由服务的入参校验拒掉（判错而不是夹到上限）。
	badTTL := approvalWaitNode("w2", "quote.send", 40*3600*24)
	exec2 := newSOPExecution(t, db, 1, "ac-ttl", "w2")
	res2, err := e.Execute(ctx, execCtxFor(exec2, badTTL))
	if err != nil {
		t.Fatalf("不该返回 error：%v", err)
	}
	if res2.Status != NodeStatusFailed {
		t.Errorf("TTL 超上限应判失败，实际 %s", res2.Status)
	}
	if !strings.Contains(res2.ErrorMessage, "上限") {
		t.Errorf("失败原因应说清是 TTL 越界，实际：%q", res2.ErrorMessage)
	}
	if n := countRows(t, db, &model.SOPTimer{}, "execution_id = ?", exec2.ID); n != 0 {
		t.Errorf("配置不合法却留下了 %d 枚定时器", n)
	}
}

// —— AC②/AC③：端到端（挂起 → 唤醒 → 按结论分支）—————————————

const (
	e2eNodeApproved = "ok_end"
	e2eNodeRejected = "no_end"
	e2eNodeExpired  = "exp_end"
)

// approvalE2E 一套图 + 执行 + 调度器，并留出"换一套内存对象"的口子（AC②）。
type approvalE2E struct {
	t       *testing.T
	db      *gorm.DB
	svc     *ApprovalRequestService
	bridge  *ApprovalResumeBridge
	agentID uint
}

// newApprovalE2E 建一个真的 SOPAgent（图里 wait→condition→三个 end）。
//
// 图必须**落库**而不是只在内存里拼：调度器点火后自己 loadGraph，
// 那条 loadGraph 走的是生产同一条路径（json.Unmarshal(agent.SOPGraph)）。
func newApprovalE2E(t *testing.T, ttlSeconds float64) *approvalE2E {
	t.Helper()
	svc, bridge, db := approvalResumeFixture(t, nil)

	graph := dto.SOPGraph{
		Name:  "t-p3-02",
		Entry: "start",
		Nodes: []dto.SOPNode{
			{ID: "start", Type: SOPNodeTypeStart, Next: []string{"w1"}},
			{ID: "w1", Type: SOPNodeTypeWait, Next: []string{"gate"}, Config: map[string]any{
				"wait_event":           WaitEventApproval,
				"approval_policy_key":  "quote.send",
				"approval_ttl_seconds": ttlSeconds,
			}},
			{ID: "gate", Type: SOPNodeTypeCondition, Conditions: []dto.SOPConditionBranch{
				{Label: "批了", Condition: "_approval_status eq approved", Next: e2eNodeApproved, Priority: 30},
				{Label: "拒了", Condition: "_approval_status eq rejected", Next: e2eNodeRejected, Priority: 20},
				{Label: "没人理", Condition: "_approval_status eq expired", Next: e2eNodeExpired, Priority: 10},
			}},
			{ID: e2eNodeApproved, Type: SOPNodeTypeEnd},
			{ID: e2eNodeRejected, Type: SOPNodeTypeEnd},
			{ID: e2eNodeExpired, Type: SOPNodeTypeEnd},
		},
	}
	raw, err := json.Marshal(graph)
	if err != nil {
		t.Fatalf("编码图失败: %v", err)
	}
	agent := &model.SOPAgent{
		Name:        "t-p3-02-" + t.Name(),
		Scenario:    "test",
		TriggerType: SOPTriggerManual,
		SOPGraph:    toJSONMapBytes(raw),
		IsActive:    true,
	}
	if err := db.WithContext(context.Background()).Create(agent).Error; err != nil {
		t.Fatalf("建 SOP 失败: %v", err)
	}
	return &approvalE2E{t: t, db: db, svc: svc, bridge: bridge, agentID: agent.ID}
}

// newDispatcher 造一个**全新**的调度器（含执行器注册表）。
//
// AC② 的"重启"就靠它表达：本测试里凡是跨步骤要用的东西，都从库里重读，
// 内存对象每换一次就等于换了一个进程。
//
// registry 必须是**空的**：NewSOPExecutionDispatcher 自己会 RegisterAllNodeExecutors，
// 而 Register 在重复注册时 panic（启动期错误语义）——先注册再传等于自己撞自己。
func (e *approvalE2E) newDispatcher() *SOPExecutionDispatcher {
	return NewSOPExecutionDispatcher(e.db, NewSOPService(e.db, nil), NewNodeExecutorRegistry(), nil)
}

func (e *approvalE2E) loadGraph() *dto.SOPGraph {
	e.t.Helper()
	var agent model.SOPAgent
	if err := e.db.First(&agent, e.agentID).Error; err != nil {
		e.t.Fatalf("重读 SOP 失败: %v", err)
	}
	var g dto.SOPGraph
	if err := json.Unmarshal(mustJSON(agent.SOPGraph), &g); err != nil {
		e.t.Fatalf("解析图失败: %v", err)
	}
	return &g
}

func (e *approvalE2E) node(id string) *dto.SOPNode {
	e.t.Helper()
	return findNodeByID(e.loadGraph(), id)
}

// suspend 用 WaitExecutor 把执行停在审批等待节点上（等价于 worker 跑到该节点的那一刻）。
func (e *approvalE2E) suspend(customerID string) *model.SOPExecution {
	e.t.Helper()
	exec := newSOPExecution(e.t, e.db, e.agentID, customerID, "w1")
	res, err := NewWaitExecutor(&SOPNodeExecutorDeps{DB: e.db}).Execute(context.Background(), execCtxFor(exec, e.node("w1")))
	if err != nil || res == nil {
		e.t.Fatalf("挂起失败：%v %+v", err, res)
	}
	if res.Status != NodeStatusWaiting {
		e.t.Fatalf("期望挂起，实际状态 %s", res.Status)
	}
	return exec
}

// fireOnce 跑一轮"轮询点火 → 派发 → worker 处理 → 排空后续任务"。
//
// 这就是 AC② 的那一段：裁决发生在上一批内存对象还活着的时候，而续跑由**传进来的这一批**
// （可以是刚 new 出来的）对象完成。
//
// 为什么要 drain：wait 节点点火后 handleNodeSuccess 只是把 condition 节点的任务投进队列，
// 而本测试的调度器没 Start()（没有 worker 协程）。不排空就只能证明"等待被满足"，
// 证明不了"按结论分支"—— 后者才是卡面要的。
func (e *approvalE2E) fireOnce(disp *SOPExecutionDispatcher) {
	e.t.Helper()
	outbox := NewSOPOutboxDispatcher(e.db, senderFunc(func(task *dispatchTask) {
		disp.processTask(context.Background(), 0, task)
	}))
	outbox.processDueTimers(context.Background())
	e.drain(disp)
}

func (e *approvalE2E) drain(disp *SOPExecutionDispatcher) {
	e.t.Helper()
	for i := 0; i < 32; i++ {
		select {
		case task := <-disp.dispatchQueue:
			disp.processTask(context.Background(), 0, task)
		default:
			return
		}
	}
	e.t.Fatalf("派发队列在 %d 轮后排空不掉：图里有环或任务在自我复制", 32)
}

type senderFunc func(task *dispatchTask)

func (f senderFunc) DispatchOrLog(task *dispatchTask) { f(task) }

func (e *approvalE2E) currentExecution(id uint) *model.SOPExecution {
	e.t.Helper()
	var fresh model.SOPExecution
	if err := e.db.First(&fresh, id).Error; err != nil {
		e.t.Fatalf("重读执行失败: %v", err)
	}
	return &fresh
}

func (e *approvalE2E) pendingApprovalFor(execID uint, nodeID string) *model.ApprovalRequest {
	e.t.Helper()
	row, err := e.svc.repo.GetPendingBySubject(context.Background(),
		ApprovalSubjectTypeSOPNode, EncodeApprovalSubject(execID, nodeID), "quote.send")
	if err != nil || row == nil {
		e.t.Fatalf("查 pending 审批失败: (%v,%v)", row, err)
	}
	return row
}

func (e *approvalE2E) decide(row *model.ApprovalRequest, verdict ApprovalVerdict) *model.ApprovalRequest {
	e.t.Helper()
	decided, err := e.svc.Decide(context.Background(), row.ID, verdict, "ops-lead", "case-by-case")
	if err != nil {
		e.t.Fatalf("裁决失败: %v", err)
	}
	return decided
}

func (e *approvalE2E) pendingTimerID(execID uint) uint {
	e.t.Helper()
	var timer model.SOPTimer
	if err := e.db.Where("execution_id = ? AND node_id = ? AND status = ?", execID, "w1", sopTimerStatusPending).
		First(&timer).Error; err != nil {
		e.t.Fatalf("查 pending 定时器失败: %v", err)
	}
	return timer.ID
}

// assertBranch 执行停在期望的分支终点，且流程读到的结论就是期望的那个。
func (e *approvalE2E) assertBranch(execID uint, wantNode, wantStatus string) {
	e.t.Helper()
	fresh := e.currentExecution(execID)
	if fresh.CurrentNode != wantNode {
		e.t.Fatalf("停在 %s，期望分支 %s（ExecutionData=%v）", fresh.CurrentNode, wantNode, fresh.ExecutionData)
	}
	if got, _ := fresh.ExecutionData[ApprovalOutcomeStatusKey].(string); got != wantStatus {
		e.t.Errorf("ExecutionData 的 %s=%q，期望 %q", ApprovalOutcomeStatusKey, got, wantStatus)
	}
	if allowed, _ := fresh.ExecutionData[ApprovalOutcomeAllowedKey].(bool); allowed && wantStatus != model.ApprovalStatusApproved {
		e.t.Errorf("%s=true 而状态是 %s ⇒ 闸门被写歪", ApprovalOutcomeAllowedKey, wantStatus)
	}
}

func TestApprovalResume_EndToEnd_Approved(t *testing.T) {
	e := newApprovalE2E(t, 600)
	exec := e.suspend("e2e-approved")
	row := e.pendingApprovalFor(exec.ID, "w1")
	timerID := e.pendingTimerID(exec.ID)

	e.decide(row, ApprovalApprove)

	// 裁决之后立刻把内存换掉：证明"叫醒"这件事不依赖裁决那一刻活着的东西。
	e.fireOnce(e.newDispatcher())
	e.assertBranch(exec.ID, e2eNodeApproved, model.ApprovalStatusApproved)

	// 等待载体必须已被消费掉，否则同一枚会被再点一次（重复推进）。
	if got := loadTimerByID(t, e.db, timerID); got.Status != "fired" {
		t.Errorf("定时器状态=%s，期望 %s", got.Status, "fired")
	}
	// 恢复凭证要留在执行数据里（卡面："流程把它写进自己的 checkpoint"）。
	if got, _ := e.currentExecution(exec.ID).ExecutionData[ApprovalOutcomeTokenKey].(string); got != row.ResumeToken {
		t.Errorf("checkpoint 里的凭证=%q，期望 %q", got, row.ResumeToken)
	}
}

func TestApprovalResume_EndToEnd_Rejected(t *testing.T) {
	e := newApprovalE2E(t, 600)
	exec := e.suspend("e2e-rejected")
	row := e.pendingApprovalFor(exec.ID, "w1")

	e.decide(row, ApprovalReject)
	e.fireOnce(e.newDispatcher())
	e.assertBranch(exec.ID, e2eNodeRejected, model.ApprovalStatusRejected)
}

// AC③：过期未裁决必须走 expired 分支，且**不卡死**。
//
// 这里用 TTL=1s 的真实时钟，而不是把时间冻住：定时器与审批行共用同一个到期时刻，
// 只有真等过去才能证明"到期这一刻读得到 expired"。清扫器刻意不在这里出现 ——
// 流程侧的到期判据不能依赖它（见 approval_sweep.go 文件头）。
func TestApprovalResume_EndToEnd_ExpiresWithoutDecision(t *testing.T) {
	e := newApprovalE2E(t, 1)
	exec := e.suspend("e2e-expired")
	row := e.pendingApprovalFor(exec.ID, "w1")
	timerID := e.pendingTimerID(exec.ID)

	time.Sleep(1400 * time.Millisecond) // 真等到过期之后（1s TTL + 余量）

	// 前提：没人裁决、也没跑清扫 ⇒ 库里那一行仍写着 pending，流程必须自己推导。
	if got := loadApprovalByID(t, e.db, row.ID); got.Status != model.ApprovalStatusPending {
		t.Fatalf("前提不成立：这一行应仍是 pending，实际 %s", got.Status)
	}

	e.fireOnce(e.newDispatcher())

	fresh := e.currentExecution(exec.ID)
	if got, _ := fresh.ExecutionData[ApprovalOutcomeStatusKey].(string); got != model.ApprovalStatusExpired {
		t.Fatalf("到期后流程读到 %q，期望 %s：库里那一行还是 pending，判据必须自己推导",
			got, model.ApprovalStatusExpired)
	}
	if fresh.ExecutionData[ApprovalOutcomeDerivedKey] != "ttl" {
		t.Errorf("%s=%v，期望 ttl（要能与库里已落库的 expired 分开）",
			ApprovalOutcomeDerivedKey, fresh.ExecutionData[ApprovalOutcomeDerivedKey])
	}
	if allowed, _ := fresh.ExecutionData[ApprovalOutcomeAllowedKey].(bool); allowed {
		t.Error("过期绝不能被读成放行")
	}
	// "不卡死"的可见形式：执行已离开等待节点、走到 expired 分支的终点、状态正常收口。
	if fresh.CurrentNode == "w1" {
		t.Errorf("流程仍停在等待节点，状态=%s：到期路径把流程吊死了", fresh.Status)
	}
	if fresh.CurrentNode != e2eNodeExpired {
		t.Fatalf("停在 %s，期望 %s（ExecutionData=%v）", fresh.CurrentNode, e2eNodeExpired, fresh.ExecutionData)
	}
	if fresh.Status != SOPStatusSuccess {
		t.Errorf("执行状态=%s，期望 %s：走完分支终点应正常收口，而不是挂在 running", fresh.Status, SOPStatusSuccess)
	}
	if got := loadTimerByID(t, e.db, timerID); got.Status != "fired" {
		t.Errorf("到期定时器状态=%s，期望 %s", got.Status, "fired")
	}
	t.Logf("AC③：到期后执行停在 %s、状态 %s，产物 %v", fresh.CurrentNode, fresh.Status, fresh.ExecutionData)

	// 另一侧：清扫器把**库里那一行**收口（待办中心据此不再显示）。
	report := NewApprovalSweepWorker(e.svc, time.Minute).RunOnce(context.Background())
	if report.Err != "" {
		t.Fatalf("清扫失败：%s", report.Err)
	}
	if report.Expired == 0 {
		t.Error("到期一行都没翻，清扫没起作用")
	}
	if got := loadApprovalByID(t, e.db, row.ID); got.Status != model.ApprovalStatusExpired {
		t.Errorf("清扫后库里仍是 %s", got.Status)
	}
}

func loadApprovalByID(t *testing.T, db *gorm.DB, id string) model.ApprovalRequest {
	t.Helper()
	var fresh model.ApprovalRequest
	if err := db.First(&fresh, "id = ?", id).Error; err != nil {
		t.Fatalf("重读审批行失败: %v", err)
	}
	return fresh
}

// —— 推送侧（NotifyDecided）——————————————————————————————

func TestNotifyDecided_OnlyWakesMatchingToken(t *testing.T) {
	_, b, db := approvalResumeFixture(t, nil)
	ctx := context.Background()

	exec := newSOPExecution(t, db, 1, "wake-match", "w1")
	future := time.Now().Add(2 * time.Hour)

	// 同一执行、同一节点上的多枚等待载体：只有凭证对得上的那枚该被提前。
	own := &model.SOPTimer{ExecutionID: exec.ID, NodeID: "w1", WaitEvent: WaitEventApproval,
		WaitUntil: future, Status: sopTimerStatusPending, Payload: model.JSONMap{approvalTimerPayloadToken: "tok-own"}}
	stranger := &model.SOPTimer{ExecutionID: exec.ID, NodeID: "w1", WaitEvent: WaitEventApproval,
		WaitUntil: future, Status: sopTimerStatusPending, Payload: model.JSONMap{approvalTimerPayloadToken: "tok-other"}}
	plain := &model.SOPTimer{ExecutionID: exec.ID, NodeID: "w1", WaitEvent: WaitEventTimer,
		WaitUntil: future, Status: sopTimerStatusPending}
	for _, tm := range []*model.SOPTimer{own, stranger, plain} {
		if err := db.Create(tm).Error; err != nil {
			t.Fatalf("建定时器失败: %v", err)
		}
	}
	row := &model.ApprovalRequest{ID: "apr-wake", SubjectType: ApprovalSubjectTypeSOPNode,
		SubjectID: EncodeApprovalSubject(exec.ID, "w1"), PolicyKey: "quote.send",
		Status: model.ApprovalStatusApproved, ResumeToken: "tok-own", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("建审批行失败: %v", err)
	}

	b.NotifyDecided(ctx, row)

	if got := loadTimerByID(t, db, own.ID); !got.WaitUntil.Before(future) {
		t.Errorf("凭证相符的定时器没被提前：wait_until=%v", got.WaitUntil)
	}
	if got := loadTimerByID(t, db, stranger.ID); !got.WaitUntil.Equal(future) {
		t.Errorf("凭证不相符的定时器被误提前：%v", got.WaitUntil)
	}
	if got := loadTimerByID(t, db, plain.ID); !got.WaitUntil.Equal(future) {
		t.Errorf("非审批等待载体被误提前：%v", got.WaitUntil)
	}
	// 推送只改时刻、**不自己点火**：点火的唯一入口是轮询器（那里有 CAS 抢占与锁）。
	if got := loadTimerByID(t, db, own.ID); got.Status != sopTimerStatusPending {
		t.Errorf("推送后定时器状态=%s，期望保持 %s", got.Status, sopTimerStatusPending)
	}

	// 重复通知不再改写（MarkDueNow 的 wait_until > now 守卫的存在理由）。
	before := loadTimerByID(t, db, own.ID).WaitUntil
	b.NotifyDecided(ctx, row)
	if after := loadTimerByID(t, db, own.ID).WaitUntil; !after.Equal(before) {
		t.Errorf("重复通知把时刻又改了一次：%v → %v", before, after)
	}
}

func TestNotifyDecided_IgnoresUnrelatedAndNilInputs(t *testing.T) {
	_, b, db := approvalResumeFixture(t, nil)
	ctx := context.Background()

	// 别的 subject_type（报价审批由 T-P3-07 接线）不该被本桥反查。
	b.NotifyDecided(ctx, &model.ApprovalRequest{ID: "apr-q", SubjectType: "quote", SubjectID: "q-1",
		Status: model.ApprovalStatusApproved, ResumeToken: "tok-q"})
	// 没有凭证（auto-approve 的记录）也没有等待者。
	b.NotifyDecided(ctx, &model.ApprovalRequest{ID: "apr-x", SubjectType: ApprovalSubjectTypeSOPNode,
		SubjectID: "9--w1", Status: model.ApprovalStatusApproved})
	// nil 输入与 nil 接收者都不能 panic。
	b.NotifyDecided(ctx, nil)
	var nilBridge *ApprovalResumeBridge
	nilBridge.NotifyDecided(ctx, &model.ApprovalRequest{ID: "apr-n", SubjectType: ApprovalSubjectTypeSOPNode, SubjectID: "9--w1"})

	if n := countRows(t, db, &model.SOPTimer{}, "1=1"); n != 0 {
		t.Errorf("以上路径不该碰任何定时器，实际库里有 %d 行", n)
	}
}

// —— 回读侧（ResolveOnFire）的失败方向 ————————————————

func TestResolveOnFire_FailClosed(t *testing.T) {
	_, b, db := approvalResumeFixture(t, nil)
	ctx := context.Background()

	cases := []struct {
		name    string
		task    *dispatchTask
		wantNil bool
	}{
		{"非审批等待不插手", &dispatchTask{TimerFired: true, WaitEvent: WaitEventTimer}, true},
		{"payload 没凭证", &dispatchTask{TimerFired: true, WaitEvent: WaitEventApproval, WaitPayload: model.JSONMap{}}, false},
		{"凭证指向不存在的行", &dispatchTask{TimerFired: true, WaitEvent: WaitEventApproval,
			WaitPayload: model.JSONMap{approvalTimerPayloadToken: "tok-nope"}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := b.ResolveOnFire(ctx, tc.task)
			if tc.wantNil {
				// 返回 nil ⇒ 调度器那一行等价于改动前（非审批档零影响）。
				if out != nil {
					t.Fatalf("非审批等待必须返回 nil（等价于改动前），实际 %v", out)
				}
				return
			}
			if out == nil {
				t.Fatal("审批等待必须给出结论，哪怕是未获批准")
			}
			if out[ApprovalOutcomeStatusKey] != approvalOutcomeUnreadable {
				t.Errorf("状态=%v，期望 %s", out[ApprovalOutcomeStatusKey], approvalOutcomeUnreadable)
			}
			if allowed, _ := out[ApprovalOutcomeAllowedKey].(bool); allowed {
				t.Error("读不出结论时绝不能放行（fail-closed）")
			}
			if out[ApprovalOutcomeErrorKey] == "" {
				t.Error("必须留下原因键，否则排查时无从下手")
			}
		})
	}

	// 撤桥之后点火：同样按未获批准推进，不 panic。
	var nilBridge *ApprovalResumeBridge
	out := nilBridge.ResolveOnFire(ctx, &dispatchTask{TimerFired: true, WaitEvent: WaitEventApproval,
		WaitPayload: model.JSONMap{approvalTimerPayloadToken: "tok"}})
	if out == nil {
		t.Fatal("nil 桥也要给出结论")
	}
	if allowed, _ := out[ApprovalOutcomeAllowedKey].(bool); allowed {
		t.Errorf("nil 桥应给出未获批准的结论，实际 %v", out)
	}
	if out[ApprovalOutcomeErrorKey] != "bridge_not_wired" {
		t.Errorf("原因=%v，期望 bridge_not_wired", out[ApprovalOutcomeErrorKey])
	}
	if n := countRows(t, db, &model.SOPTimer{}, "1=1"); n != 0 {
		t.Errorf("回读路径不该写定时器，实际 %d 行", n)
	}
}

// TestResolveOnFire_TerminalRowWinsOverResumabilityError 钉住"读到了结论"与
// "现在能不能恢复"是两件事。
//
// ByResumeToken 对 rejected/expired 行返回 (行, false, ErrApprovalResumeNotPending)。
// 若回读侧按 err 优先判失败，一次正常的**人工拒绝**就会在流程里读成 unreadable：
// 分支条件 `_approval_status eq rejected` 永不命中，流程被推到默认分支上，
// 而库里、日志里一切都"看起来正常"。
func TestResolveOnFire_TerminalRowWinsOverResumabilityError(t *testing.T) {
	svc, b, db := approvalResumeFixture(t, nil)
	ctx := context.Background()

	for _, verdict := range []ApprovalVerdict{ApprovalReject, ApprovalApprove} {
		exec := newSOPExecution(t, db, 1, "terminal-"+string(verdict), "w1")
		row, _, err := svc.Submit(ctx, ApprovalSubmitInput{SubjectType: ApprovalSubjectTypeSOPNode,
			SubjectID: EncodeApprovalSubject(exec.ID, "w1"), PolicyKey: "quote.send", TTL: time.Hour})
		if err != nil {
			t.Fatalf("入队失败: %v", err)
		}
		decided, err := svc.Decide(ctx, row.ID, verdict, "ops-lead", "x")
		if err != nil {
			t.Fatalf("裁决失败: %v", err)
		}
		out := b.ResolveOnFire(ctx, &dispatchTask{TimerFired: true, WaitEvent: WaitEventApproval,
			WaitPayload: model.JSONMap{approvalTimerPayloadToken: row.ResumeToken}})
		if out == nil {
			t.Fatalf("verdict=%s 时没有给出结论", verdict)
		}
		if got := out[ApprovalOutcomeStatusKey]; got != string(verdict) {
			t.Errorf("verdict=%s 回读到 %v（%v）：终态行必须原样送达，不能读成 unreadable",
				verdict, got, out)
		}
		if out[ApprovalOutcomeErrorKey] != nil {
			t.Errorf("正常回读不该带原因键：%v", out[ApprovalOutcomeErrorKey])
		}
		if out[ApprovalOutcomeIDKey] != decided.ID {
			t.Errorf("产物里的审批 ID=%v，期望 %s", out[ApprovalOutcomeIDKey], decided.ID)
		}
	}
}

// —— 派发任务必须捎带定时器信息 ——————————————————————————

func TestOutboxFiredTaskCarriesWaitEventAndPayload(t *testing.T) {
	db := testutil.NewTestDB(t, &model.SOPExecution{}, &model.SOPTimer{}, &model.SOPExecEvent{})
	exec := newSOPExecution(t, db, 1, "carry", "w1")
	tm := &model.SOPTimer{ExecutionID: exec.ID, NodeID: "w1", WaitEvent: WaitEventApproval,
		WaitUntil: time.Now().Add(-time.Minute), Status: sopTimerStatusPending,
		Payload: model.JSONMap{approvalTimerPayloadToken: "tok-carry"}}
	if err := db.Create(tm).Error; err != nil {
		t.Fatalf("建定时器失败: %v", err)
	}

	var got *dispatchTask
	outbox := NewSOPOutboxDispatcher(db, senderFunc(func(task *dispatchTask) { got = task }))
	outbox.processDueTimers(context.Background())

	if got == nil {
		t.Fatal("到期的定时器没有被派发")
	}
	if got.WaitEvent != WaitEventApproval {
		t.Errorf("任务没捎带 wait_event：%q", got.WaitEvent)
	}
	if payloadString(got.WaitPayload, approvalTimerPayloadToken) != "tok-carry" {
		t.Errorf("任务没捎带 payload：%v", got.WaitPayload)
	}
	if !got.TimerFired {
		t.Error("点火任务必须带 TimerFired，否则 wait 节点会被重新执行（审计R49 的死循环）")
	}
}

// —— 清扫 worker（收口 T-P3-01 的"pending 不会自动过期"）——

func TestApprovalSweepWorker_ExpireOverdueHasACaller(t *testing.T) {
	svc, _, db := approvalResumeFixture(t, nil)
	ctx := context.Background()

	overdue := make([]string, 3)
	for i := 0; i < 3; i++ {
		row, _, err := svc.Submit(ctx, ApprovalSubmitInput{SubjectType: ApprovalSubjectTypeSOPNode,
			SubjectID: "sweep-" + strconv.Itoa(i), PolicyKey: "quote.send", TTL: time.Hour})
		if err != nil {
			t.Fatalf("入队失败: %v", err)
		}
		overdue[i] = row.ID
	}
	// 未到点的那条必须不动：提前过期 = 把还在等人的审批自己判死。
	keep, _, err := svc.Submit(ctx, ApprovalSubmitInput{SubjectType: ApprovalSubjectTypeSOPNode,
		SubjectID: "sweep-keep", PolicyKey: "quote.send", TTL: 24 * time.Hour})
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	if err := db.Model(&model.ApprovalRequest{}).Where("id IN ?", overdue).
		Update("expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatalf("造到期数据失败: %v", err)
	}

	w := NewApprovalSweepWorker(svc, time.Minute)
	if w.Running() {
		t.Error("未 Start 不该在跑")
	}
	report := w.RunOnce(ctx)
	if report.Err != "" {
		t.Fatalf("清扫失败: %s", report.Err)
	}
	if report.Expired != 3 {
		t.Errorf("本轮过期=%d，期望 3", report.Expired)
	}
	if w.Rounds() != 1 {
		t.Errorf("轮次=%d，期望 1", w.Rounds())
	}
	for _, id := range overdue {
		if got := loadApprovalByID(t, db, id); got.Status != model.ApprovalStatusExpired {
			t.Errorf("清扫后 %s 仍是 %s", id, got.Status)
		}
	}
	if got := loadApprovalByID(t, db, keep.ID); got.Status != model.ApprovalStatusPending {
		t.Errorf("未到点那条被翻了：%s", got.Status)
	}

	// 再跑一轮：已经翻完的不会被重复计数（终态不可复活的可见形式）。
	if second := w.RunOnce(ctx); second.Expired != 0 {
		t.Errorf("第二轮又翻了 %d 条", second.Expired)
	}
	if w.ExpiredTotal() != 3 {
		t.Errorf("累计=%d，期望 3", w.ExpiredTotal())
	}

	// Start/Stop 幂等且不留协程：Stop 之后 Running 必须翻回来。
	w.Start(ctx)
	if !w.Running() {
		t.Error("Start 后仍报未运行")
	}
	w.Start(ctx)
	w.Stop(ctx)
	w.Stop(ctx)
	if w.Running() {
		t.Error("Stop 后仍在跑")
	}
	var nilWorker *ApprovalSweepWorker
	nilWorker.Start(ctx)
	nilWorker.Stop(ctx)
	if r := nilWorker.RunOnce(ctx); r == nil || r.Err == "" {
		t.Error("nil worker 也要给出可诊断的结果")
	}
}
