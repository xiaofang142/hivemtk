package app

// reach_rollout_wiring_test.go —— T-P5-04：把 durable 放量档接到外发闸门上。
//
// 本文件只断一件事的两种方向：**"门挂没挂链"仍由 FF_LTC_REACH_GATE 决定，
// "拦谁"改由 ltc.config 的 reach_rollout 决定**。两把旗子各管一段，任何一段单独能改
// 现网行为都是错的：
//   - 档位能拦链 ⇒ env=shadow 的观察期被一次写库悄悄变成阻断（AC① 的对比数据就此作废）；
//   - 档位不生效 ⇒ AC② 的"一键回滚 = 关开关、不回滚代码"是句空话。
//
// 与 reach_gate_wiring_test.go 同一条纪律：真 checker、真 featureflag、真触达服务，
// 外发出口换成 spy。这里再加一条：**配置也走真的 LTCConfigService + 假 KV 存储**，
// 因为本卡的成败正好落在"闸门读的是每请求的最新档位"还是"装配期抄了一份快照"。
//
// 卡面 AC 与用例的对应：
//
//	① 每阶段有对比数据 → 档位读数进快照（…SnapshotNamesModeAndCoupling）；
//	   送达率/投诉率两条实测算不出来，已在 service 侧观测用例里显式声明 unavailable
//	② 一键回滚 = 关开关、不回滚代码 → …HaltBeatsAnExistingGrant +
//	   …DurableSwitchTakesEffectWithoutReattach
//
// 本文件的语句顺序有一条纪律：**先 attachGate、后 useRolloutConfig**。闸门是每请求读
// 全局 ltc.config 的（这正是本卡要测的性质），所以"最后装进去的那份存储"才是它读到的那份；
// attachGate 里那一步会顺带装一份"运营从没配过档位"的默认存储（reach_gate_wiring_test.go
// 的夹具，与本文件的 T-P3-07 用例共用），换库必须发生在它之后。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/approval"
	"hivemtk-user/internal/service"
)

// rolloutKV 是 ltcKVStore 的内存替身：本文件要测的是"闸门按库里的档位判"，
// 而不是 KV 实现本身（后者在 ltc_config_test.go 里已经测透）。
type rolloutKV struct {
	row      string
	getErr   error
	upserted int
}

func (f *rolloutKV) Available() bool { return true }

func (f *rolloutKV) Get(context.Context, string) (string, error) {
	if f.getErr != nil {
		return "", f.getErr
	}
	return f.row, nil
}

func (f *rolloutKV) Upsert(_ context.Context, _, value string) (string, error) {
	f.upserted++
	f.row = value
	return value, nil
}

func (f *rolloutKV) EnsureTable(context.Context) error { return nil }

// useRolloutConfig 把全局 LTC 配置换成读内存 KV 的实例，并交还原来的实例。
func useRolloutConfig(t *testing.T) (*service.LTCConfigService, *rolloutKV) {
	t.Helper()
	prev := service.GlobalLTCConfig()
	kv := &rolloutKV{}
	svc := service.NewLTCConfigServiceWithStore(kv)
	service.SetGlobalLTCConfig(svc)
	t.Cleanup(func() { service.SetGlobalLTCConfig(prev) })
	return svc, kv
}

// setRollout 用**运营用的同一个入口**改档位。直接往 KV 里塞 JSON 等于绕过校验器，
// 那本文件就会在一份生产写不出来的配置上变绿。
func setRollout(t *testing.T, svc *service.LTCConfigService, mode service.ReachRolloutMode, whitelist ...string) {
	t.Helper()
	cfg := &service.LTCConfig{
		Enabled:       true,
		StagesEnabled: service.LTCStages{}.With(service.LTCStageOutreach),
		Thresholds:    service.DefaultLTCConfig().Thresholds,
		ReachRollout:  service.ReachRollout{Mode: mode, Whitelist: whitelist},
	}
	if _, err := svc.Save(context.Background(), cfg, 0); err != nil {
		t.Fatalf("写入放量档位 %s 失败：%v", mode, err)
	}
}

// attachGate 装一把 env 侧定为给定模式的门，返回服务与"实际外发了谁"的指针。
//
// W-1 白名单旗子一律打开（刹车不生效），这样"拦下来"这件事只可能由档位造成。
func attachGate(t *testing.T, mode string) (*service.ProactiveReachService, *[]string) {
	t.Helper()
	restoreApprovalState(t)
	restoreReachGateState(t)
	setApprovalWhitelistFlag(t, true)
	wireToolGate(t, "shadow")
	t.Setenv(ReachGateFlagEnv, mode)
	sent := []string{}
	svc := spyReachService(t, &sent)
	if !AttachReachGate(svc) {
		t.Fatalf("%s 态应装上钩子", mode)
	}
	return svc, &sent
}

