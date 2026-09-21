package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// ltc_config.go —— LTC-25「运营一键开启」的开关落点（新规划任务清单 T-P3-06）。
//
// 为什么这张卡存在：第一步调研把 LTC-25 判成 🔴 的唯一理由是"未定义开关落点"——
// 能力一路加到 P4~P8，而"运营点一下就让整条 LTC 跑起来"这件事今天没有任何一处能表达。
// 仓里已有的两族开关都不合用：`FF_*` 环境变量在装配期读死（改一次要重启，且不是运营能碰的），
// `config_params` 是逐条标量（六阶段 × 开关 + 四阈值 = 十个键，一起改会撕出半开状态）。
// 业务开关这一族的既有先例是遗留 KV 配置表存整份 JSON 策略（`password_policy.go`），
// 一次写 = 一次整体生效，故选它。
// 表名在本文件一律不直写：internal/service 里那道"禁止新写遗留 KV 直查"的文本守卫按"文件里
// 出现表名"判，注释里的说明也会算，而本卡走的是 repository，一行裸 SQL 都没有。
// 要查表名去看 repository.SystemConfigKVRepository。
//
// 两条贯穿全文件的判据：
//  1. **两道独立的锁**：`enabled`（总开关）× `stages_enabled.<stage>`（阶段开关）。
//     与 T-P2-05 的"旗子 × 行上百分比"同族：塌成一道锁的那天，"只开报价"和"全开"
//     就再也分不开了。
//  2. **缺省与异常一律朝关**（safe-by-default）：解析失败、阈值非法、读不到存储 ⇒
//     整份回落默认全关，并把这件事写进 `degraded`，绝不用"上一次的好值"或"部分采信"。

// LTCStage 是 LTC 流程阶段名，字面值与 ltc.config 的 stages_enabled 键一一对应。
type LTCStage string

const (
	LTCStageOpportunity LTCStage = "opportunity"
	LTCStageOutreach    LTCStage = "outreach"
	LTCStageQuote       LTCStage = "quote"
	LTCStageBill        LTCStage = "bill"
	LTCStagePayment     LTCStage = "payment"
	LTCStageCollection  LTCStage = "collection"
)

// LTCKnownStages 按 LTC 流程顺序排列；闸门与校验都以此为准。
//
// 单独造一个包级切片（而不是让调用方遍历 JSON）是为了让"漏登记一个阶段"变成可断言的事：
// 漏掉的那个阶段永远打不开，而这正是"配置写了没生效"那一类最贵的静默失效。
var LTCKnownStages = []LTCStage{
	LTCStageOpportunity, LTCStageOutreach, LTCStageQuote,
	LTCStageBill, LTCStagePayment, LTCStageCollection,
}

// LTCStages 六个阶段的独立开关。缺键 = false（朝关），所以不写就是不开。
type LTCStages struct {
	Opportunity bool `json:"opportunity"`
	Outreach    bool `json:"outreach"`
	Quote       bool `json:"quote"`
	Bill        bool `json:"bill"`
	Payment     bool `json:"payment"`
	Collection  bool `json:"collection"`
}

// With 返回只打开 stage 的那一份（其余全关）；未知阶段名原样返回。
func (s LTCStages) With(stage LTCStage) LTCStages {
	switch stage {
	case LTCStageOpportunity:
		s.Opportunity = true
	case LTCStageOutreach:
		s.Outreach = true
	case LTCStageQuote:
		s.Quote = true
	case LTCStageBill:
		s.Bill = true
	case LTCStagePayment:
		s.Payment = true
	case LTCStageCollection:
		s.Collection = true
	}
	return s
}

// Get 返回阶段开关值与"这个名字认不认"。第二个返回值必须是显式的：
// 未知阶段若按 false 返回，调用方无从分辨"运营没开这一档"与"配置里的键写错了"。
func (s LTCStages) Get(stage LTCStage) (on bool, known bool) {
	switch stage {
	case LTCStageOpportunity:
		return s.Opportunity, true
	case LTCStageOutreach:
		return s.Outreach, true
	case LTCStageQuote:
		return s.Quote, true
	case LTCStageBill:
		return s.Bill, true
	case LTCStagePayment:
		return s.Payment, true
	case LTCStageCollection:
		return s.Collection, true
	}
	return false, false
}

// OnList 返回当前打开的阶段名（顺序按流程序），用于审计与响应。
func (s LTCStages) OnList() []string {
	var out []string
	for _, st := range LTCKnownStages {
		if on, _ := s.Get(st); on {
			out = append(out, string(st))
		}
	}
	return out
}

// LTCThresholds 四项阈值，各自独立可配（C5 / CS-29）。
//
// 量程来自仓内既有生产者，不是拟的：lead_score 是 0-100 整数
// （`clue_score.go` 的"5 维度加权后映射到 0-100"），confidence 是 0-1 比值
// （`selfconsistency.Confidence = winner.count/total`），win_probability 同理是概率；
// discount_percent 是百分数。
type LTCThresholds struct {
	LeadScore       int     `json:"lead_score"`
	Confidence      float64 `json:"confidence"`
	DiscountPercent float64 `json:"discount_percent"`
	WinProbability  float64 `json:"win_probability"`
}

