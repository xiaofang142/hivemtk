package service

// T-P3-07 AC①②③④：ReachByCustomer 内部的发送前审批钩子契约。
//
// 本文件只测 service 侧的**机制**（何时问、问谁、拒了有什么后果）；三态旗子、刹车、
// 白名单与计数器归 internal/app 的接线层测（reach_gate_wiring_test.go）。
// 这么切的理由：机制层必须与裁决来源无关 —— 将来把授权源换掉（内存白名单 → 审批工单表）
// 时，这些断言仍该逐字成立。

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// gateNonce 给每个测试进程一批独占身份串。
// 冷却键写的是全局缓存：测试里通常是进程内内存缓存，但同包别的用例可能把全局后端换成
// Redis —— 那时固定身份会在一小时冷却窗内跨运行残留，把"拒发后窗口仍是空的"这类
// 对照断言变成假红。
var gateNonce = fmt.Sprint(time.Now().UnixNano() % 1e9)

// stubReachGate 记录每次被问的入参，并按预设回裁决。
//
// 它刻意不区分 shadow/block：服务侧只看 allowed 一个布尔，"记录但放行"是接线层的行为。
type stubReachGate struct {
	asks    []ReachSubject
	allowed bool
	reason  string
}

func (g *stubReachGate) CheckReachPreSend(_ context.Context, s ReachSubject) (bool, string) {
	g.asks = append(g.asks, s)
	return g.allowed, g.reason
}

func (g *stubReachGate) calls() int { return len(g.asks) }

// reachSendSpy 记录"真的外发了"的调用，按渠道分开存。
// 分开存是为了让"零外发"能对每个出口分别断言，而不是对一个混合计数断言。
type reachSendSpy struct {
	sms   []string
	email []string
	tg    []string
}

func (s *reachSendSpy) total() int { return len(s.sms) + len(s.email) + len(s.tg) }

// newSpyReachService 造一个"三个发送器全指向 spy"的服务实例，返回它与配套的库。
//
// 库是真的（testutil），不是假的：本服务的三条外发出口在发送前都要读退订标志位，
// 用无库实例会走进仓库的"退到进程全局句柄"分支 —— 在测试进程里那个句柄是 nil，
// 直接 panic；就算不 panic，"退订检查有没有真的生效"也就无从断言了。
// 表里连 RecoveryQueue 一起建，是为了让 cron 路径（recovery_queue_worker_gate_test.go）
// 能在**同一个库**里入队并由同一个服务实例消费 —— 用两个库测出来的"零外发"不算数。
func newSpyReachService(t *testing.T, spy *reachSendSpy) (*ProactiveReachService, *gorm.DB) {
	t.Helper()
	// NewTestDB 会 DROP 再重建传入的表（同进程共享一个测试库），所以一个用例里**只能调一次**：
	// 第二次调用会把第一个服务刚种下的客户抹掉（实测表现为 "customer not found" 假红）。
	db := testutil.NewTestDB(t, &model.Customer{}, &model.CustomerChannel{},
		&model.CustomerDoNotContact{}, &model.RecoveryQueue{})
	return newSpyReachServiceOn(db, spy), db
}

// newSpyReachServiceOn 在**已建好表**的库上装配同一套 spy 出口。
//
// 需要两个服务实例的用例（比如"换个入参、键必须不变"）共用这一个库：
// 判定键只由客户身份与渠道决定，多开一个库只会把第一个库的现场清掉。
func newSpyReachServiceOn(db *gorm.DB, spy *reachSendSpy) *ProactiveReachService {
	// telegram 要 accountLookup 给出活跃账号，这里用 mockAccountLookup 满足它。
	svc := NewProactiveReachService(db, &mockAccountLookup{
		supported: map[string]bool{"telegram": true},
		results:   map[string]string{"telegram": "acc-tg-1"},
	})
	svc.SetSMSRegistry(func() (func(ctx context.Context, phone, content, templateID string, params map[string]string) (string, error), error) {
		return func(_ context.Context, phone, _, _ string, _ map[string]string) (string, error) {
			spy.sms = append(spy.sms, phone)
			return "sms_spy", nil
		}, nil
	})
	svc.SetEmailRegistry(func(_ context.Context, _ uint, to, _, _ string, _ []string) (string, error) {
		spy.email = append(spy.email, to)
		return "email_spy", nil
	})
	svc.SetTelegramRegistry(func(_ context.Context, _ uint, chatID int64, _ string) error {
		spy.tg = append(spy.tg, fmt.Sprint(chatID))
		return nil
	})
	return svc
}