// block + halt ⇒ 连"W-1 已经批准过"的对象都被拒。
//
// 这条就是 AC②：出事故时要有一把能把已授权对象也按住的闸。W-1 的授权只写进程内存、
// 且撤权要走那个入口，档位是 durable 的 —— 回滚位必须盖过既有授权，否则"关开关"
// 关掉的只是新授权，已经放出去的那批还在往外发。
func TestReachRolloutHaltBeatsAnExistingGrant(t *testing.T) {
	svc, sent := attachGate(t, "block")
	ltc, _ := useRolloutConfig(t)
	setRollout(t, ltc, service.ReachRolloutHalt)

	phone := "12900005001"
	subject := "sms:" + phone
	if !ApprovalWhitelistMutate(ReachApprovalToolKey, subject, time.Now().Add(time.Hour), false) {
		t.Fatal("授权入口不可用")
	}

	err := sendSMS(t, svc, phone)
	if !errors.Is(err, service.ErrReachApprovalDenied) {
		t.Fatalf("halt 档应把已授权对象也拒掉，实际 %v", err)
	}
	if len(*sent) != 0 {
		t.Fatalf("拒绝后不得有任何外发: %v", *sent)
	}
	if _, _, byReason := reachDecisionTotal(t); byReason[service.ReachRolloutReasonHeld] != 1 {
		t.Errorf("这笔拒绝的理由应是档位造成的，实际 %v", byReason)
	}
}

// whitelist 档：名单内放行、名单外拒，且理由要说成档位造成的。
//
// 名单外的这条必须**问到 W-1 之前**就被挡下 —— 否则观测面上"这个对象被拒"的理由
// 会是 denied_default（"没人批准过他"），而真原因是"他还没进放量批次"。
// 这两件事对运营是两个完全不同的下一步动作（去灌授权 vs 改档位）。
func TestReachRolloutWhitelistModeSendsOnlyTheCohort(t *testing.T) {
	svc, sent := attachGate(t, "block")
	ltc, _ := useRolloutConfig(t)
	cohort := "12900005002"
	setRollout(t, ltc, service.ReachRolloutWhitelist, "sms:"+cohort)

	// 名单内：还要过 W-1 那道授权，两层都放行才发得出去。
	if !ApprovalWhitelistMutate(ReachApprovalToolKey, "sms:"+cohort, time.Now().Add(time.Hour), false) {
		t.Fatal("授权入口不可用")
	}
	if err := sendSMS(t, svc, cohort); err != nil {
		t.Fatalf("名单内且已授权应放行，实际 %v", err)
	}
	if len(*sent) != 1 {
		t.Fatalf("名单内应外发一次: %v", *sent)
	}
	_, _, byReason := reachDecisionTotal(t)
	if byReason[approval.ReasonWhitelisted] != 1 {
		t.Errorf("名单内这笔应记为 W-1 的 whitelisted，实际 %v", byReason)
	}

	// 名单外：即使 W-1 里有他的授权，也先被档位挡下。
	outside := "12900005003"
	ApprovalWhitelistMutate(ReachApprovalToolKey, "sms:"+outside, time.Now().Add(time.Hour), false)
	if err := sendSMS(t, svc, outside); !errors.Is(err, service.ErrReachApprovalDenied) {
		t.Fatalf("名单外应被拒，实际 %v", err)
	}
	_, _, byReason = reachDecisionTotal(t)
	if byReason[service.ReachRolloutReasonNotWhitelisted] != 1 {
		t.Errorf("名单外这笔的理由应是档位，实际 %v", byReason)
	}
	if byReason[approval.ReasonDeniedDefault] != 0 {
		t.Errorf("档位挡下时不该再产生 W-1 的拒绝理由（那是两次判定，会把归因指错）：%v", byReason)
	}
}

