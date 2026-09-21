// sop_reach_send_test.go T-P5-03：Active 出域闸门的串联契约。
//
// 卡面是「所有 Active 出域动作必经 approval_request + checkCooldown + checkDoNotContact」。
// 接线前的实测状态是 **Active 出域 = 零**：`MessageNodeBase.Execute` 只写会话消息与商家 WS，
// 节点上的 `Tools` 没有任何执行方（`sop.go` 只复制它）。所以本卡交付的东西有两件：
//
//	① 一条**真的会出域**的编排出口（`reach_send` 节点），它把发送整包交给
//	   `ProactiveReachService.ReachByCustomer`（三判据在那里面，本卡不改其选路）；
//	② 让「未获裁决」成为发不出去的状态：图上审批腿先问、没结论就挂起，
//	   点火重入时才把结论递回执行器。
//
// 三条 AC 各自对应一组可判别用例（把实现改坏，必须有对应用例变红）：
//
//	AC① DNC 客户零外发        → TestReachSend_DoNotContactCustomerSendsNothing
//	AC② 超频控零外发          → TestReachSend_InsideCooldownSendsNothing
//	AC③ 未审批停在 pending    → TestReachSend_NoVerdictParksAndSendsNothing（挂起态）
//	                             + TestReachSend_NonApprovedVerdictSendsNothing（拒/过期/读不回）
//	                             + TestReachSendEndToEnd（人工批准后同一条执行真的发出去一次）
//	反向测试（卡面点名）       → TestNodeExecutorFiles_HaveNoCustomerOutboundBeyondReachByCustomer
//	                             + TestReachSend_PreSendGateDenialDoesNotRePark（不造审批永动机）
//
// 夹具口径：一个用例只调一次 `testutil.NewTestDB`（它会 DROP 再重建，第二次调用会把第一个
// 服务刚种下的客户抹掉），审批侧与外发侧共用同一个库 —— 用两个库测出来的「零外发」不算数。
//
// 外发侧用的是**真服务 + spy 出口**（`newSpyReachServiceOn`），不是假 sender：
// DNC、频控、T-P3-07 的发送前闸门都是真代码在判，spy 只负责证明「一个字节都没出域」。
// 若换成假 sender，本文件断言的就不再是「闸门拦住了」，而是「执行器读到了假 sender 的回话」。
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// —— 夹具 ————————————————————————————————————————————————

type reachSendFixture struct {
	approval *ApprovalRequestService
	reach    *ProactiveReachService
	spy      *reachSendSpy
	gate     *stubReachGate
	db       *gorm.DB
}

// newReachSendFixture 一套同时装配着「审批运行时」与「外发服务（出口指向 spy）」的真库环境。
//
// T-P3-07 那把发送前闸门在这里预设为**放行**：本卡测的是图上的审批腿，
// 让两把门互相抵消只会测出「有一把门在拦」而说不清是哪把；
// 闸门自己拦住时的行为由 TestReachSend_PreSendGateDenialDoesNotRePark 单独覆盖。
func newReachSendFixture(t *testing.T) *reachSendFixture {
	t.Helper()
	db := testutil.NewTestDB(t,
		&model.ApprovalRequest{}, &model.SOPExecution{}, &model.SOPTimer{},
		&model.SOPAgent{}, &model.SOPExecEvent{},
		&model.Customer{}, &model.CustomerChannel{}, &model.CustomerDoNotContact{},
		&model.RecoveryQueue{},
	)
	approvalSvc := NewApprovalRequestService(repository.NewApprovalRequestRepositoryWithDB(db), nil)
	bridge := NewApprovalResumeBridge(approvalSvc, db)
	if bridge == nil {
		t.Fatal("NewApprovalResumeBridge 传了服务却返回 nil")
	}
	approvalSvc.SetWaitNotifier(bridge)
	SetApprovalResumeBridge(bridge)

	spy := &reachSendSpy{}
	gate := &stubReachGate{allowed: true, reason: "whitelisted"}
	reach := newSpyReachServiceOn(db, spy)
	reach.SetDoNotContact(NewDoNotContactService(repository.NewCustomerDoNotContactRepository(db)))
	reach.SetPreSendApprovalChecker(gate)

	SetSOPReachSender(reach)
	t.Cleanup(func() {
		SetApprovalResumeBridge(nil)
		approvalSvc.SetWaitNotifier(nil)
		SetSOPReachSender(nil)
	})
	return &reachSendFixture{approval: approvalSvc, reach: reach, spy: spy, gate: gate, db: db}
}

// reachSendSeq 保证同一进程内每个用例拿到互不撞的身份（冷却窗按 one_id 记，撞了就是上一轮的窗）。
var reachSendSeq int64

// seedCustomer 种一个只有手机号的客户。
//
// 只给 Phone 而**不给** req.Phone：可选渠道是 sms，但走的是「按客户选路」那一支，
// 也就是三判据齐全的那一支（显式指定手机号的入参分支不过频控，见 §4.19 那条登记）。
func (f *reachSendFixture) seedCustomer(t *testing.T) (customerID, oneID, phone string) {
	t.Helper()
	n := atomic.AddInt64(&reachSendSeq, 1)
	customerID = fmt.Sprintf("cust-rs-%d-%s", n, gateNonce)
	oneID = fmt.Sprintf("uid-rs-%d-%s", n, gateNonce)
	phone = fmt.Sprintf("13%02d%s", n, gateNonce)
	if err := f.db.Create(&model.Customer{ID: customerID, UnifiedID: oneID, Phone: phone}).Error; err != nil {
		t.Fatalf("建客户失败: %v", err)
	}
	return customerID, oneID, phone
}

