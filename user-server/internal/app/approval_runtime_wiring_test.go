// approval_runtime_wiring_test.go T-P3-02：异步审批运行时的装配层实跑。
//
// 与 T-P0-07 / T-P1-07 / T-P2-06 同一教训：只测 service 逻辑测不到装配层 ——
// approval_requests 这张表在开工前的实测形状就是"服务写得完整、生产构造点为 0"。
// 所以本文件断言的对象是"调过 InitApprovalRuntime 之后，进程里到底多了什么、少了什么"，
// 而且尽量走**生产同一条通路**（不手工 SetWaitNotifier、不手工 SetApprovalResumeBridge）。
package app

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"gorm.io/gorm"
)

// approvalRuntimeTestDB 建审批运行时要碰的四张表。
//
// 不接全局 DB 句柄：InitApprovalRuntime 收的是显式 *gorm.DB，把全局指过去反而会把
// 同二进制里其他用例的句柄换掉（本包已踩过 SetTestDB 共享这条坑）。
func approvalRuntimeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t,
		&model.ApprovalRequest{},
		&model.SOPTimer{},
		&model.SOPExecution{},
		&model.SOPAgent{},
	)
}

// installApprovalRuntimeCleanup 每份用例结束都停运行时：Init 会留一个在跑的清扫协程，
// 不清就会串到同包后续用例（以及它们对全局桥的断言）上。
func installApprovalRuntimeCleanup(t *testing.T) {
	t.Helper()
	t.Cleanup(StopApprovalRuntime)
	t.Cleanup(func() { service.SetApprovalResumeBridge(nil) })
}

func TestParseApprovalRuntimeMode(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"", "off"},
		{"off", "off"},
		{"false", "off"},
		{"0", "off"},
		{"no", "off"},
		{"none", "off"},
		{"shadow", "shadow"},
		{"observe", "shadow"},
		{"sweep", "shadow"},
		{"  SHADOW  ", "shadow"},
		{"on", "on"},
		{"enforce", "on"},
		{"active", "on"},
		{"resume", "on"},
		{"banana", "off"},
		{"  ", "off"},
	}
	for _, c := range cases {
		if got := parseApprovalRuntimeMode(c.raw); string(got) != c.want {
			t.Errorf("parseApprovalRuntimeMode(%q)=%s，期望 %s", c.raw, got, c.want)
		}
	}
}

// 真值只到 shadow：`FF_LTC_APPROVAL_RESUME=true` 不能让流程停下来等人工。
// 这一条单独立用例，因为它是"按习惯写 true 就改客户可感知行为"这个事故的唯一下防线。
func TestParseApprovalRuntimeMode_BoolTruthDoesNotReachOn(t *testing.T) {
	for _, raw := range []string{"true", "True", "1", "yes", "y", "on-ish"} {
		if got := parseApprovalRuntimeMode(raw); got != approvalRuntimeShadow && got != approvalRuntimeOff {
			t.Errorf("%q 被判成 %s：布尔真值绝不能直达 on", raw, got)
		}
	}
}

// AC（off 侧）：关旗时什么都不装，且撤旗要把上一装的清干净。
func TestInitApprovalRuntime_OffDoesNotAssemble(t *testing.T) {
	installApprovalRuntimeCleanup(t)
	database := approvalRuntimeTestDB(t)
	ctx := context.Background()

	// 先装一份 on 的，再切 off：off 分支必须把上一轮的桥与协程都收走。
	t.Setenv(ApprovalResumeFlagEnv, "on")
	if rt := InitApprovalRuntime(database); rt == nil || rt.Mode() != "on" {
		t.Fatalf("前置：on 档没装上（%+v）", rt)
	}
	if service.GetApprovalResumeBridge() == nil {
		t.Fatal("前置：on 档之后全局桥却是空的")
	}

	t.Setenv(ApprovalResumeFlagEnv, "off")
	if rt := InitApprovalRuntime(database); rt != nil {
		t.Errorf("off 档不该返回已装配的运行时，实际 %+v", rt)
	}
	if b := service.GetApprovalResumeBridge(); b != nil {
		t.Error("off 档必须撤桥：留着上一轮的桥等于旗子关了但闸门还拦着")
	}
	// 撤桥之后 wait 节点判失败而不是挂起：这条路径不能停在"没人管的等待"上。
	res := executeApprovalWaitNode(t, database, ctx)
	if res.Status != service.NodeStatusFailed {
		t.Errorf("撤桥后状态=%s，期望 %s", res.Status, service.NodeStatusFailed)
	}
}

