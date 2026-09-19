// kb_canary.go 知识库答案缓存的版本与灰度路由（新规划 T-P2-05 / G-1）。
//
// 一句话口径：本卡把 rag_answer_cache 里那个写死的 prompt_version="v1" 变成可按客户
// 分桶路由的命名空间；版本的物理载体是命名空间号，不是新的表、也不是 knowledge_bases
// 的行复制。四版口径（生效面/折算规则/转正回滚/分桶复用）写在 model.KnowledgeBase
// 的类型文档上，本文件是它的唯一实现处。
//
// 为什么不做"一行一版本的兄弟行"方案（实测否决，不是偏好）：
//   - knowledge_bases.kb_code 上有 UNIQUE 索引，兄弟行要么改索引（AutoMigrate 不删
//     旧索引，得在启动期手工 DROP，属 schema 变更），要么给 kb_code 编版本号后缀；
//   - 生产检索链根本不看 knowledge_bases 行：FAQ 按 agent_id 取数、RAG 按 product_id
//     取数，唯一把 kbID 传到检索里的 resolveFAQKBID 只喂 rag_answer_cache（实测见 G18）
//     ⇒ 复制行等于多一堆没人读的行，还把 1:1 隔离语义搅回 ADR-014 已经 Simplified 掉
//     的那摊。所以版本落在命名空间上，行只有一行。
//
// 五层归属：L4 业务层（决策 + 管理入口），DB 读写全部经 repository。
package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// KBCanaryFlagEnv 版本机制总开关。与 KB 行上的 CanaryPercent 是**两层独立的锁**：
// 旗子关 ⇒ 无论行上怎么配都恒用今天的键；旗子开但行没 publish 过 ⇒ 也恒用今天的键。
// 只有两道都放行才换命名空间，所以默认态下本卡对线上零影响。
const KBCanaryFlagEnv = "FF_LTC_KB_CANARY"

type kbCanaryMode string

const (
	kbCanaryOff    kbCanaryMode = "off"
	kbCanaryShadow kbCanaryMode = "shadow"
	kbCanaryOn     kbCanaryMode = "on"
)

// parseKBCanaryMode 解析开关值。
//
// 与 tool_circuit_breaker_wiring.go 同一取舍：布尔式真值（true/1/yes/on）只到 shadow，
// 一把能改变生产缓存键的旗子不该因为有人按习惯写了 `=true` 就直接拿到换键的能力；
// 要真换键必须显式写 on。认不出的值判 off 并告警。
func parseKBCanaryMode(raw string) kbCanaryMode {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "":
		return kbCanaryOff
	case "off", "false", "0", "no", "n", "none", "disabled":
		return kbCanaryOff
	case "shadow", "observe", "watch", "log", "report":
		return kbCanaryShadow
	case "on", "enforce", "active":
		return kbCanaryOn
	}
	if b, err := strconv.ParseBool(v); err == nil {
		if b {
			return kbCanaryShadow
		}
		return kbCanaryOff
	}
	switch v {
	case "yes", "y":
		return kbCanaryShadow
	}
	logger.Warnf("[kb-canary] %s=%q 无法识别 ⇒ 按 off 处理（版本路由不生效，恒用今天的缓存键）；可用值：off|shadow|on", KBCanaryFlagEnv, raw)
	return kbCanaryOff
}

// kbCanaryModeValue 读当前开关态（每次读 env，与 envFlagEnabled 同口径，支持不改代码热切）。
func kbCanaryModeValue() kbCanaryMode { return parseKBCanaryMode(os.Getenv(KBCanaryFlagEnv)) }

// KBCanaryModeForLog 供启动期日志与观察端点回显当前旗子态。
func KBCanaryModeForLog() string { return string(kbCanaryModeValue()) }