func TestReachGate_NotInstalledSendsWithoutAsking(t *testing.T) {
	spy := &reachSendSpy{}
	svc, _ := newSpyReachService(t, spy)

	// AC④ 的地基：没装钩子时（旗子 off，或某个没接线的装配点）外发行为与接线前逐字一致。
	if _, err := svc.ReachByCustomer(context.Background(), &ProactiveReachRequest{
		Phone: "139" + gateNonce, Content: "hi",
	}); err != nil {
		t.Fatalf("未装钩子时不该出错: %v", err)
	}
	if len(spy.sms) != 1 {
		t.Fatalf("未装钩子应照常外发，实际 %v", spy.sms)
	}
}

func TestReachGate_DeniedOnExplicitPhoneBranchSendsNothing(t *testing.T) {
	spy := &reachSendSpy{}
	svc, _ := newSpyReachService(t, spy)
	gate := &stubReachGate{allowed: false, reason: "denied_default"}
	svc.SetPreSendApprovalChecker(gate)

	phone := "138" + gateNonce
	_, err := svc.ReachByCustomer(context.Background(), &ProactiveReachRequest{Phone: phone, Content: "hi"})
	if err == nil {
		t.Fatal("闸门拒绝时短信分支必须返回错误")
	}
	if !errors.Is(err, ErrReachApprovalDenied) {
		t.Fatalf("错误必须是可分支判断的哨兵，实际 %v", err)
	}
	if len(spy.sms) != 0 {
		t.Fatalf("闸门拒绝后不得有任何外发，实际 %v", spy.sms)
	}
	if gate.calls() != 1 {
		t.Fatalf("一次外发应且只应问一次闸门，实际 %d 次", gate.calls())
	}
	// 收件人不进错误串：这条错误会被调用方写进台账与日志（见 recovery_queue_worker.go 文件头）。
	if strings.Contains(err.Error(), phone) {
		t.Errorf("错误里不该带收件人标识: %v", err)
	}
	if !strings.Contains(err.Error(), "denied_default") {
		t.Errorf("错误里要带上可处置的理由: %v", err)
	}
}

func TestReachGate_DeniedOnExplicitEmailBranchSendsNothing(t *testing.T) {
	spy := &reachSendSpy{}
	svc, _ := newSpyReachService(t, spy)
	gate := &stubReachGate{allowed: false, reason: "denied_explicit"}
	svc.SetPreSendApprovalChecker(gate)

	_, err := svc.ReachByCustomer(context.Background(), &ProactiveReachRequest{
		Email: "Lead" + gateNonce + "@Example.com", Content: "hi",
	})
	if !errors.Is(err, ErrReachApprovalDenied) {
		t.Fatalf("邮件分支同样要受闸门约束，实际 %v", err)
	}
	if len(spy.email) != 0 {
		t.Fatalf("闸门拒绝后不得有任何外发，实际 %v", spy.email)
	}
	if gate.calls() != 1 {
		t.Fatalf("一次外发应且只应问一次闸门，实际 %d 次", gate.calls())
	}
	// 邮件地址要归一后再进键：同一收件人换个大小写就在白名单里过掉，是假闸门的另一种写法。
	if got := gate.asks[0].Key; !strings.HasPrefix(got, "email:") || strings.Contains(got, "Lead") {
		t.Errorf("邮件判定键应为归一后的 email:<地址>，实际 %q", got)
	}
}

