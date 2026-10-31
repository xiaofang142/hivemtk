package repository

import (
	"context"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

type FAQRepository struct {
	db *gorm.DB
}

// NewFAQRepositoryDefault 默认构造（全局句柄；未装配时返回 nil，调用方按「repo 未装配」处理）。
func NewFAQRepositoryDefault() *FAQRepository {
	if db := GetDB(); db != nil {
		return NewFAQRepository(db)
	}
	return nil
}

func NewFAQRepository(db *gorm.DB) *FAQRepository {
	return &FAQRepository{db: db}
}

// Create 强制全字段 INSERT：GORM v2 对 bool 零值 (false) 不写入、会落到 column
// default，Select("*") 确保 false 也能正确落库。
func (r *FAQRepository) Create(ctx context.Context, entry *model.FAQEntry) error {
	return r.db.WithContext(ctx).Select("*").Create(entry).Error
}

// GetByID 按 ID 查询
func (r *FAQRepository) GetByID(ctx context.Context, id uint) (*model.FAQEntry, error) {
	var entry model.FAQEntry
	if err := r.db.WithContext(ctx).First(&entry, id).Error; err != nil {
		return nil, err
	}
	return &entry, nil
}

// ListEnabled 查询所有启用的 FAQ
// CountAll 统计 FAQ 总数（含停用）。统计口径不能用 ListEnabled：它把 limit 夹到
// 10000，库里的条目超过上限时会把总数报成截断值。
func (r *FAQRepository) CountAll(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.FAQEntry{}).Count(&n).Error
	return n, err
}

// CountEnabled 统计启用中的 FAQ 数。
func (r *FAQRepository) CountEnabled(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.FAQEntry{}).Where("enabled = ?", true).Count(&n).Error
	return n, err
}

func (r *FAQRepository) ListEnabled(ctx context.Context, limit int) ([]model.FAQEntry, error) {
	if limit <= 0 || limit > 10000 {
		limit = 10000
	}
	var entries []model.FAQEntry
	err := r.db.WithContext(ctx).
		Where("enabled = ?", true).
		Order("hit_count DESC, confidence DESC, id ASC").
		Limit(limit).
		Find(&entries).Error
	return entries, err
}

// MatchByKeyword 关键词匹配，兼容旧签名（agentID=0 表示共享/全局）。
func (r *FAQRepository) MatchByKeyword(ctx context.Context, msg string, topK int) ([]model.FAQEntry, error) {
	return r.MatchByKeywordForAgent(ctx, msg, 0, topK)
}

// MatchByKeywordForAgent 按智能体隔离的关键词匹配：
// agentID = 0 全局共享（向后兼容）；agentID > 0 匹配 (agent_id = agentID OR agent_id IS NULL) AND enabled
func (r *FAQRepository) MatchByKeywordForAgent(ctx context.Context, msg string, agentID uint, topK int) ([]model.FAQEntry, error) {
	if msg == "" {
		return nil, nil
	}
	all, err := r.listEnabledForAgent(ctx, agentID, 5000)
	if err != nil {
		return nil, err
	}
	return r.scoreAndRank(all, msg, topK)
}

// ListCandidates 返回指定 agent 的 FAQ 候选集（已启用），供 Service 层命中内存
// 缓存后复用，避免每次匹配都全量拉取再打分。语义与 listEnabledForAgent 一致：
// agentID == 0 全部已启用；agentID > 0 该 agent 私有 + 共享。
func (r *FAQRepository) ListCandidates(ctx context.Context, agentID uint, limit int) ([]model.FAQEntry, error) {
	return r.listEnabledForAgent(ctx, agentID, limit)
}

// ScoreCandidates 对已加载的 FAQ 候选集做内存打分排序（不触发 DB 查询）。
// 供 Service 层命中内存缓存后复用候选集、仅按不同 query 重新打分。
func (r *FAQRepository) ScoreCandidates(ctx context.Context, entries []model.FAQEntry, msg string, topK int) ([]model.FAQEntry, error) {
	return r.scoreAndRank(entries, msg, topK)
}

func (r *FAQRepository) listEnabledForAgent(ctx context.Context, agentID uint, limit int) ([]model.FAQEntry, error) {
	if limit <= 0 || limit > 10000 {
		limit = 10000
	}
	var entries []model.FAQEntry
	q := r.db.WithContext(ctx).
		Where("enabled = ?", true).
		Order("hit_count DESC, confidence DESC, id ASC").
		Limit(limit)
	if agentID > 0 {
		q = q.Where("agent_id = ? OR agent_id IS NULL", agentID)
	}
	if err := q.Find(&entries).Error; err != nil {
		return nil, err
	}
	return entries, nil
}