// LTCConfig 是 ltc.config 这一份策略。Degraded/Source 是"这一次读"的元信息，
// 故意不进 JSON：它们描述的是读路径的健康度，不是运营配的内容，
// 存进去会让下一次写入把上一次的故障状态当成配置继承下去。
type LTCConfig struct {
	Enabled       bool          `json:"enabled"`
	StagesEnabled LTCStages     `json:"stages_enabled"`
	Thresholds    LTCThresholds `json:"thresholds"`
	ReachRollout  ReachRollout  `json:"reach_rollout"`

	Degraded      bool   `json:"-"`
	DegradeReason string `json:"-"`
	Source        string `json:"-"`
}

const (
	// LTCConfigKVKey 是策略在遗留 KV 配置表中的键（表名不直写，原因见本文件头）。
	LTCConfigKVKey = "ltc.config"
	// LTCConfigCacheTTL 是进程内缓存有效期。写库的那个副本立即失效自己的缓存，
	// 其余副本最迟一个 TTL 后收敛 ⇒ 这是"改完开关多久生效"的对外承诺，不是实现细节。
	LTCConfigCacheTTL = 60 * time.Second
	// LTCConfigAuditModule 是 operation_logs 里的 module 名。
	LTCConfigAuditModule = "ltc_config"

	// LTCConfigMaxBytes 是整份策略的字节上限，写侧与 HTTP 入口共用同一个数：
	// 两处各写一个上限，早晚会出现"网关放行、服务拒收"那种对不齐的 400。
	LTCConfigMaxBytes = 8 * 1024
)

// ErrLTCStoreUnavailable 表示"存储此刻写不进去"，与"运营写的值不对"必须分开报：
// 前者要让运维去修库（503），后者要让前端把红字打在表单上（400）。
var ErrLTCStoreUnavailable = errors.New("ltc.config 存储不可用")

// LTCKnownThresholdKeys 是 thresholds 下必须逐条给出的四个键，顺序即报错与表单顺序。
// 收成一份清单是为了让"漏校验一项"变成不可能：读侧遍历的就是这一份。
var LTCKnownThresholdKeys = []string{"lead_score", "confidence", "discount_percent", "win_probability"}

const (
	ltcLeadScoreMin = 1
	ltcLeadScoreMax = 100
	ltcRateMin      = 0.001
	ltcRateMax      = 1
	ltcDiscountMin  = 0
	ltcDiscountMax  = 100
)

// LTCThresholdBound 是一条阈值的取值边界，含"0 这个数算不算合法"。
//
// 它存在的原因是管理端要渲染输入边界，而边界只能有一份：数字写在视图里就是第二份事实，
// 漂移的方向恰好是"表单允许、后端拒收"，运营在同一个输入框里看到第三种报错。
// 下面的 min/max/zero_legal 与 Validate 用的是同一批常量，改一边必然改另一边。
type LTCThresholdBound struct {
	Min       float64 `json:"min"`
	Max       float64 `json:"max"`
	ZeroLegal bool    `json:"zero_legal"`
	Desc      string  `json:"desc"`
}

var ltcThresholdBounds = map[string]LTCThresholdBound{
	"lead_score": {
		Min: ltcLeadScoreMin, Max: ltcLeadScoreMax, ZeroLegal: false,
		Desc: "线索分本身是 0~100，这里给的是及格线（最低 1）；给 0 等于拆掉这道闸门",
	},
	"confidence": {
		Min: ltcRateMin, Max: ltcRateMax, ZeroLegal: false,
		Desc: "0~1 的置信度下限，与 lead_score 是两道独立闸门，不可合并成一个分",
	},
	"discount_percent": {
		Min: ltcDiscountMin, Max: ltcDiscountMax, ZeroLegal: true,
		Desc: "达到这个折扣比例就要走二次审批；0 合法，含义是「任何折扣都要审批」",
	},
	"win_probability": {
		Min: ltcRateMin, Max: ltcRateMax, ZeroLegal: false,
		Desc: "0~1 的赢率门槛，供商机阶段推进判定用",
	},
}

// LTCKnownThresholdBounds 按 LTCKnownThresholdKeys 的顺序给出四项边界（副本）。
func LTCKnownThresholdBounds() map[string]LTCThresholdBound {
	out := make(map[string]LTCThresholdBound, len(ltcThresholdBounds))
	for k, v := range ltcThresholdBounds {
		out[k] = v
	}
	return out
}

// LTCThresholdBoundOf 查单项边界；键不认识时返回 false，由调用方决定怎么兜。
func LTCThresholdBoundOf(key string) (LTCThresholdBound, bool) {
	b, ok := ltcThresholdBounds[key]
	return b, ok
}

// 生效判定的原因字面量：调用方（含 HTTP 响应）靠它们把"为什么没生效"说清楚。
const (
	LTCReasonActive       = "active"
	LTCReasonMasterOff    = "master_off"
	LTCReasonStageOff     = "stage_off"
	LTCReasonUnknownStage = "unknown_stage"
	LTCReasonDegraded     = "config_degraded"
)

// SourceLTC / SourceDefault 标记这一次读到的配置来自哪里。
const (
	SourceLTC     = "kv"
	SourceDefault = "default"
)

