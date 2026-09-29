package llm

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"hivemtk-user/internal/pkg/featureflag"
	"hivemtk-user/internal/pkg/utils/logger"
)

// FallbackLevel 降级级别
type FallbackLevel int

const (
	LevelPrimary FallbackLevel = iota + 1
	LevelSecondary
	LevelCache
	LevelTemplate
)

// String 降级级别名称
func (l FallbackLevel) String() string {
	switch l {
	case LevelPrimary:
		return "primary_7b"
	case LevelSecondary:
		return "secondary_3b"
	case LevelCache:
		return "cache"
	case LevelTemplate:
		return "template"
	}
	return "unknown"
}

// FallbackDecision 降级决策
type FallbackDecision struct {
	Level       FallbackLevel
	FromLevel   FallbackLevel
	Reason      string
	Provider    string
	CacheKey    string
	TemplateKey string
	CanRetry    bool
}

// DecisionTree 智能降级决策树
//
// 配置示例:
//
//	tree := NewDecisionTree(DecisionTreeConfig{
//	    PrimaryProvider:   "local-llama-7b-q5",
//	    SecondaryProvider: "local-llama-3b-q4",
//	    CacheEnabled:      true,
//	    TemplateEnabled:   true,
//	    TemplateFallback:  "抱歉, 服务暂时不可用, 请稍后重试。",
//	})
type DecisionTree struct {
	primaryProvider   string
	secondaryProvider string
	cacheEnabled      bool
	templateEnabled   bool
	templateFallback  string
}

// DecisionTreeConfig 决策树配置
type DecisionTreeConfig struct {
	PrimaryProvider   string
	SecondaryProvider string
	CacheEnabled      bool
	TemplateEnabled   bool
	TemplateFallback  string
}

// NewDecisionTree 构造决策树
func NewDecisionTree(cfg DecisionTreeConfig) *DecisionTree {
	return &DecisionTree{
		primaryProvider:   cfg.PrimaryProvider,
		secondaryProvider: cfg.SecondaryProvider,
		cacheEnabled:      cfg.CacheEnabled,
		templateEnabled:   cfg.TemplateEnabled,
		templateFallback:  cfg.TemplateFallback,
	}
}

// Decide 决策: 在某一级别失败时, 决定下一级别
//
// 参数:
//   - currentLevel 当前失败的级别
//   - reason 失败原因
//   - cacheKey 缓存键 (供 LevelCache 查询)
//   - hasCacheHit 缓存是否命中
//
// 返回: FallbackDecision
func (t *DecisionTree) Decide(
	ctx context.Context,
	currentLevel FallbackLevel,
	reason string,
	cacheKey string,
	hasCacheHit bool,
) *FallbackDecision {
	if !featureflag.Get("fallback_chain").Bool() {
		if currentLevel != LevelTemplate {
			logger.Warnf("[FallbackTree] fallback_chain disabled, jumping to template from level=%s reason=%s",
				currentLevel.String(), reason)
		}
		return &FallbackDecision{
			Level:     LevelTemplate,
			FromLevel: currentLevel,
			Reason:    reason,
			CanRetry:  true,
		}
	}

	switch currentLevel {
	case LevelPrimary:
		return &FallbackDecision{
			Level:     LevelSecondary,
			FromLevel: LevelPrimary,
			Reason:    reason,
			Provider:  t.secondaryProvider,
			CanRetry:  true,
		}
	case LevelSecondary:
		if t.cacheEnabled && hasCacheHit {
			return &FallbackDecision{
				Level:     LevelCache,
				FromLevel: LevelSecondary,
				Reason:    reason,
				CacheKey:  cacheKey,
				CanRetry:  true,
			}
		}
		if t.templateEnabled {
			return &FallbackDecision{
				Level:     LevelTemplate,
				FromLevel: LevelSecondary,
				Reason:    reason,
				CanRetry:  true,
			}
		}
		return &FallbackDecision{
			Level:     LevelTemplate,
			FromLevel: LevelSecondary,
			Reason:    "no_template_configured",
			CanRetry:  true,
		}
	case LevelCache:
		if t.templateEnabled {
			return &FallbackDecision{
				Level:     LevelTemplate,
				FromLevel: LevelCache,
				Reason:    reason,
				CanRetry:  true,
			}
		}
		return &FallbackDecision{
			Level:     LevelTemplate,
			FromLevel: LevelCache,
			Reason:    "no_template_configured",
			CanRetry:  true,
		}
	case LevelTemplate:
		return &FallbackDecision{
			Level:     LevelTemplate,
			FromLevel: LevelTemplate,
			Reason:    "end_of_chain",
			CanRetry:  false,
		}
	}
	return &FallbackDecision{
		Level:    LevelTemplate,
		Reason:   fmt.Sprintf("unknown_level_%d", currentLevel),
		CanRetry: false,
	}
}

func isPermanentLLMError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	permanentMarkers := []string{
		"content_policy", "content policy", "policy violation",
		"content_filter", "content filter", "moderation",
		"context_length_exceeded", "context length", "maximum context",
		"invalid_request",
	}
	for _, m := range permanentMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// 返回: 4 级降级链中第一个成功的结果
