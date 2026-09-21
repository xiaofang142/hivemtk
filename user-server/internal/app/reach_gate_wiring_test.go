package app

// T-P3-07：把 W-1 的审批裁决接到"非工具外发出口"（ProactiveReachService 发送前钩子）上。
//
// 本文件与 approval_wiring_test.go 同一条纪律：**只用真的** WhiteListApprovalChecker +
// 真 featureflag + 真 ProactiveReachService（外发出口换成 spy）。审批门真正危险的地方
// 从来不是那段 if，而是"默认拒绝 + 内存白名单 + 一把只在一个建链点生效的旗子"被接到
// 三条没接线的路径上 —— 用假 checker、假 reach 服务，这些一个都测不出来。
//
// 卡面 AC 与用例的对应：
//
//	③ 默认 off、shadow 只记 would_deny、布尔真值降 shadow
//	  → TestAttachReachGateOffSendsWithoutAsking / …ShadowRecordsWouldDeny /
//	    …BooleanValueDowngradesToShadow
//	① gate 拒绝 ⇒ 该路径零外发。用例在 service 与 controller 两侧：
//	  recovery_queue_worker_gate_test.go（cron 恢复队列）、
//	  proactive_reach_gate_test.go（service 出口 + 直接 API 五个入口）。
//	  卡面预告的第三条"SOP 节点"经核实不经过 ReachByCustomer —— 它只写会话消息与
//	  商家 WS，所以这一侧没有用例；漏一条不是测试缺失，是卡面对路径的枚举多了一条，
//	  已回灌执行结果。本文件断的是"接线装了哪一把门"，不是"哪条路径被拦"。
//	  → TestAttachReachGateBlockRefusesUnapprovedSubject / …GrantOpensIt
//	② 判定键用客户身份而非恒空 accountID
//	  → TestAttachReachGateGrantsAreKeyedBySubject
//	④ 既有工具路径行为不变
//	  → TestReachGateDoesNotTouchToolGateReport（两把门的旗子、计数器各自独立）

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/approval"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"
)

// restoreReachGateState 保存/还原 reach 门的包级状态。
// 与 restoreApprovalState 同一条理由：包级变量没有锁，靠"只在装配期写"成立。
//
// 这里顺带把全局 ltc.config 换成一份"运营从没配过放量档"的存储：从 T-P5-04 起
// 闸门每请求读那份档位（见 reach_gate_wiring.go 的 CheckReachPreSend），而本文件的
// 夹具都不经过去建那套库 ⇒ 不装它就会读到 degraded，闸门按"取严"一律拒，
// 于是这些用例测的就不再是 T-P3-07 那把门本身。装完之后 shadow 档放行，
// 与档位这一层存在时的行为逐字一致（这条正是 TestReachRolloutShadowLeavesW1InCharge 钉的）。
func restoreReachGateState(t *testing.T) {
	t.Helper()
	m, c, n := reachGateModeValue, reachDecisions, reachAttached
	prevLTC := service.GlobalLTCConfig()
	service.SetGlobalLTCConfig(service.NewLTCConfigServiceWithStore(&rolloutKV{}))
	t.Cleanup(func() {
		reachGateModeValue, reachDecisions, reachAttached = m, c, n
		service.SetGlobalLTCConfig(prevLTC)
	})
}

// spyReachService 造一个"短信出口指向计数器"的真触达服务。
//
// 刻意不用假服务：这张卡要防的是"钩子接上了、可真正的发送路径没被包住"，
// 只有真服务真走 ReachByCustomer 才看得见那种漏法。
//
// 库必须是真的：手机直发分支在出口前要先读全局退订标志位，传 nil 库会让 DNC 仓库
// 退到进程全局句柄（本包的测试里没有初始化它 ⇒ 直接 panic，上一轮就是这样撞出来的）。
func spyReachService(t *testing.T, sent *[]string) *service.ProactiveReachService {
	t.Helper()
	db := testutil.NewTestDB(t, &model.Customer{}, &model.CustomerChannel{}, &model.CustomerDoNotContact{})
	svc := service.NewProactiveReachService(db, nil)
	svc.SetSMSRegistry(func() (func(ctx context.Context, phone, content, templateID string, params map[string]string) (string, error), error) {
		return func(_ context.Context, phone, _, _ string, _ map[string]string) (string, error) {
			*sent = append(*sent, phone)
			return "sms_gate_spy", nil
		}, nil
	})
	return svc
}