// DefaultLTCConfig 开箱状态：总开关关、六个阶段全关、四项阈值取保守值。
//
// 阈值不给 0 是有原因的：0 在 lead_score / confidence / win_probability 上的语义是
// "这道闸门恒通过"，而"抬总开关的人没顺手改阈值"是必然会发生的一次操作 ——
// 默认值必须处在"就算被打开也只是过一道常规门槛"的位置上。
func DefaultLTCConfig() *LTCConfig {
	return &LTCConfig{
		Enabled:       false,
		StagesEnabled: LTCStages{},
		Thresholds: LTCThresholds{
			LeadScore:       70,
			Confidence:      0.80,
			DiscountPercent: 15,
			WinProbability:  0.50,
		},
		// 放量档停在第一段（只观察）：现网逐字不变，是"这份配置被打开之前不该有任何拦截"
		// 的形状。注意它与 degraded 读不是一回事 —— 后者必须取严，见 ReachRolloutAllows。
		ReachRollout: ReachRollout{Mode: ReachRolloutShadow},
		Source:       SourceDefault,
	}
}

// Validate 校验整份策略。量程与"0 是否合法"逐字段分开判：
// discount_percent 的 0 是"任何折扣都要审批"（朝严），另三个的 0 是拆闸（朝松），
// 同一个数值在两个字段上方向相反，一刀切的 `> 0` 会把合法配置拒掉。
func (c LTCConfig) Validate() error {
	// 放量档单独先判、单独一个前缀：把它塞进"阈值非法"里，运营改的是档位却被告知
	// 阈值有问题，等于把一次 400 的归因指错方向。
	if err := c.ReachRollout.validateReachRollout(); err != nil {
		return fmt.Errorf("放量档非法：%w", err)
	}
	var bad []string
	checkFloat := func(name string, v float64, lo float64, hi float64, zeroLegal bool) {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			bad = append(bad, fmt.Sprintf("%s 不是有限数值（%v）", name, v))
			return
		}
		if v == 0 && zeroLegal {
			return
		}
		if v < lo || v > hi {
			bad = append(bad, fmt.Sprintf("%s 必须在 %s~%s（给 0 等于把这道闸门拆掉）", name,
				strconv.FormatFloat(lo, 'f', -1, 64), strconv.FormatFloat(hi, 'f', -1, 64)))
		}
	}
	if c.Thresholds.LeadScore < ltcLeadScoreMin || c.Thresholds.LeadScore > ltcLeadScoreMax {
		bad = append(bad, fmt.Sprintf("lead_score 必须在 %d~%d（它是 0-100 的线索分；给 0 等于把这道闸门拆掉，C5 要求两个闸门独立存在）",
			ltcLeadScoreMin, ltcLeadScoreMax))
	}
	checkFloat("confidence", c.Thresholds.Confidence, ltcRateMin, ltcRateMax, false)
	checkFloat("win_probability", c.Thresholds.WinProbability, ltcRateMin, ltcRateMax, false)
	checkFloat("discount_percent", c.Thresholds.DiscountPercent, ltcDiscountMin, ltcDiscountMax, true)

	if len(bad) > 0 {
		return fmt.Errorf("阈值非法：%s", strings.Join(bad, "；"))
	}
	return nil
}

// StageEnabled 报告阶段是否生效（两道锁都过才算）。
func (c *LTCConfig) StageEnabled(stage LTCStage) bool {
	if c == nil {
		return false
	}
	if !c.Enabled {
		return false
	}
	on, known := c.StagesEnabled.Get(stage)
	return known && on
}

// StageActive 是带原因的版本，供闸门与响应使用。
func (c *LTCConfig) StageActive(stage LTCStage) (bool, string) {
	if c == nil {
		return false, LTCReasonDegraded
	}
	if _, known := c.StagesEnabled.Get(stage); !known {
		return false, LTCReasonUnknownStage
	}
	if c.Degraded {
		return false, LTCReasonDegraded
	}
	if !c.Enabled {
		return false, LTCReasonMasterOff
	}
	on, _ := c.StagesEnabled.Get(stage)
	if !on {
		return false, LTCReasonStageOff
	}
	return true, LTCReasonActive
}

// LeadQualified 兑现 C5 的硬约束：`lead_score ≥ 阈值 ∧ confidence ≥ 阈值`，两个独立闸门。
//
// 没有"合成分"入口是刻意的 —— 一旦提供 LeadQualified(score,conf)= f(score*conf)，
// 高价值客户拿到错误答复也会自动建商机，正是 C5 判掉的 A 组简化。
func (c *LTCConfig) LeadQualified(leadScore int, confidence float64) (bool, string) {
	if leadScore < c.Thresholds.LeadScore {
		return false, fmt.Sprintf("lead_score %d < 阈值 %d", leadScore, c.Thresholds.LeadScore)
	}
	if confidence < c.Thresholds.Confidence {
		return false, fmt.Sprintf("confidence %v < 阈值 %v", confidence, c.Thresholds.Confidence)
	}
	return true, ""
}

// ReachRolloutAllows 供闸门调用：一次读（走缓存）+ 一次档位判定，与 StageActive 同一形状。
//
// 刻意不给闸门"装配期抄一份档位"的余地：AC② 要的是一键回滚，而抄快照的那把门
// 改完档位要等下次重启才咬人 —— 那正好是出事故时最没用的时候。
func (s *LTCConfigService) ReachRolloutAllows(ctx context.Context, key string) (bool, string) {
	return s.Config(ctx).ReachRolloutAllows(key)
}