// kbCachePromptVersion 把版本号折算成 rag_answer_cache.prompt_version 的命名空间字符串。
//
// 折算规则只有一条：version <= 1 ⇒ 常量 "v1"。它兜住的是"还没被切过版本的号"：从没
// publish 过的 1，以及 0 与负数（**实测更正**：补列会把存量行直接回填成 1，GORM 又按
// `default:1` 标签在客户端给新行填 1 ⇒ 仓内正常通路造不出 0；认 0 是防御，让缓存键不依赖
// "永远没人写出 0"这件事）。这些一律与升级前逐字节同键（T-P2-05 AC③）。别把它改成从
// "v0" 起步或去掉 <=1 的归并：那等于一次清空全量答案缓存。
func kbCachePromptVersion(version int) string {
	if version <= 1 {
		return faqPromptVersion
	}
	return "v" + strconv.Itoa(version)
}

// kbCanaryTargetVersion 灰度组落在哪个版本号上：稳定版本号的下一个。
//
// 下一个号而不是带后缀的临时号，是为了让"转正"退化成一次指针移动：灰度组已经焐热的
// 行正好就是新版稳定流量的行，不需要重灌（AC②）。
func kbCanaryTargetVersion(version int) int {
	if version < 1 {
		version = 1
	}
	return version + 1
}

// kbCanaryActive 行上的灰度是否成立：显式开启 + 百分比在 (0,100] + 有可分桶的 OneID。
//
// OneID 为空时**不放量**：否则所有匿名流量会塌成同一个桶（同一个哈希键），
// 要么全进灰度要么全不进，"百分比分流"当场失真。
func kbCanaryActive(kb *model.KnowledgeBase, oneID string) bool {
	if kb == nil || oneID == "" || kb.CanaryEnabled == nil || !*kb.CanaryEnabled {
		return false
	}
	return kb.CanaryPercent > 0 && kb.CanaryPercent <= 100
}

// kbInCanaryBucket 判定该 OneID 是否属于灰度组。
//
// 复用 flagBucketHash（FNV-1a + key 前缀，feature_flag.go 的按百分比放量口径），
// 与 script_ab.go 的 AssignBucket 同族；仓内已有第三套分桶（sop_abtest.go 加权变体），
// 本卡不新造第四套。同一 (kb, oneID) 恒定同桶 ⇒ 粘性。
func kbInCanaryBucket(kbID uint, oneID string, percent int) bool {
	// percent <= 0 必须显式挡住：负数转成 uint32 会变成 42 亿，`< uint32(percent)` 恒真，
	// "零放量"当场翻成"全量放量"。上界不必挡：hash%100 落在 [0,99]，percent>=100 天然全命中。
	if percent <= 0 {
		return false
	}
	return flagBucketHash(fmt.Sprintf("kb.%d", kbID), oneID)%100 < uint32(percent)
}

// KBAnswerVersionFor 本次会话该用哪个答案缓存命名空间。
//
// 三态语义（这是 AC③ 的全部内容，别改排序）：
//   - off（默认）/ kb 为 nil ⇒ 恒返回 faqPromptVersion，即挂载前的字面量 "v1"；
//   - shadow ⇒ 同样恒 "v1"，只把"本应落到哪个命名空间"打进日志。shadow 若放行稳定组
//     的新号，它就不是"什么都不改"了，观察期也就没有对照基线；
//   - on ⇒ 稳定组用 Version 号，灰度组用 Version+1 号。
//
// 返回值直接喂 ragcache.LookupRequest/StoreRequest 的 PromptVersion；读写的命名空间
// 必须同源（调用方每次入会话算一次并透传到 Store），否则中途改比例会把同一会话的
// 读键和写键劈成两个版本，读不到自己刚写的行。
func KBAnswerVersionFor(kb *model.KnowledgeBase, oneID string) string {
	mode := kbCanaryModeValue()
	if kb == nil || mode == kbCanaryOff {
		return faqPromptVersion
	}
	stable := kbCachePromptVersion(kb.Version)
	target := stable
	if kbCanaryActive(kb, oneID) && kbInCanaryBucket(kb.ID, oneID, kb.CanaryPercent) {
		target = kbCachePromptVersion(kbCanaryTargetVersion(kb.Version))
	}
	if mode == kbCanaryShadow {
		if target != faqPromptVersion {
			logger.Infof("[kb-canary] shadow: kb_id=%d one_id=%s 本应走 %s，实际仍用 %s（%s=%s）",
				kb.ID, oneID, target, faqPromptVersion, KBCanaryFlagEnv, mode)
		}
		return faqPromptVersion
	}
	if target != stable {
		logger.Infof("[kb-canary] 灰度放量: kb_id=%d one_id=%s ⇒ %s（稳定组 %s，percent=%d）",
			kb.ID, oneID, target, stable, kb.CanaryPercent)
	}
	return target
}