// AC②：判定键必须是客户身份（one_id 优先），而不是恒为空的 accountID。
//
// 这条是整张卡的核心。T-P1-05 的教训是"用一把键恒为空的白名单去判，等于没判"。
// 所以这里不止断键非空，还断**只改 req.AccountID 不改键** —— 若实现偷用了 AccountID，
// 短信/邮件分支（装配里从不填 AccountID）会得到恒空键，白名单要么全放要么全拒。
func TestReachGate_SubjectKeyUsesCustomerIdentityNotAccountID(t *testing.T) {
	ctx := context.Background()
	const acc = "acc-some-channel-account"
	oneA, oneB := "uid-gate-a-"+gateNonce, "uid-gate-b-"+gateNonce

	spy := &reachSendSpy{}
	svc, db := newSpyReachService(t, spy)
	// 两个身份各发一次：冷却窗按 one_id 记，同一身份连发两次必然第二次撞冷却（闸门虽在冷却之前
	// 被问，但那条路径就成了"被问两次却只发一次"，测不出"每次外发各问一次"）。
	seed := func(id, oneID, phone string) {
		t.Helper()
		if err := db.Create(&model.Customer{ID: id, UnifiedID: oneID, Phone: phone}).Error; err != nil {
			t.Fatalf("建客户失败: %v", err)
		}
	}
	seed("cust-gate-a", oneA, "137"+gateNonce)
	seed("cust-gate-b", oneB, "126"+gateNonce)
	gate := &stubReachGate{allowed: true, reason: "whitelisted"}
	svc.SetPreSendApprovalChecker(gate)

	// A：请求里没有 AccountID —— 生产装配的真实形状（controller 与 worker 都不填）。
	if _, err := svc.ReachByCustomer(ctx, &ProactiveReachRequest{CustomerID: "cust-gate-a", Content: "hi"}); err != nil {
		t.Fatalf("放行路径不该出错: %v", err)
	}
	// B：同样的路径，额外塞一个 AccountID。
	if _, err := svc.ReachByCustomer(ctx, &ProactiveReachRequest{
		CustomerID: "cust-gate-b", AccountID: acc, Content: "hi",
	}); err != nil {
		t.Fatalf("放行路径不该出错: %v", err)
	}

	if gate.calls() != 2 {
		t.Fatalf("两次外发应各问一次闸门，实际 %d 次", gate.calls())
	}
	got := gate.asks[0]
	if got.Key == "" {
		t.Fatal("判定键不能为空：空键的白名单判了等于没判")
	}
	if got.Key != oneA {
		t.Errorf("有 one_id 时判定键应是 one_id，实际 %q（期望 %q）", got.Key, oneA)
	}
	if got.Channel != "sms" || got.Recipient == "" {
		t.Errorf("判定入参要带上已选定的渠道与收件人，实际 %+v", got)
	}
	if gate.asks[1].Key != oneB {
		t.Errorf("塞了 AccountID 后键仍应是该客户自己的 one_id，实际 %q（期望 %q）", gate.asks[1].Key, oneB)
	}
	for _, a := range gate.asks {
		if strings.Contains(a.Key, acc) {
			t.Errorf("AccountID 不得参与判定键（它在短信/邮箱渠道恒为空）: %+v", a)
		}
	}
}