// env=shadow 时档位**只改变记账**：一条都不拦。
//
// 这条是 AC① 的前提：三段灰度要靠对比数据决定要不要进下一档，而对比数据只在
// "观察期不拦"这个条件下才存在。若档位能独立拦链，写 whitelist 的那一刻观察期就结束了。
func TestReachRolloutChangesOnlyAccountingOutsideBlock(t *testing.T) {
	svc, sent := attachGate(t, "shadow")
	ltc, _ := useRolloutConfig(t)

	setRollout(t, ltc, service.ReachRolloutHalt)
	phone := "12900005004"
	if err := sendSMS(t, svc, phone); err != nil {
		t.Fatalf("env=shadow 时档位不得拦下外发，实际 %v", err)
	}
	if len(*sent) != 1 {
		t.Fatalf("「只改变记账」的前提是这条真的发出去了: %v", *sent)
	}
	total, wouldDeny, byReason := reachDecisionTotal(t)
	if total != 1 || wouldDeny != 1 {
		t.Errorf("档位造成的拒绝仍要记账：total=%d would_deny=%d", total, wouldDeny)
	}
	if byReason[service.ReachRolloutReasonHeld] != 1 {
		t.Errorf("记账理由要点名档位，实际 %v", byReason)
	}
}

// 缺省档（shadow）下新层完全不动既有行为：拦与不拦仍只由 W-1 决定。
//
// 这条是回归锁。它成立的方式是"档位放行 ⇒ 继续问 W-1"，所以理由必须还是 W-1 那个
// denied_default —— 如果哪天有人把短路写成"放行就返回"，这条会红。
func TestReachRolloutShadowLeavesW1InCharge(t *testing.T) {
	svc, _ := attachGate(t, "block")
	ltc, _ := useRolloutConfig(t)
	setRollout(t, ltc, service.ReachRolloutShadow)

	if err := sendSMS(t, svc, "12900005005"); !errors.Is(err, service.ErrReachApprovalDenied) {
		t.Fatalf("没授权仍应被 W-1 拒掉，实际 %v", err)
	}
	_, _, byReason := reachDecisionTotal(t)
	if byReason[approval.ReasonDeniedDefault] != 1 {
		t.Errorf("理由应仍是 W-1 的 denied_default（新层没插话），实际 %v", byReason)
	}
	for reason := range byReason {
		if strings.HasPrefix(reason, "rollout_") {
			t.Errorf("shadow 档不该产生任何 rollout_ 理由，实际出现 %q", reason)
		}
	}
}

// 改档位不需要重新装配：同一个已装好的门，第二次发送就按新档位判。
//
// 这条测的是 AC② 里"不回滚代码"那半句的实现前提 —— 闸门必须**每请求读**配置。
// 若装配期抄了一份 mode 快照，这段代码在测试里会绿、在现网会把一次事故留到下次重启。
func TestReachRolloutDurableSwitchTakesEffectWithoutReattach(t *testing.T) {
	svc, _ := attachGate(t, "block")
	ltc, _ := useRolloutConfig(t)
	setRollout(t, ltc, service.ReachRolloutFull)

	phone := "12900005006"
	subject := "sms:" + phone
	ApprovalWhitelistMutate(ReachApprovalToolKey, subject, time.Now().Add(time.Hour), false)
	if err := sendSMS(t, svc, phone); err != nil {
		t.Fatalf("前置：full 档 + 已授权应放行，实际 %v", err)
	}

	setRollout(t, ltc, service.ReachRolloutHalt)
	if err := sendSMS(t, svc, phone); !errors.Is(err, service.ErrReachApprovalDenied) {
		t.Fatalf("改成 halt 后同一条路应立刻被拒（不重启、不重新装配），实际 %v", err)
	}
	_, _, byReason := reachDecisionTotal(t)
	if byReason[service.ReachRolloutReasonHeld] != 1 {
		t.Errorf("第二笔的理由应是档位，实际 %v", byReason)
	}
}

// 配置读坏了 ⇒ 不放行（与整份文件"异常朝严"一致）。
//
// 这一条刻意与"degraded 时整份配置塌回默认 shadow"对着写：shadow 的语义是放行，
// 所以故障态若走默认值，一次 DB 抖动就会把"白名单灰度中"静默升级成"全量放行"。
func TestReachRolloutDegradedDeniesInBlock(t *testing.T) {
	svc, _ := attachGate(t, "block")
	ltc, kv := useRolloutConfig(t)
	setRollout(t, ltc, service.ReachRolloutWhitelist, "sms:12900005007")
	kv.getErr = errors.New("connection reset")
	// Save 刚把这份好配置写进进程缓存（TTL 内直接命中），不失效就测不到故障读这一支。
	ltc.InvalidateCache()
	if snap, _ := GetReachGateSnapshot(); !snap.RolloutDegraded {
		t.Fatal("夹具没造出故障读 ⇒ 这一格测的不是「配置读坏了」，红绿都不作数")
	}

	phone := "12900005007"
	ApprovalWhitelistMutate(ReachApprovalToolKey, "sms:"+phone, time.Now().Add(time.Hour), false)
	if err := sendSMS(t, svc, phone); !errors.Is(err, service.ErrReachApprovalDenied) {
		t.Fatalf("配置读坏时不得放行，实际 %v", err)
	}
	_, _, byReason := reachDecisionTotal(t)
	if byReason[service.ReachRolloutReasonDegraded] != 1 {
		t.Errorf("要一眼看出是库坏了而不是名单写错了，实际 %v", byReason)
	}
}

