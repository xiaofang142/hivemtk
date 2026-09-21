package service

// ltc_reach_rollout_test.go —— T-P5-04 放量三段（shadow → 白名单 → 全量）的 durable 开关位。
//
// 这一族用例存在的理由，是"改开关要重启"这一条：现网的档位来自 FF_LTC_REACH_GATE，
// 它在装配期读死 ⇒ 出事故时运营改不动（改环境变量 = 发一次部署），而 AC② 要求
// "一键回滚 = 关开关，不回滚代码"。所以档位搬进 ltc.config（写库 + 审计 + 60s 缓存收敛）。
//
// 两条贯穿本文件的判据：
//  1. **"没配"与"配了但读坏了"必须分开**：前者是开箱第一档 shadow（现网逐字不变），
//     后者一律取严（不放行）。塌成同一个值的那天，一次存储故障就会把"白名单灰度中"
//     静默升级成"全量放行" —— 那是本卡最容易造出来的假安全。
//  2. **名单里写的是闸门判定键本身**（`reachApprovalKey` 的形状），不是新造的第二套身份：
//     两套授权键早晚会"这里批过、那里没批过"。

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// 一份最小合法载荷：thresholds 四项必填、stages_enabled 必填（既有口径）。
func rolloutPayload(t *testing.T, extra string) []byte {
	t.Helper()
	raw := `{"enabled":true,"stages_enabled":{"outreach":true},"thresholds":{"lead_score":70,"confidence":0.8,"discount_percent":15,"win_probability":0.5}`
	if extra != "" {
		raw += "," + extra
	}
	return []byte(raw + "}")
}

func mustParseRollout(t *testing.T, raw []byte) *LTCConfig {
	t.Helper()
	cfg, err := ParseLTCConfig(raw)
	if err != nil {
		t.Fatalf("这份配置应当合法，却被拒收：%v\n载荷：%s", err, raw)
	}
	return cfg
}

// ---- 档位的值域与开箱值 ------------------------------------------------------

// 存量 ltc.config（今天库里就是这一份形状）里没有 reach_rollout 这一节：
// 解析必须照常成功并落在第一档 shadow。若这里改成"缺节即拒收"，那次故障读
// 会把整份打成 degraded，把 T-P3-06 已经上线的六阶段开关一并关掉。
func TestLTCReachRollout_AbsentSectionIsStageOneNotAReject(t *testing.T) {
	cfg := mustParseRollout(t, rolloutPayload(t, ""))
	if cfg.ReachRollout.Mode != ReachRolloutShadow {
		t.Errorf("缺 reach_rollout 应落第一档 shadow，实得 %q", cfg.ReachRollout.Mode)
	}
	if cfg.Degraded {
		t.Error("缺这一节不是故障，不该打成 degraded")
	}
	if !cfg.StageEnabled(LTCStageOutreach) {
		t.Error("既有字段不能被新节影响：outreach 阶段应保持打开")
	}
}

// 四个合法档名逐条能解析；任何别的名（含"看着像"的 gradual/canary/gray）整份拒收。
// 宽容解法下 `mode:"canary"` 得到的是"保存 200、档位还是 shadow"——名义与实现相反。
//
// 每条夹具只带该档**允许带**的键：给 shadow 配名单会被另一条判据（名单只属于
// whitelist 档）先拒掉，那样这个用例的"成功"与"失败"都不再是关于档名的事实。
func TestLTCReachRollout_ModeValueRange(t *testing.T) {
	for _, tc := range []struct{ mode, payload string }{
		{"shadow", `"reach_rollout":{"mode":"shadow"}`},
		{"whitelist", `"reach_rollout":{"mode":"whitelist","whitelist":["one:1"]}`},
		{"full", `"reach_rollout":{"mode":"full"}`},
		{"halt", `"reach_rollout":{"mode":"halt"}`},
	} {
		cfg := mustParseRollout(t, rolloutPayload(t, tc.payload))
		if string(cfg.ReachRollout.Mode) != tc.mode {
			t.Errorf("%q 没被原样采信，实得 %q", tc.mode, cfg.ReachRollout.Mode)
		}
	}
	for _, bad := range []string{"", "canary", "gradual", "gray", "on", "true"} {
		_, err := ParseLTCConfig(rolloutPayload(t, `"reach_rollout":{"mode":"`+bad+`"}`))
		if err == nil {
			t.Errorf("mode=%q 应当被拒（合法值只有 %s），却解析成功", bad, strings.Join(ltcReachRolloutModes, "/"))
			continue
		}
		if !strings.Contains(err.Error(), "shadow") || !strings.Contains(err.Error(), "full") {
			t.Errorf("mode=%q 的拒收文案要点名合法值，实得：%v", bad, err)
		}
	}
}

