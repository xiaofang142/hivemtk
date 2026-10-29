package service

// reach_compliance_policy.go E1 主动触达合规策略（地区×行业可配置）。
//
// 为什么需要它：本仓已有的合规手段只有三样——发送前的 [COMPLIANCE] WARN 日志
// （reach_send_pipeline_compliance.go）、全局退订 DNC（do_not_contact.go）、
// **一组全局的**静默时段（reach.quiet_hours_start/_end）。这三样都表达不了两件事：
//
//  1. **地区差异**：美国客户适用 20:00–10:00、国内 22:00–8:00，一组全局值必然对其中一边
//     要么违法（打扰）要么过严（发不出去）；
//  2. **行业差异**：医美/金融/教育对主动营销的同意要求与可投渠道各不相同，代码里无处表达。
//
// 另外 `reach.global_per_user_daily_limit` 只挂在批量管线（reach_pipeline.go），
// 同步发送（AI 工具 reach.send）与 proactive 按客户触达**不受任何日上限约束**。
//
// 本文件提供的是「判定器」：把一份策略文档（JSON）解析成结构化规则，对一次外发请求给出
// ComplianceDecision。判定器不碰 DB、不起协程、不写表——留痕由调用方交给既有的
// ComplianceAuditLogger（异步批量落库）完成，这样它才能在单测里被逐条钉住。
//
// 策略文档存在参数中心 reach/compliance_policy_json（ValueType=json）：
// 写侧 validateValue 已校验 JSON 语法、读侧有 60s TTL 缓存与租户覆盖、变更已有操作审计。
// 因此本功能零新表、零新路由、零新前端页面。
//
// 失败语义（刻意的，且都在用例里钉住）：
//   - 未配置 / enabled=false       ⇒ 放行，原因 policy_disabled（**默认零行为变更**）
//   - 文档 JSON 非法 / 语义越界     ⇒ 放行 + ERROR 留痕一次（宁可继续发，也不静默全员拒发；
//     但必须喊出来，不许吞掉——「红是不是常态」那种糊涂账本仓吃过一次）
//   - 单条规则的静默窗口越界(<0/>23) ⇒ 该规则视为未配置窗口（回落全局静默时段），不报错
//   - 未命中任何规则且 unknown_region_deny=false ⇒ 放行并留痕 region_unruled（可观测）

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/pkg/utils/logger"
)

const (
	// CompliancePolicyParamGroup / CompliancePolicyParamKey：策略文档在参数中心的坐标。
	// group 与其它触达参数同组（reach），运维在「触达」页签里改。
	CompliancePolicyParamGroup = "reach"
	CompliancePolicyParamKey   = "compliance_policy_json"
)

// 判定原因码。它们是**对外契约**：进审计表 reason 列、进 SendStepLog.Output、
// 进 HTTP 错误串，前端与排障脚本会按字面量匹配，改字等于改契约。
const (
	ComplianceReasonAllowed           = "allowed"
	ComplianceReasonDisabled          = "policy_disabled"
	ComplianceReasonChannelDenied     = "channel_denied"
	ComplianceReasonChannelNotAllowed = "channel_not_allowed"
	ComplianceReasonOptInRequired     = "opt_in_required"
	ComplianceReasonQuietHours        = "quiet_hours"
	ComplianceReasonDailyLimit        = "daily_limit"
	ComplianceReasonRegionUnruled     = "region_unruled"
)

// 判定动作（进审计表 decision 列）。
const (
	ComplianceDecisionAllow = "allow"
	ComplianceDecisionDeny  = "deny"
	ComplianceDecisionDefer = "defer"
)

// 请求元数据键：调用方用它们给出地区/行业/同意状态。
//
// 为什么走 Metadata 而不是给 ReachSendRequest 加字段：Metadata 已是 req 上既有的扩展位
// （island 工具/HTTP 控制器都往里塞），加字段要连带改 DTO 与工具 schema；
// 而地区/行业对「发一条消息」而言是可选上下文，不是身份的一部分。
const (
	ReachMetaRegion   = "region"
	ReachMetaIndustry = "industry"
	ReachMetaOptIn    = "opt_in"
	ReachMetaConsent  = "consent"
)