// wireToolGate 按给定模式接好 W-1（reach 门的授权表与裁决来源就是它那份）。
func wireToolGate(t *testing.T, mode string) {
	t.Helper()
	t.Setenv(ApprovalGateFlagEnv, mode)
	applyApprovalGate(&tooluse.ToolExecutorConfig{})
}

func sendSMS(t *testing.T, svc *service.ProactiveReachService, phone string) error {
	t.Helper()
	_, err := svc.ReachByCustomer(context.Background(), &service.ProactiveReachRequest{
		Phone: phone, Content: "老客回归立减 30",
	})
	return err
}

func reachDecisionTotal(t *testing.T) (int64, int64, map[string]int64) {
	t.Helper()
	_, counter := GetReachGateSnapshot()
	if counter == nil {
		t.Fatal("reach 门计数器为 nil：说明钩子压根没装")
	}
	rep := counter.Report()
	return rep.Total, rep.WouldDeny, rep.ByReason
}

func TestAttachReachGateOffSendsWithoutAsking(t *testing.T) {
	restoreApprovalState(t)
	restoreReachGateState(t)
	wireToolGate(t, "off")
	t.Setenv(ReachGateFlagEnv, "off")

	sent := []string{}
	svc := spyReachService(t, &sent)
	if AttachReachGate(svc) {
		t.Fatal("off 时不该装钩子")
	}
	if err := sendSMS(t, svc, "12900001111"); err != nil {
		t.Fatalf("off 时外发不该受影响: %v", err)
	}
	if len(sent) != 1 {
		t.Fatalf("off 时应正常外发: %v", sent)
	}
	snap, counter := GetReachGateSnapshot()
	if snap.Wired || counter != nil {
		t.Errorf("off 时快照应为未接线: %+v", snap)
	}
}

func TestAttachReachGateShadowRecordsWouldDenyButSends(t *testing.T) {
	restoreApprovalState(t)
	restoreReachGateState(t)
	setApprovalWhitelistFlag(t, false)
	wireToolGate(t, "shadow")
	t.Setenv(ReachGateFlagEnv, "shadow")

	sent := []string{}
	svc := spyReachService(t, &sent)
	if !AttachReachGate(svc) {
		t.Fatal("shadow 时应装上钩子")
	}
	phone := "12900002222"
	if err := sendSMS(t, svc, phone); err != nil {
		t.Fatalf("shadow 态不得拦下任何外发: %v", err)
	}
	if len(sent) != 1 {
		t.Fatalf("shadow 应照常外发: %v", sent)
	}
	total, wouldDeny, byReason := reachDecisionTotal(t)
	if total != 1 || wouldDeny != 1 {
		t.Fatalf("shadow 的核心产出是 would_deny 计数：total=%d would_deny=%d", total, wouldDeny)
	}
	// 白名单旗子没开 ⇒ 这笔 would_deny 的理由必须是 disabled_by_flag，
	// 否则报告会把"没人被批准"与"授权源没启用"混成一件事（T-P1-06 立的就是这条口径）。
	if byReason[approval.ReasonDisabledByFlag] != 1 {
		t.Errorf("would_deny 理由分布异常: %v", byReason)
	}
	snap, _ := GetReachGateSnapshot()
	if snap.BlocksWhenDenied || snap.Mode != "shadow" {
		t.Errorf("shadow 态快照不得声称会拦: %+v", snap)
	}
}

// 刹车与 W-1 同源：block 态但白名单旗子没开时，reason=disabled_by_flag 的拒绝不拦。
// 此时"允许"这一侧无路径可达，拦下去只剩无差别失败。
func TestAttachReachGateBlockBrakeWithoutWhitelistFlag(t *testing.T) {
	restoreApprovalState(t)
	restoreReachGateState(t)
	setApprovalWhitelistFlag(t, false)
	wireToolGate(t, "shadow")
	t.Setenv(ReachGateFlagEnv, "block")

	sent := []string{}
	svc := spyReachService(t, &sent)
	if !AttachReachGate(svc) {
		t.Fatal("block 时应装上钩子")
	}
	if err := sendSMS(t, svc, "12900003333"); err != nil {
		t.Fatalf("白名单旗子没开时刹车应放行，实际被拦: %v", err)
	}
	if len(sent) != 1 {
		t.Fatalf("刹车生效时应外发: %v", sent)
	}
	if _, wouldDeny, _ := reachDecisionTotal(t); wouldDeny != 1 {
		t.Errorf("放行不等于没记账：would_deny 仍要累计，实际 %d", wouldDeny)
	}
}