// reachSendNode 一个外发节点。ttl 直接进 approval_ttl_seconds。
func reachSendNode(id, policyKey string, ttlSeconds float64) *dto.SOPNode {
	return &dto.SOPNode{
		ID:   id,
		Type: SOPNodeTypeReachSend,
		Next: []string{"done"},
		Config: map[string]any{
			"content":              "本周方案已就绪",
			"approval_policy_key":  policyKey,
			"approval_ttl_seconds": ttlSeconds,
		},
	}
}

// runOn 以给定审批结论跑一次外发节点（outcome 为 nil = 还没有结论，即第一次进）。
// 每次调用**新建一条执行**：冷却窗与幂等键都挂在执行/身份上，复用会把两次调用糊成一次。
func (f *reachSendFixture) runOn(t *testing.T, node *dto.SOPNode, customerID string, outcome model.JSONMap) (*NodeExecResult, error) {
	t.Helper()
	exec := newSOPExecution(t, f.db, 1, customerID, node.ID)
	ec := execCtxFor(exec, node)
	ec.ApprovalOutcome = outcome
	return NewReachSendExecutor().Execute(context.Background(), ec)
}

func (f *reachSendFixture) countApprovals(t *testing.T, status string) int {
	t.Helper()
	var n int64
	if err := f.db.Model(&model.ApprovalRequest{}).Where("status = ?", status).Count(&n).Error; err != nil {
		t.Fatalf("数审批行失败: %v", err)
	}
	return int(n)
}

func (f *reachSendFixture) countPendingTimers(t *testing.T) int {
	t.Helper()
	var n int64
	if err := f.db.Model(&model.SOPTimer{}).Where("status = ?", "pending").Count(&n).Error; err != nil {
		t.Fatalf("数定时器失败: %v", err)
	}
	return int(n)
}

// verdictOutcome 点火重入时调度器递给执行器的那份结论。
// 状态值用 model 侧常量而不是字面量：审批状态改名时这里要一起红，
// 而不是默默测一个生产代码再也写不出来的值。
func verdictOutcome(status string, allowed bool) model.JSONMap {
	return model.JSONMap{
		ApprovalOutcomeStatusKey:  status,
		ApprovalOutcomeAllowedKey: allowed,
	}
}

// —— AC③：未获裁决不能出域 ————————————————————————————————

// 第一次进（手里没有结论）：必须挂起，且一个字节都不出域。
//
// 这条是本卡的地基 —— 它同时钉住三件事：挂起有载体（审批行 + 定时器各一行 pending）、
// 挂起期间没发（spy 为空）、**连 T-P3-07 那把发送前闸门都没被问到**
// （还没到发送那一步就去问门，等于把「要不要发」和「发给谁获批了吗」混成一问）。
func TestReachSend_NoVerdictParksAndSendsNothing(t *testing.T) {
	f := newReachSendFixture(t)
	customerID, _, _ := f.seedCustomer(t)

	res, err := f.runOn(t, reachSendNode("r1", "reach.active", 3600), customerID, nil)
	if err != nil {
		t.Fatalf("挂起不是错误: %v", err)
	}
	if res == nil || res.Status != NodeStatusWaiting {
		t.Fatalf("未获裁决必须挂起，实际 %+v", res)
	}
	if res.WaitEvent != WaitEventApproval {
		t.Errorf("等待对象应是审批裁决，实际 %q", res.WaitEvent)
	}
	if f.spy.total() != 0 {
		t.Errorf("挂起态不得有任何外发: %+v", *f.spy)
	}
	if got := f.countApprovals(t, model.ApprovalStatusPending); got != 1 {
		t.Errorf("挂起必须留下一条 pending 审批（待办中心看得见），实际 %d 条", got)
	}
	if got := f.countPendingTimers(t); got != 1 {
		t.Errorf("挂起必须留下一条 pending 定时器（重启后能续跑），实际 %d 条", got)
	}
	if f.gate.calls() != 0 {
		t.Errorf("还没到发送那一步，不该问发送前闸门，实际问了 %d 次", f.gate.calls())
	}
}

// 审批运行时没装配（W-1 旗子 off）⇒ 判失败，不静默跳过、更不放行。
//
// 与 WaitExecutor 同一口径：一把不能把关的闸门必须把门关上。
// 若这里改成 Skipped，「旗子没开」就会变成「这批外发不需要审批」。
func TestReachSend_ApprovalRuntimeUnwiredFailsClosed(t *testing.T) {
	f := newReachSendFixture(t)
	customerID, _, _ := f.seedCustomer(t)
	SetApprovalResumeBridge(nil)

	res, err := f.runOn(t, reachSendNode("r1", "reach.active", 3600), customerID, nil)
	if err != nil {
		t.Fatalf("未装配不该以 error 上抛（调度器要按结果处置）: %v", err)
	}
	if res == nil || res.Status != NodeStatusFailed {
		t.Fatalf("未装配必须判失败，实际 %+v", res)
	}
	if res.Retryable {
		t.Errorf("未装配重试一百次也不会自己装配上，不该标可重试: %+v", res)
	}
	if f.spy.total() != 0 {
		t.Errorf("未装配时不得有任何外发: %+v", *f.spy)
	}
}