// ComplianceRule 单条规则（地区或行业一条）。
//
// 零值即「不加限制」：这样策略文档里可以只写真正要管的那几条，
// 不必为每个地区把六项全抄一遍。
type ComplianceRule struct {
	// AllowedChannels 渠道白名单：非空即生效（不在名单内的渠道一律拒）。
	AllowedChannels []string `json:"allowed_channels"`
	// DeniedChannels 渠道黑名单：优先于白名单判定。
	DeniedChannels []string `json:"denied_channels"`
	// QuietHoursStart / QuietHoursEnd 地区静默窗口（0–23，CST 小时）。
	// start > end 表示跨零点（与 inQuietHoursWindow 同口径）。nil = 用全局窗口。
	// 指针而不是 int：0 点是合法起点，用 0 当「未配置」会让「0–7 点别发」写不出来。
	QuietHoursStart *int `json:"quiet_hours_start"`
	QuietHoursEnd   *int `json:"quiet_hours_end"`
	// DailyLimit 同一客户当日触达上限（0 = 不限）。
	DailyLimit int `json:"daily_limit"`
	// RequireOptIn 要求显式同意（metadata.opt_in / consent 为 true）才可发。
	RequireOptIn bool `json:"require_opt_in"`
}

// CompliancePolicy 策略文档根。
type CompliancePolicy struct {
	Enabled bool `json:"enabled"`
	// DefaultRegion 请求未带地区时的兜底地区（大写比对）。私有化单市场部署只需写这一个字段。
	DefaultRegion string `json:"default_region"`
	// UnknownRegionDeny 未命中任何地区规则时是否拒发。
	// 默认 false：放行并留痕 region_unruled，让运维先看清「哪来的流量没有规则」再收紧。
	UnknownRegionDeny bool `json:"unknown_region_deny"`
	// Regions / Industries 规则表，键统一按「地区大写 / 行业小写」比对。
	Regions    map[string]ComplianceRule `json:"regions"`
	Industries map[string]ComplianceRule `json:"industries"`
}

// ComplianceInput 一次判定的输入（两条外发路径共用，避免两处各拼一套）。
type ComplianceInput struct {
	Channel     string
	CustomerID  string
	RecipientID string
	Region      string
	Industry    string
	// OptIn 是否已取得明确同意（请求侧显式给出）。
	OptIn bool
}

// ComplianceDecision 一次判定的结果。零值不是合法结果（Allowed=false 且无原因），
// 一律由判定器显式构造。
type ComplianceDecision struct {
	Allowed bool
	// Deferred 仅用于「可以延后」的判定（静默时段）：消息不该发、但也不该丢。
	Deferred bool
	// SendAt Deferred 时的计划发送时刻（窗口释放点）。
	SendAt   time.Time
	Reason   string
	Region   string
	Industry string
	// PolicyScope 命中的规则域（region:US / industry:medical），排障时用它回答「被哪条规则拦的」。
	PolicyScope string
}

// Decision 审计列用：allow/deny/defer。
func (d ComplianceDecision) Decision() string {
	switch {
	case d.Deferred:
		return ComplianceDecisionDefer
	case d.Allowed:
		return ComplianceDecisionAllow
	default:
		return ComplianceDecisionDeny
	}
}

// ComplianceUsageCounter 地区日上限的计数来源。
//
// 接口而不是内置 map：多实例部署时计数必须共享（Redis ZSET），
// 但当前量级用进程内实现即可（与 MemoryQuietHoursQueue / MemorySendAuditLogger 同级语义：
// 进程重启归零）。升级路径预留在装配层，不必改判定器。
type ComplianceUsageCounter interface {
	// Count 返回指定键在指定自然日（CST）已成功触达的次数。
	Count(ctx context.Context, key, day string) int
	// Incr 成功触达一次后自增。
	Incr(ctx context.Context, key, day string)
}

// MemoryComplianceUsageCounter 进程内计数实现。
//
// 有界：超过 complianceCounterMaxKeys 后整表清空（宁可少拦一天，也不让计数表无界吃内存）；
// 清理时机在 Incr，不在读路径，避免读路径加写锁。
type MemoryComplianceUsageCounter struct {
	mu   sync.Mutex
	hits map[string]int
}