// ListShared 列出全部共享 FAQ (agent_id IS NULL)
func (r *FAQRepository) ListShared(ctx context.Context, limit int) ([]model.FAQEntry, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var entries []model.FAQEntry
	err := r.db.WithContext(ctx).
		Where("agent_id IS NULL AND enabled = ?", true).
		Order("id DESC").
		Limit(limit).
		Find(&entries).Error
	return entries, err
}

// MatchByAgent 按智能体 ID 匹配：agentID == 0 返回 (nil, nil)；
// 仅匹配 enabled = true AND agent_id = ?（走 idx_faq_agent_id 索引）。
// 任何 agent 都必须显式绑定才能匹配，没有「空数组=全局」分支。
func (r *FAQRepository) MatchByAgent(ctx context.Context, agentID uint, msg string, topK int) ([]model.FAQEntry, error) {
	if agentID == 0 || msg == "" {
		return nil, nil
	}
	var entries []model.FAQEntry
	err := r.db.WithContext(ctx).
		Where("enabled = ? AND agent_id = ?", true, agentID).
		Order("hit_count DESC, confidence DESC, id ASC").
		Limit(5000).
		Find(&entries).Error
	if err != nil {
		return nil, err
	}
	return r.scoreAndRank(entries, msg, topK)
}

// ListByAgent 列出某智能体的 FAQ 集合 (不参与打分, 仅做缓存预热 / 后台同步)
func (r *FAQRepository) ListByAgent(ctx context.Context, agentID uint, limit int) ([]model.FAQEntry, error) {
	if agentID == 0 {
		return nil, nil
	}
	if limit <= 0 || limit > 10000 {
		limit = 5000
	}
	var entries []model.FAQEntry
	err := r.db.WithContext(ctx).
		Where("enabled = ? AND agent_id = ?", true, agentID).
		Order("id ASC").
		Limit(limit).
		Find(&entries).Error
	return entries, err
}

// ScoreFAQCandidates 对已加载的 FAQ 候选集打分排序。导出是为了让 service 层
// 测试复用生产同一份打分口径。刻意不收 ctx：打分是纯内存计算，不查库，
// 不给调用方留「顺手传入无租户 ctx 的查询」的入口。
func ScoreFAQCandidates(r *FAQRepository, entries []model.FAQEntry, msg string, topK int) ([]model.FAQEntry, error) {
	return r.scoreAndRank(entries, msg, topK)
}

func (r *FAQRepository) scoreAndRank(all []model.FAQEntry, msg string, topK int) ([]model.FAQEntry, error) {
	scored, err := r.scoreAndRanked(all, msg, topK)
	if err != nil {
		return nil, err
	}
	out := make([]model.FAQEntry, 0, len(scored))
	for _, s := range scored {
		e := s.Entry
		e.Confidence = s.Score
		out = append(out, e)
	}
	return out, nil
}

// FAQScoredEntry 打分结果带显式匹配方式：泛词兜底命中不得作为高置信引用展示。
type FAQScoredEntry struct {
	Entry     model.FAQEntry
	MatchType string  // model.FAQMatchExact / FAQMatchToken / FAQMatchKeywordFallback
	Score     float64 // 排序分（与 Confidence 透传值同源）
}

// ScoreCandidatesScored ScoreCandidates 的带 MatchType 变体（引用链路专用）。
func (r *FAQRepository) ScoreCandidatesScored(ctx context.Context, entries []model.FAQEntry, msg string, topK int) ([]FAQScoredEntry, error) {
	return r.scoreAndRanked(entries, msg, topK)
}