// ---- T-P5-04 外联放量档位（shadow → 白名单 → 全量，另带回滚位 halt）--------------
//
// 为什么这一族旗子住在 ltc.config 而不是继续住 `FF_LTC_REACH_GATE`：环境变量在装配期读死，
// 改一次 = 发一次部署，而 AC② 要的"一键回滚 = 关开关、不回滚代码"在现网出事故时
// 根本来不及。搬进这份策略后，改档 = 一次带审计的写库，最迟一个缓存 TTL 在全副本收敛。
//
// 四档各自的语义（前三档就是卡面要的三段）：
//   - shadow    门只记录不拦 ⇒ 现网行为逐字不变。**缺省档**（也是开箱档）。
//   - whitelist 只放行名单里的判定键，其余一律拒。
//   - full      全量放行：放量走完之后退化成观察，不是"又开了一道新权"。
//   - halt      回滚位：一个都不放。它与 whitelist＋空名单做的是同一件事，
//     但后者是"以为在灰度、其实全停"的事故形状 ⇒ 全停必须是显式写出来的一个值。
//
// 与那两把旧旗子的关系（三条都是读数面上的坑，见 ReadingHints）：
//   - 档位只管"门怎么判"，**不管"门挂没挂链"**：链由 `FF_LTC_REACH_GATE` 决定，
//     它不在 shadow 之外的任何一档里能改；env 停在 off/shadow 时这里写什么都只改变记账。
//   - 与 `enabled`（LTC 总开关）互不影响：总开关管的是六阶段，外联闸门不属于任何阶段。
//   - 名单里存的是**闸门判定键本身**（`reachApprovalKey` 的形状：one_id → customer_id →
//     "渠道:收件人"），不是第二套身份系统 —— 两套授权早晚会"这里批过、那里没批过"。

type ReachRolloutMode string

const (
	ReachRolloutShadow    ReachRolloutMode = "shadow"
	ReachRolloutWhitelist ReachRolloutMode = "whitelist"
	ReachRolloutFull      ReachRolloutMode = "full"
	ReachRolloutHalt      ReachRolloutMode = "halt"
)

var ltcReachRolloutModes = []string{"shadow", "whitelist", "full", "halt"}

const (
	// ltcReachWhitelistMaxEntries 是名单条数上限。留上限不是为了省内存，是为了让
	// "整份 8KB 装不下"这一类错误先以"名单太长"这种能看懂的形式报出来。
	ltcReachWhitelistMaxEntries = 200
	// ltcReachWhitelistMaxEntryBytes 是单条判定键上限，对齐 one_id 的 varchar(128)。
	ltcReachWhitelistMaxEntryBytes = 128
)

// 放行原因逐档分开：观测面上"这条放行了"有四种成因，塌成一个 true 就等于
// 把"还在观察""已全量""名单里点名""配置读坏了"四件事读成同一件。
const (
	ReachRolloutReasonShadow         = "rollout_shadow"
	ReachRolloutReasonWhitelisted    = "rollout_whitelisted"
	ReachRolloutReasonFull           = "rollout_full"
	ReachRolloutReasonHeld           = "rollout_halted"
	ReachRolloutReasonNotWhitelisted = "rollout_not_whitelisted"
	ReachRolloutReasonDegraded       = "rollout_config_degraded"
)

type ReachRollout struct {
	Mode      ReachRolloutMode `json:"mode"`
	Whitelist []string         `json:"whitelist,omitempty"`
}

// reachRolloutEnvelope 是这一节的**解析形状**，与上面的结构体分开是必要的：
// 指针才能区分"这一节没写"（整节缺省 = 第一档）与"写了节但没写 mode"（看起来配了、
// 其实一行没生效 ⇒ 拒收），名单同理要能区分缺省与非空。
type reachRolloutEnvelope struct {
	Mode      *string   `json:"mode"`
	Whitelist *[]string `json:"whitelist"`
}

// ReachRolloutAllows 判定一个外发对象在当前档位下是否放行，并给出可上报的原因。
//
// 故障方向刻意与整份文件的"缺省与异常一律朝关"对齐成**拒绝**：degraded 时这份的
// 内容恰恰是默认值 shadow（放行），若照默认判定，一次存储抖动就会把"白名单灰度中"
// 静默升级成"全量放行"。
func (c *LTCConfig) ReachRolloutAllows(key string) (bool, string) {
	if c == nil || c.Degraded {
		return false, ReachRolloutReasonDegraded
	}
	switch c.ReachRollout.Mode {
	case ReachRolloutWhitelist:
		for _, allowed := range c.ReachRollout.Whitelist {
			if allowed == key {
				return true, ReachRolloutReasonWhitelisted
			}
		}
		return false, ReachRolloutReasonNotWhitelisted
	case ReachRolloutFull:
		return true, ReachRolloutReasonFull
	case ReachRolloutHalt:
		return false, ReachRolloutReasonHeld
	case "", ReachRolloutShadow:
		return true, ReachRolloutReasonShadow
	}
	// 写侧已经把值域钉死，走到这里只剩"有人绕过解析器手搭了一份配置"这一种可能 ⇒ 取严。
	return false, ReachRolloutReasonDegraded
}