const complianceCounterMaxKeys = 100000

// NewMemoryComplianceUsageCounter 构造进程内计数表。
func NewMemoryComplianceUsageCounter() *MemoryComplianceUsageCounter {
	return &MemoryComplianceUsageCounter{hits: make(map[string]int, 256)}
}

// Count 读取计数（不建键：读路径只读，避免「查一下就把表撑大」）。
func (c *MemoryComplianceUsageCounter) Count(_ context.Context, key, day string) int {
	if c == nil || key == "" {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits[day+"|"+key]
}

// Incr 自增计数。
func (c *MemoryComplianceUsageCounter) Incr(_ context.Context, key, day string) {
	if c == nil || key == "" {
		return
	}
	k := day + "|" + key
	c.mu.Lock()
	if c.hits == nil {
		c.hits = make(map[string]int, 256)
	}
	if len(c.hits) >= complianceCounterMaxKeys {
		c.hits = make(map[string]int, 256)
	}
	c.hits[k]++
	c.mu.Unlock()
}

// CompliancePolicyEvaluator 合规策略判定器（可注入接口；nil = 不挂该判定）。
//
// RecordSent 与 Evaluate 的分离是刻意的：判定发生在发送之前，计数只能发生在**真正
// 委派给渠道之后**。合到一个方法里就会出现「被拒的请求也烧掉配额」的自锁。
type CompliancePolicyEvaluator interface {
	Evaluate(ctx context.Context, req *ReachSendRequest) ComplianceDecision
	RecordSent(ctx context.Context, req *ReachSendRequest)
}

// ConfigCompliancePolicy 从参数中心读策略文档的判定器（生产实现）。
type ConfigCompliancePolicy struct {
	mu    sync.RWMutex
	usage ComplianceUsageCounter
	clock func() time.Time
	// 解析缓存：原文 -> 策略。参数中心 60s TTL 已挡住 DB 压力，
	// 这里挡的是「每次外发都 json.Unmarshal 一遍」的 CPU 与错误日志重复。
	cacheRaw string
	cachePol CompliancePolicy
	cacheOK  bool
	// lastBadRaw 上一次已 ERROR 过的坏文档，避免坏文档期间每次外发都刷一条 ERROR。
	lastBadRaw string
	// rawFn 测试注入的原文来源（生产为 nil ⇒ 读参数中心）。
	// 存在的理由：判定器是纯逻辑，用例不该为了验证一条地区规则去起一个 DB 与参数中心。
	rawFn func(ctx context.Context) string
}

// NewConfigCompliancePolicy 构造生产判定器（计数走进程内实现，时钟走 time.Now）。
func NewConfigCompliancePolicy() *ConfigCompliancePolicy {
	return &ConfigCompliancePolicy{
		usage: NewMemoryComplianceUsageCounter(),
		clock: time.Now,
	}
}

// SetUsageCounter 替换计数来源（多实例部署接 Redis / 测试注入）。
func (p *ConfigCompliancePolicy) SetUsageCounter(c ComplianceUsageCounter) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.usage = c
	p.mu.Unlock()
}

// SetClock 替换时钟（测试注入；生产为 nil 语义即 time.Now）。
func (p *ConfigCompliancePolicy) SetClock(fn func() time.Time) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.clock = fn
	p.mu.Unlock()
}

func (p *ConfigCompliancePolicy) now() time.Time {
	p.mu.RLock()
	fn := p.clock
	p.mu.RUnlock()
	if fn == nil {
		return time.Now()
	}
	return fn()
}

func (p *ConfigCompliancePolicy) counter() ComplianceUsageCounter {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.usage
}

