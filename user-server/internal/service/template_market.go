package service

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// 模板/技能市场（F2 最小可用版）服务层。
//
// 职责边界：本服务是既有「内容模板 / SOP / 对接模板 / MCP / 智能路由」之上的
// **目录层**——只读浏览 + 元数据 + 分类。它不渲染任何模板内容、不改任何
// 既有实现的内容语义；要拿内容，按目录项 Source 去找那个事实源。
// 数据源是进程内注册的只读目录（repository.TemplateMarketRepository），不连 DB。

// ErrMarketEntryNotFound 目录中查无此条目（fail-closed 哨兵）。
// 上层据此映射 404 语义；不要把它包成 CodeInternal——「查无」是客户端可预期结果。
var ErrMarketEntryNotFound = errors.New("模板/技能不在市场目录中")

// MarketKind 与 MarketAvailability 常量直接复用仓储层的契约常量，
// 避免 service/repository 两份枚举漂移（单一事实源）。
const (
	MarketKindTemplate = repository.EntryKindTemplate
	MarketKindSkill    = repository.EntryKindSkill
)

// MarketItem 市场条目（API 面向的元数据契约）。
// 与 repository.TemplateMarketEntry 一一对应，字段名就是前后端契约。
type MarketItem struct {
	ID           string   `json:"id"`
	Kind         string   `json:"kind"`
	Name         string   `json:"name"`
	Category     string   `json:"category"`
	Summary      string   `json:"summary"`
	Source       string   `json:"source"`
	Availability string   `json:"availability"`
	Tags         []string `json:"tags,omitempty"`
	// PriceCents 售价（分）。0=免费（内置种子条目恒 0）；写侧条目可定价，
	// 付费条目购买走 S-22 订单链（POST /api/template-market/entries/:id/order）。
	PriceCents int64 `json:"price_cents"`
}

// MarketQuery 目录查询条件。零值 = 不过滤，返回全量。
type MarketQuery struct {
	Kind     string // 为空不筛；否则精确匹配 template|skill
	Category string // 为空不筛；否则精确匹配
	Keyword  string // 为空不筛；否则在 name/summary/tags 里做子串匹配（忽略大小写）
}

// TemplateMarketService 市场目录服务。
type TemplateMarketService struct {
	repo repository.TemplateMarketRepository
	// dbEntries 写侧已上架条目源（specs/013 S-22 发布-审核闭环的合并面）。
	// nil = 纯种子目录（进程内编译期目录，S-22 之前的形态）。
	// 非 nil = 种子 + DB approved 条目合并（审核通过即上橱窗）。
	dbEntries marketEntryCatalogSource
}

// marketEntryCatalogSource 已上架条目的只读源（由 MarketEntryService 的仓储适配，
// 不让本服务直接依赖具体仓储类型——目录层对写侧只取「已上架快照」一件事）。
type marketEntryCatalogSource interface {
	// ListApprovedEntries 取全部已上架条目。
	ListApprovedEntries(ctx context.Context) ([]*model.MarketCatalogEntry, error)
	// GetEntry 按 ID 取条目（含未上架；橱窗侧自行按状态过滤）。查无 (nil, nil)。
	GetEntry(ctx context.Context, id string) (*model.MarketCatalogEntry, error)
}

// NewTemplateMarketService 构造生产橱窗：进程内种子 + DB 已上架条目合并
// （S-22 发布-审核闭环：审核通过即上橱窗）。DB 侧读失败时**降级为纯种子**——
// 公开橱窗是匿名可达面，目录表故障不该把内置目录一起拖死。
func NewTemplateMarketService() *TemplateMarketService {
	return &TemplateMarketService{
		repo:      repository.NewMemoryTemplateMarketRepository(),
		dbEntries: repository.NewMarketEntryRepositoryDefault(),
	}
}

// NewTemplateMarketServiceWithDB 构造（种子 + 显式注入的写侧仓储；测试用）。
func NewTemplateMarketServiceWithDB(repo *repository.MarketEntryRepository) *TemplateMarketService {
	return &TemplateMarketService{
		repo:      repository.NewMemoryTemplateMarketRepository(),
		dbEntries: repo,
	}
}

// NewTemplateMarketServiceWithRepo 带显式仓储（测试注入）。
func NewTemplateMarketServiceWithRepo(repo repository.TemplateMarketRepository) *TemplateMarketService {
	return &TemplateMarketService{repo: repo}
}

// List 目录查询。过滤顺序固定：kind → category → keyword，结果保持目录稳定序。
//
// 数据源合并（S-22）：进程内种子在前（稳定序即种子序），DB 已上架条目在后
// （created_at DESC，新上架排前）。同 ID 冲突时种子优先——内置条目不允许被
// 提审条目顶掉（提审侧已查重挡住，这里是兜底）。
func (s *TemplateMarketService) List(ctx context.Context, q MarketQuery) ([]MarketItem, error) {
	entries, err := s.repo.All(ctx)
	if err != nil {
		return nil, err
	}
	dbRows := s.approvedDBEntriesTolerant(ctx, "List")
	seen := make(map[string]struct{}, len(entries)+len(dbRows))
	items := make([]MarketItem, 0, len(entries)+len(dbRows))
	for _, e := range entries {
		if !matchMarket(e, q) {
			continue
		}
		seen[e.ID] = struct{}{}
		items = append(items, toMarketItem(e))
	}
	for _, row := range dbRows {
		if _, dup := seen[row.ID]; dup {
			continue
		}
		e := marketRowToEntry(row)
		if !matchMarket(e, q) {
			continue
		}
		seen[row.ID] = struct{}{}
		items = append(items, toMarketItem(e))
	}
	return items, nil
}