// PublishKBVersion 把灰度版本转正（target=0 即 Version+1），并顺手关掉灰度。
//
// 转正必须同时清灰度：不清的话，新稳定版本号 = 原灰度命名空间号，而灰度组的目标又算成
// "稳定号 +1"，两拨流量会去读写两个不同的新命名空间，读起来像"灰度没生效"。
// target > 0 是显式激活（含退回旧号 = 回滚），同样清灰度：灰度配置是一次性的放量动作，
// 换版本号之后原先的 (percent, 目标命名空间) 组合已经对不上了。
func (s *KnowledgeBaseService) PublishKBVersion(ctx context.Context, id uint, target int) (*model.KnowledgeBase, error) {
	if s.repo == nil {
		return nil, errors.New("repo not initialized")
	}
	kb, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if kb == nil {
		return nil, fmt.Errorf("知识库不存在: id=%d", id)
	}
	current := kb.Version
	if current < 1 {
		current = 1
	}
	next := current + 1
	switch {
	case target < 0:
		return nil, fmt.Errorf("version 不能为负，got %d", target)
	case target > 0:
		next = target
	}
	if err := s.repo.UpdateVersionCanary(ctx, id, next, false, 0); err != nil {
		return nil, err
	}
	kb.Version = next
	kb.CanaryEnabled = boolPtr(false)
	kb.CanaryPercent = 0
	logger.Infof("[kb-canary] 版本切换: kb_id=%d %s ⇒ %s（灰度已清零，updated_at 未 bump ⇒ 另一版本的缓存行保留）",
		id, kbCachePromptVersion(current), kbCachePromptVersion(next))
	return kb, nil
}

// SetKBCanary 配置灰度放量比例。percent=0 且 enabled=true 表示"配好但不放量"，
// 便于先把参数落库再逐步抬比例。
func (s *KnowledgeBaseService) SetKBCanary(ctx context.Context, id uint, enabled bool, percent int) (*model.KnowledgeBase, error) {
	if s.repo == nil {
		return nil, errors.New("repo not initialized")
	}
	if percent < 0 || percent > 100 {
		return nil, fmt.Errorf("canary_percent 必须在 [0,100] 区间内，got %d", percent)
	}
	kb, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if kb == nil {
		return nil, fmt.Errorf("知识库不存在: id=%d", id)
	}
	if err := s.repo.UpdateVersionCanary(ctx, id, kb.Version, enabled, percent); err != nil {
		return nil, err
	}
	kb.CanaryEnabled = boolPtr(enabled)
	kb.CanaryPercent = percent
	return kb, nil
}

// KBVersionInfo 版本与灰度现状 + 各命名空间行数（运营判断"灰度是否真的分流了、
// 两个版本的行是否都在"，也就是 AC① 的可见证据）。
func (s *KnowledgeBaseService) KBVersionInfo(ctx context.Context, id uint) (map[string]any, error) {
	if s.repo == nil {
		return nil, errors.New("repo not initialized")
	}
	kb, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if kb == nil {
		return nil, nil
	}
	rows, err := s.repo.AnswerCacheRowsByVersion(ctx, strconv.FormatUint(uint64(kb.ID), 10))
	if err != nil {
		return nil, err
	}
	canaryEnabled := kb.CanaryEnabled != nil && *kb.CanaryEnabled
	return map[string]any{
		"kb_id":             kb.ID,
		"kb_code":           kb.KBCode,
		"mode":              KBCanaryModeForLog(),
		"version":           kb.Version,
		"stable_namespace":  kbCachePromptVersion(kb.Version),
		"canary_namespace":  kbCachePromptVersion(kbCanaryTargetVersion(kb.Version)),
		"canary_enabled":    canaryEnabled,
		"canary_percent":    kb.CanaryPercent,
		"rows_by_namespace": rows,
	}, nil
}