// validateReachRollout 校验档位与名单的形状。归一化（trim＋去重）在 ParseLTCConfig 里做，
// 这里只负责拒绝：两者分开是因为 Validate 也被"进程内手搭的 struct"这条路调用（Save），
// 那条路没有"缺节"可言，只判值本身合不合法。
func (r ReachRollout) validateReachRollout() error {
	mode := string(r.Mode)
	if mode == "" {
		mode = string(ReachRolloutShadow)
	}
	known := false
	for _, m := range ltcReachRolloutModes {
		if m == mode {
			known = true
			break
		}
	}
	if !known {
		return fmt.Errorf("reach_rollout.mode=%q 不是合法档位（四档：%s）", r.Mode, strings.Join(ltcReachRolloutModes, "/"))
	}
	switch ReachRolloutMode(mode) {
	case ReachRolloutWhitelist:
		if len(r.Whitelist) == 0 {
			return errors.New("whitelist 档的名单是空的 ⇒ 这等于一个都不放，而\"全停\"请显式写 mode=halt（halt 档）而不是靠空名单凑出来")
		}
	case ReachRolloutShadow, ReachRolloutFull, ReachRolloutHalt:
		if len(r.Whitelist) > 0 {
			return fmt.Errorf("%s 档不读名单，这里却给了 %d 条 ⇒ 这些条目永远不会生效，先清掉名单再改档", mode, len(r.Whitelist))
		}
	}
	seen := make(map[string]bool, len(r.Whitelist))
	for i, entry := range r.Whitelist {
		if entry == "" {
			return fmt.Errorf("名单第 %d 条是空条目：空串永远匹配不上任何判定键，只会让人以为已经放行了一批", i+1)
		}
		if entry != strings.TrimSpace(entry) {
			return fmt.Errorf("名单第 %d 条带首尾空格（%q）：判定键是精确等值比较，空格会让它静默失配", i+1, entry)
		}
		if len(entry) > ltcReachWhitelistMaxEntryBytes {
			return fmt.Errorf("名单第 %d 条 %d 字节，超过单条上限 %d（判定键最长就是 one_id 的 %d）",
				i+1, len(entry), ltcReachWhitelistMaxEntryBytes, ltcReachWhitelistMaxEntryBytes)
		}
		if seen[entry] {
			return fmt.Errorf("名单里有重复条目 %q：重复会让\"我放行了几个对象\"这个数对不上，先去重", entry)
		}
		seen[entry] = true
	}
	if len(r.Whitelist) > ltcReachWhitelistMaxEntries {
		return fmt.Errorf("名单 %d 条，超过上限 %d 条：白名单灰度不是批量导入的入口，规模上来了该走 whitelist→full 这一档",
			len(r.Whitelist), ltcReachWhitelistMaxEntries)
	}
	return nil
}

// ReadingHints 把这份数字最容易被读错的三处写在响应里（与 /agent/tools/risk 同形制）。
// guardedRoutes 是"当前已经挂在闸门下的各阶段路由数"，为空就是本卡交付态的真实形状。
func (c *LTCConfig) ReadingHints(guardedRoutes map[LTCStage]int) []string {
	hints := []string{
		fmt.Sprintf("多副本部署下，改完开关对本进程立即生效、对其余副本最迟 %s 生效（缓存 TTL），不是「改完即全网生效」。", LTCConfigCacheTTL),
	}
	if c.Degraded {
		hints = append(hints, "degraded=true ⇒ 这份是回落默认（全关），不是运营配出来的状态；原因见 degrade_reason，先修存储再看开关")
	} else {
		hints = append(hints, "degraded=false 且 enabled=false 才是「运营关着」；两者要能分开")
	}
	active := 0
	for _, n := range guardedRoutes {
		active += n
	}
	if active == 0 {
		hints = append(hints, "当前没有任何路由挂在闸门下 ⇒ 把这里全打开不会改变任何现网行为；"+
			"闸门的生产挂载点从 T-P4-05 起逐阶段接入（本卡交付的是落点与判据，不是已生效的开关面）")
	}
	return hints
}

// ltcKVStore 是策略存储需要的那几个方法（repository.SystemConfigKVRepository 结构上即满足）。
//
// EnsureTable 也必须留在接口里：真实仓储的 Upsert 在首写撞见缺表时会自建再重试，
// 把它从接口上抹掉就等于悄悄换了写侧语义 —— 测试里的假存储也就此"证明"了一件
// 生产里并不成立的事。
type ltcKVStore interface {
	Available() bool
	Get(ctx context.Context, key string) (string, error)
	Upsert(ctx context.Context, key, value string) (string, error)
	EnsureTable(ctx context.Context) error
}

// ltcAuditWriter 只取审计需要的那一个方法（repository.OperationLogRepository 满足）。
// 单独收窄是为了让"审计写失败"这一支能被测试直接喂进一个必然失败的实现。
type ltcAuditWriter interface {
	Create(ctx context.Context, log *model.OperationLog) error
}

// LTCConfigSaveResult 把"存没存进去"和"审计留没留下"分开报出来。
// 审计写失败不回滚配置：开关已经生效是既成事实，把它伪装成失败会让运营再点一次而变成两次变更。
type LTCConfigSaveResult struct {
	Persisted    bool     `json:"persisted"`
	AuditWritten bool     `json:"audit_written"`
	AuditError   string   `json:"audit_error,omitempty"`
	StoredBytes  int      `json:"stored_bytes"`
	StagesOn     []string `json:"stages_on"`
}