// policy 取当前生效策略。读原文 → 解析 → 缓存。
func (p *ConfigCompliancePolicy) policy(ctx context.Context) CompliancePolicy {
	raw := p.rawPolicy(ctx)

	p.mu.RLock()
	if p.cacheOK && p.cacheRaw == raw {
		pol := p.cachePol
		p.mu.RUnlock()
		return pol
	}
	badRaw := p.lastBadRaw
	p.mu.RUnlock()

	pol, err := ParseCompliancePolicy(raw)
	if err != nil {
		// 坏文档：放行（未启用）+ 同一份原文只喊一次。
		if badRaw != raw {
			logger.Errorf("[COMPLIANCE] 策略文档不可用，已按「未启用」处理（触达不受本次策略约束）: %v", err)
			p.mu.Lock()
			p.lastBadRaw = raw
			p.mu.Unlock()
		}
		pol = CompliancePolicy{}
	}

	p.mu.Lock()
	p.cacheRaw = raw
	p.cachePol = pol
	p.cacheOK = true
	p.mu.Unlock()
	return pol
}

// rawPolicy 读策略原文。
//
// 读取点形状必须写成「GetString( 与 group/key 字面量同一行」，否则
// scripts/check-config-param-readpoints.py 认不出这个读取点（该门只认两种字面量形状：
// 同一行里既有 "key" 又有 Get*(；或 IDENT = "key" 且该 IDENT 出现在 Get* 行上）。
func (p *ConfigCompliancePolicy) rawPolicy(ctx context.Context) string {
	p.mu.RLock()
	fn := p.rawFn
	p.mu.RUnlock()
	if fn != nil {
		return fn(ctx)
	}
	return GlobalConfigParam().GetString(ctx, "reach", "compliance_policy_json", "")
}

// SetPolicySourceForTest 测试专用：直接提供策略原文，绕开参数中心与 DB。
// 生产代码禁止调用——生产策略只能来自参数中心（那是唯一可审计的变更入口）。
func (p *ConfigCompliancePolicy) SetPolicySourceForTest(fn func(ctx context.Context) string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.rawFn = fn
	p.mu.Unlock()
}

// ParseCompliancePolicy 把原文解析成策略文档，并做语义校验。
//
// 空串/空白 = 「未配置」，返回零值策略（Enabled=false）且**不报错**：
// 这是默认路径，不能让它污染错误日志。
func ParseCompliancePolicy(raw string) (CompliancePolicy, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "null" {
		return CompliancePolicy{}, nil
	}
	var pol CompliancePolicy
	if err := json.Unmarshal([]byte(trimmed), &pol); err != nil {
		return CompliancePolicy{}, fmt.Errorf("策略 JSON 解析失败: %w", err)
	}
	if err := ValidateCompliancePolicy(&pol); err != nil {
		return CompliancePolicy{}, err
	}
	return normalizeCompliancePolicy(pol), nil
}

// ValidateCompliancePolicy 语义校验。
//
// 只拦**结构性错误**（渠道名空、日上限负数、规则表里出现「什么约束都没有」的空规则除外），
// 静默窗口越界不在这里判红：见 normalizeCompliancePolicy 的「视为未配置窗口」口径。
// 理由：一个手误（把 22 写成 220）不该让全渠道停机——但这也不该静默通过，
// 所以窗口越界在规范化时会被清零并在 ERROR 日志里点名。
func ValidateCompliancePolicy(pol *CompliancePolicy) error {
	if pol == nil {
		return fmt.Errorf("策略文档为空")
	}
	if err := validateComplianceRules("regions", pol.Regions); err != nil {
		return err
	}
	return validateComplianceRules("industries", pol.Industries)
}

func validateComplianceRules(scope string, rules map[string]ComplianceRule) error {
	keys := make([]string, 0, len(rules))
	for k := range rules {
		keys = append(keys, k)
	}
	sort.Strings(keys) // 报错稳定：同一次坏文档每次报同一个键
	for _, k := range keys {
		if strings.TrimSpace(k) == "" {
			return fmt.Errorf("%s 存在空键（地区/行业名不能为空）", scope)
		}
		r := rules[k]
		if r.DailyLimit < 0 {
			return fmt.Errorf("%s.%s.daily_limit 不能为负（0 表示不限）", scope, k)
		}
		for _, ch := range append(append([]string{}, r.AllowedChannels...), r.DeniedChannels...) {
			if strings.TrimSpace(ch) == "" {
				return fmt.Errorf("%s.%s 的渠道名单里有空串（空串会误伤全部渠道）", scope, k)
			}
		}
	}
	return nil
}

