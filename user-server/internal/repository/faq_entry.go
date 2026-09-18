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

func NewFAQRepository(db *gorm.DB) *FAQRepository {
	return &FAQRepository{db: db}
}

// Create 新增 FAQ
//
// 注意: GORM v2 对 bool 零值 (false) 不会写入,会使用 column default。
// 此处用 Select("*") 强制全字段 INSERT,确保 false 也能正确落库。
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

// MatchByKeyword 关键词匹配 (Layer1 核心 API) — 兼容旧签名 (agentID=0 表示共享/全局)
//
// 加 agentID 过滤
//   - agentID > 0: 仅匹配该智能体私有 (agent_id=X) + 共享 (agent_id IS NULL)
//   - agentID = 0: 全局共享 (向后兼容, 旧代码不传 agentID 也能跑)
//
// 策略:
//  1. SQL 层先按 (agent_id = ? OR agent_id IS NULL) AND enabled 过滤
//  2. 内存打分 + topK 排序
//  3. 中文 bigram 切词 + 关键词包含加权
//  4. 排序: score * (baseConfidence + log(hit_count+1)) 降序
//  5. 返回 top K
//
// 后续可扩展: pgvector 全文检索 / Embedding 向量召回
func (r *FAQRepository) MatchByKeyword(ctx context.Context, msg string, topK int) ([]model.FAQEntry, error) {
	return r.MatchByKeywordForAgent(ctx, msg, 0, topK)
}

// MatchByKeywordForAgent 按智能体隔离的关键词匹配 (隔离架构)
//
// agentID = 0: 走旧路径 (全局共享, 向后兼容)
// agentID > 0: 匹配 (agent_id = agentID OR agent_id IS NULL) AND enabled
func (r *FAQRepository) MatchByKeywordForAgent(ctx context.Context, msg string, agentID uint, topK int) ([]model.FAQEntry, error) {
	if msg == "" {
		return nil, nil
	}
	all, err := r.listEnabledForAgent(ctx, agentID, 5000)
	if err != nil {
		return nil, err
	}
	return r.scoreAndRank(ctx, all, msg, topK)
}

// ListCandidates 返回指定 agent 的 FAQ 候选集（已启用），用于 Service 层命中内存缓存后复用，
// 避免每次 FAQ 匹配都全量拉取 candidate 再打分。
//
// 语义与 listEnabledForAgent 完全一致：
//   - agentID == 0: 全部已启用 FAQ（共享池）
//   - agentID > 0: agent_id = agentID OR agent_id IS NULL（该 agent 私有 + 共享）
func (r *FAQRepository) ListCandidates(ctx context.Context, agentID uint, limit int) ([]model.FAQEntry, error) {
	return r.listEnabledForAgent(ctx, agentID, limit)
}

// ScoreCandidates 对已加载的 FAQ 候选集做内存打分排序（不触发 DB 查询）。
// 供 Service 层命中内存缓存后复用候选集、仅按不同 query 重新打分。
func (r *FAQRepository) ScoreCandidates(ctx context.Context, entries []model.FAQEntry, msg string, topK int) ([]model.FAQEntry, error) {
	return r.scoreAndRank(ctx, entries, msg, topK)
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

// ListByKB 按知识库 ID 列出 (: 查某 KB 下挂载的 FAQ 条目)
//
// 通过 JOIN agent_kb_bindings + knowledge_bases 确定 (KBID, KBType=faq) 关联的 FAQ
//
// 实现: KBID -> agent_id (via knowledge_bases.owner_agent_id) -> faq_entries.agent_id
//
// 简化实现: 直接按 agent_id 过滤 (KBType=faq 假设)
func (r *FAQRepository) ListByKB(ctx context.Context, kbID uint, agentID uint, limit int) ([]model.FAQEntry, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var entries []model.FAQEntry
	q := r.db.WithContext(ctx).
		Where("enabled = ?", true).
		Order("id DESC").
		Limit(limit)
	if agentID > 0 {
		q = q.Where("agent_id = ?", agentID)
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

// MatchByAgent 按智能体 ID 匹配 (Task 15: 强 1对1 改造)
//
// 行为:
//   - agentID == 0  -> 无绑定空, 返回 (nil, nil)
//   - 仅匹配 enabled = true AND agent_id = ? 的 FAQ
//   - 移除"空数组=全局"分支: 任何 agent 都必须显式绑定才能匹配
//
// SQL: WHERE enabled = true AND agent_id = ?  (走 idx_faq_agent_id 索引)
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
	return r.scoreAndRank(ctx, entries, msg, topK)
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

func (r *FAQRepository) scoreAndRank(ctx context.Context, all []model.FAQEntry, msg string, topK int) ([]model.FAQEntry, error) {
	msgTokens := tokenize(msg)
	if len(msgTokens) == 0 {
		return nil, nil
	}
	type scored struct {
		entry model.FAQEntry
		score float64
	}
	var results []scored
	for i := range all {
		e := all[i]
		if e.Enabled == nil || !*e.Enabled {
			continue
		}
		faqTokens := tokenize(e.Question + " " + e.Answer)
		score := jaccardSimilarity(msgTokens, faqTokens)
		for _, kw := range e.Keywords {
			if kw != "" && strings.Contains(msg, kw) {
				score += 0.3
			}
		}
		score = score * (e.Confidence + 0.1*logPlus(e.HitCount))
		if score < 0.2 {
			continue
		}
		results = append(results, scored{entry: e, score: score})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})
	if topK <= 0 || topK > len(results) {
		topK = len(results)
	}
	out := make([]model.FAQEntry, 0, topK)
	for i := 0; i < topK; i++ {
		results[i].entry.Confidence = results[i].score
		out = append(out, results[i].entry)
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
	return r.db.WithContext(ctx).Model(&model.FAQEntry{}).
		Where("id = ?", id).
		Select("*").Updates(entry).Error
}

// Delete 删除 FAQ
func (r *FAQRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.FAQEntry{}).Error
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

// DecayQuality 衰减指定 FAQ 的 QualityScore (: 质量衰减)
//
// 入参:
//   - id:    要衰减的 FAQ ID
//   - decay: 衰减量, 范围 [0, 1], 调用方负责边界 (repo 不做下限保护以保持纯函数语义)
//
// 行为:
//   - 用 GREATEST(quality_score - decay, 0) 保证不会衰减到负数
//   - 同一事务可多次调用叠加衰减
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

// ListDecayCandidates 查询符合衰减条件的 FAQ
//
// 条件:
//   - hit_count < 5 (低命中)
//   - last_hit_at 非空 且 早于 cutoff (超过 7 天未被命中)
//
// cutoff 建议为 now-7d, 调用方注入
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

// IncrementNegativeHit 用户负反馈 +1 (: 负反馈快速降权)
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