func (r *FAQRepository) scoreAndRanked(all []model.FAQEntry, msg string, topK int) ([]FAQScoredEntry, error) {
	msgTokens := tokenize(msg)
	if len(msgTokens) == 0 {
		return nil, nil
	}
	type scored struct {
		entry     model.FAQEntry
		score     float64
		level     float64
		matchType string
	}
	var results []scored
	for i := range all {
		e := all[i]
		if e.Enabled == nil || !*e.Enabled {
			continue
		}
		faqTokens := tokenize(e.Question)
		// 匹配分 = 关键词命中强度（主导）+ 问法相似度（只做同分决胜）。
		// 关键词每条独立加分会让「关键词多但问法不匹配」的 FAQ 靠堆砌压过
		// 问法更贴近的条目，答非所问比匹配不上更糟。
		// 分层而不是加权混合：一层精确命中（关键词是用户句的连续子串，强证据）、
		// 二层词元命中（共用词元，仅兜底）。中文二元组太容易巧合，词元命中数一多
		// 就会压过精确命中，混在一张分数上两套机制永远在打架。
		exactHits := faqExactKeywordHits(msg, e.Keywords)
		var level, strength float64
		if exactHits > 0 {
			level = faqMatchLevelExact
			strength = float64(exactHits)
		} else {
			tokenHits := faqTokenHits(msg, e.Keywords)
			if tokenHits > 0 {
				level = faqMatchLevelToken
				strength = float64(tokenHits)
			} else if len(e.Keywords) == 0 {
				// 这条 FAQ 没有配关键词 ⇒ 只能退回问法本身的词元重合
				//（语序颠倒时 jaccard 趋近 0，词元重合是唯一能识别
				// 「讲的是同一件事」的信号）。有关键词的条目不走这条路，
				// 避免中文二元组巧合造成答非所问。
				if overlap := faqTokenOverlapCount(msgTokens, faqTokens); overlap > 0 {
					level = faqMatchLevelToken
					strength = float64(overlap)
				}
			}
		}
		similarity := jaccardSimilarity(msgTokens, faqTokens)
		// 分数只反映"这条有多像用户问的那条"（0~1 量级，可与 0.6 门槛比）；
		// 层号只作排序键，不进分数 —— 进了会把分数放大到门槛失去意义。
		score := faqStrengthWeight*strength + faqSimilarityTieBreak*similarity
		score = score * (e.Confidence + 0.1*logPlus(e.HitCount))
		if level == 0 {
			// 精确命中与词元命中都没有 ⇒ 这条与用户问法无关。
			continue
		}
		// 匹配方式显式化：精确层=exact；词元层里配了关键词且共用词元的按 token；
		// 无关键词条目的问法词元重合视为泛化兜底（keyword_fallback），引用侧按分数过滤。
		matchType := model.FAQMatchExact
		if level == faqMatchLevelToken {
			matchType = model.FAQMatchToken
			if len(e.Keywords) == 0 {
				matchType = model.FAQMatchKeywordFallback
			}
		}
		results = append(results, scored{entry: e, score: score, level: level, matchType: matchType})
	}
	// 层是主键：先按层排，层内按强度排。
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].level != results[j].level {
			return results[i].level > results[j].level
		}
		return results[i].score > results[j].score
	})
	if topK <= 0 || topK > len(results) {
		topK = len(results)
	}
	out := make([]FAQScoredEntry, 0, topK)
	for i := 0; i < topK; i++ {
		out = append(out, FAQScoredEntry{Entry: results[i].entry, MatchType: results[i].matchType, Score: results[i].score})
	}
	return out, nil
}

// ListWithFilter 分页+过滤 (前端 FAQ 管理页面使用)
func (r *FAQRepository) ListWithFilter(ctx context.Context, filter FAQListParams) ([]model.FAQEntry, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.FAQEntry{})
	if filter.Keyword != "" {
		kw := "%" + filter.Keyword + "%"
		q = q.Where("question LIKE ? OR answer LIKE ?", kw, kw)
	}
	if filter.Category != "" {
		q = q.Where("category = ?", filter.Category)
	}
	if filter.Intent != "" {
		q = q.Where("intent = ?", filter.Intent)
	}
	if filter.Enabled != nil {
		q = q.Where("enabled = ?", *filter.Enabled)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var entries []model.FAQEntry
	offset := (filter.Page - 1) * filter.PageSize
	if offset < 0 {
		offset = 0
	}
	err := q.Order("hit_count DESC, confidence DESC, id ASC").
		Offset(offset).Limit(filter.PageSize).
		Find(&entries).Error
	return entries, total, err
}