// 快照必须把档位与那把"只改变记账"的耦合说出来。
//
// 运维在端点上看到的应该是"档位=whitelist，但 env 不在 block ⇒ 现在一条都不拦"。
// 少了这半句，这个组合的形状和"灰度已经生效"一模一样 —— 而那正是最贵的一次误读：
// 有人会在观察期就以为已经放量了。
func TestReachRolloutSnapshotNamesModeAndCoupling(t *testing.T) {
	attachGate(t, "shadow")
	ltc, _ := useRolloutConfig(t)
	setRollout(t, ltc, service.ReachRolloutWhitelist, "sms:12900005008", "sms:12900005009")

	snap, _ := GetReachGateSnapshot()
	if snap.RolloutMode != string(service.ReachRolloutWhitelist) {
		t.Errorf("快照应回显 durable 档位，实际 %q", snap.RolloutMode)
	}
	if snap.RolloutWhitelistEntries != 2 {
		t.Errorf("名单条数应读到 2，实际 %d", snap.RolloutWhitelistEntries)
	}
	if snap.RolloutDegraded {
		t.Error("读得好好的却报 degraded ⇒ 运维会去查库")
	}
	note := ReachRolloutCouplingNote(snap)
	if note == "" {
		t.Fatal("档位已生效地写库、env 却不在 block ⇒ 必须有一句话说明「现在只改变记账」")
	}
	if !strings.Contains(note, "block") || !strings.Contains(note, ReachGateFlagEnv) {
		t.Errorf("耦合说明要点名 %s 与 block，实得：%q", ReachGateFlagEnv, note)
	}

	// halt 也要在快照里看得见：回滚位没生效和档位没配是两件事。
	setRollout(t, ltc, service.ReachRolloutHalt)
	snap, _ = GetReachGateSnapshot()
	if snap.RolloutMode != string(service.ReachRolloutHalt) {
		t.Errorf("halt 必须原样回显，实际 %q", snap.RolloutMode)
	}
	if snap.RolloutWhitelistEntries != 0 {
		t.Errorf("halt 档不该带名单条目，实际 %d", snap.RolloutWhitelistEntries)
	}
}

