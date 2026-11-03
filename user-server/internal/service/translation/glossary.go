package translation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"
	i18npkg "hivemtk-user/internal/pkg/i18n"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"

	"gorm.io/gorm"
)

// ErrGlossaryNotFound 术语未找到 sentinel error
//
// 五层架构修复：controller 不应直接引用 gorm.ErrRecordNotFound，
// service 层将底层 ORM 错误转为业务 sentinel error，供 controller 用 errors.Is 检查。
// service 层引用 gorm 包仅用于错误识别，不直接持有/使用 *gorm.DB（符合五层架构）。
var ErrGlossaryNotFound = errors.New("glossary: not found")

// GlossaryRepo 术语表仓储接口（由 repository.GlossaryRepository 实现）。
type GlossaryRepo interface {
	ListActive(ctx context.Context) ([]*model.Glossary, error)
	GetByTermID(ctx context.Context, termID string) (*model.Glossary, error)
	Create(ctx context.Context, g *model.Glossary) error
	Update(ctx context.Context, g *model.Glossary) error
	Delete(ctx context.Context, termID string) error
	ListByCategory(ctx context.Context, category string) ([]*model.Glossary, error)
	ListAll(ctx context.Context, status, keyword string, page, pageSize int) ([]*model.Glossary, int64, error)
}

// GlossaryView 术语表视图（针对某目标语言渲染后的结果）。
//
// 字段说明：
//   - Lang     ：目标语言短码
//   - Mappings ：源语言词 → 目标语言词（用于 LLM 输出后置校准）
//   - Patterns ：正则保护模式（命中后保持原样，不翻译）
type GlossaryView struct {
	Lang     string            `json:"lang"`
	Mappings map[string]string `json:"mappings"`
	Patterns []string          `json:"patterns"`
}

type glossaryTranslation struct {
	Lang string `json:"lang"`
	Text string `json:"text"`
}

const glossaryCacheTTL = time.Hour

// glossaryCacheKeyPrefix 术语缓存键前缀。
//
// 键里必须带租户维度：术语是租户业务数据（品牌/SKU/价格保护词），
// 同一个 lang 在两个租户下是两套不同的映射表。键不带租户 = 租户 A 的术语视图
// 被喂给租户 B 的 LLM 输出校准。
const glossaryCacheKeyPrefix = "glossary:"

// glossaryCacheKey 拼出「租户 + 目标语言」维度的缓存键。
//
// ctx 里没有租户绑定时回落默认租户 1（离线单机形态的唯一租户）；
// 在线形态下真正的隔离由隔离 callback 在**读库那一步** fail-closed 把关——
// 这里的回落只决定缓存键的命名空间，不决定能不能读到数据。
func glossaryCacheKey(ctx context.Context, lang string) string {
	tid, ok := tenant.FromContext(ctx)
	if !ok {
		tid = tenant.DefaultTenantID
	}
	return fmt.Sprintf("%s%d:lang:%s", glossaryCacheKeyPrefix, tid, lang)
}

// glossaryCacheKeysOfTenant 列出该租户在全部受支持语言下的缓存键。
//
// 用于「本租户术语变了 ⇒ 只失效本租户本模块的缓存」。
// 这里**不能**用 cache.Clear：Redis 后端的 Clear 是 FLUSHDB
// （internal/cache/redis.go:201），一次调用会把整个 Redis 库清空——
// 别的租户、别的模块（会话、限流、权限）的缓存一起没了。
// 一个租户管理员保存一条术语，却把整台实例的缓存掀了。
func glossaryCacheKeysOfTenant(ctx context.Context) []string {
	keys := make([]string, 0, len(i18npkg.SupportedLanguages)+1)
	for lang := range i18npkg.SupportedLanguages {
		keys = append(keys, glossaryCacheKey(ctx, lang))
	}
	// NormalizeLang 的兜底目标是 "zh"，上面枚举里已含 zh；这里再补一次当前
	// 语言归一化后的结果，覆盖「枚举表将来加了语种但归一化另有分支」的情况。
	keys = append(keys, glossaryCacheKey(ctx, i18npkg.NormalizeLang("")))
	return keys
}

// GlossaryService 术语表服务。
type GlossaryService struct {
	repo  GlossaryRepo
	cache cache.Cache
}

// NewGlossaryService 构造 GlossaryService。
//
// cache 为 nil 时回退全局缓存（cache.GetGlobalCache），保证未注入 Redis
// 的单实例部署也能工作。
func NewGlossaryService(repo GlossaryRepo, c cache.Cache) *GlossaryService {
	if c == nil {
		c = cache.GetGlobalCache()
	}
	return &GlossaryService{repo: repo, cache: c}
}