// block + 白名单旗子开 + 没授权 ⇒ 真的拦下来（这条是整张卡的行为变更本体）。
func TestAttachReachGateBlockRefusesUnapprovedSubject(t *testing.T) {
	restoreApprovalState(t)
	restoreReachGateState(t)
	setApprovalWhitelistFlag(t, true)
	wireToolGate(t, "shadow")
	t.Setenv(ReachGateFlagEnv, "block")

	sent := []string{}
	svc := spyReachService(t, &sent)
	if !AttachReachGate(svc) {
		t.Fatal("block 时应装上钩子")
	}
	err := sendSMS(t, svc, "12900004444")
	if !errors.Is(err, service.ErrReachApprovalDenied) {
		t.Fatalf("未获授权的冷触达应被拒，实际 %v", err)
	}
	if len(sent) != 0 {
		t.Fatalf("拒绝后不得有任何外发: %v", sent)
	}
	if _, wouldDeny, byReason := reachDecisionTotal(t); wouldDeny != 1 ||
		byReason[approval.ReasonDeniedDefault] != 1 {
		t.Errorf("这笔拒绝应记为 denied_default: would_deny=%d %v", wouldDeny, byReason)
	}
	snap, _ := GetReachGateSnapshot()
	if !snap.BlocksWhenDenied {
		t.Errorf("block 态快照必须声称会拦: %+v", snap)
	}
	if snap.WhitelistEntriesForReach != 0 {
		t.Errorf("此时代表里针对 reach 的有效授权应为 0，实际 %d", snap.WhitelistEntriesForReach)
	}
}

// 授权走的是 W-1 那一个入口，键的是**判定对象**（这里是 sms:收件人）。
// 这条与上一条互为反证：只有"闸门真的按subject 查了表"时，加完授权才会从拒转发。
func TestAttachReachGateGrantOpensSubjectAndRevokeClosesIt(t *testing.T) {
	restoreApprovalState(t)
	restoreReachGateState(t)
	setApprovalWhitelistFlag(t, true)
	wireToolGate(t, "shadow")
	t.Setenv(ReachGateFlagEnv, "block")

	sent := []string{}
	svc := spyReachService(t, &sent)
	if !AttachReachGate(svc) {
		t.Fatal("block 时应装上钩子")
	}
	phone := "12900005555"
	if _, wouldDeny, _ := reachDecisionTotal(t); wouldDeny != 0 {
		t.Fatalf("前置：计数应从零开始，实际 %d", wouldDeny)
	}

	subject := "sms:" + phone
	if !ApprovalWhitelistMutate(ReachApprovalToolKey, subject, time.Time{}, false) {
		t.Fatal("授权入口在 W-1 checker 未接线时不可用")
	}
	if err := sendSMS(t, svc, phone); err != nil {
		t.Fatalf("已获授权的对象应放行: %v", err)
	}
	if len(sent) != 1 {
		t.Fatalf("授权后应外发: %v", sent)
	}
	snap, _ := GetReachGateSnapshot()
	if snap.WhitelistEntriesForReach != 1 {
		t.Errorf("有效授权应显示 1，实际 %d", snap.WhitelistEntriesForReach)
	}

	// 撤权立刻生效（block 态下这就是唯一的放行开关，运维要能指望它）。
	if !ApprovalWhitelistMutate(ReachApprovalToolKey, subject, time.Time{}, true) {
		t.Fatal("撤权入口不可用")
	}
	sent2 := []string{}
	svc2 := spyReachService(t, &sent2)
	if !AttachReachGate(svc2) {
		t.Fatal("block 时应装上钩子")
	}
	if err := sendSMS(t, svc2, phone); !errors.Is(err, service.ErrReachApprovalDenied) {
		t.Fatalf("撤权后应立即回到拒绝，实际 %v", err)
	}
	if len(sent2) != 0 {
		t.Errorf("撤权后不得外发: %v", sent2)
	}
}

