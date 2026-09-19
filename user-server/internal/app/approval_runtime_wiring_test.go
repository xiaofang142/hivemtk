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
//
// 全局审批服务也在这里清（T-P3-04 起它是一份真全局单例）：它比桥更会被后续用例
// 间接触到（任何拿 GlobalApprovalRequestService 的地方），留着上一份就是一个
// 没人负责、也没人知道还活着的实例。
func installApprovalRuntimeCleanup(t *testing.T) {
	t.Helper()
	t.Cleanup(StopApprovalRuntime)
	t.Cleanup(func() { service.SetApprovalResumeBridge(nil) })
	t.Cleanup(func() { service.SetGlobalApprovalRequestService(nil) })
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

// --- T-P3-04：全局登记处与待办出口 --------------------------------------------

// approvalPlusTaskTestDB 审批 + 待办两张表。
//
// 不复用 approvalRuntimeTestDB：那个槽位没有 human_tasks 表，本组用例要证的正是
// "审批入队时待办池里多了一行"。
func approvalPlusTaskTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t, &model.ApprovalRequest{}, &model.HumanTask{})
}

func installHumanTaskGlobalCleanup(t *testing.T) {
	t.Helper()
	prev := service.GlobalHumanTaskService()
	t.Cleanup(func() { service.SetGlobalHumanTaskService(prev) })
}

func countApprovalTasks(t *testing.T, database *gorm.DB, approvalID string) int64 {
	t.Helper()
	var n int64
	err := database.Model(&model.HumanTask{}).
		Where("kind = ? AND subject_type = ? AND subject_id = ?",
			model.HumanTaskKindApproval, service.HumanTaskSubjectApprovalRequest, approvalID).
		Count(&n).Error
	if err != nil {
		t.Fatalf("读待办失败: %v", err)
	}
	return n
}

// 装配层必须把审批服务登记成全局实例：裁决端点在 router 包里现取全局，
// 不登记就是"运行时装了、API 永远 503"，而 503 在监控里读起来像底座挂了。
func TestInitApprovalRuntime_RegistersGlobalService(t *testing.T) {
	installApprovalRuntimeCleanup(t)
	database := approvalPlusTaskTestDB(t)
	t.Setenv(ApprovalResumeFlagEnv, "on")

	InitApprovalRuntime(database)
	svc := service.GlobalApprovalRequestService()
	if svc == nil {
		t.Fatal("on 档之后全局审批服务仍为 nil")
	}
	if !svc.Available() {
		t.Error("全局实例 Available()=false：拿到的是一份没有底座的壳")
	}
}

// off / 无 DB 两档必须把全局**清成 nil**，尤其是"先 on 再 off"那一次：
// 留着上一份，裁决端点会对着一个已经撤掉运行时实例继续回 200（T-P3-02 ⑤(b) 同一类缺陷）。
func TestInitApprovalRuntime_OffAndNilDBClearGlobalService(t *testing.T) {
	installApprovalRuntimeCleanup(t)
	database := approvalPlusTaskTestDB(t)

	t.Setenv(ApprovalResumeFlagEnv, "on")
	if InitApprovalRuntime(database) == nil {
		t.Fatal("前置：on 档没装上")
	}
	if service.GlobalApprovalRequestService() == nil {
		t.Fatal("前置：全局审批服务没登记")
	}

	for _, c := range []struct {
		name string
		flag string
		db   *gorm.DB
	}{
		{"切回 off", "off", database},
		{"旗子还在但没了 DB 句柄", "on", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(ApprovalResumeFlagEnv, c.flag)
			if rt := InitApprovalRuntime(c.db); rt != nil {
				t.Errorf("%s 档不该返回运行时：%+v", c.flag, rt)
			}
			if got := service.GlobalApprovalRequestService(); got != nil {
				t.Errorf("%s 档全局仍是 %+v：/api/approvals/* 会对着撤掉的实例继续回 200", c.name, got)
			}
		})
	}
}

