package service

// intent_catalog.go TB-10 刀2：统一意图目录服务层（双词表收敛为单一注册表视图层）。
//
// 职责：intent_catalog 单表（source=default/custom）的读视图层——
// EffectiveCatalogDefs 把目录行收敛成识别器消费的 []IntentDef（默认行在前、
// override KV 先合、自定义行追加在后，与 intentsForRuleWithOverride 逐字同语义）；
// IsCatalogIntent 供绑定校验放行；IntentFamilyOf 供兼容视图（runtime/service/custom）。
// catalog 无行（未播种）时全部回落既有静态词表，零行为变化。
//
// 冻结契约（TB-10，2026-11-10）：
//   - 兼容映射视图（intent_families 三值）保留 2 发布周期，消费方逐个迁移后收口；
//   - service/runtime Intent 常量是目录的「内置种子」（SeedBuiltinDefaults 幂等）；
//   - 30s TTL 缓存 + 写失效（与 CustomIntentService 同口径）；
//   - 识别行为不漂移：默认词优先、词面计分语义不变、override KV 合并逐字对齐。

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
)

// intentCatalogCacheTTL 跨副本写失效兜底窗口（与绑定层同口径）。
const intentCatalogCacheTTL = 30 * time.Second

// 目录 source / family 取值（与 model 列约束一致）。
const (
	intentCatalogSourceDefault = "default"
	intentCatalogSourceCustom  = "custom"

	intentCatalogFamilyRuntime = "runtime"
	intentCatalogFamilyService = "service"
	intentCatalogFamilyCustom  = "custom"
)

var (
	globalIntentCatalogSvc *IntentCatalogService
)

// SetIntentCatalogService 装配层注入（app.InitIntentChainRuntime；nil 不生效）。
func SetIntentCatalogService(svc *IntentCatalogService) {
	globalIntentCatalogSvc = svc
}

// GetIntentCatalogService 取单例（未装配返回 nil；消费方 nil-safe）。
func GetIntentCatalogService() *IntentCatalogService {
	return globalIntentCatalogSvc
}

// IntentCatalogService 统一意图目录服务（TB-10）。
type IntentCatalogService struct {
	repo repository.IntentCatalogRepository

	mu            sync.RWMutex
	cache         map[uint][]model.IntentCatalog
	cacheLoadedAt map[uint]time.Time
}

func NewIntentCatalogService(repo repository.IntentCatalogRepository) *IntentCatalogService {
	return &IntentCatalogService{
		repo:          repo,
		cache:         make(map[uint][]model.IntentCatalog),
		cacheLoadedAt: make(map[uint]time.Time),
	}
}

// loadCatalog 读当前租户启用中目录行（缓存读；失败返 nil → 消费方回落静态词表）。
func (s *IntentCatalogService) loadCatalog(ctx context.Context) []model.IntentCatalog {
	if s == nil || s.repo == nil {
		return nil
	}
	id := tenantIDOfCtx(ctx)
	s.mu.RLock()
	loadedAt, ok := s.cacheLoadedAt[id]
	rows := s.cache[id]
	s.mu.RUnlock()
	if ok && time.Since(loadedAt) <= intentCatalogCacheTTL {
		return rows
	}
	loaded, err := s.repo.ListEnabled(ctx)
	if err != nil {
		return nil
	}
	if loaded == nil {
		loaded = []model.IntentCatalog{}
	}
	s.mu.Lock()
	s.cache[id] = loaded
	s.cacheLoadedAt[id] = time.Now()
	s.mu.Unlock()
	return loaded
}

// loadCatalogForVocab 供 vocab 端点消费的目录读法：启用行按 intent_key 升序
// （IntentKey+Family 直出；空/失败返 nil → 消费方回落静态词表）。
func (s *IntentCatalogService) loadCatalogForVocab(ctx context.Context) []model.IntentCatalog {
	rows := s.loadCatalog(ctx)
	if len(rows) == 0 {
		return nil
	}
	out := append([]model.IntentCatalog(nil), rows...)
	sort.Slice(out, func(i, j int) bool { return out[i].IntentKey < out[j].IntentKey })
	return out
}

// invalidate 写失效（种子/写面同步后调用）。
func (s *IntentCatalogService) invalidate(id uint) {
	s.mu.Lock()
	delete(s.cache, id)
	delete(s.cacheLoadedAt, id)
	s.mu.Unlock()
}

// EffectiveCatalogDefs 目录视图层 union：默认行在前（keywords 先合
// intent_keywords_override KV），自定义行追加在后（词面计分语义不变，默认优先）。
// 目录未播种/读失败 → 回落 DefaultIntents（零行为变化）；有行但全禁用 → 空词表
// （尊重管理端禁用动作）。
func (s *IntentCatalogService) EffectiveCatalogDefs(ctx context.Context) []IntentDef {
	rows := s.loadCatalog(ctx)
	if rows == nil {
		return DefaultIntents
	}
	if len(rows) == 0 && !s.hasAnyRow(ctx) {
		return DefaultIntents
	}
	override := intentOverrideFor(ctx)
	out := make([]IntentDef, 0, len(rows))
	for _, row := range rows {
		if row.Source != intentCatalogSourceDefault {
			continue
		}
		def := catalogRowToDef(row)
		if def == nil {
			continue
		}
		if extra, ok := override[row.IntentKey]; ok && len(extra) > 0 {
			def.Keywords = mergeOverrideKeywords(def.Keywords, extra)
		}
		out = append(out, *def)
	}
	for _, row := range rows {
		if row.Source != intentCatalogSourceCustom {
			continue
		}
		if def := catalogRowToDef(row); def != nil {
			out = append(out, *def)
		}
	}
	return out
}