// normalizeCompliancePolicy 规范化：地区键大写、行业键小写、渠道名小写去空格、
// 越界静默窗口清零并点名。
func normalizeCompliancePolicy(pol CompliancePolicy) CompliancePolicy {
	pol.DefaultRegion = strings.ToUpper(strings.TrimSpace(pol.DefaultRegion))
	pol.Regions = normalizeComplianceRuleMap("regions", pol.Regions)
	pol.Industries = normalizeComplianceRuleMap("industries", pol.Industries)
	return pol
}

func normalizeComplianceRuleMap(scope string, in map[string]ComplianceRule) map[string]ComplianceRule {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]ComplianceRule, len(in))
	for k, r := range in {
		key := strings.TrimSpace(k)
		if scope == "regions" {
			key = strings.ToUpper(key)
		} else {
			key = strings.ToLower(key)
		}
		r.AllowedChannels = normalizeChannelList(r.AllowedChannels)
		r.DeniedChannels = normalizeChannelList(r.DeniedChannels)
		r.QuietHoursStart = normalizeQuietHour(scope, key, "quiet_hours_start", r.QuietHoursStart)
		r.QuietHoursEnd = normalizeQuietHour(scope, key, "quiet_hours_end", r.QuietHoursEnd)
		out[key] = r
	}
	return out
}

func normalizeChannelList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, ch := range in {
		ch = strings.ToLower(strings.TrimSpace(ch))
		if ch != "" {
			out = append(out, ch)
		}
	}
	return out
}

func normalizeQuietHour(scope, key, field string, v *int) *int {
	if v == nil {
		return nil
	}
	if *v < 0 || *v > 23 {
		logger.Errorf("[COMPLIANCE] %s.%s.%s=%d 越界（合法 0–23），该规则视为未配置窗口（回落全局静默时段）",
			scope, key, field, *v)
		return nil
	}
	n := *v
	return &n
}

// Evaluate 供同步发送管线调用：从请求的 Metadata 取地区/行业/同意。
func (p *ConfigCompliancePolicy) Evaluate(ctx context.Context, req *ReachSendRequest) ComplianceDecision {
	if req == nil {
		return ComplianceDecision{Allowed: true, Reason: ComplianceReasonAllowed}
	}
	return p.Decide(ctx, ComplianceInput{
		Channel:     req.Channel,
		CustomerID:  req.CustomerID,
		RecipientID: req.RecipientID,
		Region:      metadataString(req.Metadata, ReachMetaRegion),
		Industry:    metadataString(req.Metadata, ReachMetaIndustry),
		OptIn:       metadataOptIn(req.Metadata),
	})
}

// RecordSent 成功委派给渠道后计数（只对配了日上限的规则计数）。
func (p *ConfigCompliancePolicy) RecordSent(ctx context.Context, req *ReachSendRequest) {
	if req == nil {
		return
	}
	p.RecordSentInput(ctx, ComplianceInput{
		Channel:     req.Channel,
		CustomerID:  req.CustomerID,
		RecipientID: req.RecipientID,
		Region:      metadataString(req.Metadata, ReachMetaRegion),
		Industry:    metadataString(req.Metadata, ReachMetaIndustry),
	})
}

// RecordSentInput 计数入参版本（proactive 路径用；与 Evaluate 的入参同构）。
func (p *ConfigCompliancePolicy) RecordSentInput(ctx context.Context, in ComplianceInput) {
	usage := p.counter()
	if usage == nil {
		return
	}
	pol := p.policy(ctx)
	if !pol.Enabled {
		return
	}
	region := normalizeRegion(in.Region)
	if region == "" {
		region = pol.DefaultRegion
	}
	industry := strings.ToLower(strings.TrimSpace(in.Industry))
	day := p.now().In(cstZone).Format("2006-01-02")

	if rule, ok := lookupComplianceRule(pol.Regions, region); ok && rule.DailyLimit > 0 {
		usage.Incr(ctx, complianceUsageKey(region, industry, in), day)
	}
	if rule, ok := lookupComplianceRule(pol.Industries, industry); ok && rule.DailyLimit > 0 {
		usage.Incr(ctx, complianceUsageKey(region, industry, in), day)
	}
}