// 只有 customer_id、库里没有 one_id 时，键退到 customer_id；两者都没有（直发手机号）
// 才退到"渠道:收件人"。三档都必须非空。
func TestReachGate_SubjectKeyFallbackOrder(t *testing.T) {
	spy := &reachSendSpy{}
	svc, db := newSpyReachService(t, spy)
	gate := &stubReachGate{allowed: true, reason: "whitelisted"}
	svc.SetPreSendApprovalChecker(gate)
	ctx := context.Background()

	// 档一：无客户身份 ⇒ "渠道:收件人"
	phone := "136" + gateNonce
	if _, err := svc.ReachByCustomer(ctx, &ProactiveReachRequest{
		Phone: phone, AccountID: "acc-ignored", Content: "hi",
	}); err != nil {
		t.Fatalf("放行路径不该出错: %v", err)
	}
	if gate.calls() != 1 {
		t.Fatalf("应问一次闸门，实际 %d 次", gate.calls())
	}
	if got, want := gate.asks[0].Key, "sms:"+phone; got != want {
		t.Errorf("无客户身份时键应为 %q，实际 %q", want, got)
	}

	// 档二：有 customer_id、行上 one_id 为空 ⇒ 退到 customer_id（仍须非空）。
	// BeforeCreate 钩子会给空 UnifiedID 自动补一个（见 model/customer.go），所以这里显式清空，
	// 并回读确认"库里真的没有 one_id"—— 否则这条测的是另一档。
	noOneID := "cust-nooneid-" + gateNonce
	if err := db.Create(&model.Customer{ID: noOneID, Phone: "129" + gateNonce}).Error; err != nil {
		t.Fatalf("建客户失败: %v", err)
	}
	if err := db.Model(&model.Customer{}).Where("id = ?", noOneID).Update("unified_id", "").Error; err != nil {
		t.Fatalf("清空 one_id 失败: %v", err)
	}
	var blank model.Customer
	if err := db.Select("id", "unified_id").Where("id = ?", noOneID).First(&blank).Error; err != nil {
		t.Fatalf("回读客户失败: %v", err)
	}
	if blank.UnifiedID != "" {
		t.Fatalf("前置条件不成立：one_id 仍为 %q，本用例测不到 customer_id 这一档", blank.UnifiedID)
	}
	if _, err := svc.ReachByCustomer(ctx, &ProactiveReachRequest{CustomerID: noOneID, Content: "hi"}); err != nil {
		t.Fatalf("放行路径不该出错: %v", err)
	}
	if gate.calls() != 2 {
		t.Fatalf("两次外发应各问一次闸门，实际 %d 次", gate.calls())
	}
	if got := gate.asks[1].Key; got != noOneID {
		t.Errorf("无 one_id 时键应退到 customer_id，实际 %q", got)
	}
}

// DryRun 什么都不发，因此不过闸门：拦一个本来就不外发的动作没有语义，
// 而挽回 worker 的 shadow 轮全靠 dry-run 做观察 —— 把它变成拒绝工具就自毁观察面。
func TestReachGate_DryRunIsNotGated(t *testing.T) {
	ctx := context.Background()
	oneID := "uid-dry-" + gateNonce

	spy := &reachSendSpy{}
	svc, db := newSpyReachService(t, spy)
	if err := db.Create(&model.Customer{
		ID: "cust-dry-" + gateNonce, UnifiedID: oneID, Phone: "135" + gateNonce,
	}).Error; err != nil {
		t.Fatalf("建客户失败: %v", err)
	}
	gate := &stubReachGate{allowed: false, reason: "denied_default"}
	svc.SetPreSendApprovalChecker(gate)

	resp, err := svc.ReachByCustomer(ctx, &ProactiveReachRequest{OneID: oneID, Content: "hi", DryRun: true})
	if err != nil {
		t.Fatalf("dry-run 不该被闸门拦下: %v", err)
	}
	if resp.Status != "dry_run" {
		t.Errorf("dry-run 状态异常: %+v", resp)
	}
	if gate.calls() != 0 {
		t.Errorf("dry-run 不外发 ⇒ 不该问闸门，实际问了 %d 次", gate.calls())
	}
	if spy.total() != 0 {
		t.Errorf("dry-run 不得外发: %+v", *spy)
	}
}