// IsCatalogIntent 目录放行（source=default∪custom 启用行）；目录未播种/读失败
// 时回落既有双词表+自定义校验（零行为变化）。
func (s *IntentCatalogService) IsCatalogIntent(ctx context.Context, key string) bool {
	if key == "" {
		return false
	}
	rows := s.loadCatalog(ctx)
	if rows == nil {
		if isValidChainIntent(key) {
			return true
		}
		if svc := GetCustomIntentService(); svc != nil {
			return svc.IsCustomIntentType(ctx, key)
		}
		return false
	}
	for _, row := range rows {
		if row.IntentKey == key {
			return true
		}
	}
	return false
}

// IntentFamilyOf 兼容视图映射：runtime/service/custom（both 归 service——
// 重叠键 greeting/complaint/unknown 语义一致；未登录键归 custom）。
func IntentFamilyOf(key string) string {
	switch ChainIntentFamily(key) {
	case intentCatalogFamilyRuntime:
		return intentCatalogFamilyRuntime
	case intentCatalogFamilyService, "both":
		return intentCatalogFamilyService
	default:
		return intentCatalogFamilyCustom
	}
}

// SeedBuiltinDefaults 内置种子幂等写入（DefaultTenantID）：service 16 家族全量
// + runtime 独有 9 键（重叠 greeting/complaint/unknown 归 service）。
// 已有行（含禁用占位）跳过；返回实际写入数。
func (s *IntentCatalogService) SeedBuiltinDefaults(ctx context.Context) (int, error) {
	if s.repo == nil {
		return 0, errors.New("intent catalog service: repo not ready")
	}
	seedDefs := make(map[string]IntentDef, len(DefaultIntents))
	for _, d := range DefaultIntents {
		seedDefs[d.Type] = d
	}
	keys := make([]string, 0, len(validChainServiceIntents)+len(validChainIntents))
	for k := range validChainServiceIntents {
		keys = append(keys, k)
	}
	for k := range validChainIntents {
		if !validChainServiceIntents[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	n := 0
	for _, key := range keys {
		if existing, err := s.repo.GetByKeyAny(ctx, key); err != nil {
			return n, err
		} else if existing != nil {
			continue
		}
		name := key
		desc := "agent runtime 内置意图（TB-10 种子）"
		kwJSON, exJSON := []byte("[]"), []byte("[]")
		if def, ok := seedDefs[key]; ok {
			name = def.Name
			desc = def.Description
			if b, err := json.Marshal(def.Keywords); err == nil {
				kwJSON = b
			}
			if b, err := json.Marshal(def.Examples); err == nil {
				exJSON = b
			}
		}
		rec := &model.IntentCatalog{
			TenantID:     tenant.DefaultTenantID,
			IntentKey:    key,
			Source:       intentCatalogSourceDefault,
			Family:       IntentFamilyOf(key),
			Name:         name,
			KeywordsJSON: string(kwJSON),
			ExamplesJSON: string(exJSON),
			Description:  desc,
			Enabled:      true,
		}
		if err := s.repo.Create(ctx, rec); err != nil {
			return n, err
		}
		n++
	}
	s.invalidate(tenantIDOfCtx(ctx))
	return n, nil
}

// hasAnyRow 当前租户是否存在任意目录行（含禁用）。
func (s *IntentCatalogService) hasAnyRow(ctx context.Context) bool {
	if s.repo == nil {
		return false
	}
	ok, err := s.repo.HasAnyRow(ctx)
	return err == nil && ok
}

// catalogRowToDef 目录行 → IntentDef；keywords JSON 解析失败/空词表 fail-closed 跳过。
func catalogRowToDef(rec model.IntentCatalog) *IntentDef {
	var keywords, examples []string
	if err := json.Unmarshal([]byte(rec.KeywordsJSON), &keywords); err != nil || len(keywords) == 0 {
		return nil
	}
	_ = json.Unmarshal([]byte(rec.ExamplesJSON), &examples)
	desc := rec.Description
	if strings.TrimSpace(desc) == "" {
		desc = rec.Name
	}
	return &IntentDef{
		Type:        rec.IntentKey,
		Name:        rec.Name,
		Keywords:    keywords,
		Examples:    examples,
		Description: desc,
	}
}

// mergeOverrideKeywords 词面覆盖合并（逐字对齐 intentsForRuleWithOverride 语义：
// 默认词在前、去重、TrimSpace、空词跳过）。
func mergeOverrideKeywords(base, extra []string) []string {
	seen := make(map[string]bool, len(base)+len(extra))
	for _, kw := range base {
		seen[kw] = true
	}
	appended := append([]string(nil), base...)
	for _, kw := range extra {
		kw = strings.TrimSpace(kw)
		if kw != "" && !seen[kw] {
			appended = append(appended, kw)
			seen[kw] = true
		}
	}
	return appended
}