// Decide 核心判定：地区规则与行业规则各自判、任一拒绝即拒绝、延后取最早释放点。
func (p *ConfigCompliancePolicy) Decide(ctx context.Context, in ComplianceInput) ComplianceDecision {
	pol := p.policy(ctx)
	dec := ComplianceDecision{Allowed: true, Reason: ComplianceReasonAllowed}
	dec.Region = normalizeRegion(in.Region)
	if dec.Region == "" {
		dec.Region = pol.DefaultRegion
	}
	dec.Industry = strings.ToLower(strings.TrimSpace(in.Industry))

	if !pol.Enabled {
		dec.Reason = ComplianceReasonDisabled
		return dec
	}

	now := p.now()

	// 命中候选：地区规则与行业规则各自独立命中。
	type ruleHit struct {
		rule  ComplianceRule
		scope string
	}
	var hits []ruleHit
	if r, ok := lookupComplianceRule(pol.Regions, dec.Region); ok {
		hits = append(hits, ruleHit{rule: r, scope: "region:" + dec.Region})
	} else if pol.UnknownRegionDeny {
		dec.Allowed = false
		dec.Reason = ComplianceReasonRegionUnruled
		dec.PolicyScope = "region:" + dec.Region
		return dec
	} else if dec.Region != "" || len(pol.Regions) > 0 {
		// 放行但要让人看得见「这批流量落在没有规则的地区」。
		dec.Reason = ComplianceReasonRegionUnruled
		dec.PolicyScope = "region:" + dec.Region
	}
	if r, ok := lookupComplianceRule(pol.Industries, dec.Industry); ok {
		hits = append(hits, ruleHit{rule: r, scope: "industry:" + dec.Industry})
	}

	var earliest time.Time
	for _, h := range hits {
		res := p.judgeRule(ctx, h.rule, in, dec.Region, dec.Industry, now)
		switch res.kind {
		case ComplianceDecisionDeny:
			dec.Allowed = false
			dec.Deferred = false
			dec.Reason = res.reason
			dec.PolicyScope = h.scope
			return dec
		case ComplianceDecisionDefer:
			if earliest.IsZero() || res.sendAt.Before(earliest) {
				earliest = res.sendAt
				dec.Reason = res.reason
				dec.PolicyScope = h.scope
			}
		}
	}
	if !earliest.IsZero() {
		dec.Allowed = true
		dec.Deferred = true
		dec.SendAt = earliest
		return dec
	}
	return dec
}

type ruleJudgement struct {
	kind   string // ComplianceDecisionAllow / Deny / Defer
	reason string
	sendAt time.Time
}

// judgeRule 单条规则的判定，顺序即优先级：黑名单 > 白名单 > 同意 > 静默时段 > 日上限。
//
// 为什么拒绝类排在延后类之前：一条被地区明令禁止的渠道，不该因为「现在是静默时段」
// 而进延迟队列，然后 8 小时后照样发出去。
func (p *ConfigCompliancePolicy) judgeRule(ctx context.Context, rule ComplianceRule, in ComplianceInput, region, industry string, now time.Time) ruleJudgement {
	channel := strings.ToLower(strings.TrimSpace(in.Channel))

	if containsString(rule.DeniedChannels, channel) {
		return ruleJudgement{kind: ComplianceDecisionDeny, reason: ComplianceReasonChannelDenied}
	}
	if len(rule.AllowedChannels) > 0 && !containsString(rule.AllowedChannels, channel) {
		return ruleJudgement{kind: ComplianceDecisionDeny, reason: ComplianceReasonChannelNotAllowed}
	}
	if rule.RequireOptIn && !in.OptIn {
		return ruleJudgement{kind: ComplianceDecisionDeny, reason: ComplianceReasonOptInRequired}
	}
	// 合规静默窗口维持 cstZone（J-12 分层：这是监管口径，不随客户时区漂移；
	// 客户当地时区只作用于 R-4 静默窗口，见 reach_customer_zone.go）。
	if start, end, ok := complianceQuietWindow(rule); ok && inQuietHoursWindow(now, start, end, cstZone) {
		return ruleJudgement{kind: ComplianceDecisionDefer, reason: ComplianceReasonQuietHours, sendAt: nextQuietHoursRelease(now, end, cstZone)}
	}
	if rule.DailyLimit > 0 {
		if usage := p.counter(); usage != nil {
			day := now.In(cstZone).Format("2006-01-02")
			if usage.Count(ctx, complianceUsageKey(region, industry, in), day) >= rule.DailyLimit {
				return ruleJudgement{kind: ComplianceDecisionDeny, reason: ComplianceReasonDailyLimit}
			}
		}
	}
	return ruleJudgement{kind: ComplianceDecisionAllow, reason: ComplianceReasonAllowed}
}