// 结论不是 approved 的三种情形（人工拒了 / 到期没人理 / 读不回）都必须零外发，
// 且**不再新建审批**：一条已经落终态的等待不能被这个节点重新点着
// （否则待办中心会被同一件事刷屏，且流程永远发不出去 —— 见反永动机那条）。
func TestReachSend_NonApprovedVerdictSendsNothing(t *testing.T) {
	cases := []struct {
		name   string
		status string
	}{
		{"rejected", model.ApprovalStatusRejected},
		{"expired", model.ApprovalStatusExpired},
		{"unreadable", approvalOutcomeUnreadable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newReachSendFixture(t)
			customerID, _, _ := f.seedCustomer(t)

			res, err := f.runOn(t, reachSendNode("r1", "reach.active", 3600), customerID,
				verdictOutcome(tc.status, false))
			if err != nil {
				t.Fatalf("非放行结论不该以 error 上抛: %v", err)
			}
			if res == nil || res.Status != NodeStatusSkipped {
				t.Fatalf("未获批准应跳过本条外发，实际 %+v", res)
			}
			if got := fmt.Sprint(res.Output[outputKeyReachSkip]); got != "approval:"+tc.status {
				t.Errorf("跳过理由要写清是哪个结论（三种处置动作不同），实际 %q", got)
			}
			if f.spy.total() != 0 {
				t.Errorf("%s 结论下不得有任何外发: %+v", tc.status, *f.spy)
			}
			if f.gate.calls() != 0 {
				t.Errorf("未获批准连渠道都不该选（更不该问门），实际问了 %d 次", f.gate.calls())
			}
			if got := f.countApprovals(t, model.ApprovalStatusPending); got != 0 {
				t.Errorf("已有终态结论时不该再入队一次审批，实际 pending %d 条", got)
			}
		})
	}
}

// 批准后真的发出去，且"发出去了什么"要留下可回查的痕迹（渠道 + 消息号 + 幂等键）。
//
// 归因口径在这里说清，因为它是这条链上最容易想错的一点：**发送前闸门的判定键取自客户行上的
// UnifiedID**（`ReachByCustomer` 选完路按 `customer.UnifiedID` 问门），**不是**执行数据里的
// `one_id`；后者只在 `customer_id` 缺失时才被服务当作查身份的回退档。
// "执行数据里那份 one_id 说不动判定键"由下一条用例单独钉住。
//
// 这一条串联正是「Active 出域」与「归因」两卡的接缝，缺它则归因只落在库里、闸门只看收件人。
func TestReachSend_ApprovedVerdictSendsOnceThroughCustomerPath(t *testing.T) {
	f := newReachSendFixture(t)
	customerID, oneID, phone := f.seedCustomer(t)

	res, err := f.runOn(t, reachSendNode("r1", "reach.active", 3600), customerID,
		verdictOutcome(model.ApprovalStatusApproved, true))
	if err != nil {
		t.Fatalf("放行路径不该出错: %v", err)
	}
	if res == nil || res.Status != NodeStatusCompleted {
		t.Fatalf("批准后应完成，实际 %+v", res)
	}
	if f.spy.total() != 1 || f.spy.sms[0] != phone {
		t.Fatalf("应恰好外发一次到该客户手机号，实际 %+v", *f.spy)
	}
	if f.gate.calls() != 1 {
		t.Fatalf("一次外发应且只应问一次发送前闸门，实际 %d 次", f.gate.calls())
	}
	if got := f.gate.asks[0].Key; got != oneID {
		t.Errorf("闸门判定键应为该客户的 one_id（T-P5-02 归因键），实际 %q（期望 %q）", got, oneID)
	}
	if got := fmt.Sprint(res.Output[outputKeyReachChannel]); got != "sms" {
		t.Errorf("发出去的渠道要回显进执行数据（事后只能回查这一份），实际 %q", got)
	}
	if got := fmt.Sprint(res.Output[outputKeyReachMessageID]); got != "sms_spy" {
		t.Errorf("渠道给的消息号要回显，实际 %q", got)
	}
	if got := f.countPendingTimers(t); got != 0 {
		t.Errorf("已放行的节点不该留下等待载体: %d", got)
	}
	if got := fmt.Sprint(res.SideEffects); !strings.Contains(got, "reach_sent:") {
		t.Errorf("成功外发要留幂等键，实际 %v", res.SideEffects)
	}
}

// 执行数据改不动判定键：把 `one_id` 写成别人的身份，闸门问的仍然是**这个客户行**上的 one_id。
//
// 为什么值得单独一条：`reachApprovalKey` 是"one_id > customer_id > 渠道:收件人"三档回退，
// 谁写得动执行数据谁就能挑一个判定键。而执行数据不是身份表 —— 它是上游节点写出来的产物
// （模板渲染、LLM 输出、点火时回读的结论），权威只有一张：customers 行。
// 这条测的就是"图里的一步伪造不了那把门"。
func TestReachSend_GateKeyComesFromCustomerRowNotExecutionData(t *testing.T) {
	f := newReachSendFixture(t)
	customerID, oneID, _ := f.seedCustomer(t)
	node := reachSendNode("r1", "reach.active", 3600)

	const forged = "uid-forged-" // 拼上 nonce 后是一个**不存在**的身份
	exec := newSOPExecution(t, f.db, 1, customerID, node.ID)
	exec.ExecutionData[reachNodeOneIDKey] = forged + gateNonce
	if err := f.db.Save(exec).Error; err != nil {
		t.Fatalf("写入伪造归因失败: %v", err)
	}

	ec := execCtxFor(exec, node)
	ec.ApprovalOutcome = verdictOutcome(model.ApprovalStatusApproved, true)
	res, err := NewReachSendExecutor().Execute(context.Background(), ec)
	if err != nil {
		t.Fatalf("不该以 error 上抛: %v", err)
	}
	if res == nil || res.Status != NodeStatusCompleted {
		t.Fatalf("这一条是要**发出去**的（身份伪造只该改不掉判定键，不该拦住发送），实际 %+v", res)
	}
	if f.gate.calls() != 1 {
		t.Fatalf("问门次数异常: %d", f.gate.calls())
	}
	if got := f.gate.asks[0].Key; got == forged+gateNonce {
		t.Fatal("判定键被执行数据里的伪造值顶掉了：闸门白名单从此可以按假身份授予")
	} else if got != oneID {
		t.Errorf("判定键必须是权威客户行上的 one_id，实际 %q（期望 %q）", got, oneID)
	}
	if got := f.gate.asks[0].OneID; got != oneID {
		t.Errorf("报告里的归因字段同样要来自客户行，实际 %q", got)
	}
}

