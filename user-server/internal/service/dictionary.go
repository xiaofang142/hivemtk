package service

import (
	"context"
	"errors"
	"sort"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// 业务字典（阶段四，对应 dictionaries / dictionary_transitions 两表）。
//
// 与 config_params 的分界：
//
//	config_params  一个 key 一个标量值         → 「这一个旋钮拧到几」
//	dictionaries   一个 key 一组枚举 + 跃迁     → 「有哪几种状态」「A 能不能到 B」
//
// 不寄生在参数中心上的理由：一个 5 状态的审批流要 5 条 string 参数 + 一张 10 行的
// 有序二元组，而参数中心既没有顺序也没有「成组」的概念，翻出来是 15 行平铺记录，
// 运维看不出它们是一套。
//
// 读取口径：**DB 优先，DB 没这一批就回落 Go 常量**。不反过来（常量优先、DB 只做覆盖），
// 因为字典是「存量行正在用的值域」——历史数据里躺着的状态码必须一直在册，
// 否则前端翻译存量行时会翻车。回落常量保证「没配过字典的站点行为与今天一致」。

var (
	// ErrDictionaryParentUnknown 域不存在（DB 与种子都没有）。
	ErrDictionaryParentUnknown = errors.New("dictionary: 未知的字典域")
	// ErrDictionarySystemLocked 试图改名/停用一条种子锁定项。
	ErrDictionarySystemLocked = errors.New("dictionary: 该项由代码种子锁定，不能在此处改名或停用")
)

// dictionaryRepo 仓储接口。抽出来是为了让服务层可测（不需要真库就能验证
// 「DB 没有这一批就回落种子」这条口径），与仓内其它域的 repo 接口同一做法。
type dictionaryRepo interface {
	ListByParent(ctx context.Context, parent string) ([]model.Dictionary, error)
	ListParents(ctx context.Context) ([]string, error)
	GetByCode(ctx context.Context, parent, code string) (*model.Dictionary, error)
	Upsert(ctx context.Context, d *model.Dictionary, actorID uint) error
	ListTransitions(ctx context.Context, parent string) ([]model.DictionaryTransition, error)
	ReplaceTransitions(ctx context.Context, parent string, trans []model.DictionaryTransition, actorID uint) error
}

// DictionaryService 字典服务。
type DictionaryService struct {
	repo dictionaryRepo
}

func NewDictionaryService(repo *repository.DictionaryRepository) *DictionaryService {
	return &DictionaryService{repo: repo}
}

// NewDictionaryServiceWithRepo 用注入的仓储构造（测试用）。
// 生产路径只走 NewDictionaryService；拆这个口是为了不连真库也能验证
// 「DB 没有这一批就回落种子」这条口径。
func NewDictionaryServiceWithRepo(repo dictionaryRepo) *DictionaryService {
	return &DictionaryService{repo: repo}
}

// seedOnlyDictionaryRepo 不接 DB 的只读仓储：任何查询都返回空集，
// 触发 GetDictionary 的「DB 无该域则回落种子」口径。
type seedOnlyDictionaryRepo struct{}

func (*seedOnlyDictionaryRepo) ListByParent(context.Context, string) ([]model.Dictionary, error) {
	return nil, nil
}
func (*seedOnlyDictionaryRepo) ListParents(context.Context) ([]string, error) { return nil, nil }
func (*seedOnlyDictionaryRepo) GetByCode(context.Context, string, string) (*model.Dictionary, error) {
	return nil, nil
}
func (*seedOnlyDictionaryRepo) Upsert(context.Context, *model.Dictionary, uint) error {
	return errors.New("seed-only 字典仓储不支持写入")
}
func (*seedOnlyDictionaryRepo) ListTransitions(context.Context, string) ([]model.DictionaryTransition, error) {
	return nil, nil
}
func (*seedOnlyDictionaryRepo) ReplaceTransitions(context.Context, string, []model.DictionaryTransition, uint) error {
	return errors.New("seed-only 字典仓储不支持写入")
}

// NewSeedOnlyDictionaryService 纯种子字典服务：站点尚未配置 DB 字典项时，
// CRM 编辑/筛选/导入预检仍有一套确定、可回查的受控选项。
func NewSeedOnlyDictionaryService() *DictionaryService {
	return &DictionaryService{repo: &seedOnlyDictionaryRepo{}}
}

// GetDictionary 取一个域的完整定义（项 + 跃迁）。DB 没有该域时回落种子。
func (s *DictionaryService) GetDictionary(ctx context.Context, parent string) ([]model.Dictionary, []model.DictionaryTransition, error) {
	items, err := s.repo.ListByParent(ctx, parent)
	if err != nil {
		return nil, nil, err
	}
	trans, err := s.repo.ListTransitions(ctx, parent)
	if err != nil {
		return nil, nil, err
	}
	if len(items) == 0 {
		seedItems, seedTrans, ok := lookupDictionarySeed(parent)
		if !ok {
			return nil, nil, ErrDictionaryParentUnknown
		}
		return seedItems, seedTrans, nil
	}
	return items, trans, nil
}

// DictionaryParent 管理端侧栏的一个字典域。
//
// 为什么不让 ListParents 继续返回 []string：那样管理端只能显示
// approval_status / payment_status 这种英文 key，既不像给人看的，也无法说明
// 这一域管什么。域这一层的中文名与说明是真实存在的信息，不该让前端再硬编码一份
// （config_param_groups 就是因为前端硬编码 GROUP_LABELS 才出的问题）。
type DictionaryParent struct {
	Code        string `json:"code"`
	Label       string `json:"label"`
	Description string `json:"description"`
	// ItemCount 是这一域的项数（含已停用项）：停用项不参与新写入，但存量行仍可能是它，
	// 所以侧栏徽标必须把它算进去，否则用户会以为某状态不存在。
	ItemCount int `json:"item_count"`
	// Seeded 标记该域的元数据来自代码种子。运营改了 label 也不会覆盖种子
	// （DomainMeta 不落库），这里只用于在 UI 上说明"名称由代码定义"。
	Seeded bool `json:"seeded"`
}

// ListParents 管理端目录。DB 与种子的并集（去重排序），带域级中文名。
func (s *DictionaryService) ListParents(ctx context.Context) ([]DictionaryParent, error) {
	dbParents, err := s.repo.ListParents(ctx)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(dbParents)+len(dictionarySeedIndex))
	out := make([]DictionaryParent, 0, len(dbParents)+len(dictionarySeedIndex))
	appendParent := func(code string) {
		if _, dup := seen[code]; dup {
			return
		}
		seen[code] = struct{}{}
		seed, hasSeed := dictionarySeedIndex[code]
		p := DictionaryParent{Code: code, Label: code, Seeded: hasSeed}
		if hasSeed {
			if seed.label != "" {
				p.Label = seed.label
			}
			p.Description = seed.description
		}
		out = append(out, p)
	}
	for _, p := range dbParents {
		appendParent(p)
	}
	for p := range dictionarySeedIndex {
		appendParent(p)
	}
	// 计数从「真实读到的那一份」数：DB 有就用 DB 的（运营可能加过项），
	// 否则用种子的项数。与 GetDictionary 的回落口径一致。
	for i := range out {
		items, _, err := s.GetDictionary(ctx, out[i].Code)
		if err != nil {
			continue
		}
		out[i].ItemCount = len(items)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Code < out[b].Code })
	return out, nil
}

// UpsertItem 建/改一条字典项。种子锁定项禁止改名/停用。
func (s *DictionaryService) UpsertItem(ctx context.Context, d *model.Dictionary, actorID uint) error {
	if d.ParentCode == "" || d.Code == "" {
		return errors.New("dictionary: parent_code 与 code 必填")
	}
	existing, err := s.repo.GetByCode(ctx, d.ParentCode, d.Code)
	if err != nil {
		return err
	}
	if existing != nil && existing.SystemLocked {
		return ErrDictionarySystemLocked
	}
	return s.repo.Upsert(ctx, d, actorID)
}

// ReplaceTransitions 整体替换一个域的跃迁。
func (s *DictionaryService) ReplaceTransitions(ctx context.Context, parent string, trans []model.DictionaryTransition, actorID uint) error {
	return s.repo.ReplaceTransitions(ctx, parent, trans, actorID)
}