// Categories 返回目录中出现的全部分类（去重、升序），供前端做筛选器。
// 种子与 DB 已上架条目同池统计（S-22 合并橱窗）。
func (s *TemplateMarketService) Categories(ctx context.Context) ([]string, error) {
	entries, err := s.repo.All(ctx)
	if err != nil {
		return nil, err
	}
	dbRows := s.approvedDBEntriesTolerant(ctx, "Categories")
	set := make(map[string]struct{}, len(entries)+len(dbRows))
	for _, e := range entries {
		set[e.Category] = struct{}{}
	}
	for _, row := range dbRows {
		set[row.Category] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	sort.Strings(out)
	return out, nil
}

// Get 取单条详情。未知 ID 一律 fail-closed 返回 ErrMarketEntryNotFound，
// 不把「查无」翻译成 500——空 ID 与乱敲 ID 同样视为查无。
// 种子命名空间优先，其次 DB 已上架条目；DB 里存在但未上架的条目按「查无」口径
// （橱窗只展示 approved，待审/驳回/下架对外不可见）。
func (s *TemplateMarketService) Get(ctx context.Context, id string) (*MarketItem, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrMarketEntryNotFound
	}
	e, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if e == nil {
		row := s.dbEntryByIDTolerant(ctx, id)
		if row == nil || row.Status != "approved" {
			return nil, ErrMarketEntryNotFound
		}
		item := toMarketItem(marketRowToEntry(row))
		return &item, nil
	}
	item := toMarketItem(*e)
	return &item, nil
}

// approvedDBEntriesTolerant 取 DB 已上架条目；失败降级为空（种子照常可用）。
// 公开橱窗是匿名可达面：目录表故障（表未建/连接抖动）不该把内置目录一起拖死，
// 只把降级事实留在日志里给运维看。
func (s *TemplateMarketService) approvedDBEntriesTolerant(ctx context.Context, caller string) []*model.MarketCatalogEntry {
	if s.dbEntries == nil {
		return nil
	}
	rows, err := s.dbEntries.ListApprovedEntries(ctx)
	if err != nil {
		logger.Warnf("[TemplateMarket] %s 读已上架条目失败，降级纯种子目录: %v", caller, err)
		return nil
	}
	return rows
}

// dbEntryByIDTolerant 按 ID 查 DB 条目；失败按查无处理（与上同一条降级口径）。
func (s *TemplateMarketService) dbEntryByIDTolerant(ctx context.Context, id string) *model.MarketCatalogEntry {
	if s.dbEntries == nil {
		return nil
	}
	row, err := s.dbEntries.GetEntry(ctx, id)
	if err != nil {
		logger.Warnf("[TemplateMarket] Get(%s) 读目录条目失败，按查无处理: %v", id, err)
		return nil
	}
	return row
}

// marketRowToEntry DB 行 → 目录契约条目（tags 列是 jsonb 数组文本，反解；
// 解不动按无标签处理——展示层的标签缺失不该炸整页）。
func marketRowToEntry(row *model.MarketCatalogEntry) repository.TemplateMarketEntry {
	var tags []string
	if err := json.Unmarshal([]byte(row.Tags), &tags); err != nil {
		tags = nil
	}
	return repository.TemplateMarketEntry{
		ID:           row.ID,
		Kind:         row.Kind,
		Name:         row.Name,
		Category:     row.Category,
		Summary:      row.Summary,
		Source:       row.Source,
		Availability: row.Availability,
		Tags:         tags,
		PriceCents:   row.PriceCents,
	}
}

func toMarketItem(e repository.TemplateMarketEntry) MarketItem {
	tags := make([]string, len(e.Tags))
	copy(tags, e.Tags)
	return MarketItem{
		ID:           e.ID,
		Kind:         e.Kind,
		Name:         e.Name,
		Category:     e.Category,
		Summary:      e.Summary,
		Source:       e.Source,
		Availability: e.Availability,
		Tags:         tags,
		PriceCents:   e.PriceCents,
	}
}

func matchMarket(e repository.TemplateMarketEntry, q MarketQuery) bool {
	if q.Kind != "" && e.Kind != q.Kind {
		return false
	}
	if q.Category != "" && e.Category != q.Category {
		return false
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		hay := strings.ToLower(e.Name + " " + e.Summary + " " + strings.Join(e.Tags, " "))
		if !strings.Contains(hay, strings.ToLower(kw)) {
			return false
		}
	}
	return true
}