// 节点配置里的渠道偏好要一路传到服务：图上写"这次走 telegram"，就不能自己漂回 sms。
//
// 只测"传参这一跳没断"，不重测选路（选路是 `ReachByCustomer` 的既有逻辑，本卡不改）。
// 之所以值得测：图配置落库再读出来是 JSON 形状（数组 = `[]any`），
// 而 `PreferredChannels` 要的是 `[]string` —— 这一跳的类型转换没有任何编译期保护。
func TestReachSend_PreferredChannelReachesTheService(t *testing.T) {
	f := newReachSendFixture(t)
	chatID, err := strconv.ParseInt("88"+gateNonce, 10, 64)
	if err != nil {
		t.Fatalf("构造 chatID 失败: %v", err)
	}
	n := atomic.AddInt64(&reachSendSeq, 1)
	customerID := fmt.Sprintf("cust-rs-pref-%d-%s", n, gateNonce)
	oneID := fmt.Sprintf("uid-rs-pref-%d-%s", n, gateNonce)
	if err := f.db.Create(&model.Customer{
		ID: customerID, UnifiedID: oneID,
		Phone: fmt.Sprintf("13%02d%s", n, gateNonce), TelegramChatID: chatID,
	}).Error; err != nil {
		t.Fatalf("建客户失败: %v", err)
	}

	node := reachSendNode("r1", "reach.active", 3600)
	node.Config["preferred_channels"] = []any{"telegram"} // JSON 回读后的真实形状
	res, err := f.runOn(t, node, customerID, verdictOutcome(model.ApprovalStatusApproved, true))
	if err != nil {
		t.Fatalf("不该以 error 上抛: %v", err)
	}
	if res == nil || res.Status != NodeStatusCompleted {
		t.Fatalf("偏好渠道该发出去，实际 %+v", res)
	}
	if len(f.spy.tg) != 1 || f.spy.tg[0] != fmt.Sprint(chatID) {
		t.Fatalf("图上写明的渠道必须被尊重，实际 %+v", *f.spy)
	}
	if len(f.spy.sms) != 0 {
		t.Errorf("偏好 telegram 时不该同时走 sms: %+v", *f.spy)
	}
	if got := f.gate.asks[0].Channel; got != "telegram" {
		t.Errorf("闸门要看得见最终渠道，实际 %q", got)
	}
}

// 执行行上没有 customer_id 时，归因键才是这条路的身份：`loadCustomer` 的第二档回退。
//
// 为什么值得单独一条，而不是"生产里 customer_id 必然非空"：`sop_executions.customer_id`
// 是 `not null` 却没有任何 default 约束，所以空串是一个合法的存量形状；
// 而节点这一侧唯一能替这条执行认出身份的就是 `execution_data.one_id`。不交它，服务两侧
// 都拿不到身份 ⇒ 一次本可以发出的触达静默变成"客户不存在"的失败。
func TestReachSend_ResolvesIdentityFromOneIDWhenExecutionHasNoCustomerID(t *testing.T) {
	f := newReachSendFixture(t)
	_, oneID, phone := f.seedCustomer(t)
	node := reachSendNode("r1", "reach.active", 3600)

	exec := newSOPExecution(t, f.db, 1, "", node.ID) // 空 customer_id 的执行行
	exec.ExecutionData[reachNodeOneIDKey] = oneID
	if err := f.db.Save(exec).Error; err != nil {
		t.Fatalf("写执行行失败: %v", err)
	}

	ec := execCtxFor(exec, node)
	ec.ApprovalOutcome = verdictOutcome(model.ApprovalStatusApproved, true)
	res, err := NewReachSendExecutor().Execute(context.Background(), ec)
	if err != nil {
		t.Fatalf("不该以 error 上抛: %v", err)
	}
	if res == nil || res.Status != NodeStatusCompleted {
		t.Fatalf("只有一手 one_id 时也要认得出人，实际 %+v", res)
	}
	if f.spy.total() != 1 || f.spy.sms[0] != phone {
		t.Fatalf("应发到这个客户的手机号，实际 %+v", *f.spy)
	}
	if got := f.gate.asks[0].Key; got != oneID {
		t.Errorf("判定键仍要由客户行确认，实际 %q", got)
	}
}

// —— AC①：退订客户零外发 —————————————————————————————————