// complianceQuietWindow 取规则窗口；未配置（或规范化时被判越界清零）则回落全局静默时段。
func complianceQuietWindow(rule ComplianceRule) (int, int, bool) {
	if rule.QuietHoursStart == nil && rule.QuietHoursEnd == nil {
		return 0, 0, false
	}
	start := quietHoursStartHour
	if rule.QuietHoursStart != nil {
		start = *rule.QuietHoursStart
	}
	end := quietHoursEndHour
	if rule.QuietHoursEnd != nil {
		end = *rule.QuietHoursEnd
	}
	return start, end, true
}

// complianceUsageKey 计数键：地区|行业|渠道|客户（无 customer_id 用收件人）。
func complianceUsageKey(region, industry string, in ComplianceInput) string {
	who := in.CustomerID
	if who == "" {
		who = in.RecipientID
	}
	if who == "" {
		who = "-"
	}
	return strings.Join([]string{region, industry, strings.ToLower(strings.TrimSpace(in.Channel)), who}, "|")
}

func lookupComplianceRule(rules map[string]ComplianceRule, key string) (ComplianceRule, bool) {
	if len(rules) == 0 || key == "" {
		return ComplianceRule{}, false
	}
	r, ok := rules[key]
	return r, ok
}

func normalizeRegion(v string) string { return strings.ToUpper(strings.TrimSpace(v)) }

func metadataString(md map[string]string, key string) string {
	if md == nil {
		return ""
	}
	return strings.TrimSpace(md[key])
}

func metadataOptIn(md map[string]string) bool {
	if md == nil {
		return false
	}
	for _, key := range []string{ReachMetaOptIn, ReachMetaConsent} {
		if v, ok := md[key]; ok && isTruthyMetadata(v) {
			return true
		}
	}
	return false
}

func isTruthyMetadata(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "y", "on":
		return true
	}
	return false
}

// 判定器全局单例。
//
// 为什么是全局而不是逐构造点注入：ProactiveReachService 有 4 个构造点
// （router/service_routes.go、app/quote_wiring.go、app/collection_wiring.go、
// app/recovery_worker_wiring.go），逐个注入等于「漏接一个就静默失效」——
// 本仓的外发闸门（reach_gate_wiring.go）与 WS 租户桶（A14）都是这么踩过坑的。
// 判定点只有一处（enforcePreSendApproval），装配点因此做成零。
var (
	globalCompliancePolicyMu sync.RWMutex
	globalCompliancePolicy   *ConfigCompliancePolicy
)

// GlobalCompliancePolicy 获取全局判定器（懒初始化，nil-safe）。
func GlobalCompliancePolicy() *ConfigCompliancePolicy {
	globalCompliancePolicyMu.RLock()
	p := globalCompliancePolicy
	globalCompliancePolicyMu.RUnlock()
	if p != nil {
		return p
	}
	globalCompliancePolicyMu.Lock()
	defer globalCompliancePolicyMu.Unlock()
	if globalCompliancePolicy == nil {
		globalCompliancePolicy = NewConfigCompliancePolicy()
	}
	return globalCompliancePolicy
}

// SetGlobalCompliancePolicyForTest 测试专用：替换全局判定器（传 nil 复位为懒初始化）。
// 生产代码禁止调用——生产一律走 GlobalCompliancePolicy()。
func SetGlobalCompliancePolicyForTest(p *ConfigCompliancePolicy) {
	globalCompliancePolicyMu.Lock()
	globalCompliancePolicy = p
	globalCompliancePolicyMu.Unlock()
}