func TestInitApprovalRuntime_NilDBDoesNotAssemble(t *testing.T) {
	installApprovalRuntimeCleanup(t)
	t.Setenv(ApprovalResumeFlagEnv, "on")
	if rt := InitApprovalRuntime(nil); rt != nil {
		t.Errorf("没有 DB 句柄却装上了运行时：%+v", rt)
	}
	if b := service.GetApprovalResumeBridge(); b != nil {
		t.Error("无库时不得挂桥：挂上去只会让流程挂在一个永远读不到结论的地方")
	}
}

// AC（shadow 侧，本卡的核心风险边界）：shadow 只装清扫，**没有任何流程会因此挂起**。
func TestInitApprovalRuntime_ShadowSweepsButNeverSuspends(t *testing.T) {
	installApprovalRuntimeCleanup(t)
	database := approvalRuntimeTestDB(t)
	ctx := context.Background()
	t.Setenv(ApprovalResumeFlagEnv, "shadow")

	rt := InitApprovalRuntime(database)
	if rt == nil {
		t.Fatal("shadow 档应装配（至少装清扫器）")
	}
	if rt.Mode() != "shadow" {
		t.Errorf("生效档位=%s，期望 shadow", rt.Mode())
	}
	if rt.sweeper == nil || !rt.sweeper.Running() {
		t.Error("shadow 档的清扫器必须在跑：这一档装的就是它")
	}
	if rt.bridge != nil {
		t.Error("shadow 档不得装桥：装了就等于让流程停下来等人工")
	}
	if b := service.GetApprovalResumeBridge(); b != nil {
		t.Error("shadow 档全局桥必须为空（WaitExecutor 读的就是这个全局）")
	}
	// 图里写 approval 的 wait 节点，在 shadow 档下的真实后果：判失败、且不留等待载体。
	res := executeApprovalWaitNode(t, database, ctx)
	if res.Status != service.NodeStatusFailed {
		t.Errorf("shadow 档下审批节点状态=%s，期望 %s", res.Status, service.NodeStatusFailed)
	}
	var timers int64
	database.Model(&model.SOPTimer{}).Count(&timers)
	if timers != 0 {
		t.Errorf("shadow 档留下了 %d 枚等待载体 ⇒ 有流程被挂起", timers)
	}
}

// AC（on 侧）+ AC②的装配面：装上之后，一次人工裁决经**生产通路**真的叫醒等待中的流程。
//
// 这里不手工调 SetWaitNotifier / SetApprovalResumeBridge：那两个调用点归装配层管，
// 本用例要证明的恰恰是"装配层把它们接上了"。所以路径是
// InitApprovalRuntime → Submit（服务自己入队）→ Decide（服务自己裁决）→ 定时器被提前。
func TestInitApprovalRuntime_OnWiresWakePath(t *testing.T) {
	installApprovalRuntimeCleanup(t)
	database := approvalRuntimeTestDB(t)
	ctx := context.Background()
	t.Setenv(ApprovalResumeFlagEnv, "on")

	rt := InitApprovalRuntime(database)
	if rt == nil || rt.bridge == nil {
		t.Fatalf("on 档应装桥：%+v", rt)
	}
	if b := service.GetApprovalResumeBridge(); b == nil {
		t.Fatal("on 档之后全局桥仍为空 ⇒ WaitExecutor 不会走审批档")
	}
	if rt.sweeper == nil || !rt.sweeper.Running() {
		t.Error("on 档的清扫器必须在跑")
	}

	// 一枚"某个流程正在等这次裁决"的定时器（生产里由 ExecuteApprovalWait 写入；
	// 这里直接建行，是为了把断言对象限定在"唤醒"这一件事上，不掺入挂起路径）。
	const execID = uint(4242)
	future := time.Now().Add(2 * time.Hour)
	token := "rt-wired-token"
	tm := &model.SOPTimer{
		ExecutionID: execID, NodeID: "w1", WaitEvent: service.WaitEventApproval,
		WaitUntil: future, Status: "pending",
		Payload: model.JSONMap{"resume_token": token},
	}
	if err := repository.NewSOPTimerRepository(database).Create(ctx, tm); err != nil {
		t.Fatalf("建等待载体失败: %v", err)
	}
	row, _, err := rt.svc.Submit(ctx, service.ApprovalSubmitInput{
		SubjectType: service.ApprovalSubjectTypeSOPNode,
		SubjectID:   service.EncodeApprovalSubject(execID, "w1"),
		PolicyKey:   "quote.send",
		TTL:         time.Hour,
	})
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	// 让审批行的凭证与那枚定时器对上（生产里两者由同一次挂起同时写入）。
	if err := database.Model(&model.ApprovalRequest{}).Where("id = ?", row.ID).
		Update("resume_token", token).Error; err != nil {
		t.Fatalf("对齐凭证失败: %v", err)
	}

	if _, err := rt.svc.Decide(ctx, row.ID, service.ApprovalApprove, "ops-lead", "wired"); err != nil {
		t.Fatalf("裁决失败: %v", err)
	}

	var after model.SOPTimer
	if err := database.First(&after, tm.ID).Error; err != nil {
		t.Fatalf("重读定时器失败: %v", err)
	}
	if !after.WaitUntil.Before(future) {
		t.Errorf("裁决后定时器 wait_until 仍是 %v（未提前）⇒ 装配没把唤醒出口接上", after.WaitUntil)
	}
	if after.Status != "pending" {
		t.Errorf("推送只该提前时刻，不该自己点火（状态=%s）", after.Status)
	}
}