// 大小写与首尾空格在**赋值前**归一：运营从表格里粘 "Full" 不该换来一次全关。
func TestLTCReachRollout_ModeNormalizedBeforeMatch(t *testing.T) {
	cfg := mustParseRollout(t, rolloutPayload(t, `"reach_rollout":{"mode":"  FULL  ","whitelist":[]}`))
	if cfg.ReachRollout.Mode != ReachRolloutFull {
		t.Errorf("期望归一成 full，实得 %q", cfg.ReachRollout.Mode)
	}
}

// ---- 档位语义：谁放行、谁被拦 -------------------------------------------------

// shadow 与 full 都放行，但理由必须不同：观测端点上"这一条被放行了"有两种成因，
// 混成一个 true 就等于把"还在观察"和"已经全量"这两档抹平成同一件事。
func TestLTCReachRollout_ShadowAndFullBothAllowButSayWhy(t *testing.T) {
	shadow := mustParseRollout(t, rolloutPayload(t, `"reach_rollout":{"mode":"shadow","whitelist":[]}`))
	full := mustParseRollout(t, rolloutPayload(t, `"reach_rollout":{"mode":"full","whitelist":[]}`))

	ok, reason := shadow.ReachRolloutAllows("one:9")
	if !ok || reason != ReachRolloutReasonShadow {
		t.Errorf("shadow 档应放行并说明原因，实得 (%t,%q)", ok, reason)
	}
	ok, reason = full.ReachRolloutAllows("one:9")
	if !ok || reason != ReachRolloutReasonFull {
		t.Errorf("full 档应放行并说明原因，实得 (%t,%q)", ok, reason)
	}
	if reason == ReachRolloutReasonShadow {
		t.Error("两档的理由塌成同一个 ⇒ 观测面再也分不清\"还在观察\"与\"已经全量\"")
	}
}

// halt 是 AC②「一键回滚 = 关开关，不回滚代码」落成的那个显式值：一个都不放，
// 且原因要说成"被档位按住"而不是"不在名单里" —— 出事故时运维要能一眼分开出的是
// 哪一刀（回滚位生效 ≠ 名单没配好）。
func TestLTCReachRollout_HaltDeniesEverythingWithItsOwnReason(t *testing.T) {
	cfg := mustParseRollout(t, rolloutPayload(t, `"reach_rollout":{"mode":"halt"}`))
	for _, key := range []string{"one:1", "sms:13800138000", ""} {
		ok, reason := cfg.ReachRolloutAllows(key)
		if ok || reason != ReachRolloutReasonHeld {
			t.Errorf("halt 档 %q 应一律拒且理由为 halted，实得 (%t,%q)", key, ok, reason)
		}
	}
}

// whitelist 档：名单内放行、名单外拒。键的匹配是**精确等值**，不是前缀匹配 ——
// 前缀匹配会让 `one:1` 顺手放行 `one:10…one:19`，那是"我以为在灰度、实际放了一小片"。
func TestLTCReachRollout_WhitelistAllowsOnlyExactEntries(t *testing.T) {
	cfg := mustParseRollout(t, rolloutPayload(t,
		`"reach_rollout":{"mode":"whitelist","whitelist":["one:1","sms:13800138000"]}`))

	for _, key := range []string{"one:1", "sms:13800138000"} {
		if ok, reason := cfg.ReachRolloutAllows(key); !ok || reason != ReachRolloutReasonWhitelisted {
			t.Errorf("名单内 %q 应放行，实得 (%t,%q)", key, ok, reason)
		}
	}
	for _, key := range []string{"one:", "one:10", "one:12", "sms:1380013800", "ONE:1", ""} {
		if ok, reason := cfg.ReachRolloutAllows(key); ok || reason != ReachRolloutReasonNotWhitelisted {
			t.Errorf("%q 不在名单内（前缀/大小写都不算命中），却得到 (%t,%q)", key, ok, reason)
		}
	}
}

// 开箱默认（不是 degraded 读）也要能判：DefaultLTCConfig 的档位是 shadow。
func TestLTCReachRollout_DefaultConfigIsShadowAndAllows(t *testing.T) {
	cfg := DefaultLTCConfig()
	if cfg.ReachRollout.Mode != ReachRolloutShadow {
		t.Errorf("开箱档位应是第一档 shadow，实得 %q", cfg.ReachRollout.Mode)
	}
	if ok, _ := cfg.ReachRolloutAllows("one:1"); !ok {
		t.Error("开箱 shadow 档不该拦任何对象（现网行为逐字不变是这条卡的前提）")
	}
}

// ---- 故障读必须朝严，不能塌回第一档 ---------------------------------------------