// LTCConfigService 读写 ltc.config。
type LTCConfigService struct {
	store ltcKVStore
	audit ltcAuditWriter

	mu       sync.RWMutex
	cached   *LTCConfig
	cachedAt time.Time
	nowFn    func() time.Time
}

// NewLTCConfigService 用全局 KV 仓储与全局审计仓储构造。无库句柄不报错：那时读出来就是默认全关 + degraded，
// 因为"库里没东西"和"库连不上"都必须让闸门朝关，而不是让装配失败。
func NewLTCConfigService() *LTCConfigService {
	svc := NewLTCConfigServiceWithStore(repository.NewSystemConfigKVRepository())
	svc.audit = repository.NewOperationLogRepository()
	return svc
}

// NewLTCConfigServiceWithStore 注入存储（测试与影子装配用）。
func NewLTCConfigServiceWithStore(store ltcKVStore) *LTCConfigService {
	return &LTCConfigService{store: store, nowFn: time.Now}
}

func (s *LTCConfigService) now() time.Time {
	if s.nowFn != nil {
		return s.nowFn()
	}
	return time.Now()
}

// Config 读当前生效策略：缓存优先，miss 或过 TTL 则重读。
func (s *LTCConfigService) Config(ctx context.Context) *LTCConfig {
	s.mu.RLock()
	cached, fresh := s.cached, s.cached != nil && s.now().Before(s.cachedAt.Add(LTCConfigCacheTTL))
	s.mu.RUnlock()
	if fresh {
		return cloneLTCConfig(cached)
	}
	loaded := s.load(ctx)
	s.mu.Lock()
	s.cached = loaded
	s.cachedAt = s.now()
	s.mu.Unlock()
	return cloneLTCConfig(loaded)
}

// InvalidateCache 丢弃进程内缓存（下一次读重拉）。
func (s *LTCConfigService) InvalidateCache() {
	s.mu.Lock()
	s.cached = nil
	s.mu.Unlock()
}

// load 是唯一读路径，任何异常都返回默认全关并说明原因。
func (s *LTCConfigService) load(ctx context.Context) *LTCConfig {
	def := DefaultLTCConfig()
	if s.store == nil || !s.store.Available() {
		def.Degraded = true
		def.DegradeReason = "配置存储不可用（无 DB 句柄）⇒ 按默认全关处理"
		return def
	}
	raw, err := s.store.Get(ctx, LTCConfigKVKey)
	if err != nil {
		def.Degraded = true
		def.DegradeReason = fmt.Sprintf("读取 %s 失败：%v ⇒ 按默认全关处理", LTCConfigKVKey, err)
		logger.Warnf("[ltc-config] ⚠️ %s", def.DegradeReason)
		return def
	}
	if strings.TrimSpace(raw) == "" {
		return def
	}
	cfg, err := ParseLTCConfig([]byte(raw))
	if err != nil {
		def.Degraded = true
		def.DegradeReason = fmt.Sprintf("%s 内容不可采信：%v ⇒ 按默认全关处理", LTCConfigKVKey, err)
		logger.Warnf("[ltc-config] ⚠️ %s", def.DegradeReason)
		return def
	}
	cfg.Source = SourceLTC
	return cfg
}

// StageActive 供闸门调用：一次读 + 一次判定。
func (s *LTCConfigService) StageActive(ctx context.Context, stage LTCStage) (bool, string) {
	return s.Config(ctx).StageActive(stage)
}

// Save 校验 → 落库 → 失效自己的缓存 → 写审计。
// 返回的 result 里 audit_written 独立于 err：见 LTCConfigSaveResult 的注释。
func (s *LTCConfigService) Save(ctx context.Context, cfg *LTCConfig, actorID uint) (*LTCConfigSaveResult, error) {
	if cfg == nil {
		return nil, errors.New("ltc.config 不能为空")
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("拒绝保存：%w", err)
	}
	if s.store == nil || !s.store.Available() {
		return nil, fmt.Errorf("%w（无 DB 句柄）⇒ ltc.config 未写入", ErrLTCStoreUnavailable)
	}
	out := cfg.Normalized()
	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("序列化失败：%w", err)
	}
	if len(encoded) > LTCConfigMaxBytes {
		return nil, fmt.Errorf("ltc.config 序列化后 %d 字节，超过 %d 上限", len(encoded), LTCConfigMaxBytes)
	}
	prev, prevErr := s.store.Get(ctx, LTCConfigKVKey)
	if prevErr != nil {
		// 读不到旧值不拦写入：旧值是什么是审计上的锦上添花，拦下运营要做的变更是本末倒置。
		logger.Warnf("[ltc-config] 读旧值失败（继续写入）：%v", prevErr)
	}
	if _, err := s.store.Upsert(ctx, LTCConfigKVKey, string(encoded)); err != nil {
		// 归到 ErrLTCStoreUnavailable 而不是裸错误：写不进去是存储侧的事，
		// 让 HTTP 层据此回 503，别让运营以为是自己填的值被拒了。
		return nil, fmt.Errorf("%w：写入失败：%v", ErrLTCStoreUnavailable, err)
	}

	res := &LTCConfigSaveResult{Persisted: true, StoredBytes: len(encoded), StagesOn: out.StagesEnabled.OnList()}
	s.mu.Lock()
	s.cached = out
	s.cachedAt = s.now()
	s.mu.Unlock()

	detail := out.AuditDetail()

	if s.audit == nil {
		// 不静默补装一个全局仓储：那样"审计没落库"会被伪装成"审计写成功了"。
		// 开关已写入是既成事实，回滚反而制造第二次不一致 ⇒ 只把缺的那半照实报出来。
		res.AuditError = "审计仓储未装配 ⇒ 这次变更在 operation_logs 里没有对应行"
		logger.Errorf("[ltc-config] ❌ %s（actor=%d）", res.AuditError, actorID)
		logger.Infof("[ltc-config] ✅ ltc.config 已更新：actor=%d %s", actorID, detail)
		return res, nil
	}
	if err := s.audit.Create(ctx, &model.OperationLog{
		UserID:   actorID,
		Action:   "update",
		Module:   LTCConfigAuditModule,
		Resource: LTCConfigKVKey,
		Detail:   detail,
		OldValue: prev,
		NewValue: string(encoded),
	}); err != nil {
		res.AuditError = err.Error()
		logger.Errorf("[ltc-config] ❌ 开关已改但审计没落库（%v）⇒ operation_logs 里查不到这次变更，需人工补记", err)
	} else {
		res.AuditWritten = true
	}
	logger.Infof("[ltc-config] ✅ ltc.config 已更新：actor=%d %s", actorID, detail)
	return res, nil
}