// TestApprovalRuntime_ReInitStopsPreviousSweeper 守住"重复装配不攒协程"。
//
// router.Setup 在测试进程里可被多次调用；只装不停会攒出 N 个并发清扫器，
// 它们对同一张表反复跑 UPDATE（且每一轮的成功都被计入不同实例的读数，报告口径失真）。
func TestApprovalRuntime_ReInitStopsPreviousSweeper(t *testing.T) {
	installApprovalRuntimeCleanup(t)
	database := approvalRuntimeTestDB(t)
	t.Setenv(ApprovalResumeFlagEnv, "shadow")

	first := InitApprovalRuntime(database)
	if first == nil {
		t.Fatal("前置：第一份没装上")
	}
	second := InitApprovalRuntime(database)
	if second == nil {
		t.Fatal("前置：第二份没装上")
	}
	if first == second {
		t.Fatal("两次 Init 返回了同一个实例：说明上一份根本没被停掉")
	}
	if first.sweeper.Running() {
		t.Error("上一份的清扫协程仍在跑 ⇒ 重复装配在攒协程")
	}
	if !second.sweeper.Running() {
		t.Error("新一份的清扫协程没在跑")
	}
	StopApprovalRuntime()
	if second.sweeper.Running() {
		t.Error("StopApprovalRuntime 后清扫协程仍在跑")
	}
	if b := service.GetApprovalResumeBridge(); b != nil {
		t.Error("Stop 之后全局桥未清空")
	}
}

// executeApprovalWaitNode 用**执行器**跑一个审批等待节点（走 WaitExecutor 的委托入口）。
//
// 用注册表拿执行器，而不是直接 new：委托关系挂在 NewWaitExecutor 上，
// 装配测试要碰的就是那个真实入口。
func executeApprovalWaitNode(t *testing.T, database *gorm.DB, ctx context.Context) *service.NodeExecResult {
	t.Helper()
	exec := &model.SOPExecution{
		SOPID: 1, CustomerID: "wiring-" + t.Name(), Status: "running", CurrentNode: "w1",
		StartedAt: time.Now(), ExecutionData: model.JSONMap{},
	}
	if err := database.Create(exec).Error; err != nil {
		t.Fatalf("建执行失败: %v", err)
	}
	node := &dto.SOPNode{ID: "w1", Type: "wait", Next: []string{"gate"}, Config: map[string]any{
		"wait_event":           service.WaitEventApproval,
		"approval_policy_key":  "quote.send",
		"approval_ttl_seconds": 600.0,
	}}
	ec := &service.ExecutionContext{
		Execution: exec, Node: node, CustomerID: exec.CustomerID,
		ExecutionData: exec.ExecutionData, Input: exec.ExecutionData, TraceID: "wiring",
	}
	res, err := service.NewWaitExecutor(&service.SOPNodeExecutorDeps{DB: database}).Execute(ctx, ec)
	if err != nil {
		t.Fatalf("执行器返回 error（失败应由状态表达）：%v", err)
	}
	return res
}