// 存储读坏了（degraded）时的默认值恰恰是 shadow=放行 ⇒ 一次 DB 抖动就会把
// "白名单灰度中"静默升级成"全量放行"。这里要的是相反方向：故障 ⇒ 不放行，
// 并把原因照实说出来（运维要能一眼看出是库坏了，不是自己名单写错了）。
func TestLTCReachRollout_DegradedReadDeniesAndSaysWhy(t *testing.T) {
	cfg := mustParseRollout(t, rolloutPayload(t,
		`"reach_rollout":{"mode":"whitelist","whitelist":["one:1"]}`))
	cfg.Degraded = true
	cfg.DegradeReason = "kv 读失败"

	ok, reason := cfg.ReachRolloutAllows("one:1")
	if ok {
		t.Error("degraded 读不能放行：故障会把灰度静默升级成全量")
	}
	if reason != ReachRolloutReasonDegraded {
		t.Errorf("要说清是配置故障，实得 %q", reason)
	}

	// nil 接收者同一口径（与 StageActive 的 nil 分支同形）：
	var nilCfg *LTCConfig
	if ok, reason := nilCfg.ReachRolloutAllows("one:1"); ok || reason != ReachRolloutReasonDegraded {
		t.Errorf("nil 配置应取严，实得 (%t,%q)", ok, reason)
	}
}

// ---- 名单形状：把"写了不生效"和"半开状态"挡在写侧 ---------------------------------

// mode=whitelist 而名单为空 = 一个都不放 ⇒ 这一档看起来像"灰度中"，实际是"全停"。
// 这种组合必须在写侧就被拒，而不是等运营在待办中心看到积压才发现。
func TestLTCReachRollout_WhitelistModeWithoutEntriesIsRejected(t *testing.T) {
	for _, payload := range []string{
		`"reach_rollout":{"mode":"whitelist","whitelist":[]}`,
		`"reach_rollout":{"mode":"whitelist"}`,
		`"reach_rollout":{"mode":"whitelist","whitelist":["  "]}`,
	} {
		_, err := ParseLTCConfig(rolloutPayload(t, payload))
		if err == nil {
			t.Errorf("whitelist 档配空名单应当拒收，却成功：%s", payload)
			continue
		}
		if !strings.Contains(err.Error(), "空") && !strings.Contains(err.Error(), "没有") {
			t.Errorf("拒收文案要说清\"这档等于一个都不放\"，实得：%v", err)
		}
	}
}

// mode=full 还留着名单 = 名单永不被读，正是"名义与实现相反"的形状；
// 同理 shadow / halt 档配名单也不会被读。三档都拒，逼运营先把名单清掉再改档。
func TestLTCReachRollout_WhitelistOnlyLegalInWhitelistMode(t *testing.T) {
	for _, mode := range []string{"shadow", "full", "halt"} {
		_, err := ParseLTCConfig(rolloutPayload(t,
			`"reach_rollout":{"mode":"`+mode+`","whitelist":["one:1"]}`))
		if err == nil {
			t.Errorf("%s 档配非空名单应当拒收（这一档根本不读名单）", mode)
			continue
		}
		if !strings.Contains(err.Error(), mode) {
			t.Errorf("拒收文案要点名被拒的档位 %s，实得：%v", mode, err)
		}
	}
}

// 去重与 trim 是"写进去的形状 = 实际生效的形状"的另一半；
// 空条目、超单条上限、超总条数三条各自给一条能看懂的错。
func TestLTCReachRollout_EntriesNormalizedAndBounded(t *testing.T) {
	cfg := mustParseRollout(t, rolloutPayload(t,
		`"reach_rollout":{"mode":"whitelist","whitelist":[" one:1 ","one:1","sms:13800138000"]}`))
	if got := cfg.ReachRollout.Whitelist; len(got) != 2 || got[0] != "one:1" {
		t.Errorf("期望 trim＋去重后剩 2 条且首条是 one:1，实得 %#v", got)
	}

	_, err := ParseLTCConfig(rolloutPayload(t, `"reach_rollout":{"mode":"whitelist","whitelist":["one:1",""]}`))
	if err == nil || !strings.Contains(err.Error(), "空条目") {
		t.Errorf("空条目要被点名，实得：%v", err)
	}
	tooLong := "one:" + strings.Repeat("y", 130)
	if _, err = ParseLTCConfig(rolloutPayload(t, `"reach_rollout":{"mode":"whitelist","whitelist":["`+tooLong+`"]}`)); err == nil {
		t.Errorf("单条超过 %d 字节应拒收", ltcReachWhitelistMaxEntryBytes)
	}

	oversize := make([]string, ltcReachWhitelistMaxEntries+1)
	for i := range oversize {
		oversize[i] = "one:" + strconv.Itoa(i)
	}
	encoded, jsonErr := json.Marshal(oversize)
	if jsonErr != nil {
		t.Fatal(jsonErr)
	}
	_, err = ParseLTCConfig(rolloutPayload(t, `"reach_rollout":{"mode":"whitelist","whitelist":`+string(encoded)+`}`))
	if err == nil {
		t.Errorf("超过 %d 条应拒收（整份载荷还有 8KB 上限，先报哪个都要说得清）", ltcReachWhitelistMaxEntries)
	}
}