// Update 更新 FAQ
func (r *FAQRepository) Update(ctx context.Context, id uint, entry *model.FAQEntry) error {
	// 只写「编辑表单真正拥有的列」：
	//   - `Select("*").Updates(entry)` 会把零值 tenant_id 一起写回，SaaS 下被隔离
	//     callback 判成「改写 tenant_id」直接 500；off 模式下同一条语句也会把
	//     req.Enabled 为 nil 的 NULL 写进 not-null 的 enabled 列。
	//   - 缺省字段（enabled/agent_id）保持原值，符合「编辑不传即不改」的直觉。
	updates := map[string]any{
		"question":   entry.Question,
		"answer":     entry.Answer,
		"keywords":   entry.Keywords,
		"category":   entry.Category,
		"intent":     entry.Intent,
		"confidence": entry.Confidence,
	}
	if entry.AgentID != nil {
		updates["agent_id"] = *entry.AgentID
	}
	if entry.Enabled != nil {
		updates["enabled"] = *entry.Enabled
	}
	return r.db.WithContext(ctx).Model(&model.FAQEntry{}).
		Where("id = ?", id).
		Updates(updates).Error
}

// SetEnabled 仅切换启用状态（列表页开关专用，避免全量 Update 的 required 校验）
func (r *FAQRepository) SetEnabled(ctx context.Context, id uint, enabled bool) error {
	return r.db.WithContext(ctx).Model(&model.FAQEntry{}).
		Where("id = ?", id).
		Update("enabled", enabled).Error
}

// Delete 删除 FAQ
func (r *FAQRepository) Delete(ctx context.Context, id uint) error {
	result := r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.FAQEntry{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		// 0 行 = 目标不存在；回 not found 让上层回 404，避免「删除成功」掩盖误操作。
		return gorm.ErrRecordNotFound
	}
	return nil
}

type FAQListParams struct {
	Keyword  string
	Category string
	Intent   string
	Enabled  *bool
	Page     int
	PageSize int
}

// IncrementHitCount 命中次数 +1
func (r *FAQRepository) IncrementHitCount(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).
		Model(&model.FAQEntry{}).
		Where("id = ?", id).
		UpdateColumns(map[string]any{
			"hit_count":   gorm.Expr("hit_count + 1"),
			"last_hit_at": gorm.Expr("NOW()"),
		}).
		Error
}

// DecayQuality 衰减指定 FAQ 的 QualityScore。
// decay 范围 [0, 1]，调用方负责边界（repo 不做下限保护以保持纯函数语义）。
func (r *FAQRepository) DecayQuality(ctx context.Context, id uint, decay float64) error {
	if id == 0 || decay <= 0 {
		return nil
	}
	return r.db.WithContext(ctx).
		Model(&model.FAQEntry{}).
		Where("id = ?", id).
		UpdateColumn("quality_score", gorm.Expr("GREATEST(quality_score - ?, 0)", decay)).
		Error
}

// ListDecayCandidates 查询符合衰减条件的 FAQ：
// hit_count < 5 且 last_hit_at 早于 cutoff（cutoff 建议为 now-7d，调用方注入）。
func (r *FAQRepository) ListDecayCandidates(ctx context.Context, cutoff time.Time, limit int) ([]model.FAQEntry, error) {
	if limit <= 0 || limit > 10000 {
		limit = 10000
	}
	var entries []model.FAQEntry
	err := r.db.WithContext(ctx).
		Where("hit_count < ? AND last_hit_at IS NOT NULL AND last_hit_at < ?", 5, cutoff).
		Order("last_hit_at ASC").
		Limit(limit).
		Find(&entries).Error
	return entries, err
}

// IncrementNegativeHit 用户负反馈 +1
func (r *FAQRepository) IncrementNegativeHit(ctx context.Context, id uint) error {
	if id == 0 {
		return nil
	}
	return r.db.WithContext(ctx).
		Model(&model.FAQEntry{}).
		Where("id = ?", id).
		UpdateColumn("negative_hit_count", gorm.Expr("negative_hit_count + 1")).
		Error
}

func tokenize(s string) []string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return nil
	}
	runes := []rune(s)
	tokens := make([]string, 0, len(runes))
	if !hasChinese(s) {
		for _, w := range strings.Fields(s) {
			if utf8.RuneCountInString(w) >= 2 {
				tokens = append(tokens, w)
			}
		}
		return tokens
	}
	// 英文/数字片段整体成词，不切碎片：只把 CJK 二元组入词的话，夹在中文里的
	// 英文会被切成 2 字碎片（"Telegram" → le/te/gr/am…），碎片乱撞造成答非所问。
	wordBuf := make([]rune, 0, len(runes))
	flushWord := func() {
		if len(wordBuf) >= 2 {
			// 分隔用的空格/标点不进词元。
			word := strings.TrimSpace(strings.ToLower(string(wordBuf)))
			if len(word) >= 2 {
				tokens = append(tokens, word)
			}
		}
		wordBuf = wordBuf[:0]
	}
	for _, r := range runes {
		if isCJK(r) {
			flushWord()
			continue
		}
		wordBuf = append(wordBuf, r)
	}
	flushWord()
	for i := 0; i < len(runes)-1; i++ {
		if isCJK(runes[i]) && isCJK(runes[i+1]) {
			tokens = append(tokens, string(runes[i:i+2]))
		}
	}
	for _, r := range runes {
		if isCJK(r) {
			tokens = append(tokens, string(r))
		}
	}
	return tokens
}