// AuditDetail 是 operation_logs.detail 那一行的内容，同时也是 Save 失败路径的日志正文。
// 单独成方法而不是留在 Save 里拼：改一个字段就得同时改审计与日志，两处措辞迟早分叉。
//
// 档位与名单条数必须出现在这里 —— 运营这次改的**就是**放量档时，审计面上只记着
// enabled/stages/thresholds 等于这次变更在审计上没发生过。
func (c LTCConfig) AuditDetail() string {
	mode := c.ReachRollout.Mode
	if mode == "" {
		mode = ReachRolloutShadow
	}
	return fmt.Sprintf("enabled=%t stages_on=[%s] thresholds={lead_score=%d confidence=%v discount_percent=%v win_probability=%v} reach_rollout=%s 名单 %d 条",
		c.Enabled, strings.Join(c.StagesEnabled.OnList(), ","), c.Thresholds.LeadScore, c.Thresholds.Confidence,
		c.Thresholds.DiscountPercent, c.Thresholds.WinProbability, mode, len(c.ReachRollout.Whitelist))
}

// Normalized 返回一份"存得回去"的副本：剥掉读路径元信息（degraded/source），
// 否则一次故障读会被写回成配置继承给下一个副本。
func (c *LTCConfig) Normalized() *LTCConfig {
	out := *c
	out.Degraded = false
	out.DegradeReason = ""
	out.Source = ""
	// 档位落库前补成显式值：读侧把 `"mode":""` 判成非法档位（那是"看起来配了、其实
	// 没配"的形状），而写侧的 struct 零值恰恰是 ""。不补这一步，一次 Save 就把整份
	// 配置打成 degraded —— 连带把六个阶段一起关掉。
	if out.ReachRollout.Mode == "" {
		out.ReachRollout.Mode = ReachRolloutShadow
	}
	return &out
}

func cloneLTCConfig(c *LTCConfig) *LTCConfig {
	if c == nil {
		return DefaultLTCConfig()
	}
	cp := *c
	return &cp
}