// ---- 与既有两道锁的关系：新节不能把老语义顶掉 --------------------------------------

// 总开关 enabled=false 时放量档**不改变**阶段闸门的行为（两道锁各自独立），
// 反向也要成立：把 rollout 配成 whitelist 不等于"整条 LTC 已关"。
func TestLTCReachRollout_DoesNotTouchTheTwoExistingLocks(t *testing.T) {
	cfg := mustParseRollout(t, rolloutPayload(t, `"reach_rollout":{"mode":"whitelist","whitelist":["one:1"]}`))
	if !cfg.Enabled || !cfg.StageEnabled(LTCStageOutreach) {
		t.Fatal("夹具前提：总开关开、outreach 开")
	}
	if on, _ := cfg.StagesEnabled.Get(LTCStageQuote); on {
		t.Error("新节不得顺手打开别的阶段")
	}
	cfg.Enabled = false
	if cfg.StageEnabled(LTCStageOutreach) {
		t.Error("总开关关掉后阶段必须不生效（rollout 档不参与这个判断）")
	}
	if ok, reason := cfg.ReachRolloutAllows("one:1"); !ok || reason != ReachRolloutReasonWhitelisted {
		t.Errorf("rollout 判据刻意独立于 enabled（它管的是闸门档位，不是阶段开关），实得 (%t,%q)", ok, reason)
	}
}

// 写侧要落得下去、读侧要收得回来：整节经过一次 Marshal/Parse 必须原样回来。
// 这一条是"改完开关多久生效"那个对外承诺（缓存 TTL）能成立的前提。
func TestLTCReachRollout_RoundTripsThroughJSON(t *testing.T) {
	cfg := mustParseRollout(t, rolloutPayload(t, `"reach_rollout":{"mode":"whitelist","whitelist":["one:1","sms:13900139000"]}`))
	encoded, err := json.Marshal(cfg.Normalized())
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseLTCConfig(encoded)
	if err != nil {
		t.Fatalf("自己写出去的形状读不回来：%v\n%s", err, encoded)
	}
	if back.ReachRollout.Mode != cfg.ReachRollout.Mode ||
		strings.Join(back.ReachRollout.Whitelist, "|") != strings.Join(cfg.ReachRollout.Whitelist, "|") {
		t.Errorf("往返不一致：%s → %s", cfg.ReachRollout.Mode, back.ReachRollout.Mode)
	}
	if len(encoded) > LTCConfigMaxBytes {
		t.Errorf("夹具就超过整份上限了：%d > %d", len(encoded), LTCConfigMaxBytes)
	}
}

// 审计 detail 要带上档位：运营改的是"放量档"时，operation_logs 里只记着
// enabled/stages/thresholds 等于这次变更在审计面上没发生过。
func TestLTCReachRollout_AuditDetailNamesTheMode(t *testing.T) {
	cfg := mustParseRollout(t, rolloutPayload(t, `"reach_rollout":{"mode":"whitelist","whitelist":["one:1"]}`))
	detail := cfg.AuditDetail()
	if !strings.Contains(detail, "reach_rollout=whitelist") || !strings.Contains(detail, "名单 1 条") {
		t.Errorf("审计摘要要点名档位与名单条数，实得：%q", detail)
	}
}

// 写侧必须写成读侧认得的形状。
//
// Save 落库的是 Normalized() 的 JSON，而 struct 的 mode 零值是 ""（手搭配置的这条路
// 走 Validate 是合法的 —— Validate 也被 Parse 之外的构造者调用）。"" 原样写进库的后果
// 不是"这一节没生效"，是**下一次读整份被打成 degraded**：一次普通的保存动作连带把
// T-P3-06 已上线的六阶段开关全关掉。
func TestLTCReachRollout_HandBuiltConfigPersistsAnExplicitMode(t *testing.T) {
	cfg := &LTCConfig{
		Enabled:       true,
		StagesEnabled: LTCStages{Outreach: true},
		Thresholds:    LTCThresholds{LeadScore: 70, Confidence: 0.8, DiscountPercent: 15, WinProbability: 0.5},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("手搭的最低合法配置被拒：%v", err)
	}
	encoded, err := json.Marshal(cfg.Normalized())
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseLTCConfig(encoded)
	if err != nil {
		t.Fatalf("写出去的形状读不回来（⇒ Save 一次就把库打成 degraded）：%v\n%s", err, encoded)
	}
	if back.ReachRollout.Mode != ReachRolloutShadow {
		t.Errorf("缺档位须落成显式 shadow，实得 %q", back.ReachRollout.Mode)
	}
}