func (t *DecisionTree) ExecuteWithFallback(
	ctx context.Context,
	prompt string,
	callProvider func(ctx context.Context, provider string) (string, error),
	queryCache func(ctx context.Context, key string) (string, bool),
	levelTimeoutMs int,
) (string, FallbackLevel, error) {
	if levelTimeoutMs <= 0 {
		levelTimeoutMs = 60000
	}

	levelStart := time.Now()
	cctx, cancel := context.WithTimeout(ctx, time.Duration(levelTimeoutMs)*time.Millisecond)
	defer cancel()
	content, err := callProvider(cctx, t.primaryProvider)
	if err == nil {
		logger.Infof("[FallbackTree] level=%s ok, latency=%dms",
			LevelPrimary.String(), time.Since(levelStart).Milliseconds())
		return content, LevelPrimary, nil
	}
	logger.Warnf("[FallbackTree] level=%s failed: %v (latency=%dms)",
		LevelPrimary.String(), err, time.Since(levelStart).Milliseconds())

	if isPermanentLLMError(err) {
		logger.Warnf("[FallbackTree] permanent error detected, skip secondary escalation: %v", err)
	} else {
		levelStart = time.Now()
		cctx2, cancel2 := context.WithTimeout(ctx, time.Duration(levelTimeoutMs)*time.Millisecond)
		defer cancel2()
		content, err = callProvider(cctx2, t.secondaryProvider)
		if err == nil {
			logger.Infof("[FallbackTree] level=%s ok, latency=%dms",
				LevelSecondary.String(), time.Since(levelStart).Milliseconds())
			return content, LevelSecondary, nil
		}
		logger.Warnf("[FallbackTree] level=%s failed: %v (latency=%dms)",
			LevelSecondary.String(), err, time.Since(levelStart).Milliseconds())
	}

	if t.cacheEnabled && queryCache != nil {
		levelStart = time.Now()

		hash := sha256.Sum256([]byte(prompt))
		prefix := prompt
		if len(prefix) > 32 {
			prefix = prefix[:32]
		}
		cacheKey := fmt.Sprintf("llm_fallback:%s:%x:%s", t.primaryProvider, hash, prefix)
		if cached, found := queryCache(ctx, cacheKey); found {
			logger.Infof("[FallbackTree] level=%s hit, latency=%dms",
				LevelCache.String(), time.Since(levelStart).Milliseconds())
			return cached, LevelCache, nil
		}
		logger.Warnf("[FallbackTree] level=%s miss, latency=%dms",
			LevelCache.String(), time.Since(levelStart).Milliseconds())
	}

	if t.templateEnabled && t.templateFallback != "" {
		logger.Infof("[FallbackTree] level=%s used", LevelTemplate.String())
		return t.templateFallback, LevelTemplate, nil
	}

	return "", LevelTemplate, fmt.Errorf("fallback chain exhausted: 7B/3B/cache all failed")
}

// scenarioFallbackTemplates 按场景轮换的降级文案池，客户可见；
// 「…」暗号同 defaultFallbackTemplates 的约束（须是 service 层转人工关键词表认识的词）。
var scenarioFallbackTemplates = map[DispatchScenario][]string{

	ScenarioIntentRecognize: {
		`{"major":"unknown","confidence":0,"reasoning":"intent service degraded"}`,
	},

	ScenarioSOPReply: {
		"您好，系统正在为您处理该业务，请稍候；如需加急可回复「转人工」。",
		"您的请求已记录，我们会尽快为您办理，感谢耐心等待。",
	},

	ScenarioObjection: {
		"理解您的顾虑，我先把您的问题记下来，稍后由同事为您详细解答好吗？",
		"非常抱歉给您带来困扰，您的反馈我们已收到，会安排专人与您跟进。",
	},

	ScenarioFriendlyChat: {
		"哈哈，这边网络有点小状况，稍等一下下～",
		"让我缓一缓，马上回来继续和您聊！",
	},
}

// defaultFallbackTemplates 未命中场景表时的通用降级文案池。
// 池里出现的「…」暗号必须是 service 层转人工关键词表真认识的词（现为「转人工」），
// 否则客户照文案回复也触发不了 veto，兜底回复就成了死路；两侧一致性由
// service 包的 TestFallbackCopy_HandoffKeywordIsMatchable 跨包守住。
var defaultFallbackTemplates = []string{
	"抱歉，当前客服系统繁忙，请稍后再试，或联系人工客服获取帮助。",
	"系统暂时有点忙，请稍后重试；紧急问题可直接回复「转人工」。",
}

var scenarioTemplateCursor uint64

var knownFactoryDefaultTemplateReplies = map[string]struct{}{
	"抱歉，当前客服系统繁忙，请稍后再试，或联系人工客服获取帮助。": {},
	"抱歉，当前服务暂时繁忙，请稍后再试或联系人工客服。":      {},
}

func isDefaultTemplateReply(s string) bool {
	_, ok := knownFactoryDefaultTemplateReplies[s]
	return ok
}

// ResolveDegradedTemplate 解析降级文案：显式定制的 TemplateReply 优先，否则按场景轮换（M11）
func ResolveDegradedTemplate(scenario DispatchScenario, configured string) string {
	if configured != "" && !isDefaultTemplateReply(configured) {
		return configured
	}
	return TemplateReplyFor(scenario)
}

// TemplateReplyFor 返回指定场景的兜底文案（M11）
//
// 匹配规则：
//   - 命中场景模板表 → 同场景文案池内原子轮换
//   - 未命中（含未知场景）→ 通用文案池轮换
func TemplateReplyFor(scenario DispatchScenario) string {
	pool := scenarioFallbackTemplates[scenario]
	if len(pool) == 0 {
		pool = defaultFallbackTemplates
	}
	n := atomic.AddUint64(&scenarioTemplateCursor, 1)
	return pool[(n-1)%uint64(len(pool))]
}