// ---- LTC-29 观测面：算不出来的一律标 unavailable，不许以 0 出现 ----------------
//
// 卡面 AC① 写的是"每阶段有投诉率/送达率对比数据"。实测两处都不成立（理由见下面各条
// 断言里的字符串）：把 0 报上去会被读成"这一档零投诉 ⇒ 可以进下一档"，那是用一份
// 不存在的证据做了一个不可逆的放量决定。所以本卡的观测面交付的是"哪些算得出来、
// 哪些算不出来、各缺哪一行代码"，而不是把四个数都填上。
func TestReachRolloutObservationSeparatesComputableFromNot(t *testing.T) {
	svc, _ := attachGate(t, "block")
	ltc, _ := useRolloutConfig(t)
	setRollout(t, ltc, service.ReachRolloutWhitelist, "sms:12900005011")
	if !ApprovalWhitelistMutate(ReachApprovalToolKey, "sms:12900005011", time.Now().Add(time.Hour), false) {
		t.Fatal("授权入口不可用")
	}
	if err := sendSMS(t, svc, "12900005011"); err != nil {
		t.Fatalf("前置：名单内且已授权应放行: %v", err)
	}
	// 再补一笔名单外的拒绝：观测面上只有放行没有拒绝，看不出 by_reason 是否真被带出来。
	if err := sendSMS(t, svc, "12900005012"); !errors.Is(err, service.ErrReachApprovalDenied) {
		t.Fatalf("前置：名单外应被拒，实际 %v", err)
	}

	snap, counter := GetReachGateSnapshot()
	obs := ObserveReachRollout(snap, counter)
	byName := map[string]ReachRolloutMetric{}
	for _, m := range obs.Metrics {
		byName[m.Name] = m
	}
	if len(obs.Metrics) == 0 {
		t.Fatal("观测面一条指标都没有 ⇒ 这个端点什么都没回答")
	}

	// 闸门判定累计：算得出来（每请求留痕已经接在计数器上），但口径要一起给 ——
	// 它是**本进程**的数，重启即空、多副本不合并。
	gate, ok := byName["gate_decisions_in_process"]
	if !ok {
		t.Fatal("缺闸门判定这一项：放量期唯一真有的数就是它")
	}
	if gate.Status != ReachRolloutMetricComputable || gate.Value == nil {
		t.Errorf("这一项此刻应是 computable 且带值，实得 %+v", gate)
	}
	if !strings.Contains(gate.Scope, "重启") {
		t.Errorf("口径必须说出「重启即空」，否则运维会把它当累计总量：%q", gate.Scope)
	}
	// 那两笔判定要在值里按理由切开：只有一个 total 的话，"这一档被档位拒了多少"
	// 和"一共有多少人被判过"就分不开，而放量决定要看的是前者。
	val, _ := gate.Value.(map[string]any)
	br, _ := val["by_reason"].(map[string]int64)
	if br[approval.ReasonWhitelisted] != 1 || br[service.ReachRolloutReasonNotWhitelisted] != 1 {
		t.Errorf("累计要按理由切开这两笔，实得 %v", br)
	}
	if val["total"] != int64(2) {
		t.Errorf("累计判定数应为 2，实得 %v", val["total"])
	}

	// 送达率：表与 webhook 都在，缺的是外发链路写进去的 message_id（恒为常量 ⇒ 唯一索引下
	// 所有外发挤成一行）。这一条被标成 computable 的话，读的人以为放量有依据了。
	if m := byName["delivery_rate_by_cohort"]; m.Status != ReachRolloutMetricUnavailable {
		t.Errorf("送达率此刻算不出来，实得 %+v", m)
	} else {
		if m.Value != nil {
			t.Errorf("unavailable 的项不许带值（0 会被读成「送达率为 0」）：%+v", m)
		}
		for _, want := range []string{"message_id", "sms_out"} {
			if !strings.Contains(m.Reason, want) {
				t.Errorf("送达率的理由要点名 %s（这是实测出来的根因），实得：%q", want, m.Reason)
			}
		}
		if m.Unblocker == "" {
			t.Error("要说清补哪一行代码才算得出来，否则这条 unavailable 等于一次投诉")
		}
	}

	// 投诉率：全仓没有任何持久化写入方（complaint 只有常量与瞬时的意图标签）。
	if m := byName["complaint_rate_by_cohort"]; m.Status != ReachRolloutMetricUnavailable {
		t.Errorf("投诉率此刻算不出来，实得 %+v", m)
	} else {
		if m.Value != nil {
			t.Errorf("同上，0 会被读成「没人投诉」：%+v", m)
		}
		if !strings.Contains(m.Reason, "写入") {
			t.Errorf("投诉率的理由要说清「没有落库方」，实得：%q", m.Reason)
		}
	}

	// 分档对比的分母：外发没有发送账本，判定也不落库 ⇒ 事后按批次回溯无从下手。
	if m := byName["sends_by_cohort"]; m.Status != ReachRolloutMetricUnavailable {
		t.Errorf("按档位的发送数今天算不出来（无账本），实得 %+v", m)
	}

	// 每一项都必须二选一，且 unavailable 必带理由：漏一项就是端点上沉默的 0。
	for _, m := range obs.Metrics {
		switch m.Status {
		case ReachRolloutMetricComputable, ReachRolloutMetricUnavailable:
		default:
			t.Errorf("指标 %q 的 status=%q 不在两值之内", m.Name, m.Status)
		}
		if m.Status == ReachRolloutMetricUnavailable && (m.Reason == "" || m.Unblocker == "") {
			t.Errorf("指标 %q unavailable 却没给原因或补法：%+v", m.Name, m)
		}
	}
	if obs.RolloutMode != string(service.ReachRolloutWhitelist) || obs.RolloutWhitelistEntries != 1 {
		t.Errorf("观测面要自带当前档位（否则数与档对不上号）：%+v", obs)
	}
}