// AC②：判定键必须是客户身份。有 one_id 时键是 one_id，且 req.AccountID 不得参与。
func TestAttachReachGateGrantsAreKeyedBySubject(t *testing.T) {
	restoreApprovalState(t)
	restoreReachGateState(t)
	setApprovalWhitelistFlag(t, true)
	wireToolGate(t, "shadow")
	t.Setenv(ReachGateFlagEnv, "block")

	oneID := "uid-reach-gate-1"
	ApprovalWhitelistMutate(ReachApprovalToolKey, oneID, time.Time{}, false)

	sent := []string{}
	svc := spyReachService(t, &sent)
	if !AttachReachGate(svc) {
		t.Fatal("应装上钩子")
	}
	// 带 one_id + 一个无关 accountID：放行只可能因为键=one_id。
	if _, err := svc.ReachByCustomer(context.Background(), &service.ProactiveReachRequest{
		OneID: oneID, AccountID: "acc-not-the-key", Phone: "12900006666", Content: "hi",
	}); err != nil {
		t.Fatalf("按 one_id 授权应放行，实际 %v", err)
	}
	if len(sent) != 1 {
		t.Fatalf("放行后应外发: %v", sent)
	}
	snap, _ := GetReachGateSnapshot()
	if !strings.Contains(snap.ReachToolKey, "reach") {
		t.Errorf("快照要回显授权键所在的入口名: %+v", snap)
	}
}

// W-1 没接线时 reach 门装不上：宁可不装，也不装一把"没有裁决来源"的门。
// 那正是 T-P1-05 的假闸门形态 —— 看起来在拦，实际恒放或恒拒。
func TestAttachReachGateRequiresW1Checker(t *testing.T) {
	restoreApprovalState(t)
	restoreReachGateState(t)
	setApprovalWhitelistFlag(t, true)
	wireToolGate(t, "off")
	t.Setenv(ReachGateFlagEnv, "block")

	sent := []string{}
	svc := spyReachService(t, &sent)
	if AttachReachGate(svc) {
		t.Fatal("W-1 未接线时不得装上 reach 门")
	}
	if err := sendSMS(t, svc, "12900007771"); err != nil {
		t.Fatalf("没装门就不该有额外失败: %v", err)
	}
	snap, counter := GetReachGateSnapshot()
	if snap.Wired || counter != nil {
		t.Errorf("依赖不成立时快照必须报未接线: %+v", snap)
	}
	if !snap.DependencyUnmet {
		t.Errorf("快照要说明「为什么没装」，否则运维只会看到 mode=block 而 wired=false: %+v", snap)
	}
	if len(sent) != 1 {
		t.Fatalf("外发应照常: %v", sent)
	}
}

// 布尔真值只到 shadow：与 W-1 同一纪律（把冷触达永久拒掉必须写出 block 这个词）。
func TestAttachReachGateBooleanValueDowngradesToShadow(t *testing.T) {
	restoreApprovalState(t)
	restoreReachGateState(t)
	setApprovalWhitelistFlag(t, true)
	wireToolGate(t, "shadow")
	t.Setenv(ReachGateFlagEnv, "true")

	logs := captureApprovalLog(t, func() {
		sent := []string{}
		svc := spyReachService(t, &sent)
		if !AttachReachGate(svc) {
			t.Error("true 应降到 shadow 并装门")
		}
		if err := sendSMS(t, svc, "12900008881"); err != nil {
			t.Errorf("降为 shadow 后不得拦下外发: %v", err)
		}
		if len(sent) != 1 {
			t.Errorf("降为 shadow 后应照常外发: %v", sent)
		}
	})
	if !strings.Contains(logs, ReachGateFlagEnv) || !strings.Contains(logs, "shadow") {
		t.Errorf("布尔真值要留下一条点名 %s 的告警，实际日志: %s", ReachGateFlagEnv, logs)
	}
	if snap, _ := GetReachGateSnapshot(); snap.BlocksWhenDenied {
		t.Error("布尔真值不得换来阻断权限")
	}
}