// ParseLTCConfig 严格解析：未知键、类型错、缺阈值都拒。
//
// 不用"只认已知键、其余忽略"的宽容解法是有原因的：`colleciton: true` 这种拼写错误在宽容解法下
// 得到的是"配置保存成功、阶段没开"，而运维看到的响应是 200 —— 与 password_policy 那套
// "名义与实现相反"同源，本卡要修的就是这一类。
func ParseLTCConfig(raw []byte) (*LTCConfig, error) {
	if err := checkJSONShape(raw); err != nil {
		return nil, err
	}
	var envelope struct {
		Enabled       bool                       `json:"enabled"`
		StagesEnabled map[string]bool            `json:"stages_enabled"`
		Thresholds    map[string]json.RawMessage `json:"thresholds"`
		ReachRollout  *reachRolloutEnvelope      `json:"reach_rollout"`
	}
	if err := decodeStrict(raw, &envelope); err != nil {
		return nil, err
	}

	cfg := &LTCConfig{Enabled: envelope.Enabled}

	for k, v := range envelope.StagesEnabled {
		if _, known := (LTCStages{}).Get(LTCStage(k)); !known {
			return nil, fmt.Errorf("stages_enabled 里有不认识的阶段名 %q（认识的六个：%s）；这个键写在这里不会生效，故整份拒收",
				k, joinStages())
		}
		cfg.StagesEnabled = cfg.StagesEnabled.With(LTCStage(k))
		if !v {
			// With 只能置真，显式 false 的键要落回 false，否则"关掉某阶段"这条写不了。
			if err := unsetStage(&cfg.StagesEnabled, LTCStage(k)); err != nil {
				return nil, err
			}
		}
	}

	if len(envelope.Thresholds) == 0 {
		return nil, errors.New("thresholds 必填且不能为空对象：缺省阈值会塌成 0，而 0 等于拆掉那道闸门")
	}
	// 先报"不认识的键"再报"缺哪个键"：写了 score 的人看见"缺 lead_score"只会更迷惑，
	// 他真正需要知道的是那个键名压根不存在、并且合并分数这件事被明确禁止。
	for name := range envelope.Thresholds {
		switch name {
		case "lead_score", "confidence", "discount_percent", "win_probability":
		default:
			return nil, fmt.Errorf("thresholds 里有不认识的键 %q：合并成单一分数（如 lead_confidence / score）是被 C5 明确禁止的，"+
				"这里也不会被采信（四项阈值名：%s）", name, strings.Join(LTCKnownThresholdKeys, " / "))
		}
	}
	for _, name := range LTCKnownThresholdKeys {
		if _, ok := envelope.Thresholds[name]; !ok {
			return nil, fmt.Errorf("thresholds 缺 %q：四项必须逐条给出（C5 要求各自独立可配，缺省不是「独立」而是「没配」）", name)
		}
	}
	encoded, err := json.Marshal(envelope.Thresholds)
	if err != nil {
		return nil, fmt.Errorf("thresholds 重编码失败：%w", err)
	}
	if err := decodeStrict(encoded, &cfg.Thresholds); err != nil {
		return nil, err
	}

	// 放量档：整节缺省 = 第一档 shadow（今天库里的存量配置就没有这一节，
	// 把"缺节"判成拒收会让下一次读整份回落默认，连带关掉已上线的六阶段开关）。
	// 但"写了这一节却没写 mode"必须拒 —— 那是"看起来配了、其实一行没生效"的形状。
	cfg.ReachRollout = ReachRollout{Mode: ReachRolloutShadow}
	if e := envelope.ReachRollout; e != nil {
		if e.Mode == nil {
			return nil, fmt.Errorf("reach_rollout 缺 mode：四档 %s 必须显式选一档（缺字段不等于\"沿用默认\"，这一档现在没定）",
				strings.Join(ltcReachRolloutModes, "/"))
		}
		mode := ReachRolloutMode(strings.ToLower(strings.TrimSpace(*e.Mode)))
		known := false
		for _, m := range ltcReachRolloutModes {
			if string(mode) == m {
				known = true
				break
			}
		}
		if !known {
			return nil, fmt.Errorf("reach_rollout.mode=%q 不是合法档位（四档：%s）；这个值不会被采信成任何一档，故整份拒收",
				*e.Mode, strings.Join(ltcReachRolloutModes, "/"))
		}
		cfg.ReachRollout.Mode = mode
		if e.Whitelist != nil {
			// trim 后按首次出现顺序去重：写进去的形状就是生效的形状。
			// 空条目原样带着走，交给 Validate 点名 —— 在这儿悄悄删掉会让响应里的
			// "名单 3 条"变成"名单 2 条"，而运营需要知道自己多写了一条空的。
			seen := make(map[string]bool, len(*e.Whitelist))
			entries := make([]string, 0, len(*e.Whitelist))
			for _, raw := range *e.Whitelist {
				entry := strings.TrimSpace(raw)
				if entry != "" && seen[entry] {
					continue
				}
				seen[entry] = true
				entries = append(entries, entry)
			}
			cfg.ReachRollout.Whitelist = entries
		}
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func unsetStage(s *LTCStages, stage LTCStage) error {
	switch stage {
	case LTCStageOpportunity:
		s.Opportunity = false
	case LTCStageOutreach:
		s.Outreach = false
	case LTCStageQuote:
		s.Quote = false
	case LTCStageBill:
		s.Bill = false
	case LTCStagePayment:
		s.Payment = false
	case LTCStageCollection:
		s.Collection = false
	default:
		return fmt.Errorf("未知阶段 %s", stage)
	}
	return nil
}

func joinStages() string {
	parts := make([]string, 0, len(LTCKnownStages))
	for _, s := range LTCKnownStages {
		parts = append(parts, string(s))
	}
	return strings.Join(parts, ",")
}

func checkJSONShape(raw []byte) error {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return fmt.Errorf("不是合法的 JSON 对象：%w", err)
	}
	if _, ok := probe["stages_enabled"]; !ok {
		return errors.New("缺 stages_enabled：六阶段开关必须显式给出（整个字段缺省等价于「全关」，而全关应当由 enabled 表达）")
	}
	return nil
}

func decodeStrict(raw []byte, dst any) error {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

// ---- 进程级实例（与 ConfigParamService 同一形状：后写覆盖，不用 sync.Once）------

var (
	globalLTCConfigMu sync.RWMutex
	globalLTCConfig   *LTCConfigService
)

// SetGlobalLTCConfig 装配层注入。用后写覆盖而非 sync.Once：
// Once 会让第二次装配被静默丢弃，而本服务的缓存正是"装配那一刻读到的值"。
func SetGlobalLTCConfig(svc *LTCConfigService) {
	globalLTCConfigMu.Lock()
	globalLTCConfig = svc
	globalLTCConfigMu.Unlock()
}

// GlobalLTCConfig 取全局实例；未装配时惰性建一个走全局仓储的实例（nil-safe，读不到即默认全关）。
func GlobalLTCConfig() *LTCConfigService {
	globalLTCConfigMu.RLock()
	svc := globalLTCConfig
	globalLTCConfigMu.RUnlock()
	if svc != nil {
		return svc
	}
	built := NewLTCConfigService()
	globalLTCConfigMu.Lock()
	if globalLTCConfig == nil {
		globalLTCConfig = built
	}
	svc = globalLTCConfig
	globalLTCConfigMu.Unlock()
	return svc
}
