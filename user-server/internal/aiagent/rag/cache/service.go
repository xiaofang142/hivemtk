package ragcache

import (
	"context"
	"regexp"
	"strings"
	"time"

	"hivemtk-user/internal/pkg/utils/logger"
)

type FAQAnswerCacheService struct {
	store     Store
	kbMeta    KBMetaReader
	threshold float64
	maxAge    time.Duration
	now       func() time.Time
}

// NewFAQAnswerCacheService 创建语义缓存服务
//
// threshold 传 0 时使用 DefaultSemanticThreshold (0.95)；
// 按 RT-2 契约只允许调紧（更接近 1），传大于 0.95 的值合法。
// maxAge 是条目的最长可用时间，命中前超过它就删掉该行并按未命中处理；
// 传 0 表示不按时间过期（只靠知识库 updated_at 失效）。
func NewFAQAnswerCacheService(store Store, kbMeta KBMetaReader, threshold float64, maxAge time.Duration) *FAQAnswerCacheService {
	if threshold <= 0 {
		threshold = DefaultSemanticThreshold
	}
	if threshold < DefaultSemanticThreshold {
		threshold = DefaultSemanticThreshold
	}
	return &FAQAnswerCacheService{
		store:     store,
		kbMeta:    kbMeta,
		threshold: threshold,
		maxAge:    maxAge,
		now:       time.Now,
	}
}

// SetNowFunc 注入时钟（仅测试使用）
func (s *FAQAnswerCacheService) SetNowFunc(f func() time.Time) { s.now = f }

// Lookup 三层查询：Tier1 精确 → Tier2 语义 → Miss 回源。
// 任一层命中前都先做 kb 更新时间失效校验。
func (s *FAQAnswerCacheService) Lookup(ctx context.Context, req LookupRequest) (*LookupResult, error) {
	if s == nil || s.store == nil || len(req.QueryVector) == 0 || req.KBID == "" {
		return &LookupResult{Tier: TierMiss}, nil
	}

	if e, err := s.store.GetExact(ctx, req.KBID, req.PromptVersion, req.QueryVector); err != nil {
		logger.Warnf("[ragcache] tier1 exact lookup failed: %v", err)
		ragRecallTotal.WithLabel("tier1", "false").Inc()
	} else if e != nil && s.fresh(ctx, e) {
		ragRecallTotal.WithLabel("tier1", "true").Inc()
		// Tier1 的判据就是向量逐位相等，相似度按定义是 1。留 0 的话，
		// 拿 Similarity 做置信度判断的调用方会把精确命中一律当成"最不可信的一条"丢掉
		// （编排器就是这么用的，接线时实测到这条分支从来没有被采用过）。
		return &LookupResult{Tier: TierExact, Answer: e.Answer, Similarity: 1}, nil
	} else {
		ragRecallTotal.WithLabel("tier1", "false").Inc()
	}

	e, err := s.store.GetSemantic(ctx, req.KBID, req.PromptVersion, req.QueryVector, s.threshold)
	if err != nil {
		logger.Warnf("[ragcache] tier2 semantic lookup failed: %v", err)
		ragRecallTotal.WithLabel("tier2", "false").Inc()
		ragRecallTotal.WithLabel("miss", "true").Inc()
		return &LookupResult{Tier: TierMiss}, nil
	}
	if e == nil || !s.fresh(ctx, e) {
		ragRecallTotal.WithLabel("tier2", "false").Inc()
		ragRecallTotal.WithLabel("miss", "true").Inc()
		return &LookupResult{Tier: TierMiss}, nil
	}

	sim := CosineSimilarity(req.QueryVector, e.QueryVector)
	if sim < float64(s.threshold) {
		logger.Debugf("[ragcache] semantic hit below local threshold: sim=%.6f threshold=%.2f", sim, s.threshold)
		ragRecallTotal.WithLabel("tier2", "false").Inc()
		ragRecallTotal.WithLabel("miss", "true").Inc()
		return &LookupResult{Tier: TierMiss}, nil
	}
	ragRecallTotal.WithLabel("tier2", "true").Inc()
	return &LookupResult{Tier: TierSemantic, Answer: e.Answer, Similarity: sim}, nil
}