// 拿着放行结论、闸门也放行，仍然被 DNC 拦下 —— 这条测的是「三判据是**串联**而不是任一命中即过」。
func TestReachSend_DoNotContactCustomerSendsNothing(t *testing.T) {
	f := newReachSendFixture(t)
	customerID, oneID, _ := f.seedCustomer(t)
	dnc := NewDoNotContactService(repository.NewCustomerDoNotContactRepository(f.db))
	if err := dnc.Block(context.Background(), oneID, model.DoNotContactChannelAll, "manual"); err != nil {
		t.Fatalf("写退订标志位失败: %v", err)
	}

	res, err := f.runOn(t, reachSendNode("r1", "reach.active", 3600), customerID,
		verdictOutcome(model.ApprovalStatusApproved, true))
	if err != nil {
		t.Fatalf("退订不该以 error 上抛（它是业务事实，不是故障）: %v", err)
	}
	if res == nil || res.Status != NodeStatusSkipped {
		t.Fatalf("退订客户应跳过外发，实际 %+v", res)
	}
	if got := fmt.Sprint(res.Output[outputKeyReachSkip]); got != "dnc" {
		t.Errorf("跳过理由要能区分退订与频控（两种处置动作相反），实际 %q", got)
	}
	if f.spy.total() != 0 {
		t.Errorf("退订客户不得有任何外发: %+v", *f.spy)
	}
}

// —— AC②：超频控零外发 ————————————————————————————————————

// 同一个客户第二次触达落在冷却窗内 ⇒ 零外发。
//
// 这条同时钉住一个选路口径：节点**不填** req.Phone / req.Email。`ReachByCustomer` 里
// 显式指定手机号的分支不过 `checkCooldown`（只过 DNC 与闸门），若节点能钉渠道，
// 频控这一判就被人手一条边旁路掉了。所以这里的断法是「行为上撞到冷却窗」，而不是「配置里没有 phone」。
func TestReachSend_InsideCooldownSendsNothing(t *testing.T) {
	f := newReachSendFixture(t)
	customerID, oneID, _ := f.seedCustomer(t)
	node := reachSendNode("r1", "reach.active", 3600)
	approved := verdictOutcome(model.ApprovalStatusApproved, true)

	// 第一发：占掉冷却窗（发送成功本身就会占）。
	if _, err := f.runOn(t, node, customerID, approved); err != nil {
		t.Fatalf("首发放行路径不该出错: %v", err)
	}
	if f.spy.total() != 1 {
		t.Fatalf("前置条件不成立：首发应真的外发，实际 %+v", *f.spy)
	}
	if f.reach.checkCooldown(context.Background(), oneID) {
		t.Fatal("前置条件不成立：首发后冷却窗应是占住的")
	}

	// 第二发：换一个执行、同一个人（等于同一 SOP 实例重跑），必须被频控拦下。
	res, err := f.runOn(t, node, customerID, approved)
	if err != nil {
		t.Fatalf("频控不该以 error 上抛: %v", err)
	}
	if res == nil || res.Status != NodeStatusSkipped {
		t.Fatalf("冷却窗内应跳过，实际 %+v", res)
	}
	if got := fmt.Sprint(res.Output[outputKeyReachSkip]); got != "cooldown" {
		t.Errorf("跳过理由要与退订分得开，实际 %q", got)
	}
	if f.spy.total() != 1 {
		t.Fatalf("冷却窗内不得二次外发，实际 %+v", *f.spy)
	}
}

// 重试/恢复重投同一节点不得二次外发（幂等键），与消息类节点同一口径。
func TestReachSend_AlreadySentIsNotSentAgain(t *testing.T) {
	f := newReachSendFixture(t)
	customerID, _, _ := f.seedCustomer(t)
	node := reachSendNode("r1", "reach.active", 3600)
	exec := newSOPExecution(t, f.db, 1, customerID, node.ID)
	exec.ExecutionData["_side_effects"] = []any{
		fmt.Sprintf("reach_sent:%d:r1", exec.ID),
	}

	ec := execCtxFor(exec, node)
	ec.ApprovalOutcome = verdictOutcome(model.ApprovalStatusApproved, true)
	res, err := NewReachSendExecutor().Execute(context.Background(), ec)
	if err != nil {
		t.Fatalf("幂等跳过不是错误: %v", err)
	}
	if res == nil || res.Status != NodeStatusSkipped {
		t.Fatalf("已发过的节点重跑应跳过，实际 %+v", res)
	}
	if f.spy.total() != 0 {
		t.Errorf("重跑不得二次外发: %+v", *f.spy)
	}
	if f.gate.calls() != 0 {
		t.Errorf("已发过就不该再问门，实际 %d 次", f.gate.calls())
	}
}

// —— 反永动机：发送前闸门拒了，不许再挂一次审批 ——————————————
//
// 图上审批已经批完（带着 approved 结论进来），却被 T-P3-07 那把门拦下。
// 此刻若再挂起，就会形成「批一次→发不出→再批一次」的死循环，
// 而每一圈都在待办中心留下一条新记录 —— 那比不发出去严重得多。
func TestReachSend_PreSendGateDenialDoesNotRePark(t *testing.T) {
	f := newReachSendFixture(t)
	f.gate.allowed = false
	f.gate.reason = "denied_by_whitelist"
	customerID, _, _ := f.seedCustomer(t)

	res, err := f.runOn(t, reachSendNode("r1", "reach.active", 3600), customerID,
		verdictOutcome(model.ApprovalStatusApproved, true))
	if err != nil {
		t.Fatalf("闸门拒绝不该以 error 上抛: %v", err)
	}
	if res == nil || res.Status != NodeStatusFailed {
		t.Fatalf("被发送前闸门拒了必须让这一条显形为失败，实际 %+v", res)
	}
	if res.Retryable {
		t.Errorf("白名单不会因为重试而自己长出条目，不该标可重试: %+v", res)
	}
	if !strings.Contains(res.ErrorMessage, "denied_by_whitelist") {
		t.Errorf("失败理由要带上闸门给的理由，实际 %q", res.ErrorMessage)
	}
	if f.spy.total() != 0 {
		t.Errorf("闸门拒绝后不得有任何外发: %+v", *f.spy)
	}
	if got := f.countApprovals(t, model.ApprovalStatusPending); got != 0 {
		t.Errorf("被发送前闸门拒了不该回头再挂一次审批，实际 pending %d 条", got)
	}
}