// 拒发必须发生在**写冷却键之前**：闸门说"不许发"之后还占掉一小时冷却窗，
// 等于让一次没发生的外发产生了与真外发相同的后续效果（运营补授权后要再等一小时）。
//
// 一个身份只能探一次：冷却检查本身是 SetNX（"探"就等于"占"），所以断"被拦的身份窗口是空的"
// 之后，同一条身份再发一次必然撞自己刚占的窗 —— 对照组因此用**另一个身份**。
func TestReachGate_RefusalLeavesCooldownWindowFree(t *testing.T) {
	ctx := context.Background()
	deniedOne := "uid-win-d-" + gateNonce
	controlOne := "uid-win-c-" + gateNonce

	spy := &reachSendSpy{}
	svc, db := newSpyReachService(t, spy)
	seed := func(id, oneID, phone string) {
		t.Helper()
		if err := db.Create(&model.Customer{ID: id, UnifiedID: oneID, Phone: phone}).Error; err != nil {
			t.Fatalf("建客户失败: %v", err)
		}
	}
	seed("cust-win-d-"+gateNonce, deniedOne, "134"+gateNonce)
	seed("cust-win-c-"+gateNonce, controlOne, "128"+gateNonce)

	svc.SetPreSendApprovalChecker(&stubReachGate{allowed: false, reason: "denied_default"})
	if _, err := svc.ReachByCustomer(ctx, &ProactiveReachRequest{OneID: deniedOne, Content: "hi"}); !errors.Is(err, ErrReachApprovalDenied) {
		t.Fatalf("闸门拒绝时应返回哨兵错误: %v", err)
	}
	if spy.total() != 0 {
		t.Fatalf("拒发后不得有外发: %+v", *spy)
	}
	if !svc.checkCooldown(ctx, deniedOne) {
		t.Fatal("闸门拒发不该占用冷却窗（授权补上后要能立刻重发）")
	}

	// 对照组：放行的闸门 + 另一个身份 ⇒ 真发出去 ⇒ 冷却窗必须被占用。
	// 少了这一句，上面那句"窗口是空的"在"冷却压根没生效"的情况下同样成立。
	svc.SetPreSendApprovalChecker(&stubReachGate{allowed: true, reason: "whitelisted"})
	if _, err := svc.ReachByCustomer(ctx, &ProactiveReachRequest{OneID: controlOne, Content: "hi"}); err != nil {
		t.Fatalf("对照组的发送应成功: %v", err)
	}
	if len(spy.sms) != 1 {
		t.Fatalf("对照组应真的外发: %+v", *spy)
	}
	if svc.checkCooldown(ctx, controlOne) {
		t.Fatal("真外发后冷却窗必须被占用（否则上面的空窗口断言没有反证）")
	}
}

// 闸门问在选路之后、外发之前：它必须看得见最终渠道与收件人，
// 否则报告里只有一把 one_id，运营无法回答"这次要发到哪"。
func TestReachGate_AskedOncePerSendOnChannelPickedFromCustomer(t *testing.T) {
	ctx := context.Background()
	oneID := "uid-tg-" + gateNonce
	chatID, err := strconv.ParseInt("77"+gateNonce, 10, 64)
	if err != nil {
		t.Fatalf("构造 chatID 失败: %v", err)
	}

	spy := &reachSendSpy{}
	svc, db := newSpyReachService(t, spy)
	if err := db.Create(&model.Customer{
		ID: "cust-tg-" + gateNonce, UnifiedID: oneID,
		Phone: "133" + gateNonce, TelegramChatID: chatID,
	}).Error; err != nil {
		t.Fatalf("建客户失败: %v", err)
	}
	gate := &stubReachGate{allowed: false, reason: "denied_default"}
	svc.SetPreSendApprovalChecker(gate)

	// 偏好 telegram：闸门拒 ⇒ 该渠道零外发（这条同时是"外发唯一出口都被覆盖"的证据）。
	_, err = svc.ReachByCustomer(ctx, &ProactiveReachRequest{
		OneID: oneID, Content: "hi", PreferredChannels: []string{"telegram"},
	})
	if !errors.Is(err, ErrReachApprovalDenied) {
		t.Fatalf("telegram 渠道同样要过闸门，实际 %v", err)
	}
	if len(spy.tg) != 0 || len(spy.sms) != 0 {
		t.Fatalf("拒发后不得有任何外发: %+v", *spy)
	}
	if gate.calls() != 1 {
		t.Fatalf("一次外发只问一次闸门（别在选路和发送两侧各问一遍），实际 %d 次", gate.calls())
	}
	if got := gate.asks[0]; got.Channel != "telegram" {
		t.Errorf("闸门要看得见最终渠道，实际 %+v", got)
	}
}