// LoadByLang 加载某目标语言下的术语视图。
//
// 流程：
//  1. 命中缓存 → 直接返回反序列化结果
//  2. 未命中 → 调 repo.ListActive → 构建 GlossaryView → 写缓存 → 返回
//
// 错误处理：
//   - repo 报错：返回 (nil, err)
//   - 缓存读写失败：仅记录日志，不阻断主流程（缓存只是优化）
//
// lang 为空时兜底 "zh"。未归一化的语言代码会被 NormalizeLang 处理。
func (s *GlossaryService) LoadByLang(ctx context.Context, lang string) (*GlossaryView, error) {
	lang = i18npkg.NormalizeLang(lang)

	if view, ok := s.loadFromCache(ctx, lang); ok {
		return view, nil
	}

	list, err := s.repo.ListActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("glossary: list active failed: %w", err)
	}

	view := s.buildView(lang, list)

	s.saveToCache(ctx, view)
	return view, nil
}

// Render 渲染术语表为 system prompt 块。
//
// 返回空串表示无术语或加载失败（不阻断 LLM 调用）。
// 输出格式（Markdown 友好，可直接拼接到 system prompt 末尾）：
//
//	# Glossary (target=zh)
//
//	## 必须使用的目标语言术语（用右侧替换左侧）：
//	- Apple → 苹果
//
//	## 不可翻译的保护模式（保持原样）：
//	- SKU-[A-Z0-9]{6,}
//
// Calibrate 对 LLM 输出做术语后置校准（满足 aiagent 层 OutputCalibrator 接口）。
//
// 加载目标语言术语表视图，调用 PostValidator 做术语校准与敏感模式保护。
// 返回校准后文本；加载或校准失败时返回原文（不影响主流程可用性）。
func (s *GlossaryService) Calibrate(ctx context.Context, text string, targetLang string) (string, error) {
	if text == "" {
		return text, nil
	}
	view, err := s.LoadByLang(ctx, targetLang)
	if err != nil {
		return text, err
	}
	out, _ := NewPostValidator().Validate(text, targetLang, view)
	return out, nil
}

func (s *GlossaryService) Render(ctx context.Context, lang string) string {
	lang = i18npkg.NormalizeLang(lang)
	view, err := s.LoadByLang(ctx, lang)
	if err != nil || view == nil {
		return ""
	}
	if len(view.Mappings) == 0 && len(view.Patterns) == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Glossary (target=%s)\n\n", view.Lang)

	if len(view.Mappings) > 0 {
		b.WriteString("## 必须使用的目标语言术语（用右侧替换左侧）：\n")
		for src, dst := range view.Mappings {
			fmt.Fprintf(&b, "- %s → %s\n", src, dst)
		}
		b.WriteString("\n")
	}
	if len(view.Patterns) > 0 {
		b.WriteString("## 不可翻译的保护模式（保持原样）：\n")
		for _, p := range view.Patterns {
			fmt.Fprintf(&b, "- %s\n", p)
		}
	}
	return b.String()
}

// InvalidateCache 失效指定语言的术语缓存。
//
// lang 为 "*" 或空时清空**本租户**的术语缓存（逐个语言键删）。
// 不用 cache.Clear 的理由见 glossaryCacheKeysOfTenant 的注释：
// Redis 后端的 Clear 是 FLUSHDB，会掀掉整台实例所有租户所有模块的缓存。
func (s *GlossaryService) InvalidateCache(ctx context.Context, lang string) {
	lang = i18npkg.NormalizeLang(lang)
	if lang == "" || lang == "*" {
		s.InvalidateAll(ctx)
		return
	}
	key := glossaryCacheKey(ctx, lang)
	if err := s.cache.Delete(ctx, key); err != nil {
		logger.Warnf("glossary: invalidate cache key=%s failed: %v", key, err)
	}
}

// InvalidateAll 失效**本租户**的全部术语缓存（用于术语批量导入等场景）。
//
// 逐键删本租户在所有受支持语言下的术语缓存键；**不**调 cache.Clear：
// Redis 后端的 Clear = FLUSHDB，租户之间、模块之间会互相清场。
func (s *GlossaryService) InvalidateAll(ctx context.Context) {
	for _, key := range glossaryCacheKeysOfTenant(ctx) {
		if err := s.cache.Delete(ctx, key); err != nil {
			logger.Warnf("glossary: invalidate all cache key=%s failed: %v", key, err)
		}
	}
}

// Create 创建术语。写库后失效全量缓存（保证后续 LoadByLang 拉到最新数据）。
func (s *GlossaryService) Create(ctx context.Context, g *model.Glossary) error {
	if g == nil {
		return fmt.Errorf("glossary: nil entity")
	}
	if g.Status == "" {
		g.Status = "active"
	}
	if err := s.repo.Create(ctx, g); err != nil {
		// term_id 全局唯一：重复时给出可执行文案，而不是把 SQL 唯一键冲突抛成「创建术语失败」
		if repository.IsDuplicateKeyErr(err) {
			return &GlossaryDuplicateError{TermID: g.TermID}
		}
		return fmt.Errorf("glossary: create failed: %w", err)
	}
	s.InvalidateAll(ctx)
	return nil
}