// —— 端到端：挂起 → 人工批准 → 定时器点火 → 同一条执行发出一次 ————
//
// 这条是整张卡最贵的证明：它同时要求
//
//	· 执行器第一次进会挂起（AC③），
//	· 调度器在**审批定时器点火的非 wait 节点**上把结论回读并递回执行器（新接的那一跳），
//	· 执行器拿到 approved 才去发（而不是又挂一次、或直接放行）。
//
// 少了中间那一跳，本用例会在「点火后又挂起、一条都没发」上红。
func TestReachSendEndToEnd_ApproveThenFireSendsExactlyOnce(t *testing.T) {
	f := newReachSendFixture(t)
	customerID, oneID, phone := f.seedCustomer(t)
	ctx := context.Background()

	graph := dto.SOPGraph{
		Name:  "t-p5-03",
		Entry: "start",
		Nodes: []dto.SOPNode{
			{ID: "start", Type: SOPNodeTypeStart, Next: []string{"r1"}},
			*reachSendNode("r1", "reach.active", 3600),
			{ID: "done", Type: SOPNodeTypeEnd},
		},
	}
	raw, err := json.Marshal(graph)
	if err != nil {
		t.Fatalf("编码图失败: %v", err)
	}
	agent := &model.SOPAgent{
		Name:        "t-p5-03-" + t.Name(),
		Scenario:    "test",
		TriggerType: SOPTriggerManual,
		SOPGraph:    toJSONMapBytes(raw),
		IsActive:    true,
	}
	if err := f.db.WithContext(ctx).Create(agent).Error; err != nil {
		t.Fatalf("建 SOP 失败: %v", err)
	}
	// 图必须能被既有校验接受：`SOPNodeSupportedTypes` 漏登记时这里就红，
	// 而不是等到运营在界面上保存不进去才发现。
	var parsed dto.SOPGraph
	if err := json.Unmarshal(mustJSON(agent.SOPGraph), &parsed); err != nil {
		t.Fatalf("回读图失败: %v", err)
	}
	for _, n := range parsed.Nodes {
		if !SOPNodeSupportedTypes[n.Type] {
			t.Fatalf("节点类型 %s 未登记进 SOPNodeSupportedTypes", n.Type)
		}
	}

	exec := newSOPExecution(t, f.db, agent.ID, customerID, "r1")
	ec := execCtxFor(exec, findNodeByID(&parsed, "r1"))
	first, err := NewReachSendExecutor().Execute(ctx, ec)
	if err != nil || first == nil || first.Status != NodeStatusWaiting {
		t.Fatalf("第一次进必须挂起，实际 %+v err=%v", first, err)
	}
	if f.spy.total() != 0 {
		t.Fatalf("挂起期不得外发: %+v", *f.spy)
	}

	var pending model.ApprovalRequest
	if err := f.db.WithContext(ctx).Where("status = ?", model.ApprovalStatusPending).
		First(&pending).Error; err != nil {
		t.Fatalf("读不到挂起时那条审批: %v", err)
	}
	if _, err := f.approval.Decide(ctx, pending.ID, ApprovalApprove, "ops-lead", "case-by-case"); err != nil {
		t.Fatalf("人工批准失败: %v", err)
	}

	// 换一批内存对象续跑：等于换进程（T-P3-02 的同一口径）。
	disp := NewSOPExecutionDispatcher(f.db, NewSOPService(f.db, nil), NewNodeExecutorRegistry(), nil)
	NewSOPOutboxDispatcher(f.db, senderFunc(func(task *dispatchTask) {
		disp.processTask(ctx, 0, task)
	})).processDueTimers(ctx)
	drainReachQueue(t, disp)

	if f.spy.total() != 1 || f.spy.sms[0] != phone {
		t.Fatalf("批准后方该恰好发一次，实际 %+v", *f.spy)
	}
	var decided model.SOPExecution
	if err := f.db.First(&decided, exec.ID).Error; err != nil {
		t.Fatalf("回读执行失败: %v", err)
	}
	if decided.Status != SOPStatusSuccess {
		t.Errorf("发出后流程该走完，实际执行状态 %s", decided.Status)
	}
	data, err := json.Marshal(decided.ExecutionData)
	if err != nil {
		t.Fatalf("序列化执行数据失败: %v", err)
	}
	if !strings.Contains(string(data), "reach_sent:") {
		t.Errorf("幂等键要随执行数据落库（重启后重投才拦得住），实际 %s", data)
	}
	// 归因口径：图上的审批腿问的是「这条执行的这个节点」（subject = execution+node，
	// 见 EncodeApprovalSubject），客户身份由**发送侧**那把闸门问出 —— 它拿的是客户行上的
	// one_id，而不是执行数据里的某个键。这里断的是后者：换过进程、由调度器重新装配的
	// 那一次发送，判定对象仍然是这个客户的 one_id（回落成 customer_id 就是静默换键）。
	if n := f.gate.calls(); n != 1 {
		t.Errorf("点火后的这一次发送该恰好问一次闸门，实际 %d 次", n)
	} else if got := f.gate.asks[0].Key; got != oneID {
		t.Errorf("闸门判定键该是客户的 one_id，实际 %q", got)
	}
}