// FAQ 关键词命中的加分。关键词是人工标注的强证据，比 jaccard 字面相似度可靠。
const (
	// faqMatchLevelExact 精确命中层的层号（排序主键，比词元层高）。
	faqMatchLevelExact = 2.0
	// faqMatchLevelToken 词元命中层的层号（兜底）。
	faqMatchLevelToken = 1.0
	// faqStrengthWeight 层内强度（命中个数）的权重。
	faqStrengthWeight = 1.0
	// faqSimilarityTieBreak 问法相似度权重，只做同分决胜：
	// 小于一次精确命中（1.0），相似度高不能翻盘关键词命中数不同的两条。
	faqSimilarityTieBreak = 0.3
)

// faqExactKeywordHits 统计用户问法精确命中（连续子串）的关键词个数。
// 只认精确命中：中文二元组太容易巧合，共用词元会被无关 FAQ 抢走（答非所问）。
// 代价是语序变化、字面不连续的同义问法匹配不上，交给相似度与 RAG 层兜底。
func faqExactKeywordHits(msg string, keywords []string) int {
	lowerMsg := strings.ToLower(msg)
	hits := 0
	for _, kw := range keywords {
		if kw == "" {
			continue
		}
		if strings.Contains(lowerMsg, strings.ToLower(kw)) {
			hits++
		}
	}
	return hits
}

// faqTokenHits 统计用户问法与关键词共用词元的关键词个数（精确命中为 0 时兜底）。
func faqTokenHits(msg string, keywords []string) int {
	msgTokens := tokenize(msg)
	if len(msgTokens) == 0 {
		return 0
	}
	hits := 0
	for _, kw := range keywords {
		if kw == "" {
			continue
		}
		if keywordTokenOverlap(msg, kw) {
			hits++
		}
	}
	return hits
}

// faqTokenOverlapCount 算用户问法与 FAQ 文本的词元重合个数。
// 与 jaccard 的区别：jaccard 是重合占比（语序颠倒时趋近 0），这里只数重合了几个。
// 只用于没有配关键词的 FAQ。
func faqTokenOverlapCount(msgTokens, faqTokens []string) int {
	if len(msgTokens) == 0 || len(faqTokens) == 0 {
		return 0
	}
	setB := make(map[string]struct{}, len(faqTokens))
	for _, t := range faqTokens {
		setB[t] = struct{}{}
	}
	seen := make(map[string]struct{}, len(msgTokens))
	overlap := 0
	for _, t := range msgTokens {
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		if _, ok := setB[t]; ok {
			overlap++
		}
	}
	return overlap
}

// keywordTokenOverlap 判断用户句与关键词是否共用词元（核心词在、语序不同的情况）。
func keywordTokenOverlap(msg, kw string) bool {
	msgTokens := tokenize(msg)
	if len(msgTokens) == 0 {
		return false
	}
	kwTokens := tokenize(kw)
	if len(kwTokens) == 0 {
		return false
	}
	for _, t := range kwTokens {
		for _, mt := range msgTokens {
			if t == mt {
				return true
			}
		}
	}
	return false
}

func hasChinese(s string) bool {
	for _, r := range s {
		if isCJK(r) {
			return true
		}
	}
	return false
}

func isCJK(r rune) bool {
	return r >= 0x4E00 && r <= 0x9FFF
}

func jaccardSimilarity(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	setA := make(map[string]struct{}, len(a))
	for _, s := range a {
		setA[s] = struct{}{}
	}
	setB := make(map[string]struct{}, len(b))
	for _, s := range b {
		setB[s] = struct{}{}
	}
	inter := 0
	for k := range setA {
		if _, ok := setB[k]; ok {
			inter++
		}
	}
	union := len(setA) + len(setB) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func logPlus(n int64) float64 {
	if n <= 0 {
		return 0
	}
	x := float64(n)
	if x < 1 {
		return 0
	}
	if x < 10 {
		return 0.5
	}
	if x < 100 {
		return 1.0
	}
	return 1.5
}