// Update 更新术语（按主键 ID）。写库后失效全量缓存。
func (s *GlossaryService) Update(ctx context.Context, g *model.Glossary) error {
	if g == nil {
		return fmt.Errorf("glossary: nil entity")
	}
	if err := s.repo.Update(ctx, g); err != nil {
		return fmt.Errorf("glossary: update failed: %w", err)
	}
	s.InvalidateAll(ctx)
	return nil
}

// Delete 删除术语（按 TermID）。写库后失效全量缓存。
func (s *GlossaryService) Delete(ctx context.Context, termID string) error {
	if termID == "" {
		return fmt.Errorf("glossary: empty term_id")
	}
	if err := s.repo.Delete(ctx, termID); err != nil {
		return fmt.Errorf("glossary: delete failed: %w", err)
	}
	s.InvalidateAll(ctx)
	return nil
}

// GetByTermID 按 TermID 查询单条术语。
func (s *GlossaryService) GetByTermID(ctx context.Context, termID string) (*model.Glossary, error) {
	if termID == "" {
		return nil, fmt.Errorf("glossary: empty term_id")
	}
	g, err := s.repo.GetByTermID(ctx, termID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrGlossaryNotFound
		}
		return nil, fmt.Errorf("glossary: get by term_id failed: %w", err)
	}
	return g, nil
}

// List 分页查询术语（支持 status / keyword 过滤）。
func (s *GlossaryService) List(ctx context.Context, status, keyword string, page, pageSize int) ([]*model.Glossary, int64, error) {
	list, total, err := s.repo.ListAll(ctx, status, keyword, page, pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("glossary: list failed: %w", err)
	}
	return list, total, nil
}

// ListByCategory 按分类查询 active 术语（全量，不分页）。
func (s *GlossaryService) ListByCategory(ctx context.Context, category string) ([]*model.Glossary, error) {
	list, err := s.repo.ListByCategory(ctx, category)
	if err != nil {
		return nil, fmt.Errorf("glossary: list by category failed: %w", err)
	}
	return list, nil
}

func (s *GlossaryService) loadFromCache(ctx context.Context, lang string) (*GlossaryView, bool) {
	key := glossaryCacheKey(ctx, lang)
	var view GlossaryView
	if err := s.cache.GetJSON(ctx, key, &view); err != nil {
		logger.Debugf("glossary: cache get key=%s err=%v", key, err)
		return nil, false
	}
	if view.Lang == "" {
		return nil, false
	}
	return &view, true
}

func (s *GlossaryService) saveToCache(ctx context.Context, view *GlossaryView) {
	if view == nil {
		return
	}
	key := glossaryCacheKey(ctx, view.Lang)
	if err := s.cache.SetJSON(ctx, key, view, glossaryCacheTTL); err != nil {
		logger.Warnf("glossary: cache set key=%s err=%v", key, err)
	}
}

func (s *GlossaryService) buildView(lang string, list []*model.Glossary) *GlossaryView {
	view := &GlossaryView{
		Lang:     lang,
		Mappings: make(map[string]string),
	}
	patternSeen := make(map[string]struct{})

	for _, g := range list {
		if g == nil {
			continue
		}
		if g.Pattern != "" {
			if _, ok := patternSeen[g.Pattern]; !ok {
				view.Patterns = append(view.Patterns, g.Pattern)
				patternSeen[g.Pattern] = struct{}{}
			}
		}
		if g.Preserve {
			continue
		}

		translations := parseTranslations(g.Translations)
		if len(translations) == 0 {
			continue
		}

		dst := pickTextByLang(translations, lang)
		if dst == "" {
			continue
		}

		for _, t := range translations {
			if t.Lang == lang {
				continue
			}
			if t.Text == "" || t.Text == dst {
				continue
			}
			if _, exists := view.Mappings[t.Text]; !exists {
				view.Mappings[t.Text] = dst
			}
		}
	}
	return view
}

func parseTranslations(raw model.JSONMap) []glossaryTranslation {
	if len(raw) == 0 {
		return nil
	}
	out := make([]glossaryTranslation, 0, len(raw))
	for lang, val := range raw {
		if lang == "" {
			continue
		}
		text := toString(val)
		if text == "" {
			continue
		}
		out = append(out, glossaryTranslation{Lang: lang, Text: text})
	}
	return out
}

func toString(val any) string {
	if val == nil {
		return ""
	}
	switch v := val.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func pickTextByLang(translations []glossaryTranslation, lang string) string {
	for _, t := range translations {
		if t.Lang == lang {
			return t.Text
		}
	}
	return ""
}

// GlossaryDuplicateError 术语 term_id 唯一冲突。
//
// 单独成型而不是用 fmt.Errorf：控制器需要 errors.As 判定后回 409 + 可执行文案，
// 否则会落到 ErrorFromDB 的兜底 500「创建术语失败」。
type GlossaryDuplicateError struct{ TermID string }

func (e *GlossaryDuplicateError) Error() string {
	return fmt.Sprintf("术语 %s 已存在（term_id 需唯一），请修改 term_id 或直接编辑已有术语", e.TermID)
}