func drainReachQueue(t *testing.T, disp *SOPExecutionDispatcher) {
	t.Helper()
	for i := 0; i < 32; i++ {
		select {
		case task := <-disp.dispatchQueue:
			disp.processTask(context.Background(), 0, task)
		default:
			return
		}
	}
	t.Fatalf("派发队列在 %d 轮后排空不掉", 32)
}

// —— 注册面：图能存 ≠ 执行器在，缺后者时 Noop 兜底会静默「完成」 ——————————

func TestReachSend_RegisteredInExecutorRegistry(t *testing.T) {
	reg := NewNodeExecutorRegistry()
	RegisterAllNodeExecutors(reg, &SOPNodeExecutorDeps{})
	exec, err := reg.Get(context.Background(), SOPNodeTypeReachSend)
	if err != nil {
		t.Fatalf("外发节点未注册：MustGet 会退到 Noop 兜底，节点静默 completed 而一条都不发: %v", err)
	}
	if _, ok := exec.(*ReachSendExecutor); !ok {
		t.Errorf("外发节点类型应为 *ReachSendExecutor，实际 %T", exec)
	}
	if !SOPNodeSupportedTypes[SOPNodeTypeReachSend] {
		t.Errorf("外发节点未登记进 SOPNodeSupportedTypes，图存不下来")
	}
}

// 全局 sender 的读写必须成对：装配点漏接（或撤装没撤干净）都会让节点读到上一个实例。
func TestSOPReachSender_SetAndGetRoundTrip(t *testing.T) {
	f := newReachSendFixture(t)
	if GetSOPReachSender() != ActiveReachSender(f.reach) {
		t.Fatal("夹具装配后 GetSOPReachSender 应返回同一个实例")
	}
	SetSOPReachSender(nil)
	if GetSOPReachSender() != nil {
		t.Fatal("撤装后必须读到 nil（nil 时节点判失败，不能继续用旧实例）")
	}
}

// —— 反向测试（卡面点名：绕过路径必须被测试抓到） ——————————————
//
// 静态扫过**所有定义节点执行器的文件**（按「有没有 NodeType() string 方法」现场判定，
// 不维护文件白名单，所以新增一个执行器文件会自动落在扫描面里）。
// 判据：这些文件里不许出现任何「客户侧出域」的符号 —— 既不许直接调渠道发送器
// （sendSMS 等 9 个是 ProactiveReachService 的私有出口），也不许自己构造渠道服务
// （NewSmsService 等：那等于绕过选路与三判据直接落到渠道 SDK）。
// 唯一放行的出域入口是 `ReachByCustomer`。
//
// 覆盖面要说清（否则这条门会被当成「什么都能拦」）：它拦的是**在这一层代码里点名**渠道出口，
// 拦不住「另起一个不被 NodeType() 认出来的执行器」或「经反射/间接层调用」。
// 前者的兜底是节点必须经 registry 注册且未注册即 Noop（不发），后者不在本卡射程内。
func TestNodeExecutorFiles_HaveNoCustomerOutboundBeyondReachByCustomer(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("读包目录失败: %v", err)
	}
	fset := token.NewFileSet()
	var scanned []string
	var reachCalls int
	var violations []string

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, perr := parser.ParseFile(fset, name, nil, 0)
		if perr != nil {
			t.Fatalf("解析 %s 失败: %v", name, perr)
		}
		if !declaresNodeTypeMethod(file) {
			continue
		}
		scanned = append(scanned, name)
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch {
			case sel.Sel.Name == "ReachByCustomer":
				reachCalls++
			case outboundChannelSymbols[sel.Sel.Name]:
				violations = append(violations, fmt.Sprintf("%s:%d 直接调了客户侧出域符号 %s",
					name, fset.Position(call.Pos()).Line, sel.Sel.Name))
			}
			return true
		})
	}

	if len(scanned) == 0 {
		t.Fatal("扫描面为空：这条门什么都没见过（执行器判据或目录假设变了）")
	}
	if reachCalls == 0 {
		t.Fatalf("扫描面 %v 里没有任何 ReachByCustomer 调用：Active 出域唯一出口已不在，"+
			"这条门就退化成恒绿的摆设", scanned)
	}
	if len(violations) > 0 {
		t.Fatalf("节点执行层出现了绕过三判据的出域调用：\n  %s", strings.Join(violations, "\n  "))
	}
}

func declaresNodeTypeMethod(file *ast.File) bool {
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || fn.Name.Name != "NodeType" {
			continue
		}
		if fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
			continue
		}
		if id, ok := fn.Type.Results.List[0].Type.(*ast.Ident); ok && id.Name == "string" {
			return true
		}
	}
	return false
}