// 顺序契约：DNC（永久事实）先于闸门（可补授权）判。
// 退订客户不该被问到闸门上 —— 否则 would_deny 报告会把"人家退订了"混进"没人给他开授权"。
func TestReachGate_NotAskedForDoNotContactRecipient(t *testing.T) {
	ctx := context.Background()
	oneID := "uid-dnc-" + gateNonce

	spy := &reachSendSpy{}
	svc, db := newSpyReachService(t, spy)
	if err := db.Create(&model.Customer{
		ID: "cust-dnc-" + gateNonce, UnifiedID: oneID, Phone: "132" + gateNonce,
	}).Error; err != nil {
		t.Fatalf("建客户失败: %v", err)
	}
	dncSvc := NewDoNotContactService(repository.NewCustomerDoNotContactRepository(db))
	if err := dncSvc.Block(ctx, oneID, model.DoNotContactChannelAll, "manual"); err != nil {
		t.Fatalf("写退订标志位失败: %v", err)
	}
	svc.SetDoNotContact(dncSvc)
	gate := &stubReachGate{allowed: true, reason: "whitelisted"}
	svc.SetPreSendApprovalChecker(gate)

	_, err := svc.ReachByCustomer(ctx, &ProactiveReachRequest{OneID: oneID, Content: "hi"})
	if !errors.Is(err, ErrDoNotContact) {
		t.Fatalf("退订应仍由 DNC 拦下: %v", err)
	}
	if gate.calls() != 0 {
		t.Errorf("退订客户不该问到闸门，实际 %d 次", gate.calls())
	}
}

// 判定键算不出来时**拒发、且不去问门**：出口处的 fail-closed。
//
// 这条分支在正常装配路径上不可达（`reachApprovalKey` 的三档回退保证键非空），所以只能
// 直接调用钉住 —— 而它恰恰是"键算不出来时怎么办"的唯一决断：M2 变异（把它改成放行）
// 让上面 9 条按真实身份测的用例一条都不红，因为那些用例的键本来就非空。
// 留着它不放，等于把"算不出身份就外发"这条路留给下一个改 `reachApprovalKey` 的人。
func TestReachGate_EmptySubjectKeyFailsClosed(t *testing.T) {
	spy := &reachSendSpy{}
	svc, _ := newSpyReachService(t, spy)
	// 闸门预设为"放行"：只有走到判定键这一步之前就被拒，才能证明拒的是键而不是裁决。
	gate := &stubReachGate{allowed: true, reason: "whitelisted"}
	svc.SetPreSendApprovalChecker(gate)
	ctx := context.Background()

	if err := svc.enforcePreSendApproval(ctx, ReachSubject{Channel: "sms", Recipient: "13800000000"}); !errors.Is(err, ErrReachApprovalDenied) {
		t.Fatalf("键为空必须拒发（fail-closed），实际 %v", err)
	}
	if gate.calls() != 0 {
		t.Errorf("连判定对象都没有时不该去问白名单（问了也是拿空键去查），实际问了 %d 次", gate.calls())
	}
	if spy.total() != 0 {
		t.Errorf("拒发不得有任何外发: %+v", *spy)
	}

	// 对照组：同一个服务、同一把"放行"闸门，键非空即问门且不报错。
	// 没有这一半，上面那句"拒了"可能只是因为闸门根本没接上。
	if err := svc.enforcePreSendApproval(ctx, ReachSubject{Key: "uid-" + gateNonce, Channel: "sms", Recipient: "13800000000"}); err != nil {
		t.Fatalf("键非空且闸门放行时应无错: %v", err)
	}
	if gate.calls() != 1 {
		t.Errorf("键非空时应问一次闸门，实际 %d 次", gate.calls())
	}
}