// 判决句必须**从指标列表里推出来**。
//
// 写死的那一句在补齐上游数据之后仍会说"算不出来"，到那时候误导人的就是这句话本身，
// 而且是最难发现的一种 —— 它曾经是诚实的。所以这里锁的是推导关系，不是措辞：
// 第二格（四项全 computable 却仍报缺数据）就是给那句写死的话准备的反向证明。
func TestReachRolloutObservationVerdictIsDerivedFromMetrics(t *testing.T) {
	svc, _ := attachGate(t, "block")
	ltc, _ := useRolloutConfig(t)
	setRollout(t, ltc, service.ReachRolloutWhitelist, "sms:12900005013")
	if !ApprovalWhitelistMutate(ReachApprovalToolKey, "sms:12900005013", time.Now().Add(time.Hour), false) {
		t.Fatal("授权入口不可用")
	}
	if err := sendSMS(t, svc, "12900005013"); err != nil {
		t.Fatalf("前置：装一门并有判定，实际 %v", err)
	}

	snap, counter := GetReachGateSnapshot()
	obs := ObserveReachRollout(snap, counter)
	if counter == nil {
		t.Fatal("前置：闸门判定累计必须存在，否则这一格测的是「全缺」那一支")
	}
	for _, want := range []string{"sends_by_cohort", "delivery_rate_by_cohort", "complaint_rate_by_cohort"} {
		if !strings.Contains(obs.ComparisonVerdict, want) {
			t.Errorf("判决句要点名此刻缺的那一项 %s，实得：%q", want, obs.ComparisonVerdict)
		}
	}
	if strings.Contains(obs.ComparisonVerdict, "gate_decisions_in_process") {
		t.Errorf("算得出来的那一项不该出现在「还缺」的名单里（否则这句话把有数的那条也抹成没数）：%q", obs.ComparisonVerdict)
	}

	fixed := []ReachRolloutMetric{
		{Name: "gate_decisions_in_process", Status: ReachRolloutMetricComputable},
		{Name: "sends_by_cohort", Status: ReachRolloutMetricComputable},
		{Name: "delivery_rate_by_cohort", Status: ReachRolloutMetricComputable},
		{Name: "complaint_rate_by_cohort", Status: ReachRolloutMetricComputable},
	}
	verdict := rolloutComparisonVerdict(fixed)
	if strings.Contains(verdict, "缺") || strings.Contains(verdict, "算不出来") {
		t.Errorf("四项都算得出来时判决句仍在报缺数据（写死的那份就会这样）：%q", verdict)
	}
	if !strings.Contains(verdict, "对比") {
		t.Errorf("补齐后要说的是「可以按档对比再进下一档」，实得：%q", verdict)
	}
}

// 门没装时那份"累计"也不存在：不能报 0，要报"因为没装所以没有"。
//
// 这一条与上一条成对：只测有流量的那一支，端点在 off 态就会把"没接线"显示成"零判定"，
// 而这两件事的下一步动作完全不同（去开旗子 vs 等流量）。
func TestReachRolloutObservationWithoutGateSaysSo(t *testing.T) {
	restoreApprovalState(t)
	restoreReachGateState(t)
	wireToolGate(t, "off")
	t.Setenv(ReachGateFlagEnv, "off")

	snap, counter := GetReachGateSnapshot()
	obs := ObserveReachRollout(snap, counter)
	for _, m := range obs.Metrics {
		if m.Name == "gate_decisions_in_process" {
			if m.Status != ReachRolloutMetricUnavailable || !strings.Contains(m.Reason, "没装") {
				t.Errorf("闸门未接线要说成没装，不能报 0：%+v", m)
			}
		}
	}
	if obs.Wired {
		t.Error("off 态观测面不得声称已接线")
	}
}

// 整份配置的字节形状不能因为新层而变化：durable 档位写库后仍是一份合法 ltc.config。
// （这一条由上面的 setRollout 顺带覆盖，这里显式钉一次读回。）
func TestReachRolloutStoredBytesParseBack(t *testing.T) {
	ltc, kv := useRolloutConfig(t)
	setRollout(t, ltc, service.ReachRolloutWhitelist, "sms:12900005010")
	back, err := service.ParseLTCConfig([]byte(kv.row))
	if err != nil {
		t.Fatalf("写出去的字节读不回来：%v\n%s", err, kv.row)
	}
	if back.ReachRollout.Mode != service.ReachRolloutWhitelist {
		t.Errorf("档位没落库，实得 %q", back.ReachRollout.Mode)
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal([]byte(kv.row), &probe); err != nil {
		t.Fatal(err)
	}
	if _, ok := probe["reach_rollout"]; !ok {
		t.Errorf("库里的 JSON 应有 reach_rollout 这一节：%s", kv.row)
	}
}