// StoreRequest 缓存写入请求
type StoreRequest struct {
	KBID              string
	PromptVersion     string
	QueryVector       []float32
	Answer            string
	FromKnowledgeBase bool
}

// Store 写入缓存（过 CanCache 四道门 + 记录写入时的 kb 更新时间）。
// 任一门未通过则静默跳过（返回 nil，不视为错误——不入缓存是正常业务分支）。
func (s *FAQAnswerCacheService) Store(ctx context.Context, req StoreRequest) error {
	if s == nil || s.store == nil {
		return nil
	}
	if ok, reason := s.CanCache(req.Answer, req.FromKnowledgeBase); !ok {
		logger.Debugf("[ragcache] skip caching (%s)", reason)
		return nil
	}
	kbUpdatedAt, err := s.kbMeta.GetKBUpdatedAt(ctx, req.KBID)
	if err != nil {

		logger.Warnf("[ragcache] read kb updated_at failed, skip caching: %v", err)
		return nil
	}
	return s.store.Put(ctx, &Entry{
		KBID:          req.KBID,
		PromptVersion: req.PromptVersion,
		QueryVector:   req.QueryVector,
		Answer:        req.Answer,
		CreatedAt:     s.now(),
		KBUpdatedAt:   kbUpdatedAt,
	})
}

// CanCache 入缓存资格判定。返回 (是否可缓存, 原因)。
func (s *FAQAnswerCacheService) CanCache(answer string, fromKnowledgeBase bool) (bool, string) {
	if !fromKnowledgeBase {
		return false, "not_from_kb"
	}
	if strings.TrimSpace(answer) == "" {
		return false, "empty_answer"
	}
	if IsRefusalAnswer(answer) {
		return false, "refusal_answer"
	}
	if ContainsPersonalizationVar(answer) {
		return false, "personalized_answer"
	}
	return true, ""
}

// fresh 命中资格校验：先看条目有没有过期，再看知识库有没有在它之后被改过。
// 两种都算"这行不能再用了"，所以都物理删除，而不是每次都重新判一遍。
func (s *FAQAnswerCacheService) fresh(ctx context.Context, e *Entry) bool {
	if s.maxAge > 0 && s.now().Sub(e.CreatedAt) > s.maxAge {
		age := s.now().Sub(e.CreatedAt)
		logger.Debugf("[ragcache] entry expired, drop it: id=%d kb_id=%s age=%s max_age=%s", e.ID, e.KBID, age, s.maxAge)
		s.drop(ctx, e)
		return false
	}
	if s.kbMeta == nil {
		return true
	}
	cur, err := s.kbMeta.GetKBUpdatedAt(ctx, e.KBID)
	if err != nil {

		logger.Warnf("[ragcache] freshness check failed (kb_id=%s): %v", e.KBID, err)
		return true
	}
	if cur.After(e.KBUpdatedAt) {
		logger.Infof("[ragcache] kb updated since cached, invalidate entry id=%d kb_id=%s", e.ID, e.KBID)
		s.drop(ctx, e)
		return false
	}
	return true
}

func (s *FAQAnswerCacheService) drop(ctx context.Context, e *Entry) {
	if err := s.store.Delete(ctx, e.ID); err != nil {
		logger.Warnf("[ragcache] delete stale entry failed: %v", err)
	}
}

var refusalKeywords = []string{
	"知识不足",
	"无法回答",
	"无法回复",
	"暂时无法",
	"我不知道",
	"转人工",
	"没有找到相关",
	"未找到相关",
	"超出我的知识",
}

// IsRefusalAnswer 判断答案是否为 refusal/兜底话术
func IsRefusalAnswer(answer string) bool {
	a := strings.TrimSpace(answer)
	if a == "" {
		return true
	}
	for _, kw := range refusalKeywords {
		if strings.Contains(a, kw) {
			return true
		}
	}
	return false
}

var (
	personalizationVarPattern1 = regexp.MustCompile(`\{\{\s*\.?[\w.-]+\s*\}\}`)
	personalizationVarPattern2 = regexp.MustCompile(`\{[\w][\w.-]*\}`)
)

// ContainsPersonalizationVar 判断答案是否含客户个性化变量占位符
func ContainsPersonalizationVar(answer string) bool {
	return personalizationVarPattern1.MatchString(answer) ||
		personalizationVarPattern2.MatchString(answer)
}