// 装配顺序陷阱（本卡最实际的一条）：router.go 里 InitApprovalRuntime 跑在
// InitHumanTaskRuntime **之前**（:230 与 :235）。若装配时把当时的全局待办实例
// 传进 SetTaskSink，拿到的恒是 nil ⇒ 审批永远不进待办池，而现象是"功能静默缺失"：
// 没有报错、没有日志，只有池子里永远看不到审批类待办。
//
// 所以本用例刻意按生产顺序装：先审批、后待办，再入队。
func TestInitApprovalRuntime_DeliversToHumanTaskPoolAssembledLater(t *testing.T) {
	installApprovalRuntimeCleanup(t)
	installHumanTaskGlobalCleanup(t)
	database := approvalPlusTaskTestDB(t)
	ctx := context.Background()

	service.SetGlobalHumanTaskService(nil)
	t.Setenv(ApprovalResumeFlagEnv, "on")
	if InitApprovalRuntime(database) == nil {
		t.Fatal("前置：审批运行时没装上")
	}
	// 待办底座后装（生产同序）
	if InitHumanTaskRuntime(database) == nil {
		t.Fatal("前置：待办底座没装上")
	}

	row, _, err := service.GlobalApprovalRequestService().Submit(ctx, service.ApprovalSubmitInput{
		SubjectType: "quote", SubjectID: "q_wired_1", PolicyKey: "quote.send",
	})
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	if n := countApprovalTasks(t, database, row.ID); n != 1 {
		t.Errorf("待办池里审批 %s 名下行数=%d，期望 1（出口没接上或接成了装配时的 nil）", row.ID, n)
	}

	// 裁决之后那一行必须收口成 done：链子的另一半
	if _, err := service.GlobalApprovalRequestService().Decide(ctx, row.ID, service.ApprovalApprove, "ops-lead", "wired"); err != nil {
		t.Fatalf("裁决失败: %v", err)
	}
	var done int64
	if err := database.Model(&model.HumanTask{}).
		Where("subject_id = ? AND status = ?", row.ID, model.HumanTaskStatusDone).
		Count(&done).Error; err != nil {
		t.Fatalf("读待办失败: %v", err)
	}
	if done != 1 {
		t.Errorf("裁决后收口成 done 的待办数=%d，期望 1（批了而池子里那条还能点，就是拆闸门按钮）", done)
	}
}

// 待办底座没装配时，审批入队**照常成功**且不报错。
//
// 这条钉的是失败方向的取法：deliverTask 对"投递失败"是上抛（挂起必须失败，
// 免得留下一行没人看得见的 pending），但"出口压根没装"是装配状态而不是故障，
// 与 SetTaskSink(nil) 同一含义 ⇒ 按没有出口处理。兜底本来就在：pending 行会按
// 自己的 expires_at 被清扫翻成 expired 并叫醒等待方，不会永久吊着。
//
// 反过来说，这一条不立住的话，T-P3-02 那批只装审批、不碰待办的用全会被判成失败。
func TestApprovalRuntime_WithoutHumanTaskPoolStillSubmits(t *testing.T) {
	installApprovalRuntimeCleanup(t)
	installHumanTaskGlobalCleanup(t)
	database := approvalPlusTaskTestDB(t)
	ctx := context.Background()

	service.SetGlobalHumanTaskService(nil)
	t.Setenv(ApprovalResumeFlagEnv, "on")
	rt := InitApprovalRuntime(database)
	if rt == nil {
		t.Fatal("前置：审批运行时没装上")
	}
	row, _, err := rt.svc.Submit(ctx, service.ApprovalSubmitInput{
		SubjectType: "quote", SubjectID: "q_no_pool", PolicyKey: "quote.send",
	})
	if err != nil {
		t.Fatalf("待办底座未装配时入队应成功（失败由 TTL 兜底），实际: %v", err)
	}
	if row.Status != model.ApprovalStatusPending {
		t.Errorf("status = %s，期望 pending", row.Status)
	}
}

// StopApprovalRuntime（将来 main.go 退出序列要调的那个入口）必须连全局一起清：
// 协程停了而全局还在，API 就继续回 200，而那时已经没人清扫到期 pending 了。
func TestStopApprovalRuntime_ClearsGlobalService(t *testing.T) {
	installApprovalRuntimeCleanup(t)
	database := approvalPlusTaskTestDB(t)
	t.Setenv(ApprovalResumeFlagEnv, "on")

	InitApprovalRuntime(database)
	if service.GlobalApprovalRequestService() == nil {
		t.Fatal("前置：全局审批服务没登记")
	}
	StopApprovalRuntime()
	if got := service.GlobalApprovalRequestService(); got != nil {
		t.Errorf("Stop 之后全局审批服务仍是 %+v ⇒ /api/approvals/* 对着停掉的运行时回 200", got)
	}
}