// outboundChannelSymbols 客户侧出域符号：`ProactiveReachService` 的 9 个私有发送出口
// + 各渠道服务的构造函数（自己造渠道服务 = 自己发，且必然过不了选路与三判据）。
var outboundChannelSymbols = map[string]bool{
	"sendSMS": true, "sendEmail": true, "sendTelegram": true, "sendWhatsApp": true,
	"sendWeCom": true, "sendWeChat": true, "sendFeishu": true, "sendDingTalk": true,
	"sendBridge":    true,
	"NewSmsService": true, "NewEmailService": true,
	"NewTelegramIntegrationService": true, "NewWhatsAppCloudIntegrationService": true,
	"NewWeChatIntegrationService": true, "NewFeishuIntegrationService": true,
	"NewDingTalkIntegrationService": true,
}

// —— 配置面：内容取不到时不发、也不挂 ——————————————————————

// 没填 content（或模板变量全空）时：判失败、零外发、也不挂审批。
// 挂一次审批再发现没内容可发，等于让审批人批一条空气。
func TestReachSend_EmptyContentFailsBeforeApproval(t *testing.T) {
	f := newReachSendFixture(t)
	customerID, _, _ := f.seedCustomer(t)
	node := &dto.SOPNode{
		ID: "r1", Type: SOPNodeTypeReachSend, Next: []string{"done"},
		Config: map[string]any{"approval_policy_key": "reach.active"},
	}

	res, err := f.runOn(t, node, customerID, nil)
	if err != nil {
		t.Fatalf("配置错误不该以 error 上抛: %v", err)
	}
	if res == nil || res.Status != NodeStatusFailed {
		t.Fatalf("空内容必须判失败，实际 %+v", res)
	}
	if res.Retryable {
		t.Errorf("空内容重试一百次还是空的: %+v", res)
	}
	if f.spy.total() != 0 {
		t.Errorf("空内容不得外发: %+v", *f.spy)
	}
	if got := f.countApprovals(t, model.ApprovalStatusPending); got != 0 {
		t.Errorf("没有内容就不该占用审批位，实际 pending %d 条", got)
	}
}

// 模板内容从执行数据里取值（与消息类节点同一渲染口径），且渲染结果真的到了渠道出口。
func TestReachSend_ContentRendersFromExecutionData(t *testing.T) {
	f := newReachSendFixture(t)
	customerID, _, _ := f.seedCustomer(t)
	node := reachSendNode("r1", "reach.active", 3600)
	node.Config["content"] = "您好 {{name}}，方案已就绪"

	var gotContent string
	f.reach.SetSMSRegistry(func() (func(ctx context.Context, phone, content, templateID string, params map[string]string) (string, error), error) {
		return func(_ context.Context, _, content, _ string, _ map[string]string) (string, error) {
			gotContent = content
			return "sms_render", nil
		}, nil
	})

	exec := newSOPExecution(t, f.db, 1, customerID, node.ID)
	exec.ExecutionData["name"] = "张先生"
	ec := execCtxFor(exec, node)
	ec.ApprovalOutcome = verdictOutcome(model.ApprovalStatusApproved, true)

	if _, err := NewReachSendExecutor().Execute(context.Background(), ec); err != nil {
		t.Fatalf("放行路径不该出错: %v", err)
	}
	if want := "您好 张先生，方案已就绪"; gotContent != want {
		t.Errorf("外发内容应是渲染后的模板，实际 %q（期望 %q）", gotContent, want)
	}
}

// 缺 approval_policy_key 时不挂起、不放行：审批入队没有策略就无法落库。
// 这条把「图作者忘了配」与「运行时未装配」分得开（后者红在另一个用例上）。
func TestReachSend_MissingPolicyKeyFailsClosed(t *testing.T) {
	f := newReachSendFixture(t)
	customerID, _, _ := f.seedCustomer(t)
	node := reachSendNode("r1", "", 3600)

	res, err := f.runOn(t, node, customerID, nil)
	if err != nil {
		t.Fatalf("缺策略键不该以 error 上抛: %v", err)
	}
	if res == nil || res.Status != NodeStatusFailed {
		t.Fatalf("缺 approval_policy_key 必须判失败，实际 %+v", res)
	}
	if res.Retryable {
		t.Errorf("缺配置不会因重试而补上: %+v", res)
	}
	if f.spy.total() != 0 {
		t.Errorf("缺策略键不得外发: %+v", *f.spy)
	}
}

// 没接外发服务（装配点漏了 SetSOPReachSender）⇒ 判失败而不是静默跳过。
// 与审批运行时同一口径：一把不能把关的门必须把门关上，也绝不能长得像「已经发过了」。
func TestReachSend_SenderUnwiredFailsClosed(t *testing.T) {
	f := newReachSendFixture(t)
	customerID, _, _ := f.seedCustomer(t)
	SetSOPReachSender(nil)

	res, err := f.runOn(t, reachSendNode("r1", "reach.active", 3600), customerID,
		verdictOutcome(model.ApprovalStatusApproved, true))
	if err != nil {
		t.Fatalf("未装配不该以 error 上抛: %v", err)
	}
	if res == nil || res.Status != NodeStatusFailed {
		t.Fatalf("外发服务未装配必须判失败，实际 %+v", res)
	}
	if f.spy.total() != 0 {
		t.Errorf("未装配不得有任何外发: %+v", *f.spy)
	}
}

// 编译期哨兵：外发节点必须实现 NodeExecutor 与 CompensationNoter
// （补偿穷尽性契约的入册方式：两者皆无会被 TestNodeExecutor_CompensationInventory 判红）。
var (
	_ NodeExecutor      = (*ReachSendExecutor)(nil)
	_ CompensationNoter = (*ReachSendExecutor)(nil)
)