// 认不出的值判 off（不装门），而不是"看起来严格其实全放"。
func TestAttachReachGateUnknownValueMeansOff(t *testing.T) {
	restoreApprovalState(t)
	restoreReachGateState(t)
	wireToolGate(t, "shadow")
	t.Setenv(ReachGateFlagEnv, "yes-please")

	sent := []string{}
	svc := spyReachService(t, &sent)
	if AttachReachGate(svc) {
		t.Error("认不出的值必须按 off 处理")
	}
	if err := sendSMS(t, svc, "12900009991"); err != nil {
		t.Fatalf("off 时外发不受影响: %v", err)
	}
}

// 两把门各数各的：reach 的判定不能灌进 W-1 那份 would_deny 报告 ——
// 那份报告是 T-P1-06 转阻断的准入证据，混进另一条路径的量会把结论推歪。
func TestReachGateDoesNotTouchToolGateReport(t *testing.T) {
	restoreApprovalState(t)
	restoreReachGateState(t)
	setApprovalWhitelistFlag(t, true)
	wireToolGate(t, "shadow")
	t.Setenv(ReachGateFlagEnv, "shadow")

	sent := []string{}
	svc := spyReachService(t, &sent)
	if !AttachReachGate(svc) {
		t.Fatal("应装上钩子")
	}
	if err := sendSMS(t, svc, "12900001234"); err != nil {
		t.Fatalf("shadow 不该拦: %v", err)
	}

	toolSnap, toolCounter := GetApprovalSnapshot()
	if toolCounter == nil {
		t.Fatal("W-1 计数器缺失")
	}
	if rep := toolCounter.Report(); rep.Total != 0 {
		t.Errorf("reach 的判定不得进工具门的报告，实际 %+v", rep)
	}
	if toolSnap.Wired != true {
		t.Errorf("工具门接线状态不该被 reach 影响: %+v", toolSnap)
	}
	if _, wouldDeny, _ := reachDecisionTotal(t); wouldDeny != 1 {
		t.Errorf("reach 侧应记到自己那份: would_deny=%d", wouldDeny)
	}
}

// 快照里的旗子名必须与代码实际读取的变量名同源（两处硬编码迟早漂移）。
func TestReachGateSnapshotNamesItsOwnEnv(t *testing.T) {
	restoreApprovalState(t)
	restoreReachGateState(t)
	setApprovalWhitelistFlag(t, false)
	wireToolGate(t, "shadow")
	t.Setenv(ReachGateFlagEnv, "shadow")

	svc := spyReachService(t, &[]string{})
	AttachReachGate(svc)
	snap, _ := GetReachGateSnapshot()
	if snap.GateFlagEnv != ReachGateFlagEnv {
		t.Errorf("快照 gate_flag_env = %q，want %q", snap.GateFlagEnv, ReachGateFlagEnv)
	}
	if snap.DependencyFlagEnv != ApprovalGateFlagEnv {
		t.Errorf("快照 dependency_flag_env = %q，want %q", snap.DependencyFlagEnv, ApprovalGateFlagEnv)
	}
	if snap.ReachToolKey != ReachApprovalToolKey {
		t.Errorf("快照 reach_tool_key = %q", snap.ReachToolKey)
	}
	if snap.WhitelistActiveEntries != 0 {
		t.Errorf("共享白名单总条目应读到真值，实际 %d", snap.WhitelistActiveEntries)
	}
	if snap.AttachedServices == 0 {
		t.Error("快照要报出「几个装配点拿到了钩子」，否则漏接一个装配点看不出来")
	}
}

// 两个装配点都要接：HTTP 侧与 cron 侧各一个，少接一个就是留一条盲区。
func TestAttachReachGateCountsEveryService(t *testing.T) {
	restoreApprovalState(t)
	restoreReachGateState(t)
	wireToolGate(t, "shadow")
	t.Setenv(ReachGateFlagEnv, "shadow")

	svcA := spyReachService(t, &[]string{})
	svcB := spyReachService(t, &[]string{})
	if !AttachReachGate(svcA) || !AttachReachGate(svcB) {
		t.Fatal("两次装配都应成功")
	}
	snap, _ := GetReachGateSnapshot()
	if snap.AttachedServices < 2 {
		t.Errorf("attached_services 应累计到 2，实际 %d", snap.AttachedServices)
	}
}
